// Copyright 2026 PolitePixels Limited
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

package sqlexec

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
)

var errConnectionLost = errors.New("connection lost")

type progressRow struct {
	appliedAt       time.Time
	rawAppliedAt    driver.Value
	lastStatement   *int64
	splitterVersion *int64
	statementsHash  *string
	downChecksum    *string
	name            string
	checksum        string
	durationMs      int64
	dirty           bool
}

type progressStore struct {
	mu               sync.Mutex
	rows             map[int64]progressRow
	failures         map[string]error
	columns          map[string]bool
	executed         []string
	transactions     int
	tableExists      bool
	refuseAlter      bool
	addedByOther     bool
	lost             bool
	replacing        bool
	rollbackError    error
	loseConnectionOn string
}

func newProgressStore() *progressStore {
	return &progressStore{rows: map[int64]progressRow{}, failures: map[string]error{}, columns: map[string]bool{}}
}

type progressConnector struct{ store *progressStore }

type progressDriver struct{}

type progressConn struct{ store *progressStore }

type progressTx struct{ store *progressStore }

type progressRows struct {
	columns []string
	values  [][]driver.Value
	cursor  int
}

func (store *progressStore) open() *sql.DB {
	return sql.OpenDB(progressConnector{store: store})
}

func (store *progressStore) createLegacyTable() {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.tableExists = true
}

func (store *progressStore) setRow(version int64, row progressRow) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.rows[version] = row
}

func (store *progressStore) row(version int64) (progressRow, bool) {
	store.mu.Lock()
	defer store.mu.Unlock()
	row, exists := store.rows[version]
	return row, exists
}

func (store *progressStore) failOn(statement string, err error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.failures[statement] = err
}

func (store *progressStore) loseConnectionAt(statement string) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.loseConnectionOn = statement
}

func (store *progressStore) emulateReplacingMergeTree() {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.replacing = true
}

func (store *progressStore) failRollbacks(err error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.rollbackError = err
}

func (store *progressStore) ran(prefix string) bool {
	store.mu.Lock()
	defer store.mu.Unlock()
	return slices.ContainsFunc(store.executed, func(statement string) bool { return strings.HasPrefix(statement, prefix) })
}

func (store *progressStore) reconnect() {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.lost = false
	store.loseConnectionOn = ""
}

func (store *progressStore) userStatements() []string {
	store.mu.Lock()
	defer store.mu.Unlock()
	var statements []string
	for _, statement := range store.executed {
		if strings.HasPrefix(statement, "STMT") || strings.HasPrefix(statement, "CREATE INDEX") || strings.HasPrefix(statement, "CREATE MATERIALIZED") {
			statements = append(statements, statement)
		}
	}
	return statements
}

func (store *progressStore) executedStatements() []string {
	store.mu.Lock()
	defer store.mu.Unlock()
	return append([]string(nil), store.executed...)
}

func (store *progressStore) resetExecuted() {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.executed = nil
}

func (store *progressStore) transactionCount() int {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.transactions
}

func (store *progressStore) hasColumns(names ...string) bool {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, name := range names {
		if !store.columns[name] {
			return false
		}
	}
	return true
}

func (store *progressStore) exec(query string, arguments []driver.NamedValue) (driver.Result, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.lost {
		return nil, errConnectionLost
	}
	store.executed = append(store.executed, query)
	if query == store.loseConnectionOn {
		store.lost = true
		return nil, errConnectionLost
	}
	if err, failing := store.failures[query]; failing {
		delete(store.failures, query)
		return nil, err
	}
	switch {
	case strings.HasPrefix(query, "CREATE TABLE IF NOT EXISTS sgl_migrations"):
		store.tableExists = true
	case strings.HasPrefix(query, "ALTER TABLE sgl_migrations ADD COLUMN"):
		return store.addColumn(query)
	case strings.HasPrefix(query, "INSERT INTO sgl_migrations"):
		return store.insert(arguments)
	case strings.HasPrefix(query, "UPDATE sgl_migrations SET last_statement"):
		return store.updateProgress(arguments)
	case strings.HasPrefix(query, "UPDATE sgl_migrations SET dirty"):
		row := store.rows[valueInt64(arguments, 2)]
		row.dirty = valueBool(arguments, 0)
		store.rows[valueInt64(arguments, 2)] = row
	case strings.HasPrefix(query, "UPDATE sgl_migrations SET name"):
		return store.rewrite(arguments)
	case strings.HasPrefix(query, "DELETE FROM sgl_migrations"), strings.HasPrefix(query, "ALTER TABLE sgl_migrations DELETE"):
		delete(store.rows, valueInt64(arguments, 0))
	}
	return driver.RowsAffected(1), nil
}

func (store *progressStore) addColumn(query string) (driver.Result, error) {
	fields := strings.Fields(query)
	column := fields[5]
	if store.refuseAlter {
		if store.addedByOther {
			store.columns[column] = true
		}
		return nil, fmt.Errorf("cannot add column %s", column)
	}
	if store.columns[column] {
		return nil, fmt.Errorf("duplicate column name: %s", column)
	}
	store.columns[column] = true
	return driver.RowsAffected(0), nil
}

func (store *progressStore) insert(arguments []driver.NamedValue) (driver.Result, error) {
	if !store.columns[splitterVersionColumn] || !store.columns[statementsHashColumn] {
		return nil, errors.New("no such column: splitter_version")
	}
	version := valueInt64(arguments, 0)
	var appliedAt time.Time
	if value, isTime := arguments[3].Value.(time.Time); isTime {
		appliedAt = value
	}
	if existing, exists := store.rows[version]; exists {
		if !store.replacing {
			return nil, errors.New("UNIQUE constraint failed: sgl_migrations.version")
		}
		if appliedAt.Before(existing.appliedAt) {
			return driver.RowsAffected(1), nil
		}
	}
	store.rows[version] = progressRow{
		appliedAt:       appliedAt,
		name:            valueString(arguments, 1),
		checksum:        valueString(arguments, 2),
		durationMs:      valueInt64(arguments, 4),
		downChecksum:    valueOptionalString(arguments, 5),
		lastStatement:   valueOptionalInt64(arguments, 6),
		dirty:           valueBool(arguments, 7),
		splitterVersion: valueOptionalInt64(arguments, 8),
		statementsHash:  valueOptionalString(arguments, 9),
	}
	return driver.RowsAffected(1), nil
}

func (store *progressStore) rewrite(arguments []driver.NamedValue) (driver.Result, error) {
	version := valueInt64(arguments, 7)
	row, exists := store.rows[version]
	if !exists {
		return driver.RowsAffected(0), nil
	}
	row.name = valueString(arguments, 0)
	row.checksum = valueString(arguments, 1)
	row.downChecksum = valueOptionalString(arguments, 2)
	row.dirty = valueBool(arguments, 3)
	row.lastStatement = valueOptionalInt64(arguments, 4)
	row.splitterVersion = valueOptionalInt64(arguments, 5)
	row.statementsHash = valueOptionalString(arguments, 6)
	store.rows[version] = row
	return driver.RowsAffected(1), nil
}

func (store *progressStore) updateProgress(arguments []driver.NamedValue) (driver.Result, error) {
	version := valueInt64(arguments, 3)
	row, exists := store.rows[version]
	if !exists {
		return driver.RowsAffected(0), nil
	}
	row.lastStatement = valueOptionalInt64(arguments, 0)
	row.splitterVersion = valueOptionalInt64(arguments, 1)
	row.statementsHash = valueOptionalString(arguments, 2)
	store.rows[version] = row
	return driver.RowsAffected(1), nil
}

func (store *progressStore) query(query string, arguments []driver.NamedValue) (driver.Rows, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.lost {
		return nil, errConnectionLost
	}
	store.executed = append(store.executed, query)
	switch {
	case query == mysqlEngine().currentSchemaSQL:
		return &progressRows{columns: []string{"database"}, values: [][]driver.Value{{nil}}}, nil
	case strings.Contains(query, "sqlite_master"), strings.Contains(query, "information_schema.tables"):
		exists := int64(0)
		if store.tableExists {
			exists = 1
		}
		return &progressRows{columns: []string{"count"}, values: [][]driver.Value{{exists}}}, nil
	case !store.tableExists:
		return nil, errors.New("no such table: sgl_migrations")
	case strings.HasSuffix(query, "WHERE 1 = 0"):
		column := strings.Fields(query)[1]
		if !store.columns[column] {
			return nil, fmt.Errorf("no such column: %s", column)
		}
		return &progressRows{columns: []string{column}}, nil
	case strings.HasPrefix(query, "SELECT count(*) FROM sgl_migrations"):
		count := int64(0)
		if _, exists := store.rows[valueInt64(arguments, 0)]; exists {
			count = 1
		}
		return &progressRows{columns: []string{"count"}, values: [][]driver.Value{{count}}}, nil
	case strings.HasPrefix(query, "SELECT version, name"):
		return store.historyRows(strings.Contains(query, statementsHashColumn))
	case strings.HasPrefix(query, "SELECT applied_at, duration_ms FROM sgl_migrations"):
		row := store.rows[valueInt64(arguments, 0)]
		var appliedAt driver.Value = row.appliedAt
		if row.rawAppliedAt != nil {
			appliedAt = row.rawAppliedAt
		}
		return &progressRows{columns: []string{"applied_at", "duration_ms"}, values: [][]driver.Value{{appliedAt, row.durationMs}}}, nil
	}
	return nil, fmt.Errorf("progressStore: unhandled query %q", query)
}

func (store *progressStore) historyRows(withProgress bool) (driver.Rows, error) {
	if withProgress && !store.columns[statementsHashColumn] {
		return nil, errors.New("no such column: splitter_version")
	}
	columns := []string{"version", "name", "checksum", "applied_at", "duration_ms", "down_checksum", "last_statement", "dirty"}
	if withProgress {
		columns = append(columns, splitterVersionColumn, statementsHashColumn)
	}
	rows := &progressRows{columns: columns}
	for _, version := range slices.Sorted(maps.Keys(store.rows)) {
		row := store.rows[version]
		values := []driver.Value{version, row.name, row.checksum, time.Unix(0, 0).UTC(), int64(1), optionalValue(row.downChecksum), optionalValue(row.lastStatement), row.dirty}
		if withProgress {
			values = append(values, optionalValue(row.splitterVersion), optionalValue(row.statementsHash))
		}
		rows.values = append(rows.values, values)
	}
	return rows, nil
}

func (connector progressConnector) Connect(context.Context) (driver.Conn, error) {
	return &progressConn{store: connector.store}, nil
}

func (progressConnector) Driver() driver.Driver { return progressDriver{} }

func (progressDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("progressDriver opens connections only through its connector")
}

func (*progressConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("progressConn does not prepare statements")
}

func (*progressConn) Close() error { return nil }

func (conn *progressConn) Begin() (driver.Tx, error) {
	return conn.BeginTx(context.Background(), driver.TxOptions{})
}

func (conn *progressConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	conn.store.mu.Lock()
	defer conn.store.mu.Unlock()
	conn.store.transactions++
	return progressTx{store: conn.store}, nil
}

func (conn *progressConn) ExecContext(_ context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
	return conn.store.exec(query, arguments)
}

func (conn *progressConn) QueryContext(_ context.Context, query string, arguments []driver.NamedValue) (driver.Rows, error) {
	return conn.store.query(query, arguments)
}

func (progressTx) Commit() error { return nil }

func (tx progressTx) Rollback() error {
	tx.store.mu.Lock()
	defer tx.store.mu.Unlock()
	return tx.store.rollbackError
}

func (rows *progressRows) Columns() []string { return rows.columns }

func (*progressRows) Close() error { return nil }

func (rows *progressRows) Next(destination []driver.Value) error {
	if rows.cursor >= len(rows.values) {
		return io.EOF
	}
	copy(destination, rows.values[rows.cursor])
	rows.cursor++
	return nil
}

func optionalValue[T any](value *T) driver.Value {
	if value == nil {
		return nil
	}
	return *value
}

func valueInt64(arguments []driver.NamedValue, position int) int64 {
	if pointer := valueOptionalInt64(arguments, position); pointer != nil {
		return *pointer
	}
	return 0
}

func valueOptionalInt64(arguments []driver.NamedValue, position int) *int64 {
	if position >= len(arguments) {
		return nil
	}
	if value, ok := arguments[position].Value.(int64); ok {
		return &value
	}
	return nil
}

func valueString(arguments []driver.NamedValue, position int) string {
	if pointer := valueOptionalString(arguments, position); pointer != nil {
		return *pointer
	}
	return ""
}

func valueOptionalString(arguments []driver.NamedValue, position int) *string {
	if position >= len(arguments) {
		return nil
	}
	if value, ok := arguments[position].Value.(string); ok {
		return &value
	}
	return nil
}

func valueBool(arguments []driver.NamedValue, position int) bool {
	if position >= len(arguments) {
		return false
	}
	value, ok := arguments[position].Value.(bool)
	return ok && value
}

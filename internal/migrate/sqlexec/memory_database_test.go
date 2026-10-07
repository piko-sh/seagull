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
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	memorySchema          = "public"
	memorySchemaQualifier = `"` + memorySchema + `".`
)

var (
	advisoryCallPattern = regexp.MustCompile(`pg_(try_)?advisory_(un)?lock\(hashtext\(\$1\)\)`)
	createTablePattern  = regexp.MustCompile(`CREATE TABLE IF NOT EXISTS ([a-z_]+)`)
	rowLockPattern      = regexp.MustCompile(`SELECT lock_id FROM (?:\[\d+ AS )?([a-z_]+)\]? FOR UPDATE( NOWAIT)?`)
)

type sqlStateError struct {
	state   string
	message string
}

type historyRow struct {
	name     string
	checksum string
	dirty    bool
}

type memoryDatabase struct {
	mu                    sync.Mutex
	tables                map[string]bool
	history               map[int64]historyRow
	sessionLocks          map[string]chan struct{}
	sessionLockOwners     map[string]int64
	rowLocks              map[string]chan struct{}
	rowLockOwners         map[string]int64
	lockTableRaces        int
	failUnlock            bool
	lockLost              bool
	lockChecks            []int64
	statements            []string
	ddlOutsideLock        []string
	nextSession           atomic.Int64
	openSessions          atomic.Int64
	closedSessions        atomic.Int64
	activeStatementOnConn map[int64]int
	overlappingUse        atomic.Int32
}

func newMemoryDatabase() *memoryDatabase {
	return &memoryDatabase{
		tables:                map[string]bool{},
		history:               map[int64]historyRow{},
		sessionLocks:          map[string]chan struct{}{},
		sessionLockOwners:     map[string]int64{},
		rowLocks:              map[string]chan struct{}{},
		rowLockOwners:         map[string]int64{},
		activeStatementOnConn: map[int64]int{},
	}
}

type memoryConnector struct {
	database *memoryDatabase
}

type memoryDriver struct{}

type memoryConn struct {
	database *memoryDatabase
	session  int64
}

type memoryTx struct {
	connection *memoryConn
}

type memoryRows struct {
	columns []string
	values  [][]driver.Value
	cursor  int
}

func (e sqlStateError) Error() string    { return e.message }
func (e sqlStateError) SQLState() string { return e.state }

func (database *memoryDatabase) open() *sql.DB {
	return sql.OpenDB(memoryConnector{database: database})
}

func (database *memoryDatabase) executed() []string {
	database.mu.Lock()
	defer database.mu.Unlock()
	return append([]string(nil), database.statements...)
}

func (database *memoryDatabase) outsideLock() []string {
	database.mu.Lock()
	defer database.mu.Unlock()
	return append([]string(nil), database.ddlOutsideLock...)
}

func (database *memoryDatabase) appliedVersions() []int64 {
	database.mu.Lock()
	defer database.mu.Unlock()
	versions := make([]int64, 0, len(database.history))
	for version := range database.history {
		versions = append(versions, version)
	}
	return versions
}

func (database *memoryDatabase) lockChannel(locks map[string]chan struct{}, key string) chan struct{} {
	database.mu.Lock()
	defer database.mu.Unlock()
	channel, ok := locks[key]
	if !ok {
		channel = make(chan struct{}, 1)
		locks[key] = channel
	}
	return channel
}

func (database *memoryDatabase) anyLockHeld() bool {
	return len(database.sessionLockOwners) > 0 || len(database.rowLockOwners) > 0
}

func (database *memoryDatabase) takeLock(ctx context.Context, locks map[string]chan struct{}, owners map[string]int64, key string, session int64, wait bool) (bool, error) {
	channel := database.lockChannel(locks, key)
	if !wait {
		select {
		case channel <- struct{}{}:
		default:
			return false, nil
		}
	} else {
		select {
		case channel <- struct{}{}:
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	database.mu.Lock()
	owners[key] = session
	database.mu.Unlock()
	return true, nil
}

func (database *memoryDatabase) dropLock(locks map[string]chan struct{}, owners map[string]int64, key string, session int64) bool {
	database.mu.Lock()
	owner, held := owners[key]
	if !held || owner != session {
		database.mu.Unlock()
		return false
	}
	delete(owners, key)
	channel := locks[key]
	database.mu.Unlock()
	<-channel
	return true
}

func (database *memoryDatabase) dropRowLocksOf(session int64) {
	database.mu.Lock()
	var held []string
	for key, owner := range database.rowLockOwners {
		if owner == session {
			held = append(held, key)
		}
	}
	database.mu.Unlock()
	for _, key := range held {
		database.dropLock(database.rowLocks, database.rowLockOwners, key, session)
	}
}

func (database *memoryDatabase) dropSessionLocksOf(session int64) {
	database.mu.Lock()
	var held []string
	for key, owner := range database.sessionLockOwners {
		if owner == session {
			held = append(held, key)
		}
	}
	database.mu.Unlock()
	for _, key := range held {
		database.dropLock(database.sessionLocks, database.sessionLockOwners, key, session)
	}
}

func (database *memoryDatabase) record(query string) {
	database.mu.Lock()
	defer database.mu.Unlock()
	database.statements = append(database.statements, strings.TrimSpace(query))
}

func (database *memoryDatabase) exec(ctx context.Context, session int64, query string, arguments []driver.NamedValue) (driver.Result, error) {
	database.record(query)
	query = strings.ReplaceAll(query, memorySchemaQualifier, "")
	if query == lockCheckSQL {
		return database.checkLock(session)
	}
	if strings.HasPrefix(query, "SELECT drop_lock(") {
		database.dropLock(database.sessionLocks, database.sessionLockOwners, namedString(arguments, 0), session)
		return driver.RowsAffected(0), nil
	}
	if match := advisoryCallPattern.FindStringSubmatch(query); match != nil {
		return database.execAdvisory(ctx, session, match, namedString(arguments, 0))
	}
	if match := createTablePattern.FindStringSubmatch(query); match != nil {
		return database.createTable(match[1])
	}
	if match := rowLockPattern.FindStringSubmatch(query); match != nil {
		acquired, err := database.takeLock(ctx, database.rowLocks, database.rowLockOwners, match[1], session, match[2] == "")
		if err != nil {
			return nil, err
		}
		if !acquired {
			return nil, sqlStateError{state: "55P03", message: "could not obtain lock on row"}
		}
		return driver.RowsAffected(1), nil
	}
	if strings.HasPrefix(query, "INSERT INTO") && strings.Contains(query, "lock (lock_id)") {
		return driver.RowsAffected(0), nil
	}
	if strings.HasPrefix(query, "INSERT INTO sgl_migrations") {
		return database.insertHistory(arguments)
	}
	if strings.HasPrefix(query, "UPDATE sgl_migrations SET dirty") {
		return database.clearDirty(arguments)
	}
	if strings.HasPrefix(query, "DELETE FROM sgl_migrations") {
		database.mu.Lock()
		delete(database.history, namedInt64(arguments, 0))
		database.mu.Unlock()
		return driver.RowsAffected(1), nil
	}
	return driver.RowsAffected(0), nil
}

func (database *memoryDatabase) execAdvisory(ctx context.Context, session int64, match []string, key string) (driver.Result, error) {
	if match[2] == "un" {
		if database.failUnlock {
			return nil, errors.New("unlock timed out")
		}
		database.dropLock(database.sessionLocks, database.sessionLockOwners, key, session)
		return driver.RowsAffected(0), nil
	}
	if _, err := database.takeLock(ctx, database.sessionLocks, database.sessionLockOwners, key, session, true); err != nil {
		return nil, err
	}
	return driver.RowsAffected(0), nil
}

func (database *memoryDatabase) checkLock(session int64) (driver.Result, error) {
	database.mu.Lock()
	defer database.mu.Unlock()
	database.lockChecks = append(database.lockChecks, session)
	if database.lockLost {
		return nil, errors.New("FATAL: terminating connection due to idle-in-transaction timeout")
	}
	return driver.RowsAffected(0), nil
}

func (database *memoryDatabase) loseLock() {
	database.mu.Lock()
	defer database.mu.Unlock()
	database.lockLost = true
}

func (database *memoryDatabase) checkedSessions() []int64 {
	database.mu.Lock()
	defer database.mu.Unlock()
	return append([]int64(nil), database.lockChecks...)
}

func (database *memoryDatabase) rowLockOwner(table string) int64 {
	database.mu.Lock()
	defer database.mu.Unlock()
	return database.rowLockOwners[table]
}

func (database *memoryDatabase) createTable(table string) (driver.Result, error) {
	database.mu.Lock()
	defer database.mu.Unlock()
	if strings.HasSuffix(table, "_lock") {
		if database.lockTableRaces > 0 && !database.tables[table] {
			database.lockTableRaces--
			database.tables[table] = true
			return nil, sqlStateError{state: "23505", message: `duplicate key value violates unique constraint "pg_type_typname_nsp_index"`}
		}
	} else if !database.anyLockHeld() {
		database.ddlOutsideLock = append(database.ddlOutsideLock, table)
	}
	database.tables[table] = true
	return driver.RowsAffected(0), nil
}

func (database *memoryDatabase) insertHistory(arguments []driver.NamedValue) (driver.Result, error) {
	database.mu.Lock()
	defer database.mu.Unlock()
	if !database.tables["sgl_migrations"] {
		return nil, errors.New(`relation "sgl_migrations" does not exist`)
	}
	version := namedInt64(arguments, 0)
	if _, exists := database.history[version]; exists {
		return nil, sqlStateError{state: "23505", message: "duplicate key value violates unique constraint"}
	}
	database.history[version] = historyRow{name: namedString(arguments, 1), checksum: namedString(arguments, 2), dirty: namedBool(arguments, 7)}
	return driver.RowsAffected(1), nil
}

func (database *memoryDatabase) clearDirty(arguments []driver.NamedValue) (driver.Result, error) {
	database.mu.Lock()
	defer database.mu.Unlock()
	version := namedInt64(arguments, 2)
	row, exists := database.history[version]
	if !exists {
		return driver.RowsAffected(0), nil
	}
	row.dirty = namedBool(arguments, 0)
	database.history[version] = row
	return driver.RowsAffected(1), nil
}

func (database *memoryDatabase) query(ctx context.Context, session int64, query string, arguments []driver.NamedValue) (driver.Rows, error) {
	database.record(query)
	if query == postgresEngine().currentSchemaSQL {
		return &memoryRows{columns: []string{"current_schema"}, values: [][]driver.Value{{memorySchema}}}, nil
	}
	query = strings.ReplaceAll(query, memorySchemaQualifier, "")
	if strings.HasPrefix(query, "SELECT take_lock(") {
		acquired, err := database.takeLock(ctx, database.sessionLocks, database.sessionLockOwners, namedString(arguments, 0), session, !strings.Contains(query, ", 0)"))
		if err != nil {
			return nil, err
		}
		return &memoryRows{columns: []string{"acquired"}, values: [][]driver.Value{{acquired}}}, nil
	}
	if advisoryCallPattern.MatchString(query) {
		acquired, err := database.takeLock(ctx, database.sessionLocks, database.sessionLockOwners, namedString(arguments, 0), session, false)
		if err != nil {
			return nil, err
		}
		return &memoryRows{columns: []string{"acquired"}, values: [][]driver.Value{{acquired}}}, nil
	}
	database.mu.Lock()
	defer database.mu.Unlock()
	switch {
	case strings.Contains(query, "::REGCLASS::OID"):
		return &memoryRows{columns: []string{"oid"}, values: [][]driver.Value{{int64(104)}}}, nil
	case strings.Contains(query, "to_regclass"), strings.Contains(query, "information_schema.tables"):
		exists := int64(0)
		if database.tables[strings.TrimPrefix(namedString(arguments, 0), memorySchemaQualifier)] {
			exists = 1
		}
		return &memoryRows{columns: []string{"exists"}, values: [][]driver.Value{{exists}}}, nil
	case !database.tables["sgl_migrations"]:
		return nil, errors.New(`relation "sgl_migrations" does not exist`)
	case strings.HasPrefix(query, "SELECT count(*) FROM sgl_migrations"):
		count := int64(0)
		if _, exists := database.history[namedInt64(arguments, 0)]; exists {
			count = 1
		}
		return &memoryRows{columns: []string{"count"}, values: [][]driver.Value{{count}}}, nil
	case strings.HasPrefix(query, "SELECT version, name"):
		return database.historyRows(), nil
	default:
		return nil, fmt.Errorf("memoryDatabase: unhandled query %q", query)
	}
}

func (database *memoryDatabase) historyRows() driver.Rows {
	rows := &memoryRows{columns: []string{"version", "name", "checksum", "applied_at", "duration_ms", "down_checksum", "last_statement", "dirty"}}
	for version := int64(1); version <= int64(len(database.history))+64; version++ {
		row, exists := database.history[version]
		if !exists {
			continue
		}
		rows.values = append(rows.values, []driver.Value{version, row.name, row.checksum, time.Unix(0, 0).UTC(), int64(1), nil, nil, row.dirty})
	}
	return rows
}

func (database *memoryDatabase) enter(session int64) func() {
	database.mu.Lock()
	database.activeStatementOnConn[session]++
	if database.activeStatementOnConn[session] > 1 {
		database.overlappingUse.Add(1)
	}
	database.mu.Unlock()
	return func() {
		database.mu.Lock()
		database.activeStatementOnConn[session]--
		database.mu.Unlock()
	}
}

func (connector memoryConnector) Connect(context.Context) (driver.Conn, error) {
	connector.database.openSessions.Add(1)
	return &memoryConn{database: connector.database, session: connector.database.nextSession.Add(1)}, nil
}

func (connector memoryConnector) Driver() driver.Driver { return memoryDriver{} }

func (memoryDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("memoryDriver opens connections only through its connector")
}

func (*memoryConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("memoryConn does not prepare statements")
}

func (conn *memoryConn) Close() error {
	conn.database.closedSessions.Add(1)
	conn.database.dropRowLocksOf(conn.session)
	conn.database.dropSessionLocksOf(conn.session)
	return nil
}

func (conn *memoryConn) Begin() (driver.Tx, error) {
	return memoryTx{connection: conn}, nil
}

func (conn *memoryConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return memoryTx{connection: conn}, nil
}

func (conn *memoryConn) ExecContext(ctx context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
	leave := conn.database.enter(conn.session)
	defer leave()
	return conn.database.exec(ctx, conn.session, query, arguments)
}

func (conn *memoryConn) QueryContext(ctx context.Context, query string, arguments []driver.NamedValue) (driver.Rows, error) {
	leave := conn.database.enter(conn.session)
	defer leave()
	return conn.database.query(ctx, conn.session, query, arguments)
}

func (tx memoryTx) Commit() error {
	tx.connection.database.dropRowLocksOf(tx.connection.session)
	return nil
}

func (tx memoryTx) Rollback() error {
	tx.connection.database.dropRowLocksOf(tx.connection.session)
	return nil
}

func (rows *memoryRows) Columns() []string { return rows.columns }

func (*memoryRows) Close() error { return nil }

func (rows *memoryRows) Next(destination []driver.Value) error {
	if rows.cursor >= len(rows.values) {
		return io.EOF
	}
	copy(destination, rows.values[rows.cursor])
	rows.cursor++
	return nil
}

func namedString(arguments []driver.NamedValue, position int) string {
	if position >= len(arguments) {
		return ""
	}
	if value, ok := arguments[position].Value.(string); ok {
		return value
	}
	return ""
}

func namedBool(arguments []driver.NamedValue, position int) bool {
	if position >= len(arguments) {
		return false
	}
	value, ok := arguments[position].Value.(bool)
	return ok && value
}

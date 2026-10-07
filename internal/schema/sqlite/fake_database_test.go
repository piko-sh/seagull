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

package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

var errFakeUnsupported = errors.New("fake database: operation not supported")

type fakeResult struct {
	err        error
	nextError  error
	closeError error
	columns    []string
	rows       [][]driver.Value
}

type fakeTable struct {
	kind         string
	statement    string
	columns      [][]driver.Value
	indexes      [][]driver.Value
	withoutRowID bool
	strict       bool
}

type fakeSchema struct {
	tables     map[string]fakeTable
	views      map[string]string
	failures   map[string]fakeResult
	queries    map[string]int
	viewOrder  []string
	tableOrder []string
}

func newFakeSchema() *fakeSchema {
	return &fakeSchema{tables: map[string]fakeTable{}, views: map[string]string{}, failures: map[string]fakeResult{}, queries: map[string]int{}}
}

type fakeConnector struct {
	schema *fakeSchema
}

type fakeDriver struct{}

type fakeConnection struct {
	schema *fakeSchema
}

type fakeRows struct {
	result   fakeResult
	position int
}

func (schema *fakeSchema) table(name string, columns ...[]driver.Value) *fakeSchema {
	existing := schema.tables[name]
	existing.kind = "table"
	existing.columns = columns
	schema.tables[name] = existing
	schema.tableOrder = append(schema.tableOrder, name)
	return schema
}

func (schema *fakeSchema) virtual(name, statement string, columns ...[]driver.Value) *fakeSchema {
	schema.table(name, columns...)
	existing := schema.tables[name]
	existing.kind = "virtual"
	existing.statement = statement
	schema.tables[name] = existing
	return schema
}

func (schema *fakeSchema) kind(table string, withoutRowID, strict bool) *fakeSchema {
	existing := schema.tables[table]
	existing.withoutRowID, existing.strict = withoutRowID, strict
	schema.tables[table] = existing
	return schema
}

func (schema *fakeSchema) index(table string, rows ...[]driver.Value) *fakeSchema {
	existing := schema.tables[table]
	existing.indexes = append(existing.indexes, rows...)
	schema.tables[table] = existing
	return schema
}

func (schema *fakeSchema) view(name, statement string, columns ...[]driver.Value) *fakeSchema {
	schema.views[name] = statement
	schema.viewOrder = append(schema.viewOrder, name)
	schema.tables[name] = fakeTable{kind: "view", columns: columns}
	return schema
}

func (schema *fakeSchema) fail(marker string, result fakeResult) *fakeSchema {
	schema.failures[marker] = result
	return schema
}

func (schema *fakeSchema) prefixed(names []string, rowsOf func(table fakeTable) [][]driver.Value) [][]driver.Value {
	var rows [][]driver.Value
	for _, name := range slices.Sorted(slices.Values(names)) {
		for _, row := range rowsOf(schema.tables[name]) {
			rows = append(rows, append([]driver.Value{name}, row...))
		}
	}
	return rows
}

func (schema *fakeSchema) respond(query string) fakeResult {
	schema.queries[query]++
	for marker, failure := range schema.failures {
		if strings.Contains(query, marker) {
			return failure
		}
	}
	switch query {
	case listTablesQuery:
		rows := make([][]driver.Value, 0, len(schema.tableOrder))
		for _, name := range slices.Sorted(slices.Values(schema.tableOrder)) {
			table := schema.tables[name]
			rows = append(rows, []driver.Value{name, table.kind, flag(table.withoutRowID), flag(table.strict), table.statement})
		}
		return fakeResult{columns: []string{"name", "type", "wr", "strict", "sql"}, rows: rows}
	case listViewsQuery:
		rows := make([][]driver.Value, 0, len(schema.viewOrder))
		for _, view := range schema.viewOrder {
			rows = append(rows, []driver.Value{view, schema.views[view]})
		}
		return fakeResult{columns: []string{"name", "sql"}, rows: rows}
	case relationColumnsQuery:
		rows := schema.prefixed(append(slices.Clone(schema.tableOrder), schema.viewOrder...), func(table fakeTable) [][]driver.Value { return table.columns })
		return fakeResult{columns: []string{"relation", "cid", "name", "type", "notnull", "dflt_value", "pk", "hidden"}, rows: rows}
	case tableIndexesQuery:
		rows := schema.prefixed(schema.tableOrder, func(table fakeTable) [][]driver.Value {
			if table.kind != "table" {
				return nil
			}
			return table.indexes
		})
		return fakeResult{columns: []string{"table", "name", "unique", "origin", "name"}, rows: rows}
	default:
		return fakeResult{err: fmt.Errorf("fake database: unexpected query %q", query)}
	}
}

func (connector fakeConnector) Connect(context.Context) (driver.Conn, error) {
	return fakeConnection(connector), nil
}

func (fakeConnector) Driver() driver.Driver {
	return fakeDriver{}
}

func (fakeDriver) Open(string) (driver.Conn, error) {
	return nil, errFakeUnsupported
}

func (fakeConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errFakeUnsupported
}

func (fakeConnection) Close() error {
	return nil
}

func (fakeConnection) Begin() (driver.Tx, error) {
	return nil, errFakeUnsupported
}

func (connection fakeConnection) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	result := connection.schema.respond(query)
	if result.err != nil {
		return nil, result.err
	}
	return &fakeRows{result: result, position: 0}, nil
}

func (rows *fakeRows) Columns() []string {
	return rows.result.columns
}

func (rows *fakeRows) Close() error {
	return rows.result.closeError
}

func (rows *fakeRows) Next(destination []driver.Value) error {
	if rows.position >= len(rows.result.rows) {
		if rows.result.nextError != nil {
			return rows.result.nextError
		}
		return io.EOF
	}
	copy(destination, rows.result.rows[rows.position])
	rows.position++
	return nil
}

func flag(set bool) int64 {
	if set {
		return 1
	}
	return 0
}

func column(position int64, name, declaredType string, notNull bool, defaultValue any, primaryKeyPosition, hidden int64) []driver.Value {
	notNullFlag := int64(0)
	if notNull {
		notNullFlag = 1
	}
	return []driver.Value{position, name, declaredType, notNullFlag, defaultValue, primaryKeyPosition, hidden}
}

func indexRow(name string, unique bool, origin string, columnName any) []driver.Value {
	uniqueFlag := int64(0)
	if unique {
		uniqueFlag = 1
	}
	return []driver.Value{name, uniqueFlag, origin, columnName}
}

func openFakeDatabase(t *testing.T, schema *fakeSchema) *sql.DB {
	t.Helper()

	database := sql.OpenDB(fakeConnector{schema: schema})
	t.Cleanup(func() { assert.NoError(t, database.Close()) })
	return database
}

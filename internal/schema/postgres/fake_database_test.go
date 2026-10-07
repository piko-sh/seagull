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

package postgres

import (
	"cmp"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

var (
	errFakeUnsupported      = errors.New("fake database: operation not supported")
	relationKindListPattern = regexp.MustCompile(`relkind IN \(([^)]*)\)`)
	excludedNameListPattern = regexp.MustCompile(`nspname NOT IN \(([^)]*)\)`)
	excludedPrefixPattern   = regexp.MustCompile(`NOT LIKE '([^']*)%'`)
)

type fakeQueryResult struct {
	err        error
	nextError  error
	closeError error
	columns    []string
	rows       [][]driver.Value
}

type fakeQueryHandler struct {
	respond        func(arguments []driver.NamedValue) fakeQueryResult
	respondToQuery func(query string, arguments []driver.NamedValue) fakeQueryResult
	marker         string
}

type fakeSchema struct {
	name           string
	extensionOwned bool
}

type fakeRelation struct {
	schema         string
	name           string
	kind           string
	partitionOf    string
	extensionOwned bool
}

type fakeConnector struct {
	handlers []fakeQueryHandler
}

type fakeDriver struct{}

type fakeConnection struct {
	handlers []fakeQueryHandler
}

type fakeRows struct {
	result   fakeQueryResult
	position int
}

func (connector *fakeConnector) Connect(context.Context) (driver.Conn, error) {
	return &fakeConnection{handlers: connector.handlers}, nil
}

func (*fakeConnector) Driver() driver.Driver {
	return fakeDriver{}
}

func (fakeDriver) Open(string) (driver.Conn, error) {
	return nil, errFakeUnsupported
}

func (*fakeConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errFakeUnsupported
}

func (*fakeConnection) Close() error {
	return nil
}

func (*fakeConnection) Begin() (driver.Tx, error) {
	return nil, errFakeUnsupported
}

func (connection *fakeConnection) QueryContext(
	_ context.Context,
	query string,
	arguments []driver.NamedValue,
) (driver.Rows, error) {
	for _, handler := range connection.handlers {
		if !strings.Contains(query, handler.marker) {
			continue
		}
		var result fakeQueryResult
		if handler.respondToQuery != nil {
			result = handler.respondToQuery(query, arguments)
		} else {
			result = handler.respond(arguments)
		}
		if result.err != nil {
			return nil, result.err
		}
		return &fakeRows{result: result, position: 0}, nil
	}
	return nil, fmt.Errorf("fake database: unexpected query %q", query)
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

func openFakeDatabase(t *testing.T, handlers ...fakeQueryHandler) *sql.DB {
	t.Helper()

	database := sql.OpenDB(&fakeConnector{handlers: handlers})
	t.Cleanup(func() { assert.NoError(t, database.Close()) })
	return database
}

func fixedResult(columns []string, rows ...[]driver.Value) func([]driver.NamedValue) fakeQueryResult {
	return func([]driver.NamedValue) fakeQueryResult {
		return fakeQueryResult{columns: columns, rows: rows, err: nil, nextError: nil, closeError: nil}
	}
}

func argumentString(arguments []driver.NamedValue, index int) string {
	if index >= len(arguments) {
		return ""
	}
	value, ok := arguments[index].Value.(string)
	if !ok {
		return ""
	}
	return value
}

func schemaInScope(query string, schema fakeSchema) bool {
	if schema.extensionOwned && strings.Contains(query, "deptype = 'e'") {
		return false
	}
	if match := excludedNameListPattern.FindStringSubmatch(query); match != nil &&
		strings.Contains(match[1], "'"+schema.name+"'") {
		return false
	}
	for _, match := range excludedPrefixPattern.FindAllStringSubmatch(query, -1) {
		if strings.HasPrefix(schema.name, strings.ReplaceAll(match[1], `\_`, "_")) {
			return false
		}
	}
	return true
}

func relationInScope(query string, relation fakeRelation) bool {
	match := relationKindListPattern.FindStringSubmatch(query)
	if match == nil || !strings.Contains(match[1], "'"+relation.kind+"'") {
		return false
	}
	return !relation.extensionOwned || !strings.Contains(query, "deptype = 'e'")
}

func scopedSchemas(schemas ...fakeSchema) func(string, []driver.NamedValue) fakeQueryResult {
	return func(query string, _ []driver.NamedValue) fakeQueryResult {
		result := fakeQueryResult{columns: []string{"nspname"}}
		for _, schema := range schemas {
			if schemaInScope(query, schema) {
				result.rows = append(result.rows, []driver.Value{schema.name})
			}
		}
		return result
	}
}

func scopedRelations(
	columns []string,
	describe func(relation fakeRelation) []driver.Value,
	relations ...fakeRelation,
) func(string, []driver.NamedValue) fakeQueryResult {
	return func(query string, _ []driver.NamedValue) fakeQueryResult {
		result := fakeQueryResult{columns: columns}
		for _, relation := range relations {
			if relationInScope(query, relation) {
				result.rows = append(result.rows, describe(relation))
			}
		}
		slices.SortFunc(result.rows, func(left, right []driver.Value) int {
			return cmp.Or(strings.Compare(fmt.Sprint(left[0]), fmt.Sprint(right[0])), strings.Compare(fmt.Sprint(left[1]), fmt.Sprint(right[1])))
		})
		return result
	}
}

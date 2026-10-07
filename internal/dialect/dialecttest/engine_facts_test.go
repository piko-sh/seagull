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

package dialecttest

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/martian"
)

const factsTable = "facts"

var errRefusedByTheEngine = errors.New("the scripted engine refuses the statement")

type scriptedAnswer struct {
	err     error
	columns []string
	rows    [][]driver.Value
}

type scriptedEngine struct {
	answer func(query string, arguments []driver.NamedValue) scriptedAnswer
	mu     sync.Mutex
	sent   []string
}

func (engine *scriptedEngine) Connect(context.Context) (driver.Conn, error) {
	return &scriptedConnection{engine: engine}, nil
}

func (engine *scriptedEngine) Driver() driver.Driver {
	return scriptedDriver{}
}

func (engine *scriptedEngine) record(statement string) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.sent = append(engine.sent, statement)
}

func (engine *scriptedEngine) statements() []string {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return slices.Clone(engine.sent)
}

type scriptedDriver struct{}

func (scriptedDriver) Open(string) (driver.Conn, error) {
	return nil, errRefusedByTheEngine
}

type scriptedConnection struct {
	engine *scriptedEngine
}

func (*scriptedConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errRefusedByTheEngine
}

func (*scriptedConnection) Close() error {
	return nil
}

func (*scriptedConnection) Begin() (driver.Tx, error) {
	return nil, errRefusedByTheEngine
}

func (*scriptedConnection) CheckNamedValue(*driver.NamedValue) error {
	return nil
}

func (connection *scriptedConnection) QueryContext(_ context.Context, query string, arguments []driver.NamedValue) (driver.Rows, error) {
	connection.engine.record(query)
	answer := connection.engine.answer(query, arguments)
	if answer.err != nil {
		return nil, answer.err
	}
	return &scriptedRows{columns: answer.columns, rows: answer.rows}, nil
}

func (connection *scriptedConnection) ExecContext(_ context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
	connection.engine.record(query)
	if answer := connection.engine.answer(query, arguments); answer.err != nil {
		return nil, answer.err
	}
	return driver.RowsAffected(0), nil
}

type scriptedRows struct {
	columns []string
	rows    [][]driver.Value
}

func (rows *scriptedRows) Columns() []string {
	return rows.columns
}

func (*scriptedRows) Close() error {
	return nil
}

func (rows *scriptedRows) Next(destination []driver.Value) error {
	if len(rows.rows) == 0 {
		return io.EOF
	}
	copy(destination, rows.rows[0])
	rows.rows = rows.rows[1:]
	return nil
}

func identifierRows(identifiers ...int64) scriptedAnswer {
	rows := make([][]driver.Value, 0, len(identifiers))
	for _, identifier := range identifiers {
		rows = append(rows, []driver.Value{identifier})
	}
	return scriptedAnswer{err: nil, columns: []string{"id"}, rows: rows}
}

func martianFacts(follow bool, ungrouped dialect.UngroupedColumnRule) func(string, []driver.NamedValue) scriptedAnswer {
	return func(query string, arguments []driver.NamedValue) scriptedAnswer {
		switch {
		case strings.HasPrefix(query, "CREATE VIEW ") || strings.HasPrefix(query, "ALTER TABLE "):
			return scriptedAnswer{}
		case strings.Contains(query, "NOT IN (SELECT NULL WHERE 1 = 0)"):
			return identifierRows(1, 2, 3)
		case strings.Contains(query, "IN (SELECT NULL WHERE 1 = 0)"):
			return identifierRows()
		case strings.HasSuffix(query, "ORDER BY id FETCH NEXT 2 ROWS ONLY"):
			return identifierRows(1, 2)
		case strings.HasSuffix(query, "ORDER BY id OFFSET 1 ROWS"):
			return identifierRows(2, 3)
		case strings.HasSuffix(query, "ORDER BY id OFFSET 1 ROWS FETCH NEXT 1 ROWS ONLY"):
			return identifierRows(2)
		case strings.Contains(query, "= ANY(@p1)"), strings.Contains(query, "<> ALL(@p1)"):
			return arrayFormRows(strings.Contains(query, "= ANY("), arguments)
		case strings.HasPrefix(query, "SELECT label, count(*)") && ungrouped != dialect.UngroupedColumnsNullable:
			return scriptedAnswer{err: errRefusedByTheEngine, columns: nil, rows: nil}
		case strings.HasPrefix(query, "SELECT label, count(*)"):
			return scriptedAnswer{err: nil, columns: []string{"label", "count"}, rows: [][]driver.Value{{nil, int64(0)}}}
		case strings.HasPrefix(query, "SELECT * FROM facts_star") && follow:
			return scriptedAnswer{err: nil, columns: []string{"id", "label", "extra"}, rows: nil}
		case strings.HasPrefix(query, "SELECT * FROM facts_star"):
			return scriptedAnswer{err: nil, columns: []string{"id", "label"}, rows: nil}
		case strings.HasPrefix(query, "SELECT CONVERT(label TO ") || strings.Contains(query, " AS bumped FROM facts WHERE bumped + 0 > 1"):
			return scriptedAnswer{err: nil, columns: []string{"value"}, rows: nil}
		default:
			return scriptedAnswer{err: fmt.Errorf("%w: %q", errRefusedByTheEngine, query), columns: nil, rows: nil}
		}
	}
}

func arrayFormRows(membership bool, arguments []driver.NamedValue) scriptedAnswer {
	values, isList := arguments[0].Value.([]int64)
	if !isList {
		return scriptedAnswer{err: errRefusedByTheEngine, columns: nil, rows: nil}
	}
	var identifiers []int64
	for _, identifier := range []int64{1, 2, 3} {
		if slices.Contains(values, identifier) == membership {
			identifiers = append(identifiers, identifier)
		}
	}
	return identifierRows(identifiers...)
}

func TestRunEngineFactsChecksTheInfoAgainstTheEngine(t *testing.T) {
	t.Parallel()

	arrayInfo := martian.New().Info()
	arrayInfo.Lists.ArrayMembership = dialect.Affix{Before: "= ANY(", After: ")"}
	arrayInfo.Lists.ArrayExclusion = dialect.Affix{Before: "<> ALL(", After: ")"}
	arrayInfo.ViewsFollowTheirTables = true
	arrayInfo.UngroupedColumns = dialect.UngroupedColumnsRejected
	arrayInfo.SelectAliasOperands = 0
	arrayInfo.Filters.Casts = nil
	testCases := []struct {
		answer         func(string, []driver.NamedValue) scriptedAnswer
		name           string
		info           dialect.Info
		expectSent     []string
		expectSentSome int
	}{
		{
			name:   "Martian's Info against an engine that behaves as it says",
			info:   martian.New().Info(),
			answer: martianFacts(false, dialect.UngroupedColumnsNullable),
			expectSent: []string{
				"SELECT id FROM facts WHERE id IN (SELECT NULL WHERE 1 = 0)",
				"SELECT id FROM facts ORDER BY id OFFSET 1 ROWS FETCH NEXT 1 ROWS ONLY",
				"SELECT CONVERT(label TO WHOLE64) FROM facts WHERE 1 = 0",
				"SELECT label, count(*) FROM facts WHERE 1 = 0",
				"SELECT id + 1 AS bumped FROM facts WHERE bumped + 0 > 1",
				"CREATE VIEW facts_star AS SELECT * FROM facts",
				"SELECT * FROM facts_star WHERE 1 = 0",
			},
			expectSentSome: 17,
		},
		{
			name:   "an Info with array forms, rejected ungrouped columns and views that follow their tables",
			info:   arrayInfo,
			answer: martianFacts(true, dialect.UngroupedColumnsRejected),
			expectSent: []string{
				"SELECT id FROM facts WHERE id = ANY(@p1) ORDER BY id",
				"SELECT id FROM facts WHERE id <> ALL(@p1) ORDER BY id",
				"SELECT label, count(*) FROM facts WHERE 1 = 0",
			},
			expectSentSome: 15,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := &scriptedEngine{answer: testCase.answer, mu: sync.Mutex{}, sent: nil}
			database := sql.OpenDB(engine)
			t.Cleanup(func() { require.NoError(t, database.Close()) })
			RunEngineFacts(t, &EngineFixture{Database: database, Table: factsTable, Info: testCase.info})
			sent := engine.statements()
			require.Len(t, sent, testCase.expectSentSome)
			for _, expected := range testCase.expectSent {
				require.Contains(t, sent, expected)
			}
		})
	}
}

func TestValidateEngineFixtureRejectsUnusableFixtures(t *testing.T) {
	t.Parallel()

	database := sql.OpenDB(&scriptedEngine{answer: nil, mu: sync.Mutex{}, sent: nil})
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	testCases := []struct {
		fixture  *EngineFixture
		name     string
		expected []string
	}{
		{name: "a usable fixture has no problems", fixture: &EngineFixture{Database: database, Table: factsTable}, expected: nil},
		{name: "a nil fixture", fixture: nil, expected: []string{"the EngineFixture is nil"}},
		{name: "a fixture without a database", fixture: &EngineFixture{Database: nil, Table: factsTable}, expected: []string{"EngineFixture.Database is nil"}},
		{
			name:     "a fixture naming no table",
			fixture:  &EngineFixture{Database: nil, Table: " "},
			expected: []string{"EngineFixture.Database is nil", "EngineFixture.Table names no table"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, validateEngineFixture(testCase.fixture))
		})
	}
}

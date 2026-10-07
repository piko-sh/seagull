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
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/fault"
)

func TestViewBodyAnalysisLetsAStoppedWalkUnwindToTheEntryPoint(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
	}{
		{name: "a query body", sql: "SELECT 1"},
		{name: "a body opening with a common table expression", sql: "WITH c AS (SELECT 1) SELECT * FROM c"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			statementParser := New().newStatementParser(stopEveryWalk, parsedStatementOf(t, testCase.sql))

			err := walkUnderRecover(t.Context(), func() { statementParser.analyseViewBody(nil) })

			require.ErrorIs(t, err, errWalkStopped)
		})
	}
}

func TestCreateViewFailsWhenAnalysingItsBodyPanics(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
	}{
		{name: "a view", sql: "CREATE VIEW v AS SELECT $1::widget"},
		{name: "a view with a column list", sql: "CREATE VIEW v (c) AS SELECT $1::widget"},
		{name: "a materialised view", sql: "CREATE MATERIALIZED VIEW v AS SELECT $1::widget WITH NO DATA"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, logs := newLoggingContext(t)

			mutation, err := applyWithContext(ctx, New(panicOnType(hookTypeName)), testCase.sql)

			assert.Nil(t, mutation)
			require.EqualError(t, err, "postgres: apply DDL: panic: hook fault")
			require.ErrorIs(t, err, errHookFault)
			requireRecoveredPanic(t, err, logs, "postgres: apply DDL")
		})
	}
}

func TestCreateViewStopsWithTheContextErrorWhenCancelledInItsBody(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		columns int
	}{
		{name: "a body of a few thousand columns", columns: 2_000},
		{name: "a body of many thousand columns", columns: 8_000},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			loggingContext, logs := newLoggingContext(t)
			ctx, cancel := context.WithCancelCause(loggingContext)
			defer cancel(nil)
			sql := "CREATE VIEW v AS SELECT $1::widget" + strings.Repeat(", 1", testCase.columns)

			mutation, err := applyWithContext(ctx, New(cancelOnType(hookTypeName, cancel)), sql)

			assert.Nil(t, mutation)
			require.ErrorIs(t, err, errAnalysisAbandoned)
			assert.NotErrorIs(t, err, errHookFault)
			assert.Empty(t, loggedRecords(t, logs))
		})
	}
}

func TestAViewBodyTheParserDoesNotAnalyseIsReportedOnTheMutation(t *testing.T) {
	t.Parallel()

	deepBody := "SELECT id FROM t WHERE " + strings.Repeat("(", 300) + "true" + strings.Repeat(")", 300)
	testCases := []struct {
		name        string
		sql         string
		body        string
		wantMessage string
		wantColumns []string
	}{
		{
			name:        "a view body nested beyond the depth limit",
			sql:         "CREATE VIEW v AS " + deepBody,
			body:        deepBody,
			wantMessage: "nests deeper than the parse depth limit",
			wantColumns: nil,
		},
		{
			name:        "a view with a column list whose body nests beyond the depth limit",
			sql:         "CREATE VIEW v (id) AS " + deepBody + " WITH CHECK OPTION",
			body:        deepBody,
			wantMessage: "nests deeper than the parse depth limit",
			wantColumns: []string{"id"},
		},
		{
			name:        "a table created from a prepared statement",
			sql:         "CREATE TABLE t2 AS EXECUTE fetch_rows (1)",
			body:        "EXECUTE fetch_rows (1)",
			wantMessage: "not a query Seagull analyses",
			wantColumns: nil,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyWithContext(t.Context(), New(), testCase.sql)

			require.NoError(t, err)
			require.NotNil(t, mutation)
			assert.Nil(t, mutation.ViewDefinition)
			assert.Equal(t, testCase.wantColumns, columnNames(mutation.Columns))
			require.Len(t, mutation.UnanalysedParts, 1)
			part := mutation.UnanalysedParts[0]
			assert.Contains(t, part.Message, testCase.wantMessage)
			assert.Equal(t, testCase.body, testCase.sql[part.Span.Start:part.Span.End])
		})
	}
}

func TestAViewBodyThatIsAnyQueryFormIsAnalysed(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantColumns []string
	}{
		{name: "a SELECT", sql: "CREATE VIEW v AS SELECT 1 AS a", wantColumns: []string{"a"}},
		{name: "a WITH query", sql: "CREATE VIEW v AS WITH c AS (SELECT 1 AS a) SELECT a FROM c", wantColumns: []string{"a"}},
		{name: "a VALUES list", sql: "CREATE VIEW v (a) AS VALUES (1)", wantColumns: []string{"a"}},
		{name: "a TABLE command", sql: "CREATE VIEW v AS TABLE users", wantColumns: []string{""}},
		{name: "a parenthesised query", sql: "CREATE VIEW v AS (SELECT 1 AS a)", wantColumns: []string{"a"}},
		{name: "a table created from a query", sql: "CREATE TABLE t2 AS SELECT 1 AS a WITH NO DATA", wantColumns: []string{"a"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyWithContext(t.Context(), New(), testCase.sql)

			require.NoError(t, err)
			require.NotNil(t, mutation)
			require.NotNil(t, mutation.ViewDefinition)
			assert.Empty(t, mutation.UnanalysedParts)
			assert.Equal(t, testCase.wantColumns, outputNames(mutation.ViewDefinition))
		})
	}
}

func TestAMalformedViewBodyFailsTheStatement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		wantToken string
	}{
		{name: "a view with no query after AS", sql: "CREATE VIEW v AS", wantToken: ""},
		{name: "a view whose query has an unclosed parenthesis", sql: "CREATE VIEW v AS SELECT (1", wantToken: ""},
		{name: "a view whose query runs on", sql: "CREATE VIEW v AS SELECT 1 nonsense words", wantToken: "words"},
		{name: "a table created from a malformed query", sql: "CREATE TABLE t2 AS SELECT 1 +", wantToken: ""},
		{name: "a materialised view with nothing before WITH NO DATA", sql: "CREATE MATERIALIZED VIEW v AS WITH NO DATA", wantToken: "WITH"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyWithContext(t.Context(), New(), testCase.sql)

			assert.Nil(t, mutation)
			require.ErrorIs(t, err, fault.ErrParse)
			parseError, isParseError := errors.AsType[*dialectkit.ParseError](err)
			require.True(t, isParseError)
			assert.Equal(t, testCase.wantToken, parseError.Token)
		})
	}
}

func TestACommentIsReadFromAnyStringConstant(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
		want string
	}{
		{name: "a plain string", sql: "COMMENT ON TABLE users IS 'a''b'", want: "a'b"},
		{name: "an escape string", sql: `COMMENT ON TABLE users IS E'line one\nline two'`, want: "line one\nline two"},
		{name: "a dollar-quoted string", sql: "COMMENT ON TABLE users IS $$it's$$", want: "it's"},
		{name: "a Unicode escape string", sql: `COMMENT ON TABLE users IS U&'caf\00e9'`, want: "café"},
		{name: "NULL", sql: "COMMENT ON TABLE users IS NULL", want: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyWithContext(t.Context(), New(), testCase.sql)

			require.NoError(t, err)
			require.NotNil(t, mutation)
			assert.Equal(t, testCase.want, mutation.Comment)
		})
	}
}

func TestAlterSequenceChangesOnlyASequence(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		wantMutation *ast.CatalogueMutation
		name         string
		sql          string
	}{
		{
			name: "a rename",
			sql:  "ALTER SEQUENCE orders_id_seq RENAME TO order_numbers",
			wantMutation: ast.NewCatalogueMutation(ast.MutationAlterTableRenameTable, "", "orders_id_seq",
				ast.WithNewName("order_numbers"), ast.WithIfExists(false), ast.WithTargetKind(ast.ObjectKindSequence)),
		},
		{
			name: "a qualified rename under IF EXISTS",
			sql:  "ALTER SEQUENCE IF EXISTS sales.orders_id_seq RENAME TO order_numbers",
			wantMutation: ast.NewCatalogueMutation(ast.MutationAlterTableRenameTable, "sales", "orders_id_seq",
				ast.WithNewName("order_numbers"), ast.WithIfExists(true), ast.WithTargetKind(ast.ObjectKindSequence)),
		},
		{
			name: "a move to another schema",
			sql:  "ALTER SEQUENCE orders_id_seq SET SCHEMA archive",
			wantMutation: ast.NewCatalogueMutation(ast.MutationAlterTableSetSchema, "", "orders_id_seq",
				ast.WithNewName("archive"), ast.WithIfExists(false), ast.WithTargetKind(ast.ObjectKindSequence)),
		},
		{name: "a change of options alone", sql: "ALTER SEQUENCE orders_id_seq INCREMENT BY 10 RESTART", wantMutation: nil},
		{name: "a change of owner", sql: "ALTER SEQUENCE orders_id_seq OWNED BY orders.id", wantMutation: nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyWithContext(t.Context(), New(), testCase.sql)

			require.NoError(t, err)
			assert.Equal(t, testCase.wantMutation, mutation)
		})
	}
}

func TestAMalformedAlterSequenceFailsTheStatement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		wantToken string
	}{
		{name: "no sequence name", sql: "ALTER SEQUENCE", wantToken: ""},
		{name: "no new name", sql: "ALTER SEQUENCE orders_id_seq RENAME TO", wantToken: ""},
		{name: "no new schema", sql: "ALTER SEQUENCE orders_id_seq SET SCHEMA", wantToken: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyWithContext(t.Context(), New(), testCase.sql)

			assert.Nil(t, mutation)
			parseError, isParseError := errors.AsType[*dialectkit.ParseError](err)
			require.True(t, isParseError, "got %v", err)
			assert.Equal(t, testCase.wantToken, parseError.Token)
		})
	}
}

func TestCreateViewReadsTheClausesBeforeAs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		wantNames []string
	}{
		{name: "WITH options", sql: "CREATE VIEW v WITH (security_barrier, check_option = local) AS SELECT 1 AS a", wantNames: []string{"a"}},
		{name: "a column list then WITH options", sql: "CREATE OR REPLACE VIEW v (x) WITH (fillfactor = 70) AS SELECT 1 AS a", wantNames: []string{"x"}},
		{name: "a materialised view's USING and TABLESPACE", sql: "CREATE MATERIALIZED VIEW v USING heap WITH (fillfactor = 70) TABLESPACE fast AS SELECT 1 AS a WITH NO DATA", wantNames: []string{"a"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)

			require.NotNil(t, mutation.ViewDefinition)
			var names []string
			for _, column := range mutation.ViewDefinition.OutputColumns {
				names = append(names, column.Name)
			}
			assert.Equal(t, testCase.wantNames, names)
		})
	}
}

func TestACreateViewWithoutItsQueryFailsTheStatement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantMessage string
	}{
		{name: "no AS", sql: "CREATE VIEW v SELECT 1", wantMessage: "expected keyword AS"},
		{name: "options but no AS", sql: "CREATE VIEW v WITH (security_barrier)", wantMessage: "expected keyword AS"},
		{name: "unbalanced options", sql: "CREATE VIEW v WITH (security_barrier AS SELECT 1", wantMessage: "unmatched parenthesis"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := applyDDLExpectError(t, testCase.sql)

			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, testCase.wantMessage, parseError.Message)
		})
	}
}

func TestDropStatementsRecordWhetherTheTargetMayBeMissing(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		sql          string
		wantIfExists bool
	}{
		{name: "DROP TABLE IF EXISTS", sql: "DROP TABLE IF EXISTS t CASCADE", wantIfExists: true},
		{name: "DROP TABLE", sql: "DROP TABLE t", wantIfExists: false},
		{name: "DROP VIEW IF EXISTS", sql: "DROP VIEW IF EXISTS v", wantIfExists: true},
		{name: "DROP MATERIALIZED VIEW IF EXISTS", sql: "DROP MATERIALIZED VIEW IF EXISTS v", wantIfExists: true},
		{name: "DROP VIEW", sql: "DROP VIEW v", wantIfExists: false},
		{name: "DROP INDEX IF EXISTS", sql: "DROP INDEX CONCURRENTLY IF EXISTS i", wantIfExists: true},
		{name: "DROP INDEX", sql: "DROP INDEX i", wantIfExists: false},
		{name: "DROP TRIGGER IF EXISTS", sql: "DROP TRIGGER IF EXISTS g ON t", wantIfExists: true},
		{name: "DROP TRIGGER", sql: "DROP TRIGGER g ON t", wantIfExists: false},
		{name: "DROP SEQUENCE IF EXISTS", sql: "DROP SEQUENCE IF EXISTS s", wantIfExists: true},
		{name: "DROP SEQUENCE", sql: "DROP SEQUENCE s", wantIfExists: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)

			require.NotNil(t, mutation)
			assert.Equal(t, testCase.wantIfExists, mutation.IfExists)
		})
	}
}

func TestAlterPrimaryKeyGivesTheTableItsNewKey(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		wantKind       ast.MutationKind
		wantPrimaryKey []string
		wantColumn     string
		options        []Option
	}{
		{
			name:           "where the flavour reads index options",
			sql:            "ALTER TABLE t ALTER PRIMARY KEY USING COLUMNS (b, a DESC)",
			options:        []Option{WithInlineIndexElements()},
			wantKind:       ast.MutationAlterTableAddConstraint,
			wantPrimaryKey: []string{"b", "a"},
		},
		{
			name:           "with index options after the list",
			sql:            "ALTER TABLE t ALTER PRIMARY KEY USING COLUMNS (id) USING HASH WITH (bucket_count = 4)",
			options:        []Option{WithInlineIndexElements()},
			wantKind:       ast.MutationAlterTableAddConstraint,
			wantPrimaryKey: []string{"id"},
		},
		{
			name:       "where it does not, as altering a column named primary",
			sql:        "ALTER TABLE t ALTER primary TYPE bigint",
			wantKind:   ast.MutationAlterTableAlterColumn,
			wantColumn: "primary",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyWithContext(t.Context(), New(testCase.options...), testCase.sql)

			require.NoError(t, err)
			assert.Equal(t, testCase.wantKind, mutation.Kind)
			assert.Equal(t, testCase.wantPrimaryKey, mutation.PrimaryKey)
			assert.Equal(t, testCase.wantColumn, mutation.ColumnName)
		})
	}
}

func TestAMalformedAlterPrimaryKeyFailsTheStatement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantMessage string
	}{
		{name: "no USING COLUMNS", sql: "ALTER TABLE t ALTER PRIMARY KEY (a)", wantMessage: "expected keyword USING"},
		{name: "no column list", sql: "ALTER TABLE t ALTER PRIMARY KEY USING COLUMNS a", wantMessage: "expected '(' to open the column list"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := applyWithContext(t.Context(), New(WithInlineIndexElements()), testCase.sql)

			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, testCase.wantMessage, parseError.Message)
		})
	}
}

func TestConsecutiveCommasInAnAlterTableAreALocatedSyntaxError(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		sql          string
		wantPosition int
	}{
		{name: "between two actions", sql: "ALTER TABLE users ADD COLUMN a text, , ADD COLUMN b text", wantPosition: len("ALTER TABLE users ADD COLUMN a text, ")},
		{name: "after the first action", sql: "ALTER TABLE users DROP COLUMN a,, DROP COLUMN b", wantPosition: len("ALTER TABLE users DROP COLUMN a,")},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := applyDDLExpectError(t, testCase.sql)

			require.ErrorIs(t, err, fault.ErrParse)
			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, `ALTER TABLE "users": consecutive commas in action list`, parseError.Message)
			assert.Equal(t, testCase.wantPosition, parseError.Position)
		})
	}
}

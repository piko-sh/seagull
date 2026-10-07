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

package mysql

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
)

func applyWith(t *testing.T, engine *Dialect, sql string) (*ast.CatalogueMutation, error) {
	t.Helper()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.Len(t, statements, 1)
	return engine.ApplyDDL(t.Context(), statements[0])
}

func describeDrops(mutation *ast.CatalogueMutation) []string {
	described := make([]string, 0, 1+len(mutation.AdditionalMutations))
	for _, drop := range append([]*ast.CatalogueMutation{mutation}, mutation.AdditionalMutations...) {
		ifExists := ""
		if drop.IfExists {
			ifExists = " if exists"
		}
		kind := "drop"
		switch drop.Kind {
		case ast.MutationDropView:
			kind = "drop_view"
		case ast.MutationDropTable:
			kind = "drop_table"
		default:
		}
		described = append(described, kind+" "+drop.SchemaName+"."+drop.TableName+ifExists)
	}
	return described
}

func TestDropViewDropsEveryViewItNames(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		expected []string
	}{
		{name: "one view", sql: "DROP VIEW va", expected: []string{"drop_view .va"}},
		{name: "a list of views", sql: "DROP VIEW va, shop.vb, vc", expected: []string{"drop_view .va", "drop_view shop.vb",
			"drop_view .vc"}},
		{name: "a list of views if they exist", sql: "DROP VIEW IF EXISTS va, vb CASCADE",
			expected: []string{"drop_view .va if exists", "drop_view .vb if exists"}},
		{name: "a list of views and RESTRICT", sql: "DROP VIEW va, vb RESTRICT", expected: []string{"drop_view .va", "drop_view .vb"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)

			assert.Equal(t, testCase.expected, describeDrops(mutation))
		})
	}
}

func TestAViewQueryMustBeWholeAndWellFormed(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		wantError string
	}{
		{name: "a malformed select list", sql: "CREATE VIEW v AS SELECT a b c d FROM", wantError: "syntax error"},
		{name: "words after the query", sql: "CREATE VIEW v AS SELECT 1 AS a garbage here", wantError: "syntax error near \"garbage\""},
		{name: "no query after AS", sql: "CREATE VIEW v AS", wantError: "expected a query after AS"},
		{name: "something other than a query", sql: "CREATE VIEW v AS DELETE FROM t", wantError: "expected a query after AS"},
		{name: "a check option that is not the last clause", sql: "CREATE VIEW v AS SELECT 1 AS a WITH CHECK OPTION FROM t",
			wantError: "syntax error"},
		{name: "a malformed query of CREATE TABLE AS", sql: "CREATE TABLE t AS SELECT a b c FROM", wantError: "syntax error"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyWith(t, New(), testCase.sql)

			require.ErrorContains(t, err, testCase.wantError)
			assert.Nil(t, mutation)
		})
	}
}

func TestAViewQueryMayCloseWithACheckOption(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
	}{
		{name: "WITH CHECK OPTION", sql: "CREATE VIEW v AS SELECT id FROM t WHERE id > 0 WITH CHECK OPTION"},
		{name: "WITH CASCADED CHECK OPTION", sql: "CREATE VIEW v AS SELECT id FROM t WITH CASCADED CHECK OPTION"},
		{name: "WITH LOCAL CHECK OPTION", sql: "CREATE VIEW v AS SELECT id FROM t WITH LOCAL CHECK OPTION"},
		{name: "a parenthesised query", sql: "CREATE VIEW v AS (SELECT id FROM t) WITH CHECK OPTION"},
		{name: "a closing semicolon inside a DELIMITER block",
			sql: "DELIMITER //\nCREATE VIEW v AS SELECT id FROM t WITH LOCAL CHECK OPTION;//\nDELIMITER ;"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)

			require.NotNil(t, mutation.ViewDefinition)
			assert.Equal(t, []string{"id"}, outputColumnNames(mutation.ViewDefinition))
			assert.Empty(t, mutation.UnanalysedParts)
		})
	}
}

func TestAViewQuerySeagullCannotAnalyseIsReportedAsUnanalysed(t *testing.T) {
	t.Parallel()

	const nesting = 64
	deepQuery := "SELECT a FROM t WHERE " + strings.Repeat("(", nesting) + "a = 1" + strings.Repeat(")", nesting)

	testCases := []struct {
		name        string
		sql         string
		wantMessage string
		wantColumns []string
	}{
		{
			name:        "a query nested past the parse depth",
			sql:         "CREATE VIEW v (a) AS " + deepQuery,
			wantMessage: "the query that defines view v nests more deeply than the parse depth limit of 8",
			wantColumns: []string{"a"},
		},
		{
			name:        "the query of CREATE TABLE AS nested past the parse depth",
			sql:         "CREATE TABLE copy AS " + deepQuery,
			wantMessage: "the query that defines table copy nests more deeply than the parse depth limit of 8",
			wantColumns: nil,
		},
		{
			name:        "a TABLE statement",
			sql:         "CREATE VIEW v AS TABLE t",
			wantMessage: "the TABLE statement that defines view v is not analysed",
			wantColumns: nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyWith(t, New(WithMaxParseDepth(8)), testCase.sql)

			require.NoError(t, err)
			assert.Nil(t, mutation.ViewDefinition)
			require.Len(t, mutation.UnanalysedParts, 1)
			assert.Contains(t, mutation.UnanalysedParts[0].Message, testCase.wantMessage)
			assert.Positive(t, mutation.UnanalysedParts[0].Span.End)
			var names []string
			for _, column := range mutation.Columns {
				names = append(names, column.Name)
			}
			assert.Equal(t, testCase.wantColumns, names)
		})
	}
}

func TestDDLHandlersReadTheirWholeStatement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		wantToken string
	}{
		{name: "a view without AS", sql: "CREATE VIEW v SELECT 1 AS a", wantToken: "SELECT"},
		{name: "a view column list missing a comma", sql: "CREATE VIEW v (a b) AS SELECT 1 AS a", wantToken: "b"},
		{name: "words after an index's key parts", sql: "CREATE INDEX i ON t (a) whatever 1 2 3", wantToken: "whatever"},
		{name: "an index with no key parts", sql: "CREATE INDEX i ON t", wantToken: ""},
		{name: "DROP INDEX naming no table after ON", sql: "DROP INDEX i ON", wantToken: ""},
		{name: "DROP INDEX naming no table", sql: "DROP INDEX i", wantToken: ""},
		{name: "a trigger naming no table after ON", sql: "CREATE TRIGGER tr AFTER INSERT ON", wantToken: ""},
		{name: "words after DROP TRIGGER", sql: "DROP TRIGGER tr extra", wantToken: "extra"},
		{name: "an unknown database option", sql: "CREATE DATABASE d DEFAULT CHARSET utf8mb4 nonsense", wantToken: "nonsense"},
		{name: "words after DROP DATABASE", sql: "DROP DATABASE d extra", wantToken: "extra"},
		{name: "words after DROP VIEW", sql: "DROP VIEW va vb", wantToken: "vb"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyWith(t, New(), testCase.sql)

			assert.Nil(t, mutation)
			requireParseErrorAt(t, err, testCase.wantToken)
		})
	}
}

func TestDDLHandlersReadTheOptionsMySQLWrites(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		wantKind ast.MutationKind
	}{
		{name: "an index with options", wantKind: ast.MutationCreateIndex,
			sql: "CREATE UNIQUE INDEX i USING BTREE ON t (a(10) DESC, (lower(b))) COMMENT 'x' INVISIBLE ALGORITHM = INPLACE LOCK NONE"},
		{name: "a MariaDB index that waits", wantKind: ast.MutationCreateIndex, sql: "CREATE INDEX i ON t (a) WAIT 5"},
		{name: "DROP INDEX with ALGORITHM and LOCK", wantKind: ast.MutationDropIndex,
			sql: "DROP INDEX i ON shop.t ALGORITHM = COPY LOCK = SHARED"},
		{name: "DROP INDEX that does not wait", wantKind: ast.MutationDropIndex, sql: "DROP INDEX IF EXISTS i ON t NOWAIT"},
		{name: "a database with options", wantKind: ast.MutationCreateSchema,
			sql: "CREATE DATABASE IF NOT EXISTS d DEFAULT CHARACTER SET = utf8mb4 COLLATE utf8mb4_bin ENCRYPTION 'N'"},
		{name: "a trigger whose body is one statement", wantKind: ast.MutationCreateTrigger,
			sql: "CREATE TRIGGER tr BEFORE INSERT ON t FOR EACH ROW SET NEW.a = 1"},
		{name: "a statement closed by a semicolon", wantKind: ast.MutationDropView, sql: "DROP VIEW v;"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)

			assert.Equal(t, testCase.wantKind, mutation.Kind)
		})
	}
}

func TestATriggerBodyRunsToTheEndOfTheStatement(t *testing.T) {
	t.Parallel()

	mutation := applyDDL(t, "CREATE TRIGGER touch BEFORE UPDATE ON scores FOR EACH ROW BEGIN "+
		"IF NEW.points < 0 THEN SET NEW.points = 0; END IF; CASE WHEN 1 THEN SET @x = 1; END CASE; END")

	assert.Equal(t, ast.MutationCreateTrigger, mutation.Kind)
	assert.Equal(t, "scores", mutation.TableName)
}

func TestTrimViewCheckOptionLeavesOtherEndingsAlone(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		wantCount int
	}{
		{name: "too short to hold the clause", sql: "CHECK OPTION", wantCount: 2},
		{name: "CHECK OPTION without WITH", sql: "SELECT 1 CHECK OPTION", wantCount: 4},
		{name: "a level without WITH", sql: "SELECT 1 LOCAL CHECK OPTION", wantCount: 5},
		{name: "WITH LOCAL CHECK OPTION", sql: "SELECT 1 WITH LOCAL CHECK OPTION", wantCount: 2},
		{name: "WITH CHECK OPTION alone", sql: "WITH CHECK OPTION", wantCount: 0},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens, err := tokenise(testCase.sql)
			require.NoError(t, err)

			assert.Len(t, trimViewCheckOption(tokens[:len(tokens)-1]), testCase.wantCount)
		})
	}
}

func TestDropStatementsRecordWhetherTheySayIfExists(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		sql          string
		wantKind     ast.MutationKind
		wantIfExists bool
	}{
		{name: "DROP INDEX", sql: "DROP INDEX idx ON t", wantKind: ast.MutationDropIndex, wantIfExists: false},
		{name: "DROP INDEX IF EXISTS", sql: "DROP INDEX IF EXISTS idx ON t", wantKind: ast.MutationDropIndex, wantIfExists: true},
		{name: "DROP TRIGGER", sql: "DROP TRIGGER tr", wantKind: ast.MutationDropTrigger, wantIfExists: false},
		{name: "DROP TRIGGER IF EXISTS", sql: "DROP TRIGGER IF EXISTS app.tr", wantKind: ast.MutationDropTrigger, wantIfExists: true},
		{name: "DROP DATABASE", sql: "DROP DATABASE app", wantKind: ast.MutationDropSchema, wantIfExists: false},
		{name: "DROP SCHEMA IF EXISTS", sql: "DROP SCHEMA IF EXISTS app", wantKind: ast.MutationDropSchema, wantIfExists: true},
		{name: "DROP FUNCTION", sql: "DROP FUNCTION f", wantKind: ast.MutationDropFunction, wantIfExists: false},
		{name: "DROP PROCEDURE IF EXISTS", sql: "DROP PROCEDURE IF EXISTS app.p", wantKind: ast.MutationDropFunction,
			wantIfExists: true},
		{name: "DROP TABLE IF EXISTS", sql: "DROP TABLE IF EXISTS t", wantKind: ast.MutationDropTable, wantIfExists: true},
		{name: "DROP VIEW", sql: "DROP VIEW v", wantKind: ast.MutationDropView, wantIfExists: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyWith(t, New(), testCase.sql)

			require.NoError(t, err)
			require.NotNil(t, mutation)
			assert.Equal(t, testCase.wantKind, mutation.Kind)
			assert.Equal(t, testCase.wantIfExists, mutation.IfExists)
		})
	}
}

func TestCreateOrReplaceDropsTheObjectOnlyIfItExists(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		wantKind ast.MutationKind
	}{
		{name: "a table", sql: "CREATE OR REPLACE TABLE t (a INT)", wantKind: ast.MutationDropTable},
		{name: "a database", sql: "CREATE OR REPLACE DATABASE app", wantKind: ast.MutationDropSchema},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyWith(t, New(WithCreateOrReplace(true)), testCase.sql)

			require.NoError(t, err)
			require.NotNil(t, mutation)
			assert.Equal(t, testCase.wantKind, mutation.Kind)
			assert.True(t, mutation.IfExists)
			require.NotEmpty(t, mutation.AdditionalMutations)
		})
	}
}

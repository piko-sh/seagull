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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
)

func viewBodyTokens(t *testing.T, body string) []token {
	t.Helper()

	tokens, err := tokenise(body)
	require.NoError(t, err)
	require.NotEmpty(t, tokens)
	return tokens[:len(tokens)-1]
}

func TestDropStatementsRecordWhetherTheySayIfExists(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		sql          string
		wantKind     ast.MutationKind
		wantIfExists bool
	}{
		{name: "DROP VIEW", sql: "DROP VIEW v", wantKind: ast.MutationDropView, wantIfExists: false},
		{name: "DROP VIEW IF EXISTS", sql: "DROP VIEW IF EXISTS v", wantKind: ast.MutationDropView, wantIfExists: true},
		{name: "DROP INDEX", sql: "DROP INDEX idx", wantKind: ast.MutationDropIndex, wantIfExists: false},
		{name: "DROP INDEX IF EXISTS", sql: "DROP INDEX IF EXISTS idx", wantKind: ast.MutationDropIndex, wantIfExists: true},
		{name: "DROP TRIGGER", sql: "DROP TRIGGER tr", wantKind: ast.MutationDropTrigger, wantIfExists: false},
		{name: "DROP TRIGGER IF EXISTS", sql: "DROP TRIGGER IF EXISTS tr", wantKind: ast.MutationDropTrigger,
			wantIfExists: true},
		{name: "DROP TABLE IF EXISTS", sql: "DROP TABLE IF EXISTS t", wantKind: ast.MutationDropTable, wantIfExists: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyDDLError(t, testCase.sql)

			require.NoError(t, err)
			require.NotNil(t, mutation)
			assert.Equal(t, testCase.wantKind, mutation.Kind)
			assert.Equal(t, testCase.wantIfExists, mutation.IfExists)
		})
	}
}

func TestInferViewColumnNamesReadsTheOuterProjection(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		body string
		want []string
	}{
		{name: "a bare select", body: "SELECT id, name FROM users", want: []string{"id", "name"}},
		{name: "a parenthesised select", body: "((SELECT id FROM users))", want: []string{"id"}},
		{name: "a select with no FROM", body: "SELECT 1 AS one, 2 two", want: []string{"one", "two"}},
		{name: "a common table expression", body: "WITH c AS (SELECT 1 AS x) SELECT x FROM c", want: []string{"x"}},
		{name: "a recursive common table expression with columns",
			body: "WITH RECURSIVE c(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM c) SELECT n AS step FROM c",
			want: []string{"step"}},
		{name: "two common table expressions",
			body: "WITH a AS (SELECT 1 AS x), b AS (SELECT 2 AS y) SELECT a.x, b.y FROM a, b", want: []string{"x", "y"}},
		{name: "a common table expression inside parentheses",
			body: "(WITH c AS (SELECT 1 AS x) SELECT x FROM c)", want: []string{"x"}},
		{name: "a subquery's FROM is not the outer FROM",
			body: "SELECT (SELECT max(id) FROM users) AS top, name FROM users", want: []string{"top", "name"}},
		{name: "an expression with no name", body: "SELECT id + 1, count(*) FROM users", want: []string{"", ""}},
		{name: "a keyword is no alias", body: "SELECT DISTINCT name FROM users", want: []string{"name"}},
		{name: "a reserved word ends no implicit alias", body: "SELECT id DESC FROM users", want: []string{"DESC"}},
		{name: "a quoted alias", body: `SELECT id AS "user id" FROM users`, want: []string{"user id"}},
		{name: "a body that is no select", body: "VALUES (1)", want: nil},
		{name: "a common table expression with no AS falls through to the next select",
			body: "WITH c (SELECT 1) SELECT 2 AS two", want: []string{"two"}},
		{name: "a common table expression with no body falls through to its select",
			body: "WITH c AS SELECT 1 AS one", want: []string{"one"}},
		{name: "a common table expression with no name falls through to the first select",
			body: "WITH (SELECT 1 AS one) SELECT 2", want: []string{"one"}},
		{name: "a common table expression left open", body: "WITH c AS (SELECT 1", want: nil},
		{name: "an empty body", body: "(", want: nil},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, inferViewColumnNames(viewBodyTokens(t, testCase.body), 0))
		})
	}
}

func TestSkipMatchingParensFindsTheTokenAfterTheGroup(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		body string
		want int
	}{
		{name: "an empty group", body: "() x", want: 2},
		{name: "a nested group", body: "((a, (b)), c) x", want: 11},
		{name: "a group never closed", body: "((a)", want: 4},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, skipMatchingParens(viewBodyTokens(t, testCase.body), 0))
		})
	}
}

func TestIsReservedProjectionKeywordIgnoresCase(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		word string
		want bool
	}{
		{word: "distinct", want: true},
		{word: "All", want: true},
		{word: "AS", want: true},
		{word: "desc", want: true},
		{word: "ASC", want: true},
		{word: "null", want: true},
		{word: "True", want: true},
		{word: "FALSE", want: true},
		{word: "name", want: false},
		{word: "", want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.word, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, isReservedProjectionKeyword(testCase.word))
		})
	}
}

func TestAViewPastTheDepthLimitTakesTheNamesItsProjectionGives(t *testing.T) {
	t.Parallel()

	nestedValue := strings.Repeat("(", 20) + "1" + strings.Repeat(")", 20)
	testCases := []struct {
		name string
		sql  string
		want []string
	}{
		{name: "a common table expression before the projection",
			sql:  "CREATE VIEW v AS WITH c AS (SELECT 1 AS x) SELECT x, " + nestedValue + " AS deep FROM c",
			want: []string{"x", "deep"}},
		{name: "a recursive common table expression before the projection",
			sql:  "CREATE VIEW v AS WITH RECURSIVE c(n) AS (SELECT 1) SELECT n step, " + nestedValue + " FROM c",
			want: []string{"step", ""}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(WithMaxParseDepth(8))
			mutation, err := engine.ApplyDDL(t.Context(), parseOne(t, engine, testCase.sql))

			require.NoError(t, err)
			require.NotNil(t, mutation)
			assert.Nil(t, mutation.ViewDefinition)
			require.Len(t, mutation.UnanalysedParts, 1)
			names := make([]string, 0, len(mutation.Columns))
			for _, column := range mutation.Columns {
				names = append(names, column.Name)
			}
			assert.Equal(t, testCase.want, names)
		})
	}
}

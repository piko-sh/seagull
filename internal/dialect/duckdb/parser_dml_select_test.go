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

package duckdb

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestATableFunctionWithOrdinalityAppendsTheOrdinalityColumn(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		statement  string
		ordinality bool
	}{
		{name: "WITH ORDINALITY", statement: "SELECT * FROM unnest([10, 20]) WITH ORDINALITY", ordinality: true},
		{name: "WITH ORDINALITY and an alias list", statement: "SELECT u.x, u.n FROM unnest([10, 20]) WITH ORDINALITY AS u(x, n)", ordinality: true},
		{name: "no ordinality", statement: "SELECT * FROM range(3) AS r"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, _ := analyseStatement(t, testCase.statement)

			require.Len(t, analysis.RawTableValuedFunctions, 1)
			ordinality := analysis.RawTableValuedFunctions[0].Ordinality
			if !testCase.ordinality {
				require.Nil(t, ordinality)
				return
			}
			require.NotNil(t, ordinality)
			require.Equal(t, "ordinality", ordinality.Name)
			require.Equal(t, sqlmodel.TypeCategoryInteger, ordinality.SQLType.Category)
			require.Equal(t, "int8", ordinality.SQLType.EngineName)
			require.False(t, ordinality.Nullable)
		})
	}
}

func TestATableWithAColumnAliasListBecomesARelationOfTheRenamedColumns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		sql       string
		alias     string
		columns   []string
		joinKind  ast.JoinKind
		fromTable string
	}{
		{name: "every column renamed", sql: "SELECT r.a, r.b FROM users AS r (a, b)", alias: "r", columns: []string{"a", "b"},
			joinKind: ast.JoinInner, fromTable: "users"},
		{name: "the leading column renamed without AS", sql: "SELECT * FROM main.users r (a)", alias: "r", columns: []string{"a"},
			joinKind: ast.JoinInner, fromTable: "users"},
		{name: "a joined table renamed", sql: "SELECT o.a FROM users u LEFT JOIN orders o (a) ON o.a = u.id", alias: "o",
			columns: []string{"a"}, joinKind: ast.JoinLeft, fromTable: "orders"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, buildTestCatalogue(), test.sql)

			require.Len(t, analysis.RawDerivedTables, 1)
			derived := analysis.RawDerivedTables[0]
			require.Equal(t, test.alias, derived.Alias)
			require.Equal(t, test.columns, derived.ColumnNames)
			require.Equal(t, test.joinKind, derived.JoinKind)
			require.NotNil(t, derived.InnerQuery)
			require.Len(t, derived.InnerQuery.FromTables, 1)
			require.Equal(t, test.fromTable, derived.InnerQuery.FromTables[0].Name)
			require.Contains(t, analysis.FromOrder, test.alias)
		})
	}
}

func TestAnAliasOnAParenthesisedJoinNamesItUnlessItsLastJoinIsUsing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sql     string
		derived bool
	}{
		{name: "a join on a condition", sql: "SELECT r.id FROM (users JOIN orders ON users.id = orders.user_id) AS r", derived: true},
		{name: "a join on a condition, aliased without AS", sql: "SELECT r.id FROM (users JOIN orders ON users.id = orders.user_id) r", derived: true},
		{name: "a natural join", sql: "SELECT r.id FROM (users NATURAL JOIN orders) r", derived: true},
		{name: "a cross join", sql: "SELECT r.id FROM (users CROSS JOIN orders) AS r", derived: true},
		{name: "a join using a column", sql: "SELECT users.id FROM (users JOIN orders USING (id)) AS r", derived: false},
		{name: "a join on a condition after one using a column", sql: "SELECT r.id FROM (users JOIN orders USING (id) JOIN users u ON u.id = users.id) AS r", derived: true},
		{name: "no alias", sql: "SELECT users.id FROM (users JOIN orders ON users.id = orders.user_id)", derived: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, buildTestCatalogue(), test.sql)
			if !test.derived {
				require.Empty(t, analysis.RawDerivedTables)
				require.Len(t, analysis.FromTables, 1)
				require.NotEmpty(t, analysis.JoinClauses)
				return
			}
			require.Empty(t, analysis.FromTables)
			require.Empty(t, analysis.JoinClauses)
			require.Len(t, analysis.RawDerivedTables, 1)
			derived := analysis.RawDerivedTables[0]
			require.Equal(t, "r", derived.Alias)
			require.NotNil(t, derived.InnerQuery)
			require.Len(t, derived.InnerQuery.FromTables, 1)
			require.NotEmpty(t, derived.InnerQuery.JoinClauses)
			require.Len(t, derived.InnerQuery.OutputColumns, 1)
			require.True(t, derived.InnerQuery.OutputColumns[0].IsStar)
		})
	}
}

func TestDuckDBIgnoringTheAliasOfAJoinUsingAColumnIsWarnedOf(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, buildTestCatalogue(), "SELECT users.id FROM (users JOIN orders USING (id)) AS r")
	require.Len(t, analysis.Warnings, 1)
	require.Contains(t, analysis.Warnings[0].Message, "DuckDB ignores the alias r")

	named := analyseQuery(t, buildTestCatalogue(), "SELECT r.id FROM (users JOIN orders ON users.id = orders.user_id) AS r")
	require.Empty(t, named.Warnings)
}

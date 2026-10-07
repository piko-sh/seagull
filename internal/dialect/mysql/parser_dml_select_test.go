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
	"seagull.sh/seagull/internal/dialect/dialectkit"
)

func nestedJSONTable(depth int) string {
	return "SELECT j.v FROM JSON_TABLE(?, '$' COLUMNS (" +
		strings.Repeat("NESTED PATH '$.n' COLUMNS (", depth) + "v INT PATH '$.v'" + strings.Repeat(")", depth) +
		")) AS j"
}

func TestJSONTableNestedPathsCountAgainstTheParseDepth(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		options   []Option
		depth     int
		wantDepth bool
	}{
		{name: "nesting within the limit flattens the columns", options: []Option{WithMaxParseDepth(8)}, depth: 3,
			wantDepth: false},
		{name: "nesting past a lowered limit is refused", options: []Option{WithMaxParseDepth(8)}, depth: 64,
			wantDepth: true},
		{name: "nesting far past the default limit is refused before the stack runs out",
			options: []Option{WithMaxTokensPerStatement(dialectkit.MaxTokensPerStatementCeiling)}, depth: 50_000,
			wantDepth: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, err := analyseWith(t, New(testCase.options...), nestedJSONTable(testCase.depth))

			if testCase.wantDepth {
				require.ErrorIs(t, err, dialectkit.ErrParseDepth)
				assert.Nil(t, analysis)
				return
			}
			require.NoError(t, err)
			require.Len(t, analysis.RawTableValuedFunctions, 1)
			require.Len(t, analysis.RawTableValuedFunctions[0].ColumnDefinitions, 1)
			assert.Equal(t, "v", analysis.RawTableValuedFunctions[0].ColumnDefinitions[0].Name)
		})
	}
}

func TestATableFunctionsColumnListNamesEachColumnAndItsType(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantColumns []ast.TVFColumnDefinition
	}{
		{
			name: "single and multi-word types",
			sql:  "SELECT * FROM f(?) AS x (a INT, b DOUBLE PRECISION, c INT UNSIGNED, d CHARACTER VARYING(5))",
			wantColumns: []ast.TVFColumnDefinition{
				{Name: "a", TypeName: "INT"}, {Name: "b", TypeName: "DOUBLE PRECISION"},
				{Name: "c", TypeName: "INT UNSIGNED"}, {Name: "d", TypeName: "CHARACTER VARYING(5)"},
			},
		},
		{
			name:        "a type qualified by its schema",
			sql:         "SELECT * FROM f(?) AS x (a app.money)",
			wantColumns: []ast.TVFColumnDefinition{{Name: "a", TypeName: "app.money"}},
		},
		{
			name:        "a type with a precision and a scale",
			sql:         "SELECT * FROM f(?) AS x (a DECIMAL(10, 2))",
			wantColumns: []ast.TVFColumnDefinition{{Name: "a", TypeName: "DECIMAL(10,2)"}},
		},
		{
			name:        "a type with a length and a type with a fractional-second precision",
			sql:         "SELECT * FROM f(?) AS x (a VARCHAR(20), b DATETIME(6))",
			wantColumns: []ast.TVFColumnDefinition{{Name: "a", TypeName: "VARCHAR(20)"}, {Name: "b", TypeName: "DATETIME(6)"}},
		},
		{
			name:        "a type whose list holds values rather than numbers",
			sql:         "SELECT * FROM f(?) AS x (a ENUM('on', 'off'), b INT)",
			wantColumns: []ast.TVFColumnDefinition{{Name: "a", TypeName: "ENUM"}, {Name: "b", TypeName: "INT"}},
		},
		{
			name:        "a type whose precision is written as a fraction",
			sql:         "SELECT * FROM f(?) AS x (a DECIMAL(10.5, 2))",
			wantColumns: []ast.TVFColumnDefinition{{Name: "a", TypeName: "DECIMAL(10,2)"}},
		},
		{
			name:        "a type with more modifiers than any type takes",
			sql:         "SELECT * FROM f(?) AS x (a DECIMAL(10, 2, 1))",
			wantColumns: []ast.TVFColumnDefinition{{Name: "a", TypeName: "DECIMAL"}},
		},
		{
			name:        "a type with an empty modifier list",
			sql:         "SELECT * FROM f(?) AS x (a DECIMAL())",
			wantColumns: []ast.TVFColumnDefinition{{Name: "a", TypeName: "DECIMAL"}},
		},
		{
			name:        "names with no types",
			sql:         "SELECT * FROM f(?) AS x (a, b)",
			wantColumns: []ast.TVFColumnDefinition{{Name: "a", TypeName: ""}, {Name: "b", TypeName: ""}},
		},
		{
			name:        "an empty list",
			sql:         "SELECT * FROM f(?) AS x ()",
			wantColumns: nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, testCase.sql)

			require.Len(t, analysis.RawTableValuedFunctions, 1)
			assert.Equal(t, "x", analysis.RawTableValuedFunctions[0].Alias)
			assert.Equal(t, testCase.wantColumns, analysis.RawTableValuedFunctions[0].ColumnDefinitions)
		})
	}
}

func TestATableFunctionsColumnListMustBeWellFormed(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		wantToken string
	}{
		{name: "a list never closed", sql: "SELECT * FROM f(?) AS x (a INT", wantToken: "("},
		{name: "a type's modifiers never closed", sql: "SELECT * FROM f(?) AS x (a DECIMAL(10, 2)", wantToken: "("},
		{name: "a list holding no name", sql: "SELECT * FROM f(?) AS x (1)", wantToken: "1"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, err := analyseWith(t, New(), testCase.sql)

			assert.Nil(t, analysis)
			requireParseErrorAt(t, err, testCase.wantToken)
		})
	}
}

func TestAParenthesisedJoinReadsACommaListAsCrossJoins(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		wantFrom  []string
		wantJoins []string
		wantViews int
	}{
		{name: "two tables", sql: "SELECT * FROM (a, b)", wantFrom: []string{"a"}, wantJoins: []string{"CROSS b"}},
		{name: "a table and a join", sql: "SELECT * FROM (a, b JOIN c ON b.x = c.x)", wantFrom: []string{"a"},
			wantJoins: []string{"CROSS b", "INNER c"}},
		{name: "three tables", sql: "SELECT * FROM (a, b, c)", wantFrom: []string{"a"}, wantJoins: []string{"CROSS b", "CROSS c"}},
		{name: "a lateral derived table", sql: "SELECT * FROM (a, LATERAL (SELECT 1 AS one) AS d)", wantFrom: []string{"a"},
			wantJoins: nil, wantViews: 1},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, testCase.sql)

			var from, joins []string
			for _, table := range analysis.FromTables {
				from = append(from, table.Name)
			}
			for _, join := range analysis.JoinClauses {
				joins = append(joins, join.Kind.String()+" "+join.Table.Name)
			}
			assert.Equal(t, testCase.wantFrom, from)
			assert.Equal(t, testCase.wantJoins, joins)
			assert.Len(t, analysis.RawDerivedTables, testCase.wantViews)
		})
	}
}

func TestAParenthesisedJoinRefusesAnItemThatIsNoTable(t *testing.T) {
	t.Parallel()

	analysis, err := analyseWith(t, New(), "SELECT * FROM (a, )")

	assert.Nil(t, analysis)
	requireParseErrorAt(t, err, ")")
}

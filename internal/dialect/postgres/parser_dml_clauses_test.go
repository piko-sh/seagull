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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func TestMultiColumnSetFromASubqueryTerminates(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		sql        string
		parameters int
		subqueries int
	}{
		{name: "subquery with FROM and a parameter", sql: "UPDATE users SET (name, email) = (SELECT name, email FROM users WHERE id = $2) WHERE id = $1", parameters: 2, subqueries: 1},
		{name: "subquery with FROM and LIMIT", sql: "UPDATE users SET (name, email) = (SELECT name, email FROM users LIMIT 1)", parameters: 0, subqueries: 1},
		{name: "one column from a subquery", sql: "UPDATE users SET (name) = (SELECT name FROM users LIMIT 1)", parameters: 0, subqueries: 1},
		{name: "subquery without FROM", sql: "UPDATE users SET (name, email) = (SELECT 'x', 'y') WHERE id = $1", parameters: 1, subqueries: 1},
		{name: "a single column from a subquery", sql: "UPDATE users SET name = (SELECT name FROM orders WHERE orders.id = $2) WHERE id = $1", parameters: 2, subqueries: 1},
		{name: "a list of values", sql: "UPDATE users SET (name, email) = ($2, $3) WHERE id = $1", parameters: 3},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)
			var analysis *ast.RawQueryAnalysis
			scaling.Within(t, 10*time.Second, func(ctx context.Context) {
				var analyseError error
				analysis, analyseError = engine.AnalyseQuery(ctx, newPostgresCatalogue(), statements[0])
				require.NoError(t, analyseError)
			})

			numbers := map[int]struct{}{}
			for _, reference := range analysis.ParameterReferences {
				numbers[reference.Number] = struct{}{}
			}
			require.Len(t, numbers, testCase.parameters)
			require.Len(t, analysis.PredicateSubqueries, testCase.subqueries)
		})
	}
}

func arrayColumnCatalogue() *sqlmodel.Catalogue {
	catalogue := newPostgresCatalogue()
	tags := sqlmodel.NewColumn("tags", sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}, true)
	tags.ArrayDimensions = 1
	tags.IsArray = true
	catalogue.Schemas["public"].Tables["users"].Columns = append(catalogue.Schemas["public"].Tables["users"].Columns, tags)
	return catalogue
}

func TestAnAssignmentToArrayElementsTypesItsParameters(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		sql           string
		wantCastTypes []string
	}{
		{name: "one element", sql: "UPDATE users SET tags[$1] = $2 WHERE id = $3", wantCastTypes: []string{"int4", "text", ""}},
		{name: "a slice assigns an array", sql: "UPDATE users SET tags[$1:$2] = $3 WHERE id = $4", wantCastTypes: []string{"int4", "int4", "", ""}},
		{name: "a parameter cast in the value keeps its cast", sql: "UPDATE users SET tags[1] = $1::varchar WHERE id = $2", wantCastTypes: []string{"varchar", ""}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, arrayColumnCatalogue(), testCase.sql)

			castTypes := make([]string, len(analysis.ParameterReferences))
			for index, reference := range analysis.ParameterReferences {
				if reference.CastType != nil {
					castTypes[index] = reference.CastType.EngineName
				}
			}
			require.Equal(t, testCase.wantCastTypes, castTypes)
		})
	}
}

func TestABracketedValueInAValuesRowLeavesLaterParametersTheirColumns(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantColumns []string
	}{
		{name: "an array constructor", sql: "INSERT INTO users (id, tags, name, email) VALUES ($1, ARRAY['a', 'b'], $2, $3)",
			wantColumns: []string{"id", "name", "email"}},
		{name: "a concatenation of constructors", sql: "INSERT INTO users (id, tags, name, email) VALUES ($1, ARRAY['a', 'b'] || ARRAY['c'], $2, $3)",
			wantColumns: []string{"id", "name", "email"}},
		{name: "a nested array constructor", sql: "INSERT INTO users (id, tags, name, email) VALUES ($1, ARRAY[ARRAY['a', 'b']], $2, $3)",
			wantColumns: []string{"id", "name", "email"}},
		{name: "parameters inside the constructor", sql: "INSERT INTO users (id, tags, name, email) VALUES ($1, ARRAY[$2, 'b'], $3, $4)",
			wantColumns: []string{"id", "tags", "name", "email"}},
		{name: "a second row", sql: "INSERT INTO users (id, tags, name) VALUES ($1, ARRAY['a', 'b'], $2), ($3, ARRAY['c', 'd'], $4)",
			wantColumns: []string{"id", "name", "id", "name"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, arrayColumnCatalogue(), testCase.sql)

			columns := make([]string, 0, len(analysis.ParameterReferences))
			for _, reference := range analysis.ParameterReferences {
				require.NotNil(t, reference.ColumnReference)
				columns = append(columns, reference.ColumnReference.ColumnName)
			}
			require.Equal(t, testCase.wantColumns, columns)
		})
	}
}

func TestABracketedValueInAnAssignmentLeavesLaterParametersTheirColumns(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantColumns []string
	}{
		{name: "a row assignment", sql: "UPDATE users SET (tags, name, email) = (ARRAY['a', 'b'], $1, $2) WHERE id = $3",
			wantColumns: []string{"name", "email", "id"}},
		{name: "parameters inside a row assignment's constructor", sql: "UPDATE users SET (tags, name) = (ARRAY[$1, 'b'], $2) WHERE id = $3",
			wantColumns: []string{"", "name", "id"}},
		{name: "parameters inside an assignment's constructor", sql: "UPDATE users SET tags = ARRAY[$1, $2], name = $3 WHERE id = $4",
			wantColumns: []string{"tags", "tags", "name", "id"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, arrayColumnCatalogue(), testCase.sql)

			columns := make([]string, 0, len(analysis.ParameterReferences))
			for _, reference := range analysis.ParameterReferences {
				column := ""
				if reference.ColumnReference != nil {
					column = reference.ColumnReference.ColumnName
				}
				columns = append(columns, column)
			}
			require.Equal(t, testCase.wantColumns, columns)
		})
	}
}

func TestASubqueryInAValuesRowTypesItsOwnParameters(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
	}{
		{name: "the subquery is the value", sql: "INSERT INTO posts (id, user_id, title) VALUES ($1, (SELECT id FROM users WHERE email = $2), $3)"},
		{name: "the subquery is inside the value", sql: "INSERT INTO posts (id, user_id, title) VALUES ($1, (COALESCE((SELECT id FROM users WHERE email = $2), 0)), $3)"},
		{name: "the subquery is an argument of a call", sql: "INSERT INTO posts (id, user_id, title) VALUES ($1, COALESCE((SELECT id FROM users WHERE email = $2), 0), $3)"},
		{name: "the subquery is a CASE result", sql: "INSERT INTO posts (id, user_id, title) VALUES ($1, CASE WHEN $3 = '' THEN (SELECT id FROM users WHERE email = $2) END, $3)"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newPostgresCatalogue(), testCase.sql)

			require.Len(t, analysis.PredicateSubqueries, 1)
			var compared *ast.RawParameterReference
			for index := range analysis.ParameterReferences {
				if analysis.ParameterReferences[index].Number == 2 {
					compared = &analysis.ParameterReferences[index]
				}
			}
			require.NotNil(t, compared)
			require.Equal(t, sqlmodel.ParameterContextComparison, compared.Context)
			require.Equal(t, "email", compared.ColumnReference.ColumnName)
		})
	}
}

func TestALimitOrOffsetValueMayBeAnExpressionOverItsParameters(t *testing.T) {
	t.Parallel()

	type parameter struct {
		number  int
		context sqlmodel.ParameterContext
		cast    string
	}
	limit, offset := sqlmodel.ParameterContextLimit, sqlmodel.ParameterContextOffset
	testCases := []struct {
		name string
		sql  string
		want []parameter
	}{
		{name: "a cast limit and offset", sql: "SELECT id FROM users LIMIT $1::int OFFSET $2::bigint",
			want: []parameter{{number: 1, context: limit, cast: "int4"}, {number: 2, context: offset, cast: "int8"}}},
		{name: "a named cast", sql: "SELECT id FROM users LIMIT :size::int",
			want: []parameter{{number: 1, context: limit, cast: "int4"}}},
		{name: "an offset product", sql: "SELECT id FROM users ORDER BY id LIMIT :size OFFSET :size * :page",
			want: []parameter{{number: 1, context: limit}, {number: 1, context: offset}, {number: 2, context: offset}}},
		{name: "a parenthesised offset", sql: "SELECT id FROM users LIMIT $1 OFFSET ($2::int - 1) * $1",
			want: []parameter{{number: 1, context: limit}, {number: 2, context: offset, cast: "int4"}, {number: 1, context: offset}}},
		{name: "a limit sum before a locking clause", sql: "SELECT id FROM users LIMIT $1 + 1 FOR UPDATE",
			want: []parameter{{number: 1, context: limit}}},
		{name: "an offset before a fetch", sql: "SELECT id FROM users OFFSET $1 * 2 ROWS FETCH NEXT $2 ROWS ONLY",
			want: []parameter{{number: 1, context: offset}, {number: 2, context: limit}}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newPostgresCatalogue(), testCase.sql)

			var got []parameter
			for _, reference := range analysis.ParameterReferences {
				cast := ""
				if reference.CastType != nil {
					cast = reference.CastType.EngineName
				}
				got = append(got, parameter{number: reference.Number, context: reference.Context, cast: cast})
			}
			require.Equal(t, testCase.want, got)
		})
	}
}

func TestAParenthesisThatOpensNoQueryIsNoInsertSource(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		wantToken string
	}{
		{name: "a parenthesised list of values after the column list", sql: "INSERT INTO users (id, name) (1, 'a')", wantToken: "("},
		{name: "a parenthesised value before RETURNING", sql: "INSERT INTO users (id) (1) RETURNING id", wantToken: "("},
		{name: "a parenthesised value before ON CONFLICT", sql: "INSERT INTO users (id) ($1) ON CONFLICT DO NOTHING", wantToken: "("},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := analyseQueryError(t, testCase.sql)

			parseError, isParseError := errors.AsType[*dialectkit.ParseError](err)
			require.True(t, isParseError, "got %v", err)
			assert.Equal(t, testCase.wantToken, parseError.Token)
			assert.Equal(t, "expected a query, VALUES or DEFAULT VALUES giving the rows to insert", parseError.Message)
		})
	}
}

func TestGroupingSetsNestedToTheDepthLimitFailTheStatement(t *testing.T) {
	t.Parallel()

	nested := func(depth int) string {
		return "SELECT a, b FROM t GROUP BY " + strings.Repeat("GROUPING SETS (", depth) + "a, b" + strings.Repeat(")", depth)
	}
	testCases := []struct {
		name      string
		sql       string
		wantDepth bool
	}{
		{name: "as deep as the limit allows", sql: nested(5), wantDepth: false},
		{name: "rollups within the limit", sql: "SELECT a, b FROM t GROUP BY ROLLUP (a, CUBE (b, a))", wantDepth: false},
		{name: "one level past the limit", sql: nested(6), wantDepth: true},
		{name: "far past the limit", sql: nested(40), wantDepth: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := analyseWithContext(t.Context(), New(WithMaxParseDepth(4)), testCase.sql)

			if !testCase.wantDepth {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, dialectkit.ErrParseDepth)
		})
	}
}

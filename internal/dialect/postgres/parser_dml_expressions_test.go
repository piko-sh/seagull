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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func TestQuantifiedComparisonsInTheSelectListRegisterTheirParameters(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		wantRight      string
		wantQuantifier string
		wantArray      bool
	}{
		{name: "an array parameter under ANY", sql: "SELECT 1 = ANY($1::int[]) AS ok", wantRight: "cast", wantQuantifier: "ANY", wantArray: true},
		{name: "an array parameter under ALL", sql: "SELECT 'a' <> ALL($1::text[]) AS ok", wantRight: "cast", wantQuantifier: "ALL", wantArray: true},
		{name: "a subquery under SOME", sql: "SELECT id = some(SELECT user_id FROM posts WHERE title = $1) AS ok FROM users",
			wantRight: "scalar_subquery", wantQuantifier: "SOME", wantArray: false},
		{name: "an uncast parameter under ANY", sql: "SELECT 1 = ANY($1) AS ok", wantRight: "parameter", wantQuantifier: "ANY", wantArray: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newPostgresCatalogue(), testCase.sql)

			require.Len(t, analysis.OutputColumns, 1)
			comparison, isComparison := analysis.OutputColumns[0].Expression.(*ast.ComparisonExpression)
			require.True(t, isComparison)
			require.Equal(t, testCase.wantRight, expressionKindName(comparison.Right))
			require.Equal(t, testCase.wantQuantifier, comparison.Quantifier)
			require.NotEmpty(t, analysis.ParameterReferences)
			if testCase.wantArray {
				require.NotNil(t, analysis.ParameterReferences[0].CastType)
				require.Equal(t, sqlmodel.TypeCategoryArray, analysis.ParameterReferences[0].CastType.Category)
			}
		})
	}
}

func expressionKindName(expression ast.Expression) string {
	switch expression.(type) {
	case *ast.ScalarSubqueryExpression:
		return "scalar_subquery"
	case *ast.UnknownExpression:
		return "unknown"
	case *ast.RowValueExpression:
		return "row_value"
	case *ast.CastExpression:
		return "cast"
	case *ast.ParameterExpression:
		return "parameter"
	default:
		return "other"
	}
}

func TestRowConstructorsAreRowValues(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		sql          string
		wantElements int
	}{
		{name: "a parenthesised list", sql: "SELECT id FROM users WHERE (id, name) < ($1, $2)", wantElements: 2},
		{name: "the ROW keyword", sql: "SELECT id FROM users WHERE ROW(id, name, email) = ROW($1, $2, $3)", wantElements: 3},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newPostgresCatalogue(), testCase.sql)

			require.Len(t, analysis.ClauseExpressions, 1)
			comparison, isComparison := analysis.ClauseExpressions[0].Expression.(*ast.ComparisonExpression)
			require.True(t, isComparison)
			left, leftIsRow := comparison.Left.(*ast.RowValueExpression)
			right, rightIsRow := comparison.Right.(*ast.RowValueExpression)
			require.True(t, leftIsRow && rightIsRow)
			require.Len(t, left.Elements, testCase.wantElements)
			require.Len(t, right.Elements, testCase.wantElements)
		})
	}
}

func TestACastTypesOnlyTheParametersOfItsOperand(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, newPostgresCatalogue(),
		"SELECT name = $1 AND (name)::int - $2 > 0 AS matched FROM users")

	require.Len(t, analysis.ParameterReferences, 2)
	require.Nil(t, analysis.ParameterReferences[0].CastType)
	require.Equal(t, sqlmodel.ParameterContextUnknown, analysis.ParameterReferences[0].Context)
}

func TestCompositeFieldSelectionsAreFieldAccesses(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		sql        string
		wantName   string
		wantFields []string
	}{
		{name: "a field of a column", sql: "SELECT (name).first FROM users", wantName: "first", wantFields: []string{"first"}},
		{name: "a field of a qualified column", sql: "SELECT (users.name).first FROM users", wantName: "first", wantFields: []string{"first"}},
		{name: "a field of a field", sql: "SELECT ((name).inner).first FROM users", wantName: "first", wantFields: []string{"first", "inner"}},
		{name: "two fields in a row", sql: "SELECT (name).inner.first FROM users", wantName: "first", wantFields: []string{"first", "inner"}},
		{name: "an aliased field", sql: "SELECT (name).first AS given FROM users", wantName: "given", wantFields: []string{"first"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newPostgresCatalogue(), testCase.sql)

			require.Len(t, analysis.OutputColumns, 1)
			require.Equal(t, testCase.wantName, analysis.OutputColumns[0].Name)
			var fields []string
			expression := analysis.OutputColumns[0].Expression
			for {
				access, isAccess := expression.(*ast.StructFieldAccessExpression)
				if !isAccess {
					break
				}
				fields = append(fields, access.FieldName)
				expression = access.Struct
			}
			require.Equal(t, testCase.wantFields, fields)
		})
	}
}

func TestEverySelectedFieldOfACompositeIsNoOneValue(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, newPostgresCatalogue(), "SELECT (name).* FROM users")

	require.Len(t, analysis.OutputColumns, 1)
	require.Equal(t, "unknown", expressionKindName(analysis.OutputColumns[0].Expression))
}

func TestAParameterComparedWithACompositeFieldNamesNoColumn(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, newPostgresCatalogue(), "SELECT id FROM users u WHERE (u.name).first = $1")

	require.Len(t, analysis.ParameterReferences, 1)
	require.Nil(t, analysis.ParameterReferences[0].ColumnReference)
}

func TestASubscriptWrittenWithOneStringKeepsItAsItsKey(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		wantKeys []string
	}{
		{name: "a string key", sql: "SELECT name['colour'] AS colour FROM users", wantKeys: []string{"colour"}},
		{name: "a key after a key", sql: "SELECT name['shade']['colour'] AS colour FROM users", wantKeys: []string{"colour", "shade"}},
		{name: "a position", sql: "SELECT name[1] AS first FROM users", wantKeys: []string{""}},
		{name: "a string expression", sql: "SELECT name['col' || 'our'] AS colour FROM users", wantKeys: []string{""}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newPostgresCatalogue(), testCase.sql)

			require.Len(t, analysis.OutputColumns, 1)
			var keys []string
			expression := analysis.OutputColumns[0].Expression
			for {
				subscript, isSubscript := expression.(*ast.ArraySubscriptExpression)
				if !isSubscript {
					break
				}
				keys = append(keys, subscript.Key)
				expression = subscript.Array
			}
			require.Equal(t, testCase.wantKeys, keys)
		})
	}
}

func TestRowNamesAColumnOrATableUnlessAParenthesisFollows(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		sql     string
		columns []string
	}{
		{name: "a column named row", sql: "SELECT id, row FROM posts WHERE row = $1", columns: []string{"id", "row"}},
		{name: "a table aliased row", sql: "SELECT row.id FROM users AS row", columns: []string{"id"}},
		{name: "a derived table aliased row", sql: "SELECT DISTINCT ON (row.id) row.id FROM (SELECT id FROM users) AS row", columns: []string{"id"}},
		{name: "a row constructor", sql: "SELECT ROW(1, 'a') AS pair", columns: []string{"pair"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyseQuery(t, newPostgresCatalogue(), testCase.sql)

			require.Equal(t, testCase.columns, outputNames(analysis))
		})
	}
}

func TestAParameterAfterAnOperatorOfOneTypeInAnExpressionTakesTheColumnBeforeIt(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		expected []string
	}{
		{name: "an addition in the select list", sql: "SELECT id, n + $1 AS bumped FROM items", expected: []string{"1 comparison .n -"}},
		{name: "a subtraction of a qualified column", sql: "SELECT i.n - $1 AS lowered FROM items i", expected: []string{"1 comparison i.n -"}},
		{name: "a multiplication", sql: "SELECT price * $1 AS scaled FROM items", expected: []string{"1 comparison .price -"}},
		{name: "a division in RETURNING", sql: "DELETE FROM items WHERE id = 1 RETURNING n / $1 AS share", expected: []string{"1 comparison .n -"}},
		{name: "a distance between points", sql: "SELECT location <-> $1 AS distance FROM places", expected: []string{"1 comparison .location -"}},
		{name: "a bitwise and", sql: "SELECT mask & $1 AS masked FROM flags", expected: []string{"1 comparison .mask -"}},
		{name: "a cast after the parameter wins", sql: "SELECT n + $1::numeric AS bumped FROM items", expected: []string{"1 cast - numeric"}},
		{name: "a parameter before the operator takes no partner", sql: "SELECT $1 + n AS bumped FROM items", expected: []string{"1 unknown - -"}},
		{name: "a concatenation takes no partner", sql: "SELECT label || $1 AS labelled FROM items", expected: []string{"1 unknown - -"}},
		{name: "a JSON key takes no partner", sql: "SELECT doc -> $1 AS field FROM docs", expected: []string{"1 unknown - -"}},
		{name: "a comparison is left to the analysis", sql: "SELECT name = $1 AS matched FROM users", expected: []string{"1 unknown - -"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis, _ := analyseStatement(t, testCase.sql)

			require.Equal(t, testCase.expected, describeDeclaredParameters(analysis.ParameterReferences))
		})
	}
}

func TestExpressionRecursionOutsideParenthesesIsHeldToTheDepthLimit(t *testing.T) {
	t.Parallel()

	const (
		chainLength   = 99_000
		positionDepth = 16_000
	)
	testCases := []struct {
		name string
		sql  string
	}{
		{name: "a chain of unary minus signs", sql: "SELECT " + strings.Repeat("- ", chainLength) + "1"},
		{name: "a chain of prefix operators", sql: "SELECT " + strings.Repeat("@ ", chainLength) + "1"},
		{name: "nested POSITION calls", sql: "SELECT " + strings.Repeat("position(", positionDepth) + "'a'" +
			strings.Repeat(" IN 'b')", positionDepth)},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)

			scaling.Within(t, 200*time.Millisecond, func(ctx context.Context) {
				_, err = engine.AnalyseQuery(ctx, nil, statements[0])
			})
			require.ErrorIs(t, err, dialectkit.ErrParseDepth)
		})
	}
}

func TestPrefixOperatorsWithinTheDepthLimitStillAnalyse(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		sql     string
		wantErr bool
	}{
		{name: "a short chain of minus signs", sql: "SELECT - - - 1 AS x", wantErr: false},
		{name: "a short chain of POSITION calls", sql: "SELECT position(position('a' IN 'b') IN 'c') AS x", wantErr: false},
		{name: "a chain longer than the limit", sql: "SELECT " + strings.Repeat("- ", 16) + "1 AS x", wantErr: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(WithMaxParseDepth(8))
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)

			_, err = engine.AnalyseQuery(t.Context(), nil, statements[0])

			if testCase.wantErr {
				require.ErrorIs(t, err, dialectkit.ErrParseDepth)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestExpressionNestingPastTheDepthLimitIsReported(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		nesting int
		wantErr bool
	}{
		{name: "nesting within the limit analyses", nesting: 6, wantErr: false},
		{name: "nesting past the limit is an error", nesting: 64, wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(WithMaxParseDepth(8))
			sql := "SELECT " + strings.Repeat("(", testCase.nesting) + "1" + strings.Repeat(")", testCase.nesting) + " AS x"
			statements, err := engine.ParseStatements(t.Context(), sql)
			require.NoError(t, err)

			_, err = engine.AnalyseQuery(t.Context(), nil, statements[0])

			if !testCase.wantErr {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, dialectkit.ErrParseDepth)
		})
	}
}

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

package clickhouse

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
)

func TestWithAliasesAreReplacedInEveryKindOfExpression(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		sql           string
		wantUnknown   int
		wantQualified int
	}{
		{name: "a comparison", sql: "WITH 5 AS bound SELECT id FROM items WHERE id > bound", wantUnknown: 1},
		{name: "a lambda body", sql: "WITH 5 AS bound SELECT id FROM items WHERE arrayExists(x -> x > bound, tags)", wantUnknown: 1},
		{name: "an IN list", sql: "WITH 5 AS bound SELECT id FROM items WHERE id IN (bound, 6)", wantUnknown: 1},
		{name: "a BETWEEN bound", sql: "WITH 5 AS bound SELECT id FROM items WHERE id BETWEEN bound AND 10", wantUnknown: 1},
		{name: "a subscript", sql: "WITH 1 AS first SELECT id FROM items WHERE tags[first] = 'x'", wantUnknown: 1},
		{name: "a CASE branch", sql: "WITH 5 AS bound SELECT id FROM items WHERE CASE WHEN id > bound THEN 1 ELSE bound END = 1", wantUnknown: 2},
		{name: "a logical operand", sql: "WITH 5 AS bound SELECT id FROM items WHERE id > bound OR NOT bound", wantUnknown: 2},
		{name: "a qualified column of the same name", sql: "WITH 5 AS bound SELECT id FROM items AS i WHERE i.bound > 1", wantQualified: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyse(t, testCase.sql)
			unknown, qualified := 0, 0
			for _, clause := range analysis.ClauseExpressions {
				for _, node := range everyPosition(clause.Expression) {
					switch typed := node.(type) {
					case *ast.UnknownExpression:
						unknown++
					case *ast.ColumnRefExpression:
						require.NotContains(t, []string{"bound", "first"}, typed.ColumnName+typed.TableAlias, "an alias is a value, not a column")
						if typed.TableAlias != "" {
							qualified++
						}
					}
				}
			}
			assert.Equal(t, testCase.wantUnknown, unknown)
			assert.Equal(t, testCase.wantQualified, qualified)
		})
	}
}

func TestLambdaParametersShadowWithAliasesInTheirBodies(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantColumns []string
		wantUnknown int
	}{
		{
			name:        "a parameter named like an alias",
			sql:         "WITH 5 AS x SELECT id FROM items WHERE arrayExists(x -> x > 1, tags)",
			wantColumns: []string{"x", "tags"},
		},
		{
			name:        "an alias no parameter shadows",
			sql:         "WITH 5 AS bound SELECT id FROM items WHERE arrayExists(x -> x > bound, tags)",
			wantColumns: []string{"x", "tags"},
			wantUnknown: 1,
		},
		{
			name:        "the alias outside the lambda",
			sql:         "WITH 5 AS x SELECT id FROM items WHERE arrayExists(x -> x > 1, tags) AND id > x",
			wantColumns: []string{"x", "tags", "id"},
			wantUnknown: 1,
		},
		{
			name:        "a nested lambda shadows only inside its own body",
			sql:         "WITH 5 AS y SELECT id FROM items WHERE arrayExists(x -> arrayExists(y -> y > x, tags) AND x > y, tags)",
			wantColumns: []string{"y", "x", "tags", "x", "tags"},
			wantUnknown: 1,
		},
		{
			name:        "an accumulator bound to the alias",
			sql:         "WITH 0 AS acc SELECT id FROM items WHERE arrayFold((acc, x) -> acc + x, tags, acc) > 0",
			wantColumns: []string{"acc", "x", "tags"},
			wantUnknown: 2,
		},
		{
			name:        "one of several parameters",
			sql:         "WITH 5 AS total SELECT id FROM items WHERE arrayExists((total, x) -> total > x, tags, tags)",
			wantColumns: []string{"total", "x", "tags", "tags"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyse(t, testCase.sql)
			var columns []string
			unknown := 0
			for _, clause := range analysis.ClauseExpressions {
				for _, node := range everyPosition(clause.Expression) {
					switch typed := node.(type) {
					case *ast.UnknownExpression:
						unknown++
					case *ast.ColumnRefExpression:
						columns = append(columns, typed.ColumnName)
					}
				}
			}
			assert.Equal(t, testCase.wantColumns, columns)
			assert.Equal(t, testCase.wantUnknown, unknown)
		})
	}
}

func TestWithAliasesAreReplacedInsideNestedQueries(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "WITH 5 AS bound SELECT id FROM items WHERE id > (SELECT max(id) FROM tags WHERE n > bound)")
	require.Len(t, analysis.ClauseExpressions, 1)
	comparison, isComparison := analysis.ClauseExpressions[0].Expression.(*ast.ComparisonExpression)
	require.True(t, isComparison)
	inner, isSubquery := ast.InnerQuery(comparison.Right)
	require.True(t, isSubquery)
	require.Len(t, inner.ClauseExpressions, 1)
	inside, isInside := inner.ClauseExpressions[0].Expression.(*ast.ComparisonExpression)
	require.True(t, isInside)
	assert.IsType(t, &ast.UnknownExpression{}, inside.Right)
}

func TestReplacingAliasReferencesLeavesTheOriginalExpressionUnchanged(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		build    func() ast.Expression
		name     string
		aliases  []string
		wantSame bool
	}{
		{
			name: "an expression that refers to an alias",
			build: func() ast.Expression {
				return newFunctionCall("arrayMap", &ast.LambdaExpression{Parameters: []string{"x"},
					Body: &ast.BinaryOpExpression{Left: column("x"), Operator: "+", Right: column("bound")}}, column("tags"))
			},
			aliases: []string{"bound"},
		},
		{
			name: "an expression that refers to no alias",
			build: func() ast.Expression {
				return &ast.BinaryOpExpression{Left: column("id"), Operator: "+", Right: column("other")}
			},
			aliases:  []string{"bound"},
			wantSame: true,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			original := testCase.build()
			replaced := replaceAliasReferences(original, testCase.aliases)
			assert.Equal(t, testCase.build(), original, "the original expression is not changed")
			assert.Equal(t, testCase.wantSame, replaced == original)
		})
	}
}

func everyPosition(expression ast.Expression) []ast.Expression {
	if expression == nil {
		return nil
	}
	positions := []ast.Expression{expression}
	for child := range ast.Children(expression) {
		positions = append(positions, everyPosition(child)...)
	}
	return positions
}

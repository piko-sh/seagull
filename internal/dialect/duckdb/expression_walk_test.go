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
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func column(name string) *ast.ColumnRefExpression {
	return &ast.ColumnRefExpression{TableAlias: "", ColumnName: name, Span: ast.Span{}}
}

func renameColumns(node ast.Expression) ast.Expression {
	if reference, isColumn := node.(*ast.ColumnRefExpression); isColumn {
		return column("renamed_" + reference.ColumnName)
	}
	return node
}

func TestRewriteExpressionReachesEveryKindOfChild(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		expression ast.Expression
		want       ast.Expression
		name       string
	}{
		{
			name:       "a lambda's body and bindings",
			expression: &ast.LambdaExpression{Parameters: []string{"x"}, Body: column("a"), Bindings: []ast.Expression{column("b")}},
			want: &ast.LambdaExpression{Parameters: []string{"x"}, Body: column("renamed_a"),
				Bindings: []ast.Expression{column("renamed_b")}},
		},
		{
			name:       "a named argument's value",
			expression: &ast.NamedArgumentExpression{Name: "n", Value: column("a")},
			want:       &ast.NamedArgumentExpression{Name: "n", Value: column("renamed_a")},
		},
		{
			name:       "a row's elements",
			expression: &ast.RowValueExpression{Elements: []ast.Expression{column("a"), column("b")}},
			want:       &ast.RowValueExpression{Elements: []ast.Expression{column("renamed_a"), column("renamed_b")}},
		},
		{
			name: "a call's arguments inside a window",
			expression: &ast.WindowFunctionExpression{Function: &ast.FunctionCallExpression{FunctionName: "max",
				Arguments: []ast.Expression{column("a")}}},
			want: &ast.WindowFunctionExpression{Function: &ast.FunctionCallExpression{FunctionName: "max",
				Arguments: []ast.Expression{column("renamed_a")}}},
		},
		{name: "SQL NULL", expression: nil, want: nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, rewriteExpression(testCase.expression, renameColumns))
		})
	}
}

func TestRewriteExpressionLeavesTheOriginalTreeAsItWas(t *testing.T) {
	t.Parallel()

	original := &ast.BinaryOpExpression{Operator: "+", Left: column("a"), Right: &ast.LambdaExpression{
		Parameters: []string{"x"}, Body: column("b"), Bindings: nil}}

	rewritten := rewriteExpression(original, renameColumns)

	assert.Equal(t, &ast.BinaryOpExpression{Operator: "+", Left: column("a"), Right: &ast.LambdaExpression{
		Parameters: []string{"x"}, Body: column("b"), Bindings: nil}}, original)
	assert.Equal(t, &ast.BinaryOpExpression{Operator: "+", Left: column("renamed_a"), Right: &ast.LambdaExpression{
		Parameters: []string{"x"}, Body: column("renamed_b"), Bindings: nil}}, rewritten)
}

func TestColumnNamesInFindsTheColumnsOfLambdaBodies(t *testing.T) {
	t.Parallel()

	expression := &ast.FunctionCallExpression{FunctionName: "list_transform", Arguments: []ast.Expression{
		column("l"), &ast.LambdaExpression{Parameters: []string{"x"}, Body: column("weight"), Bindings: nil}}}

	assert.Equal(t, []string{"l", "weight"}, columnNamesIn(expression))
}

func TestRewriteExpressionRewritesANodeHeldTwiceOnce(t *testing.T) {
	shared := column("seed")
	lambda := &ast.LambdaExpression{Body: column("accumulator"), Bindings: []ast.Expression{shared}}
	call := &ast.FunctionCallExpression{FunctionName: "list_reduce", Arguments: []ast.Expression{shared, lambda}}

	replacements := 0
	rewritten := rewriteExpression(call, func(node ast.Expression) ast.Expression {
		if node == shared {
			replacements++
		}
		return node
	})
	assert.Equal(t, 1, replacements)
	assert.Same(t, call, rewritten)
}

func TestNestedListReductionsAnalyseInLittleTime(t *testing.T) {
	expression := "l"
	for range 28 {
		expression = "list_reduce([" + expression + "], (a, b) -> a + b)"
	}
	sql := "SELECT " + expression + " AS total FROM (SELECT [1, 2] AS l) AS t;"
	scaling.Within(t, 100*time.Millisecond, func(ctx context.Context) {
		engine := New()
		statements, err := engine.ParseStatements(ctx, sql)
		require.NoError(t, err)
		require.Len(t, statements, 1)
		_, err = engine.AnalyseQuery(ctx, nil, statements[0])
		require.NoError(t, err)
	})
}

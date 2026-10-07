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
	"seagull.sh/seagull/internal/testkit/scaling"
)

func TestProjectionFunctionCallAttachesExpression(t *testing.T) {
	t.Parallel()

	t.Run("nested toInt64(count())", func(t *testing.T) {
		t.Parallel()
		analysis := analyse(t, "SELECT toInt64(count()) AS total FROM events")
		require.Len(t, analysis.OutputColumns, 1)
		assert.Equal(t, "total", analysis.OutputColumns[0].Name)
		outer, ok := analysis.OutputColumns[0].Expression.(*ast.FunctionCallExpression)
		require.True(t, ok, "projection expression must be a FunctionCallExpression, got %T", analysis.OutputColumns[0].Expression)
		assert.Equal(t, "toInt64", outer.FunctionName)
		require.Len(t, outer.Arguments, 1)
		inner, ok := outer.Arguments[0].(*ast.FunctionCallExpression)
		require.True(t, ok, "argument must be the nested count() call, got %T", outer.Arguments[0])
		assert.Equal(t, "count", inner.FunctionName)
	})

	t.Run("bare count()", func(t *testing.T) {
		t.Parallel()
		analysis := analyse(t, "SELECT count() AS n FROM events")
		require.Len(t, analysis.OutputColumns, 1)
		call, ok := analysis.OutputColumns[0].Expression.(*ast.FunctionCallExpression)
		require.True(t, ok, "got %T", analysis.OutputColumns[0].Expression)
		assert.Equal(t, "count", call.FunctionName)
	})

	t.Run("single-arg cast toInt64(occurrence)", func(t *testing.T) {
		t.Parallel()
		analysis := analyse(t, "SELECT toInt64(occurrence) AS n FROM events")
		require.Len(t, analysis.OutputColumns, 1)
		call, ok := analysis.OutputColumns[0].Expression.(*ast.FunctionCallExpression)
		require.True(t, ok, "got %T", analysis.OutputColumns[0].Expression)
		assert.Equal(t, "toInt64", call.FunctionName)
	})
}

func TestProjectionDirectColumnHasNoExpression(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT id FROM events")
	require.Len(t, analysis.OutputColumns, 1)
	assert.Equal(t, "id", analysis.OutputColumns[0].ColumnName)
	assert.Nil(t, analysis.OutputColumns[0].Expression)
}

func TestContainsUnknownExpressionSearchesEveryKindOfNode(t *testing.T) {
	t.Parallel()

	unknown := func() ast.Expression { return &ast.UnknownExpression{} }
	literal := func() ast.Expression { return &ast.LiteralExpression{TypeName: engineNameUInt8} }
	testCases := []struct {
		expression ast.Expression
		name       string
		want       bool
	}{
		{name: "nothing", expression: nil},
		{name: "a literal", expression: literal()},
		{name: "an unknown expression", expression: unknown(), want: true},
		{name: "inside a lambda body", expression: newFunctionCall("arrayMap",
			&ast.LambdaExpression{Parameters: []string{"x"}, Body: unknown(), Bindings: nil}, column("tags")), want: true},
		{name: "inside a lambda binding", expression: &ast.LambdaExpression{Parameters: []string{"x"}, Body: literal(),
			Bindings: []ast.Expression{unknown()}}, want: true},
		{name: "inside a named argument", expression: newFunctionCall("f", &ast.NamedArgumentExpression{Value: unknown(), Name: "a"}), want: true},
		{name: "inside a row value", expression: &ast.RowValueExpression{Elements: []ast.Expression{literal(), unknown()}}, want: true},
		{name: "inside a struct constructor", expression: &ast.StructExpression{Names: []string{"a"}, Values: []ast.Expression{unknown()}}, want: true},
		{name: "inside a window function", expression: &ast.WindowFunctionExpression{Function: newFunctionCall("sum", unknown())}, want: true},
		{name: "inside an IN list", expression: &ast.InListExpression{Inner: literal(), Values: []ast.Expression{unknown()}}, want: true},
		{name: "a window function without a call", expression: &ast.WindowFunctionExpression{Function: nil}},
		{name: "a subquery, which is not entered", expression: &ast.ScalarSubqueryExpression{InnerQuery: &ast.RawQueryAnalysis{
			OutputColumns: []ast.RawOutputColumn{{Expression: unknown()}},
		}}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, containsUnknownExpression(testCase.expression))
		})
	}
}

func TestContainsUnknownExpressionIsLinearInADeepTree(t *testing.T) {
	t.Parallel()

	chain := func(length int) ast.Expression {
		var expression ast.Expression = &ast.LiteralExpression{TypeName: engineNameUInt8}
		for range length {
			expression = &ast.BinaryOpExpression{Left: expression, Operator: "+", Right: &ast.LiteralExpression{TypeName: engineNameUInt8}}
		}
		return expression
	}
	const smallerLength = 25_000
	smaller, larger := chain(smallerLength), chain(4*smallerLength)
	scaling.Linear(t,
		func() { require.False(t, containsUnknownExpression(smaller)) },
		func() { require.False(t, containsUnknownExpression(larger)) })
}

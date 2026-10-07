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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func column(name string) *ast.ColumnRefExpression {
	return &ast.ColumnRefExpression{TableAlias: "", ColumnName: name}
}

func TestLambdaParametersStandForTheirArguments(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
		want ast.Expression
	}{
		{
			name: "the analyser binds the parameters of arrayMap",
			sql:  "SELECT arrayMap(t -> length(t), tags) FROM gulls",
			want: newFunctionCall("arrayMap", &ast.LambdaExpression{Parameters: []string{"t"}, Body: newFunctionCall("length", column("t"))},
				column("tags")),
		},
		{
			name: "the accumulator of arrayFold is bound to its starting value",
			sql:  "SELECT arrayFold((total, s) -> total + s, sightings, 0) FROM gulls",
			want: newFunctionCall("arrayFold", &ast.LambdaExpression{Parameters: []string{"total", "s"},
				Body:     &ast.BinaryOpExpression{Left: column("total"), Operator: "+", Right: column("s")},
				Bindings: []ast.Expression{&ast.LiteralExpression{TypeName: engineNameUInt8}}},
				column("sightings"), &ast.LiteralExpression{TypeName: engineNameUInt8}),
		},
		{
			name: "a nested arrayFold binds its own accumulator",
			sql:  "SELECT arrayFold((total, t) -> arrayFold((total, t) -> total, grid, t), tags, 0) FROM gulls",
			want: newFunctionCall("arrayFold", &ast.LambdaExpression{Parameters: []string{"total", "t"}, Body: newFunctionCall("arrayFold",
				&ast.LambdaExpression{Parameters: []string{"total", "t"}, Body: column("total"), Bindings: []ast.Expression{column("t")}},
				column("grid"), column("t")),
				Bindings: []ast.Expression{&ast.LiteralExpression{TypeName: engineNameUInt8}}},
				column("tags"), &ast.LiteralExpression{TypeName: engineNameUInt8}),
		},
		{
			name: "a function that takes no lambda",
			sql:  "SELECT transform(t, [1], [2]) FROM gulls",
			want: newFunctionCall("transform", column("t"), newFunctionCall(functionArray, &ast.LiteralExpression{TypeName: engineNameUInt8}),
				newFunctionCall(functionArray, &ast.LiteralExpression{TypeName: engineNameUInt8})),
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := analyse(t, testCase.sql)
			require.Len(t, analysis.OutputColumns, 1)
			require.Equal(t, withoutSpans(testCase.want), withoutSpans(analysis.OutputColumns[0].Expression))
		})
	}
}

func TestLambdaParametersInConditionsStandForTheirArguments(t *testing.T) {
	t.Parallel()

	analysis := analyse(t, "SELECT id FROM gulls WHERE arrayFold((total, s) -> total + s, sightings, 0) > 1")
	require.Len(t, analysis.ClauseExpressions, 1)
	comparison, isComparison := analysis.ClauseExpressions[0].Expression.(*ast.ComparisonExpression)
	require.True(t, isComparison)
	call, isCall := comparison.Left.(*ast.FunctionCallExpression)
	require.True(t, isCall)
	lambda, isLambda := call.Arguments[0].(*ast.LambdaExpression)
	require.True(t, isLambda)
	require.Equal(t, []ast.Expression{&ast.LiteralExpression{TypeName: engineNameUInt8}}, lambda.Bindings)
	sum, isSum := lambda.Body.(*ast.BinaryOpExpression)
	require.True(t, isSum)
	require.Equal(t, withoutSpans(column("s")), withoutSpans(sum.Right))
}

func TestNestedArrayFoldsAreBoundInLinearTime(t *testing.T) {
	t.Parallel()

	nestedFolds := func(depth int) string {
		return "SELECT " + strings.Repeat("arrayFold((total, x) -> total + x, sightings, ", depth) + "0" +
			strings.Repeat(")", depth) + " FROM gulls"
	}
	const smallerDepth = 6
	smaller, larger := nestedFolds(smallerDepth), nestedFolds(4*smallerDepth)

	analysis := analyse(t, larger)
	require.Len(t, analysis.OutputColumns, 1)
	call, isCall := analysis.OutputColumns[0].Expression.(*ast.FunctionCallExpression)
	for range 4 * smallerDepth {
		require.True(t, isCall)
		lambda, isLambda := call.Arguments[0].(*ast.LambdaExpression)
		require.True(t, isLambda)
		require.Equal(t, []ast.Expression{call.Arguments[len(call.Arguments)-1]}, lambda.Bindings, "the accumulator is the starting value")
		call, isCall = call.Arguments[len(call.Arguments)-1].(*ast.FunctionCallExpression)
	}
	require.False(t, isCall)

	scaling.Linear(t,
		func() { analyse(t, smaller) },
		func() { analyse(t, larger) })
}

func TestBindingLambdasAgainVisitsEachNodeOnce(t *testing.T) {
	t.Parallel()

	nestedFolds := func(depth int) ast.Expression {
		var start ast.Expression = &ast.LiteralExpression{TypeName: engineNameUInt8}
		for range depth {
			lambda := &ast.LambdaExpression{Parameters: []string{"total", "x"}, Body: column("total"), Bindings: []ast.Expression{start}}
			start = newFunctionCall(functionArrayFold, lambda, column("sightings"), start)
		}
		return start
	}
	const smallerDepth = 5
	smaller, larger := nestedFolds(smallerDepth), nestedFolds(4*smallerDepth)
	scaling.Linear(t,
		func() { bindExpressionLambdas(smaller) },
		func() { bindExpressionLambdas(larger) })
}

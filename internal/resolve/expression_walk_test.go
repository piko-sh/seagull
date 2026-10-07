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

package resolve

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func TestSubqueriesFindsEachQueryAnExpressionHoldsOnceInOrder(t *testing.T) {
	t.Parallel()

	first := &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{{Name: "first"}}}
	second := &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{{Name: "second"}}}
	nested := &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{{Name: "nested", Expression: &ast.ExistsExpression{InnerQuery: second}}}}
	exists := func(query *ast.RawQueryAnalysis) ast.Expression { return &ast.ExistsExpression{InnerQuery: query} }
	scalar := func(query *ast.RawQueryAnalysis) ast.Expression {
		return &ast.ScalarSubqueryExpression{InnerQuery: query}
	}
	testCases := []struct {
		expression ast.Expression
		name       string
		want       []*ast.RawQueryAnalysis
	}{
		{name: "nothing", expression: nil, want: nil},
		{name: "a column", expression: &ast.ColumnRefExpression{ColumnName: "id"}, want: nil},
		{name: "an EXISTS", expression: exists(first), want: []*ast.RawQueryAnalysis{first}},
		{name: "an EXISTS holding no query", expression: &ast.ExistsExpression{InnerQuery: nil}, want: nil},
		{name: "subqueries among a call's arguments and its FILTER", want: []*ast.RawQueryAnalysis{first, second},
			expression: &ast.FunctionCallExpression{FunctionName: "f", Arguments: []ast.Expression{scalar(first)}, FilterExpression: exists(second)}},
		{name: "subqueries in a CASE, its ELSE last", want: []*ast.RawQueryAnalysis{first, second},
			expression: &ast.CaseWhenExpression{
				Branches:   []ast.CaseWhenBranch{{Condition: exists(first), Result: &ast.LiteralExpression{TypeName: "int"}}},
				ElseResult: scalar(second),
			}},
		{name: "a subquery in a lambda's body", want: []*ast.RawQueryAnalysis{first},
			expression: &ast.LambdaExpression{Parameters: []string{"x"}, Body: scalar(first)}},
		{name: "a subquery in a window function's call", want: []*ast.RawQueryAnalysis{first},
			expression: &ast.WindowFunctionExpression{Function: &ast.FunctionCallExpression{FunctionName: "f", Arguments: []ast.Expression{scalar(first)}}}},
		{name: "one subquery written twice", want: []*ast.RawQueryAnalysis{first},
			expression: &ast.BinaryOpExpression{Operator: "+", Left: scalar(first), Right: scalar(first)}},
		{name: "a subquery's own subqueries are not entered", expression: scalar(nested), want: []*ast.RawQueryAnalysis{nested}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, Subqueries(testCase.expression))
		})
	}
}

func TestPreorderLeavesOutLambdaBindingsAndNeedsNoRecursion(t *testing.T) {
	t.Parallel()

	bound := &ast.ColumnRefExpression{ColumnName: "bound"}
	body := &ast.ColumnRefExpression{ColumnName: "body"}
	deep := func(depth int) ast.Expression {
		var expression ast.Expression = &ast.LiteralExpression{TypeName: "int"}
		for range depth {
			expression = &ast.UnaryOpExpression{Operator: "-", Operand: expression}
		}
		return expression
	}
	testCases := []struct {
		expression ast.Expression
		name       string
		wantNodes  int
	}{
		{name: "nothing", expression: nil, wantNodes: 0},
		{name: "a lambda, its body walked and its bindings not", wantNodes: 2,
			expression: &ast.LambdaExpression{Parameters: []string{"x"}, Body: body, Bindings: []ast.Expression{bound}}},
		{name: "a tree far deeper than any limit", expression: deep(200_000), wantNodes: 200_001},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			nodes := 0
			for node := range preorder(testCase.expression) {
				require.NotSame(t, bound, node)
				nodes++
			}
			require.Equal(t, testCase.wantNodes, nodes)
		})
	}
}

func TestPreorderWalksSharedLambdaBindingsInLinearTime(t *testing.T) {
	t.Parallel()

	walk := func(depth int) func() {
		expression := reduceChain(depth)
		return func() {
			nodes := 0
			for range preorder(expression) {
				nodes++
			}
			require.Equal(t, 3*depth+1, nodes)
		}
	}

	scaling.Linear(t, walk(1_000), walk(4_000))
}

func TestPreorderStopsWhenTheLoopEnds(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		after int
	}{
		{name: "after the root", after: 1},
		{name: "after a child", after: 2},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			expression := &ast.BinaryOpExpression{Operator: "+", Left: &ast.ColumnRefExpression{ColumnName: "a"}, Right: &ast.ColumnRefExpression{ColumnName: "b"}}
			visited := 0
			for range preorder(expression) {
				visited++
				if visited == testCase.after {
					break
				}
			}
			require.Equal(t, testCase.after, visited)
		})
	}
}

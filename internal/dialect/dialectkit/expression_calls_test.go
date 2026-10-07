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

package dialectkit

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"seagull.sh/seagull/internal/dialect/ast"
)

func TestFindCallLooksThroughExpressions(t *testing.T) {
	t.Parallel()

	columns := &ast.FunctionCallExpression{FunctionName: "COLUMNS"}
	nested := &ast.FunctionCallExpression{FunctionName: "min", Arguments: []ast.Expression{columns}}
	tests := []struct {
		name       string
		expression ast.Expression
		want       *ast.FunctionCallExpression
	}{
		{name: "the call itself, ignoring case", expression: columns, want: columns},
		{name: "an argument of another call", expression: nested, want: columns},
		{name: "a CASE branch", expression: &ast.CaseWhenExpression{Branches: []ast.CaseWhenBranch{{Condition: &ast.LiteralExpression{}, Result: nested}}}, want: columns},
		{name: "a comparison", expression: &ast.ComparisonExpression{Left: columns, Right: &ast.LiteralExpression{}}, want: columns},
		{name: "a lambda body", expression: &ast.LambdaExpression{Body: columns}, want: columns},
		{name: "another call", expression: &ast.FunctionCallExpression{FunctionName: "lower"}, want: nil},
		{name: "not inside a subquery", expression: &ast.ScalarSubqueryExpression{InnerQuery: &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{{Expression: columns}}}}, want: nil},
		{name: "nothing", expression: nil, want: nil},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Same(t, testCase.want, FindCall(testCase.expression, "columns"))
		})
	}
}

func TestFindProjectionAndClauseCalls(t *testing.T) {
	t.Parallel()

	columns := &ast.FunctionCallExpression{FunctionName: "columns"}
	inProjection := &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{{Expression: columns}}}
	inBranch := &ast.RawQueryAnalysis{CompoundBranches: []ast.RawCompoundBranch{{Query: inProjection}}}
	inCTE := &ast.RawQueryAnalysis{CTEDefinitions: []ast.RawCTEDefinition{{OutputColumns: []ast.RawOutputColumn{{Expression: columns}}}}}
	inWhere := &ast.RawQueryAnalysis{ClauseExpressions: []ast.ClauseExpression{{Expression: columns, Clause: ast.ClauseWhere}}}

	assert.Same(t, columns, FindProjectionCall(inProjection, "columns"))
	assert.Same(t, columns, FindProjectionCall(inBranch, "columns"))
	assert.Same(t, columns, FindProjectionCall(inCTE, "columns"))
	assert.Nil(t, FindProjectionCall(inWhere, "columns"))
	assert.Nil(t, FindProjectionCall(nil, "columns"))
	assert.Same(t, columns, FindClauseCall(inWhere, "columns"))
	assert.Nil(t, FindClauseCall(inProjection, "columns"))
	assert.Nil(t, FindClauseCall(nil, "columns"))
}

func TestFindCallSearchesTreesDeeperThanAnyParseDepth(t *testing.T) {
	t.Parallel()

	const depth = 100_000
	columns := &ast.FunctionCallExpression{FunctionName: "columns"}
	cases := []struct {
		build func(ast.Expression) ast.Expression
		name  string
	}{
		{name: "a left-leaning chain of additions", build: func(inner ast.Expression) ast.Expression {
			return &ast.BinaryOpExpression{Left: inner, Operator: "+", Right: &ast.LiteralExpression{}}
		}},
		{name: "nested casts", build: func(inner ast.Expression) ast.Expression {
			return &ast.CastExpression{Inner: inner}
		}},
		{name: "nested calls", build: func(inner ast.Expression) ast.Expression {
			return &ast.FunctionCallExpression{FunctionName: "abs", Arguments: []ast.Expression{inner}}
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var tree ast.Expression = columns
			for range depth {
				tree = testCase.build(tree)
			}

			assert.Same(t, columns, FindCall(tree, "COLUMNS"))
		})
	}
}

func TestFindCallPassesOverATypedNilCall(t *testing.T) {
	t.Parallel()

	columns := &ast.FunctionCallExpression{FunctionName: "columns"}
	tree := &ast.BinaryOpExpression{Left: (*ast.FunctionCallExpression)(nil), Operator: "+", Right: columns}

	assert.Same(t, columns, FindCall(tree, "columns"))
}

func TestFindProjectionCallSearchesDeeplyNestedBranches(t *testing.T) {
	t.Parallel()

	const depth = 10_000
	columns := &ast.FunctionCallExpression{FunctionName: "columns"}
	query := &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{{Expression: columns}}}
	for range depth {
		query = &ast.RawQueryAnalysis{CompoundBranches: []ast.RawCompoundBranch{{Query: nil}, {Query: query}}}
	}

	assert.Same(t, columns, FindProjectionCall(query, "columns"))
}

func TestFindProjectionCallSearchesBranchesInOrder(t *testing.T) {
	t.Parallel()

	first := &ast.FunctionCallExpression{FunctionName: "columns"}
	second := &ast.FunctionCallExpression{FunctionName: "columns"}
	branch := func(call *ast.FunctionCallExpression) ast.RawCompoundBranch {
		return ast.RawCompoundBranch{Query: &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{{Expression: call}}}}
	}
	query := &ast.RawQueryAnalysis{CompoundBranches: []ast.RawCompoundBranch{branch(first), branch(second)}}

	assert.Same(t, first, FindProjectionCall(query, "columns"))
}

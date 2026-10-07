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

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func TestValidateAnalysisFindsATypedNilNodeAnywhereInTheQuery(t *testing.T) {
	t.Parallel()

	column := &ast.ColumnRefExpression{ColumnName: "id"}
	where := func(expression ast.Expression) *ast.RawQueryAnalysis {
		return &ast.RawQueryAnalysis{ClauseExpressions: []ast.ClauseExpression{{Expression: expression, Clause: ast.ClauseWhere}}}
	}
	testCases := []struct {
		analysis *ast.RawQueryAnalysis
		name     string
		wantNode string
	}{
		{name: "no analysis", analysis: nil},
		{name: "a NULL operand", analysis: where(&ast.ComparisonExpression{Left: column, Operator: "=", Right: nil})},
		{name: "a nil binary operator in WHERE", analysis: where((*ast.BinaryOpExpression)(nil)), wantNode: "*ast.BinaryOpExpression"},
		{name: "a nil column reference in WHERE", analysis: where(&ast.ComparisonExpression{Left: (*ast.ColumnRefExpression)(nil), Operator: "=", Right: column}),
			wantNode: "*ast.ColumnRefExpression"},
		{name: "a nil call in the select list", analysis: &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{
			{Name: "v", Expression: (*ast.FunctionCallExpression)(nil)}}}, wantNode: "*ast.FunctionCallExpression"},
		{name: "a nil lambda inside a call", analysis: where(&ast.FunctionCallExpression{FunctionName: "any_match",
			Arguments: []ast.Expression{column, (*ast.LambdaExpression)(nil)}}), wantNode: "*ast.LambdaExpression"},
		{name: "a nil named argument of a FROM call", analysis: &ast.RawQueryAnalysis{RawTableValuedFunctions: []ast.RawTableValuedFunctionReference{
			{FunctionName: "series", Arguments: []ast.Expression{(*ast.NamedArgumentExpression)(nil)}}}}, wantNode: "*ast.NamedArgumentExpression"},
		{name: "a nil window inside an EXISTS", analysis: where(&ast.ExistsExpression{InnerQuery: where((*ast.WindowFunctionExpression)(nil))}),
			wantNode: "*ast.WindowFunctionExpression"},
		{name: "a nil scalar subquery in a derived table", analysis: &ast.RawQueryAnalysis{RawDerivedTables: []ast.RawDerivedTableReference{
			{Alias: "d", InnerQuery: where((*ast.ScalarSubqueryExpression)(nil))}}}, wantNode: "*ast.ScalarSubqueryExpression"},
		{name: "a nil EXISTS in a predicate subquery", analysis: &ast.RawQueryAnalysis{PredicateSubqueries: []*ast.RawQueryAnalysis{
			where((*ast.ExistsExpression)(nil))}}, wantNode: "*ast.ExistsExpression"},
		{name: "a nil cast in a compound branch", analysis: &ast.RawQueryAnalysis{CompoundBranches: []ast.RawCompoundBranch{
			{Query: where((*ast.CastExpression)(nil))}}}, wantNode: "*ast.CastExpression"},
		{name: "a nil case in a CTE body", analysis: &ast.RawQueryAnalysis{CTEDefinitions: []ast.RawCTEDefinition{
			{Name: "c", OutputColumns: []ast.RawOutputColumn{{Name: "v", Expression: (*ast.CaseWhenExpression)(nil)}}}}},
			wantNode: "*ast.CaseWhenExpression"},
		{name: "a nil unary operator in an INSERT ... SELECT body", analysis: &ast.RawQueryAnalysis{InsertSelect: where((*ast.UnaryOpExpression)(nil))},
			wantNode: "*ast.UnaryOpExpression"},
		{name: "a nil literal in a grouping key", analysis: &ast.RawQueryAnalysis{GroupingSets: &ast.GroupingSets{Keys: []ast.Expression{(*ast.LiteralExpression)(nil)}}},
			wantNode: "*ast.LiteralExpression"},
		{name: "a nil struct in an ARRAY JOIN source", analysis: &ast.RawQueryAnalysis{ArrayJoinClauses: []ast.RawArrayJoinClause{
			{Alias: "a", Source: (*ast.StructExpression)(nil)}}}, wantNode: "*ast.StructExpression"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			problem := ValidateAnalysis(testCase.analysis)

			if testCase.wantNode == "" {
				require.Nil(t, problem)
				return
			}
			require.NotNil(t, problem)
			require.Equal(t, diag.CodeInternalNilGuard, problem.Code)
			require.Equal(t, diag.SeverityError, problem.Severity)
			require.Equal(t, "the dialect's analysis holds a nil "+testCase.wantNode+
				" where an expression belongs, so the query was not analysed", problem.Message)
		})
	}
}

func TestValidateAnalysisWalksAQueryReachedTwiceOnce(t *testing.T) {
	t.Parallel()

	query := &ast.RawQueryAnalysis{}
	for range 64 {
		query = &ast.RawQueryAnalysis{
			ClauseExpressions:   []ast.ClauseExpression{{Expression: &ast.ExistsExpression{InnerQuery: query}, Clause: ast.ClauseWhere}},
			PredicateSubqueries: []*ast.RawQueryAnalysis{query},
		}
	}

	require.Nil(t, ValidateAnalysis(query))
}

func TestValidateAnalysisWalksANodeReachedTwiceOnce(t *testing.T) {
	t.Parallel()

	validate := func(depth int) func() {
		analysis := &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{{Name: "folded", Expression: reduceChain(depth)}}}
		return func() {
			require.Nil(t, ValidateAnalysis(analysis))
		}
	}

	scaling.Linear(t, validate(4), validate(16))
}

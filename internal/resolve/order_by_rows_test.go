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
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

type explodingEngine struct {
	*fakedialect.Engine
}

var _ dialect.FunctionResolverPort = explodingEngine{}

func (explodingEngine) ResolveFunctionCall(_ context.Context, _ *sqlmodel.Catalogue, name string, _ string, argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if name == "interrupt" {
		return nil, context.Canceled
	}
	if name != "explode" || len(argumentTypes) != 1 || argumentTypes[0].ElementType == nil {
		return nil, nil
	}
	return &ast.FunctionResolution{ReturnType: *argumentTypes[0].ElementType, NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		DataAccess: sqlmodel.DataAccessReadOnly, ReturnsSet: true}, nil
}

func newExplodingResolver() (*TypeResolver, *sqlmodel.Table) {
	engine := explodingEngine{Engine: &fakedialect.Engine{}}
	catalogue := fakedialect.NewCatalogue("public")
	integer := sqlmodel.SQLType{EngineName: "int8", Category: sqlmodel.TypeCategoryInteger}
	builtins := &ast.FunctionCatalogue{Functions: map[string][]*sqlmodel.Function{
		"count": {sqlmodel.NewFunction(nil, integer, sqlmodel.FunctionNullableNeverNull, sqlmodel.WithFunctionName("count"), sqlmodel.WithAggregate())},
		"generate_series": {sqlmodel.NewFunction(nil, integer, sqlmodel.FunctionNullableNeverNull, sqlmodel.WithFunctionName("generate_series"),
			sqlmodel.WithReturnsSet())},
		"lower": {sqlmodel.NewFunction([]sqlmodel.FunctionArgument{{Name: "value", Type: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}}},
			sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}, sqlmodel.FunctionNullableReturnsNullOnNull, sqlmodel.WithFunctionName("lower"))},
	}}
	items := &sqlmodel.Table{Name: "items", Columns: []sqlmodel.Column{
		{Name: "id", SQLType: integer},
		{Name: "name", SQLType: sqlmodel.SQLType{EngineName: "text", Category: sqlmodel.TypeCategoryText}},
		{Name: "tags", SQLType: sqlmodel.SQLType{EngineName: "int8[]", Category: sqlmodel.TypeCategoryArray, ElementType: new(integer)}},
	}}
	return NewTypeResolver(catalogue, NewFunctionResolver(builtins, catalogue, engine), engine), items
}

func TestOrderByAddsRows(t *testing.T) {
	t.Parallel()

	ordering := func(expression ast.Expression) ast.ClauseExpression {
		return ast.ClauseExpression{Clause: ast.ClauseOrderBy, Expression: expression}
	}
	testCases := []struct {
		name           string
		clauses        []ast.ClauseExpression
		aliasOperands  ast.Clause
		expected       bool
		outputAliasing bool
	}{
		{name: "a column adds no rows", clauses: []ast.ClauseExpression{ordering(column("id"))}, expected: false},
		{name: "a set-returning call adds rows", clauses: []ast.ClauseExpression{ordering(call("generate_series"))}, expected: true},
		{name: "a set-returning call inside a scalar function adds rows",
			clauses: []ast.ClauseExpression{ordering(call("lower", call("generate_series")))}, expected: true},
		{name: "a later ORDER BY expression that returns a set adds rows",
			clauses: []ast.ClauseExpression{ordering(column("id")), ordering(call("generate_series"))}, expected: true},
		{name: "a call the engine finds set-returning for its argument types adds rows",
			clauses: []ast.ClauseExpression{ordering(call("explode", column("tags")))}, expected: true},
		{name: "a call the engine does not find set-returning for its argument types adds none",
			clauses: []ast.ClauseExpression{ordering(call("explode", column("id")))}, expected: false},
		{name: "an aggregate adds no rows", clauses: []ast.ClauseExpression{ordering(call("count"))}, expected: false},
		{name: "a set-returning call inside a subquery adds none", clauses: []ast.ClauseExpression{ordering(&ast.ScalarSubqueryExpression{
			InnerQuery: &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{{Expression: call("generate_series")}}}})}, expected: false},
		{name: "a set-returning call in another clause adds none",
			clauses: []ast.ClauseExpression{{Clause: ast.ClauseWhere, Expression: call("generate_series")}}, expected: false},
		{name: "a set-returning call over a select-list alias the ORDER BY may name adds rows",
			clauses: []ast.ClauseExpression{ordering(call("explode", column("labels")))}, aliasOperands: ast.ClauseOrderBy,
			outputAliasing: true, expected: true},
		{name: "a set-returning call over a select-list alias the ORDER BY may not name adds none",
			clauses: []ast.ClauseExpression{ordering(call("explode", column("labels")))}, aliasOperands: ast.ClauseWhere,
			outputAliasing: true, expected: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver, items := newExplodingResolver()
			rules := testScopeRules
			rules.AliasOperands = testCase.aliasOperands
			scope := NewRootScopeChain(ScopeKindQuery, rules)
			scope.AddTable(ast.TableReference{Name: "items"}, ast.JoinInner, items)
			analysis := &ast.RawQueryAnalysis{ClauseExpressions: testCase.clauses}
			if testCase.outputAliasing {
				analysis.OutputColumns = []ast.RawOutputColumn{{Name: "labels", Expression: column("tags")}}
			}
			resolver.ResolveProjection(t.Context(), analysis.OutputColumns, nil, scope)

			require.Equal(t, testCase.expected, resolver.OrderByAddsRows(t.Context(), analysis, scope))
		})
	}
}

func TestOrderByAddsRowsLeavesTheResolverAsItFoundIt(t *testing.T) {
	t.Parallel()

	resolver, items := newExplodingResolver()
	scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
	scope.AddTable(ast.TableReference{Name: "items"}, ast.JoinInner, items)
	enclosingCall := call("lower", column("name"))
	resolver.resolvedCalls = map[*ast.FunctionCallExpression]resolvedCall{enclosingCall: {aggregate: true}}
	analysis := &ast.RawQueryAnalysis{ClauseExpressions: []ast.ClauseExpression{
		{Clause: ast.ClauseOrderBy, Expression: call("lower", column("missing"))},
		{Clause: ast.ClauseOrderBy, Expression: call("generate_series")},
	}}

	require.True(t, resolver.OrderByAddsRows(t.Context(), analysis, scope))
	require.Empty(t, resolver.takeNotices())
	require.Equal(t, map[*ast.FunctionCallExpression]resolvedCall{enclosingCall: {aggregate: true}}, resolver.resolvedCalls)
}

func TestOrderByAddsRowsIsFalseWithoutAQueryOrOnceInterrupted(t *testing.T) {
	t.Parallel()

	ordered := &ast.RawQueryAnalysis{ClauseExpressions: []ast.ClauseExpression{{Clause: ast.ClauseOrderBy, Expression: call("generate_series")}}}
	interrupted := &ast.RawQueryAnalysis{ClauseExpressions: []ast.ClauseExpression{
		{Clause: ast.ClauseOrderBy, Expression: call("interrupt")},
		{Clause: ast.ClauseOrderBy, Expression: call("generate_series")},
	}}
	testCases := []struct {
		analysis  *ast.RawQueryAnalysis
		name      string
		cancelled bool
	}{
		{name: "no query", analysis: nil, cancelled: false},
		{name: "a cancelled context", analysis: ordered, cancelled: true},
		{name: "a resolver interrupted before a later set-returning call", analysis: interrupted, cancelled: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			if testCase.cancelled {
				cancel(context.Canceled)
			}
			resolver, items := newExplodingResolver()
			scope := NewRootScopeChain(ScopeKindQuery, testScopeRules)
			scope.AddTable(ast.TableReference{Name: "items"}, ast.JoinInner, items)

			require.False(t, resolver.OrderByAddsRows(ctx, testCase.analysis, scope))
		})
	}
}

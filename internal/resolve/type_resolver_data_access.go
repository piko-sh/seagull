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

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// callVisitor walks a query's expressions, typing every function call it reaches and
// noting whether any may change data.
type callVisitor struct {
	// resolver types the calls.
	resolver *TypeResolver

	// visited holds the nested queries already walked, so a query reachable both from an
	// expression and from the predicate subqueries is walked once.
	visited map[*ast.RawQueryAnalysis]struct{}

	// modifying becomes true at the first call that may change data.
	modifying bool

	// stopped becomes true when the dialect's resolver was interrupted typing a call, which
	// ends the walk.
	stopped bool
}

// finished reports whether the walk has nothing left to learn: a call that may change
// data was found, the dialect's resolver was interrupted, or the context is done.
//
// Returns bool which is true when the walk should stop.
func (v *callVisitor) finished(ctx context.Context) bool {
	return v.modifying || v.stopped || ctx.Err() != nil
}

// reachedLimit notes that the walk reached the depth limit with part of the query left
// unread, which may call a function that changes data, so the query is taken to change
// data.
func (v *callVisitor) reachedLimit() {
	v.modifying = true
	v.resolver.noteDepthLimit(depthLimitError(
		"the query nests expressions or subqueries more than %d levels deep, so it was not checked for calls that change data "+
			"beyond the limit and is taken to change data", MaxExpressionResolveDepth))
}

// visitQuery walks a query's select list, clauses and nested queries.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the query; nil is ignored.
// Takes scope (*ScopeChain) which resolves its names.
// Takes depth (int) which bounds the nesting.
func (v *callVisitor) visitQuery(ctx context.Context, analysis *ast.RawQueryAnalysis, scope *ScopeChain, depth int) {
	if analysis == nil || v.finished(ctx) {
		return
	}
	if depth >= MaxExpressionResolveDepth {
		v.reachedLimit()
		return
	}
	for index := range analysis.OutputColumns {
		v.visit(ctx, analysis.OutputColumns[index].Expression, scope, depth+1)
	}
	for index := range analysis.ClauseExpressions {
		v.visit(ctx, analysis.ClauseExpressions[index].Expression, scope, depth+1)
	}
	for index := range analysis.RawTableValuedFunctions {
		v.visitTableFunction(ctx, &analysis.RawTableValuedFunctions[index], scope, depth+1)
	}
	for _, subquery := range analysis.PredicateSubqueries {
		v.visitNested(ctx, subquery, scope, depth+1)
	}
	for index := range analysis.CompoundBranches {
		v.visitNested(ctx, analysis.CompoundBranches[index].Query, scope, depth+1)
	}
}

// visitNested walks a query nested in another, in a scope that sees the enclosing query's
// names. A query is walked once however many paths reach it.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the nested query; nil is ignored.
// Takes parent (*ScopeChain) which is the enclosing query's scope.
// Takes depth (int) which bounds the nesting.
func (v *callVisitor) visitNested(ctx context.Context, analysis *ast.RawQueryAnalysis, parent *ScopeChain, depth int) {
	if analysis == nil || v.finished(ctx) {
		return
	}
	if depth >= MaxExpressionResolveDepth {
		v.reachedLimit()
		return
	}
	if _, walked := v.visited[analysis]; walked {
		return
	}
	v.visited[analysis] = struct{}{}
	scope := NewScopeChain(ScopeKindSubquery, parent)
	v.resolver.subqueryScopeBuilder().addRawTablesToScope(ctx, analysis, scope)
	v.visitQuery(ctx, analysis, scope, depth)
}

// visit walks one expression, typing each function call it holds.
//
// Takes expression (ast.Expression) which is the expression; nil is ignored.
// Takes scope (*ScopeChain) which resolves its names.
// Takes depth (int) which bounds the recursion.
func (v *callVisitor) visit(ctx context.Context, expression ast.Expression, scope *ScopeChain, depth int) {
	if expression == nil || v.finished(ctx) {
		return
	}
	if depth >= MaxExpressionResolveDepth {
		v.reachedLimit()
		return
	}
	switch typed := expression.(type) {
	case *ast.FunctionCallExpression:
		v.visitCall(ctx, typed, scope, depth)
	case *ast.WindowFunctionExpression:
		if typed.Function != nil {
			v.visitCall(ctx, typed.Function, scope, depth)
		}
	case *ast.ScalarSubqueryExpression:
		v.visitNested(ctx, typed.InnerQuery, scope, depth+1)
	case *ast.ExistsExpression:
		v.visitNested(ctx, typed.InnerQuery, scope, depth+1)
	default:
		for operand := range operands(expression) {
			v.visit(ctx, operand, scope, depth+1)
		}
	}
}

// visitTableFunction marks a FROM function call as potentially modifying data unless the
// resolver knows it is read-only, and visits its arguments. Unknown table-valued
// functions are treated as read-only.
//
// Takes reference (*ast.RawTableValuedFunctionReference) which is the FROM item.
// Takes scope (*ScopeChain) which resolves its arguments.
// Takes depth (int) which bounds the recursion.
func (v *callVisitor) visitTableFunction(ctx context.Context, reference *ast.RawTableValuedFunctionReference, scope *ScopeChain, depth int) {
	if len(reference.Functions) == 0 {
		v.visitTableFunctionCall(ctx, reference.FunctionName, reference.Arguments, scope, depth)
		return
	}
	for index := range reference.Functions {
		v.visitTableFunctionCall(ctx, reference.Functions[index].FunctionName, reference.Functions[index].Arguments, scope, depth)
	}
}

// visitTableFunctionCall notes whether one call of a FROM item may change data, then
// walks its arguments.
//
// Takes functionName (string) which is the function's name as the reference writes it,
// optionally schema-qualified.
// Takes arguments ([]ast.Expression) which are the call's arguments.
// Takes scope (*ScopeChain) which resolves the arguments.
// Takes depth (int) which bounds the recursion.
func (v *callVisitor) visitTableFunctionCall(
	ctx context.Context,
	functionName string,
	arguments []ast.Expression,
	scope *ScopeChain,
	depth int,
) {
	if v.finished(ctx) {
		return
	}
	schema, name := splitFunctionName(functionName)
	callArguments := v.resolver.resolveCallArguments(ctx, arguments, scope, new(false))
	match, resolveError, err := v.resolver.functionResolver.resolveCall(ctx, name, schema, callArguments)
	if err != nil {
		v.stopped = true
		return
	}
	if resolveError == nil && match != nil && match.dataAccess != sqlmodel.DataAccessReadOnly {
		v.modifying = true
		return
	}
	for _, argument := range arguments {
		v.visit(ctx, argument, scope, depth+1)
	}
}

// visitCall types a function call, noting whether it may change data, then walks its
// arguments and filter.
//
// Takes call (*ast.FunctionCallExpression) which is the call.
// Takes scope (*ScopeChain) which resolves its arguments.
// Takes depth (int) which bounds the recursion.
func (v *callVisitor) visitCall(ctx context.Context, call *ast.FunctionCallExpression, scope *ScopeChain, depth int) {
	if call.FunctionName != "" {
		if _, _, err := v.resolver.resolveFunctionCallExpression(ctx, call, scope, &v.modifying); interrupted(err) {
			v.stopped = true
			return
		}
	}
	for _, argument := range call.Arguments {
		v.visit(ctx, argument, scope, depth+1)
	}
	v.visit(ctx, call.FilterExpression, scope, depth+1)
}

// CallsDataModifyingFunction reports whether a query calls a function that may change
// data.
//
// It checks expressions, the functions FROM items call and nested queries throughout the
// statement. Unknown functions are treated as potentially modifying data, except a FROM
// item's table-valued function, which is taken to read; typing diagnostics are reported
// elsewhere. The walk stops when the context is done or the dialect's resolver is
// interrupted, and reports what it found until then. A query nested more deeply than
// MaxExpressionResolveDepth is taken to change data, since the part beyond the limit is
// not read, and the limit is recorded for TakeDepthLimits.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the query.
// Takes scope (*ScopeChain) which resolves the query's names.
//
// Returns bool which is true when the query calls a function that may change data.
func (r *TypeResolver) CallsDataModifyingFunction(ctx context.Context, analysis *ast.RawQueryAnalysis, scope *ScopeChain) bool {
	saved := r.notices
	defer func() { r.notices = saved }()
	visitor := callVisitor{resolver: r, visited: make(map[*ast.RawQueryAnalysis]struct{}), modifying: false, stopped: false}
	visitor.visitQuery(ctx, analysis, scope, 0)
	return visitor.modifying
}

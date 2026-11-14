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
)

// OrderByAddsRows reports whether a query's ORDER BY calls a set-returning function
// outside any subquery, as ORDER BY unnest(tags) does.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the query. A nil value has no ORDER BY.
// Takes scope (*ScopeChain) which resolves the query's names after its select list has
// been resolved.
//
// Returns bool which is true when an ORDER BY expression calls a set-returning function.
func (r *TypeResolver) OrderByAddsRows(ctx context.Context, analysis *ast.RawQueryAnalysis, scope *ScopeChain) bool {
	if analysis == nil {
		return false
	}
	enclosingCalls, enclosingNotices := r.resolvedCalls, r.notices
	r.resolvedCalls = make(map[*ast.FunctionCallExpression]resolvedCall)
	defer func() { r.resolvedCalls, r.notices = enclosingCalls, enclosingNotices }()
	for index := range analysis.ClauseExpressions {
		clauseExpression := &analysis.ClauseExpressions[index]
		if !clauseExpression.Clause.Includes(ast.ClauseOrderBy) {
			continue
		}
		if ctx.Err() != nil {
			return false
		}
		addsRows, err := r.orderingAddsRows(ctx, clauseExpression, scope)
		if err != nil {
			return false
		}
		if addsRows {
			return true
		}
	}
	return false
}

// orderingAddsRows types one ORDER BY expression and reports whether it calls a
// set-returning function outside any subquery.
//
// Takes clauseExpression (*ast.ClauseExpression) which is the ORDER BY expression.
// Takes scope (*ScopeChain) which resolves its names.
//
// Returns bool which is true when the expression calls a set-returning function.
// Returns error which is the interruption that stopped the dialect's resolver, or nil.
// Problems with the expression itself are reported elsewhere.
func (r *TypeResolver) orderingAddsRows(ctx context.Context, clauseExpression *ast.ClauseExpression, scope *ScopeChain) (bool, error) {
	restore := scope.exposeAliasOperands(clauseExpression.Clause)
	_, _, err := r.resolveExpressionType(ctx, clauseExpression.Expression, scope, new(false))
	restore()
	if interrupted(err) {
		return false, err
	}
	for node := range preorder(clauseExpression.Expression) {
		call, isCall := node.(*ast.FunctionCallExpression)
		if isCall && r.resolvedCalls[call].returnsSet {
			return true, nil
		}
	}
	return false, nil
}

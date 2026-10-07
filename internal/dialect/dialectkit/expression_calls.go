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
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
)

// FindProjectionCall finds a named function call that can affect a query's output shape.
//
// The search includes compound branches and CTE bodies and compares names without regard
// to ASCII case. It keeps its own list of the queries left to search rather than
// recursing, so queries nested to any depth are searched.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the query; nil finds nothing.
// Takes name (string) which is the function name.
//
// Returns *ast.FunctionCallExpression which is the call, or nil when there is none.
func FindProjectionCall(analysis *ast.RawQueryAnalysis, name string) *ast.FunctionCallExpression {
	pending := []*ast.RawQueryAnalysis{analysis}
	for len(pending) > 0 {
		last := len(pending) - 1
		query := pending[last]
		pending[last] = nil
		pending = pending[:last]
		if query == nil {
			continue
		}
		if found := findOutputCall(query, name); found != nil {
			return found
		}
		for _, branch := range slices.Backward(query.CompoundBranches) {
			pending = append(pending, branch.Query)
		}
	}
	return nil
}

// FindClauseCall returns the first call to a function, compared without regard to ASCII
// case, in the clause expressions of a query: its WHERE, HAVING, join conditions, GROUP
// BY, ORDER BY and SET.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the query; nil finds nothing.
// Takes name (string) which is the function name.
//
// Returns *ast.FunctionCallExpression which is the call, or nil when there is none.
func FindClauseCall(analysis *ast.RawQueryAnalysis, name string) *ast.FunctionCallExpression {
	if analysis == nil {
		return nil
	}
	for index := range analysis.ClauseExpressions {
		if found := FindCall(analysis.ClauseExpressions[index].Expression, name); found != nil {
			return found
		}
	}
	return nil
}

// FindCall returns the first call to a function, compared without regard to ASCII case,
// in an expression tree, visiting each node before its children in the order ast.Children
// gives them. It does not look inside subqueries.
//
// The walk keeps its own stack rather than recursing, so a tree of any depth, such as a
// long chain of operators, is searched to the end.
//
// Takes expression (ast.Expression) which is the tree; nil finds nothing.
// Takes name (string) which is the function name.
//
// Returns *ast.FunctionCallExpression which is the call, or nil when there is none.
func FindCall(expression ast.Expression, name string) *ast.FunctionCallExpression {
	for node := range ast.Preorder(expression) {
		if call, isCall := node.(*ast.FunctionCallExpression); isCall && call != nil && strings.EqualFold(call.FunctionName, name) {
			return call
		}
	}
	return nil
}

// findOutputCall returns the first call to a function in the output columns of a query
// and of its CTE bodies, without searching its compound branches.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the query.
// Takes name (string) which is the function name.
//
// Returns *ast.FunctionCallExpression which is the call, or nil when there is none.
func findOutputCall(analysis *ast.RawQueryAnalysis, name string) *ast.FunctionCallExpression {
	for index := range analysis.OutputColumns {
		if found := FindCall(analysis.OutputColumns[index].Expression, name); found != nil {
			return found
		}
	}
	for index := range analysis.CTEDefinitions {
		for _, column := range analysis.CTEDefinitions[index].OutputColumns {
			if found := FindCall(column.Expression, name); found != nil {
				return found
			}
		}
	}
	return nil
}

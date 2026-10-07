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
	"iter"
	"slices"

	"seagull.sh/seagull/internal/dialect/ast"
)

// Subqueries returns the queries an expression holds outside any subquery: the query of
// each EXISTS and scalar subquery, each once, in the order they are written.
//
// The expression is walked with an explicit stack, so a tree of any depth is walked
// without exhausting the goroutine's stack; the queries found are not entered.
//
// Takes expression (ast.Expression) which is the expression; nil holds none.
//
// Returns []*ast.RawQueryAnalysis which holds the queries, or nil when there are none.
func Subqueries(expression ast.Expression) []*ast.RawQueryAnalysis {
	var found []*ast.RawQueryAnalysis
	var seen map[*ast.RawQueryAnalysis]struct{}
	for node := range preorder(expression) {
		inner, holds := ast.InnerQuery(node)
		if !holds {
			continue
		}
		if _, listed := seen[inner]; listed {
			continue
		}
		if seen == nil {
			seen = make(map[*ast.RawQueryAnalysis]struct{})
		}
		seen[inner] = struct{}{}
		found = append(found, inner)
	}
	return found
}

// operands returns the expressions written directly inside an expression, in the order
// ast.Children gives them, leaving out the bindings of a lambda: they stand for values
// written elsewhere in the call that takes the lambda, such as the list a reduce folds,
// which a walk reaches there.
//
// Takes expression (ast.Expression) which is the expression; nil has no operands.
//
// Returns iter.Seq[ast.Expression] which yields the operands in order.
func operands(expression ast.Expression) iter.Seq[ast.Expression] {
	lambda, isLambda := expression.(*ast.LambdaExpression)
	if !isLambda {
		return ast.Children(expression)
	}
	return func(yield func(ast.Expression) bool) {
		if lambda != nil && lambda.Body != nil {
			yield(lambda.Body)
		}
	}
}

// preorder returns every node of an expression tree outside any subquery, each parent
// before its operands, as operands gives them. The nodes waiting to be visited sit on an
// explicit stack, so the walk needs no recursion.
//
// Takes expression (ast.Expression) which is the root; nil yields nothing.
//
// Returns iter.Seq[ast.Expression] which yields the nodes in order.
func preorder(expression ast.Expression) iter.Seq[ast.Expression] {
	return func(yield func(ast.Expression) bool) {
		if expression == nil {
			return
		}
		stack := []ast.Expression{expression}
		for len(stack) > 0 {
			last := len(stack) - 1
			node := stack[last]
			stack[last] = nil
			stack = stack[:last]
			if !yield(node) {
				return
			}
			mark := len(stack)
			for operand := range operands(node) {
				stack = append(stack, operand)
			}
			slices.Reverse(stack[mark:])
		}
	}
}

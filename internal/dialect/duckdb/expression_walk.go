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

package duckdb

import (
	"reflect"

	"seagull.sh/seagull/internal/dialect/ast"
)

// expressionRewriter rewrites one expression tree, remembering what each node it has
// rewritten became, so a node the tree holds in two places is rewritten once and stays
// shared.
type expressionRewriter struct {
	// replace returns the node to use in place of the given one, or the node itself.
	replace func(ast.Expression) ast.Expression

	// rewritten maps each pointer node already rewritten to what it became.
	rewritten map[ast.Expression]ast.Expression
}

// rewrite rewrites a node bottom up, reusing the result for a node met before.
//
// Takes expression (ast.Expression) which is the node.
//
// Returns ast.Expression which is the rewritten node.
func (rewriter *expressionRewriter) rewrite(expression ast.Expression) ast.Expression {
	if ast.IsNil(expression) {
		return expression
	}
	shared := reflect.TypeOf(expression).Kind() == reflect.Pointer
	if shared {
		if done, found := rewriter.rewritten[expression]; found {
			return done
		}
	}
	result := rewriter.replace(ast.RewriteChildren(expression, rewriter.rewrite))
	if shared {
		rewriter.rewritten[expression] = result
	}
	return result
}

// rewriteExpression rebuilds an expression tree bottom up, replacing each node with what
// replace returns for it once its children have been rewritten.
//
// It follows every kind of node ast.RewriteChildren knows, lambda bodies, named arguments
// and row values included. Subqueries are left to the caller, as their scopes differ.
//
// Nodes with unchanged children are reused, and the original tree is never modified.
// Shared nodes, such as a lambda binding also used as a call argument, are rewritten
// once.
//
// Takes expression (ast.Expression) which is the tree to rewrite; nil stands for SQL NULL
// and is returned as it is, as is a typed-nil node.
// Takes replace (func(ast.Expression) ast.Expression) which returns the node to use in
// place of the given one, or the node itself.
//
// Returns ast.Expression which is the rewritten tree.
func rewriteExpression(expression ast.Expression, replace func(ast.Expression) ast.Expression) ast.Expression {
	rewriter := &expressionRewriter{replace: replace, rewritten: make(map[ast.Expression]ast.Expression)}
	return rewriter.rewrite(expression)
}

// columnNamesIn lists the names of the unqualified and qualified columns an expression
// refers to, outside any subquery, lambda bodies included.
//
// Takes expression (ast.Expression) which is the expression to search.
//
// Returns []string which holds the column names in the order met.
func columnNamesIn(expression ast.Expression) []string {
	var names []string
	for node := range ast.Preorder(expression) {
		if column, isColumn := node.(*ast.ColumnRefExpression); isColumn && column != nil {
			names = append(names, column.ColumnName)
		}
	}
	return names
}

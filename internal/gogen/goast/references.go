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

package goast

import (
	"go/ast"
	"iter"
)

// UnqualifiedIdentifiers collects identifiers from declarations, excluding selector names
// and text inside comments and literals. It requires no generated source parse.
//
// Takes declarations ([]ast.Decl) which holds the generated declarations to inspect.
//
// Returns map[string]struct{} which contains bare identifier names, excluding selector
// field names.
func UnqualifiedIdentifiers(declarations []ast.Decl) map[string]struct{} {
	names := make(map[string]struct{})
	var walk func(ast.Node) bool
	walk = func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.Ident:
			names[node.Name] = struct{}{}
		case *ast.SelectorExpr:
			ast.Inspect(node.X, walk)
			return false
		}
		return true
	}
	for _, declaration := range declarations {
		ast.Inspect(declaration, walk)
	}
	return names
}

// SelectorQualifiers yields the identifier on the left of every selector under a node,
// such as "sql" in sql.ErrNoRows, which is how generated code refers to an imported
// package. It yields once per selector, in source order.
//
// Takes node (ast.Node) which is the syntax to walk.
//
// Returns iter.Seq[string] which yields each qualifier.
func SelectorQualifiers(node ast.Node) iter.Seq[string] {
	return func(yield func(string) bool) {
		for visited := range ast.Preorder(node) {
			selector, isSelector := visited.(*ast.SelectorExpr)
			if !isSelector {
				continue
			}
			if qualifier, isIdentifier := selector.X.(*ast.Ident); isIdentifier && !yield(qualifier.Name) {
				return
			}
		}
	}
}

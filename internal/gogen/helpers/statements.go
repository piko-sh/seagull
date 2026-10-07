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

package helpers

import (
	"go/ast"
	"go/token"
	"slices"
	"strconv"
	"strings"
)

// planStatements folds every if statement on an inlined condition to the branch it takes
// and unrolls every range over an inlined list.
func (r *renderer) planStatements() {
	for node := range ast.Preorder(r.file) {
		switch typed := node.(type) {
		case *ast.BlockStmt:
			r.planList(typed.List)
		case *ast.CaseClause:
			r.planList(typed.Body)
		case *ast.CommClause:
			r.planList(typed.Body)
		}
	}
}

// planList plans the folds and unrolls of one statement list.
//
// Takes list ([]ast.Stmt) which is the list.
func (r *renderer) planList(list []ast.Stmt) {
	for index, statement := range list {
		switch typed := statement.(type) {
		case *ast.IfStmt:
			if condition, inlined := r.inlinedCondition(typed); inlined {
				r.foldIf(typed, condition, list[index+1:])
			}
		case *ast.RangeStmt:
			if elements, variable, inlined := r.inlinedRange(typed); inlined {
				r.unroll(typed, elements, variable)
			}
		}
	}
}

// inlinedCondition reports the value of an if statement's condition when it is an inlined
// bool or its negation.
//
// Takes statement (*ast.IfStmt) which is the statement.
//
// Returns value (bool) which is the condition's value.
// Returns inlined (bool) which is false when the condition is not an inlined bool.
func (r *renderer) inlinedCondition(statement *ast.IfStmt) (value, inlined bool) {
	if statement.Init != nil {
		return false, false
	}
	condition := ast.Unparen(statement.Cond)
	negated := false
	if unary, isUnary := condition.(*ast.UnaryExpr); isUnary && unary.Op == token.NOT {
		negated = true
		condition = ast.Unparen(unary.X)
	}
	identifier, isIdentifier := condition.(*ast.Ident)
	if !isIdentifier {
		return false, false
	}
	inlinedValue, isBool := r.inlined[identifier.Name].(bool)
	if !isBool {
		return false, false
	}
	return inlinedValue != negated, true
}

// foldIf replaces an if statement with the statements of the branch it takes. When that
// branch ends in a return, branch or panic, the statements after the if statement in the
// same list can never run, so they go too.
//
// Takes statement (*ast.IfStmt) which is the statement.
// Takes condition (bool) which is its condition's value.
// Takes rest ([]ast.Stmt) which holds the statements after it in its list.
func (r *renderer) foldIf(statement *ast.IfStmt, condition bool, rest []ast.Stmt) {
	var taken ast.Stmt = statement.Body
	if !condition {
		taken = statement.Else
	}
	block, isBlock := taken.(*ast.BlockStmt)
	switch {
	case taken == nil || (isBlock && len(block.List) == 0):
		from, to := r.wholeLines(statement.Pos(), statement.End())
		r.add(edit{start: from, end: to, replacement: "", expand: nil})
		return
	case !isBlock:
		from, to := r.offset(taken.Pos()), r.offset(taken.End())
		r.add(edit{start: r.offset(statement.Pos()), end: r.offset(statement.End()), replacement: "", expand: func(extra []edit) string {
			return r.text(from, to, extra)
		}})
		return
	}
	end := statement.End()
	if terminates(block.List[len(block.List)-1]) && len(rest) > 0 {
		end = rest[len(rest)-1].End()
	}
	from, to := r.offset(block.Lbrace)+1, r.offset(block.Rbrace)
	r.add(edit{start: r.offset(statement.Pos()), end: r.offset(end), replacement: "", expand: func(extra []edit) string {
		return strings.TrimSpace(r.text(from, to, extra))
	}})
}

// inlinedRange reports the elements of a range over an inlined list of strings, written
// as for _, name := range list.
//
// Takes statement (*ast.RangeStmt) which is the statement.
//
// Returns []string which holds the list's elements.
// Returns string which is the name of the loop's variable.
// Returns bool which is false when the statement does not range over an inlined list.
func (r *renderer) inlinedRange(statement *ast.RangeStmt) ([]string, string, bool) {
	list, isList := ast.Unparen(statement.X).(*ast.Ident)
	if !isList {
		return nil, "", false
	}
	elements, isStrings := r.inlined[list.Name].([]string)
	if !isStrings {
		return nil, "", false
	}
	key, isKey := statement.Key.(*ast.Ident)
	variable, isVariable := statement.Value.(*ast.Ident)
	if !isKey || key.Name != "_" || !isVariable {
		return nil, "", false
	}
	return elements, variable.Name, true
}

// unroll replaces a range over an inlined list with one copy of its body per element, the
// loop variable written as the element.
//
// Takes statement (*ast.RangeStmt) which is the statement.
// Takes elements ([]string) which holds the list's elements.
// Takes variable (string) which is the loop variable's name.
func (r *renderer) unroll(statement *ast.RangeStmt, elements []string, variable string) {
	if len(elements) == 0 {
		from, to := r.wholeLines(statement.Pos(), statement.End())
		r.add(edit{start: from, end: to, replacement: "", expand: nil})
		return
	}
	var uses []ast.Node
	for node := range ast.Preorder(statement.Body) {
		if identifier, isIdentifier := node.(*ast.Ident); isIdentifier && identifier.Name == variable {
			uses = append(uses, identifier)
		}
	}
	from, to := r.offset(statement.Body.Lbrace)+1, r.offset(statement.Body.Rbrace)
	r.add(edit{start: r.offset(statement.Pos()), end: r.offset(statement.End()), replacement: "", expand: func(extra []edit) string {
		copies := make([]string, 0, len(elements))
		for _, element := range elements {
			substitutions := slices.Clone(extra)
			for _, use := range uses {
				substitutions = append(substitutions, edit{start: r.offset(use.Pos()), end: r.offset(use.End()), replacement: strconv.Quote(element), expand: nil})
			}
			copies = append(copies, strings.TrimSpace(r.text(from, to, substitutions)))
		}
		return strings.Join(copies, "\n")
	}})
}

// terminates reports whether a statement ends the list it is in: a return, a break,
// continue or goto, or a call of panic.
//
// Takes statement (ast.Stmt) which is the statement.
//
// Returns bool which is true when no statement after it in its list can run.
func terminates(statement ast.Stmt) bool {
	switch typed := statement.(type) {
	case *ast.ReturnStmt, *ast.BranchStmt:
		return true
	case *ast.ExprStmt:
		call, isCall := typed.X.(*ast.CallExpr)
		if !isCall {
			return false
		}
		function, isIdentifier := call.Fun.(*ast.Ident)
		return isIdentifier && function.Name == "panic"
	default:
		return false
	}
}

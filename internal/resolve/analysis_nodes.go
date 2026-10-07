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
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
)

// nodeValidator walks a dialect's analysis of a query, and the queries nested in it,
// looking for a typed-nil expression node.
type nodeValidator struct {
	// visited holds the queries already queued, so a query reachable along several paths is
	// walked once.
	visited map[*ast.RawQueryAnalysis]struct{}

	// walked holds the expression nodes already walked, so a node a dialect reaches along
	// several paths, as a lambda binding standing for an argument of its call does, is
	// walked once with the nodes below it.
	walked map[ast.Expression]struct{}

	// pending holds the queries still to walk.
	pending []*ast.RawQueryAnalysis
}

// queue adds a query to the walk unless it is nil or queued already.
//
// Takes query (*ast.RawQueryAnalysis) which is the query.
func (v *nodeValidator) queue(query *ast.RawQueryAnalysis) {
	if query == nil {
		return
	}
	if _, queued := v.visited[query]; queued {
		return
	}
	v.visited[query] = struct{}{}
	v.pending = append(v.pending, query)
}

// query walks the expressions of one query and queues the queries nested in it.
//
// Takes query (*ast.RawQueryAnalysis) which is the query, not nil.
//
// Returns ast.Expression which is the first typed-nil node found, or nil.
func (v *nodeValidator) query(query *ast.RawQueryAnalysis) ast.Expression {
	for index := range query.OutputColumns {
		if node := v.expression(query.OutputColumns[index].Expression); node != nil {
			return node
		}
	}
	for index := range query.ClauseExpressions {
		if node := v.expression(query.ClauseExpressions[index].Expression); node != nil {
			return node
		}
	}
	for index := range query.ArrayJoinClauses {
		if node := v.expression(query.ArrayJoinClauses[index].Source); node != nil {
			return node
		}
	}
	if query.GroupingSets != nil {
		if node := v.expressions(query.GroupingSets.Keys); node != nil {
			return node
		}
	}
	if node := v.tableFunctions(query.RawTableValuedFunctions); node != nil {
		return node
	}
	v.queueNested(query)
	return nil
}

// tableFunctions walks the arguments of a query's FROM-clause function calls.
//
// Takes references ([]ast.RawTableValuedFunctionReference) which are the calls.
//
// Returns ast.Expression which is the first typed-nil node found, or nil.
func (v *nodeValidator) tableFunctions(references []ast.RawTableValuedFunctionReference) ast.Expression {
	for index := range references {
		if node := v.expressions(references[index].Arguments); node != nil {
			return node
		}
		for callIndex := range references[index].Functions {
			if node := v.expressions(references[index].Functions[callIndex].Arguments); node != nil {
				return node
			}
		}
	}
	return nil
}

// queueNested queues the queries nested directly in a query: its predicate subqueries,
// derived tables, CTE bodies, compound branches and INSERT ... SELECT body.
//
// Takes query (*ast.RawQueryAnalysis) which is the query, not nil.
func (v *nodeValidator) queueNested(query *ast.RawQueryAnalysis) {
	for _, nested := range query.PredicateSubqueries {
		v.queue(nested)
	}
	for index := range query.RawDerivedTables {
		v.queue(query.RawDerivedTables[index].InnerQuery)
	}
	for index := range query.CTEDefinitions {
		v.queue(cteBody(&query.CTEDefinitions[index]))
	}
	for index := range query.CompoundBranches {
		v.queue(query.CompoundBranches[index].Query)
	}
	v.queue(query.InsertSelect)
}

// expressions walks each expression of a list.
//
// Takes expressions ([]ast.Expression) which are the expressions.
//
// Returns ast.Expression which is the first typed-nil node found, or nil.
func (v *nodeValidator) expressions(expressions []ast.Expression) ast.Expression {
	for _, expression := range expressions {
		if node := v.expression(expression); node != nil {
			return node
		}
	}
	return nil
}

// expression walks every node of an expression tree not walked before, queueing the
// subquery each EXISTS and scalar subquery holds.
//
// Takes expression (ast.Expression) which is the root; nil holds nothing.
//
// Returns ast.Expression which is the first typed-nil node found, or nil.
func (v *nodeValidator) expression(expression ast.Expression) ast.Expression {
	var found ast.Expression
	ast.Walk(expression, func(node ast.Expression) bool {
		if found != nil {
			return false
		}
		if ast.IsNil(node) {
			found = node
			return false
		}
		if _, walked := v.walked[node]; walked {
			return false
		}
		v.walked[node] = struct{}{}
		if inner, isSubquery := ast.InnerQuery(node); isSubquery {
			v.queue(inner)
		}
		return true
	})
	return found
}

// ValidateAnalysis looks through a dialect's analysis of a query, and every query nested
// in it, for a typed-nil expression node: an expression holding a nil pointer, which has
// nothing to read. A nil expression, which stands for SQL NULL, is allowed.
//
// The walk keeps its own stacks, so an analysis of any depth is walked without recursion,
// and each nested query and expression node is walked once however many paths reach it.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the query; nil holds nothing.
//
// Returns *Error which is a Q030 error naming the first typed-nil node found, or nil.
func ValidateAnalysis(analysis *ast.RawQueryAnalysis) *Error {
	validator := &nodeValidator{
		visited: make(map[*ast.RawQueryAnalysis]struct{}),
		walked:  make(map[ast.Expression]struct{}),
		pending: nil,
	}
	validator.queue(analysis)
	for len(validator.pending) > 0 {
		last := len(validator.pending) - 1
		query := validator.pending[last]
		validator.pending = validator.pending[:last]
		if node := validator.query(query); node != nil {
			return newError(diag.CodeInternalNilGuard,
				"the dialect's analysis holds a nil %T where an expression belongs, so the query was not analysed", node).
				withSeverity(diag.SeverityError)
		}
	}
	return nil
}

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
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// markQuantifiedArrays makes parameter array elements non-null for ANY, ALL and SOME
// comparisons, where NULL elements cannot match rows. Existing declarations from the
// compared column take precedence.
//
// Takes parameterTypes (map[int]*sqlmodel.QueryParameter) which holds the parameters by
// number, changed in place.
// Takes rawAnalysis (*ast.RawQueryAnalysis) which is the query; nil marks nothing.
func markQuantifiedArrays(parameterTypes map[int]*sqlmodel.QueryParameter, rawAnalysis *ast.RawQueryAnalysis) {
	numbers := make(map[int]bool)
	collectQuantifiedArrays(rawAnalysis, numbers, 0)
	for number := range numbers {
		if parameter, found := parameterTypes[number]; found && parameter.NonNullElements == nil {
			parameter.NonNullElements = new(true)
		}
	}
}

// collectQuantifiedArrays adds to numbers each parameter a quantified comparison of a
// query reads as its array: in the select list, the clauses, the arms of a set operation
// and every query nested in them.
//
// Takes rawAnalysis (*ast.RawQueryAnalysis) which is the query, or nil.
// Takes numbers (map[int]bool) which receives the parameters' numbers.
// Takes depth (int) which bounds the walk.
func collectQuantifiedArrays(rawAnalysis *ast.RawQueryAnalysis, numbers map[int]bool, depth int) {
	if rawAnalysis == nil || depth >= MaxExpressionResolveDepth {
		return
	}
	for index := range rawAnalysis.OutputColumns {
		collectQuantifiedOperands(rawAnalysis.OutputColumns[index].Expression, numbers)
	}
	for index := range rawAnalysis.ClauseExpressions {
		collectQuantifiedOperands(rawAnalysis.ClauseExpressions[index].Expression, numbers)
	}
	for index := range rawAnalysis.CompoundBranches {
		collectQuantifiedArrays(rawAnalysis.CompoundBranches[index].Query, numbers, depth+1)
	}
	for _, subquery := range collectDirectSubqueries(rawAnalysis) {
		collectQuantifiedArrays(subquery, numbers, depth+1)
	}
}

// collectQuantifiedOperands adds to numbers each parameter a quantified comparison within
// an expression, outside any subquery, reads as its array, alone or under casts.
//
// Takes expression (ast.Expression) which is the expression, or nil.
// Takes numbers (map[int]bool) which receives the parameters' numbers.
func collectQuantifiedOperands(expression ast.Expression, numbers map[int]bool) {
	for node := range preorder(expression) {
		comparison, isComparison := node.(*ast.ComparisonExpression)
		if !isComparison || comparison == nil || comparison.Quantifier == "" {
			continue
		}
		if number, isParameter := parameterStoodFor(comparison.Right, 0); isParameter {
			numbers[number] = true
		}
	}
}

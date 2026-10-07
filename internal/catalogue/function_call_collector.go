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

package catalogue

import (
	"maps"
	"slices"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/resolve"
)

// collectedCalls is what a walk of an analysis finds its queries call.
type collectedCalls struct {
	// functions holds the unique names of the functions called in expressions, sorted, or
	// nil when there are none.
	functions []string

	// tables holds the unique names of the table-valued functions read as FROM items,
	// sorted, or nil when there are none.
	tables []string

	// limited reports that the analysis nests its queries more deeply than the walk follows,
	// so calls past the limit were not seen.
	limited bool
}

// callCollector accumulates the function names a walk of an analysis finds.
type callCollector struct {
	// seen holds the discovered function names.
	seen map[string]struct{}

	// tables holds the discovered names of table-valued functions read as FROM items.
	tables map[string]struct{}

	// pending holds the nested queries still to walk, each with how deeply it is nested.
	pending []pendingQuery

	// limited records that the walk reached the depth limit and left a query out.
	limited bool
}

// collect walks every expression and nested query reachable from a raw analysis,
// recording the function names each references.
//
// It walks INSERT ... SELECT bodies, compound (UNION/INTERSECT/EXCEPT) branches, CTE
// bodies, FROM-clause derived subqueries, the arguments of table-valued function calls,
// ARRAY JOIN sources and predicate subqueries, and every subquery an expression holds, so
// a function that writes data transitively through a subquery is still discovered and the
// caller is correctly classified as data-modifying rather than routed to a read replica.
//
// Expressions are walked with ast.Walk, which follows every expression kind to any depth
// without recursing. Nested queries wait on an explicit list, and one nested more deeply
// than resolve.MaxExpressionResolveDepth, the depth the resolver follows, is left out and
// recorded as limited.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to walk.
func (c *callCollector) collect(analysis *ast.RawQueryAnalysis) {
	c.pending = append(c.pending, pendingQuery{query: analysis, depth: 0})
	for len(c.pending) > 0 {
		next := c.pending[len(c.pending)-1]
		c.pending = c.pending[:len(c.pending)-1]
		if next.query == nil {
			continue
		}
		if next.depth >= resolve.MaxExpressionResolveDepth {
			c.limited = true
			continue
		}
		c.collectFromQuery(next.query, next.depth)
	}
}

// collectFromQuery records the function names one query's expressions reference, and
// queues the queries it nests.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the query, not nil.
// Takes depth (int) which is how many queries it is nested in.
func (c *callCollector) collectFromQuery(analysis *ast.RawQueryAnalysis, depth int) {
	for _, column := range analysis.OutputColumns {
		c.collectFromExpression(column.Expression, depth)
	}
	for _, clause := range analysis.ClauseExpressions {
		c.collectFromExpression(clause.Expression, depth)
	}
	for index := range analysis.RawTableValuedFunctions {
		c.collectFromTableFunction(&analysis.RawTableValuedFunctions[index], depth)
	}
	for index := range analysis.ArrayJoinClauses {
		c.collectFromExpression(analysis.ArrayJoinClauses[index].Source, depth)
	}
	c.queue(analysis.InsertSelect, depth)
	for _, branch := range analysis.CompoundBranches {
		c.queue(branch.Query, depth)
	}
	for index := range analysis.CTEDefinitions {
		definition := &analysis.CTEDefinitions[index]
		for _, column := range definition.OutputColumns {
			c.collectFromExpression(column.Expression, depth)
		}
		for _, branch := range definition.CompoundBranches {
			c.queue(branch.Query, depth)
		}
		c.queue(definition.Query, depth)
	}
	for _, subquery := range analysis.PredicateSubqueries {
		c.queue(subquery, depth)
	}
	for index := range analysis.RawDerivedTables {
		c.queue(analysis.RawDerivedTables[index].InnerQuery, depth)
	}
}

// collectFromTableFunction records the table-valued functions a FROM item calls, and the
// functions their arguments call.
//
// Takes reference (*ast.RawTableValuedFunctionReference) which is the FROM item.
// Takes depth (int) which is how many queries the item's query is nested in.
func (c *callCollector) collectFromTableFunction(reference *ast.RawTableValuedFunctionReference, depth int) {
	if len(reference.Functions) == 0 && reference.FunctionName != "" {
		c.tables[reference.FunctionName] = struct{}{}
	}
	for _, argument := range reference.Arguments {
		c.collectFromExpression(argument, depth)
	}
	for index := range reference.Functions {
		if name := reference.Functions[index].FunctionName; name != "" {
			c.tables[name] = struct{}{}
		}
		for _, argument := range reference.Functions[index].Arguments {
			c.collectFromExpression(argument, depth)
		}
	}
}

// queue adds a query nested in another to the queries still to walk.
//
// Takes query (*ast.RawQueryAnalysis) which is the nested query; nil is skipped.
// Takes depth (int) which is how many queries the query holding it is nested in.
func (c *callCollector) queue(query *ast.RawQueryAnalysis, depth int) {
	if query != nil {
		c.pending = append(c.pending, pendingQuery{query: query, depth: depth + 1})
	}
}

// collectFromExpression walks an expression tree and records the name of every function
// it calls, through every kind of expression: arguments, named arguments, FILTER
// conditions, CASE branches, window functions, lambda bodies, struct constructors and the
// rest. The subqueries of EXISTS and scalar subqueries are queued to be walked.
//
// A name is recorded as the dialect gave it, qualified by the call's schema when it names
// one, so the call graph keys it by the catalogue's identifier rules.
//
// Takes expression (ast.Expression) which is the expression tree to walk.
// Takes depth (int) which is how many queries the expression's query is nested in.
func (c *callCollector) collectFromExpression(expression ast.Expression, depth int) {
	ast.Walk(expression, func(node ast.Expression) bool {
		if ast.IsNil(node) {
			return false
		}
		if call, isCall := node.(*ast.FunctionCallExpression); isCall {
			name := call.FunctionName
			if call.Schema != "" {
				name = call.Schema + "." + name
			}
			c.seen[name] = struct{}{}
		}
		if inner, holds := ast.InnerQuery(node); holds {
			c.queue(inner, depth)
		}
		return true
	})
}

// pendingQuery is a nested query waiting to be walked.
type pendingQuery struct {
	// query is the nested query.
	query *ast.RawQueryAnalysis

	// depth is how many queries it is nested in.
	depth int
}

// collectFunctionCallsWithinLimit collects the unique names of the functions an analysis
// calls, in the expressions of its output columns, its clauses and the queries nested in
// it, and of the table-valued functions it reads as FROM items, and says when the
// analysis nests its queries more deeply than the walk follows.
//
// Takes analysis (*ast.RawQueryAnalysis) which holds the raw query analysis.
//
// Returns collectedCalls which holds the names found.
func collectFunctionCallsWithinLimit(analysis *ast.RawQueryAnalysis) collectedCalls {
	if analysis == nil {
		return collectedCalls{functions: nil, tables: nil, limited: false}
	}

	collector := &callCollector{seen: make(map[string]struct{}), tables: make(map[string]struct{}), pending: nil, limited: false}
	collector.collect(analysis)
	return collectedCalls{functions: sortedNames(collector.seen), tables: sortedNames(collector.tables), limited: collector.limited}
}

// sortedNames returns the names of a set in order.
//
// Takes names (map[string]struct{}) which is the set.
//
// Returns []string which holds the names sorted, or nil for an empty set.
func sortedNames(names map[string]struct{}) []string {
	if len(names) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(names))
}

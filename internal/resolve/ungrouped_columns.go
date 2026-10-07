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

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// ungroupedGrouping is what a scope knows of its query's GROUP BY clause.
type ungroupedGrouping uint8

const (
	// groupingUnknown is a scope whose query's GROUP BY clause was not recorded, such as the
	// body of a CTE a dialect gives no analysis of its own, or a scope no query registered;
	// its select list is never judged.
	groupingUnknown ungroupedGrouping = iota

	// groupingByClause is a scope whose query has a GROUP BY clause.
	groupingByClause

	// groupingWithoutClause is a scope whose query has no GROUP BY clause, so an aggregate
	// in its select list makes the query one group.
	groupingWithoutClause
)

// resolvedCall is what the overload a call reached says of it.
type resolvedCall struct {
	// aggregate reports an aggregate, which collapses the rows it reads into one.
	aggregate bool

	// elementsKnown reports a call whose result's elements are as nullable as its type says,
	// as ast.FunctionResolution.ElementsKnown describes.
	elementsKnown bool
}

// registerGrouping records whether the scope's query has a GROUP BY clause: a grouping
// modifier, a column or an expression the dialect recorded for the clause, or the
// clause's place in the statement.
//
// Takes raw (*ast.RawQueryAnalysis) which is the scope's query.
func (s *ScopeChain) registerGrouping(raw *ast.RawQueryAnalysis) {
	if s == nil || raw == nil || s.groupingWithheld {
		return
	}
	s.grouped = groupingWithoutClause
	if hasGroupByClause(raw) {
		s.grouped = groupingByClause
	}
}

// readsOwnColumn reports whether a reference names a column of a relation the scope
// itself holds, rather than one of an enclosing query, which is the same in every row of
// the scope's query.
//
// Takes reference (*ast.ColumnRefExpression) which is the reference.
//
// Returns bool which is true for a column of the scope's own relations.
func (s *ScopeChain) readsOwnColumn(reference *ast.ColumnRefExpression) bool {
	if reference == nil || reference.ColumnName == "" {
		return false
	}
	column, table, err := s.ResolveColumn(reference.TableAlias, reference.ColumnName)
	return err == nil && column != nil && s.holds(table)
}

// judgeUngroupedColumns applies dialect rules to unaggregated columns in aggregate
// queries without GROUP BY. It makes affected outputs nullable and reports errors or
// warnings when the engine rejects them.
//
// Takes rawColumns ([]ast.RawOutputColumn) which is the select list.
// Takes scope (*ScopeChain) which is the select list's scope.
// Takes projection (*Projection) which holds the resolved columns and their origins.
// Takes place (func(*Error, ast.RawOutputColumn, int) *Error) which returns a problem
// found in the item at an index as it is to be reported.
func (r *TypeResolver) judgeUngroupedColumns(
	rawColumns []ast.RawOutputColumn,
	scope *ScopeChain,
	projection *Projection,
	place func(problem *Error, raw ast.RawOutputColumn, index int) *Error,
) {
	if scope == nil || scope.grouped != groupingWithoutClause || !r.selectListAggregates(rawColumns) {
		return
	}
	for index, raw := range rawColumns {
		references, bare := r.ungroupedReferences(raw, scope)
		if !bare {
			continue
		}
		for column := range projection.Columns {
			if column < len(projection.Origins) && projection.Origins[column] == index {
				projection.Columns[column].Nullable = true
			}
		}
		if r.ungroupedColumns == dialect.UngroupedColumnsNullable {
			continue
		}
		for _, reference := range references {
			projection.Problems = append(projection.Problems, place(r.ungroupedColumnProblem(reference), raw, index))
		}
	}
}

// selectListAggregates reports whether a select list calls an aggregate outside every
// window function's own call and every subquery.
//
// Takes rawColumns ([]ast.RawOutputColumn) which is the select list.
//
// Returns bool which is true when an item aggregates.
func (r *TypeResolver) selectListAggregates(rawColumns []ast.RawOutputColumn) bool {
	for index := range rawColumns {
		if r.holdsPlainAggregate(rawColumns[index].Expression, 0) {
			return true
		}
	}
	return false
}

// holdsPlainAggregate reports whether an expression calls an aggregate that collapses the
// rows it reads into one: a call that resolved to an aggregate, outside any subquery and
// lambda, the call of a window function itself excepted.
//
// Takes expression (ast.Expression) which is the expression.
// Takes depth (int) which bounds the search.
//
// Returns bool which is true when the expression aggregates.
func (r *TypeResolver) holdsPlainAggregate(expression ast.Expression, depth int) bool {
	if expression == nil || depth >= MaxExpressionResolveDepth {
		return false
	}
	switch typed := expression.(type) {
	case *ast.WindowFunctionExpression:
		return typed != nil && typed.Function != nil && r.anyHoldsPlainAggregate(slices.Values(typed.Function.Arguments), depth+1)
	case *ast.FunctionCallExpression:
		if typed == nil {
			return false
		}
		if r.resolvedCalls[typed].aggregate {
			return true
		}
		return r.anyHoldsPlainAggregate(slices.Values(typed.Arguments), depth+1)
	case *ast.LambdaExpression:
		return false
	}
	return r.anyHoldsPlainAggregate(operands(expression), depth+1)
}

// anyHoldsPlainAggregate reports whether any of a list of expressions calls an aggregate,
// as holdsPlainAggregate judges it.
//
// Takes expressions (iter.Seq[ast.Expression]) which yields the expressions.
// Takes depth (int) which bounds the search.
//
// Returns bool which is true when one of them aggregates.
func (r *TypeResolver) anyHoldsPlainAggregate(expressions iter.Seq[ast.Expression], depth int) bool {
	for expression := range expressions {
		if r.holdsPlainAggregate(expression, depth) {
			return true
		}
	}
	return false
}

// ungroupedReferences returns the references a select item makes, outside every
// aggregate, to columns of the relations of its own query: a star over them, a column it
// names alone, or a column an expression reads outside every aggregate call, subquery and
// lambda.
//
// Takes raw (ast.RawOutputColumn) which is the item.
// Takes scope (*ScopeChain) which is the item's scope.
//
// Returns []*ast.ColumnRefExpression which holds the references to report, empty for a
// star, which is reported at the item.
// Returns bool which is true when the item reads such a column.
func (r *TypeResolver) ungroupedReferences(raw ast.RawOutputColumn, scope *ScopeChain) ([]*ast.ColumnRefExpression, bool) {
	if raw.IsStar {
		return []*ast.ColumnRefExpression{{TableAlias: raw.TableAlias, ColumnName: "*", Span: ast.Span{}}}, len(scope.Tables) > 0
	}
	if raw.Expression == nil && raw.ColumnName != "" {
		reference := &ast.ColumnRefExpression{TableAlias: raw.TableAlias, ColumnName: raw.ColumnName, Span: ast.Span{}}
		return []*ast.ColumnRefExpression{reference}, scope.readsOwnColumn(reference)
	}
	var references []*ast.ColumnRefExpression
	r.collectUngroupedReferences(raw.Expression, scope, &references, 0)
	return references, len(references) > 0
}

// collectUngroupedReferences adds to references each reference an expression makes,
// outside every aggregate call, subquery and lambda, to a column of its own query's
// relations.
//
// Takes expression (ast.Expression) which is the expression.
// Takes scope (*ScopeChain) which is the expression's scope.
// Takes references (*[]*ast.ColumnRefExpression) which receives the references.
// Takes depth (int) which bounds the search.
func (r *TypeResolver) collectUngroupedReferences(
	expression ast.Expression,
	scope *ScopeChain,
	references *[]*ast.ColumnRefExpression,
	depth int,
) {
	if expression == nil || depth >= MaxExpressionResolveDepth {
		return
	}
	if column, isColumn := expression.(*ast.ColumnRefExpression); isColumn {
		if scope.readsOwnColumn(column) {
			*references = append(*references, column)
		}
		return
	}
	for child := range r.ungroupedChildren(expression) {
		r.collectUngroupedReferences(child, scope, references, depth+1)
	}
}

// ungroupedChildren returns the expressions inside an expression that its query reads in
// each row: the arguments of a window function, nothing of an aggregate call or a lambda,
// and every child of anything else.
//
// Takes expression (ast.Expression) which is the expression.
//
// Returns iter.Seq[ast.Expression] which yields the children.
func (r *TypeResolver) ungroupedChildren(expression ast.Expression) iter.Seq[ast.Expression] {
	switch typed := expression.(type) {
	case *ast.WindowFunctionExpression:
		if typed == nil || typed.Function == nil {
			return slices.Values[[]ast.Expression](nil)
		}
		return slices.Values(typed.Function.Arguments)
	case *ast.FunctionCallExpression:
		if typed == nil || r.resolvedCalls[typed].aggregate {
			return slices.Values[[]ast.Expression](nil)
		}
		return slices.Values(typed.Arguments)
	case *ast.LambdaExpression:
		return slices.Values[[]ast.Expression](nil)
	}
	return operands(expression)
}

// ungroupedColumnProblem reports a column read outside every aggregate of a query that
// aggregates without GROUP BY, as an error where the engine rejects the query and a
// warning where its mode decides.
//
// Takes reference (*ast.ColumnRefExpression) which is the column, or a star.
//
// Returns *Error which is the problem, placed at the reference where its position is
// known.
func (r *TypeResolver) ungroupedColumnProblem(reference *ast.ColumnRefExpression) *Error {
	name := reference.ColumnName
	if reference.TableAlias != "" {
		name = reference.TableAlias + "." + name
	}
	problem := newError(diag.CodeUngroupedColumn,
		"%s is read outside every aggregate of a query that aggregates without GROUP BY, which the engine rejects; "+
			"group by it or read it through an aggregate such as max", name).withSeverity(diag.SeverityError)
	if r.ungroupedColumns == dialect.UngroupedColumnsWarned {
		problem = newError(diag.CodeUngroupedColumn,
			"%s is read outside every aggregate of a query that aggregates without GROUP BY, which the engine rejects "+
				"in its default mode and otherwise reads as NULL when the query reads no row; group by it or read it "+
				"through an aggregate such as max", name)
	}
	problem.Span = Span{Start: reference.Span.Start, End: reference.Span.End}
	return problem
}

// givesOneRow identifies aggregate subqueries that guarantee one row and let the first
// column determine nullability. It excludes GROUP BY, clauses that change the row count,
// and unaggregated local columns in the first output.
//
// Takes raw (*ast.RawQueryAnalysis) which is the subquery.
// Takes scope (*ScopeChain) which is the subquery's scope.
//
// Returns bool which is true for a subquery that returns one row.
func (r *TypeResolver) givesOneRow(raw *ast.RawQueryAnalysis, scope *ScopeChain) bool {
	if raw == nil || scope == nil || len(raw.OutputColumns) == 0 || len(raw.CompoundBranches) > 0 || hasGroupByClause(raw) {
		return false
	}
	for index := range raw.ClauseExpressions {
		if raw.ClauseExpressions[index].Clause.Includes(ast.ClauseHaving) ||
			raw.ClauseExpressions[index].Clause.Includes(ast.ClauseRowShaping) {
			return false
		}
	}
	for index := range raw.ParameterReferences {
		if raw.ParameterReferences[index].Context == sqlmodel.ParameterContextLimit ||
			raw.ParameterReferences[index].Context == sqlmodel.ParameterContextOffset {
			return false
		}
	}
	first := raw.OutputColumns[0]
	if !r.holdsPlainAggregate(first.Expression, 0) {
		return false
	}
	_, ungrouped := r.ungroupedReferences(first, scope)
	return !ungrouped
}

// hasGroupByClause reports whether a query has a GROUP BY clause.
//
// Takes raw (*ast.RawQueryAnalysis) which is the query.
//
// Returns bool which is true when the dialect recorded one.
func hasGroupByClause(raw *ast.RawQueryAnalysis) bool {
	if len(raw.GroupByColumns) > 0 || raw.GroupingSets != nil || raw.Spans.GroupBy.Known() {
		return true
	}
	for index := range raw.ClauseExpressions {
		if raw.ClauseExpressions[index].Clause.Includes(ast.ClauseGroupBy) {
			return true
		}
	}
	return false
}

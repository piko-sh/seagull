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

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/observe"
	"seagull.sh/seagull/internal/sqlmodel"
)

// Projection is a query's resolved select list: its output columns, the item of the list
// each came from, and what it tells a statement that counts the query's rows.
type Projection struct {
	// Columns holds the output columns, each star expanded into the columns it stands for.
	Columns []sqlmodel.OutputColumn

	// Origins holds, parallel to Columns, the index into the raw select list of the item
	// each column came from, so the columns of one star share an index.
	Origins []int

	// Referable holds, parallel to Columns, whether a column a star stands for can be named
	// in place of the star, as ScopeChain.StarReferable says; it is false for a column of
	// any other item.
	Referable []bool

	// Problems holds the problems found, each placed where the dialect located it or, when
	// it did not, at the expression of the item it belongs to.
	Problems []*Error

	// NamedItems lists, in order, the items a later item of the select list names by their
	// alias, where the dialect has lateral column aliases.
	NamedItems []int

	// ElementsKnown holds, parallel to Columns, whether the nullability a column's type
	// gives its elements is what the query returns, as for a column a call such as array_agg
	// over a column that is never NULL gives, rather than what the type declares.
	ElementsKnown []bool

	// DataModifying reports an item that calls a function that modifies data.
	DataModifying bool

	// Aggregated reports an item that holds a window function or a call of an aggregate
	// function outside any subquery, so the query returns rows other than those it reads.
	Aggregated bool
}

// ResolveProjection resolves output columns and tracks their source select items.
//
// It also identifies aggregate items and locates errors, including values the dialect's
// drivers cannot read. Resolution stops, with nothing reported for the item it stopped
// at, when the context is cancelled or the dialect's resolver is interrupted.
//
// Takes rawColumns ([]ast.RawOutputColumn) which specifies the select list.
// Takes itemSpans ([]Span) which holds, parallel to rawColumns, where each item's
// expression is written, in the same terms as a problem's span; it may be shorter, or
// nil, when the dialect did not record the items.
// Takes scope (*ScopeChain) which provides visible tables and columns.
//
// Returns Projection which holds the columns, their origins and the problems found.
func (r *TypeResolver) ResolveProjection(
	ctx context.Context,
	rawColumns []ast.RawOutputColumn,
	itemSpans []Span,
	scope *ScopeChain,
) Projection {
	projection := r.resolveProjection(ctx, rawColumns, scope, func(problem *Error, raw ast.RawOutputColumn, index int) *Error {
		return locateItemProblem(problem, raw, itemSpanAt(itemSpans, index))
	})
	projection.Problems = append(projection.Problems, r.unreadableColumns(&projection, itemSpans)...)
	return projection
}

// resolveProjection resolves select items and reports their problems at the supplied
// locations.
//
// It also handles lateral aliases and the nullability of grouping keys omitted from
// subtotal rows.
//
// Takes rawColumns ([]ast.RawOutputColumn) which specifies the select list.
// Takes scope (*ScopeChain) which provides visible tables and columns.
// Takes place (func(*Error, ast.RawOutputColumn, int) *Error) which returns a problem
// found in the item at an index as it is to be reported.
//
// Returns Projection which holds the columns, their origins, the items named by alias and
// the problems found.
func (r *TypeResolver) resolveProjection(
	ctx context.Context,
	rawColumns []ast.RawOutputColumn,
	scope *ScopeChain,
	place func(problem *Error, raw ast.RawOutputColumn, index int) *Error,
) Projection {
	ctx, end := observe.From(ctx).Start(ctx, observe.StageResolveColumns)
	defer end(nil)

	aliases := scope.openSelectList()
	if scope != nil {
		scope.aliasOperands = nil
	}
	enclosingCalls := r.resolvedCalls
	r.resolvedCalls = make(map[*ast.FunctionCallExpression]resolvedCall)
	deactivate := scope.activateGroupingKeys()
	projection := r.resolveItems(ctx, rawColumns, scope, place, aliases)
	deactivate()
	projection.NamedItems = scope.closeSelectList(aliases)
	scope.nullGroupedAliases(rawColumns, &projection)
	r.judgeUngroupedColumns(rawColumns, scope, &projection, place)
	projection.ElementsKnown = r.knownElements(rawColumns, &projection)
	r.resolvedCalls = enclosingCalls
	return projection
}

// resolveItems resolves the items of a select list in order, sharing each with the later
// items through the select list's aliases.
//
// Takes rawColumns ([]ast.RawOutputColumn) which specifies the select list.
// Takes scope (*ScopeChain) which provides visible tables and columns.
// Takes place (func(*Error, ast.RawOutputColumn, int) *Error) which returns a problem
// found in the item at an index as it is to be reported.
// Takes aliases (*selectListAliases) which shares the items, or is nil where the dialect
// has no lateral column aliases.
//
// Returns Projection which holds the columns, their origins and the problems found.
func (r *TypeResolver) resolveItems(
	ctx context.Context,
	rawColumns []ast.RawOutputColumn,
	scope *ScopeChain,
	place func(problem *Error, raw ast.RawOutputColumn, index int) *Error,
	aliases *selectListAliases,
) Projection {
	projection := Projection{Columns: nil, Origins: nil, Referable: nil, Problems: nil, NamedItems: nil, ElementsKnown: nil,
		DataModifying: false, Aggregated: false}
	r.notices = nil
	for index, raw := range rawColumns {
		if ctx.Err() != nil {
			return projection
		}
		columns, problem, err := r.resolveSingleOutputColumn(ctx, raw, scope, &projection.DataModifying)
		if err != nil {
			return projection
		}
		if givesAlias(raw, scope) && len(columns) == 1 {
			aliases.share(index, raw.Name, &columns[0])
			scope.addAliasOperand(index, raw.Name, &columns[0])
		}
		if problem != nil {
			projection.Problems = append(projection.Problems, place(problem, raw, index))
		}
		for _, notice := range r.takeNotices() {
			projection.Problems = append(projection.Problems, place(notice, raw, index))
		}
		projection.Columns = append(projection.Columns, columns...)
		projection.Referable = append(projection.Referable, itemReferable(raw, scope, len(columns))...)
		for range columns {
			projection.Origins = append(projection.Origins, index)
		}
		projection.Aggregated = projection.Aggregated || r.holdsAggregate(ctx, raw.Expression)
	}
	return projection
}

// holdsAggregate reports whether an expression holds a window function or a call of an
// aggregate function anywhere outside a subquery.
//
// Takes expression (ast.Expression) which is the expression to search.
//
// Returns bool which is true when the expression aggregates.
func (r *TypeResolver) holdsAggregate(ctx context.Context, expression ast.Expression) bool {
	for node := range preorder(expression) {
		switch typed := node.(type) {
		case *ast.WindowFunctionExpression:
			return true
		case *ast.FunctionCallExpression:
			if typed != nil && r.functionResolver.isAggregate(ctx, typed.FunctionName, typed.Schema) {
				return true
			}
		}
	}
	return false
}

// knownElements returns, parallel to a projection's columns, whether the nullability a
// column's type gives its elements is what the query returns: true for the column of an
// item that is a call, or a window function's call, whose overload says so.
//
// Takes rawColumns ([]ast.RawOutputColumn) which is the select list.
// Takes projection (*Projection) which holds the resolved columns and their origins.
//
// Returns []bool which holds a value per column.
func (r *TypeResolver) knownElements(rawColumns []ast.RawOutputColumn, projection *Projection) []bool {
	known := make([]bool, len(projection.Columns))
	for column := range known {
		if column >= len(projection.Origins) || projection.Origins[column] >= len(rawColumns) {
			continue
		}
		expression := rawColumns[projection.Origins[column]].Expression
		if window, isWindow := expression.(*ast.WindowFunctionExpression); isWindow && window != nil {
			expression = window.Function
		}
		if call, isCall := expression.(*ast.FunctionCallExpression); isCall && call != nil {
			known[column] = r.resolvedCalls[call].elementsKnown
		}
	}
	return known
}

// isAggregate reports whether a function a call can reach is an aggregate: one of the
// catalogue's overloads of the name, or, for a name the catalogue does not hold, what the
// dialect's own resolver makes of it.
//
// A resolver that panics or is interrupted makes nothing of it here; typing the call
// reports the panic, and the analysis stops on the interruption.
//
// Takes name (string) which is the function's name.
// Takes schema (string) which is the call's schema qualifier, or empty.
//
// Returns bool which is true for an aggregate.
func (r *FunctionResolver) isAggregate(ctx context.Context, name string, schema string) bool {
	if name == "" {
		return false
	}
	candidates := r.visibleCandidates(name, schema)
	if len(candidates) == 0 {
		match, _, err := r.tryEngineResolver(ctx, name, schema, nil)
		return err == nil && match != nil && match.isAggregate
	}
	for _, candidate := range candidates {
		if candidate.IsAggregate {
			return true
		}
	}
	return false
}

// itemReferable returns, for each column an item resolved to, whether it can be named in
// place of the item: what ScopeChain.StarReferable says for a star whose columns it
// accounts for, and false otherwise.
//
// Takes raw (ast.RawOutputColumn) which is the item.
// Takes scope (*ScopeChain) which holds the relations the star expands; nil gives none.
// Takes count (int) which is how many columns the item resolved to.
//
// Returns []bool which holds count values.
func itemReferable(raw ast.RawOutputColumn, scope *ScopeChain, count int) []bool {
	if raw.IsStar && scope != nil {
		if referable := scope.StarReferable(raw.TableAlias); len(referable) == count {
			return referable
		}
	}
	return make([]bool, count)
}

// givesAlias reports whether a select item gives its column a name a later item could
// name it by: any item but a star and a bare reference to a column under the column's own
// name, which a later item naming it reads from the relations anyway.
//
// Takes raw (ast.RawOutputColumn) which is the item.
// Takes scope (*ScopeChain) which supplies the identifier rules; nil gives none.
//
// Returns bool which is true when the item gives an alias.
func givesAlias(raw ast.RawOutputColumn, scope *ScopeChain) bool {
	if raw.IsStar || raw.Name == "" || scope == nil {
		return false
	}
	return raw.ColumnName == "" || !scope.rules.Identifiers.Equal(raw.Name, raw.ColumnName)
}

// itemSpanAt returns the span of the item at an index, or the zero span when none was
// recorded for it.
//
// Takes itemSpans ([]Span) which holds the recorded spans.
// Takes index (int) which is the item's index.
//
// Returns Span which is the item's span.
func itemSpanAt(itemSpans []Span, index int) Span {
	if index < len(itemSpans) {
		return itemSpans[index]
	}
	return Span{Start: 0, End: 0}
}

// locateItemProblem places a problem the dialect did not locate: at the column an item
// that is an unknown column reference names, or the function an item that calls an
// unknown function names, when the dialect recorded where it is written, and otherwise at
// the item's expression.
//
// Takes problem (*Error) which is the problem; it is copied, never changed.
// Takes raw (ast.RawOutputColumn) which is the item.
// Takes itemSpan (Span) which is where the item's expression is written, or zero.
//
// Returns *Error which is the problem, placed where it could be.
func locateItemProblem(problem *Error, raw ast.RawOutputColumn, itemSpan Span) *Error {
	if problem.Span.Known() {
		return problem
	}
	span := itemSpan
	if named, found := namedSpan(problem.Code, raw.Expression); found {
		span = named
	}
	if !span.Known() {
		return problem
	}
	located := *problem
	located.Span = span
	return &located
}

// namedSpan returns where an expression that is itself the subject of a problem is
// written: a column reference for an unknown column, a function call or a window function
// for an unknown function.
//
// Takes code (string) which is the problem's code.
// Takes expression (ast.Expression) which is the item's expression.
//
// Returns Span which is where the reference or the function's name is written.
// Returns bool which is true when the expression is the subject and its span is known.
func namedSpan(code string, expression ast.Expression) (Span, bool) {
	var span ast.Span
	switch typed := expression.(type) {
	case *ast.ColumnRefExpression:
		if typed != nil && code == diag.CodeUnknownColumn {
			span = typed.Span
		}
	case *ast.FunctionCallExpression:
		if typed != nil && code == diag.CodeUnknownFunction {
			span = typed.Span
		}
	case *ast.WindowFunctionExpression:
		if typed != nil && typed.Function != nil && code == diag.CodeUnknownFunction {
			span = typed.Function.Span
		}
	}
	return Span{Start: span.Start, End: span.End}, span.Known()
}

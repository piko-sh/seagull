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
	"fmt"
	"slices"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// ScopeBuilder populates scope chains from raw query analyses. It is the one place that
// turns the relations of a parsed query (CTEs, FROM and JOIN tables, table-valued
// functions, derived tables, ARRAY JOIN aliases and compound branches) into scope
// entries.
//
// The builder serves two callers with one set of primitives (relation lookup, CTE
// reference matching, table-valued function registration and column scoping):
//
//   - The reporting path (BuildQueryScope and the methods it calls) backs top-level query
//     analysis and view and function-body replay. Every relation that cannot be resolved
//     produces a diagnostic.
//   - The silent path (addRawTablesToScope and the methods it calls) backs the type
//     resolver's inner-subquery scopes. It skips anything it cannot resolve, because the
//     top-level pass owns those diagnostics.
//
// Both paths take the nesting depth of a query from the scope it is resolved in, which is
// one deeper than its enclosing query's, so every walk into a nested query, whichever
// path or caller starts it, spends the same budget of MaxExpressionResolveDepth levels.
//
// A ScopeBuilder is not safe for concurrent use: it shares its TypeResolver.
type ScopeBuilder struct {
	// engine holds the dialect used for type normalisation, promotion and table-valued
	// function schemas. It is nil when the builder was derived from a type resolver whose
	// engine only implements the type-system port, in which case table-valued functions in
	// inner subqueries are skipped.
	engine dialect.DialectPort

	// catalogue holds the schema catalogue that relations are resolved against.
	catalogue *sqlmodel.Catalogue

	// TypeResolver holds the resolver used to type CTE, derived-table and compound-branch
	// projections.
	TypeResolver *TypeResolver
}

// NewScopeBuilder creates a scope builder over the given engine, catalogue and type
// resolver.
//
// Takes engine (dialect.DialectPort) which supplies type rules and table-valued function
// schemas.
// Takes catalogue (*sqlmodel.Catalogue) which holds the relations to resolve against.
// Takes typeResolver (*TypeResolver) which types the projections the builder registers.
//
// Returns *ScopeBuilder which is ready to populate scope chains.
func NewScopeBuilder(engine dialect.DialectPort, catalogue *sqlmodel.Catalogue, typeResolver *TypeResolver) *ScopeBuilder {
	return &ScopeBuilder{
		engine:       engine,
		catalogue:    catalogue,
		TypeResolver: typeResolver,
	}
}

// NewCatalogueScopeBuilder creates a scope builder with its own function and type
// resolvers, for replaying a view definition or a function body against the catalogue
// outside query analysis.
//
// Takes engine (dialect.DialectPort) which supplies the built-in functions and type
// rules.
// Takes catalogue (*sqlmodel.Catalogue) which holds the relations to resolve against.
//
// Returns *ScopeBuilder which is ready to populate scope chains.
func NewCatalogueScopeBuilder(engine dialect.DialectPort, catalogue *sqlmodel.Catalogue) *ScopeBuilder {
	functionResolver := NewFunctionResolver(engine.BuiltinFunctions(), catalogue, engine)
	return NewScopeBuilder(engine, catalogue, NewTypeResolver(catalogue, functionResolver, engine))
}

// BuildQueryScope registers the relations of a query in scope, in the order the analyser
// relies on: WITH-clause CTEs, FROM and JOIN tables (with parser-resolved derived
// tables), table-valued functions, then subquery derived tables.
//
// It then records the keys the query's grouping modifier leaves out of some rows, which
// its select list reads as NULL. The query's nesting depth is the scope's: a scope
// created as a child of an enclosing query's continues that query's depth budget.
//
// Takes raw (*ast.RawQueryAnalysis) which holds the parsed query.
// Takes scope (*ScopeChain) which receives the relations.
//
// Returns []*Error which holds the diagnostics of every phase, in phase order.
func (b *ScopeBuilder) BuildQueryScope(
	ctx context.Context,
	raw *ast.RawQueryAnalysis,
	scope *ScopeChain,
) []*Error {
	return b.buildQueryScope(ctx, raw, scope)
}

// ResolveCompoundBranches resolves UNION, INTERSECT, and EXCEPT branches and promotes
// types to match the primary SELECT.
//
// Takes branches ([]ast.RawCompoundBranch) which holds the parsed compound branches.
// Takes primaryColumns ([]sqlmodel.OutputColumn) which holds the primary SELECT output
// columns whose types are promoted in place.
// Takes outerScope (*ScopeChain) which holds the enclosing scope; each branch's scope is
// a child of it that sees the CTEs of every enclosing WITH clause but none of its
// relations. May be nil when no outer CTEs apply.
//
// Returns []*Error which holds any diagnostics produced during branch resolution.
func (b *ScopeBuilder) ResolveCompoundBranches(
	ctx context.Context,
	branches []ast.RawCompoundBranch,
	primaryColumns []sqlmodel.OutputColumn,
	outerScope *ScopeChain,
) []*Error {
	if len(branches) > 0 && outerScope.nestedTooDeeply() {
		return []*Error{compoundDepthLimit()}
	}
	return b.resolveCompoundBranches(ctx, branches, primaryColumns, outerScope, "")
}

// buildQueryScope registers the relations of a query at the nesting depth of its scope,
// the top-level query at zero, and a derived table, CTE body or compound branch one
// deeper than the query that holds it, and then the keys its grouping modifier leaves out
// of some rows.
//
// Takes raw (*ast.RawQueryAnalysis) which holds the parsed query.
// Takes scope (*ScopeChain) which receives the relations; its depth is bounded by
// MaxExpressionResolveDepth.
//
// Returns []*Error which holds the diagnostics of every phase, in phase order.
func (b *ScopeBuilder) buildQueryScope(
	ctx context.Context,
	raw *ast.RawQueryAnalysis,
	scope *ScopeChain,
) []*Error {
	diagnostics := b.resolveCTEs(ctx, raw.CTEDefinitions, scope)
	scope.SetFromOrder(raw.FromOrder)
	diagnostics = append(diagnostics, b.buildScopeChain(raw, scope)...)
	diagnostics = append(diagnostics, b.resolveTableValuedFunctions(ctx, raw.RawTableValuedFunctions, scope)...)
	if !scope.nestedTooDeeply() {
		diagnostics = append(diagnostics, b.resolveRawDerivedTables(ctx, raw.RawDerivedTables, scope)...)
	} else if len(raw.RawDerivedTables) > 0 {
		diagnostics = append(diagnostics, depthLimitError("derived tables nested more than %d levels deep were not analysed",
			MaxExpressionResolveDepth))
	}
	scope.registerGroupingKeys(raw.GroupingSets)
	scope.registerGrouping(raw)
	return diagnostics
}

// resolveNestedQuery builds the scope of a query nested in another, a derived table or a
// compound branch, and types its projection the way the top-level analysis types the
// whole query: its CTEs, relations, derived tables, ARRAY JOIN aliases and compound
// branches all take part.
//
// Takes raw (*ast.RawQueryAnalysis) which holds the nested query.
// Takes scope (*ScopeChain) which receives the nested query's relations, at its nesting
// depth.
//
// Returns []sqlmodel.OutputColumn which holds the nested query's typed projection.
// Returns []*Error which holds the diagnostics of resolving it.
func (b *ScopeBuilder) resolveNestedQuery(
	ctx context.Context,
	raw *ast.RawQueryAnalysis,
	scope *ScopeChain,
) ([]sqlmodel.OutputColumn, []*Error) {
	columns, diagnostics := b.resolveNestedProjection(ctx, raw, scope)
	diagnostics = append(diagnostics, b.resolveNestedBranches(ctx, raw, columns, scope, "")...)
	return columns, diagnostics
}

// resolveNestedProjection builds the scope of a nested query and types its select list,
// leaving its compound branches to resolveNestedBranches.
//
// Takes raw (*ast.RawQueryAnalysis) which holds the nested query.
// Takes scope (*ScopeChain) which receives the nested query's relations, at its nesting
// depth.
//
// Returns []sqlmodel.OutputColumn which holds the typed select list.
// Returns []*Error which holds the diagnostics of resolving it.
func (b *ScopeBuilder) resolveNestedProjection(
	ctx context.Context,
	raw *ast.RawQueryAnalysis,
	scope *ScopeChain,
) ([]sqlmodel.OutputColumn, []*Error) {
	diagnostics := b.buildQueryScope(ctx, raw, scope)
	diagnostics = append(diagnostics, b.ResolveArrayJoinClauses(ctx, raw.ArrayJoinClauses, scope)...)
	columns, _, columnDiagnostics := b.TypeResolver.ResolveOutputColumns(ctx, raw.OutputColumns, scope)
	return columns, append(diagnostics, columnDiagnostics...)
}

// resolveNestedBranches resolves the compound branches of a nested query into the columns
// of its select list, within the depth bound.
//
// Takes raw (*ast.RawQueryAnalysis) which holds the nested query.
// Takes columns ([]sqlmodel.OutputColumn) which holds its typed select list, updated in
// place.
// Takes scope (*ScopeChain) which is the nested query's scope, which each branch's scope
// is a child of.
// Takes recursiveName (string) which is the name of the recursive CTE the branches
// define, or empty.
//
// Returns []*Error which holds the diagnostics of resolving the branches.
func (b *ScopeBuilder) resolveNestedBranches(
	ctx context.Context,
	raw *ast.RawQueryAnalysis,
	columns []sqlmodel.OutputColumn,
	scope *ScopeChain,
	recursiveName string,
) []*Error {
	if len(raw.CompoundBranches) == 0 {
		return nil
	}
	if scope.nestedTooDeeply() {
		return []*Error{compoundDepthLimit()}
	}
	return b.resolveCompoundBranches(ctx, raw.CompoundBranches, columns, scope, recursiveName)
}

// resolveCompoundBranches is ResolveCompoundBranches once the branches are known to fit
// the depth budget: each branch is resolved in a child of outerScope, one level deeper.
//
// Takes branches ([]ast.RawCompoundBranch) which holds the parsed compound branches.
// Takes primaryColumns ([]sqlmodel.OutputColumn) which holds the primary SELECT output
// columns whose types are promoted in place.
// Takes outerScope (*ScopeChain) which holds the enclosing scope whose CTEs each branch
// sees, or nil.
// Takes recursiveName (string) which is the name of the recursive CTE the branches
// define, or empty. A branch that reads the CTE is a recursive member: like the database,
// the analyser keeps the column types the anchor gives and lets the member add only
// nullability.
//
// Returns []*Error which holds any diagnostics produced during branch resolution.
func (b *ScopeBuilder) resolveCompoundBranches(
	ctx context.Context,
	branches []ast.RawCompoundBranch,
	primaryColumns []sqlmodel.OutputColumn,
	outerScope *ScopeChain,
	recursiveName string,
) []*Error {
	var diagnostics []*Error

	for _, branch := range branches {
		if ctx.Err() != nil {
			return diagnostics
		}
		if branch.Query == nil {
			diagnostics = append(diagnostics, newError(diag.CodeInternalNilGuard, "nil compound branch query during type resolution"))
			continue
		}

		branchScope := NewRootScopeChain(ScopeKindQuery, b.TypeResolver.scopeRules)
		if outerScope != nil {
			branchScope = outerScope.CreateChildScope(ScopeKindQuery)
		}

		branchColumns, branchDiagnostics := b.resolveNestedQuery(ctx, branch.Query, branchScope)
		diagnostics = append(diagnostics, branchDiagnostics...)
		recursiveMember := recursiveName != "" && readsRelation(branch.Query, recursiveName, branchScope)

		if len(branchColumns) != len(primaryColumns) {
			if len(branchDiagnostics) == 0 {
				diagnostics = append(diagnostics, newError(diag.CodeCompoundColumnCount,
					"compound query branch has %d columns, expected %d to match primary SELECT",
					len(branchColumns), len(primaryColumns)).withSeverity(diag.SeverityError))
			}
			continue
		}
		mergeBranchNullability(primaryColumns, branchColumns)
		if !recursiveMember {
			b.promoteBranchTypes(primaryColumns, branchColumns)
		}
	}

	return diagnostics
}

// promoteBranchTypes widens each primary column's type to hold the matching branch
// column's, or takes the branch's type where the primary's is unknown. A recursive member
// is never promoted from, so the anchor keeps its types, an unknown one included.
//
// Takes primaryColumns ([]sqlmodel.OutputColumn) which holds the primary columns, updated
// in place.
// Takes branchColumns ([]sqlmodel.OutputColumn) which holds the branch's columns, as many
// as the primary's.
func (b *ScopeBuilder) promoteBranchTypes(primaryColumns, branchColumns []sqlmodel.OutputColumn) {
	for columnIndex := range primaryColumns {
		if primaryColumns[columnIndex].SQLType.Category == sqlmodel.TypeCategoryUnknown {
			primaryColumns[columnIndex].SQLType = branchColumns[columnIndex].SQLType
			continue
		}
		primaryColumns[columnIndex].SQLType = b.TypeResolver.engine.PromoteType(
			primaryColumns[columnIndex].SQLType,
			branchColumns[columnIndex].SQLType,
		)
	}
}

// buildScopeChain populates the scope chain from a raw query analysis result.
//
// FROM tables and JOIN tables are added with correct nullability adjustments. FROM
// entries that match previously resolved CTEs are registered as CTE aliases rather than
// resolved against the catalogue.
//
// Takes raw (*ast.RawQueryAnalysis) which holds the parsed FROM, JOIN, and derived table
// references.
// Takes scope (*ScopeChain) which holds the scope chain to populate.
//
// Returns []*Error which holds any warnings produced when tables cannot be resolved.
func (b *ScopeBuilder) buildScopeChain(
	raw *ast.RawQueryAnalysis,
	scope *ScopeChain,
) []*Error {
	var diagnostics []*Error

	for _, tableReference := range raw.FromTables {
		if cte, alias, ok := cteReference(tableReference, scope); ok {
			if !scope.Rules().Identifiers.Equal(alias, tableReference.Name) {
				scope.AddCTE(alias, cte.columns)
			}
			scope.AddCTEAsTable(alias, cte.columns, ast.JoinInner)
			continue
		}
		if diagnostic := b.addRelation(tableReference, ast.JoinInner, scope); diagnostic != nil {
			diagnostics = append(diagnostics, diagnostic)
		}
	}

	diagnostics = append(diagnostics, b.resolveJoinClauses(raw.JoinClauses, scope)...)

	return diagnostics
}

// resolveJoinClauses adds each join clause to the scope chain, resolving CTE references
// or catalogue tables.
//
// Takes joinClauses ([]ast.JoinClause) which holds the parsed JOIN clauses from the
// query.
// Takes scope (*ScopeChain) which holds the scope chain to populate with join table
// entries.
//
// Returns []*Error which holds any warnings produced when join tables cannot be resolved.
func (b *ScopeBuilder) resolveJoinClauses(
	joinClauses []ast.JoinClause,
	scope *ScopeChain,
) []*Error {
	var diagnostics []*Error

	for _, joinClause := range joinClauses {
		if cte, alias, ok := cteReference(joinClause.Table, scope); ok {
			scope.AddCTE(alias, cte.columns)
			scope.AddCTEAsTable(alias, cte.columns, joinClause.Kind)
			scope.joinUsing(alias, joinClause.Using)
			continue
		}
		if diagnostic := b.addRelation(joinClause.Table, joinClause.Kind, scope); diagnostic != nil {
			diagnostics = append(diagnostics, diagnostic)
			continue
		}
		scope.joinUsing(relationAlias(joinClause.Table), joinClause.Using)
	}

	return diagnostics
}

// resolveCTEs resolves each CTE definition in order and registers the results in the
// scope chain.
//
// Takes cteDefinitions ([]ast.RawCTEDefinition) which holds the parsed CTE definitions
// from the query.
// Takes scope (*ScopeChain) which holds the scope chain where resolved CTEs are
// registered, at the nesting depth of the query the WITH clause heads.
//
// Returns []*Error which holds any diagnostics produced during CTE resolution.
func (b *ScopeBuilder) resolveCTEs(
	ctx context.Context,
	cteDefinitions []ast.RawCTEDefinition,
	scope *ScopeChain,
) []*Error {
	if len(cteDefinitions) > 0 && scope.nestedTooDeeply() {
		return []*Error{depthLimitError("common table expressions nested more than %d levels deep were not analysed",
			MaxExpressionResolveDepth)}
	}
	diagnostics := make([]*Error, 0, len(cteDefinitions))

	for index := range cteDefinitions {
		if ctx.Err() != nil {
			return diagnostics
		}
		diagnostics = append(diagnostics, b.resolveSingleCTE(ctx, &cteDefinitions[index], scope)...)
	}

	return diagnostics
}

// resolveSingleCTE resolves one CTE definition by building a child scope, resolving its
// body the way any nested query is resolved, naming its columns from the CTE's column
// list, and registering it.
//
// The child scope sees the CTEs of every enclosing WITH clause, including those declared
// before this one, but none of the enclosing query's relations. A recursive CTE is
// registered with the columns its anchor gives before its recursive member is resolved.
//
// Takes cteDefinition (*ast.RawCTEDefinition) which holds the parsed CTE definition to
// resolve.
// Takes scope (*ScopeChain) which holds the parent scope where the CTE will be
// registered.
//
// Returns []*Error which holds any diagnostics produced during CTE resolution.
func (b *ScopeBuilder) resolveSingleCTE(
	ctx context.Context,
	cteDefinition *ast.RawCTEDefinition,
	scope *ScopeChain,
) []*Error {
	cteScope := scope.CreateChildScope(ScopeKindCTE)
	cteScope.groupingWithheld = cteDefinition.Query == nil
	body := cteBody(cteDefinition)

	cteColumns, diagnostics := b.resolveNestedProjection(ctx, body, cteScope)
	scoped := outputColumnsToScoped(cteColumns)
	if problem := nameColumns(scoped, cteDefinition.ColumnNames, "CTE", cteDefinition.Name); problem != nil {
		diagnostics = append(diagnostics, problem)
	}
	for index := range scoped {
		cteColumns[index].Name = scoped[index].Name
	}

	recursiveName := ""
	if cteDefinition.IsRecursive && len(body.CompoundBranches) > 0 {
		scope.AddCTE(cteDefinition.Name, scoped)
		recursiveName = cteDefinition.Name
	}
	diagnostics = append(diagnostics, b.resolveNestedBranches(ctx, body, cteColumns, cteScope, recursiveName)...)

	scope.AddCTE(cteDefinition.Name, outputColumnsToScoped(cteColumns))

	return diagnostics
}

// resolveTableValuedFunctions resolves table-valued function references and adds them to
// the scope as derived tables.
//
// Takes tableValuedFunctions ([]ast.RawTableValuedFunctionReference) which holds the
// parsed function references.
// Takes scope (*ScopeChain) which holds the scope chain to populate with derived table
// entries.
//
// Returns []*Error which holds any warnings for unresolvable function references. An
// interruption that stops the engine ends the walk with nothing reported for the function
// it stopped on.
func (b *ScopeBuilder) resolveTableValuedFunctions(
	ctx context.Context,
	tableValuedFunctions []ast.RawTableValuedFunctionReference,
	scope *ScopeChain,
) []*Error {
	var diagnostics []*Error
	for index := range tableValuedFunctions {
		diagnostic, err := b.addTableValuedFunction(ctx, tableValuedFunctions[index], scope)
		if err != nil {
			return diagnostics
		}
		if diagnostic != nil {
			diagnostics = append(diagnostics, diagnostic)
		}
	}
	return diagnostics
}

// resolveRawDerivedTables resolves subquery-based derived tables by recursively analysing
// each inner query, including the derived tables it holds in turn.
//
// Each inner query's scope is a child of scope, so it sees the CTEs of every enclosing
// WITH clause and, as a LATERAL subquery does, the relations registered before it.
//
// Takes rawDerivedTables ([]ast.RawDerivedTableReference) which holds the raw derived
// table references.
// Takes scope (*ScopeChain) which holds the scope chain to populate with resolved derived
// tables, at the nesting depth of the query holding them.
//
// Returns []*Error which holds any diagnostics produced during derived table resolution.
func (b *ScopeBuilder) resolveRawDerivedTables(
	ctx context.Context,
	rawDerivedTables []ast.RawDerivedTableReference,
	scope *ScopeChain,
) []*Error {
	var diagnostics []*Error

	for _, rawDerived := range rawDerivedTables {
		if ctx.Err() != nil {
			return diagnostics
		}
		if rawDerived.InnerQuery == nil {
			diagnostics = append(diagnostics, newError(diag.CodeInternalNilGuard, "nil derived table query during type resolution"))
			continue
		}

		innerScope := scope.CreateChildScope(ScopeKindSubquery)
		innerColumns, innerDiagnostics := b.resolveNestedQuery(ctx, rawDerived.InnerQuery, innerScope)
		diagnostics = append(diagnostics, innerDiagnostics...)

		columns := outputColumnsToScoped(innerColumns)
		if problem := nameColumns(columns, rawDerived.ColumnNames, "derived table", rawDerived.Alias); problem != nil {
			diagnostics = append(diagnostics, problem)
		}
		scope.AddDerivedTable(DerivedTableReference{
			Alias:    rawDerived.Alias,
			Columns:  columns,
			JoinKind: rawDerived.JoinKind,
			Source:   DerivedSourceSubquery,
			Using:    rawDerived.Using,
		})
	}

	return diagnostics
}

// addRawTablesToScope registers the FROM, JOIN, FROM-clause derived-table, and
// table-valued-function relations of an inner subquery into the given scope, silently.
//
// A relation may be a base table or a view; a view is folded into a synthetic table
// carrying its resolved columns (see resolveTableReference) so a subquery whose FROM or
// JOIN names a view still has that view's columns and alias in scope. A FROM-clause
// derived table (a parenthesised SELECT, including an unaliased ROW_NUMBER() OVER table)
// has its projected columns resolved against its own inner scope and registered, so a
// column or parameter in an EXISTS, scalar, derived, or predicate subquery whose FROM is
// itself a derived table resolves against those projected columns.
//
// Takes raw (*ast.RawQueryAnalysis) which holds the subquery's table references.
// Takes scope (*ScopeChain) which holds the scope chain to populate; its depth, which
// continues the depth of the query the subquery is nested in, bounds the derived tables
// and CTEs it holds in turn by MaxExpressionResolveDepth.
//
// Nothing more is registered once the context is done. Derived tables nested past the
// limit are not registered, and the limit is recorded for TypeResolver.TakeDepthLimits.
func (b *ScopeBuilder) addRawTablesToScope(ctx context.Context, raw *ast.RawQueryAnalysis, scope *ScopeChain) {
	b.addRawCTEsToScope(ctx, raw.CTEDefinitions, scope)
	scope.SetFromOrder(raw.FromOrder)
	b.addSilentRelations(raw.FromTables, raw.JoinClauses, scope)

	b.addRawTableValuedFunctionsToScope(ctx, raw.RawTableValuedFunctions, scope)
	if scope.nestedTooDeeply() {
		if len(raw.RawDerivedTables) > 0 {
			b.noteSilentDepthLimit()
		}
		return
	}
	for index := range raw.RawDerivedTables {
		if ctx.Err() != nil {
			return
		}
		b.addRawDerivedTableToScope(ctx, &raw.RawDerivedTables[index], scope)
	}
}

// addRawCTEsToScope resolves each WITH-clause CTE definition of an inner subquery and
// registers it in the given scope.
//
// Takes cteDefinitions ([]ast.RawCTEDefinition) which holds the parsed CTE definitions of
// the subquery.
// Takes scope (*ScopeChain) which receives the resolved CTEs; past
// MaxExpressionResolveDepth levels of nesting no CTE is registered and the limit is
// recorded for TypeResolver.TakeDepthLimits.
func (b *ScopeBuilder) addRawCTEsToScope(
	ctx context.Context,
	cteDefinitions []ast.RawCTEDefinition,
	scope *ScopeChain,
) {
	if scope.nestedTooDeeply() {
		if len(cteDefinitions) > 0 {
			b.noteSilentDepthLimit()
		}
		return
	}
	for index := range cteDefinitions {
		if ctx.Err() != nil {
			return
		}
		b.addSingleCTEToScope(ctx, &cteDefinitions[index], scope)
	}
}

// noteSilentDepthLimit records that the silent path left relations nested past
// MaxExpressionResolveDepth out of a scope.
func (b *ScopeBuilder) noteSilentDepthLimit() {
	if b.TypeResolver == nil {
		return
	}
	b.TypeResolver.noteDepthLimit(depthLimitError(
		"subqueries nest relations more than %d levels deep, so the relations beyond the limit were not followed and their columns are unknown",
		MaxExpressionResolveDepth))
}

// addSingleCTEToScope resolves one CTE definition's projected columns, names them from
// the CTE's column list and registers them under the CTE's name via AddCTE.
//
// The columns are resolved against an inner scope built from the CTE body's own
// relations, its WITH clause and derived tables included, chained to scope so an earlier
// CTE in the same WITH clause remains visible. Columns that fail to resolve are kept as
// nullable columns of unknown type.
//
// Takes cteDefinition (*ast.RawCTEDefinition) which holds the parsed CTE definition.
// Takes scope (*ScopeChain) which receives the resolved CTE.
func (b *ScopeBuilder) addSingleCTEToScope(
	ctx context.Context,
	cteDefinition *ast.RawCTEDefinition,
	scope *ScopeChain,
) {
	innerScope := NewScopeChain(ScopeKindCTE, scope)
	body := cteBody(cteDefinition)
	b.addRawTablesToScope(ctx, body, innerScope)
	columns := b.resolveSilentColumns(ctx, body.OutputColumns, innerScope)
	_ = nameColumns(columns, cteDefinition.ColumnNames, "CTE", cteDefinition.Name)
	scope.AddCTE(cteDefinition.Name, columns)
}

// addSilentRelations registers FROM and JOIN references in scope without reporting
// anything: a reference naming a CTE registered directly in scope becomes that CTE under
// its alias, and any other reference that resolves against the catalogue is added as a
// table. References that resolve to neither are skipped.
//
// Takes fromTables ([]ast.TableReference) which holds the FROM references.
// Takes joinClauses ([]ast.JoinClause) which holds the JOIN clauses.
// Takes scope (*ScopeChain) which receives the relations.
func (b *ScopeBuilder) addSilentRelations(
	fromTables []ast.TableReference,
	joinClauses []ast.JoinClause,
	scope *ScopeChain,
) {
	for _, tableReference := range fromTables {
		b.addSilentRelation(tableReference, ast.JoinInner, scope)
	}
	for _, joinClause := range joinClauses {
		b.addSilentRelation(joinClause.Table, joinClause.Kind, scope)
		scope.joinUsing(relationAlias(joinClause.Table), joinClause.Using)
	}
}

// addSilentRelation registers one FROM or JOIN reference in scope without reporting
// anything, preferring a CTE registered directly in scope over the catalogue.
//
// Takes reference (ast.TableReference) which names the relation and its alias.
// Takes joinKind (ast.JoinKind) which sets the nullability adjustment.
// Takes scope (*ScopeChain) which receives the relation.
func (b *ScopeBuilder) addSilentRelation(reference ast.TableReference, joinKind ast.JoinKind, scope *ScopeChain) {
	if cte, alias, ok := cteReference(reference, scope); ok {
		scope.AddCTEAsTable(alias, cte.columns, joinKind)
		return
	}
	_ = b.addRelation(reference, joinKind, scope)
}

// addRawDerivedTableToScope resolves one FROM-clause derived table's projected columns
// and registers them under the derived table's alias.
//
// The columns are resolved against an inner scope built from the derived table's own
// relations (chained to scope so a LATERAL derived table can still reference the
// enclosing query). Columns that fail to resolve keep their place as nullable columns of
// unknown type.
//
// Takes rawDerived (*ast.RawDerivedTableReference) which holds the derived table.
// Takes scope (*ScopeChain) which receives the derived table.
func (b *ScopeBuilder) addRawDerivedTableToScope(
	ctx context.Context,
	rawDerived *ast.RawDerivedTableReference,
	scope *ScopeChain,
) {
	if rawDerived.InnerQuery == nil {
		return
	}
	innerScope := NewScopeChain(ScopeKindSubquery, scope)
	b.addRawTablesToScope(ctx, rawDerived.InnerQuery, innerScope)

	columns := b.resolveSilentColumns(ctx, rawDerived.InnerQuery.OutputColumns, innerScope)
	_ = nameColumns(columns, rawDerived.ColumnNames, "derived table", rawDerived.Alias)
	scope.AddDerivedTable(DerivedTableReference{
		Alias:    rawDerived.Alias,
		Columns:  columns,
		JoinKind: rawDerived.JoinKind,
		Source:   DerivedSourceSubquery,
		Using:    rawDerived.Using,
	})
}

// resolveSilentColumns types a projection against scope without reporting anything. A
// column that fails to resolve keeps its place as a nullable column of unknown type, so
// the relation still has every column the SQL projects.
//
// Takes rawColumns ([]ast.RawOutputColumn) which holds the projection to type.
// Takes scope (*ScopeChain) which provides the relations the projection reads.
//
// Returns []ast.ScopedColumn which holds the columns, or nil when there are none.
func (b *ScopeBuilder) resolveSilentColumns(ctx context.Context, rawColumns []ast.RawOutputColumn, scope *ScopeChain) []ast.ScopedColumn {
	var dataModifying bool
	var columns []ast.ScopedColumn
	enclosingNotices := b.TypeResolver.notices
	defer func() { b.TypeResolver.notices = enclosingNotices }()
	for columnIndex := range rawColumns {
		if ctx.Err() != nil {
			return columns
		}
		resolvedColumns, _, err := b.TypeResolver.resolveSingleOutputColumn(ctx, rawColumns[columnIndex], scope, &dataModifying)
		if err != nil {
			return columns
		}
		for resolvedIndex := range resolvedColumns {
			columns = append(columns, ast.ScopedColumn{
				Name:     resolvedColumns[resolvedIndex].Name,
				SQLType:  resolvedColumns[resolvedIndex].SQLType,
				Nullable: resolvedColumns[resolvedIndex].Nullable,
			})
		}
	}
	return columns
}

// addRawTableValuedFunctionsToScope registers an inner subquery's FROM-clause
// table-valued functions into the given scope.
//
// This covers functions such as json_each(payload) je or generate_series(1, 10) g, so a
// column or parameter inside an EXISTS, scalar, derived, or predicate subquery that
// references the function's alias resolves against the function's columns.
//
// The silent path drops the diagnostics addTableValuedFunction reports: a function whose
// columns cannot be resolved is skipped (as an unresolvable FROM relation is), and the
// alias-count-mismatch warning is discarded. The top-level resolveTableValuedFunctions
// owns table-valued function diagnostics; an unknown subquery function still surfaces
// downstream as its alias reference failing to resolve.
//
// Takes tableValuedFunctions ([]ast.RawTableValuedFunctionReference) which holds the
// inner query's parsed function references.
// Takes scope (*ScopeChain) which receives the function relations.
func (b *ScopeBuilder) addRawTableValuedFunctionsToScope(
	ctx context.Context,
	tableValuedFunctions []ast.RawTableValuedFunctionReference,
	scope *ScopeChain,
) {
	if len(tableValuedFunctions) == 0 || b.engine == nil {
		return
	}
	for index := range tableValuedFunctions {
		if _, err := b.addTableValuedFunction(ctx, tableValuedFunctions[index], scope); err != nil {
			return
		}
	}
}

// addTableValuedFunction resolves one table-valued function reference and, when its
// columns resolve, registers them in scope as a derived table under the call's alias. A
// function whose columns are known only when it runs, such as one reading a file, takes
// the columns the query declares for it (DeclareRuntimeColumns).
//
// Takes reference (ast.RawTableValuedFunctionReference) which holds the function
// reference.
// Takes scope (*ScopeChain) which receives the derived table.
//
// Returns *Error which is an unknown-function warning when the columns cannot be
// resolved, naming the call whose columns are not known, a Q070 error when the engine
// panicked resolving them, an alias-count warning when surplus column aliases were
// supplied, or nil.
// Returns error which is the interruption that stopped the engine, in which case nothing
// is registered.
func (b *ScopeBuilder) addTableValuedFunction(
	ctx context.Context,
	reference ast.RawTableValuedFunctionReference,
	scope *ScopeChain,
) (*Error, error) {
	columns, columnDiagnostic, err := resolveTableValuedFunctionColumns(ctx, b.engine, b.catalogue, reference,
		callTypers{arguments: b.argumentTyper(scope), scalarCall: b.scalarCallTyper(scope)})
	if err != nil {
		return nil, err
	}
	if columns == nil && columnDiagnostic != nil {
		return columnDiagnostic, nil
	}
	if columns == nil {
		return newError(diag.CodeUnknownTable, "unknown table-valued function %q", reference.FunctionName), nil
	}
	if len(columns) == 0 {
		columns = scope.takeDeclaredColumns()
	}
	scope.AddDerivedTable(DerivedTableReference{
		Alias:    reference.Alias,
		Columns:  columns,
		Source:   DerivedSourceTableFunction,
		JoinKind: reference.JoinKind,
		Using:    nil,
	})
	return columnDiagnostic, nil
}

// argumentTyper returns the argumentTyper for the calls of a FROM clause, which types
// each argument in the scope the clause's earlier relations are registered in, so an
// argument may read a column of one, as a LATERAL call does.
//
// A problem typing an argument leaves its type unknown and is not reported here; the
// notices of the expression being typed when the FROM clause is built are kept.
//
// Takes scope (*ScopeChain) which holds the relations registered so far.
//
// Returns argumentTyper which types the arguments.
func (b *ScopeBuilder) argumentTyper(scope *ScopeChain) argumentTyper {
	return func(ctx context.Context, arguments []ast.Expression) []sqlmodel.SQLType {
		if b.TypeResolver == nil {
			return nil
		}
		enclosing := b.TypeResolver.notices
		defer func() { b.TypeResolver.notices = enclosing }()
		types := make([]sqlmodel.SQLType, len(arguments))
		dataModifying := new(false)
		for index, argument := range arguments {
			argumentType, _, err := b.TypeResolver.resolveExpressionType(ctx, argument, scope, dataModifying)
			if err != nil {
				argumentType = sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
			}
			types[index] = argumentType
		}
		return types
	}
}

// scalarCallTyper returns the scalarCallTyper for the calls of a FROM clause, which types
// a call of a function returning one value in the scope the clause's earlier relations
// are registered in, as a LATERAL call is typed.
//
// A problem typing the call leaves it untyped and is not reported here; a panic the
// dialect's resolver recovered and an interruption that stopped it are returned for the
// FROM item to report or stop on. The notices of the expression being typed when the FROM
// clause is built are kept.
//
// Takes scope (*ScopeChain) which holds the relations registered so far.
//
// Returns scalarCallTyper which types the calls.
func (b *ScopeBuilder) scalarCallTyper(scope *ScopeChain) scalarCallTyper {
	return func(ctx context.Context, functionName string, arguments []ast.Expression) (ast.ScopedColumn, bool, error) {
		if b.TypeResolver == nil {
			return ast.ScopedColumn{}, false, nil
		}
		enclosing := b.TypeResolver.notices
		defer func() { b.TypeResolver.notices = enclosing }()
		return b.TypeResolver.resolveScalarCallColumn(ctx, functionName, arguments, scope)
	}
}

// addRelation resolves a FROM or JOIN reference against the catalogue and registers it in
// scope under joinKind.
//
// Takes reference (ast.TableReference) which names the relation and its alias.
// Takes joinKind (ast.JoinKind) which sets the nullability adjustment.
// Takes scope (*ScopeChain) which receives the relation.
//
// Returns *Error which is an unknown-table warning when the reference cannot be resolved,
// or nil once the relation is registered.
func (b *ScopeBuilder) addRelation(
	reference ast.TableReference,
	joinKind ast.JoinKind,
	scope *ScopeChain,
) *Error {
	catalogueTable, resolveError := b.resolveTableReference(reference)
	if resolveError != nil {
		return newError(diag.CodeUnknownTable, "%s", resolveError.Error())
	}
	scope.AddTable(reference, joinKind, catalogueTable)
	return nil
}

// resolveTableReference looks up a table, view or sequence in the catalogue by its
// reference.
//
// A view is folded into a synthetic table carrying the view's resolved columns, so the
// view's columns and alias enter the scope like a base table's; a sequence the engine
// lets a query read is folded in the same way with the columns the dialect gave it.
//
// Takes reference (ast.TableReference) which specifies the schema, name, and alias of the
// table.
//
// Returns *sqlmodel.Table which holds the matched catalogue table or view.
// Returns error when the reference names a schema the catalogue does not hold, or the
// table cannot be found; an unqualified reference always names the table, since the
// reader did not name a schema.
func (b *ScopeBuilder) resolveTableReference(
	reference ast.TableReference,
) (*sqlmodel.Table, error) {
	if reference.Schema != "" && b.catalogue.LookupSchema(reference.Schema) == nil {
		return nil, fmt.Errorf("unknown schema %s", quotedNames(b.catalogue.SchemaNamesFor(reference.Schema)))
	}

	if table := b.catalogue.LookupTable(reference.Schema, reference.Name); table != nil {
		return table, nil
	}

	if view := b.catalogue.LookupView(reference.Schema, reference.Name); view != nil {
		return &sqlmodel.Table{
			Name:              view.Name,
			Schema:            view.Schema,
			Columns:           view.Columns,
			Comment:           "",
			VirtualModuleName: "",
			PrimaryKey:        nil,
			Indexes:           nil,
			Constraints:       nil,
			PartitionOf:       nil,
			Origin:            sqlmodel.MigrationOrigin{},
			IsVirtual:         false,
			IsWithoutRowID:    false,
		}, nil
	}

	if sequence := b.catalogue.LookupSequence(reference.Schema, reference.Name); sequence != nil && len(sequence.Columns) > 0 {
		return &sqlmodel.Table{
			Name:              sequence.Name,
			Schema:            sequence.Schema,
			Columns:           sequence.Columns,
			Comment:           "",
			VirtualModuleName: "",
			PrimaryKey:        nil,
			Indexes:           nil,
			Constraints:       nil,
			PartitionOf:       nil,
			Origin:            sqlmodel.MigrationOrigin{},
			IsVirtual:         false,
			IsWithoutRowID:    false,
		}, nil
	}

	schemas := slices.DeleteFunc(b.catalogue.SchemaNamesFor(reference.Schema), func(name string) bool { return name == "" })
	switch len(schemas) {
	case 0:
		return nil, fmt.Errorf("unknown table or view %q", reference.Name)
	case 1:
		return nil, fmt.Errorf("unknown table or view %q in schema %s", reference.Name, quotedNames(schemas))
	default:
		return nil, fmt.Errorf("unknown table or view %q on the search path %s", reference.Name, quotedNames(schemas))
	}
}

// subqueryScopeBuilder returns a scope builder for the resolver's silent inner-subquery
// scopes. The builder shares the resolver's catalogue; its engine is the resolver's
// engine when that implements dialect.DialectPort, and nil otherwise so table-valued
// functions in inner subqueries are skipped.
//
// Returns *ScopeBuilder which types projections through this resolver.
func (r *TypeResolver) subqueryScopeBuilder() *ScopeBuilder {
	builder := NewScopeBuilder(nil, r.catalogue, r)
	if engine, ok := r.engine.(dialect.DialectPort); ok {
		builder.engine = engine
	}
	return builder
}

// readsRelation reports whether a query's FROM clause names a relation, as the recursive
// member of a recursive CTE names the CTE.
//
// Takes raw (*ast.RawQueryAnalysis) which holds the query.
// Takes name (string) which is the relation's canonical name.
// Takes scope (*ScopeChain) which compares names under the dialect's rules.
//
// Returns bool which is true when a FROM or JOIN reference names the relation.
func readsRelation(raw *ast.RawQueryAnalysis, name string, scope *ScopeChain) bool {
	identifiers := scope.Rules().Identifiers
	for _, reference := range raw.FromTables {
		if reference.Schema == "" && identifiers.Equal(reference.Name, name) {
			return true
		}
	}
	for _, joinClause := range raw.JoinClauses {
		if joinClause.Table.Schema == "" && identifiers.Equal(joinClause.Table.Name, name) {
			return true
		}
	}
	return false
}

// relationAlias returns the name a FROM or JOIN reference is visible under: its alias, or
// its name when it has none.
//
// Takes reference (ast.TableReference) which names the relation and its alias.
//
// Returns string which is the visible name.
func relationAlias(reference ast.TableReference) string {
	if reference.Alias != "" {
		return reference.Alias
	}
	return reference.Name
}

// cteBody returns the query a CTE's body is: the dialect's analysis of it, or, for a
// dialect that records none, a query made of the definition's own relations, select list
// and compound branches.
//
// Takes cteDefinition (*ast.RawCTEDefinition) which holds the CTE.
//
// Returns *ast.RawQueryAnalysis which is the body.
func cteBody(cteDefinition *ast.RawCTEDefinition) *ast.RawQueryAnalysis {
	if cteDefinition.Query != nil {
		return cteDefinition.Query
	}
	body := &ast.RawQueryAnalysis{}
	body.FromTables = cteDefinition.FromTables
	body.JoinClauses = cteDefinition.JoinClauses
	body.FromOrder = cteDefinition.FromOrder
	body.OutputColumns = cteDefinition.OutputColumns
	body.CompoundBranches = cteDefinition.CompoundBranches
	body.ParameterReferences = cteDefinition.ParameterReferences
	return body
}

// nameColumns renames a relation's columns from the column list a CTE or a derived table
// writes for it, in order: a shorter list renames the leading columns and the rest keep
// their names.
//
// Takes columns ([]ast.ScopedColumn) which holds the relation's columns, renamed in
// place.
// Takes names ([]string) which is the column list, or empty.
// Takes kind (string) which names the kind of relation for the diagnostic, such as CTE.
// Takes relation (string) which is the relation's name or alias.
//
// Returns *Error which is a warning when the list names more columns than the relation
// has, whose surplus names are ignored, or nil.
func nameColumns(columns []ast.ScopedColumn, names []string, kind string, relation string) *Error {
	for index := range min(len(columns), len(names)) {
		columns[index].Name = names[index]
	}
	if len(names) <= len(columns) {
		return nil
	}
	return newError(diag.CodeCompoundColumnCount,
		"%s %q has %d columns but its column list names %d; the surplus names are ignored",
		kind, relation, len(columns), len(names))
}

// quotedNames renders names as a comma-separated list of quoted strings.
//
// Takes names ([]string) which are the names to render.
//
// Returns string which is the list, such as "main", "temp".
func quotedNames(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, strconv.Quote(name))
	}
	return strings.Join(quoted, ", ")
}

// cteReference reports whether reference names a CTE visible from lookupScope, one
// registered in it or in an enclosing scope, returning that CTE and the alias it is
// visible under: the reference's alias, or its name when it has none.
//
// Takes reference (ast.TableReference) which names the relation and its alias.
// Takes lookupScope (*ScopeChain) which is the scope the reference is made in.
//
// Returns *ResolvedCTE which is the matched CTE.
// Returns string which is the alias the CTE is visible under.
// Returns bool which is true when the reference names a CTE.
func cteReference(reference ast.TableReference, lookupScope *ScopeChain) (*ResolvedCTE, string, bool) {
	cte, exists := lookupScope.visibleCTE(reference.Name)
	if !exists {
		return nil, "", false
	}
	alias := reference.Alias
	if alias == "" {
		alias = reference.Name
	}
	return cte, alias, true
}

// outputColumnsToScoped converts output columns to scoped columns for CTE and
// derived-table registration.
//
// Takes columns ([]sqlmodel.OutputColumn) which holds the resolved output columns to
// convert.
//
// Returns []ast.ScopedColumn which holds the scoped column equivalents with name, type,
// and nullability.
func outputColumnsToScoped(columns []sqlmodel.OutputColumn) []ast.ScopedColumn {
	scoped := make([]ast.ScopedColumn, len(columns))
	for index := range columns {
		scoped[index] = ast.ScopedColumn{
			Name:     columns[index].Name,
			SQLType:  columns[index].SQLType,
			Nullable: columns[index].Nullable,
		}
	}
	return scoped
}

// compoundDepthLimit is the warning for the compound branches of a query nested too
// deeply for them to be analysed.
//
// Returns *Error which is the CodeDepthLimit warning.
func compoundDepthLimit() *Error {
	return depthLimitError("compound branches nested more than %d levels deep were not analysed", MaxExpressionResolveDepth)
}

// mergeBranchNullability makes each primary column nullable when the matching column of a
// compound branch is, because the compound's rows come from every branch.
//
// Takes primaryColumns ([]sqlmodel.OutputColumn) which holds the primary columns, updated
// in place.
// Takes branchColumns ([]sqlmodel.OutputColumn) which holds the branch's columns, as many
// as the primary's.
func mergeBranchNullability(primaryColumns, branchColumns []sqlmodel.OutputColumn) {
	for columnIndex := range primaryColumns {
		if branchColumns[columnIndex].Nullable {
			primaryColumns[columnIndex].Nullable = true
		}
	}
}

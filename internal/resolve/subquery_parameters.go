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

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/directive"
	"seagull.sh/seagull/internal/sqlmodel"
)

// parameterIdentity identifies a raw parameter reference by its display name, the column
// it compares against, and its number.
//
// It is the key used by the identity guard so a subquery parameter is only taken over by
// the subquery pass when the same parameter, not just the same number, also appears in
// the top-level flat list.
type parameterIdentity struct {
	// name is the parameter's display name.
	name string

	// tableAlias is the alias of the table the parameter compares against.
	tableAlias string

	// columnName is the column the parameter compares against.
	columnName string

	// number is the parameter's positional number.
	number int
}

// subqueryParameterPass carries the accumulators and lookup sets threaded unchanged
// through the recursive subquery-parameter walk, keeping the recursion signature small.
type subqueryParameterPass struct {
	// parameterTypes accumulates resolved parameters keyed by number.
	parameterTypes map[int]*sqlmodel.QueryParameter

	// directiveNumberMap carries directive overrides keyed by parameter number, used for
	// naming.
	directiveNumberMap map[int]*directive.ParameterDirective

	// directiveNameMap carries directive overrides keyed by parameter name, used for naming.
	directiveNameMap map[string]*directive.ParameterDirective

	// handled records the parameter numbers the subquery pass took over so the outer flat
	// pass skips them.
	handled map[int]bool

	// rootIdentities is the identity guard set: the identities present in the top-level flat
	// parameter list.
	rootIdentities map[parameterIdentity]bool
}

// resolveSubqueryParameters types every parameter that occurs inside a subquery of raw
// against a scope built from that subquery's own FROM tables.
//
// Each subquery scope is chained to scope so a correlated parameter that compares against
// an outer column still resolves (the parser leaves such a parameter unqualified; the
// inner scope is consulted first, then the outer chain). Each parameter is typed in the
// innermost subquery that owns it.
//
// The subqueries visited are the EXISTS and scalar subqueries reachable through the typed
// SELECT-list expression tree (at any nesting), the FROM-clause derived tables, the
// SELECT body of an INSERT ... SELECT, the CTE bodies that carry their own parameters,
// and the WHERE/HAVING/JOIN-ON predicate subqueries the engine records on
// PredicateSubqueries. A nested parameter is taken over here only when it passes the
// identity guard: its identity (number, name, and column reference) must be present in
// pass.rootIdentities, the top-level flat list. SQL engines flatten a subquery's
// parameters into that list unchanged, so the identity matches and the parameter is typed
// in its own scope. An engine that parses a derived table with an independent nested
// parser (ClickHouse) numbers its parameters from one again, so their identity is absent
// from the flat list, the guard declines, and an unrelated outer parameter sharing the
// same number is never suppressed.
//
// Every number taken over here is recorded in pass.handled so the outer flat pass in
// mergeRawParameters skips it, which both prevents a stale unknown-column diagnostic and
// stops the outer pass overwriting the correctly scoped type. A parameter the subquery's
// scope cannot type is left to the flat pass, which reports what it cannot resolve, so
// this pass reports nothing itself. The subqueries of a query nested
// MaxExpressionResolveDepth or more levels deep are left to the flat pass too.
//
// Takes raw (*ast.RawQueryAnalysis) which is the query whose subqueries are walked.
// Takes scope (*ScopeChain) which is the enclosing scope (each subquery scope's parent).
// Takes pass (*subqueryParameterPass) which carries the accumulators and the identity
// guard.
func (r *TypeResolver) resolveSubqueryParameters(
	ctx context.Context,
	raw *ast.RawQueryAnalysis,
	scope *ScopeChain,
	pass *subqueryParameterPass,
) {
	if raw == nil || scope.nestedTooDeeply() {
		return
	}
	for _, subquery := range collectDirectSubqueries(raw) {
		if ctx.Err() != nil {
			return
		}
		r.resolveOneSubqueryParameters(ctx, subquery, scope, pass)
	}
}

// resolveOneSubqueryParameters types the parameters owned by a single direct subquery
// against a scope chained to the enclosing scope, then recurses into its own descendants.
// What the subquery's expressions compare its parameters with is collected in its own
// scope, for the parameters nothing else types.
//
// A parameter is taken over only when it is not owned by a deeper subquery, its identity
// is present in pass.rootIdentities (the identity guard on resolveSubqueryParameters) and
// the subquery's scope types it. Every number taken over is recorded in pass.handled so
// the outer flat pass skips it.
//
// Takes subquery (*ast.RawQueryAnalysis) which is the direct subquery to type.
// Takes scope (*ScopeChain) which is the enclosing scope (the subquery scope's parent).
// Takes pass (*subqueryParameterPass) which carries the accumulators and the identity
// guard.
func (r *TypeResolver) resolveOneSubqueryParameters(
	ctx context.Context,
	subquery *ast.RawQueryAnalysis,
	scope *ScopeChain,
	pass *subqueryParameterPass,
) {
	subqueryScope := NewScopeChain(ScopeKindSubquery, scope)
	r.subqueryScopeBuilder().addRawTablesToScope(ctx, subquery, subqueryScope)
	r.addComparedParameters(r.collectComparedParameters(ctx, subquery, subqueryScope))

	deeper := directSubqueryParameterNumbers(subquery)
	for _, rawParameter := range subquery.ParameterReferences {
		if deeper[rawParameter.Number] {
			continue
		}
		if !pass.rootIdentities[identityOf(rawParameter)] {
			continue
		}
		sqlType, nullable, resolveError := r.resolveParameterType(ctx, rawParameter, subqueryScope)

		if resolveError != nil || rawParameter.ColumnReference == nil {
			continue
		}
		pass.handled[rawParameter.Number] = true
		r.upsertParameterType(
			pass.parameterTypes, rawParameter, sqlType, nullable, pass.directiveNumberMap, pass.directiveNameMap,
		)
		r.recordParameterColumn(pass.parameterTypes[rawParameter.Number], rawParameter, subqueryScope)
	}

	r.resolveSubqueryParameters(ctx, subquery, subqueryScope, pass)
}

// identityOf derives the parameterIdentity of a raw parameter reference.
//
// Takes reference (ast.RawParameterReference) which is the parameter to key.
//
// Returns parameterIdentity which uniquely identifies the reference.
func identityOf(reference ast.RawParameterReference) parameterIdentity {
	identity := parameterIdentity{number: reference.Number, name: reference.Name, tableAlias: "", columnName: ""}
	if reference.ColumnReference != nil {
		identity.tableAlias = reference.ColumnReference.TableAlias
		identity.columnName = reference.ColumnReference.ColumnName
	}
	return identity
}

// parameterIdentitySet builds the set of identities present in the top-level flat
// parameter list.
//
// The subquery pass consults it to decide whether a nested parameter was flattened into
// this list by the engine (and so should be typed in its own scope here) or belongs to an
// engine that numbers nested parameters independently (and so must be left to the flat
// pass).
//
// Takes references ([]ast.RawParameterReference) which is the top-level flat list.
//
// Returns map[parameterIdentity]bool keyed by every identity in the list.
func parameterIdentitySet(references []ast.RawParameterReference) map[parameterIdentity]bool {
	set := make(map[parameterIdentity]bool, len(references))
	for _, reference := range references {
		set[identityOf(reference)] = true
	}
	return set
}

// collectDirectSubqueries returns the subquery analyses one level inside raw.
//
// These are the EXISTS and scalar subqueries found in the SELECT-list expressions, the
// FROM-clause derived-table subqueries, the predicate subqueries, an INSERT's SELECT body
// and the CTE bodies holding parameters. Subqueries nested deeper are reached by
// recursing into the returned analyses. Resolving a derived table's parameters here is
// made safe by the identity guard in resolveSubqueryParameters, which declines any nested
// parameter not present in the top-level flat list (so an engine that numbers
// derived-table parameters independently is unaffected).
//
// Takes raw (*ast.RawQueryAnalysis) which is the query to inspect.
//
// Returns []*ast.RawQueryAnalysis which holds the immediate subquery analyses.
func collectDirectSubqueries(raw *ast.RawQueryAnalysis) []*ast.RawQueryAnalysis {
	if raw == nil {
		return nil
	}
	var subqueries []*ast.RawQueryAnalysis
	for index := range raw.OutputColumns {
		subqueries = append(subqueries, Subqueries(raw.OutputColumns[index].Expression)...)
	}
	for index := range raw.RawDerivedTables {
		if raw.RawDerivedTables[index].InnerQuery != nil {
			subqueries = append(subqueries, raw.RawDerivedTables[index].InnerQuery)
		}
	}

	for _, predicateSubquery := range raw.PredicateSubqueries {
		if predicateSubquery != nil {
			subqueries = append(subqueries, predicateSubquery)
		}
	}

	if raw.InsertSelect != nil {
		subqueries = append(subqueries, raw.InsertSelect)
	}

	for index := range raw.CTEDefinitions {
		definition := &raw.CTEDefinitions[index]
		if len(definition.ParameterReferences) == 0 {
			continue
		}
		subqueries = append(subqueries, cteBody(definition))
	}
	return subqueries
}

// directSubqueryParameterNumbers returns the set of parameter numbers that belong to any
// subquery one level inside raw. Because each engine parser flattens a subquery's own
// parameters together with its descendants' parameters into that subquery's
// ParameterReferences, this set covers every parameter nested below raw.
//
// Takes raw (*ast.RawQueryAnalysis) which is the query to inspect.
//
// Returns map[int]bool keyed by the nested parameter numbers.
func directSubqueryParameterNumbers(raw *ast.RawQueryAnalysis) map[int]bool {
	numbers := make(map[int]bool)
	for _, subquery := range collectDirectSubqueries(raw) {
		for _, parameter := range subquery.ParameterReferences {
			numbers[parameter.Number] = true
		}
	}
	return numbers
}

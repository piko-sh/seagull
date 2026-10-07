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

package emitkit

import (
	"cmp"
	"fmt"
	"maps"
	"slices"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/sqlmodel"
)

// packagePlanner plans the names of one generated package.
type packagePlanner struct {
	// request is the request being planned.
	request *emit.Request

	// scope holds the package-level names and import qualifiers.
	scope *nameScope

	// body holds the names a method body declares: the emitter's locals and receiver.
	body *nameScope

	// methods holds the names of the Queries method set.
	methods *nameScope

	// files holds the names of the generated files.
	files *fileScope

	// naming accumulates the plan.
	naming emit.Naming

	// problems accumulates the names that cannot be planned.
	problems []emit.Problem

	// style holds the style's naming choices.
	style NameStyle

	// reservation holds the names the emitter reserves.
	reservation emit.Reservation
}

// planReserved claims the emitter's import qualifiers, in its order, and takes its
// declarations as they are.
func (p *packagePlanner) planReserved() {
	p.naming.Imports = planReservedImports(p.reservation.Imports, p.request.Style.PackageNames, p.scope)
	for _, declaration := range p.reservation.Declarations {
		p.scope.take(declaration)
	}
}

// planQueryNames takes the configured query-names function and file as they are, when the
// file is enabled, reporting either when the emitter already writes it.
func (p *packagePlanner) planQueryNames() {
	names := p.request.Style.QueryNames
	if !names.Enabled {
		return
	}
	if !p.scope.take(names.Function) {
		p.problems = append(p.problems, requestProblem(fmt.Sprintf(
			"the query-names function %q is a name the generated package already declares or imports; choose another name", names.Function)))
	}
	if !p.files.take(names.File) {
		p.problems = append(p.problems, requestProblem(fmt.Sprintf(
			"the query-names file %q is a file the emitter already writes; choose another name", names.File)))
	}
}

// planInterface takes the name of the interface of the query methods as it is, when the
// style asks for the interface, before any model or query type can claim it, reporting a
// name the package already declares or imports.
func (p *packagePlanner) planInterface() {
	configured := p.request.Style.Interface
	if !configured.Enabled {
		return
	}
	if !p.scope.take(configured.Name) {
		p.problems = append(p.problems, requestProblem(fmt.Sprintf(
			"the interface name %q is a name the generated package already declares or imports; choose another name", configured.Name)))
		return
	}
	p.naming.Interface = configured.Name
}

// planQueries plans the names of every query in planning order, each against the
// package-level names claimed before any query, reporting a query whose name cannot
// become an exported identifier.
//
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the queries in planning order.
// Takes rows (map[*sqlmodel.AnalysedQuery]rowChoice) which maps each query reading its
// rows into a struct other than a Row struct of its own to the struct.
func (p *packagePlanner) planQueries(queries []*sqlmodel.AnalysedQuery, rows map[*sqlmodel.AnalysedQuery]rowChoice) {
	positions := make(map[*sqlmodel.AnalysedQuery]int, len(p.request.Queries))
	for index, query := range p.request.Queries {
		positions[query] = index
	}
	p.naming.Queries = make([]emit.QueryNaming, len(p.request.Queries))
	reserved := reservedNames{
		PackageLevel: p.scope,
		Methods:      p.methods,
		Receiver:     p.reservation.Receiver,
		Locals:       newNameScope(slices.Concat(p.reservation.Locals, p.scope.names())...),
		Imports:      slices.Sorted(maps.Values(p.naming.Imports)),
	}
	for _, query := range queries {
		naming, err := planQuery(query, reserved, p.style, rows[query])
		if err != nil {
			p.problems = append(p.problems, emit.Problem{
				Query:    query,
				Code:     diag.CodeInvalidKeywordArgumentValue,
				Message:  err.Error(),
				Severity: diag.SeverityError,
			})
			continue
		}
		p.naming.Queries[positions[query]] = naming
	}
}

// PlanPackage assigns names to a generated package while avoiding collisions.
//
// The pipeline supplies the resulting plan in emit.Request.Naming. Callers building
// requests directly can use the function to supply a plan; the built-in emitters plan a
// request that has none. Conflicting names receive numeric suffixes in a stable order.
//
// Takes request (*emit.Request) which carries the package's queries, catalogue, type
// mapping and style. PlanPackage does not change it.
// Takes reservation (emit.Reservation) which holds the names the emitter reserves.
// Takes style (NameStyle) which holds the style's naming choices.
//
// Returns emit.Naming which is the plan: the same for the same request and reservation
// whatever the order of the queries, with a QueryNaming for every query, which is zero
// for a query whose names cannot be planned.
// Returns []emit.Problem which holds the names that cannot be planned: a query name that
// cannot become an exported identifier, reported for the query under the query-name code;
// a query whose rows do not fit the table's model or the shared row type its model: or
// row: keyword names, or whose row type is named like something the package already
// declares, reported for the query under the row-type code; or a query-names file or
// function, or an interface name, that the emitter already writes, reported for the
// request.
func PlanPackage(request *emit.Request, reservation emit.Reservation, style NameStyle) (emit.Naming, []emit.Problem) {
	planner := &packagePlanner{
		request:     request,
		scope:       newNameScope(),
		body:        newNameScope(reservation.Locals...),
		methods:     newNameScope(reservation.Methods...),
		files:       newFileScope(reservation.Files...),
		naming:      emit.Naming{Files: nil, Imports: nil, Models: nil, Types: nil, Queries: nil, Interface: ""},
		problems:    nil,
		reservation: reservation,
		style:       style,
	}
	planner.body.take(reservation.Receiver)
	planner.planReserved()
	planner.planQueryNames()
	planner.planInterface()
	use := usedGoTypes(request)
	if use.inlineJSON {
		planner.scope.take(emit.InlineJSONTypeName)
	}
	planUsedImports(planner.naming.Imports, use.packages, request.Style.PackageNames, planner.scope, planner.body)
	queries := planningOrder(request.Queries)
	types := newTypePlanner(request.Catalogue, style.NamedTypes, planner.scope)
	planner.naming.Models = types.planModels()
	types.planCatalogueEnums()
	types.planModelTypes(planner.naming.Models)
	types.planQueryTypes(queries)
	planner.naming.Types = types.types
	planner.planQueries(queries, planner.planRows(queries, planner.naming.Models))
	planner.naming.Files = planFiles(queries, planner.files)
	return planner.naming, planner.problems
}

// planningOrder returns a request's queries in the order their names are planned: by
// source file, then name, then their order in the request.
//
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the request's queries.
//
// Returns []*sqlmodel.AnalysedQuery which holds the non-nil queries in planning order.
func planningOrder(queries []*sqlmodel.AnalysedQuery) []*sqlmodel.AnalysedQuery {
	ordered := make([]*sqlmodel.AnalysedQuery, 0, len(queries))
	for _, query := range queries {
		if query != nil {
			ordered = append(ordered, query)
		}
	}
	slices.SortStableFunc(ordered, func(left, right *sqlmodel.AnalysedQuery) int {
		return cmp.Or(cmp.Compare(left.Filename, right.Filename), cmp.Compare(left.Name, right.Name))
	})
	return ordered
}

// requestProblem returns an error problem with the request as a whole, one the
// configuration should have prevented, so it carries no diagnostic code.
//
// Takes message (string) which describes the problem.
//
// Returns emit.Problem which is the problem.
func requestProblem(message string) emit.Problem {
	return emit.Problem{Query: nil, Code: "", Message: message, Severity: diag.SeverityError}
}

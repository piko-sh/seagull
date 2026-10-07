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

package gogen

import (
	"cmp"
	"go/ast"
	"maps"
	"slices"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// Emission is one package's generation as its builders read it: the request, the driver
// it is generated for and the vocabulary its names are written with. Builders read it and
// never change it.
type Emission struct {
	// Declarations holds query declarations already built, for the query interface.
	Declarations []ast.Decl

	// Referenced holds identifiers from generated query and model ASTs.
	Referenced map[string]struct{}

	// Qualifiers maps the qualifier of each import a query file registered to the import's
	// path, so a file written from the query declarations can import them.
	Qualifiers map[string]string

	// rendered contains immutable SQL metadata for this request and driver.
	rendered map[renderKey]renderData

	// Request is the request being emitted. Its Naming always holds a plan: NewEmission
	// plans one when the caller made none.
	Request *emit.Request

	// Driver is the driver the package is generated for.
	Driver *Driver

	// names maps each of the request's queries to its names in the naming plan.
	names map[*sqlmodel.AnalysedQuery]*emit.QueryNaming

	// prefixes maps each query whose SQL an earlier query of the package runs too to the
	// comment line written before its SQL; nil until a query is first rendered.
	prefixes map[*sqlmodel.AnalysedQuery]string

	// Vocabulary names the package's generated helpers.
	Vocabulary Vocabulary
}

// NewEmission starts one package's generation.
//
// A request without a naming plan, as a caller running an emitter directly may pass, is
// planned here with the default reservation, which covers the names of every built-in
// emitter; the plan goes into a copy of the request, so the caller's is not changed. The
// planner's problems are not reported here: a caller that emits a request it has not
// planned plans it with WithNamingPlan first and refuses it when a problem is an error.
//
// Takes request (*emit.Request) which is the request being emitted.
// Takes driver (*Driver) which is the driver the package is generated for.
//
// Returns *Emission which holds the planned request, the driver and the request's
// vocabulary.
func NewEmission(request *emit.Request, driver *Driver) *Emission {
	planned, _ := WithNamingPlan(request)
	return &Emission{
		rendered:     nil,
		Referenced:   make(map[string]struct{}),
		Qualifiers:   make(map[string]string),
		Request:      planned,
		Driver:       driver,
		names:        queryNames(planned),
		prefixes:     nil,
		Vocabulary:   NewVocabulary(planned),
		Declarations: nil,
	}
}

// Dialect returns the dialect the queries were analysed with.
//
// Returns *dialect.Facts which describes the dialect.
func (e *Emission) Dialect() *dialect.Facts {
	return &e.Request.Dialect
}

// Naming returns a query's names from the package's naming plan. When the request's
// queries changed after the emission started, so the plan does not name one of them, the
// request is planned again first, with the default reservation.
//
// Takes query (*sqlmodel.AnalysedQuery) which is one of the request's queries.
//
// Returns *emit.QueryNaming which holds its names, or nil when the query is not one of
// the request's or the plan could not name it.
func (e *Emission) Naming(query *sqlmodel.AnalysedQuery) *emit.QueryNaming {
	if naming, ok := e.names[query]; ok {
		return naming
	}
	if !slices.Contains(e.Request.Queries, query) {
		return nil
	}
	replanned := *e.Request
	replanned.Naming = emit.Naming{Files: nil, Imports: nil, Models: nil, Types: nil, Queries: nil, Interface: ""}
	e.Request, _ = WithNamingPlan(&replanned)
	e.names = queryNames(e.Request)
	e.rendered = nil
	e.prefixes = nil
	return e.names[query]
}

// Bind returns the expression that binds a parameter: the driver's envelope around the
// access, or the access itself when the driver binds the parameter as it is.
//
// Takes access (ast.Expr) which reads the parameter's value.
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
// Takes imports (*goast.ImportTracker) which collects the imports the expression needs.
//
// Returns ast.Expr which binds the parameter.
func (e *Emission) Bind(access ast.Expr, parameter *sqlmodel.QueryParameter, imports *goast.ImportTracker) ast.Expr {
	if e.Driver.Binder == nil {
		return access
	}
	return e.Driver.Binder.Bind(access, parameter, imports)
}

// BindPositional returns the expression that binds a parameter of a query rendered in the
// positional form a runtime builder of a dialect binding by name takes: the driver's
// envelope when its binder is a PositionalBinder, or the access itself.
//
// Takes access (ast.Expr) which reads the parameter's value.
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
//
// Returns ast.Expr which binds the parameter.
func (e *Emission) BindPositional(access ast.Expr, parameter *sqlmodel.QueryParameter) ast.Expr {
	positional, binds := e.Driver.Binder.(PositionalBinder)
	if !binds {
		return access
	}
	return positional.BindPositional(access, parameter)
}

// Reading returns how the generated code reads an output column of a SQL type.
//
// Takes sqlType (sqlmodel.SQLType) which is the column's SQL type.
//
// Returns ColumnReading which is the driver's reading, or ColumnReadAsIs when it has no
// reader.
func (e *Emission) Reading(sqlType sqlmodel.SQLType) ColumnReading {
	if e.Driver.Reader == nil {
		return ColumnReadAsIs
	}
	return e.Driver.Reader.ColumnReading(sqlType)
}

// ConnectionField returns the Queries field a query runs on: the reader for a read-only
// query and the writer for any other.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns string which is the field's name.
func (e *Emission) ConnectionField(query *sqlmodel.AnalysedQuery) string {
	if query.ReadOnly {
		return e.Driver.Connection.Reader
	}
	return e.Driver.Connection.Writer
}

// RecordDeclarations retains query ASTs, their unqualified helper references and the
// qualifiers of the imports their file registered.
//
// Takes declarations ([]ast.Decl) which holds the emitted declarations whose referenced
// names are recorded.
// Takes imports (*goast.ImportTracker) which holds the imports of their file.
func (e *Emission) RecordDeclarations(declarations []ast.Decl, imports *goast.ImportTracker) {
	e.Declarations = append(e.Declarations, declarations...)
	if e.Referenced == nil {
		e.Referenced = make(map[string]struct{})
	}
	maps.Copy(e.Referenced, goast.UnqualifiedIdentifiers(declarations))
	if e.Qualifiers == nil {
		e.Qualifiers = make(map[string]string)
	}
	maps.Copy(e.Qualifiers, imports.Qualifiers())
}

// WithNamingPlan returns a request holding a naming plan and any problems found while
// planning it.
//
// A request that holds a plan is returned unchanged, with no problems. Otherwise, the
// result is a copy holding a plan made with the default reservation. A query whose names
// cannot be planned is left without names, and the problems explain why.
//
// Takes request (*emit.Request) which is the request being emitted. WithNamingPlan does
// not change it.
//
// Returns *emit.Request which holds a naming plan.
// Returns []emit.Problem which holds the problems planning met, or nil.
func WithNamingPlan(request *emit.Request) (*emit.Request, []emit.Problem) {
	if HoldsNamingPlan(request) {
		return request, nil
	}
	planned := *request
	var problems []emit.Problem
	planned.Naming, problems = emitkit.PlanPackage(&planned, emitkit.DefaultReservation(&planned.Dialect, &planned.Style), emitkit.StyleNames(&planned.Style))
	return &planned, problems
}

// HoldsNamingPlan reports whether a request holds a naming plan: one that maps the
// imports, as every plan does, and holds names for as many queries as the request has.
//
// Takes request (*emit.Request) which is the request.
//
// Returns bool which is true when the request holds a plan.
func HoldsNamingPlan(request *emit.Request) bool {
	return request.Naming.Imports != nil && len(request.Naming.Queries) == len(request.Queries)
}

// PlanOrder returns queries in the naming plan's order: by source file, then name, then
// their order in the request. Package-wide lists of queries follow it, such as the
// query-names lookup and the prepared statements, so reordering a file's queries leaves
// the generated code as it was.
//
// Takes queries ([]*sqlmodel.AnalysedQuery) which are the queries. PlanOrder does not
// change the slice.
//
// Returns []*sqlmodel.AnalysedQuery which holds the queries in the plan's order.
func PlanOrder(queries []*sqlmodel.AnalysedQuery) []*sqlmodel.AnalysedQuery {
	ordered := slices.Clone(queries)
	slices.SortStableFunc(ordered, func(left, right *sqlmodel.AnalysedQuery) int {
		return cmp.Or(cmp.Compare(left.Filename, right.Filename), cmp.Compare(left.Name, right.Name))
	})
	return ordered
}

// queryNames maps each of a planned request's queries to its names.
//
// Takes request (*emit.Request) which holds the queries and the plan.
//
// Returns map[*sqlmodel.AnalysedQuery]*emit.QueryNaming which maps each query the plan
// names to its names.
func queryNames(request *emit.Request) map[*sqlmodel.AnalysedQuery]*emit.QueryNaming {
	names := make(map[*sqlmodel.AnalysedQuery]*emit.QueryNaming, len(request.Queries))
	for index, query := range request.Queries {
		if index < len(request.Naming.Queries) && request.Naming.Queries[index].Method != "" {
			names[query] = &request.Naming.Queries[index]
		}
	}
	return names
}

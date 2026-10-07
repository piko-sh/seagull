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

package generate

import (
	"fmt"
	"go/token"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/methods"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// maxReportedProblems caps the planning problems the error refusing a request names; the
	// rest are counted.
	maxReportedProblems = 16

	// commentMarker opens the Go line comment a header is.
	commentMarker = "//"

	// headerBreakers holds what a header cannot carry and stay one valid comment line: a
	// line break, a NUL and a byte order mark.
	headerBreakers = "\r\n\x00\uFEFF"

	// blankIdentifier is the identifier that declares nothing.
	blankIdentifier = "_"
)

// tableKey finds a catalogue table by its schema and name.
type tableKey struct {
	// schema is the table's schema.
	schema string

	// name is the table's name.
	name string
}

// ValidateRequest checks a request before any of it is emitted, so an emitter can refuse
// it before reading it.
//
// A request is refused when it is nil or holds a nil query; when its package name is not
// a Go identifier, its header is not one comment line, or its helper or error prefix does
// not have the shape a directive prefix has; and when the naming plan it holds names no
// method for a query, gives a name that is not a Go identifier, or holds a slice that is
// not as long as the query's parameters or columns, or the table's columns or the type's
// values or fields, it runs parallel to. A request without a plan is not planned here.
//
// Takes request (*emit.Request) which is the request. ValidateRequest does not change it.
//
// Returns error which wraps ErrInvalidRequest when the request is refused, or nil.
func ValidateRequest(request *emit.Request) error {
	if request == nil {
		return fmt.Errorf("%w: the request is nil", ErrInvalidRequest)
	}
	if err := checkStyle(request); err != nil {
		return err
	}
	if index := slices.Index(request.Queries, nil); index >= 0 {
		return fmt.Errorf("%w: query %d is nil", ErrInvalidRequest, index)
	}
	if gogen.HoldsNamingPlan(request) {
		return checkPlan(request)
	}
	return nil
}

// validateRequest checks a request as ValidateRequest does and returns the request to
// emit: the request itself when it holds a naming plan, or a copy holding the plan made
// for it, which is refused when planning met an error problem.
//
// Takes request (*emit.Request) which is the request. validateRequest does not change it.
//
// Returns *emit.Request which holds a naming plan.
// Returns error which wraps ErrInvalidRequest when the request is refused.
func validateRequest(request *emit.Request) (*emit.Request, error) {
	if err := ValidateRequest(request); err != nil {
		return nil, err
	}
	planned, problems := gogen.WithNamingPlan(request)
	if err := refusePlan(problems); err != nil {
		return nil, err
	}
	return planned, nil
}

// checkStyle checks the text a request's generated code is written with: the package
// name, the header and the helper and error prefixes, which are written into the source
// as they are.
//
// Takes request (*emit.Request) which is the request.
//
// Returns error which wraps ErrInvalidRequest naming the first that cannot be written.
func checkStyle(request *emit.Request) error {
	style := &request.Style
	switch {
	case !token.IsIdentifier(request.Package) || request.Package == blankIdentifier:
		return fmt.Errorf("%w: package name %q is not a Go identifier", ErrInvalidRequest, request.Package)
	case !oneLineComment(style.Header):
		return fmt.Errorf("%w: header %q is not one Go line comment", ErrInvalidRequest, style.Header)
	case !namespace.ValidPrefix(style.HelperPrefix):
		return fmt.Errorf("%w: helper prefix %q must match [a-z][a-z0-9_]{0,31}", ErrInvalidRequest, style.HelperPrefix)
	case !namespace.ValidPrefix(style.ErrorPrefix):
		return fmt.Errorf("%w: error prefix %q must match [a-z][a-z0-9_]{0,31}", ErrInvalidRequest, style.ErrorPrefix)
	default:
		return nil
	}
}

// checkPlan checks the naming plan a request holds against the request: every name it
// gives is a Go identifier, it names a method for every query, and every slice it holds
// is as long as what it runs parallel to.
//
// Takes request (*emit.Request) which holds the plan.
//
// Returns error which wraps ErrInvalidRequest naming the first part that does not fit.
func checkPlan(request *emit.Request) error {
	naming := &request.Naming
	if !optionalIdentifier(naming.Interface) {
		return fmt.Errorf("%w: the naming plan's interface %q is not a Go identifier", ErrInvalidRequest, naming.Interface)
	}
	for _, path := range slices.Sorted(maps.Keys(naming.Imports)) {
		if !identifier(naming.Imports[path]) {
			return fmt.Errorf("%w: the naming plan's qualifier %q of %s is not a Go identifier", ErrInvalidRequest, naming.Imports[path], path)
		}
	}
	if err := checkModels(request.Catalogue, naming.Models); err != nil {
		return err
	}
	if err := checkNamedTypes(naming.Types); err != nil {
		return err
	}
	for index, query := range request.Queries {
		if err := checkQueryNaming(query, &naming.Queries[index]); err != nil {
			return fmt.Errorf("%w: query %s: %w", ErrInvalidRequest, query.Name, err)
		}
	}
	return nil
}

// checkModels checks the plan's models: each names a table the catalogue holds, with a
// field name, or none for a hidden column, for each of the table's columns.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the tables, or is nil.
// Takes models ([]emit.ModelNaming) which are the plan's models.
//
// Returns error which wraps ErrInvalidRequest naming the first model that does not fit.
func checkModels(catalogue *sqlmodel.Catalogue, models []emit.ModelNaming) error {
	if len(models) == 0 {
		return nil
	}
	tables := catalogueTables(catalogue)
	for index := range models {
		model := &models[index]
		table, found := tables[tableKey{schema: model.Schema, name: model.Relation}]
		switch {
		case !found:
			return fmt.Errorf("%w: model %s names table %s.%s, which the catalogue does not hold", ErrInvalidRequest, model.Type, model.Schema, model.Relation)
		case len(model.Fields) != len(table.Columns):
			return fmt.Errorf("%w: model %s names %d fields for the %d columns of %s.%s",
				ErrInvalidRequest, model.Type, len(model.Fields), len(table.Columns), model.Schema, model.Relation)
		case !identifier(model.Type) || !optionalIdentifiers(model.Fields):
			return fmt.Errorf("%w: model %s gives a name that is not a Go identifier", ErrInvalidRequest, model.Type)
		}
	}
	return nil
}

// checkNamedTypes checks the plan's named types: an enum names a constant for each of its
// values, and any other type a field for each of its fields.
//
// Takes types ([]emit.NamedType) which are the plan's named types.
//
// Returns error which wraps ErrInvalidRequest naming the first type that does not fit.
func checkNamedTypes(types []emit.NamedType) error {
	for index := range types {
		named := &types[index]
		names, want, parts := named.Fields, len(named.SQLType.StructFields), "fields"
		if named.SQLType.Category == sqlmodel.TypeCategoryEnum {
			names, want, parts = named.Constants, len(named.SQLType.EnumValues), "values"
		}
		if len(names) != want {
			return fmt.Errorf("%w: type %s names %d of its %d %s", ErrInvalidRequest, named.Name, len(names), want, parts)
		}
		if !identifier(named.Name) || !identifiers(names) {
			return fmt.Errorf("%w: type %s gives a name that is not a Go identifier", ErrInvalidRequest, named.Name)
		}
	}
	return nil
}

// checkQueryNaming checks one query's names: the plan names its method, every name is a
// Go identifier, and each slice runs parallel to the query's parameters, output columns
// or sortable columns.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
// Takes naming (*emit.QueryNaming) which holds its names.
//
// Returns error which describes the first name that does not fit.
func checkQueryNaming(query *sqlmodel.AnalysedQuery, naming *emit.QueryNaming) error {
	if naming.Method == "" {
		return errNoMethod
	}
	single := []string{
		naming.Method, naming.SQLConstant, naming.CountConstant, naming.Params, naming.ParamsArgument, naming.Row,
		naming.Builder, naming.AllowedColumns, naming.OrderBy, naming.DirectionField,
	}
	if !optionalIdentifiers(single) || !identifiers(naming.ParameterFields) || !identifiers(naming.ColumnFields) ||
		!identifiers(naming.ColumnLocals) || !identifiers(naming.OrderByConstants) || !embedIdentifiers(naming.Embeds) {
		return errNotIdentifier
	}
	sortable, sortableColumns := sortableParameter(query), 0
	if sortable != nil {
		sortableColumns = len(sortable.SortableColumns)
	}
	columns := len(query.OutputColumns)
	switch {
	case len(naming.ParameterFields) != len(query.Parameters):
		return fmt.Errorf("%w: %d parameter fields for %d parameters", errNotParallel, len(naming.ParameterFields), len(query.Parameters))
	case !parallel(len(naming.ColumnFields), columns, naming.Row != ""):
		return fmt.Errorf("%w: %d column fields for %d columns", errNotParallel, len(naming.ColumnFields), columns)
	case !parallel(len(naming.ColumnLocals), columns, naming.Row != "" && methods.HasGroupByKey(query)):
		return fmt.Errorf("%w: %d column locals for %d columns", errNotParallel, len(naming.ColumnLocals), columns)
	case len(naming.OrderByConstants) != sortableColumns:
		return fmt.Errorf("%w: %d order constants for %d sortable columns", errNotParallel, len(naming.OrderByConstants), sortableColumns)
	default:
		return nil
	}
}

// refusePlan returns the error refusing a request whose names planning met an error
// problem with, naming at most maxReportedProblems of them.
//
// Takes problems ([]emit.Problem) which are the planning problems.
//
// Returns error which wraps ErrInvalidRequest, or nil when no problem is an error.
func refusePlan(problems []emit.Problem) error {
	var messages []string
	refused := 0
	for _, problem := range problems {
		if problem.Severity != diag.SeverityError {
			continue
		}
		refused++
		if len(messages) == maxReportedProblems {
			continue
		}
		message := problem.Code + " " + problem.Message
		if problem.Query != nil {
			message = fmt.Sprintf("query %s: %s", problem.Query.Name, message)
		}
		messages = append(messages, message)
	}
	if refused == 0 {
		return nil
	}
	if left := refused - len(messages); left > 0 {
		messages = append(messages, fmt.Sprintf("and %d more", left))
	}
	return fmt.Errorf("%w: the names cannot be planned: %s", ErrInvalidRequest, strings.Join(messages, "; "))
}

// catalogueTables maps every table of a catalogue by its schema and name, skipping
// missing schemas and tables.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the tables, or is nil.
//
// Returns map[tableKey]*sqlmodel.Table which holds the tables.
func catalogueTables(catalogue *sqlmodel.Catalogue) map[tableKey]*sqlmodel.Table {
	tables := make(map[tableKey]*sqlmodel.Table)
	if catalogue == nil {
		return tables
	}
	for _, schema := range catalogue.Schemas {
		if schema == nil {
			continue
		}
		for _, table := range schema.Tables {
			if table != nil {
				tables[tableKey{schema: table.Schema, name: table.Name}] = table
			}
		}
	}
	return tables
}

// sortableParameter returns a query's first sortable parameter.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns *sqlmodel.QueryParameter which is the parameter, or nil when there is none.
func sortableParameter(query *sqlmodel.AnalysedQuery) *sqlmodel.QueryParameter {
	for index := range query.Parameters {
		if query.Parameters[index].Kind == sqlmodel.ParameterDirectiveSortable {
			return &query.Parameters[index]
		}
	}
	return nil
}

// parallel reports whether a slice of names is as long as what it runs parallel to, or is
// empty where the query does not need it.
//
// Takes have (int) which is the slice's length.
// Takes want (int) which is the length of what it runs parallel to.
// Takes required (bool) which is true when the query needs the slice.
//
// Returns bool which is true when the slice fits.
func parallel(have, want int, required bool) bool {
	return have == want || (!required && have == 0)
}

// oneLineComment reports whether a header can be written as the first line of a Go file:
// empty, or one valid UTF-8 line comment holding no NUL or byte order mark.
//
// Takes header (string) which is the header.
//
// Returns bool which is true when the header can be written.
func oneLineComment(header string) bool {
	if header == "" {
		return true
	}
	return strings.HasPrefix(header, commentMarker) && utf8.ValidString(header) && !strings.ContainsAny(header, headerBreakers)
}

// identifier reports whether a name is a Go identifier other than the blank one.
//
// Takes name (string) which is the name.
//
// Returns bool which is true for an identifier.
func identifier(name string) bool {
	return name != blankIdentifier && token.IsIdentifier(name)
}

// optionalIdentifier reports whether a name is empty or a Go identifier.
//
// Takes name (string) which is the name.
//
// Returns bool which is true for an empty name or an identifier.
func optionalIdentifier(name string) bool {
	return name == "" || identifier(name)
}

// identifiers reports whether every name is a Go identifier.
//
// Takes names ([]string) which are the names.
//
// Returns bool which is true when every name is one.
func identifiers(names []string) bool {
	return !slices.ContainsFunc(names, func(name string) bool { return !identifier(name) })
}

// optionalIdentifiers reports whether every name is empty or a Go identifier.
//
// Takes names ([]string) which are the names.
//
// Returns bool which is true when every name is empty or one.
func optionalIdentifiers(names []string) bool {
	return !slices.ContainsFunc(names, func(name string) bool { return !optionalIdentifier(name) })
}

// embedIdentifiers reports whether every embed's field and type are Go identifiers.
//
// Takes embeds ([]emit.EmbedNaming) which are the embeds.
//
// Returns bool which is true when every name is one.
func embedIdentifiers(embeds []emit.EmbedNaming) bool {
	return !slices.ContainsFunc(embeds, func(embed emit.EmbedNaming) bool { return !identifier(embed.Field) || !identifier(embed.Type) })
}

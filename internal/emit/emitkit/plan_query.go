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
	"errors"
	"fmt"
	"go/token"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// paramsArgument is the method argument holding a query's Params struct.
	paramsArgument = "params"

	// suffixParams ends the name of a query's Params struct.
	suffixParams = "Params"

	// suffixRow ends the name of a query's Row struct.
	suffixRow = "Row"

	// suffixBuilder ends the name of a runtime builder type.
	suffixBuilder = "Builder"

	// suffixOrderBy ends the name of a sortable query's column type.
	suffixOrderBy = "OrderBy"

	// suffixDirection ends the name of a sortable query's direction field.
	suffixDirection = "Direction"

	// suffixAllowedColumns ends the name of a runtime builder's column variable.
	suffixAllowedColumns = "AllowedColumns"

	// suffixCountConstant ends the name of a runtime builder's count constant.
	suffixCountConstant = "CountSQL"

	// prefixColumnLocal starts the local a grouped query scans a column of no embed into.
	prefixColumnLocal = "col"

	// fallbackArgument names a parameter argument whose name gives no identifier.
	fallbackArgument = "parameter"

	// fallbackParameterField names a parameter field whose name gives no identifier.
	fallbackParameterField = "Parameter"

	// fallbackColumnField names a column field whose name gives no identifier.
	fallbackColumnField = "Column"

	// snakeSeparator separates the words of a snake_case query name.
	snakeSeparator = "_"
)

var (
	// errUnexportableQueryName reports a query name that cannot become an exported Go
	// identifier.
	errUnexportableQueryName = errors.New("cannot become an exported Go identifier: it must start with a letter that has an upper-case form and hold only letters, digits and underscores")
)

// queryPlanner plans the names of one query against the reserved names.
type queryPlanner struct {
	// query is the query being planned.
	query *sqlmodel.AnalysedQuery

	// reserved holds the names to avoid and the scopes to claim in.
	reserved reservedNames

	// packageLevel is the scope the query's package-level names are claimed in.
	packageLevel *nameScope

	// locals holds the names a method body declares, which a package-level name a body
	// refers to must avoid, since the local would hide it.
	locals *nameScope

	// style holds the style's naming choices.
	style NameStyle

	// own lists the package-level names claimed for the query, which its body refers to.
	own []string

	// row is the struct the query's rows are read into when it is not a Row struct of the
	// query's own, or zero.
	row rowChoice

	// naming accumulates the query's names.
	naming emit.QueryNaming
}

// planPackageLevel claims the query's package-level names in order: constants and
// variables first, clear of the names a method body declares, then types.
func (p *queryPlanner) planPackageLevel() {
	camel := p.style.constantBase(p.query.Name, "")
	method := p.naming.Method
	p.naming.SQLConstant = p.claimValue(p.style.constantBase(p.query.Name, p.style.SQLConstantSuffix))
	if countsRows(p.query) {
		p.naming.CountConstant = p.claimValue(camel + suffixCountConstant)
	}
	if p.query.DynamicRuntime {
		p.naming.AllowedColumns = p.claimValue(camel + suffixAllowedColumns)
	}
	if takesParamsStruct(p.query) {
		p.naming.Params = p.claim(method + suffixParams)
		p.naming.ParamsArgument = paramsArgument
	}
	if needsRow(p.query) && p.row.name != "" {
		p.naming.Row, p.naming.SharedRow = p.row.name, p.row.shared
		p.own = append(p.own, p.row.name)
	} else if needsRow(p.query) {
		p.naming.Row = p.claim(method + suffixRow)
		for _, group := range embedGroups(p.query) {
			p.naming.Embeds = append(p.naming.Embeds, emit.EmbedNaming{
				Group: group,
				Field: "",
				Type:  p.claim(method + goast.SnakeToPascalCase(group)),
			})
		}
	}
	if p.query.DynamicRuntime {
		p.naming.Builder = p.claim(method + suffixBuilder)
	}
	if sortable := sortableParameter(p.query); sortable != nil {
		p.naming.OrderBy = p.claim(method + suffixOrderBy)
		p.naming.OrderByConstants = make([]string, len(sortable.SortableColumns))
		for index, column := range sortable.SortableColumns {
			p.naming.OrderByConstants[index] = p.claim(p.naming.OrderBy + goast.SnakeToPascalCase(column))
		}
	}
}

// claimValue takes a package-level constant or variable name for the query, clear of the
// names a method body declares, and records it as one of its own.
//
// Takes name (string) which is the name wanted.
//
// Returns string which is the name taken.
func (p *queryPlanner) claimValue(name string) string {
	claimed := claimClearOf(p.packageLevel, p.locals, name)
	p.own = append(p.own, claimed)
	return claimed
}

// claim takes a package-level name for the query and records it as one of its own.
//
// Takes name (string) which is the name wanted.
//
// Returns string which is the name taken.
func (p *queryPlanner) claim(name string) string {
	claimed := p.packageLevel.claim(name)
	p.own = append(p.own, claimed)
	return claimed
}

// planParameterFields names the Params field of every parameter, and the direction field
// a sortable parameter adds after its own, unique within the struct. A query with no
// Params struct gets its argument names in planLocals.
func (p *queryPlanner) planParameterFields() {
	if p.naming.Params == "" {
		return
	}
	fields := newNameScope()
	p.naming.ParameterFields = make([]string, len(p.query.Parameters))
	sortable := sortableParameter(p.query)
	for index := range p.query.Parameters {
		parameter := &p.query.Parameters[index]
		field := fields.claim(nonEmpty(goast.SnakeToPascalCase(parameter.Name), fallbackParameterField))
		p.naming.ParameterFields[index] = field
		if parameter == sortable {
			p.naming.DirectionField = fields.claim(field + suffixDirection)
		}
	}
}

// planColumnFields names the field each output column is read into: the Row fields of the
// columns of no embed, in order, then the Row field of each embed, then each embed's
// fields within its own struct.
func (p *queryPlanner) planColumnFields() {
	if p.naming.Row == "" {
		return
	}
	columns := p.query.OutputColumns
	row := newNameScope()
	p.naming.ColumnFields = flatColumnFields(columns, row)
	embedFields := make(map[string]*nameScope, len(p.naming.Embeds))
	for index := range p.naming.Embeds {
		embed := &p.naming.Embeds[index]
		embed.Field = row.claim(goast.SnakeToPascalCase(embed.Group))
		embedFields[embed.Group] = newNameScope()
	}
	for index := range columns {
		if columns[index].IsEmbedded {
			p.naming.ColumnFields[index] = embedFields[embedGroupName(&columns[index])].claim(columnField(&columns[index]))
		}
	}
}

// planLocals names the argument a lone parameter is passed as and a grouped query's
// column locals, which avoid every name the method body declares or refers to.
func (p *queryPlanner) planLocals() {
	body := p.locals
	for _, name := range slices.Concat(p.reserved.Imports, p.own) {
		body.take(name)
	}
	if inlinesParameter(p.query) {
		p.naming.ParameterFields = []string{body.claim(nonEmpty(goast.SnakeToCamelCase(p.query.Parameters[0].Name), fallbackArgument))}
	}
	if !groupsRows(p.query) || p.naming.Row == "" {
		return
	}
	columns := p.query.OutputColumns
	p.naming.ColumnLocals = make([]string, len(columns))
	for index := range columns {
		local := prefixColumnLocal + columnField(&columns[index])
		if columns[index].IsEmbedded {
			local = goast.SnakeToCamelCase(embedGroupName(&columns[index])) + columnField(&columns[index])
		}
		p.naming.ColumnLocals[index] = body.claim(local)
	}
}

// MethodName returns a query's Queries method name before the naming plan makes it
// unique. It converts snake_case to PascalCase with common initialisms, and otherwise
// capitalises the first letter.
//
// Takes name (string) which is the query's name, as its directive writes it.
//
// Returns string which is the method name.
// Returns error when the name cannot become an exported Go identifier.
func MethodName(name string) (string, error) {
	return exportedQueryName(name)
}

// planQuery plans the names of one query: its method, constants, types, fields and
// locals, claimed against the reserved names and the package scope.
//
// The method uses exportedQueryName to convert the query's name, claimed among the
// Queries methods. The package-level names follow, claimed in the package scope in this
// order: the SQL constant (the name in lower camel case and the style's suffix), the
// count constant, the allowed-columns variable, the Params and Row structs, the embed
// structs, the builder, and the sortable column type and its constants. Then come the
// fields, unique within their struct, and last the argument a lone parameter is passed as
// and a grouped query's column locals, which avoid the reserved locals, the receiver, the
// import qualifiers and the query's own package-level names.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
// Takes reserved (reservedNames) which holds the names to avoid and the scopes to claim
// in.
// Takes style (NameStyle) which holds the style's naming choices.
// Takes row (rowChoice) which is the struct the query's rows are read into when it is not
// a Row struct of the query's own, or zero, so the query claims a Row struct.
//
// Returns emit.QueryNaming which holds the query's names.
// Returns error when the query's name cannot become an exported Go identifier.
func planQuery(query *sqlmodel.AnalysedQuery, reserved reservedNames, style NameStyle, row rowChoice) (emit.QueryNaming, error) {
	method, err := exportedQueryName(query.Name)
	if err != nil {
		return emit.QueryNaming{}, err
	}
	if reserved.Methods != nil {
		method = reserved.Methods.claim(method)
	}
	packageLevel := reserved.PackageLevel
	if packageLevel == nil {
		packageLevel = newNameScope()
	}
	locals := newNameScope()
	locals.parent = reserved.Locals
	locals.take(reserved.Receiver)
	planner := &queryPlanner{
		query:        query,
		reserved:     reserved,
		packageLevel: packageLevel,
		locals:       locals,
		naming:       emit.QueryNaming{},
		own:          nil,
		style:        style,
		row:          row,
	}
	planner.naming.Method = method
	planner.naming.Label = goast.SnakeToPascalCase(query.Name)
	planner.planPackageLevel()
	planner.planParameterFields()
	planner.planColumnFields()
	planner.planLocals()
	return planner.naming, nil
}

// exportedQueryName returns the name of the method that runs a query: a snake_case name
// in PascalCase, with the words joined and common initialisms in their usual form, so
// get_user_by_id gives GetUserByID, and any other name with its first letter upper-cased,
// so getUser gives GetUser and GetUserByID stays as it is.
//
// Takes name (string) which is the query's name.
//
// Returns string which is the method name.
// Returns error which wraps errUnexportableQueryName when the result is not an exported
// Go identifier.
func exportedQueryName(name string) (string, error) {
	first, width := utf8.DecodeRuneInString(name)
	exported := string(unicode.ToUpper(first)) + name[width:]
	if name == "" || first == utf8.RuneError || !token.IsIdentifier(exported) || !token.IsExported(exported) {
		return "", fmt.Errorf("query name %q %w", name, errUnexportableQueryName)
	}
	if strings.Contains(name, snakeSeparator) {
		return goast.SnakeToPascalCase(name), nil
	}
	return exported, nil
}

// columnField returns the field name an output column's name gives.
//
// Takes column (*sqlmodel.OutputColumn) which is the column.
//
// Returns string which is the field name.
func columnField(column *sqlmodel.OutputColumn) string {
	return nonEmpty(goast.SnakeToPascalCase(column.Name), fallbackColumnField)
}

// nonEmpty returns a name, or a fallback when the name is empty.
//
// Takes name (string) which is the name.
// Takes fallback (string) which replaces an empty name.
//
// Returns string which is the name or the fallback.
func nonEmpty(name, fallback string) string {
	if name == "" {
		return fallback
	}
	return name
}

// inlinesParameter reports whether a query's method takes its one parameter as an
// argument of its own rather than in a Params struct: a lone parameter that is no slice,
// of a query that is neither a batch or copyfrom nor an optional or sortable one.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns bool which is true when the parameter is passed as its own argument.
func inlinesParameter(query *sqlmodel.AnalysedQuery) bool {
	if len(query.Parameters) != 1 || query.Parameters[0].IsSlice {
		return false
	}
	if query.Command == sqlmodel.QueryCommandBatch || query.Command == sqlmodel.QueryCommandCopyFrom {
		return false
	}
	return !query.IsDynamic || query.DynamicRuntime
}

// takesParamsStruct reports whether a query's method takes its parameters in a Params
// struct.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns bool which is true when the query declares a Params struct.
func takesParamsStruct(query *sqlmodel.AnalysedQuery) bool {
	return len(query.Parameters) > 0 && !inlinesParameter(query)
}

// needsRow reports whether a query reads its results into a Row struct: a query that
// returns rows, a batch whose statement returns columns, or a runtime builder.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns bool which is true when the query declares a Row struct.
func needsRow(query *sqlmodel.AnalysedQuery) bool {
	switch {
	case query.DynamicRuntime:
		return true
	case query.Command == sqlmodel.QueryCommandBatch:
		return len(query.OutputColumns) > 0
	default:
		return query.Command == sqlmodel.QueryCommandOne || query.Command == sqlmodel.QueryCommandMany ||
			query.Command == sqlmodel.QueryCommandStream
	}
}

// countsRows reports whether a runtime builder query can count its rows.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns bool which is true when the query declares a count constant.
func countsRows(query *sqlmodel.AnalysedQuery) bool {
	return query.DynamicRuntime && query.CountTemplate != nil
}

// groupsRows reports whether a query groups its rows by a key, scanning every column into
// a local first.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns bool which is true for a grouped query.
func groupsRows(query *sqlmodel.AnalysedQuery) bool {
	return len(query.GroupByKey) > 0 && len(embedGroups(query)) > 0
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

// embedGroups returns the embed groups of a query's output columns in the order they
// first appear.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns []string which holds the group names.
func embedGroups(query *sqlmodel.AnalysedQuery) []string {
	var groups []string
	for index := range query.OutputColumns {
		if !query.OutputColumns[index].IsEmbedded {
			continue
		}
		if group := embedGroupName(&query.OutputColumns[index]); !slices.Contains(groups, group) {
			groups = append(groups, group)
		}
	}
	return groups
}

// embedGroupName returns the embed group an embedded column belongs to: its EmbedName, or
// its EmbedTable when the name is empty.
//
// Takes column (*sqlmodel.OutputColumn) which is the embedded column.
//
// Returns string which is the group.
func embedGroupName(column *sqlmodel.OutputColumn) string {
	if column.EmbedName != "" {
		return column.EmbedName
	}
	return column.EmbedTable
}

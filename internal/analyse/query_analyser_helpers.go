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

package analyse

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/directive"
	"seagull.sh/seagull/internal/resolve"
	"seagull.sh/seagull/internal/sqlfile"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// keywordDistinct opens a quantifier that compares the result rows.
	keywordDistinct = "DISTINCT"

	// keywordOn follows DISTINCT in a quantifier that compares only its own keys.
	keywordOn = "ON"

	// maxPortableBound is the largest value an int holds on every platform Go builds for,
	// where an int may be 32 bits wide.
	maxPortableBound = math.MaxInt32

	// roleParameter names the directive that binds a parameter, rendered with the canonical
	// directive prefix in diagnostic messages.
	roleParameter = "param"
)

// paginationBound is the default or the max a LIMIT or OFFSET parameter's directive gives
// it.
type paginationBound struct {
	// value is the bound, or nil when the directive gives none.
	value *int

	// key is the keyword argument that gives it, default or max.
	key string
}

// assembleQueryInput groups the inputs needed to construct an AnalysedQuery result.
type assembleQueryInput struct {
	// directives holds the parsed query-level directives such as group_by and nullable
	// overrides.
	directives *directive.QueryDirectives

	// rawAnalysis holds the raw analysis result produced by the engine.
	rawAnalysis *ast.RawQueryAnalysis

	// queryName holds the name declared by the query directive.
	queryName string

	// filename holds the source file path for error reporting.
	filename string

	// outputColumns holds the resolved output columns for the query.
	outputColumns []sqlmodel.OutputColumn

	// parameters holds the resolved query parameters.
	parameters []sqlmodel.QueryParameter

	// origins holds, parallel to outputColumns, the index of the select-list or RETURNING
	// item each column came from.
	origins []int

	// referable holds, parallel to outputColumns, whether a column a star stands for can be
	// named in place of the star.
	referable []bool

	// namedItems lists the select-list items a later item names by their alias, whose
	// columns the template writes as they are.
	namedItems []int

	// block holds the raw query block containing SQL text and line information.
	block sqlfile.QueryBlock

	// location locates the query's statements in its file.
	location statementLocation

	// itemsWrapCount reports a select-list item that holds a window function, or a call of
	// an aggregate or set-returning function, outside any subquery.
	itemsWrapCount bool

	// orderByAddsRows reports an ORDER BY that calls a set-returning function outside any
	// subquery, which adds a row for each element of the set.
	orderByAddsRows bool

	// queryCommand holds the command type declared by the query directive.
	queryCommand sqlmodel.QueryCommand

	// isDynamic indicates whether the query requires dynamic SQL generation.
	isDynamic bool
}

// assembly is what assembling a query found besides the query itself.
type assembly struct {
	// diagnostics holds the problems building the query's templates found.
	diagnostics []diag.SourceError

	// countWrapped reports a count template that wraps the query in a subquery, so the count
	// is over the query's result rows.
	countWrapped bool
}

// assembleQuery constructs an AnalysedQuery from the provided input fields, its
// executable SQL as templates built from the statement's spans among them.
//
// Takes input (*assembleQueryInput) which holds all resolved query components.
//
// Returns *sqlmodel.AnalysedQuery which holds the fully assembled query ready for code
// generation.
// Returns assembly which says whether its count wraps it and holds the problems its
// templates showed.
func (a *QueryAnalyser) assembleQuery(input *assembleQueryInput) (*sqlmodel.AnalysedQuery, assembly) {
	readOnly := input.rawAnalysis.ReadOnly
	if input.directives.ReadOnlyOverride != nil {
		readOnly = *input.directives.ReadOnlyOverride
	}

	query := &sqlmodel.AnalysedQuery{
		Name:                  input.queryName,
		Description:           input.directives.Description,
		Command:               input.queryCommand,
		SQL:                   input.block.SQL,
		Filename:              input.filename,
		Line:                  input.block.StartLine,
		OutputColumns:         input.outputColumns,
		Parameters:            input.parameters,
		IsDynamic:             input.isDynamic,
		GroupByKey:            input.directives.GroupByKeys,
		InsertColumns:         input.rawAnalysis.InsertColumns,
		DynamicRuntime:        input.directives.DynamicRuntime,
		ReadsResultRows:       false,
		ReadOnly:              readOnly,
		Optional:              input.directives.Optional,
		AllowedColumns:        nil,
		CountTemplate:         nil,
		InsertTarget:          sqlmodel.TableName{},
		FilteredTemplate:      nil,
		FilteredCountTemplate: nil,
		Template:              sqlmodel.SQLTemplate{},
		RowType:               "",
		RowModel:              sqlmodel.TableName{},
		ResultShape:           resultShape(input),
	}

	if input.directives.DynamicRuntime {
		query.AllowedColumns = runtimeAllowedColumns(input)
	}

	return query, a.buildTemplates(query, input)
}

// runtimeAllowedColumns returns the columns a runtime builder may filter and order the
// query by, unqualified output names for a compound statement.
//
// Takes input (*assembleQueryInput) which holds the statement's analysis and output
// columns.
//
// Returns []sqlmodel.AllowedColumn which holds the allowed columns.
func runtimeAllowedColumns(input *assembleQueryInput) []sqlmodel.AllowedColumn {
	if len(input.rawAnalysis.CompoundBranches) > 0 {
		return extractCompoundAllowedColumns(input.outputColumns)
	}
	return extractAllowedColumns(input.outputColumns)
}

// resultShape records how a statement's own clauses use its result columns. It notes a
// compound statement, a DISTINCT quantifier, an ORDER BY or GROUP BY position, and an
// ORDER BY output name.
//
// Takes input (*assembleQueryInput) which holds the statement's analysis, text and output
// columns.
//
// Returns sqlmodel.ResultShape which describes the uses.
func resultShape(input *assembleQueryInput) sqlmodel.ResultShape {
	raw := input.rawAnalysis
	shape := sqlmodel.ResultShape{Named: nil, Distinct: distinctQuantifier(input), Compound: len(raw.CompoundBranches) > 0, Ordinal: false}
	for index := range raw.ClauseExpressions {
		item := &raw.ClauseExpressions[index]
		ordering := item.Clause.Includes(ast.ClauseOrderBy)
		if !ordering && !item.Clause.Includes(ast.ClauseGroupBy) {
			continue
		}
		switch expression := item.Expression.(type) {
		case *ast.LiteralExpression:
			shape.Ordinal = true
		case *ast.ColumnRefExpression:
			if ordering && expression.TableAlias == "" {
				shape.Named = namedOutputs(shape.Named, input.outputColumns, expression.ColumnName)
			}
		}
	}
	return shape
}

// distinctQuantifier reports whether the statement's quantifier is a plain DISTINCT,
// which compares every output column, rather than DISTINCT ON, which compares only its
// keys read from the source columns, ALL or none.
//
// Takes input (*assembleQueryInput) which holds the statement's spans and text.
//
// Returns bool which is true for a DISTINCT quantifier.
func distinctQuantifier(input *assembleQueryInput) bool {
	quantifier := input.rawAnalysis.Spans.Quantifier
	if !quantifier.Known() || len(input.location.statements) == 0 {
		return false
	}
	start := input.location.statements[len(input.location.statements)-1].Location
	if start+quantifier.End > len(input.block.SQL) {
		return false
	}
	written := strings.TrimSpace(input.block.SQL[start+quantifier.Start : start+quantifier.End])
	if len(written) < len(keywordDistinct) || !strings.EqualFold(written[:len(keywordDistinct)], keywordDistinct) {
		return false
	}
	return !opensDistinctOn(strings.Fields(written[len(keywordDistinct):]))
}

// opensDistinctOn reports whether the words after DISTINCT start an ON list.
//
// Takes rest ([]string) which holds the words after DISTINCT.
//
// Returns bool which is true when the first word is ON, alone or followed by its list.
func opensDistinctOn(rest []string) bool {
	if len(rest) == 0 {
		return false
	}
	first := strings.ToUpper(rest[0])
	return first == keywordOn || strings.HasPrefix(first, keywordOn+"(")
}

// namedOutputs adds the result columns an ORDER BY name matches to those already found.
//
// Takes named ([]int) which holds the columns found so far.
// Takes outputs ([]sqlmodel.OutputColumn) which are the result columns.
// Takes name (string) which is the name the ORDER BY writes.
//
// Returns []int which holds the columns found, each once.
func namedOutputs(named []int, outputs []sqlmodel.OutputColumn, name string) []int {
	for column := range outputs {
		if strings.EqualFold(outputs[column].Name, name) && !slices.Contains(named, column) {
			named = append(named, column)
		}
	}
	return named
}

// applyQueryColumnOverrides applies each `-- sgl.column(name, ...)` directive declared in
// the query header to the resolved output columns.
//
// For each override the Name is matched case-insensitively against the output column
// names, and a miss produces Q036 with a Levenshtein suggestion drawn from the actual
// column names. When `type:` was declared, the engine normalises the value into a
// structured SQLType that replaces the column's inferred type. When `nullable:` was
// declared, the column's Nullable flag is replaced. When `go_type:` was declared, the
// type the directive parser read from it, when the generator can write it, is stored on
// OutputColumn.GoTypeOverride for the emitter to consume.
//
// Takes outputColumns ([]sqlmodel.OutputColumn) which holds the resolved output columns
// to override.
// Takes directiveBlock (*directive.DirectiveBlock) which holds the parsed column
// overrides from the query header.
// Takes filename (string) which is the source file path for diagnostic locations.
// Takes diagnostics (*[]diag.SourceError) which collects any unknown-column diagnostics
// produced.
//
// Returns []sqlmodel.OutputColumn which is the updated output column slice.
func (a *QueryAnalyser) applyQueryColumnOverrides(
	outputColumns []sqlmodel.OutputColumn,
	directiveBlock *directive.DirectiveBlock,
	filename string,
	diagnostics *[]diag.SourceError,
) []sqlmodel.OutputColumn {
	if directiveBlock == nil || len(directiveBlock.ColumnOverrides) == 0 {
		return outputColumns
	}

	columnNames := make([]string, 0, len(outputColumns))
	for index := range outputColumns {
		columnNames = append(columnNames, outputColumns[index].Name)
	}
	errorBuilder := diagbuild.NewErrorBuilder(filename)

	for _, override := range directiveBlock.ColumnOverrides {
		matched := findOutputColumnByName(outputColumns, override.Name)
		if matched == -1 {
			*diagnostics = append(*diagnostics, errorBuilder.UnknownOverrideColumn(override.NameSpan, a.prefixes.Qualify(directive.RoleColumn), override.Name, columnNames))
			continue
		}
		a.applyOverrideToColumn(&outputColumns[matched], override)
	}
	return outputColumns
}

// applyOverrideToColumn copies the SQL type, nullable, Go-type override and struct tag
// fields from a parsed query-level directive onto the matched output column. Empty values
// leave the underlying column field unchanged, matching the directive parser's
// optionality, as does a go_type the directive parser could not read, which it reports.
//
// Takes column (*sqlmodel.OutputColumn) which receives the override fields.
// Takes override (*directive.ColumnOverride) which holds the parsed directive values.
func (a *QueryAnalyser) applyOverrideToColumn(column *sqlmodel.OutputColumn, override *directive.ColumnOverride) {
	if override.SQLType != "" {
		column.SQLType = a.engine.NormaliseTypeName(override.SQLType)
	}
	if override.Nullable != nil {
		column.Nullable = *override.Nullable
	}
	if override.ParsedGoType != nil {
		column.GoTypeOverride = new(*override.ParsedGoType)
	}
	if override.StructTags != nil {
		column.StructTags = maps.Clone(override.StructTags)
	}
}

// propagateCatalogueColumnOverrides walks the output columns and, for each one whose
// lineage traces back to a catalogue column via direct projection (SourceTable +
// SourceColumn both set), applies any migration-level override declared on the catalogue
// column.
//
// Function calls and computed expressions leave SourceTable empty, so the overrides drop
// on those; a cast the dialect traces back to the column keeps them only when it returns
// the column as its own type, as resolve.RestatesColumnType judges it, and one to another
// type keeps only a nullable override. Query-level overrides already applied by
// applyQueryColumnOverrides take precedence: this pass only fills slots that are still
// unmodified by the directive block.
//
// Takes outputColumns ([]sqlmodel.OutputColumn) which holds the resolved output columns.
// Takes directiveBlock (*directive.DirectiveBlock) which holds the query-level overrides
// that take precedence over catalogue overrides.
//
// Returns []sqlmodel.OutputColumn which is the updated output column slice.
func (a *QueryAnalyser) propagateCatalogueColumnOverrides(
	outputColumns []sqlmodel.OutputColumn,
	directiveBlock *directive.DirectiveBlock,
) []sqlmodel.OutputColumn {
	queryLevelNames := make(map[string]struct{}, len(directiveBlock.ColumnOverrides))
	for _, override := range directiveBlock.ColumnOverrides {
		queryLevelNames[strings.ToLower(override.Name)] = struct{}{}
	}

	for columnIndex := range outputColumns {
		a.propagateOverrideToOutputColumn(&outputColumns[columnIndex], queryLevelNames)
	}
	return outputColumns
}

// propagateOverrideToOutputColumn applies catalogue overrides to a directly projected
// column.
//
// Query-level overrides take precedence, and a query-level directive's struct tags are
// merged over the catalogue column's key by key. A cast to a different SQL type inherits
// only the nullability override and the struct tags.
//
// Takes column (*sqlmodel.OutputColumn) which receives the catalogue override.
// Takes queryLevelNames (map[string]struct{}) which holds the lower-cased names already
// overridden at the query level.
func (a *QueryAnalyser) propagateOverrideToOutputColumn(column *sqlmodel.OutputColumn, queryLevelNames map[string]struct{}) {
	if column.SourceTable == "" || column.SourceColumn == "" {
		return
	}
	catalogueColumn := a.catalogue.FindColumn(column.SourceSchema, column.SourceTable, column.SourceColumn)
	if catalogueColumn == nil {
		return
	}
	column.StructTags = withCatalogueStructTags(catalogueColumn.StructTags, column.StructTags)
	if _, queryOverridden := queryLevelNames[strings.ToLower(column.Name)]; queryOverridden {
		return
	}
	if catalogueColumn.NullableOverride != nil {
		column.Nullable = *catalogueColumn.NullableOverride
	}
	if !resolve.RestatesColumnType(column.SQLType, catalogueColumn.ValueType()) {
		return
	}
	if catalogueColumn.SQLTypeOverride != "" {
		column.SQLType = a.engine.NormaliseTypeName(catalogueColumn.SQLTypeOverride)
	}
	if catalogueColumn.GoTypeOverride != nil && column.GoTypeOverride == nil {
		column.GoTypeOverride = new(*catalogueColumn.GoTypeOverride)
	}
}

// applyElementNullability applies column, type and preset rules to nullable collection
// elements.
//
// Rules apply in order: column directive, catalogue column override, what the analysis
// knows of the elements, SQL type override, then preset.
//
// Takes outputColumns ([]sqlmodel.OutputColumn) which holds the resolved output columns.
// Takes elementsKnown ([]bool) which holds, parallel to outputColumns, whether the
// nullability a column's type gives its elements is what the query returns, as for
// array_agg over a column that is never NULL; such elements stay as the type says.
// Takes directiveBlock (*directive.DirectiveBlock) which holds the query-level column
// directives.
//
// Returns []sqlmodel.OutputColumn which is the updated output column slice.
func (a *QueryAnalyser) applyElementNullability(
	outputColumns []sqlmodel.OutputColumn,
	elementsKnown []bool,
	directiveBlock *directive.DirectiveBlock,
) []sqlmodel.OutputColumn {
	if a.elements == nil {
		return outputColumns
	}
	for index := range outputColumns {
		column := &outputColumns[index]
		choice := a.columnElementChoice(column, directiveBlock)
		if choice == nil && index < len(elementsKnown) && elementsKnown[index] && provenNonNullElements(column.SQLType) {
			choice = new(true)
		}
		column.SQLType = a.elements.Apply(column.SQLType, choice)
	}
	return outputColumns
}

// applyParameterElementNullability resolves nullability for parameter array elements and
// map values from engine support, type overrides and the preset. Explicit declarations
// take priority, and slice parameters are unchanged.
//
// Takes parameters ([]sqlmodel.QueryParameter) which holds the resolved parameters,
// changed in place.
//
// Returns []sqlmodel.QueryParameter which is the updated parameter slice.
func (a *QueryAnalyser) applyParameterElementNullability(parameters []sqlmodel.QueryParameter) []sqlmodel.QueryParameter {
	elements := a.parameterElements
	if elements == nil {
		elements = a.elements
	}
	if elements == nil {
		return parameters
	}
	for index := range parameters {
		parameter := &parameters[index]
		if !parameter.IsSlice {
			parameter.SQLType = elements.Apply(parameter.SQLType, parameter.NonNullElements)
		}
	}
	return parameters
}

// paginationBoundDiagnostics reports the default or max of a LIMIT or OFFSET parameter
// that the field the generated code gives the parameter cannot hold on every platform.
//
// Generated code stores the parameter in an int and compares it with a constant. Values
// above maxPortableBound would prevent that code from compiling on platforms where int is
// 32 bits wide.
//
// Takes parameters ([]sqlmodel.QueryParameter) which are the query's parameters.
// Takes parameterDirectives ([]*directive.ParameterDirective) which are the directives
// that gave them their defaults and maximums, where the problems are placed.
// Takes location (statementLocation) which places a problem whose directive was not
// located.
//
// Returns []diag.SourceError which holds a Q028 error for each such value, or nil.
func (a *QueryAnalyser) paginationBoundDiagnostics(
	parameters []sqlmodel.QueryParameter,
	parameterDirectives []*directive.ParameterDirective,
	location statementLocation,
) []diag.SourceError {
	var diagnostics []diag.SourceError
	for index := range parameters {
		parameter := &parameters[index]
		if !parameter.IsPaginationBound() {
			continue
		}
		for _, bound := range []paginationBound{{key: "default", value: parameter.DefaultLimit}, {key: "max", value: parameter.MaxLimit}} {
			if bound.value == nil || *bound.value <= maxPortableBound {
				continue
			}
			span := diagbuild.TextSpan{Line: location.startLine, Column: 1, EndLine: 0, EndColumn: 0}
			if parameterDirective := numberedDirective(parameterDirectives, parameter.Number); parameterDirective != nil &&
				parameterDirective.Span.Line > 0 {
				span = parameterDirective.Span
			}
			diagnostics = append(diagnostics, diagbuild.Located(location.filename, span, diag.CodeInvalidKeywordArgumentValue,
				diag.SeverityError, fmt.Sprintf(
					"%s: %s %d does not fit the int field the generated code gives a LIMIT or OFFSET parameter, which holds at "+
						"most %d on 32-bit platforms; lower %s",
					a.prefixes.Qualify(roleParameter), bound.key, *bound.value, maxPortableBound, bound.key), ""))
		}
	}
	return diagnostics
}

// columnElementChoice returns what an output column declares about its elements: its own
// sgl.column directive's non_null_elements, or else that of the catalogue column it
// projects as its own type, or nil when neither declares it.
//
// Takes column (*sqlmodel.OutputColumn) which is the output column.
// Takes directiveBlock (*directive.DirectiveBlock) which holds the query-level column
// directives.
//
// Returns *bool which is the declared choice, or nil.
func (a *QueryAnalyser) columnElementChoice(column *sqlmodel.OutputColumn, directiveBlock *directive.DirectiveBlock) *bool {
	for _, override := range directiveBlock.ColumnOverrides {
		if override.NonNullElements != nil && strings.EqualFold(override.Name, column.Name) {
			return override.NonNullElements
		}
	}
	if column.SourceTable == "" || column.SourceColumn == "" {
		return nil
	}
	catalogueColumn := a.catalogue.FindColumn(column.SourceSchema, column.SourceTable, column.SourceColumn)
	if catalogueColumn == nil || !resolve.RestatesColumnType(column.SQLType, catalogueColumn.ValueType()) {
		return nil
	}
	return catalogueColumn.NonNullElements
}

// isDynamicQuery reports whether a query's resolved parameters make it dynamic.
//
// A query is dynamic when it has a sortable input, an optional parameter (bound through a
// pointer the caller may leave nil; the SQL itself is unchanged), or a LIMIT/OFFSET
// parameter that carries clamp configuration (a default or max), which needs the
// params-struct / clamp path. A LIMIT/OFFSET parameter without clamp config is a
// positional bind and does not make the query dynamic.
//
// Takes parameters ([]sqlmodel.QueryParameter) which holds the resolved parameters.
//
// Returns bool which indicates whether any parameter makes the query dynamic.
func isDynamicQuery(parameters []sqlmodel.QueryParameter) bool {
	for index := range parameters {
		parameter := &parameters[index]
		switch {
		case parameter.Kind == sqlmodel.ParameterDirectiveSortable, parameter.IsOptional:
			return true
		case parameter.IsPaginationBound() && (parameter.DefaultLimit != nil || parameter.MaxLimit != nil):
			return true
		}
	}
	return false
}

// extractAllowedColumns returns the columns a dynamic runtime builder may filter and
// order by, restricted to the query's SELECT projection.
//
// A dynamic query can only be filtered and ordered over the data it already returns. Each
// allowed column is keyed on its output (result-set) name, which is what a caller passes
// to Where and OrderBy, and carries the qualified source expression the builder emits in
// its place (for example output "email" -> "users.email"). Qualifying with the table
// reference keeps the emitted filter unambiguous across joins and makes it reference the
// real column rather than the caller's text. Output columns that are computed expressions
// or aggregates carry no source column and are excluded, since they cannot be referenced
// as a bare identifier in WHERE or ORDER BY. Scoping to the projection keeps the dynamic
// surface from exposing a column the query never selects, so a sensitive column that is
// present on a FROM or JOIN table but absent from the SELECT can never become a runtime
// filter or ordering oracle.
//
// Takes outputColumns ([]sqlmodel.OutputColumn) which holds the query's projected
// columns.
//
// Returns []sqlmodel.AllowedColumn which holds the deduplicated set of columns available
// for dynamic filtering and ordering, keyed on output name.
func extractAllowedColumns(outputColumns []sqlmodel.OutputColumn) []sqlmodel.AllowedColumn {
	return allowedColumns(outputColumns, func(column *sqlmodel.OutputColumn, _ string) (string, string) {
		return column.SourceQualifier, column.SourceColumn
	})
}

// extractCompoundAllowedColumns returns the columns a runtime builder may filter and
// order a compound statement, such as a UNION ALL, or a statement read from a derived
// table, by. No table of the statement is in scope of its result, so each column is its
// output name, unqualified.
//
// Takes outputColumns ([]sqlmodel.OutputColumn) which holds the query's projected
// columns.
//
// Returns []sqlmodel.AllowedColumn which holds the deduplicated set of columns, keyed on
// output name.
func extractCompoundAllowedColumns(outputColumns []sqlmodel.OutputColumn) []sqlmodel.AllowedColumn {
	return allowedColumns(outputColumns, func(_ *sqlmodel.OutputColumn, outputName string) (string, string) {
		return "", outputName
	})
}

// allowedColumns lists the output columns that carry a source column, once per output
// name, reading each one's qualifier and column through sourceOf.
//
// Takes outputColumns ([]sqlmodel.OutputColumn) which holds the query's projected
// columns.
// Takes sourceOf (func(...) (string, string)) which returns the qualifier and column a
// builder writes for a column and its output name.
//
// Returns []sqlmodel.AllowedColumn which holds the allowed columns.
func allowedColumns(outputColumns []sqlmodel.OutputColumn,
	sourceOf func(column *sqlmodel.OutputColumn, outputName string) (string, string)) []sqlmodel.AllowedColumn {
	var allowed []sqlmodel.AllowedColumn
	seen := make(map[string]struct{})

	for index := range outputColumns {
		column := &outputColumns[index]
		if column.SourceColumn == "" {
			continue
		}

		outputName := column.Name
		if outputName == "" {
			outputName = column.SourceColumn
		}
		if _, exists := seen[outputName]; exists {
			continue
		}
		seen[outputName] = struct{}{}

		qualifier, source := sourceOf(column, outputName)
		sourceExpression := source
		if qualifier != "" {
			sourceExpression = qualifier + "." + source
		}

		allowed = append(allowed, sqlmodel.AllowedColumn{
			Name:             outputName,
			SourceExpression: sourceExpression,
			SourceQualifier:  qualifier,
			SourceColumn:     source,
			SQLType:          column.SQLType,
		})
	}

	return allowed
}

// findOutputColumnByName scans outputColumns for the first entry whose Name matches
// target case-insensitively.
//
// Takes outputColumns ([]sqlmodel.OutputColumn) which holds the columns to scan.
// Takes target (string) which is the column name to match.
//
// Returns int which is the matching index, or -1 when no match exists.
func findOutputColumnByName(outputColumns []sqlmodel.OutputColumn, target string) int {
	for index := range outputColumns {
		if strings.EqualFold(outputColumns[index].Name, target) {
			return index
		}
	}
	return -1
}

// withCatalogueStructTags returns an output column's struct tags: those of the catalogue
// column it reads, with the query-level directive's over them key by key.
//
// Takes catalogue (map[string]string) which holds the catalogue column's tags, or nil.
// Takes query (map[string]string) which holds the query-level directive's tags, or nil.
//
// Returns map[string]string which holds the merged tags, or nil when neither has any.
func withCatalogueStructTags(catalogue, query map[string]string) map[string]string {
	if len(catalogue) == 0 {
		return query
	}
	merged := maps.Clone(catalogue)
	maps.Copy(merged, query)
	return merged
}

// provenNonNullElements reports whether a type's elements, and a map's values, are marked
// never NULL, which for a column whose elements the analysis knows means the query
// returns none.
//
// Takes sqlType (sqlmodel.SQLType) which is the column's type.
//
// Returns bool which is true for an array, list or map whose elements are unmarked.
func provenNonNullElements(sqlType sqlmodel.SQLType) bool {
	return sqlType.ElementType != nil && !sqlType.ElementType.Nullable &&
		(sqlType.Category == sqlmodel.TypeCategoryArray || sqlType.Category == sqlmodel.TypeCategoryMap)
}

// numberedDirective finds the directive of a parameter by its number.
//
// Takes parameterDirectives ([]*directive.ParameterDirective) which are the directives.
// Takes number (int) which is the parameter's number.
//
// Returns *directive.ParameterDirective which is the directive, or nil when none has the
// number.
func numberedDirective(parameterDirectives []*directive.ParameterDirective, number int) *directive.ParameterDirective {
	for _, parameterDirective := range parameterDirectives {
		if parameterDirective != nil && parameterDirective.Number == number {
			return parameterDirective
		}
	}
	return nil
}

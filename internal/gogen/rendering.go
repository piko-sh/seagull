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
	"errors"
	"fmt"
	"go/ast"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// textCastOpen opens the cast that reads a column as the dialect's text type.
	textCastOpen = "CAST("

	// textCastMiddle separates the cast column from the text type.
	textCastMiddle = " AS "

	// textCastClose closes the cast.
	textCastClose = ")"

	// jsonCallClose closes the call of the dialect's to-JSON function.
	jsonCallClose = ")"

	// sliceTypePrefix starts the name of a Go slice type.
	sliceTypePrefix = "[]"

	// pointerTypePrefix starts the name of a Go pointer type.
	pointerTypePrefix = "*"

	// placeholderQualifier stands for a column's table in a suggestion when the column has
	// no single source table.
	placeholderQualifier = "its_table"
)

var (
	// errUnnamedQuery reports a query the package's naming plan holds no names for.
	errUnnamedQuery = errors.New("the naming plan holds no names for the query")

	// errNoTemplate reports a query without an SQL template to generate from.
	errNoTemplate = errors.New("the query has no SQL template")

	// arrayTextElements lists the Go types of the slices the array scanner reads an array's
	// text form into.
	arrayTextElements = map[sqlmodel.SQLTypeCategory][]string{
		sqlmodel.TypeCategoryText:    {"string"},
		sqlmodel.TypeCategoryUUID:    {"string"},
		sqlmodel.TypeCategoryInteger: {"int64", "int32", "int16", "int"},
		sqlmodel.TypeCategoryFloat:   {"float64", "float32"},
		sqlmodel.TypeCategoryBoolean: {"bool"},
	}

	// arrayLiteralElements lists the Go types of the elements of a slice the generated
	// sglArrayLiteral writes as an array's text form, by the SQL type category of the
	// elements.
	arrayLiteralElements = map[sqlmodel.SQLTypeCategory][]string{
		sqlmodel.TypeCategoryText:    {"string"},
		sqlmodel.TypeCategoryUUID:    {"string"},
		sqlmodel.TypeCategoryInteger: {"int64", "int32", "int16", "int"},
		sqlmodel.TypeCategoryBoolean: {"bool"},
	}
)

// renderKey identifies immutable SQL within one emission. The dialect, driver and style
// are fixed for that emission; a prefix distinguishes otherwise identical SQL.
type renderKey struct {
	// query identifies the analysed query for this emission.
	query *sqlmodel.AnalysedQuery

	// prefix is the identifying SQL comment prepended to the query.
	prefix string
}

// renderData contains no file-specific import tracker or mutable builder state.
type renderData struct {
	// count retains the rendered count-query form when requested.
	count *emitkit.Rendered

	// filtered retains the rendered filtered form when the query has one.
	filtered *emitkit.Rendered

	// filteredCount retains the rendered filtered count form when the query has one.
	filteredCount *emitkit.Rendered

	// sql retains the rendered executable query form.
	sql emitkit.Rendered
}

// RewriteConflict describes a result column whose use by the query would change if the
// select list converted it to text or JSON.
type RewriteConflict struct {
	// Reason says how the query uses the column.
	Reason string

	// Suggestion says how to write the query so the column keeps its meaning.
	Suggestion string
}

// Query prepares a query's names, renderer and SQL for the driver's builders, including
// count SQL when supported. When query-name lookup needs to distinguish identical SQL, a
// comment identifies the query.
//
// Takes analysed (*sqlmodel.AnalysedQuery) which is one of the request's queries.
// Takes imports (*goast.ImportTracker) which collects the imports of the file the query
// is written to.
//
// Returns *Query which holds the query, its names and its rendered SQL.
// Returns error when the plan holds no names for the query, the query has no template, or
// a template does not render.
func (e *Emission) Query(analysed *sqlmodel.AnalysedQuery, imports *goast.ImportTracker) (*Query, error) {
	prefixes, err := e.sharedSQLPrefixes()
	if err != nil {
		return nil, err
	}
	return e.renderQuery(analysed, imports, prefixes[analysed])
}

// Renderer returns how a query's SQL is written for the driver.
//
// The placeholder form is the one the query executes in, emitkit.ExecutionForm. Slice
// lists are written in the driver's list mode, with two exceptions the data decides: a
// driver binding slices as arrays expands them when the dialect has no array form, and a
// list in a form that binds by name, which cannot be spliced per element, is bound as
// written. A sortable query leaves out its base ORDER BY, which the generated code
// replaces, and the columns the driver's ColumnReader reads through a wrap are wrapped:
// in the dialect's to-JSON function for a column read as JSON whose Go type is the
// default slice, map or struct, and in a cast to the dialect's text type for one read as
// text, unless the column has a Go type of its own or the query's own clauses use it in a
// way the wrap would change, which RewriteConflict reports. A column of an embed is
// wrapped like any other. A star is expanded into its columns unless the request's style
// keeps stars.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns *emitkit.Renderer which writes the query's templates.
func (e *Emission) Renderer(query *sqlmodel.AnalysedQuery) *emitkit.Renderer {
	info := e.Dialect()
	form := emitkit.ExecutionForm(info, query)
	renderer := &emitkit.Renderer{
		WrapColumn:   nil,
		Query:        query,
		Quote:        info.IdentifierQuote,
		Lists:        info.Lists,
		Form:         form,
		Placeholders: info.Placeholders,
		Omit:         0,
		ListMode:     e.listMode(query, form),
		Prefix:       "",
		KeepStars:    e.Request.Style.KeepStars,
	}
	if query.IsDynamic && sortableParameter(query) != nil {
		renderer.Omit = emitkit.NewSegmentSet(sqlmodel.SegmentOrderBy)
	}
	if e.Driver.Reader != nil {
		renderer.WrapColumn = func(column int) (emitkit.ColumnWrap, bool) {
			if _, conflicts := e.RewriteConflict(query, column); conflicts {
				return emitkit.ColumnWrap{}, false
			}
			return e.columnWrap(&query.OutputColumns[column])
		}
	}
	return renderer
}

// InExecutionForm reports whether a query is rendered in the dialect's execution form,
// where the driver's Binder applies. A runtime builder of a dialect whose execution form
// binds by name is rendered in the positional form instead, and binds its values through
// the driver's PositionalBinder, or as they are.
//
// Takes query (*Query) which is the query.
//
// Returns bool which is true when the query's renderer writes the execution form, or the
// query has no renderer.
func (e *Emission) InExecutionForm(query *Query) bool {
	return query.Renderer == nil || query.Renderer.Form == e.Dialect().Placeholders.Execution
}

// BindForm returns the numbered or anonymous form the generated code writes the
// placeholders it adds at run time in, for a slice's elements and a runtime builder's
// predicates and pagination: the dialect's Execution form, or its positional form when
// Execution binds by name.
//
// Returns dialect.PlaceholderSigil which is the form.
func (e *Emission) BindForm() dialect.PlaceholderSigil {
	placeholders := &e.Dialect().Placeholders
	if placeholders.BindsByName() {
		return placeholders.PositionalForm()
	}
	return placeholders.Execution
}

// NeedsJSONWrap reports whether a column requires the dialect's JSON conversion.
//
// It reports whether the driver reads an output column through the dialect's to-JSON
// function: a column the driver reads as JSON whose Go type is the default slice, map or
// struct, or an array whose Go override is a slice. Generation fails for such a column
// the query's template cannot wrap.
//
// Takes column (*sqlmodel.OutputColumn) which is the output column.
//
// Returns bool which is true when the column must be wrapped to be read.
func (e *Emission) NeedsJSONWrap(column *sqlmodel.OutputColumn) bool {
	wrap, wraps := e.columnWrap(column)
	return wraps && wrap.Kind == emitkit.ColumnWrapJSON
}

// RewriteConflict reports whether a query's own clauses use one of its result columns in
// a way a rewrite in the select list would change. ORDER BY its output name or a position
// sorts the rewritten value, DISTINCT compares it, and a compound statement's other
// branches are not rewritten at all.
//
// DISTINCT keeps its rows over a decimal cast to text, which is written at the decimal's
// fixed scale, so that rewrite does not conflict with it.
//
// Takes query (*sqlmodel.AnalysedQuery) which holds the columns and the shape of the
// statement.
// Takes index (int) which is the result column.
//
// Returns RewriteConflict which describes the conflict.
// Returns bool which is true when the driver would rewrite the column and the query uses
// it so.
func (e *Emission) RewriteConflict(query *sqlmodel.AnalysedQuery, index int) (RewriteConflict, bool) {
	column := &query.OutputColumns[index]
	wrap, wraps := e.columnWrap(column)
	if !wraps {
		return RewriteConflict{}, false
	}
	shape := &query.ResultShape
	decimalText := wrap.Kind == emitkit.ColumnWrapText && column.SQLType.Category == sqlmodel.TypeCategoryDecimal
	switch {
	case shape.Compound:
		return RewriteConflict{
			Reason:     "a UNION, INTERSECT or EXCEPT, whose other branches the rewrite does not reach",
			Suggestion: "select the compound statement's rows from a derived table, such as SELECT ... FROM (... UNION ...) AS rows",
		}, true
	case shape.Distinct && !decimalText:
		return RewriteConflict{
			Reason:     "DISTINCT, which would compare the rewritten value rather than the column's own",
			Suggestion: "apply DISTINCT in a derived table and select its columns, such as SELECT ... FROM (SELECT DISTINCT ...) AS rows",
		}, true
	case shape.Ordinal:
		return RewriteConflict{
			Reason:     "an ORDER BY or GROUP BY position, which would use the rewritten value rather than the column's own",
			Suggestion: "name the column by its table instead of its position, such as ORDER BY " + qualifiedSource(column),
		}, true
	case slices.Contains(shape.Named, index):
		return RewriteConflict{
			Reason:     "an ORDER BY of its output name, which would sort the rewritten value rather than the column's own",
			Suggestion: "qualify the column with its table, such as ORDER BY " + qualifiedSource(column),
		}, true
	default:
		return RewriteConflict{}, false
	}
}

// ReadsConverted reports whether an output column uses the driver's converting scan
// adapter.
//
// Column-specific Go overrides are excluded. Adapted reading accepts any other Go type;
// ordinary converted reading needs a supported slice, map or struct.
//
// Takes column (*sqlmodel.OutputColumn) which is the output column.
//
// Returns bool which is true when the column is read through the adapter.
func (e *Emission) ReadsConverted(column *sqlmodel.OutputColumn) bool {
	if !e.converts(column) {
		return false
	}
	switch e.Reading(column.SQLType) {
	case ColumnReadConverted:
		return usesDefaultComposite(e.Request, column)
	case ColumnReadAdapted:
		return true
	default:
		return false
	}
}

// ConvertsColumn reports columns read through conversion instead of a SQL text cast.
//
// Takes column (*sqlmodel.OutputColumn) which is the output column.
// Takes wrapped (bool) which reports that the query's SQL wraps the column.
//
// Returns bool which is true when the column is read through the adapter.
func (e *Emission) ConvertsColumn(column *sqlmodel.OutputColumn, wrapped bool) bool {
	if e.ReadsConverted(column) {
		return true
	}
	return !wrapped && e.converts(column) && e.Reading(column.SQLType) == ColumnReadAsText
}

// ConvertsAnyColumn reports whether the generated code reads any output column of the
// package's queries through the driver's converting scan adapter or into a raw local it
// converts after the scan, so the package needs the helper that converts.
//
// Returns bool which is true when some column is read so.
// Returns error when a query cannot be rendered.
func (e *Emission) ConvertsAnyColumn() (bool, error) {
	_, converts := e.Driver.Reader.(ColumnConverter)
	_, readsRaw := e.Driver.Reader.(RawConverter)
	if !converts && !readsRaw {
		return false, nil
	}
	for _, analysed := range e.Request.Queries {
		query, err := e.Query(analysed, goast.NewImportTracker())
		if err != nil {
			return false, err
		}
		for index := range analysed.OutputColumns {
			_, wrapped := query.Rendered.Marks.Wrapped[index]
			if e.ConvertsColumn(&analysed.OutputColumns[index], wrapped) || e.ReadsRaw(&analysed.OutputColumns[index]) {
				return true, nil
			}
		}
	}
	return false, nil
}

// ReadsRaw reports columns scanned into driver-native locals and converted afterwards.
//
// It requires RawConverter and excludes overridden columns.
//
// Takes column (*sqlmodel.OutputColumn) which is the output column.
//
// Returns bool which is true when the column is read through a raw local.
func (e *Emission) ReadsRaw(column *sqlmodel.OutputColumn) bool {
	if column.GoTypeOverride != nil {
		return false
	}
	if _, readsRaw := e.Driver.Reader.(RawConverter); !readsRaw {
		return false
	}
	return e.Reading(column.SQLType) == ColumnReadRaw && usesDefaultComposite(e.Request, column)
}

// RawType returns the Go type of the raw local a column ReadsRaw reports is scanned into.
//
// Takes sqlType (sqlmodel.SQLType) which is the column's SQL type.
//
// Returns sqlmodel.GoType which is the raw local's Go type, or any when the driver has no
// raw converter.
func (e *Emission) RawType(sqlType sqlmodel.SQLType) sqlmodel.GoType {
	converter, readsRaw := e.Driver.Reader.(RawConverter)
	if !readsRaw {
		return sqlmodel.GoType{Composite: nil, Package: "", Name: goast.IdentAny}
	}
	return converter.RawType(sqlType)
}

// ConvertRaw returns the call that converts a raw local into a destination after the
// scan, returning an error when it cannot.
//
// Takes destination (ast.Expr) which is the address the column is read into.
// Takes raw (ast.Expr) which reads the raw local.
//
// Returns ast.Expr which is the call, or nil when the driver has no raw converter.
func (e *Emission) ConvertRaw(destination ast.Expr, raw ast.Expr) ast.Expr {
	converter, readsRaw := e.Driver.Reader.(RawConverter)
	if !readsRaw {
		return nil
	}
	return converter.ConvertRaw(destination, raw)
}

// ConvertingScan returns the scan argument that reads a column into a destination through
// the driver's converting scan adapter, or the destination itself when the driver has
// none.
//
// Takes destination (ast.Expr) which is the address the column is read into.
//
// Returns ast.Expr which is the scan argument.
func (e *Emission) ConvertingScan(destination ast.Expr) ast.Expr {
	converter, converts := e.Driver.Reader.(ColumnConverter)
	if !converts {
		return destination
	}
	return converter.ConvertingScan(destination)
}

// renderQuery returns what a driver's builders read about one query, its SQL written
// after a prefix.
//
// Takes analysed (*sqlmodel.AnalysedQuery) which is one of the request's queries.
// Takes imports (*goast.ImportTracker) which collects the imports of the file the query
// is written to.
// Takes prefix (string) which is written before the query's SQL and its count's, or is
// empty.
//
// Returns *Query which holds the query, its names and its rendered SQL.
// Returns error when the plan holds no names for the query, the query has no template, or
// a template does not render.
func (e *Emission) renderQuery(analysed *sqlmodel.AnalysedQuery, imports *goast.ImportTracker, prefix string) (*Query, error) {
	naming := e.Naming(analysed)
	if naming == nil {
		return nil, fmt.Errorf("query %q: %w", analysed.Name, errUnnamedQuery)
	}
	if len(analysed.Template.Segments) == 0 {
		return nil, fmt.Errorf("query %q: %w", analysed.Name, errNoTemplate)
	}
	renderer := e.Renderer(analysed)
	renderer.Prefix = prefix
	key := renderKey{query: analysed, prefix: prefix}
	if cached, found := e.rendered[key]; found {
		return &Query{Analysed: analysed, Naming: naming, Types: e.Request.Types, Imports: imports, Renderer: renderer, Rendered: cached.sql,
			Count: cached.count, Filtered: cached.filtered, FilteredCount: cached.filteredCount}, nil
	}
	rendered, err := emitkit.Render(analysed.Template, renderer)
	if err != nil {
		return nil, fmt.Errorf("query %q: %w", analysed.Name, err)
	}
	query := &Query{
		Analysed:      analysed,
		Naming:        naming,
		Types:         e.Request.Types,
		Imports:       imports,
		Renderer:      renderer,
		Rendered:      rendered,
		Count:         nil,
		Filtered:      nil,
		FilteredCount: nil,
	}
	if err := renderRuntimeForms(query); err != nil {
		return nil, err
	}
	if e.rendered == nil {
		e.rendered = make(map[renderKey]renderData)
	}
	e.rendered[key] = renderData{sql: query.Rendered, count: query.Count, filtered: query.Filtered, filteredCount: query.FilteredCount}
	return query, nil
}

// columnWrap returns how an output column is wrapped for the reading the driver needs, or
// false to leave it as it is.
//
// Takes column (*sqlmodel.OutputColumn) which is the output column.
//
// Returns emitkit.ColumnWrap which is the wrap.
// Returns bool which is false when the column is not wrapped.
func (e *Emission) columnWrap(column *sqlmodel.OutputColumn) (emitkit.ColumnWrap, bool) {
	info := e.Dialect()
	switch e.Reading(column.SQLType) {
	case ColumnReadAsJSON:
		if info.ToJSON == "" || !e.decodesDocument(column) || e.ReadsArrayText(column) {
			return emitkit.ColumnWrap{}, false
		}
		return emitkit.ColumnWrap{
			Affix: dialect.Affix{Before: info.ToJSON + "(", After: jsonCallClose},
			Kind:  emitkit.ColumnWrapJSON,
		}, true
	case ColumnReadAsText:
		if column.GoTypeOverride != nil || info.TextType == "" {
			return emitkit.ColumnWrap{}, false
		}
		return emitkit.ColumnWrap{
			Affix: dialect.Affix{Before: textCastOpen, After: textCastMiddle + info.TextType + textCastClose},
			Kind:  emitkit.ColumnWrapText,
		}, true
	default:
		return emitkit.ColumnWrap{}, false
	}
}

// ReadsArrayText reports whether generated code scans an array from text.
//
// The driver must support ArrayTextReader for the column, which must have no override.
// Its Go type must be a slice of mapped string, integer, float, or bool values or
// pointers to them.
//
// Takes column (*sqlmodel.OutputColumn) which is the output column.
//
// Returns bool which is true when the column is read through the array scanner.
func (e *Emission) ReadsArrayText(column *sqlmodel.OutputColumn) bool {
	reader, reads := e.Driver.Reader.(ArrayTextReader)
	if !reads || column.GoTypeOverride != nil || column.SQLType.ElementType == nil || !reader.ReadsArrayText(column.SQLType) {
		return false
	}
	resolved := goast.ResolveGoType(column.SQLType, column.Nullable, e.Request.Types)
	if resolved.Composite != nil || resolved.Package != "" {
		return false
	}
	element, sliced := strings.CutPrefix(resolved.Name, sliceTypePrefix)
	return sliced && slices.Contains(arrayTextElements[column.SQLType.ElementType.Category], strings.TrimPrefix(element, pointerTypePrefix))
}

// ListsAsArrayText reports whether a query binds slices as text arrays.
//
// The driver and dialect must support array membership. The query must be plain, prepare
// nothing at call time, and have at least one slice parameter. Every slice parameter must
// pass sliceBindsAsArrayText.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns bool which is true when the query's slices are bound as arrays' text form.
func (e *Emission) ListsAsArrayText(query *sqlmodel.AnalysedQuery) bool {
	binder, binds := e.Driver.Binder.(ArrayListBinder)
	membership := e.Dialect().Lists.ArrayMembership
	if !binds || membership.Before == "" || query.IsDynamic || query.DynamicRuntime {
		return false
	}
	if !preparesNothing(query.Command) {
		return false
	}
	listed := false
	for index := range query.Parameters {
		parameter := &query.Parameters[index]
		if !parameter.IsSlice {
			continue
		}
		if !e.sliceBindsAsArrayText(binder, parameter) {
			return false
		}
		listed = true
	}
	return listed
}

// sliceBindsAsArrayText reports whether a slice parameter can be bound as an array's text
// form. It must have no override, the binder must bind its elements' SQL type so, and its
// elements must be of a string, integer or bool Go type, or pointers to one.
//
// Takes binder (ArrayListBinder) which binds the list.
// Takes parameter (*sqlmodel.QueryParameter) which is the slice parameter.
//
// Returns bool which is true when the parameter can be bound as an array's text form.
func (e *Emission) sliceBindsAsArrayText(binder ArrayListBinder, parameter *sqlmodel.QueryParameter) bool {
	if parameter.GoTypeOverride != nil || !binder.BindsListAsArrayText(parameter.SQLType) {
		return false
	}
	element := goast.ResolveGoType(parameter.SQLType, parameter.Nullable, e.Request.Types)
	if element.Composite != nil || element.Package != "" {
		return false
	}
	return slices.Contains(arrayLiteralElements[parameter.SQLType.Category], strings.TrimPrefix(element.Name, pointerTypePrefix))
}

// listMode returns how a query's slice lists are written. They are written as arrays'
// text form when ListsAsArrayText says so, and as the driver's list mode says otherwise.
//
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
// Takes form (dialect.PlaceholderSigil) which is the form the query executes in.
//
// Returns emitkit.ListMode which is the list mode.
func (e *Emission) listMode(query *sqlmodel.AnalysedQuery, form dialect.PlaceholderSigil) emitkit.ListMode {
	if e.Driver.Lists == emitkit.ListExpand && form.Kind != dialect.PlaceholderNamed && e.ListsAsArrayText(query) {
		return emitkit.ListArray
	}
	return listMode(e.Driver.Lists, e.Dialect(), form)
}

// decodesDocument reports whether the generated code reads a column the driver reads as
// JSON by decoding the document into its Go type: the default slice, map or struct, or,
// for an array, the slice an override chose.
//
// Takes column (*sqlmodel.OutputColumn) which is the output column.
//
// Returns bool which is true when the column's document is decoded.
func (e *Emission) decodesDocument(column *sqlmodel.OutputColumn) bool {
	if column.GoTypeOverride == nil {
		return usesDefaultComposite(e.Request, column)
	}
	name := strings.TrimPrefix(column.GoTypeOverride.Name, "*")
	return column.SQLType.Category == sqlmodel.TypeCategoryArray && strings.HasPrefix(name, "[]")
}

// converts reports whether a column may be read through the driver's converting scan
// adapter at all: the driver's reader implements ColumnConverter and the column has no Go
// type override, which scans itself.
//
// Takes column (*sqlmodel.OutputColumn) which is the output column.
//
// Returns bool which is true when the adapter may read the column.
func (e *Emission) converts(column *sqlmodel.OutputColumn) bool {
	if column.GoTypeOverride != nil {
		return false
	}
	_, converts := e.Driver.Reader.(ColumnConverter)
	return converts
}

// qualifiedSource writes the table-qualified column a result column reads, for a
// suggestion, or a stand-in when the column has no single source.
//
// Takes column (*sqlmodel.OutputColumn) which is the result column.
//
// Returns string which is the qualified reference.
func qualifiedSource(column *sqlmodel.OutputColumn) string {
	qualifier := column.SourceQualifier
	if qualifier == "" {
		qualifier = column.SourceTable
	}
	if qualifier == "" || column.SourceColumn == "" {
		return placeholderQualifier + "." + column.Name
	}
	return qualifier + "." + column.SourceColumn
}

// renderRuntimeForms renders the remaining forms of a runtime builder query.
//
// These include count and, when conditions filter result rows, filtered and filtered
// count forms. A count wraps no column.
//
// Takes query (*Query) which is the query, its Rendered set, and receives the forms.
//
// Returns error when a form does not render.
func renderRuntimeForms(query *Query) error {
	analysed := query.Analysed
	if !analysed.DynamicRuntime {
		return nil
	}
	counting := *query.Renderer
	counting.WrapColumn = nil
	var err error
	if query.Count, err = renderOptional(analysed.CountTemplate, &counting); err != nil {
		return fmt.Errorf("query %q, count: %w", analysed.Name, err)
	}
	if query.Filtered, err = renderOptional(analysed.FilteredTemplate, query.Renderer); err != nil {
		return fmt.Errorf("query %q, filtered: %w", analysed.Name, err)
	}
	if query.Count == nil || query.Filtered == nil {
		return nil
	}
	if query.FilteredCount, err = renderOptional(analysed.FilteredCountTemplate, &counting); err != nil {
		return fmt.Errorf("query %q, filtered count: %w", analysed.Name, err)
	}
	return nil
}

// renderOptional renders a template a query may leave out.
//
// Takes template (*sqlmodel.SQLTemplate) which is the template, or nil.
// Takes renderer (*emitkit.Renderer) which writes it.
//
// Returns *emitkit.Rendered which is the rendering, or nil for no template.
// Returns error when the template does not render.
func renderOptional(template *sqlmodel.SQLTemplate, renderer *emitkit.Renderer) (*emitkit.Rendered, error) {
	if template == nil {
		return nil, nil
	}
	rendered, err := emitkit.Render(*template, renderer)
	if err != nil {
		return nil, err
	}
	return &rendered, nil
}

// SlicesExpand reports whether a query binds slice elements separately.
//
// Driver checks use individual elements when expansion is enabled and the whole slice
// otherwise.
//
// Takes lists (emitkit.ListMode) which is the driver's list mode, as Driver.Lists holds
// it.
// Takes info (*dialect.Facts) which describes the dialect. SlicesExpand does not change
// it.
// Takes query (*sqlmodel.AnalysedQuery) which is the query.
//
// Returns bool which is true when the query's slices are expanded.
func SlicesExpand(lists emitkit.ListMode, info *dialect.Facts, query *sqlmodel.AnalysedQuery) bool {
	return listMode(lists, info, emitkit.ExecutionForm(info, query)) == emitkit.ListExpand
}

// listMode returns how a query's slice lists are written in a placeholder form.
//
// Takes lists (emitkit.ListMode) which is the driver's list mode.
// Takes info (*dialect.Facts) which describes the dialect.
// Takes form (dialect.PlaceholderSigil) which is the form the query executes in.
//
// Returns emitkit.ListMode which is the list mode.
func listMode(lists emitkit.ListMode, info *dialect.Facts, form dialect.PlaceholderSigil) emitkit.ListMode {
	membership := info.Lists.ArrayMembership
	switch {
	case lists == emitkit.ListArray && membership.Before == "" && membership.After == "":
		return emitkit.ListExpand
	case lists == emitkit.ListExpand && form.Kind == dialect.PlaceholderNamed:
		return emitkit.ListAsWritten
	default:
		return lists
	}
}

// usesDefaultComposite reports whether a column uses a generated composite Go type that
// needs decoding or scan conversion.
//
// Takes request (*emit.Request) which holds the Go types and the naming plan.
// Takes column (*sqlmodel.OutputColumn) which is the output column.
//
// Returns bool which is true for a generated composite Go type.
func usesDefaultComposite(request *emit.Request, column *sqlmodel.OutputColumn) bool {
	resolved := goast.ResolveGoType(column.SQLType, column.Nullable, request.Types)
	if resolved.Composite != nil || strings.HasPrefix(resolved.Name, "[]") {
		return true
	}
	if resolved.Package != "" {
		return false
	}
	name := strings.TrimPrefix(resolved.Name, "*")
	for index := range request.Naming.Types {
		declared := &request.Naming.Types[index]
		if declared.Name == name && declared.SQLType.Category == sqlmodel.TypeCategoryStruct {
			return true
		}
	}
	return false
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

// preparesNothing reports whether a query command runs as a plain query, statement or
// stream that prepares nothing at call time.
//
// Takes command (sqlmodel.QueryCommand) which is the query's command.
//
// Returns bool which is true for such a command.
func preparesNothing(command sqlmodel.QueryCommand) bool {
	switch command {
	case sqlmodel.QueryCommandOne, sqlmodel.QueryCommandMany, sqlmodel.QueryCommandExec, sqlmodel.QueryCommandExecResult,
		sqlmodel.QueryCommandExecRows, sqlmodel.QueryCommandStream:
		return true
	default:
		return false
	}
}

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
)

var (
	// errUnnamedQuery reports a query the package's naming plan holds no names for.
	errUnnamedQuery = errors.New("the naming plan holds no names for the query")

	// errNoTemplate reports a query without an SQL template to generate from.
	errNoTemplate = errors.New("the query has no SQL template")
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

	// sql retains the rendered executable query form.
	sql emitkit.Rendered
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
// text, unless the column has a Go type of its own. A column of an embed is wrapped like
// any other. A star is expanded into its columns unless the request's style keeps stars.
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
		ListMode:     listMode(e.Driver.Lists, info, form),
		Prefix:       "",
		KeepStars:    e.Request.Style.KeepStars,
	}
	if query.IsDynamic && sortableParameter(query) != nil {
		renderer.Omit = emitkit.NewSegmentSet(sqlmodel.SegmentOrderBy)
	}
	if e.Driver.Reader != nil {
		renderer.WrapColumn = func(column int) (emitkit.ColumnWrap, bool) {
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
		return &Query{Analysed: analysed, Naming: naming, Types: e.Request.Types, Imports: imports, Renderer: renderer, Rendered: cached.sql, Count: cached.count}, nil
	}
	rendered, err := emitkit.Render(analysed.Template, renderer)
	if err != nil {
		return nil, fmt.Errorf("query %q: %w", analysed.Name, err)
	}
	query := &Query{
		Analysed: analysed,
		Naming:   naming,
		Types:    e.Request.Types,
		Imports:  imports,
		Renderer: renderer,
		Rendered: rendered,
		Count:    nil,
	}
	if analysed.DynamicRuntime && analysed.CountTemplate != nil {
		counting := *renderer
		counting.WrapColumn = nil
		count, countErr := emitkit.Render(*analysed.CountTemplate, &counting)
		if countErr != nil {
			return nil, fmt.Errorf("query %q, count: %w", analysed.Name, countErr)
		}
		query.Count = &count
	}
	if e.rendered == nil {
		e.rendered = make(map[renderKey]renderData)
	}
	e.rendered[key] = renderData{sql: query.Rendered, count: query.Count}
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
		if info.ToJSON == "" || !e.decodesDocument(column) {
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

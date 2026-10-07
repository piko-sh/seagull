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
	"go/ast"
	"testing"

	"github.com/stretchr/testify/require"
	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/gotypes"
	"seagull.sh/seagull/internal/sqlmodel"
)

type jsonReader struct{}

func (jsonReader) ColumnReading(sqlType sqlmodel.SQLType) ColumnReading {
	if sqlType.Category == sqlmodel.TypeCategoryArray {
		return ColumnReadAsJSON
	}
	return ColumnReadAsIs
}

var dollarForm = dialect.PlaceholderSigil{Prefix: "$", Kind: dialect.PlaceholderNumbered}

var namedForm = dialect.PlaceholderSigil{Prefix: ":", Kind: dialect.PlaceholderNamed}

type namedStructResolver struct{}

func (namedStructResolver) Resolve(sqlType sqlmodel.SQLType, nullable bool) sqlmodel.GoType {
	switch {
	case sqlType.Category == sqlmodel.TypeCategoryStruct && nullable:
		return sqlmodel.GoType{Name: "*SensorsLocation"}
	case sqlType.Category == sqlmodel.TypeCategoryStruct:
		return sqlmodel.GoType{Name: "SensorsLocation"}
	default:
		return sqlmodel.GoType{Name: "string"}
	}
}

type structJSONReader struct{}

func (structJSONReader) ColumnReading(sqlType sqlmodel.SQLType) ColumnReading {
	if sqlType.Category == sqlmodel.TypeCategoryStruct {
		return ColumnReadAsJSON
	}
	return ColumnReadAsIs
}

type convertingReader struct{}

func (convertingReader) ColumnReading(sqlType sqlmodel.SQLType) ColumnReading {
	if sqlType.Category == sqlmodel.TypeCategoryArray {
		return ColumnReadConverted
	}
	return ColumnReadAsIs
}

func (convertingReader) ConvertingScan(destination ast.Expr) ast.Expr {
	return goastutil.CallExpr(goastutil.CachedIdent("sglScanDriverComposite"), destination)
}

type convertingTextReader struct {
	convertingReader
}

func (convertingTextReader) ColumnReading(sqlType sqlmodel.SQLType) ColumnReading {
	if sqlType.Category == sqlmodel.TypeCategoryDecimal {
		return ColumnReadAsText
	}
	return ColumnReadAsIs
}

type adaptingReader struct {
	convertingReader
}

func (adaptingReader) ColumnReading(sqlType sqlmodel.SQLType) ColumnReading {
	if sqlType.Category == sqlmodel.TypeCategoryJSON {
		return ColumnReadAdapted
	}
	return ColumnReadAsIs
}

type rawReader struct {
	fixedReader
}

func (rawReader) RawType(sqlmodel.SQLType) sqlmodel.GoType {
	return sqlmodel.NewGoMap(sqlmodel.GoType{Name: "string"}, sqlmodel.GoType{Name: "any"})
}

func (rawReader) ConvertRaw(destination ast.Expr, raw ast.Expr) ast.Expr {
	return goastutil.CallExpr(goastutil.CachedIdent("sglReadRaw"), destination, raw)
}

func (rawReader) ConvertingScan(destination ast.Expr) ast.Expr {
	return goastutil.CallExpr(goastutil.CachedIdent("sglScanConverted"), destination)
}

func textSegment(text string) sqlmodel.SQLSegment {
	return sqlmodel.SQLSegment{Kind: sqlmodel.SegmentText, Text: text}
}

func parameterSegment(parameter int) sqlmodel.SQLSegment {
	return sqlmodel.SQLSegment{Kind: sqlmodel.SegmentParameter, Parameter: parameter}
}

func listSegment(parameter int) sqlmodel.SQLSegment {
	return sqlmodel.SQLSegment{Kind: sqlmodel.SegmentList, Parameter: parameter, Text: "IN "}
}

func renderingInfo(execution, positional dialect.PlaceholderSigil) dialect.Facts {
	info := dialect.Facts{Name: "test", Family: "test"}
	info.Placeholders = dialect.Placeholders{
		Sigils:     []dialect.PlaceholderSigil{execution},
		Execution:  execution,
		Positional: positional,
		NamePrefix: "p_",
	}
	info.IdentifierQuote = dialect.IdentifierQuote{Open: `"`, Close: `"`}
	info.ToJSON = "to_json"
	return info
}

func renderingEmission(info dialect.Facts, lists emitkit.ListMode, reader ColumnReader, queries ...*sqlmodel.AnalysedQuery) *Emission {
	request := testRequest()
	request.Dialect = info
	request.Queries = queries
	request.Types = &gotypes.MappingTable{Mappings: []gotypes.Mapping{{
		SQLCategory: sqlmodel.TypeCategoryText,
		NotNull:     sqlmodel.GoType{Name: "string"},
		Nullable:    sqlmodel.GoType{Name: "*string"},
	}}}
	return NewEmission(request, &Driver{Reader: reader, Lists: lists})
}

func lookupQuery() *sqlmodel.AnalysedQuery {
	return &sqlmodel.AnalysedQuery{
		Name:     "FindUsers",
		Filename: "users.sql",
		Command:  sqlmodel.QueryCommandMany,
		Parameters: []sqlmodel.QueryParameter{
			{Name: "ids", Number: 1, IsSlice: true},
			{Name: "name", Number: 2},
		},
		OutputColumns: []sqlmodel.OutputColumn{{Name: "id"}},
		Template: sqlmodel.SQLTemplate{Segments: []sqlmodel.SQLSegment{
			textSegment("SELECT id FROM users WHERE id "),
			listSegment(0),
			textSegment(" AND name = "),
			parameterSegment(1),
		}},
	}
}

func TestQueryRendersTheTemplateWithTheQuerysNames(t *testing.T) {
	t.Parallel()

	query := lookupQuery()
	emission := renderingEmission(renderingInfo(dollarForm, dollarForm), emitkit.ListExpand, nil, query)

	generated, err := emission.Query(query, goast.NewImportTracker())

	require.NoError(t, err)
	require.Equal(t, "SELECT id FROM users WHERE id IN ($2) AND name = $1", generated.Rendered.SQL)
	require.Equal(t, []emitkit.Argument{{Parameter: 1}, {Parameter: 0, Expand: true}}, generated.Rendered.Arguments)
	require.Len(t, generated.Rendered.Marks.Lists, 1)
	require.Equal(t, 2, generated.Rendered.Marks.Lists[0].First)
	require.Equal(t, "FindUsers", generated.Naming.Method)
	require.Equal(t, "findUsersSQL", generated.Naming.SQLConstant)
	require.Same(t, query, generated.Renderer.Query)
	require.Nil(t, generated.Count)
}

func TestRendererChoosesTheListModeFromTheDriverAndTheDialect(t *testing.T) {
	t.Parallel()

	arrays := renderingInfo(dollarForm, dollarForm)
	arrays.Lists = dialect.ListSyntax{
		ArrayMembership: dialect.Affix{Before: "= ANY(", After: ")"},
		ArrayExclusion:  dialect.Affix{Before: "<> ALL(", After: ")"},
	}
	testCases := []struct {
		name     string
		info     dialect.Facts
		driver   emitkit.ListMode
		expected emitkit.ListMode
		sql      string
	}{
		{
			name:     "a driver expanding lists expands them",
			info:     renderingInfo(dollarForm, dollarForm),
			driver:   emitkit.ListExpand,
			expected: emitkit.ListExpand,
			sql:      "SELECT id FROM users WHERE id IN ($2) AND name = $1",
		},
		{
			name:     "a driver binding arrays uses the dialect's array form",
			info:     arrays,
			driver:   emitkit.ListArray,
			expected: emitkit.ListArray,
			sql:      "SELECT id FROM users WHERE id = ANY($2) AND name = $1",
		},
		{
			name:     "a driver binding arrays expands lists of a dialect without an array form",
			info:     renderingInfo(dollarForm, dollarForm),
			driver:   emitkit.ListArray,
			expected: emitkit.ListExpand,
			sql:      "SELECT id FROM users WHERE id IN ($2) AND name = $1",
		},
		{
			name:     "a list in a form binding by name is bound as written",
			info:     renderingInfo(namedForm, dollarForm),
			driver:   emitkit.ListExpand,
			expected: emitkit.ListAsWritten,
			sql:      "SELECT id FROM users WHERE id IN (:p_ids) AND name = :p_name",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			query := lookupQuery()
			emission := renderingEmission(testCase.info, testCase.driver, nil, query)

			generated, err := emission.Query(query, goast.NewImportTracker())

			require.NoError(t, err)
			require.Equal(t, testCase.expected, generated.Renderer.ListMode)
			require.Equal(t, testCase.sql, generated.Rendered.SQL)
			require.Equal(t, testCase.expected == emitkit.ListExpand, SlicesExpand(testCase.driver, &testCase.info, query))
		})
	}
}

func TestRendererLeavesOutTheOrderByASortableQueryReplaces(t *testing.T) {
	t.Parallel()

	query := &sqlmodel.AnalysedQuery{
		Name:      "ListUsers",
		Filename:  "users.sql",
		Command:   sqlmodel.QueryCommandMany,
		IsDynamic: true,
		Parameters: []sqlmodel.QueryParameter{
			{Name: "order_by", Kind: sqlmodel.ParameterDirectiveSortable, SortableColumns: []string{"name"}},
			{Name: "page_size", Number: 1},
		},
		OutputColumns: []sqlmodel.OutputColumn{{Name: "id"}},
		Template: sqlmodel.SQLTemplate{Segments: []sqlmodel.SQLSegment{
			textSegment("SELECT id FROM users "),
			{Kind: sqlmodel.SegmentOrderBy, Present: true, Children: []sqlmodel.SQLSegment{textSegment("ORDER BY id")}},
			textSegment(" "),
			{Kind: sqlmodel.SegmentPagination, Present: true, Children: []sqlmodel.SQLSegment{textSegment("LIMIT "), parameterSegment(1)}},
		}},
	}
	emission := renderingEmission(renderingInfo(dollarForm, dollarForm), emitkit.ListExpand, nil, query)

	generated, err := emission.Query(query, goast.NewImportTracker())

	require.NoError(t, err)
	require.Equal(t, "SELECT id FROM users  LIMIT $1", generated.Rendered.SQL)
	require.Equal(t, &emitkit.Range{Start: 21, End: 21}, generated.Rendered.Marks.OrderBy)
}

func TestQueryRendersTheCountOfARuntimeBuilderWithItsOwnNumbering(t *testing.T) {
	t.Parallel()

	query := &sqlmodel.AnalysedQuery{
		Name:           "SearchPosts",
		Filename:       "posts.sql",
		Command:        sqlmodel.QueryCommandMany,
		DynamicRuntime: true,
		Parameters: []sqlmodel.QueryParameter{
			{Name: "page_size", Number: 1},
			{Name: "author", Number: 2},
		},
		OutputColumns: []sqlmodel.OutputColumn{{Name: "id"}},
		Template: sqlmodel.SQLTemplate{Segments: []sqlmodel.SQLSegment{
			textSegment("SELECT id FROM posts WHERE "),
			{Kind: sqlmodel.SegmentFilter, Present: true, Children: []sqlmodel.SQLSegment{textSegment("author = "), parameterSegment(1)}},
			textSegment(" LIMIT "),
			parameterSegment(0),
		}},
		CountTemplate: &sqlmodel.SQLTemplate{Segments: []sqlmodel.SQLSegment{
			textSegment("SELECT COUNT(*) FROM posts WHERE "),
			{Kind: sqlmodel.SegmentFilter, Present: true, Children: []sqlmodel.SQLSegment{textSegment("author = "), parameterSegment(1)}},
		}},
	}
	emission := renderingEmission(renderingInfo(namedForm, dollarForm), emitkit.ListExpand, nil, query)

	generated, err := emission.Query(query, goast.NewImportTracker())

	require.NoError(t, err)
	require.Equal(t, "SELECT id FROM posts WHERE author = $2 LIMIT $1", generated.Rendered.SQL)
	require.NotNil(t, generated.Count)
	require.Equal(t, "SELECT COUNT(*) FROM posts WHERE author = $1", generated.Count.SQL)
	require.Equal(t, []emitkit.Argument{{Parameter: 1}}, generated.Count.Arguments)
	require.Equal(t, "searchPostsCountSQL", generated.Naming.CountConstant)
}

func TestQueryTellsWhetherItIsRenderedInTheExecutionForm(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		execution dialect.PlaceholderSigil
		runtime   bool
		expected  bool
	}{
		{name: "a static query of a dialect executing by name", execution: namedForm, runtime: false, expected: true},
		{name: "a runtime builder of a dialect executing by name", execution: namedForm, runtime: true, expected: false},
		{name: "a runtime builder of a dialect executing by number", execution: dollarForm, runtime: true, expected: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			query := lookupQuery()
			query.DynamicRuntime = testCase.runtime
			emission := renderingEmission(renderingInfo(testCase.execution, dollarForm), emitkit.ListAsWritten, nil, query)

			generated, err := emission.Query(query, goast.NewImportTracker())

			require.NoError(t, err)
			require.Equal(t, testCase.expected, emission.InExecutionForm(generated))
		})
	}
}

func TestRendererWrapsTheColumnsTheDriverReadsAsJSON(t *testing.T) {
	t.Parallel()

	query := &sqlmodel.AnalysedQuery{
		Name:     "ListTags",
		Filename: "tags.sql",
		Command:  sqlmodel.QueryCommandMany,
		OutputColumns: []sqlmodel.OutputColumn{
			{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
			{Name: "tags", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, ElementType: &sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}}},
		},
		Template: sqlmodel.SQLTemplate{Segments: []sqlmodel.SQLSegment{
			textSegment("SELECT "),
			{Kind: sqlmodel.SegmentColumn, Column: 0, Children: []sqlmodel.SQLSegment{textSegment("id")}},
			textSegment(", "),
			{Kind: sqlmodel.SegmentColumn, Column: 1, Children: []sqlmodel.SQLSegment{textSegment("tags")}},
			textSegment(" FROM posts"),
		}},
	}
	emission := renderingEmission(renderingInfo(dollarForm, dollarForm), emitkit.ListExpand, jsonReader{}, query)

	generated, err := emission.Query(query, goast.NewImportTracker())

	require.NoError(t, err)
	require.Equal(t, `SELECT id, to_json(tags) AS "tags" FROM posts`, generated.Rendered.SQL)
	require.Equal(t, map[int]emitkit.ColumnWrapKind{1: emitkit.ColumnWrapJSON}, generated.Rendered.Marks.Wrapped)
	require.True(t, emission.NeedsJSONWrap(&query.OutputColumns[1]))
	require.False(t, emission.NeedsJSONWrap(&query.OutputColumns[0]))
}

func TestQueryRefusesAQueryItCannotGenerateFrom(t *testing.T) {
	t.Parallel()

	withoutTemplate := lookupQuery()
	withoutTemplate.Template = sqlmodel.SQLTemplate{}
	stranger := lookupQuery()
	emission := renderingEmission(renderingInfo(dollarForm, dollarForm), emitkit.ListExpand, nil, withoutTemplate)

	_, err := emission.Query(withoutTemplate, goast.NewImportTracker())
	require.ErrorIs(t, err, errNoTemplate)

	_, err = emission.Query(stranger, goast.NewImportTracker())
	require.ErrorIs(t, err, errUnnamedQuery)
}

func TestRendererWrapsTheColumnsOfAStructTypeThePackageDeclares(t *testing.T) {
	t.Parallel()

	location := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryStruct, StructFields: []sqlmodel.StructField{{Name: "label"}}}
	query := &sqlmodel.AnalysedQuery{
		Name:          "ListSensors",
		Filename:      "sensors.sql",
		Command:       sqlmodel.QueryCommandMany,
		OutputColumns: []sqlmodel.OutputColumn{{Name: "location", SQLType: location, Nullable: true}},
		Template: sqlmodel.SQLTemplate{Segments: []sqlmodel.SQLSegment{
			textSegment("SELECT "),
			{Kind: sqlmodel.SegmentColumn, Column: 0, Children: []sqlmodel.SQLSegment{textSegment("location")}},
			textSegment(" FROM sensors"),
		}},
	}
	structReader := structJSONReader{}
	declared := renderingEmission(renderingInfo(dollarForm, dollarForm), emitkit.ListExpand, structReader, query)
	declared.Request.Types = namedStructResolver{}
	declared.Request.Naming.Types = []emit.NamedType{{Name: "SensorsLocation", SQLType: location}}
	undeclared := renderingEmission(renderingInfo(dollarForm, dollarForm), emitkit.ListExpand, structReader, query)
	undeclared.Request.Types = namedStructResolver{}

	require.True(t, declared.NeedsJSONWrap(&query.OutputColumns[0]))
	require.False(t, undeclared.NeedsJSONWrap(&query.OutputColumns[0]))
}

func TestRendererLeavesTheColumnsTheDriverReadsConvertedUnwrapped(t *testing.T) {
	t.Parallel()

	tags := sqlmodel.OutputColumn{Name: "tags", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, ElementType: &sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}}}
	overridden := tags
	overridden.Name = "labels"
	overridden.GoTypeOverride = &sqlmodel.GoType{Package: "example.com/labels", Name: "Labels"}
	query := &sqlmodel.AnalysedQuery{
		Name:     "ListTags",
		Filename: "tags.sql",
		Command:  sqlmodel.QueryCommandMany,
		OutputColumns: []sqlmodel.OutputColumn{
			{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
			tags,
			overridden,
		},
		Template: sqlmodel.SQLTemplate{Segments: []sqlmodel.SQLSegment{
			textSegment("SELECT "),
			{Kind: sqlmodel.SegmentColumn, Column: 0, Children: []sqlmodel.SQLSegment{textSegment("id")}},
			textSegment(", "),
			{Kind: sqlmodel.SegmentColumn, Column: 1, Children: []sqlmodel.SQLSegment{textSegment("tags")}},
			textSegment(", "),
			{Kind: sqlmodel.SegmentColumn, Column: 2, Children: []sqlmodel.SQLSegment{textSegment("labels")}},
			textSegment(" FROM posts"),
		}},
	}
	emission := renderingEmission(renderingInfo(dollarForm, dollarForm), emitkit.ListExpand, convertingReader{}, query)

	generated, err := emission.Query(query, goast.NewImportTracker())

	require.NoError(t, err)
	require.Equal(t, "SELECT id, tags, labels FROM posts", generated.Rendered.SQL)
	require.Empty(t, generated.Rendered.Marks.Wrapped)
	require.Equal(t, []bool{false, true, false}, []bool{
		emission.ReadsConverted(&query.OutputColumns[0]),
		emission.ReadsConverted(&query.OutputColumns[1]),
		emission.ReadsConverted(&query.OutputColumns[2]),
	})
	require.False(t, emission.NeedsJSONWrap(&query.OutputColumns[1]))
}

func TestConvertsColumnReadsATextColumnTheQueryCannotCastThroughTheAdapter(t *testing.T) {
	t.Parallel()

	amount := sqlmodel.OutputColumn{Name: "amount", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryDecimal}}
	overridden := amount
	overridden.GoTypeOverride = &sqlmodel.GoType{Package: "example.com/money", Name: "Amount"}
	converting := renderingEmission(renderingInfo(dollarForm, dollarForm), emitkit.ListExpand, convertingTextReader{})
	plain := renderingEmission(renderingInfo(dollarForm, dollarForm), emitkit.ListExpand, jsonReader{})

	require.True(t, converting.ConvertsColumn(&amount, false), "a column the query cannot cast is converted from the driver's value")
	require.False(t, converting.ConvertsColumn(&amount, true), "a cast column is scanned as text")
	require.False(t, converting.ConvertsColumn(&overridden, false), "an override scans itself")
	require.False(t, plain.ConvertsColumn(&amount, false), "a reader without an adapter reads it as it is")
}

func TestConvertsColumnReadsAColumnTheDriverReadsAdaptedWhateverItsGoType(t *testing.T) {
	t.Parallel()

	document := sqlmodel.OutputColumn{Name: "document", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryJSON}}
	overridden := document
	overridden.GoTypeOverride = &sqlmodel.GoType{Package: "encoding/json", Name: "RawMessage"}
	testCases := []struct {
		name     string
		slot     sqlmodel.GoType
		column   sqlmodel.OutputColumn
		reader   ColumnReader
		converts bool
	}{
		{name: "the generated JSON type", slot: sqlmodel.GoType{Name: "JSON"}, column: document, reader: adaptingReader{}, converts: true},
		{name: "a type slot's type", slot: sqlmodel.GoType{Package: "example.com/documents", Name: "Document"}, column: document, reader: adaptingReader{}, converts: true},
		{name: "a column override, read through the override reader", slot: sqlmodel.GoType{Name: "JSON"}, column: overridden, reader: adaptingReader{}, converts: false},
		{name: "a driver reading the column as it is", slot: sqlmodel.GoType{Name: "JSON"}, column: document, reader: convertingReader{}, converts: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			emission := renderingEmission(renderingInfo(dollarForm, dollarForm), emitkit.ListExpand, testCase.reader)
			emission.Request.Types = &gotypes.MappingTable{Mappings: []gotypes.Mapping{{
				SQLCategory: sqlmodel.TypeCategoryJSON, NotNull: testCase.slot, Nullable: testCase.slot,
			}}}

			require.Equal(t, testCase.converts, emission.ConvertsColumn(&testCase.column, false))
			require.Equal(t, testCase.converts, emission.ReadsConverted(&testCase.column))
		})
	}
}

func TestConvertsAnyColumnFindsAColumnReadThroughTheAdapter(t *testing.T) {
	t.Parallel()

	tags := sqlmodel.OutputColumn{Name: "tags", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, ElementType: &sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}}}
	plain := &sqlmodel.AnalysedQuery{
		Name: "ListIDs", Filename: "ids.sql", Command: sqlmodel.QueryCommandMany,
		OutputColumns: []sqlmodel.OutputColumn{{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}}},
		Template:      sqlmodel.SQLTemplate{Segments: []sqlmodel.SQLSegment{textSegment("SELECT id FROM posts")}},
	}
	withTags := &sqlmodel.AnalysedQuery{
		Name: "ListTags", Filename: "tags.sql", Command: sqlmodel.QueryCommandMany,
		OutputColumns: []sqlmodel.OutputColumn{tags},
		Template:      sqlmodel.SQLTemplate{Segments: []sqlmodel.SQLSegment{textSegment("SELECT tags FROM posts")}},
	}
	testCases := []struct {
		name     string
		reader   ColumnReader
		queries  []*sqlmodel.AnalysedQuery
		expected bool
	}{
		{name: "a converted column", reader: convertingReader{}, queries: []*sqlmodel.AnalysedQuery{plain, withTags}, expected: true},
		{name: "no converted column", reader: convertingReader{}, queries: []*sqlmodel.AnalysedQuery{plain}, expected: false},
		{name: "a reader reading as JSON", reader: jsonReader{}, queries: []*sqlmodel.AnalysedQuery{withTags}, expected: false},
		{name: "no reader", reader: nil, queries: []*sqlmodel.AnalysedQuery{withTags}, expected: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			emission := renderingEmission(renderingInfo(dollarForm, dollarForm), emitkit.ListExpand, testCase.reader, testCase.queries...)

			converts, err := emission.ConvertsAnyColumn()

			require.NoError(t, err)
			require.Equal(t, testCase.expected, converts)
		})
	}
}

func TestRenderingCacheKeepsFileStateIndependent(t *testing.T) {
	query := lookupQuery()
	emission := renderingEmission(renderingInfo(dollarForm, dollarForm), emitkit.ListExpand, nil, query)
	firstImports, secondImports := goast.NewImportTracker(), goast.NewImportTracker()
	first, err := emission.Query(query, firstImports)
	require.NoError(t, err)
	second, err := emission.Query(query, secondImports)
	require.NoError(t, err)
	require.Len(t, emission.rendered, 1)
	require.Equal(t, first.Rendered, second.Rendered)
	require.NotSame(t, first.Renderer, second.Renderer)
	require.Same(t, firstImports, first.Imports)
	require.Same(t, secondImports, second.Imports)
	other := renderingEmission(renderingInfo(namedForm, namedForm), emitkit.ListExpand, nil, query)
	rendered, err := other.Query(query, goast.NewImportTracker())
	require.NoError(t, err)
	require.NotEqual(t, first.Rendered.SQL, rendered.Rendered.SQL)
}

func TestEmissionReadsARawColumnThroughTheDriversConverter(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		reader     ColumnReader
		name       string
		rawType    sqlmodel.GoType
		converted  string
		scanned    string
		convertsOK bool
	}{
		{name: "a driver converting raw columns", reader: rawReader{fixedReader: fixedReader{reading: ColumnReadRaw}},
			rawType:   sqlmodel.NewGoMap(sqlmodel.GoType{Name: "string"}, sqlmodel.GoType{Name: "any"}),
			converted: "sglReadRaw(&row.Point, raw)", scanned: "sglScanConverted(&row.Point)", convertsOK: true},
		{name: "a driver with no converter", reader: fixedReader{reading: ColumnReadAsIs},
			rawType: sqlmodel.GoType{Name: "any"}, scanned: "&row.Point"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			emission := renderingEmission(renderingInfo(dollarForm, dollarForm), emitkit.ListExpand, testCase.reader)
			destination := goastutil.AddressExpr(goastutil.SelectorExpr("row", "Point"))
			pointType := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryStruct, EngineName: "point"}

			require.Equal(t, testCase.rawType, emission.RawType(pointType))
			converted := emission.ConvertRaw(destination, goastutil.CachedIdent("raw"))
			if !testCase.convertsOK {
				require.Nil(t, converted)
			} else {
				require.Equal(t, testCase.converted, renderExpression(t, converted))
			}
			require.Equal(t, testCase.scanned, renderExpression(t, emission.ConvertingScan(destination)))
		})
	}
}

func TestBindFormWritesRunTimePlaceholdersInAFormThatBindsByPosition(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		execution dialect.PlaceholderSigil
		want      dialect.PlaceholderSigil
	}{
		{name: "a dialect binding by number", execution: dollarForm, want: dollarForm},
		{name: "a dialect binding by name", execution: namedForm, want: dollarForm},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			emission := renderingEmission(renderingInfo(testCase.execution, dollarForm), emitkit.ListExpand, nil)

			require.Equal(t, testCase.want, emission.BindForm())
		})
	}
}

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

package chvalues_test

import (
	"bytes"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/clickhouse"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/emit/internal/chvalues"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/sqlmodel"
)

func acmeVocabulary() gogen.Vocabulary {
	request := &emit.Request{}
	request.Style.HelperPrefix = "acme"
	request.Style.ErrorPrefix = "acme"
	return gogen.NewVocabulary(request)
}

func typeOf(category sqlmodel.SQLTypeCategory) sqlmodel.SQLType {
	return sqlmodel.SQLType{Category: category}
}

func temporalOf(kind sqlmodel.TemporalKind, precision *int) sqlmodel.SQLType {
	return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryTemporal, Temporal: kind, Precision: precision}
}

func addressOf(width int) sqlmodel.SQLType {
	address := sqlmodel.NewNetworkType("", sqlmodel.NetworkKindAddress)
	address.Width = width
	return address
}

func arrayOf(element sqlmodel.SQLType) sqlmodel.SQLType {
	return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, ElementType: &element}
}

func TestShapeDescribesHowClickHouseReadsEachLevelOfAType(t *testing.T) {
	t.Parallel()

	key := typeOf(sqlmodel.TypeCategoryText)
	value := arrayOf(typeOf(sqlmodel.TypeCategoryInteger))

	testCases := []struct {
		name    string
		want    string
		sqlType sqlmodel.SQLType
	}{
		{name: "an integer is bare", sqlType: typeOf(sqlmodel.TypeCategoryInteger), want: "n"},
		{name: "a float is bare", sqlType: typeOf(sqlmodel.TypeCategoryFloat), want: "n"},
		{name: "a decimal is bare", sqlType: typeOf(sqlmodel.TypeCategoryDecimal), want: "n"},
		{name: "a boolean is bare", sqlType: typeOf(sqlmodel.TypeCategoryBoolean), want: "n"},
		{name: "text is quoted", sqlType: typeOf(sqlmodel.TypeCategoryText), want: "q"},
		{name: "bytes are quoted", sqlType: typeOf(sqlmodel.TypeCategoryBinary), want: "q"},
		{name: "a UUID is quoted", sqlType: typeOf(sqlmodel.TypeCategoryUUID), want: "q"},
		{name: "a network value other than an address is text", sqlType: typeOf(sqlmodel.TypeCategoryNetwork), want: "q"},
		{name: "an address of a type holding only IPv4 ones", sqlType: addressOf(32), want: "4"},
		{name: "an address of a type holding IPv6 ones", sqlType: addressOf(128), want: "6"},
		{name: "an address of a type that does not say", sqlType: addressOf(0), want: "6"},
		{name: "a map of IPv4 addresses", sqlType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryMap, KeyType: &key, ElementType: new(addressOf(32))},
			want: "{q:4}"},
		{name: "an enum is quoted", sqlType: typeOf(sqlmodel.TypeCategoryEnum), want: "q"},
		{name: "a date", sqlType: temporalOf(sqlmodel.TemporalKindDate, nil), want: "d"},
		{name: "a date and time", sqlType: temporalOf(sqlmodel.TemporalKindTimestamp, nil), want: "t"},
		{name: "a date and time with a precision", sqlType: temporalOf(sqlmodel.TemporalKindTimestamp, new(3)), want: "t3"},
		{name: "a precision beyond nanoseconds is nanoseconds", sqlType: temporalOf(sqlmodel.TemporalKindTimestamp, new(12)), want: "t9"},
		{name: "a time of day is text", sqlType: temporalOf(sqlmodel.TemporalKindTime, nil), want: "q"},
		{name: "an array", sqlType: arrayOf(typeOf(sqlmodel.TypeCategoryUUID)), want: "[q]"},
		{name: "an array of arrays", sqlType: arrayOf(arrayOf(typeOf(sqlmodel.TypeCategoryInteger))), want: "[[n]]"},
		{name: "an array of an unknown type", sqlType: typeOf(sqlmodel.TypeCategoryArray), want: "[a]"},
		{name: "a map", sqlType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryMap, KeyType: &key, ElementType: &value}, want: "{q:[n]}"},
		{name: "a named tuple", sqlType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryStruct, StructFields: []sqlmodel.StructField{
			{Name: "id", SQLType: typeOf(sqlmodel.TypeCategoryInteger)},
			{Name: "it's", SQLType: temporalOf(sqlmodel.TemporalKindDate, nil)},
		}}, want: "('id'=n,'it''s'=d)"},
		{name: "an unnamed tuple", sqlType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryStruct, StructFields: []sqlmodel.StructField{
			{SQLType: typeOf(sqlmodel.TypeCategoryInteger)},
			{SQLType: typeOf(sqlmodel.TypeCategoryText)},
		}}, want: "(n,q)"},
		{name: "a tuple without fields is unknown", sqlType: typeOf(sqlmodel.TypeCategoryStruct), want: "a"},
		{name: "an unknown type", sqlType: typeOf(sqlmodel.TypeCategoryUnknown), want: "a"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, chvalues.Shape(&testCase.sqlType))
		})
	}
}

func TestBindPassesTheParametersShape(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		want      string
		parameter sqlmodel.QueryParameter
	}{
		{name: "a scalar", parameter: sqlmodel.QueryParameter{Name: "id", SQLType: typeOf(sqlmodel.TypeCategoryInteger)}, want: `acmeClickHouseValue(params.ID, "n")`},
		{name: "a slice is bound whole as an array", parameter: sqlmodel.QueryParameter{Name: "ids", IsSlice: true, SQLType: typeOf(sqlmodel.TypeCategoryUUID)}, want: `acmeClickHouseValue(params.ID, "[q]")`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			call := chvalues.Bind(acmeVocabulary(), &ast.SelectorExpr{X: ast.NewIdent("params"), Sel: ast.NewIdent("ID")}, &testCase.parameter)
			var printed bytes.Buffer
			require.NoError(t, printer.Fprint(&printed, token.NewFileSet(), call))
			require.Equal(t, testCase.want, printed.String())
		})
	}
}

func TestValuesRendersTheHelperForThePackage(t *testing.T) {
	t.Parallel()

	file, err := chvalues.Values(acmeVocabulary(), "store", chvalues.ValueParts{Documents: false, Nullables: false})
	require.NoError(t, err)
	require.Equal(t, chvalues.File, file.Name)
	content := string(file.Content)
	require.Contains(t, content, "package store")
	require.Contains(t, content, "func acmeClickHouseValue(value any, shape string) string {")
	require.Contains(t, content, `errors.New("acme: cannot write the value of a ClickHouse parameter")`)
	require.NotContains(t, content, "sgl")

	fileSet := token.NewFileSet()
	syntax, err := parser.ParseFile(fileSet, file.Name, file.Content, parser.AllErrors)
	require.NoError(t, err)
	config := types.Config{Importer: importer.Default()}
	checked, err := config.Check("store", fileSet, []*ast.File{syntax}, nil)
	require.NoError(t, err)

	listed := make([]string, 0, len(chvalues.Helpers)+len(chvalues.Sentinels))
	for _, suffix := range chvalues.Helpers {
		listed = append(listed, "acme"+suffix)
	}
	for _, suffix := range chvalues.Sentinels {
		listed = append(listed, "errAcme"+suffix)
	}
	slices.Sort(listed)
	require.Equal(t, checked.Scope().Names(), listed, "Helpers and Sentinels list every declaration of the helper file")
}

func TestValuesHoldsTheOptionalHelpersOnlyWhenAParameterNeedsThem(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		helpers []string
		parts   chvalues.ValueParts
	}{
		{name: "no parameter bound as its document", parts: chvalues.ValueParts{}, helpers: chvalues.Helpers},
		{
			name:    "a parameter bound as its document",
			parts:   chvalues.ValueParts{Documents: true, Nullables: false},
			helpers: slices.Concat(chvalues.Helpers, chvalues.EncodedHelpers),
		},
		{
			name:    "a runtime builder binding a nullable decimal",
			parts:   chvalues.ValueParts{Documents: false, Nullables: true},
			helpers: slices.Concat(chvalues.Helpers, chvalues.NullableHelpers),
		},
		{
			name:    "a parameter holding a time of day",
			parts:   chvalues.ValueParts{Durations: true},
			helpers: slices.Concat(chvalues.Helpers, chvalues.DurationHelpers),
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			file, err := chvalues.Values(acmeVocabulary(), "store", testCase.parts)
			require.NoError(t, err)
			fileSet := token.NewFileSet()
			syntax, err := parser.ParseFile(fileSet, file.Name, file.Content, parser.AllErrors)
			require.NoError(t, err)
			checked, err := (&types.Config{Importer: importer.Default()}).Check("store", fileSet, []*ast.File{syntax}, nil)
			require.NoError(t, err)

			listed := make([]string, 0, len(testCase.helpers)+len(chvalues.Sentinels))
			for _, suffix := range testCase.helpers {
				listed = append(listed, "acme"+suffix)
			}
			for _, suffix := range chvalues.Sentinels {
				listed = append(listed, "errAcme"+suffix)
			}
			slices.Sort(listed)
			require.Equal(t, listed, checked.Scope().Names())
		})
	}
}

func TestBindsDurationsFindsAParameterHoldingATimeOfDay(t *testing.T) {
	t.Parallel()

	timeOfDay := sqlmodel.NewTemporalType("Time64", sqlmodel.TemporalKindTime, false)
	timestamp := sqlmodel.NewTemporalType("DateTime", sqlmodel.TemporalKindTimestamp, true)
	listed := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, EngineName: "Array", ElementType: &timeOfDay}
	query := func(parameterTypes ...sqlmodel.SQLType) *sqlmodel.AnalysedQuery {
		parameters := make([]sqlmodel.QueryParameter, 0, len(parameterTypes))
		for index := range parameterTypes {
			parameters = append(parameters, sqlmodel.QueryParameter{Name: "p", Number: index + 1, SQLType: parameterTypes[index]})
		}
		return &sqlmodel.AnalysedQuery{Name: "Probe", Parameters: parameters}
	}
	testCases := []struct {
		name    string
		queries []*sqlmodel.AnalysedQuery
		want    bool
	}{
		{name: "a time of day", queries: []*sqlmodel.AnalysedQuery{query(timestamp), query(timeOfDay)}, want: true},
		{name: "an array of times of day", queries: []*sqlmodel.AnalysedQuery{query(listed)}, want: true},
		{name: "timestamps only", queries: []*sqlmodel.AnalysedQuery{query(timestamp)}, want: false},
		{name: "no parameter", queries: []*sqlmodel.AnalysedQuery{query()}, want: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, chvalues.BindsDurations(testCase.queries))
		})
	}
}

func TestDocumentBindsAParameterAsTheDocumentItsValueEncodesTo(t *testing.T) {
	t.Parallel()

	var printed bytes.Buffer
	require.NoError(t, printer.Fprint(&printed, token.NewFileSet(), chvalues.Document(acmeVocabulary(), goastutil.CachedIdent("data"))))

	require.Equal(t, "acmeClickHouseEncoded(data)", printed.String())
}

func TestAppendingRendersTheConversionBesideTheValueHelper(t *testing.T) {
	t.Parallel()

	values, err := chvalues.Values(acmeVocabulary(), "store", chvalues.ValueParts{Documents: false, Nullables: false})
	require.NoError(t, err)
	appending, err := chvalues.Appending(acmeVocabulary(), "store")
	require.NoError(t, err)
	require.Equal(t, chvalues.AppendingFile, appending.Name)
	content := string(appending.Content)
	require.Contains(t, content, "package store")
	require.Contains(t, content, "func acmeAppendNativeRow(appendRow func(...any) error, shape string, values ...any) error {")
	require.Contains(t, content, `errors.New("acme: cannot append the value of ClickHouse parameter")`)
	require.NotContains(t, content, "sgl")

	fileSet := token.NewFileSet()
	files := []sqlmodel.GeneratedFile{values, appending}
	syntax := make([]*ast.File, 0, len(files))
	for _, file := range files {
		parsed, err := parser.ParseFile(fileSet, file.Name, file.Content, parser.AllErrors)
		require.NoError(t, err)
		syntax = append(syntax, parsed)
	}
	config := types.Config{Importer: importer.Default()}
	checked, err := config.Check("store", fileSet, syntax, nil)
	require.NoError(t, err)

	listed := make([]string, 0, len(chvalues.Helpers)+len(chvalues.Sentinels)+len(chvalues.AppendingHelpers)+len(chvalues.AppendingSentinels))
	for _, suffix := range slices.Concat(chvalues.Helpers, chvalues.AppendingHelpers) {
		listed = append(listed, "acme"+suffix)
	}
	for _, suffix := range slices.Concat(chvalues.Sentinels, chvalues.AppendingSentinels) {
		listed = append(listed, "errAcme"+suffix)
	}
	slices.Sort(listed)
	require.Equal(t, checked.Scope().Names(), listed, "the helpers and sentinels lists name every declaration of the two files")
}

func TestAppendsRoutesAValueHoldingAnAddressOrAMapOfTuples(t *testing.T) {
	t.Parallel()

	text := typeOf(sqlmodel.TypeCategoryText)
	mapOf := func(value sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryMap, KeyType: &text, ElementType: &value}
	}
	tupleOf := func(fields ...sqlmodel.SQLType) sqlmodel.SQLType {
		tuple := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryStruct}
		for index, field := range fields {
			tuple.StructFields = append(tuple.StructFields, sqlmodel.StructField{Name: string(rune('a' + index)), SQLType: field})
		}
		return tuple
	}
	integer := typeOf(sqlmodel.TypeCategoryInteger)

	testCases := []struct {
		name    string
		sqlType sqlmodel.SQLType
		appends bool
	}{
		{name: "an IPv4 address", sqlType: addressOf(32), appends: true},
		{name: "an IPv6 address", sqlType: addressOf(128), appends: true},
		{name: "an array of addresses", sqlType: arrayOf(addressOf(32)), appends: true},
		{name: "a tuple holding an address", sqlType: tupleOf(integer, addressOf(128)), appends: true},
		{name: "a map of addresses", sqlType: mapOf(addressOf(32)), appends: true},
		{name: "a map of tuples", sqlType: mapOf(tupleOf(integer)), appends: true},
		{name: "a map of arrays of tuples", sqlType: mapOf(arrayOf(tupleOf(integer))), appends: true},
		{name: "a tuple holding a map of tuples", sqlType: tupleOf(mapOf(tupleOf(integer))), appends: true},
		{name: "a number", sqlType: integer},
		{name: "a network value other than an address", sqlType: typeOf(sqlmodel.TypeCategoryNetwork)},
		{name: "a map of numbers", sqlType: mapOf(integer)},
		{name: "a tuple of numbers", sqlType: tupleOf(integer, text)},
		{name: "an array of tuples", sqlType: arrayOf(tupleOf(integer))},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.appends, chvalues.Appends(&testCase.sqlType))
		})
	}
}

func TestAppendRowNamesEachValueAfterItsParameter(t *testing.T) {
	t.Parallel()

	parameters := []*sqlmodel.QueryParameter{
		{Name: "id", SQLType: typeOf(sqlmodel.TypeCategoryInteger)},
		{Name: "host", SQLType: addressOf(32)},
		{Name: "it's", SQLType: arrayOf(addressOf(128))},
	}
	values := []ast.Expr{
		&ast.SelectorExpr{X: ast.NewIdent("item"), Sel: ast.NewIdent("ID")},
		&ast.SelectorExpr{X: ast.NewIdent("item"), Sel: ast.NewIdent("Host")},
		&ast.SelectorExpr{X: ast.NewIdent("item"), Sel: ast.NewIdent("Its")},
	}

	call := chvalues.AppendRow(acmeVocabulary(), &ast.SelectorExpr{X: ast.NewIdent("batch"), Sel: ast.NewIdent("Append")}, parameters, values)

	var printed bytes.Buffer
	require.NoError(t, printer.Fprint(&printed, token.NewFileSet(), call))
	require.Equal(t, `acmeAppendNativeRow(batch.Append, "('id'=a,'host'=4,'it''s'=[6])", item.ID, item.Host, item.Its)`, printed.String())
}

func TestArrayListsTypesASliceParametersPlaceholderAsAnArray(t *testing.T) {
	t.Parallel()

	info := dialect.Wrap(clickhouse.New()).Info()
	list := sqlmodel.SQLSegment{Kind: sqlmodel.SegmentList, Parameter: 1, Text: "IN ", TypeText: "UInt64"}
	query := &sqlmodel.AnalysedQuery{
		Name: "Find",
		Parameters: []sqlmodel.QueryParameter{
			{Name: "kind", SQLType: typeOf(sqlmodel.TypeCategoryText)},
			{Name: "ids", IsSlice: true, SQLType: typeOf(sqlmodel.TypeCategoryInteger)},
		},
		Template: sqlmodel.SQLTemplate{Segments: []sqlmodel.SQLSegment{
			{Kind: sqlmodel.SegmentText, Text: "SELECT id FROM t WHERE "},
			{Kind: sqlmodel.SegmentFilter, Present: true, Children: []sqlmodel.SQLSegment{
				{Kind: sqlmodel.SegmentText, Text: "kind = "},
				{Kind: sqlmodel.SegmentParameter, Parameter: 0, TypeText: "String"},
				{Kind: sqlmodel.SegmentText, Text: " AND id "},
				list,
			}},
		}},
		CountTemplate: &sqlmodel.SQLTemplate{Segments: []sqlmodel.SQLSegment{
			{Kind: sqlmodel.SegmentText, Text: "SELECT count() FROM t WHERE id "},
			list,
		}},
	}
	plain := &sqlmodel.AnalysedQuery{Name: "Plain", Template: sqlmodel.SQLTemplate{Segments: []sqlmodel.SQLSegment{{Kind: sqlmodel.SegmentText, Text: "SELECT 1"}}}}
	queries := []*sqlmodel.AnalysedQuery{plain, query}

	changed := chvalues.ArrayLists(queries)

	require.Same(t, plain, changed[0])
	require.NotSame(t, query, changed[1])
	require.Equal(t, "UInt64", query.Template.Segments[1].Children[3].TypeText, "the queries passed in never change")
	require.Equal(t, "UInt64", query.CountTemplate.Segments[1].TypeText, "the queries passed in never change")
	renderer := &emitkit.Renderer{
		Query:        changed[1],
		Form:         info.Placeholders.Execution,
		Placeholders: info.Placeholders,
		Quote:        info.IdentifierQuote,
		Lists:        info.Lists,
		ListMode:     emitkit.ListAsWritten,
	}
	rendered, err := emitkit.Render(changed[1].Template, renderer)
	require.NoError(t, err)
	require.Equal(t, "SELECT id FROM t WHERE kind = {p_kind:String} AND id IN ({p_ids:Array(UInt64)})", rendered.SQL)
	count, err := emitkit.Render(*changed[1].CountTemplate, renderer)
	require.NoError(t, err)
	require.Equal(t, "SELECT count() FROM t WHERE id IN ({p_ids:Array(UInt64)})", count.SQL)

	unchanged := []*sqlmodel.AnalysedQuery{plain}
	require.Equal(t, unchanged, chvalues.ArrayLists(unchanged))
}

func TestCompiledShapesAreSharedWithinThePackage(t *testing.T) {
	var shapes chvalues.Shapes
	vocabulary := acmeVocabulary()
	parameter := sqlmodel.QueryParameter{SQLType: typeOf(sqlmodel.TypeCategoryInteger)}
	first := shapes.Bind(vocabulary, ast.NewIdent("value"), &parameter)
	second := shapes.Bind(vocabulary, ast.NewIdent("value"), &parameter)
	var before, after bytes.Buffer
	require.NoError(t, printer.Fprint(&before, token.NewFileSet(), first))
	require.NoError(t, printer.Fprint(&after, token.NewFileSet(), second))
	require.Equal(t, before.String(), after.String())
	require.Equal(t, "acmeClickHouseShapedValue(value, acmeClickHouseShapes[0])", before.String())
	parameter.SQLType = typeOf(sqlmodel.TypeCategoryText)
	shapes.Bind(vocabulary, ast.NewIdent("value"), &parameter)
	file, err := chvalues.Values(vocabulary, "store", chvalues.ValueParts{Shapes: &shapes})
	require.NoError(t, err)
	syntax, err := parser.ParseFile(token.NewFileSet(), file.Name, file.Content, 0)
	require.NoError(t, err)
	found := false
	for node := range ast.Preorder(syntax) {
		spec, ok := node.(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || spec.Names[0].Name != "acmeClickHouseShapes" {
			continue
		}
		descriptor, ok := spec.Values[0].(*ast.CompositeLit)
		require.True(t, ok)
		require.Len(t, descriptor.Elts, 2)
		found = true
		break
	}
	require.True(t, found)
}

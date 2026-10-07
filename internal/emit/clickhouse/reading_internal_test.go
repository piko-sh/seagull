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

package clickhouse

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/emit/internal/chvalues"
	"seagull.sh/seagull/internal/emit/internal/valuereading"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/helpers"
	"seagull.sh/seagull/internal/sqlmodel"
)

const chcolStub = `package chcol

type JSON struct{ values map[string]any }

func (document *JSON) MarshalJSON() ([]byte, error) { return nil, nil }
`

type chcolImporter struct {
	standard types.Importer
	fileSet  *token.FileSet
}

func (stub chcolImporter) Import(importPath string) (*types.Package, error) {
	if importPath != chvalues.DocumentPackage {
		return stub.standard.Import(importPath)
	}
	parsed, err := parser.ParseFile(stub.fileSet, "chcol.go", chcolStub, 0)
	if err != nil {
		return nil, err
	}
	config := types.Config{Importer: stub.standard}
	return config.Check(importPath, stub.fileSet, []*ast.File{parsed}, nil)
}

func acmeVocabulary() gogen.Vocabulary {
	request := &emit.Request{}
	request.Style.HelperPrefix = "acme"
	request.Style.ErrorPrefix = "acme"
	return gogen.NewVocabulary(request)
}

func checkedNames(t *testing.T, files ...sqlmodel.GeneratedFile) []string {
	t.Helper()
	fileSet := token.NewFileSet()
	syntax := make([]*ast.File, 0, len(files))
	for _, file := range files {
		parsed, err := parser.ParseFile(fileSet, file.Name, file.Content, parser.AllErrors)
		require.NoError(t, err)
		syntax = append(syntax, parsed)
	}
	config := types.Config{Importer: chcolImporter{standard: importer.Default(), fileSet: fileSet}}
	checked, err := config.Check("store", fileSet, syntax, nil)
	require.NoError(t, err)
	return checked.Scope().Names()
}

func acmeNames(suffixes ...[]string) []string {
	concatenated := slices.Concat(suffixes...)
	listed := make([]string, 0, len(concatenated))
	for _, suffix := range concatenated {
		listed = append(listed, "acme"+suffix)
	}
	slices.Sort(listed)
	return listed
}

func TestRenderCompositesDeclaresOnlyTheReservedHelpers(t *testing.T) {
	t.Parallel()

	file, err := renderComposites(acmeVocabulary(), "store", compositeParts{documents: false, entries: false})
	require.NoError(t, err)
	require.Equal(t, compositesFile, file.Name)
	content := string(file.Content)
	require.Contains(t, content, "package store")
	require.Contains(t, content, "func acmeScanNativeComposite[T any](dest *T) sql.Scanner {")
	require.NotContains(t, content, "sgl")
	require.NotContains(t, content, "chcol", "a package reading no JSON document does not import chcol")
	require.NotContains(t, content, "Entries")

	reading, err := valuereading.Render(acmeVocabulary(), "store")
	require.NoError(t, err)
	require.Equal(t, acmeNames(compositeHelpers, valuereading.Helpers), checkedNames(t, file, reading),
		"compositeHelpers lists every declaration of the adapter's file beside the shared readers")
}

func TestRenderCompositesReadsJSONDocumentsThroughTheSharedReading(t *testing.T) {
	t.Parallel()

	composites, err := renderComposites(acmeVocabulary(), "store", compositeParts{documents: true, entries: true})
	require.NoError(t, err)
	documents, err := chvalues.Documents(acmeVocabulary(), "store", helpers.Code{Text: "JSON", Imports: nil})
	require.NoError(t, err)
	jsonType := sqlmodel.GeneratedFile{Name: "json.go", Content: []byte("package store\n\ntype JSON []byte\n")}
	content := string(composites.Content)
	require.Contains(t, content, "func (scanner *acmeNativeCompositeScanner[T]) DeserializeClickHouseJSON(document *chcol.JSON) error {")
	require.Contains(t, content, "acmeReadClickHouseDocuments(target, source)")
	require.Contains(t, content, "type acmeNativeEntries map[any]any")
	require.NotContains(t, content, "func acmeReadClickHouseDocuments", "the reading is the shared file's")
	require.NotContains(t, content, "sgl")

	reading, err := valuereading.Render(acmeVocabulary(), "store")
	require.NoError(t, err)
	want := append(acmeNames(compositeHelpers, documentHelpers, entriesHelpers, chvalues.DocumentHelpers, valuereading.Helpers), "JSON")
	slices.Sort(want)
	require.Equal(t, want, checkedNames(t, composites, documents, reading, jsonType), "the helper lists name every declaration of the files")
}

func TestColumnReadingConvertsTheValuesTheDriverReadsInGoTypesOfItsOwn(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}
	wide := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger, Width: 128}
	nullableWide := wide
	nullableWide.Nullable = true
	address := sqlmodel.NewNetworkType("IPv4", sqlmodel.NetworkKindAddress)
	nullableAddress := address
	nullableAddress.Nullable = true
	text := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}
	namedOf := func(parts ...sqlmodel.SQLType) sqlmodel.SQLType {
		fields := make([]sqlmodel.StructField, 0, len(parts))
		for index := range parts {
			fields = append(fields, sqlmodel.StructField{Name: string(rune('a' + index)), SQLType: parts[index]})
		}
		return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryStruct, StructFields: fields}
	}
	unnamedOf := func(parts ...sqlmodel.SQLType) sqlmodel.SQLType {
		fields := make([]sqlmodel.StructField, 0, len(parts))
		for index := range parts {
			fields = append(fields, sqlmodel.StructField{Name: string(rune('1' + index)), Positional: true, SQLType: parts[index]})
		}
		return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryStruct, StructFields: fields}
	}
	mapOf := func(value sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryMap, KeyType: &text, ElementType: &value}
	}
	arrayOf := func(element sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, ElementType: &element}
	}
	holder := namedOf(integer, mapOf(namedOf(integer)))

	testCases := []struct {
		name    string
		sqlType sqlmodel.SQLType
		want    gogen.ColumnReading
	}{
		{name: "a map to named tuples", sqlType: mapOf(namedOf(integer, integer)), want: gogen.ColumnReadConverted},
		{name: "an array of maps to named tuples", sqlType: arrayOf(mapOf(namedOf(integer))), want: gogen.ColumnReadConverted},
		{name: "a map to arrays of named tuples", sqlType: mapOf(arrayOf(namedOf(integer))), want: gogen.ColumnReadConverted},
		{name: "a map to maps to named tuples", sqlType: mapOf(mapOf(namedOf(integer))), want: gogen.ColumnReadConverted},
		{name: "an array of named tuples holding a map to named tuples", sqlType: arrayOf(holder), want: gogen.ColumnReadConverted},
		{name: "a named tuple holding a map to named tuples", sqlType: holder, want: gogen.ColumnReadRaw},
		{name: "a map to unnamed tuples", sqlType: mapOf(unnamedOf(integer)), want: gogen.ColumnReadAsIs},
		{name: "an array of unnamed tuples holding a map to named tuples", sqlType: arrayOf(unnamedOf(integer, mapOf(namedOf(integer)))), want: gogen.ColumnReadAsIs},
		{name: "a map to text", sqlType: mapOf(text), want: gogen.ColumnReadAsIs},
		{name: "an array of named tuples", sqlType: arrayOf(namedOf(integer)), want: gogen.ColumnReadAsIs},
		{name: "an address", sqlType: address, want: gogen.ColumnReadAsIs},
		{name: "an array of addresses", sqlType: arrayOf(address), want: gogen.ColumnReadConverted},
		{name: "an array of nullable addresses", sqlType: arrayOf(nullableAddress), want: gogen.ColumnReadConverted},
		{name: "an array of arrays of addresses", sqlType: arrayOf(arrayOf(address)), want: gogen.ColumnReadConverted},
		{name: "a map to addresses", sqlType: mapOf(address), want: gogen.ColumnReadConverted},
		{name: "a named tuple holding an address", sqlType: namedOf(integer, address), want: gogen.ColumnReadRaw},
		{name: "a named tuple holding a nullable address", sqlType: namedOf(integer, nullableAddress), want: gogen.ColumnReadRaw},
		{name: "a named tuple holding an array of addresses", sqlType: namedOf(integer, arrayOf(address)), want: gogen.ColumnReadRaw},
		{name: "a named tuple holding a map to addresses", sqlType: namedOf(integer, mapOf(address)), want: gogen.ColumnReadRaw},
		{name: "a named tuple holding a named tuple holding an address", sqlType: namedOf(integer, namedOf(address)), want: gogen.ColumnReadRaw},
		{name: "an array of named tuples holding an address", sqlType: arrayOf(namedOf(integer, address)), want: gogen.ColumnReadConverted},
		{name: "a wide integer", sqlType: wide, want: gogen.ColumnReadAsIs},
		{name: "an array of wide integers", sqlType: arrayOf(wide), want: gogen.ColumnReadAsIs},
		{name: "a named tuple holding a wide integer", sqlType: namedOf(integer, wide), want: gogen.ColumnReadRaw},
		{name: "a named tuple holding a nullable wide integer", sqlType: namedOf(integer, nullableWide), want: gogen.ColumnReadAsIs},
		{name: "a named tuple holding an array of wide integers", sqlType: namedOf(integer, arrayOf(wide)), want: gogen.ColumnReadAsIs},
		{name: "a named tuple holding a named tuple holding a wide integer", sqlType: namedOf(integer, namedOf(wide)), want: gogen.ColumnReadRaw},
		{name: "an array of named tuples holding a wide integer", sqlType: arrayOf(namedOf(integer, wide)), want: gogen.ColumnReadConverted},
		{name: "an array of named tuples holding a nullable wide integer", sqlType: arrayOf(namedOf(integer, nullableWide)), want: gogen.ColumnReadAsIs},
		{name: "an unnamed tuple holding an address", sqlType: unnamedOf(integer, address), want: gogen.ColumnReadAsIs},
		{name: "an array of unnamed tuples holding an address", sqlType: arrayOf(unnamedOf(integer, address)), want: gogen.ColumnReadAsIs},
		{name: "a named tuple of plain values", sqlType: namedOf(integer, text), want: gogen.ColumnReadAsIs},
	}
	driver := &nativeDriver{}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, driver.ColumnReading(testCase.sqlType))
		})
	}
}

func TestColumnReadingReadsValuesOfUnknownTypeThroughTheConversion(t *testing.T) {
	t.Parallel()

	unknown := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUnknown, EngineName: "Dynamic"}
	integer := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}
	text := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}
	testCases := []struct {
		name    string
		sqlType sqlmodel.SQLType
		want    gogen.ColumnReading
	}{
		{name: "a value of unknown type", sqlType: unknown, want: gogen.ColumnReadAdapted},
		{name: "an array of values of unknown type", sqlType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, ElementType: &unknown},
			want: gogen.ColumnReadConverted},
		{name: "a map to values of unknown type", sqlType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryMap, KeyType: &text, ElementType: &unknown},
			want: gogen.ColumnReadConverted},
		{name: "a named tuple holding a value of unknown type", sqlType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryStruct,
			StructFields: []sqlmodel.StructField{{Name: "a", SQLType: unknown}, {Name: "b", SQLType: integer}}}, want: gogen.ColumnReadRaw},
		{name: "an unnamed tuple holding a value of unknown type", sqlType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryStruct,
			StructFields: []sqlmodel.StructField{{Name: "1", Positional: true, SQLType: unknown}}}, want: gogen.ColumnReadAsIs},
		{name: "text", sqlType: text, want: gogen.ColumnReadAsIs},
	}
	driver := &nativeDriver{}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, driver.ColumnReading(testCase.sqlType))
		})
	}
}

func TestColumnReadingReadsEveryJSONDocumentThroughTheConversion(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}
	text := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}
	document := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryJSON}
	nullableDocument := document
	nullableDocument.Nullable = true
	namedOf := func(parts ...sqlmodel.SQLType) sqlmodel.SQLType {
		fields := make([]sqlmodel.StructField, 0, len(parts))
		for index := range parts {
			fields = append(fields, sqlmodel.StructField{Name: string(rune('a' + index)), SQLType: parts[index]})
		}
		return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryStruct, StructFields: fields}
	}
	unnamedOf := func(parts ...sqlmodel.SQLType) sqlmodel.SQLType {
		fields := make([]sqlmodel.StructField, 0, len(parts))
		for index := range parts {
			fields = append(fields, sqlmodel.StructField{Name: string(rune('1' + index)), Positional: true, SQLType: parts[index]})
		}
		return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryStruct, StructFields: fields}
	}
	mapOf := func(value sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryMap, KeyType: &text, ElementType: &value}
	}
	arrayOf := func(element sqlmodel.SQLType) sqlmodel.SQLType {
		return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, ElementType: &element}
	}

	testCases := []struct {
		name    string
		sqlType sqlmodel.SQLType
		want    gogen.ColumnReading
		raw     string
	}{
		{name: "a document", sqlType: document, want: gogen.ColumnReadAdapted},
		{name: "a nullable document", sqlType: nullableDocument, want: gogen.ColumnReadAdapted},
		{name: "an array of documents", sqlType: arrayOf(document), want: gogen.ColumnReadConverted},
		{name: "an array of nullable documents", sqlType: arrayOf(nullableDocument), want: gogen.ColumnReadConverted},
		{name: "a map of documents", sqlType: mapOf(document), want: gogen.ColumnReadRaw, raw: "acmeNativeEntries"},
		{name: "a map of nullable documents", sqlType: mapOf(nullableDocument), want: gogen.ColumnReadConverted},
		{name: "a map of arrays of documents", sqlType: mapOf(arrayOf(document)), want: gogen.ColumnReadConverted},
		{name: "a map of named tuples holding a document", sqlType: mapOf(namedOf(document, integer)), want: gogen.ColumnReadConverted},
		{name: "an array of named tuples holding a document", sqlType: arrayOf(namedOf(document, integer)), want: gogen.ColumnReadConverted},
		{name: "a named tuple holding a document", sqlType: namedOf(document, integer), want: gogen.ColumnReadRaw, raw: "map[string]any"},
		{name: "a named tuple holding an array of documents", sqlType: namedOf(arrayOf(document)), want: gogen.ColumnReadRaw, raw: "map[string]any"},
		{name: "an unnamed tuple holding a document", sqlType: unnamedOf(document, integer), want: gogen.ColumnReadRaw, raw: "[]any"},
		{name: "an array of unnamed tuples holding a document", sqlType: arrayOf(unnamedOf(document, integer)), want: gogen.ColumnReadRaw, raw: "[][]any"},
		{name: "an array of arrays of unnamed tuples holding a document", sqlType: arrayOf(arrayOf(unnamedOf(document))), want: gogen.ColumnReadRaw,
			raw: "[][][]any"},
		{name: "a map of unnamed tuples holding a document", sqlType: mapOf(unnamedOf(document, integer)), want: gogen.ColumnReadConverted},
		{name: "an array of maps of unnamed tuples holding a document", sqlType: arrayOf(mapOf(unnamedOf(document))), want: gogen.ColumnReadConverted},
	}
	driver := &nativeDriver{vocabulary: acmeVocabulary()}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, driver.ColumnReading(testCase.sqlType))
			if testCase.raw != "" {
				require.Equal(t, testCase.raw, emitkit.NewFileImports(&emit.Naming{}).Type(driver.RawType(testCase.sqlType)))
			}
		})
	}
}

func TestHoldsDriverValuesLooksThroughEveryLevelGeneratedCodeFollows(t *testing.T) {
	t.Parallel()

	nested := func(levels int) *sqlmodel.SQLType {
		sqlType := sqlmodel.SQLType{EngineName: "IPv4", Category: sqlmodel.TypeCategoryNetwork, Network: sqlmodel.NetworkKindAddress, Width: 32}
		for range levels {
			sqlType = sqlmodel.SQLType{EngineName: "Array", Category: sqlmodel.TypeCategoryArray, ElementType: new(sqlType)}
		}
		return &sqlType
	}

	testCases := []struct {
		name   string
		levels int
		want   bool
	}{
		{name: "an address in an array", levels: 1, want: true},
		{name: "an address nested forty levels deep", levels: 40, want: true},
		{name: "an address at the deepest level generated code follows", levels: sqlmodel.MaxTypeDepth - 1, want: true},
		{name: "an address past the deepest level", levels: sqlmodel.MaxTypeDepth, want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, holdsDriverValues(nested(testCase.levels), placeColumn, 0))
		})
	}
}

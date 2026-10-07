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
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/emit/internal/chvalues"
	"seagull.sh/seagull/internal/gogen/helpers"
	"seagull.sh/seagull/internal/gotypes"
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

var jsonDocument = sqlmodel.SQLType{Category: sqlmodel.TypeCategoryJSON}

func nullableJSON() sqlmodel.SQLType {
	document := jsonDocument
	document.Nullable = true
	return document
}

func textMapOf(value sqlmodel.SQLType) sqlmodel.SQLType {
	return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryMap, KeyType: new(typeOf(sqlmodel.TypeCategoryText)), ElementType: &value}
}

func namedTupleOf(fields ...sqlmodel.SQLType) sqlmodel.SQLType {
	tuple := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryStruct}
	for index, field := range fields {
		tuple.StructFields = append(tuple.StructFields, sqlmodel.StructField{Name: string(rune('a' + index)), SQLType: field})
	}
	return tuple
}

func TestDocumentsRendersTheReadingOfJSONDocumentsForThePackage(t *testing.T) {
	t.Parallel()

	file, err := chvalues.Documents(acmeVocabulary(), "store", helpers.Code{Text: "JSON", Imports: nil})
	require.NoError(t, err)
	require.Equal(t, chvalues.DocumentsFile, file.Name)
	content := string(file.Content)
	require.Contains(t, content, "package store")
	require.Contains(t, content, "func acmeReadClickHouseDocuments(target reflect.Value, source any) (bool, error) {")
	require.Contains(t, content, "case *chcol.JSON:")
	require.Contains(t, content, "reflect.TypeFor[JSON]()")
	require.Contains(t, content, `"`+chvalues.DocumentPackage+`"`)
	require.NotContains(t, content, "sgl")
	require.NotContains(t, content, "MarshalJSON", "the stand-in's method is left out")

	fileSet := token.NewFileSet()
	documents, err := parser.ParseFile(fileSet, file.Name, file.Content, parser.AllErrors)
	require.NoError(t, err)
	jsonType, err := parser.ParseFile(fileSet, "json.go", "package store\n\ntype JSON []byte\n", 0)
	require.NoError(t, err)
	config := types.Config{Importer: chcolImporter{standard: importer.Default(), fileSet: fileSet}}
	checked, err := config.Check("store", fileSet, []*ast.File{documents, jsonType}, nil)
	require.NoError(t, err)

	listed := make([]string, 0, len(chvalues.DocumentHelpers)+1)
	for _, suffix := range chvalues.DocumentHelpers {
		listed = append(listed, "acme"+suffix)
	}
	listed = append(listed, "JSON")
	slices.Sort(listed)
	require.Equal(t, checked.Scope().Names(), listed, "DocumentHelpers lists every declaration of the file")
}

func TestJSONTypeWritesTheGoTypeTheJSONSlotMapsADocumentTo(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		slot sqlmodel.GoType
		want helpers.Code
	}{
		{name: "the generated JSON type", slot: sqlmodel.GoType{Name: "JSON"}, want: helpers.Code{Text: "JSON", Imports: nil}},
		{name: "a type of another package", slot: sqlmodel.GoType{Package: "encoding/json", Name: "RawMessage"},
			want: helpers.Code{Text: "json.RawMessage", Imports: []string{"encoding/json"}}},
		{name: "a versioned module's type", slot: sqlmodel.GoType{Package: "example.com/documents/v2", Name: "Document"},
			want: helpers.Code{Text: "documents.Document", Imports: []string{"example.com/documents/v2"}}},
		{name: "a slice of bytes", slot: sqlmodel.GoType{Name: "[]byte"}, want: helpers.Code{Text: "[]byte", Imports: nil}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			table := &gotypes.MappingTable{Mappings: []gotypes.Mapping{{
				SQLCategory: sqlmodel.TypeCategoryJSON, NotNull: testCase.slot, Nullable: testCase.slot,
			}}}

			require.Equal(t, testCase.want, chvalues.JSONType(table))
		})
	}
}

func TestHoldsJSONFindsADocumentAtAnyDepth(t *testing.T) {
	t.Parallel()

	integer := typeOf(sqlmodel.TypeCategoryInteger)
	testCases := []struct {
		name     string
		sqlType  sqlmodel.SQLType
		holds    bool
		maps     bool
		nestsMap bool
	}{
		{name: "a document", sqlType: jsonDocument, holds: true},
		{name: "a nullable document", sqlType: nullableJSON(), holds: true},
		{name: "an array of documents", sqlType: arrayOf(jsonDocument), holds: true},
		{name: "a map of documents", sqlType: textMapOf(jsonDocument), holds: true, maps: true},
		{name: "a map of nullable documents", sqlType: textMapOf(nullableJSON()), holds: true},
		{name: "a map of arrays of documents", sqlType: textMapOf(arrayOf(jsonDocument)), holds: true},
		{name: "a tuple holding a document", sqlType: namedTupleOf(jsonDocument, integer), holds: true},
		{name: "an array of maps of documents", sqlType: arrayOf(textMapOf(jsonDocument)), holds: true, nestsMap: true},
		{name: "a tuple holding a map of documents", sqlType: namedTupleOf(textMapOf(jsonDocument)), holds: true, nestsMap: true},
		{name: "a map of maps of documents", sqlType: textMapOf(textMapOf(jsonDocument)), holds: true, nestsMap: true},
		{name: "a number", sqlType: integer},
		{name: "an array of numbers", sqlType: arrayOf(integer)},
		{name: "a map of tuples of numbers", sqlType: textMapOf(namedTupleOf(integer))},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.holds, chvalues.HoldsJSON(&testCase.sqlType), "HoldsJSON")
			require.Equal(t, testCase.maps, chvalues.MapsJSON(&testCase.sqlType), "MapsJSON")
			require.Equal(t, testCase.nestsMap, chvalues.NestsJSONMap(&testCase.sqlType), "NestsJSONMap")
		})
	}
}

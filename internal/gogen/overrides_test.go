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

	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gotypes"
	"seagull.sh/seagull/internal/sqlmodel"
)

type overrideReader struct {
	fixedReader
	reads bool
}

func (reader overrideReader) ReadsOverride(sqlmodel.GoType, sqlmodel.SQLType) bool {
	return reader.reads
}

func (overrideReader) OverrideScan(destination ast.Expr, _ sqlmodel.SQLType) ast.Expr {
	return goastutil.CallExpr(goastutil.CachedIdent("sglScanOverride"), destination)
}

func TestOverrideChosenTellsTheUsersGoTypesFromTheGenerators(t *testing.T) {
	t.Parallel()

	label := sqlmodel.GoType{Package: "example.com/kinds", Name: "Label"}
	table := &gotypes.MappingTable{Mappings: []gotypes.Mapping{
		{SQLCategory: sqlmodel.TypeCategoryText, SQLName: "varchar", NotNull: label, Nullable: sqlmodel.GoType{Package: "example.com/kinds", Name: "*Label"}},
	}}
	varchar := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar")
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	testCases := []struct {
		types    sqlmodel.GoTypeResolver
		override *sqlmodel.GoType
		name     string
		sqlType  sqlmodel.SQLType
		chosen   bool
	}{
		{name: "a column's own override", types: table, override: &label, sqlType: text, chosen: true},
		{name: "a SQL type override", types: table, sqlType: varchar, chosen: true},
		{name: "a SQL type override of an array's elements", types: table,
			sqlType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, EngineName: "varchar[]", ElementType: &varchar}, chosen: true},
		{name: "a type no override covers", types: table, sqlType: text, chosen: false},
		{name: "a table that cannot tell", types: nil, sqlType: varchar, chosen: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.chosen, OverrideChosen(testCase.types, testCase.override, testCase.sqlType))
		})
	}
}

func TestPredeclaredGoTypeKnowsGosOwnTypes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		goType      sqlmodel.GoType
		predeclared bool
	}{
		{goType: sqlmodel.GoType{Name: "string"}, predeclared: true},
		{goType: sqlmodel.GoType{Name: "*int32"}, predeclared: true},
		{goType: sqlmodel.GoType{Name: "[]byte"}, predeclared: true},
		{goType: sqlmodel.GoType{Name: "[]*bool"}, predeclared: true},
		{goType: sqlmodel.GoType{Package: "encoding/json", Name: "RawMessage"}, predeclared: false},
		{goType: sqlmodel.GoType{Name: "Mood"}, predeclared: false},
		{goType: sqlmodel.GoType{Package: "example.com/kinds", Name: "[]Label"}, predeclared: false},
		{goType: sqlmodel.NewGoMap(sqlmodel.GoType{Name: "string"}, sqlmodel.GoType{Name: "string"}), predeclared: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.goType.Package+"."+testCase.goType.Name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.predeclared, PredeclaredGoType(testCase.goType))
		})
	}
}

func TestChosenGoTypeWritesTheOverrideOrTheMappedType(t *testing.T) {
	t.Parallel()

	label := sqlmodel.GoType{Package: "example.com/kinds", Name: "Label"}
	labels := sqlmodel.GoType{Package: "example.com/kinds", Name: "[]Label"}
	table := &gotypes.MappingTable{Mappings: []gotypes.Mapping{
		{SQLCategory: sqlmodel.TypeCategoryText, NotNull: sqlmodel.GoType{Name: "string"}, Nullable: sqlmodel.GoType{Name: "*string"}},
	}}
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	textArray := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, EngineName: "text[]", ElementType: &text}
	testCases := []struct {
		override *sqlmodel.GoType
		name     string
		want     sqlmodel.GoType
		sqlType  sqlmodel.SQLType
		nullable bool
	}{
		{name: "an override of a value that cannot be NULL", override: &label, sqlType: text, want: label},
		{name: "an override of a nullable value", override: &label, sqlType: text, nullable: true,
			want: sqlmodel.GoType{Package: "example.com/kinds", Name: "*Label"}},
		{name: "a slice override of a nullable array", override: &labels, sqlType: textArray, nullable: true, want: labels},
		{name: "no override", sqlType: text, want: sqlmodel.GoType{Name: "string"}},
		{name: "no override of a nullable value", sqlType: text, nullable: true, want: sqlmodel.GoType{Name: "*string"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, ChosenGoType(table, testCase.override, testCase.sqlType, testCase.nullable))
		})
	}
}

func TestHoldsTextOrBytesKnowsGosOwnTextAndBytes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		goType sqlmodel.GoType
		holds  bool
	}{
		{goType: sqlmodel.GoType{Name: "string"}, holds: true},
		{goType: sqlmodel.GoType{Name: "*string"}, holds: true},
		{goType: sqlmodel.GoType{Name: "[]byte"}, holds: true},
		{goType: sqlmodel.GoType{Name: "*[]uint8"}, holds: true},
		{goType: sqlmodel.GoType{Package: "encoding/json", Name: "RawMessage"}, holds: false},
		{goType: sqlmodel.GoType{Name: "Document"}, holds: false},
		{goType: sqlmodel.NewGoMap(sqlmodel.GoType{Name: "string"}, sqlmodel.GoType{Name: "string"}), holds: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.goType.Package+"."+testCase.goType.Name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.holds, HoldsTextOrBytes(testCase.goType))
		})
	}
}

func TestEncodesJSONDocumentBindsAnOverriddenJSONParameterAsItsDocument(t *testing.T) {
	t.Parallel()

	document := sqlmodel.GoType{Package: "example.com/kinds", Name: "Document"}
	text := sqlmodel.GoType{Name: "string"}
	jsonType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "jsonb")
	testCases := []struct {
		parameter sqlmodel.QueryParameter
		name      string
		encodes   bool
	}{
		{name: "a JSON parameter of a type of its own", encodes: true,
			parameter: sqlmodel.QueryParameter{Name: "body", SQLType: jsonType, GoTypeOverride: &document}},
		{name: "a JSON parameter held as text", encodes: false,
			parameter: sqlmodel.QueryParameter{Name: "body", SQLType: jsonType, GoTypeOverride: &text}},
		{name: "a JSON parameter no override chose", encodes: false,
			parameter: sqlmodel.QueryParameter{Name: "body", SQLType: jsonType}},
		{name: "a slice of JSON documents", encodes: false,
			parameter: sqlmodel.QueryParameter{Name: "bodies", SQLType: jsonType, GoTypeOverride: &document, IsSlice: true}},
		{name: "a parameter that is no JSON", encodes: false,
			parameter: sqlmodel.QueryParameter{Name: "label", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), GoTypeOverride: &document}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.encodes, EncodesJSONDocument(&gotypes.MappingTable{}, &testCase.parameter))
		})
	}
}

func TestEmissionReadsAnOverriddenColumnThroughTheDriversAdapter(t *testing.T) {
	t.Parallel()

	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	overridden := sqlmodel.OutputColumn{Name: "label", SQLType: text, GoTypeOverride: &sqlmodel.GoType{Package: "example.com/kinds", Name: "Label"}}
	plain := sqlmodel.OutputColumn{Name: "rank", SQLType: sqlmodel.NewIntegerType("integer", 32, false)}
	testCases := []struct {
		reader  ColumnReader
		name    string
		column  sqlmodel.OutputColumn
		scan    string
		reads   bool
		scanned []bool
	}{
		{name: "an adapter reading the override", reader: overrideReader{fixedReader: fixedReader{reading: ColumnReadAsIs}, reads: true},
			column: overridden, reads: true, scan: "sglScanOverride(&row.Label)", scanned: []bool{true}},
		{name: "an adapter leaving the override to the scan", reader: overrideReader{fixedReader: fixedReader{reading: ColumnReadAsIs}, reads: false},
			column: overridden, reads: false, scan: "sglScanOverride(&row.Label)", scanned: []bool{false}},
		{name: "a column no override chose", reader: overrideReader{fixedReader: fixedReader{reading: ColumnReadAsIs}, reads: true},
			column: plain, reads: false, scan: "sglScanOverride(&row.Label)", scanned: []bool{false}},
		{name: "a driver with no adapter", reader: fixedReader{reading: ColumnReadAsIs},
			column: overridden, reads: false, scan: "&row.Label", scanned: []bool{false}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			query := &sqlmodel.AnalysedQuery{
				Name: "ListLabels", Filename: "labels.sql", Command: sqlmodel.QueryCommandMany,
				OutputColumns: []sqlmodel.OutputColumn{testCase.column},
				Template:      sqlmodel.SQLTemplate{Segments: []sqlmodel.SQLSegment{textSegment("SELECT label FROM labels")}},
			}
			emission := renderingEmission(renderingInfo(dollarForm, dollarForm), emitkit.ListExpand, testCase.reader, query)
			destination := goastutil.AddressExpr(goastutil.SelectorExpr("row", "Label"))

			require.Equal(t, testCase.reads, emission.ReadsOverride(&query.OutputColumns[0]))
			require.Equal(t, testCase.reads, emission.ReadsAnyOverride())
			require.Equal(t, testCase.scan, renderExpression(t, emission.OverrideScan(destination, &query.OutputColumns[0])))
			scanned, err := emission.OverrideScannedColumns(query)
			require.NoError(t, err)
			require.Equal(t, testCase.scanned, scanned)
		})
	}
}

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

package methods

import (
	"go/ast"
	"testing"

	"github.com/stretchr/testify/require"
	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gotypes"
	"seagull.sh/seagull/internal/sqlmodel"
)

type convertingReader struct{}

func (convertingReader) ColumnReading(sqlType sqlmodel.SQLType) gogen.ColumnReading {
	if sqlType.Category == sqlmodel.TypeCategoryArray {
		return gogen.ColumnReadConverted
	}
	return gogen.ColumnReadAsIs
}

func (convertingReader) ConvertingScan(destination ast.Expr) ast.Expr {
	return goastutil.CallExpr(goastutil.CachedIdent("sglScanDriverComposite"), destination)
}

type convertingReaderWithoutAdapter struct{}

func (convertingReaderWithoutAdapter) ColumnReading(sqlmodel.SQLType) gogen.ColumnReading {
	return gogen.ColumnReadConverted
}

type rawReader struct{}

func (rawReader) ColumnReading(sqlType sqlmodel.SQLType) gogen.ColumnReading {
	if sqlType.Category == sqlmodel.TypeCategoryStruct {
		return gogen.ColumnReadRaw
	}
	return gogen.ColumnReadAsIs
}

func (rawReader) RawType(sqlmodel.SQLType) sqlmodel.GoType {
	return sqlmodel.NewGoMap(sqlmodel.GoType{Composite: nil, Package: "", Name: "string"}, sqlmodel.GoType{Composite: nil, Package: "", Name: "any"})
}

func (rawReader) ConvertRaw(destination, raw ast.Expr) ast.Expr {
	return goastutil.CallExpr(goastutil.CachedIdent("sglReadNativeColumn"), destination, raw)
}

func enumEmission(t *testing.T, enums gogen.EnumStrings) *gogen.Emission {
	t.Helper()
	driver := testDriver()
	driver.Enums = enums
	mappings, ok := textMappings().(*gotypes.MappingTable)
	require.True(t, ok)
	mappings.Namer = func(sqlType sqlmodel.SQLType) (string, bool) {
		return "Mood", sqlType.Category == sqlmodel.TypeCategoryEnum
	}
	return testEmission(testDialectInfo(dollar, dialect.PlaceholderSigil{}), mappings, driver)
}

func enumColumn(name string, nullable bool) sqlmodel.OutputColumn {
	return sqlmodel.OutputColumn{
		Name:     name,
		SQLType:  sqlmodel.SQLType{Category: sqlmodel.TypeCategoryEnum, EngineName: "mood", EnumValues: []string{"calm", "stormy"}},
		Nullable: nullable,
	}
}

func enumArrayColumn(name string) sqlmodel.OutputColumn {
	return sqlmodel.OutputColumn{Name: name, SQLType: sqlmodel.SQLType{
		Category:    sqlmodel.TypeCategoryArray,
		ElementType: &sqlmodel.SQLType{Category: sqlmodel.TypeCategoryEnum, EngineName: "mood", EnumValues: []string{"calm", "stormy"}},
	}}
}

func TestBuildScanReadsEnumsThroughGoStringsInTheShapesTheDriverNeeds(t *testing.T) {
	t.Parallel()

	columns := []sqlmodel.OutputColumn{textColumn("id", false), enumColumn("mood", false), enumColumn("maybe", true), enumArrayColumn("moods")}
	testCases := []struct {
		name         string
		enums        gogen.EnumStrings
		arguments    []string
		declarations string
		conversions  string
	}{
		{
			name:         "a driver converting values",
			enums:        gogen.EnumStrings{Scan: gogen.EnumValue, Bind: 0},
			arguments:    []string{"&row.ID", "(*string)(&row.Mood)", "&raw.Maybe", "&row.Moods"},
			declarations: "var raw struct {\n\tMaybe *string\n}\n",
			conversions:  "row.Maybe = (*Mood)(raw.Maybe)\n",
		},
		{
			name:         "a driver converting slices",
			enums:        gogen.EnumStrings{Scan: gogen.EnumSlice, Bind: 0},
			arguments:    []string{"&row.ID", "&row.Mood", "&row.Maybe", "&raw.Moods"},
			declarations: "var raw struct {\n\tMoods []string\n}\n",
			conversions:  "row.Moods = sglConvertStrings[Mood](raw.Moods)\n",
		},
		{
			name:         "a driver converting nothing",
			enums:        gogen.EnumStrings{},
			arguments:    []string{"&row.ID", "&row.Mood", "&row.Maybe", "&row.Moods"},
			declarations: "",
			conversions:  "",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			emission := enumEmission(t, testCase.enums)
			query := queryFor(t, emission, &sqlmodel.AnalysedQuery{
				Name:          "ListItems",
				Filename:      "items.sql",
				Command:       sqlmodel.QueryCommandMany,
				OutputColumns: columns,
				Template:      template(text("SELECT id, mood, maybe, moods FROM items")),
			})

			scan := BuildScan(emission, query)

			require.Equal(t, testCase.arguments, renderExpressions(t, scan.Arguments))
			require.Equal(t, testCase.declarations, renderStatements(t, scan.Declarations))
			require.Equal(t, testCase.conversions, renderStatements(t, scan.Conversions))
		})
	}
}

func TestBuildScanReadsSlicesOfPointersAndMapsThroughGoStrings(t *testing.T) {
	t.Parallel()

	element := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryEnum, EngineName: "mood", EnumValues: []string{"calm", "stormy"}, Nullable: true}
	value := element
	value.Nullable = false
	key := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryUUID, EngineName: "UUID"}
	columns := []sqlmodel.OutputColumn{
		{Name: "maybes", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, ElementType: &element}},
		{Name: "labels", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryMap, KeyType: &key, ElementType: &value}},
	}
	emission := enumEmission(t, gogen.EnumStrings{Scan: gogen.EnumSlice | gogen.EnumMap, Bind: 0})
	query := queryFor(t, emission, &sqlmodel.AnalysedQuery{
		Name:          "ListItems",
		Filename:      "items.sql",
		Command:       sqlmodel.QueryCommandMany,
		OutputColumns: columns,
		Template:      template(text("SELECT maybes, labels FROM items")),
	})

	scan := BuildScan(emission, query)

	require.Equal(t, []string{"&raw.Maybes", "&raw.Labels"}, renderExpressions(t, scan.Arguments))
	require.Equal(t, "var raw struct {\n\tMaybes\t[]*string\n\tLabels\tmap[uuid.UUID]string\n}\n", renderStatements(t, scan.Declarations))
	require.Equal(t, "row.Maybes = sglConvertStringPointers[Mood](raw.Maybes)\nrow.Labels = sglConvertStringMap[Mood](raw.Labels)\n",
		renderStatements(t, scan.Conversions))
}

func TestBuildScanGivesEachRawValueAFieldOfItsOwn(t *testing.T) {
	t.Parallel()

	emission := enumEmission(t, gogen.EnumStrings{Scan: gogen.EnumValue, Bind: 0})
	embedded := enumColumn("maybe", true)
	embedded.IsEmbedded = true
	embedded.EmbedTable = "users"
	embedded.EmbedName = "buyer"
	embedded.EmbedIsOuter = true
	query := queryFor(t, emission, &sqlmodel.AnalysedQuery{
		Name:          "ListOrders",
		Filename:      "orders.sql",
		Command:       sqlmodel.QueryCommandMany,
		OutputColumns: []sqlmodel.OutputColumn{enumColumn("maybe", true), embedded},
		Template:      template(text("SELECT maybe, buyer.maybe FROM orders")),
	})

	scan := BuildScan(emission, query)

	require.Equal(t, []string{"&raw.Maybe", "&raw.Maybe2"}, renderExpressions(t, scan.Arguments))
	require.Equal(t, "row.Maybe = (*Mood)(raw.Maybe)\nrow.Buyer.Maybe = (*Mood)(raw.Maybe2)\n", renderStatements(t, scan.Conversions))
}

func TestGroupedLocalsReadEnumsThroughGoStrings(t *testing.T) {
	t.Parallel()

	emission := enumEmission(t, gogen.EnumStrings{Scan: gogen.EnumValue, Bind: 0})
	query := queryFor(t, emission, &sqlmodel.AnalysedQuery{
		Name:       "ListOrders",
		Filename:   "orders.sql",
		Command:    sqlmodel.QueryCommandMany,
		GroupByKey: []string{"orders.id"},
		OutputColumns: []sqlmodel.OutputColumn{
			textColumn("id", false),
			enumColumn("mood", false),
			enumColumn("maybe", true),
			embeddedColumn("sku", "items", true),
		},
		Template: template(text("SELECT orders.id, orders.mood, orders.maybe, items.sku FROM orders LEFT JOIN items ON items.order_id = orders.id")),
	})
	locals := query.Naming.ColumnLocals

	source := renderDecl(t, GroupedManyMethod(emission, query, StaticPrelude(query)))

	require.Contains(t, source, "var "+locals[1]+" Mood")
	require.Contains(t, source, "var "+locals[2]+" *Mood")
	require.Contains(t, source, "rows.Scan(&"+locals[0]+", (*string)(&"+locals[1]+"), &raw.Maybe, &"+locals[3]+")")
	require.Contains(t, source, locals[2]+" = (*Mood)(raw.Maybe)")
}

func textArrayColumn(name string) sqlmodel.OutputColumn {
	return sqlmodel.OutputColumn{Name: name, SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, ElementType: &sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}}}
}

func convertingEmission(reader gogen.ColumnReader) *gogen.Emission {
	driver := testDriver()
	driver.Reader = reader
	return testEmission(testDialectInfo(dollar, dialect.PlaceholderSigil{}), textMappings(), driver)
}

func TestBuildScanReadsConvertedColumnsThroughTheDriversAdapter(t *testing.T) {
	t.Parallel()

	overridden := textArrayColumn("labels")
	overridden.GoTypeOverride = &sqlmodel.GoType{Composite: nil, Package: "example.com/labels", Name: "Labels"}
	embedded := textArrayColumn("tags")
	embedded.IsEmbedded = true
	embedded.EmbedTable = "users"
	embedded.EmbedName = "buyer"
	columns := []sqlmodel.OutputColumn{textColumn("id", false), textArrayColumn("tags"), overridden, embedded}
	testCases := []struct {
		name      string
		reader    gogen.ColumnReader
		arguments []string
	}{
		{
			name:      "a reader with a converting adapter",
			reader:    convertingReader{},
			arguments: []string{"&row.ID", "sglScanDriverComposite(&row.Tags)", "&row.Labels", "sglScanDriverComposite(&row.Buyer.Tags)"},
		},
		{
			name:      "a reader without a converting adapter",
			reader:    convertingReaderWithoutAdapter{},
			arguments: []string{"&row.ID", "&row.Tags", "&row.Labels", "&row.Buyer.Tags"},
		},
		{
			name:      "no reader",
			reader:    nil,
			arguments: []string{"&row.ID", "&row.Tags", "&row.Labels", "&row.Buyer.Tags"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			emission := convertingEmission(testCase.reader)
			query := queryFor(t, emission, &sqlmodel.AnalysedQuery{
				Name:          "ListOrders",
				Filename:      "orders.sql",
				Command:       sqlmodel.QueryCommandMany,
				OutputColumns: columns,
				Template:      template(text("SELECT id, tags, labels, buyer.tags FROM orders")),
			})

			scan := BuildScan(emission, query)

			require.Equal(t, testCase.arguments, renderExpressions(t, scan.Arguments))
			require.Equal(t, "SELECT id, tags, labels, buyer.tags FROM orders", query.Rendered.SQL)
		})
	}
}

func TestGroupedLocalsReadConvertedColumnsThroughTheDriversAdapter(t *testing.T) {
	t.Parallel()

	emission := convertingEmission(convertingReader{})
	query := queryFor(t, emission, &sqlmodel.AnalysedQuery{
		Name:          "ListOrders",
		Filename:      "orders.sql",
		Command:       sqlmodel.QueryCommandMany,
		GroupByKey:    []string{"orders.id"},
		OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false), textArrayColumn("tags"), embeddedColumn("sku", "items", true)},
		Template:      template(text("SELECT orders.id, orders.tags, items.sku FROM orders LEFT JOIN items ON items.order_id = orders.id")),
	})
	locals := query.Naming.ColumnLocals

	source := renderDecl(t, GroupedManyMethod(emission, query, StaticPrelude(query)))

	require.Contains(t, source, "rows.Scan(&"+locals[0]+", sglScanDriverComposite(&"+locals[1]+"), &"+locals[2]+")")
}

func structColumn(name string) sqlmodel.OutputColumn {
	field := sqlmodel.StructField{Name: "label", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}}
	return sqlmodel.OutputColumn{Name: name, SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryStruct, StructFields: []sqlmodel.StructField{field}}}
}

func rawQuery(t *testing.T, emission *gogen.Emission, command sqlmodel.QueryCommand) *gogen.Query {
	t.Helper()

	overridden := structColumn("pinned")
	overridden.GoTypeOverride = &sqlmodel.GoType{Composite: nil, Package: "example.com/pins", Name: "Pin"}
	return queryFor(t, emission, &sqlmodel.AnalysedQuery{
		Name:          "ListFields",
		Filename:      "fields.sql",
		Command:       command,
		OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false), structColumn("fields"), overridden},
		Template:      template(text("SELECT id, fields, pinned FROM records")),
	})
}

func TestBuildScanReadsRawColumnsIntoALocalAndConvertsThemAfterTheScan(t *testing.T) {
	t.Parallel()

	emission := convertingEmission(rawReader{})
	scan := BuildScan(emission, rawQuery(t, emission, sqlmodel.QueryCommandMany))

	require.Equal(t, []string{"&row.ID", "&raw.Fields", "&row.Pinned"}, renderExpressions(t, scan.Arguments))
	require.Equal(t, "var raw struct {\n\tFields map[string]any\n}\n", renderStatements(t, scan.Declarations))
	require.Empty(t, scan.Conversions)
	require.Equal(t, "if err := sglReadNativeColumn(&row.Fields, raw.Fields); err != nil {\n\treturn nil, err\n}\n",
		renderStatements(t, scan.CheckedConversions(goastutil.ReturnStmt(goastutil.CachedIdent("nil"), goastutil.CachedIdent("err")))))
}

func TestEveryReadingMethodChecksTheConversionOfARawColumn(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		command sqlmodel.QueryCommand
		want    string
	}{
		{name: "one row", command: sqlmodel.QueryCommandOne,
			want: "if err := sglReadNativeColumn(&row.Fields, raw.Fields); err != nil {\n\t\treturn ListFieldsRow{}, err\n\t}"},
		{name: "many rows", command: sqlmodel.QueryCommandMany,
			want: "if err := sglReadNativeColumn(&row.Fields, raw.Fields); err != nil {\n\t\t\treturn nil, err\n\t\t}"},
		{name: "a stream of rows", command: sqlmodel.QueryCommandStream,
			want: "if err := sglReadNativeColumn(&row.Fields, raw.Fields); err != nil {"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			emission := convertingEmission(rawReader{})
			query := rawQuery(t, emission, testCase.command)
			method, err := Method(emission, query, StaticPrelude(query))
			require.NoError(t, err)

			source := renderDecl(t, method)
			require.Contains(t, source, "&raw.Fields")
			require.Contains(t, source, testCase.want)
		})
	}
}

func TestGroupedLocalsReadRawColumnsAndConvertThemAfterTheScan(t *testing.T) {
	t.Parallel()

	emission := convertingEmission(rawReader{})
	query := queryFor(t, emission, &sqlmodel.AnalysedQuery{
		Name:          "ListOrders",
		Filename:      "orders.sql",
		Command:       sqlmodel.QueryCommandMany,
		GroupByKey:    []string{"orders.id"},
		OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false), structColumn("fields"), embeddedColumn("sku", "items", true)},
		Template:      template(text("SELECT orders.id, orders.fields, items.sku FROM orders LEFT JOIN items ON items.order_id = orders.id")),
	})
	locals := query.Naming.ColumnLocals

	source := renderDecl(t, GroupedManyMethod(emission, query, StaticPrelude(query)))

	require.Contains(t, source, "rows.Scan(&"+locals[0]+", &raw.Fields, &"+locals[2]+")")
	require.Contains(t, source, "if err := sglReadNativeColumn(&"+locals[1]+", raw.Fields); err != nil {")
}

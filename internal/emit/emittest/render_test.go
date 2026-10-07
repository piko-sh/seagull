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

package emittest

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/clickhouse"
	"seagull.sh/seagull/internal/dialect/martian"
	"seagull.sh/seagull/internal/dialect/mysql"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestRenderQueryWritesTheDialectsForms(t *testing.T) {
	t.Parallel()

	pair := query("Find", commandMany, "SELECT id FROM items WHERE kind = {kind} AND id = {id};", scenarioNone, text("kind"), integer("id"))
	repeated := query("Twice", commandMany, "SELECT id FROM items WHERE kind = {kind} OR name = {kind};", scenarioNone, text("kind"))
	sliced := query("Listed", commandMany, "SELECT id FROM items WHERE id IN ({ids});", scenarioNone, integers("ids"))
	sorted := sortableQuery("Sorted", "SELECT id FROM [[DBTX]] WHERE rank > {rank};", integer("rank"))
	testCases := []struct {
		name     string
		engine   dialect.DialectPort
		entry    corpusQuery
		expected string
	}{
		{
			name:   "numbered placeholders are named by directives",
			engine: postgres.New(),
			entry:  pair,
			expected: "-- sgl.query(name: Find, command: many)\n-- $1 as sgl.param(kind)\n-- $2 as sgl.param(id)\n" +
				"SELECT id FROM items WHERE kind = $1 AND id = $2;\n",
		},
		{
			name:   "anonymous placeholders are anchored by number",
			engine: mysql.New(),
			entry:  pair,
			expected: "-- sgl.query(name: Find, command: many)\n-- ?1 as sgl.param(kind)\n-- ?2 as sgl.param(id)\n" +
				"SELECT id FROM items WHERE kind = ? AND id = ?;\n",
		},
		{
			name:     "a parameter written twice takes the named form where the body form is anonymous",
			engine:   mysql.New(),
			entry:    repeated,
			expected: "-- sgl.query(name: Twice, command: many)\nSELECT id FROM items WHERE kind = :kind OR name = :kind;\n",
		},
		{
			name:     "typed named placeholders carry the core type and need no directive",
			engine:   clickhouse.New(),
			entry:    pair,
			expected: "-- sgl.query(name: Find, command: many)\nSELECT id FROM items WHERE kind = {kind:String} AND id = {id:UInt64};\n",
		},
		{
			name:   "a named placeholder with a quality is anchored by itself",
			engine: clickhouse.New(),
			entry:  sliced,
			expected: "-- sgl.query(name: Listed, command: many)\n-- {ids:UInt64} as sgl.param(ids, kind: slice)\n" +
				"SELECT id FROM items WHERE id IN ({ids:UInt64});\n",
		},
		{
			name:   "headers follow the query line and quoted identifiers take the dialect's quotes",
			engine: martian.New(),
			entry:  sorted,
			expected: "-- sgl.query(name: Sorted, command: many)\n-- sgl.sortable(order_by, [id, kind])\n-- @p1 as sgl.param(rank)\n" +
				"SELECT id FROM [DBTX] WHERE rank > @p1;\n",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			syntax := newDialectSyntax(testCase.engine)
			rendered, err := syntax.renderQuery(&testCase.entry, 0)
			require.NoError(t, err)
			require.Equal(t, testCase.expected, rendered)
		})
	}
}

func TestRenderCaseLeavesOutDroppedQueriesAndReversesOnRequest(t *testing.T) {
	t.Parallel()

	syntax := newDialectSyntax(postgres.New())
	definition := corpusCase{name: "pair", files: []corpusFile{
		{name: "both.sql", queries: []corpusQuery{
			query("First", commandMany, "SELECT id FROM items;", scenarioNone),
			query("Second", commandMany, "SELECT kind FROM items;", scenarioNone),
		}},
		{name: "dropped.sql", queries: []corpusQuery{query("Third", commandMany, "SELECT name FROM items;", scenarioNone)}},
	}}

	files, err := syntax.renderCase(definition, map[string]int{"Third": droppedVariant}, true)

	require.NoError(t, err)
	require.Equal(t, map[string]string{"both.sql": "-- sgl.query(name: Second, command: many)\nSELECT kind FROM items;\n\n" +
		"-- sgl.query(name: First, command: many)\nSELECT id FROM items;\n"}, files)
}

func TestCatalogueFoldsNamesAsTheDialectDoes(t *testing.T) {
	t.Parallel()

	tables := []corpusTable{
		{name: "items", columns: []corpusColumn{{name: "id", kind: columnInteger}, {name: "name", kind: columnText, nullable: true}}},
		{name: "DBTX", quoted: true, columns: []corpusColumn{{name: "id", kind: columnInteger}}},
	}
	testCases := []struct {
		name          string
		engine        dialect.DialectPort
		schema        string
		expectedNames []string
		firstColumn   string
	}{
		{name: "upper folding", engine: martian.New(), schema: "DBO", expectedNames: []string{"ITEMS", "DBTX"}, firstColumn: "ID"},
		{name: "lower folding", engine: postgres.New(), schema: "public", expectedNames: []string{"items", "DBTX"}, firstColumn: "id"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			syntax := newDialectSyntax(testCase.engine)
			catalogue := syntax.catalogue(tables)
			schema := catalogue.Schemas[catalogue.Identifiers.Key(testCase.schema)]
			require.NotNil(t, schema)
			require.Equal(t, testCase.schema, catalogue.DefaultSchema)
			for _, name := range testCase.expectedNames {
				table := schema.Tables[catalogue.Identifiers.Key(name)]
				require.NotNil(t, table, "table %s", name)
				require.Equal(t, name, table.Name)
				require.Equal(t, testCase.firstColumn, table.Columns[0].Name)
			}
			items := schema.Tables[catalogue.Identifiers.Key(testCase.expectedNames[0])]
			require.Equal(t, syntax.core.Text, items.Columns[1].SQLType)
			require.True(t, items.Columns[1].Nullable)
		})
	}
}

func TestTypeOfGivesEachColumnKindTheDialectsType(t *testing.T) {
	t.Parallel()

	syntax := newDialectSyntax(postgres.New())
	core := syntax.core
	enum := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryEnum, EngineName: "feeling", Schema: "public", EnumValues: []string{"calm", "lively"}}
	nullableEnum := enum
	nullableEnum.Nullable = true
	enumArray := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, ElementType: &enum}
	testCases := []struct {
		name        string
		kind        columnKind
		expected    sqlmodel.SQLType
		placeholder sqlmodel.SQLType
	}{
		{name: "an integer takes the count type", kind: columnInteger, expected: core.Count, placeholder: core.Count},
		{name: "text takes the text type", kind: columnText, expected: core.Text, placeholder: core.Text},
		{name: "overridden text keeps the text type", kind: columnOverriddenText, expected: core.Text, placeholder: core.Text},
		{name: "an overridden integer keeps the count type", kind: columnOverriddenInteger, expected: core.Count, placeholder: core.Count},
		{name: "a JSON document takes the JSON type", kind: columnJSON, expected: core.JSON, placeholder: core.JSON},
		{name: "the enum takes the schema's enum and a text placeholder", kind: columnEnum, expected: enum, placeholder: core.Text},
		{
			name:        "nullable enums are an array of the enum that admits NULL",
			kind:        columnNullableEnums,
			expected:    sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, ElementType: &nullableEnum},
			placeholder: core.Text,
		},
		{
			name:        "an enum map maps text to the enum",
			kind:        columnEnumMap,
			expected:    sqlmodel.SQLType{Category: sqlmodel.TypeCategoryMap, KeyType: &core.Text, ElementType: &enum},
			placeholder: core.Text,
		},
		{
			name:        "an enum grid is an array of arrays of the enum",
			kind:        columnEnumGrid,
			expected:    sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, ElementType: &enumArray},
			placeholder: core.Text,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, syntax.typeOf(testCase.kind))
			require.Equal(t, testCase.placeholder, syntax.placeholderType(testCase.kind))
		})
	}
}

func TestEnumTypeIsNamedAsTheDialectFoldsItInItsSchema(t *testing.T) {
	t.Parallel()

	withoutSearchPath := newDialectSyntax(postgres.New())
	withoutSearchPath.info.SchemaSearchPath = nil
	testCases := []struct {
		name           string
		syntax         dialectSyntax
		expectedName   string
		expectedSchema string
	}{
		{name: "lower folding", syntax: newDialectSyntax(postgres.New()), expectedName: "feeling", expectedSchema: "public"},
		{name: "upper folding", syntax: newDialectSyntax(martian.New()), expectedName: "FEELING", expectedSchema: "DBO"},
		{name: "no search path leaves the schema unnamed", syntax: withoutSearchPath, expectedName: "feeling", expectedSchema: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			enum := testCase.syntax.enumType()

			require.Equal(t, sqlmodel.TypeCategoryEnum, enum.Category)
			require.Equal(t, testCase.expectedName, enum.EngineName)
			require.Equal(t, testCase.expectedSchema, enum.Schema)
			require.Equal(t, enumValueList, enum.EnumValues)
		})
	}
}

func TestCatalogueDeclaresTheEnumAndOverridesItsTablesHold(t *testing.T) {
	t.Parallel()

	syntax := newDialectSyntax(postgres.New())
	feelings, _ := findCase(caseEnums)
	overrides, _ := findCase(caseOverrides)

	catalogue := syntax.catalogue(slices.Concat(feelings.tables, overrides.tables))

	schema := catalogue.Schemas[catalogue.Identifiers.Key("public")]
	require.NotNil(t, schema)
	enum := schema.Enums[catalogue.Identifiers.Key("feeling")]
	require.NotNil(t, enum)
	require.Equal(t, "feeling", enum.Name)
	require.Equal(t, "public", enum.Schema)
	require.Equal(t, enumValueList, enum.Values)
	mood := schema.Tables[catalogue.Identifiers.Key(tableFeelings)].Columns[1]
	require.Equal(t, syntax.enumType(), mood.SQLType)
	require.Nil(t, mood.GoTypeOverride)
	notes := schema.Tables[catalogue.Identifiers.Key(tableNotes)].Columns
	require.Equal(t, &sqlmodel.GoType{Package: "html/template", Name: "HTML"}, notes[1].GoTypeOverride)
	require.Equal(t, &sqlmodel.GoType{Package: "go/token", Name: "Pos"}, notes[2].GoTypeOverride)
	require.True(t, notes[2].Nullable)
}

func TestNumberedNameNamesAValueByItsPosition(t *testing.T) {
	t.Parallel()

	require.Equal(t, "p0", numberedName("p", 0))
	require.Equal(t, "p12", numberedName("p", 12))
}

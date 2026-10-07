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

package models

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gotypes"
	"seagull.sh/seagull/internal/resolve"
	"seagull.sh/seagull/internal/sqlmodel"
)

func singleTableCatalogue(table *sqlmodel.Table) *sqlmodel.Catalogue {
	return &sqlmodel.Catalogue{
		DefaultSchema: "main",
		Schemas: map[string]*sqlmodel.Schema{
			"main": {Name: "main", Tables: map[string]*sqlmodel.Table{table.Name: table}},
		},
	}
}

func TestEmitModelsLeavesOutHiddenColumns(t *testing.T) {
	t.Parallel()

	rank := sqlmodel.NewColumn("rank", sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}, true)
	rank.Hidden = true
	table := &sqlmodel.Table{
		Name:   "docs",
		Schema: "main",
		Columns: []sqlmodel.Column{
			sqlmodel.NewColumn("title", sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}, false),
			rank,
		},
	}

	files, err := generateModels(t.Context(), "store", singleTableCatalogue(table), textMappings())
	require.NoError(t, err)
	require.Len(t, files, 1)
	source := string(files[0].Content)
	requireValidGo(t, files[0].Content)
	require.Contains(t, source, "Title string")
	require.NotContains(t, source, "Rank")
}

func arrayColumn(name string, element sqlmodel.SQLType, nullable bool) sqlmodel.Column {
	column := sqlmodel.NewColumn(name, element, nullable)
	column.IsArray = true
	column.ArrayDimensions = 1
	return column
}

func TestModelsAndRowsAgreeOnColumnTypes(t *testing.T) {
	t.Parallel()

	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	counter := sqlmodel.NewIntegerType("UInt32", 32, true)
	nullableCounter := counter
	nullableCounter.Nullable = true
	readings := sqlmodel.NewSQLType(sqlmodel.TypeCategoryArray, "Array(Nullable(UInt32))")
	readings.ElementType = &nullableCounter

	table := &sqlmodel.Table{
		Name:   "voyages",
		Schema: "main",
		Columns: []sqlmodel.Column{
			sqlmodel.NewColumn("boat", text, false),
			sqlmodel.NewColumn("note", text, true),
			arrayColumn("ports", text, false),
			arrayColumn("sightings", text, true),
			sqlmodel.NewColumn("readings", readings, false),
		},
	}
	mappings := gotypes.NewTypeMapper(nil).BuildMappingTable(nil)

	scope := resolve.NewRootScopeChain(resolve.ScopeKindQuery, resolve.ScopeRules{
		RowIDAliases: nil,
		RowIDType:    sqlmodel.SQLType{},
		Identifiers:  sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingNone, Insensitive: true},
	})
	scope.AddTable(ast.TableReference{Name: "voyages"}, ast.JoinInner, table)

	for index := range table.Columns {
		column := &table.Columns[index]
		scoped, _, err := scope.ResolveColumn("", column.Name)
		require.NoError(t, err)
		model := mappings.Resolve(column.ValueType(), column.Nullable)
		row := mappings.Resolve(scoped.SQLType, scoped.Nullable)
		require.Equal(t, model, row, "the model and a row disagree on %s", column.Name)
	}

	files, err := generateModels(t.Context(), "store", singleTableCatalogue(table), mappings)
	require.NoError(t, err)
	source := string(files[0].Content)
	requireValidGo(t, files[0].Content)
	require.Contains(t, source, "Sightings []string")
	require.Contains(t, source, "Readings  []*uint32")
}

func TestEmitModelsBuildsStructPerTable(t *testing.T) {
	catalogue := &sqlmodel.Catalogue{
		DefaultSchema: "public",
		Schemas: map[string]*sqlmodel.Schema{
			"public": {
				Name: "public",
				Tables: map[string]*sqlmodel.Table{
					"users": {
						Name:   "users",
						Schema: "public",
						Columns: []sqlmodel.Column{
							{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}},
							{Name: "tags", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}, ArrayDimensions: 1},
						},
					},
				},
			},
			"tenancy": {
				Name: "tenancy",
				Tables: map[string]*sqlmodel.Table{
					"accounts": {
						Name:   "accounts",
						Schema: "tenancy",
						Columns: []sqlmodel.Column{
							{Name: "name", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
						},
					},
				},
			},
		},
	}

	files, err := generateModels(t.Context(), "mypkg", catalogue, textMappings())
	require.NoError(t, err)
	require.Len(t, files, 1)

	modelsFile := files[0]
	require.Equal(t, "models.go", modelsFile.Name)
	source := string(modelsFile.Content)
	requireValidGo(t, modelsFile.Content)

	assert.Contains(t, source, "type Users struct")
	assert.Contains(t, source, "Tags []string")

	assert.Contains(t, source, "type TenancyAccounts struct")
}

func TestEmitModelsReturnsNilForEmptyCatalogue(t *testing.T) {
	files, err := generateModels(t.Context(), "mypkg", &sqlmodel.Catalogue{Schemas: map[string]*sqlmodel.Schema{}}, textMappings())
	require.NoError(t, err)
	assert.Nil(t, files)
}

func TestEmitModelsDeclaresTheNamedTypesOfThePlan(t *testing.T) {
	t.Parallel()

	mood := sqlmodel.NewSQLType(sqlmodel.TypeCategoryEnum, "mood")
	mood.Schema = "main"
	mood.EnumValues = []string{"happy", "in-progress", "in_progress"}
	label := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	label.Nullable = true
	berth := sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "STRUCT(number INTEGER, label VARCHAR)")
	berth.StructFields = []sqlmodel.StructField{
		{Name: "number", SQLType: sqlmodel.NewIntegerType("integer", 32, false)},
		{Name: "label", SQLType: label},
	}
	table := &sqlmodel.Table{
		Name:   "boats",
		Schema: "main",
		Columns: []sqlmodel.Column{
			sqlmodel.NewColumn("mood", mood, false),
			sqlmodel.NewColumn("berth", berth, true),
		},
	}
	catalogue := singleTableCatalogue(table)
	catalogue.Schemas["main"].Enums = map[string]*sqlmodel.Enum{"mood": {Name: "mood", Schema: "main", Values: mood.EnumValues}}
	mappings := gotypes.NewTypeMapper(nil).WithSchema(catalogue).BuildMappingTable(nil)
	request := &emit.Request{Catalogue: catalogue, Types: mappings, Package: "store"}
	naming, problems := emitkit.PlanPackage(request, emitkit.DefaultReservation(&request.Dialect, &request.Style),
		emitkit.NameStyle{NamedTypes: mappings.NamesType, SQLConstantSuffix: "SQL"})
	require.Empty(t, problems)
	request.Naming = naming
	mappings.Namer = emitkit.TypeNamer(request)

	files, err := GenerateWithReferences(t.Context(), "store", catalogue, mappings, &naming, &request.Style, nil)

	require.NoError(t, err)
	require.Len(t, files, 1)
	source := string(files[0].Content)
	requireValidGo(t, files[0].Content)
	require.Contains(t, source, "Mood  Mood ")
	require.Contains(t, source, "Berth *BoatsBerth")
	require.Contains(t, source, "type Mood string")
	require.Contains(t, source, "MoodHappy       Mood = \"happy\"")
	require.Contains(t, source, "MoodInProgress  Mood = \"in-progress\"")
	require.Contains(t, source, "MoodInProgress2 Mood = \"in_progress\"")
	require.Contains(t, source, "func (value Mood) Valid() bool {")
	require.Contains(t, source, "type BoatsBerth struct {")
	require.Contains(t, source, "Label  *string `json:\"label\"`")
}

func TestEmitModelsDeclaresNamedTypesWithoutATable(t *testing.T) {
	t.Parallel()

	status := sqlmodel.NewSQLType(sqlmodel.TypeCategoryEnum, "Enum8('on' = 1, 'off' = 2)")
	status.EnumValues = []string{"on", "off"}
	naming := &emit.Naming{Types: []emit.NamedType{{Name: "ListLightsStatus", Constants: []string{"ListLightsStatusOn", "ListLightsStatusOff"}, SQLType: status}}}

	files, err := GenerateWithReferences(t.Context(), "store", nil, textMappings(), naming, &emit.Style{}, nil)

	require.NoError(t, err)
	require.Len(t, files, 1)
	requireValidGo(t, files[0].Content)
	require.Contains(t, string(files[0].Content), "type ListLightsStatus string")
}

func TestGenerateWithReferencesStopsWithTheCauseOfADoneContext(t *testing.T) {
	t.Parallel()

	cause := errors.New("cancelled by the test")
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(cause)
	table := &sqlmodel.Table{
		Name:    "docs",
		Schema:  "main",
		Columns: []sqlmodel.Column{sqlmodel.NewColumn("title", sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}, false)},
	}

	files, err := generateModels(ctx, "store", singleTableCatalogue(table), textMappings())

	require.ErrorIs(t, err, cause)
	require.Nil(t, files)
}

func documentedCatalogue() *sqlmodel.Catalogue {
	return &sqlmodel.Catalogue{
		DefaultSchema: "public",
		SearchPath:    []string{"public"},
		Schemas: map[string]*sqlmodel.Schema{"public": {
			Name: "public",
			Tables: map[string]*sqlmodel.Table{"boats": {
				Name:    "boats",
				Schema:  "public",
				Comment: "Boats moored in the harbour.",
				Columns: []sqlmodel.Column{
					{Name: "id", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger}, Comment: "The boat's registration."},
					{Name: "name", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}},
					{Name: "secret", SQLType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText}, Comment: "Never shown.", Hidden: true},
				},
			}},
		}},
	}
}

func TestModelsCarryTheCatalogueCommentsOnlyWhenTheStyleAsks(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		wants   []string
		absents []string
		comment bool
	}{
		{
			name:    "doc comments",
			comment: true,
			wants: []string{
				"// Boats moored in the harbour.\ntype Boats struct",
				"\t// The boat's registration.\n\tID   int64",
			},
			absents: []string{"Never shown."},
		},
		{name: "no doc comments", comment: false, absents: []string{"Boats moored in the harbour.", "The boat's registration."}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			catalogue := documentedCatalogue()
			request := &emit.Request{Catalogue: catalogue, Types: textMappings(), Package: "mypkg"}
			request.Style.DocComments = testCase.comment
			naming, problems := emitkit.PlanPackage(request, emitkit.DefaultReservation(&request.Dialect, &request.Style), emitkit.NameStyle{SQLConstantSuffix: "SQL"})
			require.Empty(t, problems)

			files, err := GenerateWithReferences(t.Context(), "mypkg", catalogue, textMappings(), &naming, &request.Style, map[string]struct{}{})
			require.NoError(t, err)
			require.Len(t, files, 1)
			source := string(files[0].Content)
			requireValidGo(t, files[0].Content)
			for _, want := range testCase.wants {
				require.Contains(t, source, want)
			}
			for _, absent := range testCase.absents {
				require.NotContains(t, source, absent)
			}
		})
	}
}

func TestGenerateWithReferencesRefusesAModelOfATableTheCatalogueLacks(t *testing.T) {
	t.Parallel()

	naming := emit.Naming{Models: []emit.ModelNaming{{Schema: "public", Relation: "ghosts", Type: "Ghosts", Fields: nil}}}

	files, err := GenerateWithReferences(t.Context(), "mypkg", documentedCatalogue(), textMappings(), &naming, &emit.Style{}, nil)

	require.Nil(t, files)
	require.EqualError(t, err, "model Ghosts names table public.ghosts, which the catalogue does not hold")
}

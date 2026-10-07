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

package emitkit_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

func catalogueEnum(schema, name string, values ...string) sqlmodel.SQLType {
	enum := sqlmodel.NewSQLType(sqlmodel.TypeCategoryEnum, name)
	enum.Schema = schema
	enum.EnumValues = values
	return enum
}

func inlineEnum(values ...string) sqlmodel.SQLType {
	enum := sqlmodel.NewSQLType(sqlmodel.TypeCategoryEnum, "enum")
	enum.EnumValues = values
	return enum
}

func structOf(fields ...sqlmodel.StructField) sqlmodel.SQLType {
	structType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "STRUCT")
	structType.StructFields = fields
	return structType
}

func listOf(element sqlmodel.SQLType) sqlmodel.SQLType {
	return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, EngineName: "LIST", ElementType: &element}
}

func mapOf(key, value sqlmodel.SQLType) sqlmodel.SQLType {
	return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryMap, EngineName: "MAP", KeyType: &key, ElementType: &value}
}

func typeNames(types []emit.NamedType) []string {
	names := make([]string, len(types))
	for index := range types {
		names[index] = types[index].Name
	}
	return names
}

func enumCatalogue() *sqlmodel.Catalogue {
	catalogue := catalogueOf(tableOf("users",
		sqlmodel.NewColumn("mood", catalogueEnum("public", "mood", "happy", "sad"), false),
		sqlmodel.NewColumn("status", inlineEnum("active", "banned"), true),
	))
	catalogue.Schemas["public"].Enums["mood"] = &sqlmodel.Enum{Name: "mood", Schema: "public", Values: []string{"happy", "sad"}}
	catalogue.Schemas["public"].Enums["progress"] = &sqlmodel.Enum{Name: "progress", Schema: "public", Values: []string{"in-progress", "in_progress", "", "!!", "done", "2nd"}}
	catalogue.Schemas["audit"] = &sqlmodel.Schema{Name: "audit", Enums: map[string]*sqlmodel.Enum{
		"mood": {Name: "mood", Schema: "audit", Values: []string{"tense"}},
	}}
	return catalogue
}

func TestPlanPackageNamesEnums(t *testing.T) {
	t.Parallel()

	statusParameter := sqlmodel.QueryParameter{Name: "status", SQLType: inlineEnum("active", "banned")}
	query := queryOf("ListTickets", sqlmodel.QueryCommandMany, []sqlmodel.QueryParameter{statusParameter},
		[]sqlmodel.OutputColumn{
			{Name: "mood", SQLType: catalogueEnum("public", "mood", "happy", "sad")},
			{Name: "priority", SQLType: inlineEnum("low", "high")},
		})

	naming, problems := emitkit.PlanPackage(requestOf(enumCatalogue(), query), defaultReservation(), emitkit.NameStyle{NamedTypes: namesEnumsAndStructs})

	require.Empty(t, problems)
	require.Equal(t, []string{"AuditMood", "Mood", "Progress", "UsersStatus", "ListTicketsPriority"}, typeNames(naming.Types))
	require.Equal(t, []string{"AuditMoodTense"}, naming.Types[0].Constants)
	require.Equal(t, []string{"MoodHappy", "MoodSad"}, naming.Types[1].Constants)
	require.Equal(t, []string{"ProgressInProgress", "ProgressInProgress2", "ProgressUnnamed", "ProgressUnnamed2", "ProgressDone", "Progress2nd"}, naming.Types[2].Constants)
	require.Equal(t, []string{"UsersStatusActive", "UsersStatusBanned"}, naming.Types[3].Constants)
	require.Equal(t, []string{"ListTicketsPriorityLow", "ListTicketsPriorityHigh"}, naming.Types[4].Constants)
	require.Nil(t, naming.Types[1].Fields)
}

func TestPlanPackageNamesASharedInlineEnumAfterTheFirstTableDeclaringIt(t *testing.T) {
	t.Parallel()

	declared := func(name string, file, statement int, values ...string) *sqlmodel.Table {
		table := tableOf(name, sqlmodel.NewColumn("id", sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "BIGINT"), false),
			sqlmodel.NewColumn("status", inlineEnum(values...), false))
		table.Origin = sqlmodel.MigrationOrigin{Filename: "migration.sql", Index: file, Statement: statement}
		return table
	}
	testCases := []struct {
		name   string
		tables []*sqlmodel.Table
		want   []string
	}{
		{
			name: "tables of one migration in the order it declares them",
			tables: []*sqlmodel.Table{
				declared("users", 0, 0, "active", "banned"), declared("orders", 0, 1, "open", "closed"), declared("posts", 0, 2, "active", "banned"),
			},
			want: []string{"UsersStatus", "OrdersStatus"},
		},
		{
			name: "a table a later migration adds renames nothing",
			tables: []*sqlmodel.Table{
				declared("users", 0, 0, "active", "banned"), declared("archived", 1, 0, "active", "banned"),
			},
			want: []string{"UsersStatus"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			naming, problems := emitkit.PlanPackage(requestOf(catalogueOf(testCase.tables...)), defaultReservation(),
				emitkit.NameStyle{NamedTypes: namesEnumsAndStructs})

			require.Empty(t, problems)
			require.Equal(t, testCase.want, typeNames(naming.Types))
		})
	}
}

func TestPlanPackageNamesNoTypesWithoutThePredicate(t *testing.T) {
	t.Parallel()

	naming, problems := emitkit.PlanPackage(requestOf(enumCatalogue()), defaultReservation(), emitkit.NameStyle{})

	require.Empty(t, problems)
	require.Nil(t, naming.Types)
}

func TestPlanPackageNamesStructs(t *testing.T) {
	t.Parallel()

	position := structOf(
		sqlmodel.StructField{Name: "lat", SQLType: sqlmodel.NewFloatType("DOUBLE", 64)},
		sqlmodel.StructField{Name: "lon", SQLType: sqlmodel.NewFloatType("DOUBLE", 64)},
	)
	nullableHarbour := textType()
	nullableHarbour.Nullable = true
	berth := structOf(
		sqlmodel.StructField{Name: "harbour", SQLType: nullableHarbour},
		sqlmodel.StructField{Name: "position", SQLType: position},
		sqlmodel.StructField{Name: "Harbour", SQLType: textType()},
	)
	stop := structOf(sqlmodel.StructField{Name: "port", SQLType: textType()})
	fee := structOf(sqlmodel.StructField{Name: "amount", SQLType: integerType()})
	summary := structOf(sqlmodel.StructField{Name: "count", SQLType: integerType()})
	catalogue := catalogueOf(tableOf("boats",
		sqlmodel.NewColumn("berth", berth, true),
		sqlmodel.NewColumn("route", listOf(stop), false),
		sqlmodel.NewColumn("fees", mapOf(textType(), fee), false),
	))
	query := queryOf("GetBoat", sqlmodel.QueryCommandOne, nil, []sqlmodel.OutputColumn{
		{Name: "berth", SQLType: berth, Nullable: true},
		{Name: "summary", SQLType: summary},
		{Name: "positions", SQLType: listOf(position)},
	})

	naming, problems := emitkit.PlanPackage(requestOf(catalogue, query), defaultReservation(), emitkit.NameStyle{NamedTypes: namesEnumsAndStructs})

	require.Empty(t, problems)
	require.Equal(t, []string{"BoatsBerth", "BoatsBerthPosition", "BoatsRouteItem", "BoatsFeesValue", "GetBoatSummary"}, typeNames(naming.Types))
	require.Equal(t, []string{"Harbour", "Position", "Harbour2"}, naming.Types[0].Fields)
	require.True(t, naming.Types[0].SQLType.StructFields[0].SQLType.Nullable)
	require.False(t, naming.Types[0].SQLType.Nullable)
	require.Equal(t, []string{"Lat", "Lon"}, naming.Types[1].Fields)
	require.Nil(t, naming.Types[0].Constants)
}

func TestTypeNamerNamesThePlannedTypes(t *testing.T) {
	t.Parallel()

	berth := structOf(
		sqlmodel.StructField{Name: "harbour", SQLType: textType()},
		sqlmodel.StructField{Name: "mood", SQLType: catalogueEnum("public", "mood", "happy", "sad")},
	)
	catalogue := enumCatalogue()
	catalogue.Schemas["public"].Tables["boats"] = tableOf("boats", sqlmodel.NewColumn("berth", berth, false))
	request := requestOf(catalogue)
	naming, problems := emitkit.PlanPackage(request, defaultReservation(), emitkit.NameStyle{NamedTypes: namesEnumsAndStructs})
	require.Empty(t, problems)
	request.Naming = naming

	namer := emitkit.TypeNamer(request)

	nullableBerth := berth
	nullableBerth.Nullable = true
	otherBerth := structOf(sqlmodel.StructField{Name: "harbour", SQLType: integerType()})
	tests := []struct {
		name     string
		sqlType  sqlmodel.SQLType
		expected string
		found    bool
	}{
		{name: "a catalogue enum", sqlType: catalogueEnum("public", "mood"), expected: "Mood", found: true},
		{name: "a catalogue enum in another schema", sqlType: catalogueEnum("audit", "mood"), expected: "AuditMood", found: true},
		{name: "an inline enum by its values", sqlType: inlineEnum("active", "banned"), expected: "UsersStatus", found: true},
		{name: "an inline enum of other values", sqlType: inlineEnum("banned", "active"), expected: "", found: false},
		{name: "a struct by its fields", sqlType: berth, expected: "BoatsBerth", found: true},
		{name: "a nullable struct of the same fields", sqlType: nullableBerth, expected: "BoatsBerth", found: true},
		{name: "a struct whose field types differ", sqlType: otherBerth, expected: "", found: false},
		{name: "a text type", sqlType: textType(), expected: "", found: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			name, found := namer(test.sqlType)

			require.Equal(t, test.found, found)
			require.Equal(t, test.expected, name)
		})
	}
}

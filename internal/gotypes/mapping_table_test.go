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

package gotypes

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestMappingTableOverridesRankAboveStructure(t *testing.T) {
	t.Parallel()

	int8Type := sqlmodel.GoType{Package: "", Name: "int8"}
	table := &MappingTable{Mappings: []Mapping{
		{SQLCategory: sqlmodel.TypeCategoryInteger, SQLName: "Int8", NotNull: int8Type, Nullable: sqlmodel.GoType{Name: "*int8"}},
		{SQLCategory: sqlmodel.TypeCategoryText, SQLName: "", NotNull: sqlmodel.GoType{Package: "example.com/text", Name: "Text"}, Nullable: sqlmodel.GoType{Name: "*string"}},
		{SQLCategory: sqlmodel.TypeCategoryText, SQLName: "", NotNull: sqlmodel.GoType{Package: "example.com/later", Name: "Text"}, Nullable: sqlmodel.GoType{Name: "*string"}},
	}}

	tests := []struct {
		name     string
		sqlType  sqlmodel.SQLType
		nullable bool
		want     sqlmodel.GoType
	}{
		{name: "an engine name override wins over the structure", sqlType: sqlmodel.NewIntegerType("Int8", 64, false), want: int8Type},
		{name: "a case-folded name still matches", sqlType: sqlmodel.NewIntegerType("INT8", 64, false), want: int8Type},
		{name: "a later category override wins", sqlType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), want: sqlmodel.GoType{Package: "example.com/later", Name: "Text"}},
		{name: "an uncovered type maps by structure", sqlType: sqlmodel.NewIntegerType("BIG", 64, false), want: sqlmodel.GoType{Name: "int64"}},
		{name: "an uncovered nullable type maps by structure", sqlType: sqlmodel.NewIntegerType("BIG", 64, false), nullable: true, want: sqlmodel.GoType{Name: "*int64"}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, table.Resolve(testCase.sqlType, testCase.nullable))
		})
	}
}

func TestMappingTablePrefersCaseSensitiveName(t *testing.T) {
	t.Parallel()

	mappings := &MappingTable{Mappings: []Mapping{
		{SQLCategory: sqlmodel.TypeCategoryInteger, SQLName: "int8", NotNull: sqlmodel.GoType{Name: "int64"}, Nullable: sqlmodel.GoType{Name: "*int64"}},
		{SQLCategory: sqlmodel.TypeCategoryInteger, SQLName: "Int8", NotNull: sqlmodel.GoType{Name: "int8"}, Nullable: sqlmodel.GoType{Name: "*int8"}},
	}}

	require.Equal(t, "int64", mappings.Resolve(sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "int8"), false).Name)
	require.Equal(t, "int8", mappings.Resolve(sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "Int8"), false).Name)
}

func TestMappingTableArrayPreservesElementPackage(t *testing.T) {
	t.Parallel()

	mappings := &MappingTable{Mappings: []Mapping{
		{
			SQLCategory: sqlmodel.TypeCategoryText,
			SQLName:     "identifier",
			NotNull:     sqlmodel.GoType{Package: "example.com/ids", Name: "ID"},
			Nullable:    sqlmodel.GoType{Package: "example.com/ids", Name: "*ID"},
		},
	}}
	arrayType := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, ElementType: new(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "identifier"))}

	resolved := mappings.Resolve(arrayType, false)
	require.Equal(t, "[]ID", resolved.Name)
	require.Equal(t, "example.com/ids", resolved.Package)
}

func TestNilMappingTableMapsByStructure(t *testing.T) {
	t.Parallel()

	var empty *MappingTable
	require.Equal(t, sqlmodel.GoType{Name: "string"}, empty.Resolve(sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), false))
	require.Equal(t, sqlmodel.GoType{Name: "any"}, empty.Resolve(sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "mystery"), true))
}

func moodType() sqlmodel.SQLType {
	mood := sqlmodel.NewSQLType(sqlmodel.TypeCategoryEnum, "mood")
	mood.Schema = "public"
	mood.EnumValues = []string{"happy", "sad"}
	return mood
}

func berthType() sqlmodel.SQLType {
	berth := sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "STRUCT")
	berth.StructFields = []sqlmodel.StructField{
		{Name: "harbour", SQLType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "VARCHAR")},
		{Name: "mood", SQLType: moodType()},
	}
	return berth
}

func namerOf(names map[string]string) func(sqlmodel.SQLType) (string, bool) {
	return func(sqlType sqlmodel.SQLType) (string, bool) {
		name, ok := names[sqlType.EngineName]
		return name, ok
	}
}

func TestMappingTableNamesEnumsAndStructsWithItsNamer(t *testing.T) {
	t.Parallel()

	stringSlot := sqlmodel.TypeSlot{NotNull: sqlmodel.GoType{Name: "string"}}
	moodArray := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, ElementType: new(moodType())}
	nullableMood := moodType()
	nullableMood.Nullable = true
	nullableMoodArray := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, ElementType: &nullableMood}
	namer := namerOf(map[string]string{"mood": "Mood", "STRUCT": "BoatsBerth"})

	tests := []struct {
		name     string
		table    *MappingTable
		sqlType  sqlmodel.SQLType
		nullable bool
		want     sqlmodel.GoType
	}{
		{name: "an enum with no namer maps to string", table: &MappingTable{}, sqlType: moodType(), want: sqlmodel.GoType{Name: "string"}},
		{name: "a nullable enum with no namer maps to a string pointer", table: &MappingTable{}, sqlType: moodType(), nullable: true, want: sqlmodel.GoType{Name: "*string"}},
		{name: "a named enum maps to its type", table: &MappingTable{Namer: namer}, sqlType: moodType(), want: sqlmodel.GoType{Name: "Mood"}},
		{name: "a nullable named enum maps to a pointer", table: &MappingTable{Namer: namer}, sqlType: moodType(), nullable: true, want: sqlmodel.GoType{Name: "*Mood"}},
		{name: "an array of a named enum", table: &MappingTable{Namer: namer}, sqlType: moodArray, want: sqlmodel.GoType{Name: "[]Mood"}},
		{name: "an array of a nullable named enum", table: &MappingTable{Namer: namer}, sqlType: nullableMoodArray, want: sqlmodel.GoType{Name: "[]*Mood"}},
		{name: "an enum the namer leaves alone maps to string", table: &MappingTable{Namer: namerOf(nil)}, sqlType: moodType(), want: sqlmodel.GoType{Name: "string"}},
		{name: "a set enum slot wins over the namer", table: &MappingTable{Namer: namer, Slots: sqlmodel.TypeSlots{Enum: stringSlot}}, sqlType: moodType(), nullable: true, want: sqlmodel.GoType{Name: "*string"}},
		{name: "a named struct maps to its type", table: &MappingTable{Namer: namer}, sqlType: berthType(), want: sqlmodel.GoType{Name: "BoatsBerth"}},
		{name: "a nullable named struct maps to a pointer", table: &MappingTable{Namer: namer}, sqlType: berthType(), nullable: true, want: sqlmodel.GoType{Name: "*BoatsBerth"}},
		{
			name:    "an override wins over the namer",
			table:   &MappingTable{Namer: namer, Mappings: []Mapping{{SQLCategory: sqlmodel.TypeCategoryEnum, SQLName: "mood", NotNull: sqlmodel.GoType{Package: "example.com/moods", Name: "Mood"}}}},
			sqlType: moodType(),
			want:    sqlmodel.GoType{Package: "example.com/moods", Name: "Mood"},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, testCase.table.Resolve(testCase.sqlType, testCase.nullable))
		})
	}
}

func TestMappingTableAnonymousStructUsesTheNamerForItsFields(t *testing.T) {
	t.Parallel()

	table := &MappingTable{Namer: namerOf(map[string]string{"mood": "Mood"})}

	resolved := table.Resolve(berthType(), false)

	require.NotNil(t, resolved.Composite)
	require.Len(t, resolved.Composite.Fields, 2)
	require.Equal(t, sqlmodel.GoType{Name: "Mood"}, resolved.Composite.Fields[1].Type)
}

func TestMappingTableNamesType(t *testing.T) {
	t.Parallel()

	positional := berthType()
	positional.StructFields[0].Positional = true
	stringSlot := sqlmodel.TypeSlot{NotNull: sqlmodel.GoType{Name: "string"}}
	moodOverride := []Mapping{{SQLCategory: sqlmodel.TypeCategoryEnum, SQLName: "mood", NotNull: sqlmodel.GoType{Name: "string"}}}
	structOverride := []Mapping{{SQLCategory: sqlmodel.TypeCategoryStruct, NotNull: sqlmodel.GoType{Name: "any"}}}

	tests := []struct {
		name    string
		table   *MappingTable
		sqlType sqlmodel.SQLType
		want    bool
	}{
		{name: "an enum under a zero slot", table: &MappingTable{}, sqlType: moodType(), want: true},
		{name: "an enum under a nil table", table: nil, sqlType: moodType(), want: true},
		{name: "an enum under a set slot", table: &MappingTable{Slots: sqlmodel.TypeSlots{Enum: stringSlot}}, sqlType: moodType(), want: false},
		{name: "an enum an override covers", table: &MappingTable{Mappings: moodOverride}, sqlType: moodType(), want: false},
		{name: "a struct of named fields under a zero slot", table: &MappingTable{}, sqlType: berthType(), want: true},
		{name: "a struct with a positional field", table: &MappingTable{}, sqlType: positional, want: false},
		{name: "a struct under a set slot", table: &MappingTable{Slots: sqlmodel.TypeSlots{Struct: stringSlot}}, sqlType: berthType(), want: false},
		{name: "a struct a category override covers", table: &MappingTable{Mappings: structOverride}, sqlType: berthType(), want: false},
		{name: "a text type", table: &MappingTable{}, sqlType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"), want: false},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, testCase.table.NamesType(testCase.sqlType))
		})
	}
}

func TestMappingTableOverridesWithin(t *testing.T) {
	t.Parallel()

	label := sqlmodel.GoType{Package: "example.com/kinds", Name: "Label"}
	table := &MappingTable{Mappings: []Mapping{
		{SQLCategory: sqlmodel.TypeCategoryText, SQLName: "varchar", NotNull: label, Nullable: sqlmodel.GoType{Package: "example.com/kinds", Name: "*Label"}},
	}}
	varchar := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar")
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	integer := sqlmodel.NewIntegerType("integer", 32, false)

	tests := []struct {
		name     string
		sqlType  sqlmodel.SQLType
		table    *MappingTable
		overrode bool
	}{
		{name: "an overridden type", sqlType: varchar, table: table, overrode: true},
		{name: "a type no override covers", sqlType: text, table: table, overrode: false},
		{name: "an array of an overridden type", sqlType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, EngineName: "varchar[]", ElementType: &varchar}, table: table, overrode: true},
		{name: "an array of a type no override covers", sqlType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, EngineName: "integer[]", ElementType: &integer}, table: table, overrode: false},
		{name: "a map holding an overridden value", sqlType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryMap, EngineName: "Map", KeyType: &text, ElementType: &varchar}, table: table, overrode: true},
		{name: "a struct holding an overridden field", sqlType: sqlmodel.SQLType{Category: sqlmodel.TypeCategoryStruct, EngineName: "Tuple", StructFields: []sqlmodel.StructField{{Name: "a", SQLType: integer}, {Name: "b", SQLType: varchar}}}, table: table, overrode: true},
		{name: "a nil table overrides nothing", sqlType: varchar, table: nil, overrode: false},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.overrode, testCase.table.OverridesWithin(testCase.sqlType))
		})
	}
}

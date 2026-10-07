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
	"bytes"
	"go/ast"
	"go/printer"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/gotypes"
	"seagull.sh/seagull/internal/sqlmodel"
)

var enumType = sqlmodel.SQLType{Category: sqlmodel.TypeCategoryEnum, EngineName: "mood", EnumValues: []string{"calm", "stormy"}}

var textType = sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "String"}

func namedEnums() *gotypes.MappingTable {
	return &gotypes.MappingTable{
		Namer: func(sqlType sqlmodel.SQLType) (string, bool) {
			return "Mood", sqlType.Category == sqlmodel.TypeCategoryEnum
		},
		Slots:    sqlmodel.TypeSlots{},
		Mappings: nil,
	}
}

func namedEnumsAndStructs() *gotypes.MappingTable {
	return &gotypes.MappingTable{
		Namer: func(sqlType sqlmodel.SQLType) (string, bool) {
			switch sqlType.Category {
			case sqlmodel.TypeCategoryEnum:
				return "Mood", true
			case sqlmodel.TypeCategoryStruct:
				return "Pair", true
			default:
				return "", false
			}
		},
		Slots:    sqlmodel.TypeSlots{},
		Mappings: nil,
	}
}

func nullableEnumType() sqlmodel.SQLType {
	element := enumType
	element.Nullable = true
	return element
}

func enumArray(nullableElements bool) sqlmodel.SQLType {
	element := enumType
	element.Nullable = nullableElements
	return arrayOf(element)
}

func arrayOf(element sqlmodel.SQLType) sqlmodel.SQLType {
	return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, ElementType: &element}
}

func mapOf(key, value sqlmodel.SQLType) sqlmodel.SQLType {
	return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryMap, KeyType: &key, ElementType: &value}
}

func pairOf(first sqlmodel.SQLType) sqlmodel.SQLType {
	return sqlmodel.SQLType{Category: sqlmodel.TypeCategoryStruct, StructFields: []sqlmodel.StructField{
		{Name: "first", SQLType: first},
		{Name: "label", SQLType: textType},
	}}
}

func renderExpression(t *testing.T, expression ast.Expr) string {
	t.Helper()
	var buffer bytes.Buffer
	require.NoError(t, printer.Fprint(&buffer, token.NewFileSet(), expression))
	return buffer.String()
}

func TestHeldEnumFindsTheEnumTypeThePackageDeclares(t *testing.T) {
	t.Parallel()

	stringEnums := &gotypes.MappingTable{Namer: nil, Slots: sqlmodel.TypeSlots{}, Mappings: nil}
	foreign := &gotypes.MappingTable{Namer: nil, Slots: sqlmodel.TypeSlots{}, Mappings: []gotypes.Mapping{{
		SQLCategory: sqlmodel.TypeCategoryEnum,
		NotNull:     sqlmodel.GoType{Composite: nil, Package: "example.com/moods", Name: "Mood"},
		Nullable:    sqlmodel.GoType{Composite: nil, Package: "example.com/moods", Name: "*Mood"},
	}}}
	anyMaps := namedEnums()
	anyMaps.Slots.Map = sqlmodel.TypeSlot{NotNull: sqlmodel.GoType{Composite: nil, Package: "", Name: "any"}}
	override := &sqlmodel.GoType{Composite: nil, Package: "", Name: "Feeling"}
	text := sqlmodel.GoType{Composite: nil, Package: "", Name: "string"}
	testCases := []struct {
		resolver sqlmodel.GoTypeResolver
		override *sqlmodel.GoType
		name     string
		want     EnumHolding
		sqlType  sqlmodel.SQLType
		nullable bool
		slice    bool
	}{
		{name: "a NOT NULL enum", sqlType: enumType, resolver: namedEnums(), want: EnumHolding{Name: "Mood", Shape: EnumValue, Pointer: false}},
		{name: "a nullable enum", sqlType: enumType, nullable: true, resolver: namedEnums(), want: EnumHolding{Name: "Mood", Shape: EnumValue, Pointer: true}},
		{name: "a slice parameter of an enum", sqlType: enumType, slice: true, resolver: namedEnums(), want: EnumHolding{Name: "Mood", Shape: EnumSlice, Pointer: false}},
		{name: "an array of an enum", sqlType: enumArray(false), resolver: namedEnums(), want: EnumHolding{Name: "Mood", Shape: EnumSlice, Pointer: false}},
		{name: "a nullable array of an enum", sqlType: enumArray(false), nullable: true, resolver: namedEnums(), want: EnumHolding{Name: "Mood", Shape: EnumSlice, Pointer: false}},
		{name: "an array of nullable enum elements", sqlType: enumArray(true), resolver: namedEnums(), want: EnumHolding{Name: "Mood", Shape: EnumSlice, Pointer: true}},
		{name: "a nullable slice parameter, a slice of pointers", sqlType: enumType, nullable: true, slice: true, resolver: namedEnums(), want: EnumHolding{Name: "Mood", Shape: EnumSlice, Pointer: true}},
		{name: "a map of enum values", sqlType: mapOf(textType, enumType), resolver: namedEnums(), want: EnumHolding{Key: &text, Name: "Mood", Shape: EnumMap, Pointer: false}},
		{name: "a map keyed by an enum", sqlType: mapOf(enumType, textType), resolver: namedEnums(), want: EnumHolding{Name: "Mood", Shape: EnumNestedInMap, Pointer: false}},
		{name: "a map of nullable enum values", sqlType: mapOf(textType, nullableEnumType()), resolver: namedEnums(), want: EnumHolding{Name: "Mood", Shape: EnumNestedInMap, Pointer: false}},
		{name: "a map of enum arrays", sqlType: mapOf(textType, enumArray(false)), resolver: namedEnums(), want: EnumHolding{Name: "Mood", Shape: EnumNestedInMap, Pointer: false}},
		{name: "an array of maps of enum values", sqlType: arrayOf(mapOf(textType, enumType)), resolver: namedEnums(), want: EnumHolding{Name: "Mood", Shape: EnumNestedInMap, Pointer: false}},
		{name: "a slice parameter of a map of enum values", sqlType: mapOf(textType, enumType), slice: true, resolver: namedEnums(), want: EnumHolding{Name: "Mood", Shape: EnumNestedInMap, Pointer: false}},
		{name: "an array of enum arrays", sqlType: arrayOf(enumArray(false)), resolver: namedEnums(), want: EnumHolding{Name: "Mood", Shape: EnumNested, Pointer: false}},
		{name: "a slice parameter of an enum array", sqlType: enumArray(false), slice: true, resolver: namedEnums(), want: EnumHolding{Name: "Mood", Shape: EnumNested, Pointer: false}},
		{name: "an anonymous struct with an enum field", sqlType: pairOf(enumType), resolver: namedEnums(), want: EnumHolding{Name: "Mood", Shape: EnumNested, Pointer: false}},
		{name: "a struct type the package declares with an enum field", sqlType: pairOf(enumType), resolver: namedEnumsAndStructs(), want: EnumHolding{Name: "Mood", Shape: EnumNested, Pointer: false}},
		{name: "a struct with a map of enum values", sqlType: pairOf(mapOf(textType, enumType)), resolver: namedEnums(), want: EnumHolding{Name: "Mood", Shape: EnumNestedInMap, Pointer: false}},
		{name: "a struct of text", sqlType: pairOf(textType), resolver: namedEnumsAndStructs(), want: EnumHolding{}},
		{name: "a map the Map slot types", sqlType: mapOf(textType, enumType), resolver: anyMaps, want: EnumHolding{}},
		{name: "a map of text", sqlType: mapOf(textType, textType), resolver: namedEnums(), want: EnumHolding{}},
		{name: "an enum mapped to Go's own string", sqlType: enumType, resolver: stringEnums, want: EnumHolding{}},
		{name: "an array of an enum mapped to Go's own string", sqlType: enumArray(true), resolver: stringEnums, want: EnumHolding{}},
		{name: "an enum mapped to another package's type", sqlType: enumType, resolver: foreign, want: EnumHolding{}},
		{name: "an enum with a Go type override", sqlType: enumType, override: override, resolver: namedEnums(), want: EnumHolding{}},
		{name: "a text value", sqlType: textType, resolver: namedEnums(), want: EnumHolding{}},
		{name: "no type mapping", sqlType: enumType, resolver: nil, want: EnumHolding{}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, HeldEnum(testCase.sqlType, testCase.nullable, testCase.slice, testCase.override, testCase.resolver))
		})
	}
}

func TestEnumHoldingConvertsToAndFromGoStrings(t *testing.T) {
	t.Parallel()

	request := testRequest()
	request.Style.HelperPrefix = "acme"
	vocabulary := NewVocabulary(request)
	key := sqlmodel.GoType{Composite: nil, Package: "github.com/google/uuid", Name: "UUID"}
	testCases := []struct {
		name        string
		strings     string
		fromStrings string
		stringType  string
		holding     EnumHolding
	}{
		{
			name:        "a value",
			holding:     EnumHolding{Name: "Mood", Shape: EnumValue, Pointer: false},
			strings:     "string(x)",
			fromStrings: "Mood(x)",
			stringType:  "string",
		},
		{
			name:        "a pointer",
			holding:     EnumHolding{Name: "Mood", Shape: EnumValue, Pointer: true},
			strings:     "(*string)(x)",
			fromStrings: "(*Mood)(x)",
			stringType:  "*string",
		},
		{
			name:        "a slice",
			holding:     EnumHolding{Name: "Mood", Shape: EnumSlice, Pointer: false},
			strings:     "acmeConvertStrings[string](x)",
			fromStrings: "acmeConvertStrings[Mood](x)",
			stringType:  "[]string",
		},
		{
			name:        "a slice of pointers",
			holding:     EnumHolding{Name: "Mood", Shape: EnumSlice, Pointer: true},
			strings:     "acmeConvertStringPointers[string](x)",
			fromStrings: "acmeConvertStringPointers[Mood](x)",
			stringType:  "[]*string",
		},
		{
			name:        "a map of values",
			holding:     EnumHolding{Key: &key, Name: "Mood", Shape: EnumMap, Pointer: false},
			strings:     "acmeConvertStringMap[string](x)",
			fromStrings: "acmeConvertStringMap[Mood](x)",
			stringType:  "map[uuid.UUID]string",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			value := goastutil.CachedIdent("x")
			require.Equal(t, testCase.strings, renderExpression(t, testCase.holding.Strings(value, vocabulary)))
			require.Equal(t, testCase.fromStrings, renderExpression(t, testCase.holding.FromStrings(value, vocabulary)))
			require.Equal(t, testCase.stringType, renderExpression(t, testCase.holding.StringType(goast.NewImportTracker())))
		})
	}
}

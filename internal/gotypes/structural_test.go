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

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestStructuralMappingReadsAttributesNotNames(t *testing.T) {
	t.Parallel()

	monetary := sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "CASH")
	monetary.Monetary = true
	tests := []struct {
		name     string
		sqlType  sqlmodel.SQLType
		nullable bool
		want     sqlmodel.GoType
	}{
		{name: "an 8-bit signed integer", sqlType: sqlmodel.NewIntegerType("WHOLE8", 8, false), want: sqlmodel.GoType{Name: "int8"}},
		{name: "a 16-bit signed integer", sqlType: sqlmodel.NewIntegerType("WHOLE16", 16, false), want: sqlmodel.GoType{Name: "int16"}},
		{name: "a 24-bit integer widens to 32 bits", sqlType: sqlmodel.NewIntegerType("WHOLE24", 24, false), want: sqlmodel.GoType{Name: "int32"}},
		{name: "a 32-bit signed integer", sqlType: sqlmodel.NewIntegerType("WHOLE32", 32, false), want: sqlmodel.GoType{Name: "int32"}},
		{name: "a 64-bit signed integer", sqlType: sqlmodel.NewIntegerType("WHOLE64", 64, false), want: sqlmodel.GoType{Name: "int64"}},
		{name: "an unsigned 8-bit integer", sqlType: sqlmodel.NewIntegerType("UWHOLE8", 8, true), want: sqlmodel.GoType{Name: "uint8"}},
		{name: "an unsigned 16-bit integer", sqlType: sqlmodel.NewIntegerType("UWHOLE16", 16, true), want: sqlmodel.GoType{Name: "uint16"}},
		{name: "an unsigned 32-bit integer", sqlType: sqlmodel.NewIntegerType("UWHOLE32", 32, true), want: sqlmodel.GoType{Name: "uint32"}},
		{name: "an unsigned 64-bit integer", sqlType: sqlmodel.NewIntegerType("UWHOLE64", 64, true), nullable: true, want: sqlmodel.GoType{Name: "*uint64"}},
		{name: "a 128-bit integer goes through the BigInt slot", sqlType: sqlmodel.NewIntegerType("WHOLE128", 128, false), want: sqlmodel.GoType{Name: "string"}},
		{name: "an integer of unknown width is lossless", sqlType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "WHOLE"), want: sqlmodel.GoType{Name: "int64"}},
		{name: "a 32-bit float", sqlType: sqlmodel.NewFloatType("REAL32", 32), want: sqlmodel.GoType{Name: "float32"}},
		{name: "a 64-bit float", sqlType: sqlmodel.NewFloatType("REAL64", 64), nullable: true, want: sqlmodel.GoType{Name: "*float64"}},
		{name: "a float of unknown width", sqlType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryFloat, "REAL"), want: sqlmodel.GoType{Name: "float64"}},
		{name: "a decimal goes through the Decimal slot", sqlType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "EXACT"), want: sqlmodel.GoType{Package: goPackageDecimal, Name: "Decimal"}},
		{name: "money is the text the engine writes", sqlType: monetary, nullable: true, want: sqlmodel.GoType{Name: "*string"}},
		{name: "a date", sqlType: sqlmodel.NewTemporalType("DAYSTAMP", sqlmodel.TemporalKindDate, false), want: sqlmodel.GoType{Package: "time", Name: "Time"}},
		{name: "a zoned timestamp", sqlType: sqlmodel.NewTemporalType("MOMENTZ", sqlmodel.TemporalKindTimestamp, true), nullable: true, want: sqlmodel.GoType{Package: "time", Name: "*Time"}},
		{name: "a time of day goes through the TimeOfDay slot", sqlType: sqlmodel.NewTemporalType("CLOCK", sqlmodel.TemporalKindTime, false), want: sqlmodel.GoType{Package: "time", Name: "Time"}},
		{name: "a zoned time of day is text", sqlType: sqlmodel.NewTemporalType("CLOCKZ", sqlmodel.TemporalKindTime, true), nullable: true, want: sqlmodel.GoType{Name: "*string"}},
		{name: "an interval goes through the Interval slot", sqlType: sqlmodel.NewTemporalType("SPAN", sqlmodel.TemporalKindInterval, false), want: sqlmodel.GoType{Name: "string"}},
		{name: "an IP address", sqlType: sqlmodel.NewNetworkType("ADDRESS", sqlmodel.NetworkKindAddress), want: sqlmodel.GoType{Package: "net/netip", Name: "Addr"}},
		{name: "a network prefix has no single Go type", sqlType: sqlmodel.NewNetworkType("NETWORK", sqlmodel.NetworkKindPrefix), want: sqlmodel.GoType{Name: "any"}},
		{name: "a boolean", sqlType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryBoolean, "FLAG"), nullable: true, want: sqlmodel.GoType{Name: "*bool"}},
		{name: "binary is a byte slice even when nullable", sqlType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryBinary, "BLOB"), nullable: true, want: sqlmodel.GoType{Name: "[]byte"}},
		{name: "JSON uses the inline type", sqlType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "DOCUMENT"), nullable: true, want: sqlmodel.GoType{Name: emit.InlineJSONTypeName}},
		{name: "a UUID", sqlType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUUID, "IDENT"), want: sqlmodel.GoType{Package: "github.com/google/uuid", Name: "UUID"}},
		{name: "an enum is a string", sqlType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryEnum, "mood"), want: sqlmodel.GoType{Name: "string"}},
		{name: "an unknown type is any", sqlType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "mystery"), want: sqlmodel.GoType{Name: "any"}},
	}
	table := NewTypeMapper(nil).BuildMappingTable(nil)
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, table.Resolve(testCase.sqlType, testCase.nullable))
		})
	}
}

func TestStructuralMappingRecursesIntoComposites(t *testing.T) {
	t.Parallel()

	table := NewTypeMapper(nil).BuildMappingTable(nil)
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "String")
	counter := sqlmodel.NewIntegerType("UInt32", 32, true)
	address := sqlmodel.NewNetworkType("IPv4", sqlmodel.NetworkKindAddress)

	mapType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryMap, "Map")
	mapType.KeyType = &text
	mapType.ElementType = &counter
	require.Equal(t, sqlmodel.NewGoMap(sqlmodel.GoType{Name: "string"}, sqlmodel.GoType{Name: "uint32"}), table.Resolve(mapType, false))

	nullableCounter := counter
	nullableCounter.Nullable = true
	mapOfNullable := mapType
	mapOfNullable.ElementType = &nullableCounter
	require.Equal(t, sqlmodel.NewGoMap(sqlmodel.GoType{Name: "string"}, sqlmodel.GoType{Name: "*uint32"}), table.Resolve(mapOfNullable, true))

	addressList := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, EngineName: "Array", ElementType: &address}
	require.Equal(t, sqlmodel.GoType{Package: "net/netip", Name: "[]Addr"}, table.Resolve(addressList, false))

	listKey := mapType
	listKey.KeyType = &addressList
	require.Equal(t, sqlmodel.GoType{Name: "any"}, table.Resolve(listKey, false), "a slice cannot key a Go map")

	tuple := sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "Tuple")
	tuple.StructFields = []sqlmodel.StructField{
		{Name: "visit_count", SQLType: counter},
		{Name: "origin", SQLType: address},
	}
	expected := sqlmodel.NewGoStruct([]sqlmodel.GoField{
		{Name: "VisitCount", Tag: `json:"visit_count"`, Type: sqlmodel.GoType{Name: "uint32"}},
		{Name: "Origin", Tag: `json:"origin"`, Type: sqlmodel.GoType{Package: "net/netip", Name: "Addr"}},
	})
	require.Equal(t, expected, table.Resolve(tuple, false))

	pointer := table.Resolve(tuple, true)
	require.Equal(t, "*", pointer.Name)
	require.Equal(t, expected.Composite, pointer.Composite)

	unnamed := tuple
	unnamed.StructFields = []sqlmodel.StructField{{Name: "_1", SQLType: counter, Positional: true}}
	require.Equal(t, sqlmodel.GoType{Name: "[]any"}, table.Resolve(unnamed, false), "a positional tuple holds its fields in order")
	require.Equal(t, sqlmodel.GoType{Name: "[]any"}, table.Resolve(unnamed, true), "a positional tuple is never a pointer")

	unnamedList := sqlmodel.NewSQLType(sqlmodel.TypeCategoryArray, "Array")
	unnamedList.ElementType = &unnamed
	require.Equal(t, sqlmodel.GoType{Name: "[][]any"}, table.Resolve(unnamedList, false), "a list of positional tuples")

	fieldless := tuple
	fieldless.StructFields = nil
	require.Equal(t, sqlmodel.GoType{Name: "any"}, table.Resolve(fieldless, false), "a struct of no fields")

	repeated := tuple
	repeated.StructFields = []sqlmodel.StructField{{Name: "a_b", SQLType: text}, {Name: "a-b", SQLType: text}}
	fields := table.Resolve(repeated, false).Composite.Fields
	require.Equal(t, "AB", fields[0].Name)
	require.Equal(t, "AB2", fields[1].Name)
}

func TestStructuralMappingBoundsDepth(t *testing.T) {
	t.Parallel()

	deep := sqlmodel.NewIntegerType("WHOLE64", 64, false)
	for range sqlmodel.MaxTypeDepth + 4 {
		deep = sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, EngineName: "LIST", ElementType: new(deep)}
	}
	resolved := NewTypeMapper(nil).BuildMappingTable(nil).Resolve(deep, false)
	require.Contains(t, resolved.Name, "any")
}

func TestStructuralMappingKeepsArrayElementNullability(t *testing.T) {
	t.Parallel()

	table := NewTypeMapper(nil).BuildMappingTable(nil)
	counter := sqlmodel.NewIntegerType("UInt32", 32, true)
	nullableCounter := counter
	nullableCounter.Nullable = true

	plain := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, EngineName: "Array", ElementType: &counter}
	require.Equal(t, sqlmodel.GoType{Name: "[]uint32"}, table.Resolve(plain, false))
	require.Equal(t, sqlmodel.GoType{Name: "[]uint32"}, table.Resolve(plain, true), "a NULL array is a nil slice")

	ofNullable := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, EngineName: "Array", ElementType: &nullableCounter}
	require.Equal(t, sqlmodel.GoType{Name: "[]*uint32"}, table.Resolve(ofNullable, false), "a NULL element needs a pointer")

	nested := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, EngineName: "Array", ElementType: &ofNullable}
	require.Equal(t, sqlmodel.GoType{Name: "[][]*uint32"}, table.Resolve(nested, false))
}

func TestStructuralMappingGivesEveryStructFieldADistinctName(t *testing.T) {
	t.Parallel()

	table := NewTypeMapper(nil).BuildMappingTable(nil)
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "String")
	testCases := []struct {
		name   string
		fields []string
		want   []string
	}{
		{
			name:   "a suffixed name that a later field spells already",
			fields: []string{"a_b", "aB", "a_b_2"},
			want:   []string{"AB", "AB2", "AB22"},
		},
		{
			name:   "a field spelling a suffix taken before the repeat",
			fields: []string{"a_b_2", "a_b", "aB"},
			want:   []string{"AB2", "AB", "AB3"},
		},
		{
			name:   "three spellings of one name",
			fields: []string{"a_b", "a-b", "aB"},
			want:   []string{"AB", "AB2", "AB3"},
		},
		{
			name:   "distinct names keep their spelling",
			fields: []string{"visit_count", "origin"},
			want:   []string{"VisitCount", "Origin"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tuple := sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "Tuple")
			for _, name := range testCase.fields {
				tuple.StructFields = append(tuple.StructFields, sqlmodel.StructField{Name: name, SQLType: text})
			}
			resolved := table.Resolve(tuple, false)
			require.NotNil(t, resolved.Composite)
			names := make([]string, 0, len(resolved.Composite.Fields))
			for _, field := range resolved.Composite.Fields {
				names = append(names, field.Name)
			}
			require.Equal(t, testCase.want, names)
		})
	}
}

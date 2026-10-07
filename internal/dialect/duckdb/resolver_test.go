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

package duckdb

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestResolveListValueElementsMayBeNullWhenAValueMayBe(t *testing.T) {
	t.Parallel()

	text := builtinTypeMap["varchar"]
	nullLiteral := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	nullLiteral.Nullable = true

	resolution, err := resolveListValue([]sqlmodel.SQLType{text, nullLiteral})
	require.NoError(t, err)
	require.True(t, resolution.ReturnType.ElementType.Nullable)

	resolution, err = resolveListValue([]sqlmodel.SQLType{text, text})
	require.NoError(t, err)
	require.False(t, resolution.ReturnType.ElementType.Nullable)
}

func TestResolveFunctionCallKnowsTheElementsOfTheListsItCollects(t *testing.T) {
	t.Parallel()

	integer := builtinTypeMap["integer"]
	nullableInteger := integer
	nullableInteger.Nullable = true
	mapType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryMap, "map(varchar, integer)")
	mapType.KeyType = new(builtinTypeMap["varchar"])
	mapType.ElementType = &integer

	testCases := []struct {
		resolve      func([]sqlmodel.SQLType) (*ast.FunctionResolution, error)
		name         string
		arguments    []sqlmodel.SQLType
		wantKnown    bool
		wantNullable bool
	}{
		{name: "list of a value never NULL", resolve: resolveArrayAgg, arguments: []sqlmodel.SQLType{integer}, wantKnown: true, wantNullable: false},
		{name: "list of a value that may be NULL", resolve: resolveArrayAgg, arguments: []sqlmodel.SQLType{nullableInteger}, wantKnown: true, wantNullable: true},
		{name: "the keys of a map", resolve: resolveMapKeys, arguments: []sqlmodel.SQLType{mapType}, wantKnown: true, wantNullable: false},
		{name: "the values of a map", resolve: resolveMapValues, arguments: []sqlmodel.SQLType{mapType}, wantKnown: false, wantNullable: false},
		{name: "a list of values", resolve: resolveListValue, arguments: []sqlmodel.SQLType{integer, integer}, wantKnown: true, wantNullable: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := testCase.resolve(testCase.arguments)
			require.NoError(t, err)
			require.NotNil(t, resolution.ReturnType.ElementType)
			require.Equal(t, testCase.wantKnown, resolution.ElementsKnown)
			require.Equal(t, testCase.wantNullable, resolution.ReturnType.ElementType.Nullable)
		})
	}
}

func TestResolveStructInsertAppendsTheAddedFieldsToTheStruct(t *testing.T) {
	t.Parallel()

	berth := sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "struct")
	berth.StructFields = []sqlmodel.StructField{
		{Name: "pontoon", SQLType: builtinTypeMap["varchar"], Positional: false},
	}
	added := sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "struct")
	added.StructFields = []sqlmodel.StructField{
		{Name: "depth", SQLType: builtinTypeMap["integer"], Positional: false},
		{Name: "crane", SQLType: builtinTypeMap["boolean"], Positional: false},
	}

	testCases := []struct {
		name          string
		argumentTypes []sqlmodel.SQLType
		wantCategory  sqlmodel.SQLTypeCategory
		wantFields    []string
		wantBehaviour sqlmodel.FunctionNullableBehaviour
	}{
		{name: "named fields added to a struct", argumentTypes: []sqlmodel.SQLType{berth, added},
			wantCategory: sqlmodel.TypeCategoryStruct, wantFields: []string{"pontoon", "depth", "crane"},
			wantBehaviour: sqlmodel.FunctionNullableNeverNull},
		{name: "named fields added to a value that is no struct", argumentTypes: []sqlmodel.SQLType{builtinTypeMap["integer"], added},
			wantCategory: sqlmodel.TypeCategoryUnknown, wantFields: nil, wantBehaviour: sqlmodel.FunctionNullableCalledOnNull},
		{name: "a value passed by position", argumentTypes: []sqlmodel.SQLType{berth, builtinTypeMap["integer"]},
			wantCategory: sqlmodel.TypeCategoryStruct, wantFields: []string{"pontoon"},
			wantBehaviour: sqlmodel.FunctionNullableReturnsNullOnNull},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := resolveStructInsert(testCase.argumentTypes)

			require.NoError(t, err)
			require.Equal(t, testCase.wantCategory, resolution.ReturnType.Category)
			require.Equal(t, testCase.wantBehaviour, resolution.NullableBehaviour)
			names := make([]string, 0, len(resolution.ReturnType.StructFields))
			for _, field := range resolution.ReturnType.StructFields {
				names = append(names, field.Name)
			}
			if testCase.wantFields == nil {
				require.Empty(t, names)
				return
			}
			require.Equal(t, testCase.wantFields, names)
		})
	}
}

func TestResolveStructInsertMakesEveryAddedFieldNullableAndLeavesTheStructAsItWas(t *testing.T) {
	t.Parallel()

	berth := sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "struct")
	berth.StructFields = []sqlmodel.StructField{{Name: "pontoon", SQLType: builtinTypeMap["varchar"], Positional: false}}
	added := sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "struct")
	added.StructFields = []sqlmodel.StructField{{Name: "depth", SQLType: builtinTypeMap["integer"], Positional: false}}

	resolution, err := resolveStructInsert([]sqlmodel.SQLType{berth, added})

	require.NoError(t, err)
	require.Len(t, resolution.ReturnType.StructFields, 2)
	require.True(t, resolution.ReturnType.StructFields[1].SQLType.Nullable)
	require.Len(t, berth.StructFields, 1)
	require.False(t, added.StructFields[0].SQLType.Nullable)
}

func TestResolveFunctionCallTypesRoundingAndCommonArgumentsAsDuckDB(t *testing.T) {
	t.Parallel()

	engine := New()
	smallint := engine.NormaliseTypeName("smallint")
	integer := engine.NormaliseTypeName("integer")
	realType := engine.NormaliseTypeName("real")
	double := engine.NormaliseTypeName("double")
	decimal := engine.NormaliseTypeName("decimal(12, 3)")

	testCases := []struct {
		name          string
		function      string
		argumentTypes []sqlmodel.SQLType
		want          sqlmodel.SQLType
	}{
		{name: "ceil of an integer is a double", function: "ceil", argumentTypes: []sqlmodel.SQLType{integer}, want: double},
		{name: "floor of a real stays a real", function: "floor", argumentTypes: []sqlmodel.SQLType{realType}, want: realType},
		{name: "ceiling of a decimal stays a decimal", function: "ceiling", argumentTypes: []sqlmodel.SQLType{decimal}, want: decimal},
		{name: "greatest of two integers is the wider", function: "greatest", argumentTypes: []sqlmodel.SQLType{smallint, integer}, want: integer},
		{name: "greatest of a decimal and a real is the real", function: "greatest", argumentTypes: []sqlmodel.SQLType{decimal, realType}, want: realType},
		{name: "least of an integer and a decimal is the decimal", function: "least", argumentTypes: []sqlmodel.SQLType{integer, decimal}, want: decimal},
		{name: "coalesce of a real and a decimal is the real", function: "coalesce", argumentTypes: []sqlmodel.SQLType{realType, decimal}, want: realType},
		{name: "mod of two smallints stays a smallint", function: "mod", argumentTypes: []sqlmodel.SQLType{smallint, smallint}, want: smallint},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := engine.ResolveFunctionCall(t.Context(), nil, testCase.function, "", testCase.argumentTypes)
			require.NoError(t, err)
			require.NotNil(t, resolution)
			assert.Equal(t, testCase.want.Category, resolution.ReturnType.Category)
			assert.Equal(t, testCase.want.EngineName, resolution.ReturnType.EngineName)
		})
	}
}

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

package postgres

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestResolveFunctionCall(t *testing.T) {
	t.Parallel()

	int2Type := sqlmodel.NewIntegerType("int2", 16, false)
	int4Type := sqlmodel.NewIntegerType("int4", 32, false)
	int8Type := sqlmodel.NewIntegerType("int8", 64, false)
	float4Type := sqlmodel.NewFloatType("float4", 32)
	textType := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText, EngineName: "text"}
	textArrayType := sqlmodel.SQLType{
		Category:    sqlmodel.TypeCategoryArray,
		EngineName:  "text[]",
		ElementType: &textType,
	}

	type testCase struct {
		description         string
		functionName        string
		argumentTypes       []sqlmodel.SQLType
		expectNil           bool
		expectedEngineName  string
		expectedCategory    sqlmodel.SQLTypeCategory
		expectedIsAggregate bool
		expectedReturnsSet  bool
		expectedElementType *sqlmodel.SQLType
	}

	cases := []testCase{
		{
			description:         "array_agg with int4 returns int4 array and is aggregate",
			functionName:        "array_agg",
			argumentTypes:       []sqlmodel.SQLType{int4Type},
			expectedEngineName:  "int4[]",
			expectedCategory:    sqlmodel.TypeCategoryArray,
			expectedIsAggregate: true,
			expectedElementType: &int4Type,
		},
		{
			description:  "array_agg without arguments does not resolve",
			functionName: "array_agg",
			expectNil:    true,
		},
		{
			description:        "unnest with text array returns text element and returns set",
			functionName:       "unnest",
			argumentTypes:      []sqlmodel.SQLType{textArrayType},
			expectedEngineName: "text",
			expectedCategory:   sqlmodel.TypeCategoryText,
			expectedReturnsSet: true,
		},
		{
			description:         "sum with int2 promotes to int8",
			functionName:        "sum",
			argumentTypes:       []sqlmodel.SQLType{int2Type},
			expectedEngineName:  "int8",
			expectedCategory:    sqlmodel.TypeCategoryInteger,
			expectedIsAggregate: true,
		},
		{
			description:         "sum with int4 promotes to int8 (small integer promotion)",
			functionName:        "sum",
			argumentTypes:       []sqlmodel.SQLType{int4Type},
			expectedEngineName:  "int8",
			expectedCategory:    sqlmodel.TypeCategoryInteger,
			expectedIsAggregate: true,
		},
		{
			description:         "sum with int8 promotes to numeric (large integer)",
			functionName:        "sum",
			argumentTypes:       []sqlmodel.SQLType{int8Type},
			expectedEngineName:  "numeric",
			expectedCategory:    sqlmodel.TypeCategoryDecimal,
			expectedIsAggregate: true,
		},
		{
			description:         "sum with float4 promotes to float8",
			functionName:        "sum",
			argumentTypes:       []sqlmodel.SQLType{float4Type},
			expectedEngineName:  "float8",
			expectedCategory:    sqlmodel.TypeCategoryFloat,
			expectedIsAggregate: true,
		},
		{
			description:         "avg with int4 returns numeric",
			functionName:        "avg",
			argumentTypes:       []sqlmodel.SQLType{int4Type},
			expectedEngineName:  "numeric",
			expectedCategory:    sqlmodel.TypeCategoryDecimal,
			expectedIsAggregate: true,
		},
		{
			description:         "avg with float4 returns float8",
			functionName:        "avg",
			argumentTypes:       []sqlmodel.SQLType{float4Type},
			expectedEngineName:  "float8",
			expectedCategory:    sqlmodel.TypeCategoryFloat,
			expectedIsAggregate: true,
		},
		{
			description:         "min with int4 returns int4 (identity aggregate)",
			functionName:        "min",
			argumentTypes:       []sqlmodel.SQLType{int4Type},
			expectedEngineName:  "int4",
			expectedCategory:    sqlmodel.TypeCategoryInteger,
			expectedIsAggregate: true,
		},
		{
			description:         "max with text returns text (identity aggregate)",
			functionName:        "max",
			argumentTypes:       []sqlmodel.SQLType{textType},
			expectedEngineName:  "text",
			expectedCategory:    sqlmodel.TypeCategoryText,
			expectedIsAggregate: true,
		},
		{
			description:   "unknown function returns nil resolution",
			functionName:  "totally_unknown_function",
			argumentTypes: []sqlmodel.SQLType{int4Type},
			expectNil:     true,
		},
		{
			description:        "pg_typeof returns text regardless of argument",
			functionName:       "pg_typeof",
			argumentTypes:      []sqlmodel.SQLType{int4Type},
			expectedEngineName: "text",
			expectedCategory:   sqlmodel.TypeCategoryText,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.description, func(t *testing.T) {
			t.Parallel()

			resolution, err := resolveFunctionCall(nil, testCase.functionName, testCase.argumentTypes)
			require.NoError(t, err)

			if testCase.expectNil {
				assert.Nil(t, resolution, "expected nil resolution for unknown function")
				return
			}

			require.NotNil(t, resolution, "expected non-nil resolution")
			assert.Equal(t, testCase.expectedEngineName, resolution.ReturnType.EngineName, "return type engine name")
			assert.Equal(t, testCase.expectedCategory, resolution.ReturnType.Category, "return type category")
			assert.Equal(t, testCase.expectedIsAggregate, resolution.IsAggregate, "is aggregate")
			assert.Equal(t, testCase.expectedReturnsSet, resolution.ReturnsSet, "returns set")

			if testCase.expectedElementType != nil {
				require.NotNil(t, resolution.ReturnType.ElementType, "expected element type")
				assert.Equal(t, testCase.expectedElementType.EngineName, resolution.ReturnType.ElementType.EngineName)
				assert.Equal(t, testCase.expectedElementType.Category, resolution.ReturnType.ElementType.Category)
			}
		})
	}
}

func TestResolveFunctionCallTypesWindowValueFunctionsFromTheirArgument(t *testing.T) {
	t.Parallel()

	timestampType := sqlmodel.NewTemporalType("timestamptz", sqlmodel.TemporalKindTimestamp, true)
	textType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	integerType := sqlmodel.NewIntegerType("int4", 32, false)

	testCases := []struct {
		functionName  string
		argumentTypes []sqlmodel.SQLType
		want          sqlmodel.SQLType
	}{
		{functionName: "lag", argumentTypes: []sqlmodel.SQLType{timestampType}, want: timestampType},
		{functionName: "lag", argumentTypes: []sqlmodel.SQLType{textType, integerType, textType}, want: textType},
		{functionName: "lead", argumentTypes: []sqlmodel.SQLType{integerType, integerType}, want: integerType},
		{functionName: "first_value", argumentTypes: []sqlmodel.SQLType{textType}, want: textType},
		{functionName: "last_value", argumentTypes: []sqlmodel.SQLType{timestampType}, want: timestampType},
		{functionName: "nth_value", argumentTypes: []sqlmodel.SQLType{textType, integerType}, want: textType},
		{functionName: "nullif", argumentTypes: []sqlmodel.SQLType{textType, textType}, want: textType},
	}

	for _, testCase := range testCases {
		t.Run(testCase.functionName, func(t *testing.T) {
			t.Parallel()

			resolution, err := resolveFunctionCall(nil, testCase.functionName, testCase.argumentTypes)
			require.NoError(t, err)
			require.NotNil(t, resolution)
			assert.Equal(t, testCase.want.Category, resolution.ReturnType.Category)
			assert.Equal(t, testCase.want.EngineName, resolution.ReturnType.EngineName)
			assert.False(t, resolution.IsAggregate)
		})
	}
}

func TestResolveFunctionCallTypesIntervalAndOrderedSetAggregates(t *testing.T) {
	t.Parallel()

	interval := sqlmodel.NewTemporalType("interval", sqlmodel.TemporalKindInterval, false)
	numeric := sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "numeric")
	float := sqlmodel.NewFloatType("float8", 64)
	integer := sqlmodel.NewIntegerType("int4", 32, false)
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	fractions := sqlmodel.SQLType{Category: sqlmodel.TypeCategoryArray, EngineName: "numeric[]", ElementType: &numeric}

	testCases := []struct {
		name          string
		functionName  string
		argumentTypes []sqlmodel.SQLType
		want          string
	}{
		{name: "sum of intervals is an interval", functionName: "sum", argumentTypes: []sqlmodel.SQLType{interval}, want: "interval"},
		{name: "avg of intervals is an interval", functionName: "avg", argumentTypes: []sqlmodel.SQLType{interval}, want: "interval"},
		{name: "percentile_cont over intervals is an interval", functionName: "percentile_cont", argumentTypes: []sqlmodel.SQLType{numeric, interval}, want: "interval"},
		{name: "percentile_cont over integers is a float8", functionName: "percentile_cont", argumentTypes: []sqlmodel.SQLType{float, integer}, want: "float8"},
		{name: "percentile_cont of several fractions is an array", functionName: "percentile_cont", argumentTypes: []sqlmodel.SQLType{fractions, interval}, want: "interval[]"},
		{name: "percentile_disc takes the sorted value's type", functionName: "percentile_disc", argumentTypes: []sqlmodel.SQLType{numeric, text}, want: "text"},
		{name: "percentile_disc of several fractions is an array", functionName: "percentile_disc", argumentTypes: []sqlmodel.SQLType{fractions, integer}, want: "int4[]"},
		{name: "mode takes the sorted value's type", functionName: "mode", argumentTypes: []sqlmodel.SQLType{text}, want: "text"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := resolveFunctionCall(nil, testCase.functionName, testCase.argumentTypes)
			require.NoError(t, err)
			require.NotNil(t, resolution)
			require.Equal(t, testCase.want, resolution.ReturnType.EngineName)
			require.True(t, resolution.IsAggregate)
			require.Equal(t, sqlmodel.FunctionNullableCalledOnNull, resolution.NullableBehaviour)
		})
	}
}

func TestResolveFunctionCallLeavesOrderedSetAggregatesItCannotType(t *testing.T) {
	t.Parallel()

	float := sqlmodel.NewFloatType("float8", 64)

	unknown := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")

	testCases := []struct {
		name         string
		functionName string
		arguments    []sqlmodel.SQLType
	}{
		{name: "percentile_cont without its sorted value", functionName: "percentile_cont", arguments: []sqlmodel.SQLType{float}},
		{name: "percentile_disc without its sorted value", functionName: "percentile_disc", arguments: []sqlmodel.SQLType{float}},
		{name: "mode without its sorted value", functionName: "mode"},
		{name: "percentile_cont of a fraction of unknown type", functionName: "percentile_cont", arguments: []sqlmodel.SQLType{unknown, float}},
		{name: "percentile_disc of a fraction of unknown type", functionName: "percentile_disc", arguments: []sqlmodel.SQLType{unknown, float}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			arguments := testCase.arguments
			functionName := testCase.functionName
			resolution, err := resolveFunctionCall(nil, functionName, arguments)
			require.NoError(t, err)
			require.Nil(t, resolution)
		})
	}
}

func TestResolveFunctionCallTypesTheArrayConstructor(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.NewIntegerType("int4", 32, false)
	bigint := sqlmodel.NewIntegerType("int8", 64, false)
	numeric := sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "numeric")
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	unknown := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")

	testCases := []struct {
		name      string
		arguments []sqlmodel.SQLType
		want      string
		category  sqlmodel.SQLTypeCategory
	}{
		{name: "an array of its elements", arguments: []sqlmodel.SQLType{numeric, numeric}, want: "numeric[]", category: sqlmodel.TypeCategoryArray},
		{name: "elements of unknown type take the known one", arguments: []sqlmodel.SQLType{unknown, text}, want: "text[]", category: sqlmodel.TypeCategoryArray},
		{name: "integers and decimals meet in numeric", arguments: []sqlmodel.SQLType{integer, numeric}, want: "numeric[]", category: sqlmodel.TypeCategoryArray},
		{name: "integers meet in the wider", arguments: []sqlmodel.SQLType{integer, bigint}, want: "int8[]", category: sqlmodel.TypeCategoryArray},
		{name: "no known element leaves it unknown", arguments: []sqlmodel.SQLType{unknown}, category: sqlmodel.TypeCategoryUnknown},
		{name: "an empty array is unknown", category: sqlmodel.TypeCategoryUnknown},
		{name: "elements that meet in no type leave it unknown", arguments: []sqlmodel.SQLType{integer, text}, category: sqlmodel.TypeCategoryUnknown},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := resolveFunctionCall(nil, "array", testCase.arguments)
			require.NoError(t, err)
			require.NotNil(t, resolution)
			require.Equal(t, testCase.category, resolution.ReturnType.Category)
			require.Equal(t, testCase.want, resolution.ReturnType.EngineName)
			require.Equal(t, sqlmodel.FunctionNullableNeverNull, resolution.NullableBehaviour)
		})
	}
}

func TestResolveFunctionCallArrayConstructorElementsMayBeNull(t *testing.T) {
	t.Parallel()

	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	nullLiteral := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	nullLiteral.Nullable = true
	nullableText := text
	nullableText.Nullable = true

	testCases := []struct {
		name      string
		arguments []sqlmodel.SQLType
		nullable  bool
	}{
		{name: "values that are never NULL", arguments: []sqlmodel.SQLType{text, text}, nullable: false},
		{name: "a NULL literal", arguments: []sqlmodel.SQLType{text, nullLiteral}, nullable: true},
		{name: "a value that may be NULL", arguments: []sqlmodel.SQLType{nullableText}, nullable: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := resolveFunctionCall(nil, "array", testCase.arguments)
			require.NoError(t, err)
			require.NotNil(t, resolution.ReturnType.ElementType)
			require.Equal(t, testCase.nullable, resolution.ReturnType.ElementType.Nullable)
		})
	}
}

func TestResolveFunctionCallKnowsTheElementsOfTheArraysItCollects(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.NewIntegerType("int4", 32, false)
	nullableInteger := integer
	nullableInteger.Nullable = true

	testCases := []struct {
		name         string
		function     string
		arguments    []sqlmodel.SQLType
		wantNullable bool
	}{
		{name: "array_agg of a value never NULL", function: "array_agg", arguments: []sqlmodel.SQLType{integer}, wantNullable: false},
		{name: "array_agg of a value that may be NULL", function: "array_agg", arguments: []sqlmodel.SQLType{nullableInteger}, wantNullable: true},
		{name: "an array of values never NULL", function: "array", arguments: []sqlmodel.SQLType{integer, integer}, wantNullable: false},
		{name: "an array holding a value that may be NULL", function: "array", arguments: []sqlmodel.SQLType{integer, nullableInteger}, wantNullable: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := resolveFunctionCall(nil, testCase.function, testCase.arguments)
			require.NoError(t, err)
			require.NotNil(t, resolution)
			require.True(t, resolution.ElementsKnown)
			require.NotNil(t, resolution.ReturnType.ElementType)
			require.Equal(t, testCase.wantNullable, resolution.ReturnType.ElementType.Nullable)
		})
	}
}

func TestResolveFunctionCallLeavesOtherFunctionsToTheCatalogue(t *testing.T) {
	t.Parallel()

	integer := sqlmodel.NewIntegerType("int4", widthInteger, false)
	testCases := []struct {
		name         string
		functionName string
		wantResolved bool
	}{
		{name: "a function it types", functionName: "array_agg", wantResolved: true},
		{name: "a function it types, written in capitals", functionName: "ARRAY_AGG", wantResolved: true},
		{name: "a function the catalogue types", functionName: "length", wantResolved: false},
		{name: "a function nobody knows", functionName: "no_such_function", wantResolved: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := resolveFunctionCall(nil, testCase.functionName, []sqlmodel.SQLType{integer})

			require.NoError(t, err)
			assert.Equal(t, testCase.wantResolved, resolution != nil)
		})
	}
}

func TestEveryTabledFunctionResolvesThroughTheTable(t *testing.T) {
	t.Parallel()

	integerArray := arrayTypeOf(sqlmodel.NewIntegerType("int4", widthInteger, false), 1)
	integer := sqlmodel.NewIntegerType("int4", widthInteger, false)
	argumentLists := []struct {
		name      string
		arguments []sqlmodel.SQLType
	}{
		{name: "no arguments", arguments: nil},
		{name: "an array and an element", arguments: []sqlmodel.SQLType{integerArray, integer}},
		{name: "an element and an array", arguments: []sqlmodel.SQLType{integer, integerArray}},
	}
	for name := range functionResolvers {
		for _, argumentList := range argumentLists {
			t.Run(name+" with "+argumentList.name, func(t *testing.T) {
				t.Parallel()

				require.NotPanics(t, func() {
					_, err := resolveFunctionCall(nil, name, argumentList.arguments)
					require.NoError(t, err)
				})
			})
		}
	}
}

func TestArrayPassthroughFunctionsTypeTheirResultAsTheirArray(t *testing.T) {
	t.Parallel()

	integerArray := arrayTypeOf(sqlmodel.NewIntegerType("int4", widthInteger, false), 1)
	integer := sqlmodel.NewIntegerType("int4", widthInteger, false)
	testCases := []struct {
		name      string
		function  string
		arguments []sqlmodel.SQLType
	}{
		{name: "array_append takes its first argument", function: "array_append", arguments: []sqlmodel.SQLType{integerArray, integer}},
		{name: "array_cat takes its first argument", function: "array_cat", arguments: []sqlmodel.SQLType{integerArray, integerArray}},
		{name: "array_prepend takes its second argument", function: "array_prepend", arguments: []sqlmodel.SQLType{integer, integerArray}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := resolveFunctionCall(nil, testCase.function, testCase.arguments)

			require.NoError(t, err)
			require.NotNil(t, resolution)
			assert.Equal(t, integerArray.Category, resolution.ReturnType.Category)
			assert.Equal(t, integerArray.EngineName, resolution.ReturnType.EngineName)
		})
	}
}

func TestResolveFunctionCallMeetsCommonArgumentsInTheirCommonType(t *testing.T) {
	t.Parallel()

	engine := New()
	smallint := engine.NormaliseTypeName("smallint")
	integer := engine.NormaliseTypeName("integer")
	realType := engine.NormaliseTypeName("real")
	double := engine.NormaliseTypeName("double precision")
	numeric := engine.NormaliseTypeName("numeric")
	timestamp := engine.NormaliseTypeName("timestamptz")
	unknown := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")

	testCases := []struct {
		name          string
		functionName  string
		argumentTypes []sqlmodel.SQLType
		want          string
	}{
		{name: "greatest of two integers is the wider", functionName: "greatest", argumentTypes: []sqlmodel.SQLType{smallint, integer}, want: "int4"},
		{name: "greatest of an integer and a real is the real", functionName: "greatest", argumentTypes: []sqlmodel.SQLType{integer, realType}, want: "float4"},
		{name: "greatest of a decimal and a double is the double", functionName: "greatest", argumentTypes: []sqlmodel.SQLType{numeric, double}, want: "float8"},
		{name: "least of an integer and a decimal is the decimal", functionName: "least", argumentTypes: []sqlmodel.SQLType{integer, numeric}, want: "numeric"},
		{name: "coalesce of a double and a decimal is the double", functionName: "coalesce", argumentTypes: []sqlmodel.SQLType{double, numeric}, want: "float8"},
		{name: "least of timestamps keeps them", functionName: "least", argumentTypes: []sqlmodel.SQLType{timestamp, timestamp}, want: "timestamptz"},
		{name: "a placeholder takes no part", functionName: "greatest", argumentTypes: []sqlmodel.SQLType{unknown, integer}, want: "int4"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := engine.ResolveFunctionCall(t.Context(), nil, testCase.functionName, "", testCase.argumentTypes)
			require.NoError(t, err)
			require.NotNil(t, resolution)
			assert.Equal(t, testCase.want, resolution.ReturnType.EngineName)
			assert.Equal(t, sqlmodel.FunctionNullableCalledOnNull, resolution.NullableBehaviour)
		})
	}
}

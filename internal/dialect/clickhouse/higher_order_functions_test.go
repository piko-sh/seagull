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

package clickhouse

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestHigherOrderFunctionsAreTypedFromTheirLambda(t *testing.T) {
	t.Parallel()

	const nullWhenArray = sqlmodel.FunctionNullableNullWhenArgumentNull
	testCases := []struct {
		name      string
		function  string
		arguments []string
		want      string
		nullable  sqlmodel.FunctionNullableBehaviour
	}{
		{name: "arrayMap returns an array of the lambda's result", function: "arrayMap", arguments: []string{"UInt64", "Array(String)"}, want: "Array(UInt64)", nullable: nullWhenArray},
		{name: "arrayFilter keeps the array", function: "arrayFilter", arguments: []string{"Bool", "Array(String)"}, want: "Array(String)", nullable: nullWhenArray},
		{name: "arraySort by a key keeps the array", function: "arraySort", arguments: []string{"UInt64", "Array(String)"}, want: "Array(String)", nullable: nullWhenArray},
		{name: "arraySort without a key keeps the array", function: "arraySort", arguments: []string{"Array(String)"}, want: "Array(String)", nullable: nullWhenArray},
		{name: "arraySplit returns pieces", function: "arraySplit", arguments: []string{"Bool", "Array(UInt8)"}, want: "Array(Array(UInt8))", nullable: nullWhenArray},
		{name: "arrayFirst returns an element", function: "arrayFirst", arguments: []string{"Bool", "Array(String)"}, want: "String", nullable: nullWhenArray},
		{
			name: "arrayFirstOrNull returns a Nullable element", function: "arrayFirstOrNull", arguments: []string{"Bool", "Array(String)"},
			want: "String", nullable: sqlmodel.FunctionNullableCalledOnNull,
		},
		{name: "arrayMax returns the lambda's result", function: "arrayMax", arguments: []string{"UInt64", "Array(String)"}, want: "UInt64", nullable: nullWhenArray},
		{name: "arrayMin returns an element without a lambda", function: "arrayMin", arguments: []string{"Array(UInt16)"}, want: "UInt16", nullable: nullWhenArray},
		{name: "arraySum sums the lambda's result", function: "arraySum", arguments: []string{"UInt16", "Array(UInt8)"}, want: "UInt64", nullable: nullWhenArray},
		{name: "arraySum sums the elements", function: "arraySum", arguments: []string{"Array(Int8)"}, want: "Int64", nullable: nullWhenArray},
		{name: "arrayCumSum keeps running sums", function: "arrayCumSum", arguments: []string{"UInt16", "Array(UInt8)"}, want: "Array(UInt64)", nullable: nullWhenArray},
		{name: "arrayAvg is a Float64", function: "arrayAvg", arguments: []string{"UInt16", "Array(UInt8)"}, want: "Float64", nullable: nullWhenArray},
		{name: "arrayCount is a UInt32", function: "arrayCount", arguments: []string{"Array(UInt8)"}, want: "UInt32", nullable: nullWhenArray},
		{name: "arrayExists is a UInt8", function: "arrayExists", arguments: []string{"Array(UInt8)"}, want: "UInt8", nullable: nullWhenArray},
		{
			name: "arrayFold returns the accumulator", function: "arrayFold", arguments: []string{"UInt64", "Array(UInt8)", "UInt64"},
			want: "UInt64", nullable: sqlmodel.FunctionNullableNullWhenArgumentNull,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			argumentTypes := make([]sqlmodel.SQLType, len(testCase.arguments))
			for index, argument := range testCase.arguments {
				argumentTypes[index] = normaliseBuiltinTypeName(argument)
			}
			resolution, err := newFunctionResolver().ResolveFunctionCall(t.Context(), nil, testCase.function, "", argumentTypes)
			require.NoError(t, err)
			require.Equal(t, testCase.want, typeSpelling(resolution.ReturnType))
			require.Equal(t, testCase.nullable, resolution.NullableBehaviour)
		})
	}
}

func TestArrayMapWithAnUntypedLambdaHasNoOpinion(t *testing.T) {
	t.Parallel()

	resolution, err := newFunctionResolver().ResolveFunctionCall(t.Context(), nil, "arrayMap", "",
		[]sqlmodel.SQLType{unknownType(), normaliseBuiltinTypeName("Array(String)")})
	require.ErrorIs(t, err, errResolverNoOpinion)
	require.Nil(t, resolution)
}

func TestArrayElementOfNullableElementsMayBeNull(t *testing.T) {
	t.Parallel()

	resolution, err := newFunctionResolver().ResolveFunctionCall(t.Context(), nil, "arrayElement", "",
		[]sqlmodel.SQLType{normaliseBuiltinTypeName("Array(Nullable(String))"), normaliseBuiltinTypeName("UInt8")})
	require.NoError(t, err)
	require.Equal(t, sqlmodel.FunctionNullableCalledOnNull, resolution.NullableBehaviour)
}

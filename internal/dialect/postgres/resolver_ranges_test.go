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

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

func TestResolveFunctionCallTypesRangeBoundsByTheElementType(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		function string
		rangeOf  string
		want     string
	}{
		{name: "the lower bound of an int4range", function: "lower", rangeOf: "int4range", want: "int4"},
		{name: "the upper bound of a tstzrange", function: "upper", rangeOf: "tstzrange", want: "timestamptz"},
		{name: "the lower bound of a daterange", function: "lower", rangeOf: "daterange", want: "date"},
		{name: "the upper bound of a numrange", function: "upper", rangeOf: "numrange", want: "numeric"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := resolveFunctionCall(nil, testCase.function, []sqlmodel.SQLType{builtinTypeMap[testCase.rangeOf]})
			require.NoError(t, err)
			require.NotNil(t, resolution)
			require.Equal(t, testCase.want, resolution.ReturnType.EngineName)
			require.Equal(t, sqlmodel.FunctionNullableCalledOnNull, resolution.NullableBehaviour)
		})
	}
}

func TestResolveFunctionCallLeavesLowerOfTextToTheCatalogue(t *testing.T) {
	t.Parallel()

	resolution, err := resolveFunctionCall(nil, "lower", []sqlmodel.SQLType{builtinTypeMap["text"]})

	require.NoError(t, err)
	require.Nil(t, resolution)
}

func TestResolveFunctionCallAggregatesRangesIntoTheirMultirange(t *testing.T) {
	t.Parallel()

	resolution, err := resolveFunctionCall(nil, "range_agg", []sqlmodel.SQLType{builtinTypeMap["tstzrange"]})

	require.NoError(t, err)
	require.NotNil(t, resolution)
	require.Equal(t, "tstzmultirange", resolution.ReturnType.EngineName)
	require.True(t, resolution.IsAggregate)
}

func TestResolveFunctionCallTypesEnumFunctionsByTheEnumTheirArgumentNames(t *testing.T) {
	t.Parallel()

	catalogue := fakedialect.NewCatalogue("public")
	catalogue.Schemas["public"].Enums["mood"] = &sqlmodel.Enum{Name: "mood", Schema: "public", Values: []string{"sad", "happy"}}
	named := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "mood")

	first, err := resolveFunctionCall(catalogue, "enum_first", []sqlmodel.SQLType{named})
	require.NoError(t, err)
	require.NotNil(t, first)
	require.Equal(t, sqlmodel.TypeCategoryEnum, first.ReturnType.Category)
	require.Equal(t, sqlmodel.FunctionNullableNeverNull, first.NullableBehaviour)

	every, err := resolveFunctionCall(catalogue, "enum_range", []sqlmodel.SQLType{named})
	require.NoError(t, err)
	require.NotNil(t, every)
	require.Equal(t, sqlmodel.TypeCategoryArray, every.ReturnType.Category)
	require.Equal(t, sqlmodel.TypeCategoryEnum, every.ReturnType.ElementType.Category)

	unknown, err := resolveFunctionCall(catalogue, "enum_range", []sqlmodel.SQLType{sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "missing")})
	require.NoError(t, err)
	require.Nil(t, unknown)
}

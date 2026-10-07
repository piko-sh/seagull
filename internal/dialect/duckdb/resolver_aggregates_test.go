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

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestAggregatesTypedByTheirArgumentsFollowDuckDB(t *testing.T) {
	t.Parallel()

	integer := builtinTypeMap["integer"]
	text := builtinTypeMap["varchar"]
	double := builtinTypeMap["double"]
	decimal := builtinTypeMap["decimal"]
	fractions := newArrayType(double)
	unknown := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	testCases := []struct {
		name      string
		function  string
		arguments []sqlmodel.SQLType
		want      sqlmodel.SQLType
		aggregate bool
	}{
		{name: "arg_max gives the argument's type", function: "arg_max", arguments: []sqlmodel.SQLType{text, integer}, want: text, aggregate: true},
		{name: "min_by gives the argument's type", function: "min_by", arguments: []sqlmodel.SQLType{decimal, double}, want: decimal, aggregate: true},
		{name: "arg_max with a count gives a list", function: "arg_max", arguments: []sqlmodel.SQLType{integer, double, builtinTypeMap["bigint"]}, want: newArrayType(integer), aggregate: true},
		{name: "max_by with a count gives a list", function: "max_by", arguments: []sqlmodel.SQLType{text, double, integer}, want: newArrayType(text), aggregate: true},
		{name: "mean of a decimal is a double", function: "mean", arguments: []sqlmodel.SQLType{decimal}, want: double, aggregate: true},
		{name: "approx_quantile keeps the values' type", function: "approx_quantile", arguments: []sqlmodel.SQLType{integer, decimal}, want: integer, aggregate: true},
		{name: "quantile of a list of fractions is a list", function: "quantile", arguments: []sqlmodel.SQLType{integer, fractions}, want: newArrayType(integer), aggregate: true},
		{name: "reservoir_quantile keeps the values' type", function: "reservoir_quantile", arguments: []sqlmodel.SQLType{double, decimal, integer}, want: double, aggregate: true},
		{name: "histogram maps each value to a count", function: "histogram", arguments: []sqlmodel.SQLType{text}, want: newMapType(text, builtinTypeMap["ubigint"]), aggregate: true},
		{name: "approx_top_k lists the values", function: "approx_top_k", arguments: []sqlmodel.SQLType{text, integer}, want: newArrayType(text), aggregate: true},
		{name: "list_max gives an element", function: "list_max", arguments: []sqlmodel.SQLType{newArrayType(text)}, want: text},
		{name: "list_first of an untyped list is unknown", function: "list_first", arguments: []sqlmodel.SQLType{unknown}, want: unknown},
		{name: "list_sum of integers is a hugeint", function: "list_sum", arguments: []sqlmodel.SQLType{newArrayType(integer)}, want: builtinTypeMap["hugeint"]},
		{name: "list_sum of doubles is a double", function: "list_sum", arguments: []sqlmodel.SQLType{newArrayType(double)}, want: double},
		{name: "list_sum of an untyped list is unknown", function: "list_sum", arguments: []sqlmodel.SQLType{unknown}, want: unknown},
		{name: "list_histogram maps each element to a count", function: "list_histogram", arguments: []sqlmodel.SQLType{newArrayType(integer)}, want: newMapType(integer, builtinTypeMap["ubigint"])},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := New().ResolveFunctionCall(t.Context(), nil, testCase.function, "", testCase.arguments)

			require.NoError(t, err)
			require.NotNil(t, resolution)
			require.Equal(t, testCase.want, resolution.ReturnType)
			require.Equal(t, testCase.aggregate, resolution.IsAggregate)
			require.Equal(t, sqlmodel.FunctionNullableCalledOnNull, resolution.NullableBehaviour)
		})
	}
}

func TestAMapKeyIsNeverNull(t *testing.T) {
	t.Parallel()

	key := builtinTypeMap["varchar"]
	key.Nullable = true

	built := newMapType(key, builtinTypeMap["integer"])

	require.False(t, built.KeyType.Nullable)
	require.Equal(t, mapEngineName, built.EngineName)
}

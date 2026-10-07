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

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestMapConstructorsCarryTheirKeyAndValueTypes(t *testing.T) {
	t.Parallel()

	text := builtinTypeMap["varchar"]
	integer := builtinTypeMap["integer"]
	bigint := builtinTypeMap["bigint"]
	double := builtinTypeMap["double"]
	decimal := builtinTypeMap["decimal"]
	nullableText := text
	nullableText.Nullable = true
	unknown := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	nullValue := unknown
	nullValue.Nullable = true
	untyped := sqlmodel.NewSQLType(sqlmodel.TypeCategoryMap, mapEngineName)
	entry := sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "struct")
	entry.StructFields = []sqlmodel.StructField{{Name: "k", SQLType: text}, {Name: "v", SQLType: double}}
	testCases := []struct {
		name      string
		function  string
		arguments []sqlmodel.SQLType
		want      sqlmodel.SQLType
		nullable  sqlmodel.FunctionNullableBehaviour
	}{
		{name: "a literal maps its keys to its values", function: functionMapLiteral, arguments: []sqlmodel.SQLType{text, integer, text, integer}, want: newMapType(text, integer), nullable: sqlmodel.FunctionNullableNeverNull},
		{name: "a literal widens its integers", function: functionMapLiteral, arguments: []sqlmodel.SQLType{text, integer, text, bigint}, want: newMapType(text, bigint), nullable: sqlmodel.FunctionNullableNeverNull},
		{name: "a literal of integers and decimals holds decimals", function: functionMapLiteral, arguments: []sqlmodel.SQLType{integer, decimal, integer, integer}, want: newMapType(integer, decimal), nullable: sqlmodel.FunctionNullableNeverNull},
		{name: "a literal of decimals and doubles holds doubles", function: functionMapLiteral, arguments: []sqlmodel.SQLType{text, decimal, text, double}, want: newMapType(text, double), nullable: sqlmodel.FunctionNullableNeverNull},
		{name: "a literal holding a NULL value has values that may be NULL", function: functionMapLiteral, arguments: []sqlmodel.SQLType{text, nullValue, text, integer}, want: newMapType(text, func() sqlmodel.SQLType { value := integer; value.Nullable = true; return value }()), nullable: sqlmodel.FunctionNullableNeverNull},
		{name: "a literal's key is never NULL", function: functionMapLiteral, arguments: []sqlmodel.SQLType{nullableText, integer}, want: newMapType(text, integer), nullable: sqlmodel.FunctionNullableNeverNull},
		{name: "an empty literal is untyped", function: functionMapLiteral, want: untyped, nullable: sqlmodel.FunctionNullableNeverNull},
		{name: "map of two lists pairs their elements", function: "map", arguments: []sqlmodel.SQLType{newArrayType(text), newArrayType(integer)}, want: newMapType(text, integer), nullable: sqlmodel.FunctionNullableReturnsNullOnNull},
		{name: "map of no lists is untyped", function: "map", want: untyped, nullable: sqlmodel.FunctionNullableReturnsNullOnNull},
		{name: "map_from_entries reads the key and the value from each entry", function: "map_from_entries", arguments: []sqlmodel.SQLType{newArrayType(entry)}, want: newMapType(text, double), nullable: sqlmodel.FunctionNullableReturnsNullOnNull},
		{name: "map_from_entries of untyped entries is untyped", function: "map_from_entries", arguments: []sqlmodel.SQLType{unknown}, want: untyped, nullable: sqlmodel.FunctionNullableReturnsNullOnNull},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := New().ResolveFunctionCall(t.Context(), nil, testCase.function, "", testCase.arguments)

			require.NoError(t, err)
			require.NotNil(t, resolution)
			require.Equal(t, testCase.want, resolution.ReturnType)
			require.Equal(t, testCase.nullable, resolution.NullableBehaviour)
			require.False(t, resolution.IsAggregate)
		})
	}
}

func TestAMapLiteralIsNotTheMapCall(t *testing.T) {
	t.Parallel()

	analysis := analyseQuery(t, buildTestCatalogue(), "SELECT MAP {[1]: [2]} AS literal, map([1], [2]) AS zipped")

	require.Len(t, analysis.OutputColumns, 2)
	names := make([]string, 0, len(analysis.OutputColumns))
	for _, column := range analysis.OutputColumns {
		call, isCall := column.Expression.(*ast.FunctionCallExpression)
		require.Truef(t, isCall, "got %T", column.Expression)
		names = append(names, call.FunctionName)
	}
	require.Equal(t, []string{functionMapLiteral, "map"}, names)
}

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

func TestSubcolumnsAreTypedAsClickHouseKeepsThem(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		container string
		want      map[string]string
		behaviour sqlmodel.FunctionNullableBehaviour
	}{
		{name: "an array", container: "Array(String)", want: map[string]string{"size0": "UInt64"}, behaviour: sqlmodel.FunctionNullableReturnsNullOnNull},
		{
			name: "an array of arrays", container: "Array(Array(UInt8))",
			want: map[string]string{"size0": "UInt64", "size1": "Array(UInt64)"}, behaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
		},
		{
			name: "an array of Nullable values", container: "Array(Nullable(String))",
			want: map[string]string{"size0": "UInt64", "null": "Array(UInt8)"}, behaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
		},
		{
			name: "a map", container: "Map(String, UInt32)",
			want:      map[string]string{"size0": "UInt64", "keys": "Array(String)", "values": "Array(UInt32)"},
			behaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
		},
		{
			name: "a Nested column", container: "Nested(colony String, minutes UInt16)",
			want:      map[string]string{"size0": "UInt64", "colony": "Array(String)", "minutes": "Array(UInt16)"},
			behaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
		},
		{
			name: "a named tuple, read by name or by position", container: "Tuple(a UInt8, b String)",
			want:      map[string]string{"a": "UInt8", "b": "String", "_1": "UInt8", "_2": "String"},
			behaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
		},
		{
			name: "an unnamed tuple", container: "Tuple(UInt8, String)",
			want: map[string]string{"_1": "UInt8", "_2": "String"}, behaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
		},
		{name: "a scalar", container: "String", want: map[string]string{"null": "UInt8"}, behaviour: sqlmodel.FunctionNullableNeverNull},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			subcolumns, behaviour := subcolumnsOf(normaliseBuiltinTypeName(testCase.container))
			require.Equal(t, sqlmodel.TypeCategoryStruct, subcolumns.Category)
			got := make(map[string]string, len(subcolumns.StructFields))
			for _, field := range subcolumns.StructFields {
				got[field.Name] = typeSpelling(field.SQLType)
			}
			require.Equal(t, testCase.want, got)
			require.Equal(t, testCase.behaviour, behaviour)
		})
	}
}

func TestSubcolumnsOfDynamicValuesStayUnknown(t *testing.T) {
	t.Parallel()

	for _, container := range []sqlmodel.SQLType{normaliseBuiltinTypeName("JSON"), unknownType()} {
		subcolumns, _ := subcolumnsOf(container)
		require.Equal(t, container, subcolumns)
	}
}

func typeSpelling(sqlType sqlmodel.SQLType) string {
	if sqlType.ElementType != nil {
		return sqlType.EngineName + "(" + typeSpelling(*sqlType.ElementType) + ")"
	}
	return sqlType.EngineName
}

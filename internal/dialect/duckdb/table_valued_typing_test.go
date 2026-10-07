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

func TestTypedTableValuedFunctionColumnsFollowTheArguments(t *testing.T) {
	t.Parallel()

	engine := New()
	integer := builtinTypeMap["integer"]
	timestamp := builtinTypeMap["timestamp"]
	timestamptz := builtinTypeMap["timestamptz"]
	date := builtinTypeMap["date"]
	interval := builtinTypeMap["interval"]
	unknown := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	list := newArrayType(builtinTypeMap["varchar"])

	testCases := []struct {
		name      string
		function  string
		arguments []sqlmodel.SQLType
		want      string
	}{
		{name: "integers count as BIGINT", function: "generate_series", arguments: []sqlmodel.SQLType{integer, integer}, want: "int8"},
		{name: "range of one integer", function: "range", arguments: []sqlmodel.SQLType{integer}, want: "int8"},
		{name: "timestamps", function: "generate_series", arguments: []sqlmodel.SQLType{timestamp, timestamp, interval}, want: "timestamp"},
		{name: "dates step as timestamps", function: "range", arguments: []sqlmodel.SQLType{date, date, interval}, want: "timestamp"},
		{name: "timestamps with time zones", function: "generate_series", arguments: []sqlmodel.SQLType{timestamptz, timestamptz, interval}, want: "timestamptz"},
		{name: "untyped bounds stepped by an interval", function: "generate_series", arguments: []sqlmodel.SQLType{unknown, unknown, interval}, want: "timestamp"},
		{name: "the elements of a list", function: "unnest", arguments: []sqlmodel.SQLType{list}, want: "varchar"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			columns := engine.TypedTableValuedFunctionColumns(nil, testCase.function, testCase.arguments)
			require.Len(t, columns, 1)
			require.Equal(t, testCase.function, columns[0].Name)
			require.Equal(t, testCase.want, columns[0].SQLType.EngineName)
		})
	}
}

func TestTypedTableValuedFunctionColumnsLeaveWhatTheyCannotType(t *testing.T) {
	t.Parallel()

	unknown := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	engine := New()
	require.Nil(t, engine.TypedTableValuedFunctionColumns(nil, "generate_series", []sqlmodel.SQLType{unknown, unknown}))
	require.Nil(t, engine.TypedTableValuedFunctionColumns(nil, "unnest", []sqlmodel.SQLType{unknown}))
	require.Nil(t, engine.TypedTableValuedFunctionColumns(nil, "read_csv", []sqlmodel.SQLType{unknown}))
}

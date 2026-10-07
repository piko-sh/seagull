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
)

func TestTypedTableValuedFunctionColumnsFollowTheArguments(t *testing.T) {
	t.Parallel()

	engine := New()
	integer := engine.NormaliseTypeName("integer")
	bigint := engine.NormaliseTypeName("bigint")
	numeric := engine.NormaliseTypeName("numeric")
	timestamp := engine.NormaliseTypeName("timestamp")
	timestamptz := engine.NormaliseTypeName("timestamptz")
	date := engine.NormaliseTypeName("date")
	interval := engine.NormaliseTypeName("interval")
	unknown := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	textArray := engine.NormaliseTypeName("text[]")

	testCases := []struct {
		name      string
		function  string
		arguments []sqlmodel.SQLType
		want      string
		column    string
	}{
		{name: "integers", function: "generate_series", arguments: []sqlmodel.SQLType{integer, integer}, want: "int4", column: "generate_series"},
		{name: "an integer and a bigint", function: "generate_series", arguments: []sqlmodel.SQLType{integer, bigint}, want: "int8", column: "generate_series"},
		{name: "decimals", function: "generate_series", arguments: []sqlmodel.SQLType{numeric, integer}, want: "numeric", column: "generate_series"},
		{name: "an integer and a parameter", function: "generate_series", arguments: []sqlmodel.SQLType{integer, unknown}, want: "int4", column: "generate_series"},
		{name: "timestamps", function: "generate_series", arguments: []sqlmodel.SQLType{timestamp, timestamp, interval}, want: "timestamp", column: "generate_series"},
		{name: "timestamps with time zones", function: "generate_series", arguments: []sqlmodel.SQLType{timestamptz, timestamptz, interval}, want: "timestamptz", column: "generate_series"},
		{name: "dates step as timestamps with time zones", function: "generate_series", arguments: []sqlmodel.SQLType{date, date, interval}, want: "timestamptz", column: "generate_series"},
		{name: "a date and an untyped literal", function: "generate_series", arguments: []sqlmodel.SQLType{date, unknown, unknown}, want: "timestamptz", column: "generate_series"},
		{name: "untyped bounds stepped by an interval", function: "generate_series", arguments: []sqlmodel.SQLType{unknown, unknown, interval}, want: "timestamptz", column: "generate_series"},
		{name: "a time zone argument", function: "pg_catalog.generate_series", arguments: []sqlmodel.SQLType{timestamptz, timestamptz, interval, unknown}, want: "timestamptz", column: "generate_series"},
		{name: "the elements of an array", function: "unnest", arguments: []sqlmodel.SQLType{textArray}, want: "text", column: "unnest"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			columns := engine.TypedTableValuedFunctionColumns(nil, testCase.function, testCase.arguments)
			require.Len(t, columns, 1)
			require.Equal(t, testCase.column, columns[0].Name)
			require.Equal(t, testCase.want, columns[0].SQLType.EngineName)
		})
	}
}

func TestTypedTableValuedFunctionColumnsLeaveWhatTheyCannotType(t *testing.T) {
	t.Parallel()

	engine := New()
	unknown := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	textArray := engine.NormaliseTypeName("text[]")

	testCases := []struct {
		name      string
		function  string
		arguments []sqlmodel.SQLType
	}{
		{name: "untyped bounds", function: "generate_series", arguments: []sqlmodel.SQLType{unknown, unknown}},
		{name: "one argument", function: "generate_series", arguments: []sqlmodel.SQLType{unknown}},
		{name: "an unnest of an untyped value", function: "unnest", arguments: []sqlmodel.SQLType{unknown}},
		{name: "an unnest of several arrays", function: "unnest", arguments: []sqlmodel.SQLType{textArray, textArray}},
		{name: "another function", function: "json_each", arguments: []sqlmodel.SQLType{unknown}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Nil(t, engine.TypedTableValuedFunctionColumns(nil, testCase.function, testCase.arguments))
		})
	}
}

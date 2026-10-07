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

package mysql

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestIfNullIsNullOnlyWhenBothArgumentsMayBe(t *testing.T) {
	t.Parallel()

	integer := func(nullable bool) sqlmodel.SQLType {
		sqlType := sqlmodel.NewIntegerType("int", widthInt, false)
		sqlType.Nullable = nullable
		return sqlType
	}
	testCases := []struct {
		name          string
		first, second sqlmodel.SQLType
		wantNullable  bool
	}{
		{name: "a nullable value with a default that is never NULL", first: integer(true), second: integer(false), wantNullable: false},
		{name: "a value that is never NULL", first: integer(false), second: integer(true), wantNullable: false},
		{name: "two nullable values", first: integer(true), second: integer(true), wantNullable: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := newFunctionResolver().ResolveFunctionCall(nil, "IFNULL", "", []sqlmodel.SQLType{testCase.first, testCase.second})
			require.NoError(t, err)
			require.NotNil(t, resolution)
			require.Equal(t, sqlmodel.TypeCategoryInteger, resolution.ReturnType.Category)
			nullable := resolution.NullableBehaviour != sqlmodel.FunctionNullableNeverNull
			require.Equal(t, testCase.wantNullable, nullable)
			require.Equal(t, testCase.wantNullable, resolution.ReturnType.Nullable)
		})
	}
}

func TestDateArithmeticTypesTheValueItShifts(t *testing.T) {
	t.Parallel()

	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar")
	integer := sqlmodel.NewIntegerType("int", widthInt, false)
	date := sqlmodel.NewTemporalType("date", sqlmodel.TemporalKindDate, false)
	datetime := sqlmodel.NewTemporalType("datetime", sqlmodel.TemporalKindTimestamp, false)
	clock := sqlmodel.NewTemporalType("time", sqlmodel.TemporalKindTime, false)
	unit := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	unknown := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	testCases := []struct {
		name          string
		function      string
		arguments     []sqlmodel.SQLType
		wantEngine    string
		wantNullOnAny bool
	}{
		{name: "DATE_ADD of a string gives a string", function: "date_add", arguments: []sqlmodel.SQLType{text, unknown}, wantEngine: "varchar", wantNullOnAny: true},
		{name: "DATE_SUB of a number gives a string", function: "date_sub", arguments: []sqlmodel.SQLType{integer, unknown}, wantEngine: "varchar", wantNullOnAny: true},
		{name: "ADDDATE of a date gives a date and time", function: "adddate", arguments: []sqlmodel.SQLType{date, integer}, wantEngine: "datetime"},
		{name: "SUBDATE of a date and time gives a date and time", function: "subdate", arguments: []sqlmodel.SQLType{datetime, integer}, wantEngine: "datetime"},
		{name: "ADDTIME of a time gives a time", function: "addtime", arguments: []sqlmodel.SQLType{clock, text}, wantEngine: "time"},
		{name: "SUBTIME of a string gives a string", function: "subtime", arguments: []sqlmodel.SQLType{text, text}, wantEngine: "varchar", wantNullOnAny: true},
		{name: "DATE_ADD of a parameter gives a date and time", function: "date_add", arguments: []sqlmodel.SQLType{unknown, unknown}, wantEngine: "datetime"},
		{name: "TIMESTAMPADD shifts its third argument", function: "timestampadd", arguments: []sqlmodel.SQLType{unit, integer, text}, wantEngine: "varchar", wantNullOnAny: true},
		{name: "TIMESTAMPADD of a date and time gives a date and time", function: "timestampadd", arguments: []sqlmodel.SQLType{unit, integer, datetime}, wantEngine: "datetime"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := newFunctionResolver().ResolveFunctionCall(nil, testCase.function, "", testCase.arguments)
			require.NoError(t, err)
			require.NotNil(t, resolution)
			require.Equal(t, testCase.wantEngine, resolution.ReturnType.EngineName)
			require.Equal(t, testCase.wantNullOnAny, resolution.NullableBehaviour == sqlmodel.FunctionNullableCalledOnNull)
		})
	}
}

func TestShiftedDateArgumentNamesTheValueEachDateFunctionShifts(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		function     string
		wantPosition int
		wantShifts   bool
	}{
		{name: "DATE_ADD shifts its first argument", function: "date_add", wantPosition: 0, wantShifts: true},
		{name: "DATE_SUB shifts its first argument", function: "date_sub", wantPosition: 0, wantShifts: true},
		{name: "ADDDATE shifts its first argument", function: "adddate", wantPosition: 0, wantShifts: true},
		{name: "SUBDATE shifts its first argument", function: "subdate", wantPosition: 0, wantShifts: true},
		{name: "ADDTIME shifts its first argument", function: "addtime", wantPosition: 0, wantShifts: true},
		{name: "SUBTIME shifts its first argument", function: "subtime", wantPosition: 0, wantShifts: true},
		{name: "TIMESTAMPADD shifts the value after its unit and interval", function: "timestampadd", wantPosition: 2,
			wantShifts: true},
		{name: "TIMESTAMPDIFF shifts no value", function: "timestampdiff", wantPosition: 0, wantShifts: false},
		{name: "a name in upper case is not looked up", function: "DATE_ADD", wantPosition: 0, wantShifts: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			position, shifts := ShiftedDateArgument(testCase.function)

			require.Equal(t, testCase.wantShifts, shifts)
			require.Equal(t, testCase.wantPosition, position)
		})
	}
}

func TestUnixTimestampIsExactWhereTheValueHasFractionalSeconds(t *testing.T) {
	t.Parallel()

	precise := sqlmodel.NewTemporalType("datetime", sqlmodel.TemporalKindTimestamp, false)
	precise.Precision = new(3)
	testCases := []struct {
		name         string
		argument     sqlmodel.SQLType
		wantCategory sqlmodel.SQLTypeCategory
	}{
		{name: "a date and time in whole seconds", argument: sqlmodel.NewTemporalType("datetime", sqlmodel.TemporalKindTimestamp, false), wantCategory: sqlmodel.TypeCategoryInteger},
		{name: "a date and time with fractional seconds", argument: precise, wantCategory: sqlmodel.TypeCategoryDecimal},
		{name: "a string read as a date and time", argument: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar"), wantCategory: sqlmodel.TypeCategoryDecimal},
		{name: "a parameter", argument: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), wantCategory: sqlmodel.TypeCategoryDecimal},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := newFunctionResolver().ResolveFunctionCall(nil, "unix_timestamp", "", []sqlmodel.SQLType{testCase.argument})
			require.NoError(t, err)
			require.NotNil(t, resolution)
			require.Equal(t, testCase.wantCategory, resolution.ReturnType.Category)
			require.Equal(t, sqlmodel.FunctionNullableCalledOnNull, resolution.NullableBehaviour)
		})
	}
}

func TestExtractMayBeNullOverAStringReadAsADate(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		value        sqlmodel.SQLType
		wantNullable sqlmodel.FunctionNullableBehaviour
	}{
		{name: "a date and time", value: sqlmodel.NewTemporalType("datetime", sqlmodel.TemporalKindTimestamp, false), wantNullable: sqlmodel.FunctionNullableReturnsNullOnNull},
		{name: "a parameter", value: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), wantNullable: sqlmodel.FunctionNullableReturnsNullOnNull},
		{name: "a string", value: sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar"), wantNullable: sqlmodel.FunctionNullableCalledOnNull},
		{name: "a number", value: sqlmodel.NewIntegerType("int", widthInt, false), wantNullable: sqlmodel.FunctionNullableCalledOnNull},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution, err := newFunctionResolver().ResolveFunctionCall(nil, "extract", "", []sqlmodel.SQLType{testCase.value})
			require.NoError(t, err)
			require.NotNil(t, resolution)
			require.Equal(t, "bigint", resolution.ReturnType.EngineName)
			require.Equal(t, testCase.wantNullable, resolution.NullableBehaviour)
		})
	}
}

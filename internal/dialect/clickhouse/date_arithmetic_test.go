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
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func spellTemporal(sqlType sqlmodel.SQLType) string {
	if sqlType.EngineName == engineNameDateTime64 && sqlType.Precision != nil {
		return sqlType.EngineName + "(" + strconv.Itoa(*sqlType.Precision) + ")"
	}
	return sqlType.EngineName
}

func TestMovingADateOrTimeByAnIntervalTypesItAsClickHouseDoes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		left  string
		right string
		want  string
		kind  arithmeticKind
	}{
		{name: "a Date moved by days stays a Date", kind: arithmeticAddition, left: "Date", right: "IntervalDay", want: "Date"},
		{name: "a Date moved by weeks stays a Date", kind: arithmeticAddition, left: "Date", right: "IntervalWeek", want: "Date"},
		{name: "a Date moved by hours becomes a DateTime", kind: arithmeticAddition, left: "Date", right: "IntervalHour", want: "DateTime"},
		{name: "a Date less an hour becomes a DateTime", kind: arithmeticSubtraction, left: "Date", right: "IntervalHour", want: "DateTime"},
		{name: "an interval of days plus a Date", kind: arithmeticAddition, left: "IntervalDay", right: "Date", want: "Date"},
		{name: "an interval of hours plus a Date", kind: arithmeticAddition, left: "IntervalHour", right: "Date", want: "DateTime"},
		{name: "a Date32 moved by days stays a Date32", kind: arithmeticAddition, left: "Date32", right: "IntervalDay", want: "Date32"},
		{name: "a Date32 moved by minutes becomes a DateTime64", kind: arithmeticAddition, left: "Date32", right: "IntervalMinute", want: "DateTime64(3)"},
		{name: "a DateTime moved by days stays a DateTime", kind: arithmeticAddition, left: "DateTime", right: "IntervalDay", want: "DateTime"},
		{name: "a DateTime moved by milliseconds becomes a DateTime64", kind: arithmeticAddition, left: "DateTime", right: "IntervalMillisecond", want: "DateTime64(3)"},
		{name: "a DateTime64 keeps a finer precision", kind: arithmeticAddition, left: "DateTime64(6)", right: "IntervalMillisecond", want: "DateTime64(6)"},
		{name: "a DateTime64 takes a finer interval's precision", kind: arithmeticAddition, left: "DateTime64(3)", right: "IntervalNanosecond", want: "DateTime64(9)"},
		{name: "a DateTime64 moved by microseconds", kind: arithmeticAddition, left: "DateTime64(3)", right: "IntervalMicrosecond", want: "DateTime64(6)"},
		{name: "a DateTime64 moved by days keeps its precision", kind: arithmeticAddition, left: "DateTime64(3)", right: "IntervalDay", want: "DateTime64(3)"},
		{name: "a Date moved by a number of days stays a Date", kind: arithmeticAddition, left: "Date", right: "UInt8", want: "Date"},
		{name: "a Date cannot move by milliseconds", kind: arithmeticAddition, left: "Date", right: "IntervalMillisecond", want: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := arithmeticResult(testCase.kind, normaliseBuiltinTypeName(testCase.left), normaliseBuiltinTypeName(testCase.right))
			require.Equal(t, testCase.want, spellTemporal(result))
		})
	}
}

func TestDateFunctionsTypeTheMovedValueAsClickHouseDoes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		function  string
		arguments []string
		want      string
	}{
		{name: "addDays on a Date", function: "addDays", arguments: []string{"Date", "Int64"}, want: "Date"},
		{name: "addWeeks on a Date", function: "addWeeks", arguments: []string{"Date", "Int64"}, want: "Date"},
		{name: "addYears on a Date", function: "addYears", arguments: []string{"Date", "Int64"}, want: "Date"},
		{name: "addHours on a Date", function: "addHours", arguments: []string{"Date", "Int64"}, want: "DateTime"},
		{name: "addMinutes on a Date", function: "addMinutes", arguments: []string{"Date", "Int64"}, want: "DateTime"},
		{name: "addMonths on a Date32", function: "addMonths", arguments: []string{"Date32", "Int64"}, want: "Date32"},
		{name: "addHours on a Date32", function: "addHours", arguments: []string{"Date32", "Int64"}, want: "DateTime64(3)"},
		{name: "addSeconds on a Date32", function: "addSeconds", arguments: []string{"Date32", "Int64"}, want: "DateTime64(3)"},
		{name: "addDays on a DateTime", function: "addDays", arguments: []string{"DateTime", "Int64"}, want: "DateTime"},
		{name: "addMilliseconds on a DateTime", function: "addMilliseconds", arguments: []string{"DateTime", "Int64"}, want: "DateTime64(3)"},
		{name: "addMicroseconds on a DateTime", function: "addMicroseconds", arguments: []string{"DateTime", "Int64"}, want: "DateTime64(6)"},
		{name: "addNanoseconds on a DateTime64", function: "addNanoseconds", arguments: []string{"DateTime64(3)", "Int64"}, want: "DateTime64(9)"},
		{name: "subtractDays on a Date", function: "subtractDays", arguments: []string{"Date", "Int64"}, want: "Date"},
		{name: "subtractHours on a Date", function: "subtractHours", arguments: []string{"Date", "Int64"}, want: "DateTime"},
		{name: "subtractMilliseconds on a DateTime64", function: "subtractMilliseconds", arguments: []string{"DateTime64(6)", "Int64"}, want: "DateTime64(6)"},
		{name: "date_add of days to a Date", function: "date_add", arguments: []string{"IntervalDay", "Int64", "Date"}, want: "Date"},
		{name: "DATE_ADD of hours to a Date", function: "DATE_ADD", arguments: []string{"IntervalHour", "Int64", "Date"}, want: "DateTime"},
		{name: "dateAdd of days to a Date", function: "dateAdd", arguments: []string{"IntervalDay", "Int64", "Date"}, want: "Date"},
		{name: "date_sub of days from a Date", function: "date_sub", arguments: []string{"IntervalDay", "Int64", "Date"}, want: "Date"},
		{name: "dateSub of hours from a Date", function: "dateSub", arguments: []string{"IntervalHour", "Int64", "Date"}, want: "DateTime"},
		{name: "timestamp_add of days to a Date", function: "timestamp_add", arguments: []string{"IntervalDay", "Int64", "Date"}, want: "Date"},
		{name: "timestampSub of hours from a Date", function: "timestampSub", arguments: []string{"IntervalHour", "Int64", "Date"}, want: "DateTime"},
		{name: "date_add of hours to a Date32", function: "date_add", arguments: []string{"IntervalHour", "Int64", "Date32"}, want: "DateTime64(3)"},
		{name: "date_add of months to a DateTime64", function: "date_add", arguments: []string{"IntervalMonth", "Int64", "DateTime64(3)"}, want: "DateTime64(3)"},
		{name: "date_add of nanoseconds to a DateTime", function: "date_add", arguments: []string{"IntervalNanosecond", "Int64", "DateTime"}, want: "DateTime64(9)"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			arguments := make([]sqlmodel.SQLType, len(testCase.arguments))
			for index, name := range testCase.arguments {
				arguments[index] = normaliseBuiltinTypeName(name)
			}
			resolution, err := New().ResolveFunctionCall(t.Context(), nil, testCase.function, "", arguments)
			require.NoError(t, err)
			require.NotNil(t, resolution)
			require.Equal(t, testCase.want, spellTemporal(resolution.ReturnType))
			require.Equal(t, sqlmodel.FunctionNullableReturnsNullOnNull, resolution.NullableBehaviour)
		})
	}
}

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

func TestBinaryResultTypesTemporalArithmetic(t *testing.T) {
	t.Parallel()

	timestamp := builtinTypeMap["timestamp"]
	timestamptz := builtinTypeMap["timestamptz"]
	date := builtinTypeMap["date"]
	timeOfDay := builtinTypeMap["time"]
	interval := builtinTypeMap["interval"]
	integer := builtinTypeMap["integer"]
	double := builtinTypeMap["double"]

	testCases := []struct {
		name     string
		operator string
		left     sqlmodel.SQLType
		right    sqlmodel.SQLType
		expected string
	}{
		{name: "timestamp minus timestamp is an interval", operator: "-", left: timestamp, right: timestamp, expected: "interval"},
		{name: "timestamptz minus timestamptz is an interval", operator: "-", left: timestamptz, right: timestamptz, expected: "interval"},
		{name: "timestamptz minus timestamp is an interval", operator: "-", left: timestamptz, right: timestamp, expected: "interval"},
		{name: "date minus timestamp is an interval", operator: "-", left: date, right: timestamp, expected: "interval"},
		{name: "date minus date is a BIGINT number of days", operator: "-", left: date, right: date, expected: "int8"},
		{name: "date plus an integer is a date", operator: "+", left: date, right: integer, expected: "date"},
		{name: "an integer plus a date is a date", operator: "+", left: integer, right: date, expected: "date"},
		{name: "date minus an integer is a date", operator: "-", left: date, right: integer, expected: "date"},
		{name: "date plus an interval is a timestamp", operator: "+", left: date, right: interval, expected: "timestamp"},
		{name: "date minus an interval is a timestamp", operator: "-", left: date, right: interval, expected: "timestamp"},
		{name: "date plus a time is a timestamp", operator: "+", left: date, right: timeOfDay, expected: "timestamp"},
		{name: "timestamp minus an interval keeps the timestamp", operator: "-", left: timestamp, right: interval, expected: "timestamp"},
		{name: "an interval plus a timestamptz is a timestamptz", operator: "+", left: interval, right: timestamptz, expected: "timestamptz"},
		{name: "time plus an interval is a time", operator: "+", left: timeOfDay, right: interval, expected: "time"},
		{name: "an interval times a number is an interval", operator: "*", left: interval, right: double, expected: "interval"},
		{name: "an interval divided by a number is an interval", operator: "/", left: interval, right: integer, expected: "interval"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, behaviour, typed := binaryResult(testCase.operator, testCase.left, testCase.right)
			require.True(t, typed)
			require.Equal(t, testCase.expected, result.EngineName)
			require.Equal(t, sqlmodel.FunctionNullableReturnsNullOnNull, behaviour)
		})
	}
}

func TestBinaryResultLeavesTimeMinusTimeToTheEngine(t *testing.T) {
	t.Parallel()

	timeOfDay := builtinTypeMap["time"]
	_, _, typed := binaryResult("-", timeOfDay, timeOfDay)
	require.False(t, typed)
}

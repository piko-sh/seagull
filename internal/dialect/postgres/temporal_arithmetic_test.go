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

func TestBinaryResultTypesTemporalArithmetic(t *testing.T) {
	t.Parallel()

	engine := New()
	timestamp := engine.NormaliseTypeName("timestamp")
	timestamptz := engine.NormaliseTypeName("timestamptz")
	date := engine.NormaliseTypeName("date")
	timeOfDay := engine.NormaliseTypeName("time")
	interval := engine.NormaliseTypeName("interval")
	integer := engine.NormaliseTypeName("integer")
	float := engine.NormaliseTypeName("float8")
	numeric := engine.NormaliseTypeName("numeric")

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
		{name: "timestamp minus date is an interval", operator: "-", left: timestamp, right: date, expected: "interval"},
		{name: "date minus timestamptz is an interval", operator: "-", left: date, right: timestamptz, expected: "interval"},
		{name: "date minus date is a number of days", operator: "-", left: date, right: date, expected: "int4"},
		{name: "time minus time is an interval", operator: "-", left: timeOfDay, right: timeOfDay, expected: "interval"},
		{name: "date plus an integer is a date", operator: "+", left: date, right: integer, expected: "date"},
		{name: "an integer plus a date is a date", operator: "+", left: integer, right: date, expected: "date"},
		{name: "date minus an integer is a date", operator: "-", left: date, right: integer, expected: "date"},
		{name: "date plus an interval is a timestamp", operator: "+", left: date, right: interval, expected: "timestamp"},
		{name: "an interval plus a date is a timestamp", operator: "+", left: interval, right: date, expected: "timestamp"},
		{name: "date minus an interval is a timestamp", operator: "-", left: date, right: interval, expected: "timestamp"},
		{name: "date plus a time is a timestamp", operator: "+", left: date, right: timeOfDay, expected: "timestamp"},
		{name: "timestamp plus an interval keeps the timestamp", operator: "+", left: timestamp, right: interval, expected: "timestamp"},
		{name: "timestamptz minus an interval keeps the timestamptz", operator: "-", left: timestamptz, right: interval, expected: "timestamptz"},
		{name: "an interval plus a timestamptz is a timestamptz", operator: "+", left: interval, right: timestamptz, expected: "timestamptz"},
		{name: "time plus an interval is a time", operator: "+", left: timeOfDay, right: interval, expected: "time"},
		{name: "an interval times a number is an interval", operator: "*", left: interval, right: float, expected: "interval"},
		{name: "a number times an interval is an interval", operator: "*", left: integer, right: interval, expected: "interval"},
		{name: "an interval divided by a number is an interval", operator: "/", left: interval, right: numeric, expected: "interval"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, behaviour, typed := engine.BinaryResult(testCase.operator, testCase.left, testCase.right)
			require.True(t, typed)
			require.Equal(t, testCase.expected, result.EngineName)
			require.Equal(t, sqlmodel.FunctionNullableReturnsNullOnNull, behaviour)
		})
	}
}

func TestBinaryResultLeavesOtherTemporalOperandsToTheDefault(t *testing.T) {
	t.Parallel()

	engine := New()
	interval := engine.NormaliseTypeName("interval")
	timestamp := engine.NormaliseTypeName("timestamp")
	integer := engine.NormaliseTypeName("integer")

	testCases := []struct {
		name     string
		operator string
		left     sqlmodel.SQLType
		right    sqlmodel.SQLType
	}{
		{name: "interval plus interval", operator: "+", left: interval, right: interval},
		{name: "integer minus integer", operator: "-", left: integer, right: integer},
		{name: "timestamp times an integer", operator: "*", left: timestamp, right: integer},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, _, typed := engine.BinaryResult(testCase.operator, testCase.left, testCase.right)
			require.False(t, typed)
		})
	}
}

func TestCockroachDBStyleDateDifferenceFollowsTheIntegerType(t *testing.T) {
	t.Parallel()

	engine := New(WithExtraTypes(map[string]sqlmodel.SQLType{"integer": sqlmodel.NewIntegerType("int8", 64, false)}))
	date := engine.NormaliseTypeName("date")

	result, _, typed := engine.BinaryResult("-", date, date)
	require.True(t, typed)
	require.Equal(t, "int8", result.EngineName)
}

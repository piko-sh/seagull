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

func TestStartOfIntervalTypesTheBucketByTheIntervalsUnit(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		value    sqlmodel.SQLType
		interval string
		want     sqlmodel.SQLType
	}{
		{name: "minutes of a DateTime", value: primitiveType("DateTime"), interval: "IntervalMinute", want: primitiveType("DateTime")},
		{name: "days of a Date", value: primitiveType("Date"), interval: "IntervalDay", want: primitiveType("DateTime")},
		{name: "months of a Date", value: primitiveType("Date"), interval: "IntervalMonth", want: primitiveType("Date")},
		{name: "a year of a DateTime64", value: dateTime64Type(3), interval: "IntervalYear", want: primitiveType("Date")},
		{name: "seconds of a DateTime64", value: dateTime64Type(3), interval: "IntervalSecond", want: primitiveType("DateTime")},
		{name: "milliseconds of a DateTime64", value: dateTime64Type(6), interval: "IntervalMillisecond", want: dateTime64Type(6)},
		{name: "microseconds of a DateTime64", value: dateTime64Type(3), interval: "IntervalMicrosecond", want: dateTime64Type(6)},
		{name: "milliseconds of a DateTime", value: primitiveType("DateTime"), interval: "IntervalMillisecond", want: dateTime64Type(3)},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution := resolveStartOfInterval([]sqlmodel.SQLType{testCase.value, primitiveType(testCase.interval)})

			require.NotNil(t, resolution)
			require.Equal(t, testCase.want, resolution.ReturnType)
			require.Equal(t, sqlmodel.FunctionNullableReturnsNullOnNull, resolution.NullableBehaviour)
		})
	}
}

func TestStartOfIntervalLeavesAStringIntervalToTheCatalogue(t *testing.T) {
	t.Parallel()

	require.Nil(t, resolveStartOfInterval([]sqlmodel.SQLType{primitiveType("DateTime"), primitiveType("String")}))
	require.Nil(t, resolveStartOfInterval([]sqlmodel.SQLType{primitiveType("DateTime")}))
}

func TestTransformTypesByItsDefaultOrItsValue(t *testing.T) {
	t.Parallel()

	stringType := primitiveType("String")
	testCases := []struct {
		name      string
		arguments []sqlmodel.SQLType
		want      sqlmodel.SQLType
	}{
		{
			name:      "with a default, the replacements and the default",
			arguments: []sqlmodel.SQLType{stringType, normaliseBuiltinTypeName("Array(String)"), normaliseBuiltinTypeName("Array(String)"), stringType},
			want:      stringType,
		},
		{
			name:      "without a default, the value",
			arguments: []sqlmodel.SQLType{primitiveType("Int64"), normaliseBuiltinTypeName("Array(UInt8)"), normaliseBuiltinTypeName("Array(UInt8)")},
			want:      primitiveType("Int64"),
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolution := resolveTransform(testCase.arguments)

			require.NotNil(t, resolution)
			require.Equal(t, testCase.want, resolution.ReturnType)
		})
	}
}

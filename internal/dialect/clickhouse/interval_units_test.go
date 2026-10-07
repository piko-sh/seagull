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

	"seagull.sh/seagull/internal/dialect/ast"
)

func TestBareIntervalUnitArgumentIsAUnitNotAColumn(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		source   string
		wantType string
	}{
		{name: "a unit in date_diff", source: "date_diff(day, start, finish)", wantType: engineNameString},
		{name: "a unit in dateDiff, in upper case", source: "dateDiff(HOUR, start, finish)", wantType: engineNameString},
		{name: "an SQL_TSI_ unit in timestampdiff", source: "timestampdiff(SQL_TSI_MINUTE, start, finish)", wantType: engineNameString},
		{name: "a plural unit in date_add", source: "date_add(DAYS, 1, start)", wantType: "IntervalDay"},
		{name: "an abbreviated unit in timestamp_sub", source: "timestamp_sub(hh, 1, start)", wantType: "IntervalHour"},
		{name: "an SQL_TSI_ unit in dateAdd", source: "dateAdd(SQL_TSI_MONTH, 1, start)", wantType: "IntervalMonth"},
		{name: "a short unit in date_sub", source: "date_sub(ms, 1, start)", wantType: "IntervalMillisecond"},
		{name: "a column that names no unit", source: "date_diff(unit, start, finish)", wantType: ""},
		{name: "a qualified column", source: "date_diff(t.day, start, finish)", wantType: ""},
		{name: "a function that takes no unit word", source: "age(hour, start, finish)", wantType: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			call, isCall := parseExpressionFromSource(t, testCase.source).(*ast.FunctionCallExpression)

			require.True(t, isCall)
			literal, isLiteral := call.Arguments[0].(*ast.LiteralExpression)
			require.Equal(t, testCase.wantType != "", isLiteral)
			if isLiteral {
				require.Equal(t, testCase.wantType, literal.TypeName)
			}
		})
	}
}

func TestIntervalUnitOfIgnoresTheCaseOfASCIILettersOnly(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		word     string
		wantUnit string
	}{
		{name: "a unit in lower case", word: "second", wantUnit: "Second"},
		{name: "a plural in mixed case", word: "MiNuTeS", wantUnit: "Minute"},
		{name: "a one-letter abbreviation", word: "q", wantUnit: "Quarter"},
		{name: "an SQL_TSI_ form", word: "Sql_Tsi_Year", wantUnit: "Year"},
		{name: "a word that names no unit", word: "fortnight"},
		{name: "the empty word", word: ""},
		{name: "a long s, which upper-cases to S", word: "ſ"},
		{name: "a dotless i, which upper-cases to I", word: "mıNUTE"},
		{name: "a word with a multi-byte letter", word: "éon"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			unit, isUnit := intervalUnitOf(testCase.word)
			require.Equal(t, testCase.wantUnit != "", isUnit)
			require.Equal(t, testCase.wantUnit, unit)
		})
	}
}

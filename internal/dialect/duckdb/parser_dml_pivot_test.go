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
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func pivotInList(column string, count int) string {
	values := make([]string, count)
	for index := range values {
		values[index] = fmt.Sprint(index)
	}
	return column + " IN (" + strings.Join(values, ", ") + ")"
}

func pivotInLists(lists, values int) []string {
	inLists := make([]string, lists)
	for index := range inLists {
		inLists[index] = pivotInList(fmt.Sprintf("c%d", index), values)
	}
	return inLists
}

func analyseWith(ctx context.Context, t *testing.T, engine *Dialect, sql string) error {
	t.Helper()
	statements, err := engine.ParseStatements(ctx, sql)
	require.NoError(t, err)
	require.Len(t, statements, 1)
	_, err = engine.AnalyseQuery(ctx, nil, statements[0])
	return err
}

func TestPivotRefusesMoreOutputColumnsThanTheLimitBeforeMakingThem(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		sql     string
		keyword string
		options []Option
	}{
		{name: "a PIVOT statement over four IN lists of thirty values",
			sql:     "PIVOT t ON " + strings.Join(pivotInLists(4, 30), ", ") + " USING sum(x) GROUP BY g",
			keyword: "PIVOT"},
		{name: "a PIVOT_WIDER statement over five IN lists of thirty values",
			sql:     "PIVOT_WIDER t ON " + strings.Join(pivotInLists(5, 30), ", ") + " USING sum(x) GROUP BY g",
			keyword: "PIVOT_WIDER"},
		{name: "a PIVOT clause over four IN lists of thirty values",
			sql:     "SELECT * FROM t PIVOT (sum(x) FOR " + strings.Join(pivotInLists(4, 30), " ") + " GROUP BY g)",
			keyword: "PIVOT"},
		{name: "a lower limit", sql: "PIVOT t ON c0 IN (1, 2, 3), c1 IN (1, 2, 3) USING sum(x) GROUP BY g", keyword: "PIVOT",
			options: []Option{WithMaxPivotColumns(8)}},
		{name: "more aggregates than the limit", sql: "PIVOT t ON c0 IN (1) USING sum(x), sum(y), sum(z) GROUP BY g",
			keyword: "PIVOT", options: []Option{WithMaxPivotColumns(2)}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New(testCase.options...)
			scaling.Within(t, 50*time.Millisecond, func(ctx context.Context) {
				err := analyseWith(ctx, t, engine, testCase.sql)

				require.ErrorIs(t, err, fault.ErrLimit)
				require.ErrorIs(t, err, errPivotColumnLimit)
				var located interface{ Location() (string, int) }
				require.ErrorAs(t, err, &located)
				token, position := located.Location()
				assert.Equal(t, testCase.keyword, token)
				assert.Equal(t, strings.Index(testCase.sql, testCase.keyword), position)
			})
		})
	}
}

func TestPivotWithinTheLimitMakesEveryColumn(t *testing.T) {
	t.Parallel()

	engine := New(WithMaxPivotColumns(9))
	statements, err := engine.ParseStatements(t.Context(), "PIVOT t ON c0 IN (1, 2, 3), c1 IN (1, 2, 3) USING sum(x) GROUP BY g")
	require.NoError(t, err)

	analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])

	require.NoError(t, err)
	require.Len(t, analysis.OutputColumns, 10)
	assert.Equal(t, "1_1", analysis.OutputColumns[1].Name)
	assert.Equal(t, "3_3", analysis.OutputColumns[9].Name)
}

func TestPivotColumnCountStopsBeforeItOverflows(t *testing.T) {
	t.Parallel()

	wide := make([]pivotValue, 1<<12)
	testCases := []struct {
		name       string
		valueLists [][]pivotValue
		aggregates int
		limit      int
		wantCount  int
		wantFits   bool
	}{
		{name: "a product that fits", valueLists: [][]pivotValue{make([]pivotValue, 3), make([]pivotValue, 4)}, aggregates: 2,
			limit: 24, wantCount: 24, wantFits: true},
		{name: "a product one past the limit", valueLists: [][]pivotValue{make([]pivotValue, 5), make([]pivotValue, 5)},
			aggregates: 1, limit: 24, wantCount: 0, wantFits: false},
		{name: "a product past the largest int", valueLists: [][]pivotValue{wide, wide, wide, wide, wide, wide},
			aggregates: 1, limit: math.MaxInt, wantCount: 0, wantFits: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			count, fits := pivotColumnCount(testCase.valueLists, testCase.aggregates, testCase.limit)

			assert.Equal(t, testCase.wantCount, count)
			assert.Equal(t, testCase.wantFits, fits)
		})
	}
}

func TestPivotErrorsArePlacedAtTheTokenTheyAreAbout(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		wantToken string
		wantAt    string
	}{
		{name: "an ON column without an IN list", sql: "PIVOT t ON c0 IN (1), species USING sum(x) GROUP BY g",
			wantToken: "species", wantAt: "species USING"},
		{name: "a pivot whose source columns cannot be listed", sql: "PIVOT t ON c0 IN (1) USING sum(x)",
			wantToken: "PIVOT", wantAt: "PIVOT"},
		{name: "an unpivot whose source columns cannot be listed", sql: "UNPIVOT t ON a, b INTO NAME n VALUE v",
			wantToken: "UNPIVOT", wantAt: "UNPIVOT"},
		{name: "an unpivot of columns it cannot list", sql: "UNPIVOT t ON COLUMNS(*) INTO NAME n VALUE v",
			wantToken: "COLUMNS", wantAt: "COLUMNS"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := analyseWith(t.Context(), t, New(), testCase.sql)

			require.ErrorIs(t, err, fault.ErrParse)
			require.ErrorIs(t, err, errUnanalysable)
			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, testCase.wantToken, parseError.Token)
			assert.Equal(t, strings.Index(testCase.sql, testCase.wantAt), parseError.Position)
		})
	}
}

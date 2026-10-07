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

package diagbuild_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
)

func limitedDiagnostic(code string, severity diag.ErrorSeverity) diag.SourceError {
	return diagbuild.Block("queries/report.sql", 7, code, severity, "problem "+code)
}

func placeAtFileStart(code string, severity diag.ErrorSeverity, message string) diag.SourceError {
	return diagbuild.Block("queries/report.sql", 1, code, severity, message)
}

func TestLimiterKeepsTheFirstDiagnosticsAndReportsTheRestOnce(t *testing.T) {
	t.Parallel()

	unknownColumn := limitedDiagnostic(diag.CodeUnknownColumn, diag.SeverityWarning)
	unknownTable := limitedDiagnostic(diag.CodeUnknownTable, diag.SeverityWarning)
	parseError := limitedDiagnostic(diag.CodeParseError, diag.SeverityError)
	aliasPrefix := limitedDiagnostic(diag.CodeAliasPrefix, diag.SeverityHint)
	testCases := []struct {
		name        string
		batches     [][]diag.SourceError
		want        []diag.SourceError
		limit       int
		wantDropped int
		wantFull    bool
	}{
		{
			name:        "under the cap every diagnostic is kept",
			limit:       3,
			batches:     [][]diag.SourceError{{unknownColumn, parseError}},
			want:        []diag.SourceError{unknownColumn, parseError},
			wantDropped: 0,
			wantFull:    false,
		},
		{
			name:        "at the cap every diagnostic is kept and the limiter is full",
			limit:       2,
			batches:     [][]diag.SourceError{{unknownColumn}, {parseError}},
			want:        []diag.SourceError{unknownColumn, parseError},
			wantDropped: 0,
			wantFull:    true,
		},
		{
			name:    "over the cap the notice takes the highest severity left out",
			limit:   1,
			batches: [][]diag.SourceError{{unknownColumn, aliasPrefix}, {parseError, unknownTable}},
			want: []diag.SourceError{
				unknownColumn,
				placeAtFileStart(diag.CodeDiagnosticLimit, diag.SeverityError, "and 3 more diagnostics were left out"),
			},
			wantDropped: 3,
			wantFull:    true,
		},
		{
			name:    "over the cap a kept error does not raise the notice above what was left out",
			limit:   1,
			batches: [][]diag.SourceError{{parseError, aliasPrefix, unknownTable}},
			want: []diag.SourceError{
				parseError,
				placeAtFileStart(diag.CodeDiagnosticLimit, diag.SeverityWarning, "and 2 more diagnostics were left out"),
			},
			wantDropped: 2,
			wantFull:    true,
		},
		{
			name:    "over the cap by one the notice speaks of one diagnostic",
			limit:   2,
			batches: [][]diag.SourceError{{unknownColumn, unknownTable, aliasPrefix}},
			want: []diag.SourceError{
				unknownColumn,
				unknownTable,
				placeAtFileStart(diag.CodeDiagnosticLimit, diag.SeverityHint, "and 1 more diagnostic was left out"),
			},
			wantDropped: 1,
			wantFull:    true,
		},
		{
			name:    "a zero limit keeps nothing and counts everything",
			limit:   0,
			batches: [][]diag.SourceError{{unknownColumn}, {unknownTable}},
			want: []diag.SourceError{
				placeAtFileStart(diag.CodeDiagnosticLimit, diag.SeverityWarning, "and 2 more diagnostics were left out"),
			},
			wantDropped: 2,
			wantFull:    true,
		},
		{
			name:    "a negative limit keeps nothing and counts everything",
			limit:   -5,
			batches: [][]diag.SourceError{{aliasPrefix, parseError}},
			want: []diag.SourceError{
				placeAtFileStart(diag.CodeDiagnosticLimit, diag.SeverityError, "and 2 more diagnostics were left out"),
			},
			wantDropped: 2,
			wantFull:    true,
		},
		{
			name:        "a non-positive limit with nothing added reports nothing",
			limit:       0,
			batches:     nil,
			want:        nil,
			wantDropped: 0,
			wantFull:    true,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			limiter := diagbuild.NewLimiter(testCase.limit)
			for _, batch := range testCase.batches {
				limiter.Add(batch...)
			}

			assert.Equal(t, testCase.wantFull, limiter.Full())
			assert.Equal(t, testCase.wantDropped, limiter.Dropped())
			assert.Equal(t, testCase.want, limiter.Finish(placeAtFileStart))
		})
	}
}

func TestLimiterFinishWithNothingDroppedNeverPlacesANotice(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		added []diag.SourceError
		limit int
	}{
		{name: "nothing added", added: nil, limit: 4},
		{name: "fewer than the cap", added: []diag.SourceError{limitedDiagnostic(diag.CodeUnknownColumn, diag.SeverityWarning)}, limit: 4},
		{
			name: "exactly the cap",
			added: []diag.SourceError{
				limitedDiagnostic(diag.CodeUnknownColumn, diag.SeverityWarning),
				limitedDiagnostic(diag.CodeParseError, diag.SeverityError),
			},
			limit: 2,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			limiter := diagbuild.NewLimiter(testCase.limit)
			limiter.Add(testCase.added...)

			got := limiter.Finish(func(code string, _ diag.ErrorSeverity, _ string) diag.SourceError {
				require.FailNow(t, "no notice should be placed", "place was called with %s", code)
				return diag.SourceError{}
			})

			assert.Zero(t, limiter.Dropped())
			assert.Equal(t, testCase.added, got)
		})
	}
}

func TestLimiterFinishWithoutAPlaceLeavesTheNoticeUnlocated(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		want diag.SourceError
		add  []diag.SourceError
	}{
		{
			name: "a dropped warning gives an unlocated warning",
			add:  []diag.SourceError{limitedDiagnostic(diag.CodeUnknownColumn, diag.SeverityWarning)},
			want: diagbuild.Unlocated(diag.CodeDiagnosticLimit, diag.SeverityWarning, "and 1 more diagnostic was left out"),
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			limiter := diagbuild.NewLimiter(0)
			limiter.Add(testCase.add...)

			assert.Equal(t, []diag.SourceError{testCase.want}, limiter.Finish(nil))
		})
	}
}

func TestLimiterFinishCanBeCalledAgain(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		added []diag.SourceError
		limit int
	}{
		{
			name: "with diagnostics left out",
			added: []diag.SourceError{
				limitedDiagnostic(diag.CodeUnknownColumn, diag.SeverityWarning),
				limitedDiagnostic(diag.CodeParseError, diag.SeverityError),
				limitedDiagnostic(diag.CodeUnknownTable, diag.SeverityWarning),
			},
			limit: 2,
		},
		{
			name:  "with nothing left out",
			added: []diag.SourceError{limitedDiagnostic(diag.CodeUnknownColumn, diag.SeverityWarning)},
			limit: 2,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			limiter := diagbuild.NewLimiter(testCase.limit)
			limiter.Add(testCase.added...)

			first := limiter.Finish(placeAtFileStart)
			second := limiter.Finish(placeAtFileStart)

			assert.Equal(t, first, second)
			assert.LessOrEqual(t, len(first), testCase.limit+1)
		})
	}
}

func TestLimiterZeroValueKeepsNothing(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		added []diag.SourceError
		want  int
	}{
		{name: "two diagnostics are counted", added: []diag.SourceError{
			limitedDiagnostic(diag.CodeUnknownColumn, diag.SeverityWarning),
			limitedDiagnostic(diag.CodeUnknownTable, diag.SeverityHint),
		}, want: 2},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var limiter diagbuild.Limiter
			limiter.Add(testCase.added...)

			assert.True(t, limiter.Full())
			assert.Equal(t, testCase.want, limiter.Dropped())
		})
	}
}

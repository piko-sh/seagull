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

package check

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
)

func TestQueryReporterPlacesEachProblemWhereTheDialectRecordedIt(t *testing.T) {
	t.Parallel()

	const sql = "SELECT id\nFROM items\nWHERE nmae = 1"
	testCases := []struct {
		name       string
		span       ast.Span
		wantLine   int
		wantColumn int
	}{
		{name: "a known place", span: ast.Span{Start: 27, End: 31}, wantLine: 5, wantColumn: 7},
		{name: "an unknown place, on the query", span: ast.Span{}, wantLine: 3, wantColumn: 1},
		{name: "a place past the block's text, on the query", span: ast.Span{Start: 40, End: 44}, wantLine: 3, wantColumn: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reporter := newQueryReporter("items.sql", sql, 3)
			reporter.add(testCase.span, diag.CodeUnknownColumn, diag.SeverityWarning, `unknown column "nmae"`, "name")

			diagnostics := reporter.finish()

			require.Len(t, diagnostics, 1)
			require.Equal(t, "items.sql", diagnostics[0].Filename)
			require.Equal(t, testCase.wantLine, diagnostics[0].Line)
			require.Equal(t, testCase.wantColumn, diagnostics[0].Column)
			require.Equal(t, "name", diagnostics[0].Suggestion)
		})
	}
}

func TestQueryReporterKeepsTheQueryCapOfProblems(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		problems    int
		wantSummary bool
	}{
		{name: "as many problems as the cap", problems: MaxDiagnosticsPerQuery, wantSummary: false},
		{name: "more problems than the cap", problems: MaxDiagnosticsPerQuery + 3, wantSummary: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reporter := newQueryReporter("items.sql", "SELECT 1", 3)
			for index := range testCase.problems {
				severity := diag.SeverityWarning
				if index == testCase.problems-1 {
					severity = diag.SeverityError
				}
				reporter.add(ast.Span{Start: 0, End: 6}, diag.CodeInsertShape, severity, "a problem", "")
			}

			diagnostics := reporter.finish()

			if !testCase.wantSummary {
				require.Len(t, diagnostics, testCase.problems)
				return
			}
			require.Len(t, diagnostics, MaxDiagnosticsPerQuery+1)
			summary := diagnostics[len(diagnostics)-1]
			require.Equal(t, diag.CodeDiagnosticLimit, summary.Code)
			require.Equal(t, diag.SeverityError, summary.Severity, "an error left out keeps the summary an error")
			require.Equal(t, 3, summary.Line)
		})
	}
}

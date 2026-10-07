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

package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
)

func unknownTable(line int) diag.SourceError {
	return diag.SourceError{
		Filename: "articles.sql", Message: `unknown table or alias "articles"`, Code: diag.CodeUnknownColumn,
		Line: line, Column: 1, EndLine: line, EndColumn: 70, Severity: diag.SeverityWarning,
	}
}

func TestCollectDiagnosticsReportsIdenticalDiagnosticsOnce(t *testing.T) {
	t.Parallel()

	withSuggestion := unknownTable(2)
	withSuggestion.Suggestion = "authors"
	asError := unknownTable(2)
	asError.Severity = diag.SeverityError
	otherFile := unknownTable(2)
	otherFile.Filename = "authors.sql"
	otherCode := unknownTable(2)
	otherCode.Code = diag.CodeUnknownTable
	otherMessage := unknownTable(2)
	otherMessage.Message = `unknown table or alias "authors"`
	otherEnd := unknownTable(2)
	otherEnd.EndColumn = 12

	tests := []struct {
		name     string
		lists    [][]diag.SourceError
		expected []diag.SourceError
	}{
		{name: "nothing", lists: nil, expected: []diag.SourceError{}},
		{
			name:     "a repeat within one stage",
			lists:    [][]diag.SourceError{{unknownTable(2), unknownTable(2), unknownTable(2)}},
			expected: []diag.SourceError{unknownTable(2)},
		},
		{
			name:     "a repeat across stages keeps the first",
			lists:    [][]diag.SourceError{{unknownTable(2)}, {unknownTable(5), withSuggestion}},
			expected: []diag.SourceError{unknownTable(2), unknownTable(5)},
		},
		{
			name:     "a different severity, file, code, message or span is another diagnostic",
			lists:    [][]diag.SourceError{{unknownTable(2), asError, otherFile, otherCode, otherMessage, otherEnd, unknownTable(3)}},
			expected: []diag.SourceError{unknownTable(2), asError, otherFile, otherCode, otherMessage, otherEnd, unknownTable(3)},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.expected, collectDiagnostics(DefaultMaxDiagnostics, testCase.lists...))
		})
	}
}

func TestDiagnosticCollectorKeepsTheFirstUpToItsCapAndSummarisesTheRest(t *testing.T) {
	t.Parallel()

	asError := unknownTable(9)
	asError.Severity = diag.SeverityError
	tests := []struct {
		name         string
		limit        int
		diagnostics  []diag.SourceError
		wantKept     []diag.SourceError
		wantNotice   string
		wantSeverity diag.ErrorSeverity
	}{
		{
			name:        "within the cap nothing is summarised",
			limit:       3,
			diagnostics: []diag.SourceError{unknownTable(1), unknownTable(2), unknownTable(2), unknownTable(3)},
			wantKept:    []diag.SourceError{unknownTable(1), unknownTable(2), unknownTable(3)},
		},
		{
			name:         "past the cap the rest are summarised at their highest severity",
			limit:        2,
			diagnostics:  []diag.SourceError{unknownTable(1), unknownTable(2), unknownTable(3), asError, unknownTable(4)},
			wantKept:     []diag.SourceError{unknownTable(1), unknownTable(2)},
			wantNotice:   "and 3 more diagnostics were left out",
			wantSeverity: diag.SeverityError,
		},
		{
			name:         "a repeat of a kept diagnostic is not counted",
			limit:        1,
			diagnostics:  []diag.SourceError{unknownTable(1), unknownTable(1), unknownTable(2)},
			wantKept:     []diag.SourceError{unknownTable(1)},
			wantNotice:   "and 1 more diagnostic was left out",
			wantSeverity: diag.SeverityWarning,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			collector := newDiagnosticCollector(testCase.limit)
			collector.add(testCase.diagnostics...)
			collected := collector.collected()
			if testCase.wantNotice == "" {
				assert.Equal(t, testCase.wantKept, collected)
				return
			}
			require.Len(t, collected, len(testCase.wantKept)+1)
			assert.Equal(t, testCase.wantKept, collected[:len(testCase.wantKept)])
			notice := collected[len(testCase.wantKept)]
			assert.Equal(t, diag.CodeDiagnosticLimit, notice.Code)
			assert.Equal(t, testCase.wantSeverity, notice.Severity)
			assert.Equal(t, testCase.wantNotice, notice.Message)
		})
	}
}

func TestDiagnosticCollectorHandsOutCopiesItKeepsGathering(t *testing.T) {
	t.Parallel()

	collector := newDiagnosticCollector(DefaultMaxDiagnostics)
	collector.add(unknownTable(1))
	first := collector.collected()
	first[0].Message = "changed"
	collector.add(unknownTable(2))
	second := collector.collected()

	require.Len(t, second, 2)
	assert.Equal(t, unknownTable(1), second[0])
	assert.Equal(t, unknownTable(2), second[1])
	assert.Len(t, first, 1)
}

func TestDiagnosticCollectorHoldsNoMoreThanItsCap(t *testing.T) {
	t.Parallel()

	collector := newDiagnosticCollector(10)
	for line := range 10_000 {
		collector.add(unknownTable(line + 1))
	}

	assert.Len(t, collector.kept, 10)
	collected := collector.collected()
	require.Len(t, collected, 11)
	assert.Equal(t, "and 9990 more diagnostics were left out", collected[10].Message)
}

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

package diag_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/testkit/scaling"
)

var errWriteFailed = errors.New("write failed")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errWriteFailed
}

type countingWriter struct {
	writes  int
	largest int
	failAt  int
}

func (writer *countingWriter) Write(text []byte) (int, error) {
	writer.writes++
	writer.largest = max(writer.largest, len(text))
	if writer.writes == writer.failAt {
		return 0, errWriteFailed
	}
	return len(text), nil
}

func TestPresent(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		display    string
		sourceLine string
		diagnostic diag.SourceError
		expected   diag.Presentation
	}{
		{
			name:       "error with a one-word suggestion",
			display:    "db/queries/tasks.sql",
			sourceLine: "SELECT tilte FROM tasks",
			diagnostic: diag.SourceError{
				Code: "Q001", Message: `unknown column "tilte"; did you mean "title"?`, Suggestion: "title",
				Line: 3, Column: 8, EndLine: 3, EndColumn: 13, Severity: diag.SeverityError,
			},
			expected: diag.Presentation{
				Location: "db/queries/tasks.sql:3:8", Severity: "error", Code: "Q001", Title: "Unknown column",
				Message: `unknown column "tilte"`, Help: `did you mean "title"?`, Source: "SELECT tilte FROM tasks",
				LineNo: "3", Gutter: " ", CaretPad: "       ", CaretWidth: 5,
			},
		},
		{
			name:       "a suggestion embedded mid-message is removed with its punctuation",
			display:    "db/queries/tasks.sql",
			sourceLine: "-- sgl.query(Bar, mnay)",
			diagnostic: diag.SourceError{
				Code:       "Q028",
				Message:    `invalid value "mnay" for keyword argument "command"; did you mean "many"? allowed: one, many, exec`,
				Suggestion: "many", Line: 1, Column: 19, EndLine: 1, EndColumn: 23, Severity: diag.SeverityError,
			},
			expected: diag.Presentation{
				Location: "db/queries/tasks.sql:1:19", Severity: "error", Code: "Q028", Title: "Invalid keyword argument value",
				Message: `invalid value "mnay" for keyword argument "command"; allowed: one, many, exec`,
				Help:    `did you mean "many"?`, Source: "-- sgl.query(Bar, mnay)",
				LineNo: "1", Gutter: " ", CaretPad: "                  ", CaretWidth: 4,
			},
		},
		{
			name:       "a suggestion that is not in the message leaves the message alone",
			display:    "q.sql",
			diagnostic: diag.SourceError{Code: "Q999", Message: "something; did you mean it?", Suggestion: "other", Line: 2},
			expected: diag.Presentation{
				Location: "q.sql:2", Severity: "error", Code: "Q999", Message: "something; did you mean it?",
				Help: `did you mean "other"?`,
			},
		},
		{
			name:       "warning with a sentence suggestion and the code repeated in the message",
			display:    "db/queries/users.sql",
			sourceLine: "DELETE FROM users",
			diagnostic: diag.SourceError{
				Code: "Q009", Message: "Q009: exec on a statement without a filter", Suggestion: "add a WHERE clause",
				Line: 7, Column: 1, Severity: diag.SeverityWarning,
			},
			expected: diag.Presentation{
				Location: "db/queries/users.sql:7:1", Severity: "warning", Code: "Q009", Title: "Command does not match the query's output",
				Message: "exec on a statement without a filter", Help: "add a WHERE clause", Source: "DELETE FROM users",
				LineNo: "7", Gutter: " ", CaretPad: "", CaretWidth: 1,
			},
		},
		{
			name:       "hint without a suggestion",
			display:    "db/queries/batch.sql",
			sourceLine: "SELECT 1; SELECT 2;",
			diagnostic: diag.SourceError{Code: "Q012", Message: "query block holds 2 statements", Line: 2, Column: 11, Severity: diag.SeverityHint},
			expected: diag.Presentation{
				Location: "db/queries/batch.sql:2:11", Severity: "hint", Code: "Q012", Title: "Query block holds more than one statement",
				Message: "query block holds 2 statements", Source: "SELECT 1; SELECT 2;",
				LineNo: "2", Gutter: " ", CaretPad: "          ", CaretWidth: 1,
			},
		},
		{
			name:       "tabs before the column are kept",
			display:    "q.sql",
			sourceLine: "\tSELECT\ttilte FROM tasks",
			diagnostic: diag.SourceError{Code: "Q001", Message: "unknown column", Line: 4, Column: 9, EndLine: 4, EndColumn: 14},
			expected: diag.Presentation{
				Location: "q.sql:4:9", Severity: "error", Code: "Q001", Title: "Unknown column", Message: "unknown column",
				Source: "\tSELECT\ttilte FROM tasks", LineNo: "4", Gutter: " ", CaretPad: "\t      \t", CaretWidth: 5,
			},
		},
		{
			name:       "the gutter is as wide as a long line number",
			display:    "q.sql",
			sourceLine: "SELECT x",
			diagnostic: diag.SourceError{Code: "Q001", Message: "unknown column", Line: 12345, Column: 8, EndLine: 12345, EndColumn: 9},
			expected: diag.Presentation{
				Location: "q.sql:12345:8", Severity: "error", Code: "Q001", Title: "Unknown column", Message: "unknown column",
				Source: "SELECT x", LineNo: "12345", Gutter: "     ", CaretPad: "       ", CaretWidth: 1,
			},
		},
		{
			name:       "a span over several lines gets one caret",
			display:    "q.sql",
			sourceLine: "SELECT (a +",
			diagnostic: diag.SourceError{Code: "Q004", Message: "cannot infer the type", Line: 3, Column: 8, EndLine: 5, EndColumn: 3},
			expected: diag.Presentation{
				Location: "q.sql:3:8", Severity: "error", Code: "Q004", Title: "Expression type cannot be inferred", Message: "cannot infer the type",
				Source: "SELECT (a +", LineNo: "3", Gutter: " ", CaretPad: "       ", CaretWidth: 1,
			},
		},
		{
			name:       "a span past the end of the line is cut at the end",
			display:    "q.sql",
			sourceLine: "SELECT ab",
			diagnostic: diag.SourceError{Code: "Q001", Message: "unknown column", Line: 1, Column: 8, EndLine: 1, EndColumn: 40},
			expected: diag.Presentation{
				Location: "q.sql:1:8", Severity: "error", Code: "Q001", Title: "Unknown column", Message: "unknown column",
				Source: "SELECT ab", LineNo: "1", Gutter: " ", CaretPad: "       ", CaretWidth: 2,
			},
		},
		{
			name:       "a span is measured in characters, not bytes",
			display:    "q.sql",
			sourceLine: "SELECT naïve FROM t",
			diagnostic: diag.SourceError{Code: "Q001", Message: "unknown column", Line: 1, Column: 8, EndLine: 1, EndColumn: 14},
			expected: diag.Presentation{
				Location: "q.sql:1:8", Severity: "error", Code: "Q001", Title: "Unknown column", Message: "unknown column",
				Source: "SELECT naïve FROM t", LineNo: "1", Gutter: " ", CaretPad: "       ", CaretWidth: 5,
			},
		},
		{
			name:       "a column past the end of the line pads to the end",
			display:    "q.sql",
			sourceLine: "abc",
			diagnostic: diag.SourceError{Code: "Q010", Message: "unexpected end of input", Line: 1, Column: 99},
			expected: diag.Presentation{
				Location: "q.sql:1:99", Severity: "error", Code: "Q010", Title: "SQL cannot be parsed or analysed", Message: "unexpected end of input",
				Source: "abc", LineNo: "1", Gutter: " ", CaretPad: "   ", CaretWidth: 1,
			},
		},
		{
			name:       "no column puts the caret at the start of the line",
			display:    "q.sql",
			sourceLine: "abc",
			diagnostic: diag.SourceError{Code: "Q010", Message: "cannot parse", Line: 1, EndLine: 1, EndColumn: 3},
			expected: diag.Presentation{
				Location: "q.sql:1", Severity: "error", Code: "Q010", Title: "SQL cannot be parsed or analysed", Message: "cannot parse",
				Source: "abc", LineNo: "1", Gutter: " ", CaretPad: "", CaretWidth: 2,
			},
		},
		{
			name:       "a trailing carriage return is dropped",
			display:    "q.sql",
			sourceLine: "SELECT x\r\n",
			diagnostic: diag.SourceError{Code: "Q001", Message: "unknown column", Line: 1, Column: 8},
			expected: diag.Presentation{
				Location: "q.sql:1:8", Severity: "error", Code: "Q001", Title: "Unknown column", Message: "unknown column",
				Source: "SELECT x", LineNo: "1", Gutter: " ", CaretPad: "       ", CaretWidth: 1,
			},
		},
		{
			name:       "no source line",
			display:    "db/queries/tasks.sql",
			sourceLine: "",
			diagnostic: diag.SourceError{Code: "Q001", Message: "unknown column", Suggestion: "title", Line: 3, Column: 8, EndLine: 3, EndColumn: 13},
			expected: diag.Presentation{
				Location: "db/queries/tasks.sql:3:8", Severity: "error", Code: "Q001", Title: "Unknown column",
				Message: "unknown column", Help: `did you mean "title"?`,
			},
		},
		{
			name:       "a source line without a line number is not quoted",
			display:    "sgl.yaml",
			sourceLine: "version: 1",
			diagnostic: diag.SourceError{Code: "Q037", Message: "unknown column", Severity: diag.SeverityError},
			expected: diag.Presentation{
				Location: "sgl.yaml", Severity: "error", Code: "Q037", Title: "Column override names an unknown table column", Message: "unknown column",
			},
		},
		{
			name:       "a line without a column",
			display:    "q.sql",
			diagnostic: diag.SourceError{Code: "Q999", Message: "something", Line: 2},
			expected:   diag.Presentation{Location: "q.sql:2", Severity: "error", Code: "Q999", Message: "something"},
		},
		{
			name:       "no path",
			sourceLine: "SELECT x",
			diagnostic: diag.SourceError{Code: "Q001", Message: "unknown column", Line: 1, Column: 8},
			expected: diag.Presentation{
				Location: "1:8", Severity: "error", Code: "Q001", Title: "Unknown column", Message: "unknown column",
				Source: "SELECT x", LineNo: "1", Gutter: " ", CaretPad: "       ", CaretWidth: 1,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, diag.Present(testCase.diagnostic, testCase.display, testCase.sourceLine))
		})
	}
}

func TestWriteText(t *testing.T) {
	t.Parallel()

	tilte := diag.Present(diag.SourceError{
		Code: "Q001", Message: `unknown column "tilte"; did you mean "title"?`, Suggestion: "title",
		Line: 3, Column: 8, EndLine: 3, EndColumn: 13, Severity: diag.SeverityError,
	}, "db/queries/tasks.sql", "SELECT tilte FROM tasks")
	labelled := tilte
	labelled.Label = "not a column of tasks"

	testCases := []struct {
		name     string
		expected string
		items    []diag.Presentation
	}{
		{
			name:     "nothing to print",
			items:    nil,
			expected: "",
		},
		{
			name:  "source line and help",
			items: []diag.Presentation{tilte},
			expected: "db/queries/tasks.sql:3:8: error[Q001]: unknown column \"tilte\"\n" +
				"   |\n" +
				" 3 | SELECT tilte FROM tasks\n" +
				"   |        ^^^^^\n" +
				"help: did you mean \"title\"?\n",
		},
		{
			name:  "a label follows the carets",
			items: []diag.Presentation{labelled},
			expected: "db/queries/tasks.sql:3:8: error[Q001]: unknown column \"tilte\"\n" +
				"   |\n" +
				" 3 | SELECT tilte FROM tasks\n" +
				"   |        ^^^^^ not a column of tasks\n" +
				"help: did you mean \"title\"?\n",
		},
		{
			name: "tabs and a wide gutter",
			items: []diag.Presentation{diag.Present(diag.SourceError{
				Code: "Q050", Message: "directive uses an alias prefix", Line: 1024, Column: 5, EndLine: 1024, EndColumn: 8, Severity: diag.SeverityHint,
			}, "q.sql", "\t-- app.query(Get, one)")},
			expected: "q.sql:1024:5: hint[Q050]: directive uses an alias prefix\n" +
				"      |\n" +
				" 1024 | \t-- app.query(Get, one)\n" +
				"      | \t   ^^^\n",
		},
		{
			name: "no source line prints the header and help",
			items: []diag.Presentation{diag.Present(diag.SourceError{
				Code: "Q037", Message: `type override references unknown column "authors.nmae"`, Suggestion: "authors.name", Severity: diag.SeverityError,
			}, "sgl.yaml", "")},
			expected: "sgl.yaml: error[Q037]: type override references unknown column \"authors.nmae\"\n" +
				"help: did you mean \"authors.name\"?\n",
		},
		{
			name: "blocks are separated by a blank line",
			items: []diag.Presentation{
				diag.Present(diag.SourceError{Code: "Q001", Message: "unknown column", Line: 1, Column: 1, Severity: diag.SeverityWarning}, "a.sql", "nmae"),
				diag.Present(diag.SourceError{Code: "Q006", Message: "duplicate query name", Severity: diag.SeverityError}, "b.sql", ""),
			},
			expected: "a.sql:1:1: warning[Q001]: unknown column\n" +
				"   |\n" +
				" 1 | nmae\n" +
				"   | ^\n" +
				"\n" +
				"b.sql: error[Q006]: duplicate query name\n",
		},
		{
			name:     "no location and no code",
			items:    []diag.Presentation{{Severity: "error", Message: "the generator failed"}},
			expected: "error: the generator failed\n",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var output bytes.Buffer
			require.NoError(t, diag.WriteText(&output, testCase.items))
			require.Equal(t, testCase.expected, output.String())
		})
	}
}

func TestWriteTextReportsAWriteFailure(t *testing.T) {
	t.Parallel()

	items := []diag.Presentation{{Severity: "error", Code: "Q001", Message: "unknown column"}}
	require.ErrorIs(t, diag.WriteText(failingWriter{}, items), errWriteFailed)
}

func TestPresentQuotesAWindowOfALongLine(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a", 1_000) + "tilte" + strings.Repeat("b", 1_000)
	wide := strings.Repeat("é", 1_000) + "tilte" + strings.Repeat("ü", 1_000)
	at := func(column, endColumn int) diag.SourceError {
		return diag.SourceError{Code: "Q001", Message: "unknown column", Line: 1, Column: column, EndLine: 1, EndColumn: endColumn, Severity: diag.SeverityError}
	}
	testCases := []struct {
		name           string
		sourceLine     string
		diagnostic     diag.SourceError
		wantSource     string
		wantCaretPad   string
		wantCaretWidth int
	}{
		{
			name:           "a span in the middle keeps some text before it",
			sourceLine:     long,
			diagnostic:     at(1_001, 1_006),
			wantSource:     "..." + strings.Repeat("a", 40) + "tilte" + strings.Repeat("b", 115) + "...",
			wantCaretPad:   strings.Repeat(" ", 43),
			wantCaretWidth: 5,
		},
		{
			name:           "a span at the start is quoted from the start",
			sourceLine:     long,
			diagnostic:     at(1, 3),
			wantSource:     strings.Repeat("a", 160) + "...",
			wantCaretPad:   "",
			wantCaretWidth: 2,
		},
		{
			name:           "a span at the end fills the window back from the end",
			sourceLine:     long,
			diagnostic:     at(len(long), len(long)+1),
			wantSource:     "..." + strings.Repeat("b", 160),
			wantCaretPad:   strings.Repeat(" ", 3+159),
			wantCaretWidth: 1,
		},
		{
			name:           "a span longer than the window is underlined to the window's end",
			sourceLine:     long,
			diagnostic:     at(1_001, 2_006),
			wantSource:     "..." + strings.Repeat("a", 40) + "tilte" + strings.Repeat("b", 115) + "...",
			wantCaretPad:   strings.Repeat(" ", 43),
			wantCaretWidth: 120,
		},
		{
			name:           "multi-byte runes are never split",
			sourceLine:     wide,
			diagnostic:     at(2_001, 2_006),
			wantSource:     "..." + strings.Repeat("é", 40) + "tilte" + strings.Repeat("ü", 115) + "...",
			wantCaretPad:   strings.Repeat(" ", 43),
			wantCaretWidth: 5,
		},
		{
			name:           "a line of exactly the window is quoted whole",
			sourceLine:     strings.Repeat("c", 160),
			diagnostic:     at(150, 152),
			wantSource:     strings.Repeat("c", 160),
			wantCaretPad:   strings.Repeat(" ", 149),
			wantCaretWidth: 2,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			presentation := diag.Present(testCase.diagnostic, "q.sql", testCase.sourceLine)
			assert.Equal(t, testCase.wantSource, presentation.Source)
			assert.Equal(t, testCase.wantCaretPad, presentation.CaretPad)
			assert.Equal(t, testCase.wantCaretWidth, presentation.CaretWidth)
			assert.True(t, utf8.ValidString(presentation.Source))
			assert.LessOrEqual(t, utf8.RuneCountInString(presentation.Source), 160+2*len("..."))
		})
	}
}

func TestWriteTextWritesEachBlockAsItIsLaidOut(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", 1<<20)
	items := make([]diag.Presentation, 0, 200)
	for column := range 200 {
		items = append(items, diag.Present(diag.SourceError{
			Code: "Q001", Message: "unknown column", Line: 1, Column: column*5_000 + 1, EndLine: 1, EndColumn: column*5_000 + 4, Severity: diag.SeverityError,
		}, "q.sql", long))
	}

	testCases := []struct {
		name        string
		failAt      int
		wantErr     error
		wantWrites  int
		wantLargest int
	}{
		{name: "every block is written on its own", failAt: 0, wantErr: nil, wantWrites: 200, wantLargest: 400},
		{name: "writing stops at the first failure", failAt: 3, wantErr: errWriteFailed, wantWrites: 3, wantLargest: 400},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			writer := &countingWriter{failAt: testCase.failAt}
			err := diag.WriteText(writer, items)
			if testCase.wantErr != nil {
				require.ErrorIs(t, err, testCase.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, testCase.wantWrites, writer.writes)
			assert.LessOrEqual(t, writer.largest, testCase.wantLargest)
		})
	}
}

func TestDidYouMeanPhrasesTheSuggestionThePresentationRemoves(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		suggestion string
		want       string
	}{
		{name: "a name", suggestion: "title", want: `did you mean "title"?`},
		{name: "a name that needs quoting", suggestion: `say "hi"`, want: `did you mean "say \"hi\""?`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, diag.DidYouMean(testCase.suggestion))
			presentation := diag.Present(diag.SourceError{Message: "unknown column; " + diag.DidYouMean(testCase.suggestion), Suggestion: testCase.suggestion}, "", "")
			assert.Equal(t, "unknown column", presentation.Message)
		})
	}
}

func TestPresentingManyDiagnosticsOnAHugeLineStaysWithinBudget(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("y", 4<<20)
	scaling.Within(t, time.Second, func(context.Context) {
		var written bytes.Buffer
		items := make([]diag.Presentation, 0, 1_000)
		for index := range 1_000 {
			column := index*4_000 + 1
			items = append(items, diag.Present(diag.SourceError{
				Code: "Q001", Message: "unknown column", Line: 1, Column: column, EndLine: 1, EndColumn: column + 3_000, Severity: diag.SeverityError,
			}, "q.sql", long))
		}
		require.NoError(t, diag.WriteText(&written, items))
		assert.Less(t, written.Len(), 1_000*500)
	})
}

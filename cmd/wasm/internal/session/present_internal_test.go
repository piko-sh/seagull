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

package session

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
)

func TestPresentDiagnosticLayouts(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		file       string
		lines      []string
		diagnostic seagull.SourceError
		text       string
		caretPad   string
		width      int
	}{
		{
			name:  "one-word suggestion goes on the help line",
			file:  "db/queries/tasks.sql",
			lines: []string{"-- sgl.query(ListTasks, many)", "", "SELECT tilte FROM tasks"},
			diagnostic: seagull.SourceError{
				Filename: "tasks.sql", Message: `Q001: unknown column "tilte"; did you mean "title"?`, Code: "Q001", Suggestion: "title",
				Line: 3, Column: 8, EndLine: 3, EndColumn: 13, Severity: seagull.SeverityError,
			},
			text: "db/queries/tasks.sql:3:8: error[Q001]: unknown column \"tilte\"\n" +
				"   |\n" +
				" 3 | SELECT tilte FROM tasks\n" +
				"   |        ^^^^^\n" +
				"help: did you mean \"title\"?\n",
			caretPad: "       ",
			width:    5,
		},
		{
			name:  "tabs are kept in the padding",
			file:  "db/queries/tasks.sql",
			lines: []string{"\tSELECT\ttilte FROM tasks"},
			diagnostic: seagull.SourceError{
				Filename: "tasks.sql", Message: "unknown column", Code: "Q001", Suggestion: "",
				Line: 1, Column: 9, EndLine: 1, EndColumn: 14, Severity: seagull.SeverityWarning,
			},
			text: "db/queries/tasks.sql:1:9: warning[Q001]: unknown column\n" +
				"   |\n" +
				" 1 | \tSELECT\ttilte FROM tasks\n" +
				"   | \t      \t^^^^^\n",
			caretPad: "\t      \t",
			width:    5,
		},
		{
			name:  "a span over several lines gets one caret",
			file:  "db/queries/tasks.sql",
			lines: []string{"SELECT id,", "  title FROM tasks"},
			diagnostic: seagull.SourceError{
				Filename: "tasks.sql", Message: "too wide", Code: "Q012", Suggestion: "give each statement its own header",
				Line: 1, Column: 1, EndLine: 2, EndColumn: 8, Severity: seagull.SeverityHint,
			},
			text: "db/queries/tasks.sql:1:1: hint[Q012]: too wide\n" +
				"   |\n" +
				" 1 | SELECT id,\n" +
				"   | ^\n" +
				"help: give each statement its own header\n",
			caretPad: "",
			width:    1,
		},
		{
			name:  "a line beyond the file shows no source",
			file:  "db/queries/tasks.sql",
			lines: []string{"SELECT 1"},
			diagnostic: seagull.SourceError{
				Filename: "tasks.sql", Message: "unknown directive", Code: "Q026", Suggestion: "sgl.query",
				Line: 12, Column: 4, EndLine: 0, EndColumn: 0, Severity: seagull.SeverityError,
			},
			text:     "db/queries/tasks.sql:12:4: error[Q026]: unknown directive\nhelp: did you mean \"sgl.query\"?\n",
			caretPad: "",
			width:    0,
		},
		{
			name:  "no file and no code",
			file:  "",
			lines: nil,
			diagnostic: seagull.SourceError{
				Filename: "", Message: "the override names no column", Code: "", Suggestion: "",
				Line: 0, Column: 0, EndLine: 0, EndColumn: 0, Severity: seagull.SeverityError,
			},
			text:     "error: the override names no column\n",
			caretPad: "",
			width:    0,
		},
		{
			name:  "a two-digit line widens the gutter",
			file:  "sgl.yaml",
			lines: []string{"", "", "", "", "", "", "", "", "", "go: 1"},
			diagnostic: seagull.SourceError{
				Filename: "sgl.yaml", Message: "bad", Code: "Q037", Suggestion: "",
				Line: 10, Column: 0, EndLine: 0, EndColumn: 0, Severity: seagull.SeverityError,
			},
			text:     "sgl.yaml:10: error[Q037]: bad\n    |\n 10 | go: 1\n    | ^\n",
			caretPad: "",
			width:    1,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			view := presentDiagnostic(testCase.file, testCase.lines, &testCase.diagnostic)

			require.Equal(t, testCase.text, view.Text)
			require.Equal(t, testCase.caretPad, view.CaretPad)
			require.Equal(t, testCase.width, view.CaretWidth)
			require.Equal(t, testCase.diagnostic.Suggestion, view.Suggestion)
		})
	}
}

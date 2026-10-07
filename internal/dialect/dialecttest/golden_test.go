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

package dialecttest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/martian"
)

const (
	martianGoldenSpec         = `{"description": "a Martian case"}`
	martianGoldenMigration    = "CREATE TABLE items (id WHOLE64 PRIMARY KEY, name TEXTUAL);"
	martianGoldenQuery        = "-- sgl.query(name: GetItem, command: one)\nSELECT id, name FROM items WHERE id = @p1;"
	martianUnknownColumnQuery = "-- sgl.query(name: GetItem, command: one)\nSELECT id, missing FROM items WHERE id = @p1;"
	testDirectoryMode         = 0o755
	testFileMode              = 0o644
)

func newMartian() dialect.DialectPort {
	return martian.New()
}

func writeCaseFiles(t *testing.T, directory string, files map[string]string) {
	t.Helper()

	for name, content := range files {
		target := filepath.Join(directory, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(target), testDirectoryMode))
		require.NoError(t, os.WriteFile(target, []byte(content), testFileMode))
	}
}

func writeFuzzCaseFile(f *testing.F, directory, name, content string) {
	f.Helper()

	target := filepath.Join(directory, filepath.FromSlash(name))
	require.NoErrorf(f, os.MkdirAll(filepath.Dir(target), testDirectoryMode), "creating %s", filepath.Dir(target))
	require.NoErrorf(f, os.WriteFile(target, []byte(content), testFileMode), "writing %s", target)
}

func martianCase(t *testing.T, specification, query string) string {
	t.Helper()

	directory := t.TempDir()
	writeCaseFiles(t, directory, map[string]string{
		specFile:                      specification,
		"migrations/001_items.up.sql": martianGoldenMigration,
		"queries/get_item.sql":        query,
	})
	return directory
}

func recordedMartianCase(t *testing.T, specification, query string) string {
	t.Helper()

	directory := martianCase(t, martianGoldenSpec, query)
	report := runGoldenCase(t.Context(), martian.New(), directory, goldenSettings{update: true})
	require.Empty(t, report.problems)
	writeCaseFiles(t, directory, map[string]string{specFile: specification})
	return directory
}

func TestRunGoldenCaseReportsHowTheRunDiffersFromTheCase(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		prepare        func(t *testing.T) string
		name           string
		expectProblems []string
	}{
		{
			name: "a case whose output matches its goldens passes",
			prepare: func(t *testing.T) string {
				return recordedMartianCase(t, martianGoldenSpec, martianGoldenQuery)
			},
			expectProblems: nil,
		},
		{
			name: "a golden file that differs from the output is a mismatch",
			prepare: func(t *testing.T) string {
				directory := recordedMartianCase(t, martianGoldenSpec, martianGoldenQuery)
				writeCaseFiles(t, directory, map[string]string{"golden/catalogue.json": "{}\n"})
				return directory
			},
			expectProblems: []string{"golden file mismatch: "},
		},
		{
			name: "a golden file the output needs but the case lacks is missing",
			prepare: func(t *testing.T) string {
				directory := recordedMartianCase(t, martianGoldenSpec, martianGoldenQuery)
				require.NoError(t, os.Remove(filepath.Join(directory, goldenDirectory, "queries.json")))
				return directory
			},
			expectProblems: []string{"queries.json (re-record with WithUpdate)"},
		},
		{
			name: "a golden file the run produced nothing for is stale",
			prepare: func(t *testing.T) string {
				directory := recordedMartianCase(t, martianGoldenSpec, martianGoldenQuery)
				writeCaseFiles(t, directory, map[string]string{"golden/diagnostics.json": "[]\n"})
				return directory
			},
			expectProblems: []string{"diagnostics.json exists but the run produced nothing for it"},
		},
		{
			name: "a case that should fail but generates is reported",
			prepare: func(t *testing.T) string {
				return martianCase(t, `{"description": "x", "shouldError": true}`, martianGoldenQuery)
			},
			expectProblems: []string{"expected Generate to return an error"},
		},
		{
			name: "a case that should fail and does passes",
			prepare: func(t *testing.T) string {
				directory := martianCase(t, `{"description": "x", "shouldError": true}`, martianGoldenQuery)
				require.NoError(t, os.RemoveAll(filepath.Join(directory, "migrations")))
				return directory
			},
			expectProblems: nil,
		},
		{
			name: "a case whose generation fails is reported",
			prepare: func(t *testing.T) string {
				directory := martianCase(t, martianGoldenSpec, martianGoldenQuery)
				require.NoError(t, os.RemoveAll(filepath.Join(directory, "migrations")))
				return directory
			},
			expectProblems: []string{"Generate failed: building catalogue: reading migration directory migrations: "},
		},
		{
			name: "an expected diagnostic the run lacks is reported",
			prepare: func(t *testing.T) string {
				return recordedMartianCase(t, `{"description": "x", "diagnostics": [{"code": "Q003", "messageContains": "nowhere"}]}`,
					martianGoldenQuery)
			},
			expectProblems: []string{`expected diagnostic not found: severity= code=Q003 message containing "nowhere"`},
		},
		{
			name: "an expected diagnostic the run reports passes",
			prepare: func(t *testing.T) string {
				specification := `{"description": "x", "expectedDiagnosticCount": 1, "diagnostics": [` +
					`{"code": "Q001", "severity": "WARNING", "messageContains": "MISSING", "filename": "get_item.sql", "onLine": 2}]}`
				return recordedMartianCase(t, specification, martianUnknownColumnQuery)
			},
			expectProblems: nil,
		},
		{
			name: "a diagnostic count the run does not meet is reported",
			prepare: func(t *testing.T) string {
				return recordedMartianCase(t, `{"description": "x", "expectedDiagnosticCount": 3}`, martianUnknownColumnQuery)
			},
			expectProblems: []string{"unexpected diagnostic count: expected 3, got 1"},
		},
		{
			name: "a misspelt spec key fails the case instead of asserting nothing",
			prepare: func(t *testing.T) string {
				return martianCase(t, `{"description": "x", "expectedDiagnostic": 1}`, martianGoldenQuery)
			},
			expectProblems: []string{`parsing testspec.json: json: unknown field "expectedDiagnostic"`},
		},
		{
			name: "a misspelt key of an expected diagnostic fails the case",
			prepare: func(t *testing.T) string {
				return martianCase(t, `{"description": "x", "diagnostics": [{"code": "Q001", "message": "MISSING"}]}`, martianGoldenQuery)
			},
			expectProblems: []string{`parsing testspec.json: json: unknown field "message"`},
		},
		{
			name: "content after the spec fails the case",
			prepare: func(t *testing.T) string {
				return martianCase(t, `{"description": "x"} {"shouldError": true}`, martianGoldenQuery)
			},
			expectProblems: []string{"parsing testspec.json: " + errSpecTrailingContent.Error()},
		},
		{
			name: "a spec that is not JSON fails the case",
			prepare: func(t *testing.T) string {
				return martianCase(t, `{"description": `, martianGoldenQuery)
			},
			expectProblems: []string{"parsing testspec.json: unexpected EOF"},
		},
		{
			name: "a spec larger than the kit reads fails the case",
			prepare: func(t *testing.T) string {
				return martianCase(t, `{"description": "`+strings.Repeat("x", maxSpecBytes)+`"}`, martianGoldenQuery)
			},
			expectProblems: []string{"reading testspec.json: " + errFileTooLarge.Error()},
		},
		{
			name: "a case without a spec fails",
			prepare: func(t *testing.T) string {
				directory := martianCase(t, martianGoldenSpec, martianGoldenQuery)
				require.NoError(t, os.Remove(filepath.Join(directory, specFile)))
				return directory
			},
			expectProblems: []string{"reading testspec.json: open testspec.json: "},
		},
		{
			name: "a custom function the generator refuses fails the case",
			prepare: func(t *testing.T) string {
				return martianCase(t, `{"description": "x", "customFunctions": [{"name": ""}]}`, martianGoldenQuery)
			},
			expectProblems: []string{"creating the generator: "},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			report := runGoldenCase(t.Context(), martian.New(), testCase.prepare(t), goldenSettings{update: false})
			require.Len(t, report.problems, len(testCase.expectProblems), "problems: %v", report.problems)
			for index, expected := range testCase.expectProblems {
				require.Contains(t, report.problems[index], expected)
			}
			require.Empty(t, report.notes)
		})
	}
}

func TestRunGoldenCaseReRecordsAndRemovesStaleGoldens(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		query       string
		stale       bool
		expectNotes []string
		expectFiles []string
	}{
		{
			name:        "a fresh case records what the run produced",
			query:       martianGoldenQuery,
			stale:       false,
			expectNotes: []string{"updated golden file: ", "catalogue.json", "updated golden file: ", "queries.json"},
			expectFiles: []string{"catalogue.json", "queries.json"},
		},
		{
			name:        "a golden the run no longer produces is removed",
			query:       martianGoldenQuery,
			stale:       true,
			expectNotes: []string{"updated golden file: ", "catalogue.json", "updated golden file: ", "queries.json", "removed stale golden file: ", "diagnostics.json"},
			expectFiles: []string{"catalogue.json", "queries.json"},
		},
		{
			name:        "a run with diagnostics records them",
			query:       martianUnknownColumnQuery,
			stale:       false,
			expectNotes: []string{"updated golden file: ", "catalogue.json", "updated golden file: ", "queries.json", "updated golden file: ", "diagnostics.json"},
			expectFiles: []string{"catalogue.json", "diagnostics.json", "queries.json"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			directory := martianCase(t, martianGoldenSpec, testCase.query)
			if testCase.stale {
				writeCaseFiles(t, directory, map[string]string{"golden/diagnostics.json": "[]\n"})
			}
			report := runGoldenCase(t.Context(), martian.New(), directory, goldenSettings{update: true})
			require.Empty(t, report.problems)
			require.Len(t, report.notes, len(testCase.expectNotes)/2)
			for index := range report.notes {
				require.Contains(t, report.notes[index], testCase.expectNotes[2*index])
				require.Contains(t, report.notes[index], testCase.expectNotes[2*index+1])
			}
			entries, err := os.ReadDir(filepath.Join(directory, goldenDirectory))
			require.NoError(t, err)
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				names = append(names, entry.Name())
			}
			require.Equal(t, testCase.expectFiles, names)
			require.Empty(t, runGoldenCase(t.Context(), martian.New(), directory, goldenSettings{update: false}).problems)
		})
	}
}

func TestRunGoldenCaseRecoveredTurnsAPanicIntoAProblemAndLogsItsStackOnce(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		newDialect     func() dialect.DialectPort
		name           string
		expectProblems []string
		expectLogged   int
	}{
		{
			name:           "a dialect that builds runs its case",
			newDialect:     newMartian,
			expectProblems: nil,
			expectLogged:   0,
		},
		{
			name:           "a dialect constructor that panics fails its case alone",
			newDialect:     func() dialect.DialectPort { panic("the dialect is broken") },
			expectProblems: []string{"case panicked: the dialect is broken"},
			expectLogged:   1,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var logged []string
			logf := func(format string, arguments ...any) { logged = append(logged, fmt.Sprintf(format, arguments...)) }
			directory := recordedMartianCase(t, martianGoldenSpec, martianGoldenQuery)
			report := runGoldenCaseRecovered(t.Context(), logf, testCase.newDialect, directory, goldenSettings{update: false})
			require.Equal(t, testCase.expectProblems, report.problems)
			require.Len(t, logged, testCase.expectLogged)
			for _, entry := range logged {
				require.Contains(t, entry, "panicked: the dialect is broken")
				require.Contains(t, entry, "goroutine")
			}
		})
	}
}

func TestGoldenCasesListsTheDirectoriesHoldingASpec(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		files       map[string]string
		name        string
		expected    []string
		expectError error
	}{
		{
			name: "cases are listed in name order",
			files: map[string]string{
				"b_case/" + specFile: martianGoldenSpec,
				"a_case/" + specFile: martianGoldenSpec,
				"no_spec/notes.txt":  "",
				"loose.json":         "",
			},
			expected:    []string{"a_case", "b_case"},
			expectError: nil,
		},
		{
			name:        "a directory without a case is refused",
			files:       map[string]string{"no_spec/notes.txt": ""},
			expected:    nil,
			expectError: errNoGoldenCases,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			directory := t.TempDir()
			writeCaseFiles(t, directory, testCase.files)
			cases, err := goldenCases(directory)
			require.ErrorIs(t, err, testCase.expectError)
			require.Equal(t, testCase.expected, cases)
		})
	}
}

func TestGoldenCasesRefusesADirectoryItCannotRead(t *testing.T) {
	t.Parallel()

	cases, err := goldenCases(filepath.Join(t.TempDir(), "absent"))
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Nil(t, cases)
}

func TestValidateNewDialectRejectsAMissingConstructor(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		newDialect func() dialect.DialectPort
		name       string
		expected   []string
	}{
		{name: "a constructor is usable", newDialect: newMartian, expected: nil},
		{name: "a nil constructor is refused", newDialect: nil, expected: []string{"newDialect is nil"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, validateNewDialect(testCase.newDialect))
		})
	}
}

func TestJSONEqualComparesValuesNotLayout(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		expected string
		actual   string
		equal    bool
	}{
		{name: "identical bytes", expected: `{"a": 1}`, actual: `{"a": 1}`, equal: true},
		{name: "a different layout of one value", expected: "{\"a\": 1, \"b\": [1, 2]}", actual: "{\n  \"b\": [1, 2],\n  \"a\": 1\n}", equal: true},
		{name: "different values", expected: `{"a": 1}`, actual: `{"a": 2}`, equal: false},
		{name: "a document that is not JSON", expected: `{"a": 1`, actual: `{"a": 1}`, equal: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.equal, jsonEqual([]byte(testCase.expected), []byte(testCase.actual)))
		})
	}
}

func TestMatchesDiagnosticComparesEverySetField(t *testing.T) {
	t.Parallel()

	line := 4
	otherLine := 5
	actual := diag.SourceError{Code: "Q001", Severity: diag.SeverityWarning, Message: `unknown column "missing"`, Filename: "queries/get.sql", Line: 4}
	testCases := []struct {
		name     string
		expected goldenDiagnostic
		matches  bool
	}{
		{name: "an empty expectation matches any diagnostic", expected: goldenDiagnostic{}, matches: true},
		{
			name:     "every field set and matching",
			expected: goldenDiagnostic{Code: "Q001", Severity: "warning", MessageContains: "missing", Filename: "get.sql", OnLine: &line},
			matches:  true,
		},
		{name: "another code", expected: goldenDiagnostic{Code: "Q002"}, matches: false},
		{name: "another severity", expected: goldenDiagnostic{Severity: "error"}, matches: false},
		{name: "a severity in another case", expected: goldenDiagnostic{Severity: "Warning"}, matches: true},
		{name: "another message", expected: goldenDiagnostic{MessageContains: "table"}, matches: false},
		{name: "another file", expected: goldenDiagnostic{Filename: "list.sql"}, matches: false},
		{name: "another line", expected: goldenDiagnostic{OnLine: &otherLine}, matches: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.matches, matchesDiagnostic(actual, testCase.expected))
		})
	}
}

func TestRunGoldenComparesEveryCaseItRecorded(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"001_select", "002_unknown_column"} {
		query := martianGoldenQuery
		if name == "002_unknown_column" {
			query = martianUnknownColumnQuery
		}
		writeCaseFiles(t, filepath.Join(directory, name), map[string]string{
			specFile:                      martianGoldenSpec,
			"migrations/001_items.up.sql": martianGoldenMigration,
			"queries/get_item.sql":        query,
		})
	}

	RunGolden(t, newMartian, directory, WithUpdate(true))
	RunGolden(t, newMartian, directory)
	_, err := os.Stat(filepath.Join(directory, "002_unknown_column", goldenDirectory, "diagnostics.json"))
	require.NoError(t, err)
}

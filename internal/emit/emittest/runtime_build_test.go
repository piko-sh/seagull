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

package emittest

import (
	"errors"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/emit/emittest/internal/pgxrecorder"
	"seagull.sh/seagull/internal/emit/emittest/internal/stubs/pgx"
	"seagull.sh/seagull/internal/sqlmodel"
)

type unreadableFS struct{}

func (unreadableFS) Open(string) (fs.File, error) {
	return nil, errors.New("unreadable")
}

func TestExcludeFailedReadsTheCompilerReport(t *testing.T) {
	t.Parallel()

	first := &lanePackage{name: "p0", caseName: "commands"}
	second := &lanePackage{name: "p1", caseName: "slices"}
	output := "# emittest.local/lane/p1\np1/queries.sql.go:3:1: undefined: sglList\n# emittest.local/lane/p9\nother\n"

	failed := excludeFailed([]*lanePackage{first, second}, output)

	require.Equal(t, []*lanePackage{second}, failed)
	require.Empty(t, first.failure)
	require.Empty(t, second.unavailable, "a package that does not build fails rather than being passed over")
	require.Equal(t, "the generated package does not build with the harness:\np1/queries.sql.go:3:1: undefined: sglList\n", second.failure)
}

func TestBuildLeavingOutFailuresReportsWhatStillFails(t *testing.T) {
	t.Parallel()

	const (
		firstReport  = "# emittest.local/lane/p0\np0/a.go:1:1: undefined: x\n"
		secondReport = "# emittest.local/lane/p1\np1/a.go:1:1: undefined: y\n"
		mainReport   = "# emittest.local/lane\n./main.go:9:2: undefined: z\n"
	)
	errBuild := errors.New("exit status 1")
	testCases := []struct {
		name             string
		outputs          []string
		writeError       error
		expectedBuilt    []string
		expectedFailure  string
		expectedFailures map[string]string
		expectedAttempts int
	}{
		{
			name:             "a program that builds keeps every package",
			outputs:          []string{""},
			expectedBuilt:    []string{"p0", "p1", "p2"},
			expectedFailure:  "",
			expectedFailures: map[string]string{},
			expectedAttempts: 1,
		},
		{
			name:             "a package the compiler names is left out with its report and the rest build",
			outputs:          []string{firstReport, ""},
			expectedBuilt:    []string{"p1", "p2"},
			expectedFailure:  "",
			expectedFailures: map[string]string{"p0": buildFailurePrefix + "p0/a.go:1:1: undefined: x\n"},
			expectedAttempts: 2,
		},
		{
			name:            "a build that still fails after the last attempt reports its output",
			outputs:         []string{firstReport, secondReport},
			expectedBuilt:   nil,
			expectedFailure: "building the runtime lane program still failed after leaving out 2 packages that do not build with the harness:\n" + secondReport,
			expectedFailures: map[string]string{
				"p0": buildFailurePrefix + "p0/a.go:1:1: undefined: x\n",
				"p1": buildFailurePrefix + "p1/a.go:1:1: undefined: y\n",
			},
			expectedAttempts: maxBuildAttempts,
		},
		{
			name:             "a failure that names no package fails the whole program",
			outputs:          []string{mainReport},
			expectedBuilt:    nil,
			expectedFailure:  "building the runtime lane program failed: exit status 1\n" + mainReport,
			expectedFailures: map[string]string{},
			expectedAttempts: 1,
		},
		{
			name:             "a program that cannot be written fails with the file it names",
			outputs:          []string{""},
			writeError:       errors.New("writing p0/a.go: no space left on device"),
			expectedBuilt:    nil,
			expectedFailure:  "building the runtime lane program failed: writing p0/a.go: no space left on device\n",
			expectedFailures: map[string]string{},
			expectedAttempts: 1,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			included := []*lanePackage{{name: "p0", caseName: "a"}, {name: "p1", caseName: "b"}, {name: "p2", caseName: "c"}}
			everything := slices.Clone(included)
			attempts := 0
			built, failure := buildLeavingOutFailures(included, func([]*lanePackage) (string, error) {
				output := testCase.outputs[attempts]
				attempts++
				switch {
				case testCase.writeError != nil:
					return "", testCase.writeError
				case output == "":
					return "", nil
				default:
					return output, errBuild
				}
			})

			names := make([]string, 0, len(built))
			for _, entry := range built {
				names = append(names, entry.name)
			}
			if testCase.expectedBuilt == nil {
				require.Empty(t, built)
			} else {
				require.Equal(t, testCase.expectedBuilt, names)
			}
			require.Equal(t, testCase.expectedFailure, failure)
			require.Equal(t, testCase.expectedAttempts, attempts)
			failures := map[string]string{}
			for _, entry := range everything {
				if entry.failure != "" {
					failures[entry.name] = entry.failure
				}
			}
			require.Equal(t, testCase.expectedFailures, failures)
		})
	}
}

func TestWritingTheProgramNamesTheFileThatFailed(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		write    func(lane *runtimeLane) error
		expected []string
	}{
		{
			name:     "a file whose directory cannot be made",
			write:    func(lane *runtimeLane) error { return lane.writeFile("p0/queries.sql.go", []byte("package p0\n")) },
			expected: []string{"writing p0/queries.sql.go: "},
		},
		{
			name: "a harness module file that cannot be read",
			write: func(lane *runtimeLane) error {
				return lane.writeModule("modules/m0", "example.com/driver", unreadableFS{})
			},
			expected: []string{"reading . of the harness module example.com/driver: ", "unreadable"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			blocked := filepath.Join(t.TempDir(), "file")
			require.NoError(t, os.WriteFile(blocked, nil, fileMode))
			lane := &runtimeLane{directory: blocked}

			err := testCase.write(lane)

			require.Error(t, err)
			for _, expected := range testCase.expected {
				require.Contains(t, err.Error(), expected)
			}
		})
	}
}

func TestTheLaneLeavesOutAPackageTheCompilerRejects(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("building the program needs the go command; skipped under -short")
	}

	current := &subject{cases: map[string]*caseState{
		"good":   builtCase("package good\n\nvar Answer = 42\n"),
		"broken": builtCase("package broken\n\nvar Answer = undefinedName\n"),
	}}
	good := &lanePackage{subject: current, caseName: "good", name: "p0", source: "_ = p0.Answer\n", imports: map[string]bool{}}
	broken := &lanePackage{subject: current, caseName: "broken", name: "p1", source: "_ = p1.Answer\n", imports: map[string]bool{}}
	lane := &runtimeLane{harness: DatabaseSQL(), directory: t.TempDir()}

	binary, built, failure := lane.build(t.Context(), []*lanePackage{good, broken})

	require.Empty(t, failure)
	require.Equal(t, []*lanePackage{good}, built)
	require.FileExists(t, binary)
	require.Empty(t, good.failure)
	require.True(t, strings.HasPrefix(broken.failure, buildFailurePrefix), broken.failure)
	require.Contains(t, broken.failure, "undefined: undefinedName")
}

func builtCase(source string) *caseState {
	state := &caseState{result: &caseResult{generated: &app.Result{Files: []sqlmodel.GeneratedFile{{Name: "answer.go", Content: []byte(source)}}}}}
	state.generateOnce.Do(func() {})
	return state
}

func TestStubVersionMatchesTheModulesMajorVersion(t *testing.T) {
	t.Parallel()

	require.Equal(t, "v2.0.0", stubVersion("github.com/ClickHouse/clickhouse-go/v2"))
	require.Equal(t, "v0.0.0", stubVersion("github.com/example/driver"))
	require.Equal(t, "v0.0.0", stubVersion("github.com/example/driver/v1"))
}

func TestProgramSourceCallsEveryPackage(t *testing.T) {
	t.Parallel()

	included := []*lanePackage{
		{name: "p0", source: "queries := p0.New(Connect())\nvar names func(string) string\n_, _ = queries, names\n", imports: map[string]bool{"time": true}},
		{name: "p1", source: "queries := p1.New(Connect())\n_ = queries\n", imports: map[string]bool{}},
	}

	source, err := programSource(included)

	require.NoError(t, err)
	parsed, err := parser.ParseFile(token.NewFileSet(), "main.go", source, parser.ImportsOnly)
	require.NoError(t, err)
	imports := make([]string, 0, len(parsed.Imports))
	for _, specification := range parsed.Imports {
		imports = append(imports, specification.Path.Value)
	}
	require.Contains(t, imports, `"time"`)
	require.Contains(t, imports, `"emittest.local/lane/p0"`)
	require.Contains(t, imports, `"emittest.local/lane/p1"`)
	require.Contains(t, string(source), "emittestPackagep0(ctx, encoder)\n\temittestPackagep1(ctx, encoder)")
}

func TestGoVersionNamesALanguageVersion(t *testing.T) {
	t.Parallel()

	require.Regexp(t, regexp.MustCompile(`^[0-9]+\.[0-9]+$`), goVersion())
}

func TestMarkedValuesTakeTheDialectsPositionalPlaceholders(t *testing.T) {
	t.Parallel()

	numbered := dialect.PlaceholderSigil{Prefix: ":", Kind: dialect.PlaceholderNumbered}
	anonymous := dialect.PlaceholderSigil{Prefix: "%s", Kind: dialect.PlaceholderAnonymous}
	named := dialect.PlaceholderSigil{Prefix: "@", Kind: dialect.PlaceholderNamed}
	mark := recordedValueMark
	testCases := []struct {
		name         string
		placeholders dialect.Placeholders
		sql          string
		want         string
	}{
		{name: "a numbered form numbers each value", placeholders: dialect.Placeholders{Execution: numbered},
			sql: "INSERT INTO t (a, b) VALUES (" + mark + "1, " + mark + "2)", want: "INSERT INTO t (a, b) VALUES (:1, :2)"},
		{name: "an anonymous form writes the same placeholder for each", placeholders: dialect.Placeholders{Execution: anonymous},
			sql: "INSERT INTO t (a, b) VALUES (" + mark + "1, " + mark + "2)", want: "INSERT INTO t (a, b) VALUES (%s, %s)"},
		{name: "a named form uses its positional one", placeholders: dialect.Placeholders{Execution: named, Positional: numbered},
			sql: "INSERT INTO t (a) VALUES (" + mark + "1)", want: "INSERT INTO t (a) VALUES (:1)"},
		{name: "SQL without marks is kept", placeholders: dialect.Placeholders{Execution: numbered},
			sql: "SELECT 1", want: "SELECT 1"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			outcome := &laneOutcome{Statements: []laneStatement{{SQL: testCase.sql}}}
			info := &dialect.Info{Placeholders: testCase.placeholders}

			writeMarkedPlaceholders(outcome, info)

			require.Equal(t, testCase.want, outcome.Statements[0].SQL)
		})
	}
}

func TestACopyFromRowRecordedByThePgxHarnessTakesTheDialectsPlaceholders(t *testing.T) {
	t.Parallel()

	connection := &pgxrecorder.Connection{}
	rows := [][]any{{int64(1), "a"}}
	_, err := connection.CopyFrom(t.Context(), pgx.Identifier{"audit", "items"}, []string{"id", "kind"},
		pgx.CopyFromSlice(len(rows), func(index int) ([]any, error) { return rows[index], nil }))
	require.NoError(t, err)
	recorded := pgxrecorder.Take()
	require.Len(t, recorded, 1)
	outcome := &laneOutcome{Statements: []laneStatement{{SQL: recorded[0].SQL}}}
	info := &dialect.Info{Placeholders: dialect.Placeholders{Execution: dialect.PlaceholderSigil{Prefix: ":", Kind: dialect.PlaceholderNumbered}}}

	writeMarkedPlaceholders(outcome, info)

	require.Equal(t, "INSERT INTO audit.items (id, kind) VALUES (:1, :2)", outcome.Statements[0].SQL)
}

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

package harness

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
)

const (
	suitesDirectory     = "../.."
	repositoryManifest  = "../../../../go.mod"
	standInDriverModule = "golang.org/x/mod"
	standInRunner       = `package main
import (
	"fmt"
	"case_runner/db"
)
var _ = db.New
func main() {
	fmt.Printf("{\"driver\": %q, \"rows\": [1, 2]}\n", databaseDriver)
}
`
	standInGolden = "{\n  \"driver\": \"semver\",\n  \"rows\": [\n    1,\n    2\n  ]\n}\n"
)

func TestDiagnosticProblemsReportsErrorsAndUnexpectedCodes(t *testing.T) {
	t.Parallel()

	warning := seagull.SourceError{Code: seagull.CodeAliasPrefix, Severity: seagull.SeverityWarning, Message: "an alias"}
	failure := seagull.SourceError{Code: seagull.CodeParseError, Severity: seagull.SeverityError, Message: "cannot parse"}
	tests := []struct {
		expected    *[]string
		name        string
		diagnostics []seagull.SourceError
		want        []string
	}{
		{name: "no expectation accepts warnings", expected: nil, diagnostics: []seagull.SourceError{warning}, want: nil},
		{name: "an error diagnostic always fails", expected: nil, diagnostics: []seagull.SourceError{failure},
			want: []string{"generation reported error Q010: cannot parse"}},
		{name: "an empty expectation pins no diagnostics", expected: new([]string{}), diagnostics: []seagull.SourceError{warning},
			want: []string{"generation reported diagnostics [Q050], the spec expects []"}},
		{name: "the expected codes match in any order", expected: &[]string{"Q050", "Q050"}, diagnostics: []seagull.SourceError{warning, warning}, want: nil},
		{name: "a missing code is reported", expected: &[]string{"Q050"}, diagnostics: nil,
			want: []string{"generation reported diagnostics [], the spec expects [Q050]"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, DiagnosticProblems(test.expected, test.diagnostics))
		})
	}
}

func TestLoadSpecReadsTheCaseSpec(t *testing.T) {
	t.Parallel()

	spec := LoadSpec(t, "testdata/case")

	require.Equal(t, "A case that pins its diagnostics.", spec.Description)
	require.Equal(t, "pgx", spec.Emitter)
	require.NotNil(t, spec.ExpectedDiagnostics)
	require.Empty(t, *spec.ExpectedDiagnostics)
	require.True(t, spec.Skip)
	require.Equal(t, "the engine is not ready", spec.SkipReason)
}

func TestDecodeSpecRefusesAKeyTheSpecDoesNotDefine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		content     string
		wantError   string
		wantTrailer bool
	}{
		{name: "a known key decodes", content: `{"description": "d", "expectedDiagnostics": ["Q050"]}`},
		{name: "a singular misspelling is refused", content: `{"expectedDiagnostic": ["Q050"]}`, wantError: `unknown field "expectedDiagnostic"`},
		{name: "a snake case misspelling is refused", content: `{"expected_diagnostics": []}`, wantError: `unknown field "expected_diagnostics"`},
		{name: "the dialect goldens' count key is refused", content: `{"expectedDiagnosticCount": 1}`, wantError: `unknown field "expectedDiagnosticCount"`},
		{name: "an unknown key inside a type override is refused", content: `{"typeOverrides": [{"goTyp": "string"}]}`, wantError: `unknown field "goTyp"`},
		{name: "a second document is refused", content: `{"description": "d"} {}`, wantTrailer: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := decodeSpec([]byte(test.content))
			switch {
			case test.wantTrailer:
				require.ErrorIs(t, err, errTrailingData)
			case test.wantError != "":
				require.ErrorContains(t, err, test.wantError)
			default:
				require.NoError(t, err)
			}
		})
	}
}

func TestEverySuiteCaseSpecDecodesStrictly(t *testing.T) {
	t.Parallel()

	specs, err := filepath.Glob(filepath.Join(suitesDirectory, "*", defaultTestdata, "*", specFile))
	require.NoError(t, err)
	require.NotEmpty(t, specs, "no suite case was found under %s", suitesDirectory)
	for _, path := range specs {
		content, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		_, decodeErr := decodeSpec(content)
		assert.NoError(t, decodeErr, "decoding %s", path)
	}
}

func TestCaseNamesListsTheDirectoriesHoldingASpecInOrder(t *testing.T) {
	t.Parallel()

	testdata := t.TempDir()
	for _, directory := range []string{"010_last", "002_second", "001_first", "003_without_spec", "004_nested/005_inner"} {
		require.NoError(t, os.MkdirAll(filepath.Join(testdata, filepath.FromSlash(directory)), 0o750))
	}
	for _, spec := range []string{"010_last", "002_second", "001_first", "004_nested/005_inner"} {
		require.NoError(t, os.WriteFile(filepath.Join(testdata, filepath.FromSlash(spec), specFile), []byte("{}"), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(testdata, "README.md"), []byte("notes"), 0o600))

	tests := []struct {
		name     string
		testdata string
		want     []string
	}{
		{name: "case directories in name order", testdata: testdata, want: []string{"001_first", "002_second", "010_last"}},
		{name: "an empty directory holds no cases", testdata: t.TempDir(), want: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, caseNames(t, test.testdata))
		})
	}
}

func TestEmitterOptionsSelectsTheEmitterTheSpecNames(t *testing.T) {
	t.Parallel()

	emitters := map[string]seagull.Option{"pgx": seagull.WithLogger(nil), "native": seagull.WithLogger(nil)}
	tests := []struct {
		name        string
		emitter     string
		wantError   string
		wantOptions int
	}{
		{name: "no emitter selects database/sql", emitter: "", wantOptions: 0},
		{name: "database/sql needs no option", emitter: databaseSQLEmitter, wantOptions: 0},
		{name: "a known emitter selects its option", emitter: "pgx", wantOptions: 1},
		{name: "an unknown emitter lists the accepted ones", emitter: "pgxx", wantError: `"pgxx"; this suite accepts [database/sql native pgx]`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			options, err := emitterOptions(emitters, test.emitter)
			if test.wantError != "" {
				require.ErrorIs(t, err, errUnknownEmitter)
				require.ErrorContains(t, err, test.wantError)
				return
			}
			require.NoError(t, err)
			require.Len(t, options, test.wantOptions)
		})
	}
}

func TestStopBeforeLeavesAMarginAheadOfTheTestDeadline(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		deadline time.Time
		want     time.Time
		name     string
	}{
		{name: "a long run keeps the full margin", deadline: now.Add(time.Hour), want: now.Add(time.Hour - testDeadlineMargin)},
		{name: "a short run keeps a tenth of what is left", deadline: now.Add(5 * time.Minute), want: now.Add(5*time.Minute - 30*time.Second)},
		{name: "a deadline already passed keeps no margin", deadline: now.Add(-time.Second), want: now.Add(-time.Second)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, stopBefore(test.deadline, now))
		})
	}
}

func TestCaseContextEndsBeforeTheCaseTimeoutAndTheTestDeadline(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
	}{
		{name: "a case context has a deadline within its limits"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			started := time.Now()
			ctx, finish := caseContext(t)
			defer finish()

			deadline, hasDeadline := ctx.Deadline()
			require.True(t, hasDeadline)
			assert.False(t, deadline.After(started.Add(caseTimeout+time.Minute)), "the case timeout bounds the case")
			if testDeadline, hasTestDeadline := t.Deadline(); hasTestDeadline {
				assert.True(t, deadline.Before(testDeadline), "the case ends before go test's deadline")
			}
			require.NoError(t, context.Cause(ctx))
		})
	}
}

func TestRunGeneratesBuildsRunsAndChecksEveryCase(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a runner with the go command")
	}
	manifest, err := os.ReadFile(repositoryManifest)
	require.NoError(t, err)
	version, found, err := RequiredVersion(manifest, standInDriverModule)
	require.NoError(t, err)
	require.True(t, found, "the repository requires %s", standInDriverModule)

	suiteDirectory := t.TempDir()
	writeFile(t, filepath.Join(suiteDirectory, moduleFile), []byte(RunnerModuleFile("suite", map[string]string{standInDriverModule: version})))
	writeStandInCase(t, filepath.Join(suiteDirectory, defaultTestdata, "001_items"), true)
	writeFile(t, filepath.Join(suiteDirectory, defaultTestdata, "002_skipped", specFile), []byte(`{"description": "a skipped case", "skip": true}`))
	writeStandInCase(t, filepath.Join(suiteDirectory, "recording", "001_items"), false)
	t.Chdir(suiteDirectory)

	tests := []struct {
		name     string
		testdata string
		update   bool
	}{
		{name: "the output matches the committed golden", testdata: "", update: false},
		{name: "the output is recorded as the golden", testdata: "recording", update: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			Run(t, &Suite{
				Dialect:          func() dialect.Dialect { return dialect_engine_sqlite.New() },
				Driver:           Driver{Module: standInDriverModule, Import: standInDriverModule + "/semver", Name: "semver"},
				BuildEnvironment: []string{"GOPROXY=off", "GOSUMDB=off"},
				Testdata:         test.testdata,
				Update:           test.update,
			})
		})
	}
	recorded, err := os.ReadFile(filepath.Join(suiteDirectory, "recording", "001_items", "golden", "output.json"))
	require.NoError(t, err)
	require.Equal(t, standInGolden, string(recorded))
}

func writeStandInCase(t *testing.T, caseDirectory string, withGolden bool) {
	t.Helper()

	files := map[string]string{
		specFile:                       `{"description": "a case built and run without a database", "expectedDiagnostics": []}`,
		"migrations/0001_items.up.sql": "CREATE TABLE items (id INTEGER PRIMARY KEY, label TEXT NOT NULL);\n",
		"queries/items.sql":            "-- sgl.query(ListItems, many)\nSELECT id, label FROM items ORDER BY id;\n",
		runnerSource:                   standInRunner,
	}
	if withGolden {
		files["golden/output.json"] = standInGolden
	}
	for name, content := range files {
		writeFile(t, filepath.Join(caseDirectory, filepath.FromSlash(name)), []byte(content))
	}
}

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

package catalogue

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	// q022Warning is one line of sgl vet --format json.
	q022Warning = `{"file":"db/queries/a.sql","database":"a","severity":"warning","code":"Q022","message":"m","line":1,"column":1}`

	// q027Error is one line of sgl vet --format json for an error.
	q027Error = `{"file":"db/queries/a.sql","database":"a","severity":"error","code":"Q027","message":"m","line":2,"column":3}`
)

func TestCompareCodes(t *testing.T) {
	for name, test := range map[string]struct {
		expected   []string
		reported   []string
		missing    []string
		unexpected []string
	}{
		"nothing expected or reported": {},
		"the same codes":               {expected: []string{"Q022"}, reported: []string{"Q022"}},
		"order and repeats ignored": {
			expected: []string{"Q027", "Q009"},
			reported: []string{"Q009", "Q027", "Q009"},
		},
		"a code missing": {
			expected: []string{"Q009", "Q027"},
			reported: []string{"Q009"},
			missing:  []string{"Q027"},
		},
		"a code unexpected": {
			reported:   []string{"Q046", "Q001"},
			unexpected: []string{"Q001", "Q046"},
		},
		"both at once": {
			expected:   []string{"Q050", "Q051"},
			reported:   []string{"Q051", "Q052"},
			missing:    []string{"Q050"},
			unexpected: []string{"Q052"},
		},
		"case matters": {
			expected:   []string{"q022"},
			reported:   []string{"Q022"},
			missing:    []string{"q022"},
			unexpected: []string{"Q022"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			missing, unexpected := CompareCodes(test.expected, test.reported)
			require.Equal(t, test.missing, missing)
			require.Equal(t, test.unexpected, unexpected)
		})
	}
}

func TestVetResultReportsWhatDiffers(t *testing.T) {
	matched := VetResult{Variant: "sqlite/014_runtime_builder", Expected: []string{"Q022"}, Reported: []string{"Q022"}}
	require.False(t, matched.Differs())
	require.Equal(t, "sqlite/014_runtime_builder: reports [Q022] as expected", matched.String())

	differing := VetResult{
		Variant:    "tooling/041_vet_and_diagnostics",
		Expected:   []string{"Q009", "Q027"},
		Reported:   []string{"Q001", "Q009"},
		Missing:    []string{"Q027"},
		Unexpected: []string{"Q001"},
	}
	require.True(t, differing.Differs())
	require.Equal(t, "tooling/041_vet_and_diagnostics: missing Q027; unexpected Q001 (expected_diagnostics [Q009, Q027], sgl vet reported [Q001, Q009])", differing.String())

	failed := VetResult{Variant: "sqlite/001_first_query", Failure: "sgl vet exited 2"}
	require.True(t, failed.Differs())
	require.Equal(t, "sqlite/001_first_query: sgl vet exited 2", failed.String())

	skipped := VetResult{Variant: "embedding/050_schema_from_live_database", Skipped: "no database"}
	require.False(t, skipped.Differs())
	require.Equal(t, "embedding/050_schema_from_live_database: skipped; no database", skipped.String())
}

func TestParseVetJSON(t *testing.T) {
	codes, hasError, err := ParseVetJSON(nil)
	require.NoError(t, err)
	require.Empty(t, codes)
	require.False(t, hasError)

	codes, hasError, err = ParseVetJSON([]byte(q022Warning + "\n" + q022Warning + "\n"))
	require.NoError(t, err)
	require.Equal(t, []string{"Q022", "Q022"}, codes)
	require.False(t, hasError)

	codes, hasError, err = ParseVetJSON([]byte(q022Warning + "\n" + q027Error + "\n"))
	require.NoError(t, err)
	require.Equal(t, []string{"Q022", "Q027"}, codes)
	require.True(t, hasError)

	_, _, err = ParseVetJSON([]byte("db/queries/a.sql:1:1: warning[Q022]: m\n"))
	require.Error(t, err)

	_, _, err = ParseVetJSON([]byte(`{"severity":"error","message":"no code"}` + "\n"))
	require.ErrorContains(t, err, "no code")
}

func TestRunVetTellsFindingsFromFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		runErr    error
		name      string
		output    string
		wantCodes []string
		wantFails bool
	}{
		{name: "clean", output: "", runErr: nil, wantCodes: nil, wantFails: false},
		{name: "a warning", output: q022Warning + "\n", runErr: nil, wantCodes: []string{"Q022"}, wantFails: false},
		{name: "an error, which exits 1", output: q022Warning + "\n" + q027Error + "\n", runErr: exitedWith(1), wantCodes: []string{"Q022", "Q027"}, wantFails: false},
		{name: "exit 1 without an error", output: q022Warning + "\n", runErr: exitedWith(1), wantCodes: nil, wantFails: true},
		{name: "a configuration error", output: "", runErr: exitedWith(2), wantCodes: nil, wantFails: true},
		{name: "an error finding but exit 3", output: q027Error + "\n", runErr: exitedWith(3), wantCodes: nil, wantFails: true},
		{name: "output that is not JSON lines", output: "not json\n", runErr: nil, wantCodes: nil, wantFails: true},
		{name: "sgl that cannot start", output: "", runErr: errors.New("no such file"), wantCodes: nil, wantFails: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var gotDirectory string
			var gotArguments []string
			run := func(_ context.Context, directory string, arguments []string) ([]byte, []byte, error) {
				gotDirectory, gotArguments = directory, arguments
				return []byte(test.output), []byte("the fake sgl's standard error"), test.runErr
			}
			codes, err := runVet(t.Context(), "variant", "broken/sgl.yaml", []string{"go", "run", "."}, run)

			require.Equal(t, "variant", gotDirectory)
			require.Equal(t, []string{"go", "run", ".", "-c", "broken/sgl.yaml", "vet", "--format", "json"}, gotArguments)
			if test.wantFails {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.wantCodes, codes)
		})
	}
}

func TestVariantConfigsListsNestedProjects(t *testing.T) {
	directory := t.TempDir()
	for _, path := range []string{"sgl.yaml", "broken/sgl.yaml", "a/b/sgl.yaml", "db/queries/tasks.sql", "adopt.yaml"} {
		require.NoError(t, os.MkdirAll(filepath.Join(directory, filepath.Dir(path)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(directory, path), []byte("version: 1\n"), 0o644))
	}
	configs, err := variantConfigs(directory)
	require.NoError(t, err)
	require.Equal(t, []string{"sgl.yaml", "a/b/sgl.yaml", "broken/sgl.yaml"}, configs)
}

func TestPrepareOverlayLeavesTheVariantAlone(t *testing.T) {
	source := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(source, "db", "queries"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(source, "change"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(source, "db", "queries", "tasks.sql"), []byte("before\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(source, "change", "tasks.sql"), []byte("after\n"), 0o644))

	target := filepath.Join(t.TempDir(), "tooling", "042_ci_pipeline")
	require.NoError(t, prepareOverlay(source, target, map[string]string{"db/queries/tasks.sql": "change/tasks.sql"}))

	changed, err := os.ReadFile(filepath.Join(target, "db", "queries", "tasks.sql"))
	require.NoError(t, err)
	require.Equal(t, "after\n", string(changed))
	original, err := os.ReadFile(filepath.Join(source, "db", "queries", "tasks.sql"))
	require.NoError(t, err)
	require.Equal(t, "before\n", string(original))

	require.Error(t, prepareOverlay(source, filepath.Join(t.TempDir(), "missing"), map[string]string{"db/queries/tasks.sql": "change/none.sql"}))
}

func TestVetSpecialsNameRealVariants(t *testing.T) {
	variants, err := vetVariants("..")
	require.NoError(t, err)
	require.NotEmpty(t, variants)

	for name, special := range vetSpecials {
		require.Contains(t, variants, name)
		directory := filepath.Join("..", filepath.FromSlash(name))
		if special.ownSGL {
			require.FileExists(t, filepath.Join(directory, "main.go"), "%s is vetted with its own sgl", name)
		}
		for replaced, replacement := range special.overlay {
			require.FileExists(t, filepath.Join(directory, filepath.FromSlash(replaced)), "%s", name)
			require.FileExists(t, filepath.Join(directory, filepath.FromSlash(replacement)), "%s", name)
		}
	}
}

// diagnosticsFixture is the fixture examples directory with two more variants under
// tooling: one whose sgl cannot start and one with no example.yaml.
//
// Returns string which is the examples directory.
func diagnosticsFixture(t *testing.T) string {
	t.Helper()
	return writeExamples(t, map[string]string{
		"tooling/043_broken_sgl/" + configFile:  "version: 1\n",
		"tooling/043_broken_sgl/" + VariantFile: "summary: A broken sgl.yaml.\n",
		"tooling/044_no_manifest/" + configFile: "version: 1\n",
	})
}

// stubVet is a CommandRunner that answers sgl vet for the variants of
// diagnosticsFixture as a real sgl would, and records the arguments of each run.
//
// Takes runs (*[][]string) which receives each run's arguments.
//
// Returns CommandRunner which is the stub.
func stubVet(t *testing.T, runs *[][]string) CommandRunner {
	t.Helper()
	return func(_ context.Context, directory string, arguments []string) ([]byte, []byte, error) {
		*runs = append(*runs, arguments)
		variant := filepath.Base(filepath.Dir(directory)) + "/" + filepath.Base(directory)
		switch variant {
		case "sqlite/001_first_query":
			return []byte(q022Warning + "\n"), nil, nil
		case "postgres/001_first_query":
			return []byte(q027Error + "\n"), []byte("1 error"), exitedWith(1)
		case "embedding/053_custom_sgl_binary":
			return []byte(`{"severity":"warning","code":"Q001"}` + "\n"), nil, nil
		case "tooling/042_ci_pipeline":
			changed, err := os.ReadFile(filepath.Join(directory, "db", "queries", "tasks.sql"))
			if err != nil || string(changed) != "after\n" {
				return nil, []byte("the transcript's change was not applied"), exitedWith(2)
			}
			return []byte(`{"severity":"warning","code":"Q046"}` + "\n"), nil, nil
		}
		return nil, nil, errors.New("no such file")
	}
}

func TestCheckDiagnosticsComparesEachVariantWithWhatSglReports(t *testing.T) {
	t.Parallel()

	examples := diagnosticsFixture(t)
	var runs [][]string
	results, err := CheckDiagnostics(t.Context(), examples, filepath.Join(t.TempDir(), "scratch"), "sgl", "go-tool", stubVet(t, &runs))
	require.NoError(t, err)

	tests := []struct {
		variant        string
		wantFailure    string
		wantExpected   []string
		wantReported   []string
		wantUnexpected []string
		wantSkipped    bool
	}{
		{variant: "embedding/050_schema_from_live_database", wantSkipped: true},
		{variant: "embedding/053_custom_sgl_binary", wantExpected: []string{"Q001"}, wantReported: []string{"Q001"}},
		{variant: "postgres/001_first_query", wantReported: []string{"Q027"}, wantUnexpected: []string{"Q027"}},
		{variant: "sqlite/001_first_query", wantExpected: []string{"Q022"}, wantReported: []string{"Q022"}},
		{variant: "tooling/042_ci_pipeline", wantExpected: []string{"Q046"}, wantReported: []string{"Q046"}},
		{variant: "tooling/043_broken_sgl", wantFailure: "running sgl -c sgl.yaml vet --format json: no such file"},
		{variant: "tooling/044_no_manifest", wantFailure: VariantFile},
	}
	require.Len(t, results, len(tests))
	for index, test := range tests {
		t.Run(test.variant, func(t *testing.T) {
			result := results[index]
			require.Equal(t, test.variant, result.Variant)
			require.Equal(t, test.wantSkipped, result.Skipped != "")
			require.Contains(t, result.Failure, test.wantFailure)
			require.Equal(t, test.wantFailure != "", result.Failure != "")
			require.Equal(t, test.wantExpected, result.Expected)
			require.Equal(t, test.wantReported, result.Reported)
			require.Equal(t, test.wantUnexpected, result.Unexpected)
			require.Empty(t, result.Missing)
		})
	}

	require.Contains(t, runs, []string{"go-tool", "run", ".", "-c", "broken/sgl.yaml", "vet", "--format", "json"})
	require.Contains(t, runs, []string{"sgl", "-c", "sgl.yaml", "vet", "--format", "json"})
	original, err := os.ReadFile(filepath.Join(examples, "tooling", "042_ci_pipeline", "db", "queries", "tasks.sql"))
	require.NoError(t, err)
	require.Equal(t, "before\n", string(original))
}

func TestCheckDiagnosticsRefusesWhatItCannotCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want     error
		prepare  func(t *testing.T, examples, scratch string)
		name     string
		wantText string
	}{
		{
			name:     "a scratch directory that is not empty",
			prepare:  func(t *testing.T, _, scratch string) { writeFiles(t, scratch, map[string]string{"left-over": "x"}) },
			want:     ErrNotEmpty,
			wantText: "",
		},
		{
			name: "a special variant that does not exist",
			prepare: func(t *testing.T, examples, _ string) {
				require.NoError(t, os.RemoveAll(filepath.Join(examples, "tooling", "042_ci_pipeline")))
			},
			want:     nil,
			wantText: "vetSpecials names tooling/042_ci_pipeline",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			examples, scratch := diagnosticsFixture(t), t.TempDir()
			test.prepare(t, examples, scratch)
			var runs [][]string
			_, err := CheckDiagnostics(t.Context(), examples, scratch, "sgl", "go", stubVet(t, &runs))
			require.Error(t, err)
			if test.want != nil {
				require.ErrorIs(t, err, test.want)
			}
			require.ErrorContains(t, err, test.wantText)
			require.Empty(t, runs)
		})
	}
}

func TestCheckDiagnosticsStopsBeforeTheNextVariantOnceCancelled(t *testing.T) {
	t.Parallel()

	stopped := errors.New("the check was stopped")
	tests := []struct {
		name     string
		before   bool
		wantRuns int
	}{
		{name: "cancelled before the first variant", before: true, wantRuns: 0},
		{name: "cancelled during the first vetted variant", before: false, wantRuns: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			if test.before {
				cancel(stopped)
			}
			var runs [][]string
			stub := stubVet(t, &runs)
			run := func(ctx context.Context, directory string, arguments []string) ([]byte, []byte, error) {
				defer cancel(stopped)
				return stub(ctx, directory, arguments)
			}
			results, err := CheckDiagnostics(ctx, diagnosticsFixture(t), t.TempDir(), "sgl", "go", run)
			require.ErrorIs(t, err, stopped)
			require.Nil(t, results)
			require.Len(t, runs, test.wantRuns)
		})
	}
}

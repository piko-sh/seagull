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

package provenance

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func newRepository(t *testing.T) string {
	t.Helper()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	require.NoError(t, exec.Command("git", "init", "-q", root).Run())
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pkg", "testdata", "fuzz", "FuzzThing"), 0o700))
	files := map[string]string{
		"go.mod":                           "module thing\n",
		"go.sum":                           "",
		"pkg/thing.go":                     "package pkg\n",
		"pkg/testdata/fuzz/FuzzThing/seed": "go test fuzz v1\nstring(\"a\")\n",
		".cache/ignored":                   "cache\n",
		"pkg/testdata/fuzz/FuzzThing/sgl-reduce-1-1": "scratch\n",
	}
	for path, content := range files {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(content), 0o600))
	}
	return root
}

func TestMatchesSkipsOnlyAWholePassWithTheSameProvenance(t *testing.T) {
	if testing.Short() {
		t.Skip("runs git and go")
	}
	t.Parallel()

	tests := []struct {
		name        string
		status      string
		before      string
		change      func(t *testing.T, root string)
		budget      bool
		wantMatches bool
	}{
		{name: "a passing whole run with the same provenance", status: "passed", budget: true, wantMatches: true},
		{name: "a failed run", status: "failed", budget: true},
		{name: "a partial budget", status: "passed", budget: false},
		{name: "a source change during the run", status: "passed", budget: true, before: "an older fingerprint"},
		{
			name: "a source edit after the run", status: "passed", budget: true,
			change: func(t *testing.T, root string) {
				appendTo(t, filepath.Join(root, "pkg", "thing.go"), "// edited\n")
			},
		},
		{
			name: "a corpus change after the run", status: "passed", budget: true,
			change: func(t *testing.T, root string) {
				appendTo(t, filepath.Join(root, "pkg", "testdata", "fuzz", "FuzzThing", "new"), "go test fuzz v1\nstring(\"b\")\n")
			},
		},
		{
			name: "an edit to a cache", status: "passed", budget: true, wantMatches: true,
			change: func(t *testing.T, root string) {
				appendTo(t, filepath.Join(root, ".cache", "ignored"), "more\n")
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			root := newRepository(t)
			target := Target{
				Root: root, Go: "go", Name: "thing/fuzz", Module: ".", Package: "./pkg",
				Function: "FuzzThing", Command: "campaign", Budget: "10m/500", Workers: "1",
			}
			file := filepath.Join(t.TempDir(), "record.json")
			require.ErrorIs(t, Matches(ctx, target, file), ErrMismatch, "a missing record runs the target")
			before := testCase.before
			if before == "" {
				fingerprint, err := SourceFingerprint(ctx, root)
				require.NoError(t, err)
				before = fingerprint
			}

			outcome := Outcome{Status: testCase.status, SourceBefore: before, Engine: "", BudgetCompleted: testCase.budget}
			require.NoError(t, Record(ctx, target, outcome, file))
			if testCase.change != nil {
				testCase.change(t, root)
			}

			err := Matches(ctx, target, file)
			if testCase.wantMatches {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrMismatch)
		})
	}
}

func TestSourceUnchangedReadsTheRecord(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		content   string
		wantError bool
	}{
		{name: "unchanged", content: `{"source_unchanged": true}`},
		{name: "changed", content: `{"source_unchanged": false}`, wantError: true},
		{name: "not JSON", content: `{`, wantError: true},
		{name: "not an object", content: `[]`, wantError: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			file := filepath.Join(t.TempDir(), "record.json")
			require.NoError(t, os.WriteFile(file, []byte(testCase.content), 0o600))

			err := SourceUnchanged(file)
			if testCase.wantError {
				require.ErrorIs(t, err, ErrMismatch)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestSourceFingerprintFailsOutsideARepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}

	directory := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(directory))

	_, err := SourceFingerprint(context.Background(), directory)

	require.Error(t, err)
}

func appendTo(t *testing.T, path string, text string) {
	t.Helper()

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = file.WriteString(text)
	require.NoError(t, err)
	require.NoError(t, file.Close())
}

func TestSourceFingerprintIgnoresCachesAndReducerScratchInputs(t *testing.T) {
	if testing.Short() {
		t.Skip("runs git")
	}
	t.Parallel()

	ctx := context.Background()
	root := newRepository(t)
	before, err := SourceFingerprint(ctx, root)
	require.NoError(t, err)

	appendTo(t, filepath.Join(root, ".cache", "ignored"), "more\n")
	appendTo(t, filepath.Join(root, "pkg", "testdata", "fuzz", "FuzzThing", "sgl-reduce-1-1"), "more\n")
	after, err := SourceFingerprint(ctx, root)
	require.NoError(t, err)
	require.Equal(t, before, after)

	appendTo(t, filepath.Join(root, "pkg", "thing.go"), "// edited\n")
	edited, err := SourceFingerprint(ctx, root)
	require.NoError(t, err)
	require.NotEqual(t, before, edited)
}

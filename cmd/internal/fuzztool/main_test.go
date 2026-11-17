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

//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/internal/fuzztool/supervise"
)

func TestRunReturnsTheStatusTheRunnerExpects(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	record := filepath.Join(directory, "record.json")
	require.NoError(t, os.WriteFile(record, []byte(`{"source_unchanged": true}`), 0o600))
	changed := filepath.Join(directory, "changed.json")
	require.NoError(t, os.WriteFile(changed, []byte(`{"source_unchanged": false}`), 0o600))
	manifest := filepath.Join(directory, "manifest.json")
	require.NoError(t, os.WriteFile(manifest, []byte(`{"name": "Bad Name"}`), 0o600))

	tests := []struct {
		name       string
		wantStdout string
		arguments  []string
		wantStatus int
	}{
		{name: "no subcommand", arguments: nil, wantStatus: statusUsage},
		{name: "an unknown subcommand", arguments: []string{"absent"}, wantStatus: statusUsage},
		{name: "disk usage without a directory", arguments: []string{"disk-usage"}, wantStatus: statusUsage},
		{name: "disk usage of a missing directory", arguments: []string{"disk-usage", filepath.Join(directory, "missing")}, wantStatus: statusFailed},
		{name: "an unknown flag", arguments: []string{"intake", "--absent", "x"}, wantStatus: statusUsage},
		{name: "a missing required flag", arguments: []string{"intake", "--source", directory}, wantStatus: statusUsage},
		{
			name:       "a manifest with a bad name",
			arguments:  []string{"intake", "--source", directory, "--manifest", manifest, "--output-root", directory},
			wantStatus: statusUsage,
		},
		{name: "a positional argument", arguments: []string{"provenance", "unchanged", "--file", record, "extra"}, wantStatus: statusUsage},
		{name: "an unchanged source", arguments: []string{"provenance", "unchanged", "--file", record}, wantStatus: 0},
		{name: "a changed source", arguments: []string{"provenance", "unchanged", "--file", changed}, wantStatus: statusFailed},
		{name: "a missing record", arguments: []string{"provenance", "unchanged", "--file", filepath.Join(directory, "absent")}, wantStatus: statusFailed},
		{name: "an unknown provenance subcommand", arguments: []string{"provenance", "absent"}, wantStatus: statusUsage},
		{
			name: "a budget flag that is not a boolean",
			arguments: []string{"provenance", "record", "--root", directory, "--target", "t", "--module", ".", "--package", ".",
				"--function", "F", "--command", "run", "--budget", "1s/1", "--workers", "1", "--file", record,
				"--status", "passed", "--budget-completed", "maybe", "--source-before", "x"},
			wantStatus: statusUsage,
		},
		{
			name: "a reduce budget that is not positive",
			arguments: []string{"reduce", "--module", ".", "--package", ".", "--function", "F", "--input", record,
				"--output", filepath.Join(directory, "out"), "--attempts", "0"},
			wantStatus: statusUsage,
		},
		{name: "a command without its environment", arguments: []string{"command", "true"}, wantStatus: supervise.StatusFailClosed},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			status := run(context.Background(), testCase.arguments, streams{stdout: &stdout, stderr: &stderr})
			require.Equal(t, testCase.wantStatus, status, stderr.String())
		})
	}
}

func TestRunIntakeWritesUnderTheSuiteName(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	source := filepath.Join(directory, "suite")
	require.NoError(t, os.Mkdir(source, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(source, "a.sql"), []byte("CREATE TABLE t (a int); SELECT a FROM t;"), 0o600))
	manifest := filepath.Join(directory, "manifest.json")
	require.NoError(t, os.WriteFile(manifest, []byte(`{"name": "fixture", "licence": "MIT", "revision": "1",
		"upstream": "u", "format": "sql", "dialects": ["sqlite"]}`), 0o600))
	arguments := []string{"intake", "--source", source, "--manifest", manifest, "--output-root", filepath.Join(directory, "external")}
	var stdout, stderr bytes.Buffer

	status := run(context.Background(), arguments, streams{stdout: &stdout, stderr: &stderr})

	require.Equal(t, 0, status, stderr.String())
	require.Equal(t, "admitted 1 files and 1 seeds, rejected 0 files, skipped 0 files beyond the file limit and 0 duplicate seeds\n", stdout.String())
	require.FileExists(t, filepath.Join(directory, "external", "fixture", "seeds.jsonl"))
	require.Equal(t, statusUsage, run(context.Background(), arguments, streams{stdout: &stdout, stderr: &stderr}), "an existing output is refused")
}

func TestCommandConfigReadsTheCampaignEnvironment(t *testing.T) {
	t.Parallel()

	complete := map[string]string{
		"SEAGULL_FUZZ_MANAGED_ROOT": "/managed", "SEAGULL_FUZZ_METADATA_DIR": "/metadata", "SEAGULL_FUZZ_MIN_FREE_MB": "2",
		"SEAGULL_FUZZ_MAX_DISK_MB": "3", "GOMODCACHE": "/modules", "TMPDIR": "/temporary",
	}
	tests := []struct {
		name      string
		override  map[string]string
		missing   string
		wantError bool
	}{
		{name: "a complete environment"},
		{name: "a missing variable", missing: "TMPDIR", wantError: true},
		{name: "a limit that is not a number", override: map[string]string{"SEAGULL_FUZZ_MAX_DISK_MB": "lots"}, wantError: true},
		{name: "a negative limit", override: map[string]string{"SEAGULL_FUZZ_MIN_FREE_MB": "-1"}, wantError: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			lookup := func(name string) (string, bool) {
				if name == testCase.missing {
					return "", false
				}
				if value, found := testCase.override[name]; found {
					return value, true
				}
				value, found := complete[name]
				return value, found
			}

			config, err := commandConfig(lookup)
			if testCase.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, supervise.Config{
				ManagedRoot: "/managed", MetadataDirectory: "/metadata", Locations: []string{"/modules", "/temporary"},
				MinimumFreeBytes: 2 << 20, MaximumManagedBytes: 3 << 20,
			}, config)
		})
	}
}

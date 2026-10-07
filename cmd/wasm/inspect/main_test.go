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

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/wasm/internal/session"
)

func writeProject(t *testing.T, directory string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		target := filepath.Join(directory, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
		require.NoError(t, os.WriteFile(target, []byte(content), 0o644))
	}
}

func TestRunNeedsExactlyOneMode(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		arguments []string
		message   string
	}{
		{name: "no mode", arguments: nil, message: "choose exactly one of"},
		{name: "two modes", arguments: []string{"-codes", "-dialects"}, message: "choose exactly one of"},
		{name: "stray argument", arguments: []string{"-codes", "extra"}, message: `unexpected argument "extra"`},
		{name: "out with starters", arguments: []string{"-starters", "x", "-out", "y"}, message: "-out does not apply to -starters"},
		{name: "unknown flag", arguments: []string{"-nope"}, message: "flag provided but not defined"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer

			status := run(t.Context(), testCase.arguments, &stdout, &stderr)

			require.Equal(t, exitUsage, status)
			require.Contains(t, stderr.String(), testCase.message)
			require.Empty(t, stdout.String())
		})
	}
}

func TestRunPrintsDialects(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer

	status := run(t.Context(), []string{"-dialects"}, &stdout, &stderr)

	require.Equal(t, exitOK, status, stderr.String())
	var listed []session.DialectView
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &listed))
	require.NotEmpty(t, listed)
}

func TestBatchWritesAMirrorTree(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	project := filepath.Join(root, "projects", "tasks")
	files, err := session.Starter("sqlite")
	require.NoError(t, err)
	writeProject(t, project, files)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "projects", ".hidden", "db"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "projects", ".hidden", "sgl.yaml"), []byte("version: 7\n"), 0o644))
	out := filepath.Join(root, "out")
	var stdout, stderr bytes.Buffer

	status := run(t.Context(), []string{"-batch", filepath.Join(root, "projects"), "-out", out}, &stdout, &stderr)

	require.Equal(t, exitOK, status, stderr.String())
	data, err := os.ReadFile(filepath.Join(out, "tasks", inspectFile))
	require.NoError(t, err)
	var response session.Response
	require.NoError(t, json.Unmarshal(data, &response))
	require.True(t, response.OK, response.Error)
	require.Zero(t, response.DurationMS)
	require.Empty(t, response.Stages)
	require.NoFileExists(t, filepath.Join(project, inspectFile))
	require.Contains(t, stderr.String(), "3 files, 3 queries, 0 errors")
}

func TestStartersWritesOneFilePerDialect(t *testing.T) {
	t.Parallel()

	out := t.TempDir()
	var stdout, stderr bytes.Buffer

	status := run(t.Context(), []string{"-starters", out}, &stdout, &stderr)

	require.Equal(t, exitOK, status, stderr.String())
	listed, err := session.Dialects()
	require.NoError(t, err)
	entries, err := os.ReadDir(out)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	for _, dialect := range listed {
		require.True(t, slices.Contains(names, dialect.Name+".json"), dialect.Name)
	}
	var starter starterView
	data, err := os.ReadFile(filepath.Join(out, "postgres.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &starter))
	require.Equal(t, "postgres", starter.Dialect)
	require.Len(t, starter.Files, 5)
	require.True(t, starter.Response.OK, starter.Response.Error)
}

func TestBatchWritesEachProjectsInspectJSONBesideIt(t *testing.T) {
	t.Parallel()

	starter, err := session.Starter("sqlite")
	require.NoError(t, err)
	testCases := []struct {
		files    map[string]string
		name     string
		flags    []string
		messages []string
		status   int
		timed    bool
	}{
		{
			name: "a project the playground runs", files: starter, flags: nil,
			messages: []string{"3 files, 3 queries, 0 errors"}, status: exitOK, timed: false,
		},
		{
			name: "a project the playground runs, with its timings kept", files: starter, flags: []string{"-timings"},
			messages: []string{"3 files, 3 queries, 0 errors"}, status: exitOK, timed: true,
		},
		{
			name: "a project the playground cannot run",
			files: map[string]string{
				"sgl.yaml":         "version: 1\ndatabases:\n  - name: app\n    dialect: sqlite\n    go:\n      driver: pgx\n",
				"db/queries/a.sql": "",
			},
			flags:    nil,
			messages: []string{`failed: database "app": go.driver pgx needs`, "inspect: the playground could not run"}, status: exitFailure, timed: false,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			project := filepath.Join(root, "app")
			writeProject(t, project, testCase.files)
			var stdout, stderr bytes.Buffer

			status := run(t.Context(), append([]string{"-batch", root}, testCase.flags...), &stdout, &stderr)

			require.Equal(t, testCase.status, status, stderr.String())
			for _, message := range testCase.messages {
				require.Contains(t, stderr.String(), message)
			}
			data, err := os.ReadFile(filepath.Join(project, inspectFile))
			require.NoError(t, err)
			var response session.Response
			require.NoError(t, json.Unmarshal(data, &response))
			require.Equal(t, testCase.timed, response.DurationMS > 0)
			require.Equal(t, testCase.timed, len(response.Stages) > 0)
		})
	}
}

func TestRunWritesTheDialectsToAFileWhenAsked(t *testing.T) {
	t.Parallel()

	out := filepath.Join(t.TempDir(), "nested", "dialects.json")
	var stdout, stderr bytes.Buffer

	status := run(t.Context(), []string{"-dialects", "-out", out}, &stdout, &stderr)

	require.Equal(t, exitOK, status, stderr.String())
	require.Empty(t, stdout.String())
	data, err := os.ReadFile(out)
	require.NoError(t, err)
	var listed []session.DialectView
	require.NoError(t, json.Unmarshal(data, &listed))
	require.NotEmpty(t, listed)
}

func TestRunReportsWhatItCouldNotDo(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	regularFile := filepath.Join(root, "file")
	require.NoError(t, os.WriteFile(regularFile, []byte("not a directory"), 0o644))
	unreadable := filepath.Join(root, "unreadable")
	writeProject(t, unreadable, map[string]string{"sgl.yaml": "version: 7\n", "db/queries/a.sql": ""})
	testCases := []struct {
		name      string
		arguments []string
	}{
		{name: "a batch directory that does not exist", arguments: []string{"-batch", filepath.Join(root, "missing")}},
		{name: "a project whose configuration cannot be read", arguments: []string{"-batch", unreadable}},
		{name: "an output file under a regular file", arguments: []string{"-dialects", "-out", filepath.Join(regularFile, "dialects.json")}},
		{name: "starters written into a regular file", arguments: []string{"-starters", regularFile}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer

			status := run(t.Context(), testCase.arguments, &stdout, &stderr)

			require.Equal(t, exitFailure, status)
			require.True(t, strings.HasPrefix(stderr.String(), "inspect: "), stderr.String())
			require.Empty(t, stdout.String())
		})
	}
}

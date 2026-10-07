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

package facade_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit"
)

const repositoryDirectory = "../.."

func TestDocumentationWorkflows(t *testing.T) {
	testCases := []struct {
		arguments       func(t *testing.T, project string) []string
		name            string
		page            string
		expectOutput    []string
		publishesModels bool
		reportsFailures bool
	}{
		{
			name:            "generate",
			page:            "generate-from-go",
			arguments:       projectAndOutputArguments,
			expectOutput:    nil,
			publishesModels: true,
			reportsFailures: true,
		},
		{
			name:            "vet-catalogue",
			page:            "vet-catalogue",
			arguments:       projectArguments,
			expectOutput:    []string{"query ListItems: 0 parameters, 2 output columns", "schema \"main\": 1 tables"},
			publishesModels: false,
			reportsFailures: true,
		},
		{
			name:            "custom-source",
			page:            "custom-schema-source",
			arguments:       outputArguments,
			expectOutput:    []string{"source catalogue preserved"},
			publishesModels: true,
			reportsFailures: false,
		},
		{
			name:            "observe",
			page:            "observe-generator",
			arguments:       projectArguments,
			expectOutput:    nil,
			publishesModels: false,
			reportsFailures: true,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			program := documentationExample(t, "how-to/"+testCase.page+".md", testCase.name, "go")
			binary := documentationBinary(t, program)
			project := documentationProject(t)
			arguments := testCase.arguments(t, project)

			output, err := documentationCommand(t.Context(), binary, project, arguments)
			require.NoError(t, err, "running Markdown example:\n%s", output)
			for _, expected := range testCase.expectOutput {
				require.Contains(t, output, expected, "the example's output")
			}
			if testCase.publishesModels {
				_, err := os.Stat(filepath.Join(project, "internal/store/models.go"))
				require.NoError(t, err, "the example publishes models")
			}
			if testCase.reportsFailures {
				requireReportsFailures(t, binary, project, arguments)
			}
		})
	}
}

func projectArguments(_ *testing.T, project string) []string {
	return []string{"-project", project}
}

func projectAndOutputArguments(_ *testing.T, project string) []string {
	return []string{"-project", project, "-out", filepath.Join(project, "internal/store")}
}

func outputArguments(_ *testing.T, project string) []string {
	return []string{"-out", filepath.Join(project, "internal/store")}
}

func requireReportsFailures(t *testing.T, binary, project string, arguments []string) {
	t.Helper()

	query := filepath.Join(project, "db/queries/items.sql")
	require.NoError(t, os.WriteFile(query, []byte("-- sgl.query(ListItems, invalid)\nSELECT id FROM items;"), 0o600))
	output, err := documentationCommand(t.Context(), binary, project, arguments)
	require.Error(t, err, "the example reports SQL diagnostics:\n%s", output)
	require.Contains(t, output, "error diagnostic")

	require.NoError(t, os.RemoveAll(filepath.Join(project, "db/migrations")))
	output, err = documentationCommand(t.Context(), binary, project, arguments)
	require.Error(t, err, "the example reports an operational failure:\n%s", output)
	require.Contains(t, output, "reading migration directory")
}

func documentationBinary(t *testing.T, program string) string {
	t.Helper()

	repository, err := filepath.Abs(repositoryDirectory)
	require.NoError(t, err)
	directory := t.TempDir()
	mainFile := filepath.Join(directory, "main.go")
	require.NoError(t, os.WriteFile(mainFile, []byte(program), 0o600))
	binary := filepath.Join(directory, "workflow")
	workspace := []string{"GOWORK=" + filepath.Join(repository, "go.work")}
	output, err := testkit.GoCommand(t, repository, workspace, "build", "-o", binary, mainFile).CombinedOutput()
	require.NoError(t, err, "building documentation checker/example:\n%s", output)
	return binary
}

func documentationExample(t *testing.T, page, name, language string) string {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(docsDirectory, page))
	require.NoError(t, err)
	pattern := regexp.MustCompile("(?s)<!-- example: " + regexp.QuoteMeta(name) + " -->\\s*```" + regexp.QuoteMeta(language) + "\\n(.*?)\\n```")
	match := pattern.FindSubmatch(content)
	require.Len(t, match, 2, "%s: missing marked %s example", page, name)
	return string(match[1])
}

func documentationProject(t *testing.T) string {
	t.Helper()

	directory := t.TempDir()
	files := map[string]string{
		"db/migrations/0001_items.up.sql": "CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT NOT NULL);",
		"db/queries/items.sql":            "-- sgl.query(ListItems, many)\nSELECT id, name FROM items;",
	}
	for name, content := range files {
		path := filepath.Join(directory, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
	return directory
}

func documentationCommand(ctx context.Context, binary, directory string, arguments []string) (string, error) {
	command := exec.CommandContext(ctx, binary, arguments...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	return string(output), err
}

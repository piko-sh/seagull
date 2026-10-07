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

package clitest_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/sgl/cli"
	"seagull.sh/seagull/cmd/sgl/internal/clitest"
)

func TestProjectWritesAndReadsFilesInItsDirectory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		file    string
		content string
	}{
		{name: "a file at the root", file: "sgl.yaml", content: "version: 1\n"},
		{name: "a file in new directories", file: "db/queries/authors.sql", content: "SELECT 1;\n"},
		{name: "an empty file", file: "empty.sql", content: ""},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			project := clitest.NewProject(t)
			require.False(t, project.Exists(testCase.file))

			project.Write(testCase.file, testCase.content)

			assert.True(t, project.Exists(testCase.file))
			assert.Equal(t, testCase.content, project.Read(testCase.file))
			assert.Equal(t, filepath.Join(project.Dir, filepath.FromSlash(testCase.file)), project.Path(testCase.file))
		})
	}
}

func TestProjectReplacesAFileItWritesAgain(t *testing.T) {
	t.Parallel()

	project := clitest.NewProject(t)
	project.Write("notes.txt", "first")

	project.Write("notes.txt", "second")

	assert.Equal(t, "second", project.Read("notes.txt"))
}

func TestProjectRunsSglInItsDirectory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		arguments  []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{name: "a command that succeeds", arguments: []string{"version"}, wantCode: 0, wantStdout: "config schema", wantStderr: ""},
		{name: "an unknown command", arguments: []string{"no-such-command"}, wantCode: 2, wantStdout: "", wantStderr: `unknown command "no-such-command"`},
		{name: "a command that needs the project's configuration", arguments: []string{"config", "validate"}, wantCode: 2, wantStdout: "", wantStderr: "sgl.yaml"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			project := clitest.NewProject(t)

			result := project.Run(testCase.arguments...)

			assert.Equal(t, testCase.wantCode, result.Code, result.Stderr)
			assert.Contains(t, result.Stdout, testCase.wantStdout)
			assert.Contains(t, result.Stderr, testCase.wantStderr)
		})
	}
}

func TestProjectRunsSglWithTheContextAndOptionsItIsGiven(t *testing.T) {
	t.Parallel()

	project := clitest.NewProject(t)
	project.Write("sgl.yaml", "version: 1\ndatabases:\n  - name: app\n    dialect: sqlite\n")
	elsewhere := clitest.NewProject(t)
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()

	inProject := project.RunWith(t.Context(), nil, "config", "validate")
	elsewhereResult := project.RunWith(t.Context(), []cli.Option{cli.WithDirectory(elsewhere.Dir)}, "config", "validate")
	interrupted := project.RunWith(cancelled, nil, "generate")

	assert.Equal(t, 0, inProject.Code, inProject.Stderr)
	assert.Equal(t, 2, elsewhereResult.Code, "a later option replaces the project's directory")
	assert.NotEqual(t, 0, interrupted.Code, "a cancelled context stops the command")
}

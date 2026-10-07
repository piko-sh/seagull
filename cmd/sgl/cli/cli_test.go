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

package cli_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/sgl/cli"
	"seagull.sh/seagull/cmd/sgl/internal/clitest"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_registry"
)

func sqliteProject(t *testing.T) *clitest.Project {
	t.Helper()
	project := clitest.NewProject(t)
	result := project.Run("init", "--dialect", "sqlite")
	require.Equal(t, 0, result.Code, result.Stderr)
	return project
}

func requireCode(t *testing.T, result clitest.Result, code int) {
	t.Helper()
	require.Equal(t, code, result.Code, "stdout:\n%s\nstderr:\n%s", result.Stdout, result.Stderr)
}

func TestHelpListsCommands(t *testing.T) {
	t.Parallel()

	for _, spelling := range []string{"help", "-h", "--help"} {
		result := clitest.NewProject(t).Run(spelling)
		requireCode(t, result, 0)
		for _, command := range []string{"init", "generate", "vet", "fmt", "explain", "describe", "schema", "migrate", "seed", "dialects", "config", "version"} {
			assert.Contains(t, result.Stdout, "  "+command+" ")
		}
	}
}

func TestUsageErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		arguments []string
		stderr    string
	}{
		{name: "unknown command", arguments: []string{"frobnicate"}, stderr: `unknown command "frobnicate"`},
		{name: "unknown global flag", arguments: []string{"--frobnicate", "version"}, stderr: "frobnicate"},
		{name: "unknown log level", arguments: []string{"--log-level", "loud", "version"}, stderr: "unknown --log-level"},
		{name: "unknown command flag", arguments: []string{"generate", "--frobnicate"}, stderr: "frobnicate"},
		{name: "no configuration", arguments: []string{"generate"}, stderr: "no sgl.yaml found"},
		{name: "explicit configuration missing", arguments: []string{"-c", "missing.yaml", "vet"}, stderr: "missing.yaml"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := clitest.NewProject(t).Run(testCase.arguments...)
			requireCode(t, result, 2)
			assert.Contains(t, result.Stderr, testCase.stderr)
		})
	}

	result := clitest.NewProject(t).Run()
	requireCode(t, result, 2)
	assert.Contains(t, result.Stdout, "Commands:")
}

func TestMainContextRecoversAPanicAsAnInternalError(t *testing.T) {
	t.Parallel()

	const secret = "postgres://sgl:hunter2@localhost:5432/app"
	tests := []struct {
		name     string
		panicked any
	}{
		{name: "a panic quoting a configured connection string", panicked: "driver failed for " + secret},
		{name: "a panic with an error", panicked: errors.New("factory exploded near " + secret)},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			project := clitest.NewProject(t)
			project.Write("sgl.yaml", "databases:\n  - name: app\n    dialect: exploding\n    migrate:\n      dsn: \""+secret+"\"\n")
			options := []cli.Option{cli.WithDialect("exploding", func(dialect_registry.Options) (dialect.Dialect, error) { panic(testCase.panicked) })}

			result := project.RunWith(t.Context(), options, "vet")

			requireCode(t, result, 3)
			assert.Contains(t, result.Stderr, "sgl: internal error: ")
			assert.NotContains(t, result.Stderr, "hunter2")
			assert.Equal(t, 1, strings.Count(result.Stderr, "recovered a panic"), "the stack is logged once")
			_, errorLine, found := strings.Cut(result.Stderr, "sgl: internal error: ")
			require.True(t, found, result.Stderr)
			assert.NotContains(t, errorLine, "goroutine", "the error line carries no stack")
		})
	}
}

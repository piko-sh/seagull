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
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/sgl/internal/clitest"
)

func TestEveryCommandPrintsItsHelpOnRequest(t *testing.T) {
	t.Parallel()

	commands := []struct {
		usage     string
		arguments []string
	}{
		{usage: "Usage: sgl init ", arguments: []string{"init"}},
		{usage: "Usage: sgl generate ", arguments: []string{"generate"}},
		{usage: "Usage: sgl vet ", arguments: []string{"vet"}},
		{usage: "Usage: sgl fmt ", arguments: []string{"fmt"}},
		{usage: "Usage: sgl explain ", arguments: []string{"explain"}},
		{usage: "Usage: sgl describe ", arguments: []string{"describe"}},
		{usage: "Usage: sgl schema dump [--from-db DSN] [--format json|sql] | sgl schema diff ", arguments: []string{"schema"}},
		{usage: "Usage: sgl schema dump ", arguments: []string{"schema", "dump"}},
		{usage: "Usage: sgl schema diff ", arguments: []string{"schema", "diff"}},
		{usage: "Usage: sgl migrate ", arguments: []string{"migrate"}},
		{usage: "Usage: sgl migrate ", arguments: []string{"migrate", "up"}},
		{usage: "Usage: sgl migrate ", arguments: []string{"migrate", "new"}},
		{usage: "Usage: sgl seed ", arguments: []string{"seed"}},
		{usage: "Usage: sgl seed ", arguments: []string{"seed", "apply"}},
		{usage: "Usage: sgl dialects\n", arguments: []string{"dialects"}},
		{usage: "Usage: sgl config schema | sgl config validate [path]\n", arguments: []string{"config"}},
		{usage: "Usage: sgl config schema\n", arguments: []string{"config", "schema"}},
		{usage: "Usage: sgl config validate [path]\n", arguments: []string{"config", "validate"}},
		{usage: "Usage: sgl version\n", arguments: []string{"version"}},
	}
	project := clitest.NewProject(t)
	for _, command := range commands {
		for _, spelling := range []string{"-h", "--help", "-help"} {
			arguments := append(slices.Clone(command.arguments), spelling)
			result := project.Run(arguments...)
			require.Equal(t, 0, result.Code, "%v: %s", arguments, result.Stderr)
			require.True(t, strings.HasPrefix(result.Stdout, command.usage), "%v: %s", arguments, result.Stdout)
			require.Empty(t, result.Stderr, arguments)
		}
	}
	entries, err := os.ReadDir(project.Dir)
	require.NoError(t, err)
	require.Empty(t, entries, "asking for help wrote files")
}

func TestCommandGroupsPrintTheirUsageOnMisuse(t *testing.T) {
	t.Parallel()

	project := clitest.NewProject(t)
	for _, group := range []string{"schema", "config"} {
		result := project.Run(group)
		requireCode(t, result, 2)
		require.Empty(t, result.Stdout, group)
		require.True(t, strings.HasPrefix(result.Stderr, "Usage: sgl "+group), "%s: %s", group, result.Stderr)
	}
	result := project.Run("config", "validate", "--strict")
	requireCode(t, result, 2)
	require.Contains(t, result.Stderr, "flag provided but not defined: -strict")
	require.Contains(t, result.Stderr, "Usage: sgl config validate")
}

func TestFlagHelpNamesTheValues(t *testing.T) {
	t.Parallel()

	result := clitest.NewProject(t).Run("init", "-h")
	requireCode(t, result, 0)
	require.Contains(t, result.Stdout, "  -dialect name\n")
	require.NotContains(t, result.Stdout, "`")
	require.NotContains(t, result.Stdout, "-dialect sgl")

	result = clitest.NewProject(t).Run("migrate", "-h")
	requireCode(t, result, 0)
	require.Contains(t, result.Stdout, "  -dsn DSN\n")
	require.Contains(t, result.Stdout, "  -database name\n")
}

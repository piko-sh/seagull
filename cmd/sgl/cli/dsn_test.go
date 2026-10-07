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
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/sgl/cli"
	"seagull.sh/seagull/cmd/sgl/internal/clitest"
)

func runIn(t *testing.T, project *clitest.Project, directory string, arguments ...string) clitest.Result {
	return project.RunWith(t.Context(), []cli.Option{cli.WithDirectory(project.Path(directory))}, arguments...)
}

func requireNoFiles(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	require.Empty(t, entries, "sgl wrote into the process's working directory")
}

func TestConfiguredSQLiteFilesResolveAgainstTheConfigurationFile(t *testing.T) {
	elsewhere := t.TempDir()
	t.Chdir(elsewhere)

	project := sqliteProject(t)
	project.Write("nested/keep.txt", "")
	project.Write("sgl.yaml", strings.Replace(project.Read("sgl.yaml"), "    seeds: db/seeds\n",
		"    seeds: db/seeds\n    migrate:\n      dsn: file:club.db?_pragma=busy_timeout(1000)\n", 1))

	requireCode(t, project.Run("migrate", "up"), 0)
	require.True(t, project.Exists("club.db"))

	result := runIn(t, project, "nested", "migrate", "status")
	requireCode(t, result, 0)
	require.Contains(t, result.Stdout, "init  applied")
	require.False(t, project.Exists("nested/club.db"))

	project.Write("sgl.yaml", strings.Replace(project.Read("sgl.yaml"), "    seeds: db/seeds\n",
		"    seeds: db/seeds\n    schema_from: club.db\n", 1))
	project.Write("db/migrations/0002_ignored.up.sql", "CREATE TABLE ignored (id INTEGER);\n")
	result = runIn(t, project, "nested", "schema", "dump")
	requireCode(t, result, 0)
	require.Contains(t, result.Stdout, `"name": "authors"`)
	require.NotContains(t, result.Stdout, `"name": "ignored"`)

	requireNoFiles(t, elsewhere)
}

func TestCommandLineSQLiteFilesResolveAgainstTheWorkingDirectory(t *testing.T) {
	elsewhere := t.TempDir()
	t.Chdir(elsewhere)

	project := sqliteProject(t)
	project.Write("nested/keep.txt", "")

	requireCode(t, project.Run("migrate", "up", "--dsn", "plain.db"), 0)
	require.True(t, project.Exists("plain.db"))

	requireCode(t, runIn(t, project, "nested", "migrate", "up", "--dsn", "file:nested.db?mode=rwc"), 0)
	require.True(t, project.Exists("nested/nested.db"))

	result := runIn(t, project, "nested", "schema", "dump", "--from-db", "nested.db", "--format", "sql")
	requireCode(t, result, 0)
	require.Contains(t, result.Stdout, "CREATE TABLE main.authors")

	t.Setenv("SGL_DATABASE_URL", "file:from-environment.db")
	requireCode(t, runIn(t, project, "nested", "migrate", "up"), 0)
	require.True(t, project.Exists("nested/from-environment.db"))

	absolute := filepath.Join(t.TempDir(), "absolute.db")
	requireCode(t, project.Run("migrate", "up", "--dsn", "file:"+filepath.ToSlash(absolute)), 0)
	require.FileExists(t, absolute)

	requireCode(t, project.Run("migrate", "status", "--dsn", ":memory:"), 0)

	requireNoFiles(t, elsewhere)
}

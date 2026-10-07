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
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/sgl/cli"
	"seagull.sh/seagull/cmd/sgl/internal/clitest"
)

const failingMigration = "-- sgl.migration(no_transaction: true)\n" +
	"CREATE TABLE harbours (id INTEGER PRIMARY KEY);\n" +
	"INSERT INTO lighthouses (id) VALUES (1);\n" +
	"CREATE TABLE moorings (id INTEGER PRIMARY KEY);\n"

const fixedMigration = "-- sgl.migration(no_transaction: true)\n" +
	"CREATE TABLE harbours (id INTEGER PRIMARY KEY);\n" +
	"INSERT INTO harbours (id) VALUES (1);\n" +
	"CREATE TABLE moorings (id INTEGER PRIMARY KEY);\n"

type terminal struct {
	*strings.Reader
}

func (terminal) Fd() uintptr {
	return 0
}

func typing(answers string) []cli.Option {
	return []cli.Option{cli.WithIO(terminal{Reader: strings.NewReader(answers)}, nil, nil), cli.WithTerminal(true)}
}

func dirtyProject(t *testing.T) (*clitest.Project, string) {
	t.Helper()
	project, dsn := databaseProject(t)
	project.Write("db/migrations/0002_harbours.up.sql", failingMigration)
	result := project.Run("migrate", "up", "--dsn", dsn)
	requireCode(t, result, 4)
	require.Contains(t, result.Stderr, "statement 2/3 of migration 2")
	status := project.Run("migrate", "status", "--dsn", dsn)
	require.Contains(t, status.Stdout, "harbours  dirty")
	return project, dsn
}

func tableCount(t *testing.T, dsn, table string) int {
	t.Helper()
	database, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	defer func() { assert.NoError(t, database.Close()) }()
	var count int
	require.NoError(t, database.QueryRowContext(t.Context(), "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&count))
	return count
}

func TestMigrateForceRecordsAMigrationWithoutRunningIt(t *testing.T) {
	t.Parallel()

	project, dsn := databaseProject(t)
	project.Write("db/migrations/0002_harbours.up.sql", fixedMigration)

	result := project.Run("migrate", "force", "2", "--yes", "--dsn", dsn)
	requireCode(t, result, 0)
	assert.Equal(t, "forced 2 harbours\n", result.Stdout)
	assert.Contains(t, result.Stderr, "Migration 2 (harbours) is pending. sgl will record it as applied")
	assert.Zero(t, tableCount(t, dsn, "harbours"), "force runs no SQL")

	status := project.Run("migrate", "status", "--dsn", dsn)
	assert.Contains(t, status.Stdout, "init      pending")
	assert.Contains(t, status.Stdout, "harbours  applied")

	result = project.Run("migrate", "up", "--dsn", dsn)
	requireCode(t, result, 0)
	assert.Equal(t, "up 1 init\n1 applied\n", result.Stdout, "only the earlier pending migration runs")
}

func TestMigrateForceAsksOnATerminal(t *testing.T) {
	t.Parallel()

	project, dsn := dirtyProject(t)

	result := project.RunWith(t.Context(), typing("n\n"), "migrate", "force", "2", "--dsn", dsn)
	requireCode(t, result, 1)
	assert.Contains(t, result.Stderr, "Migration 2 (harbours) is dirty.")
	assert.Contains(t, result.Stderr, "Force it? [y/N] ")
	assert.Contains(t, result.Stderr, "nothing changed")

	result = project.RunWith(t.Context(), typing("yes\n"), "migrate", "force", "2", "--dsn", dsn)
	requireCode(t, result, 0)
	assert.Equal(t, "forced 2 harbours\n", result.Stdout)
	assert.Contains(t, project.Run("migrate", "status", "--dsn", dsn).Stdout, "harbours  applied")

	result = project.RunWith(t.Context(), typing("y\n"), "migrate", "force", "1", "--dsn", dsn)
	requireCode(t, result, 1)
	assert.Contains(t, result.Stderr, "cannot force migration 1: migration 2 after it is applied")
}

func TestMigrateForceAndRepairRefuseToRunUnconfirmed(t *testing.T) {
	t.Parallel()

	project, dsn := databaseProject(t)
	for _, arguments := range [][]string{{"migrate", "force", "1"}, {"migrate", "repair"}} {
		result := project.Run(append(arguments, "--dsn", dsn)...)
		requireCode(t, result, 2)
		assert.Contains(t, result.Stderr, "rewrites the migration history, so it asks for confirmation; run it in a terminal or pass --yes")
	}
	result := project.RunWith(t.Context(), typing(""), "migrate", "force", "1", "--dsn", dsn)
	requireCode(t, result, 2)
	assert.Contains(t, result.Stderr, "no answer: standard input ended")
}

func TestMigrateRecoveryArgumentErrors(t *testing.T) {
	t.Parallel()

	project, dsn := databaseProject(t)
	tests := []struct {
		message   string
		arguments []string
		code      int
	}{
		{arguments: []string{"migrate", "force", "--yes"}, message: "migrate force needs one version", code: 2},
		{arguments: []string{"migrate", "force", "one", "--yes"}, message: `migrate force: version "one" is not a number`, code: 2},
		{arguments: []string{"migrate", "force", "9", "--yes"}, message: "no migration file has version 9", code: 2},
		{arguments: []string{"migrate", "repair", "1", "2", "--yes"}, message: "migrate repair takes at most one version", code: 2},
		{arguments: []string{"migrate", "repair", "two", "--yes"}, message: `migrate repair: version "two" is not a number`, code: 2},
		{arguments: []string{"migrate", "repair", "9", "--yes"}, message: "no migration file has version 9", code: 2},
		{arguments: []string{"migrate", "up", "--completed", "1"}, message: "--completed applies only to migrate repair", code: 2},
		{arguments: []string{"migrate", "repair", "--completed", "-2", "--yes"}, message: "--completed must be zero or more", code: 2},
		{arguments: []string{"migrate", "repair", "--completed", "1", "--yes"}, message: "--completed: no migration is dirty", code: 2},
		{arguments: []string{"migrate", "up", "--lock-timeout", "-1s"}, message: "--lock-timeout must not be negative", code: 2},
		{arguments: []string{"seed", "apply", "--lock-timeout", "-1s"}, message: "--lock-timeout must not be negative", code: 2},
		{arguments: []string{"migrate", "explode"}, message: "want up, down, to, status, validate, new, force or repair", code: 2},
	}
	for _, testCase := range tests {
		result := project.Run(append(testCase.arguments, "--dsn", dsn)...)
		requireCode(t, result, testCase.code)
		assert.Contains(t, result.Stderr, testCase.message, testCase.arguments)
	}
}

func TestMigrateRepairRebaselinesAChangedMigration(t *testing.T) {
	t.Parallel()

	project, dsn := databaseProject(t)
	requireCode(t, project.Run("migrate", "up", "--dsn", dsn), 0)
	result := project.Run("migrate", "repair", "--yes", "--dsn", dsn)
	requireCode(t, result, 0)
	assert.Equal(t, "nothing to repair\n", result.Stdout)

	project.Write("db/migrations/0001_init.up.sql", project.Read("db/migrations/0001_init.up.sql")+"-- a reviewed comment\n")
	requireCode(t, project.Run("migrate", "validate", "--dsn", dsn), 1)

	result = project.Run("migrate", "repair", "--yes", "--dsn", dsn)
	requireCode(t, result, 0)
	assert.Equal(t, "re-baselined 1 init\n1 re-baselined, 0 resolved\n", result.Stdout)
	assert.Contains(t, result.Stderr, "Migration 1 (init) changed since it was applied:\n  up file:   recorded ")
	requireCode(t, project.Run("migrate", "validate", "--dsn", dsn), 0)
}

func TestMigrateRepairResumesAFixedDirtyMigration(t *testing.T) {
	t.Parallel()

	project, dsn := dirtyProject(t)
	project.Write("db/migrations/0002_harbours.up.sql", fixedMigration)
	result := project.Run("migrate", "up", "--dsn", dsn)
	requireCode(t, result, 4)
	assert.Contains(t, result.Stderr, "checksum mismatch for migration 2", "an edited dirty migration is not resumed")

	result = project.Run("migrate", "repair", "--yes", "--dsn", dsn)
	requireCode(t, result, 2)
	assert.Contains(t, result.Stderr, "migration 2 is dirty; check which of its statements ran, then pass --completed with how many did")

	result = project.Run("migrate", "repair", "2", "--completed", "1", "--yes", "--dsn", dsn)
	requireCode(t, result, 0)
	assert.Equal(t, "resolved 2 harbours: up resumes at statement 2 of 3\n0 re-baselined, 1 resolved\n", result.Stdout)
	assert.Contains(t, result.Stderr, "Migration 2 (harbours) is dirty: the history records its first 1 statements as run")
	assert.Contains(t, result.Stderr, "Its file changed since the run")
	assert.Contains(t, result.Stderr, "    2. INSERT INTO harbours (id) VALUES (1)")

	result = project.Run("migrate", "up", "--dsn", dsn)
	requireCode(t, result, 0)
	assert.Equal(t, "up 2 harbours\n1 applied\n", result.Stdout)
	assert.Equal(t, 1, tableCount(t, dsn, "moorings"))
	requireCode(t, project.Run("migrate", "validate", "--dsn", dsn), 0)
}

func TestMigrateRepairAsksHowManyStatementsRan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		answers    string
		wantStdout string
		wantCode   int
		wantStatus string
	}{
		{name: "none ran", answers: "0\ny\n", wantStdout: "reset 2 harbours: pending, it runs from its first statement\n0 re-baselined, 1 resolved\n", wantStatus: "harbours  pending"},
		{name: "all ran", answers: "4\n3\nY\n", wantStdout: "resolved 2 harbours: applied\n0 re-baselined, 1 resolved\n", wantStatus: "harbours  applied"},
		{name: "left dirty", answers: "\ny\n", wantStdout: "left 2 harbours dirty\n0 re-baselined, 0 resolved\n", wantStatus: "harbours  dirty"},
		{name: "declined", answers: "1\n\n", wantCode: 1, wantStatus: "harbours  dirty"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			project, dsn := dirtyProject(t)
			result := project.RunWith(t.Context(), typing(testCase.answers), "migrate", "repair", "--dsn", dsn)
			requireCode(t, result, testCase.wantCode)
			assert.Equal(t, testCase.wantStdout, result.Stdout)
			assert.Contains(t, result.Stderr, "How many of migration 2's 3 statements ran? Enter 0 to 3, or nothing to leave it dirty: ")
			assert.Contains(t, project.Run("migrate", "status", "--dsn", dsn).Stdout, testCase.wantStatus)
		})
	}
}

func TestMigrateAndSeedAcceptTheLockFlags(t *testing.T) {
	t.Parallel()

	project, dsn := databaseProject(t)
	result := project.Run("migrate", "up", "--lock-timeout", "5s", "--non-blocking", "--dsn", dsn)
	requireCode(t, result, 0)
	result = project.Run("seed", "apply", "--lock-timeout", "5s", "--non-blocking", "--dsn", dsn)
	requireCode(t, result, 0)
	assert.Equal(t, "1 applied\n", result.Stdout)
	_, err := os.Stat(project.Path("app.db"))
	require.NoError(t, err)
}

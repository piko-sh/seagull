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
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/sgl/internal/clitest"
)

const aliasQuery = "--   seagull.query(ListAuthors,   many)\nSELECT id FROM authors;\n"

const aliasMigration = "--  seagull.migration(no_transaction:true)\nCREATE TABLE harbours (id INTEGER PRIMARY KEY);\n"

func TestFmtListsChecksAndWrites(t *testing.T) {
	t.Parallel()

	project := sqliteProject(t)
	project.Write("db/queries/alias.sql", aliasQuery)

	result := project.Run("fmt", "-l")
	requireCode(t, result, 0)
	assert.Equal(t, "db/queries/alias.sql\n", result.Stdout)

	result = project.Run("fmt", "--check")
	requireCode(t, result, 1)
	assert.Equal(t, "db/queries/alias.sql\n", result.Stdout)

	result = project.Run("fmt", "db/queries/alias.sql")
	requireCode(t, result, 0)
	assert.Contains(t, result.Stdout, "sgl.query(ListAuthors, many)")
	assert.Equal(t, aliasQuery, project.Read("db/queries/alias.sql"))

	requireCode(t, project.Run("fmt", "-w"), 0)
	assert.Contains(t, project.Read("db/queries/alias.sql"), "-- sgl.query(ListAuthors, many)")
	requireCode(t, project.Run("fmt", "--check"), 0)
}

func TestFmtRewritePrefix(t *testing.T) {
	t.Parallel()

	project := sqliteProject(t)
	requireCode(t, project.Run("fmt", "-w", "--rewrite-prefix", "seagull", "db/queries"), 0)
	assert.Contains(t, project.Read("db/queries/example.sql"), "-- seagull.query(GetAuthor, one)")

	result := project.Run("fmt", "--rewrite-prefix", "Bad Prefix")
	requireCode(t, result, 2)
	assert.Contains(t, result.Stderr, "directive prefix")
}

func TestFmtWithoutConfiguration(t *testing.T) {
	t.Parallel()

	project := clitest.NewProject(t)
	project.Write("queries/alias.sql", aliasQuery)
	result := project.Run("fmt", "-l", "queries")
	requireCode(t, result, 0)
	assert.Equal(t, "queries/alias.sql\n", result.Stdout)

	requireCode(t, project.Run("fmt"), 2)
	requireCode(t, project.Run("fmt", "-l", "missing"), 2)
}

func TestFmtLeavesMigrationsOutUnlessIncluded(t *testing.T) {
	t.Parallel()

	project := sqliteProject(t)
	project.Write("db/queries/alias.sql", aliasQuery)
	project.Write("db/migrations/0002_harbours.up.sql", aliasMigration)

	for _, arguments := range [][]string{{"fmt", "-l"}, {"fmt", "--check"}} {
		result := project.Run(arguments...)
		assert.Equal(t, "db/queries/alias.sql\n", result.Stdout, arguments)
	}
	requireCode(t, project.Run("fmt", "--check"), 1)

	requireCode(t, project.Run("fmt", "-w"), 0)
	assert.Equal(t, aliasMigration, project.Read("db/migrations/0002_harbours.up.sql"), "fmt -w leaves migrations alone")
	requireCode(t, project.Run("fmt", "--check"), 0)

	result := project.Run("fmt", "-l", "--include-migrations")
	requireCode(t, result, 0)
	assert.Equal(t, "db/migrations/0002_harbours.up.sql\n", result.Stdout)
	requireCode(t, project.Run("fmt", "-w", "--include-migrations"), 0)
	assert.Equal(t, "-- sgl.migration(no_transaction:true)\nCREATE TABLE harbours (id INTEGER PRIMARY KEY);\n", project.Read("db/migrations/0002_harbours.up.sql"))
}

func TestFmtRefusesAPathInsideMigrations(t *testing.T) {
	t.Parallel()

	project := sqliteProject(t)
	project.Write("db/migrations/0002_harbours.up.sql", aliasMigration)

	for _, path := range []string{"db/migrations/0002_harbours.up.sql", "db/migrations"} {
		result := project.Run("fmt", "-w", path)
		requireCode(t, result, 2)
		assert.Contains(t, result.Stderr, path+" is in the migrations directory db/migrations, which sgl fmt leaves alone")
		assert.Contains(t, result.Stderr, "checksum mismatch")
		assert.Contains(t, result.Stderr, "--include-migrations")
	}
	assert.Equal(t, aliasMigration, project.Read("db/migrations/0002_harbours.up.sql"))

	result := project.Run("fmt", "-l", "--include-migrations", "db/migrations")
	requireCode(t, result, 0)
	assert.Equal(t, "db/migrations/0002_harbours.up.sql\n", result.Stdout)
}

func TestFmtOfADirectoryHoldingMigrationsLeavesThemOut(t *testing.T) {
	t.Parallel()

	project := sqliteProject(t)
	project.Write("db/queries/alias.sql", aliasQuery)
	project.Write("db/migrations/0002_harbours.up.sql", aliasMigration)

	result := project.Run("fmt", "-l", "db")
	requireCode(t, result, 0)
	assert.Equal(t, "db/queries/alias.sql\n", result.Stdout)
	assert.Contains(t, result.Stderr, "leaving out the migrations directory db/migrations; pass --include-migrations to format it")
}

func TestFmtNeverChangesAnAppliedMigration(t *testing.T) {
	t.Parallel()

	project, dsn := databaseProject(t)
	project.Write("db/migrations/0002_harbours.up.sql", aliasMigration)
	requireCode(t, project.Run("migrate", "up", "--dsn", dsn), 0)

	requireCode(t, project.Run("fmt", "-w"), 0)
	assert.Equal(t, aliasMigration, project.Read("db/migrations/0002_harbours.up.sql"))
	requireCode(t, project.Run("migrate", "validate", "--dsn", dsn), 0)

	project.Write("db/migrations/0003_moorings.up.sql", "CREATE TABLE moorings (id INTEGER PRIMARY KEY);\n")
	result := project.Run("migrate", "up", "--dsn", dsn)
	requireCode(t, result, 0)
	assert.Equal(t, "up 3 moorings\n1 applied\n", result.Stdout)
}

func TestFmtReadsFilesAsTheirDialectWritesSQL(t *testing.T) {
	t.Parallel()

	project := clitest.NewProject(t)
	requireCode(t, project.Run("init", "--dialect", "mysql"), 0)
	query := "-- seagull.query(Note, one)\nSELECT 'it\\'s\n-- seagull.query(  X ,one)\n';\n"
	project.Write("db/queries/note.sql", query)

	result := project.Run("fmt", "db/queries/note.sql")
	requireCode(t, result, 0)
	assert.Equal(t, "-- sgl.query(Note, one)\nSELECT 'it\\'s\n-- seagull.query(  X ,one)\n';\n", result.Stdout,
		"a directive-like line inside a MySQL string is SQL, not a directive")

	project.Write("loose/note.sql", query)
	result = project.Run("fmt", "loose/note.sql")
	requireCode(t, result, 0)
	assert.Equal(t, "-- sgl.query(Note, one)\nSELECT 'it\\'s\n-- sgl.query(X, one)\n';\n", result.Stdout,
		"a file outside every configured directory is read with neutral rules")
}

func TestFmtListsAndThenWritesWhenAskedForBoth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		arguments []string
		code      int
	}{
		{name: "list and write", arguments: []string{"fmt", "-l", "-w"}, code: 0},
		{name: "check and write", arguments: []string{"fmt", "-w", "--check"}, code: 1},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			project := sqliteProject(t)
			project.Write("db/queries/alias.sql", aliasQuery)

			result := project.RunWith(t.Context(), nil, testCase.arguments...)

			requireCode(t, result, testCase.code)
			assert.Equal(t, "db/queries/alias.sql\n", result.Stdout)
			assert.Contains(t, project.Read("db/queries/alias.sql"), "-- sgl.query(ListAuthors, many)")
		})
	}
}

func TestFmtRefusesAFileLargerThanItReads(t *testing.T) {
	t.Parallel()

	project := sqliteProject(t)
	project.Write("db/queries/huge.sql", aliasQuery+strings.Repeat("-- padding\n", (4<<20)/len("-- padding\n")+1))

	result := project.RunWith(t.Context(), nil, "fmt", "db/queries/huge.sql")

	requireCode(t, result, 2)
	assert.Contains(t, result.Stderr, "huge.sql")
	assert.Contains(t, result.Stderr, "larger than 4194304 bytes")
}

func TestFmtFormatsASymbolicLinkWithoutReplacingIt(t *testing.T) {
	t.Parallel()

	project := sqliteProject(t)
	project.Write("shared/alias.sql", aliasQuery)
	if err := os.Symlink(project.Path("shared/alias.sql"), project.Path("db/queries/linked.sql")); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}

	result := project.RunWith(t.Context(), nil, "fmt", "-w", "db/queries/linked.sql")

	requireCode(t, result, 0)
	assert.Contains(t, project.Read("shared/alias.sql"), "-- sgl.query(ListAuthors, many)")
	info, err := os.Lstat(project.Path("db/queries/linked.sql"))
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "the link is still a link")
}

func TestFmtStopsWhenCancelled(t *testing.T) {
	t.Parallel()

	project := sqliteProject(t)
	project.Write("db/queries/alias.sql", aliasQuery)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	result := project.RunWith(ctx, nil, "fmt", "-w")

	requireCode(t, result, 130)
	assert.Equal(t, aliasQuery, project.Read("db/queries/alias.sql"), "nothing is rewritten once cancelled")
}

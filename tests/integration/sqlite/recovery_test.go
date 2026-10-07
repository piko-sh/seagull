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

//go:build integration

package sqlite_test

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_sqlite"
	"seagull.sh/seagull/tests/integration/internal/harness"
)

func TestSQLiteRepairResumesADirtyMigration(t *testing.T) {
	ctx := t.Context()
	database := openMigrationDatabase(t)
	content := []byte("CREATE TABLE boats (id INTEGER PRIMARY KEY);\nCREATE TABLE harbours (id INTEGER PRIMARY KEY);\n")
	files := fstest.MapFS{"migrations/0001_boats.up.sql": {Data: content}}
	for _, statement := range []string{
		legacyHistoryDDL,
		"INSERT INTO sgl_migrations (version, name, checksum, duration_ms, last_statement, dirty) VALUES (1, 'boats', '" + harness.ChecksumOf(content) + "', 0, 0, 1)",
		"CREATE TABLE boats (id INTEGER PRIMARY KEY)",
	} {
		_, err := database.ExecContext(ctx, statement)
		require.NoError(t, err)
	}
	migrator, err := migrate.NewMigrator(database, migrate_backend_sqlite.New(), files, "migrations")
	require.NoError(t, err)
	_, err = migrator.Up(ctx)
	var mismatch *migrate.ResumeMismatchError
	require.ErrorAs(t, err, &mismatch)

	plan, err := migrator.PlanRepair(ctx)
	require.NoError(t, err)
	require.Len(t, plan.Dirty, 1)
	require.Equal(t, []string{"CREATE TABLE boats (id INTEGER PRIMARY KEY)", "CREATE TABLE harbours (id INTEGER PRIMARY KEY)"}, plan.Dirty[0].Statements)
	result, err := migrator.Repair(ctx, migrate.RepairOptions{Plan: plan, Completed: map[int64]int{1: 1}})
	require.NoError(t, err)
	require.Equal(t, []int64{1}, result.Resolved)

	applied, err := migrator.Up(ctx)
	require.NoError(t, err, "the repaired migration resumes after the statement that ran")
	require.Equal(t, 1, applied)
	require.Equal(t, 1, harness.CountOf(t, database, "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'harbours'"))
	require.Equal(t, []harness.HistoryEntry{{Version: 1, Dirty: false, LastStatement: 1}}, harness.ReadHistory(t, database))
}

func TestSQLiteRepairMakesAnUnstartedDirtyMigrationPending(t *testing.T) {
	ctx := t.Context()
	database := openMigrationDatabase(t)
	content := []byte("CREATE TABLE boats (id INTEGER PRIMARY KEY);\nCREATE TABLE harbours (id INTEGER PRIMARY KEY);\n")
	files := fstest.MapFS{"migrations/0001_boats.up.sql": {Data: content}}
	for _, statement := range []string{
		legacyHistoryDDL,
		"INSERT INTO sgl_migrations (version, name, checksum, duration_ms, last_statement, dirty) VALUES (1, 'boats', 'an older file', 0, 1, 1)",
	} {
		_, err := database.ExecContext(ctx, statement)
		require.NoError(t, err)
	}
	migrator, err := migrate.NewMigrator(database, migrate_backend_sqlite.New(), files, "migrations")
	require.NoError(t, err)

	plan, err := migrator.PlanRepair(ctx)
	require.NoError(t, err)
	require.False(t, plan.Dirty[0].ChecksumMatch)
	_, err = migrator.Repair(ctx, migrate.RepairOptions{Plan: plan, Completed: map[int64]int{1: 0}})
	require.NoError(t, err)
	require.Empty(t, harness.ReadHistory(t, database), "a migration none of whose statements ran is pending again")

	applied, err := migrator.Up(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, applied)
	require.Equal(t, 2, harness.CountOf(t, database, "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name IN ('boats', 'harbours')"))
}

func TestSQLiteForceAndRepairRewriteTheHistory(t *testing.T) {
	ctx := t.Context()
	database := openMigrationDatabase(t)
	files := fstest.MapFS{
		"migrations/0001_boats.up.sql":    {Data: []byte("CREATE TABLE boats (id INTEGER PRIMARY KEY);\n")},
		"migrations/0002_harbours.up.sql": {Data: []byte("CREATE TABLE harbours (id INTEGER PRIMARY KEY);\n")},
	}
	migrator, err := migrate.NewMigrator(database, migrate_backend_sqlite.New(), files, "migrations")
	require.NoError(t, err)

	require.NoError(t, migrator.Force(ctx, 1))
	require.Zero(t, harness.CountOf(t, database, "SELECT count(*) FROM sqlite_master WHERE name = 'boats'"), "force runs no SQL")
	applied, err := migrator.Up(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, applied, "only the migration after the forced one runs")

	files["migrations/0001_boats.up.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE boats (id INTEGER PRIMARY KEY); -- reviewed\n")}
	var changed *migrate.ChecksumMismatchError
	require.ErrorAs(t, migrator.Validate(ctx), &changed)
	plan, err := migrator.PlanRepair(ctx)
	require.NoError(t, err)
	require.Len(t, plan.Changed, 1)
	_, err = migrator.Repair(ctx, migrate.RepairOptions{Plan: plan, Completed: nil})
	require.NoError(t, err)
	require.NoError(t, migrator.Validate(ctx))
}

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

package postgres_test

import (
	"context"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/migrate/sqlexec"
	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_postgres"
	"seagull.sh/seagull/tests/integration/internal/harness"
)

func TestPostgresRepairResumesADirtyMigration(t *testing.T) {
	ctx := t.Context()
	database := openMigrationDatabase(t)
	content := []byte("CREATE TABLE boats (id INT PRIMARY KEY);\nCREATE TABLE harbours (id INT PRIMARY KEY);\n")
	files := fstest.MapFS{"migrations/0001_boats.up.sql": {Data: content}}
	for _, statement := range []string{
		legacyHistoryDDL,
		"INSERT INTO sgl_migrations (version, name, checksum, duration_ms, last_statement, dirty) VALUES (1, 'boats', '" + harness.ChecksumOf(content) + "', 0, 0, TRUE)",
		"CREATE TABLE boats (id INT PRIMARY KEY)",
	} {
		_, err := database.ExecContext(ctx, statement)
		require.NoError(t, err)
	}
	migrator, err := migrate.NewMigrator(database, migrate_backend_postgres.New(), files, "migrations")
	require.NoError(t, err)
	_, err = migrator.Up(ctx)
	var mismatch *migrate.ResumeMismatchError
	require.ErrorAs(t, err, &mismatch)

	plan, err := migrator.PlanRepair(ctx)
	require.NoError(t, err)
	require.Len(t, plan.Dirty, 1)
	require.Equal(t, []string{"CREATE TABLE boats (id INT PRIMARY KEY)", "CREATE TABLE harbours (id INT PRIMARY KEY)"}, plan.Dirty[0].Statements)
	result, err := migrator.Repair(ctx, migrate.RepairOptions{Plan: plan, Completed: map[int64]int{1: 1}})
	require.NoError(t, err)
	require.Equal(t, []int64{1}, result.Resolved)

	applied, err := migrator.Up(ctx)
	require.NoError(t, err, "the repaired migration resumes after the statement that ran")
	require.Equal(t, 1, applied)
	require.Equal(t, 1, harness.CountOf(t, database, "SELECT count(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = 'harbours'"))
	require.Equal(t, []harness.HistoryEntry{{Version: 1, Dirty: false, LastStatement: 1}}, harness.ReadHistory(t, database))
}

func TestPostgresForceAndRepairRewriteTheHistory(t *testing.T) {
	ctx := t.Context()
	database := openMigrationDatabase(t)
	files := fstest.MapFS{
		"migrations/0001_boats.up.sql":    {Data: []byte("CREATE TABLE boats (id INT PRIMARY KEY);\n")},
		"migrations/0002_harbours.up.sql": {Data: []byte("CREATE TABLE harbours (id INT PRIMARY KEY);\n")},
	}
	migrator, err := migrate.NewMigrator(database, migrate_backend_postgres.New(), files, "migrations")
	require.NoError(t, err)

	require.NoError(t, migrator.Force(ctx, 1))
	require.Zero(t, harness.CountOf(t, database, "SELECT count(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = 'boats'"), "force runs no SQL")
	applied, err := migrator.Up(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, applied, "only the migration after the forced one runs")

	files["migrations/0001_boats.up.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE boats (id INT PRIMARY KEY); -- reviewed\n")}
	var changed *migrate.ChecksumMismatchError
	require.ErrorAs(t, migrator.Validate(ctx), &changed)
	plan, err := migrator.PlanRepair(ctx)
	require.NoError(t, err)
	require.Len(t, plan.Changed, 1)
	_, err = migrator.Repair(ctx, migrate.RepairOptions{Plan: plan, Completed: nil})
	require.NoError(t, err)
	require.NoError(t, migrator.Validate(ctx))
}

func TestPostgresLockTimeoutAndNonBlockingLock(t *testing.T) {
	for name, backend := range map[string]migrate.Backend{
		"advisory lock": migrate_backend_postgres.New(),
		"table lock":    migrate_backend_postgres.NewPgBouncer(),
	} {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			database := openMigrationDatabase(t)
			files := fstest.MapFS{"migrations/0001_boats.up.sql": {Data: []byte("CREATE TABLE boats (id INT PRIMARY KEY);\n")}}
			holder, err := migrate.NewExecutor(database, backend)
			require.NoError(t, err)
			require.NoError(t, holder.AcquireLock(ctx))

			waiting, err := migrate.NewMigrator(database, backend, files, "migrations", migrate.WithNonBlockingLock())
			require.NoError(t, err)
			_, err = waiting.Up(ctx)
			require.ErrorIs(t, err, migrate.ErrLockNotAcquired)

			bounded, err := migrate.NewMigrator(database, backend, files, "migrations", migrate.WithLockTimeout(300*time.Millisecond))
			require.NoError(t, err)
			_, err = bounded.Up(ctx)
			require.ErrorIs(t, err, migrate.ErrLockTimeout)
			require.ErrorIs(t, bounded.Force(ctx, 1), migrate.ErrLockTimeout)

			seedHolder, err := sqlexec.NewSeedExecutor(database, backend)
			require.NoError(t, err)
			require.NoError(t, seedHolder.AcquireSeedLock(ctx))
			seeder, err := migrate.NewSeeder(database, backend, fstest.MapFS{"seeds/001_boats.sql": {Data: []byte("SELECT 1;\n")}}, "seeds", migrate.WithNonBlockingLock())
			require.NoError(t, err)
			_, err = seeder.Apply(ctx)
			require.ErrorIs(t, err, migrate.ErrLockNotAcquired)
			require.NoError(t, seedHolder.ReleaseSeedLock(ctx))

			require.NoError(t, holder.ReleaseLock(ctx))
			applied, err := bounded.Up(ctx)
			require.NoError(t, err)
			require.Equal(t, 1, applied)
		})
	}
}

func TestPostgresTableLockOutlivesTheLockTimeout(t *testing.T) {
	database := openMigrationDatabase(t)
	files := harness.MigrationFiles(
		"CREATE TABLE "+harness.MigrationTable+" (id INTEGER PRIMARY KEY, label VARCHAR(64) NOT NULL);\n",
		"ALTER TABLE "+harness.MigrationTable+" ADD COLUMN colour VARCHAR(32);\n",
		"ALTER TABLE "+harness.MigrationTable+" DROP COLUMN colour;\n",
		"DELETE FROM "+harness.MigrationTable+";",
	)
	backend := migrate_backend_postgres.NewPgBouncer()
	probed := map[int64]error{}
	probe := func(ctx context.Context, hook migrate.MigrationHookContext) error {
		probed[hook.Version] = tryLockRow(ctx, database, backend.Names().MigrationLock)
		return nil
	}
	migrator, err := migrate.NewMigrator(database, backend, files, "migrations",
		migrate.WithLockTimeout(time.Minute), migrate.WithBeforeMigration(probe))
	require.NoError(t, err)

	applied, err := migrator.Up(t.Context())
	require.NoError(t, err)
	require.Equal(t, 2, applied)
	for version, probeErr := range probed {
		require.Errorf(t, probeErr, "the lock row was free before migration %d, so the lock ended with its acquisition's context", version)
	}
}

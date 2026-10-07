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

package mysql_test

import (
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/migrate/sqlexec"
	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_mysql"
	"seagull.sh/seagull/tests/integration/internal/harness"
)

func TestMySQLRepairResumesAFixedDirtyMigration(t *testing.T) {
	ctx := t.Context()
	database := openMigrationDatabase(t)
	files := fstest.MapFS{
		"migrations/0001_harbours.up.sql": {Data: []byte("CREATE TABLE harbours (id INT PRIMARY KEY, name VARCHAR(64) NOT NULL);\n" +
			"INSERT INTO lighthouses (id) VALUES (1);\n" +
			"INSERT INTO harbours (id, name) VALUES (1, 'Padstow');\n")},
	}
	migrator, err := migrate.NewMigrator(database, migrate_backend_mysql.New(), files, "migrations")
	require.NoError(t, err)
	_, err = migrator.Up(ctx)
	require.ErrorContains(t, err, "statement 2/3")
	require.Equal(t, []harness.HistoryEntry{{Version: 1, Dirty: true, LastStatement: 0}}, harness.ReadHistory(t, database))

	files["migrations/0001_harbours.up.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE harbours (id INT PRIMARY KEY, name VARCHAR(64) NOT NULL);\n" +
		"INSERT INTO harbours (id, name) VALUES (2, 'Fowey');\n" +
		"INSERT INTO harbours (id, name) VALUES (1, 'Padstow');\n")}
	var changed *migrate.ChecksumMismatchError
	_, err = migrator.Up(ctx)
	require.ErrorAs(t, err, &changed, "an edited dirty migration is not resumed")

	plan, err := migrator.PlanRepair(ctx)
	require.NoError(t, err)
	require.Len(t, plan.Dirty, 1)
	require.False(t, plan.Dirty[0].ChecksumMatch)
	_, err = migrator.Repair(ctx, migrate.RepairOptions{Plan: plan, Completed: map[int64]int{1: 1}})
	require.NoError(t, err)

	applied, err := migrator.Up(ctx)
	require.NoError(t, err, "the repaired migration resumes with its fixed second statement")
	require.Equal(t, 1, applied)
	require.Equal(t, 2, harness.CountOf(t, database, "SELECT count(*) FROM harbours"))
	require.Equal(t, []harness.HistoryEntry{{Version: 1, Dirty: false, LastStatement: 2}}, harness.ReadHistory(t, database))
	require.NoError(t, migrator.Validate(ctx))
}

func TestMySQLForceAndRepairRewriteTheHistory(t *testing.T) {
	ctx := t.Context()
	database := openMigrationDatabase(t)
	files := fstest.MapFS{
		"migrations/0001_boats.up.sql":    {Data: []byte("CREATE TABLE boats (id INT PRIMARY KEY);\n")},
		"migrations/0002_harbours.up.sql": {Data: []byte("CREATE TABLE harbours (id INT PRIMARY KEY);\n")},
	}
	migrator, err := migrate.NewMigrator(database, migrate_backend_mysql.New(), files, "migrations")
	require.NoError(t, err)

	require.NoError(t, migrator.Force(ctx, 1))
	require.Zero(t, harness.CountOf(t, database, "SELECT count(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'boats'"), "force runs no SQL")
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

func TestMySQLLockTimeoutAndNonBlockingLock(t *testing.T) {
	ctx := t.Context()
	database := openMigrationDatabase(t)
	files := fstest.MapFS{"migrations/0001_boats.up.sql": {Data: []byte("CREATE TABLE boats (id INT PRIMARY KEY);\n")}}
	holder, err := migrate.NewExecutor(database, migrate_backend_mysql.New())
	require.NoError(t, err)
	require.NoError(t, holder.AcquireLock(ctx))

	waiting, err := migrate.NewMigrator(database, migrate_backend_mysql.New(), files, "migrations", migrate.WithNonBlockingLock())
	require.NoError(t, err)
	_, err = waiting.Up(ctx)
	require.ErrorIs(t, err, migrate.ErrLockNotAcquired)

	bounded, err := migrate.NewMigrator(database, migrate_backend_mysql.New(), files, "migrations", migrate.WithLockTimeout(300*time.Millisecond))
	require.NoError(t, err)
	_, err = bounded.Up(ctx)
	require.ErrorIs(t, err, migrate.ErrLockTimeout)

	seedHolder, err := sqlexec.NewSeedExecutor(database, migrate_backend_mysql.New())
	require.NoError(t, err)
	require.NoError(t, seedHolder.AcquireSeedLock(ctx))
	seeder, err := migrate.NewSeeder(database, migrate_backend_mysql.New(), fstest.MapFS{"seeds/001_boats.sql": {Data: []byte("SELECT 1;\n")}}, "seeds", migrate.WithNonBlockingLock())
	require.NoError(t, err)
	_, err = seeder.Apply(ctx)
	require.ErrorIs(t, err, migrate.ErrLockNotAcquired)
	require.NoError(t, seedHolder.ReleaseSeedLock(ctx))

	require.NoError(t, holder.ReleaseLock(ctx))
	applied, err := bounded.Up(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, applied)
}

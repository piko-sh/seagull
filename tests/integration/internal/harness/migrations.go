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

package harness

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"maps"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/migrate"
)

const (
	// MigrationTable is the table the shared migration lifecycle creates and seeds.
	MigrationTable = "sgl_it_widgets"

	// migrationsDirectory is the directory of the lifecycle's migration files.
	migrationsDirectory = "migrations"

	// seedsDirectory is the directory of the lifecycle's seed files.
	seedsDirectory = "seeds"

	// createWidgetsFile is the lifecycle's first up migration.
	createWidgetsFile = "migrations/0001_create_widgets.up.sql"

	// lifecycleMigrations is the number of migrations the lifecycle applies.
	lifecycleMigrations = 2

	// seededWidgets is the number of rows the lifecycle's seed inserts.
	seededWidgets = 2
)

// HistoryEntry is the part of a migration history row the run-time tests compare.
type HistoryEntry struct {
	// Version is the migration's version.
	Version int64

	// LastStatement is the 0-based index of the last statement recorded as run, or -1 when
	// none is recorded.
	LastStatement int64

	// Dirty reports whether the migration is recorded as dirty.
	Dirty bool
}

// MigrationFiles returns the migrations and seed of the shared lifecycle, written for one
// engine: an up and a down migration that create and drop MigrationTable, an up and a
// down migration that add and drop a colour column, and a seed that empties the table and
// inserts two rows.
//
// Takes createWidgets (string) which creates MigrationTable with id and label columns.
// Takes addColour (string) which adds the colour column.
// Takes dropColour (string) which drops it again.
// Takes clearWidgets (string) which empties MigrationTable before the seed's inserts.
//
// Returns fstest.MapFS which holds migrations/ and seeds/.
func MigrationFiles(createWidgets, addColour, dropColour, clearWidgets string) fstest.MapFS {
	return fstest.MapFS{
		createWidgetsFile:                         {Data: []byte(createWidgets)},
		"migrations/0001_create_widgets.down.sql": {Data: []byte("DROP TABLE " + MigrationTable + ";\n")},
		"migrations/0002_add_colour.up.sql":       {Data: []byte(addColour)},
		"migrations/0002_add_colour.down.sql":     {Data: []byte(dropColour)},
		"seeds/001_widgets.sql": {Data: []byte(clearWidgets + "\nINSERT INTO " + MigrationTable +
			" (id, label) VALUES (1, 'first');\nINSERT INTO " + MigrationTable + " (id, label) VALUES (2, 'second');\n")},
	}
}

// MigratorLifecycle drives a migrator and a seeder through their whole lifecycle on a
// database: status before the history table exists, up, validate, an up with nothing
// pending, seed, reseed, an edited applied migration refused, and down to nothing. Settle
// waits for a change to become visible, which an engine that applies mutations
// asynchronously needs.
//
// Takes database (*sql.DB) which is an empty database.
// Takes backend (migrate.Backend) which is the engine's backend.
// Takes files (fstest.MapFS) which holds the lifecycle's files, from MigrationFiles.
// Takes settle (func(t *testing.T, check func() bool)) which waits until check holds.
func MigratorLifecycle(t *testing.T, database *sql.DB, backend migrate.Backend, files fstest.MapFS, settle func(t *testing.T, check func() bool)) {
	t.Helper()

	migrator, err := migrate.NewMigrator(database, backend, files, migrationsDirectory)
	require.NoError(t, err)
	migrateUp(t, migrator)
	seedAndReseed(t, database, backend, files, settle)
	refuseEditedMigration(t, database, backend, files)
	rollBack(t, migrator, settle)
}

// ConcurrentFirstBoot races several migrators, as replicas booting at once would, to
// apply the same migrations to an empty database, and checks that between them they apply
// each migration once and none fails.
//
// Takes database (*sql.DB) which is an empty database.
// Takes backend (migrate.Backend) which is the engine's backend.
// Takes files (fstest.MapFS) which holds the lifecycle's files, from MigrationFiles.
func ConcurrentFirstBoot(t *testing.T, database *sql.DB, backend migrate.Backend, files fstest.MapFS) {
	t.Helper()

	results := RaceUp(t, Replicas(t, database, backend, files, concurrentReplicas))
	total := TotalApplied(t, results, "")
	require.Equal(t, lifecycleMigrations, total, "the replicas between them must apply each migration once")
}

// AppliedVersions returns the versions a migrator reports applied, comma-separated.
//
// Takes migrator (*migrate.Migrator) which reports the status.
//
// Returns string which lists the applied versions, such as "1,2".
func AppliedVersions(t *testing.T, migrator *migrate.Migrator) string {
	t.Helper()

	statuses, err := migrator.Status(t.Context())
	require.NoError(t, err)
	var versions []string
	for _, status := range statuses {
		if status.Applied {
			versions = append(versions, strconv.FormatInt(status.Version, 10))
		}
	}
	return strings.Join(versions, ",")
}

// CountWidgets returns the number of rows in MigrationTable.
//
// Takes database (*sql.DB) which holds the table.
//
// Returns int which is the count.
func CountWidgets(t *testing.T, database *sql.DB) int {
	t.Helper()

	return CountOf(t, database, "SELECT COUNT(*) FROM "+MigrationTable)
}

// CountOf runs a query whose one row and column is a count.
//
// Takes database (*sql.DB) which runs the query.
// Takes query (string) which returns the count.
//
// Returns int which is the count.
func CountOf(t *testing.T, database *sql.DB, query string) int {
	t.Helper()

	var count int
	require.NoError(t, database.QueryRowContext(t.Context(), query).Scan(&count))
	return count
}

// SettleImmediately checks a condition at once, for an engine whose changes are visible
// as soon as the statement that makes them returns.
//
// Takes check (func() bool) which reports whether the change is visible.
func SettleImmediately(t *testing.T, check func() bool) {
	t.Helper()

	require.True(t, check())
}

// ReadHistory returns the rows of the default migration history table, in version order.
//
// Takes database (*sql.DB) which holds the history table.
//
// Returns []HistoryEntry which holds the rows.
func ReadHistory(t *testing.T, database *sql.DB) []HistoryEntry {
	t.Helper()

	rows, err := database.QueryContext(t.Context(),
		"SELECT version, dirty, COALESCE(last_statement, -1) FROM sgl_migrations ORDER BY version")
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var entries []HistoryEntry
	for rows.Next() {
		var entry HistoryEntry
		require.NoError(t, rows.Scan(&entry.Version, &entry.Dirty, &entry.LastStatement))
		entries = append(entries, entry)
	}
	require.NoError(t, rows.Err())
	return entries
}

// ChecksumOf returns a file's checksum as the migration history records it.
//
// Takes content ([]byte) which is the file's content.
//
// Returns string which is the hexadecimal SHA-256 digest.
func ChecksumOf(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// WithParameter appends a query parameter to a URL-style connection string.
//
// Takes connectionString (string) which is the connection string.
// Takes key (string) which is the parameter's name.
// Takes value (string) which is its value.
//
// Returns string which is the connection string with the parameter.
func WithParameter(connectionString, key, value string) string {
	separator := "?"
	if strings.Contains(connectionString, "?") {
		separator = "&"
	}
	return connectionString + separator + key + "=" + value
}

// migrateUp checks a migrator before and after it applies every migration.
//
// Takes migrator (*migrate.Migrator) which runs the lifecycle's migrations.
func migrateUp(t *testing.T, migrator *migrate.Migrator) {
	t.Helper()
	ctx := t.Context()

	fresh, err := migrator.Status(ctx)
	require.NoError(t, err, "status must read a database that has no history table")
	require.Len(t, fresh, lifecycleMigrations)
	for _, status := range fresh {
		assert.False(t, status.Applied, "migration %d applied before Up", status.Version)
	}
	require.NoError(t, migrator.Validate(ctx))

	applied, err := migrator.Up(ctx)
	require.NoError(t, err)
	require.Equal(t, lifecycleMigrations, applied)

	statuses, err := migrator.Status(ctx)
	require.NoError(t, err)
	require.Len(t, statuses, lifecycleMigrations)
	for _, status := range statuses {
		assert.True(t, status.Applied, "migration %d applied", status.Version)
		assert.True(t, status.ChecksumMatch, "migration %d checksum", status.Version)
		assert.True(t, status.HasDownMigration, "migration %d down file", status.Version)
		assert.False(t, status.Dirty, "migration %d dirty", status.Version)
		assert.False(t, status.AppliedAt.IsZero(), "migration %d applied at", status.Version)
	}
	require.NoError(t, migrator.Validate(ctx))

	again, err := migrator.Up(ctx)
	require.NoError(t, err)
	require.Zero(t, again)
}

// seedAndReseed checks a seeder before and after it applies, applies again and reseeds.
//
// Takes database (*sql.DB) which holds the migrated table.
// Takes backend (migrate.Backend) which is the engine's backend.
// Takes files (fstest.MapFS) which holds the seed.
// Takes settle (func(t *testing.T, check func() bool)) which waits until check holds.
func seedAndReseed(t *testing.T, database *sql.DB, backend migrate.Backend, files fstest.MapFS, settle func(t *testing.T, check func() bool)) {
	t.Helper()
	ctx := t.Context()

	seeder, err := migrate.NewSeeder(database, backend, files, seedsDirectory)
	require.NoError(t, err)

	fresh, err := seeder.Status(ctx)
	require.NoError(t, err, "seed status must read a database that has no seed history table")
	require.Len(t, fresh, 1)
	assert.False(t, fresh[0].Applied)

	seeded, err := seeder.Apply(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, seeded)
	require.Equal(t, seededWidgets, CountWidgets(t, database))

	statuses, err := seeder.Status(ctx)
	require.NoError(t, err)
	require.Len(t, statuses, 1)
	assert.True(t, statuses[0].Applied)
	assert.True(t, statuses[0].ChecksumMatch)
	assert.False(t, statuses[0].AppliedAt.IsZero())

	again, err := seeder.Apply(ctx)
	require.NoError(t, err)
	require.Zero(t, again)

	reseeded, err := seeder.Reseed(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, reseeded)
	settle(t, func() bool { return CountWidgets(t, database) == seededWidgets })
}

// refuseEditedMigration checks that validate and up both report a migration edited after
// it was applied, even with nothing pending.
//
// Takes database (*sql.DB) which holds the applied migrations.
// Takes backend (migrate.Backend) which is the engine's backend.
// Takes files (fstest.MapFS) which holds the migrations as they were applied.
func refuseEditedMigration(t *testing.T, database *sql.DB, backend migrate.Backend, files fstest.MapFS) {
	t.Helper()
	ctx := t.Context()

	tampered := fstest.MapFS{}
	maps.Copy(tampered, files)
	tampered[createWidgetsFile] = &fstest.MapFile{Data: append([]byte("-- edited after it was applied\n"), files[createWidgetsFile].Data...)}
	migrator, err := migrate.NewMigrator(database, backend, tampered, migrationsDirectory)
	require.NoError(t, err)

	var mismatch *migrate.ChecksumMismatchError
	require.ErrorAs(t, migrator.Validate(ctx), &mismatch)
	assert.Equal(t, int64(1), mismatch.Version)
	applied, err := migrator.Up(ctx)
	require.ErrorAs(t, err, &mismatch, "up must report an edited applied migration even with nothing pending")
	require.Zero(t, applied)
}

// rollBack rolls the lifecycle's migrations back one at a time.
//
// Takes migrator (*migrate.Migrator) which applied them.
// Takes settle (func(t *testing.T, check func() bool)) which waits until check holds.
func rollBack(t *testing.T, migrator *migrate.Migrator, settle func(t *testing.T, check func() bool)) {
	t.Helper()
	ctx := t.Context()

	rolledBack, err := migrator.Down(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, 1, rolledBack)
	settle(t, func() bool { return AppliedVersions(t, migrator) == "1" })

	rolledBack, err = migrator.DownTo(ctx, 0)
	require.NoError(t, err)
	require.Equal(t, 1, rolledBack)
	settle(t, func() bool { return AppliedVersions(t, migrator) == "" })
}

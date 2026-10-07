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

package sqlexec

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/migrate"
)

var everyConstructor = []namedConstructor{
	{name: "postgres", build: Postgres},
	{name: "postgres_pgbouncer", build: PostgresPgBouncer},
	{name: "cockroachdb", build: CockroachDB},
	{name: "mysql", build: MySQL},
	{name: "mysql_with_dsn", build: func(options ...BackendOption) Backend { return MySQLWithDSN("dsn", options...) }},
	{name: "clickhouse", build: ClickHouse},
	{name: "sqlite", build: SQLite},
	{name: "duckdb", build: DuckDB},
}

type namedConstructor struct {
	build func(options ...BackendOption) Backend
	name  string
}

type recordingStore struct {
	mu      sync.Mutex
	queries []string
}

type recordingConnector struct {
	store *recordingStore
}

type recordingDriver struct {
	store *recordingStore
}

type recordingConn struct {
	store *recordingStore
}

type recordingTx struct{}

type noRows struct{}

func (s *recordingStore) record(query string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries = append(s.queries, query)
}

func (s *recordingStore) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.queries...)
}

func (c *recordingConnector) Connect(_ context.Context) (driver.Conn, error) {
	return &recordingConn{store: c.store}, nil
}

func (c *recordingConnector) Driver() driver.Driver {
	return &recordingDriver{store: c.store}
}

func (d *recordingDriver) Open(_ string) (driver.Conn, error) {
	return &recordingConn{store: d.store}, nil
}

func (*recordingConn) Prepare(_ string) (driver.Stmt, error) {
	return nil, errors.New("Prepare not supported by recordingConn")
}

func (*recordingConn) Close() error { return nil }

func (*recordingConn) Begin() (driver.Tx, error) { return recordingTx{}, nil }

func (*recordingConn) BeginTx(_ context.Context, _ driver.TxOptions) (driver.Tx, error) {
	return recordingTx{}, nil
}

func (c *recordingConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	c.store.record(query)
	return driver.RowsAffected(0), nil
}

func (c *recordingConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.store.record(query)
	return noRows{}, nil
}

func (recordingTx) Commit() error   { return nil }
func (recordingTx) Rollback() error { return nil }

func (noRows) Columns() []string {
	return []string{"version", "name", "checksum", "applied_at", "duration_ms"}
}
func (noRows) Close() error                { return nil }
func (noRows) Next(_ []driver.Value) error { return io.EOF }

func fakeDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database := sql.OpenDB(&retryConnector{store: &retryStore{versions: map[int64]bool{}}})
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestBackendLockModes(t *testing.T) {
	t.Parallel()

	defaults := namesFor(DefaultNamespace)
	advisory := postgresAdvisoryLocks(defaults, false)
	table := postgresTableLocks(defaults, false)
	cockroachTable := cockroachDBTableLocks(defaults, false)
	none := noLocks(defaults, false)
	tests := []struct {
		name      string
		backend   Backend
		wantLocks lockPair
		wantError error
	}{
		{name: "postgres defaults to advisory", backend: Postgres(), wantLocks: advisory},
		{name: "postgres table lock", backend: Postgres(WithLock(LockTable)), wantLocks: table},
		{name: "postgres without a lock", backend: Postgres(WithLock(LockNone)), wantLocks: none},
		{name: "pgbouncer defaults to a table lock", backend: PostgresPgBouncer(), wantLocks: table},
		{name: "pgbouncer back to advisory", backend: PostgresPgBouncer(WithLock(LockAdvisory)), wantLocks: advisory},
		{name: "mysql defaults to a schema-scoped advisory lock", backend: MySQL(), wantLocks: mysqlAdvisoryLocks(defaults, false)},
		{name: "mysql server-wide advisory lock", backend: MySQL(WithServerWideLockKey()), wantLocks: mysqlAdvisoryLocks(defaults, true)},
		{name: "mysql without a lock", backend: MySQL(WithLock(LockNone)), wantLocks: none},
		{name: "mysql table lock is unsupported", backend: MySQL(WithLock(LockTable)), wantError: ErrUnsupportedLock},
		{name: "sqlite advisory lock is unsupported", backend: SQLite(WithLock(LockAdvisory)), wantError: ErrUnsupportedLock},
		{name: "clickhouse table lock is unsupported", backend: ClickHouse(WithLock(LockTable)), wantError: ErrUnsupportedLock},
		{name: "sqlite none is the default", backend: SQLite(WithLock(LockNone)), wantLocks: none},
		{name: "cockroachdb defaults to a durable table lock", backend: CockroachDB(), wantLocks: cockroachTable},
		{name: "cockroachdb without a lock", backend: CockroachDB(WithLock(LockNone)), wantLocks: none},
		{name: "cockroachdb advisory lock is unsupported", backend: CockroachDB(WithLock(LockAdvisory)), wantError: ErrUnsupportedLock},
		{name: "duckdb defaults to no lock", backend: DuckDB(), wantLocks: none},
		{name: "duckdb without a lock", backend: DuckDB(WithLock(LockNone)), wantLocks: none},
		{name: "duckdb advisory lock is unsupported", backend: DuckDB(WithLock(LockAdvisory)), wantError: ErrUnsupportedLock},
		{name: "duckdb table lock is unsupported", backend: DuckDB(WithLock(LockTable)), wantError: ErrUnsupportedLock},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if testCase.wantError != nil {
				require.ErrorIs(t, testCase.backend.err, testCase.wantError)
				assert.ErrorIs(t, testCase.backend.err, migrate.ErrInvalidConfig)
				return
			}
			require.NoError(t, testCase.backend.err)
			assert.Equal(t, testCase.wantLocks, lockPair{migration: testCase.backend.migrationLock, seed: testCase.backend.seedLock})
		})
	}
}

func TestBackendRejectsTablesThatShareAName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		backend Backend
	}{
		{name: "history table named after the seed table", backend: Postgres(WithHistoryTable("sgl_seeds"))},
		{name: "history table named after the migration lock table", backend: PostgresPgBouncer(WithHistoryTable("sgl_migration_lock"))},
		{name: "history table named after the seed lock table", backend: SQLite(WithHistoryTable("sgl_seed_lock"))},
		{name: "names differing only in case", backend: MySQL(WithHistoryTable("SGL_SEEDS"))},
		{name: "namespaced seed table", backend: ClickHouse(WithNamespace("acme"), WithHistoryTable("acme_seeds"))},
		{name: "duckdb history table named after the seed lock table", backend: DuckDB(WithHistoryTable("SGL_SEED_LOCK"))},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.ErrorIs(t, testCase.backend.err, ErrInvalidIdentifier)
			_, err := NewExecutor(fakeDatabase(t), testCase.backend)
			require.ErrorIs(t, err, ErrInvalidIdentifier)
		})
	}
}

func TestBackendSharesNoAcquisitionState(t *testing.T) {
	t.Parallel()

	backend := PostgresPgBouncer()
	first, err := NewExecutor(fakeDatabase(t), backend)
	require.NoError(t, err)
	second, err := NewExecutor(fakeDatabase(t), backend)
	require.NoError(t, err)

	require.Same(t, first.backend.migrationLock, second.backend.migrationLock)
	require.Equal(t, newPostgresTableLock("sgl_migration_lock", "sgl_migration_lock_single_row", "migration table"),
		first.backend.migrationLock, "a lock strategy must hold only its configuration")
}

func TestBackendOptionsComposeInAnyOrder(t *testing.T) {
	t.Parallel()

	first := Postgres(WithNamespace("app"), WithHistoryTable("history"), WithLock(LockTable))
	second := Postgres(WithLock(LockTable), WithHistoryTable("history"), WithNamespace("app"))

	require.NoError(t, first.err)
	assert.Equal(t, first.Names(), second.Names())
	assert.Equal(t, Names{
		History:                 "history",
		Seeds:                   "app_seeds",
		MigrationLock:           "app_migration_lock",
		SeedLock:                "app_seed_lock",
		MigrationLockConstraint: "app_migration_lock_single_row",
		SeedLockConstraint:      "app_seed_lock_single_row",
		LockKey:                 "history",
		SeedLockKey:             "app_seeds",
	}, first.Names())
	assert.Contains(t, createHistorySQL(first), "CREATE TABLE IF NOT EXISTS history (")
}

func TestBackendRecordsInvalidOptions(t *testing.T) {
	t.Parallel()

	database := fakeDatabase(t)
	files := fstest.MapFS{}

	for name, backend := range map[string]Backend{
		"namespace":     SQLite(WithNamespace("bad name")),
		"history table": MySQL(WithHistoryTable("drop;table")),
	} {
		require.ErrorIs(t, backend.err, ErrInvalidIdentifier, name)

		_, err := NewExecutor(database, backend)
		require.ErrorIs(t, err, ErrInvalidIdentifier, name)
		_, err = NewSeedExecutor(database, backend)
		require.ErrorIs(t, err, ErrInvalidIdentifier, name)
		_, err = NewMigrator(database, backend, files, "migrations")
		require.ErrorIs(t, err, ErrInvalidIdentifier, name)
		_, err = NewSeeder(database, backend, files, "seeds")
		require.ErrorIs(t, err, ErrInvalidIdentifier, name)
	}
}

func TestConstructorsRejectUnusableInput(t *testing.T) {
	t.Parallel()

	database := fakeDatabase(t)

	_, err := NewExecutor(database, Backend{})
	require.ErrorIs(t, err, migrate.ErrInvalidConfig)
	_, err = NewSeedExecutor(database, Backend{})
	require.ErrorIs(t, err, migrate.ErrInvalidConfig)
	_, err = NewExecutor(nil, SQLite())
	require.ErrorIs(t, err, migrate.ErrInvalidConfig)
	_, err = NewMigrator(database, SQLite(), nil, "migrations")
	require.ErrorIs(t, err, migrate.ErrInvalidConfig)
}

func TestNewMigratorAndSeederRunAgainstTheDatabase(t *testing.T) {
	t.Parallel()

	database := fakeDatabase(t)
	files := fstest.MapFS{
		"db/migrations/0001_init.up.sql": &fstest.MapFile{Data: []byte("OK1;\n")},
		"db/seeds/0001_rows.sql":         &fstest.MapFile{Data: []byte("OK2;\n")},
	}

	migrator, err := NewMigrator(database, SQLite(), files, "db/migrations", migrate.WithNonBlockingLock())
	require.NoError(t, err)
	statuses, err := migrator.Status(t.Context())
	require.NoError(t, err)
	require.Len(t, statuses, 1)
	assert.Equal(t, "init", statuses[0].Name)

	seeder, err := NewSeeder(database, SQLite(), files, "db/seeds")
	require.NoError(t, err)
	require.NotNil(t, seeder)

	executor, err := NewExecutor(database, Postgres(), WithProgressBatchSize(5))
	require.NoError(t, err)
	assert.Equal(t, 5, executor.progressUpdateBatchSize)
}

func TestLockModeString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "advisory", LockAdvisory.String())
	assert.Equal(t, "table", LockTable.String())
	assert.Equal(t, "none", LockNone.String())
	assert.Equal(t, "LockMode(9)", LockMode(9).String())
}

func renderPersistedText(backend Backend) string {
	executor := newExecutor(nil, backend)
	parts := []string{
		backend.historyTable(),
		backend.seedTable(),
		createHistorySQL(backend),
		createSeedSQL(backend),
		fmt.Sprintf("%#v", backend.migrationLock),
		fmt.Sprintf("%#v", backend.seedLock),
		fmt.Sprintf("%#v", backend.Names()),
		insertSeedSQL(backend, "$1"),
		executor.deleteHistorySQL(),
		newSeedExecutor(nil, backend).clearSeedHistorySQL(),
		executor.appliedVersionsSQL(executor.historyTable(), true),
	}
	return strings.Join(parts, "\n")
}

func TestDefaultNamespaceNames(t *testing.T) {
	t.Parallel()

	expected := Names{
		History:                 "sgl_migrations",
		Seeds:                   "sgl_seeds",
		MigrationLock:           "sgl_migration_lock",
		SeedLock:                "sgl_seed_lock",
		MigrationLockConstraint: "sgl_migration_lock_single_row",
		SeedLockConstraint:      "sgl_seed_lock_single_row",
		LockKey:                 "sgl_migrations",
		SeedLockKey:             "sgl_seeds",
	}
	for _, constructor := range everyConstructor {
		assert.Equalf(t, expected, constructor.build(WithServerWideLockKey()).Names(), "constructor %s", constructor.name)
	}
	assert.Equal(t, expected, Backend{}.Names())
	assert.Equal(t, "sgl_migrations", defaultHistoryTableName)
	assert.Equal(t, "sgl_seeds", defaultSeedTableName)
}

func TestWithNamespaceDerivesEveryPersistedName(t *testing.T) {
	t.Parallel()

	expected := Names{
		History:                 "acme_migrations",
		Seeds:                   "acme_seeds",
		MigrationLock:           "acme_migration_lock",
		SeedLock:                "acme_seed_lock",
		MigrationLockConstraint: "acme_migration_lock_single_row",
		SeedLockConstraint:      "acme_seed_lock_single_row",
		LockKey:                 "acme_migrations",
		SeedLockKey:             "acme_seeds",
	}
	for _, constructor := range everyConstructor {
		for _, scope := range [][]BackendOption{{WithServerWideLockKey()}, nil} {
			backend := constructor.build(append([]BackendOption{WithNamespace("acme")}, scope...)...)
			require.NoErrorf(t, backend.err, "constructor %s", constructor.name)

			if scope != nil {
				assert.Equalf(t, expected, backend.Names(), "constructor %s", constructor.name)
			}
			text := renderPersistedText(backend)
			assert.NotContainsf(t, strings.ToLower(text), "sgl", "constructor %s left a default name behind:\n%s", constructor.name, text)
			assert.Containsf(t, text, "acme_migrations", "constructor %s", constructor.name)
			assert.Containsf(t, text, "acme_seeds", "constructor %s", constructor.name)
		}
	}
}

func TestWithNamespaceRekeysLockStrategies(t *testing.T) {
	t.Parallel()

	acme := namesFor("acme")
	postgres := Postgres(WithNamespace("acme"))
	require.Equal(t, postgresAdvisoryLocks(acme, false), lockPair{migration: postgres.migrationLock, seed: postgres.seedLock})

	mysql := MySQL(WithNamespace("acme"))
	require.Equal(t, mysqlAdvisoryLocks(acme, false), lockPair{migration: mysql.migrationLock, seed: mysql.seedLock})

	pgbouncer := PostgresPgBouncer(WithNamespace("acme"))
	migrationLock, ok := pgbouncer.migrationLock.(*tableLock)
	require.True(t, ok)
	require.Equal(t, "acme_migration_lock", migrationLock.table)
	require.Contains(t, migrationLock.createSQL, "CREATE TABLE IF NOT EXISTS acme_migration_lock (")
	require.Contains(t, migrationLock.createSQL, "CONSTRAINT acme_migration_lock_single_row CHECK")
	seedLock, ok := pgbouncer.seedLock.(*tableLock)
	require.True(t, ok)
	require.Equal(t, "acme_seed_lock", seedLock.table)
	require.Contains(t, seedLock.createSQL, "CREATE TABLE IF NOT EXISTS acme_seed_lock (")
	require.Contains(t, seedLock.createSQL, "CONSTRAINT acme_seed_lock_single_row CHECK")
}

func TestWithNamespaceRejectsUnsafeNamespaces(t *testing.T) {
	t.Parallel()

	for _, namespace := range []string{"", "1acme", "acme-db", "acme db", "acme;drop", `"acme"`, strings.Repeat("a", 40), strings.Repeat("a", 64)} {
		backend := PostgresPgBouncer(WithNamespace(namespace))
		require.ErrorIsf(t, backend.err, ErrInvalidIdentifier, "namespace %q", namespace)
		require.Equalf(t, PostgresPgBouncer().Names(), backend.Names(), "namespace %q must leave the default names", namespace)
	}
}

func TestWithNamespaceAcceptsLongestSafeNamespace(t *testing.T) {
	t.Parallel()

	namespace := strings.Repeat("a", 37)
	backend := PostgresPgBouncer(WithNamespace(namespace))
	require.NoError(t, backend.err)
	require.Equal(t, namespace+"_migration_lock_single_row", backend.Names().MigrationLockConstraint)
}

func TestWithNamespaceAndWithHistoryTableAreOrderIndependent(t *testing.T) {
	t.Parallel()

	for _, constructor := range everyConstructor {
		historyFirst := constructor.build(WithHistoryTable("app_history"), WithNamespace("acme"))
		namespaceFirst := constructor.build(WithNamespace("acme"), WithHistoryTable("app_history"))
		require.NoError(t, historyFirst.err)

		assert.Equalf(t, renderPersistedText(namespaceFirst), renderPersistedText(historyFirst), "constructor %s", constructor.name)
		assert.Equalf(t, "app_history", historyFirst.historyTable(), "constructor %s", constructor.name)
		assert.Equalf(t, "acme_seeds", historyFirst.seedTable(), "constructor %s", constructor.name)
		assert.Containsf(t, historyFirst.Names().LockKey, "app_history", "constructor %s", constructor.name)
		assert.Containsf(t, createHistorySQL(historyFirst), "CREATE TABLE IF NOT EXISTS app_history (", "constructor %s", constructor.name)
	}

	renamed := Postgres(WithHistoryTable("app_history"), WithNamespace("acme"))
	require.Equal(t, &postgresAdvisoryLock{key: "app_history", label: "PostgreSQL"}, renamed.migrationLock)
	require.Equal(t, &postgresAdvisoryLock{key: "acme_seeds", label: "PostgreSQL seed"}, renamed.seedLock)
}

func TestWithHistoryTableKeepsDefaultNameAcrossNamespace(t *testing.T) {
	t.Parallel()

	pinned := Postgres(WithHistoryTable(defaultHistoryTableName), WithNamespace("acme"))

	require.NoError(t, pinned.err)
	require.Equal(t, defaultHistoryTableName, pinned.historyTable())
	require.Equal(t, "acme_seeds", pinned.seedTable())
}

func TestWithNamespaceLastOptionWins(t *testing.T) {
	t.Parallel()

	for _, constructor := range everyConstructor {
		twice := constructor.build(WithNamespace("first"), WithNamespace("second"))
		once := constructor.build(WithNamespace("second"))

		assert.Equalf(t, renderPersistedText(once), renderPersistedText(twice), "constructor %s", constructor.name)
		assert.Equalf(t, renderPersistedText(constructor.build()), renderPersistedText(constructor.build(WithNamespace(DefaultNamespace))), "constructor %s", constructor.name)
	}
}

func TestSeedExecutorUsesNamespacedSeedTable(t *testing.T) {
	t.Parallel()

	store := &recordingStore{}
	database := sql.OpenDB(&recordingConnector{store: store})
	t.Cleanup(func() { require.NoError(t, database.Close()) })

	executor := newSeedExecutor(database, SQLite(WithNamespace("acme")))
	ctx := t.Context()

	require.NoError(t, executor.EnsureSeedTable(ctx))
	_, err := executor.AppliedSeeds(ctx)
	require.NoError(t, err)
	require.NoError(t, executor.ExecuteSeed(ctx, migrate.SeedRecord{Name: "seed", Checksum: "abc", Content: []byte("SELECT 1"), Version: 1}))
	require.NoError(t, executor.ClearSeedHistory(ctx))

	queries := store.recorded()
	require.NotEmpty(t, queries)
	seedTableQueries := 0
	for _, query := range queries {
		assert.NotContains(t, query, "sgl", "query %q used a default name", query)
		if strings.Contains(query, "acme_seeds") {
			seedTableQueries++
		}
	}
	assert.Equal(t, 4, seedTableQueries, "create, select, insert and delete must all target acme_seeds: %v", queries)
}

func TestAcmeNamespaceLeavesNoSeagullPersistedNames(t *testing.T) {
	t.Parallel()

	seagullVocabulary := regexp.MustCompile(`(?i)sgl|seagull`)
	for _, constructor := range everyConstructor {
		backend := constructor.build(WithNamespace("acme"))
		require.NoError(t, backend.err, constructor.name)

		names := backend.Names()
		persisted := []string{
			names.History, names.Seeds, names.MigrationLock, names.SeedLock,
			names.MigrationLockConstraint, names.SeedLockConstraint, names.LockKey, names.SeedLockKey,
			renderPersistedText(backend),
		}
		for _, text := range persisted {
			assert.NotRegexpf(t, seagullVocabulary, text, "%s persists seagull vocabulary", constructor.name)
		}
		assert.Equal(t, "acme_migrations", names.History, constructor.name)
		assert.Equal(t, "acme_seeds", names.Seeds, constructor.name)
		assert.Contains(t, createSeedSQL(backend), "acme_seeds", constructor.name)
	}
}

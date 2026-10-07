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
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/migrate"
	"seagull.sh/seagull/internal/sqllex"
)

var errRulesFault = errors.New("rules fault")

func createHistorySQL(backend Backend) string {
	return backend.engine.createHistoryTable(backend.historyTable())
}

func createSeedSQL(backend Backend) string {
	return backend.engine.createSeedTable(backend.seedTable())
}

func insertSeedSQL(backend Backend, placeholder string) string {
	return backend.engine.insertSeed(backend.seedTable(), placeholder, placeholder, placeholder, placeholder)
}

func TestBuiltInEnginesPassValidation(t *testing.T) {
	t.Parallel()

	duckDB, err := engineFromSpec(duckDBSpec)
	require.NoError(t, err)
	for name, built := range map[string]*engine{
		"duckdb":             duckDB,
		"postgres":           postgresEngine(),
		"postgres_pgbouncer": postgresPgBouncerEngine(),
		"cockroachdb":        cockroachDBEngine(),
		"timescaledb":        timescaleDBEngine(),
		"mysql":              mysqlEngine(),
		"clickhouse":         clickHouseEngine(),
		"sqlite":             sqliteEngine(),
	} {
		require.NoError(t, validateEngine(built), name)
	}
}

func TestPostgresBackendHasExpectedSQL(t *testing.T) {
	t.Parallel()

	backend := Postgres()

	require.Contains(t, createHistorySQL(backend), "sgl_migrations")
	require.Contains(t, createHistorySQL(backend), "TIMESTAMPTZ")
	require.Contains(t, createSeedSQL(backend), "sgl_seeds")
	require.IsType(t, &postgresAdvisoryLock{}, backend.migrationLock)
	require.Equal(t, "$1", backend.placeholder(1))
	require.Equal(t, "$5", backend.placeholder(5))
	require.True(t, backend.engine.multiStatementSeeds)
	require.True(t, backend.engine.transactionalDDL)
}

func TestPostgresPgBouncerBackendUsesTableLock(t *testing.T) {
	t.Parallel()

	backend := PostgresPgBouncer()

	require.IsType(t, &tableLock{}, backend.migrationLock)
	require.IsType(t, &tableLock{}, backend.seedLock)
	require.Equal(t, "$1", backend.placeholder(1))
	require.Contains(t, createHistorySQL(backend), "sgl_migrations")
}

func TestMySQLBackendUsesQuestionPlaceholdersAndAdvisoryLock(t *testing.T) {
	t.Parallel()

	backend := MySQL()

	require.IsType(t, &pollingAdvisoryLock{}, backend.migrationLock)
	require.Equal(t, "?", backend.placeholder(1))
	require.Equal(t, "?", backend.placeholder(99))
	require.False(t, backend.engine.multiStatementSeeds)
	require.Contains(t, createHistorySQL(backend), "ENGINE=InnoDB")
}

func TestMySQLWithDSNTogglesSeedSplittingBasedOnDSN(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		dsn       string
		wantSplit bool
	}{
		{"plain DSN keeps splitting", "user:pass@tcp(localhost)/db", true},
		{"DSN with multiStatements disables splitting", "user:pass@tcp(localhost)/db?multiStatements=true", false},
		{"unrelated query parameters keep splitting", "user:pass@tcp(localhost)/db?parseTime=true", true},
		{"multiStatements=1 disables splitting", "user:pass@tcp(localhost)/db?parseTime=true&multiStatements=1", false},
		{"multiStatements=True disables splitting", "user:pass@tcp(localhost)/db?multiStatements=True", false},
		{"multiStatements=false keeps splitting", "user:pass@tcp(localhost)/db?multiStatements=false", true},
		{"a password holding the parameter keeps splitting", "user:multiStatements=true@tcp(localhost)/db", true},
		{"a database name holding the parameter keeps splitting", "user:pass@tcp(localhost)/multiStatements=true", true},
		{"an unparsable value keeps splitting", "user:pass@tcp(localhost)/db?multiStatements=%zz", true},
		{"no slash keeps splitting", "multiStatements=true", true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.wantSplit, !MySQLWithDSN(testCase.dsn).engine.multiStatementSeeds)
		})
	}
}

func TestWithDriverMultiStatementsSetsHowSeedsRun(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		backend Backend
		whole   bool
	}{
		{name: "mysql splits by default", backend: MySQL(), whole: false},
		{name: "mysql runs whole on request", backend: MySQL(WithDriverMultiStatements(true)), whole: true},
		{name: "postgres runs whole by default", backend: Postgres(), whole: true},
		{name: "postgres splits on request", backend: Postgres(WithDriverMultiStatements(false)), whole: false},
		{name: "an explicit option overrides the DSN", backend: MySQLWithDSN("u@tcp(h)/db?multiStatements=true", WithDriverMultiStatements(false)), whole: false},
		{name: "duckdb splits on request", backend: DuckDB(WithDriverMultiStatements(false)), whole: false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, testCase.backend.err)
			require.Equal(t, testCase.whole, testCase.backend.engine.multiStatementSeeds)
		})
	}
}

func TestMySQLLockKeysAreScopedToTheSchema(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		backend         Backend
		wantLockKey     string
		wantSeedLockKey string
		wantNameSQL     string
	}{
		{
			name:            "scoped by default",
			backend:         MySQL(),
			wantLockKey:     "CONCAT(COALESCE(DATABASE(), ''), '.', 'sgl_migrations')",
			wantSeedLockKey: "CONCAT(COALESCE(DATABASE(), ''), '.', 'sgl_seeds')",
			wantNameSQL: "SELECT IF(CHAR_LENGTH(CONCAT(COALESCE(DATABASE(), ''), '.', 'sgl_migrations')) > 64, " +
				"SHA2(CONCAT(COALESCE(DATABASE(), ''), '.', 'sgl_migrations'), 256), " +
				"CONCAT(COALESCE(DATABASE(), ''), '.', 'sgl_migrations'))",
		},
		{
			name:            "server-wide on request",
			backend:         MySQL(WithNamespace("acme"), WithServerWideLockKey()),
			wantLockKey:     "acme_migrations",
			wantSeedLockKey: "acme_seeds",
			wantNameSQL:     "",
		},
		{
			name:            "server-wide with a dsn",
			backend:         MySQLWithDSN("user@tcp(localhost)/app", WithNamespace("acme"), WithServerWideLockKey()),
			wantLockKey:     "acme_migrations",
			wantSeedLockKey: "acme_seeds",
			wantNameSQL:     "",
		},
		{
			name:            "other engines ignore the option",
			backend:         Postgres(WithServerWideLockKey()),
			wantLockKey:     "sgl_migrations",
			wantSeedLockKey: "sgl_seeds",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.NoError(t, testCase.backend.err)
			require.Equal(t, testCase.wantLockKey, testCase.backend.Names().LockKey)
			require.Equal(t, testCase.wantSeedLockKey, testCase.backend.Names().SeedLockKey)
			if lock, ok := testCase.backend.migrationLock.(*pollingAdvisoryLock); ok {
				require.Equal(t, testCase.wantNameSQL, lock.nameSQL)
				require.Equal(t, "SELECT GET_LOCK(?, 5)", lock.waitSQL)
				require.Equal(t, "SELECT RELEASE_LOCK(?)", lock.releaseSQL)
			}
		})
	}
}

func TestSQLiteBackendUsesNoOpLockAndQuestionPlaceholders(t *testing.T) {
	t.Parallel()

	backend := SQLite()

	require.IsType(t, &noOpLock{}, backend.migrationLock)
	require.Equal(t, "?", backend.placeholder(1))
	require.True(t, backend.engine.multiStatementSeeds)
	require.Contains(t, createHistorySQL(backend), "sgl_migrations")
	require.Contains(t, createSeedSQL(backend), "sgl_seeds")
}

func TestDuckDBBackendUsesNoOpLockAndNumberedPlaceholders(t *testing.T) {
	t.Parallel()

	backend := DuckDB()

	require.NoError(t, backend.err)
	require.Equal(t, "DuckDB", backend.engine.name)
	require.Equal(t, LockNone, backend.lockMode)
	require.IsType(t, &noOpLock{}, backend.migrationLock)
	require.IsType(t, &noOpLock{}, backend.seedLock)
	require.Equal(t, "$1", backend.placeholder(1))
	require.Equal(t, "$8", backend.placeholder(8))
	require.True(t, backend.engine.transactionalDDL, "DuckDB runs DDL inside transactions")
	require.True(t, backend.engine.multiStatementSeeds, "duckdb-go runs every statement of one Exec")
	require.False(t, backend.engine.appendOnlyHistory)
	require.Empty(t, backend.engine.historyReadModifier)

	require.Contains(t, createHistorySQL(backend), "CREATE TABLE IF NOT EXISTS sgl_migrations (")
	require.Contains(t, createHistorySQL(backend), "TIMESTAMPTZ NOT NULL DEFAULT current_timestamp")
	require.Contains(t, createSeedSQL(backend), "CREATE TABLE IF NOT EXISTS sgl_seeds (")
	require.Equal(t,
		"INSERT INTO sgl_seeds (version, name, checksum, duration_ms) VALUES ($1, $1, $1, $1) ON CONFLICT (version) DO NOTHING",
		insertSeedSQL(backend, "$1"),
	)
	require.Equal(t,
		"SELECT count(*) FROM information_schema.tables WHERE table_catalog = current_database() "+
			"AND table_schema = current_schema() AND lower(table_name) = lower($1)",
		backend.engine.tableExists(backend.placeholder(1)),
		"the probe must look only in the session's current database and schema, ignoring case as DuckDB does",
	)
	require.Equal(t, "DELETE FROM sgl_migrations WHERE version = $1", newExecutor(nil, backend).deleteHistorySQL())
	require.Equal(t, "DELETE FROM sgl_seeds", newSeedExecutor(nil, backend).clearSeedHistorySQL())
}

func TestBackendHistoryTableFallsBackToDefault(t *testing.T) {
	t.Parallel()

	require.Equal(t, "sgl_migrations", Backend{}.historyTable())
	for _, backend := range []Backend{Postgres(), SQLite(), MySQL(), PostgresPgBouncer(), ClickHouse(), CockroachDB(), DuckDB()} {
		require.Equal(t, "sgl_migrations", backend.historyTable())
	}
}

func TestWithHistoryTableRenamesDDLAndLockKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		backend Backend
		table   string
		lock    lockStrategy
	}{
		{
			name:    "postgres",
			backend: Postgres(WithHistoryTable("identity_sgl_migrations")),
			table:   "identity_sgl_migrations",
			lock:    &postgresAdvisoryLock{key: "identity_sgl_migrations", label: "PostgreSQL"},
		},
		{
			name:    "sqlite",
			backend: SQLite(WithHistoryTable("scheduler_sgl_migrations")),
			table:   "scheduler_sgl_migrations",
			lock:    &noOpLock{},
		},
		{
			name:    "mysql",
			backend: MySQL(WithHistoryTable("content_sgl_migrations"), WithServerWideLockKey()),
			table:   "content_sgl_migrations",
			lock:    newMySQLAdvisoryLock("content_sgl_migrations", "MySQL", true),
		},
		{
			name:    "chained renames keep the last",
			backend: Postgres(WithHistoryTable("first_name"), WithHistoryTable("second_name")),
			table:   "second_name",
			lock:    &postgresAdvisoryLock{key: "second_name", label: "PostgreSQL"},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.NoError(t, testCase.backend.err)
			require.Equal(t, testCase.table, testCase.backend.historyTable())
			require.Contains(t, createHistorySQL(testCase.backend), "CREATE TABLE IF NOT EXISTS "+testCase.table+" (")
			require.NotContains(t, createHistorySQL(testCase.backend), "EXISTS sgl_migrations")
			require.Contains(t, createSeedSQL(testCase.backend), "sgl_seeds")
			require.Equal(t, testCase.lock, testCase.backend.migrationLock)
		})
	}
}

func TestWithHistoryTableEmptyNameIsNoop(t *testing.T) {
	t.Parallel()

	require.Equal(t, Postgres().Names(), Postgres(WithHistoryTable("")).Names())
}

func TestClickHouseBackendUsesNoOpLockAndAppendOnlyHistory(t *testing.T) {
	t.Parallel()

	backend := ClickHouse()

	require.IsType(t, &noOpLock{}, backend.migrationLock)
	require.IsType(t, &noOpLock{}, backend.seedLock)
	require.Equal(t, "?", backend.placeholder(1))
	require.Equal(t, "?", backend.placeholder(99))
	require.False(t, backend.engine.multiStatementSeeds)

	require.False(t, backend.engine.transactionalDDL, "ClickHouse has no DDL transactions")
	require.True(t, backend.engine.appendOnlyHistory, "ClickHouse history must be append-only")
	require.Equal(t, " FINAL", backend.engine.historyReadModifier, "ClickHouse reads history with FINAL")
	require.Contains(t, createHistorySQL(backend), "ENGINE = ReplacingMergeTree(applied_at)")
	require.Contains(t, createSeedSQL(backend), "ENGINE = ReplacingMergeTree(applied_at)")

	executor := newExecutor(nil, backend)
	require.Equal(t,
		"ALTER TABLE sgl_migrations DELETE WHERE version = ? SETTINGS mutations_sync = 2",
		executor.deleteHistorySQL(),
		"a rollback must remove the history row before it returns",
	)
	require.Equal(t, "TRUNCATE TABLE sgl_seeds", newSeedExecutor(nil, backend).clearSeedHistorySQL())
}

func TestDeleteHistorySQLDefaultsToStandardDelete(t *testing.T) {
	t.Parallel()

	require.Equal(t, "DELETE FROM sgl_migrations WHERE version = $1", newExecutor(nil, Postgres()).deleteHistorySQL())
	require.Equal(t, "DELETE FROM app_history WHERE version = ?", newExecutor(nil, MySQL(WithHistoryTable("app_history"))).deleteHistorySQL())
}

func TestSeedExecutor_ClearSeedHistorySQL(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		backend  Backend
		expected string
	}{
		{name: "postgres", backend: Postgres(), expected: "DELETE FROM sgl_seeds"},
		{name: "mysql", backend: MySQL(), expected: "DELETE FROM sgl_seeds"},
		{name: "sqlite", backend: SQLite(), expected: "DELETE FROM sgl_seeds"},
		{name: "clickhouse", backend: ClickHouse(), expected: "TRUNCATE TABLE sgl_seeds"},
		{name: "clickhouse namespaced", backend: ClickHouse(WithNamespace("acme")), expected: "TRUNCATE TABLE acme_seeds"},
		{name: "duckdb", backend: DuckDB(), expected: "DELETE FROM sgl_seeds"},
		{name: "postgres namespaced", backend: Postgres(WithNamespace("acme")), expected: "DELETE FROM acme_seeds"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			executor := newSeedExecutor(nil, testCase.backend)

			require.Equal(t, testCase.expected, executor.clearSeedHistorySQL())
		})
	}
}

func TestClickHouseSeedInsertIsIdempotent(t *testing.T) {
	t.Parallel()

	sqlText := insertSeedSQL(ClickHouse(), "?")
	require.Contains(t, sqlText, "INSERT INTO sgl_seeds")
	require.Contains(t, sqlText, "NOT IN (SELECT version FROM sgl_seeds)",
		"the guard must filter the candidate row out when the version is already recorded")

	require.Equal(t, 4, strings.Count(sqlText, "?"),
		"the rendered SQL must take exactly four positional placeholders")
}

func TestWithHistoryTableRejectsUnsafeIdentifiers(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"sgl_migrations; DROP TABLE users",
		"name with space",
		"name'with'quote",
		"name\"with\"double",
		"1starts_with_digit",
		"--comment",
		"sgl-migrations",
		string(make([]byte, 64)),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			renamed := Postgres(WithHistoryTable(name))
			require.ErrorIs(t, renamed.err, ErrInvalidIdentifier)
			require.Equal(t, defaultHistoryTableName, renamed.historyTable(),
				"a refused rename must leave the default name")
		})
	}
}

func TestLexicalSplitterTellsACallbackPanicFromMalformedSQL(t *testing.T) {
	t.Parallel()

	panickingRules := sqllex.Rules{ProceduralBlock: func([]string) bool { panic(errRulesFault) }}
	tests := []struct {
		name          string
		rules         sqllex.Rules
		content       string
		wantCallback  bool
		wantMalformed bool
	}{
		{name: "a panicking procedural block callback", rules: panickingRules, content: "CREATE TRIGGER t BEGIN SELECT 1; END;", wantCallback: true},
		{name: "an unterminated string", rules: sqllex.Rules{}, content: "SELECT 'open", wantMalformed: true},
		{name: "well-formed SQL", rules: panickingRules, content: "SELECT 1; SELECT 2;"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			spec := minimalSpec()
			spec.Rules = testCase.rules
			var logs bytes.Buffer
			ctx := logging.ContextWithLogger(t.Context(), slog.New(slog.NewJSONHandler(&logs, nil)))
			backend := Custom(spec)
			_, err := backend.engine.split(ctx, testCase.content)

			wantRecords := 0
			if testCase.wantCallback {
				wantRecords = 1
			}
			assert.Equal(t, wantRecords, strings.Count(logs.String(), "lexical rules callback ProceduralBlock"),
				"a callback panic is logged once, with its stack")
			if testCase.wantCallback {
				assert.Contains(t, logs.String(), "withLoggedPanics")
				assert.NotContains(t, err.Error(), "goroutine", "the error carries no stack")
			}

			require.Equal(t, testCase.wantCallback, errors.Is(err, sqllex.ErrRulesCallback))
			require.Equal(t, testCase.wantMalformed, errors.Is(err, ErrMalformedStatement))
			if testCase.wantCallback {
				require.ErrorIs(t, err, errRulesFault)
				require.ErrorContains(t, err, "lexical rules callback ProceduralBlock panicked")
			}
			if !testCase.wantCallback && !testCase.wantMalformed {
				require.NoError(t, err)
			}
		})
	}
}

func TestRequiresNoTransactionReturnsAPredicatePanicAsAnError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		predicate    sqllex.StatementPredicate
		name         string
		wantRequired bool
		wantPanic    bool
	}{
		{name: "a panicking predicate", predicate: func([]sqllex.Token) bool { panic(errRulesFault) }, wantPanic: true},
		{name: "a predicate that matches", predicate: func(tokens []sqllex.Token) bool { return len(tokens) > 0 && tokens[0].IsWord("VACUUM") }, wantRequired: true},
		{name: "a predicate that does not match", predicate: func([]sqllex.Token) bool { return false }},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var logs bytes.Buffer
			ctx := logging.ContextWithLogger(t.Context(), slog.New(slog.NewJSONHandler(&logs, nil)))
			built := &engine{rules: sqllex.Rules{RequiresNoTransaction: testCase.predicate}}

			var required bool
			var err error
			require.NotPanics(t, func() { required, err = built.requiresNoTransaction(ctx, []string{"SELECT 1", "VACUUM"}) })

			require.Equal(t, testCase.wantRequired, required)
			if !testCase.wantPanic {
				require.NoError(t, err)
				require.Empty(t, logs.String())
				return
			}
			require.ErrorIs(t, err, sqllex.ErrRulesCallback)
			require.ErrorIs(t, err, errRulesFault)
			require.ErrorContains(t, err, "checking statement 1 of 2: lexical rules callback RequiresNoTransaction panicked")
			require.Equal(t, 1, strings.Count(logs.String(), `"msg":"recovered a panic"`), "the stack is logged once")
		})
	}
}

func TestAMigrationWhoseRulesPanicRunsNothing(t *testing.T) {
	t.Parallel()

	spec := minimalSpec()
	spec.Rules = sqllex.Rules{RequiresNoTransaction: func([]sqllex.Token) bool { panic(errRulesFault) }}
	store := newProgressStore()
	database := store.open()
	t.Cleanup(func() { _ = database.Close() })
	executor, err := NewExecutor(database, Custom(spec))
	require.NoError(t, err)
	record := migrate.MigrationRecord{Name: "m", Checksum: "c", Content: []byte("STMT_0 a;"), Version: 4, SkipUpTo: -1}

	err = executor.ExecuteMigration(t.Context(), record, migrate.MigrationDirectionUp, true)

	require.ErrorIs(t, err, sqllex.ErrRulesCallback)
	require.ErrorContains(t, err, "checking whether migration 4 can run in a transaction")
	require.Empty(t, store.userStatements())
}

func TestMySQLMigrationsTakeTheDirtyTrackedPath(t *testing.T) {
	t.Parallel()

	require.False(t, MySQL().engine.transactionalDDL, "MySQL commits DDL implicitly, so its migrations must be tracked statement by statement")
}

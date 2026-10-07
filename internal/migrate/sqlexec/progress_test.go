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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/migrate"
	"seagull.sh/seagull/internal/sqllex"
)

var sixStatementMigration = fstest.MapFS{
	"migrations/0001_boats.up.sql": {Data: sixStatementContent()},
}

func digestOf(statements []string) string {
	digest := newStatementDigest()
	for _, statement := range statements {
		digest.add(statement)
	}
	return digest.sum()
}

func TestResumeDigest(t *testing.T) {
	t.Parallel()

	statements := []string{"STMT_0 a", "STMT_1 b", "STMT_2 c"}
	testCases := []struct {
		name          string
		record        migrate.MigrationRecord
		wantMismatch  bool
		wantReason    string
		wantCompleted []string
	}{
		{
			name:          "a first run resumes nothing",
			record:        migrate.MigrationRecord{SkipUpTo: -1},
			wantCompleted: nil,
		},
		{
			name:          "matching progress resumes",
			record:        migrate.MigrationRecord{SkipUpTo: 1, SplitterVersion: splitterVersion, StatementsHash: digestOf(statements[:2])},
			wantCompleted: statements[:2],
		},
		{
			name:          "every statement completed resumes",
			record:        migrate.MigrationRecord{SkipUpTo: 2, SplitterVersion: splitterVersion, StatementsHash: digestOf(statements)},
			wantCompleted: statements,
		},
		{
			name:         "progress recorded without a digest is refused",
			record:       migrate.MigrationRecord{SkipUpTo: 0},
			wantMismatch: true,
			wantReason:   "recorded without the statements it covers",
		},
		{
			name:         "progress from another splitter version is refused",
			record:       migrate.MigrationRecord{SkipUpTo: 0, SplitterVersion: splitterVersion + 1, StatementsHash: digestOf(statements[:1])},
			wantMismatch: true,
			wantReason:   "statement splitter version 2",
		},
		{
			name:         "progress past the last statement is refused",
			record:       migrate.MigrationRecord{SkipUpTo: 3, SplitterVersion: splitterVersion, StatementsHash: digestOf(statements)},
			wantMismatch: true,
			wantReason:   "only 3 statements",
		},
		{
			name:         "a different digest is refused",
			record:       migrate.MigrationRecord{SkipUpTo: 1, SplitterVersion: splitterVersion, StatementsHash: digestOf([]string{"STMT_0 a", "STMT_1 x"})},
			wantMismatch: true,
			wantReason:   "no longer match",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			testCase.record.Name = "boats"
			testCase.record.Version = 7
			digest, err := resumeDigest(testCase.record, statements)
			if !testCase.wantMismatch {
				require.NoError(t, err)
				require.Equal(t, digestOf(testCase.wantCompleted), digest.sum())
				return
			}
			var mismatch *migrate.ResumeMismatchError
			require.ErrorAs(t, err, &mismatch)
			require.Equal(t, int64(7), mismatch.Version)
			require.Equal(t, testCase.record.SkipUpTo, mismatch.LastStatement)
			require.Equal(t, splitterVersion, mismatch.SplitterVersion)
			require.Equal(t, len(statements), mismatch.StatementCount)
			require.ErrorContains(t, err, testCase.wantReason)
			require.ErrorContains(t, err, "then repair its history")
		})
	}
}

func TestStatementDigestSeparatesStatements(t *testing.T) {
	t.Parallel()

	require.NotEqual(t, digestOf([]string{"ab", "c"}), digestOf([]string{"a", "bc"}))
	require.NotEqual(t, digestOf(nil), digestOf([]string{""}))
	require.Equal(t, digestOf([]string{"a", "b"}), digestOf([]string{"a", "b"}))
}

func sixStatementContent() []byte {
	return append([]byte("-- sgl.migration(no_transaction: true)\n"), buildStatements(6)...)
}

func TestInterruptedMigrationResumesAtTheExactStatement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		options      []ExecutorOption
		wantRecorded int64
		wantRerun    []string
	}{
		{
			name:         "every statement is checkpointed by default",
			options:      nil,
			wantRecorded: 2,
			wantRerun:    []string{"STMT_3 col3", "STMT_4 col4", "STMT_5 col5"},
		},
		{
			name:         "a batch loses the statements since its last checkpoint",
			options:      []ExecutorOption{WithProgressBatchSize(5)},
			wantRecorded: -1,
			wantRerun:    []string{"STMT_0 col0", "STMT_1 col1", "STMT_2 col2", "STMT_3 col3", "STMT_4 col4", "STMT_5 col5"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			store := newProgressStore()
			database := store.open()
			t.Cleanup(func() { _ = database.Close() })
			executor, err := NewExecutor(database, SQLite(), testCase.options...)
			require.NoError(t, err)
			migrator, err := migrate.New(executor, sixStatementMigration, "migrations")
			require.NoError(t, err)

			store.loseConnectionAt("STMT_3 col3")
			_, upError := migrator.Up(t.Context())
			require.ErrorIs(t, upError, errConnectionLost)
			row, exists := store.row(1)
			require.True(t, exists)
			require.True(t, row.dirty)
			require.NotNil(t, row.lastStatement)
			require.Equal(t, testCase.wantRecorded, *row.lastStatement)

			store.reconnect()
			store.resetExecuted()
			applied, err := migrator.Up(t.Context())
			require.NoError(t, err)
			require.Equal(t, 1, applied)
			require.Equal(t, testCase.wantRerun, store.userStatements())
			row, _ = store.row(1)
			require.False(t, row.dirty)
			require.Equal(t, int64(5), *row.lastStatement)
			require.Equal(t, digestOf([]string{"STMT_0 col0", "STMT_1 col1", "STMT_2 col2", "STMT_3 col3", "STMT_4 col4", "STMT_5 col5"}), *row.statementsHash)
		})
	}
}

func TestMySQLStatementTwoOfThreeFailsThenResumes(t *testing.T) {
	t.Parallel()

	store := newProgressStore()
	database := store.open()
	t.Cleanup(func() { _ = database.Close() })
	files := fstest.MapFS{
		"migrations/0001_harbours.up.sql": {Data: []byte("STMT_0 harbours;\nSTMT_1 unique_index;\nSTMT_2 padstow;\n")},
	}
	migrator, err := NewMigrator(database, MySQL(WithLock(LockNone)), files, "migrations")
	require.NoError(t, err)

	store.failOn("STMT_1 unique_index", errors.New("Duplicate entry 'Gull' for key 'boats_name_key'"))
	applied, err := migrator.Up(t.Context())

	require.ErrorAs(t, err, new(*migrate.MigrationExecutionError))
	require.ErrorContains(t, err, "statement 2/3")
	require.Zero(t, applied)
	row, exists := store.row(1)
	require.True(t, exists, "MySQL commits DDL implicitly, so the failed migration must stay recorded as dirty")
	require.True(t, row.dirty)
	require.Equal(t, int64(0), *row.lastStatement)
	require.Zero(t, store.transactionCount(), "MySQL migrations must not run in a transaction")

	statuses, err := migrator.Status(t.Context())
	require.NoError(t, err)
	require.True(t, statuses[0].Dirty)

	store.resetExecuted()
	applied, err = migrator.Up(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, applied)
	require.Equal(t, []string{"STMT_1 unique_index", "STMT_2 padstow"}, store.userStatements())
	row, _ = store.row(1)
	require.False(t, row.dirty)
}

func TestLegacyDirtyMigrationIsUpgradedAndGuarded(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		lastStatement int64
		wantMismatch  bool
		wantRun       []string
	}{
		{
			name:          "progress recorded by an earlier splitter is not resumed",
			lastStatement: 0,
			wantMismatch:  true,
			wantRun:       nil,
		},
		{
			name:          "a migration that completed no statement runs from the start",
			lastStatement: -1,
			wantMismatch:  false,
			wantRun:       []string{"STMT_0 col0", "STMT_1 col1", "STMT_2 col2", "STMT_3 col3", "STMT_4 col4", "STMT_5 col5"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			store := newProgressStore()
			store.createLegacyTable()
			store.setRow(1, progressRow{name: "boats", checksum: checksumOf(sixStatementContent()), dirty: true, lastStatement: &testCase.lastStatement})
			database := store.open()
			t.Cleanup(func() { _ = database.Close() })
			migrator, err := NewMigrator(database, SQLite(), sixStatementMigration, "migrations")
			require.NoError(t, err)

			statuses, err := migrator.Status(t.Context())
			require.NoError(t, err, "status must read a history table that predates the progress columns")
			require.True(t, statuses[0].Dirty)
			require.False(t, store.hasColumns(splitterVersionColumn), "status must not upgrade the table")

			_, upError := migrator.Up(t.Context())

			require.True(t, store.hasColumns(splitterVersionColumn, statementsHashColumn), "up must upgrade the table under the lock")
			require.Equal(t, testCase.wantRun, store.userStatements())
			if !testCase.wantMismatch {
				require.NoError(t, upError)
				row, _ := store.row(1)
				require.False(t, row.dirty)
				return
			}
			var mismatch *migrate.ResumeMismatchError
			require.ErrorAs(t, upError, &mismatch)
			require.Equal(t, int64(1), mismatch.Version)
			require.Zero(t, mismatch.RecordedSplitterVersion)
			row, _ := store.row(1)
			require.True(t, row.dirty, "a refused migration stays dirty")
		})
	}
}

func checksumOf(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func minimalSpec() BackendSpec {
	spec := testBackendSpec()
	spec.AdvisoryLock = nil
	spec.TableLock = nil
	spec.DefaultLock = LockNone
	return spec
}

func TestHistoryUpgradeToleratesAConcurrentUpgrade(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		addedByOther bool
		wantError    bool
	}{
		{name: "another process added the column first", addedByOther: true, wantError: false},
		{name: "the column cannot be added", addedByOther: false, wantError: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			store := newProgressStore()
			store.refuseAlter = true
			store.addedByOther = testCase.addedByOther
			database := store.open()
			t.Cleanup(func() { _ = database.Close() })
			executor, err := NewExecutor(database, SQLite())
			require.NoError(t, err)

			ensureError := executor.EnsureMigrationTable(t.Context())
			if testCase.wantError {
				require.ErrorContains(t, ensureError, "upgrading migration table")
				return
			}
			require.NoError(t, ensureError)
		})
	}
}

func TestEnsureMigrationTableAddsTheProgressColumnsOnce(t *testing.T) {
	t.Parallel()

	store := newProgressStore()
	database := store.open()
	t.Cleanup(func() { _ = database.Close() })
	executor, err := NewExecutor(database, SQLite())
	require.NoError(t, err)

	require.NoError(t, executor.EnsureMigrationTable(t.Context()))
	require.NoError(t, executor.EnsureMigrationTable(t.Context()))

	require.True(t, store.hasColumns(splitterVersionColumn, statementsHashColumn))
}

func TestMigrationRunsInATransactionOnlyWhereItCan(t *testing.T) {
	t.Parallel()

	noTransactionRules := sqllex.Rules{RequiresNoTransaction: func(tokens []sqllex.Token) bool {
		return len(tokens) > 0 && tokens[0].IsWord("STMT_NOTX")
	}}
	customSpec := minimalSpec()
	customSpec.Rules = noTransactionRules
	customSpec.TransactionalDDL = true

	testCases := []struct {
		name            string
		backend         Backend
		content         string
		useTransaction  bool
		dirty           bool
		wantTransaction bool
	}{
		{name: "sqlite runs a plain migration in a transaction", backend: SQLite(), content: "STMT_0 a;", useTransaction: true, wantTransaction: true},
		{name: "the no_transaction directive is honoured", backend: SQLite(), content: "STMT_0 a;", useTransaction: false, wantTransaction: false},
		{name: "a dirty retry never runs in a transaction", backend: SQLite(), content: "STMT_0 a;", useTransaction: true, dirty: true, wantTransaction: false},
		{name: "postgres runs a plain migration in a transaction", backend: Postgres(WithLock(LockNone)), content: "STMT_0 a;", useTransaction: true, wantTransaction: true},
		{name: "postgres detects an index built concurrently", backend: Postgres(WithLock(LockNone)), content: "CREATE INDEX CONCURRENTLY i ON t (c);", useTransaction: true, wantTransaction: false},
		{name: "postgres ignores a mention in a literal", backend: Postgres(WithLock(LockNone)), content: "STMT_0 'CONCURRENTLY';", useTransaction: true, wantTransaction: true},
		{name: "postgres does not know continuous aggregates", backend: Postgres(WithLock(LockNone)), content: "CREATE MATERIALIZED VIEW d WITH (timescaledb.continuous) AS SELECT 1;", useTransaction: true, wantTransaction: true},
		{name: "timescaledb detects a continuous aggregate", backend: TimescaleDB(WithLock(LockNone)), content: "CREATE MATERIALIZED VIEW d WITH (timescaledb.continuous) AS SELECT 1;", useTransaction: true, wantTransaction: false},
		{name: "mysql never runs a migration in a transaction", backend: MySQL(WithLock(LockNone)), content: "STMT_0 a;", useTransaction: true, wantTransaction: false},
		{name: "sqlite does not treat concurrently as a keyword", backend: SQLite(), content: "CREATE INDEX CONCURRENTLY i ON t (c);", useTransaction: true, wantTransaction: true},
		{name: "a custom spec's rules decide", backend: Custom(customSpec), content: "STMT_NOTX a;", useTransaction: true, wantTransaction: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			store := newProgressStore()
			database := store.open()
			t.Cleanup(func() { _ = database.Close() })
			executor, err := NewExecutor(database, testCase.backend)
			require.NoError(t, err)
			require.NoError(t, executor.EnsureMigrationTable(t.Context()))
			record := migrate.MigrationRecord{Name: "m", Checksum: "c", Content: []byte(testCase.content), Version: 1, SkipUpTo: -1, Dirty: testCase.dirty}
			if testCase.dirty {
				store.setRow(1, progressRow{name: "m", checksum: "c", dirty: true})
			}

			require.NoError(t, executor.ExecuteMigration(t.Context(), record, migrate.MigrationDirectionUp, testCase.useTransaction))

			require.Equal(t, testCase.wantTransaction, store.transactionCount() > 0)
			row, exists := store.row(1)
			require.True(t, exists)
			require.False(t, row.dirty)
		})
	}
}

func TestSeedsCommitWithTheirRecordWhereTheEngineAllows(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		backend         Backend
		wantTransaction bool
	}{
		{name: "cockroachdb keeps seeds atomic", backend: CockroachDB(WithLock(LockNone)), wantTransaction: true},
		{name: "mysql keeps seeds atomic", backend: MySQL(WithLock(LockNone)), wantTransaction: true},
		{name: "postgres keeps seeds atomic", backend: Postgres(WithLock(LockNone)), wantTransaction: true},
		{name: "clickhouse has no transactions", backend: ClickHouse(), wantTransaction: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			store := newProgressStore()
			database := store.open()
			t.Cleanup(func() { _ = database.Close() })
			executor, err := NewSeedExecutor(database, testCase.backend)
			require.NoError(t, err)

			require.NoError(t, executor.ExecuteSeed(t.Context(), migrate.SeedRecord{Name: "s", Checksum: "c", Content: []byte("STMT_0 a; STMT_1 b;"), Version: 1}))

			require.Equal(t, testCase.wantTransaction, store.transactionCount() > 0)
		})
	}
}

func TestSeedsRunWholeOrSplitAsTheDriverAllows(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		backend Backend
		want    []string
	}{
		{name: "split by default on mysql", backend: MySQL(WithLock(LockNone)), want: []string{"STMT_0 a", "STMT_1 b"}},
		{name: "whole with a multi-statement driver", backend: MySQL(WithLock(LockNone), WithDriverMultiStatements(true)), want: []string{"STMT_0 a; STMT_1 b;"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			store := newProgressStore()
			database := store.open()
			t.Cleanup(func() { _ = database.Close() })
			executor, err := NewSeedExecutor(database, testCase.backend)
			require.NoError(t, err)

			require.NoError(t, executor.ExecuteSeed(t.Context(), migrate.SeedRecord{Name: "s", Checksum: "c", Content: []byte("STMT_0 a; STMT_1 b;"), Version: 1}))

			require.Equal(t, testCase.want, store.userStatements())
		})
	}
}

func TestBackendSplit(t *testing.T) {
	t.Parallel()

	spec := minimalSpec()
	spec.Split = func(string) ([]string, error) { return []string{"  STMT_0 a ", "", "\n", "STMT_1 b"}, nil }

	statements, err := Custom(spec).Split("ignored")
	require.NoError(t, err)
	require.Equal(t, []string{"STMT_0 a", "STMT_1 b"}, statements, "a spec's statements are trimmed and blank ones dropped")

	_, err = Backend{}.Split("SELECT 1;")
	require.ErrorIs(t, err, migrate.ErrInvalidConfig)

	_, err = Postgres().Split("SELECT 'open")
	require.ErrorIs(t, err, ErrMalformedStatement)
	require.ErrorIs(t, err, sqllex.ErrUnterminatedString)

	statements, err = Custom(minimalSpec()).Split(`SELECT "a;b"; SELECT 2;`)
	require.NoError(t, err)
	require.Equal(t, []string{`SELECT "a;b"`, "SELECT 2"}, statements, "a spec without rules splits standard SQL")
}

func TestMalformedMigrationRunsNothing(t *testing.T) {
	t.Parallel()

	store := newProgressStore()
	database := store.open()
	t.Cleanup(func() { _ = database.Close() })
	executor, err := NewExecutor(database, SQLite())
	require.NoError(t, err)

	err = executor.ExecuteMigration(t.Context(), migrate.MigrationRecord{Content: []byte("STMT_0 a; STMT_1 'open"), Version: 3, SkipUpTo: -1}, migrate.MigrationDirectionUp, false)

	require.ErrorIs(t, err, ErrMalformedStatement)
	require.ErrorContains(t, err, "splitting migration 3 statements")
	require.Empty(t, store.userStatements())
}

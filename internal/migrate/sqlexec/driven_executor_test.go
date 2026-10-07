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
	"io"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/migrate"
)

var (
	errStatementFailed = errors.New("statement failed")
	errRollbackFailed  = errors.New("rollback failed")
)

type retryStore struct {
	mu                       sync.Mutex
	versions                 map[int64]bool
	failingStatementFailures int
}

type retryConnector struct{ store *retryStore }

type retryDriver struct{ store *retryStore }

type retryConn struct{ store *retryStore }

type retryTx struct{}

type retryCountRows struct {
	count int64
	done  bool
}

type retryEmptyRows struct{}

func (s *retryStore) version(arguments []driver.NamedValue) int64 {
	if len(arguments) == 0 {
		return 0
	}
	if value, ok := arguments[0].Value.(int64); ok {
		return value
	}
	return 0
}

func (s *retryStore) exec(query string, arguments []driver.NamedValue) (driver.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case strings.Contains(query, "INSERT INTO sgl_migrations"):
		version := s.version(arguments)
		if s.versions[version] {
			return nil, errors.New("UNIQUE constraint failed: sgl_migrations.version")
		}
		s.versions[version] = true
		return driver.RowsAffected(1), nil
	case strings.Contains(query, "FAIL_ONCE"):
		if s.failingStatementFailures > 0 {
			s.failingStatementFailures--
			return nil, errors.New("simulated statement 0 failure")
		}
		return driver.RowsAffected(0), nil
	default:

		return driver.RowsAffected(0), nil
	}
}

func (s *retryStore) query(query string, arguments []driver.NamedValue) (driver.Rows, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.Contains(query, "count(*)") {
		count := int64(0)
		if s.versions[s.version(arguments)] {
			count = 1
		}
		return &retryCountRows{count: count}, nil
	}
	return &retryEmptyRows{}, nil
}

func (c *retryConnector) Connect(context.Context) (driver.Conn, error) {
	return &retryConn{store: c.store}, nil
}
func (c *retryConnector) Driver() driver.Driver { return &retryDriver{store: c.store} }

func (d *retryDriver) Open(string) (driver.Conn, error) { return &retryConn{store: d.store}, nil }

func (*retryConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("prepare unsupported") }
func (*retryConn) Close() error                        { return nil }
func (*retryConn) Begin() (driver.Tx, error)           { return retryTx{}, nil }
func (*retryConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return retryTx{}, nil
}

func (c *retryConn) ExecContext(_ context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
	return c.store.exec(query, arguments)
}

func (c *retryConn) QueryContext(_ context.Context, query string, arguments []driver.NamedValue) (driver.Rows, error) {
	return c.store.query(query, arguments)
}

func (retryTx) Commit() error   { return nil }
func (retryTx) Rollback() error { return nil }

func (*retryCountRows) Columns() []string { return []string{"count"} }
func (*retryCountRows) Close() error      { return nil }
func (r *retryCountRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = r.count
	return nil
}

func (*retryEmptyRows) Columns() []string           { return []string{"unused"} }
func (*retryEmptyRows) Close() error                { return nil }
func (*retryEmptyRows) Next(_ []driver.Value) error { return io.EOF }

func TestExecutorsStopWhenTheTableLocksTransactionHasEnded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		backend   Backend
		name      string
		lockTable string
		lost      bool
		wantCheck bool
	}{
		{name: "a lost postgres table lock", backend: PostgresPgBouncer(), lockTable: "sgl_migration_lock", lost: true, wantCheck: true},
		{name: "a lost cockroachdb table lock", backend: CockroachDB(), lockTable: "sgl_migration_lock", lost: true, wantCheck: true},
		{name: "a held postgres table lock", backend: PostgresPgBouncer(), lockTable: "sgl_migration_lock", wantCheck: true},
		{name: "an advisory lock has no transaction to check", backend: Postgres(), lost: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			memory := newMemoryDatabase()
			database := memory.open()
			t.Cleanup(func() { _ = database.Close() })
			executor, err := NewExecutor(database, testCase.backend)
			require.NoError(t, err)
			require.NoError(t, executor.AcquireLock(t.Context()))
			t.Cleanup(func() { _ = executor.ReleaseLock(t.Context()) })
			require.NoError(t, executor.EnsureMigrationTable(t.Context()))
			lockSession := memory.rowLockOwner(testCase.lockTable)
			if testCase.lost {
				memory.loseLock()
			}
			record := migrate.MigrationRecord{Name: "users", Checksum: "c", Content: []byte("CREATE TABLE users (id BIGINT);"), Version: 1, SkipUpTo: -1}

			err = executor.ExecuteMigration(t.Context(), record, migrate.MigrationDirectionUp, true)

			wantLost := testCase.lost && testCase.wantCheck
			require.Equal(t, wantLost, errors.Is(err, ErrLockLost), "%v", err)
			if wantLost {
				require.ErrorContains(t, err, "before migration 1")
				require.ErrorContains(t, err, "idle-in-transaction timeout")
				require.NotContains(t, memory.executed(), "CREATE TABLE users (id BIGINT)", "a run that lost its lock must run nothing")
				require.Empty(t, memory.appliedVersions())
			} else {
				require.NoError(t, err)
				require.ElementsMatch(t, []int64{1}, memory.appliedVersions())
			}
			if !testCase.wantCheck {
				require.Empty(t, memory.checkedSessions())
				return
			}
			require.Equal(t, []int64{lockSession}, memory.checkedSessions(), "the check must run in the lock's own transaction")
		})
	}
}

func TestSeedExecutorStopsWhenTheTableLocksTransactionHasEnded(t *testing.T) {
	t.Parallel()

	memory := newMemoryDatabase()
	database := memory.open()
	t.Cleanup(func() { _ = database.Close() })
	executor, err := NewSeedExecutor(database, PostgresPgBouncer())
	require.NoError(t, err)
	require.NoError(t, executor.AcquireSeedLock(t.Context()))
	t.Cleanup(func() { _ = executor.ReleaseSeedLock(t.Context()) })
	memory.loseLock()

	err = executor.ExecuteSeed(t.Context(), migrate.SeedRecord{Name: "rows", Checksum: "c", Content: []byte("INSERT INTO rows VALUES (1);"), Version: 3})

	require.ErrorIs(t, err, ErrLockLost)
	require.ErrorContains(t, err, "before seed 3 (rows)")
	require.NotContains(t, memory.executed(), "INSERT INTO rows VALUES (1);")
}

func TestExecutorConstructorsRejectOptionsTheyCannotUse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		message string
		options []ExecutorOption
	}{
		{name: "a nil clock", options: []ExecutorOption{WithClock(nil)}, message: "WithClock needs a clock, not nil"},
		{name: "an empty batch", options: []ExecutorOption{WithProgressBatchSize(0)}, message: "at least one statement per batch, got 0"},
		{name: "a negative batch", options: []ExecutorOption{WithProgressBatchSize(-7)}, message: "got -7"},
		{name: "every problem at once", options: []ExecutorOption{WithClock(nil), WithProgressBatchSize(0)}, message: "WithClock needs a clock"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewExecutor(fakeDatabase(t), SQLite(), testCase.options...)
			require.ErrorIs(t, err, migrate.ErrInvalidConfig)
			require.ErrorContains(t, err, testCase.message)
			_, err = NewSeedExecutor(fakeDatabase(t), SQLite(), testCase.options...)
			require.ErrorIs(t, err, migrate.ErrInvalidConfig)
			require.ErrorContains(t, err, testCase.message)
		})
	}
}

func threeStatementRecord() migrate.MigrationRecord {
	return migrate.MigrationRecord{Name: "boats", Checksum: "c", Content: []byte("STMT_0 a;\nSTMT_1 b;\nSTMT_2 c;\n"), Version: 1, SkipUpTo: -1}
}

func TestCustomSpecsTakeEveryMigrationPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		failRollback     error
		failOn           map[string]error
		wantErrors       []error
		name             string
		wantMessage      string
		wantStatements   []string
		direction        migrate.MigrationDirection
		wantTransactions int
		transactionalDDL bool
		appendOnly       bool
		useTransaction   bool
		existingRow      bool
		wantRow          bool
		wantProgress     bool
	}{
		{
			name:       "an append-only up records one completed row",
			appendOnly: true, direction: migrate.MigrationDirectionUp, useTransaction: true,
			wantStatements: []string{"STMT_0 a", "STMT_1 b", "STMT_2 c"}, wantRow: true,
		},
		{
			name:       "an append-only up that fails part-way records nothing",
			appendOnly: true, direction: migrate.MigrationDirectionUp, useTransaction: true,
			failOn:         map[string]error{"STMT_1 b": errStatementFailed},
			wantErrors:     []error{errStatementFailed},
			wantMessage:    "executing SQL: statement 2/3 of migration 1",
			wantStatements: []string{"STMT_0 a", "STMT_1 b"},
		},
		{
			name:      "a statement-by-statement up tracks its progress",
			direction: migrate.MigrationDirectionUp, useTransaction: true,
			wantStatements: []string{"STMT_0 a", "STMT_1 b", "STMT_2 c"}, wantRow: true, wantProgress: true,
		},
		{
			name:      "a non-transactional down removes the row",
			direction: migrate.MigrationDirectionDown, existingRow: true,
			wantStatements: []string{"STMT_0 a", "STMT_1 b", "STMT_2 c"},
		},
		{
			name:      "a non-transactional down that fails part-way keeps the row",
			direction: migrate.MigrationDirectionDown, existingRow: true,
			failOn:         map[string]error{"STMT_1 b": errStatementFailed},
			wantErrors:     []error{errStatementFailed},
			wantMessage:    "executing SQL: statement 2/3 of migration 1",
			wantStatements: []string{"STMT_0 a", "STMT_1 b"}, wantRow: true,
		},
		{
			name:             "a down without a transaction on transactional DDL removes the row in one",
			transactionalDDL: true, direction: migrate.MigrationDirectionDown, existingRow: true,
			wantStatements: []string{"STMT_0 a", "STMT_1 b", "STMT_2 c"}, wantTransactions: 1,
		},
		{
			name:             "a failed rollback of a down's history transaction is joined",
			transactionalDDL: true, direction: migrate.MigrationDirectionDown, existingRow: true,
			failOn:         map[string]error{"DELETE FROM sgl_migrations WHERE version = $1": errStatementFailed},
			failRollback:   errRollbackFailed,
			wantErrors:     []error{errStatementFailed, errRollbackFailed},
			wantMessage:    "rolling back down-migration history transaction: rollback failed",
			wantStatements: []string{"STMT_0 a", "STMT_1 b", "STMT_2 c"}, wantTransactions: 1, wantRow: true,
		},
		{
			name:             "a failed rollback of a migration's transaction is joined",
			transactionalDDL: true, direction: migrate.MigrationDirectionUp, useTransaction: true,
			failOn:         map[string]error{"STMT_1 b": errStatementFailed},
			failRollback:   errRollbackFailed,
			wantErrors:     []error{errStatementFailed, errRollbackFailed},
			wantMessage:    "rolling back migration transaction: rollback failed",
			wantStatements: []string{"STMT_0 a", "STMT_1 b"}, wantTransactions: 1,
		},
		{
			name:             "a clean rollback leaves the statement failure alone",
			transactionalDDL: true, direction: migrate.MigrationDirectionUp, useTransaction: true,
			failOn:         map[string]error{"STMT_1 b": errStatementFailed},
			wantErrors:     []error{errStatementFailed},
			wantMessage:    "executing SQL: statement 2/3 of migration 1",
			wantStatements: []string{"STMT_0 a", "STMT_1 b"}, wantTransactions: 1,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			spec := minimalSpec()
			spec.TransactionalDDL = testCase.transactionalDDL
			spec.AppendOnlyHistory = testCase.appendOnly
			store := newProgressStore()
			database := store.open()
			t.Cleanup(func() { _ = database.Close() })
			executor, err := NewExecutor(database, Custom(spec))
			require.NoError(t, err)
			require.NoError(t, executor.EnsureMigrationTable(t.Context()))
			if testCase.existingRow {
				store.setRow(1, progressRow{name: "boats", checksum: "c"})
			}
			for statement, failure := range testCase.failOn {
				store.failOn(statement, failure)
			}
			store.failRollbacks(testCase.failRollback)
			store.resetExecuted()

			err = executor.ExecuteMigration(t.Context(), threeStatementRecord(), testCase.direction, testCase.useTransaction)

			if len(testCase.wantErrors) == 0 {
				require.NoError(t, err)
			}
			for _, want := range testCase.wantErrors {
				require.ErrorIs(t, err, want)
			}
			if testCase.wantMessage != "" {
				require.ErrorContains(t, err, testCase.wantMessage)
			}
			require.Equal(t, testCase.wantStatements, store.userStatements())
			require.Equal(t, testCase.wantTransactions, store.transactionCount())
			row, exists := store.row(1)
			require.Equal(t, testCase.wantRow, exists)
			if exists && testCase.direction == migrate.MigrationDirectionUp {
				require.False(t, row.dirty)
				require.Equal(t, testCase.wantProgress, row.lastStatement != nil)
			}
			wroteProgress := slices.ContainsFunc(store.executedStatements(), func(statement string) bool {
				return strings.HasPrefix(statement, "UPDATE sgl_migrations SET last_statement")
			})
			require.Equal(t, testCase.wantProgress, wroteProgress)
		})
	}
}

func TestNewExecutor_PreservesBackend(t *testing.T) {
	t.Parallel()

	backend := SQLite()

	require.Equal(t, backend.Names(), newExecutor(nil, backend).backend.Names())
	require.Equal(t, backend.Names(), newSeedExecutor(nil, backend).backend.Names())
}

func TestExecutor_FirstStatementFailureThenRetryResumes(t *testing.T) {
	t.Parallel()

	store := &retryStore{versions: map[int64]bool{}, failingStatementFailures: 1}
	database := sql.OpenDB(&retryConnector{store: store})
	t.Cleanup(func() { _ = database.Close() })

	executor := newExecutor(database, SQLite())

	migration := migrate.MigrationRecord{
		Name:     "create_things",
		Checksum: "abc",
		Content:  []byte("FAIL_ONCE;\nOK2;\n"),
		Version:  1,
		SkipUpTo: -1,
	}

	firstError := executor.ExecuteMigration(t.Context(), migration, migrate.MigrationDirectionUp, false)
	require.Error(t, firstError)

	retryError := executor.ExecuteMigration(t.Context(), migration, migrate.MigrationDirectionUp, false)
	require.NoError(t, retryError, "retry after first-statement failure must resume, not hit a duplicate-key error")
}

func TestProgressIsCheckpointedAfterEveryStatementByDefault(t *testing.T) {
	t.Parallel()

	require.Equal(t, 1, newExecutorSettings(nil).progressBatchSize)
}

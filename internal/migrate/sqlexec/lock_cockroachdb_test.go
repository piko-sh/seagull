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
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/migrate"
)

const durableLockTableID = int64(448)

type durableLockStore struct {
	mu              sync.Mutex
	statements      []string
	setError        error
	failOnlySetting string
	rollbacks       int
	commits         int
}

type durableLockConnector struct{ store *durableLockStore }

type durableLockDriver struct{}

type durableLockConn struct {
	store         *durableLockStore
	inTransaction bool
}

type durableLockTx struct{ connection *durableLockConn }

func (store *durableLockStore) record(statement string, inTransaction bool) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if inTransaction {
		statement = "in transaction: " + statement
	}
	store.statements = append(store.statements, statement)
}

func (store *durableLockStore) recorded() []string {
	store.mu.Lock()
	defer store.mu.Unlock()
	return append([]string(nil), store.statements...)
}

func (store *durableLockStore) finished() (int, int) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.commits, store.rollbacks
}

func (connector durableLockConnector) Connect(context.Context) (driver.Conn, error) {
	return &durableLockConn{store: connector.store, inTransaction: false}, nil
}

func (durableLockConnector) Driver() driver.Driver { return durableLockDriver{} }

func (durableLockDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("durableLockDriver opens connections only through its connector")
}

func (*durableLockConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("durableLockConn does not prepare statements")
}

func (*durableLockConn) Close() error { return nil }

func (conn *durableLockConn) Begin() (driver.Tx, error) {
	conn.inTransaction = true
	return durableLockTx{connection: conn}, nil
}

func (conn *durableLockConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return conn.Begin()
}

func (conn *durableLockConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	conn.store.record(query, conn.inTransaction)
	if strings.HasPrefix(query, "SET ") && conn.store.setError != nil && strings.Contains(query, conn.store.failOnlySetting) {
		return nil, conn.store.setError
	}
	return driver.RowsAffected(0), nil
}

func (conn *durableLockConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	conn.store.record(query, conn.inTransaction)
	return &singleValueRows{value: durableLockTableID}, nil
}

func (tx durableLockTx) Commit() error {
	tx.connection.inTransaction = false
	tx.connection.store.mu.Lock()
	defer tx.connection.store.mu.Unlock()
	tx.connection.store.commits++
	return nil
}

func (tx durableLockTx) Rollback() error {
	tx.connection.inTransaction = false
	tx.connection.store.mu.Lock()
	defer tx.connection.store.mu.Unlock()
	tx.connection.store.rollbacks++
	return nil
}

func openDurableLockDB(t *testing.T, store *durableLockStore) *sql.DB {
	t.Helper()
	database := sql.OpenDB(durableLockConnector{store: store})
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestCockroachDBBackendIsDurablyLockedAndNotTransactional(t *testing.T) {
	t.Parallel()

	backend := CockroachDB()

	require.NoError(t, backend.err)
	require.False(t, backend.engine.transactionalDDL, "CockroachDB does not make schema changes atomic with their transaction")
	migrationLock, ok := backend.migrationLock.(*cockroachDBTableLock)
	require.True(t, ok, "CockroachDB's migration lock is its own table lock")
	require.Equal(t, "SELECT 'sgl_migration_lock'::REGCLASS::OID::INT8", migrationLock.idSQL)
	seedLock, ok := backend.seedLock.(*cockroachDBTableLock)
	require.True(t, ok, "CockroachDB's seed lock is its own table lock")
	require.Equal(t, "SELECT 'sgl_seed_lock'::REGCLASS::OID::INT8", seedLock.idSQL)
	require.Equal(t, *newPostgresTableLock("sgl_migration_lock", "sgl_migration_lock_single_row", "migration table"), migrationLock.base,
		"CockroachDB must lock the same table and row as PostgreSQL, so earlier releases still exclude it")
}

func TestCockroachDBTableLockLocksTheRowByIDInADurableTransaction(t *testing.T) {
	t.Parallel()

	locks := cockroachDBTableLocks(namesFor(DefaultNamespace), false)
	tests := []struct {
		name    string
		lock    lockStrategy
		try     bool
		table   string
		rowLock string
	}{
		{name: "migration lock waits", lock: locks.migration, table: "sgl_migration_lock", rowLock: "SELECT lock_id FROM [448 AS sgl_migration_lock] FOR UPDATE"},
		{name: "migration lock tries", lock: locks.migration, try: true, table: "sgl_migration_lock", rowLock: "SELECT lock_id FROM [448 AS sgl_migration_lock] FOR UPDATE NOWAIT"},
		{name: "seed lock waits", lock: locks.seed, table: "sgl_seed_lock", rowLock: "SELECT lock_id FROM [448 AS sgl_seed_lock] FOR UPDATE"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			store := &durableLockStore{}
			database := openDurableLockDB(t, store)
			take := testCase.lock.Acquire
			if testCase.try {
				take = testCase.lock.TryAcquire
			}

			handle, err := take(t.Context(), database)
			require.NoError(t, err)
			require.NotNil(t, handle.transaction)
			require.Equal(t, testCase.table, handle.name)
			require.NoError(t, testCase.lock.Release(t.Context(), handle))

			statements := store.recorded()
			require.Len(t, statements, 6)
			require.Contains(t, statements[0], "CREATE TABLE IF NOT EXISTS "+testCase.table)
			require.Equal(t, "INSERT INTO "+testCase.table+" (lock_id) VALUES (1) ON CONFLICT DO NOTHING", statements[1])
			require.Equal(t, "SELECT '"+testCase.table+"'::REGCLASS::OID::INT8", statements[2], "the table must be resolved outside the lock transaction")
			require.Equal(t, "in transaction: SET LOCAL enable_durable_locking_for_serializable = true", statements[3])
			require.Equal(t, "in transaction: SET LOCAL idle_in_transaction_session_timeout = 0", statements[4],
				"the lock transaction sits idle for the whole run, so the server must not end it for idling")
			require.Equal(t, "in transaction: "+testCase.rowLock, statements[5])
			commits, rollbacks := store.finished()
			require.Equal(t, 1, commits)
			require.Zero(t, rollbacks)
		})
	}
}

func TestCockroachDBTableLockReportsAFailedDurableLockingSetting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		setError        error
		wantUnsupported bool
	}{
		{
			name:            "server before v23.2 reports SQLSTATE 42704",
			setError:        sqlStateError{state: "42704", message: `unrecognized configuration parameter "enable_durable_locking_for_serializable"`},
			wantUnsupported: true,
		},
		{
			name:            "driver without SQLSTATE reports the message",
			setError:        errors.New(`pq: unrecognized configuration parameter "enable_durable_locking_for_serializable"`),
			wantUnsupported: true,
		},
		{
			name:            "any other failure",
			setError:        sqlStateError{state: "08006", message: "connection reset by peer"},
			wantUnsupported: false,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			store := &durableLockStore{setError: testCase.setError}
			database := openDurableLockDB(t, store)
			lock := cockroachDBTableLocks(namesFor(DefaultNamespace), false).migration

			_, err := lock.Acquire(t.Context(), database)

			require.ErrorIs(t, err, testCase.setError)
			require.NotErrorIs(t, err, migrate.ErrLockNotAcquired, "a server that cannot lock durably is not a held lock")
			require.Equal(t, testCase.wantUnsupported, errors.Is(err, ErrUnsupportedLock))
			if testCase.wantUnsupported {
				require.ErrorIs(t, err, migrate.ErrInvalidConfig)
				require.ErrorContains(t, err, "CockroachDB v23.2 or later")
				require.ErrorContains(t, err, "WithLock(LockNone)")
			}
			for _, statement := range store.recorded() {
				require.NotContains(t, statement, "FOR UPDATE", "the row lock was taken without durable locking")
			}
			commits, rollbacks := store.finished()
			require.Zero(t, commits)
			require.Equal(t, 1, rollbacks)
		})
	}
}

func TestPostgresTableLockStillLocksTheRowByName(t *testing.T) {
	t.Parallel()

	store := &durableLockStore{}
	database := openDurableLockDB(t, store)
	lock := PostgresPgBouncer().migrationLock

	handle, err := lock.TryAcquire(t.Context(), database)
	require.NoError(t, err)
	require.NoError(t, lock.Release(t.Context(), handle))

	statements := store.recorded()
	require.Len(t, statements, 4)
	require.Equal(t, "in transaction: SET LOCAL idle_in_transaction_session_timeout = 0", statements[2],
		"the only setting PostgreSQL's table lock changes is the idle timeout of its own transaction")
	require.Equal(t, "in transaction: SELECT lock_id FROM sgl_migration_lock FOR UPDATE NOWAIT", statements[3])
}

func TestTableLockStopsWhenItsTransactionCannotBeKeptOpen(t *testing.T) {
	t.Parallel()

	refused := sqlStateError{state: "42501", message: "permission denied to set parameter"}
	tests := []struct {
		name string
		lock lockStrategy
	}{
		{name: "postgres", lock: PostgresPgBouncer().migrationLock},
		{name: "cockroachdb", lock: CockroachDB().migrationLock},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			store := &durableLockStore{setError: refused, failOnlySetting: "idle_in_transaction_session_timeout"}
			database := openDurableLockDB(t, store)

			_, err := testCase.lock.Acquire(t.Context(), database)

			require.ErrorIs(t, err, refused)
			require.ErrorContains(t, err, "preparing migration table lock transaction")
			require.NotErrorIs(t, err, migrate.ErrLockNotAcquired)
			for _, statement := range store.recorded() {
				require.NotContains(t, statement, "FOR UPDATE", "the row lock was taken in a transaction the server may end")
			}
			commits, rollbacks := store.finished()
			require.Zero(t, commits)
			require.Equal(t, 1, rollbacks)
		})
	}
}

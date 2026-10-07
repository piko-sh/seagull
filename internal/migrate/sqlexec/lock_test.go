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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/migrate"
	"seagull.sh/seagull/internal/testkit/scaling"
)

type fakeLockStore struct {
	mu               sync.Mutex
	lockNames        []string
	held             bool
	acquisitions     atomic.Int32
	releases         atomic.Int32
	failTableLockSQL bool
	nullGetLock      bool
	waitsWhileHeld   bool
	getLockCalls     atomic.Int32
	advisoryKeys     []string
}

type fakeLockConnector struct {
	store *fakeLockStore
}

type fakeLockDriver struct {
	store *fakeLockStore
}

type fakeLockConn struct {
	store *fakeLockStore
}

type fakeLockTx struct {
	connection *fakeLockConn
}

type singleValueRows struct {
	value    driver.Value
	consumed bool
}

type fakePostgresSQLStateError struct {
	sqlState string
}

type stallingStore struct {
	transactionContext context.Context
	mu                 sync.Mutex
	stallCommit        bool
}

type stallingConnector struct{ store *stallingStore }

type stallingDriver struct{}

type stallingConn struct{ store *stallingStore }

type stallingTx struct {
	ctx   context.Context
	stall bool
}

func (s *fakeLockStore) tryHold() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.held {
		return false
	}
	s.held = true
	s.acquisitions.Add(1)
	return true
}

func (s *fakeLockStore) waitWhileHeld(ctx context.Context, query string) (bool, error) {
	waiting := strings.Contains(query, "GET_LOCK(?, 5)") || query == postgresAdvisoryLockSQL
	if !s.waitsWhileHeld || !waiting {
		return false, nil
	}
	s.mu.Lock()
	held := s.held
	s.mu.Unlock()
	if !held {
		return false, nil
	}
	if strings.Contains(query, "GET_LOCK") {
		s.getLockCalls.Add(1)
	}
	<-ctx.Done()
	return true, ctx.Err()
}

func (s *fakeLockStore) recordKey(query string, arguments []driver.NamedValue) {
	if !strings.Contains(query, "hashtext($1)") {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.advisoryKeys = append(s.advisoryKeys, namedString(arguments, 0))
}

func (s *fakeLockStore) release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.held = false
	s.releases.Add(1)
}

func (s *fakeLockStore) exec(query string) (driver.Result, error) {
	switch {
	case query == postgresAdvisoryLockSQL:
		s.tryHold()
		return driver.RowsAffected(0), nil
	case query == postgresAdvisoryUnlockSQL:
		s.release()
		return driver.RowsAffected(0), nil
	case strings.Contains(query, "RELEASE_LOCK(?)"):
		s.release()
		return driver.RowsAffected(0), nil
	case strings.Contains(query, "CREATE TABLE") && strings.Contains(query, "sgl_migration_lock"):
		return driver.RowsAffected(0), nil
	case strings.Contains(query, "INSERT INTO sgl_migration_lock"):
		return driver.RowsAffected(0), nil
	case strings.Contains(query, "FOR UPDATE NOWAIT") && strings.Contains(query, "sgl_migration_lock"):
		if !s.tryHold() {
			return nil, errors.New("pq: could not obtain lock on row in relation \"sgl_migration_lock\"")
		}
		return driver.RowsAffected(0), nil
	case strings.Contains(query, "FOR UPDATE") && strings.Contains(query, "sgl_migration_lock"):
		if s.failTableLockSQL {
			return nil, errors.New("pq: deadlock detected")
		}
		s.tryHold()
		return driver.RowsAffected(0), nil
	default:
		return nil, fmt.Errorf("fakeLockStore.exec: unhandled SQL %q", query)
	}
}

func (s *fakeLockStore) query(query string) (driver.Rows, error) {
	switch {
	case query == postgresEngine().currentSchemaSQL:
		return &singleValueRows{value: nil}, nil
	case query == postgresAdvisoryTryLockSQL:
		return &singleValueRows{value: s.tryHold()}, nil
	case strings.HasPrefix(query, "SELECT IF(CHAR_LENGTH(CONCAT(COALESCE(DATABASE(), ''), '.', 'sgl_migrations'))"):
		return &singleValueRows{value: "app.sgl_migrations"}, nil
	case strings.Contains(query, "GET_LOCK(?, 5)"):
		s.getLockCalls.Add(1)
		if s.nullGetLock {
			return &singleValueRows{value: nil}, nil
		}
		if s.tryHold() {
			return &singleValueRows{value: int64(1)}, nil
		}
		return &singleValueRows{value: int64(0)}, nil
	case strings.Contains(query, "GET_LOCK(?, 0)"):
		if s.tryHold() {
			return &singleValueRows{value: int64(1)}, nil
		}
		return &singleValueRows{value: int64(0)}, nil
	default:
		return nil, fmt.Errorf("fakeLockStore.query: unhandled SQL %q", query)
	}
}

func (c *fakeLockConnector) Connect(_ context.Context) (driver.Conn, error) {
	return &fakeLockConn{store: c.store}, nil
}

func (c *fakeLockConnector) Driver() driver.Driver {
	return &fakeLockDriver{store: c.store}
}

func (d *fakeLockDriver) Open(_ string) (driver.Conn, error) {
	return &fakeLockConn{store: d.store}, nil
}

func (*fakeLockConn) Prepare(_ string) (driver.Stmt, error) {
	return nil, errors.New("Prepare not supported by fakeLockConn")
}

func (*fakeLockConn) Close() error { return nil }

func (c *fakeLockConn) Begin() (driver.Tx, error) { return &fakeLockTx{connection: c}, nil }

func (c *fakeLockConn) BeginTx(_ context.Context, _ driver.TxOptions) (driver.Tx, error) {
	return &fakeLockTx{connection: c}, nil
}

func (c *fakeLockConn) ExecContext(ctx context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
	if waited, err := c.store.waitWhileHeld(ctx, query); waited {
		return nil, err
	}
	c.store.recordKey(query, arguments)
	return c.store.exec(query)
}

func (c *fakeLockConn) QueryContext(ctx context.Context, query string, arguments []driver.NamedValue) (driver.Rows, error) {
	if waited, err := c.store.waitWhileHeld(ctx, query); waited {
		return nil, err
	}
	c.store.recordKey(query, arguments)
	if len(arguments) == 1 && !strings.Contains(query, "hashtext($1)") {
		if name, ok := arguments[0].Value.(string); ok {
			c.store.mu.Lock()
			c.store.lockNames = append(c.store.lockNames, name)
			c.store.mu.Unlock()
		}
	}
	return c.store.query(query)
}

func (*fakeLockTx) Commit() error   { return nil }
func (*fakeLockTx) Rollback() error { return nil }

func (*singleValueRows) Columns() []string { return []string{"result"} }
func (*singleValueRows) Close() error      { return nil }
func (r *singleValueRows) Next(dest []driver.Value) error {
	if r.consumed {
		return io.EOF
	}
	r.consumed = true
	dest[0] = r.value
	return nil
}

func (e fakePostgresSQLStateError) Error() string { return "postgres server error" }

func (e fakePostgresSQLStateError) SQLState() string { return e.sqlState }

func (store *stallingStore) begun() context.Context {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.transactionContext
}

func (connector stallingConnector) Connect(context.Context) (driver.Conn, error) {
	return &stallingConn{store: connector.store}, nil
}

func (stallingConnector) Driver() driver.Driver { return stallingDriver{} }

func (stallingDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("stallingDriver opens connections only through its connector")
}

func (*stallingConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("stallingConn does not prepare statements")
}

func (*stallingConn) Close() error { return nil }

func (*stallingConn) Begin() (driver.Tx, error) {
	return nil, errors.New("stallingConn begins transactions only with a context")
}

func (conn *stallingConn) BeginTx(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
	conn.store.mu.Lock()
	defer conn.store.mu.Unlock()
	conn.store.transactionContext = ctx
	return stallingTx{ctx: ctx, stall: conn.store.stallCommit}, nil
}

func (*stallingConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return driver.RowsAffected(0), nil
}

func (tx stallingTx) Commit() error {
	if !tx.stall {
		return nil
	}
	<-tx.ctx.Done()
	return tx.ctx.Err()
}

func (stallingTx) Rollback() error { return nil }

func openFakeLockDB(store *fakeLockStore) *sql.DB {
	return sql.OpenDB(&fakeLockConnector{store: store})
}

func postgresMigrationLock() *postgresAdvisoryLock {
	return &postgresAdvisoryLock{key: "sgl_migrations", label: "PostgreSQL"}
}

func serverWideMySQLLock() *pollingAdvisoryLock {
	return newMySQLAdvisoryLock("sgl_migrations", "MySQL", true)
}

func migrationTableLock() *tableLock {
	return newTableLock(postgresTableLockSQL, "sgl_migration_lock", "sgl_migration_lock_single_row", "migration table")
}

func TestPostgresAdvisoryLock_AcquireAndRelease(t *testing.T) {
	t.Parallel()

	store := &fakeLockStore{}
	database := openFakeLockDB(store)
	t.Cleanup(func() { _ = database.Close() })

	lock := postgresMigrationLock()

	handle, err := lock.Acquire(t.Context(), database)
	require.NoError(t, err)
	require.NotNil(t, handle.connection, "PostgreSQL advisory lock must pin a connection")
	require.Nil(t, handle.transaction)
	require.Equal(t, int32(1), store.acquisitions.Load())

	require.NoError(t, lock.Release(t.Context(), handle))
	require.Equal(t, int32(1), store.releases.Load())
}

func TestPostgresAdvisoryLock_TryAcquireSucceedsWhenFree(t *testing.T) {
	t.Parallel()

	store := &fakeLockStore{}
	database := openFakeLockDB(store)
	t.Cleanup(func() { _ = database.Close() })

	lock := postgresMigrationLock()

	handle, err := lock.TryAcquire(t.Context(), database)
	require.NoError(t, err)
	require.NotNil(t, handle.connection)
	require.NoError(t, lock.Release(t.Context(), handle))
}

func TestPostgresAdvisoryLock_TryAcquireReturnsLockNotAcquiredWhenHeld(t *testing.T) {
	t.Parallel()

	store := &fakeLockStore{}
	database := openFakeLockDB(store)
	t.Cleanup(func() { _ = database.Close() })

	lock := postgresMigrationLock()
	first, err := lock.Acquire(t.Context(), database)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lock.Release(t.Context(), first) })

	_, secondError := lock.TryAcquire(t.Context(), database)
	require.ErrorIs(t, secondError, migrate.ErrLockNotAcquired,
		"a non-blocking acquire against a held advisory lock must surface ErrLockNotAcquired")
}

func TestMySQLAdvisoryLock_AcquireAndRelease(t *testing.T) {
	t.Parallel()

	store := &fakeLockStore{}
	database := openFakeLockDB(store)
	t.Cleanup(func() { _ = database.Close() })

	lock := serverWideMySQLLock()

	handle, err := lock.Acquire(t.Context(), database)
	require.NoError(t, err)
	require.NotNil(t, handle.connection, "MySQL advisory lock must pin a connection")
	require.Equal(t, "sgl_migrations", handle.name)
	require.Equal(t, int32(1), store.acquisitions.Load())

	require.NoError(t, lock.Release(t.Context(), handle))
	require.Equal(t, int32(1), store.releases.Load())
	require.Equal(t, []string{"sgl_migrations"}, store.lockNames)
}

func TestMySQLAdvisoryLock_ScopedKeyResolvesTheNameOnce(t *testing.T) {
	t.Parallel()

	store := &fakeLockStore{}
	database := openFakeLockDB(store)
	t.Cleanup(func() { _ = database.Close() })

	lock := newMySQLAdvisoryLock("sgl_migrations", "MySQL", false)

	handle, err := lock.Acquire(t.Context(), database)
	require.NoError(t, err)
	require.Equal(t, "app.sgl_migrations", handle.name)
	require.NoError(t, lock.Release(t.Context(), handle))
	require.Equal(t, []string{"app.sgl_migrations"}, store.lockNames)
}

func TestMySQLAdvisoryLock_TryAcquireReturnsLockNotAcquiredWhenHeld(t *testing.T) {
	t.Parallel()

	store := &fakeLockStore{}
	database := openFakeLockDB(store)
	t.Cleanup(func() { _ = database.Close() })

	lock := serverWideMySQLLock()
	first, err := lock.Acquire(t.Context(), database)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lock.Release(t.Context(), first) })

	_, secondError := lock.TryAcquire(t.Context(), database)
	require.ErrorIs(t, secondError, migrate.ErrLockNotAcquired,
		"GET_LOCK returning 0 must map to ErrLockNotAcquired")
}

func TestMySQLAdvisoryLock_AcquireWaitsUntilContextEnds(t *testing.T) {
	t.Parallel()

	store := &fakeLockStore{}
	database := openFakeLockDB(store)
	t.Cleanup(func() { _ = database.Close() })

	lock := serverWideMySQLLock()
	lock.pollInterval = time.Millisecond
	first, err := lock.Acquire(t.Context(), database)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lock.Release(t.Context(), first) })

	cause := errors.New("deploy window closed")
	ctx, cancel := context.WithTimeoutCause(t.Context(), 20*time.Millisecond, cause)
	defer cancel()

	_, secondError := lock.Acquire(ctx, database)

	require.ErrorIs(t, secondError, cause)
	require.ErrorContains(t, secondError, "waiting for MySQL lock sgl_migrations")
	require.Greater(t, store.getLockCalls.Load(), int32(2), "a timed-out poll must be retried")
}

func TestLockAcquisitionNamesTheCauseWhenTheContextEndsInsideAWait(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		lock    lockStrategy
		name    string
		message string
	}{
		{name: "MySQL GET_LOCK", lock: serverWideMySQLLock(), message: "waiting for MySQL lock sgl_migrations"},
		{name: "PostgreSQL pg_advisory_lock", lock: postgresMigrationLock(), message: "acquiring PostgreSQL advisory lock"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			store := &fakeLockStore{waitsWhileHeld: true}
			database := openFakeLockDB(store)
			t.Cleanup(func() { _ = database.Close() })

			first, err := testCase.lock.Acquire(t.Context(), database)
			require.NoError(t, err)
			t.Cleanup(func() { _ = testCase.lock.Release(t.Context(), first) })

			cause := errors.New("deploy window closed")
			ctx, cancel := context.WithTimeoutCause(t.Context(), 20*time.Millisecond, cause)
			defer cancel()

			_, secondError := testCase.lock.Acquire(ctx, database)

			require.ErrorIs(t, secondError, cause)
			require.ErrorContains(t, secondError, testCase.message)
		})
	}
}

func TestMySQLAdvisoryLock_AcquireReportsNullResult(t *testing.T) {
	t.Parallel()

	store := &fakeLockStore{nullGetLock: true}
	database := openFakeLockDB(store)
	t.Cleanup(func() { _ = database.Close() })

	_, err := serverWideMySQLLock().Acquire(t.Context(), database)

	require.ErrorContains(t, err, "returned NULL")
	require.Equal(t, int32(1), store.getLockCalls.Load())
}

func TestTableLock_AcquireAndRelease(t *testing.T) {
	t.Parallel()

	store := &fakeLockStore{}
	database := openFakeLockDB(store)
	t.Cleanup(func() { _ = database.Close() })

	lock := migrationTableLock()

	handle, err := lock.Acquire(t.Context(), database)
	require.NoError(t, err)
	require.NotNil(t, handle.connection, "table-based lock must pin a connection")
	require.NotNil(t, handle.transaction, "table-based lock must hold its transaction in the handle")

	require.NoError(t, lock.Release(t.Context(), handle))
}

func TestTableLock_TryAcquireReturnsLockNotAcquiredWhenHeld(t *testing.T) {
	t.Parallel()

	store := &fakeLockStore{}
	database := openFakeLockDB(store)
	t.Cleanup(func() { _ = database.Close() })

	lock := migrationTableLock()
	first, err := lock.Acquire(t.Context(), database)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lock.Release(t.Context(), first) })

	_, secondError := lock.TryAcquire(t.Context(), database)
	require.ErrorIs(t, secondError, migrate.ErrLockNotAcquired,
		"FOR UPDATE NOWAIT against a held row must map to ErrLockNotAcquired")
}

func TestTableLock_AcquirePropagatesNonContentionError(t *testing.T) {
	t.Parallel()

	store := &fakeLockStore{failTableLockSQL: true}
	database := openFakeLockDB(store)
	t.Cleanup(func() { _ = database.Close() })

	_, err := migrationTableLock().Acquire(t.Context(), database)
	require.Error(t, err)
	require.NotErrorIs(t, err, migrate.ErrLockNotAcquired,
		"a non-contention lock failure must not be misreported as ErrLockNotAcquired")
}

func TestPollingAdvisoryLockSpacesOutPollsThatReturnAtOnce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		interval time.Duration
		wait     time.Duration
		maxPolls int32
	}{
		{name: "the default interval", interval: lockPollMinimumInterval, wait: 100 * time.Millisecond, maxPolls: 1},
		{name: "a short interval", interval: 10 * time.Millisecond, wait: 100 * time.Millisecond, maxPolls: 12},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			store := &fakeLockStore{}
			database := openFakeLockDB(store)
			t.Cleanup(func() { _ = database.Close() })
			holder := serverWideMySQLLock()
			first, err := holder.Acquire(t.Context(), database)
			require.NoError(t, err)
			t.Cleanup(func() { _ = holder.Release(t.Context(), first) })
			contender := serverWideMySQLLock()
			contender.pollInterval = testCase.interval
			before := store.getLockCalls.Load()

			cause := errors.New("deploy window closed")
			ctx, cancel := context.WithTimeoutCause(t.Context(), testCase.wait, cause)
			defer cancel()
			_, err = contender.Acquire(ctx, database)

			require.ErrorIs(t, err, cause)
			polls := store.getLockCalls.Load() - before
			require.Positive(t, polls)
			require.LessOrEqual(t, polls, testCase.maxPolls, "a wait query that returns at once must not be polled in a tight loop")
		})
	}
}

func TestPostgresAdvisoryLockBindsItsKey(t *testing.T) {
	t.Parallel()

	for _, statement := range []string{postgresAdvisoryLockSQL, postgresAdvisoryTryLockSQL, postgresAdvisoryUnlockSQL} {
		require.Contains(t, statement, "(hashtext($1))", "the key must reach hashtext as a bound text parameter")
	}

	tests := []struct {
		take func(lock *postgresAdvisoryLock) func(context.Context, *sql.DB) (lockHandle, error)
		name string
	}{
		{name: "waiting", take: func(lock *postgresAdvisoryLock) func(context.Context, *sql.DB) (lockHandle, error) {
			return lock.Acquire
		}},
		{name: "trying", take: func(lock *postgresAdvisoryLock) func(context.Context, *sql.DB) (lockHandle, error) {
			return lock.TryAcquire
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			store := &fakeLockStore{}
			database := openFakeLockDB(store)
			t.Cleanup(func() { _ = database.Close() })
			lock := postgresMigrationLock()

			handle, err := testCase.take(lock)(t.Context(), database)
			require.NoError(t, err)
			require.NoError(t, lock.Release(t.Context(), handle))

			store.mu.Lock()
			defer store.mu.Unlock()
			require.Equal(t, []string{"sgl_migrations", "sgl_migrations"}, store.advisoryKeys)
			require.Equal(t, int32(1), store.releases.Load())
		})
	}
}

func TestIsDuplicateObjectError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		err  error
		want bool
	}{
		{sqlStateError{state: "23505", message: "duplicate key value violates unique constraint"}, true},
		{sqlStateError{state: "42P07", message: "relation already exists"}, true},
		{sqlStateError{state: "42710", message: "duplicate object"}, true},
		{sqlStateError{state: "40001", message: "restart transaction"}, true},
		{sqlStateError{state: "42601", message: "syntax error"}, false},
		{fmt.Errorf("creating: %w", sqlStateError{state: "23505", message: "duplicate"}), true},
		{errors.New(`table "sgl_migration_lock" already exists`), true},
		{errors.New("connection refused"), false},
	}

	for _, testCase := range cases {
		require.Equalf(t, testCase.want, isDuplicateObjectError(testCase.err), "error: %q", testCase.err.Error())
	}
}

func TestIsLockNotAvailableErrorMatchesOnlyTheDriverForms(t *testing.T) {
	t.Parallel()

	cases := []struct {
		err  error
		name string
		want bool
	}{
		{name: "pgx", err: errors.New(`ERROR: could not obtain lock on row in relation "app_migration_lock" (SQLSTATE 55P03)`), want: true},
		{name: "pgx code alone", err: errors.New("ERROR: canceling statement (SQLSTATE 55P03)"), want: true},
		{name: "lib/pq", err: errors.New(`pq: could not obtain lock on row in relation "app_migration_lock"`), want: true},
		{name: "go-sql-driver/mysql", err: errors.New("Error 1205 (HY000): Lock wait timeout exceeded; try restarting transaction"), want: true},
		{name: "older go-sql-driver/mysql", err: errors.New("Error 1205: Lock wait timeout exceeded; try restarting transaction"), want: true},
		{name: "the MySQL phrase", err: errors.New("Lock wait timeout exceeded"), want: true},
		{name: "a table named after error 1205", err: errors.New(`pq: relation "app1205_migration_lock" does not exist`), want: false},
		{name: "a table named after SQLSTATE 55P03", err: errors.New(`permission denied for table app55P03_migration_lock (SQLSTATE 42501)`), want: false},
		{name: "a MySQL error naming 1205", err: errors.New("Error 1146 (42S02): Table 'app.app1205_seed_lock' doesn't exist"), want: false},
		{name: "a bare number", err: errors.New("ERROR: 55P03 lock_not_available"), want: false},
		{name: "unrelated", err: errors.New("connection refused"), want: false},
		{name: "empty", err: errors.New(""), want: false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, isLockNotAvailableError(testCase.err))
		})
	}
}

func TestIsLockNotAvailableError_DetectsTypedPostgresSQLState(t *testing.T) {
	t.Parallel()

	lockError := fakePostgresSQLStateError{sqlState: postgresErrorCodeLockNotAvailable}
	require.True(t, isLockNotAvailableError(lockError), "typed 55P03 must be detected")

	wrapped := fmt.Errorf("acquire migration lock: %w", lockError)
	require.True(t, isLockNotAvailableError(wrapped), "wrapped typed 55P03 must be detected")

	other := fakePostgresSQLStateError{sqlState: "23505"}
	require.False(t, isLockNotAvailableError(other), "unrelated SQLSTATE must not match")
}

func TestLocksRejectUnsafeKeys(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	strategies := map[string]lockStrategy{
		"postgres":        &postgresAdvisoryLock{key: "drop table users; --", label: "PostgreSQL"},
		"postgres seed":   &postgresAdvisoryLock{key: "seeds; DROP TABLE users", label: "PostgreSQL seed"},
		"mysql":           newMySQLAdvisoryLock("drop table users; --", "MySQL", true),
		"mysql seed":      newMySQLAdvisoryLock("seeds'", "MySQL seed", false),
		"custom advisory": newCustomAdvisoryLock(testAdvisoryLockSQL, questionPlaceholder, "", "custom"),
	}
	for name, strategy := range strategies {
		_, acquireError := strategy.Acquire(ctx, nil)
		require.ErrorIs(t, acquireError, ErrInvalidIdentifier, name)
		_, tryError := strategy.TryAcquire(ctx, nil)
		require.ErrorIs(t, tryError, ErrInvalidIdentifier, name)
	}
}

func TestLocksReleaseAnEmptyHandle(t *testing.T) {
	t.Parallel()

	for name, strategy := range map[string]lockStrategy{
		"postgres": postgresMigrationLock(),
		"mysql":    serverWideMySQLLock(),
		"table":    migrationTableLock(),
		"none":     &noOpLock{},
	} {
		require.NoError(t, strategy.Release(t.Context(), lockHandle{}), name)
	}
}

func TestNoOpLockPinsNothing(t *testing.T) {
	t.Parallel()

	lock := &noOpLock{}

	handle, err := lock.Acquire(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, lockHandle{}, handle)

	handle, err = lock.TryAcquire(t.Context(), nil)
	require.NoError(t, err)
	require.Equal(t, lockHandle{}, handle)
}

func TestTableLockCommitIsCutShortWhenItStalls(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		stallCommit bool
		wantTimeout bool
	}{
		{name: "a stalled commit is cut short", stallCommit: true, wantTimeout: true},
		{name: "a prompt commit is not", stallCommit: false, wantTimeout: false},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			store := &stallingStore{stallCommit: testCase.stallCommit}
			database := sql.OpenDB(stallingConnector{store: store})
			t.Cleanup(func() { _ = database.Close() })
			acquisition, cancelAcquisition := context.WithCancel(t.Context())
			handle, err := migrationTableLock().Acquire(acquisition, database)
			require.NoError(t, err)
			cancelAcquisition()
			require.NoError(t, store.begun().Err(), "ending the acquisition's context must not end the lock transaction")

			var commitError error
			scaling.Within(t, time.Second, func(context.Context) {
				commitError = commitWithin(handle, 20*time.Millisecond)
			})
			_ = handle.connection.Close()

			require.Equal(t, testCase.wantTimeout, errors.Is(commitError, errLockReleaseTimeout))
			if !testCase.wantTimeout {
				require.NoError(t, commitError)
			}
			require.Error(t, store.begun().Err(), "the lock transaction's context must be released once the commit is done")
		})
	}
}

func TestHolderSlotRefusesAnEndedContextEveryTime(t *testing.T) {
	t.Parallel()

	const attempts = 200
	cause := errors.New("deploy window closed")
	for _, wait := range []bool{true, false} {
		ctx, cancel := context.WithCancelCause(t.Context())
		cancel(cause)
		slot := newHolderSlot()
		for range attempts {
			err := slot.claim(ctx, wait)
			require.ErrorIs(t, err, cause, "wait %t", wait)
			require.ErrorIs(t, err, context.Canceled, "wait %t", wait)
		}
		require.NoError(t, slot.claim(t.Context(), wait), "an ended context must leave the slot free")
		slot.free()
	}
}

func TestJoinWorkCloseErrorNamesTheRunsOwnConnection(t *testing.T) {
	t.Parallel()

	errRelease := errors.New("unlock timed out")
	errClose := errors.New("connection reset")
	tests := []struct {
		releaseError error
		closeError   error
		name         string
		wantMessage  string
	}{
		{name: "nothing failed"},
		{name: "the release failed", releaseError: errRelease, wantMessage: "unlock timed out"},
		{name: "the close failed", closeError: errClose, wantMessage: "closing seed connection: connection reset"},
		{name: "both failed", releaseError: errRelease, closeError: errClose, wantMessage: "unlock timed out\nclosing seed connection: connection reset"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := joinWorkCloseError("seed", testCase.releaseError, testCase.closeError)

			if testCase.wantMessage == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, testCase.wantMessage)
		})
	}
}

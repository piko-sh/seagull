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
	"slices"
	"strings"
	"time"

	"seagull.sh/seagull/internal/migrate"
)

const (
	// lockReleaseTimeout bounds how long Release waits for the unlock SQL.
	//
	// Release is invoked from deferred cleanup after possibly long-running migrations. Even
	// when the caller's context is cancelled the lock is still freed so subsequent migration
	// runs do not hang waiting for the session to be reaped.
	lockReleaseTimeout = 5 * time.Second

	// mysqlLockPollSeconds is how long one GET_LOCK call waits before the wait loop checks
	// the context and tries again.
	mysqlLockPollSeconds = 5

	// mysqlLockNameLimit is the longest name MySQL accepts for GET_LOCK, in characters.
	mysqlLockNameLimit = 64

	// tableLockCreateAttempts bounds how often a lock table's CREATE is retried when a
	// concurrent creator wins the race.
	tableLockCreateAttempts = 3

	// errorFormatPinningConnection holds the format string for connection pinning errors.
	errorFormatPinningConnection = "pinning database connection: %w"

	// postgresErrorCodeLockNotAvailable is the SQLSTATE PostgreSQL reports when a SELECT ...
	// FOR UPDATE NOWAIT fails because the row is locked by another session.
	postgresErrorCodeLockNotAvailable = "55P03"

	// mysqlErrorNumberLockWaitTimeout is the MySQL error number reported as a string by
	// driver error messages when the lock wait exceeds innodb_lock_wait_timeout.
	mysqlErrorNumberLockWaitTimeout = "1205"

	// mysqlErrorNumberLockWaitTimeoutUint is the numeric form of MySQL error 1205 used by
	// the typed-driver-error check in isLockNotAvailableError.
	mysqlErrorNumberLockWaitTimeoutUint uint16 = 1205

	// lockPollMinimumInterval is the least time between two polls of a polling advisory
	// lock, so a wait query that returns at once, rather than blocking on the server as
	// MySQL's GET_LOCK does, cannot turn the wait into a tight loop of round trips.
	lockPollMinimumInterval = 500 * time.Millisecond

	// lockCheckTimeout bounds the statement that confirms a table lock's transaction is
	// still open before each migration or seed.
	lockCheckTimeout = 5 * time.Second

	// lockCheckSQL is the statement run in a table lock's transaction to confirm the
	// transaction, and so the row lock it holds, is still open. It reads no data, so it
	// cannot conflict with anything the run does.
	lockCheckSQL = "SELECT 1"

	// postgresAdvisoryLockSQL waits for the session-level advisory lock on the hash of the
	// key bound as its one parameter.
	postgresAdvisoryLockSQL = "SELECT pg_advisory_lock(hashtext($1))"

	// postgresAdvisoryTryLockSQL takes the advisory lock on the hash of the key bound as its
	// one parameter only when it is free.
	postgresAdvisoryTryLockSQL = "SELECT pg_try_advisory_lock(hashtext($1))"

	// postgresAdvisoryUnlockSQL releases the advisory lock on the hash of the key bound as
	// its one parameter.
	postgresAdvisoryUnlockSQL = "SELECT pg_advisory_unlock(hashtext($1))"

	// postgresKeepLockOpenSQL stops the server ending a table lock's transaction while it
	// sits idle for the whole run, as a server with idle_in_transaction_session_timeout set
	// would; it applies to the lock transaction only.
	postgresKeepLockOpenSQL = "SET LOCAL idle_in_transaction_session_timeout = 0"
)

var (
	// errLockReleaseTimeout is the cause attached to the detached release context's deadline
	// so a caller inspecting context.Cause can tell a slow unlock apart from an unrelated
	// cancellation of the parent context.
	errLockReleaseTimeout = errors.New("migration lock release timed out")

	// duplicateObjectSQLStates lists the SQLSTATEs a CREATE TABLE IF NOT EXISTS reports when
	// a concurrent session created the same table first: a unique violation on PostgreSQL's
	// catalogue (23505), a duplicate table (42P07), a duplicate object (42710), and
	// CockroachDB's retryable serialisation failure (40001).
	duplicateObjectSQLStates = []string{"23505", "42P07", "42710", "40001"}

	// lockNotAvailableMessages lists text marking a lock-not-available error from a driver
	// whose errors expose no SQLSTATE or error number.
	//
	// go-sql-driver/mysql writes "Error 1205 (HY000): ..." (or "Error 1205: ..." in older
	// releases), pgx ends its message with "(SQLSTATE 55P03)", and PostgreSQL and MySQL word
	// the failure as the last two phrases. A bare number is not enough, as a table or schema
	// name can hold it.
	lockNotAvailableMessages = []string{
		"Error " + mysqlErrorNumberLockWaitTimeout + " (",
		"Error " + mysqlErrorNumberLockWaitTimeout + ":",
		"SQLSTATE " + postgresErrorCodeLockNotAvailable,
		"could not obtain lock",
		"Lock wait timeout exceeded",
	}

	// postgresTableLockSessionSQL holds the statements the PostgreSQL table lock runs in its
	// transaction before taking the row lock.
	postgresTableLockSessionSQL = []string{postgresKeepLockOpenSQL}
)

// postgresTableLockSQL renders PostgreSQL's single-row lock tables, which CockroachDB
// also accepts.
var postgresTableLockSQL = TableLockSQL{
	Create: func(table, constraint string) string {
		return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    lock_id INTEGER NOT NULL PRIMARY KEY DEFAULT 1,
    CONSTRAINT %s CHECK (lock_id = 1)
)`, table, constraint)
	},
	InsertRow: func(table string) string {
		return fmt.Sprintf("INSERT INTO %s (lock_id) VALUES (1) ON CONFLICT DO NOTHING", table)
	},
	Lock: func(table string) string {
		return fmt.Sprintf("SELECT lock_id FROM %s FOR UPDATE", table)
	},
	TryLock: func(table string) string {
		return fmt.Sprintf("SELECT lock_id FROM %s FOR UPDATE NOWAIT", table)
	},
}

// lockHandle is what one acquisition of a lock holds. The executor keeps it and passes it
// back to Release, so lock strategies hold no per-acquisition state and one Backend can
// serve any number of executors at once.
type lockHandle struct {
	// connection is the session that holds the lock, or nil when the lock pins none.
	connection *sql.Conn

	// transaction is the open transaction that holds a row lock, or nil for session locks.
	// Work cannot share a session whose transaction holds the lock, so the executor pins a
	// second connection for it.
	transaction *sql.Tx

	// cancel ends the context the lock transaction was begun on, which ends the transaction
	// and its session; nil for session locks. Release calls it when the commit outlasts
	// lockReleaseTimeout, and always once the commit is done.
	cancel context.CancelCauseFunc

	// name is the lock name resolved at acquisition, so release names the same lock even if
	// the session's schema changed meanwhile.
	name string

	// discard asks Release to close the session rather than return it to the pool, as the
	// run that held the lock worked on it and may have changed its settings.
	discard bool
}

// lockStrategy takes and releases a database-specific lock that serialises migration or
// seed runs across processes. Implementations are immutable; each acquisition's state
// lives in the lockHandle it returns.
type lockStrategy interface {
	// Acquire takes the lock, waiting until it is free or ctx ends.
	//
	// Takes database (*sql.DB) which is the connection pool to lock through.
	//
	// Returns lockHandle which holds what the acquisition pinned.
	// Returns error when the lock cannot be acquired.
	Acquire(ctx context.Context, database *sql.DB) (lockHandle, error)

	// TryAcquire takes the lock only when it is free.
	//
	// Takes database (*sql.DB) which is the connection pool to lock through.
	//
	// Returns lockHandle which holds what the acquisition pinned.
	// Returns error which wraps migrate.ErrLockNotAcquired when another session holds the
	// lock, or the failure otherwise.
	TryAcquire(ctx context.Context, database *sql.DB) (lockHandle, error)

	// Release frees a lock taken by Acquire or TryAcquire. A session whose unlock fails is
	// discarded rather than returned to the pool, so it cannot keep holding the lock.
	//
	// Takes handle (lockHandle) which is what the acquisition returned.
	//
	// Returns error when the lock cannot be released.
	Release(ctx context.Context, handle lockHandle) error
}

// postgresAdvisoryLock takes PostgreSQL's session-scoped pg_advisory_lock on the hash of
// its key. PostgreSQL advisory locks are per database, so the key needs no scoping.
type postgresAdvisoryLock struct {
	// key is bound as a parameter and hashed with hashtext() to form the lock; it must be a
	// safe identifier.
	key string

	// label names the lock in error messages, such as "PostgreSQL seed".
	label string
}

// Acquire pins a connection and blocks in pg_advisory_lock until the lock is held or ctx
// ends; a session whose wait was interrupted is discarded.
//
// Takes database (*sql.DB) which is the connection pool to pin a connection from.
//
// Returns lockHandle which holds the pinned connection.
// Returns error when the connection cannot be pinned or the lock fails.
func (lock *postgresAdvisoryLock) Acquire(ctx context.Context, database *sql.DB) (lockHandle, error) {
	if err := validateLockKey(lock.key, lock.label); err != nil {
		return lockHandle{}, err
	}
	connection, connectionError := database.Conn(ctx)
	if connectionError != nil {
		return lockHandle{}, withContextCause(ctx, fmt.Errorf(errorFormatPinningConnection, connectionError))
	}

	if _, lockError := connection.ExecContext(ctx, postgresAdvisoryLockSQL, lock.key); lockError != nil {
		discardConnection(connection)
		return lockHandle{}, withContextCause(ctx, fmt.Errorf("acquiring %s advisory lock: %w", lock.label, lockError))
	}
	return lockHandle{connection: connection, transaction: nil, name: lock.key, cancel: nil, discard: false}, nil
}

// TryAcquire pins a connection and runs pg_try_advisory_lock.
//
// Takes database (*sql.DB) which is the connection pool to pin a connection from.
//
// Returns lockHandle which holds the pinned connection.
// Returns error which wraps migrate.ErrLockNotAcquired when another session holds the
// lock.
func (lock *postgresAdvisoryLock) TryAcquire(ctx context.Context, database *sql.DB) (lockHandle, error) {
	if err := validateLockKey(lock.key, lock.label); err != nil {
		return lockHandle{}, err
	}
	connection, connectionError := database.Conn(ctx)
	if connectionError != nil {
		return lockHandle{}, withContextCause(ctx, fmt.Errorf(errorFormatPinningConnection, connectionError))
	}

	var acquired bool
	if scanError := connection.QueryRowContext(ctx, postgresAdvisoryTryLockSQL, lock.key).Scan(&acquired); scanError != nil {
		discardConnection(connection)
		return lockHandle{}, withContextCause(ctx, fmt.Errorf("scanning %s try-lock result: %w", lock.label, scanError))
	}
	if !acquired {
		_ = connection.Close()
		return lockHandle{}, migrate.ErrLockNotAcquired
	}
	return lockHandle{connection: connection, transaction: nil, name: lock.key, cancel: nil, discard: false}, nil
}

// Release runs pg_advisory_unlock on a context detached from cancellation, then returns
// the session to the pool, or discards it when the unlock failed.
//
// Takes handle (lockHandle) which holds the pinned connection.
//
// Returns error when the unlock or the close fails.
func (lock *postgresAdvisoryLock) Release(ctx context.Context, handle lockHandle) error {
	if handle.connection == nil {
		return nil
	}
	if err := validateLockKey(lock.key, lock.label); err != nil {
		discardConnection(handle.connection)
		return err
	}

	releaseCtx, cancel := detachContextForRelease(ctx)
	defer cancel()
	_, unlockError := handle.connection.ExecContext(releaseCtx, postgresAdvisoryUnlockSQL, lock.key)
	closeError := finishSession(handle, unlockError)
	return joinLockReleaseErrors("releasing "+lock.label+" advisory lock", unlockError, closeError)
}

// pollingAdvisoryLock takes a session-scoped named lock through rendered SQL that waits a
// bounded time per call, as MySQL's GET_LOCK does. MySQL and Custom backends use it.
type pollingAdvisoryLock struct {
	// key is the lock key; it must be a safe identifier.
	key string

	// nameSQL, when set, is the query that resolves the lock name for key on the pinned
	// session; empty means the name is key itself.
	nameSQL string

	// waitSQL takes the lock name as its one bind parameter and waits a bounded time.
	waitSQL string

	// trySQL takes the lock name as its one bind parameter and does not wait.
	trySQL string

	// releaseSQL takes the lock name as its one bind parameter and releases the lock.
	releaseSQL string

	// description is how Names reports the key.
	description string

	// label names the lock in error messages, such as "MySQL seed".
	label string

	// pollInterval is the least time between the starts of two polls of the wait query; the
	// constructors set it to lockPollMinimumInterval.
	pollInterval time.Duration

	// serverWide records that the key is not scoped to the current schema.
	serverWide bool
}

// newMySQLAdvisoryLock builds a GET_LOCK lock for a key.
//
// Takes key (string) which is the lock key.
// Takes label (string) which names the lock in error messages.
// Takes serverWide (bool) which keeps the key server-wide instead of scoping it to the
// current schema.
//
// Returns *pollingAdvisoryLock which is the lock.
func newMySQLAdvisoryLock(key, label string, serverWide bool) *pollingAdvisoryLock {
	lock := &pollingAdvisoryLock{
		key:          key,
		nameSQL:      "",
		waitSQL:      fmt.Sprintf("SELECT GET_LOCK(?, %d)", mysqlLockPollSeconds),
		trySQL:       "SELECT GET_LOCK(?, 0)",
		releaseSQL:   "SELECT RELEASE_LOCK(?)",
		description:  key,
		label:        label,
		pollInterval: lockPollMinimumInterval,
		serverWide:   serverWide,
	}
	if !serverWide {
		lock.nameSQL = "SELECT " + mysqlScopedLockName(key)
		lock.description = mysqlScopedLockDescription(key)
	}
	return lock
}

// newCustomAdvisoryLock builds a lock from a BackendSpec's advisory lock SQL.
//
// Takes statements (AdvisoryLockSQL) which renders the lock's SQL.
// Takes placeholder (func(int) string) which renders the bind-parameter marker.
// Takes key (string) which is the lock key.
// Takes label (string) which names the lock in error messages.
//
// Returns *pollingAdvisoryLock which is the lock.
func newCustomAdvisoryLock(statements AdvisoryLockSQL, placeholder func(int) string, key, label string) *pollingAdvisoryLock {
	lock := &pollingAdvisoryLock{
		key:          key,
		nameSQL:      "",
		waitSQL:      statements.Wait(placeholder(1)),
		trySQL:       statements.Try(placeholder(1)),
		releaseSQL:   statements.Release(placeholder(1)),
		description:  key,
		label:        label,
		pollInterval: lockPollMinimumInterval,
		serverWide:   false,
	}
	if statements.Name != nil {
		lock.nameSQL = statements.Name(key)
	}
	return lock
}

// Acquire pins a connection, resolves the lock name, and polls the wait query until the
// lock is held or ctx ends; a session whose wait failed is discarded. Polls start at
// least pollInterval apart, so a wait query that returns at once is not run in a tight
// loop.
//
// Takes database (*sql.DB) which is the connection pool to pin a connection from.
//
// Returns lockHandle which holds the pinned connection and the lock name.
// Returns error when the connection cannot be pinned, the lock reports an error, or ctx
// ends first, which wraps ctx's cause wherever the end lands, between two polls or inside
// one.
func (lock *pollingAdvisoryLock) Acquire(ctx context.Context, database *sql.DB) (lockHandle, error) {
	connection, name, err := lock.pin(ctx, database)
	if err != nil {
		return lockHandle{}, withContextCause(ctx, err)
	}
	for {
		handle, finished, pollError := lock.poll(ctx, connection, name)
		if finished {
			return handle, pollError
		}
	}
}

// TryAcquire pins a connection, resolves the lock name, and takes the lock only when it
// is free.
//
// Takes database (*sql.DB) which is the connection pool to pin a connection from.
//
// Returns lockHandle which holds the pinned connection and the lock name.
// Returns error which wraps migrate.ErrLockNotAcquired when another session holds the
// lock.
func (lock *pollingAdvisoryLock) TryAcquire(ctx context.Context, database *sql.DB) (lockHandle, error) {
	connection, name, err := lock.pin(ctx, database)
	if err != nil {
		return lockHandle{}, withContextCause(ctx, err)
	}
	acquired, queryError := queryLockResult(ctx, connection, lock.trySQL, name)
	if queryError != nil {
		discardConnection(connection)
		return lockHandle{}, withContextCause(ctx, fmt.Errorf("acquiring %s lock: %w", lock.label, queryError))
	}
	if !acquired {
		_ = connection.Close()
		return lockHandle{}, migrate.ErrLockNotAcquired
	}
	return lockHandle{connection: connection, transaction: nil, name: name, cancel: nil, discard: false}, nil
}

// Release runs the release query on a context detached from cancellation, then returns
// the session to the pool, or discards it when the release failed.
//
// Takes handle (lockHandle) which holds the pinned connection and the lock name.
//
// Returns error when the release or the close fails.
func (lock *pollingAdvisoryLock) Release(ctx context.Context, handle lockHandle) error {
	if handle.connection == nil {
		return nil
	}
	releaseCtx, cancel := detachContextForRelease(ctx)
	defer cancel()
	_, unlockError := handle.connection.ExecContext(releaseCtx, lock.releaseSQL, handle.name)
	closeError := finishSession(handle, unlockError)
	return joinLockReleaseErrors("releasing "+lock.label+" lock", unlockError, closeError)
}

// poll runs the wait query once and, when the lock was not acquired, waits until
// pollInterval has passed since the query began, or ctx ends. A session whose wait failed
// or was cut short is discarded.
//
// Takes connection (*sql.Conn) which is the pinned connection.
// Takes name (string) which is the lock name.
//
// Returns lockHandle which holds the pinned connection when the lock was acquired.
// Returns bool which is true when the wait is over: the lock was acquired, or it failed.
// Returns error when the lock reports an error or ctx ends, wrapping ctx's cause then.
func (lock *pollingAdvisoryLock) poll(ctx context.Context, connection *sql.Conn, name string) (lockHandle, bool, error) {
	nextPoll := time.NewTimer(lock.pollInterval)
	defer nextPoll.Stop()
	acquired, queryError := queryLockResult(ctx, connection, lock.waitSQL, name)
	switch {
	case queryError == nil && acquired:
		return lockHandle{connection: connection, transaction: nil, name: name, cancel: nil, discard: false}, true, nil
	case ctx.Err() != nil:
		discardConnection(connection)
		return lockHandle{}, true, fmt.Errorf("waiting for %s lock %s: %w", lock.label, lock.key, context.Cause(ctx))
	case queryError != nil:
		discardConnection(connection)
		return lockHandle{}, true, fmt.Errorf("acquiring %s lock: %w", lock.label, queryError)
	}
	select {
	case <-nextPoll.C:
		return lockHandle{}, false, nil
	case <-ctx.Done():
		discardConnection(connection)
		return lockHandle{}, true, fmt.Errorf("waiting for %s lock %s: %w", lock.label, lock.key, context.Cause(ctx))
	}
}

// pin validates the key, pins a connection and resolves the lock name on it.
//
// Takes database (*sql.DB) which is the connection pool to pin a connection from.
//
// Returns *sql.Conn which is the pinned connection.
// Returns string which is the lock name.
// Returns error when the key is unsafe, no connection can be pinned, or the name query
// fails.
func (lock *pollingAdvisoryLock) pin(ctx context.Context, database *sql.DB) (*sql.Conn, string, error) {
	if err := validateLockKey(lock.key, lock.label); err != nil {
		return nil, "", err
	}
	connection, connectionError := database.Conn(ctx)
	if connectionError != nil {
		return nil, "", fmt.Errorf(errorFormatPinningConnection, connectionError)
	}
	if lock.nameSQL == "" {
		return connection, lock.key, nil
	}
	var name string
	if scanError := connection.QueryRowContext(ctx, lock.nameSQL).Scan(&name); scanError != nil {
		discardConnection(connection)
		return nil, "", fmt.Errorf("resolving %s lock name: %w", lock.label, scanError)
	}
	return connection, name, nil
}

// tableLock holds a row lock on a single-row lock table with SELECT ...
//
// FOR UPDATE inside an open transaction. It works where session-level advisory locks do
// not survive, such as behind PgBouncer in transaction mode, and on CockroachDB.
type tableLock struct {
	// table is the lock table.
	table string

	// createSQL creates the lock table if it does not exist.
	createSQL string

	// insertSQL inserts the single lock row, doing nothing when it exists.
	insertSQL string

	// lockSQL waits for the row lock.
	lockSQL string

	// tryLockSQL takes the row lock only when it is free.
	tryLockSQL string

	// label names the lock in error messages, such as "seed table".
	label string

	// sessionSQL holds the statements run in the lock transaction before the row lock, such
	// as one that keeps the server from ending the transaction while it sits idle.
	sessionSQL []string
}

// newTableLock builds a table lock from rendered SQL for a lock table.
//
// Takes statements (TableLockSQL) which renders the lock's SQL.
// Takes table (string) which is the lock table.
// Takes constraint (string) which is the single-row CHECK constraint.
// Takes label (string) which names the lock in error messages.
// Takes sessionSQL (...string) which are run in the lock transaction before the row lock.
//
// Returns *tableLock which is the lock.
//
// Concurrency: the returned strategy is immutable; acquisitions use separate handles.
func newTableLock(statements TableLockSQL, table, constraint, label string, sessionSQL ...string) *tableLock {
	return &tableLock{
		table:      table,
		createSQL:  statements.Create(table, constraint),
		insertSQL:  statements.InsertRow(table),
		lockSQL:    statements.Lock(table),
		tryLockSQL: statements.TryLock(table),
		label:      label,
		sessionSQL: sessionSQL,
	}
}

// newPostgresTableLock builds PostgreSQL's table lock, whose transaction turns off
// idle_in_transaction_session_timeout, so a server that sets it cannot end the lock while
// the run works on its own connection. CockroachDB's lock builds on it.
//
// Takes table (string) which is the lock table.
// Takes constraint (string) which is the single-row CHECK constraint.
// Takes label (string) which names the lock in error messages.
//
// Returns *tableLock which is the lock.
func newPostgresTableLock(table, constraint, label string) *tableLock {
	return newTableLock(postgresTableLockSQL, table, constraint, label, postgresTableLockSessionSQL...)
}

// Acquire pins a connection, creates the lock table if needed, inserts the lock row, and
// waits for the row lock inside a held transaction.
//
// Takes database (*sql.DB) which is the connection pool to pin a connection from.
//
// Returns lockHandle which holds the pinned connection and the lock transaction.
// Returns error when any step of the acquisition fails.
func (lock *tableLock) Acquire(ctx context.Context, database *sql.DB) (lockHandle, error) {
	return lock.acquire(ctx, database, lock.lockSQL)
}

// TryAcquire is like Acquire but does not wait for the row lock.
//
// Takes database (*sql.DB) which is the connection pool to pin a connection from.
//
// Returns lockHandle which holds the pinned connection and the lock transaction.
// Returns error which wraps migrate.ErrLockNotAcquired when another session holds the row
// lock.
func (lock *tableLock) TryAcquire(ctx context.Context, database *sql.DB) (lockHandle, error) {
	return lock.acquire(ctx, database, lock.tryLockSQL)
}

// Release commits the lock transaction, which frees the row lock, then returns the
// session to the pool, or discards it when the commit failed.
//
// *sql.Tx Commit takes no context, so the caller's context is not used. A commit that
// outlasts lockReleaseTimeout, as on a stalled server or a half-open connection, is cut
// short by cancelling the context the transaction was begun on: a driver that watches it,
// as pgx does, abandons the commit, and the session is discarded, which frees the row
// lock.
//
// Takes handle (lockHandle) which holds the pinned connection and the lock transaction.
//
// Returns error when the commit or the close fails, wrapping errLockReleaseTimeout when
// the commit was cut short.
func (lock *tableLock) Release(_ context.Context, handle lockHandle) error {
	if handle.connection == nil {
		return nil
	}
	var commitError error
	if handle.transaction != nil {
		commitError = commitWithin(handle, lockReleaseTimeout)
	}
	closeError := finishSession(handle, commitError)
	return joinLockReleaseErrors("committing "+lock.label+" lock transaction", commitError, closeError)
}

// acquire pins a connection, creates the lock table, inserts the lock row, opens a
// transaction and runs the row-lock statement. On failure the session is discarded,
// except when the row was locked by another session.
//
// Takes database (*sql.DB) which is the connection pool to pin from.
// Takes rowLockSQL (string) which is the waiting or non-waiting row-lock statement.
//
// Returns lockHandle which holds the pinned connection and the lock transaction.
// Returns error which wraps migrate.ErrLockNotAcquired when the row is locked, or the
// failure otherwise.
func (lock *tableLock) acquire(ctx context.Context, database *sql.DB, rowLockSQL string) (lockHandle, error) {
	connection, pinError := lock.pinWithRow(ctx, database)
	if pinError != nil {
		return lockHandle{}, withContextCause(ctx, pinError)
	}
	transaction, cancel, beginError := lock.begin(ctx, connection)
	if beginError != nil {
		return lockHandle{}, withContextCause(ctx, beginError)
	}
	handle, lockError := lock.lockRow(ctx, connection, transaction, cancel, rowLockSQL)
	return handle, withContextCause(ctx, lockError)
}

// pinWithRow pins a connection and makes sure the lock table and its single row exist. On
// failure the session is discarded.
//
// Takes database (*sql.DB) which is the connection pool to pin from.
//
// Returns *sql.Conn which is the pinned connection.
// Returns error when no connection can be pinned or the table or row cannot be created.
func (lock *tableLock) pinWithRow(ctx context.Context, database *sql.DB) (*sql.Conn, error) {
	connection, connectionError := database.Conn(ctx)
	if connectionError != nil {
		return nil, fmt.Errorf(errorFormatPinningConnection, connectionError)
	}
	if createError := lock.createTable(ctx, connection); createError != nil {
		discardConnection(connection)
		return nil, fmt.Errorf("creating %s lock table: %w", lock.label, createError)
	}
	if _, insertError := connection.ExecContext(ctx, lock.insertSQL); insertError != nil {
		discardConnection(connection)
		return nil, fmt.Errorf("inserting %s lock row: %w", lock.label, insertError)
	}
	return connection, nil
}

// begin opens the lock transaction on the pinned connection, discarding the session when
// it cannot.
//
// The transaction is begun on a context detached from ctx's cancellation, because
// database/sql rolls a transaction back when its context ends: the lock must last until
// Release, not until the acquisition's context ends, which under migrate.WithLockTimeout
// is as soon as the lock is held. The detached context can still be cancelled through the
// returned function, which Release does when the commit outlasts its bound.
//
// Takes connection (*sql.Conn) which is the pinned connection.
//
// Returns *sql.Tx which is the open lock transaction.
// Returns context.CancelCauseFunc which ends the transaction's context.
// Returns error when the transaction cannot be opened.
func (lock *tableLock) begin(ctx context.Context, connection *sql.Conn) (*sql.Tx, context.CancelCauseFunc, error) {
	transactionContext, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	transaction, beginError := connection.BeginTx(transactionContext, nil)
	if beginError != nil {
		cancel(nil)
		discardConnection(connection)
		return nil, nil, fmt.Errorf("beginning %s lock transaction: %w", lock.label, beginError)
	}
	return transaction, cancel, nil
}

// lockRow runs the session statements and then the row-lock statement in the open lock
// transaction. On failure the transaction is rolled back and the session discarded,
// except when the row was locked by another session, when the session returns to the
// pool.
//
// Takes connection (*sql.Conn) which is the pinned connection.
// Takes transaction (*sql.Tx) which is the open lock transaction on it.
// Takes cancel (context.CancelCauseFunc) which ends the transaction's context.
// Takes rowLockSQL (string) which is the waiting or non-waiting row-lock statement.
//
// Returns lockHandle which holds the pinned connection and the lock transaction.
// Returns error which wraps migrate.ErrLockNotAcquired when the row is locked, or the
// failure otherwise.
func (lock *tableLock) lockRow(
	ctx context.Context,
	connection *sql.Conn,
	transaction *sql.Tx,
	cancel context.CancelCauseFunc,
	rowLockSQL string,
) (lockHandle, error) {
	for _, statement := range lock.sessionSQL {
		if _, sessionError := transaction.ExecContext(ctx, statement); sessionError != nil {
			_ = transaction.Rollback()
			cancel(nil)
			discardConnection(connection)
			return lockHandle{}, fmt.Errorf("preparing %s lock transaction: %w", lock.label, sessionError)
		}
	}
	if _, lockError := transaction.ExecContext(ctx, rowLockSQL); lockError != nil {
		rollbackError := transaction.Rollback()
		cancel(nil)
		if !isLockNotAvailableError(lockError) {
			discardConnection(connection)
			return lockHandle{}, fmt.Errorf("acquiring %s lock: %w", lock.label, lockError)
		}
		_ = finishSession(lockHandle{connection: connection, transaction: nil, name: lock.table, cancel: nil, discard: false}, rollbackError)
		return lockHandle{}, migrate.ErrLockNotAcquired
	}
	return lockHandle{connection: connection, transaction: transaction, name: lock.table, cancel: cancel, discard: false}, nil
}

// createTable runs the lock table's CREATE TABLE IF NOT EXISTS, retrying when a
// concurrent session created the table first. IF NOT EXISTS is not race-safe on
// PostgreSQL: the loser of two concurrent creates fails with a unique violation on the
// catalogue, and by then the winner's table exists.
//
// Takes connection (*sql.Conn) which is the pinned connection.
//
// Returns error when the table cannot be created.
func (lock *tableLock) createTable(ctx context.Context, connection *sql.Conn) error {
	var createError error
	for range tableLockCreateAttempts {
		if _, createError = connection.ExecContext(ctx, lock.createSQL); createError == nil || !isDuplicateObjectError(createError) {
			return createError
		}
	}
	return createError
}

// noOpLock takes no lock, for engines that serialise writers themselves (SQLite) or offer
// no lock (ClickHouse, DuckDB), and for LockNone.
type noOpLock struct{}

// Acquire takes no lock.
//
// Returns lockHandle which pins nothing.
// Returns error which is always nil.
func (*noOpLock) Acquire(_ context.Context, _ *sql.DB) (lockHandle, error) { return lockHandle{}, nil }

// TryAcquire takes no lock.
//
// Returns lockHandle which pins nothing.
// Returns error which is always nil.
func (*noOpLock) TryAcquire(_ context.Context, _ *sql.DB) (lockHandle, error) {
	return lockHandle{}, nil
}

// Release releases nothing.
//
// Returns error which is always nil.
func (*noOpLock) Release(_ context.Context, _ lockHandle) error { return nil }

// mysqlNumberedError matches errors that expose a MySQL error number through a method,
// without importing a driver. go-sql-driver/mysql exposes Number as a field instead and
// is handled by the message fallback in isLockNotAvailableError.
type mysqlNumberedError interface {
	error

	// Number returns the driver-assigned MySQL error number.
	//
	// Returns uint16 which is the driver-assigned MySQL error number.
	Number() uint16
}

// sqlStateReporter matches the typed error shape of drivers that report a SQLSTATE code
// through a SQLState method (lib/pq and pgx/stdlib both qualify), without importing a
// driver.
type sqlStateReporter interface {
	error

	// SQLState returns the five-character SQLSTATE code reported by the server.
	//
	// Returns string which is the five-character SQLSTATE code.
	SQLState() string
}

// holderSlot admits one lock holder at a time within the process. A goroutine that wants
// the lock while another holds it waits here, rather than on the database, so it never
// shares the holder's pinned session.
type holderSlot chan struct{}

// newHolderSlot returns an empty slot.
//
// Returns holderSlot which admits one holder.
func newHolderSlot() holderSlot {
	return make(holderSlot, 1)
}

// claim takes the slot, waiting until it is free or ctx ends when wait is true. A ctx
// that has already ended never takes the slot, even a free one.
//
// Takes wait (bool) which reports whether to wait for the current holder.
//
// Returns error which wraps migrate.ErrLockNotAcquired when wait is false and the slot is
// taken, or the context's error and cause when ctx ends first.
func (slot holderSlot) claim(ctx context.Context, wait bool) error {
	if stopped := contextStopped(ctx); stopped != nil {
		return fmt.Errorf("claiming the lock in this process: %w", stopped)
	}
	if !wait {
		select {
		case slot <- struct{}{}:
			return nil
		default:
			return fmt.Errorf("another goroutine in this process holds the lock: %w", migrate.ErrLockNotAcquired)
		}
	}
	select {
	case slot <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("waiting for another goroutine in this process to release the lock: %w", context.Cause(ctx))
	}
}

// free empties the slot for the next holder.
func (slot holderSlot) free() {
	<-slot
}

// heldLock is the state of a lock an executor holds and of the run that holds it.
type heldLock struct {
	// workConnection is a connection pinned for the run's work apart from the lock's own
	// session: under a lock whose session holds an open transaction (the table lock), and,
	// on an engine whose sessions keep a current schema, under a lock that pins no session.
	// Nil otherwise.
	workConnection *sql.Conn

	// schema is the schema the run's session created unqualified tables in when the run
	// began, or empty when the engine keeps none or the session had none.
	schema string

	// handle is what the lock's acquisition pinned.
	handle lockHandle
}

// detachContextForRelease returns a context that does not inherit cancellation from
// parent, with a short timeout so the unlock SQL has a fair chance to run even when the
// parent context is already cancelled.
//
// Returns context.Context which is the detached release context.
// Returns context.CancelFunc which the caller must invoke to release the timer.
func detachContextForRelease(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeoutCause(context.WithoutCancel(parent), lockReleaseTimeout, errLockReleaseTimeout)
}

// validateLockKey checks that lockKey is safe to interpolate into lock SQL. Every
// acquisition runs it, so a strategy built with an unsafe key fails rather than enabling
// injection.
//
// Takes lockKey (string) which is the textual lock identifier to validate.
// Takes site (string) which names the lock in the error message.
//
// Returns error which wraps ErrInvalidIdentifier when lockKey fails the safe-identifier
// grammar.
func validateLockKey(lockKey, site string) error {
	if err := validateIdentifier(lockKey); err != nil {
		return fmt.Errorf("%s lock key: %w", site, err)
	}
	return nil
}

// joinLockReleaseErrors combines an unlock error and a close error into a single error so
// the caller can observe both when both fail.
//
// Takes prefix (string) which describes the unlock step for error wrapping.
// Takes unlockError (error) which is the failure from running the unlock SQL.
// Takes closeError (error) which is the failure from closing the connection.
//
// Returns error which is nil when neither failed, the surviving error when only one
// failed, and a joined error otherwise.
func joinLockReleaseErrors(prefix string, unlockError, closeError error) error {
	var wrappedUnlock error
	if unlockError != nil {
		wrappedUnlock = fmt.Errorf("%s: %w", prefix, unlockError)
	}
	return errors.Join(wrappedUnlock, closeError)
}

// discardConnection closes a pinned connection without returning its session to the pool,
// so a session that may still hold a lock is ended rather than reused.
//
// Takes connection (*sql.Conn) which is the connection to discard.
func discardConnection(connection *sql.Conn) {
	_ = connection.Raw(func(any) error { return driver.ErrBadConn })
}

// finishSession returns a healthy session to the pool, or discards one whose last lock
// statement failed or that a run asked to end.
//
// Takes handle (lockHandle) which holds the pinned connection.
// Takes failure (error) which is the lock statement's failure, or nil.
//
// Returns error which is the close failure of a healthy session.
func finishSession(handle lockHandle, failure error) error {
	if failure != nil || handle.discard {
		discardConnection(handle.connection)
		return nil
	}
	return handle.connection.Close()
}

// queryLockResult runs one lock query on a pinned connection.
//
// Takes connection (*sql.Conn) which is the pinned connection.
// Takes query (string) which is the lock statement.
// Takes name (string) which is the lock name, bound as the query's one parameter.
//
// Returns bool which is true when the lock was acquired and false when it was not.
// Returns error when the query fails or reports NULL.
func queryLockResult(ctx context.Context, connection *sql.Conn, query, name string) (bool, error) {
	var acquired sql.NullBool
	if scanError := connection.QueryRowContext(ctx, query, name).Scan(&acquired); scanError != nil {
		return false, fmt.Errorf("scanning lock result: %w", scanError)
	}
	if !acquired.Valid {
		return false, errors.New("the lock query returned NULL, indicating a lock error (deadlock or killed session)")
	}
	return acquired.Bool, nil
}

// withContextCause adds the context's cancellation cause to a lock-acquisition failure
// when missing, leaving other failures unchanged.
//
// Takes failure (error) which is the acquisition's failure, or nil.
//
// Returns error which wraps the cause and failure, or is failure itself.
func withContextCause(ctx context.Context, failure error) error {
	if failure == nil || ctx.Err() == nil {
		return failure
	}
	cause := context.Cause(ctx)
	if errors.Is(failure, cause) {
		return failure
	}
	return fmt.Errorf("%w: %w", cause, failure)
}

// mysqlScopedLockName renders the SQL expression that scopes a MySQL lock key to the
// session's current schema, replacing a name longer than MySQL's 64-character limit with
// its SHA2-256 digest.
//
// Takes key (string) which is the validated lock key.
//
// Returns string which is the expression.
func mysqlScopedLockName(key string) string {
	scoped := mysqlScopedLockDescription(key)
	return fmt.Sprintf("IF(CHAR_LENGTH(%[1]s) > %[2]d, SHA2(%[1]s, 256), %[1]s)", scoped, mysqlLockNameLimit)
}

// mysqlScopedLockDescription renders the schema-scoped form of a MySQL lock key, as Names
// reports it.
//
// Takes key (string) which is the validated lock key.
//
// Returns string which is the CONCAT expression.
func mysqlScopedLockDescription(key string) string {
	return fmt.Sprintf("CONCAT(COALESCE(DATABASE(), ''), '.', '%s')", key)
}

// postgresAdvisoryLocks builds PostgreSQL's advisory migration and seed locks.
//
// Takes names (Names) which holds the lock keys.
//
// Returns lockPair which holds the two locks.
func postgresAdvisoryLocks(names Names, _ bool) lockPair {
	return lockPair{
		migration: &postgresAdvisoryLock{key: names.LockKey, label: "PostgreSQL"},
		seed:      &postgresAdvisoryLock{key: names.SeedLockKey, label: "PostgreSQL seed"},
	}
}

// postgresTableLocks builds the table-based migration and seed locks for PostgreSQL.
// CockroachDB's locks use the same lock tables; see cockroachDBTableLocks.
//
// Takes names (Names) which names the lock tables and their constraints.
//
// Returns lockPair which holds the two locks.
func postgresTableLocks(names Names, _ bool) lockPair {
	return lockPair{
		migration: newPostgresTableLock(names.MigrationLock, names.MigrationLockConstraint, "migration table"),
		seed:      newPostgresTableLock(names.SeedLock, names.SeedLockConstraint, "seed table"),
	}
}

// mysqlAdvisoryLocks builds MySQL's GET_LOCK migration and seed locks.
//
// Takes names (Names) which holds the lock keys.
// Takes serverWide (bool) which keeps the keys server-wide instead of scoping them to the
// current schema.
//
// Returns lockPair which holds the two locks.
func mysqlAdvisoryLocks(names Names, serverWide bool) lockPair {
	return lockPair{
		migration: newMySQLAdvisoryLock(names.LockKey, "MySQL", serverWide),
		seed:      newMySQLAdvisoryLock(names.SeedLockKey, "MySQL seed", serverWide),
	}
}

// noLocks builds locks that do nothing.
//
// Returns lockPair which holds two no-op locks.
func noLocks(_ Names, _ bool) lockPair {
	return lockPair{migration: &noOpLock{}, seed: &noOpLock{}}
}

// isLockNotAvailableError reports whether err indicates the lock could not be acquired.
//
// Driver-typed errors are preferred, so wrapped errors still match: a SQLSTATE of 55P03
// (lock_not_available) or MySQL error 1205 (lock wait timeout exceeded). Drivers that
// lack these methods fall back to the message forms in lockNotAvailableMessages.
//
// Takes err (error) which is the error to inspect.
//
// Returns bool which is true when the error matches a known lock-not-available pattern.
func isLockNotAvailableError(err error) bool {
	if err == nil {
		return false
	}
	if stateError, ok := errors.AsType[sqlStateReporter](err); ok && stateError.SQLState() == postgresErrorCodeLockNotAvailable {
		return true
	}
	if mysqlError, ok := errors.AsType[mysqlNumberedError](err); ok && mysqlError.Number() == mysqlErrorNumberLockWaitTimeoutUint {
		return true
	}
	message := err.Error()
	return slices.ContainsFunc(lockNotAvailableMessages, func(form string) bool {
		return strings.Contains(message, form)
	})
}

// isDuplicateObjectError reports whether err shows that a concurrent session created the
// object a CREATE ... IF NOT EXISTS was creating.
//
// Takes err (error) which is the error to inspect.
//
// Returns bool which is true for the duplicate-object SQLSTATEs or their messages.
func isDuplicateObjectError(err error) bool {
	if stateError, ok := errors.AsType[sqlStateReporter](err); ok {
		return slices.Contains(duplicateObjectSQLStates, stateError.SQLState())
	}
	message := err.Error()
	return strings.Contains(message, "already exists") ||
		strings.Contains(message, "duplicate key value violates unique constraint")
}

// commitWithin commits a table lock's transaction, cancelling the context it was begun on
// when the commit takes longer than timeout.
//
// Drivers that watch that context, as pgx does, then abandon the commit, and database/sql
// discards the session, which frees the row lock. The context is cancelled once the
// commit is done either way.
//
// Takes handle (lockHandle) which holds the lock transaction and its cancel function.
// Takes timeout (time.Duration) which bounds the commit.
//
// Returns error when the commit fails, wrapping errLockReleaseTimeout when it was cut
// short.
func commitWithin(handle lockHandle, timeout time.Duration) error {
	if handle.cancel == nil {
		return handle.transaction.Commit()
	}
	expiry := time.AfterFunc(timeout, func() { handle.cancel(errLockReleaseTimeout) })
	commitError := handle.transaction.Commit()
	if !expiry.Stop() && commitError != nil && !errors.Is(commitError, errLockReleaseTimeout) {
		commitError = fmt.Errorf("%w: %w", errLockReleaseTimeout, commitError)
	}
	handle.cancel(nil)
	return commitError
}

// confirmLockHeld runs lockCheckSQL in the transaction that holds a table lock, so a run
// whose lock session the server ended stops before its next migration or seed instead of
// running while another process may hold the lock. It does nothing for a lock held
// without a transaction.
//
// The statement runs on a context detached from ctx's cancellation and bounded by
// lockCheckTimeout, as a statement the driver cancelled part-way would end the lock
// transaction itself.
//
// Takes transaction (*sql.Tx) which is the lock transaction, or nil.
//
// Returns error which wraps ErrLockLost and the failure when the transaction has ended.
func confirmLockHeld(ctx context.Context, transaction *sql.Tx) error {
	if transaction == nil {
		return nil
	}
	checkContext, cancel := context.WithTimeoutCause(context.WithoutCancel(ctx), lockCheckTimeout, errLockCheckTimeout)
	defer cancel()
	if _, err := transaction.ExecContext(checkContext, lockCheckSQL); err != nil {
		return fmt.Errorf("%w: %w", ErrLockLost, withContextCause(checkContext, err))
	}
	return nil
}

// joinWorkCloseError joins a lock release's failure with the failure to close the
// connection a run worked on apart from the lock's session.
//
// Takes run (string) which names the run, such as "migration".
// Takes releaseError (error) which is the release's failure, or nil.
// Takes workCloseError (error) which is the work connection's close failure, or nil.
//
// Returns error which is nil when neither failed, and names the close otherwise.
func joinWorkCloseError(run string, releaseError, workCloseError error) error {
	if workCloseError == nil {
		return releaseError
	}
	return errors.Join(releaseError, fmt.Errorf("closing %s connection: %w", run, workCloseError))
}

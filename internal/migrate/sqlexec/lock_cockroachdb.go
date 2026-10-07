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
	"errors"
	"fmt"
	"strings"
)

const (
	// cockroachDBDurableLockingSQL keeps migration locks durable during schema changes.
	//
	// It prevents range changes from releasing a FOR UPDATE lock. The setting applies only
	// to the lock transaction.
	cockroachDBDurableLockingSQL = "SET LOCAL enable_durable_locking_for_serializable = true"

	// cockroachDBDurableLockingRequirement explains what a CockroachDB server older than
	// v23.2, which has no enable_durable_locking_for_serializable, lacks.
	cockroachDBDurableLockingRequirement = "the CockroachDB table lock needs " +
		"enable_durable_locking_for_serializable (CockroachDB v23.2 or later) so that a " +
		"range split cannot drop it; upgrade, or use WithLock(LockNone) and serialise " +
		"migrations outside the migrator"

	// sqlStateUndefinedObject is the SQLSTATE CockroachDB and PostgreSQL report for a SET of
	// a session variable the server does not have.
	sqlStateUndefinedObject = "42704"
)

// cockroachDBTableLock holds a durable row lock on PostgreSQL's single-row lock table, in
// a transaction that leases no descriptor but the lock table's own.
//
// An open CockroachDB transaction holds a lease on every descriptor it resolved a name
// through, the database and schema included, until it ends, and a schema change that
// bumps a descriptor's version waits for the leases on the old version. A lock
// transaction that named its table would therefore block, for the whole run, every
// migration that changes the database or schema descriptor: CREATE FUNCTION, CREATE
// SCHEMA, GRANT ... ON SCHEMA, ALTER DEFAULT PRIVILEGES and the like. So the lock
// resolves the table's descriptor ID in a short transaction of its own first, and the
// lock transaction refers to the table by that ID alone.
//
// It locks the same row as the PostgreSQL table lock, so processes running an earlier
// release, which lock the row by name, still exclude it and are excluded by it. Like the
// PostgreSQL table lock, its transaction turns off idle_in_transaction_session_timeout.
type cockroachDBTableLock struct {
	// idSQL resolves the lock table's descriptor ID on the pinned session.
	idSQL string

	// base holds the lock table, its CREATE and INSERT, and the label; its Release commits
	// the lock transaction.
	base tableLock
}

// newCockroachDBTableLock builds a CockroachDB table lock.
//
// Takes table (string) which is the lock table.
// Takes constraint (string) which is the single-row CHECK constraint.
// Takes label (string) which names the lock in error messages.
//
// Returns *cockroachDBTableLock which is the lock.
func newCockroachDBTableLock(table, constraint, label string) *cockroachDBTableLock {
	return &cockroachDBTableLock{
		idSQL: fmt.Sprintf("SELECT '%s'::REGCLASS::OID::INT8", table),
		base:  *newPostgresTableLock(table, constraint, label),
	}
}

// Acquire pins a connection, creates the lock table if needed, inserts the lock row, and
// waits for the durable row lock inside a held transaction.
//
// Takes database (*sql.DB) which is the connection pool to pin a connection from.
//
// Returns lockHandle which holds the pinned connection and the lock transaction.
// Returns error when any step of the acquisition fails.
func (lock *cockroachDBTableLock) Acquire(ctx context.Context, database *sql.DB) (lockHandle, error) {
	return lock.acquire(ctx, database, false)
}

// TryAcquire is like Acquire but does not wait for the row lock.
//
// Takes database (*sql.DB) which is the connection pool to pin a connection from.
//
// Returns lockHandle which holds the pinned connection and the lock transaction.
// Returns error which wraps migrate.ErrLockNotAcquired when another session holds the row
// lock.
func (lock *cockroachDBTableLock) TryAcquire(ctx context.Context, database *sql.DB) (lockHandle, error) {
	return lock.acquire(ctx, database, true)
}

// Release commits the lock transaction, which frees the row lock, then returns the
// session to the pool, or discards it when the commit failed.
//
// Takes handle (lockHandle) which holds the pinned connection and the lock transaction.
//
// Returns error when the commit or the close fails.
func (lock *cockroachDBTableLock) Release(ctx context.Context, handle lockHandle) error {
	return lock.base.Release(ctx, handle)
}

// acquire pins a connection and acquires a durable CockroachDB row lock. Failed sessions
// are discarded unless another session already holds the lock.
//
// Takes database (*sql.DB) which is the connection pool to pin from.
// Takes nowait (bool) which reports whether to fail rather than wait for the row lock.
//
// Returns lockHandle which holds the pinned connection and the lock transaction.
// Returns error which wraps migrate.ErrLockNotAcquired when the row is locked,
// ErrUnsupportedLock when the server has no durable locking, or the failure otherwise.
func (lock *cockroachDBTableLock) acquire(ctx context.Context, database *sql.DB, nowait bool) (lockHandle, error) {
	connection, pinError := lock.base.pinWithRow(ctx, database)
	if pinError != nil {
		return lockHandle{}, withContextCause(ctx, pinError)
	}
	var tableID int64
	if scanError := connection.QueryRowContext(ctx, lock.idSQL).Scan(&tableID); scanError != nil {
		discardConnection(connection)
		return lockHandle{}, withContextCause(ctx, fmt.Errorf("resolving the %s lock table's descriptor ID: %w", lock.base.label, scanError))
	}
	transaction, cancel, beginError := lock.base.begin(ctx, connection)
	if beginError != nil {
		return lockHandle{}, withContextCause(ctx, beginError)
	}
	if durableError := makeCockroachDBLockDurable(ctx, transaction); durableError != nil {
		_ = transaction.Rollback()
		cancel(nil)
		discardConnection(connection)
		return lockHandle{}, withContextCause(ctx, fmt.Errorf("preparing %s lock transaction: %w", lock.base.label, durableError))
	}
	handle, lockError := lock.base.lockRow(ctx, connection, transaction, cancel, cockroachDBRowLockSQL(tableID, lock.base.table, nowait))
	return handle, withContextCause(ctx, lockError)
}

// cockroachDBTableLocks builds CockroachDB's table-based migration and seed locks.
//
// Takes names (Names) which names the lock tables and their constraints.
//
// Returns lockPair which holds the two locks.
func cockroachDBTableLocks(names Names, _ bool) lockPair {
	return lockPair{
		migration: newCockroachDBTableLock(names.MigrationLock, names.MigrationLockConstraint, "migration table"),
		seed:      newCockroachDBTableLock(names.SeedLock, names.SeedLockConstraint, "seed table"),
	}
}

// cockroachDBRowLockSQL renders the row lock on a lock table referred to by its
// descriptor ID, so the transaction resolves no name and leases no database or schema.
//
// Takes tableID (int64) which is the lock table's descriptor ID.
// Takes table (string) which is the lock table's name, used as the alias.
// Takes nowait (bool) which reports whether to fail rather than wait for the lock.
//
// Returns string which is the SELECT ... FOR UPDATE statement.
func cockroachDBRowLockSQL(tableID int64, table string, nowait bool) string {
	statement := fmt.Sprintf("SELECT lock_id FROM [%d AS %s] FOR UPDATE", tableID, table)
	if nowait {
		statement += " NOWAIT"
	}
	return statement
}

// makeCockroachDBLockDurable turns on durable locking for the lock transaction, so its
// row lock survives range splits and moves.
//
// Takes transaction (*sql.Tx) which is the open lock transaction.
//
// Returns error which wraps ErrUnsupportedLock when the server is older than CockroachDB
// v23.2 and so has no durable locking, or the failure otherwise.
func makeCockroachDBLockDurable(ctx context.Context, transaction *sql.Tx) error {
	_, err := transaction.ExecContext(ctx, cockroachDBDurableLockingSQL)
	switch {
	case err == nil:
		return nil
	case isUndefinedSessionVariable(err):
		return fmt.Errorf("%w: %s: %w", ErrUnsupportedLock, cockroachDBDurableLockingRequirement, err)
	default:
		return fmt.Errorf("enabling durable locking: %w", err)
	}
}

// isUndefinedSessionVariable reports whether err shows that the server has no session
// variable of the name a SET used.
//
// Takes err (error) which is the error to inspect.
//
// Returns bool which is true for SQLSTATE 42704 or its message.
func isUndefinedSessionVariable(err error) bool {
	if stateError, ok := errors.AsType[sqlStateReporter](err); ok {
		return stateError.SQLState() == sqlStateUndefinedObject
	}
	return strings.Contains(err.Error(), "unrecognized configuration parameter")
}

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
	"log/slog"
	"time"

	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/migrate"
	"seagull.sh/seagull/internal/panicguard"
)

const (
	// leaseTerm is how long a lease lasts without renewal, after which another run may take
	// the lock from a run that stopped without releasing it.
	leaseTerm = 2 * time.Minute

	// leaseRenewal is how often a held lease is renewed, well within its term.
	leaseRenewal = 20 * time.Second

	// leasePollMinimum is the first wait between attempts to take a held lease.
	leasePollMinimum = 25 * time.Millisecond

	// leasePollMaximum caps the growing wait between attempts.
	leasePollMaximum = 500 * time.Millisecond

	// leaseOwnerBytes is how many random bytes identify one acquisition's lease.
	leaseOwnerBytes = 16

	// leaseInsertAttempts bounds the inserts one attempt makes. A lease cleared from an
	// ended run, or released between an insert and the read of its holder, is retried at
	// once rather than after a poll.
	leaseInsertAttempts = 3

	// leaseTableSuffix follows a lock table name in the name of its lease table, so a lease
	// table never collides with a row-lock table of the same namespace.
	leaseTableSuffix = "_lease"
)

// errLeaseReleased is the cause given to the renewal's context when its lease is
// released.
var errLeaseReleased = errors.New("the lease was released")

// leaseLock serialises runs on engines without session locks.
//
// A row with the lease owner and expiry holds the lock. The lease renews during a run. An
// expired lease can be taken over, as can one held by a process that ended in the same
// machine and PID namespace.
type leaseLock struct {
	// placeholder renders the engine's bind parameters.
	placeholder func(int) string

	// table is the validated lock table name.
	table string

	// label names the lock in errors and log records.
	label string

	// createSQL creates the lease table.
	createSQL string

	// expireSQL deletes an expired lease, given the time now in Unix milliseconds.
	expireSQL string

	// insertSQL takes the lease, given its owner and expiry.
	insertSQL string

	// holderSQL reads the owner of the held lease.
	holderSQL string

	// renewSQL extends a lease, given its new expiry and its owner.
	renewSQL string

	// deleteSQL deletes a lease, given its owner.
	deleteSQL string
}

// newLeaseLock returns a lease lock on a table.
//
// Takes table (string) which is the validated lock table name.
// Takes label (string) which names the lock in errors.
// Takes placeholder (func(int) string) which renders bind parameters.
//
// Returns *leaseLock which is the lock.
func newLeaseLock(table, label string, placeholder func(int) string) *leaseLock {
	return &leaseLock{
		placeholder: placeholder,
		table:       table,
		label:       label,
		createSQL:   "CREATE TABLE IF NOT EXISTS " + table + " (id INTEGER PRIMARY KEY, owner VARCHAR(64) NOT NULL, expires_at BIGINT NOT NULL)",
		expireSQL:   "DELETE FROM " + table + " WHERE id = 1 AND expires_at < " + placeholder(1),
		insertSQL:   "INSERT INTO " + table + " (id, owner, expires_at) VALUES (1, " + placeholder(1) + ", " + placeholder(2) + ")",
		holderSQL:   "SELECT owner FROM " + table + " WHERE id = 1",
		renewSQL:    "UPDATE " + table + " SET expires_at = " + placeholder(1) + " WHERE id = 1 AND owner = " + placeholder(2),
		deleteSQL:   "DELETE FROM " + table + " WHERE id = 1 AND owner = " + placeholder(1),
	}
}

// Acquire takes the lease, waiting while another run holds an unexpired one.
//
// Takes database (*sql.DB) which is the pool to lock through.
//
// Returns lockHandle which holds the owner and the function that stops the renewal.
// Returns error when the table cannot be used or ctx ends first, carrying ctx's cause.
func (lock *leaseLock) Acquire(ctx context.Context, database *sql.DB) (lockHandle, error) {
	wait := leasePollMinimum
	for {
		handle, taken, err := lock.attempt(ctx, database)
		if err != nil || taken {
			return handle, withContextCause(ctx, err)
		}
		if !waitFor(ctx, wait) {
			return lockHandle{}, fmt.Errorf("waiting for the %s lease: %w", lock.label, context.Cause(ctx))
		}
		wait = min(wait*2, leasePollMaximum)
	}
}

// TryAcquire takes the lease only when no other run holds an unexpired one.
//
// Takes database (*sql.DB) which is the pool to lock through.
//
// Returns lockHandle which holds the owner and the function that stops the renewal.
// Returns error which wraps migrate.ErrLockNotAcquired when another run holds the lease.
func (lock *leaseLock) TryAcquire(ctx context.Context, database *sql.DB) (lockHandle, error) {
	handle, taken, err := lock.attempt(ctx, database)
	if err != nil {
		return lockHandle{}, withContextCause(ctx, err)
	}
	if !taken {
		return lockHandle{}, fmt.Errorf("%s lease: %w", lock.label, migrate.ErrLockNotAcquired)
	}
	return handle, nil
}

// Release stops renewing the lease, waits for the renewal to end, and deletes the lease
// if it is still this run's. The delete runs on a context detached from ctx's
// cancellation, so a cancelled run still frees the lock rather than holding it until the
// lease expires.
//
// Takes handle (lockHandle) which is what the acquisition returned.
//
// Returns error when the lease cannot be deleted.
func (lock *leaseLock) Release(ctx context.Context, handle lockHandle) error {
	if handle.cancel != nil {
		handle.cancel(errLeaseReleased)
	}
	if handle.database == nil {
		return nil
	}
	releaseContext, cancel := detachContextForRelease(ctx)
	defer cancel()
	if _, err := handle.database.ExecContext(releaseContext, lock.deleteSQL, handle.name); err != nil {
		return fmt.Errorf("releasing the %s lease: %w", lock.label, withContextCause(releaseContext, err))
	}
	return nil
}

// attempt makes one attempt to take the lease. It removes an expired lease, then inserts
// its own, which the table's key admits only when no lease is held.
//
// Takes database (*sql.DB) which is the pool to lock through.
//
// Returns lockHandle which holds the lease when taken.
// Returns bool which is true when the lease was taken.
// Returns error when the table cannot be created, read or written.
func (lock *leaseLock) attempt(ctx context.Context, database *sql.DB) (lockHandle, bool, error) {
	if err := ensureTable(ctx, database, lock.table, lock.createSQL); err != nil {
		return lockHandle{}, false, fmt.Errorf("creating the %s lease table: %w", lock.label, err)
	}
	if _, err := database.ExecContext(ctx, lock.expireSQL, time.Now().UnixMilli()); err != nil {
		return lockHandle{}, false, fmt.Errorf("clearing an expired %s lease: %w", lock.label, err)
	}
	owner, err := leaseOwner()
	if err != nil {
		return lockHandle{}, false, err
	}
	for attempt := range leaseInsertAttempts {
		holder, insertError := lock.insert(ctx, database, owner)
		switch {
		case insertError == nil && holder == "":
			return lock.startRenewal(ctx, database, owner), true, nil
		case errors.Is(insertError, sql.ErrNoRows) && attempt+1 < leaseInsertAttempts:
			continue
		case insertError != nil:
			return lockHandle{}, false, insertError
		}
		cleared, clearError := lock.clearEndedHolder(ctx, database, holder)
		if clearError != nil || !cleared {
			return lockHandle{}, false, clearError
		}
	}
	return lockHandle{}, false, nil
}

// insert inserts this run's lease and, when the insert fails, reads who holds the lease.
//
// Takes database (*sql.DB) which is the pool to lock through.
// Takes owner (string) which identifies this run's lease.
//
// Returns string which is empty when the lease was taken, and the holder's owner when
// another run holds it.
// Returns error when the insert failed and no other holder can be read. It wraps
// sql.ErrNoRows when the lease was free by the time it was read.
func (lock *leaseLock) insert(ctx context.Context, database *sql.DB, owner string) (string, error) {
	_, insertError := database.ExecContext(ctx, lock.insertSQL, owner, time.Now().Add(leaseTerm).UnixMilli())
	if insertError == nil {
		return "", nil
	}
	var holder string
	readError := database.QueryRowContext(ctx, lock.holderSQL).Scan(&holder)
	if readError == nil && holder != "" && holder != owner {
		return holder, nil
	}
	return "", fmt.Errorf("taking the %s lease: %w", lock.label, errors.Join(insertError, readError))
}

// clearEndedHolder deletes a lease whose holder is known to have ended, such as a run
// killed on this machine, so the next insert takes it rather than waiting out its term.
// The delete names the holder, so a lease another run took meanwhile stays.
//
// Takes database (*sql.DB) which is the pool to lock through.
// Takes holder (string) which is the lease's owner.
//
// Returns bool which is true when the lease was cleared.
// Returns error when the lease cannot be deleted.
func (lock *leaseLock) clearEndedHolder(ctx context.Context, database *sql.DB, holder string) (bool, error) {
	if !leaseHolderGone(holder) {
		return false, nil
	}
	if _, err := database.ExecContext(ctx, lock.deleteSQL, holder); err != nil {
		return false, fmt.Errorf("clearing the %s lease of an ended run: %w", lock.label, err)
	}
	return true, nil
}

// startRenewal starts the goroutine that renews a taken lease, and returns the handle
// whose cancel function stops it and waits for it to end.
//
// Takes database (*sql.DB) which is the pool the lease was taken through.
// Takes owner (string) which identifies this run's lease.
//
// Returns lockHandle which holds the lease.
func (lock *leaseLock) startRenewal(ctx context.Context, database *sql.DB, owner string) lockHandle {
	renewal, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	done := make(chan struct{})
	go func() {
		defer close(done)
		lock.renew(renewal, database, owner)
	}()
	stop := func(cause error) {
		cancel(cause)
		<-done
	}
	return lockHandle{connection: nil, transaction: nil, database: database, cancel: stop, name: owner, discard: false}
}

// renew extends a held lease until the context ends.
//
// Failed renewals are logged and retried at the next tick. Renewal stops if another run
// has taken the lease.
//
// Takes database (*sql.DB) which is the pool the lease was taken through.
// Takes owner (string) which identifies this run's lease.
func (lock *leaseLock) renew(ctx context.Context, database *sql.DB, owner string) {
	ctx, logger := logging.From(ctx)
	ticker := time.NewTicker(leaseRenewal)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		held, err := lock.extend(ctx, database, owner)
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			logger.WarnContext(ctx, "renewing the lease failed, so it is tried again at the next renewal",
				slog.String("lock", lock.label), logging.ErrAttr(err))
		case !held:
			logger.ErrorContext(ctx, "the lease was lost, so another run may hold the lock", slog.String("lock", lock.label))
			return
		}
	}
}

// extend runs one renewal of a lease, turning a panic in the driver into an error.
//
// Takes database (*sql.DB) which is the pool the lease was taken through.
// Takes owner (string) which identifies this run's lease.
//
// Returns bool which is false only when the renewal found no lease of this owner. A
// driver that cannot count the rows changed leaves the lease taken as held.
// Returns error when the renewal fails.
func (lock *leaseLock) extend(ctx context.Context, database *sql.DB, owner string) (bool, error) {
	return panicguard.RunValue(ctx, "renewing the "+lock.label+" lease", func() (bool, error) {
		result, err := database.ExecContext(ctx, lock.renewSQL, time.Now().Add(leaseTerm).UnixMilli(), owner)
		if err != nil {
			return true, err
		}
		if updated, countError := result.RowsAffected(); countError == nil {
			return updated > 0, nil
		}
		return true, nil
	})
}

// leaseLocks builds the migration and seed lease locks of an engine with no session
// locks.
//
// Takes placeholder (func(int) string) which renders bind parameters.
//
// Returns lockFactory which builds the pair for a backend's names.
func leaseLocks(placeholder func(int) string) lockFactory {
	return func(names Names, _ bool) lockPair {
		return lockPair{
			migration: newLeaseLock(names.MigrationLease, "migration", placeholder),
			seed:      newLeaseLock(names.SeedLease, "seed", placeholder),
		}
	}
}

// waitFor waits for a duration or until ctx ends, whichever comes first.
//
// Takes wait (time.Duration) which is how long to wait.
//
// Returns bool which is true when the wait elapsed and false when ctx ended first.
func waitFor(ctx context.Context, wait time.Duration) bool {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

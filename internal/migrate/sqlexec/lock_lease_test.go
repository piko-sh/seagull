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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/migrate"
)

var errLeaseRowTaken = errors.New("UNIQUE constraint failed: lease.id")

type leaseTable struct {
	owner         string
	mu            sync.Mutex
	expiresAt     int64
	created       bool
	held          bool
	refuseInserts bool
}

type leaseConnector struct{ table *leaseTable }

type leaseConn struct{ table *leaseTable }

type leaseRows struct {
	values []string
	cursor int
}

type leaseResult int64

func (connector leaseConnector) Connect(context.Context) (driver.Conn, error) {
	return leaseConn(connector), nil
}

func (leaseConnector) Driver() driver.Driver { return nil }

func (leaseConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }

func (leaseConn) Close() error { return nil }

func (leaseConn) Begin() (driver.Tx, error) { return nil, driver.ErrSkip }

func (conn leaseConn) ExecContext(_ context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
	table := conn.table
	table.mu.Lock()
	defer table.mu.Unlock()
	millis := func(index int) int64 { return namedInt64(arguments, index) }
	text := func(index int) string { return namedString(arguments, index) }
	switch {
	case strings.HasPrefix(query, "CREATE TABLE"):
		table.created = true
		return leaseResult(0), nil
	case strings.Contains(query, "expires_at <"):
		if table.held && table.expiresAt < millis(0) {
			table.held = false
			return leaseResult(1), nil
		}
		return leaseResult(0), nil
	case strings.HasPrefix(query, "INSERT"):
		if table.held || table.refuseInserts {
			return nil, errLeaseRowTaken
		}
		table.held, table.owner, table.expiresAt = true, text(0), millis(1)
		return leaseResult(1), nil
	case strings.HasPrefix(query, "UPDATE"):
		if !table.held || table.owner != text(1) {
			return leaseResult(0), nil
		}
		table.expiresAt = millis(0)
		return leaseResult(1), nil
	case strings.HasPrefix(query, "DELETE"):
		if !table.held || table.owner != text(0) {
			return leaseResult(0), nil
		}
		table.held = false
		return leaseResult(1), nil
	}
	return nil, errors.New("unexpected statement: " + query)
}

func (conn leaseConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	table := conn.table
	table.mu.Lock()
	defer table.mu.Unlock()
	switch {
	case strings.HasPrefix(query, "SELECT 1"):
		if !table.created {
			return nil, errors.New("no such table")
		}
		return &leaseRows{}, nil
	case strings.HasPrefix(query, "SELECT owner"):
		if !table.held {
			return &leaseRows{}, nil
		}
		return &leaseRows{values: []string{table.owner}}, nil
	}
	return nil, errors.New("unexpected query: " + query)
}

func (*leaseRows) Columns() []string { return []string{"owner"} }

func (*leaseRows) Close() error { return nil }

func (rows *leaseRows) Next(destination []driver.Value) error {
	if rows.cursor >= len(rows.values) {
		return io.EOF
	}
	destination[0] = rows.values[rows.cursor]
	rows.cursor++
	return nil
}

func (result leaseResult) RowsAffected() (int64, error) { return int64(result), nil }

func (leaseResult) LastInsertId() (int64, error) { return 0, nil }

func (table *leaseTable) state() (owner string, held bool) {
	table.mu.Lock()
	defer table.mu.Unlock()
	return table.owner, table.held
}

func openLeaseDatabase(t *testing.T) (*sql.DB, *leaseTable) {
	t.Helper()
	table := &leaseTable{}
	database := sql.OpenDB(leaseConnector{table: table})
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	return database, table
}

func TestLeaseLockSerialisesRunsAndReleases(t *testing.T) {
	t.Parallel()

	database, table := openLeaseDatabase(t)
	lock := newLeaseLock("sgl_migration_lock_lease", "migration", questionPlaceholder)

	handle, err := lock.Acquire(t.Context(), database)
	require.NoError(t, err)
	owner, held := table.state()
	require.True(t, held)
	require.Equal(t, handle.name, owner)

	_, err = lock.TryAcquire(t.Context(), database)
	require.ErrorIs(t, err, migrate.ErrLockNotAcquired)

	require.NoError(t, lock.Release(t.Context(), handle))
	_, held = table.state()
	require.False(t, held, "a released lease frees the lock")

	other, err := lock.TryAcquire(t.Context(), database)
	require.NoError(t, err)
	require.NoError(t, lock.Release(t.Context(), other))
}

func TestLeaseLockReleaseOnCancelledContextStillDeletes(t *testing.T) {
	t.Parallel()

	database, table := openLeaseDatabase(t)
	lock := newLeaseLock("sgl_seed_lock_lease", "seed", questionPlaceholder)
	handle, err := lock.Acquire(t.Context(), database)
	require.NoError(t, err)

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	require.NoError(t, lock.Release(cancelled, handle))
	_, held := table.state()
	require.False(t, held, "a cancelled run still frees its lease")
}

func TestLeaseLockTakesOverAnExpiredLease(t *testing.T) {
	t.Parallel()

	database, table := openLeaseDatabase(t)
	table.created, table.held, table.owner, table.expiresAt = true, true, "stale", time.Now().Add(-time.Minute).UnixMilli()
	lock := newLeaseLock("sgl_migration_lock_lease", "migration", questionPlaceholder)

	handle, err := lock.TryAcquire(t.Context(), database)
	require.NoError(t, err)
	owner, held := table.state()
	require.True(t, held)
	require.NotEqual(t, "stale", owner)
	require.NoError(t, lock.Release(t.Context(), handle))
}

func TestLeaseLockReportsAnInsertThatFailsWithNoHolder(t *testing.T) {
	t.Parallel()

	database, table := openLeaseDatabase(t)
	table.refuseInserts = true
	lock := newLeaseLock("sgl_migration_lock_lease", "migration", questionPlaceholder)

	_, err := lock.Acquire(t.Context(), database)
	require.ErrorIs(t, err, errLeaseRowTaken, "an insert that fails with no lease held is an error, not a wait")
	require.NotErrorIs(t, err, migrate.ErrLockNotAcquired)
}

func TestLeaseLockAcquireStopsWhenContextEnds(t *testing.T) {
	t.Parallel()

	database, table := openLeaseDatabase(t)
	table.created, table.held, table.owner, table.expiresAt = true, true, "other", time.Now().Add(time.Hour).UnixMilli()
	lock := newLeaseLock("sgl_migration_lock_lease", "migration", questionPlaceholder)

	cause := errors.New("the run was stopped")
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(cause)
	_, err := lock.Acquire(ctx, database)
	require.ErrorIs(t, err, cause)
}

func TestLeaseLockExtendReportsALostLease(t *testing.T) {
	t.Parallel()

	database, table := openLeaseDatabase(t)
	lock := newLeaseLock("sgl_migration_lock_lease", "migration", questionPlaceholder)
	handle, err := lock.Acquire(t.Context(), database)
	require.NoError(t, err)

	held, err := lock.extend(t.Context(), database, handle.name)
	require.NoError(t, err)
	require.True(t, held)

	table.mu.Lock()
	table.owner = "another run"
	table.mu.Unlock()
	held, err = lock.extend(t.Context(), database, handle.name)
	require.NoError(t, err)
	require.False(t, held, "a lease another run holds is no longer this run's")
	require.NoError(t, lock.Release(t.Context(), handle))
}

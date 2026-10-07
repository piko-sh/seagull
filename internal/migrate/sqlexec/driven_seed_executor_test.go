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
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/migrate"
)

type fakeSeedStore struct {
	applied           map[int64]struct{}
	lockChannel       chan struct{}
	mu                sync.Mutex
	seedSQLExecutions atomic.Int32
	insertConflicts   atomic.Int32
	lockHolder        atomic.Int32
	peakLockHolders   atomic.Int32
	lockAcquisitions  atomic.Int32
	lockReleases      atomic.Int32
}

func newFakeSeedStore() *fakeSeedStore {
	store := &fakeSeedStore{
		applied:     make(map[int64]struct{}),
		lockChannel: make(chan struct{}, 1),
	}
	store.lockChannel <- struct{}{}
	return store
}

type fakeSeedConnector struct {
	store *fakeSeedStore
}

type fakeSeedDriver struct {
	store *fakeSeedStore
}

type fakeSeedConn struct {
	store *fakeSeedStore
}

type fakeSeedTx struct {
	connection *fakeSeedConn
}

type emptyRows struct{}

type seedRowsConnector struct {
	rows [][]driver.Value
}

type seedRowsDriver struct {
	rows [][]driver.Value
}

type seedRowsConn struct {
	rows [][]driver.Value
}

type seedRows struct {
	rows  [][]driver.Value
	index int
}

func (c *fakeSeedConnector) Connect(_ context.Context) (driver.Conn, error) {
	return &fakeSeedConn{store: c.store}, nil
}

func (c *fakeSeedConnector) Driver() driver.Driver {
	return &fakeSeedDriver{store: c.store}
}

func (d *fakeSeedDriver) Open(_ string) (driver.Conn, error) {
	return &fakeSeedConn{store: d.store}, nil
}

func (*fakeSeedConn) Prepare(_ string) (driver.Stmt, error) {
	return nil, errors.New("Prepare not supported by fakeSeedConn")
}

func (*fakeSeedConn) Close() error { return nil }

func (c *fakeSeedConn) Begin() (driver.Tx, error) {
	return &fakeSeedTx{connection: c}, nil
}

func (c *fakeSeedConn) BeginTx(_ context.Context, _ driver.TxOptions) (driver.Tx, error) {
	return &fakeSeedTx{connection: c}, nil
}

func (c *fakeSeedConn) ExecContext(_ context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
	return c.store.exec(query, arguments)
}

func (c *fakeSeedConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if query == postgresEngine().currentSchemaSQL {
		return &singleValueRows{value: nil}, nil
	}
	return &emptyRows{}, nil
}

func (*fakeSeedTx) Commit() error   { return nil }
func (*fakeSeedTx) Rollback() error { return nil }

func (*emptyRows) Columns() []string {
	return []string{"version", "name", "checksum", "applied_at", "duration_ms"}
}
func (*emptyRows) Close() error                { return nil }
func (*emptyRows) Next(_ []driver.Value) error { return io.EOF }

func (s *fakeSeedStore) exec(query string, arguments []driver.NamedValue) (driver.Result, error) {
	switch {
	case strings.Contains(query, "CREATE TABLE IF NOT EXISTS sgl_seeds"):
		return driver.RowsAffected(0), nil
	case query == postgresAdvisoryLockSQL && namedString(arguments, 0) == "sgl_seeds":
		<-s.lockChannel
		holders := s.lockHolder.Add(1)
		if holders > s.peakLockHolders.Load() {
			s.peakLockHolders.Store(holders)
		}
		s.lockAcquisitions.Add(1)
		return driver.RowsAffected(0), nil
	case query == postgresAdvisoryUnlockSQL && namedString(arguments, 0) == "sgl_seeds":
		s.lockHolder.Add(-1)
		s.lockReleases.Add(1)
		s.lockChannel <- struct{}{}
		return driver.RowsAffected(0), nil
	case strings.Contains(query, "INSERT") && strings.Contains(query, "sgl_seeds"):
		return s.recordSeed(query, arguments)
	case strings.Contains(query, "DELETE FROM sgl_seeds"):
		s.mu.Lock()
		s.applied = make(map[int64]struct{})
		s.mu.Unlock()
		return driver.RowsAffected(0), nil
	case strings.HasPrefix(strings.TrimSpace(query), "/* seed body"):
		s.seedSQLExecutions.Add(1)
		return driver.RowsAffected(0), nil
	default:
		return nil, fmt.Errorf("fakeSeedStore: unhandled SQL %q", query)
	}
}

func (s *fakeSeedStore) recordSeed(query string, arguments []driver.NamedValue) (driver.Result, error) {
	if len(arguments) == 0 {
		return nil, errors.New("fakeSeedStore: INSERT missing arguments")
	}
	version, ok := arguments[0].Value.(int64)
	if !ok {
		return nil, fmt.Errorf("fakeSeedStore: INSERT version arg expected int64, got %T", arguments[0].Value)
	}

	idempotent := strings.Contains(query, "ON CONFLICT") || strings.Contains(query, "INSERT IGNORE")

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.applied[version]; exists {
		if idempotent {
			s.insertConflicts.Add(1)
			return driver.RowsAffected(0), nil
		}
		return nil, fmt.Errorf("fakeSeedStore: duplicate key value violates unique constraint (version=%d)", version)
	}
	s.applied[version] = struct{}{}
	return driver.RowsAffected(1), nil
}

func (c *seedRowsConnector) Connect(context.Context) (driver.Conn, error) {
	return &seedRowsConn{rows: c.rows}, nil
}

func (c *seedRowsConnector) Driver() driver.Driver { return &seedRowsDriver{rows: c.rows} }

func (d *seedRowsDriver) Open(string) (driver.Conn, error) { return &seedRowsConn{rows: d.rows}, nil }

func (*seedRowsConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare is not supported")
}

func (*seedRowsConn) Close() error { return nil }

func (*seedRowsConn) Begin() (driver.Tx, error) { return nil, errors.New("begin is not supported") }

func (c *seedRowsConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &seedRows{rows: c.rows}, nil
}

func (*seedRows) Columns() []string {
	return []string{"version", "name", "checksum", "applied_at", "duration_ms"}
}

func (*seedRows) Close() error { return nil }

func (r *seedRows) Next(dest []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.index])
	r.index++
	return nil
}

func openFakeSeedDB(store *fakeSeedStore) *sql.DB {
	return sql.OpenDB(&fakeSeedConnector{store: store})
}

func TestSeedExecutor_ExecuteSeed_UsesIdempotentInsertForPostgresDialect(t *testing.T) {
	t.Parallel()

	store := newFakeSeedStore()
	store.applied[42] = struct{}{}

	database := openFakeSeedDB(store)
	t.Cleanup(func() { _ = database.Close() })

	executor := newSeedExecutor(database, Postgres())

	err := executor.ExecuteSeed(t.Context(), migrate.SeedRecord{
		Version:  42,
		Name:     "users",
		Checksum: "abc",
		Content:  []byte("/* seed body 42 */"),
	})

	require.NoError(t, err, "second writer must not surface a primary-key violation when the dialect uses ON CONFLICT")
	require.Equal(t, int32(1), store.insertConflicts.Load(), "the conflict path should have absorbed the duplicate INSERT")
}

func TestSeedExecutor_ExecuteSeed_UsesInsertIgnoreForMySQLDialect(t *testing.T) {
	t.Parallel()

	store := newFakeSeedStore()
	store.applied[7] = struct{}{}

	database := openFakeSeedDB(store)
	t.Cleanup(func() { _ = database.Close() })

	executor := newSeedExecutor(database, MySQL())

	err := executor.ExecuteSeed(t.Context(), migrate.SeedRecord{
		Version:  7,
		Name:     "products",
		Checksum: "xyz",
		Content:  []byte("/* seed body 7 */"),
	})

	require.NoError(t, err)
	require.Equal(t, int32(1), store.insertConflicts.Load(), "MySQL dialect should hit INSERT IGNORE on duplicates")
}

func TestSeedExecutor_ConcurrentApplyIsIdempotent(t *testing.T) {
	t.Parallel()

	store := newFakeSeedStore()

	database := openFakeSeedDB(store)
	t.Cleanup(func() { _ = database.Close() })

	const replicas = 4
	startBarrier := make(chan struct{})
	results := make(chan error, replicas)

	for range replicas {
		executor := newSeedExecutor(database, Postgres())
		go func() {
			<-startBarrier
			results <- executor.ExecuteSeed(t.Context(), migrate.SeedRecord{
				Version:  100,
				Name:     "concurrent",
				Checksum: "deadbeef",
				Content:  []byte("/* seed body 100 */"),
			})
		}()
	}

	close(startBarrier)
	for range replicas {
		require.NoError(t, <-results, "every concurrent writer must succeed without surfacing a duplicate-key violation")
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	require.Len(t, store.applied, 1, "exactly one sgl_seeds row must exist for the version")
}

func TestSeedExecutor_SQLiteSeedLockNeedsNoDatabase(t *testing.T) {
	t.Parallel()

	dialect := SQLite()
	executor := newSeedExecutor(nil, dialect)

	err := executor.AcquireSeedLock(t.Context())
	require.NoError(t, err, "no-op SQLite lock must succeed without a database")

	err = executor.ReleaseSeedLock(t.Context())
	require.NoError(t, err, "no-op SQLite lock release must succeed")
}

func TestSeedExecutor_SecondAcquireWaitsForTheFirst(t *testing.T) {
	t.Parallel()

	store := newFakeSeedStore()
	database := openFakeSeedDB(store)
	t.Cleanup(func() { _ = database.Close() })

	executor := newSeedExecutor(database, Postgres())

	require.NoError(t, executor.AcquireSeedLock(t.Context()), "first acquire should succeed")

	cause := errors.New("deploy window closed")
	ctx, cancel := context.WithTimeoutCause(t.Context(), 10*time.Millisecond, cause)
	defer cancel()
	require.ErrorIs(t, executor.AcquireSeedLock(ctx), cause, "a second acquire must wait for the first holder")

	require.NoError(t, executor.ReleaseSeedLock(t.Context()))
	require.NoError(t, executor.AcquireSeedLock(t.Context()))
	require.NoError(t, executor.ReleaseSeedLock(t.Context()))
	require.Equal(t, int32(2), store.lockAcquisitions.Load())
}

func TestSeedExecutor_AdvisoryLockSerialisesConcurrentRuns(t *testing.T) {
	t.Parallel()

	store := newFakeSeedStore()

	database := openFakeSeedDB(store)
	t.Cleanup(func() { _ = database.Close() })

	const concurrentRuns = 8
	startBarrier := make(chan struct{})
	done := make(chan error, concurrentRuns)

	for runIndex := range concurrentRuns {
		executor := newSeedExecutor(database, Postgres())
		go func() {
			<-startBarrier
			if acquireError := executor.AcquireSeedLock(t.Context()); acquireError != nil {
				done <- acquireError
				return
			}
			seedError := executor.ExecuteSeed(t.Context(), migrate.SeedRecord{
				Version:  int64(200 + runIndex),
				Name:     fmt.Sprintf("seed_%d", runIndex),
				Checksum: "chk",
				Content:  []byte("/* seed body N */"),
			})

			releaseError := executor.ReleaseSeedLock(t.Context())
			done <- errors.Join(seedError, releaseError)
		}()
	}

	close(startBarrier)
	for range concurrentRuns {
		require.NoError(t, <-done)
	}

	require.Equal(t, int32(1), store.peakLockHolders.Load(),
		"advisory lock must serialise replicas; peak concurrent holders should never exceed one")
	require.Equal(t, int32(concurrentRuns), store.lockAcquisitions.Load(),
		"every replica should have acquired the lock once")
	require.Equal(t, int32(concurrentRuns), store.lockReleases.Load(),
		"every replica should have released the lock once")
}

func TestSeedExecutorLeavesTheSeedLockLabelToTheSeeder(t *testing.T) {
	t.Parallel()

	memory := newMemoryDatabase()
	first, second := memory.open(), memory.open()
	t.Cleanup(func() { _ = first.Close(); _ = second.Close() })
	holder, err := NewSeedExecutor(first, Postgres())
	require.NoError(t, err)
	require.NoError(t, holder.TryAcquireSeedLock(t.Context()))
	t.Cleanup(func() { _ = holder.ReleaseSeedLock(context.Background()) })
	contender, err := NewSeedExecutor(second, Postgres())
	require.NoError(t, err)
	seeder, err := migrate.NewSeeder(contender, fstest.MapFS{}, "seeds", migrate.WithNonBlockingLock())
	require.NoError(t, err)

	tests := []struct {
		call      func() error
		name      string
		wantLabel string
	}{
		{name: "the executor alone", call: func() error { return contender.TryAcquireSeedLock(t.Context()) }},
		{name: "through the seeder", call: func() error {
			_, applyError := seeder.Apply(t.Context())
			return applyError
		}, wantLabel: "acquiring seed lock: "},
	}
	for _, testCase := range tests {
		err := testCase.call()
		require.ErrorIs(t, err, migrate.ErrLockNotAcquired, testCase.name)
		require.Equal(t, strings.Count(testCase.wantLabel, "seed lock"), strings.Count(err.Error(), "seed lock"), "%s: %v", testCase.name, err)
		require.True(t, strings.HasPrefix(err.Error(), testCase.wantLabel), "%s: %v", testCase.name, err)
	}
}

func TestSeedExecutorReleaseFailureCarriesNoSeedLockLabel(t *testing.T) {
	t.Parallel()

	memory := newMemoryDatabase()
	memory.failUnlock = true
	database := memory.open()
	t.Cleanup(func() { _ = database.Close() })
	executor, err := NewSeedExecutor(database, Postgres())
	require.NoError(t, err)
	require.NoError(t, executor.AcquireSeedLock(t.Context()))

	err = executor.ReleaseSeedLock(t.Context())

	require.ErrorContains(t, err, "releasing PostgreSQL seed advisory lock: unlock timed out")
	require.NotContains(t, err.Error(), "releasing seed lock", "the Seeder names the step, so the executor must not")
}

func TestSeedLoopReportsTheCauseOfACancellation(t *testing.T) {
	t.Parallel()

	cause := errors.New("deploy window closed")
	ctx, cancel := context.WithCancelCause(t.Context())
	t.Cleanup(func() { cancel(nil) })
	store := &cancelStore{versions: map[int64]bool{}, cancel: func() { cancel(cause) }, cancelAfterStatement: "STMT_0 "}
	database := sql.OpenDB(&cancelConnector{store: store})
	t.Cleanup(func() { _ = database.Close() })
	executor, err := NewSeedExecutor(database, MySQL(WithLock(LockNone)))
	require.NoError(t, err)

	err = executor.ExecuteSeed(ctx, migrate.SeedRecord{Name: "rows", Checksum: "c", Content: buildStatements(3), Version: 1})

	require.ErrorIs(t, err, cause)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorContains(t, err, "seed cancelled before statement 2: "+context.Canceled.Error()+": deploy window closed")
}

func TestSeedTransactionRollbackFailureIsJoinedIntoTheResult(t *testing.T) {
	t.Parallel()

	errStatement := errors.New("duplicate entry")
	errRollback := errors.New("connection reset by peer")
	tests := []struct {
		name         string
		failRollback error
		wantRollback bool
	}{
		{name: "a failed rollback is joined", failRollback: errRollback, wantRollback: true},
		{name: "a clean rollback leaves the statement failure alone", failRollback: nil, wantRollback: false},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			store := newProgressStore()
			store.failOn("STMT_1 b", errStatement)
			store.failRollbacks(testCase.failRollback)
			database := store.open()
			t.Cleanup(func() { _ = database.Close() })
			executor, err := NewSeedExecutor(database, MySQL(WithLock(LockNone)))
			require.NoError(t, err)

			err = executor.ExecuteSeed(t.Context(), migrate.SeedRecord{Name: "rows", Checksum: "c", Content: []byte("STMT_0 a; STMT_1 b;"), Version: 1})

			require.ErrorIs(t, err, errStatement)
			require.Equal(t, testCase.wantRollback, errors.Is(err, errRollback))
			if testCase.wantRollback {
				require.ErrorContains(t, err, "rolling back seed transaction: connection reset by peer")
			}
		})
	}
}

func TestAppliedSeedsParsesEveryAppliedAtRepresentation(t *testing.T) {
	t.Parallel()

	native := time.Date(2026, 10, 6, 12, 30, 0, 0, time.UTC)
	database := sql.OpenDB(&seedRowsConnector{rows: [][]driver.Value{
		{int64(1), "text", "c1", "2026-10-06 12:30:00", int64(3)},
		{int64(2), "bytes", "c2", []byte("2026-10-06T12:30:00Z"), int64(4)},
		{int64(3), "native", "c3", native, int64(5)},
		{int64(4), "missing", "c4", nil, int64(6)},
	}})
	t.Cleanup(func() { _ = database.Close() })

	executor, err := NewSeedExecutor(database, SQLite())
	require.NoError(t, err)

	seeds, err := executor.AppliedSeeds(t.Context())
	require.NoError(t, err)
	require.Len(t, seeds, 4)
	assert.True(t, seeds[0].AppliedAt.Equal(native))
	assert.True(t, seeds[1].AppliedAt.Equal(native))
	assert.True(t, seeds[2].AppliedAt.Equal(native))
	assert.True(t, seeds[3].AppliedAt.IsZero())
	assert.Equal(t, "text", seeds[0].Name)
	assert.Equal(t, int64(6), seeds[3].DurationMs)
}

func TestAppliedSeedsWarnsOnceForEveryAppliedAtItCannotRead(t *testing.T) {
	t.Parallel()

	database := sql.OpenDB(&seedRowsConnector{rows: [][]driver.Value{
		{int64(1), "text", "c1", "2026-10-06 12:30:00", int64(3)},
		{int64(2), "garbled", "c2", "yesterday", int64(4)},
		{int64(3), "missing", "c3", nil, int64(5)},
	}})
	t.Cleanup(func() { _ = database.Close() })
	executor, err := NewSeedExecutor(database, SQLite())
	require.NoError(t, err)
	var logs bytes.Buffer
	ctx := logging.ContextWithLogger(t.Context(), slog.New(slog.NewJSONHandler(&logs, nil)))

	seeds, err := executor.AppliedSeeds(ctx)

	require.NoError(t, err)
	require.Len(t, seeds, 3)
	records := warnings(t, &logs)
	require.Len(t, records, 1, "one read warns once, however many rows it could not read")
	assert.Equal(t, "sgl_seeds", records[0]["table"])
	assert.InDelta(t, 2, records[0]["rows"], 0)
	assert.InDelta(t, 2, records[0]["first_version"], 0)
}

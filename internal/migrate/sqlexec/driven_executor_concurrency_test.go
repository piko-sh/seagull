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
	"errors"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/migrate"
)

var threeMigrations = fstest.MapFS{
	"migrations/0001_users.up.sql":    {Data: []byte("CREATE TABLE users (id BIGINT);\n")},
	"migrations/0002_posts.up.sql":    {Data: []byte("CREATE TABLE posts (id BIGINT);\n")},
	"migrations/0003_comments.up.sql": {Data: []byte("CREATE TABLE comments (id BIGINT);\n")},
}

func TestExecutorsFromOneBackendLockIndependently(t *testing.T) {
	t.Parallel()

	for name, backend := range map[string]Backend{
		"advisory": Postgres(),
		"table":    PostgresPgBouncer(),
		"seed":     CockroachDB(),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			const executors = 4
			results := make(chan error, executors)
			for range executors {
				database := newMemoryDatabase().open()
				t.Cleanup(func() { _ = database.Close() })
				executor, err := NewExecutor(database, backend)
				require.NoError(t, err)
				seedExecutor, err := NewSeedExecutor(database, backend)
				require.NoError(t, err)
				go func() {
					if err := executor.AcquireLock(t.Context()); err != nil {
						results <- err
						return
					}
					if err := seedExecutor.AcquireSeedLock(t.Context()); err != nil {
						results <- errors.Join(err, executor.ReleaseLock(t.Context()))
						return
					}
					time.Sleep(5 * time.Millisecond)
					results <- errors.Join(seedExecutor.ReleaseSeedLock(t.Context()), executor.ReleaseLock(t.Context()))
				}()
			}
			for range executors {
				require.NoError(t, <-results)
			}
		})
	}
}

func TestExecutorSerialisesInProcessLockHolders(t *testing.T) {
	t.Parallel()

	for name, backend := range map[string]Backend{
		"advisory": Postgres(),
		"table":    PostgresPgBouncer(),
		"none":     SQLite(),
		"duckdb":   DuckDB(),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			database := newMemoryDatabase().open()
			t.Cleanup(func() { _ = database.Close() })
			executor, err := NewExecutor(database, backend)
			require.NoError(t, err)

			require.NoError(t, executor.AcquireLock(t.Context()))
			require.ErrorIs(t, executor.TryAcquireLock(t.Context()), migrate.ErrLockNotAcquired)

			waiter := make(chan error, 1)
			go func() { waiter <- executor.AcquireLock(t.Context()) }()
			select {
			case err := <-waiter:
				require.Failf(t, "a second holder in the same process got the lock early", "%v", err)
			case <-time.After(20 * time.Millisecond):
			}

			require.NoError(t, executor.ReleaseLock(t.Context()))
			require.NoError(t, <-waiter)
			require.NoError(t, executor.ReleaseLock(t.Context()))
			require.NoError(t, executor.ReleaseLock(t.Context()), "releasing with no lock held is a no-op")
		})
	}
}

func TestExecutorLockWaitHonoursContext(t *testing.T) {
	t.Parallel()

	database := newMemoryDatabase().open()
	t.Cleanup(func() { _ = database.Close() })
	executor, err := NewExecutor(database, Postgres())
	require.NoError(t, err)
	require.NoError(t, executor.AcquireLock(t.Context()))
	t.Cleanup(func() { _ = executor.ReleaseLock(context.Background()) })

	cause := errors.New("deploy window closed")
	ctx, cancel := context.WithTimeoutCause(t.Context(), 10*time.Millisecond, cause)
	defer cancel()

	require.ErrorIs(t, executor.AcquireLock(ctx), cause)
}

func TestConcurrentUpAndStatusOnOneMigrator(t *testing.T) {
	t.Parallel()

	memory := newMemoryDatabase()
	database := memory.open()
	t.Cleanup(func() { _ = database.Close() })
	migrator, err := NewMigrator(database, Postgres(), threeMigrations, "migrations")
	require.NoError(t, err)

	const callers = 8
	results := make(chan error, callers)
	for index := range callers {
		go func() {
			if index%2 == 0 {
				_, err := migrator.Up(t.Context())
				results <- err
				return
			}
			_, err := migrator.Status(t.Context())
			results <- err
		}()
	}
	for range callers {
		require.NoError(t, <-results)
	}

	require.ElementsMatch(t, []int64{1, 2, 3}, memory.appliedVersions())
	require.Zero(t, memory.overlappingUse.Load(), "two calls shared one session at the same time")
}

func TestConcurrentFirstBootCreatesTablesUnderTheLock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		backend        Backend
		lockTableRaces int
	}{
		{name: "advisory lock", backend: Postgres()},
		{name: "table lock", backend: PostgresPgBouncer(), lockTableRaces: 1},
		{name: "cockroachdb", backend: CockroachDB(), lockTableRaces: 1},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			memory := newMemoryDatabase()
			memory.lockTableRaces = testCase.lockTableRaces
			const replicas = 3
			results := make(chan error, replicas)
			counts := make(chan int, replicas)
			for range replicas {
				database := memory.open()
				t.Cleanup(func() { _ = database.Close() })
				migrator, err := NewMigrator(database, testCase.backend, threeMigrations, "migrations")
				require.NoError(t, err)
				go func() {
					applied, err := migrator.Up(t.Context())
					counts <- applied
					results <- err
				}()
			}
			total := 0
			for range replicas {
				require.NoError(t, <-results)
				total += <-counts
			}

			require.Equal(t, 3, total)
			require.ElementsMatch(t, []int64{1, 2, 3}, memory.appliedVersions())
			require.Empty(t, memory.outsideLock(), "history DDL ran before the lock was held")
		})
	}
}

func TestStatusAndValidateOnFreshDatabaseRunNoDDL(t *testing.T) {
	t.Parallel()

	for name, backend := range map[string]Backend{
		"advisory": Postgres(),
		"table":    PostgresPgBouncer(),
		"duckdb":   DuckDB(),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			memory := newMemoryDatabase()
			database := memory.open()
			t.Cleanup(func() { _ = database.Close() })
			migrator, err := NewMigrator(database, backend, threeMigrations, "migrations")
			require.NoError(t, err)

			statuses, err := migrator.Status(t.Context())
			require.NoError(t, err)
			require.Len(t, statuses, 3)
			for _, status := range statuses {
				require.False(t, status.Applied)
			}
			require.NoError(t, migrator.Validate(t.Context()))

			for _, statement := range memory.executed() {
				require.NotContains(t, statement, "CREATE", "a read-only call ran DDL")
			}
		})
	}
}

func TestFailedUnlockDiscardsTheSession(t *testing.T) {
	t.Parallel()

	memory := newMemoryDatabase()
	memory.failUnlock = true
	database := memory.open()
	t.Cleanup(func() { _ = database.Close() })
	executor, err := NewExecutor(database, Postgres())
	require.NoError(t, err)

	require.NoError(t, executor.AcquireLock(t.Context()))
	releaseError := executor.ReleaseLock(t.Context())

	require.ErrorContains(t, releaseError, "unlock timed out")
	require.Equal(t, int64(1), memory.closedSessions.Load(), "the session still holding the lock went back to the pool")
}

func TestCancelledAcquireDiscardsTheSession(t *testing.T) {
	t.Parallel()

	memory := newMemoryDatabase()
	database := memory.open()
	t.Cleanup(func() { _ = database.Close() })
	holder, err := NewExecutor(database, Postgres())
	require.NoError(t, err)
	waiter, err := NewExecutor(database, Postgres())
	require.NoError(t, err)
	require.NoError(t, holder.AcquireLock(t.Context()))
	t.Cleanup(func() { _ = holder.ReleaseLock(context.Background()) })

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()

	require.Error(t, waiter.AcquireLock(ctx))
	require.Equal(t, int64(1), memory.closedSessions.Load(), "a session whose lock request was cancelled went back to the pool")
}

func TestUpReturnsTheReleaseFailure(t *testing.T) {
	t.Parallel()

	memory := newMemoryDatabase()
	memory.failUnlock = true
	database := memory.open()
	t.Cleanup(func() { _ = database.Close() })
	migrator, err := NewMigrator(database, Postgres(), threeMigrations, "migrations")
	require.NoError(t, err)

	applied, err := migrator.Up(t.Context())

	require.Equal(t, 3, applied)
	require.ErrorContains(t, err, "releasing migration lock")
	require.True(t, strings.Contains(err.Error(), "unlock timed out"))
}

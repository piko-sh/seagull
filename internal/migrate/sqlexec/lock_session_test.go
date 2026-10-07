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
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type pinningLock struct {
	releases      atomic.Int32
	transactional bool
}

func (lock *pinningLock) Acquire(ctx context.Context, database *sql.DB) (lockHandle, error) {
	connection, err := database.Conn(ctx)
	if err != nil {
		return lockHandle{}, err
	}
	if !lock.transactional {
		return lockHandle{connection: connection, transaction: nil, name: "pinned"}, nil
	}
	transaction, err := connection.BeginTx(ctx, nil)
	if err != nil {
		_ = connection.Close()
		return lockHandle{}, err
	}
	return lockHandle{connection: connection, transaction: transaction, name: "pinned"}, nil
}

func (lock *pinningLock) TryAcquire(ctx context.Context, database *sql.DB) (lockHandle, error) {
	return lock.Acquire(ctx, database)
}

func (lock *pinningLock) Release(_ context.Context, handle lockHandle) error {
	lock.releases.Add(1)
	if handle.connection == nil {
		return nil
	}
	if handle.transaction != nil {
		_ = handle.transaction.Commit()
	}
	return handle.connection.Close()
}

func executorWithLock(t *testing.T, strategy lockStrategy) (*Executor, *sql.DB) {
	t.Helper()

	database := openFakeLockDB(&fakeLockStore{})
	t.Cleanup(func() { _ = database.Close() })
	backend := Postgres()
	backend.migrationLock = strategy
	return newExecutor(database, backend), database
}

func TestTableLockRunsMigrationsOnSeparateConnection(t *testing.T) {
	t.Parallel()

	strategy := &pinningLock{transactional: true}
	executor, _ := executorWithLock(t, strategy)

	require.NoError(t, executor.AcquireLock(t.Context()))
	held := executor.held
	require.NotNil(t, held.handle.connection)
	require.NotNil(t, held.workConnection)
	require.Same(t, held.workConnection, executor.queryExecutor())
	require.NotSame(t, held.handle.connection, executor.queryExecutor())

	require.NoError(t, executor.ReleaseLock(t.Context()))
	require.Nil(t, executor.held)
	require.Equal(t, int32(1), strategy.releases.Load())
}

func TestAdvisoryLockRunsMigrationsOnLockConnection(t *testing.T) {
	t.Parallel()

	strategy := &pinningLock{}
	executor, database := executorWithLock(t, strategy)

	require.NoError(t, executor.TryAcquireLock(t.Context()))
	require.Nil(t, executor.held.workConnection)
	require.Same(t, executor.held.handle.connection, executor.queryExecutor())

	require.NoError(t, executor.ReleaseLock(t.Context()))
	require.Same(t, database, executor.queryExecutor())
}

func TestTableLockReleasedWhenWorkConnectionUnavailable(t *testing.T) {
	t.Parallel()

	strategy := &pinningLock{transactional: true}
	executor, database := executorWithLock(t, strategy)
	database.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeoutCause(t.Context(), 50*time.Millisecond, errors.New("no second connection"))
	defer cancel()

	err := executor.AcquireLock(ctx)

	require.ErrorContains(t, err, "pinning migration connection")
	require.Nil(t, executor.held)
	require.Equal(t, int32(1), strategy.releases.Load())
	database.SetMaxOpenConns(2)
	require.NoError(t, executor.TryAcquireLock(t.Context()), "a failed acquisition must free the in-process slot")
	require.NoError(t, executor.ReleaseLock(t.Context()))
}

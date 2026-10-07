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

package migrate_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/migrate"
)

func newTestMigrator(t *testing.T, executor migrate.ExecutorPort, directory string, options ...migrate.Option) *migrate.Migrator {
	t.Helper()
	migrator, err := migrate.New(executor, os.DirFS("."), directory, options...)
	require.NoError(t, err)
	return migrator
}

func TestMigrationServiceUp(t *testing.T) {
	ctx := t.Context()
	executor := &mockExecutor{}
	service := newTestMigrator(t,
		executor,
		"testdata/basic_migrations",
	)

	count, err := service.Up(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.Len(t, executor.applied, 2)
	assert.Equal(t, int64(1), executor.applied[0].Version)
	assert.Equal(t, int64(2), executor.applied[1].Version)
	assert.Equal(t, "create_users", executor.applied[0].Name)
	assert.Equal(t, "add_orders", executor.applied[1].Name)
}

func TestMigrationServiceUpIdempotent(t *testing.T) {
	ctx := t.Context()
	executor := &mockExecutor{}
	service := newTestMigrator(t,
		executor,
		"testdata/basic_migrations",
	)

	count, err := service.Up(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	count, err = service.Up(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestMigrationServiceUpChecksumMismatch(t *testing.T) {
	ctx := t.Context()
	executor := &mockExecutor{
		preApplied: []migrate.AppliedMigration{
			{Version: 1, Name: "create_users", Checksum: "wrong_checksum"},
		},
	}
	service := newTestMigrator(t,
		executor,
		"testdata/basic_migrations",
	)

	_, err := service.Up(ctx)
	require.Error(t, err)
	var checksumError *migrate.ChecksumMismatchError
	require.ErrorAs(t, err, &checksumError)
	assert.Equal(t, int64(1), checksumError.Version)
}

func TestMigrationServiceDown(t *testing.T) {
	ctx := t.Context()
	executor := &mockExecutor{}
	service := newTestMigrator(t,
		executor,
		"testdata/down_migrations",
	)

	count, err := service.Up(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	count, err = service.Down(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.Len(t, executor.applied, 1)
	assert.Equal(t, int64(1), executor.applied[0].Version)
}

func TestMigrationServiceDownNoDownFile(t *testing.T) {
	ctx := t.Context()
	executor := &mockExecutor{}
	service := newTestMigrator(t,
		executor,
		"testdata/no_down_migrations",
	)

	count, err := service.Up(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	_, err = service.Down(ctx, 1)
	require.Error(t, err)
	require.ErrorIs(t, err, migrate.ErrNoDownMigration)
}

func TestMigrationServiceStatus(t *testing.T) {
	ctx := t.Context()
	executor := &mockExecutor{}
	service := newTestMigrator(t,
		executor,
		"testdata/basic_migrations",
	)

	count, err := service.Up(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	statuses, statusError := service.Status(ctx)
	require.NoError(t, statusError)
	require.Len(t, statuses, 2)

	assert.True(t, statuses[0].Applied)
	assert.True(t, statuses[0].ChecksumMatch)
	assert.True(t, statuses[0].HasDownMigration)
	assert.Equal(t, "create_users", statuses[0].Name)

	assert.True(t, statuses[1].Applied)
	assert.True(t, statuses[1].ChecksumMatch)
	assert.False(t, statuses[1].HasDownMigration)
	assert.Equal(t, "add_orders", statuses[1].Name)
}

func TestMigrationServiceValidate(t *testing.T) {
	ctx := t.Context()
	executor := &mockExecutor{}
	service := newTestMigrator(t,
		executor,
		"testdata/basic_migrations",
	)

	_, err := service.Up(ctx)
	require.NoError(t, err)

	validateError := service.Validate(ctx)
	require.NoError(t, validateError)
}

func TestMigrationServiceNoTransactionDirective(t *testing.T) {
	ctx := t.Context()
	executor := &mockExecutor{}
	service := newTestMigrator(t,
		executor,
		"testdata/no_transaction",
	)

	count, err := service.Up(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.False(t, executor.lastUsedTransaction)
}

func TestMigrationServiceSkippedMigration(t *testing.T) {
	ctx := t.Context()

	content, readError := os.ReadFile("testdata/basic_migrations/0002_add_orders.up.sql")
	require.NoError(t, readError)
	checksum := computeTestChecksum(content)

	executor := &mockExecutor{
		preApplied: []migrate.AppliedMigration{
			{Version: 2, Name: "add_orders", Checksum: checksum},
		},
	}

	service := newTestMigrator(t,
		executor,
		"testdata/basic_migrations",
	)

	count, err := service.Up(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	require.Len(t, executor.applied, 1)
	assert.Equal(t, int64(1), executor.applied[0].Version)
}

func TestMigrationServiceUpTo(t *testing.T) {
	ctx := t.Context()
	executor := &mockExecutor{}
	service := newTestMigrator(t,
		executor,
		"testdata/basic_migrations",
	)

	count, err := service.UpTo(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	require.Len(t, executor.applied, 1)
	assert.Equal(t, int64(1), executor.applied[0].Version)
}

func TestMigrationServiceUpToAll(t *testing.T) {
	ctx := t.Context()
	executor := &mockExecutor{}
	service := newTestMigrator(t,
		executor,
		"testdata/basic_migrations",
	)

	count, err := service.UpTo(ctx, 9999)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestMigrationServiceDownTo(t *testing.T) {
	ctx := t.Context()
	executor := &mockExecutor{}
	service := newTestMigrator(t,
		executor,
		"testdata/down_migrations",
	)

	count, err := service.Up(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	count, err = service.DownTo(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	require.Len(t, executor.applied, 1)
	assert.Equal(t, int64(1), executor.applied[0].Version)
}

func TestMigrationServiceDownToZero(t *testing.T) {
	ctx := t.Context()
	executor := &mockExecutor{}
	service := newTestMigrator(t,
		executor,
		"testdata/down_migrations",
	)

	count, err := service.Up(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	count, err = service.DownTo(ctx, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.Empty(t, executor.applied)
}

func TestMigrationServiceDownChecksumValidation(t *testing.T) {
	ctx := t.Context()

	upContent, readError := os.ReadFile("testdata/down_migrations/0001_create_users.up.sql")
	require.NoError(t, readError)
	upChecksum := computeTestChecksum(upContent)

	executor := &mockExecutor{
		preApplied: []migrate.AppliedMigration{
			{
				Version:      1,
				Name:         "create_users",
				Checksum:     upChecksum,
				DownChecksum: "wrong_down_checksum",
			},
		},
	}

	service := newTestMigrator(t,
		executor,
		"testdata/down_migrations",
	)

	_, err := service.Down(ctx, 1)
	require.Error(t, err)
	var downChecksumError *migrate.DownChecksumMismatchError
	require.ErrorAs(t, err, &downChecksumError)
	assert.Equal(t, int64(1), downChecksumError.Version)
}

func TestMigrationServiceLifecycleHooks(t *testing.T) {
	ctx := t.Context()
	executor := &mockExecutor{}

	var beforeRunCount int
	var beforeMigrationCount int
	var afterMigrationCount int
	var afterRunApplied int

	service := newTestMigrator(t,
		executor,
		"testdata/basic_migrations",
		migrate.WithBeforeRun(func(_ context.Context, hook migrate.MigrationRunHookContext) error {
			beforeRunCount++
			assert.Equal(t, 2, hook.PendingCount)
			return nil
		}),
		migrate.WithBeforeMigration(func(_ context.Context, _ migrate.MigrationHookContext) error {
			beforeMigrationCount++
			return nil
		}),
		migrate.WithAfterMigration(func(_ context.Context, _ migrate.MigrationHookContext) error {
			afterMigrationCount++
			return nil
		}),
		migrate.WithAfterRun(func(_ context.Context, _ migrate.MigrationRunHookContext, applied int) error {
			afterRunApplied = applied
			return nil
		}),
	)

	count, err := service.Up(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.Equal(t, 1, beforeRunCount)
	assert.Equal(t, 2, beforeMigrationCount)
	assert.Equal(t, 2, afterMigrationCount)
	assert.Equal(t, 2, afterRunApplied)
}

func TestMigrationServiceNonBlockingLockAlreadyHeld(t *testing.T) {
	ctx := t.Context()
	executor := &mockExecutor{
		tryLockError: migrate.ErrLockNotAcquired,
	}
	service := newTestMigrator(t,
		executor,
		"testdata/basic_migrations",
		migrate.WithNonBlockingLock(),
	)

	_, err := service.Up(ctx)
	require.Error(t, err)
	require.ErrorIs(t, err, migrate.ErrLockNotAcquired)
}

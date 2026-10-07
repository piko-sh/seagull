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

package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mysql_lexical "seagull.sh/seagull/internal/dialect/mysql/lexical"
	postgres_lexical "seagull.sh/seagull/internal/dialect/postgres/lexical"
	"seagull.sh/seagull/internal/sqlfile"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/testkit/fakefs"
)

type lexicalMigrationExecutor struct {
	*mockMigrationExecutor
	rules sqllex.Rules
}

func (e lexicalMigrationExecutor) LexicalRules() sqllex.Rules {
	return e.rules
}

func testChecksum(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

func TestComputeRollbackStepsClampsNegative(t *testing.T) {
	t.Parallel()

	applied := []AppliedMigration{{Version: 1}, {Version: 2}}

	assert.Equal(t, 0, computeRollbackSteps(applied, new(-3), nil))

	assert.Equal(t, 0, computeRollbackSteps(applied, new(0), nil))

	assert.Equal(t, 1, computeRollbackSteps(applied, new(1), nil))

	assert.Equal(t, len(applied), computeRollbackSteps(applied, new(10), nil))
}

func setupMigrationService(
	t *testing.T,
	executor *mockMigrationExecutor,
	reader *fakefs.FileReader,
	options ...Option,
) *Migrator {
	t.Helper()
	configured, err := newSettings(options)
	require.NoError(t, err)
	return newMigrator(executor, reader, "/migrations", configured)
}

func threeFileMigrationReader() *fakefs.FileReader {
	return &fakefs.FileReader{
		Directories: map[string][]os.DirEntry{
			"/migrations": {
				&fakefs.DirEntry{EntryName: "001_create_users.up.sql"},
				&fakefs.DirEntry{EntryName: "001_create_users.down.sql"},
				&fakefs.DirEntry{EntryName: "002_create_posts.up.sql"},
				&fakefs.DirEntry{EntryName: "002_create_posts.down.sql"},
				&fakefs.DirEntry{EntryName: "003_add_email.up.sql"},
				&fakefs.DirEntry{EntryName: "003_add_email.down.sql"},
			},
		},
		Files: map[string][]byte{
			"/migrations/001_create_users.up.sql":   []byte("CREATE TABLE users (id int);"),
			"/migrations/001_create_users.down.sql": []byte("DROP TABLE users;"),
			"/migrations/002_create_posts.up.sql":   []byte("CREATE TABLE posts (id int);"),
			"/migrations/002_create_posts.down.sql": []byte("DROP TABLE posts;"),
			"/migrations/003_add_email.up.sql":      []byte("ALTER TABLE users ADD COLUMN email text;"),
			"/migrations/003_add_email.down.sql":    []byte("ALTER TABLE users DROP COLUMN email;"),
		},
	}
}

func appliedFromReader(reader *fakefs.FileReader, versions ...int64) []AppliedMigration {
	nameByVersion := map[int64]string{
		1: "create_users",
		2: "create_posts",
		3: "add_email",
	}
	filenameByVersion := map[int64]string{
		1: "/migrations/001_create_users.up.sql",
		2: "/migrations/002_create_posts.up.sql",
		3: "/migrations/003_add_email.up.sql",
	}
	downFilenameByVersion := map[int64]string{
		1: "/migrations/001_create_users.down.sql",
		2: "/migrations/002_create_posts.down.sql",
		3: "/migrations/003_add_email.down.sql",
	}

	result := make([]AppliedMigration, 0, len(versions))
	for _, version := range versions {
		upContent := reader.Files[filenameByVersion[version]]
		downContent := reader.Files[downFilenameByVersion[version]]
		result = append(result, AppliedMigration{
			Version:      version,
			Name:         nameByVersion[version],
			Checksum:     testChecksum(upContent),
			DownChecksum: testChecksum(downContent),
			AppliedAt:    time.Date(2026, 1, int(version), 0, 0, 0, 0, time.UTC),
		})
	}
	return result
}

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("creates with required arguments", func(t *testing.T) {
		t.Parallel()

		migrator, err := New(&mockMigrationExecutor{}, fstest.MapFS{}, "db/migrations/")
		require.NoError(t, err)
		assert.Equal(t, "db/migrations", migrator.directory)
		assert.False(t, migrator.nonBlockingLock, "non-blocking lock should default to false")
	})

	t.Run("options are applied", func(t *testing.T) {
		t.Parallel()

		migrator, err := New(&mockMigrationExecutor{}, fstest.MapFS{}, "", WithNonBlockingLock(), WithDirectivePrefixes("acme"))
		require.NoError(t, err)
		assert.Equal(t, ".", migrator.directory)
		assert.True(t, migrator.nonBlockingLock, "WithNonBlockingLock should set the flag")
		assert.Equal(t, "acme", migrator.prefixes.Canonical)
	})

	t.Run("invalid configuration is rejected", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name      string
			executor  ExecutorPort
			files     fs.FS
			directory string
			options   []Option
		}{
			{name: "nil executor", executor: nil, files: fstest.MapFS{}},
			{name: "nil files", executor: &mockMigrationExecutor{}, files: nil},
			{name: "absolute directory", executor: &mockMigrationExecutor{}, files: fstest.MapFS{}, directory: "/migrations"},
			{name: "bad prefix", executor: &mockMigrationExecutor{}, files: fstest.MapFS{}, options: []Option{WithDirectivePrefixes("Bad")}},
			{name: "bad size limit", executor: &mockMigrationExecutor{}, files: fstest.MapFS{}, options: []Option{WithMaxFileBytes(0)}},
		}
		for _, testCase := range tests {
			migrator, err := New(testCase.executor, testCase.files, testCase.directory, testCase.options...)
			require.ErrorIs(t, err, ErrInvalidConfig, testCase.name)
			assert.Nil(t, migrator, testCase.name)

			seeder, err := NewSeeder(nil, testCase.files, testCase.directory, testCase.options...)
			require.ErrorIs(t, err, ErrInvalidConfig, testCase.name)
			assert.Nil(t, seeder, testCase.name)
		}
	})
}

func TestMigrationService_Up(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		reader        *fakefs.FileReader
		applied       []AppliedMigration
		executorError error
		ensureError   error
		appliedError  error
		lockError     error
		wantCount     int
		wantError     bool
		wantErrType   any
		wantExecuted  []int64
	}{
		{
			name:         "applies all pending migrations",
			reader:       threeFileMigrationReader(),
			applied:      nil,
			wantCount:    3,
			wantExecuted: []int64{1, 2, 3},
		},
		{
			name:         "skips already applied migrations",
			reader:       threeFileMigrationReader(),
			applied:      appliedFromReader(threeFileMigrationReader(), 1, 2),
			wantCount:    1,
			wantExecuted: []int64{3},
		},
		{
			name:      "no pending returns zero",
			reader:    threeFileMigrationReader(),
			applied:   appliedFromReader(threeFileMigrationReader(), 1, 2, 3),
			wantCount: 0,
		},
		{
			name:   "checksum mismatch returns error",
			reader: threeFileMigrationReader(),
			applied: []AppliedMigration{
				{
					Version:  1,
					Name:     "create_users",
					Checksum: "wrong-checksum",
				},
			},
			wantError:   true,
			wantErrType: &ChecksumMismatchError{},
		},
		{
			name:          "executor error is propagated",
			reader:        threeFileMigrationReader(),
			applied:       nil,
			executorError: errors.New("database exploded"),
			wantError:     true,
		},
		{
			name:        "ensure migration table error is propagated",
			reader:      threeFileMigrationReader(),
			applied:     nil,
			ensureError: errors.New("cannot create table"),
			wantError:   true,
		},
		{
			name:         "applied versions error is propagated",
			reader:       threeFileMigrationReader(),
			applied:      nil,
			appliedError: errors.New("cannot read versions"),
			wantError:    true,
		},
		{
			name:        "lock acquisition failure returns LockAcquisitionError",
			reader:      threeFileMigrationReader(),
			applied:     nil,
			lockError:   errors.New("lock held"),
			wantError:   true,
			wantErrType: &LockAcquisitionError{},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var executedVersions []int64
			executor := &mockMigrationExecutor{
				ensureMigrationTableFn: func(_ context.Context) error {
					return testCase.ensureError
				},
				appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
					if testCase.appliedError != nil {
						return nil, testCase.appliedError
					}
					return testCase.applied, nil
				},
				acquireLockFn: func(_ context.Context) error {
					return testCase.lockError
				},
				releaseLockFn: func(_ context.Context) error {
					return nil
				},
				executeMigrationFn: func(_ context.Context, migration MigrationRecord, _ MigrationDirection, _ bool) error {
					if testCase.executorError != nil {
						return testCase.executorError
					}
					executedVersions = append(executedVersions, migration.Version)
					return nil
				},
			}

			service := setupMigrationService(t, executor, testCase.reader)
			count, err := service.Up(t.Context())

			if testCase.wantError {
				require.Error(t, err)
				if testCase.wantErrType != nil {
					assert.ErrorAs(t, err, &testCase.wantErrType,
						"error should be of the expected type")
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.wantCount, count)
			if testCase.wantExecuted != nil {
				assert.Equal(t, testCase.wantExecuted, executedVersions,
					"migrations should be executed in version order")
			}
		})
	}
}

func TestMigrationService_Up_lock_acquired_and_released(t *testing.T) {
	t.Parallel()

	reader := threeFileMigrationReader()
	var lockAcquired, lockReleased bool

	executor := &mockMigrationExecutor{
		appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
			return nil, nil
		},
		acquireLockFn: func(_ context.Context) error {
			lockAcquired = true
			return nil
		},
		releaseLockFn: func(_ context.Context) error {
			lockReleased = true
			return nil
		},
	}

	service := setupMigrationService(t, executor, reader)
	_, err := service.Up(t.Context())

	require.NoError(t, err)
	assert.True(t, lockAcquired, "lock should be acquired during Up")
	assert.True(t, lockReleased, "lock should be released after Up")
}

func TestMigrationService_Down(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		reader         *fakefs.FileReader
		applied        []AppliedMigration
		steps          int
		executorError  error
		wantCount      int
		wantError      bool
		wantErrType    any
		wantRolledBack []int64
	}{
		{
			name:           "rolls back N migrations in reverse order",
			reader:         threeFileMigrationReader(),
			applied:        appliedFromReader(threeFileMigrationReader(), 1, 2, 3),
			steps:          2,
			wantCount:      2,
			wantRolledBack: []int64{3, 2},
		},
		{
			name:           "rolls back all when N exceeds applied count",
			reader:         threeFileMigrationReader(),
			applied:        appliedFromReader(threeFileMigrationReader(), 1, 2),
			steps:          5,
			wantCount:      2,
			wantRolledBack: []int64{2, 1},
		},
		{
			name:      "returns zero when nothing applied",
			reader:    threeFileMigrationReader(),
			applied:   nil,
			steps:     3,
			wantCount: 0,
		},
		{
			name: "no down file returns NoDownMigrationError",
			reader: &fakefs.FileReader{
				Directories: map[string][]os.DirEntry{
					"/migrations": {
						&fakefs.DirEntry{EntryName: "001_create_users.up.sql"},
					},
				},
				Files: map[string][]byte{
					"/migrations/001_create_users.up.sql": []byte("CREATE TABLE users (id int);"),
				},
			},
			applied: []AppliedMigration{
				{
					Version:  1,
					Name:     "create_users",
					Checksum: testChecksum([]byte("CREATE TABLE users (id int);")),
				},
			},
			steps:       1,
			wantError:   true,
			wantErrType: &NoDownMigrationError{},
		},
		{
			name:          "executor error during rollback is propagated",
			reader:        threeFileMigrationReader(),
			applied:       appliedFromReader(threeFileMigrationReader(), 1, 2, 3),
			steps:         1,
			executorError: errors.New("rollback failed"),
			wantError:     true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var rolledBackVersions []int64
			executor := &mockMigrationExecutor{
				appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
					return testCase.applied, nil
				},
				acquireLockFn: func(_ context.Context) error {
					return nil
				},
				releaseLockFn: func(_ context.Context) error {
					return nil
				},
				executeMigrationFn: func(_ context.Context, migration MigrationRecord, _ MigrationDirection, _ bool) error {
					if testCase.executorError != nil {
						return testCase.executorError
					}
					rolledBackVersions = append(rolledBackVersions, migration.Version)
					return nil
				},
			}

			service := setupMigrationService(t, executor, testCase.reader)
			count, err := service.Down(t.Context(), testCase.steps)

			if testCase.wantError {
				require.Error(t, err)
				if testCase.wantErrType != nil {
					assert.ErrorAs(t, err, &testCase.wantErrType,
						"error should be of the expected type")
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.wantCount, count)
			if testCase.wantRolledBack != nil {
				assert.Equal(t, testCase.wantRolledBack, rolledBackVersions,
					"migrations should be rolled back in reverse version order")
			}
		})
	}
}

func TestMigrationService_UpTo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		reader        *fakefs.FileReader
		applied       []AppliedMigration
		targetVersion int64
		wantCount     int
		wantExecuted  []int64
	}{
		{
			name:          "applies up to target version",
			reader:        threeFileMigrationReader(),
			applied:       nil,
			targetVersion: 2,
			wantCount:     2,
			wantExecuted:  []int64{1, 2},
		},
		{
			name:          "target below all pending applies none",
			reader:        threeFileMigrationReader(),
			applied:       nil,
			targetVersion: 0,
			wantCount:     0,
		},
		{
			name:          "target above all pending applies all",
			reader:        threeFileMigrationReader(),
			applied:       nil,
			targetVersion: 100,
			wantCount:     3,
			wantExecuted:  []int64{1, 2, 3},
		},
		{
			name:          "respects already applied when targeting",
			reader:        threeFileMigrationReader(),
			applied:       appliedFromReader(threeFileMigrationReader(), 1),
			targetVersion: 2,
			wantCount:     1,
			wantExecuted:  []int64{2},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var executedVersions []int64
			executor := &mockMigrationExecutor{
				appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
					return testCase.applied, nil
				},
				acquireLockFn: func(_ context.Context) error {
					return nil
				},
				releaseLockFn: func(_ context.Context) error {
					return nil
				},
				executeMigrationFn: func(_ context.Context, migration MigrationRecord, _ MigrationDirection, _ bool) error {
					executedVersions = append(executedVersions, migration.Version)
					return nil
				},
			}

			service := setupMigrationService(t, executor, testCase.reader)
			count, err := service.UpTo(t.Context(), testCase.targetVersion)

			require.NoError(t, err)
			assert.Equal(t, testCase.wantCount, count)
			if testCase.wantExecuted != nil {
				assert.Equal(t, testCase.wantExecuted, executedVersions)
			}
		})
	}
}

func TestMigrationService_DownTo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		reader         *fakefs.FileReader
		applied        []AppliedMigration
		targetVersion  int64
		wantCount      int
		wantRolledBack []int64
	}{
		{
			name:           "rolls back to target version exclusive",
			reader:         threeFileMigrationReader(),
			applied:        appliedFromReader(threeFileMigrationReader(), 1, 2, 3),
			targetVersion:  1,
			wantCount:      2,
			wantRolledBack: []int64{3, 2},
		},
		{
			name:          "target at highest applied rolls back nothing",
			reader:        threeFileMigrationReader(),
			applied:       appliedFromReader(threeFileMigrationReader(), 1, 2, 3),
			targetVersion: 3,
			wantCount:     0,
		},
		{
			name:           "target below all applied rolls back everything",
			reader:         threeFileMigrationReader(),
			applied:        appliedFromReader(threeFileMigrationReader(), 1, 2, 3),
			targetVersion:  0,
			wantCount:      3,
			wantRolledBack: []int64{3, 2, 1},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var rolledBackVersions []int64
			executor := &mockMigrationExecutor{
				appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
					return testCase.applied, nil
				},
				acquireLockFn: func(_ context.Context) error {
					return nil
				},
				releaseLockFn: func(_ context.Context) error {
					return nil
				},
				executeMigrationFn: func(_ context.Context, migration MigrationRecord, _ MigrationDirection, _ bool) error {
					rolledBackVersions = append(rolledBackVersions, migration.Version)
					return nil
				},
			}

			service := setupMigrationService(t, executor, testCase.reader)
			count, err := service.DownTo(t.Context(), testCase.targetVersion)

			require.NoError(t, err)
			assert.Equal(t, testCase.wantCount, count)
			if testCase.wantRolledBack != nil {
				assert.Equal(t, testCase.wantRolledBack, rolledBackVersions,
					"migrations should be rolled back in reverse version order")
			}
		})
	}
}

func TestMigrationService_Status(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		reader       *fakefs.FileReader
		applied      []AppliedMigration
		appliedError error
		wantCount    int
		wantError    bool
		verify       func(t *testing.T, statuses []MigrationStatus)
	}{
		{
			name:      "combines files and applied state",
			reader:    threeFileMigrationReader(),
			applied:   appliedFromReader(threeFileMigrationReader(), 1, 2),
			wantCount: 3,
			verify: func(t *testing.T, statuses []MigrationStatus) {
				t.Helper()

				assert.True(t, statuses[0].Applied, "version 1 should be applied")
				assert.True(t, statuses[0].ChecksumMatch, "version 1 checksum should match")
				assert.True(t, statuses[0].HasDownMigration, "version 1 should have down migration")
				assert.Equal(t, int64(1), statuses[0].Version)
				assert.Equal(t, "create_users", statuses[0].Name)

				assert.True(t, statuses[1].Applied, "version 2 should be applied")
				assert.True(t, statuses[1].ChecksumMatch, "version 2 checksum should match")

				assert.False(t, statuses[2].Applied, "version 3 should be pending")
				assert.False(t, statuses[2].ChecksumMatch, "version 3 checksum match should be false when not applied")
				assert.True(t, statuses[2].HasDownMigration, "version 3 should have down migration")
			},
		},
		{
			name:   "checksum mismatch flagged correctly",
			reader: threeFileMigrationReader(),
			applied: []AppliedMigration{
				{
					Version:  1,
					Name:     "create_users",
					Checksum: "wrong-checksum",
				},
			},
			wantCount: 3,
			verify: func(t *testing.T, statuses []MigrationStatus) {
				t.Helper()
				assert.True(t, statuses[0].Applied, "version 1 should be applied")
				assert.False(t, statuses[0].ChecksumMatch, "version 1 checksum should NOT match")
			},
		},
		{
			name:         "applied versions error is propagated",
			reader:       threeFileMigrationReader(),
			appliedError: errors.New("query failed"),
			wantError:    true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			executor := &mockMigrationExecutor{
				appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
					if testCase.appliedError != nil {
						return nil, testCase.appliedError
					}
					return testCase.applied, nil
				},
			}

			service := setupMigrationService(t, executor, testCase.reader)
			statuses, err := service.Status(t.Context())

			if testCase.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, statuses, testCase.wantCount)
			if testCase.verify != nil {
				testCase.verify(t, statuses)
			}
		})
	}
}

func TestMigrationService_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		reader       *fakefs.FileReader
		applied      []AppliedMigration
		appliedError error
		wantError    bool
		wantErrType  any
	}{
		{
			name:      "all checksums match returns nil",
			reader:    threeFileMigrationReader(),
			applied:   appliedFromReader(threeFileMigrationReader(), 1, 2, 3),
			wantError: false,
		},
		{
			name:   "checksum mismatch returns ChecksumMismatchError",
			reader: threeFileMigrationReader(),
			applied: []AppliedMigration{
				{
					Version:  1,
					Name:     "create_users",
					Checksum: "tampered-checksum",
				},
			},
			wantError:   true,
			wantErrType: &ChecksumMismatchError{},
		},
		{
			name: "missing file for applied migration returns MissingMigrationFileError",
			reader: &fakefs.FileReader{
				Directories: map[string][]os.DirEntry{
					"/migrations": {
						&fakefs.DirEntry{EntryName: "001_create_users.up.sql"},
					},
				},
				Files: map[string][]byte{
					"/migrations/001_create_users.up.sql": []byte("CREATE TABLE users (id int);"),
				},
			},
			applied: []AppliedMigration{
				{
					Version:  1,
					Name:     "create_users",
					Checksum: testChecksum([]byte("CREATE TABLE users (id int);")),
				},
				{
					Version:  2,
					Name:     "create_posts",
					Checksum: "some-checksum",
				},
			},
			wantError:   true,
			wantErrType: &MissingMigrationFileError{},
		},
		{
			name:         "applied versions error is propagated",
			reader:       threeFileMigrationReader(),
			appliedError: errors.New("query broken"),
			wantError:    true,
		},
		{
			name:      "no applied migrations validates successfully",
			reader:    threeFileMigrationReader(),
			applied:   nil,
			wantError: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			executor := &mockMigrationExecutor{
				appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
					if testCase.appliedError != nil {
						return nil, testCase.appliedError
					}
					return testCase.applied, nil
				},
			}

			service := setupMigrationService(t, executor, testCase.reader)
			err := service.Validate(t.Context())

			if testCase.wantError {
				require.Error(t, err)
				if testCase.wantErrType != nil {
					assert.ErrorAs(t, err, &testCase.wantErrType,
						"error should be of the expected type")
				}
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestMigrationService_Hooks(t *testing.T) {
	t.Parallel()

	t.Run("before migration hook called for each migration", func(t *testing.T) {
		t.Parallel()

		reader := threeFileMigrationReader()
		var hookedVersions []int64

		executor := &mockMigrationExecutor{
			appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
				return nil, nil
			},
			acquireLockFn: func(_ context.Context) error {
				return nil
			},
			releaseLockFn: func(_ context.Context) error {
				return nil
			},
		}

		hook := WithBeforeMigration(func(_ context.Context, hookCtx MigrationHookContext) error {
			hookedVersions = append(hookedVersions, hookCtx.Version)
			assert.Equal(t, MigrationDirectionUp, hookCtx.Direction)
			return nil
		})

		service := setupMigrationService(t, executor, reader, hook)
		count, err := service.Up(t.Context())

		require.NoError(t, err)
		assert.Equal(t, 3, count)
		assert.Equal(t, []int64{1, 2, 3}, hookedVersions,
			"before hook should fire for each pending migration in order")
	})

	t.Run("after migration hook called for each migration", func(t *testing.T) {
		t.Parallel()

		reader := threeFileMigrationReader()
		var hookedVersions []int64

		executor := &mockMigrationExecutor{
			appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
				return nil, nil
			},
			acquireLockFn: func(_ context.Context) error {
				return nil
			},
			releaseLockFn: func(_ context.Context) error {
				return nil
			},
		}

		hook := WithAfterMigration(func(_ context.Context, hookCtx MigrationHookContext) error {
			hookedVersions = append(hookedVersions, hookCtx.Version)
			return nil
		})

		service := setupMigrationService(t, executor, reader, hook)
		count, err := service.Up(t.Context())

		require.NoError(t, err)
		assert.Equal(t, 3, count)
		assert.Equal(t, []int64{1, 2, 3}, hookedVersions,
			"after hook should fire for each applied migration in order")
	})

	t.Run("before run hook called before execution", func(t *testing.T) {
		t.Parallel()

		reader := threeFileMigrationReader()
		var hookContext MigrationRunHookContext
		hookCalled := false

		executor := &mockMigrationExecutor{
			appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
				return nil, nil
			},
			acquireLockFn: func(_ context.Context) error {
				return nil
			},
			releaseLockFn: func(_ context.Context) error {
				return nil
			},
		}

		hook := WithBeforeRun(func(_ context.Context, ctx MigrationRunHookContext) error {
			hookCalled = true
			hookContext = ctx
			return nil
		})

		service := setupMigrationService(t, executor, reader, hook)
		_, err := service.Up(t.Context())

		require.NoError(t, err)
		assert.True(t, hookCalled, "before run hook should be called")
		assert.Equal(t, 3, hookContext.PendingCount)
		assert.Equal(t, MigrationDirectionUp, hookContext.Direction)
		assert.Equal(t, []int64{1, 2, 3}, hookContext.PendingVersions)
	})

	t.Run("after run hook called after execution", func(t *testing.T) {
		t.Parallel()

		reader := threeFileMigrationReader()
		var appliedCount int
		hookCalled := false

		executor := &mockMigrationExecutor{
			appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
				return nil, nil
			},
			acquireLockFn: func(_ context.Context) error {
				return nil
			},
			releaseLockFn: func(_ context.Context) error {
				return nil
			},
		}

		hook := WithAfterRun(func(_ context.Context, _ MigrationRunHookContext, applied int) error {
			hookCalled = true
			appliedCount = applied
			return nil
		})

		service := setupMigrationService(t, executor, reader, hook)
		_, err := service.Up(t.Context())

		require.NoError(t, err)
		assert.True(t, hookCalled, "after run hook should be called")
		assert.Equal(t, 3, appliedCount, "after run hook should receive correct applied count")
	})

	t.Run("before migration hook error cancels the run", func(t *testing.T) {
		t.Parallel()

		reader := threeFileMigrationReader()
		hookError := errors.New("hook vetoed the migration")

		executor := &mockMigrationExecutor{
			appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
				return nil, nil
			},
			acquireLockFn: func(_ context.Context) error {
				return nil
			},
			releaseLockFn: func(_ context.Context) error {
				return nil
			},
		}

		hook := WithBeforeMigration(func(_ context.Context, _ MigrationHookContext) error {
			return hookError
		})

		service := setupMigrationService(t, executor, reader, hook)
		count, err := service.Up(t.Context())

		require.ErrorIs(t, err, hookError)
		assert.Equal(t, 0, count, "no migrations should be applied when hook returns error")
	})

	t.Run("before run hook error cancels the run", func(t *testing.T) {
		t.Parallel()

		reader := threeFileMigrationReader()
		hookError := errors.New("run vetoed")

		executor := &mockMigrationExecutor{
			appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
				return nil, nil
			},
			acquireLockFn: func(_ context.Context) error {
				return nil
			},
			releaseLockFn: func(_ context.Context) error {
				return nil
			},
		}

		hook := WithBeforeRun(func(_ context.Context, _ MigrationRunHookContext) error {
			return hookError
		})

		service := setupMigrationService(t, executor, reader, hook)
		count, err := service.Up(t.Context())

		require.ErrorIs(t, err, hookError)
		assert.Equal(t, 0, count)
	})

	t.Run("hooks fire during rollback with correct direction", func(t *testing.T) {
		t.Parallel()

		reader := threeFileMigrationReader()
		var beforeVersions []int64
		var afterVersions []int64
		var runDirection MigrationDirection

		executor := &mockMigrationExecutor{
			appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
				return appliedFromReader(threeFileMigrationReader(), 1, 2, 3), nil
			},
			acquireLockFn: func(_ context.Context) error {
				return nil
			},
			releaseLockFn: func(_ context.Context) error {
				return nil
			},
		}

		service := setupMigrationService(t, executor, reader,
			WithBeforeMigration(func(_ context.Context, hookCtx MigrationHookContext) error {
				beforeVersions = append(beforeVersions, hookCtx.Version)
				assert.Equal(t, MigrationDirectionDown, hookCtx.Direction)
				return nil
			}),
			WithAfterMigration(func(_ context.Context, hookCtx MigrationHookContext) error {
				afterVersions = append(afterVersions, hookCtx.Version)
				return nil
			}),
			WithBeforeRun(func(_ context.Context, ctx MigrationRunHookContext) error {
				runDirection = ctx.Direction
				return nil
			}),
		)

		count, err := service.Down(t.Context(), 2)

		require.NoError(t, err)
		assert.Equal(t, 2, count)
		assert.Equal(t, []int64{3, 2}, beforeVersions)
		assert.Equal(t, []int64{3, 2}, afterVersions)
		assert.Equal(t, MigrationDirectionDown, runDirection)
	})
}

func TestMigrationService_NonBlockingLock(t *testing.T) {
	t.Parallel()

	t.Run("uses TryAcquireLock instead of AcquireLock", func(t *testing.T) {
		t.Parallel()

		reader := threeFileMigrationReader()
		var acquireCalled atomic.Bool
		var tryAcquireCalled atomic.Bool

		executor := &mockMigrationExecutor{
			appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
				return nil, nil
			},
			acquireLockFn: func(_ context.Context) error {
				acquireCalled.Store(true)
				return nil
			},
			tryAcquireLockFn: func(_ context.Context) error {
				tryAcquireCalled.Store(true)
				return nil
			},
			releaseLockFn: func(_ context.Context) error {
				return nil
			},
		}

		service := setupMigrationService(t, executor, reader, WithNonBlockingLock())
		_, err := service.Up(t.Context())

		require.NoError(t, err)
		assert.True(t, tryAcquireCalled.Load(),
			"TryAcquireLock should be called when non-blocking lock is configured")
		assert.False(t, acquireCalled.Load(),
			"AcquireLock should NOT be called when non-blocking lock is configured")
	})

	t.Run("blocking lock uses AcquireLock by default", func(t *testing.T) {
		t.Parallel()

		reader := threeFileMigrationReader()
		var acquireCalled atomic.Bool
		var tryAcquireCalled atomic.Bool

		executor := &mockMigrationExecutor{
			appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
				return nil, nil
			},
			acquireLockFn: func(_ context.Context) error {
				acquireCalled.Store(true)
				return nil
			},
			tryAcquireLockFn: func(_ context.Context) error {
				tryAcquireCalled.Store(true)
				return nil
			},
			releaseLockFn: func(_ context.Context) error {
				return nil
			},
		}

		service := setupMigrationService(t, executor, reader)
		_, err := service.Up(t.Context())

		require.NoError(t, err)
		assert.True(t, acquireCalled.Load(),
			"AcquireLock should be called when non-blocking lock is NOT configured")
		assert.False(t, tryAcquireCalled.Load(),
			"TryAcquireLock should NOT be called when non-blocking lock is NOT configured")
	})

	t.Run("non-blocking lock failure on Down returns LockAcquisitionError", func(t *testing.T) {
		t.Parallel()

		reader := threeFileMigrationReader()
		executor := &mockMigrationExecutor{
			appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
				return appliedFromReader(threeFileMigrationReader(), 1, 2), nil
			},
			tryAcquireLockFn: func(_ context.Context) error {
				return ErrLockNotAcquired
			},
			releaseLockFn: func(_ context.Context) error {
				return nil
			},
		}

		service := setupMigrationService(t, executor, reader, WithNonBlockingLock())
		_, err := service.Down(t.Context(), 1)

		require.Error(t, err)
		var lockError *LockAcquisitionError
		assert.ErrorAs(t, err, &lockError, "should wrap as LockAcquisitionError")
		assert.ErrorIs(t, lockError, ErrLockNotAcquired)
	})
}

func TestMigrationService_NoTransactionDirective(t *testing.T) {
	t.Parallel()

	t.Run("migration with no-transaction directive runs without transaction", func(t *testing.T) {
		t.Parallel()

		noTransactionContent := []byte("-- sgl.migration(no_transaction: true)\nCREATE INDEX CONCURRENTLY idx ON users (email);")
		reader := &fakefs.FileReader{
			Directories: map[string][]os.DirEntry{
				"/migrations": {
					&fakefs.DirEntry{EntryName: "001_add_index.up.sql"},
				},
			},
			Files: map[string][]byte{
				"/migrations/001_add_index.up.sql": noTransactionContent,
			},
		}

		var capturedUseTransaction bool
		executor := &mockMigrationExecutor{
			appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
				return nil, nil
			},
			acquireLockFn: func(_ context.Context) error {
				return nil
			},
			releaseLockFn: func(_ context.Context) error {
				return nil
			},
			executeMigrationFn: func(_ context.Context, _ MigrationRecord, _ MigrationDirection, useTransaction bool) error {
				capturedUseTransaction = useTransaction
				return nil
			},
		}

		service := setupMigrationService(t, executor, reader)
		count, err := service.Up(t.Context())

		require.NoError(t, err)
		assert.Equal(t, 1, count)
		assert.False(t, capturedUseTransaction,
			"migration with no-transaction directive should run without transaction wrapping")
	})

	t.Run("migration without directive runs with transaction", func(t *testing.T) {
		t.Parallel()

		reader := &fakefs.FileReader{
			Directories: map[string][]os.DirEntry{
				"/migrations": {
					&fakefs.DirEntry{EntryName: "001_create_table.up.sql"},
				},
			},
			Files: map[string][]byte{
				"/migrations/001_create_table.up.sql": []byte("CREATE TABLE foo (id int);"),
			},
		}

		var capturedUseTransaction bool
		executor := &mockMigrationExecutor{
			appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
				return nil, nil
			},
			acquireLockFn: func(_ context.Context) error {
				return nil
			},
			releaseLockFn: func(_ context.Context) error {
				return nil
			},
			executeMigrationFn: func(_ context.Context, _ MigrationRecord, _ MigrationDirection, useTransaction bool) error {
				capturedUseTransaction = useTransaction
				return nil
			},
		}

		service := setupMigrationService(t, executor, reader)
		count, err := service.Up(t.Context())

		require.NoError(t, err)
		assert.Equal(t, 1, count)
		assert.True(t, capturedUseTransaction,
			"migration without the directive should run with transaction wrapping")
	})
}

func TestMigrationService_DownChecksumMismatch(t *testing.T) {
	t.Parallel()

	reader := threeFileMigrationReader()

	applied := appliedFromReader(threeFileMigrationReader(), 1, 2, 3)
	applied[2].DownChecksum = "tampered-down-checksum"

	executor := &mockMigrationExecutor{
		appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
			return applied, nil
		},
		acquireLockFn: func(_ context.Context) error {
			return nil
		},
		releaseLockFn: func(_ context.Context) error {
			return nil
		},
	}

	service := setupMigrationService(t, executor, reader)
	_, err := service.Down(t.Context(), 1)

	require.Error(t, err)
	var mismatchError *DownChecksumMismatchError
	assert.ErrorAs(t, err, &mismatchError,
		"should return DownChecksumMismatchError when the down file checksum differs")
	assert.Equal(t, int64(3), mismatchError.Version)
}

func TestMigrationService_ContextCancellation(t *testing.T) {
	t.Parallel()

	t.Run("cancelled context during Up stops early", func(t *testing.T) {
		t.Parallel()

		reader := threeFileMigrationReader()
		var executedCount int

		ctx, cancel := context.WithCancel(t.Context())

		executor := &mockMigrationExecutor{
			appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
				return nil, nil
			},
			acquireLockFn: func(_ context.Context) error {
				return nil
			},
			releaseLockFn: func(_ context.Context) error {
				return nil
			},
			executeMigrationFn: func(_ context.Context, _ MigrationRecord, _ MigrationDirection, _ bool) error {
				executedCount++
				if executedCount == 1 {
					cancel()
				}
				return nil
			},
		}

		service := setupMigrationService(t, executor, reader)
		count, err := service.Up(ctx)

		require.Error(t, err)
		assert.Equal(t, 1, count, "should stop after the first migration")
	})
}

func TestMigrationService_MigrationRecordContent(t *testing.T) {
	t.Parallel()

	reader := threeFileMigrationReader()
	var capturedRecord MigrationRecord

	executor := &mockMigrationExecutor{
		appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {

			return appliedFromReader(threeFileMigrationReader(), 1, 2), nil
		},
		acquireLockFn: func(_ context.Context) error {
			return nil
		},
		releaseLockFn: func(_ context.Context) error {
			return nil
		},
		executeMigrationFn: func(_ context.Context, migration MigrationRecord, _ MigrationDirection, _ bool) error {
			capturedRecord = migration
			return nil
		},
	}

	service := setupMigrationService(t, executor, reader)
	count, err := service.Up(t.Context())

	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.Equal(t, int64(3), capturedRecord.Version)
	assert.Equal(t, "add_email", capturedRecord.Name)
	assert.Equal(t,
		testChecksum([]byte("ALTER TABLE users ADD COLUMN email text;")),
		capturedRecord.Checksum,
		"up checksum should match the file content",
	)
	assert.Equal(t,
		testChecksum([]byte("ALTER TABLE users DROP COLUMN email;")),
		capturedRecord.DownChecksum,
		"down checksum should be included in the record",
	)
}

func TestMigrationService_ReadDirError(t *testing.T) {
	t.Parallel()

	reader := &fakefs.FileReader{
		ReadDirErrors: map[string]error{
			"/migrations": errors.New("permission denied"),
		},
	}
	executor := &mockMigrationExecutor{}

	service := setupMigrationService(t, executor, reader)

	_, upError := service.Up(t.Context())
	require.Error(t, upError, "Up should propagate directory read errors")

	_, downError := service.Down(t.Context(), 1)
	require.Error(t, downError, "Down should propagate directory read errors")

	_, statusError := service.Status(t.Context())
	require.Error(t, statusError, "Status should propagate directory read errors")

	validateError := service.Validate(t.Context())
	require.Error(t, validateError, "Validate should propagate directory read errors")
}

func TestMigrationService_WithDirectivePrefixes(t *testing.T) {
	t.Parallel()

	runUp := func(t *testing.T, content string, options ...Option) bool {
		t.Helper()
		reader := &fakefs.FileReader{
			Directories: map[string][]os.DirEntry{
				"/migrations": {
					&fakefs.DirEntry{EntryName: "001_add_index.up.sql"},
				},
			},
			Files: map[string][]byte{
				"/migrations/001_add_index.up.sql": []byte(content),
			},
		}
		var capturedUseTransaction bool
		executor := &mockMigrationExecutor{
			appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
				return nil, nil
			},
			acquireLockFn: func(_ context.Context) error {
				return nil
			},
			releaseLockFn: func(_ context.Context) error {
				return nil
			},
			executeMigrationFn: func(_ context.Context, _ MigrationRecord, _ MigrationDirection, useTransaction bool) error {
				capturedUseTransaction = useTransaction
				return nil
			},
		}
		service := setupMigrationService(t, executor, reader, options...)
		count, err := service.Up(t.Context())
		require.NoError(t, err)
		require.Equal(t, 1, count)
		return capturedUseTransaction
	}

	acmeContent := "-- acme.migration(no_transaction: true)\nCREATE TABLE t (id INT);"
	aliasContent := "-- acmedb.migration(no_transaction: true)\nCREATE TABLE t (id INT);"
	seagullContent := "-- seagull.migration(no_transaction: true)\nCREATE TABLE t (id INT);"

	assert.True(t, runUp(t, acmeContent), "the default prefixes must not recognise acme")
	assert.False(t, runUp(t, seagullContent), "the default prefixes include the seagull alias")
	assert.False(t, runUp(t, acmeContent, WithDirectivePrefixes("acme", "acmedb")))
	assert.False(t, runUp(t, aliasContent, WithDirectivePrefixes("acme", "acmedb")))
	assert.True(t, runUp(t, seagullContent, WithDirectivePrefixes("acme")), "a custom set replaces the defaults")
}

func TestMigrationService_UpReportsDriftWhenNothingPending(t *testing.T) {
	t.Parallel()

	editedReader := threeFileMigrationReader()
	editedApplied := appliedFromReader(threeFileMigrationReader(), 1, 2, 3)
	editedReader.Files["/migrations/002_create_posts.up.sql"] = []byte("CREATE TABLE posts (id bigint);")

	deletedReader := threeFileMigrationReader()
	deletedApplied := appliedFromReader(threeFileMigrationReader(), 1, 2, 3)
	deletedReader.Directories["/migrations"] = deletedReader.Directories["/migrations"][:4]

	tests := []struct {
		name        string
		reader      *fakefs.FileReader
		applied     []AppliedMigration
		target      *int64
		wantErrType any
	}{
		{name: "edited applied file", reader: editedReader, applied: editedApplied, wantErrType: &ChecksumMismatchError{}},
		{name: "deleted applied file", reader: deletedReader, applied: deletedApplied, wantErrType: &MissingMigrationFileError{}},
		{name: "edited applied file below the target", reader: editedReader, applied: editedApplied, target: new(int64(3)), wantErrType: &ChecksumMismatchError{}},
		{name: "deleted applied file below the target", reader: deletedReader, applied: deletedApplied, target: new(int64(1)), wantErrType: &MissingMigrationFileError{}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var lockTaken atomic.Bool
			executor := &mockMigrationExecutor{
				appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
					return testCase.applied, nil
				},
				acquireLockFn: func(_ context.Context) error {
					lockTaken.Store(true)
					return nil
				},
			}

			service := setupMigrationService(t, executor, testCase.reader)
			var count int
			var err error
			if testCase.target == nil {
				count, err = service.Up(t.Context())
			} else {
				count, err = service.UpTo(t.Context(), *testCase.target)
			}

			require.Error(t, err)
			require.ErrorAs(t, err, &testCase.wantErrType)
			require.Zero(t, count)
			require.False(t, lockTaken.Load())
		})
	}
}

func TestMigrationService_UpValidatesDriftUnderTheLock(t *testing.T) {
	t.Parallel()

	reader := threeFileMigrationReader()
	reads := 0
	executor := &mockMigrationExecutor{
		appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
			reads++
			if reads == 1 {
				return appliedFromReader(threeFileMigrationReader(), 1), nil
			}
			drifted := appliedFromReader(threeFileMigrationReader(), 1, 2, 3)
			drifted[2].Checksum = "written-by-another-process"
			return drifted, nil
		},
	}

	service := setupMigrationService(t, executor, reader)
	_, err := service.Up(t.Context())

	var mismatch *ChecksumMismatchError
	require.ErrorAs(t, err, &mismatch)
	require.Equal(t, int64(3), mismatch.Version)
}

func TestMigrationService_TableCreationOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		run     func(service *Migrator) error
		applied []AppliedMigration
		want    []string
	}{
		{
			name:    "up creates the history table under the lock",
			run:     func(service *Migrator) error { _, err := service.Up(t.Context()); return err },
			applied: nil,
			want:    []string{"applied", "lock", "ensure", "applied", "execute", "execute", "execute", "release"},
		},
		{
			name:    "up with nothing pending takes no lock and runs no DDL",
			run:     func(service *Migrator) error { _, err := service.Up(t.Context()); return err },
			applied: appliedFromReader(threeFileMigrationReader(), 1, 2, 3),
			want:    []string{"applied"},
		},
		{
			name:    "down runs no DDL",
			run:     func(service *Migrator) error { _, err := service.Down(t.Context(), 1); return err },
			applied: appliedFromReader(threeFileMigrationReader(), 1, 2, 3),
			want:    []string{"lock", "applied", "execute", "release"},
		},
		{
			name:    "status is read-only",
			run:     func(service *Migrator) error { _, err := service.Status(t.Context()); return err },
			applied: nil,
			want:    []string{"applied"},
		},
		{
			name:    "validate is read-only",
			run:     func(service *Migrator) error { return service.Validate(t.Context()) },
			applied: nil,
			want:    []string{"applied"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var calls []string
			executor := &mockMigrationExecutor{
				ensureMigrationTableFn: func(_ context.Context) error {
					calls = append(calls, "ensure")
					return nil
				},
				appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
					calls = append(calls, "applied")
					return testCase.applied, nil
				},
				acquireLockFn: func(_ context.Context) error {
					calls = append(calls, "lock")
					return nil
				},
				releaseLockFn: func(_ context.Context) error {
					calls = append(calls, "release")
					return nil
				},
				executeMigrationFn: func(_ context.Context, _ MigrationRecord, _ MigrationDirection, _ bool) error {
					calls = append(calls, "execute")
					return nil
				},
			}

			require.NoError(t, testCase.run(setupMigrationService(t, executor, threeFileMigrationReader())))
			require.Equal(t, testCase.want, calls)
		})
	}
}

func TestMigrationService_ReleaseErrorIsReturned(t *testing.T) {
	t.Parallel()

	releaseError := errors.New("unlock timed out")
	tests := []struct {
		name      string
		run       func(service *Migrator) (int, error)
		applied   []AppliedMigration
		runError  error
		wantCount int
	}{
		{
			name:      "up keeps the applied count",
			run:       func(service *Migrator) (int, error) { return service.Up(t.Context()) },
			wantCount: 3,
		},
		{
			name:      "down keeps the rolled-back count",
			run:       func(service *Migrator) (int, error) { return service.Down(t.Context(), 2) },
			applied:   appliedFromReader(threeFileMigrationReader(), 1, 2, 3),
			wantCount: 2,
		},
		{
			name:      "a migration failure is kept alongside the release failure",
			run:       func(service *Migrator) (int, error) { return service.Up(t.Context()) },
			runError:  errors.New("statement failed"),
			wantCount: 0,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			executor := &mockMigrationExecutor{
				appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
					return testCase.applied, nil
				},
				releaseLockFn: func(_ context.Context) error {
					return releaseError
				},
				executeMigrationFn: func(_ context.Context, _ MigrationRecord, _ MigrationDirection, _ bool) error {
					return testCase.runError
				},
			}

			count, err := testCase.run(setupMigrationService(t, executor, threeFileMigrationReader()))

			require.ErrorIs(t, err, releaseError)
			require.ErrorContains(t, err, "releasing migration lock")
			if testCase.runError != nil {
				require.ErrorIs(t, err, testCase.runError)
				require.ErrorAs(t, err, new(*MigrationExecutionError))
			}
			require.Equal(t, testCase.wantCount, count)
		})
	}
}

func TestMigrationService_DirtyRetryRunsThroughTheHookPipeline(t *testing.T) {
	t.Parallel()

	twoFileReader := func() *fakefs.FileReader {
		reader := threeFileMigrationReader()
		reader.Directories["/migrations"] = reader.Directories["/migrations"][:4]
		return reader
	}
	dirtyFirst := func() []AppliedMigration {
		applied := appliedFromReader(threeFileMigrationReader(), 1)
		applied[0].Dirty = true
		applied[0].LastStatement = new(0)
		return applied
	}
	dirtySecond := func() []AppliedMigration {
		applied := appliedFromReader(threeFileMigrationReader(), 2)
		applied[0].Dirty = true
		return applied
	}

	tests := []struct {
		name        string
		reader      *fakefs.FileReader
		applied     []AppliedMigration
		wantCount   int
		wantEvents  []string
		wantErrType any
	}{
		{
			name: "dirty migration is the only pending one",
			reader: func() *fakefs.FileReader {
				reader := threeFileMigrationReader()
				reader.Directories["/migrations"] = reader.Directories["/migrations"][:2]
				return reader
			}(),
			applied:   dirtyFirst(),
			wantCount: 1,
			wantEvents: []string{
				"before-run [1] 1",
				"before 1", "execute 1 skip 0", "after 1",
				"after-run [1] 1 applied 1",
			},
		},
		{
			name:      "dirty migration followed by a pending one",
			reader:    twoFileReader(),
			applied:   dirtyFirst(),
			wantCount: 2,
			wantEvents: []string{
				"before-run [1 2] 2",
				"before 1", "execute 1 skip 0", "after 1",
				"before 2", "execute 2 skip -1", "after 2",
				"after-run [1 2] 2 applied 2",
			},
		},
		{
			name:        "dirty migration behind an earlier pending one blocks the run",
			reader:      twoFileReader(),
			applied:     dirtySecond(),
			wantEvents:  nil,
			wantErrType: &DirtyMigrationError{},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var events []string
			executor := &mockMigrationExecutor{
				appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
					return testCase.applied, nil
				},
				executeMigrationFn: func(_ context.Context, migration MigrationRecord, _ MigrationDirection, _ bool) error {
					events = append(events, fmt.Sprintf("execute %d skip %d", migration.Version, migration.SkipUpTo))
					return nil
				},
			}
			service := setupMigrationService(t, executor, testCase.reader,
				WithBeforeRun(func(_ context.Context, hook MigrationRunHookContext) error {
					events = append(events, fmt.Sprintf("before-run %v %d", hook.PendingVersions, hook.PendingCount))
					return nil
				}),
				WithBeforeMigration(func(_ context.Context, hook MigrationHookContext) error {
					events = append(events, fmt.Sprintf("before %d", hook.Version))
					return nil
				}),
				WithAfterMigration(func(_ context.Context, hook MigrationHookContext) error {
					events = append(events, fmt.Sprintf("after %d", hook.Version))
					return nil
				}),
				WithAfterRun(func(_ context.Context, hook MigrationRunHookContext, applied int) error {
					events = append(events, fmt.Sprintf("after-run %v %d applied %d", hook.PendingVersions, hook.PendingCount, applied))
					return nil
				}),
			)

			count, err := service.Up(t.Context())

			if testCase.wantErrType != nil {
				require.ErrorAs(t, err, &testCase.wantErrType)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, testCase.wantCount, count)
			require.Equal(t, testCase.wantEvents, events)
		})
	}
}

func TestMigrationService_CallsRunOneAtATime(t *testing.T) {
	t.Parallel()

	var active, peak atomic.Int32
	enter := func() {
		current := active.Add(1)
		for {
			observed := peak.Load()
			if current <= observed || peak.CompareAndSwap(observed, current) {
				break
			}
		}
		time.Sleep(time.Millisecond)
	}
	leave := func() { active.Add(-1) }

	executor := &mockMigrationExecutor{
		appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
			enter()
			defer leave()
			return nil, nil
		},
		executeMigrationFn: func(_ context.Context, _ MigrationRecord, _ MigrationDirection, _ bool) error {
			enter()
			defer leave()
			return nil
		},
	}
	service := setupMigrationService(t, executor, threeFileMigrationReader())

	const callers = 8
	results := make(chan error, callers)
	for index := range callers {
		go func() {
			switch index % 3 {
			case 0:
				_, err := service.Up(t.Context())
				results <- err
			case 1:
				_, err := service.Status(t.Context())
				results <- err
			default:
				results <- service.Validate(t.Context())
			}
		}()
	}
	for range callers {
		require.NoError(t, <-results)
	}
	require.Equal(t, int32(1), peak.Load())
}

func TestMigrationService_WaitingCallHonoursContext(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	entered := make(chan struct{})
	executor := &mockMigrationExecutor{
		appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
			close(entered)
			<-release
			return nil, nil
		},
	}
	service := setupMigrationService(t, executor, threeFileMigrationReader())

	done := make(chan error, 1)
	go func() {
		_, err := service.Status(t.Context())
		done <- err
	}()
	<-entered

	cause := errors.New("deploy window closed")
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(cause)
	_, err := service.Up(ctx)

	require.ErrorIs(t, err, cause)
	close(release)
	require.NoError(t, <-done)
}

func TestMigrationService_DirtyRetryCarriesTheRecordedProgress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		lastStatement   *int
		splitterVersion int
		statementsHash  string
		want            MigrationRecord
	}{
		{
			name:            "progress with a digest",
			lastStatement:   new(2),
			splitterVersion: 4,
			statementsHash:  "abc123",
			want:            MigrationRecord{SkipUpTo: 2, SplitterVersion: 4, StatementsHash: "abc123", Dirty: true},
		},
		{
			name:            "progress from an earlier release",
			lastStatement:   new(1),
			splitterVersion: 0,
			statementsHash:  "",
			want:            MigrationRecord{SkipUpTo: 1, SplitterVersion: 0, StatementsHash: "", Dirty: true},
		},
		{
			name:            "no progress recorded",
			lastStatement:   nil,
			splitterVersion: 0,
			statementsHash:  "",
			want:            MigrationRecord{SkipUpTo: -1, SplitterVersion: 0, StatementsHash: "", Dirty: true},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reader := threeFileMigrationReader()
			reader.Directories["/migrations"] = reader.Directories["/migrations"][:2]
			applied := appliedFromReader(threeFileMigrationReader(), 1)
			applied[0].Dirty = true
			applied[0].LastStatement = testCase.lastStatement
			applied[0].SplitterVersion = testCase.splitterVersion
			applied[0].StatementsHash = testCase.statementsHash
			var records []MigrationRecord
			executor := &mockMigrationExecutor{
				appliedVersionsFn: func(_ context.Context) ([]AppliedMigration, error) {
					return applied, nil
				},
				executeMigrationFn: func(_ context.Context, migration MigrationRecord, _ MigrationDirection, _ bool) error {
					records = append(records, migration)
					return nil
				},
			}
			service := setupMigrationService(t, executor, reader)

			_, err := service.Up(t.Context())

			require.NoError(t, err)
			require.Len(t, records, 1)
			got := records[0]
			require.Equal(t, testCase.want, MigrationRecord{SkipUpTo: got.SkipUpTo, SplitterVersion: got.SplitterVersion, StatementsHash: got.StatementsHash, Dirty: got.Dirty})
		})
	}
}

func TestMigrationService_LeavesAutoDetectionToTheExecutor(t *testing.T) {
	t.Parallel()

	reader := threeFileMigrationReader()
	reader.Directories["/migrations"] = reader.Directories["/migrations"][:2]
	reader.Files["/migrations/001_create_users.up.sql"] = []byte("CREATE INDEX CONCURRENTLY users_email ON users (email);")
	var useTransactions []bool
	executor := &mockMigrationExecutor{
		executeMigrationFn: func(_ context.Context, _ MigrationRecord, _ MigrationDirection, useTransaction bool) error {
			useTransactions = append(useTransactions, useTransaction)
			return nil
		},
	}
	service := setupMigrationService(t, executor, reader)

	_, err := service.Up(t.Context())

	require.NoError(t, err)
	require.Equal(t, []bool{true}, useTransactions, "only an explicit directive turns the transaction off; the executor applies its engine's rules")
}

func TestResumeMismatchErrorExplainsTheMismatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  ResumeMismatchError
		want string
	}{
		{name: "no digest recorded", err: ResumeMismatchError{Version: 3, Name: "boats", LastStatement: 1, SplitterVersion: 1, StatementCount: 4}, want: "recorded without the statements it covers"},
		{name: "another splitter", err: ResumeMismatchError{Version: 3, Name: "boats", LastStatement: 1, RecordedSplitterVersion: 2, SplitterVersion: 1, StatementCount: 4}, want: "splitter version 2 and the file is now split by version 1"},
		{name: "too few statements", err: ResumeMismatchError{Version: 3, Name: "boats", LastStatement: 4, RecordedSplitterVersion: 1, SplitterVersion: 1, StatementCount: 4}, want: "only 4 statements"},
		{name: "different statements", err: ResumeMismatchError{Version: 3, Name: "boats", LastStatement: 1, RecordedSplitterVersion: 1, SplitterVersion: 1, StatementCount: 4}, want: "no longer match"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			message := testCase.err.Error()
			require.Contains(t, message, "migration 3 (boats) is dirty after statement")
			require.Contains(t, message, testCase.want)
			require.Contains(t, message, "then repair its history")
			require.NotContains(t, message, "sgl ", "a library error names the operation, not a command of the CLI")
		})
	}
}

func TestMigrationServiceReadsNoTransactionWithTheExecutorsRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		content   string
		rules     *sqllex.Rules
		wantNoTxn bool
	}{
		{name: "an explicit directive", content: "-- sgl.migration(no_transaction: true)\nCREATE TABLE foo (id int);", wantNoTxn: true},
		{name: "a directive set to false", content: "-- sgl.migration(no_transaction: false)\nCREATE TABLE foo (id int);"},
		{name: "an auto-detected statement is left to the executor", content: "CREATE INDEX CONCURRENTLY idx ON users (email);"},
		{name: "directive text inside a string is not a directive", rules: new(postgres_lexical.Rules()),
			content: "SELECT 'x\n-- sgl.migration(no_transaction: true)\n';"},
		{name: "directive text inside a dollar-quoted body is not a directive", rules: new(postgres_lexical.Rules()),
			content: "CREATE FUNCTION f() RETURNS int AS $$\n-- sgl.migration(no_transaction: true)\nSELECT 1 $$ LANGUAGE sql;"},
		{name: "a hash comment is no directive line", rules: new(mysql_lexical.Rules()),
			content: "# sgl.migration(no_transaction: true)\nCREATE TABLE foo (id int);"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reader := &fakefs.FileReader{
				Directories: map[string][]os.DirEntry{"/migrations": {&fakefs.DirEntry{EntryName: "001_change.up.sql"}}},
				Files:       map[string][]byte{"/migrations/001_change.up.sql": []byte(testCase.content)},
			}
			useTransaction := false
			base := &mockMigrationExecutor{
				appliedVersionsFn: func(context.Context) ([]AppliedMigration, error) { return nil, nil },
				acquireLockFn:     func(context.Context) error { return nil },
				releaseLockFn:     func(context.Context) error { return nil },
				executeMigrationFn: func(_ context.Context, _ MigrationRecord, _ MigrationDirection, transaction bool) error {
					useTransaction = transaction
					return nil
				},
			}
			var executor ExecutorPort = base
			if testCase.rules != nil {
				executor = lexicalMigrationExecutor{mockMigrationExecutor: base, rules: *testCase.rules}
			}

			configured, err := newSettings(nil)
			require.NoError(t, err)
			_, err = newMigrator(executor, reader, "/migrations", configured).Up(t.Context())
			require.NoError(t, err)
			assert.Equal(t, !testCase.wantNoTxn, useTransaction)
		})
	}
}

func TestMigrationService_UpRefusesAPendingMigrationHoldingAnotherToolsDownSection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		marker  string
		tool    string
		applied []AppliedMigration
	}{
		{name: "a goose down section", marker: "-- +goose Down", tool: "goose"},
		{name: "a sql-migrate down section", marker: "-- +migrate Down", tool: "sql-migrate"},
		{name: "a dbmate down section", marker: "-- migrate:down", tool: "dbmate"},
		{name: "a tern down section", marker: "---- create above / drop below ----", tool: "tern"},
		{
			name: "a dirty migration about to be resumed", marker: "-- +goose Down", tool: "goose",
			applied: []AppliedMigration{{Version: 2, Name: "create_posts", Dirty: true}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			reader := threeFileMigrationReader()
			content := []byte("CREATE TABLE posts (id int);\n" + test.marker + "\nDROP TABLE posts;\n")
			reader.Files["/migrations/002_create_posts.up.sql"] = content
			applied := test.applied
			for index := range applied {
				applied[index].Checksum = testChecksum(content)
			}
			var locked, ensured bool
			var executed []int64
			executor := &mockMigrationExecutor{
				appliedVersionsFn:      func(context.Context) ([]AppliedMigration, error) { return applied, nil },
				acquireLockFn:          func(context.Context) error { locked = true; return nil },
				ensureMigrationTableFn: func(context.Context) error { ensured = true; return nil },
				executeMigrationFn: func(_ context.Context, migration MigrationRecord, _ MigrationDirection, _ bool) error {
					executed = append(executed, migration.Version)
					return nil
				},
			}

			count, err := setupMigrationService(t, executor, reader).Up(t.Context())

			var foreign *ForeignDownSectionError
			require.ErrorAs(t, err, &foreign)
			require.Equal(t, "002_create_posts.up.sql", foreign.Filename)
			require.Equal(t, test.tool, foreign.Tool)
			require.Equal(t, test.marker, foreign.Marker)
			require.Equal(t, 2, foreign.Line)
			diagnostic := sqlfile.ForeignDownSectionError(foreign.Filename, sqlfile.ForeignDownSection{Marker: test.marker, Tool: test.tool, Line: 2})
			require.Equal(t, diagnostic.Message+"; "+diagnostic.Suggestion, err.Error())
			require.Zero(t, count)
			require.False(t, locked, "the run refuses before it takes the lock")
			require.False(t, ensured, "the run refuses before it creates the history table")
			require.Empty(t, executed, "the run refuses before it runs any migration")
		})
	}
}

func TestMigrationService_UpRunsPastAnAppliedMigrationHoldingAnotherToolsDownSection(t *testing.T) {
	t.Parallel()

	reader := threeFileMigrationReader()
	reader.Files["/migrations/002_create_posts.up.sql"] = []byte("CREATE TABLE posts (id int);\n-- +goose Down\nDROP TABLE posts;\n")
	applied := appliedFromReader(reader, 1, 2)
	var executed []int64
	executor := &mockMigrationExecutor{
		appliedVersionsFn: func(context.Context) ([]AppliedMigration, error) { return applied, nil },
		executeMigrationFn: func(_ context.Context, migration MigrationRecord, _ MigrationDirection, _ bool) error {
			executed = append(executed, migration.Version)
			return nil
		},
	}

	count, err := setupMigrationService(t, executor, reader).Up(t.Context())

	require.NoError(t, err, "a migration applied before cannot be undone again by refusing it now")
	require.Equal(t, 1, count)
	require.Equal(t, []int64{3}, executed)
}

func TestMigrationService_UpToLeavesAForeignDownSectionBeyondTheTargetAlone(t *testing.T) {
	t.Parallel()

	reader := threeFileMigrationReader()
	reader.Files["/migrations/003_add_email.up.sql"] = []byte("ALTER TABLE users ADD COLUMN email text;\n-- migrate:down\nALTER TABLE users DROP COLUMN email;\n")
	var executed []int64
	executor := &mockMigrationExecutor{
		executeMigrationFn: func(_ context.Context, migration MigrationRecord, _ MigrationDirection, _ bool) error {
			executed = append(executed, migration.Version)
			return nil
		},
	}

	count, err := setupMigrationService(t, executor, reader).UpTo(t.Context(), 2)

	require.NoError(t, err)
	require.Equal(t, 2, count)
	require.Equal(t, []int64{1, 2}, executed)
}

func TestMigrationService_DownRefusesWhileAMigrationIsDirty(t *testing.T) {
	t.Parallel()

	noTransactionReader := threeFileMigrationReader()
	noTransactionReader.Files["/migrations/003_add_email.down.sql"] = []byte(
		"-- sgl.migration(no_transaction: true)\nALTER TABLE users DROP COLUMN email;")

	tests := []struct {
		roll          func(ctx context.Context, service *Migrator) (int, error)
		reader        *fakefs.FileReader
		name          string
		lastStatement *int
		wantLast      int
	}{
		{
			name:          "Down over a transactional down file",
			reader:        threeFileMigrationReader(),
			lastStatement: new(0),
			wantLast:      0,
			roll:          func(ctx context.Context, service *Migrator) (int, error) { return service.Down(ctx, 1) },
		},
		{
			name:          "Down over a no-transaction down file",
			reader:        noTransactionReader,
			lastStatement: new(-1),
			wantLast:      -1,
			roll:          func(ctx context.Context, service *Migrator) (int, error) { return service.Down(ctx, 1) },
		},
		{
			name:          "DownTo over a transactional down file",
			reader:        threeFileMigrationReader(),
			lastStatement: nil,
			wantLast:      -1,
			roll:          func(ctx context.Context, service *Migrator) (int, error) { return service.DownTo(ctx, 0) },
		},
		{
			name:          "DownTo over a no-transaction down file",
			reader:        noTransactionReader,
			lastStatement: new(0),
			wantLast:      0,
			roll:          func(ctx context.Context, service *Migrator) (int, error) { return service.DownTo(ctx, 1) },
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			applied := appliedFromReader(testCase.reader, 1, 2, 3)
			applied[2].Dirty = true
			applied[2].LastStatement = testCase.lastStatement
			executed, hooked := false, false
			var locks, releases atomic.Int32
			executor := &mockMigrationExecutor{
				appliedVersionsFn: func(context.Context) ([]AppliedMigration, error) { return applied, nil },
				acquireLockFn: func(context.Context) error {
					locks.Add(1)
					return nil
				},
				releaseLockFn: func(context.Context) error {
					releases.Add(1)
					return nil
				},
				executeMigrationFn: func(context.Context, MigrationRecord, MigrationDirection, bool) error {
					executed = true
					return nil
				},
			}
			service := setupMigrationService(t, executor, testCase.reader,
				WithBeforeRun(func(context.Context, MigrationRunHookContext) error {
					hooked = true
					return nil
				}))

			count, err := testCase.roll(t.Context(), service)

			require.ErrorIs(t, err, ErrDirtyRollback)
			var dirtyError *DirtyMigrationError
			require.ErrorAs(t, err, &dirtyError)
			assert.Equal(t, &DirtyMigrationError{Version: 3, LastStatement: testCase.wantLast}, dirtyError)
			assert.Zero(t, count)
			assert.False(t, executed, "no down file runs over a dirty migration")
			assert.False(t, hooked, "no hook runs")
			assert.Equal(t, int32(1), locks.Load())
			assert.Equal(t, int32(1), releases.Load())
		})
	}
}

func TestMigrationService_DownRefusesNegativeStepsBeforeTakingTheLock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		steps int
	}{
		{name: "minus one", steps: -1},
		{name: "the smallest int", steps: math.MinInt},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			touched := false
			executor := &mockMigrationExecutor{
				acquireLockFn: func(context.Context) error {
					touched = true
					return nil
				},
				appliedVersionsFn: func(context.Context) ([]AppliedMigration, error) {
					touched = true
					return nil, nil
				},
			}
			service := setupMigrationService(t, executor, threeFileMigrationReader())

			count, err := service.Down(t.Context(), testCase.steps)

			require.ErrorIs(t, err, ErrInvalidConfig)
			assert.ErrorContains(t, err, fmt.Sprintf("Down: steps must not be negative, got %d", testCase.steps))
			assert.Zero(t, count)
			assert.False(t, touched)
		})
	}
}

func TestMigrationService_RunsStopWithTheCauseAndHowFarTheyGot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		run     func(service *Migrator, ctx context.Context) (int, error)
		name    string
		applied []int64
		want    string
	}{
		{
			name: "Up",
			run:  (*Migrator).Up,
			want: "migration run stopped after 1 of 3 migrations",
		},
		{
			name:    "Down",
			run:     func(service *Migrator, ctx context.Context) (int, error) { return service.Down(ctx, 3) },
			applied: []int64{1, 2, 3},
			want:    "rollback stopped after 1 of 3 migrations",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cause := errors.New("deploy window closed")
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			reader := threeFileMigrationReader()
			applied := appliedFromReader(reader, testCase.applied...)
			executor := &mockMigrationExecutor{
				appliedVersionsFn: func(context.Context) ([]AppliedMigration, error) { return applied, nil },
				executeMigrationFn: func(context.Context, MigrationRecord, MigrationDirection, bool) error {
					cancel(cause)
					return nil
				},
			}
			service := setupMigrationService(t, executor, reader)

			count, err := testCase.run(service, ctx)

			require.ErrorIs(t, err, cause)
			require.ErrorIs(t, err, context.Canceled)
			assert.ErrorContains(t, err, testCase.want)
			assert.Equal(t, 1, count)
		})
	}
}

func TestCallGateReportsAnEndedContextTheSameWayWhetherOrNotItIsFree(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		busy bool
	}{
		{name: "free gate", busy: false},
		{name: "busy gate", busy: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			gate := newCallGate()
			if testCase.busy {
				leave, err := gate.enter(t.Context())
				require.NoError(t, err)
				defer leave()
			}
			cause := errors.New("deploy window closed")
			ctx, cancel := context.WithCancelCause(t.Context())
			cancel(cause)

			for range 32 {
				leave, err := gate.enter(ctx)
				assert.Nil(t, leave)
				require.ErrorIs(t, err, cause)
				require.ErrorIs(t, err, context.Canceled)
				require.EqualError(t, err, "not starting the call: "+context.Canceled.Error()+": deploy window closed")
			}
		})
	}
}

func TestMigrationServiceReadsFilesWithinTheCaps(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"migrations/0001_users.up.sql":  &fstest.MapFile{Data: []byte("CREATE TABLE users (id int);")},
		"migrations/0002_posts.up.sql":  &fstest.MapFile{Data: []byte("CREATE TABLE posts (id int);")},
		"migrations/0003_tags.up.sql":   &fstest.MapFile{Data: []byte("CREATE TABLE tags (id int);")},
		"migrations/0003_tags.down.sql": &fstest.MapFile{Data: []byte("DROP TABLE tags;")},
	}
	total := int64(0)
	for _, file := range files {
		total += int64(len(file.Data))
	}

	tests := []struct {
		wantError error
		name      string
		options   []Option
	}{
		{name: "the defaults", options: nil},
		{name: "a file cap at the ceiling reads every file", options: []Option{WithMaxFileBytes(sqlfile.MaxFileBytesCeiling)}},
		{name: "as many files as the directory holds", options: []Option{WithMaxFiles(4)}},
		{name: "fewer files than the directory holds", options: []Option{WithMaxFiles(3)}, wantError: ErrLimit},
		{name: "exactly the bytes the files hold", options: []Option{WithMaxTotalBytes(total)}},
		{name: "one byte fewer than the files hold", options: []Option{WithMaxTotalBytes(total - 1)}, wantError: ErrLimit},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			migrator, err := New(&mockMigrationExecutor{}, files, "migrations", testCase.options...)
			require.NoError(t, err)

			for call := range 3 {
				statuses, statusError := migrator.Status(t.Context())
				if testCase.wantError != nil {
					require.ErrorIs(t, statusError, testCase.wantError, "call %d", call)
					continue
				}
				require.NoError(t, statusError, "each call reads within a budget of its own, call %d", call)
				require.Len(t, statuses, 3)
			}
		})
	}
}

func TestMigrationServiceRefusesAMalformedTransactionDirectiveBeforeRunningAnything(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		directive string
		down      bool
		wantFile  string
	}{
		{name: "a misspelt no_transaction in a pending migration", directive: "-- sgl.migration(no_transacton: true)", wantFile: "003_add_email.up.sql"},
		{name: "a no_transaction that is not a boolean", directive: "-- sgl.migration(no_transaction: yes)", wantFile: "003_add_email.up.sql"},
		{name: "a call left unclosed", directive: "-- sgl.migration(no_transaction: true", wantFile: "003_add_email.up.sql"},
		{name: "text after the call", directive: "-- sgl.migration(no_transaction: true) now", wantFile: "003_add_email.up.sql"},
		{name: "a misspelt role", directive: "-- sgl.migraton(no_transaction: true)", wantFile: "003_add_email.up.sql"},
		{name: "a misspelt no_transaction in a down migration", directive: "-- sgl.migration(no_transacton: true)", down: true,
			wantFile: "002_create_posts.down.sql"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reader := threeFileMigrationReader()
			applied := appliedFromReader(reader, 1)
			if testCase.down {
				reader.Files["/migrations/002_create_posts.down.sql"] = []byte(testCase.directive + "\nDROP TABLE posts;")
				applied = appliedFromReader(reader, 1, 2, 3)
			} else {
				reader.Files["/migrations/003_add_email.up.sql"] = []byte(testCase.directive + "\nALTER TABLE users ADD COLUMN email text;")
			}
			executed := 0
			beforeHooks := 0
			executor := &mockMigrationExecutor{
				appliedVersionsFn: func(context.Context) ([]AppliedMigration, error) { return applied, nil },
				acquireLockFn:     func(context.Context) error { return nil },
				releaseLockFn:     func(context.Context) error { return nil },
				executeMigrationFn: func(context.Context, MigrationRecord, MigrationDirection, bool) error {
					executed++
					return nil
				},
			}
			service := setupMigrationService(t, executor, reader, WithBeforeRun(func(context.Context, MigrationRunHookContext) error {
				beforeHooks++
				return nil
			}))

			var count int
			var err error
			if testCase.down {
				count, err = service.Down(t.Context(), 3)
			} else {
				count, err = service.Up(t.Context())
			}

			require.ErrorIs(t, err, ErrMalformedDirective)
			assert.Contains(t, err.Error(), testCase.wantFile)
			assert.Contains(t, err.Error(), "line 1")
			assert.Zero(t, count)
			assert.Zero(t, executed, "no migration runs")
			assert.Zero(t, beforeHooks, "no hook runs")
		})
	}
}

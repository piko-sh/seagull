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
	"bytes"
	"context"
	"log/slog"
	"math"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/observe"
	"seagull.sh/seagull/internal/sqlfile"
)

type stageRecorder struct {
	stages []observe.Stage
}

type faultyObserver struct {
	start func(ctx context.Context) (context.Context, func(err error))
}

func (r *stageRecorder) Start(ctx context.Context, stage observe.Stage, _ ...slog.Attr) (context.Context, func(error)) {
	r.stages = append(r.stages, stage)
	return ctx, func(error) {}
}

func (observer faultyObserver) Start(ctx context.Context, _ observe.Stage, _ ...slog.Attr) (context.Context, func(err error)) {
	return observer.start(ctx)
}

func TestLoggerAndObserverOptionsReachEveryCall(t *testing.T) {
	t.Parallel()

	recorder := &stageRecorder{}
	files := fstest.MapFS{
		"migrations/0001_init.up.sql": &fstest.MapFile{Data: []byte("CREATE TABLE t (id int);")},
		"seeds/0001_rows.sql":         &fstest.MapFile{Data: []byte("INSERT INTO t VALUES (1);")},
	}
	options := []Option{
		WithLogger(slog.New(slog.NewTextHandler(new(bytes.Buffer), &slog.HandlerOptions{Level: slog.LevelDebug}))),
		WithObserver(recorder),
	}

	migrator, err := New(&mockMigrationExecutor{}, files, "migrations", options...)
	require.NoError(t, err)
	_, err = migrator.Status(t.Context())
	require.NoError(t, err)
	require.NoError(t, migrator.Validate(t.Context()))

	seeder, err := NewSeeder(&mockSeedExecutor{}, files, "seeds", options...)
	require.NoError(t, err)
	_, err = seeder.Status(t.Context())
	require.NoError(t, err)

	assert.Equal(t, []observe.Stage{observe.StageMigrateStatus, observe.StageMigrateValidate, observe.StageSeedStatus}, recorder.stages)
}

func TestWithMaxFileBytesCapsMigrationFiles(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{"migrations/0001_init.up.sql": &fstest.MapFile{Data: []byte("CREATE TABLE t (id int);")}}
	migrator, err := New(&mockMigrationExecutor{}, files, "migrations", WithMaxFileBytes(4))
	require.NoError(t, err)

	_, err = migrator.Status(t.Context())
	require.ErrorIs(t, err, ErrFileTooLarge)
}

func TestWithLockTimeoutRejectsANegativeTimeout(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{"migrations/0001_init.up.sql": &fstest.MapFile{Data: []byte("CREATE TABLE t (id int);")}}
	_, err := New(&mockMigrationExecutor{}, files, "migrations", WithLockTimeout(-time.Second))
	require.ErrorIs(t, err, ErrInvalidConfig)
	assert.ErrorContains(t, err, "WithLockTimeout: timeout must not be negative, got -1s")

	_, err = New(&mockMigrationExecutor{}, files, "migrations", WithLockTimeout(0))
	require.NoError(t, err, "zero waits until the context ends")
}

func TestFaultyObserverNeverFailsAMigrationRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		start func(ctx context.Context) (context.Context, func(err error))
		name  string
	}{
		{name: "nil end function", start: func(ctx context.Context) (context.Context, func(err error)) { return ctx, nil }},
		{name: "panicking start", start: func(context.Context) (context.Context, func(err error)) { panic("start failed") }},
		{name: "panicking end function", start: func(ctx context.Context) (context.Context, func(err error)) {
			return ctx, func(error) { panic("end failed") }
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files := fstest.MapFS{
				"migrations/0001_init.up.sql": &fstest.MapFile{Data: []byte("CREATE TABLE t (id int);")},
				"seeds/001_rows.sql":          &fstest.MapFile{Data: []byte("INSERT INTO t VALUES (1);")},
			}
			option := WithObserver(faultyObserver{start: testCase.start})
			migrator, err := New(&mockMigrationExecutor{}, files, "migrations", option)
			require.NoError(t, err)
			seeder, err := NewSeeder(&mockSeedExecutor{}, files, "seeds", option)
			require.NoError(t, err)

			require.NotPanics(t, func() {
				_, statusError := migrator.Status(t.Context())
				require.NoError(t, statusError)
				require.NoError(t, migrator.Validate(t.Context()))
				applied, upError := migrator.Up(t.Context())
				require.NoError(t, upError)
				assert.Equal(t, 1, applied)
				_, seedError := seeder.Apply(t.Context())
				require.NoError(t, seedError)
			})
		})
	}
}

func TestNewRefusesANilHook(t *testing.T) {
	t.Parallel()

	tests := []struct {
		option Option
		name   string
		want   string
	}{
		{name: "before migration", option: WithBeforeMigration(nil), want: "WithBeforeMigration: hook must not be nil"},
		{name: "after migration", option: WithAfterMigration(nil), want: "WithAfterMigration: hook must not be nil"},
		{name: "before run", option: WithBeforeRun(nil), want: "WithBeforeRun: hook must not be nil"},
		{name: "after run", option: WithAfterRun(nil), want: "WithAfterRun: hook must not be nil"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			migrator, err := New(&mockMigrationExecutor{}, fstest.MapFS{}, "migrations", testCase.option)
			require.ErrorIs(t, err, ErrInvalidConfig)
			assert.ErrorContains(t, err, testCase.want)
			assert.Nil(t, migrator)

			seeder, err := NewSeeder(&mockSeedExecutor{}, fstest.MapFS{}, "seeds", testCase.option)
			require.ErrorIs(t, err, ErrInvalidConfig)
			assert.Nil(t, seeder)
		})
	}
}

func TestNewValidatesTheCapsOnTheFilesACallReads(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		option    Option
		wantError string
	}{
		{name: "the largest file cap", option: WithMaxFileBytes(sqlfile.MaxFileBytesCeiling)},
		{name: "a file cap past the ceiling", option: WithMaxFileBytes(sqlfile.MaxFileBytesCeiling + 1), wantError: "WithMaxFileBytes: limit must be at most 1099511627776, got 1099511627777"},
		{name: "a file cap meaning no cap", option: WithMaxFileBytes(math.MaxInt64), wantError: "WithMaxFileBytes: limit must be at most 1099511627776, got 9223372036854775807"},
		{name: "one file", option: WithMaxFiles(1)},
		{name: "the largest file count", option: WithMaxFiles(math.MaxInt)},
		{name: "no files", option: WithMaxFiles(0), wantError: "WithMaxFiles: limit must be positive, got 0"},
		{name: "a negative file count", option: WithMaxFiles(-1), wantError: "WithMaxFiles: limit must be positive, got -1"},
		{name: "one byte in total", option: WithMaxTotalBytes(1)},
		{name: "the largest total", option: WithMaxTotalBytes(math.MaxInt64)},
		{name: "no bytes in total", option: WithMaxTotalBytes(0), wantError: "WithMaxTotalBytes: limit must be positive, got 0"},
		{name: "a negative total", option: WithMaxTotalBytes(-1), wantError: "WithMaxTotalBytes: limit must be positive, got -1"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			migrator, err := New(&mockMigrationExecutor{}, fstest.MapFS{}, "migrations", testCase.option)
			seeder, seedError := NewSeeder(&mockSeedExecutor{}, fstest.MapFS{}, "seeds", testCase.option)
			if testCase.wantError == "" {
				require.NoError(t, err)
				require.NoError(t, seedError)
				return
			}
			require.ErrorIs(t, err, ErrInvalidConfig)
			assert.ErrorContains(t, err, testCase.wantError)
			assert.Nil(t, migrator)
			require.ErrorIs(t, seedError, ErrInvalidConfig)
			assert.Nil(t, seeder)
		})
	}
}

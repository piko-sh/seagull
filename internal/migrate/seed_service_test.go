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
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fakefs"
)

type tryingSeedExecutor struct {
	mockSeedExecutor
	tryError error
	tried    int
}

func (executor *tryingSeedExecutor) TryAcquireSeedLock(context.Context) error {
	executor.tried++
	return executor.tryError
}

func threeSeedFileReader() *fakefs.FileReader {
	return &fakefs.FileReader{
		Directories: map[string][]os.DirEntry{
			"seeds": {
				&fakefs.DirEntry{EntryName: "001_authors.sql"},
				&fakefs.DirEntry{EntryName: "002_posts.sql"},
				&fakefs.DirEntry{EntryName: "003_comments.sql"},
			},
		},
		Files: map[string][]byte{
			"seeds/001_authors.sql":  []byte("INSERT INTO authors (name) VALUES ('Alice');"),
			"seeds/002_posts.sql":    []byte("INSERT INTO posts (title) VALUES ('Hello');"),
			"seeds/003_comments.sql": []byte("INSERT INTO comments (body) VALUES ('Nice');"),
		},
	}
}

func emptySeedFileReader() *fakefs.FileReader {
	return &fakefs.FileReader{
		Directories: map[string][]os.DirEntry{
			"seeds": {},
		},
		Files: map[string][]byte{},
	}
}

func TestSeedService_Apply_NoPendingSeeds(t *testing.T) {
	reader := emptySeedFileReader()
	executor := &mockSeedExecutor{}
	service := newSeeder(executor, reader, "seeds", defaultSettings(t))

	applied, err := service.Apply(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 0, applied)
}

func TestSeedService_Apply_AcquiresAndReleasesSeedLock(t *testing.T) {
	reader := threeSeedFileReader()
	var acquireCount, releaseCount, executeCount atomic.Int32
	var heldDuringExecute atomic.Bool
	executor := &mockSeedExecutor{
		acquireSeedLockFn: func(_ context.Context) error {
			acquireCount.Add(1)
			heldDuringExecute.Store(true)
			return nil
		},
		releaseSeedLockFn: func(_ context.Context) error {
			heldDuringExecute.Store(false)
			releaseCount.Add(1)
			return nil
		},
		executeSeedFn: func(_ context.Context, _ SeedRecord) error {
			require.True(t, heldDuringExecute.Load(),
				"seed lock must be held while ExecuteSeed runs")
			executeCount.Add(1)
			return nil
		},
	}
	service := newSeeder(executor, reader, "seeds", defaultSettings(t))

	applied, err := service.Apply(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 3, applied)
	assert.Equal(t, int32(1), acquireCount.Load(), "lock must be acquired exactly once per Apply")
	assert.Equal(t, int32(1), releaseCount.Load(), "lock must be released exactly once per Apply")
	assert.Equal(t, int32(3), executeCount.Load(), "all three pending seeds should execute under the lock")
}

func TestSeedService_Apply_ReleasesLockOnExecutionError(t *testing.T) {
	reader := threeSeedFileReader()
	var releaseCount atomic.Int32
	expectedError := errors.New("boom")
	executor := &mockSeedExecutor{
		releaseSeedLockFn: func(_ context.Context) error {
			releaseCount.Add(1)
			return nil
		},
		executeSeedFn: func(_ context.Context, _ SeedRecord) error {
			return expectedError
		},
	}
	service := newSeeder(executor, reader, "seeds", defaultSettings(t))

	_, err := service.Apply(t.Context())
	require.Error(t, err)
	assert.Equal(t, int32(1), releaseCount.Load(),
		"lock must always be released, even when a seed fails")
}

func TestSeedService_Apply_SurfacesAcquireLockError(t *testing.T) {
	reader := threeSeedFileReader()
	expectedError := errors.New("lock unavailable")
	executor := &mockSeedExecutor{
		acquireSeedLockFn: func(_ context.Context) error {
			return expectedError
		},
	}
	service := newSeeder(executor, reader, "seeds", defaultSettings(t))

	_, err := service.Apply(t.Context())
	require.Error(t, err)
	assert.ErrorIs(t, err, expectedError)
}

func TestSeederReportsASeedLockFailureOnceAsALockAcquisitionError(t *testing.T) {
	t.Parallel()

	lockFailure := errors.New("lock unavailable")
	tests := []struct {
		run  func(seeder *Seeder, ctx context.Context) (int, error)
		name string
	}{
		{name: "Apply", run: (*Seeder).Apply},
		{name: "Reseed", run: (*Seeder).Reseed},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			executed := false
			executor := &mockSeedExecutor{
				acquireSeedLockFn: func(context.Context) error { return lockFailure },
				executeSeedFn: func(context.Context, SeedRecord) error {
					executed = true
					return nil
				},
			}
			seeder := newSeeder(executor, threeSeedFileReader(), "seeds", defaultSettings(t))

			count, err := testCase.run(seeder, t.Context())

			var lockError *LockAcquisitionError
			require.ErrorAs(t, err, &lockError)
			assert.Equal(t, lockSeed, lockError.Lock)
			assert.ErrorIs(t, err, lockFailure)
			assert.EqualError(t, err, "acquiring seed lock: lock unavailable")
			assert.Zero(t, count)
			assert.False(t, executed)
		})
	}
}

func TestSeederStopsBetweenSeedsWithTheCauseAndHowFarItGot(t *testing.T) {
	t.Parallel()

	cause := errors.New("deploy window closed")
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	executed := 0
	executor := &mockSeedExecutor{
		executeSeedFn: func(context.Context, SeedRecord) error {
			executed++
			cancel(cause)
			return nil
		},
	}
	seeder := newSeeder(executor, threeSeedFileReader(), "seeds", defaultSettings(t))

	count, err := seeder.Apply(ctx)

	require.ErrorIs(t, err, cause)
	require.ErrorIs(t, err, context.Canceled)
	assert.ErrorContains(t, err, "seed run stopped after 1 of 3 seeds")
	assert.Equal(t, 1, count)
	assert.Equal(t, 1, executed)
}

func TestSeedService_Apply_ThreePendingSeeds(t *testing.T) {
	reader := threeSeedFileReader()
	var executedVersions []int64
	executor := &mockSeedExecutor{
		executeSeedFn: func(_ context.Context, seed SeedRecord) error {
			executedVersions = append(executedVersions, seed.Version)
			return nil
		},
	}
	service := newSeeder(executor, reader, "seeds", defaultSettings(t))

	applied, err := service.Apply(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 3, applied)
	assert.Equal(t, []int64{1, 2, 3}, executedVersions)
}

func TestSeedService_Apply_SkipsAlreadyApplied(t *testing.T) {
	reader := threeSeedFileReader()
	content1 := reader.Files["seeds/001_authors.sql"]
	content2 := reader.Files["seeds/002_posts.sql"]

	var executedVersions []int64
	executor := &mockSeedExecutor{
		appliedSeedsFn: func(_ context.Context) ([]AppliedSeed, error) {
			return []AppliedSeed{
				{
					Version:   1,
					Name:      "authors",
					Checksum:  testChecksum(content1),
					AppliedAt: time.Now(),
				},
				{
					Version:   2,
					Name:      "posts",
					Checksum:  testChecksum(content2),
					AppliedAt: time.Now(),
				},
			}, nil
		},
		executeSeedFn: func(_ context.Context, seed SeedRecord) error {
			executedVersions = append(executedVersions, seed.Version)
			return nil
		},
	}
	service := newSeeder(executor, reader, "seeds", defaultSettings(t))

	applied, err := service.Apply(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, applied)
	assert.Equal(t, []int64{3}, executedVersions)
}

func TestSeedService_Apply_WarnsOnChecksumMismatch(t *testing.T) {
	reader := threeSeedFileReader()
	executor := &mockSeedExecutor{
		appliedSeedsFn: func(_ context.Context) ([]AppliedSeed, error) {
			return []AppliedSeed{
				{
					Version:   1,
					Name:      "authors",
					Checksum:  "stale-checksum-that-does-not-match",
					AppliedAt: time.Now(),
				},
			}, nil
		},
	}
	service := newSeeder(executor, reader, "seeds", defaultSettings(t))

	applied, err := service.Apply(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 2, applied)
}

func TestSeedService_Apply_ExecutionError(t *testing.T) {
	reader := threeSeedFileReader()
	expectedError := errors.New("database is read-only")
	executor := &mockSeedExecutor{
		executeSeedFn: func(_ context.Context, seed SeedRecord) error {
			if seed.Version == 2 {
				return expectedError
			}
			return nil
		},
	}
	service := newSeeder(executor, reader, "seeds", defaultSettings(t))

	applied, err := service.Apply(t.Context())
	assert.Equal(t, 1, applied)
	require.Error(t, err)

	var seedError *SeedExecutionError
	require.ErrorAs(t, err, &seedError)
	assert.Equal(t, int64(2), seedError.Version)
	assert.Equal(t, "posts", seedError.Name)
	assert.ErrorIs(t, seedError, expectedError)
}

func TestSeedService_Apply_ContextCancellation(t *testing.T) {
	reader := threeSeedFileReader()
	var callCount atomic.Int32
	executor := &mockSeedExecutor{
		executeSeedFn: func(_ context.Context, _ SeedRecord) error {
			callCount.Add(1)
			return nil
		},
	}

	ctx, cancel := context.WithCancel(t.Context())

	executor.executeSeedFn = func(_ context.Context, seed SeedRecord) error {
		callCount.Add(1)
		if seed.Version == 1 {
			cancel()
		}
		return nil
	}

	service := newSeeder(executor, reader, "seeds", defaultSettings(t))
	applied, err := service.Apply(ctx)

	assert.Equal(t, 1, applied)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestSeedService_Reseed(t *testing.T) {
	reader := threeSeedFileReader()
	var cleared bool
	var executedVersions []int64
	executor := &mockSeedExecutor{
		clearSeedHistoryFn: func(_ context.Context) error {
			cleared = true
			return nil
		},
		executeSeedFn: func(_ context.Context, seed SeedRecord) error {
			executedVersions = append(executedVersions, seed.Version)
			return nil
		},
	}
	service := newSeeder(executor, reader, "seeds", defaultSettings(t))

	applied, err := service.Reseed(t.Context())
	require.NoError(t, err)
	assert.True(t, cleared)
	assert.Equal(t, 3, applied)
	assert.Equal(t, []int64{1, 2, 3}, executedVersions)
}

func TestSeedService_Status(t *testing.T) {
	reader := threeSeedFileReader()
	content1 := reader.Files["seeds/001_authors.sql"]
	executor := &mockSeedExecutor{
		appliedSeedsFn: func(_ context.Context) ([]AppliedSeed, error) {
			return []AppliedSeed{
				{
					Version:   1,
					Name:      "authors",
					Checksum:  testChecksum(content1),
					AppliedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				},
			}, nil
		},
	}
	service := newSeeder(executor, reader, "seeds", defaultSettings(t))

	statuses, err := service.Status(t.Context())
	require.NoError(t, err)
	require.Len(t, statuses, 3)

	assert.True(t, statuses[0].Applied)
	assert.True(t, statuses[0].ChecksumMatch)
	assert.Equal(t, int64(1), statuses[0].Version)

	assert.False(t, statuses[1].Applied)
	assert.True(t, statuses[1].ChecksumMatch)
	assert.Equal(t, int64(2), statuses[1].Version)

	assert.False(t, statuses[2].Applied)
	assert.Equal(t, int64(3), statuses[2].Version)
}

func TestSeedService_Status_ChecksumMismatch(t *testing.T) {
	reader := threeSeedFileReader()
	executor := &mockSeedExecutor{
		appliedSeedsFn: func(_ context.Context) ([]AppliedSeed, error) {
			return []AppliedSeed{
				{
					Version:  1,
					Name:     "authors",
					Checksum: "old-checksum",
				},
			}, nil
		},
	}
	service := newSeeder(executor, reader, "seeds", defaultSettings(t))

	statuses, err := service.Status(t.Context())
	require.NoError(t, err)
	require.Len(t, statuses, 3)

	assert.True(t, statuses[0].Applied)
	assert.False(t, statuses[0].ChecksumMatch)
}

func TestSeedService_StatusIsReadOnly(t *testing.T) {
	ensured := false
	executor := &mockSeedExecutor{
		ensureSeedTableFn: func(_ context.Context) error {
			ensured = true
			return nil
		},
	}
	service := newSeeder(executor, threeSeedFileReader(), "seeds", defaultSettings(t))

	statuses, err := service.Status(t.Context())

	require.NoError(t, err)
	require.Len(t, statuses, 3)
	require.False(t, ensured)
}

func TestSeedService_ReleaseErrorIsReturned(t *testing.T) {
	releaseError := errors.New("unlock timed out")
	tests := []struct {
		name string
		run  func(service *Seeder) (int, error)
	}{
		{name: "apply", run: func(service *Seeder) (int, error) { return service.Apply(t.Context()) }},
		{name: "reseed", run: func(service *Seeder) (int, error) { return service.Reseed(t.Context()) }},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			executor := &mockSeedExecutor{
				releaseSeedLockFn: func(_ context.Context) error { return releaseError },
			}
			service := newSeeder(executor, threeSeedFileReader(), "seeds", defaultSettings(t))

			applied, err := testCase.run(service)

			require.ErrorIs(t, err, releaseError)
			require.ErrorContains(t, err, "releasing seed lock")
			require.Equal(t, 3, applied)
		})
	}
}

func TestSeedService_CallsRunOneAtATime(t *testing.T) {
	var active, peak atomic.Int32
	track := func() {
		current := active.Add(1)
		for {
			observed := peak.Load()
			if current <= observed || peak.CompareAndSwap(observed, current) {
				break
			}
		}
		time.Sleep(time.Millisecond)
		active.Add(-1)
	}
	executor := &mockSeedExecutor{
		appliedSeedsFn: func(_ context.Context) ([]AppliedSeed, error) {
			track()
			return nil, nil
		},
		executeSeedFn: func(_ context.Context, _ SeedRecord) error {
			track()
			return nil
		},
	}
	service := newSeeder(executor, threeSeedFileReader(), "seeds", defaultSettings(t))

	const callers = 6
	results := make(chan error, callers)
	for index := range callers {
		go func() {
			if index%2 == 0 {
				_, err := service.Apply(t.Context())
				results <- err
				return
			}
			_, err := service.Status(t.Context())
			results <- err
		}()
	}
	for range callers {
		require.NoError(t, <-results)
	}
	require.Equal(t, int32(1), peak.Load())
}

func defaultSettings(t *testing.T) settings {
	t.Helper()
	configured, err := newSettings(nil)
	require.NoError(t, err)
	return configured
}

func TestSeederWithNonBlockingLockTriesTheSeedLock(t *testing.T) {
	t.Parallel()

	waited := false
	executor := &tryingSeedExecutor{
		acquireSeedLockFn: func(context.Context) error {
			waited = true
			return nil
		},
		tryError: ErrLockNotAcquired,
	}
	configured, err := newSettings([]Option{WithNonBlockingLock()})
	require.NoError(t, err)
	service := newSeeder(executor, threeSeedFileReader(), "seeds", configured)

	_, err = service.Apply(t.Context())
	require.ErrorIs(t, err, ErrLockNotAcquired)
	_, err = service.Reseed(t.Context())
	require.ErrorIs(t, err, ErrLockNotAcquired)
	assert.Equal(t, 2, executor.tried)
	assert.False(t, waited)
}

func TestNewSeederRefusesANonBlockingLockItCannotTry(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{"seeds/001_authors.sql": &fstest.MapFile{Data: []byte("INSERT INTO authors VALUES (1);")}}
	_, err := NewSeeder(&mockSeedExecutor{}, files, "seeds", WithNonBlockingLock())
	require.ErrorIs(t, err, ErrInvalidConfig)
	assert.ErrorContains(t, err, "migrate.SeedLockTrierPort")

	_, err = NewSeeder(&tryingSeedExecutor{}, files, "seeds", WithNonBlockingLock())
	require.NoError(t, err)
}

func TestSeederWithLockTimeoutGivesUpOnTheSeedLock(t *testing.T) {
	t.Parallel()

	executor := &mockSeedExecutor{acquireSeedLockFn: func(ctx context.Context) error {
		<-ctx.Done()
		return context.Cause(ctx)
	}}
	configured, err := newSettings([]Option{WithLockTimeout(10 * time.Millisecond)})
	require.NoError(t, err)
	service := newSeeder(executor, threeSeedFileReader(), "seeds", configured)

	_, err = service.Apply(t.Context())
	require.ErrorIs(t, err, ErrLockTimeout)
	assert.ErrorContains(t, err, "acquiring seed lock: timed out waiting for the lock: the seed lock was still held elsewhere after 10ms")
}

func TestSeederReadsSeedFilesWithinTheCaps(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"seeds/001_authors.sql": &fstest.MapFile{Data: []byte("INSERT INTO authors VALUES (1);")},
		"seeds/002_posts.sql":   &fstest.MapFile{Data: []byte("INSERT INTO posts VALUES (1);")},
	}
	tests := []struct {
		wantError error
		name      string
		options   []Option
	}{
		{name: "the defaults", options: nil},
		{name: "fewer files than the directory holds", options: []Option{WithMaxFiles(1)}, wantError: ErrLimit},
		{name: "fewer bytes than the files hold", options: []Option{WithMaxTotalBytes(40)}, wantError: ErrLimit},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			seeder, err := NewSeeder(&mockSeedExecutor{}, files, "seeds", testCase.options...)
			require.NoError(t, err)

			for range 2 {
				_, statusError := seeder.Status(t.Context())
				_, applyError := seeder.Apply(t.Context())
				if testCase.wantError == nil {
					require.NoError(t, statusError)
					require.NoError(t, applyError)
					continue
				}
				require.ErrorIs(t, statusError, testCase.wantError)
				require.ErrorIs(t, applyError, testCase.wantError)
			}
		})
	}
}

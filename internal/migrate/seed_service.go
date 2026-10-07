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
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/observe"
	"seagull.sh/seagull/internal/sqlfile"
)

// Seeder applies, inspects and re-applies seed files through a SeedExecutorPort.
//
// Seeds are forward-only SQL files intended to run after migrations. Applied seeds are
// recorded in a history table and skipped on subsequent Apply calls.
//
// A Seeder is safe for concurrent use: its calls run one at a time in the process, and
// coordination across processes depends on the SeedExecutorPort's lock implementation.
type Seeder struct {
	// executor holds the database-specific seed operations.
	executor SeedExecutorPort

	// gate runs the Seeder's calls one at a time.
	gate callGate

	// fileReader reads the seed files.
	fileReader sqlfile.FileReader

	// directory holds the path to the directory containing seed files.
	directory string

	settings
}

// NewSeeder creates a Seeder over a SeedExecutorPort. For database/sql, use
// internal/migrate/sqlexec.NewSeeder.
//
// Takes executor (SeedExecutorPort) which runs the seeds against the database.
// Takes files (fs.FS) which holds the seed files, such as an embed.FS.
// Takes directory (string) which is the slash-separated directory of the seed files
// within files.
// Takes options (...Option) which configure locking, logging and limits; migration-only
// options are ignored.
//
// Returns *Seeder which is ready to run.
// Returns error which wraps ErrInvalidConfig when the executor or files are nil, the
// directory is not a valid path, an option is invalid, or WithNonBlockingLock is set for
// an executor that does not implement SeedLockTrierPort.
func NewSeeder(executor SeedExecutorPort, files fs.FS, directory string, options ...Option) (*Seeder, error) {
	if executor == nil {
		return nil, fmt.Errorf("migrate.NewSeeder needs an executor: %w", ErrInvalidConfig)
	}
	reader, cleaned, configured, err := prepare(files, directory, options)
	if err != nil {
		return nil, err
	}
	if _, tries := executor.(SeedLockTrierPort); configured.nonBlockingLock && !tries {
		return nil, fmt.Errorf("WithNonBlockingLock: the seed executor cannot take its lock without waiting; it does not implement migrate.SeedLockTrierPort: %w", ErrInvalidConfig)
	}
	return newSeeder(executor, reader, cleaned, configured), nil
}

// newSeeder assembles a Seeder from a file reader.
//
// Takes executor (SeedExecutorPort) which runs the seeds.
// Takes fileReader (sqlfile.FileReader) which reads the seed files.
// Takes directory (string) which is the seed directory.
// Takes configured (settings) which holds the configuration.
//
// Returns *Seeder which is ready to run.
func newSeeder(executor SeedExecutorPort, fileReader sqlfile.FileReader, directory string, configured settings) *Seeder {
	return &Seeder{
		executor:   executor,
		gate:       newCallGate(),
		fileReader: fileReader,
		directory:  directory,
		settings:   configured,
	}
}

// Apply executes all pending seed files in version order, skipping those already applied
// and warning on checksum mismatches.
//
// Apply holds the executor's seed lock while reading the history and executing pending
// seeds. Backends without a database lock require callers to coordinate concurrent runs;
// an idempotent history INSERT does not prevent duplicate execution of the seed SQL.
//
// Returns int which is the number of seeds applied.
// Returns error which is a *LockAcquisitionError naming the seed lock when it cannot be
// taken, or the failure of a seed, joined with any failure to release the lock.
func (s *Seeder) Apply(ctx context.Context) (_ int, err error) {
	ctx = s.contextFor(ctx)
	ctx, end := observe.From(ctx).Start(ctx, observe.StageSeedApply)
	defer func() { end(err) }()

	leave, gateError := s.gate.enter(ctx)
	if gateError != nil {
		return 0, gateError
	}
	defer leave()

	if lockError := s.acquireSeedLock(ctx); lockError != nil {
		return 0, &LockAcquisitionError{Cause: lockError, Lock: lockSeed}
	}
	defer func() { err = s.joinReleaseError(ctx, err) }()

	return s.applyLocked(ctx)
}

// Status returns the list of all known seeds and their applied state. It runs no DDL: a
// database without a seed history table reports every seed as pending.
//
// Returns []SeedStatus which lists all seeds.
// Returns error when the status cannot be determined.
func (s *Seeder) Status(ctx context.Context) (_ []SeedStatus, err error) {
	ctx = s.contextFor(ctx)
	ctx, end := observe.From(ctx).Start(ctx, observe.StageSeedStatus)
	defer func() { end(err) }()

	leave, gateError := s.gate.enter(ctx)
	if gateError != nil {
		return nil, gateError
	}
	defer leave()

	files, err := readSeedFiles(ctx, s.callReader(s.fileReader), s.directory)
	if err != nil {
		return nil, err
	}

	applied, err := s.executor.AppliedSeeds(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading applied seeds: %w", err)
	}

	appliedByVersion := buildAppliedSeedMap(applied)
	statuses := make([]SeedStatus, 0, len(files))

	for _, file := range files {
		status := SeedStatus{
			Version:       file.Version,
			Name:          file.Name,
			Filename:      file.Filename,
			Applied:       false,
			ChecksumMatch: true,
			AppliedAt:     time.Time{},
		}

		if record, ok := appliedByVersion[file.Version]; ok {
			status.Applied = true
			status.AppliedAt = record.AppliedAt
			status.ChecksumMatch = record.Checksum == file.Checksum
		}

		statuses = append(statuses, status)
	}

	return statuses, nil
}

// Reseed clears the seed history table and re-applies all seed files.
//
// Returns int which is the number of seeds applied.
// Returns error which is a *LockAcquisitionError naming the seed lock when it cannot be
// taken, or the failure to clear the history or apply a seed, joined with any failure to
// release the lock.
func (s *Seeder) Reseed(ctx context.Context) (_ int, err error) {
	ctx = s.contextFor(ctx)
	ctx, end := observe.From(ctx).Start(ctx, observe.StageSeedReseed)
	defer func() { end(err) }()

	leave, gateError := s.gate.enter(ctx)
	if gateError != nil {
		return 0, gateError
	}
	defer leave()

	if lockError := s.acquireSeedLock(ctx); lockError != nil {
		return 0, &LockAcquisitionError{Cause: lockError, Lock: lockSeed}
	}
	defer func() { err = s.joinReleaseError(ctx, err) }()

	if err := s.executor.EnsureSeedTable(ctx); err != nil {
		return 0, fmt.Errorf("ensuring seed table: %w", err)
	}

	if err := s.executor.ClearSeedHistory(ctx); err != nil {
		return 0, fmt.Errorf("clearing seed history: %w", err)
	}

	return s.applyLocked(ctx)
}

// acquireSeedLock acquires the seed lock, without waiting when WithNonBlockingLock is
// set, and waiting no longer than WithLockTimeout allows otherwise.
//
// Returns error when the lock cannot be acquired, wrapping ErrLockTimeout when the
// timeout cut the wait short.
func (s *Seeder) acquireSeedLock(ctx context.Context) error {
	return s.acquireWithin(ctx, lockSeed, func(lockContext context.Context) error {
		if trier, tries := s.executor.(SeedLockTrierPort); tries && s.nonBlockingLock {
			return trier.TryAcquireSeedLock(lockContext)
		}
		return s.executor.AcquireSeedLock(lockContext)
	})
}

// joinReleaseError releases the seed lock on a context detached from cancellation, and
// joins any release failure into the run's result.
//
// Takes runError (error) which is the run's result so far.
//
// Returns error which is runError unchanged when the release succeeds, or joined with the
// release failure otherwise.
func (s *Seeder) joinReleaseError(ctx context.Context, runError error) error {
	releaseError := s.executor.ReleaseSeedLock(context.WithoutCancel(ctx))
	if releaseError == nil {
		return runError
	}
	return errors.Join(runError, fmt.Errorf("releasing seed lock: %w", releaseError))
}

// applyLocked executes all pending seed files in version order.
//
// It skips those already applied and warns on checksum mismatches. The caller MUST
// already hold the seed lock: both Apply (a normal run) and Reseed (which clears the
// history first under the same held lock) funnel through here so the read-modify-write of
// the seed history always happens under a single lock, never split across an
// acquire/release boundary.
//
// Returns int which is the number of seeds applied.
// Returns error when a seed fails to execute.
func (s *Seeder) applyLocked(ctx context.Context) (int, error) {
	logger := logging.LoggerFrom(ctx)

	files, err := readSeedFiles(ctx, s.callReader(s.fileReader), s.directory)
	if err != nil {
		return 0, err
	}
	if len(files) == 0 {
		return 0, nil
	}

	if err := s.executor.EnsureSeedTable(ctx); err != nil {
		return 0, fmt.Errorf("ensuring seed table: %w", err)
	}

	applied, err := s.executor.AppliedSeeds(ctx)
	if err != nil {
		return 0, fmt.Errorf("reading applied seeds: %w", err)
	}

	appliedByVersion := buildAppliedSeedMap(applied)
	pending := filterPendingSeeds(ctx, files, appliedByVersion)

	count := 0
	for _, seed := range pending {
		if stopped := contextStopped(ctx); stopped != nil {
			return count, fmt.Errorf("seed run stopped after %d of %d seeds: %w", count, len(pending), stopped)
		}

		record := SeedRecord{
			Version:  seed.Version,
			Name:     seed.Name,
			Checksum: seed.Checksum,
			Content:  seed.Content,
		}

		if executionError := s.executor.ExecuteSeed(ctx, record); executionError != nil {
			return count, &SeedExecutionError{
				Version: seed.Version,
				Name:    seed.Name,
				Cause:   executionError,
			}
		}

		logger.Log(ctx, logging.LevelInternal, "seed applied",
			slog.Int64(logFieldVersion, seed.Version),
			slog.String("name", seed.Name))
		count++
	}

	return count, nil
}

// buildAppliedSeedMap creates a lookup map from version to applied seed.
//
// Takes applied ([]AppliedSeed) which holds the applied seed records.
//
// Returns map[int64]AppliedSeed which maps version numbers to their applied seed records.
func buildAppliedSeedMap(applied []AppliedSeed) map[int64]AppliedSeed {
	appliedByVersion := make(map[int64]AppliedSeed, len(applied))
	for _, seed := range applied {
		appliedByVersion[seed.Version] = seed
	}
	return appliedByVersion
}

// filterPendingSeeds returns seed files that have not yet been applied, logging warnings
// for checksum mismatches.
//
// Takes files ([]sqlfile.SeedFile) which holds all seed files to filter.
// Takes appliedByVersion (map[int64]AppliedSeed) which holds the already-applied seeds.
//
// Returns []sqlfile.SeedFile which holds only the pending seed files.
func filterPendingSeeds(
	ctx context.Context,
	files []sqlfile.SeedFile,
	appliedByVersion map[int64]AppliedSeed,
) []sqlfile.SeedFile {
	logger := logging.LoggerFrom(ctx)

	pending := make([]sqlfile.SeedFile, 0, len(files))

	for _, file := range files {
		record, ok := appliedByVersion[file.Version]
		if !ok {
			pending = append(pending, file)
			continue
		}

		if record.Checksum != file.Checksum {
			logger.WarnContext(ctx, "seed file checksum changed since last application",
				slog.Int64(logFieldVersion, file.Version),
				slog.String("name", file.Name),
				slog.String("applied_checksum", record.Checksum),
				slog.String("file_checksum", file.Checksum))
		}
	}

	return pending
}

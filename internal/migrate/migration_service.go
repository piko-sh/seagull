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
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"slices"
	"time"

	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/observe"
	"seagull.sh/seagull/internal/sqlfile"
	"seagull.sh/seagull/internal/sqllex"
)

const (
	// errorFormatEnsuringMigrationTable holds the format string for wrapping errors from
	// EnsureMigrationTable.
	errorFormatEnsuringMigrationTable = "ensuring migration table: %w"

	// errorFormatReadingAppliedVersions holds the format string for wrapping errors from
	// AppliedVersions.
	errorFormatReadingAppliedVersions = "reading applied versions: %w"

	// logFieldVersion holds the structured log field name for migration version numbers.
	logFieldVersion = "version"

	// lockMigration names the migration lock in errors and timeout causes.
	lockMigration = "migration"

	// lockSeed names the seed lock in errors and timeout causes.
	lockSeed = "seed"
)

// Migrator applies, rolls back and inspects versioned migrations through an ExecutorPort.
// It is safe for concurrent use: its calls run one at a time in the process, and the
// coordination across processes depends on the ExecutorPort's lock implementation.
type Migrator struct {
	// executor holds the database-specific migration operations.
	executor ExecutorPort

	// gate runs the Migrator's calls one at a time.
	gate callGate

	// fileReader reads the migration files.
	fileReader sqlfile.FileReader

	// directory holds the path to the directory containing migration files.
	directory string

	settings
}

// New creates a Migrator over an ExecutorPort, for databases reached through something
// other than database/sql or with history tables of their own. For database/sql, use
// internal/migrate/sqlexec.NewMigrator.
//
// Takes executor (ExecutorPort) which runs the migrations against the database.
// Takes files (fs.FS) which holds the migration files, such as an embed.FS.
// Takes directory (string) which is the slash-separated directory of the migration files
// within files.
// Takes options (...Option) which configure locking, hooks, prefixes, logging and limits.
//
// Returns *Migrator which is ready to run.
// Returns error which wraps ErrInvalidConfig when the executor or files are nil, the
// directory is not a valid path, or an option is invalid.
func New(executor ExecutorPort, files fs.FS, directory string, options ...Option) (*Migrator, error) {
	if executor == nil {
		return nil, fmt.Errorf("migrate.New needs an executor: %w", ErrInvalidConfig)
	}
	reader, cleaned, configured, err := prepare(files, directory, options)
	if err != nil {
		return nil, err
	}
	return newMigrator(executor, reader, cleaned, configured), nil
}

// newMigrator assembles a Migrator from a file reader.
//
// Takes executor (ExecutorPort) which runs the migrations.
// Takes fileReader (sqlfile.FileReader) which reads the migration files.
// Takes directory (string) which is the migration directory.
// Takes configured (settings) which holds the configuration.
//
// Returns *Migrator which is ready to run.
func newMigrator(executor ExecutorPort, fileReader sqlfile.FileReader, directory string, configured settings) *Migrator {
	return &Migrator{
		executor:   executor,
		gate:       newCallGate(),
		fileReader: fileReader,
		directory:  directory,
		settings:   configured,
	}
}

// Up applies all pending up migrations in version order.
//
// Returns int which is the number of migrations applied.
// Returns error when migration reading, checksum validation, lock acquisition, or
// execution fails.
func (service *Migrator) Up(ctx context.Context) (int, error) {
	ctx = service.contextFor(ctx)
	return service.applyUpMigrations(ctx, nil)
}

// UpTo applies pending up migrations up to and including the target version.
//
// Takes targetVersion (int64) which specifies the maximum version to apply.
//
// Returns int which is the number of migrations applied.
// Returns error when migration reading, checksum validation, lock acquisition, or
// execution fails.
func (service *Migrator) UpTo(ctx context.Context, targetVersion int64) (int, error) {
	ctx = service.contextFor(ctx)
	return service.applyUpMigrations(ctx, &targetVersion)
}

// Down rolls back the last n applied migrations in reverse version order. It rolls back
// nothing while a migration is dirty.
//
// Takes steps (int) which specifies how many migrations to roll back; it must not be
// negative.
//
// Returns int which is the number of migrations rolled back.
// Returns error which wraps ErrInvalidConfig for negative steps, wraps ErrDirtyRollback
// and a *DirtyMigrationError while a migration is dirty, or reports a failure to read the
// migrations, take the lock or run a rollback.
func (service *Migrator) Down(ctx context.Context, steps int) (int, error) {
	if steps < 0 {
		return 0, fmt.Errorf("Down: steps must not be negative, got %d: %w", steps, ErrInvalidConfig)
	}
	ctx = service.contextFor(ctx)
	return service.rollbackMigrations(ctx, &steps, nil)
}

// DownTo rolls back applied migrations down to (but not including) the target version. It
// rolls back nothing while a migration is dirty.
//
// Takes targetVersion (int64) which specifies the version to roll back to.
//
// Returns int which is the number of migrations rolled back.
// Returns error which wraps ErrDirtyRollback and a *DirtyMigrationError while a migration
// is dirty, or reports a failure to read the migrations, take the lock or run a rollback.
func (service *Migrator) DownTo(ctx context.Context, targetVersion int64) (int, error) {
	ctx = service.contextFor(ctx)
	return service.rollbackMigrations(ctx, nil, &targetVersion)
}

// Status returns the list of all known migrations and their applied state. It runs no
// DDL: a database without a history table reports every migration as pending.
//
// Returns []MigrationStatus which holds the status of each known migration.
// Returns error when reading files or querying applied versions fails.
func (service *Migrator) Status(ctx context.Context) (_ []MigrationStatus, err error) {
	ctx = service.contextFor(ctx)
	ctx, end := observe.From(ctx).Start(ctx, observe.StageMigrateStatus)
	defer func() { end(err) }()

	leave, gateError := service.gate.enter(ctx)
	if gateError != nil {
		return nil, gateError
	}
	defer leave()

	allFiles, readError := readMigrationFiles(ctx, service.callReader(service.fileReader), service.directory)
	if readError != nil {
		return nil, readError
	}

	applied, appliedError := service.executor.AppliedVersions(ctx)
	if appliedError != nil {
		return nil, fmt.Errorf(errorFormatReadingAppliedVersions, appliedError)
	}

	appliedByVersion := make(map[int64]AppliedMigration, len(applied))
	for index := range applied {
		appliedByVersion[applied[index].Version] = applied[index]
	}

	upFiles := filterByDirection(allFiles, MigrationDirectionUp)
	downVersions := buildDownVersionSet(allFiles)

	statuses := make([]MigrationStatus, 0, len(upFiles))
	for _, file := range upFiles {
		status := MigrationStatus{
			Version:          file.Version,
			Name:             file.Name,
			Filename:         file.Filename,
			HasDownMigration: downVersions[file.Version],
			AppliedAt:        time.Time{},
			LastStatement:    nil,
			Applied:          false,
			ChecksumMatch:    false,
			Dirty:            false,
		}

		if appliedMigration, found := appliedByVersion[file.Version]; found {
			status.Applied = true
			status.AppliedAt = appliedMigration.AppliedAt
			status.ChecksumMatch = appliedMigration.Checksum == file.Checksum
			status.Dirty = appliedMigration.Dirty
			status.LastStatement = appliedMigration.LastStatement
		}

		statuses = append(statuses, status)
	}

	return statuses, nil
}

// Validate checks that all applied migration checksums match their on-disk files without
// executing anything. It runs no DDL: a database without a history table validates.
//
// Returns error when file reading, version querying, or checksum validation fails.
func (service *Migrator) Validate(ctx context.Context) (err error) {
	ctx = service.contextFor(ctx)
	ctx, end := observe.From(ctx).Start(ctx, observe.StageMigrateValidate)
	defer func() { end(err) }()

	leave, gateError := service.gate.enter(ctx)
	if gateError != nil {
		return gateError
	}
	defer leave()

	allFiles, readError := readMigrationFiles(ctx, service.callReader(service.fileReader), service.directory)
	if readError != nil {
		return readError
	}

	upFiles := filterByDirection(allFiles, MigrationDirectionUp)

	applied, appliedError := service.executor.AppliedVersions(ctx)
	if appliedError != nil {
		return fmt.Errorf(errorFormatReadingAppliedVersions, appliedError)
	}

	return validateChecksums(upFiles, applied)
}

// applyUpMigrations reads migration files, validates checksums, acquires the lock,
// creates the history table, and executes all pending up migrations up to the optional
// target version.
//
// Drift (an edited or deleted applied migration) is reported before anything else, even
// when nothing is pending, and checked again under the lock in case another process
// changed the history meanwhile.
//
// Takes targetVersion (*int64) which specifies the maximum version to apply, or nil to
// apply all pending migrations.
//
// Returns int which is the number of migrations applied.
// Returns error when any step fails, joined with any failure to release the lock.
func (service *Migrator) applyUpMigrations(
	ctx context.Context,
	targetVersion *int64,
) (_ int, err error) {
	ctx, end := observe.From(ctx).Start(ctx, observe.StageMigrateUp)
	defer func() { end(err) }()

	leave, gateError := service.gate.enter(ctx)
	if gateError != nil {
		return 0, gateError
	}
	defer leave()

	allFiles, readError := readMigrationFiles(ctx, service.callReader(service.fileReader), service.directory)
	if readError != nil {
		return 0, readError
	}

	upFiles := filterByDirection(allFiles, MigrationDirectionUp)

	pending, _, pendingError := service.pendingUp(ctx, upFiles, targetVersion, errorFormatReadingAppliedVersions)
	if pendingError != nil || len(pending) == 0 {
		return 0, pendingError
	}

	if lockError := service.acquireLock(ctx); lockError != nil {
		return 0, &LockAcquisitionError{Cause: lockError, Lock: lockMigration}
	}
	defer func() { err = service.joinReleaseError(ctx, err) }()

	if ensureError := service.executor.EnsureMigrationTable(ctx); ensureError != nil {
		return 0, fmt.Errorf(errorFormatEnsuringMigrationTable, ensureError)
	}

	pending, applied, pendingError := service.pendingUp(ctx, upFiles, targetVersion, "reading applied versions under lock: %w")
	if pendingError != nil || len(pending) == 0 {
		return 0, pendingError
	}

	service.warnSkippedMigrations(ctx, pending, applied)

	return service.executePendingUp(ctx, pending, applied, buildDownChecksumMap(allFiles))
}

// pendingUp reads the applied migrations, checks them against the files, and returns the
// migrations still to apply.
//
// Takes upFiles ([]sqlfile.MigrationFile) which holds the up migration files.
// Takes targetVersion (*int64) which limits the pending migrations, or nil for all.
// Takes readErrorFormat (string) which wraps a failure to read the applied versions.
//
// Returns []sqlfile.MigrationFile which holds the pending migrations in version order.
// Returns []AppliedMigration which holds the applied migrations read.
// Returns error when the history cannot be read, or an applied migration's file was
// edited or deleted.
func (service *Migrator) pendingUp(
	ctx context.Context,
	upFiles []sqlfile.MigrationFile,
	targetVersion *int64,
	readErrorFormat string,
) ([]sqlfile.MigrationFile, []AppliedMigration, error) {
	applied, appliedError := service.executor.AppliedVersions(ctx)
	if appliedError != nil {
		return nil, nil, fmt.Errorf(readErrorFormat, appliedError)
	}

	if checksumError := validateChecksums(upFiles, applied); checksumError != nil {
		return nil, nil, checksumError
	}

	pending := filterByTargetVersion(computePending(upFiles, applied), targetVersion)
	if foreignError := refuseForeignDownSections(pending); foreignError != nil {
		return nil, nil, foreignError
	}
	if directiveError := service.refuseMalformedDirectives(pending); directiveError != nil {
		return nil, nil, directiveError
	}
	return pending, applied, nil
}

// refuseMalformedDirectives refuses migrations about to run whose sgl.migration directive
// cannot be read for certain, so none of them runs inside or outside a transaction
// against what it asked.
//
// Takes files ([]sqlfile.MigrationFile) which holds the migrations about to run.
//
// Returns error which wraps ErrMalformedDirective for the first such migration, or nil.
func (service *Migrator) refuseMalformedDirectives(files []sqlfile.MigrationFile) error {
	for index := range files {
		if _, err := service.requestsNoTransaction(files[index]); err != nil {
			return err
		}
	}
	return nil
}

// rollbackMigrations reads migration files, acquires the lock, and rolls back applied
// migrations by step count or down to a target version. It does not create the migration
// history table: a database without that table has nothing to roll back.
//
// Takes steps (*int) which specifies how many migrations to roll back, or nil to use
// targetVersion instead.
// Takes targetVersion (*int64) which specifies the version to roll back to, or nil to use
// steps instead.
//
// Returns int which is the number of migrations rolled back.
// Returns error when any step fails, joined with any failure to release the lock.
func (service *Migrator) rollbackMigrations(
	ctx context.Context,
	steps *int,
	targetVersion *int64,
) (_ int, err error) {
	ctx, end := observe.From(ctx).Start(ctx, observe.StageMigrateDown)
	defer func() { end(err) }()

	leave, gateError := service.gate.enter(ctx)
	if gateError != nil {
		return 0, gateError
	}
	defer leave()

	allFiles, readError := readMigrationFiles(ctx, service.callReader(service.fileReader), service.directory)
	if readError != nil {
		return 0, readError
	}

	downFilesByVersion := buildDownFileMap(allFiles)

	if lockError := service.acquireLock(ctx); lockError != nil {
		return 0, &LockAcquisitionError{Cause: lockError, Lock: lockMigration}
	}
	defer func() { err = service.joinReleaseError(ctx, err) }()

	applied, appliedError := service.executor.AppliedVersions(ctx)
	if appliedError != nil {
		return 0, fmt.Errorf(errorFormatReadingAppliedVersions, appliedError)
	}

	if len(applied) == 0 {
		return 0, nil
	}

	rollbackCount := computeRollbackSteps(applied, steps, targetVersion)
	if rollbackCount == 0 {
		return 0, nil
	}

	if dirtyError := refuseDirtyRollback(applied); dirtyError != nil {
		return 0, dirtyError
	}

	return service.executeRollbacks(ctx, applied, rollbackCount, downFilesByVersion)
}

// warnSkippedMigrations logs a warning for each pending migration whose version is
// earlier than the maximum applied version.
//
// Takes pending ([]sqlfile.MigrationFile) which holds the migrations to check.
// Takes applied ([]AppliedMigration) which holds the already applied migrations.
func (*Migrator) warnSkippedMigrations(
	ctx context.Context,
	pending []sqlfile.MigrationFile,
	applied []AppliedMigration,
) {
	skippedVersions := detectSkippedMigrations(pending, applied)
	if len(skippedVersions) == 0 {
		return
	}
	logger := logging.LoggerFrom(ctx)
	for _, version := range skippedVersions {
		logger.WarnContext(ctx, "applying skipped migration",
			slog.Int64(logFieldVersion, version),
		)
	}
}

// executePendingUp runs before-run hooks, applies each pending migration with its
// before/after hooks, and runs after-run hooks.
//
// A dirty migration left by an interrupted non-transactional run is retried as the first
// pending migration, resuming after its last completed statement, through the same hooks
// as every other migration. A dirty migration that is not the next pending one blocks the
// run.
//
// Takes pending ([]sqlfile.MigrationFile) which holds the migrations to apply.
// Takes applied ([]AppliedMigration) which holds the applied migrations read under the
// lock.
// Takes downChecksumsByVersion (map[int64]string) which maps versions to their
// down-migration checksums.
//
// Returns int which is the number of migrations applied.
// Returns error when any hook or migration execution fails, or a dirty migration from a
// different version blocks progress.
func (service *Migrator) executePendingUp(
	ctx context.Context,
	pending []sqlfile.MigrationFile,
	applied []AppliedMigration,
	downChecksumsByVersion map[int64]string,
) (int, error) {
	resume, dirtyError := resumePoint(ctx, pending, applied)
	if dirtyError != nil {
		return 0, dirtyError
	}

	if hookError := service.runBeforeRunHooks(ctx, pending, MigrationDirectionUp); hookError != nil {
		return 0, hookError
	}

	return service.applyPendingUpMigrations(ctx, pending, downChecksumsByVersion, resume)
}

// applyPendingUpMigrations iterates through pending migrations, running hooks and
// executing each in sequence.
//
// Takes pending ([]sqlfile.MigrationFile) which holds the migrations to apply.
// Takes downChecksumsByVersion (map[int64]string) which maps versions to their
// down-migration checksums.
// Takes resume (*AppliedMigration) which is the dirty history row of the first pending
// migration when it is a retry, or nil.
//
// Returns int which is the number of migrations successfully applied.
// Returns error when a hook or migration execution fails.
func (service *Migrator) applyPendingUpMigrations(
	ctx context.Context,
	pending []sqlfile.MigrationFile,
	downChecksumsByVersion map[int64]string,
	resume *AppliedMigration,
) (int, error) {
	count := 0

	for index, migration := range pending {
		if stopped := contextStopped(ctx); stopped != nil {
			return count, fmt.Errorf("migration run stopped after %d of %d migrations: %w", count, len(pending), stopped)
		}

		var retried *AppliedMigration
		if index == 0 {
			retried = resume
		}
		if applyError := service.applyUpMigration(ctx, migration, downChecksumsByVersion[migration.Version], retried); applyError != nil {
			return count, applyError
		}
		count++
	}

	if hookError := service.runAfterRunHooks(ctx, pending, MigrationDirectionUp, count); hookError != nil {
		return count, hookError
	}

	return count, nil
}

// applyUpMigration runs one up migration between its before and after hooks.
//
// Takes migration (sqlfile.MigrationFile) which is the migration to apply.
// Takes downChecksum (string) which is the checksum of its down migration, or empty.
// Takes retried (*AppliedMigration) which is the dirty history row an interrupted run of
// this migration left, or nil.
//
// Returns error when a hook or the migration fails.
func (service *Migrator) applyUpMigration(
	ctx context.Context,
	migration sqlfile.MigrationFile,
	downChecksum string,
	retried *AppliedMigration,
) error {
	hookContext := MigrationHookContext{
		Version:   migration.Version,
		Name:      migration.Name,
		Direction: MigrationDirectionUp,
	}

	if hookError := service.runBeforeMigrationHooks(ctx, hookContext); hookError != nil {
		return hookError
	}

	noTransaction, directiveError := service.requestsNoTransaction(migration)
	if directiveError != nil {
		return directiveError
	}
	useTransaction := !noTransaction
	record := upRecord(migration, downChecksum, retried)

	if executeError := service.executor.ExecuteMigration(ctx, record, MigrationDirectionUp, useTransaction); executeError != nil {
		return &MigrationExecutionError{
			Cause:     executeError,
			Name:      migration.Name,
			Version:   migration.Version,
			Direction: MigrationDirectionUp,
		}
	}

	if hookError := service.runAfterMigrationHooks(ctx, hookContext); hookError != nil {
		return hookError
	}

	logging.LoggerFrom(ctx).Log(ctx, logging.LevelTrace, "applied migration",
		slog.Int64(logFieldVersion, migration.Version),
		slog.String("name", migration.Name),
		slog.Int("skip_up_to", record.SkipUpTo),
	)
	return nil
}

// executeRollbacks runs before-run hooks, rolls back the specified number of migrations
// in reverse order, and runs after-run hooks.
//
// Takes applied ([]AppliedMigration) which holds all applied migrations in version order.
// Takes steps (int) which specifies how many migrations to roll back from the end.
// Takes downFilesByVersion (map[int64]sqlfile.MigrationFile) which maps versions to their
// down-migration files.
//
// Returns int which is the number of migrations rolled back.
// Returns error when any hook or rollback execution fails.
func (service *Migrator) executeRollbacks(
	ctx context.Context,
	applied []AppliedMigration,
	steps int,
	downFilesByVersion map[int64]sqlfile.MigrationFile,
) (int, error) {
	applied = slices.Clone(applied)
	slices.SortFunc(applied, func(left, right AppliedMigration) int {
		return cmp.Compare(left.Version, right.Version)
	})

	rollbackVersions := make([]int64, 0, steps)
	downFiles := make([]sqlfile.MigrationFile, 0, steps)
	for index := len(applied) - 1; index >= len(applied)-steps; index-- {
		rollbackVersions = append(rollbackVersions, applied[index].Version)
		if downFile, exists := downFilesByVersion[applied[index].Version]; exists {
			downFiles = append(downFiles, downFile)
		}
	}
	if directiveError := service.refuseMalformedDirectives(downFiles); directiveError != nil {
		return 0, directiveError
	}

	if hookError := service.runBeforeRunHooksFromVersions(ctx, rollbackVersions, MigrationDirectionDown); hookError != nil {
		return 0, hookError
	}

	count := 0
	for index := len(applied) - 1; index >= len(applied)-steps; index-- {
		if stopped := contextStopped(ctx); stopped != nil {
			return count, fmt.Errorf("rollback stopped after %d of %d migrations: %w", count, steps, stopped)
		}

		downFile, exists := downFilesByVersion[applied[index].Version]
		if !exists {
			return count, &NoDownMigrationError{Version: applied[index].Version}
		}

		if rollbackError := service.executeSingleRollback(ctx, applied[index], downFile); rollbackError != nil {
			return count, rollbackError
		}
		count++
	}

	if hookError := service.runAfterRunHooksFromVersions(ctx, rollbackVersions, MigrationDirectionDown, count); hookError != nil {
		return count, hookError
	}

	return count, nil
}

// executeSingleRollback validates the down checksum, runs before/after migration hooks,
// and executes a single rollback migration.
//
// Takes appliedMigration (AppliedMigration) which holds the applied migration record to
// roll back.
// Takes downFile (sqlfile.MigrationFile) which holds the down migration file content.
//
// Returns error when checksum validation, hook execution, or migration execution fails.
func (service *Migrator) executeSingleRollback(
	ctx context.Context,
	appliedMigration AppliedMigration,
	downFile sqlfile.MigrationFile,
) error {
	logger := logging.LoggerFrom(ctx)
	hookContext := MigrationHookContext{
		Version:   downFile.Version,
		Name:      downFile.Name,
		Direction: MigrationDirectionDown,
	}

	if hookError := service.runBeforeMigrationHooks(ctx, hookContext); hookError != nil {
		return hookError
	}

	if checksumError := validateDownChecksum(ctx, appliedMigration, downFile); checksumError != nil {
		return checksumError
	}

	noTransaction, directiveError := service.requestsNoTransaction(downFile)
	if directiveError != nil {
		return directiveError
	}
	useTransaction := !noTransaction
	record := MigrationRecord{
		Name:            downFile.Name,
		Checksum:        downFile.Checksum,
		DownChecksum:    "",
		StatementsHash:  "",
		Content:         downFile.Content,
		Version:         downFile.Version,
		SkipUpTo:        -1,
		SplitterVersion: 0,
		Dirty:           false,
	}

	executeError := service.executor.ExecuteMigration(
		ctx, record, MigrationDirectionDown, useTransaction,
	)
	if executeError != nil {
		return &MigrationExecutionError{
			Cause:     executeError,
			Name:      downFile.Name,
			Version:   downFile.Version,
			Direction: MigrationDirectionDown,
		}
	}

	if hookError := service.runAfterMigrationHooks(ctx, hookContext); hookError != nil {
		return hookError
	}

	logger.Log(ctx, logging.LevelTrace, "rolled back migration",
		slog.Int64(logFieldVersion, downFile.Version),
		slog.String("name", downFile.Name),
	)
	return nil
}

// runBeforeRunHooks invokes all registered before-run hooks with a context built from the
// pending migration files.
//
// Takes pending ([]sqlfile.MigrationFile) which holds the migrations about to be applied.
// Takes direction (MigrationDirection) which indicates whether the run is up or down.
//
// Returns error when any hook returns an error.
func (service *Migrator) runBeforeRunHooks(
	ctx context.Context,
	pending []sqlfile.MigrationFile,
	direction MigrationDirection,
) error {
	if len(service.beforeRunHooks) == 0 {
		return nil
	}
	return service.runBeforeRunHooksFromVersions(ctx, migrationVersions(pending), direction)
}

// runBeforeRunHooksFromVersions invokes all registered before-run hooks with a context
// built from explicit version numbers.
//
// Takes versions ([]int64) which holds the migration versions about to be processed.
// Takes direction (MigrationDirection) which indicates whether the run is up or down.
//
// Returns error when any hook returns an error.
func (service *Migrator) runBeforeRunHooksFromVersions(
	ctx context.Context,
	versions []int64,
	direction MigrationDirection,
) error {
	hookContext := MigrationRunHookContext{
		Direction:       direction,
		PendingCount:    len(versions),
		PendingVersions: versions,
	}
	return runHooks(ctx, "migration before-run-hook", service.beforeRunHooks, func(hook BeforeRunHook) error {
		return hook(ctx, hookContext)
	})
}

// runAfterRunHooks invokes all registered after-run hooks with a context built from the
// pending migration files and the count of applied migrations.
//
// Takes pending ([]sqlfile.MigrationFile) which holds the migrations that were processed.
// Takes direction (MigrationDirection) which indicates whether the run was up or down.
// Takes applied (int) which is the number of migrations that were applied.
//
// Returns error when any hook returns an error.
func (service *Migrator) runAfterRunHooks(
	ctx context.Context,
	pending []sqlfile.MigrationFile,
	direction MigrationDirection,
	applied int,
) error {
	if len(service.afterRunHooks) == 0 {
		return nil
	}
	return service.runAfterRunHooksFromVersions(ctx, migrationVersions(pending), direction, applied)
}

// runAfterRunHooksFromVersions invokes all registered after-run hooks with a context
// built from explicit version numbers and the count of applied migrations.
//
// Takes versions ([]int64) which holds the migration versions that were processed.
// Takes direction (MigrationDirection) which indicates whether the run was up or down.
// Takes applied (int) which is the number of migrations that were applied.
//
// Returns error when any hook returns an error.
func (service *Migrator) runAfterRunHooksFromVersions(
	ctx context.Context,
	versions []int64,
	direction MigrationDirection,
	applied int,
) error {
	hookContext := MigrationRunHookContext{
		Direction:       direction,
		PendingCount:    len(versions),
		PendingVersions: versions,
	}
	return runHooks(ctx, "migration after-run-hook", service.afterRunHooks, func(hook AfterRunHook) error {
		return hook(ctx, hookContext, applied)
	})
}

// runBeforeMigrationHooks invokes all registered before-migration hooks for a single
// migration.
//
// Takes hookContext (MigrationHookContext) which holds the version, name, and direction
// of the migration.
//
// Returns error when any hook returns an error.
func (service *Migrator) runBeforeMigrationHooks(
	ctx context.Context,
	hookContext MigrationHookContext,
) error {
	return runHooks(ctx, "migration before-hook", service.beforeMigrationHooks, func(hook BeforeMigrationHook) error {
		return hook(ctx, hookContext)
	})
}

// runAfterMigrationHooks invokes all registered after-migration hooks for a single
// migration.
//
// Takes hookContext (MigrationHookContext) which holds the version, name, and direction
// of the migration.
//
// Returns error when any hook returns an error.
func (service *Migrator) runAfterMigrationHooks(
	ctx context.Context,
	hookContext MigrationHookContext,
) error {
	return runHooks(ctx, "migration after-hook", service.afterMigrationHooks, func(hook AfterMigrationHook) error {
		return hook(ctx, hookContext)
	})
}

// acquireLock acquires the migration lock, without waiting when WithNonBlockingLock is
// set, and waiting no longer than WithLockTimeout allows otherwise.
//
// Returns error when the lock cannot be acquired, wrapping ErrLockTimeout when the
// timeout cut the wait short.
func (service *Migrator) acquireLock(ctx context.Context) error {
	return service.acquireWithin(ctx, lockMigration, func(lockContext context.Context) error {
		if service.nonBlockingLock {
			return service.executor.TryAcquireLock(lockContext)
		}
		return service.executor.AcquireLock(lockContext)
	})
}

// joinReleaseError releases the migration lock on a context detached from cancellation,
// and joins any release failure into the run's result.
//
// Takes runError (error) which is the run's result so far.
//
// Returns error which is runError unchanged when the release succeeds, or joined with the
// release failure otherwise.
func (service *Migrator) joinReleaseError(ctx context.Context, runError error) error {
	releaseError := service.executor.ReleaseLock(context.WithoutCancel(ctx))
	if releaseError == nil {
		return runError
	}
	return errors.Join(runError, fmt.Errorf("releasing migration lock: %w", releaseError))
}

// requestsNoTransaction reads an explicit migration transaction directive using accepted
// prefixes.
//
// The executor's lexical rules are used when available, with neutral rules otherwise, so
// strings and comments are handled correctly.
//
// Takes file (sqlfile.MigrationFile) which is the migration file.
//
// Returns bool which is true when the migration asks to run without BEGIN/COMMIT.
// Returns error which wraps ErrMalformedDirective, naming the file, when its
// sgl.migration directive cannot be read for certain.
func (service *Migrator) requestsNoTransaction(file sqlfile.MigrationFile) (bool, error) {
	lexicon := syntax.NeutralLexicon()
	if lexical, ok := service.executor.(lexicalExecutor); ok {
		lexicon.Rules = lexical.LexicalRules()
	}
	noTransaction, err := syntax.RequestsNoTransaction(string(file.Content), lexicon, service.prefixes)
	if err != nil {
		return false, fmt.Errorf("migration %s: %w", file.Filename, err)
	}
	return noTransaction, nil
}

// callGate lets one call through at a time, so the calls on one Migrator or Seeder never
// share the ExecutorPort's locked session.
type callGate chan struct{}

// newCallGate returns an open gate.
//
// Returns callGate which admits one call at a time.
func newCallGate() callGate {
	return make(callGate, 1)
}

// enter waits until no other call is inside the gate, or ctx ends. A ctx that has already
// ended never enters, so the same cancellation is reported the same way whether or not
// the gate is free.
//
// Returns func() which lets the next call in; the caller must invoke it.
// Returns error which wraps the context's error and cause when ctx has ended.
func (gate callGate) enter(ctx context.Context) (func(), error) {
	if stopped := contextStopped(ctx); stopped != nil {
		return nil, fmt.Errorf("not starting the call: %w", stopped)
	}
	select {
	case gate <- struct{}{}:
		return func() { <-gate }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("waiting for another call on this instance to finish: %w", contextStopped(ctx))
	}
}

// lexicalExecutor is an ExecutorPort that says how its engine writes SQL, so the Migrator
// reads a migration's directive comments as the engine does. internal/migrate/sqlexec's
// Executor is one.
type lexicalExecutor interface {
	// LexicalRules returns the engine's lexical rules.
	//
	// Returns sqllex.Rules which are the rules.
	LexicalRules() sqllex.Rules
}

// prepare validates the inputs shared by New and NewSeeder and builds the file reader.
//
// Takes files (fs.FS) which holds the files.
// Takes directory (string) which is the directory within files.
// Takes options ([]Option) which configure the run time.
//
// Returns sqlfile.FileReader which reads the files.
// Returns string which is the cleaned directory.
// Returns settings which holds the configuration.
// Returns error which wraps ErrInvalidConfig when an input is invalid.
func prepare(files fs.FS, directory string, options []Option) (sqlfile.FileReader, string, settings, error) {
	if files == nil {
		return nil, "", settings{}, fmt.Errorf("no file system to read from: %w", ErrInvalidConfig)
	}
	cleaned := "."
	if directory != "" {
		cleaned = path.Clean(directory)
	}
	if !fs.ValidPath(cleaned) {
		return nil, "", settings{}, fmt.Errorf("directory %q is not a valid relative, slash-separated path: %w", directory, ErrInvalidConfig)
	}
	configured, err := newSettings(options)
	if err != nil {
		return nil, "", settings{}, err
	}
	return sqlfile.NewFSReader(files, configured.maxFileBytes, configured.maxFiles), cleaned, configured, nil
}

// refuseForeignDownSections refuses migrations about to run that hold another migration
// tool's down section, which the runner would apply with the rest of the file, undoing
// it.
//
// Takes pending ([]sqlfile.MigrationFile) which holds the migrations about to run.
//
// Returns error which is a *ForeignDownSectionError for the first such migration, or nil.
func refuseForeignDownSections(pending []sqlfile.MigrationFile) error {
	for index := range pending {
		if section, found := sqlfile.FindForeignDownSection(pending[index].Content); found {
			return &ForeignDownSectionError{
				Filename: pending[index].Filename,
				Marker:   section.Marker,
				Tool:     section.Tool,
				Line:     section.Line,
			}
		}
	}
	return nil
}

// upRecord builds the record of an up migration, carrying the progress of the interrupted
// run it retries.
//
// Takes migration (sqlfile.MigrationFile) which is the migration to apply.
// Takes downChecksum (string) which is the checksum of its down migration, or empty.
// Takes retried (*AppliedMigration) which is the dirty history row an interrupted run
// left, or nil for a first run.
//
// Returns MigrationRecord which is the record to pass to the executor.
func upRecord(migration sqlfile.MigrationFile, downChecksum string, retried *AppliedMigration) MigrationRecord {
	record := MigrationRecord{
		Name:            migration.Name,
		Checksum:        migration.Checksum,
		DownChecksum:    downChecksum,
		StatementsHash:  "",
		Content:         migration.Content,
		Version:         migration.Version,
		SkipUpTo:        -1,
		SplitterVersion: 0,
		Dirty:           false,
	}
	if retried == nil {
		return record
	}
	record.Dirty = true
	record.SkipUpTo = lastCompletedStatement(*retried)
	record.SplitterVersion = retried.SplitterVersion
	record.StatementsHash = retried.StatementsHash
	return record
}

// resumePoint finds the dirty migration the run resumes.
//
// Takes pending ([]sqlfile.MigrationFile) which holds the pending migrations, which
// include any dirty one.
// Takes applied ([]AppliedMigration) which holds the applied migration records.
//
// Returns *AppliedMigration which is the dirty history row of the first pending
// migration, or nil when no migration is dirty.
// Returns error which is a DirtyMigrationError when the dirty migration is not the next
// pending one.
func resumePoint(ctx context.Context, pending []sqlfile.MigrationFile, applied []AppliedMigration) (*AppliedMigration, error) {
	dirtyMigration := findDirtyMigration(applied)
	if dirtyMigration == nil {
		return nil, nil
	}
	skipUpTo := lastCompletedStatement(*dirtyMigration)
	if len(pending) == 0 || pending[0].Version != dirtyMigration.Version {
		return nil, &DirtyMigrationError{
			Version:       dirtyMigration.Version,
			LastStatement: skipUpTo,
		}
	}
	logging.LoggerFrom(ctx).Log(ctx, logging.LevelTrace, "retrying dirty migration",
		slog.Int64(logFieldVersion, dirtyMigration.Version),
		slog.Int("skip_up_to", skipUpTo),
	)
	return dirtyMigration, nil
}

// lastCompletedStatement returns the last statement a history row records as completed.
//
// Takes applied (AppliedMigration) which is the history row.
//
// Returns int which is the 0-based index of the last completed statement, or -1 when none
// completed or none was recorded.
func lastCompletedStatement(applied AppliedMigration) int {
	if applied.LastStatement == nil {
		return -1
	}
	return *applied.LastStatement
}

// findDirtyMigration scans applied migrations for one marked as dirty.
//
// Takes applied ([]AppliedMigration) which holds the applied migration records.
//
// Returns *AppliedMigration which is the dirty migration, or nil if none is dirty.
func findDirtyMigration(applied []AppliedMigration) *AppliedMigration {
	for index := range applied {
		if applied[index].Dirty {
			return &applied[index]
		}
	}
	return nil
}

// refuseDirtyRollback refuses a rollback while the history holds a dirty migration.
//
// A down file assumes its up file ran in full. Run over a partly applied migration, it
// can fail part-way after dropping objects, and the dirty row would then let the next Up
// resume as if the dropped objects were still there.
//
// Takes applied ([]AppliedMigration) which holds the history read under the lock.
//
// Returns error which wraps ErrDirtyRollback and a *DirtyMigrationError, or nil when no
// migration is dirty.
func refuseDirtyRollback(applied []AppliedMigration) error {
	dirty := findDirtyMigration(applied)
	if dirty == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrDirtyRollback, &DirtyMigrationError{
		Version:       dirty.Version,
		LastStatement: lastCompletedStatement(*dirty),
	})
}

// validateDownChecksum checks that the down migration file checksum matches the checksum
// recorded when the up migration was applied.
//
// Takes appliedMigration (AppliedMigration) which holds the recorded down checksum.
// Takes downFile (sqlfile.MigrationFile) which holds the current file checksum.
//
// Returns error when the recorded checksum does not match the file checksum.
func validateDownChecksum(
	ctx context.Context,
	appliedMigration AppliedMigration,
	downFile sqlfile.MigrationFile,
) error {
	recordedDownChecksum := appliedMigration.DownChecksum
	if recordedDownChecksum != "" && recordedDownChecksum != downFile.Checksum {
		return &DownChecksumMismatchError{
			Version:          downFile.Version,
			Name:             downFile.Name,
			RecordedChecksum: recordedDownChecksum,
			FileChecksum:     downFile.Checksum,
		}
	}
	if recordedDownChecksum == "" {
		logger := logging.LoggerFrom(ctx)
		logger.WarnContext(ctx, "no recorded down checksum for migration, skipping validation",
			slog.Int64(logFieldVersion, downFile.Version),
		)
	}
	return nil
}

// filterByDirection returns only files matching the given direction.
//
// Takes files ([]sqlfile.MigrationFile) which holds all migration files.
// Takes direction (MigrationDirection) which specifies the direction to filter by.
//
// Returns []sqlfile.MigrationFile which holds only files matching the specified
// direction.
func filterByDirection(
	files []sqlfile.MigrationFile,
	direction MigrationDirection,
) []sqlfile.MigrationFile {
	var filtered []sqlfile.MigrationFile
	for _, file := range files {
		if directionOf(file) == direction {
			filtered = append(filtered, file)
		}
	}
	return filtered
}

// buildDownFileMap indexes down migration files by version.
//
// Takes files ([]sqlfile.MigrationFile) which holds all migration files.
//
// Returns map[int64]sqlfile.MigrationFile which maps version numbers to their
// corresponding down migration files.
func buildDownFileMap(
	files []sqlfile.MigrationFile,
) map[int64]sqlfile.MigrationFile {
	result := make(map[int64]sqlfile.MigrationFile)
	for _, file := range files {
		if file.Down {
			result[file.Version] = file
		}
	}
	return result
}

// buildDownChecksumMap returns a map of version to down-migration checksum for all
// versions that have a .down.sql file.
//
// Takes files ([]sqlfile.MigrationFile) which holds all migration files.
//
// Returns map[int64]string which maps version numbers to their down-migration checksums.
func buildDownChecksumMap(files []sqlfile.MigrationFile) map[int64]string {
	result := make(map[int64]string)
	for _, file := range files {
		if file.Down {
			result[file.Version] = file.Checksum
		}
	}
	return result
}

// buildDownVersionSet returns a set of versions that have down migration files.
//
// Takes files ([]sqlfile.MigrationFile) which holds all migration files.
//
// Returns map[int64]bool which contains true for each version that has a down migration
// file.
func buildDownVersionSet(files []sqlfile.MigrationFile) map[int64]bool {
	result := make(map[int64]bool)
	for _, file := range files {
		if file.Down {
			result[file.Version] = true
		}
	}
	return result
}

// filterByTargetVersion filters pending migrations to only include those up to and
// including the target version.
//
// If targetVersion is nil, all pending migrations are returned.
//
// Takes pending ([]sqlfile.MigrationFile) which holds the migrations to filter.
// Takes targetVersion (*int64) which specifies the maximum version to include, or nil to
// include all.
//
// Returns []sqlfile.MigrationFile which holds the filtered migrations.
func filterByTargetVersion(
	pending []sqlfile.MigrationFile,
	targetVersion *int64,
) []sqlfile.MigrationFile {
	if targetVersion == nil {
		return pending
	}
	var filtered []sqlfile.MigrationFile
	for _, file := range pending {
		if file.Version <= *targetVersion {
			filtered = append(filtered, file)
		}
	}
	return filtered
}

// computeRollbackSteps determines how many migrations to roll back.
//
// If steps is provided, that value is used (clamped to applied length). If targetVersion
// is provided, it counts applied migrations with version greater than targetVersion.
//
// Takes applied ([]AppliedMigration) which holds all applied migrations.
// Takes steps (*int) which specifies the number of migrations to roll back, or nil to use
// targetVersion.
// Takes targetVersion (*int64) which specifies the version to roll back to, or nil to use
// steps.
//
// Returns int which is the number of migrations to roll back.
func computeRollbackSteps(
	applied []AppliedMigration,
	steps *int,
	targetVersion *int64,
) int {
	if steps != nil {
		if *steps < 0 {
			return 0
		}
		if *steps > len(applied) {
			return len(applied)
		}
		return *steps
	}
	if targetVersion != nil {
		count := 0
		for index := range applied {
			if applied[index].Version > *targetVersion {
				count++
			}
		}
		return count
	}
	return 0
}

// computePending returns up migration files that have not been successfully applied yet,
// sorted by version ascending. Dirty (partially-applied) migrations are treated as
// pending since they need to be retried.
//
// Takes upFiles ([]sqlfile.MigrationFile) which holds all up migration files.
// Takes applied ([]AppliedMigration) which holds already applied migrations.
//
// Returns []sqlfile.MigrationFile which holds the unapplied or dirty migrations sorted by
// version.
func computePending(
	upFiles []sqlfile.MigrationFile,
	applied []AppliedMigration,
) []sqlfile.MigrationFile {
	appliedSet := make(map[int64]struct{}, len(applied))
	for index := range applied {
		if !applied[index].Dirty {
			appliedSet[applied[index].Version] = struct{}{}
		}
	}

	var pending []sqlfile.MigrationFile
	for _, file := range upFiles {
		if _, alreadyApplied := appliedSet[file.Version]; !alreadyApplied {
			pending = append(pending, file)
		}
	}

	slices.SortFunc(pending, func(left, right sqlfile.MigrationFile) int {
		return cmp.Compare(left.Version, right.Version)
	})

	return pending
}

// validateChecksums verifies that all applied migrations have matching checksums with
// on-disk files.
//
// Takes upFiles ([]sqlfile.MigrationFile) which holds the on-disk migration files.
// Takes applied ([]AppliedMigration) which holds the applied migration records.
//
// Returns error when a file is missing or its checksum does not match the applied record.
func validateChecksums(
	upFiles []sqlfile.MigrationFile,
	applied []AppliedMigration,
) error {
	filesByVersion := make(map[int64]sqlfile.MigrationFile, len(upFiles))
	for _, file := range upFiles {
		filesByVersion[file.Version] = file
	}

	for index := range applied {
		migration := &applied[index]
		file, exists := filesByVersion[migration.Version]
		if !exists {
			return &MissingMigrationFileError{
				Version: migration.Version,
				Name:    migration.Name,
			}
		}
		if file.Checksum != migration.Checksum {
			return &ChecksumMismatchError{
				Version:         migration.Version,
				Name:            migration.Name,
				AppliedChecksum: migration.Checksum,
				FileChecksum:    file.Checksum,
			}
		}
	}

	return nil
}

// detectSkippedMigrations finds pending migrations whose version is earlier than the
// maximum applied version.
//
// These are migrations that were added after later migrations were already applied, for
// example from branch merges.
//
// Takes pending ([]sqlfile.MigrationFile) which holds the pending migrations to check.
// Takes applied ([]AppliedMigration) which holds the already applied migrations.
//
// Returns []int64 which holds the version numbers of skipped migrations.
func detectSkippedMigrations(
	pending []sqlfile.MigrationFile,
	applied []AppliedMigration,
) []int64 {
	if len(applied) == 0 {
		return nil
	}

	maxApplied := applied[len(applied)-1].Version

	var skipped []int64
	for _, migration := range pending {
		if migration.Version < maxApplied {
			skipped = append(skipped, migration.Version)
		}
	}

	return skipped
}

// contextStopped returns why ctx has ended: its error, joined by its cause when the cause
// says more, so the result matches both context.Canceled or context.DeadlineExceeded and
// the cause the caller gave.
//
// Returns error which is nil while ctx has not ended.
func contextStopped(ctx context.Context) error {
	err := ctx.Err()
	if err == nil {
		return nil
	}
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, err) {
		return fmt.Errorf("%w: %w", err, cause)
	}
	return err
}

// migrationVersions returns the versions of migration files, in their order.
//
// Takes files ([]sqlfile.MigrationFile) which holds the files.
//
// Returns []int64 which holds one version per file.
func migrationVersions(files []sqlfile.MigrationFile) []int64 {
	versions := make([]int64, len(files))
	for index := range files {
		versions[index] = files[index].Version
	}
	return versions
}

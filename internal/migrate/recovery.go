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
	"fmt"
	"log/slog"
	"maps"
	"reflect"
	"slices"

	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/observe"
	"seagull.sh/seagull/internal/sqlfile"
)

// RepairPlan describes what Repair would change, as PlanRepair read it: the applied
// migrations whose files no longer match their recorded checksums, and the dirty
// migrations that wait for someone to say which of their statements ran. Repair applies a
// plan only while the history still matches it.
type RepairPlan struct {
	// Versions holds the versions the plan was limited to, or is empty when it covers every
	// migration.
	Versions []int64

	// Changed lists the clean applied migrations whose up file, or whose down file, no
	// longer matches the checksum recorded for it, in version order.
	Changed []ChangedMigration

	// Dirty lists the dirty migrations in version order.
	Dirty []DirtyMigration

	// Missing lists the versions of applied migrations that have no file, which Repair
	// cannot change.
	Missing []int64
}

// Empty reports whether the plan has nothing Repair can change.
//
// Returns bool which is true when no migration is changed or dirty.
func (plan *RepairPlan) Empty() bool {
	return len(plan.Changed) == 0 && len(plan.Dirty) == 0
}

// ChangedMigration is an applied migration whose files no longer match the checksums
// recorded for it.
type ChangedMigration struct {
	// Name holds the migration's name.
	Name string

	// RecordedChecksum holds the checksum recorded when the migration was applied.
	RecordedChecksum string

	// FileChecksum holds the checksum of the up file now.
	FileChecksum string

	// RecordedDownChecksum holds the down file's checksum recorded when the migration was
	// applied, or is empty when none was recorded.
	RecordedDownChecksum string

	// DownFileChecksum holds the checksum of the down file now, or is empty when the
	// migration has no down file.
	DownFileChecksum string

	// Version holds the migration's version.
	Version int64
}

// DirtyMigration is a migration whose interrupted run left it dirty.
type DirtyMigration struct {
	// Name holds the migration's name.
	Name string

	// Statements holds the migration's statements as the executor divides its file now,
	// which RepairOptions.Completed counts.
	Statements []string

	// LastStatement holds the 0-based index of the last statement the history records as
	// completed, counted as the interrupted run divided the file, or -1 when it records
	// none.
	LastStatement int

	// Version holds the migration's version.
	Version int64

	// ChecksumMatch reports whether the file still matches the checksum recorded for it.
	ChecksumMatch bool
}

// RepairOptions says which repair to apply.
type RepairOptions struct {
	// Plan is the required, reviewed result of PlanRepair. Repair makes no changes if the
	// history no longer matches it.
	Plan *RepairPlan

	// Completed records completed statement counts for dirty migrations. Zero resets a
	// migration, a full count marks it applied, and omitted entries leave it unchanged.
	Completed map[int64]int
}

// RepairResult reports what Repair changed.
type RepairResult struct {
	// Rebaselined lists the migrations whose recorded checksums were replaced by their
	// files' checksums.
	Rebaselined []int64

	// Resolved lists the dirty migrations whose history was rewritten.
	Resolved []int64
}

// Force marks a migration as applied without executing it.
//
// It records the current checksums and clears dirty progress under the migration lock.
// Earlier pending migrations remain pending, and a later recorded version prevents the
// change. Migration hooks do not run.
//
// Takes version (int64) which is the migration to force; it must have an up file.
//
// Returns error which is an *UnknownMigrationError when no file has the version, a
// *LaterMigrationError when a later migration is recorded, ErrHistoryEditingUnsupported
// when the executor cannot rewrite the history, or the failure to take the lock or write
// the history, joined with any failure to release the lock.
func (service *Migrator) Force(ctx context.Context, version int64) (err error) {
	ctx = service.contextFor(ctx)
	ctx, end := observe.From(ctx).Start(ctx, observe.StageMigrateForce, slog.Int64(logFieldVersion, version))
	defer func() { end(err) }()
	editor, editable := service.executor.(HistoryEditorPort)
	if !editable {
		return ErrHistoryEditingUnsupported
	}

	leave, gateError := service.gate.enter(ctx)
	if gateError != nil {
		return gateError
	}
	defer leave()

	allFiles, readError := readMigrationFiles(ctx, service.callReader(service.fileReader), service.directory)
	if readError != nil {
		return readError
	}
	upFile, found := upFilesByVersion(allFiles)[version]
	if !found {
		return &UnknownMigrationError{Version: version}
	}

	applied, lockError := service.lockedHistory(ctx)
	if lockError != nil {
		return lockError
	}
	defer func() { err = service.joinReleaseError(ctx, err) }()

	for index := range applied {
		if applied[index].Version > version {
			return &LaterMigrationError{Version: version, Later: applied[index].Version, LaterDirty: applied[index].Dirty}
		}
	}

	if recordError := editor.RecordApplied(ctx, upRecord(upFile, buildDownChecksumMap(allFiles)[version], nil)); recordError != nil {
		return fmt.Errorf("forcing migration %d: %w", version, recordError)
	}
	logging.LoggerFrom(ctx).WarnContext(ctx, "forced migration as applied without running it",
		slog.Int64(logFieldVersion, version),
		slog.String("name", upFile.Name),
	)
	return nil
}

// PlanRepair reads what Repair would change, for someone to review before applying it. It
// takes no lock and runs no DDL.
//
// Takes versions (...int64) which limit the plan to these migrations; none covers every
// migration.
//
// Returns *RepairPlan which lists the changed, dirty and missing migrations.
// Returns error which is an *UnknownMigrationError for a version with neither a file nor
// a history row, ErrHistoryEditingUnsupported when the executor cannot rewrite the
// history, or the failure to read the files or the history.
func (service *Migrator) PlanRepair(ctx context.Context, versions ...int64) (_ *RepairPlan, err error) {
	ctx = service.contextFor(ctx)
	ctx, end := observe.From(ctx).Start(ctx, observe.StageMigrateRepairPlan)
	defer func() { end(err) }()
	editor, editable := service.executor.(HistoryEditorPort)
	if !editable {
		return nil, ErrHistoryEditingUnsupported
	}

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
	return planRepair(editor, allFiles, applied, versions)
}

// Repair applies a reviewed repair plan under the migration lock.
//
// It rechecks the history before updating checksums or dirty progress. If the history has
// changed, nothing is modified. No migrations or hooks run.
//
// Takes options (RepairOptions) which hold the reviewed plan and the dirty migrations to
// resolve.
//
// Returns RepairResult which lists what changed.
// Returns error which wraps ErrInvalidConfig for a missing plan or a Completed entry that
// is not a dirty migration of the plan or counts outside its statements,
// ErrRepairOutOfDate when the history changed since the plan was made,
// ErrHistoryEditingUnsupported when the executor cannot rewrite the history, or the
// failure to take the lock or write the history, joined with any failure to release the
// lock.
func (service *Migrator) Repair(ctx context.Context, options RepairOptions) (_ RepairResult, err error) {
	ctx = service.contextFor(ctx)
	ctx, end := observe.From(ctx).Start(ctx, observe.StageMigrateRepair)
	defer func() { end(err) }()
	editor, editable := service.executor.(HistoryEditorPort)
	if !editable {
		return RepairResult{}, ErrHistoryEditingUnsupported
	}
	if validationError := validateRepairOptions(options); validationError != nil {
		return RepairResult{}, validationError
	}

	leave, gateError := service.gate.enter(ctx)
	if gateError != nil {
		return RepairResult{}, gateError
	}
	defer leave()

	allFiles, readError := readMigrationFiles(ctx, service.callReader(service.fileReader), service.directory)
	if readError != nil {
		return RepairResult{}, readError
	}

	applied, lockError := service.lockedHistory(ctx)
	if lockError != nil {
		return RepairResult{}, lockError
	}
	defer func() { err = service.joinReleaseError(ctx, err) }()

	current, planError := planRepair(editor, allFiles, applied, options.Plan.Versions)
	if planError != nil {
		return RepairResult{}, planError
	}
	if !reflect.DeepEqual(current, options.Plan) {
		return RepairResult{}, ErrRepairOutOfDate
	}
	return service.applyRepair(ctx, editor, allFiles, options)
}

// lockedHistory takes the migration lock, makes sure the history table exists, and reads
// the history under the lock. The caller releases the lock when it returns no error.
//
// Returns []AppliedMigration which holds the history read under the lock.
// Returns error which is a *LockAcquisitionError when the lock cannot be taken, or the
// failure to create or read the history, after the lock is released again.
func (service *Migrator) lockedHistory(ctx context.Context) ([]AppliedMigration, error) {
	if lockError := service.acquireLock(ctx); lockError != nil {
		return nil, &LockAcquisitionError{Cause: lockError, Lock: lockMigration}
	}
	if ensureError := service.executor.EnsureMigrationTable(ctx); ensureError != nil {
		return nil, service.joinReleaseError(ctx, fmt.Errorf(errorFormatEnsuringMigrationTable, ensureError))
	}
	applied, appliedError := service.executor.AppliedVersions(ctx)
	if appliedError != nil {
		return nil, service.joinReleaseError(ctx, fmt.Errorf("reading applied versions under lock: %w", appliedError))
	}
	return applied, nil
}

// applyRepair re-baselines the plan's changed migrations and resolves the dirty ones
// options.Completed names, in version order.
//
// Takes editor (HistoryEditorPort) which rewrites the history.
// Takes allFiles ([]sqlfile.MigrationFile) which holds every migration file.
// Takes options (RepairOptions) which hold the plan and the resolutions.
//
// Returns RepairResult which lists what changed.
// Returns error when the history cannot be written; what changed before it stays.
func (*Migrator) applyRepair(ctx context.Context, editor HistoryEditorPort, allFiles []sqlfile.MigrationFile, options RepairOptions) (RepairResult, error) {
	upFiles := upFilesByVersion(allFiles)
	downChecksums := buildDownChecksumMap(allFiles)
	result := RepairResult{Rebaselined: nil, Resolved: nil}
	for _, changed := range options.Plan.Changed {
		upFile := upFiles[changed.Version]
		if recordError := editor.RecordApplied(ctx, upRecord(upFile, downChecksums[changed.Version], nil)); recordError != nil {
			return result, fmt.Errorf("re-baselining migration %d: %w", changed.Version, recordError)
		}
		result.Rebaselined = append(result.Rebaselined, changed.Version)
	}
	for _, dirty := range options.Plan.Dirty {
		completed, resolve := options.Completed[dirty.Version]
		if !resolve {
			continue
		}
		record := upRecord(upFiles[dirty.Version], downChecksums[dirty.Version], nil)
		var recordError error
		if completed == len(dirty.Statements) {
			recordError = editor.RecordApplied(ctx, record)
		} else {
			recordError = editor.RecordProgress(ctx, record, completed)
		}
		if recordError != nil {
			return result, fmt.Errorf("resolving dirty migration %d: %w", dirty.Version, recordError)
		}
		result.Resolved = append(result.Resolved, dirty.Version)
	}
	logging.LoggerFrom(ctx).WarnContext(ctx, "repaired migration history",
		slog.Any("rebaselined", result.Rebaselined),
		slog.Any("resolved", result.Resolved),
	)
	return result, nil
}

// validateRepairOptions checks that the options name a plan, and that every resolution
// names a dirty migration of it and counts within its statements.
//
// Takes options (RepairOptions) which are checked.
//
// Returns error which wraps ErrInvalidConfig naming the first problem, or nil.
func validateRepairOptions(options RepairOptions) error {
	if options.Plan == nil {
		return fmt.Errorf("Repair needs the plan PlanRepair returned: %w", ErrInvalidConfig)
	}
	dirtyByVersion := make(map[int64]int, len(options.Plan.Dirty))
	for index := range options.Plan.Dirty {
		dirtyByVersion[options.Plan.Dirty[index].Version] = index
	}
	for _, version := range slices.Sorted(maps.Keys(options.Completed)) {
		index, dirty := dirtyByVersion[version]
		if !dirty {
			return fmt.Errorf("migration %d is not a dirty migration of the plan: %w", version, ErrInvalidConfig)
		}
		completed, total := options.Completed[version], len(options.Plan.Dirty[index].Statements)
		if completed < 0 || completed > total {
			return fmt.Errorf("migration %d has %d statements, so between 0 and %d of them ran, not %d: %w",
				version, total, total, completed, ErrInvalidConfig)
		}
	}
	return nil
}

// planRepair compares the history with the files and lists what a repair can change.
//
// Takes editor (HistoryEditorPort) which divides dirty migrations into statements.
// Takes allFiles ([]sqlfile.MigrationFile) which holds every migration file.
// Takes applied ([]AppliedMigration) which holds the history.
// Takes versions ([]int64) which limit the plan, or are empty for every migration.
//
// Returns *RepairPlan which lists the changed, dirty and missing migrations.
// Returns error which is an *UnknownMigrationError for a version with neither a file nor
// a history row, or the failure to divide a dirty migration.
func planRepair(editor HistoryEditorPort, allFiles []sqlfile.MigrationFile, applied []AppliedMigration, versions []int64) (*RepairPlan, error) {
	plan := &RepairPlan{Versions: slices.Clone(versions), Changed: nil, Dirty: nil, Missing: nil}
	upFiles := upFilesByVersion(allFiles)
	recorded := make(map[int64]struct{}, len(applied))
	for index := range applied {
		recorded[applied[index].Version] = struct{}{}
	}
	wanted := make(map[int64]struct{}, len(versions))
	for _, version := range versions {
		_, hasFile := upFiles[version]
		if _, isRecorded := recorded[version]; !hasFile && !isRecorded {
			return nil, &UnknownMigrationError{Version: version}
		}
		wanted[version] = struct{}{}
	}
	downChecksums := buildDownChecksumMap(allFiles)
	for index := range applied {
		row := &applied[index]
		if _, isWanted := wanted[row.Version]; len(versions) > 0 && !isWanted {
			continue
		}
		upFile, hasFile := upFiles[row.Version]
		switch {
		case !hasFile:
			plan.Missing = append(plan.Missing, row.Version)
		case row.Dirty:
			statements, splitError := editor.SplitStatements(upFile.Content)
			if splitError != nil {
				return nil, fmt.Errorf("dividing dirty migration %d into statements: %w", row.Version, splitError)
			}
			plan.Dirty = append(plan.Dirty, DirtyMigration{
				Name:          upFile.Name,
				Statements:    statements,
				LastStatement: lastCompletedStatement(*row),
				Version:       row.Version,
				ChecksumMatch: row.Checksum == upFile.Checksum,
			})
		case checksumsChanged(row, upFile.Checksum, downChecksums[row.Version]):
			plan.Changed = append(plan.Changed, ChangedMigration{
				Name:                 upFile.Name,
				RecordedChecksum:     row.Checksum,
				FileChecksum:         upFile.Checksum,
				RecordedDownChecksum: row.DownChecksum,
				DownFileChecksum:     downChecksums[row.Version],
				Version:              row.Version,
			})
		}
	}
	return plan, nil
}

// checksumsChanged reports whether an applied migration's files no longer match its
// history row: the up file always, the down file only when its checksum was recorded, as
// Up and Down check them.
//
// Takes row (*AppliedMigration) which is the history row.
// Takes upChecksum (string) which is the up file's checksum now.
// Takes downChecksum (string) which is the down file's checksum now, or empty.
//
// Returns bool which is true when a recorded checksum differs from its file's.
func checksumsChanged(row *AppliedMigration, upChecksum, downChecksum string) bool {
	return row.Checksum != upChecksum || (row.DownChecksum != "" && row.DownChecksum != downChecksum)
}

// upFilesByVersion indexes the up files by version.
//
// Takes allFiles ([]sqlfile.MigrationFile) which holds every migration file.
//
// Returns map[int64]sqlfile.MigrationFile which maps each version to its up file.
func upFilesByVersion(allFiles []sqlfile.MigrationFile) map[int64]sqlfile.MigrationFile {
	upFiles := make(map[int64]sqlfile.MigrationFile, len(allFiles))
	for _, file := range allFiles {
		if directionOf(file) == MigrationDirectionUp {
			upFiles[file.Version] = file
		}
	}
	return upFiles
}

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
	"errors"
	"fmt"

	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/sqlfile"
)

var (
	// ErrLockNotAcquired is returned when a non-blocking lock attempt fails because another
	// process already holds the migration lock.
	ErrLockNotAcquired = errors.New("migration lock is already held")

	// ErrLockTimeout is the cause of a lock acquisition that WithLockTimeout cut short: the
	// lock was still held elsewhere when the timeout passed. The acquisition error wraps it.
	ErrLockTimeout = errors.New("timed out waiting for the lock")

	// ErrHistoryEditingUnsupported is returned by Migrator.Force, Migrator.PlanRepair and
	// Migrator.Repair when the ExecutorPort does not implement HistoryEditorPort, so the
	// history cannot be rewritten without running a migration. It wraps ErrInvalidConfig.
	ErrHistoryEditingUnsupported = fmt.Errorf("the executor cannot rewrite the migration history; it does not implement migrate.HistoryEditorPort: %w", ErrInvalidConfig)

	// ErrRepairOutOfDate is returned by Migrator.Repair when the history no longer matches
	// what the caller resolved, such as a dirty migration that another run has resumed since
	// the repair was planned. Plan the repair again.
	ErrRepairOutOfDate = errors.New("the migration history changed since the repair was planned")

	// ErrNoDownMigration is returned when a rollback is requested for a migration version
	// that has no corresponding .down.sql file.
	ErrNoDownMigration = errors.New("no down migration file")

	// ErrDirtyRollback is returned by Migrator.Down and Migrator.DownTo while the history
	// holds a dirty migration, before anything runs.
	//
	// The error also wraps a *DirtyMigrationError naming the migration. Finish it with Up,
	// or record which of its statements ran with Migrator.PlanRepair and Migrator.Repair,
	// then roll back.
	ErrDirtyRollback = errors.New("cannot roll back while a migration is dirty")

	// ErrInvalidConfig is returned by New and NewSeeder, and by the sqlexec constructors,
	// for a configuration that cannot be used. It is the generator's sentinel of the same
	// name.
	ErrInvalidConfig = fault.ErrInvalidConfig

	// ErrFileTooLarge is returned when a migration or seed file exceeds the size limit set
	// with WithMaxFileBytes. It is the generator's sentinel of the same name.
	ErrFileTooLarge = fault.ErrFileTooLarge

	// ErrLimit is returned when a call's migration or seed files go past the caps set with
	// WithMaxFiles or WithMaxTotalBytes. It is the generator's sentinel of the same name.
	ErrLimit = fault.ErrLimit

	// ErrPanic is the base error for a panic recovered from code the migrator called, such
	// as a hook or a backend's callback, so a caller can tell a crash in that code from a
	// failure of the database.
	//
	// *HookPanicError wraps it. It is the generator's sentinel of the same name.
	ErrPanic = fault.ErrPanic

	// ErrMalformedDirective is returned, before anything runs, for a migration about to run
	// whose sgl.migration directive cannot be read for certain, such as
	// sgl.migration(no_transacton: true), or whose directive names an unknown role, since
	// running it inside or outside a transaction against what it asked cannot be undone.
	//
	// The error names the file, the line and the problem.
	ErrMalformedDirective = syntax.ErrMalformedMigrationDirective
)

// ChecksumMismatchError is returned when an applied migration's recorded checksum does
// not match the current file on disk.
type ChecksumMismatchError struct {
	// AppliedChecksum holds the checksum recorded when the migration was applied.
	AppliedChecksum string

	// FileChecksum holds the current checksum of the migration file on disk.
	FileChecksum string

	// Name holds the human-readable name of the migration.
	Name string

	// Version holds the numeric version of the migration.
	Version int64
}

// Error returns a human-readable message describing the checksum mismatch.
//
// Returns string which contains the version, name, applied checksum, and file checksum.
func (e *ChecksumMismatchError) Error() string {
	return fmt.Sprintf(
		"checksum mismatch for migration %d (%s): applied=%s file=%s",
		e.Version, e.Name, e.AppliedChecksum, e.FileChecksum,
	)
}

// DownChecksumMismatchError is returned when a down migration file's checksum does not
// match the checksum recorded when the up migration was applied.
type DownChecksumMismatchError struct {
	// RecordedChecksum holds the down checksum recorded when the up migration was applied.
	RecordedChecksum string

	// FileChecksum holds the current checksum of the down migration file on disk.
	FileChecksum string

	// Name holds the human-readable name of the migration.
	Name string

	// Version holds the numeric version of the migration.
	Version int64
}

// Error returns a human-readable message describing the down checksum mismatch.
//
// Returns string which contains the version, name, recorded checksum, and file checksum.
func (e *DownChecksumMismatchError) Error() string {
	return fmt.Sprintf(
		"down checksum mismatch for migration %d (%s): recorded=%s file=%s",
		e.Version, e.Name, e.RecordedChecksum, e.FileChecksum,
	)
}

// MigrationExecutionError wraps an error from executing a migration's SQL content,
// carrying the migration identity and direction.
type MigrationExecutionError struct {
	// Cause holds the underlying error from migration execution.
	Cause error

	// Name holds the human-readable name of the migration.
	Name string

	// Version holds the numeric version of the migration.
	Version int64

	// Direction holds whether the migration was an up or down operation.
	Direction MigrationDirection
}

// Error returns a human-readable message describing the execution failure.
//
// Returns string which contains the direction label, version, name, and underlying cause.
func (e *MigrationExecutionError) Error() string {
	label := "migration"
	if e.Direction == MigrationDirectionDown {
		label = "rollback"
	}
	return fmt.Sprintf("%s %d (%s): %v", label, e.Version, e.Name, e.Cause)
}

// Unwrap returns the underlying cause for errors.Is/errors.As.
//
// Returns error which is the wrapped cause of the execution failure.
func (e *MigrationExecutionError) Unwrap() error {
	return e.Cause
}

// LockAcquisitionError wraps a failure to acquire the migration lock or, from a Seeder,
// the seed lock.
type LockAcquisitionError struct {
	// Cause holds the underlying error from the lock acquisition attempt.
	Cause error

	// Lock names the lock that was not acquired: "migration" or "seed". Empty means the
	// migration lock.
	Lock string
}

// Error returns a human-readable message describing the lock failure.
//
// Returns string which names the lock and contains the underlying cause of the failure.
func (e *LockAcquisitionError) Error() string {
	lock := e.Lock
	if lock == "" {
		lock = lockMigration
	}
	return fmt.Sprintf("acquiring %s lock: %v", lock, e.Cause)
}

// Unwrap returns the underlying cause for errors.Is/errors.As.
//
// Returns error which is the wrapped cause of the lock acquisition failure.
func (e *LockAcquisitionError) Unwrap() error {
	return e.Cause
}

// MissingMigrationFileError is returned when the database records an applied migration
// but no corresponding file exists on disk.
type MissingMigrationFileError struct {
	// Name holds the human-readable name of the missing migration.
	Name string

	// Version holds the numeric version of the missing migration.
	Version int64
}

// Error returns a human-readable message describing the missing file.
//
// Returns string which contains the version and name of the missing migration.
func (e *MissingMigrationFileError) Error() string {
	return fmt.Sprintf(
		"applied migration %d (%s) has no corresponding file on disk",
		e.Version, e.Name,
	)
}

// ForeignDownSectionError reports an up migration containing another tool's down section,
// which would immediately undo its work. Previously applied migrations are exempt.
type ForeignDownSectionError struct {
	// Filename holds the up migration's file name.
	Filename string

	// Marker holds the line that starts the down section, as written.
	Marker string

	// Tool names the migration tool whose layout the marker belongs to.
	Tool string

	// Line holds the 1-based line of the marker.
	Line int
}

// Error returns the message sgl generate reports for the file, with its suggestion.
//
// Returns string which names the file, the tool, the marker and where to move the
// statements after it.
func (e *ForeignDownSectionError) Error() string {
	diagnostic := sqlfile.ForeignDownSectionError(e.Filename, sqlfile.ForeignDownSection{Marker: e.Marker, Tool: e.Tool, Line: e.Line})
	return diagnostic.Message + "; " + diagnostic.Suggestion
}

// NoDownMigrationError is returned when a rollback is requested for a specific version
// that has no .down.sql file.
type NoDownMigrationError struct {
	// Version holds the numeric version of the migration missing a down file.
	Version int64
}

// Error returns a human-readable message describing the missing down file.
//
// Returns string which contains the version number.
func (e *NoDownMigrationError) Error() string {
	return fmt.Sprintf("no down migration for version %d", e.Version)
}

// Is reports whether target matches ErrNoDownMigration.
//
// Takes target (error) which specifies the error to compare against.
//
// Returns bool which is true if target is ErrNoDownMigration.
func (*NoDownMigrationError) Is(target error) bool {
	return target == ErrNoDownMigration
}

// DirtyMigrationError is returned when a dirty (partially-applied) migration blocks
// further progress. If the dirty migration matches the next pending version it can be
// retried automatically; otherwise manual intervention is required.
type DirtyMigrationError struct {
	// Version holds the numeric version of the dirty migration.
	Version int64

	// LastStatement holds the 0-based index of the last successfully applied statement, or
	// -1 if no statements completed.
	LastStatement int
}

// Error returns a human-readable message describing the dirty migration.
//
// Returns string which contains the version and last completed statement.
func (e *DirtyMigrationError) Error() string {
	return fmt.Sprintf(
		"migration %d is dirty (last completed statement: %d); "+
			"resolve manually or retry",
		e.Version, e.LastStatement,
	)
}

// UnknownMigrationError is returned when a migration is named by a version that no
// migration file has.
type UnknownMigrationError struct {
	// Version holds the version that was asked for.
	Version int64
}

// Error returns a human-readable message naming the version.
//
// Returns string which contains the version.
func (e *UnknownMigrationError) Error() string {
	return fmt.Sprintf("no migration file has version %d", e.Version)
}

// LaterMigrationError is returned by Migrator.Force when the history records a migration
// after the one to force, so forcing it would not make it the last applied migration.
// Roll the later migrations back first, or force the later one instead; a dirty one is
// resolved with Migrator.Repair.
type LaterMigrationError struct {
	// Version holds the version that was to be forced.
	Version int64

	// Later holds the version of the first migration recorded after it.
	Later int64

	// LaterDirty reports whether that migration is dirty.
	LaterDirty bool
}

// Error returns a human-readable message naming both migrations and the remedy.
//
// Returns string which explains why the migration was not forced.
func (e *LaterMigrationError) Error() string {
	if e.LaterDirty {
		return fmt.Sprintf("cannot force migration %d: migration %d after it is dirty; repair the history of migration %d first",
			e.Version, e.Later, e.Later)
	}
	return fmt.Sprintf("cannot force migration %d: migration %d after it is applied; roll it back first, or force %d instead",
		e.Version, e.Later, e.Later)
}

// ResumeMismatchError reports that a dirty migration's completed statements no longer
// match the current file.
//
// The migration remains dirty until its history is checked and repaired.
type ResumeMismatchError struct {
	// Name holds the human-readable name of the migration.
	Name string

	// Version holds the numeric version of the migration.
	Version int64

	// LastStatement holds the 0-based index of the last statement the interrupted run
	// completed.
	LastStatement int

	// RecordedSplitterVersion holds the splitter version the interrupted run recorded, or
	// zero when it recorded none.
	RecordedSplitterVersion int

	// SplitterVersion holds the version of the splitter that divided the file now.
	SplitterVersion int

	// StatementCount holds the number of statements the file splits into now.
	StatementCount int
}

// Error returns a human-readable message describing why the migration was not resumed.
//
// Returns string which names the migration, the recorded progress and the remedy.
func (e *ResumeMismatchError) Error() string {
	return fmt.Sprintf(
		"migration %d (%s) is dirty after statement %d, but %s, so it is not resumed "+
			"automatically; check which of its statements ran, then repair its history",
		e.Version, e.Name, e.LastStatement, e.reason(),
	)
}

// reason explains what no longer matches.
//
// Returns string which describes the mismatch.
func (e *ResumeMismatchError) reason() string {
	switch {
	case e.RecordedSplitterVersion == 0:
		return "its progress was recorded without the statements it covers"
	case e.RecordedSplitterVersion != e.SplitterVersion:
		return fmt.Sprintf("its progress was recorded by statement splitter version %d and the file is now split by version %d",
			e.RecordedSplitterVersion, e.SplitterVersion)
	case e.LastStatement >= e.StatementCount:
		return fmt.Sprintf("the file now splits into only %d statements", e.StatementCount)
	default:
		return "the statements it completed no longer match the leading statements of the file as it is split now"
	}
}

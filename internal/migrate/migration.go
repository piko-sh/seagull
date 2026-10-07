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
	"time"
)

// AppliedMigration represents a migration that has been applied to the database, as
// recorded in the migration history table. It is part of the ExecutorPort contract, which
// is allowed to gain fields in a minor release.
type AppliedMigration struct {
	// AppliedAt is when the migration was applied.
	AppliedAt time.Time

	// LastStatement is the 0-based index of the last successfully executed statement.
	//
	// Nil means the migration completed as a single unit without statement-level tracking.
	// The sentinel value -1 stored in the database means no statements have completed yet,
	// which is what the pre-record dirty-migration insert writes before any user SQL runs.
	LastStatement *int

	// Name is the migration name.
	Name string

	// Checksum is the SHA-256 hex digest recorded when the migration was applied.
	Checksum string

	// DownChecksum is the SHA-256 hex digest of the corresponding .down.sql file at the time
	// the up migration was applied. Empty if no down file existed or the migration was
	// applied before this feature.
	DownChecksum string

	// StatementsHash is the lowercase hexadecimal SHA-256 digest an executor recorded of the
	// statements 0..LastStatement, as its splitter divided the file. Empty means none was
	// recorded: the migration ran in a transaction, or its row predates the field.
	StatementsHash string

	// Version is the migration version number.
	Version int64

	// DurationMs is how long the migration took to execute in milliseconds.
	DurationMs int64

	// SplitterVersion is the version of the statement splitter that divided the migration
	// when its progress was last recorded. Zero means none was recorded.
	SplitterVersion int

	// Dirty indicates the migration failed partway through and is in a partially-applied
	// state. A dirty migration must be resolved (retried or manually fixed) before further
	// migrations can be applied.
	Dirty bool
}

// MigrationRecord holds the data needed to execute and record a single migration. It is
// part of the ExecutorPort contract, which is allowed to gain fields in a minor release,
// so construct it with field names.
type MigrationRecord struct {
	// Name is the migration name.
	Name string

	// Checksum is the SHA-256 hex digest of Content.
	Checksum string

	// DownChecksum is the SHA-256 hex digest of the corresponding .down.sql file, if one
	// exists. Empty when no down file is available.
	DownChecksum string

	// StatementsHash is the digest the interrupted run recorded of the statements it
	// completed (AppliedMigration.StatementsHash). Empty for a first run.
	StatementsHash string

	// Content is the SQL to execute.
	Content []byte

	// Version is the migration version number.
	Version int64

	// SkipUpTo is the last completed statement index when resuming a dirty migration. The
	// value -1 starts from the beginning; otherwise execution starts after this index.
	SkipUpTo int

	// SplitterVersion is the splitter version the interrupted run recorded
	// (AppliedMigration.SplitterVersion). Zero for a first run.
	SplitterVersion int

	// Dirty reports that the history holds a dirty row for this migration, left by an
	// interrupted run that this one retries. The executor must update that row rather than
	// insert another, and so runs the migration on its non-transactional path.
	Dirty bool
}

// MigrationStatus combines a migration file with its applied state, used by the Status
// operation to present a unified view.
type MigrationStatus struct {
	// AppliedAt is when the migration was applied (zero value if not applied).
	AppliedAt time.Time

	// LastStatement is the 0-based index of the last successfully applied statement.
	//
	// It is only meaningful when Dirty is true. A value of -1 means the migration was
	// pre-recorded but no statements had completed before the failure.
	LastStatement *int

	// Name is the migration name.
	Name string

	// Filename is the source filename.
	Filename string

	// Version is the migration version number.
	Version int64

	// Applied indicates whether this migration has been applied.
	Applied bool

	// ChecksumMatch indicates whether the file checksum matches the recorded checksum. Only
	// meaningful when Applied is true.
	ChecksumMatch bool

	// HasDownMigration indicates whether a .down.sql file exists for this version.
	HasDownMigration bool

	// Dirty indicates the migration is in a partially-applied state. Only meaningful when
	// Applied is true.
	Dirty bool
}

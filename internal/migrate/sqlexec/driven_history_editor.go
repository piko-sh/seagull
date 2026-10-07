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
	"fmt"
	"time"

	"seagull.sh/seagull/internal/migrate"
)

const (
	// rewritePlaceholderName is the 1-based bind-parameter index for the name column in the
	// history rewrite UPDATE statement.
	rewritePlaceholderName = 1

	// rewritePlaceholderChecksum is the 1-based bind-parameter index for the checksum column
	// in the history rewrite UPDATE statement.
	rewritePlaceholderChecksum = 2

	// rewritePlaceholderDownChecksum is the 1-based bind-parameter index for the
	// down_checksum column in the history rewrite UPDATE statement.
	rewritePlaceholderDownChecksum = 3

	// rewritePlaceholderDirty is the 1-based bind-parameter index for the dirty column in
	// the history rewrite UPDATE statement.
	rewritePlaceholderDirty = 4

	// rewritePlaceholderLastStatement is the 1-based bind-parameter index for the
	// last_statement column in the history rewrite UPDATE statement.
	rewritePlaceholderLastStatement = 5

	// rewritePlaceholderSplitterVersion is the 1-based bind-parameter index for the
	// splitter_version column in the history rewrite UPDATE statement.
	rewritePlaceholderSplitterVersion = 6

	// rewritePlaceholderStatementsHash is the 1-based bind-parameter index for the
	// statements_hash column in the history rewrite UPDATE statement.
	rewritePlaceholderStatementsHash = 7

	// rewritePlaceholderVersion is the 1-based bind-parameter index for the version column
	// in the history rewrite UPDATE statement.
	rewritePlaceholderVersion = 8
)

// historyProgress is what a rewritten history row records about a migration's run.
type historyProgress struct {
	// lastStatement is the 0-based index of the last completed statement, or nil when the
	// row records no statement progress.
	lastStatement *int

	// statementsHash is the digest of the completed statements, or nil with lastStatement.
	statementsHash *string

	// splitterVersion is the splitter version of the progress, or nil with lastStatement.
	splitterVersion *int

	// dirty reports whether the migration is recorded as partly applied.
	dirty bool
}

// SplitStatements divides a migration's content into the statements ExecuteMigration
// runs, under the backend's lexical rules (or its BackendSpec's Split).
//
// Takes content ([]byte) which is the migration's SQL.
//
// Returns []string which holds each statement's text.
// Returns error which wraps ErrMalformedStatement when the content cannot be split. A
// panic in a BackendSpec's Split is returned as an error, with its stack logged once
// through the default logger.
func (executor *Executor) SplitStatements(content []byte) ([]string, error) {
	return executor.backend.engine.split(context.Background(), string(content))
}

// RecordApplied marks a migration as clean and applied without executing it.
//
// It stores the current name and checksums, clears progress, and preserves an existing
// row's applied time and duration.
//
// Takes migration (migrate.MigrationRecord) which holds the version, name and checksums.
//
// Returns error when the history cannot be read or written.
func (executor *Executor) RecordApplied(ctx context.Context, migration migrate.MigrationRecord) error {
	return executor.rewriteHistory(ctx, migration, historyProgress{lastStatement: nil, statementsHash: nil, splitterVersion: nil, dirty: false})
}

// RecordProgress sets a migration's completed statement count for repair.
//
// Zero removes its history row. Otherwise it records dirty progress and the completed
// prefix's digest so the next Up can resume safely.
//
// Takes migration (migrate.MigrationRecord) which holds the version, name, checksums and
// content.
// Takes completed (int) which is how many leading statements ran.
//
// Returns error which wraps migrate.ErrInvalidConfig when completed is not at least zero
// and less than the number of statements, or when the engine's history is append-only and
// so records no progress, or the failure to split the content or write the history.
func (executor *Executor) RecordProgress(ctx context.Context, migration migrate.MigrationRecord, completed int) error {
	statements, splitError := executor.backend.engine.split(ctx, string(migration.Content))
	if splitError != nil {
		return fmt.Errorf("splitting migration %d statements: %w", migration.Version, splitError)
	}
	if completed < 0 || completed >= len(statements) {
		return fmt.Errorf("migration %d has %d statements, so a partial run completed between 0 and %d of them, not %d: %w",
			migration.Version, len(statements), max(len(statements)-1, 0), completed, migrate.ErrInvalidConfig)
	}
	if completed == 0 {
		return executor.deleteHistoryRow(ctx, migration.Version)
	}
	if executor.backend.engine.appendOnlyHistory {
		return fmt.Errorf("%s records no statement progress, so a migration runs again from its first statement; remove its row instead: %w",
			executor.backend.engine.name, migrate.ErrInvalidConfig)
	}
	digest := newStatementDigest()
	for _, statement := range statements[:completed] {
		digest.add(statement)
	}
	return executor.rewriteHistory(ctx, migration, historyProgress{
		lastStatement:   new(completed - 1),
		statementsHash:  new(digest.sum()),
		splitterVersion: new(splitterVersion),
		dirty:           true,
	})
}

// rewriteHistory writes a migration's history row: an UPDATE of the existing row, a
// replacement on an append-only history, or an INSERT when there is none.
//
// Takes migration (migrate.MigrationRecord) which holds the version, name and checksums.
// Takes row (historyProgress) which holds the dirty flag and statement progress to
// record.
//
// Returns error when the history cannot be read or written.
func (executor *Executor) rewriteHistory(ctx context.Context, migration migrate.MigrationRecord, row historyProgress) error {
	exists, existsError := executor.historyRecordExists(ctx, migration.Version)
	if existsError != nil {
		return existsError
	}
	switch {
	case !exists:
		return executor.insertHistoryRow(ctx, migration, row, executor.clock.Now(), 0)
	case executor.backend.engine.appendOnlyHistory:
		return executor.replaceHistoryRow(ctx, migration, row)
	default:
		return executor.updateHistoryRow(ctx, migration, row)
	}
}

// updateHistoryRow replaces the name, checksums, dirty flag and statement progress of an
// existing history row, keeping its applied time and duration.
//
// Takes migration (migrate.MigrationRecord) which holds the version, name and checksums.
// Takes row (historyProgress) which holds the dirty flag and statement progress.
//
// Returns error when the UPDATE fails.
func (executor *Executor) updateHistoryRow(ctx context.Context, migration migrate.MigrationRecord, row historyProgress) error {
	placeholder := executor.backend.placeholder
	updateSQL := fmt.Sprintf(
		"UPDATE %s SET name = %s, checksum = %s, down_checksum = %s, dirty = %s, last_statement = %s, %s = %s, %s = %s WHERE version = %s",
		executor.historyTable(),
		placeholder(rewritePlaceholderName),
		placeholder(rewritePlaceholderChecksum),
		placeholder(rewritePlaceholderDownChecksum),
		placeholder(rewritePlaceholderDirty),
		placeholder(rewritePlaceholderLastStatement),
		splitterVersionColumn, placeholder(rewritePlaceholderSplitterVersion),
		statementsHashColumn, placeholder(rewritePlaceholderStatementsHash),
		placeholder(rewritePlaceholderVersion),
	)
	_, updateError := executor.queryExecutor().ExecContext(ctx, updateSQL,
		migration.Name, migration.Checksum, nullableDownChecksum(migration.DownChecksum), row.dirty,
		nullableValue(row.lastStatement), nullableValue(row.splitterVersion), nullableValue(row.statementsHash), migration.Version,
	)
	if updateError != nil {
		return fmt.Errorf("rewriting the history of migration %d: %w", migration.Version, updateError)
	}
	return nil
}

// replaceHistoryRow replaces the history row of an append-only history: it reads the
// row's applied time and duration and inserts the new row with them.
//
// The history is a ReplacingMergeTree versioned by applied_at, which keeps, of the rows
// for one version, the one with the latest applied_at and, among equals, the one inserted
// last. The new row carries the old row's applied_at, so it replaces the old without a
// delete, and a failed insert leaves the old row in place. Only when the old row's
// applied time cannot be read, so the new row could not be sure to outrank it, is the old
// row removed first with the backend's synchronous delete.
//
// Takes migration (migrate.MigrationRecord) which holds the version, name and checksums.
// Takes row (historyProgress) which holds the dirty flag and statement progress.
//
// Returns error when the row cannot be read, removed or inserted.
func (executor *Executor) replaceHistoryRow(ctx context.Context, migration migrate.MigrationRecord, row historyProgress) error {
	table := executor.historyTable()
	selectSQL := fmt.Sprintf("SELECT applied_at, duration_ms FROM %s%s WHERE version = %s",
		table, executor.backend.engine.historyReadModifier, executor.backend.placeholder(1))
	var rawAppliedAt any
	var durationMs int64
	if scanError := executor.queryExecutor().QueryRowContext(ctx, selectSQL, migration.Version).Scan(&rawAppliedAt, &durationMs); scanError != nil {
		return fmt.Errorf("reading the history of migration %d: %w", migration.Version, scanError)
	}
	var fallbacks appliedAtFallbacks
	appliedAt := fallbacks.parse(rawAppliedAt, migration.Version)
	fallbacks.report(ctx, table)
	if fallbacks.count > 0 {
		if deleteError := executor.deleteHistoryRow(ctx, migration.Version); deleteError != nil {
			return deleteError
		}
	}
	return executor.insertHistoryRow(ctx, migration, row, appliedAt, durationMs)
}

// insertHistoryRow inserts a history row.
//
// Takes migration (migrate.MigrationRecord) which holds the version, name and checksums.
// Takes row (historyProgress) which holds the dirty flag and statement progress.
// Takes appliedAt (time.Time) which is the applied time to record.
// Takes durationMs (int64) which is the duration to record.
//
// Returns error when the INSERT fails.
func (executor *Executor) insertHistoryRow(ctx context.Context, migration migrate.MigrationRecord, row historyProgress, appliedAt time.Time, durationMs int64) error {
	_, insertError := executor.queryExecutor().ExecContext(ctx, executor.historyInsertSQL(),
		migration.Version, migration.Name, migration.Checksum, appliedAt.UTC(), durationMs,
		nullableDownChecksum(migration.DownChecksum), nullableValue(row.lastStatement), row.dirty,
		nullableValue(row.splitterVersion), nullableValue(row.statementsHash),
	)
	if insertError != nil {
		return fmt.Errorf("recording the history of migration %d: %w", migration.Version, insertError)
	}
	return nil
}

// deleteHistoryRow removes a migration's history row, with the engine's synchronous
// delete where it has one. A missing row is not an error.
//
// Takes version (int64) which identifies the row.
//
// Returns error when the DELETE fails.
func (executor *Executor) deleteHistoryRow(ctx context.Context, version int64) error {
	if _, deleteError := executor.queryExecutor().ExecContext(ctx, executor.deleteHistorySQL(), version); deleteError != nil {
		return fmt.Errorf("removing the history of migration %d: %w", version, deleteError)
	}
	return nil
}

// nullableValue returns the value a pointer points to as a bind value, or nil for NULL.
//
// Takes value (*T) which is the optional value.
//
// Returns any which is the value or nil.
func nullableValue[T any](value *T) any {
	if value == nil {
		return nil
	}
	return *value
}

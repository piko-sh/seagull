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
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/migrate"
)

// appliedAtLayouts lists the text forms applied_at comes back in: RFC 3339 from most
// drivers, SQLite's datetime('now'), and Go's Time.String form, which modernc.org/sqlite
// stores when a time.Time is bound to a TEXT column.
var appliedAtLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05",
	"2006-01-02 15:04:05.999999999 -0700 MST",
}

// appliedAtFallbacks counts the applied_at values one history read could not parse, so
// the read warns once, however many rows fell back to the zero time.
type appliedAtFallbacks struct {
	// firstVersion is the version of the first row whose applied_at could not be read.
	firstVersion int64

	// count is how many rows' applied_at could not be read.
	count int
}

// parse converts one row's applied_at, noting the row when the value cannot be read.
//
// Takes raw (any) which is the database driver's applied_at value.
// Takes version (int64) which identifies the row.
//
// Returns time.Time which is the parsed timestamp, or the zero time when it cannot be
// read.
func (fallbacks *appliedAtFallbacks) parse(raw any, version int64) time.Time {
	appliedAt, parsed := parseAppliedAt(raw)
	if !parsed {
		if fallbacks.count == 0 {
			fallbacks.firstVersion = version
		}
		fallbacks.count++
	}
	return appliedAt
}

// report logs one warning when any row's applied_at could not be read, naming the table,
// how many rows fell back to the zero time and the first of them.
//
// Takes table (string) which is the history table read.
func (fallbacks *appliedAtFallbacks) report(ctx context.Context, table string) {
	if fallbacks.count == 0 {
		return
	}
	logging.LoggerFrom(ctx).WarnContext(ctx, "applied_at could not be read, so the zero time is reported in its place",
		slog.String("table", table),
		slog.Int("rows", fallbacks.count),
		slog.Int64("first_version", fallbacks.firstVersion),
	)
}

// AppliedVersions returns all applied migrations ordered by version ascending. A database
// without a history table has applied none, so Status and Validate run no DDL.
//
// Returns []migrate.AppliedMigration which holds the applied migration records.
// Returns error when the query or row scanning fails.
//
// A table an earlier release created, which no Up has upgraded yet, lacks the
// splitter_version and statements_hash columns; its rows are read without them, so the
// migrations it records carry no progress digest.
func (executor *Executor) AppliedVersions(
	ctx context.Context,
) ([]migrate.AppliedMigration, error) {
	runner := executor.queryExecutor()
	table := executor.historyTable()
	return readUnlessMissing(ctx, runner, executor.backend, table, func() ([]migrate.AppliedMigration, error) {
		applied, readError := executor.readAppliedVersions(ctx, runner, table, executor.appliedVersionsSQL(table, true), true)
		if readError == nil {
			return applied, nil
		}
		legacy, legacyError := executor.readAppliedVersions(ctx, runner, table, executor.appliedVersionsSQL(table, false), false)
		if legacyError != nil {
			return nil, readError
		}
		return legacy, nil
	})
}

// readAppliedVersions reads the history table.
//
// Takes runner (queryRunner) which is the connection to read on.
// Takes table (string) which is the history table the query reads.
// Takes query (string) which is the SELECT to run.
// Takes withProgress (bool) which reports that the SELECT reads the splitter_version and
// statements_hash columns.
//
// Returns []migrate.AppliedMigration which holds the applied migration records.
// Returns error when the query or row scanning fails, joined with the failure to close
// the rows when that fails too.
func (*Executor) readAppliedVersions(
	ctx context.Context,
	runner queryRunner,
	table, query string,
	withProgress bool,
) (applied []migrate.AppliedMigration, err error) {
	rows, queryError := runner.QueryContext(ctx, query)
	if queryError != nil {
		return nil, fmt.Errorf("querying applied versions: %w", queryError)
	}
	defer closeRows(rows, &err)

	var fallbacks appliedAtFallbacks
	for rows.Next() {
		migration, scanError := scanAppliedMigration(rows, withProgress, &fallbacks)
		if scanError != nil {
			return nil, scanError
		}
		applied = append(applied, migration)
	}

	if rowsError := rows.Err(); rowsError != nil {
		return nil, fmt.Errorf("iterating applied migrations: %w", rowsError)
	}

	fallbacks.report(ctx, table)
	return applied, nil
}

// updateHistory inserts or deletes a migration record in the migration history table
// depending on the direction.
//
// Takes runner (execContextRunner) which executes the write. The transactional engines
// pass the active *sql.Tx; the non-transactional engines (ClickHouse) pass the pinned
// connection / pool directly.
// Takes migration (migrate.MigrationRecord) which holds the migration metadata.
// Takes direction (migrate.MigrationDirection) which specifies whether this is an up or
// down migration.
// Takes appliedAt (time.Time) which is the timestamp to record.
// Takes durationMs (int64) which is the execution duration in milliseconds.
//
// Returns error when the INSERT or DELETE statement fails.
func (executor *Executor) updateHistory(
	ctx context.Context,
	runner execContextRunner,
	migration migrate.MigrationRecord,
	direction migrate.MigrationDirection,
	appliedAt time.Time,
	durationMs int64,
) error {
	if direction == migrate.MigrationDirectionUp {
		_, insertError := runner.ExecContext(ctx, executor.historyInsertSQL(),
			migration.Version, migration.Name, migration.Checksum, appliedAt.UTC(), durationMs,
			nullableDownChecksum(migration.DownChecksum), nil, false, nil, nil,
		)
		if insertError != nil {
			return fmt.Errorf("inserting migration record: %w", insertError)
		}
		return nil
	}

	_, deleteError := runner.ExecContext(ctx, executor.deleteHistorySQL(), migration.Version)
	if deleteError != nil {
		return fmt.Errorf("deleting migration record: %w", deleteError)
	}
	return nil
}

// deleteHistorySQL builds the statement that removes a single migration history row by
// version, honouring the engine's own statement when it has one (ClickHouse runs a
// synchronous ALTER TABLE ... DELETE mutation) and falling back to a standard DELETE
// otherwise.
//
// Returns string which is the complete delete statement.
func (executor *Executor) deleteHistorySQL() string {
	placeholder := executor.backend.placeholder
	tableName := executor.historyTable()
	if executor.backend.engine.deleteHistory != nil {
		return executor.backend.engine.deleteHistory(tableName, placeholder(1))
	}
	return fmt.Sprintf(
		"DELETE FROM %s WHERE version = %s",
		tableName,
		placeholder(1),
	)
}

// clearDirty marks a non-transactional migration as successfully completed by setting
// dirty = FALSE and recording the final duration.
//
// Takes version (int64) which identifies the migration record.
// Takes start (time.Time) which is when execution began, used to compute the final
// duration.
//
// Returns error when the UPDATE statement fails.
func (executor *Executor) clearDirty(
	ctx context.Context,
	version int64,
	start time.Time,
) error {
	placeholder := executor.backend.placeholder
	durationMs := executor.clock.Now().Sub(start).Milliseconds()
	updateSQL := fmt.Sprintf(
		"UPDATE %s SET dirty = %s, duration_ms = %s WHERE version = %s",
		executor.historyTable(),
		placeholder(clearDirtyPlaceholderDirty),
		placeholder(clearDirtyPlaceholderDurationMs),
		placeholder(clearDirtyPlaceholderVersion),
	)

	_, updateError := executor.queryExecutor().ExecContext(ctx, updateSQL, false, durationMs, version)
	if updateError != nil {
		return fmt.Errorf("clearing dirty flag for migration %d: %w", version, updateError)
	}

	return nil
}

// readUnlessMissing runs a history read, treating a missing table as an empty history.
//
// Only a failed read asks whether its table exists, so a healthy read costs no extra
// query. When the table exists, the read runs once more in case another process created
// it after the first read began.
//
// Takes runner (queryRunner) which is the connection the read runs on.
// Takes backend (Backend) which renders the existence query.
// Takes table (string) which is the table the read uses.
// Takes read (func() ([]T, error)) which reads the history.
//
// Returns []T which holds the rows read, or none when the table does not exist.
// Returns error when the read fails for any other reason.
func readUnlessMissing[T any](
	ctx context.Context,
	runner queryRunner,
	backend Backend,
	table string,
	read func() ([]T, error),
) ([]T, error) {
	rows, readError := read()
	if readError == nil {
		return rows, nil
	}
	var count int64
	existsSQL := backend.engine.tableExists(backend.placeholder(1))
	if probeError := runner.QueryRowContext(ctx, existsSQL, table).Scan(&count); probeError != nil {
		return nil, readError
	}
	if count == 0 {
		return nil, nil
	}
	return read()
}

// scanAppliedMigration reads one history row.
//
// Takes rows (*sql.Rows) which is positioned on the row.
// Takes withProgress (bool) which reports that the row holds the splitter_version and
// statements_hash columns after the original eight.
// Takes fallbacks (*appliedAtFallbacks) which notes a row whose applied_at cannot be
// read.
//
// Returns migrate.AppliedMigration which is the row's migration.
// Returns error when the row cannot be scanned.
func scanAppliedMigration(rows *sql.Rows, withProgress bool, fallbacks *appliedAtFallbacks) (migrate.AppliedMigration, error) {
	var migration migrate.AppliedMigration
	var downChecksum, statementsHash sql.NullString
	var lastStatement, recordedSplitterVersion sql.NullInt32
	var dirty sql.NullBool
	var appliedAtRaw any
	destinations := []any{
		&migration.Version,
		&migration.Name,
		&migration.Checksum,
		&appliedAtRaw,
		&migration.DurationMs,
		&downChecksum,
		&lastStatement,
		&dirty,
	}
	if withProgress {
		destinations = append(destinations, &recordedSplitterVersion, &statementsHash)
	}
	if scanError := rows.Scan(destinations...); scanError != nil {
		return migration, fmt.Errorf("scanning applied migration: %w", scanError)
	}
	migration.AppliedAt = fallbacks.parse(appliedAtRaw, migration.Version)
	migration.DownChecksum = downChecksum.String
	if lastStatement.Valid {
		migration.LastStatement = new(int(lastStatement.Int32))
	}
	migration.Dirty = dirty.Valid && dirty.Bool
	migration.SplitterVersion = int(recordedSplitterVersion.Int32)
	migration.StatementsHash = statementsHash.String
	return migration, nil
}

// parseAppliedAt converts the raw applied_at value from the database into a time.Time,
// handling native time.Time (PostgreSQL), string formats (SQLite), []byte values (MySQL
// frequently returns applied_at as a byte slice) and Unix seconds.
//
// A NULL, a text in no known layout, a number out of range or a value of another driver
// type cannot be read; the zero time is returned for it so the migration record stays
// usable for version and checksum equality checks, and the caller reports it once per
// read through appliedAtFallbacks.
//
// Takes raw (any) which is the database driver's applied_at value.
//
// Returns time.Time which is the parsed timestamp, or the zero time when it cannot be
// read.
// Returns bool which is false when the value cannot be read.
func parseAppliedAt(raw any) (time.Time, bool) {
	switch value := raw.(type) {
	case time.Time:
		return value, true
	case []byte:
		return parseAppliedAtText(string(value))
	case string:
		return parseAppliedAtText(value)
	case int64:
		return time.Unix(value, 0).UTC(), true
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) || value >= math.MaxInt64 || value <= math.MinInt64 {
			return time.Time{}, false
		}
		return time.Unix(int64(value), 0).UTC(), true
	default:
		return time.Time{}, false
	}
}

// parseAppliedAtText parses a textual applied_at value in any of the known layouts,
// ignoring the monotonic clock reading Time.String appends.
//
// Takes value (string) which is the stored text.
//
// Returns time.Time which is the parsed timestamp.
// Returns bool which is false when no layout matches.
func parseAppliedAtText(value string) (time.Time, bool) {
	value, _, _ = strings.Cut(value, " m=")
	for _, layout := range appliedAtLayouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// closeRows closes rows as a deferred clean-up, joining a close failure into the
// function's result.
//
// Takes rows (*sql.Rows) which are the rows to close.
// Takes err (*error) which is the function's result, joined with the close failure.
func closeRows(rows *sql.Rows, err *error) {
	if closeError := rows.Close(); closeError != nil {
		*err = errors.Join(*err, fmt.Errorf("closing rows: %w", closeError))
	}
}

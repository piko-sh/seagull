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
	"slices"
	"strings"
	"unicode"

	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/migrate"
	"seagull.sh/seagull/internal/sqllex"
)

// errUnsafeSettingValue is returned when a session setting reads back as a value that
// cannot be written into the statement that restores it.
var errUnsafeSettingValue = errors.New("the session setting holds a value that cannot be restored safely")

// sessionSettings describes the session settings an engine ignores inside a transaction.
// It says how to recognise a statement changing one, how to read its value before a
// migration and how to restore it after, so a migration cannot leave a pooled connection
// changed.
type sessionSettings struct {
	// matches reports whether a statement's significant tokens change such a setting.
	matches func(tokens []sqllex.Token) bool

	// read selects the setting's current value.
	read string

	// restore sets the setting back to a value read, which it takes as its one verb.
	restore string
}

// sqliteSessionSettings describes PRAGMA foreign_keys, which SQLite ignores inside a
// transaction. A migration disabling foreign keys to rebuild a table, as SQLite's
// documentation advises, would otherwise run the rebuild with them still enforced and let
// DROP TABLE delete the rows that refer to the table.
var sqliteSessionSettings = &sessionSettings{
	matches: func(tokens []sqllex.Token) bool {
		return len(tokens) >= 2 && tokens[0].IsWord("PRAGMA") && tokens[1].IsWord("foreign_keys")
	},
	read:    "PRAGMA foreign_keys",
	restore: "PRAGMA foreign_keys = %s",
}

// sessionStatements returns the indexes of a migration's statements that change a session
// setting the engine ignores inside a transaction.
//
// Takes statements ([]string) which holds the migration's statements.
//
// Returns map[int]bool which holds the indexes, or nil when there are none.
func (e *engine) sessionStatements(statements []string) map[int]bool {
	if e.sessionSettings == nil {
		return nil
	}
	var indexes map[int]bool
	for index, statement := range statements {
		if !e.sessionSettings.matches(sqllex.SignificantTokens(e.rules, statement)) {
			continue
		}
		if indexes == nil {
			indexes = make(map[int]bool)
		}
		indexes[index] = true
	}
	return indexes
}

// pinSession runs the rest of a migration on one connection and reads the session setting
// so it can be restored after. The connection is the session lock's or work connection
// when the run holds one, and otherwise one taken from the pool for the migration.
//
// Returns func() which restores the setting and releases a connection taken from the
// pool. Call it once the migration has ended.
// Returns error when no connection can be taken or the setting cannot be read.
//
// Concurrency: sets the pinned connection under the executor mutex.
func (executor *Executor) pinSession(ctx context.Context) (func(), error) {
	settings := executor.backend.engine.sessionSettings
	runner := executor.queryExecutor()
	var connection *sql.Conn
	if pool, isPool := runner.(*sql.DB); isPool {
		taken, err := pool.Conn(ctx)
		if err != nil {
			return nil, fmt.Errorf("taking a connection for the migration's session settings: %w", err)
		}
		connection, runner = taken, taken
	}
	value, readError := readSessionSetting(ctx, runner, settings)
	if readError != nil {
		if connection != nil {
			discardConnection(connection)
		}
		return nil, readError
	}
	if connection != nil {
		executor.mutex.Lock()
		executor.pinned = connection
		executor.mutex.Unlock()
	}
	return func() { executor.restoreSession(ctx, runner, connection, fmt.Sprintf(settings.restore, value)) }, nil
}

// restoreSession runs the statement that restores a session setting and releases a
// connection pinSession took from the pool. A connection whose setting could not be
// restored is discarded rather than returned to the pool, and the failure is logged, as
// the migration itself has already ended.
//
// Takes runner (queryRunner) which is the connection the migration ran on.
// Takes connection (*sql.Conn) which is the connection taken from the pool, or nil.
// Takes restore (string) which restores the setting.
//
// Concurrency: clears the pinned connection under the executor mutex.
func (executor *Executor) restoreSession(ctx context.Context, runner queryRunner, connection *sql.Conn, restore string) {
	ctx, logger := logging.From(ctx)
	restoreContext, cancel := checkpointContext(ctx)
	defer cancel()
	_, restoreError := runner.ExecContext(restoreContext, restore)
	if restoreError != nil {
		logger.WarnContext(ctx, "restoring a session setting after the migration failed",
			logging.ErrAttr(withContextCause(restoreContext, restoreError)))
	}
	if connection == nil {
		return
	}
	executor.mutex.Lock()
	executor.pinned = nil
	executor.mutex.Unlock()
	if restoreError != nil {
		discardConnection(connection)
		return
	}
	_ = connection.Close()
}

// readSessionSetting reads a session setting's value and checks that it can be written
// back into the statement that restores it.
//
// Takes runner (queryRunner) which is the connection the migration runs on.
// Takes settings (*sessionSettings) which says how to read the setting.
//
// Returns string which is the value read.
// Returns error when the setting cannot be read or holds anything but letters and digits.
func readSessionSetting(ctx context.Context, runner queryRunner, settings *sessionSettings) (string, error) {
	var value string
	if err := runner.QueryRowContext(ctx, settings.read).Scan(&value); err != nil {
		return "", fmt.Errorf("reading the session setting before the migration: %w", err)
	}
	notPlain := func(character rune) bool { return !unicode.IsLetter(character) && !unicode.IsDigit(character) }
	if value == "" || strings.ContainsFunc(value, notPlain) {
		return "", fmt.Errorf("reading %q: %w", value, errUnsafeSettingValue)
	}
	return value, nil
}

// settingsAroundTransaction splits a transactional migration's session statements into
// those it opens with, which run before the transaction begins, and those it closes with,
// which run after it commits.
//
// Takes run (*migrationRun) which is the migration, its session statements marked.
//
// Returns []int which holds the indexes of the statements to run before.
// Returns []int which holds the indexes of the statements to run after.
// Returns error which wraps migrate.ErrSettingInsideTransaction for a session statement
// between other statements.
func settingsAroundTransaction(run *migrationRun) (before, after []int, err error) {
	index := 0
	for index < len(run.statements) && run.session[index] {
		before = append(before, index)
		index++
	}
	last := len(run.statements) - 1
	for last >= index && run.session[last] {
		after = append(after, last)
		last--
	}
	slices.Reverse(after)
	for middle := index; middle <= last; middle++ {
		if run.session[middle] {
			return nil, nil, fmt.Errorf("migration %d (%s), statement %d: %q runs between other statements, where the engine ignores it inside the migration's transaction. "+
				"Move it to the start or the end of the migration, or mark the migration no_transaction: %w",
				run.record.Version, run.record.Name, middle+1, strings.TrimSpace(run.statements[middle]), migrate.ErrSettingInsideTransaction)
		}
	}
	return before, after, nil
}

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
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"time"

	"seagull.sh/seagull/internal/migrate"
)

// splitterVersion identifies the statement-splitting rules used by migration progress
// records.
//
// Increment it when statement boundaries can change so interrupted migrations cannot
// resume under incompatible rules.
const splitterVersion = 1

// statementDigest accumulates the SHA-256 digest of a migration's leading statements.
// Each statement contributes its length as an eight-byte big-endian integer followed by
// its text, so no two different statement lists share an input.
type statementDigest struct {
	// hash is the running SHA-256 state.
	hash hash.Hash
}

// newStatementDigest returns the digest of no statements.
//
// Returns statementDigest which is ready to add statements to.
func newStatementDigest() statementDigest {
	return statementDigest{hash: sha256.New()}
}

// add appends one statement to the digest.
//
// Takes statement (string) which is the statement's text.
func (digest statementDigest) add(statement string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(statement)))
	_, _ = digest.hash.Write(length[:])
	_, _ = digest.hash.Write([]byte(statement))
}

// sum returns the digest of the statements added so far, leaving the state unchanged.
//
// Returns string which is the lowercase hexadecimal SHA-256 digest.
func (digest statementDigest) sum() string {
	return hex.EncodeToString(digest.hash.Sum(nil))
}

// migrationRun is one execution of a migration: its record, its statements as the backend
// splits them now, and when it began.
type migrationRun struct {
	// start is when the execution began.
	start time.Time

	// digest holds the digest of the statements the run has completed, including any an
	// interrupted run completed before it.
	digest statementDigest

	// statements holds the migration's statements in order.
	statements []string

	// record is the migration as the Migrator passed it.
	record migrate.MigrationRecord
}

// upgradeHistoryTable adds each of historyColumnAdditions that the history table lacks.
// It runs under the migration lock, after the table is created, so a table created by an
// earlier release gains the columns before anything writes to them.
//
// A column is probed with a SELECT that reads it and no rows, which fails when the column
// does not exist; no engine's error text is interpreted. When adding a column fails, the
// probe runs again, so a column another process added in the meantime is not an error.
//
// Takes runner (queryRunner) which is the connection the migrations run on.
//
// Returns error when a missing column cannot be added.
func (executor *Executor) upgradeHistoryTable(ctx context.Context, runner queryRunner) error {
	table := executor.historyTable()
	for _, column := range historyColumnAdditions {
		if historyHasColumn(ctx, runner, table, column) {
			continue
		}
		if _, addError := runner.ExecContext(ctx, executor.backend.engine.addHistoryColumn(table, column)); addError != nil {
			if historyHasColumn(ctx, runner, table, column) {
				continue
			}
			return fmt.Errorf("adding column %s to %s: %w", column, table, addError)
		}
	}
	return nil
}

// resumeDigest checks that a retried migration can resume where its interrupted run
// stopped, and returns the digest of the statements that run completed.
//
// A record whose SkipUpTo is -1 resumes nothing and always passes. Otherwise the record
// must carry the current splitterVersion and the digest of the statements 0..SkipUpTo as
// they split now; anything else means the statements it completed cannot be matched to
// the current split.
//
// Takes record (migrate.MigrationRecord) which is the migration to run.
// Takes statements ([]string) which holds its statements as the backend splits them now.
//
// Returns statementDigest which holds the digest of the statements already completed.
// Returns error which is a *migrate.ResumeMismatchError when resuming is unsafe.
func resumeDigest(record migrate.MigrationRecord, statements []string) (statementDigest, error) {
	digest := newStatementDigest()
	if record.SkipUpTo < 0 {
		return digest, nil
	}
	mismatch := &migrate.ResumeMismatchError{
		Name:                    record.Name,
		Version:                 record.Version,
		LastStatement:           record.SkipUpTo,
		RecordedSplitterVersion: record.SplitterVersion,
		SplitterVersion:         splitterVersion,
		StatementCount:          len(statements),
	}
	if record.SplitterVersion != splitterVersion || record.SkipUpTo >= len(statements) {
		return digest, mismatch
	}
	for _, statement := range statements[:record.SkipUpTo+1] {
		digest.add(statement)
	}
	if digest.sum() != record.StatementsHash {
		return digest, mismatch
	}
	return digest, nil
}

// historyHasColumn reports whether a table has a column, by reading the column from no
// rows.
//
// Takes runner (queryRunner) which is the connection to probe on.
// Takes table (string) which is the validated table name.
// Takes column (string) which is the column name.
//
// Returns bool which is true when the query succeeds.
func historyHasColumn(ctx context.Context, runner queryRunner, table, column string) bool {
	rows, queryError := runner.QueryContext(ctx, fmt.Sprintf("SELECT %s FROM %s WHERE 1 = 0", column, table))
	if queryError != nil {
		return false
	}
	closeError := rows.Close()
	return closeError == nil && rows.Err() == nil
}

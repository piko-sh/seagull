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

package source

import (
	"context"
	"database/sql"
	"errors"
)

// sglClickHouseBatches writes rows through clickhouse-go in separately committed chunks.
//
// Each chunk prepares the INSERT once in a transaction, then sends its rows as one block
// on commit. A failed chunk writes no rows, while earlier chunks stay written.
//
// Takes db (*sql.DB) which the transactions are begun on.
// Takes query (string) which is the INSERT, whose VALUES row the driver drops.
// Takes rows (int) which is how many rows the batch writes.
// Takes chunk (int) which is how many rows one chunk holds.
// Takes appendRow (func(*sql.Stmt, int) error) which appends the row at an index.
//
// Returns int which is the index of the first row of the chunk that failed.
// Returns error when a transaction cannot be begun or committed, the INSERT cannot be
// prepared, or a row cannot be appended.
func sglClickHouseBatches(ctx context.Context, db *sql.DB, query string, rows, chunk int, appendRow func(*sql.Stmt, int) error) (int, error) {
	for offset := 0; offset < rows; offset += chunk {
		if err := sglClickHouseBatch(ctx, db, query, offset, min(offset+chunk, rows), appendRow); err != nil {
			return offset, err
		}
	}
	return 0, nil
}

// sglClickHouseBatch appends one chunk's rows in a transaction of their own and commits
// it, or rolls it back, sending nothing, when a row cannot be appended.
//
// Takes db (*sql.DB) which the transaction is begun on.
// Takes query (string) which is the INSERT.
// Takes offset (int) which is the index of the chunk's first row.
// Takes end (int) which is the index past its last row.
// Takes appendRow (func(*sql.Stmt, int) error) which appends one row.
//
// Returns error when the chunk cannot be written.
func sglClickHouseBatch(ctx context.Context, db *sql.DB, query string, offset, end int, appendRow func(*sql.Stmt, int) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	statement, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return errors.Join(err, tx.Rollback())
	}
	for index := offset; index < end; index++ {
		if err := appendRow(statement, index); err != nil {
			return errors.Join(err, statement.Close(), tx.Rollback())
		}
	}
	if err := statement.Close(); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

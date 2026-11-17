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
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type batchLog struct {
	pending   []driver.Value
	committed [][]driver.Value
	rolled    int
}

type batchConn struct{ log *batchLog }

func (conn batchConn) Prepare(string) (driver.Stmt, error) { return batchStmt(conn), nil }
func (batchConn) Close() error                             { return nil }
func (conn batchConn) Begin() (driver.Tx, error)           { return batchTx(conn), nil }
func (batchConn) CheckNamedValue(*driver.NamedValue) error { return nil }

type batchTx batchConn

func (tx batchTx) Commit() error {
	tx.log.committed = append(tx.log.committed, tx.log.pending)
	tx.log.pending = nil
	return nil
}

func (tx batchTx) Rollback() error {
	tx.log.pending = nil
	tx.log.rolled++
	return nil
}

type batchStmt batchConn

func (batchStmt) Close() error  { return nil }
func (batchStmt) NumInput() int { return -1 }
func (statement batchStmt) Exec(values []driver.Value) (driver.Result, error) {
	if values[0] == "refused" {
		return nil, errors.New("refused")
	}
	statement.log.pending = append(statement.log.pending, values[0])
	return driver.RowsAffected(0), nil
}
func (batchStmt) Query([]driver.Value) (driver.Rows, error) { return nil, errors.New("unsupported") }

type batchConnector struct{ log *batchLog }

func (connector batchConnector) Connect(context.Context) (driver.Conn, error) {
	return batchConn(connector), nil
}
func (batchConnector) Driver() driver.Driver { return nil }

func TestClickHouseBatchesCommitEachChunk(t *testing.T) {
	t.Parallel()

	log := &batchLog{}
	db := sql.OpenDB(batchConnector{log: log})
	t.Cleanup(func() { _ = db.Close() })
	rows := []string{"a", "b", "c", "refused", "e"}
	failed, err := sglClickHouseBatches(t.Context(), db, "INSERT INTO t (v) VALUES (?)", len(rows), 2, func(statement *sql.Stmt, index int) error {
		_, err := statement.ExecContext(t.Context(), rows[index])
		return err
	})
	require.ErrorContains(t, err, "refused")
	require.Equal(t, 2, failed, "the chunk starting at row 2 failed")
	require.Equal(t, [][]driver.Value{{"a", "b"}}, log.committed, "the chunk before it stays written")
	require.Equal(t, 1, log.rolled)
}

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

package sqlrecorder

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

const recorderDriverName = "sqlrecorder"

var registerRecorder = sync.OnceFunc(func() { sql.Register(recorderDriverName, recordingDriver{}) })

func TestRecorderKeepsEveryStatementInOrder(t *testing.T) {
	ctx := t.Context()
	database := Connect()
	defer func() { require.NoError(t, database.Close()) }()
	Take()

	_, err := database.ExecContext(ctx, "DELETE FROM items WHERE id = $1", int64(7))
	require.NoError(t, err)
	rows, err := database.QueryContext(ctx, "SELECT id FROM items WHERE kind = :kind", sql.Named("kind", "a"))
	require.NoError(t, err)
	require.False(t, rows.Next())
	require.NoError(t, rows.Close())
	var id int64
	err = database.QueryRowContext(ctx, "SELECT id FROM items WHERE name = ?", []string{"x"}).Scan(&id)
	require.True(t, errors.Is(err, sql.ErrNoRows))

	require.Equal(t, []Statement{
		{SQL: "DELETE FROM items WHERE id = $1", Arguments: []any{int64(7)}},
		{SQL: "SELECT id FROM items WHERE kind = :kind", Arguments: []any{sql.Named("kind", "a")}},
		{SQL: "SELECT id FROM items WHERE name = ?", Arguments: []any{[]string{"x"}}},
	}, Take())
	require.Empty(t, Take())
}

func TestRecorderRecordsPreparedStatements(t *testing.T) {
	ctx := t.Context()
	database := Connect()
	defer func() { require.NoError(t, database.Close()) }()
	statement, err := database.PrepareContext(ctx, "INSERT INTO items (id) VALUES ($1)")
	require.NoError(t, err)
	Take()

	result, err := statement.ExecContext(ctx, int64(3))
	require.NoError(t, err)
	affected, err := result.RowsAffected()
	require.NoError(t, err)
	require.Zero(t, affected)
	require.NoError(t, statement.Close())

	require.Equal(t, []Statement{{SQL: "INSERT INTO items (id) VALUES ($1)", Arguments: []any{int64(3)}}}, Take())
}

func TestRecorderRecordsWhatEachDatabaseSQLCallSends(t *testing.T) {
	registerRecorder()
	database, err := sql.Open(recorderDriverName, "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, database.Close()) })
	require.Equal(t, recordingDriver{}, database.Driver())
	Take()

	testCases := []struct {
		name     string
		run      func(ctx context.Context, database *sql.DB) error
		expected []Statement
	}{
		{
			name: "Exec records the statement and reports no change",
			run: func(ctx context.Context, database *sql.DB) error {
				result, err := database.ExecContext(ctx, "UPDATE items SET kind = $1 WHERE id = $2", "a", int64(7))
				if err != nil {
					return err
				}
				inserted, err := result.LastInsertId()
				if err != nil || inserted != 0 {
					return errors.Join(err, errors.New("an insert was reported"))
				}
				affected, err := result.RowsAffected()
				if err != nil || affected != 0 {
					return errors.Join(err, errors.New("a change was reported"))
				}
				return nil
			},
			expected: []Statement{{SQL: "UPDATE items SET kind = $1 WHERE id = $2", Arguments: []any{"a", int64(7)}}},
		},
		{
			name: "Query records the statement and answers with no rows or columns",
			run: func(ctx context.Context, database *sql.DB) error {
				rows, err := database.QueryContext(ctx, "SELECT id FROM items WHERE kind = @kind", sql.Named("kind", "a"))
				if err != nil {
					return err
				}
				columns, err := rows.Columns()
				if err != nil || len(columns) != 0 || rows.Next() {
					return errors.Join(err, errors.New("the result set was not empty"))
				}
				return errors.Join(rows.Err(), rows.Close())
			},
			expected: []Statement{{SQL: "SELECT id FROM items WHERE kind = @kind", Arguments: []any{sql.Named("kind", "a")}}},
		},
		{
			name: "QueryRow records the statement and finds no row",
			run: func(ctx context.Context, database *sql.DB) error {
				var id int64
				err := database.QueryRowContext(ctx, "SELECT id FROM items WHERE id = ?", int64(3)).Scan(&id)
				if !errors.Is(err, sql.ErrNoRows) {
					return errors.Join(err, errors.New("a row was found"))
				}
				return nil
			},
			expected: []Statement{{SQL: "SELECT id FROM items WHERE id = ?", Arguments: []any{int64(3)}}},
		},
		{
			name: "a committed transaction records each statement in order",
			run: func(ctx context.Context, database *sql.DB) error {
				transaction, err := database.BeginTx(ctx, nil)
				if err != nil {
					return err
				}
				if _, err := transaction.ExecContext(ctx, "INSERT INTO items (id) VALUES ($1)", int64(1)); err != nil {
					return errors.Join(err, transaction.Rollback())
				}
				rows, err := transaction.QueryContext(ctx, "SELECT id FROM items")
				if err != nil {
					return errors.Join(err, transaction.Rollback())
				}
				if err := rows.Close(); err != nil {
					return errors.Join(err, transaction.Rollback())
				}
				return transaction.Commit()
			},
			expected: []Statement{
				{SQL: "INSERT INTO items (id) VALUES ($1)", Arguments: []any{int64(1)}},
				{SQL: "SELECT id FROM items", Arguments: []any{}},
			},
		},
		{
			name: "a rolled back transaction still records what it sent",
			run: func(ctx context.Context, database *sql.DB) error {
				transaction, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault, ReadOnly: false})
				if err != nil {
					return err
				}
				if _, err := transaction.ExecContext(ctx, "DELETE FROM items"); err != nil {
					return errors.Join(err, transaction.Rollback())
				}
				return transaction.Rollback()
			},
			expected: []Statement{{SQL: "DELETE FROM items", Arguments: []any{}}},
		},
		{
			name: "a prepared statement records each run",
			run: func(ctx context.Context, database *sql.DB) error {
				statement, err := database.PrepareContext(ctx, "SELECT id FROM items WHERE kind = $1")
				if err != nil {
					return err
				}
				rows, err := statement.QueryContext(ctx, "b")
				if err != nil {
					return errors.Join(err, statement.Close())
				}
				return errors.Join(rows.Close(), statement.Close())
			},
			expected: []Statement{{SQL: "SELECT id FROM items WHERE kind = $1", Arguments: []any{"b"}}},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require.NoError(t, testCase.run(t.Context(), database))

			require.Equal(t, testCase.expected, Take())
		})
	}
}

func TestRecorderRecordsTheDriversContextFreeCallsAsTheContextOnes(t *testing.T) {
	testCases := []struct {
		name     string
		run      func() error
		expected []Statement
	}{
		{
			name: "a statement executed without a context",
			run: func() error {
				_, err := preparedStatement{query: "DELETE FROM items WHERE id = $1"}.Exec([]driver.Value{int64(4)})
				return err
			},
			expected: []Statement{{SQL: "DELETE FROM items WHERE id = $1", Arguments: []any{int64(4)}}},
		},
		{
			name: "a statement queried without a context",
			run: func() error {
				rows, err := preparedStatement{query: "SELECT id FROM items WHERE kind = $1"}.Query([]driver.Value{"c"})
				if err != nil {
					return err
				}
				if !errors.Is(rows.Next(nil), io.EOF) {
					return errors.New("a row was returned")
				}
				return rows.Close()
			},
			expected: []Statement{{SQL: "SELECT id FROM items WHERE kind = $1", Arguments: []any{"c"}}},
		},
		{
			name: "a transaction begun without a context records nothing",
			run: func() error {
				transaction, err := connection{}.Begin()
				if err != nil {
					return err
				}
				return transaction.Commit()
			},
			expected: nil,
		},
		{
			name: "a statement prepared on a connection takes any number of arguments",
			run: func() error {
				statement, err := connection{}.Prepare("SELECT 1")
				if err != nil {
					return err
				}
				if statement.NumInput() != -1 {
					return errors.New("the statement limits its arguments")
				}
				return errors.Join(statement.Close(), connection{}.Close())
			},
			expected: nil,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			Take()

			require.NoError(t, testCase.run())

			require.Equal(t, testCase.expected, Take())
		})
	}
}

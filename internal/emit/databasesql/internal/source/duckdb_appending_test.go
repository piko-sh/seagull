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

type appendingConn struct{}

func (appendingConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unsupported") }
func (appendingConn) Close() error                        { return nil }
func (appendingConn) Begin() (driver.Tx, error)           { return nil, errors.New("unsupported") }

func (appendingConn) CheckNamedValue(value *driver.NamedValue) error {
	if _, list := value.Value.([]string); list {
		return nil
	}
	return driver.ErrSkip
}

type appendingConnector struct{}

func (appendingConnector) Connect(context.Context) (driver.Conn, error) { return appendingConn{}, nil }
func (appendingConnector) Driver() driver.Driver                        { return nil }

type recordingAppender struct {
	failAt  int
	rows    [][]driver.Value
	flushed [][][]driver.Value
	cleared int
}

func (appender *recordingAppender) AppendRow(values ...driver.Value) error {
	if appender.failAt >= 0 && values[0] == int64(appender.failAt) {
		return errors.New("refused")
	}
	appender.rows = append(appender.rows, values)
	return nil
}

func (appender *recordingAppender) Clear() error {
	appender.rows = nil
	appender.cleared++
	return nil
}

func (appender *recordingAppender) Close() error {
	if appender.rows != nil {
		appender.flushed = append(appender.flushed, appender.rows)
	}
	appender.rows = nil
	return nil
}

type nullableText struct {
	text  string
	valid bool
}

func (value nullableText) Value() (driver.Value, error) {
	if !value.valid {
		return nil, nil
	}
	return value.text, nil
}

func appendRows(t *testing.T, ctx context.Context, rows, chunk int, appender *recordingAppender) (bool, int, error) {
	t.Helper()
	db := sql.OpenDB(appendingConnector{})
	t.Cleanup(func() { _ = db.Close() })
	open := func(driver.Conn) (sglDuckDBAppender, error) { return appender, nil }
	return sglDuckDBAppend(ctx, db, rows, chunk, open, func(appender sglDuckDBAppender, checker driver.NamedValueChecker, index int) error {
		label := "row"
		return sglDuckDBAppendRow(appender, checker, index, int32(index), &label, nullableText{text: "t", valid: index%2 == 0}, []string{"a"})
	})
}

func TestDuckDBAppendConvertsEachValueAsDatabaseSQLDoes(t *testing.T) {
	t.Parallel()

	appender := &recordingAppender{failAt: -1}
	appended, failed, err := appendRows(t, t.Context(), 3, 2, appender)
	require.NoError(t, err)
	require.True(t, appended)
	require.Zero(t, failed)
	require.Len(t, appender.flushed, 2, "one flush for each chunk")
	require.Equal(t, []driver.Value{int64(0), int64(0), "row", "t", []string{"a"}}, appender.flushed[0][0])
	require.Equal(t, []driver.Value{int64(1), int64(1), "row", nil, []string{"a"}}, appender.flushed[0][1])
	require.Len(t, appender.flushed[1], 1)
}

func TestDuckDBAppendDropsTheChunkHoldingARowItCannotAppend(t *testing.T) {
	t.Parallel()

	appender := &recordingAppender{failAt: 3}
	_, failed, err := appendRows(t, t.Context(), 5, 2, appender)
	require.ErrorContains(t, err, "refused")
	require.Equal(t, 2, failed, "the chunk starting at row 2 failed")
	require.Len(t, appender.flushed, 1, "the chunk before it stays written")
	require.Equal(t, 1, appender.cleared)
}

func TestDuckDBAppendStopsWhenTheContextIsDone(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	appender := &recordingAppender{failAt: -1}
	_, _, err := appendRows(t, ctx, 2, 2, appender)
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, appender.flushed)
}

func TestDuckDBAppendLeavesTheStatementsToAConnectionWithoutAnAppender(t *testing.T) {
	t.Parallel()

	db := sql.OpenDB(appendingConnector{})
	t.Cleanup(func() { _ = db.Close() })
	open := func(driver.Conn) (sglDuckDBAppender, error) { return nil, errors.New("no appender here") }
	appended, failed, err := sglDuckDBAppend(t.Context(), db, 3, 2, open, func(sglDuckDBAppender, driver.NamedValueChecker, int) error {
		return errors.New("no row should be appended")
	})
	require.NoError(t, err)
	require.False(t, appended)
	require.Zero(t, failed)
}

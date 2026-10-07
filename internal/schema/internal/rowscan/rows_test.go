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

package rowscan_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/schema/internal/rowscan"
)

var errUnsupported = errors.New("fake database: operation not supported")

type result struct {
	queryError error
	nextError  error
	closeError error
	columns    []string
	rows       [][]driver.Value
}

type connector struct {
	result result
}

type fakeDriver struct{}

type connection struct {
	result result
}

type rows struct {
	result   result
	position int
}

func (source connector) Connect(context.Context) (driver.Conn, error) {
	return connection(source), nil
}

func (connector) Driver() driver.Driver {
	return fakeDriver{}
}

func (fakeDriver) Open(string) (driver.Conn, error) {
	return nil, errUnsupported
}

func (connection) Prepare(string) (driver.Stmt, error) {
	return nil, errUnsupported
}

func (connection) Close() error {
	return nil
}

func (connection) Begin() (driver.Tx, error) {
	return nil, errUnsupported
}

func (source connection) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	if source.result.queryError != nil {
		return nil, source.result.queryError
	}
	return &rows{result: source.result, position: 0}, nil
}

func (set *rows) Columns() []string {
	return set.result.columns
}

func (set *rows) Close() error {
	return set.result.closeError
}

func (set *rows) Next(destination []driver.Value) error {
	if set.position >= len(set.result.rows) {
		if set.result.nextError != nil {
			return set.result.nextError
		}
		return io.EOF
	}
	copy(destination, set.result.rows[set.position])
	set.position++
	return nil
}

func openDatabase(t *testing.T, answer result) *sql.DB {
	t.Helper()

	database := sql.OpenDB(connector{result: answer})
	t.Cleanup(func() { assert.NoError(t, database.Close()) })
	return database
}

func TestStringsCollectsOneTextColumnAndReportsEveryFailure(t *testing.T) {
	t.Parallel()

	queryFailure := errors.New("permission denied")
	iterationFailure := errors.New("connection reset while reading")
	closeFailure := errors.New("connection reset while closing")
	tests := []struct {
		wantError error
		name      string
		answer    result
		want      []string
		wantText  string
	}{
		{
			name:   "rows in order",
			answer: result{columns: []string{"name"}, rows: [][]driver.Value{{"b"}, {"a"}}},
			want:   []string{"b", "a"},
		},
		{name: "no rows", answer: result{columns: []string{"name"}}, want: nil},
		{name: "query fails", answer: result{queryError: queryFailure}, wantError: queryFailure},
		{name: "iteration fails", answer: result{columns: []string{"name"}, nextError: iterationFailure}, wantError: iterationFailure},
		{
			name:      "closing after the last row fails",
			answer:    result{columns: []string{"name"}, rows: [][]driver.Value{{"a"}}, closeError: closeFailure},
			wantError: closeFailure,
		},
		{
			name:      "closing after a row that does not scan fails",
			answer:    result{columns: []string{"name"}, rows: [][]driver.Value{{nil}}, closeError: closeFailure},
			wantError: closeFailure,
			wantText:  "closing rows: connection reset while closing",
		},
		{
			name:     "a row that does not scan",
			answer:   result{columns: []string{"name"}, rows: [][]driver.Value{{nil}}},
			wantText: "Scan error",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			values, err := rowscan.Strings(t.Context(), openDatabase(t, testCase.answer), "SELECT name FROM t WHERE x = ?", 1)
			if testCase.wantError == nil && testCase.wantText == "" {
				require.NoError(t, err)
				assert.Equal(t, testCase.want, values)
				return
			}
			require.Error(t, err)
			assert.Nil(t, values)
			if testCase.wantError != nil {
				assert.ErrorIs(t, err, testCase.wantError)
			}
			if testCase.wantText != "" {
				assert.ErrorContains(t, err, testCase.wantText)
			}
		})
	}
}

func TestStoppedJoinsTheCauseAnEndedContextCarries(t *testing.T) {
	t.Parallel()

	cause := errors.New("deploy window closed")
	tests := []struct {
		context   func(t *testing.T) context.Context
		wantError []error
		name      string
		wantText  string
	}{
		{
			name:    "a live context",
			context: func(t *testing.T) context.Context { return t.Context() },
		},
		{
			name: "cancelled without a cause",
			context: func(t *testing.T) context.Context {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				return ctx
			},
			wantError: []error{context.Canceled},
			wantText:  context.Canceled.Error(),
		},
		{
			name: "cancelled with a cause",
			context: func(t *testing.T) context.Context {
				ctx, cancel := context.WithCancelCause(t.Context())
				cancel(cause)
				return ctx
			},
			wantError: []error{context.Canceled, cause},
			wantText:  context.Canceled.Error() + ": deploy window closed",
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := rowscan.Stopped(testCase.context(t))
			if testCase.wantText == "" {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, testCase.wantText)
			for _, want := range testCase.wantError {
				assert.ErrorIs(t, err, want)
			}
		})
	}
}

func TestWithCauseSaysWhyAContextCutAQueryShort(t *testing.T) {
	t.Parallel()

	cause := errors.New("deploy window closed")
	cancelled := func(t *testing.T) context.Context {
		ctx, cancel := context.WithCancelCause(t.Context())
		cancel(cause)
		return ctx
	}
	tests := []struct {
		context  func(t *testing.T) context.Context
		failure  error
		name     string
		wantText string
		wantNil  bool
	}{
		{name: "no failure", context: cancelled, failure: nil, wantNil: true},
		{name: "a live context", context: func(t *testing.T) context.Context { return t.Context() }, failure: io.ErrUnexpectedEOF, wantText: "unexpected EOF"},
		{name: "a cancelled query", context: cancelled, failure: context.Canceled, wantText: context.Canceled.Error() + ": deploy window closed"},
		{name: "a failure that already says why", context: cancelled, failure: cause, wantText: "deploy window closed"},
		{
			name: "cancelled without a cause",
			context: func(t *testing.T) context.Context {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				return ctx
			},
			failure:  context.Canceled,
			wantText: context.Canceled.Error(),
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := rowscan.WithCause(testCase.context(t), testCase.failure)
			if testCase.wantNil {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, testCase.wantText)
			assert.ErrorIs(t, err, testCase.failure)
		})
	}
}

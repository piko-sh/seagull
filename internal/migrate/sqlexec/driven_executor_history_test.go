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
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/logging"
)

const appliedAtWarning = "applied_at could not be read, so the zero time is reported in its place"

var errRowsClose = errors.New("rows close failed")

type historyRowsConnector struct {
	closeError error
	rows       [][]driver.Value
}

type historyRowsDriver struct{}

type historyRowsConn historyRowsConnector

type historyRows struct {
	closeError error
	values     [][]driver.Value
}

func (connector historyRowsConnector) Connect(context.Context) (driver.Conn, error) {
	return historyRowsConn(connector), nil
}

func (historyRowsConnector) Driver() driver.Driver { return historyRowsDriver{} }

func (historyRowsDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("historyRowsDriver opens connections only through its connector")
}

func (historyRowsConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("historyRowsConn does not prepare statements")
}

func (historyRowsConn) Close() error { return nil }

func (historyRowsConn) Begin() (driver.Tx, error) {
	return nil, errors.New("historyRowsConn does not begin transactions")
}

func (conn historyRowsConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &historyRows{values: conn.rows, closeError: conn.closeError}, nil
}

func (*historyRows) Columns() []string {
	return []string{"version", "name", "checksum", "applied_at", "duration_ms", "down_checksum", "last_statement", "dirty", "splitter_version", "statements_hash"}
}

func (rows *historyRows) Close() error { return rows.closeError }

func (rows *historyRows) Next(destination []driver.Value) error {
	if len(rows.values) == 0 {
		return io.EOF
	}
	copy(destination, rows.values[0])
	rows.values = rows.values[1:]
	return nil
}

func historyValues(version, appliedAt driver.Value) []driver.Value {
	return []driver.Value{version, "boats", "c", appliedAt, int64(1), nil, nil, false, nil, nil}
}

func warnings(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	for line := range strings.Lines(logs.String()) {
		var record map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &record))
		if record["msg"] == appliedAtWarning {
			records = append(records, record)
		}
	}
	return records
}

func TestAppliedVersionsWarnsOnceForEveryAppliedAtItCannotRead(t *testing.T) {
	t.Parallel()

	readable := time.Date(2026, 10, 6, 12, 30, 0, 0, time.UTC)
	tests := []struct {
		name          string
		rows          [][]driver.Value
		wantRows      float64
		wantFirst     float64
		wantWarnings  int
		wantZeroTimes int
	}{
		{
			name:         "every value readable",
			rows:         [][]driver.Value{historyValues(int64(1), readable), historyValues(int64(2), "2026-10-06 12:30:00")},
			wantWarnings: 0,
		},
		{
			name: "three unreadable values among readable ones",
			rows: [][]driver.Value{
				historyValues(int64(1), readable),
				historyValues(int64(2), "not a date"),
				historyValues(int64(3), nil),
				historyValues(int64(4), readable),
				historyValues(int64(5), true),
			},
			wantWarnings: 1, wantRows: 3, wantFirst: 2, wantZeroTimes: 3,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			database := sql.OpenDB(historyRowsConnector{rows: testCase.rows, closeError: nil})
			t.Cleanup(func() { _ = database.Close() })
			executor, err := NewExecutor(database, SQLite())
			require.NoError(t, err)
			var logs bytes.Buffer
			ctx := logging.ContextWithLogger(t.Context(), slog.New(slog.NewJSONHandler(&logs, nil)))

			applied, err := executor.AppliedVersions(ctx)
			require.NoError(t, err)

			zeroTimes := 0
			for _, migration := range applied {
				if migration.AppliedAt.IsZero() {
					zeroTimes++
				}
			}
			assert.Equal(t, testCase.wantZeroTimes, zeroTimes)
			records := warnings(t, &logs)
			require.Len(t, records, testCase.wantWarnings)
			if testCase.wantWarnings == 0 {
				return
			}
			assert.Equal(t, "WARN", records[0]["level"])
			assert.Equal(t, "sgl_migrations", records[0]["table"])
			assert.Equal(t, testCase.wantRows, records[0]["rows"])
			assert.Equal(t, testCase.wantFirst, records[0]["first_version"])
		})
	}
}

func TestReadAppliedVersionsJoinsTheFailureToCloseItsRows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		closeError error
		name       string
	}{
		{name: "a scan failure and a close failure", closeError: errRowsClose},
		{name: "a scan failure alone", closeError: nil},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			database := sql.OpenDB(historyRowsConnector{rows: [][]driver.Value{historyValues("not a version", nil)}, closeError: testCase.closeError})
			t.Cleanup(func() { _ = database.Close() })
			executor := newExecutor(database, SQLite())

			_, err := executor.readAppliedVersions(t.Context(), database, "sgl_migrations", "SELECT version, name FROM sgl_migrations", true)

			require.ErrorContains(t, err, "scanning applied migration")
			require.Equal(t, testCase.closeError != nil, errors.Is(err, errRowsClose))
			if testCase.closeError != nil {
				require.ErrorContains(t, err, "closing rows: rows close failed")
			}
		})
	}
}

func appliedAtOf(raw any) time.Time {
	appliedAt, _ := parseAppliedAt(raw)
	return appliedAt
}

func TestParseAppliedAt_HandlesNativeTimeTime(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Second)

	require.Equal(t, now, appliedAtOf(now))
}

func TestParseAppliedAt_HandlesRFC3339(t *testing.T) {
	t.Parallel()

	got := appliedAtOf("2026-01-02T15:04:05Z")
	require.Equal(t, 2026, got.Year())
	require.Equal(t, time.January, got.Month())
}

func TestParseAppliedAt_HandlesSQLiteFormat(t *testing.T) {
	t.Parallel()

	got := appliedAtOf("2026-05-03 12:34:56")
	require.Equal(t, 2026, got.Year())
	require.Equal(t, time.May, got.Month())
}

func TestParseAppliedAt_HandlesEveryTextLayout(t *testing.T) {
	t.Parallel()

	instant := time.Date(2026, 10, 6, 6, 10, 34, 172113129, time.UTC)
	cases := []struct {
		name  string
		value string
		want  time.Time
	}{
		{name: "rfc3339 nano", value: "2026-10-06T06:10:34.172113129Z", want: instant},
		{name: "rfc3339", value: "2026-10-06T06:10:34Z", want: instant.Truncate(time.Second)},
		{name: "sqlite datetime", value: "2026-10-06 06:10:34", want: instant.Truncate(time.Second)},
		{name: "go time string", value: "2026-10-06 06:10:34.172113129 +0000 UTC", want: instant},
		{name: "go time string with monotonic reading", value: "2026-10-06 06:10:34.172113129 +0000 UTC m=+0.001", want: instant},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := appliedAtOf(testCase.value)
			require.True(t, testCase.want.Equal(got), "got %v", got)
			require.True(t, testCase.want.Equal(appliedAtOf([]byte(testCase.value))))
		})
	}
}

func TestParseAppliedAt_HandlesUnixSecondsAsInt64(t *testing.T) {
	t.Parallel()

	got := appliedAtOf(int64(1700000000))
	require.Equal(t, time.Unix(1700000000, 0).UTC(), got)
}

func TestParseAppliedAt_HandlesUnixSecondsAsFloat64(t *testing.T) {
	t.Parallel()

	got := appliedAtOf(float64(1700000000))
	require.Equal(t, time.Unix(1700000000, 0).UTC(), got)
}

func TestParseAppliedAtReportsAValueItCannotRead(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		raw  any
		name string
	}{
		{name: "null", raw: nil},
		{name: "bytes in no known layout", raw: []byte("anything")},
		{name: "text in no known layout", raw: "not a date"},
		{name: "a float out of range", raw: math.Inf(1)},
		{name: "another driver type", raw: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			appliedAt, parsed := parseAppliedAt(testCase.raw)
			require.False(t, parsed)
			require.True(t, appliedAt.IsZero())
		})
	}
}

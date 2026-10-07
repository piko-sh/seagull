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
	"database/sql/driver"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/migrate"
	"seagull.sh/seagull/internal/observe"
)

var _ observe.Clock = (*steppingClock)(nil)

type steppingClock struct {
	mu   sync.Mutex
	now  time.Time
	step time.Duration
}

func newSteppingClock(start time.Time, step time.Duration) *steppingClock {
	return &steppingClock{mu: sync.Mutex{}, now: start, step: step}
}

type historyCapture struct {
	mu         sync.Mutex
	appliedAt  time.Time
	durationMs int64
	captured   bool
}

type captureConnector struct{ capture *historyCapture }

type captureDriver struct{ capture *historyCapture }

type captureConn struct{ capture *historyCapture }

type captureTx struct{}

type captureEmptyRows struct{}

func (c *steppingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	current := c.now
	c.now = c.now.Add(c.step)
	return current
}

func (capture *historyCapture) recordInsert(query string, arguments []driver.NamedValue) {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	switch {
	case strings.Contains(query, "INSERT INTO sgl_migrations"):
		if appliedAt, ok := arguments[3].Value.(time.Time); ok {
			capture.appliedAt = appliedAt
		}
		if durationMs, ok := arguments[4].Value.(int64); ok {
			capture.durationMs = durationMs
		}
		capture.captured = true
	case strings.Contains(query, "INSERT INTO sgl_seeds"):
		if durationMs, ok := arguments[3].Value.(int64); ok {
			capture.durationMs = durationMs
		}
		capture.captured = true
	}
}

func (c *captureConnector) Connect(context.Context) (driver.Conn, error) {
	return &captureConn{capture: c.capture}, nil
}
func (c *captureConnector) Driver() driver.Driver { return &captureDriver{capture: c.capture} }

func (d *captureDriver) Open(string) (driver.Conn, error) {
	return &captureConn{capture: d.capture}, nil
}

func (*captureConn) Prepare(string) (driver.Stmt, error) { return nil, io.ErrUnexpectedEOF }
func (*captureConn) Close() error                        { return nil }
func (*captureConn) Begin() (driver.Tx, error)           { return captureTx{}, nil }
func (*captureConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return captureTx{}, nil
}

func (c *captureConn) ExecContext(_ context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
	c.capture.recordInsert(query, arguments)
	return driver.RowsAffected(1), nil
}

func (*captureConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &captureEmptyRows{}, nil
}

func (captureTx) Commit() error   { return nil }
func (captureTx) Rollback() error { return nil }

func (*captureEmptyRows) Columns() []string         { return []string{"unused"} }
func (*captureEmptyRows) Close() error              { return nil }
func (*captureEmptyRows) Next([]driver.Value) error { return io.EOF }

func TestExecuteMigrationRecordsClockDerivedTimings(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	const step = 1500 * time.Millisecond

	capture := &historyCapture{}
	database := sql.OpenDB(&captureConnector{capture: capture})
	t.Cleanup(func() { _ = database.Close() })

	executor := newExecutor(
		database,
		SQLite(),
		WithClock(newSteppingClock(start, step)),
	)

	migration := migrate.MigrationRecord{
		Name:     "create_things",
		Checksum: "abc",
		Content:  []byte("CREATE TABLE things (id INTEGER);\n"),
		Version:  1,
		SkipUpTo: -1,
	}

	require.NoError(t, executor.ExecuteMigration(t.Context(), migration, migrate.MigrationDirectionUp, true))

	require.True(t, capture.captured, "the migration history INSERT must have run")
	require.True(t, capture.appliedAt.Equal(start), "applied_at must be the clock's start time, got %s", capture.appliedAt)
	require.Equal(t, step.Milliseconds(), capture.durationMs, "duration_ms must be the gap between the two clock reads")
}

func TestExecuteSeedRecordsClockDerivedDuration(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	const step = 2500 * time.Millisecond

	capture := &historyCapture{}
	database := sql.OpenDB(&captureConnector{capture: capture})
	t.Cleanup(func() { _ = database.Close() })

	executor := newSeedExecutor(
		database,
		SQLite(),
		WithClock(newSteppingClock(start, step)),
	)

	seed := migrate.SeedRecord{
		Name:     "seed_things",
		Checksum: "def",
		Content:  []byte("INSERT INTO things (id) VALUES (1);\n"),
		Version:  1,
	}

	require.NoError(t, executor.ExecuteSeed(t.Context(), seed))

	require.True(t, capture.captured, "the seed history INSERT must have run")
	require.Equal(t, step.Milliseconds(), capture.durationMs, "duration_ms must be the gap between the two clock reads")
}

func TestWithExecutorClockIgnoresNil(t *testing.T) {
	t.Parallel()

	capture := &historyCapture{}
	database := sql.OpenDB(&captureConnector{capture: capture})
	t.Cleanup(func() { _ = database.Close() })

	executor := newExecutor(
		database,
		SQLite(),
		WithClock(nil),
	)

	migration := migrate.MigrationRecord{
		Name:     "create_things",
		Content:  []byte("CREATE TABLE things (id INTEGER);\n"),
		Version:  1,
		SkipUpTo: -1,
	}

	require.NoError(t, executor.ExecuteMigration(t.Context(), migration, migrate.MigrationDirectionUp, true))
	require.True(t, capture.captured, "the default clock must still drive a working migration")
}

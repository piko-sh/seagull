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
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/migrate"
)

type cancelStore struct {
	mu                    sync.Mutex
	cancel                context.CancelFunc
	cancelAfterStatement  string
	cancelDuringStatement string
	lastStatementWrites   []int64
	writeDeadlines        []bool
	versions              map[int64]bool
}

type cancelConnector struct{ store *cancelStore }

type cancelDriver struct{ store *cancelStore }

type cancelConn struct{ store *cancelStore }

type cancelTx struct{}

type cancelCountRows struct {
	count int64
	done  bool
}

type cancelEmptyRows struct{}

func (store *cancelStore) exec(ctx context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
	store.mu.Lock()
	defer store.mu.Unlock()

	if store.cancel != nil && store.cancelDuringStatement != "" && strings.Contains(query, store.cancelDuringStatement) {
		store.cancel()
		store.cancel = nil
		return nil, context.Canceled
	}
	switch {
	case strings.Contains(query, "INSERT INTO sgl_migrations"):
		version := namedInt64(arguments, 0)
		if store.versions[version] {
			return nil, errors.New("UNIQUE constraint failed: sgl_migrations.version")
		}
		store.versions[version] = true
		return driver.RowsAffected(1), nil
	case strings.Contains(query, "UPDATE sgl_migrations") && strings.Contains(query, "last_statement"):
		store.lastStatementWrites = append(store.lastStatementWrites, namedInt64(arguments, 0))
		_, hasDeadline := ctx.Deadline()
		store.writeDeadlines = append(store.writeDeadlines, hasDeadline)
		return driver.RowsAffected(1), nil
	default:
		if store.cancel != nil && store.cancelAfterStatement != "" &&
			strings.Contains(query, store.cancelAfterStatement) {
			store.cancel()
			store.cancel = nil
		}
		return driver.RowsAffected(0), nil
	}
}

func (store *cancelStore) query(query string, arguments []driver.NamedValue) (driver.Rows, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if strings.Contains(query, "count(*)") {
		count := int64(0)
		if store.versions[namedInt64(arguments, 0)] {
			count = 1
		}
		return &cancelCountRows{count: count}, nil
	}
	return &cancelEmptyRows{}, nil
}

func (store *cancelStore) writes() []int64 {
	store.mu.Lock()
	defer store.mu.Unlock()
	return append([]int64(nil), store.lastStatementWrites...)
}

func (store *cancelStore) deadlines() []bool {
	store.mu.Lock()
	defer store.mu.Unlock()
	return append([]bool(nil), store.writeDeadlines...)
}

func (connector *cancelConnector) Connect(context.Context) (driver.Conn, error) {
	return &cancelConn{store: connector.store}, nil
}
func (connector *cancelConnector) Driver() driver.Driver {
	return &cancelDriver{store: connector.store}
}

func (driverImpl *cancelDriver) Open(string) (driver.Conn, error) {
	return &cancelConn{store: driverImpl.store}, nil
}

func (*cancelConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare unsupported")
}
func (*cancelConn) Close() error              { return nil }
func (*cancelConn) Begin() (driver.Tx, error) { return cancelTx{}, nil }
func (*cancelConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return cancelTx{}, nil
}

func (conn *cancelConn) ExecContext(
	ctx context.Context, query string, arguments []driver.NamedValue,
) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return conn.store.exec(ctx, query, arguments)
}

func (conn *cancelConn) QueryContext(
	ctx context.Context, query string, arguments []driver.NamedValue,
) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return conn.store.query(query, arguments)
}

func (cancelTx) Commit() error   { return nil }
func (cancelTx) Rollback() error { return nil }

func (*cancelCountRows) Columns() []string { return []string{"count"} }
func (*cancelCountRows) Close() error      { return nil }
func (rows *cancelCountRows) Next(dest []driver.Value) error {
	if rows.done {
		return io.EOF
	}
	rows.done = true
	dest[0] = rows.count
	return nil
}

func (*cancelEmptyRows) Columns() []string           { return []string{"unused"} }
func (*cancelEmptyRows) Close() error                { return nil }
func (*cancelEmptyRows) Next(_ []driver.Value) error { return io.EOF }

func namedInt64(arguments []driver.NamedValue, position int) int64 {
	if position >= len(arguments) {
		return 0
	}
	if value, ok := arguments[position].Value.(int64); ok {
		return value
	}
	return 0
}

func TestExecutor_CancellationFlushesResumeCheckpoint(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	store := &cancelStore{
		versions:             map[int64]bool{},
		cancel:               cancel,
		cancelAfterStatement: "STMT_0 ",
	}
	database := sql.OpenDB(&cancelConnector{store: store})
	t.Cleanup(func() { _ = database.Close() })

	executor := newExecutor(
		database,
		SQLite(),
		WithProgressBatchSize(5),
	)

	migration := migrate.MigrationRecord{
		Name:     "create_things",
		Checksum: "abc",
		Content:  buildStatements(8),
		Version:  1,
		SkipUpTo: -1,
	}

	err := executor.ExecuteMigration(ctx, migration, migrate.MigrationDirectionUp, false)

	require.Error(t, err, "a cancelled migration must surface the cancellation error")
	require.ErrorIs(t, err, context.Canceled)

	writes := store.writes()
	require.Equal(t, []int64{0}, writes,
		"the cancellation handler must flush exactly the last completed statement index (0) via a detached context")
}

func TestExecutor_CancellationBeforeFirstStatementDoesNotFlush(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	store := &cancelStore{versions: map[int64]bool{}}
	database := sql.OpenDB(&cancelConnector{store: store})
	t.Cleanup(func() { _ = database.Close() })

	executor := newExecutor(database, SQLite())

	content := buildStatements(4)
	recordedVersion, statementsHash := recordedProgress(t, SQLite(), content, 0)
	migration := migrate.MigrationRecord{
		Name:            "create_things",
		Checksum:        "abc",
		Content:         content,
		Version:         1,
		SkipUpTo:        0,
		SplitterVersion: recordedVersion,
		StatementsHash:  statementsHash,
		Dirty:           true,
	}

	err := executor.ExecuteMigration(ctx, migration, migrate.MigrationDirectionUp, false)

	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, store.writes(),
		"no statement completed past the resume point, so nothing should be flushed")
}

func TestWithProgressBatchSize_ControlsCheckpointCadence(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		batchSize  int
		statements int

		wantWrites []int64
	}{
		{
			name:       "batch of two over six statements",
			batchSize:  2,
			statements: 6,
			wantWrites: []int64{1, 3, 5},
		},
		{
			name:       "batch larger than statement count flushes only at the end",
			batchSize:  100,
			statements: 4,
			wantWrites: []int64{3},
		},
		{
			name:       "a batch of one flushes every statement",
			batchSize:  1,
			statements: 3,
			wantWrites: []int64{0, 1, 2},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			store := &cancelStore{versions: map[int64]bool{}}
			database := sql.OpenDB(&cancelConnector{store: store})
			t.Cleanup(func() { _ = database.Close() })

			executor := newExecutor(
				database,
				SQLite(),
				WithProgressBatchSize(testCase.batchSize),
			)

			migration := migrate.MigrationRecord{
				Name:     "create_things",
				Checksum: "abc",
				Content:  buildStatements(testCase.statements),
				Version:  1,
				SkipUpTo: -1,
			}

			err := executor.ExecuteMigration(t.Context(), migration, migrate.MigrationDirectionUp, false)

			require.NoError(t, err)
			require.Equal(t, testCase.wantWrites, store.writes(),
				"the batch size must control which statement indices are flushed as progress checkpoints")
		})
	}
}

func buildStatements(count int) []byte {
	var builder strings.Builder
	for index := range count {
		builder.WriteString("STMT_")
		builder.WriteString(strconv.Itoa(index))
		builder.WriteString(" col")
		builder.WriteString(strconv.Itoa(index))
		builder.WriteString(";\n")
	}
	return []byte(builder.String())
}

func recordedProgress(t *testing.T, backend Backend, content []byte, lastStatement int) (int, string) {
	t.Helper()
	statements, err := backend.Split(string(content))
	require.NoError(t, err)
	digest := newStatementDigest()
	for _, statement := range statements[:lastStatement+1] {
		digest.add(statement)
	}
	return splitterVersion, digest.sum()
}

func TestExecutorWritesTheCheckpointWhenACancellationStopsAStatement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		cancelDuring  string
		wantWrites    []int64
		wantDeadlines []bool
		batchSize     int
	}{
		{
			name:          "a batch of five stopped in its third statement",
			batchSize:     5,
			cancelDuring:  "STMT_2 ",
			wantWrites:    []int64{1},
			wantDeadlines: []bool{true},
		},
		{
			name:          "a batch of three stopped in the second statement after its checkpoint",
			batchSize:     3,
			cancelDuring:  "STMT_4 ",
			wantWrites:    []int64{2, 3},
			wantDeadlines: []bool{false, true},
		},
		{
			name:          "a statement stopped right after a checkpoint writes nothing more",
			batchSize:     2,
			cancelDuring:  "STMT_2 ",
			wantWrites:    []int64{1},
			wantDeadlines: []bool{false},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			store := &cancelStore{versions: map[int64]bool{}, cancel: cancel, cancelDuringStatement: testCase.cancelDuring}
			database := sql.OpenDB(&cancelConnector{store: store})
			t.Cleanup(func() { _ = database.Close() })
			executor, err := NewExecutor(database, SQLite(), WithProgressBatchSize(testCase.batchSize))
			require.NoError(t, err)
			migration := migrate.MigrationRecord{Name: "create_things", Checksum: "abc", Content: buildStatements(8), Version: 1, SkipUpTo: -1}

			err = executor.ExecuteMigration(ctx, migration, migrate.MigrationDirectionUp, false)

			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, testCase.wantWrites, store.writes(),
				"the statement before the cancelled one must be recorded, although the run's context has ended")
			require.Equal(t, testCase.wantDeadlines, store.deadlines(),
				"a checkpoint written after the run's context ended must be bounded by its own deadline")
		})
	}
}

func TestCheckpointContextOutlivesItsParentWithinItsOwnBound(t *testing.T) {
	t.Parallel()

	cause := errors.New("deploy window closed")
	tests := []struct {
		parent func(t *testing.T) context.Context
		name   string
	}{
		{name: "a live parent", parent: func(t *testing.T) context.Context { return t.Context() }},
		{name: "a cancelled parent", parent: func(t *testing.T) context.Context {
			ctx, cancel := context.WithCancelCause(t.Context())
			cancel(cause)
			return ctx
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			before := time.Now()
			flushContext, cancel := checkpointContext(testCase.parent(t))
			defer cancel()

			require.NoError(t, flushContext.Err())
			deadline, bounded := flushContext.Deadline()
			require.True(t, bounded)
			require.WithinDuration(t, before.Add(checkpointFlushTimeout), deadline, time.Second)
			cancel()
			require.NotErrorIs(t, context.Cause(flushContext), cause)
		})
	}
}

func TestExecutorLoopsReportTheCauseOfACancellation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		useTransaction bool
	}{
		{name: "statement by statement", useTransaction: false},
		{name: "in a transaction", useTransaction: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cause := errors.New("deploy window closed")
			ctx, cancel := context.WithCancelCause(t.Context())
			t.Cleanup(func() { cancel(nil) })
			store := &cancelStore{versions: map[int64]bool{}, cancel: func() { cancel(cause) }, cancelAfterStatement: "STMT_0 "}
			database := sql.OpenDB(&cancelConnector{store: store})
			t.Cleanup(func() { _ = database.Close() })
			executor, err := NewExecutor(database, SQLite())
			require.NoError(t, err)
			migration := migrate.MigrationRecord{Name: "create_things", Checksum: "abc", Content: buildStatements(3), Version: 1, SkipUpTo: -1}

			err = executor.ExecuteMigration(ctx, migration, migrate.MigrationDirectionUp, testCase.useTransaction)

			require.ErrorIs(t, err, cause)
			require.ErrorIs(t, err, context.Canceled)
			require.ErrorContains(t, err, "migration 1 cancelled before statement 2: "+context.Canceled.Error()+": deploy window closed")
		})
	}
}

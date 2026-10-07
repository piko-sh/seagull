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

package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_registry"
)

const watcherTimeout = 5 * time.Second

type notifyingWriter struct {
	written chan struct{}
	buffer  bytes.Buffer
	mutex   sync.Mutex
}

type watcherRun struct {
	signals   chan os.Signal
	done      chan struct{}
	finished  chan struct{}
	forced    *atomic.Bool
	forcedNow chan struct{}
}

func (w *notifyingWriter) Write(data []byte) (int, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	written, err := w.buffer.Write(data)
	select {
	case w.written <- struct{}{}:
	default:
	}
	return written, err
}

func (w *notifyingWriter) String() string {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	return w.buffer.String()
}

func TestMainRunsACommand(t *testing.T) {
	assert.Equal(t, exitOK, Main([]string{"version"}))
}

func TestInterruptedStatusFollowsTheSignalThatCancelledTheRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cause  error
		status int
		want   int
	}{
		{name: "an interrupt", cause: &interruptError{signal: os.Interrupt}, status: exitInterrupted, want: 130},
		{name: "a termination signal", cause: &interruptError{signal: syscall.SIGTERM}, status: exitInterrupted, want: 143},
		{name: "a hang-up signal", cause: &interruptError{signal: syscall.SIGHUP}, status: exitInterrupted, want: 129},
		{name: "a cancellation with no signal", cause: nil, status: exitInterrupted, want: 130},
		{name: "a run that failed for another reason", cause: &interruptError{signal: syscall.SIGTERM}, status: exitDatabase, want: exitDatabase},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancelCause(t.Context())
			cancel(testCase.cause)

			assert.Equal(t, testCase.want, interruptedStatus(ctx, testCase.status))
		})
	}
}

func TestMainContextExitsWithTheStatusOfTheSignalThatCancelledIt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		signal os.Signal
		status int
	}{
		{name: "an interrupt", signal: os.Interrupt, status: 130},
		{name: "a termination signal", signal: syscall.SIGTERM, status: 143},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			directory := minimalProject(t)
			ctx, cancel := context.WithCancelCause(t.Context())
			cancel(&interruptError{signal: testCase.signal})
			var stderr bytes.Buffer

			status := MainContext(ctx, []string{"generate"}, WithIO(strings.NewReader(""), io.Discard, &stderr), WithDirectory(directory))

			assert.Equal(t, testCase.status, status, stderr.String())
			assert.Contains(t, stderr.String(), "sgl: interrupted")
		})
	}
}

func TestMainWithSignalsWarnsOnTheConfiguredErrorStream(t *testing.T) {
	t.Parallel()

	directory := minimalProject(t)
	release := make(chan struct{})
	stderr := &notifyingWriter{written: make(chan struct{}, 1), mutex: sync.Mutex{}, buffer: bytes.Buffer{}}
	signals := make(chan os.Signal, 2)
	blocking := WithDialect("blocking", func(options dialect_registry.Options) (dialect.Dialect, error) {
		<-release
		return dialect_registry.New("sqlite", options)
	})
	finished := make(chan int, 1)
	writeFile(t, directory, "sgl.yaml", "databases:\n  - name: app\n    dialect: blocking\n")

	go func() {
		finished <- mainWithSignals(signals, func() { assert.Fail(t, "forced an exit") }, []string{"generate"},
			WithIO(strings.NewReader(""), io.Discard, stderr), WithDirectory(directory), WithName("seagull"), blocking)
	}()
	signals <- syscall.SIGTERM
	<-stderr.written
	close(release)

	assert.Equal(t, 143, <-finished)
	assert.Contains(t, stderr.String(), "seagull: SIGTERM received; cancelling.")
}

func minimalProject(t *testing.T) string {
	t.Helper()

	directory := t.TempDir()
	writeFile(t, directory, "sgl.yaml", "databases:\n  - name: app\n    dialect: sqlite\n")
	writeFile(t, directory, "db/migrations/0001_init.up.sql", "CREATE TABLE authors (id INTEGER PRIMARY KEY, name TEXT NOT NULL);\n")
	writeFile(t, directory, "db/queries/authors.sql", "-- sgl.query(GetAuthor, one)\nSELECT id, name FROM authors WHERE id = ?;\n")
	return directory
}

func writeFile(t *testing.T, directory, name, content string) {
	t.Helper()

	path := filepath.Join(directory, filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func startWatcher(t *testing.T) (*watcherRun, context.Context) {
	t.Helper()

	ctx, cancel := context.WithCancelCause(t.Context())
	t.Cleanup(func() { cancel(nil) })
	run := &watcherRun{
		signals:   make(chan os.Signal, 2),
		done:      make(chan struct{}),
		finished:  make(chan struct{}),
		forced:    &atomic.Bool{},
		forcedNow: make(chan struct{}),
	}
	watcher := &signalWatcher{
		signals: run.signals,
		done:    run.done,
		stderr:  io.Discard,
		cancel:  cancel,
		forceExit: func() {
			run.forced.Store(true)
			close(run.forcedNow)
		},
	}
	go func() {
		watcher.watch()
		close(run.finished)
	}()
	return run, ctx
}

func waitFor(t *testing.T, channel <-chan struct{}, what string) {
	t.Helper()

	select {
	case <-channel:
	case <-time.After(watcherTimeout):
		require.FailNow(t, "timed out waiting for "+what)
	}
}

func TestSignalWatcherReturnsWhenTheCommandFinishes(t *testing.T) {
	t.Parallel()

	run, ctx := startWatcher(t)
	close(run.done)

	waitFor(t, run.finished, "the watcher to return")
	require.NoError(t, ctx.Err())
	require.False(t, run.forced.Load())
}

func TestSignalWatcherReturnsWhenTheCommandFinishesAfterOneInterrupt(t *testing.T) {
	t.Parallel()

	run, ctx := startWatcher(t)
	run.signals <- os.Interrupt
	waitFor(t, ctx.Done(), "the command to be cancelled")
	close(run.done)

	waitFor(t, run.finished, "the watcher to return")
	require.False(t, run.forced.Load())
}

func TestSignalWatcherForcesExitOnTheSecondInterrupt(t *testing.T) {
	t.Parallel()

	run, ctx := startWatcher(t)
	run.signals <- os.Interrupt
	run.signals <- os.Interrupt

	waitFor(t, run.forcedNow, "the forced exit")
	waitFor(t, run.finished, "the watcher to return")
	require.ErrorIs(t, ctx.Err(), context.Canceled)
}

func TestMainWithSignalsLeavesNoGoroutineAfterAnInterrupt(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	signals := make(chan os.Signal, 2)
	signals <- os.Interrupt

	mainWithSignals(signals, func() { assert.Fail(t, "forced an exit") }, []string{"version"}, WithIO(nil, io.Discard, io.Discard))
}

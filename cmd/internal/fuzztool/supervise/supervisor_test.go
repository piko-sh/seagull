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

//go:build linux || darwin

package supervise

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newTestSupervisor(t *testing.T, maximum uint64, signals <-chan os.Signal, arguments ...string) (*Supervisor, string) {
	t.Helper()

	root := t.TempDir()
	metadata := filepath.Join(root, "metadata")
	require.NoError(t, os.Mkdir(metadata, 0o700))
	supervisor := New(Config{
		ManagedRoot:         root,
		MetadataDirectory:   metadata,
		Locations:           []string{filepath.Join(root, "missing", "child")},
		MinimumFreeBytes:    1,
		MaximumManagedBytes: maximum,
	}, arguments, signals, &bytes.Buffer{})
	supervisor.timings = timings{
		poll:           10 * time.Millisecond,
		resourceCheck:  20 * time.Millisecond,
		terminateAfter: 200 * time.Millisecond,
		killAfter:      400 * time.Millisecond,
		cleanupGrace:   300 * time.Millisecond,
	}
	return supervisor, metadata
}

func readOutcome(t *testing.T, metadata string) string {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(metadata, "outcome.txt"))
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	require.NoError(t, err)
	return strings.TrimSpace(string(content))
}

func TestRunReportsHowTheCommandEnded(t *testing.T) {
	if testing.Short() {
		t.Skip("starts processes")
	}
	t.Parallel()

	tests := []struct {
		name          string
		script        string
		wantOutcome   string
		wantRecorded  string
		wantStatus    int
		wantExitCode  int
		maximum       uint64
		interruptWith os.Signal
	}{
		{name: "a passing command", script: "exit 0", wantStatus: 0, wantExitCode: 0, wantRecorded: "passed", maximum: 1 << 40},
		{name: "a failing command keeps its status", script: "exit 3", wantStatus: 3, wantExitCode: 3, wantRecorded: "failed", maximum: 1 << 40},
		{
			name: "a signal death reports as a shell does", script: "kill -TERM $$",
			wantStatus: 128 + int(syscall.SIGTERM), wantExitCode: -int(syscall.SIGTERM), wantRecorded: "failed", maximum: 1 << 40,
		},
		{
			name: "a descendant left running fails cleanup", script: "sleep 30 & exit 0",
			wantStatus: StatusFailClosed, wantExitCode: 0, wantOutcome: OutcomeCleanupFailed, wantRecorded: OutcomeCleanupFailed, maximum: 1 << 40,
		},
		{
			name: "an interrupt is recorded and forwarded", script: "sleep 30", interruptWith: syscall.SIGTERM,
			wantStatus: StatusFailClosed, wantExitCode: -int(syscall.SIGINT), wantOutcome: OutcomeInterrupted, wantRecorded: OutcomeInterrupted, maximum: 1 << 40,
		},
		{
			name: "an ignored interrupt escalates to the group", script: "trap '' INT TERM; sleep 30", interruptWith: syscall.SIGINT,
			wantStatus: StatusFailClosed, wantExitCode: -int(syscall.SIGKILL), wantOutcome: OutcomeInterrupted, wantRecorded: OutcomeInterrupted, maximum: 1 << 40,
		},
		{
			name: "growing storage stops the command", script: "head -c 2097152 /dev/zero > ../grown; sleep 30",
			wantStatus: StatusFailClosed, wantExitCode: -int(syscall.SIGINT), wantOutcome: OutcomeResourceStopped,
			wantRecorded: OutcomeResourceStopped, maximum: 1 << 20,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			signals := make(chan os.Signal, 1)
			supervisor, metadata := newTestSupervisor(t, testCase.maximum, signals, "sh", "-c", "cd \"$0\" && "+testCase.script, "")
			supervisor.arguments[3] = metadata
			if testCase.interruptWith != nil {
				go func() {
					require.Eventually(t, func() bool {
						_, err := os.Stat(filepath.Join(metadata, "launcher.pid"))
						return err == nil
					}, 5*time.Second, 10*time.Millisecond)
					time.Sleep(100 * time.Millisecond)
					signals <- testCase.interruptWith
				}()
			}

			status := supervisor.Run(&bytes.Buffer{})

			require.Equal(t, testCase.wantStatus, status)
			require.Equal(t, testCase.wantOutcome, readOutcome(t, metadata))
			var record map[string]any
			content, err := os.ReadFile(filepath.Join(metadata, "execution.json"))
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(content, &record))
			require.Equal(t, testCase.wantRecorded, record["outcome"])
			require.InDelta(t, testCase.wantExitCode, record["exit_code"], 0)
			require.Error(t, syscall.Kill(-supervisor.command.Process.Pid, 0), "the group is gone")
		})
	}
}

func TestRunFailsClosedWithoutStartingAtTheStorageLimit(t *testing.T) {
	t.Parallel()

	supervisor, metadata := newTestSupervisor(t, 0, nil, "true")

	require.Equal(t, StatusFailClosed, supervisor.Run(&bytes.Buffer{}))
	require.Equal(t, OutcomeResourceStopped, readOutcome(t, metadata))
	require.Nil(t, supervisor.command)
}

func TestRunFailsClosedWhenTheCommandCannotStart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		arguments []string
	}{
		{name: "a missing program", arguments: []string{filepath.Join(t.TempDir(), "absent")}},
		{name: "an empty command line", arguments: nil},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			supervisor, metadata := newTestSupervisor(t, 1<<40, nil, testCase.arguments...)
			var stderr bytes.Buffer

			require.Equal(t, StatusFailClosed, supervisor.Run(&stderr))
			require.Equal(t, OutcomeMonitorFailed, readOutcome(t, metadata))
			require.Contains(t, stderr.String(), "Fuzz command monitor failed")
		})
	}
}

func TestReleaseCacheReleasesOnlyTheDeadMatchingWorker(t *testing.T) {
	if testing.Short() {
		t.Skip("starts a process")
	}
	t.Parallel()

	dead := exec.Command("true")
	require.NoError(t, dead.Run())
	deadProcess := dead.Process.Pid

	tests := []struct {
		name        string
		record      string
		wantError   error
		wantRelease bool
		symlink     bool
	}{
		{name: "another command's lease", record: `{"CommandToken": "another-command", "PID": 123}`},
		{name: "a live worker", record: `{"CommandToken": "TOKEN", "PID": ` + strconv.Itoa(os.Getpid()) + `}`, wantError: errWorkerSurvived},
		{name: "a dead worker", record: `{"CommandToken": "TOKEN", "PID": ` + strconv.Itoa(deadProcess) + `}`, wantRelease: true},
		{name: "a fractional identity", record: `{"CommandToken": "TOKEN", "PID": 5.0}`, wantError: errInvalidWorker},
		{name: "a negative identity", record: `{"CommandToken": "TOKEN", "PID": -1}`, wantError: errInvalidWorker},
		{name: "a record that is not an object", record: `[1]`, wantError: errInvalidLease},
		{name: "an oversized record", record: `{"pad": "` + strings.Repeat("x", maxLeaseBytes) + `"}`, wantError: errUnsafeLease},
		{name: "a linked record", record: `{"CommandToken": "TOKEN", "PID": 1}`, symlink: true, wantError: errUnsafeLease},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			supervisor, _ := newTestSupervisor(t, 1<<40, nil, "true")
			supervisor.token = "TOKEN"
			lease := filepath.Join(supervisor.config.ManagedRoot, "programs", "lease")
			require.NoError(t, os.MkdirAll(lease, 0o700))
			owner := filepath.Join(lease, "owner.json")
			record := strings.ReplaceAll(testCase.record, "TOKEN", supervisor.token)
			if testCase.symlink {
				target := filepath.Join(supervisor.config.ManagedRoot, "elsewhere.json")
				require.NoError(t, os.WriteFile(target, []byte(record), 0o600))
				require.NoError(t, os.Symlink(target, owner))
			} else {
				require.NoError(t, os.WriteFile(owner, []byte(record), 0o600))
			}

			err := supervisor.releaseCache()

			if testCase.wantError != nil {
				require.ErrorIs(t, err, testCase.wantError)
			} else {
				require.NoError(t, err)
			}
			_, statErr := os.Stat(lease)
			require.Equal(t, testCase.wantRelease, errors.Is(statErr, os.ErrNotExist))
		})
	}
}

func TestReleaseCacheIgnoresAMissingLease(t *testing.T) {
	t.Parallel()

	supervisor, _ := newTestSupervisor(t, 1<<40, nil, "true")

	require.NoError(t, supervisor.releaseCache())
}

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

package catalogue

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	// fakeModeVariable, when set, makes the test binary act as a program RunCommand starts.
	// Its value says what the program does: fakePrint, fakeDirectory, fakeFlood or
	// fakeWait.
	fakeModeVariable = "CATALOGUE_FAKE_PROGRAM"

	// fakeStdoutVariable is what the fake program prints to standard output.
	fakeStdoutVariable = "CATALOGUE_FAKE_STDOUT"

	// fakeStderrVariable is what the fake program prints to standard error.
	fakeStderrVariable = "CATALOGUE_FAKE_STDERR"

	// fakeStatusVariable is the fake program's exit status.
	fakeStatusVariable = "CATALOGUE_FAKE_STATUS"

	// fakePrint prints fakeStdoutVariable and fakeStderrVariable and exits with fakeStatusVariable.
	fakePrint = "print"

	// fakeDirectory prints the directory the program runs in.
	fakeDirectory = "directory"

	// fakeFlood writes one byte more to standard output than RunCommand keeps.
	fakeFlood = "flood"

	// fakeWait runs until it is stopped.
	fakeWait = "wait"
)

// exitedWith is the error a stub CommandRunner returns for a program that exited with
// a status.
type exitedWith int

func (e exitedWith) Error() string { return "exit status " + strconv.Itoa(int(e)) }

func (e exitedWith) ExitCode() int { return int(e) }

// actAsFakeProgram makes the test binary act as the program fakeModeVariable describes,
// and exit, when that variable is set; otherwise it returns at once.
func actAsFakeProgram() {
	mode, ok := os.LookupEnv(fakeModeVariable)
	if !ok {
		return
	}
	switch mode {
	case fakeDirectory:
		directory, err := os.Getwd()
		if err != nil {
			os.Exit(1)
		}
		_, _ = fmt.Fprint(os.Stdout, directory)
	case fakeFlood:
		_, _ = os.Stdout.Write(bytes.Repeat([]byte("x"), maxCommandOutput+1))
	case fakeWait:
		time.Sleep(time.Hour)
	default:
		_, _ = fmt.Fprint(os.Stdout, os.Getenv(fakeStdoutVariable))
		_, _ = fmt.Fprint(os.Stderr, os.Getenv(fakeStderrVariable))
	}
	status, err := strconv.Atoi(os.Getenv(fakeStatusVariable))
	if err != nil {
		status = 0
	}
	os.Exit(status)
}

func TestRunCommandReturnsTheProgramsOutputAndStatus(t *testing.T) {
	tests := []struct {
		name       string
		stdout     string
		stderr     string
		status     int
		wantExited bool
	}{
		{name: "a program that succeeds", stdout: "out", stderr: "err", status: 0, wantExited: false},
		{name: "a program that exits 1", stdout: "findings", stderr: "", status: 1, wantExited: true},
		{name: "a program that exits 3 with no output", stdout: "", stderr: "failed", status: 3, wantExited: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(fakeModeVariable, fakePrint)
			t.Setenv(fakeStdoutVariable, test.stdout)
			t.Setenv(fakeStderrVariable, test.stderr)
			t.Setenv(fakeStatusVariable, strconv.Itoa(test.status))

			stdout, stderr, err := RunCommand(t.Context(), t.TempDir(), []string{os.Args[0]})

			require.Equal(t, test.stdout, string(stdout))
			require.Equal(t, test.stderr, string(stderr))
			status, exited := exitStatus(err)
			require.Equal(t, test.wantExited, exited)
			require.Equal(t, test.wantExited, err != nil)
			if exited {
				require.Equal(t, test.status, status)
				var exitErr *exec.ExitError
				require.ErrorAs(t, err, &exitErr)
			}
		})
	}
}

func TestRunCommandRunsTheProgramInTheDirectory(t *testing.T) {
	t.Setenv(fakeModeVariable, fakeDirectory)
	working, err := os.Getwd()
	require.NoError(t, err)
	relative, err := filepath.Rel(working, os.Args[0])
	require.NoError(t, err)
	tests := []struct {
		name    string
		program string
	}{
		{name: "an absolute program path", program: os.Args[0]},
		{name: "a program path relative to the current directory", program: relative},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			stdout, _, err := RunCommand(t.Context(), directory, []string{test.program})
			require.NoError(t, err)
			want, err := filepath.EvalSymlinks(directory)
			require.NoError(t, err)
			got, err := filepath.EvalSymlinks(string(stdout))
			require.NoError(t, err)
			require.Equal(t, want, got)
		})
	}
}

func TestRunCommandRefusesWhatItCannotRun(t *testing.T) {
	stopped := errors.New("the check was stopped")
	tests := []struct {
		want      error
		name      string
		mode      string
		arguments []string
		stopAfter time.Duration
	}{
		{name: "no program", mode: fakePrint, arguments: nil, want: ErrNoCommand, stopAfter: 0},
		{name: "more output than it keeps", mode: fakeFlood, arguments: []string{os.Args[0]}, want: ErrOutputTooLarge, stopAfter: 0},
		{name: "a run the context stops", mode: fakeWait, arguments: []string{os.Args[0]}, want: stopped, stopAfter: 200 * time.Millisecond},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(fakeModeVariable, test.mode)
			ctx := t.Context()
			if test.stopAfter > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeoutCause(ctx, test.stopAfter, stopped)
				defer cancel()
			}

			stdout, _, err := RunCommand(ctx, t.TempDir(), test.arguments)

			require.ErrorIs(t, err, test.want)
			_, exited := exitStatus(err)
			require.False(t, exited)
			require.LessOrEqual(t, len(stdout), maxCommandOutput)
		})
	}
}

func TestRunCommandReportsAProgramThatCannotStart(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-program")

	_, _, err := RunCommand(t.Context(), t.TempDir(), []string{missing})

	require.Error(t, err)
	_, exited := exitStatus(err)
	require.False(t, exited)
}

func TestCappedBufferKeepsOnlyWhatFits(t *testing.T) {
	tests := []struct {
		name          string
		writes        []string
		limit         int
		wantKept      string
		wantTruncated bool
	}{
		{name: "writes under the limit", writes: []string{"ab", "cd"}, limit: 8, wantKept: "abcd", wantTruncated: false},
		{name: "writes that reach the limit exactly", writes: []string{"abcd"}, limit: 4, wantKept: "abcd", wantTruncated: false},
		{name: "a write that crosses the limit", writes: []string{"ab", "cdef"}, limit: 3, wantKept: "abc", wantTruncated: true},
		{name: "writes after the limit", writes: []string{"abc", "d", "e"}, limit: 3, wantKept: "abc", wantTruncated: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			buffer := &cappedBuffer{limit: test.limit}
			for _, chunk := range test.writes {
				written, err := buffer.Write([]byte(chunk))
				require.NoError(t, err)
				require.Equal(t, len(chunk), written)
			}
			require.Equal(t, test.wantKept, buffer.data.String())
			require.Equal(t, test.wantTruncated, buffer.truncated)
		})
	}
}

func TestExitStatusReadsTheStatusFromAWrappedError(t *testing.T) {
	tests := []struct {
		err        error
		name       string
		wantStatus int
		wantExited bool
	}{
		{name: "no error", err: nil, wantStatus: 0, wantExited: false},
		{name: "a status", err: exitedWith(2), wantStatus: 2, wantExited: true},
		{name: "a wrapped status", err: fmt.Errorf("running sgl: %w", exitedWith(1)), wantStatus: 1, wantExited: true},
		{name: "an error without a status", err: errors.New("no such file"), wantStatus: 0, wantExited: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, exited := exitStatus(test.err)
			require.Equal(t, test.wantStatus, status)
			require.Equal(t, test.wantExited, exited)
		})
	}
}

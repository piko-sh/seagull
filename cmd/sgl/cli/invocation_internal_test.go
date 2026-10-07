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
	"errors"
	"fmt"
	"io/fs"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/internal/config"
	"seagull.sh/seagull/cmd/sgl/internal/output"
	"seagull.sh/seagull/sdk/dialect/dialect_registry"
	"seagull.sh/seagull/sdk/migrate"
)

func TestFailSaysWhatACancelledOperationWasDoing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "a bare cancellation", err: context.Canceled, want: "sgl: interrupted\n"},
		{name: "a bare signal", err: &interruptError{signal: syscall.SIGTERM}, want: "sgl: interrupted\n"},
		{name: "an operation the signal stopped", err: fmt.Errorf("replaying the migrations: %w", &interruptError{signal: syscall.SIGTERM}),
			want: "sgl: interrupted: replaying the migrations: SIGTERM received\n"},
		{name: "an operation a cancellation stopped", err: fmt.Errorf("database %q: %w", "app", context.Canceled),
			want: "sgl: interrupted: database \"app\": " + context.Canceled.Error() + "\n"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var stderr bytes.Buffer
			inv := &invocation{streams: output.IO{Stdin: nil, Stdout: &bytes.Buffer{}, Stderr: &stderr}, program: defaultProgram}

			assert.Equal(t, exitInterrupted, inv.fail(testCase.err))
			assert.Equal(t, testCase.want, stderr.String())
		})
	}
}

func TestParseLogLevel(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"debug", "INFO", "warn", "warning", "error"} {
		_, err := parseLogLevel(name)
		require.NoError(t, err, name)
	}
	_, err := parseLogLevel("loud")
	require.Error(t, err)
}

func TestExitCodeFor(t *testing.T) {
	t.Parallel()

	assert.Equal(t, exitUsage, exitCodeFor(fmt.Errorf("wrapped: %w", config.ErrInvalid)))
	assert.Equal(t, exitUsage, exitCodeFor(dialect_registry.ErrUnknownDialect))
	assert.Equal(t, exitUsage, exitCodeFor(fs.ErrNotExist))
	assert.Equal(t, exitDatabase, exitCodeFor(databaseError(errors.New("down"))))
	assert.Equal(t, exitInternal, exitCodeFor(errors.New("bug")))
	require.NoError(t, databaseError(nil))
}

func TestExitCodeForTheConfigurationSentinels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "an unset environment variable", err: fmt.Errorf("sgl.yaml: %w", config.ErrUnsetVariable), want: exitUsage},
		{name: "an invalid configuration", err: config.ErrInvalid, want: exitUsage},
		{name: "a file too large to read", err: errFileTooLarge, want: exitUsage},
		{name: "a path that is not a regular file", err: errNotRegularFile, want: exitUsage},
		{name: "anything else", err: errors.New("bug"), want: exitInternal},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, exitCodeFor(testCase.err))
		})
	}
}

func TestFailPrintsTheCommandHintUnderTheError(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer
	inv := &invocation{streams: output.IO{Stdin: nil, Stdout: &bytes.Buffer{}, Stderr: &stderr}, program: defaultProgram}
	inv.fail(&migrate.LaterMigrationError{Version: 2, Later: 3, LaterDirty: true})

	require.Equal(t, "sgl: cannot force migration 2: migration 3 after it is dirty; repair the history of migration 3 first\n"+
		"hint: run `sgl migrate repair 3` first\n", stderr.String())
}

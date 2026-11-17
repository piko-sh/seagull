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
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type brokenCommandWriter struct{ short bool }

type overcountingWriter struct{}

func (writer brokenCommandWriter) Write(data []byte) (int, error) {
	if writer.short && len(data) > 0 {
		return len(data) - 1, nil
	}
	return 0, io.ErrClosedPipe
}

func (overcountingWriter) Write(data []byte) (int, error) {
	return len(data) + 1, nil
}

func TestEveryCommandReportsFailedHelpOutput(t *testing.T) {
	t.Parallel()

	commands := []string{"init", "generate", "vet", "fmt", "explain", "describe", "schema", "migrate", "seed", "dialects", "config", "version", "help"}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			t.Parallel()

			arguments := []string{command, "--help"}
			require.Equal(t, exitOK, MainContext(t.Context(), arguments, WithIO(nil, io.Discard, io.Discard)))
			var diagnostics strings.Builder
			require.Equal(t, exitInternal, MainContext(t.Context(), arguments, WithIO(nil, brokenCommandWriter{}, &diagnostics)))
			require.Contains(t, diagnostics.String(), "writing stdout")
		})
	}
}

func TestOutputFailuresPreserveExistingFailure(t *testing.T) {
	t.Parallel()

	require.Equal(t, exitUsage, MainContext(t.Context(), []string{"unknown"}, WithIO(nil, io.Discard, brokenCommandWriter{})))
	require.Equal(t, exitInternal, MainContext(t.Context(), []string{"version"}, WithIO(nil, brokenCommandWriter{short: true}, io.Discard)))
}

func TestCheckedOutputKeepsTheFirstFailure(t *testing.T) {
	t.Parallel()

	failure := &outputFailure{err: nil, mutex: sync.Mutex{}}
	stdout := &checkedOutput{writer: overcountingWriter{}, failure: failure, name: "stdout"}
	stderr := &checkedOutput{writer: brokenCommandWriter{}, failure: failure, name: "stderr"}

	count, err := stdout.Write([]byte("text"))
	require.ErrorIs(t, err, io.ErrShortWrite)
	require.Zero(t, count, "a count beyond the buffer is not trusted")
	_, err = stderr.Write([]byte("text"))
	require.ErrorIs(t, err, io.ErrClosedPipe)

	require.ErrorIs(t, failure.Err(), io.ErrShortWrite)
	require.ErrorContains(t, failure.Err(), "writing stdout")
}

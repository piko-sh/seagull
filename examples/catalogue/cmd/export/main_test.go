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

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// examplesDirectory is the examples directory, from this command's directory.
const examplesDirectory = "../../.."

func TestRunExportsTheExamples(t *testing.T) {
	t.Parallel()

	occupied := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(occupied, "manifest.json"), []byte("{}"), 0o600))
	tests := []struct {
		name       string
		wantStdout string
		wantStderr string
		arguments  []string
		wantStatus int
	}{
		{
			name:       "a full export",
			arguments:  []string{"-examples", examplesDirectory, "-out", filepath.Join(t.TempDir(), "full")},
			wantStatus: 0,
			wantStdout: "exported ",
			wantStderr: "",
		},
		{
			name:       "an output directory that is not empty",
			arguments:  []string{"-examples", examplesDirectory, "-out", occupied},
			wantStatus: 1,
			wantStdout: "",
			wantStderr: "export: " + occupied + ": the directory is not empty\n",
		},
		{
			name:       "no output directory",
			arguments:  []string{"-examples", examplesDirectory},
			wantStatus: 2,
			wantStdout: "",
			wantStderr: "export: -out is required\n",
		},
		{
			name:       "an unknown flag",
			arguments:  []string{"-output", "x"},
			wantStatus: 2,
			wantStdout: "",
			wantStderr: "flag provided but not defined: -output",
		},
		{
			name:       "a request for help",
			arguments:  []string{"-h"},
			wantStatus: 0,
			wantStdout: "",
			wantStderr: "-examples",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			status := run(t.Context(), test.arguments, &stdout, &stderr)

			require.Equal(t, test.wantStatus, status, "stdout: %s\nstderr: %s", stdout.String(), stderr.String())
			require.Contains(t, stdout.String(), test.wantStdout)
			require.Contains(t, stderr.String(), test.wantStderr)
			if test.wantStderr == "" {
				require.Empty(t, stderr.String())
			}
		})
	}
}

func TestRunStopsWhenTheContextIsDone(t *testing.T) {
	t.Parallel()

	stopped := errors.New("interrupted")
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(stopped)
	var stdout, stderr bytes.Buffer

	status := run(ctx, []string{"-examples", examplesDirectory, "-out", t.TempDir()}, &stdout, &stderr)

	require.Equal(t, 1, status)
	require.Empty(t, stdout.String())
	require.Contains(t, stderr.String(), "interrupted")
}

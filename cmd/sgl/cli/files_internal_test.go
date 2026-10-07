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
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"piko.sh/safedisk"
)

func TestListDirectoryRefusesTooManyEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entries int
		problem string
	}{
		{name: "as many entries as allowed", entries: maxDirectoryEntries},
		{name: "one entry too many", entries: maxDirectoryEntries + 1, problem: fmt.Sprintf("it holds more than %d entries", maxDirectoryEntries)},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			directory := t.TempDir()
			for index := range testCase.entries {
				require.NoError(t, os.WriteFile(filepath.Join(directory, fmt.Sprintf("%05d_m.up.sql", index)), nil, 0o600))
			}

			entries, err := listDirectory(directory)

			if testCase.problem != "" {
				require.ErrorContains(t, err, testCase.problem)
				assert.ErrorIs(t, err, errTooManyEntries)
				assert.Equal(t, exitUsage, exitCodeFor(err))
				return
			}
			require.NoError(t, err)
			assert.Len(t, entries, testCase.entries)
		})
	}
}

func TestListDirectoryOfAMissingDirectory(t *testing.T) {
	t.Parallel()

	_, err := listDirectory(filepath.Join(t.TempDir(), "missing"))

	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestReadFileReadsOnlyARegularFileWithinItsLimit(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(directory, "small.sql"), []byte("SELECT 1;"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(directory, "folder.sql"), 0o750))

	tests := []struct {
		name    string
		file    string
		limit   int64
		want    string
		problem error
	}{
		{name: "a file within the limit", file: "small.sql", limit: 9, want: "SELECT 1;"},
		{name: "a file past the limit", file: "small.sql", limit: 8, problem: errFileTooLarge},
		{name: "a directory", file: "folder.sql", limit: 8, problem: errNotRegularFile},
		{name: "a missing file", file: "missing.sql", limit: 8, problem: fs.ErrNotExist},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			sandbox, err := safedisk.NewSandbox(directory, safedisk.ModeReadOnly)
			require.NoError(t, err)
			t.Cleanup(func() { assert.NoError(t, sandbox.Close()) })
			path := filepath.Join(directory, testCase.file)

			data, err := readFileIn(sandbox, testCase.file, path, testCase.limit)

			if testCase.problem != nil {
				require.ErrorIs(t, err, testCase.problem)
				require.ErrorContains(t, err, path)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, testCase.want, string(data))
		})
	}
}

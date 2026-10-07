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

package fakefs

import (
	"errors"
	"io/fs"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileReaderServesRegisteredContentAndInjectedErrors(t *testing.T) {
	t.Parallel()

	errInjected := errors.New("injected")
	reader := &FileReader{
		Files:          map[string][]byte{"migrations/001.sql": []byte("CREATE TABLE t (id int);"), "broken.sql": []byte("unused")},
		Directories:    map[string][]os.DirEntry{"migrations": {&DirEntry{EntryName: "001.sql", Directory: false}}, "locked": nil},
		ReadFileErrors: map[string]error{"broken.sql": errInjected},
		ReadDirErrors:  map[string]error{"locked": errInjected},
	}
	testCases := []struct {
		name          string
		reader        *FileReader
		read          func(reader *FileReader) (any, error)
		expected      any
		expectedError error
	}{
		{
			name:     "a registered file is served",
			reader:   reader,
			read:     func(reader *FileReader) (any, error) { return reader.ReadFile(t.Context(), "migrations/001.sql") },
			expected: []byte("CREATE TABLE t (id int);"),
		},
		{
			name:          "an injected file error wins over the content",
			reader:        reader,
			read:          func(reader *FileReader) (any, error) { return reader.ReadFile(t.Context(), "broken.sql") },
			expected:      []byte(nil),
			expectedError: errInjected,
		},
		{
			name:          "an unknown file does not exist",
			reader:        reader,
			read:          func(reader *FileReader) (any, error) { return reader.ReadFile(t.Context(), "missing.sql") },
			expected:      []byte(nil),
			expectedError: os.ErrNotExist,
		},
		{
			name:          "a reader with no maps holds no files",
			reader:        &FileReader{},
			read:          func(reader *FileReader) (any, error) { return reader.ReadFile(t.Context(), "migrations/001.sql") },
			expected:      []byte(nil),
			expectedError: os.ErrNotExist,
		},
		{
			name:     "a registered directory is listed",
			reader:   reader,
			read:     func(reader *FileReader) (any, error) { return reader.ReadDir(t.Context(), "migrations") },
			expected: []os.DirEntry{&DirEntry{EntryName: "001.sql", Directory: false}},
		},
		{
			name:          "an injected directory error wins over the listing",
			reader:        reader,
			read:          func(reader *FileReader) (any, error) { return reader.ReadDir(t.Context(), "locked") },
			expected:      []os.DirEntry(nil),
			expectedError: errInjected,
		},
		{
			name:          "an unknown directory does not exist",
			reader:        &FileReader{},
			read:          func(reader *FileReader) (any, error) { return reader.ReadDir(t.Context(), "migrations") },
			expected:      []os.DirEntry(nil),
			expectedError: os.ErrNotExist,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			actual, err := testCase.read(testCase.reader)

			if testCase.expectedError != nil {
				require.ErrorIs(t, err, testCase.expectedError)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, testCase.expected, actual)
		})
	}
}

func TestDirEntryReportsItsNameAndKind(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name              string
		entry             *DirEntry
		expectedDirectory bool
		expectedType      fs.FileMode
	}{
		{name: "a file", entry: &DirEntry{EntryName: "001.sql", Directory: false}, expectedDirectory: false, expectedType: 0},
		{name: "a directory", entry: &DirEntry{EntryName: "seeds", Directory: true}, expectedDirectory: true, expectedType: fs.ModeDir},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			info, err := testCase.entry.Info()

			assert.Equal(t, testCase.entry.EntryName, testCase.entry.Name())
			assert.Equal(t, testCase.expectedDirectory, testCase.entry.IsDir())
			assert.Equal(t, testCase.expectedType, testCase.entry.Type())
			require.NoError(t, err)
			require.NotNil(t, info)
			assert.Equal(t, testCase.entry.EntryName, info.Name())
			assert.Equal(t, testCase.expectedDirectory, info.IsDir())
			assert.Equal(t, testCase.expectedType, info.Mode())
			assert.Zero(t, info.Size())
			assert.True(t, info.ModTime().IsZero())
			assert.Nil(t, info.Sys())
		})
	}
}

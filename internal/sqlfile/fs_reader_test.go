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

package sqlfile

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"math"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/testkit/scaling"
)

type endlessDirectory struct {
	requested []int
}

func (directory *endlessDirectory) Open(name string) (fs.File, error) {
	if name != "huge" {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &endlessDirectoryFile{directory: directory}, nil
}

type endlessDirectoryFile struct {
	directory *endlessDirectory
	served    int
}

func (*endlessDirectoryFile) Stat() (fs.FileInfo, error) {
	return fstest.MapFS{"huge": &fstest.MapFile{Mode: fs.ModeDir}}.Stat("huge")
}

func (*endlessDirectoryFile) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (*endlessDirectoryFile) Close() error {
	return nil
}

func (file *endlessDirectoryFile) ReadDir(count int) ([]fs.DirEntry, error) {
	file.directory.requested = append(file.directory.requested, count)
	if count <= 0 {
		count = math.MaxInt32
	}
	entries := make([]fs.DirEntry, 0, min(count, 1024))
	for range count {
		file.served++
		entries = append(entries, fs.FileInfoToDirEntry(fakeFileInfo{name: fmt.Sprintf("%010d.sql", file.served)}))
	}
	return entries, nil
}

type fakeFileInfo struct {
	name string
}

func (info fakeFileInfo) Name() string  { return info.name }
func (fakeFileInfo) Size() int64        { return 0 }
func (fakeFileInfo) Mode() fs.FileMode  { return 0 }
func (fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (fakeFileInfo) IsDir() bool        { return false }
func (fakeFileInfo) Sys() any           { return nil }

func TestFSReaderReadFile(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"migrations/0001_init.up.sql":   &fstest.MapFile{Data: []byte("CREATE TABLE foo (id INT);")},
		"migrations/0002_at_cap.up.sql": &fstest.MapFile{Data: []byte(strings.Repeat("A", 1024))},
		"migrations/0003_huge.up.sql":   &fstest.MapFile{Data: []byte(strings.Repeat("A", 1025))},
	}

	tests := []struct {
		name     string
		path     string
		maxBytes int64
		wantErr  error
		wantLen  int
	}{
		{name: "returns contents", path: "migrations/0001_init.up.sql", maxBytes: 1024, wantLen: 26},
		{name: "accepts a file at the cap", path: "migrations/0002_at_cap.up.sql", maxBytes: 1024, wantLen: 1024},
		{name: "rejects an oversized file", path: "migrations/0003_huge.up.sql", maxBytes: 1024, wantErr: fault.ErrFileTooLarge},
		{name: "non-positive cap selects the default", path: "migrations/0003_huge.up.sql", maxBytes: 0, wantLen: 1025},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			content, err := NewFSReader(files, testCase.maxBytes, 0).ReadFile(t.Context(), testCase.path)
			if testCase.wantErr != nil {
				require.ErrorIs(t, err, testCase.wantErr)
				assert.ErrorIs(t, err, fault.ErrLimit)
				return
			}
			require.NoError(t, err)
			assert.Len(t, content, testCase.wantLen)
		})
	}

	_, err := NewFSReader(files, 0, 0).ReadFile(t.Context(), "missing.sql")
	require.Error(t, err)
	assert.Equal(t, DefaultMaxFileBytes, NewFSReader(files, -1, -1).maxFileBytes)
}

func TestFSReaderReadDir(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"migrations/0002_alter.up.sql": &fstest.MapFile{Data: []byte("ALTER TABLE")},
		"migrations/0001_init.up.sql":  &fstest.MapFile{Data: []byte("CREATE TABLE")},
		"migrations/0003_data.up.sql":  &fstest.MapFile{Data: []byte("INSERT INTO")},
	}

	entries, err := NewFSReader(files, 0, 0).ReadDir(t.Context(), "migrations")
	require.NoError(t, err)
	require.Len(t, entries, 3)
	assert.Equal(t, "0001_init.up.sql", entries[0].Name())
	assert.Equal(t, "0003_data.up.sql", entries[2].Name())

	_, err = NewFSReader(files, 0, 2).ReadDir(t.Context(), "migrations")
	require.ErrorIs(t, err, fault.ErrLimit)

	_, err = NewFSReader(files, 0, 0).ReadDir(t.Context(), "no-such-directory")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no-such-directory")
}

func TestFSReaderHonoursCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	reader := NewFSReader(fstest.MapFS{"a.sql": &fstest.MapFile{Data: []byte("x")}}, 0, 0)

	_, err := reader.ReadFile(ctx, "a.sql")
	require.ErrorIs(t, err, context.Canceled)
	_, err = reader.ReadDir(ctx, ".")
	require.ErrorIs(t, err, context.Canceled)
}

func TestNewFSReaderSelectsTheDefaultsAndTheCeiling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		maxFileBytes     int64
		maxEntries       int
		wantMaxFileBytes int64
		wantMaxEntries   int
	}{
		{name: "non-positive caps select the defaults", maxFileBytes: 0, maxEntries: 0, wantMaxFileBytes: DefaultMaxFileBytes, wantMaxEntries: DefaultMaxFiles},
		{name: "negative caps select the defaults", maxFileBytes: -1, maxEntries: -1, wantMaxFileBytes: DefaultMaxFileBytes, wantMaxEntries: DefaultMaxFiles},
		{name: "caps within range are kept", maxFileBytes: 1024, maxEntries: 3, wantMaxFileBytes: 1024, wantMaxEntries: 3},
		{name: "a cap at the ceiling is kept", maxFileBytes: MaxFileBytesCeiling, maxEntries: 1, wantMaxFileBytes: MaxFileBytesCeiling, wantMaxEntries: 1},
		{name: "a cap past the ceiling is lowered to it", maxFileBytes: math.MaxInt64, maxEntries: math.MaxInt, wantMaxFileBytes: MaxFileBytesCeiling, wantMaxEntries: math.MaxInt},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			reader := NewFSReader(fstest.MapFS{}, testCase.maxFileBytes, testCase.maxEntries)
			assert.Equal(t, testCase.wantMaxFileBytes, reader.maxFileBytes)
			assert.Equal(t, testCase.wantMaxEntries, reader.maxEntries)
		})
	}
}

func TestFSReaderReadsWholeFilesUnderTheLargestCap(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{"queries/q.sql": &fstest.MapFile{Data: []byte("SELECT 1;")}}
	tests := []struct {
		name         string
		maxFileBytes int64
	}{
		{name: "the largest int64", maxFileBytes: math.MaxInt64},
		{name: "the ceiling", maxFileBytes: MaxFileBytesCeiling},
		{name: "one below the largest int64", maxFileBytes: math.MaxInt64 - 1},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			content, err := NewFSReader(files, testCase.maxFileBytes, 0).ReadFile(t.Context(), "queries/q.sql")
			require.NoError(t, err)
			assert.Equal(t, "SELECT 1;", string(content))
		})
	}
}

func TestReadLimitReadsOnePastTheCapWithoutOverflowing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		maxFileBytes int64
		want         int64
	}{
		{name: "a small cap", maxFileBytes: 16, want: 17},
		{name: "the ceiling", maxFileBytes: MaxFileBytesCeiling, want: MaxFileBytesCeiling + 1},
		{name: "the largest int64", maxFileBytes: math.MaxInt64, want: math.MaxInt64},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, readLimit(testCase.maxFileBytes))
		})
	}
}

func TestFSReaderRefusesAPathThatIsNoRegularFile(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"queries/pipe.sql":   &fstest.MapFile{Mode: fs.ModeNamedPipe},
		"queries/socket.sql": &fstest.MapFile{Mode: fs.ModeSocket},
		"queries/device.sql": &fstest.MapFile{Mode: fs.ModeDevice | fs.ModeCharDevice},
		"queries/nested":     &fstest.MapFile{Mode: fs.ModeDir},
	}
	tests := []struct {
		name string
		path string
	}{
		{name: "a named pipe", path: "queries/pipe.sql"},
		{name: "a socket", path: "queries/socket.sql"},
		{name: "a device", path: "queries/device.sql"},
		{name: "a directory", path: "queries/nested"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			content, err := NewFSReader(files, 0, 0).ReadFile(t.Context(), testCase.path)
			require.ErrorIs(t, err, errNotRegularFile)
			assert.Contains(t, err.Error(), testCase.path)
			assert.Nil(t, content)
		})
	}
}

func TestFSReaderReadDirReadsOnlyOnePastTheCap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		maxEntries    int
		wantRequested int
	}{
		{name: "a small cap", maxEntries: 3, wantRequested: 4},
		{name: "the default cap", maxEntries: 0, wantRequested: DefaultMaxFiles + 1},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			directory := &endlessDirectory{}
			entries, err := NewFSReader(directory, 0, testCase.maxEntries).ReadDir(t.Context(), "huge")
			require.ErrorIs(t, err, fault.ErrLimit)
			assert.Nil(t, entries)
			assert.Equal(t, []int{testCase.wantRequested}, directory.requested)
		})
	}
}

func TestFSReaderRefusesAHugeDirectoryWithinBudget(t *testing.T) {
	t.Parallel()

	scaling.Within(t, time.Second, func(ctx context.Context) {
		_, err := NewFSReader(&endlessDirectory{}, 0, 10).ReadDir(ctx, "huge")
		require.ErrorIs(t, err, fault.ErrLimit)
	})
}

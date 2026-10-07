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
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"seagull.sh/seagull/internal/testkit/fakefs"
)

func TestReadSeedFiles_LeavesDuplicatesToDuplicateSeed(t *testing.T) {
	reader := &fakefs.FileReader{
		Directories: map[string][]os.DirEntry{
			"seeds": {
				&fakefs.DirEntry{EntryName: "001_first.sql"},
				&fakefs.DirEntry{EntryName: "001_second.sql"},
			},
		},
		Files: map[string][]byte{
			"seeds/001_first.sql":  []byte("INSERT 1;"),
			"seeds/001_second.sql": []byte("INSERT 2;"),
		},
	}

	files, err := ReadSeedFiles(t.Context(), reader, "seeds")
	require.NoError(t, err)

	first, second, found := DuplicateSeed(files)
	require.True(t, found)
	assert.Equal(t, "001_first.sql", first.Filename)
	assert.Equal(t, "001_second.sql", second.Filename)
	assert.Equal(t, int64(1), second.Version)
}

func TestDuplicateSeedWithDistinctVersions(t *testing.T) {
	first, second, found := DuplicateSeed([]SeedFile{{Version: 1, Filename: "a"}, {Version: 2, Filename: "b"}})

	assert.False(t, found)
	assert.Empty(t, first.Filename)
	assert.Empty(t, second.Filename)
}

func TestReadSeedFiles_ValidFiles(t *testing.T) {
	reader := &fakefs.FileReader{
		Directories: map[string][]os.DirEntry{
			"seeds": {
				&fakefs.DirEntry{EntryName: "001_users.sql"},
				&fakefs.DirEntry{EntryName: "002_posts.sql"},
				&fakefs.DirEntry{EntryName: "010_comments.sql"},
			},
		},
		Files: map[string][]byte{
			"seeds/001_users.sql":    []byte("INSERT INTO users (name) VALUES ('a');"),
			"seeds/002_posts.sql":    []byte("INSERT INTO posts (title) VALUES ('b');"),
			"seeds/010_comments.sql": []byte("INSERT INTO comments (body) VALUES ('c');"),
		},
	}

	files, err := ReadSeedFiles(t.Context(), reader, "seeds")
	require.NoError(t, err)
	require.Len(t, files, 3)

	assert.Equal(t, int64(1), files[0].Version)
	assert.Equal(t, "users", files[0].Name)
	assert.Equal(t, "001_users.sql", files[0].Filename)

	assert.Equal(t, int64(2), files[1].Version)
	assert.Equal(t, "posts", files[1].Name)

	assert.Equal(t, int64(10), files[2].Version)
	assert.Equal(t, "comments", files[2].Name)
}

func TestReadSeedFiles_SortedByVersion(t *testing.T) {
	reader := &fakefs.FileReader{
		Directories: map[string][]os.DirEntry{
			"seeds": {
				&fakefs.DirEntry{EntryName: "003_third.sql"},
				&fakefs.DirEntry{EntryName: "001_first.sql"},
				&fakefs.DirEntry{EntryName: "002_second.sql"},
			},
		},
		Files: map[string][]byte{
			"seeds/003_third.sql":  []byte("c"),
			"seeds/001_first.sql":  []byte("a"),
			"seeds/002_second.sql": []byte("b"),
		},
	}

	files, err := ReadSeedFiles(t.Context(), reader, "seeds")
	require.NoError(t, err)
	require.Len(t, files, 3)

	assert.Equal(t, int64(1), files[0].Version)
	assert.Equal(t, int64(2), files[1].Version)
	assert.Equal(t, int64(3), files[2].Version)
}

func TestReadSeedFiles_SkipsInvalidFiles(t *testing.T) {
	reader := &fakefs.FileReader{
		Directories: map[string][]os.DirEntry{
			"seeds": {
				&fakefs.DirEntry{EntryName: "001_valid.sql"},
				&fakefs.DirEntry{EntryName: "README.md"},
				&fakefs.DirEntry{EntryName: "no_version.sql"},
				&fakefs.DirEntry{EntryName: "subdir", Directory: true},
				&fakefs.DirEntry{EntryName: "003_migration.up.sql"},
				&fakefs.DirEntry{EntryName: "002_another.sql"},
			},
		},
		Files: map[string][]byte{
			"seeds/001_valid.sql":        []byte("INSERT 1;"),
			"seeds/003_migration.up.sql": []byte("CREATE TABLE x;"),
			"seeds/002_another.sql":      []byte("INSERT 2;"),
		},
	}

	files, err := ReadSeedFiles(t.Context(), reader, "seeds")
	require.NoError(t, err)

	require.Len(t, files, 3)
	assert.Equal(t, "valid", files[0].Name)
	assert.Equal(t, "another", files[1].Name)
	assert.Equal(t, "migration.up", files[2].Name)
}

func TestReadSeedFiles_EmptyDirectory(t *testing.T) {
	reader := &fakefs.FileReader{
		Directories: map[string][]os.DirEntry{
			"seeds": {},
		},
	}

	files, err := ReadSeedFiles(t.Context(), reader, "seeds")
	require.NoError(t, err)
	assert.Empty(t, files)
}

func TestReadSeedFiles_ChecksumComputed(t *testing.T) {
	content := []byte("INSERT INTO users (name) VALUES ('test');")
	reader := &fakefs.FileReader{
		Directories: map[string][]os.DirEntry{
			"seeds": {
				&fakefs.DirEntry{EntryName: "001_test.sql"},
			},
		},
		Files: map[string][]byte{
			"seeds/001_test.sql": content,
		},
	}

	files, err := ReadSeedFiles(t.Context(), reader, "seeds")
	require.NoError(t, err)
	require.Len(t, files, 1)

	expected := seedChecksum(content)
	assert.Equal(t, expected, files[0].Checksum)
}

func TestReadSeedFiles_DirectoryReadError(t *testing.T) {
	reader := &fakefs.FileReader{
		Directories:   map[string][]os.DirEntry{},
		ReadDirErrors: map[string]error{"seeds": os.ErrNotExist},
	}

	_, err := ReadSeedFiles(t.Context(), reader, "seeds")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading seed directory")
}

func seedChecksum(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

func TestReadSeedFilesLeavesOutFilesNoReaderCanLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mode fs.FileMode
	}{
		{name: "a named pipe", mode: fs.ModeNamedPipe},
		{name: "a socket", mode: fs.ModeSocket},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files := fstest.MapFS{
				"seeds/001_demo.sql": &fstest.MapFile{Data: []byte("INSERT INTO t VALUES (1);")},
				"seeds/002_pipe.sql": &fstest.MapFile{Mode: testCase.mode},
			}
			seeds, err := ReadSeedFiles(t.Context(), NewFSReader(files, 0, 0), "seeds")
			require.NoError(t, err)
			require.Len(t, seeds, 1)
			assert.Equal(t, "001_demo.sql", seeds[0].Filename)
		})
	}
}

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

package session_test

import (
	"fmt"
	"io/fs"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/wasm/internal/session"
)

type countingFS struct {
	fstest.MapFS
	read *atomic.Int64
}

func (filesystem countingFS) Open(name string) (fs.File, error) {
	file, err := filesystem.MapFS.Open(name)
	if err != nil {
		return nil, err
	}
	if _, isDirectory := file.(fs.ReadDirFile); isDirectory {
		return file, nil
	}
	return countingFile{File: file, read: filesystem.read}, nil
}

type countingFile struct {
	fs.File
	read *atomic.Int64
}

func (file countingFile) Read(buffer []byte) (int, error) {
	count, err := file.File.Read(buffer)
	file.read.Add(int64(count))
	return count, err
}

func TestLoadRequestReadsTheConfiguredDirectories(t *testing.T) {
	t.Parallel()

	filesystem := fstest.MapFS{
		"sgl.yaml":                  {Data: []byte("version: 1\ndatabases:\n  - name: app\n    dialect: sqlite\n    migrations: schema\n")},
		"schema/0001_init.up.sql":   {Data: []byte("CREATE TABLE t (id INTEGER);\n")},
		"schema/README.md":          {Data: []byte("notes")},
		"db/queries/t.sql":          {Data: []byte("-- sgl.query(ListT, many)\nSELECT id FROM t;\n")},
		"db/queries/nested/x.sql":   {Data: []byte("SELECT 1;\n")},
		"db/seeds/0001_t.sql":       {Data: []byte("INSERT INTO t VALUES (1);\n")},
		"internal/store/models.go":  {Data: []byte("package store\n")},
		"db/migrations/ignored.sql": {Data: []byte("SELECT 1;\n")},
	}

	request, err := session.LoadRequest(filesystem)

	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"sgl.yaml":                "version: 1\ndatabases:\n  - name: app\n    dialect: sqlite\n    migrations: schema\n",
		"schema/0001_init.up.sql": "CREATE TABLE t (id INTEGER);\n",
		"db/queries/t.sql":        "-- sgl.query(ListT, many)\nSELECT id FROM t;\n",
	}, request.Files)
	require.Equal(t, session.ProtocolVersion, request.V)
}

func TestLoadRequestRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	_, err := session.LoadRequest(fstest.MapFS{"sgl.yaml": {Data: []byte("version: 7\n")}})

	require.ErrorContains(t, err, "sgl.yaml: invalid configuration")
}

func TestLoadRequestReadsOnlyWhatARequestMayHold(t *testing.T) {
	t.Parallel()

	manyFiles := fstest.MapFS{}
	for index := range session.MaxFiles + 1 {
		manyFiles[fmt.Sprintf("db/queries/%02d.sql", index)] = &fstest.MapFile{Data: []byte("SELECT 1;\n")}
	}
	largeTotal := fstest.MapFS{}
	for index := range 5 {
		largeTotal[fmt.Sprintf("db/queries/%d.sql", index)] = &fstest.MapFile{Data: []byte(strings.Repeat("-", session.MaxFileBytes-1))}
	}
	manyEntries := fstest.MapFS{}
	for index := range 1025 {
		manyEntries[fmt.Sprintf("db/queries/%04d.txt", index)] = &fstest.MapFile{Data: nil}
	}
	testCases := []struct {
		filesystem fstest.MapFS
		name       string
		message    string
		maxRead    int64
	}{
		{
			name:       "a file larger than a request may hold",
			filesystem: fstest.MapFS{"db/queries/big.sql": {Data: []byte(strings.Repeat("-", 4<<20))}},
			message:    "db/queries/big.sql is more than the playground takes: files of at most 262144 bytes",
			maxRead:    session.MaxFileBytes + 1,
		},
		{
			name:       "a configuration file larger than a request may hold",
			filesystem: fstest.MapFS{"sgl.yaml": {Data: []byte("version: 1\n" + strings.Repeat("#", 4<<20))}},
			message:    "sgl.yaml is more than the playground takes: files of at most 262144 bytes",
			maxRead:    session.MaxFileBytes + 1,
		},
		{
			name:       "more files than a request may hold",
			filesystem: manyFiles,
			message:    "the project holds more than the playground takes: at most 64 files",
			maxRead:    int64(session.MaxFiles * len("SELECT 1;\n")),
		},
		{
			name:       "more bytes in all than a request may hold",
			filesystem: largeTotal,
			message:    "the project holds more than the playground takes: at most 1048576 bytes in all",
			maxRead:    5 * session.MaxFileBytes,
		},
		{
			name:       "a directory with more entries than are listed",
			filesystem: manyEntries,
			message:    "db/queries holds more than the playground takes: at most 1024 entries",
			maxRead:    0,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			read := &atomic.Int64{}

			_, err := session.LoadRequest(countingFS{MapFS: testCase.filesystem, read: read})

			require.EqualError(t, err, testCase.message)
			require.LessOrEqual(t, read.Load(), testCase.maxRead)
		})
	}
}

func TestLoadRequestSkipsWhatIsNotARegularFile(t *testing.T) {
	t.Parallel()

	filesystem := fstest.MapFS{
		"sgl.yaml":              {Data: nil, Mode: fs.ModeNamedPipe},
		"sgl.yml":               {Data: []byte("version: 1\ndatabases:\n  - name: app\n    dialect: sqlite\n")},
		"db/queries/pipe.sql":   {Data: nil, Mode: fs.ModeNamedPipe},
		"db/queries/device.sql": {Data: nil, Mode: fs.ModeDevice},
		"db/queries/t.sql":      {Data: []byte("SELECT 1;\n")},
	}

	request, err := session.LoadRequest(filesystem)

	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"sgl.yml":          "version: 1\ndatabases:\n  - name: app\n    dialect: sqlite\n",
		"db/queries/t.sql": "SELECT 1;\n",
	}, request.Files)
}

func TestLoadRequestReadsADirectoryNamedTwiceOnce(t *testing.T) {
	t.Parallel()

	filesystem := fstest.MapFS{
		"sgl.yaml":  {Data: []byte("version: 1\ndatabases:\n  - name: app\n    dialect: sqlite\n    migrations: sql\n    queries: sql\n")},
		"sql/t.sql": {Data: []byte(strings.Repeat("-", session.MaxFileBytes))},
		"sql/u.sql": {Data: []byte(strings.Repeat("-", session.MaxFileBytes))},
		"sql/v.sql": {Data: []byte(strings.Repeat("-", session.MaxFileBytes-1024))},
	}

	request, err := session.LoadRequest(filesystem)

	require.NoError(t, err)
	require.Len(t, request.Files, 4)
}

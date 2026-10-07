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
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/dialect/dialect_engine_clickhouse"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_duckdb"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_mysql"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_postgres"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
)

func TestAnchorDSNResolvesRelativeSQLiteFiles(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("the expected paths are POSIX paths")
	}
	testCases := []struct {
		name      string
		dsn       string
		directory string
		want      string
	}{
		{name: "empty", dsn: "", directory: "/project", want: ""},
		{name: "plain relative file", dsn: "club.db", directory: "/project", want: "/project/club.db"},
		{name: "plain file below a directory", dsn: "data/../db/club.db", directory: "/project", want: "/project/db/club.db"},
		{name: "plain file with parameters", dsn: "club.db?_pragma=foreign_keys(1)", directory: "/project", want: "/project/club.db?_pragma=foreign_keys(1)"},
		{name: "plain absolute file", dsn: "/data/club.db", directory: "/project", want: "/data/club.db"},
		{name: "memory", dsn: ":memory:", directory: "/project", want: ":memory:"},
		{name: "memory with parameters", dsn: ":memory:?_pragma=foreign_keys(1)", directory: "/project", want: ":memory:?_pragma=foreign_keys(1)"},
		{name: "relative URI", dsn: "file:club.db", directory: "/project", want: "file:/project/club.db"},
		{name: "relative URI with a query", dsn: "file:club.db?mode=ro&_pragma=busy_timeout(5000)", directory: "/project", want: "file:/project/club.db?mode=ro&_pragma=busy_timeout(5000)"},
		{name: "relative URI with a fragment", dsn: "file:./club.db#notes", directory: "/project", want: "file:/project/club.db#notes"},
		{name: "relative URI in an awkward directory", dsn: "file:club.db", directory: "/my project/50%?#", want: "file:/my project/50%25%3F%23/club.db"},
		{name: "absolute URI", dsn: "file:/data/club.db?mode=ro", directory: "/project", want: "file:/data/club.db?mode=ro"},
		{name: "URI with an authority", dsn: "file:///data/club.db", directory: "/project", want: "file:///data/club.db"},
		{name: "memory URI", dsn: "file::memory:?cache=shared", directory: "/project", want: "file::memory:?cache=shared"},
		{name: "named memory URI", dsn: "file:shared?mode=memory&cache=shared", directory: "/project", want: "file:shared?mode=memory&cache=shared"},
		{name: "memdb URI", dsn: "file:shared.db?vfs=memdb", directory: "/project", want: "file:shared.db?vfs=memdb"},
		{name: "temporary database URI", dsn: "file:?cache=private", directory: "/project", want: "file:?cache=private"},
		{name: "remote URL", dsn: "libsql://db.example.com/club", directory: "/project", want: "libsql://db.example.com/club"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, anchorDSN(dialect_engine_sqlite.Family, testCase.dsn, testCase.directory))
		})
	}
}

func TestAnchorDSNLeavesOtherEnginesAlone(t *testing.T) {
	t.Parallel()

	directory := filepath.Join(t.TempDir(), "project")
	for family, dsn := range map[string]string{
		dialect_engine_postgres.Family:   "host=localhost dbname=club",
		dialect_engine_mysql.Family:      "club:secret@tcp(localhost:3306)/club",
		dialect_engine_clickhouse.Family: "clickhouse://localhost:9000/club",
		dialect_engine_duckdb.Family:     "club.duckdb",
	} {
		require.Equal(t, dsn, anchorDSN(family, dsn, directory), family)
	}
}

func TestSQLiteFileNamesTheFileAConnectionStringOpens(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("the expected paths are POSIX paths")
	}
	testCases := []struct {
		name   string
		family string
		dsn    string
		want   string
		isFile bool
	}{
		{name: "plain file", family: dialect_engine_sqlite.Family, dsn: "/project/club.db", want: "/project/club.db", isFile: true},
		{name: "plain file with parameters", family: dialect_engine_sqlite.Family, dsn: "/project/club.db?_pragma=foreign_keys(1)", want: "/project/club.db", isFile: true},
		{name: "URI", family: dialect_engine_sqlite.Family, dsn: "file:/project/club.db?mode=ro", want: "/project/club.db", isFile: true},
		{name: "escaped URI", family: dialect_engine_sqlite.Family, dsn: "file:/my%20project/50%25.db", want: "/my project/50%.db", isFile: true},
		{name: "URI with an empty authority", family: dialect_engine_sqlite.Family, dsn: "file:///data/club.db", want: "/data/club.db", isFile: true},
		{name: "URI with a host", family: dialect_engine_sqlite.Family, dsn: "file://host/club.db"},
		{name: "memory", family: dialect_engine_sqlite.Family, dsn: ":memory:"},
		{name: "memory URI", family: dialect_engine_sqlite.Family, dsn: "file::memory:?cache=shared"},
		{name: "named memory URI", family: dialect_engine_sqlite.Family, dsn: "file:shared?mode=memory&cache=shared"},
		{name: "memdb", family: dialect_engine_sqlite.Family, dsn: "shared.db?vfs=memdb"},
		{name: "temporary database", family: dialect_engine_sqlite.Family, dsn: "file:?cache=private"},
		{name: "remote URL", family: dialect_engine_sqlite.Family, dsn: "libsql://db.example.com/club"},
		{name: "another family", family: dialect_engine_postgres.Family, dsn: "/project/club.db"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			name, isFile := sqliteFile(testCase.family, testCase.dsn)
			require.Equal(t, testCase.isFile, isFile)
			if testCase.isFile {
				require.Equal(t, testCase.want, name)
			}
		})
	}
}

func TestRequireSchemaFileRefusesAMissingSQLiteFile(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	missing := filepath.Join(directory, "missing.db")
	inv := &invocation{program: "seagull"}
	require.ErrorContains(t, inv.requireSchemaFile(dialect_engine_sqlite.Family, missing), "does not exist; seagull reads a schema")
	require.NoFileExists(t, missing)
	require.NoError(t, inv.requireSchemaFile(dialect_engine_sqlite.Family, ":memory:"))
	require.NoError(t, inv.requireSchemaFile(dialect_engine_postgres.Family, missing))
}

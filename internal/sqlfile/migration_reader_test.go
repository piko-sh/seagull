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
	"errors"
	"io/fs"
	"os"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/testkit/fakefs"
)

func TestReadMigrationFilesVersionedLeavesDuplicatesToDuplicateMigration(t *testing.T) {
	t.Parallel()

	reader := &fakefs.FileReader{
		Directories: map[string][]os.DirEntry{
			"migrations": {
				&fakefs.DirEntry{EntryName: "001_first.up.sql"},
				&fakefs.DirEntry{EntryName: "001_first.down.sql"},
				&fakefs.DirEntry{EntryName: "001_second.up.sql"},
				&fakefs.DirEntry{EntryName: "002_third.up.sql"},
			},
		},
		Files: map[string][]byte{
			"migrations/001_first.up.sql":   []byte("CREATE TABLE a;"),
			"migrations/001_first.down.sql": []byte("DROP TABLE a;"),
			"migrations/001_second.up.sql":  []byte("CREATE TABLE b;"),
			"migrations/002_third.up.sql":   []byte("CREATE TABLE c;"),
		},
	}

	files, err := ReadMigrationFilesVersioned(t.Context(), reader, "migrations")
	require.NoError(t, err)
	require.Len(t, files, 4)

	first, second, found := DuplicateMigration(files)
	require.True(t, found)
	assert.Equal(t, "001_first.up.sql", first.Filename)
	assert.Equal(t, "001_second.up.sql", second.Filename)
	assert.False(t, second.Down)
}

func TestDuplicateMigration(t *testing.T) {
	t.Parallel()

	upMigration := func(version int64, filename string) MigrationFile {
		return MigrationFile{Version: version, Filename: filename}
	}
	downMigration := func(version int64, filename string) MigrationFile {
		return MigrationFile{Version: version, Filename: filename, Down: true}
	}

	tests := []struct {
		name       string
		files      []MigrationFile
		wantFirst  string
		wantSecond string
		wantFound  bool
	}{
		{name: "no files", files: nil},
		{name: "up and down of one version", files: []MigrationFile{upMigration(1, "a.up"), downMigration(1, "a.down")}},
		{name: "distinct versions", files: []MigrationFile{upMigration(1, "a.up"), upMigration(2, "b.up")}},
		{name: "two up files", files: []MigrationFile{upMigration(1, "a.up"), upMigration(1, "b.up")}, wantFirst: "a.up", wantSecond: "b.up", wantFound: true},
		{name: "two down files", files: []MigrationFile{upMigration(1, "a.up"), downMigration(1, "a.down"), downMigration(1, "b.down")}, wantFirst: "a.down", wantSecond: "b.down", wantFound: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			first, second, found := DuplicateMigration(testCase.files)
			assert.Equal(t, testCase.wantFound, found)
			assert.Equal(t, testCase.wantFirst, first.Filename)
			assert.Equal(t, testCase.wantSecond, second.Filename)
		})
	}
}

func TestReadMigrationFilesVersioned(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		reader         *fakefs.FileReader
		directory      string
		wantFiles      []MigrationFile
		wantErrMessage string
	}{
		{
			name: "parses version and name from filename",
			reader: &fakefs.FileReader{
				Directories: map[string][]os.DirEntry{
					"/migrations": {
						&fakefs.DirEntry{EntryName: "001_create_users.up.sql", Directory: false},
					},
				},
				Files: map[string][]byte{
					"/migrations/001_create_users.up.sql": []byte("CREATE TABLE users;"),
				},
			},
			directory: "/migrations",
			wantFiles: []MigrationFile{
				{
					Version:  1,
					Name:     "create_users",
					Down:     false,
					Filename: "001_create_users.up.sql",
					Content:  []byte("CREATE TABLE users;"),
					Checksum: computeChecksum([]byte("CREATE TABLE users;")),
				},
			},
		},
		{
			name: "parses down migration",
			reader: &fakefs.FileReader{
				Directories: map[string][]os.DirEntry{
					"/migrations": {
						&fakefs.DirEntry{EntryName: "001_create_users.down.sql", Directory: false},
					},
				},
				Files: map[string][]byte{
					"/migrations/001_create_users.down.sql": []byte("DROP TABLE users;"),
				},
			},
			directory: "/migrations",
			wantFiles: []MigrationFile{
				{
					Version:  1,
					Name:     "create_users",
					Down:     true,
					Filename: "001_create_users.down.sql",
					Content:  []byte("DROP TABLE users;"),
					Checksum: computeChecksum([]byte("DROP TABLE users;")),
				},
			},
		},
		{
			name: "sorts by version ascending",
			reader: &fakefs.FileReader{
				Directories: map[string][]os.DirEntry{
					"/migrations": {
						&fakefs.DirEntry{EntryName: "002_add_email.up.sql", Directory: false},
						&fakefs.DirEntry{EntryName: "001_create_users.up.sql", Directory: false},
					},
				},
				Files: map[string][]byte{
					"/migrations/002_add_email.up.sql":    []byte("ALTER TABLE users ADD email;"),
					"/migrations/001_create_users.up.sql": []byte("CREATE TABLE users;"),
				},
			},
			directory: "/migrations",
			wantFiles: []MigrationFile{
				{
					Version:  1,
					Name:     "create_users",
					Down:     false,
					Filename: "001_create_users.up.sql",
					Content:  []byte("CREATE TABLE users;"),
					Checksum: computeChecksum([]byte("CREATE TABLE users;")),
				},
				{
					Version:  2,
					Name:     "add_email",
					Down:     false,
					Filename: "002_add_email.up.sql",
					Content:  []byte("ALTER TABLE users ADD email;"),
					Checksum: computeChecksum([]byte("ALTER TABLE users ADD email;")),
				},
			},
		},
		{
			name: "sorts up before down within same version",
			reader: &fakefs.FileReader{
				Directories: map[string][]os.DirEntry{
					"/migrations": {
						&fakefs.DirEntry{EntryName: "001_create_users.down.sql", Directory: false},
						&fakefs.DirEntry{EntryName: "001_create_users.up.sql", Directory: false},
					},
				},
				Files: map[string][]byte{
					"/migrations/001_create_users.down.sql": []byte("DROP TABLE users;"),
					"/migrations/001_create_users.up.sql":   []byte("CREATE TABLE users;"),
				},
			},
			directory: "/migrations",
			wantFiles: []MigrationFile{
				{
					Version:  1,
					Name:     "create_users",
					Down:     false,
					Filename: "001_create_users.up.sql",
					Content:  []byte("CREATE TABLE users;"),
					Checksum: computeChecksum([]byte("CREATE TABLE users;")),
				},
				{
					Version:  1,
					Name:     "create_users",
					Down:     true,
					Filename: "001_create_users.down.sql",
					Content:  []byte("DROP TABLE users;"),
					Checksum: computeChecksum([]byte("DROP TABLE users;")),
				},
			},
		},
		{
			name: "skips directories",
			reader: &fakefs.FileReader{
				Directories: map[string][]os.DirEntry{
					"/migrations": {
						&fakefs.DirEntry{EntryName: "subdir", Directory: true},
						&fakefs.DirEntry{EntryName: "001_create_users.up.sql", Directory: false},
					},
				},
				Files: map[string][]byte{
					"/migrations/001_create_users.up.sql": []byte("CREATE TABLE users;"),
				},
			},
			directory: "/migrations",
			wantFiles: []MigrationFile{
				{
					Version:  1,
					Name:     "create_users",
					Down:     false,
					Filename: "001_create_users.up.sql",
					Content:  []byte("CREATE TABLE users;"),
					Checksum: computeChecksum([]byte("CREATE TABLE users;")),
				},
			},
		},
		{
			name: "skips non-matching filenames",
			reader: &fakefs.FileReader{
				Directories: map[string][]os.DirEntry{
					"/migrations": {
						&fakefs.DirEntry{EntryName: "README.md", Directory: false},
						&fakefs.DirEntry{EntryName: "001_create_users.up.sql", Directory: false},
					},
				},
				Files: map[string][]byte{
					"/migrations/001_create_users.up.sql": []byte("CREATE TABLE users;"),
				},
			},
			directory: "/migrations",
			wantFiles: []MigrationFile{
				{
					Version:  1,
					Name:     "create_users",
					Down:     false,
					Filename: "001_create_users.up.sql",
					Content:  []byte("CREATE TABLE users;"),
					Checksum: computeChecksum([]byte("CREATE TABLE users;")),
				},
			},
		},
		{
			name: "ReadDir error returns wrapped error",
			reader: &fakefs.FileReader{
				ReadDirErrors: map[string]error{
					"/migrations": errors.New("permission denied"),
				},
			},
			directory:      "/migrations",
			wantErrMessage: "reading migration directory",
		},
		{
			name: "ReadFile error returns wrapped error",
			reader: &fakefs.FileReader{
				Directories: map[string][]os.DirEntry{
					"/migrations": {
						&fakefs.DirEntry{EntryName: "001_create_users.up.sql", Directory: false},
					},
				},
				ReadFileErrors: map[string]error{
					"/migrations/001_create_users.up.sql": errors.New("disk failure"),
				},
			},
			directory:      "/migrations",
			wantErrMessage: "reading migration file",
		},
		{
			name: "computes SHA-256 checksum",
			reader: &fakefs.FileReader{
				Directories: map[string][]os.DirEntry{
					"/migrations": {
						&fakefs.DirEntry{EntryName: "001_init.up.sql", Directory: false},
					},
				},
				Files: map[string][]byte{
					"/migrations/001_init.up.sql": []byte("SELECT 1;"),
				},
			},
			directory: "/migrations",
			wantFiles: []MigrationFile{
				{
					Version:  1,
					Name:     "init",
					Down:     false,
					Filename: "001_init.up.sql",
					Content:  []byte("SELECT 1;"),
					Checksum: computeChecksum([]byte("SELECT 1;")),
				},
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			got, err := ReadMigrationFilesVersioned(ctx, testCase.reader, testCase.directory)

			if testCase.wantErrMessage != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.wantErrMessage)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.wantFiles, got)
		})
	}
}

func TestComputeChecksum(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content []byte
		check   func(t *testing.T, result string)
	}{
		{
			name:    "deterministic for same content",
			content: []byte("CREATE TABLE users;"),
			check: func(t *testing.T, result string) {
				t.Helper()

				again := computeChecksum([]byte("CREATE TABLE users;"))
				assert.Equal(t, again, result, "checksum should be deterministic")
			},
		},
		{
			name:    "different content produces different checksum",
			content: []byte("CREATE TABLE users;"),
			check: func(t *testing.T, result string) {
				t.Helper()
				other := computeChecksum([]byte("DROP TABLE users;"))
				assert.NotEqual(t, result, other, "different content should produce different checksums")
			},
		},
		{
			name:    "empty content produces valid hex string",
			content: []byte{},
			check: func(t *testing.T, result string) {
				t.Helper()

				expected := sha256.Sum256([]byte{})
				assert.Equal(t, hex.EncodeToString(expected[:]), result)
			},
		},
		{
			name:    "result is 64 hex characters for SHA-256",
			content: []byte("anything"),
			check: func(t *testing.T, result string) {
				t.Helper()
				assert.Len(t, result, 64, "SHA-256 hex digest should be 64 characters")

				for _, character := range result {
					assert.True(t,
						(character >= '0' && character <= '9') || (character >= 'a' && character <= 'f'),
						"character %q should be a lowercase hex digit", character,
					)
				}
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := computeChecksum(testCase.content)
			testCase.check(t, result)
		})
	}
}

func TestMigrationNamingDiagnosticsWarnAboutEachIgnoredFile(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		reader        *fakefs.FileReader
		name          string
		wantFilenames []string
	}{
		{
			name: "warns about a .sql file outside the versioned naming scheme",
			reader: &fakefs.FileReader{
				Directories: map[string][]os.DirEntry{
					"/migrations": {
						&fakefs.DirEntry{EntryName: "001_init.up.sql", Directory: false},
						&fakefs.DirEntry{EntryName: "001_init.down.sql", Directory: false},
						&fakefs.DirEntry{EntryName: "notes.sql", Directory: false},
						&fakefs.DirEntry{EntryName: "init.up.sql", Directory: false},
						&fakefs.DirEntry{EntryName: "README.md", Directory: false},
						&fakefs.DirEntry{EntryName: "archive.sql", Directory: true},
					},
				},
			},
			wantFilenames: []string{"notes.sql", "init.up.sql"},
		},
		{
			name: "returns nothing when every file conforms",
			reader: &fakefs.FileReader{
				Directories: map[string][]os.DirEntry{
					"/migrations": {
						&fakefs.DirEntry{EntryName: "001_init.up.sql", Directory: false},
					},
				},
			},
			wantFilenames: nil,
		},
		{
			name:          "returns nothing when the directory cannot be read",
			reader:        &fakefs.FileReader{},
			wantFilenames: nil,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			listed, _ := ListMigrationFiles(t.Context(), testCase.reader, "/migrations")
			diagnostics := MigrationNamingDiagnostics(t.Context(), testCase.reader, "/migrations", listed)

			var filenames []string
			for _, diagnostic := range diagnostics {
				filenames = append(filenames, diagnostic.Filename)
				assert.Equal(t, diag.CodeIgnoredMigrationFile, diagnostic.Code)
				assert.Equal(t, diag.SeverityWarning, diagnostic.Severity)
				assert.Contains(t, diagnostic.Message, "must be named <version>_<name>.up.sql")
			}
			assert.Equal(t, testCase.wantFilenames, filenames)
		})
	}
}

func migrationEntries(names ...string) *fakefs.FileReader {
	entries := make([]os.DirEntry, 0, len(names))
	for _, name := range names {
		entries = append(entries, &fakefs.DirEntry{EntryName: name, Directory: false})
	}
	return &fakefs.FileReader{Directories: map[string][]os.DirEntry{"db/migrations": entries}}
}

func TestMigrationNamingDiagnosticsRefuseADirectoryWithoutAnUpMigration(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		reader         *fakefs.FileReader
		wantFilename   string
		wantMessage    []string
		wantSuggestion string
	}{
		{
			name:           "every file is ignored",
			reader:         migrationEntries("001_schema.sql", "002_orders.sql", "notes.txt"),
			wantFilename:   "001_schema.sql",
			wantMessage:    []string{`"db/migrations"`, "001_schema.sql and 1 more .sql file are ignored", "<version>_<name>.up.sql"},
			wantSuggestion: "rename 001_schema.sql to 001_schema.up.sql",
		},
		{
			name:           "the one file is ignored",
			reader:         migrationEntries("schema.sql"),
			wantFilename:   "schema.sql",
			wantMessage:    []string{`"db/migrations"`, "schema.sql is ignored", "<version>_<name>.up.sql"},
			wantSuggestion: "name each migration <version>_<name>.up.sql, such as 001_schema.up.sql",
		},
		{
			name:           "only down migrations",
			reader:         migrationEntries("001_schema.down.sql"),
			wantFilename:   "001_schema.down.sql",
			wantMessage:    []string{`"db/migrations"`, "001_schema.down.sql is a down migration", "<version>_<name>.up.sql"},
			wantSuggestion: "add the up migration 001_schema.up.sql",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			listed, err := ListMigrationFiles(t.Context(), testCase.reader, "db/migrations")
			require.NoError(t, err)
			diagnostics := MigrationNamingDiagnostics(t.Context(), testCase.reader, "db/migrations", listed)

			require.Len(t, diagnostics, 1)
			diagnostic := diagnostics[0]
			assert.Equal(t, diag.CodeIgnoredMigrationFile, diagnostic.Code)
			assert.Equal(t, diag.SeverityError, diagnostic.Severity)
			assert.Equal(t, testCase.wantFilename, diagnostic.Filename)
			for _, fragment := range testCase.wantMessage {
				assert.Contains(t, diagnostic.Message, fragment)
			}
			assert.Equal(t, testCase.wantSuggestion, diagnostic.Suggestion)
		})
	}
}

func nestedMigrationEntries(top []string, nested map[string][]string) *fakefs.FileReader {
	reader := migrationEntries(top...)
	for subdirectory, names := range nested {
		reader.Directories["db/migrations"] = append(reader.Directories["db/migrations"], &fakefs.DirEntry{EntryName: subdirectory, Directory: true})
		entries := make([]os.DirEntry, 0, len(names))
		for _, name := range names {
			entries = append(entries, &fakefs.DirEntry{EntryName: name, Directory: false})
		}
		reader.Directories["db/migrations/"+subdirectory] = entries
	}
	return reader
}

func TestMigrationNamingDiagnosticsReportMigrationsInSubdirectories(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		reader         *fakefs.FileReader
		name           string
		wantFilename   string
		wantSuggestion string
		wantSeverity   diag.ErrorSeverity
	}{
		{
			name:           "a directory per migration, up and down inside",
			reader:         nestedMigrationEntries(nil, map[string][]string{"01_init": {"down.sql", "up.sql"}}),
			wantFilename:   "01_init/up.sql",
			wantSuggestion: "move 01_init/up.sql into the migration directory as 01_init.up.sql",
			wantSeverity:   diag.SeverityError,
		},
		{
			name:           "a directory per migration holding one migration.sql",
			reader:         nestedMigrationEntries(nil, map[string][]string{"20250101000000_init": {"migration.sql"}}),
			wantFilename:   "20250101000000_init/migration.sql",
			wantSuggestion: "move 20250101000000_init/migration.sql into the migration directory as 20250101000000_init.up.sql",
			wantSeverity:   diag.SeverityError,
		},
		{
			name:           "a directory holding only a down half",
			reader:         nestedMigrationEntries(nil, map[string][]string{"01_init": {"down.sql"}}),
			wantFilename:   "01_init/down.sql",
			wantSuggestion: "move 01_init/down.sql into the migration directory as 01_init.down.sql",
			wantSeverity:   diag.SeverityError,
		},
		{
			name:           "a subdirectory beside the migrations",
			reader:         nestedMigrationEntries([]string{"001_init.up.sql"}, map[string][]string{"archive": {"old.sql", "notes.md"}}),
			wantFilename:   "archive/old.sql",
			wantSuggestion: "move each migration into the migration directory as a file named <version>_<name>.up.sql",
			wantSeverity:   diag.SeverityWarning,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			listed, err := ListMigrationFiles(t.Context(), testCase.reader, "db/migrations")
			require.NoError(t, err)
			diagnostics := MigrationNamingDiagnostics(t.Context(), testCase.reader, "db/migrations", listed)

			require.Len(t, diagnostics, 1)
			require.Equal(t, diag.CodeIgnoredMigrationFile, diagnostics[0].Code)
			require.Equal(t, testCase.wantSeverity, diagnostics[0].Severity)
			require.Equal(t, testCase.wantFilename, diagnostics[0].Filename)
			require.Contains(t, diagnostics[0].Message, "<version>_<name>.up.sql")
			require.Equal(t, testCase.wantSuggestion, diagnostics[0].Suggestion)
		})
	}
}

func TestMigrationNamingDiagnosticsLeaveASubdirectoryWithoutSQLFilesAlone(t *testing.T) {
	t.Parallel()

	reader := nestedMigrationEntries([]string{"001_init.up.sql"}, map[string][]string{"docs": {"README.md"}})
	listed, err := ListMigrationFiles(t.Context(), reader, "db/migrations")
	require.NoError(t, err)

	require.Empty(t, MigrationNamingDiagnostics(t.Context(), reader, "db/migrations", listed))
}

func TestMigrationNamingDiagnosticsLeaveADirectoryWithoutSQLFilesAlone(t *testing.T) {
	t.Parallel()

	reader := migrationEntries("README.md")
	listed, err := ListMigrationFiles(t.Context(), reader, "db/migrations")
	require.NoError(t, err)

	assert.Empty(t, MigrationNamingDiagnostics(t.Context(), reader, "db/migrations", listed))
}

func TestListMigrationFilesReadsNothingUntilAsked(t *testing.T) {
	t.Parallel()

	reader := &fakefs.FileReader{
		Directories: map[string][]os.DirEntry{
			"migrations": {
				&fakefs.DirEntry{EntryName: "10_later.up.sql"},
				&fakefs.DirEntry{EntryName: "9_earlier.down.sql"},
				&fakefs.DirEntry{EntryName: "9_earlier.up.sql"},
				&fakefs.DirEntry{EntryName: "schema.sql"},
			},
		},
		ReadFileErrors: map[string]error{"migrations/10_later.up.sql": errors.New("disk failure")},
		Files:          map[string][]byte{"migrations/9_earlier.up.sql": []byte("CREATE TABLE a;")},
	}

	files, err := ListMigrationFiles(t.Context(), reader, "migrations")
	require.NoError(t, err)
	require.Equal(t, []string{"9_earlier.up.sql", "9_earlier.down.sql", "10_later.up.sql"},
		[]string{files[0].Filename, files[1].Filename, files[2].Filename})
	require.Len(t, files, 3)
	require.Nil(t, files[0].Content)
	require.Empty(t, files[0].Checksum)

	require.NoError(t, ReadMigrationContent(t.Context(), reader, "migrations", &files[0]))
	require.Equal(t, []byte("CREATE TABLE a;"), files[0].Content)
	require.Equal(t, computeChecksum([]byte("CREATE TABLE a;")), files[0].Checksum)

	err = ReadMigrationContent(t.Context(), reader, "migrations", &files[2])
	require.ErrorContains(t, err, "reading migration file migrations/10_later.up.sql")
}

func TestListMigrationFilesLeavesOutFilesNoReaderCanLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mode fs.FileMode
	}{
		{name: "a named pipe", mode: fs.ModeNamedPipe},
		{name: "a socket", mode: fs.ModeSocket},
		{name: "a device", mode: fs.ModeDevice | fs.ModeCharDevice},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			files := fstest.MapFS{
				"migrations/001_init.up.sql": &fstest.MapFile{Data: []byte("CREATE TABLE t (id int);")},
				"migrations/002_pipe.up.sql": &fstest.MapFile{Mode: testCase.mode},
			}
			listed, err := ListMigrationFiles(t.Context(), NewFSReader(files, 0, 0), "migrations")
			require.NoError(t, err)
			require.Len(t, listed, 1)
			assert.Equal(t, "001_init.up.sql", listed[0].Filename)
		})
	}
}

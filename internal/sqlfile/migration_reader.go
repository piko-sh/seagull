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
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
)

const (
	// upMigrationForm is the name form of an up migration, as diagnostics write it.
	upMigrationForm = "<version>_<name>.up.sql"

	// upMigrationSuffix ends the name of every up migration.
	upMigrationSuffix = ".up.sql"

	// downMigrationSuffix ends the name of every down migration.
	downMigrationSuffix = ".down.sql"

	// nestedDownFile is the name a directory-per-migration layout gives a migration's down
	// half, as in 01_init/down.sql.
	nestedDownFile = "down.sql"
)

var (
	// migrationFilePattern matches filenames in the {version}_{name}.{up|down}.sql
	// convention. The version is a numeric prefix, the name is a descriptive segment, and
	// the direction is either "up" or "down".
	migrationFilePattern = regexp.MustCompile(`^(\d+)_(.+)\.(up|down)\.sql$`)
)

// parsedMigrationFilename holds the fields extracted from a
// {version}_{name}.{up|down}.sql filename.
type parsedMigrationFilename struct {
	// name is the descriptive segment between the version prefix and the direction suffix.
	name string

	// version is the numeric migration version parsed from the filename prefix.
	version int64

	// down records whether the file is the down migration.
	down bool
}

// ReadMigrationFilesVersioned reads versioned up and down migration files in execution
// order.
//
// Duplicate versions are retained so the caller can report them through
// DuplicateMigration.
//
// Takes fileReader (FileReader) which provides filesystem access.
// Takes directory (string) which is the path to the migration files.
//
// Returns []MigrationFile which contains the parsed migration files with checksums.
// Returns error when the directory cannot be read or filenames are malformed.
func ReadMigrationFilesVersioned(
	ctx context.Context,
	fileReader FileReader,
	directory string,
) ([]MigrationFile, error) {
	files, err := ListMigrationFiles(ctx, fileReader, directory)
	if err != nil {
		return nil, err
	}
	for index := range files {
		if err := ReadMigrationContent(ctx, fileReader, directory, &files[index]); err != nil {
			return nil, err
		}
	}
	return files, nil
}

// ListMigrationFiles lists the migration files of a directory, as
// ReadMigrationFilesVersioned selects and orders them, without reading any of them, so a
// caller can read and process one file at a time.
//
// Each file's Content and Checksum are left empty; ReadMigrationContent fills them. Named
// pipes, sockets and devices are left out, as directories are.
//
// Takes fileReader (FileReader) which provides filesystem access.
// Takes directory (string) which is the path to the migration files.
//
// Returns []MigrationFile which holds the files in version order, up before down.
// Returns error when the directory cannot be read or a version overflows.
func ListMigrationFiles(
	ctx context.Context,
	fileReader FileReader,
	directory string,
) ([]MigrationFile, error) {
	entries, readError := fileReader.ReadDir(ctx, directory)
	if readError != nil {
		return nil, fmt.Errorf("reading migration directory %s: %w", directory, readError)
	}

	var files []MigrationFile
	for _, entry := range entries {
		if entry.IsDir() || isSpecialFile(entry) {
			continue
		}

		parsed, matched, parseError := parseMigrationFilename(entry.Name())
		if parseError != nil {
			return nil, parseError
		}
		if !matched {
			continue
		}

		files = append(files, MigrationFile{
			Version:  parsed.version,
			Name:     parsed.name,
			Down:     parsed.down,
			Filename: entry.Name(),
			Content:  nil,
			Checksum: "",
		})
	}

	slices.SortStableFunc(files, compareMigrationFiles)

	return files, nil
}

// ReadMigrationContent reads a listed migration file's content and computes its checksum.
//
// Takes fileReader (FileReader) which provides filesystem access.
// Takes directory (string) which is the path to the migration files.
// Takes file (*MigrationFile) which is a file ListMigrationFiles returned; its Content
// and Checksum are set.
//
// Returns error when the file cannot be read.
func ReadMigrationContent(ctx context.Context, fileReader FileReader, directory string, file *MigrationFile) error {
	filePath := path.Join(directory, file.Filename)
	content, err := fileReader.ReadFile(ctx, filePath)
	if err != nil {
		return fmt.Errorf("reading migration file %s: %w", filePath, err)
	}
	file.Content = content
	file.Checksum = computeChecksum(content)
	return nil
}

// DuplicateMigration finds the first two migration files that share a version and a
// direction, such as two 001_*.up.sql files after a branch merge.
//
// Takes files ([]MigrationFile) which are sorted as ReadMigrationFilesVersioned returns
// them.
//
// Returns first (MigrationFile) which is the file seen first for the shared slot.
// Returns second (MigrationFile) which is the conflicting file.
// Returns found (bool) which is false when every version and direction is distinct.
func DuplicateMigration(files []MigrationFile) (first, second MigrationFile, found bool) {
	for index := 1; index < len(files); index++ {
		if compareMigrationFiles(files[index-1], files[index]) == 0 {
			return files[index-1], files[index], true
		}
	}
	return MigrationFile{}, MigrationFile{}, false
}

// MigrationNamingDiagnostics reports the .sql files of a migration directory that the
// migration runner skips, so the generator and the runner skip the same files and the
// generator says so.
//
// While at least one listed file is an up migration, each .sql file outside the
// {version}_{name}.{up|down}.sql convention, and each subdirectory holding .sql files
// (the runner reads none), gets a Q048 warning. Without an up migration, a directory
// containing skipped SQL files or only down migrations gets one Q048 error: none of those
// files can contribute an up migration to schema replay.
//
// Takes fileReader (FileReader) which provides filesystem access.
// Takes directory (string) which is the path to the migration files directory.
// Takes listed ([]MigrationFile) which are the files ListMigrationFiles selected from it.
//
// Returns []diag.SourceError which holds the warnings or the error, or nil when there are
// no naming problems or the directory cannot be read.
func MigrationNamingDiagnostics(
	ctx context.Context,
	fileReader FileReader,
	directory string,
	listed []MigrationFile,
) []diag.SourceError {
	entries, err := fileReader.ReadDir(ctx, directory)
	if err != nil {
		return nil
	}

	ignored, nested := skippedMigrationFiles(ctx, fileReader, directory, entries)
	if slices.ContainsFunc(listed, func(file MigrationFile) bool { return !file.Down }) {
		diagnostics := make([]diag.SourceError, 0, len(ignored)+len(nested))
		for _, name := range ignored {
			diagnostics = append(diagnostics, ignoredMigrationWarning(name))
		}
		for _, file := range nested {
			diagnostics = append(diagnostics, nestedMigrationWarning(file))
		}
		return diagnostics
	}
	switch {
	case len(ignored) > 0:
		return []diag.SourceError{ignoredDirectoryError(directory, ignored)}
	case len(nested) > 0:
		return []diag.SourceError{nestedDirectoryError(directory, nested[0])}
	case len(listed) > 0:
		return []diag.SourceError{downOnlyDirectoryError(directory, listed[0])}
	}
	return nil
}

// compareMigrationFiles orders migration files by version, then up before down.
//
// Takes left (MigrationFile) which is the first file.
// Takes right (MigrationFile) which is the second file.
//
// Returns int which is negative, zero or positive as left sorts before, with or after
// right.
func compareMigrationFiles(left, right MigrationFile) int {
	return cmp.Or(cmp.Compare(left.Version, right.Version), compareDown(left.Down, right.Down))
}

// compareDown orders an up migration before a down migration.
//
// Takes left (bool) which reports whether the first file is a down migration.
// Takes right (bool) which reports whether the second file is a down migration.
//
// Returns int which is negative, zero or positive as left sorts before, with or after
// right.
func compareDown(left, right bool) int {
	switch {
	case left == right:
		return 0
	case right:
		return -1
	default:
		return 1
	}
}

// parseMigrationFilename extracts the version, descriptive name, and direction from a
// migration filename following the {version}_{name}.{up|down}.sql convention.
//
// Takes filename (string) which is the directory entry name.
//
// Returns parsedMigrationFilename which holds the extracted fields (zero value when no
// match).
// Returns bool which is true when the filename matched the convention.
// Returns error when the numeric version prefix overflows int64.
func parseMigrationFilename(filename string) (parsedMigrationFilename, bool, error) {
	matches := migrationFilePattern.FindStringSubmatch(filename)
	if matches == nil {
		return parsedMigrationFilename{}, false, nil
	}

	version, parseError := strconv.ParseInt(matches[1], 10, 64)
	if parseError != nil {
		return parsedMigrationFilename{}, false, fmt.Errorf("parsing version from %s: %w", filename, parseError)
	}

	const directionGroupIndex = 3
	down := matches[directionGroupIndex] == "down"

	return parsedMigrationFilename{name: matches[2], version: version, down: down}, true, nil
}

// computeChecksum returns the SHA-256 hex digest of the given content.
//
// Takes content ([]byte) which holds the raw bytes to hash.
//
// Returns string which is the hex-encoded SHA-256 digest.
func computeChecksum(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

// skippedMigrationFiles lists the .sql files of a migration directory the runner skips:
// those outside the {version}_{name}.{up|down}.sql convention, and, for each subdirectory
// holding .sql files, its first one.
//
// Takes fileReader (FileReader) which provides filesystem access.
// Takes directory (string) which is the migration directory.
// Takes entries ([]os.DirEntry) which are its entries.
//
// Returns ignored ([]string) which holds the skipped files of the directory itself.
// Returns nested ([]string) which holds one file per subdirectory, relative to it.
func skippedMigrationFiles(ctx context.Context, fileReader FileReader, directory string, entries []os.DirEntry) (ignored, nested []string) {
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			if first, found := firstSQLFile(ctx, fileReader, directory, name); found {
				nested = append(nested, first)
			}
			continue
		}
		if !strings.HasSuffix(name, sqlFileSuffix) {
			continue
		}
		if _, matched, _ := parseMigrationFilename(name); !matched {
			ignored = append(ignored, name)
		}
	}
	return ignored, nested
}

// firstSQLFile returns the first .sql file of a subdirectory of the migration directory,
// as a path relative to the migration directory.
//
// Takes fileReader (FileReader) which provides filesystem access.
// Takes directory (string) which is the migration directory.
// Takes subdirectory (string) which is the subdirectory's name.
//
// Returns string which is the file's path, such as 01_init/up.sql; a file other than
// down.sql is preferred, as the one that would have been the up migration.
// Returns bool which is false when the subdirectory holds no .sql file or cannot be read.
func firstSQLFile(ctx context.Context, fileReader FileReader, directory, subdirectory string) (string, bool) {
	entries, err := fileReader.ReadDir(ctx, path.Join(directory, subdirectory))
	if err != nil {
		return "", false
	}
	first := ""
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), sqlFileSuffix) {
			continue
		}
		if entry.Name() != nestedDownFile {
			return path.Join(subdirectory, entry.Name()), true
		}
		first = path.Join(subdirectory, entry.Name())
	}
	return first, first != ""
}

// nestedMigrationWarning reports a .sql file in a subdirectory of the migration
// directory, which the runner does not read.
//
// Takes file (string) which is the file's path relative to the migration directory.
//
// Returns diag.SourceError which is the Q048 warning, at the file's first line.
func nestedMigrationWarning(file string) diag.SourceError {
	return diagbuild.BlockWithSuggestion(file, 1, diag.CodeIgnoredMigrationFile, diag.SeverityWarning,
		fmt.Sprintf("%s is ignored, as is every file in %s: the migration runner reads only the files of the "+
			"migration directory itself, named %s or <version>_<name>.down.sql", file, path.Dir(file), upMigrationForm),
		nestedMigrationSuggestion(file))
}

// nestedDirectoryError reports a migration directory whose migrations all sit in
// subdirectories, as a directory-per-migration layout keeps them, so the runner applies
// nothing.
//
// Takes directory (string) which is the migration directory.
// Takes file (string) which is the first nested .sql file, relative to it.
//
// Returns diag.SourceError which is the Q048 error, at that file.
func nestedDirectoryError(directory, file string) diag.SourceError {
	return directoryError(directory, file, file+" is in a subdirectory, which the runner does not read", nestedMigrationSuggestion(file))
}

// nestedMigrationSuggestion says where a nested migration file belongs: a file named
// down.sql in a directory named like a migration becomes its down migration, any other
// its up migration.
//
// Takes file (string) which is the nested file's path, such as 01_init/up.sql.
//
// Returns string which is the suggestion.
func nestedMigrationSuggestion(file string) string {
	directory, name := path.Split(file)
	version := strings.TrimSuffix(directory, "/")
	target := version + upMigrationSuffix
	if name == nestedDownFile {
		target = version + downMigrationSuffix
	}
	if _, matched, err := parseMigrationFilename(target); !matched || err != nil {
		return "move each migration into the migration directory as a file named " + upMigrationForm
	}
	return "move " + file + " into the migration directory as " + target
}

// ignoredMigrationWarning reports one .sql file the migration runner skips.
//
// Takes name (string) which is the file's name.
//
// Returns diag.SourceError which is the Q048 warning, at the file's first line.
func ignoredMigrationWarning(name string) diag.SourceError {
	return diagbuild.Block(name, 1, diag.CodeIgnoredMigrationFile, diag.SeverityWarning,
		fmt.Sprintf("%s is ignored; migration files must be named %s or <version>_<name>.down.sql", name, upMigrationForm))
}

// ignoredDirectoryError reports a migration directory whose .sql files the migration
// runner skips, every one.
//
// Takes directory (string) which is the migration directory.
// Takes ignored ([]string) which are the skipped files, in directory order.
//
// Returns diag.SourceError which is the Q048 error, at the first skipped file.
func ignoredDirectoryError(directory string, ignored []string) diag.SourceError {
	first := ignored[0]
	skipped := first + " is ignored"
	switch more := len(ignored) - 1; more {
	case 0:
	case 1:
		skipped = first + " and 1 more .sql file are ignored"
	default:
		skipped = fmt.Sprintf("%s and %d more .sql files are ignored", first, more)
	}
	suggestion := "name each migration " + upMigrationForm + ", such as 001_schema.up.sql"
	if renamed, ok := upMigrationName(first); ok {
		suggestion = "rename " + first + " to " + renamed
	}
	return directoryError(directory, first, skipped, suggestion)
}

// downOnlyDirectoryError reports a migration directory that holds down migrations and no
// up migration, so the runner applies nothing.
//
// Takes directory (string) which is the migration directory.
// Takes first (MigrationFile) which is its first down migration.
//
// Returns diag.SourceError which is the Q048 error, at that down migration.
func downOnlyDirectoryError(directory string, first MigrationFile) diag.SourceError {
	upName := strings.TrimSuffix(first.Filename, downMigrationSuffix) + upMigrationSuffix
	return directoryError(directory, first.Filename, first.Filename+" is a down migration", "add the up migration "+upName)
}

// directoryError builds the Q048 error for a migration directory the runner applies
// nothing from.
//
// Takes directory (string) which is the migration directory.
// Takes filename (string) which is the file the error is reported at.
// Takes skipped (string) which says why that file is not applied.
// Takes suggestion (string) which says how to fix it.
//
// Returns diag.SourceError which is the error.
func directoryError(directory, filename, skipped, suggestion string) diag.SourceError {
	return diagbuild.BlockWithSuggestion(filename, 1, diag.CodeIgnoredMigrationFile, diag.SeverityError,
		fmt.Sprintf("no file in the migration directory %q is a migration the runner applies, so the schema would be empty "+
			"and every column typed any: %s; up migrations must be named %s", directory, skipped, upMigrationForm),
		suggestion)
}

// upMigrationName returns the up migration name of a file that has a version and a name
// but no direction, such as 001_schema.up.sql for 001_schema.sql.
//
// Takes filename (string) which is the skipped file's name.
//
// Returns string which is the up migration name.
// Returns bool which is false when the file has no version prefix.
func upMigrationName(filename string) (string, bool) {
	renamed := strings.TrimSuffix(filename, sqlFileSuffix) + upMigrationSuffix
	if _, matched, err := parseMigrationFilename(renamed); !matched || err != nil {
		return "", false
	}
	return renamed, true
}

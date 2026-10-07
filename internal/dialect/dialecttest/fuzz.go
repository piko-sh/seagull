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

package dialecttest

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/fault"
)

const (
	// fuzzMigrationFile is the migration file name a fuzz input is served as.
	fuzzMigrationFile = "001_fuzz.up.sql"

	// fuzzQueryFile is the query file name a fuzz input is served as.
	fuzzQueryFile = "fuzz.sql"

	// fuzzInputDeadline is how long one input may take before its context's deadline passes
	// and the input fails, so the fuzzer records an input that hangs the generator as a
	// failure rather than stalling.
	fuzzInputDeadline = 10 * time.Second

	// fuzzWatchdogDeadline is how long one input may take before the run is crashed: long
	// enough after fuzzInputDeadline that only a loop that never reads its context meets it.
	fuzzWatchdogDeadline = 2 * fuzzInputDeadline

	// fuzzMaxInputBytes bounds the combined size of one fuzz input, so the fuzzer spends its
	// time on shapes rather than on sheer length.
	fuzzMaxInputBytes = 64 << 10
)

// FuzzGenerate fuzzes generation using a migration file and a query file.
//
// Golden cases provide the seed corpus. Each run checks that generation finishes within
// the deadline and produces valid errors and diagnostic locations without panicking. A
// nil newDialect stops the test before any input runs.
//
// Takes f (*testing.F) which is the fuzz test.
// Takes newDialect (func() dialect.DialectPort) which returns a fresh dialect per input.
// Takes directories (...string) which hold golden cases to seed from, such as testdata.
func FuzzGenerate(f *testing.F, newDialect func() dialect.DialectPort, directories ...string) {
	f.Helper()

	if problems := validateNewDialect(newDialect); len(problems) > 0 {
		f.Fatalf("dialecttest: invalid FuzzGenerate arguments:\n%s", strings.Join(problems, "\n"))
	}
	seeds := 0
	for _, directory := range directories {
		seeds += addGoldenSeeds(f, directory)
	}
	if seeds == 0 {
		f.Fatalf("no seeds found in %v", directories)
	}

	f.Fuzz(func(t *testing.T, migration, query string) {
		if len(migration)+len(query) > fuzzMaxInputBytes {
			t.Skip("input larger than the fuzz bound")
		}
		checkGenerateInvariants(t, newDialect(), migration, query)
	})
}

// addGoldenSeeds adds one seed per query file of every golden case under directory.
//
// Takes f (*testing.F) which receives the seeds.
// Takes directory (string) which holds the case directories.
//
// Returns int which is the number of seeds added.
func addGoldenSeeds(f *testing.F, directory string) int {
	f.Helper()

	entries, err := os.ReadDir(directory)
	if err != nil {
		f.Fatalf("reading seed cases: %v", err)
	}
	seeds := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		caseFiles := os.DirFS(filepath.Join(directory, entry.Name()))
		migration := strings.Join(sqlFiles(f, caseFiles, "migrations", isUpMigration), "\n")
		for _, query := range sqlFiles(f, caseFiles, "queries", isSQLFile) {
			f.Add(migration, query)
			seeds++
		}
	}
	return seeds
}

// sqlFiles reads the matching files of one directory, in name order; a missing directory
// holds none.
//
// Takes f (*testing.F) which registers fuzz inputs and reports setup failures.
// Takes files (fs.FS) which is the case directory.
// Takes directory (string) which is the directory within it.
// Takes keep (func(string) bool) which selects files by name.
//
// Returns []string which holds each file's content.
func sqlFiles(f *testing.F, files fs.FS, directory string, keep func(string) bool) []string {
	f.Helper()

	entries, err := fs.ReadDir(files, directory)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		f.Fatalf("reading %s: %v", directory, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && keep(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	slices.Sort(names)
	contents := make([]string, 0, len(names))
	for _, name := range names {
		content, readErr := fs.ReadFile(files, path.Join(directory, name))
		if readErr != nil {
			f.Fatalf("reading %s: %v", name, readErr)
		}
		contents = append(contents, string(content))
	}
	return contents
}

// isUpMigration reports whether a file name is a forward migration.
//
// Takes name (string) which is the file name.
//
// Returns bool which is true for a .sql file that is not a down migration.
func isUpMigration(name string) bool {
	return isSQLFile(name) && !strings.HasSuffix(name, ".down.sql")
}

// isSQLFile reports whether a file name is a SQL file.
//
// Takes name (string) which is the file name.
//
// Returns bool which is true for a .sql file.
func isSQLFile(name string) bool {
	return strings.HasSuffix(name, ".sql")
}

// checkGenerateInvariants generates one fuzz input and checks the invariants every input
// must keep. The input's context passes its deadline after fuzzInputDeadline, which fails
// the input; a watchdog crashes the run after fuzzWatchdogDeadline, for a generator that
// never reads its context.
//
// Takes t (*testing.T) which reports test failures.
// Takes engine (dialect.DialectPort) which is a fresh dialect.
// Takes migration (string) which is the migration file's content.
// Takes query (string) which is the query file's content.
//
// Panics in the watchdog callback when an input exceeds fuzzWatchdogDeadline.
func checkGenerateInvariants(t *testing.T, engine dialect.DialectPort, migration, query string) {
	t.Helper()

	watchdog := time.AfterFunc(fuzzWatchdogDeadline, func() {
		panic(fmt.Sprintf("Generate ran for longer than %s on one input and never stopped at its context's deadline", fuzzWatchdogDeadline))
	})
	defer watchdog.Stop()

	problems, err := generateInvariantProblems(t.Context(), engine, migration, query, fuzzInputDeadline)
	if err != nil {
		t.Fatalf("%v", err)
	}
	for _, problem := range problems {
		t.Error(problem)
	}
}

// generateInvariantProblems generates one fuzz input within a deadline and describes each
// invariant its diagnostics break.
//
// Takes engine (dialect.DialectPort) which is a fresh dialect.
// Takes migration (string) which is the migration file's content.
// Takes query (string) which is the query file's content.
// Takes deadline (time.Duration) which is how long the input may take.
//
// Returns []string which describes each diagnostic placed before the first line.
// Returns error which wraps errFuzzInputDeadline when the input ran past its deadline, or
// errUnexpectedGenerateError when generation failed with neither a limit nor malformed
// SQL.
func generateInvariantProblems(ctx context.Context, engine dialect.DialectPort, migration, query string, deadline time.Duration) ([]string, error) {
	ctx, cancel := context.WithTimeoutCause(ctx, deadline, errFuzzInputDeadline)
	defer cancel()

	generator, err := app.New(engine)
	if err != nil {
		return nil, fmt.Errorf("creating the generator: %w", err)
	}
	result, err := generator.Generate(ctx, app.Input{
		Package:     packageName,
		PackagePath: "",
		Migrations: fstest.MapFS{
			path.Join(migrationDirectory, fuzzMigrationFile): &fstest.MapFile{Data: []byte(migration)},
			path.Join(queryDirectory, fuzzQueryFile):         &fstest.MapFile{Data: []byte(query)},
		},
		Queries:      nil,
		MigrationDir: migrationDirectory,
		QueryDir:     queryDirectory,
	})
	if cause := context.Cause(ctx); errors.Is(cause, errFuzzInputDeadline) {
		return nil, fmt.Errorf("generation ran for longer than %s on one input: %w", deadline, cause)
	}
	if err != nil {
		if !errors.Is(err, fault.ErrLimit) && !errors.Is(err, fault.ErrMalformedSQL) {
			return nil, fmt.Errorf("%w, got: %w", errUnexpectedGenerateError, err)
		}
		return nil, nil
	}
	var problems []string
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Line < 1 {
			problems = append(problems, fmt.Sprintf("every diagnostic must sit on a line numbered from 1, got %s at line %d: %s",
				diagnostic.Code, diagnostic.Line, diagnostic.Message))
		}
	}
	return problems, nil
}

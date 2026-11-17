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
	"cmp"
	"context"
	"crypto/sha256"
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
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/sqlmodel"
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

	// fuzzWatchdogDeadline is how long one input may take before the run is crashed. It is
	// long enough after fuzzInputDeadline that only a loop that never reads its context
	// meets it.
	fuzzWatchdogDeadline = 2 * fuzzInputDeadline

	// fuzzMaxInputBytes bounds the combined size of one fuzz input, so the fuzzer spends its
	// time on shapes rather than on sheer length.
	fuzzMaxInputBytes = 64 << 10
)

// FuzzGenerate fuzzes generation using a migration file and a query file.
//
// Golden cases provide the seed corpus, with the seeds admitted from external test suites
// under the module's testdata/external for the dialect, as addExternalSeeds reads them.
// Each run checks that generation finishes within the deadline and produces valid errors
// and diagnostic locations without panicking. A nil newDialect stops the test before any
// input runs.
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
	info := newDialect().Info()
	addBindingSeeds(f, &info)
	addExternalSeeds(f, info.Name)
	f.Add("CREATE TABLE items (d BIGINT);", "-- sgl.query(InsertLiteral, batch)\nINSERT INTO items (d) VALUES (1);")
	f.Add("CREATE TABLE items (d BIGINT);", "-- sgl.query(CopyLiteral, copyfrom)\nINSERT INTO items (d) VALUES (1);")

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

// sqlFiles reads the matching files of one directory, in name order. A missing directory
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

// checkGenerateInvariants checks deadlines, diagnostics and query bindings.
//
// The context expires after fuzzInputDeadline. If generation ignores cancellation, a
// watchdog crashes the worker and identifies the input by its test name and source
// hashes.
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
		panic(fmt.Sprintf("Generate ignored its deadline for %s on %s, migration SHA-256 %x, query SHA-256 %x",
			fuzzWatchdogDeadline, t.Name(), sha256.Sum256([]byte(migration)), sha256.Sum256([]byte(query))))
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
// Returns []string which describes invalid diagnostics, panics and broken bindings.
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
	details, err := generator.GenerateDetailed(ctx, app.Input{
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
	problems := diagnosticLocationProblems(details.Result.Diagnostics, map[string]string{
		path.Join(migrationDirectory, fuzzMigrationFile): migration,
		path.Join(queryDirectory, fuzzQueryFile):         query,
	})
	problems = append(problems, invariantDiagnosticProblems(details.Result.Diagnostics)...)
	info := engine.Info()
	return append(problems, passingTemplateProblems(&info, "fuzz", details.Report)...), nil
}

// invariantDiagnosticProblems describes recovered panics and unlocated diagnostics.
//
// Takes diagnostics ([]diag.SourceError) which came from generation.
//
// Returns []string which describes each broken invariant.
func invariantDiagnosticProblems(diagnostics []diag.SourceError) []string {
	var problems []string
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == diag.CodeRecoveredPanic {
			problems = append(problems, "generation recovered a panic: "+diagnostic.Message)
		}
		if diagnostic.Line < 1 {
			problems = append(problems, fmt.Sprintf("every diagnostic must sit on a line numbered from 1, got %s at line %d: %s",
				diagnostic.Code, diagnostic.Line, diagnostic.Message))
		}
	}
	return problems
}

// passingTemplateProblems checks the template of every query without an error.
//
// Takes info (*dialect.Info) which describes the dialect.
// Takes caseName (string) which names the input in messages.
// Takes report (*app.Report) which holds the queries and diagnostics.
//
// Returns []string which describes each template problem.
func passingTemplateProblems(info *dialect.Info, caseName string, report *app.Report) []string {
	failed := failedFuzzQueries(report)
	var problems []string
	for _, analysed := range report.Queries {
		if len(analysed.Template.Segments) > 0 && !failed[analysed] {
			problems = append(problems, templateProblems(info, caseName, analysed)...)
		}
	}
	return problems
}

// failedFuzzQueries finds the queries that hold an error diagnostic.
//
// A query owns the lines from its own start up to the next query in the same file. The
// starts are sorted once per file, so each diagnostic costs one binary search.
//
// Takes report (*app.Report) which holds the queries and diagnostics.
//
// Returns map[*sqlmodel.AnalysedQuery]bool which holds every failed query.
func failedFuzzQueries(report *app.Report) map[*sqlmodel.AnalysedQuery]bool {
	byFile := make(map[string][]*sqlmodel.AnalysedQuery)
	for _, query := range report.Queries {
		byFile[query.Filename] = append(byFile[query.Filename], query)
	}
	for _, queries := range byFile {
		slices.SortStableFunc(queries, func(left, right *sqlmodel.AnalysedQuery) int { return cmp.Compare(left.Line, right.Line) })
	}
	failed := make(map[*sqlmodel.AnalysedQuery]bool)
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Severity != diag.SeverityError {
			continue
		}
		queries := byFile[diagnostic.Filename]
		following, _ := slices.BinarySearchFunc(queries, diagnostic.Line, func(query *sqlmodel.AnalysedQuery, line int) int {
			return cmp.Compare(query.Line, line+1)
		})
		if following == 0 {
			continue
		}
		owner := queries[following-1]
		for _, query := range queries[:following] {
			if query.Line == owner.Line {
				failed[query] = true
			}
		}
	}
	return failed
}

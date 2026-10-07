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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"slices"
	"strings"
	"testing"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/goldenjson"
)

const (
	// specFile names the file that marks a directory as a golden case and describes it.
	specFile = "testspec.json"

	// goldenDirectory names the directory holding a case's golden files.
	goldenDirectory = "golden"

	// goldenPackage is the package name golden cases generate into.
	goldenPackage = "test"

	// goldenFileMode is the permission of re-recorded golden files.
	goldenFileMode fs.FileMode = 0o644

	// goldenDirectoryMode is the permission of re-recorded golden directories.
	goldenDirectoryMode fs.FileMode = 0o755

	// maxSpecBytes bounds the size of a testspec.json the kit reads.
	maxSpecBytes = 1 << 20

	// maxGoldenBytes bounds the size of a golden file the kit reads.
	maxGoldenBytes = 64 << 20
)

// GoldenOption configures RunGolden.
type GoldenOption func(*goldenSettings)

// goldenSettings holds the options of one RunGolden call.
type goldenSettings struct {
	// update re-records the golden files instead of comparing them.
	update bool
}

// goldenSpec is the testspec.json of one golden case.
type goldenSpec struct {
	// ExpectedDiagnosticCount, when set, is the exact number of diagnostics expected.
	ExpectedDiagnosticCount *int `json:"expectedDiagnosticCount"`

	// Description says what the case covers.
	Description string `json:"description"`

	// Diagnostics lists diagnostics that must appear among the run's.
	Diagnostics []goldenDiagnostic `json:"diagnostics"`

	// CustomFunctions declares functions the dialect does not know.
	CustomFunctions []sqlmodel.CustomFunctionConfig `json:"customFunctions"`

	// ShouldError expects Generate to fail fatally.
	ShouldError bool `json:"shouldError"`
}

// goldenDiagnostic describes a diagnostic a golden case expects. Empty fields match any
// value.
type goldenDiagnostic struct {
	// OnLine, when set, is the expected line.
	OnLine *int `json:"onLine"`

	// Severity is error, warning or hint.
	Severity string `json:"severity"`

	// Code is the diagnostic code, such as Q001.
	Code string `json:"code"`

	// MessageContains is a substring of the expected message.
	MessageContains string `json:"messageContains"`

	// Filename is a suffix of the expected file name.
	Filename string `json:"filename"`
}

// goldenReport is what one golden case found: the problems reported as failures and the
// notes logged.
type goldenReport struct {
	// problems describes each way the run differs from the case's expectations.
	problems []string

	// notes describes each golden file re-recorded or removed.
	notes []string
}

// goldenFile is one golden file of a case and what the run produced for it.
type goldenFile struct {
	// value is what the run produced, compared through its curated projection.
	value any

	// path is the golden file.
	path string

	// produced is true when the run produced the value.
	produced bool
}

// WithUpdate re-records the golden files from the current output instead of comparing
// them, and removes a golden file the run no longer produces. Wire it to a test flag,
// such as flag.Bool("update", false, "update golden files"), and review the resulting
// diff.
//
// Takes update (bool) which re-records when true.
//
// Returns GoldenOption which sets the mode.
func WithUpdate(update bool) GoldenOption {
	return func(settings *goldenSettings) {
		settings.update = update
	}
}

// RunGolden compares each case's catalogue, queries and diagnostics with its golden
// files.
//
// Cases are subdirectories containing testspec.json, migrations and queries. A
// testspec.json holding a key the kit does not know fails its case, so a misspelt key
// cannot turn an expectation off. WithUpdate re-records the curated JSON output and
// removes stale goldens. A nil newDialect stops the test before any case runs, and a case
// whose dialect panics fails alone, its stack logged.
//
// Takes t (*testing.T) which reports test failures.
// Takes newDialect (func() dialect.DialectPort) which returns a fresh dialect for each
// case.
// Takes directory (string) which holds the case directories, such as testdata.
// Takes options (...GoldenOption) which configure the run, such as WithUpdate.
func RunGolden(t *testing.T, newDialect func() dialect.DialectPort, directory string, options ...GoldenOption) {
	t.Helper()

	if problems := validateNewDialect(newDialect); len(problems) > 0 {
		t.Fatalf("dialecttest: invalid RunGolden arguments:\n%s", strings.Join(problems, "\n"))
	}
	settings := goldenSettings{update: false}
	for _, option := range options {
		option(&settings)
	}

	cases, err := goldenCases(directory)
	if err != nil {
		t.Fatalf("dialecttest: %v", err)
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			report := runGoldenCaseRecovered(t.Context(), t.Logf, newDialect, filepath.Join(directory, name), settings)
			for _, note := range report.notes {
				t.Log(note)
			}
			for _, problem := range report.problems {
				t.Error(problem)
			}
		})
	}
}

// validateNewDialect reports a missing dialect constructor.
//
// Takes newDialect (func() dialect.DialectPort) which is the constructor to validate.
//
// Returns []string which describes the problem, or is empty.
func validateNewDialect(newDialect func() dialect.DialectPort) []string {
	if newDialect == nil {
		return []string{"newDialect is nil"}
	}
	return nil
}

// goldenCases lists the case directories under directory, sorted.
//
// Takes directory (string) which holds the case directories.
//
// Returns []string which holds the names of the subdirectories with a testspec.json.
// Returns error when the directory cannot be read or holds no case.
func goldenCases(directory string) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("reading golden cases: %w", err)
	}
	var cases []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, statErr := os.Stat(filepath.Join(directory, entry.Name(), specFile)); statErr != nil {
			continue
		}
		cases = append(cases, entry.Name())
	}
	slices.Sort(cases)
	if len(cases) == 0 {
		return nil, fmt.Errorf("%w in %s", errNoGoldenCases, directory)
	}
	return cases, nil
}

// runGoldenCaseRecovered runs one golden case with a fresh dialect, turning a panic in
// the dialect or the generator into a problem, so one broken case fails alone. The
// panic's stack is logged once and kept out of the problem.
//
// Takes logf (func(string, ...any)) which logs the stack, such as t.Logf.
// Takes newDialect (func() dialect.DialectPort) which returns a fresh dialect.
// Takes caseDirectory (string) which is the case directory.
// Takes settings (goldenSettings) which selects comparing or re-recording.
//
// Returns report (goldenReport) which holds the case's problems and notes.
func runGoldenCaseRecovered(
	ctx context.Context,
	logf func(format string, arguments ...any),
	newDialect func() dialect.DialectPort,
	caseDirectory string,
	settings goldenSettings,
) (report goldenReport) {
	defer func() {
		if recovered := recover(); recovered != nil {
			logf("%s panicked: %v\n%s", caseDirectory, recovered, debug.Stack())
			report = goldenReport{problems: []string{fmt.Sprintf("case panicked: %v", recovered)}, notes: nil}
		}
	}()
	return runGoldenCase(ctx, newDialect(), caseDirectory, settings)
}

// runGoldenCase generates one case and compares the output with its golden files, or
// re-records them.
//
// Takes engine (dialect.DialectPort) which is a fresh dialect.
// Takes caseDirectory (string) which is the case directory.
// Takes settings (goldenSettings) which selects comparing or re-recording.
//
// Returns goldenReport which holds the case's problems and notes.
func runGoldenCase(ctx context.Context, engine dialect.DialectPort, caseDirectory string, settings goldenSettings) goldenReport {
	specification, err := loadGoldenSpec(caseDirectory)
	if err != nil {
		return goldenProblem(err.Error())
	}
	emitter := &recordingEmitter{}
	generator, err := app.New(engine, app.WithEmitter(emitter), app.WithCustomFunctions(specification.CustomFunctions...))
	if err != nil {
		return goldenProblem(fmt.Sprintf("creating the generator: %v", err))
	}

	result, err := generator.Generate(ctx, app.Input{
		Package:      goldenPackage,
		PackagePath:  "",
		Migrations:   os.DirFS(caseDirectory),
		Queries:      nil,
		MigrationDir: "migrations",
		QueryDir:     "queries",
	})
	if specification.ShouldError {
		if err == nil {
			return goldenProblem("expected Generate to return an error")
		}
		return goldenReport{problems: nil, notes: nil}
	}
	if err != nil {
		return goldenProblem(fmt.Sprintf("Generate failed: %v", err))
	}

	goldens := filepath.Join(caseDirectory, goldenDirectory)
	var report goldenReport
	for _, file := range []goldenFile{
		{path: filepath.Join(goldens, "catalogue.json"), value: emitter.catalogue, produced: emitter.catalogue != nil},
		{path: filepath.Join(goldens, "queries.json"), value: emitter.queries, produced: len(emitter.queries) > 0},
		{path: filepath.Join(goldens, "diagnostics.json"), value: result.Diagnostics, produced: len(result.Diagnostics) > 0},
	} {
		settled := settleGoldenFile(file, settings)
		report.problems = append(report.problems, settled.problems...)
		report.notes = append(report.notes, settled.notes...)
	}
	report.problems = append(report.problems, checkExpectedDiagnostics(specification, result.Diagnostics)...)
	return report
}

// goldenProblem returns a report of one problem.
//
// Takes problem (string) which describes it.
//
// Returns goldenReport which holds the problem.
func goldenProblem(problem string) goldenReport {
	return goldenReport{problems: []string{problem}, notes: nil}
}

// settleGoldenFile compares a value with its golden file when the run produced it, and
// otherwise rejects a golden file left over from an earlier run, so a golden file that
// exists is always compared. When re-recording it writes or removes the file instead.
//
// Takes file (goldenFile) which is the golden file and what the run produced for it.
// Takes settings (goldenSettings) which selects comparing or re-recording.
//
// Returns goldenReport which holds the problems found or the files changed.
func settleGoldenFile(file goldenFile, settings goldenSettings) goldenReport {
	switch {
	case !settings.update && file.produced:
		return goldenReport{problems: compareGolden(file.path, file.value), notes: nil}
	case !settings.update:
		return goldenReport{problems: rejectStaleGolden(file.path), notes: nil}
	case file.produced:
		if err := recordGolden(file.path, file.value); err != nil {
			return goldenProblem(err.Error())
		}
		return goldenReport{problems: nil, notes: []string{"updated golden file: " + file.path}}
	default:
		removed, err := removeStaleGolden(file.path)
		if err != nil {
			return goldenProblem(err.Error())
		}
		if removed {
			return goldenReport{problems: nil, notes: []string{"removed stale golden file: " + file.path}}
		}
		return goldenReport{problems: nil, notes: nil}
	}
}

// loadGoldenSpec reads a case's testspec.json, refusing a key the kit does not know and
// anything after the spec's one JSON object.
//
// Takes caseDirectory (string) which is the case directory.
//
// Returns goldenSpec which describes the case.
// Returns error when the spec cannot be read or decoded.
func loadGoldenSpec(caseDirectory string) (goldenSpec, error) {
	content, err := readBounded(os.DirFS(caseDirectory), specFile, maxSpecBytes)
	if err != nil {
		return goldenSpec{}, fmt.Errorf("reading %s: %w", specFile, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var specification goldenSpec
	if err := decoder.Decode(&specification); err != nil {
		return goldenSpec{}, fmt.Errorf("parsing %s: %w", specFile, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return goldenSpec{}, fmt.Errorf("parsing %s: %w", specFile, errSpecTrailingContent)
	}
	return specification, nil
}

// readBounded reads a file of at most limit bytes.
//
// Takes files (fs.FS) which holds the file.
// Takes name (string) which names the file within files.
// Takes limit (int) which is the most bytes the file may hold.
//
// Returns []byte which is the file's content.
// Returns error when the file cannot be read or holds more than limit bytes.
func readBounded(files fs.FS, name string, limit int) ([]byte, error) {
	file, err := files.Open(name)
	if err != nil {
		return nil, err
	}
	content, readErr := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, err
	}
	if len(content) > limit {
		return nil, fmt.Errorf("%w: %s holds more than %d bytes", errFileTooLarge, name, limit)
	}
	return content, nil
}

// encodeGolden encodes the curated projection of a value as indented JSON ending in a
// newline, as a golden file holds it.
//
// Takes value (any) which is the value to encode.
//
// Returns []byte which is the encoded value.
// Returns error when the value cannot be encoded.
func encodeGolden(value any) ([]byte, error) {
	encoded, err := json.MarshalIndent(goldenjson.Project(value), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

// compareGolden compares the curated projection of a value with its golden file.
//
// Takes goldenPath (string) which is the golden file.
// Takes value (any) which is the value to compare.
//
// Returns []string which describes a missing or unreadable golden file or a mismatch.
func compareGolden(goldenPath string, value any) []string {
	actual, err := encodeGolden(value)
	if err != nil {
		return []string{fmt.Sprintf("encoding %s: %v", goldenPath, err)}
	}
	expected, err := readBounded(os.DirFS(filepath.Dir(goldenPath)), filepath.Base(goldenPath), maxGoldenBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return []string{fmt.Sprintf("golden file not found at %s (re-record with WithUpdate)", goldenPath)}
	}
	if err != nil {
		return []string{fmt.Sprintf("reading %s: %v", goldenPath, err)}
	}
	if !jsonEqual(expected, actual) {
		return []string{fmt.Sprintf("golden file mismatch: %s\nexpected:\n%s\nactual:\n%s", goldenPath, expected, actual)}
	}
	return nil
}

// recordGolden writes the curated projection of a value to its golden file, creating its
// directory.
//
// Takes goldenPath (string) which is the golden file.
// Takes value (any) which is the value to record.
//
// Returns error when the value cannot be encoded or the file written.
func recordGolden(goldenPath string, value any) error {
	actual, err := encodeGolden(value)
	if err != nil {
		return fmt.Errorf("encoding %s: %w", goldenPath, err)
	}
	if err := os.MkdirAll(filepath.Dir(goldenPath), goldenDirectoryMode); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(goldenPath), err)
	}
	if err := os.WriteFile(goldenPath, actual, goldenFileMode); err != nil {
		return fmt.Errorf("writing %s: %w", goldenPath, err)
	}
	return nil
}

// rejectStaleGolden reports a golden file that exists although the run produced nothing
// for it.
//
// Takes goldenPath (string) which is the golden file.
//
// Returns []string which describes the stale file, or is empty.
func rejectStaleGolden(goldenPath string) []string {
	_, err := os.Stat(goldenPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return []string{fmt.Sprintf("checking %s: %v", goldenPath, err)}
	}
	return []string{fmt.Sprintf("%s exists but the run produced nothing for it; delete the golden or restore the output", goldenPath)}
}

// removeStaleGolden removes a golden file the run produced nothing for.
//
// Takes goldenPath (string) which is the golden file.
//
// Returns bool which is true when a file was removed.
// Returns error when the file exists but cannot be removed.
func removeStaleGolden(goldenPath string) (bool, error) {
	err := os.Remove(goldenPath)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("removing stale %s: %w", goldenPath, err)
	}
	return true, nil
}

// jsonEqual reports whether two JSON documents hold the same value.
//
// Takes expected ([]byte) which is the first document.
// Takes actual ([]byte) which is the second document.
//
// Returns bool which is true when both decode to equal values.
func jsonEqual(expected, actual []byte) bool {
	if bytes.Equal(expected, actual) {
		return true
	}
	var expectedValue, actualValue any
	if json.Unmarshal(expected, &expectedValue) != nil || json.Unmarshal(actual, &actualValue) != nil {
		return false
	}
	return reflect.DeepEqual(expectedValue, actualValue)
}

// checkExpectedDiagnostics checks the spec's expected diagnostic count and that every
// listed diagnostic appears.
//
// Takes specification (goldenSpec) which lists the expectations.
// Takes diagnostics ([]diag.SourceError) which are the run's diagnostics.
//
// Returns []string which describes each expectation the diagnostics miss.
func checkExpectedDiagnostics(specification goldenSpec, diagnostics []diag.SourceError) []string {
	var problems []string
	if specification.ExpectedDiagnosticCount != nil && *specification.ExpectedDiagnosticCount != len(diagnostics) {
		problems = append(problems, fmt.Sprintf("unexpected diagnostic count: expected %d, got %d",
			*specification.ExpectedDiagnosticCount, len(diagnostics)))
	}
	for _, expected := range specification.Diagnostics {
		if !slices.ContainsFunc(diagnostics, func(actual diag.SourceError) bool { return matchesDiagnostic(actual, expected) }) {
			problems = append(problems, fmt.Sprintf("expected diagnostic not found: severity=%s code=%s message containing %q",
				expected.Severity, expected.Code, expected.MessageContains))
		}
	}
	return problems
}

// matchesDiagnostic reports whether a diagnostic matches every set field of an
// expectation.
//
// Takes actual (diag.SourceError) which is a reported diagnostic.
// Takes expected (goldenDiagnostic) which is the expectation.
//
// Returns bool which is true when every set field matches.
func matchesDiagnostic(actual diag.SourceError, expected goldenDiagnostic) bool {
	if expected.Code != "" && actual.Code != expected.Code {
		return false
	}
	if expected.Severity != "" && !matchesSeverity(actual.Severity, expected.Severity) {
		return false
	}
	if expected.MessageContains != "" && !strings.Contains(actual.Message, expected.MessageContains) {
		return false
	}
	if expected.Filename != "" && !strings.HasSuffix(actual.Filename, expected.Filename) {
		return false
	}
	return expected.OnLine == nil || actual.Line == *expected.OnLine
}

// matchesSeverity reports whether a severity matches its name.
//
// Takes actual (diag.ErrorSeverity) which is the reported severity.
// Takes expected (string) which is error, warning or hint, in any case.
//
// Returns bool which is true when they match.
func matchesSeverity(actual diag.ErrorSeverity, expected string) bool {
	return strings.EqualFold(actual.String(), expected)
}

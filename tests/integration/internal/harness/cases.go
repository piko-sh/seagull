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

package harness

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/emit"
)

const (
	// defaultTestdata is the directory a suite's cases live in when it names none.
	defaultTestdata = "testdata"

	// specFile is the file that marks a directory as a case and describes it.
	specFile = "testspec.json"

	// databaseSQLEmitter is the emitter name a spec may give for the default emitter.
	databaseSQLEmitter = "database/sql"

	// generatedPackage is the package a case's code is generated into.
	generatedPackage = "db"

	// migrationDirectory is the directory of a case that holds its migrations.
	migrationDirectory = "migrations"

	// queryDirectory is the directory of a case that holds its queries.
	queryDirectory = "queries"

	// skipReasonDefault is the reason a skipped case gives when its spec gives none.
	skipReasonDefault = "the case is marked skip in its testspec.json"

	// caseTimeout bounds one case's preparation, generation, build, run and check, so a hung
	// lock, server or runner fails that case by name instead of stalling the suite.
	caseTimeout = 10 * time.Minute

	// testDeadlineMargin is how long before go test's own deadline a case is stopped, so the
	// case says why it stopped and its clean-ups run before go test panics.
	testDeadlineMargin = time.Minute

	// deadlineMarginShare caps the margin at this fraction of the time left before go test's
	// deadline, so a short -timeout still leaves the cases most of it.
	deadlineMarginShare = 10
)

// Suite describes one engine's suite of cases.
type Suite struct {
	// Dialect returns the dialect each case is generated with.
	Dialect func() dialect.Dialect

	// Prepare creates the case's database and applies its migrations under the case's
	// context, and returns the variables the runner reads, such as DATABASE_URL. Nil runs
	// the runner with the inherited environment only, for runners that open an in-memory
	// database.
	Prepare func(ctx context.Context, t *testing.T, caseDirectory string) []string

	// Normalise rewrites, in place, the values of a decoded output that a run cannot
	// reproduce, such as generated identifiers. Both the runner's output and the golden pass
	// through it before they are compared, with each number decoded as a json.Number; nil
	// compares them as they are.
	Normalise func(value any)

	// Emitters maps each emitter name a testspec.json may give, other than the database/sql
	// emitter every case uses by default, to the option selecting it.
	Emitters map[string]seagull.Option

	// Testdata is the directory holding the cases, relative to the suite's module; empty
	// selects testdata.
	Testdata string

	// Driver is the database/sql driver the runner links and opens.
	Driver Driver

	// BuildEnvironment holds variables the runner is built with besides the inherited
	// environment, such as CGO_ENABLED=1.
	BuildEnvironment []string

	// Update records each runner's output as its case's golden instead of comparing.
	Update bool
}

// Driver names the database/sql driver a runner links.
type Driver struct {
	// Module is the driver's module. The runner requires the version the suite's own go.mod
	// requires, so dependency tooling updates both together.
	Module string

	// Import is the package the runner imports for its side effect of registering the
	// driver.
	Import string

	// Name is the name the driver registers, which the runner reads as databaseDriver.
	Name string
}

// Spec is a case's testspec.json.
type Spec struct {
	// ExpectedDiagnostics, when present, lists the codes of every diagnostic generation must
	// report, in any order; an empty list pins that it reports none.
	ExpectedDiagnostics *[]string `json:"expectedDiagnostics,omitempty"`

	// TypeSlots, when present, are the type slots of the preset the case generates with.
	TypeSlots *seagull.TypeSlots `json:"typeSlots,omitempty"`

	// Description says what the case shows.
	Description string `json:"description"`

	// Emitter names the emitter, or is empty for database/sql.
	Emitter string `json:"emitter,omitempty"`

	// SkipReason says why a skipped case is skipped.
	SkipReason string `json:"skipReason,omitempty"`

	// CustomFunctions declares the functions the case's queries call that the dialect does
	// not know.
	CustomFunctions []seagull.CustomFunction `json:"customFunctions,omitempty"`

	// TypeOverrides maps SQL types or columns to Go types for the case.
	TypeOverrides []seagull.TypeOverride `json:"typeOverrides,omitempty"`

	// Skip leaves the case out of the run.
	Skip bool `json:"skip,omitempty"`

	// KeepStars generates with a preset that writes SELECT * and RETURNING * as the queries
	// write them, as the go.keep_stars setting does.
	KeepStars bool `json:"keepStars,omitempty"`

	// ReuseModels generates with a preset that reads the rows of a query whose rows are a
	// table's model into the model, as the go.reuse_models setting does.
	ReuseModels bool `json:"reuseModels,omitempty"`
}

// Run runs every case of a suite as a subtest named after its directory, in name order.
// Each case is generated, built, run and compared with its golden; a failure in one case
// does not stop the others.
//
// Takes suite (*Suite) which describes the engine.
func Run(t *testing.T, suite *Suite) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	testdata := suite.Testdata
	if testdata == "" {
		testdata = defaultTestdata
	}
	for _, name := range caseNames(t, testdata) {
		t.Run(name, func(t *testing.T) {
			runCase(t, suite, filepath.Join(testdata, name))
		})
	}
}

// LoadSpec reads a case's testspec.json, failing the test on a key the spec does not
// define, so a misspelt key cannot quietly switch a check off.
//
// Takes caseDirectory (string) which is the case's directory.
//
// Returns Spec which is the decoded spec.
func LoadSpec(t *testing.T, caseDirectory string) Spec {
	t.Helper()

	content, err := fs.ReadFile(os.DirFS(caseDirectory), specFile)
	require.NoError(t, err, "reading %s", specFile)
	spec, err := decodeSpec(content)
	require.NoError(t, err, "parsing %s in %s", specFile, caseDirectory)
	return spec
}

// DiagnosticProblems returns what is wrong with a generation's diagnostics: each
// diagnostic of error severity, which stops the generation, and, when the spec lists the
// diagnostics it expects, a mismatch between those codes and the codes reported.
//
// Takes expected (*[]string) which holds the expected codes, or is nil to check none.
// Takes diagnostics ([]seagull.SourceError) which are the reported diagnostics.
//
// Returns []string which describes each problem, or is empty.
func DiagnosticProblems(expected *[]string, diagnostics []seagull.SourceError) []string {
	var problems []string
	codes := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		codes = append(codes, string(diagnostic.Code))
		if diagnostic.Severity == seagull.SeverityError {
			problems = append(problems, fmt.Sprintf("generation reported error %s: %s", diagnostic.Code, diagnostic.Message))
		}
	}
	if expected == nil {
		return problems
	}
	want := slices.Clone(*expected)
	got := slices.Clone(codes)
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(want, got) {
		problems = append(problems, fmt.Sprintf("generation reported diagnostics %v, the spec expects %v", got, want))
	}
	return problems
}

// caseNames returns the names of the case directories under testdata, sorted: every
// directory holding a testspec.json.
//
// Takes testdata (string) which is the directory of the cases.
//
// Returns []string which holds the names.
func caseNames(t *testing.T, testdata string) []string {
	t.Helper()

	entries, err := os.ReadDir(testdata)
	require.NoError(t, err, "reading the cases under %s", testdata)
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, statErr := fs.Stat(os.DirFS(testdata), filepath.ToSlash(filepath.Join(entry.Name(), specFile))); statErr != nil {
			continue
		}
		names = append(names, entry.Name())
	}
	slices.Sort(names)
	return names
}

// runCase generates, builds, runs and checks one case, all under one context that ends
// after caseTimeout or as go test's deadline draws near. When it ends early the case
// fails with the reason.
//
// Takes suite (*Suite) which describes the engine.
// Takes caseDirectory (string) which is the case's directory.
func runCase(t *testing.T, suite *Suite, caseDirectory string) {
	t.Helper()

	spec := LoadSpec(t, caseDirectory)
	t.Log(spec.Description)
	if spec.Skip {
		t.Skip(cmp.Or(spec.SkipReason, skipReasonDefault))
	}

	ctx, finish := caseContext(t)
	defer finish()

	var environment []string
	if suite.Prepare != nil {
		environment = suite.Prepare(ctx, t, caseDirectory)
	}
	files := generate(ctx, t, suite, caseDirectory, &spec)
	binary := buildRunner(ctx, t, suite, caseDirectory, files)
	output := executeRunner(ctx, t, binary, environment)
	assertGolden(t, filepath.Join(caseDirectory, "golden", "output.json"), output, suite)
}

// caseContext returns the context a case runs under: the test's own, ended with
// errCaseTimeout once caseTimeout has passed and with errTestDeadline a margin before go
// test's deadline, and the function that ends it. That function, deferred, fails the case
// with the cause when the context ended before the case did.
//
// Returns context.Context which bounds the case.
// Returns func() which reports an early end and releases the context.
func caseContext(t *testing.T) (context.Context, func()) {
	t.Helper()

	ctx, cancelTimeout := context.WithTimeoutCause(t.Context(), caseTimeout, errCaseTimeout)
	cancelDeadline := context.CancelFunc(func() {})
	if deadline, hasDeadline := t.Deadline(); hasDeadline {
		ctx, cancelDeadline = context.WithDeadlineCause(ctx, stopBefore(deadline, time.Now()), errTestDeadline)
	}
	return ctx, func() {
		if ctx.Err() != nil {
			assert.Failf(t, "the case stopped early", "%v", context.Cause(ctx))
		}
		cancelDeadline()
		cancelTimeout()
	}
}

// stopBefore returns when a case stops ahead of go test's deadline: testDeadlineMargin
// before it, or the time left divided by deadlineMarginShare when that is less, and no
// margin once the deadline has passed.
//
// Takes deadline (time.Time) which is go test's deadline.
// Takes now (time.Time) which is the current time.
//
// Returns time.Time which is when the case stops.
func stopBefore(deadline, now time.Time) time.Time {
	margin := max(0, min(testDeadlineMargin, deadline.Sub(now)/deadlineMarginShare))
	return deadline.Add(-margin)
}

// decodeSpec decodes a testspec.json, refusing a key Spec does not define and anything
// after the one object.
//
// Takes content ([]byte) which is the file's content.
//
// Returns Spec which is the decoded spec.
// Returns error when the content is not one valid spec.
func decodeSpec(content []byte) (Spec, error) {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var spec Spec
	if err := decodeOnly(decoder, &spec); err != nil {
		return Spec{}, err
	}
	return spec, nil
}

// generate generates a case's package and checks its diagnostics against the spec.
//
// Takes suite (*Suite) which describes the engine.
// Takes caseDirectory (string) which is the case's directory.
// Takes spec (*Spec) which is the case's spec.
//
// Returns []emit.GeneratedFile which holds the generated files.
func generate(ctx context.Context, t *testing.T, suite *Suite, caseDirectory string, spec *Spec) []emit.GeneratedFile {
	t.Helper()

	options, err := emitterOptions(suite.Emitters, spec.Emitter)
	require.NoError(t, err)
	options = append(options,
		seagull.WithCustomFunctions(spec.CustomFunctions...),
		seagull.WithTypeOverrides(spec.TypeOverrides...),
	)
	if spec.TypeSlots != nil || spec.KeepStars || spec.ReuseModels {
		chosen := seagull.DefaultPreset()
		if spec.TypeSlots != nil {
			chosen.Types = *spec.TypeSlots
		}
		chosen.KeepStars = spec.KeepStars
		chosen.ReuseModels = spec.ReuseModels
		options = append(options, seagull.WithPreset(chosen))
	}
	generator, err := seagull.New(suite.Dialect(), options...)
	require.NoError(t, err)

	result, err := generator.Generate(ctx, seagull.Input{
		Migrations:   os.DirFS(caseDirectory),
		Queries:      nil,
		Package:      generatedPackage,
		PackagePath:  "",
		MigrationDir: migrationDirectory,
		QueryDir:     queryDirectory,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	for _, problem := range DiagnosticProblems(spec.ExpectedDiagnostics, result.Diagnostics) {
		assert.Fail(t, problem)
	}
	require.NotEmpty(t, result.Files, "generation produced no files")
	return result.Files
}

// emitterOptions returns the options that select the emitter a spec names.
//
// Takes emitters (map[string]seagull.Option) which maps the names the suite accepts.
// Takes name (string) which is the spec's emitter, or empty for database/sql.
//
// Returns []seagull.Option which selects the emitter.
// Returns error when the suite accepts no emitter of that name.
func emitterOptions(emitters map[string]seagull.Option, name string) ([]seagull.Option, error) {
	if name == "" || name == databaseSQLEmitter {
		return nil, nil
	}
	option, found := emitters[name]
	if !found {
		accepted := append([]string{databaseSQLEmitter}, slices.Sorted(maps.Keys(emitters))...)
		return nil, fmt.Errorf("%w: %q; this suite accepts %v", errUnknownEmitter, name, accepted)
	}
	return []seagull.Option{option}, nil
}

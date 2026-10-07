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

package emittest

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/martian"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/testkit/knownfailure"
)

const (
	// knownFailuresField names the suite's known failures in messages.
	knownFailuresField = "Suite.KnownFailures"

	// subjectNoun says what the suite checks against, in messages.
	subjectNoun = "dialect"

	// checkOperation names a check in the log record of a panic it raised.
	checkOperation = "emittest check "
)

// allChecks lists every check the suite runs, in order.
var allChecks = slices.Concat(staticChecks, runtimeChecks, corpusChecks)

// Suite describes an emitter for Run: how to build it, the dialects it generates for, the
// optional runtime harness, and the checks it is known to fail today.
type Suite struct {
	// KnownFailures records failing checks and their reasons, keyed by check name or
	// <dialect>/<check>. Listed checks must still fail, and each entry must give a reason.
	KnownFailures map[string]string

	// New returns a fresh emitter for one generation with the dialect whose facts info
	// holds. It must not change info.
	New func(info *dialect.Facts) emit.EmitterPort

	// Harness, when set, adds the runtime lane: the generated code is built with the
	// harness's recording stand-in for the driver, and every method of the runtime cases is
	// called. The runtime checks are skipped without it.
	Harness *Harness

	// Name names the emitter in messages.
	Name string

	// Dialects lists the suite's prebuilt dialects, each with a distinct name.
	//
	// Run adds the martian dialect, an invented engine that shares no convention with a real
	// database, unless one of that name is listed, so every emitter is shown to assume no
	// real engine; a known failure may name it. The suite serialises access to each dialect.
	Dialects []dialect.Dialect

	// Corpora lists golden-style case directories written in one dialect's SQL, whose output
	// the corpus checks examine.
	Corpora []Corpus
}

// Corpus describes a tree of dialect-specific generation test cases.
//
// Each case supplies a spec, migrations and queries. Skipped cases and cases producing
// errors are excluded from checks of generated code.
type Corpus struct {
	// Cases holds the case directories at its root, such as os.DirFS("testdata").
	Cases fs.FS

	// Dialect is the name of the dialect the cases are written for, which must be one of
	// Suite.Dialects.
	Dialect string
}

// check is one behaviour the suite checks for each dialect.
type check struct {
	// run performs the check for one dialect and reports its outcome.
	run func(ctx context.Context, current *subject) knownfailure.Outcome

	// name is the check's subtest name and its KnownFailures key.
	name string

	// toolchain is true when the check needs the go command, so it is skipped under -short.
	toolchain bool
}

// Run checks an emitter's generation and runtime contracts for each dialect in the suite,
// and for the martian dialect, which it adds to them.
//
// Checks run as subtests. KnownFailures entries must still fail; unexpected failures fail
// the suite. A check that panics fails with the panic's value, and its stack is logged
// once.
//
// Takes t (*testing.T) which reports test failures.
// Takes suite (Suite) which describes the emitter and the dialects to drive it with.
func Run(t *testing.T, suite Suite) {
	t.Helper()

	suite.Dialects = withMartian(suite.Dialects)
	if problems := validateSuite(&suite, allChecks); len(problems) > 0 {
		t.Fatalf("emittest: invalid Suite %q:\n%s", suite.Name, strings.Join(problems, "\n"))
	}
	table := knownFailureTable(&suite, allChecks)
	lane := newRuntimeLane(t, &suite)
	subjects := make([]*subject, 0, len(suite.Dialects))
	for _, engine := range suite.Dialects {
		subjects = append(subjects, newSubject(&suite, dialect.Unwrap(engine), lane))
	}
	lane.subjects = subjects
	for _, current := range subjects {
		t.Run(current.info.Name, func(t *testing.T) {
			t.Parallel()
			for _, entry := range allChecks {
				t.Run(entry.name, func(t *testing.T) {
					t.Parallel()
					runCheck(t, table, current, entry)
				})
			}
		})
	}
}

// withMartian returns the dialects a suite lists with the martian dialect added, an
// invented engine that shares no convention with a real database. It adds nothing to an
// empty list, which the suite refuses, or to one that already holds a dialect of that
// name.
//
// Takes dialects ([]dialect.Dialect) which are the dialects the suite lists.
//
// Returns []dialect.Dialect which holds them and, unless it was listed, the martian
// dialect, in a slice of its own.
func withMartian(dialects []dialect.Dialect) []dialect.Dialect {
	if len(dialects) == 0 {
		return dialects
	}
	for _, engine := range dialects {
		if dialect.Unwrap(engine) != nil && engine.Info().Name == martian.Name {
			return dialects
		}
	}
	return append(slices.Clone(dialects), dialect.Wrap(martian.New()))
}

// validateSuite reports what makes a suite unusable: a missing emitter or dialect, two
// dialects of one name, a corpus for a dialect the suite lacks, and a known failure that
// names no check or no dialect of the suite, or gives no reason.
//
// Takes suite (*Suite) which is the suite to validate.
// Takes checks ([]check) which are the suite's checks.
//
// Returns []string which describes each problem.
func validateSuite(suite *Suite, checks []check) []string {
	var problems []string
	if suite.New == nil {
		problems = append(problems, "Suite.New is nil")
	}
	if len(suite.Dialects) == 0 {
		problems = append(problems, "Suite.Dialects is empty")
	}
	names := make(map[string]bool, len(suite.Dialects))
	for index, engine := range suite.Dialects {
		if dialect.Unwrap(engine) == nil {
			problems = append(problems, fmt.Sprintf("Suite.Dialects[%d] holds no dialect", index))
			continue
		}
		name := engine.Info().Name
		if names[name] {
			problems = append(problems, fmt.Sprintf("two dialects are named %q", name))
		}
		names[name] = true
	}
	for _, corpus := range suite.Corpora {
		if !names[corpus.Dialect] || corpus.Cases == nil {
			problems = append(problems, fmt.Sprintf("a corpus names dialect %q, which the suite does not drive, or has no cases", corpus.Dialect))
		}
	}
	return append(problems, knownFailureTable(suite, checks).Problems()...)
}

// knownFailureTable returns the suite's known failures with the names their keys may use:
// a check's name alone covers every dialect, and a dialect's name, a slash and a check's
// name cover that dialect alone.
//
// Takes suite (*Suite) which holds the known failures and the dialects.
// Takes checks ([]check) which are the suite's checks.
//
// Returns *knownfailure.Table which judges the suite's checks.
func knownFailureTable(suite *Suite, checks []check) *knownfailure.Table {
	checkNames := make([]string, 0, len(checks))
	for _, entry := range checks {
		checkNames = append(checkNames, entry.name)
	}
	dialectNames := make([]string, 0, len(suite.Dialects))
	for _, engine := range suite.Dialects {
		if dialect.Unwrap(engine) != nil {
			dialectNames = append(dialectNames, engine.Info().Name)
		}
	}
	return &knownfailure.Table{Failures: suite.KnownFailures, Field: knownFailuresField, Subject: subjectNoun,
		Checks: checkNames, Subjects: dialectNames}
}

// runCheck runs one check for one dialect and reports its verdict, holding a known
// failure to still failing.
//
// The check runs under the subtest's context, which carries a logger writing warnings to
// the subtest's output, so the stack of a panic recovered during the check is logged
// there once.
//
// Takes t (*testing.T) which reports test failures.
// Takes table (*knownfailure.Table) which holds the suite's known failures.
// Takes current (*subject) which is the dialect under test.
// Takes entry (check) which is the check to run.
func runCheck(t *testing.T, table *knownfailure.Table, current *subject, entry check) {
	t.Helper()

	if entry.toolchain && testing.Short() {
		t.Skip("the check needs the go command; skipped under -short")
	}
	ctx := logging.ContextWithLogger(t.Context(), slog.New(slog.NewTextHandler(t.Output(),
		&slog.HandlerOptions{AddSource: false, Level: slog.LevelWarn, ReplaceAttr: withoutTime})))
	report := table.Judge(entry.name, guardedRun(ctx, current, entry), table.Lookup(current.info.Name, entry.name))
	if report.Skip != "" {
		t.Skip(report.Skip)
	}
	if report.Note != "" {
		t.Log(report.Note)
	}
	for _, problem := range report.Errors {
		t.Error(problem)
	}
}

// guardedRun runs a check, turning a panic into the check's failure. The panic's stack is
// logged once through the logger the context carries and is kept out of the failure.
//
// Takes current (*subject) which is the dialect under test.
// Takes entry (check) which is the check to run.
//
// Returns knownfailure.Outcome which is the check's outcome, or a failure naming the
// value it panicked with.
func guardedRun(ctx context.Context, current *subject, entry check) (result knownfailure.Outcome) {
	defer func() {
		if recovered := recover(); recovered != nil {
			logging.LogRecoveredPanic(ctx, checkOperation+entry.name, recovered)
			result = knownfailure.Failed("check panicked: %v", recovered)
		}
	}()
	return entry.run(ctx, current)
}

// withoutTime drops the time from a log record written to a subtest's output, which the
// test output places already.
//
// Takes groups ([]string) which are the groups the attribute is in.
// Takes attribute (slog.Attr) which is the attribute.
//
// Returns slog.Attr which is the attribute, or an empty one for the record's time.
func withoutTime(groups []string, attribute slog.Attr) slog.Attr {
	if len(groups) == 0 && attribute.Key == slog.TimeKey {
		return slog.Attr{Key: "", Value: slog.Value{}}
	}
	return attribute
}

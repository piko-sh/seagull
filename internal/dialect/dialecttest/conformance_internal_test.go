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
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/sqlmodel"
)

type commentPrefixDialect struct {
	dialect.DialectPort
	prefix string
}

func (wrapped commentPrefixDialect) Info() dialect.Info {
	info := wrapped.DialectPort.Info()
	info.Comments.LinePrefix = wrapped.prefix
	return info
}

type attributelessDialect struct {
	dialect.DialectPort
}

func (attributelessDialect) BuiltinTypes() *ast.TypeCatalogue {
	return &ast.TypeCatalogue{Types: map[string]sqlmodel.SQLType{
		"whole": sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "whole"),
		"stamp": sqlmodel.NewSQLType(sqlmodel.TypeCategoryTemporal, "stamp"),
		"text":  sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"),
	}}
}

func (attributelessDialect) CoreTypes() dialect.CoreTypes {
	return dialect.CoreTypes{
		Boolean:  sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "flag"),
		Text:     sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"),
		Count:    sqlmodel.NewSQLType(sqlmodel.TypeCategoryInteger, "whole"),
		JSON:     sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
		RowCount: sqlmodel.NewIntegerType("whole32", 32, false),
		RowID:    sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
	}
}

func TestJudgeCheckHoldsKnownFailuresToFailing(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		outcome       checkOutcome
		failures      map[string]string
		expectSkip    string
		expectNote    string
		expectErrors  []string
		inChild       bool
		expectAnyNote bool
	}{
		{
			name:     "an ordinary pass reports nothing",
			outcome:  passed,
			failures: nil,
		},
		{
			name:         "an ordinary failure reports its problems",
			outcome:      failed("broken"),
			failures:     nil,
			expectErrors: []string{"broken"},
		},
		{
			name:          "a known failure that still fails is noted",
			outcome:       failed("broken"),
			failures:      map[string]string{"InfoIsValid": "a recorded bug"},
			expectAnyNote: true,
		},
		{
			name:         "a known failure that now passes is an error",
			outcome:      passed,
			failures:     map[string]string{"InfoIsValid": "a recorded bug"},
			expectErrors: []string{"InfoIsValid is recorded as a known failure (a recorded bug) but now passes; remove it from Fixture.KnownFailures"},
		},
		{
			name:       "a check that does not apply is skipped",
			outcome:    notApplicable("no fixture"),
			failures:   nil,
			expectSkip: "no fixture",
		},
		{
			name:         "a known failure that does not apply is an error",
			outcome:      notApplicable("no fixture"),
			failures:     map[string]string{"InfoIsValid": "a recorded bug"},
			expectErrors: []string{"InfoIsValid is recorded as a known failure (a recorded bug) but does not apply: no fixture"},
		},
		{
			name:         "inside the subprocess problems are reported as they are",
			outcome:      failed("broken"),
			failures:     map[string]string{"InfoIsValid": "a recorded bug"},
			inChild:      true,
			expectErrors: []string{"broken"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			failures := knownFailureTable(&Fixture{KnownFailures: testCase.failures}, conformanceChecks)
			verdict := judgeCheck(failures, "InfoIsValid", testCase.outcome, testCase.inChild)
			require.Equal(t, testCase.expectSkip, verdict.Skip)
			require.Equal(t, testCase.expectErrors, verdict.Errors)
			if testCase.expectAnyNote {
				require.Contains(t, verdict.Note, "still failing:\nbroken")
			} else {
				require.Equal(t, testCase.expectNote, verdict.Note)
			}
		})
	}
}

func TestValidateFixtureRejectsUnusableFixtures(t *testing.T) {
	t.Parallel()

	newDialect := func() dialect.DialectPort { return postgres.New() }
	testCases := []struct {
		fixture  *Fixture
		name     string
		expected []string
	}{
		{
			name:     "a usable fixture has no problems",
			fixture:  &Fixture{NewDialect: newDialect, KnownFailures: map[string]string{"InfoIsValid": "a recorded bug"}},
			expected: nil,
		},
		{
			name:     "a nil fixture",
			fixture:  nil,
			expected: []string{"the Fixture is nil"},
		},
		{
			name:     "a fixture without a dialect",
			fixture:  &Fixture{NewDialect: nil},
			expected: []string{"Fixture.NewDialect is nil"},
		},
		{
			name:     "a flavour without its constructor",
			fixture:  &Fixture{NewDialect: newDialect, Flavour: &FlavourFixture{New: nil, UserFunction: "f"}},
			expected: []string{"Fixture.Flavour is set but its New is nil"},
		},
		{
			name:     "a known failure naming no check",
			fixture:  &Fixture{NewDialect: newDialect, KnownFailures: map[string]string{"Renamed": "a recorded bug"}},
			expected: []string{`Fixture.KnownFailures names "Renamed", which is not a check`},
		},
		{
			name:     "a known failure giving no reason",
			fixture:  &Fixture{NewDialect: newDialect, KnownFailures: map[string]string{"InfoIsValid": " "}},
			expected: []string{`Fixture.KnownFailures entry "InfoIsValid" gives no reason`},
		},
		{
			name: "every problem at once",
			fixture: &Fixture{
				NewDialect:    nil,
				Flavour:       &FlavourFixture{New: nil},
				KnownFailures: map[string]string{"Renamed": ""},
			},
			expected: []string{
				"Fixture.NewDialect is nil",
				"Fixture.Flavour is set but its New is nil",
				`Fixture.KnownFailures names "Renamed", which is not a check`,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, validateFixture(testCase.fixture, conformanceChecks))
		})
	}
}

func TestRunCheckRecoveredTurnsAPanicIntoAFailureAndLogsItsStackOnce(t *testing.T) {
	t.Parallel()

	panickingDialect := func() dialect.DialectPort { panic("the dialect is broken") }
	testCases := []struct {
		check          conformanceCheck
		name           string
		expected       checkOutcome
		expectedLogged int
	}{
		{
			name: "a check that passes is reported as it is",
			check: conformanceCheck{name: "Passing", run: func(context.Context, checkEnvironment, *Fixture) checkOutcome {
				return passed
			}},
			expected:       passed,
			expectedLogged: 0,
		},
		{
			name: "a check that panics fails with the panic's value",
			check: conformanceCheck{name: "Panicking", run: func(context.Context, checkEnvironment, *Fixture) checkOutcome {
				panic("boom")
			}},
			expected:       failed("check panicked: boom"),
			expectedLogged: 1,
		},
		{
			name: "a dialect that panics fails the check that drives it",
			check: conformanceCheck{name: "InfoIsValid", run: func(ctx context.Context, environment checkEnvironment, fixture *Fixture) checkOutcome {
				return checkInfoIsValid(ctx, environment, &Fixture{NewDialect: panickingDialect})
			}},
			expected:       failed("check panicked: the dialect is broken"),
			expectedLogged: 1,
		},
		{
			name: "a check that panics with an error fails with its text",
			check: conformanceCheck{name: "Erroring", run: func(context.Context, checkEnvironment, *Fixture) checkOutcome {
				panic(errors.New("an error value"))
			}},
			expected:       failed("check panicked: an error value"),
			expectedLogged: 1,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var logged []string
			logf := func(format string, arguments ...any) { logged = append(logged, fmt.Sprintf(format, arguments...)) }
			outcome := runCheckRecovered(t.Context(), logf, testCase.check, checkEnvironment{testName: t.Name(), inChild: false}, &Fixture{})
			require.Equal(t, testCase.expected, outcome)
			require.Len(t, logged, testCase.expectedLogged)
			for _, entry := range logged {
				require.Contains(t, entry, testCase.check.name+" panicked")
				require.Contains(t, entry, "goroutine")
				require.NotContains(t, outcome.Problems[0], "goroutine")
			}
		})
	}
}

func TestConformanceCheckNamesAreUnique(t *testing.T) {
	t.Parallel()

	seen := make(map[string]struct{})
	for _, check := range conformanceChecks {
		require.NotContains(t, seen, check.name)
		seen[check.name] = struct{}{}
	}
}

func TestDirectiveLineUsesTheDialectCommentPrefix(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		prefix   string
		expected string
	}{
		{name: "the dialect's own prefix", prefix: "#", expected: "# sgl.query(name: Deep, command: one)\n"},
		{name: "the SQL default when none is declared", prefix: "", expected: "-- sgl.query(name: Deep, command: one)\n"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := commentPrefixDialect{DialectPort: postgres.New(), prefix: testCase.prefix}
			require.Equal(t, testCase.expected, directiveLine(engine, "Deep", "one"))
		})
	}
}

func TestStructuralAttributesCheck(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		engine   func() dialect.DialectPort
		problems []string
	}{
		{name: "a dialect whose types carry their attributes passes", engine: func() dialect.DialectPort { return postgres.New() }, problems: nil},
		{
			name:   "a dialect whose types lack their attributes fails",
			engine: func() dialect.DialectPort { return attributelessDialect{DialectPort: postgres.New()} },
			problems: []string{
				`the built-in temporal type "stamp" has no structural attributes`,
				`the built-in integer type "whole" has no structural attributes`,
				"the core boolean type is text, not boolean",
				"the core count type is not an integer of a known width",
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			outcome := checkStructuralAttributes(t.Context(), checkEnvironment{}, &Fixture{NewDialect: testCase.engine})
			require.Equal(t, testCase.problems, outcome.Problems)
		})
	}
}

func TestEntryPointProbes(t *testing.T) {
	probes := []struct {
		run  func(t *testing.T)
		name string
	}{
		{name: "RunConformance", run: func(t *testing.T) { RunConformance(t, nil) }},
		{name: "RunGolden", run: func(t *testing.T) { RunGolden(t, nil, "testdata") }},
		{name: "RunEngineFacts", run: func(t *testing.T) { RunEngineFacts(t, &EngineFixture{Database: nil, Table: ""}) }},
	}
	for _, probe := range probes {
		t.Run(probe.name, func(t *testing.T) {
			if os.Getenv(isolatedCheckEnvironment) != t.Name() {
				t.Skip("runs only as the subprocess of TestEntryPointsStopOnAnUnusableArgumentBeforeRunningAnything")
			}
			probe.run(t)
		})
	}
}

func FuzzEntryPointProbe(f *testing.F) {
	if os.Getenv(isolatedCheckEnvironment) != f.Name() {
		f.Skip("runs only as the subprocess of TestEntryPointsStopOnAnUnusableArgumentBeforeRunningAnything")
	}
	FuzzGenerate(f, nil, "testdata")
}

func TestEntryPointsStopOnAnUnusableArgumentBeforeRunningAnything(t *testing.T) {
	if testing.Short() {
		t.Skip("reruns the test binary in a subprocess; skipped under -short")
	}
	t.Parallel()

	testCases := []struct {
		probe          string
		expectMessages []string
	}{
		{probe: "TestEntryPointProbes/RunConformance", expectMessages: []string{"dialecttest: invalid Fixture:", "the Fixture is nil"}},
		{probe: "TestEntryPointProbes/RunGolden", expectMessages: []string{"dialecttest: invalid RunGolden arguments:", "newDialect is nil"}},
		{
			probe:          "TestEntryPointProbes/RunEngineFacts",
			expectMessages: []string{"dialecttest: invalid EngineFixture:", "EngineFixture.Database is nil", "EngineFixture.Table names no table"},
		},
		{probe: "FuzzEntryPointProbe", expectMessages: []string{"dialecttest: invalid FuzzGenerate arguments:", "newDialect is nil"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.probe, func(t *testing.T) {
			t.Parallel()

			output, err := runTestInSubprocess(t.Context(), testCase.probe)
			require.Error(t, err)
			require.Contains(t, string(output), "--- FAIL: "+testCase.probe+" ")
			for _, message := range testCase.expectMessages {
				require.Contains(t, string(output), message)
			}
			require.NotContains(t, string(output), "=== RUN   "+testCase.probe+"/")
		})
	}
}

func CheckNamesForTest() []string {
	names := make([]string, 0, len(conformanceChecks))
	for _, check := range conformanceChecks {
		names = append(names, check.name)
	}
	return names
}

func RunCheckForTest(ctx context.Context, name string, fixture *Fixture) (problems []string, notApplicable string) {
	for _, check := range conformanceChecks {
		if check.name == name {
			outcome := runCheckRecovered(ctx, func(string, ...any) {}, check, checkEnvironment{testName: name, inChild: true}, fixture)
			return outcome.Problems, outcome.NotApplicable
		}
	}
	return []string{"no conformance check is named " + name}, ""
}

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
	"bytes"
	"context"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/martian"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/knownfailure"
)

type silentEmitter struct{}

func (silentEmitter) Emit(context.Context, *emit.Request) ([]sqlmodel.GeneratedFile, error) {
	return nil, nil
}

type panickingEmitter struct{}

func (panickingEmitter) Emit(context.Context, *emit.Request) ([]sqlmodel.GeneratedFile, error) {
	panic("emit boom")
}

func newSilentEmitter(*dialect.Facts) emit.EmitterPort {
	return silentEmitter{}
}

func checkNamed(t *testing.T, name string) check {
	t.Helper()

	index := slices.IndexFunc(allChecks, func(entry check) bool { return entry.name == name })
	require.GreaterOrEqual(t, index, 0, "the suite has no check named %s", name)
	return allChecks[index]
}

func TestKnownFailureTablePrefersTheDialectsOwnEntry(t *testing.T) {
	t.Parallel()

	suite := &Suite{
		KnownFailures: map[string]string{"BatchRows": "M-2", "postgres/BatchRows": "M-3", "SliceArguments": "N-16"},
		Dialects:      []dialect.Dialect{dialect.Wrap(postgres.New()), dialect.Wrap(martian.New())},
	}
	table := knownFailureTable(suite, allChecks)
	testCases := []struct {
		name     string
		dialect  string
		check    string
		expected knownfailure.Record
	}{
		{name: "the dialect's own entry wins", dialect: "postgres", check: "BatchRows", expected: knownfailure.Record{Reason: "M-3", Known: true, Scoped: true}},
		{name: "another dialect takes the shared entry", dialect: "martian", check: "BatchRows", expected: knownfailure.Record{Reason: "M-2", Known: true, Scoped: false}},
		{name: "a shared entry covers every dialect", dialect: "postgres", check: "SliceArguments", expected: knownfailure.Record{Reason: "N-16", Known: true, Scoped: false}},
		{name: "a check with no entry is not known", dialect: "postgres", check: "OutputTypeChecks", expected: knownfailure.Record{Reason: "", Known: false, Scoped: false}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, table.Lookup(testCase.dialect, testCase.check))
		})
	}
}

func TestGuardedRunReportsAPanickingCheckAsAFailureWithItsStackLoggedOnce(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		run           func(context.Context, *subject) knownfailure.Outcome
		expected      knownfailure.Outcome
		expectedStack bool
	}{
		{
			name:          "a check that panics fails with the panic's value",
			run:           func(context.Context, *subject) knownfailure.Outcome { panic("boom") },
			expected:      knownfailure.Outcome{NotApplicable: "", Problems: []string{"check panicked: boom"}},
			expectedStack: true,
		},
		{
			name: "a check that panics with an error fails with its text",
			run: func(context.Context, *subject) knownfailure.Outcome {
				var missing *subject
				return knownfailure.OutcomeOf([]string{missing.info.Name})
			},
			expected: knownfailure.Outcome{NotApplicable: "", Problems: []string{
				"check panicked: runtime error: invalid memory address or nil pointer dereference"}},
			expectedStack: true,
		},
		{
			name:          "a check that returns reports its own outcome",
			run:           func(context.Context, *subject) knownfailure.Outcome { return knownfailure.Failed("broken") },
			expected:      knownfailure.Outcome{NotApplicable: "", Problems: []string{"broken"}},
			expectedStack: false,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var output bytes.Buffer
			ctx := logging.ContextWithLogger(t.Context(), slog.New(slog.NewTextHandler(&output, nil)))

			result := guardedRun(ctx, &subject{}, check{name: "Panics", run: testCase.run, toolchain: false})

			require.Equal(t, testCase.expected, result)
			for _, problem := range result.Problems {
				require.NotContains(t, problem, "goroutine", "the stack stays out of the failure")
			}
			if testCase.expectedStack {
				require.Equal(t, 1, strings.Count(output.String(), "recovered a panic"))
				require.Contains(t, output.String(), "operation=\"emittest check Panics\"")
				require.Contains(t, output.String(), "goroutine")
				return
			}
			require.Empty(t, output.String())
		})
	}
}

func TestRunReportsAPanickingEmitterInsteadOfCrashing(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		emitter       func(*dialect.Facts) emit.EmitterPort
		panicsPerCase int
	}{
		{
			name:          "an emitter constructor that panics stops the case's first generation",
			emitter:       func(*dialect.Facts) emit.EmitterPort { panic("constructor boom") },
			panicsPerCase: 1,
		},
		{
			name:          "an emitter that panics while emitting fails each of the case's three generations",
			emitter:       func(*dialect.Facts) emit.EmitterPort { return panickingEmitter{} },
			panicsPerCase: 3,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			current := newSubject(&Suite{Name: "panics", New: testCase.emitter}, postgres.New(), &runtimeLane{})
			var output bytes.Buffer
			ctx := logging.ContextWithLogger(t.Context(), slog.New(slog.NewTextHandler(&output, nil)))

			result := guardedRun(ctx, current, check{name: "GeneratesWithoutError", run: checkGeneratesWithoutError, toolchain: false})

			require.NotEmpty(t, result.Problems)
			for _, problem := range result.Problems {
				require.Contains(t, problem, "Generate failed instead of reporting a diagnostic")
				require.Contains(t, problem, "boom")
				require.NotContains(t, problem, "goroutine")
			}
			require.Equal(t, testCase.panicsPerCase*len(generalCaseNames()), strings.Count(output.String(), "recovered a panic"),
				"each panic is logged once")
		})
	}
}

func TestValidateSuiteRejectsUnusableSuites(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		suite    Suite
		expected []string
	}{
		{
			name:     "a complete suite is valid",
			suite:    Suite{Name: "silent", New: newSilentEmitter, Dialects: []dialect.Dialect{dialect.Wrap(postgres.New()), dialect.Wrap(martian.New())}},
			expected: nil,
		},
		{
			name:     "a suite needs an emitter and a dialect",
			suite:    Suite{Name: "empty"},
			expected: []string{"Suite.New is nil", "Suite.Dialects is empty"},
		},
		{
			name:     "two dialects need distinct names",
			suite:    Suite{Name: "twice", New: newSilentEmitter, Dialects: []dialect.Dialect{dialect.Wrap(postgres.New()), dialect.Wrap(postgres.New())}},
			expected: []string{`two dialects are named "postgres"`},
		},
		{
			name: "a corpus must name a dialect of the suite",
			suite: Suite{Name: "corpus", New: newSilentEmitter, Dialects: []dialect.Dialect{dialect.Wrap(postgres.New())},
				Corpora: []Corpus{{Cases: nil, Dialect: "martian"}}},
			expected: []string{`a corpus names dialect "martian", which the suite does not drive, or has no cases`},
		},
		{
			name: "a known failure must name a check and a dialect of the suite, with a reason",
			suite: Suite{Name: "failures", New: newSilentEmitter, Dialects: []dialect.Dialect{dialect.Wrap(postgres.New())},
				KnownFailures: map[string]string{"Renamed": "N-1", "martian/BatchRows": "M-2", "postgres/BatchRows": " "}},
			expected: []string{
				`Suite.KnownFailures entry "postgres/BatchRows" gives no reason`,
				`Suite.KnownFailures names "Renamed", which is not a check`,
				`Suite.KnownFailures names "martian/BatchRows", whose dialect the kit does not drive`,
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.ElementsMatch(t, testCase.expected, validateSuite(&testCase.suite, allChecks))
		})
	}
}

func TestWithMartianAddsTheMartianDialectUnlessListed(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		listed   []dialect.Dialect
		expected []string
	}{
		{name: "a suite of real dialects gets the martian one", listed: []dialect.Dialect{dialect.Wrap(postgres.New())}, expected: []string{"postgres", "martian"}},
		{name: "a suite listing the martian dialect keeps its own", listed: []dialect.Dialect{dialect.Wrap(martian.New()), dialect.Wrap(postgres.New())}, expected: []string{"martian", "postgres"}},
		{name: "an empty suite stays empty for validation to refuse", listed: nil, expected: nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			listed := slices.Clone(testCase.listed)
			driven := withMartian(listed)
			var names []string
			for _, engine := range driven {
				names = append(names, engine.Info().Name)
			}
			require.Equal(t, testCase.expected, names)
			require.Len(t, listed, len(testCase.listed), "the listed dialects are left as they were")
		})
	}
}

func TestCheckNamesAreUnique(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool)
	for _, entry := range allChecks {
		require.False(t, seen[entry.name], "two checks are named %s", entry.name)
		seen[entry.name] = true
	}
}

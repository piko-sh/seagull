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

package knownfailure_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/knownfailure"
)

func sharedTable(failures map[string]string) *knownfailure.Table {
	return &knownfailure.Table{
		Failures: failures,
		Field:    "Suite.KnownFailures",
		Subject:  "dialect",
		Checks:   []string{"Binds", "Scans"},
		Subjects: []string{"postgres", "sqlite"},
	}
}

func singleTable(failures map[string]string) *knownfailure.Table {
	return &knownfailure.Table{
		Failures: failures,
		Field:    "Fixture.KnownFailures",
		Subject:  "dialect",
		Checks:   []string{"Binds", "Scans"},
		Subjects: nil,
	}
}

func TestProblemsApplyOneSetOfReasonAndScopeRules(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		table    *knownfailure.Table
		expected []string
	}{
		{
			name:     "a shared table with valid keys has no problems",
			table:    sharedTable(map[string]string{"Binds": "N-1", "sqlite/Scans": "N-2"}),
			expected: nil,
		},
		{
			name:     "a shared table rejects a key naming no check",
			table:    sharedTable(map[string]string{"Renamed": "N-1"}),
			expected: []string{`Suite.KnownFailures names "Renamed", which is not a check`},
		},
		{
			name:     "a shared table rejects a scoped key naming no check",
			table:    sharedTable(map[string]string{"sqlite/Renamed": "N-1"}),
			expected: []string{`Suite.KnownFailures names "sqlite/Renamed", which is not a check`},
		},
		{
			name:     "a shared table rejects a key naming a subject it does not drive",
			table:    sharedTable(map[string]string{"mysql/Binds": "N-1"}),
			expected: []string{`Suite.KnownFailures names "mysql/Binds", whose dialect the kit does not drive`},
		},
		{
			name:     "a shared table rejects an empty reason",
			table:    sharedTable(map[string]string{"Binds": ""}),
			expected: []string{`Suite.KnownFailures entry "Binds" gives no reason`},
		},
		{
			name:     "a shared table rejects a reason of only spaces",
			table:    sharedTable(map[string]string{"postgres/Scans": " \t"}),
			expected: []string{`Suite.KnownFailures entry "postgres/Scans" gives no reason`},
		},
		{
			name:     "a single-subject table with valid keys has no problems",
			table:    singleTable(map[string]string{"Binds": "N-1"}),
			expected: nil,
		},
		{
			name:     "a single-subject table rejects an empty reason",
			table:    singleTable(map[string]string{"Scans": ""}),
			expected: []string{`Fixture.KnownFailures entry "Scans" gives no reason`},
		},
		{
			name:     "a single-subject table reads a separated key as a check it does not have",
			table:    singleTable(map[string]string{"postgres/Binds": "N-1"}),
			expected: []string{`Fixture.KnownFailures names "postgres/Binds", which is not a check`},
		},
		{
			name:  "problems are sorted",
			table: sharedTable(map[string]string{"Zeta": "N-1", "Alpha": "N-2", "Binds": ""}),
			expected: []string{
				`Suite.KnownFailures entry "Binds" gives no reason`,
				`Suite.KnownFailures names "Alpha", which is not a check`,
				`Suite.KnownFailures names "Zeta", which is not a check`,
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.expected, testCase.table.Problems())
		})
	}
}

func TestLookupPrefersTheEntryNamingTheSubjectAlone(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		table    *knownfailure.Table
		subject  string
		check    string
		expected knownfailure.Record
	}{
		{
			name:     "a scoped entry wins over the shared one",
			table:    sharedTable(map[string]string{"Binds": "shared", "sqlite/Binds": "sqlite only"}),
			subject:  "sqlite",
			check:    "Binds",
			expected: knownfailure.Record{Reason: "sqlite only", Known: true, Scoped: true},
		},
		{
			name:     "the shared entry covers a subject with no entry of its own",
			table:    sharedTable(map[string]string{"Binds": "shared", "sqlite/Binds": "sqlite only"}),
			subject:  "postgres",
			check:    "Binds",
			expected: knownfailure.Record{Reason: "shared", Known: true, Scoped: false},
		},
		{
			name:     "a check with no entry is not known",
			table:    sharedTable(map[string]string{"Binds": "shared"}),
			subject:  "postgres",
			check:    "Scans",
			expected: knownfailure.Record{Reason: "", Known: false, Scoped: false},
		},
		{
			name:     "a single-subject table names its subject alone",
			table:    singleTable(map[string]string{"Scans": "N-3"}),
			subject:  "",
			check:    "Scans",
			expected: knownfailure.Record{Reason: "N-3", Known: true, Scoped: true},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.expected, testCase.table.Lookup(testCase.subject, testCase.check))
		})
	}
}

func TestJudgeHoldsAKnownFailureToStillFailing(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		table    *knownfailure.Table
		outcome  knownfailure.Outcome
		record   knownfailure.Record
		expected knownfailure.Verdict
	}{
		{
			name:     "a passing check reports nothing",
			table:    sharedTable(nil),
			outcome:  knownfailure.Passed(),
			record:   knownfailure.Record{Reason: "", Known: false, Scoped: false},
			expected: knownfailure.Verdict{Skip: "", Note: "", Errors: nil},
		},
		{
			name:     "an ordinary failure reports its problems",
			table:    sharedTable(nil),
			outcome:  knownfailure.OutcomeOf([]string{"first", "second"}),
			record:   knownfailure.Record{Reason: "", Known: false, Scoped: false},
			expected: knownfailure.Verdict{Skip: "", Note: "", Errors: []string{"first", "second"}},
		},
		{
			name:     "a check that does not apply is skipped",
			table:    sharedTable(nil),
			outcome:  knownfailure.NotApplicable("no harness"),
			record:   knownfailure.Record{Reason: "", Known: false, Scoped: false},
			expected: knownfailure.Verdict{Skip: "no harness", Note: "", Errors: nil},
		},
		{
			name:     "a shared known failure that does not apply is skipped",
			table:    sharedTable(nil),
			outcome:  knownfailure.NotApplicable("no harness"),
			record:   knownfailure.Record{Reason: "N-1", Known: true, Scoped: false},
			expected: knownfailure.Verdict{Skip: "no harness", Note: "", Errors: nil},
		},
		{
			name:    "a scoped known failure that does not apply is an error",
			table:   sharedTable(nil),
			outcome: knownfailure.NotApplicable("no harness"),
			record:  knownfailure.Record{Reason: "N-1", Known: true, Scoped: true},
			expected: knownfailure.Verdict{Skip: "", Note: "", Errors: []string{
				"Binds is recorded as a known failure (N-1) but does not apply: no harness"}},
		},
		{
			name:    "a shared known failure that passes asks to be removed or narrowed",
			table:   sharedTable(nil),
			outcome: knownfailure.Passed(),
			record:  knownfailure.Record{Reason: "N-1", Known: true, Scoped: false},
			expected: knownfailure.Verdict{Skip: "", Note: "", Errors: []string{
				"Binds is recorded as a known failure (N-1) but now passes; remove it from Suite.KnownFailures, " +
					"or narrow it to each dialect that still fails"}},
		},
		{
			name:    "a scoped known failure that passes asks to be removed",
			table:   singleTable(nil),
			outcome: knownfailure.Passed(),
			record:  knownfailure.Record{Reason: "N-1", Known: true, Scoped: true},
			expected: knownfailure.Verdict{Skip: "", Note: "", Errors: []string{
				"Binds is recorded as a known failure (N-1) but now passes; remove it from Fixture.KnownFailures"}},
		},
		{
			name:     "a known failure that still fails is logged",
			table:    sharedTable(nil),
			outcome:  knownfailure.Failed("bound %d of %d", 1, 2),
			record:   knownfailure.Record{Reason: "N-1", Known: true, Scoped: false},
			expected: knownfailure.Verdict{Skip: "", Note: "known failure (N-1), still failing:\nbound 1 of 2", Errors: nil},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.expected, testCase.table.Judge("Binds", testCase.outcome, testCase.record))
		})
	}
}

func TestOutcomeConstructorsSayWhatTheCheckFound(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		outcome  knownfailure.Outcome
		expected knownfailure.Outcome
	}{
		{
			name:     "passed holds nothing",
			outcome:  knownfailure.Passed(),
			expected: knownfailure.Outcome{NotApplicable: "", Problems: nil},
		},
		{
			name:     "failed formats one problem",
			outcome:  knownfailure.Failed("check panicked: %v", "boom"),
			expected: knownfailure.Outcome{NotApplicable: "", Problems: []string{"check panicked: boom"}},
		},
		{
			name:     "not applicable carries its reason",
			outcome:  knownfailure.NotApplicable("no runtime lane"),
			expected: knownfailure.Outcome{NotApplicable: "no runtime lane", Problems: nil},
		},
		{
			name:     "an outcome of no problems has passed",
			outcome:  knownfailure.OutcomeOf(nil),
			expected: knownfailure.Outcome{NotApplicable: "", Problems: nil},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.expected, testCase.outcome)
		})
	}
}

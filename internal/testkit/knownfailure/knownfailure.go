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

package knownfailure

import (
	"fmt"
	"slices"
	"strings"
)

const (
	// Separator separates a subject's name from a check's name in a key that records a known
	// failure of that subject alone.
	Separator = "/"
)

// Outcome is the result of one check for one subject: the problems found, or why the
// check does not apply.
type Outcome struct {
	// NotApplicable, when set, says why the check does not apply.
	NotApplicable string

	// Problems describes each way the subject failed the check.
	Problems []string
}

// Record is a kit's record of one check for one subject.
type Record struct {
	// Reason says why the subject fails the check.
	Reason string

	// Known is true when the kit records the check as failing.
	Known bool

	// Scoped is true when the record names the subject alone, rather than every subject the
	// kit drives.
	Scoped bool
}

// Verdict is what a subtest reports for one check's outcome.
type Verdict struct {
	// Skip, when set, skips the subtest with this message.
	Skip string

	// Note, when set, is logged.
	Note string

	// Errors are reported as failures.
	Errors []string
}

// Table is one kit's known failures and the names its keys may use.
type Table struct {
	// Failures maps each key to why the check fails. A key that is a check's name covers
	// every subject the kit drives; a subject's name, Separator and a check's name covers
	// that subject alone.
	Failures map[string]string

	// Field names the map in messages, such as Suite.KnownFailures.
	Field string

	// Subject says what the kit checks, such as dialect, in messages.
	Subject string

	// Checks lists the name of every check the kit runs.
	Checks []string

	// Subjects lists the names of the subjects the kit drives. A kit that drives one subject
	// leaves it empty: its keys are check names, and each names that subject alone.
	Subjects []string
}

// Problems reports what makes the table unusable: a key that names no check of the kit or
// no subject it drives, and an entry that gives no reason.
//
// Returns []string which describes each problem, sorted.
func (table *Table) Problems() []string {
	var problems []string
	for key, reason := range table.Failures {
		subjectName, checkName, scoped := table.split(key)
		switch {
		case scoped && !slices.Contains(table.Subjects, subjectName):
			problems = append(problems, fmt.Sprintf("%s names %q, whose %s the kit does not drive", table.Field, key, table.Subject))
		case !slices.Contains(table.Checks, checkName):
			problems = append(problems, fmt.Sprintf("%s names %q, which is not a check", table.Field, key))
		case strings.TrimSpace(reason) == "":
			problems = append(problems, fmt.Sprintf("%s entry %q gives no reason", table.Field, key))
		}
	}
	slices.Sort(problems)
	return problems
}

// Lookup returns the table's record of a check for one subject: the entry naming the
// subject alone when there is one, and otherwise the entry covering every subject.
//
// Takes subject (string) which names the subject; a kit that drives one subject may pass
// an empty name.
// Takes check (string) which names the check.
//
// Returns Record which says whether the check is known to fail and why.
func (table *Table) Lookup(subject, check string) Record {
	if len(table.Subjects) == 0 {
		reason, known := table.Failures[check]
		return Record{Reason: reason, Known: known, Scoped: true}
	}
	if reason, known := table.Failures[subject+Separator+check]; known {
		return Record{Reason: reason, Known: true, Scoped: true}
	}
	reason, known := table.Failures[check]
	return Record{Reason: reason, Known: known, Scoped: false}
}

// Judge decides what a subtest reports for a check's outcome.
//
// A check that does not apply is skipped, unless the subject alone is recorded as failing
// it. An ordinary check fails on its problems. A known failure must still fail: it is
// reported once it passes, and its problems are otherwise only logged.
//
// Takes check (string) which names the check.
// Takes outcome (Outcome) which is the check's result.
// Takes record (Record) which is the table's record of the check for the subject.
//
// Returns Verdict which says what to report.
func (table *Table) Judge(check string, outcome Outcome, record Record) Verdict {
	switch {
	case outcome.NotApplicable != "" && record.Known && record.Scoped:
		return Verdict{Skip: "", Note: "", Errors: []string{fmt.Sprintf(
			"%s is recorded as a known failure (%s) but does not apply: %s", check, record.Reason, outcome.NotApplicable)}}
	case outcome.NotApplicable != "":
		return Verdict{Skip: outcome.NotApplicable, Note: "", Errors: nil}
	case !record.Known:
		return Verdict{Skip: "", Note: "", Errors: outcome.Problems}
	case len(outcome.Problems) == 0:
		return Verdict{Skip: "", Note: "", Errors: []string{table.nowPasses(check, record)}}
	default:
		return Verdict{Skip: "", Note: fmt.Sprintf("known failure (%s), still failing:\n%s", record.Reason,
			strings.Join(outcome.Problems, "\n")), Errors: nil}
	}
}

// split reads a key as the subject it names alone, if any, and the check it names.
//
// Takes key (string) which is the key.
//
// Returns subject (string) which names the subject, when the key is scoped.
// Returns check (string) which names the check.
// Returns scoped (bool) which is true when the key names one subject alone.
func (table *Table) split(key string) (subject, check string, scoped bool) {
	if len(table.Subjects) == 0 {
		return "", key, false
	}
	subject, check, scoped = strings.Cut(key, Separator)
	if !scoped {
		return "", key, false
	}
	return subject, check, true
}

// nowPasses describes a known failure that no longer fails.
//
// Takes check (string) which names the check.
// Takes record (Record) which is the table's record of it.
//
// Returns string which says to remove the record, or to narrow one covering every
// subject.
func (table *Table) nowPasses(check string, record Record) string {
	message := fmt.Sprintf("%s is recorded as a known failure (%s) but now passes; remove it from %s", check, record.Reason, table.Field)
	if !record.Scoped {
		message += fmt.Sprintf(", or narrow it to each %s that still fails", table.Subject)
	}
	return message
}

// Passed returns the outcome of a check that found no problems.
//
// Returns Outcome which holds no problems.
func Passed() Outcome {
	return Outcome{NotApplicable: "", Problems: nil}
}

// Failed returns the outcome of a check that found one problem.
//
// Takes format (string) which is the problem's format string.
// Takes arguments (...any) which fill the format.
//
// Returns Outcome which holds the problem.
func Failed(format string, arguments ...any) Outcome {
	return Outcome{NotApplicable: "", Problems: []string{fmt.Sprintf(format, arguments...)}}
}

// NotApplicable returns the outcome of a check that does not apply.
//
// Takes reason (string) which says why.
//
// Returns Outcome which carries the reason.
func NotApplicable(reason string) Outcome {
	return Outcome{NotApplicable: reason, Problems: nil}
}

// OutcomeOf turns a list of problems into an outcome.
//
// Takes problems ([]string) which may be empty, when the check passed.
//
// Returns Outcome which holds the problems.
func OutcomeOf(problems []string) Outcome {
	return Outcome{NotApplicable: "", Problems: problems}
}

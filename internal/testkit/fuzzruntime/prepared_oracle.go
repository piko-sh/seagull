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

package fuzzruntime

import (
	"fmt"
	"strings"
)

// preparationFirstFailure selects failure before any statement is prepared.
const preparationFirstFailure = 252

// preparationSecondFailure selects failure after one successful preparation.
const preparationSecondFailure = 253

// preparedCatalogueSize counts independently modelled static queries.
const preparedCatalogueSize = 2

const (
	// preparedPrepare records a successful statement preparation.
	preparedPrepare = "prepare"

	// preparedUse records an execution through a cached statement.
	preparedUse = "use"

	// preparedClose records a statement closure.
	preparedClose = "close"

	// preparedPrepareError records a rejected preparation attempt.
	preparedPrepareError = "prepare-error"

	// preparedInjectedFailure is the message of the failure the helper injects.
	preparedInjectedFailure = "injected preparation failure"
)

// preparedObservations tracks ownership and execution for one request.
type preparedObservations struct {
	// opened tracks live statement handles.
	opened map[string]bool

	// prepared identifies all successfully prepared statements.
	prepared map[string]bool

	// used counts executions through cached statements.
	used map[string]int

	// failures counts rejected preparation attempts.
	failures int
}

// accept validates and records one statement lifecycle transition.
//
// Takes event (PreparedEvent) which reports a driver operation.
//
// Returns []string which describes illegal ownership transitions.
func (observed *preparedObservations) accept(event PreparedEvent) []string {
	var problems []string
	switch event.Operation {
	case preparedPrepare:
		if observed.prepared[event.SQL] {
			problems = append(problems, "prepared the same static SQL twice")
		}
		observed.prepared[event.SQL], observed.opened[event.SQL] = true, true
	case preparedUse:
		if !observed.opened[event.SQL] {
			problems = append(problems, "executed an unprepared or closed statement")
		}
		observed.used[event.SQL]++
	case preparedClose:
		if !observed.opened[event.SQL] {
			problems = append(problems, "closed an unprepared or already closed statement")
		}
		delete(observed.opened, event.SQL)
	case preparedPrepareError:
		observed.failures++
	default:
		problems = append(problems, "unknown prepared lifecycle event "+event.Operation)
	}
	return problems
}

// executionProblems verifies catalogue membership and use of cached statements.
//
// Takes calls ([]Call) which holds terminal SQL executions.
//
// Returns []string which describes missing caching or unexpected static SQL.
func (observed *preparedObservations) executionProblems(calls []Call) []string {
	executed := make(map[string]int)
	for _, call := range calls {
		for _, statement := range call.Statements {
			executed[statement.SQL]++
		}
	}
	var problems []string
	kinds := make(map[string]bool)
	for query := range observed.prepared {
		kind := preparedStaticKind(query)
		if kind == "" || kinds[kind] {
			problems = append(problems, "prepared unexpected or repeated catalogue SQL: "+query)
		}
		kinds[kind] = true
		if observed.used[query] != executed[query] {
			problems = append(problems, fmt.Sprintf("static SQL used its prepared statement %d times for %d executions", observed.used[query], executed[query]))
		}
	}
	return problems
}

// PreparedProblems checks static caching, failure cleanup and statement closure.
//
// Takes enabled (bool) which selects the prepared catalogue.
// Takes data ([]byte) which selects an optional preparation failure.
// Takes response (Response) which contains observed lifecycle events.
//
// Returns []string which describes missing caching, errors or leaked statements.
func PreparedProblems(enabled bool, data []byte, response Response) []string {
	if !enabled {
		if response.PreparationError != "" || len(response.Prepared) != 0 {
			return []string{"direct execution unexpectedly prepared statements"}
		}
		return nil
	}
	failure := preparedFailureOrdinal(data)
	problems := preparedFailureProblems(failure, response)
	observed := preparedObservations{opened: make(map[string]bool), prepared: make(map[string]bool), used: make(map[string]int), failures: 0}
	for _, event := range response.Prepared {
		problems = append(problems, observed.accept(event)...)
	}
	wantPrepared, wantFailures := preparedCatalogueSize, 0
	if failure > 0 {
		wantPrepared, wantFailures = failure-1, 1
	}
	if len(observed.prepared) != wantPrepared || observed.failures != wantFailures || len(observed.opened) != 0 {
		problems = append(problems, fmt.Sprintf("prepared lifecycle has %d preparations, %d failures and %d leaks, expected %d, %d and zero",
			len(observed.prepared), observed.failures, len(observed.opened), wantPrepared, wantFailures))
	}
	return append(problems, observed.executionProblems(response.Calls)...)
}

// preparedStaticKind identifies either independently declared static query.
//
// Takes query (string) which is observed at the driver boundary.
//
// Returns string which is empty for a dynamic or unrelated query.
func preparedStaticKind(query string) string {
	normal := strings.Join(strings.Fields(strings.ToLower(query)), " ")
	for _, operator := range []string{">=", "<"} {
		if strings.HasPrefix(normal, "select id, score from fuzz_items where id "+operator+" ") {
			return operator
		}
	}
	return ""
}

// preparedFailureProblems checks whether the selected failure was propagated.
//
// Takes failure (int) which is zero or the failing preparation ordinal.
// Takes response (Response) which reports preparation and method results.
//
// Returns []string which describes unexpected or swallowed preparation failures.
func preparedFailureProblems(failure int, response Response) []string {
	if failure == 0 {
		if response.PreparationError != "" {
			return []string{"preparation failed: " + response.PreparationError}
		}
		return nil
	}
	if !strings.Contains(response.PreparationError, preparedInjectedFailure) || len(response.Calls) != 0 {
		return []string{"preparation failure was swallowed or execution continued"}
	}
	return nil
}

// preparedFailureOrdinal reads which preparation the input makes fail.
//
// Takes data ([]byte) which selects an optional preparation failure.
//
// Returns int which is zero for no failure, or the failing preparation counted from one.
func preparedFailureOrdinal(data []byte) int {
	if len(data) == 0 || data[0] < preparationFirstFailure || data[0] > preparationSecondFailure {
		return 0
	}
	return int(data[0]) - preparationFirstFailure + 1
}

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

package scaling

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	// runsPerWorkload is how many times each workload runs; the median is compared, so a
	// single pause in one run does not decide the result.
	runsPerWorkload = 3

	// maximumGrowth is the largest ratio allowed between the larger workload, four times the
	// size of the smaller, and the smaller: linear work grows about four times and quadratic
	// work about sixteen.
	maximumGrowth = 6

	// timingFloor is the shortest median the growth comparison trusts; a faster smaller
	// workload is compared as if it took this long, so timer noise cannot fail the test.
	timingFloor = 2 * time.Millisecond

	// attempts is how many times a timing is measured before the test fails. Work that grows
	// too fast misses on every attempt; a busy machine rarely slows every one of them.
	attempts = 3

	// cancellationSlack is how many budgets pass before the context Within hands the
	// operation is cancelled, so a run slowed by a busy machine is measured rather than
	// stopped part-way.
	cancellationSlack = 4
)

// errBudgetExceeded is the cause of the context Within hands an operation once its budget
// has elapsed.
var errBudgetExceeded = errors.New("the operation ran past its time budget")

// Within runs operation and fails the test when it takes longer than budget on every one
// of three attempts.
//
// After four times the budget, the operation's context is cancelled with
// errBudgetExceeded, allowing an operation that honours cancellation to stop. Coverage
// runs execute the operation once without checking elapsed time, because instrumentation
// distorts the timing.
//
// Takes t (testing.TB) which is the test.
// Takes budget (time.Duration) which is the longest the operation may take.
// Takes operation (func(ctx context.Context)) which is the work under test.
func Within(t testing.TB, budget time.Duration, operation func(ctx context.Context)) {
	t.Helper()

	within(t, budget, operation, testing.CoverMode() == "")
}

// Linear fails the test when the larger workload, four times the size of the smaller,
// takes more than six times as long on every one of three attempts, each comparing the
// median of three runs of each. In a coverage run it runs each workload once and compares
// no times.
//
// Takes t (testing.TB) which is the test.
// Takes smaller (func()) which runs the workload at its base size.
// Takes larger (func()) which runs the same workload at four times that size.
func Linear(t testing.TB, smaller, larger func()) {
	t.Helper()

	linear(t, smaller, larger, testing.CoverMode() == "")
}

// within is Within, with the timing checked only when timed is set.
//
// Takes t (testing.TB) which is the test.
// Takes budget (time.Duration) which is the longest the operation may take.
// Takes operation (func(ctx context.Context)) which is the work under test.
// Takes timed (bool) which is false to run operation once and check no time.
func within(t testing.TB, budget time.Duration, operation func(ctx context.Context), timed bool) {
	t.Helper()

	if !timed {
		operation(t.Context())
		return
	}
	allowed := budget * raceSlowdown
	var elapsed time.Duration
	for range attempts {
		elapsed = timeOnce(t, allowed, operation)
		if !exceedsBudget(elapsed, allowed) {
			return
		}
	}
	require.Failf(t, "the operation ran past its budget",
		"the operation took %s on its last attempt, past its budget of %s", elapsed, allowed)
}

// linear is Linear, with the timing compared only when timed is set.
//
// Takes t (testing.TB) which is the test.
// Takes smaller (func()) which runs the workload at its base size.
// Takes larger (func()) which runs the same workload at four times that size.
// Takes timed (bool) which is false to run each workload once and compare no times.
func linear(t testing.TB, smaller, larger func(), timed bool) {
	t.Helper()

	if !timed {
		smaller()
		larger()
		return
	}

	var smallerMedian, largerMedian time.Duration
	for range attempts {
		smallerMedian = medianDuration(smaller)
		largerMedian = medianDuration(larger)
		if growth(smallerMedian, largerMedian) <= float64(maximumGrowth) {
			return
		}
	}
	require.Failf(t, "the work grows faster than linearly",
		"four times the input took %s against %s on the last attempt", largerMedian, smallerMedian)
}

// timeOnce runs operation once under a context cancelled after cancellationSlack budgets
// and returns how long it took.
//
// Takes t (testing.TB) which is the test whose context the operation's derives from.
// Takes allowed (time.Duration) which is the budget.
// Takes operation (func(ctx context.Context)) which is the work under test.
//
// Returns time.Duration which is how long the operation took.
func timeOnce(t testing.TB, allowed time.Duration, operation func(ctx context.Context)) time.Duration {
	t.Helper()

	ctx, cancel := context.WithTimeoutCause(t.Context(), allowed*cancellationSlack, errBudgetExceeded)
	defer cancel()

	started := time.Now()
	operation(ctx)
	return time.Since(started)
}

// exceedsBudget reports whether an elapsed time is past a budget.
//
// Takes elapsed (time.Duration) which is how long the operation took.
// Takes budget (time.Duration) which is how long it was allowed.
//
// Returns bool which is true when the operation ran past its budget.
func exceedsBudget(elapsed, budget time.Duration) bool {
	return elapsed > budget
}

// medianDuration runs a workload runsPerWorkload times and returns the median duration.
//
// Takes workload (func()) which is the work to time.
//
// Returns time.Duration which is the median of the runs.
func medianDuration(workload func()) time.Duration {
	durations := make([]time.Duration, 0, runsPerWorkload)
	for range runsPerWorkload {
		started := time.Now()
		workload()
		durations = append(durations, time.Since(started))
	}
	return median(durations)
}

// median returns the middle value of a set of durations.
//
// Takes durations ([]time.Duration) which holds at least one duration; it is sorted in
// place.
//
// Returns time.Duration which is the median, or zero for an empty set.
func median(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	slices.Sort(durations)
	return durations[len(durations)/2]
}

// growth returns how many times longer the larger workload took than the smaller, timing
// a smaller workload faster than timingFloor as if it had taken that long.
//
// Takes smaller (time.Duration) which is the smaller workload's median.
// Takes larger (time.Duration) which is the larger workload's median.
//
// Returns float64 which is the ratio of the two.
func growth(smaller, larger time.Duration) float64 {
	return float64(larger) / float64(max(smaller, timingFloor))
}

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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExceedsBudgetComparesElapsedWithTheBudget(t *testing.T) {
	testCases := []struct {
		name    string
		elapsed time.Duration
		budget  time.Duration
		want    bool
	}{
		{name: "well inside the budget", elapsed: time.Millisecond, budget: time.Second, want: false},
		{name: "exactly at the budget", elapsed: time.Second, budget: time.Second, want: false},
		{name: "past the budget", elapsed: 2 * time.Second, budget: time.Second, want: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.want, exceedsBudget(testCase.elapsed, testCase.budget))
		})
	}
}

func TestMedianReturnsTheMiddleDuration(t *testing.T) {
	testCases := []struct {
		name      string
		durations []time.Duration
		want      time.Duration
	}{
		{name: "an empty set", durations: nil, want: 0},
		{name: "one run", durations: []time.Duration{time.Second}, want: time.Second},
		{name: "an outlier is ignored", durations: []time.Duration{time.Millisecond, time.Hour, 2 * time.Millisecond}, want: 2 * time.Millisecond},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.want, median(testCase.durations))
		})
	}
}

func TestGrowthComparesMediansAboveTheTimingFloor(t *testing.T) {
	testCases := []struct {
		name    string
		smaller time.Duration
		larger  time.Duration
		want    float64
	}{
		{name: "linear work grows four times", smaller: 10 * time.Millisecond, larger: 40 * time.Millisecond, want: 4},
		{name: "quadratic work grows sixteen times", smaller: 10 * time.Millisecond, larger: 160 * time.Millisecond, want: 16},
		{name: "a smaller workload below the floor is timed at the floor", smaller: time.Microsecond, larger: 4 * time.Millisecond, want: 2},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.InDelta(t, testCase.want, growth(testCase.smaller, testCase.larger), 0.001)
		})
	}
}

func TestWithinRunsTheOperationWithACancellableContext(t *testing.T) {
	ran := false
	within(t, time.Second, func(ctx context.Context) {
		require.NoError(t, ctx.Err())
		_, hasDeadline := ctx.Deadline()
		require.True(t, hasDeadline)
		ran = true
	}, true)
	require.True(t, ran)
}

func TestLinearAcceptsWorkThatGrowsWithItsInput(t *testing.T) {
	work := func(size int) func() {
		return func() {
			total := 0
			for index := range size {
				total += index
			}
			_ = total
		}
	}
	linear(t, work(1_000), work(4_000), true)
}

func TestAnUntimedRunDoesEachWorkloadOnceAndChecksNoTime(t *testing.T) {
	operations, smaller, larger := 0, 0, 0
	within(t, time.Nanosecond, func(context.Context) { operations++ }, false)
	linear(t, func() { smaller++ }, func() { larger++ }, false)
	assert.Equal(t, 1, operations, "an untimed run does the operation once, past its budget or not")
	assert.Equal(t, 1, smaller)
	assert.Equal(t, 1, larger)
}

func TestTheExportedHelpersTimeTheWorkOutsideACoverageRun(t *testing.T) {
	ran := 0
	Within(t, time.Second, func(context.Context) { ran++ })
	Linear(t, func() { ran++ }, func() { ran++ })
	if testing.CoverMode() == "" {
		assert.GreaterOrEqual(t, ran, 1+2*runsPerWorkload, "timed runs repeat each workload for a median")
		return
	}
	assert.Equal(t, 3, ran, "a coverage run does each workload once")
}

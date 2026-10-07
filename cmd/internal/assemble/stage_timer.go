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

package assemble

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"seagull.sh/seagull/sdk/observe"
)

// StageTiming is the time a generation spent in one stage.
type StageTiming struct {
	// Stage names the stage.
	Stage observe.Stage

	// Total is the summed duration of the stage's ended spans.
	Total time.Duration

	// Spans is the number of the stage's spans that have ended.
	Spans int

	// Failures is the number of the stage's spans that ended with an error.
	Failures int
}

// StageTimer is the observer behind sgl generate --timings and the playground's stage
// timings.
//
// It adds together the time spent in each stage, including stages that run once per
// query, and reports one total per stage. It is safe for concurrent use.
type StageTimer struct {
	// clock reads the time each span starts and ends.
	clock observe.Clock

	// positions maps each stage to its entry in timings.
	positions map[observe.Stage]int

	// timings holds each stage's totals, in the order the stages first started.
	timings []StageTiming

	// mutex guards positions, timings and stopped.
	mutex sync.Mutex

	// stopped stops the timer taking further spans.
	stopped bool
}

// NewStageTimer creates an empty timer that times with a clock.
//
// Takes clock (observe.Clock) which reads the time, such as observe.SystemClock().
//
// Returns *StageTimer which is ready to observe a generation.
func NewStageTimer(clock observe.Clock) *StageTimer {
	return &StageTimer{clock: clock, positions: map[observe.Stage]int{}, timings: nil, mutex: sync.Mutex{}, stopped: false}
}

// Start opens a span for a stage and returns the function that closes it. Once the timer
// is stopped, the span is not timed.
//
// Takes ctx (context.Context) which is returned unchanged.
// Takes stage (observe.Stage) which names the work.
//
// Returns context.Context which is ctx unchanged.
// Returns func(error) which records the span's duration and outcome.
func (timer *StageTimer) Start(ctx context.Context, stage observe.Stage, _ ...slog.Attr) (context.Context, func(err error)) {
	if !timer.open(stage) {
		return ctx, func(error) {}
	}
	started := timer.clock.Now()
	return ctx, func(err error) {
		timer.record(stage, timer.clock.Now().Sub(started), err)
	}
}

// Stop stops the timer taking further spans. A span already started still records when it
// ends.
//
// Concurrency: holds the timer's mutex.
func (timer *StageTimer) Stop() {
	timer.mutex.Lock()
	defer timer.mutex.Unlock()
	timer.stopped = true
}

// Stages returns the timings of the stages in the order they first started.
//
// Returns []StageTiming which is a copy, one entry per stage.
//
// Concurrency: holds the timer's mutex.
func (timer *StageTimer) Stages() []StageTiming {
	timer.mutex.Lock()
	defer timer.mutex.Unlock()
	return slices.Clone(timer.timings)
}

// open notes that a stage has started, giving a stage seen for the first time its entry.
//
// Takes stage (observe.Stage) which has started.
//
// Returns bool which is false when the timer is stopped and the span is not timed.
//
// Concurrency: holds the timer's mutex.
func (timer *StageTimer) open(stage observe.Stage) bool {
	timer.mutex.Lock()
	defer timer.mutex.Unlock()
	if timer.stopped {
		return false
	}
	if _, seen := timer.positions[stage]; !seen {
		timer.positions[stage] = len(timer.timings)
		timer.timings = append(timer.timings, StageTiming{Stage: stage, Total: 0, Spans: 0, Failures: 0})
	}
	return true
}

// record adds one ended span to its stage's totals.
//
// Takes stage (observe.Stage) which names the span's stage, opened when it started.
// Takes elapsed (time.Duration) which is the span's duration.
// Takes err (error) which is the span's outcome.
//
// Concurrency: holds the timer's mutex.
func (timer *StageTimer) record(stage observe.Stage, elapsed time.Duration, err error) {
	timer.mutex.Lock()
	defer timer.mutex.Unlock()
	timing := &timer.timings[timer.positions[stage]]
	timing.Total += elapsed
	timing.Spans++
	if err != nil {
		timing.Failures++
	}
}

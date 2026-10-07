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
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/sdk/observe"
)

const (
	concurrentSpans = 64
)

type timerStep struct {
	err     error
	stage   observe.Stage
	elapsed time.Duration
	stop    bool
}

type steppingClock struct {
	now   time.Time
	mutex sync.Mutex
}

func (c *steppingClock) Now() time.Time {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.now
}

func (c *steppingClock) advance(elapsed time.Duration) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.now = c.now.Add(elapsed)
}

func TestStageTimerSumsEachStageInTheOrderItFirstStarted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		steps []timerStep
		want  []StageTiming
	}{
		{
			name:  "no span",
			steps: nil,
			want:  nil,
		},
		{
			name:  "one span",
			steps: []timerStep{{stage: observe.StagePlan, elapsed: 3 * time.Millisecond}},
			want:  []StageTiming{{Stage: observe.StagePlan, Total: 3 * time.Millisecond, Spans: 1, Failures: 0}},
		},
		{
			name: "several spans of two stages, one failing",
			steps: []timerStep{
				{stage: observe.StagePlan, elapsed: 2 * time.Millisecond},
				{stage: observe.StageEmit, elapsed: time.Millisecond, err: errors.New("emitter failed")},
				{stage: observe.StagePlan, elapsed: 5 * time.Millisecond},
			},
			want: []StageTiming{
				{Stage: observe.StagePlan, Total: 7 * time.Millisecond, Spans: 2, Failures: 0},
				{Stage: observe.StageEmit, Total: time.Millisecond, Spans: 1, Failures: 1},
			},
		},
		{
			name: "a span started once the timer stopped",
			steps: []timerStep{
				{stage: observe.StagePlan, elapsed: 2 * time.Millisecond},
				{stop: true},
				{stage: observe.StageEmit, elapsed: time.Millisecond},
				{stage: observe.StagePlan, elapsed: time.Millisecond},
			},
			want: []StageTiming{{Stage: observe.StagePlan, Total: 2 * time.Millisecond, Spans: 1, Failures: 0}},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			clock := &steppingClock{now: time.Date(2026, time.October, 12, 9, 0, 0, 0, time.UTC), mutex: sync.Mutex{}}
			timer := NewStageTimer(clock)
			for _, step := range testCase.steps {
				if step.stop {
					timer.Stop()
					continue
				}
				_, end := timer.Start(t.Context(), step.stage)
				clock.advance(step.elapsed)
				end(step.err)
			}

			assert.Equal(t, testCase.want, timer.Stages())
		})
	}
}

func TestStageTimerOrdersANestedStageAfterTheStageAroundIt(t *testing.T) {
	t.Parallel()

	clock := &steppingClock{now: time.Date(2026, time.October, 12, 9, 0, 0, 0, time.UTC), mutex: sync.Mutex{}}
	timer := NewStageTimer(clock)

	_, endGenerate := timer.Start(t.Context(), observe.StageGenerate)
	_, endPlan := timer.Start(t.Context(), observe.StagePlan)
	clock.advance(2 * time.Millisecond)
	endPlan(nil)
	clock.advance(time.Millisecond)
	endGenerate(nil)

	assert.Equal(t, []StageTiming{
		{Stage: observe.StageGenerate, Total: 3 * time.Millisecond, Spans: 1, Failures: 0},
		{Stage: observe.StagePlan, Total: 2 * time.Millisecond, Spans: 1, Failures: 0},
	}, timer.Stages())
}

func TestStageTimerRecordsASpanStartedBeforeItStopped(t *testing.T) {
	t.Parallel()

	clock := &steppingClock{now: time.Date(2026, time.October, 12, 9, 0, 0, 0, time.UTC), mutex: sync.Mutex{}}
	timer := NewStageTimer(clock)

	_, end := timer.Start(t.Context(), observe.StageEmit)
	timer.Stop()
	clock.advance(4 * time.Millisecond)
	end(nil)

	assert.Equal(t, []StageTiming{{Stage: observe.StageEmit, Total: 4 * time.Millisecond, Spans: 1, Failures: 0}}, timer.Stages())
}

func TestStageTimerStagesIsACopy(t *testing.T) {
	t.Parallel()

	timer := NewStageTimer(observe.SystemClock())
	_, end := timer.Start(t.Context(), observe.StagePlan)
	end(nil)

	stages := timer.Stages()
	require.Len(t, stages, 1)
	stages[0].Spans = 99

	assert.Equal(t, 1, timer.Stages()[0].Spans)
}

func TestStageTimerCountsSpansThatEndOnSeveralGoroutines(t *testing.T) {
	t.Parallel()

	timer := NewStageTimer(observe.SystemClock())
	var waitGroup sync.WaitGroup
	for range concurrentSpans {
		waitGroup.Go(func() {
			_, end := timer.Start(t.Context(), observe.StageAnalyseQuery)
			end(nil)
		})
	}
	waitGroup.Wait()

	stages := timer.Stages()
	require.Len(t, stages, 1)
	assert.Equal(t, concurrentSpans, stages[0].Spans)
}

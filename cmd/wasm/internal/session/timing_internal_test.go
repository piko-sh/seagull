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

package session

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/internal/assemble"
	"seagull.sh/seagull/sdk/observe"
)

type steppingClock struct {
	now   time.Time
	step  time.Duration
	reads int
	mutex sync.Mutex
}

func newSteppingClock(step time.Duration) *steppingClock {
	return &steppingClock{now: time.Date(2026, time.October, 12, 9, 0, 0, 0, time.UTC), step: step, reads: 0, mutex: sync.Mutex{}}
}

func (clock *steppingClock) Now() time.Time {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	current := clock.now
	clock.now = clock.now.Add(clock.step)
	clock.reads++
	return current
}

func (clock *steppingClock) readCount() int {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	return clock.reads
}

func TestStageViewsSumEachStageWithItsClock(t *testing.T) {
	t.Parallel()

	type span struct {
		stage  observe.Stage
		nested []observe.Stage
	}
	testCases := []struct {
		name  string
		spans []span
		want  []StageView
	}{
		{
			name:  "no stage",
			spans: nil,
			want:  []StageView{},
		},
		{
			name:  "one stage",
			spans: []span{{stage: observe.StageGenerate, nested: nil}},
			want:  []StageView{{Stage: "generate", MS: 2}},
		},
		{
			name: "a repeated stage is summed in the order stages first start",
			spans: []span{
				{stage: observe.StageAnalyse, nested: []observe.Stage{observe.StageAnalyseQuery, observe.StageAnalyseQuery}},
				{stage: observe.StageVet, nested: nil},
			},
			want: []StageView{{Stage: "generate.analyse", MS: 10}, {Stage: "generate.analyse.query", MS: 4}, {Stage: "vet", MS: 2}},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			timer := assemble.NewStageTimer(newSteppingClock(2 * time.Millisecond))
			for _, outer := range testCase.spans {
				_, endOuter := timer.Start(t.Context(), outer.stage)
				for _, inner := range outer.nested {
					_, endInner := timer.Start(t.Context(), inner)
					endInner(nil)
				}
				endOuter(nil)
			}

			require.Equal(t, testCase.want, stageViews(timer.Stages()))
		})
	}
}

func TestMillisecondsTruncatesToWholeMicroseconds(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		elapsed time.Duration
		want    float64
	}{
		{name: "zero", elapsed: 0, want: 0},
		{name: "whole milliseconds", elapsed: 3 * time.Millisecond, want: 3},
		{name: "microseconds", elapsed: 1500 * time.Microsecond, want: 1.5},
		{name: "nanoseconds below a microsecond", elapsed: time.Microsecond + 999*time.Nanosecond, want: 0.001},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.InDelta(t, testCase.want, milliseconds(testCase.elapsed), 0)
		})
	}
}

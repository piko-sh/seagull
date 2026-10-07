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
	"math"
	"time"

	"seagull.sh/seagull/cmd/internal/assemble"
)

const (
	// microsecondsPerMillisecond converts microseconds to milliseconds.
	microsecondsPerMillisecond = 1000
)

// stageViews converts a generation's stage timings to the views the response holds.
//
// Takes stages ([]assemble.StageTiming) which holds the timings in the order the stages
// first started.
//
// Returns []StageView which holds one entry per stage, in the same order.
func stageViews(stages []assemble.StageTiming) []StageView {
	views := make([]StageView, 0, len(stages))
	for _, timing := range stages {
		views = append(views, StageView{Stage: string(timing.Stage), MS: milliseconds(timing.Total)})
	}
	return views
}

// milliseconds converts a duration to milliseconds after truncating it to whole
// microseconds.
//
// Takes elapsed (time.Duration) which is converted.
//
// Returns float64 which is the duration in milliseconds.
func milliseconds(elapsed time.Duration) float64 {
	return math.Round(float64(elapsed.Microseconds())) / microsecondsPerMillisecond
}

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

package harness

import "time"

// deadlineScale multiplies every per-input, per-statement and watchdog deadline of the
// fuzz targets, so an engine whose DDL is slow gets room without loosening the others. It
// stays 1 unless a suite's TestMain raises it before the tests run.
var deadlineScale = 1

// ScaleDeadlines multiplies the fuzz deadlines of the suite by factor. Call it from
// TestMain before Main, since the tests read it without locking.
//
// Takes factor (int) which is the multiplier; one or less leaves the deadlines as they
// are.
func ScaleDeadlines(factor int) {
	deadlineScale = max(factor, 1)
}

// scaled returns a deadline multiplied by the suite's deadline scale.
//
// Takes deadline (time.Duration) which is the deadline at scale one.
//
// Returns time.Duration which is the deadline the suite allows.
func scaled(deadline time.Duration) time.Duration {
	return deadline * time.Duration(deadlineScale)
}

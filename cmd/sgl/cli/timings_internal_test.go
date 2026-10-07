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

package cli

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"seagull.sh/seagull/cmd/internal/assemble"
	"seagull.sh/seagull/sdk/observe"
)

func TestWriteTimingsPrintsOneRowPerStage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		stages []assemble.StageTiming
		want   string
	}{
		{
			name:   "no stage",
			stages: nil,
			want: "timings for database \"app\":\n" +
				"  STAGE  SPANS  TOTAL  FAILED\n",
		},
		{
			name:   "one stage",
			stages: []assemble.StageTiming{{Stage: observe.StagePlan, Total: 3 * time.Millisecond, Spans: 1, Failures: 0}},
			want: "timings for database \"app\":\n" +
				"  STAGE          SPANS  TOTAL  FAILED\n" +
				"  generate.plan  1      3ms    0\n",
		},
		{
			name: "two stages, one with a failed span, rounded to the microsecond",
			stages: []assemble.StageTiming{
				{Stage: observe.StagePlan, Total: 7 * time.Millisecond, Spans: 2, Failures: 0},
				{Stage: observe.StageEmit, Total: time.Millisecond + 400*time.Nanosecond, Spans: 1, Failures: 1},
			},
			want: "timings for database \"app\":\n" +
				"  STAGE          SPANS  TOTAL  FAILED\n" +
				"  generate.plan  2      7ms    0\n" +
				"  generate.emit  1      1ms    1\n",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var rendered bytes.Buffer

			writeTimings(&rendered, "app", testCase.stages)

			assert.Equal(t, testCase.want, rendered.String())
		})
	}
}

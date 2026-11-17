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

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRepresentationFreeMatchesIntervalTextWithDurations(t *testing.T) {
	t.Parallel()
	for text, want := range map[string]time.Duration{"7 days": 7 * 24 * time.Hour, "-28 days": -28 * 24 * time.Hour, "1 day 02:00:00": 26 * time.Hour,
		"-01:30:00.5": -(90*time.Minute + 500*time.Millisecond), "-1 days +02:00:00": -22 * time.Hour} {
		got, ok := intervalDuration(text)
		require.True(t, ok, text)
		require.Equal(t, want, got, text)
	}
	for _, text := range []string{"1 mon", "café", "", "1 year 2 days"} {
		_, ok := intervalDuration(text)
		require.False(t, ok, text)
	}
	require.Equal(t, [][]string{{"duration:604800000000000", `text:"0"`, `text:"0.5"`}},
		representationFree([][]string{{`text:"7 days"`, `text:"0.000000"`, `text:"0.5"`}}))
	require.NotEqual(t, representationFree([][]string{{`text:"1.000000"`}}), representationFree([][]string{{`text:"1"`}}), "only zero loses its scale")
}

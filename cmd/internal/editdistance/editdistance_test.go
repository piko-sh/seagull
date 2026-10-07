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

package editdistance

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/scaling"
)

func TestDistanceCountsRuneEditsUpToTheLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  string
		right string
		limit int
		want  int
	}{
		{name: "equal", left: "seagull", right: "seagull", limit: 3, want: 0},
		{name: "one insertion", left: "migratons", right: "migrations", limit: 3, want: 1},
		{name: "from empty", left: "", right: "sgl", limit: 4, want: 3},
		{name: "a multi-byte rune is one edit", left: "café", right: "cafe", limit: 3, want: 1},
		{name: "capped at the limit", left: "abcdef", right: "uvwxyz", limit: 3, want: 3},
		{name: "lengths too far apart", left: "a", right: "abcdefgh", limit: 2, want: 2},
		{name: "no limit measures nothing", left: "a", right: "b", limit: 0, want: 0},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, Distance(testCase.left, testCase.right, testCase.limit))
		})
	}
}

func TestDistanceIsBoundedForLongText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  string
		right string
	}{
		{name: "a megabyte against a short name", left: strings.Repeat("x", 1<<20), right: "migrations"},
		{name: "two megabytes of similar length", left: strings.Repeat("x", 1<<20), right: strings.Repeat("y", 1<<20)},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var distance int
			scaling.Within(t, 100*time.Millisecond, func(context.Context) {
				distance = Distance(testCase.left, testCase.right, 3)
			})

			require.Equal(t, 3, distance)
		})
	}
}

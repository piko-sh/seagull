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

package check

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func TestSourceLinesPlaceOffsetsAsSpanOfDoes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		sql       string
		startLine int
	}{
		{name: "one line", sql: "SELECT id FROM users", startLine: 1},
		{name: "several lines", sql: "-- sgl.query(name: Get)\nSELECT id,\n  name\nFROM users\n", startLine: 4},
		{name: "empty lines and a trailing break", sql: "\n\nSELECT 1\n\n", startLine: 2},
		{name: "multi-byte text", sql: "SELECT 'ä€'\nFROM \"ünïcode\"", startLine: 7},
		{name: "no text", sql: "", startLine: 3},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			lines := NewSourceLines(testCase.sql, testCase.startLine)
			for start := -1; start <= len(testCase.sql)+1; start++ {
				for end := start - 1; end <= len(testCase.sql)+1; end++ {
					require.Equal(t, diagbuild.SpanOf(testCase.sql, testCase.startLine, start, end), lines.Span(start, end),
						"offsets %d to %d", start, end)
				}
			}
		})
	}
}

func TestSourceLinesPlaceManyProblemsInLinearTime(t *testing.T) {
	t.Parallel()

	placeAll := func(lineCount int) func() {
		sql := strings.Repeat("SELECT unknown_column\n", lineCount)
		return func() {
			lines := NewSourceLines(sql, 1)
			for offset := 0; offset < len(sql); offset += len("SELECT unknown_column\n") {
				_ = lines.Span(offset+len("SELECT "), offset+len("SELECT unknown_column"))
			}
		}
	}

	scaling.Linear(t, placeAll(4_000), placeAll(16_000))
}

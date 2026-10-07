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

package diagbuild

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSpanOf(t *testing.T) {
	t.Parallel()

	sql := "-- sgl.query(name: Q, command: many)\nSELECT nmae\nFROM users;"

	tests := []struct {
		name  string
		start int
		end   int
		want  TextSpan
	}{
		{name: "statement on the second line", start: 37, end: len(sql), want: TextSpan{Line: 6, Column: 1, EndLine: 7, EndColumn: 12}},
		{name: "word inside a line", start: 44, end: 48, want: TextSpan{Line: 6, Column: 8, EndLine: 6, EndColumn: 12}},
		{name: "offsets past the text are clamped", start: 200, end: 300, want: TextSpan{Line: 7, Column: 12, EndLine: 7, EndColumn: 12}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, SpanOf(sql, 5, testCase.start, testCase.end))
		})
	}
}

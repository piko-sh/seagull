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
	"slices"
	"strings"

	"seagull.sh/seagull/internal/diagbuild"
)

const (
	// MaxDiagnosticsPerQuery is the most diagnostics one query block reports, and the most
	// one pass reports for it; the rest are counted and summarised in one Q066 diagnostic.
	MaxDiagnosticsPerQuery = 1_000
)

// SourceLines places byte offsets of a query block's text in its file, as
// diagbuild.SpanOf does, finding the lines through an index of the text's line breaks
// that is built on first use, so placing many problems costs one pass over the text.
type SourceLines struct {
	// sql is the text the offsets point into.
	sql string

	// breaks holds the offset of each line break in sql, in order; nil until first use.
	breaks []int

	// startLine is the file line the text starts on.
	startLine int

	// indexed is true once breaks is built.
	indexed bool
}

// NewSourceLines returns the placer for a query block's text.
//
// Takes sql (string) which is the text the offsets point into.
// Takes startLine (int) which is the file line the text starts on.
//
// Returns *SourceLines which places offsets of the text.
func NewSourceLines(sql string, startLine int) *SourceLines {
	return &SourceLines{sql: sql, breaks: nil, startLine: startLine, indexed: false}
}

// Span turns a range of byte offsets in the text into a span of the file, exactly as
// diagbuild.SpanOf does: lines are counted from the line the text starts on, columns
// count bytes from one, and the range is clamped to the text.
//
// Takes start (int) which is the offset of the first byte.
// Takes end (int) which is the offset one past the last byte.
//
// Returns diagbuild.TextSpan which covers the range.
func (lines *SourceLines) Span(start, end int) diagbuild.TextSpan {
	start = min(max(start, 0), len(lines.sql))
	end = min(max(end, start), len(lines.sql))
	line, column := lines.position(start)
	endLine, endColumn := lines.position(end)
	return diagbuild.TextSpan{Line: line, Column: column, EndLine: endLine, EndColumn: endColumn}
}

// position returns the file line and one-based byte column of an offset in the text.
//
// Takes offset (int) which is the offset, within the text.
//
// Returns line (int) which is the line.
// Returns column (int) which is the column.
func (lines *SourceLines) position(offset int) (line int, column int) {
	if !lines.indexed {
		lines.index()
	}
	preceding, _ := slices.BinarySearch(lines.breaks, offset)
	lineStart := 0
	if preceding > 0 {
		lineStart = lines.breaks[preceding-1] + 1
	}
	return lines.startLine + preceding, offset - lineStart + 1
}

// index records the offset of every line break in the text.
func (lines *SourceLines) index() {
	lines.indexed = true
	for offset := 0; offset < len(lines.sql); {
		next := strings.IndexByte(lines.sql[offset:], '\n')
		if next == -1 {
			return
		}
		lines.breaks = append(lines.breaks, offset+next)
		offset += next + 1
	}
}

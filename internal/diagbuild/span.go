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

import "strings"

// TextSpan represents a range of text in a source file.
//
// All positions are 1-based. EndColumn is exclusive (one past the last character).
type TextSpan struct {
	// Line holds the 1-based starting line number.
	Line int

	// Column holds the 1-based starting column number.
	Column int

	// EndLine holds the 1-based ending line number.
	EndLine int

	// EndColumn holds the 1-based exclusive ending column number.
	EndColumn int
}

// SpanOf turns a range of byte offsets in a piece of SQL into a span of the file the SQL
// was read from. Lines are counted from the line the SQL starts on, and columns count
// bytes from one.
//
// Takes sql (string) which is the text the offsets point into.
// Takes startLine (int) which is the file line the text starts on.
// Takes start (int) which is the offset of the first byte.
// Takes end (int) which is the offset one past the last byte.
//
// Returns TextSpan which covers the range, clamped to the text.
func SpanOf(sql string, startLine int, start int, end int) TextSpan {
	start = min(max(start, 0), len(sql))
	end = min(max(end, start), len(sql))
	line, column := positionOf(sql, startLine, start)
	endLine, endColumn := positionOf(sql, startLine, end)
	return TextSpan{Line: line, Column: column, EndLine: endLine, EndColumn: endColumn}
}

// positionOf returns the file line and one-based byte column of an offset in a piece of
// SQL.
//
// Takes sql (string) which is the text.
// Takes startLine (int) which is the file line the text starts on.
// Takes offset (int) which is the offset, within the text.
//
// Returns line (int) which is the line.
// Returns column (int) which is the column.
func positionOf(sql string, startLine int, offset int) (line int, column int) {
	preceding := sql[:offset]
	line = startLine + strings.Count(preceding, "\n")
	column = offset - strings.LastIndexByte(preceding, '\n')
	return line, column
}

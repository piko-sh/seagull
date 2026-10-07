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

package emitkit

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// lineCommentMarker opens a Go line comment.
	lineCommentMarker = "//"

	// lineCommentSpace separates the marker from the comment's text, so no line of a
	// generated doc comment reads as a directive such as //go:generate.
	lineCommentSpace = " "

	// byteOrderMark is the rune Go source may hold only at its very start.
	byteOrderMark = '\uFEFF'
)

// CommentLines formats text as Go doc comment lines, dropping trailing spaces and
// carriage returns and using bare // lines between paragraphs.
//
// It trims empty lines from both ends and produces no comment for whitespace alone. NULs,
// byte order marks and invalid UTF-8 bytes are replaced with U+FFFD so the result is
// valid in a Go comment.
//
// Takes text (string) which is the text, its lines separated by newlines.
//
// Returns []string which holds the comment's lines, or nil for no text.
func CommentLines(text string) []string {
	lines := strings.Split(strings.Map(commentRune, text), "\n")
	for index, line := range lines {
		lines[index] = strings.TrimRightFunc(line, unicode.IsSpace)
	}
	first, last := 0, len(lines)
	for first < last && lines[first] == "" {
		first++
	}
	for last > first && lines[last-1] == "" {
		last--
	}
	if first == last {
		return nil
	}
	comment := make([]string, 0, last-first)
	for _, line := range lines[first:last] {
		if line == "" {
			comment = append(comment, lineCommentMarker)
			continue
		}
		comment = append(comment, lineCommentMarker+lineCommentSpace+line)
	}
	return comment
}

// commentRune returns the rune a doc comment writes for a rune of its text: none for a
// carriage return, the replacement character for a rune Go source cannot hold in a
// comment, and the rune itself otherwise. strings.Map hands it utf8.RuneError for each
// byte that is not valid UTF-8.
//
// Takes character (rune) which is the rune of the text.
//
// Returns rune which is the rune written, or -1 to leave it out.
func commentRune(character rune) rune {
	switch character {
	case '\r':
		return -1
	case 0, byteOrderMark:
		return utf8.RuneError
	default:
		return character
	}
}

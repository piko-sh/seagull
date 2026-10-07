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

package dialectkit

import (
	"strings"
)

const (
	// executableCommentOpener opens a comment whose contents MySQL runs, optionally followed
	// by the minimum server version that runs it.
	executableCommentOpener = "/*!"

	// versionedExecutableCommentOpener opens a comment whose contents MariaDB runs, followed
	// by the minimum server version that runs it.
	versionedExecutableCommentOpener = "/*M!"
)

// LexicalRules declares the lexical rules that vary between SQL dialects.
//
// Each engine constructs one value describing its dialect and passes the embedded rule
// sets to the shared scanners, so the scanning code itself never branches on a dialect
// name: every difference is a data value here.
type LexicalRules struct {
	// Comments describes how the dialect recognises line and block comments.
	Comments CommentRules

	// Numbers describes which base-prefixed integer literals the dialect accepts.
	Numbers NumberRules
}

// CommentRules captures the dialect-specific variations in SQL comment syntax. The shared
// comment scanner reproduces every engine's behaviour by reading these flags rather than
// by holding per-dialect code paths.
type CommentRules struct {
	// DoubleDashRequiresWhitespace gates whether a "--" sequence begins a line comment.
	//
	// When set, a "--" begins a line comment only when the next byte is whitespace or a
	// control character (any byte at or below a space, including a newline, a carriage
	// return, or end of input). MySQL requires this so that "a--b" is not misread as a
	// comment; the other dialects treat "--" as a comment unconditionally.
	DoubleDashRequiresWhitespace bool

	// HashLineComment makes a "#" begin a line comment that runs to end of line. MySQL and
	// ClickHouse accept this form; the other dialects do not.
	HashLineComment bool

	// NestedBlockComments makes "/* ... */" comments nest, so an inner "/*" must be balanced
	// by a matching "*/" before the outer comment closes.
	//
	// Postgres, DuckDB, and ClickHouse nest; MySQL and SQLite close on the first "*/".
	NestedBlockComments bool
}

// SkipWhitespaceAndComments advances past a run of whitespace and SQL comments starting
// at position, honouring the dialect's comment rules. It stops at the first byte that is
// neither whitespace nor part of a comment.
//
// Takes input (string) which is the source being scanned.
// Takes position (int) which is the byte offset to begin scanning from.
//
// Returns int which is the byte offset of the first non-whitespace, non-comment byte (or
// the length of input when the remainder is whitespace and comments).
// Returns error when a block comment is opened but never closed before end of input.
func (rules CommentRules) SkipWhitespaceAndComments(input string, position int) (int, error) {
	return rules.skip(input, position, false)
}

// SkipToExecutableComment advances past whitespace and comments as
// SkipWhitespaceAndComments does, except that it stops in front of an executable comment,
// one opening with "/*!" or "/*M!", whose contents the server runs as SQL.
//
// An opener counts only where a block comment would start, so one written inside a line
// comment or an ordinary block comment stays part of that comment. The scan reads each
// byte once, so its cost grows linearly with the text it skips.
//
// Takes input (string) which is the source being scanned.
// Takes position (int) which is the byte offset to begin scanning from.
//
// Returns int which is the byte offset of the first byte that is neither whitespace nor
// part of an ordinary comment: an executable comment's opener, another token, or the
// length of input.
// Returns error when a block comment is opened but never closed before end of input.
func (rules CommentRules) SkipToExecutableComment(input string, position int) (int, error) {
	return rules.skip(input, position, true)
}

// skip advances past whitespace and comments, stopping in front of an executable comment
// when stopAtExecutable is set.
//
// Takes input (string) which is the source being scanned.
// Takes position (int) which is the byte offset to begin scanning from.
// Takes stopAtExecutable (bool) which makes an executable comment end the run instead of
// being skipped.
//
// Returns int which is the offset where the run ends.
// Returns error when a block comment is opened but never closed before end of input.
func (rules CommentRules) skip(input string, position int, stopAtExecutable bool) (int, error) {
	for position < len(input) {
		next, err := rules.skipOne(input, position, stopAtExecutable)
		if err != nil || next == position {
			return next, err
		}
		position = next
	}
	return position, nil
}

// skipOne advances past the whitespace byte or the comment that begins at position.
//
// Takes input (string) which is the source being scanned.
// Takes position (int) which is the offset of the candidate, inside input.
// Takes stopAtExecutable (bool) which leaves an executable comment unskipped.
//
// Returns int which is the offset after the whitespace or comment, or position when
// neither begins there, or an executable comment does and stopAtExecutable is set.
// Returns error when a block comment is opened but never closed before end of input.
func (rules CommentRules) skipOne(input string, position int, stopAtExecutable bool) (int, error) {
	character := input[position]
	switch {
	case character == ' ' || character == '\t' || character == '\n' || character == '\r':
		return position + 1, nil
	case rules.startsDoubleDashComment(input, position), rules.HashLineComment && character == '#':
		return consumeToLineEnd(input, position), nil
	case character == '/' && position+1 < len(input) && input[position+1] == '*':
		if stopAtExecutable && ExecutableCommentOpenerLength(input, position) > 0 {
			return position, nil
		}
		return skipBlockComment(input, position, rules.NestedBlockComments)
	default:
		return position, nil
	}
}

// startsDoubleDashComment reports whether a "--" line comment begins at position under
// the dialect's rules.
//
// Takes input (string) which is the source being scanned.
// Takes position (int) which is the candidate comment start.
//
// Returns bool which is true when a line comment begins at position.
func (rules CommentRules) startsDoubleDashComment(input string, position int) bool {
	if position+1 >= len(input) || input[position] != '-' || input[position+1] != '-' {
		return false
	}
	if !rules.DoubleDashRequiresWhitespace {
		return true
	}

	if position+2 >= len(input) {
		return true
	}
	return input[position+2] <= ' '
}

// ExecutableCommentOpenerLength returns the length of the executable comment opener,
// "/*!" or "/*M!", that begins at position, or zero when none does.
//
// Takes input (string) which is the source being scanned.
// Takes position (int) which is the candidate opener's offset.
//
// Returns int which is the opener's length in bytes, or zero, as it is for a position
// outside input.
func ExecutableCommentOpenerLength(input string, position int) int {
	if position < 0 || position >= len(input) {
		return 0
	}
	rest := input[position:]
	switch {
	case strings.HasPrefix(rest, executableCommentOpener):
		return len(executableCommentOpener)
	case strings.HasPrefix(rest, versionedExecutableCommentOpener):
		return len(versionedExecutableCommentOpener)
	default:
		return 0
	}
}

// consumeToLineEnd advances position past the remainder of the current line.
//
// It stops at the next newline or carriage return so the caller's whitespace handling
// consumes it. Stopping on a carriage return as well as a newline lets the scanner
// terminate a line comment under classic Mac (lone "\r") line endings as well as Unix and
// Windows endings.
//
// Takes input (string) which is the source being scanned.
// Takes position (int) which is the byte offset to begin from.
//
// Returns int which is the offset of the next newline or carriage return, or the length
// of input.
func consumeToLineEnd(input string, position int) int {
	for position < len(input) && input[position] != '\n' && input[position] != '\r' {
		position++
	}
	return position
}

// skipBlockComment advances past a "/* ... */" block comment beginning at position,
// nesting inner comments when nested is true.
//
// Takes input (string) which is the source being scanned.
// Takes position (int) which indexes the opening "/*".
// Takes nested (bool) which enables balanced nesting of inner block comments.
//
// Returns int which is the offset after the closing "*/", or where scanning stopped on an
// unterminated comment.
// Returns error when the comment is not closed before end of input.
func skipBlockComment(input string, position int, nested bool) (int, error) {
	startPosition := position
	position += 2
	depth := 1
	for position < len(input) && depth > 0 {
		if nested && position+1 < len(input) && input[position] == '/' && input[position+1] == '*' {
			depth++
			position += 2
			continue
		}
		if position+1 < len(input) && input[position] == '*' && input[position+1] == '/' {
			depth--
			position += 2
			continue
		}
		position++
	}

	if depth > 0 {
		return position, ParseErrorAt(input, startPosition, startPosition+2, "unterminated block comment")
	}

	return position, nil
}

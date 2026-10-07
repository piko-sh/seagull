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

package sqllex

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	// excerptRunes is the most runes of source text an error message quotes.
	excerptRunes = 16

	// excerptEllipsis marks an excerpt cut short.
	excerptEllipsis = "..."

	// proceduralBlockCallback names the ProceduralBlock callback in a CallbackError.
	proceduralBlockCallback = "ProceduralBlock"

	// requiresNoTransactionCallback names the RequiresNoTransaction callback in a
	// CallbackError.
	requiresNoTransactionCallback = "RequiresNoTransaction"
)

var (
	// ErrMalformed is wrapped by every lexical error, so a caller can map all of them to its
	// own malformed-SQL error with one errors.Is check.
	ErrMalformed = errors.New("malformed SQL")

	// ErrUnterminatedString reports a string literal whose closing quote is missing.
	ErrUnterminatedString = errors.New("unterminated string literal")

	// ErrUnterminatedQuotedIdentifier reports a quoted identifier whose closing quote or
	// bracket is missing.
	ErrUnterminatedQuotedIdentifier = errors.New("unterminated quoted identifier")

	// ErrUnterminatedComment reports a block comment whose closing */ is missing.
	ErrUnterminatedComment = errors.New("unterminated block comment")

	// ErrUnterminatedDollarQuote reports a dollar-quoted string whose closing tag is
	// missing.
	ErrUnterminatedDollarQuote = errors.New("unterminated dollar-quoted string")

	// ErrUnterminatedBlock reports a procedural BEGIN ... END block, or a CASE inside one,
	// still open at the end of the input.
	ErrUnterminatedBlock = errors.New("unterminated procedural block")

	// ErrRulesCallback is wrapped by every CallbackError, so a caller can tell a panic in a
	// host-supplied Rules callback from malformed SQL with one errors.Is check.
	ErrRulesCallback = errors.New("a lexical rules callback panicked")
)

// Error is a lexical error and where it happened. It wraps its sentinel (such as
// ErrUnterminatedString) and ErrMalformed, so errors.Is matches either.
type Error struct {
	// Err is the sentinel naming the problem.
	Err error

	// Excerpt is up to sixteen runes of the source text at Offset, cut on a rune boundary,
	// followed by an ellipsis when more text remains.
	Excerpt string

	// Offset is the byte offset where the failing construct starts.
	Offset int

	// Line is the 1-based line of Offset.
	Line int

	// Column is the 1-based column of Offset, counted in runes so multi-byte characters
	// count once.
	Column int
}

// newError builds an Error for the construct starting at offset in input.
//
// Takes input (string) which is the text being scanned.
// Takes offset (int) which is the byte offset of the failing construct.
// Takes sentinel (error) which names the problem.
//
// Returns *Error which carries the position and an excerpt.
func newError(input string, offset int, sentinel error) *Error {
	line, column := lineAndColumn(input, offset)
	return &Error{
		Err:     sentinel,
		Excerpt: excerpt(input, offset),
		Offset:  offset,
		Line:    line,
		Column:  column,
	}
}

// Error describes the problem and its position.
//
// Returns string which is the message, such as `unterminated string literal at line 2,
// column 8 (byte 19) near "'abc"`.
func (lexicalError *Error) Error() string {
	return fmt.Sprintf("%v at line %d, column %d (byte %d) near %q",
		lexicalError.Err, lexicalError.Line, lexicalError.Column, lexicalError.Offset, lexicalError.Excerpt)
}

// Unwrap returns the sentinel and ErrMalformed.
//
// Returns []error which holds Err and ErrMalformed.
func (lexicalError *Error) Unwrap() []error {
	return []error{lexicalError.Err, ErrMalformed}
}

// CallbackError reports a panic recovered from a host-supplied Rules callback, such as
// ProceduralBlock or RequiresNoTransaction.
//
// Split and RequiresNoTransaction return the callback error with the position reached. It
// wraps ErrRulesCallback and, if the panic value is an error, that value too. It never
// wraps ErrMalformed, because the failure is in the callback rather than the SQL text.
type CallbackError struct {
	// Value is the value the callback panicked with.
	Value any

	// Callback names the Rules field whose function panicked, such as "ProceduralBlock".
	Callback string
}

// Error names the callback and the panic value.
//
// Returns string which is the message, such as `lexical rules callback ProceduralBlock
// panicked: index out of range`.
func (callbackError *CallbackError) Error() string {
	return fmt.Sprintf("lexical rules callback %s panicked: %v", callbackError.Callback, callbackError.Value)
}

// Unwrap returns ErrRulesCallback, and the panic value when it is an error.
//
// Returns []error which holds ErrRulesCallback, followed by Value when Value is an error.
func (callbackError *CallbackError) Unwrap() []error {
	if wrapped, ok := callbackError.Value.(error); ok {
		return []error{ErrRulesCallback, wrapped}
	}
	return []error{ErrRulesCallback}
}

// Excerpt returns up to maxRunes runes from the start of text, cut on a rune boundary so
// no multi-byte character is split, followed by an ellipsis when text continues past the
// cut.
//
// Each run of invalid UTF-8 is replaced with U+FFFD, so the result always prints cleanly;
// an invalid byte counts as one rune towards maxRunes.
//
// A maxRunes of zero or less keeps no text, so the result is the ellipsis alone, or the
// empty string when text is empty.
//
// Takes text (string) which is the text to quote.
// Takes maxRunes (int) which is the most runes of text to keep.
//
// Returns string which is the excerpt.
func Excerpt(text string, maxRunes int) string {
	if maxRunes <= 0 {
		if text == "" {
			return ""
		}
		return excerptEllipsis
	}
	end := 0
	for range maxRunes {
		if end >= len(text) {
			break
		}
		_, width := utf8.DecodeRuneInString(text[end:])
		end += width
	}
	quoted := strings.ToValidUTF8(text[:end], string(utf8.RuneError))
	if end < len(text) {
		return quoted + excerptEllipsis
	}
	return quoted
}

// lineAndColumn returns the 1-based line and rune column of offset in input. A line break
// is a newline; a carriage return before it belongs to the earlier line.
//
// Takes input (string) which is the text.
// Takes offset (int) which is the byte offset, clamped to the text.
//
// Returns line (int) which is the 1-based line.
// Returns column (int) which is the 1-based column in runes.
func lineAndColumn(input string, offset int) (line, column int) {
	offset = min(max(offset, 0), len(input))
	before := input[:offset]
	line = strings.Count(before, "\n") + 1
	lineStart := strings.LastIndexByte(before, '\n') + 1
	column = utf8.RuneCountInString(before[lineStart:]) + 1
	return line, column
}

// excerpt returns up to excerptRunes runes of input from offset, cut on a rune boundary
// and marked with an ellipsis when cut short. Invalid UTF-8 is replaced so the excerpt
// always prints cleanly.
//
// Takes input (string) which is the text.
// Takes offset (int) which is the byte offset, clamped to the text.
//
// Returns string which is the excerpt.
func excerpt(input string, offset int) string {
	offset = min(max(offset, 0), len(input))
	return Excerpt(input[offset:], excerptRunes)
}

// callPredicate makes one call to a host-supplied Rules callback and turns a panic inside
// it into a *CallbackError. Only the callback runs under the recovery, so a panic in the
// scanner or splitter themselves is not caught here.
//
// Takes name (string) which names the Rules field being called.
// Takes call (func() bool) which calls the callback and returns its answer.
//
// Returns result (bool) which is the callback's answer, or false when it panicked.
// Returns err (error) which is a *CallbackError when the callback panicked.
func callPredicate(name string, call func() bool) (result bool, err error) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		result = false
		err = &CallbackError{Value: recovered, Callback: name}
	}()
	return call(), nil
}

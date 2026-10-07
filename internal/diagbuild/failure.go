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
	"errors"
	"fmt"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/sqllex"
)

// locatedError is a dialect error that says where in the text it read the problem is, as
// dialectkit.ParseError does through its Location method.
//
// Placing errors through the method keeps the diagnostic builders free of the dialect
// tool kit, so the migration run time, which builds diagnostics too, links no part of the
// analyser.
type locatedError interface {
	error

	// Location returns the source text of the token at fault and its byte offset.
	//
	// Returns token (string) which is the token's source text, empty at end of input.
	// Returns position (int) which is the token's byte offset.
	Location() (token string, position int)
}

// Failure turns a dialect's SQL processing error into a Q010 diagnostic, or into a Q070
// diagnostic when the error is a recovered panic.
//
// It uses the error's location when that location matches the source text, and otherwise
// points to the start of the statement.
//
// Takes filename (string) which is the file the text was read from.
// Takes text (string) which is the text the dialect read, whose offsets its error gives.
// Takes startLine (int) which is the file line the text starts on.
// Takes context (string) which says what failed, such as `failed to parse query "Get"`.
// Takes err (error) which is the dialect's error; nil reports the context alone, at the
// start of the statement.
//
// Returns diag.SourceError which is the Q010 or Q070 error.
func Failure(filename string, text string, startLine int, context string, err error) diag.SourceError {
	if err == nil {
		return Block(filename, startLine, diag.CodeParseError, diag.SeverityError, context)
	}
	code := failureCode(err)
	message := context + ": " + failureText(err)
	if start, end, found := failurePlace(text, err); found {
		return Located(filename, SpanOf(text, startLine, start, end), code, diag.SeverityError, message, "")
	}
	return Block(filename, startLine, code, diag.SeverityError, message)
}

// failureCode returns the code a dialect's error is reported with: Q070 for a panic
// recovered from Seagull, the dialect or a host-supplied lexical rules callback, and Q010
// for anything else.
//
// Takes err (error) which is the dialect's error.
//
// Returns string which is diag.CodeRecoveredPanic or diag.CodeParseError.
func failureCode(err error) string {
	if errors.Is(err, fault.ErrPanic) || errors.Is(err, sqllex.ErrRulesCallback) {
		return diag.CodeRecoveredPanic
	}
	return diag.CodeParseError
}

// failurePlace returns where in a text a dialect's error places the problem: the token a
// located error, such as a dialectkit.ParseError, names, when the text holds that token
// at its position, or the rest of the line a sqllex.Error's construct opens on.
//
// Takes text (string) which is the text the dialect read.
// Takes err (error) which is the dialect's error.
//
// Returns start (int) which is the offset the place starts at.
// Returns end (int) which is the offset one past its end.
// Returns found (bool) which is false when the error carries no place in the text.
func failurePlace(text string, err error) (start int, end int, found bool) {
	if located, isLocated := errors.AsType[locatedError](err); isLocated {
		token, position := located.Location()
		if position < 0 || position > len(text) || !strings.HasPrefix(text[position:], token) {
			return 0, 0, false
		}
		return position, position + len(token), true
	}
	if lexicalError, isLexicalError := errors.AsType[*sqllex.Error](err); isLexicalError {
		offset := lexicalError.Offset
		if offset < 0 || offset >= len(text) {
			return 0, 0, false
		}
		line, _, _ := strings.Cut(text[offset:], "\n")
		return offset, offset + len(strings.TrimRight(line, "\r")), true
	}
	return 0, 0, false
}

// failureText returns a dialect's error as a diagnostic message gives it: its own text,
// in which a sqllex.Error, whose text gives the line, column and offset of the problem in
// the text the dialect split, says only what the problem is and the text it starts with.
//
// Takes err (error) which is the dialect's error.
//
// Returns string which is the text.
func failureText(err error) string {
	text := err.Error()
	if lexicalError, isLexicalError := errors.AsType[*sqllex.Error](err); isLexicalError {
		text = strings.Replace(text, lexicalError.Error(), fmt.Sprintf("%v near %q", lexicalError.Err, lexicalError.Excerpt), 1)
	}
	return text
}

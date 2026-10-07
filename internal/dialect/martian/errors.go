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

package martian

import (
	"errors"

	"seagull.sh/seagull/internal/dialect/dialectkit"
)

// errSyntax is the base error of every Martian syntax error, each of which is also a
// dialectkit.ParseError.
var errSyntax = errors.New("martian syntax error")

// syntaxError is a Martian syntax error: a dialectkit.ParseError, which matches the
// generator's parse sentinel and says where in the text the problem is, that also matches
// errSyntax.
type syntaxError struct {
	*dialectkit.ParseError
}

// Unwrap returns errSyntax and the parse error, so the error matches both.
//
// Returns []error which holds errSyntax and the parse error.
func (e *syntaxError) Unwrap() []error {
	return []error{errSyntax, e.ParseError}
}

// newSyntaxError returns the syntax error at the text from start to end of source.
//
// Takes source (string) which is the text the tokeniser read.
// Takes start (int) which is the byte offset the problem starts at.
// Takes end (int) which is the byte offset one past the text the problem names.
// Takes message (string) which says what was wrong.
//
// Returns error which is the located syntax error.
func newSyntaxError(source string, start, end int, message string) error {
	return &syntaxError{ParseError: dialectkit.ParseErrorAt(source, start, end, message)}
}

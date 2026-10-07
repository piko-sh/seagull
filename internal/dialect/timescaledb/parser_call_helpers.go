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

package timescaledb

import (
	"fmt"
	"strings"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/postgres/pgext"
)

// captureRemainingCallArguments reads the rest of a call's arguments up to the call's
// closing parenthesis, which it consumes, and returns them as written, separated by a
// comma and a space.
//
// Each argument is captured as its source text, so literals, quoted identifiers and
// nested calls keep their spelling and the captured text is valid SQL when replayed.
//
// Takes p (pgext.ParserContext) which is positioned at the first of the remaining
// arguments.
// Takes operation (string) which names the call in an error.
// Takes openParen (pgext.Token) which is the call's opening parenthesis, where an
// unterminated call is reported.
//
// Returns string which is the captured arguments.
// Returns error when an argument opens a group it does not close, or the call is not
// closed before the end of the statement.
func captureRemainingCallArguments(p pgext.ParserContext, operation string, openParen pgext.Token) (string, error) {
	var arguments []string
	for {
		argument, err := p.CaptureUntilBoundary()
		if err != nil {
			return "", fmt.Errorf("%s arguments: %w", operation, err)
		}
		if argument != "" {
			arguments = append(arguments, argument)
		}
		if !consumeArgumentSeparator(p) {
			break
		}
	}
	if err := expectCallClose(p, operation, openParen); err != nil {
		return "", err
	}
	return strings.Join(arguments, ", "), nil
}

// expectCallClose consumes the closing `)` of a call, reporting an unterminated call at
// its opening parenthesis when the close is missing so the user can locate it.
//
// Takes p (pgext.ParserContext) which is positioned where the close paren is expected.
// Takes operation (string) which is the canonical function name for the diagnostic.
// Takes openParen (pgext.Token) which is the call's opening parenthesis.
//
// Returns error when the close paren is missing.
func expectCallClose(p pgext.ParserContext, operation string, openParen pgext.Token) error {
	if p.CurrentToken().Kind != pgext.TokenRightParen {
		return dialectkit.NewParseError(openParen, operation+": unterminated call")
	}
	p.Advance()
	return nil
}

// expectCallEnd requires the end of the statement after a call's closing parenthesis, so
// a statement such as SELECT add_retention_policy(...) FROM t, which may run the call any
// number of times, is refused rather than read as the call alone.
//
// Takes p (pgext.ParserContext) which is positioned just after the call.
// Takes operation (string) which is the canonical function name for the diagnostic.
//
// Returns error which is a dialectkit.ParseError at the first token after the call, or
// nil at the end of the statement.
func expectCallEnd(p pgext.ParserContext, operation string) error {
	if p.AtEnd() {
		return nil
	}
	return dialectkit.NewParseError(p.CurrentToken(), fmt.Sprintf("expected the end of the statement after the %s call", operation))
}

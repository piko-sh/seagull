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

package clickhouse

// statementTokens starts reading the tokens of one statement of a script, as
// dialectkit.LexStatements asks a dialect's statement tokeniser to. Token positions are
// byte offsets into the script, so a statement cut from it keeps its place in it.
//
// Takes input (string) which is the whole script.
// Takes start (int) which is the byte offset of the statement's first byte.
// Takes end (int) which is the byte offset one past its last byte.
//
// Returns func() (token, error) which reads the statement's tokens in order, then a
// tokenEOF token at the end of the statement.
func statementTokens(input string, start, end int) func() (token, error) {
	lexer := &tokeniser{input: input[:end], position: start, previous: tokenEOF, hasPrevious: false}
	return lexer.read
}

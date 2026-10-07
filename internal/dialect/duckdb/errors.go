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

package duckdb

import (
	"errors"
	"fmt"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

// maxLimitErrorTokenRunes is the most runes of the token a limitError quotes in its
// message, so a limit reached at a huge token gives a short message.
const maxLimitErrorTokenRunes = 32

var (
	// errPivotColumnLimit reports a PIVOT whose IN lists and aggregates make more output
	// columns than the dialect's limit. It matches the generator's limit sentinel.
	errPivotColumnLimit = dialectkit.LimitSentinel("a PIVOT makes more output columns than the limit allows")

	// errTypeDepth reports a written type that nests sqlmodel.MaxTypeDepth levels or more,
	// deeper than the analyser and the code generators read a type. It matches the
	// generator's limit sentinel.
	errTypeDepth = dialectkit.LimitSentinel("a type nests deeper than Seagull reads")

	// errUnanalysable marks SQL DuckDB runs whose output columns cannot be known when
	// generating code, such as a PIVOT whose columns come from the data, so a view or table
	// macro body holding it is recorded as not analysed rather than failing its migration.
	errUnanalysable = errors.New("the output columns cannot be known when generating code")
)

// limitError is a limit of the dialect's own that a statement goes past, placed at the
// token where it does. It matches its sentinel, and so the generator's limit sentinel,
// with errors.Is, and gives its place through Location as a dialectkit.ParseError does.
type limitError struct {
	// sentinel is the limit's sentinel.
	sentinel error

	// token is the source text of the token the limit is reached at.
	token string

	// detail gives the numbers, such as "810000 columns, limit 4096".
	detail string

	// position is the byte offset of the token.
	position int
}

// Error formats the error as the sentinel's text, the token it is at and the numbers.
//
// Returns string which is the formatted error.
func (e *limitError) Error() string {
	return fmt.Sprintf("%v near %q: %s", e.sentinel, sqllex.Excerpt(e.token, maxLimitErrorTokenRunes), e.detail)
}

// Unwrap returns the limit's sentinel.
//
// Returns error which is the sentinel.
func (e *limitError) Unwrap() error {
	return e.sentinel
}

// Location returns the source token and byte offset used to place the error in a
// diagnostic.
//
// Returns token (string) which is the token's source text.
// Returns position (int) which is the token's byte offset.
func (e *limitError) Location() (token string, position int) {
	return e.token, e.position
}

// unanalysableError is a syntax error at SQL DuckDB runs whose output columns cannot be
// known when generating code. It matches errUnanalysable as well as the generator's parse
// sentinel, so a view or table macro body holding it is recorded as not analysed while a
// query holding it fails.
type unanalysableError struct {
	// ParseError places the error and says what cannot be known.
	*dialectkit.ParseError
}

// Unwrap returns the parse error and errUnanalysable.
//
// Returns []error which holds both.
func (e *unanalysableError) Unwrap() []error {
	return []error{e.ParseError, errUnanalysable}
}

// newLimitError returns a limitError at a token.
//
// Takes sentinel (error) which is the limit's sentinel.
// Takes at (token) which is the token the limit is reached at.
// Takes detail (string) which gives the numbers.
//
// Returns error which is the located limit error.
func newLimitError(sentinel error, at token, detail string) error {
	return &limitError{sentinel: sentinel, token: at.Raw, detail: detail, position: at.Start}
}

// newUnanalysableError returns an unanalysableError at a token.
//
// Takes at (token) which is the token the SQL that cannot be analysed starts at.
// Takes message (string) which says why and what to write instead.
//
// Returns error which is the located error.
func newUnanalysableError(at token, message string) error {
	return &unanalysableError{ParseError: dialectkit.NewParseError(at, message)}
}

// typeDepthError returns the limit error for a type nesting sqlmodel.MaxTypeDepth levels
// or more, placed at a token of the type.
//
// Takes at (token) which is the token the type, or its part past the limit, starts at.
//
// Returns error which is the located limit error matching errTypeDepth.
func typeDepthError(at token) error {
	return newLimitError(errTypeDepth, at, fmt.Sprintf("%d levels or more", sqlmodel.MaxTypeDepth))
}

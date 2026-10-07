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

package resolve

import (
	"context"
	"errors"
	"fmt"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/fault"
)

var (
	// ErrScopeChainDepthExceeded is returned by the qualified and unqualified resolvers when
	// a lookup visits maxScopeChainDepth scopes of the parent chain without finding the
	// name. It is reported under diag.CodeDepthLimit, so the user learns the lookup stopped
	// at the depth limit rather than that the column does not exist.
	ErrScopeChainDepthExceeded error = depthLimitError("a column was looked up through more than %d enclosing scopes, "+
		"so the lookup stopped", maxScopeChainDepth-1)
)

// Span locates a resolution problem in the source of the statement being analysed, as
// byte offsets into the SQL the dialect parsed. The zero span means the dialect reported
// no position, and the analyser then reports the problem on the statement.
type Span struct {
	// Start is the byte offset of the first byte.
	Start int

	// End is the byte offset one past the last byte; zero when the span is unknown.
	End int
}

// Known reports whether the span locates anything.
//
// Returns bool which is true when the span covers at least one byte.
func (span Span) Known() bool {
	return span.End > span.Start
}

// Error describes a query-resolution problem for the analyser to turn into a diagnostic.
type Error struct {
	// Code is the diagnostic code, such as diag.CodeUnknownColumn.
	Code string

	// Message describes the problem without its code.
	Message string

	// Suggestion is the name the query most likely meant, or empty.
	Suggestion string

	// Span locates the problem in the statement, or is zero when unknown.
	Span Span

	// Severity is how serious the problem is.
	Severity diag.ErrorSeverity
}

// newError creates a warning with a code and a formatted message.
//
// Takes code (string) which is the diagnostic code.
// Takes format (string) which is the message format.
// Takes arguments (...any) which fill the format.
//
// Returns *Error which is the warning.
func newError(code string, format string, arguments ...any) *Error {
	return &Error{
		Code:       code,
		Message:    fmt.Sprintf(format, arguments...),
		Suggestion: "",
		Span:       Span{Start: 0, End: 0},
		Severity:   diag.SeverityWarning,
	}
}

// Error returns the message, without the code.
//
// Returns string which is the message.
func (e *Error) Error() string {
	return e.Message
}

// withSuggestion records the name the query most likely meant.
//
// Takes suggestion (string) which is the name, or empty for none.
//
// Returns *Error which is the error, for chaining.
func (e *Error) withSuggestion(suggestion string) *Error {
	e.Suggestion = suggestion
	return e
}

// withSeverity sets how serious the problem is.
//
// Takes severity (diag.ErrorSeverity) which is the severity.
//
// Returns *Error which is the error, for chaining.
func (e *Error) withSeverity(severity diag.ErrorSeverity) *Error {
	e.Severity = severity
	return e
}

// ExtractErrorCode returns the diagnostic code a resolution failure carries: the code of
// the Error it is or wraps, or the expression-type code for a failure that carries none.
//
// Takes err (error) which is the failure.
//
// Returns string which is the code.
func ExtractErrorCode(err error) string {
	return asError(err).Code
}

// depthLimitError creates the warning for a part of a statement the analysis did not
// follow because it is nested more deeply than a fixed limit allows.
//
// Takes format (string) which is the message format.
// Takes arguments (...any) which fill the format.
//
// Returns *Error which is the warning, under diag.CodeDepthLimit.
func depthLimitError(format string, arguments ...any) *Error {
	return newError(diag.CodeDepthLimit, format, arguments...)
}

// asError returns a failure as a resolution Error: the Error it is or wraps, or, for a
// failure that carries no code, one reported as an expression that could not be typed.
//
// Takes err (error) which is the failure.
//
// Returns *Error which is the resolution error.
func asError(err error) *Error {
	if resolveError, ok := errors.AsType[*Error](err); ok {
		return resolveError
	}
	return newError(diag.CodeExpressionTypeError, "%s", err.Error())
}

// locatedAt returns a failure placed at a span the dialect recorded, when it has no place
// of its own. The failure is copied, never changed, as some are shared.
//
// Takes err (error) which is the failure; nil stays nil.
// Takes span (ast.Span) which is where the expression that failed is written.
//
// Returns error which is the failure, placed where it could be.
func locatedAt(err error, span ast.Span) error {
	if err == nil || !span.Known() || interrupted(err) {
		return err
	}
	problem := asError(err)
	if problem.Span.Known() {
		return err
	}
	located := *problem
	located.Span = Span{Start: span.Start, End: span.End}
	return &located
}

// interrupted reports whether a failure is a context being cancelled or running out of
// time, which stops the analysis rather than being reported as a problem with the query.
//
// Takes err (error) which is the failure; nil is no interruption.
//
// Returns bool which is true for an interruption.
func interrupted(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// contextFailure returns why a context is done: its error, joined by its cause when the
// cause says more.
//
// Returns error which matches the context's error and its cause, or nil while the context
// is not done.
func contextFailure(ctx context.Context) error {
	err := ctx.Err()
	if err == nil {
		return nil
	}
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, err) {
		return fmt.Errorf("%w: %w", err, cause)
	}
	return err
}

// engineFailure sorts the error a dialect returned when the analyser asked it to resolve
// part of a query.
//
// An error that says a context was cancelled or ran out of time, or any error once the
// analysis's own context is done, is returned so the analysis stops. A panic the dialect
// recovered is a problem to report where the query asks for what failed. Any other error
// means the dialect has no opinion, and the analyser carries on without its answer.
//
// Takes err (error) which is the dialect's error; nil means it returned none.
// Takes subject (string) which names what the dialect was asked to resolve, such as
// `function "lower"`.
//
// Returns *Error which is the Q070 error for a recovered panic, or nil.
// Returns error which is the interruption that stops the analysis, or nil.
func engineFailure(ctx context.Context, err error, subject string) (*Error, error) {
	switch {
	case err == nil:
		return nil, nil
	case interrupted(err):
		return nil, fmt.Errorf("the dialect stopped resolving %s: %w", subject, err)
	case ctx.Err() != nil:
		return nil, fmt.Errorf("the dialect stopped resolving %s: %w", subject, contextFailure(ctx))
	case errors.Is(err, fault.ErrPanic):
		return newError(diag.CodeRecoveredPanic, "the dialect failed to resolve %s: %v", subject, err).
			withSeverity(diag.SeverityError), nil
	default:
		return nil, nil
	}
}

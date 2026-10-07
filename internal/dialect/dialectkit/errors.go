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
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/sqllex"
)

// maxParseErrorTokenRunes is the most runes of the token a ParseError quotes in its
// message, so a syntax error at a huge literal gives a short message.
const maxParseErrorTokenRunes = 32

// ParseError describes a syntax error at a token in a statement.
//
// It wraps the parse sentinel for errors.Is. The generator uses the token's location to
// report the file's line and column.
type ParseError struct {
	// Token is the source text of the token the error is at, or empty at end of input.
	Token string

	// Message says what was wrong, such as "expected keyword FROM".
	Message string

	// Position is the byte offset of the token in the text the dialect tokenised.
	Position int
}

// NewParseError returns a ParseError at token.
//
// Takes token (Token[K]) which is the token the error is at; an empty Raw text marks end
// of input.
// Takes message (string) which says what was wrong.
//
// Returns *ParseError which locates the error.
func NewParseError[K comparable](token Token[K], message string) *ParseError {
	return &ParseError{Token: token.Raw, Message: message, Position: token.Start}
}

// Error formats the error as "syntax error near "token": message", or "syntax error at
// end of input: message" when there is no token. A token longer than
// maxParseErrorTokenRunes runes is cut to that many and ends in an ellipsis; Location
// still gives the whole token.
//
// Returns string which is the formatted error.
func (e *ParseError) Error() string {
	if e.Token == "" {
		return "syntax error at end of input: " + e.Message
	}
	return fmt.Sprintf("syntax error near %q: %s", sqllex.Excerpt(e.Token, maxParseErrorTokenRunes), e.Message)
}

// Unwrap returns the generator's parse sentinel.
//
// Returns error which is the parse sentinel.
func (*ParseError) Unwrap() error {
	return fault.ErrParse
}

// Location returns the source token and byte offset used to locate the error in a
// diagnostic.
//
// Returns token (string) which is the token's source text, empty at end of input.
// Returns position (int) which is the token's byte offset.
func (e *ParseError) Location() (token string, position int) {
	return e.Token, e.Position
}

// PanicError is a panic recovered while a dialect parsed or analysed a statement,
// returned as an error so a parser bug fails one statement rather than the run.
//
// It matches the generator's panic sentinel with errors.Is, and also the panic value when
// that value is an error. It holds no stack: RecoveredError logs the stack once, where
// the panic was recovered.
type PanicError struct {
	// Value is the value the panic was raised with.
	Value any

	// Operation names what was running, such as "postgres: analyse query".
	Operation string
}

// Error formats the error as "operation: panic: value".
//
// Returns string which is the formatted error.
func (e *PanicError) Error() string {
	return fmt.Sprintf("%s: panic: %v", e.Operation, e.Value)
}

// Unwrap returns the generator's panic sentinel, followed by the panic value when it is
// itself an error.
//
// Returns []error which holds the panic sentinel and, when the panic value is an error,
// that value.
func (e *PanicError) Unwrap() []error {
	if wrapped, ok := e.Value.(error); ok {
		return []error{fault.ErrPanic, wrapped}
	}
	return []error{fault.ErrPanic}
}

// limitError is a limit sentinel whose message stands alone but which still matches the
// generator's limit sentinel with errors.Is.
type limitError struct {
	// parent is the generator's limit sentinel.
	parent error

	// message is the sentinel's text.
	message string
}

var (
	// ErrParseDepth is returned when nesting in a statement, such as subqueries or
	// parentheses, goes deeper than the dialect's parse depth limit. It wraps the
	// generator's limit sentinel, so errors.Is matches both.
	ErrParseDepth error = &limitError{message: "statement nests deeper than the parse depth limit", parent: fault.ErrLimit}

	// ErrTokenBudget is returned when a statement holds more tokens than the dialect's
	// per-statement budget. It wraps the generator's limit sentinel, so errors.Is matches
	// both.
	ErrTokenBudget error = &limitError{message: "statement exceeds the per-statement token budget", parent: fault.ErrLimit}

	// ErrWalkBudget reports a parser that read its statement's tokens far more often than a
	// parse of it can need, as a loop that has stopped advancing through the tokens does, so
	// the statement is refused rather than analysed forever. It wraps the generator's limit
	// sentinel, so errors.Is matches both.
	ErrWalkBudget error = &limitError{message: "the parser stopped advancing through the statement", parent: fault.ErrLimit}

	// ErrArmExtent is returned by SplitCompoundArms when the marks of a compound query's
	// arms do not fit what was gathered: a mark beyond the end of a list, an arm that ends
	// before it starts, or an arm that starts before the one before it ends.
	ErrArmExtent = errors.New("compound query arm extents do not fit what was gathered")
)

// Error returns the sentinel's message.
//
// Returns string which is the message.
func (e *limitError) Error() string {
	return e.message
}

// Unwrap returns the generator's limit sentinel.
//
// Returns error which is the parent sentinel.
func (e *limitError) Unwrap() error {
	return e.parent
}

// LimitSentinel returns a new sentinel for a limit of the dialect's own, whose message
// stands alone but which matches the generator's limit sentinel with errors.Is, as
// ErrParseDepth and ErrTokenBudget do.
//
// Takes message (string) which is the sentinel's text, such as "type name nests too
// deeply".
//
// Returns error which is the sentinel; each call returns a distinct one.
func LimitSentinel(message string) error {
	return &limitError{message: message, parent: fault.ErrLimit}
}

// TokenBudgetExceeded reports a statement holding more tokens than the per-statement
// budget allows, when its tokenising stopped at the budget and so its full count is not
// known. It wraps ErrTokenBudget.
//
// Takes limit (int) which is the per-statement budget.
//
// Returns error which names the budget.
func TokenBudgetExceeded(limit int) error {
	return fmt.Errorf("%w: more than %d tokens", ErrTokenBudget, limit)
}

// DepthError reports nesting beyond the parse depth limit, wrapping ErrParseDepth.
//
// Takes limit (int) which is the depth limit that was exceeded.
//
// Returns error which names the limit.
func DepthError(limit int) error {
	return fmt.Errorf("%w (limit %d)", ErrParseDepth, limit)
}

// CheckTokenBudget reports a statement whose token count exceeds limit, wrapping
// ErrTokenBudget.
//
// Takes count (int) which is the statement's token count.
// Takes limit (int) which is the per-statement budget.
//
// Returns error which names both numbers, or nil when the statement fits.
func CheckTokenBudget(count, limit int) error {
	if count <= limit {
		return nil
	}
	return fmt.Errorf("%w: %d tokens, limit %d", ErrTokenBudget, count, limit)
}

// UnboundParameterError returns a ParseError at a parameter written in a statement the
// dialect runs without analysing, such as SET or EXPLAIN, which Seagull therefore cannot
// bind, so the generated code would send the statement without its value.
//
// Takes parameter (Token[K]) which is the parameter's token.
// Takes verb (string) which is the word the statement opens with, upper-cased.
// Takes hint (string) which says what to write instead, or is empty.
//
// Returns *ParseError which names the statement and gives the hint.
func UnboundParameterError[K comparable](parameter Token[K], verb, hint string) *ParseError {
	message := "no parameter can be bound in " + verb + " statements, which run without analysis"
	if hint != "" {
		message += "; " + hint
	}
	return NewParseError(parameter, message)
}

// ParseErrorAt creates a syntax error for a location the tokeniser cannot read.
//
// The span is bounded by the source and adjusted to whole characters. A location at the
// text's end identifies end of input.
//
// Takes source (string) which is the text the tokeniser reads.
// Takes start (int) which is the byte offset of the place.
// Takes end (int) which is the byte offset where the text the error names ends.
// Takes message (string) which says what was wrong, such as "unexpected character".
//
// Returns *ParseError which locates the error.
func ParseErrorAt(source string, start, end int, message string) *ParseError {
	start = max(start, 0)
	if start >= len(source) {
		return &ParseError{Token: "", Message: message, Position: start}
	}
	if end <= start {
		_, width := utf8.DecodeRuneInString(source[start:])
		end = start + width
	}
	end = min(end, len(source))
	for end < len(source) && !utf8.RuneStart(source[end]) {
		end++
	}
	return &ParseError{Token: source[start:end], Message: message, Position: start}
}

// Recover turns a panic in a dialect entry point into the error the entry point returns.
//
// An entry point defers a function literal that calls recover itself and passes the
// result straight in, because recover stops a panic only when the deferred function calls
// it directly:
//
//	defer func() {
//		dialectkit.Recover(ctx, "postgres: analyse query", recover(), &analysis, &err)
//	}()
//
// On a panic it sets *result to its zero value and *err to what RecoveredError returns.
// Without a panic it changes nothing.
//
// Takes operation (string) which names what was running.
// Takes recovered (any) which is the value recover returned, nil when nothing panicked.
// Takes result (*T) which is the entry point's named result, or nil when it has none
// besides its error.
// Takes err (*error) which is the entry point's named error result. When it is nil there
// is nowhere to report the panic, so the panic goes on.
func Recover[T any](ctx context.Context, operation string, recovered any, result *T, err *error) {
	if recovered == nil {
		return
	}
	if err == nil {
		panic(recovered)
	}
	if result != nil {
		var zero T
		*result = zero
	}
	*err = RecoveredError(ctx, operation, recovered)
}

// RecoveredError converts a recovered panic into a returned error.
//
// An interrupt, which a Cursor raises when its Interrupt stops a walk, is returned as the
// Interrupt's error, such as the context's cause or ErrWalkBudget, and nothing is logged.
// Any other value is logged once, with the stack, through the context's logger, and
// returned as a *PanicError that holds no stack. Called from the deferred function that
// recovered, the logged stack still holds the frames that panicked.
//
// Takes operation (string) which names what was running.
// Takes recovered (any) which is the value recover returned.
//
// Returns error which is the Interrupt's error, or else the *PanicError.
func RecoveredError(ctx context.Context, operation string, recovered any) error {
	if interrupted, ok := recovered.(walkInterrupted); ok && interrupted.err != nil {
		return interrupted.err
	}
	logging.LogRecoveredPanic(ctx, operation, recovered)
	return &PanicError{Value: recovered, Operation: operation}
}

// IsInterrupt reports whether a recovered value is the interrupt a Cursor raises when its
// Interrupt stops a walk, rather than a fault.
//
// Takes recovered (any) which is the value recover returned.
//
// Returns bool which is true for an interrupt that carries an error.
func IsInterrupt(recovered any) bool {
	interrupted, ok := recovered.(walkInterrupted)
	return ok && interrupted.err != nil
}

// ReportEndAt moves an end-of-input parse error to the token following a parsed fragment.
//
// Other errors and actual end-of-input tokens are left unchanged.
//
// Takes err (error) which is the error the parser reported, or nil.
// Takes next (Token[K]) which is the token that follows the part in the statement.
//
// Returns error which is the error, at next when it was at the end of input.
func ReportEndAt[K comparable](err error, next Token[K]) error {
	parseError, ok := errors.AsType[*ParseError](err)
	if !ok || parseError.Token != "" || next.Raw == "" {
		return err
	}
	return NewParseError(next, parseError.Message)
}

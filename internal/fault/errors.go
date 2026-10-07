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

package fault

import (
	"errors"

	"seagull.sh/seagull/internal/diag"
)

// childError is a sentinel whose message stands alone but which still matches its parent
// sentinel with errors.Is.
type childError struct {
	// parent is the broader sentinel this one refines.
	parent error

	// message is the error text.
	message string
}

var (
	// ErrInvalidConfig is the base sentinel for a generator configuration the generator
	// cannot use.
	ErrInvalidConfig = errors.New("invalid configuration")

	// ErrLimit is the base sentinel for input that exceeds a configured limit.
	ErrLimit = errors.New("limit exceeded")

	// ErrParse is the base sentinel for SQL that cannot be parsed.
	ErrParse = errors.New("parse error")

	// ErrPanic is the base sentinel for a panic recovered from code the generator or the
	// migration runner called. The panic's stack is logged where it was recovered and is
	// never part of the error.
	ErrPanic = errors.New("recovered a panic")

	// ErrDiagnostics is the base sentinel for a run that reported diagnostics of error
	// severity. The generator's diagnostics error wraps it, so callers detect such a run
	// with errors.Is.
	ErrDiagnostics = errors.New("generation reported error diagnostics")

	// ErrInvalidPrefix is returned when a configured prefix, header or naming value has the
	// wrong shape or collides with another. It wraps ErrInvalidConfig.
	ErrInvalidPrefix error = &childError{message: "invalid prefix", parent: ErrInvalidConfig}

	// ErrNoSource is returned when generation has nothing to read: no dialect, or no
	// migrations or schema source to build the catalogue from. It wraps ErrInvalidConfig.
	ErrNoSource error = &childError{message: "no source to generate from", parent: ErrInvalidConfig}

	// ErrFileTooLarge is returned when a migration, query or seed file exceeds the
	// configured size limit. It wraps ErrLimit.
	ErrFileTooLarge error = &childError{message: "file exceeds the configured size limit", parent: ErrLimit}

	// ErrMalformedSQL is returned when SQL holds an unterminated string literal,
	// dollar-quoted block or block comment, or an unbalanced BEGIN or CASE block. It wraps
	// ErrParse.
	ErrMalformedSQL error = &childError{message: "malformed SQL statement", parent: ErrParse}
)

// Error returns the sentinel's message.
//
// Returns string which is the message without the parent's text.
func (e *childError) Error() string {
	return e.message
}

// Unwrap returns the parent sentinel.
//
// Returns error which is the parent sentinel.
func (e *childError) Unwrap() error {
	return e.parent
}

// HasErrors reports whether any diagnostic has error severity.
//
// Takes diagnostics ([]diag.SourceError) which holds the diagnostics to check.
//
// Returns bool which is true when at least one diagnostic is an error.
func HasErrors(diagnostics []diag.SourceError) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == diag.SeverityError {
			return true
		}
	}
	return false
}

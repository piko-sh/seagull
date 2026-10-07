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

package seagull

import "seagull.sh/seagull/internal/fault"

var (
	// ErrInvalidConfig is the base error for a generator configuration that cannot be used.
	ErrInvalidConfig = fault.ErrInvalidConfig

	// ErrLimit is the base error for input that exceeds a configured limit.
	ErrLimit = fault.ErrLimit

	// ErrParse is the base error for SQL that cannot be parsed. Generate and Vet report such
	// SQL as a Q010 diagnostic, so the *DiagnosticsError that Result.Err and Report.Err
	// return matches it.
	ErrParse = fault.ErrParse

	// ErrPanic is the base error for a panic Seagull recovered from code it called, such as
	// a dialect or a host callback, so the run fails with an error instead of crashing.
	//
	// The panic's stack is logged once at warning level through the context's logger and is
	// not part of the error. When the panic value was itself an error, the returned error
	// matches that value too.
	ErrPanic = fault.ErrPanic

	// ErrDiagnostics is the error a DiagnosticsError wraps, so callers can detect
	// error-severity diagnostics with errors.Is.
	ErrDiagnostics = fault.ErrDiagnostics

	// ErrNoSource is returned when a required source is missing: the dialect, the migration
	// or query file system, or the catalogue returned by a schema source.
	//
	// Empty query directories are allowed. It wraps ErrInvalidConfig.
	ErrNoSource = fault.ErrNoSource

	// ErrInvalidPrefix is returned when a directive, helper or error prefix has the wrong
	// shape or a directive prefix repeats. It wraps ErrInvalidConfig.
	ErrInvalidPrefix = fault.ErrInvalidPrefix

	// ErrFileTooLarge is returned when a migration or query file exceeds the size limit set
	// with WithMaxFileBytes. It wraps ErrLimit.
	ErrFileTooLarge = fault.ErrFileTooLarge

	// ErrMalformedSQL is returned by the migration runner of sdk/migrate when SQL holds an
	// unterminated string literal, dollar-quoted block or block comment, or an unbalanced
	// BEGIN or CASE block.
	//
	// The generator reports the same problem as a Q010 diagnostic. It wraps ErrParse.
	ErrMalformedSQL = fault.ErrMalformedSQL
)

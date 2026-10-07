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

package diag

import (
	"errors"
	"fmt"
	"strconv"
)

const (
	// SeverityError indicates a fatal error that prevents code generation.
	SeverityError ErrorSeverity = iota

	// SeverityWarning indicates a potential problem that does not block generation.
	SeverityWarning

	// SeverityHint indicates a suggestion for improvement.
	SeverityHint
)

var (
	// errUnknownSeverity is returned when a severity has no name: an undeclared value to
	// marshal, or text that names no severity to unmarshal.
	errUnknownSeverity = errors.New("diag: unknown severity")

	// severityNames names each declared severity, in value order.
	severityNames = [...]string{
		SeverityError:   "error",
		SeverityWarning: "warning",
		SeverityHint:    "hint",
	}
)

// SourceError is a diagnostic error or warning mapped back to the source SQL file with
// line and column information.
type SourceError struct {
	// Filename is the source SQL file path.
	Filename string

	// Message describes the error.
	Message string

	// Code is a stable error code for documentation and suppression (e.g. "Q001" for unknown
	// column).
	Code string

	// Suggestion is an optional fix suggestion.
	Suggestion string

	// Line is the one-based line number.
	Line int

	// Column is the one-based column within Line, counted in bytes of the line's UTF-8 text,
	// not in runes or UTF-16 code units: on a line with non-ASCII text, an editor that
	// counts characters converts it.
	Column int

	// EndLine is the one-based end line number, if the error spans a range.
	EndLine int

	// EndColumn is the one-based column within EndLine just past the span's last byte,
	// counted in bytes as Column is.
	EndColumn int

	// Severity indicates whether this is an error, warning, or hint.
	Severity ErrorSeverity
}

// ErrorSeverity classifies diagnostic severity. It travels in JSON and other text
// encodings by name: error, warning or hint.
type ErrorSeverity uint8

// String returns the severity's name: error, warning or hint.
//
// Returns string which is the name, or ErrorSeverity(n) for an undeclared value.
func (severity ErrorSeverity) String() string {
	if int(severity) < len(severityNames) {
		return severityNames[severity]
	}
	return "ErrorSeverity(" + strconv.Itoa(int(severity)) + ")"
}

// MarshalText encodes the severity as its name, so JSON and other text encodings carry
// error, warning or hint rather than a number.
//
// Returns []byte which is the name.
// Returns error which wraps errUnknownSeverity for an undeclared value.
func (severity ErrorSeverity) MarshalText() ([]byte, error) {
	if int(severity) < len(severityNames) {
		return []byte(severityNames[severity]), nil
	}
	return nil, fmt.Errorf("%w: %d", errUnknownSeverity, uint8(severity))
}

// UnmarshalText decodes a severity from its name: error, warning or hint, exactly as
// MarshalText writes them.
//
// Takes text ([]byte) which is the name.
//
// Returns error which wraps errUnknownSeverity when the text names no severity; the
// severity is then unchanged.
func (severity *ErrorSeverity) UnmarshalText(text []byte) error {
	for value, name := range severityNames {
		if string(text) == name {
			*severity = ErrorSeverity(value)
			return nil
		}
	}
	return fmt.Errorf("%w: %q", errUnknownSeverity, text)
}

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

package app

import (
	"fmt"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/fault"
)

// DiagnosticsError reports that a run produced diagnostics of error severity. Result.Err
// and Report.Err return one; it matches ErrDiagnostics with errors.Is, and ErrParse too
// when SQL could not be parsed (an error-severity Q010).
type DiagnosticsError struct {
	// Diagnostics holds every diagnostic of the run, including those below error severity.
	Diagnostics []diag.SourceError
}

// Error summarises the error-severity diagnostics: their count and the first one's
// location, code and message.
//
// Returns string which is the summary, or the text of ErrDiagnostics when no diagnostic
// has error severity.
func (e *DiagnosticsError) Error() string {
	errorCount := 0
	var first *diag.SourceError
	for index := range e.Diagnostics {
		if e.Diagnostics[index].Severity != diag.SeverityError {
			continue
		}
		errorCount++
		if first == nil {
			first = &e.Diagnostics[index]
		}
	}
	if first == nil {
		return fault.ErrDiagnostics.Error()
	}
	var builder strings.Builder
	_, _ = fmt.Fprintf(&builder, "%d error diagnostic", errorCount)
	if errorCount != 1 {
		builder.WriteString("s")
	}
	builder.WriteString(": ")
	if first.Filename != "" {
		_, _ = fmt.Fprintf(&builder, "%s:%d:%d: ", first.Filename, first.Line, first.Column)
	}
	if first.Code != "" {
		_, _ = fmt.Fprintf(&builder, "[%s] ", first.Code)
	}
	builder.WriteString(first.Message)
	return builder.String()
}

// Unwrap returns ErrDiagnostics.
//
// Returns error which is ErrDiagnostics.
func (*DiagnosticsError) Unwrap() error {
	return fault.ErrDiagnostics
}

// Is reports whether the error matches ErrParse: it does when an error-severity
// diagnostic says SQL could not be parsed (Q010). ErrDiagnostics is matched through
// Unwrap.
//
// Takes target (error) which is the error compared against.
//
// Returns bool which is true when target is ErrParse and a parse error was reported.
func (e *DiagnosticsError) Is(target error) bool {
	if target != fault.ErrParse {
		return false
	}
	for index := range e.Diagnostics {
		if e.Diagnostics[index].Severity == diag.SeverityError && e.Diagnostics[index].Code == diag.CodeParseError {
			return true
		}
	}
	return false
}

// diagnosticsErr returns a *DiagnosticsError holding the diagnostics when any has error
// severity, and nil otherwise.
//
// Takes diagnostics ([]diag.SourceError) which holds the diagnostics of a run.
//
// Returns error which is a *DiagnosticsError, or nil.
func diagnosticsErr(diagnostics []diag.SourceError) error {
	if !fault.HasErrors(diagnostics) {
		return nil
	}
	return &DiagnosticsError{Diagnostics: diagnostics}
}

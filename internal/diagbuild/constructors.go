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

package diagbuild

import (
	"seagull.sh/seagull/internal/diag"
)

// Block constructs a SourceError positioned at the start of a query block.
//
// Takes filename (string) which specifies the source file path.
// Takes line (int) which specifies the line number within the file.
// Takes code (string) which specifies the diagnostic error code.
// Takes severity (diag.ErrorSeverity) which specifies the error severity level.
// Takes message (string) which specifies the human-readable error message.
//
// Returns diag.SourceError which holds the constructed source error.
func Block(filename string, line int, code string, severity diag.ErrorSeverity, message string) diag.SourceError {
	return BlockWithSuggestion(filename, line, code, severity, message, "")
}

// BlockWithSuggestion constructs a SourceError positioned at the start of a line, such as
// a query block's header or a file's first line, with a suggestion of how to fix it.
//
// Takes filename (string) which specifies the source file path.
// Takes line (int) which specifies the line number within the file.
// Takes code (string) which specifies the diagnostic error code.
// Takes severity (diag.ErrorSeverity) which specifies the error severity level.
// Takes message (string) which specifies the human-readable error message.
// Takes suggestion (string) which says how to fix the problem, or is empty.
//
// Returns diag.SourceError which holds the constructed source error.
func BlockWithSuggestion(filename string, line int, code string, severity diag.ErrorSeverity, message string, suggestion string) diag.SourceError {
	return diag.SourceError{
		Filename:   filename,
		Line:       line,
		Column:     1,
		Message:    message,
		Severity:   severity,
		Code:       code,
		Suggestion: suggestion,
		EndLine:    0,
		EndColumn:  0,
	}
}

// Unlocated constructs a SourceError without a file location, for diagnostics whose
// caller will assign a source position.
//
// Takes code (string) which specifies the diagnostic error code.
//
// Takes severity (diag.ErrorSeverity) which specifies the error severity level.
//
// Takes message (string) which specifies the human-readable error message.
//
// Returns diag.SourceError which holds the constructed source error.
func Unlocated(code string, severity diag.ErrorSeverity, message string) diag.SourceError {
	return diag.SourceError{
		Filename:   "",
		Line:       0,
		Column:     0,
		Message:    message,
		Severity:   severity,
		Code:       code,
		Suggestion: "",
		EndLine:    0,
		EndColumn:  0,
	}
}

// Located constructs a SourceError at a span of a file, with an optional suggestion of
// the name the source most likely meant.
//
// Takes filename (string) which specifies the source file path.
// Takes span (TextSpan) which locates the problem.
// Takes code (string) which specifies the diagnostic error code.
// Takes severity (diag.ErrorSeverity) which specifies the error severity level.
// Takes message (string) which specifies the human-readable error message.
// Takes suggestion (string) which is the suggested name, or empty.
//
// Returns diag.SourceError which holds the constructed source error.
func Located(filename string, span TextSpan, code string, severity diag.ErrorSeverity, message string, suggestion string) diag.SourceError {
	return diag.SourceError{
		Filename:   filename,
		Line:       span.Line,
		Column:     span.Column,
		Message:    message,
		Severity:   severity,
		Code:       code,
		Suggestion: suggestion,
		EndLine:    span.EndLine,
		EndColumn:  span.EndColumn,
	}
}

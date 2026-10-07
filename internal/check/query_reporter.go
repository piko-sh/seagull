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

package check

import (
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect/ast"
)

// queryReporter places the problems a pass finds in one query block, at the place the
// dialect recorded for each where it lies in the block's text and on the query otherwise.
// It keeps the first MaxDiagnosticsPerQuery problems and counts the rest.
type queryReporter struct {
	// limiter keeps the problems found, up to MaxDiagnosticsPerQuery, and counts the rest.
	limiter *diagbuild.Limiter

	// lines places the dialect's offsets in the query file.
	lines *SourceLines

	// filename is the query file.
	filename string

	// sql is the query block's text, which the dialect's offsets point into.
	sql string

	// line is the file line the query block starts on.
	line int
}

// newQueryReporter returns the reporter of one query block's problems.
//
// Takes filename (string) which is the query file.
// Takes sql (string) which is the query block's text.
// Takes line (int) which is the file line the block starts on.
//
// Returns *queryReporter which has reported nothing yet.
func newQueryReporter(filename, sql string, line int) *queryReporter {
	return &queryReporter{
		limiter:  diagbuild.NewLimiter(MaxDiagnosticsPerQuery),
		lines:    NewSourceLines(sql, line),
		filename: filename,
		sql:      sql,
		line:     line,
	}
}

// add records a problem. Once MaxDiagnosticsPerQuery problems are kept, a problem is only
// counted, and not placed.
//
// Takes span (ast.Span) which locates the problem in the query block's text, or is zero.
// Takes code (string) which is the diagnostic code.
// Takes severity (diag.ErrorSeverity) which is how serious the problem is.
// Takes message (string) which describes it.
// Takes suggestion (string) which is the name the query most likely meant, or empty.
func (r *queryReporter) add(span ast.Span, code string, severity diag.ErrorSeverity, message, suggestion string) {
	if r.limiter.Full() {
		r.limiter.Add(diagbuild.Unlocated(code, severity, message))
		return
	}
	location := diagbuild.TextSpan{Line: r.line, Column: 1, EndLine: 0, EndColumn: 0}
	if span.Known() && span.End <= len(r.sql) {
		location = r.lines.Span(span.Start, span.End)
	}
	r.limiter.Add(diagbuild.Located(r.filename, location, code, severity, message, suggestion))
}

// finish returns the problems kept, followed by one Q066 diagnostic on the query when any
// were left out, at the highest severity among them.
//
// Returns []diag.SourceError which holds the diagnostics, or nil when there are none.
func (r *queryReporter) finish() []diag.SourceError {
	return r.limiter.Finish(func(code string, severity diag.ErrorSeverity, message string) diag.SourceError {
		return diagbuild.Block(r.filename, r.line, code, severity, message)
	})
}

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

package pipeline

import (
	"slices"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
)

// diagnosticIdentity is what makes two diagnostics the same report: one code, severity,
// file, span and message. The suggestion is left out, so a repeat that differs only in
// its suggestion is still a repeat.
type diagnosticIdentity struct {
	// filename is the file the diagnostic is reported against.
	filename string

	// message describes the problem.
	message string

	// code is the diagnostic's code.
	code string

	// line is the first line of the span.
	line int

	// column is the first column of the span.
	column int

	// endLine is the last line of the span.
	endLine int

	// endColumn is the last column of the span.
	endColumn int

	// severity is the diagnostic's severity.
	severity diag.ErrorSeverity
}

// diagnosticCollector gathers the diagnostics of a run in the order its stages report
// them.
//
// Duplicate diagnostics are reported once. Beyond the cap, one Q066 diagnostic summarises
// the omitted diagnostics and carries their highest severity, so omitted errors still
// cause the run to fail. All pipeline diagnostics pass through this collector.
type diagnosticCollector struct {
	// kept holds the identity of every diagnostic kept, so a repeat is skipped.
	kept map[diagnosticIdentity]struct{}

	// limiter keeps the first diagnostics up to the cap and counts the rest.
	limiter *diagbuild.Limiter
}

// newDiagnosticCollector creates a collector that keeps at most limit diagnostics.
//
// Takes limit (int) which is the most diagnostics kept.
//
// Returns *diagnosticCollector which holds nothing yet.
func newDiagnosticCollector(limit int) *diagnosticCollector {
	return &diagnosticCollector{kept: make(map[diagnosticIdentity]struct{}), limiter: diagbuild.NewLimiter(limit)}
}

// add gathers diagnostics in order. A repeat of a diagnostic kept is skipped; once the
// cap is reached every further diagnostic is only counted, a repeat of one left out
// included, so the memory held stays within the cap.
//
// Takes diagnostics (...diag.SourceError) which are gathered.
func (collector *diagnosticCollector) add(diagnostics ...diag.SourceError) {
	for index := range diagnostics {
		identity := identityOf(&diagnostics[index])
		if _, repeated := collector.kept[identity]; repeated {
			continue
		}
		if !collector.limiter.Full() {
			collector.kept[identity] = struct{}{}
		}
		collector.limiter.Add(diagnostics[index])
	}
}

// collected returns the diagnostics gathered so far, followed by the Q066 diagnostic when
// any were left out. The collector may go on gathering afterwards.
//
// Returns []diag.SourceError which is a new slice, empty and never nil when there is no
// diagnostic.
func (collector *diagnosticCollector) collected() []diag.SourceError {
	finished := collector.limiter.Finish(nil)
	if len(finished) == 0 {
		return []diag.SourceError{}
	}
	return slices.Clone(finished)
}

// identityOf returns the identity of a diagnostic.
//
// Takes diagnostic (*diag.SourceError) which is the diagnostic.
//
// Returns diagnosticIdentity which identifies it.
func identityOf(diagnostic *diag.SourceError) diagnosticIdentity {
	return diagnosticIdentity{
		filename:  diagnostic.Filename,
		message:   diagnostic.Message,
		code:      diagnostic.Code,
		line:      diagnostic.Line,
		column:    diagnostic.Column,
		endLine:   diagnostic.EndLine,
		endColumn: diagnostic.EndColumn,
		severity:  diagnostic.Severity,
	}
}

// collectDiagnostics joins the diagnostics of a run's stages in order, as a
// diagnosticCollector gathers them.
//
// Takes limit (int) which is the most diagnostics kept.
// Takes lists (...[]diag.SourceError) which hold each stage's diagnostics, in the order
// they are reported.
//
// Returns []diag.SourceError which holds each distinct diagnostic once, in the order it
// first appears, up to the limit, then a Q066 diagnostic when any were left out; it is
// empty, never nil, when there is none.
func collectDiagnostics(limit int, lists ...[]diag.SourceError) []diag.SourceError {
	collector := newDiagnosticCollector(limit)
	for _, list := range lists {
		collector.add(list...)
	}
	return collector.collected()
}

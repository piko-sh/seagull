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
	"fmt"
	"slices"

	"seagull.sh/seagull/internal/diag"
)

// Limiter keeps the first diagnostics up to a cap and counts the rest by severity.
//
// The zero value keeps nothing and counts everything, as NewLimiter(0) does.
type Limiter struct {
	// dropped counts the diagnostics left out, keyed by their severity; it is nil until one
	// is left out.
	dropped map[diag.ErrorSeverity]int

	// kept holds the diagnostics added while the cap had room, in the order they were added.
	kept []diag.SourceError

	// limit is the most diagnostics kept.
	limit int
}

// NewLimiter returns a limiter that keeps at most limit diagnostics.
//
// Takes limit (int) which is the most diagnostics kept; a non-positive limit keeps
// nothing and counts everything.
//
// Returns *Limiter which keeps nothing yet.
func NewLimiter(limit int) *Limiter {
	return &Limiter{dropped: nil, kept: nil, limit: max(limit, 0)}
}

// Add keeps each diagnostic while the cap has room, in order, and counts the rest.
//
// Takes diagnostics (...diag.SourceError) which are kept or counted.
func (limiter *Limiter) Add(diagnostics ...diag.SourceError) {
	room := max(limiter.limit-len(limiter.kept), 0)
	if room > 0 {
		taken := min(room, len(diagnostics))
		limiter.kept = append(limiter.kept, diagnostics[:taken]...)
		diagnostics = diagnostics[taken:]
	}
	if len(diagnostics) == 0 {
		return
	}
	if limiter.dropped == nil {
		limiter.dropped = make(map[diag.ErrorSeverity]int)
	}
	for index := range diagnostics {
		limiter.dropped[diagnostics[index].Severity]++
	}
}

// Full reports whether the cap is reached, so that every diagnostic added from now on is
// only counted.
//
// Returns bool which is true once the limiter keeps no more diagnostics.
func (limiter *Limiter) Full() bool {
	return len(limiter.kept) >= limiter.limit
}

// Dropped returns how many diagnostics were left out.
//
// Returns int which is the number counted rather than kept.
func (limiter *Limiter) Dropped() int {
	total := 0
	for _, count := range limiter.dropped {
		total += count
	}
	return total
}

// Finish returns the kept diagnostics followed, when any were left out, by one Q066
// diagnostic whose severity is the highest severity left out, built by place.
//
// Errors are the highest severity, then warnings, then hints, so a run whose errors were
// left out still fails. The Q066 message says how many were left out, such as "and 1234
// more diagnostics were left out".
//
// Takes place (func(code string, severity diag.ErrorSeverity, message string)
// diag.SourceError) which builds the Q066 diagnostic, positioned where the caller
// chooses; nil builds it without a position, as Unlocated does.
//
// Returns []diag.SourceError which holds the kept diagnostics in the order they were
// added, then the Q066 diagnostic when any were left out. The limiter's own slice is
// never extended, so Finish may be called again.
func (limiter *Limiter) Finish(place func(code string, severity diag.ErrorSeverity, message string) diag.SourceError) []diag.SourceError {
	kept := slices.Clip(limiter.kept)
	dropped := limiter.Dropped()
	if dropped == 0 {
		return kept
	}
	if place == nil {
		place = Unlocated
	}
	return append(kept, place(diag.CodeDiagnosticLimit, limiter.highestDroppedSeverity(), droppedMessage(dropped)))
}

// highestDroppedSeverity returns the highest severity among the diagnostics left out: the
// one with the lowest value, since errors come before warnings and hints.
//
// Returns diag.ErrorSeverity which is the highest severity left out, or
// diag.SeverityError when nothing was left out.
func (limiter *Limiter) highestDroppedSeverity() diag.ErrorSeverity {
	highest, found := diag.SeverityError, false
	for severity, count := range limiter.dropped {
		if count > 0 && (!found || severity < highest) {
			highest, found = severity, true
		}
	}
	return highest
}

// droppedMessage says how many diagnostics were left out.
//
// Takes dropped (int) which is the number left out.
//
// Returns string which is the message.
func droppedMessage(dropped int) string {
	if dropped == 1 {
		return "and 1 more diagnostic was left out"
	}
	return fmt.Sprintf("and %d more diagnostics were left out", dropped)
}

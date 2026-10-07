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

package sqlite

import "seagull.sh/seagull/internal/dialect/ast"

const (
	// conflictResolutionIgnore is the resolution OR IGNORE names: a row that fails a
	// constraint, a uniqueness, NOT NULL or CHECK constraint alike, is skipped.
	conflictResolutionIgnore = "IGNORE"

	// conflictResolutionReplace is the resolution OR REPLACE names, and REPLACE INTO
	// implies: the rows a new row conflicts with are deleted before it is inserted.
	conflictResolutionReplace = "REPLACE"
)

// conflictResolutions are the resolutions the OR clause of an INSERT or UPDATE may name.
var conflictResolutions = []string{"ROLLBACK", "ABORT", "FAIL", conflictResolutionIgnore, conflictResolutionReplace}

// upsertClauses gathers what the ON CONFLICT clauses of an INSERT do with a row that
// conflicts with an existing one.
type upsertClauses struct {
	// skips records a DO NOTHING clause.
	skips bool

	// updates records a DO UPDATE clause.
	updates bool
}

// add records one ON CONFLICT clause.
//
// Takes action (ast.ConflictAction) which is the clause's DO NOTHING or DO UPDATE.
func (u *upsertClauses) add(action ast.ConflictAction) {
	switch action {
	case ast.ConflictActionDoNothing:
		u.skips = true
	case ast.ConflictActionDoUpdate:
		u.updates = true
	default:
	}
}

// conflictAction names what the INSERT does with a row that conflicts with an existing
// one.
//
// DO NOTHING and OR IGNORE can skip rows, which then produce no RETURNING result. OR
// IGNORE skips constraint failures regardless of any ON CONFLICT clauses that follow.
// Otherwise, DO UPDATE updates the existing row, while OR REPLACE and REPLACE INTO
// replace it, preserving one written row per input row.
//
// Takes resolution (string) which is the upper-cased resolution the statement's OR clause
// names, REPLACE for REPLACE INTO, or empty when it names none.
//
// Returns ast.ConflictAction which is the statement's conflict action.
func (u upsertClauses) conflictAction(resolution string) ast.ConflictAction {
	switch {
	case u.skips, resolution == conflictResolutionIgnore:
		return ast.ConflictActionDoNothing
	case u.updates, resolution == conflictResolutionReplace:
		return ast.ConflictActionDoUpdate
	default:
		return ast.ConflictActionNone
	}
}

// parseConflictResolutionIfPresent consumes the OR clause after INSERT or UPDATE that
// names how a constraint failure is resolved: OR ROLLBACK, ABORT, FAIL, IGNORE or
// REPLACE.
//
// Returns string which is the upper-cased resolution, or empty when the statement has no
// OR clause.
// Returns error when OR is followed by no resolution.
func (p *parser) parseConflictResolutionIfPresent() (string, error) {
	if !p.MatchKeyword(keywordOR) {
		return "", nil
	}
	resolution, err := p.ExpectKeyword(conflictResolutions...)
	if err != nil {
		return "", err
	}
	return resolution.Keyword(tokenIdentifier), nil
}

// parseInsertVerb consumes the verb of an INSERT, REPLACE or INSERT OR resolution, and
// gives the resolution it names.
//
// Returns string which is REPLACE for REPLACE, the upper-cased resolution of INSERT OR
// resolution, or empty for a plain INSERT.
// Returns error when OR is followed by no resolution.
func (p *parser) parseInsertVerb() (string, error) {
	if p.MatchKeyword(conflictResolutionReplace) {
		return conflictResolutionReplace, nil
	}
	p.MatchKeyword("INSERT")
	return p.parseConflictResolutionIfPresent()
}

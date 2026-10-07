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

package postgres

import (
	"fmt"

	"seagull.sh/seagull/internal/dialect/ast"
)

// maxStatementWarnings is the most warnings one statement reports before the rest are
// summarised in one more, so a statement repeating a mistake thousands of times cannot
// flood the diagnostics.
const maxStatementWarnings = 32

// statementWarnings collects the warnings of one statement, keeping the first
// maxStatementWarnings and counting the rest.
type statementWarnings struct {
	// kept holds the warnings kept, in the order they were found.
	kept []ast.Warning

	// leftOut counts the warnings found once the cap was reached.
	leftOut int
}

// room reports whether another warning fits under the cap, counting it as left out when
// it does not, so a producer builds only the warnings that are kept.
//
// Returns bool which is true when the caller should add the warning.
func (warnings *statementWarnings) room() bool {
	if len(warnings.kept) < maxStatementWarnings {
		return true
	}
	warnings.leftOut++
	return false
}

// add keeps a warning room admitted.
//
// Takes warning (ast.Warning) which is the warning.
func (warnings *statementWarnings) add(warning ast.Warning) {
	warnings.kept = append(warnings.kept, warning)
}

// finish returns the kept warnings and, when any were left out, one more on the statement
// saying how many.
//
// Returns []ast.Warning which holds the statement's warnings, or nil when there are none.
func (warnings *statementWarnings) finish() []ast.Warning {
	if warnings.leftOut == 0 {
		return warnings.kept
	}
	summary := fmt.Sprintf("and %d more warnings about this statement were left out", warnings.leftOut)
	if warnings.leftOut == 1 {
		summary = "and 1 more warning about this statement was left out"
	}
	return append(warnings.kept, ast.Warning{Message: summary, Span: ast.Span{Start: 0, End: 0}})
}

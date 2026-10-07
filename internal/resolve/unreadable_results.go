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

package resolve

import (
	"fmt"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
)

// unreadableColumns reports select-list columns rejected by the dialect's
// ResultCheckerPort, locating each error at its source item.
//
// Takes projection (*Projection) which holds the resolved columns and their origins.
// Takes itemSpans ([]Span) which holds, parallel to the select list, where each item's
// expression is written; a missing span leaves the error on the statement.
//
// Returns []*Error which holds an error for each column refused.
func (r *TypeResolver) unreadableColumns(projection *Projection, itemSpans []Span) []*Error {
	checker, checks := r.engine.(dialect.ResultCheckerPort)
	if !checks {
		return nil
	}
	var problems []*Error
	for index := range projection.Columns {
		column := &projection.Columns[index]
		problem, unreadable := checker.UnreadableResult(column.SQLType)
		if !unreadable {
			continue
		}
		origin := 0
		if index < len(projection.Origins) {
			origin = projection.Origins[index]
		}
		problems = append(problems, &Error{
			Code:       diag.CodeEmitterUnsupported,
			Message:    fmt.Sprintf("column %q cannot be read: %s", column.Name, problem.Reason),
			Suggestion: problem.Suggestion,
			Span:       itemSpanAt(itemSpans, origin),
			Severity:   diag.SeverityError,
		})
	}
	return problems
}

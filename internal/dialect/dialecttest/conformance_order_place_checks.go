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

package dialecttest

import (
	"context"
	"fmt"

	"seagull.sh/seagull/internal/dialect/ast"
)

// checkOrderByPlace checks the recorded insertion point for ORDER BY.
//
// Takes fixture (*Fixture) which supplies the dialect and its statements.
//
// Returns checkOutcome which holds every problem found.
func checkOrderByPlace(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	return runSpanCheck(ctx, fixture, orderByPlaceProblems)
}

// orderByPlaceProblems examines the ORDER BY place of one statement.
//
// Takes subject (spanSubject) which is the statement.
//
// Returns []string which describes each problem.
func orderByPlaceProblems(_ *spanCorpus, subject spanSubject) []string {
	analysis := subject.analysis
	place := analysis.Spans.OrderByPlace
	recorded := place != (ast.Span{})
	switch {
	case analysis.DataChange != 0 && recorded:
		return []string{fmt.Sprintf("a statement that changes rows records OrderByPlace %+v", place)}
	case analysis.DataChange != 0, !recorded && len(analysis.CompoundBranches) == 0 && !isSelect(subject):
		return nil
	case place.Start <= 0 || place.End != place.Start || place.Start > len(subject.text):
		return []string{fmt.Sprintf("a query must record OrderByPlace as a zero-width span inside it, got %+v", place)}
	}
	problems := tokenEndProblems(subject, place.Start)
	return append(problems, placeOrderProblems(&analysis.Spans, place.Start)...)
}

// tokenEndProblems checks that an offset is the end of one of the statement's tokens,
// and, when the statement writes ORDER BY, the end of the token right before it.
//
// Takes subject (spanSubject) which is the statement.
// Takes place (int) which is the offset.
//
// Returns []string which describes each problem.
func tokenEndProblems(subject spanSubject, place int) []string {
	orderBy := subject.analysis.Spans.OrderBy
	before := -1
	for index, token := range subject.tokens {
		if token.End == place {
			before = index
		}
	}
	switch {
	case before < 0:
		return []string{fmt.Sprintf("OrderByPlace %d is not the end of a token", place)}
	case orderBy.Known() && (before+1 >= len(subject.tokens) || subject.tokens[before+1].Start != orderBy.Start):
		return []string{fmt.Sprintf("OrderByPlace %d must be the end of the token right before ORDER BY at %d", place,
			orderBy.Start)}
	default:
		return nil
	}
}

// placeOrderProblems checks that the place follows the clauses ORDER BY follows, precedes
// those that follow it, and lies outside every row-shaping clause.
//
// Takes spans (*ast.StatementSpans) which are the statement's spans.
// Takes place (int) which is the place's offset.
//
// Returns []string which describes each problem.
func placeOrderProblems(spans *ast.StatementSpans, place int) []string {
	var problems []string
	for _, clause := range leadingClauses(spans) {
		if clause.span.Known() && clause.span.End > place {
			problems = append(problems, fmt.Sprintf("OrderByPlace %d must follow %s", place, clause.name))
		}
	}
	for _, clause := range []namedSpan{
		{name: "Pagination", span: spans.Pagination},
		{name: "Locking", span: spans.Locking},
		{name: trailingName, span: spans.Trailing},
	} {
		if clause.span.Known() && clause.span.Start < place {
			problems = append(problems, fmt.Sprintf("OrderByPlace %d must precede %s", place, clause.name))
		}
	}
	for index, shaping := range spans.RowShaping {
		if shaping.Start < place && shaping.End > place {
			problems = append(problems, fmt.Sprintf("OrderByPlace %d lies inside RowShaping[%d]", place, index))
		}
	}
	return problems
}

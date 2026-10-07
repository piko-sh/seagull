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
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqllex"
)

const (
	// whereName names the Where span in messages.
	whereName = "Where"

	// havingName names the Having span in messages.
	havingName = "Having"

	// orderByName names the OrderBy span in messages.
	orderByName = "OrderBy"

	// trailingName names the Trailing span in messages.
	trailingName = "Trailing"

	// trailingClauseCount is how many clause spans clauseList adds after the leading ones,
	// besides the row-shaping clauses and VALUES rows.
	trailingClauseCount = 5
)

// namedSpan is a clause span with the name a message calls it by.
type namedSpan struct {
	// name is the field's name.
	name string

	// span is the span.
	span ast.Span
}

// leadingClauses lists the clause spans that must come in a fixed order before the
// row-shaping, ordering and pagination clauses.
//
// Takes spans (*ast.StatementSpans) which are the statement's spans.
//
// Returns []namedSpan which holds them in order, known or not.
func leadingClauses(spans *ast.StatementSpans) []namedSpan {
	return []namedSpan{
		{name: "Quantifier", span: spans.Quantifier},
		{name: "Projection", span: spans.Projection},
		{name: "From", span: spans.From},
		{name: whereName, span: spans.Where},
		{name: "GroupBy", span: spans.GroupBy},
		{name: havingName, span: spans.Having},
	}
}

// checkClauseSpans checks that the clause spans of a statement are recorded for the
// clauses it has, lie inside the statement in the order the clauses must come, do not
// overlap, and that the parts of a clause lie inside it.
//
// Takes fixture (*Fixture) which supplies the dialect and its statements.
//
// Returns checkOutcome which holds every problem found.
func checkClauseSpans(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	return runSpanCheck(ctx, fixture, clauseSpanProblems)
}

// clauseSpanProblems examines the clause spans of one statement.
//
// Takes corpus (*spanCorpus) which describes the dialect.
// Takes subject (spanSubject) which is the statement.
//
// Returns []string which describes each problem.
func clauseSpanProblems(corpus *spanCorpus, subject spanSubject) []string {
	spans := &subject.analysis.Spans
	problems := insideProblems(subject, clauseList(spans))
	problems = append(problems, overlapProblems(clauseList(spans))...)
	problems = append(problems, orderProblems(spans)...)
	problems = append(problems, partProblems(spans)...)
	problems = append(problems, terminatorProblems(corpus, subject)...)
	problems = append(problems, compoundProblems(subject)...)
	return append(problems, presenceProblems(subject)...)
}

// clauseList lists the top-level clause spans of a statement that are known.
//
// Takes spans (*ast.StatementSpans) which are the statement's spans.
//
// Returns []namedSpan which holds the known clause spans.
func clauseList(spans *ast.StatementSpans) []namedSpan {
	all := make([]namedSpan, 0, len(spans.RowShaping)+len(spans.Values)+len(leadingClauses(spans))+trailingClauseCount)
	all = append(all, leadingClauses(spans)...)
	all = append(all,
		namedSpan{name: orderByName, span: spans.OrderBy},
		namedSpan{name: "Pagination", span: spans.Pagination},
		namedSpan{name: "Locking", span: spans.Locking},
		namedSpan{name: trailingName, span: spans.Trailing},
		namedSpan{name: "Returning", span: spans.Returning},
	)
	for index, span := range spans.RowShaping {
		all = append(all, namedSpan{name: fmt.Sprintf("RowShaping[%d]", index), span: span})
	}
	for index, tuple := range spans.Values {
		all = append(all, namedSpan{name: fmt.Sprintf("Values[%d]", index), span: tuple.Span})
	}
	return slices.DeleteFunc(all, func(entry namedSpan) bool { return entry.span == (ast.Span{}) })
}

// insideProblems checks that each clause span covers at least one byte of the statement
// and none outside it.
//
// Takes subject (spanSubject) which is the statement.
// Takes clauses ([]namedSpan) which are the clause spans.
//
// Returns []string which describes each problem.
func insideProblems(subject spanSubject, clauses []namedSpan) []string {
	var problems []string
	for _, clause := range clauses {
		if !clause.span.Known() || clause.span.Start < 0 || clause.span.End > len(subject.text) {
			problems = append(problems, fmt.Sprintf("%s %+v does not lie inside the statement of %d bytes",
				clause.name, clause.span, len(subject.text)))
		}
	}
	return problems
}

// overlapProblems checks that no two clause spans overlap.
//
// Takes clauses ([]namedSpan) which are the clause spans.
//
// Returns []string which describes each problem.
func overlapProblems(clauses []namedSpan) []string {
	var problems []string
	for first := range clauses {
		for second := first + 1; second < len(clauses); second++ {
			left, right := clauses[first].span, clauses[second].span
			if left.Start < right.End && right.Start < left.End {
				problems = append(problems, fmt.Sprintf("%s %+v overlaps %s %+v", clauses[first].name, left,
					clauses[second].name, right))
			}
		}
	}
	return problems
}

// orderProblems checks that the clauses come in the order a statement must write them:
// the select list and its quantifier, FROM, WHERE, GROUP BY and HAVING in that order,
// then the row-shaping clauses, ORDER BY, pagination and locking, and the trailing
// clauses last.
//
// Takes spans (*ast.StatementSpans) which are the statement's spans.
//
// Returns []string which describes each problem.
func orderProblems(spans *ast.StatementSpans) []string {
	var problems []string
	previous := namedSpan{name: "", span: ast.Span{}}
	for _, clause := range leadingClauses(spans) {
		if !clause.span.Known() {
			continue
		}
		if previous.span.Known() && clause.span.Start < previous.span.End {
			problems = append(problems, fmt.Sprintf("%s must follow %s", clause.name, previous.name))
		}
		previous = clause
	}
	following := make([]namedSpan, 0, len(spans.RowShaping)+2)
	following = append(following, namedSpan{name: orderByName, span: spans.OrderBy}, namedSpan{name: "Locking", span: spans.Locking})
	for index, span := range spans.RowShaping {
		following = append(following, namedSpan{name: fmt.Sprintf("RowShaping[%d]", index), span: span})
	}
	for _, clause := range following {
		if clause.span.Known() && previous.span.Known() && clause.span.Start < previous.span.End {
			problems = append(problems, fmt.Sprintf("%s must follow %s", clause.name, previous.name))
		}
	}
	problems = append(problems, mustFollow("Pagination", spans.Pagination, following[:1])...)
	problems = append(problems, mustFollow("Pagination", spans.Pagination, following[2:])...)
	return append(problems, mustFollow("Trailing", spans.Trailing, withoutTrailing(clauseList(spans)))...)
}

// withoutTrailing removes Trailing from a list of clause spans, so the clauses it must
// follow can be listed.
//
// Takes clauses ([]namedSpan) which are the clause spans.
//
// Returns []namedSpan which holds the others.
func withoutTrailing(clauses []namedSpan) []namedSpan {
	return slices.DeleteFunc(clauses, func(clause namedSpan) bool { return clause.name == trailingName })
}

// mustFollow checks that a clause, when known, starts after each of others ends.
//
// Takes name (string) which is the clause's name.
// Takes span (ast.Span) which is the clause's span.
// Takes others ([]namedSpan) which are the clauses it must follow.
//
// Returns []string which describes each problem.
func mustFollow(name string, span ast.Span, others []namedSpan) []string {
	if !span.Known() {
		return nil
	}
	var problems []string
	for _, other := range others {
		if other.span.Known() && span.Start < other.span.End {
			problems = append(problems, fmt.Sprintf("%s must follow %s", name, other.name))
		}
	}
	return problems
}

// partProblems checks that the parts of a clause lie inside it: the WHERE condition, the
// select list's items and the RETURNING clause's items.
//
// Takes spans (*ast.StatementSpans) which are the statement's spans.
//
// Returns []string which describes each problem.
func partProblems(spans *ast.StatementSpans) []string {
	var problems []string
	if spans.WhereCondition.Known() && !spans.Where.Contains(spans.WhereCondition) {
		problems = append(problems, "WhereCondition must lie inside Where")
	}
	if spans.Where.Known() && !spans.WhereCondition.Known() {
		problems = append(problems, "Where is recorded without its WhereCondition")
	}
	for index, item := range spans.ProjectionItems {
		if !spans.Projection.Contains(item.Span) {
			problems = append(problems, fmt.Sprintf("ProjectionItems[%d] must lie inside Projection", index))
		}
	}
	for index, item := range spans.ReturningItems {
		if !spans.Returning.Contains(item.Span) {
			problems = append(problems, fmt.Sprintf("ReturningItems[%d] must lie inside Returning", index))
		}
	}
	return problems
}

// terminatorProblems checks that a recorded terminator follows the statement and is the
// dialect's terminator.
//
// Takes corpus (*spanCorpus) which describes the dialect.
// Takes subject (spanSubject) which is the statement.
//
// Returns []string which describes each problem.
func terminatorProblems(corpus *spanCorpus, subject spanSubject) []string {
	terminator := subject.analysis.Spans.Terminator
	if terminator == (ast.Span{}) {
		return nil
	}
	expected := corpus.info.Lexical.Terminator
	if expected == "" {
		expected = sqllex.DefaultTerminator
	}
	if terminator.Start < len(subject.text) || spanText(subject, terminator) != expected {
		return []string{fmt.Sprintf("Terminator %+v covers %q, want the terminator %q after the statement",
			terminator, spanText(subject, terminator), expected)}
	}
	return nil
}

// compoundProblems checks that a compound statement records none of the clauses that
// belong to its branches.
//
// Takes subject (spanSubject) which is the statement.
//
// Returns []string which describes each problem.
func compoundProblems(subject spanSubject) []string {
	if len(subject.analysis.CompoundBranches) == 0 {
		return nil
	}
	spans := &subject.analysis.Spans
	var problems []string
	for _, clause := range leadingClauses(spans) {
		if clause.span != (ast.Span{}) {
			problems = append(problems, "a compound statement records "+clause.name+", which belongs to a branch")
		}
	}
	if len(spans.ProjectionItems) > 0 || len(spans.RowShaping) > 0 {
		problems = append(problems, "a compound statement records projection items or row shaping, which belong to a branch")
	}
	return problems
}

// presenceProblems checks that a SELECT, UPDATE or DELETE records the clauses it writes
// outside every parenthesis, so a dialect that records nothing cannot pass the other
// checks.
//
// Takes subject (spanSubject) which is the statement.
//
// Returns []string which describes each problem.
func presenceProblems(subject spanSubject) []string {
	analysis := subject.analysis
	if len(analysis.CompoundBranches) > 0 {
		return nil
	}
	spans := analysis.Spans
	var problems []string
	switch {
	case analysis.DataChange == ast.DataChangeUpdate || analysis.DataChange == ast.DataChangeDelete:
		problems = append(problems, requirePresent(subject, spans.Where, whereName, "WHERE")...)
		problems = append(problems, requirePresent(subject, spans.Returning, "Returning", "RETURNING")...)
	case analysis.DataChange == 0 && isSelect(subject):
		problems = append(problems, selectPresenceProblems(subject)...)
	}
	return problems
}

// selectPresenceProblems checks that a SELECT records its select list and the clauses it
// writes outside every parenthesis.
//
// Takes subject (spanSubject) which is the statement.
//
// Returns []string which describes each problem.
func selectPresenceProblems(subject spanSubject) []string {
	spans := subject.analysis.Spans
	var problems []string
	if !spans.Projection.Known() {
		problems = append(problems, "a SELECT records no Projection")
	}
	problems = append(problems, requirePresent(subject, spans.Where, whereName, "WHERE")...)
	problems = append(problems, requirePresent(subject, spans.Having, havingName, "HAVING")...)
	if index := topLevelWordIndex(subject.tokens, "GROUP", 0); index >= 0 && index+1 < len(subject.tokens) &&
		subject.tokens[index+1].IsWord("BY") && !spans.GroupBy.Known() {
		problems = append(problems, "a SELECT with GROUP BY records no GroupBy")
	}
	if index := topLevelWordIndex(subject.tokens, "ORDER", 0); index >= 0 && !spans.OrderBy.Known() {
		problems = append(problems, "a SELECT with ORDER BY records no OrderBy")
	}
	paginated := topLevelWordIndex(subject.tokens, "LIMIT", 0) >= 0 || topLevelWordIndex(subject.tokens, "OFFSET", 0) >= 0 ||
		topLevelWordIndex(subject.tokens, "FETCH", 0) >= 0
	if paginated && !spans.Pagination.Known() && len(spans.RowShaping) == 0 {
		problems = append(problems, "a SELECT with LIMIT, OFFSET or FETCH records no Pagination")
	}
	return problems
}

// requirePresent checks that a clause the statement writes outside every parenthesis is
// recorded.
//
// Takes subject (spanSubject) which is the statement.
// Takes span (ast.Span) which is the clause's span.
// Takes name (string) which is the span's field name.
// Takes keyword (string) which is the keyword that opens the clause.
//
// Returns []string which describes the problem, if any.
func requirePresent(subject spanSubject, span ast.Span, name, keyword string) []string {
	if topLevelWordIndex(subject.tokens, keyword, 0) < 0 || span.Known() {
		return nil
	}
	return []string{fmt.Sprintf("the statement writes %s but records no %s", keyword, name)}
}

// isSelect reports whether a statement is a query: SELECT, or a WITH clause followed by
// SELECT.
//
// Takes subject (spanSubject) which is the statement.
//
// Returns bool which is true for a query.
func isSelect(subject spanSubject) bool {
	if len(subject.tokens) == 0 {
		return false
	}
	if subject.tokens[0].IsWord("SELECT") {
		return true
	}
	return subject.tokens[0].IsWord("WITH") && topLevelWordIndex(subject.tokens, "SELECT", 1) >= 0
}

// checkProjectionItems checks that a query's select list records one item per output
// column the dialect returns, in order, each inside the select list with its expression
// and alias inside it, and likewise for a RETURNING clause.
//
// Takes fixture (*Fixture) which supplies the dialect and its statements.
//
// Returns checkOutcome which holds every problem found.
func checkProjectionItems(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	return runSpanCheck(ctx, fixture, projectionItemProblems)
}

// projectionItemProblems examines the select list or RETURNING items of one statement.
//
// Takes subject (spanSubject) which is the statement.
//
// Returns []string which describes each problem.
func projectionItemProblems(_ *spanCorpus, subject spanSubject) []string {
	analysis := subject.analysis
	spans := analysis.Spans
	switch {
	case analysis.HasReturning:
		return itemProblems(subject, "ReturningItems", spans.ReturningItems, analysis.OutputColumns)
	case spans.Projection.Known():
		return itemProblems(subject, "ProjectionItems", spans.ProjectionItems, analysis.OutputColumns)
	default:
		return nil
	}
}

// itemProblems checks a list of item spans against the output columns they stand for.
//
// Takes subject (spanSubject) which is the statement.
// Takes name (string) which is the list's field name.
// Takes items ([]ast.ItemSpan) which are the item spans.
// Takes columns ([]ast.RawOutputColumn) which are the output columns.
//
// Returns []string which describes each problem.
func itemProblems(subject spanSubject, name string, items []ast.ItemSpan, columns []ast.RawOutputColumn) []string {
	if len(items) != len(columns) {
		return []string{fmt.Sprintf("%s records %d items for %d output columns", name, len(items), len(columns))}
	}
	var problems []string
	previousEnd := -1
	for index, item := range items {
		label := fmt.Sprintf("%s[%d] %q", name, index, spanText(subject, item.Span))
		if !item.Span.Contains(item.Expression) {
			problems = append(problems, label+" does not hold its expression")
		}
		if item.Alias.Known() && (!item.Span.Contains(item.Alias) || item.Alias.Start < item.Expression.End) {
			problems = append(problems, label+" records an alias outside it or before its expression")
		}
		if item.Span.Start < previousEnd {
			problems = append(problems, label+" overlaps the item before it")
		}
		if item.Star != columns[index].IsStar {
			problems = append(problems, fmt.Sprintf("%s records Star %t but the output column's IsStar is %t", label, item.Star,
				columns[index].IsStar))
		}
		if item.Star && !strings.HasSuffix(spanText(subject, item.Expression), "*") {
			problems = append(problems, label+" is a star whose expression does not end in *")
		}
		previousEnd = item.Span.End
	}
	return problems
}

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

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqllex"
)

const (
	// openingParenthesis opens a parenthesised part of a statement.
	openingParenthesis = "("

	// closingParenthesis closes a parenthesised part of a statement, or a parenthesised
	// statement.
	closingParenthesis = ")"
)

var (
	// clauseFollowers lists the keywords that may follow a complete top-level clause.
	clauseFollowers = []string{
		"SELECT", "FROM", "WHERE", "GROUP", "HAVING", "WINDOW", "QUALIFY", "ORDER", "LIMIT", "OFFSET", "FETCH",
		"FOR", "LOCK", "USING", "SETTINGS", "FORMAT", "RETURNING", "INTO", "UNION", "INTERSECT", "EXCEPT",
	}

	// clauseClosers lists the closing parenthesis and statement terminator that may follow a
	// top-level clause.
	clauseClosers = []string{closingParenthesis, ";"}
)

// boundedClause is a clause span with the keywords that open it.
type boundedClause struct {
	// opens lists the keywords the clause starts with, and is nil for a part of a clause,
	// such as the select list, that starts with no keyword of its own.
	opens []string

	namedSpan
}

// checkClauseSpansCoverWholeClauses checks that each clause span a code generator cuts or
// replaces covers whole tokens and a whole clause, so that leaving out an ORDER BY,
// replacing a pagination or counting up to a clause never keeps part of a clause or drops
// part of another.
//
// Takes fixture (*Fixture) which supplies the dialect and its statements.
//
// Returns checkOutcome which holds every problem found.
func checkClauseSpansCoverWholeClauses(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	return runSpanCheck(ctx, fixture, wholeClauseProblems)
}

// wholeClauseProblems examines the clause spans of one statement.
//
// Takes subject (spanSubject) which is the statement.
//
// Returns []string which describes each problem.
func wholeClauseProblems(_ *spanCorpus, subject spanSubject) []string {
	clauses := boundedClauses(&subject.analysis.Spans)
	problems := make([]string, 0, len(clauses))
	for _, clause := range clauses {
		problems = append(problems, clauseBoundaryProblems(subject, clause)...)
	}
	return problems
}

// boundedClauses lists the clause spans whose bounds a code generator relies on, with the
// keywords each opens with.
//
// Takes spans (*ast.StatementSpans) which are the statement's spans.
//
// Returns []boundedClause which holds them, known or not.
func boundedClauses(spans *ast.StatementSpans) []boundedClause {
	return []boundedClause{
		{namedSpan: namedSpan{name: projectionName, span: spans.Projection}, opens: nil},
		{namedSpan: namedSpan{name: fromName, span: spans.From}, opens: []string{"FROM"}},
		{namedSpan: namedSpan{name: whereName, span: spans.Where}, opens: []string{"WHERE"}},
		{namedSpan: namedSpan{name: whereConditionName, span: spans.WhereCondition}, opens: nil},
		{namedSpan: namedSpan{name: groupByName, span: spans.GroupBy}, opens: []string{"GROUP"}},
		{namedSpan: namedSpan{name: havingName, span: spans.Having}, opens: []string{"HAVING"}},
		{namedSpan: namedSpan{name: orderByName, span: spans.OrderBy}, opens: []string{"ORDER"}},
		{namedSpan: namedSpan{name: paginationName, span: spans.Pagination}, opens: []string{"LIMIT", "OFFSET", "FETCH"}},
		{namedSpan: namedSpan{name: lockingName, span: spans.Locking}, opens: []string{"FOR", "LOCK"}},
		{namedSpan: namedSpan{name: returningName, span: spans.Returning}, opens: []string{"RETURNING"}},
	}
}

// clauseBoundaryProblems checks where a known clause span starts and ends.
//
// Takes subject (spanSubject) which is the statement.
// Takes clause (boundedClause) which is the clause span.
//
// Returns []string which describes each problem.
func clauseBoundaryProblems(subject spanSubject, clause boundedClause) []string {
	if !clause.span.Known() {
		return nil
	}
	first, next := tokensAround(subject.tokens, clause.span)
	if first < 0 {
		return []string{fmt.Sprintf("%s %+v covers no token", clause.name, clause.span)}
	}
	problems := clauseStartProblems(subject, clause, first)
	return append(problems, clauseEndProblems(subject, clause, first, next)...)
}

// clauseStartProblems checks that a clause span starts where a token starts and, for a
// clause that opens with a keyword, at that keyword.
//
// Takes subject (spanSubject) which is the statement.
// Takes clause (boundedClause) which is the clause span.
// Takes first (int) which is the index of the first token inside the span.
//
// Returns []string which describes the problem, or nil.
func clauseStartProblems(subject spanSubject, clause boundedClause, first int) []string {
	if straddled, found := tokenAcross(subject.tokens, clause.span.Start); found {
		return []string{fmt.Sprintf("%s %q starts inside %q", clause.name, spanText(subject, clause.span), straddled.Raw)}
	}
	if clause.opens != nil && !isAnyWord(subject.tokens[first], clause.opens) {
		return []string{fmt.Sprintf("%s %q starts at %q, not at the keyword that opens it", clause.name,
			spanText(subject, clause.span), subject.tokens[first].Raw)}
	}
	return nil
}

// clauseEndProblems checks that a clause span ends where a token ends, balances its
// parentheses, and is followed by the end of the statement or a token that may follow a
// whole clause.
//
// Takes subject (spanSubject) which is the statement.
// Takes clause (boundedClause) which is the clause span.
// Takes first (int) which is the index of the first token inside the span.
// Takes next (int) which is the index of the first token after it, or the number of
// tokens when none follows.
//
// Returns []string which describes the problem, or nil.
func clauseEndProblems(subject spanSubject, clause boundedClause, first, next int) []string {
	if straddled, found := tokenAcross(subject.tokens, clause.span.End); found {
		return []string{fmt.Sprintf("%s %q ends inside %q", clause.name, spanText(subject, clause.span), straddled.Raw)}
	}
	balanced := balancesParentheses(subject.tokens[first:next])
	if next == len(subject.tokens) {
		if balanced {
			return nil
		}
		return []string{fmt.Sprintf("%s %q does not balance its parentheses", clause.name, spanText(subject, clause.span))}
	}
	if balanced && endsClause(subject.tokens[next], clause) {
		return nil
	}
	return []string{fmt.Sprintf("%s %q ends before %q, inside its clause", clause.name, spanText(subject, clause.span),
		subject.tokens[next].Raw)}
}

// tokensAround returns the first token a span covers and the first token after it.
//
// Takes tokens ([]sqllex.Token) which are the statement's significant tokens.
// Takes span (ast.Span) which is the span, relative to the statement.
//
// Returns first (int) which is the index of the first token inside the span, or -1.
// Returns next (int) which is the index of the first token that starts at or after the
// span's end, or the number of tokens when none does.
func tokensAround(tokens []sqllex.Token, span ast.Span) (first, next int) {
	first, next = -1, len(tokens)
	for index, current := range tokens {
		if first < 0 && current.Start >= span.Start && current.End <= span.End {
			first = index
		}
		if current.Start >= span.End {
			return first, index
		}
	}
	return first, next
}

// tokenAcross returns the token an offset falls inside, past its first byte and before
// its end, where a span that starts or ends at the offset would split it.
//
// Takes tokens ([]sqllex.Token) which are the statement's significant tokens.
// Takes offset (int) which is the offset, relative to the statement.
//
// Returns sqllex.Token which is the token.
// Returns bool which is false when the offset falls between tokens.
func tokenAcross(tokens []sqllex.Token, offset int) (sqllex.Token, bool) {
	index := slices.IndexFunc(tokens, func(current sqllex.Token) bool {
		return current.Start < offset && offset < current.End
	})
	if index < 0 {
		return sqllex.Token{}, false
	}
	return tokens[index], true
}

// balancesParentheses reports whether tokens close every parenthesis they open, and close
// none they do not open.
//
// Takes tokens ([]sqllex.Token) which are the tokens.
//
// Returns bool which is true when the parentheses balance.
func balancesParentheses(tokens []sqllex.Token) bool {
	depth := 0
	for _, current := range tokens {
		switch current.Raw {
		case openingParenthesis:
			depth++
		case closingParenthesis:
			depth--
		}
		if depth < 0 {
			return false
		}
	}
	return depth == 0
}

// endsClause reports whether a token may follow a complete top-level clause.
//
// Takes current (sqllex.Token) which is the token after the clause.
// Takes clause (boundedClause) which is the clause.
//
// Returns bool which is true when the token may follow the whole clause.
func endsClause(current sqllex.Token, clause boundedClause) bool {
	if slices.Contains(clauseClosers, current.Raw) {
		return true
	}
	return isAnyWord(current, clauseFollowers) && !isAnyWord(current, clause.opens)
}

// isAnyWord reports whether a token is one of some words, ignoring case.
//
// Takes current (sqllex.Token) which is the token.
// Takes words ([]string) which are the words.
//
// Returns bool which is true when the token is one of them.
func isAnyWord(current sqllex.Token, words []string) bool {
	return slices.ContainsFunc(words, current.IsWord)
}

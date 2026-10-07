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
	"io/fs"
	"path"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqllex"
)

const (
	// queryDirectoryName is the directory a corpus case keeps its query files in.
	queryDirectoryName = "queries"

	// sqlExtension is the extension of a query file.
	sqlExtension = ".sql"

	// negationOffset is how many tokens before a sole IN-list placeholder a NOT negating the
	// list sits.
	negationOffset = 3

	// noSpanSubjects says why a span check does not apply.
	noSpanSubjects = "the fixture sets no Spans statements or corpora"
)

// spanSubject is one analysed statement whose spans the span checks examine.
type spanSubject struct {
	// analysis is the dialect's analysis of the statement.
	analysis *ast.RawQueryAnalysis

	// label names where the statement came from, for messages.
	label string

	// source is the text the statement was parsed from.
	source string

	// text is the statement's own text, the offsets of its spans index into.
	text string

	// tokens are the statement's significant tokens under the dialect's lexical rules, with
	// offsets into text.
	tokens []sqllex.Token

	// location is the offset of the statement's first byte in source.
	location int
}

// spanCorpus is what the span checks examine for a fixture: every statement it could
// analyse, the problems with the fixture's own statements, and the dialect's description.
type spanCorpus struct {
	// subjects holds the analysed statements.
	subjects []spanSubject

	// problems describes each fixture statement the dialect could not analyse.
	problems []string

	// info describes the dialect.
	info dialect.Info
}

// collectSpanCorpus analyses the fixture's span statements, which must all analyse, and
// every statement of every query file in its corpora that does.
//
// Takes fixture (*Fixture) which supplies the dialect and its statements.
//
// Returns *spanCorpus which holds the analysed statements.
func collectSpanCorpus(ctx context.Context, fixture *Fixture) *spanCorpus {
	engine := fixture.NewDialect()
	corpus := &spanCorpus{subjects: nil, problems: nil, info: engine.Info()}
	for index, statement := range fixture.Spans.Statements {
		label := fmt.Sprintf("Spans.Statements[%d]", index)
		subjects, err := analyseSpanSubjects(ctx, engine, corpus.info.Lexical, label, statement)
		if err != nil {
			corpus.problems = append(corpus.problems, fmt.Sprintf("%s %q must analyse: %v", label, statement, err))
			continue
		}
		corpus.subjects = append(corpus.subjects, subjects...)
	}
	for _, files := range fixture.Spans.Corpora {
		corpus.subjects = append(corpus.subjects, corpusSubjects(ctx, engine, corpus.info.Lexical, files)...)
	}
	return corpus
}

// corpusSubjects analyses every statement of every query file in a corpus, skipping the
// files and statements the dialect rejects.
//
// Takes engine (dialect.DialectPort) which analyses the statements.
// Takes rules (sqllex.Rules) which are the dialect's lexical rules.
// Takes files (fs.FS) which holds the corpus.
//
// Returns []spanSubject which holds the statements analysed.
func corpusSubjects(ctx context.Context, engine dialect.DialectPort, rules sqllex.Rules, files fs.FS) []spanSubject {
	var subjects []spanSubject
	_ = fs.WalkDir(files, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || path.Ext(name) != sqlExtension || path.Base(path.Dir(name)) != queryDirectoryName {
			return nil
		}
		content, err := fs.ReadFile(files, name)
		if err != nil {
			return nil
		}
		found, err := analyseSpanSubjects(ctx, engine, rules, name, sqllex.TrimByteOrderMark(string(content)))
		if err == nil {
			subjects = append(subjects, found...)
		}
		return nil
	})
	return subjects
}

// analyseSpanSubjects parses SQL and analyses each statement in it. A statement the
// dialect cannot analyse fails the whole text when it is a fixture statement and is
// skipped otherwise, which the caller decides by the error.
//
// Takes engine (dialect.DialectPort) which parses and analyses.
// Takes rules (sqllex.Rules) which are the dialect's lexical rules.
// Takes label (string) which names the text.
// Takes source (string) which is the SQL.
//
// Returns []spanSubject which holds the analysed statements.
// Returns error when parsing fails or a statement does not analyse.
func analyseSpanSubjects(
	ctx context.Context,
	engine dialect.DialectPort,
	rules sqllex.Rules,
	label string,
	source string,
) ([]spanSubject, error) {
	statements, err := engine.ParseStatements(ctx, source)
	if err != nil {
		return nil, err
	}
	subjects := make([]spanSubject, 0, len(statements))
	for index, statement := range statements {
		analysis, analyseErr := engine.AnalyseQuery(ctx, nil, statement)
		if analyseErr != nil {
			return nil, analyseErr
		}
		text := source[statement.Location : statement.Location+statement.Length]
		tokens, lexErr := significantTokens(rules, text)
		if lexErr != nil {
			return nil, lexErr
		}
		subjects = append(subjects, spanSubject{
			analysis: analysis,
			label:    fmt.Sprintf("%s statement %d", label, index+1),
			source:   source,
			text:     text,
			tokens:   tokens,
			location: statement.Location,
		})
	}
	return subjects, nil
}

// significantTokens scans text under the rules and keeps every token that is not
// whitespace or an ordinary comment; executable comments are kept.
//
// Takes rules (sqllex.Rules) which are the dialect's lexical rules.
// Takes text (string) which is the statement.
//
// Returns []sqllex.Token which holds the significant tokens.
// Returns error when the text leaves a construct unterminated.
func significantTokens(rules sqllex.Rules, text string) ([]sqllex.Token, error) {
	tokens, err := sqllex.Tokens(rules, text)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(tokens, func(token sqllex.Token) bool { return token.Kind.IsNoise() }), nil
}

// spanText returns the text a span of a subject covers, or a marker when the span lies
// outside it.
//
// Takes subject (spanSubject) which is the statement.
// Takes span (ast.Span) which is the span.
//
// Returns string which is the covered text.
func spanText(subject spanSubject, span ast.Span) string {
	start, end := subject.location+span.Start, subject.location+span.End
	if span.Start < 0 || start > end || end > len(subject.source) {
		return "<outside the source>"
	}
	return subject.source[start:end]
}

// runSpanCheck applies a check to every statement of the fixture's span corpus.
//
// Takes fixture (*Fixture) which supplies the dialect and its statements.
// Takes check (func(*spanCorpus, spanSubject) []string) which examines one statement.
//
// Returns checkOutcome which holds every problem found.
func runSpanCheck(ctx context.Context, fixture *Fixture, check func(*spanCorpus, spanSubject) []string) checkOutcome {
	if fixture.Spans == nil || len(fixture.Spans.Statements) == 0 && len(fixture.Spans.Corpora) == 0 {
		return notApplicable(noSpanSubjects)
	}
	corpus := collectSpanCorpus(ctx, fixture)
	problems := corpus.problems
	for _, subject := range corpus.subjects {
		for _, problem := range check(corpus, subject) {
			problems = append(problems, fmt.Sprintf("%s (%q): %s", subject.label, subject.text, problem))
		}
	}
	return outcomeOf(problems)
}

// checkPlaceholderSpans checks that every placeholder the dialect's lexical rules see is
// covered by exactly one placeholder span, and that each span reads under the dialect's
// placeholder forms with the name, number and type it records.
//
// Takes fixture (*Fixture) which supplies the dialect and its statements.
//
// Returns checkOutcome which holds every problem found.
func checkPlaceholderSpans(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	return runSpanCheck(ctx, fixture, placeholderSpanProblems)
}

// checkPlaceholdersAreParameters checks that every placeholder of a statement names a
// parameter the statement's analysis holds, wherever it stands: in any arm of a compound
// statement, in a derived table, a CTE or a subquery.
//
// A placeholder the analysis leaves out matches none of the query's parameters, which the
// generator reports as an error on valid SQL (N-203).
//
// Takes fixture (*Fixture) which supplies the dialect and its statements.
//
// Returns checkOutcome which holds every problem found.
func checkPlaceholdersAreParameters(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	return runSpanCheck(ctx, fixture, unboundPlaceholderProblems)
}

// unboundPlaceholderProblems describes each placeholder of one statement that names no
// parameter of the statement's analysis.
//
// Takes subject (spanSubject) which is the statement.
//
// Returns []string which describes each such placeholder.
func unboundPlaceholderProblems(_ *spanCorpus, subject spanSubject) []string {
	analysis := subject.analysis
	numbers := make(map[int]bool, len(analysis.ParameterReferences))
	for index := range analysis.ParameterReferences {
		numbers[analysis.ParameterReferences[index].Number] = true
	}
	var problems []string
	for index := range analysis.Spans.Placeholders {
		span := &analysis.Spans.Placeholders[index]
		if number := placeholderNumber(analysis, span); number == 0 || !numbers[number] {
			problems = append(problems, fmt.Sprintf("the placeholder %q names no parameter of the statement", spanText(subject, span.Span)))
		}
	}
	return problems
}

// placeholderSpanProblems examines the placeholder spans of one statement.
//
// Takes corpus (*spanCorpus) which describes the dialect.
// Takes subject (spanSubject) which is the statement.
//
// Returns []string which describes each problem.
func placeholderSpanProblems(corpus *spanCorpus, subject spanSubject) []string {
	spans := subject.analysis.Spans.Placeholders
	if len(spans) == 0 && len(subject.analysis.ParameterReferences) == 0 {
		return nil
	}
	covered := make([]int, len(spans))
	problems := placeholderCoverageProblems(subject, spans, covered)
	texts := make([]string, len(spans))
	for index := range spans {
		texts[index] = spanText(subject, spans[index].Span)
	}
	numbers := dialectkit.NumberPlaceholders(texts, corpus.info.Placeholders)
	for index := range spans {
		if covered[index] == 0 {
			problems = append(problems, fmt.Sprintf("the placeholder span %q covers no placeholder", texts[index]))
			continue
		}
		problems = append(problems, placeholderReadingProblems(corpus.info.Placeholders, subject, spans[index], numbers[index])...)
	}
	return problems
}

// placeholderCoverageProblems checks that every placeholder token is covered by exactly
// one span, counting how many tokens each span covers.
//
// Takes subject (spanSubject) which is the statement.
// Takes spans ([]ast.PlaceholderSpan) which are its placeholder spans.
// Takes covered ([]int) which receives, per span, the number of tokens it covers.
//
// Returns []string which describes each problem.
func placeholderCoverageProblems(subject spanSubject, spans []ast.PlaceholderSpan, covered []int) []string {
	var problems []string
	for _, token := range subject.tokens {
		if token.Kind != sqllex.KindParameter {
			continue
		}
		matches := 0
		for index := range spans {
			if spans[index].Span.Start == token.Start && spans[index].Span.End == token.End {
				matches++
				covered[index]++
			}
		}
		if matches != 1 {
			problems = append(problems, fmt.Sprintf("the placeholder %q at %d is covered by %d spans, want 1", token.Raw, token.Start, matches))
		}
	}
	return problems
}

// placeholderReadingProblems checks that a placeholder span reads under the dialect's
// forms and records what its text says.
//
// Takes placeholders (dialect.Placeholders) which describes the dialect's forms.
// Takes subject (spanSubject) which is the statement.
// Takes span (ast.PlaceholderSpan) which is the span.
// Takes number (int) which is the number its form implies: its own for a numbered form,
// the next one for a numbered form written without one, and its ordinal among the
// anonymous placeholders for an anonymous form.
//
// Returns []string which describes each problem.
func placeholderReadingProblems(
	placeholders dialect.Placeholders,
	subject spanSubject,
	span ast.PlaceholderSpan,
	number int,
) []string {
	text := spanText(subject, span.Span)
	reading, ok := placeholders.Read(text)
	if !ok {
		return []string{fmt.Sprintf("the placeholder %q is not written in any of Info.Placeholders.Sigils", text)}
	}
	var problems []string
	switch reading.Sigil.Kind {
	case dialect.PlaceholderNumbered:
		if span.Number != number {
			problems = append(problems, fmt.Sprintf("the placeholder %q records number %d, want %d", text, span.Number, number))
		}
	case dialect.PlaceholderAnonymous:
		if span.Number != number {
			problems = append(problems, fmt.Sprintf("the anonymous placeholder %q records number %d, want its ordinal %d",
				text, span.Number, number))
		}
	case dialect.PlaceholderNamed:
		if span.Name != reading.Name {
			problems = append(problems, fmt.Sprintf("the placeholder %q records name %q", text, span.Name))
		}
		if reading.Type != "" && spanText(subject, span.Type) != reading.Type {
			problems = append(problems, fmt.Sprintf("the placeholder %q records type %q", text, spanText(subject, span.Type)))
		}
	}
	return problems
}

// checkInListSpans checks that a placeholder records the IN list it sits in exactly when
// it is the list's sole element, with the list and operator located.
//
// Takes fixture (*Fixture) which supplies the dialect and its statements.
//
// Returns checkOutcome which holds every problem found.
func checkInListSpans(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	return runSpanCheck(ctx, fixture, inListSpanProblems)
}

// inListSpanProblems examines the IN list spans of one statement's placeholders.
//
// Takes subject (spanSubject) which is the statement.
//
// Returns []string which describes each problem.
func inListSpanProblems(_ *spanCorpus, subject spanSubject) []string {
	var problems []string
	for _, span := range subject.analysis.Spans.Placeholders {
		index := slices.IndexFunc(subject.tokens, func(token sqllex.Token) bool { return token.Start == span.Span.Start })
		if index < 0 {
			continue
		}
		expected, sole := soleInList(subject.tokens, index)
		text := spanText(subject, span.Span)
		switch {
		case sole && span.List != expected:
			problems = append(problems, fmt.Sprintf("the placeholder %q is the sole element of %q but records the list %+v, want %+v",
				text, spanText(subject, expected.List), span.List, expected))
		case !sole && span.List != (ast.ListSpan{}):
			problems = append(problems, fmt.Sprintf("the placeholder %q is not the sole element of an IN list but records one", text))
		}
	}
	return problems
}

// soleInList reports whether the token at index is the sole element of an IN list, and
// where the list and its operator are.
//
// Takes tokens ([]sqllex.Token) which are the statement's significant tokens.
// Takes index (int) which is the placeholder's index.
//
// Returns ast.ListSpan which locates the list.
// Returns bool which is true when the placeholder is the list's sole element.
func soleInList(tokens []sqllex.Token, index int) (ast.ListSpan, bool) {
	if index < 2 || index+1 >= len(tokens) || tokens[index-1].Raw != "(" || tokens[index+1].Raw != ")" ||
		!tokens[index-2].IsWord("IN") {
		return ast.ListSpan{}, false
	}
	first := tokens[index-2]
	negated := index >= negationOffset && tokens[index-negationOffset].IsWord("NOT")
	if negated {
		first = tokens[index-negationOffset]
	}
	return ast.ListSpan{
		Operator: ast.Span{Start: first.Start, End: tokens[index-2].End},
		List:     ast.Span{Start: tokens[index-1].Start, End: tokens[index+1].End},
		Negated:  negated,
	}, true
}

// checkValuesTupleSpans checks that each VALUES row is parenthesised and its items are
// separated by exactly one comma, and that an INSERT ... VALUES records its rows.
//
// Takes fixture (*Fixture) which supplies the dialect and its statements.
//
// Returns checkOutcome which holds every problem found.
func checkValuesTupleSpans(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	return runSpanCheck(ctx, fixture, valuesTupleProblems)
}

// valuesTupleProblems examines the VALUES rows of one statement.
//
// Takes subject (spanSubject) which is the statement.
//
// Returns []string which describes each problem.
func valuesTupleProblems(_ *spanCorpus, subject spanSubject) []string {
	tuples := subject.analysis.Spans.Values
	var problems []string
	if len(tuples) == 0 && isInsertValues(subject) {
		problems = append(problems, "an INSERT ... VALUES records no VALUES rows")
	}
	previousEnd := -1
	for index, tuple := range tuples {
		text := spanText(subject, tuple.Span)
		if !strings.HasPrefix(text, "(") || !strings.HasSuffix(text, ")") {
			problems = append(problems, fmt.Sprintf("VALUES row %d covers %q, which is not parenthesised", index+1, text))
			continue
		}
		if previousEnd >= 0 && !gapHolds(subject, previousEnd, tuple.Span.Start, ",") {
			problems = append(problems, fmt.Sprintf("VALUES row %d does not follow the previous row after one comma", index+1))
		}
		previousEnd = tuple.Span.End
		problems = append(problems, tupleItemProblems(subject, index, tuple)...)
	}
	return problems
}

// tupleItemProblems checks that the items of a VALUES row lie inside it, separated by
// exactly one comma, with nothing but the parentheses around them.
//
// Takes subject (spanSubject) which is the statement.
// Takes index (int) which is the row's index.
// Takes tuple (ast.TupleSpan) which is the row.
//
// Returns []string which describes each problem.
func tupleItemProblems(subject spanSubject, index int, tuple ast.TupleSpan) []string {
	if len(tuple.Items) == 0 {
		if !gapHolds(subject, tuple.Span.Start+1, tuple.Span.End-1) {
			return []string{fmt.Sprintf("VALUES row %d records no items", index+1)}
		}
		return nil
	}
	var problems []string
	position := tuple.Span.Start + 1
	for itemIndex, item := range tuple.Items {
		separators := []string{","}
		if itemIndex == 0 {
			separators = nil
		}
		if !item.Known() || item.Start < position || item.End > tuple.Span.End-1 || !gapHolds(subject, position, item.Start, separators...) {
			problems = append(problems, fmt.Sprintf("VALUES row %d item %d covers %q, which does not follow the previous item after one comma",
				index+1, itemIndex+1, spanText(subject, item)))
			return problems
		}
		position = item.End
	}
	if !gapHolds(subject, position, tuple.Span.End-1) {
		problems = append(problems, fmt.Sprintf("VALUES row %d has text after its last item %q", index+1,
			spanText(subject, tuple.Items[len(tuple.Items)-1])))
	}
	return problems
}

// gapHolds reports whether the significant tokens between two offsets are exactly the
// given texts, in order.
//
// Takes subject (spanSubject) which is the statement.
// Takes start (int) which is the offset the gap starts at.
// Takes end (int) which is the offset the gap ends at.
// Takes expected (...string) which lists the texts the gap must hold.
//
// Returns bool which is true when the gap holds exactly those tokens.
func gapHolds(subject spanSubject, start, end int, expected ...string) bool {
	if start > end {
		return false
	}
	var found []string
	for _, token := range subject.tokens {
		if token.Start >= start && token.End <= end {
			found = append(found, token.Raw)
		}
	}
	return slices.Equal(found, expected)
}

// isInsertValues reports whether a statement is an INSERT whose rows come from a VALUES
// list at its top level.
//
// Takes subject (spanSubject) which is the statement.
//
// Returns bool which is true for an INSERT ... VALUES.
func isInsertValues(subject spanSubject) bool {
	if subject.analysis.DataChange != ast.DataChangeInsert || subject.analysis.InsertSelect != nil {
		return false
	}
	return topLevelWordIndex(subject.tokens, "VALUES", 0) >= 0
}

// topLevelWordIndex returns the index of the first token, at or after from, that is the
// word outside every parenthesis, or -1.
//
// Takes tokens ([]sqllex.Token) which are the statement's significant tokens.
// Takes word (string) which is the word to find.
// Takes from (int) which is the index to start at.
//
// Returns int which is the index, or -1.
func topLevelWordIndex(tokens []sqllex.Token, word string, from int) int {
	depth := 0
	for index, token := range tokens {
		switch token.Raw {
		case "(", "[":
			depth++
			continue
		case ")", "]":
			depth--
			continue
		}
		if index >= from && depth == 0 && token.IsWord(word) {
			return index
		}
	}
	return -1
}

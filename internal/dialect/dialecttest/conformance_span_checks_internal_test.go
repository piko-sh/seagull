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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqllex"
)

func spanTestCorpus() *spanCorpus {
	return &spanCorpus{
		subjects: nil,
		problems: nil,
		info: dialect.Info{
			Lexical: sqllex.Rules{Parameters: sqllex.ParameterRules{NamedSigils: ":", QuestionMark: true, DollarNumbers: true}},
			Placeholders: dialect.Placeholders{Sigils: []dialect.PlaceholderSigil{
				{Prefix: "$", Kind: dialect.PlaceholderNumbered},
				{Prefix: "?", Kind: dialect.PlaceholderAnonymous},
				{Prefix: ":", Kind: dialect.PlaceholderNamed},
			}},
		},
	}
}

func spanTestSubject(t *testing.T, text string, analysis *ast.RawQueryAnalysis) spanSubject {
	t.Helper()

	tokens, err := significantTokens(spanTestCorpus().info.Lexical, text+";")
	require.NoError(t, err)
	return spanSubject{analysis: analysis, label: "test", source: text + ";", text: text, tokens: tokens[:len(tokens)-1], location: 0}
}

func spanOf(text, part string) ast.Span {
	start := strings.Index(text, part)
	return ast.Span{Start: start, End: start + len(part)}
}

func lastSpanOf(text, part string) ast.Span {
	start := strings.LastIndex(text, part)
	return ast.Span{Start: start, End: start + len(part)}
}

func TestPlaceholderSpanProblems(t *testing.T) {
	t.Parallel()

	text := "SELECT a FROM t WHERE a = $2 AND b = ? AND c = :name AND d = ?"
	complete := []ast.PlaceholderSpan{
		{Span: spanOf(text, "$2"), Number: 2},
		{Span: spanOf(text, "?"), Number: 1},
		{Span: spanOf(text, ":name"), Name: "name"},
		{Span: lastSpanOf(text, "?"), Number: 2},
	}
	testCases := []struct {
		name         string
		placeholders []ast.PlaceholderSpan
		problems     []string
	}{
		{name: "every placeholder covered and read", placeholders: complete, problems: nil},
		{name: "a placeholder left uncovered", placeholders: complete[:3], problems: []string{"the placeholder \"?\" at 61 is covered by 0 spans, want 1"}},
		{name: "a wrong number", placeholders: []ast.PlaceholderSpan{{Span: spanOf(text, "$2"), Number: 3}, complete[1], complete[2], complete[3]},
			problems: []string{"the placeholder \"$2\" records number 3, want 2"}},
		{name: "a wrong anonymous ordinal", placeholders: []ast.PlaceholderSpan{complete[0], complete[1], complete[2], {Span: lastSpanOf(text, "?"), Number: 1}},
			problems: []string{"the anonymous placeholder \"?\" records number 1, want its ordinal 2"}},
		{name: "a span over no placeholder", placeholders: append(append([]ast.PlaceholderSpan(nil), complete...), ast.PlaceholderSpan{Span: spanOf(text, "a")}),
			problems: []string{"the placeholder span \"a\" covers no placeholder"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := &ast.RawQueryAnalysis{Spans: ast.StatementSpans{Placeholders: testCase.placeholders}}
			require.Equal(t, testCase.problems, placeholderSpanProblems(spanTestCorpus(), spanTestSubject(t, text, analysis)))
		})
	}
}

func TestInListSpanProblems(t *testing.T) {
	t.Parallel()

	text := "SELECT a FROM t WHERE a NOT IN ($1) AND b IN ($2, $3)"
	sole := ast.ListSpan{Operator: spanOf(text, "NOT IN"), List: spanOf(text, "($1)"), Negated: true}
	testCases := []struct {
		name         string
		placeholders []ast.PlaceholderSpan
		problems     int
	}{
		{name: "the sole element records its list", placeholders: []ast.PlaceholderSpan{
			{Span: spanOf(text, "$1"), Number: 1, List: sole}, {Span: spanOf(text, "$2"), Number: 2}, {Span: spanOf(text, "$3"), Number: 3}}, problems: 0},
		{name: "the sole element records no list", placeholders: []ast.PlaceholderSpan{{Span: spanOf(text, "$1"), Number: 1}}, problems: 1},
		{name: "a list member that is not alone records a list", placeholders: []ast.PlaceholderSpan{{Span: spanOf(text, "$2"), Number: 2, List: sole}}, problems: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := &ast.RawQueryAnalysis{Spans: ast.StatementSpans{Placeholders: testCase.placeholders}}
			require.Len(t, inListSpanProblems(spanTestCorpus(), spanTestSubject(t, text, analysis)), testCase.problems)
		})
	}
}

func TestValuesTupleProblems(t *testing.T) {
	t.Parallel()

	text := "INSERT INTO t (a, b) VALUES ($1, f($2, 3)), (4, 5)"
	first := ast.TupleSpan{Span: spanOf(text, "($1, f($2, 3))"), Items: []ast.Span{spanOf(text, "$1"), spanOf(text, "f($2, 3)")}}
	second := ast.TupleSpan{Span: spanOf(text, "(4, 5)"), Items: []ast.Span{spanOf(text, "4"), spanOf(text, "5")}}
	testCases := []struct {
		name     string
		values   []ast.TupleSpan
		problems int
	}{
		{name: "rows and items separated by commas", values: []ast.TupleSpan{first, second}, problems: 0},
		{name: "an INSERT ... VALUES recording no rows", values: nil, problems: 1},
		{name: "an item swallowing the comma", values: []ast.TupleSpan{{Span: first.Span, Items: []ast.Span{{Start: first.Items[0].Start, End: first.Items[1].End}}}, second}, problems: 0},
		{name: "an item missing", values: []ast.TupleSpan{{Span: first.Span, Items: first.Items[:1]}, second}, problems: 1},
		{name: "a row that is not parenthesised", values: []ast.TupleSpan{{Span: spanOf(text, "$1, f($2, 3)"), Items: nil}}, problems: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := &ast.RawQueryAnalysis{DataChange: ast.DataChangeInsert, Spans: ast.StatementSpans{Values: testCase.values}}
			require.Len(t, valuesTupleProblems(spanTestCorpus(), spanTestSubject(t, text, analysis)), testCase.problems)
		})
	}
}

func TestClauseSpanProblems(t *testing.T) {
	t.Parallel()

	text := "SELECT DISTINCT a AS b FROM t WHERE a = 1 GROUP BY a HAVING count(*) > 1 ORDER BY a LIMIT 3"
	complete := ast.StatementSpans{
		Quantifier:      spanOf(text, "DISTINCT"),
		Projection:      spanOf(text, "a AS b"),
		ProjectionItems: []ast.ItemSpan{{Span: spanOf(text, "a AS b"), Expression: spanOf(text, "a AS"), Alias: spanOf(text, "AS b")}},
		From:            spanOf(text, "FROM t"),
		Where:           spanOf(text, "WHERE a = 1"),
		WhereCondition:  spanOf(text, "a = 1"),
		GroupBy:         spanOf(text, "GROUP BY a"),
		Having:          spanOf(text, "HAVING count(*) > 1"),
		OrderBy:         spanOf(text, "ORDER BY a"),
		Pagination:      spanOf(text, "LIMIT 3"),
		Terminator:      ast.Span{Start: len(text), End: len(text) + 1},
	}
	complete.ProjectionItems[0].Expression = ast.Span{Start: complete.Projection.Start, End: complete.Projection.Start + 1}
	testCases := []struct {
		name     string
		mutate   func(*ast.StatementSpans)
		problems int
	}{
		{name: "every clause recorded in order", mutate: func(*ast.StatementSpans) {}, problems: 0},
		{name: "a clause the statement writes left unrecorded", mutate: func(spans *ast.StatementSpans) { spans.Having = ast.Span{} }, problems: 1},
		{name: "clauses out of order", mutate: func(spans *ast.StatementSpans) { spans.Where, spans.GroupBy = spans.GroupBy, spans.Where }, problems: 2},
		{name: "a span outside the statement", mutate: func(spans *ast.StatementSpans) { spans.Pagination.End = len(text) + 4 }, problems: 1},
		{name: "a condition outside its clause", mutate: func(spans *ast.StatementSpans) { spans.WhereCondition = spans.From }, problems: 1},
		{name: "a terminator that is not one", mutate: func(spans *ast.StatementSpans) { spans.Terminator = spans.Pagination }, problems: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			spans := complete
			testCase.mutate(&spans)
			analysis := &ast.RawQueryAnalysis{Spans: spans, OutputColumns: []ast.RawOutputColumn{{Name: "b"}}}
			require.Len(t, clauseSpanProblems(spanTestCorpus(), spanTestSubject(t, text, analysis)), testCase.problems,
				"%v", clauseSpanProblems(spanTestCorpus(), spanTestSubject(t, text, analysis)))
		})
	}
}

func TestClauseSpanProblemsForACompoundQuery(t *testing.T) {
	t.Parallel()

	text := "SELECT a FROM t UNION SELECT b FROM u ORDER BY 1"
	branch := &ast.RawQueryAnalysis{}
	recorded := &ast.RawQueryAnalysis{
		CompoundBranches: []ast.RawCompoundBranch{{Query: branch, Operator: ast.CompoundUnion}},
		Spans:            ast.StatementSpans{OrderBy: spanOf(text, "ORDER BY 1"), From: spanOf(text, "FROM t")},
	}

	problems := clauseSpanProblems(spanTestCorpus(), spanTestSubject(t, text, recorded))

	require.Equal(t, []string{"a compound statement records From, which belongs to a branch"}, problems)
}

func TestProjectionItemProblems(t *testing.T) {
	t.Parallel()

	text := "SELECT t.*, a + 1 AS b FROM t"
	items := []ast.ItemSpan{
		{Span: spanOf(text, "t.*"), Expression: spanOf(text, "t.*"), Star: true},
		{Span: spanOf(text, "a + 1 AS b"), Expression: spanOf(text, "a + 1"), Alias: spanOf(text, "AS b")},
	}
	columns := []ast.RawOutputColumn{{IsStar: true, TableAlias: "t"}, {Name: "b"}}
	testCases := []struct {
		name     string
		items    []ast.ItemSpan
		problems int
	}{
		{name: "one item per output column", items: items, problems: 0},
		{name: "an item missing", items: items[:1], problems: 1},
		{name: "a star not marked", items: []ast.ItemSpan{{Span: items[0].Span, Expression: items[0].Expression}, items[1]}, problems: 1},
		{name: "an alias before its expression", items: []ast.ItemSpan{items[0], {Span: items[1].Span, Expression: items[1].Span, Alias: items[1].Alias}}, problems: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			analysis := &ast.RawQueryAnalysis{
				OutputColumns: columns,
				Spans:         ast.StatementSpans{Projection: spanOf(text, "t.*, a + 1 AS b"), ProjectionItems: testCase.items},
			}
			require.Len(t, projectionItemProblems(spanTestCorpus(), spanTestSubject(t, text, analysis)), testCase.problems)
		})
	}
}

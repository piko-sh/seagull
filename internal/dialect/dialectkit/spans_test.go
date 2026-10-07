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

package dialectkit_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqllex"
)

type spanKind uint8

const (
	spanWord spanKind = iota + 1
	spanParameter
	spanLeft
	spanRight
	spanOther
)

func spanTokens(t *testing.T, text string) []dialectkit.Token[spanKind] {
	t.Helper()

	lexical, err := sqllex.Tokens(sqllex.Rules{Parameters: sqllex.ParameterRules{NamedSigils: ":", QuestionMark: true, DollarNumbers: true}}, text)
	require.NoError(t, err)
	var tokens []dialectkit.Token[spanKind]
	for _, token := range lexical {
		if token.Kind.IsNoise() {
			continue
		}
		kind := spanOther
		switch {
		case token.Kind == sqllex.KindWord:
			kind = spanWord
		case token.Kind == sqllex.KindParameter:
			kind = spanParameter
		case token.Raw == "(":
			kind = spanLeft
		case token.Raw == ")":
			kind = spanRight
		}
		tokens = append(tokens, dialectkit.Token[spanKind]{Kind: kind, Value: token.Raw, Raw: token.Raw, Start: token.Start, End: token.End})
	}
	return tokens
}

func spanText(text string, offset int, span ast.Span) string {
	return text[offset+span.Start : offset+span.End]
}

func TestPlaceholderSpans(t *testing.T) {
	t.Parallel()

	prefix := "-- leading\n"
	statement := "SELECT a FROM t WHERE a NOT IN ($2) AND b IN (?, ?) AND c = :name AND d IN (?)"
	text := prefix + statement
	placeholders := dialect.Placeholders{
		Sigils: []dialect.PlaceholderSigil{
			{Prefix: "$", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered},
			{Prefix: "?", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderAnonymous},
			{Prefix: ":", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNamed},
		},
	}
	kinds := dialectkit.PlaceholderTokens[spanKind]{
		IsParameter:      func(token dialectkit.Token[spanKind]) bool { return token.Kind == spanParameter },
		Word:             spanWord,
		LeftParenthesis:  spanLeft,
		RightParenthesis: spanRight,
	}

	spans := dialectkit.PlaceholderSpans(spanTokens(t, text), kinds, placeholders, len(prefix))

	require.Len(t, spans, 5)
	require.Equal(t, "$2", spanText(text, len(prefix), spans[0].Span))
	require.Equal(t, 2, spans[0].Number)
	require.True(t, spans[0].List.Negated)
	require.Equal(t, "NOT IN", spanText(text, len(prefix), spans[0].List.Operator))
	require.Equal(t, "($2)", spanText(text, len(prefix), spans[0].List.List))
	require.Equal(t, 1, spans[1].Number)
	require.Equal(t, ast.ListSpan{}, spans[1].List)
	require.Equal(t, 2, spans[2].Number)
	require.Equal(t, "name", spans[3].Name)
	require.Equal(t, 3, spans[4].Number)
	require.False(t, spans[4].List.Negated)
	require.Equal(t, "IN", spanText(text, len(prefix), spans[4].List.Operator))
}

func TestPlaceholderSpansTypedForm(t *testing.T) {
	t.Parallel()

	text := "SELECT 1 WHERE id = {id:UInt64}"
	start := strings.Index(text, "{")
	tokens := []dialectkit.Token[spanKind]{{Kind: spanParameter, Value: "{id:UInt64}", Raw: "{id:UInt64}", Start: start, End: len(text)}}
	placeholders := dialect.Placeholders{
		Sigils: []dialect.PlaceholderSigil{{Prefix: "{", Suffix: "}", TypeSeparator: ":", Kind: dialect.PlaceholderNamed}},
	}
	kinds := dialectkit.PlaceholderTokens[spanKind]{
		IsParameter:      func(token dialectkit.Token[spanKind]) bool { return token.Kind == spanParameter },
		Word:             spanWord,
		LeftParenthesis:  spanLeft,
		RightParenthesis: spanRight,
	}

	spans := dialectkit.PlaceholderSpans(tokens, kinds, placeholders, 0)

	require.Len(t, spans, 1)
	require.Equal(t, "id", spans[0].Name)
	require.Equal(t, "UInt64", spanText(text, 0, spans[0].Type))
}

func TestTerminatorSpan(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		text     string
		rules    sqllex.Rules
		expected string
	}{
		{name: "a terminator after the last token", text: "SELECT 1;", expected: ";"},
		{name: "a terminator after a comment", text: "SELECT 1 /* done */ ;\nSELECT 2", expected: ";"},
		{name: "no terminator", text: "SELECT 1", expected: ""},
		{name: "another statement follows without a terminator", text: "SELECT 1 SELECT 2", expected: ""},
		{name: "a custom terminator", text: "SELECT 1 GO", rules: sqllex.Rules{Terminator: "GO"}, expected: "GO"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			span := dialectkit.TerminatorSpan(testCase.rules, testCase.text, 0, len("SELECT 1"))
			if testCase.expected == "" {
				require.False(t, span.Known())
				return
			}
			require.Equal(t, testCase.expected, spanText(testCase.text, 0, span))
		})
	}
}

func TestTokenRangeCoversTheTokensFromFirstUpToEnd(t *testing.T) {
	t.Parallel()

	const text = "SELECT a FROM t"
	tokens := spanTokens(t, text)
	cases := []struct {
		name     string
		first    int
		end      int
		wantText string
		wantOK   bool
	}{
		{name: "one token", first: 1, end: 2, wantText: "a", wantOK: true},
		{name: "several tokens", first: 2, end: 4, wantText: "FROM t", wantOK: true},
		{name: "an end past the last token stops there", first: 2, end: 99, wantText: "FROM t", wantOK: true},
		{name: "an empty range", first: 2, end: 2, wantText: "", wantOK: false},
		{name: "a negative first", first: -1, end: 2, wantText: "", wantOK: false},
		{name: "a first past the tokens", first: 9, end: 12, wantText: "", wantOK: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			start, stop, ok := dialectkit.TokenRange(tokens, testCase.first, testCase.end)
			span := dialectkit.TokenRangeSpan(tokens, testCase.first, testCase.end)

			require.Equal(t, testCase.wantOK, ok)
			require.Equal(t, testCase.wantText, text[start:stop])
			require.Equal(t, ast.Span{Start: start, End: stop}, span)
		})
	}
}

func TestRecordAndExtendTokenRangeRecordTheTopLevelClause(t *testing.T) {
	t.Parallel()

	const text = "SELECT a FROM t LIMIT 1 OFFSET 2"
	tokens := spanTokens(t, text)
	recorder := dialectkit.NewSpanRecorder(0)

	dialectkit.RecordTokenRange(recorder, dialectkit.SpanFrom, tokens, 2, 4)
	dialectkit.RecordTokenRange(recorder, dialectkit.SpanFrom, tokens, 0, 1)
	dialectkit.RecordTokenRange(recorder, dialectkit.SpanWhere, tokens, 4, 4)
	dialectkit.ExtendTokenRange(recorder, dialectkit.SpanPagination, tokens, 4, 6)
	dialectkit.ExtendTokenRange(recorder, dialectkit.SpanPagination, tokens, 6, 8)
	dialectkit.RecordTokenRange[spanKind](nil, dialectkit.SpanFrom, tokens, 2, 4)
	dialectkit.ExtendTokenRange[spanKind](nil, dialectkit.SpanFrom, tokens, 2, 4)
	spans := recorder.Finish(&ast.RawQueryAnalysis{})

	require.Equal(t, "FROM t", text[spans.From.Start:spans.From.End], "the first clause recorded wins")
	require.False(t, spans.Where.Known(), "an empty range records nothing")
	require.Equal(t, "LIMIT 1 OFFSET 2", text[spans.Pagination.Start:spans.Pagination.End])
}

func TestTokenItemSpanLocatesAnItemItsExpressionAndItsAlias(t *testing.T) {
	t.Parallel()

	const text = "SELECT b AS c, * FROM t"
	tokens := spanTokens(t, text)
	recorder := dialectkit.NewSpanRecorder(0)
	cases := []struct {
		name           string
		first          int
		expressionEnd  int
		end            int
		wantItem       string
		wantExpression string
		wantAlias      string
		star           bool
	}{
		{name: "an aliased item", first: 1, expressionEnd: 2, end: 4, wantItem: "b AS c", wantExpression: "b", wantAlias: "AS c"},
		{name: "a star item", first: 5, expressionEnd: 6, end: 6, star: true, wantItem: "*", wantExpression: "*", wantAlias: ""},
		{name: "an empty item", first: 4, expressionEnd: 4, end: 4, wantItem: "", wantExpression: "", wantAlias: ""},
		{name: "an expression end past the tokens", first: 6, expressionEnd: 40, end: 50, wantItem: "FROM t", wantExpression: "FROM t", wantAlias: ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			item := dialectkit.TokenItemSpan(recorder, tokens, testCase.first, testCase.expressionEnd, testCase.end, testCase.star)

			require.Equal(t, testCase.wantItem, text[item.Span.Start:item.Span.End])
			require.Equal(t, testCase.wantExpression, text[item.Expression.Start:item.Expression.End])
			require.Equal(t, testCase.wantAlias, text[item.Alias.Start:item.Alias.End])
			require.Equal(t, testCase.star && testCase.wantItem != "", item.Star)
		})
	}
}

func TestRecordOrderByPlaceAtRecordsTheEndOfTheTokenBefore(t *testing.T) {
	t.Parallel()

	const text = "SELECT a FROM t"
	tokens := spanTokens(t, text)
	cases := []struct {
		name     string
		position int
		want     ast.Span
	}{
		{name: "after the last token", position: 4, want: ast.Span{Start: len(text), End: len(text)}},
		{name: "after the first token", position: 1, want: ast.Span{Start: len("SELECT"), End: len("SELECT")}},
		{name: "before the first token", position: 0, want: ast.Span{}},
		{name: "past the tokens", position: 5, want: ast.Span{}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			recorder := dialectkit.NewSpanRecorder(0)

			dialectkit.RecordOrderByPlaceAt(recorder, tokens, testCase.position)

			require.Equal(t, testCase.want, recorder.Finish(&ast.RawQueryAnalysis{}).OrderByPlace)
		})
	}
}

func TestSourceBetweenGivesTheTextAsWritten(t *testing.T) {
	t.Parallel()

	const text = "SELECT  a,(b)   FROM t"
	tokens := spanTokens(t, text)
	cases := []struct {
		name   string
		source string
		first  int
		end    int
		want   string
	}{
		{name: "from the script", source: text, first: 1, end: 7, want: "a,(b)   FROM"},
		{name: "without the script", source: "", first: 1, end: 7, want: "a , ( b ) FROM"},
		{name: "from a script the tokens do not lie in", source: "short", first: 1, end: 7, want: "a , ( b ) FROM"},
		{name: "an end past the tokens", source: text, first: 6, end: 99, want: "FROM t"},
		{name: "an empty range", source: text, first: 3, end: 3, want: ""},
		{name: "a negative first", source: text, first: -1, end: 3, want: ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, dialectkit.SourceBetween(testCase.source, tokens, testCase.first, testCase.end))
		})
	}
}

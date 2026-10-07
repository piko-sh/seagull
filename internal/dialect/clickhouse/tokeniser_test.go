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

package clickhouse

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/clickhouse/lexical"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/dialecttest"
	"seagull.sh/seagull/internal/testkit/lexdiff"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func tokenise_t(t *testing.T, input string) []token {
	t.Helper()
	tokens, err := tokenise(input)
	require.NoError(t, err)
	return tokens
}

func TestTokeniser_EmptyInputEmitsOnlyEOF(t *testing.T) {
	t.Parallel()

	tokens := tokenise_t(t, "")
	require.Len(t, tokens, 1)
	assert.Equal(t, tokenEOF, tokens[0].Kind)
}

func TestTokeniser_Identifiers(t *testing.T) {
	t.Parallel()

	tokens := tokenise_t(t, "users user_id _name col2")
	kinds := tokenKinds(tokens)
	assert.Equal(t, []tokenKind{tokenIdentifier, tokenIdentifier, tokenIdentifier, tokenIdentifier, tokenEOF}, kinds)
	assert.Equal(t, "users", tokens[0].Value)
	assert.Equal(t, "user_id", tokens[1].Value)
	assert.Equal(t, "_name", tokens[2].Value)
	assert.Equal(t, "col2", tokens[3].Value)
}

func TestTokeniser_DoubleQuotedIdentifier(t *testing.T) {
	t.Parallel()

	tokens := tokenise_t(t, `"CamelCase"`)
	require.GreaterOrEqual(t, len(tokens), 1)
	assert.Equal(t, tokenIdentifier, tokens[0].Kind)
	assert.Equal(t, "CamelCase", tokens[0].Value)
}

func TestTokeniser_BacktickIdentifier(t *testing.T) {
	t.Parallel()

	tokens := tokenise_t(t, "`weird name`")
	require.GreaterOrEqual(t, len(tokens), 1)
	assert.Equal(t, tokenIdentifier, tokens[0].Kind)
	assert.Equal(t, "weird name", tokens[0].Value)
}

func TestTokeniser_DoubledBacktickEscape(t *testing.T) {
	t.Parallel()

	tokens := tokenise_t(t, "`with``embedded`")
	require.GreaterOrEqual(t, len(tokens), 1)
	assert.Equal(t, tokenIdentifier, tokens[0].Kind)
	assert.Equal(t, "with`embedded", tokens[0].Value)
}

func TestTokeniser_StringLiteralBasic(t *testing.T) {
	t.Parallel()

	tokens := tokenise_t(t, "'hello world'")
	require.GreaterOrEqual(t, len(tokens), 1)
	assert.Equal(t, tokenString, tokens[0].Kind)
	assert.Equal(t, "hello world", tokens[0].Value)
}

func TestTokeniser_StringDoubledQuoteEscape(t *testing.T) {
	t.Parallel()

	tokens := tokenise_t(t, "'O''Reilly'")
	require.GreaterOrEqual(t, len(tokens), 1)
	assert.Equal(t, tokenString, tokens[0].Kind)
	assert.Equal(t, "O'Reilly", tokens[0].Value)
}

func TestTokeniser_StringBackslashEscapes(t *testing.T) {
	t.Parallel()

	tokens := tokenise_t(t, `'tab\there\nnewline\\slash\'quote'`)
	require.GreaterOrEqual(t, len(tokens), 1)
	assert.Equal(t, tokenString, tokens[0].Kind)
	assert.Equal(t, "tab\there\nnewline\\slash'quote", tokens[0].Value)
}

func TestTokeniser_UnterminatedString(t *testing.T) {
	t.Parallel()

	_, err := tokenise("'unterminated")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unterminated string")
}

func TestTokeniser_Numbers(t *testing.T) {
	t.Parallel()

	tokens := tokenise_t(t, "1 42 3.14 1.5e10 1.5E-3")
	for index := range 5 {
		assert.Equal(t, tokenNumber, tokens[index].Kind, "token %d should be number", index)
	}
	assert.Equal(t, "1", tokens[0].Value)
	assert.Equal(t, "42", tokens[1].Value)
	assert.Equal(t, "3.14", tokens[2].Value)
	assert.Equal(t, "1.5e10", tokens[3].Value)
	assert.Equal(t, "1.5E-3", tokens[4].Value)
}

func TestTokeniser_HexAndBinaryNumbers(t *testing.T) {
	t.Parallel()

	tokens := tokenise_t(t, "0xFF 0xdeadbeef 0b1010 0o777")
	for index := range 4 {
		assert.Equal(t, tokenNumber, tokens[index].Kind, "token %d should be number", index)
	}
	assert.Equal(t, "0xFF", tokens[0].Value)
	assert.Equal(t, "0xdeadbeef", tokens[1].Value)
	assert.Equal(t, "0b1010", tokens[2].Value)
	assert.Equal(t, "0o777", tokens[3].Value)
}

func TestTokeniser_ClickHouseParameter(t *testing.T) {
	t.Parallel()

	tokens := tokenise_t(t, "{user_id:UInt64}")
	require.GreaterOrEqual(t, len(tokens), 1)
	assert.Equal(t, tokenClickHouseParam, tokens[0].Kind)
	assert.Equal(t, "user_id:UInt64", tokens[0].Value)
}

func TestTokeniser_ClickHouseParameterInContext(t *testing.T) {
	t.Parallel()

	tokens := tokenise_t(t, "SELECT id FROM users WHERE id = {uid:UInt32}")
	hasParameter := false
	for _, current := range tokens {
		if current.Kind == tokenClickHouseParam {
			hasParameter = true
			assert.Equal(t, "uid:UInt32", current.Value)
		}
	}
	assert.True(t, hasParameter, "expected to see a ClickHouse placeholder token")
}

func TestTokeniser_UnterminatedClickHouseParameter(t *testing.T) {
	t.Parallel()

	_, err := tokenise("{user_id:UInt64")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unterminated placeholder")
}

func TestTokeniser_LambdaArrow(t *testing.T) {
	t.Parallel()

	tokens := tokenise_t(t, "x -> x + 1")
	hasArrow := false
	for _, current := range tokens {
		if current.Kind == tokenArrow {
			hasArrow = true
			assert.Equal(t, "->", current.Value)
		}
	}
	assert.True(t, hasArrow, "expected lambda arrow token")
}

func TestTokeniser_CastOperator(t *testing.T) {
	t.Parallel()

	tokens := tokenise_t(t, "x::String")
	require.GreaterOrEqual(t, len(tokens), 3)
	assert.Equal(t, tokenIdentifier, tokens[0].Kind)
	assert.Equal(t, tokenCast, tokens[1].Kind)
	assert.Equal(t, "::", tokens[1].Value)
	assert.Equal(t, tokenIdentifier, tokens[2].Kind)
	assert.Equal(t, "String", tokens[2].Value)
}

func TestTokeniser_TwoCharOperators(t *testing.T) {
	t.Parallel()

	tokens := tokenise_t(t, "a <= b >= c <> d != e || f && g << h >> i")
	wantOperators := []string{"<=", ">=", "<>", "!=", "||", "&&", "<<", ">>"}
	foundOperators := []string{}
	for _, current := range tokens {
		if current.Kind == tokenOperator {
			foundOperators = append(foundOperators, current.Value)
		}
	}
	assert.Equal(t, wantOperators, foundOperators)
}

func TestTokeniser_LineComment(t *testing.T) {
	t.Parallel()

	tokens := tokenise_t(t, "SELECT -- comment\n  id")
	identifierTokens := 0
	for _, current := range tokens {
		if current.Kind == tokenIdentifier {
			identifierTokens++
		}
	}
	assert.Equal(t, 2, identifierTokens, "comment should not produce a token")
}

func TestTokeniser_BlockComment(t *testing.T) {
	t.Parallel()

	tokens := tokenise_t(t, "SELECT /* block\n comment */ id")
	identifierTokens := 0
	for _, current := range tokens {
		if current.Kind == tokenIdentifier {
			identifierTokens++
		}
	}
	assert.Equal(t, 2, identifierTokens)
}

func TestTokeniser_NestedBlockComment(t *testing.T) {
	t.Parallel()

	tokens := tokenise_t(t, "SELECT /* outer /* inner */ still in outer */ id")
	identifierTokens := 0
	for _, current := range tokens {
		if current.Kind == tokenIdentifier {
			identifierTokens++
		}
	}
	assert.Equal(t, 2, identifierTokens)
}

func TestTokeniser_Punctuation(t *testing.T) {
	t.Parallel()

	tokens := tokenise_t(t, "a.b,c;d(e)[f]*g")
	wantKinds := []tokenKind{
		tokenIdentifier, tokenDot, tokenIdentifier,
		tokenComma, tokenIdentifier,
		tokenSemicolon, tokenIdentifier,
		tokenLeftParen, tokenIdentifier, tokenRightParen,
		tokenLeftBracket, tokenIdentifier, tokenRightBracket,
		tokenStar, tokenIdentifier,
		tokenEOF,
	}
	assert.Equal(t, wantKinds, tokenKinds(tokens))
}

func TestTokeniser_UnexpectedCharacter(t *testing.T) {
	t.Parallel()

	_, err := tokenise("SELECT \x00 FROM users")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected character")
}

func TestTokeniser_FullSelectShape(t *testing.T) {
	t.Parallel()

	tokens := tokenise_t(t, "SELECT id, name FROM users WHERE id = {uid:UInt32} ORDER BY id LIMIT 10")

	wantIdentifierSubstrings := []string{"SELECT", "id", "name", "FROM", "users", "WHERE", "id", "ORDER", "BY", "id", "LIMIT"}
	identifierValues := []string{}
	for _, current := range tokens {
		if current.Kind == tokenIdentifier {
			identifierValues = append(identifierValues, current.Value)
		}
	}
	assert.Equal(t, wantIdentifierSubstrings, identifierValues)

	hasParameter := false
	for _, current := range tokens {
		if current.Kind == tokenClickHouseParam && current.Value == "uid:UInt32" {
			hasParameter = true
		}
	}
	assert.True(t, hasParameter, "expected the placeholder to be tokenised")
}

func tokenKinds(tokens []token) []tokenKind {
	kinds := make([]tokenKind, len(tokens))
	for index, current := range tokens {
		kinds[index] = current.Kind
	}
	return kinds
}

func TestTokeniser_NonASCIIIdentifierKeepsCodepoints(t *testing.T) {
	t.Parallel()

	tokens := tokenise_t(t, "é_col")

	require.GreaterOrEqual(t, len(tokens), 2, "expected at least one identifier token plus EOF")
	assert.Equal(t, tokenIdentifier, tokens[0].Kind)
	assert.Equal(t, "é_col", tokens[0].Value)
	assert.Equal(t, tokenEOF, tokens[len(tokens)-1].Kind)
}

func TestTokeniser_HexAndUnicodeEscapes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "hex", input: `'\x41\x42'`, want: "AB"},
		{name: "unicode escape", input: "'\\u00e9'", want: "é"},
		{name: "truncated hex falls back", input: `'\xZZ'`, want: "xZZ"},
		{name: "legacy single char", input: `'a\tb'`, want: "a\tb"},
	}
	for index := range cases {
		testCase := cases[index]
		t.Run(testCase.name, func(testRunner *testing.T) {
			testRunner.Parallel()
			tokens, err := tokenise(testCase.input)
			require.NoError(testRunner, err)
			require.NotEmpty(testRunner, tokens)
			assert.Equal(testRunner, testCase.want, tokens[0].Value)
		})
	}
}

func TestTokeniser_HashLineComments(t *testing.T) {
	t.Parallel()

	tokens := tokenise_t(t, "SELECT 1 # it's a note\n, 2")
	values := make([]string, 0, len(tokens))
	for _, current := range tokens {
		values = append(values, current.Value)
	}
	assert.Equal(t, []string{"SELECT", "1", ",", "2", ""}, values)
}

func TestTokeniser_BackslashEscapedIdentifiers(t *testing.T) {
	t.Parallel()

	tokens := tokenise_t(t, "`a\\`b` \"c\\\"d\" `e``f`")
	require.Len(t, tokens, 4)
	for index, expected := range []string{"a`b", `c"d`, "e`f"} {
		assert.Equal(t, tokenIdentifier, tokens[index].Kind)
		assert.Equal(t, expected, tokens[index].Value)
	}
}

func TestTokeniserReadsHeredocsAsStrings(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		input     string
		wantValue string
	}{
		{name: "an untagged heredoc", input: "$$a;b$$", wantValue: "a;b"},
		{name: "a tagged heredoc holding quotes and placeholders", input: "$tag$it's {x:UInt8} $1 $$ $tag$", wantValue: "it's {x:UInt8} $1 $$ "},
		{name: "a heredoc over lines", input: "$body$multi\nline$body$", wantValue: "multi\nline"},
		{name: "an empty heredoc", input: "$$$$", wantValue: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens := tokenise_t(t, testCase.input)
			require.Equal(t, []tokenKind{tokenString, tokenEOF}, tokenKinds(tokens))
			require.Equal(t, testCase.wantValue, tokens[0].Value)
			require.Equal(t, testCase.input, tokens[0].Raw)
		})
	}
}

func TestTokeniserRejectsAnUnterminatedHeredoc(t *testing.T) {
	t.Parallel()

	_, err := tokenise("SELECT $tag$never closed")
	require.ErrorContains(t, err, "unterminated heredoc")
}

func TestTokeniserTellsMapLiteralsFromPlaceholders(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		input     string
		wantKinds []tokenKind
		wantRaw   []string
	}{
		{
			name:      "a placeholder names its parameter before the colon",
			input:     "{ id : UInt64 }",
			wantKinds: []tokenKind{tokenClickHouseParam, tokenEOF},
			wantRaw:   []string{"{ id : UInt64 }", ""},
		},
		{
			name:  "a map literal with string keys",
			input: "{'a': 1, 'b}': 2}",
			wantKinds: []tokenKind{
				tokenLeftBrace, tokenString, tokenOperator, tokenNumber, tokenComma,
				tokenString, tokenOperator, tokenNumber, tokenRightBrace, tokenEOF,
			},
			wantRaw: []string{"{", "'a'", ":", "1", ",", "'b}'", ":", "2", "}", ""},
		},
		{
			name:      "a map literal with a number key",
			input:     "{1: 'x'}",
			wantKinds: []tokenKind{tokenLeftBrace, tokenNumber, tokenOperator, tokenString, tokenRightBrace, tokenEOF},
			wantRaw:   []string{"{", "1", ":", "'x'", "}", ""},
		},
		{
			name:      "a map literal with a boolean key",
			input:     "{true: 1}",
			wantKinds: []tokenKind{tokenLeftBrace, tokenIdentifier, tokenOperator, tokenNumber, tokenRightBrace, tokenEOF},
			wantRaw:   []string{"{", "true", ":", "1", "}", ""},
		},
		{
			name:  "a map literal whose values are spelt like type names",
			input: "{true: false, false: true}",
			wantKinds: []tokenKind{
				tokenLeftBrace, tokenIdentifier, tokenOperator, tokenIdentifier, tokenComma,
				tokenIdentifier, tokenOperator, tokenIdentifier, tokenRightBrace, tokenEOF,
			},
			wantRaw: []string{"{", "true", ":", "false", ",", "false", ":", "true", "}", ""},
		},
		{
			name:      "a map literal of literal words in any case",
			input:     "{TRUE: Null}",
			wantKinds: []tokenKind{tokenLeftBrace, tokenIdentifier, tokenOperator, tokenIdentifier, tokenRightBrace, tokenEOF},
			wantRaw:   []string{"{", "TRUE", ":", "Null", "}", ""},
		},
		{
			name:  "a map literal of literal words holding others",
			input: "{true: {false: nan}, false: {true: inf}}",
			wantKinds: []tokenKind{
				tokenLeftBrace, tokenIdentifier, tokenOperator, tokenLeftBrace, tokenIdentifier, tokenOperator, tokenIdentifier,
				tokenRightBrace, tokenComma, tokenIdentifier, tokenOperator, tokenLeftBrace, tokenIdentifier, tokenOperator,
				tokenIdentifier, tokenRightBrace, tokenRightBrace, tokenEOF,
			},
			wantRaw: []string{
				"{", "true", ":", "{", "false", ":", "nan", "}", ",", "false", ":", "{", "true", ":", "inf", "}", "}", "",
			},
		},
		{
			name:      "a placeholder named by a literal word",
			input:     "{true:UInt8}",
			wantKinds: []tokenKind{tokenClickHouseParam, tokenEOF},
			wantRaw:   []string{"{true:UInt8}", ""},
		},
		{
			name:      "a placeholder whose type is spelt like a literal word",
			input:     "{flag: true}",
			wantKinds: []tokenKind{tokenClickHouseParam, tokenEOF},
			wantRaw:   []string{"{flag: true}", ""},
		},
		{
			name:  "a map literal whose name key maps to a string and a list",
			input: "{a: 'x', b: [1]}",
			wantKinds: []tokenKind{
				tokenLeftBrace, tokenIdentifier, tokenOperator, tokenString, tokenComma,
				tokenIdentifier, tokenOperator, tokenLeftBracket, tokenNumber, tokenRightBracket, tokenRightBrace, tokenEOF,
			},
			wantRaw: []string{"{", "a", ":", "'x'", ",", "b", ":", "[", "1", "]", "}", ""},
		},
		{
			name:      "an empty map literal",
			input:     "{ }",
			wantKinds: []tokenKind{tokenLeftBrace, tokenRightBrace, tokenEOF},
			wantRaw:   []string{"{", "}", ""},
		},
		{
			name:  "a map literal holding another and a placeholder beside it",
			input: "({'a': {'b': 1}}, {p:UInt8})",
			wantKinds: []tokenKind{
				tokenLeftParen, tokenLeftBrace, tokenString, tokenOperator, tokenLeftBrace, tokenString, tokenOperator,
				tokenNumber, tokenRightBrace, tokenRightBrace, tokenComma, tokenClickHouseParam, tokenRightParen, tokenEOF,
			},
			wantRaw: []string{"(", "{", "'a'", ":", "{", "'b'", ":", "1", "}", "}", ",", "{p:UInt8}", ")", ""},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			tokens := tokenise_t(t, testCase.input)
			require.Equal(t, testCase.wantKinds, tokenKinds(tokens))
			raw := make([]string, len(tokens))
			for index, current := range tokens {
				raw[index] = current.Raw
			}
			require.Equal(t, testCase.wantRaw, raw)
		})
	}
}

func TestBraceDecidesFromTheFirstColonBraceOrNewlineAfterIt(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		input       string
		message     string
		placeholder bool
	}{
		{name: "a colon then a type", input: "{n: UInt8}", message: "", placeholder: true},
		{name: "a colon then a value", input: "{n: 1}", message: "", placeholder: false},
		{name: "a closing brace before any colon", input: "{n} + {'a': 1}", message: "placeholder is missing ':' separator", placeholder: true},
		{name: "an opening brace before any colon", input: "{n {m:UInt8}", message: "unexpected '{' inside placeholder", placeholder: true},
		{name: "a newline before any colon", input: "{n\n: UInt8}", message: "unexpected newline inside placeholder", placeholder: true},
		{name: "nothing after the name", input: "{n", message: "unterminated placeholder", placeholder: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			lexer := &tokeniser{input: testCase.input, position: 0, previous: tokenEOF, hasPrevious: false}
			assert.Equal(t, testCase.placeholder, lexer.opensPlaceholder())
			tokens, err := tokenise(testCase.input)
			if testCase.message == "" {
				require.NoError(t, err)
				return
			}
			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, testCase.message, parseError.Message)
			assert.Nil(t, tokens)
		})
	}
}

func TestTokenisingMapLiteralsOnOneLineIsLinear(t *testing.T) {
	t.Parallel()

	tokeniseRows := func(rows int) func() {
		input := singleLineMapLiteralInsert(rows)
		return func() {
			_, err := tokenise(input)
			require.NoError(t, err)
		}
	}
	scaling.Linear(t, tokeniseRows(20_000), tokeniseRows(80_000))
}

func BenchmarkTokeniseSingleLineMapLiteralInsert(b *testing.B) {
	input := singleLineMapLiteralInsert(50_000)
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for b.Loop() {
		_, err := tokenise(input)
		require.NoError(b, err)
	}
}

func singleLineMapLiteralInsert(rows int) string {
	var builder strings.Builder
	builder.WriteString("INSERT INTO t (m) VALUES ")
	for row := range rows {
		if row > 0 {
			builder.WriteByte(',')
		}
		_, _ = fmt.Fprintf(&builder, "({k%d: 1})", row)
	}
	return builder.String()
}

func TestTokeniserReadsLeadingDotNumbersWhereClickHouseDoes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		sql       string
		wantKinds []tokenKind
	}{
		{sql: ".5", wantKinds: []tokenKind{tokenNumber}},
		{sql: "SELECT .5", wantKinds: []tokenKind{tokenIdentifier, tokenDot, tokenNumber}},
		{sql: "(.25 + 1)", wantKinds: []tokenKind{tokenLeftParen, tokenNumber, tokenOperator, tokenNumber, tokenRightParen}},
		{sql: "pair.1", wantKinds: []tokenKind{tokenIdentifier, tokenDot, tokenNumber}},
		{sql: "f(x).2", wantKinds: []tokenKind{tokenIdentifier, tokenLeftParen, tokenIdentifier, tokenRightParen, tokenDot, tokenNumber}},
		{sql: "a[1].3", wantKinds: []tokenKind{tokenIdentifier, tokenLeftBracket, tokenNumber, tokenRightBracket, tokenDot, tokenNumber}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.sql, func(t *testing.T) {
			t.Parallel()
			tokens, err := tokenise(testCase.sql)
			require.NoError(t, err)
			kinds := make([]tokenKind, 0, len(tokens))
			for _, current := range tokens {
				if current.Kind != tokenEOF {
					kinds = append(kinds, current.Kind)
				}
			}
			assert.Equal(t, testCase.wantKinds, kinds)
		})
	}
}

func TestTokensCarryTheirSourceSpan(t *testing.T) {
	t.Parallel()

	tokens, err := tokenise("SELECT `order`, \"group\", 'it''s'")
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(tokens), 6)
	assert.Equal(t, "`order`", tokens[1].Raw)
	assert.True(t, tokens[1].Quoted)
	assert.Equal(t, "order", tokens[1].Value)
	assert.True(t, tokens[3].Quoted)
	assert.False(t, tokens[5].Quoted, "a string literal is not a delimited identifier")
	assert.Equal(t, "it's", tokens[5].Value)
	assert.Equal(t, tokens[5].Start+len(tokens[5].Raw), tokens[5].End)
}

func TestTokeniserSpansMatchSharedScanner(t *testing.T) {
	t.Parallel()

	files, err := lexdiff.Corpus("clickhouse")
	require.NoError(t, err)
	samples := []string{
		"SELECT 'a\\'b', 'c''d', `q``x`, \"r\"\"s\" /* a /* b */ */ -- c\nFROM t;",
		"SELECT {id:UInt64}, x::String, [1, 2] FROM t;",
		"INSERT INTO t VALUES ({ id : UInt64 }, {'a': 1, 'b}': 2}, [{1: 'x'}, {}]);",
		"INSERT INTO t VALUES ({true: 1, false: 0}, {a: 'x'}, {p: UInt8});",
		"INSERT INTO t VALUES ({true: false, false: {TRUE: NULL}}, {flag: true}, {true: UInt8}, {true: nan, false: inf});",
	}
	for index, sample := range samples {
		files = append(files, lexdiff.File{Path: "sample " + string(rune('a'+index)), Content: sample})
	}

	checked := 0
	var problems []string
	for _, file := range files {
		tokens, tokeniseErr := tokenise(file.Content)
		if tokeniseErr != nil {
			continue
		}
		checked++
		for _, problem := range dialecttest.CompareTokeniserSpans(lexical.Rules(), file.Content, spanTokens(file.Content, tokens),
			lexicalRules.Comments.SkipWhitespaceAndComments) {
			problems = append(problems, file.Path+": "+problem)
		}
	}
	require.Greater(t, checked, len(samples))
	require.Empty(t, problems)
}

func spanTokens(source string, tokens []token) []dialecttest.Token {
	result := make([]dialecttest.Token, 0, len(tokens))
	for _, current := range tokens {
		if current.Kind == tokenEOF {
			continue
		}
		result = append(result, dialecttest.Token{Position: current.Start, Class: spanClass(source, current)})
	}
	return result
}

func spanClass(source string, candidate token) dialecttest.TokenClass {
	switch candidate.Kind {
	case tokenIdentifier:
		if strings.IndexByte("`\"", source[candidate.Start]) >= 0 {
			return dialecttest.TokenQuotedIdentifier
		}
		return dialecttest.TokenWord
	case tokenString:
		return dialecttest.TokenString
	default:
		return dialecttest.TokenOther
	}
}

func tokenise(input string) ([]token, error) {
	next := statementTokens(input, 0, len(input))
	var tokens []token
	for {
		current, err := next()
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, current)
		if current.Kind == tokenEOF {
			return tokens, nil
		}
	}
}

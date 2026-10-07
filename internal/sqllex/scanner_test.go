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

package sqllex_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqllex"
)

type tokenView struct {
	raw  string
	kind sqllex.Kind
}

var standardRules = sqllex.Rules{}

var richRules = sqllex.Rules{
	ProceduralBlock: func(leadingWords []string) bool {
		return len(leadingWords) > 0 && leadingWords[0] == "CREATE"
	},
	Quotes: sqllex.QuoteRules{
		StringPrefixes:    "BX",
		SingleQuoteEscape: sqllex.EscapeDoubledOrBackslash,
		Backticks:         true,
		Brackets:          true,
		DollarQuoting:     true,
		EscapeStrings:     true,
	},
	Parameters: sqllex.ParameterRules{
		NamedSigils:          ":@",
		QuestionMark:         true,
		NumberedQuestionMark: true,
		DollarNumbers:        true,
		Braced:               true,
	},
	Comments: sqllex.CommentRules{
		DoubleDashRequiresWhitespace: true,
		HashLineComments:             true,
		NestedBlockComments:          true,
	},
	DelimiterCommand:             true,
	TerminatorOutsideParentheses: true,
	DollarInIdentifiers:          true,
}

func withRules(change func(rules *sqllex.Rules)) sqllex.Rules {
	rules := standardRules
	change(&rules)
	return rules
}

func significantViews(t *testing.T, rules sqllex.Rules, input string) []tokenView {
	t.Helper()
	tokens, err := sqllex.Tokens(rules, input)
	require.NoError(t, err)
	var views []tokenView
	for _, token := range tokens {
		require.Equal(t, input[token.Start:token.End], token.Raw)
		if !token.Kind.IsNoise() {
			views = append(views, tokenView{raw: token.Raw, kind: token.Kind})
		}
	}
	return views
}

func word(raw string) tokenView        { return tokenView{raw: raw, kind: sqllex.KindWord} }
func punctuation(raw string) tokenView { return tokenView{raw: raw, kind: sqllex.KindPunctuation} }
func str(raw string) tokenView         { return tokenView{raw: raw, kind: sqllex.KindString} }
func identifier(raw string) tokenView  { return tokenView{raw: raw, kind: sqllex.KindQuotedIdentifier} }
func number(raw string) tokenView      { return tokenView{raw: raw, kind: sqllex.KindNumber} }
func parameter(raw string) tokenView   { return tokenView{raw: raw, kind: sqllex.KindParameter} }

func TestScannerSignificantTokens(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		input    string
		expected []tokenView
		rules    sqllex.Rules
	}{
		{
			name:     "words and punctuation",
			rules:    standardRules,
			input:    "SELECT a, b FROM t;",
			expected: []tokenView{word("SELECT"), word("a"), punctuation(","), word("b"), word("FROM"), word("t"), punctuation(";")},
		},
		{
			name:     "doubled single quote",
			rules:    standardRules,
			input:    "'it''s;' x",
			expected: []tokenView{str("'it''s;'"), word("x")},
		},
		{
			name:     "standard strings ignore backslashes",
			rules:    standardRules,
			input:    `'a\' b`,
			expected: []tokenView{str(`'a\'`), word("b")},
		},
		{
			name:     "backslash escape keeps the string open",
			rules:    withRules(func(rules *sqllex.Rules) { rules.Quotes.SingleQuoteEscape = sqllex.EscapeBackslash }),
			input:    `'a\';b' c`,
			expected: []tokenView{str(`'a\';b'`), word("c")},
		},
		{
			name:     "backslash mode reads a doubled quote as two strings",
			rules:    withRules(func(rules *sqllex.Rules) { rules.Quotes.SingleQuoteEscape = sqllex.EscapeBackslash }),
			input:    "'a''b'",
			expected: []tokenView{str("'a'"), str("'b'")},
		},
		{
			name:     "doubled or backslash accepts both",
			rules:    withRules(func(rules *sqllex.Rules) { rules.Quotes.SingleQuoteEscape = sqllex.EscapeDoubledOrBackslash }),
			input:    `'a''b\'c'`,
			expected: []tokenView{str(`'a''b\'c'`)},
		},
		{
			name:     "double quotes are identifiers by default",
			rules:    standardRules,
			input:    `"a;""b"`,
			expected: []tokenView{identifier(`"a;""b"`)},
		},
		{
			name:     "double quotes can be strings",
			rules:    withRules(func(rules *sqllex.Rules) { rules.Quotes.DoubleQuote = sqllex.DoubleQuoteString }),
			input:    `"a;b"`,
			expected: []tokenView{str(`"a;b"`)},
		},
		{
			name: "double quotes can honour backslashes",
			rules: withRules(func(rules *sqllex.Rules) {
				rules.Quotes.DoubleQuoteEscape = sqllex.EscapeDoubledOrBackslash
			}),
			input:    `"a\";b"`,
			expected: []tokenView{identifier(`"a\";b"`)},
		},
		{
			name:     "backticks are punctuation unless enabled",
			rules:    standardRules,
			input:    "`a`",
			expected: []tokenView{punctuation("`"), word("a"), punctuation("`")},
		},
		{
			name:     "backticks quote identifiers when enabled",
			rules:    withRules(func(rules *sqllex.Rules) { rules.Quotes.Backticks = true }),
			input:    "`a;``b`",
			expected: []tokenView{identifier("`a;``b`")},
		},
		{
			name:     "brackets quote identifiers when enabled",
			rules:    withRules(func(rules *sqllex.Rules) { rules.Quotes.Brackets = true }),
			input:    "[a b;c] [d]",
			expected: []tokenView{identifier("[a b;c]"), identifier("[d]")},
		},
		{
			name:     "brackets are punctuation unless enabled",
			rules:    standardRules,
			input:    "a[1]",
			expected: []tokenView{word("a"), punctuation("["), number("1"), punctuation("]")},
		},
		{
			name:     "empty dollar tag",
			rules:    withRules(func(rules *sqllex.Rules) { rules.Quotes.DollarQuoting = true }),
			input:    "$$a;'b$$ c",
			expected: []tokenView{str("$$a;'b$$"), word("c")},
		},
		{
			name:     "named dollar tag ignores other tags",
			rules:    withRules(func(rules *sqllex.Rules) { rules.Quotes.DollarQuoting = true }),
			input:    "$body$ $$ $x$ ; $body$",
			expected: []tokenView{str("$body$ $$ $x$ ; $body$")},
		},
		{
			name: "dollar numbers are parameters, not tags",
			rules: withRules(func(rules *sqllex.Rules) {
				rules.Quotes.DollarQuoting = true
				rules.Parameters.DollarNumbers = true
			}),
			input:    "$1 $23",
			expected: []tokenView{parameter("$1"), parameter("$23")},
		},
		{
			name:     "an unclosed tag is punctuation",
			rules:    withRules(func(rules *sqllex.Rules) { rules.Quotes.DollarQuoting = true }),
			input:    "$tag x",
			expected: []tokenView{punctuation("$"), word("tag"), word("x")},
		},
		{
			name:     "escape strings honour backslashes",
			rules:    withRules(func(rules *sqllex.Rules) { rules.Quotes.EscapeStrings = true }),
			input:    `E'a\';b' e'c'`,
			expected: []tokenView{str(`E'a\';b'`), str("e'c'")},
		},
		{
			name:     "without escape strings E is a word",
			rules:    standardRules,
			input:    `E'a' x`,
			expected: []tokenView{word("E"), str("'a'"), word("x")},
		},
		{
			name:     "string prefixes join the literal",
			rules:    withRules(func(rules *sqllex.Rules) { rules.Quotes.StringPrefixes = "BX" }),
			input:    "X'0F' b'10' Y'1'",
			expected: []tokenView{str("X'0F'"), str("b'10'"), word("Y"), str("'1'")},
		},
		{
			name:     "a prefix letter inside a word stays in the word",
			rules:    withRules(func(rules *sqllex.Rules) { rules.Quotes.EscapeStrings = true }),
			input:    "type'x'",
			expected: []tokenView{word("type"), str("'x'")},
		},
		{
			name:     "double dash needs whitespace when asked",
			rules:    withRules(func(rules *sqllex.Rules) { rules.Comments.DoubleDashRequiresWhitespace = true }),
			input:    "a--b\nc -- d",
			expected: []tokenView{word("a"), punctuation("-"), punctuation("-"), word("b"), word("c")},
		},
		{
			name:     "hash comments when enabled",
			rules:    withRules(func(rules *sqllex.Rules) { rules.Comments.HashLineComments = true }),
			input:    "a # it's; here\nb",
			expected: []tokenView{word("a"), word("b")},
		},
		{
			name:     "hash is punctuation unless enabled",
			rules:    standardRules,
			input:    "a#b",
			expected: []tokenView{word("a"), punctuation("#"), word("b")},
		},
		{
			name:     "nested block comments",
			rules:    withRules(func(rules *sqllex.Rules) { rules.Comments.NestedBlockComments = true }),
			input:    "/* a /* b */ c; */ d",
			expected: []tokenView{word("d")},
		},
		{
			name:     "flat block comments close at the first closer",
			rules:    standardRules,
			input:    "/* a /* b */ c",
			expected: []tokenView{word("c")},
		},
		{
			name: "question mark parameters",
			rules: withRules(func(rules *sqllex.Rules) {
				rules.Parameters.QuestionMark = true
				rules.Parameters.NumberedQuestionMark = true
			}),
			input:    "? ?12",
			expected: []tokenView{parameter("?"), parameter("?12")},
		},
		{
			name:     "question marks are punctuation unless enabled",
			rules:    standardRules,
			input:    "?",
			expected: []tokenView{punctuation("?")},
		},
		{
			name:     "named parameters and doubled sigils",
			rules:    withRules(func(rules *sqllex.Rules) { rules.Parameters.NamedSigils = ":@" }),
			input:    "x::int :name @other : y",
			expected: []tokenView{word("x"), punctuation("::"), word("int"), parameter(":name"), parameter("@other"), punctuation(":"), word("y")},
		},
		{
			name: "named sigils after an operand are punctuation when the rules say so",
			rules: withRules(func(rules *sqllex.Rules) {
				rules.Parameters.NamedSigils = ":"
				rules.Parameters.NoNamedParameterAfterOperand = true
			}),
			input: "x:y 'k':v ]:w =:a (:b x :c",
			expected: []tokenView{
				word("x"), punctuation(":"), word("y"), str("'k'"), punctuation(":"), word("v"), punctuation("]"), punctuation(":"), word("w"),
				punctuation("="), parameter(":a"), punctuation("("), parameter(":b"), word("x"), parameter(":c"),
			},
		},
		{
			name:     "braced parameters",
			rules:    withRules(func(rules *sqllex.Rules) { rules.Parameters.Braced = true }),
			input:    "{id:Int32} {x}",
			expected: []tokenView{parameter("{id:Int32}"), punctuation("{"), word("x"), punctuation("}")},
		},
		{
			name:  "braces opening with no name before a colon are punctuation",
			rules: withRules(func(rules *sqllex.Rules) { rules.Parameters.Braced = true }),
			input: "{ id : Int32 } {'a': 1, 'b}': 2} {1: 'x'} {}",
			expected: []tokenView{
				parameter("{ id : Int32 }"),
				punctuation("{"), str("'a'"), punctuation(":"), number("1"), punctuation(","), str("'b}'"), punctuation(":"), number("2"), punctuation("}"),
				punctuation("{"), number("1"), punctuation(":"), str("'x'"), punctuation("}"),
				punctuation("{"), punctuation("}"),
			},
		},
		{
			name:  "braces whose colon no type name follows are punctuation",
			rules: withRules(func(rules *sqllex.Rules) { rules.Parameters.Braced = true }),
			input: "{true: 1} {a: 'x'} {p: UInt8}",
			expected: []tokenView{
				punctuation("{"), word("true"), punctuation(":"), number("1"), punctuation("}"),
				punctuation("{"), word("a"), punctuation(":"), str("'x'"), punctuation("}"),
				parameter("{p: UInt8}"),
			},
		},
		{
			name: "braces whose name and type are both literal words are punctuation",
			rules: withRules(func(rules *sqllex.Rules) {
				rules.Parameters.Braced = true
				rules.Parameters.BracedLiteralWords = []string{"true", "false", "null"}
			}),
			input: "{true: false, false: true} {TRUE: Null} {x: true} {true: UInt8}",
			expected: []tokenView{
				punctuation("{"), word("true"), punctuation(":"), word("false"), punctuation(","),
				word("false"), punctuation(":"), word("true"), punctuation("}"),
				punctuation("{"), word("TRUE"), punctuation(":"), word("Null"), punctuation("}"),
				parameter("{x: true}"),
				parameter("{true: UInt8}"),
			},
		},
		{
			name:     "numbers",
			rules:    standardRules,
			input:    "12 1.5e-3 .5 0x1F 1e x",
			expected: []tokenView{number("12"), number("1.5e-3"), number(".5"), number("0x1F"), number("1"), word("e"), word("x")},
		},
		{
			name:     "unicode words and punctuation",
			rules:    standardRules,
			input:    "naïve € 名前",
			expected: []tokenView{word("naïve"), punctuation("€"), word("名前")},
		},
		{
			name:     "dollar inside identifiers when allowed",
			rules:    withRules(func(rules *sqllex.Rules) { rules.DollarInIdentifiers = true; rules.Quotes.DollarQuoting = true }),
			input:    "amount$gbp$net",
			expected: []tokenView{word("amount$gbp$net")},
		},
		{
			name:     "dollar ends identifiers by default",
			rules:    standardRules,
			input:    "a$b",
			expected: []tokenView{word("a"), punctuation("$"), word("b")},
		},
		{
			name:     "invalid UTF-8 is one-byte punctuation",
			rules:    standardRules,
			input:    "a\xff",
			expected: []tokenView{word("a"), punctuation("\xff")},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.expected, significantViews(t, testCase.rules, testCase.input))
		})
	}
}

func TestScannerByteOrderMark(t *testing.T) {
	t.Parallel()

	mark := "\xef\xbb\xbf"
	tokens, err := sqllex.Tokens(standardRules, mark+"a"+mark)
	require.NoError(t, err)
	require.Equal(t, []sqllex.Token{
		{Raw: mark, Start: 0, End: 3, Kind: sqllex.KindWhitespace},
		{Raw: "a", Start: 3, End: 4, Kind: sqllex.KindWord},
		{Raw: mark, Start: 4, End: 7, Kind: sqllex.KindPunctuation},
	}, tokens)
	require.Equal(t, sqllex.ByteOrderMark, mark)
	require.Equal(t, "a", sqllex.TrimByteOrderMark(mark+"a"))
	require.Equal(t, "a", sqllex.TrimByteOrderMark("a"))
}

func TestScannerCoversInputExactly(t *testing.T) {
	t.Parallel()

	input := "SELECT /* c */ 'x', \"y\" -- tail\n FROM t;"
	tokens, err := sqllex.Tokens(richRules, input)
	require.NoError(t, err)
	offset := 0
	for _, token := range tokens {
		require.Equal(t, offset, token.Start)
		offset = token.End
	}
	require.Equal(t, len(input), offset)
}

func TestScannerNoiseTokens(t *testing.T) {
	t.Parallel()

	tokens, err := sqllex.Tokens(standardRules, " -- a\r\n/* b */x")
	require.NoError(t, err)
	kinds := make([]sqllex.Kind, 0, len(tokens))
	raws := make([]string, 0, len(tokens))
	for _, token := range tokens {
		kinds = append(kinds, token.Kind)
		raws = append(raws, token.Raw)
	}
	require.Equal(t, []sqllex.Kind{sqllex.KindWhitespace, sqllex.KindComment, sqllex.KindWhitespace, sqllex.KindComment, sqllex.KindWord}, kinds)
	require.Equal(t, []string{" ", "-- a", "\r\n", "/* b */", "x"}, raws)
}

func TestScannerCursor(t *testing.T) {
	t.Parallel()

	scanner := sqllex.NewScanner(standardRules, "  /* a */ b c")
	require.Equal(t, 0, scanner.Offset())
	require.Equal(t, 10, scanner.SkipNoise())
	require.Equal(t, 10, scanner.Offset())

	lookahead := *scanner
	peeked, ok := lookahead.NextSignificant()
	require.True(t, ok)
	require.Equal(t, "b", peeked.Raw)
	require.Equal(t, 10, scanner.Offset())

	first, ok := scanner.NextSignificant()
	require.True(t, ok)
	require.Equal(t, "b", first.Raw)
	second, ok := scanner.NextSignificant()
	require.True(t, ok)
	require.Equal(t, sqllex.Token{Raw: "c", Start: 12, End: 13, Kind: sqllex.KindWord}, second)
	_, ok = scanner.NextSignificant()
	require.False(t, ok)
	require.NoError(t, scanner.Err())
	require.Equal(t, 13, scanner.SkipNoise())
}

func TestScannerSkipNoiseStopsAtUnterminatedComment(t *testing.T) {
	t.Parallel()

	scanner := sqllex.NewScanner(standardRules, " /* open")
	require.Equal(t, len(" /* open"), scanner.SkipNoise())
	require.ErrorIs(t, scanner.Err(), sqllex.ErrUnterminatedComment)
}

func TestExecutableCommentKind(t *testing.T) {
	t.Parallel()

	tokens := sqllex.SignificantTokens(executableCommentRules, "/*!40101 SET NAMES utf8mb4 */")

	require.Len(t, tokens, 1)
	require.Equal(t, sqllex.KindExecutableComment, tokens[0].Kind)
	require.False(t, tokens[0].Kind.IsNoise())
	require.Equal(t, "executable comment", tokens[0].Kind.String())
}

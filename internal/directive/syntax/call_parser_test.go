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

package syntax

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
)

func runParseCallArguments(content string) (*CallArguments, []diag.SourceError) {
	lexer := lexerFromContent(content)
	errorBuilder := diagbuild.NewErrorBuilder("test.sql")
	return ParseCallArguments(lexer, errorBuilder, "acme")
}

func runParseValue(content string) (ParsedValue, diagbuild.TextSpan, []diag.SourceError) {
	lexer := lexerFromContent(content)
	errorBuilder := diagbuild.NewErrorBuilder("test.sql")
	return parseValue(lexer, errorBuilder, 0)
}

func TestParseCallArguments_EmptyParenthesisList(t *testing.T) {
	t.Parallel()

	call, diagnostics := runParseCallArguments("()")

	require.NotNil(t, call)
	assert.True(t, call.Closed)
	assert.Empty(t, call.Positionals)
	assert.Empty(t, call.KeywordArguments)
	assert.Empty(t, diagnostics)
}

func TestParseCallArguments_SinglePositional(t *testing.T) {
	t.Parallel()

	call, diagnostics := runParseCallArguments("(GetUser)")

	require.NotNil(t, call)
	require.Len(t, call.Positionals, 1)
	assert.Equal(t, "GetUser", call.Positionals[0].Value.Raw)
	assert.Empty(t, diagnostics)
}

func TestParseCallArguments_TwoPositionalsCommaSeparated(t *testing.T) {
	t.Parallel()

	call, diagnostics := runParseCallArguments("(GetUser, one)")

	require.NotNil(t, call)
	require.Len(t, call.Positionals, 2)
	assert.Equal(t, "GetUser", call.Positionals[0].Value.Raw)
	assert.Equal(t, "one", call.Positionals[1].Value.Raw)
	assert.Empty(t, diagnostics)
}

func TestParseCallArguments_PositionalAndKeyword(t *testing.T) {
	t.Parallel()

	call, diagnostics := runParseCallArguments("(GetUser, command: one)")

	require.NotNil(t, call)
	require.Len(t, call.Positionals, 1)
	assert.Equal(t, "GetUser", call.Positionals[0].Value.Raw)
	require.Len(t, call.KeywordArguments, 1)
	assert.Equal(t, "command", call.KeywordArguments[0].Key)
	assert.Equal(t, "one", call.KeywordArguments[0].Value.Raw)
	assert.Empty(t, diagnostics)
}

func TestParseCallArguments_PositionalAfterKeywordEmitsSyntax(t *testing.T) {
	t.Parallel()

	call, diagnostics := runParseCallArguments("(name: GetUser, extra)")

	require.NotNil(t, call)
	found := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == diag.CodeDirectiveSyntax {
			found = true
		}
	}
	assert.True(t, found, "expected directive-syntax diagnostic for positional after keyword")
}

func TestParseCallArguments_MissingOpenParenthesisEmitsSyntax(t *testing.T) {
	t.Parallel()

	call, diagnostics := runParseCallArguments("name: GetUser)")

	assert.Nil(t, call)
	require.NotEmpty(t, diagnostics)
	assert.Equal(t, diag.CodeDirectiveSyntax, diagnostics[0].Code)
}

func TestParseCallArguments_UnclosedCallEmitsUnclosed(t *testing.T) {
	t.Parallel()

	call, diagnostics := runParseCallArguments("(name: GetUser")

	require.NotNil(t, call)
	assert.False(t, call.Closed)
	found := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == diag.CodeUnclosedDirective {
			found = true
		}
	}
	assert.True(t, found, "expected unclosed-directive diagnostic")
}

func TestParseCallArguments_GarbageSeparatorRecovers(t *testing.T) {
	t.Parallel()

	call, diagnostics := runParseCallArguments("(name: GetUser ; command: one)")

	require.NotNil(t, call)
	found := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == diag.CodeDirectiveSyntax {
			found = true
		}
	}
	assert.True(t, found, "expected directive-syntax diagnostic for garbage separator")
}

func TestParseCallArguments_DottedValuePositionalJoinsWithDots(t *testing.T) {
	t.Parallel()

	call, diagnostics := runParseCallArguments("(orders.id)")

	require.NotNil(t, call)
	require.Len(t, call.Positionals, 1)
	assert.Equal(t, "orders.id", call.Positionals[0].Value.Raw)
	assert.Empty(t, diagnostics)
}

func TestParseSingleArgument_KeywordWithStringValue(t *testing.T) {
	t.Parallel()

	call, diagnostics := runParseCallArguments(`(go_type: "github.com/google/uuid.UUID")`)

	require.NotNil(t, call)
	require.Len(t, call.KeywordArguments, 1)
	assert.Equal(t, "go_type", call.KeywordArguments[0].Key)
	assert.Equal(t, "github.com/google/uuid.UUID", call.KeywordArguments[0].Value.Raw)
	assert.Empty(t, diagnostics)
}

func TestParseSingleArgument_NumericKeyword(t *testing.T) {
	t.Parallel()

	call, diagnostics := runParseCallArguments("(max: 100)")

	require.NotNil(t, call)
	require.Len(t, call.KeywordArguments, 1)
	assert.Equal(t, "100", call.KeywordArguments[0].Value.Raw)
	assert.Empty(t, diagnostics)
}

func TestParseSingleArgument_NegativeNumericKeyword(t *testing.T) {
	t.Parallel()

	call, diagnostics := runParseCallArguments("(default: -5)")

	require.NotNil(t, call)
	require.Len(t, call.KeywordArguments, 1)
	assert.Equal(t, "-5", call.KeywordArguments[0].Value.Raw)
	assert.Empty(t, diagnostics)
}

func TestParseValue_IdentifierValue(t *testing.T) {
	t.Parallel()

	value, _, diagnostics := runParseValue("orders")

	assert.Equal(t, "orders", value.Raw)
	assert.False(t, value.IsList)
	assert.Empty(t, diagnostics)
}

func TestParseValue_DottedIdentifier(t *testing.T) {
	t.Parallel()

	value, _, diagnostics := runParseValue("sgl.foo.bar")

	assert.Equal(t, "sgl.foo.bar", value.Raw)
	assert.False(t, value.IsList)
	assert.Empty(t, diagnostics)
}

func TestParseValue_StringLiteralStripsQuotes(t *testing.T) {
	t.Parallel()

	value, _, diagnostics := runParseValue(`"hello world"`)

	assert.Equal(t, "hello world", value.Raw)
	assert.False(t, value.IsList)
	assert.Empty(t, diagnostics)
}

func TestParseValue_NegativeNumberLiteral(t *testing.T) {
	t.Parallel()

	value, _, diagnostics := runParseValue("-123")

	assert.Equal(t, "-123", value.Raw)
	assert.Empty(t, diagnostics)
}

func TestParseValue_UnexpectedTokenEmitsSyntax(t *testing.T) {
	t.Parallel()

	_, _, diagnostics := runParseValue(":")

	require.NotEmpty(t, diagnostics)
	assert.Equal(t, diag.CodeDirectiveSyntax, diagnostics[0].Code)
}

func TestParseValue_DottedIdentifierTrailingDotEmitsSyntax(t *testing.T) {
	t.Parallel()

	value, _, diagnostics := runParseValue("sgl.")

	assert.Equal(t, "sgl", value.Raw)
	require.NotEmpty(t, diagnostics)
	assert.Equal(t, diag.CodeDirectiveSyntax, diagnostics[0].Code)
}

func TestParseValueKeepsTheQuotedPartsOfADottedName(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		input           string
		want            string
		wantDiagnostics int
	}{
		{name: "a quoted last part", input: `events."UserID"`, want: `events."UserID"`, wantDiagnostics: 0},
		{name: "a quoted first part", input: `"Events".user_id`, want: `"Events".user_id`, wantDiagnostics: 0},
		{name: "every part quoted, a quote inside doubled", input: `"my"."Ta""ble"."Id"`, want: `"my"."Ta""ble"."Id"`, wantDiagnostics: 0},
		{name: "a single-quoted part reads as a quoted name", input: `events.'UserID'`, want: `events."UserID"`, wantDiagnostics: 0},
		{name: "a string alone stays a plain value", input: `"events.UserID"`, want: "events.UserID", wantDiagnostics: 0},
		{name: "a dot followed by punctuation", input: `"events".:`, want: `"events"`, wantDiagnostics: 1},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			value, _, diagnostics := runParseValue(testCase.input)

			assert.Equal(t, testCase.want, value.Raw)
			assert.Len(t, diagnostics, testCase.wantDiagnostics)
		})
	}
}

func TestParseListValue_EmptyListShapeIsRecognised(t *testing.T) {
	t.Parallel()

	value, _, diagnostics := runParseValue("[]")

	assert.True(t, value.IsList)
	assert.Empty(t, value.AsList)
	assert.Empty(t, diagnostics)
}

func TestParseListValue_PopulatedListPreservesOrder(t *testing.T) {
	t.Parallel()

	value, _, diagnostics := runParseValue("[a, b, c]")

	assert.True(t, value.IsList)
	assert.Equal(t, []string{"a", "b", "c"}, value.AsList)
	assert.Empty(t, diagnostics)
}

func TestParseListValue_UnterminatedListResetsAndDiagnoses(t *testing.T) {
	t.Parallel()

	value, _, diagnostics := runParseValue("[a, b")

	assert.False(t, value.IsList, "unterminated list must reset isList")
	assert.Empty(t, value.AsList)
	found := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == diag.CodeUnclosedDirective {
			found = true
		}
	}
	assert.True(t, found, "expected Q032 for unterminated list")
}

func TestParseListValue_MismatchedCloserResetsAndDiagnoses(t *testing.T) {
	t.Parallel()

	value, _, diagnostics := runParseValue("[a, b)")

	assert.False(t, value.IsList, "mismatched closer must reset isList")
	assert.Empty(t, value.AsList)
	found := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == diag.CodeInvalidListLiteral {
			found = true
		}
	}
	assert.True(t, found, "expected Q034 for mismatched closer")
}

func TestConsumeUntilListBoundary_StopsAtComma(t *testing.T) {
	t.Parallel()

	lexer := lexerFromContent("garbage,next")
	consumeUntilListBoundary(lexer)

	peek := lexer.Peek()
	assert.Equal(t, tokenComma, peek.Kind, "consume should leave the comma as next")
}

func TestConsumeUntilListBoundary_StopsAtRBracket(t *testing.T) {
	t.Parallel()

	lexer := lexerFromContent("garbage]")
	consumeUntilListBoundary(lexer)

	peek := lexer.Peek()
	assert.Equal(t, tokenRBracket, peek.Kind)
}

func TestConsumeUntilListBoundary_StopsAtEOF(t *testing.T) {
	t.Parallel()

	lexer := lexerFromContent("garbage_only")
	consumeUntilListBoundary(lexer)

	peek := lexer.Peek()
	assert.Equal(t, TokenEOF, peek.Kind)
}

func TestDirectiveValueNestingIsBounded(t *testing.T) {
	t.Parallel()

	const depth = 50_000
	content := strings.Repeat("[", depth) + strings.Repeat("]", depth)
	_, _, diagnostics := runParseValue(content)
	require.NotEmpty(t, diagnostics, "deep nesting should produce a diagnostic rather than crash")
	containsDepthDiagnostic := false
	for _, diagnostic := range diagnostics {
		if strings.Contains(diagnostic.Message, "too deep") {
			containsDepthDiagnostic = true
			break
		}
	}
	assert.True(t, containsDepthDiagnostic, "expected a value-nesting-too-deep diagnostic")
}

func TestParseCallArguments_ValueWhereASeparatorBelongsMarksTheArgumentFailed(t *testing.T) {
	t.Parallel()

	call, diagnostics := runParseCallArguments("(GetAuthor, one, readonly true false, nullable: [a, b] x)")

	require.Len(t, diagnostics, 2)
	require.Len(t, call.Positionals, 3)
	assert.False(t, call.Positionals[1].Failed)
	assert.True(t, call.Positionals[2].Failed)
	assert.Equal(t, "readonly", call.Positionals[2].Value.Raw)
	require.Len(t, call.KeywordArguments, 1)
	assert.True(t, call.KeywordArguments[0].Failed)
	assert.True(t, call.Closed)
}

func TestParseCallArguments_PunctuationWhereASeparatorBelongsSeparates(t *testing.T) {
	t.Parallel()

	call, diagnostics := runParseCallArguments("(name: GetUser ; command: one)")

	require.Len(t, diagnostics, 1)
	require.Len(t, call.KeywordArguments, 2)
	assert.False(t, call.KeywordArguments[0].Failed)
	assert.Equal(t, "command", call.KeywordArguments[1].Key)
}

func TestParseCallArgumentsCapsTheDiagnosticsOfOneCall(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		content    string
		wantNotice bool
	}{
		{name: "a few stray tokens are all reported", content: "(name: GetUser ; ;)", wantNotice: false},
		{name: "many stray tokens are summarised", content: "(name: GetUser" + strings.Repeat(" ;", 5000) + ")", wantNotice: true},
		{name: "many stray tokens in an unclosed call are summarised", content: "(name: GetUser" + strings.Repeat(" ;", 5000), wantNotice: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			call, diagnostics := runParseCallArguments(testCase.content)

			require.NotNil(t, call)
			require.Len(t, call.KeywordArguments, 1)
			if !testCase.wantNotice {
				assert.LessOrEqual(t, len(diagnostics), maxDiagnosticsPerCall)
				assert.NotContains(t, codesOf(diagnostics), diag.CodeDiagnosticLimit)
				return
			}
			require.Len(t, diagnostics, maxDiagnosticsPerCall+1)
			notice := diagnostics[maxDiagnosticsPerCall]
			assert.Equal(t, diag.CodeDiagnosticLimit, notice.Code)
			assert.Equal(t, diag.SeverityError, notice.Severity)
			assert.Contains(t, notice.Message, "more diagnostics were left out")
			assert.Equal(t, 1, notice.Column)
		})
	}
}

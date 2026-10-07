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

package pgext_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/postgres/pgext"
)

func TestTokenKindsHaveFixedValuesAndNames(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		kind      pgext.TokenKind
		wantValue uint8
		wantName  string
	}{
		{kind: pgext.TokenInvalid, wantValue: 0, wantName: "invalid"},
		{kind: pgext.TokenIdentifier, wantValue: 1, wantName: "identifier"},
		{kind: pgext.TokenNumber, wantValue: 2, wantName: "number"},
		{kind: pgext.TokenString, wantValue: 3, wantName: "string"},
		{kind: pgext.TokenEscapeString, wantValue: 4, wantName: "escape string"},
		{kind: pgext.TokenDollarString, wantValue: 5, wantName: "dollar-quoted string"},
		{kind: pgext.TokenBitString, wantValue: 6, wantName: "bit string"},
		{kind: pgext.TokenOperator, wantValue: 7, wantName: "operator"},
		{kind: pgext.TokenLeftParen, wantValue: 8, wantName: "left parenthesis"},
		{kind: pgext.TokenRightParen, wantValue: 9, wantName: "right parenthesis"},
		{kind: pgext.TokenLeftBracket, wantValue: 10, wantName: "left bracket"},
		{kind: pgext.TokenRightBracket, wantValue: 11, wantName: "right bracket"},
		{kind: pgext.TokenComma, wantValue: 12, wantName: "comma"},
		{kind: pgext.TokenSemicolon, wantValue: 13, wantName: "semicolon"},
		{kind: pgext.TokenDot, wantValue: 14, wantName: "dot"},
		{kind: pgext.TokenStar, wantValue: 15, wantName: "star"},
		{kind: pgext.TokenPositionalParameter, wantValue: 16, wantName: "positional parameter"},
		{kind: pgext.TokenNamedParameter, wantValue: 17, wantName: "named parameter"},
		{kind: pgext.TokenCast, wantValue: 18, wantName: "cast"},
		{kind: pgext.TokenArrow, wantValue: 19, wantName: "arrow"},
		{kind: pgext.TokenDoubleArrow, wantValue: 20, wantName: "double arrow"},
		{kind: pgext.TokenHashArrow, wantValue: 21, wantName: "hash arrow"},
		{kind: pgext.TokenHashDoubleArrow, wantValue: 22, wantName: "hash double arrow"},
		{kind: pgext.TokenEOF, wantValue: 23, wantName: "end of statement"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.wantName, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.wantValue, uint8(testCase.kind))
			require.Equal(t, testCase.wantName, testCase.kind.String())
		})
	}
}

func TestTokenKindStringNamesAnUnknownValue(t *testing.T) {
	t.Parallel()

	require.Equal(t, "TokenKind(99)", pgext.TokenKind(99).String())
}

func TestTokenKeywordMatchingRejectsQuotedIdentifiers(t *testing.T) {
	t.Parallel()

	bare := pgext.Token{Kind: pgext.TokenIdentifier, Value: "select", Raw: "select"}
	quoted := pgext.Token{Kind: pgext.TokenIdentifier, Value: "select", Raw: `"select"`, Quoted: true}

	require.True(t, bare.IsKeyword(pgext.TokenIdentifier, "SELECT"))
	require.False(t, quoted.IsKeyword(pgext.TokenIdentifier, "SELECT"))
	require.Empty(t, quoted.Keyword(pgext.TokenIdentifier))
}

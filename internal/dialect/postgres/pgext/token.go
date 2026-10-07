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

package pgext

import (
	"strconv"

	"seagull.sh/seagull/internal/dialect/dialectkit"
)

const (
	// TokenInvalid is the zero TokenKind, which no token the dialect produces carries.
	TokenInvalid TokenKind = 0

	// TokenIdentifier is a word: a keyword, or a bare or double-quoted identifier. A
	// double-quoted identifier has Quoted set and never matches a keyword.
	TokenIdentifier TokenKind = 1

	// TokenNumber is a numeric literal, such as 42, 1.5, .5, 1e3 or 0x1F.
	TokenNumber TokenKind = 2

	// TokenString is a single-quoted string literal. Value holds its text with the quotes
	// removed and doubled quotes collapsed.
	TokenString TokenKind = 3

	// TokenEscapeString is an E'...' escape string literal. Value holds its decoded text;
	// Raw holds it as written.
	TokenEscapeString TokenKind = 4

	// TokenDollarString is a dollar-quoted string literal, such as $$...$$ or $tag$...$tag$.
	// Value holds only the text between the delimiters.
	TokenDollarString TokenKind = 5

	// TokenBitString is a B'...' bit string literal. Value holds the bits.
	TokenBitString TokenKind = 6

	// TokenOperator is an operator symbol, such as =, <>, ||, @> or !~*, and the bare colon.
	TokenOperator TokenKind = 7

	// TokenLeftParen is "(".
	TokenLeftParen TokenKind = 8

	// TokenRightParen is ")".
	TokenRightParen TokenKind = 9

	// TokenLeftBracket is "[".
	TokenLeftBracket TokenKind = 10

	// TokenRightBracket is "]".
	TokenRightBracket TokenKind = 11

	// TokenComma is ",".
	TokenComma TokenKind = 12

	// TokenSemicolon is ";".
	TokenSemicolon TokenKind = 13

	// TokenDot is ".".
	TokenDot TokenKind = 14

	// TokenStar is "*".
	TokenStar TokenKind = 15

	// TokenPositionalParameter is a numbered parameter placeholder such as $1.
	TokenPositionalParameter TokenKind = 16

	// TokenNamedParameter is a named parameter placeholder such as :name.
	TokenNamedParameter TokenKind = 17

	// TokenCast is the "::" cast operator.
	TokenCast TokenKind = 18

	// TokenArrow is the "->" JSON access operator.
	TokenArrow TokenKind = 19

	// TokenDoubleArrow is the "->>" JSON access operator.
	TokenDoubleArrow TokenKind = 20

	// TokenHashArrow is the "#>" JSON path operator.
	TokenHashArrow TokenKind = 21

	// TokenHashDoubleArrow is the "#>>" JSON path operator.
	TokenHashDoubleArrow TokenKind = 22

	// TokenEOF marks the end of the statement.
	TokenEOF TokenKind = 23
)

// kindNames holds the name of each kind, in the order of the kinds' values from
// TokenInvalid to TokenEOF.
var kindNames = [TokenEOF + 1]string{
	"invalid", "identifier", "number", "string", "escape string", "dollar-quoted string",
	"bit string", "operator", "left parenthesis", "right parenthesis", "left bracket",
	"right bracket", "comma", "semicolon", "dot", "star", "positional parameter",
	"named parameter", "cast", "arrow", "double arrow", "hash arrow", "hash double arrow",
	"end of statement",
}

// TokenKind classifies a token of the postgres dialect. Each kind has a fixed value that
// never changes.
type TokenKind uint8

// String returns the kind's name, such as "identifier" or "left parenthesis".
//
// Returns string which is the name, or "TokenKind(N)" for a value no kind has.
func (k TokenKind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return "TokenKind(" + strconv.Itoa(int(k)) + ")"
}

// Token is one token of a postgres statement: its kind, its decoded Value, its Raw source
// text, its Start and End byte offsets in the script, and whether it is a Quoted
// identifier.
type Token = dialectkit.Token[TokenKind]

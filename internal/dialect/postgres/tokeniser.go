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

package postgres

import (
	"strings"
	"unicode/utf8"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/postgres/pgext"
)

const (
	// tokenIdentifier is a bare or double-quoted identifier.
	tokenIdentifier = pgext.TokenIdentifier

	// tokenNumber is a numeric literal.
	tokenNumber = pgext.TokenNumber

	// tokenString is a single-quoted string literal.
	tokenString = pgext.TokenString

	// tokenEscapeString is an E'...' escape string literal.
	tokenEscapeString = pgext.TokenEscapeString

	// tokenDollarString is a dollar-quoted string literal.
	tokenDollarString = pgext.TokenDollarString

	// tokenBitString is a B'...' bit string literal.
	tokenBitString = pgext.TokenBitString

	// tokenOperator is a SQL operator token.
	tokenOperator = pgext.TokenOperator

	// tokenLeftParen is the '(' token.
	tokenLeftParen = pgext.TokenLeftParen

	// tokenRightParen is the ')' token.
	tokenRightParen = pgext.TokenRightParen

	// tokenLeftBracket is the '[' token.
	tokenLeftBracket = pgext.TokenLeftBracket

	// tokenRightBracket is the ']' token.
	tokenRightBracket = pgext.TokenRightBracket

	// tokenComma is the ',' token.
	tokenComma = pgext.TokenComma

	// tokenSemicolon is the ';' token.
	tokenSemicolon = pgext.TokenSemicolon

	// tokenDot is the '.' token.
	tokenDot = pgext.TokenDot

	// tokenStar is the '*' token.
	tokenStar = pgext.TokenStar

	// tokenDollarParam is a positional parameter such as $1.
	tokenDollarParam = pgext.TokenPositionalParameter

	// tokenNamedParam is a named parameter such as :name.
	tokenNamedParam = pgext.TokenNamedParameter

	// tokenCast is the '::' cast operator.
	tokenCast = pgext.TokenCast

	// tokenArrow is the '->' JSON access operator.
	tokenArrow = pgext.TokenArrow

	// tokenDoubleArrow is the '->>' JSON access operator.
	tokenDoubleArrow = pgext.TokenDoubleArrow

	// tokenHashArrow is the '#>' JSON path operator.
	tokenHashArrow = pgext.TokenHashArrow

	// tokenHashDoubleArrow is the '#>>' JSON path operator.
	tokenHashDoubleArrow = pgext.TokenHashDoubleArrow

	// tokenEOF marks the end of input.
	tokenEOF = pgext.TokenEOF

	// caseInsensitiveNotMatchOperator is the three-character operator that negates a
	// case-insensitive POSIX regular expression match.
	caseInsensitiveNotMatchOperator = "!~*"
)

var (
	// lexicalRules declares Postgres lexical rules for the shared scanners: nested block
	// comments and 0x/0o/0b base-prefixed integer literals.
	lexicalRules = dialectkit.LexicalRules{
		Comments: dialectkit.CommentRules{
			NestedBlockComments:          true,
			DoubleDashRequiresWhitespace: false,
			HashLineComment:              false,
		},
		Numbers: dialectkit.NumberRules{
			HexPrefix:                true,
			OctalPrefix:              true,
			BinaryPrefix:             true,
			RequireDigitsAfterPrefix: false,
		},
	}

	// distanceOperatorSpellings holds the distance operators, longest first so the longest
	// that matches is read: the geometric and full-text <->, the trigram distances of
	// pg_trgm, the vector distances of pgvector and the PostGIS distances.
	distanceOperatorSpellings = []string{
		"<<<->", "<->>>", "<<->>", "<<#>>",
		"<<->", "<->>",
		"<->", "<#>", "<=>", "<+>", "<~>", "<%>", "|=|",
	}

	// twoCharacterOperators holds the set of two-character SQL operators the tokeniser
	// recognises.
	twoCharacterOperators = map[string]bool{
		"<=": true, ">=": true, "<>": true, "!=": true, "||": true,
		"<<": true, ">>": true, "&&": true, "@>": true, "<@": true,
		"~*": true, "!~": true,
		"@@": true, "=>": true,
		"?|": true, "?&": true, "@?": true, "#-": true,
		"//": true,
	}
)

// tokenKind is the postgres token classification, shared with extensions through pgext.
type tokenKind = pgext.TokenKind

var (
	// singleCharTokens maps an ASCII byte to its token kind; the zero kind, TokenInvalid,
	// means the byte has no single-character token.
	singleCharTokens = [256]tokenKind{
		'(': tokenLeftParen,
		')': tokenRightParen,
		'[': tokenLeftBracket,
		']': tokenRightBracket,
		',': tokenComma,
		';': tokenSemicolon,
		'.': tokenDot,
		'*': tokenStar,
	}
)

// token is a single lexical token produced by the tokeniser.
type token = pgext.Token

// tokeniser scans an input SQL string and emits tokens on demand.
type tokeniser struct {
	// input is the source SQL being scanned.
	input string

	// position is the current byte offset within input.
	position int
}

// next advances and returns the next token from the input.
//
// Returns token which is the scanned token.
// Returns error when a lexical error occurs.
func (t *tokeniser) next() (token, error) {
	if err := t.skipWhitespaceAndComments(); err != nil {
		return token{}, err
	}

	if t.position >= len(t.input) {
		return t.emit(tokenEOF, t.position, ""), nil
	}

	if dialectkit.StartsNumber(t.input, t.position) {
		return t.readNumber()
	}

	character := t.input[t.position]

	if produced, ok := t.readSingleCharToken(character); ok {
		return produced, nil
	}

	return t.readMultiCharToken(character)
}

// emit builds the token that began at start and ends at the cursor, slicing its raw text
// from the source.
//
// Takes kind (tokenKind) which classifies the token.
// Takes start (int) which is the byte offset of the token's first byte.
// Takes value (string) which is the token's decoded text.
//
// Returns token which carries the kind, value, raw text and byte span.
func (t *tokeniser) emit(kind tokenKind, start int, value string) token {
	return token{Value: value, Raw: t.input[start:t.position], Start: start, End: t.position, Kind: kind, Quoted: false}
}

// readSingleCharToken returns the single-character token for character if one is mapped.
//
// Takes character (byte) which is the candidate byte to classify.
//
// Returns token which is the produced token when ok is true.
// Returns bool which is true when character maps to a single-char token.
func (t *tokeniser) readSingleCharToken(character byte) (token, bool) {
	mapped := singleCharTokens[character]
	if mapped == pgext.TokenInvalid {
		return token{}, false
	}
	startPosition := t.position
	t.position++
	return t.emit(mapped, startPosition, string(character)), true
}

// readMultiCharToken dispatches to the appropriate multi-character reader.
//
// Takes character (byte) which is the leading byte of the token.
//
// Returns token which is the scanned token.
// Returns error when no reader accepts the input.
func (t *tokeniser) readMultiCharToken(character byte) (token, error) {
	switch character {
	case '$':
		return t.readDollarToken()
	case ':':
		return t.readColonToken()
	case '\'':
		return t.readString()
	case '"':
		return t.readQuotedIdentifier()
	}
	if literal, prefixed, err := t.readPrefixedLiteral(character); prefixed {
		return literal, err
	}

	leading, _ := utf8.DecodeRuneInString(t.input[t.position:])
	if isIdentifierStart(leading) {
		return t.readIdentifier()
	}
	return t.readOperator()
}

// skipWhitespaceAndComments advances past whitespace and comments.
//
// Returns error when a block comment is left unterminated.
func (t *tokeniser) skipWhitespaceAndComments() error {
	position, err := lexicalRules.Comments.SkipWhitespaceAndComments(t.input, t.position)
	t.position = position
	return err
}

// readString reads a single-quoted string literal.
//
// Returns token which is the string literal token.
// Returns error when the literal is unterminated.
func (t *tokeniser) readString() (token, error) {
	return t.readDelimitedLiteral('\'', tokenString, "string literal")
}

// readDelimitedLiteral reads a literal delimited by delimiter on both sides.
//
// Doubled delimiters inside the literal are treated as escaped delimiters.
//
// Takes delimiter (byte) which opens and closes the literal.
// Takes kind (tokenKind) which classifies the produced token.
// Takes errorDescription (string) which names the literal in error messages.
//
// Returns token which is the produced literal token.
// Returns error when the literal is unterminated.
func (t *tokeniser) readDelimitedLiteral(delimiter byte, kind tokenKind, errorDescription string) (token, error) {
	startPosition := t.position

	value, position, ok := dialectkit.ScanDoubledDelimiter(t.input, t.position, delimiter)
	if !ok {
		return token{}, dialectkit.ParseErrorAt(t.input, startPosition, startPosition, "unterminated "+errorDescription)
	}
	t.position = position

	return t.emit(kind, startPosition, value), nil
}

// readBitString reads a B'...' or X'...' bit string literal.
//
// Returns token which is the bit string token.
// Returns error when the literal is unterminated.
func (t *tokeniser) readBitString() (token, error) {
	startPosition := t.position
	t.position += 2

	var builder strings.Builder
	for t.position < len(t.input) {
		character := t.input[t.position]
		if character == '\'' {
			t.position++
			return t.emit(tokenBitString, startPosition, builder.String()), nil
		}
		builder.WriteByte(character)
		t.position++
	}

	return token{}, dialectkit.ParseErrorAt(t.input, startPosition, startPosition, "unterminated bit string")
}

// readQuotedIdentifier reads a double-quoted identifier, which is always a name and never
// a keyword.
//
// Returns token which is the identifier token.
// Returns error when the identifier is unterminated.
func (t *tokeniser) readQuotedIdentifier() (token, error) {
	identifier, err := t.readDelimitedLiteral('"', tokenIdentifier, "quoted identifier")
	identifier.Quoted = err == nil
	return identifier, err
}

// readNumber reads a numeric literal: digits with an optional fraction and exponent, a
// leading-dot literal such as .5, or a base-prefixed integer such as 0x1F.
//
// Returns token which is the number token.
// Returns error when scanning fails.
func (t *tokeniser) readNumber() (token, error) {
	startPosition := t.position
	end, err := lexicalRules.Numbers.ReadNumber(t.input, t.position)
	if err != nil {
		return token{}, err
	}
	t.position = end
	return t.emit(tokenNumber, startPosition, t.input[startPosition:t.position]), nil
}

// readIdentifier reads an unquoted identifier. After its first character an identifier
// may contain '$', as PostgreSQL allows, so amount$gbp is one identifier and never opens
// a dollar-quoted string.
//
// The cursor advances by UTF-8 rune width so multi-byte code points (Unicode letters or
// digits inside an identifier body) are not truncated mid-code-point as they would be by
// a byte-at-a-time scan.
//
// An unquoted identifier is folded to lower case, as PostgreSQL folds it, so the names
// the parser hands on are canonical; Raw keeps the spelling as written.
//
// Returns token which is the identifier token.
// Returns error which is always nil; declared for caller uniformity.
func (t *tokeniser) readIdentifier() (token, error) {
	startPosition := t.position
	for t.position < len(t.input) {
		character, width := utf8.DecodeRuneInString(t.input[t.position:])
		if character != '$' && !isIdentifierPart(character) {
			break
		}
		t.position += width
	}
	return t.emit(tokenIdentifier, startPosition, strings.ToLower(t.input[startPosition:t.position])), nil
}

// readDollarToken dispatches to dollar-parameter or dollar-quoted string.
//
// Returns token which is the produced token.
// Returns error when the dollar-quoted literal is malformed.
func (t *tokeniser) readDollarToken() (token, error) {
	startPosition := t.position

	if t.position+1 < len(t.input) && dialectkit.IsDigit(t.input[t.position+1]) {
		end, _, err := dialectkit.ReadParameterNumber(t.input, startPosition, t.position+1, maxDollarParameterNumber)
		if err != nil {
			return token{}, err
		}
		t.position = end
		return t.emit(tokenDollarParam, startPosition, t.input[startPosition:t.position]), nil
	}

	return t.readDollarQuotedString()
}

// readDollarQuotedString reads a $tag$ ... $tag$ dollar-quoted string.
//
// Returns token which is the dollar string token.
// Returns error when the literal is malformed or unterminated.
func (t *tokeniser) readDollarQuotedString() (token, error) {
	startPosition := t.position
	t.position++

	var tag string
	leadingTagRune, _ := utf8.DecodeRuneInString(t.input[t.position:])
	switch {
	case t.position < len(t.input) && t.input[t.position] == '$':
		t.position++
		tag = ""
	case t.position < len(t.input) && isIdentifierStart(leadingTagRune):
		tagStart := t.position
		for t.position < len(t.input) {
			character, width := utf8.DecodeRuneInString(t.input[t.position:])
			if !isIdentifierPart(character) {
				break
			}
			t.position += width
		}
		if t.position >= len(t.input) || t.input[t.position] != '$' {
			return token{}, dialectkit.ParseErrorAt(t.input, startPosition, startPosition, "invalid dollar-quoted string")
		}
		tag = t.input[tagStart:t.position]
		t.position++
	default:
		return token{}, dialectkit.ParseErrorAt(t.input, startPosition, startPosition, "unexpected character after $")
	}

	endDelimiter := "$" + tag + "$"
	var builder strings.Builder
	for t.position < len(t.input) {
		if t.input[t.position] == '$' && strings.HasPrefix(t.input[t.position:], endDelimiter) {
			t.position += len(endDelimiter)
			return t.emit(tokenDollarString, startPosition, builder.String()), nil
		}
		builder.WriteByte(t.input[t.position])
		t.position++
	}

	return token{}, dialectkit.ParseErrorAt(t.input, startPosition, startPosition, "unterminated dollar-quoted string")
}

// readColonToken reads :: cast, :name parameter, or a bare ':' operator. A colon written
// directly after an operand, as between the bounds of the array slice tags[lo:hi], is the
// operator, so :name is a parameter only where an operand is expected.
//
// Returns token which is the produced token.
// Returns error which is always nil; declared for caller uniformity.
func (t *tokeniser) readColonToken() (token, error) {
	startPosition := t.position

	if t.position+1 < len(t.input) && t.input[t.position+1] == ':' {
		t.position += 2
		return t.emit(tokenCast, startPosition, "::"), nil
	}

	parameterRune, _ := utf8.DecodeRuneInString(t.input[t.position+1:])
	if t.position+1 < len(t.input) && isIdentifierStart(parameterRune) && !t.followsOperand(startPosition) {
		t.position++
		for t.position < len(t.input) {
			character, width := utf8.DecodeRuneInString(t.input[t.position:])
			if !isIdentifierPart(character) {
				break
			}
			t.position += width
		}
		return t.emit(tokenNamedParam, startPosition, t.input[startPosition:t.position]), nil
	}

	t.position++
	return t.emit(tokenOperator, startPosition, ":"), nil
}

// followsOperand reports whether the byte before an offset ends an operand: a name, a
// number, a string or quoted name, or a closing bracket, written with no space between.
//
// Takes offset (int) which is the offset of the byte after the candidate operand.
//
// Returns bool which is true when an operand ends right before the offset.
func (t *tokeniser) followsOperand(offset int) bool {
	if offset == 0 {
		return false
	}
	previous, _ := utf8.DecodeLastRuneInString(t.input[:offset])
	return isIdentifierPart(previous) || strings.ContainsRune("'\"`)]}", previous)
}

// readOperator reads an arrow, multi-character, or single-character operator.
//
// Returns token which is the operator token.
// Returns error when no operator pattern matches.
func (t *tokeniser) readOperator() (token, error) {
	startPosition := t.position
	character := t.input[t.position]

	if produced, ok := t.readArrowOperator(character, startPosition); ok {
		return produced, nil
	}

	if produced, ok := t.readHashArrowOperator(character, startPosition); ok {
		return produced, nil
	}

	if produced, ok := t.readDistanceOperator(startPosition); ok {
		return produced, nil
	}

	if produced, ok := t.readTwoOrThreeCharOperator(startPosition); ok {
		return produced, nil
	}

	return t.readSingleCharOperator(character, startPosition)
}

// readArrowOperator reads -> or ->> JSON access operators when present.
//
// Takes character (byte) which is the candidate leading byte.
// Takes startPosition (int) which is the start offset of the token.
//
// Returns token which is the produced operator token when matched is true.
// Returns bool which is true when an arrow operator was consumed.
func (t *tokeniser) readArrowOperator(character byte, startPosition int) (token, bool) {
	return t.readArrowLikeOperator(character, '-', startPosition, tokenArrow, "->", tokenDoubleArrow, "->>")
}

// readHashArrowOperator reads #> or #>> JSON path operators when present.
//
// Takes character (byte) which is the candidate leading byte.
// Takes startPosition (int) which is the start offset of the token.
//
// Returns token which is the produced operator token when matched is true.
// Returns bool which is true when a hash-arrow operator was consumed.
func (t *tokeniser) readHashArrowOperator(character byte, startPosition int) (token, bool) {
	return t.readArrowLikeOperator(character, '#', startPosition, tokenHashArrow, "#>", tokenHashDoubleArrow, "#>>")
}

// readArrowLikeOperator reads a prefix> or prefix>> operator pair.
//
// Takes character (byte) which is the candidate leading byte.
// Takes prefix (byte) which is the expected leading byte.
// Takes startPosition (int) which is the start offset of the token.
// Takes singleKind (tokenKind) which classifies the prefix> variant.
// Takes singleValue (string) which is the literal text of the prefix> variant.
// Takes doubleKind (tokenKind) which classifies the prefix>> variant.
// Takes doubleValue (string) which is the literal text of the prefix>> variant.
//
// Returns token which is the produced operator token when matched is true.
// Returns bool which is true when an operator was consumed.
func (t *tokeniser) readArrowLikeOperator(
	character byte,
	prefix byte,
	startPosition int,
	singleKind tokenKind,
	singleValue string,
	doubleKind tokenKind,
	doubleValue string,
) (token, bool) {
	if character != prefix || t.position+1 >= len(t.input) || t.input[t.position+1] != '>' {
		return token{}, false
	}
	if t.position+2 < len(t.input) && t.input[t.position+2] == '>' {
		t.position += len(doubleValue)
		return t.emit(doubleKind, startPosition, doubleValue), true
	}
	t.position += len(singleValue)
	return t.emit(singleKind, startPosition, singleValue), true
}

// readDistanceOperator reads a distance operator, such as <-> or <=>, as one token.
//
// Takes startPosition (int) which is the start offset of the token.
//
// Returns token which is the produced operator token when matched is true.
// Returns bool which is true when a distance operator was consumed.
func (t *tokeniser) readDistanceOperator(startPosition int) (token, bool) {
	rest := t.input[t.position:]
	for _, operator := range distanceOperatorSpellings {
		if strings.HasPrefix(rest, operator) {
			t.position += len(operator)
			return t.emit(tokenOperator, startPosition, operator), true
		}
	}
	return token{}, false
}

// readTwoOrThreeCharOperator reads a two- or three-character operator.
//
// Takes startPosition (int) which is the start offset of the token.
//
// Returns token which is the produced operator token when matched is true.
// Returns bool which is true when an operator was consumed.
func (t *tokeniser) readTwoOrThreeCharOperator(startPosition int) (token, bool) {
	if t.position+1 >= len(t.input) {
		return token{}, false
	}

	twoChar := t.input[t.position : t.position+2]

	if strings.HasPrefix(t.input[t.position:], caseInsensitiveNotMatchOperator) {
		t.position += len(caseInsensitiveNotMatchOperator)
		return t.emit(tokenOperator, startPosition, caseInsensitiveNotMatchOperator), true
	}

	if twoCharacterOperators[twoChar] {
		t.position += 2
		return t.emit(tokenOperator, startPosition, twoChar), true
	}

	return token{}, false
}

// readSingleCharOperator reads a single-character SQL operator.
//
// Takes character (byte) which is the candidate operator byte.
// Takes startPosition (int) which is the start offset of the token.
//
// Returns token which is the operator token.
// Returns error when character is not a recognised operator.
func (t *tokeniser) readSingleCharOperator(character byte, startPosition int) (token, error) {
	singleCharacterOperators := "=<>+-/%~&|!#^?@"
	if strings.ContainsRune(singleCharacterOperators, rune(character)) {
		t.position++
		return t.emit(tokenOperator, startPosition, string(character)), nil
	}

	return token{}, dialectkit.ParseErrorAt(t.input, startPosition, startPosition, "unexpected character")
}

// isIdentifierStart reports whether the rune may begin an identifier.
//
// ASCII letters and the underscore take a fast path; runes beyond the ASCII range are
// accepted when unicode.IsLetter reports them as letters, so identifiers named with
// accented or non-Latin letters begin correctly.
//
// Takes character (rune) which is the rune to classify.
//
// Returns bool which is true for letters, underscore, or non-ASCII letters.
func isIdentifierStart(character rune) bool {
	return dialectkit.IsIdentStart(character)
}

// isIdentifierPart reports whether the rune may appear within an identifier after the
// first character, namely an identifier-start rune or a digit.
//
// ASCII runes take a fast path; runes beyond the ASCII range are accepted when
// unicode.IsLetter or unicode.IsDigit reports them, so Unicode letters and digits inside
// an identifier body are preserved.
//
// Takes character (rune) which is the rune to classify.
//
// Returns bool which is true for identifier-start runes or decimal digits.
func isIdentifierPart(character rune) bool {
	return dialectkit.IsIdentPart(character)
}

// readStatementTokens starts reading the tokens of input[start:end], as a
// dialectkit.StatementTokeniser does. Token positions are byte offsets into input, so a
// statement cut from a larger script keeps its place in it.
//
// Takes input (string) which is the SQL text holding the span.
// Takes start (int) which is the byte offset where scanning begins.
// Takes end (int) which is the byte offset where scanning stops.
//
// Returns func() (token, error) which reads the next token, giving tokenEOF at the end of
// the span, or an error when a lexical error occurs.
func readStatementTokens(input string, start, end int) func() (token, error) {
	lexer := &tokeniser{input: input[:end], position: start}
	return lexer.next
}

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

package sqlite

import (
	"strings"
	"unicode/utf8"

	"seagull.sh/seagull/internal/dialect/dialectkit"
)

const (
	// tokenIdentifier marks bare or quoted identifiers and unreserved words.
	tokenIdentifier tokenKind = iota

	// tokenNumber marks integer, decimal, hex, or exponent numeric literals.
	tokenNumber

	// tokenString marks a single-quoted string literal.
	tokenString

	// tokenBlobLiteral marks an x'...' hexadecimal blob literal.
	tokenBlobLiteral

	// tokenOperator marks an arithmetic, comparison, or bitwise operator.
	tokenOperator

	// tokenLeftParen marks the '(' punctuation.
	tokenLeftParen

	// tokenRightParen marks the ')' punctuation.
	tokenRightParen

	// tokenComma marks the ',' punctuation.
	tokenComma

	// tokenSemicolon marks the ';' statement terminator.
	tokenSemicolon

	// tokenDot marks the '.' qualifier between schema, table, and column.
	tokenDot

	// tokenStar marks the '*' wildcard or multiplication operator.
	tokenStar

	// tokenQuestionMark marks an anonymous '?' positional parameter.
	tokenQuestionMark

	// tokenNumberedParameter marks a numbered '?N' positional parameter.
	tokenNumberedParameter

	// tokenNamedParameter marks a named ':', '@', or '$' parameter.
	tokenNamedParameter

	// tokenArrow marks the '->' JSON path operator.
	tokenArrow

	// tokenDoubleArrow marks the '->>' JSON value operator.
	tokenDoubleArrow

	// tokenEOF marks the end of the token stream.
	tokenEOF
)

var (
	// lexicalRules declares SQLite lexical rules for the shared scanners: plain "--" line
	// comments, non-nesting block comments, and 0x hexadecimal integer literals (SQLite has
	// no 0o octal or 0b binary forms).
	lexicalRules = dialectkit.LexicalRules{
		Comments: dialectkit.CommentRules{},
		Numbers: dialectkit.NumberRules{
			HexPrefix:                true,
			OctalPrefix:              false,
			BinaryPrefix:             false,
			RequireDigitsAfterPrefix: false,
		},
	}

	// twoCharacterOperators lists every two-character SQL operator recognised by the lexer.
	// It is package-level so readOperator does not allocate a fresh map on each operator
	// token.
	twoCharacterOperators = map[string]bool{
		"<=": true, ">=": true, "<>": true, "!=": true, "==": true, "||": true,
		"<<": true, ">>": true,
	}
)

// tokenKind classifies a lexed SQL token.
type tokenKind uint8

var (
	// singleCharacterTokens maps ASCII byte values to their tokenKind plus one, so that a
	// zero entry signals "not a single-character token".
	singleCharacterTokens = [256]tokenKind{}
)

// token is a single lexed SQL token: its kind, decoded value, source text and byte span,
// and whether it is a delimited identifier, which is never a keyword.
type token = dialectkit.Token[tokenKind]

// tokeniser tracks scan progress through a SQL input string.
type tokeniser struct {
	// input is the SQL source being scanned.
	input string

	// position is the current byte offset within input.
	position int
}

// next returns the next token from the input stream.
//
// Returns token which is the lexed token, or tokenEOF when input is exhausted.
// Returns error when an unterminated literal or unexpected character is encountered.
func (t *tokeniser) next() (token, error) {
	if err := t.skipWhitespaceAndComments(); err != nil {
		return token{}, err
	}

	if t.position >= len(t.input) {
		return t.emit(tokenEOF, "", t.position), nil
	}

	if dialectkit.StartsNumber(t.input, t.position) {
		return t.readNumber()
	}

	character := t.input[t.position]

	if current, ok := t.readSingleCharacterToken(character); ok {
		return current, nil
	}

	return t.readMultiCharacterToken(character)
}

// emit builds the token that spans input[start:t.position], so its source text and end
// offset always match what the tokeniser consumed.
//
// Takes kind (tokenKind) which classifies the token.
// Takes value (string) which is the token's decoded text.
// Takes start (int) which is the byte offset of the token's first byte.
//
// Returns token which is the built token.
func (t *tokeniser) emit(kind tokenKind, value string, start int) token {
	return token{Kind: kind, Value: value, Raw: t.input[start:t.position], Start: start, End: t.position, Quoted: false}
}

// emitQuoted builds a delimited identifier token, such as "order", `order` or [order],
// which names an object and is never matched as a keyword.
//
// Takes value (string) which is the identifier without its delimiters.
// Takes start (int) which is the byte offset of the opening delimiter.
//
// Returns token which is the built identifier token.
func (t *tokeniser) emitQuoted(value string, start int) token {
	quoted := t.emit(tokenIdentifier, value, start)
	quoted.Quoted = true
	return quoted
}

// readSingleCharacterToken consumes one byte if it maps to a single-character punctuation
// token.
//
// Takes character (byte) which is the current byte at the scan position.
//
// Returns token which is the produced token when matched, else the zero value.
// Returns bool which is true when a single-character token was consumed.
func (t *tokeniser) readSingleCharacterToken(character byte) (token, bool) {
	mapped := singleCharacterTokens[character]
	if mapped == 0 {
		return token{}, false
	}
	startPosition := t.position
	t.position++
	return t.emit(mapped-1, string(character), startPosition), true
}

// readMultiCharacterToken dispatches to the appropriate scanner for tokens longer than
// one byte.
//
// Takes character (byte) which is the current byte at the scan position.
//
// Returns token which is the lexed multi-character token.
// Returns error when the underlying scanner reports a malformed literal.
func (t *tokeniser) readMultiCharacterToken(character byte) (token, error) {
	switch {
	case character == '?':
		return t.readQuestionParameter()
	case character == ':' || character == '@' || character == '$':
		return t.readNamedParameter()
	case character == '\'':
		return t.readString()
	case character == '"':
		return t.readQuotedIdentifier('"')
	case character == '`':
		return t.readQuotedIdentifier('`')
	case character == '[':
		return t.readBracketIdentifier()
	case (character == 'x' || character == 'X') && t.position+1 < len(t.input) && t.input[t.position+1] == '\'':
		return t.readBlobLiteral()
	}

	leading, _ := utf8.DecodeRuneInString(t.input[t.position:])
	if isIdentifierStart(leading) {
		return t.readIdentifier()
	}
	return t.readOperator()
}

// skipWhitespaceAndComments advances past spaces, tabs, newlines, line comments, and
// block comments.
//
// Returns error when a block comment is left unterminated.
func (t *tokeniser) skipWhitespaceAndComments() error {
	position, err := lexicalRules.Comments.SkipWhitespaceAndComments(t.input, t.position)
	t.position = position
	return err
}

// readString lexes a single-quoted SQL string literal with doubled-quote escaping.
//
// Returns token which is the lexed string token without surrounding quotes.
// Returns error when the literal is unterminated.
func (t *tokeniser) readString() (token, error) {
	startPosition := t.position

	value, position, ok := dialectkit.ScanDoubledDelimiter(t.input, t.position, '\'')
	if !ok {
		return token{}, dialectkit.ParseErrorAt(t.input, startPosition, startPosition, "unterminated string literal")
	}
	t.position = position

	return t.emit(tokenString, value, startPosition), nil
}

// readQuotedIdentifier lexes a double-quote or backtick identifier with doubled-quote
// escaping.
//
// Takes quote (byte) which is the opening and closing quote character.
//
// Returns token which is the lexed identifier without surrounding quotes.
// Returns error when the identifier is unterminated.
func (t *tokeniser) readQuotedIdentifier(quote byte) (token, error) {
	startPosition := t.position

	value, position, ok := dialectkit.ScanDoubledDelimiter(t.input, t.position, quote)
	if !ok {
		return token{}, dialectkit.ParseErrorAt(t.input, startPosition, startPosition, "unterminated quoted identifier")
	}
	t.position = position

	return t.emitQuoted(value, startPosition), nil
}

// readBracketIdentifier lexes a `[name]` bracketed identifier.
//
// Returns token which is the lexed identifier without surrounding brackets.
// Returns error when the identifier is unterminated.
func (t *tokeniser) readBracketIdentifier() (token, error) {
	startPosition := t.position
	t.position++

	var builder strings.Builder
	for t.position < len(t.input) {
		character := t.input[t.position]
		if character == ']' {
			t.position++
			return t.emitQuoted(builder.String(), startPosition), nil
		}
		builder.WriteByte(character)
		t.position++
	}

	return token{}, dialectkit.ParseErrorAt(t.input, startPosition, startPosition, "unterminated bracket identifier")
}

// readNumber lexes an integer, hexadecimal, decimal, or exponent numeric literal,
// including one that starts with its decimal point, such as .5.
//
// Returns token which is the lexed numeric token.
// Returns error when a base-prefixed literal is malformed.
func (t *tokeniser) readNumber() (token, error) {
	startPosition := t.position

	end, err := lexicalRules.Numbers.ReadNumber(t.input, t.position)
	if err != nil {
		return token{}, err
	}
	t.position = end

	return t.emit(tokenNumber, t.input[startPosition:t.position], startPosition), nil
}

// readIdentifier lexes an unquoted identifier, which SQLite continues through '$' after
// its first character, as in price$usd.
//
// Advances by UTF-8 rune width so multi-byte code points (Unicode letters or digits
// inside an identifier body, for example accented or non-Latin letters) are not truncated
// mid-code-point as they would be by a byte-at-a-time scan.
//
// Returns token which is the lexed identifier token.
// Returns error which is currently always nil.
func (t *tokeniser) readIdentifier() (token, error) {
	startPosition := t.position
	t.position = t.nameEnd(startPosition)
	return t.emit(tokenIdentifier, t.input[startPosition:t.position], startPosition), nil
}

// nameEnd returns the offset just past the name starting at start: its identifier
// characters, and the '$' SQLite reads as part of a name after its first character.
//
// Takes start (int) which is the offset of the name's first character.
//
// Returns int which is the offset of the first character that cannot continue the name,
// or start when none can begin it.
func (t *tokeniser) nameEnd(start int) int {
	position := start
	for position < len(t.input) {
		character, width := utf8.DecodeRuneInString(t.input[position:])
		if !isIdentifierPart(character) && (character != '$' || position == start) {
			break
		}
		position += width
	}
	return position
}

// readQuestionParameter lexes an anonymous `?` or numbered `?N` parameter.
//
// Returns token which is the lexed parameter token.
// Returns error which is currently always nil.
func (t *tokeniser) readQuestionParameter() (token, error) {
	startPosition := t.position
	t.position++

	if t.position < len(t.input) && isDigit(t.input[t.position]) {
		for t.position < len(t.input) && isDigit(t.input[t.position]) {
			t.position++
		}
		return t.emit(tokenNumberedParameter, t.input[startPosition:t.position], startPosition), nil
	}

	return t.emit(tokenQuestionMark, "?", startPosition), nil
}

// readNamedParameter lexes a named `:`, `@`, or `$` parameter including its identifier
// body, which continues through '$' after its first character as an identifier does.
//
// SQLite reads the sigil only as the start of a name, so a sigil with no name after it,
// such as the first colon of a PostgreSQL-style `::` cast, is an unexpected character.
//
// Returns token which is the lexed parameter token.
// Returns error when no name follows the sigil.
func (t *tokeniser) readNamedParameter() (token, error) {
	startPosition := t.position
	t.position = t.nameEnd(startPosition + 1)
	if t.position == startPosition+1 {
		return token{}, dialectkit.ParseErrorAt(t.input, startPosition, t.position, "unexpected character")
	}
	return t.emit(tokenNamedParameter, t.input[startPosition:t.position], startPosition), nil
}

// readBlobLiteral lexes an `x'...'` hexadecimal blob literal.
//
// Returns token which is the lexed blob token without surrounding quotes.
// Returns error when the literal is unterminated.
func (t *tokeniser) readBlobLiteral() (token, error) {
	startPosition := t.position
	t.position += 2

	var builder strings.Builder
	for t.position < len(t.input) {
		character := t.input[t.position]
		if character == '\'' {
			t.position++
			return t.emit(tokenBlobLiteral, builder.String(), startPosition), nil
		}
		builder.WriteByte(character)
		t.position++
	}

	return token{}, dialectkit.ParseErrorAt(t.input, startPosition, startPosition, "unterminated blob literal")
}

// readOperator lexes a one- or two-character operator, or the three-character ->> arrow.
//
// Returns token which is the lexed operator token.
// Returns error when the current byte is not a recognised operator.
func (t *tokeniser) readOperator() (token, error) {
	startPosition := t.position
	character := t.input[t.position]

	if character == '-' && t.position+1 < len(t.input) && t.input[t.position+1] == '>' {
		if t.position+2 < len(t.input) && t.input[t.position+2] == '>' {
			t.position += doubleArrowOperatorLength
			return t.emit(tokenDoubleArrow, "->>", startPosition), nil
		}
		t.position += 2
		return t.emit(tokenArrow, "->", startPosition), nil
	}

	if t.position+1 < len(t.input) {
		twoCharacters := t.input[t.position : t.position+2]
		if twoCharacterOperators[twoCharacters] {
			t.position += 2
			return t.emit(tokenOperator, twoCharacters, startPosition), nil
		}
	}

	singleCharacterOperators := "=<>+-/%~&|!"
	if strings.ContainsRune(singleCharacterOperators, rune(character)) {
		t.position++
		return t.emit(tokenOperator, string(character), startPosition), nil
	}

	return token{}, dialectkit.ParseErrorAt(t.input, startPosition, startPosition, "unexpected character")
}

func init() {
	singleCharacterTokens['('] = tokenLeftParen + 1
	singleCharacterTokens[')'] = tokenRightParen + 1
	singleCharacterTokens[','] = tokenComma + 1
	singleCharacterTokens[';'] = tokenSemicolon + 1
	singleCharacterTokens['.'] = tokenDot + 1
	singleCharacterTokens['*'] = tokenStar + 1
}

// isDigit reports whether character is an ASCII decimal digit.
//
// Takes character (byte) which is the byte to test.
//
// Returns bool which is true when character is '0' through '9'.
func isDigit(character byte) bool {
	return character >= '0' && character <= '9'
}

// isIdentifierStart reports whether the rune may start an unquoted identifier.
//
// Runes below utf8.RuneSelf take an ASCII fast-path covering letters and the underscore;
// any other rune is accepted when unicode.IsLetter reports it as a letter, so non-ASCII
// identifiers using accented or non-Latin letters tokenise correctly.
//
// Takes character (rune) which is the decoded code point to test.
//
// Returns bool which is true for ASCII letters, the underscore, or any Unicode letter.
func isIdentifierStart(character rune) bool {
	return dialectkit.IsIdentStart(character)
}

// isIdentifierPart reports whether the rune may continue an unquoted identifier after the
// first character.
//
// This extends the identifier-start set with decimal digits, so for runes below
// utf8.RuneSelf the ASCII digit check is inlined, while any other rune is accepted when
// unicode.IsLetter or unicode.IsDigit reports it.
//
// Takes character (rune) which is the decoded code point to test.
//
// Returns bool which is true for identifier starts and decimal digits.
func isIdentifierPart(character rune) bool {
	return dialectkit.IsIdentPart(character)
}

// statementTokens starts reading the tokens of input[start:end], one at a time, as
// dialectkit.LexStatements reads a statement. Token positions are byte offsets into
// input, so a statement cut from a larger script keeps its place in it.
//
// Takes input (string) which is the SQL text holding the statement.
// Takes start (int) which is the byte offset where scanning begins.
// Takes end (int) which is the byte offset where scanning stops.
//
// Returns func() (token, error) which reads the next token, giving tokenEOF at end.
func statementTokens(input string, start, end int) func() (token, error) {
	lexer := &tokeniser{input: input[:end], position: start}
	return lexer.next
}

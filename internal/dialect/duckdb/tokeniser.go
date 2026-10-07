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

package duckdb

import (
	"strings"
	"unicode/utf8"

	"seagull.sh/seagull/internal/dialect/dialectkit"
)

const (
	// operatorNotILike is the four-character !~~* operator, a case-insensitive NOT LIKE.
	operatorNotILike = "!~~*"

	// threeCharOperatorLength is the length of the operators in threeCharOperators.
	threeCharOperatorLength = 3

	// digitSeparator is the byte DuckDB allows between two digits of a numeric literal, as
	// in 1_000_000.
	digitSeparator = '_'
)

const (
	// tokenIdentifier denotes an unquoted or quoted SQL identifier.
	tokenIdentifier tokenKind = iota

	// tokenNumber denotes a numeric literal.
	tokenNumber

	// tokenString denotes a single-quoted string literal.
	tokenString

	// tokenDollarString denotes a dollar-quoted string literal such as $$body$$ or
	// $tag$body$tag$.
	tokenDollarString

	// tokenBitString denotes a B'...' bit-string literal.
	tokenBitString

	// tokenOperator denotes an operator token such as "=" or "<=".
	tokenOperator

	// tokenLeftParen denotes the "(" punctuation token.
	tokenLeftParen

	// tokenRightParen denotes the ")" punctuation token.
	tokenRightParen

	// tokenLeftBracket denotes the "[" punctuation token.
	tokenLeftBracket

	// tokenRightBracket denotes the "]" punctuation token.
	tokenRightBracket

	// tokenComma denotes the "," punctuation token.
	tokenComma

	// tokenSemicolon denotes the ";" statement terminator token.
	tokenSemicolon

	// tokenDot denotes the "." punctuation token.
	tokenDot

	// tokenStar denotes the "*" punctuation or wildcard token.
	tokenStar

	// tokenDollarParam denotes a $N positional parameter reference.
	tokenDollarParam

	// tokenNamedParam denotes a :name or $name named parameter reference.
	tokenNamedParam

	// tokenQuestionParam denotes a ? parameter, which takes the number after the largest one
	// before it, or a ?N parameter, which gives its number.
	tokenQuestionParam

	// tokenCast denotes the "::" cast operator.
	tokenCast

	// tokenArrow denotes the "->" JSON arrow operator.
	tokenArrow

	// tokenDoubleArrow denotes the "->>" JSON double-arrow operator.
	tokenDoubleArrow

	// tokenLeftBrace denotes the "{" that opens a struct or map literal.
	tokenLeftBrace

	// tokenRightBrace denotes the "}" that closes a struct or map literal.
	tokenRightBrace

	// tokenEscapeString denotes an E'...' string literal, whose backslash escapes are
	// resolved.
	tokenEscapeString

	// tokenEOF denotes the end-of-input sentinel token.
	tokenEOF
)

var (
	// lexicalRules declares DuckDB lexical rules for the shared scanners: nested block
	// comments, and decimal numeric literals only. DuckDB has no base-prefixed integer
	// literal, so 0x1F reads as the integer 0 followed by the alias x1F, as DuckDB itself
	// reads it.
	lexicalRules = dialectkit.LexicalRules{
		Comments: dialectkit.CommentRules{
			NestedBlockComments:          true,
			DoubleDashRequiresWhitespace: false,
			HashLineComment:              false,
		},
		Numbers: dialectkit.NumberRules{
			HexPrefix:                false,
			OctalPrefix:              false,
			BinaryPrefix:             false,
			RequireDigitsAfterPrefix: false,
		},
	}
)

// tokenKind classifies a single lexical token in a SQL statement.
type tokenKind uint8

// token is a single lexical token produced by the tokeniser: its DuckDB kind, its decoded
// value, its raw source text, its byte span and whether it is a delimited identifier.
type token = dialectkit.Token[tokenKind]

// tokeniser walks the SQL input string and produces a sequence of tokens.
type tokeniser struct {
	// input is the SQL source text being scanned.
	input string

	// position is the current zero-based byte offset within input.
	position int
}

// next reads and returns the next token from the input stream.
//
// Returns token which is the next lexed token, or tokenEOF at end of input.
// Returns error when an unterminated literal or unexpected character is found.
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

	if current, ok := t.readDoubledStarOperator(character); ok {
		return current, nil
	}

	if current, ok := t.readSingleCharToken(character); ok {
		return current, nil
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
	return token{Kind: kind, Value: value, Raw: t.input[start:t.position], Start: start, End: t.position, Quoted: false}
}

// readDoubledStarOperator reads the "**" exponent operator, which would otherwise read as
// two "*" tokens.
//
// Takes character (byte) which is the current byte at the cursor.
//
// Returns token which is the lexed operator when matched.
// Returns bool which is true when "**" was consumed.
func (t *tokeniser) readDoubledStarOperator(character byte) (token, bool) {
	if character != '*' || t.position+1 >= len(t.input) || t.input[t.position+1] != '*' {
		return token{}, false
	}
	startPosition := t.position
	t.position += 2
	return t.emit(tokenOperator, startPosition, "**"), true
}

// readSingleCharToken attempts to consume a one-byte punctuation token.
//
// Takes character (byte) which is the current byte at the cursor.
//
// Returns token which is the produced punctuation token when matched.
// Returns bool which is true when a punctuation token was produced.
func (t *tokeniser) readSingleCharToken(character byte) (token, bool) {
	mapped := singleCharTokens[character]
	if !mapped.ok {
		return token{}, false
	}
	startPosition := t.position
	t.position++
	return t.emit(mapped.kind, startPosition, string(character)), true
}

// readMultiCharToken dispatches to the appropriate reader for non-trivial token starts
// such as identifiers, numbers, and string literals.
//
// Takes character (byte) which is the current byte at the cursor.
//
// Returns token which is the lexed token produced by the dispatched reader.
// Returns error when the dispatched reader fails to recognise the input.
func (t *tokeniser) readMultiCharToken(character byte) (token, error) {
	switch {
	case character == '$':
		return t.readDollarToken()
	case character == '?':
		return t.readQuestionParameter()
	case character == ':':
		return t.readColonToken()
	case character == '\'':
		return t.readString()
	case character == '"':
		return t.readQuotedIdentifier()
	case (character == 'B' || character == 'b') && t.position+1 < len(t.input) && t.input[t.position+1] == '\'':
		return t.readBitString()
	case (character == 'E' || character == 'e') && t.position+1 < len(t.input) && t.input[t.position+1] == '\'':
		return t.readEscapeString()
	}

	if isIdentStart(t.runeAt(t.position)) {
		return t.readIdentifier()
	}
	return t.readOperator()
}

// skipWhitespaceAndComments advances the cursor past whitespace, line comments, and block
// comments in the input.
//
// Returns error when a block comment is opened but never closed before end of input.
func (t *tokeniser) skipWhitespaceAndComments() error {
	position, err := lexicalRules.Comments.SkipWhitespaceAndComments(t.input, t.position)
	t.position = position
	return err
}

// readString reads a single-quoted string literal.
//
// Returns token which is the lexed tokenString.
// Returns error when the literal is not terminated before end of input.
func (t *tokeniser) readString() (token, error) {
	return t.readDelimitedLiteral('\'', tokenString, "string literal")
}

// readDelimitedLiteral reads a literal bounded by a single-byte delimiter, treating a
// doubled delimiter as an embedded literal occurrence.
//
// Takes delimiter (byte) which is the quoting byte that opens and closes the literal.
// Takes kind (tokenKind) which is the token kind to assign to the result.
// Takes errorDescription (string) which names the literal in error messages.
//
// Returns token which is the lexed token of the requested kind.
// Returns error when the literal is not terminated before end of input.
func (t *tokeniser) readDelimitedLiteral(delimiter byte, kind tokenKind, errorDescription string) (token, error) {
	startPosition := t.position

	value, position, ok := dialectkit.ScanDoubledDelimiter(t.input, t.position, delimiter)
	if !ok {
		return token{}, dialectkit.ParseErrorAt(t.input, startPosition, startPosition, "unterminated "+errorDescription)
	}
	t.position = position

	return t.emit(kind, startPosition, value), nil
}

// readBitString reads a B'...' bit-string literal.
//
// Returns token which is the lexed tokenBitString.
// Returns error when the literal is not terminated before end of input.
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

// readEscapeString reads an E'...' string literal, resolving each backslash escape to the
// character it escapes and a doubled quote to one quote.
//
// Returns token which is the lexed tokenEscapeString.
// Returns error when the literal is not terminated before end of input.
func (t *tokeniser) readEscapeString() (token, error) {
	startPosition := t.position
	t.position += 2

	var builder strings.Builder
	for t.position < len(t.input) {
		character := t.input[t.position]
		if character == '\\' && t.position+1 < len(t.input) {
			t.position++
			builder.WriteByte(t.input[t.position])
			t.position++
			continue
		}
		if character == '\'' {
			t.position++
			if t.position < len(t.input) && t.input[t.position] == '\'' {
				builder.WriteByte('\'')
				t.position++
				continue
			}
			return t.emit(tokenEscapeString, startPosition, builder.String()), nil
		}
		builder.WriteByte(character)
		t.position++
	}

	return token{}, dialectkit.ParseErrorAt(t.input, startPosition, startPosition, "unterminated escape string")
}

// readQuotedIdentifier reads a double-quoted SQL identifier, which is always a name and
// never a keyword.
//
// Returns token which is the lexed tokenIdentifier, marked Quoted, with its content.
// Returns error when the identifier is not terminated before end of input.
func (t *tokeniser) readQuotedIdentifier() (token, error) {
	identifier, err := t.readDelimitedLiteral('"', tokenIdentifier, "quoted identifier")
	identifier.Quoted = err == nil
	return identifier, err
}

// readNumber reads a numeric literal: digits with an optional fraction and exponent, or a
// leading-dot literal such as .5. As in DuckDB, a single underscore between two digits
// separates them, as in 1_000_000; the token's value leaves the separators out, while its
// raw text keeps them.
//
// Returns token which is the lexed tokenNumber.
// Returns error when the literal cannot be read under the dialect's number rules.
func (t *tokeniser) readNumber() (token, error) {
	startPosition := t.position
	end, err := lexicalRules.Numbers.ReadSeparatedNumber(t.input, t.position, digitSeparator)
	if err != nil {
		return token{}, err
	}
	t.position = end
	return t.emit(tokenNumber, startPosition, strings.ReplaceAll(t.input[startPosition:t.position], string(digitSeparator), "")), nil
}

// readIdentifier reads an unquoted SQL identifier from the current cursor. The scan
// advances by UTF-8 rune width so that multi-byte codepoints (Unicode letters or digits
// inside an identifier body) are not truncated mid-codepoint as they would be by a
// byte-at-a-time scan.
//
// Returns token which is the lexed tokenIdentifier.
// Returns error which is always nil for the unquoted identifier path.
func (t *tokeniser) readIdentifier() (token, error) {
	startPosition := t.position
	t.advanceOverIdentifierBody()
	return t.emit(tokenIdentifier, startPosition, t.input[startPosition:t.position]), nil
}

// readQuestionParameter reads a ? parameter and the digits of its number, when it gives
// one, as in ?3.
//
// Returns token which is the lexed tokenQuestionParam.
// Returns error when the parameter number exceeds maxDollarParameterNumber.
func (t *tokeniser) readQuestionParameter() (token, error) {
	startPosition := t.position
	end, _, err := dialectkit.ReadParameterNumber(t.input, startPosition, t.position+1, maxDollarParameterNumber)
	if err != nil {
		return token{}, err
	}
	t.position = end
	return t.emit(tokenQuestionParam, startPosition, t.input[startPosition:t.position]), nil
}

// readDollarToken dispatches a leading "$" to a "$N" positional parameter reference, a
// dollar-quoted string literal ($$...$$ or $tag$...$tag$), or a "$name" named parameter
// reference.
//
// DuckDB supports all three: a digit after the "$" makes a numbered parameter, a "$"
// after the tag a dollar quote, and a name followed by anything else a named parameter.
//
// Returns token which is the lexed tokenDollarParam, tokenDollarString or
// tokenNamedParam.
// Returns error when the parameter number exceeds maxDollarParameterNumber or the
// dollar-quoted literal is malformed or unterminated.
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

	if !t.opensDollarQuote() && isIdentStart(t.runeAt(startPosition+1)) {
		t.position++
		t.advanceOverIdentifierBody()
		return t.emit(tokenNamedParam, startPosition, t.input[startPosition:t.position]), nil
	}

	return t.readDollarQuotedString()
}

// opensDollarQuote reports whether the "$" at the cursor opens a dollar-quoted string:
// "$$", or a tag followed by "$". The cursor is left where it was.
//
// Returns bool which is true for a dollar-quote opener.
func (t *tokeniser) opensDollarQuote() bool {
	saved := t.position
	t.position++
	_, opens := t.readDollarQuoteTag()
	t.position = saved
	return opens
}

// readDollarQuotedString reads a $tag$ ... $tag$ dollar-quoted string.
//
// The tag is empty for the $$...$$ form or a leading-letter identifier for the tagged
// form. The matching close delimiter must repeat the same tag.
//
// Returns token which is the lexed tokenDollarString.
// Returns error when the opening delimiter is malformed or the literal is never
// terminated.
func (t *tokeniser) readDollarQuotedString() (token, error) {
	startPosition := t.position
	t.position++

	tag, ok := t.readDollarQuoteTag()
	if !ok {
		return token{}, dialectkit.ParseErrorAt(t.input, startPosition, startPosition, "invalid dollar-quoted string")
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

// readDollarQuoteTag consumes the opening tag of a dollar-quoted string starting just
// past the leading "$".
//
// It accepts an empty tag (the immediate "$$" form) or an identifier-led tag terminated
// by a closing "$". The cursor is left immediately after the opening delimiter.
//
// Returns string which is the tag text, empty for the $$ form.
// Returns bool which is true when a well-formed opening delimiter was consumed, false
// when the bytes after "$" do not begin a dollar quote.
func (t *tokeniser) readDollarQuoteTag() (string, bool) {
	if t.position < len(t.input) && t.input[t.position] == '$' {
		t.position++
		return "", true
	}

	leadingTagRune := t.runeAt(t.position)
	if t.position >= len(t.input) || !isIdentStart(leadingTagRune) {
		return "", false
	}

	tagStart := t.position
	t.advanceOverIdentifierBody()
	if t.position >= len(t.input) || t.input[t.position] != '$' {
		return "", false
	}
	tag := t.input[tagStart:t.position]
	t.position++
	return tag, true
}

// readColonToken distinguishes casts, named parameters and colon operators.
//
// A colon following an operand is an operator, allowing lambda bodies, struct values and
// slice bounds to be read correctly.
//
// Returns token which is the lexed token of the matching kind.
// Returns error which is always nil for the colon dispatch path.
func (t *tokeniser) readColonToken() (token, error) {
	startPosition := t.position

	if t.position+1 < len(t.input) && t.input[t.position+1] == ':' {
		t.position += 2
		return t.emit(tokenCast, startPosition, "::"), nil
	}

	if t.position+1 < len(t.input) && t.input[t.position+1] == '=' {
		t.position += 2
		return t.emit(tokenOperator, startPosition, ":="), nil
	}

	if t.position+1 < len(t.input) && isIdentStart(t.runeAt(t.position+1)) && !t.followsOperand(startPosition) {
		t.position++
		t.advanceOverIdentifierBody()
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
	return isIdentPart(previous) || strings.ContainsRune(`'")]}`, previous)
}

// runeAt decodes the UTF-8 rune that begins at the given byte offset. When the offset is
// at or beyond the end of the input the decode yields utf8.RuneError, which no identifier
// classifier accepts, so callers do not need a separate bounds guard before classifying.
//
// Takes offset (int) which is the byte offset to decode from.
//
// Returns rune which is the decoded rune, or utf8.RuneError when the offset is out of
// range or the bytes are not valid UTF-8.
func (t *tokeniser) runeAt(offset int) rune {
	if offset >= len(t.input) {
		return utf8.RuneError
	}
	character, _ := utf8.DecodeRuneInString(t.input[offset:])
	return character
}

// advanceOverIdentifierBody moves the cursor forward over every rune that may appear in
// an identifier body, advancing by UTF-8 rune width so that multi-byte codepoints are not
// truncated mid-codepoint. The cursor stops on the first rune that cannot continue an
// identifier, or at the end of the input.
func (t *tokeniser) advanceOverIdentifierBody() {
	for t.position < len(t.input) {
		character, width := utf8.DecodeRuneInString(t.input[t.position:])
		if !isIdentPart(character) {
			break
		}
		t.position += width
	}
}

// readOperator reads an operator token, trying arrow forms and multi-byte operators
// before falling back to a single-byte operator.
//
// Returns token which is the lexed tokenOperator or arrow variant.
// Returns error when the byte does not begin any recognised operator.
func (t *tokeniser) readOperator() (token, error) {
	startPosition := t.position
	character := t.input[t.position]

	if current, ok := t.readArrowOperator(character, startPosition); ok {
		return current, nil
	}

	if current, ok := t.readTwoOrThreeCharOperator(startPosition); ok {
		return current, nil
	}

	return t.readSingleCharOperator(character, startPosition)
}

// readArrowOperator reads the "->" or "->>" JSON arrow operator.
//
// Takes character (byte) which is the current byte at the cursor.
// Takes startPosition (int) which is the start byte offset of the operator.
//
// Returns token which is the lexed tokenArrow or tokenDoubleArrow.
// Returns bool which is true when an arrow operator was consumed.
func (t *tokeniser) readArrowOperator(character byte, startPosition int) (token, bool) {
	if character != '-' || t.position+1 >= len(t.input) || t.input[t.position+1] != '>' {
		return token{}, false
	}
	if strings.HasPrefix(t.input[t.position:], operatorJSONText) {
		t.position += len(operatorJSONText)
		return t.emit(tokenDoubleArrow, startPosition, operatorJSONText), true
	}
	t.position += len(operatorJSONExtract)
	return t.emit(tokenArrow, startPosition, operatorJSONExtract), true
}

// readTwoOrThreeCharOperator reads a multi-byte operator such as "<=" or "!~*" when one
// matches at the cursor.
//
// Takes startPosition (int) which is the start byte offset of the operator.
//
// Returns token which is the lexed tokenOperator when matched.
// Returns bool which is true when a multi-byte operator was consumed.
func (t *tokeniser) readTwoOrThreeCharOperator(startPosition int) (token, bool) {
	if t.position+1 >= len(t.input) {
		return token{}, false
	}

	twoChar := t.input[t.position : t.position+2]

	if strings.HasPrefix(t.input[t.position:], operatorNotILike) {
		t.position += len(operatorNotILike)
		return t.emit(tokenOperator, startPosition, operatorNotILike), true
	}

	if t.position+threeCharOperatorLength <= len(t.input) {
		threeChar := t.input[t.position : t.position+threeCharOperatorLength]
		if _, ok := threeCharOperators[threeChar]; ok {
			t.position += len(threeChar)
			return t.emit(tokenOperator, startPosition, threeChar), true
		}
	}

	if _, ok := twoCharOperators[twoChar]; ok {
		t.position += 2
		return t.emit(tokenOperator, startPosition, twoChar), true
	}

	return token{}, false
}

// readSingleCharOperator reads a one-byte operator such as "=" or "+".
//
// Takes character (byte) which is the current byte at the cursor.
// Takes startPosition (int) which is the start byte offset of the operator.
//
// Returns token which is the lexed tokenOperator.
// Returns error when the byte is not a recognised single-byte operator.
func (t *tokeniser) readSingleCharOperator(character byte, startPosition int) (token, error) {
	singleCharOperators := "=<>+-/%~&|!^@#"
	if strings.ContainsRune(singleCharOperators, rune(character)) {
		t.position++
		return t.emit(tokenOperator, startPosition, string(character)), nil
	}

	return token{}, dialectkit.ParseErrorAt(t.input, startPosition, startPosition, "unexpected character")
}

// singleCharToken is the token a byte lexes as on its own.
type singleCharToken struct {
	// kind is the token's kind.
	kind tokenKind

	// ok is true when the byte lexes as a token of its own.
	ok bool
}

var (
	// singleCharTokens maps each byte that always lexes as a token of its own to that
	// token's kind. An entry whose ok is false is no mapping, so the zero tokenKind stays
	// usable as a real mapping.
	singleCharTokens = [256]singleCharToken{
		'(': {kind: tokenLeftParen, ok: true},
		')': {kind: tokenRightParen, ok: true},
		'[': {kind: tokenLeftBracket, ok: true},
		']': {kind: tokenRightBracket, ok: true},
		',': {kind: tokenComma, ok: true},
		';': {kind: tokenSemicolon, ok: true},
		'.': {kind: tokenDot, ok: true},
		'*': {kind: tokenStar, ok: true},
		'{': {kind: tokenLeftBrace, ok: true},
		'}': {kind: tokenRightBrace, ok: true},
	}

	// twoCharOperators holds every two-character SQL operator recognised by the lexer.
	twoCharOperators = map[string]struct{}{
		"<=": {}, ">=": {}, "<>": {}, "!=": {}, "||": {},
		"<<": {}, ">>": {}, "&&": {}, "@>": {}, "<@": {},
		"~*": {}, "!~": {}, "~~": {}, "==": {}, "=>": {},
		"//": {}, "^@": {},
	}

	// threeCharOperators holds the three-character SQL operators recognised by the
	// tokeniser. The four-character !~~* (case-insensitive NOT LIKE) is read on its own
	// before them.
	threeCharOperators = map[string]struct{}{
		"!~*": {}, "!~~": {}, "~~*": {}, "<->": {}, "<=>": {},
	}
)

// tokeniseSpan scans input[start:end] and returns its token stream. Token positions are
// byte offsets into input, so a statement cut from a larger script keeps its place in it.
//
// Takes input (string) which is the SQL text holding the span.
// Takes start (int) which is the byte offset where scanning begins.
// Takes end (int) which is the byte offset where scanning stops.
//
// Returns []token which is the ordered stream of tokens ending in tokenEOF.
// Returns error when a lexical error occurs.
func tokeniseSpan(input string, start, end int) ([]token, error) {
	lexer := &tokeniser{input: input[:end], position: start}
	var tokens []token

	for {
		current, err := lexer.next()
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, current)
		if current.Kind == tokenEOF {
			break
		}
	}

	return tokens, nil
}

// statementTokens returns a reader of the tokens of input[start:end], ending with
// tokenEOF. Token positions are byte offsets into input.
//
// Takes input (string) which is the SQL text holding the statement.
// Takes start (int) which is the byte offset where reading begins.
// Takes end (int) which is the byte offset where reading stops.
//
// Returns func() (token, error) which reads the next token on each call.
func statementTokens(input string, start, end int) func() (token, error) {
	lexer := &tokeniser{input: input[:end], position: start}
	return lexer.next
}

// isIdentStart reports whether the rune can begin an unquoted SQL identifier.
//
// It accepts an ASCII letter, an underscore, or a non-ASCII letter per unicode.IsLetter.
// ASCII runes take a fast path so the common case avoids the Unicode tables, and
// non-ASCII letters are accepted so that Unicode-named columns, for example accented or
// CJK identifiers, tokenise as a single token rather than being split mid-codepoint.
//
// Takes character (rune) which is the rune to classify.
//
// Returns bool which is true for an ASCII letter, underscore, or a non-ASCII letter.
func isIdentStart(character rune) bool {
	return dialectkit.IsIdentStart(character)
}

// isIdentPart reports whether the rune can appear in an unquoted SQL identifier after the
// first character: any identifier-start rune plus decimal digits. ASCII runes take a fast
// path; non-ASCII runes are accepted when they are Unicode letters or digits.
//
// Takes character (rune) which is the rune to classify.
//
// Returns bool which is true for an identifier-start rune or a digit.
func isIdentPart(character rune) bool {
	return dialectkit.IsIdentPart(character)
}

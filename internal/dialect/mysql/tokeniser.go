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

package mysql

import (
	"strings"
	"unicode/utf8"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqllex"
)

const (
	// blockCommentCloser closes a block comment, executable or not.
	blockCommentCloser = "*/"

	// substituteCharacter is the MySQL backslash-Z escape value (0x1A).
	substituteCharacter = 0x1A

	// backspaceCharacter is the MySQL backslash-b escape value (0x08).
	backspaceCharacter = 0x08

	// threeCharacterOperatorLength is the byte length of the three-character operators ->>
	// and <=>.
	threeCharacterOperatorLength = 3
)

const (
	// tokenIdentifier marks a bare or quoted identifier.
	tokenIdentifier tokenKind = iota

	// tokenNumber marks an integer, decimal, or scientific literal.
	tokenNumber

	// tokenString marks a quoted string literal: '...', or "..." where the SQL mode reads it
	// as a string.
	tokenString

	// tokenHexString marks an X'...' hexadecimal literal.
	tokenHexString

	// tokenBitString marks a B'...' bit-string literal.
	tokenBitString

	// tokenOperator marks an arithmetic, comparison, or logical operator.
	tokenOperator

	// tokenLeftParen marks a '(' delimiter.
	tokenLeftParen

	// tokenRightParen marks a ')' delimiter.
	tokenRightParen

	// tokenLeftBracket marks a '[' delimiter.
	tokenLeftBracket

	// tokenRightBracket marks a ']' delimiter.
	tokenRightBracket

	// tokenComma marks a ',' separator.
	tokenComma

	// tokenSemicolon marks a ';' statement terminator.
	tokenSemicolon

	// tokenDot marks a '.' qualifier between identifiers.
	tokenDot

	// tokenStar marks a '*' wildcard or multiplication operator.
	tokenStar

	// tokenQuestionMark marks a positional '?' parameter placeholder.
	tokenQuestionMark

	// tokenNamedParameter marks a ':name' named parameter placeholder.
	tokenNamedParameter

	// tokenArrow marks the '->' JSON-extract operator.
	tokenArrow

	// tokenDoubleArrow marks the '->>' JSON-extract-and-unquote operator.
	tokenDoubleArrow

	// tokenUserVariable marks an '@name' user variable reference.
	tokenUserVariable

	// tokenSystemVariable marks an '@@name' system variable reference.
	tokenSystemVariable

	// tokenEOF marks the end of the input stream.
	tokenEOF
)

var (
	// lexicalRules declares MySQL lexical rules for the shared scanners: "--" line comments
	// that require trailing whitespace, "#" line comments, non-nesting block comments, and
	// 0x/0b base-prefixed integer literals (MySQL has no 0o octal form).
	lexicalRules = dialectkit.LexicalRules{
		Comments: dialectkit.CommentRules{
			DoubleDashRequiresWhitespace: true,
			HashLineComment:              true,
			NestedBlockComments:          false,
		},
		Numbers: dialectkit.NumberRules{
			HexPrefix:                true,
			BinaryPrefix:             true,
			OctalPrefix:              false,
			RequireDigitsAfterPrefix: false,
		},
	}

	// twoCharacterOperators lists the two-character operators recognised by the tokeniser.
	twoCharacterOperators = map[string]bool{
		"<=": true, ">=": true, "<>": true, "!=": true, "||": true,
		"<<": true, ">>": true, "&&": true,
	}
)

// tokenKind classifies a token produced by the MySQL tokeniser.
type tokenKind uint8

var (
	// singleCharacterTokens maps a leading byte to its token kind plus one, where zero
	// indicates "not a single-character token". The +1 offset preserves the zero-value
	// meaning for unmapped bytes.
	singleCharacterTokens = [256]tokenKind{}
)

// token is a lexical unit produced by the MySQL tokeniser: the shared dialectkit token
// over MySQL's own kinds, so a delimited identifier carries Quoted and never matches a
// keyword.
type token = dialectkit.Token[tokenKind]

var (
	// noToken is the token returned alongside an error or a failed match.
	noToken token
)

// tokeniser walks the SQL source and emits tokens for the parser.
type tokeniser struct {
	// input holds the SQL source being scanned.
	input string

	// position is the current zero-based byte offset within input.
	position int

	// nameEnd is the offset one past the previous token when it was an identifier, and -1
	// otherwise, so a '.' written straight after a name qualifies it, as in t.5col, rather
	// than starting a leading-dot number such as .5.
	nameEnd int

	// insideExecutableComment reports whether the tokeniser is reading the contents of an
	// executable comment, so the next */ closes it rather than being read as operators.
	insideExecutableComment bool

	// doubleQuote says whether "..." is a string, as in the server's default SQL mode, or a
	// quoted identifier, as under ANSI_QUOTES.
	doubleQuote sqllex.DoubleQuoteMeaning
}

// read returns the next token, remembering where it ends when it is a name, so a '.'
// written straight after it qualifies it rather than starting a number.
//
// Returns token which is the next token, or an EOF token at the end of the statement.
// Returns error when a lexical error occurs.
func (t *tokeniser) read() (token, error) {
	current, err := t.next()
	if err != nil {
		return noToken, err
	}
	t.nameEnd = -1
	if current.Kind == tokenIdentifier {
		t.nameEnd = current.End
	}
	return current, nil
}

// next advances past whitespace and returns the next token.
//
// Returns token which holds the next lexical unit, or an EOF token at end of input.
// Returns error when a literal cannot be scanned.
func (t *tokeniser) next() (token, error) {
	if err := t.skipWhitespaceAndComments(); err != nil {
		return noToken, err
	}

	if t.position >= len(t.input) {
		return t.emit(tokenEOF, t.position, ""), nil
	}

	if dialectkit.StartsNumber(t.input, t.position) && (t.position != t.nameEnd || t.input[t.position] != '.') {
		return t.readNumber()
	}

	character := t.input[t.position]

	if character == '?' {
		return t.readQuestionMarkToken(), nil
	}

	if current, ok := t.readSingleCharacterToken(character); ok {
		return current, nil
	}

	return t.readMultiCharacterToken(character)
}

// readQuestionMarkToken reads a '?' placeholder, consuming any trailing digits so the
// resulting token covers the full '?N' form when present.
//
// Engines using the wire protocol's anonymous '?' still parse correctly because the lone
// '?' branch leaves value="?", while directive-annotated queries written as '?1', '?2'
// produce a single token with value "?N" that the parser deduplicates by number like a
// named parameter.
//
// Returns token which holds the question-mark token with any captured numeric suffix.
func (t *tokeniser) readQuestionMarkToken() token {
	startPosition := t.position
	t.position++
	for t.position < len(t.input) && t.input[t.position] >= '0' && t.input[t.position] <= '9' {
		t.position++
	}
	return t.emit(tokenQuestionMark, startPosition, t.input[startPosition:t.position])
}

// emit returns a token of kind that began at start and ends at the scanner's position,
// carrying its source text as Raw.
//
// Takes kind (tokenKind) which classifies the token.
// Takes start (int) which is the byte offset of the token's first byte.
// Takes value (string) which is the token's decoded text.
//
// Returns token which is the token.
func (t *tokeniser) emit(kind tokenKind, start int, value string) token {
	return token{Kind: kind, Value: value, Raw: t.input[start:t.position], Start: start, End: t.position, Quoted: false}
}

// readSingleCharacterToken emits a token for a known single-character byte.
//
// Takes character (byte) which is the byte at the current scanner position.
//
// Returns token which is the emitted token when the byte matches.
// Returns bool which is true when the byte produced a token.
func (t *tokeniser) readSingleCharacterToken(character byte) (token, bool) {
	mapped := singleCharacterTokens[character]
	if mapped == 0 {
		return noToken, false
	}
	startPosition := t.position
	t.position++
	return t.emit(mapped-1, startPosition, string(character)), true
}

// readMultiCharacterToken dispatches to the reader for the current byte.
//
// Takes character (byte) which is the byte at the current scanner position.
//
// Returns token which holds the scanned multi-character token.
// Returns error when the underlying reader fails.
func (t *tokeniser) readMultiCharacterToken(character byte) (token, error) {
	switch {
	case character == '@':
		return t.readAtToken()
	case character == ':':
		return t.readColonToken()
	case character == '\'':
		return t.readString()
	case character == '"' && t.doubleQuote == sqllex.DoubleQuoteString:
		return t.readEscapedQuoted('"', tokenString, "string literal")
	case character == '"':
		return t.readQuotedIdentifier()
	case character == '`':
		return t.readBacktickIdentifier()
	case (character == 'X' || character == 'x') && t.position+1 < len(t.input) && t.input[t.position+1] == '\'':
		return t.readHexString()
	case (character == 'B' || character == 'b') && t.position+1 < len(t.input) && t.input[t.position+1] == '\'':
		return t.readBitString()
	}
	leading, _ := utf8.DecodeRuneInString(t.input[t.position:])
	if isIdentifierStart(leading) {
		return t.readIdentifier()
	}
	return t.readOperator()
}

// skipWhitespaceAndComments advances past whitespace and SQL comments.
//
// An executable comment, /*!NNNNN ... */ or /*M!NNNNNN ... */, is not skipped: the server
// runs its contents, so only its delimiters and version number are passed over and the
// contents are read as SQL. An opener written inside a line comment or an ordinary block
// comment is part of that comment. Each byte is read once, so the cost grows linearly
// with the text skipped.
//
// Returns error when a block comment is left unterminated.
func (t *tokeniser) skipWhitespaceAndComments() error {
	for {
		if t.enterExecutableComment() || t.leaveExecutableComment() {
			continue
		}
		position, err := lexicalRules.Comments.SkipToExecutableComment(t.input, t.position)
		if err != nil {
			return err
		}
		if position == t.position {
			return nil
		}
		t.position = position
	}
}

// enterExecutableComment passes over the opening delimiter and version number of an
// executable comment.
//
// Returns bool which is true when an executable comment opened at the current position.
func (t *tokeniser) enterExecutableComment() bool {
	if t.insideExecutableComment {
		return false
	}
	opener := dialectkit.ExecutableCommentOpenerLength(t.input, t.position)
	if opener == 0 {
		return false
	}
	t.position += opener
	for t.position < len(t.input) && dialectkit.IsDigit(t.input[t.position]) {
		t.position++
	}
	t.insideExecutableComment = true
	return true
}

// leaveExecutableComment passes over the closing delimiter of an open executable comment.
//
// Returns bool which is true when an executable comment closed at the current position.
func (t *tokeniser) leaveExecutableComment() bool {
	if !t.insideExecutableComment || !strings.HasPrefix(t.input[t.position:], blockCommentCloser) {
		return false
	}
	t.position += len(blockCommentCloser)
	t.insideExecutableComment = false
	return true
}

// readString scans a single-quoted string literal with escape handling.
//
// Returns token which holds the decoded string contents.
// Returns error when the literal is not terminated before end of input.
func (t *tokeniser) readString() (token, error) {
	return t.readEscapedQuoted('\'', tokenString, "string literal")
}

// readEscapedQuoted scans text between two delimiter bytes the way MySQL's default mode
// reads a quoted string: a doubled delimiter stands for one, and a backslash escapes the
// byte after it.
//
// Takes delimiter (byte) which opens and closes the text.
// Takes kind (tokenKind) which classifies the produced token.
// Takes description (string) which names the construct in the error message.
//
// Returns token which holds the decoded contents.
// Returns error when the text is not terminated before end of input.
func (t *tokeniser) readEscapedQuoted(delimiter byte, kind tokenKind, description string) (token, error) {
	startPosition := t.position
	t.position++

	var builder strings.Builder
	for t.position < len(t.input) {
		character := t.input[t.position]
		if character == '\\' && t.position+1 < len(t.input) {
			t.position++
			escaped := t.input[t.position]

			if escaped == '%' || escaped == '_' {
				builder.WriteByte('\\')
				builder.WriteByte(escaped)
			} else {
				builder.WriteByte(mysqlBackslashEscape(escaped))
			}
			t.position++
			continue
		}
		if character == delimiter {
			t.position++
			if t.position < len(t.input) && t.input[t.position] == delimiter {
				builder.WriteByte(delimiter)
				t.position++
				continue
			}
			return t.emit(kind, startPosition, builder.String()), nil
		}
		builder.WriteByte(character)
		t.position++
	}

	return noToken, dialectkit.ParseErrorAt(t.input, startPosition, startPosition, "unterminated "+description)
}

// readHexString scans an X'...' hexadecimal literal.
//
// Returns token which holds the literal payload between the quotes.
// Returns error when the literal is not terminated before end of input.
func (t *tokeniser) readHexString() (token, error) {
	startPosition := t.position
	t.position += 2

	var builder strings.Builder
	for t.position < len(t.input) {
		character := t.input[t.position]
		if character == '\'' {
			t.position++
			return t.emit(tokenHexString, startPosition, builder.String()), nil
		}
		builder.WriteByte(character)
		t.position++
	}

	return noToken, dialectkit.ParseErrorAt(t.input, startPosition, startPosition, "unterminated hex string")
}

// readBitString scans a B'...' bit-string literal.
//
// Returns token which holds the literal payload between the quotes.
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

	return noToken, dialectkit.ParseErrorAt(t.input, startPosition, startPosition, "unterminated bit string")
}

// readBacktickIdentifier scans a backtick-delimited identifier.
//
// Returns token which holds the decoded identifier.
// Returns error when the identifier is not terminated before end of input.
func (t *tokeniser) readBacktickIdentifier() (token, error) {
	return t.readDelimitedIdentifier('`')
}

// readQuotedIdentifier scans a double-quoted name, read as an identifier as under
// ANSI_QUOTES. Only a doubled quote escapes a quote: MySQL reads a backslash in a quoted
// identifier as itself, so "a\" names a\.
//
// Returns token which holds the decoded identifier.
// Returns error when the identifier is not terminated before end of input.
func (t *tokeniser) readQuotedIdentifier() (token, error) {
	return t.readDelimitedIdentifier('"')
}

// readDelimitedIdentifier scans an identifier bounded by a given delimiter.
//
// Takes delimiter (byte) which is the byte that opens and closes the identifier and which
// is doubled to escape itself.
//
// Returns token which holds the decoded identifier.
// Returns error when the identifier is not terminated before end of input.
func (t *tokeniser) readDelimitedIdentifier(delimiter byte) (token, error) {
	startPosition := t.position

	value, position, ok := dialectkit.ScanDoubledDelimiter(t.input, t.position, delimiter)
	if !ok {
		return noToken, dialectkit.ParseErrorAt(t.input, startPosition, startPosition, "unterminated quoted identifier")
	}
	t.position = position

	identifier := t.emit(tokenIdentifier, startPosition, value)
	identifier.Quoted = true
	return identifier, nil
}

// readNumber scans a numeric literal: a 0x or 0b base-prefixed integer, or decimal digits
// with an optional fractional part and exponent, including a leading-dot literal such as
// .5.
//
// Returns token which holds the raw numeric text.
// Returns error when the literal cannot be scanned.
func (t *tokeniser) readNumber() (token, error) {
	startPosition := t.position

	end, err := lexicalRules.Numbers.ReadNumber(t.input, t.position)
	if err != nil {
		return noToken, err
	}
	t.position = end
	return t.emit(tokenNumber, startPosition, t.input[startPosition:t.position]), nil
}

// readIdentifier scans a bare identifier or unquoted keyword. After its first character
// an identifier may contain '$', as MySQL allows.
//
// Advances by UTF-8 rune width so multi-byte code points (Unicode letters or digits
// inside an identifier body, for example accented or CJK characters) are not truncated
// mid-code-point as they would be by a byte-at-a-time scan.
//
// Returns token which holds the identifier text.
// Returns error which is always nil; the signature aligns with peer readers.
func (t *tokeniser) readIdentifier() (token, error) {
	startPosition := t.position
	for t.position < len(t.input) {
		character, width := utf8.DecodeRuneInString(t.input[t.position:])
		if character != '$' && !isIdentifierPart(character) {
			break
		}
		t.position += width
	}
	return t.emit(tokenIdentifier, startPosition, t.input[startPosition:t.position]), nil
}

// readAtToken scans an @user or @@system variable reference.
//
// Returns token which holds the variable token including the @ prefix.
// Returns error which is always nil; the signature aligns with peer readers.
func (t *tokeniser) readAtToken() (token, error) {
	startPosition := t.position
	t.position++

	if t.position < len(t.input) && t.input[t.position] == '@' {
		t.position++
		t.consumeSystemVariableQualifier()
		t.consumeVariableName()
		return t.emit(tokenSystemVariable, startPosition, t.input[startPosition:t.position]), nil
	}

	t.consumeVariableName()
	return t.emit(tokenUserVariable, startPosition, t.input[startPosition:t.position]), nil
}

// consumeSystemVariableQualifier consumes a GLOBAL, SESSION or LOCAL qualifier and the
// dot after it, written in any case, as MySQL reads @@GLOBAL.time_zone and
// @@Session.sql_mode.
func (t *tokeniser) consumeSystemVariableQualifier() {
	if t.position >= len(t.input) {
		return
	}
	leading, _ := utf8.DecodeRuneInString(t.input[t.position:])
	if !isIdentifierStart(leading) {
		return
	}

	saved := t.position
	t.consumeVariableName()
	if t.position < len(t.input) && t.input[t.position] == '.' {
		qualifier := t.input[saved:t.position]
		if strings.EqualFold(qualifier, "global") || strings.EqualFold(qualifier, "session") ||
			strings.EqualFold(qualifier, "local") {
			t.position++
			return
		}
	}
	t.position = saved
}

// consumeVariableName advances over an unquoted variable name.
//
// Advances by UTF-8 rune width so multi-byte code points within the name are not
// truncated mid-code-point.
func (t *tokeniser) consumeVariableName() {
	for t.position < len(t.input) {
		character, width := utf8.DecodeRuneInString(t.input[t.position:])
		if !isIdentifierPart(character) {
			break
		}
		t.position += width
	}
}

// readColonToken scans a named-parameter placeholder or a colon operator. A colon written
// directly after an operand, as after the statement label of l1:LOOP, is the operator, so
// :name is a parameter only where an operand is expected.
//
// Returns token which holds the placeholder or operator token.
// Returns error which is always nil; the signature aligns with peer readers.
func (t *tokeniser) readColonToken() (token, error) {
	startPosition := t.position

	if t.position+1 < len(t.input) && !t.followsOperand(startPosition) {
		leading, _ := utf8.DecodeRuneInString(t.input[t.position+1:])
		if isIdentifierStart(leading) {
			t.position++
			t.consumeVariableName()
			return t.emit(tokenNamedParameter, startPosition, t.input[startPosition:t.position]), nil
		}
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

// readOperator scans an operator token, preferring longer matches.
//
// Returns token which holds the matched operator.
// Returns error when the current byte does not start a known operator.
func (t *tokeniser) readOperator() (token, error) {
	startPosition := t.position
	character := t.input[t.position]

	if current, ok := t.readArrowOperator(character, startPosition); ok {
		return current, nil
	}

	if current, ok := t.readThreeCharacterOperator(startPosition); ok {
		return current, nil
	}

	if current, ok := t.readTwoCharacterOperator(startPosition); ok {
		return current, nil
	}

	return t.readSingleCharacterOperator(character, startPosition)
}

// readArrowOperator scans -> and ->> JSON-extract operators.
//
// Takes character (byte) which is the byte at the current position.
// Takes startPosition (int) which is the byte offset where the token began.
//
// Returns token which holds the arrow operator when matched.
// Returns bool which is true when an arrow operator was consumed.
func (t *tokeniser) readArrowOperator(character byte, startPosition int) (token, bool) {
	if character != '-' || t.position+1 >= len(t.input) || t.input[t.position+1] != '>' {
		return noToken, false
	}
	if t.position+2 < len(t.input) && t.input[t.position+2] == '>' {
		t.position += threeCharacterOperatorLength
		return t.emit(tokenDoubleArrow, startPosition, "->>"), true
	}
	t.position += 2
	return t.emit(tokenArrow, startPosition, "->"), true
}

// readThreeCharacterOperator scans the <=> null-safe comparison operator.
//
// Takes startPosition (int) which is the byte offset where the token began.
//
// Returns token which holds the matched operator.
// Returns bool which is true when the three-character operator matches.
func (t *tokeniser) readThreeCharacterOperator(startPosition int) (token, bool) {
	if t.position+2 >= len(t.input) {
		return noToken, false
	}

	threeCharacters := t.input[t.position : t.position+threeCharacterOperatorLength]
	if threeCharacters == "<=>" {
		t.position += threeCharacterOperatorLength
		return t.emit(tokenOperator, startPosition, "<=>"), true
	}

	return noToken, false
}

// readTwoCharacterOperator scans a two-character operator listed in
// twoCharacterOperators.
//
// Takes startPosition (int) which is the byte offset where the token began.
//
// Returns token which holds the matched operator.
// Returns bool which is true when a two-character operator was consumed.
func (t *tokeniser) readTwoCharacterOperator(startPosition int) (token, bool) {
	if t.position+1 >= len(t.input) {
		return noToken, false
	}

	twoCharacters := t.input[t.position : t.position+2]
	if twoCharacterOperators[twoCharacters] {
		t.position += 2
		return t.emit(tokenOperator, startPosition, twoCharacters), true
	}

	return noToken, false
}

// readSingleCharacterOperator scans a single-byte operator.
//
// Takes character (byte) which is the byte at the current position.
// Takes startPosition (int) which is the byte offset where the token began.
//
// Returns token which holds the operator when matched.
// Returns error when the byte is not a known operator character.
func (t *tokeniser) readSingleCharacterOperator(character byte, startPosition int) (token, error) {
	singleCharacterOperators := "=<>+-/%~&|!^"
	if strings.ContainsRune(singleCharacterOperators, rune(character)) {
		t.position++
		return t.emit(tokenOperator, startPosition, string(character)), nil
	}

	return noToken, dialectkit.ParseErrorAt(t.input, startPosition, startPosition, "unexpected character")
}

func init() {
	singleCharacterTokens['('] = tokenLeftParen + 1
	singleCharacterTokens[')'] = tokenRightParen + 1
	singleCharacterTokens['['] = tokenLeftBracket + 1
	singleCharacterTokens[']'] = tokenRightBracket + 1
	singleCharacterTokens[','] = tokenComma + 1
	singleCharacterTokens[';'] = tokenSemicolon + 1
	singleCharacterTokens['.'] = tokenDot + 1
	singleCharacterTokens['*'] = tokenStar + 1
}

// mysqlBackslashEscape resolves a single MySQL backslash escape character.
//
// Takes character (byte) which is the byte that follows the backslash.
//
// Returns byte which is the escaped value, or the original byte when no special meaning
// applies.
func mysqlBackslashEscape(character byte) byte {
	switch character {
	case 'n':
		return '\n'
	case 't':
		return '\t'
	case 'r':
		return '\r'
	case 'b':
		return backspaceCharacter
	case '0':
		return 0
	case 'Z':
		return substituteCharacter
	default:
		return character
	}
}

// isIdentifierStart reports whether a rune may start an identifier.
//
// An ASCII fast-path admits ASCII letters and underscore directly; any rune above the
// ASCII range is admitted when unicode.IsLetter recognises it, so Unicode-named columns
// such as accented or CJK identifiers tokenise as single identifiers.
//
// Takes character (rune) which is the rune to test.
//
// Returns bool which is true for ASCII letters, underscore, or any non-ASCII letter
// recognised by unicode.IsLetter.
func isIdentifierStart(character rune) bool {
	return dialectkit.IsIdentStart(character)
}

// isIdentifierPart reports whether a rune may continue an identifier.
//
// This is the identifier-start set plus decimal digits. The ASCII fast-path guards the
// rune into the ASCII range before the digit check so the comparison never loses data;
// non-ASCII runes are admitted when unicode.IsLetter or unicode.IsDigit recognises them.
//
// Takes character (rune) which is the rune to test.
//
// Returns bool which is true for identifier-start runes or decimal digits.
func isIdentifierPart(character rune) bool {
	return dialectkit.IsIdentPart(character)
}

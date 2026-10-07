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
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"seagull.sh/seagull/internal/dialect/clickhouse/lexical"
	"seagull.sh/seagull/internal/dialect/dialectkit"
)

const (
	// escapeMarkerWidth is the single byte (x / u / U) following the backslash that selects
	// the escape form.
	escapeMarkerWidth = 1

	// hexByteEscapeDigits is the digit count of the \xHH escape form.
	hexByteEscapeDigits = 2

	// hexRuneEscapeDigits is the digit count of the \uHHHH escape form.
	hexRuneEscapeDigits = 4

	// hexLongRuneEscapeDigits is the digit count of the \UHHHHHHHH escape form.
	hexLongRuneEscapeDigits = 8

	// hexDigitBits is the number of bits one hexadecimal digit holds.
	hexDigitBits = 4

	// placeholderScanStops are the bytes that end the scan for a placeholder's colon after a
	// brace: the colon itself, either brace, and a newline, none of which a placeholder's
	// name holds.
	placeholderScanStops = ":{}\n"

	// hexAlphaOffset is the value added to an alphabetic hexadecimal digit (a-f / A-F) when
	// decoding it to its 0-15 numeric value.
	hexAlphaOffset = 10
)

// tokenKind classifies a single lexical token.
//
// Each kind maps to a small, well-defined parser state, so the set is kept narrow on
// purpose. ClickHouse's `{name:Type}` placeholder gets its own kind so the parser can
// pluck the name and type tag out without rescanning.
type tokenKind uint8

const (
	// tokenIdentifier is a bare, quoted, or backtick-quoted identifier.
	tokenIdentifier tokenKind = iota

	// tokenNumber is a numeric literal.
	tokenNumber

	// tokenString is a single-quoted string literal.
	tokenString

	// tokenOperator is an arithmetic, bitwise, or comparison operator.
	tokenOperator

	// tokenLeftParen is the `(` punctuation token.
	tokenLeftParen

	// tokenRightParen is the `)` punctuation token.
	tokenRightParen

	// tokenLeftBracket is the `[` punctuation token.
	tokenLeftBracket

	// tokenRightBracket is the `]` punctuation token.
	tokenRightBracket

	// tokenComma is the `,` punctuation token.
	tokenComma

	// tokenSemicolon is the `;` statement separator token.
	tokenSemicolon

	// tokenDot is the `.` qualifier and tuple-field-access token.
	tokenDot

	// tokenStar is the `*` wildcard and multiplication token.
	tokenStar

	// tokenClickHouseParam is the brace-delimited placeholder `{name:Type}`.
	//
	// The token's value is the placeholder body without braces (`name:Type`); the parser
	// splits on the embedded colon.
	tokenClickHouseParam

	// tokenArrow is the lambda arrow `->` (e.g. `arrayMap(x -> x + 1, arr)`).
	//
	// ClickHouse does not use `->` for JSON access (which postgres does); JSONExtract*
	// functions handle that.
	tokenArrow

	// tokenCast is the postfix `::` cast operator.
	//
	// ClickHouse accepts both `CAST(expr AS type)` and `expr::type`, with `::` being
	// idiomatic for short casts.
	tokenCast

	// tokenLeftBrace is a `{` that opens no placeholder, such as the brace of the map
	// literal `{'a': 1}`, which ClickHouse reads only in the data of an INSERT.
	tokenLeftBrace

	// tokenRightBrace is the `}` that closes a map literal.
	tokenRightBrace

	// tokenEOF marks the end of the input stream.
	tokenEOF
)

// token is one lexed token: its kind, its decoded value, its source text and byte span,
// and whether it is a delimited identifier, which keyword matching never treats as a
// keyword.
type token = dialectkit.Token[tokenKind]

// tokeniser advances over the input byte by byte.
//
// It retains the previous token kind to distinguish a decimal point from tuple access.
type tokeniser struct {
	// input is the SQL text being scanned.
	input string

	// position is the current byte offset of the scan cursor.
	position int

	// previous is the kind of the last token emitted, which decides whether a '.' before a
	// digit starts a number or accesses a tuple element.
	previous tokenKind

	// hasPrevious is true once a token has been emitted.
	hasPrevious bool
}

// read emits the next token and remembers its kind, which decides whether a '.' after it
// starts a number or accesses an element.
//
// Returns token which is the next lexed token, or a tokenEOF token when the input is
// exhausted.
// Returns error when a string, placeholder, or comment is malformed or unterminated.
func (t *tokeniser) read() (token, error) {
	current, err := t.next()
	if err != nil {
		return token{}, err
	}
	t.previous, t.hasPrevious = current.Kind, true
	return current, nil
}

// next emits the next token from the input stream.
//
// Whitespace and comments are skipped first, then the leading byte dispatches to the
// single-char or multi-char reader. Identifier-start dispatch decodes the leading UTF-8
// codepoint so a multi-byte rune (for example a Unicode letter at the start of a
// quoted-or-bare identifier) routes through readIdentifier rather than falling into
// readOperator's "unexpected character" path.
//
// Returns token which is the next lexed token, or a tokenEOF sentinel when the input is
// exhausted.
// Returns error when a string, placeholder, or comment is malformed or unterminated, in
// which case the token is the zero value.
func (t *tokeniser) next() (token, error) {
	if err := t.skipWhitespaceAndComments(); err != nil {
		return token{}, err
	}

	if t.position >= len(t.input) {
		return t.emit(tokenEOF, "", t.position), nil
	}

	character := t.input[t.position]

	if character == '.' && dialectkit.StartsNumber(t.input, t.position) && !t.followsOperand() {
		return t.readNumber()
	}
	if current, ok := t.readSingleCharToken(character); ok {
		return current, nil
	}

	return t.readMultiCharToken(character)
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

// emitDelimited builds a token read between delimiters: a string literal, or a quoted
// identifier such as "order" or `order`, which names an object and is never matched as a
// keyword.
//
// Takes kind (tokenKind) which classifies the token.
// Takes value (string) which is the text without its delimiters or escapes.
// Takes start (int) which is the byte offset of the opening delimiter.
//
// Returns token which is the built token.
func (t *tokeniser) emitDelimited(kind tokenKind, value string, start int) token {
	delimited := t.emit(kind, value, start)
	delimited.Quoted = kind == tokenIdentifier
	return delimited
}

// readSingleCharToken emits a punctuation token when the leading byte maps to one in the
// dispatch tables.
//
// Takes character (byte) which is the leading byte under the cursor.
//
// Returns token which is the lexed punctuation token when one matches.
// Returns bool which is true when the byte mapped to a single-character token.
func (t *tokeniser) readSingleCharToken(character byte) (token, bool) {
	mapped := singleCharTokens[character]
	if !mapped.ok {
		return token{}, false
	}
	startPosition := t.position
	t.position++
	return t.emit(mapped.kind, string(character), startPosition), true
}

// readMultiCharToken routes to the appropriate scanner for multi-byte constructs.
//
// Order matters: the colon must be tried before generic operators because `::` and `:`
// need separate handling, and a `{` is a placeholder when a name follows it and a brace
// of a map literal otherwise. For the identifier-start branch the leading UTF-8 codepoint
// is decoded so a multi-byte rune is correctly recognised; without the decode the
// function only inspects the high byte and the dispatch falls into readOperator on a
// Unicode letter.
//
// Takes character (byte) which is the leading byte under the cursor.
//
// Returns token which is the lexed multi-byte token.
// Returns error when the construct is malformed or unterminated.
func (t *tokeniser) readMultiCharToken(character byte) (token, error) {
	switch {
	case character == '{':
		if !t.opensPlaceholder() {
			startPosition := t.position
			t.position++
			return t.emit(tokenLeftBrace, "{", startPosition), nil
		}
		return t.readClickHouseParam()
	case character == ':':
		return t.readColonToken()
	case character == '\'':
		return t.readString()
	case character == '"':
		return t.readQuotedIdentifier()
	case character == '`':
		return t.readBacktickIdentifier()
	case character == '$':
		if current, isHeredoc, err := t.readHeredoc(); isHeredoc {
			return current, err
		}
	case isDigit(character):
		return t.readNumber()
	}
	leading, _ := utf8.DecodeRuneInString(t.input[t.position:])
	if isIdentStart(leading) {
		return t.readIdentifier()
	}
	return t.readOperator()
}

// skipWhitespaceAndComments advances the cursor past insignificant whitespace and SQL
// comments.
//
// The shared lexical rules recognise `--` and `#` line comments and nested `/* */` block
// comments.
//
// Returns error when a block comment is unterminated.
func (t *tokeniser) skipWhitespaceAndComments() error {
	position, err := lexicalRules.Comments.SkipWhitespaceAndComments(t.input, t.position)
	t.position = position
	return err
}

// readString reads a single-quoted ClickHouse string literal.
//
// It supports both doubled single-quote escapes and backslash escapes (`\\`, `\'`, `\n`,
// `\t`, `\r`, `\0`). The returned value carries the unescaped string body without the
// enclosing quotes.
//
// Returns token which is the lexed string token holding the unescaped body.
// Returns error when the string literal is unterminated.
func (t *tokeniser) readString() (token, error) {
	return t.readQuoted('\'', tokenString, "string literal")
}

// readQuoted reads text between two delimiter bytes the way ClickHouse reads every quoted
// form, as decodeQuoted decodes it.
//
// Takes delimiter (byte) which opens and closes the text.
// Takes kind (tokenKind) which classifies the produced token.
// Takes description (string) which names the construct in the error message.
//
// Returns token which holds the unescaped body without its delimiters.
// Returns error when the text is unterminated.
func (t *tokeniser) readQuoted(delimiter byte, kind tokenKind, description string) (token, error) {
	startPosition := t.position
	body, end, terminated := decodeQuoted(t.input, startPosition, delimiter)
	if !terminated {
		return token{}, dialectkit.ParseErrorAt(t.input, startPosition, startPosition, "unterminated "+description)
	}
	t.position = end
	return t.emitDelimited(kind, body, startPosition), nil
}

// readHeredoc reads a ClickHouse heredoc, a string written as $$...$$ or $tag$...$tag$
// and read verbatim up to the next occurrence of its opening tag, so quotes, terminators,
// backslashes and placeholders inside it are text.
//
// A tag starts with a letter or an underscore and continues with letters, digits and
// underscores, as the lexical leaf reads it.
//
// Returns token which is the string token holding the heredoc's body.
// Returns bool which is false when the cursor does not open a heredoc.
// Returns error when the heredoc's closing tag never comes.
func (t *tokeniser) readHeredoc() (token, bool, error) {
	startPosition := t.position
	openerEnd, opened := heredocOpenerEnd(t.input, startPosition)
	if !opened {
		return token{}, false, nil
	}
	opener := t.input[startPosition:openerEnd]
	body, _, closed := strings.Cut(t.input[openerEnd:], opener)
	if !closed {
		return token{}, true, dialectkit.ParseErrorAt(t.input, startPosition, startPosition, "unterminated heredoc string literal")
	}
	t.position = openerEnd + len(body) + len(opener)
	return t.emit(tokenString, body, startPosition), true, nil
}

// readQuotedIdentifier reads a double-quoted identifier.
//
// ClickHouse accepts both double-quoted and backtick-quoted identifiers; this helper
// handles the double-quote form. A doubled quote ("") or a backslash escape (\") stands
// for a double quote in the value.
//
// Returns token which is the lexed identifier token.
// Returns error when the quoted identifier is unterminated.
func (t *tokeniser) readQuotedIdentifier() (token, error) {
	return t.readQuoted('"', tokenIdentifier, "quoted identifier")
}

// readBacktickIdentifier reads a backtick-quoted identifier.
//
// A doubled backtick or a backslash escape (\`) stands for a backtick in the value.
//
// Returns token which is the lexed identifier token.
// Returns error when the quoted identifier is unterminated.
func (t *tokeniser) readBacktickIdentifier() (token, error) {
	return t.readQuoted('`', tokenIdentifier, "quoted identifier")
}

// opensPlaceholder reports whether the `{` under the cursor opens a `{name:Type}`
// placeholder: a name follows it and a type name follows the first colon on its line,
// each after any blanks, as the shared lexical rules read it.
//
// A brace followed by anything else, such as the key of the map literal `{'a': 1}`, the
// `}` of the empty map `{}`, or a name whose colon a value follows, as in the map literal
// `{true: 1}`, opens a map literal, as ClickHouse reads it when the text after the colon
// is no type. So does a brace whose name and type name are both literal words, as in the
// map literal `{true: false}`, which the rows of INSERT ... VALUES read as data and which
// elsewhere names no type.
//
// The scan stops at the first colon, brace or newline after the brace, so each brace
// costs only the length of its own placeholder: with no colon before the next brace or
// the end of the line the brace opens a placeholder, which the placeholder reader then
// reports as malformed.
//
// Returns bool which is true when the brace opens a placeholder.
func (t *tokeniser) opensPlaceholder() bool {
	body := t.input[t.position+1:]
	if !startsWithIdentifier(body) {
		return false
	}
	separator := strings.IndexAny(body, placeholderScanStops)
	if separator < 0 || body[separator] != ':' {
		return true
	}
	name, typeName := body[:separator], body[separator+1:]
	return startsWithIdentifier(typeName) && (!isLiteralWord(leadingIdentifier(name)) || !isLiteralWord(leadingIdentifier(typeName)))
}

// readClickHouseParam reads a placeholder of the form `{name:Type}`.
//
// The returned token's value is the placeholder body without braces, so callers can split
// on the embedded colon to recover the name and the ClickHouse type tag. The body must
// match `name:Type` where name is a valid identifier and Type is non-empty. Newlines and
// embedded `{` inside the body are rejected because they would surface as confusing
// parser errors downstream.
//
// Returns token which is the lexed placeholder token carrying the body without braces.
// Returns error when the closing brace is missing, the colon separating name from type is
// missing, the name or type is empty, or a newline or `{` appears inside the body.
func (t *tokeniser) readClickHouseParam() (token, error) {
	startPosition := t.position
	t.position++

	var builder strings.Builder
	for t.position < len(t.input) {
		character := t.input[t.position]
		if character == '}' {
			body := builder.String()
			t.position++
			if err := validateClickHouseParamBody(body, t.input[startPosition:t.position], startPosition); err != nil {
				return token{}, err
			}
			return t.emit(tokenClickHouseParam, body, startPosition), nil
		}
		if character == '\n' {
			return token{}, dialectkit.ParseErrorAt(t.input, startPosition, startPosition, "unexpected newline inside placeholder")
		}
		if character == '{' {
			return token{}, dialectkit.ParseErrorAt(t.input, startPosition, startPosition, "unexpected '{' inside placeholder")
		}
		builder.WriteByte(character)
		t.position++
	}

	return token{}, dialectkit.ParseErrorAt(t.input, startPosition, startPosition, "unterminated placeholder")
}

// readNumber reads a numeric literal: a base-prefixed integer (0x / 0o / 0b), or decimal
// digits with an optional fraction and exponent, including a literal that starts with its
// decimal point, such as .5.
//
// Returns token which is the lexed numeric token.
// Returns error when a base prefix is not followed by at least one digit.
func (t *tokeniser) readNumber() (token, error) {
	startPosition := t.position
	end, err := lexicalRules.Numbers.ReadNumber(t.input, t.position)
	if err != nil {
		return token{}, err
	}
	t.position = end
	return t.emit(tokenNumber, t.input[startPosition:t.position], startPosition), nil
}

// followsOperand reports whether the last token ends an operand, after which a '.'
// accesses an element, as in t.1, rather than starting a number such as .5.
//
// Returns bool which is true after an identifier, a number or a closing bracket.
func (t *tokeniser) followsOperand() bool {
	if !t.hasPrevious {
		return false
	}
	switch t.previous {
	case tokenIdentifier, tokenNumber, tokenRightParen, tokenRightBracket:
		return true
	default:
		return false
	}
}

// readIdentifier consumes an identifier from the current cursor.
//
// It advances by UTF-8 rune width so multi-byte codepoints (Unicode letters or digits
// inside an identifier body) are not truncated mid-codepoint as they would be by a
// byte-at-a-time scan.
//
// Returns token which is the lexed identifier token.
// Returns error which is always nil and present for interface symmetry with the other
// readers.
func (t *tokeniser) readIdentifier() (token, error) {
	startPosition := t.position
	for t.position < len(t.input) {
		character, width := utf8.DecodeRuneInString(t.input[t.position:])
		if !isIdentPart(character) {
			break
		}
		t.position += width
	}
	return t.emit(tokenIdentifier, t.input[startPosition:t.position], startPosition), nil
}

// readColonToken handles `::` (cast) and a single `:` (operator).
//
// ClickHouse does not use `:name` for named parameters (those are `{name:Type}` instead),
// so this scanner is simpler than postgres'.
//
// Returns token which is the cast or operator token.
// Returns error which is always nil and present for interface symmetry with the other
// readers.
func (t *tokeniser) readColonToken() (token, error) {
	startPosition := t.position

	if t.position+1 < len(t.input) && t.input[t.position+1] == ':' {
		t.position += 2
		return t.emit(tokenCast, "::", startPosition), nil
	}

	t.position++
	return t.emit(tokenOperator, ":", startPosition), nil
}

// readOperator scans the next operator token.
//
// ClickHouse's operator surface is a subset of postgres' (no JSON `->>`, `#>`, `#>>`
// since JSON access uses functions). The lambda arrow `->` becomes its own token kind so
// the parser can dispatch directly.
//
// Returns token which is the lexed operator token.
// Returns error when the byte does not start any valid operator.
func (t *tokeniser) readOperator() (token, error) {
	startPosition := t.position
	character := t.input[t.position]

	if current, ok := t.readArrowOperator(character, startPosition); ok {
		return current, nil
	}

	if current, ok := t.readTwoCharOperator(startPosition); ok {
		return current, nil
	}

	return t.readSingleCharOperator(character, startPosition)
}

// readArrowOperator handles the lambda arrow `->`.
//
// Takes character (byte) which is the leading byte under the cursor.
// Takes startPosition (int) which is the byte offset recorded on the token.
//
// Returns token which is the arrow token when one matches.
// Returns bool which is false when the cursor is not on a `-` or no `>` follows.
func (t *tokeniser) readArrowOperator(character byte, startPosition int) (token, bool) {
	if character != '-' || t.position+1 >= len(t.input) || t.input[t.position+1] != '>' {
		return token{}, false
	}
	t.position += 2
	return t.emit(tokenArrow, "->", startPosition), true
}

// readTwoCharOperator matches a two-character operator from the twoCharOperators set.
//
// Takes startPosition (int) which is the byte offset recorded on the token.
//
// Returns token which is the operator token when one matches.
// Returns bool which is false when the next two bytes do not form a known two-character
// operator.
func (t *tokeniser) readTwoCharOperator(startPosition int) (token, bool) {
	if t.position+1 >= len(t.input) {
		return token{}, false
	}

	twoChar := t.input[t.position : t.position+2]
	if _, ok := twoCharOperators[twoChar]; ok {
		t.position += 2
		return t.emit(tokenOperator, twoChar, startPosition), true
	}

	return token{}, false
}

// readSingleCharOperator scans a single-character operator.
//
// The allowed set covers the SQL arithmetic, bitwise, and comparison operators ClickHouse
// exposes. JDBC-style `?` placeholders are deliberately excluded because ClickHouse SQL
// uses `{name:Type}` for parameter binding; a stray `?` produces an explicit "unexpected
// character" diagnostic rather than silently tokenising as an operator.
//
// Takes character (byte) which is the candidate operator byte.
// Takes startPosition (int) which is the byte offset recorded on the token.
//
// Returns token which is the lexed operator token.
// Returns error when the byte is not a recognised single-character operator.
func (t *tokeniser) readSingleCharOperator(character byte, startPosition int) (token, error) {
	if strings.IndexByte("=<>+-/%~&|!^", character) >= 0 {
		t.position++
		return t.emit(tokenOperator, string(character), startPosition), nil
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
	// lexicalRules declares ClickHouse lexical rules for the shared scanners: nested block
	// comments, # line comments, 0x/0o/0b base-prefixed integer literals, and a strict
	// requirement that a base prefix be followed by at least one digit.
	lexicalRules = dialectkit.LexicalRules{
		Comments: dialectkit.CommentRules{
			NestedBlockComments:          true,
			DoubleDashRequiresWhitespace: false,
			HashLineComment:              true,
		},
		Numbers: dialectkit.NumberRules{
			HexPrefix:                true,
			OctalPrefix:              true,
			BinaryPrefix:             true,
			RequireDigitsAfterPrefix: true,
		},
	}

	// literalWords lists the words ClickHouse reads as values rather than names, which the
	// shared lexical rules give, so a brace holding only such words opens a map literal
	// where the shared scanner reads one.
	literalWords = lexical.Rules().Parameters.BracedLiteralWords

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
		'}': {kind: tokenRightBrace, ok: true},
	}

	// twoCharOperators holds the two-character operators ClickHouse exposes: the comparisons
	// `<=`, `>=`, `<>`, `!=`, logical `&&`, concatenation `||`, and bitwise shifts `<<` and
	// `>>`.
	twoCharOperators = map[string]struct{}{
		"<=": {}, ">=": {}, "<>": {}, "!=": {}, "||": {},
		"<<": {}, ">>": {}, "&&": {},
	}
)

// decodeStringEscape translates a single backslash-escape character to its expanded byte.
//
// An unrecognised escape returns the character after the backslash; the caller has
// already consumed the backslash.
//
// Takes escape (byte) which is the character following the backslash.
//
// Returns byte which is the decoded escape value.
func decodeStringEscape(escape byte) byte {
	switch escape {
	case 'n':
		return '\n'
	case 't':
		return '\t'
	case 'r':
		return '\r'
	case '0':
		return 0
	case '\\':
		return '\\'
	case '\'':
		return '\''
	case '"':
		return '"'
	default:
		return escape
	}
}

// heredocOpenerEnd reads the opener of a heredoc at start: $$, or $ then a tag and $.
//
// Takes input (string) which is the SQL text.
// Takes start (int) which is the offset of the opening '$'.
//
// Returns int which is the offset just past the opener.
// Returns bool which is false when start opens no heredoc.
func heredocOpenerEnd(input string, start int) (int, bool) {
	position := start + 1
	for position < len(input) && input[position] != '$' {
		character := input[position]
		isTagCharacter := character == '_' || (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(position > start+1 && isDigit(character))
		if !isTagCharacter {
			return 0, false
		}
		position++
	}
	if position >= len(input) {
		return 0, false
	}
	return position + 1, true
}

// startsWithIdentifier reports whether text starts with an identifier after any blanks.
//
// Takes text (string) which is the text.
//
// Returns bool which is true when the first character past the blanks starts an
// identifier.
func startsWithIdentifier(text string) bool {
	leading, _ := utf8.DecodeRuneInString(strings.TrimLeft(text, " \t"))
	return isIdentStart(leading)
}

// leadingIdentifier returns the identifier text starts with after any blanks.
//
// Takes text (string) which is the text.
//
// Returns string which is the identifier, or the empty string when none starts the text.
func leadingIdentifier(text string) string {
	text = strings.TrimLeft(text, " \t")
	end := strings.IndexFunc(text, func(character rune) bool { return !isIdentPart(character) })
	if end < 0 {
		return text
	}
	return text[:end]
}

// isLiteralWord reports whether a word is one ClickHouse reads as a value, such as true
// or null, compared without regard to case.
//
// Takes word (string) which is the word.
//
// Returns bool which is true for a literal word.
func isLiteralWord(word string) bool {
	return slices.ContainsFunc(literalWords, func(literal string) bool { return strings.EqualFold(literal, word) })
}

// validateClickHouseParamBody enforces the `name:Type` shape on a stripped parameter
// body.
//
// The colon must be present and divide the body into a non-empty identifier (name) and
// non-empty type tag.
//
// Takes body (string) which is the placeholder body without braces.
// Takes placeholder (string) which is the placeholder as written, braces included, which
// an error names.
// Takes position (int) which is the byte offset of the placeholder, where an error is.
//
// Returns error which is a *dialectkit.ParseError when the body lacks a colon, has an
// empty name or type, or has a name that does not start with an identifier character.
func validateClickHouseParamBody(body string, placeholder string, position int) error {
	nameSegment, typeSegment, found := strings.Cut(body, ":")
	if !found {
		return &dialectkit.ParseError{Token: placeholder, Message: "placeholder is missing ':' separator", Position: position}
	}
	name := strings.TrimSpace(nameSegment)
	typeTag := strings.TrimSpace(typeSegment)
	if name == "" {
		return &dialectkit.ParseError{Token: placeholder, Message: "placeholder has empty parameter name", Position: position}
	}
	if typeTag == "" {
		return &dialectkit.ParseError{Token: placeholder, Message: "placeholder has empty type tag", Position: position}
	}
	leading, _ := utf8.DecodeRuneInString(name)
	if !isIdentStart(leading) {
		message := fmt.Sprintf("placeholder parameter name %q does not start with an identifier character", name)
		return &dialectkit.ParseError{Token: placeholder, Message: message, Position: position}
	}
	return nil
}

// isDigit reports whether the byte is an ASCII decimal digit (0-9).
//
// Takes character (byte) which is the candidate digit.
//
// Returns bool which is true when the byte is in the range 0-9.
func isDigit(character byte) bool {
	return character >= '0' && character <= '9'
}

// isIdentStart reports whether the rune can begin a SQL identifier.
//
// The analyser accepts an ASCII letter, an underscore, or a non-ASCII letter according to
// unicode.IsLetter.
//
// Takes character (rune) which is the candidate leading rune.
//
// Returns bool which is true when the rune may start an identifier.
func isIdentStart(character rune) bool {
	return dialectkit.IsIdentStart(character)
}

// isIdentPart reports whether the rune can appear in a SQL identifier after the first
// character (identifier-start plus digits).
//
// Takes character (rune) which is the candidate body rune.
//
// Returns bool which is true when the rune may appear after the first character.
func isIdentPart(character rune) bool {
	return dialectkit.IsIdentPart(character)
}

// decodeQuoted decodes text between two delimiter bytes the way ClickHouse reads every
// quoted form, string literals, quoted identifiers and the labels of an enum type alike:
// a doubled delimiter stands for one, and a backslash starts an escape sequence, which
// decodeEscape reads.
//
// Takes input (string) which is the text holding the quoted form.
// Takes start (int) which is the offset of the opening delimiter.
// Takes delimiter (byte) which opens and closes the text.
//
// Returns string which is the decoded text without its delimiters.
// Returns int which is the offset just past the closing delimiter.
// Returns bool which is false when the text is unterminated.
func decodeQuoted(input string, start int, delimiter byte) (string, int, bool) {
	var builder strings.Builder
	position := start + 1
	for position < len(input) {
		character := input[position]
		if character == '\\' && position+1 < len(input) {
			position = decodeEscape(input, position, &builder)
			continue
		}
		if character == delimiter {
			position++
			if position < len(input) && input[position] == delimiter {
				builder.WriteByte(delimiter)
				position++
				continue
			}
			return builder.String(), position, true
		}
		builder.WriteByte(character)
		position++
	}
	return "", position, false
}

// decodeEscape decodes the escape sequence whose backslash is at position, writes the
// decoded bytes into builder, and returns the offset just past the sequence.
//
// Beyond the single-character escapes handled by decodeStringEscape it recognises hex
// (\xHH) and Unicode (\uHHHH, \UHHHHHHHH) forms. A truncated or non-hex sequence falls
// back to single-character decoding, which discards the backslash.
//
// Takes input (string) which is the text holding the sequence.
// Takes position (int) which is the offset of the leading backslash.
// Takes builder (*strings.Builder) which receives the decoded bytes.
//
// Returns int which is the offset just past the sequence.
func decodeEscape(input string, position int, builder *strings.Builder) int {
	position++
	if position >= len(input) {
		builder.WriteByte('\\')
		return position
	}
	marker := input[position]
	digits := position + escapeMarkerWidth
	switch marker {
	case 'x':
		if value, ok := decodeHexByte(input, digits); ok {
			builder.WriteByte(value)
			return digits + hexByteEscapeDigits
		}
	case 'u':
		if value, ok := decodeHexRune(input, digits, hexRuneEscapeDigits); ok {
			_, _ = builder.WriteRune(value)
			return digits + hexRuneEscapeDigits
		}
	case 'U':
		if value, ok := decodeHexRune(input, digits, hexLongRuneEscapeDigits); ok {
			_, _ = builder.WriteRune(value)
			return digits + hexLongRuneEscapeDigits
		}
	}
	builder.WriteByte(decodeStringEscape(marker))
	return position + 1
}

// decodeHexByte reads the two hexadecimal digits of a \xHH escape.
//
// Takes input (string) which is the text holding the digits.
// Takes start (int) which is the offset of the first digit.
//
// Returns byte which is the byte the digits give.
// Returns bool which is false when two hex digits do not follow.
func decodeHexByte(input string, start int) (byte, bool) {
	if start+hexByteEscapeDigits > len(input) {
		return 0, false
	}
	high, highValid := hexDigitValue(input[start])
	low, lowValid := hexDigitValue(input[start+1])
	if !highValid || !lowValid {
		return 0, false
	}
	return high<<hexDigitBits | low, true
}

// decodeHexRune reads exactly count hexadecimal digits starting at start as a Unicode
// code point.
//
// It returns false when fewer than count digits remain, a non-hex byte is encountered, or
// the value is not a valid code point, leaving fallback handling to the caller.
//
// Takes input (string) which is the text holding the digits.
// Takes start (int) which is the offset of the first hex digit.
// Takes count (int) which is the exact number of digits to read.
//
// Returns rune which is the decoded code point.
// Returns bool which is true when count valid hex digits name a code point.
func decodeHexRune(input string, start, count int) (rune, bool) {
	if start+count > len(input) {
		return 0, false
	}
	var value rune
	for offset := range count {
		digit, valid := hexDigitValue(input[start+offset])
		if !valid || value > unicode.MaxRune>>hexDigitBits {
			return 0, false
		}
		value = value<<hexDigitBits | rune(digit)
	}
	return value, value <= unicode.MaxRune
}

// hexDigitValue maps a single hexadecimal ASCII byte to its 0-15 value.
//
// Takes character (byte) which is the candidate hexadecimal digit.
//
// Returns digit (byte) which is the 0-15 value of the byte.
// Returns valid (bool) which is false for any non-hex byte.
func hexDigitValue(character byte) (digit byte, valid bool) {
	switch {
	case character >= '0' && character <= '9':
		return character - '0', true
	case character >= 'a' && character <= 'f':
		return character - 'a' + hexAlphaOffset, true
	case character >= 'A' && character <= 'F':
		return character - 'A' + hexAlphaOffset, true
	default:
		return 0, false
	}
}

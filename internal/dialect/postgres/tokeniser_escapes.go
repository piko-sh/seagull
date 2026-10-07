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
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"seagull.sh/seagull/internal/dialect/dialectkit"
)

const (
	// maxOctalEscapeDigits is the most octal digits a \ooo escape of an E'...' string reads.
	maxOctalEscapeDigits = 3

	// maxHexByteEscapeDigits is the most hexadecimal digits a \xhh escape of an E'...'
	// string reads.
	maxHexByteEscapeDigits = 2

	// shortUnicodeEscapeDigits is how many hexadecimal digits a \uXXXX escape, or a \XXXX
	// escape of a U&'...' string, holds.
	shortUnicodeEscapeDigits = 4

	// longUnicodeEscapeDigits is how many hexadecimal digits a \UXXXXXXXX escape of an
	// E'...' string holds.
	longUnicodeEscapeDigits = 8

	// plusUnicodeEscapeDigits is how many hexadecimal digits a \+XXXXXX escape of a U&'...'
	// string holds.
	plusUnicodeEscapeDigits = 6

	// octalDigitBits is how many bits an octal digit holds.
	octalDigitBits = 3

	// hexadecimalDigitBits is how many bits a hexadecimal digit holds.
	hexadecimalDigitBits = 4

	// unicodePrefixLength is the length of the U& that opens a Unicode escape string or
	// identifier.
	unicodePrefixLength = len("U&")

	// defaultUnicodeEscape is the escape character of a U&'...' string written without
	// UESCAPE.
	defaultUnicodeEscape = '\\'

	// invalidUnicodeEscapeMessage is the message of a syntax error at a malformed Unicode
	// escape.
	invalidUnicodeEscapeMessage = "invalid Unicode escape"
)

// readPrefixedLiteral reads a literal whose quote follows a prefix: E'...' with backslash
// escapes, B'...' and X'...' bit strings, N'...' national character strings, and U&'...'
// strings and U&"..." identifiers with Unicode escapes.
//
// Takes character (byte) which is the byte at the cursor.
//
// Returns token which is the literal's token.
// Returns bool which is true when a prefixed literal starts at the cursor.
// Returns error when the literal is unterminated or holds a malformed escape.
func (t *tokeniser) readPrefixedLiteral(character byte) (token, bool, error) {
	rest := t.input[t.position+1:]
	switch {
	case strings.HasPrefix(rest, "'"):
		switch character {
		case 'E', 'e':
			literal, err := t.readEscapeString()
			return literal, true, err
		case 'B', 'b', 'X', 'x':
			literal, err := t.readBitString()
			return literal, true, err
		case 'N', 'n':
			literal, err := t.readNationalString()
			return literal, true, err
		}
	case character == 'U' || character == 'u':
		switch {
		case strings.HasPrefix(rest, "&'"):
			literal, err := t.readUnicodeLiteral('\'', tokenString)
			return literal, true, err
		case strings.HasPrefix(rest, "&\""):
			literal, err := t.readUnicodeLiteral('"', tokenIdentifier)
			return literal, true, err
		}
	}
	return token{}, false, nil
}

// readNationalString reads an N'...' national character string, which PostgreSQL reads as
// a plain string.
//
// Returns token which is the string literal token, spanning its prefix.
// Returns error when the literal is unterminated.
func (t *tokeniser) readNationalString() (token, error) {
	startPosition := t.position
	t.position++
	literal, err := t.readString()
	if err != nil {
		return token{}, err
	}
	return t.emit(tokenString, startPosition, literal.Value), nil
}

// readUnicodeLiteral reads a U&'...' string or a U&"..." identifier, decoding its \XXXX
// and \+XXXXXX escapes, or those of the escape character a following UESCAPE 'c' names,
// which the token then spans too.
//
// Takes delimiter (byte) which is the quote that opens and closes the literal.
// Takes kind (tokenKind) which is tokenString or tokenIdentifier.
//
// Returns token which is the literal's token, its value decoded; an identifier is quoted.
// Returns error when the literal is unterminated, the UESCAPE clause is malformed, or an
// escape is.
func (t *tokeniser) readUnicodeLiteral(delimiter byte, kind tokenKind) (token, error) {
	startPosition := t.position
	body, position, closed := dialectkit.ScanDoubledDelimiter(t.input, t.position+unicodePrefixLength, delimiter)
	if !closed {
		return token{}, dialectkit.ParseErrorAt(t.input, startPosition, startPosition, "unterminated Unicode escape literal")
	}
	t.position = position
	escape, err := t.readUnicodeEscapeClause()
	if err != nil {
		return token{}, err
	}
	value, decoded := decodeUnicodeEscapes(body, escape)
	if !decoded {
		return token{}, dialectkit.ParseErrorAt(t.input, startPosition, t.position, invalidUnicodeEscapeMessage)
	}
	literal := t.emit(kind, startPosition, value)
	literal.Quoted = kind == tokenIdentifier
	return literal, nil
}

// readUnicodeEscapeClause reads the UESCAPE 'c' that may follow a Unicode escape literal,
// past any whitespace and comments, leaving the cursor where it was when none follows.
//
// Returns byte which is the escape character it names, or a backslash when none follows.
// Returns error when UESCAPE names no single character, or one that cannot escape.
func (t *tokeniser) readUnicodeEscapeClause() (byte, error) {
	afterLiteral := t.position
	keyword, err := lexicalRules.Comments.SkipWhitespaceAndComments(t.input, afterLiteral)
	if err != nil || !hasWordAt(t.input, keyword, "UESCAPE") {
		return defaultUnicodeEscape, nil
	}
	quote, err := lexicalRules.Comments.SkipWhitespaceAndComments(t.input, keyword+len("UESCAPE"))
	if err != nil || quote >= len(t.input) || t.input[quote] != '\'' {
		return 0, dialectkit.ParseErrorAt(t.input, keyword, keyword, "expected the escape character after UESCAPE")
	}
	escape, position, closed := dialectkit.ScanDoubledDelimiter(t.input, quote, '\'')
	if !closed || len(escape) != 1 || !canEscapeUnicode(escape[0]) {
		return 0, dialectkit.ParseErrorAt(t.input, quote, quote, "invalid Unicode escape character")
	}
	t.position = position
	return escape[0], nil
}

// readEscapeString reads an E'...' Postgres escape string literal, decoding its backslash
// escapes: \b, \f, \n, \r and \t, octal \o to \ooo, hexadecimal \xh and \xhh, and Unicode
// \uXXXX and \UXXXXXXXX; a backslash before any other character stands for that
// character.
//
// Returns token which is the escape string token.
// Returns error when the literal is unterminated or a Unicode escape is malformed.
func (t *tokeniser) readEscapeString() (token, error) {
	startPosition := t.position
	t.position += 2

	var builder strings.Builder
	for t.position < len(t.input) {
		character := t.input[t.position]
		if character == '\\' && t.position+1 < len(t.input) {
			next, decoded := decodeBackslashEscape(t.input, t.position+1, &builder)
			if !decoded {
				return token{}, dialectkit.ParseErrorAt(t.input, t.position, t.position, invalidUnicodeEscapeMessage)
			}
			t.position = next
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

// decodeBackslashEscape decodes the escape after a backslash in an E'...' string.
//
// Takes input (string) which is the text holding the string.
// Takes position (int) which is the offset of the byte after the backslash; it is within
// input.
// Takes builder (*strings.Builder) which receives the decoded bytes.
//
// Returns int which is the offset after the escape.
// Returns bool which is false for a malformed Unicode escape.
func decodeBackslashEscape(input string, position int, builder *strings.Builder) (int, bool) {
	character := input[position]
	switch character {
	case 'b':
		builder.WriteByte('\b')
	case 'f':
		builder.WriteByte('\f')
	case 'n':
		builder.WriteByte('\n')
	case 'r':
		builder.WriteByte('\r')
	case 't':
		builder.WriteByte('\t')
	case 'x':
		digits := countDigits(input, position+1, maxHexByteEscapeDigits, dialectkit.IsHexDigit)
		if digits == 0 {
			builder.WriteByte(character)
			break
		}
		builder.WriteByte(digitsByte(input[position+1:position+1+digits], hexadecimalDigitBits, hexDigitValue))
		return position + 1 + digits, true
	case 'u', 'U':
		return decodeEscapedRune(input, position, builder)
	default:
		if digits := countDigits(input, position, maxOctalEscapeDigits, isOctalDigit); digits > 0 {
			builder.WriteByte(digitsByte(input[position:position+digits], octalDigitBits, octalDigitValue))
			return position + digits, true
		}
		builder.WriteByte(character)
	}
	return position + 1, true
}

// digitsByte reads the byte a run of octal or hexadecimal digits names, keeping its low
// eight bits as PostgreSQL does for an octal escape past 255.
//
// Takes digits (string) which are the digits, each valid for the radix.
// Takes bitsPerDigit (byte) which is how many bits each digit holds.
// Takes value (func(byte) byte) which gives a digit's value.
//
// Returns byte which is the value's low eight bits.
func digitsByte(digits string, bitsPerDigit byte, value func(byte) byte) byte {
	var result byte
	for index := range len(digits) {
		result = result<<bitsPerDigit | value(digits[index])
	}
	return result
}

// octalDigitValue returns the value of an octal digit.
//
// Takes digit (byte) which is the digit, 0 to 7.
//
// Returns byte which is its value.
func octalDigitValue(digit byte) byte {
	return digit - '0'
}

// hexDigitValue returns the value of a hexadecimal digit.
//
// Takes digit (byte) which is the digit, in either case.
//
// Returns byte which is its value.
func hexDigitValue(digit byte) byte {
	switch {
	case digit >= 'a':
		return digit - 'a' + decimalBase
	case digit >= 'A':
		return digit - 'A' + decimalBase
	default:
		return digit - '0'
	}
}

// decodeEscapedRune decodes a \uXXXX or \UXXXXXXXX escape of an E'...' string, joining a
// \u escape of a high surrogate with the \u escape of the low surrogate that must follow.
//
// Takes input (string) which is the text holding the string.
// Takes position (int) which is the offset of the u or U after the backslash.
// Takes builder (*strings.Builder) which receives the decoded rune.
//
// Returns int which is the offset after the escape.
// Returns bool which is false when the escape is malformed or names no character.
func decodeEscapedRune(input string, position int, builder *strings.Builder) (int, bool) {
	width := shortUnicodeEscapeDigits
	if input[position] == 'U' {
		width = longUnicodeEscapeDigits
	}
	decoded, next, ok := hexCodePoint(input, position+1, width)
	if !ok {
		return position, false
	}
	if utf16.IsSurrogate(decoded) {
		if !strings.HasPrefix(input[next:], "\\u") {
			return position, false
		}
		low, afterLow, lowOk := hexCodePoint(input, next+len("\\u"), shortUnicodeEscapeDigits)
		decoded = utf16.DecodeRune(decoded, low)
		if !lowOk || decoded == unicode.ReplacementChar {
			return position, false
		}
		next = afterLow
	}
	if !utf8.ValidRune(decoded) {
		return position, false
	}
	builder.WriteString(string(decoded))
	return next, true
}

// decodeUnicodeEscapes decodes the escapes of a U&'...' string or U&"..." identifier: the
// escape character followed by four hexadecimal digits, or by + and six, names a
// character, a high surrogate followed by a low one names one character, and the escape
// character written twice stands for itself.
//
// Takes body (string) which is the literal's text between its quotes.
// Takes escape (byte) which is the escape character.
//
// Returns string which is the decoded text.
// Returns bool which is false when an escape is malformed or names no character.
func decodeUnicodeEscapes(body string, escape byte) (string, bool) {
	var builder strings.Builder
	for index := 0; index < len(body); {
		if body[index] != escape {
			builder.WriteByte(body[index])
			index++
			continue
		}
		if index+1 < len(body) && body[index+1] == escape {
			builder.WriteByte(escape)
			index += 2
			continue
		}
		decoded, next, ok := unicodeEscapeAt(body, index, escape)
		if ok && utf16.IsSurrogate(decoded) {
			var low rune
			low, next, ok = unicodeEscapeAt(body, next, escape)
			decoded = utf16.DecodeRune(decoded, low)
			ok = ok && decoded != unicode.ReplacementChar
		}
		if !ok || !utf8.ValidRune(decoded) {
			return "", false
		}
		builder.WriteString(string(decoded))
		index = next
	}
	return builder.String(), true
}

// unicodeEscapeAt reads the code point an escape of a U&'...' literal names: the escape
// character at index followed by four hexadecimal digits, or by + and six.
//
// Takes body (string) which is the literal's text.
// Takes index (int) which is the offset of the escape character.
// Takes escape (byte) which is the escape character.
//
// Returns rune which is the code point.
// Returns int which is the offset after the escape.
// Returns bool which is false when no well-formed escape starts at index.
func unicodeEscapeAt(body string, index int, escape byte) (rune, int, bool) {
	if index+1 >= len(body) || body[index] != escape {
		return 0, index, false
	}
	if body[index+1] == '+' {
		return hexCodePoint(body, index+2, plusUnicodeEscapeDigits)
	}
	return hexCodePoint(body, index+1, shortUnicodeEscapeDigits)
}

// hexCodePoint reads a code point written as exactly width hexadecimal digits.
//
// Takes text (string) which holds the digits.
// Takes start (int) which is the offset of the first digit.
// Takes width (int) which is how many digits the code point is written with.
//
// Returns rune which is the code point.
// Returns int which is the offset after the digits.
// Returns bool which is false when fewer than width hexadecimal digits follow.
func hexCodePoint(text string, start, width int) (rune, int, bool) {
	if countDigits(text, start, width, dialectkit.IsHexDigit) != width {
		return 0, start, false
	}
	value, err := strconv.ParseUint(text[start:start+width], 16, 32)
	if err != nil || value > unicode.MaxRune {
		return 0, start, false
	}
	return rune(value), start + width, true
}

// countDigits counts the digits that start at an offset, up to a limit.
//
// Takes text (string) which holds the digits.
// Takes start (int) which is the offset of the first candidate digit.
// Takes limit (int) which is the most digits counted.
// Takes isDigit (func(byte) bool) which reports whether a byte is a digit.
//
// Returns int which is how many digits follow, at most limit.
func countDigits(text string, start, limit int, isDigit func(byte) bool) int {
	count := 0
	for count < limit && start+count < len(text) && isDigit(text[start+count]) {
		count++
	}
	return count
}

// isOctalDigit reports whether a byte is an octal digit.
//
// Takes character (byte) which is the byte.
//
// Returns bool which is true for 0 to 7.
func isOctalDigit(character byte) bool {
	return character >= '0' && character <= '7'
}

// canEscapeUnicode reports whether a character may be the escape character UESCAPE names:
// anything but a hexadecimal digit, a plus sign, a quote or whitespace.
//
// Takes character (byte) which is the candidate.
//
// Returns bool which is true when it may escape.
func canEscapeUnicode(character byte) bool {
	return !dialectkit.IsHexDigit(character) && !strings.ContainsRune("+'\" \t\n\r\f", rune(character))
}

// hasWordAt reports whether a word, matched without regard to case, starts at an offset
// and ends there too, with no identifier character after it.
//
// Takes text (string) which is the text.
// Takes position (int) which is the offset.
// Takes word (string) which is the upper-case word.
//
// Returns bool which is true when the word is written at the offset.
func hasWordAt(text string, position int, word string) bool {
	end := position + len(word)
	if end > len(text) || !strings.EqualFold(text[position:end], word) {
		return false
	}
	next, _ := utf8.DecodeRuneInString(text[end:])
	return end == len(text) || !isIdentifierPart(next)
}

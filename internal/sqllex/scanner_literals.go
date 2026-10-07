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

package sqllex

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// prefixedOpenerWidth is the width of a one-letter prefix and its quote, such as E'.
	prefixedOpenerWidth = 2
)

// quotedAt reads a string literal or quoted identifier starting at start.
//
// Takes start (int) which is the candidate token's first byte.
//
// Returns lexeme which is the quoted token, or the error for an unterminated one.
// Returns bool which is true when start opens a quoted token under the rules.
func (s *Scanner) quotedAt(start int) (lexeme, bool) {
	quotes := &s.rules.Quotes
	switch character := s.input[start]; {
	case character == '\'':
		return s.quoted(start, 1, quotes.SingleQuoteEscape, KindString), true
	case character == '"':
		kind := KindQuotedIdentifier
		if quotes.DoubleQuote == DoubleQuoteString {
			kind = KindString
		}
		return s.quoted(start, 1, quotes.DoubleQuoteEscape, kind), true
	case character == '`' && quotes.Backticks:
		return s.quoted(start, 1, quotes.BacktickEscape, KindQuotedIdentifier), true
	case character == '[' && quotes.Brackets:
		return s.bracketed(start), true
	case character == '$' && quotes.DollarQuoting:
		return s.dollarQuoted(start)
	}
	return s.prefixedStringAt(start)
}

// prefixedStringAt reads a string literal whose quote follows a one-letter prefix, such
// as E'...' or X'...'.
//
// Takes start (int) which is the candidate prefix letter.
//
// Returns lexeme which is the string token, or the error for an unterminated one.
// Returns bool which is true when start opens a prefixed string under the rules.
func (s *Scanner) prefixedStringAt(start int) (lexeme, bool) {
	if start+1 >= len(s.input) || s.input[start+1] != '\'' {
		return lexeme{}, false
	}
	letter := upperASCII(s.input[start])
	if letter < 'A' || letter > 'Z' {
		return lexeme{}, false
	}
	quotes := &s.rules.Quotes
	if letter == 'E' && quotes.EscapeStrings {
		return s.quoted(start, prefixedOpenerWidth, EscapeDoubledOrBackslash, KindString), true
	}
	if strings.IndexByte(quotes.StringPrefixes, letter) >= 0 {
		return s.quoted(start, prefixedOpenerWidth, quotes.SingleQuoteEscape, KindString), true
	}
	return lexeme{}, false
}

// quoted reads a token closed by the same quote byte that ends its opener.
//
// Takes start (int) which is the token's first byte.
// Takes openerWidth (int) which is the width of the opener, its last byte the quote.
// Takes mode (EscapeMode) which says how the quote is escaped inside the token.
// Takes kind (Kind) which classifies the token.
//
// Returns lexeme which is the token, or the unterminated error for its kind.
func (s *Scanner) quoted(start, openerWidth int, mode EscapeMode, kind Kind) lexeme {
	quote := s.input[start+openerWidth-1]
	position := start + openerWidth
	for position < len(s.input) {
		character := s.input[position]
		if character == '\\' && mode.allowsBackslash() {
			position += len(`\'`)
			continue
		}
		if character != quote {
			position++
			continue
		}
		if mode.allowsDoubling() && position+1 < len(s.input) && s.input[position+1] == quote {
			position += len("''")
			continue
		}
		return lexeme{err: nil, end: position + 1, kind: kind}
	}
	return lexeme{err: unterminated(kind), end: len(s.input), kind: kind}
}

// bracketed reads a [...] identifier, closed by the first ']'.
//
// Takes start (int) which is the offset of the '['.
//
// Returns lexeme which is the identifier, or the unterminated-identifier error.
func (s *Scanner) bracketed(start int) lexeme {
	if index := strings.IndexByte(s.input[start+1:], ']'); index >= 0 {
		return lexeme{err: nil, end: start + 1 + index + 1, kind: KindQuotedIdentifier}
	}
	return lexeme{err: ErrUnterminatedQuotedIdentifier, end: len(s.input), kind: KindQuotedIdentifier}
}

// dollarQuoted reads a $tag$ ... $tag$ string starting at start.
//
// Takes start (int) which is the offset of the opening '$'.
//
// Returns lexeme which is the string, or the unterminated-dollar-quote error.
// Returns bool which is true when start opens a dollar-quoted string.
func (s *Scanner) dollarQuoted(start int) (lexeme, bool) {
	openerEnd, opened := dollarTagEnd(s.input, start)
	if !opened {
		return lexeme{}, false
	}
	closer := s.input[start:openerEnd]
	if index := strings.Index(s.input[openerEnd:], closer); index >= 0 {
		return lexeme{err: nil, end: openerEnd + index + len(closer), kind: KindString}, true
	}
	return lexeme{err: ErrUnterminatedDollarQuote, end: len(s.input), kind: KindString}, true
}

// parameterAt reads a bind-parameter placeholder starting at start.
//
// Takes start (int) which is the candidate token's first byte.
//
// Returns int which is the offset just past the placeholder.
// Returns bool which is true when start opens a placeholder under the rules.
func (s *Scanner) parameterAt(start int) (int, bool) {
	parameters := &s.rules.Parameters
	character := s.input[start]
	switch {
	case character == '?' && parameters.QuestionMark:
		if parameters.NumberedQuestionMark {
			return digitsEnd(s.input, start+1), true
		}
		return start + 1, true
	case character == '$' && parameters.DollarNumbers && start+1 < len(s.input) && isDigit(s.input[start+1]):
		return digitsEnd(s.input, start+1), true
	case character == '{' && parameters.Braced:
		return s.bracedParameterEnd(start)
	case strings.IndexByte(parameters.NamedSigils, character) >= 0:
		if parameters.NoNamedParameterAfterOperand && endsOperand(s.input[:start]) {
			return 0, false
		}
		return s.namedParameterEnd(start)
	}
	return 0, false
}

// bracedParameterEnd recognises a single-line {name:Type} placeholder with valid names
// and no nested braces, excluding pairs of literal words.
//
// Takes start (int) which is the offset of the '{'.
//
// Returns int which is the offset just past the '}'.
// Returns bool which is true when start opens a braced placeholder.
func (s *Scanner) bracedParameterEnd(start int) (int, bool) {
	body := s.input[start+1:]
	if !startsWithName(body) {
		return 0, false
	}
	closing := strings.IndexAny(body, "}{\n")
	if closing < 0 || body[closing] != '}' {
		return 0, false
	}
	name, typeName, found := strings.Cut(body[:closing], ":")
	if !found || !startsWithName(typeName) {
		return 0, false
	}
	if s.isLiteralWord(leadingName(name)) && s.isLiteralWord(leadingName(typeName)) {
		return 0, false
	}
	return start + 1 + closing + 1, true
}

// isLiteralWord reports whether a word is one the rules list in BracedLiteralWords,
// compared without regard to case.
//
// Takes word (string) which is the word.
//
// Returns bool which is true when the engine reads the word as a value.
func (s *Scanner) isLiteralWord(word string) bool {
	for _, literal := range s.rules.Parameters.BracedLiteralWords {
		if strings.EqualFold(literal, word) {
			return true
		}
	}
	return false
}

// namedParameterEnd reads a named placeholder: a sigil followed by an identifier.
//
// Takes start (int) which is the offset of the sigil.
//
// Returns int which is the offset just past the name.
// Returns bool which is true when an identifier follows the sigil. A doubled sigil is not
// a placeholder.
func (s *Scanner) namedParameterEnd(start int) (int, bool) {
	leading, _ := utf8.DecodeRuneInString(s.input[start+1:])
	if !isIdentifierStart(leading) {
		return 0, false
	}
	return s.identifierEnd(start + 1), true
}

// numberAt reads a numeric literal starting at start: digits with an optional fraction
// and exponent, a fraction with a leading point, or a base-prefixed integer such as 0x1F.
//
// Takes start (int) which is the candidate token's first byte.
//
// Returns int which is the offset just past the literal.
// Returns bool which is true when start opens a number.
func (s *Scanner) numberAt(start int) (int, bool) {
	input := s.input
	if !isDigit(input[start]) && (input[start] != '.' || start+1 >= len(input) || !isDigit(input[start+1])) {
		return 0, false
	}
	if end, prefixed := basePrefixedEnd(input, start); prefixed {
		return end, true
	}
	position := digitsEnd(input, start)
	if position < len(input) && input[position] == '.' {
		position = digitsEnd(input, position+1)
	}
	return exponentEnd(input, position), true
}

// wordAt reads an unquoted identifier or keyword starting at start.
//
// Takes start (int) which is the candidate token's first byte.
//
// Returns int which is the offset just past the word.
// Returns bool which is true when start opens a word.
func (s *Scanner) wordAt(start int) (int, bool) {
	leading, _ := utf8.DecodeRuneInString(s.input[start:])
	if !isIdentifierStart(leading) {
		return 0, false
	}
	return s.identifierEnd(start), true
}

// identifierEnd returns the offset after the identifier characters starting at start,
// counting '$' when the rules allow it inside identifiers.
//
// Takes start (int) which is the offset of the identifier's first character.
//
// Returns int which is the offset of the first character that cannot continue it.
func (s *Scanner) identifierEnd(start int) int {
	position := start
	for position < len(s.input) {
		character, width := utf8.DecodeRuneInString(s.input[position:])
		if !isIdentifierPart(character) && (character != '$' || !s.rules.DollarInIdentifiers) {
			break
		}
		position += width
	}
	return position
}

// punctuationEnd returns the end of the punctuation token starting at start: one rune, or
// both bytes of a doubled named-parameter sigil such as ::.
//
// Takes start (int) which is the token's first byte.
//
// Returns int which is the offset just past the punctuation.
func (s *Scanner) punctuationEnd(start int) int {
	character := s.input[start]
	if start+1 < len(s.input) && s.input[start+1] == character &&
		strings.IndexByte(s.rules.Parameters.NamedSigils, character) >= 0 {
		return start + len("::")
	}
	_, width := utf8.DecodeRuneInString(s.input[start:])
	return start + width
}

// dollarTagEnd reads the dollar-quote opener at start: $$, or $ then a tag that starts
// with a letter or underscore, continues with letters, digits and underscores, and ends
// with $.
//
// Takes input (string) which is the text.
// Takes start (int) which is the offset of the '$'.
//
// Returns int which is the offset just past the opener.
// Returns bool which is true when start opens a dollar-quoted string.
func dollarTagEnd(input string, start int) (int, bool) {
	position := start + 1
	if position < len(input) && input[position] == '$' {
		return position + 1, true
	}
	leading, width := utf8.DecodeRuneInString(input[position:])
	if !isIdentifierStart(leading) {
		return 0, false
	}
	position += width
	for position < len(input) {
		character, characterWidth := utf8.DecodeRuneInString(input[position:])
		if character == '$' {
			return position + 1, true
		}
		if !isIdentifierPart(character) {
			return 0, false
		}
		position += characterWidth
	}
	return 0, false
}

// unterminated returns the sentinel for an unterminated token of kind.
//
// Takes kind (Kind) which is the token's kind.
//
// Returns error which is ErrUnterminatedQuotedIdentifier or ErrUnterminatedString.
func unterminated(kind Kind) error {
	if kind == KindQuotedIdentifier {
		return ErrUnterminatedQuotedIdentifier
	}
	return ErrUnterminatedString
}

// endsOperand reports whether text ends with the last character of an operand: a name or
// number, a closing quote, or a closing bracket.
//
// Takes text (string) which is the input before a candidate sigil.
//
// Returns bool which is true when an operand ends right before the sigil.
func endsOperand(text string) bool {
	if text == "" {
		return false
	}
	last, _ := utf8.DecodeLastRuneInString(text)
	return isIdentifierPart(last) || strings.ContainsRune("'\"`)]}", last)
}

// startsWithName reports whether text starts with a name after any blanks.
//
// Takes text (string) which is the text.
//
// Returns bool which is true when the first character past the blanks starts a name.
func startsWithName(text string) bool {
	leading, _ := utf8.DecodeRuneInString(strings.TrimLeft(text, " \t"))
	return isIdentifierStart(leading)
}

// leadingName returns the name text starts with after any blanks.
//
// Takes text (string) which is the text.
//
// Returns string which is the name, or the empty string when no name starts the text.
func leadingName(text string) string {
	text = strings.TrimLeft(text, " \t")
	end := strings.IndexFunc(text, func(character rune) bool { return !isIdentifierPart(character) })
	if end < 0 {
		return text
	}
	return text[:end]
}

// basePrefixedEnd reads a base-prefixed integer such as 0x1F, 0o17 or 0b101.
//
// Takes input (string) which is the text.
// Takes start (int) which is the offset of the leading '0'.
//
// Returns int which is the offset just past the literal.
// Returns bool which is true when start opens a prefixed integer.
func basePrefixedEnd(input string, start int) (int, bool) {
	if input[start] != '0' || start+2 >= len(input) || !isAlphanumeric(input[start+2]) {
		return 0, false
	}
	switch input[start+1] {
	case 'x', 'X', 'o', 'O', 'b', 'B':
	default:
		return 0, false
	}
	position := start + len("0x")
	for position < len(input) && (isAlphanumeric(input[position]) || input[position] == '_') {
		position++
	}
	return position, true
}

// exponentEnd reads an optional exponent, e or E with an optional sign and at least one
// digit, starting at position.
//
// Takes input (string) which is the text.
// Takes position (int) which is the offset after the mantissa.
//
// Returns int which is the offset after the exponent, or position when there is none.
func exponentEnd(input string, position int) int {
	if position >= len(input) || (input[position] != 'e' && input[position] != 'E') {
		return position
	}
	exponent := position + 1
	if exponent < len(input) && (input[exponent] == '+' || input[exponent] == '-') {
		exponent++
	}
	if exponent >= len(input) || !isDigit(input[exponent]) {
		return position
	}
	return digitsEnd(input, exponent)
}

// digitsEnd returns the offset after the run of ASCII digits starting at position.
//
// Takes input (string) which is the text.
// Takes position (int) which is the offset to start from.
//
// Returns int which is the offset of the first non-digit, or the length of input.
func digitsEnd(input string, position int) int {
	for position < len(input) && isDigit(input[position]) {
		position++
	}
	return position
}

// isIdentifierStart reports whether character can begin an unquoted identifier: a letter
// (in any script) or an underscore.
//
// Takes character (rune) which is the rune to classify.
//
// Returns bool which is true when character can begin an identifier.
func isIdentifierStart(character rune) bool {
	if character < utf8.RuneSelf {
		return character == '_' || (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z')
	}
	return unicode.IsLetter(character)
}

// isIdentifierPart reports whether character can continue an unquoted identifier: an
// identifier-start character or a digit.
//
// Takes character (rune) which is the rune to classify.
//
// Returns bool which is true when character can continue an identifier.
func isIdentifierPart(character rune) bool {
	if character < utf8.RuneSelf {
		return isIdentifierStart(character) || (character >= '0' && character <= '9')
	}
	return unicode.IsLetter(character) || unicode.IsDigit(character)
}

// isDigit reports whether character is an ASCII digit.
//
// Takes character (byte) which is the byte to classify.
//
// Returns bool which is true for '0' to '9'.
func isDigit(character byte) bool {
	return character >= '0' && character <= '9'
}

// isAlphanumeric reports whether character is an ASCII letter or digit.
//
// Takes character (byte) which is the byte to classify.
//
// Returns bool which is true for letters and digits.
func isAlphanumeric(character byte) bool {
	folded := upperASCII(character)
	return isDigit(character) || (folded >= 'A' && folded <= 'Z')
}

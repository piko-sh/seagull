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

package martian

import (
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/sqllex"
)

const (
	// lineCommentPrefix opens a comment that runs to the end of the line.
	lineCommentPrefix = "--"

	// blockCommentOpen opens a block comment.
	blockCommentOpen = "/*"

	// blockCommentClose closes a block comment.
	blockCommentClose = "*/"

	// placeholderPrefix opens a parameter placeholder such as @p1.
	placeholderPrefix = "@p"

	// concatenationOperator is the operator Martian does not have.
	concatenationOperator = "||"
)

const (
	// tokenIdentifier is an unquoted identifier or keyword, folded to upper case.
	tokenIdentifier tokenKind = iota

	// tokenQuotedIdentifier is a bracket-delimited identifier, kept as written.
	tokenQuotedIdentifier

	// tokenNumber is an integer or decimal literal.
	tokenNumber

	// tokenString is a single-quoted string literal.
	tokenString

	// tokenPlaceholder is a parameter placeholder such as @p1; its value is the number.
	tokenPlaceholder

	// tokenSymbol is punctuation or an operator.
	tokenSymbol

	// tokenEnd marks the end of the input.
	tokenEnd
)

var (
	// twoCharacterSymbols lists the operators spelt with two characters.
	twoCharacterSymbols = []string{"<=", ">=", "<>"}
)

// tokenKind classifies a token.
type tokenKind uint8

// token is one lexical unit of Martian SQL.
type token struct {
	// value is the folded identifier, the delimited name, the literal text, the placeholder
	// number or the symbol.
	value string

	// offset is the byte offset of the token in the input.
	offset int

	// end is the byte offset one past the token's last byte.
	end int

	// kind classifies the token.
	kind tokenKind
}

// isKeyword reports whether the token is the given keyword. Only unquoted identifiers are
// keywords, so [SELECT] names a column.
//
// Takes keyword (string) which is the upper-case keyword.
//
// Returns bool which is true when the token is that keyword.
func (t token) isKeyword(keyword string) bool {
	return t.kind == tokenIdentifier && t.value == keyword
}

// isSymbol reports whether the token is the given symbol.
//
// Takes symbol (string) which is the symbol to compare with.
//
// Returns bool which is true when the token is that symbol.
func (t token) isSymbol(symbol string) bool {
	return t.kind == tokenSymbol && t.value == symbol
}

// isName reports whether the token can name a table or column.
//
// Returns bool which is true for unquoted and delimited identifiers.
func (t token) isName() bool {
	return t.kind == tokenIdentifier || t.kind == tokenQuotedIdentifier
}

// tokeniser splits Martian SQL into tokens.
type tokeniser struct {
	// input is the SQL being split.
	input string

	// tokens collects the tokens found so far.
	tokens []token

	// position is the byte offset of the next unread character.
	position int
}

// next consumes the token, the stretch of whitespace or comment, or the byte order mark
// that opens the input, at the current position.
//
// Returns error when the text at the current position is not valid Martian.
func (s *tokeniser) next() error {
	rest := s.input[s.position:]
	character := rest[0]
	switch {
	case s.position == 0 && strings.HasPrefix(rest, sqllex.ByteOrderMark):
		s.position = len(sqllex.ByteOrderMark)
		return nil
	case isSpace(character):
		s.position++
		return nil
	case strings.HasPrefix(rest, lineCommentPrefix):
		s.skipLineComment()
		return nil
	case strings.HasPrefix(rest, blockCommentOpen):
		return s.skipBlockComment()
	case strings.HasPrefix(rest, concatenationOperator):
		return newSyntaxError(s.input, s.position, s.position+len(concatenationOperator), "Martian has no || operator; join strings with CONCAT")
	case character == '[':
		return s.readQuotedIdentifier()
	case character == '\'':
		return s.readString()
	case character == '@':
		return s.readPlaceholder()
	case isDigit(character):
		s.readNumber()
		return nil
	case isIdentifierStart(character):
		s.readIdentifier()
		return nil
	default:
		return s.readSymbol()
	}
}

// skipLineComment skips a comment up to, but not including, the end of the line.
func (s *tokeniser) skipLineComment() {
	end := strings.IndexByte(s.input[s.position:], '\n')
	if end < 0 {
		s.position = len(s.input)
		return
	}
	s.position += end
}

// skipBlockComment skips a block comment.
//
// Returns error when the comment never closes.
func (s *tokeniser) skipBlockComment() error {
	end := strings.Index(s.input[s.position+len(blockCommentOpen):], blockCommentClose)
	if end < 0 {
		return newSyntaxError(s.input, s.position, s.position+len(blockCommentOpen), "unterminated block comment")
	}
	s.position += len(blockCommentOpen) + end + len(blockCommentClose)
	return nil
}

// readQuotedIdentifier reads a bracket-delimited identifier, in which ]] stands for a
// literal closing bracket.
//
// Returns error when the identifier never closes or is empty.
func (s *tokeniser) readQuotedIdentifier() error {
	start := s.position
	var name strings.Builder
	for index := start + 1; index < len(s.input); index++ {
		if s.input[index] != ']' {
			name.WriteByte(s.input[index])
			continue
		}
		if index+1 < len(s.input) && s.input[index+1] == ']' {
			name.WriteByte(']')
			index++
			continue
		}
		if name.Len() == 0 {
			return newSyntaxError(s.input, start, index+1, "empty delimited identifier")
		}
		s.tokens = append(s.tokens, token{value: name.String(), offset: start, end: index + 1, kind: tokenQuotedIdentifier})
		s.position = index + 1
		return nil
	}
	return newSyntaxError(s.input, start, start+1, "unterminated delimited identifier")
}

// readString reads a single-quoted string literal, in which a doubled quote stands for
// one quote.
//
// Returns error when the literal never closes.
func (s *tokeniser) readString() error {
	start := s.position
	var text strings.Builder
	for index := start + 1; index < len(s.input); index++ {
		if s.input[index] != '\'' {
			text.WriteByte(s.input[index])
			continue
		}
		if index+1 < len(s.input) && s.input[index+1] == '\'' {
			text.WriteByte('\'')
			index++
			continue
		}
		s.tokens = append(s.tokens, token{value: text.String(), offset: start, end: index + 1, kind: tokenString})
		s.position = index + 1
		return nil
	}
	return newSyntaxError(s.input, start, start+1, "unterminated string literal")
}

// readPlaceholder reads a parameter placeholder, @p followed by a number from 1.
//
// Returns error when the placeholder is not @p and a positive number.
func (s *tokeniser) readPlaceholder() error {
	start := s.position
	rest := s.input[start:]
	if !strings.HasPrefix(strings.ToLower(rest), placeholderPrefix) {
		return newSyntaxError(s.input, start, start+1, "a placeholder is written @p and a number")
	}
	end := len(placeholderPrefix)
	for end < len(rest) && isDigit(rest[end]) {
		end++
	}
	number, err := strconv.Atoi(rest[len(placeholderPrefix):end])
	if err != nil || number < 1 {
		return newSyntaxError(s.input, start, start+end, "a placeholder is numbered from @p1")
	}
	s.tokens = append(s.tokens, token{value: strconv.Itoa(number), offset: start, end: start + end, kind: tokenPlaceholder})
	s.position = start + end
	return nil
}

// readNumber reads an integer or decimal literal.
func (s *tokeniser) readNumber() {
	start := s.position
	end := start
	for end < len(s.input) && (isDigit(s.input[end]) || s.input[end] == '.') {
		end++
	}
	s.tokens = append(s.tokens, token{value: s.input[start:end], offset: start, end: end, kind: tokenNumber})
	s.position = end
}

// readIdentifier reads an unquoted identifier and folds it to upper case.
func (s *tokeniser) readIdentifier() {
	start := s.position
	end := start
	for end < len(s.input) && isIdentifierPart(s.input[end]) {
		end++
	}
	s.tokens = append(s.tokens, token{value: strings.ToUpper(s.input[start:end]), offset: start, end: end, kind: tokenIdentifier})
	s.position = end
}

// readSymbol reads punctuation or an operator.
//
// Returns error when the character is not one Martian knows.
func (s *tokeniser) readSymbol() error {
	start := s.position
	for _, symbol := range twoCharacterSymbols {
		if strings.HasPrefix(s.input[start:], symbol) {
			s.tokens = append(s.tokens, token{value: symbol, offset: start, end: start + len(symbol), kind: tokenSymbol})
			s.position += len(symbol)
			return nil
		}
	}
	character := s.input[start]
	if !strings.ContainsRune("(),;.*=<>+-/", rune(character)) {
		return newSyntaxError(s.input, start, start+1, "unexpected character")
	}
	s.tokens = append(s.tokens, token{value: string(character), offset: start, end: start + 1, kind: tokenSymbol})
	s.position++
	return nil
}

// tokenise splits Martian SQL into tokens, ending with a tokenEnd token.
//
// Takes input (string) which is the SQL to split.
//
// Returns []token which holds the tokens in order.
// Returns error when the input holds an unterminated literal or comment, the || operator,
// a malformed placeholder or a character Martian does not know.
func tokenise(input string) ([]token, error) {
	state := &tokeniser{input: input, tokens: nil, position: 0}
	for state.position < len(input) {
		if err := state.next(); err != nil {
			return nil, err
		}
	}
	state.tokens = append(state.tokens, token{value: "", offset: len(input), end: len(input), kind: tokenEnd})
	return state.tokens, nil
}

// isSpace reports whether a byte is whitespace.
//
// Takes character (byte) which is the byte to classify.
//
// Returns bool which is true for spaces, tabs, carriage returns and newlines.
func isSpace(character byte) bool {
	return character == ' ' || character == '\t' || character == '\r' || character == '\n'
}

// isDigit reports whether a byte is a decimal digit.
//
// Takes character (byte) which is the byte to classify.
//
// Returns bool which is true for 0 to 9.
func isDigit(character byte) bool {
	return character >= '0' && character <= '9'
}

// isIdentifierStart reports whether a byte can start an unquoted identifier.
//
// Takes character (byte) which is the byte to classify.
//
// Returns bool which is true for ASCII letters and the underscore.
func isIdentifierStart(character byte) bool {
	return (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || character == '_'
}

// isIdentifierPart reports whether a byte can continue an unquoted identifier.
//
// Takes character (byte) which is the byte to classify.
//
// Returns bool which is true for identifier starts and digits.
func isIdentifierPart(character byte) bool {
	return isIdentifierStart(character) || isDigit(character)
}

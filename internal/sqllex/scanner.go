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
)

const (
	// ByteOrderMark is the UTF-8 byte order mark some editors write at the start of a file.
	// The scanner reads it as whitespace when it opens the input.
	ByteOrderMark = "\uFEFF"
)

// Scanner reads SQL tokens under an engine's lexical rules.
//
// Next advances the cursor, and Err reports unterminated constructs. Copying the scanner
// permits independent lookahead. A scanner is not safe for concurrent use.
type Scanner struct {
	// err is the first lexical error, or nil.
	err error

	// input is the text being scanned.
	input string

	// rules describes the engine's lexical rules.
	rules Rules

	// position is the byte offset of the next token.
	position int
}

// NewScanner returns a Scanner positioned at the start of input.
//
// Takes rules (Rules) which describes the engine's lexical rules.
// Takes input (string) which is the SQL text.
//
// Returns *Scanner which reads input from its first byte.
func NewScanner(rules Rules, input string) *Scanner {
	return &Scanner{err: nil, input: input, rules: rules, position: 0}
}

// Next reads the next token, whitespace and comments included.
//
// Returns Token which is the token read.
// Returns bool which is false at the end of the input or after a lexical error; Err then
// says which.
func (s *Scanner) Next() (Token, bool) {
	if s.err != nil || s.position >= len(s.input) {
		return Token{}, false
	}
	start := s.position
	found := s.scanAt(start)
	if found.err != nil {
		s.err = newError(s.input, start, found.err)
		s.position = len(s.input)
		return Token{}, false
	}
	s.position = found.end
	return Token{Raw: s.input[start:found.end], Start: start, End: found.end, Kind: found.kind}, true
}

// NextSignificant reads the next token that is not whitespace or an ordinary comment.
// Executable comments are significant tokens.
//
// Returns Token which is the token read.
// Returns bool which is false at the end of the input or after a lexical error.
func (s *Scanner) NextSignificant() (Token, bool) {
	for {
		token, ok := s.Next()
		if !ok || !token.Kind.IsNoise() {
			return token, ok
		}
	}
}

// SkipNoise advances past whitespace and ordinary comments without reading the token
// after them. Executable comments stop the scan.
//
// Returns int which is the offset of the next significant byte, or the length of the
// input when only noise remains. An unterminated block comment stops the scanner and is
// reported through Err.
func (s *Scanner) SkipNoise() int {
	for s.err == nil && s.position < len(s.input) {
		found, isNoise := s.noiseAt(s.position)
		if !isNoise {
			break
		}
		if found.err != nil {
			s.err = newError(s.input, s.position, found.err)
			s.position = len(s.input)
			break
		}
		s.position = found.end
	}
	return s.position
}

// Offset returns the byte offset of the next token.
//
// Returns int which is the offset.
func (s *Scanner) Offset() int {
	return s.position
}

// Err returns the lexical error that stopped the scanner.
//
// Returns error which is an *Error, or nil when none occurred.
func (s *Scanner) Err() error {
	return s.err
}

// seek moves the scanner to offset, which must be a token boundary.
//
// Takes offset (int) which is the byte offset of the next token.
func (s *Scanner) seek(offset int) {
	s.position = offset
}

// scanAt reads the token starting at start.
//
// Takes start (int) which is the token's first byte.
//
// Returns lexeme which is the token's kind and end, or the error that stopped it.
func (s *Scanner) scanAt(start int) lexeme {
	if found, isNoise := s.noiseAt(start); isNoise {
		return found
	}
	if found, matched := s.executableCommentAt(start); matched {
		return found
	}
	if found, matched := s.quotedAt(start); matched {
		return found
	}
	if end, matched := s.parameterAt(start); matched {
		return lexeme{err: nil, end: end, kind: KindParameter}
	}
	if end, matched := s.numberAt(start); matched {
		return lexeme{err: nil, end: end, kind: KindNumber}
	}
	if end, matched := s.wordAt(start); matched {
		return lexeme{err: nil, end: end, kind: KindWord}
	}
	return lexeme{err: nil, end: s.punctuationEnd(start), kind: KindPunctuation}
}

// noiseAt reads whitespace or an ordinary comment starting at start.
//
// Takes start (int) which is the candidate token's first byte.
//
// Returns lexeme which is the noise token, or the unterminated-comment error.
// Returns bool which is true when start opens whitespace or an ordinary comment.
func (s *Scanner) noiseAt(start int) (lexeme, bool) {
	if start == 0 && strings.HasPrefix(s.input, ByteOrderMark) {
		return lexeme{err: nil, end: len(ByteOrderMark), kind: KindWhitespace}, true
	}
	if isWhitespace(s.input[start]) {
		end := start + 1
		for end < len(s.input) && isWhitespace(s.input[end]) {
			end++
		}
		return lexeme{err: nil, end: end, kind: KindWhitespace}, true
	}
	if s.opensLineComment(start) {
		return lexeme{err: nil, end: lineEnd(s.input, start), kind: KindComment}, true
	}
	if strings.HasPrefix(s.input[start:], "/*") && !s.opensExecutableComment(start) {
		end, closed := blockCommentEnd(s.input, start, s.rules.Comments.NestedBlockComments)
		if !closed {
			return lexeme{err: ErrUnterminatedComment, end: end, kind: KindComment}, true
		}
		return lexeme{err: nil, end: end, kind: KindComment}, true
	}
	return lexeme{}, false
}

// opensExecutableComment reports whether an executable comment starts at start under the
// rules.
//
// Takes start (int) which is the candidate comment's first byte.
//
// Returns bool which is true when the rules enable executable comments and the input
// opens one with /*! or /*M!.
func (s *Scanner) opensExecutableComment(start int) bool {
	if !s.rules.Comments.ExecutableComments {
		return false
	}
	rest := s.input[start:]
	return strings.HasPrefix(rest, "/*!") || strings.HasPrefix(rest, "/*M!")
}

// executableCommentAt reads an executable comment starting at start.
//
// Takes start (int) which is the candidate token's first byte.
//
// Returns lexeme which is the executable comment, or the unterminated-comment error.
// Returns bool which is true when start opens an executable comment.
func (s *Scanner) executableCommentAt(start int) (lexeme, bool) {
	if !s.opensExecutableComment(start) {
		return lexeme{}, false
	}
	end, closed := blockCommentEnd(s.input, start, s.rules.Comments.NestedBlockComments)
	if !closed {
		return lexeme{err: ErrUnterminatedComment, end: end, kind: KindExecutableComment}, true
	}
	return lexeme{err: nil, end: end, kind: KindExecutableComment}, true
}

// opensLineComment reports whether a line comment starts at start under the rules.
//
// Takes start (int) which is the candidate comment's first byte.
//
// Returns bool which is true for -- (with whitespace after it when the rules ask) or,
// when the rules allow it, #.
func (s *Scanner) opensLineComment(start int) bool {
	if s.input[start] == '#' {
		return s.rules.Comments.HashLineComments
	}
	if !strings.HasPrefix(s.input[start:], "--") {
		return false
	}
	if !s.rules.Comments.DoubleDashRequiresWhitespace {
		return true
	}
	after := start + len("--")
	return after >= len(s.input) || s.input[after] <= ' '
}

// lexeme is what one scan step found: the token's kind and end, or the sentinel of the
// lexical error that stopped it.
type lexeme struct {
	// err is the sentinel of a lexical error, or nil.
	err error

	// end is the byte offset just past the token.
	end int

	// kind classifies the token.
	kind Kind
}

// Tokens scans all of input and returns every token, including whitespace and comments.
//
// Takes rules (Rules) which describes the engine's lexical rules.
// Takes input (string) which is the SQL text.
//
// Returns []Token which holds the tokens in order; together they cover input exactly.
// Returns error which is an *Error when a construct is left unterminated.
func Tokens(rules Rules, input string) ([]Token, error) {
	scanner := NewScanner(rules, input)
	var tokens []Token
	for {
		token, ok := scanner.Next()
		if !ok {
			break
		}
		tokens = append(tokens, token)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return tokens, nil
}

// TrimByteOrderMark returns text without a leading UTF-8 byte order mark.
//
// Takes text (string) which may start with ByteOrderMark.
//
// Returns string which is text without it.
func TrimByteOrderMark(text string) string {
	return strings.TrimPrefix(text, ByteOrderMark)
}

// lineEnd returns the offset of the line break that ends the line comment starting at
// start, or the length of input when the comment runs to the end.
//
// Takes input (string) which is the text.
// Takes start (int) which is the comment's first byte.
//
// Returns int which is the offset of the newline or carriage return.
func lineEnd(input string, start int) int {
	if index := strings.IndexAny(input[start:], "\r\n"); index >= 0 {
		return start + index
	}
	return len(input)
}

// blockCommentEnd returns the offset just past the block comment starting at start.
//
// Takes input (string) which is the text.
// Takes start (int) which is the offset of the opening /*.
// Takes nested (bool) which makes inner /* need their own */.
//
// Returns int which is the offset after the closing */, or the length of input.
// Returns bool which is true when the comment was closed.
func blockCommentEnd(input string, start int, nested bool) (int, bool) {
	position := start + len("/*")
	depth := 1
	for position+1 < len(input) {
		switch {
		case input[position] == '*' && input[position+1] == '/':
			depth--
			position += len("*/")
			if depth == 0 {
				return position, true
			}
		case nested && input[position] == '/' && input[position+1] == '*':
			depth++
			position += len("/*")
		default:
			position++
		}
	}
	return len(input), false
}

// isWhitespace reports whether character separates tokens: space, tab, newline, carriage
// return, vertical tab or form feed.
//
// Takes character (byte) which is the byte to classify.
//
// Returns bool which is true for whitespace.
func isWhitespace(character byte) bool {
	return character == ' ' || character == '\t' || character == '\n' || character == '\r' ||
		character == '\v' || character == '\f'
}

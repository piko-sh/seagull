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

// Kind classifies a token.
type Kind uint8

const (
	// KindWhitespace is a run of whitespace, or the byte order mark at the start of the
	// input.
	KindWhitespace Kind = iota

	// KindComment is a line or block comment, including its delimiters. A line comment stops
	// before the line break that ends it.
	KindComment

	// KindWord is an unquoted identifier or keyword.
	KindWord

	// KindQuotedIdentifier is a delimited identifier, including its quotes.
	KindQuotedIdentifier

	// KindString is a string literal, including its quotes, prefix and dollar tags.
	KindString

	// KindNumber is a numeric literal.
	KindNumber

	// KindParameter is a bind-parameter placeholder, including its sigil or braces.
	KindParameter

	// KindPunctuation is any other character, such as an operator, a parenthesis or the
	// terminator. Each punctuation token is one character, except that a doubled named
	// parameter sigil, such as ::, is one token.
	KindPunctuation

	// KindExecutableComment is a block comment the server executes, such as MySQL's /*!40101
	// SET NAMES utf8 */, including its delimiters. It is significant, not noise.
	KindExecutableComment
)

// String returns the kind's name.
//
// Returns string which is the name, such as "word".
func (kind Kind) String() string {
	switch kind {
	case KindWhitespace:
		return "whitespace"
	case KindComment:
		return "comment"
	case KindWord:
		return "word"
	case KindQuotedIdentifier:
		return "quoted identifier"
	case KindString:
		return "string"
	case KindNumber:
		return "number"
	case KindParameter:
		return "parameter"
	case KindPunctuation:
		return "punctuation"
	case KindExecutableComment:
		return "executable comment"
	default:
		return "unknown"
	}
}

// IsNoise reports whether the kind carries no meaning for the statement: whitespace or an
// ordinary comment. Executable comments are significant.
//
// Returns bool which is true for KindWhitespace and KindComment.
func (kind Kind) IsNoise() bool {
	return kind == KindWhitespace || kind == KindComment
}

// Token is one lexical token and where it sits in the input.
type Token struct {
	// Raw is the token's source text, input[Start:End].
	Raw string

	// Start is the byte offset of the token's first byte.
	Start int

	// End is the byte offset just past the token's last byte.
	End int

	// Kind classifies the token.
	Kind Kind
}

// IsWord reports whether the token is the unquoted word, compared without regard to ASCII
// case. A quoted identifier never matches, so "select" is not the keyword SELECT.
//
// Takes word (string) which is the word to compare against.
//
// Returns bool which is true when the token is that word.
func (token Token) IsWord(word string) bool {
	return token.Kind == KindWord && equalFoldASCII(token.Raw, word)
}

// equalFoldASCII reports whether two strings are equal when ASCII letters are compared
// without regard to case. Other bytes must match exactly.
//
// Takes left (string) which is the first string.
// Takes right (string) which is the second string.
//
// Returns bool which is true when the strings match.
func equalFoldASCII(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range len(left) {
		if upperASCII(left[index]) != upperASCII(right[index]) {
			return false
		}
	}
	return true
}

// upperASCII upper-cases an ASCII letter and leaves every other byte unchanged.
//
// Takes character (byte) which is the byte to fold.
//
// Returns byte which is the folded byte.
func upperASCII(character byte) byte {
	if character >= 'a' && character <= 'z' {
		return character - ('a' - 'A')
	}
	return character
}

// upperWord returns word with its ASCII letters upper-cased.
//
// Takes word (string) which is the word to fold.
//
// Returns string which is the folded word, sharing word's memory when no letter changes.
func upperWord(word string) string {
	for index := range len(word) {
		if word[index] >= 'a' && word[index] <= 'z' {
			folded := []byte(word)
			for position := index; position < len(folded); position++ {
				folded[position] = upperASCII(folded[position])
			}
			return string(folded)
		}
	}
	return word
}

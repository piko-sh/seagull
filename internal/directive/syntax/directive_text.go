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

package syntax

// IsIdentifierStart reports whether a byte can start a directive identifier: an ASCII
// letter or an underscore.
//
// Takes character (byte) which is the byte to classify.
//
// Returns bool which is true when the byte can start an identifier.
func IsIdentifierStart(character byte) bool {
	return (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || character == '_'
}

// IsIdentifierPart reports whether a byte can continue a directive identifier: an ASCII
// letter, digit or underscore.
//
// Takes character (byte) which is the byte to classify.
//
// Returns bool which is true when the byte can continue an identifier.
func IsIdentifierPart(character byte) bool {
	return IsIdentifierStart(character) || IsDigit(character)
}

// IsDigit reports whether a byte is an ASCII digit.
//
// Takes character (byte) which is the byte to classify.
//
// Returns bool which is true for 0 to 9.
func IsDigit(character byte) bool {
	return character >= '0' && character <= '9'
}

// SkipString returns the offset just past the directive string that opens at start with a
// ' or " quote, and whether its closing quote was found. A backslash escapes the byte
// after it, and a doubled quote stands for one quote.
//
// Takes text (string) which is the directive text.
// Takes start (int) which is the offset of the opening quote.
//
// Returns int which is the offset just past the closing quote, or the length of the text
// when the string is unterminated.
// Returns bool which is true when the closing quote was found.
func SkipString(text string, start int) (int, bool) {
	quote := text[start]
	index := start + 1
	for index < len(text) {
		switch {
		case text[index] == '\\' && index+1 < len(text):
			index += 2
		case text[index] != quote:
			index++
		case index+1 < len(text) && text[index+1] == quote:
			index += 2
		default:
			return index + 1, true
		}
	}
	return index, false
}

// LineDepth walks one line of directive text, computing both the net paren and bracket
// depth change and the offset of an unterminated opening quote when present.
//
// Strings are skipped during the depth count, so quoted parens do not affect it. If a
// string opener is found whose closing quote does not appear before the end of the line,
// unterminated is the offset of the opener and quote is the opening character; the depth
// returned reflects only the brackets seen before the unterminated string, since the rest
// of the line is untrustworthy until the user closes the quote.
//
// Takes input (string) which is the line to scan.
//
// Returns depth (int) which is the net paren and bracket depth change.
// Returns unterminated (int) which is the offset of an unterminated opener, or -1.
// Returns quote (byte) which is the opening quote of the unterminated string.
func LineDepth(input string) (depth, unterminated int, quote byte) {
	index := 0
	for index < len(input) {
		character := input[index]
		switch character {
		case '\'', '"':
			next, closed := SkipString(input, index)
			if !closed {
				return depth, index, character
			}
			index = next
		case '(', '[':
			depth++
			index++
		case ')', ']':
			depth--
			index++
		default:
			index++
		}
	}
	return depth, -1, 0
}

// leadingIdentifier returns the identifier at the start of text, or "" when text does not
// begin with one.
//
// Takes text (string) which is the text to read from.
//
// Returns string which is the leading identifier.
func leadingIdentifier(text string) string {
	if text == "" || !IsIdentifierStart(text[0]) {
		return ""
	}
	end := 1
	for end < len(text) && IsIdentifierPart(text[end]) {
		end++
	}
	return text[:end]
}

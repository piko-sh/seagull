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

package fuzztext

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// lastASCII is the last code point written unescaped in JSON output.
	lastASCII = '~'

	// firstPrintable is the first code point written unescaped in JSON output.
	firstPrintable = ' '

	// surrogateBase is the first code point that needs a surrogate pair in a \u escape.
	surrogateBase = 0x10000

	// highSurrogate and lowSurrogate start the two halves of a surrogate pair.
	highSurrogate = 0xd800

	// lowSurrogate starts the second half of a surrogate pair.
	lowSurrogate = 0xdc00

	// surrogateBits is the number of code point bits each half carries.
	surrogateBits = 10

	// firstSeparator and lastSeparator bound the information separators.
	firstSeparator = 0x1c

	// lastSeparator is the last information separator.
	lastSeparator = 0x1f
)

// lineBoundaries are the runes that end a line for SplitLines.
var lineBoundaries = []rune{'\n', '\r', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029}

// IsSpace reports whether a rune is whitespace for stripping and splitting.
//
// It adds the four information separators to Unicode white space, as the
// earlier tooling did, so seeds and corpora split the same way.
//
// Takes character (rune) which is the rune to classify.
//
// Returns bool which is true for whitespace.
func IsSpace(character rune) bool {
	if character >= firstSeparator && character <= lastSeparator {
		return true
	}
	return unicode.IsSpace(character)
}

// Strip removes leading and trailing whitespace as IsSpace defines it.
//
// Takes text (string) which is the text to strip.
//
// Returns string which is the stripped text.
func Strip(text string) string {
	return strings.TrimFunc(text, IsSpace)
}

// TrimLeft removes leading whitespace as IsSpace defines it.
//
// Takes text (string) which is the text to trim.
//
// Returns string which is the trimmed text.
func TrimLeft(text string) string {
	return strings.TrimLeftFunc(text, IsSpace)
}

// Fields splits text around runs of whitespace as IsSpace defines it.
//
// Takes text (string) which is the text to split.
//
// Returns []string which holds the non-empty fields.
func Fields(text string) []string {
	return strings.FieldsFunc(text, IsSpace)
}

// SplitLines splits text at every line boundary, dropping the boundaries.
//
// A boundary is \n, \r, \r\n, \v, \f, an information separator, NEL or a line
// or paragraph separator. A trailing boundary adds no empty line.
//
// Takes text (string) which is the text to split.
//
// Returns []string which holds the lines.
func SplitLines(text string) []string {
	var lines []string
	start := 0
	for index := 0; index < len(text); {
		character, width := utf8.DecodeRuneInString(text[index:])
		if !slices.Contains(lineBoundaries, character) {
			index += width
			continue
		}
		lines = append(lines, text[start:index])
		index += width
		if character == '\r' && index < len(text) && text[index] == '\n' {
			index++
		}
		start = index
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}

// MarshalASCII writes a value as JSON whose bytes are all printable ASCII.
//
// Every other character is a \u escape, and <, > and & stay literal.
//
// Takes value (any) which is the value to write.
// Takes indent (bool) which selects two-space indentation over compact output.
//
// Returns []byte which holds the JSON without a trailing newline.
// Returns error when the value cannot be written as JSON.
func MarshalASCII(value any, indent bool) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if indent {
		encoder.SetIndent("", "  ")
	}
	if err := encoder.Encode(value); err != nil {
		return nil, fmt.Errorf("writing JSON: %w", err)
	}
	return escapeNonASCII(bytes.TrimSuffix(buffer.Bytes(), []byte("\n"))), nil
}

// escapeNonASCII rewrites every rune outside printable ASCII as a \u escape.
//
// Encoded JSON holds such runes only inside strings, where an escape means the same.
//
// Takes encoded ([]byte) which is valid UTF-8 JSON.
//
// Returns []byte which holds the escaped JSON.
func escapeNonASCII(encoded []byte) []byte {
	var builder strings.Builder
	builder.Grow(len(encoded))
	for _, character := range string(encoded) {
		if character >= firstPrintable && character <= lastASCII || character == '\n' {
			builder.WriteString(string(character))
			continue
		}
		if character < surrogateBase {
			fmt.Fprintf(&builder, `\u%04x`, character)
			continue
		}
		offset := character - surrogateBase
		fmt.Fprintf(&builder, `\u%04x\u%04x`, highSurrogate+(offset>>surrogateBits), lowSurrogate+(offset&(1<<surrogateBits-1)))
	}
	return []byte(builder.String())
}

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

package corpus

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"seagull.sh/seagull/cmd/internal/fuzztool/fuzztext"
)

const (
	// MaxInput bounds the summed length of a corpus file's arguments.
	MaxInput = 64 << 10

	// MaxEnvelope bounds the size of a whole corpus file.
	MaxEnvelope = MaxInput*4 + 1024

	// KindString is the corpus argument type for a Go string.
	KindString = "string"

	// KindBytes is the corpus argument type for a Go byte slice.
	KindBytes = "[]byte"

	// header is the first line of every supported corpus file.
	header = "go test fuzz v1"

	// maxArguments is the most arguments a registered target takes.
	maxArguments = 3

	// hexadecimalBase and octalBase are the bases of numeric escapes.
	hexadecimalBase = 16

	// octalBase is the base of a three-digit octal escape.
	octalBase = 8

	// octalWidth is the number of digits in an octal escape.
	octalWidth = 3

	// maxRune is the last Unicode code point.
	maxRune = 0x10ffff

	// firstSurrogate and lastSurrogate bound the UTF-16 surrogate range.
	firstSurrogate = 0xd800

	// lastSurrogate is the last UTF-16 surrogate code point.
	lastSurrogate = 0xdfff

	// lastPrintable is the last byte Quote writes unescaped.
	lastPrintable = 126

	// maxByte is the largest value an escape may give a single byte.
	maxByte = 255
)

var (
	// argumentLine matches one typed argument line of a corpus file.
	argumentLine = regexp.MustCompile(`^(string|\[\]byte)\((.*)\)$`)

	// singleEscapes maps each one-character escape to its byte.
	singleEscapes = map[byte]byte{
		'a': '\a', 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t', 'v': '\v', '\\': '\\', '"': '"',
	}

	// unicodeWidths maps each hexadecimal escape letter to its digit count.
	unicodeWidths = map[byte]int{'x': 2, 'u': 4, 'U': 8}

	// errUnsupported marks every refusal of a corpus file.
	errUnsupported = errors.New("unsupported corpus")
)

// Argument is one typed value of a corpus file.
type Argument struct {
	// Kind is KindString or KindBytes.
	Kind string

	// Value holds the argument's exact bytes.
	Value []byte
}

// Decode reads a corpus file whose arguments are strings and byte slices.
//
// Takes content ([]byte) which is the whole file.
//
// Returns []Argument which holds between one and three arguments.
// Returns error when the file is oversized, malformed or of another version.
func Decode(content []byte) ([]Argument, error) {
	if len(content) > MaxEnvelope {
		return nil, fmt.Errorf("%w: the envelope exceeds its bound", errUnsupported)
	}
	if !utf8.Valid(content) {
		return nil, fmt.Errorf("%w: the file is not UTF-8", errUnsupported)
	}
	lines := fuzztext.SplitLines(string(content))
	if len(lines) == 0 || lines[0] != header {
		return nil, fmt.Errorf("%w: unsupported Go corpus version", errUnsupported)
	}
	var arguments []Argument
	total := 0
	for _, line := range lines[1:] {
		if fuzztext.Strip(line) == "" {
			continue
		}
		match := argumentLine.FindStringSubmatch(line)
		if match == nil {
			return nil, fmt.Errorf("%w: unsupported corpus argument", errUnsupported)
		}
		value, err := Unquote(match[2])
		if err != nil {
			return nil, err
		}
		arguments = append(arguments, Argument{Kind: match[1], Value: value})
		total += len(value)
	}
	if len(arguments) < 1 || len(arguments) > maxArguments || total > MaxInput {
		return nil, fmt.Errorf("%w: corpus arguments exceed their bounds", errUnsupported)
	}
	return arguments, nil
}

// Encode writes a corpus file that keeps every argument's type and exact bytes.
//
// Takes arguments ([]Argument) which are the values to write.
//
// Returns []byte which holds the file.
func Encode(arguments []Argument) []byte {
	var builder strings.Builder
	builder.WriteString(header + "\n")
	for _, argument := range arguments {
		builder.WriteString(argument.Kind + "(" + Quote(argument.Value) + ")\n")
	}
	return []byte(builder.String())
}

// Quote writes bytes as a Go interpreted string, escaping every byte outside
// printable ASCII, so invalid UTF-8 survives exactly.
//
// Takes data ([]byte) which is the value to quote.
//
// Returns string which is the quoted literal.
func Quote(data []byte) string {
	var builder strings.Builder
	builder.Grow(len(data) + 2)
	builder.WriteByte('"')
	for _, value := range data {
		if value >= ' ' && value <= lastPrintable && value != '"' && value != '\\' {
			builder.WriteByte(value)
			continue
		}
		fmt.Fprintf(&builder, `\x%02x`, value)
	}
	builder.WriteByte('"')
	return builder.String()
}

// Unquote decodes a Go interpreted string literal without evaluating anything else.
//
// Takes text (string) which is the quoted literal.
//
// Returns []byte which holds the decoded bytes.
// Returns error when the literal is malformed.
func Unquote(text string) ([]byte, error) {
	if len(text) < 2 || text[0] != '"' || text[len(text)-1] != '"' {
		return nil, fmt.Errorf("%w: expected a Go interpreted string", errUnsupported)
	}
	body := text[1 : len(text)-1]
	var result []byte
	for index := 0; index < len(body); {
		if body[index] != '\\' {
			character, width := utf8.DecodeRuneInString(body[index:])
			if character == '"' || character < ' ' {
				return nil, fmt.Errorf("%w: unescaped character in Go string", errUnsupported)
			}
			result = append(result, body[index:index+width]...)
			index += width
			continue
		}
		decoded, consumed, err := unescape(body[index+1:])
		if err != nil {
			return nil, err
		}
		result = append(result, decoded...)
		index += 1 + consumed
	}
	return result, nil
}

// unescape decodes the escape that follows a backslash.
//
// Takes rest (string) which is the literal after the backslash.
//
// Returns []byte which holds the escaped bytes.
// Returns int which is the number of bytes of rest consumed.
// Returns error when the escape is malformed.
func unescape(rest string) ([]byte, int, error) {
	if rest == "" {
		return nil, 0, fmt.Errorf("%w: incomplete Go escape", errUnsupported)
	}
	escape := rest[0]
	if value, found := singleEscapes[escape]; found {
		return []byte{value}, 1, nil
	}
	if width, found := unicodeWidths[escape]; found {
		return unescapeHexadecimal(escape, rest[1:min(len(rest), 1+width)], width)
	}
	digits := rest[:min(len(rest), octalWidth)]
	value, err := strconv.ParseUint(digits, octalBase, 16)
	if len(digits) != octalWidth || err != nil || value > maxByte || !isOctal(digits) {
		return nil, 0, fmt.Errorf("%w: invalid octal Go escape", errUnsupported)
	}
	return []byte{byte(value)}, octalWidth, nil
}

// unescapeHexadecimal decodes a \x, \u or \U escape.
//
// Takes escape (byte) which is the escape letter.
// Takes digits (string) which holds up to width hexadecimal digits.
// Takes width (int) which is the number of digits the escape needs.
//
// Returns []byte which holds the escaped bytes.
// Returns int which is the number of bytes consumed after the backslash.
// Returns error when the escape is malformed or names no Unicode scalar value.
func unescapeHexadecimal(escape byte, digits string, width int) ([]byte, int, error) {
	value, err := strconv.ParseUint(digits, hexadecimalBase, 32)
	if len(digits) != width || err != nil || !isHexadecimal(digits) {
		return nil, 0, fmt.Errorf("%w: invalid hexadecimal Go escape", errUnsupported)
	}
	if escape == 'x' && value <= maxByte {
		return []byte{byte(value)}, 1 + width, nil
	}
	if value > maxRune || value >= firstSurrogate && value <= lastSurrogate {
		return nil, 0, fmt.Errorf("%w: invalid Unicode Go escape", errUnsupported)
	}
	return utf8.AppendRune(nil, rune(value)), 1 + width, nil
}

// isHexadecimal reports whether every byte is a hexadecimal digit.
//
// Takes digits (string) which is the text to check.
//
// Returns bool which is true when every byte is a hexadecimal digit.
func isHexadecimal(digits string) bool {
	for index := range len(digits) {
		character := digits[index]
		isDigit := character >= '0' && character <= '9'
		isLower := character >= 'a' && character <= 'f'
		isUpper := character >= 'A' && character <= 'F'
		if !isDigit && !isLower && !isUpper {
			return false
		}
	}
	return true
}

// isOctal reports whether every byte is an octal digit.
//
// Takes digits (string) which is the text to check.
//
// Returns bool which is true when every byte is an octal digit.
func isOctal(digits string) bool {
	for index := range len(digits) {
		if digits[index] < '0' || digits[index] > '7' {
			return false
		}
	}
	return true
}

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

package goast

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// rawStringQuote delimits a Go raw string literal.
	rawStringQuote = "`"

	// rawStringBreakers holds the characters a Go raw string literal cannot carry unchanged:
	// the backtick ends it, a carriage return is discarded from it, and a NUL or byte order
	// mark makes the source file invalid.
	rawStringBreakers = "`\r\x00\uFEFF"
)

// RawStringAllowed reports whether text survives being written between backticks.
//
// A Go raw string literal cannot hold a backtick, loses every carriage return, and may
// not contain a NUL, a byte order mark or bytes that are not valid UTF-8, so text with
// any of those must be written as an interpreted literal instead.
//
// Takes text (string) which is the value to embed.
//
// Returns bool which is true when a raw string literal holds text byte for byte.
func RawStringAllowed(text string) bool {
	return utf8.ValidString(text) && !strings.ContainsAny(text, rawStringBreakers)
}

// StringLiteral returns the Go string literal for text, preferring a raw string.
//
// SQL and other text taken from user input reads best as a raw string, so the literal is
// raw whenever RawStringAllowed holds and an escaped, interpreted literal otherwise.
// Either way the compiled constant equals text byte for byte.
//
// Takes text (string) which is the value to embed.
//
// Returns *ast.BasicLit which is the string literal node.
func StringLiteral(text string) *ast.BasicLit {
	if RawStringAllowed(text) {
		return &ast.BasicLit{Kind: token.STRING, Value: rawStringQuote + text + rawStringQuote}
	}
	return &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(text)}
}

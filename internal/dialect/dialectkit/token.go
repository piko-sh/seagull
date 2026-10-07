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

package dialectkit

import (
	"strings"
)

// Token is one lexical token of a statement.
//
// Kind is the dialect's own classification, so each dialect keeps its own set of kinds
// and each kit helper uses only the kinds its caller supplies. Value is the decoded text
// a parser reads, such as an identifier without its delimiters or a string literal with
// its escapes resolved, while Raw is the source text exactly as written. Start and End
// are byte offsets into the text the dialect tokenised, so a parser can slice the source
// between any two tokens rather than rebuild it.
type Token[K comparable] struct {
	// Kind is the dialect's classification of the token.
	Kind K

	// Value is the decoded text of the token: an identifier without its delimiters, a
	// literal without its quotes and escapes, or an operator's symbol.
	Value string

	// Raw is the source text of the token exactly as written, delimiters included.
	Raw string

	// Start is the byte offset of the token's first byte.
	Start int

	// End is the byte offset one past the token's last byte.
	End int

	// Quoted is true for a delimited identifier, such as "order" or `order`. A quoted
	// identifier is always a name and never a keyword.
	Quoted bool
}

// IsKeyword reports whether the token is the given keyword: a bare word of the word kind,
// not delimited, whose text matches keyword ignoring case.
//
// Takes word (K) which is the dialect's kind for words (identifiers and keywords).
// Takes keyword (string) which is the keyword to match.
//
// Returns bool which is true when the token is that keyword.
func (t Token[K]) IsKeyword(word K, keyword string) bool {
	return t.Kind == word && !t.Quoted && strings.EqualFold(t.Value, keyword)
}

// Keyword returns the token's text upper-cased when it is a bare word that could be a
// keyword, and the empty string otherwise, so a lookup table of keywords never matches a
// delimited identifier or a literal.
//
// Takes word (K) which is the dialect's kind for words.
//
// Returns string which is the upper-cased keyword candidate, or "".
func (t Token[K]) Keyword(word K) string {
	if t.Kind != word || t.Quoted {
		return ""
	}
	return strings.ToUpper(t.Value)
}

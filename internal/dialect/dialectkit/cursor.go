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

// Cursor walks a token stream for a hand-written parser.
//
// It knows only two of the dialect's kinds: WordKind, the kind keywords are matched
// against, and EndKind, the kind of the end-of-input token. Keyword matching ignores case
// and never matches a delimited identifier, so a column named "order" stays a column. The
// fields are exported so a parser that embeds a Cursor can save and restore Position to
// backtrack, or index Tokens directly.
type Cursor[K comparable] struct {
	// WordKind is the kind of bare words, which keyword matching applies to.
	WordKind K

	// EndKind is the kind of the end-of-input token.
	EndKind K

	// Interrupt is called on every read of the stream and says whether the walk must stop.
	//
	// An error stops the read with an interrupt panic, which Recover and RecoveredError
	// convert back to the original error. Child cursors inherit Interrupt so all cursors
	// over a statement share one budget, as WalkGuard describes. Nil disables interruption.
	Interrupt func() error

	// Tokens is the token stream being walked. It may end in an EndKind token; positions
	// past the last token read as end of input either way.
	Tokens []Token[K]

	// Position is the index of the current token.
	Position int
}

// NewCursor returns a Cursor at the start of tokens.
//
// Takes tokens ([]Token[K]) which is the stream to walk.
// Takes wordKind (K) which is the kind keyword matching applies to.
// Takes endKind (K) which is the kind of the end-of-input token.
//
// Returns Cursor[K] which is positioned on the first token.
func NewCursor[K comparable](tokens []Token[K], wordKind, endKind K) Cursor[K] {
	return Cursor[K]{WordKind: wordKind, EndKind: endKind, Tokens: tokens, Interrupt: nil, Position: 0}
}

// Current returns the token at the cursor without consuming it.
//
// Returns Token[K] which is the current token, or an end-of-input token placed after the
// last token when the cursor has run past the stream.
func (c *Cursor[K]) Current() Token[K] {
	return c.PeekAt(0)
}

// Peek returns the token after the current one without consuming anything.
//
// Returns Token[K] which is the next token, or an end-of-input token.
func (c *Cursor[K]) Peek() Token[K] {
	return c.PeekAt(1)
}

// PeekAt returns the token offset places after the current one without consuming
// anything.
//
// Takes offset (int) which is the distance from the current token; 0 is the current token
// and a negative offset looks back.
//
// Returns Token[K] which is the token at that place, or an end-of-input token when it
// lies outside the stream.
//
// Panics with an interrupt when the cursor's Interrupt returns an error, which Recover
// and RecoveredError turn back into that error.
func (c *Cursor[K]) PeekAt(offset int) Token[K] {
	if c.Interrupt != nil {
		if err := c.Interrupt(); err != nil {
			panic(walkInterrupted{err: err})
		}
	}
	index := c.Position + offset
	if index < 0 || index >= len(c.Tokens) {
		return c.endToken()
	}
	return c.Tokens[index]
}

// Advance consumes and returns the current token, including an explicit EndKind token.
// Past the token slice it returns a synthetic end-of-input token and stays put.
//
// Returns Token[K] which is the token that was current.
func (c *Cursor[K]) Advance() Token[K] {
	current := c.Current()
	if c.Position < len(c.Tokens) {
		c.Position++
	}
	return current
}

// AtEnd reports whether the cursor has reached the end of input.
//
// Returns bool which is true past the last token or on an EndKind token.
func (c *Cursor[K]) AtEnd() bool {
	return c.Position >= len(c.Tokens) || c.Tokens[c.Position].Kind == c.EndKind
}

// IsKeyword reports whether the current token is one of keywords, without consuming it.
//
// Takes keywords (...string) which are the keywords to test, matched ignoring case.
//
// Returns bool which is true when the current token is a bare word equal to one of them.
func (c *Cursor[K]) IsKeyword(keywords ...string) bool {
	current := c.Current()
	for _, keyword := range keywords {
		if current.IsKeyword(c.WordKind, keyword) {
			return true
		}
	}
	return false
}

// MatchKeyword consumes the current token when it is keyword.
//
// Takes keyword (string) which is the keyword to match, ignoring case.
//
// Returns bool which is true when the keyword matched and was consumed.
func (c *Cursor[K]) MatchKeyword(keyword string) bool {
	if !c.IsKeyword(keyword) {
		return false
	}
	c.Position++
	return true
}

// ExpectKeyword consumes the current token when it is one of keywords and reports a
// ParseError naming the token otherwise.
//
// Takes keywords (...string) which are the acceptable keywords.
//
// Returns Token[K] which is the consumed token.
// Returns error which is a *ParseError when the current token is none of the keywords.
func (c *Cursor[K]) ExpectKeyword(keywords ...string) (Token[K], error) {
	if c.IsKeyword(keywords...) {
		return c.Advance(), nil
	}
	return Token[K]{}, NewParseError(c.Current(), "expected "+describeKeywords(keywords))
}

// RequireEnd reports a ParseError when tokens remain, so an analyser that has parsed a
// complete statement rejects anything after it rather than silently dropping it.
//
// Returns error which is a *ParseError naming the first unparsed token, or nil at end of
// input.
func (c *Cursor[K]) RequireEnd() error {
	if c.AtEnd() {
		return nil
	}
	return NewParseError(c.Current(), "expected the end of the statement")
}

// endToken returns the end-of-input token, placed after the last token of the stream.
//
// Returns Token[K] which has EndKind and an empty text.
func (c *Cursor[K]) endToken() Token[K] {
	offset := 0
	if count := len(c.Tokens); count > 0 {
		offset = c.Tokens[count-1].End
	}
	return Token[K]{Kind: c.EndKind, Value: "", Raw: "", Start: offset, End: offset, Quoted: false}
}

// describeKeywords renders a keyword list for a message: "keyword SELECT" for one and
// "one of SELECT, WITH" for several.
//
// Takes keywords ([]string) which are the expected keywords.
//
// Returns string which names them.
func describeKeywords(keywords []string) string {
	upper := make([]string, len(keywords))
	for index, keyword := range keywords {
		upper[index] = strings.ToUpper(keyword)
	}
	if len(upper) == 1 {
		return "keyword " + upper[0]
	}
	return "one of " + strings.Join(upper, ", ")
}

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

import (
	"errors"
	"slices"
	"strings"
	"unicode"

	"seagull.sh/seagull/internal/sqllex"
)

const (
	// lineSpace lists the characters that space a directive comment's text on its line.
	lineSpace = " \t"

	// neutralLinePrefix opens a directive comment when no dialect is at hand.
	neutralLinePrefix = "--"

	// byteOrderMark is the byte order mark as a character, which a line may open with before
	// its first token.
	byteOrderMark = '\uFEFF'

	// maxLexicalRecoveries is the most unterminated strings, quoted identifiers and comments
	// ScanTokens recovers from. Each recovery reads the rest of the text again, so the cap
	// keeps a text holding many of them linear to read.
	maxLexicalRecoveries = 64
)

// Lexicon is how a dialect writes the SQL that directives sit in: its lexical rules, and
// the prefix that opens the line comments directives are written in.
type Lexicon struct {
	// LinePrefix opens a directive comment, such as "--".
	LinePrefix string

	// Rules are the dialect's lexical rules.
	Rules sqllex.Rules
}

// DirectiveComments reads a text under the lexicon and returns its directive comments in
// order, with the index of its lines.
//
// Takes text (string) which is the SQL text.
//
// Returns []DirectiveComment which holds every directive comment in the text.
// Returns LineIndex which indexes the text's lines.
func (lexicon Lexicon) DirectiveComments(text string) ([]DirectiveComment, LineIndex) {
	return lexicon.DirectiveCommentsFromTokens(text, Tokens(lexicon.Rules, text))
}

// DirectiveCommentsFromTokens reads comments from tokens already scanned under this
// lexicon.
//
// Takes text (string) which is the source SQL indexed by the tokens.
// Takes tokens ([]sqllex.Token) which holds tokens already scanned from the supplied SQL.
//
// Returns []DirectiveComment which holds the directive comments in source order.
// Returns LineIndex which maps source byte offsets to lines.
func (lexicon Lexicon) DirectiveCommentsFromTokens(text string, tokens []sqllex.Token) ([]DirectiveComment, LineIndex) {
	lines := NewLineIndex(text)
	var comments []DirectiveComment
	for _, token := range tokens {
		if comment, ok := ReadDirectiveComment(text, lines, token, lexicon.LinePrefix); ok {
			comments = append(comments, comment)
		}
	}
	return comments, lines
}

// TokenScan is what ScanTokens read from a text.
type TokenScan struct {
	// Tokens holds the tokens in order; together they cover the text.
	Tokens []sqllex.Token

	// TruncatedAt is the byte offset of the unterminated construct from which the text
	// became one punctuation token, because more than maxLexicalRecoveries came before it,
	// or -1 when the whole text was read.
	TruncatedAt int

	// Recovered reports whether an unterminated construct was read as punctuation to the end
	// of its line.
	Recovered bool
}

// Truncated reports whether the end of the text was left unread because it held too many
// unterminated constructs.
//
// Returns bool which is true when TruncatedAt marks where reading stopped.
func (scan TokenScan) Truncated() bool {
	return scan.TruncatedAt >= 0
}

// LineIndex maps the byte offsets of a text to its lines. A line ends at a newline; a
// carriage return before it belongs to the line it ends.
type LineIndex struct {
	// starts holds the offset of the first byte of each line, in order.
	starts []int

	// contentStarts holds, for each line, the offset of its first character that is not
	// blank as isLineBlank reads it, or of the newline that ends it (the end of the text on
	// the last line) when it holds none, so BeginsLine need not scan the line.
	contentStarts []int
}

// NewLineIndex indexes the lines of a text.
//
// Takes text (string) which is the text to index.
//
// Returns LineIndex which maps the text's offsets to lines.
func NewLineIndex(text string) LineIndex {
	starts := []int{0}
	contentStarts := []int{}
	content := -1
	for offset, character := range text {
		if character == '\n' {
			if content < 0 {
				content = offset
			}
			contentStarts = append(contentStarts, content)
			starts = append(starts, offset+1)
			content = -1
			continue
		}
		if content < 0 && !isLineBlank(character) {
			content = offset
		}
	}
	if content < 0 {
		content = len(text)
	}
	contentStarts = append(contentStarts, content)
	return LineIndex{starts: starts, contentStarts: contentStarts}
}

// Line returns the 1-based line an offset is on: the number of lines that start at or
// before it.
//
// Takes offset (int) which is a byte offset into the text.
//
// Returns int which is the line number.
func (index LineIndex) Line(offset int) int {
	line, _ := slices.BinarySearch(index.starts, offset+1)
	return line
}

// LineStart returns the offset of the first byte of the line an offset is on.
//
// Takes offset (int) which is a byte offset into the text.
//
// Returns int which is the offset where the line starts.
func (index LineIndex) LineStart(offset int) int {
	return index.starts[index.Line(offset)-1]
}

// Column returns the 1-based byte column of an offset on its line.
//
// Takes offset (int) which is a byte offset into the text.
//
// Returns int which is the column.
func (index LineIndex) Column(offset int) int {
	return offset - index.LineStart(offset) + 1
}

// BeginsLine reports whether only blank characters come before an offset on its line:
// white space as unicode.IsSpace reads it, which is what stripped SQL is trimmed of, and
// byte order marks, which a text may open with.
//
// A comment therefore begins its line whether or not the blank text before it is trimmed
// away.
//
// Takes text (string) which is the indexed text.
// Takes offset (int) which is a byte offset into the text.
//
// Returns bool which is true when the offset is the line's first non-blank byte.
func (index LineIndex) BeginsLine(text string, offset int) bool {
	line := index.Line(offset) - 1
	if line < len(index.contentStarts) {
		return offset <= index.contentStarts[line]
	}
	return strings.TrimLeftFunc(text[index.LineStart(offset):offset], isLineBlank) == ""
}

// endsLine reports whether only blank characters, as BeginsLine reads them, come after an
// offset on its line.
//
// Takes text (string) which is the indexed text.
// Takes offset (int) which is a byte offset into the text.
//
// Returns bool which is true when nothing else comes before the newline that ends the
// line, or before the end of the text on the last line.
func (index LineIndex) endsLine(text string, offset int) bool {
	end := len(text)
	if line := index.Line(offset); line < len(index.starts) {
		end = index.starts[line] - 1
	}
	return strings.TrimLeftFunc(text[offset:end], isLineBlank) == ""
}

// DirectiveComment is a line comment alone on its line that opens with the directive
// comment prefix, such as `-- sgl.query(GetUser, one)`.
type DirectiveComment struct {
	// Body is the comment's text after the prefix and one space or tab, when one follows the
	// prefix.
	Body string

	// Token is the comment token.
	Token sqllex.Token

	// BodyStart is the byte offset of Body in the text.
	BodyStart int
}

// NeutralLexicon recognises common SQL quoting and comment forms without a dialect.
//
// It protects text inside literals while reading directives from standard line comments.
//
// Returns Lexicon which reads text without an engine's rules.
func NeutralLexicon() Lexicon {
	var rules sqllex.Rules
	rules.Quotes.Backticks = true
	rules.Quotes.DollarQuoting = true
	return Lexicon{LinePrefix: neutralLinePrefix, Rules: rules}
}

// Tokens reads every token of a text under a dialect's lexical rules.
//
// A string, quoted identifier or comment left unterminated stops the dialect's scanner;
// the text from the start of that construct to the end of its line becomes one
// punctuation token, which a reader treats as SQL, and reading resumes on the next line,
// so the constructs after it are still found. The dialect reports the unterminated text
// itself when it parses the statement. Past maxLexicalRecoveries such constructs the rest
// of the text becomes one punctuation token, as ScanTokens describes.
//
// Takes rules (sqllex.Rules) which are the dialect's lexical rules.
// Takes text (string) which is the SQL text.
//
// Returns []sqllex.Token which holds the tokens in order; together they cover the text.
func Tokens(rules sqllex.Rules, text string) []sqllex.Token {
	return ScanTokens(rules, text).Tokens
}

// ScanTokens reads every token of a text under a dialect's lexical rules, recovering from
// unterminated constructs as Tokens does.
//
// Each recovery reads the rest of the text again, so only the first maxLexicalRecoveries
// unterminated constructs are recovered from: from the next one on, the rest of the text
// becomes one punctuation token and TruncatedAt says where it starts, so a reader can
// report that the text after it was not read.
//
// Takes rules (sqllex.Rules) which are the dialect's lexical rules.
// Takes text (string) which is the SQL text.
//
// Returns TokenScan which holds the tokens and how they were read.
func ScanTokens(rules sqllex.Rules, text string) TokenScan {
	scan := TokenScan{Tokens: nil, TruncatedAt: -1, Recovered: false}
	offset := 0
	for recoveries := 0; offset < len(text); recoveries++ {
		scanner := sqllex.NewScanner(rules, text[offset:])
		for {
			token, ok := scanner.Next()
			if !ok {
				break
			}
			token.Start += offset
			token.End += offset
			scan.Tokens = append(scan.Tokens, token)
		}
		lexicalError, failedScan := errors.AsType[*sqllex.Error](scanner.Err())
		if !failedScan {
			break
		}
		scan.Recovered = true
		failed := offset + lexicalError.Offset
		if recoveries == maxLexicalRecoveries {
			scan.TruncatedAt = failed
			scan.Tokens = append(scan.Tokens, sqllex.Token{Raw: text[failed:], Start: failed, End: len(text), Kind: sqllex.KindPunctuation})
			break
		}
		resume := len(text)
		if newline := strings.IndexByte(text[failed:], '\n'); newline >= 0 {
			resume = failed + newline + 1
		}
		scan.Tokens = append(scan.Tokens, sqllex.Token{Raw: text[failed:resume], Start: failed, End: resume, Kind: sqllex.KindPunctuation})
		offset = resume
	}
	return scan
}

// ReadDirectiveComment reads a token as a directive comment: a comment, not a block
// comment, that opens with the directive comment prefix and is the only token on its
// line, with nothing but blank characters before or after it.
//
// A carriage return ends a line comment. If SQL follows a lone carriage return on the
// same line, the comment is rejected as a directive, so removing a directive line cannot
// remove SQL.
//
// Takes text (string) which is the text the token was read from.
// Takes lines (LineIndex) which indexes the text's lines.
// Takes token (sqllex.Token) which is the candidate token.
// Takes linePrefix (string) which opens a directive comment, such as "--".
//
// Returns DirectiveComment which describes the comment.
// Returns bool which is false when the token is not a directive comment.
func ReadDirectiveComment(text string, lines LineIndex, token sqllex.Token, linePrefix string) (DirectiveComment, bool) {
	if token.Kind != sqllex.KindComment || linePrefix == "" || !strings.HasPrefix(token.Raw, linePrefix) ||
		!lines.BeginsLine(text, token.Start) || !lines.endsLine(text, token.End) {
		return DirectiveComment{}, false
	}
	bodyOffset := len(linePrefix)
	if bodyOffset < len(token.Raw) && strings.IndexByte(lineSpace, token.Raw[bodyOffset]) >= 0 {
		bodyOffset++
	}
	body := strings.TrimRight(token.Raw[bodyOffset:], "\r")
	return DirectiveComment{Body: body, Token: token, BodyStart: token.Start + bodyOffset}, true
}

// DirectiveName splits the directive name a comment's text opens with, after any spaces
// and tabs: a prefix such as sgl, optionally followed by a dot and a role such as query.
//
// Takes body (string) which is a directive comment's text.
//
// Returns prefix (string) which is the leading identifier, or empty when the text does
// not open with one.
// Returns role (string) which is the identifier after the dot, or empty when there is
// none.
// Returns rest (string) which is the text after the name.
func DirectiveName(body string) (prefix, role, rest string) {
	text := strings.TrimLeft(body, lineSpace)
	prefix = leadingIdentifier(text)
	rest = text[len(prefix):]
	if prefix == "" {
		return "", "", rest
	}
	afterDot, hasDot := strings.CutPrefix(rest, ".")
	if !hasDot {
		return prefix, "", rest
	}
	role = leadingIdentifier(afterDot)
	if role == "" {
		return prefix, "", rest
	}
	return prefix, role, afterDot[len(role):]
}

// OpensCall reports whether the text after a directive name opens its argument list: a
// parenthesis after any spaces and tabs.
//
// Takes rest (string) which is the text after the name.
//
// Returns bool which is true when the call's parenthesis follows.
func OpensCall(rest string) bool {
	return strings.HasPrefix(strings.TrimLeft(rest, lineSpace), "(")
}

// isLineBlank reports whether a character may come before a line's first token: white
// space, or a byte order mark.
//
// Takes character (rune) which is the character to classify.
//
// Returns bool which is true for a blank character.
func isLineBlank(character rune) bool {
	return unicode.IsSpace(character) || character == byteOrderMark
}

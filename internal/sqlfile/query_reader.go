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

package sqlfile

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqllex"
)

const (
	// sqlFileSuffix ends the name of every SQL file.
	sqlFileSuffix = ".sql"

	// queryRole is the directive role whose call opens a query block.
	queryRole = "query"
)

// QueryBlock holds a single query extracted from a multi-query file, together with its
// starting line offset for source mapping.
type QueryBlock struct {
	// SQL is the original text of the query block.
	SQL string

	// tokens holds immutable lexical tokens with block-relative offsets.
	tokens []sqllex.Token

	// Lexical holds the dialect rules used to scan the block.
	Lexical sqllex.Rules

	// StartLine is the one-based source line at which the block starts.
	StartLine int

	// recovered reports a lexical failure while scanning the source file.
	recovered bool
}

// Tokens returns the block tokens; callers must not modify the returned slice.
//
// Returns []sqllex.Token which is an immutable token view relative to the query block.
func (block QueryBlock) Tokens() []sqllex.Token {
	if block.tokens != nil {
		return block.tokens
	}
	return syntax.Tokens(block.Lexical, block.SQL)
}

// StrictTokens returns reusable tokens only when the splitter needed no lexical recovery.
//
// Returns []sqllex.Token which is the immutable token view, or nil after lexical
// recovery.
func (block QueryBlock) StrictTokens() []sqllex.Token {
	if block.recovered {
		return nil
	}
	return block.tokens
}

// QuerySyntax is what SplitQueryFile needs to know to find the query blocks of a file:
// how the dialect writes SQL text and how directives are written.
type QuerySyntax struct {
	// Prefixes names the directive prefixes to recognise.
	Prefixes namespace.Set

	// Directives lists recognised directive roles so misspelled directives can be reported
	// at their own blocks.
	Directives []string

	// Lexicon is how the dialect writes SQL: its lexical rules and the prefix that opens a
	// directive comment.
	Lexicon syntax.Lexicon
}

// blockStart is where one block begins.
type blockStart struct {
	// offset is the byte offset of the block's first byte.
	offset int

	// opened is true when an opener begins the block, and false for the text before the
	// first opener.
	opened bool
}

// querySplitter holds the state of one SplitQueryFile call.
type querySplitter struct {
	// text is the complete source file being split.
	text string

	// tokens holds the source file's lexical tokens.
	tokens []sqllex.Token

	// lines maps source byte offsets to line numbers.
	lines syntax.LineIndex

	// syntax defines the query directives and dialect lexical rules.
	syntax QuerySyntax

	// truncatedAt is the byte offset from which the file was read as one stretch of SQL,
	// because it held too many unterminated constructs, or -1 when it was read whole.
	truncatedAt int

	// recovered reports whether lexical recovery was needed in the file.
	recovered bool
}

// newQuerySplitter reads a file's tokens and line starts.
//
// Takes text (string) which is the file content without its byte order mark.
// Takes querySyntax (QuerySyntax) which describes the dialect's lexical rules and
// directives.
//
// Returns *querySplitter which is ready to cut the file into blocks.
func newQuerySplitter(text string, querySyntax QuerySyntax) *querySplitter {
	scan := syntax.ScanTokens(querySyntax.Lexicon.Rules, text)
	return &querySplitter{
		text:        text,
		syntax:      querySyntax,
		tokens:      scan.Tokens,
		truncatedAt: scan.TruncatedAt,
		recovered:   scan.Recovered,
		lines:       syntax.NewLineIndex(text),
	}
}

// blocks cuts the file at every block opener.
//
// Returns []QueryBlock which holds the blocks in file order.
func (s *querySplitter) blocks() []QueryBlock {
	starts := s.blockStarts()
	var blocks []QueryBlock
	for index, start := range starts {
		end := len(s.text)
		if index+1 < len(starts) {
			end = starts[index+1].offset
		}
		if !start.opened && !s.holdsQuery(start.offset, end) {
			continue
		}
		if block, ok := s.block(start.offset, end); ok {
			blocks = append(blocks, block)
		}
	}
	return blocks
}

// blockStarts finds where each block begins: the start of the file, and each opener,
// moved up over the comment lines directly above it.
//
// Returns []blockStart which holds the starts in file order.
func (s *querySplitter) blockStarts() []blockStart {
	starts := []blockStart{{offset: 0, opened: false}}
	floor := 0
	sawCode := false
	for index, token := range s.tokens {
		if !token.Kind.IsNoise() {
			sawCode = true
			continue
		}
		if !s.opensBlock(token, sawCode) {
			continue
		}
		start := s.leadingCommentStart(index, floor)
		starts = append(starts, blockStart{offset: start, opened: true})
		floor = index + 1
		sawCode = false
	}
	return starts
}

// opensBlock reports whether a token opens a block.
//
// Takes token (sqllex.Token) which is a whitespace or comment token.
// Takes sawCode (bool) which is true once the current block's SQL has started.
//
// Returns bool which is true for a query call, or, after the SQL has started, a call with
// an accepted prefix and a role that is not a directive.
func (s *querySplitter) opensBlock(token sqllex.Token, sawCode bool) bool {
	comment, isDirectiveComment := syntax.ReadDirectiveComment(s.text, s.lines, token, s.syntax.Lexicon.LinePrefix)
	if !isDirectiveComment {
		return false
	}
	prefix, role, rest := syntax.DirectiveName(comment.Body)
	switch s.syntax.Prefixes.Match(prefix) {
	case namespace.KindCanonical, namespace.KindAlias:
		return opensQuery(role, rest) ||
			(sawCode && role != "" && syntax.OpensCall(rest) && !slices.Contains(s.syntax.Directives, role))
	case namespace.KindForeign:
		return opensQuery(role, rest)
	case namespace.KindNone:
	}
	return false
}

// leadingCommentStart returns where the block an opener begins starts: the start of the
// opener's line, moved up over the prose comment lines directly above it. The move stops
// at a blank line, a line holding SQL, a directive comment, or the token at floor.
//
// Takes openerIndex (int) which is the index of the opener token.
// Takes floor (int) which is the lowest token index the move may reach.
//
// Returns int which is the byte offset where the block starts.
func (s *querySplitter) leadingCommentStart(openerIndex, floor int) int {
	start := s.lines.LineStart(s.tokens[openerIndex].Start)
	for index := openerIndex - 1; index >= floor; index-- {
		token := s.tokens[index]
		if token.Kind == sqllex.KindWhitespace {
			if strings.Count(token.Raw, "\n") > 1 {
				break
			}
			continue
		}
		if token.Kind != sqllex.KindComment || !s.lines.BeginsLine(s.text, token.Start) || s.looksLikeDirective(token) {
			break
		}
		start = s.lines.LineStart(token.Start)
	}
	return start
}

// looksLikeDirective reports whether a comment carries a directive call or a parameter
// binding, which belongs to the block above rather than moving down with the opener.
//
// Takes token (sqllex.Token) which is the comment.
//
// Returns bool which is true when the comment's text opens a call with a recognised
// prefix, or holds " as <prefix>." for one.
func (s *querySplitter) looksLikeDirective(token sqllex.Token) bool {
	comment, isDirectiveComment := syntax.ReadDirectiveComment(s.text, s.lines, token, s.syntax.Lexicon.LinePrefix)
	if !isDirectiveComment {
		return false
	}
	prefix, _, rest := syntax.DirectiveName(comment.Body)
	if s.syntax.Prefixes.Match(prefix) != namespace.KindNone && (syntax.OpensCall(rest) || strings.HasPrefix(rest, ".")) {
		return true
	}
	for _, recognised := range slices.Concat(s.syntax.Prefixes.Accepted(), s.syntax.Prefixes.Foreign) {
		if strings.Contains(comment.Body, " as "+recognised+".") {
			return true
		}
	}
	return false
}

// holdsQuery reports whether a stretch of the file holds anything a query block is made
// of: SQL, or a directive comment.
//
// Takes start (int) which is the stretch's first byte offset.
// Takes end (int) which is the offset just past its last byte.
//
// Returns bool which is true when a significant token or a directive comment starts
// inside the stretch.
func (s *querySplitter) holdsQuery(start, end int) bool {
	for _, token := range s.tokens {
		if token.Start >= end {
			return false
		}
		if token.Start >= start && (!token.Kind.IsNoise() || s.looksLikeDirective(token)) {
			return true
		}
	}
	return false
}

// block builds the block covering a stretch of the file, trimmed of the whitespace around
// it.
//
// Takes start (int) which is the stretch's first byte offset.
// Takes end (int) which is the offset just past its last byte.
//
// Returns QueryBlock which is the block.
// Returns bool which is false when the stretch holds only whitespace.
func (s *querySplitter) block(start, end int) (QueryBlock, bool) {
	stretch := s.text[start:end]
	trimmed := strings.TrimSpace(stretch)
	if trimmed == "" {
		return QueryBlock{}, false
	}
	first := start + strings.Index(stretch, trimmed)
	last := first + len(trimmed)
	from, _ := slices.BinarySearchFunc(s.tokens, first, func(token sqllex.Token, offset int) int {
		return cmp.Compare(token.End, offset+1)
	})
	to, _ := slices.BinarySearchFunc(s.tokens, last, func(token sqllex.Token, offset int) int {
		return cmp.Compare(token.Start, offset)
	})
	tokens := make([]sqllex.Token, to-from)
	for index, token := range s.tokens[from:to] {
		token.Start, token.End = max(token.Start, first)-first, min(token.End, last)-first
		token.Raw = trimmed[token.Start:token.End]
		tokens[index] = token
	}
	return QueryBlock{SQL: trimmed, Lexical: s.syntax.Lexicon.Rules, StartLine: s.lines.Line(first), tokens: tokens, recovered: s.recovered}, true
}

// ListQueryFiles returns the names of the query files in a directory: every file whose
// name ends in .sql, sorted by name, leaving out directories, named pipes, sockets and
// devices. It reads no file, so a caller can read and process one file at a time.
//
// Takes fileReader (FileReader) which provides filesystem access.
// Takes directory (string) which is the path to the query files.
//
// Returns []string which holds the file names, sorted.
// Returns error when the directory cannot be read.
func ListQueryFiles(ctx context.Context, fileReader FileReader, directory string) ([]string, error) {
	entries, err := fileReader.ReadDir(ctx, directory)
	if err != nil {
		return nil, fmt.Errorf("reading query directory %s: %w", directory, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || isSpecialFile(entry) || !strings.HasSuffix(entry.Name(), sqlFileSuffix) {
			continue
		}
		names = append(names, entry.Name())
	}
	slices.Sort(names)
	return names, nil
}

// SplitQueryFile splits a SQL file holding several queries into one block per query.
//
// The file is read under the dialect's lexical rules, after any leading byte order mark,
// so text inside a string, a quoted identifier or a block comment never opens a block. A
// block opens at a line comment that begins its line with a query call, such as `--
// sgl.query(`, written with an accepted or a foreign prefix, and, after a block's SQL has
// started, at a call written with an accepted prefix and a role that is not a directive.
// The comment lines directly above an opener, with no blank line between, belong to the
// block it opens rather than to the SQL of the block above. Text before the first opener
// that holds only prose comments, such as a licence header, is not a block.
//
// Each block's SQL runs from its first to its last non-blank character, and StartLine is
// the line that character is on. When the file leaves a string or comment unterminated,
// reading resumes on the next line, so later blocks are still found and the block holding
// the unterminated text is left for the dialect to report. Past the number of
// unterminated constructs syntax.ScanTokens recovers from, the rest of the file is read
// as one stretch of SQL, and the line where that began is returned so the caller can
// report it.
//
// Takes content ([]byte) which holds the raw SQL file bytes.
// Takes querySyntax (QuerySyntax) which describes the dialect's lexical rules and the
// directives.
//
// Returns []QueryBlock which holds the individual query blocks with their line offsets.
// Returns int which is the 1-based line from which the file's directives were left
// unread, or 0 when the whole file was read.
func SplitQueryFile(content []byte, querySyntax QuerySyntax) ([]QueryBlock, int) {
	text := sqllex.TrimByteOrderMark(string(content))
	splitter := newQuerySplitter(text, querySyntax)
	truncatedLine := 0
	if splitter.truncatedAt >= 0 {
		truncatedLine = splitter.lines.Line(splitter.truncatedAt)
	}
	return splitter.blocks(), truncatedLine
}

// opensQuery reports whether a directive name is the query role followed by its argument
// list, a space, a tab or the end of the comment.
//
// Takes role (string) which is the role after the prefix.
// Takes rest (string) which is the text after the name.
//
// Returns bool which is true when the name opens a query block.
func opensQuery(role, rest string) bool {
	return role == queryRole && (rest == "" || strings.IndexByte("( \t", rest[0]) >= 0)
}

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

package directive

import (
	"fmt"
	"strings"
	"unicode"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqlfile"
	"seagull.sh/seagull/internal/sqllex"
)

// blockComments holds the directive comments of a query block, read under the block's
// lexical rules, so a comment opener inside a string, a quoted identifier or a block
// comment is never taken for a directive.
type blockComments struct {
	// lines indexes the block's lines.
	lines syntax.LineIndex

	// header holds the directive comments before the block's SQL, in order.
	header []syntax.DirectiveComment

	// trailing holds the directive comments after the block's SQL has started.
	trailing []syntax.DirectiveComment

	// startLine is the file line of the block's first line.
	startLine int
}

// physicalLine returns the file line of an offset in the block.
//
// Takes offset (int) which is a byte offset into the block's SQL.
//
// Returns int which is the 1-based file line.
func (comments *blockComments) physicalLine(offset int) int {
	return comments.startLine + comments.lines.Line(offset) - 1
}

// bodySpan returns the file span of a stretch of a directive comment's text.
//
// Takes comment (syntax.DirectiveComment) which is the comment.
// Takes start (int) which is the stretch's first byte offset in the comment's text.
// Takes end (int) which is the offset just past the stretch.
//
// Returns diagbuild.TextSpan which covers the stretch on the comment's line.
func (comments *blockComments) bodySpan(comment syntax.DirectiveComment, start, end int) diagbuild.TextSpan {
	line := comments.physicalLine(comment.BodyStart)
	column := comments.lines.Column(comment.BodyStart)
	return diagbuild.TextSpan{Line: line, Column: column + start, EndLine: line, EndColumn: column + end}
}

// buildLogicalLine starts a logical line at a header comment and joins its continuation
// lines.
//
// The line keeps joining the header comments on the lines directly below it while the
// running paren depth remains open and no unterminated string literal has been seen.
// Continuation lines are accumulated into a strings.Builder, so joining stays linear, and
// are capped at maxContinuationLineCount, so an unclosed paren cannot pull an unbounded
// stretch of the file into a single logical line.
//
// Takes first (int) which is the index of the first comment in the header.
//
// Returns syntax.LogicalLine which is the assembled directive line.
// Returns int which is the index of the first header comment after the line.
// Returns bool which is true when the cap cut the continuation short.
func (comments *blockComments) buildLogicalLine(first int) (syntax.LogicalLine, int, bool) {
	comment := comments.header[first]
	startPhysical := comments.physicalLine(comment.BodyStart)
	segments := []syntax.LineSegment{{
		PhysicalLine: startPhysical,
		ContentStart: 0,
		ContentEnd:   len(comment.Body),
		ColumnOffset: comments.lines.Column(comment.BodyStart),
	}}
	var content strings.Builder
	content.WriteString(comment.Body)
	depth, unterminatedOffset, unterminatedQuote := syntax.LineDepth(comment.Body)

	last := first
	continuations := 0
	for depth > 0 && unterminatedOffset < 0 && comments.continues(last) {
		if continuations >= maxContinuationLineCount {
			break
		}
		last++
		continuations++
		next := comments.header[last]
		segmentStart := content.Len() + 1
		content.WriteByte(' ')
		content.WriteString(next.Body)
		nextDepth, nextUnterminatedColumn, nextUnterminatedQuote := syntax.LineDepth(next.Body)
		depth += nextDepth
		if nextUnterminatedColumn >= 0 {
			unterminatedOffset = segmentStart + nextUnterminatedColumn
			unterminatedQuote = nextUnterminatedQuote
		}
		segments = append(segments, syntax.LineSegment{
			PhysicalLine: comments.physicalLine(next.BodyStart),
			ContentStart: segmentStart,
			ContentEnd:   segmentStart + len(next.Body),
			ColumnOffset: comments.lines.Column(next.BodyStart),
		})
	}
	capped := depth > 0 && unterminatedOffset < 0 && continuations >= maxContinuationLineCount && comments.continues(last)

	return syntax.LogicalLine{
		Content:                  content.String(),
		Segments:                 segments,
		StartLine:                startPhysical,
		EndLine:                  comments.physicalLine(comments.header[last].BodyStart),
		UnterminatedStringOffset: unterminatedOffset,
		UnterminatedStringQuote:  unterminatedQuote,
	}, last + 1, capped
}

// continues reports whether the header comment after an index sits on the line directly
// below it, so it can continue the directive.
//
// Takes index (int) which is the index of a header comment.
//
// Returns bool which is true when the next header comment is on the next line.
func (comments *blockComments) continues(index int) bool {
	if index+1 >= len(comments.header) {
		return false
	}
	current := comments.lines.Line(comments.header[index].Token.Start)
	return comments.lines.Line(comments.header[index+1].Token.Start) == current+1
}

// lineLimits records the caps collectLogicalLines hit while gathering a header.
type lineLimits struct {
	// continuationLine is the file line of the first directive whose continuation lines were
	// cut at maxContinuationLineCount, or zero when none was.
	continuationLine int

	// logicalLines is true when the header held more than maxLogicalLineCount directive
	// lines.
	logicalLines bool
}

// readBlockComments reads the directive comments of a query block: the line comments that
// begin their line and open with the dialect's directive comment prefix, split into those
// before the block's first SQL token and those after it.
//
// Takes block (sqlfile.QueryBlock) which holds the SQL text, its lexical rules and its
// starting line.
//
// Returns blockComments which holds the comments in order.
func (p *Parser) readBlockComments(block sqlfile.QueryBlock) blockComments {
	lines := syntax.NewLineIndex(block.SQL)
	comments := blockComments{lines: lines, header: nil, trailing: nil, startLine: block.StartLine}
	inHeader := true
	for _, token := range block.Tokens() {
		if !token.Kind.IsNoise() {
			inHeader = false
			continue
		}
		if token.Kind != sqllex.KindComment {
			continue
		}
		comment, isDirectiveComment := syntax.ReadDirectiveComment(block.SQL, lines, token, p.commentStyle.LinePrefix)
		if !isDirectiveComment {
			continue
		}
		if inHeader {
			comments.header = append(comments.header, comment)
			continue
		}
		comments.trailing = append(comments.trailing, comment)
	}
	return comments
}

// collectLogicalLines gathers the directive lines of a block's header.
//
// A comment line that does not begin a directive (see looksLikeDirective) is prose owned
// by the SQL author and is skipped without scanning, so an apostrophe or an unbalanced
// bracket in that prose can neither be misreported as an unterminated string nor merge a
// following directive into itself through continuation. Each retained directive line is
// joined with the comment lines directly below it while its paren depth is open.
// Collection stops once maxLogicalLineCount logical lines have been gathered.
//
// Takes comments (*blockComments) which holds the block's directive comments.
//
// Returns []syntax.LogicalLine which are the collected directive lines after
// continuation.
// Returns lineLimits which reports each cap collection hit.
func (p *Parser) collectLogicalLines(comments *blockComments) ([]syntax.LogicalLine, lineLimits) {
	var result []syntax.LogicalLine
	var limits lineLimits
	index := 0
	for index < len(comments.header) {
		body := comments.header[index].Body
		if strings.TrimSpace(body) == "" || !p.looksLikeDirective(body) {
			index++
			continue
		}
		if len(result) >= maxLogicalLineCount {
			limits.logicalLines = true
			return result, limits
		}
		built, next, continuationCapped := comments.buildLogicalLine(index)
		if continuationCapped && limits.continuationLine == 0 {
			limits.continuationLine = built.StartLine
		}
		result = append(result, built)
		index = next
	}
	return result, limits
}

// description collects header prose while excluding directives and their continuation
// lines. It trims trailing spaces and preserves paragraph breaks without treating
// intervening directives as separators.
//
// Takes comments (*blockComments) which holds the block's directive comments.
//
// Returns string which is the prose, its lines joined by newlines, or empty when the
// header holds none.
func (p *Parser) description(comments *blockComments) string {
	var lines []string
	previousLine := -1
	index := 0
	for index < len(comments.header) {
		comment := comments.header[index]
		if strings.TrimSpace(comment.Body) != "" && p.looksLikeDirective(comment.Body) {
			_, next, _ := comments.buildLogicalLine(index)
			previousLine = comments.lines.Line(comments.header[next-1].Token.Start)
			index = next
			continue
		}
		line := comments.lines.Line(comment.Token.Start)
		text := strings.TrimRightFunc(comment.Body, unicode.IsSpace)
		if previousLine >= 0 && line > previousLine+1 {
			lines = append(lines, "")
		}
		lines = append(lines, text)
		previousLine = line
		index++
	}
	return joinParagraphs(lines)
}

// trailingDirectiveDiagnostics reports directives and bindings written after a block's
// SQL begins.
//
// Unknown directives are errors; known directives and bindings are warnings that they are
// ignored.
//
// Takes comments (*blockComments) which holds the block's directive comments.
// Takes errorBuilder (diagbuild.ErrorBuilder) which constructs the diagnostics.
//
// Returns []diag.SourceError which holds one diagnostic per trailing directive.
func (p *Parser) trailingDirectiveDiagnostics(comments *blockComments, errorBuilder diagbuild.ErrorBuilder) []diag.SourceError {
	var diagnostics []diag.SourceError
	prefixes := p.policy.Prefixes
	for _, comment := range comments.trailing {
		prefix, role, rest := syntax.DirectiveName(comment.Body)
		kind := prefixes.Match(prefix)
		if (kind == namespace.KindCanonical || kind == namespace.KindAlias) && syntax.OpensCall(rest) {
			nameStart := len(comment.Body) - len(strings.TrimLeft(comment.Body, " \t"))
			nameEnd := len(comment.Body) - len(rest)
			span := comments.bodySpan(comment, nameStart, nameEnd)
			name := prefixes.Qualify(role)
			if _, known := lookupDirective(role); !known || role == "" {
				diagnostics = append(diagnostics, errorBuilder.UnknownDirective(span, name, directiveNames(prefixes)))
				continue
			}
			diagnostics = append(diagnostics, ignoredTrailingDirective(errorBuilder, span, name))
			continue
		}
		if _, isAnchor := p.anchorSigil(strings.TrimLeft(comment.Body, " \t")); isAnchor && p.looksLikeParameterBinding(comment.Body) {
			span := comments.bodySpan(comment, 0, len(comment.Body))
			diagnostics = append(diagnostics, ignoredTrailingDirective(errorBuilder, span, "parameter binding"))
		}
	}
	return diagnostics
}

// joinParagraphs joins prose lines with newlines, leaving out empty lines at either end
// and keeping one empty line between paragraphs.
//
// Takes lines ([]string) which are the prose lines, an empty one between paragraphs.
//
// Returns string which is the joined prose.
func joinParagraphs(lines []string) string {
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" && (len(kept) == 0 || kept[len(kept)-1] == "") {
			continue
		}
		kept = append(kept, line)
	}
	for len(kept) > 0 && kept[len(kept)-1] == "" {
		kept = kept[:len(kept)-1]
	}
	return strings.Join(kept, "\n")
}

// limitDiagnostics reports the caps collection hit: a header with more directive lines
// than the parser reads, and a directive whose continuation lines were cut short, which
// would otherwise be parsed from a truncated text without saying so.
//
// Takes limits (lineLimits) which records the caps hit.
// Takes startLine (int) which is the block's first line.
// Takes errorBuilder (diagbuild.ErrorBuilder) which constructs the diagnostics.
//
// Returns []diag.SourceError which holds one diagnostic per cap hit.
func limitDiagnostics(limits lineLimits, startLine int, errorBuilder diagbuild.ErrorBuilder) []diag.SourceError {
	var diagnostics []diag.SourceError
	if limits.logicalLines {
		span := diagbuild.TextSpan{Line: startLine, Column: 1, EndLine: startLine, EndColumn: 1}
		diagnostics = append(diagnostics, errorBuilder.At(span, diag.CodeDirectiveLineLimit,
			fmt.Sprintf("directive header exceeded %d logical lines and was truncated", maxLogicalLineCount)))
	}
	if limits.continuationLine > 0 {
		span := diagbuild.TextSpan{Line: limits.continuationLine, Column: 1, EndLine: limits.continuationLine, EndColumn: 1}
		diagnostics = append(diagnostics, errorBuilder.At(span, diag.CodeDirectiveLineLimit,
			fmt.Sprintf("directive continued over more than %d lines and was truncated", maxContinuationLineCount)))
	}
	return diagnostics
}

// ignoredTrailingDirective warns that a directive written after a query's SQL is ignored.
//
// Takes errorBuilder (diagbuild.ErrorBuilder) which constructs the diagnostic.
// Takes span (diagbuild.TextSpan) which locates the directive.
// Takes name (string) which names the directive, such as sgl.column.
//
// Returns diag.SourceError which is the warning.
func ignoredTrailingDirective(errorBuilder diagbuild.ErrorBuilder, span diagbuild.TextSpan, name string) diag.SourceError {
	warning := errorBuilder.At(span, diag.CodeDirectiveWrongContext,
		name+" after the query's SQL is ignored; directives belong in the comment header above the SQL")
	warning.Severity = diag.SeverityWarning
	return warning
}

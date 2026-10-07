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
	"strings"

	"seagull.sh/seagull/internal/namespace"
)

// StripDirectives removes the directive comment lines from a SQL string, so the emitted
// SQL constant carries only the statement.
//
// A directive line is a line comment that begins its line with the lexicon's prefix and
// opens a call or a dotted name with the canonical prefix or an alias, such as `--
// sgl.query(` or `-- sgl(`, or binds a parameter, such as `-- $1 as sgl.param(` or `--
// {name:String} as sgl.param(`: a placeholder written in any form, then as, then an
// accepted prefix and a dot. The continuation lines of a directive whose parentheses stay
// open are stripped with it. The text is read under the lexicon's rules, so a line inside
// a string, a quoted identifier or a block comment is never touched. Directives written
// with a foreign prefix belong to another tool and are kept.
//
// Takes sql (string) which is the raw SQL text possibly containing directives.
// Takes lexicon (Lexicon) which is how the dialect writes SQL.
// Takes prefixes (namespace.Set) which names the directive prefixes to strip.
//
// The text kept is exactly what StripRanges locates, since both share one implementation,
// so a position in the SQL maps onto the stripped text through those ranges.
//
// Returns string which is the SQL with directive lines removed and the whitespace around
// it trimmed.
func StripDirectives(sql string, lexicon Lexicon, prefixes namespace.Set) string {
	return KeptText(sql, StripRanges(sql, lexicon, prefixes))
}

// IsDirectiveText reports whether the text of a directive comment opens a directive with
// the canonical prefix or an alias: a call or a dotted name, such as sgl.query( or sgl(,
// or a parameter binding, such as $1 as sgl.param.
//
// Takes body (string) which is the comment's text after its prefix.
// Takes prefixes (namespace.Set) which names the directive prefixes to recognise.
//
// Returns bool which is true when the text opens a directive.
func IsDirectiveText(body string, prefixes namespace.Set) bool {
	prefix, role, rest := DirectiveName(body)
	if prefix != "" {
		return prefixes.Accepts(prefix) && (role != "" || OpensCall(rest))
	}
	text := strings.TrimLeft(body, lineSpace)
	if text == "" || strings.IndexByte(lineSpace, text[0]) >= 0 {
		return false
	}
	for _, accepted := range prefixes.Accepted() {
		if strings.Contains(text, " as "+accepted+".") {
			return true
		}
	}
	return false
}

// directiveLines returns the lines of the directive comments to strip: each directive
// line and the continuation lines below it while its parentheses stay open.
//
// Takes comments ([]DirectiveComment) which are the text's directive comments in order.
// Takes lines (LineIndex) which indexes the text's lines.
// Takes prefixes (namespace.Set) which names the directive prefixes to strip.
//
// Returns map[int]struct{} which holds the 1-based line numbers to strip.
func directiveLines(comments []DirectiveComment, lines LineIndex, prefixes namespace.Set) map[int]struct{} {
	stripped := make(map[int]struct{})
	for index := 0; index < len(comments); index++ {
		if !IsDirectiveText(comments[index].Body, prefixes) {
			continue
		}
		line := lines.Line(comments[index].Token.Start)
		stripped[line] = struct{}{}
		depth, unterminated, _ := LineDepth(comments[index].Body)
		for depth > 0 && unterminated < 0 && index+1 < len(comments) && lines.Line(comments[index+1].Token.Start) == line+1 {
			index++
			line++
			stripped[line] = struct{}{}
			var change int
			change, unterminated, _ = LineDepth(comments[index].Body)
			depth += change
		}
	}
	return stripped
}

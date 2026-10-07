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

const (
	// directiveIndent separates the comment opener from a directive's text.
	directiveIndent = " "

	// parameterKeyword joins a parameter reference to its directive, as in $1 as sgl.param.
	parameterKeyword = "as"

	// indentSpace lists the characters that indent a line or a comment's text.
	indentSpace = " \t"

	// directiveSpace lists the characters normalised as spacing inside a directive.
	directiveSpace = " \t\r\v\f"
)

// FormatSource rewrites the directives of a SQL file into their canonical form.
//
// A directive comment, `-- <prefix>(`, `-- <prefix>.<role>(` or `-- <anchor> as
// <prefix>.<role>`, written with an alias of prefixes is rewritten to the canonical
// prefix. Its spacing is normalised: one space after the comment opener, single spaces
// between tokens, none inside brackets or before a comma, and one after a comma. The
// comment lines directly below a directive whose brackets stay open continue it; they
// keep their indentation after the comment opener and are normalised the same way. The
// file is read under the lexicon's rules, so a line inside a string, a quoted identifier,
// a dollar-quoted body or a block comment is never touched; quoted directive values
// follow the directive grammar, in which a backslash escapes the byte after it. Text
// after a directive's closing parenthesis is preserved apart from trailing whitespace.
// Quoted values and comments that are not directives are unchanged. Line endings are
// preserved, and formatting is idempotent.
//
// Takes source (string) which is the SQL file content.
// Takes lexicon (Lexicon) which is how the dialect writes SQL.
// Takes prefixes (namespace.Set) which names the canonical prefix and its aliases.
//
// Returns string which is the formatted content.
func FormatSource(source string, lexicon Lexicon, prefixes namespace.Set) string {
	comments, lines := lexicon.DirectiveComments(source)
	var builder strings.Builder
	builder.Grow(len(source))
	written := 0
	depth := 0
	previousLine := 0
	for _, comment := range comments {
		line := lines.Line(comment.Token.Start)
		if line != previousLine+1 {
			depth = 0
		}
		previousLine = line
		formatted, nextDepth, changed := formatComment(comment.Token.Raw[len(lexicon.LinePrefix):], depth, prefixes)
		depth = nextDepth
		if !changed {
			continue
		}
		builder.WriteString(source[written:comment.Token.Start])
		builder.WriteString(lexicon.LinePrefix)
		builder.WriteString(formatted)
		written = comment.Token.End
	}
	builder.WriteString(source[written:])
	return builder.String()
}

// formatComment formats the text of one directive comment after its opener, as a
// continuation of an open directive when depth is positive and as a possible directive
// otherwise.
//
// Takes body (string) which is the comment's text after the opener.
// Takes depth (int) which is the bracket depth left open by the comment above.
// Takes prefixes (namespace.Set) which names the canonical prefix and its aliases.
//
// Returns string which is the formatted text after the opener.
// Returns int which is the bracket depth left open by this comment.
// Returns bool which is true when the comment belongs to a directive.
func formatComment(body string, depth int, prefixes namespace.Set) (string, int, bool) {
	if depth > 0 {
		spacing := body[:len(body)-len(strings.TrimLeft(body, indentSpace))]
		normalised, nextDepth := normaliseDirective(body[len(spacing):], depth)
		return spacing + normalised, nextDepth, true
	}
	text, isDirective := canonicalHead(strings.TrimLeft(body, indentSpace), prefixes)
	if !isDirective {
		return body, 0, false
	}
	normalised, nextDepth := normaliseDirective(text, 0)
	return directiveIndent + normalised, nextDepth, true
}

// canonicalHead recognises the start of a directive and rewrites an alias prefix to the
// canonical one.
//
// Takes text (string) which is the comment text after the opener and its spacing.
// Takes prefixes (namespace.Set) which names the canonical prefix and its aliases.
//
// Returns string which is the text with the canonical prefix.
// Returns bool which is true when text opens a directive with an accepted prefix.
func canonicalHead(text string, prefixes namespace.Set) (string, bool) {
	prefix := leadingIdentifier(text)
	if prefix != "" {
		if !prefixes.Accepts(prefix) || !opensCall(text[len(prefix):]) {
			return text, false
		}
		return prefixes.Canonical + text[len(prefix):], true
	}
	return canonicalParameter(text, prefixes)
}

// opensCall reports whether the text after a prefix opens a directive call: `(` or
// `.<role>(`.
//
// Takes rest (string) which is the text after the prefix.
//
// Returns bool which is true when it opens a call.
func opensCall(rest string) bool {
	if strings.HasPrefix(rest, "(") {
		return true
	}
	role, isQualified := strings.CutPrefix(rest, ".")
	if !isQualified {
		return false
	}
	name := leadingIdentifier(role)
	return name != "" && strings.HasPrefix(role[len(name):], "(")
}

// canonicalParameter recognises a parameter directive, `<anchor> as <prefix>.<role>`,
// whose anchor is a placeholder in any form, such as $1, :email or {id:UInt64}, and
// rewrites an alias prefix to the canonical one.
//
// Only spaces and tabs may separate the anchor, as and the prefix, matching the directive
// lexer. A carriage return, vertical tab or form feed prevents a match.
//
// Takes text (string) which is the comment text after the opener and its spacing.
// Takes prefixes (namespace.Set) which names the canonical prefix and its aliases.
//
// Returns string which is the rewritten text with single spaces around as.
// Returns bool which is true when text is a parameter directive with an accepted prefix.
func canonicalParameter(text string, prefixes namespace.Set) (string, bool) {
	end := strings.IndexAny(text, directiveSpace)
	if end <= 0 || IsIdentifierPart(text[0]) {
		return text, false
	}
	reference := text[:end]
	after := strings.TrimLeft(text[end:], indentSpace)
	keyword, isParameter := strings.CutPrefix(after, parameterKeyword)
	if !isParameter || keyword == "" || strings.IndexByte(indentSpace, keyword[0]) < 0 {
		return text, false
	}
	after = strings.TrimLeft(keyword, indentSpace)
	prefix := leadingIdentifier(after)
	if !prefixes.Accepts(prefix) || !strings.HasPrefix(after[len(prefix):], ".") {
		return text, false
	}
	role := leadingIdentifier(after[len(prefix)+1:])
	if role == "" {
		return text, false
	}
	return reference + " " + parameterKeyword + " " + prefixes.Canonical + after[len(prefix):], true
}

// normaliseDirective normalises the spacing of directive text until the bracket that
// closes the call, copying the rest of the line unchanged apart from trailing spaces.
// Quoted values are copied as written, read by the directive grammar's string rules.
//
// Takes text (string) which is the directive text.
// Takes depth (int) which is the bracket depth open before text.
//
// Returns string which is the normalised text.
// Returns int which is the bracket depth open after text.
func normaliseDirective(text string, depth int) (string, int) {
	var builder strings.Builder
	builder.Grow(len(text))
	pendingSpace := false
	index := 0
	for index < len(text) {
		character := text[index]
		switch {
		case strings.IndexByte(directiveSpace, character) >= 0:
			pendingSpace = true
			index++
			continue
		case character == '\'' || character == '"':
			end, _ := SkipString(text, index)
			writeSeparated(&builder, pendingSpace, character)
			builder.WriteString(text[index+1 : end])
			pendingSpace = false
			index = end
			continue
		}
		writeSeparated(&builder, pendingSpace, character)
		pendingSpace = character == ','
		index++
		switch character {
		case '(', '[':
			depth++
		case ')', ']':
			depth = max(depth-1, 0)
			if depth == 0 && character == ')' {
				builder.WriteString(strings.TrimRight(text[index:], directiveSpace))
				return builder.String(), 0
			}
		}
	}
	return builder.String(), depth
}

// writeSeparated writes a character, preceded by one space when a separator is pending
// and the character may follow one.
//
// Takes builder (*strings.Builder) which receives the output.
// Takes pendingSpace (bool) which is true when whitespace or a comma came before.
// Takes character (byte) which is the character to write.
func writeSeparated(builder *strings.Builder, pendingSpace bool, character byte) {
	if pendingSpace && builder.Len() > 0 && !strings.ContainsRune(")],", rune(character)) {
		written := builder.String()
		if last := written[len(written)-1]; last != '(' && last != '[' {
			builder.WriteByte(' ')
		}
	}
	builder.WriteByte(character)
}

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

package goldenjson

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// templateOpen opens a segment other than text in the compact form.
	templateOpen = "⟨"

	// templateClose closes a segment other than text in the compact form.
	templateClose = "⟩"

	// templateBodySeparator separates a segment's head from its body in the compact form.
	templateBodySeparator = ":"

	// templateHeadSpecials lists the characters, besides those every text escapes, that are
	// escaped inside a star's head, where a space, an equals sign, a full stop or a colon
	// delimits its parts.
	templateHeadSpecials = " =.:"

	// templateByteEscape writes one byte as a hexadecimal escape.
	templateByteEscape = `\x%02x`
)

// clauseHeads names the clause segments in the compact form.
var clauseHeads = map[sqlmodel.SQLSegmentKind]string{
	sqlmodel.SegmentFilter:     "where",
	sqlmodel.SegmentOrderBy:    "order",
	sqlmodel.SegmentPagination: "pagination",
	sqlmodel.SegmentValuesRow:  "values",
}

// Template writes a query template in the deterministic one-line form golden files
// record: text as written, with control bytes, backslashes and the angle brackets that
// delimit segments escaped, and each other segment as a marker giving its kind,
// attributes and children.
//
// Takes template (sqlmodel.SQLTemplate) which is the template to write.
//
// Returns string which is the compact form, empty for a template with no segments and no
// arguments.
func Template(template sqlmodel.SQLTemplate) string {
	return compactTemplate(template)
}

// templateText returns the compact form of a value that is a query template.
//
// Takes value (reflect.Value) which is the candidate.
//
// Returns string which is the template's compact form.
// Returns bool which is true when the value is a sqlmodel.SQLTemplate.
func templateText(value reflect.Value) (string, bool) {
	if !value.CanInterface() {
		return "", false
	}
	template, isTemplate := reflect.TypeAssert[sqlmodel.SQLTemplate](value)
	if !isTemplate {
		return "", false
	}
	return Template(template), true
}

// compactTemplate writes a template in the compact form.
//
// Text escapes control bytes, backslashes and the Unicode angle brackets used as segment
// delimiters. Segment markers encode their kind, attributes and children; unused fields
// are omitted.
//
// Takes template (sqlmodel.SQLTemplate) which is the template to write.
//
// Returns string which holds the compact form.
func compactTemplate(template sqlmodel.SQLTemplate) string {
	var builder strings.Builder
	writeTemplateSegments(&builder, template.Segments)
	if len(template.Arguments) > 0 {
		builder.WriteString(templateOpen + "arguments")
		for _, parameter := range template.Arguments {
			builder.WriteString(" p" + strconv.Itoa(parameter))
		}
		builder.WriteString(templateClose)
	}
	return builder.String()
}

// writeTemplateSegments writes segments in the compact form.
//
// Takes builder (*strings.Builder) which receives the text.
// Takes segments ([]sqlmodel.SQLSegment) which are the segments to write.
func writeTemplateSegments(builder *strings.Builder, segments []sqlmodel.SQLSegment) {
	for index := range segments {
		writeTemplateSegment(builder, &segments[index])
	}
}

// writeTemplateSegment writes one segment in the compact form.
//
// Takes builder (*strings.Builder) which receives the text.
// Takes segment (*sqlmodel.SQLSegment) which is the segment to write.
func writeTemplateSegment(builder *strings.Builder, segment *sqlmodel.SQLSegment) {
	switch segment.Kind {
	case sqlmodel.SegmentText:
		writeTemplateEscaped(builder, segment.Text, "")
	case sqlmodel.SegmentParameter:
		builder.WriteString(templateOpen)
		writeTemplateParameterHead(builder, segment)
		builder.WriteString(templateClose)
	case sqlmodel.SegmentList:
		builder.WriteString(templateOpen)
		if segment.Negated {
			builder.WriteString("not ")
		}
		builder.WriteString("in ")
		writeTemplateParameterHead(builder, segment)
		builder.WriteString(templateBodySeparator)
		writeTemplateEscaped(builder, segment.Text, "")
		builder.WriteString(templateClose)
	case sqlmodel.SegmentColumn:
		writeTemplateColumn(builder, segment)
	case sqlmodel.SegmentStar:
		writeTemplateStar(builder, segment)
	case sqlmodel.SegmentFilter, sqlmodel.SegmentOrderBy, sqlmodel.SegmentPagination, sqlmodel.SegmentValuesRow:
		writeTemplateClause(builder, segment)
	default:
		builder.WriteString(templateOpen + segment.Kind.String())
		writeTemplateBody(builder, segment.Children)
		builder.WriteString(templateClose)
	}
}

// writeTemplateParameterHead writes a parameter's index and, when it carries one, its
// type.
//
// Takes builder (*strings.Builder) which receives the text.
// Takes segment (*sqlmodel.SQLSegment) which is the parameter or list segment.
func writeTemplateParameterHead(builder *strings.Builder, segment *sqlmodel.SQLSegment) {
	builder.WriteString("p" + strconv.Itoa(segment.Parameter))
	if segment.TypeText != "" {
		builder.WriteByte(' ')
		writeTemplateEscaped(builder, segment.TypeText, templateBodySeparator)
	}
}

// writeTemplateColumn writes an output column segment.
//
// Takes builder (*strings.Builder) which receives the text.
// Takes segment (*sqlmodel.SQLSegment) which is the column segment.
func writeTemplateColumn(builder *strings.Builder, segment *sqlmodel.SQLSegment) {
	builder.WriteString(templateOpen + "c" + strconv.Itoa(segment.Column))
	if segment.Aliased {
		builder.WriteString(" aliased")
	}
	builder.WriteString(templateBodySeparator)
	writeTemplateSegments(builder, segment.Children)
	builder.WriteString(templateClose)
}

// writeTemplateStar writes a star segment: whether it is written as it is, the columns it
// stands for, then its text.
//
// Takes builder (*strings.Builder) which receives the text.
// Takes segment (*sqlmodel.SQLSegment) which is the star segment.
func writeTemplateStar(builder *strings.Builder, segment *sqlmodel.SQLSegment) {
	builder.WriteString(templateOpen + "star")
	if segment.AsWritten {
		builder.WriteString(" written")
	}
	for _, column := range segment.Star {
		builder.WriteString(" " + strconv.Itoa(column.Column) + "=")
		if column.Qualifier != "" {
			writeTemplateEscaped(builder, column.Qualifier, templateHeadSpecials)
			builder.WriteByte('.')
		}
		writeTemplateEscaped(builder, column.Name, templateHeadSpecials)
	}
	builder.WriteString(templateBodySeparator)
	writeTemplateEscaped(builder, segment.Text, "")
	builder.WriteString(templateClose)
}

// writeTemplateClause writes a filter, ORDER BY, pagination or VALUES row segment: its
// place alone when it is absent and empty, and otherwise its children, marked absent when
// a clause that can be absent is.
//
// Takes builder (*strings.Builder) which receives the text.
// Takes segment (*sqlmodel.SQLSegment) which is the clause segment.
func writeTemplateClause(builder *strings.Builder, segment *sqlmodel.SQLSegment) {
	builder.WriteString(templateOpen + clauseHeads[segment.Kind])
	absent := !segment.Present && segment.Kind != sqlmodel.SegmentValuesRow
	if absent && len(segment.Children) > 0 {
		builder.WriteString(" absent")
	}
	if !absent || len(segment.Children) > 0 {
		writeTemplateBody(builder, segment.Children)
	}
	builder.WriteString(templateClose)
}

// writeTemplateBody writes the separator and a segment's children.
//
// Takes builder (*strings.Builder) which receives the text.
// Takes children ([]sqlmodel.SQLSegment) which are the children to write.
func writeTemplateBody(builder *strings.Builder, children []sqlmodel.SQLSegment) {
	builder.WriteString(templateBodySeparator)
	writeTemplateSegments(builder, children)
}

// writeTemplateEscaped writes text with the compact form's escapes, escaping with a
// backslash the characters specials lists as well as those every text escapes.
//
// Takes builder (*strings.Builder) which receives the text.
// Takes text (string) which is the text to write.
// Takes specials (string) which lists the further characters to escape.
func writeTemplateEscaped(builder *strings.Builder, text string, specials string) {
	for index := 0; index < len(text); {
		character, size := utf8.DecodeRuneInString(text[index:])
		builder.WriteString(templateEscape(text[index:index+size], character, specials))
		index += size
	}
}

// templateEscape returns how the compact form writes one character.
//
// Takes raw (string) which is the character's bytes as written.
// Takes character (rune) which is the decoded character, utf8.RuneError for a byte of
// invalid UTF-8.
// Takes specials (string) which lists the further characters to escape.
//
// Returns string which is the character as the compact form writes it.
func templateEscape(raw string, character rune, specials string) string {
	switch {
	case character == utf8.RuneError && len(raw) == 1:
		return fmt.Sprintf(templateByteEscape, raw[0])
	case character == '\\' || raw == templateOpen || raw == templateClose || strings.ContainsRune(specials, character):
		return `\` + raw
	case character == '\n':
		return `\n`
	case character == '\r':
		return `\r`
	case character == '\t':
		return `\t`
	case character < ' ' || character == unicode.MaxASCII:
		return fmt.Sprintf(templateByteEscape, character)
	default:
		return raw
	}
}

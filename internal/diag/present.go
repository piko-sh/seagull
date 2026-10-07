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

package diag

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// caret underlines the span in the quoted source line.
	caret = "^"

	// helpPrefix starts the line that phrases the suggestion.
	helpPrefix = "help: "

	// wordSpace separates the words of a message.
	wordSpace = " "

	// didYouMeanFormat phrases a suggestion as a question.
	didYouMeanFormat = "did you mean %q?"

	// maxQuotedSourceRunes is the most runes of a source line a presentation quotes: a
	// longer line, such as a minified migration, is cut to a window around the span.
	maxQuotedSourceRunes = 160

	// leadingContextRunes is how many runes before the span a cut window keeps, when the
	// line has them.
	leadingContextRunes = maxQuotedSourceRunes / 4

	// ellipsis marks where a quoted source line was cut.
	ellipsis = "..."
)

// Presentation is one diagnostic laid out for display: a header, the quoted source line
// with carets under the span, and a help line. Present fills it and WriteText prints it;
// other renderers, such as an HTML panel, can lay the same fields out their own way.
type Presentation struct {
	// Location is the path with the line and column, such as db/queries/tasks.sql:3:8. It
	// drops the column, or the line too, when the diagnostic has none.
	Location string

	// Severity is error, warning or hint.
	Severity string

	// Code is the diagnostic code, such as Q001.
	Code string

	// Title is the code's title in the code table, or empty for an unlisted code.
	Title string

	// Message describes the problem, without a leading copy of the code or the "did you
	// mean" that Help repeats.
	Message string

	// Help phrases the suggested fix, or is empty.
	Help string

	// Source is the quoted source line, or empty when it is unavailable. A line longer than
	// 160 runes is cut to a window around the span, with "..." where it was cut.
	Source string

	// LineNo is the line number shown in the gutter, or empty without a source line.
	LineNo string

	// Gutter is blank padding as wide as LineNo, for the gutter's other lines.
	Gutter string

	// CaretPad is the whitespace before the carets. It keeps the quoted line's tabs, so the
	// carets line up however wide a terminal draws a tab.
	CaretPad string

	// Label follows the carets, or is empty. Present leaves it empty, because the suggestion
	// goes on the help line; a caller may set it to annotate the span.
	Label string

	// CaretWidth is how many carets underline the span, within the quoted window: at least
	// one with a source line, zero without.
	CaretWidth int
}

// sourceWindow is the part of a source line a presentation quotes, with the carets that
// underline the span within it.
type sourceWindow struct {
	// text is the quoted text, with "..." where the line was cut.
	text string

	// pad is the whitespace before the carets.
	pad string

	// width is how many carets underline the span.
	width int
}

// Present lays a diagnostic out for display.
//
// The caller supplies the display path and source line; no files are read. Lines longer
// than 160 runes are clipped to a window of at most 160 runes around the span. Clipping
// preserves rune boundaries and marks omitted text with "...".
//
// Takes diagnostic (SourceError) which is the diagnostic.
// Takes display (string) which is the path to show for diagnostic.Filename, or empty.
// Takes sourceLine (string) which is the text of line diagnostic.Line, or empty when it
// is unavailable.
//
// Returns Presentation which holds the parts to print.
func Present(diagnostic SourceError, display string, sourceLine string) Presentation {
	presentation := Presentation{
		Location:   location(display, diagnostic.Line, diagnostic.Column),
		Severity:   diagnostic.Severity.String(),
		Code:       diagnostic.Code,
		Title:      codeTitle(diagnostic.Code),
		Message:    presentedMessage(diagnostic),
		Help:       helpText(diagnostic.Suggestion),
		Source:     "",
		LineNo:     "",
		Gutter:     "",
		CaretPad:   "",
		Label:      "",
		CaretWidth: 0,
	}
	source := strings.TrimRight(sourceLine, "\r\n")
	if source == "" || diagnostic.Line <= 0 {
		return presentation
	}
	window := quotedWindow(diagnostic, source)
	presentation.Source = window.text
	presentation.LineNo = strconv.Itoa(diagnostic.Line)
	presentation.Gutter = strings.Repeat(" ", len(presentation.LineNo))
	presentation.CaretPad = window.pad
	presentation.CaretWidth = window.width
	return presentation
}

// WriteText prints presentations for a terminal, one block each, separated by a blank
// line.
//
//	db/queries/tasks.sql:3:8: error[Q001]: unknown column "tilte"
//	   |
//	 3 | SELECT tilte FROM tasks
//	   |        ^^^^^
//	help: did you mean "title"?
//
// A presentation without a source line prints the header and the help line only. Each
// block is written as soon as it is laid out, so the text of every block is never held at
// once, and writing stops at the first error.
//
// Takes w (io.Writer) which receives the text.
// Takes items ([]Presentation) which are printed in order.
//
// Returns error when writing fails.
func WriteText(w io.Writer, items []Presentation) error {
	var text strings.Builder
	for index := range items {
		text.Reset()
		if index > 0 {
			text.WriteByte('\n')
		}
		writeBlock(&text, &items[index])
		if _, err := io.WriteString(w, text.String()); err != nil {
			return err
		}
	}
	return nil
}

// DidYouMean phrases a suggestion as the question a diagnostic's message and help line
// ask, such as `did you mean "title"?`, so every producer and renderer words it alike.
//
// Takes suggestion (string) which is the suggested name or value.
//
// Returns string which is the question.
func DidYouMean(suggestion string) string {
	return fmt.Sprintf(didYouMeanFormat, suggestion)
}

// writeBlock prints one presentation.
//
// Takes text (*strings.Builder) which receives the block.
// Takes item (*Presentation) which is printed.
func writeBlock(text *strings.Builder, item *Presentation) {
	text.WriteString(header(item))
	text.WriteByte('\n')
	if item.Source != "" {
		fmt.Fprintf(text, " %s |\n", item.Gutter)
		fmt.Fprintf(text, " %s | %s\n", item.LineNo, item.Source)
		fmt.Fprintf(text, " %s | %s%s", item.Gutter, item.CaretPad, strings.Repeat(caret, max(item.CaretWidth, 1)))
		if item.Label != "" {
			text.WriteString(" " + item.Label)
		}
		text.WriteByte('\n')
	}
	if item.Help != "" {
		text.WriteString(helpPrefix + item.Help + "\n")
	}
}

// header renders a presentation's first line: location, severity, code and message.
//
// Takes item (*Presentation) which is rendered.
//
// Returns string which is the line without its newline.
func header(item *Presentation) string {
	severity := item.Severity
	if item.Code != "" {
		severity += "[" + item.Code + "]"
	}
	if item.Location == "" {
		return severity + ": " + item.Message
	}
	return item.Location + ": " + severity + ": " + item.Message
}

// location renders a path with a line and column.
//
// Takes display (string) which is the path, or empty.
// Takes line (int) which is one-based, or zero when unknown.
// Takes column (int) which is one-based, or zero when unknown.
//
// Returns string which is path, path:line or path:line:column, without the path and its
// colon when display is empty.
func location(display string, line, column int) string {
	if line <= 0 {
		return display
	}
	position := strconv.Itoa(line)
	if column > 0 {
		position += ":" + strconv.Itoa(column)
	}
	if display == "" {
		return position
	}
	return display + ":" + position
}

// codeTitle looks a code's title up in the code table.
//
// Takes code (string) which is the diagnostic code.
//
// Returns string which is the title, or empty for an unlisted code.
func codeTitle(code string) string {
	if code == "" {
		return ""
	}
	info, found := LookupCode(code)
	if !found {
		return ""
	}
	return info.Title
}

// presentedMessage returns a diagnostic's message without the parts the rendering shows
// elsewhere: a leading copy of the code, and the "did you mean" that repeats the
// suggestion printed on the help line, wherever it sits in the message.
//
// Takes diagnostic (SourceError) which is presented.
//
// Returns string which is the message to print.
func presentedMessage(diagnostic SourceError) string {
	message := strings.TrimPrefix(diagnostic.Message, diagnostic.Code+": ")
	if diagnostic.Suggestion == "" {
		return message
	}
	return withoutSuggestion(message, DidYouMean(diagnostic.Suggestion))
}

// withoutSuggestion removes a "did you mean" fragment from a message and tidies the
// punctuation around it.
//
// A trailing fragment takes its leading separator with it (`unknown column "x"; did you
// mean "y"?` becomes `unknown column "x"`). A fragment mid-message keeps the separator
// before it for the text that follows (`...; did you mean "y"? allowed: a, b` becomes
// `...; allowed: a, b`).
//
// Takes message (string) which is the message to tidy.
// Takes fragment (string) which is the "did you mean" text to remove.
//
// Returns string which is the message without the fragment, or the message unchanged when
// it does not hold the fragment.
func withoutSuggestion(message, fragment string) string {
	before, after, found := strings.Cut(message, fragment)
	if !found {
		return message
	}
	before = strings.TrimRight(before, wordSpace)
	after = strings.TrimLeft(after, wordSpace)
	if after == "" {
		return strings.TrimRight(before, ";,: ")
	}
	if before == "" {
		return after
	}
	return before + wordSpace + after
}

// helpText phrases a suggestion: a single word as "did you mean", anything longer as is.
//
// Takes suggestion (string) which is the diagnostic's suggestion, or empty.
//
// Returns string which is the help line's text, or empty without a suggestion.
func helpText(suggestion string) string {
	if suggestion == "" {
		return ""
	}
	if strings.ContainsAny(suggestion, " \t") {
		return suggestion
	}
	return DidYouMean(suggestion)
}

// quotedWindow cuts a source line to the window a presentation quotes.
//
// The window is the whole line when it holds at most maxQuotedSourceRunes runes.
// Otherwise, it holds that many runes and starts up to leadingContextRunes runes before
// the span, cut on rune boundaries and marked with an ellipsis at each cut end.
//
// Takes diagnostic (SourceError) which holds the span, its columns one-based byte offsets
// and its end exclusive.
// Takes line (string) which is the source line the span starts on.
//
// Returns sourceWindow which holds the quoted text, the caret padding and the caret
// count, at least one.
func quotedWindow(diagnostic SourceError, line string) sourceWindow {
	start := min(max(diagnostic.Column-1, 0), len(line))
	end := spanEnd(diagnostic, line, start)
	if !runesExceed(line, maxQuotedSourceRunes) {
		return sourceWindow{text: line, pad: caretPadding(line[:start]), width: max(utf8.RuneCountInString(line[start:end]), 1)}
	}
	from := runesBack(line, start, leadingContextRunes)
	to := runesForward(line, from, maxQuotedSourceRunes)
	if to == len(line) {
		from = runesBack(line, len(line), maxQuotedSourceRunes)
	}
	var text strings.Builder
	pad := caretPadding(line[from:start])
	if from > 0 {
		text.WriteString(ellipsis)
		pad = strings.Repeat(" ", len(ellipsis)) + pad
	}
	text.WriteString(line[from:to])
	if to < len(line) {
		text.WriteString(ellipsis)
	}
	return sourceWindow{text: text.String(), pad: pad, width: max(utf8.RuneCountInString(line[start:max(min(end, to), start)]), 1)}
}

// spanEnd returns the byte offset in a line just past the part of a span the carets
// underline: the span's end for a span that ends on its first line, cut at the end of the
// line, and the start otherwise, so one caret marks it.
//
// Takes diagnostic (SourceError) which holds the span, its end exclusive.
// Takes line (string) which is the source line the span starts on.
// Takes start (int) which is the byte offset the span starts at, within the line.
//
// Returns int which is the end offset, never before start.
func spanEnd(diagnostic SourceError, line string, start int) int {
	if diagnostic.EndLine != diagnostic.Line || diagnostic.EndColumn <= diagnostic.Column {
		return start
	}
	return min(max(diagnostic.EndColumn-1, start), len(line))
}

// runesExceed reports whether a text holds more than limit runes, reading no further than
// the rune past the limit.
//
// Takes text (string) which is the text.
// Takes limit (int) which is the most runes allowed.
//
// Returns bool which is true when the text holds more runes than limit.
func runesExceed(text string, limit int) bool {
	return runesForward(text, 0, limit) < len(text)
}

// runesForward returns the byte offset count runes after an offset, or the end of the
// text when fewer remain.
//
// Takes text (string) which is the text.
// Takes offset (int) which is the byte offset to start from, on a rune boundary.
// Takes count (int) which is how many runes to move.
//
// Returns int which is the byte offset reached.
func runesForward(text string, offset, count int) int {
	for moved := 0; moved < count && offset < len(text); moved++ {
		_, width := utf8.DecodeRuneInString(text[offset:])
		offset += width
	}
	return offset
}

// runesBack returns the byte offset count runes before an offset, or the start of the
// text when fewer precede it.
//
// Takes text (string) which is the text.
// Takes offset (int) which is the byte offset to start from.
// Takes count (int) which is how many runes to move.
//
// Returns int which is the byte offset reached.
func runesBack(text string, offset, count int) int {
	for moved := 0; moved < count && offset > 0; moved++ {
		_, width := utf8.DecodeLastRuneInString(text[:offset])
		offset -= width
	}
	return offset
}

// caretPadding returns the whitespace that puts a caret after a stretch of a line,
// keeping tabs so the caret lines up however the terminal renders them.
//
// Takes before (string) which is the part of the line before the caret.
//
// Returns string which is the padding, one space or tab per rune.
func caretPadding(before string) string {
	var padding strings.Builder
	for _, character := range before {
		if character == '\t' {
			padding.WriteByte('\t')
			continue
		}
		padding.WriteByte(' ')
	}
	return padding.String()
}

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

package session

import (
	"strings"

	"seagull.sh/seagull"
)

// DiagnosticView presents a diagnostic from the request's files.
//
// Text contains the formatted CLI-style message, while the other fields let clients
// render its parts themselves.
type DiagnosticView struct {
	// File is the request path of the file the diagnostic is in, the configuration file for
	// a diagnostic about the configuration, or empty.
	File string `json:"file"`

	// Severity is error, warning or hint.
	Severity string `json:"severity"`

	// Code is the diagnostic code, such as Q001.
	Code string `json:"code"`

	// Title is the code's short title, or empty for an unknown code.
	Title string `json:"title"`

	// Message describes the problem, without a leading copy of the code or the "did you
	// mean" that Help repeats.
	Message string `json:"message"`

	// Suggestion is the suggested replacement or fix, as the diagnostic gives it.
	Suggestion string `json:"suggestion"`

	// Help phrases the suggestion for the help line, or is empty.
	Help string `json:"help"`

	// Label follows the carets, or is empty.
	Label string `json:"label"`

	// Location is the file, line and column, as file:line:column.
	Location string `json:"location"`

	// Source is the source line the diagnostic points at, or empty when there is none to
	// show.
	Source string `json:"source"`

	// LineNo is the line number shown in the gutter beside Source, or empty.
	LineNo string `json:"lineNo"`

	// Gutter is blank padding as wide as LineNo, for the gutter's other lines.
	Gutter string `json:"gutter"`

	// CaretPad is the whitespace before the carets, keeping tabs so the carets line up under
	// Source however tabs are rendered.
	CaretPad string `json:"caretPad"`

	// Text is the whole diagnostic as sgl vet prints it, ending with a newline.
	Text string `json:"text"`

	// Line is the one-based line, or zero when unknown.
	Line int `json:"line"`

	// Column is the one-based column in bytes, or zero when unknown.
	Column int `json:"column"`

	// EndLine is the one-based end line, or zero.
	EndLine int `json:"endLine"`

	// EndColumn is the one-based column just past the span, or zero.
	EndColumn int `json:"endColumn"`

	// CaretWidth is the number of carets under Source, or zero without a source line.
	CaretWidth int `json:"caretWidth"`
}

// diagnosticViews presents every diagnostic of a run.
//
// Takes files (map[string]string) which maps request paths to contents.
// Takes resolved (*project) which maps file names back to request paths.
// Takes diagnostics ([]seagull.SourceError) which are presented.
//
// Returns []DiagnosticView which holds one entry per diagnostic, in order.
func diagnosticViews(files map[string]string, resolved *project, diagnostics []seagull.SourceError) []DiagnosticView {
	lines := map[string][]string{}
	views := make([]DiagnosticView, 0, len(diagnostics))
	for index := range diagnostics {
		diagnostic := &diagnostics[index]
		file := resolved.sourcePath(files, diagnostic.Filename)
		fileLines, cached := lines[file]
		if !cached {
			fileLines = strings.Split(files[file], "\n")
			lines[file] = fileLines
		}
		views = append(views, presentDiagnostic(file, fileLines, diagnostic))
	}
	return views
}

// presentDiagnostic lays out one diagnostic with seagull.Present and seagull.WriteText.
//
// Takes file (string) which is the request path of its file, or empty.
// Takes fileLines ([]string) which holds the file's lines, or none.
// Takes diagnostic (*seagull.SourceError) which is presented.
//
// Returns DiagnosticView which holds the layout and its parts.
func presentDiagnostic(file string, fileLines []string, diagnostic *seagull.SourceError) DiagnosticView {
	source := ""
	if diagnostic.Line > 0 && diagnostic.Line <= len(fileLines) {
		source = fileLines[diagnostic.Line-1]
	}
	presentation := seagull.Present(*diagnostic, file, source)
	var text strings.Builder
	_ = seagull.WriteText(&text, []seagull.Presentation{presentation})
	return DiagnosticView{
		File:       file,
		Severity:   presentation.Severity,
		Code:       presentation.Code,
		Title:      presentation.Title,
		Message:    presentation.Message,
		Suggestion: diagnostic.Suggestion,
		Help:       presentation.Help,
		Label:      presentation.Label,
		Location:   presentation.Location,
		Source:     presentation.Source,
		LineNo:     presentation.LineNo,
		Gutter:     presentation.Gutter,
		CaretPad:   presentation.CaretPad,
		Text:       text.String(),
		Line:       diagnostic.Line,
		Column:     diagnostic.Column,
		EndLine:    diagnostic.EndLine,
		EndColumn:  diagnostic.EndColumn,
		CaretWidth: presentation.CaretWidth,
	}
}

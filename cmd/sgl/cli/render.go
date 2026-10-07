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

package cli

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"piko.sh/safedisk"

	"seagull.sh/seagull"
	"seagull.sh/seagull/cmd/internal/config"
)

const (
	// maxSourceLineBytes is the longest source line a diagnostic quotes; a longer line is
	// shown as tooLongLine.
	maxSourceLineBytes = 1 << 20

	// tooLongLine stands in for a source line longer than maxSourceLineBytes.
	tooLongLine = "(line too long to show)"

	// tableColumnGap is the number of spaces between the columns of a table.
	tableColumnGap = 2

	// formatText is the human-readable diagnostic format.
	formatText = "text"

	// formatJSON writes one JSON object per diagnostic.
	formatJSON = "json"

	// formatGitHub writes GitHub Actions workflow annotations.
	formatGitHub = "github"

	// formatSARIF writes a SARIF 2.1.0 log.
	formatSARIF = "sarif"
)

// finding is one diagnostic with the file it belongs to resolved.
type finding struct {
	// path is the absolute path of the file, or empty when the diagnostic has none.
	path string

	// display is the file name to print.
	display string

	// database names the database the diagnostic belongs to.
	database string

	// diagnostic is the diagnostic itself.
	diagnostic seagull.SourceError
}

// presentation lays a finding out without its source line, for the formats that print no
// source.
//
// Returns seagull.Presentation which holds the location, severity, message and help.
func (item *finding) presentation() seagull.Presentation {
	return seagull.Present(item.diagnostic, item.display, "")
}

// sourceText is one line of a file a diagnostic may quote.
type sourceText struct {
	// text is the line without its line ending, or empty when it is too long.
	text string

	// tooLong reports that the line is longer than maxSourceLineBytes.
	tooLong bool
}

// jsonFinding is the JSON form of one finding.
type jsonFinding struct {
	// File is the file the diagnostic belongs to.
	File string `json:"file"`

	// Database names the database.
	Database string `json:"database"`

	// Severity is error, warning or hint.
	Severity string `json:"severity"`

	// Code is the diagnostic code, such as Q001.
	Code string `json:"code"`

	// Message describes the problem.
	Message string `json:"message"`

	// Suggestion is the suggested fix, if any.
	Suggestion string `json:"suggestion,omitempty"`

	// Line is one-based, or zero.
	Line int `json:"line"`

	// Column is one-based, or zero.
	Column int `json:"column"`

	// EndLine is one-based, or zero.
	EndLine int `json:"end_line,omitempty"`

	// EndColumn is one-based, or zero.
	EndColumn int `json:"end_column,omitempty"`
}

// findingsFor resolves the files of a database's diagnostics.
//
// Takes database (*config.Database) which produced the diagnostics.
// Takes loaded (*project) which holds the configuration path, used for diagnostics that
// come from the configuration rather than a file.
// Takes diagnostics ([]seagull.SourceError) which are the diagnostics.
//
// Returns []finding which holds one entry per diagnostic, in order.
func (inv *invocation) findingsFor(database *config.Database, loaded *project, diagnostics []seagull.SourceError) []finding {
	findings := make([]finding, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		path := sourcePath(database, diagnostic.Filename)
		display := diagnostic.Filename
		switch {
		case path != "":
			display = inv.display(path)
		case diagnostic.Filename == "":
			display = inv.display(loaded.path)
		}
		diagnostic.Suggestion = cmp.Or(diagnostic.Suggestion, diagnosticHint(diagnostic, inv.program))
		findings = append(findings, finding{path: path, display: display, database: database.Name, diagnostic: diagnostic})
	}
	return findings
}

// countFindings counts findings by severity.
//
// Takes findings ([]finding) which are counted.
//
// Returns errorCount (int) which counts error findings.
// Returns warningCount (int) which counts warning findings.
// Returns hintCount (int) which counts hint findings.
func countFindings(findings []finding) (errorCount, warningCount, hintCount int) {
	for index := range findings {
		switch findings[index].diagnostic.Severity {
		case seagull.SeverityError:
			errorCount++
		case seagull.SeverityWarning:
			warningCount++
		default:
			hintCount++
		}
	}
	return errorCount, warningCount, hintCount
}

// writeFindings renders findings in a format.
//
// Takes writer (io.Writer) which receives the rendering.
// Takes format (string) which is text, json, github or sarif.
// Takes findings ([]finding) which are rendered.
//
// Returns error when the format is unknown or writing fails.
func writeFindings(writer io.Writer, format string, findings []finding) error {
	switch format {
	case formatText:
		return writeText(writer, findings)
	case formatJSON:
		return writeJSONLines(writer, findings)
	case formatGitHub:
		return writeGitHub(writer, findings)
	case formatSARIF:
		return writeSARIF(writer, findings)
	default:
		return usageError("unknown --format %q: want text, json, github or sarif", format)
	}
}

// writeText renders findings for a terminal: location, severity, code and message, then
// the source line with carets under the span, then the help line. A source line longer
// than maxSourceLineBytes is shown as tooLongLine.
//
// Takes writer (io.Writer) which receives the rendering.
// Takes findings ([]finding) which are rendered.
//
// Returns error when writing fails.
func writeText(writer io.Writer, findings []finding) error {
	sources := map[string][]sourceText{}
	presentations := make([]seagull.Presentation, 0, len(findings))
	for index := range findings {
		item := &findings[index]
		line := sourceLine(sources, item.path, item.diagnostic.Line)
		if !line.tooLong {
			presentations = append(presentations, seagull.Present(item.diagnostic, item.display, line.text))
			continue
		}
		presentation := seagull.Present(item.diagnostic, item.display, tooLongLine)
		presentation.CaretPad, presentation.CaretWidth = "", 1
		presentations = append(presentations, presentation)
	}
	return seagull.WriteText(writer, presentations)
}

// sourcePath finds the file a diagnostic names: a query file first, then a migration.
//
// Takes database (*config.Database) which holds the query and migration directories.
// Takes filename (string) which is the diagnostic's file name, relative to its directory.
//
// Returns string which is the absolute path, or empty when neither directory has it.
func sourcePath(database *config.Database, filename string) string {
	if filename == "" {
		return ""
	}
	for _, directory := range []string{database.Queries, database.Migrations} {
		candidate := filepath.Join(directory, filepath.FromSlash(filename))
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

// sourceLine returns one line of a file, reading and caching the file on first use.
//
// Takes cache (map[string][]sourceText) which holds the files read so far.
// Takes path (string) which is the file, or empty.
// Takes line (int) which is one-based.
//
// Returns sourceText which is the line without its line ending, empty when it cannot be
// read.
func sourceLine(cache map[string][]sourceText, path string, line int) sourceText {
	if path == "" || line <= 0 {
		return sourceText{text: "", tooLong: false}
	}
	lines, cached := cache[path]
	if !cached {
		lines = readLines(path)
		cache[path] = lines
	}
	if line > len(lines) {
		return sourceText{text: "", tooLong: false}
	}
	return lines[line-1]
}

// readLines reads the lines of a regular file, through a read-only sandbox rooted at its
// directory, as far as its first maxSourceFileBytes bytes.
//
// Takes path (string) which is the file.
//
// Returns []sourceText which holds the lines, or none when the file cannot be read.
func readLines(path string) []sourceText {
	sandbox, err := safedisk.NewSandbox(filepath.Dir(path), safedisk.ModeReadOnly)
	if err != nil {
		return nil
	}
	defer func() { _ = sandbox.Close() }()
	name := filepath.Base(path)
	if info, statErr := sandbox.Stat(name); statErr != nil || !info.Mode().IsRegular() {
		return nil
	}
	file, err := sandbox.Open(name)
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()
	return scanLines(io.LimitReader(file, maxSourceFileBytes))
}

// scanLines splits text into lines.
//
// A line longer than maxSourceLineBytes is marked too long rather than kept, and the
// lines after it are still read. Reading stops at the end of the text or at the first
// error, keeping the lines read so far.
//
// Takes reader (io.Reader) which supplies the text.
//
// Returns []sourceText which holds the lines without their line endings.
func scanLines(reader io.Reader) []sourceText {
	buffered := bufio.NewReader(reader)
	var lines []sourceText
	var current []byte
	tooLong := false
	for {
		fragment, isPrefix, err := buffered.ReadLine()
		if err != nil {
			return lines
		}
		if !tooLong && len(current)+len(fragment) > maxSourceLineBytes {
			current, tooLong = nil, true
		}
		if !tooLong {
			current = append(current, fragment...)
		}
		if isPrefix {
			continue
		}
		lines = append(lines, sourceText{text: string(current), tooLong: tooLong})
		current, tooLong = current[:0], false
	}
}

// newTable creates the writer every sgl table is laid out with: tab-separated cells,
// padded with spaces into aligned columns. The caller flushes it.
//
// Takes writer (io.Writer) which receives the table.
//
// Returns *tabwriter.Writer which aligns the cells written to it.
func newTable(writer io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(writer, 0, 0, tableColumnGap, ' ', 0)
}

// writeJSONLines renders one JSON object per finding.
//
// Takes writer (io.Writer) which receives the rendering.
// Takes findings ([]finding) which are rendered.
//
// Returns error when encoding fails.
func writeJSONLines(writer io.Writer, findings []finding) error {
	encoder := json.NewEncoder(writer)
	for index := range findings {
		item := &findings[index]
		diagnostic := item.diagnostic
		presented := item.presentation()
		if err := encoder.Encode(jsonFinding{
			File:       item.display,
			Database:   item.database,
			Severity:   presented.Severity,
			Code:       diagnostic.Code,
			Message:    presented.Message,
			Suggestion: diagnostic.Suggestion,
			Line:       diagnostic.Line,
			Column:     diagnostic.Column,
			EndLine:    diagnostic.EndLine,
			EndColumn:  diagnostic.EndColumn,
		}); err != nil {
			return fmt.Errorf("encoding a diagnostic: %w", err)
		}
	}
	return nil
}

// writeGitHub renders findings as GitHub Actions workflow annotations.
//
// Takes writer (io.Writer) which receives the rendering.
// Takes findings ([]finding) which are rendered.
//
// Returns error when writing fails.
func writeGitHub(writer io.Writer, findings []finding) error {
	var buffer bytes.Buffer
	for index := range findings {
		item := &findings[index]
		diagnostic := item.diagnostic
		level := "error"
		switch diagnostic.Severity {
		case seagull.SeverityWarning:
			level = "warning"
		case seagull.SeverityHint:
			level = "notice"
		default:
		}
		properties := []string{"file=" + escapeProperty(item.display)}
		if diagnostic.Line > 0 {
			properties = append(properties, fmt.Sprintf("line=%d", diagnostic.Line))
		}
		if diagnostic.Column > 0 {
			properties = append(properties, fmt.Sprintf("col=%d", diagnostic.Column))
		}
		properties = append(properties, "title="+escapeProperty(diagnostic.Code))
		presented := item.presentation()
		message := presented.Message
		if presented.Help != "" {
			message += "\nhelp: " + presented.Help
		}
		fmt.Fprintf(&buffer, "::%s %s::%s\n", level, strings.Join(properties, ","), escapeData(message))
	}
	_, err := writer.Write(buffer.Bytes())
	return err
}

// escapeData escapes a workflow command's message.
//
// Takes value (string) which is the message.
//
// Returns string which is safe in a workflow command.
func escapeData(value string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(value)
}

// escapeProperty escapes a workflow command's property value.
//
// Takes value (string) which is the value.
//
// Returns string which is safe in a workflow command property.
func escapeProperty(value string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C").Replace(value)
}

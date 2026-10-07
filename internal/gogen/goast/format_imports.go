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

package goast

import (
	"cmp"
	"go/scanner"
	"go/token"
	"slices"
	"strconv"
	"strings"
)

const (
	// importGroupStandard ranks a standard library import, whose first path element holds no
	// dot, first.
	importGroupStandard = 0

	// importGroupThirdParty ranks an import whose first path element holds a dot, such as
	// github.com/google/uuid, after the standard library.
	importGroupThirdParty = 1

	// importGroupAppEngine ranks an import whose path starts with appengine last, as
	// goimports does.
	importGroupAppEngine = 2
)

// importLine is one import specification as it is written back into the source.
type importLine struct {
	// name is the explicit package name, or empty when the import has none.
	name string

	// path is the unquoted import path, used to sort and group the import.
	path string

	// literal is the import path exactly as the source wrote it, quotes included.
	literal string

	// run numbers the blank-line separated run of imports the specification sits in;
	// goimports sorts each run on its own.
	run int
}

// importSection is the run of import declarations at the head of a Go file.
type importSection struct {
	// lines holds every import specification of the run, in source order.
	lines []importLine

	// start is the byte offset of the first import keyword.
	start int

	// end is the byte offset just past the last import declaration.
	end int

	// declarations counts the import declarations in the run.
	declarations int

	// lastLine is the line on which the previous specification of the first declaration
	// ends, or zero before the first specification.
	lastLine int

	// parenthesised reports whether the first declaration is parenthesised.
	parenthesised bool
}

// importScanner reads the import declarations of a Go file token by token.
type importScanner struct {
	// file maps the scanner's positions to byte offsets.
	file *token.File

	// literal is the text of the current token.
	literal string

	// scanner produces the file's tokens, comments included.
	scanner scanner.Scanner

	// position is the position of the current token.
	position token.Pos

	// current is the current token.
	current token.Token
}

// newImportScanner starts an import scanner on the first token of source.
//
// Takes source ([]byte) which is the Go file.
//
// Returns *importScanner which is positioned on the first token.
func newImportScanner(source []byte) *importScanner {
	fileSet := token.NewFileSet()
	reader := &importScanner{
		file:     fileSet.AddFile("", fileSet.Base(), len(source)),
		literal:  "",
		scanner:  scanner.Scanner{},
		position: token.NoPos,
		current:  token.ILLEGAL,
	}
	reader.scanner.Init(reader.file, source, nil, scanner.ScanComments)
	reader.next()
	return reader
}

// readDeclaration reads one import declaration, parenthesised or not, and the semicolon
// that ends it.
//
// Takes section (*importSection) which receives the declaration's specifications.
//
// Returns bool which is false when the tokens do not form a plain import declaration.
func (reader *importScanner) readDeclaration(section *importSection) bool {
	if section.declarations == 0 {
		section.start = reader.offset()
	}
	reader.next()
	if reader.current == token.LPAREN {
		if section.declarations == 0 {
			section.parenthesised = true
		}
		reader.next()
		for reader.current != token.RPAREN {
			if !reader.readSpec(section) {
				return false
			}
			if reader.current == token.SEMICOLON {
				reader.next()
			}
		}
		section.end = reader.offset() + 1
		reader.next()
	} else if !reader.readSpec(section) {
		return false
	}
	section.declarations++
	if reader.current != token.SEMICOLON && reader.current != token.EOF {
		return false
	}
	reader.next()
	return true
}

// next advances to the following token.
func (reader *importScanner) next() {
	reader.position, reader.current, reader.literal = reader.scanner.Scan()
}

// offset returns the byte offset of the current token.
//
// Returns int which is the offset.
func (reader *importScanner) offset() int {
	return reader.file.Offset(reader.position)
}

// line returns the line holding a byte offset.
//
// Takes offset (int) which is the byte offset.
//
// Returns int which is the 1-based line number.
func (reader *importScanner) line(offset int) int {
	return reader.file.Line(reader.file.Pos(offset))
}

// expect consumes the current token when it is the wanted one.
//
// Takes wanted (token.Token) which is the token the grammar requires.
//
// Returns bool which is true when the token matched.
func (reader *importScanner) expect(wanted token.Token) bool {
	if reader.current != wanted {
		return false
	}
	reader.next()
	return true
}

// readSpec reads one import specification, an optional name followed by a quoted path,
// and records where the declaration ends.
//
// A blank line before a specification of the first declaration starts a new run, as it
// does for goimports; the specifications of later declarations join the last run, as
// goimports merges them.
//
// Takes section (*importSection) which receives the specification.
//
// Returns bool which is false when the tokens do not form a plain specification.
func (reader *importScanner) readSpec(section *importSection) bool {
	run := 0
	if count := len(section.lines); count > 0 {
		run = section.lines[count-1].run
	}
	if section.declarations == 0 {
		line := reader.line(reader.offset())
		if section.lastLine > 0 && line > section.lastLine+1 {
			run++
		}
	}
	name := ""
	if reader.current == token.IDENT || reader.current == token.PERIOD {
		name = reader.current.String()
		if reader.current == token.IDENT {
			name = reader.literal
		}
		reader.next()
	}
	if reader.current != token.STRING {
		return false
	}
	path, err := strconv.Unquote(reader.literal)
	if err != nil || path == "C" {
		return false
	}
	section.lines = append(section.lines, importLine{name: name, path: path, literal: reader.literal, run: run})
	section.end = reader.offset() + len(reader.literal)
	section.lastLine = reader.line(section.end - 1)
	reader.next()
	return true
}

// groupImports rewrites the import declarations of printed Go source into the single
// sorted and grouped block that goimports writes in format-only mode.
//
// Every import declaration is merged into one, preserving the first declaration's
// blank-line-separated runs. Within each run, imports are sorted by group (standard
// library, paths whose first element holds a dot, then appengine paths), path and name;
// exact duplicates within the run are dropped. Blank lines separate runs and groups. A
// lone import keeps the unparenthesised form unless its declaration was parenthesised.
//
// Source whose import declarations carry a comment, that imports "C", or whose imports
// cannot be read is returned unchanged, so comments and the cgo preamble keep their
// places and gofmt reports any syntax error; gofmt still sorts such imports.
//
// Takes source ([]byte) which is the printed Go file.
//
// Returns []byte which is the source with its imports regrouped.
func groupImports(source []byte) []byte {
	section, readable := readImportSection(source)
	if !readable || section.declarations == 0 {
		return source
	}

	lines := sortedImportLines(section.lines)
	parenthesised := section.declarations > 1 || section.parenthesised

	var builder strings.Builder
	builder.Grow(len(source))
	builder.Write(source[:section.start])
	writeImportBlock(&builder, lines, parenthesised)
	builder.Write(source[section.end:])
	return []byte(builder.String())
}

// readImportSection scans past the package clause and reads every import declaration that
// follows it.
//
// Takes source ([]byte) which is the Go file.
//
// Returns importSection which describes the import declarations.
// Returns bool which is false when the imports cannot be regrouped as plain text.
func readImportSection(source []byte) (importSection, bool) {
	reader := newImportScanner(source)
	section := importSection{lines: nil, start: 0, end: 0, declarations: 0, lastLine: 0, parenthesised: false}

	for reader.current == token.COMMENT {
		reader.next()
	}
	if !reader.expect(token.PACKAGE) || !reader.expect(token.IDENT) || !reader.expect(token.SEMICOLON) {
		return section, false
	}
	for reader.current == token.IMPORT {
		if !reader.readDeclaration(&section) {
			return section, false
		}
	}
	if section.declarations > 0 && reader.current == token.COMMENT {
		return section, reader.line(reader.offset()) != reader.line(section.end-1)
	}
	return section, true
}

// sortedImportLines sorts each run of imports the way goimports does and drops exact
// duplicates within a run.
//
// Takes lines ([]importLine) which holds the imports in source order.
//
// Returns []importLine which holds the imports in their written order.
func sortedImportLines(lines []importLine) []importLine {
	sorted := slices.Clone(lines)
	slices.SortFunc(sorted, compareImportLines)
	return slices.CompactFunc(sorted, func(left, right importLine) bool {
		return left.run == right.run && left.path == right.path && left.name == right.name
	})
}

// compareImportLines orders two imports by run, then group, then path, then name.
//
// Takes left (importLine) which is the first import.
// Takes right (importLine) which is the second import.
//
// Returns int which is negative, zero or positive as left sorts before, with or after
// right.
func compareImportLines(left, right importLine) int {
	return cmp.Or(
		cmp.Compare(left.run, right.run),
		cmp.Compare(importGroup(left.path), importGroup(right.path)),
		cmp.Compare(left.path, right.path),
		cmp.Compare(left.name, right.name),
	)
}

// importGroup returns the group goimports places an import path in when no local prefix
// is configured.
//
// Takes path (string) which is the unquoted import path.
//
// Returns int which is the group rank.
func importGroup(path string) int {
	if strings.HasPrefix(path, "appengine") {
		return importGroupAppEngine
	}
	firstElement, _, _ := strings.Cut(path, "/")
	if strings.Contains(firstElement, ".") {
		return importGroupThirdParty
	}
	return importGroupStandard
}

// writeImportBlock writes the merged import declaration, with a blank line between runs
// and between groups. An import declaration left with no imports is dropped, as goimports
// drops it.
//
// Takes builder (*strings.Builder) which receives the declaration.
// Takes lines ([]importLine) which are the sorted imports.
// Takes parenthesised (bool) which keeps a lone import inside parentheses.
func writeImportBlock(builder *strings.Builder, lines []importLine, parenthesised bool) {
	if len(lines) == 0 {
		return
	}
	if len(lines) == 1 && !parenthesised {
		builder.WriteString("import ")
		writeImportLine(builder, lines[0])
		return
	}
	builder.WriteString("import (\n")
	for index, line := range lines {
		if index > 0 && (line.run != lines[index-1].run || importGroup(line.path) != importGroup(lines[index-1].path)) {
			builder.WriteByte('\n')
		}
		builder.WriteByte('\t')
		writeImportLine(builder, line)
		builder.WriteByte('\n')
	}
	builder.WriteByte(')')
}

// writeImportLine writes one import specification: its name, when it has one, and its
// quoted path.
//
// Takes builder (*strings.Builder) which receives the specification.
// Takes line (importLine) which is the import to write.
func writeImportLine(builder *strings.Builder, line importLine) {
	if line.name != "" {
		builder.WriteString(line.name)
		builder.WriteByte(' ')
	}
	builder.WriteString(line.literal)
}

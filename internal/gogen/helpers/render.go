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

package helpers

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

var (
	// errInvalidSource is wrapped by every error a source that breaks the directive rules
	// produces.
	errInvalidSource = errors.New("invalid helper source")

	// errMissingData is wrapped by every error a source raises for a flag or value its data
	// does not hold.
	errMissingData = errors.New("missing helper data")
)

// Vocabulary names the identifiers and message text of a generated package; it is
// satisfied by gogen.Vocabulary.
type Vocabulary interface {
	// Helper returns the name of a generated helper with the suffix.
	//
	// Takes suffix (string) which is the helper's name without its prefix.
	//
	// Returns string which is the helper's name.
	Helper(suffix string) string

	// Sentinel returns the name of an unexported generated error with the suffix.
	//
	// Takes suffix (string) which is the error's name without its prefix.
	//
	// Returns string which is the error's name.
	Sentinel(suffix string) string

	// ErrorValue returns the name of an exported generated error with the suffix.
	//
	// Takes suffix (string) which is the error's name without its prefix.
	//
	// Returns string which is the error's name.
	ErrorValue(suffix string) string

	// Message returns generated error text prefixed with the error prefix.
	//
	// Takes text (string) which is the text without its prefix.
	//
	// Returns string which is the prefixed text.
	Message(text string) string

	// DocComments reports whether the rendered file keeps the comments that document the
	// source's declarations, specs and fields.
	//
	// Returns bool which is false when the rendered file leaves them out.
	DocComments() bool
}

// Data is what a source is rendered with.
type Data struct {
	// Flags holds the value of every flag the source's when directives name.
	Flags map[string]bool

	// Values holds the value of every key the source's data and inline directives name: a
	// string, int, rune, bool, []string or Code.
	Values map[string]any

	// Renames maps an identifier of the source to the name it is rendered with, as it is;
	// the vocabulary does not rename it further.
	Renames map[string]string

	// Package is the package name of the rendered file.
	Package string
}

// Code is Go source text a value is written as, as it is, with the imports it needs.
type Code struct {
	// Text is the Go source text.
	Text string

	// Imports lists the import paths Text refers to.
	Imports []string
}

// renderer holds one rendering of a source: the parsed source and the edits that
// specialise it.
type renderer struct {
	// vocabulary names the generated package's identifiers.
	vocabulary Vocabulary

	// data holds the flags, values, renames and package name.
	data *Data

	// fileSet holds the source's positions.
	fileSet *token.FileSet

	// file is the parsed source.
	file *ast.File

	// tokens maps the source's positions to byte offsets.
	tokens *token.File

	// inlined maps the name of every inlined declaration to its value.
	inlined map[string]any

	// source is the helper's Go source.
	source []byte

	// edits holds the edits that specialise the source, sorted by start.
	edits []edit

	// imports lists the import paths the written values need.
	imports []string
}

// newRenderer parses a source for rendering.
//
// Takes source ([]byte) which is the helper's Go source.
// Takes vocabulary (Vocabulary) which names the generated package's identifiers.
// Takes data (*Data) which holds the flags, values, renames and package name.
//
// Returns *renderer which holds the parsed source.
// Returns error when the source does not parse.
func newRenderer(source []byte, vocabulary Vocabulary, data *Data) (*renderer, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "source.go", source, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidSource, err)
	}
	return &renderer{
		vocabulary: vocabulary,
		data:       data,
		fileSet:    fileSet,
		file:       file,
		tokens:     fileSet.File(file.Package),
		inlined:    make(map[string]any),
		source:     source,
		edits:      nil,
		imports:    nil,
	}, nil
}

// plan works out every edit that specialises the source.
//
// Returns error when a directive is malformed or names data the data does not hold.
func (r *renderer) plan() error {
	r.add(edit{start: 0, end: r.offset(r.file.Package), replacement: "", expand: nil})
	r.add(edit{start: r.offset(r.file.Name.Pos()), end: r.offset(r.file.Name.End()), replacement: r.data.Package, expand: nil})
	attached, err := r.planDeclarations()
	if err != nil {
		return err
	}
	if err := r.planComments(attached); err != nil {
		return err
	}
	r.planStatements()
	if err := r.planNames(); err != nil {
		return err
	}
	sortEdits(r.edits)
	return nil
}

// offset returns the byte offset of a position in the source.
//
// Takes position (token.Pos) which is the position.
//
// Returns int which is its byte offset.
func (r *renderer) offset(position token.Pos) int {
	return r.tokens.Offset(position)
}

// lineStart returns the offset of the start of the line an offset is on.
//
// Takes offset (int) which is the offset.
//
// Returns int which is the offset just past the previous newline.
func (r *renderer) lineStart(offset int) int {
	return bytes.LastIndexByte(r.source[:offset], '\n') + 1
}

// lineEnd returns the offset just past the newline that ends the line an offset is on, or
// the end of the source on its last line.
//
// Takes offset (int) which is the offset.
//
// Returns int which is the offset just past the line's newline.
func (r *renderer) lineEnd(offset int) int {
	newline := bytes.IndexByte(r.source[offset:], '\n')
	if newline < 0 {
		return len(r.source)
	}
	return offset + newline + 1
}

// wholeLines returns the range of the lines a node spans, from the start of its first
// line to just past the newline of its last.
//
// Takes start (token.Pos) which is where the node starts.
// Takes end (token.Pos) which is where the node ends.
//
// Returns from (int) which is the start of the first line.
// Returns to (int) which is the end of the last line.
func (r *renderer) wholeLines(start, end token.Pos) (from, to int) {
	return r.lineStart(r.offset(start)), r.lineEnd(r.offset(end) - 1)
}

// add records an edit.
//
// Takes change (edit) which is the edit.
func (r *renderer) add(change edit) {
	r.edits = append(r.edits, change)
}

// File renders a source into a generated file with the name.
//
// Takes name (string) which is the generated file's name.
// Takes source ([]byte) which is the helper's Go source.
// Takes vocabulary (Vocabulary) which names the generated package's identifiers.
// Takes data (*Data) which holds the flags, values, renames and package name.
//
// Returns sqlmodel.GeneratedFile which is the rendered file.
// Returns error when the source is invalid, its data is incomplete or the result does not
// format.
func File(name string, source []byte, vocabulary Vocabulary, data *Data) (sqlmodel.GeneratedFile, error) {
	content, err := render(source, vocabulary, data)
	if err != nil {
		return sqlmodel.GeneratedFile{Name: "", Content: nil}, fmt.Errorf("rendering %s: %w", name, err)
	}
	return sqlmodel.GeneratedFile{Name: name, Content: content}, nil
}

// render specialises a helper source for one generated package and returns the formatted
// Go file.
//
// Takes source ([]byte) which is the helper's Go source.
// Takes vocabulary (Vocabulary) which names the generated package's identifiers.
// Takes data (*Data) which holds the flags, values, renames and package name.
//
// Returns []byte which is the formatted file.
// Returns error when the source is invalid, its data is incomplete or the result does not
// format.
func render(source []byte, vocabulary Vocabulary, data *Data) ([]byte, error) {
	state, err := newRenderer(source, vocabulary, data)
	if err != nil {
		return nil, err
	}
	if err := state.plan(); err != nil {
		return nil, err
	}
	specialised := state.text(0, len(source), nil)
	return finish([]byte(specialised), state.imports)
}

// finish sorts out a specialised source's imports, dropping those it no longer uses and
// adding those its values need, spaces its declarations and formats it.
//
// Declarations are spaced as go/printer spaces declarations it has no positions for: one
// blank line before a declaration with a doc comment or of another kind than the one
// before it, and none otherwise.
//
// Takes specialised ([]byte) which is the source with every edit applied.
// Takes needed ([]string) which lists the import paths the written values need.
//
// Returns []byte which is the formatted file.
// Returns error when the specialised source does not parse or format.
func finish(specialised []byte, needed []string) ([]byte, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "specialised.go", specialised, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parsing the rendered helper: %w", err)
	}
	tokens := fileSet.File(file.Package)
	edits := declarationSpacing(file, tokens, specialised)
	edits = append(edits, settleImports(file, tokens, needed))
	sortEdits(edits)
	settler := &renderer{
		vocabulary: nil,
		data:       nil,
		fileSet:    fileSet,
		file:       file,
		tokens:     tokens,
		inlined:    nil,
		source:     specialised,
		edits:      edits,
		imports:    nil,
	}
	settled := settler.text(0, len(specialised), nil)

	formatted, err := goast.FormatText([]byte(settled))
	if err != nil {
		return nil, fmt.Errorf("formatting the rendered helper: %w", err)
	}
	return formatted, nil
}

// declarationSpacing returns the edits that close every run of blank lines between two
// top-level declarations, so the printer spaces them by its own rules alone.
//
// Takes file (*ast.File) which is the parsed file.
// Takes tokens (*token.File) which maps its positions to offsets.
// Takes source ([]byte) which is the file's text.
//
// Returns []edit which holds one edit per gap holding only white space.
func declarationSpacing(file *ast.File, tokens *token.File, source []byte) []edit {
	var edits []edit
	for index := 1; index < len(file.Decls); index++ {
		start := tokens.Offset(file.Decls[index-1].End())
		next := file.Decls[index].Pos()
		if doc := declarationDoc(file.Decls[index]); doc != nil {
			next = doc.Pos()
		}
		end := tokens.Offset(next)
		if gap := string(source[start:end]); strings.TrimSpace(gap) == "" && strings.Count(gap, "\n") > 1 {
			edits = append(edits, edit{start: start, end: end, replacement: "\n", expand: nil})
		}
	}
	return edits
}

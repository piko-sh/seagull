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
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/printer"
	"go/token"
)

const (
	// maxFormattedFileBytes is the upper limit on a single generated Go file. The cap bounds
	// the source passed to the formatter, and the formatted file.
	maxFormattedFileBytes = 16 << 20

	// printerTabWidth is the tab width the AST printer uses before gofmt normalises the
	// output.
	printerTabWidth = 4
)

// FormatFileWithAST builds a complete Go source file and formats it with FormatSource.
//
// The maxFormattedFileBytes ceiling rejects oversized source before formatting it, and an
// oversized formatted file.
//
// Takes packageName (string) which is the Go package name.
// Takes tracker (*ImportTracker) which holds collected import paths.
// Takes declarations ([]ast.Decl) which are the top-level declarations.
//
// Returns []byte which is the formatted Go source code; the orchestration prepends the
// generated-file header afterwards.
// Returns error when formatting fails.
func FormatFileWithAST(
	packageName string,
	tracker *ImportTracker,
	declarations []ast.Decl,
) ([]byte, error) {
	return FormatDocumentedFile(packageName, "", tracker, declarations)
}

// FormatDocumentedFile builds a complete Go source file whose package clause carries a
// one-line doc comment, and formats it as FormatFileWithAST does.
//
// Takes packageName (string) which is the Go package name.
// Takes packageDoc (string) which is the package doc comment's text, without the comment
// marker; empty writes no doc comment.
// Takes tracker (*ImportTracker) which holds collected import paths.
// Takes declarations ([]ast.Decl) which are the top-level declarations.
//
// Returns []byte which is the formatted Go source code.
// Returns error when the source or the formatted file is longer than the ceiling, or
// formatting fails.
func FormatDocumentedFile(
	packageName string,
	packageDoc string,
	tracker *ImportTracker,
	declarations []ast.Decl,
) ([]byte, error) {
	fileSet := token.NewFileSet()
	file := &ast.File{
		Name:  ast.NewIdent(packageName),
		Decls: declarations,
	}

	tracker.ApplyImports(fileSet, file)

	formatted, err := FormatSource(fileSet, file)
	if err != nil {
		return nil, err
	}

	if packageDoc != "" {
		formatted = append([]byte("// "+packageDoc+"\n"), formatted...)
	}

	if err := checkFileSize(formatted); err != nil {
		return nil, err
	}

	return formatted, nil
}

// FormatSource prints a Go file and formats it the way gofmt and goimports would, without
// adding or removing imports. Every generated Go file is formatted here, so native and
// WebAssembly builds produce the same bytes.
//
// The import tracker has already added every import the declarations use, so all that is
// left of goimports is its layout: groupImports merges, sorts and groups the imports as
// goimports does in format-only mode, and go/format applies gofmt. Neither resolves a
// missing import from the environment, which keeps the output identical on every machine
// and lets the generator run where there is no Go toolchain to ask, such as in a browser,
// without linking goimports' package loader.
//
// Takes fileSet (*token.FileSet) which holds the positions the declarations refer to.
// Takes file (*ast.File) which is the file to print, with its imports already applied.
//
// Returns []byte which is the formatted source.
// Returns error when printing or formatting fails, or the printed source is longer than
// maxFormattedFileBytes, which is refused before it is formatted.
func FormatSource(fileSet *token.FileSet, file *ast.File) ([]byte, error) {
	buffer := new(bytes.Buffer)
	printerConfig := printer.Config{Mode: printer.TabIndent | printer.UseSpaces, Tabwidth: printerTabWidth, Indent: 0}
	if err := printerConfig.Fprint(buffer, fileSet, file); err != nil {
		return nil, fmt.Errorf("printing Go AST: %w", err)
	}
	printed := buffer.Bytes()
	if err := checkFileSize(printed); err != nil {
		return nil, err
	}

	formatted, err := format.Source(groupImports(printed))
	if err != nil {
		return nil, fmt.Errorf("formatting Go AST: %w", err)
	}

	return crossCheckFormat(fileSet, file, printed, formatted)
}

// FormatText formats already written Go source, preserving canonical import grouping. It
// avoids printing a second AST when a helper renderer already has complete text.
//
// Takes source ([]byte) which is the source to format.
//
// Returns []byte which is canonical Go source.
// Returns error when the source is longer than maxFormattedFileBytes, which is refused
// before it is formatted, or parsing, formatting or a formatcheck comparison fails.
func FormatText(source []byte) ([]byte, error) {
	if err := checkFileSize(source); err != nil {
		return nil, err
	}
	formatted, err := format.Source(groupImports(source))
	if err != nil {
		return nil, fmt.Errorf("formatting Go source: %w", err)
	}
	return crossCheckTextFormat(source, formatted)
}

// checkFileSize refuses generated Go source longer than maxFormattedFileBytes.
//
// Takes source ([]byte) which is the source.
//
// Returns error which wraps errGeneratedFileTooLarge when the source is too long, or nil.
func checkFileSize(source []byte) error {
	if len(source) > maxFormattedFileBytes {
		return fmt.Errorf("%w: %d bytes exceed the limit of %d", errGeneratedFileTooLarge, len(source), maxFormattedFileBytes)
	}
	return nil
}

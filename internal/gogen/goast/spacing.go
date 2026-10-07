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
	"cmp"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
)

// SpaceDeclarations writes a blank line between every two top-level declarations of Go
// source that a blank line does not already part, before any comment that comes first.
// The result stays gofmt-clean, since gofmt keeps a single blank line where it finds one.
//
// Takes source ([]byte) which is formatted Go source.
//
// Returns []byte which is the spaced source.
// Returns error when the source is longer than maxFormattedFileBytes or does not parse.
func SpaceDeclarations(source []byte) ([]byte, error) {
	if err := checkFileSize(source); err != nil {
		return nil, err
	}
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "", source, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("spacing declarations: %w", err)
	}
	tokens := fileSet.File(file.Package)
	var breaks []int
	for index := 1; index < len(file.Decls); index++ {
		endLine := tokens.Line(file.Decls[index-1].End())
		next := firstAfterLine(tokens, file.Comments, endLine, file.Decls[index].Pos())
		if line := tokens.Line(next); line == endLine+1 {
			breaks = append(breaks, tokens.Offset(tokens.LineStart(line)))
		}
	}
	if len(breaks) == 0 {
		return source, nil
	}
	var spaced bytes.Buffer
	spaced.Grow(len(source) + len(breaks))
	previous := 0
	for _, offset := range breaks {
		_, _ = spaced.Write(source[previous:offset])
		_ = spaced.WriteByte('\n')
		previous = offset
	}
	_, _ = spaced.Write(source[previous:])
	return spaced.Bytes(), nil
}

// firstAfterLine returns where the first comment that starts after a line and before a
// declaration starts, or the declaration's own position when no comment does.
//
// Takes tokens (*token.File) which maps positions to lines.
// Takes comments ([]*ast.CommentGroup) which are the file's comments, in source order.
// Takes line (int) which is the line the previous declaration ends on.
// Takes declaration (token.Pos) which is where the next declaration starts.
//
// Returns token.Pos which is the position.
func firstAfterLine(tokens *token.File, comments []*ast.CommentGroup, line int, declaration token.Pos) token.Pos {
	first, _ := slices.BinarySearchFunc(comments, line+1, func(group *ast.CommentGroup, target int) int {
		return cmp.Compare(tokens.Line(group.Pos()), target)
	})
	if first < len(comments) && comments[first].Pos() < declaration {
		return comments[first].Pos()
	}
	return declaration
}

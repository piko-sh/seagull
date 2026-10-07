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

package diagbuild

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
)

func TestBlockError(t *testing.T) {
	t.Parallel()

	got := Block("queries/users.sql", 42, "Q010", diag.SeverityError, "failed to parse query")

	assert.Equal(t, diag.SourceError{
		Filename: "queries/users.sql",
		Line:     42,
		Column:   1,
		Message:  "failed to parse query",
		Severity: diag.SeverityError,
		Code:     "Q010",
	}, got)
}

func TestBlockError_warning_severity(t *testing.T) {
	t.Parallel()

	got := Block("file.sql", 10, "Q003", diag.SeverityWarning, "unknown table")

	assert.Equal(t, "file.sql", got.Filename)
	assert.Equal(t, 10, got.Line)
	assert.Equal(t, 1, got.Column)
	assert.Equal(t, diag.SeverityWarning, got.Severity)
	assert.Equal(t, "Q003", got.Code)
	assert.Equal(t, "unknown table", got.Message)
}

func TestLocated(t *testing.T) {
	t.Parallel()

	got := Located("q.sql", TextSpan{Line: 2, Column: 8, EndLine: 2, EndColumn: 12}, "Q001", diag.SeverityWarning,
		`unknown column "nmae"`, "name")

	assert.Equal(t, diag.SourceError{
		Filename:   "q.sql",
		Line:       2,
		Column:     8,
		EndLine:    2,
		EndColumn:  12,
		Message:    `unknown column "nmae"`,
		Severity:   diag.SeverityWarning,
		Code:       "Q001",
		Suggestion: "name",
	}, got)
}

func TestBlockWithSuggestion(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		suggestion string
	}{
		{name: "a suggestion", suggestion: "rename 001_schema.sql to 001_schema.up.sql"},
		{name: "no suggestion", suggestion: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := BlockWithSuggestion("001_schema.sql", 3, "Q048", diag.SeverityError, "001_schema.sql is ignored", testCase.suggestion)

			require.Equal(t, diag.SourceError{
				Filename:   "001_schema.sql",
				Line:       3,
				Column:     1,
				Message:    "001_schema.sql is ignored",
				Severity:   diag.SeverityError,
				Code:       "Q048",
				Suggestion: testCase.suggestion,
			}, got)
		})
	}
}

func TestDiagnosticsOutsideTheDiagnosticPackagesAreBuiltByItsConstructors(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..")
	skipped := []string{".git", "bin", "cmd", "docs", "examples", "hack", "testdata", "tests"}
	owners := []string{filepath.Join("internal", "diag"), filepath.Join("internal", "diagbuild")}
	for _, owner := range owners {
		require.DirExists(t, filepath.Join(root, owner), "the diagnostic package %s has moved; update owners", owner)
	}
	var literals []string
	scanned := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if slices.Contains(skipped, entry.Name()) || slices.Contains(owners, relative) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		scanned++
		found, err := sourceErrorLiterals(path, relative)
		literals = append(literals, found...)
		return err
	})

	require.NoError(t, err)
	require.NotZero(t, scanned, "no Go source file was scanned under %s", root)
	require.Empty(t, literals, "build these diagnostics with a diagbuild constructor")
}

func sourceErrorLiterals(path, relative string) ([]string, error) {
	files := token.NewFileSet()
	file, err := parser.ParseFile(files, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var literals []string
	for node := range ast.Preorder(file) {
		literal, isLiteral := node.(*ast.CompositeLit)
		if !isLiteral {
			continue
		}
		if namesSourceError(literal.Type) && len(literal.Elts) > 0 {
			literals = append(literals, relative+":"+strconv.Itoa(files.Position(literal.Pos()).Line))
		}
		if list, isList := literal.Type.(*ast.ArrayType); isList && namesSourceError(list.Elt) {
			for _, element := range literal.Elts {
				if inner, isInner := element.(*ast.CompositeLit); isInner && inner.Type == nil && len(inner.Elts) > 0 {
					literals = append(literals, relative+":"+strconv.Itoa(files.Position(inner.Pos()).Line))
				}
			}
		}
	}
	return literals, nil
}

func namesSourceError(expression ast.Expr) bool {
	selector, isSelector := expression.(*ast.SelectorExpr)
	if !isSelector {
		return false
	}
	qualifier, isIdentifier := selector.X.(*ast.Ident)
	return isIdentifier && qualifier.Name == "diag" && selector.Sel.Name == "SourceError"
}

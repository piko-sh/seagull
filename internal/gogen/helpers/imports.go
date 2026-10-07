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
	"go/ast"
	"go/token"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"seagull.sh/seagull/internal/gogen/goast"
)

// versionElement matches an import path's major version element, such as v2.
var versionElement = regexp.MustCompile(`^v[0-9]+$`)

// settleImports returns the edit that rewrites a rendered file's import declarations as
// one that holds the imports it still uses and those its written values need, in that
// order. A file left with one import writes it without parentheses, and one left with
// none has no import declaration.
//
// Takes file (*ast.File) which is the parsed rendered file.
// Takes tokens (*token.File) which maps its positions to offsets.
// Takes needed ([]string) which lists the import paths the written values need.
//
// Returns edit which replaces the import declarations, or inserts one after the package
// clause.
func settleImports(file *ast.File, tokens *token.File, needed []string) edit {
	used := qualifiers(file)
	lines, present, start, end := keptImports(file, tokens, used)
	for _, importPath := range needed {
		quoted := strconv.Quote(importPath)
		if !present[quoted] && used[assumedPackageName(importPath)] {
			present[quoted] = true
			lines = append(lines, quoted)
		}
	}
	block := importBlock(lines)
	if start < 0 {
		start = tokens.Offset(file.Name.End())
		if block != "" {
			block = "\n\n" + block
		}
		return edit{start: start, end: start, replacement: block, expand: nil}
	}
	return edit{start: start, end: end, replacement: block, expand: nil}
}

// keptImports lists the imports of a file's import declarations that it uses.
//
// Takes file (*ast.File) which is the parsed file.
// Takes tokens (*token.File) which maps its positions to offsets.
// Takes used (map[string]bool) which holds the qualifiers the file uses.
//
// Returns lines ([]string) which hold the used imports, each as importLine writes it.
// Returns present (map[string]bool) which holds the quoted paths of the used imports.
// Returns start (int) which is the offset of the first import declaration, or -1.
// Returns end (int) which is the offset just past the last import declaration, or -1.
func keptImports(file *ast.File, tokens *token.File, used map[string]bool) (lines []string, present map[string]bool, start, end int) {
	present = make(map[string]bool)
	start, end = -1, -1
	for _, declaration := range file.Decls {
		general, isGeneral := declaration.(*ast.GenDecl)
		if !isGeneral || general.Tok != token.IMPORT {
			continue
		}
		if start < 0 {
			start = tokens.Offset(general.Pos())
		}
		end = tokens.Offset(general.End())
		for _, spec := range general.Specs {
			imported, isImport := spec.(*ast.ImportSpec)
			if isImport && referenced(imported, used) {
				present[imported.Path.Value] = true
				lines = append(lines, importLine(imported))
			}
		}
	}
	return lines, present, start, end
}

// importLine writes one import as it stands in an import declaration.
//
// Takes spec (*ast.ImportSpec) which is the import.
//
// Returns string which is its name, when it has one, and its quoted path.
func importLine(spec *ast.ImportSpec) string {
	if spec.Name != nil {
		return spec.Name.Name + " " + spec.Path.Value
	}
	return spec.Path.Value
}

// importBlock writes an import declaration holding the imports.
//
// Takes lines ([]string) which hold the imports, each as importLine writes it.
//
// Returns string which is the declaration, or empty for no imports.
func importBlock(lines []string) string {
	switch len(lines) {
	case 0:
		return ""
	case 1:
		return "import " + lines[0]
	default:
		return "import (\n\t" + strings.Join(lines, "\n\t") + "\n)"
	}
}

// referenced reports whether a file uses an import: by its name, or by the package name
// assumed from its path when it has none. A blank or dot import always counts.
//
// Takes spec (*ast.ImportSpec) which is the import.
// Takes used (map[string]bool) which holds the qualifiers the file uses.
//
// Returns bool which is true when the file uses the import.
func referenced(spec *ast.ImportSpec, used map[string]bool) bool {
	if spec.Name != nil {
		return spec.Name.Name == "_" || spec.Name.Name == "." || used[spec.Name.Name]
	}
	importPath, err := strconv.Unquote(spec.Path.Value)
	if err != nil {
		return true
	}
	return used[assumedPackageName(importPath)]
}

// qualifiers collects every identifier a file writes before a dot, which covers every use
// of an imported package.
//
// Takes file (*ast.File) which is the file.
//
// Returns map[string]bool which holds the qualifiers.
func qualifiers(file *ast.File) map[string]bool {
	used := make(map[string]bool)
	for qualifier := range goast.SelectorQualifiers(file) {
		used[qualifier] = true
	}
	return used
}

// assumedPackageName returns the package name an import path is assumed to declare: its
// last element, or the one before a major version element, without a go- prefix and cut
// at the first character an identifier cannot hold.
//
// Takes importPath (string) which is the import path.
//
// Returns string which is the assumed package name.
func assumedPackageName(importPath string) string {
	base := path.Base(importPath)
	if versionElement.MatchString(base) {
		base = path.Base(path.Dir(importPath))
	}
	base = strings.TrimPrefix(base, "go-")
	if index := strings.IndexFunc(base, func(character rune) bool {
		return character != '_' && !unicode.IsLetter(character) && !unicode.IsDigit(character)
	}); index >= 0 {
		base = base[:index]
	}
	return base
}

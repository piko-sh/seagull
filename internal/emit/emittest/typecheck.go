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

package emittest

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// goFileExtension ends the name of a Go source file.
	goFileExtension = ".go"
)

// versionSuffix matches the major-version element that ends a module path, such as v5.
var versionSuffix = regexp.MustCompile(`^v[0-9]+$`)

// checkedPackage is a generated package parsed and type-checked.
type checkedPackage struct {
	// fileSet positions the parsed files.
	fileSet *token.FileSet

	// info records what the type checker resolved.
	info *types.Info

	// types is the checked package.
	types *types.Package

	// external maps the import path of every package outside the standard library the files
	// import to true.
	external map[string]bool

	// files holds the parsed Go files, in the order generated.
	files []*ast.File

	// problems lists each parse and type error, located.
	problems []string
}

// position writes where a node sits, as file:line.
//
// Takes node (ast.Node) which is the node.
//
// Returns string which is the position.
func (checked *checkedPackage) position(node ast.Node) string {
	where := checked.fileSet.Position(node.Pos())
	return where.Filename + ":" + strconv.Itoa(where.Line)
}

// typeProblems describes the package's parse and type errors as one problem, or none.
//
// Takes caseName (string) which names the case the package was generated from.
//
// Returns []string which holds the problem, or nothing.
func (checked *checkedPackage) typeProblems(caseName string) []string {
	if len(checked.problems) == 0 {
		return nil
	}
	return []string{"case " + caseName + ": the output does not type-check:\n\t" + strings.Join(checked.problems, "\n\t")}
}

// stubbingImporter imports the standard library from the toolchain's export data and
// stands in an empty package for any other path, so generated code type-checks without
// its driver modules.
type stubbingImporter struct {
	// standard imports the standard library.
	standard types.Importer

	// standIns records the import paths provided by stand-in packages.
	standIns map[string]bool
}

// Import resolves standard-library imports and supplies stand-ins for other packages.
//
// This lets generated code be type-checked without loading every external dependency.
//
// Takes importPath (string) which is the path.
//
// Returns *types.Package which is the package.
// Returns error when a standard library package cannot be imported.
func (stubber *stubbingImporter) Import(importPath string) (*types.Package, error) {
	if isStandardLibrary(importPath) {
		imported, err := stubber.standard.Import(importPath)
		if err == nil || !strings.Contains(importPath, "_") {
			return imported, err
		}
	}
	stubber.standIns[importPath] = true
	stub := types.NewPackage(importPath, stubPackageName(importPath))
	stub.MarkComplete()
	return stub, nil
}

// typeCheck parses and type-checks the Go files of a generation, importing the standard
// library from the toolchain's export data. A selector into a package outside the
// standard library is not a problem, since its stand-in is empty.
//
// Takes files ([]sqlmodel.GeneratedFile) which are the generated files.
//
// Returns *checkedPackage which holds the checked package and its problems.
func typeCheck(files []sqlmodel.GeneratedFile) *checkedPackage {
	fileSet := token.NewFileSet()
	return typeCheckWith(fileSet, importer.ForCompiler(fileSet, "gc", nil), files)
}

// typeCheckWith parses and type-checks the Go files of a generation as typeCheck does,
// importing the standard library through the importer given.
//
// Takes fileSet (*token.FileSet) which positions the parsed files.
// Takes standard (types.Importer) which imports the standard library.
// Takes files ([]sqlmodel.GeneratedFile) which are the generated files.
//
// Returns *checkedPackage which holds the checked package and its problems.
func typeCheckWith(fileSet *token.FileSet, standard types.Importer, files []sqlmodel.GeneratedFile) *checkedPackage {
	checked := &checkedPackage{
		fileSet:  fileSet,
		info:     newInfo(),
		types:    nil,
		external: make(map[string]bool),
		files:    nil,
		problems: nil,
	}
	for _, file := range files {
		if path.Ext(file.Name) != goFileExtension {
			continue
		}
		parsed, err := parser.ParseFile(checked.fileSet, file.Name, file.Content, parser.SkipObjectResolution|parser.ParseComments)
		if err != nil {
			checked.problems = append(checked.problems, "parsing: "+err.Error())
			continue
		}
		checked.files = append(checked.files, parsed)
	}
	stubber := &stubbingImporter{standard: standard, standIns: make(map[string]bool)}
	var typeErrors []string
	config := types.Config{Importer: stubber, Error: func(err error) { typeErrors = append(typeErrors, err.Error()) }}
	checked.types, _ = config.Check(generatedPackage, checked.fileSet, checked.files, checked.info)
	stubbedNames := stubbedLocalNames(checked.files, stubber.standIns)
	for _, problem := range typeErrors {
		if !slices.ContainsFunc(stubbedNames, func(name string) bool { return strings.Contains(problem, "undefined: "+name+".") }) {
			checked.problems = append(checked.problems, problem)
		}
	}
	for importPath := range stubber.standIns {
		checked.external[importPath] = true
	}
	return checked
}

// newInfo returns the type checker's record of what it resolves.
//
// Returns *types.Info which records definitions, uses, selections and types.
func newInfo() *types.Info {
	return &types.Info{
		Types:        make(map[ast.Expr]types.TypeAndValue),
		Instances:    nil,
		Defs:         make(map[*ast.Ident]types.Object),
		Uses:         make(map[*ast.Ident]types.Object),
		Implicits:    nil,
		Selections:   make(map[*ast.SelectorExpr]*types.Selection),
		Scopes:       nil,
		InitOrder:    nil,
		FileVersions: nil,
	}
}

// stubbedLocalNames returns the names the files refer to stand-in packages by.
//
// Takes files ([]*ast.File) which are the parsed files.
// Takes standIns (map[string]bool) which holds the stand-ins' import paths.
//
// Returns []string which holds each local name.
func stubbedLocalNames(files []*ast.File, standIns map[string]bool) []string {
	var names []string
	for _, file := range files {
		for _, specification := range file.Imports {
			importPath, err := strconv.Unquote(specification.Path.Value)
			if err != nil || !standIns[importPath] {
				continue
			}
			name := stubPackageName(importPath)
			if specification.Name != nil {
				name = specification.Name.Name
			}
			names = append(names, name)
		}
	}
	return names
}

// isStandardLibrary reports whether an import path names a standard library package: its
// first element has no dot.
//
// Takes importPath (string) which is the path.
//
// Returns bool which is true for the standard library.
func isStandardLibrary(importPath string) bool {
	first, _, _ := strings.Cut(importPath, "/")
	return !strings.Contains(first, ".")
}

// stubPackageName returns the name a package of an import path is assumed to declare: its
// last element, or the one before a major-version element, without a -go suffix.
//
// Takes importPath (string) which is the path.
//
// Returns string which is the assumed name.
func stubPackageName(importPath string) string {
	elements := strings.Split(importPath, "/")
	name := elements[len(elements)-1]
	if versionSuffix.MatchString(name) && len(elements) > 1 {
		name = elements[len(elements)-2]
	}
	return strings.TrimSuffix(name, "-go")
}

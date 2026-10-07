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

package emitkit

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"maps"
	"slices"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// interfaceFile is the name of the generated file holding the interface of the query
	// methods.
	interfaceFile = "querier_interface.go"

	// functionKeyword starts a printed function type, before its parameters.
	functionKeyword = "func"
)

// interfaceMethod is one method of the interface of the query methods.
type interfaceMethod struct {
	// name is the method's name.
	name string

	// signature is the method's parameters and results as Go source, such as (ctx
	// context.Context) error.
	signature string

	// doc holds the lines of the method's doc comment, each with its comment marker.
	doc []string
}

// importedPackage is a package a query file imports.
type importedPackage struct {
	// path is the package's import path.
	path string

	// spec is the import as the query file writes it, with its alias where it has one.
	spec string
}

// interfaceMethods gathers the query methods of a package's query files, and the imports
// their signatures need.
type interfaceMethods struct {
	// imports maps the import path of each package a signature names to its import as the
	// query files write it.
	imports map[string]string

	// methods holds the methods found, in the order they were read.
	methods []interfaceMethod
}

// read gathers the query methods one query file declares, and the imports their
// signatures need.
//
// Takes file (*sqlmodel.GeneratedFile) which is the query file.
//
// Returns error when the file does not parse or a signature does not print.
func (collected *interfaceMethods) read(file *sqlmodel.GeneratedFile) error {
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, file.Name, file.Content, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("reading the query methods of %s: %w", file.Name, err)
	}
	imported := importedPackages(parsed)
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || !function.Name.IsExported() || !receivesQueries(function) {
			continue
		}
		var signature strings.Builder
		if err := printer.Fprint(&signature, fileSet, function.Type); err != nil {
			return fmt.Errorf("reading the method %s of %s: %w", function.Name.Name, file.Name, err)
		}
		collected.useImports(function.Type, imported)
		collected.methods = append(collected.methods, interfaceMethod{
			name:      function.Name.Name,
			signature: strings.TrimPrefix(signature.String(), functionKeyword),
			doc:       docLines(function.Doc),
		})
	}
	return nil
}

// useImports records the import of every package a signature names.
//
// Takes signature (*ast.FuncType) which is the method's type.
// Takes imported (map[string]importedPackage) which maps each qualifier the query file
// declares to its package.
func (collected *interfaceMethods) useImports(signature *ast.FuncType, imported map[string]importedPackage) {
	for qualifier := range goast.SelectorQualifiers(signature) {
		if found, declared := imported[qualifier]; declared {
			collected.imports[found.path] = found.spec
		}
	}
}

// source writes the interface file's source: its package clause, its imports, the
// interface with its methods by name, and the assertion that *Queries implements it.
//
// Takes packageName (string) which is the generated package's name.
// Takes name (string) which is the interface's name.
//
// Returns string which is the unformatted source.
func (collected *interfaceMethods) source(packageName, name string) string {
	var builder strings.Builder
	builder.WriteString("package " + packageName + "\n\n")
	if len(collected.imports) > 0 {
		builder.WriteString("import (\n")
		for _, importPath := range slices.Sorted(maps.Keys(collected.imports)) {
			builder.WriteString(collected.imports[importPath] + "\n")
		}
		builder.WriteString(")\n\n")
	}
	if len(collected.methods) == 0 {
		builder.WriteString("type " + name + " interface{}\n\n")
	} else {
		builder.WriteString("type " + name + " interface {\n")
		methods := slices.SortedStableFunc(slices.Values(collected.methods), func(left, right interfaceMethod) int {
			return cmp.Compare(left.name, right.name)
		})
		for _, method := range methods {
			for _, line := range method.doc {
				builder.WriteString(line + "\n")
			}
			builder.WriteString(method.name + method.signature + "\n")
		}
		builder.WriteString("}\n\n")
	}
	builder.WriteString("var _ " + name + " = (*" + goast.IdentQueries + ")(nil)\n")
	return builder.String()
}

// QuerierInterface generates querier_interface.go with an interface for exported *Queries
// methods in the planned query files and an implementation assertion. It excludes querier
// and runtime builder methods and leaves the file header for the caller.
//
// Takes packageName (string) which is the generated package's name.
// Takes naming (emit.Naming) which is the package's naming plan, whose Interface names
// the interface, or is empty when the style asks for none.
// Takes files ([]sqlmodel.GeneratedFile) which are the package's generated files, with or
// without their header.
//
// Returns sqlmodel.GeneratedFile which is the file, or the zero file when the plan names
// no interface.
// Returns error when a query file does not parse or the interface does not format.
func QuerierInterface(packageName string, naming emit.Naming, files []sqlmodel.GeneratedFile) (sqlmodel.GeneratedFile, error) {
	if naming.Interface == "" {
		return sqlmodel.GeneratedFile{Name: "", Content: nil}, nil
	}
	queryFiles := make(map[string]bool, len(naming.Files))
	for _, name := range naming.Files {
		queryFiles[name] = true
	}
	collected := &interfaceMethods{imports: make(map[string]string), methods: nil}
	for index := range files {
		if !queryFiles[files[index].Name] {
			continue
		}
		if err := collected.read(&files[index]); err != nil {
			return sqlmodel.GeneratedFile{Name: "", Content: nil}, err
		}
	}
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, interfaceFile, collected.source(packageName, naming.Interface), parser.ParseComments)
	if err != nil {
		return sqlmodel.GeneratedFile{Name: "", Content: nil}, fmt.Errorf("writing the interface %s: %w", naming.Interface, err)
	}
	content, err := goast.FormatSource(fileSet, parsed)
	if err != nil {
		return sqlmodel.GeneratedFile{Name: "", Content: nil}, fmt.Errorf("formatting the interface %s: %w", naming.Interface, err)
	}
	return sqlmodel.GeneratedFile{Name: interfaceFile, Content: content}, nil
}

// importedPackages maps each qualifier a file's imports declare to its package: an
// import's alias, or the name its path implies.
//
// Takes file (*ast.File) which is the parsed file.
//
// Returns map[string]importedPackage which maps the qualifiers.
func importedPackages(file *ast.File) map[string]importedPackage {
	imported := make(map[string]importedPackage, len(file.Imports))
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		qualifier := goast.ImportQualifier(importPath)
		written := spec.Path.Value
		if spec.Name != nil {
			qualifier = spec.Name.Name
			written = spec.Name.Name + " " + spec.Path.Value
		}
		imported[qualifier] = importedPackage{path: importPath, spec: written}
	}
	return imported
}

// receivesQueries reports whether a function is a method on *Queries.
//
// Takes function (*ast.FuncDecl) which is the function.
//
// Returns bool which is true for a method whose receiver is a *Queries.
func receivesQueries(function *ast.FuncDecl) bool {
	if function.Recv == nil || len(function.Recv.List) != 1 {
		return false
	}
	pointer, isPointer := function.Recv.List[0].Type.(*ast.StarExpr)
	if !isPointer {
		return false
	}
	named, isIdentifier := pointer.X.(*ast.Ident)
	return isIdentifier && named.Name == goast.IdentQueries
}

// docLines returns the lines of a doc comment as written, each with its marker.
//
// Takes doc (*ast.CommentGroup) which is the doc comment, or nil.
//
// Returns []string which holds the lines, or nil.
func docLines(doc *ast.CommentGroup) []string {
	if doc == nil {
		return nil
	}
	lines := make([]string, 0, len(doc.List))
	for _, comment := range doc.List {
		lines = append(lines, comment.Text)
	}
	return lines
}

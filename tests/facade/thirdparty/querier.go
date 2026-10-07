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

package thirdparty

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"maps"
	"slices"
	"strconv"
	"strings"

	"seagull.sh/seagull/sdk/emit"
)

const (
	// interfaceFile is the file holding the interface of the query methods.
	interfaceFile = "querier_interface.go"

	// querierSource declares the DBTX interface database/sql's connections and transactions
	// satisfy, the Queries struct holding one connection, New, and the WithTx and RunInTx
	// helpers.
	querierSource = `type DBTX interface {
	ExecContext(ctx __context__.Context, query string, args ...any) (__sql__.Result, error)
	QueryContext(ctx __context__.Context, query string, args ...any) (*__sql__.Rows, error)
	QueryRowContext(ctx __context__.Context, query string, args ...any) *__sql__.Row
}
type Queries struct {
	db DBTX
}
func New(db DBTX) *Queries {
	return &Queries{db: db}
}
func (queries *Queries) WithTx(transaction *__sql__.Tx) *Queries {
	return &Queries{db: transaction}
}
func (queries *Queries) RunInTx(ctx __context__.Context, db *__sql__.DB, fn func(*Queries) error) error {
	transaction, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	if err = fn(queries.WithTx(transaction)); err != nil {
		return err
	}
	return transaction.Commit()
}
`
)

// importedPackage is a package a query file imports.
type importedPackage struct {
	// path is the package's import path.
	path string

	// written is the import as the query file writes it, with its alias where it has one.
	written string
}

// interfaceMethod is one method of the interface of the query methods.
type interfaceMethod struct {
	// name is the method's name.
	name string

	// signature is the method's parameters and results as Go source.
	signature string

	// doc holds the lines of the method's doc comment, each with its marker.
	doc []string
}

// querierFile writes querier.go: the DBTX interface, the Queries struct, New, WithTx and
// RunInTx.
//
// Returns emit.GeneratedFile which is the file.
// Returns bool which is true: every package gets the file.
// Returns error when the file does not format.
func (current *generation) querierFile() (emit.GeneratedFile, bool, error) {
	file := newSource(&current.request.Naming)
	file.write(strings.NewReplacer(
		"__context__", file.qualifier(contextPackage), "__sql__", file.qualifier(sqlPackage),
	).Replace(querierSource))
	generated, err := file.generated(querierFile, current.request)
	return generated, true, err
}

// withInterface adds querier_interface.go when the naming plan names an interface: the
// interface lists the exported Queries methods of the query files by name, each with its
// doc comment, and the file checks that *Queries implements it.
//
// Takes files ([]emit.GeneratedFile) which are the package's other files.
//
// Returns []emit.GeneratedFile which holds them, then the interface's file.
// Returns error when a query file does not parse or the interface does not format.
func (current *generation) withInterface(files []emit.GeneratedFile) ([]emit.GeneratedFile, error) {
	name := current.request.Naming.Interface
	if name == "" {
		return files, nil
	}
	queryFiles := slices.Collect(maps.Values(current.request.Naming.Files))
	imports := make(map[string]string)
	var methods []interfaceMethod
	for index := range files {
		if !slices.Contains(queryFiles, files[index].Name) {
			continue
		}
		found, err := queryMethods(&files[index], imports)
		if err != nil {
			return nil, err
		}
		methods = append(methods, found...)
	}
	slices.SortStableFunc(methods, func(left, right interfaceMethod) int { return cmp.Compare(left.name, right.name) })
	var text strings.Builder
	text.WriteString(current.request.Style.Header + "\n\npackage " + current.request.Package + "\n\n" + importGroups(imports))
	text.WriteString("type " + name + " interface {\n")
	for _, method := range methods {
		for _, line := range method.doc {
			text.WriteString(line + "\n")
		}
		text.WriteString(method.name + method.signature + "\n")
	}
	text.WriteString("}\n\nvar _ " + name + " = (*Queries)(nil)\n")
	formatted, err := format.Source([]byte(text.String()))
	if err != nil {
		return nil, fmt.Errorf("formatting the interface %s: %w", name, err)
	}
	return append(files, emit.GeneratedFile{Name: interfaceFile, Content: formatted}), nil
}

// queryMethods reads the exported Queries methods a query file declares, and records the
// import of each package their signatures name.
//
// Takes file (*emit.GeneratedFile) which is the query file.
// Takes imports (map[string]string) which maps an import path to its import as the query
// files write it; it gains the imports the signatures need.
//
// Returns []interfaceMethod which holds the methods, in the order the file declares them.
// Returns error when the file does not parse or a signature does not print.
func queryMethods(file *emit.GeneratedFile, imports map[string]string) ([]interfaceMethod, error) {
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, file.Name, file.Content, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("reading the query methods of %s: %w", file.Name, err)
	}
	declared := declaredImports(parsed)
	var methods []interfaceMethod
	for _, declaration := range parsed.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction || !function.Name.IsExported() || !receivesQueries(function) {
			continue
		}
		var signature strings.Builder
		if err := printer.Fprint(&signature, fileSet, function.Type); err != nil {
			return nil, fmt.Errorf("reading the method %s of %s: %w", function.Name.Name, file.Name, err)
		}
		for node := range ast.Preorder(function.Type) {
			if imported, found := declared[selectorQualifier(node)]; found {
				imports[imported.path] = imported.written
			}
		}
		methods = append(methods, interfaceMethod{
			name:      function.Name.Name,
			signature: strings.TrimPrefix(signature.String(), "func"),
			doc:       docLines(function.Doc),
		})
	}
	return methods, nil
}

// declaredImports maps each qualifier a file's imports declare to its package: an
// import's alias, or the name its path implies.
//
// Takes file (*ast.File) which is the parsed file.
//
// Returns map[string]importedPackage which maps the qualifiers.
func declaredImports(file *ast.File) map[string]importedPackage {
	declared := make(map[string]importedPackage, len(file.Imports))
	for _, specification := range file.Imports {
		importPath, err := strconv.Unquote(specification.Path.Value)
		if err != nil {
			continue
		}
		qualifier, written := impliedName(importPath), specification.Path.Value
		if specification.Name != nil {
			qualifier, written = specification.Name.Name, specification.Name.Name+" "+specification.Path.Value
		}
		if qualifier != "" {
			declared[qualifier] = importedPackage{path: importPath, written: written}
		}
	}
	return declared
}

// selectorQualifier returns the package a node names a member of, such as sql in
// sql.ErrNoRows.
//
// Takes node (ast.Node) which is the node.
//
// Returns string which is the qualifier, or empty when the node is not a selector on an
// identifier.
func selectorQualifier(node ast.Node) string {
	selector, isSelector := node.(*ast.SelectorExpr)
	if !isSelector {
		return ""
	}
	qualifier, isIdentifier := selector.X.(*ast.Ident)
	if !isIdentifier {
		return ""
	}
	return qualifier.Name
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
	return isIdentifier && named.Name == "Queries"
}

// importGroups writes an import declaration as goimports lays it out: the standard
// library's packages, then the others, each group sorted by path.
//
// Takes imports (map[string]string) which maps each import path to its import as written.
//
// Returns string which is the declaration, or empty when there is no import.
func importGroups(imports map[string]string) string {
	if len(imports) == 0 {
		return ""
	}
	var standard, others []string
	for _, importPath := range slices.Sorted(maps.Keys(imports)) {
		if strings.Contains(strings.SplitN(importPath, "/", 2)[0], ".") {
			others = append(others, imports[importPath])
			continue
		}
		standard = append(standard, imports[importPath])
	}
	groups := make([]string, 0, 2)
	for _, group := range [][]string{standard, others} {
		if len(group) > 0 {
			groups = append(groups, strings.Join(group, "\n"))
		}
	}
	return "import (\n" + strings.Join(groups, "\n\n") + "\n)\n\n"
}

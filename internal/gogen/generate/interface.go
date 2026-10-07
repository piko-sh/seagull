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

package generate

import (
	"cmp"
	"context"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"maps"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/observe"
	"seagull.sh/seagull/internal/sqlmodel"
)

// querierInterface builds the interface from query declarations already in memory. Driver
// helpers supplied only as source text are outside the query method interface.
//
// Takes emission (*gogen.Emission) which holds the package plan, driver and imports.
//
// Returns sqlmodel.GeneratedFile which declares the interface of generated query methods.
// Returns error when the interface cannot be formatted.
func querierInterface(ctx context.Context, emission *gogen.Emission) (sqlmodel.GeneratedFile, error) {
	request := emission.Request
	name := request.Naming.Interface
	if name == "" {
		return sqlmodel.GeneratedFile{Name: "", Content: nil}, nil
	}
	methods := queryMethods(emission.Declarations)
	naming := request.Naming
	naming.Imports = signatureImports(request.Naming.Imports, emission.Qualifiers)
	imports := emitkit.NewFileImports(&naming)
	paths := make(map[string]string, len(naming.Imports))
	for path, qualifier := range naming.Imports {
		paths[qualifier] = path
	}
	var body strings.Builder
	body.WriteString("type " + name + " interface {")
	if len(methods) > 0 {
		body.WriteByte('\n')
	}
	positions := token.NewFileSet()
	for _, method := range methods {
		if method.Doc != nil {
			for _, comment := range method.Doc.List {
				body.WriteString(comment.Text)
				body.WriteByte('\n')
			}
		}
		body.WriteString(method.Name.Name)
		var signature strings.Builder
		if err := printer.Fprint(&signature, positions, method.Type); err != nil {
			return sqlmodel.GeneratedFile{Name: "", Content: nil}, fmt.Errorf("printing interface method %s: %w", method.Name.Name, err)
		}
		body.WriteString(strings.TrimPrefix(signature.String(), "func"))
		body.WriteByte('\n')
		useSignatureImports(method.Type, paths, imports)
	}
	body.WriteString("}\n\nvar _ " + name + " = (*Queries)(nil)\n")
	text := "package " + request.Package + "\n\n" + imports.Declaration() + "\n" + body.String()
	_, end := observe.From(ctx).Start(ctx, observe.StageFormat)
	content, err := goast.FormatText([]byte(text))
	end(err)
	return sqlmodel.GeneratedFile{Name: "querier_interface.go", Content: content}, err
}

// queryMethods selects exported methods on Queries in deterministic order.
//
// Takes declarations ([]ast.Decl) which holds the generated declarations to inspect.
//
// Returns []*ast.FuncDecl which holds the query receiver methods in declaration order.
func queryMethods(declarations []ast.Decl) []*ast.FuncDecl {
	var methods []*ast.FuncDecl
	for _, declaration := range declarations {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || !function.Name.IsExported() || function.Recv == nil || len(function.Recv.List) != 1 {
			continue
		}
		pointer, ok := function.Recv.List[0].Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		receiver, ok := pointer.X.(*ast.Ident)
		if ok && receiver.Name == goast.IdentQueries {
			methods = append(methods, function)
		}
	}
	slices.SortStableFunc(methods, func(left, right *ast.FuncDecl) int { return cmp.Compare(left.Name.Name, right.Name.Name) })

	return methods
}

// signatureImports returns the qualifier of every import a method signature may refer to:
// the naming plan's imports, and those a query file registered under a qualifier of its
// own, such as a standard library package the plan does not hold.
//
// Takes planned (map[string]string) which maps the plan's import paths to qualifiers.
// Takes registered (map[string]string) which maps the qualifiers the query files
// registered to their paths.
//
// Returns map[string]string which maps each path to its qualifier.
func signatureImports(planned, registered map[string]string) map[string]string {
	imports := maps.Clone(planned)
	if imports == nil {
		imports = make(map[string]string, len(registered))
	}
	for _, qualifier := range slices.Sorted(maps.Keys(registered)) {
		if _, found := imports[registered[qualifier]]; !found {
			imports[registered[qualifier]] = qualifier
		}
	}
	return imports
}

// useSignatureImports includes only package qualifiers read by a method signature.
//
// Takes signature (*ast.FuncType) which is the method signature whose type references are
// inspected.
// Takes paths (map[string]string) which maps import identifiers to their package paths.
// Takes imports (*emitkit.FileImports) which records packages needed by the generated
// interface.
func useSignatureImports(signature *ast.FuncType, paths map[string]string, imports *emitkit.FileImports) {
	for qualifier := range goast.SelectorQualifiers(signature) {
		if path, ok := paths[qualifier]; ok {
			imports.Qualifier(path)
		}
	}
}

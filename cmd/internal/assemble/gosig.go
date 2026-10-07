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

package assemble

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"maps"
	"slices"
	"strconv"
	"strings"

	"seagull.sh/seagull/sdk/emit"
)

const (
	// BuilderQueryMethod is the runtime builder method that assembles a runtime-built
	// query's SQL from its constant.
	BuilderQueryMethod = "buildQuery"

	// BuilderCountMethod is the runtime builder method that assembles the count query of a
	// runtime-built query.
	BuilderCountMethod = "buildCountQuery"

	// querierType is the receiver type of the generated query methods.
	querierType = "Queries"
)

// GoIndex holds the declarations of the generated Go files, parsed once, so describing
// any number of queries reads each file a single time. A file that is not Go source, or
// does not parse, is left out.
type GoIndex struct {
	// fileSet positions the parsed files.
	fileSet *token.FileSet

	// methods maps a method name to its declarations, in file order.
	methods map[string][]*ast.FuncDecl

	// types maps a type name to its declaration.
	types map[string]*ast.TypeSpec

	// constants maps each package-level string constant's name to its value.
	constants map[string]string
}

// NewGoIndex parses the Go files among generated files and indexes their methods, types
// and string constants.
//
// Takes files ([]emit.GeneratedFile) which are the generated files.
//
// Returns *GoIndex which indexes their declarations.
func NewGoIndex(files []emit.GeneratedFile) *GoIndex {
	index := &GoIndex{
		fileSet:   token.NewFileSet(),
		methods:   map[string][]*ast.FuncDecl{},
		types:     map[string]*ast.TypeSpec{},
		constants: map[string]string{},
	}
	for _, file := range files {
		if !strings.HasSuffix(file.Name, ".go") {
			continue
		}
		syntax, err := parser.ParseFile(index.fileSet, file.Name, file.Content, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for _, declaration := range syntax.Decls {
			index.add(declaration)
		}
	}
	return index
}

// FileSet returns the file set that positions the indexed declarations.
//
// Returns *token.FileSet which positions every node the index returns.
func (index *GoIndex) FileSet() *token.FileSet {
	return index.fileSet
}

// Method finds a query's generated method by name. A runtime builder declares methods
// such as Count, All and Where, which a query may share its name with, so a method on the
// querier type wins over any other.
//
// Takes name (string) which is the method's name.
//
// Returns *ast.FuncDecl which is the method, or nil when no type declares it.
func (index *GoIndex) Method(name string) *ast.FuncDecl {
	candidates := index.methods[name]
	for _, candidate := range candidates {
		if receiverName(candidate) == querierType {
			return candidate
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return nil
}

// MethodsOf lists the methods declared on a type, in name order.
//
// Takes receiver (string) which is the type's name.
//
// Returns []*ast.FuncDecl which holds the methods.
func (index *GoIndex) MethodsOf(receiver string) []*ast.FuncDecl {
	var found []*ast.FuncDecl
	for _, name := range slices.Sorted(maps.Keys(index.methods)) {
		for _, method := range index.methods[name] {
			if receiverName(method) == receiver {
				found = append(found, method)
			}
		}
	}
	return found
}

// TypeSpec finds a type declaration by name.
//
// Takes name (string) which is the type's name.
//
// Returns *ast.TypeSpec which is the declaration, or nil.
func (index *GoIndex) TypeSpec(name string) *ast.TypeSpec {
	return index.types[name]
}

// Constant returns a package-level string constant's value.
//
// Takes name (string) which is the constant's name.
//
// Returns string which is its value.
// Returns bool which is false when no string constant has the name.
func (index *GoIndex) Constant(name string) (string, bool) {
	value, found := index.constants[name]
	return value, found
}

// SentConstant finds the constant holding the SQL a method sends: the first package-level
// string constant its body names, where a field or method name after a dot does not
// count.
//
// Takes method (*ast.FuncDecl) which is searched.
//
// Returns string which is the constant's name, or empty when the body names none.
func (index *GoIndex) SentConstant(method *ast.FuncDecl) string {
	if method == nil || method.Body == nil {
		return ""
	}
	return firstReference(method.Body, index.constants)
}

// BuilderConstant finds the constant a runtime-built query's builder sends: the one the
// named method of the builder type the query's method returns names, such as the query
// BuilderQueryMethod or the count query BuilderCountMethod assembles.
//
// Takes method (*ast.FuncDecl) which is the query's method.
// Takes builderMethod (string) which names the builder method to search.
//
// Returns string which is the constant's name, or empty when the called method yields no
// builder with that method, or the builder's method names no constant.
func (index *GoIndex) BuilderConstant(method *ast.FuncDecl, builderMethod string) string {
	if method == nil || method.Type.Results == nil || len(method.Type.Results.List) == 0 {
		return ""
	}
	builder := namedType(method.Type.Results.List[0].Type)
	if builder == "" {
		return ""
	}
	for _, candidate := range index.methods[builderMethod] {
		if receiverName(candidate) == builder {
			return index.SentConstant(candidate)
		}
	}
	return ""
}

// Signature prints a query's generated method and the struct types its parameters and
// results use, as Go source: each type declaration, then the method's declaration without
// its body.
//
// Takes name (string) which is the method's name.
//
// Returns string which is the printed declarations, or empty when the method is absent.
func (index *GoIndex) Signature(name string) string {
	declaration := index.Method(name)
	if declaration == nil {
		return ""
	}
	var buffer bytes.Buffer
	for _, typeName := range LocalTypeNames(declaration.Type) {
		spec := index.types[typeName]
		if spec == nil {
			continue
		}
		if _, isStruct := spec.Type.(*ast.StructType); !isStruct {
			continue
		}
		_ = format.Node(&buffer, index.fileSet, &ast.GenDecl{Doc: nil, TokPos: 0, Tok: token.TYPE, Lparen: 0, Specs: []ast.Spec{spec}, Rparen: 0})
		_, _ = buffer.WriteString("\n\n")
	}
	signature := &ast.FuncDecl{Doc: nil, Recv: declaration.Recv, Name: declaration.Name, Type: declaration.Type, Body: nil}
	_ = format.Node(&buffer, index.fileSet, signature)
	return buffer.String()
}

// SQLConst finds the SQL a query's generated method runs: the first package-level string
// constant its body refers to or, for a runtime-built query whose method only returns its
// builder, the first one the builder's BuilderQueryMethod refers to.
//
// Takes name (string) which is the method's name.
//
// Returns identifier (string) which is the constant's name.
// Returns sql (string) which is the constant's value.
// Returns ok (bool) which is false when the method is absent or refers to no string
// constant.
func (index *GoIndex) SQLConst(name string) (identifier, sql string, ok bool) {
	declaration := index.Method(name)
	if declaration == nil {
		return "", "", false
	}
	identifier = index.SentConstant(declaration)
	if identifier == "" {
		identifier = index.BuilderConstant(declaration, BuilderQueryMethod)
	}
	if identifier == "" {
		return "", "", false
	}
	return identifier, index.constants[identifier], true
}

// add indexes one top-level declaration.
//
// Takes declaration (ast.Decl) which is indexed.
func (index *GoIndex) add(declaration ast.Decl) {
	switch typed := declaration.(type) {
	case *ast.FuncDecl:
		if typed.Recv != nil && len(typed.Recv.List) > 0 {
			index.methods[typed.Name.Name] = append(index.methods[typed.Name.Name], typed)
		}
	case *ast.GenDecl:
		for _, spec := range typed.Specs {
			index.addSpec(typed.Tok, spec)
		}
	default:
	}
}

// addSpec indexes a type declaration, or the constants of a constant declaration whose
// values are string literals.
//
// Takes kind (token.Token) which is the declaration's keyword.
// Takes spec (ast.Spec) which is indexed.
func (index *GoIndex) addSpec(kind token.Token, spec ast.Spec) {
	if typeSpec, isType := spec.(*ast.TypeSpec); isType && kind == token.TYPE {
		if _, found := index.types[typeSpec.Name.Name]; !found {
			index.types[typeSpec.Name.Name] = typeSpec
		}
		return
	}
	values, isValue := spec.(*ast.ValueSpec)
	if !isValue || kind != token.CONST || len(values.Names) != len(values.Values) {
		return
	}
	for position, name := range values.Names {
		if value, isString := stringLiteral(values.Values[position]); isString {
			index.constants[name.Name] = value
		}
	}
}

// LocalTypeNames lists the exported identifiers a type expression names, such as the
// local types a method's parameters and results use, leaving out those qualified by a
// package.
//
// Takes node (ast.Node) which is inspected, such as a method's *ast.FuncType.
//
// Returns []string which holds each name once, in order of appearance.
func LocalTypeNames(node ast.Node) []string {
	var names []string
	seen := map[string]bool{}
	ast.Inspect(node, func(child ast.Node) bool {
		if _, isSelector := child.(*ast.SelectorExpr); isSelector {
			return false
		}
		identifier, isIdentifier := child.(*ast.Ident)
		if isIdentifier && identifier.IsExported() && !seen[identifier.Name] {
			seen[identifier.Name] = true
			names = append(names, identifier.Name)
		}
		return true
	})
	return names
}

// receiverName returns the name of a method's receiver type.
//
// Takes method (*ast.FuncDecl) which is a method.
//
// Returns string which is the receiver type's name, or empty for a receiver of any other
// form.
func receiverName(method *ast.FuncDecl) string {
	if method.Recv == nil || len(method.Recv.List) == 0 {
		return ""
	}
	return namedType(method.Recv.List[0].Type)
}

// namedType returns the name of a type written as a name or a pointer to one.
//
// Takes expression (ast.Expr) which is the type expression.
//
// Returns string which is the type's name, or empty for any other type.
func namedType(expression ast.Expr) string {
	if pointer, isPointer := expression.(*ast.StarExpr); isPointer {
		expression = pointer.X
	}
	if identifier, isIdentifier := expression.(*ast.Ident); isIdentifier {
		return identifier.Name
	}
	return ""
}

// firstReference finds the first identifier in a body that names one of a set of
// constants. A field or method name after a dot is not a reference.
//
// Takes body (*ast.BlockStmt) which is searched in source order.
// Takes constants (map[string]string) which holds the constants by name.
//
// Returns string which is the constant's name, or empty when the body names none.
func firstReference(body *ast.BlockStmt, constants map[string]string) string {
	selected := map[*ast.Ident]bool{}
	for node := range ast.Preorder(body) {
		if selector, isSelector := node.(*ast.SelectorExpr); isSelector {
			selected[selector.Sel] = true
		}
	}
	for node := range ast.Preorder(body) {
		identifier, isIdentifier := node.(*ast.Ident)
		if !isIdentifier || selected[identifier] {
			continue
		}
		if _, found := constants[identifier.Name]; found {
			return identifier.Name
		}
	}
	return ""
}

// stringLiteral reads a string literal.
//
// Takes expression (ast.Expr) which may be a string literal.
//
// Returns string which is the literal's value.
// Returns bool which is false when the expression is not a string literal.
func stringLiteral(expression ast.Expr) (string, bool) {
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

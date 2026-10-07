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

package session

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/token"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"seagull.sh/seagull/cmd/internal/assemble"
	"seagull.sh/seagull/sdk/emit"
)

const (
	// rowSuffix ends the name of a query's generated row type.
	rowSuffix = "Row"

	// paramsSuffix ends the name of a query's generated parameter type.
	paramsSuffix = "Params"

	// contextType is the type of a generated method's first parameter.
	contextType = "context.Context"

	// jsonTag is the struct tag key that names a row field's column.
	jsonTag = "json"
)

// goIndex reads what the generated files say about each query: their declarations, parsed
// once per run by the index sgl describe uses and shared by every query view, and how
// their fields' json tags name columns.
type goIndex struct {
	// declarations holds the generated files' methods, types and string constants.
	declarations *assemble.GoIndex

	// tags says how the generated fields' json tags name their columns.
	tags jsonTagStyle
}

// newGoIndex parses the generated Go files. A file that does not parse is skipped.
//
// Takes files ([]emit.GeneratedFile) which are the generated files.
// Takes tags (jsonTagStyle) which says how the fields' json tags name their columns.
//
// Returns *goIndex which indexes their declarations.
func newGoIndex(files []emit.GeneratedFile, tags jsonTagStyle) *goIndex {
	return &goIndex{declarations: assemble.NewGoIndex(files), tags: tags}
}

// describe reads what the generated code says about a query. Its method and the SQL it
// sends are found as sgl describe finds them: a method on the querier type wins over a
// runtime builder's method of the same name.
//
// Takes query (*emit.AnalysedQuery) which is described.
//
// Returns generatedQuery which is empty when the query's method was not generated.
func (index *goIndex) describe(query *emit.AnalysedQuery) generatedQuery {
	method := index.declarations.Method(query.Name)
	if method == nil {
		return generatedQuery{file: "", signature: "", types: "", sql: "", constant: "", countSQL: "", parameterTypes: nil, columnTypes: nil, rowType: "", line: 0}
	}
	position := index.declarations.FileSet().Position(method.Pos())
	constant, sql, found := index.declarations.SQLConst(query.Name)
	if !found {
		constant, sql = "", ""
	}
	rowType := index.rowType(query.Name, method)
	return generatedQuery{
		file:           position.Filename,
		signature:      index.render(&ast.FuncDecl{Doc: nil, Recv: method.Recv, Name: method.Name, Type: method.Type, Body: nil}),
		types:          index.typeDeclarations(query.Name, method, rowType),
		sql:            sql,
		constant:       constant,
		countSQL:       index.constant(index.declarations.BuilderConstant(method, assemble.BuilderCountMethod)),
		parameterTypes: index.parameterTypes(method),
		columnTypes:    index.columnTypes(query, rowType),
		rowType:        rowType,
		line:           position.Line,
	}
}

// constant returns the value of one of the generated code's string constants.
//
// Takes name (string) which is the constant's name, or empty.
//
// Returns string which is its value, or empty when no string constant has the name.
func (index *goIndex) constant(name string) string {
	value, found := index.declarations.Constant(name)
	if !found {
		return ""
	}
	return value
}

// rowType identifies the struct returned by a query or its builder, preferring the
// query's own row type.
//
// For a struct without exported fields, such as a runtime builder, it searches the types
// returned by its methods. Each type is examined once, so chaining methods that return
// the same builder cannot cause a loop.
//
// Takes name (string) which is the query's name.
// Takes method (*ast.FuncDecl) which is the query's method.
//
// Returns string which is the struct's name, or empty when the query returns no rows.
func (index *goIndex) rowType(name string, method *ast.FuncDecl) string {
	if index.structType(name+rowSuffix) != nil {
		return name + rowSuffix
	}
	pending := index.returnedTypes(method)
	visited := map[string]bool{}
	for len(pending) > 0 {
		candidate := pending[0]
		pending = pending[1:]
		if visited[candidate] {
			continue
		}
		visited[candidate] = true
		structure := index.structType(candidate)
		switch {
		case structure == nil || candidate == name+paramsSuffix:
		case hasExportedField(structure):
			return candidate
		default:
			for _, terminal := range index.declarations.MethodsOf(candidate) {
				pending = append(pending, index.returnedTypes(terminal)...)
			}
		}
	}
	return ""
}

// returnedTypes lists the local exported type names a method's results name.
//
// Takes method (*ast.FuncDecl) which is inspected.
//
// Returns []string which holds the names in order of appearance.
func (*goIndex) returnedTypes(method *ast.FuncDecl) []string {
	if method.Type.Results == nil {
		return nil
	}
	return assemble.LocalTypeNames(method.Type.Results)
}

// parameterTypes lists the Go type of each query parameter, by position: the method's
// parameters after the context, or the fields of its parameter struct when it takes one.
//
// Takes method (*ast.FuncDecl) which is the query's method.
//
// Returns []string which holds the rendered types.
func (index *goIndex) parameterTypes(method *ast.FuncDecl) []string {
	fields := expandFields(method.Type.Params)
	if len(fields) > 0 && index.render(fields[0].expression) == contextType {
		fields = fields[1:]
	}
	if len(fields) == 1 {
		name := typeName(fields[0].expression)
		if structure := index.structType(name); structure != nil && strings.HasSuffix(name, paramsSuffix) {
			fields = expandFields(structure.Fields)
		}
	}
	types := make([]string, 0, len(fields))
	for _, field := range fields {
		types = append(types, index.render(field.expression))
	}
	return types
}

// columnTypes lists the Go type of each result column, by position, from the json tags of
// the struct the query's rows are read into and of the structs its sgl.embed groups
// become. Where the fields carry no json tags, they are matched by name, as namedTypes
// describes.
//
// Takes query (*emit.AnalysedQuery) which holds the columns.
// Takes rowType (string) which names the struct the rows are read into, or is empty.
//
// Returns []string which holds the rendered types, empty where a column's field is not
// found.
func (index *goIndex) columnTypes(query *emit.AnalysedQuery, rowType string) []string {
	row := index.structType(rowType)
	if row == nil {
		return nil
	}
	if index.tags.omitted {
		return index.namedTypes(query, row)
	}
	tagged, embeds := splitRowFields(expandFields(row.Fields))
	groups := map[string]map[string][]ast.Expr{}
	types := make([]string, 0, len(query.OutputColumns))
	for position := range query.OutputColumns {
		column := &query.OutputColumns[position]
		fields := tagged
		if column.IsEmbedded {
			fields = index.embedGroup(groups, embeds, embedName(column))
		}
		types = append(types, index.takeField(fields, index.tags.name(column.Name)))
	}
	return types
}

// namedTypes matches result columns to untagged row fields and returns their Go types,
// ignoring case and underscores when comparing names.
//
// Takes query (*emit.AnalysedQuery) which holds the columns.
// Takes row (*ast.StructType) which is the struct the rows are read into.
//
// Returns []string which holds the rendered types, empty where no field matches, or nil
// when the query has an sgl.embed group, whose fields cannot be told apart this way.
func (index *goIndex) namedTypes(query *emit.AnalysedQuery, row *ast.StructType) []string {
	if slices.ContainsFunc(query.OutputColumns, func(column emit.OutputColumn) bool { return column.IsEmbedded }) {
		return nil
	}
	fields := map[string][]ast.Expr{}
	for _, field := range expandFields(row.Fields) {
		key := comparableName(field.name)
		fields[key] = append(fields[key], field.expression)
	}
	types := make([]string, 0, len(query.OutputColumns))
	for position := range query.OutputColumns {
		types = append(types, index.takeField(fields, comparableName(query.OutputColumns[position].Name)))
	}
	return types
}

// embedGroup returns the tagged fields of the struct an sgl.embed group becomes. Groups
// take the row's untagged fields in the order the groups first appear among the columns.
//
// Takes groups (map[string]map[string][]ast.Expr) which caches the groups seen so far.
// Takes embeds ([]goField) which holds the row's untagged fields.
// Takes name (string) which names the group, as embedName does.
//
// Returns map[string][]ast.Expr which maps each column name to its field types.
func (index *goIndex) embedGroup(groups map[string]map[string][]ast.Expr, embeds []goField, name string) map[string][]ast.Expr {
	if fields, found := groups[name]; found {
		return fields
	}
	fields := map[string][]ast.Expr{}
	if position := len(groups); position < len(embeds) {
		if structure := index.structType(typeName(embeds[position].expression)); structure != nil {
			fields, _ = splitRowFields(expandFields(structure.Fields))
		}
	}
	groups[name] = fields
	return fields
}

// takeField renders and removes the first field tagged with a column's name, so a
// repeated column name takes the next field.
//
// Takes fields (map[string][]ast.Expr) which maps column names to field types.
// Takes column (string) which is the column's name.
//
// Returns string which is the rendered type, or empty when no field is left.
func (index *goIndex) takeField(fields map[string][]ast.Expr, column string) string {
	remaining := fields[column]
	if len(remaining) == 0 {
		return ""
	}
	fields[column] = remaining[1:]
	return index.render(remaining[0])
}

// typeDeclarations renders the declarations of the local types a method takes and
// returns, with the struct its rows are read into, the query's parameter type and the
// types their exported fields use. Structs with no exported or embedded field, such as a
// runtime builder, are left out.
//
// Takes name (string) which is the query's name.
// Takes method (*ast.FuncDecl) which is the query's method.
// Takes rowType (string) which names the struct the rows are read into, or is empty.
//
// Returns string which holds the declarations, separated by blank lines.
func (index *goIndex) typeDeclarations(name string, method *ast.FuncDecl, rowType string) string {
	pending := append(assemble.LocalTypeNames(method.Type), rowType, name+paramsSuffix)
	seen := map[string]bool{}
	var rendered []string
	for len(pending) > 0 {
		current := pending[0]
		pending = pending[1:]
		spec := index.declarations.TypeSpec(current)
		if seen[current] || spec == nil {
			continue
		}
		seen[current] = true
		structure, isStruct := spec.Type.(*ast.StructType)
		if isStruct && !hasExportedField(structure) {
			continue
		}
		if isStruct {
			pending = append(pending, exportedFieldTypes(structure)...)
		}
		rendered = append(rendered, index.render(&ast.GenDecl{Doc: nil, TokPos: 0, Tok: token.TYPE, Lparen: 0, Specs: []ast.Spec{spec}, Rparen: 0}))
	}
	return strings.Join(rendered, "\n\n")
}

// structType finds a struct type by name.
//
// Takes name (string) which is the type's name.
//
// Returns *ast.StructType which is the struct, or nil when there is no such struct.
func (index *goIndex) structType(name string) *ast.StructType {
	spec := index.declarations.TypeSpec(name)
	if spec == nil {
		return nil
	}
	structure, isStruct := spec.Type.(*ast.StructType)
	if !isStruct {
		return nil
	}
	return structure
}

// render prints a syntax node as gofmt would.
//
// Takes node (any) which is an ast.Node.
//
// Returns string which is the printed node, or empty when it cannot be printed.
func (index *goIndex) render(node any) string {
	var buffer bytes.Buffer
	if err := format.Node(&buffer, index.declarations.FileSet(), node); err != nil {
		return ""
	}
	return buffer.String()
}

// jsonTagStyle says how the generated fields' json tags name the columns they hold, so a
// column's field can be found by its tag.
type jsonTagStyle struct {
	// name returns the name a column's json tag carries, as go.json_tags.case writes it.
	name func(column string) string

	// omitted reports that the fields carry no json tags, as go.json_tags.omit asks.
	omitted bool
}

// generatedQuery is what the generated code says about one query.
type generatedQuery struct {
	// file is the generated file holding the method, or empty.
	file string

	// signature is the method's signature, or empty.
	signature string

	// types holds the declarations of the types the generated method accepts and returns.
	types string

	// sql is the SQL the method sends, or empty when it cannot be found.
	sql string

	// constant names the constant holding sql, or is empty.
	constant string

	// countSQL is the SQL a runtime-built query's Count sends before its filters are added,
	// or empty for any other query.
	countSQL string

	// rowType names the struct each row is read into: the query's own row type, a table's
	// model or a row type queries share; it is empty when the query returns no rows.
	rowType string

	// parameterTypes holds the Go type of each parameter by position.
	parameterTypes []string

	// columnTypes holds the Go type of each result column by position.
	columnTypes []string

	// line is the method's one-based line, or zero.
	line int
}

// goField is one field or parameter of a declaration: a name and its type.
type goField struct {
	// expression is the field's type.
	expression ast.Expr

	// tag is the field's struct tag without quotes, or empty.
	tag reflect.StructTag

	// name is the field's name, or empty for an embedded or unnamed field.
	name string
}

// comparableName returns a name in lower case without underscores, so a column's name and
// the field the generated code names after it compare equal.
//
// Takes name (string) which is a column's or a field's name.
//
// Returns string which is the comparable form.
func comparableName(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, "_", ""))
}

// expandFields lists the fields of a field list, one per name.
//
// Takes list (*ast.FieldList) which is a parameter list or struct body, or nil.
//
// Returns []goField which holds one entry per name, or one per unnamed field.
func expandFields(list *ast.FieldList) []goField {
	if list == nil {
		return nil
	}
	var fields []goField
	for _, field := range list.List {
		tag := reflect.StructTag("")
		if field.Tag != nil {
			if unquoted, err := strconv.Unquote(field.Tag.Value); err == nil {
				tag = reflect.StructTag(unquoted)
			}
		}
		if len(field.Names) == 0 {
			fields = append(fields, goField{expression: field.Type, tag: tag, name: ""})
			continue
		}
		for _, name := range field.Names {
			fields = append(fields, goField{expression: field.Type, tag: tag, name: name.Name})
		}
	}
	return fields
}

// splitRowFields splits a row struct's fields into those a json tag names, keyed by the
// tag's name in field order, and the untagged ones an sgl.embed group becomes.
//
// Takes fields ([]goField) which are the struct's fields.
//
// Returns tagged (map[string][]ast.Expr) which maps each tag name to its field types.
// Returns embeds ([]goField) which holds the untagged fields in order.
func splitRowFields(fields []goField) (tagged map[string][]ast.Expr, embeds []goField) {
	tagged = map[string][]ast.Expr{}
	for _, field := range fields {
		name, _, _ := strings.Cut(field.tag.Get(jsonTag), ",")
		if name == "" || name == "-" {
			embeds = append(embeds, field)
			continue
		}
		tagged[name] = append(tagged[name], field.expression)
	}
	return tagged, embeds
}

// exportedFieldTypes lists the local type names a struct's exported fields use.
//
// Takes structure (*ast.StructType) which is inspected.
//
// Returns []string which holds the names in order.
func exportedFieldTypes(structure *ast.StructType) []string {
	var names []string
	for _, field := range expandFields(structure.Fields) {
		if field.name == "" || token.IsExported(field.name) {
			names = append(names, assemble.LocalTypeNames(field.expression)...)
		}
	}
	return names
}

// hasExportedField reports whether a struct has an exported or embedded field.
//
// Takes structure (*ast.StructType) which is inspected.
//
// Returns bool which is true when a caller can see any of its fields.
func hasExportedField(structure *ast.StructType) bool {
	for _, field := range expandFields(structure.Fields) {
		if field.name == "" || token.IsExported(field.name) {
			return true
		}
	}
	return false
}

// typeName returns the local type name a type expression names, looking through pointers
// and slices.
//
// Takes expression (ast.Expr) which is a type.
//
// Returns string which is the name, or empty for any other type.
func typeName(expression ast.Expr) string {
	for {
		switch typed := expression.(type) {
		case *ast.Ident:
			return typed.Name
		case *ast.StarExpr:
			expression = typed.X
		case *ast.ArrayType:
			expression = typed.Elt
		default:
			return ""
		}
	}
}

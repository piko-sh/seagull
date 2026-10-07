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
	"bytes"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// contextPackage is the import path of the context package.
	contextPackage = "context"

	// contextType is the name of context.Context.
	contextType = "Context"
)

// queryMethod is one method of the generated package that runs a query: an exported
// method whose first parameter is a context.Context.
type queryMethod struct {
	// declaration is the method's declaration.
	declaration *ast.FuncDecl

	// function is the method's object.
	function *types.Func
}

// queryMethods lists the package's query methods, in source order.
//
// Returns []queryMethod which holds the methods.
func (checked *checkedPackage) queryMethods() []queryMethod {
	var methods []queryMethod
	for _, file := range checked.files {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv == nil || !function.Name.IsExported() || function.Body == nil {
				continue
			}
			object, ok := checked.info.Defs[function.Name].(*types.Func)
			if !ok {
				continue
			}
			parameters := object.Signature().Params()
			if parameters.Len() == 0 || !isContext(parameters.At(0).Type()) {
				continue
			}
			methods = append(methods, queryMethod{declaration: function, function: object})
		}
	}
	return methods
}

// fieldUses counts how often each struct field is selected, and how often a composite
// literal sets it.
//
// Returns map[*types.Var]int which counts selections.
// Returns map[*types.Var]int which counts composite literal keys.
func (checked *checkedPackage) fieldUses() (selected, keyed map[*types.Var]int) {
	selected = make(map[*types.Var]int)
	for _, selection := range checked.info.Selections {
		if field, ok := selection.Obj().(*types.Var); ok && selection.Kind() == types.FieldVal {
			selected[field]++
		}
	}
	keyed = make(map[*types.Var]int)
	for _, object := range checked.info.Uses {
		if field, ok := object.(*types.Var); ok && field.IsField() {
			keyed[field]++
		}
	}
	return selected, keyed
}

// unboundParameters reports each query method argument the method never uses, and each
// field of a parameter struct nothing reads.
//
// Returns []string which describes each problem.
func (checked *checkedPackage) unboundParameters() []string {
	selected, _ := checked.fieldUses()
	var problems []string
	for _, method := range checked.queryMethods() {
		used := checked.usedObjects(method.declaration.Body)
		parameters := method.function.Signature().Params()
		for index := 1; index < parameters.Len(); index++ {
			problems = append(problems, checked.unboundParameter(method, parameters.At(index), used, selected)...)
		}
	}
	return slices.Compact(problems)
}

// unboundParameter reports whether a method never uses one of its arguments, and each
// field of the argument's struct nothing reads.
//
// Takes method (queryMethod) which is the method.
// Takes parameter (*types.Var) which is the argument.
// Takes used (map[types.Object]bool) which holds the objects the method's body uses.
// Takes selected (map[*types.Var]int) which counts each field's selections.
//
// Returns []string which describes each problem.
func (checked *checkedPackage) unboundParameter(method queryMethod, parameter *types.Var, used map[types.Object]bool,
	selected map[*types.Var]int) []string {
	var problems []string
	where := checked.position(method.declaration)
	if parameter.Name() != "_" && !used[parameter] {
		problems = append(problems, fmt.Sprintf("%s: %s never uses its argument %s", where, method.function.Name(), parameter.Name()))
	}
	named, structure, ok := checked.packageStruct(dereference(parameter.Type()))
	if !ok || checked.usedWhole(method.declaration.Body, parameter) {
		return problems
	}
	for field := range structure.Fields() {
		if selected[field] == 0 {
			problems = append(problems, fmt.Sprintf("%s: %s never reads %s.%s", where, method.function.Name(), named.Obj().Name(), field.Name()))
		}
	}
	return problems
}

// usedWhole reports whether a node uses an object other than to select a field of it,
// such as a value of a named struct type handed to a helper whole, whose fields the
// helper reads.
//
// Takes node (ast.Node) which is the node to search.
// Takes object (types.Object) which is the object.
//
// Returns bool which is true when some use is not the operand of a selector.
func (checked *checkedPackage) usedWhole(node ast.Node, object types.Object) bool {
	selecting := make(map[*ast.Ident]bool)
	whole := false
	for child := range ast.Preorder(node) {
		switch typed := child.(type) {
		case *ast.SelectorExpr:
			if identifier, ok := typed.X.(*ast.Ident); ok {
				selecting[identifier] = true
			}
		case *ast.Ident:
			whole = whole || checked.info.Uses[typed] == object && !selecting[typed]
		}
	}
	return whole
}

// filledWhole collects the fields a package fills as a whole value: taken by address, as
// a scan target is, or assigned whole, so a decoder or the driver fills the fields of
// their struct type.
//
// Returns map[*types.Var]bool which holds the fields.
func (checked *checkedPackage) filledWhole() map[*types.Var]bool {
	filled := make(map[*types.Var]bool)
	for _, file := range checked.files {
		for child := range ast.Preorder(file) {
			switch typed := child.(type) {
			case *ast.UnaryExpr:
				if typed.Op == token.AND {
					checked.markField(filled, typed.X)
				}
			case *ast.AssignStmt:
				for _, left := range typed.Lhs {
					checked.markField(filled, left)
				}
			}
		}
	}
	return filled
}

// markField records the field an expression selects, if it selects one.
//
// Takes filled (map[*types.Var]bool) which gathers the fields.
// Takes expression (ast.Expr) which is the expression.
func (checked *checkedPackage) markField(filled map[*types.Var]bool, expression ast.Expr) {
	selector, ok := ast.Unparen(expression).(*ast.SelectorExpr)
	if !ok {
		return
	}
	if field, ok := checked.info.Uses[selector.Sel].(*types.Var); ok && field.IsField() {
		filled[field] = true
	}
}

// usedObjects collects every object an identifier within a node refers to.
//
// Takes node (ast.Node) which is the node to search.
//
// Returns map[types.Object]bool which holds each object used.
func (checked *checkedPackage) usedObjects(node ast.Node) map[types.Object]bool {
	used := make(map[types.Object]bool)
	for child := range ast.Preorder(node) {
		if identifier, ok := child.(*ast.Ident); ok {
			if object := checked.info.Uses[identifier]; object != nil {
				used[object] = true
			}
		}
	}
	return used
}

// collectRows gathers generated row structs from a result type.
//
// It follows containers and row fields but excludes structs with methods and values
// filled whole by a driver or decoder.
//
// Takes candidate (types.Type) which is the type.
// Takes rows (map[*types.Named]*types.Struct) which gathers the rows and their structure.
// Takes whole (map[*types.Var]bool) which holds the fields filled whole.
func (checked *checkedPackage) collectRows(candidate types.Type, rows map[*types.Named]*types.Struct, whole map[*types.Var]bool) {
	switch shape := types.Unalias(candidate).(type) {
	case *types.Pointer:
		checked.collectRows(shape.Elem(), rows, whole)
	case *types.Slice:
		checked.collectRows(shape.Elem(), rows, whole)
	case *types.Signature:
		for parameter := range shape.Params().Variables() {
			checked.collectRows(parameter.Type(), rows, whole)
		}
	case *types.Named:
		structure, ok := shape.Underlying().(*types.Struct)
		if !ok || shape.Obj().Pkg() != checked.types || shape.NumMethods() > 0 || rows[shape] != nil {
			return
		}
		rows[shape] = structure
		for field := range structure.Fields() {
			if !whole[field] {
				checked.collectRows(field.Type(), rows, whole)
			}
		}
	}
}

// packageStruct returns a type as a struct the package declares.
//
// Takes candidate (types.Type) which is the type.
//
// Returns *types.Named which is the named type.
// Returns *types.Struct which is its structure.
// Returns bool which is false when the type is not a struct of the package.
func (checked *checkedPackage) packageStruct(candidate types.Type) (*types.Named, *types.Struct, bool) {
	named, ok := types.Unalias(candidate).(*types.Named)
	if !ok || named.Obj().Pkg() != checked.types {
		return nil, nil, false
	}
	structure, ok := named.Underlying().(*types.Struct)
	return named, structure, ok
}

// isContext reports whether a type is context.Context.
//
// Takes candidate (types.Type) which is the type.
//
// Returns bool which is true for context.Context.
func isContext(candidate types.Type) bool {
	named, ok := types.Unalias(candidate).(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == contextPackage && named.Obj().Name() == contextType
}

// bindingProblems reports each unused argument and unread parameter field of a package,
// or nothing for a package that was not generated.
//
// Takes checked (*checkedPackage) which is the package, or nil.
//
// Returns []string which describes each problem.
func bindingProblems(checked *checkedPackage) []string {
	if checked == nil {
		return nil
	}
	return checked.unboundParameters()
}

// rowProblems reports each field of a row type a query method returns that nothing fills:
// neither a selection, such as a scan target, nor a composite literal sets it.
//
// Takes checked (*checkedPackage) which is the package, or nil.
//
// Returns []string which describes each problem.
func rowProblems(checked *checkedPackage) []string {
	if checked == nil {
		return nil
	}
	selected, keyed := checked.fieldUses()
	rows := make(map[*types.Named]*types.Struct)
	whole := checked.filledWhole()
	for _, method := range checked.queryMethods() {
		for result := range method.function.Signature().Results().Variables() {
			checked.collectRows(result.Type(), rows, whole)
		}
	}
	var problems []string
	for row, structure := range rows {
		for field := range structure.Fields() {
			if selected[field] == 0 && keyed[field] == 0 {
				problems = append(problems, fmt.Sprintf("%s: nothing fills %s.%s",
					checked.fileSet.Position(field.Pos()), row.Obj().Name(), field.Name()))
			}
		}
	}
	slices.Sort(problems)
	return problems
}

// dereference removes pointers and slices from a type.
//
// Takes candidate (types.Type) which is the type.
//
// Returns types.Type which is the innermost element type.
func dereference(candidate types.Type) types.Type {
	for {
		switch shape := types.Unalias(candidate).(type) {
		case *types.Pointer:
			candidate = shape.Elem()
		case *types.Slice:
			candidate = shape.Elem()
		default:
			return candidate
		}
	}
}

// stringConstants returns the value of every package-level string constant.
//
// Takes checked (*checkedPackage) which is the type-checked generated package.
//
// Returns []string which holds the values.
func stringConstants(checked *checkedPackage) []string {
	if checked == nil || checked.types == nil {
		return nil
	}
	var values []string
	scope := checked.types.Scope()
	for _, name := range scope.Names() {
		value, ok := scope.Lookup(name).(*types.Const)
		if ok && value.Val().Kind() == constant.String {
			values = append(values, constant.StringVal(value.Val()))
		}
	}
	return values
}

// placeholderProblems reports each SQL constant that writes a placeholder in a form other
// than the dialect's execution or positional form, writes a numbered placeholder without
// its number or a named one without the name prefix, or mixes forms.
//
// Takes checked (*checkedPackage) which is the package, or nil.
// Takes info (*dialect.Info) which describes the dialect.
//
// Returns []string which describes each problem.
func placeholderProblems(checked *checkedPackage, info *dialect.Info) []string {
	if checked == nil || checked.types == nil {
		return nil
	}
	var problems []string
	scope := checked.types.Scope()
	for _, name := range scope.Names() {
		value, ok := scope.Lookup(name).(*types.Const)
		if !ok || !isPlainString(value.Type()) || value.Val().Kind() != constant.String {
			continue
		}
		for _, problem := range sqlPlaceholderProblems(constant.StringVal(value.Val()), info) {
			problems = append(problems, fmt.Sprintf("%s: the SQL constant %s %s", checked.fileSet.Position(value.Pos()), name, problem))
		}
	}
	return problems
}

// isPlainString reports whether a constant's type is string or untyped string, the type a
// SQL constant has, rather than a named string type such as a sort column.
//
// Takes candidate (types.Type) which is the constant's type.
//
// Returns bool which is true for string and untyped string.
func isPlainString(candidate types.Type) bool {
	basic, ok := candidate.(*types.Basic)
	return ok && (basic.Kind() == types.String || basic.Kind() == types.UntypedString)
}

// sqlPlaceholderProblems checks the placeholders of one SQL text. Text that does not lex,
// or holds no word or no placeholder, is not SQL with parameters and passes.
//
// Takes text (string) which is the SQL.
// Takes info (*dialect.Info) which describes the dialect.
//
// Returns []string which describes each problem.
func sqlPlaceholderProblems(text string, info *dialect.Info) []string {
	scan, err := scanStatement(text, info)
	if err != nil || !slices.ContainsFunc(scan.tokens, func(token sqllex.Token) bool { return token.Kind == sqllex.KindWord }) {
		return nil
	}
	count := countPlaceholders(scan, info)
	var problems []string
	for _, foreign := range count.foreign {
		problems = append(problems, fmt.Sprintf("writes %q, which is not the execution form %s", foreign, describeSigil(info.Placeholders.Execution)))
	}
	for name := range count.names {
		if !strings.HasPrefix(name, info.Placeholders.NamePrefix) {
			problems = append(problems, fmt.Sprintf("writes the placeholder %s without the name prefix %q", name, info.Placeholders.NamePrefix))
		}
	}
	if len(count.forms) > 1 || (len(count.numbers) > 0 && count.anonymous > 0) {
		problems = append(problems, "mixes placeholder forms")
	}
	slices.Sort(problems)
	return problems
}

// describeSigil writes the first placeholder of a form.
//
// Takes sigil (dialect.PlaceholderSigil) which is the form.
//
// Returns string which is an example placeholder.
func describeSigil(sigil dialect.PlaceholderSigil) string {
	return sigil.Render(1, "name", "Type")
}

// fileNameProblem says why Go would treat a generated file's name specially: a leading
// underscore or dot makes Go ignore it, a _test suffix makes it a test, and a final
// operating system or architecture element makes it a build constraint.
//
// Takes name (string) which is the file's name.
//
// Returns string which describes the problem, or nothing.
func fileNameProblem(name string) string {
	if !strings.HasSuffix(name, goFileExtension) {
		return ""
	}
	if strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
		return "starts with " + name[:1] + ", so the go command ignores it"
	}
	if strings.HasSuffix(name, "_test"+goFileExtension) {
		return "ends in _test.go, so it compiles only into tests"
	}
	stem, _, _ := strings.Cut(name, ".")
	elements := strings.Split(stem, "_")
	if len(elements) < 2 {
		return ""
	}
	last := elements[len(elements)-1]
	if knownOperatingSystems[last] || knownArchitectures[last] {
		return "ends in _" + last + ", so it builds only for that target"
	}
	return ""
}

// firstDifference describes the first way two generations differ, or returns nothing when
// they are the same.
//
// Takes first ([]sqlmodel.GeneratedFile) which is one generation.
// Takes second ([]sqlmodel.GeneratedFile) which is the other.
//
// Returns string which describes the difference, or nothing.
func firstDifference(first, second []sqlmodel.GeneratedFile) string {
	names := func(files []sqlmodel.GeneratedFile) []string {
		listed := make([]string, 0, len(files))
		for _, file := range files {
			listed = append(listed, file.Name)
		}
		return listed
	}
	if !slices.Equal(names(first), names(second)) {
		return fmt.Sprintf("the files %v became %v", names(first), names(second))
	}
	for index := range first {
		if !bytes.Equal(first[index].Content, second[index].Content) {
			return firstDifferentLine(first[index].Name, string(first[index].Content), string(second[index].Content))
		}
	}
	return ""
}

// firstDifferentLine describes the first line two versions of a file differ on.
//
// Takes name (string) which is the file's name.
// Takes first (string) which is one version.
// Takes second (string) which is the other.
//
// Returns string which locates and shows the difference.
func firstDifferentLine(name, first, second string) string {
	firstLines, secondLines := strings.Split(first, "\n"), strings.Split(second, "\n")
	for index := range min(len(firstLines), len(secondLines)) {
		if firstLines[index] != secondLines[index] {
			return fmt.Sprintf("%s:%d %q became %q", name, index+1, firstLines[index], secondLines[index])
		}
	}
	return fmt.Sprintf("%s changed length from %d to %d lines", name, len(firstLines), len(secondLines))
}

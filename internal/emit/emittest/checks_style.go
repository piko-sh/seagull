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
	"context"
	"fmt"
	"go/ast"
	"go/types"
	"maps"
	"path"
	"reflect"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/knownfailure"
)

const (
	// maxRowDepth bounds the walk through a method's result type to the rows it holds.
	maxRowDepth = 8

	// interfaceFile is the file the interface of the query methods is generated in.
	interfaceFile = "querier_interface.go"

	// modelsFile is the file the models are generated in.
	modelsFile = "models.go"

	// queryFileSuffix ends the name of every generated query file.
	queryFileSuffix = ".sql.go"

	// queriesType is the struct the query methods are declared on.
	queriesType = "Queries"

	// jsonTagKey is the key of the json struct tag.
	jsonTagKey = "json"

	// omitEmptyOption is the json tag option the corpus preset asks for.
	omitEmptyOption = ",omitempty"
)

// queryFileMethods lists the exported methods the package's query files declare on
// *Queries, by name.
//
// Returns []string which holds the methods' names, sorted.
func (checked *checkedPackage) queryFileMethods() []string {
	var names []string
	for _, file := range checked.files {
		if !strings.HasSuffix(path.Base(checked.fileSet.Position(file.Pos()).Filename), queryFileSuffix) {
			continue
		}
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if isFunction && function.Name.IsExported() && receivesQueries(function) {
				names = append(names, function.Name.Name)
			}
		}
	}
	slices.Sort(names)
	return names
}

// modelStructs gathers the struct types the models file declares.
//
// Returns map[*types.Named]*types.Struct which maps each to its structure.
func (checked *checkedPackage) modelStructs() map[*types.Named]*types.Struct {
	structures := make(map[*types.Named]*types.Struct)
	for _, file := range checked.files {
		if path.Base(checked.fileSet.Position(file.Pos()).Filename) != modelsFile {
			continue
		}
		for _, declaration := range file.Decls {
			if general, isGeneral := declaration.(*ast.GenDecl); isGeneral {
				checked.addStructs(general, structures)
			}
		}
	}
	return structures
}

// addStructs adds the struct types a declaration declares.
//
// Takes declaration (*ast.GenDecl) which is the declaration.
// Takes structures (map[*types.Named]*types.Struct) which gathers each struct type and
// its structure.
func (checked *checkedPackage) addStructs(declaration *ast.GenDecl, structures map[*types.Named]*types.Struct) {
	for _, specification := range declaration.Specs {
		typeSpecification, isType := specification.(*ast.TypeSpec)
		if !isType {
			continue
		}
		if named, structure, isStruct := checked.packageStruct(checked.info.Defs[typeSpecification.Name].Type()); isStruct {
			structures[named] = structure
		}
	}
}

// holdsStruct reports whether a field holds a struct the package declares, such as an
// embed group of a row, directly, through a pointer or in a slice, rather than a column.
//
// Takes candidate (types.Type) which is the field's type.
//
// Returns bool which is true for such a field.
func (checked *checkedPackage) holdsStruct(candidate types.Type) bool {
	switch shape := types.Unalias(candidate).(type) {
	case *types.Pointer:
		return checked.holdsStruct(shape.Elem())
	case *types.Slice:
		return checked.holdsStruct(shape.Elem())
	default:
		named, _, isStruct := checked.packageStruct(candidate)
		return isStruct && named.NumMethods() == 0
	}
}

// documentation reads the doc comment of each method on Queries and of each method of the
// interface of the query methods, as go/ast gives its text.
//
// Returns methods (map[string]string) which maps each Queries method to its doc comment's
// text.
// Returns listed (map[string]string) which maps each interface method to its doc
// comment's text.
func (checked *checkedPackage) documentation() (methods, listed map[string]string) {
	methods = make(map[string]string)
	listed = make(map[string]string)
	for _, file := range checked.files {
		for _, declaration := range file.Decls {
			switch typed := declaration.(type) {
			case *ast.FuncDecl:
				if receivesQueries(typed) {
					methods[typed.Name.Name] = typed.Doc.Text()
				}
			case *ast.GenDecl:
				maps.Copy(listed, interfaceDocumentation(typed))
			}
		}
	}
	return methods, listed
}

// declarationsNamed counts the type declarations of a name across the package's files.
//
// Takes name (string) which is the name.
//
// Returns int which is how many type specifications declare it.
func (checked *checkedPackage) declarationsNamed(name string) int {
	count := 0
	for _, file := range checked.files {
		for _, declaration := range file.Decls {
			general, isGeneral := declaration.(*ast.GenDecl)
			if !isGeneral {
				continue
			}
			for _, specification := range general.Specs {
				if typeSpecification, isType := specification.(*ast.TypeSpec); isType && typeSpecification.Name.Name == name {
					count++
				}
			}
		}
	}
	return count
}

// rowStructNames adds the names of the structs the package declares that a method's
// result holds its rows in: directly, through a pointer, in a slice, as a parameter of a
// function it returns, such as a stream's yield, or as a type argument of another
// package's type, such as iter.Seq2.
//
// Takes candidate (types.Type) which is the result's type.
// Takes names ([]string) which holds the names found so far.
// Takes depth (int) which bounds the walk.
//
// Returns []string which holds the names, each once.
func (checked *checkedPackage) rowStructNames(candidate types.Type, names []string, depth int) []string {
	if depth >= maxRowDepth {
		return names
	}
	switch shape := types.Unalias(candidate).(type) {
	case *types.Pointer:
		return checked.rowStructNames(shape.Elem(), names, depth+1)
	case *types.Slice:
		return checked.rowStructNames(shape.Elem(), names, depth+1)
	case *types.Signature:
		for parameter := range shape.Params().Variables() {
			names = checked.rowStructNames(parameter.Type(), names, depth+1)
		}
		return names
	case *types.Named:
		if _, _, isStruct := checked.packageStruct(shape); isStruct {
			if !slices.Contains(names, shape.Obj().Name()) {
				names = append(names, shape.Obj().Name())
			}
			return names
		}
		for argument := range shape.TypeArgs().Types() {
			names = checked.rowStructNames(argument, names, depth+1)
		}
		return checked.rowStructNames(shape.Underlying(), names, depth+1)
	default:
		return names
	}
}

// checkQuerierInterface checks that the corpus generates an interface containing exactly
// the Queries methods declared in query files. It verifies that *Queries implements the
// interface, including cases with type-name collisions and query descriptions.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkQuerierInterface(ctx context.Context, current *subject) knownfailure.Outcome {
	names := append(generalCaseNames(), caseReservedTables, caseDescribed)
	return current.overCases(ctx, names, func(name string, result *caseResult) []string {
		definition, _ := findCase(name)
		problems, ok := generatedCleanly(name, result, definition.refusable)
		if !ok {
			return problemsUnlessGeneral(definition, problems)
		}
		return prefixed("case "+name+": ", interfaceProblems(result.generated, current.checkedOutput(ctx, name)))
	})
}

// interfaceProblems reports what keeps a generation's interface of the query methods from
// matching its Queries type.
//
// Takes generated (*app.Result) which is the generation.
// Takes checked (*checkedPackage) which is its type-checked output.
//
// Returns []string which describes each problem.
func interfaceProblems(generated *app.Result, checked *checkedPackage) []string {
	if !slices.ContainsFunc(generated.Files, func(file sqlmodel.GeneratedFile) bool { return file.Name == interfaceFile }) {
		return []string{"the generation wrote no " + interfaceFile + " though the preset asks for the interface"}
	}
	if checked == nil || checked.types == nil {
		return []string{"the output does not type-check"}
	}
	declared, isType := checked.types.Scope().Lookup(corpusInterfaceName).(*types.TypeName)
	queries, isQueries := checked.types.Scope().Lookup(queriesType).(*types.TypeName)
	if !isType || !isQueries {
		return []string{"the package declares no " + corpusInterfaceName + " type and Queries struct"}
	}
	contract, isInterface := declared.Type().Underlying().(*types.Interface)
	if !isInterface {
		return []string{corpusInterfaceName + " is not an interface"}
	}
	listed := make([]string, 0, contract.NumMethods())
	for method := range contract.Methods() {
		listed = append(listed, method.Name())
	}
	var problems []string
	if want := checked.queryFileMethods(); !slices.Equal(slices.Sorted(slices.Values(listed)), want) {
		problems = append(problems, fmt.Sprintf("%s lists %v; the query files declare %v on Queries", corpusInterfaceName, slices.Sorted(slices.Values(listed)), want))
	}
	if !types.Implements(types.NewPointer(queries.Type()), contract) {
		problems = append(problems, "*Queries does not implement "+corpusInterfaceName)
	}
	return problems
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
	return isIdentifier && named.Name == queriesType
}

// checkFieldTags checks that every field holding a column, in the models and in the rows
// the general cases' methods return, carries the tags the corpus preset asks for: a json
// tag first, with the omitempty option, and a db tag naming the same column.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkFieldTags(ctx context.Context, current *subject) knownfailure.Outcome {
	return current.overCases(ctx, generalCaseNames(), func(name string, result *caseResult) []string {
		if _, ok := generatedCleanly(name, result, true); !ok {
			return nil
		}
		return prefixed("case "+name+": ", tagProblems(current.checkedOutput(ctx, name)))
	})
}

// tagProblems reports each field holding a column whose struct tag differs from what the
// corpus preset asks for.
//
// Takes checked (*checkedPackage) which is the type-checked output, or nil.
//
// Returns []string which describes each problem.
func tagProblems(checked *checkedPackage) []string {
	if checked == nil || checked.types == nil {
		return nil
	}
	structures := checked.modelStructs()
	whole := checked.filledWhole()
	for _, method := range checked.queryMethods() {
		for result := range method.function.Signature().Results().Variables() {
			checked.collectRows(result.Type(), structures, whole)
		}
	}
	var problems []string
	for named, structure := range structures {
		for index := range structure.NumFields() {
			field := structure.Field(index)
			if checked.holdsStruct(field.Type()) {
				continue
			}
			if problem := tagProblem(structure.Tag(index)); problem != "" {
				problems = append(problems, fmt.Sprintf("%s: %s.%s %s", checked.fileSet.Position(field.Pos()), named.Obj().Name(), field.Name(), problem))
			}
		}
	}
	slices.Sort(problems)
	return problems
}

// tagProblem says how a field's struct tag differs from what the corpus preset asks for.
//
// Takes tag (string) which is the field's tag.
//
// Returns string which describes the difference, or empty when there is none.
func tagProblem(tag string) string {
	tags := reflect.StructTag(tag)
	name, hasJSON := tags.Lookup(jsonTagKey)
	column, hasColumn := tags.Lookup(corpusTagKey)
	switch {
	case !hasJSON || !strings.HasPrefix(tag, jsonTagKey+":"):
		return fmt.Sprintf("has the tag %q, which does not start with json", tag)
	case !strings.HasSuffix(name, omitEmptyOption):
		return fmt.Sprintf("has the json tag %q, without the omitempty option", name)
	case !hasColumn || strings.TrimSuffix(name, omitEmptyOption) != column:
		return fmt.Sprintf("has the tag %q, whose %s tag does not name the column the json tag does", tag, corpusTagKey)
	default:
		return ""
	}
}

// checkDescriptionsDocumentMethods checks that each query's description is the doc
// comment of its method and of the interface's method, and that a query without one
// leaves its method undocumented, over the described case.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkDescriptionsDocumentMethods(ctx context.Context, current *subject) knownfailure.Outcome {
	return current.overCases(ctx, []string{caseDescribed}, func(name string, result *caseResult) []string {
		problems, ok := generatedCleanly(name, result, false)
		if !ok {
			return problems
		}
		definition, _ := findCase(name)
		return prefixed("case "+name+": ", documentationProblems(definition, current.checkedOutput(ctx, name)))
	})
}

// documentationProblems reports each query of a case whose method's doc comment, or whose
// interface method's, is not its description.
//
// Takes definition (corpusCase) which is the case.
// Takes checked (*checkedPackage) which is its type-checked output.
//
// Returns []string which describes each problem.
func documentationProblems(definition corpusCase, checked *checkedPackage) []string {
	methods, listed := checked.documentation()
	var problems []string
	for _, file := range definition.files {
		for index := range file.queries {
			entry := &file.queries[index]
			want := ""
			if entry.description != "" {
				want = entry.description + "\n"
			}
			if got := methods[entry.name]; got != want {
				problems = append(problems, fmt.Sprintf("method %s is documented %q, not %q", entry.name, got, want))
			}
			if got := listed[entry.name]; got != want {
				problems = append(problems, fmt.Sprintf("%s.%s is documented %q, not %q", corpusInterfaceName, entry.name, got, want))
			}
		}
	}
	return problems
}

// interfaceDocumentation reads the doc comment of each method of the interface of the
// query methods, when a declaration declares it.
//
// Takes declaration (*ast.GenDecl) which is a declaration of the package.
//
// Returns map[string]string which maps each method to its doc comment's text, or nil.
func interfaceDocumentation(declaration *ast.GenDecl) map[string]string {
	for _, specification := range declaration.Specs {
		typeSpecification, isType := specification.(*ast.TypeSpec)
		if !isType || typeSpecification.Name.Name != corpusInterfaceName {
			continue
		}
		contract, isInterface := typeSpecification.Type.(*ast.InterfaceType)
		if !isInterface {
			return nil
		}
		documented := make(map[string]string)
		for _, method := range contract.Methods.List {
			for _, name := range method.Names {
				documented[name.Name] = method.Doc.Text()
			}
		}
		return documented
	}
	return nil
}

// checkManyReturnsAnEmptySlice checks that every call of the general runtime cases that
// returns a slice of rows, a many method's or a runtime builder's All, returns an empty
// slice rather than nil when the driver gives no row, as the corpus preset asks.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkManyReturnsAnEmptySlice(ctx context.Context, current *subject) knownfailure.Outcome {
	return current.overCalls(ctx, generalCaseNames(), nil, nilRowsProblems)
}

// nilRowsProblems reports a call that returns a slice of rows and returned nil, having
// run without an error.
//
// Takes caseName (string) which is the call's case.
// Takes call (*laneCall) which is the call.
//
// Returns []string which holds the problem, or nil.
func nilRowsProblems(caseName string, call *laneCall) []string {
	if !call.slice || call.outcome == nil || call.outcome.Error != "" || call.outcome.Panic != "" || !call.outcome.NilSlice {
		return nil
	}
	return []string{describeCall(caseName, call) + " returned nil for no rows rather than an empty slice"}
}

// checkRowTypes checks that the queries naming a table's model or a shared row type read
// their rows into it: each method's rows are of that struct, which the package declares
// once, and no Row struct of the query's own is declared, over the row types case.
//
// Takes current (*subject) which is the dialect under test.
//
// Returns knownfailure.Outcome which holds the problems.
func checkRowTypes(ctx context.Context, current *subject) knownfailure.Outcome {
	return current.overCases(ctx, []string{caseRowTypes}, func(name string, result *caseResult) []string {
		problems, ok := generatedCleanly(name, result, false)
		if !ok {
			return problems
		}
		return prefixed("case "+name+": ", rowTypeProblems(current.checkedOutput(ctx, name)))
	})
}

// rowTypeProblems reports each query of the row types case whose rows are not of the
// struct it names, a shared row type the package does not declare exactly once, and a Row
// struct a query of the case declares of its own.
//
// Takes checked (*checkedPackage) which is the case's type-checked output, or nil.
//
// Returns []string which describes each problem.
func rowTypeProblems(checked *checkedPackage) []string {
	if checked == nil || checked.types == nil {
		return []string{"the output does not type-check"}
	}
	var problems []string
	for _, method := range checked.queryMethods() {
		want, named := rowTypeReturns[method.function.Name()]
		if !named || !receivesQueries(method.declaration) {
			continue
		}
		var got []string
		for result := range method.function.Signature().Results().Variables() {
			got = checked.rowStructNames(result.Type(), got, 0)
		}
		if !slices.Equal(got, []string{want}) {
			problems = append(problems, fmt.Sprintf("method %s reads its rows into %v, not %s", method.function.Name(), got, want))
		}
	}
	if declared := checked.declarationsNamed(sharedKindRow); declared != 1 {
		problems = append(problems, fmt.Sprintf("the package declares %s %d times, not once", sharedKindRow, declared))
	}
	for queryName := range rowTypeReturns {
		if checked.types.Scope().Lookup(queryName+"Row") != nil {
			problems = append(problems, fmt.Sprintf("the package declares %sRow, a Row struct of the query's own", queryName))
		}
	}
	if checked.types.Scope().Lookup("SearchItemModelsRow") != nil {
		problems = append(problems, "the package declares SearchItemModelsRow, a Row struct of the runtime builder's own")
	}
	slices.Sort(problems)
	return problems
}

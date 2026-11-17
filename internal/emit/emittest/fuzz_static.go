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
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// fuzzStarProjection is the star projection of the fuzz model's query.
	fuzzStarProjection = "SELECT i.*"

	// fuzzExpandedProjection lists the columns the star projection stands for, as the
	// emitter expands it.
	fuzzExpandedProjection = "SELECT i.id, i.score, i.category"

	// fuzzParameterTokenPrefix starts the token standing for a resolved parameter. Its
	// leading NUL byte keeps it apart from any SQL token.
	fuzzParameterTokenPrefix = "\x00parameter:"
)

// fuzzStaticArgument identifies the original parameter bound by a generated argument.
type fuzzStaticArgument struct {
	// name identifies a driver argument bound by name.
	name string

	// parameter indexes the original query parameters.
	parameter int
}

// fuzzStaticMappingProblems checks executable constants and parameter and output
// mappings.
//
// Takes checked (*checkedPackage) which resolves generated Go expressions.
// Takes info (*dialect.Info) which supplies lexical rules.
// Takes generated (*app.DetailedResult) which supplies the original analysis.
//
// Returns []string which describes changed SQL, bindings or output columns.
func fuzzStaticMappingProblems(checked *checkedPackage, info *dialect.Info, generated *app.DetailedResult) []string {
	var problems []string
	buffers := fuzzScanBuffers(checked)
	methods := checked.queryMethods()
	for _, query := range generated.Report.Queries {
		if query.DynamicRuntime {
			continue
		}
		methodName := generated.Result.Methods[query.Name]
		found := false
		for _, method := range methods {
			if method.function.Name() != methodName {
				continue
			}
			found = true
			problems = append(problems, fuzzStaticMethodProblems(checked, buffers, info, query, method)...)
		}
		if !found {
			problems = append(problems, fmt.Sprintf("query %s has no generated method %q", query.Name, methodName))
		}
	}
	return problems
}

// fuzzStaticMethodProblems checks the SQL executed and the columns scanned by one method.
//
// Takes checked (*checkedPackage) which resolves generated expressions.
// Takes buffers (map[types.Object][]ast.Expr) which holds the package's scan buffers.
// Takes info (*dialect.Info) which supplies lexical rules.
// Takes query (*sqlmodel.AnalysedQuery) which supplies the input contract.
// Takes method (queryMethod) which identifies the generated method.
//
// Returns []string which describes changed constants or mappings.
func fuzzStaticMethodProblems(checked *checkedPackage, buffers map[types.Object][]ast.Expr, info *dialect.Info,
	query *sqlmodel.AnalysedQuery, method queryMethod) []string {
	var problems []string
	executions, scans := 0, 0
	for node := range ast.Preorder(method.declaration.Body) {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			continue
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		switch selector.Sel.Name {
		case "Query", "QueryContext", "Exec", "ExecContext", "QueryRow", "QueryRowContext":
			executions++
			problems = append(problems, fuzzStaticExecutionProblems(checked, info, query, method, call)...)
		case "Scan":
			scans++
			problems = append(problems, fuzzStaticScanProblems(checked, buffers, query, call)...)
		default:
		}
	}
	if executions != 1 {
		problems = append(problems, fmt.Sprintf("query %s has %d static execution calls", query.Name, executions))
	}
	if len(query.OutputColumns) > 0 && scans != 1 {
		problems = append(problems, fmt.Sprintf("query %s has %d scan calls", query.Name, scans))
	}
	return problems
}

// fuzzStaticExecutionProblems checks emitted SQL and its bound parameters.
//
// Takes checked (*checkedPackage) which resolves constants and fields.
// Takes info (*dialect.Info) which supplies lexical rules.
// Takes query (*sqlmodel.AnalysedQuery) which owns the original SQL.
// Takes method (queryMethod) which declares the input arguments.
// Takes call (*ast.CallExpr) which invokes the driver.
//
// Returns []string which describes an invalid execution mapping.
func fuzzStaticExecutionProblems(checked *checkedPackage, info *dialect.Info, query *sqlmodel.AnalysedQuery, method queryMethod, call *ast.CallExpr) []string {
	const sqlArgument = 1
	const bindingsStart = 2
	if len(call.Args) < bindingsStart {
		return []string{"static execution has no SQL argument"}
	}
	sql, ok := fuzzStringConstant(checked, call.Args[sqlArgument])
	if !ok {
		return []string{fmt.Sprintf("query %s does not execute a SQL constant", query.Name)}
	}
	arguments := make([]fuzzStaticArgument, 0, len(call.Args)-bindingsStart)
	for _, argument := range call.Args[bindingsStart:] {
		binding, err := fuzzStaticBinding(checked, query, method, argument)
		if err != nil {
			return []string{err.Error()}
		}
		arguments = append(arguments, binding)
	}
	want, err := fuzzStaticSQLTokens(info, query, query.SQL, nil)
	if err != nil {
		return []string{err.Error()}
	}
	got, err := fuzzStaticSQLTokens(info, query, sql, arguments)
	if err != nil {
		return []string{err.Error()}
	}
	if !slices.Equal(want, got) {
		return []string{fmt.Sprintf("query %s changed static SQL or bindings: want %q, got %q", query.Name, want, got)}
	}
	return nil
}

// fuzzStringConstant reads the resolved value of a Go string expression.
//
// Takes checked (*checkedPackage) which supplies constant values.
// Takes expression (ast.Expr) which may be a constant reference.
//
// Returns string which is the resolved text.
// Returns bool which reports a string constant.
func fuzzStringConstant(checked *checkedPackage, expression ast.Expr) (string, bool) {
	value := checked.info.Types[expression].Value
	if value == nil || value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(value), true
}

// fuzzStaticBinding finds the query parameter read by one generated driver argument.
//
// Takes checked (*checkedPackage) which resolves fields and variables.
// Takes query (*sqlmodel.AnalysedQuery) which declares logical parameters.
// Takes method (queryMethod) which declares Go input variables.
// Takes argument (ast.Expr) which is a driver argument.
//
// Returns fuzzStaticArgument which identifies the source parameter and driver name.
// Returns error when the argument does not read exactly one source parameter.
func fuzzStaticBinding(checked *checkedPackage, query *sqlmodel.AnalysedQuery, method queryMethod, argument ast.Expr) (fuzzStaticArgument, error) {
	binding := fuzzStaticArgument{name: "", parameter: -1}
	if call, ok := argument.(*ast.CallExpr); ok && len(call.Args) == 2 {
		if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "Named" {
			binding.name, _ = fuzzStringConstant(checked, call.Args[0])
			argument = call.Args[1]
		}
	}
	inputs := make(map[types.Object]bool)
	parameters := method.function.Signature().Params()
	for index := 1; index < parameters.Len(); index++ {
		parameter := parameters.At(index)
		_, _, structure := checked.packageStruct(dereference(parameter.Type()))
		inputs[parameter] = structure
	}
	matches := 0
	for node := range ast.Preorder(argument) {
		name := fuzzInputName(checked, node, inputs)
		if name == "" {
			continue
		}
		index := slices.IndexFunc(query.Parameters, func(parameter sqlmodel.QueryParameter) bool {
			return fuzzFieldKey(name) == fuzzFieldKey(parameter.Name)
		})
		if index < 0 {
			return binding, fmt.Errorf("query %s binds unknown input %q", query.Name, name)
		}
		binding.parameter = index
		matches++
	}
	if matches != 1 {
		return binding, fmt.Errorf("query %s argument reads %d source parameters", query.Name, matches)
	}
	return binding, nil
}

// fuzzInputName identifies a scalar input or a field of a parameter struct.
//
// Takes checked (*checkedPackage) which resolves input objects.
// Takes node (ast.Node) which may read a method argument.
// Takes inputs (map[types.Object]bool) which identifies struct and scalar inputs.
//
// Returns string which is empty for unrelated expressions.
func fuzzInputName(checked *checkedPackage, node ast.Node, inputs map[types.Object]bool) string {
	switch current := node.(type) {
	case *ast.SelectorExpr:
		identifier, ok := current.X.(*ast.Ident)
		if ok && inputs[checked.info.Uses[identifier]] {
			return current.Sel.Name
		}
	case *ast.Ident:
		structure, found := inputs[checked.info.Uses[current]]
		if found && !structure {
			return current.Name
		}
	default:
	}
	return ""
}

// fuzzFieldKey compares the bounded model's Go and SQL field names.
//
// Takes name (string) which contains an ASCII field identifier.
//
// Returns string which ignores case and SQL word separators.
func fuzzFieldKey(name string) string { return strings.ToLower(strings.ReplaceAll(name, "_", "")) }

// fuzzStaticSQLTokens preserves SQL positions while resolving logical parameter
// identities.
//
// Takes info (*dialect.Info) which supplies lexical and placeholder rules.
// Takes query (*sqlmodel.AnalysedQuery) which declares source parameters.
// Takes sql (string) which is original or emitted SQL.
// Takes arguments ([]fuzzStaticArgument) which is nil for original SQL.
//
// Returns []string which retains literals, operators and ordered parameter identities.
// Returns error when a placeholder lacks a source or driver argument.
func fuzzStaticSQLTokens(info *dialect.Info, query *sqlmodel.AnalysedQuery, sql string, arguments []fuzzStaticArgument) ([]string, error) {
	sql = strings.Replace(sql, fuzzStarProjection, fuzzExpandedProjection, 1)
	tokens, err := sqllex.Tokens(executionRules(info), sql)
	if err != nil {
		return nil, err
	}
	var result []string
	ordinal := 0
	used := make([]bool, len(arguments))
	for _, token := range tokens {
		if token.Kind.IsNoise() || token.Raw == ";" {
			continue
		}
		if token.Kind != sqllex.KindParameter {
			result = append(result, fuzzStaticToken(token))
			continue
		}
		identity, err := fuzzStaticParameter(info, query, token.Raw, ordinal, arguments, used)
		if err != nil {
			return nil, err
		}
		ordinal++
		result = append(result, identity)
	}
	if slices.Contains(used, false) {
		return nil, errors.New("static execution has unused driver arguments")
	}
	return result, nil
}

// fuzzStaticToken normalises identifier quoting while preserving literal contents.
//
// Takes token (sqllex.Token) which is not a placeholder.
//
// Returns string which preserves the SQL token's meaning.
func fuzzStaticToken(token sqllex.Token) string {
	text := token.Raw
	if token.Kind == sqllex.KindQuotedIdentifier {
		text = text[1 : len(text)-1]
	}
	if token.Kind == sqllex.KindWord || token.Kind == sqllex.KindQuotedIdentifier {
		return strings.ToLower(text)
	}
	return text
}

// fuzzStaticParameter resolves one placeholder through the original or emitted bindings.
//
// Takes info (*dialect.Info) which supplies accepted placeholder forms.
// Takes query (*sqlmodel.AnalysedQuery) which owns the logical parameters.
// Takes raw (string) which is the placeholder token.
// Takes ordinal (int) which counts anonymous placeholder occurrences.
// Takes arguments ([]fuzzStaticArgument) which is nil for original SQL.
// Takes used ([]bool) which records referenced driver arguments.
//
// Returns string which identifies the source parameter and its type annotation.
// Returns error when the placeholder cannot be resolved.
func fuzzStaticParameter(info *dialect.Info, query *sqlmodel.AnalysedQuery, raw string, ordinal int, arguments []fuzzStaticArgument, used []bool) (string, error) {
	reading, ok := info.Placeholders.Read(raw)
	if !ok {
		return "", fmt.Errorf("unknown static placeholder %q", raw)
	}
	index := reading.Number - 1
	if reading.Sigil.Kind == dialect.PlaceholderAnonymous || reading.Number == 0 && reading.Sigil.Kind == dialect.PlaceholderNumbered {
		index = ordinal
	}
	if arguments == nil {
		index = fuzzSourceParameter(query.Parameters, reading, index+1)
	} else {
		if reading.Sigil.Kind == dialect.PlaceholderNamed {
			index = slices.IndexFunc(arguments, func(argument fuzzStaticArgument) bool { return argument.name == reading.Name })
		}
		if index < 0 || index >= len(arguments) {
			return "", fmt.Errorf("static placeholder %q has no driver argument", raw)
		}
		used[index] = true
		index = arguments[index].parameter
	}
	if index < 0 || index >= len(query.Parameters) {
		return "", fmt.Errorf("static placeholder %q has no source parameter", raw)
	}
	return fuzzParameterTokenPrefix + strconv.Itoa(index) + ":" + reading.Type, nil
}

// fuzzSourceParameter resolves an original placeholder by its declared identity.
//
// Takes parameters ([]sqlmodel.QueryParameter) which declares source parameters.
// Takes reading (dialect.PlaceholderReading) which holds the parsed placeholder.
// Takes number (int) which identifies a numbered or anonymous input.
//
// Returns int which is the parameter index or negative one.
func fuzzSourceParameter(parameters []sqlmodel.QueryParameter, reading dialect.PlaceholderReading, number int) int {
	return slices.IndexFunc(parameters, func(parameter sqlmodel.QueryParameter) bool {
		if reading.Sigil.Kind != dialect.PlaceholderNamed {
			return parameter.Number == number
		}
		name := parameter.Placeholder
		if name == "" {
			name = parameter.Name
		}
		return name == reading.Name
	})
}

// fuzzStaticScanProblems checks output tags against original column names and order.
//
// Takes checked (*checkedPackage) which resolves scan destinations.
// Takes buffers (map[types.Object][]ast.Expr) which holds the package's scan buffers.
// Takes query (*sqlmodel.AnalysedQuery) which declares output columns.
// Takes call (*ast.CallExpr) which scans one row.
//
// Returns []string which describes a missing or misidentified output column.
func fuzzStaticScanProblems(checked *checkedPackage, buffers map[types.Object][]ast.Expr, query *sqlmodel.AnalysedQuery,
	call *ast.CallExpr) []string {
	arguments := fuzzScanArguments(buffers, checked, call)
	if len(arguments) != len(query.OutputColumns) {
		return []string{fmt.Sprintf("query %s scans %d columns, expected %d", query.Name, len(arguments), len(query.OutputColumns))}
	}
	for index, argument := range arguments {
		selection := fuzzScanField(checked, argument)
		if selection == nil {
			continue
		}
		_, structure, local := checked.packageStruct(dereference(selection.Recv()))
		if !local {
			continue
		}
		field := selection.Index()[0]
		name, _, _ := strings.Cut(reflect.StructTag(structure.Tag(field)).Get(jsonTagKey), jsonOptionSeparator)
		if name != query.OutputColumns[index].Name {
			return []string{fmt.Sprintf("query %s output %d maps %q to %q", query.Name, index, query.OutputColumns[index].Name, name)}
		}
	}
	return nil
}

// fuzzScanArguments expands a locally declared scan buffer.
//
// Takes buffers (map[types.Object][]ast.Expr) which maps each local slice to the elements
// of the literal assigned to it.
// Takes checked (*checkedPackage) which resolves the buffer's variable.
// Takes call (*ast.CallExpr) which may pass a variadic slice.
//
// Returns []ast.Expr which holds the actual scan destinations.
func fuzzScanArguments(buffers map[types.Object][]ast.Expr, checked *checkedPackage, call *ast.CallExpr) []ast.Expr {
	if !call.Ellipsis.IsValid() || len(call.Args) != 1 {
		return call.Args
	}
	identifier, ok := call.Args[0].(*ast.Ident)
	if !ok {
		return call.Args
	}
	if arguments, found := buffers[checked.info.Uses[identifier]]; found {
		return arguments
	}
	return call.Args
}

// fuzzScanBuffers maps each local variable assigned a slice literal to that literal's
// elements, in one pass over the generated files.
//
// Takes checked (*checkedPackage) which resolves variable definitions.
//
// Returns map[types.Object][]ast.Expr which holds the first literal of each variable.
func fuzzScanBuffers(checked *checkedPackage) map[types.Object][]ast.Expr {
	buffers := make(map[types.Object][]ast.Expr)
	for _, file := range checked.files {
		for node := range ast.Preorder(file) {
			object, elements, ok := fuzzAssignedSlice(checked, node)
			if !ok {
				continue
			}
			if _, seen := buffers[object]; !seen {
				buffers[object] = elements
			}
		}
	}
	return buffers
}

// fuzzAssignedSlice reads a slice literal assigned to a local variable.
//
// Takes checked (*checkedPackage) which resolves variable definitions.
// Takes node (ast.Node) which may initialise the local variable.
//
// Returns types.Object which is the variable defined.
// Returns []ast.Expr which holds the slice elements.
// Returns bool which reports a slice literal definition.
func fuzzAssignedSlice(checked *checkedPackage, node ast.Node) (types.Object, []ast.Expr, bool) {
	assignment, ok := node.(*ast.AssignStmt)
	if !ok || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
		return nil, nil, false
	}
	name, ok := assignment.Lhs[0].(*ast.Ident)
	if !ok {
		return nil, nil, false
	}
	object := checked.info.Defs[name]
	literal, ok := assignment.Rhs[0].(*ast.CompositeLit)
	if object == nil || !ok {
		return nil, nil, false
	}
	return object, literal.Elts, true
}

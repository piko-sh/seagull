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

package dialecttest

import (
	"context"
	"fmt"
	"slices"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// parameterTypingQueriesFile is the query file the parameter typing checks generate
	// from.
	parameterTypingQueriesFile = "parameters.sql"

	// rowComparisonQuery is the query checkRowComparisonsTypeTheirParameters analyses, its
	// placeholders those of rowComparisonParameters in order.
	rowComparisonQuery = "SELECT id FROM items WHERE (id, name) < (%s, %s) OR (id, name) IN ((%s, %s))"
)

var (
	// rowComparisonParameters names the placeholders of rowComparisonQuery, in order.
	rowComparisonParameters = []string{"after_id", "after_name", "listed_id", "listed_name"}

	// sharedTypeFunctions names the functions a dialect may list whose arguments and result
	// share one type: GREATEST and LEAST, the scalar MAX and MIN of two values, and the
	// two-argument forms of IFNULL and NVL.
	sharedTypeFunctions = []string{"greatest", "least", "max", "min", "ifnull", "nvl"}
)

// checkAssignedExpressionParametersAreNotTypedAsTheColumn checks that a parameter an
// INSERT's VALUES row assigns to a column only as an argument of a function, as
// length($1) assigned to the integer id, does not take the column's type, while one that
// is the whole value does.
//
// Takes fixture (*Fixture) which supplies the dialect and its base migration.
//
// Returns checkOutcome which holds any problem found.
func checkAssignedExpressionParametersAreNotTypedAsTheColumn(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	engine := fixture.NewDialect()
	info := engine.Info()
	if fixture.BaseMigration == "" || len(info.Placeholders.Sigils) == 0 {
		return notApplicable("the fixture sets no BaseMigration or the dialect lists no placeholder form")
	}
	sigil := info.Placeholders.Sigils[0]
	if sigil.TypeSeparator != "" {
		return notApplicable("the dialect's placeholders declare their own types")
	}
	statement := fmt.Sprintf("INSERT INTO items (id, name) VALUES (length(%s), %s)",
		sigil.Render(1, info.Placeholders.BoundName("label"), ""), sigil.Render(2, info.Placeholders.BoundName("name"), ""))
	emitter, result, err := runGenerate(ctx, engine, map[string]string{baseMigrationFile: fixture.BaseMigration},
		map[string]string{parameterTypingQueriesFile: directiveLine(engine, "AssignedExpression", "exec") + statement + ";\n"})
	if err != nil {
		return failed(generationFatalFormat, err)
	}
	if len(result.Diagnostics) > 0 {
		return failed(undiagnosedFormat, statement, formatDiagnostics(result.Diagnostics))
	}
	analysed := findQuery(emitter.queries, "AssignedExpression")
	if analysed == nil || len(analysed.Parameters) != 2 {
		return failed("%q must be analysed into two parameters", statement)
	}
	var problems []string
	if category := analysed.Parameters[0].SQLType.Category; category == sqlmodel.TypeCategoryInteger {
		problems = append(problems, fmt.Sprintf("the parameter of length(...) in %q is %s, the type of the column id it is part of the value of",
			statement, describeType(&analysed.Parameters[0].SQLType)))
	}
	if category := analysed.Parameters[1].SQLType.Category; category != sqlmodel.TypeCategoryText {
		problems = append(problems, fmt.Sprintf("the parameter assigned to name in %q is %s, but must take the column's text type",
			statement, describeType(&analysed.Parameters[1].SQLType)))
	}
	return outcomeOf(problems)
}

// checkComparedExpressionParametersAreNotTypedAsTheColumn checks that parameters inherit
// the compared expression's type and nullability rather than those of a column within it.
//
// Takes fixture (*Fixture) which supplies the dialect and its base migration.
//
// Returns checkOutcome which holds any problem found.
func checkComparedExpressionParametersAreNotTypedAsTheColumn(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	engine := fixture.NewDialect()
	info := engine.Info()
	if fixture.BaseMigration == "" || len(info.Placeholders.Sigils) == 0 {
		return notApplicable("the fixture sets no BaseMigration or the dialect lists no placeholder form")
	}
	sigil := info.Placeholders.Sigils[0]
	if sigil.TypeSeparator != "" {
		return notApplicable("the dialect's placeholders declare their own types")
	}
	statement := fmt.Sprintf("SELECT id FROM items WHERE coalesce(name, 'none') = %s AND length(name) = %s",
		sigil.Render(1, info.Placeholders.BoundName("label"), ""), sigil.Render(2, info.Placeholders.BoundName("size"), ""))
	emitter, result, err := runGenerate(ctx, engine, map[string]string{baseMigrationFile: fixture.BaseMigration},
		map[string]string{parameterTypingQueriesFile: directiveLine(engine, "ComparedExpression", "many") + statement + ";\n"})
	if err != nil {
		return failed(generationFatalFormat, err)
	}
	if len(result.Diagnostics) > 0 {
		return failed(undiagnosedFormat, statement, formatDiagnostics(result.Diagnostics))
	}
	analysed := findQuery(emitter.queries, "ComparedExpression")
	if analysed == nil || len(analysed.Parameters) != 2 {
		return failed("%q must be analysed into two parameters", statement)
	}
	var problems []string
	if coalesced := &analysed.Parameters[0]; coalesced.Nullable || coalesced.SQLType.Category != sqlmodel.TypeCategoryText {
		problems = append(problems, fmt.Sprintf("the parameter compared with coalesce(name, 'none') in %q is %s, nullable %t, but must "+
			"take the expression's text type, which is never NULL, not the nullable column's", statement,
			describeType(&coalesced.SQLType), coalesced.Nullable))
	}
	if measured := &analysed.Parameters[1]; measured.SQLType.Category == sqlmodel.TypeCategoryText {
		problems = append(problems, fmt.Sprintf("the parameter compared with length(name) in %q is %s, the type of the column the call "+
			"is over", statement, describeType(&measured.SQLType)))
	}
	return outcomeOf(problems)
}

// checkRowComparisonsTypeTheirParameters checks, for a dialect that compares row values,
// that each parameter of a row compared with a row of columns, and of a row tested
// against a list of rows, takes the type of the column at its position.
//
// Takes fixture (*Fixture) which supplies the dialect, its base migration and whether it
// compares row values.
//
// Returns checkOutcome which holds any problem found.
func checkRowComparisonsTypeTheirParameters(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if !fixture.RowComparisons || fixture.BaseMigration == "" {
		return notApplicable("the fixture does not compare row values")
	}
	engine := fixture.NewDialect()
	info := engine.Info()
	if len(info.Placeholders.Sigils) == 0 {
		return notApplicable("the dialect lists no placeholder form")
	}
	placeholders := make([]any, len(rowComparisonParameters))
	for index, name := range rowComparisonParameters {
		placeholders[index] = info.Placeholders.Sigils[0].Render(index+1, info.Placeholders.BoundName(name), placeholderFormType)
	}
	statement := fmt.Sprintf(rowComparisonQuery, placeholders...)
	emitter, result, err := runGenerate(ctx, engine, map[string]string{baseMigrationFile: fixture.BaseMigration},
		map[string]string{parameterTypingQueriesFile: directiveLine(engine, "RowComparison", "many") + statement + ";\n"})
	if err != nil {
		return failed(generationFatalFormat, err)
	}
	if len(result.Diagnostics) > 0 {
		return failed(undiagnosedFormat, statement, formatDiagnostics(result.Diagnostics))
	}
	analysed := findQuery(emitter.queries, "RowComparison")
	if analysed == nil || len(analysed.Parameters) != len(rowComparisonParameters) {
		return failed("%q must be analysed into %d parameters", statement, len(rowComparisonParameters))
	}
	return outcomeOf(rowParameterProblems(statement, analysed.Parameters))
}

// rowParameterProblems compares the parameters of the row comparison check with the
// categories of the columns at their positions: the integer id, then the text name.
//
// Takes statement (string) which is the query.
// Takes parameters ([]sqlmodel.QueryParameter) which are its parameters.
//
// Returns []string which describes each problem found.
func rowParameterProblems(statement string, parameters []sqlmodel.QueryParameter) []string {
	want := []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryInteger, sqlmodel.TypeCategoryText}
	var problems []string
	for index := range parameters {
		if expected := want[index%len(want)]; parameters[index].SQLType.Category != expected {
			problems = append(problems, fmt.Sprintf("the parameter %d of %q is %s, but must take the %s type of the column at its position",
				parameters[index].Number, statement, describeType(&parameters[index].SQLType), expected))
		}
	}
	return problems
}

// checkSharedTypeArgumentsTakeTheirSiblingsType checks that function parameters assigned
// in UPDATE inherit the neighbouring column's type when the function's arguments share a
// type.
//
// Takes fixture (*Fixture) which supplies the dialect and its base migration.
//
// Returns checkOutcome which holds any problem found.
func checkSharedTypeArgumentsTakeTheirSiblingsType(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	engine := fixture.NewDialect()
	info := engine.Info()
	if fixture.BaseMigration == "" || len(info.Placeholders.Sigils) == 0 {
		return notApplicable("the fixture sets no BaseMigration or the dialect lists no placeholder form")
	}
	sigil := info.Placeholders.Sigils[0]
	if sigil.TypeSeparator != "" {
		return notApplicable("the dialect's placeholders declare their own types")
	}
	functions := sharedTypeFunctionsOf(engine.BuiltinFunctions())
	if len(functions) == 0 {
		return notApplicable("the dialect lists none of the functions whose arguments share the result's type")
	}
	var problems []string
	for _, name := range functions {
		statement := fmt.Sprintf("UPDATE items SET id = %s(id, %s), name = %s(%s, name)", name,
			sigil.Render(1, info.Placeholders.BoundName("floor"), ""), name, sigil.Render(2, info.Placeholders.BoundName("label"), ""))
		problems = append(problems, sharedTypeProblems(ctx, fixture, engine, statement)...)
	}
	return outcomeOf(problems)
}

// sharedTypeFunctionsOf lists the functions of sharedTypeFunctions a catalogue holds as a
// scalar function of two arguments whose arguments and result share one type.
//
// Takes catalogue (*ast.FunctionCatalogue) which is the dialect's built-in functions; nil
// holds none.
//
// Returns []string which holds the names.
func sharedTypeFunctionsOf(catalogue *ast.FunctionCatalogue) []string {
	if catalogue == nil {
		return nil
	}
	var names []string
	for _, name := range sharedTypeFunctions {
		if slices.ContainsFunc(catalogue.Functions[name], sharesItsArgumentsType) {
			names = append(names, name)
		}
	}
	return names
}

// sharesItsArgumentsType reports whether a signature is a scalar function that takes two
// arguments of no declared type and returns their type.
//
// Takes signature (*sqlmodel.Function) which is the signature.
//
// Returns bool which is true for such a signature.
func sharesItsArgumentsType(signature *sqlmodel.Function) bool {
	if signature == nil || signature.IsAggregate || len(signature.Arguments) == 0 || len(signature.Arguments) > 2 {
		return false
	}
	if len(signature.Arguments) == 1 && !signature.IsVariadic {
		return false
	}
	for index := range signature.Arguments {
		if signature.Arguments[index].Type.Category != sqlmodel.TypeCategoryUnknown {
			return false
		}
	}
	return signature.ReturnType.Category == sqlmodel.TypeCategoryUnknown
}

// sharedTypeProblems generates one statement of the shared type check,
// checkSharedTypeArgumentsTakeTheirSiblingsType, and compares its two parameters with the
// integer id and the text name.
//
// Takes fixture (*Fixture) which supplies the base migration.
// Takes engine (dialect.DialectPort) which is the dialect.
// Takes statement (string) which is the statement.
//
// Returns []string which describes each problem found.
func sharedTypeProblems(ctx context.Context, fixture *Fixture, engine dialect.DialectPort, statement string) []string {
	emitter, result, err := runGenerate(ctx, engine, map[string]string{baseMigrationFile: fixture.BaseMigration},
		map[string]string{parameterTypingQueriesFile: directiveLine(engine, "SharedType", "exec") + statement + ";\n"})
	if err != nil {
		return []string{fmt.Sprintf(generationFatalFormat, err)}
	}
	if len(result.Diagnostics) > 0 {
		return []string{fmt.Sprintf(undiagnosedFormat, statement, formatDiagnostics(result.Diagnostics))}
	}
	analysed := findQuery(emitter.queries, "SharedType")
	if analysed == nil || len(analysed.Parameters) != 2 {
		return []string{fmt.Sprintf("%q must be analysed into two parameters", statement)}
	}
	want := []sqlmodel.SQLTypeCategory{sqlmodel.TypeCategoryInteger, sqlmodel.TypeCategoryText}
	var problems []string
	for index := range analysed.Parameters {
		if category := analysed.Parameters[index].SQLType.Category; category != want[index] {
			problems = append(problems, fmt.Sprintf("the parameter %d of %q is %s, but must take the %s type of the column beside it",
				analysed.Parameters[index].Number, statement, describeType(&analysed.Parameters[index].SQLType), want[index]))
		}
	}
	return problems
}

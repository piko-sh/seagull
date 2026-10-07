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
	"maps"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// scalarCallQueriesFile is the query file of the scalar call checks.
	scalarCallQueriesFile = "scalar_calls.sql"
)

// checkScalarFunctionsInFromFollowTheDialect checks that supported scalar FROM calls have
// the same type as select-list calls and use the function name or alias as their column
// name.
//
// Takes fixture (*Fixture) which supplies the dialect and its base migration.
//
// Returns checkOutcome which holds any problem found.
func checkScalarFunctionsInFromFollowTheDialect(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	engine := fixture.NewDialect()
	if !engine.Info().ScalarFunctionsInFrom {
		return notApplicable("Info.ScalarFunctionsInFrom is false")
	}
	name, found := argumentlessScalarFunction(engine)
	if !found {
		return notApplicable("the dialect lists no built-in function of no arguments that returns one value")
	}
	queries := directiveLine(engine, "InTheSelectList", "one") + "SELECT " + name + "() AS probe;\n\n" +
		directiveLine(engine, "InFrom", "one") + "SELECT * FROM " + name + "();\n\n" +
		directiveLine(engine, "Aliased", "one") + "SELECT probe FROM " + name + "() AS probe;\n"
	emitter, result, err := runGenerate(ctx, engine, map[string]string{baseMigrationFile: fixture.BaseMigration},
		map[string]string{scalarCallQueriesFile: queries})
	if err != nil {
		return failed(generationFatalFormat, err)
	}
	if len(result.Diagnostics) > 0 {
		return failed("calls of %s() in a select list and in FROM must analyse without diagnostics, got %s", name,
			formatDiagnostics(result.Diagnostics))
	}
	inList := findQuery(emitter.queries, "InTheSelectList")
	if inList == nil || len(inList.OutputColumns) != 1 {
		return failed("SELECT %s() AS probe must give one column", name)
	}
	var problems []string
	for _, expected := range []struct{ query, column string }{{query: "InFrom", column: name}, {query: "Aliased", column: "probe"}} {
		problems = append(problems, scalarCallProblems(findQuery(emitter.queries, expected.query), expected.column, &inList.OutputColumns[0])...)
	}
	return outcomeOf(problems)
}

// scalarCallProblems describes how the columns of a query reading a scalar call in FROM
// differ from the one column the call gives in a select list.
//
// Takes analysed (*sqlmodel.AnalysedQuery) which is the query, or nil when it was not
// analysed.
// Takes column (string) which is the name the column must have.
// Takes want (*sqlmodel.OutputColumn) which is the call's column in a select list.
//
// Returns []string which holds the problems.
func scalarCallProblems(analysed *sqlmodel.AnalysedQuery, column string, want *sqlmodel.OutputColumn) []string {
	if analysed == nil || len(analysed.OutputColumns) != 1 {
		return []string{"a call in FROM must give one column, named " + column}
	}
	got := &analysed.OutputColumns[0]
	var problems []string
	if !strings.EqualFold(got.Name, column) {
		problems = append(problems, "the call's column in "+analysed.Name+" is named "+got.Name+", but must be named "+column)
	}
	if got.SQLType.Category != want.SQLType.Category || !strings.EqualFold(got.SQLType.EngineName, want.SQLType.EngineName) ||
		got.Nullable != want.Nullable {
		problems = append(problems, "the call's column in "+analysed.Name+" is "+describeType(&got.SQLType)+
			", but must be typed as in the select list, "+describeType(&want.SQLType))
	}
	return problems
}

// argumentlessScalarFunction returns the first built-in function, in name order, that
// takes no arguments and returns one value of a known type.
//
// Takes engine (dialect.DialectPort) which lists the built-in functions.
//
// Returns string which is the function's name.
// Returns bool which is false when the dialect lists none.
func argumentlessScalarFunction(engine dialect.DialectPort) (string, bool) {
	functions := engine.BuiltinFunctions()
	if functions == nil {
		return "", false
	}
	for _, name := range slices.Sorted(maps.Keys(functions.Functions)) {
		if slices.ContainsFunc(functions.Functions[name], returnsOneValueOfNoArguments) {
			return name, true
		}
	}
	return "", false
}

// returnsOneValueOfNoArguments reports whether a signature takes no arguments and returns
// one value of a known type, as a call of it in a select list does.
//
// Takes signature (*sqlmodel.Function) which is the signature.
//
// Returns bool which is true for such a signature.
func returnsOneValueOfNoArguments(signature *sqlmodel.Function) bool {
	return signature != nil && len(signature.Arguments) == 0 && !signature.IsAggregate && !signature.ReturnsSet &&
		!signature.IsVariadic && signature.ReturnType.Category != sqlmodel.TypeCategoryUnknown
}

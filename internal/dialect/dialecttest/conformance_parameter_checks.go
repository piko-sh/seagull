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
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// namedParameterStatementFormat bounds id by two placeholders, compares a third with it
	// and passes a fourth to lower() compared with lower(name).
	namedParameterStatementFormat = "SELECT name FROM items WHERE id BETWEEN %s AND %s AND %s < id AND lower(name) = lower(%s);"

	// comparedParameterStatementFormat compares an expression of the fixture's items table,
	// which names no column on its own, with one placeholder in the select list and with
	// another in WHERE.
	comparedParameterStatementFormat = "SELECT id + 1 = %s AS matched FROM items WHERE id + 1 > %s"

	// comparedExpressionProbe selects the expression the placeholders of
	// comparedParameterStatementFormat are compared with, to learn its type.
	comparedExpressionProbe = "SELECT id + 1 AS shifted FROM items;"
)

var (
	// namedParameterNames are the names the placeholders of namedParameterStatementFormat
	// take from the columns they are compared with, in order.
	namedParameterNames = []string{"id_from", "id_to", "id", "name"}

	// conditionStatementFormats write a placeholder where a condition stands: the whole
	// WHERE condition, an operand of AND and the condition of a CASE branch.
	conditionStatementFormats = []string{
		"SELECT id FROM items WHERE %s;",
		"SELECT id FROM items WHERE id > 0 AND %s;",
		"SELECT CASE WHEN %s THEN id ELSE 0 END AS chosen FROM items;",
	}
)

// placedParameter is a parameter expression an analysis holds, with where it was found.
type placedParameter struct {
	// place names the part of the statement holding it, such as the select list.
	place string

	// number is the parameter's number.
	number int
}

// checkParameterExpressionsCarryTheirNumbers verifies that placeholder expressions retain
// their parameter numbers.
//
// This lets the analyser infer parameter types from their use in expressions.
//
// Takes fixture (*Fixture) which supplies the dialect.
//
// Returns checkOutcome which holds every problem found.
func checkParameterExpressionsCarryTheirNumbers(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	engine := fixture.NewDialect()
	info := engine.Info()
	var problems []string
	for index, sigil := range info.Placeholders.Sigils {
		statement := comparedParameterStatement(&info, sigil)
		label := fmt.Sprintf("Info.Placeholders.Sigils[%d] %q", index, statement)
		analysis, err := analyseOneStatement(ctx, engine, statement)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s must analyse: %v", label, err))
			continue
		}
		problems = append(problems, parameterExpressionProblems(label, analysis)...)
	}
	return outcomeOf(problems)
}

// comparedParameterStatement writes comparedParameterStatementFormat with two
// placeholders in one form.
//
// Takes info (*dialect.Info) which describes the dialect.
// Takes sigil (dialect.PlaceholderSigil) which is the form.
//
// Returns string which is the statement.
func comparedParameterStatement(info *dialect.Info, sigil dialect.PlaceholderSigil) string {
	first := sigil.Render(1, info.Placeholders.BoundName("first"), placeholderFormType)
	second := sigil.Render(2, info.Placeholders.BoundName("second"), placeholderFormType)
	return fmt.Sprintf(comparedParameterStatementFormat, first, second)
}

// analyseOneStatement parses a statement and analyses it without a catalogue.
//
// Takes engine (dialect.DialectPort) which parses and analyses.
// Takes statement (string) which is the SQL of one statement.
//
// Returns *ast.RawQueryAnalysis which is the statement's analysis.
// Returns error when the statement does not parse into one statement or does not analyse.
func analyseOneStatement(ctx context.Context, engine dialect.DialectPort, statement string) (*ast.RawQueryAnalysis, error) {
	statements, err := engine.ParseStatements(ctx, statement)
	if err != nil {
		return nil, err
	}
	if len(statements) != 1 {
		return nil, fmt.Errorf("parsed into %d statements, want 1", len(statements))
	}
	return engine.AnalyseQuery(ctx, nil, statements[0])
}

// parameterExpressionProblems describes how the parameter expressions of an analysis of
// comparedParameterStatementFormat differ from its placeholders: the select list must
// hold the first placeholder's parameter and the clauses the second's, each once.
//
// Takes label (string) which names the statement for messages.
// Takes analysis (*ast.RawQueryAnalysis) which is the statement's analysis.
//
// Returns []string which holds the problems.
func parameterExpressionProblems(label string, analysis *ast.RawQueryAnalysis) []string {
	placeholders := analysis.Spans.Placeholders
	if len(placeholders) != 2 {
		return []string{fmt.Sprintf("%s: the analysis must record 2 placeholder spans, got %d", label, len(placeholders))}
	}
	want := []placedParameter{
		{place: "the select list", number: placeholderNumber(analysis, &placeholders[0])},
		{place: "the clauses", number: placeholderNumber(analysis, &placeholders[1])},
	}
	got := placedParameters(analysis)
	if slices.Equal(got, want) {
		return nil
	}
	return []string{fmt.Sprintf("%s: the placeholders must be analysed into the parameter expressions %s, got %s",
		label, describePlacedParameters(want), describePlacedParameters(got))}
}

// placeholderNumber returns the number of the parameter a placeholder span stands for:
// the span's own number for a numbered or anonymous form, and for a named form the number
// of the parameter reference of its name.
//
// Takes analysis (*ast.RawQueryAnalysis) which holds the parameter references.
// Takes span (*ast.PlaceholderSpan) which is the placeholder.
//
// Returns int which is the parameter's number, or zero when no reference has its name.
func placeholderNumber(analysis *ast.RawQueryAnalysis, span *ast.PlaceholderSpan) int {
	if span.Number > 0 {
		return span.Number
	}
	for index := range analysis.ParameterReferences {
		reference := &analysis.ParameterReferences[index]
		if span.Name != "" && strings.EqualFold(reference.Name, span.Name) {
			return reference.Number
		}
	}
	return 0
}

// placedParameters lists the parameter expressions of an analysis's select list, then
// those of its clauses, outside any subquery.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis.
//
// Returns []placedParameter which holds them in the order met.
func placedParameters(analysis *ast.RawQueryAnalysis) []placedParameter {
	var placed []placedParameter
	for index := range analysis.OutputColumns {
		for _, number := range parameterNumbers(analysis.OutputColumns[index].Expression, 0) {
			placed = append(placed, placedParameter{place: "the select list", number: number})
		}
	}
	for index := range analysis.ClauseExpressions {
		for _, number := range parameterNumbers(analysis.ClauseExpressions[index].Expression, 0) {
			placed = append(placed, placedParameter{place: "the clauses", number: number})
		}
	}
	return placed
}

// parameterNumbers returns the numbers of the parameter expressions inside an expression,
// in the order met.
//
// Takes expression (ast.Expression) which is the expression, or nil.
// Takes depth (int) which bounds the walk.
//
// Returns []int which holds the numbers.
func parameterNumbers(expression ast.Expression, depth int) []int {
	if expression == nil || depth > maxWalkDepth {
		return nil
	}
	if parameter, isParameter := expression.(*ast.ParameterExpression); isParameter && parameter != nil {
		return []int{parameter.Number}
	}
	var numbers []int
	for _, child := range nestedExpressions(expression) {
		numbers = append(numbers, parameterNumbers(child, depth+1)...)
	}
	return numbers
}

// describePlacedParameters renders parameter expressions for a problem.
//
// Takes placed ([]placedParameter) which holds the expressions.
//
// Returns string which is the rendering.
func describePlacedParameters(placed []placedParameter) string {
	if len(placed) == 0 {
		return "[]"
	}
	parts := make([]string, len(placed))
	for index, each := range placed {
		parts[index] = fmt.Sprintf("parameter %d in %s", each.number, each.place)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// checkComparedParametersAreTyped checks that a parameter compared with an expression
// that names no column, in the select list and in WHERE, takes the type of that
// expression, written in the first form a query may write a placeholder in.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which lists every parameter typed otherwise.
func checkComparedParametersAreTyped(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	engine := fixture.NewDialect()
	info := engine.Info()
	if len(info.Placeholders.Sigils) == 0 {
		return notApplicable("Info.Placeholders lists no form a query may write a placeholder in")
	}
	statement := comparedParameterStatement(&info, info.Placeholders.Sigils[0]) + ";"
	emitter, result, err := runGenerate(ctx, engine,
		map[string]string{"001_items.up.sql": fixture.BaseMigration},
		map[string]string{
			"compared.sql": directiveLine(engine, "Compared", "many") + statement,
			"shifted.sql":  directiveLine(engine, "Shifted", "many") + comparedExpressionProbe,
		})
	if err != nil {
		return failed(generationFatalFormat, err)
	}
	if len(result.Diagnostics) > 0 {
		return failed("%q and %q must analyse without diagnostics, got %s", statement, comparedExpressionProbe,
			formatDiagnostics(result.Diagnostics))
	}
	compared, shifted := findQuery(emitter.queries, "Compared"), findQuery(emitter.queries, "Shifted")
	if compared == nil || shifted == nil || len(shifted.OutputColumns) != 1 {
		return failed("the queries Compared and Shifted must be analysed")
	}
	want := shifted.OutputColumns[0].SQLType.Category
	if len(compared.Parameters) != 2 {
		return failed("%q holds two placeholders but reports %d parameters", statement, len(compared.Parameters))
	}
	var problems []string
	for index := range compared.Parameters {
		parameter := &compared.Parameters[index]
		if parameter.SQLType.Category != want || want == sqlmodel.TypeCategoryUnknown {
			problems = append(problems, fmt.Sprintf("the parameter %d of %q is %s, but it is compared with id + 1, which is %s",
				parameter.Number, statement, describeType(&parameter.SQLType), describeType(&shifted.OutputColumns[0].SQLType)))
		}
	}
	return outcomeOf(problems)
}

// checkParametersInStatementsRunWithoutAnalysisAreRefused checks that a parameter in a
// statement the dialect runs without analysing, which the generated code could not bind,
// is refused with a parse error rather than reported as an internal fault.
//
// Takes fixture (*Fixture) which supplies the dialect and the statement.
//
// Returns checkOutcome which holds any problem found.
func checkParametersInStatementsRunWithoutAnalysisAreRefused(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if fixture.UnboundStatement == "" {
		return notApplicable("the fixture names no statement the dialect runs without analysing")
	}
	engine := fixture.NewDialect()
	query := directiveLine(engine, "Unbound", "exec") + fixture.UnboundStatement + ";\n"
	_, result, err := runGenerate(ctx, engine, map[string]string{baseMigrationFile: fixture.BaseMigration},
		map[string]string{"queries.sql": query})
	if err != nil {
		return failed(generationFatalFormat, err)
	}
	refused := slices.ContainsFunc(result.Diagnostics, func(diagnostic diag.SourceError) bool {
		return diagnostic.Code == diag.CodeParseError && diagnostic.Severity == diag.SeverityError &&
			strings.Contains(diagnostic.Message, "no parameter can be bound")
	})
	internal := slices.ContainsFunc(result.Diagnostics, func(diagnostic diag.SourceError) bool {
		return diagnostic.Code == diag.CodeInternalNilGuard
	})
	if !refused || internal {
		return failed("the parameter in %q must be refused with a %s error saying no parameter can be bound there, got %s",
			fixture.UnboundStatement, diag.CodeParseError, formatDiagnostics(result.Diagnostics))
	}
	return passed
}

// checkCoalescedParametersTakeTheirSiblingsType checks that COALESCE parameters inherit
// the neighbouring column's type and remain nullable unless their placeholder specifies
// otherwise.
//
// Takes fixture (*Fixture) which supplies the dialect and its base migration.
//
// Returns checkOutcome which holds any problem found.
func checkCoalescedParametersTakeTheirSiblingsType(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	engine := fixture.NewDialect()
	info := engine.Info()
	if len(info.Placeholders.Sigils) == 0 {
		return notApplicable("Info.Placeholders lists no form a query may write a placeholder in")
	}
	sigil := info.Placeholders.Sigils[0]
	statement := "SELECT name FROM items WHERE id = COALESCE(" + sigil.Render(1, info.Placeholders.BoundName("wanted"), placeholderFormType) + ", id);"
	emitter, result, err := runGenerate(ctx, engine, map[string]string{baseMigrationFile: fixture.BaseMigration},
		map[string]string{"coalesced.sql": directiveLine(engine, "Coalesced", "many") + statement})
	if err != nil {
		return failed(generationFatalFormat, err)
	}
	if len(result.Diagnostics) > 0 {
		return failed("%q must analyse without diagnostics, got %s", statement, formatDiagnostics(result.Diagnostics))
	}
	coalesced := findQuery(emitter.queries, "Coalesced")
	if coalesced == nil || len(coalesced.Parameters) != 1 {
		return failed("the query Coalesced was not analysed into one parameter")
	}
	parameter := &coalesced.Parameters[0]
	var problems []string
	if parameter.SQLType.Category != sqlmodel.TypeCategoryInteger {
		problems = append(problems, fmt.Sprintf("the parameter of %q is %s, but must take the integer type of id, which COALESCE "+
			"resolves it to", statement, describeType(&parameter.SQLType)))
	}
	if sigil.TypeSeparator == "" && !parameter.Nullable {
		problems = append(problems, fmt.Sprintf("the parameter of %q must be nullable, as COALESCE falls back to id when it is NULL", statement))
	}
	return outcomeOf(problems)
}

// checkQuantifiedArrayParametersAreArraysOfTheComparedType checks, for a dialect that
// compares a value with each element of an array, that the parameter of id = ANY($1) and
// id <> ALL($1) is an array of id's integer type, named after id.
//
// Takes fixture (*Fixture) which supplies the dialect and its base migration.
//
// Returns checkOutcome which holds any problem found.
func checkQuantifiedArrayParametersAreArraysOfTheComparedType(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if !fixture.QuantifiedArrays {
		return notApplicable("the fixture does not compare a value with the elements of an array")
	}
	engine := fixture.NewDialect()
	info := engine.Info()
	if len(info.Placeholders.Sigils) == 0 {
		return notApplicable("Info.Placeholders lists no form a query may write a placeholder in")
	}
	placeholder := info.Placeholders.Sigils[0].Render(1, info.Placeholders.BoundName("wanted"), placeholderFormType)
	var problems []string
	for _, quantified := range []string{"id = ANY(" + placeholder + ")", "id <> ALL(" + placeholder + ")"} {
		statement := "SELECT name FROM items WHERE " + quantified + ";"
		emitter, result, err := runGenerate(ctx, engine, map[string]string{baseMigrationFile: fixture.BaseMigration},
			map[string]string{"quantified.sql": directiveLine(engine, "Quantified", "many") + statement})
		if err != nil {
			return failed(generationFatalFormat, err)
		}
		query := findQuery(emitter.queries, "Quantified")
		if len(result.Diagnostics) > 0 || query == nil || len(query.Parameters) != 1 {
			problems = append(problems, fmt.Sprintf("%q must analyse into one parameter without diagnostics, got %s", statement,
				formatDiagnostics(result.Diagnostics)))
			continue
		}
		parameter := &query.Parameters[0]
		element := parameter.SQLType.ElementType
		if parameter.SQLType.Category != sqlmodel.TypeCategoryArray || element == nil || element.Category != sqlmodel.TypeCategoryInteger {
			problems = append(problems, fmt.Sprintf("the parameter of %q is %s, but must be an array of id's integer type", statement,
				describeType(&parameter.SQLType)))
		}
		if !strings.EqualFold(parameter.Name, "id") {
			problems = append(problems, fmt.Sprintf("the parameter of %q is named %q, but must be named after id", statement, parameter.Name))
		}
	}
	return outcomeOf(problems)
}

// checkComparedParametersAreNamedAfterTheirColumns checks that unnamed predicate
// parameters receive names derived from the columns they are compared with.
//
// Takes fixture (*Fixture) which supplies the dialect and its base migration.
//
// Returns checkOutcome which holds any problem found.
func checkComparedParametersAreNamedAfterTheirColumns(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	engine := fixture.NewDialect()
	info := engine.Info()
	if len(info.Placeholders.Sigils) == 0 || info.Placeholders.Sigils[0].Kind == dialect.PlaceholderNamed {
		return notApplicable("the dialect's first placeholder form names its parameter")
	}
	sigil := info.Placeholders.Sigils[0]
	placeholders := make([]any, 0, len(namedParameterNames))
	for index := range namedParameterNames {
		placeholders = append(placeholders, sigil.Render(index+1, "", placeholderFormType))
	}
	statement := fmt.Sprintf(namedParameterStatementFormat, placeholders...)
	emitter, result, err := runGenerate(ctx, engine, map[string]string{baseMigrationFile: fixture.BaseMigration},
		map[string]string{"named.sql": directiveLine(engine, "Named", "many") + statement})
	if err != nil {
		return failed(generationFatalFormat, err)
	}
	query := findQuery(emitter.queries, "Named")
	if len(result.Diagnostics) > 0 || query == nil {
		return failed("%q must analyse without diagnostics, got %s", statement, formatDiagnostics(result.Diagnostics))
	}
	names := make([]string, 0, len(query.Parameters))
	for index := range query.Parameters {
		names = append(names, strings.ToLower(query.Parameters[index].Name))
	}
	if !slices.Equal(names, namedParameterNames) {
		return failed("the parameters of %q are named %v, but must be named %v after the columns they are compared with", statement, names,
			namedParameterNames)
	}
	return passed
}

// checkParametersStandingForConditionsAreBooleans checks that a placeholder standing
// alone where a condition stands, as a WHERE condition, an operand of AND or the
// condition of a CASE branch, is typed boolean, as the engine reads it.
//
// Takes fixture (*Fixture) which supplies the dialect and its base migration.
//
// Returns checkOutcome which holds any problem found.
func checkParametersStandingForConditionsAreBooleans(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	engine := fixture.NewDialect()
	info := engine.Info()
	if len(info.Placeholders.Sigils) == 0 || info.Placeholders.Sigils[0].TypeSeparator != "" {
		return notApplicable("the dialect's first placeholder form declares its parameter's type")
	}
	placeholder := info.Placeholders.Sigils[0].Render(1, info.Placeholders.BoundName("flag"), placeholderFormType)
	statements := make([]string, 0, len(conditionStatementFormats))
	for _, format := range conditionStatementFormats {
		statements = append(statements, fmt.Sprintf(format, placeholder))
	}
	var problems []string
	for _, statement := range statements {
		emitter, result, err := runGenerate(ctx, engine, map[string]string{baseMigrationFile: fixture.BaseMigration},
			map[string]string{"condition.sql": directiveLine(engine, "Condition", "many") + statement})
		if err != nil {
			return failed(generationFatalFormat, err)
		}
		query := findQuery(emitter.queries, "Condition")
		if len(result.Diagnostics) > 0 || query == nil || len(query.Parameters) != 1 {
			problems = append(problems, fmt.Sprintf("%q must analyse into one parameter without diagnostics, got %s", statement,
				formatDiagnostics(result.Diagnostics)))
			continue
		}
		if category := query.Parameters[0].SQLType.Category; category != sqlmodel.TypeCategoryBoolean {
			problems = append(problems, fmt.Sprintf("the parameter of %q is %s, but stands for a condition, so it must be boolean",
				statement, describeType(&query.Parameters[0].SQLType)))
		}
	}
	return outcomeOf(problems)
}

// checkValuesWithoutAColumnListTypeTheirParametersByPosition checks that each placeholder
// of an INSERT that lists no target columns, standing alone as a value, takes the type
// and the name of the table's column at its position, as the engine assigns it.
//
// Takes fixture (*Fixture) which supplies the dialect and its base migration.
//
// Returns checkOutcome which holds any problem found.
func checkValuesWithoutAColumnListTypeTheirParametersByPosition(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	engine := fixture.NewDialect()
	info := engine.Info()
	if len(info.Placeholders.Sigils) == 0 || info.Placeholders.Sigils[0].TypeSeparator != "" || info.Placeholders.Sigils[0].Kind == dialect.PlaceholderNamed {
		return notApplicable("the dialect's first placeholder form names or types its parameter")
	}
	sigil := info.Placeholders.Sigils[0]
	statement := "INSERT INTO items VALUES (" + sigil.Render(1, "", placeholderFormType) + ", " + sigil.Render(2, "", placeholderFormType) + ");"
	emitter, result, err := runGenerate(ctx, engine, map[string]string{baseMigrationFile: fixture.BaseMigration},
		map[string]string{"positional.sql": directiveLine(engine, "Positional", "exec") + statement})
	if err != nil {
		return failed(generationFatalFormat, err)
	}
	query := findQuery(emitter.queries, "Positional")
	if len(result.Diagnostics) > 0 || query == nil || len(query.Parameters) != 2 {
		return failed("%q must analyse into two parameters without diagnostics, got %s", statement, formatDiagnostics(result.Diagnostics))
	}
	var problems []string
	for index, want := range []struct {
		name     string
		category sqlmodel.SQLTypeCategory
	}{{name: "id", category: sqlmodel.TypeCategoryInteger}, {name: "name", category: sqlmodel.TypeCategoryText}} {
		parameter := &query.Parameters[index]
		if !strings.EqualFold(parameter.Name, want.name) || parameter.SQLType.Category != want.category {
			problems = append(problems, fmt.Sprintf("parameter %d of %q is %s named %q, but must take the type and name of the column %s",
				index+1, statement, describeType(&parameter.SQLType), parameter.Name, want.name))
		}
	}
	return outcomeOf(problems)
}

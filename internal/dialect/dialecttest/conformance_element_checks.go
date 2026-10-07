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
	"strings"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// problemFormat reports a problem described in full.
	problemFormat = "%s"

	// elementQueryName names the query the element check generates.
	elementQueryName = "ReadSamples"

	// parameterQueryName names the query the parameter element check generates.
	parameterQueryName = "FindSamples"

	// bracketedQueryName names the query the bracketed value check generates.
	bracketedQueryName = "AddSamples"

	// bracketedParameterColumn names the column of samples the parameter after a bracketed
	// value is assigned to.
	bracketedParameterColumn = "id"
)

// checkElementsTheEngineCanHoldNullInArePointers checks element nullability and preset
// overrides.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds every problem found.
func checkElementsTheEngineCanHoldNullInArePointers(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if fixture.Elements == nil {
		return notApplicable("the dialect has no array, list or map type")
	}
	engine := fixture.NewDialect()
	byDefault, problem := elementColumnType(ctx, engine, fixture, false)
	if problem != "" {
		return failed(problemFormat, problem)
	}
	declared, problem := elementColumnType(ctx, engine, fixture, true)
	if problem != "" {
		return failed(problemFormat, problem)
	}
	holder, implements := engine.(dialect.ElementNullabilityPort)
	holds := implements && holder.HoldsNullElements(byDefault)
	var problems []string
	switch {
	case holds && !byDefault.ElementType.Nullable:
		problems = append(problems, "the elements of readings must be nullable by default: the engine can hold NULL among them")
	case !holds && byDefault.ElementType.Nullable:
		problems = append(problems, "the elements of readings must follow their type where the engine says in the type where NULL can be")
	}
	if declared.ElementType.Nullable {
		problems = append(problems, "the elements of readings must be non-null when the preset declares elements non-null")
	}
	return outcomeOf(problems)
}

// checkAggregatedNonNullElementsAreNotPointers checks that the elements of an array or a
// list an aggregate collects from a column that is never NULL are never NULL either,
// whatever the engine could hold in such a type: the analysis proves them non-null, so
// the default that makes such elements pointers does not apply.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds every problem found.
func checkAggregatedNonNullElementsAreNotPointers(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if fixture.Elements == nil || fixture.Elements.Aggregate == "" {
		return notApplicable("the dialect has no aggregate collecting an array or a list")
	}
	engine := fixture.NewDialect()
	query := "SELECT " + fixture.Elements.Aggregate + " AS ids FROM samples"
	emitter, result, err := runGenerate(ctx, engine,
		map[string]string{"001_samples.up.sql": fixture.Elements.Migration},
		map[string]string{"samples.sql": directiveLine(engine, elementQueryName, "one") + query + ";"})
	if err != nil {
		return failed(generationFatalFormat, err)
	}
	if len(result.Diagnostics) > 0 {
		return failed("%q must analyse without diagnostics, got %s", query, formatDiagnostics(result.Diagnostics))
	}
	analysed := findQuery(emitter.queries, elementQueryName)
	if analysed == nil || len(analysed.OutputColumns) != 1 || analysed.OutputColumns[0].SQLType.ElementType == nil {
		return failed("%q must be analysed into one column of an array or a list", query)
	}
	if analysed.OutputColumns[0].SQLType.ElementType.Nullable {
		return failed("the elements of %q must be non-null: the aggregated column is never NULL", query)
	}
	return passed
}

// checkArrayParametersHoldNullElements checks parameter-element nullability against the
// dialect and preset. Quantified comparisons require non-null elements.
//
// Takes fixture (*Fixture) which supplies the dialect and its SQL.
//
// Returns checkOutcome which holds every problem found.
func checkArrayParametersHoldNullElements(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if fixture.Elements == nil || fixture.Elements.Parameter == "" {
		return notApplicable("the dialect binds no parameter of an array, list or map type")
	}
	engine := fixture.NewDialect()
	byDefault, problem := elementParameterType(ctx, engine, fixture, fixture.Elements.Parameter, false)
	if problem != "" {
		return failed(problemFormat, problem)
	}
	declared, problem := elementParameterType(ctx, engine, fixture, fixture.Elements.Parameter, true)
	if problem != "" {
		return failed(problemFormat, problem)
	}
	holder, implements := engine.(dialect.ElementNullabilityPort)
	holds := implements && holder.HoldsNullElements(byDefault)
	var problems []string
	switch {
	case holds && !byDefault.ElementType.Nullable:
		problems = append(problems, "the elements of a parameter compared with readings must be nullable by default: the engine can hold NULL among them")
	case !holds && byDefault.ElementType.Nullable:
		problems = append(problems, "the elements of a parameter compared with readings must follow their type where the engine says in the type where NULL can be")
	}
	if declared.ElementType.Nullable {
		problems = append(problems, "the elements of a parameter compared with readings must be non-null when the preset declares elements non-null")
	}
	if fixture.Elements.Quantified != "" {
		quantified, problem := elementParameterType(ctx, engine, fixture, fixture.Elements.Quantified, false)
		switch {
		case problem != "":
			problems = append(problems, problem)
		case quantified.ElementType.Nullable:
			problems = append(problems, "the elements of an array a quantified comparison reads must be non-null: a NULL element can only make the comparison NULL")
		}
	}
	return outcomeOf(problems)
}

// elementParameterType generates a query binding one parameter of an array, list or map
// type and returns the parameter's type.
//
// Takes engine (dialect.DialectPort) which is the dialect.
// Takes fixture (*Fixture) which supplies the migration.
// Takes query (string) which is the query binding the parameter.
// Takes nonNull (bool) which is the preset's element choice.
//
// Returns sqlmodel.SQLType which is the parameter's type.
// Returns string which describes why the type could not be read, or is empty.
func elementParameterType(ctx context.Context, engine dialect.DialectPort, fixture *Fixture, query string, nonNull bool) (sqlmodel.SQLType, string) {
	chosen := app.DefaultPreset()
	chosen.NonNullElements = nonNull
	emitter, result, err := runGenerate(ctx, engine,
		map[string]string{"001_samples.up.sql": fixture.Elements.Migration},
		map[string]string{"samples.sql": directiveLine(engine, parameterQueryName, "many") + query + ";"},
		app.WithPreset(chosen))
	if err != nil {
		return sqlmodel.SQLType{}, "generation itself should not fail fatally: " + err.Error()
	}
	if len(result.Diagnostics) > 0 {
		return sqlmodel.SQLType{}, query + " must analyse without diagnostics, got " + formatDiagnostics(result.Diagnostics)
	}
	analysed := findQuery(emitter.queries, parameterQueryName)
	if analysed == nil || len(analysed.Parameters) != 1 || analysed.Parameters[0].SQLType.ElementType == nil {
		return sqlmodel.SQLType{}, query + " must be analysed into one parameter of an array, list or map type"
	}
	return analysed.Parameters[0].SQLType, ""
}

// elementColumnType generates a query reading the readings column of Fixture.Elements and
// returns the column's type.
//
// Takes engine (dialect.DialectPort) which is the dialect.
// Takes fixture (*Fixture) which supplies the migration.
// Takes nonNull (bool) which is the preset's element choice.
//
// Returns sqlmodel.SQLType which is the column's type.
// Returns string which describes why the type could not be read, or is empty.
func elementColumnType(ctx context.Context, engine dialect.DialectPort, fixture *Fixture, nonNull bool) (sqlmodel.SQLType, string) {
	chosen := app.DefaultPreset()
	chosen.NonNullElements = nonNull
	emitter, result, err := runGenerate(ctx, engine,
		map[string]string{"001_samples.up.sql": fixture.Elements.Migration},
		map[string]string{"samples.sql": directiveLine(engine, elementQueryName, "many") + "SELECT readings FROM samples;"},
		app.WithPreset(chosen))
	if err != nil {
		return sqlmodel.SQLType{}, "generation itself should not fail fatally: " + err.Error()
	}
	if len(result.Diagnostics) > 0 {
		return sqlmodel.SQLType{}, "reading readings must analyse without diagnostics, got " + formatDiagnostics(result.Diagnostics)
	}
	query := findQuery(emitter.queries, elementQueryName)
	if query == nil || len(query.OutputColumns) != 1 || query.OutputColumns[0].SQLType.ElementType == nil {
		return sqlmodel.SQLType{}, "the query " + elementQueryName + " must be analysed into the column readings of an array, list or map type"
	}
	return query.OutputColumns[0].SQLType, ""
}

// checkBracketedValuesLeaveLaterParametersTheirColumns checks that bracketed or braced
// INSERT values count as one value, keeping later parameters aligned with their columns.
//
// Takes fixture (*Fixture) which supplies the dialect, its table samples and the values
// written for its column readings.
//
// Returns checkOutcome which holds every problem found.
func checkBracketedValuesLeaveLaterParametersTheirColumns(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	if fixture.Elements == nil || len(fixture.Elements.Literals) == 0 {
		return notApplicable("the dialect writes no value of an array, list or map with brackets or braces")
	}
	engine := fixture.NewDialect()
	info := engine.Info()
	if len(info.Placeholders.Sigils) == 0 {
		return notApplicable("the dialect lists no placeholder form")
	}
	sigil := info.Placeholders.Sigils[0]
	boundName := info.Placeholders.BoundName(bracketedParameterColumn)
	wantName := bracketedParameterColumn
	if sigil.Kind == dialect.PlaceholderNamed {
		wantName = boundName
	}
	placeholder := sigil.Render(1, boundName, placeholderFormType)
	var problems []string
	for _, literal := range fixture.Elements.Literals {
		statement := "INSERT INTO samples (readings, " + bracketedParameterColumn + ") VALUES (" + literal + ", " + placeholder + ")"
		problems = append(problems, bracketedValueProblems(ctx, engine, fixture, statement, wantName)...)
	}
	return outcomeOf(problems)
}

// bracketedValueProblems generates an INSERT whose VALUES row holds a bracketed value
// before its one parameter and reports the parameter unless it takes the integer type of
// the column id it is assigned to and the name it must have.
//
// Takes engine (dialect.DialectPort) which is the dialect.
// Takes fixture (*Fixture) which supplies the migration.
// Takes statement (string) which is the INSERT.
// Takes wantName (string) which is the parameter's name: the column's, or the one its
// placeholder gives it.
//
// Returns []string which describes each problem found.
func bracketedValueProblems(ctx context.Context, engine dialect.DialectPort, fixture *Fixture, statement, wantName string) []string {
	emitter, result, err := runGenerate(ctx, engine,
		map[string]string{"001_samples.up.sql": fixture.Elements.Migration},
		map[string]string{"samples.sql": directiveLine(engine, bracketedQueryName, "exec") + statement + ";"})
	if err != nil {
		return []string{fmt.Sprintf(generationFatalFormat, err)}
	}
	if len(result.Diagnostics) > 0 {
		return []string{fmt.Sprintf("%q must analyse without diagnostics, got %s", statement, formatDiagnostics(result.Diagnostics))}
	}
	analysed := findQuery(emitter.queries, bracketedQueryName)
	if analysed == nil || len(analysed.Parameters) != 1 {
		return []string{fmt.Sprintf("%q must be analysed into one parameter", statement)}
	}
	parameter := &analysed.Parameters[0]
	if strings.EqualFold(parameter.Name, wantName) && parameter.SQLType.Category == sqlmodel.TypeCategoryInteger {
		return nil
	}
	return []string{fmt.Sprintf("the parameter after the bracketed value in %q is %s named %q, but must be named %q and take the type of the column %s at its position",
		statement, describeType(&parameter.SQLType), parameter.Name, wantName, bracketedParameterColumn)}
}

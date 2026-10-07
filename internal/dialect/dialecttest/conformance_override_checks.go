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

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// declaredOverrideColumn is the column the declared-parameter check overrides.
	declaredOverrideColumn = "declared_items.value"

	// declaredOverridePackage is the import path of the Go type the check overrides the
	// column with.
	declaredOverridePackage = "example.com/declared"

	// declaredOverrideName is the name of the Go type the check overrides the column with.
	declaredOverrideName = "Value"

	// patternOverrideColumn is the text column the declared-parameter check also overrides,
	// which the fixture's patterns are matched against.
	patternOverrideColumn = "declared_items.label"

	// patternOverrideName is the name of the Go type the check overrides the text column
	// with.
	patternOverrideName = "Label"
)

// DeclaredParameterFixture supplies the queries that check how a column override reaches
// a parameter that declares its own type, by a cast at the parameter or by the type its
// placeholder carries.
type DeclaredParameterFixture struct {
	// Migration creates a table named declared_items with a column named value, the column
	// the check overrides, and a text column named label, which it overrides too.
	Migration string

	// Restating holds queries whose parameters preserve value's declared type. These
	// exercise the dialect's supported comparisons and assignments.
	Restating string

	// Differing holds queries like Restating's whose every parameter declares a type other
	// than value's.
	Differing string

	// Patterns holds queries that use parameters as patterns against label. Those parameters
	// must not inherit label's column override.
	Patterns string

	// ProjectionViews creates, after Migration, a view named declared_view that selects
	// value as it is, under the name value, and cast to another type, under the name other,
	// which the projection queries read.
	ProjectionViews string

	// Projections holds queries whose output should inherit a column override, including
	// unchanged projections and casts to the same SQL type.
	Projections string

	// CastProjections holds queries like Projections' whose every output column returns
	// value cast to another type, including declared_view's other, so none takes the
	// override: the column override describes the column's own values, not the cast's.
	CastProjections string
}

// checkColumnOverridesFollowDeclaredTypes checks which parameter declarations inherit
// column overrides.
//
// Takes fixture (*Fixture) which supplies the dialect and its queries.
//
// Returns checkOutcome which lists every parameter that takes the override wrongly.
func checkColumnOverridesFollowDeclaredTypes(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	declared := fixture.DeclaredParameters
	if declared == nil {
		return notApplicable("Fixture.DeclaredParameters is not set")
	}
	var problems []string
	for _, group := range []struct {
		queries   string
		label     string
		overrides bool
	}{
		{queries: declared.Restating, label: "Restating", overrides: true},
		{queries: declared.Differing, label: "Differing", overrides: false},
		{queries: declared.Patterns, label: "Patterns", overrides: false},
	} {
		problems = append(problems, declaredParameterProblems(ctx, fixture, declared.Migration, group.queries, group.label,
			group.overrides)...)
	}
	return outcomeOf(problems)
}

// declaredParameterProblems generates one group of the fixture's queries with the column
// overridden and describes every parameter that takes the override when it should not, or
// misses it when it should.
//
// Takes fixture (*Fixture) which supplies the dialect.
// Takes migration (string) which creates declared_items.
// Takes queries (string) which holds the group's queries.
// Takes label (string) which names the group for messages.
// Takes overrides (bool) which is true when every parameter must take the override.
//
// Returns []string which holds the problems found.
func declaredParameterProblems(
	ctx context.Context,
	fixture *Fixture,
	migration, queries, label string,
	overrides bool,
) []string {
	if queries == "" {
		return []string{fmt.Sprintf("Fixture.DeclaredParameters.%s must hold queries", label)}
	}
	columnOverrides := []app.TypeOverride{
		{SQLTypeName: "", Column: declaredOverrideColumn, GoPackage: declaredOverridePackage, GoName: declaredOverrideName, NonNullElements: nil, StructTags: nil},
		{SQLTypeName: "", Column: patternOverrideColumn, GoPackage: declaredOverridePackage, GoName: patternOverrideName, NonNullElements: nil, StructTags: nil},
	}
	emitter, result, err := runGenerate(ctx, fixture.NewDialect(),
		map[string]string{"001_declared_items.up.sql": migration},
		map[string]string{"declared.sql": queries},
		app.WithTypeOverrides(columnOverrides...))
	if err != nil {
		return []string{fmt.Sprintf(generationFatalFormat, err)}
	}
	if failures := errorDiagnostics(result.Diagnostics); len(failures) > 0 {
		return []string{fmt.Sprintf("the %s queries must analyse without errors, got %s", label, formatDiagnostics(failures))}
	}
	if len(emitter.queries) == 0 {
		return []string{fmt.Sprintf("the %s queries must be analysed", label)}
	}
	var problems []string
	for _, query := range emitter.queries {
		if len(query.Parameters) == 0 {
			problems = append(problems, fmt.Sprintf("the %s query %s must hold a parameter", label, query.Name))
		}
		for index := range query.Parameters {
			parameter := &query.Parameters[index]
			if takesDeclaredOverride(parameter, overrides) != overrides {
				problems = append(problems, declaredOverrideProblem(label, query.Name, parameter, overrides))
			}
		}
	}
	return problems
}

// errorDiagnostics returns the diagnostics of error severity.
//
// Takes diagnostics ([]diag.SourceError) which are a run's diagnostics.
//
// Returns []diag.SourceError which holds those of error severity.
func errorDiagnostics(diagnostics []diag.SourceError) []diag.SourceError {
	return slices.DeleteFunc(slices.Clone(diagnostics), func(diagnostic diag.SourceError) bool {
		return diagnostic.Severity != diag.SeverityError
	})
}

// takesDeclaredOverride reports whether a parameter took an override the check gives: the
// one of declared_items.value when it should take that one, and any when it should take
// none.
//
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
// Takes overrides (bool) which is true when the parameter should take value's override.
//
// Returns bool which is true when its Go type is an override the check looks for.
func takesDeclaredOverride(parameter *sqlmodel.QueryParameter, overrides bool) bool {
	if parameter.GoTypeOverride == nil {
		return false
	}
	return !overrides || (parameter.GoTypeOverride.Package == declaredOverridePackage &&
		parameter.GoTypeOverride.Name == declaredOverrideName)
}

// declaredOverrideProblem describes a parameter whose Go type does not follow the rule.
//
// Takes label (string) which names the group of queries.
// Takes queryName (string) which names the query.
// Takes parameter (*sqlmodel.QueryParameter) which is the parameter.
// Takes overrides (bool) which is true when the parameter should have taken the override.
//
// Returns string which is the problem.
func declaredOverrideProblem(label, queryName string, parameter *sqlmodel.QueryParameter, overrides bool) string {
	if overrides {
		return fmt.Sprintf("the %s query %s: parameter %d (%s, %s) declares value's own type or none, so it must take the "+
			"column override of %s", label, queryName, parameter.Number, parameter.Name, describeType(&parameter.SQLType),
			declaredOverrideColumn)
	}
	return fmt.Sprintf("the %s query %s: parameter %d (%s, %s) declares another type than its column or is a pattern, so "+
		"it must keep its own Go type rather than a column override", label, queryName, parameter.Number, parameter.Name,
		describeType(&parameter.SQLType))
}

// checkColumnOverridesFollowProjectedTypes verifies that projections preserve column
// overrides appropriately.
//
// Unchanged columns and casts to their own type keep the override; casts to another type
// discard it.
//
// Takes fixture (*Fixture) which supplies the dialect and its queries.
//
// Returns checkOutcome which lists every output column that takes the override wrongly or
// misses it.
func checkColumnOverridesFollowProjectedTypes(ctx context.Context, _ checkEnvironment, fixture *Fixture) checkOutcome {
	declared := fixture.DeclaredParameters
	if declared == nil {
		return notApplicable("Fixture.DeclaredParameters is not set")
	}
	var problems []string
	for _, group := range []struct {
		queries   string
		label     string
		overrides bool
	}{
		{queries: declared.Projections, label: "Projections", overrides: true},
		{queries: declared.CastProjections, label: "CastProjections", overrides: false},
	} {
		problems = append(problems, projectedColumnProblems(ctx, fixture, group.queries, group.label, group.overrides)...)
	}
	return outcomeOf(problems)
}

// projectedColumnProblems generates one group of the fixture's projection queries with
// the column overridden and describes every output column that takes the override when it
// should not, or misses it when it should.
//
// Takes fixture (*Fixture) which supplies the dialect and its migrations.
// Takes queries (string) which holds the group's queries.
// Takes label (string) which names the group for messages.
// Takes overrides (bool) which is true when every output column must take the override.
//
// Returns []string which holds the problems found.
func projectedColumnProblems(ctx context.Context, fixture *Fixture, queries, label string, overrides bool) []string {
	declared := fixture.DeclaredParameters
	if queries == "" || declared.ProjectionViews == "" {
		return []string{fmt.Sprintf("Fixture.DeclaredParameters.%s and ProjectionViews must hold statements", label)}
	}
	columnOverrides := []app.TypeOverride{
		{SQLTypeName: "", Column: declaredOverrideColumn, GoPackage: declaredOverridePackage, GoName: declaredOverrideName, NonNullElements: nil, StructTags: nil},
	}
	emitter, result, err := runGenerate(ctx, fixture.NewDialect(),
		map[string]string{"001_declared_items.up.sql": declared.Migration, "002_declared_view.up.sql": declared.ProjectionViews},
		map[string]string{"projected.sql": queries},
		app.WithTypeOverrides(columnOverrides...))
	if err != nil {
		return []string{fmt.Sprintf(generationFatalFormat, err)}
	}
	if failures := errorDiagnostics(result.Diagnostics); len(failures) > 0 {
		return []string{fmt.Sprintf("the %s queries must analyse without errors, got %s", label, formatDiagnostics(failures))}
	}
	if len(emitter.queries) == 0 {
		return []string{fmt.Sprintf("the %s queries must be analysed", label)}
	}
	var problems []string
	for _, query := range emitter.queries {
		if len(query.OutputColumns) == 0 {
			problems = append(problems, fmt.Sprintf("the %s query %s must return a column", label, query.Name))
		}
		for index := range query.OutputColumns {
			column := &query.OutputColumns[index]
			if takesProjectedOverride(column) == overrides {
				continue
			}
			problems = append(problems, projectedOverrideProblem(label, query.Name, column, overrides))
		}
	}
	return problems
}

// takesProjectedOverride reports whether an output column took the override of
// declared_items.value.
//
// Takes column (*sqlmodel.OutputColumn) which is the output column.
//
// Returns bool which is true when its Go type is that override.
func takesProjectedOverride(column *sqlmodel.OutputColumn) bool {
	return column.GoTypeOverride != nil && column.GoTypeOverride.Package == declaredOverridePackage &&
		column.GoTypeOverride.Name == declaredOverrideName
}

// projectedOverrideProblem describes an output column whose Go type does not follow the
// rule.
//
// Takes label (string) which names the group of queries.
// Takes queryName (string) which names the query.
// Takes column (*sqlmodel.OutputColumn) which is the output column.
// Takes overrides (bool) which is true when the column should have taken the override.
//
// Returns string which is the problem.
func projectedOverrideProblem(label, queryName string, column *sqlmodel.OutputColumn, overrides bool) string {
	if overrides {
		return fmt.Sprintf("the %s query %s: column %s (%s) returns value as it is or as its own type, so it must take "+
			"the column override of %s", label, queryName, column.Name, describeType(&column.SQLType), declaredOverrideColumn)
	}
	return fmt.Sprintf("the %s query %s: column %s (%s) returns value cast to another type, so it must keep its own Go "+
		"type rather than the column override of %s", label, queryName, column.Name, describeType(&column.SQLType),
		declaredOverrideColumn)
}

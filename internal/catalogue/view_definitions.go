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

package catalogue

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/panicguard"
	"seagull.sh/seagull/internal/resolve"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

// definedView is a view whose definition parses to one statement, ready to be analysed
// against the catalogue.
type definedView struct {
	// view is the catalogue's view, typed in place.
	view *sqlmodel.View

	// statement is its parsed definition.
	statement ast.ParsedStatement

	// failed records that analysing the definition panicked, so it is not analysed again.
	failed bool
}

// retypeView analyses one view's definition and applies the result to its columns. A
// panic in the analysis, and an analysis the dialect left with a nil node, are reported
// once; the view then keeps its columns and is not analysed again.
//
// Takes defined (*definedView) which is the view and its parsed definition.
//
// Returns bool which is true when a column changed.
// Returns *diag.SourceError which is the Q070 error for a panic, the Q030 error for a nil
// node, or nil.
func (b *catalogueBuilder) retypeView(ctx context.Context, defined *definedView) (bool, *diag.SourceError) {
	if defined.failed {
		return false, nil
	}
	var invalid *resolve.Error
	changed, err := panicguard.RunValue(ctx, "catalogue: type view definition", func() (bool, error) {
		var retyped bool
		retyped, invalid = b.applyViewDefinition(ctx, defined)
		return retyped, nil
	})
	if errors.Is(err, fault.ErrPanic) {
		defined.failed = true
		return false, new(viewPanic(defined.view, "analysing", err))
	}
	if invalid != nil {
		defined.failed = true
		return false, new(diagbuild.Unlocated(invalid.Code, invalid.Severity,
			fmt.Sprintf("view %s: %s", qualifiedName(defined.view.Schema, defined.view.Name), invalid.Message)))
	}
	return changed, nil
}

// applyViewDefinition analyses one view's definition and applies the result to its
// columns.
//
// Takes defined (*definedView) which is the view and its parsed definition.
//
// Returns bool which is true when a column changed.
// Returns *resolve.Error which is the Q030 problem when the dialect's analysis holds a
// nil node, or nil.
func (b *catalogueBuilder) applyViewDefinition(ctx context.Context, defined *definedView) (bool, *resolve.Error) {
	analysis, err := b.engine.AnalyseQuery(ctx, b.catalogue, defined.statement)
	if err != nil || analysis == nil || analysis.DataChange != 0 {
		return false, nil
	}
	columns, problems := b.resolveViewColumns(ctx, analysis)
	if index := slices.IndexFunc(problems, func(problem *resolve.Error) bool {
		return problem != nil && problem.Code == diag.CodeInternalNilGuard
	}); index >= 0 {
		return false, problems[index]
	}
	if len(columns) != len(defined.view.Columns) || hasErrorProblem(problems) {
		return false, nil
	}
	changed := false
	for index := range columns {
		changed = applyAnalysedColumn(&defined.view.Columns[index], &columns[index]) || changed
	}
	return changed, nil
}

// TypeViewsFromDefinitions resolves external catalogue views from their SQL definitions.
//
// Dependent views are reanalysed until their types settle, for at most one round more
// than there are views. Definitions that cannot be analysed safely leave the source's
// column metadata unchanged. A panic in the dialect while parsing or analysing a
// definition is recovered, logged once with its stack, and leaves that view as it was.
//
// Takes engine (dialect.DialectPort) which parses and analyses the definitions.
// Takes catalogue (*sqlmodel.Catalogue) which holds the views; it is changed in place and
// must already carry the dialect's identifier rules (see Adopt).
//
// Returns []diag.SourceError which holds a Q070 error for each definition whose parsing
// or analysis panicked, and a Q071 warning when the views had not settled once the round
// limit was reached.
// Returns error when the context is cancelled, wrapping its cause.
func TypeViewsFromDefinitions(
	ctx context.Context,
	engine dialect.DialectPort,
	catalogue *sqlmodel.Catalogue,
) ([]diag.SourceError, error) {
	views, diagnostics, err := parseViewDefinitions(ctx, engine, catalogue)
	if err != nil || len(views) == 0 {
		return diagnostics, err
	}
	builder := viewTypingBuilder(engine, catalogue)
	for range len(views) + 1 {
		changed := false
		for index := range views {
			if err := context.Cause(ctx); err != nil {
				return diagnostics, fmt.Errorf("typing views from their definitions: %w", err)
			}
			retyped, problem := builder.retypeView(ctx, &views[index])
			changed = retyped || changed
			if problem != nil {
				diagnostics = append(diagnostics, *problem)
			}
		}
		if !changed {
			return diagnostics, nil
		}
	}
	diagnostics = append(diagnostics, diagbuild.Unlocated(diag.CodeViewDerivationUnsettled, diag.SeverityWarning,
		fmt.Sprintf("the columns of the %d views typed from their definitions did not settle, so some views may have "+
			"columns the database does not report; look for views that read each other in a cycle", len(views))))
	return diagnostics, nil
}

// parseViewDefinitions parses the definition of every view that has one, in schema and
// view order, keeping those that parse to exactly one statement.
//
// Takes engine (dialect.DialectPort) which parses the definitions.
// Takes catalogue (*sqlmodel.Catalogue) which holds the views.
//
// Returns []definedView which holds the views with their statements.
// Returns []diag.SourceError which holds a Q070 error for each definition whose parsing
// panicked.
// Returns error when the context is cancelled, wrapping its cause.
func parseViewDefinitions(
	ctx context.Context,
	engine dialect.DialectPort,
	catalogue *sqlmodel.Catalogue,
) ([]definedView, []diag.SourceError, error) {
	var views []definedView
	var diagnostics []diag.SourceError
	for _, view := range viewsWithDefinitions(catalogue) {
		if err := context.Cause(ctx); err != nil {
			return nil, diagnostics, fmt.Errorf("typing views from their definitions: %w", err)
		}
		statements, parseError := panicguard.RunValue(ctx, "catalogue: parse view definition", func() ([]ast.ParsedStatement, error) {
			return engine.ParseStatements(ctx, view.Definition)
		})
		if errors.Is(parseError, fault.ErrPanic) {
			diagnostics = append(diagnostics, viewPanic(view, "parsing", parseError))
			continue
		}
		if parseError != nil || len(statements) != 1 {
			continue
		}
		views = append(views, definedView{view: view, statement: statements[0], failed: false})
	}
	return views, diagnostics, nil
}

// viewPanic reports a panic recovered while parsing or analysing a view's definition.
//
// Takes view (*sqlmodel.View) which is the view.
// Takes doing (string) which says what panicked, such as "parsing".
// Takes err (error) which is the recovered panic.
//
// Returns diag.SourceError which is the Q070 error.
func viewPanic(view *sqlmodel.View, doing string, err error) diag.SourceError {
	return diagbuild.Unlocated(diag.CodeRecoveredPanic, diag.SeverityError,
		fmt.Sprintf("%s the definition of view %s: %v", doing, qualifiedName(view.Schema, view.Name), err))
}

// viewsWithDefinitions lists the views of a catalogue that carry a definition, in schema
// and view order.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the views; nil holds none.
//
// Returns []*sqlmodel.View which holds them.
func viewsWithDefinitions(catalogue *sqlmodel.Catalogue) []*sqlmodel.View {
	if catalogue == nil {
		return nil
	}
	var views []*sqlmodel.View
	for _, schemaKey := range slices.Sorted(maps.Keys(catalogue.Schemas)) {
		schema := catalogue.Schemas[schemaKey]
		if schema == nil {
			continue
		}
		for _, viewKey := range slices.Sorted(maps.Keys(schema.Views)) {
			if view := schema.Views[viewKey]; view != nil && strings.TrimSpace(view.Definition) != "" {
				views = append(views, view)
			}
		}
	}
	return views
}

// viewTypingBuilder returns a catalogue builder over an existing catalogue, for analysing
// view definitions against it as the migration replay does.
//
// Takes engine (dialect.DialectPort) which analyses the definitions.
// Takes catalogue (*sqlmodel.Catalogue) which the definitions are analysed against.
//
// Returns *catalogueBuilder which resolves view columns.
func viewTypingBuilder(engine dialect.DialectPort, catalogue *sqlmodel.Catalogue) *catalogueBuilder {
	return &catalogueBuilder{
		engine:                  engine,
		catalogue:               catalogue,
		calledFunctions:         nil,
		calledTableFunctions:    nil,
		prefixes:                namespace.Set{Canonical: "", Aliases: nil, Foreign: nil},
		columnOverrides:         nil,
		pendingColumnDirectives: nil,
		statementDiagnostics:    nil,
		partitionParents:        nil,
		inheritances:            nil,
		localColumns:            nil,
		statement:               statementSite{filename: "", line: 0},
		searchPaths:             newSearchPaths(catalogue.SearchPath),
		followingViews:          nil,
		lexicon:                 syntax.Lexicon{LinePrefix: "", Rules: sqllex.Rules{}},
	}
}

// hasErrorProblem reports whether any problem found resolving a definition is an error.
//
// Takes problems ([]*resolve.Error) which are the problems.
//
// Returns bool which is true when one has error severity.
func hasErrorProblem(problems []*resolve.Error) bool {
	return slices.ContainsFunc(problems, func(problem *resolve.Error) bool {
		return problem != nil && problem.Severity == diag.SeverityError
	})
}

// applyAnalysedColumn gives a view column what the analysis of the view's definition
// concluded about it: a known type, NOT NULL, and the type overrides of the column it
// projects unchanged where the view column has none.
//
// Takes target (*sqlmodel.Column) which is the view's column, changed in place.
// Takes analysed (*sqlmodel.Column) which is the analysed column at its position.
//
// Returns bool which is true when the column changed.
func applyAnalysedColumn(target, analysed *sqlmodel.Column) bool {
	changed := false
	if analysed.SQLType.Category != sqlmodel.TypeCategoryUnknown && !reflect.DeepEqual(target.SQLType, analysed.SQLType) {
		target.SQLType = analysed.SQLType
		changed = true
	}
	if target.Nullable && !analysed.Nullable {
		target.Nullable = false
		changed = true
	}
	if target.SQLTypeOverride == "" && analysed.SQLTypeOverride != "" {
		target.SQLTypeOverride = analysed.SQLTypeOverride
		changed = true
	}
	if target.GoTypeOverride == nil && analysed.GoTypeOverride != nil {
		target.GoTypeOverride = cloneGoType(analysed.GoTypeOverride)
		changed = true
	}
	if target.NullableOverride == nil && analysed.NullableOverride != nil {
		target.NullableOverride = new(*analysed.NullableOverride)
		changed = true
	}
	if target.NonNullElements == nil && analysed.NonNullElements != nil {
		target.NonNullElements = new(*analysed.NonNullElements)
		changed = true
	}
	if target.StructTags == nil && analysed.StructTags != nil {
		target.StructTags = maps.Clone(analysed.StructTags)
		changed = true
	}
	return changed
}

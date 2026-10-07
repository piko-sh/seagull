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

package resolve

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/directive"
	"seagull.sh/seagull/internal/observe"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// MaxExpressionResolveDepth bounds the analyser's recursion through an expression tree:
	// type resolution here, and the walks the checks and the catalogue builder make over the
	// same trees. A deeply nested expression, which a dialect's own parse-depth cap admits,
	// would otherwise overflow the stack.
	MaxExpressionResolveDepth = 256

	// maxInferExpressionNameDepth caps how deep CAST and COALESCE chains may recurse during
	// column-name inference so deeply nested parser output cannot exhaust the Go stack.
	maxInferExpressionNameDepth = 64
)

// TypeResolver performs bottom-up expression type inference, resolving raw output columns
// and parameter references from the dialect into fully typed results. This is where
// column references are looked up in the scope chain and function calls go through
// overload resolution.
//
// A TypeResolver is not safe for concurrent use: it keeps state while it types a query,
// such as the notices it collects, the depth it has reached and the comparisons of the
// query's parameters. Use one per goroutine.
type TypeResolver struct {
	// engine holds the dialect's type normalisation, promotion, and casting rules.
	engine dialect.TypeSystemPort

	// catalogue holds the database catalogue for table and type lookups.
	catalogue *sqlmodel.Catalogue

	// functionResolver holds the function overload resolver for function call expressions.
	functionResolver *FunctionResolver

	// operatorFeatures maps each operator that needs one to the expression feature it
	// requires, read once from the dialect's Info.
	operatorFeatures map[string]ast.ExpressionFeature

	// argumentNames maps the number of each parameter of the query being resolved that a
	// call passes by name to that name; it is set for the duration of ResolveParameters.
	argumentNames map[int]string

	// clauseExpressions holds the clause expressions of the query being resolved, where a
	// problem with a compared column finds the column's position; it is set for the duration
	// of ResolveParameters.
	clauseExpressions []ast.ClauseExpression

	// assigned holds the parameters the query being resolved assigns to a column only as
	// part of a larger expression, which the column's type does not type; it is set for the
	// duration of ResolveParameters.
	assigned assignedParameters

	// notices collects the problems found while typing an expression that still typed, such
	// as a function an engine resolved without declaring its data access; the output column
	// resolution reports and clears them.
	notices []*Error

	// depthLimits holds, one of each, the depth limits the walks that report nothing as they
	// go, the silent scope builder and the data-access walk, reached while the current query
	// was resolved; TakeDepthLimits reports and clears them.
	depthLimits []*Error

	// parameterComparisons maps the number of each parameter of the query being resolved
	// that an expression compares, where the dialect placed it in the expression, to what it
	// is compared with; it is set for the duration of ResolveParameters.
	parameterComparisons map[int]comparedValue

	// comparedParameters receives, while the comparisons of a query's parameters are
	// collected, what each parameter a predicate compares is compared with; nil otherwise.
	comparedParameters map[int]comparedValue

	// parameterColumns maps the number of each parameter of the query being resolved that a
	// predicate compares with a column, lists against one or bounds by one to that column;
	// it is set for the duration of ResolveParameters.
	parameterColumns map[int]comparedColumn

	// comparedColumns receives, while the comparisons of a query's parameters are collected,
	// the column each parameter a predicate compares is compared with; nil otherwise.
	comparedColumns map[int]comparedColumn

	// parameterPatterns holds the number of each parameter of the query being resolved that
	// a pattern-matching predicate matches a value against; it is set for the duration of
	// ResolveParameters.
	parameterPatterns map[int]bool

	// comparedPatterns receives, while the comparisons of a query's parameters are
	// collected, the number of each parameter a pattern-matching predicate matches a value
	// against; nil otherwise.
	comparedPatterns map[int]bool

	// parameterNames maps the number of each parameter of the query being resolved that a
	// predicate compares with a column to the name the column gives it; it is set for the
	// duration of ResolveParameters.
	parameterNames map[int]comparedName

	// comparedNames receives, while the comparisons of a query's parameters are collected,
	// the name the column a predicate compares each parameter with gives it; nil otherwise.
	comparedNames map[int]comparedName

	// lambdaParameters holds the parameters of the lambdas whose bodies are being typed,
	// innermost last; a name one declares refers to it while its body is typed.
	lambdaParameters []lambdaParameter

	// offeredElements holds the elements of the lists a call passes, offered to the lambda
	// argument of the call typed next, which takes them; nil otherwise.
	offeredElements []listElement

	// typedArguments holds, while the lambda arguments of a call are typed, the arguments of
	// the call already typed, by the expression written for each, so a lambda binding that
	// stands for one of them, as the list a reduce folds does, takes its type without typing
	// it again; nil otherwise.
	typedArguments map[ast.Expression]callArgument

	// resolvedCalls holds what the overload each call typed while a select list is resolved
	// reached says of it, so the list can be judged by those overloads; nil otherwise.
	resolvedCalls map[*ast.FunctionCallExpression]resolvedCall

	// core holds the dialect's types for the values the resolver types itself, read once
	// from the dialect.
	core dialect.CoreTypes

	// scopeRules holds the dialect's name-resolution rules, which the scopes the resolver
	// and its scope builders create are rooted in.
	scopeRules ScopeRules

	// expressions is the set of expression kinds the dialect supports, read once from its
	// Info.
	expressions ast.ExpressionFeature

	// ungroupedColumns is how the engine treats a column read outside every aggregate of a
	// query that aggregates without GROUP BY, read once from the dialect's Info.
	ungroupedColumns dialect.UngroupedColumnRule

	// expressionDepth bounds recursion through ResolveExpressionType so a deeply nested
	// expression tree cannot overflow the goroutine stack (a fatal, non-recoverable error).
	// The increment/decrement is balanced per call, so reusing a resolver across queries is
	// safe.
	expressionDepth int
}

// NewTypeResolver creates a type resolver with the given catalogue, function resolver,
// and dialect.
//
// Takes catalogue (*sqlmodel.Catalogue) which specifies the database catalogue for
// lookups.
// Takes functionResolver (*FunctionResolver) which specifies the function overload
// resolver.
// Takes engine (TypeRules) which supplies the dialect's type system rules and the
// expression kinds it supports.
//
// Returns *TypeResolver which holds the initialised type resolver.
func NewTypeResolver(
	catalogue *sqlmodel.Catalogue,
	functionResolver *FunctionResolver,
	engine TypeRules,
) *TypeResolver {
	info := engine.Info()
	return &TypeResolver{
		catalogue:            catalogue,
		functionResolver:     functionResolver,
		engine:               engine,
		operatorFeatures:     info.OperatorFeatures,
		core:                 engine.CoreTypes(),
		scopeRules:           ScopeRulesFor(engine),
		expressions:          info.Expressions,
		argumentNames:        nil,
		clauseExpressions:    nil,
		assigned:             assignedParameters{embedded: nil, partial: nil, calls: nil, operands: nil},
		parameterComparisons: nil,
		comparedParameters:   nil,
		parameterColumns:     nil,
		comparedColumns:      nil,
		parameterPatterns:    nil,
		comparedPatterns:     nil,
		parameterNames:       nil,
		comparedNames:        nil,
		notices:              nil,
		depthLimits:          nil,
		lambdaParameters:     nil,
		offeredElements:      nil,
		typedArguments:       nil,
		resolvedCalls:        nil,
		ungroupedColumns:     info.UngroupedColumns,
		expressionDepth:      0,
	}
}

// NewRootScope creates the outermost scope of a chain under the dialect's scope rules.
//
// Takes kind (ScopeKind) which specifies the scope kind.
//
// Returns *ScopeChain which holds the new scope.
func (r *TypeResolver) NewRootScope(kind ScopeKind) *ScopeChain {
	return NewRootScopeChain(kind, r.scopeRules)
}

// ResolveOutputColumns resolves raw output columns from the dialect into fully typed
// output columns using the scope chain for column lookups and function resolver for
// expression types.
//
// Every projected column the SQL returns has an output column, so a generated scan has a
// destination for each: one that cannot be resolved is kept as a nullable column of
// unknown type, with the problem reported. Resolution stops without a diagnostic when the
// context is cancelled or the dialect's resolver is interrupted; the caller reports the
// cancellation.
//
// Takes rawColumns ([]ast.RawOutputColumn) which specifies the unresolved output columns.
// Takes scope (*ScopeChain) which provides visible tables and columns for name
// resolution.
//
// Returns []sqlmodel.OutputColumn which holds the fully typed output columns.
// Returns bool which indicates whether any expression modifies data.
// Returns []*Error which holds the problems found.
func (r *TypeResolver) ResolveOutputColumns(
	ctx context.Context,
	rawColumns []ast.RawOutputColumn,
	scope *ScopeChain,
) ([]sqlmodel.OutputColumn, bool, []*Error) {
	projection := r.resolveProjection(ctx, rawColumns, scope, func(problem *Error, _ ast.RawOutputColumn, _ int) *Error {
		return problem
	})
	return projection.Columns, projection.DataModifying, projection.Problems
}

// ResolveParameters resolves raw parameter references from the dialect into fully typed
// query parameters using the scope chain for context-based type inference and directive
// declarations for overrides.
//
// Takes rawAnalysis (*ast.RawQueryAnalysis) which holds the unresolved query: its flat
// parameter list plus the subqueries whose parameters need their own scope.
// Takes scope (*ScopeChain) which provides visible tables and columns for name
// resolution.
// Takes parameterDirectives ([]*directive.ParameterDirective) which specifies the
// directive overrides for parameters.
//
// Returns []sqlmodel.QueryParameter which holds the fully typed parameters in ordinal
// order.
// Returns []*Error which holds the problems found.
func (r *TypeResolver) ResolveParameters(
	ctx context.Context,
	rawAnalysis *ast.RawQueryAnalysis,
	scope *ScopeChain,
	parameterDirectives []*directive.ParameterDirective,
) ([]sqlmodel.QueryParameter, []*Error) {
	ctx, end := observe.From(ctx).Start(ctx, observe.StageResolveParameters)
	defer end(nil)

	directiveNumberMap := make(map[int]*directive.ParameterDirective, len(parameterDirectives))
	directiveNameMap := make(map[string]*directive.ParameterDirective, len(parameterDirectives))
	for _, parameterDirective := range parameterDirectives {
		directiveNumberMap[parameterDirective.Number] = parameterDirective
		if parameterDirective.DirectiveName != "" {
			directiveNameMap[parameterDirective.DirectiveName] = parameterDirective
		}
	}

	parameterTypes := make(map[int]*sqlmodel.QueryParameter)

	var rawParameters []ast.RawParameterReference
	if rawAnalysis != nil {
		rawParameters = rawAnalysis.ParameterReferences
		compared := r.collectComparedParameters(ctx, rawAnalysis, scope)
		r.parameterComparisons, r.parameterColumns = compared.values, compared.columns
		r.parameterPatterns, r.parameterNames = compared.patterns, compared.names
		r.argumentNames = rawAnalysis.ArgumentNames
		r.clauseExpressions = rawAnalysis.ClauseExpressions
		r.assigned = classifyAssignedParameters(rawAnalysis)
		defer func() {
			r.argumentNames, r.clauseExpressions = nil, nil
			r.assigned = assignedParameters{embedded: nil, partial: nil, calls: nil, operands: nil}
			r.parameterComparisons, r.parameterColumns, r.parameterPatterns, r.parameterNames = nil, nil, nil, nil
		}()
	}

	subqueryParameters := make(map[int]bool)
	pass := &subqueryParameterPass{
		parameterTypes:     parameterTypes,
		directiveNumberMap: directiveNumberMap,
		directiveNameMap:   directiveNameMap,
		handled:            subqueryParameters,
		rootIdentities:     parameterIdentitySet(rawParameters),
	}
	r.resolveSubqueryParameters(ctx, rawAnalysis, scope, pass)

	flat := flatParameterPass{scope: scope, armScopes: r.compoundArmScopes(ctx, rawAnalysis, scope), skip: subqueryParameters}
	rawParameters = assignPositionalTargets(rawParameters, positionalTargets(rawAnalysis, scope))
	diagnostics := r.mergeRawParameters(ctx, rawParameters, flat, directiveNumberMap, directiveNameMap, parameterTypes)

	markQuantifiedArrays(parameterTypes, rawAnalysis)
	r.applyParameterDirectives(parameterTypes, parameterDirectives)

	parameters := collectParameters(parameterTypes)
	disambiguateParameterNames(parameters)
	return parameters, diagnostics
}

// TakeDepthLimits returns the depth limits the silent scope builder and the data-access
// walk reached since they were last taken, one of each, and clears them, so a walk that
// stopped short is reported rather than silent.
//
// Returns []*Error which holds a CodeDepthLimit warning per limit reached, or nil.
func (r *TypeResolver) TakeDepthLimits() []*Error {
	limits := r.depthLimits
	r.depthLimits = nil
	return limits
}

// noteDepthLimit records, once, a depth limit a walk that reports nothing as it goes
// reached.
//
// Takes problem (*Error) which is the CodeDepthLimit warning.
func (r *TypeResolver) noteDepthLimit(problem *Error) {
	for _, limit := range r.depthLimits {
		if *limit == *problem {
			return
		}
	}
	r.depthLimits = append(r.depthLimits, problem)
}

// takeNotices returns the notices collected since they were last taken, one of each
// problem noted more than once, in the order first noted, and clears them.
//
// Returns []*Error which holds the notices.
func (r *TypeResolver) takeNotices() []*Error {
	notices := r.notices
	r.notices = nil
	if len(notices) < 2 {
		return notices
	}
	seen := make(map[Error]struct{}, len(notices))
	distinct := make([]*Error, 0, len(notices))
	for _, notice := range notices {
		if _, noted := seen[*notice]; noted {
			continue
		}
		seen[*notice] = struct{}{}
		distinct = append(distinct, notice)
	}
	return distinct
}

// resolveSingleOutputColumn resolves a single raw output column into one or more typed
// output columns. Star expressions expand to multiple columns; column references and
// expressions each produce one, even when they cannot be resolved.
//
// Takes raw (ast.RawOutputColumn) which specifies the unresolved output column.
// Takes scope (*ScopeChain) which specifies the scope chain for column lookups.
// Takes dataModifying (*bool) which specifies a flag set to true if the expression
// modifies data.
//
// Returns []sqlmodel.OutputColumn which holds the resolved output columns.
// Returns *Error which describes why the column could not be resolved, or nil.
// Returns error which is the interruption that stopped the dialect's resolver while the
// column was typed, or nil.
func (r *TypeResolver) resolveSingleOutputColumn(
	ctx context.Context,
	raw ast.RawOutputColumn,
	scope *ScopeChain,
	dataModifying *bool,
) ([]sqlmodel.OutputColumn, *Error, error) {
	if raw.IsStar {
		starColumns, starError := r.expandStar(raw.TableAlias, scope)
		if starError != nil {
			return nil, starError, nil
		}
		return starColumns, nil, nil
	}

	if raw.ColumnName != "" {
		columns, problem := r.resolveColumnRefOutput(raw, scope)
		return columns, problem, nil
	}

	return r.resolveExpressionOutput(ctx, raw, scope, dataModifying)
}

// resolveColumnRefOutput resolves a projected column and its source metadata.
//
// Unresolved references still produce an output column with an unknown, nullable type,
// preserving the row's shape.
//
// Takes raw (ast.RawOutputColumn) which specifies the raw column reference.
// Takes scope (*ScopeChain) which specifies the scope chain for column lookups.
//
// Returns []sqlmodel.OutputColumn which holds the resolved output column.
// Returns *Error which describes why the reference could not be resolved, or nil.
func (*TypeResolver) resolveColumnRefOutput(
	raw ast.RawOutputColumn,
	scope *ScopeChain,
) ([]sqlmodel.OutputColumn, *Error) {
	outputName := raw.Name
	if outputName == "" {
		outputName = raw.ColumnName
	}

	column, table, resolveError := scope.ResolveColumn(raw.TableAlias, raw.ColumnName)
	if resolveError != nil {
		if row, _, isRow := rowValueInstead(scope, raw.TableAlias, raw.ColumnName, resolveError); isRow {
			return []sqlmodel.OutputColumn{NewOutputColumn(outputName, row.SQLType, row.Nullable)}, nil
		}
		return []sqlmodel.OutputColumn{unresolvedOutputColumn(outputName)}, asError(resolveError)
	}
	if column == nil {
		return []sqlmodel.OutputColumn{unresolvedOutputColumn(outputName)},
			newError(diag.CodeInternalNilGuard, "nil column resolved for %s", raw.ColumnName)
	}

	sourceTable := ""
	sourceSchema := ""
	sourceQualifier := ""
	if table != nil {
		sourceTable = table.Name
		sourceSchema = table.Schema
		sourceQualifier = table.Alias
	}

	return []sqlmodel.OutputColumn{{
		Name:            outputName,
		SQLType:         column.SQLType,
		Nullable:        column.Nullable,
		SourceTable:     sourceTable,
		SourceSchema:    sourceSchema,
		SourceColumn:    column.Name,
		SourceQualifier: sourceQualifier,
		GoTypeOverride:  nil,
		StructTags:      nil,
		EmbedTable:      "",
		EmbedName:       "",
		IsEmbedded:      false,
		EmbedIsOuter:    false,
	}}, nil
}

// resolveExpressionOutput resolves an expression-based output column by inferring its
// type through the expression type resolver.
//
// Takes raw (ast.RawOutputColumn) which specifies the raw expression column.
// Takes scope (*ScopeChain) which specifies the scope chain for expression resolution.
// Takes dataModifying (*bool) which specifies a flag set to true if the expression
// modifies data.
//
// Returns []sqlmodel.OutputColumn which holds the resolved output column.
// Returns *Error which describes why the expression could not be typed, under the code of
// the problem found, or nil.
// Returns error which is the interruption that stopped the dialect's resolver, in which
// case the column is not resolved and nothing is reported.
func (r *TypeResolver) resolveExpressionOutput(
	ctx context.Context,
	raw ast.RawOutputColumn,
	scope *ScopeChain,
	dataModifying *bool,
) ([]sqlmodel.OutputColumn, *Error, error) {
	sqlType, nullable, expressionError := r.resolveExpressionType(ctx, raw.Expression, scope, dataModifying)
	if interrupted(expressionError) {
		return nil, nil, expressionError
	}

	var problem *Error
	if expressionError != nil {
		sqlType = sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
		nullable = true
		problem = asError(expressionError)
	}

	outputName := raw.Name
	nameLimited := false
	if outputName == "" {
		outputName, nameLimited = inferExpressionNameWithinLimit(raw.Expression)
	}
	if outputName == "" {
		outputName = "?column?"
	}

	output := NewOutputColumn(outputName, sqlType, nullable)
	castLimited := applyCastColumnSource(&output, raw.Expression, scope)
	if nameLimited || castLimited {
		r.notices = append(r.notices, depthLimitError(
			"the expression of output column %q nests casts or COALESCE calls more than %d deep, so its name or source was not followed",
			outputName, maxInferExpressionNameDepth))
	}

	return []sqlmodel.OutputColumn{output}, problem, nil
}

// compoundArmScopes builds a scope for each other arm of a compound query, chained to the
// query's scope, holding the arm's own relations. The arms of a query nested
// MaxExpressionResolveDepth or more levels deep get none, and the limit is recorded for
// TakeDepthLimits.
//
// Takes rawAnalysis (*ast.RawQueryAnalysis) which is the query, or nil.
// Takes scope (*ScopeChain) which is the query's scope.
//
// Returns []*ScopeChain which holds one scope per arm, or nil for a query of one arm.
func (r *TypeResolver) compoundArmScopes(ctx context.Context, rawAnalysis *ast.RawQueryAnalysis, scope *ScopeChain) []*ScopeChain {
	if rawAnalysis == nil || len(rawAnalysis.CompoundBranches) == 0 {
		return nil
	}
	if scope.nestedTooDeeply() {
		r.subqueryScopeBuilder().noteSilentDepthLimit()
		return nil
	}
	scopes := make([]*ScopeChain, 0, len(rawAnalysis.CompoundBranches))
	for index := range rawAnalysis.CompoundBranches {
		arm := rawAnalysis.CompoundBranches[index].Query
		if arm == nil {
			continue
		}
		armScope := NewScopeChain(ScopeKindSubquery, scope)
		r.subqueryScopeBuilder().addRawTablesToScope(ctx, arm, armScope)
		scopes = append(scopes, armScope)
	}
	return scopes
}

// resolveParameterInArms types a parameter the query's scope could not in the scope of
// the first other arm of a compound query that resolves it, as a parameter a later arm
// compares with a column of that arm's own relations needs.
//
// Takes raw (ast.RawParameterReference) which is the parameter.
// Takes armScopes ([]*ScopeChain) which holds the arms' scopes.
//
// Returns sqlType (sqlmodel.SQLType) which is the parameter's type.
// Returns nullable (bool) which is true when the parameter is nullable.
// Returns found (bool) which is false when no arm resolves the parameter.
func (r *TypeResolver) resolveParameterInArms(
	ctx context.Context,
	raw ast.RawParameterReference,
	armScopes []*ScopeChain,
) (sqlType sqlmodel.SQLType, nullable bool, found bool) {
	for _, armScope := range armScopes {
		armType, armNullable, err := r.resolveParameterType(ctx, raw, armScope)
		if err == nil {
			return armType, armNullable, true
		}
	}
	return sqlmodel.SQLType{}, false, false
}

// mergeRawParameters resolves and merges parameter references, promoting types for
// repeated numbers and using compound-branch scopes when necessary. Parameters already
// typed in subqueries are skipped, and COALESCE fallbacks remain nullable.
//
// Takes rawParameters ([]ast.RawParameterReference) which specifies the unresolved
// references.
// Takes flat (flatParameterPass) which holds the scopes and the numbers to leave to the
// subquery pass.
// Takes directiveNumberMap (map[int]*directive.ParameterDirective) which specifies
// directives keyed by number.
// Takes directiveNameMap (map[string]*directive.ParameterDirective) which specifies
// directives keyed by name.
// Takes parameterTypes (map[int]*sqlmodel.QueryParameter) which accumulates the merged
// parameters keyed by number.
//
// Returns []*Error which holds the problems found, each reported once: the parameters an
// INSERT assigns to the columns of a table the catalogue does not hold all name the same
// unknown table, which is one problem, not one per parameter. The merge stops when the
// context is done.
func (r *TypeResolver) mergeRawParameters(
	ctx context.Context,
	rawParameters []ast.RawParameterReference,
	flat flatParameterPass,
	directiveNumberMap map[int]*directive.ParameterDirective,
	directiveNameMap map[string]*directive.ParameterDirective,
	parameterTypes map[int]*sqlmodel.QueryParameter,
) []*Error {
	problems := problemSet{diagnostics: nil, reported: make(map[Error]bool)}
	scope := flat.scope
	directTypes := make(map[int]sqlmodel.SQLType)

	for _, raw := range rawParameters {
		if ctx.Err() != nil {
			return problems.diagnostics
		}
		if flat.skip[raw.Number] {
			continue
		}
		sqlType, nullable, resolveError := r.typeFlatParameter(ctx, raw, flat)
		if resolveError != nil {
			problems.add(r.locateColumnProblem(asError(resolveError), raw.ColumnReference))
		}
		problems.add(r.conflictingUse(directTypes, parameterTypes[raw.Number], raw, scope))
		r.upsertParameterType(parameterTypes, raw, sqlType, nullable, directiveNumberMap, directiveNameMap)
		r.recordParameterColumn(parameterTypes[raw.Number], raw, scope)
	}

	return problems.diagnostics
}

// locateColumnProblem places a problem with the column a parameter is compared with at
// that column's name, when the problem has no place of its own and one of the query's
// clauses names the column where the dialect recorded its position.
//
// Takes problem (*Error) which is the problem; it is copied, never changed.
// Takes reference (*ast.ColumnReference) which is the compared column, or nil.
//
// Returns *Error which is the problem, placed at the column when its position is known.
func (r *TypeResolver) locateColumnProblem(problem *Error, reference *ast.ColumnReference) *Error {
	if problem == nil || reference == nil || problem.Span.Known() {
		return problem
	}
	for index := range r.clauseExpressions {
		if span, found := columnSpan(r.clauseExpressions[index].Expression, reference); found {
			located := *problem
			located.Span = Span{Start: span.Start, End: span.End}
			return &located
		}
	}
	return problem
}

// upsertParameterType records a resolved parameter in parameterTypes by number: it merges
// the type into an existing entry following cast and promotion rules, or inserts a new
// entry with the resolved display name. The flat outer pass and the subquery pass share
// it for identical merge semantics.
//
// Takes parameterTypes (map[int]*sqlmodel.QueryParameter) which accumulates parameters.
// Takes raw (ast.RawParameterReference) which is the parameter being recorded.
// Takes sqlType (sqlmodel.SQLType) which is the resolved type.
// Takes nullable (bool) which indicates whether the reference context is nullable.
// Takes directiveNumberMap (map[int]*directive.ParameterDirective) which specifies
// directives keyed by number.
// Takes directiveNameMap (map[string]*directive.ParameterDirective) which specifies
// directives keyed by name.
func (r *TypeResolver) upsertParameterType(
	parameterTypes map[int]*sqlmodel.QueryParameter,
	raw ast.RawParameterReference,
	sqlType sqlmodel.SQLType,
	nullable bool,
	directiveNumberMap map[int]*directive.ParameterDirective,
	directiveNameMap map[string]*directive.ParameterDirective,
) {
	if existing, exists := parameterTypes[raw.Number]; exists {
		r.mergeExistingParameterType(existing, sqlType, nullable, raw.CastType != nil)

		if raw.Context == sqlmodel.ParameterContextLimit || raw.Context == sqlmodel.ParameterContextOffset {
			if existing.IsPaginationBound() || existing.SQLType.Category == sqlmodel.TypeCategoryUnknown {
				existing.Context = raw.Context
			}
		}
		return
	}
	parameterTypes[raw.Number] = &sqlmodel.QueryParameter{
		Number:          raw.Number,
		Name:            r.parameterName(raw, directiveNumberMap, directiveNameMap),
		Placeholder:     "",
		SQLType:         sqlType,
		Nullable:        nullable,
		Context:         raw.Context,
		DefaultLimit:    nil,
		MaxLimit:        nil,
		GoTypeOverride:  nil,
		NonNullElements: nil,
		SortableColumns: nil,
		IsSlice:         false,
		IsOptional:      false,
		Kind:            sqlmodel.ParameterDirectiveNone,
	}
}

// mergeExistingParameterType merges a newly resolved type into an existing parameter
// entry. Cast types take precedence; otherwise the engine's type promotion rules apply.
//
// Takes existing (*sqlmodel.QueryParameter) which specifies the parameter to update.
// Takes sqlType (sqlmodel.SQLType) which specifies the newly resolved type.
// Takes nullable (bool) which specifies whether the new reference context is nullable.
// Takes hasCastType (bool) which specifies whether the reference included an explicit
// cast.
func (r *TypeResolver) mergeExistingParameterType(
	existing *sqlmodel.QueryParameter,
	sqlType sqlmodel.SQLType,
	nullable bool,
	hasCastType bool,
) {
	if hasCastType || (existing.SQLType.Category == sqlmodel.TypeCategoryUnknown && sqlType.Category != sqlmodel.TypeCategoryUnknown) {
		existing.SQLType = sqlType
	} else if existing.SQLType.Category != sqlmodel.TypeCategoryUnknown && sqlType.Category != sqlmodel.TypeCategoryUnknown {
		existing.SQLType = r.engine.PromoteType(existing.SQLType, sqlType)
	}
	if nullable {
		existing.Nullable = true
	}
}

// applyParameterDirectives applies directive overrides (type hints, nullability, kind) to
// the merged parameter map.
//
// A sortable directive, which no placeholder stands for, creates its own entry; any other
// directive for a number no placeholder has adds nothing, since the check passes report
// it (Q042) and a parameter for it would bind nothing.
//
// Takes parameterTypes (map[int]*sqlmodel.QueryParameter) which specifies the parameters
// to update.
// Takes parameterDirectives ([]*directive.ParameterDirective) which specifies the
// directives to apply.
func (r *TypeResolver) applyParameterDirectives(
	parameterTypes map[int]*sqlmodel.QueryParameter,
	parameterDirectives []*directive.ParameterDirective,
) {
	assignSortableNumbers(parameterTypes, parameterDirectives)

	for _, parameterDirective := range parameterDirectives {
		parameter, found := directiveParameter(parameterTypes, parameterDirective)
		if !found {
			continue
		}
		r.applyParameterDirective(parameter, parameterDirective)
	}
}

// applyParameterDirective applies one directive's name, type, nullability, limits and
// kind to its parameter. A declared type replaces any go_type the parameter took from the
// column it is compared with.
//
// Takes parameter (*sqlmodel.QueryParameter) which receives the directive.
// Takes parameterDirective (*directive.ParameterDirective) which is the directive.
func (r *TypeResolver) applyParameterDirective(
	parameter *sqlmodel.QueryParameter,
	parameterDirective *directive.ParameterDirective,
) {
	parameter.Name = parameterDirective.Name
	if parameterDirective.Anchor != parameterDirective.Name {
		parameter.Placeholder = parameterDirective.Anchor
	}

	if parameterDirective.TypeHint != nil {
		parameter.SQLType = r.resolveCustomType(r.engine.NormaliseTypeName(*parameterDirective.TypeHint))
		parameter.GoTypeOverride = nil
	}
	if parameterDirective.Nullable != nil {
		parameter.Nullable = *parameterDirective.Nullable
	}
	if parameterDirective.NonNullElements != nil {
		parameter.NonNullElements = new(*parameterDirective.NonNullElements)
	}

	parameter.IsSlice = parameterDirective.IsSlice
	parameter.IsOptional = parameterDirective.IsOptional
	if parameterDirective.IsOptional {
		parameter.Nullable = true
	}
	if parameterDirective.DefaultValue != nil {
		parameter.DefaultLimit = parameterDirective.DefaultValue
	}
	if parameterDirective.MaxValue != nil {
		parameter.MaxLimit = parameterDirective.MaxValue
	}

	parameter.Kind = parameterDirective.Kind
	r.applyDirectiveKind(parameter, parameterDirective)
}

// applyDirectiveKind copies the sortable allow-list and makes a sortable parameter
// non-nullable. Other parameter kinds need no changes here.
//
// Takes parameter (*sqlmodel.QueryParameter) which specifies the parameter to modify.
// Takes parameterDirective (*directive.ParameterDirective) which specifies the directive
// with kind and constraints.
func (*TypeResolver) applyDirectiveKind(
	parameter *sqlmodel.QueryParameter,
	parameterDirective *directive.ParameterDirective,
) {
	if parameterDirective.Kind == sqlmodel.ParameterDirectiveSortable {
		parameter.SortableColumns = parameterDirective.Columns
		parameter.Nullable = false
	}
}

// expandStar expands a SELECT * or table.* into fully typed output columns by reading all
// columns from the matching table or CTE in the scope.
//
// Takes tableAlias (string) which specifies the table to expand, or empty for all tables.
// Takes scope (*ScopeChain) which specifies the scope chain for table lookups.
//
// Returns []sqlmodel.OutputColumn which holds the expanded output columns.
// Returns *Error which is a Q003 error when the specified table alias is unknown.
func (*TypeResolver) expandStar(
	tableAlias string,
	scope *ScopeChain,
) ([]sqlmodel.OutputColumn, *Error) {
	if tableAlias != "" {
		return expandQualifiedStar(tableAlias, scope)
	}

	starColumns := scope.StarColumns()
	outputColumns := make([]sqlmodel.OutputColumn, len(starColumns))
	for index := range starColumns {
		if starColumns[index].Table == nil {
			outputColumns[index] = NewOutputColumn(starColumns[index].Column.Name, starColumns[index].Column.SQLType, starColumns[index].Column.Nullable)
			continue
		}
		table := starColumns[index].Table
		outputColumns[index] = starOutputColumn(starColumns[index].Column, table.Name, table.Schema, table.Alias)
	}
	return outputColumns, nil
}

// resolveParameterType infers a parameter's SQL type and nullability.
//
// The type comes from the cast type, the referenced column, what an expression the
// dialect placed it in compares it with, the called function's signature, or the usage
// context, in that order. A parameter a predicate compares with an expression that is no
// column alone, such as my_macro(col), takes no type from a column the dialect records
// for it: it takes the expression's type, or none when that is unknown. A parameter a
// statement assigns to a column only as part of a larger expression skips the column and
// is typed from the expression (resolveEmbeddedParameterType). A function argument is
// nullable when the signature takes NULL there as a value. When the column reference
// cannot be resolved against the scope chain the resolver falls back to a catalogue-wide
// lookup so subquery parameters (whose inner scope the dialect does not preserve) still
// get a type; ambiguous fallbacks let the original Q001/Q002 diagnostic stand.
//
// Takes raw (ast.RawParameterReference) which specifies the raw parameter to resolve.
// Takes scope (*ScopeChain) which specifies the scope chain for column reference lookups.
//
// Returns sqlmodel.SQLType which holds the inferred SQL type.
// Returns bool which indicates whether the parameter is nullable; an explicit type that
// is itself nullable, such as ClickHouse's {name:Nullable(String)}, makes it so.
// Returns error which indicates a scope resolution failure that the catalogue fallback
// could not disambiguate.
func (r *TypeResolver) resolveParameterType(
	ctx context.Context,
	raw ast.RawParameterReference,
	scope *ScopeChain,
) (sqlmodel.SQLType, bool, error) {
	if raw.CastType != nil {
		return r.resolveCustomType(*raw.CastType), raw.CastType.Nullable, nil
	}
	if raw.Context == sqlmodel.ParameterContextLike {
		return r.resolveLikeParameterType(raw, scope)
	}
	embedded := r.embeddedAssignment(raw)
	if raw.ColumnReference != nil && !embedded && !r.comparedWithExpression(raw.Number) {
		return r.resolveColumnReferencedParameterType(raw, scope)
	}
	if compared, found := r.parameterComparisons[raw.Number]; found {
		return compared.sqlType, compared.nullable, nil
	}

	if embedded {
		return r.resolveEmbeddedParameterType(ctx, raw, scope)
	}
	if raw.Context == sqlmodel.ParameterContextFunctionArgument && raw.EnclosingFunctionName != "" {
		return r.resolveFunctionArgumentParameterType(raw)
	}
	return r.resolveContextOnlyParameterType(raw.Context), false, nil
}

// resolveFunctionArgumentParameterType types a parameter passed to a function from the
// function's signature: by the parameter's name when the call passes it by name, and by
// its position otherwise.
//
// Takes raw (ast.RawParameterReference) which is the parameter, with its enclosing
// function and argument position.
//
// Returns sqlmodel.SQLType which is the declared type, or the context's type when the
// signature gives none.
// Returns bool which is true when the signature takes NULL there as a value.
// Returns error which is always nil.
func (r *TypeResolver) resolveFunctionArgumentParameterType(raw ast.RawParameterReference) (sqlmodel.SQLType, bool, error) {
	if argumentName, named := r.argumentNames[raw.Number]; named && argumentName != "" {
		nullable := r.functionResolver.namedArgumentAcceptsNull(raw.EnclosingFunctionName, argumentName)
		if argumentType, resolved := r.functionResolver.NamedArgumentType(raw.EnclosingFunctionName, argumentName); resolved {
			return r.resolveCustomType(argumentType), nullable, nil
		}
		return r.resolveContextOnlyParameterType(raw.Context), nullable, nil
	}
	nullable := r.functionResolver.ArgumentAcceptsNull(raw.EnclosingFunctionName, raw.ArgumentOrdinal)
	if argumentType, resolved := r.functionResolver.ArgumentType(raw.EnclosingFunctionName, raw.ArgumentOrdinal); resolved {
		return r.resolveCustomType(argumentType), nullable, nil
	}
	return r.resolveContextOnlyParameterType(raw.Context), nullable, nil
}

// resolveLikeParameterType returns the type for a LIKE-pattern parameter, always text,
// while still surfacing a Q001-style error when the LHS column reference resolves neither
// in scope, nor in the comparison the pattern's expression makes, nor via catalogue
// fallback.
//
// Takes raw (ast.RawParameterReference) which specifies the raw parameter under
// resolution.
// Takes scope (*ScopeChain) which specifies the active scope chain.
//
// Returns sqlmodel.SQLType which is always text for LIKE parameters.
// Returns bool which is always false (LIKE parameters are not nullable based on the
// column).
// Returns error which surfaces unresolved column references so the diagnostic pass can
// emit Q001.
func (r *TypeResolver) resolveLikeParameterType(
	raw ast.RawParameterReference,
	scope *ScopeChain,
) (sqlmodel.SQLType, bool, error) {
	likeType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "")
	if raw.ColumnReference == nil || raw.ColumnReference.ColumnName == "" {
		return likeType, false, nil
	}
	_, _, err := scope.ResolveColumn(raw.ColumnReference.TableAlias, raw.ColumnReference.ColumnName)
	if err == nil {
		return likeType, false, nil
	}
	if _, compared := r.parameterComparisons[raw.Number]; compared {
		return likeType, false, nil
	}
	if _, ok := r.findColumnInCatalogue(raw.ColumnReference); ok {
		return likeType, false, nil
	}
	if _, ok := resolveBareColumnFallback(scope, raw.ColumnReference); ok {
		return likeType, false, nil
	}
	return likeType, false, err
}

// resolveColumnReferencedParameterType finds the type of a parameter's target column.
//
// If the active scope cannot resolve it, comparison-based inference and catalogue lookup
// provide fallbacks.
//
// Takes raw (ast.RawParameterReference) which is the parameter, whose ColumnReference
// names the column it is compared against or assigned to.
// Takes scope (*ScopeChain) which specifies the active scope chain.
//
// Returns sqlmodel.SQLType which holds the resolved column type or Unknown when neither
// the scope nor the catalogue could match.
// Returns bool which indicates whether the column is nullable.
// Returns error which surfaces unresolved column references so the diagnostic pass can
// emit Q001.
func (r *TypeResolver) resolveColumnReferencedParameterType(
	raw ast.RawParameterReference,
	scope *ScopeChain,
) (sqlmodel.SQLType, bool, error) {
	reference := raw.ColumnReference
	column, _, err := scope.ResolveColumn(reference.TableAlias, reference.ColumnName)
	if err != nil {
		if compared, found := r.parameterComparisons[raw.Number]; found {
			return compared.sqlType, compared.nullable, nil
		}
		if fallback, ok := r.findColumnInCatalogue(reference); ok {
			return fallback.sqlType, fallback.nullable, nil
		}
		if bare, ok := resolveBareColumnFallback(scope, reference); ok {
			return bare.SQLType, bare.Nullable, nil
		}
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), false, err
	}
	if column == nil {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), false, nil
	}
	return column.SQLType, column.Nullable, nil
}

// findColumnInCatalogue resolves a column via a catalogue-wide lookup.
//
// It searches every schema and returns the column's type only when exactly one table
// holds it. A non-empty TableAlias narrows the search to tables with that name (per the
// SQL convention of using bare table names as default aliases); an empty TableAlias scans
// all tables and refuses ambiguous results so the genuine Q001/Q002 diagnostic can fire.
// Comparison follows the catalogue's identifier rules. Views are skipped because their
// column types come from a SELECT body that this fallback does not re-resolve.
//
// Takes reference (*ast.ColumnReference) which specifies the column to look up.
//
// Returns catalogueColumnMatch which holds the matched column's type and nullability when
// ok is true.
// Returns bool which is true when exactly one column matched.
func (r *TypeResolver) findColumnInCatalogue(
	reference *ast.ColumnReference,
) (catalogueColumnMatch, bool) {
	return findColumnInCatalogueFor(r.catalogue, reference)
}

// parameterName chooses a parameter's display name from directives and SQL context,
// preferring names of compared columns over function argument names. The caller resolves
// duplicate names.
//
// Takes raw (ast.RawParameterReference) which specifies the raw parameter reference.
// Takes directiveNumberMap (map[int]*directive.ParameterDirective) which specifies
// directives keyed by number.
// Takes directiveNameMap (map[string]*directive.ParameterDirective) which specifies
// directives keyed by name.
//
// Returns string which holds the resolved parameter name.
func (r *TypeResolver) parameterName(
	raw ast.RawParameterReference,
	directiveNumberMap map[int]*directive.ParameterDirective,
	directiveNameMap map[string]*directive.ParameterDirective,
) string {
	if name, named := declaredParameterName(raw, directiveNumberMap, directiveNameMap); named {
		return name
	}
	if argumentName, named := r.argumentNames[raw.Number]; named && argumentName != "" {
		return argumentName
	}
	compared, comparedFound := r.parameterNames[raw.Number]
	if comparedFound && compared.bound {
		return compared.name
	}
	if raw.ColumnReference != nil && raw.ColumnReference.ColumnName != "" {
		if raw.Context == sqlmodel.ParameterContextLike {
			return raw.ColumnReference.ColumnName + "_like"
		}
		return raw.ColumnReference.ColumnName
	}
	if comparedFound {
		return compared.name
	}
	return r.fallbackParameterName(raw)
}

// resolveContextOnlyParameterType returns the type that a parameter takes from its
// surrounding context alone, used when no cast or column reference is available.
//
// Takes parameterContext (sqlmodel.ParameterContext) which specifies the SQL context the
// parameter appears in.
//
// Returns sqlmodel.SQLType which is the dialect's row-count type for LIMIT/OFFSET and
// Unknown otherwise.
func (r *TypeResolver) resolveContextOnlyParameterType(parameterContext sqlmodel.ParameterContext) sqlmodel.SQLType {
	switch parameterContext {
	case sqlmodel.ParameterContextLimit, sqlmodel.ParameterContextOffset:
		return r.core.RowCount
	default:
	}
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
}

// TypeRules is the part of a dialect the type resolver reads: the type system and the
// supported expression kinds from its Info.
type TypeRules interface {
	dialect.TypeSystemPort

	// Info describes the dialect.
	//
	// Returns dialect.Info which holds the dialect's capabilities.
	Info() dialect.Info
}

// catalogueColumnMatch holds the type information for a column found by a catalogue-wide
// lookup.
type catalogueColumnMatch struct {
	// sqlType is the resolved SQL type of the matched column.
	sqlType sqlmodel.SQLType

	// nullable indicates whether the matched column accepts NULL values.
	nullable bool
}

// flatParameterPass is what the flat pass over a query's parameters resolves them in.
type flatParameterPass struct {
	// scope is the query's scope.
	scope *ScopeChain

	// skip holds the parameter numbers the subquery pass typed already.
	skip map[int]bool

	// armScopes holds the scopes of the other arms of a compound query, in which a parameter
	// the query's scope cannot resolve is looked for; nil for a query of one arm.
	armScopes []*ScopeChain
}

// unresolvedOutputColumn is the column a projection that cannot be resolved stands for:
// nullable and of unknown type, so it still takes a scan destination.
//
// Takes name (string) which is the column's name.
//
// Returns sqlmodel.OutputColumn which is the placeholder column.
func unresolvedOutputColumn(name string) sqlmodel.OutputColumn {
	return NewOutputColumn(name, sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true)
}

// columnSpan finds, in an expression, a reference to a column with the given name, and
// the given qualifier when both carry one, whose position the dialect recorded. The
// position only places a diagnostic, so names are compared without regard to case.
//
// Takes expression (ast.Expression) which is the expression to search, outside any
// subquery.
// Takes reference (*ast.ColumnReference) which names the column.
//
// Returns ast.Span which is the position of the first such reference.
// Returns bool which is true when one was found.
func columnSpan(expression ast.Expression, reference *ast.ColumnReference) (ast.Span, bool) {
	for node := range preorder(expression) {
		column, isColumn := node.(*ast.ColumnRefExpression)
		if !isColumn {
			continue
		}
		if span, found := placedReferenceSpan(column, reference); found {
			return span, true
		}
	}
	return ast.Span{}, false
}

// placedReferenceSpan reports where a column reference is written when it names the given
// column, comparing names without regard to case and qualifiers only when both carry one.
//
// Takes column (*ast.ColumnRefExpression) which is the reference, or nil.
// Takes reference (*ast.ColumnReference) which names the column.
//
// Returns ast.Span which is the reference's position.
// Returns bool which is true when the reference names the column and its position is
// known.
func placedReferenceSpan(column *ast.ColumnRefExpression, reference *ast.ColumnReference) (ast.Span, bool) {
	if column == nil || !column.Span.Known() || !strings.EqualFold(column.ColumnName, reference.ColumnName) {
		return ast.Span{}, false
	}
	if column.TableAlias != "" && reference.TableAlias != "" && !strings.EqualFold(column.TableAlias, reference.TableAlias) {
		return ast.Span{}, false
	}
	return column.Span, true
}

// directiveParameter finds the parameter a directive describes. Only a sortable
// directive, whose placeholder the ORDER BY it builds supplies, adds a parameter the SQL
// does not reference.
//
// Takes parameterTypes (map[int]*sqlmodel.QueryParameter) which maps parameter numbers to
// the parameters found so far, and receives a sortable directive's parameter.
// Takes parameterDirective (*directive.ParameterDirective) which is the directive.
//
// Returns *sqlmodel.QueryParameter which is the parameter.
// Returns bool which is false when the directive describes no parameter.
func directiveParameter(
	parameterTypes map[int]*sqlmodel.QueryParameter,
	parameterDirective *directive.ParameterDirective,
) (*sqlmodel.QueryParameter, bool) {
	if parameter, exists := parameterTypes[parameterDirective.Number]; exists {
		return parameter, true
	}
	if parameterDirective.Kind != sqlmodel.ParameterDirectiveSortable {
		return nil, false
	}
	parameter := &sqlmodel.QueryParameter{
		Number:          parameterDirective.Number,
		Name:            parameterDirective.Name,
		Placeholder:     "",
		SQLType:         sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
		Nullable:        false,
		DefaultLimit:    nil,
		MaxLimit:        nil,
		GoTypeOverride:  nil,
		NonNullElements: nil,
		SortableColumns: nil,
		IsSlice:         false,
		IsOptional:      false,
		Kind:            sqlmodel.ParameterDirectiveNone,
		Context:         sqlmodel.ParameterContextComparison,
	}
	parameterTypes[parameterDirective.Number] = parameter
	return parameter, true
}

// applyCastColumnSource records the underlying source column of an output expression that
// is a column reference wrapped in nothing but CASTs, so a cast-only projection such as
// page_id::content.uuid_v4 keeps the source metadata a bare column reference would carry.
//
// Cast-only projections retain their source for the dynamic runtime builder's allow-list
// and for column overrides and array wrapping. The expression resolver still determines
// the cast result's type and nullability. Other expression shapes, such as a function
// call, arithmetic or COALESCE, are left without a source.
//
// Takes output (*sqlmodel.OutputColumn) which receives the resolved source metadata.
// Takes expression (ast.Expression) which is the output column's expression.
// Takes scope (*ScopeChain) which resolves the underlying column.
//
// Returns bool which is true when the chain of casts ran past the depth limit, so the
// column behind it was not found.
func applyCastColumnSource(
	output *sqlmodel.OutputColumn,
	expression ast.Expression,
	scope *ScopeChain,
) bool {
	reference, limited := unwrapCasts(expression)
	if reference == nil {
		return limited
	}
	column, table, resolveError := scope.ResolveColumn(reference.TableAlias, reference.ColumnName)
	if resolveError != nil || column == nil {
		return false
	}
	output.SourceColumn = column.Name
	if table != nil {
		output.SourceTable = table.Name
		output.SourceSchema = table.Schema
		output.SourceQualifier = table.Alias
	}
	return false
}

// unwrapCastToColumnRef peels any chain of CAST expressions and returns the column
// reference they wrap, or nil when the expression is not a column reference behind zero
// or more casts.
//
// Takes expression (ast.Expression) which is the expression to peel.
//
// Returns *ast.ColumnRefExpression which is the wrapped column reference, or nil when the
// expression is not a column behind zero or more casts.
func unwrapCastToColumnRef(expression ast.Expression) *ast.ColumnRefExpression {
	reference, _ := unwrapCasts(expression)
	return reference
}

// unwrapCasts peels a chain of CAST expressions as unwrapCastToColumnRef does, and says
// when the chain ran past the depth limit.
//
// Takes expression (ast.Expression) which is the expression to peel.
//
// Returns *ast.ColumnRefExpression which is the wrapped column reference, or nil.
// Returns bool which is true when the chain is longer than MaxExpressionResolveDepth, so
// it was not followed to its end.
func unwrapCasts(expression ast.Expression) (*ast.ColumnRefExpression, bool) {
	for range MaxExpressionResolveDepth {
		switch typed := expression.(type) {
		case *ast.ColumnRefExpression:
			return typed, false
		case *ast.CastExpression:
			if typed == nil {
				return nil, false
			}
			expression = typed.Inner
		default:
			return nil, false
		}
	}
	_, stillCast := expression.(*ast.CastExpression)
	_, column := expression.(*ast.ColumnRefExpression)
	return nil, stillCast || column
}

// inferExpressionName produces a reasonable column name for an unaliased SELECT
// expression.
//
// SQL does not require expressions in the projection to have a name, and PostgreSQL falls
// back to the literal placeholder "?column?", which is not a valid Go identifier. To keep
// generated row structs usable, this helper walks common expression shapes (column refs,
// casts, COALESCE, function calls) and lifts the most descriptive underlying name.
// Callers fall back to "?column?" when this returns the empty string.
//
// Takes expression (ast.Expression) which is the unaliased SELECT expression.
//
// Returns string which is the inferred column name, or empty when no sensible name can be
// derived.
func inferExpressionName(expression ast.Expression) string {
	name, _ := inferExpressionNameWithinLimit(expression)
	return name
}

// inferExpressionNameWithinLimit infers a column name as inferExpressionName does, and
// says when the expression nests casts and COALESCE calls more deeply than the inference
// follows.
//
// Takes expression (ast.Expression) which is the unaliased SELECT expression.
//
// Returns string which is the inferred column name, or empty.
// Returns bool which is true when the depth limit stopped the inference.
func inferExpressionNameWithinLimit(expression ast.Expression) (string, bool) {
	name := inferExpressionNameDepth(expression, 0)
	if name != "" {
		return name, false
	}
	return "", nameNestingReachesLimit(expression, 0)
}

// nameNestingReachesLimit reports whether the casts and COALESCE calls name inference
// descends through nest as deep as maxInferExpressionNameDepth.
//
// Takes expression (ast.Expression) which is the expression.
// Takes depth (int) which is the current depth.
//
// Returns bool which is true when the nesting reaches the limit.
func nameNestingReachesLimit(expression ast.Expression, depth int) bool {
	if depth >= maxInferExpressionNameDepth {
		return expression != nil
	}
	switch typed := expression.(type) {
	case *ast.CastExpression:
		return typed != nil && nameNestingReachesLimit(typed.Inner, depth+1)
	case *ast.CoalesceExpression:
		if typed == nil {
			return false
		}
		for _, argument := range typed.Arguments {
			if nameNestingReachesLimit(argument, depth+1) {
				return true
			}
		}
	}
	return false
}

// inferExpressionNameDepth walks an expression to infer a column name, stopping once the
// recursion depth reaches maxInferExpressionNameDepth.
//
// Takes expression (ast.Expression) which is the expression to inspect.
// Takes depth (int) which is the current recursion depth.
//
// Returns string which is the inferred column name, or empty when none applies.
func inferExpressionNameDepth(expression ast.Expression, depth int) string {
	if depth >= maxInferExpressionNameDepth {
		return ""
	}
	if expression == nil {
		return ""
	}
	switch typed := expression.(type) {
	case *ast.ColumnRefExpression:
		return inferColumnRefName(typed)
	case *ast.CastExpression:
		return inferCastName(typed, depth)
	case *ast.CoalesceExpression:
		return inferCoalesceName(typed, depth)
	case *ast.FunctionCallExpression:
		return inferFunctionCallName(typed)
	case *ast.ScalarSubqueryExpression:
		return inferScalarSubqueryName(typed, depth)
	default:
		return ""
	}
}

// inferScalarSubqueryName returns the name of the one column a scalar subquery selects,
// as an unaliased scalar subquery takes it: its alias or column name, or the name its
// expression infers.
//
// Takes expression (*ast.ScalarSubqueryExpression) which is the scalar subquery.
// Takes depth (int) which is the current recursion depth.
//
// Returns string which is the column's name, or empty when none applies.
func inferScalarSubqueryName(expression *ast.ScalarSubqueryExpression, depth int) string {
	if expression == nil || expression.InnerQuery == nil || len(expression.InnerQuery.OutputColumns) == 0 {
		return ""
	}
	column := expression.InnerQuery.OutputColumns[0]
	switch {
	case column.IsStar:
		return ""
	case column.Name != "":
		return column.Name
	case column.ColumnName != "":
		return column.ColumnName
	default:
		return inferExpressionNameDepth(column.Expression, depth+1)
	}
}

// inferColumnRefName returns the column name carried by a column reference, or empty when
// the expression is nil.
//
// Takes expression (*ast.ColumnRefExpression) which is the column reference.
//
// Returns string which is the column name, or empty when expression is nil.
func inferColumnRefName(expression *ast.ColumnRefExpression) string {
	if expression == nil {
		return ""
	}
	return expression.ColumnName
}

// inferCastName recurses into the inner expression of a CAST so the resulting column
// inherits the inner reference's name where possible.
//
// Takes expression (*ast.CastExpression) which is the cast expression.
// Takes depth (int) which is the current recursion depth.
//
// Returns string which is the inner expression's name, or empty when none applies.
func inferCastName(expression *ast.CastExpression, depth int) string {
	if expression == nil {
		return ""
	}
	return inferExpressionNameDepth(expression.Inner, depth+1)
}

// inferCoalesceName returns the first non-empty name found by recursing into a COALESCE
// argument list. Mirrors the convention that COALESCE inherits the name of its first
// defaulted column.
//
// Takes expression (*ast.CoalesceExpression) which is the COALESCE expression.
// Takes depth (int) which is the current recursion depth.
//
// Returns string which is the first non-empty argument name, or empty when none applies.
func inferCoalesceName(expression *ast.CoalesceExpression, depth int) string {
	if expression == nil {
		return ""
	}
	for _, argument := range expression.Arguments {
		if name := inferExpressionNameDepth(argument, depth+1); name != "" {
			return name
		}
	}
	return ""
}

// inferFunctionCallName returns the function name as the inferred column name. This
// matches the common Postgres convention where an unaliased function-call projection gets
// the function name.
//
// Takes expression (*ast.FunctionCallExpression) which is the function call.
//
// Returns string which is the function name, or empty when expression is nil.
func inferFunctionCallName(expression *ast.FunctionCallExpression) string {
	if expression == nil {
		return ""
	}
	return expression.FunctionName
}

// positionalName is the name a parameter falls back to when nothing names it: p and its
// number.
//
// Takes number (int) which is the parameter's number.
//
// Returns string which is the name, such as p2.
func positionalName(number int) string {
	return fmt.Sprintf("p%d", number)
}

// disambiguateParameterNames suffixes duplicate parameter names with a 1-based ordinal so
// each name is unique within the query.
//
// A name the query, a directive or a signature gives keeps its bare form ahead of a
// positional pN fallback that happens to spell the same; otherwise the first occurrence
// keeps its bare name and later ones get suffixes (`_2`, `_3`, ...) in declaration order,
// so suffixes are stable across runs.
//
// Takes parameters ([]sqlmodel.QueryParameter) which holds the parameters to disambiguate
// in place; the caller must pass them in ordinal (Number-ascending) order.
func disambiguateParameterNames(parameters []sqlmodel.QueryParameter) {
	if len(parameters) < 2 {
		return
	}

	counts := make(map[string]int, len(parameters))
	for index := range parameters {
		counts[parameters[index].Name]++
	}

	order := make([]int, 0, len(parameters))
	for index := range parameters {
		if parameters[index].Name != positionalName(parameters[index].Number) {
			order = append(order, index)
		}
	}
	for index := range parameters {
		if parameters[index].Name == positionalName(parameters[index].Number) {
			order = append(order, index)
		}
	}

	seen := make(map[string]int, len(parameters))
	for _, index := range order {
		name := parameters[index].Name
		if counts[name] < 2 {
			continue
		}
		seen[name]++
		if seen[name] == 1 {
			continue
		}
		parameters[index].Name = fmt.Sprintf("%s_%d", name, seen[name])
	}
}

// assignSortableNumbers gives each standalone sortable directive a parameter number after
// the highest bound placeholder (and any other directive) so sortable inputs appear last
// in the generated params struct and never collide with a bound placeholder. Sortable
// directives bind no placeholder, so they start with number zero until assigned here.
//
// Takes parameterTypes (map[int]*sqlmodel.QueryParameter) whose keys are the bound
// placeholder numbers resolved so far.
// Takes parameterDirectives ([]*directive.ParameterDirective) which are mutated in place.
func assignSortableNumbers(
	parameterTypes map[int]*sqlmodel.QueryParameter,
	parameterDirectives []*directive.ParameterDirective,
) {
	maxNumber := 0
	for number := range parameterTypes {
		if number > maxNumber {
			maxNumber = number
		}
	}
	for _, parameterDirective := range parameterDirectives {
		if parameterDirective.Kind != sqlmodel.ParameterDirectiveSortable && parameterDirective.Number > maxNumber {
			maxNumber = parameterDirective.Number
		}
	}
	for _, parameterDirective := range parameterDirectives {
		if parameterDirective.Kind == sqlmodel.ParameterDirectiveSortable {
			maxNumber++
			parameterDirective.Number = maxNumber
		}
	}
}

// collectParameters converts the parameter map into an ordered slice, sorted by parameter
// number from 1 to the maximum number. Missing numbers are skipped.
//
// Takes parameterTypes (map[int]*sqlmodel.QueryParameter) which specifies the parameters
// keyed by number.
//
// Returns []sqlmodel.QueryParameter which holds the parameters in ordinal order.
func collectParameters(parameterTypes map[int]*sqlmodel.QueryParameter) []sqlmodel.QueryParameter {
	numbers := slices.Sorted(maps.Keys(parameterTypes))
	result := make([]sqlmodel.QueryParameter, 0, len(numbers))
	for _, number := range numbers {
		result = append(result, *parameterTypes[number])
	}

	return result
}

// expandQualifiedStar expands a table.* into the columns of the table or CTE the
// qualifier names, preferring a table in the scope over a CTE of the same name.
//
// Takes tableAlias (string) which specifies the table or CTE to expand.
// Takes scope (*ScopeChain) which specifies the scope chain for table lookups.
//
// Returns []sqlmodel.OutputColumn which holds the expanded output columns.
// Returns *Error which is a Q003 error when the table alias is unknown.
func expandQualifiedStar(tableAlias string, scope *ScopeChain) ([]sqlmodel.OutputColumn, *Error) {
	if table, exists := scope.Table(tableAlias); exists {
		return scopedTableStarColumns(table), nil
	}
	if cte, exists := scope.CTE(tableAlias); exists {
		outputColumns := make([]sqlmodel.OutputColumn, len(cte.columns))
		for index := range cte.columns {
			outputColumns[index] = starOutputColumn(cte.columns[index], cte.name, "", tableAlias)
		}
		return outputColumns, nil
	}
	return nil, newError(diag.CodeUnknownTable, "unknown table %q in SELECT *", tableAlias).
		withSuggestion(scope.suggest(tableAlias, scope.relationNames))
}

// scopedTableStarColumns expands every column of a scoped table, attributing each to the
// table and the reference that qualifies it.
//
// Takes table (*ScopedTable) which specifies the table to expand.
//
// Returns []sqlmodel.OutputColumn which holds one output column per table column.
func scopedTableStarColumns(table *ScopedTable) []sqlmodel.OutputColumn {
	outputColumns := make([]sqlmodel.OutputColumn, len(table.Columns))
	for index := range table.Columns {
		outputColumns[index] = starOutputColumn(table.Columns[index], table.Name, table.Schema, table.Alias)
	}
	return outputColumns
}

// starOutputColumn builds the output column a star expansion projects for one scoped
// column, recording where the column came from.
//
// Takes column (ast.ScopedColumn) which specifies the column being projected.
// Takes sourceTable (string) which specifies the table or CTE the column belongs to.
// Takes sourceSchema (string) which specifies the schema of sourceTable, or empty when
// unknown.
// Takes sourceQualifier (string) which specifies the reference that qualifies the column.
//
// Returns sqlmodel.OutputColumn which projects the column unchanged.
func starOutputColumn(
	column ast.ScopedColumn,
	sourceTable string,
	sourceSchema string,
	sourceQualifier string,
) sqlmodel.OutputColumn {
	output := NewOutputColumn(column.Name, column.SQLType, column.Nullable)
	output.SourceTable = sourceTable
	output.SourceSchema = sourceSchema
	output.SourceColumn = column.Name
	output.SourceQualifier = sourceQualifier
	return output
}

// resolveBareColumnFallback retries an unqualified column lookup when a qualified
// parameter reference fails to resolve.
//
// A parameter hoisted out of a subquery keeps that subquery's local table alias, which is
// absent from the parent scope, so the qualified lookup wrongly fails. If the bare column
// name then resolves unambiguously in the current scope, the parameter is typed from it
// and the spurious Q001/Q002 is suppressed. A reference that was already unqualified, or
// whose bare column is ambiguous or unknown, returns ok=false so the original diagnostic
// stands.
//
// Takes scope (*ScopeChain) which specifies the active scope chain.
// Takes reference (*ast.ColumnReference) which is the parameter's target column.
//
// Returns *ast.ScopedColumn which holds the unambiguously resolved column.
// Returns bool which reports whether such a column was found.
func resolveBareColumnFallback(
	scope *ScopeChain,
	reference *ast.ColumnReference,
) (*ast.ScopedColumn, bool) {
	if reference.TableAlias == "" {
		return nil, false
	}
	column, _, err := scope.ResolveColumn("", reference.ColumnName)
	if err != nil || column == nil {
		return nil, false
	}
	return column, true
}

// findColumnInCatalogueFor is the catalogue-wide column lookup of the type resolver's
// parameter path, keeping its match, ambiguity-refusal and view-skip rules in one body.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the schema state to search.
// Takes reference (*ast.ColumnReference) which specifies the column to look up.
//
// Returns catalogueColumnMatch which holds the matched column's type and nullability when
// ok is true.
// Returns bool which is true when exactly one column matched.
func findColumnInCatalogueFor(
	catalogue *sqlmodel.Catalogue,
	reference *ast.ColumnReference,
) (catalogueColumnMatch, bool) {
	if catalogue == nil || reference == nil || reference.ColumnName == "" {
		return catalogueColumnMatch{}, false
	}

	var found catalogueColumnMatch
	matchCount := 0
	for _, schema := range catalogue.Schemas {
		if schema == nil {
			continue
		}
		match, hits, ambiguous := matchColumnInSchema(schema, catalogue.Identifiers, reference)
		if ambiguous {
			return catalogueColumnMatch{}, false
		}
		matchCount += hits
		if matchCount > 1 {
			return catalogueColumnMatch{}, false
		}
		if hits == 1 {
			found = match
		}
	}

	if matchCount != 1 {
		return catalogueColumnMatch{}, false
	}
	return found, true
}

// matchColumnInSchema scans a single schema for tables holding the reference's column,
// applying the optional TableAlias filter. It returns early when more than one match is
// seen so the caller can short-circuit before walking later schemas.
//
// Takes schema (*sqlmodel.Schema) which specifies the schema to scan.
// Takes identifiers (sqlmodel.IdentifierCase) which compares the names.
// Takes reference (*ast.ColumnReference) which specifies the column to look up.
//
// Returns catalogueColumnMatch which holds the column type when a single match was found.
// Returns int which is the number of matches encountered (capped at 2 so callers can
// recognise ambiguity).
// Returns bool which is true when the schema alone contains two or more matches, allowing
// the caller to abort.
func matchColumnInSchema(
	schema *sqlmodel.Schema,
	identifiers sqlmodel.IdentifierCase,
	reference *ast.ColumnReference,
) (catalogueColumnMatch, int, bool) {
	var found catalogueColumnMatch
	matches := 0
	for _, table := range schema.Tables {
		if table == nil {
			continue
		}
		if reference.TableAlias != "" && !identifiers.Equal(table.Name, reference.TableAlias) {
			continue
		}
		match, ok := findColumnInTable(table, identifiers, reference.ColumnName)
		if !ok {
			continue
		}
		matches++
		if matches > 1 {
			return catalogueColumnMatch{}, matches, true
		}
		found = match
	}
	return found, matches, false
}

// findColumnInTable returns the type information for a column on a specific table,
// comparing names by the dialect's identifier rules.
//
// Takes table (*sqlmodel.Table) which specifies the table to search.
// Takes identifiers (sqlmodel.IdentifierCase) which compares the names.
// Takes columnName (string) which specifies the column's canonical name.
//
// Returns catalogueColumnMatch which holds the matched column's type.
// Returns bool which is true when the column was found.
func findColumnInTable(table *sqlmodel.Table, identifiers sqlmodel.IdentifierCase, columnName string) (catalogueColumnMatch, bool) {
	for index := range table.Columns {
		if identifiers.Equal(table.Columns[index].Name, columnName) {
			return catalogueColumnMatch{
				sqlType:  arrayWrappedSQLType(&table.Columns[index]),
				nullable: table.Columns[index].Nullable,
			}, true
		}
	}
	return catalogueColumnMatch{}, false
}

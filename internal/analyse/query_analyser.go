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

package analyse

import (
	"context"
	"fmt"
	"slices"

	"seagull.sh/seagull/internal/check"
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/directive"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/observe"
	"seagull.sh/seagull/internal/panicguard"
	"seagull.sh/seagull/internal/resolve"
	"seagull.sh/seagull/internal/sqlfile"
	"seagull.sh/seagull/internal/sqlmodel"
)

// QueryAnalyser is the analysis-stage orchestrator that transforms raw SQL queries into
// fully typed AnalysedQuery results. It coordinates directive parsing, scope chain
// construction, type resolution, nullability propagation, and diagnostic analysis.
//
// A QueryAnalyser is not safe for concurrent use: its type resolver and diagnostic
// analyser keep state while a query is analysed. Use one per goroutine.
type QueryAnalyser struct {
	// engine holds the dialect used for parsing and analysis.
	engine dialect.DialectPort

	// catalogue holds the database catalogue containing schema, table, and column metadata.
	catalogue *sqlmodel.Catalogue

	// directiveParser holds the parser used to extract directives from comment blocks.
	directiveParser *directive.Parser

	// typeResolver holds the resolver used to determine output column and parameter types.
	typeResolver *resolve.TypeResolver

	// scopeBuilder holds the builder that registers the query's relations in its scope
	// chain.
	scopeBuilder *resolve.ScopeBuilder

	// diagnosticAnalyser holds the analyser used to produce additional warnings and hints.
	diagnosticAnalyser *check.DiagnosticAnalyser

	// elements decides which elements and map values of the output columns are nullable; nil
	// leaves the analysed types alone.
	elements *resolve.ElementNullability

	// parameterElements decides which elements and map values of the parameters are
	// nullable; nil takes elements.
	parameterElements *resolve.ElementNullability

	// prefixes holds the directive prefixes recognised in query files.
	prefixes namespace.Set

	// embedPrefixes lists the directive prefixes recognised in inline embed markers.
	embedPrefixes []string

	// info describes the dialect, read once when the analyser is made.
	info dialect.Info
}

// NewQueryAnalyser creates a query analyser with all sub-components initialised from the
// given engine and catalogue.
//
// Takes engine (dialect.DialectPort) which provides SQL parsing, analysis, and type
// resolution capabilities.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the database schema metadata.
//
// Takes policy (directive.PrefixPolicy) which names the directive prefixes to recognise.
//
// Returns *QueryAnalyser which holds the fully initialised analyser.
func NewQueryAnalyser(engine dialect.DialectPort, catalogue *sqlmodel.Catalogue, policy directive.PrefixPolicy) *QueryAnalyser {
	functionResolver := resolve.NewFunctionResolver(engine.BuiltinFunctions(), catalogue, engine)
	typeResolver := resolve.NewTypeResolver(catalogue, functionResolver, engine)
	info := engine.Info()

	return &QueryAnalyser{
		engine:             engine,
		info:               info,
		catalogue:          catalogue,
		directiveParser:    directive.NewParser(info.Placeholders.Sigils, info.Comments, policy),
		prefixes:           policy.Prefixes,
		embedPrefixes:      policy.Prefixes.Accepted(),
		typeResolver:       typeResolver,
		scopeBuilder:       resolve.NewScopeBuilder(engine, catalogue, typeResolver),
		diagnosticAnalyser: check.NewDiagnosticAnalyser(engine, catalogue),
		elements:           nil,
		parameterElements:  nil,
	}
}

// SetParameterElementNullability sets which elements of the arrays and lists, and which
// values of the maps, a query's parameters hold as nullable; nil, the default, takes what
// SetElementNullability sets.
//
// Takes elements (*resolve.ElementNullability) which decides them.
func (a *QueryAnalyser) SetParameterElementNullability(elements *resolve.ElementNullability) {
	a.parameterElements = elements
}

// SetElementNullability sets which elements of arrays and lists, and which values of
// maps, the output columns hold as nullable, as each column, type override and the preset
// declare them. Without it the analysed types are left as they are.
//
// Takes elements (*resolve.ElementNullability) which decides them.
func (a *QueryAnalyser) SetElementNullability(elements *resolve.ElementNullability) {
	a.elements = elements
}

// AnalyseQuery performs the full analysis pipeline on a single query block.
//
// The pipeline proceeds in order: parse directives, parse SQL, raw analysis, build scope
// chain, resolve CTEs, resolve output columns, resolve parameters, propagate nullability,
// validate, and assemble the final AnalysedQuery.
//
// A raw analysis holding a typed-nil expression node is reported as Q030 and not analysed
// further. A panic while the block is analysed, in Seagull or in the dialect, is reported
// as Q070 on the block, with its stack logged once, and the block gives no query.
//
// Takes block (sqlfile.QueryBlock) which holds the raw SQL text and line information.
// Takes filename (string) which identifies the source file for query diagnostics.
//
// Returns *sqlmodel.AnalysedQuery which holds the fully analysed query, or nil if
// analysis fails early.
//
// Returns []diag.SourceError which holds all diagnostics produced during analysis.
func (a *QueryAnalyser) AnalyseQuery(
	ctx context.Context,
	block sqlfile.QueryBlock,
	filename string,
) (*sqlmodel.AnalysedQuery, []diag.SourceError) {
	ctx, end := observe.From(ctx).Start(ctx, observe.StageAnalyseQuery)
	defer end(nil)

	var allDiagnostics []diag.SourceError
	query, err := panicguard.RunValue(ctx, fmt.Sprintf("analysing the query block at %s:%d", filename, block.StartLine),
		func() (*sqlmodel.AnalysedQuery, error) {
			return a.analyseQuery(ctx, block, filename, &allDiagnostics), nil
		})
	if err == nil {
		return query, limitDiagnostics(filename, block.StartLine, allDiagnostics)
	}
	if ctx.Err() != nil {
		return nil, limitDiagnostics(filename, block.StartLine, allDiagnostics)
	}
	return nil, limitDiagnostics(filename, block.StartLine, allDiagnostics,
		diagbuild.Failure(filename, block.SQL, block.StartLine, "failed to analyse the query block", err))
}

// analyseQuery is AnalyseQuery without its panic guard, collecting the diagnostics as it
// goes so those found before a panic are still reported.
//
// Takes block (sqlfile.QueryBlock) which holds the raw SQL text and line information.
// Takes filename (string) which identifies the source file for query diagnostics.
// Takes allDiagnostics (*[]diag.SourceError) which receives the diagnostics.
//
// Returns *sqlmodel.AnalysedQuery which holds the fully analysed query, or nil if
// analysis fails early.
func (a *QueryAnalyser) analyseQuery(
	ctx context.Context,
	block sqlfile.QueryBlock,
	filename string,
	allDiagnostics *[]diag.SourceError,
) *sqlmodel.AnalysedQuery {
	directiveBlock, directiveDiagnostics := a.directiveParser.Parse(block, filename)
	*allDiagnostics = append(*allDiagnostics, directiveDiagnostics...)

	queryName, queryCommand, directives := extractDirectiveMetadata(directiveBlock)
	if queryName == "" {
		return nil
	}

	rawAnalysis, location, parseDiagnostics := a.parseQueryStatements(ctx, block, filename, queryName)
	*allDiagnostics = append(*allDiagnostics, parseDiagnostics...)
	if rawAnalysis == nil {
		return nil
	}
	if problem := resolve.ValidateAnalysis(rawAnalysis); problem != nil {
		*allDiagnostics = append(*allDiagnostics, location.diagnostics([]*resolve.Error{problem})...)
		return nil
	}

	directive.BindNamedAnchors(directiveBlock.Parameters, rawAnalysis.ParameterReferences)
	resolution := a.resolveQueryTypes(ctx, rawAnalysis, directiveBlock, location)
	*allDiagnostics = append(*allDiagnostics, resolution.diagnostics...)
	if ctx.Err() != nil {
		return nil
	}

	if rawAnalysis.ReadOnly && resolution.calledDataModifyingFunction {
		rawAnalysis.ReadOnly = false
	}

	outputColumns := resolution.outputColumns
	parameters := resolution.parameters
	scope := resolution.scope

	outputColumns, embedDiagnostics := resolveEmbedDirectives(a.embedInputFor(directiveBlock, directives, block, location, scope), outputColumns)
	*allDiagnostics = append(*allDiagnostics, embedDiagnostics...)
	outputColumns = a.decorateOutputColumns(outputColumns, resolution.elementsKnown, directiveBlock, directives, filename, allDiagnostics)
	parameters = a.decorateParameters(parameters, directiveBlock, location, allDiagnostics)

	query, assembled := a.assembleQuery(&assembleQueryInput{
		queryName:     queryName,
		queryCommand:  queryCommand,
		block:         block,
		location:      location,
		filename:      filename,
		outputColumns: outputColumns,
		origins:       resolution.origins,
		referable:     resolution.referable,
		namedItems:    resolution.namedItems,
		aggregated:    resolution.aggregated,
		parameters:    parameters,
		isDynamic:     isDynamicQuery(parameters),
		directives:    directives,
		rawAnalysis:   rawAnalysis,
	})
	*allDiagnostics = append(*allDiagnostics, assembled.diagnostics...)
	*allDiagnostics = append(*allDiagnostics, a.applyRowType(query, directives, directiveBlock, filename)...)
	subject := a.diagnosticSubject(query, assembled.countWrapped, rawAnalysis, scope, directiveBlock, filename, *allDiagnostics)
	*allDiagnostics = append(*allDiagnostics, a.diagnosticAnalyser.Analyse(ctx, subject)...)
	return query
}

// embedInputFor gathers what binding a query's embeds needs.
//
// Takes directiveBlock (*directive.DirectiveBlock) which holds the parsed embed
// directives.
// Takes directives (*directive.QueryDirectives) which holds the query's group_by keys.
// Takes block (sqlfile.QueryBlock) which holds the raw SQL text and its start line.
// Takes location (statementLocation) which locates the embed diagnostics.
// Takes scope (*resolve.ScopeChain) which holds the query's table references.
//
// Returns embedInput which holds the directives, scope and diagnostic context.
func (a *QueryAnalyser) embedInputFor(
	directiveBlock *directive.DirectiveBlock,
	directives *directive.QueryDirectives,
	block sqlfile.QueryBlock,
	location statementLocation,
	scope *resolve.ScopeChain,
) embedInput {
	var groupByKeys []string
	if directives != nil {
		groupByKeys = directives.GroupByKeys
	}
	return embedInput{
		directiveBlock: directiveBlock,
		scope:          scope,
		errorBuilder:   diagbuild.NewErrorBuilder(location.filename),
		tokens:         block.Tokens,
		sql:            block.SQL,
		lines:          location.lines,
		prefixes:       a.prefixes,
		markerPrefixes: a.embedPrefixes,
		groupByKeys:    groupByKeys,
		startLine:      block.StartLine,
	}
}

// diagnosticSubject gathers what the diagnostic passes read of the assembled query.
//
// Takes query (*sqlmodel.AnalysedQuery) which holds the assembled query to analyse.
//
// Takes countWrapped (bool) which is true when the query's derived count query wraps the
// original in a subquery.
//
// Takes rawAnalysis (*ast.RawQueryAnalysis) which holds the raw analysis result.
//
// Takes scope (*resolve.ScopeChain) which holds the resolved scope chain.
//
// Takes directiveBlock (*directive.DirectiveBlock) which holds the parsed directive
// block.
//
// Takes filename (string) which specifies the source file path for error reporting.
//
// Takes priorDiagnostics ([]diag.SourceError) which holds the diagnostics already
// collected for this query so passes can avoid double-reporting one.
//
// Returns *check.DiagnosticContext which the passes analyse.
func (a *QueryAnalyser) diagnosticSubject(
	query *sqlmodel.AnalysedQuery,
	countWrapped bool,
	rawAnalysis *ast.RawQueryAnalysis,
	scope *resolve.ScopeChain,
	directiveBlock *directive.DirectiveBlock,
	filename string,
	priorDiagnostics []diag.SourceError,
) *check.DiagnosticContext {
	return &check.DiagnosticContext{
		Query:               query,
		CountWrapped:        countWrapped,
		RawAnalysis:         rawAnalysis,
		Scope:               scope,
		ParameterDirectives: directiveBlock.Parameters,
		Filename:            filename,
		ExistingDiagnostics: priorDiagnostics,
		Prefixes:            a.prefixes,
	}
}

// parseQueryStatements parses the SQL in a query block and produces a raw analysis
// result.
//
// Takes block (sqlfile.QueryBlock) which holds the raw SQL text and line information.
//
// Takes filename (string) which specifies the source file path for error reporting.
//
// Takes queryName (string) which specifies the query name for error messages.
//
// Returns *ast.RawQueryAnalysis which holds the raw analysis result, or nil if parsing
// fails.
//
// Returns statementLocation which locates the analysed statement in its file.
//
// Returns []diag.SourceError which holds any parse or analysis errors.
func (a *QueryAnalyser) parseQueryStatements(
	ctx context.Context,
	block sqlfile.QueryBlock,
	filename string,
	queryName string,
) (*ast.RawQueryAnalysis, statementLocation, []diag.SourceError) {
	var diagnostics []diag.SourceError
	location := statementLocation{
		filename:   filename,
		sql:        block.SQL,
		lines:      check.NewSourceLines(block.SQL, block.StartLine),
		startLine:  block.StartLine,
		statement:  diagbuild.TextSpan{Line: block.StartLine, Column: 1, EndLine: 0, EndColumn: 0},
		statements: nil,
		offset:     0,
	}

	statements, parseError := a.engine.ParseStatements(ctx, block.SQL)
	if parseError != nil {
		return nil, location, []diag.SourceError{
			location.parseFailure(parseError, fmt.Sprintf("failed to parse query %q", queryName)),
		}
	}
	location.statements = statements

	if len(statements) == 0 {
		return nil, location, []diag.SourceError{
			diagbuild.Block(filename, block.StartLine, diag.CodeParseError, diag.SeverityError,
				fmt.Sprintf("query %q contains no SQL statements", queryName)),
		}
	}

	primaryStatement := statements[len(statements)-1]
	location.offset = primaryStatement.Location
	if primaryStatement.Length > 0 {
		location.statement = location.lines.Span(primaryStatement.Location,
			primaryStatement.Location+primaryStatement.Length)
	}

	if len(statements) > 1 {
		diagnostics = append(diagnostics, diagbuild.Block(filename, block.StartLine, diag.CodeMultipleStatements, diag.SeverityError,
			fmt.Sprintf("query %q contains %d statements; give each its own header", queryName, len(statements))))
	}

	rawAnalysis, analysisError := a.engine.AnalyseQuery(ctx, a.catalogue, primaryStatement)
	if analysisError != nil {
		diagnostics = append(diagnostics, location.parseFailure(analysisError, fmt.Sprintf("failed to analyse query %q", queryName)))
		return nil, location, diagnostics
	}
	if rawAnalysis != nil {
		diagnostics = append(diagnostics, location.dialectWarnings(rawAnalysis.Warnings)...)
	}

	return rawAnalysis, location, diagnostics
}

// resolveQueryTypes builds the scope chain and resolves all output column and parameter
// types for a query.
//
// Takes rawAnalysis (*ast.RawQueryAnalysis) which holds the raw analysis result to
// resolve types for.
//
// Takes directiveBlock (*directive.DirectiveBlock) which holds the parsed parameter
// directives.
//
// Takes location (statementLocation) which locates the statement the problems found are
// reported on.
//
// Returns queryTypeResolution which holds the resolved columns, parameters, scope, and
// diagnostics.
func (a *QueryAnalyser) resolveQueryTypes(
	ctx context.Context,
	rawAnalysis *ast.RawQueryAnalysis,
	directiveBlock *directive.DirectiveBlock,
	location statementLocation,
) queryTypeResolution {
	scope := a.scopeBuilder.TypeResolver.NewRootScope(resolve.ScopeKindQuery)
	scope.DeclareRuntimeColumns(a.declaredColumns(directiveBlock))
	_ = a.typeResolver.TakeDepthLimits()

	problems := a.scopeBuilder.BuildQueryScope(ctx, rawAnalysis, scope)
	problems = append(problems, a.scopeBuilder.ResolveArrayJoinClauses(ctx, rawAnalysis.ArrayJoinClauses, scope)...)

	projection := a.typeResolver.ResolveProjection(ctx, rawAnalysis.OutputColumns, location.itemSpans(rawAnalysis), scope)
	problems = append(problems, projection.Problems...)
	problems = append(problems, a.scopeBuilder.ResolveCompoundBranches(ctx, rawAnalysis.CompoundBranches, projection.Columns, scope)...)

	parameters, parameterProblems := a.typeResolver.ResolveParameters(ctx, rawAnalysis, scope, directiveBlock.Parameters)
	problems = append(problems, parameterProblems...)
	calledDataModifyingFunction := projection.DataModifying || a.typeResolver.CallsDataModifyingFunction(ctx, rawAnalysis, scope)
	problems = append(problems, a.typeResolver.TakeDepthLimits()...)

	return queryTypeResolution{
		outputColumns:               projection.Columns,
		origins:                     projection.Origins,
		referable:                   projection.Referable,
		elementsKnown:               projection.ElementsKnown,
		namedItems:                  projection.NamedItems,
		parameters:                  parameters,
		scope:                       scope,
		calledDataModifyingFunction: calledDataModifyingFunction,
		aggregated:                  projection.Aggregated,
		diagnostics:                 location.diagnostics(problems),
	}
}

// decorateOutputColumns applies what a query's directives and the catalogue say of its
// output columns: their nullability, the query's and the catalogue's column overrides,
// and the nullability of their elements.
//
// Takes outputColumns ([]sqlmodel.OutputColumn) which are the resolved columns.
// Takes elementsKnown ([]bool) which holds, parallel to outputColumns, whether the
// nullability a column's type gives its elements is what the query returns.
// Takes directiveBlock (*directive.DirectiveBlock) which holds the query's directives.
// Takes directives (*directive.QueryDirectives) which holds the query header's
// directives.
// Takes filename (string) which is the query file, for diagnostics.
// Takes diagnostics (*[]diag.SourceError) which receives the overrides' problems.
//
// Returns []sqlmodel.OutputColumn which holds the decorated columns.
func (a *QueryAnalyser) decorateOutputColumns(outputColumns []sqlmodel.OutputColumn, elementsKnown []bool,
	directiveBlock *directive.DirectiveBlock, directives *directive.QueryDirectives, filename string,
	diagnostics *[]diag.SourceError) []sqlmodel.OutputColumn {
	outputColumns = propagateOutputNullability(outputColumns, directives)
	outputColumns = a.applyQueryColumnOverrides(outputColumns, directiveBlock, filename, diagnostics)
	outputColumns = a.propagateCatalogueColumnOverrides(outputColumns, directiveBlock)
	return a.applyElementNullability(outputColumns, elementsKnown, directiveBlock)
}

// decorateParameters applies what a query's directives say of its parameters: their
// nullability and that of their elements, and reports a LIMIT or OFFSET bound the
// generated code cannot hold.
//
// Takes parameters ([]sqlmodel.QueryParameter) which are the resolved parameters.
// Takes directiveBlock (*directive.DirectiveBlock) which holds the query's directives.
// Takes location (statementLocation) which places the problems found.
// Takes diagnostics (*[]diag.SourceError) which receives the problems found.
//
// Returns []sqlmodel.QueryParameter which holds the decorated parameters.
func (a *QueryAnalyser) decorateParameters(parameters []sqlmodel.QueryParameter, directiveBlock *directive.DirectiveBlock,
	location statementLocation, diagnostics *[]diag.SourceError) []sqlmodel.QueryParameter {
	parameters = propagateParameterNullability(parameters, directiveBlock.Parameters)
	parameters = a.applyParameterElementNullability(parameters)
	*diagnostics = append(*diagnostics, a.paginationBoundDiagnostics(parameters, directiveBlock.Parameters, location)...)
	return parameters
}

// declaredColumns returns the columns a query's column directives declare, for a source
// whose columns are known only when the query runs, such as a file a table function
// reads: each named column, typed by its type: value or of unknown type, and nullable
// unless the directive says nullable: false.
//
// Takes directiveBlock (*directive.DirectiveBlock) which holds the column directives.
//
// Returns []ast.ScopedColumn which holds the columns in directive order, or nil when
// there are none.
func (a *QueryAnalyser) declaredColumns(directiveBlock *directive.DirectiveBlock) []ast.ScopedColumn {
	if directiveBlock == nil || len(directiveBlock.ColumnOverrides) == 0 {
		return nil
	}
	columns := make([]ast.ScopedColumn, 0, len(directiveBlock.ColumnOverrides))
	for _, override := range directiveBlock.ColumnOverrides {
		sqlType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
		if override.SQLType != "" {
			sqlType = a.engine.NormaliseTypeName(override.SQLType)
		}
		columns = append(columns, ast.ScopedColumn{
			Name:     override.Name,
			SQLType:  sqlType,
			Nullable: override.Nullable == nil || *override.Nullable,
		})
	}
	return columns
}

// queryTypeResolution holds the results of query type resolution, grouping the resolved
// output columns, parameters, scope chain, data-modification flag, and any diagnostics
// produced during resolution.
type queryTypeResolution struct {
	// scope holds the fully populated scope chain for the query.
	scope *resolve.ScopeChain

	// outputColumns holds the resolved output columns with types and nullability.
	outputColumns []sqlmodel.OutputColumn

	// origins holds, parallel to outputColumns, the index of the select-list or RETURNING
	// item each column came from.
	origins []int

	// referable holds, parallel to outputColumns, whether a column a star stands for can be
	// named in place of the star.
	referable []bool

	// elementsKnown holds, parallel to outputColumns, whether the nullability a column's
	// type gives its elements is what the query returns.
	elementsKnown []bool

	// namedItems lists the select-list items a later item names by their alias.
	namedItems []int

	// parameters holds the resolved query parameters with types.
	parameters []sqlmodel.QueryParameter

	// diagnostics holds any warnings or errors produced during type resolution.
	diagnostics []diag.SourceError

	// calledDataModifyingFunction indicates whether the query invokes a data-modifying
	// function.
	calledDataModifyingFunction bool

	// aggregated reports a select-list item that holds an aggregate or a window function
	// outside any subquery.
	aggregated bool
}

// statementLocation is where the analysed statement of a query block sits in its file, so
// a problem found while resolving it is reported there.
type statementLocation struct {
	// filename identifies the query file.
	filename string

	// sql is the query block's text, which the dialect's offsets point into.
	sql string

	// lines places the dialect's offsets in the query file.
	lines *check.SourceLines

	// statements lists the block's statements; the last is the analysed one.
	statements []ast.ParsedStatement

	// statement covers the analysed statement.
	statement diagbuild.TextSpan

	// startLine is the file line the block's text starts on.
	startLine int

	// offset is the byte offset of the analysed statement in sql.
	offset int
}

// diagnostics turns the problems found while resolving the statement into diagnostics,
// each placed as place places it.
//
// Takes problems ([]*resolve.Error) which are the problems found.
//
// Returns []diag.SourceError which holds one diagnostic per problem.
func (location statementLocation) diagnostics(problems []*resolve.Error) []diag.SourceError {
	diagnostics := make([]diag.SourceError, 0, len(problems))
	for index, problem := range problems {
		diagnostics = append(diagnostics, location.place(index, ast.Span{Start: problem.Span.Start, End: problem.Span.End},
			problem.Code, problem.Severity, problem.Message, problem.Suggestion))
	}
	return diagnostics
}

// place turns one problem found in the statement into a diagnostic: at its own span when
// the dialect located it, and on the statement otherwise. A problem at or past
// check.MaxDiagnosticsPerQuery among those of its kind, which the block cannot report, is
// not placed.
//
// Takes index (int) which is the problem's place among those of its kind.
// Takes span (ast.Span) which locates the problem in the block's text, or is zero.
// Takes code (string) which is the diagnostic code.
// Takes severity (diag.ErrorSeverity) which is how serious the problem is.
// Takes message (string) which describes it.
// Takes suggestion (string) which is the name the query most likely meant, or empty.
//
// Returns diag.SourceError which is the diagnostic.
func (location statementLocation) place(
	index int,
	span ast.Span,
	code string,
	severity diag.ErrorSeverity,
	message, suggestion string,
) diag.SourceError {
	if index >= check.MaxDiagnosticsPerQuery {
		return diagbuild.Unlocated(code, severity, message)
	}
	placed := location.statement
	if span.Known() {
		placed = location.lines.Span(span.Start, span.End)
	}
	return diagbuild.Located(location.filename, placed, code, severity, message, suggestion)
}

// dialectWarnings turns what the dialect warns of in the statement into warnings, each
// placed as place places it.
//
// Takes warnings ([]ast.Warning) which are the dialect's warnings.
//
// Returns []diag.SourceError which holds one located warning per dialect warning.
func (location statementLocation) dialectWarnings(warnings []ast.Warning) []diag.SourceError {
	diagnostics := make([]diag.SourceError, 0, len(warnings))
	for index, warning := range warnings {
		diagnostics = append(diagnostics, location.place(index, warning.Span, diag.CodeEngineReading, diag.SeverityWarning,
			warning.Message, ""))
	}
	return diagnostics
}

// limitDiagnostics keeps the first check.MaxDiagnosticsPerQuery diagnostics of a query
// block and summarises the rest in one Q066 diagnostic on the block, at the highest
// severity among those left out, so a block whose errors were left out still fails.
//
// Takes filename (string) which is the query file.
// Takes startLine (int) which is the line the block starts on.
// Takes diagnostics ([]diag.SourceError) which are the block's diagnostics.
// Takes more (...diag.SourceError) which follow them.
//
// Returns []diag.SourceError which holds the diagnostics the block reports.
func limitDiagnostics(filename string, startLine int, diagnostics []diag.SourceError, more ...diag.SourceError) []diag.SourceError {
	if len(diagnostics)+len(more) <= check.MaxDiagnosticsPerQuery {
		return slices.Concat(diagnostics, more)
	}
	limiter := diagbuild.NewLimiter(check.MaxDiagnosticsPerQuery)
	limiter.Add(diagnostics...)
	limiter.Add(more...)
	return limiter.Finish(func(code string, severity diag.ErrorSeverity, message string) diag.SourceError {
		return diagbuild.Block(filename, startLine, code, severity, message)
	})
}

// extractDirectiveMetadata extracts the query name, command, and query-level directives
// from a parsed directive block.
//
// Takes directiveBlock (*directive.DirectiveBlock) which holds the parsed directive
// block.
//
// Returns string which holds the query name, or empty if no name directive was found.
//
// Returns sqlmodel.QueryCommand which holds the query command type.
//
// Returns *directive.QueryDirectives which holds the extracted query-level directive
// settings.
func extractDirectiveMetadata(
	directiveBlock *directive.DirectiveBlock,
) (string, sqlmodel.QueryCommand, *directive.QueryDirectives) {
	var queryName string
	if directiveBlock.Name != nil {
		queryName = directiveBlock.Name.Value
	}
	var queryCommand sqlmodel.QueryCommand
	if directiveBlock.Command != nil {
		queryCommand = directiveBlock.Command.Command
	}
	return queryName, queryCommand, directive.ExtractQueryDirectives(directiveBlock)
}

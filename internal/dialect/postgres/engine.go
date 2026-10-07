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

package postgres

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/postgres/lexical"
	"seagull.sh/seagull/internal/dialect/postgres/pgext"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

var (
	// statementAnalysers dispatches each DML statement kind to the parser routine that
	// analyses it.
	statementAnalysers = [statementKindCount]func(*parser) (*ast.RawQueryAnalysis, error){
		statementKindSelect: (*parser).analyseSelect,
		statementKindInsert: dialectkit.ChangingRows(ast.DataChangeInsert, (*parser).analyseInsert),
		statementKindUpdate: dialectkit.ChangingRows(ast.DataChangeUpdate, (*parser).analyseUpdate),
		statementKindDelete: dialectkit.ChangingRows(ast.DataChangeDelete, (*parser).analyseDelete),
		statementKindValues: (*parser).analyseSelect,
		statementKindCall:   (*parser).analyseCall,
	}

	// ddlHandlers dispatches each DDL statement kind to the parser routine that produces the
	// corresponding catalogue mutation.
	ddlHandlers = [statementKindCount]ddlHandler{
		statementKindCreateTable: func(p *parser, engine *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseCreateTable(engine)
		},
		statementKindDropTable: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) { return p.parseDropTable() },
		statementKindAlterTable: func(p *parser, engine *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseAlterTable(engine)
		},
		statementKindCreateView: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) { return p.parseCreateView() },
		statementKindDropView:   func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) { return p.parseDropView() },
		statementKindAlterView:  func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) { return p.parseAlterView() },
		statementKindCreateIndex: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseCreateIndex()
		},
		statementKindDropIndex: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) { return p.parseDropIndex() },
		statementKindCreateTrigger: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseCreateOrDropTrigger(statementKindCreateTrigger)
		},
		statementKindDropTrigger: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseCreateOrDropTrigger(statementKindDropTrigger)
		},
		statementKindCreateType: func(p *parser, engine *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseCreateType(engine)
		},
		statementKindAlterType: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) { return p.parseAlterType() },
		statementKindDropType:  func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) { return p.parseDropType() },
		statementKindCreateFunction: func(p *parser, engine *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseCreateFunction(engine)
		},
		statementKindDropFunction: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseDropFunction()
		},
		statementKindCreateSchema: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseCreateSchema()
		},
		statementKindDropSchema: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) { return p.parseDropSchema() },
		statementKindCreateExtension: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseCreateExtension()
		},
		statementKindDropExtension: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseDropExtension()
		},
		statementKindCreateSequence: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseCreateSequence()
		},
		statementKindDropSequence: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseDropSequence()
		},
		statementKindComment: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) { return p.parseComment() },
		statementKindSetSearchPath: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseSearchPath()
		},
		statementKindAlterSequence: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseAlterSequence()
		},
	}
)

var (
	// runOnRules names the words that open a DDL statement, for finding a statement that
	// runs on into the next one because the semicolon between them is missing.
	runOnRules = dialectkit.RunOnRules[tokenKind]{
		Starters: map[string][]string{
			keywordCREATE: append(slices.Clone(runOnObjects), "INDEX", "TRIGGER", "UNIQUE", "OR", "TEMP", "TEMPORARY",
				"UNLOGGED", "GLOBAL", "LOCAL", keywordFOREIGN, "RECURSIVE", "RULE", "POLICY", "AGGREGATE", "CAST",
				"CONSTRAINT", "EVENT", "OPERATOR", "COLLATION", "STATISTICS", "PUBLICATION", "SUBSCRIPTION", "SERVER",
				"ROLE", "USER", "DATABASE", "TABLESPACE", "TEXT", "LANGUAGE"),
			"COMMENT":   {keywordON},
			"ALTER":     runOnObjects,
			keywordDROP: append(slices.Clone(runOnObjects), "INDEX", "TRIGGER"),
		},
		Word:  tokenIdentifier,
		Open:  tokenLeftParen,
		Close: tokenRightParen,
	}

	// alterTableRunOnRules are runOnRules for an ALTER TABLE, whose actions open with ALTER
	// and DROP followed by a column name, which may be a word such as type.
	alterTableRunOnRules = dialectkit.RunOnRules[tokenKind]{
		Starters: map[string][]string{
			keywordCREATE: {keywordTABLE, "VIEW", "MATERIALIZED", "INDEX", "UNIQUE", keywordTYPE, "SEQUENCE", "OR",
				"FUNCTION", "TRIGGER"},
			"COMMENT":   {keywordON},
			"ALTER":     {keywordTABLE},
			keywordDROP: {keywordTABLE},
		},
		Word:  tokenIdentifier,
		Open:  tokenLeftParen,
		Close: tokenRightParen,
	}

	// runOnObjects names the objects an ALTER or DROP statement opens with.
	runOnObjects = []string{keywordTABLE, "VIEW", "MATERIALIZED", keywordTYPE, "DOMAIN", "SEQUENCE", keywordSCHEMA,
		"FUNCTION", "PROCEDURE", "EXTENSION"}
)

// Option configures a PostgreSQL dialect.
//
// Options apply in the order given, and a flavour such as cockroachdb applies its own
// before the caller's. Every registering option is additive: extra types merge, a later
// entry replacing an earlier one of the same name; extra-function registrations run in
// order after the built-in catalogue; statement extensions and post-parse hooks are
// consulted in order; and the type, cast and promote hooks chain, the most recently added
// consulted first, so a caller's hook can override a flavour's while a hook that returns
// nil falls through to the earlier hooks and then to the built-in rules. WithName,
// WithLexicalRules and the limit options set a single value, the last one winning.
type Option func(*settings)

// Dialect is the PostgreSQL implementation of dialect.DialectPort.
type Dialect struct {
	// functions holds the built-in PostgreSQL function catalogue plus any extra signatures
	// supplied via the dialect options.
	functions *ast.FunctionCatalogue

	// types holds the built-in PostgreSQL type catalogue plus any extra definitions supplied
	// via the dialect options.
	types *ast.TypeCatalogue

	// typeResolver normalises type names through the hooks, the extra types and the built-in
	// types.
	typeResolver *typeResolver

	// accessors maps each accessor type, by lower-cased name, to the names of the functions
	// that return it, when the dialect was built WithAccessorArrows; nil otherwise.
	accessors map[string][]string

	// settings stores the active dialect configuration and override hooks.
	settings settings

	// withheldFunctions holds the lower-cased names of the built-in functions the dialect
	// leaves out, which ResolveFunctionCall never resolves.
	withheldFunctions map[string]struct{}

	// initErr is the error building the dialect met, such as built-in signatures that did
	// not decode, which ApplyDDL and AnalyseQuery return; nil when it was built whole.
	initErr error

	// info is the description Info hands out copies of, built once from the settings so the
	// parser can read it without copying it for every statement.
	info dialect.Info
}

// New creates the PostgreSQL dialect, named postgres and in the postgres family.
//
// Takes options (...Option) which configure the dialect's name, parser limits, types,
// functions and hooks; see Option for how they combine.
//
// Returns *Dialect which is the configured dialect.
//
// A built-in function catalogue that did not decode whole, or a problem an extra function
// registration reported, does not stop the dialect being built: every ApplyDDL and
// AnalyseQuery returns it instead.
func New(options ...Option) *Dialect {
	configured := settings{}
	configured.Name = "postgres"
	configured.LexicalRules = lexical.Rules()
	for _, option := range options {
		option(&configured)
	}
	return newDialect(&configured, builtinFunctionSpec)
}

// newDialect builds the dialect New describes from its configured settings and the
// built-in function signatures loadSpecification gives.
//
// Takes configured (*settings) which is the dialect's configuration, copied into the
// dialect.
// Takes loadSpecification (func() (*dialectkit.FunctionSpec, error)) which gives the
// decoded built-in function signatures.
//
// Returns *Dialect which is the dialect.
func newDialect(configured *settings, loadSpecification func() (*dialectkit.FunctionSpec, error)) *Dialect {
	withheld := make(map[string]struct{}, len(configured.WithheldFunctions))
	for _, name := range configured.WithheldFunctions {
		withheld[name] = struct{}{}
	}
	types := buildTypeCatalogue(configured.ExtraTypes)
	functions, err := buildFunctionCatalogue(loadSpecification, configured.ExtraFunctions, withheld, types)
	if err != nil {
		err = fmt.Errorf("%s: building the dialect: %w", configured.Name, err)
	}
	var accessors map[string][]string
	if len(configured.AccessorTypes) > 0 {
		accessors = accessorFunctions(functions, configured.AccessorTypes)
	}
	engine := &Dialect{
		settings:          *configured,
		types:             types,
		functions:         functions,
		typeResolver:      newTypeResolver(configured.TypeNormaliserHooks, configured.ExtraTypes),
		accessors:         accessors,
		withheldFunctions: withheld,
		initErr:           err,
		info:              dialect.Info{},
	}
	engine.info = engine.describe()
	return engine
}

// Info describes the PostgreSQL dialect: dollar-numbered parameters, the public default
// schema, RETURNING support and the PostgreSQL expression set.
//
// Its lexical rules are PostgreSQL's unless WithLexicalRules replaced them. The family is
// always postgres, so flavours built on this dialect, and a custom WithName, keep the
// postgres type mappings and drivers. Each call returns a copy the caller owns: changing
// its slices or maps changes neither the dialect nor another caller's copy.
//
// Returns dialect.Info which describes the dialect.
func (engine *Dialect) Info() dialect.Info {
	return engine.info.Clone()
}

// ParseStatements splits, tokenises and classifies SQL statements for the PostgreSQL
// dialect.
//
// The dialect's lexical rules (PostgreSQL's unless WithLexicalRules replaced them) decide
// where each statement begins and ends; each statement is then tokenised in place, so
// Location and Length are the byte span of the statement's first to last significant
// token in sql. A statement is tokenised no further than the per-statement token budget,
// so a huge statement costs no more memory than the budget allows; ApplyDDL and
// AnalyseQuery refuse such a statement when they would walk it. Registered
// StatementExtensions may override the built-in classification so child engines can claim
// their own statement shapes; each is handed its own copy of the statement's tokens. The
// context is checked before each statement is tokenised and classified. A panic, in the
// dialect or in an extension's Classify, is recovered and returned as a
// dialectkit.PanicError, and its stack is logged once at Warn through the context's
// logger.
//
// Takes sql (string) which is the source SQL to split, tokenise and classify.
//
// Returns results ([]ast.ParsedStatement) which holds one entry per parsed statement.
// Returns err (error) when the SQL leaves a string, comment or procedural block
// unterminated, tokenising a statement fails, classifying panics, or the context is done,
// which gives the context's cause.
func (engine *Dialect) ParseStatements(ctx context.Context, sql string) (results []ast.ParsedStatement, err error) {
	defer func() { dialectkit.Recover(ctx, "postgres: parse statements", recover(), &results, &err) }()

	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	statements, err := engine.lexStatements(ctx, sql)
	if err != nil {
		return nil, err
	}

	results = make([]ast.ParsedStatement, 0, len(statements))
	for _, statement := range statements {
		if err := context.Cause(ctx); err != nil {
			return nil, err
		}
		results = append(results, ast.ParsedStatement{
			Raw:      engine.classify(sql, statement),
			Location: statement.Span.Start,
			Length:   statement.Span.End - statement.Span.Start,
		})
	}

	return results, nil
}

// ApplyDDL applies a DDL statement to the catalogue for the PostgreSQL dialect.
//
// A malformed statement is reported as a returned syntax error. A panic, in the parser,
// an extension or a hook, fails the statement rather than the calling apply loop: it is
// recovered and returned as a dialectkit.PanicError, and its stack is logged once at Warn
// through the context's logger. It returns the context's cause once the context is done,
// before dispatch or when the parser's walk stops, so a long-running catalogue build can
// be cancelled by the caller; it rejects a statement over the per-statement token budget
// before the parser walks it. A part of the statement the parser does not analyse, such
// as a view body nested beyond the parse depth limit, is listed in the mutation's
// UnanalysedParts.
//
// Takes statement (ast.ParsedStatement) which is the DDL statement to apply.
//
// Returns mutation (*ast.CatalogueMutation) which describes the catalogue change.
// Returns err (error) which is non-nil when the statement was parsed by another dialect
// (wrapping dialect.ErrForeignStatement), is malformed, a hook fails, the parser panics,
// or the context is cancelled.
func (engine *Dialect) ApplyDDL(
	ctx context.Context,
	statement ast.ParsedStatement,
) (mutation *ast.CatalogueMutation, err error) {
	defer func() { dialectkit.Recover(ctx, "postgres: apply DDL", recover(), &mutation, &err) }()

	parsed, ok := statement.Raw.(*parsedStatement)
	if !ok {
		return nil, fmt.Errorf("%w: %T", dialect.ErrForeignStatement, statement.Raw)
	}

	if ctxErr := context.Cause(ctx); ctxErr != nil {
		return nil, ctxErr
	}
	if engine.initErr != nil {
		return nil, engine.initErr
	}

	if engine.hasDDLHandler(parsed) {
		if budgetErr := engine.checkTokenBudget(parsed); budgetErr != nil {
			return nil, budgetErr
		}
	}
	if runOnError := checkRunOn(ctx, parsed); runOnError != nil {
		return nil, runOnError
	}

	return engine.applyParsedDDL(ctx, parsed)
}

// AnalyseQuery performs structural analysis of a DML statement for the PostgreSQL
// dialect.
//
// The analysis must consume the whole statement: tokens left after a complete statement
// fail it with a dialectkit.ParseError naming the first of them, so nothing is silently
// dropped. A statement with no analysing routine is analysed as empty only when it opens
// with a word that starts a statement holding nothing to analyse, such as SET or
// TRUNCATE, and otherwise fails with a dialectkit.ParseError. A malformed statement is
// reported as a returned syntax error. A panic in the analyser or a hook is recovered and
// returned as a dialectkit.PanicError, and its stack is logged once at Warn through the
// context's logger. A statement over the per-statement token budget is rejected before
// the parser walks it, and the walk stops with the context's cause once the context is
// done.
//
// Takes catalogue (*sqlmodel.Catalogue) which is the schema the query runs against, which
// tells what a user-defined function called in a FROM clause returns; nil holds none.
// Takes statement (ast.ParsedStatement) which is the DML statement to analyse.
//
// Returns analysis (*ast.RawQueryAnalysis) which describes the query structure.
// Returns err (error) which is non-nil when the statement was parsed by another dialect
// (wrapping dialect.ErrForeignStatement), is malformed or over budget, analysis panics,
// or the context is cancelled.
func (engine *Dialect) AnalyseQuery(
	ctx context.Context,
	catalogue *sqlmodel.Catalogue,
	statement ast.ParsedStatement,
) (analysis *ast.RawQueryAnalysis, err error) {
	defer func() { dialectkit.Recover(ctx, "postgres: analyse query", recover(), &analysis, &err) }()

	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	parsed, ok := statement.Raw.(*parsedStatement)
	if !ok {
		return nil, fmt.Errorf("%w: %T", dialect.ErrForeignStatement, statement.Raw)
	}
	if engine.initErr != nil {
		return nil, engine.initErr
	}

	analyser := queryAnalyser(parsed)
	if analyser == nil {
		if err := engine.unanalysedStatementError(parsed); err != nil {
			return nil, err
		}
		return &ast.RawQueryAnalysis{}, nil
	}
	if budgetErr := engine.checkTokenBudget(parsed); budgetErr != nil {
		return nil, budgetErr
	}

	statementParser := engine.newStatementParser(dialectkit.WalkGuard(ctx, len(parsed.tokens)), parsed)
	statementParser.catalogue = catalogue
	statementParser.spans = dialectkit.NewSpanRecorder(statement.Location)

	analysis, err = analyser(statementParser)
	if err == nil {
		err = statementParser.requireEnd()
	}
	if statementParser.syntax.err != nil {
		return nil, explainForeignPlaceholder(statementParser.syntax.err, parsed.tokens)
	}
	if err != nil {
		return nil, explainForeignPlaceholder(err, parsed.tokens)
	}
	warnings := statementWarnings{kept: analysis.Warnings, leftOut: 0}
	foreignPlaceholderWarnings(parsed.tokens, &warnings)
	untypedParameterWarnings(analysis, statementParser.untypedPlaces(), parsed.tokens, &warnings)
	analysis.Warnings = warnings.finish()
	engine.recordStatementSpans(statementParser, analysis, parsed, statement)
	return analysis, nil
}

// BuiltinFunctions returns the PostgreSQL built-in function catalogue.
//
// Returns *ast.FunctionCatalogue which lists every registered function signature.
func (engine *Dialect) BuiltinFunctions() *ast.FunctionCatalogue {
	return engine.functions
}

// BuiltinTypes returns the PostgreSQL built-in type catalogue.
//
// Returns *ast.TypeCatalogue which lists every registered type.
func (engine *Dialect) BuiltinTypes() *ast.TypeCatalogue {
	return engine.types
}

// NormaliseTypeName resolves a raw type name to a structured SQLType.
//
// A name matches a built-in or extra type whatever its letter case, and an unknown name
// is folded to lower case, as an unquoted identifier is. When no modifiers are passed, a
// name written with a double-quoted word or a modifier list is read as a column type is:
// "Shapes"."Pair" names the type Pair in the schema Shapes, keeping their letters, a
// quoted word names a built-in or extra type only when written in lower case, so "Point"
// is not point, and numeric(10,2) has precision 10 and scale 2.
//
// Takes name (string) which is the raw type name from the SQL source.
// Takes modifiers (...int) which carry optional precision or length values.
//
// Returns sqlmodel.SQLType which describes the resolved type.
func (engine *Dialect) NormaliseTypeName(name string, modifiers ...int) sqlmodel.SQLType {
	return engine.typeResolver.normalise(name, modifiers...)
}

// TableValuedFunctionColumns returns output columns for a known function: a built-in
// table-valued function's, or else those its set-returning signatures in the dialect's
// function catalogue agree on, extra functions included, such as a flavour's.
//
// Takes functionName (string) which is the table-valued function name.
//
// Returns []ast.ScopedColumn which lists the output columns, or nil when the function is
// not known.
func (engine *Dialect) TableValuedFunctionColumns(functionName string) []ast.ScopedColumn {
	return builtinTableFunctionColumns(engine.functions, functionName)
}

// TableValuedFunctionColumnsFromCatalogue resolves user-defined functions returning
// composite, set-of, or inline RETURNS TABLE column lists by looking up the function
// signature in the catalogue.
//
// The functionName may be a bare identifier, found through the catalogue's search path in
// order, as PostgreSQL resolves it, or a schema-qualified "schema.name" reference, found
// in that schema alone.
//
// A panic is recovered and returned as a dialectkit.PanicError, and its stack is logged
// once at Warn through the context's logger.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the registered function signatures.
// Takes functionName (string) which is the bare or schema-qualified function name.
//
// Returns columns ([]ast.ScopedColumn) which lists the output columns, or nil when no
// matching set-returning function is found.
// Returns err (error) which is non-nil only when the lookup panics.
func (*Dialect) TableValuedFunctionColumnsFromCatalogue(
	ctx context.Context,
	catalogue *sqlmodel.Catalogue,
	functionName string,
) (columns []ast.ScopedColumn, err error) {
	defer func() {
		dialectkit.Recover(ctx, "postgres: table-valued function columns", recover(), &columns, &err)
	}()

	return catalogueTableFunctionColumns(catalogue, functionName), nil
}

// PromoteType consults registered hooks, then returns the wider type within the same
// category, comparing the widths of integers and of floats. Across numeric categories it
// prefers a float, then a decimal.
//
// Takes left (sqlmodel.SQLType) which is the first candidate type.
// Takes right (sqlmodel.SQLType) which is the second candidate type.
//
// Returns sqlmodel.SQLType which is the first hook result, the wider of the two when both
// share a numeric category, the winning number across categories, otherwise left.
func (engine *Dialect) PromoteType(
	left sqlmodel.SQLType,
	right sqlmodel.SQLType,
) sqlmodel.SQLType {
	promoted, answered := dialectkit.FirstHookAnswer(engine.settings.PromoteTypeHooks,
		func(hook func(left, right sqlmodel.SQLType) *sqlmodel.SQLType) *sqlmodel.SQLType {
			return hook(left, right)
		})
	if answered {
		return promoted
	}

	if across, numeric := dialectkit.PromoteAcrossNumericCategories(left, right); numeric {
		return across
	}
	if left.Category != right.Category {
		return left
	}

	switch left.Category {
	case sqlmodel.TypeCategoryInteger, sqlmodel.TypeCategoryFloat:
		if right.Width > left.Width {
			return right
		}
		return left
	default:
		return left
	}
}

// CanImplicitCast reports whether PostgreSQL allows an implicit conversion. A decimal
// converts to a float implicitly, but a float converts to a decimal only by assignment.
//
// Takes from (sqlmodel.SQLType) which is the source type.
// Takes to (sqlmodel.SQLType) which is the destination type.
//
// Returns bool which is true when the conversion is permitted.
func (engine *Dialect) CanImplicitCast(
	from sqlmodel.SQLType,
	to sqlmodel.SQLType,
) bool {
	allowed, answered := dialectkit.FirstHookAnswer(engine.settings.ImplicitCastHooks,
		func(hook func(from, to sqlmodel.SQLType) *bool) *bool { return hook(from, to) })
	if answered {
		return allowed
	}

	switch {
	case from.Category == sqlmodel.TypeCategoryInteger && to.Category == sqlmodel.TypeCategoryFloat:
		return true
	case from.Category == sqlmodel.TypeCategoryInteger && to.Category == sqlmodel.TypeCategoryDecimal:
		return true
	case from.Category == sqlmodel.TypeCategoryDecimal && to.Category == sqlmodel.TypeCategoryFloat:
		return true
	case from.Category == sqlmodel.TypeCategoryText && to.Category == sqlmodel.TypeCategoryText:
		return true
	default:
		return false
	}
}

// CoerceArgument recognises PostgreSQL function-argument conversions beyond
// CanImplicitCast. A quoted literal is read as the parameter's type, which never makes
// the result nullable.
//
// Takes argument (dialect.CallArgument) which is the value the call passes.
//
// Returns dialect.ArgumentCoercion which says the conversion never gives NULL.
// Returns bool which is true when PostgreSQL converts the argument.
func (*Dialect) CoerceArgument(argument dialect.CallArgument, _ sqlmodel.SQLType) (dialect.ArgumentCoercion, bool) {
	switch {
	case argument.Literal && argument.Type.Category == sqlmodel.TypeCategoryText:
		return dialect.ArgumentCoercion{MayBeNull: false}, true
	default:
		return dialect.ArgumentCoercion{}, false
	}
}

// UntypedArgumentType chooses float8, PostgreSQL's preferred numeric type, for a
// placeholder such as abs($1) whose overloads all take numbers.
//
// Takes candidates ([]sqlmodel.SQLType) which holds the overloads' declared types.
//
// Returns sqlmodel.SQLType which is double precision.
// Returns bool which is false unless every candidate is a number and one is float8.
func (*Dialect) UntypedArgumentType(candidates []sqlmodel.SQLType) (sqlmodel.SQLType, bool) {
	preferred := -1
	for index := range candidates {
		if !dialectkit.IsNumericCategory(candidates[index]) {
			return sqlmodel.SQLType{}, false
		}
		if candidates[index].Category == sqlmodel.TypeCategoryFloat && candidates[index].Width == widthDouble {
			preferred = index
		}
	}
	if preferred < 0 {
		return sqlmodel.SQLType{}, false
	}
	return candidates[preferred], true
}

// CoreTypes returns PostgreSQL's core types: boolean, text, bigint for counts, json,
// integer for a LIMIT or OFFSET parameter, no implicit row identifier, and void.
//
// Returns dialect.CoreTypes which holds the core types.
func (engine *Dialect) CoreTypes() dialect.CoreTypes {
	return engine.coreTypes()
}

// BinaryResult types the PostgreSQL operators whose result differs from the common type
// of their operands: temporal arithmetic, such as the interval between two timestamps, a
// division of integers where the flavour makes it exact, concatenation, JSON extraction,
// the operators that test and yield a boolean, and the shifts.
//
// Takes operator (string) which is the operator.
// Takes left (sqlmodel.SQLType) which is the left operand's type.
// Takes right (sqlmodel.SQLType) which is the right operand's type.
//
// Returns sqlmodel.SQLType which is the result's type.
// Returns sqlmodel.FunctionNullableBehaviour which says when the result is NULL.
// Returns bool which is false for an operator left to the structural default.
func (engine *Dialect) BinaryResult(operator string, left, right sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
	return engine.binaryResult(operator, left, right)
}

// UnaryResult types NOT as a boolean that is NULL when its operand is.
//
// Takes operator (string) which is the operator.
//
// Returns sqlmodel.SQLType which is the result's type.
// Returns sqlmodel.FunctionNullableBehaviour which says when the result is NULL.
// Returns bool which is true only for NOT.
func (engine *Dialect) UnaryResult(operator string, _ sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
	return engine.unaryResult(operator)
}

// ResolveFunctionCall resolves a function call using PostgreSQL overload rules.
//
// A generate_series called in a select list is typed by its bounds, as in FROM, and a
// function the flavour withholds, such as mode in CockroachDB, is left unresolved. A
// panic, in the resolver or a type hook, is recovered and returned as a
// dialectkit.PanicError, and its stack is logged once at Warn through the context's
// logger.
//
// Takes catalogue (*sqlmodel.Catalogue) which provides the registered functions to
// overload resolve against.
// Takes name (string) which is the function name to resolve; its schema qualifier does
// not change how the built-in rules type it.
// Takes argumentTypes ([]sqlmodel.SQLType) which gives the actual argument types of the
// call.
//
// Returns resolution (*ast.FunctionResolution) which describes the matched signature and
// coercions.
// Returns err (error) when no matching signature can be resolved or resolving panics.
func (engine *Dialect) ResolveFunctionCall(
	ctx context.Context,
	catalogue *sqlmodel.Catalogue,
	name string,
	_ string,
	argumentTypes []sqlmodel.SQLType,
) (resolution *ast.FunctionResolution, err error) {
	defer func() { dialectkit.Recover(ctx, "postgres: resolve function call", recover(), &resolution, &err) }()

	if _, withheld := engine.withheldFunctions[strings.ToLower(name)]; withheld {
		return nil, nil
	}
	if strings.EqualFold(name, seriesFunction) {
		if seriesType, typed := engine.seriesType(argumentTypes); typed {
			return &ast.FunctionResolution{
				ReturnType:        seriesType,
				NullingArgument:   0,
				NullableBehaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
				DataAccess:        sqlmodel.DataAccessReadOnly,
				IsAggregate:       false,
				ReturnsSet:        true,
				ElementsKnown:     false,
			}, nil
		}
	}
	switch strings.ToLower(name) {
	case "coalesce", "greatest", "least":
		return engine.resolveCommonArgument(argumentTypes), nil
	}
	return resolveFunctionCall(catalogue, name, argumentTypes)
}

// LoadExtensionFunctions returns the signatures for a PostgreSQL extension. Each call
// returns signatures of its own, so a caller may change them without changing what a
// later call returns.
//
// Takes name (string) which is the extension to look up.
//
// Returns []*sqlmodel.Function which lists the extension signatures, or nil when the
// extension is unknown.
func (*Dialect) LoadExtensionFunctions(name string) []*sqlmodel.Function {
	return copyExtensionFunctions(lookupExtensionFunctions(name))
}

// describe builds the dialect's description from its settings.
//
// Returns dialect.Info which describes the dialect, holding slices and maps of its own.
func (engine *Dialect) describe() dialect.Info {
	info := dialect.Info{
		Comments:            ast.DefaultSQLCommentStyle(),
		Lexical:             engine.settings.LexicalRules,
		Name:                engine.settings.Name,
		Family:              Family,
		SchemaSearchPath:    engine.searchPath(),
		RowIDAliases:        nil,
		SystemColumns:       systemColumns,
		SelectAliases:       ast.ClauseGroupBy | ast.ClauseOrderBy,
		SelectAliasOperands: 0,
		OperatorFeatures:    operatorFeatures,
		Identifiers:         sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingLower, Insensitive: false},
		Placeholders: dialect.Placeholders{
			Sigils: []dialect.PlaceholderSigil{
				{Prefix: "$", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered},
				{Prefix: ":", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNamed},
			},
			Execution:  dialect.PlaceholderSigil{Prefix: "$", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered},
			Positional: dialect.PlaceholderSigil{},
			NamePrefix: "",
		},
		Expressions: ast.SQLFeaturesBase |
			ast.SQLFeatureScalarSubquery |
			ast.SQLFeatureWindowFunction |
			ast.SQLFeatureArraySubscript |
			ast.SQLFeatureJSONOp |
			ast.SQLFeatureBitwiseOp |
			ast.SQLFeatureStructFieldAccess,
		Returning:              ast.DataChangeInsert | ast.DataChangeUpdate | ast.DataChangeDelete,
		AsyncMutations:         false,
		ScalarFunctionsInFrom:  true,
		UngroupedColumns:       dialect.UngroupedColumnsRejected,
		ViewsFollowTheirTables: false,
		IdentifierQuote:        dialect.IdentifierQuote{Open: `"`, Close: `"`},
		Drivers:                []string{driverPgx},
		ToJSON:                 "to_json",
		TextType:               "text",
		Filters:                dialect.FilterSyntax{Operators: filterOperators, JSONPathFunctions: nil, Casts: castForms, CastTypes: castTypes},
		Lists:                  listSyntax,
		Pagination:             paginationSyntax,
		MaxParameters:          maxParameters,
	}
	return info.Clone()
}

// classify classifies one lexed statement: by its leading words, by the flavour's INSERT
// words, and then by the first statement extension that claims it.
//
// Takes sql (string) which is the script the statement was read from.
// Takes statement (dialectkit.LexedStatement[tokenKind]) which is the statement.
//
// Returns *parsedStatement which is the classified statement.
func (engine *Dialect) classify(sql string, statement dialectkit.LexedStatement[tokenKind]) *parsedStatement {
	kind := classifyStatement(statement.Tokens)
	if opensInsert(statement.Tokens, engine.settings.InsertKeywords) {
		kind = statementKindInsert
	}
	extensionOwner := -1
	if len(engine.settings.StatementExtensions) > 0 {
		override, ownerIndex := classifyViaExtensionsIndexed(statement.Tokens, engine.settings.StatementExtensions)
		if override != statementKindUnknown {
			kind = override
			extensionOwner = ownerIndex
		}
	}
	return &parsedStatement{
		source:         sql,
		tokens:         statement.Tokens,
		kind:           kind,
		extensionOwner: extensionOwner,
		start:          statement.Span.Start,
		end:            statement.Span.End,
		overBudget:     statement.OverBudget,
	}
}

// applyParsedDDL runs the statement's handler and the post-parse hooks, with a parser
// whose walk stops once the context is done or the walk stops advancing; the stopped walk
// unwinds to ApplyDDL, which returns its error.
//
// Takes parsed (*parsedStatement) which is the classified statement.
//
// Returns *ast.CatalogueMutation which describes the catalogue change.
// Returns error when the statement is malformed or a hook fails.
func (engine *Dialect) applyParsedDDL(ctx context.Context, parsed *parsedStatement) (*ast.CatalogueMutation, error) {
	statementParser := engine.newStatementParser(dialectkit.WalkGuard(ctx, len(parsed.tokens)), parsed)

	mutation, err := engine.dispatchDDL(statementParser, parsed)
	if statementParser.syntax.err != nil {
		return nil, statementParser.syntax.err
	}
	if err != nil {
		return mutation, err
	}
	if mutation != nil {
		for _, part := range statementParser.unanalysedParts {
			ast.WithUnanalysedPart(part)(mutation)
		}
	}

	if len(engine.settings.PostParseHooks) > 0 {
		hookContext := newParserContext(statementParser, engine)
		for index, hook := range engine.settings.PostParseHooks {
			if hookErr := hook(hookContext, parsed.kind, mutation); hookErr != nil {
				return mutation, fmt.Errorf("post-parse hook %d: %w", index, hookErr)
			}
		}
	}

	return mutation, nil
}

// searchPath returns the search path the database's sessions start with.
//
// Returns []string which is the path WithSearchPath set, or public alone.
func (engine *Dialect) searchPath() []string {
	if len(engine.settings.SearchPath) > 0 {
		return slices.Clone(engine.settings.SearchPath)
	}
	return []string{"public"}
}

// newStatementParser returns a parser over a statement's tokens, configured with the
// dialect's limits and types and the statement's source text. The parsers it starts for
// nested queries share its interrupt, so the whole statement's walk shares one budget.
//
// Takes interrupt (func() error) which stops the walk, as one dialectkit.WalkGuard built
// for the statement does.
// Takes parsed (*parsedStatement) which is the classified statement.
//
// Returns *parser which is ready to walk the statement.
func (engine *Dialect) newStatementParser(interrupt func() error, parsed *parsedStatement) *parser {
	statementParser := newParser(parsed.tokens)
	statementParser.Interrupt = interrupt
	statementParser.maxParseDepth = engine.settings.Limits.ParseDepth()
	statementParser.types = engine.typeResolver
	statementParser.functions = engine.functions
	statementParser.source = parsed.source
	statementParser.insertKeywords = engine.settings.InsertKeywords
	statementParser.aliasNamesSingleColumnFunctions = engine.settings.AliasNamesSingleColumnFunctions
	statementParser.indexOptions = engine.settings.InlineIndexElements
	statementParser.computedColumnShorthand = engine.settings.ComputedColumnShorthand
	statementParser.notVisibleColumns = engine.settings.NotVisibleColumns
	statementParser.asOfSystemTime = engine.settings.AsOfSystemTime
	statementParser.parametersTypedFromContext = engine.settings.ParametersTypedFromContext
	statementParser.highestNumberedPlaceholder = highestPlaceholderNumber(parsed.tokens)
	return statementParser
}

// checkTokenBudget reports a statement over the per-statement token budget: one whose
// tokenising stopped at a budget, or one holding more tokens than the dialect's budget.
//
// Takes parsed (*parsedStatement) which is the classified statement.
//
// Returns error which wraps dialectkit.ErrTokenBudget, or nil when the statement fits.
func (engine *Dialect) checkTokenBudget(parsed *parsedStatement) error {
	if parsed.overBudget {
		return dialectkit.TokenBudgetExceeded(len(parsed.tokens))
	}
	return engine.settings.Limits.CheckTokens(len(parsed.tokens))
}

// hasDDLHandler reports whether a built-in handler or the owning extension will parse the
// statement, so the token budget is only charged to statements the parser walks.
//
// Takes parsed (*parsedStatement) which is the classified statement.
//
// Returns bool which is true when dispatchDDL would invoke a parser for the statement.
func (engine *Dialect) hasDDLHandler(parsed *parsedStatement) bool {
	if parsed.kind < pgext.StatementKindExtensionBase {
		return int(parsed.kind) < len(ddlHandlers) && ddlHandlers[parsed.kind] != nil
	}
	return parsed.extensionOwner >= 0 && parsed.extensionOwner < len(engine.settings.StatementExtensions)
}

// dispatchDDL routes a parsed statement to its handler.
//
// Built-in kinds dispatch through the ddlHandlers array; extension kinds (from
// pgext.StatementKindExtensionBase up) dispatch through the cached extensionOwner index
// recorded on parsedStatement during ParseStatements, avoiding the per-dispatch
// re-Classify scan. The function rejects any extension kind that did not pass through
// ParseStatements (extensionOwner == -1) or whose owner index points outside the current
// extension registry. Both shapes indicate a caller has hand-rolled a parsedStatement
// with inconsistent fields, so the safer response is to decline and let the post-parse
// hook chain run rather than risk reading past the registry. It returns (nil, nil) when
// no handler claims the statement so the post-parse hook chain still runs (hooks may want
// to observe even non-matched statements).
//
// Takes p (*parser) which is positioned at the start of the statement tokens.
// Takes parsed (*parsedStatement) which carries the statement kind and extension owner.
//
// Returns *ast.CatalogueMutation which describes the catalogue change, or nil.
// Returns error when the claiming handler fails.
func (engine *Dialect) dispatchDDL(
	p *parser,
	parsed *parsedStatement,
) (*ast.CatalogueMutation, error) {
	kind := parsed.kind
	if kind < pgext.StatementKindExtensionBase {
		if int(kind) < len(ddlHandlers) && ddlHandlers[kind] != nil {
			return ddlHandlers[kind](p, engine)
		}
		return nil, nil
	}
	if parsed.extensionOwner < 0 || parsed.extensionOwner >= len(engine.settings.StatementExtensions) {
		return nil, nil
	}
	ctx := newParserContext(p, engine)
	owner := engine.settings.StatementExtensions[parsed.extensionOwner]
	return owner.Parse(ctx, kind)
}

// settings holds configuration for a PostgreSQL variant. Flavours such as CockroachDB,
// YugabyteDB or TimescaleDB add their own types, functions, hooks and statement
// extensions to it through the options, without forking the parser.
//
// Every option that registers something adds to what earlier options registered, so a
// flavour's own settings survive a caller's options of the same kind.
type settings struct {
	// ExtraTypes holds the extra type definitions, keyed by lower-cased name. Each
	// WithExtraTypes merges into it, a later entry replacing an earlier one of the same
	// name.
	ExtraTypes map[string]sqlmodel.SQLType

	// Name is the dialect name Info reports. It defaults to "postgres"; flavours such as
	// cockroachdb or timescaledb set it through WithName.
	Name string

	// ExtraFunctions holds the function registrations, run in order after the built-in
	// catalogue is built.
	ExtraFunctions []func(*FunctionCatalogueBuilder)

	// WithheldFunctions holds the lower-cased names of the built-in functions left out of
	// the catalogue, before any extra function registers. Each WithoutBuiltinFunctions adds
	// to it.
	WithheldFunctions []string

	// TypeNormaliserHooks holds the NormaliseTypeName hooks in registration order. The most
	// recently added hook is consulted first.
	TypeNormaliserHooks []func(name string, modifiers []int) *sqlmodel.SQLType

	// ImplicitCastHooks holds the CanImplicitCast hooks in registration order. The most
	// recently added hook is consulted first.
	ImplicitCastHooks []func(from, to sqlmodel.SQLType) *bool

	// PromoteTypeHooks holds the PromoteType hooks in registration order. The most recently
	// added hook is consulted first.
	PromoteTypeHooks []func(left, right sqlmodel.SQLType) *sqlmodel.SQLType

	// StatementExtensions lets child engines recognise and parse SQL statements postgres
	// does not natively support.
	//
	// Extensions are consulted in registration order. Extension claims override built-in
	// classifications even when the built-in classifier already returned a non-unknown kind;
	// this is required to recognise function-call-form DDL like TimescaleDB's `SELECT
	// create_hypertable(...)` which the built-in classifier would otherwise route as a
	// SELECT. Only kinds from pgext.StatementKindExtensionBase up are honoured; an extension
	// that returns a built-in kind is ignored.
	StatementExtensions []pgext.StatementExtensionPort

	// PostParseHooks run in registration order after successful DDL dispatch, including
	// extension handlers and statements with no handler. Used by child engines to enrich
	// mutations with engine-specific metadata; the mutation may be nil.
	PostParseHooks []pgext.PostParseHook

	// InsertKeywords holds the upper-cased words that open an INSERT besides INSERT itself,
	// such as CockroachDB's UPSERT. Each WithInsertKeywords adds to it.
	InsertKeywords []string

	// RowValueTypeHooks holds the RowValueType hooks in registration order. The most
	// recently added hook is consulted first.
	RowValueTypeHooks []func(relation dialect.RowRelation) *sqlmodel.SQLType

	// AccessorTypes lists the types, by name, whose values -> applies to the value on its
	// left as the call of the accessor's function on it. WithAccessorArrows adds to it.
	AccessorTypes []string

	// SearchPath lists the schemas the database's sessions start with on their search path,
	// in order, as WithSearchPath sets them; empty for PostgreSQL's public alone.
	SearchPath []string

	// LexicalRules decide where ParseStatements splits a script into statements, and Info
	// reports them as the dialect's lexical rules. New starts from PostgreSQL's rules; a
	// flavour replaces them with its own through WithLexicalRules.
	LexicalRules sqllex.Rules

	// Limits caps recursion through analyseSelect (subquery, CTE and compound-branch
	// nesting) and the expression chain, and the number of tokens one statement may hold.
	// Its zero value selects the dialectkit defaults.
	Limits dialectkit.Limits

	// AliasNamesSingleColumnFunctions makes the alias of a table function that returns one
	// column name that column, whatever the function calls it. It is set by
	// WithAliasNamesSingleColumnFunctions.
	AliasNamesSingleColumnFunctions bool

	// DecimalIntegerDivision makes / between two integers return numeric. It is set by
	// WithDecimalIntegerDivision.
	DecimalIntegerDivision bool

	// InlineIndexElements reads INDEX, UNIQUE INDEX, INVERTED INDEX, VECTOR INDEX and FAMILY
	// elements of a CREATE TABLE as declarations rather than as columns, and the USING HASH
	// and STORING options of a primary key or unique constraint. It is set by
	// WithInlineIndexElements.
	InlineIndexElements bool

	// ComputedColumnShorthand reads AS (expression) STORED or VIRTUAL after a column's type
	// as a computed column. It is set by WithComputedColumnShorthand.
	ComputedColumnShorthand bool

	// NotVisibleColumns reads NOT VISIBLE on a column as hiding it from SELECT *. It is set
	// by WithNotVisibleColumns.
	NotVisibleColumns bool

	// AsOfSystemTime reads AS OF SYSTEM TIME after the FROM clause of a query. It is set by
	// WithAsOfSystemTime.
	AsOfSystemTime bool

	// ParametersTypedFromContext reports that the engine types a parameter from what its
	// place wants. It is set by WithParametersTypedFromContext.
	ParametersTypedFromContext bool
}

// ddlHandler is a function that parses a DDL statement into a catalogue mutation.
type ddlHandler func(*parser, *Dialect) (*ast.CatalogueMutation, error)

// WithName sets the name the dialect reports in Info, for a flavour built on this
// dialect. The family stays postgres, so type mappings and drivers keep resolving.
//
// Takes name (string) which is the dialect name to report.
//
// Returns Option which applies the name.
func WithName(name string) Option {
	return func(configured *settings) {
		configured.Name = name
	}
}

// WithExtraTypes adds named type definitions to the PostgreSQL dialect.
//
// Names are folded to lower case, so an unquoted type name matches whatever its case, and
// a quoted one only when written in lower case, as PostgreSQL keeps "Point" apart from
// point. Extra definitions override built-ins, and later registrations replace earlier
// definitions of the same name. Within one call, names that differ only in case are taken
// in sorted order, so the one that sorts last wins, whatever order the map gives them in.
//
// Takes types (map[string]sqlmodel.SQLType) which maps each type name to its definition.
//
// Returns Option which adds the types.
func WithExtraTypes(types map[string]sqlmodel.SQLType) Option {
	return func(configured *settings) {
		if configured.ExtraTypes == nil {
			configured.ExtraTypes = make(map[string]sqlmodel.SQLType, len(types))
		}
		for _, name := range slices.Sorted(maps.Keys(types)) {
			configured.ExtraTypes[strings.ToLower(name)] = types[name]
		}
	}
}

// WithExtraFunctions registers additional functions after the built-in PostgreSQL
// function catalogue is built. Repeated calls add registrations, which run in order.
//
// Takes register (func(*FunctionCatalogueBuilder)) which adds further signatures to the
// builder.
//
// Returns Option which adds the registration.
func WithExtraFunctions(register func(*FunctionCatalogueBuilder)) Option {
	return func(configured *settings) {
		if register != nil {
			configured.ExtraFunctions = append(configured.ExtraFunctions, register)
		}
	}
}

// WithoutBuiltinFunctions removes all overloads of the named built-in functions.
//
// Names match without regard to case, and repeated calls accumulate. Extra functions are
// registered afterwards, allowing replacement signatures.
//
// Takes names (...string) which name the built-in functions to leave out.
//
// Returns Option which leaves them out.
func WithoutBuiltinFunctions(names ...string) Option {
	return func(configured *settings) {
		for _, name := range names {
			configured.WithheldFunctions = append(configured.WithheldFunctions, strings.ToLower(name))
		}
	}
}

// WithTypeNormaliserHook adds a NormaliseTypeName hook. Hooks are consulted most recent
// first, before the extra and built-in types; the first non-nil result wins.
//
// A quoted type name holding an upper-case letter is never passed to a hook, as it names
// no type spelt in lower case.
//
// Takes hook (func(string, []int) *sqlmodel.SQLType) which receives the lower-cased type
// name and its modifiers and returns the resolved type, or nil to decline.
//
// Returns Option which adds the hook.
func WithTypeNormaliserHook(hook func(string, []int) *sqlmodel.SQLType) Option {
	return func(configured *settings) {
		if hook != nil {
			configured.TypeNormaliserHooks = append(configured.TypeNormaliserHooks, hook)
		}
	}
}

// WithImplicitCastHook adds a CanImplicitCast hook. Hooks are consulted most recent
// first, before the built-in rules; the first non-nil result wins.
//
// Takes hook (func(from, to sqlmodel.SQLType) *bool) which returns whether the cast is
// allowed, or nil to decline.
//
// Returns Option which adds the hook.
func WithImplicitCastHook(hook func(from, to sqlmodel.SQLType) *bool) Option {
	return func(configured *settings) {
		if hook != nil {
			configured.ImplicitCastHooks = append(configured.ImplicitCastHooks, hook)
		}
	}
}

// WithPromoteTypeHook adds a PromoteType hook. Hooks are consulted most recent first,
// before the built-in rules; the first non-nil result wins.
//
// Takes hook (func(left, right sqlmodel.SQLType) *sqlmodel.SQLType) which returns the
// promoted type, or nil to decline.
//
// Returns Option which adds the hook.
func WithPromoteTypeHook(hook func(left, right sqlmodel.SQLType) *sqlmodel.SQLType) Option {
	return func(configured *settings) {
		if hook != nil {
			configured.PromoteTypeHooks = append(configured.PromoteTypeHooks, hook)
		}
	}
}

// WithStatementExtensions registers statement extensions that recognise and parse SQL
// beyond the built-in grammar, including specialised forms of built-in statement kinds.
//
// Extensions are appended in declaration order and consulted in that order; the first
// that claims a statement parses it. An extension that returns a kind below
// pgext.StatementKindExtensionBase is ignored. A valid claim overrides the built-in
// classification, as needed for forms such as SELECT create_hypertable(...).
//
// Takes extensions (...pgext.StatementExtensionPort) which are appended in declaration
// order; a nil extension is left out.
//
// Returns Option which adds the extensions.
func WithStatementExtensions(extensions ...pgext.StatementExtensionPort) Option {
	return func(configured *settings) {
		for _, extension := range extensions {
			if extension != nil {
				configured.StatementExtensions = append(configured.StatementExtensions, extension)
			}
		}
	}
}

// WithPostParseHook registers a hook that runs after successful DDL dispatch, including
// extension handlers and statements with no handler. Hooks run in declaration order.
//
// Takes hook (pgext.PostParseHook) which runs after each built-in DDL handler.
//
// Returns Option which adds the hook.
func WithPostParseHook(hook pgext.PostParseHook) Option {
	return func(configured *settings) {
		if hook != nil {
			configured.PostParseHooks = append(configured.PostParseHooks, hook)
		}
	}
}

// WithInsertKeywords registers additional words that begin INSERT-like statements.
//
// This supports dialect forms such as CockroachDB's UPSERT. Registrations accumulate.
// These statements do not accept ON CONFLICT, and their analysis records that they
// replace a conflicting row, as ON CONFLICT DO UPDATE does.
//
// Takes keywords (...string) which are the words, matched without regard to case.
//
// Returns Option which adds the words.
func WithInsertKeywords(keywords ...string) Option {
	return func(configured *settings) {
		for _, keyword := range keywords {
			if keyword != "" {
				configured.InsertKeywords = append(configured.InsertKeywords, strings.ToUpper(keyword))
			}
		}
	}
}

// WithRowValueTypeHook adds a hook for typing a relation used as a value.
//
// The newest hook is tried first. A nil result falls through to earlier hooks and then
// PostgreSQL's default behaviour.
//
// Takes hook (func(dialect.RowRelation) *sqlmodel.SQLType) which returns the row's type,
// or nil to leave the relation to the earlier hooks and the built-in rule.
//
// Returns Option which adds the hook.
func WithRowValueTypeHook(hook func(relation dialect.RowRelation) *sqlmodel.SQLType) Option {
	return func(configured *settings) {
		if hook != nil {
			configured.RowValueTypeHooks = append(configured.RowValueTypeHooks, hook)
		}
	}
}

// WithAliasNamesSingleColumnFunctions makes a table function's alias name its single
// output column.
//
// This follows CockroachDB's rules. An explicit column alias list still takes precedence.
//
// Returns Option which sets the rule.
func WithAliasNamesSingleColumnFunctions() Option {
	return func(configured *settings) {
		configured.AliasNamesSingleColumnFunctions = true
	}
}

// WithAccessorArrows types value -> accessor as the equivalent accessor-function call,
// selecting an overload from the value type. The result is unknown when no overload
// matches or matching return types disagree; JSON arrows keep their usual meaning.
//
// Takes accessorTypes (...string) which name the accessor types, as the dialect's type
// table spells them; a later call adds to them.
//
// Returns Option which sets the rule.
func WithAccessorArrows(accessorTypes ...string) Option {
	return func(configured *settings) {
		configured.AccessorTypes = append(configured.AccessorTypes, accessorTypes...)
	}
}

// WithDecimalIntegerDivision makes / between two integers return numeric, the exact
// quotient, as CockroachDB divides them, where PostgreSQL truncates the quotient to an
// integer. CockroachDB's // is its integer division.
//
// Returns Option which sets the rule.
func WithDecimalIntegerDivision() Option {
	return func(configured *settings) {
		configured.DecimalIntegerDivision = true
	}
}

// WithInlineIndexElements enables CockroachDB index and column-family declarations inside
// CREATE TABLE.
//
// Unique indexes become unique constraints. The parser also accepts hash-sharding and
// stored-column options on keys.
//
// Returns Option which sets the rule.
func WithInlineIndexElements() Option {
	return func(configured *settings) {
		configured.InlineIndexElements = true
	}
}

// WithComputedColumnShorthand reads AS (expression) STORED or VIRTUAL after a column's
// type as a computed column, CockroachDB's form of GENERATED ALWAYS AS (expression), so
// an INSERT leaves the column out, as it does a generated one. PostgreSQL requires the
// GENERATED ALWAYS keywords.
//
// Returns Option which sets the rule.
func WithComputedColumnShorthand() Option {
	return func(configured *settings) {
		configured.ComputedColumnShorthand = true
	}
}

// WithNotVisibleColumns reads NOT VISIBLE on a column as hiding it from SELECT * and t.*,
// as CockroachDB does, while a query can still name it. PostgreSQL has no hidden columns.
//
// Returns Option which sets the rule.
func WithNotVisibleColumns() Option {
	return func(configured *settings) {
		configured.NotVisibleColumns = true
	}
}

// WithAsOfSystemTime enables CockroachDB historical and follower reads through AS OF
// SYSTEM TIME without changing column types. The clause requires a constant time on the
// top-level query and rejects parameters.
//
// Returns Option which sets the rule.
func WithAsOfSystemTime() Option {
	return func(configured *settings) {
		configured.AsOfSystemTime = true
	}
}

// WithParametersTypedFromContext enables CockroachDB-style parameter inference, including
// text for any-type function arguments and integers for LIMIT or OFFSET. Only parameters
// first encountered as operands of NULL tests remain subject to untyped-parameter
// warnings.
//
// Returns Option which sets the rule.
func WithParametersTypedFromContext() Option {
	return func(configured *settings) {
		configured.ParametersTypedFromContext = true
	}
}

// WithSearchPath sets the initial schema search order, defaulting to public. Migrations
// create unqualified objects in the first existing schema, and generated names omit
// schemas on the path.
//
// Names follow unquoted identifier rules; "$user" is omitted, and migrations may later
// change the path.
//
// Takes schemas (...string) which are the schemas, in order; none keeps the default.
//
// Returns Option which applies the search path.
func WithSearchPath(schemas ...string) Option {
	return func(configured *settings) {
		configured.SearchPath = slices.DeleteFunc(slices.Clone(schemas), func(schema string) bool {
			return schema == currentUserSchema || schema == ""
		})
	}
}

// WithLexicalRules replaces the lexical rules ParseStatements splits a script with, and
// that Info reports, for a flavour whose statement boundaries or transaction rules differ
// from PostgreSQL's. The rules should describe PostgreSQL's quoting and comments, because
// each statement is still read by the PostgreSQL tokeniser.
//
// Takes rules (sqllex.Rules) which are the flavour's lexical rules, such as those its own
// lexical package returns.
//
// Returns Option which applies the rules to the dialect.
func WithLexicalRules(rules sqllex.Rules) Option {
	return func(configured *settings) {
		configured.LexicalRules = rules
	}
}

// WithMaxParseDepth sets the maximum parser recursion depth across analysis and
// expression nesting.
//
// Deeply nested user input is otherwise able to overflow the goroutine stack with a
// fatal, non-recoverable error that the engine's recover guards cannot catch. The default
// is high (dialectkit.DefaultMaxParseDepth) so realistic queries are unaffected; lower it
// to harden against hostile input or raise it for unusually nested generated queries.
// Exceeding it fails the statement with an error wrapping dialectkit.ErrParseDepth.
//
// Takes depth (int) which is the maximum nesting depth; values below 1 are ignored so the
// default remains in force, and values above dialectkit.MaxParseDepthCeiling are held at
// the ceiling.
//
// Returns Option which applies the depth cap to the dialect.
func WithMaxParseDepth(depth int) Option {
	return func(configured *settings) {
		configured.Limits = configured.Limits.WithMaxParseDepth(depth)
	}
}

// WithMaxTokensPerStatement sets the maximum number of tokens one statement may hold.
//
// The budget bounds the memory and work an adversarially long statement costs: a script
// is tokenised no further into a statement than the budget, and a statement over it that
// the parser would walk fails with an error wrapping dialectkit.ErrTokenBudget. The
// default is high (dialectkit.DefaultMaxTokensPerStatement) so realistic statements,
// including generated SQL with large IN lists, are unaffected; lower it to harden against
// hostile input or raise it for unusually large generated statements. A statement the
// parser does not walk, such as a large seed INSERT inside a migration, is not refused.
//
// Takes limit (int) which is the maximum token count; values below 1 are ignored so the
// default remains in force, and values above dialectkit.MaxTokensPerStatementCeiling are
// held at the ceiling.
//
// Returns Option which applies the token budget to the dialect.
func WithMaxTokensPerStatement(limit int) Option {
	return func(configured *settings) {
		configured.Limits = configured.Limits.WithMaxTokensPerStatement(limit)
	}
}

// checkRunOn detects adjacent migration statements with a missing semicolon. It skips
// functions, procedures and schemas that can contain statements, and recognises privilege
// uses of CREATE in GRANT or REVOKE.
//
// Takes parsed (*parsedStatement) which is the statement.
//
// Returns error which locates the statement that runs on, the error reading a statement
// over the token budget met, or nil.
func checkRunOn(ctx context.Context, parsed *parsedStatement) error {
	if parsed.kind == statementKindCreateFunction || parsed.kind == statementKindCreateSchema {
		return nil
	}
	rules := runOnRules
	if parsed.kind == statementKindAlterTable {
		rules = alterTableRunOnRules
	}
	next, found, err := findRunOn(ctx, parsed, rules)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	return dialectkit.NewParseError(next, "a semicolon is missing before "+strings.ToUpper(next.Value)+
		": the statement runs on into the next one")
}

// findRunOn finds where a statement runs on into the next one. A statement over the token
// budget kept only its first tokens, so its whole span is read again a token at a time,
// in constant memory, to find a run-on past them.
//
// Takes parsed (*parsedStatement) which is the statement.
// Takes rules (dialectkit.RunOnRules[tokenKind]) which say which words open a statement.
//
// Returns token which is the first token of the statement it runs on into.
// Returns bool which is true when the statement runs on.
// Returns error which is the context's cause once it is done, or the error re-reading a
// statement over the budget met.
func findRunOn(ctx context.Context, parsed *parsedStatement, rules dialectkit.RunOnRules[tokenKind]) (token, bool, error) {
	if !parsed.overBudget {
		next, found := dialectkit.RunOnStatement(parsed.source, parsed.tokens, rules)
		return next, found, nil
	}
	return dialectkit.RunOnStream(ctx, parsed.source, readStatementTokens(parsed.source, parsed.start, parsed.end),
		tokenEOF, rules)
}

// queryAnalyser selects an analyser for the statement's kind.
//
// Statements claimed by extensions still use their leading keyword to select query
// analysis.
//
// Takes parsed (*parsedStatement) which is the classified statement.
//
// Returns func(*parser) (*ast.RawQueryAnalysis, error) which analyses the statement, or
// nil when the statement has nothing to analyse.
func queryAnalyser(parsed *parsedStatement) func(*parser) (*ast.RawQueryAnalysis, error) {
	if int(parsed.kind) < len(statementAnalysers) {
		return statementAnalysers[parsed.kind]
	}
	if len(parsed.tokens) == 0 {
		return nil
	}
	switch {
	case parsed.tokens[0].IsKeyword(tokenIdentifier, "CALL"):
		return (*parser).analyseCall
	case parsed.tokens[0].IsKeyword(tokenIdentifier, "SELECT"):
		return statementAnalysers[statementKindSelect]
	default:
		return nil
	}
}

// catalogueTableFunctionColumns resolves the output columns of a user-defined
// set-returning function, as TableValuedFunctionColumnsFromCatalogue describes.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the registered function signatures;
// nil holds none.
// Takes functionName (string) which is the bare or schema-qualified function name.
//
// Returns []ast.ScopedColumn which lists the output columns, or nil when no matching
// set-returning function is found.
func catalogueTableFunctionColumns(catalogue *sqlmodel.Catalogue, functionName string) []ast.ScopedColumn {
	schemaName, bareName := splitQualifiedName(functionName)
	for schema, overloads := range catalogue.FunctionsOnPath(schemaName, bareName) {
		if columns := overloadColumns(catalogue, schema, bareName, overloads); columns != nil {
			return columns
		}
	}
	return nil
}

// splitQualifiedName splits "schema.name" into ("schema", "name").
//
// A bare identifier returns ("", name).
//
// Takes qualifiedName (string) which is the bare or schema-qualified name.
//
// Returns schema (string) which is the schema part, or empty for a bare name.
// Returns name (string) which is the bare name part.
func splitQualifiedName(qualifiedName string) (schema string, name string) {
	before, after, found := strings.Cut(qualifiedName, ".")
	if !found {
		return "", qualifiedName
	}
	return before, after
}

// overloadColumns resolves output columns from the first function overload with a known
// result shape, preferring set-returning overloads. Composite and table-row results
// expose their fields; base types expose one column named after the function or its sole
// output parameter.
//
// Takes catalogue (*sqlmodel.Catalogue) which is searched for composite return types.
// Takes schema (*sqlmodel.Schema) which declares the overloads.
// Takes functionName (string) which is the bare function name.
// Takes signatures ([]*sqlmodel.Function) which are the schema's overloads of the
// function; a nil entry is skipped.
//
// Returns []ast.ScopedColumn which lists the output columns, or nil when no overload
// resolves to columns.
func overloadColumns(
	catalogue *sqlmodel.Catalogue,
	schema *sqlmodel.Schema,
	functionName string,
	signatures []*sqlmodel.Function,
) []ast.ScopedColumn {
	for _, returnsSet := range []bool{true, false} {
		for _, signature := range signatures {
			if signature == nil || signature.ReturnsSet != returnsSet {
				continue
			}
			if columns := signatureColumns(catalogue, schema, functionName, signature); columns != nil {
				return columns
			}
		}
	}
	return nil
}

// signatureColumns returns the columns one row of a function's result has.
//
// Takes catalogue (*sqlmodel.Catalogue) which is searched for composite return types and
// tables.
// Takes schema (*sqlmodel.Schema) which declares the function.
// Takes functionName (string) which is the bare function name.
// Takes signature (*sqlmodel.Function) which is the overload.
//
// Returns []ast.ScopedColumn which lists the columns, or nil when the result's shape is
// not known.
func signatureColumns(
	catalogue *sqlmodel.Catalogue,
	schema *sqlmodel.Schema,
	functionName string,
	signature *sqlmodel.Function,
) []ast.ScopedColumn {
	if columns := resolveCompositeColumns(catalogue, schema, signature.ReturnType); columns != nil {
		return columns
	}
	if columns := rowTypeColumns(catalogue, signature.ReturnType); columns != nil {
		return columns
	}
	if column, isBase := baseTypeColumn(catalogue, cmp.Or(signature.ResultName, functionName), signature.ReturnType); isBase {
		return []ast.ScopedColumn{column}
	}
	return nil
}

// rowTypeColumns resolves a function's table-row return type to its columns. Every column
// is nullable because row types omit table NOT NULL constraints and a single-row function
// may return a row of NULLs.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the tables.
// Takes returnType (sqlmodel.SQLType) which is the function's return type.
//
// Returns []ast.ScopedColumn which lists the columns, or nil when the return type names
// no table.
func rowTypeColumns(catalogue *sqlmodel.Catalogue, returnType sqlmodel.SQLType) []ast.ScopedColumn {
	if catalogue == nil || returnType.Category != sqlmodel.TypeCategoryUnknown || returnType.EngineName == "" {
		return nil
	}
	table := catalogue.LookupTable(returnType.Schema, returnType.EngineName)
	if table == nil {
		return nil
	}
	columns := make([]ast.ScopedColumn, 0, len(table.Columns))
	for index := range table.Columns {
		if table.Columns[index].Hidden {
			continue
		}
		columns = append(columns, ast.ScopedColumn{Name: table.Columns[index].Name, SQLType: table.Columns[index].SQLType, Nullable: true})
	}
	return columns
}

// baseTypeColumn returns the one column a function returning a base type, or a set of
// one, produces.
//
// Takes catalogue (*sqlmodel.Catalogue) which resolves an enum or domain return type.
// Takes columnName (string) which names the column.
// Takes returnType (sqlmodel.SQLType) which is the function's return type.
//
// Returns ast.ScopedColumn which is the column.
// Returns bool which is false when the return type is not a known base type, such as
// record, whose columns only a column definition list gives.
func baseTypeColumn(catalogue *sqlmodel.Catalogue, columnName string, returnType sqlmodel.SQLType) (ast.ScopedColumn, bool) {
	resolved := catalogue.ResolveNamedType(returnType)
	if resolved.Category == sqlmodel.TypeCategoryUnknown || resolved.Category == sqlmodel.TypeCategoryComposite ||
		strings.EqualFold(resolved.EngineName, recordTypeName) {
		return ast.ScopedColumn{}, false
	}
	return ast.ScopedColumn{Name: columnName, SQLType: resolved, Nullable: true}, true
}

// resolveCompositeColumns finds the columns of a composite return type.
//
// Takes catalogue (*sqlmodel.Catalogue) which is searched when the type lives in a
// different schema than the declaring schema.
// Takes declaringSchema (*sqlmodel.Schema) which is searched first for the composite type
// definition.
// Takes returnType (sqlmodel.SQLType) which carries the composite type name and optional
// schema qualifier.
//
// Returns []ast.ScopedColumn which lists the composite fields, or nil when no matching
// composite type is found.
func resolveCompositeColumns(
	catalogue *sqlmodel.Catalogue,
	declaringSchema *sqlmodel.Schema,
	returnType sqlmodel.SQLType,
) []ast.ScopedColumn {
	typeName := returnType.EngineName
	if typeName == "" {
		return nil
	}

	schema := declaringSchema
	if returnType.Schema != "" && (declaringSchema == nil || returnType.Schema != declaringSchema.Name) {
		if catalogue == nil {
			return nil
		}
		targetSchema := catalogue.Schemas[catalogue.Key(returnType.Schema)]
		if targetSchema == nil {
			return nil
		}
		schema = targetSchema
	}
	if schema == nil {
		return nil
	}

	compositeType := schema.CompositeTypes[typeName]
	if compositeType == nil {
		return nil
	}
	columns := make([]ast.ScopedColumn, len(compositeType.Fields))
	for index := range compositeType.Fields {
		columns[index] = ast.ScopedColumn{
			Name:     compositeType.Fields[index].Name,
			SQLType:  compositeType.Fields[index].SQLType,
			Nullable: compositeType.Fields[index].Nullable,
		}
	}
	return columns
}

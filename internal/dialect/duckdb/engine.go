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

package duckdb

import (
	"context"
	"fmt"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/duckdb/lexical"
	"seagull.sh/seagull/internal/sqlmodel"
)

// columnsMatcher is the name of the COLUMNS(...) column matcher.
const columnsMatcher = "columns"

var (
	// ddlHandlers dispatches each statementKind to its DDL parsing handler.
	ddlHandlers = [statementKindCount]ddlHandler{
		statementKindCreateTable: func(statementParser *parser, engine *Dialect) (*ast.CatalogueMutation, error) {
			return statementParser.parseCreateTable(engine)
		},
		statementKindDropTable: func(statementParser *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return statementParser.parseDropTable()
		},
		statementKindAlterTable: func(statementParser *parser, engine *Dialect) (*ast.CatalogueMutation, error) {
			return statementParser.parseAlterTable(engine)
		},
		statementKindCreateView: func(statementParser *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return statementParser.parseCreateView()
		},
		statementKindDropView: func(statementParser *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return statementParser.parseDropView()
		},
		statementKindAlterView: func(statementParser *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return statementParser.parseAlterView()
		},
		statementKindCreateIndex: func(statementParser *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return statementParser.parseCreateIndex()
		},
		statementKindDropIndex: func(statementParser *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return statementParser.parseDropIndex()
		},
		statementKindCreateType: func(statementParser *parser, engine *Dialect) (*ast.CatalogueMutation, error) {
			return statementParser.parseCreateType(engine)
		},
		statementKindAlterType: func(statementParser *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return statementParser.parseAlterType()
		},
		statementKindDropType: func(statementParser *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return statementParser.parseDropType()
		},
		statementKindCreateFunction: func(statementParser *parser, engine *Dialect) (*ast.CatalogueMutation, error) {
			return statementParser.parseCreateMacro(engine)
		},
		statementKindDropFunction: func(statementParser *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return statementParser.parseDropFunction()
		},
		statementKindCreateMacro: func(statementParser *parser, engine *Dialect) (*ast.CatalogueMutation, error) {
			return statementParser.parseCreateMacro(engine)
		},
		statementKindDropMacro: func(statementParser *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return statementParser.parseDropFunction()
		},
		statementKindCreateSchema: func(statementParser *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return statementParser.parseCreateSchema()
		},
		statementKindDropSchema: func(statementParser *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return statementParser.parseDropSchema()
		},
		statementKindCreateSequence: func(statementParser *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return statementParser.parseCreateSequence()
		},
		statementKindDropSequence: func(statementParser *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return statementParser.parseDropSequence()
		},
		statementKindComment: func(statementParser *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return statementParser.parseComment()
		},
		statementKindInstall: func(_ *parser, _ *Dialect) (*ast.CatalogueMutation, error) { return nil, nil },
		statementKindLoad:    func(_ *parser, _ *Dialect) (*ast.CatalogueMutation, error) { return nil, nil },
	}
)

// statementVerbs lists the words a DuckDB statement may start with whose statement the
// analyser does not read, such as transaction control, settings and maintenance, so a
// query file may hold one, while a statement starting with any other word is rejected.
var statementVerbs = map[string]struct{}{
	"ABORT": {}, "ALTER": {}, "ANALYZE": {}, "ATTACH": {}, "BEGIN": {}, "CALL": {}, "CHECKPOINT": {},
	"COMMENT": {}, "COMMIT": {}, "COPY": {}, keywordCREATE: {}, "DEALLOCATE": {}, "DESC": {}, "DESCRIBE": {},
	"DETACH": {}, keywordDROP: {}, "END": {}, "EXECUTE": {}, "EXPLAIN": {}, "EXPORT": {}, "FORCE": {},
	"IMPORT": {}, keywordINSTALL: {}, keywordLOAD: {}, "MERGE": {}, "PRAGMA": {}, "PREPARE": {}, "RESET": {},
	"ROLLBACK": {}, keywordSET: {}, "SHOW": {}, "START": {}, "SUMMARIZE": {}, "TRUNCATE": {}, "UPDATE": {},
	"USE": {}, "VACUUM": {}, keywordWITH: {},
}

var (
	// runOnRules names the words that open a DuckDB statement, for finding a statement that
	// runs on into the next one because the semicolon between them is missing.
	runOnRules = dialectkit.RunOnRules[tokenKind]{
		Starters: map[string][]string{
			keywordCREATE:  runOnCreated,
			"ALTER":        runOnAltered,
			keywordDROP:    runOnDropped,
			"COMMENT":      {keywordON},
			keywordINSTALL: nil,
			keywordLOAD:    nil,
			"FORCE":        {keywordINSTALL},
		},
		Word:  tokenIdentifier,
		Open:  tokenLeftParen,
		Close: tokenRightParen,
	}

	// queryRunOnRules are runOnRules for a statement holding a query, such as CREATE VIEW
	// ... AS SELECT, whose select list may name a column install or load at the start of a
	// line, so only the words that name what they create, alter or drop open a statement
	// there.
	queryRunOnRules = dialectkit.RunOnRules[tokenKind]{
		Starters: map[string][]string{
			keywordCREATE: runOnCreated,
			"ALTER":       runOnAltered,
			keywordDROP:   runOnDropped,
			"COMMENT":     {keywordON},
		},
		Word:  tokenIdentifier,
		Open:  tokenLeftParen,
		Close: tokenRightParen,
	}

	// alterTableRunOnRules are runOnRules for an ALTER TABLE, whose actions open with ALTER
	// and DROP followed by a column name, which may be a word such as type, and whose added
	// column may be named install or load.
	alterTableRunOnRules = dialectkit.RunOnRules[tokenKind]{
		Starters: map[string][]string{
			keywordCREATE: runOnCreated,
			"ALTER":       {keywordTABLE},
			keywordDROP:   {keywordTABLE},
			"COMMENT":     {keywordON},
		},
		Word:  tokenIdentifier,
		Open:  tokenLeftParen,
		Close: tokenRightParen,
	}

	// runOnCreated names the words a CREATE statement opens with.
	runOnCreated = []string{"OR", "TEMP", "TEMPORARY", "PERSISTENT", keywordTABLE, "VIEW", "INDEX", keywordUNIQUE,
		keywordTYPE, "FUNCTION", keywordMACRO, keywordSCHEMA, "SEQUENCE", "SECRET"}

	// runOnAltered names the objects an ALTER statement opens with.
	runOnAltered = []string{keywordTABLE, "VIEW", "SEQUENCE", keywordTYPE}

	// runOnDropped names the objects a DROP statement opens with.
	runOnDropped = []string{keywordTABLE, "VIEW", "INDEX", keywordTYPE, "FUNCTION", keywordMACRO, keywordSCHEMA,
		"SEQUENCE", "SECRET"}

	// queryWords are the words that, outside any parentheses, show a statement holds a
	// query.
	queryWords = map[string]struct{}{keywordSELECT: {}, keywordFROM: {}, keywordVALUES: {}, keywordPIVOT: {}, keywordUNPIVOT: {}}
)

// Option configures a DuckDB dialect.
//
// Options apply in the order given, and a flavour applies its own before the caller's.
// Every registering option is additive: extra types merge, a later entry replacing an
// earlier one of the same name; extra-function registrations run in order after the
// built-in catalogue; and the type, cast and promote hooks chain, the most recently added
// consulted first, so a caller's hook can override a flavour's while a hook that returns
// nil falls through to the earlier hooks and then to the built-in rules. WithName and the
// limit options set a single value, the last one winning.
type Option func(*settings)

// Dialect is the DuckDB implementation of dialect.DialectPort.
type Dialect struct {
	// functions holds the resolved function catalogue for this engine.
	functions *ast.FunctionCatalogue

	// types holds the resolved type catalogue for this engine.
	types *ast.TypeCatalogue

	// typeResolver normalises type names through the hooks, the extra types and the built-in
	// types.
	typeResolver *typeResolver

	// settings holds the dialect configuration applied to this engine.
	settings settings

	// initErr is what went wrong while New built the function catalogue: a built-in spec
	// that did not decode. ApplyDDL and AnalyseQuery return it, since New returns no error.
	initErr error

	// info describes the dialect. Info hands out copies, so the engine reads it directly.
	info dialect.Info
}

// New creates the DuckDB dialect, named duckdb and in the duckdb family.
//
// Takes options (...Option) which configure the dialect's name, parser limits, types,
// functions and hooks; see Option for how they combine.
//
// Returns *Dialect which is the configured dialect.
func New(options ...Option) *Dialect {
	configured := settings{}
	configured.Name = "duckdb"
	for _, option := range options {
		option(&configured)
	}

	types := buildTypeCatalogue(configured.ExtraTypes)
	functions, initErr := buildFunctionCatalogue(configured.ExtraFunctions, types)
	return &Dialect{
		settings:     configured,
		initErr:      initErr,
		info:         describe(configured.Name),
		types:        types,
		functions:    functions,
		typeResolver: newTypeResolver(configured.TypeNormaliserHooks, configured.ExtraTypes),
	}
}

// Info describes the DuckDB dialect: dollar-numbered parameters, the main default schema,
// RETURNING support and the DuckDB expression set, which adds lambdas and struct field
// access to the PostgreSQL-like base.
//
// Returns dialect.Info which describes the dialect, a copy the caller may change.
func (engine *Dialect) Info() dialect.Info {
	return engine.info.Clone()
}

// ParseStatements tokenises and classifies SQL statements for DuckDB.
//
// A statement holding more tokens than the per-statement budget keeps only the tokens
// read before the budget was reached, enough to classify it; ApplyDDL and AnalyseQuery
// refuse it. A panic while tokenising or classifying fails the call rather than the
// caller: it is returned as a dialectkit.PanicError, and its stack is logged once at Warn
// through the context's logger.
//
// Takes sql (string) which is the source text containing one or more statements.
//
// Returns results ([]ast.ParsedStatement) which is the ordered list of parsed statements
// with classification metadata.
// Returns error when tokenising fails, the call panics, or the context is cancelled.
func (engine *Dialect) ParseStatements(ctx context.Context, sql string) (results []ast.ParsedStatement, err error) {
	defer func() { dialectkit.Recover(ctx, "duckdb: parse statements", recover(), &results, &err) }()

	if ctxErr := context.Cause(ctx); ctxErr != nil {
		return nil, ctxErr
	}
	statements, err := engine.lexStatements(ctx, sql)
	if err != nil {
		return nil, err
	}

	results = make([]ast.ParsedStatement, 0, len(statements))
	for _, statement := range statements {
		results = append(results, ast.ParsedStatement{
			Raw: &parsedStatement{
				source:     sql,
				tokens:     statement.Tokens,
				start:      statement.Span.Start,
				end:        statement.Span.End,
				kind:       classifyStatement(statement.Tokens),
				overBudget: statement.OverBudget,
			},
			Location: statement.Span.Start,
			Length:   statement.Span.End - statement.Span.Start,
		})
	}

	return results, nil
}

// ApplyDDL applies a DDL statement to the catalogue for the DuckDB dialect.
//
// A malformed statement is reported as a returned syntax error, and so is a statement
// that runs on into the next one because the semicolon between them is missing. A panic
// while the statement is applied, such as a parser bug or a panicking hook, fails the
// statement rather than the calling apply loop: it is returned as a
// dialectkit.PanicError, and its stack is logged once at Warn through the context's
// logger. It returns the context's cause before dispatch once ctx is done, and the
// parser's walk stops with it, so a long-running catalogue build can be cancelled by the
// caller. A DDL statement over the per-statement token budget is rejected before the
// parser walks it.
//
// Takes statement (ast.ParsedStatement) which is the DDL statement to apply.
//
// Returns mutation (*ast.CatalogueMutation) which describes the mutation, or nil when the
// statement produces none.
// Returns error when the statement was parsed by another dialect, the statement is
// malformed, runs on into the next one or is over the token budget, the handler panics,
// the context is cancelled, or the built-in function catalogue did not decode.
func (engine *Dialect) ApplyDDL(
	ctx context.Context,
	statement ast.ParsedStatement,
) (mutation *ast.CatalogueMutation, err error) {
	defer func() { dialectkit.Recover(ctx, "duckdb: apply DDL", recover(), &mutation, &err) }()

	parsed, ok := statement.Raw.(*parsedStatement)
	if !ok {
		return nil, fmt.Errorf("%w: %T", dialect.ErrForeignStatement, statement.Raw)
	}
	if engine.initErr != nil {
		return nil, engine.initErr
	}

	if ctxErr := context.Cause(ctx); ctxErr != nil {
		return nil, ctxErr
	}

	if runOnError := checkRunOn(ctx, parsed); runOnError != nil {
		return nil, runOnError
	}

	if int(parsed.kind) >= len(ddlHandlers) || ddlHandlers[parsed.kind] == nil {
		return nil, nil
	}

	if budgetErr := engine.checkTokenBudget(parsed); budgetErr != nil {
		return nil, budgetErr
	}

	statementParser := engine.newStatementParser(dialectkit.WalkGuard(ctx, len(parsed.tokens)), parsed)

	mutation, err = ddlHandlers[parsed.kind](statementParser, engine)
	if syntaxErr := statementParser.syntaxError(); syntaxErr != nil {
		return nil, syntaxErr
	}
	return mutation, err
}

// AnalyseQuery analyses a DuckDB DML statement and requires all tokens to be consumed.
//
// A malformed statement returns a syntax error, and a statement exceeding the token
// budget is rejected before parsing, whether or not it is one the analyser reads. A panic
// while the statement is analysed, such as a parser bug or a panicking hook, fails the
// statement: it is returned as a dialectkit.PanicError, and its stack is logged once at
// Warn through the context's logger. It returns the context's cause once ctx is done, and
// the parser's walk stops with it. The analysis records what an INSERT does with a row
// that conflicts, from its ON CONFLICT clause or its OR REPLACE or OR IGNORE, and
// ast.ConflictActionNone for a statement with neither.
//
// Takes catalogue (*sqlmodel.Catalogue) which supplies the schema objects available to
// the query.
// Takes statement (ast.ParsedStatement) which is the DML statement to analyse.
//
// Returns analysis (*ast.RawQueryAnalysis) which describes the statement structure.
// Returns error when the statement was parsed by another dialect, the statement is
// malformed or over the token budget, the analyser panics, the context is cancelled, or
// the built-in function catalogue did not decode.
func (engine *Dialect) AnalyseQuery(
	ctx context.Context,
	catalogue *sqlmodel.Catalogue,
	statement ast.ParsedStatement,
) (analysis *ast.RawQueryAnalysis, err error) {
	defer func() { dialectkit.Recover(ctx, "duckdb: analyse query", recover(), &analysis, &err) }()

	if ctxErr := context.Cause(ctx); ctxErr != nil {
		return nil, ctxErr
	}
	parsed, ok := statement.Raw.(*parsedStatement)
	if !ok {
		return nil, fmt.Errorf("%w: %T", dialect.ErrForeignStatement, statement.Raw)
	}
	if engine.initErr != nil {
		return nil, engine.initErr
	}

	if parsed.overBudget {
		return nil, dialectkit.TokenBudgetExceeded(engine.settings.Limits.TokensPerStatement())
	}

	if int(parsed.kind) >= len(dmlAnalysers) || dmlAnalysers[parsed.kind] == nil {
		if unrecognised := unrecognisedStatement(parsed); unrecognised != nil {
			return nil, unrecognised
		}
		if unbound := unboundParameter(parsed); unbound != nil {
			return nil, unbound
		}
		return withConflictActionRecorded(&ast.RawQueryAnalysis{}), nil
	}
	analyser := dmlAnalysers[parsed.kind]

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
	if syntaxErr := statementParser.syntaxError(); syntaxErr != nil {
		return nil, syntaxErr
	}
	if err != nil {
		return nil, err
	}
	if matcherErr := rejectProjectedColumnsMatcher(analysis); matcherErr != nil {
		return nil, matcherErr
	}
	resolveStructFieldReferences(analysis, nil, catalogue)
	engine.recordStatementSpans(statementParser, analysis, parsed, statement)
	analysis.Warnings = statementParser.statement.sortedWarnings()
	return withConflictActionRecorded(analysis), nil
}

// BuiltinFunctions returns the DuckDB built-in function catalogue.
//
// Returns *ast.FunctionCatalogue which holds the builtin functions.
func (engine *Dialect) BuiltinFunctions() *ast.FunctionCatalogue {
	return engine.functions
}

// BuiltinTypes returns the DuckDB built-in type catalogue.
//
// Returns *ast.TypeCatalogue which holds the builtin types.
func (engine *Dialect) BuiltinTypes() *ast.TypeCatalogue {
	return engine.types
}

// NormaliseTypeName resolves a raw type name to a structured SQLType.
//
// Takes name (string) which is the raw type name to normalise.
// Takes modifiers (...int) which are optional type modifiers.
//
// Returns sqlmodel.SQLType which is the normalised type.
func (engine *Dialect) NormaliseTypeName(name string, modifiers ...int) sqlmodel.SQLType {
	return engine.typeResolver.normalise(name, modifiers...)
}

// TableValuedFunctionColumns returns output columns for a known table-valued function.
//
// Takes functionName (string) which is the table-valued function name.
//
// Returns []ast.ScopedColumn which is a copy of the columns for the named function, or
// nil when the name is unknown.
func (*Dialect) TableValuedFunctionColumns(functionName string) []ast.ScopedColumn {
	columns, exists := tableValuedFunctionColumns[functionName]
	if !exists {
		return nil
	}
	result := make([]ast.ScopedColumn, len(columns))
	copy(result, columns)
	return result
}

// TableValuedFunctionColumnsFromCatalogue resolves user-defined functions returning
// composite or set-of types by looking up the function signature and return type in the
// catalogue.
//
// A name qualified by a schema, such as reports.f, is looked up in that schema alone, and
// an unqualified name in each schema on the catalogue's search path in turn, so the first
// schema on the path that holds the function decides its columns. A table macro's columns
// are typed by analysing its body, whose walk stops with the context's cause once ctx is
// done. A panic while the body is analysed, such as a parser bug or a panicking hook, is
// returned as a dialectkit.PanicError, and its stack is logged once at Warn through the
// context's logger.
//
// Takes catalogue (*sqlmodel.Catalogue) which is the catalogue to search; nil holds no
// functions.
// Takes functionName (string) which is the function name to look up, optionally qualified
// by its schema.
//
// Returns columns ([]ast.ScopedColumn) which lists the resolved output columns, or nil
// when no matching set-returning function is found.
// Returns error when typing a table macro's body panics or the context is cancelled.
func (engine *Dialect) TableValuedFunctionColumnsFromCatalogue(
	ctx context.Context,
	catalogue *sqlmodel.Catalogue,
	functionName string,
) (columns []ast.ScopedColumn, err error) {
	defer func() {
		dialectkit.Recover(ctx, "duckdb: table-valued function columns", recover(), &columns, &err)
	}()

	if ctxErr := context.Cause(ctx); ctxErr != nil {
		return nil, ctxErr
	}
	schemaName, bareName := "", functionName
	if qualifier, name, qualified := strings.Cut(functionName, "."); qualified {
		schemaName, bareName = qualifier, name
	}
	for schema, signatures := range catalogue.FunctionsOnPath(schemaName, bareName) {
		for _, signature := range signatures {
			if signature == nil || !signature.ReturnsSet {
				continue
			}
			recorded := resolveCompositeColumns(catalogue, schema, signature.ReturnType)
			if recorded == nil {
				continue
			}
			columns = engine.typeTableMacroColumns(ctx, catalogue, signature, recorded)
			if ctxErr := context.Cause(ctx); ctxErr != nil {
				return nil, ctxErr
			}
			return columns, nil
		}
	}
	return nil, nil
}

// PromoteType consults promotion hooks, then widens integer or float types within the
// same category, and across numeric categories prefers a float, then a decimal. Other
// pairs keep the left type.
//
// Takes left (sqlmodel.SQLType) which is the left-hand operand type.
// Takes right (sqlmodel.SQLType) which is the right-hand operand type.
//
// Returns sqlmodel.SQLType which is the promoted type.
func (engine *Dialect) PromoteType(
	left sqlmodel.SQLType,
	right sqlmodel.SQLType,
) sqlmodel.SQLType {
	if promoted, answered := dialectkit.FirstHookAnswer(engine.settings.PromoteTypeHooks,
		func(hook func(left, right sqlmodel.SQLType) *sqlmodel.SQLType) *sqlmodel.SQLType {
			return hook(left, right)
		}); answered {
		return promoted
	}

	if across, numeric := dialectkit.PromoteAcrossNumericCategories(left, right); numeric {
		return across
	}
	if left.Category != right.Category {
		return left
	}

	switch left.Category {
	case sqlmodel.TypeCategoryInteger:
		if promotionRank(right) > promotionRank(left) {
			return right
		}
		return left
	case sqlmodel.TypeCategoryFloat:
		if right.Width > left.Width {
			return right
		}
		return left
	default:
		return left
	}
}

// CanImplicitCast reports whether DuckDB allows implicit conversion between types.
//
// A DECIMAL converts to a floating-point type, as DuckDB passes a fraction literal such
// as 0.5 to a function taking a DOUBLE, and a VARCHAR converts to JSON, as DuckDB reads a
// VARCHAR argument of a JSON function as a JSON document.
//
// Takes from (sqlmodel.SQLType) which is the source type.
// Takes to (sqlmodel.SQLType) which is the destination type.
//
// Returns bool which is true when an implicit cast is allowed.
func (engine *Dialect) CanImplicitCast(
	from sqlmodel.SQLType,
	to sqlmodel.SQLType,
) bool {
	if allowed, answered := dialectkit.FirstHookAnswer(engine.settings.ImplicitCastHooks,
		func(hook func(from, to sqlmodel.SQLType) *bool) *bool { return hook(from, to) }); answered {
		return allowed
	}

	switch {
	case from.Category == sqlmodel.TypeCategoryInteger && to.Category == sqlmodel.TypeCategoryFloat:
		return true
	case from.Category == sqlmodel.TypeCategoryInteger && to.Category == sqlmodel.TypeCategoryDecimal:
		return true
	case from.Category == sqlmodel.TypeCategoryFloat && to.Category == sqlmodel.TypeCategoryDecimal:
		return true
	case from.Category == sqlmodel.TypeCategoryDecimal && to.Category == sqlmodel.TypeCategoryFloat:
		return true
	case from.Category == sqlmodel.TypeCategoryText && to.Category == sqlmodel.TypeCategoryText:
		return true
	case from.Category == sqlmodel.TypeCategoryText && to.Category == sqlmodel.TypeCategoryJSON:
		return true
	default:
		return false
	}
}

// CoerceArgument reports DuckDB function-argument conversions from JSON to text and from
// string literals to temporal types. These conversions never return NULL; invalid
// temporal literals fail during binding.
//
// Takes argument (dialect.CallArgument) which is the value the call passes.
// Takes parameter (sqlmodel.SQLType) which is the type the parameter declares.
//
// Returns dialect.ArgumentCoercion which says the conversion never gives NULL.
// Returns bool which is true for a JSON argument to a text parameter and a string literal
// to a temporal one.
func (*Dialect) CoerceArgument(argument dialect.CallArgument, parameter sqlmodel.SQLType) (dialect.ArgumentCoercion, bool) {
	coerced := argument.Type.Category == sqlmodel.TypeCategoryJSON && parameter.Category == sqlmodel.TypeCategoryText ||
		argument.Literal && argument.Type.Category == sqlmodel.TypeCategoryText && parameter.Category == sqlmodel.TypeCategoryTemporal
	return dialect.ArgumentCoercion{MayBeNull: false}, coerced
}

// CoreTypes returns DuckDB's core types: BOOLEAN, VARCHAR, BIGINT for counts, JSON,
// INTEGER for a LIMIT or OFFSET parameter, and no implicit row identifier.
//
// Returns dialect.CoreTypes which holds the core types.
func (*Dialect) CoreTypes() dialect.CoreTypes {
	return coreTypes
}

// BinaryResult types the DuckDB operators whose result differs from the common type of
// their operands: temporal arithmetic, ||, / and the JSON arrows.
//
// Takes operator (string) which is the operator.
// Takes left (sqlmodel.SQLType) which is the left operand's type.
// Takes right (sqlmodel.SQLType) which is the right operand's type.
//
// Returns sqlmodel.SQLType which is the result's type.
// Returns sqlmodel.FunctionNullableBehaviour which says when the result is NULL.
// Returns bool which is false for an operator left to the structural default.
func (*Dialect) BinaryResult(operator string, left, right sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
	return binaryResult(operator, left, right)
}

// UnaryResult types NOT as a boolean that is NULL when its operand is, and leaves every
// other prefix operator to the structural default.
//
// Takes operator (string) which is the operator.
//
// Returns sqlmodel.SQLType which is the result's type.
// Returns sqlmodel.FunctionNullableBehaviour which says when the result is NULL.
// Returns bool which is true for NOT.
func (*Dialect) UnaryResult(operator string, _ sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
	return unaryResult(operator)
}

// ResolveFunctionCall resolves a function call using DuckDB overload rules: a polymorphic
// built-in by its argument types, and a scalar macro whose body leaves its type to its
// arguments by typing the body with the call's argument types, as DuckDB expands a macro
// at each call.
//
// The walk of each macro body parsed stops with the context's cause once ctx is done. A
// panic while the call is resolved, such as a parser bug or a panicking hook, is returned
// as a dialectkit.PanicError, and its stack is logged once at Warn through the context's
// logger.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds known functions.
// Takes name (string) which is the function name being called.
// Takes schema (string) which scopes the lookup to a schema.
// Takes argumentTypes ([]sqlmodel.SQLType) which describes call argument types in
// declared order.
//
// Returns resolution (*ast.FunctionResolution) which is the resolved overload, or nil
// when no polymorphic rule matches and no macro body types the call.
// Returns error when resolution fails, panics, or the context is cancelled.
func (engine *Dialect) ResolveFunctionCall(
	ctx context.Context,
	catalogue *sqlmodel.Catalogue,
	name string,
	schema string,
	argumentTypes []sqlmodel.SQLType,
) (resolution *ast.FunctionResolution, err error) {
	defer func() {
		dialectkit.Recover(ctx, "duckdb: resolve function call", recover(), &resolution, &err)
	}()

	if ctxErr := context.Cause(ctx); ctxErr != nil {
		return nil, ctxErr
	}
	if schema == "" {
		switch strings.ToLower(name) {
		case "coalesce", "greatest", "least":
			return engine.resolveCommonArgument(argumentTypes, sqlmodel.FunctionNullableCalledOnNull), nil
		case "mod":
			return engine.resolveCommonArgument(argumentTypes, sqlmodel.FunctionNullableReturnsNullOnNull), nil
		}
	}
	resolution, err = newFunctionResolver().ResolveFunctionCall(catalogue, name, schema, argumentTypes)
	if err != nil || resolution != nil {
		return resolution, err
	}
	resolution = engine.resolveMacroCall(ctx, catalogue, name, schema, argumentTypes)
	if ctxErr := context.Cause(ctx); ctxErr != nil {
		return nil, ctxErr
	}
	return resolution, nil
}

// newStatementParser returns a parser over a statement's tokens, configured with the
// dialect's limits and types. Its walk, and the walks of the parsers it starts for nested
// constructs, share interrupt, so the whole statement has one walk budget.
//
// Takes interrupt (func() error) which stops the walk, normally a dialectkit.WalkGuard
// over the statement.
// Takes parsed (*parsedStatement) which is the classified statement.
//
// Returns *parser which is ready to walk the statement.
func (engine *Dialect) newStatementParser(interrupt func() error, parsed *parsedStatement) *parser {
	statementParser := engine.newBodyParser(interrupt, parsed.tokens, parsed.source)
	statementParser.statement.highestNumberedPlaceholder = highestPlaceholderNumber(parsed.tokens)
	return statementParser
}

// newBodyParser returns a parser over tokens read from source, such as a macro's body,
// configured with the dialect's limits, types and functions.
//
// Takes interrupt (func() error) which stops the walk, normally a dialectkit.WalkGuard
// over the tokens.
// Takes tokens ([]token) which are the tokens to walk.
// Takes source (string) which is the text the tokens were read from.
//
// Returns *parser which is ready to walk the tokens.
func (engine *Dialect) newBodyParser(interrupt func() error, tokens []token, source string) *parser {
	statementParser := newParser(tokens)
	statementParser.Interrupt = interrupt
	statementParser.maxParseDepth = engine.settings.Limits.ParseDepth()
	statementParser.maxPivotColumns = engine.settings.pivotColumns()
	statementParser.types = engine.typeResolver
	statementParser.functions = engine.functions
	statementParser.source = source
	return statementParser
}

// checkTokenBudget reports a statement holding more tokens than the dialect's
// per-statement budget.
//
// Takes parsed (*parsedStatement) which is the statement.
//
// Returns error wrapping dialectkit.ErrTokenBudget when the statement is over budget.
func (engine *Dialect) checkTokenBudget(parsed *parsedStatement) error {
	if parsed.overBudget {
		return dialectkit.TokenBudgetExceeded(engine.settings.Limits.TokensPerStatement())
	}
	return engine.settings.Limits.CheckTokens(len(parsed.tokens))
}

// typeTableMacroColumns types the columns of a table macro whose body projects plain
// columns of the catalogue's tables, such as AS TABLE SELECT species, kg FROM landings,
// from those columns, so they are not left untyped.
//
// A column the body computes keeps the type the catalogue recorded for it. A column of a
// table the body outer-joins may be NULL.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the tables.
// Takes signature (*sqlmodel.Function) which is the macro, with its body.
// Takes columns ([]ast.ScopedColumn) which are the columns the catalogue recorded.
//
// Returns []ast.ScopedColumn which holds the columns, typed where the body allows.
func (engine *Dialect) typeTableMacroColumns(
	ctx context.Context,
	catalogue *sqlmodel.Catalogue,
	signature *sqlmodel.Function,
	columns []ast.ScopedColumn,
) []ast.ScopedColumn {
	if signature.BodySQL == "" {
		return columns
	}
	analysis := engine.analyseMacroBody(ctx, catalogue, signature.BodySQL)
	if analysis == nil || len(analysis.OutputColumns) != len(columns) {
		return columns
	}
	scope := newQueryScope(nil, analysis.FromTables, analysis.JoinClauses)
	outerJoined := false
	for _, join := range analysis.JoinClauses {
		outerJoined = outerJoined || join.Kind.Nullable != 0
	}
	for index, output := range analysis.OutputColumns {
		if output.Expression != nil || output.IsStar || output.ColumnName == "" {
			continue
		}
		reference := &ast.ColumnRefExpression{TableAlias: output.TableAlias, ColumnName: output.ColumnName, Span: ast.Span{}}
		if found := findCatalogueColumn(catalogue, reference, scope); found != nil {
			columns[index].SQLType = found.SQLType
			columns[index].Nullable = found.Nullable || outerJoined
		}
	}
	return columns
}

// analyseMacroBody analyses a table macro's body against the catalogue. The walk stops
// with the context's cause once ctx is done or the walk stops advancing, as
// dialectkit.WalkGuard describes.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the tables.
// Takes body (string) which is the body's SQL.
//
// Returns *ast.RawQueryAnalysis which is the body's analysis, or nil when the body is not
// one well-formed query.
func (engine *Dialect) analyseMacroBody(ctx context.Context, catalogue *sqlmodel.Catalogue, body string) *ast.RawQueryAnalysis {
	statements, err := engine.lexStatements(ctx, body)
	if err != nil || len(statements) != 1 || statements[0].OverBudget {
		return nil
	}
	tokens := statements[0].Tokens
	statementParser := engine.newBodyParser(dialectkit.WalkGuard(ctx, len(tokens)), tokens, body)
	statementParser.catalogue = catalogue
	_, analysis, err := statementParser.analyseNestedQuery(tokens)
	if err != nil || statementParser.syntaxError() != nil {
		return nil
	}
	return analysis
}

// settings holds configuration for a DuckDB variant. A flavour adds its own types,
// functions and hooks through the options, without forking the parser.
//
// Every option that registers something adds to what earlier options registered, so a
// flavour's own settings survive a caller's options of the same kind.
type settings struct {
	// ExtraTypes holds the extra type definitions, keyed by lower-cased name. Each
	// WithExtraTypes merges into it, a later entry replacing an earlier one of the same
	// name.
	ExtraTypes map[string]sqlmodel.SQLType

	// Name is the dialect name Info reports. It defaults to "duckdb"; a flavour sets it
	// through WithName.
	Name string

	// ExtraFunctions holds the function registrations, run in order after the built-in
	// catalogue is built.
	ExtraFunctions []func(*FunctionCatalogueBuilder)

	// TypeNormaliserHooks holds the NormaliseTypeName hooks in registration order. The most
	// recently added hook is consulted first.
	TypeNormaliserHooks []func(name string, modifiers []int) *sqlmodel.SQLType

	// ImplicitCastHooks holds the CanImplicitCast hooks in registration order. The most
	// recently added hook is consulted first.
	ImplicitCastHooks []func(from, to sqlmodel.SQLType) *bool

	// PromoteTypeHooks holds the PromoteType hooks in registration order. The most recently
	// added hook is consulted first.
	PromoteTypeHooks []func(left, right sqlmodel.SQLType) *sqlmodel.SQLType

	// Limits caps how deeply a statement may nest, through analysis, expression and
	// compound-type parsing, and how many tokens it may hold; zero values select
	// dialectkit's defaults.
	Limits dialectkit.Limits

	// MaxPivotColumns caps the output columns one PIVOT may make. Zero selects
	// maxPivotOutputColumns.
	MaxPivotColumns int
}

// pivotColumns returns the effective limit on a PIVOT's output columns, falling back to
// maxPivotOutputColumns when unset.
//
// Returns int which is the limit.
func (d *settings) pivotColumns() int {
	if d.MaxPivotColumns > 0 {
		return d.MaxPivotColumns
	}
	return maxPivotOutputColumns
}

// ddlHandler parses a DDL statement into a catalogue mutation.
type ddlHandler func(*parser, *Dialect) (*ast.CatalogueMutation, error)

// dmlAnalyser analyses one DML statement kind.
type dmlAnalyser func(*parser) (*ast.RawQueryAnalysis, error)

var (
	// dmlAnalysers dispatches each analysable statementKind to its analyser.
	dmlAnalysers = [statementKindCount]dmlAnalyser{
		statementKindSelect: (*parser).analyseSelect,
		statementKindInsert: dialectkit.ChangingRows(ast.DataChangeInsert, (*parser).analyseInsert),
		statementKindUpdate: dialectkit.ChangingRows(ast.DataChangeUpdate, (*parser).analyseUpdate),
		statementKindDelete: dialectkit.ChangingRows(ast.DataChangeDelete, (*parser).analyseDelete),
		statementKindValues: (*parser).analyseSelect,
	}
)

// WithName sets the name the dialect reports in Info, for a flavour built on this
// dialect. The family stays duckdb, so type mappings and drivers keep resolving.
//
// Takes name (string) which is the dialect name to report.
//
// Returns Option which applies the name.
func WithName(name string) Option {
	return func(configured *settings) {
		configured.Name = name
	}
}

// WithMaxParseDepth sets the maximum parser recursion depth across analysis, expression
// and compound-type nesting.
//
// Deeply nested user input is otherwise able to overflow the goroutine stack with a
// fatal, non-recoverable error. The default is high (dialectkit.DefaultMaxParseDepth) so
// realistic queries are unaffected; lower it to harden against hostile input or raise it
// for unusually nested generated queries. Exceeding it fails the statement with an error
// wrapping dialectkit.ErrParseDepth.
//
// Takes depth (int) which is the maximum nesting depth; values below 1 are ignored so the
// default remains in force, and values above dialectkit.MaxParseDepthCeiling are lowered
// to it.
//
// Returns Option which applies the depth cap to the dialect.
func WithMaxParseDepth(depth int) Option {
	return func(configured *settings) {
		configured.Limits = configured.Limits.WithMaxParseDepth(depth)
	}
}

// WithMaxTokensPerStatement sets the maximum number of tokens one statement may carry
// before the DDL and analysis parsers refuse to walk it.
//
// The budget bounds the work required by an unusually long statement. The default is high
// (dialectkit.DefaultMaxTokensPerStatement) so realistic statements, including generated
// IN lists, are unaffected; lower it to harden against hostile input or raise it for
// unusually large generated statements. Statements the engine does not parse, such as a
// large seed INSERT inside a migration, are never refused. Exceeding it fails the
// statement with an error wrapping dialectkit.ErrTokenBudget.
//
// Takes limit (int) which is the maximum token count; values below 1 are ignored so the
// default remains in force, and values above dialectkit.MaxTokensPerStatementCeiling are
// lowered to it.
//
// Returns Option which applies the token budget to the dialect.
func WithMaxTokensPerStatement(limit int) Option {
	return func(configured *settings) {
		configured.Limits = configured.Limits.WithMaxTokensPerStatement(limit)
	}
}

// WithMaxPivotColumns sets the most output columns one PIVOT may make: one for each
// combination of its IN lists' values and each aggregate.
//
// The limit bounds the columns a short PIVOT over several long IN lists would make. The
// default (maxPivotOutputColumns) is far above what a generated row type can use; lower
// it to harden against hostile input or raise it for an unusually wide pivot. Exceeding
// it fails the statement with an error wrapping seagull.ErrLimit, placed at the PIVOT.
//
// Takes limit (int) which is the most output columns; values below 1 are ignored so the
// default remains in force.
//
// Returns Option which applies the limit to the dialect.
func WithMaxPivotColumns(limit int) Option {
	return func(configured *settings) {
		if limit > 0 {
			configured.MaxPivotColumns = limit
		}
	}
}

// WithExtraTypes adds named type definitions to the DuckDB dialect.
//
// Names match without regard to case. Extra definitions override built-ins, and later
// registrations replace earlier definitions of the same name.
//
// Takes types (map[string]sqlmodel.SQLType) which maps each type name to its definition.
//
// Returns Option which adds the types.
func WithExtraTypes(types map[string]sqlmodel.SQLType) Option {
	return func(configured *settings) {
		if configured.ExtraTypes == nil {
			configured.ExtraTypes = make(map[string]sqlmodel.SQLType, len(types))
		}
		for name := range types {
			configured.ExtraTypes[strings.ToLower(name)] = types[name]
		}
	}
}

// WithExtraFunctions registers additional functions after the built-in DuckDB function
// catalogue is built. Repeated calls add registrations, which run in order.
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

// WithTypeNormaliserHook adds a NormaliseTypeName hook. Hooks are consulted most recent
// first, before the extra and built-in types; the first non-nil result wins.
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

// unboundParameter refuses a parameter in a statement Seagull runs without analysing,
// such as SET or COPY, which the generated code could not bind.
//
// Takes parsed (*parsedStatement) which is the statement.
//
// Returns error which is a dialectkit.ParseError at the first parameter, or nil when the
// statement holds none.
func unboundParameter(parsed *parsedStatement) error {
	for _, candidate := range parsed.tokens {
		if isParameterToken(candidate.Kind) {
			return dialectkit.UnboundParameterError(candidate, parsed.tokens[0].Keyword(tokenIdentifier), "")
		}
	}
	return nil
}

// unrecognisedStatement reports a statement Seagull has no analyser for whose first word
// starts no statement DuckDB runs, such as a misspelt SELECT, so the query fails rather
// than passing as a statement with nothing to analyse.
//
// A statement DuckDB runs that has nothing for the analyser to read, such as SET, PRAGMA
// or a DDL statement, is accepted.
//
// Takes parsed (*parsedStatement) which is the statement.
//
// Returns error which is a dialectkit.ParseError at the first token, or nil when the
// statement is one DuckDB runs.
func unrecognisedStatement(parsed *parsedStatement) error {
	if parsed.kind != statementKindUnknown || len(parsed.tokens) == 0 {
		return nil
	}
	first := parsed.tokens[0]
	if _, known := statementVerbs[first.Keyword(tokenIdentifier)]; known {
		return nil
	}
	return dialectkit.NewParseError(first, "expected a statement DuckDB runs, such as SELECT, INSERT, UPDATE or DELETE")
}

// rejectProjectedColumnsMatcher reports a COLUMNS(...) matcher in a select list, where it
// stands for a set of columns chosen when the query runs, which the generator cannot
// list, so a generated row would not match what the query returns.
//
// In a WHERE or other clause it expands to one condition per column and is accepted.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysed query.
//
// Returns error which is a dialectkit.ParseError at the matcher, or nil when there is
// none.
func rejectProjectedColumnsMatcher(analysis *ast.RawQueryAnalysis) error {
	call := dialectkit.FindProjectionCall(analysis, columnsMatcher)
	if call == nil {
		return nil
	}
	return &dialectkit.ParseError{
		Token:    call.FunctionName,
		Message:  "COLUMNS(...) in a select list is not supported: the generator cannot list the columns it matches; name the columns instead",
		Position: call.Span.Start,
	}
}

// resolveCompositeColumns expands a composite return type into its scoped columns by
// looking the type up in the declaring or target schema.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds all known schemas.
// Takes declaringSchema (*sqlmodel.Schema) which is the function's home schema.
// Takes returnType (sqlmodel.SQLType) which is the composite return type to expand.
//
// Returns []ast.ScopedColumn which is the field list of the matching composite type, or
// nil when no match is found.
func resolveCompositeColumns(
	catalogue *sqlmodel.Catalogue,
	declaringSchema *sqlmodel.Schema,
	returnType sqlmodel.SQLType,
) []ast.ScopedColumn {
	typeName := returnType.EngineName
	if typeName == "" {
		return nil
	}

	searchSchemas := []*sqlmodel.Schema{declaringSchema}
	if returnType.Schema != "" && returnType.Schema != declaringSchema.Name {
		if targetSchema, exists := catalogue.Schemas[returnType.Schema]; exists && targetSchema != nil {
			searchSchemas = []*sqlmodel.Schema{targetSchema}
		}
	}

	for _, schema := range searchSchemas {
		compositeType := schema.CompositeTypes[typeName]
		if compositeType == nil {
			continue
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
	return nil
}

// describe builds the description of a DuckDB dialect of a name.
//
// Takes name (string) which is the name the dialect reports.
//
// Returns dialect.Info which describes the dialect, sharing no slice or map with any
// other description.
func describe(name string) dialect.Info {
	info := dialect.Info{
		Comments:            ast.DefaultSQLCommentStyle(),
		Lexical:             lexical.Rules(),
		Name:                name,
		Family:              Family,
		SchemaSearchPath:    []string{"main"},
		RowIDAliases:        nil,
		SystemColumns:       systemColumns,
		SelectAliases:       selectAliases,
		SelectAliasOperands: selectAliasOperands,
		OperatorFeatures:    operatorFeatures,
		Identifiers:         sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingNone, Insensitive: true},
		Placeholders: dialect.Placeholders{
			Sigils: []dialect.PlaceholderSigil{
				{Prefix: "$", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered},
				{Prefix: "?", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered},
				{Prefix: ":", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNamed},
				{Prefix: "$", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNamed},
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
			ast.SQLFeatureLambda |
			ast.SQLFeatureStructFieldAccess |
			ast.SQLFeatureStructConstruction,
		Returning:              ast.DataChangeInsert | ast.DataChangeUpdate | ast.DataChangeDelete,
		AsyncMutations:         false,
		ScalarFunctionsInFrom:  false,
		UngroupedColumns:       dialect.UngroupedColumnsRejected,
		ViewsFollowTheirTables: true,
		IdentifierQuote:        dialect.IdentifierQuote{Open: `"`, Close: `"`},
		Drivers:                []string{driverDuckDB},
		ToJSON:                 "to_json",
		TextType:               "VARCHAR",
		Filters:                dialect.FilterSyntax{Operators: filterOperators, JSONPathFunctions: jsonPathFunctions, Casts: castForms, CastTypes: castTypes},
		Lists:                  listSyntax,
		Pagination:             paginationSyntax,
		MaxParameters:          maxParameters,
	}
	return info.Clone()
}

// checkRunOn detects adjacent migration statements with a missing semicolon. It skips
// macros and functions, whose bodies may hold anything, and reads every token of a
// statement over the token budget again, one at a time, so a missing semicolon past the
// budget is found too.
//
// Takes parsed (*parsedStatement) which is the statement.
//
// Returns error which locates the statement that runs on, the context's cause, or the
// error that stopped the statement being read again; nil when the statement ends where it
// should.
func checkRunOn(ctx context.Context, parsed *parsedStatement) error {
	if parsed.kind == statementKindCreateMacro || parsed.kind == statementKindCreateFunction {
		return nil
	}
	rules := runOnRules
	switch {
	case parsed.kind == statementKindAlterTable:
		rules = alterTableRunOnRules
	case holdsQuery(parsed.tokens):
		rules = queryRunOnRules
	}
	next, found, err := findRunOn(ctx, parsed, rules)
	if err != nil || !found {
		return err
	}
	return dialectkit.NewParseError(next, "a semicolon is missing before "+strings.ToUpper(next.Value)+
		": the statement runs on into the next one")
}

// findRunOn finds the word that opens the next statement where a statement runs on into
// it: among the statement's tokens, or, for a statement over the token budget, among
// every token of its span, read again one at a time in constant memory.
//
// Takes parsed (*parsedStatement) which is the statement.
// Takes rules (dialectkit.RunOnRules[tokenKind]) which say how statements open.
//
// Returns token which is the word that opens the next statement.
// Returns bool which is true when the statement runs on.
// Returns error which is the context's cause or the error that stopped the statement
// being read again.
func findRunOn(ctx context.Context, parsed *parsedStatement, rules dialectkit.RunOnRules[tokenKind]) (token, bool, error) {
	if !parsed.overBudget {
		next, found := dialectkit.RunOnStatement(parsed.source, parsed.tokens, rules)
		return next, found, nil
	}
	return dialectkit.RunOnStream(ctx, parsed.source, statementTokens(parsed.source, parsed.start, parsed.end), tokenEOF, rules)
}

// holdsQuery reports whether a statement holds a query: a SELECT, FROM, VALUES, PIVOT or
// UNPIVOT outside any parentheses.
//
// Takes tokens ([]token) which are the statement's tokens.
//
// Returns bool which is true when it does.
func holdsQuery(tokens []token) bool {
	depth := 0
	for index := range tokens {
		if tokens[index].Kind == tokenLeftParen {
			depth++
			continue
		}
		if tokens[index].Kind == tokenRightParen {
			depth--
			continue
		}
		if _, query := queryWords[tokens[index].Keyword(tokenIdentifier)]; query && depth == 0 {
			return true
		}
	}
	return false
}

// withConflictActionRecorded records that a statement whose analysis says nothing of
// conflicting rows has no conflict clause, since DuckDB writes one only on an INSERT,
// which records its own.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the statement's analysis.
//
// Returns *ast.RawQueryAnalysis which is the analysis, its conflict action recorded.
func withConflictActionRecorded(analysis *ast.RawQueryAnalysis) *ast.RawQueryAnalysis {
	if analysis.ConflictAction == ast.ConflictActionUnrecorded {
		analysis.ConflictAction = ast.ConflictActionNone
	}
	return analysis
}

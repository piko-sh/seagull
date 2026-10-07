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

package sqlite

import (
	"context"
	"fmt"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/sqlite/lexical"
	"seagull.sh/seagull/internal/sqlmodel"
)

var (
	// ddlHandlers dispatches DDL statement kinds to their parser entry points. A kind with
	// no entry produces no catalogue mutation and is never walked by the parser.
	ddlHandlers = [statementKindUnknown + 1]ddlHandler{
		statementKindCreateTable: func(p *parser, engine *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseCreateTable(engine)
		},
		statementKindDropTable: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseDropTable()
		},
		statementKindAlterTable: func(p *parser, engine *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseAlterTable(engine)
		},
		statementKindCreateView: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseCreateView()
		},
		statementKindDropView: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseDropView()
		},
		statementKindCreateIndex: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseCreateIndex()
		},
		statementKindDropIndex: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseDropIndex()
		},
		statementKindCreateVirtualTable: func(p *parser, engine *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseCreateVirtualTable(engine)
		},
		statementKindCreateTrigger: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseCreateTrigger()
		},
		statementKindDropTrigger: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseDropTrigger()
		},
	}

	// queryAnalysers dispatches DML statement kinds to their analysers. A kind with no entry
	// yields an empty analysis and is never walked by the parser.
	queryAnalysers = [statementKindUnknown + 1]queryAnalyser{
		statementKindSelect: (*parser).analyseSelect,
		statementKindInsert: dialectkit.ChangingRows(ast.DataChangeInsert, (*parser).analyseInsert),
		statementKindUpdate: dialectkit.ChangingRows(ast.DataChangeUpdate, (*parser).analyseUpdate),
		statementKindDelete: dialectkit.ChangingRows(ast.DataChangeDelete, (*parser).analyseDelete),
		statementKindValues: (*parser).analyseSelect,
		statementKindPragma: (*parser).analysePragma,
	}

	// runnableStatementWords lists the words that open a statement SQLite runs that has no
	// query for the analyser to read, such as a PRAGMA or a transaction statement. A
	// statement opening with any other word that no analyser takes is not SQLite.
	runnableStatementWords = map[string]bool{
		"ALTER": true, "ANALYZE": true, "ATTACH": true, "BEGIN": true, "COMMIT": true, keywordCREATE: true,
		"DETACH": true, keywordDROP: true, "END": true, "EXPLAIN": true, "PRAGMA": true, "REINDEX": true,
		"RELEASE": true, "ROLLBACK": true, "SAVEPOINT": true, "VACUUM": true,
	}
)

var (
	// runOnRules names the words that open a statement, for finding a migration statement
	// that runs on into the next one because the semicolon between them is missing.
	runOnRules = dialectkit.RunOnRules[tokenKind]{
		Starters: map[string][]string{
			keywordCREATE: nil,
			"ALTER":       {keywordTABLE},
			keywordDROP:   {keywordTABLE, "VIEW", "INDEX", "TRIGGER"},
		},
		Word:  tokenIdentifier,
		Open:  tokenLeftParen,
		Close: tokenRightParen,
	}

	// alterTableRunOnRules are runOnRules for an ALTER TABLE, whose DROP action names a
	// column, which may be a word such as index.
	alterTableRunOnRules = dialectkit.RunOnRules[tokenKind]{
		Starters: map[string][]string{
			keywordCREATE: nil,
			"ALTER":       {keywordTABLE},
			keywordDROP:   {keywordTABLE},
		},
		Word:  tokenIdentifier,
		Open:  tokenLeftParen,
		Close: tokenRightParen,
	}
)

// Option configures a SQLite dialect.
//
// Options apply in the order given. Every registering option is additive: extra types
// merge, a later entry replacing an earlier one of the same name; extra-function
// registrations run in order after the built-in catalogue; and the type, cast and promote
// hooks chain, the most recently added consulted first, so a later hook can override an
// earlier one while a hook that returns nil falls through to the earlier hooks and then
// to the built-in rules. WithName and the limit options set a single value, the last one
// winning.
type Option func(*settings)

// Dialect is the SQLite implementation of dialect.DialectPort.
type Dialect struct {
	// functions catalogues the SQLite built-in functions plus any extra signatures supplied
	// through the options.
	functions *ast.FunctionCatalogue

	// types catalogues the SQLite built-in storage classes and affinities plus any extra
	// definitions supplied through the options.
	types *ast.TypeCatalogue

	// typeResolver normalises type names through the hooks, the extra types and the built-in
	// types.
	typeResolver *typeResolver

	// initError is the error building the dialect met, such as a built-in function catalogue
	// that did not decode, which ApplyDDL and AnalyseQuery return; nil when the dialect was
	// built whole.
	initError error

	// settings holds the dialect configuration applied to this engine.
	settings settings
}

// New creates the SQLite dialect, named sqlite and in the sqlite family.
//
// An error building the built-in function catalogue does not stop New: the functions that
// decoded are registered, and ApplyDDL and AnalyseQuery return the error.
//
// Takes options (...Option) which configure the dialect's name, parser limits, types,
// functions and hooks; see Option for how they combine.
//
// Returns *Dialect which is the configured dialect.
func New(options ...Option) *Dialect {
	return newDialect(builtinFunctionSpec, options...)
}

// newDialect creates the SQLite dialect with the built-in function catalogue loadSpec
// gives.
//
// Takes loadSpec (func() (*dialectkit.FunctionSpec, error)) which decodes the built-in
// function catalogue.
// Takes options (...Option) which configure the dialect.
//
// Returns *Dialect which is the configured dialect, holding any error loadSpec gave.
func newDialect(loadSpec func() (*dialectkit.FunctionSpec, error), options ...Option) *Dialect {
	configured := settings{}
	configured.Name = "sqlite"
	for _, option := range options {
		option(&configured)
	}

	types := buildTypeCatalogue(configured.ExtraTypes)
	functions, functionsError := buildFunctionCatalogue(loadSpec, configured.ExtraFunctions, types)
	return &Dialect{
		settings:     configured,
		types:        types,
		functions:    functions,
		typeResolver: newTypeResolver(configured.TypeNormaliserHooks, configured.ExtraTypes),
		initError:    functionsError,
	}
}

// Info describes the SQLite dialect: question-mark parameters with the :name, @name and
// $name forms, the main and temp schemas on the search path, identifiers matched
// regardless of case, the rowid aliases, RETURNING support and the SQLite expression set.
//
// Returns dialect.Info which describes the dialect and shares no slice or map with it or
// with any other Info it returns.
func (engine *Dialect) Info() dialect.Info {
	info := dialect.Info{
		Comments:            ast.DefaultSQLCommentStyle(),
		Lexical:             lexical.Rules(),
		Name:                engine.settings.Name,
		Family:              Family,
		SchemaSearchPath:    schemaSearchPath,
		RowIDAliases:        rowIDAliases,
		SystemColumns:       nil,
		SelectAliases:       selectAliases,
		SelectAliasOperands: selectAliases,
		OperatorFeatures:    operatorFeatures,
		Identifiers:         sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingNone, Insensitive: true},
		Placeholders:        placeholders,
		Expressions: ast.SQLFeaturesBase |
			ast.SQLFeatureWindowFunction |
			ast.SQLFeatureJSONOp |
			ast.SQLFeatureScalarSubquery |
			ast.SQLFeatureBitwiseOp,
		Returning:              ast.DataChangeInsert | ast.DataChangeUpdate | ast.DataChangeDelete,
		AsyncMutations:         false,
		ScalarFunctionsInFrom:  false,
		UngroupedColumns:       dialect.UngroupedColumnsNullable,
		ViewsFollowTheirTables: true,
		IdentifierQuote:        dialect.IdentifierQuote{Open: `"`, Close: `"`},
		Drivers:                drivers,
		ToJSON:                 "",
		TextType:               "text",
		Filters:                dialect.FilterSyntax{Operators: nil, JSONPathFunctions: jsonPathFunctions, Casts: castForms, CastTypes: castTypes},
		Lists:                  listSyntax,
		Pagination:             paginationSyntax,
		MaxParameters:          maxParameters,
	}
	return info.Clone()
}

// ParseStatements tokenises and classifies SQL statements for the SQLite dialect.
//
// The context is checked before each statement is tokenised. A statement holding more
// tokens than the dialect's token budget keeps only the tokens within the budget, enough
// to classify it, so one huge statement costs no more than the budget; ApplyDDL and
// AnalyseQuery refuse it where they would walk it. A panic while the statements are split
// or classified is recovered and returned as a dialectkit.PanicError, and its stack is
// logged once at Warn through the context's logger.
//
// Takes sql (string) which is the raw SQL text potentially containing multiple
// statements.
//
// Returns results ([]ast.ParsedStatement) which is one entry per statement found.
// Returns err (error) when tokenising the input fails, splitting or classifying panics,
// or the context is cancelled, when it is the context's cause.
func (engine *Dialect) ParseStatements(ctx context.Context, sql string) (results []ast.ParsedStatement, err error) {
	defer func() { dialectkit.Recover(ctx, "sqlite: parse statements", recover(), &results, &err) }()

	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	statements, err := lexStatements(ctx, sql, engine.settings.Limits.TokensPerStatement())
	if err != nil {
		return nil, err
	}

	results = make([]ast.ParsedStatement, 0, len(statements))
	for _, statement := range statements {
		results = append(results, ast.ParsedStatement{
			Raw: &parsedStatement{
				source:     sql,
				tokens:     statement.Tokens,
				kind:       classifyStatement(statement.Tokens),
				overBudget: statement.OverBudget,
				span:       statement.Span,
			},
			Location: statement.Span.Start,
			Length:   statement.Span.End - statement.Span.Start,
		})
	}

	return results, nil
}

// ApplyDDL applies a DDL statement to the catalogue for the SQLite dialect.
//
// A malformed statement, including one with tokens left after the statement's grammar
// ends, is reported as a returned syntax error wrapping the generator's parse sentinel;
// only a trigger's body is left unread. A panic while the statement is parsed, from a
// parser bug or a type hook, fails the statement rather than crashing the calling apply
// loop: it is returned as a dialectkit.PanicError, and its stack is logged once at Warn
// through the context's logger. It honours the context before dispatch so the catalogue
// build loop can be cancelled by the caller, rejects a statement over the token budget
// before the parser walks it, and stops the walk with the context's cause once ctx is
// done, or with dialectkit.ErrWalkBudget once the parser stops advancing.
//
// Takes statement (ast.ParsedStatement) which is the parsed DDL statement to apply.
//
// Returns mutation (*ast.CatalogueMutation) which describes the catalogue change, or nil
// when the statement produces none.
// Returns err (error) when the statement is malformed or was parsed by another dialect,
// exceeds the token budget or the walk budget, the parser panics, the dialect was not
// built whole, or the context is cancelled, when it is the context's cause.
func (engine *Dialect) ApplyDDL(ctx context.Context, statement ast.ParsedStatement) (mutation *ast.CatalogueMutation, err error) {
	defer func() { dialectkit.Recover(ctx, "sqlite: apply DDL", recover(), &mutation, &err) }()

	parsed, ok := statement.Raw.(*parsedStatement)
	if !ok {
		return nil, fmt.Errorf("%w: %T", dialect.ErrForeignStatement, statement.Raw)
	}

	if cause := context.Cause(ctx); cause != nil {
		return nil, cause
	}
	if engine.initError != nil {
		return nil, engine.initError
	}
	if runOnError := checkRunOn(ctx, parsed); runOnError != nil {
		return nil, runOnError
	}

	if int(parsed.kind) >= len(ddlHandlers) || ddlHandlers[parsed.kind] == nil {
		return nil, nil
	}
	if budgetError := engine.settings.checkTokenBudget(parsed); budgetError != nil {
		return nil, budgetError
	}

	p := engine.newStatementParser(dialectkit.WalkGuard(ctx, len(parsed.tokens)), parsed.tokens, nil)

	mutation, err = ddlHandlers[parsed.kind](p, engine)
	if err == nil && parsed.kind != statementKindCreateTrigger {
		err = p.requireEnd()
	}
	if p.syntaxError != nil {
		return nil, p.syntaxError
	}
	if err != nil {
		return nil, err
	}
	return mutation, nil
}

// AnalyseQuery performs structural analysis of a DML statement for the SQLite dialect.
//
// The analysis must consume the whole statement: tokens left after a complete statement
// fail it with a dialectkit.ParseError naming the first of them, so nothing is silently
// dropped. A malformed statement, including one nested past the depth cap, is reported as
// a returned error wrapping the generator's parse or limit sentinel. A panic in the
// analyser, from a parser bug or a type hook, is returned as a dialectkit.PanicError, and
// its stack is logged once at Warn through the context's logger. A statement over the
// token budget is rejected before the parser walks it, even one the analyser would leave
// unanalysed, and the walk stops with the context's cause once ctx is done, or with
// dialectkit.ErrWalkBudget once the parser stops advancing. The analysis of a statement
// the dialect reads records its conflict action, which is ast.ConflictActionNone for any
// statement but an INSERT that handles conflicting rows.
//
// Takes catalogue (*sqlmodel.Catalogue) which is the schema the query runs against; it
// tells the analyser which names are FTS5 tables, whose table-named column and
// table-valued form take a full-text query.
// Takes statement (ast.ParsedStatement) which is the parsed DML statement to analyse.
//
// Returns analysis (*ast.RawQueryAnalysis) which holds the analysed query structure.
// Returns err (error) when the statement is malformed or was parsed by another dialect,
// exceeds the token budget or the walk budget, the parser panics, the dialect was not
// built whole, or the context is cancelled, when it is the context's cause.
func (engine *Dialect) AnalyseQuery(
	ctx context.Context,
	catalogue *sqlmodel.Catalogue,
	statement ast.ParsedStatement,
) (analysis *ast.RawQueryAnalysis, err error) {
	defer func() { dialectkit.Recover(ctx, "sqlite: analyse query", recover(), &analysis, &err) }()

	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	parsed, ok := statement.Raw.(*parsedStatement)
	if !ok {
		return nil, fmt.Errorf("%w: %T", dialect.ErrForeignStatement, statement.Raw)
	}
	if engine.initError != nil {
		return nil, engine.initError
	}

	if int(parsed.kind) >= len(queryAnalysers) || queryAnalysers[parsed.kind] == nil {
		if parsed.overBudget {
			return nil, engine.settings.checkTokenBudget(parsed)
		}
		return unanalysedStatement(parsed.tokens)
	}
	if budgetError := engine.settings.checkTokenBudget(parsed); budgetError != nil {
		return nil, budgetError
	}

	p := engine.newStatementParser(dialectkit.WalkGuard(ctx, len(parsed.tokens)), parsed.tokens, catalogue)
	p.spans = dialectkit.NewSpanRecorder(statement.Location)

	analysis, err = queryAnalysers[parsed.kind](p)
	if err == nil {
		err = p.requireEnd()
	}
	if p.syntaxError != nil {
		return nil, p.syntaxError
	}
	if err != nil {
		return nil, err
	}
	engine.recordStatementSpans(p, analysis, parsed, statement)
	analysis.Warnings = p.sortedWarnings()
	if analysis.ConflictAction == ast.ConflictActionUnrecorded {
		analysis.ConflictAction = ast.ConflictActionNone
	}
	return analysis, nil
}

// BuiltinFunctions returns the SQLite built-in function catalogue.
//
// Returns *ast.FunctionCatalogue which is the engine's function catalogue.
func (engine *Dialect) BuiltinFunctions() *ast.FunctionCatalogue {
	return engine.functions
}

// BuiltinTypes returns the SQLite built-in type catalogue.
//
// Returns *ast.TypeCatalogue which is the engine's type catalogue.
func (engine *Dialect) BuiltinTypes() *ast.TypeCatalogue {
	return engine.types
}

// NormaliseTypeName resolves a raw type name to a structured SQLType.
//
// When no modifiers are passed, a list of one or two numbers, each with an optional sign,
// written at the end of the name, as in decimal(10, 2) or varchar(255), is read as the
// type's modifiers, so the name resolves as a column declared with it does.
//
// Takes name (string) which is the raw type name as it appears in DDL.
// Takes modifiers (...int) which are optional precision or length modifiers.
//
// Returns sqlmodel.SQLType which is the normalised type using SQLite affinity rules.
func (engine *Dialect) NormaliseTypeName(name string, modifiers ...int) sqlmodel.SQLType {
	if len(modifiers) == 0 {
		if base, written, found := splitTypeModifiers(name); found {
			return engine.typeResolver.normalise(base, written...)
		}
	}
	return engine.typeResolver.normalise(name, modifiers...)
}

// TableValuedFunctionColumns returns the output column schema for a known table-valued
// function.
//
// Built-in table-valued functions, such as json_each and the pragma functions, the
// built-in schema tables the analyser reads as table-valued, such as sqlite_schema, and
// the result of a PRAGMA statement the dialect declares are resolved here. SQLite has no
// SQL-level CREATE FUNCTION, so the only table-valued sources a migration creates are
// FTS5 tables, which TableValuedFunctionColumnsFromCatalogue resolves.
//
// Takes functionName (string) which is the function name as it appears in the SQL source.
//
// Returns []ast.ScopedColumn which is the output schema, or nil when the function is not
// recognised.
func (*Dialect) TableValuedFunctionColumns(functionName string) []ast.ScopedColumn {
	lowered := strings.ToLower(functionName)
	columns, exists := tableValuedFunctionColumns[lowered]
	if !exists {
		columns, exists = pragmaResults[lowered]
	}
	if !exists {
		return nil
	}
	result := make([]ast.ScopedColumn, len(columns))
	copy(result, columns)
	return result
}

// TableValuedFunctionColumnsFromCatalogue resolves the table-valued form of an FTS5
// table, `docs('query')`, which returns the table's matching rows.
//
// A panic during the lookup is recovered and returned as a dialectkit.PanicError, and its
// stack is logged once at Warn through the context's logger.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the FTS5 tables migrations created.
// Takes functionName (string) which is the table-valued function name.
//
// Returns columns ([]ast.ScopedColumn) which holds the table's declared columns, or nil
// when functionName is not an FTS5 table.
// Returns err (error) when the context is cancelled, when it is the context's cause, or
// the lookup panics.
func (*Dialect) TableValuedFunctionColumnsFromCatalogue(
	ctx context.Context,
	catalogue *sqlmodel.Catalogue,
	functionName string,
) (columns []ast.ScopedColumn, err error) {
	defer func() {
		dialectkit.Recover(ctx, "sqlite: table-valued function columns", recover(), &columns, &err)
	}()

	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	return fullTextTableColumns(catalogue, functionName), nil
}

// PromoteType consults the promote hooks, most recent first, and returns their first
// result. Without one, it prefers a REAL, then a NUMERIC, across numeric categories, and
// otherwise keeps the left operand's type unchanged.
//
// Takes left (sqlmodel.SQLType) which is the left operand's type.
// Takes right (sqlmodel.SQLType) which is the right operand's type.
//
// Returns sqlmodel.SQLType which is a hook's result, the winning number or the left
// operand's type.
func (engine *Dialect) PromoteType(left sqlmodel.SQLType, right sqlmodel.SQLType) sqlmodel.SQLType {
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
	return left
}

// CanImplicitCast reports whether SQLite allows implicit conversion between the given
// type categories.
//
// The cast hooks are consulted first, most recent first. Otherwise integers widen to
// decimals and reals, decimals to reals, and JSON and a UUID pass as text, because SQLite
// stores a JSON value and a UUID column's value as TEXT and its functions take them as
// such.
//
// Takes from (sqlmodel.SQLType) which is the source type.
// Takes to (sqlmodel.SQLType) which is the destination type.
//
// Returns bool which is true when implicit conversion is permitted.
func (engine *Dialect) CanImplicitCast(from sqlmodel.SQLType, to sqlmodel.SQLType) bool {
	allowed, answered := dialectkit.FirstHookAnswer(engine.settings.ImplicitCastHooks,
		func(hook func(from, to sqlmodel.SQLType) *bool) *bool { return hook(from, to) })
	if answered {
		return allowed
	}

	switch {
	case from.Category == sqlmodel.TypeCategoryInteger && to.Category == sqlmodel.TypeCategoryDecimal:
		return true
	case from.Category == sqlmodel.TypeCategoryInteger && to.Category == sqlmodel.TypeCategoryFloat:
		return true
	case from.Category == sqlmodel.TypeCategoryDecimal && to.Category == sqlmodel.TypeCategoryFloat:
		return true
	case from.Category == sqlmodel.TypeCategoryText && to.Category == sqlmodel.TypeCategoryText:
		return true
	case from.Category == sqlmodel.TypeCategoryJSON && to.Category == sqlmodel.TypeCategoryText:
		return true
	case from.Category == sqlmodel.TypeCategoryUUID && to.Category == sqlmodel.TypeCategoryText:
		return true
	default:
		return false
	}
}

// CoerceArgument reports SQLite function-argument conversion from blobs to text,
// supporting JSONB or JSON text stored as bytes. This conversion never returns NULL;
// CanImplicitCast leaves blob and text values distinct elsewhere.
//
// Takes argument (dialect.CallArgument) which is the value the call passes.
// Takes parameter (sqlmodel.SQLType) which is the type the parameter declares.
//
// Returns dialect.ArgumentCoercion which says the conversion never gives NULL.
// Returns bool which is true for a blob argument to a text parameter.
func (*Dialect) CoerceArgument(argument dialect.CallArgument, parameter sqlmodel.SQLType) (dialect.ArgumentCoercion, bool) {
	coerced := argument.Type.Category == sqlmodel.TypeCategoryBinary && parameter.Category == sqlmodel.TypeCategoryText
	return dialect.ArgumentCoercion{MayBeNull: false}, coerced
}

// CoreTypes returns SQLite's core types: BOOLEAN, TEXT, BIGINT for counts and the rowid,
// JSON, and INTEGER for a LIMIT or OFFSET parameter.
//
// Returns dialect.CoreTypes which holds the core types.
func (engine *Dialect) CoreTypes() dialect.CoreTypes {
	return engine.coreTypes()
}

// BinaryResult types the SQLite operators whose result differs from the common type of
// their operands: ||, the bit operators and the JSON arrows.
//
// Takes operator (string) which is the operator.
// Takes left (sqlmodel.SQLType) which is the left operand's type.
//
// Returns sqlmodel.SQLType which is the result's type.
// Returns sqlmodel.FunctionNullableBehaviour which says when the result is NULL.
// Returns bool which is false for an operator left to the structural default.
func (engine *Dialect) BinaryResult(operator string, left, _ sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
	return engine.binaryResult(operator, left)
}

// UnaryResult types NOT and ~.
//
// Takes operator (string) which is the operator.
//
// Returns sqlmodel.SQLType which is the result's type.
// Returns sqlmodel.FunctionNullableBehaviour which says when the result is NULL.
// Returns bool which is false for an operator left to the structural default.
func (engine *Dialect) UnaryResult(operator string, _ sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
	return engine.unaryResult(operator)
}

// newStatementParser returns a parser over a statement's tokens, configured with the
// dialect's limits, type resolver and function catalogue. Its walk, and the walks of the
// parsers it starts for nested statements, share the interrupt, so the whole statement
// stops when it does.
//
// Takes interrupt (func() error) which stops the walk, as dialectkit.WalkGuard builds it
// for the statement.
// Takes tokens ([]token) which is the statement's token stream.
// Takes catalogue (*sqlmodel.Catalogue) which is the schema a query runs against, or nil
// for DDL.
//
// Returns *parser which is ready to walk the statement.
func (engine *Dialect) newStatementParser(interrupt func() error, tokens []token, catalogue *sqlmodel.Catalogue) *parser {
	p := newParser(tokens)
	p.Interrupt = interrupt
	p.maxParseDepth = engine.settings.Limits.ParseDepth()
	p.types = engine.typeResolver
	p.functions = engine.functions
	p.fullTextTables = fullTextTableNames(catalogue)
	if catalogue != nil {
		p.columnNames = newColumnNames(catalogue, tokens)
	}
	return p
}

// settings holds the configuration of a SQLite dialect: its name, its extra types and
// functions, the type-system hooks and the parser limits.
//
// Every option that registers something adds to what earlier options registered, so a
// flavour's own settings survive a caller's options of the same kind.
type settings struct {
	// ExtraTypes holds the extra type definitions, keyed by lower-cased name. Each
	// WithExtraTypes merges into it, a later entry replacing an earlier one of the same
	// name.
	ExtraTypes map[string]sqlmodel.SQLType

	// Name is the dialect name Info reports; New sets it to sqlite.
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

	// Limits caps how deeply a statement may nest and how many tokens it may hold; a field
	// left at zero selects the dialectkit default.
	Limits dialectkit.Limits
}

// checkTokenBudget rejects a statement holding more tokens than the dialect's budget,
// including one whose tokenising stopped at a budget, whose full count is not known.
//
// Takes parsed (*parsedStatement) which is the statement.
//
// Returns error wrapping dialectkit.ErrTokenBudget when the statement is over budget.
func (d settings) checkTokenBudget(parsed *parsedStatement) error {
	if parsed.overBudget {
		return dialectkit.TokenBudgetExceeded(min(len(parsed.tokens), d.Limits.TokensPerStatement()))
	}
	return d.Limits.CheckTokens(len(parsed.tokens))
}

// ddlHandler is a function that parses a DDL statement into a catalogue mutation.
type ddlHandler func(*parser, *Dialect) (*ast.CatalogueMutation, error)

// queryAnalyser is a function that analyses a DML statement.
type queryAnalyser func(*parser) (*ast.RawQueryAnalysis, error)

// WithName sets the name the dialect reports in Info, for a flavour built on this
// dialect. The family stays sqlite, so type mappings and drivers keep resolving.
//
// Takes name (string) which is the dialect name to report.
//
// Returns Option which applies the name.
func WithName(name string) Option {
	return func(configured *settings) {
		configured.Name = name
	}
}

// WithExtraTypes adds named type definitions to the SQLite dialect.
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

// WithExtraFunctions registers additional functions after the built-in SQLite function
// catalogue is built, such as the application-defined functions a program registers with
// its SQLite driver. Repeated calls add registrations, which run in order.
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

// WithMaxParseDepth sets the maximum parser recursion depth for analysis and expression
// nesting.
//
// Deeply nested user input is otherwise able to overflow the goroutine stack with a
// fatal, non-recoverable error. The default is high (dialectkit.DefaultMaxParseDepth) so
// realistic queries are unaffected; lower it to harden against hostile input or raise it
// for unusually nested generated queries. Exceeding it fails the statement with an error
// wrapping dialectkit.ErrParseDepth.
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

// WithMaxTokensPerStatement sets the maximum number of tokens a single statement may
// contain before the parser refuses to walk it.
//
// Some nested shapes cost more than linear time, so the token budget bounds the work
// required by hostile or runaway input, and ParseStatements keeps no more than the
// budget's tokens of any one statement. The default is high
// (dialectkit.DefaultMaxTokensPerStatement) so realistic queries, including generated
// ones with large IN lists, are unaffected; lower it to harden against hostile input or
// raise it for unusually large generated statements. Exceeding it fails the statement
// with an error wrapping dialectkit.ErrTokenBudget.
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

// checkRunOn detects adjacent migration statements with a missing semicolon.
//
// CREATE TRIGGER bodies and unclassified statements are skipped. Statements over the
// token budget are scanned again to the end, one token at a time in constant memory. This
// catches missing semicolons even beyond the budget in large INSERT seeds.
//
// Takes parsed (*parsedStatement) which is the statement.
//
// Returns error which locates the statement that runs on, the context's cause, or the
// tokeniser's error for a part of the statement read past the budget; nil otherwise.
func checkRunOn(ctx context.Context, parsed *parsedStatement) error {
	if parsed.kind == statementKindCreateTrigger || parsed.kind == statementKindUnknown {
		return nil
	}
	rules := runOnRules
	if parsed.kind == statementKindAlterTable {
		rules = alterTableRunOnRules
	}
	next, found, err := findRunOn(ctx, parsed, rules)
	if err != nil || !found {
		return err
	}
	return dialectkit.NewParseError(next, "a semicolon is missing before "+strings.ToUpper(next.Value)+
		": the statement runs on into the next one")
}

// findRunOn finds where a statement runs on into the next one.
//
// It uses the tokens the statement holds when it is within the token budget. When the
// statement is over the budget and holds only its first tokens, it reads the statement
// again from start to end, a token at a time.
//
// Takes parsed (*parsedStatement) which is the statement.
// Takes rules (dialectkit.RunOnRules[tokenKind]) which say how statements begin.
//
// Returns token which is the word that opens the next statement.
// Returns bool which is true when the statement runs on.
// Returns error which is the context's cause or the tokeniser's error.
func findRunOn(ctx context.Context, parsed *parsedStatement, rules dialectkit.RunOnRules[tokenKind]) (token, bool, error) {
	if !parsed.overBudget {
		next, found := dialectkit.RunOnStatement(parsed.source, parsed.tokens, rules)
		return next, found, nil
	}
	read := statementTokens(parsed.source, parsed.span.Start, parsed.span.End)
	return dialectkit.RunOnStream(ctx, parsed.source, read, tokenEOF, rules)
}

// unanalysedStatement returns an empty analysis for runnable SQLite statements that need
// no query analysis. It rejects unknown statement-opening words and parameters the
// generated code could not bind.
//
// Takes tokens ([]token) which are the statement's tokens.
//
// Returns *ast.RawQueryAnalysis which is empty for a runnable statement.
// Returns error which is a dialectkit.ParseError at the first token, or at the parameter,
// otherwise.
func unanalysedStatement(tokens []token) (*ast.RawQueryAnalysis, error) {
	if len(tokens) == 0 {
		return &ast.RawQueryAnalysis{}, nil
	}
	verb := tokens[0].Keyword(tokenIdentifier)
	if !runnableStatementWords[verb] {
		return nil, dialectkit.NewParseError(tokens[0], "expected a statement SQLite runs")
	}
	for _, candidate := range tokens {
		if isParameterToken(candidate.Kind) {
			return nil, dialectkit.UnboundParameterError(candidate, verb, "")
		}
	}
	return &ast.RawQueryAnalysis{}, nil
}

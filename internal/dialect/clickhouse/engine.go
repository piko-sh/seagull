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

package clickhouse

import (
	"context"
	"fmt"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/clickhouse/lexical"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

// columnsMatcher is the name of the COLUMNS(...) column matcher.
const columnsMatcher = "columns"

var (
	// ddlParserDispatch maps each statementKind to the parser method that produces its
	// catalogue mutation, so ApplyDDL dispatches every kind with one lookup.
	//
	// Every kind has an entry. The non-DDL kinds (Select / Insert) and the Unknown sentinel
	// map to nil, which ApplyDDL treats as a statement that changes no catalogue, returning
	// no mutation without walking it.
	ddlParserDispatch = map[statementKind]func(*parser) (*ast.CatalogueMutation, error){
		statementKindCreateTable:            (*parser).parseCreateTable,
		statementKindDropTable:              (*parser).parseDropTable,
		statementKindAlterTable:             (*parser).parseAlterTable,
		statementKindCreateView:             (*parser).parseCreateView,
		statementKindCreateMaterializedView: (*parser).parseCreateMaterializedView,
		statementKindCreateDictionary:       (*parser).parseCreateDictionary,
		statementKindCreateFunction:         (*parser).parseCreateFunction,
		statementKindCreateDatabase:         (*parser).parseCreateDatabase,
		statementKindDropView:               (*parser).parseDropView,
		statementKindDropDictionary:         (*parser).parseDropDictionary,
		statementKindDropFunction:           (*parser).parseDropFunction,
		statementKindDropDatabase:           (*parser).parseDropDatabase,
		statementKindRenameTable:            (*parser).parseRenameTable,
		statementKindExchangeTables:         (*parser).parseExchangeTables,
		statementKindTruncate:               (*parser).parseTruncateTable,
		statementKindOptimize:               (*parser).parseOptimize,
		statementKindSystem:                 (*parser).parseSystem,
		statementKindUse:                    (*parser).parseUseDatabase,
		statementKindShow:                   (*parser).parseShow,
		statementKindSet:                    (*parser).parseSet,
		statementKindExplain:                (*parser).parseExplain,
		statementKindDescribeTable:          (*parser).parseDescribeTable,
		statementKindCheckTable:             (*parser).parseCheckTable,
		statementKindBackup:                 (*parser).parseBackup,
		statementKindRestore:                (*parser).parseRestore,
		statementKindKillQuery:              (*parser).parseKillQuery,
		statementKindKillMutation:           (*parser).parseKillMutation,
		statementKindAttachTable:            (*parser).parseAttachTable,
		statementKindDetachTable:            (*parser).parseDetachTable,
		statementKindCreateUser:             (*parser).parseCreateUser,
		statementKindAlterUser:              (*parser).parseAlterUser,
		statementKindDropUser:               (*parser).parseDropUser,
		statementKindCreateRole:             (*parser).parseCreateRole,
		statementKindAlterRole:              (*parser).parseAlterRole,
		statementKindDropRole:               (*parser).parseDropRole,
		statementKindCreatePolicy:           (*parser).parseCreatePolicy,
		statementKindAlterPolicy:            (*parser).parseAlterPolicy,
		statementKindDropPolicy:             (*parser).parseDropPolicy,
		statementKindCreateQuota:            (*parser).parseCreateQuota,
		statementKindAlterQuota:             (*parser).parseAlterQuota,
		statementKindDropQuota:              (*parser).parseDropQuota,
		statementKindCreateSettingsProfile:  (*parser).parseCreateSettingsProfile,
		statementKindAlterSettingsProfile:   (*parser).parseAlterSettingsProfile,
		statementKindDropSettingsProfile:    (*parser).parseDropSettingsProfile,
		statementKindGrant:                  (*parser).parseGrant,
		statementKindRevoke:                 (*parser).parseRevoke,
		statementKindSelect:                 nil,
		statementKindInsert:                 nil,
		statementKindDelete:                 nil,
		statementKindUpdate:                 nil,
		statementKindUnknown:                nil,
	}

	// statementAnalysers maps each statement kind AnalyseQuery walks to the parser method
	// that analyses it, indexed by kind. Kinds with no entry produce an empty analysis
	// without the token stream being walked.
	statementAnalysers = [statementKindUnknown + 1]func(*parser) (*ast.RawQueryAnalysis, error){
		statementKindSelect:     (*parser).analyseSelect,
		statementKindInsert:     dialectkit.ChangingRows(ast.DataChangeInsert, (*parser).analyseInsert),
		statementKindDelete:     dialectkit.ChangingRows(ast.DataChangeDelete, (*parser).analyseDelete),
		statementKindUpdate:     dialectkit.ChangingRows(ast.DataChangeUpdate, (*parser).analyseUpdate),
		statementKindAlterTable: (*parser).analyseAlterTable,
	}

	// clickHouseNumericRanks assigns a synthetic widening rank to every numeric ClickHouse
	// type the engine recognises.
	//
	// Larger ranks denote wider or more general types within their category; the PromoteType
	// LCT helper compares ranks to choose the dominant operand when both sides share a
	// category. Types absent from this table fall back to rank 0 and lose to a recognised
	// type with a higher rank; ties preserve the left operand.
	clickHouseNumericRanks = map[string]int{
		"UInt8":      1,
		"Int8":       1,
		"UInt16":     2,
		"Int16":      2,
		"UInt32":     3,
		"Int32":      3,
		"UInt64":     4,
		"Int64":      4,
		"UInt128":    5,
		"Int128":     5,
		"UInt256":    6,
		"Int256":     6,
		"BFloat16":   7,
		"Float32":    8,
		"Float64":    9,
		"Decimal32":  10,
		"Decimal64":  11,
		"Decimal128": 12,
		"Decimal256": 13,
	}

	// runOnRules names the words that open a statement, for finding a migration statement
	// that runs on into the next one because the semicolon between them is missing.
	runOnRules = dialectkit.RunOnRules[tokenKind]{
		Starters: map[string][]string{
			"CREATE": nil,
			"ALTER":  {"TABLE"},
			"DROP":   {"TABLE", "VIEW", "DICTIONARY", "DATABASE", "FUNCTION"},
			"RENAME": {"TABLE", "DICTIONARY", "DATABASE"},
		},
		Word:  tokenIdentifier,
		Open:  tokenLeftParen,
		Close: tokenRightParen,
	}

	// alterTableRunOnRules are runOnRules for an ALTER TABLE, whose DROP actions name a
	// column, index or projection, whose name may be a word such as view.
	alterTableRunOnRules = dialectkit.RunOnRules[tokenKind]{
		Starters: map[string][]string{
			"CREATE": nil,
			"ALTER":  {"TABLE"},
			"DROP":   {"TABLE"},
			"RENAME": {"TABLE"},
		},
		Word:  tokenIdentifier,
		Open:  tokenLeftParen,
		Close: tokenRightParen,
	}
)

// Option configures a ClickHouse dialect.
//
// Options apply in the order given. Every registering option is additive: extra types
// merge, a later entry replacing an earlier one of the same name; extra-function
// registrations run in order after the built-in catalogue; and the type, cast and promote
// hooks chain, the most recently added consulted first, so a later hook can override an
// earlier one while a hook that returns nil falls through to the earlier hooks and then
// to the built-in rules. WithName, WithJoinUseNulls, WithGroupByUseNulls and the limit
// options set a single value, the last one winning.
//
// Hooks are host code the dialect calls without a guard of its own: a hook that panics
// inside ApplyDDL or AnalyseQuery fails that statement with a dialectkit.PanicError, and
// elsewhere the panic goes on to the guard where Seagull called the dialect.
type Option func(*settings)

// Dialect is the ClickHouse implementation of dialect.DialectPort.
//
// It composes the tokeniser, classifier, DDL parser, DML parser, builtin types, and
// builtin functions into a single dialect that the analyser drives.
type Dialect struct {
	// functions is the ClickHouse built-in function catalogue plus any extra signatures
	// supplied through the options.
	functions *ast.FunctionCatalogue

	// types is the ClickHouse built-in type catalogue plus any extra types supplied through
	// the options.
	types *ast.TypeCatalogue

	// typeResolver resolves type names through the hooks, the extra types and the built-in
	// grammar.
	typeResolver *typeResolver

	// functionResolver types the calls the static catalogue cannot, with the dialect's
	// catalogue, types and limits.
	functionResolver *functionResolver

	// initErr is what went wrong while New built the function catalogue: a line of the
	// built-in spec or the alias table that did not decode, or a WithExtraFunctions
	// registration that panicked. ApplyDDL and AnalyseQuery return it, since New returns no
	// error.
	initErr error

	// settings holds the resolved per-variant configuration and hooks.
	settings settings
}

// New creates the ClickHouse dialect, named clickhouse and in the clickhouse family.
//
// A WithExtraFunctions registration that panics cannot crash New: the panic is recovered,
// its stack logged once at Warn through the default logger, and every ApplyDDL and
// AnalyseQuery then returns it as a dialectkit.PanicError, as they do an error decoding
// the built-in function catalogue.
//
// Takes options (...Option) which configure the dialect's name, parser limits, types,
// functions, hooks and assumed join_use_nulls setting; see Option for how they combine.
//
// Returns *Dialect which is the configured dialect.
func New(options ...Option) *Dialect {
	configured := settings{}
	configured.Name = "clickhouse"
	for _, option := range options {
		option(&configured)
	}

	types := buildTypeCatalogue(configured.ExtraTypes)
	functions, initErr := buildFunctionCatalogue(embeddedTables, configured.ExtraFunctions, types)
	typeResolver := newTypeResolver(configured.TypeNormaliserHooks, configured.ExtraTypes, configured.resolvedMaxTypeParseDepth())
	return &Dialect{
		settings:         configured,
		types:            types,
		functions:        functions,
		typeResolver:     typeResolver,
		functionResolver: newDialectFunctionResolver(functions, typeResolver, configured.Limits),
		initErr:          initErr,
	}
}

// Info describes the ClickHouse dialect: `{name:Type}` parameters, the default database
// as the search path, case-sensitive identifiers, no RETURNING, and asynchronous
// mutations, since ClickHouse runs ALTER UPDATE and ALTER DELETE as background mutations
// and so supports the asyncexec query command.
//
// The expression set is the base set plus scalar subqueries, window functions, array
// subscripts, the JSONExtract family, bitwise functions, lambdas for higher-order array
// functions, and tuple field access.
//
// Every slice and map in the result is the caller's own, so changing one changes no other
// Info value and no dialect.
//
// Returns dialect.Info which describes the dialect.
func (engine *Dialect) Info() dialect.Info {
	info := dialect.Info{
		Comments:            ast.DefaultSQLCommentStyle(),
		Lexical:             lexical.Rules(),
		Name:                engine.settings.Name,
		Family:              Family,
		SchemaSearchPath:    []string{"default"},
		RowIDAliases:        nil,
		SystemColumns:       systemColumns,
		SelectAliases:       selectAliases,
		SelectAliasOperands: 0,
		OperatorFeatures:    operatorFeatures,
		Identifiers:         sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingNone, Insensitive: false},
		Placeholders:        placeholders(),
		Expressions: ast.SQLFeaturesBase |
			ast.SQLFeatureScalarSubquery |
			ast.SQLFeatureWindowFunction |
			ast.SQLFeatureArraySubscript |
			ast.SQLFeatureJSONOp |
			ast.SQLFeatureBitwiseOp |
			ast.SQLFeatureLambda |
			ast.SQLFeatureStructFieldAccess,
		Returning:              0,
		AsyncMutations:         true,
		ScalarFunctionsInFrom:  false,
		UngroupedColumns:       dialect.UngroupedColumnsRejected,
		ViewsFollowTheirTables: false,
		IdentifierQuote:        dialect.IdentifierQuote{Open: "`", Close: "`"},
		Drivers:                []string{driverClickHouse},
		ToJSON:                 "",
		TextType:               "String",
		Filters:                dialect.FilterSyntax{Operators: filterOperators, JSONPathFunctions: nil, Casts: castForms, CastTypes: castTypes},
		Lists:                  listSyntax,
		Pagination:             paginationSyntax,
		MaxParameters:          maxParameters,
	}
	return info.Clone()
}

// ParseStatements tokenises the input and classifies each semicolon-separated statement.
//
// The returned slice carries the engine-private payload (token stream plus classified
// kind) on each ParsedStatement so downstream handlers can resume parsing without
// re-tokenising. A statement is tokenised only as far as the per-statement token budget,
// enough to classify it, and ApplyDDL and AnalyseQuery refuse it; the context is checked
// before each statement.
//
// A panic while tokenising or classifying is recovered and returned as a
// dialectkit.PanicError, and its stack is logged once at Warn through the context's
// logger.
//
// Takes sql (string) which is the raw SQL text to parse.
//
// Returns []ast.ParsedStatement which is one entry per statement.
// Returns error when tokenising the SQL fails, the context is done, giving its cause, or
// a parser invariant panics.
func (engine *Dialect) ParseStatements(ctx context.Context, sql string) (results []ast.ParsedStatement, err error) {
	defer func() { dialectkit.Recover(ctx, "clickhouse: parse statements", recover(), &results, &err) }()

	if ctxErr := context.Cause(ctx); ctxErr != nil {
		return nil, ctxErr
	}
	statements, lexErr := dialectkit.LexStatements(ctx, lexical.Rules(), sql, statementTokens, tokenEOF,
		engine.settings.Limits.TokensPerStatement())
	if lexErr != nil {
		return nil, lexErr
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

// ApplyDDL applies a DDL statement to the catalogue for the ClickHouse dialect.
//
// Syntax errors in the statement are returned as ordinary errors, as is a statement that
// opens with no ClickHouse statement word, such as a misspelt SELECT, or one whose
// parentheses or brackets do not balance. A panic while the statement is applied, a
// parser bug or a hook that panics, cannot crash the calling apply loop: it is recovered
// and returned as a dialectkit.PanicError, and its stack is logged once at Warn through
// the context's logger. It returns the context's cause before dispatch so the catalogue
// build loop can be cancelled by the caller, stops a walk once the context is done, and
// refuses to walk a statement longer than the configured token budget. A statement kind
// the catalogue ignores is a no-op whatever its length.
//
// Takes statement (ast.ParsedStatement) which carries the classified tokens to apply.
//
// Returns *ast.CatalogueMutation which is the catalogue change, or nil for a no-op kind.
// Returns error when another dialect parsed the statement, wrapping
// dialect.ErrForeignStatement, or when New could not build the dialect's function
// catalogue, the statement is malformed, the token budget is exceeded, the walk stops
// advancing, the context is cancelled, or a parser invariant panics.
func (engine *Dialect) ApplyDDL(
	ctx context.Context,
	statement ast.ParsedStatement,
) (mutation *ast.CatalogueMutation, err error) {
	defer func() { dialectkit.Recover(ctx, "clickhouse: apply DDL", recover(), &mutation, &err) }()

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

	handler, found := ddlParserDispatch[parsed.kind]
	if !found || handler == nil {
		return nil, nil
	}

	statementParser, budgetErr := engine.newStatementParser(dialectkit.WalkGuard(ctx, len(parsed.tokens)), parsed)
	if budgetErr != nil {
		return nil, budgetErr
	}
	if balanceErr := unbalancedBracket(parsed.tokens); balanceErr != nil {
		return nil, balanceErr
	}

	mutation, err = handler(statementParser)
	if statementParser.syntaxError != nil {
		return nil, statementParser.syntaxError
	}
	return mutation, err
}

// AnalyseQuery performs structural analysis of a DML statement for the ClickHouse
// dialect.
//
// Syntax errors in the statement are returned as ordinary errors, among them parentheses
// or brackets that do not balance, an empty select item and an AS with no alias after it.
// A panic while the statement is analysed, a parser bug or a hook that panics, cannot
// crash the calling analyser: it is recovered and returned as a dialectkit.PanicError,
// and its stack is logged once at Warn through the context's logger. The walk stops once
// the context is done, and statements longer than the configured token budget are refused
// before any parsing begins.
//
// Takes statement (ast.ParsedStatement) which carries the classified tokens to analyse.
//
// Returns *ast.RawQueryAnalysis which is the structural analysis result.
// Returns error when another dialect parsed the statement, wrapping
// dialect.ErrForeignStatement, or when New could not build the dialect's function
// catalogue, the statement is malformed, the token budget is exceeded, the walk stops
// advancing, a parameter type is invalid, a parser invariant panics, or the context is
// cancelled.
func (engine *Dialect) AnalyseQuery(
	ctx context.Context,
	_ *sqlmodel.Catalogue,
	statement ast.ParsedStatement,
) (analysis *ast.RawQueryAnalysis, err error) {
	defer func() { dialectkit.Recover(ctx, "clickhouse: analyse query", recover(), &analysis, &err) }()

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

	if int(parsed.kind) >= len(statementAnalysers) || statementAnalysers[parsed.kind] == nil {
		return unanalysedStatement(parsed)
	}
	analyser := statementAnalysers[parsed.kind]

	statementParser, budgetErr := engine.newStatementParser(dialectkit.WalkGuard(ctx, len(parsed.tokens)), parsed)
	if budgetErr != nil {
		return nil, budgetErr
	}
	if balanceErr := unbalancedBracket(parsed.tokens); balanceErr != nil {
		return nil, balanceErr
	}
	statementParser.spans = dialectkit.NewSpanRecorder(statement.Location)

	analysis, err = analyser(statementParser)
	if braceErr := statementParser.misplacedBrace(); braceErr != nil {
		return nil, braceErr
	}
	if statementParser.syntaxError != nil {
		return nil, statementParser.syntaxError
	}
	if err != nil {
		return analysis, err
	}
	if endErr := statementParser.requireEnd(); endErr != nil {
		return nil, endErr
	}
	if matcherErr := rejectColumnsMatcher(analysis); matcherErr != nil {
		return nil, matcherErr
	}
	statementParser.numberPlacedParameters()
	resolveElementAccess(analysis, nil)
	resolveExpressionAliases(analysis, nil)
	bindLambdaParameters(analysis)
	engine.recordStatementSpans(statementParser, analysis, parsed, statement)
	return analysis, nil
}

// BuiltinFunctions returns the ClickHouse built-in function catalogue.
//
// Returns *ast.FunctionCatalogue which is the engine's function catalogue.
func (engine *Dialect) BuiltinFunctions() *ast.FunctionCatalogue {
	return engine.functions
}

// BuiltinTypes returns the ClickHouse built-in type catalogue.
//
// Returns *ast.TypeCatalogue which is the engine's type catalogue.
func (engine *Dialect) BuiltinTypes() *ast.TypeCatalogue {
	return engine.types
}

// NormaliseTypeName resolves a raw type name to a structured SQLType for ClickHouse.
//
// It handles Nullable(T) / Array(T) / LowCardinality(T) / Tuple(...) / Map(K, V) /
// Nested(...) / Enum8(...) / Enum16(...) / FixedString(N) / Decimal(P, S) /
// DateTime64(p[, 'tz']) / Variant / AggregateFunction wrappers. The parsed result is
// unwrapped to the inner type. Callers that need the Nullable outer-wrapper flag, such as
// the DDL parser reading a CREATE TABLE column, use the type parser's result instead.
//
// Takes name (string) which is the raw type spelling.
// Takes modifiers (...int) which are the type modifiers such as precision and scale.
//
// Returns sqlmodel.SQLType which is the structured, Nullable-unwrapped type.
func (engine *Dialect) NormaliseTypeName(name string, modifiers ...int) sqlmodel.SQLType {
	return engine.typeResolver.normalise(name, modifiers...)
}

// TableValuedFunctionColumns returns output columns for a known table-valued function.
//
// A nil result is returned for functions whose output shape depends on the source (such
// as remote / cluster / file / url / s3), whose column list resolves at runtime. Only
// built-in table-valued functions are resolved here. ClickHouse user-defined functions
// are scalar lambda expressions (CREATE FUNCTION name AS (x) -> ...), not table-valued,
// so there are no user table-valued functions to register in the catalogue and the engine
// does not implement dialect.CatalogueFunctionResolverPort. This is a dialect limitation.
//
// Takes functionName (string) which is the table-valued function name to resolve.
//
// Returns []ast.ScopedColumn which is the function's output columns, or nil when the
// function is unknown or resolves at runtime.
func (*Dialect) TableValuedFunctionColumns(functionName string) []ast.ScopedColumn {
	columns, exists := clickhouseTableValuedFunctionColumns[functionName]
	if !exists {
		return nil
	}
	result := make([]ast.ScopedColumn, len(columns))
	copy(result, columns)
	return result
}

// PromoteType returns the wider type when both operands belong to a numeric category.
//
// ClickHouse follows a least-common-type rule: within the integer family the wider bit
// width wins; mixing a floating-point operand with anything numeric collapses to the
// float (Float64 if either side is Float64, else the wider float width); mixing a decimal
// operand with an integer keeps the decimal; mixing across non-numeric categories returns
// the left operand unchanged. Within a category, unrecognised engine names have rank
// zero; a recognised higher-ranked type wins, while ties preserve the left operand.
//
// Takes left (sqlmodel.SQLType) which is the existing operand type.
// Takes right (sqlmodel.SQLType) which is the candidate type the caller wants to fold in.
//
// Returns sqlmodel.SQLType which is the promoted type or the left operand when no
// promotion applies.
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
	if !isPromotableNumericCategory(left.Category) || !isPromotableNumericCategory(right.Category) {
		return left
	}
	if left.Category != right.Category {
		return crossCategoryNumericPromotion(left, right)
	}
	if left.Category == sqlmodel.TypeCategoryInteger {
		return promoteSameCategoryInteger(left, right)
	}
	if clickHouseNumericTypeRank(right.EngineName) > clickHouseNumericTypeRank(left.EngineName) {
		return right
	}
	return left
}

// CoalesceTypedUpToNonNullable reports that ClickHouse types coalesce and ifNull only by
// their arguments up to the first that cannot be NULL.
//
// Returns bool which is always true.
func (*Dialect) CoalesceTypedUpToNonNullable() bool {
	return true
}

// CanImplicitCast reports whether ClickHouse converts a value of one type to another
// implicitly, as it does for a function argument or a branch of a conditional.
//
// A type converts to any type of its own category, and a number widens across the numeric
// categories the way ClickHouse's arithmetic does: an integer to a float or a decimal,
// and a decimal to a float, never the reverse. The configured ImplicitCastHooks are
// consulted first, most recent first, and a non-nil result overrides the default.
//
// Takes from (sqlmodel.SQLType) which is the source type.
// Takes to (sqlmodel.SQLType) which is the target type.
//
// Returns bool which is true when the implicit cast is allowed.
func (engine *Dialect) CanImplicitCast(
	from sqlmodel.SQLType,
	to sqlmodel.SQLType,
) bool {
	allowed, answered := dialectkit.FirstHookAnswer(engine.settings.ImplicitCastHooks,
		func(hook func(from, to sqlmodel.SQLType) *bool) *bool {
			return hook(from, to)
		})
	if answered {
		return allowed
	}
	return from.Category == to.Category || widensNumerically(from.Category, to.Category)
}

// CoreTypes returns ClickHouse's core types: Bool, String, UInt64 for counts and for a
// LIMIT or OFFSET parameter, JSON, and no implicit row identifier.
//
// Returns dialect.CoreTypes which holds the core types.
func (*Dialect) CoreTypes() dialect.CoreTypes {
	return coreTypes
}

// BinaryResult types the ClickHouse operators whose result differs from the common type
// of their operands: || and the arithmetic operators, whose integer results widen by
// ClickHouse's own rules, so a Float64 times an integer is a Float64 and a UInt8 plus a
// UInt8 is a UInt16.
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

// UnaryResult types NOT as ClickHouse does, a Bool over a Bool and a UInt8 otherwise,
// NULL when its operand may be, and a prefix minus by ClickHouse's negation rule, so
// negating a UInt8 gives an Int16.
//
// Takes operator (string) which is the operator.
// Takes operand (sqlmodel.SQLType) which is the operand's type.
//
// Returns sqlmodel.SQLType which is the result's type.
// Returns sqlmodel.FunctionNullableBehaviour which says when the result is NULL.
// Returns bool which is true for NOT and minus.
func (*Dialect) UnaryResult(operator string, operand sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
	return unaryResult(operator, operand)
}

// ResolveFunctionCall delegates to the ClickHouse polymorphic function resolver.
//
// It is used for arrayMap / arrayFilter / tupleElement / map / coalesce / if / multiIf,
// the arithmetic functions, the aggregate combinator family (countIf, sumOrNull,
// uniqMerge, and similar) and calls to functions created as lambdas, where the static
// catalogue cannot express the return type from argument types alone.
//
// A call of a function created as a lambda is typed from its body, parsed with the
// dialect's types, hooks and limits; each body is typed at most once for each set of
// argument types, a function its own body reaches again gives no type, and typing gives
// up with no opinion past a budget, so recursive or heavily nested functions cannot hang
// the caller. A panic while the call is resolved, such as one while a function created as
// a lambda has its body parsed, is recovered and returned as a dialectkit.PanicError, and
// its stack is logged once at Warn through the context's logger.
//
// Takes catalogue (*sqlmodel.Catalogue) which provides type and function context.
// Takes name (string) which is the function name to resolve.
// Takes schema (string) which is the schema the call is qualified by.
// Takes argumentTypes ([]sqlmodel.SQLType) which are the resolved argument types.
//
// Returns *ast.FunctionResolution which is the resolved signature and return type.
// Returns error when the function cannot be resolved for the given arguments, the context
// is done, giving its cause, or a resolver invariant panics.
func (engine *Dialect) ResolveFunctionCall(
	ctx context.Context,
	catalogue *sqlmodel.Catalogue,
	name string,
	schema string,
	argumentTypes []sqlmodel.SQLType,
) (resolution *ast.FunctionResolution, err error) {
	defer func() { dialectkit.Recover(ctx, "clickhouse: resolve function call", recover(), &resolution, &err) }()

	if ctxErr := context.Cause(ctx); ctxErr != nil {
		return nil, ctxErr
	}
	resolution, err = engine.functionResolver.ResolveFunctionCall(ctx, catalogue, name, schema, argumentTypes)
	if ctxErr := context.Cause(ctx); ctxErr != nil {
		return nil, ctxErr
	}
	return resolution, err
}

// newStatementParser creates a parser over a statement's tokens configured with the
// dialect's limits, refusing statements longer than the per-statement token budget. Its
// walk, and the walks of the parsers it starts for nested constructs, share the
// interrupt, so the whole statement stops when the interrupt does, as
// dialectkit.WalkGuard describes.
//
// Takes interrupt (func() error) which stops the statement's walk, as the
// dialectkit.WalkGuard the calling entry point built does.
// Takes parsed (*parsedStatement) which is the statement.
//
// Returns *parser which is the configured parser positioned at the statement start.
// Returns error which wraps dialectkit.ErrTokenBudget when the statement holds more
// tokens than the budget allows.
func (engine *Dialect) newStatementParser(interrupt func() error, parsed *parsedStatement) (*parser, error) {
	if err := parsed.checkTokenBudget(engine.settings.Limits); err != nil {
		return nil, err
	}
	tokens := parsed.tokens
	statementParser := newParser(tokens)
	statementParser.Interrupt = interrupt
	statementParser.maxParseDepth = engine.settings.Limits.ParseDepth()
	statementParser.types = engine.typeResolver
	statementParser.joinUseNulls = engine.settings.JoinUseNulls
	if enabled, found := statementSetting(tokens, settingJoinUseNulls); found {
		statementParser.joinUseNulls = enabled
	}
	statementParser.groupByUseNulls = engine.settings.GroupByUseNulls
	if enabled, found := statementSetting(tokens, settingGroupByUseNulls); found {
		statementParser.groupByUseNulls = enabled
	}
	return statementParser, nil
}

// settings holds the configuration of a ClickHouse dialect: its name, its extra types and
// functions, the type-system hooks, the parser limits and the join_use_nulls setting it
// assumes.
//
// Every option that registers something adds to what earlier options registered, so a
// flavour's own settings survive a caller's options of the same kind.
type settings struct {
	// ExtraTypes holds the extra type definitions, keyed by lower-cased name. Each
	// WithExtraTypes merges into it, a later entry replacing an earlier one of the same
	// name.
	ExtraTypes map[string]sqlmodel.SQLType

	// Name is the dialect name Info reports; New sets it to clickhouse.
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

	// Limits caps recursion through analysis and expression parsing and the token count of a
	// single statement; a zero field selects the shared default.
	Limits dialectkit.Limits

	// MaxTypeParseDepth caps wrapper nesting inside a type name (Array(Array(...)) and
	// similar). Zero selects defaultMaxTypeParseDepth.
	MaxTypeParseDepth int

	// JoinUseNulls is the server's join_use_nulls setting the analysis assumes for a query
	// that does not set it in its own SETTINGS clause.
	JoinUseNulls bool

	// GroupByUseNulls is the server's group_by_use_nulls setting the analysis assumes for a
	// query that does not set it in its own SETTINGS clause.
	GroupByUseNulls bool
}

// resolvedMaxTypeParseDepth returns the effective type-name nesting cap, falling back to
// defaultMaxTypeParseDepth when unset.
//
// Returns int which is the configured cap or defaultMaxTypeParseDepth.
func (d settings) resolvedMaxTypeParseDepth() int {
	if d.MaxTypeParseDepth > 0 {
		return d.MaxTypeParseDepth
	}
	return defaultMaxTypeParseDepth
}

// analyseAlterTable analyses an ALTER TABLE statement for its parameter placeholders.
//
// An asynchronous ALTER TABLE ... UPDATE or DELETE mutation is marked as one, with its
// action body in the dialect's detail and the kind of row change it makes, so the
// analyser can recommend asyncexec and judge a RETURNING clause, and every `{name:Type}`
// placeholder in the statement is registered as an assignment parameter. A RETURNING
// clause, which ClickHouse does not have, is recorded so the analyser reports it.
//
// The whole statement is probed for the async body on a throwaway parser so this parser's
// cursor is untouched.
//
// Returns *ast.RawQueryAnalysis which holds the placeholders and any async body.
// Returns error when a placeholder carries a malformed or unrecognised type tag.
func (p *parser) analyseAlterTable() (*ast.RawQueryAnalysis, error) {
	analysis := &ast.RawQueryAnalysis{}
	if body, change, isAsync := alterTableAsyncBody(p.Tokens, p.Interrupt); isAsync {
		analysis.AsyncMutation = true
		analysis.DataChange = change
		detailOf(analysis).asyncBody = body
	}
	p.recordMutationClauses(analysis)
	p.collectClickHouseParametersUntilReturning(analysis, sqlmodel.ParameterContextAssignment)
	p.parseMutationTail(analysis)
	if p.firstParameterTypeError != nil {
		return analysis, p.firstParameterTypeError
	}
	return analysis, nil
}

// recordMutationClauses records the target table, the assignments and the WHERE condition
// of an asynchronous ALTER TABLE ...
//
// UPDATE or DELETE mutation, so the names they use are checked against the table, and
// where the condition is written. The header is read on a throwaway parser over the same
// tokens, so this parser's cursor is untouched.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the table, the clauses and the
// spans.
func (p *parser) recordMutationClauses(analysis *ast.RawQueryAnalysis) {
	probe := newParser(p.Tokens)
	probe.Interrupt = p.Interrupt
	if !probe.MatchKeyword("ALTER") || !probe.MatchKeyword("TABLE") {
		return
	}
	probe.matchIfExists()
	database, name, err := probe.parseDatabaseQualifiedName()
	if err != nil {
		return
	}
	probe.matchOnCluster()
	isUpdate := probe.MatchKeyword("UPDATE")
	if !isUpdate && !probe.MatchKeyword("DELETE") {
		return
	}
	analysis.FromTables = []ast.TableReference{{Schema: database, Name: name, Alias: ""}}
	where := topLevelKeywordIndex(p.Tokens, probe.Position, kwWhere)
	if isUpdate {
		assignmentsEnd := where
		if assignmentsEnd < 0 {
			assignmentsEnd = len(p.Tokens)
		}
		p.recordAssignments(analysis, probe.Position, assignmentsEnd, name)
	}
	if where < 0 {
		return
	}
	conditionEnd := topLevelKeywordIndex(p.Tokens, where+1, kwReturning, kwSettings)
	if conditionEnd < 0 {
		conditionEnd = len(p.Tokens)
	}
	condition, err := p.readCondition(where+1, conditionEnd)
	if err != nil {
		p.recordSyntaxError(err)
		return
	}
	addClauseExpressions(analysis, ast.ClauseWhere, []ast.Expression{condition})
	p.recordSpan(dialectkit.SpanWhere, where, conditionEnd)
	p.recordSpan(dialectkit.SpanWhereCondition, where+1, conditionEnd)
}

// WithName sets the name the dialect reports in Info, for a flavour built on this
// dialect. The family stays clickhouse, so type mappings and drivers keep resolving.
//
// Takes name (string) which is the dialect name to report.
//
// Returns Option which applies the name.
func WithName(name string) Option {
	return func(configured *settings) {
		configured.Name = name
	}
}

// WithExtraTypes adds named type definitions to the ClickHouse dialect.
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
			configured.ExtraTypes[canonicaliseTypeName(name)] = types[name]
		}
	}
}

// WithExtraFunctions registers additional functions after the built-in ClickHouse
// function catalogue is built, such as executable user-defined functions the server loads
// from its configuration. Repeated calls add registrations, which run in order.
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
// first, before the extra and built-in types, both for a whole type name and for each
// bare type name inside a wrapper such as Array(...); the first non-nil result wins.
//
// Takes hook (func(string, []int) *sqlmodel.SQLType) which receives the type name and its
// modifiers and returns the resolved type, or nil to decline.
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

// WithJoinUseNulls sets the join_use_nulls value assumed during analysis.
//
// Enable it when unmatched outer-join rows should produce NULLs. The default is false,
// and a query's SETTINGS clause takes precedence.
//
// Takes enabled (bool) which is the assumed join_use_nulls setting.
//
// Returns Option which applies the setting.
func WithJoinUseNulls(enabled bool) Option {
	return func(configured *settings) {
		configured.JoinUseNulls = enabled
	}
}

// WithGroupByUseNulls sets the group_by_use_nulls value assumed during analysis.
//
// Enable it when omitted subtotal keys should be nullable. The default is false, and a
// query's SETTINGS clause takes precedence.
//
// Takes enabled (bool) which is the assumed group_by_use_nulls setting.
//
// Returns Option which applies the setting.
func WithGroupByUseNulls(enabled bool) Option {
	return func(configured *settings) {
		configured.GroupByUseNulls = enabled
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
// default remains in force, and a value above dialectkit.MaxParseDepthCeiling is held at
// the ceiling.
//
// Returns Option which applies the depth cap to the dialect.
func WithMaxParseDepth(depth int) Option {
	return func(configured *settings) {
		configured.Limits = configured.Limits.WithMaxParseDepth(depth)
	}
}

// WithMaxTokensPerStatement sets the maximum number of tokens a single statement may hold
// before the parser refuses to walk it.
//
// The budget bounds the work required by an adversarially long statement, its tokenising
// included: a statement is tokenised only as far as the budget, so one huge statement
// costs no more memory than the budget allows. The default is generous
// (dialectkit.DefaultMaxTokensPerStatement) so realistic and generated statements are
// unaffected; lower it to harden against hostile input or raise it for unusually large
// generated statements. Exceeding it fails the statement with an error wrapping
// dialectkit.ErrTokenBudget.
//
// Takes limit (int) which is the maximum token count; values below 1 are ignored so the
// default remains in force, and a value above dialectkit.MaxTokensPerStatementCeiling is
// held at the ceiling.
//
// Returns Option which applies the token budget to the dialect.
func WithMaxTokensPerStatement(limit int) Option {
	return func(configured *settings) {
		configured.Limits = configured.Limits.WithMaxTokensPerStatement(limit)
	}
}

// WithMaxTypeParseDepth sets the maximum wrapper nesting the type-name parser accepts.
//
// Type names such as Array(Array(Tuple(...))) are parsed recursively, so unbounded
// nesting would otherwise exhaust the goroutine stack. The default
// (defaultMaxTypeParseDepth) is also the most it can be, sqlmodel.MaxTypeDepth, the depth
// to which Seagull follows a type's parts, such as when it checks that clickhouse-go can
// read a result; lower it to harden against hostile input. Exceeding it fails the type
// with an error wrapping dialectkit.ErrParseDepth.
//
// Takes depth (int) which is the maximum wrapper nesting; values below 1 are ignored so
// the default remains in force, and a value above sqlmodel.MaxTypeDepth is held at
// sqlmodel.MaxTypeDepth.
//
// Returns Option which applies the type depth cap to the dialect.
func WithMaxTypeParseDepth(depth int) Option {
	return func(configured *settings) {
		if depth > 0 {
			configured.MaxTypeParseDepth = min(depth, sqlmodel.MaxTypeDepth)
		}
	}
}

// checkRunOn detects adjacent migration statements with a missing semicolon. It skips
// GRANT, REVOKE and unclassified statements, and reads every token of a statement over
// the token budget again, one at a time, so a missing semicolon past the budget is found
// too.
//
// Takes parsed (*parsedStatement) which is the statement.
//
// Returns error which locates the statement that runs on, the context's cause, or the
// error that stopped the statement being read again; nil when the statement ends where it
// should.
func checkRunOn(ctx context.Context, parsed *parsedStatement) error {
	switch parsed.kind {
	case statementKindUnknown, statementKindGrant, statementKindRevoke:
		return nil
	default:
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

// rejectColumnsMatcher rejects dynamic COLUMNS matchers in queries.
//
// Their output is chosen at execution time, so the generator cannot build a matching row
// type.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysed query.
//
// Returns error which is a dialectkit.ParseError at the matcher, or nil when there is
// none.
func rejectColumnsMatcher(analysis *ast.RawQueryAnalysis) error {
	call := dialectkit.FindProjectionCall(analysis, columnsMatcher)
	if call == nil {
		call = dialectkit.FindClauseCall(analysis, columnsMatcher)
	}
	if call == nil {
		return nil
	}
	return &dialectkit.ParseError{
		Token:    call.FunctionName,
		Message:  "COLUMNS(...) is not supported: the generator cannot list the columns it matches; name the columns instead",
		Position: call.Span.Start,
	}
}

// widensNumerically reports whether ClickHouse widens a number of one category to another
// implicitly: an integer to a float or a decimal, and a decimal to a float.
//
// Takes from (sqlmodel.SQLTypeCategory) which is the source category.
// Takes to (sqlmodel.SQLTypeCategory) which is the target category.
//
// Returns bool which is true when the widening is implicit.
func widensNumerically(from, to sqlmodel.SQLTypeCategory) bool {
	switch from {
	case sqlmodel.TypeCategoryInteger:
		return to == sqlmodel.TypeCategoryFloat || to == sqlmodel.TypeCategoryDecimal
	case sqlmodel.TypeCategoryDecimal:
		return to == sqlmodel.TypeCategoryFloat
	default:
		return false
	}
}

// topLevelKeywordIndex returns the index of the first token, at or after from, that is
// one of the keywords outside every parenthesis or bracket, or -1.
//
// Takes tokens ([]token) which are the statement's tokens.
// Takes from (int) which is the index to start at.
// Takes keywords (...string) which are the keywords to find.
//
// Returns int which is the index, or -1.
func topLevelKeywordIndex(tokens []token, from int, keywords ...string) int {
	depth := 0
	for index := from; index < len(tokens); index++ {
		if depth == 0 && identifierMatchesAny(tokens[index], keywords) {
			return index
		}
		depth = nestingAfter(tokens[index], depth)
	}
	return -1
}

// alterTableAsyncBody reports whether an ALTER TABLE statement is an asynchronous
// mutation (ALTER TABLE ... UPDATE / DELETE) and captures its action body text.
//
// The statement header is parsed on a throwaway parser so the caller's analysis parser is
// untouched, mirroring the header grammar of parseAlterTable (ALTER TABLE [IF EXISTS]
// [db.]name [ON CLUSTER c] <action>). This lets the query-analysis path mark exactly the
// statements the DDL path records as MutationAsyncDataUpdate / MutationAsyncDataDelete.
// The body stops before a RETURNING clause.
//
// Takes tokens ([]token) which are the statement's tokens.
// Takes interrupt (func() error) which is the interrupt of the parser that walks the
// statement, shared so the probe's reads count towards the statement's walk.
//
// Returns string which is the captured action body text.
// Returns ast.DataChange which is the row change the action makes.
// Returns bool which is true when the action is UPDATE or DELETE.
func alterTableAsyncBody(tokens []token, interrupt func() error) (string, ast.DataChange, bool) {
	probe := newParser(tokens)
	probe.Interrupt = interrupt
	if !probe.MatchKeyword("ALTER") || !probe.MatchKeyword("TABLE") {
		return "", 0, false
	}
	probe.matchIfExists()
	if _, _, err := probe.parseDatabaseQualifiedName(); err != nil {
		return "", 0, false
	}
	probe.matchOnCluster()
	switch {
	case probe.MatchKeyword("UPDATE"):
		return probe.consumeTextUntilReturning(), ast.DataChangeUpdate, true
	case probe.MatchKeyword("DELETE"):
		return probe.consumeTextUntilReturning(), ast.DataChangeDelete, true
	default:
		return "", 0, false
	}
}

// promoteSameCategoryInteger promotes two integers of the same category.
//
// When the operands share a width rank but differ in sign (for example Int32 vs UInt32)
// the least-common-type is the next wider signed type, because the signed range at the
// shared width cannot hold the unsigned operand's high-bit values. The 64-bit pair (Int64
// vs UInt64) is excluded because stock ClickHouse throws rather than promoting to Int128,
// so the left operand is preserved for that pair. Otherwise the wider rank wins, with
// ties preserving the left operand. The mixed-sign decision keys off the resolver's
// integerRank table (bit-width ranks) rather than clickHouseNumericTypeRank so a single
// helper, nextWiderSignedInteger, owns the widening map; the two rank tables agree on
// integer widths.
//
// Takes left (sqlmodel.SQLType) which is the existing operand type.
// Takes right (sqlmodel.SQLType) which is the candidate type.
//
// Returns sqlmodel.SQLType which is the promoted integer type.
func promoteSameCategoryInteger(left, right sqlmodel.SQLType) sqlmodel.SQLType {
	leftWidth := integerRank(left.EngineName)
	rightWidth := integerRank(right.EngineName)
	if leftWidth == rightWidth && leftWidth != 0 &&
		isUnsignedInteger(left.EngineName) != isUnsignedInteger(right.EngineName) {
		if widened, found := nextWiderSignedInteger(leftWidth); found {
			return widened
		}
	}
	if clickHouseNumericTypeRank(right.EngineName) > clickHouseNumericTypeRank(left.EngineName) {
		return right
	}
	return left
}

// isPromotableNumericCategory reports whether a SQLType category participates in the
// ClickHouse numeric promotion matrix. Integer, floating-point, and fixed-point decimal
// categories qualify; all other categories fall through to the left operand at the call
// site.
//
// Takes category (sqlmodel.SQLTypeCategory) which classifies the candidate type.
//
// Returns bool which is true when the category may be promoted.
func isPromotableNumericCategory(category sqlmodel.SQLTypeCategory) bool {
	switch category {
	case sqlmodel.TypeCategoryInteger,
		sqlmodel.TypeCategoryFloat,
		sqlmodel.TypeCategoryDecimal:
		return true
	default:
		return false
	}
}

// crossCategoryNumericPromotion returns ClickHouse's least supertype of two numbers of
// different categories. An integer and a decimal give the decimal, a Float32 and an
// integer of at most 16 bits give Float32, and any other pair with a float gives Float64.
//
// Takes left (sqlmodel.SQLType) which is the existing type.
// Takes right (sqlmodel.SQLType) which is the candidate type.
//
// Returns sqlmodel.SQLType which is the supertype.
func crossCategoryNumericPromotion(left, right sqlmodel.SQLType) sqlmodel.SQLType {
	float, other := left, right
	if right.Category == sqlmodel.TypeCategoryFloat {
		float, other = right, left
	}
	switch {
	case float.Category != sqlmodel.TypeCategoryFloat:
		if left.Category == sqlmodel.TypeCategoryDecimal {
			return left
		}
		return right
	case other.Category == sqlmodel.TypeCategoryInteger && float.Width == widthWord && other.Width <= widthShort:
		return float
	default:
		return primitiveType(engineNameFloat64)
	}
}

// clickHouseNumericTypeRank returns a synthetic rank for a numeric engine type name.
//
// Higher ranks denote wider or more general types within their category. Unknown engine
// names produce rank 0, below every recognised type; equal ranks preserve the left
// operand in PromoteType.
//
// Takes engineName (string) which is the ClickHouse type spelling.
//
// Returns int which is the comparable rank.
func clickHouseNumericTypeRank(engineName string) int {
	if rank, exists := clickHouseNumericRanks[engineName]; exists {
		return rank
	}
	return 0
}

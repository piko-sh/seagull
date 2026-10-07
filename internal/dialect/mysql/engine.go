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

package mysql

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/mysql/lexical"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

var (
	// ddlHandlers dispatches DDL statement kinds to their parser entry points.
	ddlHandlers = [statementKindCount]ddlHandler{
		statementKindCreateTable: func(p *parser, engine *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseCreateTable(engine)
		},
		statementKindDropTable: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) { return p.parseDropTable() },
		statementKindAlterTable: func(p *parser, engine *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseAlterTable(engine)
		},
		statementKindCreateView:  func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) { return p.parseCreateView() },
		statementKindDropView:    func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) { return p.parseDropView() },
		statementKindCreateIndex: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) { return p.parseCreateIndex() },
		statementKindDropIndex:   func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) { return p.parseDropIndex() },
		statementKindCreateTrigger: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseCreateOrDropTrigger(statementKindCreateTrigger)
		},
		statementKindDropTrigger: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseCreateOrDropTrigger(statementKindDropTrigger)
		},
		statementKindCreateDatabase: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseCreateDatabase()
		},
		statementKindDropDatabase: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) { return p.parseDropDatabase() },
		statementKindCreateFunction: func(p *parser, engine *Dialect) (*ast.CatalogueMutation, error) {
			return p.parseCreateFunction(engine)
		},
		statementKindDropFunction: func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) { return p.parseDropFunction() },
		statementKindRenameTable:  func(p *parser, _ *Dialect) (*ast.CatalogueMutation, error) { return p.parseRenameTable() },
	}

	// runOnRules names the words that open a statement, for finding a migration statement
	// that runs on into the next one because the semicolon between them is missing.
	runOnRules = dialectkit.RunOnRules[tokenKind]{
		Starters: map[string][]string{
			keywordCREATE: nil,
			"ALTER":       {keywordTABLE, "VIEW", keywordDATABASE, keywordSCHEMA, keywordFUNCTION, keywordPROCEDURE, "EVENT"},
			keywordDROP: {keywordTABLE, "VIEW", keywordDATABASE, keywordSCHEMA, keywordFUNCTION, keywordPROCEDURE, "TRIGGER",
				"EVENT", keywordINDEX},
			"RENAME": {keywordTABLE},
		},
		Word:  tokenIdentifier,
		Open:  tokenLeftParen,
		Close: tokenRightParen,
	}

	// alterTableRunOnRules are runOnRules for an ALTER TABLE, whose actions open with ALTER
	// and DROP followed by an index, a key or a column, whose name may be any word.
	alterTableRunOnRules = dialectkit.RunOnRules[tokenKind]{
		Starters: map[string][]string{
			keywordCREATE: nil,
			"ALTER":       {keywordTABLE},
			keywordDROP:   {keywordTABLE},
			"RENAME":      {keywordTABLE},
		},
		Word:  tokenIdentifier,
		Open:  tokenLeftParen,
		Close: tokenRightParen,
	}

	// queryAnalysers dispatches DML statement kinds to their analysers. Kinds without an
	// entry use analyseUnanalysedStatement to check whether an empty analysis is allowed.
	queryAnalysers = [statementKindCount]queryAnalyser{
		statementKindSelect:  (*parser).analyseSelect,
		statementKindInsert:  dialectkit.ChangingRows(ast.DataChangeInsert, (*parser).analyseInsert),
		statementKindReplace: dialectkit.ChangingRows(ast.DataChangeInsert, (*parser).analyseInsert),
		statementKindUpdate:  dialectkit.ChangingRows(ast.DataChangeUpdate, (*parser).analyseUpdate),
		statementKindDelete:  dialectkit.ChangingRows(ast.DataChangeDelete, (*parser).analyseDelete),
		statementKindValues:  (*parser).analyseSelect,
		statementKindCall:    (*parser).analyseCall,
		statementKindShow:    (*parser).analyseShow,
	}
)

// Option configures a MySQL dialect.
//
// Options apply in the order given, and a flavour such as mariadb applies its own before
// the caller's. Every registering option is additive: extra types merge, a later entry
// replacing an earlier one of the same name; the built-ins WithoutBuiltinFunctions names
// accumulate and are left out first; extra-function registrations run in order after the
// built-in catalogue; and the type, cast, promote and function-call hooks chain, the most
// recently added consulted first, so a caller's hook can override a flavour's while a
// hook that returns nil falls through to the earlier hooks and then to the built-in
// rules. WithName, WithReturning, the capability options and the limit options set a
// single value, the last one winning.
type Option func(*settings)

// Dialect is the MySQL implementation of dialect.DialectPort.
type Dialect struct {
	// functions is the MySQL function catalogue used for resolution.
	functions *ast.FunctionCatalogue

	// types is the MySQL type catalogue: the built-in types merged with any extra types.
	types *ast.TypeCatalogue

	// typeResolver normalises type names through the hooks, the extra types and the built-in
	// types.
	typeResolver *typeResolver

	// initErr is the error building the dialect met, such as built-in signatures that did
	// not decode, which ApplyDDL and AnalyseQuery return; nil when it was built whole.
	initErr error

	// settings captures dialect-level overrides for this engine instance.
	settings settings
}

// New creates the MySQL dialect, named mysql and in the mysql family.
//
// Takes options (...Option) which configure the dialect's name, parser limits, types,
// functions, hooks and capabilities; see Option for how they combine.
//
// Returns *Dialect which is the configured dialect.
func New(options ...Option) *Dialect {
	configured := settings{}
	configured.Name = "mysql"
	configured.UngroupedColumns = dialect.UngroupedColumnsWarned
	for _, option := range options {
		option(&configured)
	}
	return newDialect(configured, builtinFunctionSpec)
}

// newDialect builds the dialect a configuration describes, keeping any error building it
// meets for ApplyDDL and AnalyseQuery to report.
//
// Takes configured (settings) which is the configuration with every option applied.
// Takes loadSpec (func() (*dialectkit.FunctionSpec, error)) which gives the decoded
// built-in function signatures.
//
// Returns *Dialect which is the dialect.
func newDialect(configured settings, loadSpec func() (*dialectkit.FunctionSpec, error)) *Dialect {
	types := buildTypeCatalogue(configured.ExtraTypes)
	functions, err := buildFunctionCatalogue(loadSpec, configured.ExtraFunctions, configured.WithheldFunctions, types)
	if err != nil {
		err = fmt.Errorf("%s: building the dialect: %w", configured.Name, err)
	}
	return &Dialect{
		settings:     configured,
		types:        types,
		functions:    functions,
		typeResolver: newTypeResolver(configured.TypeNormaliserHooks, configured.ExtraTypes),
		initErr:      err,
	}
}

// Info describes the MySQL dialect: question-mark parameters, no default schema, and the
// MySQL expression set, which excludes || string concatenation because MySQL treats || as
// logical OR.
//
// RETURNING is accepted on no statement unless WithReturning names some, as MariaDB does.
// The family is always mysql, so MariaDB and a custom WithName keep the MySQL type
// mappings and drivers. Each call returns a fresh copy, so a caller that changes one
// changes neither the dialect nor any other caller's copy.
//
// Returns dialect.Info which describes the dialect.
func (engine *Dialect) Info() dialect.Info {
	info := dialect.Info{
		Comments:            ast.DefaultSQLCommentStyle(),
		Lexical:             engine.settings.lexicalRules(),
		Name:                engine.settings.Name,
		Family:              Family,
		SchemaSearchPath:    []string{""},
		RowIDAliases:        nil,
		SystemColumns:       systemColumns,
		SelectAliases:       ast.ClauseGroupBy | ast.ClauseHaving | ast.ClauseOrderBy,
		SelectAliasOperands: ast.ClauseGroupBy | ast.ClauseHaving | ast.ClauseOrderBy,
		OperatorFeatures:    engine.operatorFeatures(),
		Identifiers:         sqlmodel.IdentifierCase{Unquoted: sqlmodel.IdentifierFoldingNone, Insensitive: true},
		Placeholders:        placeholderForms,
		Expressions: (ast.SQLFeaturesBase &^ ast.SQLFeatureStringConcat) |
			ast.SQLFeatureWindowFunction |
			ast.SQLFeatureJSONOp |
			ast.SQLFeatureScalarSubquery |
			ast.SQLFeatureBitwiseOp,
		Returning:              engine.settings.Returning,
		AsyncMutations:         false,
		ScalarFunctionsInFrom:  false,
		UngroupedColumns:       engine.settings.UngroupedColumns,
		ViewsFollowTheirTables: false,
		IdentifierQuote:        dialect.IdentifierQuote{Open: "`", Close: "`"},
		Drivers:                []string{driverMySQL},
		ToJSON:                 "",
		TextType:               "char",
		Filters:                dialect.FilterSyntax{Operators: nil, JSONPathFunctions: jsonPathFunctions, Casts: castForms, CastTypes: castTypes},
		Lists:                  listSyntax,
		Pagination:             paginationSyntax,
		MaxParameters:          maxParameters,
	}
	return info.Clone()
}

// ParseStatements tokenises and classifies SQL statements.
//
// A panic while tokenising or classifying is recovered and returned as a
// dialectkit.PanicError, and its stack is logged once at Warn through the context's
// logger. The context is checked before each statement is tokenised, and tokenising a
// statement stops at the dialect's token budget: a statement past it keeps only the
// tokens read so far, enough to classify it, and fails with an error wrapping
// dialectkit.ErrTokenBudget once it is applied or analysed, unless it is a statement
// ApplyDDL passes over.
//
// Takes sql (string) which is the SQL source to scan.
//
// Returns []ast.ParsedStatement which holds one entry per classified statement.
// Returns error when tokenisation fails, classifying panics, or the context is cancelled,
// giving the context's cause.
func (engine *Dialect) ParseStatements(ctx context.Context, sql string) (results []ast.ParsedStatement, err error) {
	defer func() { dialectkit.Recover(ctx, "mysql: parse statements", recover(), &results, &err) }()

	if cause := context.Cause(ctx); cause != nil {
		return nil, cause
	}
	statements, err := lexStatements(ctx, engine.settings.lexicalRules(), sql, engine.settings.Limits.TokensPerStatement())
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

// ApplyDDL applies a DDL statement to the catalogue for the MySQL dialect.
//
// A malformed statement is reported as a returned syntax error, as are tokens left after
// a complete statement, other than a closing semicolon, and a parenthesis never closed or
// closing nothing; only the body of a routine or a trigger runs to the end of the
// statement unread. A panic, in the parser or in a hook, fails the statement rather than
// the calling apply loop: it is recovered and returned as a dialectkit.PanicError, and
// its stack is logged once at Warn through the context's logger. It honours the context
// before dispatch, and the parser's walk stops once the context is done, so the catalogue
// build loop can be cancelled by the caller; it rejects a statement over the token budget
// before the parser walks it, limiting the work required by a single statement.
// Statements without a DDL handler are never walked and so never count against the
// budget.
//
// Takes statement (ast.ParsedStatement) which is the DDL statement to apply.
//
// Returns *ast.CatalogueMutation which describes the catalogue change, or nil when the
// statement kind produces no mutation.
// Returns error when the statement was parsed by another dialect (wrapping
// dialect.ErrForeignStatement), is malformed, the context is cancelled, the token budget
// is exceeded, the walk stops advancing, or the parser panics.
func (engine *Dialect) ApplyDDL(
	ctx context.Context,
	statement ast.ParsedStatement,
) (mutation *ast.CatalogueMutation, err error) {
	defer func() { dialectkit.Recover(ctx, "mysql: apply DDL", recover(), &mutation, &err) }()

	parsed, ok := statement.Raw.(*parsedStatement)
	if !ok {
		return nil, fmt.Errorf("%w: %T", dialect.ErrForeignStatement, statement.Raw)
	}

	if cause := context.Cause(ctx); cause != nil {
		return nil, cause
	}
	if engine.initErr != nil {
		return nil, engine.initErr
	}
	if runOnError := engine.settings.checkRunOn(ctx, parsed); runOnError != nil {
		return nil, runOnError
	}

	if int(parsed.kind) >= len(ddlHandlers) || ddlHandlers[parsed.kind] == nil {
		return nil, nil
	}
	if budgetError := engine.settings.checkTokenBudget(parsed); budgetError != nil {
		return nil, budgetError
	}
	if syntaxError := engine.rejectUnavailableSyntax(parsed.tokens); syntaxError != nil {
		return nil, syntaxError
	}

	p := engine.newStatementParser(dialectkit.WalkGuard(ctx, len(parsed.tokens)), parsed)

	mutation, err = ddlHandlers[parsed.kind](p, engine)
	if err == nil {
		err = p.requireStatementEnd()
	}
	if p.syntaxError != nil {
		return nil, p.syntaxError
	}
	if err != nil {
		return nil, err
	}
	if balanceError := unbalancedParenthesis(parsed.tokens); balanceError != nil {
		return nil, balanceError
	}
	return mutation, nil
}

// AnalyseQuery performs structural analysis of a DML statement for the MySQL dialect.
//
// The analysis must consume the whole statement: tokens left after a complete statement
// fail it with a dialectkit.ParseError naming the first of them, and so does a
// parenthesis never closed or closing nothing, so nothing is silently dropped. Syntax
// errors, including expressions nested past the depth cap, are returned as ordinary
// errors so the analyser reports them as diagnostics. A panic, in the analyser or in a
// hook, is recovered and returned as a dialectkit.PanicError, and its stack is logged
// once at Warn through the context's logger. A statement over the token budget, even one
// with nothing to analyse, is rejected before the parser walks it, and the walk stops
// once the context is done or the parser stops advancing, so a single statement cannot
// drive the analyser into a very long walk. A statement the engine runs that holds
// nothing to analyse, such as SET or TRUNCATE, yields an empty analysis; one opening with
// a word that starts no statement is rejected with a dialectkit.ParseError. The analysis
// of a statement the dialect reads records its conflict action, which is
// ast.ConflictActionNone for any statement but an INSERT or REPLACE that handles
// conflicting rows.
//
// Takes statement (ast.ParsedStatement) which is the DML statement to analyse.
//
// Returns *ast.RawQueryAnalysis which holds the structural analysis result.
// Returns error when the statement was parsed by another dialect (wrapping
// dialect.ErrForeignStatement), is malformed or over budget, the walk stops advancing,
// analysis panics, or the context is cancelled.
func (engine *Dialect) AnalyseQuery(
	ctx context.Context,
	_ *sqlmodel.Catalogue,
	statement ast.ParsedStatement,
) (analysis *ast.RawQueryAnalysis, err error) {
	defer func() { dialectkit.Recover(ctx, "mysql: analyse query", recover(), &analysis, &err) }()

	if cause := context.Cause(ctx); cause != nil {
		return nil, cause
	}
	parsed, ok := statement.Raw.(*parsedStatement)
	if !ok {
		return nil, fmt.Errorf("%w: %T", dialect.ErrForeignStatement, statement.Raw)
	}
	if engine.initErr != nil {
		return nil, engine.initErr
	}

	if parsed.overBudget {
		return nil, engine.settings.checkTokenBudget(parsed)
	}
	if int(parsed.kind) >= len(queryAnalysers) || queryAnalysers[parsed.kind] == nil {
		return analyseUnanalysedStatement(parsed)
	}
	if budgetError := engine.settings.checkTokenBudget(parsed); budgetError != nil {
		return nil, budgetError
	}
	if syntaxError := engine.rejectUnavailableSyntax(parsed.tokens); syntaxError != nil {
		return nil, syntaxError
	}

	p := engine.newStatementParser(dialectkit.WalkGuard(ctx, len(parsed.tokens)), parsed)
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
	if balanceError := unbalancedParenthesis(parsed.tokens); balanceError != nil {
		return nil, balanceError
	}
	engine.recordStatementSpans(p, analysis, parsed, statement)
	analysis.Warnings = p.sortedWarnings()
	if analysis.ConflictAction == ast.ConflictActionUnrecorded {
		analysis.ConflictAction = ast.ConflictActionNone
	}
	return analysis, nil
}

// BuiltinFunctions returns the MySQL built-in function catalogue.
//
// Returns *ast.FunctionCatalogue which lists the engine's resolved function definitions.
func (engine *Dialect) BuiltinFunctions() *ast.FunctionCatalogue {
	return engine.functions
}

// BuiltinTypes returns the MySQL built-in type catalogue.
//
// Returns *ast.TypeCatalogue which lists the engine's resolved type definitions.
func (engine *Dialect) BuiltinTypes() *ast.TypeCatalogue {
	return engine.types
}

// NormaliseTypeName resolves a raw type name to a structured SQLType.
//
// When no modifiers are passed, a list of one or two numbers written at the end of the
// name, as in decimal(10,2) or varchar(20), is read as the type's modifiers.
//
// Takes name (string) which is the raw type name from the SQL source.
// Takes modifiers (...int) which carry optional precision and scale modifiers parsed from
// the type expression.
//
// Returns sqlmodel.SQLType which describes the normalised type.
func (engine *Dialect) NormaliseTypeName(name string, modifiers ...int) sqlmodel.SQLType {
	if len(modifiers) == 0 {
		return engine.typeResolver.normaliseWritten(name)
	}
	return engine.typeResolver.normalise(name, modifiers...)
}

// TableValuedFunctionColumns returns output columns for a known TVF, or for the result of
// a SHOW statement the dialect declares, which its analysis reads as one.
//
// Takes functionName (string) which is the table-valued function name.
//
// Returns []ast.ScopedColumn which lists output columns, or nil when the function is not
// recognised.
func (engine *Dialect) TableValuedFunctionColumns(functionName string) []ast.ScopedColumn {
	if shown, isShow := showResults[functionName]; isShow {
		result := make([]ast.ScopedColumn, 0, len(shown))
		for _, column := range shown {
			result = append(result, ast.ScopedColumn{Name: column.name, SQLType: engine.NormaliseTypeName(column.typeName),
				Nullable: column.nullable})
		}
		return result
	}
	columns, exists := tableValuedFunctionColumns[functionName]
	if !exists {
		return nil
	}
	result := make([]ast.ScopedColumn, len(columns))
	copy(result, columns)
	return result
}

// TableValuedFunctionColumnsFromCatalogue resolves set-returning UDFs.
//
// Looks up the function signature and return type in the catalogue to recover column
// definitions for composite or set-of return types. An unqualified name is looked up
// through the schemas on the catalogue's search path, in order, and a name qualified as
// schema.name in that schema alone, so the first schema on the path that defines the
// function decides its columns. Missing catalogue entries are passed over. A panic is
// recovered and returned as a dialectkit.PanicError, and its stack is logged once at Warn
// through the context's logger.
//
// Takes catalogue (*sqlmodel.Catalogue) which is the schema catalogue to search; nil
// gives no columns.
// Takes functionName (string) which is the table-valued function name, bare or qualified
// by its schema.
//
// Returns []ast.ScopedColumn which lists output columns, or nil when no matching
// set-returning signature is found.
// Returns error which is non-nil only when the lookup panics.
func (*Dialect) TableValuedFunctionColumnsFromCatalogue(
	ctx context.Context,
	catalogue *sqlmodel.Catalogue,
	functionName string,
) (columns []ast.ScopedColumn, err error) {
	defer func() {
		dialectkit.Recover(ctx, "mysql: table-valued function columns", recover(), &columns, &err)
	}()

	schemaName, bareName := splitQualifiedFunctionName(functionName)
	for schema, signatures := range catalogue.FunctionsOnPath(schemaName, bareName) {
		for _, signature := range signatures {
			if signature == nil || !signature.ReturnsSet {
				continue
			}
			if resolved := resolveCompositeColumns(catalogue, schema, signature.ReturnType); resolved != nil {
				return resolved, nil
			}
		}
	}
	return nil, nil
}

// PromoteType consults promotion hooks, then widens integer or float types within the
// same category, and across numeric categories prefers a float, then a decimal. Other
// pairs keep the left type.
//
// Takes left (sqlmodel.SQLType) which is the left operand type.
// Takes right (sqlmodel.SQLType) which is the right operand type.
//
// Returns sqlmodel.SQLType which is the promoted type for the pair.
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

// CanImplicitCast reports whether MySQL permits an implicit type conversion.
//
// It includes conversions used by function arguments while preserving MySQL's rules for
// mixed-type expressions.
//
// Takes from (sqlmodel.SQLType) which is the source type.
// Takes to (sqlmodel.SQLType) which is the destination type.
//
// Returns bool which is true when MySQL allows the implicit conversion.
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
	case to.Category == sqlmodel.TypeCategoryText && convertsToString(from.Category):
		return true
	case from.Category == sqlmodel.TypeCategoryText && to.Category == sqlmodel.TypeCategoryJSON:
		return true
	default:
		return false
	}
}

// CoerceArgument recognises MySQL's date and time function-argument conversions.
//
// Invalid date values can produce NULL, so these conversions may make the result
// nullable.
//
// Takes argument (dialect.CallArgument) which is the value the call passes; MySQL
// converts a computed value as it does a literal.
// Takes parameter (sqlmodel.SQLType) which is the type the function's parameter declares.
//
// Returns dialect.ArgumentCoercion which says the call may be NULL.
// Returns bool which is true when MySQL converts the argument.
func (*Dialect) CoerceArgument(argument dialect.CallArgument, parameter sqlmodel.SQLType) (dialect.ArgumentCoercion, bool) {
	if parameter.Category != sqlmodel.TypeCategoryTemporal {
		return dialect.ArgumentCoercion{}, false
	}
	switch argument.Type.Category {
	case sqlmodel.TypeCategoryText, sqlmodel.TypeCategoryInteger, sqlmodel.TypeCategoryDecimal, sqlmodel.TypeCategoryFloat:
		return dialect.ArgumentCoercion{MayBeNull: true}, true
	default:
		return dialect.ArgumentCoercion{}, false
	}
}

// CoreTypes returns MySQL's core types: BOOLEAN, TEXT, BIGINT for counts, JSON, INT for a
// LIMIT or OFFSET parameter, and no implicit row identifier.
//
// Returns dialect.CoreTypes which holds the core types.
func (engine *Dialect) CoreTypes() dialect.CoreTypes {
	return engine.coreTypes()
}

// BinaryResult types the MySQL operators whose result differs from the common type of
// their operands: - between two temporal values, /, DIV, the bit operators and the JSON
// arrows.
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

// ResolveFunctionCall resolves a function call using MySQL overload rules.
//
// Only registered signatures with a matching argument count are considered. Custom
// function-call hooks run first. A panic, in a hook or in resolution, is recovered and
// returned as a dialectkit.PanicError, and its stack is logged once at Warn through the
// context's logger.
//
// Takes catalogue (*sqlmodel.Catalogue) which is the schema catalogue to search.
// Takes name (string) which is the function name to resolve.
// Takes schema (string) which scopes the lookup, or empty for any schema.
// Takes argumentTypes ([]sqlmodel.SQLType) which carries the actual argument types
// provided at the call site.
//
// Returns *ast.FunctionResolution which describes the chosen signature, or nil when no
// overload matches or the catalogue does not register the function.
// Returns error when resolution fails or panics.
func (engine *Dialect) ResolveFunctionCall(
	ctx context.Context,
	catalogue *sqlmodel.Catalogue,
	name string,
	schema string,
	argumentTypes []sqlmodel.SQLType,
) (resolution *ast.FunctionResolution, err error) {
	defer func() { dialectkit.Recover(ctx, "mysql: resolve function call", recover(), &resolution, &err) }()

	lowered := strings.ToLower(name)
	if engine.functions == nil || !slices.ContainsFunc(engine.functions.Functions[lowered], takesArguments(len(argumentTypes))) {
		return nil, nil
	}
	if hooked, answered := dialectkit.FirstHookAnswer(engine.settings.FunctionCallHooks,
		func(hook func(name string, argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution) *ast.FunctionResolution {
			return hook(lowered, argumentTypes)
		}); answered {
		return &hooked, nil
	}
	return newFunctionResolver().ResolveFunctionCall(catalogue, name, schema, argumentTypes)
}

// operatorFeatures returns the operators the dialect has and the expression feature each
// needs, leaving out the -> and ->> JSON operators when the engine lacks them, so neither
// the analyser nor the runtime query builder offers them.
//
// Returns map[string]ast.ExpressionFeature which maps each operator to its feature.
func (engine *Dialect) operatorFeatures() map[string]ast.ExpressionFeature {
	if !engine.settings.JSONArrowsUnavailable {
		return operatorFeatures
	}
	features := maps.Clone(operatorFeatures)
	delete(features, operatorJSONExtract)
	delete(features, operatorJSONUnquote)
	return features
}

// newStatementParser returns a parser over a statement's tokens, configured with the
// dialect's limits, types and capabilities. Its walk, and the walks of the parsers it
// starts for nested constructs, which share the interrupt and so one budget for the whole
// statement, stop when the interrupt returns an error.
//
// Takes interrupt (func() error) which is the statement's dialectkit.WalkGuard.
// Takes parsed (*parsedStatement) which is the classified statement.
//
// Returns *parser which is ready to walk the statement.
func (engine *Dialect) newStatementParser(interrupt func() error, parsed *parsedStatement) *parser {
	p := newParser(parsed.tokens)
	p.Interrupt = interrupt
	p.source = parsed.source
	p.maxParseDepth = engine.settings.Limits.ParseDepth()
	p.types = engine.typeResolver
	p.functions = engine.functions
	p.rowAliasUnavailable = engine.settings.InsertRowAliasUnavailable
	p.jsonValueReturningUnavailable = engine.settings.JSONValueReturningUnavailable
	p.sequences = engine.settings.Sequences
	p.offsetFetch = engine.settings.OffsetFetch
	p.createOrReplace = engine.settings.CreateOrReplace
	return p
}

// rejectUnavailableSyntax reports syntax disabled or unsupported by the selected engine.
//
// The first offending token is reported, preventing SQL that the server would reject.
//
// Takes tokens ([]token) which is the statement's token stream.
//
// Returns error which is a dialectkit.ParseError at the first unavailable token, or nil.
func (engine *Dialect) rejectUnavailableSyntax(tokens []token) error {
	for _, current := range tokens {
		switch {
		case current.Kind == tokenLeftBracket || current.Kind == tokenRightBracket:
			return dialectkit.NewParseError(current, "MySQL has no array subscript or bracket syntax")
		case engine.settings.JSONArrowsUnavailable && (current.Kind == tokenArrow || current.Kind == tokenDoubleArrow):
			return dialectkit.NewParseError(current, "the "+current.Value+" JSON operator is not available in this dialect; "+
				"use JSON_EXTRACT, or JSON_UNQUOTE(JSON_EXTRACT(...)) for ->>")
		}
	}
	return nil
}

// settings holds configuration for a MySQL variant. Flavours such as MariaDB add their
// own name, functions, types, hooks and capabilities to it through the options, without
// forking the parser.
//
// Every option that registers something adds to what earlier options registered, so a
// flavour's own settings survive a caller's options of the same kind.
type settings struct {
	// ExtraTypes holds the extra type definitions, keyed by lower-cased name. Each
	// WithExtraTypes merges into it, a later entry replacing an earlier one of the same
	// name.
	ExtraTypes map[string]sqlmodel.SQLType

	// Name is the dialect name Info reports. It defaults to "mysql"; flavours such as
	// mariadb set it through WithName.
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

	// FunctionCallHooks holds the ResolveFunctionCall hooks in registration order. The most
	// recently added hook is consulted first.
	FunctionCallHooks []func(name string, argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution

	// Limits caps how deeply a statement may nest, through analysis and expression parsing,
	// and how many tokens it may hold; a zero limit selects dialectkit's default.
	Limits dialectkit.Limits

	// Returning is the set of statements that accept a RETURNING clause; it is empty unless
	// WithReturning sets it.
	Returning ast.DataChange

	// JSONArrowsUnavailable records that the engine lacks the -> and ->> JSON operators, so
	// a statement using them is rejected rather than generating SQL the server refuses.
	JSONArrowsUnavailable bool

	// InsertRowAliasUnavailable records that the engine lacks the row alias of INSERT ...
	// VALUES (...) AS alias, so a statement using it is rejected.
	InsertRowAliasUnavailable bool

	// JSONValueReturningUnavailable records that the engine's JSON_VALUE lacks the RETURNING
	// clause, so a call using it is rejected.
	JSONValueReturningUnavailable bool

	// UngroupedColumns is how the engine treats a column read outside every aggregate of a
	// query that aggregates without GROUP BY. MySQL's default mode, with ONLY_FULL_GROUP_BY,
	// rejects the query, so it is dialect.UngroupedColumnsWarned unless WithUngroupedColumns
	// sets it.
	UngroupedColumns dialect.UngroupedColumnRule

	// ANSIQuotes records that the server's sql_mode holds ANSI_QUOTES, which reads "..." as
	// a quoted identifier rather than a string.
	ANSIQuotes bool

	// Sequences records that the engine has sequences: NEXT VALUE FOR and PREVIOUS VALUE
	// FOR, and the NEXTVAL, LASTVAL and SETVAL functions, whose first argument names a
	// sequence rather than a column.
	Sequences bool

	// OffsetFetch records that the engine takes the standard row limit, OFFSET n ROWS and
	// FETCH {FIRST | NEXT} n ROWS {ONLY | WITH TIES}, beside LIMIT.
	OffsetFetch bool

	// CreateOrReplace records that a CREATE of a table, index, database or trigger may say
	// OR REPLACE, replacing an object of the same name.
	CreateOrReplace bool
}

// lexicalRules returns the engine's lexical rules for its SQL mode: "..." is a string
// unless the mode holds ANSI_QUOTES.
//
// Returns sqllex.Rules which describes the engine.
func (d *settings) lexicalRules() sqllex.Rules {
	if d.ANSIQuotes {
		return lexical.ANSIQuotesRules()
	}
	return lexical.Rules()
}

// checkRunOn detects adjacent migration statements with a missing semicolon.
//
// Routines, triggers, events and unclassified statements are skipped because they may
// contain nested statements. Statements over the token budget are scanned again to the
// end, one token at a time in constant memory. This catches missing semicolons even
// beyond the budget in large INSERT seeds.
//
// Takes parsed (*parsedStatement) which is the statement.
//
// Returns error which locates the statement that runs on, the context's cause, or the
// tokeniser's error for a part of the statement read past the budget; nil otherwise.
func (d *settings) checkRunOn(ctx context.Context, parsed *parsedStatement) error {
	switch parsed.kind {
	case statementKindCreateFunction, statementKindCreateTrigger, statementKindUnknown:
		return nil
	default:
	}
	rules := runOnRules
	if parsed.kind == statementKindAlterTable {
		rules = alterTableRunOnRules
	}
	next, found, err := d.findRunOn(ctx, parsed, rules)
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
func (d *settings) findRunOn(
	ctx context.Context,
	parsed *parsedStatement,
	rules dialectkit.RunOnRules[tokenKind],
) (token, bool, error) {
	if !parsed.overBudget {
		next, found := dialectkit.RunOnStatement(parsed.source, parsed.tokens, rules)
		return next, found, nil
	}
	read := statementTokeniser(d.lexicalRules().Quotes.DoubleQuote)(parsed.source, parsed.span.Start, parsed.span.End)
	return dialectkit.RunOnStream(ctx, parsed.source, read, tokenEOF, rules)
}

// checkTokenBudget rejects a statement holding more tokens than the dialect's budget,
// including one whose tokenising stopped at a budget, whose full count is not known: it
// holds more than the tokens it kept, and more than this dialect's budget when that is
// smaller.
//
// Takes parsed (*parsedStatement) which is the statement.
//
// Returns error wrapping dialectkit.ErrTokenBudget when the statement is over budget.
func (d *settings) checkTokenBudget(parsed *parsedStatement) error {
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
// dialect. The family stays mysql, so type mappings and drivers keep resolving.
//
// Takes name (string) which is the dialect name to report.
//
// Returns Option which applies the name.
func WithName(name string) Option {
	return func(configured *settings) {
		configured.Name = name
	}
}

// WithExtraTypes adds named type definitions to the MySQL dialect.
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

// WithExtraFunctions registers additional functions after the built-in MySQL function
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

// WithFunctionCallHook adds a hook for resolving built-in function results.
//
// Hooks run newest first for registered signatures with matching argument counts. The
// first non-nil result takes precedence over built-in rules.
//
// Takes hook (func(name string, argumentTypes []sqlmodel.SQLType)
// *ast.FunctionResolution) which receives the lower-cased function name and the call's
// argument types, each marked nullable when the argument may be NULL, and returns the
// resolution, or nil to decline.
//
// Returns Option which adds the hook.
func WithFunctionCallHook(hook func(name string, argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution) Option {
	return func(configured *settings) {
		if hook != nil {
			configured.FunctionCallHooks = append(configured.FunctionCallHooks, hook)
		}
	}
}

// WithReturning sets the statements that accept a RETURNING clause, replacing any set
// before. MySQL accepts it on none, which is the default; a flavour that accepts it on
// some, as MariaDB does on INSERT and DELETE, names them.
//
// Takes statements (ast.DataChange) which is the set of statements, or zero for none.
//
// Returns Option which installs the set on a dialect.
func WithReturning(statements ast.DataChange) Option {
	return func(configured *settings) {
		configured.Returning = statements
	}
}

// WithJSONArrows sets whether the engine has the -> and ->> JSON operators.
//
// MySQL has them and they are on by default; a flavour without them, such as MariaDB,
// turns them off so a statement using them fails with a syntax error naming the operator
// instead of generating SQL the server rejects.
//
// Takes supported (bool) which records whether the operators are available.
//
// Returns Option which applies the capability.
func WithJSONArrows(supported bool) Option {
	return func(configured *settings) {
		configured.JSONArrowsUnavailable = !supported
	}
}

// WithInsertRowAlias controls whether INSERT VALUES accepts a row alias.
//
// It is enabled by default. Dialects without this MySQL syntax disable it so unsupported
// statements fail during analysis.
//
// Takes supported (bool) which records whether the row alias is available.
//
// Returns Option which applies the capability.
func WithInsertRowAlias(supported bool) Option {
	return func(configured *settings) {
		configured.InsertRowAliasUnavailable = !supported
	}
}

// WithJSONValueReturning sets whether the engine's JSON_VALUE takes a RETURNING clause
// naming the type of its result, which MySQL 8.0.21 added.
//
// It is on by default; a flavour without it, such as MariaDB, turns it off so a call
// using it fails with a syntax error instead of generating SQL the server rejects.
//
// Takes supported (bool) which records whether the clause is available.
//
// Returns Option which applies the capability.
func WithJSONValueReturning(supported bool) Option {
	return func(configured *settings) {
		configured.JSONValueReturningUnavailable = !supported
	}
}

// WithUngroupedColumns sets how the engine treats a column a query reads outside every
// aggregate when the query aggregates without GROUP BY, as dialect.Info.UngroupedColumns
// describes.
//
// MySQL rejects such a query in its default mode, whose sql_mode holds
// ONLY_FULL_GROUP_BY, and returns one row otherwise, so the default is
// dialect.UngroupedColumnsWarned; a flavour whose default mode accepts the query, such as
// MariaDB, sets dialect.UngroupedColumnsNullable.
//
// Takes rule (dialect.UngroupedColumnRule) which is how the engine treats the column.
//
// Returns Option which applies the rule.
func WithUngroupedColumns(rule dialect.UngroupedColumnRule) Option {
	return func(configured *settings) {
		configured.UngroupedColumns = rule
	}
}

// WithANSIQuotes sets whether the server runs with ANSI_QUOTES in its sql_mode.
//
// MySQL's and MariaDB's default mode reads "..." as a string, as in CONCAT(a, ":", b), so
// the dialect does too unless this option turns ANSI_QUOTES on, which reads "..." as a
// quoted identifier, as standard SQL does.
//
// Takes enabled (bool) which records whether the mode holds ANSI_QUOTES.
//
// Returns Option which applies the mode.
func WithANSIQuotes(enabled bool) Option {
	return func(configured *settings) {
		configured.ANSIQuotes = enabled
	}
}

// WithSequences enables sequence expressions and NEXTVAL, LASTVAL and SETVAL calls, as
// MariaDB supports them. It reads sequence syntax; the flavour registers function
// signatures through WithExtraFunctions, and the MySQL default disables sequences.
//
// Takes supported (bool) which records whether the engine has sequences.
//
// Returns Option which applies the capability.
func WithSequences(supported bool) Option {
	return func(configured *settings) {
		configured.Sequences = supported
	}
}

// WithOffsetFetch sets whether the engine takes the standard row limit, OFFSET n ROWS and
// FETCH {FIRST | NEXT} n ROWS {ONLY | WITH TIES}, beside LIMIT, as MariaDB does. MySQL
// takes LIMIT alone, which is the default.
//
// Takes supported (bool) which records whether the engine takes the standard form.
//
// Returns Option which applies the capability.
func WithOffsetFetch(supported bool) Option {
	return func(configured *settings) {
		configured.OffsetFetch = supported
	}
}

// WithCreateOrReplace permits OR REPLACE for tables, indexes, databases and triggers,
// dropping an existing object before creating its replacement. The MySQL default permits
// OR REPLACE only for views.
//
// Takes supported (bool) which records whether the engine takes the form.
//
// Returns Option which applies the capability.
func WithCreateOrReplace(supported bool) Option {
	return func(configured *settings) {
		configured.CreateOrReplace = supported
	}
}

// WithMaxParseDepth sets the maximum parser recursion depth for analysis and expression
// nesting.
//
// Deeply nested user input is otherwise able to overflow the goroutine stack with a
// fatal, non-recoverable error. The default is high (dialectkit.DefaultMaxParseDepth) so
// realistic queries are unaffected; lower it to harden against hostile input or raise it,
// up to dialectkit.MaxParseDepthCeiling, for unusually nested generated queries.
// Exceeding it fails the statement with an error wrapping dialectkit.ErrParseDepth.
//
// Takes depth (int) which is the maximum nesting depth; values below 1 are ignored so the
// default remains in force.
//
// Returns Option which installs the depth cap on a dialect.
func WithMaxParseDepth(depth int) Option {
	return func(configured *settings) {
		configured.Limits = configured.Limits.WithMaxParseDepth(depth)
	}
}

// WithMaxTokensPerStatement sets the maximum number of tokens a single statement may
// contain before the parser refuses to walk it.
//
// Some nested shapes cost more than linear time, so the token budget bounds the work
// required by hostile or runaway input. The default is high
// (dialectkit.DefaultMaxTokensPerStatement) so realistic queries, including generated
// ones with large IN lists, are unaffected; lower it to harden against hostile input or
// raise it, up to dialectkit.MaxTokensPerStatementCeiling, for unusually large generated
// statements. Tokenising a statement stops at the budget, and a statement past it fails
// with an error wrapping dialectkit.ErrTokenBudget once it is applied or analysed.
//
// Takes limit (int) which is the maximum token count; values below 1 are ignored so the
// default remains in force.
//
// Returns Option which installs the token budget on a dialect.
func WithMaxTokensPerStatement(limit int) Option {
	return func(configured *settings) {
		configured.Limits = configured.Limits.WithMaxTokensPerStatement(limit)
	}
}

// splitQualifiedFunctionName splits a function name qualified as schema.name into its
// schema and its bare name.
//
// Takes functionName (string) which is the bare or qualified name.
//
// Returns schema (string) which is the schema, or empty for a bare name.
// Returns name (string) which is the bare name.
func splitQualifiedFunctionName(functionName string) (schema string, name string) {
	before, after, qualified := strings.Cut(functionName, ".")
	if !qualified {
		return "", functionName
	}
	return before, after
}

// resolveCompositeColumns expands a composite return type into columns.
//
// Takes catalogue (*sqlmodel.Catalogue) which carries the schemas to consult for
// composite type lookups.
// Takes declaringSchema (*sqlmodel.Schema) which is the schema declaring the calling
// function.
// Takes returnType (sqlmodel.SQLType) which is the composite return type to expand.
//
// Returns []ast.ScopedColumn which lists one entry per composite field, or nil when the
// type is not a known composite or a catalogue entry it needs is missing.
func resolveCompositeColumns(
	catalogue *sqlmodel.Catalogue,
	declaringSchema *sqlmodel.Schema,
	returnType sqlmodel.SQLType,
) []ast.ScopedColumn {
	typeName := returnType.EngineName
	if typeName == "" || declaringSchema == nil {
		return nil
	}

	schema := declaringSchema
	if returnType.Schema != "" && returnType.Schema != declaringSchema.Name {
		if targetSchema := catalogue.Schemas[returnType.Schema]; targetSchema != nil {
			schema = targetSchema
		}
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

// convertsToString reports whether MySQL converts a value of a category to a string where
// a string is expected: a string, a number, or a date and time.
//
// Takes category (sqlmodel.SQLTypeCategory) which is the value's category.
//
// Returns bool which is true when the value converts.
func convertsToString(category sqlmodel.SQLTypeCategory) bool {
	switch category {
	case sqlmodel.TypeCategoryText, sqlmodel.TypeCategoryInteger, sqlmodel.TypeCategoryDecimal,
		sqlmodel.TypeCategoryFloat, sqlmodel.TypeCategoryTemporal:
		return true
	default:
		return false
	}
}

// takesArguments returns a test of whether an overload takes a number of arguments.
//
// Takes count (int) which is the number of arguments a call passes.
//
// Returns func(*sqlmodel.Function) bool which reports whether an overload takes them.
func takesArguments(count int) func(*sqlmodel.Function) bool {
	return func(overload *sqlmodel.Function) bool {
		return count >= overload.RequiredArguments() && (overload.IsVariadic || count <= len(overload.Arguments))
	}
}

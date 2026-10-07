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
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// minTokensForCreateOrReplace is the minimum token count needed to recognise a CREATE OR
	// REPLACE prefix.
	minTokensForCreateOrReplace = 4

	// indexAfterOrReplace is the token index immediately following the CREATE OR REPLACE
	// prefix.
	indexAfterOrReplace = 3

	// schemaQualifiedCallLookahead is the offset from a schema identifier to the opening
	// parenthesis of a schema-qualified call (schema . name (), so the '(' sits three tokens
	// past the schema token.
	schemaQualifiedCallLookahead = 3

	// maxArrayDimensions caps the bracket suffixes an array type may declare. DuckDB nests
	// lists arbitrarily, but six dimensions is far beyond realistic schemas and keeps a
	// hostile `[][][]...` suffix from building ever-deeper wrapped types.
	maxArrayDimensions = 6

	// maxDollarParameterNumber is the highest positional parameter number a $N placeholder
	// may carry. It matches the PostgreSQL wire-protocol limit the other engines share and
	// keeps a pathologically long digit run from overflowing.
	maxDollarParameterNumber = 65535

	// minimumStructExtractTokens is the smallest token offset from the opening parenthesis
	// of struct_extract(struct, 'field') to its closing one: four tokens, spanning the
	// opening parenthesis, the struct, the comma and the field name.
	minimumStructExtractTokens = 4

	// minimumStructInsertArguments is the fewest arguments a struct_insert call that adds a
	// field takes: the struct and one field.
	minimumStructInsertArguments = 2
)

// statementKind tags a parsed SQL statement with its top-level category.
type statementKind uint8

const (
	// statementKindCreateTable identifies CREATE TABLE statements.
	statementKindCreateTable statementKind = iota

	// statementKindDropTable identifies DROP TABLE statements.
	statementKindDropTable

	// statementKindAlterTable identifies ALTER TABLE statements.
	statementKindAlterTable

	// statementKindCreateView identifies CREATE VIEW statements.
	statementKindCreateView

	// statementKindDropView identifies DROP VIEW statements.
	statementKindDropView

	// statementKindCreateIndex identifies CREATE INDEX statements.
	statementKindCreateIndex

	// statementKindDropIndex identifies DROP INDEX statements.
	statementKindDropIndex

	// statementKindCreateType identifies CREATE TYPE statements.
	statementKindCreateType

	// statementKindAlterType identifies ALTER TYPE statements.
	statementKindAlterType

	// statementKindDropType identifies DROP TYPE statements.
	statementKindDropType

	// statementKindCreateFunction identifies CREATE FUNCTION statements.
	statementKindCreateFunction

	// statementKindDropFunction identifies DROP FUNCTION statements.
	statementKindDropFunction

	// statementKindCreateMacro identifies CREATE MACRO statements.
	statementKindCreateMacro

	// statementKindDropMacro identifies DROP MACRO statements.
	statementKindDropMacro

	// statementKindCreateSchema identifies CREATE SCHEMA statements.
	statementKindCreateSchema

	// statementKindDropSchema identifies DROP SCHEMA statements.
	statementKindDropSchema

	// statementKindCreateSequence identifies CREATE SEQUENCE statements.
	statementKindCreateSequence

	// statementKindDropSequence identifies DROP SEQUENCE statements.
	statementKindDropSequence

	// statementKindAlterSequence identifies ALTER SEQUENCE statements.
	statementKindAlterSequence

	// statementKindAlterView identifies ALTER VIEW statements.
	statementKindAlterView

	// statementKindComment identifies COMMENT ON statements.
	statementKindComment

	// statementKindInstall identifies INSTALL extension statements.
	statementKindInstall

	// statementKindLoad identifies LOAD extension statements.
	statementKindLoad

	// statementKindSelect identifies SELECT statements.
	statementKindSelect

	// statementKindInsert identifies INSERT statements.
	statementKindInsert

	// statementKindUpdate identifies UPDATE statements.
	statementKindUpdate

	// statementKindDelete identifies DELETE statements.
	statementKindDelete

	// statementKindValues identifies bare VALUES statements.
	statementKindValues

	// statementKindUnknown marks statements the classifier could not name.
	statementKindUnknown

	// statementKindCount is the sentinel one past the last statement kind.
	statementKindCount
)

var (
	// firstWordClassifiers maps a leading keyword to a function that classifies the
	// statement based on further tokens.
	firstWordClassifiers = map[string]func([]token) statementKind{
		keywordCREATE: classifyCreateStatement,
		keywordDROP:   classifyDropStatement,
		"ALTER":       classifyAlterStatement,
		keywordWITH:   classifyWithStatement,
	}

	// firstWordStaticKinds maps leading keywords whose statement kind is fully determined
	// without inspecting further tokens.
	firstWordStaticKinds = map[string]statementKind{
		keywordSELECT:      statementKindSelect,
		keywordFROM:        statementKindSelect,
		keywordTABLE:       statementKindSelect,
		keywordPIVOT:       statementKindSelect,
		keywordPIVOTWIDER:  statementKindSelect,
		keywordUNPIVOT:     statementKindSelect,
		keywordPIVOTLONGER: statementKindSelect,
		"INSERT":           statementKindInsert,
		"UPDATE":           statementKindUpdate,
		"DELETE":           statementKindDelete,
		keywordVALUES:      statementKindValues,
		keywordINSTALL:     statementKindInstall,
		keywordLOAD:        statementKindLoad,
		"COMMENT":          statementKindComment,
	}

	// createObjectKinds maps the keyword following CREATE (and any prefixes) to the matching
	// statement kind.
	createObjectKinds = map[string]statementKind{
		keywordTABLE:  statementKindCreateTable,
		"VIEW":        statementKindCreateView,
		"INDEX":       statementKindCreateIndex,
		keywordUNIQUE: statementKindCreateIndex,
		keywordTYPE:   statementKindCreateType,
		"FUNCTION":    statementKindCreateFunction,
		keywordMACRO:  statementKindCreateMacro,
		keywordSCHEMA: statementKindCreateSchema,
		"SEQUENCE":    statementKindCreateSequence,
	}

	// dmlKeywords maps DML-introducing keywords to their statement kind.
	dmlKeywords = map[string]statementKind{
		keywordSELECT: statementKindSelect,
		"INSERT":      statementKindInsert,
		"UPDATE":      statementKindUpdate,
		"DELETE":      statementKindDelete,
		keywordVALUES: statementKindValues,
	}
)

// parsedStatement holds the tokens and classified kind for one SQL statement extracted
// from a multi-statement script.
type parsedStatement struct {
	// source is the script the statement was read from, so source text can be sliced from it
	// exactly as written.
	source string

	// tokens are the lexical tokens that make up this statement, in order.
	tokens []token

	// start is the byte offset in source of the statement's first token.
	start int

	// end is the byte offset in source just past the statement's last token, so a check can
	// read every token of a statement over the token budget again.
	end int

	// kind is the classified statement category.
	kind statementKind

	// overBudget is true for a statement holding more tokens than the dialect's budget,
	// whose tokens stop where the budget was reached.
	overBudget bool
}

// IsParsedStatement marks the type as a parsed statement for interface dispatch.
func (*parsedStatement) IsParsedStatement() {}

// argumentOrdinalCursor records how far the argument-ordinal scan of one call's argument
// list has progressed, so the scan for the call's next placeholder resumes there.
type argumentOrdinalCursor struct {
	// position is the index of the next token to examine.
	position int

	// depth is the parenthesis and bracket nesting relative to the call's opening
	// parenthesis.
	depth int

	// ordinal is the number of top-level commas seen so far.
	ordinal int
}

// statementState holds what every parser over one statement shares: the first syntax
// error found while walking it, and the string literal keys of its subscripts.
//
// The expression chain returns expressions rather than errors, so a malformed token
// stream found deep inside it is recorded here instead; the parser and every child parser
// spawned for the same statement share one state, and ApplyDDL or AnalyseQuery return the
// recorded error.
type statementState struct {
	// firstSyntaxError is the earliest recorded syntax error, or nil when none was recorded.
	firstSyntaxError error

	// warnings maps the offset of the token each warning is about to the warning, so a
	// clause parsed twice warns once; nil until first used.
	warnings map[int]ast.Warning

	// tokens are the tokens of the statement, which every parser sharing the state walks
	// part of.
	tokens []token

	// highestNumberedPlaceholder is the highest number a numbered placeholder, such as $2 or
	// ?2, is written with anywhere in the statement, so a named placeholder is numbered
	// after it, wherever the two are written.
	highestNumberedPlaceholder int

	// lambdas records whether the statement holds anything that may declare a lambda or list
	// comprehension name, once mayHoldLambdas has looked.
	lambdas lambdaPresence
}

// newStatementState returns the state of a statement whose tokens are given.
//
// Takes tokens ([]token) which are the statement's tokens.
//
// Returns *statementState which is empty but for the tokens.
func newStatementState(tokens []token) *statementState {
	return &statementState{
		firstSyntaxError:           nil,
		warnings:                   nil,
		tokens:                     tokens,
		highestNumberedPlaceholder: 0,
		lambdas:                    lambdaPresenceUnknown,
	}
}

// warn records a warning about the SQL a token starts, once however often the token is
// parsed.
//
// Takes at (token) which is the token the warning is about.
// Takes message (string) which says how the engine reads it and how to write it instead.
func (state *statementState) warn(at token, message string) {
	if state.warnings == nil {
		state.warnings = make(map[int]ast.Warning)
	}
	state.warnings[at.Start] = ast.Warning{Message: message, Span: ast.Span{Start: at.Start, End: at.End}}
}

// sortedWarnings returns the warnings recorded for the statement, in source order.
//
// Returns []ast.Warning which holds the warnings, or nil when there are none.
func (state *statementState) sortedWarnings() []ast.Warning {
	if len(state.warnings) == 0 {
		return nil
	}
	offsets := slices.Sorted(maps.Keys(state.warnings))
	warnings := make([]ast.Warning, 0, len(offsets))
	for _, offset := range offsets {
		warnings = append(warnings, state.warnings[offset])
	}
	return warnings
}

// parser holds the state required to walk a flat token stream into the
// internal/dialect/ast values the analyser reads.
//
// Syntax errors are returned where the parse function returns an error and otherwise
// recorded on the shared statement state, so a malformed statement never panics. A walk
// its Interrupt stops, or a parser bug that panics, unwinds to the engine's entry point,
// which fails the statement with it.
type parser struct {
	// types resolves type names in casts through the dialect's hooks, extra types and
	// built-in types; nil resolves through the built-in DuckDB types alone.
	types *typeResolver

	// functions is the dialect's built-in function catalogue, which types a parameter
	// compared with a built-in call; nil leaves such a parameter typed from its column.
	functions *ast.FunctionCatalogue

	// catalogue is the schema a query is analysed against, which lists the columns of a
	// table a PIVOT or UNPIVOT reads; nil when parsing DDL.
	catalogue *sqlmodel.Catalogue

	// source is the script the statement's tokens were read from, so captured text can be
	// sliced from it exactly as written; empty when the tokens were built without it.
	source string

	// parenthesisIndex answers the enclosing-parenthesis and enclosing-LIKE lookups the
	// parameter-context scans make; nil until first used.
	parenthesisIndex *dialectkit.ParenthesisScanIndex

	// lambdaScopes answers which lambdas and list comprehensions declare a name at each
	// token; nil until first used.
	lambdaScopes *lambdaScopeIndex

	// statement records the first syntax error found by a parse function that cannot return
	// one, and the statement's subscript keys; child parsers for the same statement share
	// it.
	statement *statementState

	// argumentCursors maps a call's opening-parenthesis index to the progress of its
	// argument-ordinal scan; nil until first used.
	argumentCursors map[int]argumentOrdinalCursor

	// namedParameterMap maps a named parameter's identifier to the sequential number it was
	// first assigned.
	namedParameterMap map[string]int

	// placeholderNumbers preserves assigned placeholder numbers when shared child parsers
	// revisit the same statement.
	placeholderNumbers map[int]int

	// scannedSubqueries maps the source offset of the parenthesis opening each subquery a
	// clause scan analysed to its analysis, shared with child parsers, so a parser that
	// records the clause's expressions without analysing its subqueries again still gives
	// each subquery its query.
	scannedSubqueries map[int]*ast.RawQueryAnalysis

	// insertProjectionTable is the INSERT target table whose columns the current INSERT ...
	// SELECT projection items are assigned to.
	//
	// It is empty when not parsing such a projection list.
	insertProjectionTable string

	// parameterReferences collects every bind parameter discovered while walking the
	// statement.
	parameterReferences []ast.RawParameterReference

	// rawDerivedTables holds derived table references observed in FROM clauses for later
	// analysis.
	rawDerivedTables []ast.RawDerivedTableReference

	// predicateSubqueries collects subqueries found in a token-scanned predicate position
	// (WHERE / HAVING / JOIN ON), so the analyser can resolve each one's parameters against
	// the subquery's own scope rather than the parent.
	predicateSubqueries []*ast.RawQueryAnalysis

	// rawTableValuedFunctions holds table-valued function references observed in FROM
	// clauses for later analysis.
	rawTableValuedFunctions []ast.RawTableValuedFunctionReference

	// fromOrder receives the names the items of the FROM clause being parsed are visible
	// under, in order; nil outside a FROM clause.
	fromOrder *[]string

	// argumentNames maps the number of each parameter a call passes by name to that name.
	argumentNames map[int]string

	// spans records the spans of the top-level statement; it is nil in every other parser,
	// so a nested query's clauses are never recorded as the statement's.
	spans *dialectkit.SpanRecorder

	// clauseExpressions collects the expressions of the clauses parsed so far whose query
	// has not taken them yet.
	clauseExpressions []ast.ClauseExpression

	// itemSpans holds the spans of the items of the select list or RETURNING clause parsed
	// last.
	itemSpans []ast.ItemSpan

	// insertProjectionColumns is the ordered INSERT target column list, active only while
	// the INSERT ... SELECT body's projection list is parsed.
	//
	// It is nil otherwise.
	insertProjectionColumns []string

	// Cursor walks the statement's tokens; its keyword matching never matches a quoted
	// identifier.
	dialectkit.Cursor[tokenKind]

	// parameterCount tracks the highest parameter number assigned so far.
	parameterCount int

	// analysisDepth bounds recursion through analyseSelect across compound branches, CTE
	// bodies, derived tables, scalar subqueries, and view bodies. Child parsers inherit the
	// parent depth so the global cap holds across instances.
	analysisDepth int

	// expressionDepth bounds recursion through the expression precedence chain so deeply
	// nested parentheses cannot overflow the stack.
	expressionDepth int

	// typeDepth bounds recursion through compound column-type parsing (STRUCT/MAP/UNION
	// nesting). List and array suffixes have a separate dimension limit.
	typeDepth int

	// maxParseDepth is the effective cap for analysisDepth, expressionDepth and typeDepth,
	// which the engine sets from the dialect and child parsers inherit.
	//
	// The capped recursions are analyseSelect (subquery and CTE nesting), the expression
	// precedence chain (parenthesis nesting), and compound column-type nesting. The cap is
	// essential because Go raises a fatal, non-recoverable stack overflow that no recover
	// can catch, so deeply nested input would otherwise crash the host process.
	maxParseDepth int

	// maxPivotColumns is the most output columns one PIVOT may make, which the engine sets
	// from the dialect and child parsers inherit.
	maxPivotColumns int

	// insertProjectionIndex is the zero-based ordinal of the projection item currently being
	// parsed in an INSERT ... SELECT body.
	insertProjectionIndex int

	// aliasStart is the index of the token an output column's alias starts at, or of the
	// token after its expression when it has none; -1 before the alias is looked for.
	aliasStart int

	// hasDataModifyingCTE is set when a CTE uses INSERT, UPDATE, or DELETE.
	hasDataModifyingCTE bool

	// lastArgumentWasVariadic records whether the most recently parsed call argument used
	// variadic syntax.
	lastArgumentWasVariadic bool

	// suppressClauses stops clause expressions being recorded, inside a clause whose names
	// the analyser must not check, such as an ON CONFLICT action naming the EXCLUDED row.
	suppressClauses bool

	// skipSubqueries leaves the subqueries an expression holds unanalysed, in a parser that
	// only records the names of a clause another scan has analysed already.
	skipSubqueries bool

	// pivotArm records that the query being parsed has a PIVOT or UNPIVOT statement as an
	// arm, whose ORDER BY names the pivot's columns rather than its source's.
	pivotArm bool
}

// newParser builds a parser over the given token stream with the default parse depth cap.
//
// Takes tokens ([]token) which is the flat token stream to walk.
//
// Returns *parser which is the initialised parser.
func newParser(tokens []token) *parser {
	return &parser{
		types:                   nil,
		functions:               nil,
		catalogue:               nil,
		source:                  "",
		parenthesisIndex:        nil,
		lambdaScopes:            nil,
		statement:               newStatementState(tokens),
		argumentCursors:         nil,
		Cursor:                  dialectkit.NewCursor(tokens, tokenIdentifier, tokenEOF),
		maxParseDepth:           dialectkit.DefaultMaxParseDepth,
		maxPivotColumns:         maxPivotOutputColumns,
		namedParameterMap:       make(map[string]int),
		placeholderNumbers:      make(map[int]int),
		scannedSubqueries:       make(map[int]*ast.RawQueryAnalysis),
		insertProjectionTable:   "",
		parameterReferences:     nil,
		rawDerivedTables:        nil,
		predicateSubqueries:     nil,
		rawTableValuedFunctions: nil,
		fromOrder:               nil,
		argumentNames:           nil,
		spans:                   nil,
		clauseExpressions:       nil,
		itemSpans:               nil,
		insertProjectionColumns: nil,
		parameterCount:          0,
		analysisDepth:           0,
		expressionDepth:         0,
		typeDepth:               0,
		insertProjectionIndex:   0,
		aliasStart:              -1,
		hasDataModifyingCTE:     false,
		lastArgumentWasVariadic: false,
		suppressClauses:         false,
		skipSubqueries:          false,
		pivotArm:                false,
	}
}

// sourceText returns the source text spanning the tokens from startIndex up to but
// excluding endIndex, sliced from the script exactly as written. Without the script it
// joins the tokens' raw text with single spaces.
//
// Takes startIndex (int) which is the index of the first token.
// Takes endIndex (int) which is one past the index of the last token.
//
// Returns string which is the text, or empty for an empty or invalid range.
func (p *parser) sourceText(startIndex, endIndex int) string {
	return dialectkit.SourceBetween(p.source, p.Tokens, startIndex, endIndex)
}

// parseIdentifierOrKeyword consumes an identifier or quoted string, accepting both
// because DuckDB allows quoted identifiers.
//
// Returns string which is the consumed token's value.
// Returns error when the current token is neither an identifier nor a string literal.
func (p *parser) parseIdentifierOrKeyword() (string, error) {
	current := p.Current()
	if current.Kind == tokenIdentifier || current.Kind == tokenString {
		p.Position++
		return current.Value, nil
	}
	return "", dialectkit.NewParseError(current, "expected an identifier")
}

// parseSchemaQualifiedName consumes an optionally schema-qualified identifier of the form
// "name" or "schema.name".
//
// Returns schema (string) which is the schema name when present, else empty.
// Returns name (string) which is the unqualified identifier.
// Returns err (error) when the identifier cannot be parsed.
func (p *parser) parseSchemaQualifiedName() (schema string, name string, err error) {
	first, parseError := p.parseIdentifierOrKeyword()
	if parseError != nil {
		return "", "", parseError
	}

	if p.Current().Kind == tokenDot {
		p.Advance()
		second, secondError := p.parseIdentifierOrKeyword()
		if secondError != nil {
			return "", "", secondError
		}
		return first, second, nil
	}

	return "", first, nil
}

// skipParenthesised consumes a balanced parenthesised group starting at the current
// token.
//
// Returns error when the current token is not '(' or the group is unbalanced.
func (p *parser) skipParenthesised() error {
	if p.Current().Kind != tokenLeftParen {
		return dialectkit.NewParseError(p.Current(), "expected '('")
	}
	p.Advance()
	depth := 1
	for depth > 0 && !p.AtEnd() {
		switch p.Current().Kind {
		case tokenLeftParen:
			depth++
		case tokenRightParen:
			depth--
		default:
		}
		p.Advance()
	}
	if depth != 0 {
		return dialectkit.NewParseError(p.Current(), "unmatched parenthesis")
	}
	return nil
}

// collectParenthesised consumes a balanced parenthesised group and returns the tokens
// that lay strictly inside it.
//
// Returns []token which holds the inner tokens, excluding the outer parentheses.
// Returns error when the current token is not '(' or the group is unbalanced.
func (p *parser) collectParenthesised() ([]token, error) {
	if p.Current().Kind != tokenLeftParen {
		return nil, dialectkit.NewParseError(p.Current(), "expected '('")
	}
	p.Advance()
	start := p.Position
	depth := 1
	for !p.AtEnd() {
		switch p.Current().Kind {
		case tokenLeftParen:
			depth++
		case tokenRightParen:
			depth--
		default:
		}
		if depth == 0 {
			end := p.Position
			p.Advance()
			return tokenView(p.Tokens, start, end), nil
		}
		p.Advance()
	}
	return nil, dialectkit.NewParseError(p.Current(), "unmatched parenthesis")
}

// newChildParser builds a parser over tokens for a nested construct of the statement
// being parsed.
//
// The child inherits the recursion counters and depth cap so no nesting path can reset
// the global bound, and shares the syntax error sink so a malformed nested construct
// fails the whole statement.
//
// Takes tokens ([]token) which is the nested construct's token stream.
//
// Returns *parser which is the child parser.
func (p *parser) newChildParser(tokens []token) *parser {
	child := newParser(tokens)
	child.Interrupt = p.Interrupt
	child.analysisDepth = p.analysisDepth
	child.expressionDepth = p.expressionDepth
	child.typeDepth = p.typeDepth
	child.maxParseDepth = p.maxParseDepth
	child.maxPivotColumns = p.maxPivotColumns
	child.statement = p.statement
	child.types = p.types
	child.functions = p.functions
	child.catalogue = p.catalogue
	child.source = p.source
	child.namedParameterMap = p.namedParameterMap
	child.placeholderNumbers = p.placeholderNumbers
	child.scannedSubqueries = p.scannedSubqueries
	return child
}

// analyseNestedQuery analyses a complete nested query and merges its parameters into the
// parent.
//
// Malformed nested queries fail the statement rather than leaving part of the input
// unanalysed.
//
// Takes innerTokens ([]token) which are the tokens between the query's parentheses.
//
// Returns *parser which is the child parser, holding the nested query's own FROM-clause
// state.
// Returns *ast.RawQueryAnalysis which is the nested query's analysis.
// Returns error which is non-nil, and recorded as the statement's syntax error, when the
// query is malformed or leaves tokens unparsed.
func (p *parser) analyseNestedQuery(innerTokens []token) (*parser, *ast.RawQueryAnalysis, error) {
	if p.skipSubqueries {
		return nil, nil, errSubquerySkipped
	}
	child := p.newChildParser(innerTokens)
	child.parameterCount = p.parameterCount
	analysis, err := child.analyseSelect()
	if err == nil {
		err = child.requireEnd()
	}
	if err != nil {
		p.recordSyntaxError(err)
		return child, nil, err
	}
	p.parameterCount = child.parameterCount
	p.parameterReferences = append(p.parameterReferences, child.parameterReferences...)
	p.adoptArgumentNames(child)
	return child, analysis, nil
}

// recordSyntaxError keeps err as the statement's syntax error unless an earlier one was
// already recorded.
//
// Takes err (error) which is the syntax error found.
func (p *parser) recordSyntaxError(err error) {
	if err != nil && p.statement.firstSyntaxError == nil {
		p.statement.firstSyntaxError = err
	}
}

// requireEnd records and returns a syntax error when tokens remain after the statement
// the parser has analysed, so an unparsed tail fails the statement instead of being
// dropped.
//
// Returns error which is a dialectkit.ParseError naming the first unparsed token, or nil
// at the end of the tokens.
func (p *parser) requireEnd() error {
	err := p.RequireEnd()
	p.recordSyntaxError(err)
	return err
}

// syntaxErrorAt records and returns a syntax error at the current token.
//
// Takes message (string) which says what was wrong.
//
// Returns error which is the recorded dialectkit.ParseError.
func (p *parser) syntaxErrorAt(message string) error {
	err := dialectkit.NewParseError(p.Current(), message)
	p.recordSyntaxError(err)
	return err
}

// syntaxError returns the first syntax error recorded for the statement.
//
// Returns error which is the recorded syntax error, or nil when there is none.
func (p *parser) syntaxError() error {
	return p.statement.firstSyntaxError
}

// skipParenthesisedOrRecord consumes a balanced parenthesised group, recording a syntax
// error when the group is malformed, for parse functions that cannot return one.
func (p *parser) skipParenthesisedOrRecord() {
	p.recordSyntaxError(p.skipParenthesised())
}

// registerParameterFromToken records a bind parameter reference, dispatching by the
// parameter token's shape. A placeholder the statement registered before keeps the number
// it was registered under.
//
// Takes parameterToken (token) which is the original parameter token.
// Takes context (sqlmodel.ParameterContext) which records where in the statement the
// parameter appears.
// Takes columnReference (*ast.ColumnReference) which is the inferred target column, or
// nil when none is known.
// Takes castType (*sqlmodel.SQLType) which is the inline cast type applied to the
// parameter, or nil when no cast applies.
//
// Returns int which is the parameter's sequential number.
func (p *parser) registerParameterFromToken(
	parameterToken token,
	context sqlmodel.ParameterContext,
	columnReference *ast.ColumnReference,
	castType *sqlmodel.SQLType,
) int {
	var number int
	switch {
	case parameterToken.Kind == tokenDollarParam, parameterToken.Kind == tokenQuestionParam && len(parameterToken.Value) > 1:
		number = p.registerDollarParameter(parameterToken, context, columnReference, castType)
	case parameterToken.Kind == tokenNamedParam:
		number = p.registerNamedParameter(parameterToken, context, columnReference, castType)
	default:
		number = p.registerSequentialParameter(context, columnReference, castType)
	}
	if registered, known := p.placeholderNumbers[parameterToken.Start]; known {
		return registered
	}
	p.placeholderNumbers[parameterToken.Start] = number
	return number
}

// registerSequentialParameter assigns the next sequential parameter number and records
// the reference.
//
// Takes context (sqlmodel.ParameterContext) which records where the parameter appears.
// Takes columnReference (*ast.ColumnReference) which is the inferred target column, or
// nil when none is known.
// Takes castType (*sqlmodel.SQLType) which is the inline cast type, or nil when no cast
// applies.
//
// Returns int which is the newly assigned parameter number.
func (p *parser) registerSequentialParameter(
	context sqlmodel.ParameterContext,
	columnReference *ast.ColumnReference,
	castType *sqlmodel.SQLType,
) int {
	p.parameterCount++
	number := p.parameterCount
	p.parameterReferences = append(p.parameterReferences, ast.RawParameterReference{
		Number:                number,
		Context:               context,
		ColumnReference:       columnReference,
		CastType:              castType,
		Name:                  "",
		EnclosingFunctionName: "",
		ArgumentOrdinal:       0,
	})
	return number
}

// registerDollarParameter records a $N positional parameter, preserving the explicit
// number and bumping the watermark when needed.
//
// Takes parameterToken (token) which carries the $N spelling.
// Takes context (sqlmodel.ParameterContext) which records where the parameter appears.
// Takes columnReference (*ast.ColumnReference) which is the inferred target column, or
// nil when none is known.
// Takes castType (*sqlmodel.SQLType) which is the inline cast type, or nil when no cast
// applies.
//
// Returns int which is the parameter's number as written.
func (p *parser) registerDollarParameter(
	parameterToken token,
	context sqlmodel.ParameterContext,
	columnReference *ast.ColumnReference,
	castType *sqlmodel.SQLType,
) int {
	number, conversionError := strconv.Atoi(parameterToken.Value[1:])
	if conversionError != nil || number < 1 || number > maxDollarParameterNumber {
		p.recordSyntaxError(dialectkit.NewParseError(parameterToken,
			fmt.Sprintf("positional parameter number must be between 1 and %d", maxDollarParameterNumber)))
	}
	if number > p.parameterCount {
		p.parameterCount = number
	}
	p.parameterReferences = append(p.parameterReferences, ast.RawParameterReference{
		Number:                number,
		Context:               context,
		ColumnReference:       columnReference,
		CastType:              castType,
		Name:                  "",
		EnclosingFunctionName: "",
		ArgumentOrdinal:       0,
	})
	return number
}

// registerNamedParameter records a :name parameter, reusing the number previously
// assigned to the same name when one exists.
//
// Takes parameterToken (token) which carries the :name spelling.
// Takes context (sqlmodel.ParameterContext) which records where the parameter appears.
// Takes columnReference (*ast.ColumnReference) which is the inferred target column, or
// nil when none is known.
// Takes castType (*sqlmodel.SQLType) which is the inline cast type, or nil when no cast
// applies.
//
// Returns int which is the number associated with the name.
func (p *parser) registerNamedParameter(
	parameterToken token,
	context sqlmodel.ParameterContext,
	columnReference *ast.ColumnReference,
	castType *sqlmodel.SQLType,
) int {
	name := parameterToken.Value[1:]
	if existingNumber, exists := p.namedParameterMap[name]; exists {
		p.parameterReferences = append(p.parameterReferences, ast.RawParameterReference{
			Number:                existingNumber,
			Name:                  name,
			Context:               context,
			ColumnReference:       columnReference,
			CastType:              castType,
			EnclosingFunctionName: "",
			ArgumentOrdinal:       0,
		})
		return existingNumber
	}
	number := max(p.parameterCount, p.statement.highestNumberedPlaceholder) + 1
	p.parameterCount = number
	p.namedParameterMap[name] = number
	p.parameterReferences = append(p.parameterReferences, ast.RawParameterReference{
		Number:                number,
		Name:                  name,
		Context:               context,
		ColumnReference:       columnReference,
		CastType:              castType,
		EnclosingFunctionName: "",
		ArgumentOrdinal:       0,
	})
	return number
}

// classifyStatement labels a statement from its first token.
//
// Takes tokens ([]token) which is the statement's token slice.
//
// Returns statementKind which is the classified category or statementKindUnknown when the
// lead-in is not recognised.
func classifyStatement(tokens []token) statementKind {
	if len(tokens) == 0 {
		return statementKindUnknown
	}

	if tokens[0].Kind == tokenLeftParen {
		return classifyParenthesisedStatement(tokens)
	}

	first := tokens[0].Keyword(tokenIdentifier)

	if kind, found := firstWordStaticKinds[first]; found {
		return kind
	}

	if classifier, found := firstWordClassifiers[first]; found {
		return classifier(tokens)
	}

	return statementKindUnknown
}

// classifyParenthesisedStatement labels a statement that opens with a parenthesis, such
// as (SELECT ...) UNION (SELECT ...), by the first word inside its parentheses.
//
// Takes tokens ([]token) which is the statement's token slice.
//
// Returns statementKind which is statementKindSelect for a parenthesised query, or
// statementKindUnknown.
func classifyParenthesisedStatement(tokens []token) statementKind {
	index := 0
	for index < len(tokens) && tokens[index].Kind == tokenLeftParen {
		index++
	}
	if index < len(tokens) && classifyStatement(tokens[index:]) == statementKindSelect {
		return statementKindSelect
	}
	return statementKindUnknown
}

// classifyCreateStatement decides which CREATE variant the tokens describe.
//
// Takes tokens ([]token) which is the statement's token slice.
//
// Returns statementKind which is the CREATE variant or statementKindUnknown when no
// object keyword is recognised.
func classifyCreateStatement(tokens []token) statementKind {
	if len(tokens) < 2 {
		return statementKindUnknown
	}

	index := skipCreatePrefixes(tokens)
	if index >= len(tokens) {
		return statementKindUnknown
	}

	upper := tokens[index].Keyword(tokenIdentifier)
	if kind, found := createObjectKinds[upper]; found {
		return kind
	}

	return statementKindUnknown
}

// skipCreatePrefixes returns the index of the first object keyword past any OR REPLACE
// and TEMP/TEMPORARY prefixes.
//
// Takes tokens ([]token) which is the statement's token slice.
//
// Returns int which is the index of the object keyword, or len(tokens) when the prefix is
// malformed.
func skipCreatePrefixes(tokens []token) int {
	index := 1
	upper := tokens[index].Keyword(tokenIdentifier)

	if upper == "OR" {
		if len(tokens) < minTokensForCreateOrReplace {
			return len(tokens)
		}
		index = indexAfterOrReplace
		upper = tokens[index].Keyword(tokenIdentifier)
	}

	if upper == "TEMP" || upper == "TEMPORARY" {
		if index+1 >= len(tokens) {
			return len(tokens)
		}
		index++
	}

	return index
}

// classifyDropStatement labels a DROP statement by its object keyword.
//
// Takes tokens ([]token) which is the statement's token slice.
//
// Returns statementKind which is the DROP variant or statementKindUnknown when the object
// keyword is not recognised.
func classifyDropStatement(tokens []token) statementKind {
	if len(tokens) < 2 {
		return statementKindUnknown
	}

	second := tokens[1].Keyword(tokenIdentifier)
	switch second {
	case keywordTABLE:
		return statementKindDropTable
	case "VIEW":
		return statementKindDropView
	case "INDEX":
		return statementKindDropIndex
	case keywordTYPE:
		return statementKindDropType
	case "FUNCTION":
		return statementKindDropFunction
	case keywordMACRO:
		return statementKindDropMacro
	case keywordSCHEMA:
		return statementKindDropSchema
	case "SEQUENCE":
		return statementKindDropSequence
	}

	return statementKindUnknown
}

// classifyAlterStatement labels an ALTER statement by its object keyword.
//
// Takes tokens ([]token) which is the statement's token slice.
//
// Returns statementKind which is the ALTER variant or statementKindUnknown when the
// object keyword is not recognised.
func classifyAlterStatement(tokens []token) statementKind {
	if len(tokens) < 2 {
		return statementKindUnknown
	}

	second := tokens[1].Keyword(tokenIdentifier)
	switch second {
	case keywordTABLE:
		return statementKindAlterTable
	case keywordTYPE:
		return statementKindAlterType
	case "SEQUENCE":
		return statementKindAlterSequence
	case "VIEW":
		return statementKindAlterView
	}

	return statementKindUnknown
}

// classifyWithStatement scans a WITH statement's body for the first top-level DML keyword
// to decide the statement kind.
//
// Takes tokens ([]token) which is the statement's token slice.
//
// Returns statementKind which is the DML kind found, defaulting to statementKindSelect
// when nothing more specific is seen.
func classifyWithStatement(tokens []token) statementKind {
	depth := 0
	for _, current := range tokens {
		if current.Kind == tokenLeftParen {
			depth++
			continue
		}
		if current.Kind == tokenRightParen {
			depth--
			continue
		}
		if depth != 0 || current.Kind != tokenIdentifier {
			continue
		}
		if kind, matched := classifyDMLKeyword(current.Value); matched {
			return kind
		}
	}
	return statementKindSelect
}

// classifyDMLKeyword looks up a candidate DML keyword.
//
// Takes value (string) which is the raw identifier value.
//
// Returns statementKind which is the matching kind when found.
// Returns bool which is true when value matches a DML keyword.
func classifyDMLKeyword(value string) (statementKind, bool) {
	kind, matched := dmlKeywords[strings.ToUpper(value)]
	return kind, matched
}

// highestPlaceholderNumber returns the highest number a numbered placeholder is written
// with among a statement's tokens: $N, or ?N.
//
// Takes tokens ([]token) which are the statement's tokens.
//
// Returns int which is the highest number, or 0 when no placeholder is numbered.
func highestPlaceholderNumber(tokens []token) int {
	highest := 0
	for index := range tokens {
		numbered := tokens[index].Kind == tokenDollarParam ||
			tokens[index].Kind == tokenQuestionParam && len(tokens[index].Value) > 1
		if !numbered {
			continue
		}
		if number, err := strconv.Atoi(tokens[index].Value[1:]); err == nil {
			highest = max(highest, number)
		}
	}
	return highest
}

// isParameterToken reports whether a token kind represents a bind parameter placeholder.
//
// Takes kind (tokenKind) which is the token kind to test.
//
// Returns bool which is true for the $N, ?, ?N and :name parameter kinds.
func isParameterToken(kind tokenKind) bool {
	return kind == tokenDollarParam || kind == tokenNamedParam || kind == tokenQuestionParam
}

// tokenView returns tokens[start:end] with its capacity capped at its length, or nil when
// the range is empty, so an append by the holder reallocates rather than overwriting the
// tokens that follow the range.
//
// Takes tokens ([]token) which is the token stream the view is taken from.
// Takes start (int) which is the first index in the view.
// Takes end (int) which is the index just past the view.
//
// Returns []token which is the capacity-capped view.
func tokenView(tokens []token, start, end int) []token {
	if end == start {
		return nil
	}
	return tokens[start:end:end]
}

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
	"slices"
	"strconv"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// minTokensForCreateOrReplace is the minimum number of tokens required for a CREATE OR
	// REPLACE prefix to be considered well-formed.
	minTokensForCreateOrReplace = 4

	// indexAfterOrReplace is the token index following CREATE OR REPLACE.
	indexAfterOrReplace = 3

	// sqlSecurityTokenCount is the number of tokens in a SQL SECURITY {DEFINER | INVOKER}
	// modifier.
	sqlSecurityTokenCount = 3

	// unmatchedParenthesis is the message of the syntax error reported at a parenthesis
	// group that is not closed before the end of the statement.
	unmatchedParenthesis = "unmatched parenthesis"
)

// statementKind enumerates the high-level categories of SQL statement that the classifier
// recognises.
type statementKind uint8

const (
	// statementKindCreateTable identifies a CREATE TABLE statement.
	statementKindCreateTable statementKind = iota

	// statementKindDropTable identifies a DROP TABLE statement.
	statementKindDropTable

	// statementKindAlterTable identifies an ALTER TABLE statement.
	statementKindAlterTable

	// statementKindCreateView identifies a CREATE VIEW statement.
	statementKindCreateView

	// statementKindDropView identifies a DROP VIEW statement.
	statementKindDropView

	// statementKindCreateIndex identifies a CREATE INDEX statement.
	statementKindCreateIndex

	// statementKindDropIndex identifies a DROP INDEX statement.
	statementKindDropIndex

	// statementKindCreateTrigger identifies a CREATE TRIGGER statement.
	statementKindCreateTrigger

	// statementKindDropTrigger identifies a DROP TRIGGER statement.
	statementKindDropTrigger

	// statementKindCreateDatabase identifies a CREATE DATABASE or SCHEMA statement.
	statementKindCreateDatabase

	// statementKindDropDatabase identifies a DROP DATABASE or SCHEMA statement.
	statementKindDropDatabase

	// statementKindSelect identifies a SELECT statement.
	statementKindSelect

	// statementKindInsert identifies an INSERT statement.
	statementKindInsert

	// statementKindUpdate identifies an UPDATE statement.
	statementKindUpdate

	// statementKindDelete identifies a DELETE statement.
	statementKindDelete

	// statementKindValues identifies a standalone VALUES statement.
	statementKindValues

	// statementKindReplace identifies a REPLACE statement.
	statementKindReplace

	// statementKindCreateFunction identifies a CREATE FUNCTION or PROCEDURE statement.
	statementKindCreateFunction

	// statementKindDropFunction identifies a DROP FUNCTION or PROCEDURE statement.
	statementKindDropFunction

	// statementKindCall identifies a CALL statement.
	statementKindCall

	// statementKindRenameTable identifies a RENAME TABLE statement.
	statementKindRenameTable

	// statementKindShow identifies a SHOW statement.
	statementKindShow

	// statementKindUnknown identifies a statement whose kind could not be determined.
	statementKindUnknown

	// statementKindCount is the sentinel count of recognised statement kinds.
	statementKindCount
)

var (
	// firstWordClassifiers dispatches on the leading keyword for statements whose kind
	// requires further inspection beyond the first word.
	firstWordClassifiers = map[string]func([]token) statementKind{
		keywordCREATE: classifyCreateStatement,
		keywordDROP:   classifyDropStatement,
		"ALTER":       classifyAlterStatement,
		keywordWITH:   classifyWithStatement,
		"RENAME":      classifyRenameStatement,
	}

	// firstWordStaticKinds maps leading keywords whose statement kind is fully determined by
	// the first word.
	firstWordStaticKinds = map[string]statementKind{
		keywordSELECT:  statementKindSelect,
		"INSERT":       statementKindInsert,
		"UPDATE":       statementKindUpdate,
		"DELETE":       statementKindDelete,
		keywordVALUES:  statementKindValues,
		keywordREPLACE: statementKindReplace,
		keywordCALL:    statementKindCall,
		"SHOW":         statementKindShow,
	}

	// createObjectKinds maps the noun following CREATE (optionally after modifiers) to the
	// matching statementKind.
	createObjectKinds = map[string]statementKind{
		keywordTABLE:     statementKindCreateTable,
		"VIEW":           statementKindCreateView,
		"INDEX":          statementKindCreateIndex,
		keywordUNIQUE:    statementKindCreateIndex,
		"TRIGGER":        statementKindCreateTrigger,
		keywordDATABASE:  statementKindCreateDatabase,
		keywordSCHEMA:    statementKindCreateDatabase,
		keywordFUNCTION:  statementKindCreateFunction,
		keywordPROCEDURE: statementKindCreateFunction,
	}

	// dmlKeywords maps the data-manipulation keywords recognised inside a WITH statement to
	// their matching statementKind.
	dmlKeywords = map[string]statementKind{
		keywordSELECT:  statementKindSelect,
		"INSERT":       statementKindInsert,
		"UPDATE":       statementKindUpdate,
		"DELETE":       statementKindDelete,
		keywordVALUES:  statementKindValues,
		keywordREPLACE: statementKindReplace,
	}
)

// parsedStatement bundles a token slice with its classified statement kind.
type parsedStatement struct {
	// source is the script the statement was read from; token offsets index into it.
	source string

	// tokens holds the raw token slice for the statement. For a statement over the token
	// budget it holds only the tokens read before the budget was reached.
	tokens []token

	// span is where the statement sits in source, so a statement over the budget can be read
	// again to its end.
	span sqllex.Statement

	// kind records the classified statement category.
	kind statementKind

	// overBudget reports a statement holding more tokens than the dialect's budget, whose
	// tokenising stopped at the budget.
	overBudget bool
}

// IsParsedStatement marks the type as conforming to the parsed statement marker
// interface.
func (*parsedStatement) IsParsedStatement() {}

// parser holds the mutable state required to walk a single SQL statement and accumulate
// parameter and table references.
type parser struct {
	// types normalises type names, such as a CAST target, through the dialect's hooks and
	// extra types; nil resolves through the built-in types alone.
	types *typeResolver

	// functions is the dialect's built-in function catalogue, consulted for the result type
	// of a function call a parameter is compared with; nil leaves such parameters untyped.
	functions *ast.FunctionCatalogue

	// parenthesisIndex answers the enclosing-parenthesis and enclosing-LIKE lookups the
	// parameter-context scans make; nil until first used.
	parenthesisIndex *dialectkit.ParenthesisScanIndex

	// argumentIndex answers the argument-ordinal and matching-parenthesis lookups the
	// parameter-context scans make; nil until first used.
	argumentIndex *argumentScanIndex

	// namedParameterMap maps named parameter labels to their assigned sequential numbers.
	namedParameterMap map[string]int

	// placeholderNumbers preserves assigned placeholder numbers when shared child parsers
	// revisit the same statement.
	placeholderNumbers map[int]int

	// scannedSubqueries maps the source offset of the parenthesis opening each subquery a
	// clause scan analysed to its analysis, shared with child parsers, so a parser that
	// records the clause's expressions without analysing its subqueries again still gives
	// each subquery its query.
	scannedSubqueries map[int]*ast.RawQueryAnalysis

	// warnings maps the offset of the token each warning is about to the warning, shared
	// with child parsers, so SQL a parser walks more than once is warned of once; nil in a
	// parser whose warnings no analysis reports.
	warnings map[int]ast.Warning

	// rowLocked records whether a query anywhere in the statement carries a FOR UPDATE, FOR
	// SHARE or LOCK IN SHARE MODE locking clause. Every child parser shares it, so a lock in
	// a CTE body, a derived table or a subquery keeps the statement off a read replica.
	rowLocked *bool

	// source is the script the statement was read from, which token offsets index into, so a
	// routine body can be kept exactly as written; empty when it is not known.
	source string

	// insertProjectionTable is the INSERT target table whose columns the current INSERT ...
	// SELECT projection items are assigned to.
	//
	// Empty when not inside an INSERT ... SELECT projection.
	insertProjectionTable string

	// parameterReferences accumulates references to bind parameters encountered during
	// parsing.
	parameterReferences []ast.RawParameterReference

	// syntaxError holds the first syntax error met by a parsing step that has no error
	// return of its own, such as the expression chain. The engine returns it once the
	// statement has been walked; nil when no such error occurred.
	syntaxError error

	// rawDerivedTables collects parsed derived (subquery) table references.
	rawDerivedTables []ast.RawDerivedTableReference

	// predicateSubqueries collects subqueries found in a token-scanned predicate position
	// (WHERE / HAVING / JOIN ON), so the analyser can resolve each one's parameters against
	// the subquery's own scope rather than the parent.
	predicateSubqueries []*ast.RawQueryAnalysis

	// rawTableValuedFunctions collects parsed table-valued function references appearing in
	// FROM clauses.
	rawTableValuedFunctions []ast.RawTableValuedFunctionReference

	// fromOrder receives the names the items of the FROM clause being parsed are visible
	// under, in order; nil outside a FROM clause.
	fromOrder *[]string

	// insertProjectionColumns is the ordered INSERT target column list whose entries the
	// INSERT ... SELECT projection items map onto positionally.
	//
	// Nil when not inside an INSERT ... SELECT projection.
	insertProjectionColumns []string

	// spans records the spans of the top-level statement; it is nil in every other parser,
	// so a nested query's clauses are never recorded as the statement's.
	spans *dialectkit.SpanRecorder

	// clauseExpressions collects the expressions of the clauses parsed so far whose query
	// has not taken them yet.
	clauseExpressions []ast.ClauseExpression

	// itemSpans holds the spans of the items of the select list or RETURNING clause parsed
	// last.
	itemSpans []ast.ItemSpan

	// Cursor walks the statement's tokens; its keyword matching never matches a quoted
	// identifier.
	dialectkit.Cursor[tokenKind]

	// parameterCount is the running count of parameters assigned so far.
	parameterCount int

	// analysisDepth bounds recursion through analyseSelect across compound branches, CTE
	// bodies, derived tables, scalar subqueries, and EXISTS / IN-subquery expressions. Child
	// parsers inherit the parent depth so the global cap holds across instances.
	analysisDepth int

	// expressionDepth bounds recursion through the expression precedence chain so deeply
	// nested parentheses cannot overflow the stack.
	expressionDepth int

	// maxParseDepth is the effective cap for analysisDepth and expressionDepth. It caps
	// every user-driven recursion, because Go raises a fatal, non-recoverable stack overflow
	// that no recover can catch; the engine sets it from the dialect's limits and child
	// parsers inherit it.
	maxParseDepth int

	// insertProjectionIndex is the zero-based ordinal of the projection item currently being
	// parsed within the INSERT ... SELECT select list.
	insertProjectionIndex int

	// aliasStart is the index of the token an output column's alias starts at, or of the
	// token after its expression when it has none; -1 before the alias is looked for.
	aliasStart int

	// hasDataModifyingCTE records whether any CTE in the statement performs a data-modifying
	// operation.
	hasDataModifyingCTE bool

	// rowAliasUnavailable records that the dialect lacks the row alias of INSERT ... VALUES
	// (...) AS alias, so the parser rejects one.
	rowAliasUnavailable bool

	// jsonValueReturningUnavailable records that the dialect's JSON_VALUE lacks the
	// RETURNING clause, so the parser rejects one.
	jsonValueReturningUnavailable bool

	// sequences records that the dialect has sequences, so the parser reads NEXT VALUE FOR
	// and PREVIOUS VALUE FOR, and the name NEXTVAL, LASTVAL and SETVAL take first, as a
	// sequence.
	sequences bool

	// offsetFetch records that the dialect takes OFFSET n ROWS and FETCH FIRST n ROWS ONLY
	// as a row limit.
	offsetFetch bool

	// createOrReplace records that a CREATE of a table, index, database or trigger may say
	// OR REPLACE.
	createOrReplace bool

	// suppressClauses stops clause expressions being recorded, inside a clause whose names
	// the analyser must not check, such as ON DUPLICATE KEY UPDATE naming the inserted row.
	suppressClauses bool

	// skipSubqueries leaves the subqueries an expression holds unanalysed, in a parser that
	// only records the names of a clause another scan has analysed already.
	skipSubqueries bool

	// strictExpressions makes the expression grammar report a syntax error wherever it would
	// otherwise step over a token it cannot read, such as a missing operand or an unclosed
	// parenthesis, in a parser that checks a condition another scan stepped over.
	strictExpressions bool
}

// newParser creates a parser over the token stream seeded with the default depth cap.
//
// Takes tokens ([]token) which is the lexed token stream to walk.
//
// Returns *parser which is ready to analyse the statement.
func newParser(tokens []token) *parser {
	return &parser{
		types:                         nil,
		functions:                     nil,
		parenthesisIndex:              nil,
		argumentIndex:                 nil,
		Cursor:                        dialectkit.NewCursor(tokens, tokenIdentifier, tokenEOF),
		maxParseDepth:                 dialectkit.DefaultMaxParseDepth,
		namedParameterMap:             make(map[string]int),
		placeholderNumbers:            make(map[int]int),
		scannedSubqueries:             make(map[int]*ast.RawQueryAnalysis),
		warnings:                      make(map[int]ast.Warning),
		source:                        "",
		insertProjectionTable:         "",
		parameterReferences:           nil,
		syntaxError:                   nil,
		rawDerivedTables:              nil,
		predicateSubqueries:           nil,
		rawTableValuedFunctions:       nil,
		fromOrder:                     nil,
		insertProjectionColumns:       nil,
		spans:                         nil,
		clauseExpressions:             nil,
		itemSpans:                     nil,
		parameterCount:                0,
		analysisDepth:                 0,
		expressionDepth:               0,
		insertProjectionIndex:         0,
		aliasStart:                    -1,
		rowLocked:                     new(bool),
		hasDataModifyingCTE:           false,
		rowAliasUnavailable:           false,
		jsonValueReturningUnavailable: false,
		sequences:                     false,
		offsetFetch:                   false,
		createOrReplace:               false,
		suppressClauses:               false,
		skipSubqueries:                false,
		strictExpressions:             false,
	}
}

// newChildParser creates a nested parser sharing depth, parameter and dialect state.
//
// Shared depth counters retain the global recursion bound, and shared placeholder numbers
// prevent renumbering.
//
// Takes tokens ([]token) which is the nested construct's token slice.
//
// Returns *parser which is the child parser.
func (p *parser) newChildParser(tokens []token) *parser {
	child := newParser(tokens)
	child.Interrupt = p.Interrupt
	child.types = p.types
	child.functions = p.functions
	child.rowAliasUnavailable = p.rowAliasUnavailable
	child.jsonValueReturningUnavailable = p.jsonValueReturningUnavailable
	child.sequences = p.sequences
	child.offsetFetch = p.offsetFetch
	child.createOrReplace = p.createOrReplace
	child.parameterCount = p.parameterCount
	child.placeholderNumbers = p.placeholderNumbers
	child.scannedSubqueries = p.scannedSubqueries
	child.rowLocked = p.rowLocked
	child.warnings = p.warnings
	child.analysisDepth = p.analysisDepth
	child.expressionDepth = p.expressionDepth
	child.maxParseDepth = p.maxParseDepth
	child.source = p.source
	return child
}

// newNestedQueryParser creates the child parser that analyses a nested query, such as a
// subquery, a derived table or a CTE body. Besides what newChildParser inherits, it
// shares the parent's named parameters, so a name written both inside and outside the
// nested query is one parameter.
//
// Takes tokens ([]token) which is the nested query's token slice.
//
// Returns *parser which is the child parser.
func (p *parser) newNestedQueryParser(tokens []token) *parser {
	child := p.newChildParser(tokens)
	child.namedParameterMap = p.namedParameterMap
	return child
}

// enterAnalysis counts one level of analysis recursion, failing with an error wrapping
// dialectkit.ErrParseDepth once the cap is reached. A caller that succeeds must call
// leaveAnalysis when it returns.
//
// Returns error when the analysis depth cap is reached.
func (p *parser) enterAnalysis() error {
	if p.analysisDepth >= p.maxParseDepth {
		return dialectkit.DepthError(p.maxParseDepth)
	}
	p.analysisDepth++
	return nil
}

// leaveAnalysis releases the level enterAnalysis counted.
func (p *parser) leaveAnalysis() {
	p.analysisDepth--
}

// requireEnd records and returns a syntax error when tokens remain after the statement
// the parser has analysed, so an unparsed tail fails the statement instead of being
// dropped. A tail opening with INTO, a query storing its row in variables or a file, is
// refused with a message saying so.
//
// Returns error which is a dialectkit.ParseError naming the first unparsed token, or nil
// at the end of the tokens.
func (p *parser) requireEnd() error {
	if p.IsKeyword("INTO") {
		return p.syntaxErrorAt(selectIntoMessage)
	}
	err := p.RequireEnd()
	if err != nil {
		p.recordSyntaxError(err)
	}
	return err
}

// requireStatementEnd records and returns a syntax error when tokens other than a closing
// semicolon remain after the statement a DDL handler has read, so a clause the handler
// does not know fails the statement instead of being dropped.
//
// Returns error which is a dialectkit.ParseError naming the first unread token, or nil at
// the end of the statement.
func (p *parser) requireStatementEnd() error {
	for p.Current().Kind == tokenSemicolon {
		p.Advance()
	}
	err := p.RequireEnd()
	if err != nil {
		p.recordSyntaxError(err)
	}
	return err
}

// reportSyntaxError records a syntax error at the current token for a parsing step that
// has no error return of its own, such as the expression chain; the engine returns it
// once the statement has been walked.
//
// Takes message (string) which says what was wrong.
func (p *parser) reportSyntaxError(message string) {
	p.recordSyntaxError(dialectkit.NewParseError(p.Current(), message))
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

// parseIdentifierOrKeyword consumes an identifier or string literal and returns its
// value.
//
// Returns string which is the consumed identifier or string value.
// Returns error when the current token is neither an identifier nor a string literal.
func (p *parser) parseIdentifierOrKeyword() (string, error) {
	current := p.Current()
	if current.Kind == tokenIdentifier || current.Kind == tokenString {
		p.Position++
		return current.Value, nil
	}
	return "", dialectkit.NewParseError(current, "expected identifier")
}

// parseSchemaQualifiedName parses a possibly database-qualified name.
//
// For MySQL, the schema component corresponds to the database name.
//
// Returns schema (string) which is the database qualifier or empty when the name was
// unqualified.
// Returns name (string) which is the parsed identifier.
// Returns err (error) when an identifier cannot be parsed at the current position.
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

// skipParenthesised consumes a balanced parenthesised group at the current position
// without retaining its contents.
//
// Returns error when the opening parenthesis is missing or the group is unmatched.
func (p *parser) skipParenthesised() error {
	open := p.Current()
	if open.Kind != tokenLeftParen {
		return dialectkit.NewParseError(open, "expected '('")
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
		return dialectkit.NewParseError(open, unmatchedParenthesis)
	}
	return nil
}

// collectParenthesised consumes a balanced parenthesised group and returns the tokens it
// contained, excluding the outer parentheses.
//
// The result is a capacity-capped view of the statement's own token slice rather than a
// copy, so nested groups cost no allocation per level and an append by the caller cannot
// overwrite the tokens that follow the group.
//
// Returns []token which is the slice of inner tokens.
// Returns error when the opening parenthesis is missing or the group is unmatched.
func (p *parser) collectParenthesised() ([]token, error) {
	open := p.Current()
	if open.Kind != tokenLeftParen {
		return nil, dialectkit.NewParseError(open, "expected '('")
	}
	p.Advance()
	start := p.Position
	depth := 1
	for depth > 0 && !p.AtEnd() {
		switch p.Current().Kind {
		case tokenLeftParen:
			depth++
		case tokenRightParen:
			depth--
			if depth == 0 {
				end := p.Position
				p.Advance()
				return p.Tokens[start:end:end], nil
			}
		default:
		}
		p.Advance()
	}
	return nil, dialectkit.NewParseError(open, unmatchedParenthesis)
}

// skipParenthesisedIfPresent consumes a balanced parenthesised group when the current
// token is an opening parenthesis.
//
// Returns error when the group is unmatched.
func (p *parser) skipParenthesisedIfPresent() error {
	if p.Current().Kind != tokenLeftParen {
		return nil
	}
	return p.skipParenthesised()
}

// expectKeywordSequence consumes each of keywords in order, as in `DROP TABLE` or `DROP
// TRIGGER`.
//
// Takes keywords (...string) which are the keywords that must follow one another.
//
// Returns error when any keyword in the sequence is missing.
func (p *parser) expectKeywordSequence(keywords ...string) error {
	for _, keyword := range keywords {
		if _, err := p.ExpectKeyword(keyword); err != nil {
			return err
		}
	}
	return nil
}

// recordSyntaxError remembers err as the statement's syntax error unless an earlier one
// is already held, so the first problem met is the one reported.
//
// Takes err (error) which is the syntax error met by a step without an error return.
func (p *parser) recordSyntaxError(err error) {
	if p.syntaxError == nil {
		p.syntaxError = err
	}
}

// adoptSyntaxError carries a child parser's syntax error into this parser so a problem
// inside a subquery, CTE, or derived table is reported for the whole statement.
//
// Takes child (*parser) which is the finished child parser.
func (p *parser) adoptSyntaxError(child *parser) {
	if child.syntaxError != nil {
		p.recordSyntaxError(child.syntaxError)
	}
}

// registerParameterFromToken records a bind parameter encountered in the token stream,
// dispatching to named or sequential registration.
//
// Takes parameterToken (token) which is the parameter token being recorded.
// Takes context (sqlmodel.ParameterContext) which describes the syntactic context where
// the parameter occurs.
// Takes columnReference (*ast.ColumnReference) which is the column the parameter is bound
// against when known.
// Takes castType (*sqlmodel.SQLType) which is the explicit cast type when the parameter
// carries one.
//
// Returns int which is the assigned parameter number.
func (p *parser) registerParameterFromToken(
	parameterToken token,
	context sqlmodel.ParameterContext,
	columnReference *ast.ColumnReference,
	castType *sqlmodel.SQLType,
) int {
	return p.registerParameterFromTokenWithFunctionArgument(
		parameterToken, context, columnReference, castType, functionArgumentMetadata{})
}

// registerParameterFromTokenWithFunctionArgument records a bind parameter and
// additionally stamps the enclosing function/TVF argument metadata onto the resulting
// reference.
//
// Used by the flat-scan WHERE/HAVING resolver, which can compute the enclosing function
// name and argument ordinal directly from the token stream at registration time. The
// common registerParameterFromToken path forwards an empty functionArgumentMetadata so
// non-function placeholders are unaffected.
//
// Takes parameterToken (token) which is the parameter token being recorded.
// Takes context (sqlmodel.ParameterContext) which describes the syntactic context.
// Takes columnReference (*ast.ColumnReference) which is the bound column when known.
// Takes castType (*sqlmodel.SQLType) which is the explicit cast type when present.
// Takes functionArgument (functionArgumentMetadata) which carries the enclosing function
// name and argument ordinal, empty when the placeholder is not a function argument.
//
// Returns int which is the assigned parameter number, or the number the placeholder was
// first registered under when a parser of the statement registered it before.
func (p *parser) registerParameterFromTokenWithFunctionArgument(
	parameterToken token,
	context sqlmodel.ParameterContext,
	columnReference *ast.ColumnReference,
	castType *sqlmodel.SQLType,
	functionArgument functionArgumentMetadata,
) int {
	var number int
	switch {
	case parameterToken.Kind == tokenNamedParameter:
		number = p.registerNamedParameter(parameterToken, context, columnReference, castType, functionArgument)
	case parameterToken.Kind == tokenQuestionMark && len(parameterToken.Value) > 1:
		number = p.registerNumberedQuestionMark(parameterToken, context, columnReference, castType, functionArgument)
	default:
		number = p.registerSequentialParameter(context, columnReference, castType, functionArgument)
	}
	if registered, known := p.placeholderNumbers[parameterToken.Start]; known {
		return registered
	}
	p.placeholderNumbers[parameterToken.Start] = number
	return number
}

// registerNumberedQuestionMark records a '?N' positional bind parameter whose explicit
// index permits the same number to appear multiple times in the SQL while binding to a
// single argument slot. Reuse of an index appends another reference with the same Number
// so downstream merging treats them as one parameter.
//
// Takes parameterToken (token) which holds the '?N' token with its numeric suffix.
// Takes context (sqlmodel.ParameterContext) which describes the syntactic context where
// the parameter occurs.
// Takes columnReference (*ast.ColumnReference) which is the column the parameter is bound
// against when known.
// Takes castType (*sqlmodel.SQLType) which is the explicit cast type when the parameter
// carries one.
// Takes functionArgument (functionArgumentMetadata) which carries the enclosing function
// name and argument ordinal, empty when the placeholder is not a function argument.
//
// Returns int which is the parameter number from the token's numeric suffix.
func (p *parser) registerNumberedQuestionMark(
	parameterToken token,
	context sqlmodel.ParameterContext,
	columnReference *ast.ColumnReference,
	castType *sqlmodel.SQLType,
	functionArgument functionArgumentMetadata,
) int {
	number, parseError := strconv.Atoi(parameterToken.Value[1:])
	if parseError != nil || number <= 0 {
		return p.registerSequentialParameter(context, columnReference, castType, functionArgument)
	}
	p.parameterCount = max(p.parameterCount, number)
	p.parameterReferences = append(p.parameterReferences, ast.RawParameterReference{
		Number:                number,
		Context:               context,
		ColumnReference:       columnReference,
		CastType:              castType,
		EnclosingFunctionName: functionArgument.enclosingFunctionName,
		ArgumentOrdinal:       functionArgument.argumentOrdinal,
		Name:                  "",
	})
	return number
}

// registerSequentialParameter records a positional bind parameter and returns the freshly
// assigned number.
//
// Takes context (sqlmodel.ParameterContext) which describes the syntactic context where
// the parameter occurs.
// Takes columnReference (*ast.ColumnReference) which is the column the parameter is bound
// against when known.
// Takes castType (*sqlmodel.SQLType) which is the explicit cast type when the parameter
// carries one.
// Takes functionArgument (functionArgumentMetadata) which carries the enclosing function
// name and argument ordinal, empty when the placeholder is not a function argument.
//
// Returns int which is the newly assigned parameter number.
func (p *parser) registerSequentialParameter(
	context sqlmodel.ParameterContext,
	columnReference *ast.ColumnReference,
	castType *sqlmodel.SQLType,
	functionArgument functionArgumentMetadata,
) int {
	p.parameterCount++
	number := p.parameterCount
	p.parameterReferences = append(p.parameterReferences, ast.RawParameterReference{
		Number:                number,
		Context:               context,
		ColumnReference:       columnReference,
		CastType:              castType,
		EnclosingFunctionName: functionArgument.enclosingFunctionName,
		ArgumentOrdinal:       functionArgument.argumentOrdinal,
		Name:                  "",
	})
	return number
}

// registerNamedParameter records a named bind parameter, reusing the previously assigned
// number when the same name has been seen before.
//
// Takes parameterToken (token) which is the named parameter token.
// Takes context (sqlmodel.ParameterContext) which describes the syntactic context where
// the parameter occurs.
// Takes columnReference (*ast.ColumnReference) which is the column the parameter is bound
// against when known.
// Takes castType (*sqlmodel.SQLType) which is the explicit cast type when the parameter
// carries one.
// Takes functionArgument (functionArgumentMetadata) which carries the enclosing function
// name and argument ordinal, empty when the placeholder is not a function argument.
//
// Returns int which is the assigned (or reused) parameter number.
func (p *parser) registerNamedParameter(
	parameterToken token,
	context sqlmodel.ParameterContext,
	columnReference *ast.ColumnReference,
	castType *sqlmodel.SQLType,
	functionArgument functionArgumentMetadata,
) int {
	name := parameterToken.Value[1:]
	if existingNumber, exists := p.namedParameterMap[name]; exists {
		p.parameterReferences = append(p.parameterReferences, ast.RawParameterReference{
			Number:                existingNumber,
			Name:                  name,
			Context:               context,
			ColumnReference:       columnReference,
			CastType:              castType,
			EnclosingFunctionName: functionArgument.enclosingFunctionName,
			ArgumentOrdinal:       functionArgument.argumentOrdinal,
		})
		return existingNumber
	}
	p.parameterCount++
	number := p.parameterCount
	p.namedParameterMap[name] = number
	p.parameterReferences = append(p.parameterReferences, ast.RawParameterReference{
		Number:                number,
		Name:                  name,
		Context:               context,
		ColumnReference:       columnReference,
		CastType:              castType,
		EnclosingFunctionName: functionArgument.enclosingFunctionName,
		ArgumentOrdinal:       functionArgument.argumentOrdinal,
	})
	return number
}

// functionArgumentMetadata carries the enclosing function name and the zero-based
// argument slot of a placeholder that sits inside a function or table-valued function
// argument list.
//
// The analyser pairs these two facts to look up the matched function signature's declared
// argument type and back-propagate it onto an otherwise untyped placeholder. Both fields
// are zero-valued (empty name, zero ordinal) for the common non-function-argument case,
// leaving the placeholder unaffected.
type functionArgumentMetadata struct {
	// enclosingFunctionName is the lower-cased, optionally schema-qualified name of the
	// function whose argument list the placeholder sits in, recorded exactly as the engine
	// records the function name on RawTableValuedFunctionReference / FunctionCallExpression.
	// Empty when the placeholder is not a function argument.
	enclosingFunctionName string

	// argumentOrdinal is the zero-based position of the placeholder among the enclosing
	// call's top-level arguments (the argument slot, not the parameter number). Meaningful
	// only when enclosingFunctionName is non-empty.
	argumentOrdinal int
}

// classifyStatement returns the statementKind for a tokenised statement.
//
// Takes tokens ([]token) which is the tokenised statement to classify.
//
// Returns statementKind which is the inferred kind, or statementKindUnknown when no rule
// matches.
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

// classifyParenthesisedStatement returns the statementKind for a statement that opens
// with a parenthesis: a query whose first arm is in parentheses, as in (SELECT ...) UNION
// (SELECT ...), when the first word inside them starts a query.
//
// Takes tokens ([]token) which is the tokenised statement, opening with `(`.
//
// Returns statementKind which is statementKindSelect for a query, and
// statementKindUnknown otherwise.
func classifyParenthesisedStatement(tokens []token) statementKind {
	for _, current := range tokens {
		if current.Kind != tokenLeftParen {
			if startsQuery(current) {
				return statementKindSelect
			}
			return statementKindUnknown
		}
	}
	return statementKindUnknown
}

// classifyCreateStatement returns the statementKind for a CREATE statement.
//
// Takes tokens ([]token) which is the tokenised CREATE statement.
//
// Returns statementKind which is the kind of object being created, or
// statementKindUnknown when the object noun is unrecognised.
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

// skipCreatePrefixes advances past CREATE modifiers such as OR REPLACE, TEMPORARY,
// ALGORITHM = ..., DEFINER = ... and SQL SECURITY ..., in any order, to locate the
// object-kind keyword, so the forms mysqldump writes are classified.
//
// Takes tokens ([]token) which is the tokenised CREATE statement.
//
// Returns int which is the index of the object-kind keyword, or len(tokens) when the
// prefix is malformed.
func skipCreatePrefixes(tokens []token) int {
	index := 1
	if tokens[index].IsKeyword(tokenIdentifier, "OR") {
		if len(tokens) < minTokensForCreateOrReplace {
			return len(tokens)
		}
		index = indexAfterOrReplace
	}
	for index < len(tokens) {
		switch tokens[index].Keyword(tokenIdentifier) {
		case "TEMP", "TEMPORARY":
			index++
		case "ALGORITHM":
			index = skipCreateAssignment(tokens, index+1) + 1
		case "DEFINER":
			index = skipCreateDefiner(tokens, skipCreateAssignment(tokens, index+1))
		case keywordSQL:
			if index+1 >= len(tokens) || !tokens[index+1].IsKeyword(tokenIdentifier, "SECURITY") {
				return index
			}
			index += sqlSecurityTokenCount
		default:
			return index
		}
	}
	return len(tokens)
}

// skipCreateAssignment skips the = of a CREATE modifier such as ALGORITHM = MERGE.
//
// Takes tokens ([]token) which is the tokenised statement.
// Takes index (int) which is the position after the modifier's keyword.
//
// Returns int which is the position of the modifier's value.
func skipCreateAssignment(tokens []token, index int) int {
	if index < len(tokens) && tokens[index].Kind == tokenOperator && tokens[index].Value == "=" {
		return index + 1
	}
	return index
}

// skipCreateDefiner skips the user of a DEFINER modifier, such as `root`@`localhost`,
// 'app'@'%' or CURRENT_USER(), up to the next modifier or object keyword.
//
// Takes tokens ([]token) which is the tokenised statement.
// Takes index (int) which is the position of the user.
//
// Returns int which is the position of the next modifier or object keyword.
func skipCreateDefiner(tokens []token, index int) int {
	for index < len(tokens) {
		upper := tokens[index].Keyword(tokenIdentifier)
		if _, isObject := createObjectKinds[upper]; isObject || upper == keywordSQL || upper == "ALGORITHM" || upper == "EVENT" {
			return index
		}
		index++
	}
	return index
}

// classifyDropStatement returns the statementKind for a DROP statement, reading DROP
// TEMPORARY TABLE as a DROP TABLE.
//
// Takes tokens ([]token) which is the tokenised DROP statement.
//
// Returns statementKind which is the kind of object being dropped, or
// statementKindUnknown when the object noun is unrecognised.
func classifyDropStatement(tokens []token) statementKind {
	if len(tokens) < 2 {
		return statementKindUnknown
	}

	second := tokens[1].Keyword(tokenIdentifier)
	if second == "TEMPORARY" && len(tokens) > 2 && tokens[2].IsKeyword(tokenIdentifier, keywordTABLE) {
		return statementKindDropTable
	}
	switch second {
	case keywordTABLE:
		return statementKindDropTable
	case "VIEW":
		return statementKindDropView
	case "INDEX":
		return statementKindDropIndex
	case "TRIGGER":
		return statementKindDropTrigger
	case keywordDATABASE, keywordSCHEMA:
		return statementKindDropDatabase
	case keywordFUNCTION, keywordPROCEDURE:
		return statementKindDropFunction
	}

	return statementKindUnknown
}

// classifyAlterStatement returns the statementKind for an ALTER statement.
//
// Takes tokens ([]token) which is the tokenised ALTER statement.
//
// Returns statementKind which is statementKindAlterTable for ALTER TABLE and
// statementKindUnknown for every other ALTER target.
func classifyAlterStatement(tokens []token) statementKind {
	if len(tokens) < 2 {
		return statementKindUnknown
	}

	if tokens[1].IsKeyword(tokenIdentifier, keywordTABLE) {
		return statementKindAlterTable
	}

	return statementKindUnknown
}

// classifyRenameStatement returns the statementKind for a RENAME statement.
//
// Takes tokens ([]token) which is the tokenised RENAME statement.
//
// Returns statementKind which is statementKindRenameTable for RENAME TABLE and
// statementKindUnknown for anything else, such as RENAME USER.
func classifyRenameStatement(tokens []token) statementKind {
	if len(tokens) > 1 && tokens[1].IsKeyword(tokenIdentifier, keywordTABLE) {
		return statementKindRenameTable
	}
	return statementKindUnknown
}

// classifyWithStatement returns the statementKind for a WITH statement by scanning for
// the first DML keyword at outer scope.
//
// Takes tokens ([]token) which is the tokenised WITH statement.
//
// Returns statementKind which is the kind of the trailing DML, defaulting to
// statementKindSelect.
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
		if depth != 0 {
			continue
		}
		if kind, matched := dmlKeywords[current.Keyword(tokenIdentifier)]; matched {
			return kind
		}
	}
	return statementKindSelect
}

// isParameterToken reports whether the token kind represents a bind parameter
// placeholder.
//
// Takes kind (tokenKind) which is the token kind to test.
//
// Returns bool reporting whether the token denotes a bind parameter.
func isParameterToken(kind tokenKind) bool {
	return kind == tokenQuestionMark || kind == tokenNamedParameter
}

// unbalancedParenthesis finds a parenthesis of a statement that is not matched: the first
// closing one with no opening one before it, or else the innermost opening one never
// closed.
//
// The expression parsers stop at the end of the statement wherever they are, so without
// this check a statement such as SELECT (((1 would read as complete.
//
// Takes tokens ([]token) which are the statement's tokens.
//
// Returns error which is a dialectkit.ParseError at the unmatched parenthesis, or nil
// when every parenthesis is matched.
func unbalancedParenthesis(tokens []token) error {
	depth := 0
	for index := range tokens {
		switch tokens[index].Kind {
		case tokenLeftParen:
			depth++
		case tokenRightParen:
			if depth == 0 {
				return dialectkit.NewParseError(tokens[index], "unexpected ')'")
			}
			depth--
		default:
		}
	}
	if depth == 0 {
		return nil
	}
	closing := 0
	for _, current := range slices.Backward(tokens) {
		switch current.Kind {
		case tokenRightParen:
			closing++
		case tokenLeftParen:
			if closing == 0 {
				return dialectkit.NewParseError(current, unmatchedParenthesis)
			}
			closing--
		default:
		}
	}
	return nil
}

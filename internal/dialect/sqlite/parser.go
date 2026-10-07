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
	"strconv"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// messageUnmatchedParenthesis is the syntax error message for a parenthesised group that
	// lacks its closing parenthesis.
	messageUnmatchedParenthesis = "unmatched parenthesis"

	// messageExpectedExpression is the syntax error message for a token met where an
	// expression must start, such as a clause keyword or the end of the statement.
	messageExpectedExpression = "expected an expression"

	// messageExpectedCondition is the syntax error message for a WHERE, HAVING or ON keyword
	// with no condition after it.
	messageExpectedCondition = "expected a condition"

	// messageConditionLeftover is the syntax error message for a token left over after a
	// complete condition.
	messageConditionLeftover = "expected the end of the condition"
)

// statementKind classifies a top-level SQL statement by its leading keyword sequence.
type statementKind uint8

const (
	// statementKindCreateTable denotes a CREATE TABLE statement.
	statementKindCreateTable statementKind = iota

	// statementKindDropTable denotes a DROP TABLE statement.
	statementKindDropTable

	// statementKindAlterTable denotes an ALTER TABLE statement.
	statementKindAlterTable

	// statementKindCreateView denotes a CREATE VIEW statement.
	statementKindCreateView

	// statementKindDropView denotes a DROP VIEW statement.
	statementKindDropView

	// statementKindCreateIndex denotes a CREATE INDEX statement.
	statementKindCreateIndex

	// statementKindDropIndex denotes a DROP INDEX statement.
	statementKindDropIndex

	// statementKindCreateTrigger denotes a CREATE TRIGGER statement.
	statementKindCreateTrigger

	// statementKindDropTrigger denotes a DROP TRIGGER statement.
	statementKindDropTrigger

	// statementKindCreateVirtualTable denotes a CREATE VIRTUAL TABLE statement.
	statementKindCreateVirtualTable

	// statementKindSelect denotes a SELECT statement.
	statementKindSelect

	// statementKindInsert denotes an INSERT or REPLACE statement.
	statementKindInsert

	// statementKindUpdate denotes an UPDATE statement.
	statementKindUpdate

	// statementKindDelete denotes a DELETE statement.
	statementKindDelete

	// statementKindValues denotes a bare VALUES statement.
	statementKindValues

	// statementKindPragma denotes a PRAGMA statement.
	statementKindPragma

	// statementKindUnknown denotes any unrecognised statement shape.
	statementKindUnknown
)

var (
	// withBodyStatementKinds maps the keyword that follows a WITH clause to the matching
	// outer-statement kind.
	withBodyStatementKinds = map[string]statementKind{
		keywordSELECT: statementKindSelect,
		"INSERT":      statementKindInsert,
		"REPLACE":     statementKindInsert,
		"UPDATE":      statementKindUpdate,
		"DELETE":      statementKindDelete,
		keywordVALUES: statementKindValues,
	}
)

// parsedStatement holds the token slice and classified kind for one top-level SQL
// statement.
type parsedStatement struct {
	// source is the script the statement was read from; token offsets index into it.
	source string

	// tokens are the lexed tokens that form the statement body; for a statement over the
	// token budget, only those read before the budget was reached.
	tokens []token

	// span is where the statement sits in source, so a statement over the budget can be read
	// again to its end.
	span sqllex.Statement

	// kind records the classified statement shape.
	kind statementKind

	// overBudget reports a statement holding more tokens than the token budget it was
	// tokenised under, so tokens holds only its first ones.
	overBudget bool
}

// IsParsedStatement is a marker method that identifies the type as a parsed SQL
// statement.
func (*parsedStatement) IsParsedStatement() {}

// parser walks a token slice and produces analysis records describing the statement's
// parameters, derived tables, and other interesting clauses.
type parser struct {
	// parenthesisIndex answers the enclosing-parenthesis and enclosing-LIKE lookups the
	// parameter-context scans make; nil until first used.
	parenthesisIndex *dialectkit.ParenthesisScanIndex

	// groups answers the group, argument-slot and arity lookups the parameter-context scans
	// make; nil until first used.
	groups *parenthesisGroups

	// likeColumns holds the column each LIKE-style operator's left-hand side names, keyed by
	// the operator's token index, as the parameter-context scans worked it out; nil until
	// first used.
	likeColumns map[int]*ast.ColumnReference

	// castTypes holds the types placeholders take from the CAST type names the
	// parameter-context scans read, keyed by the index of the name's first token; nil until
	// first used.
	castTypes map[int]sqlmodel.SQLType

	// types normalises the type names the statement spells, such as a CAST target, through
	// the dialect's hooks and extra types; nil resolves through the built-in SQLite rules
	// alone.
	types *typeResolver

	// functions is the dialect's function catalogue, which types a placeholder compared with
	// a call whose result type does not depend on its arguments; nil when the parser was
	// built outside an engine.
	functions *ast.FunctionCatalogue

	// fullTextTables holds the lower-cased names of the catalogue's FTS5 tables, whose
	// table-named column and table-valued form take a full-text query; nil when unknown.
	fullTextTables map[string]bool

	// fullTextSources holds the lower-cased names and aliases by which the FROM clause being
	// analysed reads an FTS5 table; nil until one is read.
	fullTextSources map[string]bool

	// namedParameterMap remembers the parameter index assigned to each named placeholder for
	// re-use across references.
	namedParameterMap map[string]int

	// placeholderNumbers preserves assigned placeholder numbers when shared child parsers
	// revisit the same statement.
	placeholderNumbers map[int]int

	// scannedSubqueries maps the source offset of the parenthesis opening each subquery a
	// clause scan analysed to its analysis, shared with child parsers, so a parser that
	// records the clause's expressions without analysing its subqueries again still gives
	// each subquery its query.
	scannedSubqueries map[int]*ast.RawQueryAnalysis

	// columnNames holds the names a double-quoted name in the statement may name a column
	// by, shared with child parsers; nil reads every such name as a column, as for DDL.
	columnNames *columnNames

	// warnings maps the offset of the token each warning is about to the warning, shared
	// with child parsers, so a clause parsed twice warns once.
	warnings map[int]ast.Warning

	// insertTargetTable is the unqualified INSERT target table name used as the column
	// reference table alias for INSERT ... SELECT projection placeholders.
	//
	// The qualifier is load-bearing because a bare column name (for example "reason") may
	// exist on several tables. It is consumed and cleared alongside insertTargetColumns.
	insertTargetTable string

	// insertTargetColumns holds the target column names of an enclosing INSERT ... SELECT so
	// the SELECT body's top-level projection placeholders bind to the INSERT target column
	// positionally, mirroring the VALUES path.
	//
	// It is consumed and cleared by the first parseSelectList so nested subqueries and
	// compound branches do not inherit the binding.
	insertTargetColumns []string

	// rawDerivedTables collects every subquery used as a derived table.
	rawDerivedTables []ast.RawDerivedTableReference

	// predicateSubqueries collects every subquery found in a token-scanned predicate
	// position (WHERE / HAVING / JOIN ON), so the analyser can resolve its parameters
	// against the subquery's own scope rather than the parent.
	predicateSubqueries []*ast.RawQueryAnalysis

	// rawTableValuedFunctions collects every table-valued function call.
	rawTableValuedFunctions []ast.RawTableValuedFunctionReference

	// fromOrder receives the names the items of the FROM clause being parsed are visible
	// under, in order; nil outside a SELECT's FROM clause.
	fromOrder *[]string

	// parameterReferences collects every observed parameter reference.
	parameterReferences []ast.RawParameterReference

	// castTypeStops holds, for each token position, the index of the first ")" or AS after
	// it, where the search for a CAST's type stops; nil until first used.
	castTypeStops []int

	// spans records the spans of the top-level statement; it is nil in every other parser,
	// so a nested query's clauses are never recorded as the statement's.
	spans *dialectkit.SpanRecorder

	// clauseExpressions collects the expressions of the clauses parsed so far whose query
	// has not taken them yet.
	clauseExpressions []ast.ClauseExpression

	// itemSpans holds the spans of the items of the select list or RETURNING clause parsed
	// last.
	itemSpans []ast.ItemSpan

	// syntaxError holds the first syntax error met by a parsing step that has no error
	// return of its own, such as the expression chain. The engine returns it once the
	// statement has been walked; nil when no such error occurred.
	syntaxError error

	// Cursor walks the statement's tokens; its keyword matching never treats a delimited
	// identifier as a keyword.
	dialectkit.Cursor[tokenKind]

	// parameterCount is the highest parameter index assigned so far.
	parameterCount int

	// analysisDepth bounds recursion through analyseSelect across compound branches, CTE
	// bodies, derived tables, scalar subqueries, and view bodies. Child parsers inherit the
	// parent depth so the global cap holds across instances.
	analysisDepth int

	// expressionDepth bounds recursion through the expression precedence chain so deeply
	// nested parentheses cannot overflow the stack.
	expressionDepth int

	// maxParseDepth is the effective cap for analysisDepth and expressionDepth, which keeps
	// deeply nested input from overflowing the goroutine stack. The engine sets it from the
	// dialect and child parsers inherit it; newParser seeds it with
	// dialectkit.DefaultMaxParseDepth.
	maxParseDepth int

	// aliasStart is the index of the token a select-list item's alias starts at; -1 before
	// the alias is looked for.
	aliasStart int

	// suppressClauses stops clause expressions being recorded, inside a clause whose names
	// the analyser must not check, such as an upsert action naming the excluded row.
	suppressClauses bool

	// skipSubqueries leaves the subqueries an expression holds unanalysed, in a parser that
	// only records the names of a clause another scan has analysed already.
	skipSubqueries bool

	// endsInValues reports that the arm of a query parsed last is a VALUES list, after which
	// SQLite takes no ORDER BY or LIMIT.
	endsInValues bool
}

// newParser creates a parser over the token stream seeded with the default depth cap.
//
// Takes tokens ([]token) which is the lexed token stream to walk.
//
// Returns *parser which is ready to analyse the statement.
func newParser(tokens []token) *parser {
	return &parser{
		parenthesisIndex:        nil,
		groups:                  nil,
		likeColumns:             nil,
		castTypes:               nil,
		castTypeStops:           nil,
		types:                   nil,
		functions:               nil,
		fullTextTables:          nil,
		fullTextSources:         nil,
		Cursor:                  dialectkit.NewCursor(tokens, tokenIdentifier, tokenEOF),
		maxParseDepth:           dialectkit.DefaultMaxParseDepth,
		namedParameterMap:       make(map[string]int),
		placeholderNumbers:      make(map[int]int),
		scannedSubqueries:       make(map[int]*ast.RawQueryAnalysis),
		insertTargetTable:       "",
		insertTargetColumns:     nil,
		rawDerivedTables:        nil,
		predicateSubqueries:     nil,
		rawTableValuedFunctions: nil,
		fromOrder:               nil,
		parameterReferences:     nil,
		spans:                   nil,
		clauseExpressions:       nil,
		itemSpans:               nil,
		syntaxError:             nil,
		parameterCount:          0,
		analysisDepth:           0,
		expressionDepth:         0,
		aliasStart:              -1,
		suppressClauses:         false,
		skipSubqueries:          false,
		columnNames:             nil,
		warnings:                make(map[int]ast.Warning),
		endsInValues:            false,
	}
}

// tokenAt returns the token at an absolute position.
//
// Takes index (int) which is the absolute token index to read.
//
// Returns token which is the token at the index, or the end-of-input token when out of
// range.
func (p *parser) tokenAt(index int) token {
	return p.PeekAt(index - p.Position)
}

// parseIdentifierOrKeyword consumes the next identifier or string literal and returns its
// value.
//
// Returns string which is the value of the consumed token.
// Returns error when the current token is neither an identifier nor a string literal.
func (p *parser) parseIdentifierOrKeyword() (string, error) {
	current := p.Current()
	if current.Kind == tokenIdentifier || current.Kind == tokenString {
		p.Position++
		return current.Value, nil
	}
	return "", dialectkit.NewParseError(current, "expected an identifier")
}

// skipParenthesised consumes a balanced parenthesised group and discards its contents.
//
// Returns error when the opening parenthesis is missing or the group is unmatched.
func (p *parser) skipParenthesised() error {
	if p.Current().Kind != tokenLeftParen {
		return dialectkit.NewParseError(p.Current(), "expected (")
	}
	opening := p.Current()
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
		return dialectkit.NewParseError(opening, messageUnmatchedParenthesis)
	}
	return nil
}

// collectParenthesised consumes a balanced parenthesised group and returns the tokens
// between the parentheses.
//
// The result is a capacity-capped view of the statement's own token slice rather than a
// copy, so nested groups cost no allocation per level and an append by the caller cannot
// overwrite the tokens that follow the group.
//
// Returns []token which holds the inner tokens of the group.
// Returns error when the opening parenthesis is missing or the group is unmatched.
func (p *parser) collectParenthesised() ([]token, error) {
	if p.Current().Kind != tokenLeftParen {
		return nil, dialectkit.NewParseError(p.Current(), "expected (")
	}
	opening := p.Advance()
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
	return nil, dialectkit.NewParseError(opening, messageUnmatchedParenthesis)
}

// expectKeywordSequence consumes each of keywords in order, as in `DROP TABLE` or `CREATE
// VIRTUAL TABLE`.
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

// requireEnd checks that the analysis consumed the whole statement, allowing only a
// trailing semicolon, so tokens the grammar does not understand fail the statement rather
// than silently dropping the columns and parameters they hold.
//
// Returns error which is a dialectkit.ParseError naming the first unparsed token, or nil
// at the end of the statement.
func (p *parser) requireEnd() error {
	for p.Current().Kind == tokenSemicolon {
		p.Advance()
	}
	return p.RequireEnd()
}

// newChildParser returns a parser over a nested statement's tokens, such as a CTE body,
// derived table or subquery, that continues this parser's parameter numbering and depth,
// keeps the numbers its placeholders were registered under, and shares its dialect
// configuration.
//
// Takes tokens ([]token) which are the nested statement's tokens.
//
// Returns *parser which is ready to analyse the nested statement.
func (p *parser) newChildParser(tokens []token) *parser {
	child := newParser(tokens)
	child.Interrupt = p.Interrupt
	child.parameterCount = p.parameterCount
	child.placeholderNumbers = p.placeholderNumbers
	child.scannedSubqueries = p.scannedSubqueries
	child.analysisDepth = p.analysisDepth
	child.expressionDepth = p.expressionDepth
	child.maxParseDepth = p.maxParseDepth
	child.types = p.types
	child.functions = p.functions
	child.fullTextTables = p.fullTextTables
	child.columnNames = p.columnNames
	child.warnings = p.warnings
	return child
}

// analyseNested analyses a nested SELECT in a child parser and requires it to consume
// every token of the nested statement.
//
// The child shares this parser's named parameters, so a name written both inside and
// outside the nested statement is one parameter, as SQLite binds it. The child's syntax
// error, if any, is carried into this parser.
//
// Takes tokens ([]token) which are the nested statement's tokens, without the enclosing
// parentheses.
//
// Returns *parser which is the finished child parser, whose parameters the caller merges.
// Returns *ast.RawQueryAnalysis which describes the nested statement.
// Returns error when the nested statement fails to parse or leaves tokens unparsed.
func (p *parser) analyseNested(tokens []token) (*parser, *ast.RawQueryAnalysis, error) {
	child := p.newChildParser(tokens)
	child.namedParameterMap = p.namedParameterMap
	analysis, err := child.analyseSelect()
	if err == nil {
		err = child.RequireEnd()
	}
	p.adoptSyntaxError(child)
	return child, analysis, err
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

// registerParameterFromToken dispatches parameter registration based on the placeholder
// token kind.
//
// Takes parameterToken (token) which is the placeholder token.
// Takes context (sqlmodel.ParameterContext) which records where the parameter appears in
// the statement.
// Takes columnReference (*ast.ColumnReference) which links the parameter to its target
// column when known.
// Takes castType (*sqlmodel.SQLType) which holds an explicit CAST target when present.
//
// Returns int which is the resolved parameter index, or the index the placeholder was
// first registered under when a parser of the statement registered it before.
func (p *parser) registerParameterFromToken(
	parameterToken token,
	context sqlmodel.ParameterContext,
	columnReference *ast.ColumnReference,
	castType *sqlmodel.SQLType,
) int {
	var number int
	switch parameterToken.Kind {
	case tokenNumberedParameter:
		number = p.registerNumberedParameter(parameterToken, context, columnReference, castType)
	case tokenNamedParameter:
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

// registerSequentialParameter assigns the next available index to an anonymous
// placeholder.
//
// Takes context (sqlmodel.ParameterContext) which records where the parameter appears in
// the statement.
// Takes columnReference (*ast.ColumnReference) which links the parameter to its target
// column when known.
// Takes castType (*sqlmodel.SQLType) which holds an explicit CAST target when present.
//
// Returns int which is the newly assigned parameter index.
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

// registerNumberedParameter records a `?N` placeholder reference.
//
// Takes parameterToken (token) which carries the `?N` literal.
// Takes context (sqlmodel.ParameterContext) which records where the parameter appears in
// the statement.
// Takes columnReference (*ast.ColumnReference) which links the parameter to its target
// column when known.
// Takes castType (*sqlmodel.SQLType) which holds an explicit CAST target when present.
//
// Returns int which is the parameter index parsed from the token.
func (p *parser) registerNumberedParameter(
	parameterToken token,
	context sqlmodel.ParameterContext,
	columnReference *ast.ColumnReference,
	castType *sqlmodel.SQLType,
) int {
	number, parseError := strconv.Atoi(parameterToken.Value[1:])
	if parseError != nil || number <= 0 {
		return p.registerSequentialParameter(context, columnReference, castType)
	}
	p.parameterCount = max(p.parameterCount, number)
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

// registerNamedParameter records a named placeholder, reusing the index when the name has
// been seen before.
//
// Takes parameterToken (token) which carries the named placeholder.
// Takes context (sqlmodel.ParameterContext) which records where the parameter appears in
// the statement.
// Takes columnReference (*ast.ColumnReference) which links the parameter to its target
// column when known.
// Takes castType (*sqlmodel.SQLType) which holds an explicit CAST target when present.
//
// Returns int which is the index associated with the parameter name.
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
	p.parameterCount++
	number := p.parameterCount
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

// classifyStatement identifies the statement kind from its leading keyword.
//
// Takes tokens ([]token) which holds the statement tokens.
//
// Returns statementKind which describes the classified shape.
func classifyStatement(tokens []token) statementKind {
	if len(tokens) == 0 {
		return statementKindUnknown
	}

	first := tokens[0].Keyword(tokenIdentifier)

	switch first {
	case keywordCREATE:
		return classifyCreateStatement(tokens)
	case keywordDROP:
		return classifyDropStatement(tokens)
	case "ALTER":
		return statementKindAlterTable
	case keywordSELECT:
		return statementKindSelect
	case "INSERT", "REPLACE":
		return statementKindInsert
	case "UPDATE":
		return statementKindUpdate
	case "DELETE":
		return statementKindDelete
	case keywordVALUES:
		return statementKindValues
	case "PRAGMA":
		return statementKindPragma
	case keywordWITH:
		return classifyWithStatement(tokens)
	}

	return statementKindUnknown
}

// classifyCreateStatement resolves the variant of a CREATE statement.
//
// Takes tokens ([]token) which begins with the CREATE keyword.
//
// Returns statementKind which is the specific create variant.
func classifyCreateStatement(tokens []token) statementKind {
	if len(tokens) < 2 {
		return statementKindUnknown
	}

	second := tokens[1].Keyword(tokenIdentifier)
	switch second {
	case keywordTABLE:
		return statementKindCreateTable
	case "TEMP", "TEMPORARY":
		return classifyCreateTempStatement(tokens)
	case "VIEW":
		return statementKindCreateView
	case "INDEX", keywordUNIQUE:
		return statementKindCreateIndex
	case "VIRTUAL":
		return statementKindCreateVirtualTable
	case keywordTRIGGER:
		return statementKindCreateTrigger
	}

	return statementKindUnknown
}

// classifyCreateTempStatement resolves CREATE TEMP and CREATE TEMPORARY variants.
//
// Takes tokens ([]token) which begins with CREATE TEMP or CREATE TEMPORARY.
//
// Returns statementKind which is the specific temporary create variant.
func classifyCreateTempStatement(tokens []token) statementKind {
	if len(tokens) < minTokensForClassification {
		return statementKindUnknown
	}

	third := tokens[2].Keyword(tokenIdentifier)
	switch third {
	case keywordTABLE:
		return statementKindCreateTable
	case "VIEW":
		return statementKindCreateView
	case keywordTRIGGER:
		return statementKindCreateTrigger
	}

	return statementKindUnknown
}

// classifyDropStatement resolves the variant of a DROP statement.
//
// Takes tokens ([]token) which begins with the DROP keyword.
//
// Returns statementKind which is the specific drop variant.
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
	case keywordTRIGGER:
		return statementKindDropTrigger
	}

	return statementKindUnknown
}

// classifyWithStatement resolves the outer statement that follows a WITH clause.
//
// Takes tokens ([]token) which begins with the WITH keyword.
//
// Returns statementKind which is the outer DML kind, defaulting to SELECT.
func classifyWithStatement(tokens []token) statementKind {
	depth := 0
	for _, current := range tokens {
		switch current.Kind {
		case tokenLeftParen:
			depth++
		case tokenRightParen:
			depth--
		case tokenIdentifier:
			if depth != 0 {
				continue
			}
			if kind, found := withBodyStatementKinds[current.Keyword(tokenIdentifier)]; found {
				return kind
			}
		default:
		}
	}
	return statementKindSelect
}

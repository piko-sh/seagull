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
	"slices"
	"strconv"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/postgres/pgext"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// minTokensForCreateOrReplace is the minimum token count for a CREATE OR REPLACE form
	// before the object keyword can appear.
	minTokensForCreateOrReplace = 4

	// minCommentColumnParts is the number of dotted parts the column of a COMMENT ON COLUMN
	// has at least: its table and its name.
	minCommentColumnParts = 2

	// maxCommentColumnParts is the number of dotted parts the column of a COMMENT ON COLUMN
	// has at most: its database, schema, table and name.
	maxCommentColumnParts = 4

	// indexAfterOrReplace is the token index of the object keyword after CREATE OR REPLACE.
	indexAfterOrReplace = 3
)

// statementKind enumerates the top-level SQL statement forms the parser classifies. The
// built-in kinds are unexported; a pgext.StatementExtensionPort names its own kinds in
// the reserved range from pgext.StatementKindExtensionBase, and they are dispatched
// through the extension registry rather than the built-in handlers.
type statementKind = pgext.StatementKind

const (
	// statementKindUnknown marks a statement whose form cannot be classified from its
	// leading tokens. Placed first (value 0) so the zero value of statementKind is a safe
	// decline sentinel for extension Classify callbacks; no built-in kind can accidentally
	// share the decline value.
	statementKindUnknown statementKind = iota

	// statementKindCreateTable marks a CREATE TABLE statement.
	statementKindCreateTable

	// statementKindDropTable marks a DROP TABLE statement.
	statementKindDropTable

	// statementKindAlterTable marks an ALTER TABLE statement.
	statementKindAlterTable

	// statementKindCreateView marks a CREATE VIEW or MATERIALIZED VIEW statement.
	statementKindCreateView

	// statementKindDropView marks a DROP VIEW or DROP MATERIALIZED VIEW statement.
	statementKindDropView

	// statementKindAlterView identifies an ALTER VIEW or ALTER MATERIALIZED VIEW statement.
	statementKindAlterView

	// statementKindCreateIndex marks a CREATE INDEX statement.
	statementKindCreateIndex

	// statementKindDropIndex marks a DROP INDEX statement.
	statementKindDropIndex

	// statementKindCreateTrigger marks a CREATE TRIGGER statement.
	statementKindCreateTrigger

	// statementKindDropTrigger marks a DROP TRIGGER statement.
	statementKindDropTrigger

	// statementKindCreateType marks a CREATE TYPE statement.
	statementKindCreateType

	// statementKindAlterType marks an ALTER TYPE statement.
	statementKindAlterType

	// statementKindDropType marks a DROP TYPE statement.
	statementKindDropType

	// statementKindCreateFunction marks a CREATE FUNCTION or PROCEDURE statement.
	statementKindCreateFunction

	// statementKindDropFunction marks a DROP FUNCTION or PROCEDURE statement.
	statementKindDropFunction

	// statementKindCreateSchema marks a CREATE SCHEMA statement.
	statementKindCreateSchema

	// statementKindDropSchema marks a DROP SCHEMA statement.
	statementKindDropSchema

	// statementKindCreateExtension marks a CREATE EXTENSION statement.
	statementKindCreateExtension

	// statementKindDropExtension marks a DROP EXTENSION statement.
	statementKindDropExtension

	// statementKindCreateSequence marks a CREATE SEQUENCE statement.
	statementKindCreateSequence

	// statementKindDropSequence marks a DROP SEQUENCE statement.
	statementKindDropSequence

	// statementKindComment marks a COMMENT statement.
	statementKindComment

	// statementKindSelect marks a SELECT statement.
	statementKindSelect

	// statementKindInsert marks an INSERT statement.
	statementKindInsert

	// statementKindUpdate marks an UPDATE statement.
	statementKindUpdate

	// statementKindDelete marks a DELETE statement.
	statementKindDelete

	// statementKindValues marks a standalone VALUES statement.
	statementKindValues

	// statementKindCall marks a CALL statement, which invokes a procedure.
	statementKindCall

	// statementKindSetSearchPath marks a SET or RESET of the search path.
	statementKindSetSearchPath

	// statementKindAlterSequence marks an ALTER SEQUENCE statement.
	statementKindAlterSequence

	// statementKindCount is the sentinel count of statement kinds.
	statementKindCount
)

var (
	// firstWordClassifiers maps a statement's leading keyword to the classifier that
	// resolves its specific kind by inspecting subsequent tokens.
	firstWordClassifiers = map[string]func([]token) statementKind{
		keywordCREATE: classifyCreateStatement,
		keywordDROP:   classifyDropStatement,
		"ALTER":       classifyAlterStatement,
		keywordWITH:   classifyWithStatement,
		keywordSET:    classifySetStatement,
		"RESET":       classifyResetStatement,
	}

	// firstWordStaticKinds maps a statement's leading keyword directly to its kind when no
	// further inspection is required.
	firstWordStaticKinds = map[string]statementKind{
		keywordSELECT: statementKindSelect,
		keywordTABLE:  statementKindSelect,
		"INSERT":      statementKindInsert,
		"UPDATE":      statementKindUpdate,
		"DELETE":      statementKindDelete,
		keywordVALUES: statementKindValues,
		"COMMENT":     statementKindComment,
		"CALL":        statementKindCall,
	}

	// createObjectKinds maps each CREATE object keyword to its specific statement kind so
	// the CREATE-statement classifier can avoid a long switch.
	createObjectKinds = map[string]statementKind{
		keywordTABLE:   statementKindCreateTable,
		"VIEW":         statementKindCreateView,
		"MATERIALIZED": statementKindCreateView,
		"INDEX":        statementKindCreateIndex,
		keywordUNIQUE:  statementKindCreateIndex,
		keywordTYPE:    statementKindCreateType,
		"DOMAIN":       statementKindCreateType,
		"FUNCTION":     statementKindCreateFunction,
		"PROCEDURE":    statementKindCreateFunction,
		keywordSCHEMA:  statementKindCreateSchema,
		"EXTENSION":    statementKindCreateExtension,
		"TRIGGER":      statementKindCreateTrigger,
		"CONSTRAINT":   statementKindCreateTrigger,
		"SEQUENCE":     statementKindCreateSequence,
	}

	// dmlKeywords maps each DML keyword to its statement kind for use when classifying CTE
	// bodies.
	dmlKeywords = map[string]statementKind{
		keywordSELECT: statementKindSelect,
		"INSERT":      statementKindInsert,
		"UPDATE":      statementKindUpdate,
		"DELETE":      statementKindDelete,
		keywordVALUES: statementKindValues,
	}
)

// parsedStatement holds the tokens and classification of a single SQL statement extracted
// from a larger script.
type parsedStatement struct {
	// source is the script the statement was read from; token offsets index into it.
	source string

	// tokens holds the statement's tokens excluding the trailing semicolon.
	tokens []token

	// extensionOwner is the index into settings.StatementExtensions of the extension that
	// claimed this statement during ParseStatements, or -1 when the kind is a built-in.
	//
	// Caching the index here lets dispatchDDL look up the owning extension in O(1) rather
	// than re-running Classify against every registered extension on every apply call. The
	// index becomes meaningless if the extension list is reordered between parse and
	// dispatch, but the Dialect treats the list as immutable for the lifetime of the engine.
	extensionOwner int

	// start is the byte offset in source of the statement's first significant token.
	start int

	// end is the byte offset in source just past the statement's last significant token.
	end int

	// kind tags the statement's top-level form.
	kind statementKind

	// overBudget reports a statement holding more tokens than the per-statement budget,
	// whose tokens hold only those read before the budget was reached.
	overBudget bool
}

// IsParsedStatement marks the receiver as a parsed statement value.
func (*parsedStatement) IsParsedStatement() {}

// syntaxState holds the first syntax error any parser over one statement hits.
//
// A parser and the child parsers it spawns for subqueries and CTE bodies share one
// syntaxState, so a malformed construct inside a subquery fails the whole statement even
// when the subquery's caller otherwise falls back to an unknown expression.
type syntaxState struct {
	// err is the first syntax error recorded, or nil.
	err error

	// depthReached records that an expression nested to the parser's depth limit, which
	// fails the statement, so no parser over the statement descends into another expression
	// and a deep statement fails in time proportional to its length.
	depthReached bool
}

// parser holds the mutable state used while walking a token stream and extracting
// parameter and table references.
type parser struct {
	// syntax is the statement-wide syntax error record shared with child parsers.
	syntax *syntaxState

	// types resolves type names in casts through the dialect's types, extra types and hooks;
	// nil resolves through the built-in PostgreSQL types alone.
	types *typeResolver

	// functions is the dialect's built-in function catalogue, which types a parameter
	// compared with a built-in call; nil leaves such a parameter typed from its column.
	functions *ast.FunctionCatalogue

	// catalogue is the schema the query runs against, which tells a user-defined function
	// called in a FROM clause apart by what it returns; nil for DDL.
	catalogue *sqlmodel.Catalogue

	// parenthesisIndex answers the enclosing-parenthesis and enclosing-LIKE lookups the
	// parameter-context scans make; nil until first used.
	parenthesisIndex *dialectkit.ParenthesisScanIndex

	// namedParameterMap deduplicates named parameters by mapping each name to its assigned
	// positional number.
	namedParameterMap map[string]int

	// placeholderNumbers preserves assigned placeholder numbers when shared child parsers
	// revisit the same statement.
	placeholderNumbers map[int]int

	// scannedSubqueries maps the source offset of the parenthesis opening each subquery a
	// clause scan analysed to its analysis, shared with child parsers, so a parser that
	// records the clause's expressions without analysing its subqueries again still gives
	// each subquery its query.
	scannedSubqueries map[int]*ast.RawQueryAnalysis

	// rowLocked records that a query anywhere in the statement carried a row-locking clause
	// (FOR UPDATE, FOR NO KEY UPDATE, FOR SHARE or FOR KEY SHARE). Every child parser shares
	// it, so a lock in a CTE body, a derived table or a subquery keeps the statement off a
	// read replica.
	rowLocked *bool

	// source is the script the statement's tokens were read from, so captured text can be
	// sliced from it exactly as written; empty when the tokens were built without it.
	source string

	// insertProjectionTable is the INSERT target table whose columns the current INSERT ...
	// SELECT projection items are assigned to.
	//
	// Empty when the parser is not inside an INSERT ... SELECT projection list.
	insertProjectionTable string

	// primaryKeySharding records how USING HASH shards the primary key of the CREATE TABLE
	// being parsed; its zero value records an unsharded key.
	primaryKeySharding keySharding

	// parameterReferences collects every parameter reference found in the statement.
	parameterReferences []ast.RawParameterReference

	// rawDerivedTables collects derived-table references found while walking FROM clauses.
	rawDerivedTables []ast.RawDerivedTableReference

	// predicateSubqueries collects subqueries found in a token-scanned predicate position
	// (WHERE / HAVING / JOIN ON), so the analyser can resolve each one's parameters against
	// the subquery's own scope rather than the parent.
	predicateSubqueries []*ast.RawQueryAnalysis

	// rawTableValuedFunctions collects table-valued function references found while walking
	// FROM clauses.
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

	// insertProjectionColumns is the ordered INSERT target column list, used to map each
	// top-level INSERT ... SELECT projection item positionally onto its target column.
	//
	// Nil when the parser is not inside an INSERT ... SELECT projection list.
	insertProjectionColumns []string

	// insertKeywords holds the upper-cased words a flavour adds to INSERT as the opening
	// word of an INSERT, such as UPSERT.
	insertKeywords []string

	// paginationValues holds the values of the statement's own LIMIT and OFFSET clauses,
	// each ranked as PostgreSQL types its parameters.
	paginationValues []rankedTree

	// unanalysedParts holds the parts of a DDL statement the parser did not analyse, such as
	// a view body nested beyond the depth limit, which the statement's mutation reports.
	unanalysedParts []ast.Warning

	// Cursor walks the statement's tokens; its keyword matching never matches a quoted
	// identifier.
	dialectkit.Cursor[tokenKind]

	// parameterCount tracks the highest positional parameter number assigned so far.
	parameterCount int

	// highestNumberedPlaceholder is the highest number a numbered placeholder, such as $2,
	// is written with anywhere in the statement, which every named placeholder is numbered
	// after, so a name never shares a number written explicitly later in the statement.
	highestNumberedPlaceholder int

	// aliasStart is the index of the token an output column's alias starts at, or of the
	// token after its expression when it has none; -1 before the alias is looked for.
	aliasStart int

	// insertProjectionIndex is the zero-based ordinal of the INSERT ... SELECT projection
	// item currently being parsed.
	//
	// It advances once per top-level projection item, so a literal or column item still
	// consumes its target-column slot.
	insertProjectionIndex int

	// analysisDepth tracks recursion through analyseSelect across CTE bodies, compound
	// (UNION/INTERSECT/EXCEPT) branches, derived tables, scalar subqueries, and view bodies.
	//
	// When the counter exceeds maxParseDepth the parser returns an error wrapping
	// dialectkit.ErrParseDepth so a pathologically deep query cannot blow the goroutine
	// stack. Child parsers spawned for nested constructs inherit the parent's depth so the
	// global recursion bound holds across parser instances.
	analysisDepth int

	// expressionDepth bounds recursion through the expression precedence chain so deeply
	// nested parentheses cannot overflow the stack. Mirrors analysisDepth: incremented
	// before the recursive descent and decremented after.
	expressionDepth int

	// maxParseDepth is the effective cap for analysisDepth and expressionDepth;
	// dialectkit.DefaultMaxParseDepth until the engine sets it from the dialect's limits,
	// and child parsers inherit it via newChildParser so the global cap holds across
	// instances.
	maxParseDepth int

	// hasDataModifyingCTE records that a CTE contained INSERT, UPDATE, or DELETE.
	hasDataModifyingCTE bool

	// lastArgumentWasVariadic records that the last function argument parsed used the
	// VARIADIC keyword.
	lastArgumentWasVariadic bool

	// suppressClauses stops clause expressions being recorded, inside a clause whose names
	// the analyser must not check, such as an ON CONFLICT action naming the EXCLUDED row.
	suppressClauses bool

	// skipSubqueries leaves the subqueries an expression holds unanalysed, in a parser that
	// only records the names of a clause another scan has analysed already.
	skipSubqueries bool

	// aliasNamesSingleColumnFunctions reports whether the alias of a table function that
	// returns one column names that column whatever the function calls it.
	aliasNamesSingleColumnFunctions bool

	// indexOptions reports whether a primary key or unique constraint may carry USING HASH
	// and STORING (...).
	indexOptions bool

	// computedColumnShorthand reports whether AS (expression) STORED or VIRTUAL after a
	// column's type declares a computed column.
	computedColumnShorthand bool

	// notVisibleColumns reports whether NOT VISIBLE hides a column from SELECT *.
	notVisibleColumns bool

	// asOfSystemTime reports whether AS OF SYSTEM TIME may follow the FROM clause of a
	// query.
	asOfSystemTime bool

	// parametersTypedFromContext reports that the engine types a parameter from what its
	// place wants, so only a test for NULL leaves one untyped.
	parametersTypedFromContext bool
}

// newParser creates a parser over tokens with the default recursion cap.
//
// Takes tokens ([]token) which is the token stream to walk.
//
// Returns *parser which is ready to analyse the supplied tokens.
func newParser(tokens []token) *parser {
	return &parser{
		syntax:                  new(syntaxState),
		types:                   nil,
		functions:               nil,
		catalogue:               nil,
		parenthesisIndex:        nil,
		source:                  "",
		Cursor:                  dialectkit.NewCursor(tokens, tokenIdentifier, tokenEOF),
		maxParseDepth:           dialectkit.DefaultMaxParseDepth,
		namedParameterMap:       make(map[string]int),
		placeholderNumbers:      make(map[int]int),
		scannedSubqueries:       make(map[int]*ast.RawQueryAnalysis),
		insertProjectionTable:   "",
		primaryKeySharding:      keySharding{buckets: "", hashed: false},
		parameterReferences:     nil,
		rawDerivedTables:        nil,
		predicateSubqueries:     nil,
		rawTableValuedFunctions: nil,
		fromOrder:               nil,
		argumentNames:           nil,
		spans:                   nil,
		clauseExpressions:       nil,
		itemSpans:               nil,
		aliasStart:              -1,
		insertProjectionColumns: nil,
		parameterCount:          0,

		highestNumberedPlaceholder: 0,
		insertProjectionIndex:      0,
		analysisDepth:              0,
		expressionDepth:            0,
		rowLocked:                  new(bool),
		hasDataModifyingCTE:        false,
		lastArgumentWasVariadic:    false,
		suppressClauses:            false,
		insertKeywords:             nil,
		skipSubqueries:             false,

		aliasNamesSingleColumnFunctions: false,
		indexOptions:                    false,
		computedColumnShorthand:         false,
		notVisibleColumns:               false,
		asOfSystemTime:                  false,
		paginationValues:                nil,
		unanalysedParts:                 nil,
		parametersTypedFromContext:      false,
	}
}

// newChildParser creates a parser over tokens that inherits the recursion state required
// to keep the global depth caps intact across parser instances.
//
// Nested constructs (derived tables, scalar subqueries, EXISTS subqueries, CTE bodies,
// view bodies, IN-list subqueries) spawn a fresh parser; routing every such spawn through
// this helper guarantees the child continues counting from the parent's analysisDepth and
// shares the same maxParseDepth, so no nesting path can reset the cap to zero and recurse
// without bound. parameterCount is also carried so positional bind numbers stay monotonic
// across the boundary.
//
// Takes tokens ([]token) which is the child statement's token slice.
//
// Returns *parser which is the child parser pre-seeded with the inherited depth and
// parameter state.
func (p *parser) newChildParser(tokens []token) *parser {
	child := newParser(tokens)
	child.Interrupt = p.Interrupt
	child.syntax = p.syntax
	child.types = p.types
	child.functions = p.functions
	child.catalogue = p.catalogue
	child.source = p.source
	child.parameterCount = p.parameterCount
	child.highestNumberedPlaceholder = p.highestNumberedPlaceholder
	child.analysisDepth = p.analysisDepth
	child.expressionDepth = p.expressionDepth
	child.maxParseDepth = p.maxParseDepth
	child.insertKeywords = p.insertKeywords
	child.aliasNamesSingleColumnFunctions = p.aliasNamesSingleColumnFunctions
	child.indexOptions = p.indexOptions
	child.computedColumnShorthand = p.computedColumnShorthand
	child.notVisibleColumns = p.notVisibleColumns
	child.asOfSystemTime = p.asOfSystemTime
	child.placeholderNumbers = p.placeholderNumbers
	child.scannedSubqueries = p.scannedSubqueries
	child.rowLocked = p.rowLocked
	return child
}

// isInsertKeyword reports whether the cursor is on a word that opens an INSERT: INSERT,
// or a word the flavour adds to it.
//
// Returns bool which is true for such a word.
func (p *parser) isInsertKeyword() bool {
	return p.IsKeyword("INSERT") || p.IsKeyword(p.insertKeywords...)
}

// requireEnd records and returns a syntax error when tokens remain after the statement
// the parser has analysed, so an unparsed tail fails the statement instead of being
// dropped.
//
// Returns error which is a dialectkit.ParseError naming the first unparsed token, or nil
// at the end of the tokens.
func (p *parser) requireEnd() error {
	return p.recordSyntaxError(p.RequireEnd())
}

// syntaxError records and returns a syntax error at the current token.
//
// Takes message (string) which says what was wrong.
//
// Returns error which is the recorded dialectkit.ParseError.
func (p *parser) syntaxError(message string) error {
	return p.recordSyntaxError(dialectkit.NewParseError(p.Current(), message))
}

// statementEnd returns the index one past the last token of the statement, excluding an
// end-of-input token.
//
// Returns int which is the index.
func (p *parser) statementEnd() int {
	end := len(p.Tokens)
	if end > 0 && p.Tokens[end-1].Kind == tokenEOF {
		end--
	}
	return end
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
	return dialectkit.SourceBetween(p.source, p.Tokens[:p.statementEnd()], startIndex, endIndex)
}

// parseIdentifierOrKeyword consumes and returns the current identifier or quoted string
// value.
//
// Returns string which is the identifier value.
// Returns error when the current token is neither an identifier nor a string.
func (p *parser) parseIdentifierOrKeyword() (string, error) {
	current := p.Current()
	if current.Kind == tokenIdentifier || current.Kind == tokenString {
		p.Position++
		return current.Value, nil
	}
	return "", dialectkit.NewParseError(current, "expected identifier")
}

// parseSchemaQualifiedName consumes a name of the form `database.schema.name`,
// `schema.name` or a bare `name` from the current position.
//
// A database qualifier, which the engine takes only when it names the database the
// statement runs in, is dropped, so the name reads as `schema.name`.
//
// Returns schema (string) which is the schema qualifier, or "" when absent.
// Returns name (string) which is the trailing identifier.
// Returns err (error) when an identifier cannot be parsed.
func (p *parser) parseSchemaQualifiedName() (schema string, name string, err error) {
	first, parseError := p.parseIdentifierOrKeyword()
	if parseError != nil {
		return "", "", parseError
	}
	if p.Current().Kind != tokenDot {
		return "", first, nil
	}
	p.Advance()
	second, secondError := p.parseIdentifierOrKeyword()
	if secondError != nil {
		return "", "", secondError
	}
	if p.Current().Kind != tokenDot || p.Peek().Kind == tokenStar {
		return first, second, nil
	}
	p.Advance()
	third, thirdError := p.parseIdentifierOrKeyword()
	if thirdError != nil {
		return "", "", thirdError
	}
	return second, third, nil
}

// skipParenthesised consumes a balanced parenthesised group starting at the current
// position.
//
// Returns error when the current token is not '(' or the group is unmatched.
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
		return p.syntaxError("unmatched parenthesis")
	}
	return nil
}

// collectParenthesised consumes a balanced parenthesised group and returns its inner
// tokens.
//
// The result is a capacity-capped view of the parser's own token slice rather than a
// copy, so collecting a deeply nested group costs no allocation per level; callers only
// read it, and an append would reallocate rather than overwrite the parent's tokens.
//
// Returns []token which is the slice of tokens between the outer parentheses, or nil for
// an empty group.
// Returns error when the current token is not '(' or the group is unmatched.
func (p *parser) collectParenthesised() ([]token, error) {
	if p.Current().Kind != tokenLeftParen {
		return nil, dialectkit.NewParseError(p.Current(), "expected '('")
	}
	p.Advance()
	start := p.Position
	depth := 1
	for !p.AtEnd() {
		depth = adjustParenthesisDepth(p.Current().Kind, depth)
		if depth == 0 {
			break
		}
		p.Advance()
	}
	if depth != 0 {
		return nil, p.syntaxError("unmatched parenthesis")
	}
	end := p.Position
	p.Advance()
	if end == start {
		return nil, nil
	}
	return p.Tokens[start:end:end], nil
}

// recordSyntaxError keeps err as the statement's syntax error when none was recorded
// before, so the first failure is the one reported.
//
// Takes err (error) which is the syntax error to record; nil is ignored.
//
// Returns error which is err unchanged, so callers can record and propagate in one step.
func (p *parser) recordSyntaxError(err error) error {
	if err != nil && p.syntax.err == nil {
		p.syntax.err = err
	}
	return err
}

// requireKeyword consumes one of keywords, recording a syntax error that fails the whole
// statement when the current token matches none of them.
//
// Takes keywords (...string) which is the list of acceptable keywords.
//
// Returns error when the current token is not one of the keywords.
func (p *parser) requireKeyword(keywords ...string) error {
	_, err := p.ExpectKeyword(keywords...)
	return p.recordSyntaxError(err)
}

// requireKeywordSequence consumes each keyword in order, recording a syntax error at the
// first that does not match.
//
// Takes keywords (...string) which are the keywords that must follow one another.
//
// Returns error when any keyword in the sequence is missing.
func (p *parser) requireKeywordSequence(keywords ...string) error {
	for _, keyword := range keywords {
		if err := p.requireKeyword(keyword); err != nil {
			return err
		}
	}
	return nil
}

// requireSkipParenthesised consumes a balanced parenthesised group, recording a syntax
// error when the group is unmatched.
//
// Returns error when the group cannot be skipped.
func (p *parser) requireSkipParenthesised() error {
	return p.recordSyntaxError(p.skipParenthesised())
}

// requireIdentifierOrKeyword consumes an identifier or string token, recording a syntax
// error when the current token is neither.
//
// Returns string which is the consumed identifier value.
// Returns error when the current token is not an identifier.
func (p *parser) requireIdentifierOrKeyword() (string, error) {
	name, err := p.parseIdentifierOrKeyword()
	return name, p.recordSyntaxError(err)
}

// requireSchemaQualifiedName consumes a schema-qualified name, recording a syntax error
// when it is malformed.
//
// Returns schema (string) which is the schema qualifier, or "" when absent.
// Returns name (string) which is the trailing identifier.
// Returns err (error) when the name cannot be parsed.
func (p *parser) requireSchemaQualifiedName() (schema string, name string, err error) {
	schema, name, err = p.parseSchemaQualifiedName()
	return schema, name, p.recordSyntaxError(err)
}

// registerParameterFromToken records a parameter reference for a dollar, named, or
// sequential parameter token.
//
// Takes parameterToken (token) which is the parameter token to record.
// Takes context (sqlmodel.ParameterContext) which classifies the parameter's syntactic
// role.
// Takes columnReference (*ast.ColumnReference) which is the associated column when known.
// Takes castType (*sqlmodel.SQLType) which is the explicit cast type when known.
//
// Returns int which is the parameter's positional number.
func (p *parser) registerParameterFromToken(
	parameterToken token,
	context sqlmodel.ParameterContext,
	columnReference *ast.ColumnReference,
	castType *sqlmodel.SQLType,
) int {
	var number int
	switch parameterToken.Kind {
	case tokenDollarParam:
		number = p.registerDollarParameter(parameterToken, context, columnReference, castType)
	case tokenNamedParam:
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

// registerSequentialParameter allocates the next positional number to a sequential
// placeholder and records its metadata.
//
// Takes context (sqlmodel.ParameterContext) which classifies the parameter's syntactic
// role.
// Takes columnReference (*ast.ColumnReference) which is the associated column when known.
// Takes castType (*sqlmodel.SQLType) which is the explicit cast type when known.
//
// Returns int which is the allocated parameter number.
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

// registerDollarParameter records a `$n` placeholder reference and updates the parameter
// count to track the highest seen number.
//
// Takes parameterToken (token) which is the `$n` token.
// Takes context (sqlmodel.ParameterContext) which classifies the parameter's syntactic
// role.
// Takes columnReference (*ast.ColumnReference) which is the associated column when known.
// Takes castType (*sqlmodel.SQLType) which is the explicit cast type when known.
//
// Returns int which is the parameter's positional number.
func (p *parser) registerDollarParameter(
	parameterToken token,
	context sqlmodel.ParameterContext,
	columnReference *ast.ColumnReference,
	castType *sqlmodel.SQLType,
) int {
	number, conversionError := strconv.Atoi(parameterToken.Value[1:])
	if conversionError != nil {
		number = 0
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

// registerNamedParameter records a `:name` placeholder reference, reusing an existing
// positional number when the name has been seen before. A new name takes the number after
// every number assigned so far and every number a numbered placeholder is written with in
// the statement.
//
// Takes parameterToken (token) which is the `:name` token.
// Takes context (sqlmodel.ParameterContext) which classifies the parameter's syntactic
// role.
// Takes columnReference (*ast.ColumnReference) which is the associated column when known.
// Takes castType (*sqlmodel.SQLType) which is the explicit cast type when known.
//
// Returns int which is the parameter's positional number.
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
	number := max(p.parameterCount, p.highestNumberedPlaceholder) + 1
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

// classifyStatement returns the kind of the statement represented by the supplied tokens.
//
// Takes tokens ([]token) which is the token stream of a single statement.
//
// Returns statementKind which is the resolved kind, or statementKindUnknown when the
// leading tokens are not recognised.
func classifyStatement(tokens []token) statementKind {
	if len(tokens) == 0 {
		return statementKindUnknown
	}

	if tokens[0].Kind == tokenLeftParen {
		return classifyParenthesisedQuery(tokens)
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

// opensInsert reports whether a statement opens with a word a flavour adds to INSERT,
// such as UPSERT, directly or after a WITH list.
//
// Takes tokens ([]token) which is the statement's token stream.
// Takes keywords ([]string) which are the upper-cased words.
//
// Returns bool which is true when the statement's first word outside the WITH list's
// parentheses is one of the words.
func opensInsert(tokens []token, keywords []string) bool {
	if len(keywords) == 0 || len(tokens) == 0 {
		return false
	}
	if !tokens[0].IsKeyword(tokenIdentifier, keywordWITH) {
		return slices.Contains(keywords, tokens[0].Keyword(tokenIdentifier))
	}
	depth := 0
	for _, candidate := range tokens[1:] {
		switch {
		case candidate.Kind == tokenLeftParen:
			depth++
		case candidate.Kind == tokenRightParen:
			depth--
		case depth != 0:
		case slices.Contains(keywords, candidate.Keyword(tokenIdentifier)):
			return true
		default:
			if _, isDML := dmlKeywords[candidate.Keyword(tokenIdentifier)]; isDML {
				return false
			}
		}
	}
	return false
}

// classifyParenthesisedQuery returns the kind of a statement opening with a parenthesis:
// a query, such as (SELECT ...) UNION (SELECT ...), when the first word after the opening
// parentheses starts one.
//
// Takes tokens ([]token) which is the statement's token stream, opening with '('.
//
// Returns statementKind which is statementKindSelect for a parenthesised query, or
// statementKindUnknown otherwise.
func classifyParenthesisedQuery(tokens []token) statementKind {
	for _, candidate := range tokens {
		if candidate.Kind == tokenLeftParen {
			continue
		}
		if candidate.IsKeyword(tokenIdentifier, keywordSELECT) || candidate.IsKeyword(tokenIdentifier, keywordWITH) ||
			candidate.IsKeyword(tokenIdentifier, keywordVALUES) || candidate.IsKeyword(tokenIdentifier, keywordTABLE) {
			return statementKindSelect
		}
		return statementKindUnknown
	}
	return statementKindUnknown
}

// classifyViaExtensionsIndexed consults each registered StatementExtensionPort in
// registration order for the first valid extension claim.
//
// It returns the first non-zero statementKind that sits inside the extension reservation
// range from pgext.StatementKindExtensionBase, along with the registry index of the
// extension that claimed the statement. The cached index is stored on parsedStatement so
// dispatchDDL can skip the re-classification scan when the extension's Parse method runs.
//
// Extensions are expected to return either 0 (decline) or a kind in their reserved range;
// any kind below pgext.StatementKindExtensionBase is treated as a misuse and rejected, so
// an extension cannot hijack a built-in handler by returning a raw integer like 1 that
// happens to alias statementKindCreateTable.
//
// Takes tokens ([]token) which is the candidate statement's token slice; each extension
// is handed its own copy, so none can change what the parser or another extension reads.
// Takes extensions ([]pgext.StatementExtensionPort) which is the registry walked in
// registration order.
//
// Returns statementKind which is the first valid extension claim, or statementKindUnknown
// when every extension declines (or returns a kind outside the reservation range).
// Returns int which is the index of the claiming extension, or -1 when no extension
// claimed the statement.
func classifyViaExtensionsIndexed(tokens []token, extensions []pgext.StatementExtensionPort) (statementKind, int) {
	for index, extension := range extensions {
		kind := extension.Classify(slices.Clone(tokens))
		if kind == 0 {
			continue
		}
		if kind < pgext.StatementKindExtensionBase {
			continue
		}
		return kind, index
	}
	return statementKindUnknown, -1
}

// classifyCreateStatement returns the kind of a CREATE statement.
//
// Takes tokens ([]token) which is the statement's token stream.
//
// Returns statementKind which is the specific CREATE kind, or statementKindUnknown when
// the object keyword is unrecognised.
func classifyCreateStatement(tokens []token) statementKind {
	if len(tokens) < 2 {
		return statementKindUnknown
	}

	index := skipCreatePrefixes(tokens)
	if index >= len(tokens) {
		return statementKindUnknown
	}

	upper := tokens[index].Keyword(tokenIdentifier)
	if upper == keywordFOREIGN {
		if index+1 < len(tokens) && tokens[index+1].IsKeyword(tokenIdentifier, keywordTABLE) {
			return statementKindCreateTable
		}
		return statementKindUnknown
	}
	if kind, found := createObjectKinds[upper]; found {
		return kind
	}

	return statementKindUnknown
}

// skipCreatePrefixes returns the index of the first token after any OR REPLACE and the
// GLOBAL / LOCAL, TEMP / TEMPORARY and UNLOGGED qualifiers following CREATE.
//
// Takes tokens ([]token) which is the statement's token stream.
//
// Returns int which is the index of the next object keyword, or len(tokens) when the
// prefixes run past the end of the stream.
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
		case "GLOBAL", "LOCAL", "TEMP", "TEMPORARY", "UNLOGGED":
			index++
		default:
			return index
		}
	}
	return index
}

// classifyDropStatement returns the kind of a DROP statement.
//
// Takes tokens ([]token) which is the statement's token stream.
//
// Returns statementKind which is the specific DROP kind, or statementKindUnknown when the
// object keyword is unrecognised.
func classifyDropStatement(tokens []token) statementKind {
	if len(tokens) < 2 {
		return statementKindUnknown
	}

	switch tokens[1].Keyword(tokenIdentifier) {
	case keywordTABLE:
		return statementKindDropTable
	case "VIEW", "MATERIALIZED":
		return statementKindDropView
	case "INDEX":
		return statementKindDropIndex
	case keywordTYPE, "DOMAIN":
		return statementKindDropType
	case "FUNCTION", "PROCEDURE":
		return statementKindDropFunction
	case keywordSCHEMA:
		return statementKindDropSchema
	case "EXTENSION":
		return statementKindDropExtension
	case "TRIGGER":
		return statementKindDropTrigger
	case "SEQUENCE":
		return statementKindDropSequence
	}

	return statementKindUnknown
}

// classifyAlterStatement returns the kind of an ALTER statement.
//
// Takes tokens ([]token) which is the statement's token stream.
//
// Returns statementKind which is the specific ALTER kind, or statementKindUnknown when
// the object keyword is unrecognised.
func classifyAlterStatement(tokens []token) statementKind {
	if len(tokens) < 2 {
		return statementKindUnknown
	}

	switch tokens[1].Keyword(tokenIdentifier) {
	case keywordTABLE:
		return statementKindAlterTable
	case keywordTYPE, "DOMAIN":
		return statementKindAlterType
	case "VIEW", "MATERIALIZED":
		return statementKindAlterView
	case "SEQUENCE":
		return statementKindAlterSequence
	}

	return statementKindUnknown
}

// classifyWithStatement returns the kind of a WITH (CTE) statement by scanning for the
// first DML keyword at top level.
//
// Takes tokens ([]token) which is the statement's token stream.
//
// Returns statementKind which is the DML kind embedded in the WITH, or
// statementKindSelect when no other DML keyword is found.
func classifyWithStatement(tokens []token) statementKind {
	depth := 0
	for _, candidate := range tokens {
		if candidate.Kind == tokenLeftParen {
			depth++
			continue
		}
		if candidate.Kind == tokenRightParen {
			depth--
			continue
		}
		if depth != 0 {
			continue
		}
		if kind, matched := dmlKeywords[candidate.Keyword(tokenIdentifier)]; matched {
			return kind
		}
	}
	return statementKindSelect
}

// highestPlaceholderNumber returns the highest number a numbered placeholder is written
// with among tokens.
//
// Takes tokens ([]token) which are the statement's tokens.
//
// Returns int which is the highest number, or zero when no numbered placeholder is
// written.
func highestPlaceholderNumber(tokens []token) int {
	highest := 0
	for index := range tokens {
		if tokens[index].Kind != tokenDollarParam {
			continue
		}
		if number, err := strconv.Atoi(tokens[index].Value[1:]); err == nil {
			highest = max(highest, number)
		}
	}
	return highest
}

// isParameterToken reports whether a token kind represents a parameter placeholder.
//
// Takes kind (tokenKind) which is the token kind to test.
//
// Returns bool which is true for dollar or named parameter tokens.
func isParameterToken(kind tokenKind) bool {
	return kind == tokenDollarParam || kind == tokenNamedParam
}

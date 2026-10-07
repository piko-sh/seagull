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
	"errors"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
)

const (
	// ifNotExistsTokenWidth is the number of tokens consumed by the `IF NOT EXISTS` clause
	// when skipping it as a CREATE prefix.
	ifNotExistsTokenWidth = 3

	// multiWordObjectKeywordTokenCount is the minimum token count required to recognise a
	// two-word object kind ("ROW POLICY", "SETTINGS PROFILE") on a CREATE / ALTER / DROP
	// prefix.
	multiWordObjectKeywordTokenCount = 3

	// classifyPolicyKeyword is the POLICY keyword string the multi-word object classifiers
	// match on.
	classifyPolicyKeyword = "POLICY"

	// classifyProfileKeyword is the PROFILE keyword string the multi-word object classifiers
	// match on.
	classifyProfileKeyword = "PROFILE"

	// objectKindTable is the TABLE object keyword that may follow ATTACH / DETACH at the top
	// level, shared by the classifier and the parser.
	objectKindTable = "TABLE"

	// objectKindView is the VIEW object keyword that may follow ATTACH / DETACH at the top
	// level, shared by the classifier and the parser.
	objectKindView = "VIEW"

	// objectKindDictionary is the DICTIONARY object keyword that may follow ATTACH / DETACH
	// at the top level, shared by the classifier and the parser.
	objectKindDictionary = "DICTIONARY"

	// objectKindDatabase is the DATABASE object keyword that may follow ATTACH / DETACH at
	// the top level, shared by the classifier and the parser.
	objectKindDatabase = "DATABASE"

	// messageUnmatchedParenthesis is the syntax error reported at a "(" that is never
	// closed.
	messageUnmatchedParenthesis = "unmatched parenthesis"

	// messageUnmatchedBracket is the syntax error reported at a "[" that is never closed.
	messageUnmatchedBracket = "unmatched bracket"

	// messageExpectedExpression is the syntax error at a select item with nothing in it.
	messageExpectedExpression = "expected an expression"

	// messageExpectedAlias is the syntax error at an AS with no alias after it.
	messageExpectedAlias = "expected an alias after AS"
)

// statementKind enumerates every top-level SQL statement the ClickHouse dialect
// recognises. Each kind routes to its own parsing branch in ApplyDDL / AnalyseQuery so
// additions stay localised.
type statementKind uint8

const (
	// statementKindCreateTable marks `CREATE TABLE ...`.
	statementKindCreateTable statementKind = iota

	// statementKindCreateView marks `CREATE VIEW ...`.
	statementKindCreateView

	// statementKindCreateMaterializedView marks `CREATE MATERIALIZED VIEW ...`.
	statementKindCreateMaterializedView

	// statementKindCreateDictionary marks `CREATE DICTIONARY ...`.
	statementKindCreateDictionary

	// statementKindCreateFunction marks `CREATE FUNCTION ...`.
	statementKindCreateFunction

	// statementKindCreateDatabase marks `CREATE DATABASE ...`.
	statementKindCreateDatabase

	// statementKindDropTable marks `DROP TABLE ...`.
	statementKindDropTable

	// statementKindDropView marks `DROP VIEW ...`.
	statementKindDropView

	// statementKindDropDictionary marks `DROP DICTIONARY ...`.
	statementKindDropDictionary

	// statementKindDropFunction marks `DROP FUNCTION ...`.
	statementKindDropFunction

	// statementKindDropDatabase marks `DROP DATABASE ...`.
	statementKindDropDatabase

	// statementKindAlterTable marks `ALTER TABLE ...`.
	statementKindAlterTable

	// statementKindRenameTable marks `RENAME TABLE ...`.
	statementKindRenameTable

	// statementKindExchangeTables marks `EXCHANGE TABLES ...`.
	statementKindExchangeTables

	// statementKindTruncate marks `TRUNCATE ...`.
	statementKindTruncate

	// statementKindOptimize marks a table-optimisation statement.
	statementKindOptimize

	// statementKindSystem marks `SYSTEM ...`.
	statementKindSystem

	// statementKindUse marks `USE ...`.
	statementKindUse

	// statementKindShow marks `SHOW ...` (read-only).
	statementKindShow

	// statementKindSet marks `SET ...`.
	statementKindSet

	// statementKindSelect marks `SELECT ...` (read-only).
	statementKindSelect

	// statementKindInsert marks `INSERT ...`.
	statementKindInsert

	// statementKindDelete marks a lightweight `DELETE FROM ... [WHERE ...]`.
	//
	// The ALTER TABLE ... DELETE mutation form is classified as statementKindAlterTable
	// instead.
	statementKindDelete

	// statementKindCreateUser marks `CREATE USER ...`.
	statementKindCreateUser

	// statementKindAlterUser marks `ALTER USER ...`.
	statementKindAlterUser

	// statementKindDropUser marks `DROP USER ...`.
	statementKindDropUser

	// statementKindCreateRole marks `CREATE ROLE ...`.
	statementKindCreateRole

	// statementKindAlterRole marks `ALTER ROLE ...`.
	statementKindAlterRole

	// statementKindDropRole marks `DROP ROLE ...`.
	statementKindDropRole

	// statementKindCreatePolicy marks `CREATE [ROW] POLICY ...`.
	statementKindCreatePolicy

	// statementKindAlterPolicy marks `ALTER [ROW] POLICY ...`.
	statementKindAlterPolicy

	// statementKindDropPolicy marks `DROP [ROW] POLICY ...`.
	statementKindDropPolicy

	// statementKindCreateQuota marks `CREATE QUOTA ...`.
	statementKindCreateQuota

	// statementKindAlterQuota marks `ALTER QUOTA ...`.
	statementKindAlterQuota

	// statementKindDropQuota marks `DROP QUOTA ...`.
	statementKindDropQuota

	// statementKindCreateSettingsProfile marks `CREATE SETTINGS PROFILE ...`.
	statementKindCreateSettingsProfile

	// statementKindAlterSettingsProfile marks `ALTER SETTINGS PROFILE ...`.
	statementKindAlterSettingsProfile

	// statementKindDropSettingsProfile marks `DROP SETTINGS PROFILE ...`.
	statementKindDropSettingsProfile

	// statementKindGrant marks `GRANT ...`.
	statementKindGrant

	// statementKindRevoke marks `REVOKE ...`.
	statementKindRevoke

	// statementKindExplain marks `EXPLAIN ...` (read-only).
	statementKindExplain

	// statementKindDescribeTable marks `DESCRIBE [TABLE] ...` / `DESC ...` (read-only).
	statementKindDescribeTable

	// statementKindCheckTable marks `CHECK TABLE ...` (read-only).
	statementKindCheckTable

	// statementKindBackup marks `BACKUP ...`.
	statementKindBackup

	// statementKindRestore marks `RESTORE ...`.
	statementKindRestore

	// statementKindKillQuery marks `KILL QUERY ...`.
	statementKindKillQuery

	// statementKindKillMutation marks `KILL MUTATION ...`.
	statementKindKillMutation

	// statementKindAttachTable marks top-level `ATTACH TABLE ...`.
	statementKindAttachTable

	// statementKindDetachTable marks top-level `DETACH TABLE ...`.
	statementKindDetachTable

	// statementKindUpdate marks a lightweight `UPDATE ...
	//
	// SET ... WHERE ...`.
	statementKindUpdate

	// statementKindUnknown marks a statement that matched no recognised shape.
	statementKindUnknown
)

var (
	// firstWordClassifiers dispatches on the first keyword to the appropriate sub-classifier
	// when the second token is also needed for disambiguation (e.g. CREATE TABLE vs CREATE
	// MATERIALIZED VIEW).
	firstWordClassifiers = map[string]func([]token) statementKind{
		"CREATE":   classifyCreateStatement,
		"DROP":     classifyDropStatement,
		"ALTER":    classifyAlterStatement,
		"RENAME":   classifyRenameStatement,
		"EXCHANGE": classifyExchangeStatement,
		"WITH":     classifyWithStatement,
		"DESCRIBE": classifyDescribeStatement,
		"DESC":     classifyDescribeStatement,
		"CHECK":    classifyCheckStatement,
		"KILL":     classifyKillStatement,
		"ATTACH":   classifyAttachStatement,
		"DETACH":   classifyDetachStatement,
	}

	// firstWordStaticKinds is the fast path for statements whose first keyword unambiguously
	// identifies the kind.
	firstWordStaticKinds = map[string]statementKind{
		"SELECT":   statementKindSelect,
		"INSERT":   statementKindInsert,
		"DELETE":   statementKindDelete,
		"UPDATE":   statementKindUpdate,
		"TRUNCATE": statementKindTruncate,
		"OPTIMIZE": statementKindOptimize,
		"SYSTEM":   statementKindSystem,
		"USE":      statementKindUse,
		"SHOW":     statementKindShow,
		"SET":      statementKindSet,
		"EXPLAIN":  statementKindExplain,
		"BACKUP":   statementKindBackup,
		"RESTORE":  statementKindRestore,
		"GRANT":    statementKindGrant,
		"REVOKE":   statementKindRevoke,
	}

	// createObjectKinds dispatches CREATE-prefixed statements by the object keyword.
	// MATERIALIZED VIEW is handled specially because it is two words.
	createObjectKinds = map[string]statementKind{
		"TABLE":      statementKindCreateTable,
		"VIEW":       statementKindCreateView,
		"DICTIONARY": statementKindCreateDictionary,
		"FUNCTION":   statementKindCreateFunction,
		"DATABASE":   statementKindCreateDatabase,
		"USER":       statementKindCreateUser,
		"ROLE":       statementKindCreateRole,
		"QUOTA":      statementKindCreateQuota,
	}

	// dropObjectKinds dispatches DROP-prefixed statements by the object keyword.
	dropObjectKinds = map[string]statementKind{
		"TABLE":      statementKindDropTable,
		"VIEW":       statementKindDropView,
		"DICTIONARY": statementKindDropDictionary,
		"FUNCTION":   statementKindDropFunction,
		"DATABASE":   statementKindDropDatabase,
		"USER":       statementKindDropUser,
		"ROLE":       statementKindDropRole,
		"QUOTA":      statementKindDropQuota,
	}

	// statementWords lists the words a ClickHouse statement opens with, so a statement the
	// dialect classifies as none of the kinds it analyses still runs, with nothing to
	// analyse in it, when it opens with one of them, and is rejected when it does not.
	statementWords = map[string]bool{
		"ALTER": true, "ATTACH": true, "BACKUP": true, "BEGIN": true, "CHECK": true, "COMMIT": true,
		"CREATE": true, "DELETE": true, "DESC": true, "DESCRIBE": true, "DETACH": true, "DROP": true,
		"EXCHANGE": true, "EXISTS": true, "EXPLAIN": true, "GRANT": true, "INSERT": true, "KILL": true,
		"MOVE": true, "OPTIMIZE": true, "RENAME": true, "RESTORE": true, "REVOKE": true, "ROLLBACK": true,
		"SELECT": true, "SET": true, "SHOW": true, "SYSTEM": true, "TRUNCATE": true, "UNDROP": true,
		"UPDATE": true, "USE": true, "WATCH": true, kwWith: true,
	}

	// alterObjectKinds dispatches ALTER-prefixed statements by the object keyword (USER /
	// ROLE / QUOTA). TABLE has its own action grammar and POLICY / PROFILE forms are handled
	// inline by classifyAlterStatement.
	alterObjectKinds = map[string]statementKind{
		"USER":  statementKindAlterUser,
		"ROLE":  statementKindAlterRole,
		"QUOTA": statementKindAlterQuota,
	}
)

// parsedStatement is the engine-private payload attached to each ast.ParsedStatement. It
// carries the lexed tokens for this statement plus the classification result so
// downstream handlers do not re-scan.
type parsedStatement struct {
	// source is the script the statement was read from; token offsets index into it.
	source string

	// tokens holds the lexed tokens for this statement: for a statement over the token
	// budget, only those read before the budget was reached.
	tokens []token

	// start is the byte offset in source of the statement's first token.
	start int

	// end is the byte offset in source just past the statement's last token, so a check can
	// read every token of a statement over the token budget again.
	end int

	// kind holds the classification result for this statement.
	kind statementKind

	// overBudget reports a statement holding more tokens than the budget it was tokenised
	// under, of which tokens holds only the first.
	overBudget bool
}

// IsParsedStatement marks parsedStatement as implementing the marker interface required
// by ast.ParsedStatement.
func (*parsedStatement) IsParsedStatement() {}

// checkTokenBudget refuses a statement holding more tokens than a dialect's budget, or
// than the budget it was tokenised under.
//
// Takes limits (dialectkit.Limits) which hold the budget of the dialect walking it.
//
// Returns error which wraps dialectkit.ErrTokenBudget, or nil when the statement fits.
func (parsed *parsedStatement) checkTokenBudget(limits dialectkit.Limits) error {
	if parsed.overBudget {
		return dialectkit.TokenBudgetExceeded(len(parsed.tokens))
	}
	return limits.CheckTokens(len(parsed.tokens))
}

// parser is the per-statement parsing state for the ClickHouse engine. It maintains a
// forward-only cursor through the token stream plus the accumulator state used during DML
// analysis.
type parser struct {
	// namedParameterMap deduplicates `{name:Type}` placeholders by name so the generated Go
	// method exposes one parameter per distinct name.
	namedParameterMap map[string]int

	// placedParameters names the parameter each ParameterExpression the statement's parsers
	// built stands for. Child parsers share it, so once the whole statement is read the
	// statement's parser numbers every one of them as the statement numbers its parameters,
	// whichever parser built it.
	placedParameters map[*ast.ParameterExpression]string

	// assignedPlaceholders maps the offset of each `{name:Type}` placeholder that is the
	// whole value an INSERT ... VALUES row or an UPDATE assignment gives a column to that
	// column, which registerClickHouseParameter records the parameter as assigned to.
	assignedPlaceholders map[int]ast.ColumnReference

	// firstParameterTypeError captures the first `{name:Type}` placeholder whose type tag
	// failed to parse.
	//
	// Surfacing the cause lets analyseSelect / analyseInsert wrap it into a
	// diagnostic-friendly error instead of silently degrading the parameter to an untyped
	// binding.
	firstParameterTypeError error

	// syntaxError captures the first syntax error found by a parsing routine that has no
	// error result of its own (the expression chain). ApplyDDL and AnalyseQuery return it in
	// preference to any partial result.
	syntaxError error

	// types resolves the type names read while walking the statement, through the dialect's
	// hooks and extra types. newParser seeds it with the built-ins alone; the engine
	// overrides it from the dialect and child parsers inherit it.
	types *typeResolver

	// spans records the spans of the top-level statement; it is nil in every other parser,
	// so a nested query's clauses are never recorded as the statement's.
	spans *dialectkit.SpanRecorder

	// Cursor walks the statement's tokens; its keyword matching never treats a delimited
	// identifier as a keyword.
	dialectkit.Cursor[tokenKind]

	// tail holds the clauses that close the SELECT arm parsed last, which are the
	// statement's own clauses once the whole statement is read.
	tail armTail

	// parameterCount tracks the number of positional parameters seen so far.
	parameterCount int

	// analysisDepth bounds recursion through analyseSelect across compound branches, CTE
	// bodies, derived tables, scalar subqueries, and view bodies. Mirrors the duckdb /
	// postgres convention.
	analysisDepth int

	// expressionDepth bounds recursion through the parseExpression chain (lambda then or
	// then and through primary to paren).
	//
	// Without the bound, deeply nested expressions like `((((... ((1)) ...))))` can drive
	// the parser past the Go stack limit. It uses the same threshold as analysisDepth.
	expressionDepth int

	// maxParseDepth is the effective cap for analysisDepth and expressionDepth. newParser
	// seeds it with defaultMaxParseDepth; the engine overrides it from the dialect and child
	// parsers inherit it so the global cap holds across instances.
	maxParseDepth int

	// aliasStart is the index of the token the alias parsed last starts at, its AS keyword
	// included; -1 before an alias is found.
	aliasStart int

	// insertData is the index of the first token of an INSERT's data, its VALUES rows or the
	// rows after FORMAT, where ClickHouse reads a map literal such as {'a': 1}; -1 when the
	// statement has none.
	insertData int

	// joinUseNulls is the join_use_nulls setting the statement runs with: the dialect's
	// assumed server setting unless the statement sets its own. It decides whether an outer
	// join's unmatched side is nullable.
	joinUseNulls bool

	// groupByUseNulls is the group_by_use_nulls setting the statement runs with: the
	// dialect's assumed server setting unless the statement sets its own. It decides whether
	// a key a subtotal row does not group by is NULL or its type's default value.
	groupByUseNulls bool
}

// newParser builds a parser positioned at the start of the supplied token stream with the
// default parse-depth cap, dialectkit.DefaultMaxParseDepth, and an empty named-parameter
// map.
//
// Takes tokens ([]token) which is the lexed statement to parse.
//
// Returns *parser which is the ready-to-use parser state.
func newParser(tokens []token) *parser {
	return &parser{
		Cursor:                  dialectkit.NewCursor(tokens, tokenIdentifier, tokenEOF),
		maxParseDepth:           dialectkit.DefaultMaxParseDepth,
		types:                   newTypeResolver(nil, nil, defaultMaxTypeParseDepth),
		namedParameterMap:       make(map[string]int),
		placedParameters:        make(map[*ast.ParameterExpression]string),
		firstParameterTypeError: nil,
		syntaxError:             nil,
		spans:                   nil,
		tail:                    armTail{orderBy: offsetRange{}, pagination: offsetRange{}, trailing: offsetRange{}, rowShaping: nil, orderByPlace: 0},
		parameterCount:          0,
		analysisDepth:           0,
		expressionDepth:         0,
		aliasStart:              -1,
		insertData:              -1,
		assignedPlaceholders:    make(map[int]ast.ColumnReference),
		joinUseNulls:            false,
		groupByUseNulls:         false,
	}
}

// parseIdentifierOrKeyword reads the next token as an identifier.
//
// It accepts both bare identifiers and quoted forms (any token kind flagged
// tokenIdentifier by the tokeniser).
//
// Returns string which is the identifier text.
// Returns error when the next token is not an identifier.
func (p *parser) parseIdentifierOrKeyword() (string, error) {
	current := p.Current()
	if current.Kind == tokenIdentifier {
		p.Position++
		return current.Value, nil
	}
	return "", dialectkit.NewParseError(current, "expected an identifier")
}

// parseDatabaseQualifiedName reads either `name` or `database.name`.
//
// ClickHouse uses `database.table` qualification (postgres-style `schema.table`); the
// catalogue treats the database as a schema for uniform resolution.
//
// Returns database (string) which is the qualifying database, empty when absent.
// Returns name (string) which is the object name.
// Returns err (error) when an identifier cannot be read.
func (p *parser) parseDatabaseQualifiedName() (database string, name string, err error) {
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

// skipParenthesised consumes a balanced parenthesised block starting at the current
// cursor position.
//
// It is used to skip past column-definition lists and parameter lists when the caller
// does not care about the contents.
//
// Returns error when the cursor is not on `(` or the parens are unbalanced.
func (p *parser) skipParenthesised() error {
	if p.Current().Kind != tokenLeftParen {
		return dialectkit.NewParseError(p.Current(), "expected '('")
	}
	opening := p.Advance()
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

// collectParenthesised consumes a balanced parenthesised block and returns the inner
// tokens, excluding the outer parens.
//
// It is used when the caller needs to re-parse the inner content (for example column
// definitions or function bodies). The result is a sub-slice of the parser's own token
// stream rather than a copy, so nested groups re-parsed level by level cost no extra
// memory; its capacity is clipped to its length so an append by a caller can never
// overwrite the tokens that follow the group.
//
// Returns []token which is the inner token slice, sharing storage with p.tokens.
// Returns error when the cursor is not on `(` or the parens are unbalanced.
func (p *parser) collectParenthesised() ([]token, error) {
	if p.Current().Kind != tokenLeftParen {
		return nil, dialectkit.NewParseError(p.Current(), "expected '('")
	}
	opening := p.Advance()
	start := p.Position
	depth := 1
	for !p.AtEnd() {
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

// expectKeywordSequence consumes each of keywords in order, each one required.
//
// Takes keywords (...string) which are the keywords that must appear next, in order.
//
// Returns error when any keyword in the sequence does not match the token at the cursor.
func (p *parser) expectKeywordSequence(keywords ...string) error {
	for _, keyword := range keywords {
		if _, err := p.ExpectKeyword(keyword); err != nil {
			return err
		}
	}
	return nil
}

// newChildParser creates a parser over a token sub-sequence, inheriting the parent's
// analysis and expression depths and every configured limit.
//
// Nested expressions and subqueries share the same depth limits. Creating a child parser
// does not reset the nesting depth.
//
// Takes tokens ([]token) which is the token sub-sequence the child walks.
//
// Returns *parser which is the child parser positioned at the start of tokens.
func (p *parser) newChildParser(tokens []token) *parser {
	child := newParser(tokens)
	child.Interrupt = p.Interrupt
	child.analysisDepth = p.analysisDepth
	child.expressionDepth = p.expressionDepth
	child.maxParseDepth = p.maxParseDepth
	child.types = p.types
	child.joinUseNulls = p.joinUseNulls
	child.groupByUseNulls = p.groupByUseNulls
	child.placedParameters = p.placedParameters
	return child
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

// depthError reports nesting beyond the parser's depth limit, wrapping
// dialectkit.ErrParseDepth.
//
// Returns error which names the limit.
func (p *parser) depthError() error {
	return dialectkit.DepthError(p.maxParseDepth)
}

// recordSyntaxError keeps the first syntax error reported by a parsing routine that has
// no error result of its own; later errors are dropped because the first one is the root
// cause.
//
// Takes err (error) which is the syntax error to record.
func (p *parser) recordSyntaxError(err error) {
	if p.syntaxError == nil {
		p.syntaxError = err
	}
}

// absorbChildFailure records a nested parser's depth-limit failure on this parser.
//
// Nested subquery bodies that fail to parse fall back to an untyped expression by design,
// but a failure caused by a nesting limit must still surface so an over-deep statement is
// reported rather than silently accepted.
//
// Takes err (error) which is the nested parser's failure, or nil.
func (p *parser) absorbChildFailure(err error) {
	if errors.Is(err, dialectkit.ErrParseDepth) {
		p.recordSyntaxError(err)
	}
}

// classifyStatement identifies the kind of the statement represented by the token slice.
//
// The caller (ApplyDDL or AnalyseQuery) decides how to surface an unrecognised statement.
//
// Takes tokens ([]token) which is the statement to classify.
//
// Returns statementKind which is the recognised kind, or statementKindUnknown when the
// leading token matches no recognised shape.
func classifyStatement(tokens []token) statementKind {
	if len(tokens) == 0 {
		return statementKindUnknown
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

// unanalysedStatementError reports unsupported or unrecognised ClickHouse statements.
//
// For supported alternate syntax, it suggests a form the analyser can read.
//
// Takes tokens ([]token) which is the statement.
//
// Returns error which is a dialectkit.ParseError at the statement's first token, or nil
// for a statement ClickHouse runs.
func unanalysedStatementError(tokens []token) error {
	if len(tokens) == 0 {
		return nil
	}
	first := tokens[0]
	switch keyword := first.Keyword(tokenIdentifier); {
	case first.Kind == tokenLeftParen:
		return dialectkit.NewParseError(first, "a query that opens with a parenthesis is not supported; "+
			"remove the parentheses around its first SELECT")
	case keyword == kwFrom:
		return dialectkit.NewParseError(first, "a query that opens with FROM is not supported; start it with SELECT")
	case statementWords[keyword]:
		return nil
	default:
		return dialectkit.NewParseError(first, "unrecognised statement: expected a statement such as SELECT, INSERT or ALTER")
	}
}

// unanalysedStatement answers a statement no analyser takes: an empty analysis for a
// statement the engine runs with nothing to analyse, and an error for one that opens with
// no statement word or holds a parameter the generated code could not bind.
//
// Statements over the token budget are refused because unread tokens could contain
// parameters that generated code cannot bind.
//
// Takes parsed (*parsedStatement) which is the statement.
//
// Returns *ast.RawQueryAnalysis which is empty for a statement with nothing to analyse.
// Returns error which is the dialectkit.ParseError, or the token budget error, otherwise.
func unanalysedStatement(parsed *parsedStatement) (*ast.RawQueryAnalysis, error) {
	if unrecognised := unanalysedStatementError(parsed.tokens); unrecognised != nil {
		return nil, unrecognised
	}
	if parsed.overBudget {
		return nil, dialectkit.TokenBudgetExceeded(len(parsed.tokens))
	}
	if unbound := unboundParameter(parsed.tokens); unbound != nil {
		return nil, unbound
	}
	return &ast.RawQueryAnalysis{}, nil
}

// unboundParameter refuses a {name:Type} parameter in a statement Seagull runs without
// analysing, such as SET or EXPLAIN, which the generated code could not bind.
//
// Takes tokens ([]token) which are the statement's tokens.
//
// Returns error which is a dialectkit.ParseError at the first parameter, or nil when the
// statement holds none.
func unboundParameter(tokens []token) error {
	for _, candidate := range tokens {
		if candidate.Kind == tokenClickHouseParam {
			return dialectkit.UnboundParameterError(candidate, tokens[0].Keyword(tokenIdentifier), "")
		}
	}
	return nil
}

// classifyCreateStatement recognises the CREATE family, including the two-word
// MATERIALIZED VIEW, ROW POLICY, and SETTINGS PROFILE object shapes.
//
// Takes tokens ([]token) which is the CREATE-prefixed statement.
//
// Returns statementKind which is the recognised CREATE kind, or statementKindUnknown when
// the object keyword is missing or unknown.
func classifyCreateStatement(tokens []token) statementKind {
	if len(tokens) < 2 {
		return statementKindUnknown
	}

	index := skipCreatePrefixes(tokens)
	if index >= len(tokens) {
		return statementKindUnknown
	}

	upper := tokens[index].Keyword(tokenIdentifier)

	if upper == "MATERIALIZED" && index+1 < len(tokens) && tokens[index+1].IsKeyword(tokenIdentifier, "VIEW") {
		return statementKindCreateMaterializedView
	}

	if upper == "ROW" && index+1 < len(tokens) && tokens[index+1].IsKeyword(tokenIdentifier, classifyPolicyKeyword) {
		return statementKindCreatePolicy
	}
	if upper == classifyPolicyKeyword {
		return statementKindCreatePolicy
	}
	if upper == "SETTINGS" && index+1 < len(tokens) && tokens[index+1].IsKeyword(tokenIdentifier, classifyProfileKeyword) {
		return statementKindCreateSettingsProfile
	}
	if upper == classifyProfileKeyword {
		return statementKindCreateSettingsProfile
	}

	if kind, found := createObjectKinds[upper]; found {
		return kind
	}

	return statementKindUnknown
}

// skipCreatePrefixes advances past optional `OR REPLACE` / `TEMPORARY` / `IF NOT EXISTS`
// modifiers to land on the object-kind keyword.
//
// Takes tokens ([]token) which is the CREATE-prefixed statement.
//
// Returns int which is the index of the next interesting token, or len(tokens) when the
// input is malformed.
func skipCreatePrefixes(tokens []token) int {
	index := 1
	for index < len(tokens) {
		upper := tokens[index].Keyword(tokenIdentifier)
		switch upper {
		case "OR":
			if index+1 < len(tokens) && tokens[index+1].IsKeyword(tokenIdentifier, "REPLACE") {
				index += 2
				continue
			}
			return index
		case "TEMPORARY", "TEMP":
			index++
			continue
		case "IF":
			if index+2 < len(tokens) &&
				tokens[index+1].IsKeyword(tokenIdentifier, "NOT") &&
				tokens[index+2].IsKeyword(tokenIdentifier, "EXISTS") {
				index += ifNotExistsTokenWidth
				continue
			}
			return index
		default:
			return index
		}
	}
	return index
}

// classifyDropStatement recognises the DROP family, including the two-word ROW POLICY and
// SETTINGS PROFILE object shapes.
//
// Takes tokens ([]token) which is the DROP-prefixed statement.
//
// Returns statementKind which is the recognised DROP kind, or statementKindUnknown when
// the object keyword is missing or unknown.
func classifyDropStatement(tokens []token) statementKind {
	if len(tokens) < 2 {
		return statementKindUnknown
	}
	second := tokens[1].Keyword(tokenIdentifier)
	if second == "ROW" && len(tokens) >= multiWordObjectKeywordTokenCount && tokens[2].IsKeyword(tokenIdentifier, classifyPolicyKeyword) {
		return statementKindDropPolicy
	}
	if second == classifyPolicyKeyword {
		return statementKindDropPolicy
	}
	if second == "SETTINGS" && len(tokens) >= multiWordObjectKeywordTokenCount && tokens[2].IsKeyword(tokenIdentifier, classifyProfileKeyword) {
		return statementKindDropSettingsProfile
	}
	if second == classifyProfileKeyword {
		return statementKindDropSettingsProfile
	}
	if kind, found := dropObjectKinds[second]; found {
		return kind
	}
	return statementKindUnknown
}

// classifyAlterStatement recognises ALTER TABLE (which covers ClickHouse's ADD COLUMN /
// DROP COLUMN / MODIFY COLUMN / UPDATE / DELETE / RENAME COLUMN / MATERIALIZE COLUMN /
// MODIFY TTL / MODIFY ORDER BY sub-forms) plus the RBAC ALTER family (USER / ROLE / QUOTA
// / ROW POLICY / SETTINGS PROFILE).
//
// Takes tokens ([]token) which is the ALTER-prefixed statement.
//
// Returns statementKind which is the recognised ALTER kind, or statementKindUnknown when
// the object keyword is missing or unknown.
func classifyAlterStatement(tokens []token) statementKind {
	if len(tokens) < 2 {
		return statementKindUnknown
	}
	second := tokens[1].Keyword(tokenIdentifier)
	if second == "TABLE" {
		return statementKindAlterTable
	}
	if second == "ROW" && len(tokens) >= multiWordObjectKeywordTokenCount && tokens[2].IsKeyword(tokenIdentifier, classifyPolicyKeyword) {
		return statementKindAlterPolicy
	}
	if second == classifyPolicyKeyword {
		return statementKindAlterPolicy
	}
	if second == "SETTINGS" && len(tokens) >= multiWordObjectKeywordTokenCount && tokens[2].IsKeyword(tokenIdentifier, classifyProfileKeyword) {
		return statementKindAlterSettingsProfile
	}
	if second == classifyProfileKeyword {
		return statementKindAlterSettingsProfile
	}
	if kind, found := alterObjectKinds[second]; found {
		return kind
	}
	return statementKindUnknown
}

// classifyDescribeStatement recognises `DESCRIBE [TABLE] name` / `DESC name`.
//
// All forms route to statementKindDescribeTable.
//
// Returns statementKind which is always statementKindDescribeTable.
func classifyDescribeStatement(_ []token) statementKind {
	return statementKindDescribeTable
}

// classifyCheckStatement recognises `CHECK TABLE name [PARTITION ...] [PART ...]`.
//
// Takes tokens ([]token) which is the CHECK-prefixed statement.
//
// Returns statementKind which is statementKindCheckTable on a match, or
// statementKindUnknown otherwise.
func classifyCheckStatement(tokens []token) statementKind {
	if len(tokens) < 2 {
		return statementKindUnknown
	}
	second := tokens[1].Keyword(tokenIdentifier)
	if second == "TABLE" {
		return statementKindCheckTable
	}
	return statementKindUnknown
}

// classifyKillStatement recognises `KILL QUERY ...` and `KILL MUTATION ...`.
//
// Takes tokens ([]token) which is the KILL-prefixed statement.
//
// Returns statementKind which is statementKindKillQuery or statementKindKillMutation on a
// match, or statementKindUnknown otherwise.
func classifyKillStatement(tokens []token) statementKind {
	if len(tokens) < 2 {
		return statementKindUnknown
	}
	switch tokens[1].Keyword(tokenIdentifier) {
	case "QUERY":
		return statementKindKillQuery
	case "MUTATION":
		return statementKindKillMutation
	default:
		return statementKindUnknown
	}
}

// classifyAttachStatement recognises top-level `ATTACH {TABLE | VIEW | DICTIONARY |
// DATABASE} name`.
//
// Bare `ATTACH name` (without one of the object keywords) falls back to
// statementKindUnknown so the caller can surface a diagnostic rather than misclassifying
// an unrelated statement as an ATTACH TABLE.
//
// Takes tokens ([]token) which is the ATTACH-prefixed statement.
//
// Returns statementKind which is statementKindAttachTable on a match, or
// statementKindUnknown otherwise.
func classifyAttachStatement(tokens []token) statementKind {
	if len(tokens) < 2 {
		return statementKindUnknown
	}
	switch tokens[1].Keyword(tokenIdentifier) {
	case objectKindTable, objectKindView, objectKindDictionary, objectKindDatabase:
		return statementKindAttachTable
	default:
		return statementKindUnknown
	}
}

// classifyDetachStatement recognises top-level `DETACH {TABLE | VIEW | DICTIONARY |
// DATABASE} name`.
//
// Bare `DETACH name` (without one of the object keywords) falls back to
// statementKindUnknown so the caller can surface a diagnostic rather than misclassifying
// an unrelated statement as a DETACH TABLE.
//
// Takes tokens ([]token) which is the DETACH-prefixed statement.
//
// Returns statementKind which is statementKindDetachTable on a match, or
// statementKindUnknown otherwise.
func classifyDetachStatement(tokens []token) statementKind {
	if len(tokens) < 2 {
		return statementKindUnknown
	}
	switch tokens[1].Keyword(tokenIdentifier) {
	case objectKindTable, objectKindView, objectKindDictionary, objectKindDatabase:
		return statementKindDetachTable
	default:
		return statementKindUnknown
	}
}

// classifyRenameStatement recognises `RENAME TABLE` / `RENAME DATABASE` / `RENAME
// DICTIONARY`.
//
// All three collapse to a single rename mutation kind; the parser side disambiguates and
// emits the appropriate catalogue change.
//
// Takes tokens ([]token) which is the RENAME-prefixed statement.
//
// Returns statementKind which is statementKindRenameTable on a match, or
// statementKindUnknown otherwise.
func classifyRenameStatement(tokens []token) statementKind {
	if len(tokens) < 2 {
		return statementKindUnknown
	}
	second := tokens[1].Keyword(tokenIdentifier)
	if second == "TABLE" || second == "DICTIONARY" || second == "DATABASE" {
		return statementKindRenameTable
	}
	return statementKindUnknown
}

// classifyExchangeStatement recognises `EXCHANGE TABLES a AND b`, ClickHouse's atomic
// two-table swap.
//
// The classifier only checks for the leading `EXCHANGE TABLES` token sequence; the parser
// body validates the AND token and the optional ON CLUSTER clause.
//
// Takes tokens ([]token) which is the EXCHANGE-prefixed statement.
//
// Returns statementKind which is statementKindExchangeTables on a match, or
// statementKindUnknown otherwise.
func classifyExchangeStatement(tokens []token) statementKind {
	if len(tokens) < 2 {
		return statementKindUnknown
	}
	second := tokens[1].Keyword(tokenIdentifier)
	if second == "TABLES" {
		return statementKindExchangeTables
	}
	return statementKindUnknown
}

// classifyWithStatement walks a WITH-prefixed statement to find the DML keyword (SELECT /
// INSERT) that follows the CTE list.
//
// It mirrors the duckdb implementation, tracking parenthesis depth and returning on the
// first top-level DML keyword.
//
// Takes tokens ([]token) which is the WITH-prefixed statement.
//
// Returns statementKind which is statementKindSelect or statementKindInsert, defaulting
// to statementKindSelect when no DML keyword is found.
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
		switch current.Keyword(tokenIdentifier) {
		case "SELECT":
			return statementKindSelect
		case "INSERT":
			return statementKindInsert
		}
	}
	return statementKindSelect
}

// unbalancedBracket finds the first parenthesis or square bracket of a statement that is
// not matched: a closing one with no opening one before it, a closing one of the other
// kind than the one it would close, or an opening one never closed.
//
// The parsers stop at the end of the statement wherever they are, so without this check a
// statement such as SELECT f( or SELECT * FROM numbers(10 would be read as complete.
//
// Takes tokens ([]token) which are the statement's tokens.
//
// Returns error which is a dialectkit.ParseError at the unmatched token, or nil when
// every parenthesis and bracket is matched.
func unbalancedBracket(tokens []token) error {
	var open []token
	for _, current := range tokens {
		switch current.Kind {
		case tokenLeftParen, tokenLeftBracket:
			open = append(open, current)
		case tokenRightParen, tokenRightBracket:
			opener := tokenLeftParen
			if current.Kind == tokenRightBracket {
				opener = tokenLeftBracket
			}
			if len(open) == 0 || open[len(open)-1].Kind != opener {
				return dialectkit.NewParseError(current, "unexpected '"+current.Raw+"'")
			}
			open = open[:len(open)-1]
		default:
		}
	}
	if len(open) == 0 {
		return nil
	}
	unmatched := open[len(open)-1]
	if unmatched.Kind == tokenLeftBracket {
		return dialectkit.NewParseError(unmatched, messageUnmatchedBracket)
	}
	return dialectkit.NewParseError(unmatched, messageUnmatchedParenthesis)
}

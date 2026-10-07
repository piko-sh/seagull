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
	"errors"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
)

// selectIntoMessage refuses a query that stores its row with INTO rather than returning
// it.
const selectIntoMessage = "SELECT ... INTO is not supported: it stores the row in variables or a file rather than " +
	"returning it, and a variable it sets lives only on the connection that ran it; select the values as columns instead"

var (
	// unanalysedStatementVerbs lists the words that open a statement MySQL or MariaDB runs
	// but that holds nothing Seagull analyses, such as SET or TRUNCATE. A statement opening
	// with any other word the dialect does not classify is rejected as unrecognised.
	unanalysedStatementVerbs = map[string]struct{}{
		"ALTER": {}, "ANALYZE": {}, "BACKUP": {}, "BEGIN": {}, "BINLOG": {}, "CACHE": {}, "CHANGE": {},
		"CHECK": {}, "CHECKSUM": {}, "CLONE": {}, "COMMIT": {}, "CREATE": {}, "DEALLOCATE": {},
		"DESC": {}, "DESCRIBE": {}, "DO": {}, "DROP": {}, "EXECUTE": {}, "EXPLAIN": {}, "FLUSH": {},
		"GET": {}, "GRANT": {}, "HANDLER": {}, "HELP": {}, "IMPORT": {}, "INSTALL": {}, "KILL": {},
		"LOAD": {}, "LOCK": {}, "OPTIMIZE": {}, "PREPARE": {}, "PURGE": {}, "RELEASE": {},
		"RENAME": {}, "REPAIR": {}, "RESET": {}, "RESIGNAL": {}, "RESTART": {}, "REVOKE": {},
		"ROLLBACK": {}, "SAVEPOINT": {}, "SET": {}, "SHUTDOWN": {}, "SIGNAL": {},
		"START": {}, "STOP": {}, "TRUNCATE": {}, "UNINSTALL": {}, "UNLOCK": {}, "USE": {}, "XA": {},
	}

	// selectItemReservedWords lists the reserved words that open the clause after a select
	// list, which an unquoted select-list item can therefore never be.
	selectItemReservedWords = []string{
		keywordFROM, keywordWHERE, keywordGROUP, keywordHAVING, keywordORDER, keywordLIMIT, keywordFOR,
		keywordUNION, keywordINTERSECT, keywordEXCEPT, keywordON, "INTO", "WINDOW", "LOCK", "PROCEDURE",
	}
)

// parseConditionStrictly reads the tokens a scan stepped over as the clause's condition,
// with the expression grammar reporting every token it cannot read, and records a syntax
// error when they do not form exactly one expression.
//
// Takes start (int) which is the index of the condition's first token.
// Takes end (int) which is one past the index of its last token.
//
// Returns []ast.Expression which holds the condition, or nil when it is malformed.
func (p *parser) parseConditionStrictly(start, end int) []ast.Expression {
	if start < 0 || start >= end || end > len(p.Tokens) {
		return nil
	}
	scratch := p.newChildParser(p.Tokens[start:end:end])
	scratch.skipSubqueries = true
	scratch.strictExpressions = true
	expression := scratch.parseExpression()
	if scratch.syntaxError != nil {
		p.recordSyntaxError(p.conditionError(scratch.syntaxError, scratch.AtEnd()))
		return nil
	}
	if !scratch.AtEnd() {
		p.recordSyntaxError(dialectkit.NewParseError(scratch.Current(), "unexpected "+scratch.Current().Raw+
			" after the condition"))
		return nil
	}
	return []ast.Expression{expression}
}

// conditionError places a syntax error met while reading a condition: an error the
// condition's own parser found at the end of the condition is reported at the token that
// follows the condition in the statement, since the condition stopped short there.
//
// Takes err (error) which is the error the condition's parser recorded.
// Takes atEnd (bool) which is true when that parser had reached the condition's end.
//
// Returns error which is the error to record.
func (p *parser) conditionError(err error, atEnd bool) error {
	if !atEnd {
		return err
	}
	parseError, ok := errors.AsType[*dialectkit.ParseError](err)
	if !ok || parseError.Token != "" {
		return err
	}
	return dialectkit.NewParseError(p.Current(), parseError.Message)
}

// requireSelectItem reports a syntax error when the current token is a reserved word that
// opens the clause after a select list, which an unquoted select-list item can never be,
// so SELECT FROM t is not read as a column named FROM.
//
// Returns error which is the recorded dialectkit.ParseError, or nil.
func (p *parser) requireSelectItem() error {
	if !p.IsKeyword(selectItemReservedWords...) {
		return nil
	}
	return p.syntaxErrorAt("expected a select-list item before " + p.Current().Raw)
}

// requireStrictExpression records a syntax error at the current token when the parser
// reads a condition strictly, for a token the expression grammar would otherwise step
// over.
//
// Takes message (string) which says what was expected.
func (p *parser) requireStrictExpression(message string) {
	if p.strictExpressions {
		p.reportSyntaxError(message)
	}
}

// analyseUnanalysedStatement answers a statement no analyser takes: a statement the
// engine runs that holds nothing to analyse yields an empty analysis, unless it holds a
// parameter Seagull cannot bind, and any other statement is rejected.
//
// Takes parsed (*parsedStatement) which is the statement.
//
// Returns *ast.RawQueryAnalysis which is empty for a statement with nothing to analyse.
// Returns error which is a dialectkit.ParseError for a statement the dialect cannot read
// or a parameter it cannot bind.
func analyseUnanalysedStatement(parsed *parsedStatement) (*ast.RawQueryAnalysis, error) {
	if len(parsed.tokens) == 0 {
		return &ast.RawQueryAnalysis{}, nil
	}
	first := parsed.tokens[0]
	verb := first.Keyword(tokenIdentifier)
	if parsed.kind == statementKindUnknown {
		if err := unrecognisedStatementError(first, verb); err != nil {
			return nil, err
		}
	}
	for _, candidate := range parsed.tokens {
		if isParameterToken(candidate.Kind) {
			return nil, dialectkit.UnboundParameterError(candidate, verb, "")
		}
	}
	return &ast.RawQueryAnalysis{}, nil
}

// unrecognisedStatementError rejects an unclassified statement whose first word opens no
// statement the engine runs, or opens one Seagull reads only in another form.
//
// Takes first (token) which is the statement's first token.
// Takes verb (string) which is that token's upper-cased keyword, or empty.
//
// Returns error which is a dialectkit.ParseError at the first token, or nil for a
// statement with nothing to analyse.
func unrecognisedStatementError(first token, verb string) error {
	switch {
	case first.Kind == tokenLeftParen:
		return dialectkit.NewParseError(first, "expected a query inside the parentheses")
	case verb == "TABLE":
		return dialectkit.NewParseError(first, "a TABLE statement is not supported; write SELECT * FROM the table instead")
	case verb == "":
		return dialectkit.NewParseError(first, "expected a statement")
	}
	if _, runs := unanalysedStatementVerbs[verb]; runs {
		return nil
	}
	return dialectkit.NewParseError(first, "unrecognised statement starting with "+first.Raw)
}

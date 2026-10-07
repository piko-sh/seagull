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
	"errors"
	"slices"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
)

var (
	// errSubquerySkipped is what analysing a subquery returns in a parser that only records
	// the names of a clause whose subqueries another scan analysed.
	errSubquerySkipped = errors.New("subquery left unanalysed while recording a clause's names")

	// placeholderTokenKinds tells the shared placeholder locator how the postgres tokeniser
	// classifies the tokens it needs.
	placeholderTokenKinds = dialectkit.PlaceholderTokens[tokenKind]{
		IsParameter:      func(candidate token) bool { return isParameterToken(candidate.Kind) },
		Word:             tokenIdentifier,
		LeftParenthesis:  tokenLeftParen,
		RightParenthesis: tokenRightParen,
	}
)

// valuesRowTokens tells the shared VALUES row splitter how the postgres tokeniser
// classifies the tokens that nest and separate a row.
var valuesRowTokens = dialectkit.ListTokens[tokenKind]{
	Openers: []tokenKind{tokenLeftParen, tokenLeftBracket},
	Closers: []tokenKind{tokenRightParen, tokenRightBracket},
	Comma:   tokenComma,
}

// recordValuesRow records one parenthesised VALUES row of an INSERT: its span and its
// items' spans for the top-level statement, and each item's expression as a clause
// expression, its subqueries left unanalysed.
//
// Takes open (int) which is the index of the row's "(".
// Takes closing (int) which is the index of its ")".
func (p *parser) recordValuesRow(open, closing int) {
	p.spans.AddValuesRow(dialectkit.ValuesRow(p.Tokens, open, closing, valuesRowTokens, p.spans))
	p.recordClauseExpressions(ast.ClauseInsertValue, p.reparse(open+1, closing, true, false))
}

// parseCondition parses the condition of a WHERE, HAVING or join clause, as
// parseWhereClause does, and records the expression it holds under the clause.
//
// Takes clause (ast.Clause) which is the clause the condition belongs to.
//
// Returns int which is the index of the condition's first token.
func (p *parser) parseCondition(clause ast.Clause) int {
	start := p.Position
	if condition := p.parseWhereClause(); condition != nil {
		p.recordClauseExpressions(clause, []ast.Expression{condition})
	}
	return start
}

// parseConditionTokens validates previously scanned tokens as one complete condition.
//
// It uses a separate parser to avoid registering parameters or analysing subqueries
// twice.
//
// Takes start (int) which is the index of the condition's first token.
// Takes end (int) which is one past the index of its last token.
//
// Returns ast.Expression which is the condition, or nil when it does not parse.
func (p *parser) parseConditionTokens(start, end int) ast.Expression {
	if start < 0 || start >= end || end > len(p.Tokens) {
		return nil
	}
	scratch := p.newChildParser(p.Tokens[start:end:end])
	scratch.syntax = new(syntaxState)
	scratch.skipSubqueries = true
	condition := scratch.parseExpression()
	if scratch.syntax.err != nil {
		_ = p.recordSyntaxError(dialectkit.ReportEndAt(scratch.syntax.err, p.tokenAfter(end)))
		return nil
	}
	if !scratch.AtEnd() {
		_ = p.recordSyntaxError(dialectkit.NewParseError(scratch.Current(), "expected the end of the condition"))
		return nil
	}
	return condition
}

// tokenAfter returns the token at an index of the statement, or the end of input past its
// last token.
//
// Takes index (int) which is the index.
//
// Returns token which is the token at index.
func (p *parser) tokenAfter(index int) token {
	return p.PeekAt(index - p.Position)
}

// recordClauseExpressions records the expressions a clause holds, unless the parser is
// inside a clause whose names the analyser must not check, such as an ON CONFLICT action
// that names the EXCLUDED row.
//
// Takes clause (ast.Clause) which is the clause.
// Takes expressions ([]ast.Expression) which are its expressions.
func (p *parser) recordClauseExpressions(clause ast.Clause, expressions []ast.Expression) {
	if p.suppressClauses {
		return
	}
	for _, expression := range expressions {
		if expression != nil {
			p.clauseExpressions = append(p.clauseExpressions, ast.ClauseExpression{Expression: expression, Clause: clause})
		}
	}
}

// takeClauseExpressions removes and returns the clause expressions recorded since mark,
// so each query takes the expressions of its own clauses.
//
// Takes mark (int) which is the count of clause expressions before the query's clauses.
//
// Returns []ast.ClauseExpression which holds the query's clause expressions.
func (p *parser) takeClauseExpressions(mark int) []ast.ClauseExpression {
	if mark >= len(p.clauseExpressions) {
		return nil
	}
	taken := slices.Clone(p.clauseExpressions[mark:])
	p.clauseExpressions = p.clauseExpressions[:mark]
	return taken
}

// reparse reads skipped tokens as expressions for clause-reference tracking. It uses a
// separate parser so partial or unsupported syntax does not affect statement analysis.
//
// Takes start (int) which is the index of the first token.
// Takes end (int) which is one past the index of the last token.
// Takes list (bool) which is true for a comma-separated list, such as ORDER BY keys, each
// of which may carry a direction and NULLS placement.
// Takes subqueries (bool) which is true when the subqueries in the tokens are analysed,
// for a clause whose scan did not analyse them itself.
//
// Returns []ast.Expression which holds the parsed expressions.
func (p *parser) reparse(start, end int, list, subqueries bool) []ast.Expression {
	if start < 0 || start >= end || end > len(p.Tokens) {
		return nil
	}
	scratch := p.newChildParser(p.Tokens[start:end:end])
	scratch.syntax = new(syntaxState)
	scratch.skipSubqueries = !subqueries
	var expressions []ast.Expression
	for !scratch.AtEnd() {
		before := scratch.Position
		expressions = append(expressions, scratch.parseExpression())
		if list {
			scratch.skipSortModifiers()
		}
		if !list || scratch.Current().Kind != tokenComma || scratch.Position == before {
			break
		}
		scratch.Advance()
	}
	return expressions
}

// skipSortModifiers consumes the direction, USING operator and NULLS placement that may
// follow an ORDER BY key.
func (p *parser) skipSortModifiers() {
	if !p.MatchKeyword("ASC") && !p.MatchKeyword("DESC") && p.MatchKeyword(keywordUSING) {
		p.Advance()
	}
	if p.MatchKeyword("NULLS") {
		if !p.MatchKeyword(keywordFIRST) {
			p.MatchKeyword("LAST")
		}
	}
}

// locate records where a column reference or function call parsed from the tokens
// starting at first was written, unless it already knows.
//
// Takes expression (ast.Expression) which is the parsed expression.
// Takes first (int) which is the index of its first token.
//
// Returns ast.Expression which is the expression.
func (p *parser) locate(expression ast.Expression, first int) ast.Expression {
	switch typed := expression.(type) {
	case *ast.ColumnRefExpression:
		if !typed.Span.Known() {
			typed.Span = dialectkit.TokenRangeSpan(p.Tokens, first, p.Position)
		}
	case *ast.FunctionCallExpression:
		if !typed.Span.Known() {
			typed.Span = p.nameSpan(first)
		}
	case *ast.WindowFunctionExpression:
		if typed.Function != nil && !typed.Function.Span.Known() {
			typed.Function.Span = p.nameSpan(first)
		}
	}
	return expression
}

// nameSpan returns the span of a possibly qualified function name starting at the token
// at first: its words and the dots between them.
//
// Takes first (int) which is the index of the name's first token.
//
// Returns ast.Span which covers the name.
func (p *parser) nameSpan(first int) ast.Span {
	end := first + 1
	for end+1 < len(p.Tokens) && p.Tokens[end].Kind == tokenDot && p.Tokens[end+1].Kind == tokenIdentifier {
		end += 2
	}
	return dialectkit.TokenRangeSpan(p.Tokens, first, end)
}

// recordSetTargets records each column an UPDATE's SET clause assigns, among the tokens
// from start up to end, as a column reference. The targets of an ON CONFLICT action's SET
// are recorded too, though the action's other expressions are not, as they name columns
// of the table the INSERT writes.
//
// Takes start (int) which is the index of the first token of the target list.
// Takes end (int) which is one past the index of its last token.
func (p *parser) recordSetTargets(start, end int) {
	for index := start; index < end && index < len(p.Tokens); index++ {
		if p.Tokens[index].Kind != tokenIdentifier {
			continue
		}
		p.clauseExpressions = append(p.clauseExpressions, ast.ClauseExpression{
			Expression: &ast.ColumnRefExpression{TableAlias: "", ColumnName: p.Tokens[index].Value, Span: dialectkit.TokenRangeSpan(p.Tokens, index, index+1)},
			Clause:     ast.ClauseSetTarget,
		})
	}
}

// recordSetValues records the value an UPDATE's SET clause assigns, among the tokens from
// start up to end: one expression, or each element of a parenthesised list a multi-column
// assignment gives.
//
// Takes start (int) which is the index of the value's first token.
// Takes end (int) which is one past the index of its last token.
func (p *parser) recordSetValues(start, end int) {
	if start >= end || start+1 >= len(p.Tokens) {
		return
	}
	opensList := p.Tokens[start].Kind == tokenLeftParen && p.Tokens[end-1].Kind == tokenRightParen && !p.opensQueryAt(start)
	if opensList {
		p.recordClauseExpressions(ast.ClauseSetValue, p.reparse(start+1, end-1, true, true))
		return
	}
	p.recordClauseExpressions(ast.ClauseSetValue, p.reparse(start, end, false, true))
}

// recordStatementSpans fills the analysis of the top-level statement with its spans, its
// placeholders and its terminator.
//
// Takes p (*parser) which is the parser that walked the statement.
// Takes analysis (*ast.RawQueryAnalysis) which receives the spans.
// Takes parsed (*parsedStatement) which is the statement.
// Takes statement (ast.ParsedStatement) which locates it.
func (engine *Dialect) recordStatementSpans(p *parser, analysis *ast.RawQueryAnalysis, parsed *parsedStatement,
	statement ast.ParsedStatement) {
	spans := p.spans.Finish(analysis)
	spans.Placeholders = dialectkit.PlaceholderSpans(parsed.tokens, placeholderTokenKinds, engine.info.Placeholders,
		statement.Location)
	spans.Terminator = dialectkit.TerminatorSpan(engine.settings.LexicalRules, parsed.source, statement.Location,
		statement.Location+statement.Length)
	analysis.Spans = spans
}

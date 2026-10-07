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
	"slices"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
)

const (
	// qualifiedNameTokens is the number of tokens of a qualified name: the qualifier, the
	// dot and the name.
	qualifiedNameTokens = 3
)

var (
	// errSubquerySkipped is what analysing a subquery returns in a parser that only records
	// the names of a clause whose subqueries another scan analysed.
	errSubquerySkipped = errors.New("subquery left unanalysed while recording a clause's names")

	// placeholderTokenKinds tells the shared placeholder locator how the mysql tokeniser
	// classifies the tokens it needs.
	placeholderTokenKinds = dialectkit.PlaceholderTokens[tokenKind]{
		IsParameter:      func(candidate token) bool { return isParameterToken(candidate.Kind) },
		Word:             tokenIdentifier,
		LeftParenthesis:  tokenLeftParen,
		RightParenthesis: tokenRightParen,
	}

	// valuesRowTokens tells the shared VALUES row splitter how the mysql tokeniser
	// classifies the tokens that nest and separate a row.
	valuesRowTokens = dialectkit.ListTokens[tokenKind]{
		Openers: []tokenKind{tokenLeftParen},
		Closers: []tokenKind{tokenRightParen},
		Comma:   tokenComma,
	}
)

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

// parseCondition scans, validates and records a clause's condition.
//
// The tokens must form exactly one complete expression; missing or malformed conditions
// produce syntax errors.
//
// Takes clause (ast.Clause) which is the clause the condition belongs to.
// Takes scan (func()) which steps over the condition, registering its parameters.
//
// Returns int which is the index of the condition's first token.
func (p *parser) parseCondition(clause ast.Clause, scan func()) int {
	start := p.Position
	scan()
	if p.Position == start {
		p.reportSyntaxError("expected a condition")
		return start
	}
	p.recordClauseExpressions(clause, p.parseConditionStrictly(start, p.Position))
	return start
}

// recordClauseExpressions records the expressions a clause holds, unless the parser is
// inside a clause whose names the analyser must not check, such as ON DUPLICATE KEY
// UPDATE naming the inserted row.
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

// dropClauseExpressions discards the clause expressions recorded since mark, for a query
// whose names the analyser does not check, such as a VALUES list whose ORDER BY names its
// generated columns.
//
// Takes mark (int) which is the count of clause expressions to keep.
func (p *parser) dropClauseExpressions(mark int) {
	if mark < len(p.clauseExpressions) {
		p.clauseExpressions = p.clauseExpressions[:mark]
	}
}

// reparse reads skipped tokens as expressions for clause-reference tracking. It uses a
// separate parser so partial or unsupported syntax does not affect statement analysis.
//
// Takes start (int) which is the index of the first token.
// Takes end (int) which is one past the index of the last token.
// Takes list (bool) which is true for a comma-separated list, such as ORDER BY keys, each
// of which may carry a direction.
// Takes subqueries (bool) which is true when the subqueries in the tokens are analysed,
// for a clause whose scan did not analyse them itself.
//
// Returns []ast.Expression which holds the parsed expressions.
func (p *parser) reparse(start, end int, list, subqueries bool) []ast.Expression {
	if start < 0 || start >= end || end > len(p.Tokens) {
		return nil
	}
	var expressions []ast.Expression
	scratch := p.newChildParser(p.Tokens[start:end:end])
	scratch.skipSubqueries = !subqueries
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

// skipSortModifiers consumes the direction and NULLS placement that may follow an ORDER
// BY key.
func (p *parser) skipSortModifiers() {
	if !p.MatchKeyword(keywordASC) {
		p.MatchKeyword(keywordDESC)
	}
	if p.MatchKeyword(keywordNULLS) && !p.MatchKeyword(keywordFIRST) {
		p.MatchKeyword(keywordLAST)
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

// recordSetTarget records the column an UPDATE's SET clause assigns, written in the
// tokens from start up to end as a name or a qualified name, as a column reference.
//
// Takes start (int) which is the index of the target's first token.
// Takes end (int) which is one past the index of its last token.
func (p *parser) recordSetTarget(start, end int) {
	if start >= end || end > len(p.Tokens) {
		return
	}
	target := &ast.ColumnRefExpression{TableAlias: "", ColumnName: p.Tokens[end-1].Value, Span: dialectkit.TokenRangeSpan(p.Tokens, start, end)}
	if end-start == qualifiedNameTokens {
		target.TableAlias = p.Tokens[start].Value
	}
	p.recordClauseExpressions(ast.ClauseSetTarget, []ast.Expression{target})
}

// recordSetTargetList records each column a multi-column assignment of an UPDATE's SET
// clause assigns, among the tokens from start up to end, as a column reference.
//
// Takes start (int) which is the index of the first token of the target list.
// Takes end (int) which is one past the index of its last token.
func (p *parser) recordSetTargetList(start, end int) {
	for index := start; index < end && index < len(p.Tokens); index++ {
		if p.Tokens[index].Kind == tokenIdentifier {
			p.recordSetTarget(index, index+1)
		}
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
	opensList := p.Tokens[start].Kind == tokenLeftParen && p.Tokens[end-1].Kind == tokenRightParen &&
		!p.Tokens[start+1].IsKeyword(tokenIdentifier, keywordSELECT) && !p.Tokens[start+1].IsKeyword(tokenIdentifier, keywordWITH) &&
		!p.Tokens[start+1].IsKeyword(tokenIdentifier, keywordVALUES)
	if opensList {
		p.recordClauseExpressions(ast.ClauseSetValue, p.reparse(start+1, end-1, true, false))
		return
	}
	p.recordClauseExpressions(ast.ClauseSetValue, p.reparse(start, end, false, false))
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
	spans.Placeholders = dialectkit.PlaceholderSpans(parsed.tokens, placeholderTokenKinds, placeholderForms,
		statement.Location)
	spans.Terminator = dialectkit.TerminatorSpan(engine.settings.lexicalRules(), parsed.source, statement.Location,
		statement.Location+statement.Length)
	analysis.Spans = spans
}

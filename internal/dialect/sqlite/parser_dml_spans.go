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
	"slices"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/sqlite/lexical"
)

var (
	// placeholderTokenKinds tells the shared placeholder locator how the SQLite tokeniser
	// classifies the tokens it needs.
	placeholderTokenKinds = dialectkit.PlaceholderTokens[tokenKind]{
		IsParameter:      func(candidate token) bool { return isParameterToken(candidate.Kind) },
		Word:             tokenIdentifier,
		LeftParenthesis:  tokenLeftParen,
		RightParenthesis: tokenRightParen,
	}

	// valuesRowTokens tells the shared VALUES row splitter how the SQLite tokeniser
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

// parseCondition parses the condition of a WHERE or HAVING clause, as
// parseWhereExpression does, and records the expression it holds under the clause.
//
// Takes clause (ast.Clause) which is the clause the condition belongs to.
//
// Returns int which is the index of the condition's first token.
func (p *parser) parseCondition(clause ast.Clause) int {
	start := p.Position
	p.parseWhereExpression()
	p.recordClauseExpressions(clause, p.conditionExpressions(start, p.Position))
	return start
}

// conditionExpressions validates scanned tokens as a single condition expression.
//
// An isolated parser avoids changing the statement's registered parameters or subqueries.
// Empty or incomplete conditions produce syntax errors.
//
// Takes start (int) which is the index of the condition's first token.
// Takes end (int) which is one past the index of its last token.
//
// Returns []ast.Expression which holds the condition, or nil when it does not read.
func (p *parser) conditionExpressions(start, end int) []ast.Expression {
	for end > start && p.Tokens[end-1].Kind == tokenSemicolon {
		end--
	}
	if start >= end {
		p.recordSyntaxError(dialectkit.NewParseError(p.tokenAt(start), messageExpectedCondition))
		return nil
	}
	scratch := p.newChildParser(p.Tokens[start:end:end])
	scratch.skipSubqueries = true
	expression := scratch.parseExpression()
	if scratch.syntaxError == nil && !scratch.AtEnd() {
		scratch.recordSyntaxError(dialectkit.NewParseError(scratch.Current(), messageConditionLeftover))
	}
	if scratch.syntaxError != nil {
		p.recordSyntaxError(dialectkit.ReportEndAt(scratch.syntaxError, p.tokenAt(end)))
		return nil
	}
	return []ast.Expression{expression}
}

// recordClauseExpressions records the expressions a clause holds, unless the parser is
// inside a clause whose names the analyser must not check, such as an upsert action that
// names the excluded row.
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

// reparse reads skipped tokens as expressions for clause-reference tracking.
//
// It uses a separate parser so partial or unsupported syntax does not affect statement
// analysis. The parser shares this parser's interrupt, and a panic in it is not caught
// here: it fails the whole statement through the engine's recovery.
//
// Takes start (int) which is the index of the first token.
// Takes end (int) which is one past the index of the last token.
// Takes list (bool) which is true for a comma-separated list, such as ORDER BY keys, each
// of which may carry a direction and NULLS placement.
// Takes subqueries (bool) which is true when the subqueries in the tokens are analysed,
// for a clause whose scan did not analyse them itself.
//
// Returns []ast.Expression which holds the parsed expressions.
func (p *parser) reparse(start, end int, list, subqueries bool) (expressions []ast.Expression) {
	if start < 0 || start >= end || end > len(p.Tokens) {
		return nil
	}
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
	if !p.MatchKeyword("ASC") {
		p.MatchKeyword("DESC")
	}
	if p.MatchKeyword("NULLS") {
		if !p.MatchKeyword("FIRST") {
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
			typed.Span = dialectkit.TokenRangeSpan(p.Tokens, first, first+1)
		}
	case *ast.WindowFunctionExpression:
		if typed.Function != nil && !typed.Function.Span.Known() {
			typed.Function.Span = dialectkit.TokenRangeSpan(p.Tokens, first, first+1)
		}
	}
	return expression
}

// recordAssignment records the columns and value of one assignment of an UPDATE's SET
// clause, among the tokens from start up to end: a column or a parenthesised column list,
// "=", and the value or a parenthesised list of values.
//
// Takes start (int) which is the index of the assignment's first token.
// Takes end (int) which is one past the index of its last token.
func (p *parser) recordAssignment(start, end int) {
	equals := p.assignmentOperator(start, end)
	if equals < 0 {
		return
	}
	var targets []ast.Expression
	for index := start; index < equals; index++ {
		if p.Tokens[index].Kind == tokenIdentifier {
			targets = append(targets, &ast.ColumnRefExpression{
				TableAlias: "",
				ColumnName: p.Tokens[index].Value,
				Span:       dialectkit.TokenRangeSpan(p.Tokens, index, index+1),
			})
		}
	}
	p.recordClauseExpressions(ast.ClauseSetTarget, targets)
	valueStart := equals + 1
	if p.Tokens[start].Kind == tokenLeftParen && valueStart+1 < end && p.Tokens[valueStart].Kind == tokenLeftParen &&
		p.Tokens[end-1].Kind == tokenRightParen && !p.Tokens[valueStart+1].IsKeyword(tokenIdentifier, keywordSELECT) &&
		!p.Tokens[valueStart+1].IsKeyword(tokenIdentifier, keywordWITH) {
		p.recordClauseExpressions(ast.ClauseSetValue, p.reparse(valueStart+1, end-1, true, true))
		return
	}
	p.recordClauseExpressions(ast.ClauseSetValue, p.reparse(valueStart, end, false, true))
}

// assignmentOperator returns the index of the "=" outside every parenthesis that splits
// an assignment's columns from its value, or -1.
//
// Takes start (int) which is the index of the assignment's first token.
// Takes end (int) which is one past the index of its last token.
//
// Returns int which is the index of "=", or -1.
func (p *parser) assignmentOperator(start, end int) int {
	depth := 0
	for index := start; index < end && index < len(p.Tokens); index++ {
		kind := p.Tokens[index].Kind
		if kind == tokenLeftParen {
			depth++
		}
		if kind == tokenRightParen {
			depth--
		}
		if depth == 0 && kind == tokenOperator && p.Tokens[index].Value == "=" {
			return index
		}
	}
	return -1
}

// writtenSchema returns the schema a table reference starting at the token at first
// writes, as in main.items, or empty when it writes none.
//
// Takes first (int) which is the index of the reference's first token.
//
// Returns string which is the schema as written.
func (p *parser) writtenSchema(first int) string {
	if first+2 < len(p.Tokens) && p.Tokens[first].Kind == tokenIdentifier && p.Tokens[first+1].Kind == tokenDot &&
		p.Tokens[first+2].Kind == tokenIdentifier {
		return p.Tokens[first].Value
	}
	return ""
}

// recordStatementSpans fills the analysis of the top-level statement with its spans, its
// placeholders and its terminator.
//
// Takes p (*parser) which is the parser that walked the statement.
// Takes analysis (*ast.RawQueryAnalysis) which receives the spans.
// Takes parsed (*parsedStatement) which is the statement.
// Takes statement (ast.ParsedStatement) which locates it.
func (*Dialect) recordStatementSpans(p *parser, analysis *ast.RawQueryAnalysis, parsed *parsedStatement,
	statement ast.ParsedStatement) {
	spans := p.spans.Finish(analysis)
	spans.Placeholders = dialectkit.PlaceholderSpans(parsed.tokens, placeholderTokenKinds, placeholders, statement.Location)
	spans.Terminator = dialectkit.TerminatorSpan(lexical.Rules(), parsed.source, statement.Location,
		statement.Location+statement.Length)
	analysis.Spans = spans
}

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
	"errors"
	"maps"
	"slices"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
)

var (
	// errSubquerySkipped is what analysing a subquery returns in a parser that only records
	// the names of a clause whose subqueries another scan analysed.
	errSubquerySkipped = errors.New("subquery left unanalysed while recording a clause's names")

	// placeholderTokenKinds tells the shared placeholder locator how the DuckDB tokeniser
	// classifies the tokens it needs.
	placeholderTokenKinds = dialectkit.PlaceholderTokens[tokenKind]{
		IsParameter:      func(candidate token) bool { return isParameterToken(candidate.Kind) },
		Word:             tokenIdentifier,
		LeftParenthesis:  tokenLeftParen,
		RightParenthesis: tokenRightParen,
	}

	// valuesRowTokens tells the shared VALUES row splitter how the DuckDB tokeniser
	// classifies the tokens that nest and separate a row.
	valuesRowTokens = dialectkit.ListTokens[tokenKind]{
		Openers: []tokenKind{tokenLeftParen, tokenLeftBracket, tokenLeftBrace},
		Closers: []tokenKind{tokenRightParen, tokenRightBracket, tokenRightBrace},
		Comma:   tokenComma,
	}
)

// recordSpan records the tokens from first up to end as a clause of the top-level
// statement, unless the statement's clause of that kind is recorded already.
//
// Takes field (dialectkit.SpanField) which names the clause.
// Takes first (int) which is the index of the clause's first token.
// Takes end (int) which is one past the index of its last token.
func (p *parser) recordSpan(field dialectkit.SpanField, first, end int) {
	dialectkit.RecordTokenRange(p.spans, field, p.Tokens, first, end)
}

// extendSpan widens a clause of the top-level statement to cover the tokens from first up
// to end as well, for a clause written in several parts, such as LIMIT and OFFSET.
//
// Takes field (dialectkit.SpanField) which names the clause.
// Takes first (int) which is the index of the part's first token.
// Takes end (int) which is one past the index of its last token.
func (p *parser) extendSpan(field dialectkit.SpanField, first, end int) {
	dialectkit.ExtendTokenRange(p.spans, field, p.Tokens, first, end)
}

// recordRowShaping records the tokens from first up to end as a clause of the top-level
// statement that shapes its rows after HAVING, such as QUALIFY.
//
// Takes first (int) which is the index of the clause's first token.
// Takes end (int) which is one past the index of its last token.
func (p *parser) recordRowShaping(first, end int) {
	if start, stop, ok := dialectkit.TokenRange(p.Tokens, first, end); ok {
		p.spans.AddRowShaping(start, stop)
	}
}

// recordOrderByPlace records, for the top-level statement, that its ORDER BY clause is or
// would be written right after the token before the current one: after the clauses of its
// last arm, QUALIFY and WINDOW included, and before the ordering and pagination.
func (p *parser) recordOrderByPlace() {
	dialectkit.RecordOrderByPlaceAt(p.spans, p.Tokens, p.Position)
}

// itemSpan returns the span of one select-list or RETURNING item.
//
// Takes first (int) which is the index of the item's first token.
// Takes expressionEnd (int) which is one past the index of its expression's last token.
// Takes end (int) which is one past the index of the item's last token, its alias
// included.
// Takes star (bool) which is true for a star item.
//
// Returns ast.ItemSpan which locates the item, or is zero outside the top-level
// statement.
func (p *parser) itemSpan(first, expressionEnd, end int, star bool) ast.ItemSpan {
	return dialectkit.TokenItemSpan(p.spans, p.Tokens, first, expressionEnd, end, star)
}

// recordValuesRow records one parenthesised VALUES row of an INSERT: its span and its
// items' spans for the top-level statement, and each item's expression as a clause
// expression, its subqueries left unanalysed.
//
// Takes open (int) which is the index of the row's "(".
// Takes closing (int) which is the index of its ")".
func (p *parser) recordValuesRow(open, closing int) {
	p.spans.AddValuesRow(dialectkit.ValuesRow(p.Tokens, open, closing, valuesRowTokens, p.spans))
	p.recordClauseExpressions(ast.ClauseInsertValue, p.reparse(open+1, closing, true))
}

// parseCondition scans and records a clause's condition and parameters.
//
// The scanned text must form exactly one complete expression. Empty or malformed
// conditions produce syntax errors.
//
// Takes clause (ast.Clause) which is the clause the condition belongs to.
// Takes terminators (map[string]struct{}) which are the keywords that end the condition.
//
// Returns int which is the index of the condition's first token.
func (p *parser) parseCondition(clause ast.Clause, terminators map[string]struct{}) int {
	start := p.Position
	p.skipTokensUntilTerminatorSet(terminators)
	if p.Position == start {
		p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected a condition"))
		return start
	}
	expression, problem := p.parseConditionExpression(start, p.Position)
	if problem != nil {
		p.recordSyntaxError(problem)
		return start
	}
	p.recordClauseExpressions(clause, []ast.Expression{expression})
	return start
}

// parseConditionExpression parses the tokens a condition's scan stepped over, from start
// up to end, as one expression, in a parser of its own whose parameters and subqueries
// are discarded, as reparse does.
//
// Takes start (int) which is the index of the condition's first token.
// Takes end (int) which is one past the index of its last token.
//
// Returns expression (ast.Expression) which is the condition, or nil when it does not
// read as one expression.
// Returns problem (error) which is the syntax error the condition holds: the expression
// grammar's own, placed at the token after the condition when it ran out of tokens, or
// one at the first token left over after the expression.
func (p *parser) parseConditionExpression(start, end int) (expression ast.Expression, problem error) {
	scratch := p.newChildParser(tokenView(p.Tokens, start, end))
	scratch.statement = newStatementState(scratch.Tokens)
	scratch.namedParameterMap = maps.Clone(p.namedParameterMap)
	scratch.parameterCount = p.parameterCount
	scratch.skipSubqueries = true
	expression = scratch.locate(scratch.parseExpression(), 0)
	if err := scratch.syntaxError(); err != nil {
		return nil, p.conditionSyntaxError(err, end)
	}
	if !scratch.AtEnd() {
		return nil, dialectkit.NewParseError(scratch.Current(), "expected the end of the condition")
	}
	p.adoptArgumentNames(scratch)
	return expression, nil
}

// conditionSyntaxError places a syntax error the expression grammar raised at the end of
// a condition's tokens on the token that follows the condition, so it names what the
// statement holds there rather than the end of the input.
//
// Takes err (error) which is the expression grammar's syntax error.
// Takes end (int) which is the index of the token after the condition.
//
// Returns error which is the syntax error to record.
func (p *parser) conditionSyntaxError(err error, end int) error {
	parseError, ok := errors.AsType[*dialectkit.ParseError](err)
	if !ok || parseError.Token != "" || end >= len(p.Tokens) {
		return err
	}
	return dialectkit.NewParseError(p.Tokens[end], parseError.Message)
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

// dropClauseExpressions discards the clause expressions recorded since mark, for a query
// whose names the analyser does not check, such as a VALUES list whose ORDER BY names its
// generated columns, or a PIVOT whose lists name the pivoted rows.
//
// Takes mark (int) which is the count of clause expressions to keep.
func (p *parser) dropClauseExpressions(mark int) {
	if mark < len(p.clauseExpressions) {
		p.clauseExpressions = p.clauseExpressions[:mark]
	}
}

// reparse extracts expressions from tokens already visited by a clause scan.
//
// It uses an isolated parser so collecting names cannot change the statement's
// parameters, subqueries or errors. Unsupported syntax ends the collection early.
//
// Takes start (int) which is the index of the first token.
// Takes end (int) which is one past the index of the last token.
// Takes list (bool) which is true for a comma-separated list, such as ORDER BY keys, each
// of which may carry a direction and NULLS placement.
//
// Returns []ast.Expression which holds the parsed expressions.
func (p *parser) reparse(start, end int, list bool) (expressions []ast.Expression) {
	if start < 0 || start >= end || end > len(p.Tokens) {
		return nil
	}
	scratch := p.newChildParser(tokenView(p.Tokens, start, end))
	scratch.statement = newStatementState(scratch.Tokens)
	scratch.namedParameterMap = maps.Clone(p.namedParameterMap)
	scratch.parameterCount = p.parameterCount
	scratch.skipSubqueries = true
	for !scratch.AtEnd() {
		before := scratch.Position
		expressions = append(expressions, scratch.locate(scratch.parseExpression(), before))
		if list {
			scratch.skipSortModifiers()
		}
		if !list || scratch.Current().Kind != tokenComma || scratch.Position == before {
			break
		}
		scratch.Advance()
	}
	p.adoptArgumentNames(scratch)
	return expressions
}

// skipSortModifiers consumes the direction and NULLS placement that may follow an ORDER
// BY key.
func (p *parser) skipSortModifiers() {
	if !p.MatchKeyword(keywordASC) {
		p.MatchKeyword(keywordDESC)
	}
	if p.MatchKeyword(keywordNULLS) {
		if !p.MatchKeyword(keywordFIRST) {
			p.MatchKeyword(keywordLAST)
		}
	}
}

// locate records where a column reference or function call parsed from the tokens
// starting at first was written, unless it already knows: the whole reference for a
// column, the name for a call, and the whole name for a three-part name read as a field
// of a column.
//
// Takes expression (ast.Expression) which is the parsed expression.
// Takes first (int) which is the index of its first token.
//
// Returns ast.Expression which is the expression.
func (p *parser) locate(expression ast.Expression, first int) ast.Expression {
	switch typed := expression.(type) {
	case *ast.ColumnRefExpression:
		if !typed.Span.Known() {
			typed.Span = p.absoluteSpan(first, p.Position)
		}
	case *ast.FunctionCallExpression:
		if !typed.Span.Known() {
			typed.Span = p.nameSpan(first)
		}
	case *ast.WindowFunctionExpression:
		if typed.Function != nil && !typed.Function.Span.Known() {
			typed.Function.Span = p.nameSpan(first)
		}
	case *ast.StructFieldAccessExpression:
		if column, isColumn := typed.Struct.(*ast.ColumnRefExpression); isColumn && !column.Span.Known() {
			column.Span = p.absoluteSpan(first, p.Position)
		}
	}
	return expression
}

// absoluteSpan returns the span from the first byte of the token at first to the last
// byte of the token before end, as offsets into the text the tokens were read from.
//
// Takes first (int) which is the index of the first token.
// Takes end (int) which is one past the index of the last token.
//
// Returns ast.Span which covers the tokens, or is zero for an empty range.
func (p *parser) absoluteSpan(first, end int) ast.Span {
	return dialectkit.TokenRangeSpan(p.Tokens, first, end)
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
	return p.absoluteSpan(first, end)
}

// recordSetTargets records each column an UPDATE's SET clause assigns, among the tokens
// from start up to end, as a column reference.
//
// Takes start (int) which is the index of the first token of the target list.
// Takes end (int) which is one past the index of its last token.
func (p *parser) recordSetTargets(start, end int) {
	var targets []ast.Expression
	for index := start; index < end && index < len(p.Tokens); index++ {
		if p.Tokens[index].Kind != tokenIdentifier {
			continue
		}
		targets = append(targets, &ast.ColumnRefExpression{
			TableAlias: "",
			ColumnName: p.Tokens[index].Value,
			Span:       p.absoluteSpan(index, index+1),
		})
	}
	p.recordClauseExpressions(ast.ClauseSetTarget, targets)
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
		!p.Tokens[start+1].IsKeyword(tokenIdentifier, keywordVALUES) && !p.Tokens[start+1].IsKeyword(tokenIdentifier, keywordFROM)
	if opensList {
		p.recordClauseExpressions(ast.ClauseSetValue, p.reparse(start+1, end-1, true))
		return
	}
	p.recordClauseExpressions(ast.ClauseSetValue, p.reparse(start, end, false))
}

// recordOrderBy records the keys of an ORDER BY list among the tokens from start up to
// end, unless the list is ALL, which orders by every column and names none.
//
// Takes start (int) which is the index of the list's first token.
// Takes end (int) which is one past the index of its last token.
func (p *parser) recordOrderBy(start, end int) {
	if start < end && start < len(p.Tokens) && p.Tokens[start].IsKeyword(tokenIdentifier, keywordALL) {
		return
	}
	p.recordClauseExpressions(ast.ClauseOrderBy, p.reparse(start, end, true))
}

// recordStatementSpans fills the analysis of the top-level statement with its spans, its
// placeholders and its terminator.
//
// Takes statementParser (*parser) which is the parser that walked the statement.
// Takes analysis (*ast.RawQueryAnalysis) which receives the spans.
// Takes parsed (*parsedStatement) which is the statement.
// Takes statement (ast.ParsedStatement) which locates it.
func (engine *Dialect) recordStatementSpans(statementParser *parser, analysis *ast.RawQueryAnalysis, parsed *parsedStatement,
	statement ast.ParsedStatement) {
	spans := statementParser.spans.Finish(analysis)
	spans.Placeholders = dialectkit.PlaceholderSpans(parsed.tokens, placeholderTokenKinds, engine.info.Placeholders,
		statement.Location)
	spans.Terminator = dialectkit.TerminatorSpan(engine.info.Lexical, parsed.source, statement.Location,
		statement.Location+statement.Length)
	analysis.Spans = spans
}

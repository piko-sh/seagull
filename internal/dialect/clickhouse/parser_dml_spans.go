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
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/clickhouse/lexical"
	"seagull.sh/seagull/internal/dialect/dialectkit"
)

// singleValueAssignmentTokens is the number of tokens of an assignment whose value is one
// token, as in `column = {name:Type}`.
const singleValueAssignmentTokens = 3

var (
	// placeholderTokenKinds tells the shared placeholder locator how the ClickHouse
	// tokeniser classifies the tokens it needs.
	placeholderTokenKinds = dialectkit.PlaceholderTokens[tokenKind]{
		IsParameter:      func(candidate token) bool { return candidate.Kind == tokenClickHouseParam },
		Word:             tokenIdentifier,
		LeftParenthesis:  tokenLeftParen,
		RightParenthesis: tokenRightParen,
	}

	// valuesRowTokens tells the shared VALUES row splitter how the ClickHouse tokeniser
	// classifies the tokens that nest and separate a row.
	valuesRowTokens = dialectkit.ListTokens[tokenKind]{
		Openers: []tokenKind{tokenLeftParen, tokenLeftBracket, tokenLeftBrace},
		Closers: []tokenKind{tokenRightParen, tokenRightBracket, tokenRightBrace},
		Comma:   tokenComma,
	}
)

// offsetRange is a range of byte offsets in the text a statement was read from; the zero
// range holds nothing.
type offsetRange struct {
	// start is the offset of the first byte.
	start int

	// end is the offset one past the last byte.
	end int
}

// widened returns the range grown to cover another.
//
// Takes other (offsetRange) which is the range to cover as well.
//
// Returns offsetRange which covers both, or other when the range holds nothing.
func (span offsetRange) widened(other offsetRange) offsetRange {
	if span.end <= span.start {
		return other
	}
	return offsetRange{start: min(span.start, other.start), end: max(span.end, other.end)}
}

// armTail holds the clauses that close one SELECT arm: in ClickHouse every arm of a
// compound query has its own ORDER BY, LIMIT, SETTINGS and FORMAT, so the statement's own
// are those of the arm read last.
type armTail struct {
	// rowShaping holds the QUALIFY and LIMIT n BY clauses.
	rowShaping []offsetRange

	// orderBy covers the ORDER BY clause.
	orderBy offsetRange

	// orderByPlace is the offset one past the last byte of the token before where the arm's
	// ORDER BY clause is or would go, or zero outside the top-level statement.
	orderByPlace int

	// pagination covers the LIMIT, OFFSET and FETCH clauses.
	pagination offsetRange

	// trailing covers the SETTINGS and FORMAT clauses.
	trailing offsetRange
}

// recordSpan records the tokens from first up to end as a clause of the top-level
// statement, unless the statement's clause of that kind is recorded already.
//
// Takes field (dialectkit.SpanField) which names the clause.
// Takes first (int) which is the index of the clause's first token.
// Takes end (int) which is one past the index of its last token.
func (p *parser) recordSpan(field dialectkit.SpanField, first, end int) {
	dialectkit.RecordTokenRange(p.spans, field, p.Tokens, first, end)
}

// itemSpan returns the span of one select-list item.
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

// tailPart returns the range of the tokens from first up to end, for a clause that closes
// a SELECT arm.
//
// Takes first (int) which is the index of the clause's first token.
// Takes end (int) which is one past the index of its last token.
//
// Returns offsetRange which covers the tokens, or the zero range outside the top-level
// statement.
func (p *parser) tailPart(first, end int) offsetRange {
	if p.spans == nil {
		return offsetRange{}
	}
	start, stop, _ := dialectkit.TokenRange(p.Tokens, first, end)
	return offsetRange{start: start, end: stop}
}

// placeAfterLastToken returns the offset one past the last byte of the token before the
// cursor, the place a clause not written there would go.
//
// Returns int which is the offset, or zero outside the top-level statement or at its
// start.
func (p *parser) placeAfterLastToken() int {
	if p.spans == nil || p.Position == 0 || p.Position > len(p.Tokens) {
		return 0
	}
	return p.Tokens[p.Position-1].End
}

// finishTail records the clauses that close the arm read last as the statement's own.
func (p *parser) finishTail() {
	tail := p.tail
	for _, rowShaping := range tail.rowShaping {
		p.spans.AddRowShaping(rowShaping.start, rowShaping.end)
	}
	p.spans.Record(dialectkit.SpanOrderBy, tail.orderBy.start, tail.orderBy.end)
	p.spans.RecordOrderByPlace(tail.orderByPlace)
	p.spans.Record(dialectkit.SpanPagination, tail.pagination.start, tail.pagination.end)
	p.spans.Record(dialectkit.SpanTrailing, tail.trailing.start, tail.trailing.end)
}

// recordValuesRows records each parenthesised row of an INSERT ... VALUES list among the
// tokens from first up to end: its span and its items' spans for the top-level statement,
// and each item's expression as a clause expression.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the items' expressions.
// Takes first (int) which is the index of the list's first token.
// Takes end (int) which is one past the index of its last token.
func (p *parser) recordValuesRows(analysis *ast.RawQueryAnalysis, first, end int) {
	end = min(end, len(p.Tokens))
	depth := 0
	open := -1
	for index := first; index < end; index++ {
		switch p.Tokens[index].Kind {
		case tokenLeftParen, tokenLeftBracket, tokenLeftBrace:
			if depth == 0 && p.Tokens[index].Kind == tokenLeftParen {
				open = index
			}
			depth++
		case tokenRightParen, tokenRightBracket, tokenRightBrace:
			depth--
			if depth == 0 && open >= 0 && p.Tokens[index].Kind == tokenRightParen {
				p.spans.AddValuesRow(dialectkit.ValuesRow(p.Tokens, open, index, valuesRowTokens, p.spans))
				addClauseExpressions(analysis, ast.ClauseInsertValue, p.reparseList(open+1, index))
				open = -1
			}
		default:
		}
	}
}

// reparseExpression extracts the expression that starts the tokens already visited by a
// clause scan.
//
// An isolated parser discards syntax errors so name collection cannot change the
// statement's analysis. The isolated parser shares this parser's interrupt and nesting
// depth, and a panic in it, an interrupted walk included, is not caught here: it fails
// the statement at the entry point.
//
// Takes first (int) which is the index of the first token.
// Takes end (int) which is one past the index of the last token.
//
// Returns []ast.Expression which holds the parsed expression, or nothing when the range
// holds no tokens.
func (p *parser) reparseExpression(first, end int) []ast.Expression {
	scratch, ok := p.reparser(first, end)
	if !ok || scratch.AtEnd() {
		return nil
	}
	return []ast.Expression{scratch.parseExpression()}
}

// reparseList extracts the comma-separated expressions among tokens already visited by a
// clause scan.
//
// An isolated parser discards syntax errors so name collection cannot change the
// statement's analysis. Unsupported syntax ends the collection early. The isolated parser
// shares this parser's interrupt and nesting depth, and a panic in it, an interrupted
// walk included, is not caught here: it fails the statement at the entry point.
//
// Takes first (int) which is the index of the first token.
// Takes end (int) which is one past the index of the last token.
//
// Returns []ast.Expression which holds the parsed expressions in order.
func (p *parser) reparseList(first, end int) (expressions []ast.Expression) {
	scratch, ok := p.reparser(first, end)
	if !ok {
		return nil
	}
	for !scratch.AtEnd() {
		before := scratch.Position
		expressions = append(expressions, scratch.parseExpression())
		if scratch.Current().Kind != tokenComma || scratch.Position == before {
			break
		}
		scratch.Advance()
	}
	return expressions
}

// reparser builds the isolated parser that re-reads the tokens from first up to end.
//
// Takes first (int) which is the index of the first token.
// Takes end (int) which is one past the index of the last token.
//
// Returns *parser which reads only those tokens.
// Returns bool which is false when the range is empty or out of bounds.
func (p *parser) reparser(first, end int) (*parser, bool) {
	if first < 0 || first >= end || end > len(p.Tokens) {
		return nil, false
	}
	return p.newChildParser(p.Tokens[first:end:end]), true
}

// recordAssignments records the columns and values of the `column = value, ...`
// assignments of an UPDATE among the tokens from first up to end, stopping at an IN
// PARTITION clause.
//
// Takes analysis (*ast.RawQueryAnalysis) which owns the assignments.
// Takes first (int) which is the index of the first assignment's first token.
// Takes end (int) which is one past the index of the last assignment's last token.
// Takes table (string) which is the table the UPDATE changes.
func (p *parser) recordAssignments(analysis *ast.RawQueryAnalysis, first, end int, table string) {
	end = min(end, len(p.Tokens))
	depth := 0
	start := first
	for index := first; index < end; index++ {
		current := p.Tokens[index]
		if depth == 0 && current.IsKeyword(tokenIdentifier, "IN") && index+1 < end &&
			p.Tokens[index+1].IsKeyword(tokenIdentifier, "PARTITION") {
			end = index
			break
		}
		if depth == 0 && current.Kind == tokenComma {
			p.recordAssignment(analysis, start, index, table)
			start = index + 1
		}
		depth = nestingAfter(current, depth)
	}
	p.recordAssignment(analysis, start, end, table)
}

// recordAssignment records the column and the value of one `column = value` assignment
// among the tokens from first up to end, and a value that is a `{name:Type}` placeholder
// alone as assigned to the column.
//
// Takes analysis (*ast.RawQueryAnalysis) which owns the assignment.
// Takes first (int) which is the index of the assignment's first token.
// Takes end (int) which is one past the index of its last token.
// Takes table (string) which is the table the UPDATE changes.
func (p *parser) recordAssignment(analysis *ast.RawQueryAnalysis, first, end int, table string) {
	if first+2 > end || p.Tokens[first].Kind != tokenIdentifier || p.Tokens[first+1].Kind != tokenOperator ||
		p.Tokens[first+1].Value != "=" {
		return
	}
	if end == first+singleValueAssignmentTokens && p.Tokens[first+2].Kind == tokenClickHouseParam {
		p.assignedPlaceholders[p.Tokens[first+2].Start] = ast.ColumnReference{TableAlias: table, ColumnName: p.Tokens[first].Value}
	}
	target := &ast.ColumnRefExpression{TableAlias: "", ColumnName: p.Tokens[first].Value, Span: p.absoluteSpan(first, first+1)}
	addClauseExpressions(analysis, ast.ClauseSetTarget, []ast.Expression{target})
	addClauseExpressions(analysis, ast.ClauseSetValue, p.reparseExpression(first+2, end))
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

// recordStatementSpans fills the analysis of the top-level statement with its spans, its
// placeholders and its terminator.
//
// Takes statementParser (*parser) which is the parser that walked the statement.
// Takes analysis (*ast.RawQueryAnalysis) which receives the spans.
// Takes parsed (*parsedStatement) which is the statement.
// Takes statement (ast.ParsedStatement) which locates it.
func (*Dialect) recordStatementSpans(statementParser *parser, analysis *ast.RawQueryAnalysis, parsed *parsedStatement,
	statement ast.ParsedStatement) {
	statementParser.finishTail()
	spans := statementParser.spans.Finish(analysis)
	spans.Placeholders = dialectkit.PlaceholderSpans(parsed.tokens, placeholderTokenKinds, placeholders(), statement.Location)
	spans.Terminator = dialectkit.TerminatorSpan(lexical.Rules(), parsed.source, statement.Location,
		statement.Location+statement.Length)
	analysis.Spans = spans
}

// addClauseExpressions records the expressions a clause holds on the analysis that owns
// the clause.
//
// Takes analysis (*ast.RawQueryAnalysis) which owns the clause.
// Takes clause (ast.Clause) which is the clause.
// Takes expressions ([]ast.Expression) which are its expressions.
func addClauseExpressions(analysis *ast.RawQueryAnalysis, clause ast.Clause, expressions []ast.Expression) {
	for _, expression := range expressions {
		if expression != nil {
			analysis.ClauseExpressions = append(analysis.ClauseExpressions, ast.ClauseExpression{Expression: expression, Clause: clause})
		}
	}
}

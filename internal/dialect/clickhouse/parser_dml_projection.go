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
	"seagull.sh/seagull/internal/dialect/dialectkit"
)

// parseProjectionList reads the SELECT projection list.
//
// Each expression becomes one RawOutputColumn; `*` and `t.*` produce star-expansion
// markers. A comma may end the list where the FROM clause follows it, as in SELECT a, b,
// FROM t; any other item with nothing in it, as in SELECT , 1 or SELECT 1,, is a syntax
// error, as ClickHouse reads it.
//
// Takes analysis (*ast.RawQueryAnalysis) which is the analysis to populate.
//
// Returns error when a projection item is empty or malformed.
func (p *parser) parseProjectionList(analysis *ast.RawQueryAnalysis) error {
	start := p.Position
	var end int
	var items []ast.ItemSpan
	for {
		first := p.Position
		if p.AtEnd() || p.Current().Kind == tokenComma {
			return dialectkit.NewParseError(p.Current(), messageExpectedExpression)
		}
		p.aliasStart = -1
		column, err := p.parseProjectionColumn(analysis)
		if err != nil {
			return err
		}
		if p.Position == first {
			return dialectkit.NewParseError(p.Current(), messageExpectedExpression)
		}
		analysis.OutputColumns = append(analysis.OutputColumns, column)
		expressionEnd := p.Position
		if p.aliasStart >= 0 {
			expressionEnd = p.aliasStart
		}
		items = append(items, p.itemSpan(first, expressionEnd, p.Position, column.IsStar))
		end = p.Position
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
		if p.opensFromClause() {
			break
		}
	}
	if !p.spans.Recorded(dialectkit.SpanProjection) {
		p.recordSpan(dialectkit.SpanProjection, start, end)
		p.spans.RecordProjectionItems(items)
	}
	return nil
}

// opensFromClause reports whether the cursor, just past a comma of the SELECT list, is on
// a FROM that opens the FROM clause rather than on a column named from: a FROM followed
// by a table, a table function, a subquery or a placeholder.
//
// FROM followed by FROM, AS, a comma, an operator or the end of the statement is the
// column.
//
// Returns bool which is true when the comma before the cursor ends the list.
func (p *parser) opensFromClause() bool {
	if !p.IsKeyword(kwFrom) {
		return false
	}
	following := p.Peek()
	switch following.Kind {
	case tokenLeftParen, tokenClickHouseParam:
		return true
	case tokenIdentifier:
		return !following.IsKeyword(tokenIdentifier, kwFrom) && !following.IsKeyword(tokenIdentifier, kwAs)
	default:
		return false
	}
}

// tryProjectionExpressionTree builds an ast.Expression tree for the computed projection
// item at the cursor, so the type resolver can infer its type. Without it a function call
// such as toInt64(count()) resolves to `any` (no expression is attached).
//
// The trial restores the cursor without registering parameters, so the caller still
// drives the real placeholder-registering consume afterwards. The tree is returned only
// when the expression parser consumes exactly up to the projection boundary the consume
// stops at; a shorter or longer span (a form the grammar does not model) yields nil so
// the column falls back to an untyped projection rather than carrying a partial tree. A
// tree that stops one word short of the boundary is kept too, the word being the item's
// alias written without AS, as in count() total. An expression nested past the depth cap
// is not silently degraded: the failure is recorded as the parser's syntax error so the
// statement is reported.
//
// Returns ast.Expression which is the parsed tree, or nil when none fits.
// Returns int which is the index of the token after the expression when the item ends in
// an alias written without AS, and -1 otherwise.
func (p *parser) tryProjectionExpressionTree() (ast.Expression, int) {
	start := p.Position
	boundary := p.projectionBoundaryPosition()
	expression := p.parseExpression()
	end := p.Position
	p.Position = start
	if containsUnknownExpression(expression) {
		return nil, -1
	}
	switch {
	case end == boundary:
		return expression, -1
	case end+1 == boundary && end > start && p.Tokens[end].Kind == tokenIdentifier && !isTopClauseKeyword(p.Tokens[end]):
		return expression, end
	default:
		return nil, -1
	}
}

// projectionBoundaryPosition returns the offset where the current projection item ends,
// scanning over parentheses and stopping at a top-level comma or projection keyword. It
// registers nothing and restores the cursor, so it validates an expression-tree trial
// without disturbing parameter numbering.
//
// Returns int which is the offset of the projection boundary.
func (p *parser) projectionBoundaryPosition() int {
	start := p.Position
	depth := 0
	for !p.AtEnd() {
		current := p.Current()
		if depth == 0 && tokenIsTopLevelStop(current, projectionStopKeywords) {
			break
		}
		newDepth, halt := advanceParenDepth(current, depth)
		if halt {
			break
		}
		depth = newDepth
		p.Advance()
	}
	boundary := p.Position
	p.Position = start
	return boundary
}

// containsUnknownExpression reports whether an expression tree holds a part the parser
// could not model, so a projection whose tree is only partly understood stays untyped
// rather than typed from the part that was.
//
// Every node ast.Children knows is searched, lambda bodies, named arguments and row
// values included, but not the subqueries the tree holds.
//
// Takes expression (ast.Expression) which is the tree to inspect.
//
// Returns bool which is true when any node is an UnknownExpression.
func containsUnknownExpression(expression ast.Expression) bool {
	for node := range ast.Preorder(expression) {
		if _, unknown := node.(*ast.UnknownExpression); unknown {
			return true
		}
	}
	return false
}

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
	"fmt"
	"slices"
	"strconv"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

var (
	// expressionTerminatorKeywords lists keywords that terminate a free-form expression.
	//
	// The set covers WHERE, ON and HAVING. JOIN-introducing keywords are included so a
	// JOIN's ON expression stops at the next JOIN rather than swallowing the subsequent JOIN
	// clause, which would otherwise prevent the next joined table from being added to the
	// scope chain and leave its projected columns typed as `any`.
	expressionTerminatorKeywords = map[string]struct{}{
		keywordGROUP: {}, keywordHAVING: {}, keywordQUALIFY: {}, keywordORDER: {}, keywordLIMIT: {},
		keywordOFFSET: {}, keywordFETCH: {}, keywordFOR: {}, "WINDOW": {},
		keywordUNION: {}, keywordINTERSECT: {}, keywordEXCEPT: {},
		keywordRETURNING: {}, keywordSET: {}, keywordON: {},
		keywordFROM: {}, keywordWHERE: {}, "INTO": {},
		keywordPIVOT: {}, keywordUNPIVOT: {},
		keywordJOIN: {}, "INNER": {}, "LEFT": {}, "RIGHT": {},
		"FULL": {}, "CROSS": {}, "NATURAL": {},
	}

	// joinKeywordTerminators is the subset of expressionTerminatorKeywords whose tokens are
	// also valid SQL function names.
	//
	// Examples include LEFT() and RIGHT(). When such a token is immediately followed by an
	// opening parenthesis it is a function call rather than a JOIN starter, so the
	// terminator check skips it to avoid prematurely ending the surrounding expression and
	// silently dropping any parameters that follow.
	joinKeywordTerminators = map[string]struct{}{
		"INNER": {}, "LEFT": {}, "RIGHT": {}, "FULL": {}, "CROSS": {}, "NATURAL": {},
	}

	// whereExpressionTerminatorKeywords is the terminator set used for WHERE and HAVING
	// expressions.
	//
	// It deliberately omits the JOIN-introducing keywords (JOIN, INNER, LEFT, RIGHT, FULL,
	// CROSS, NATURAL) because those are all legal unquoted column names in DuckDB, and a
	// WHERE/HAVING predicate never legitimately starts a JOIN at top level (JOINs precede
	// WHERE in the FROM clause). Including them would treat a column named
	// `left`/`right`/`inner` and the like as a clause boundary and silently drop every
	// parameter that follows it. The ON-clause scan keeps the JOIN keywords via
	// expressionTerminatorKeywords so a join condition still stops at the next JOIN.
	whereExpressionTerminatorKeywords = map[string]struct{}{
		keywordGROUP: {}, keywordHAVING: {}, keywordQUALIFY: {}, keywordORDER: {}, keywordLIMIT: {},
		keywordOFFSET: {}, keywordFETCH: {}, keywordFOR: {}, "WINDOW": {},
		keywordUNION: {}, keywordINTERSECT: {}, keywordEXCEPT: {},
		keywordRETURNING: {}, keywordSET: {}, keywordON: {},
		keywordFROM: {}, keywordWHERE: {}, "INTO": {},
		keywordPIVOT: {}, keywordUNPIVOT: {},
	}

	// orderByTerminators lists keywords that end an ORDER BY list at depth zero.
	orderByTerminators = map[string]struct{}{
		keywordLIMIT: {}, keywordOFFSET: {}, keywordFETCH: {}, keywordFOR: {},
		keywordUNION: {}, keywordINTERSECT: {}, keywordEXCEPT: {},
		keywordRETURNING: {}, "WINDOW": {},
	}

	// limitOffsetValueTerminators lists the keywords that end a LIMIT/OFFSET/FETCH value
	// expression at the top level, so scanning a compound value (a function call or
	// parenthesised expression) stops at the keyword introducing the next clause.
	limitOffsetValueTerminators = map[string]struct{}{
		keywordLIMIT: {}, keywordOFFSET: {}, keywordFETCH: {}, keywordFOR: {},
		keywordUNION: {}, keywordINTERSECT: {}, keywordEXCEPT: {}, keywordRETURNING: {},
		keywordROW: {}, keywordROWS: {}, keywordWITH: {}, "ONLY": {}, "TIES": {},
		"NEXT": {}, "FIRST": {},
	}

	// insertSourceTerminators lists the keywords that end an INSERT source expression at
	// depth zero.
	insertSourceTerminators = map[string]struct{}{
		keywordON: {}, keywordRETURNING: {},
	}

	// onConflictTargetPredicateTerminators lists the keywords that end the optional WHERE
	// index-predicate of an ON CONFLICT target at depth zero. The predicate stops before the
	// DO action; RETURNING guards against a malformed clause that omits the action entirely.
	onConflictTargetPredicateTerminators = map[string]struct{}{
		"DO": {}, keywordRETURNING: {},
	}

	// setExpressionTerminators lists keywords that end a SET expression at depth zero.
	setExpressionTerminators = map[string]struct{}{
		keywordWHERE: {}, keywordFROM: {}, keywordRETURNING: {},
		keywordORDER: {}, keywordLIMIT: {},
	}
)

// parseWhereClause skips tokens of a WHERE expression up to the next clause boundary,
// registering parameters along the way.
func (p *parser) parseWhereClause() {
	p.parseExpressionUntilTerminator()
}

// parseExpressionUntilTerminator skips a WHERE or HAVING expression up to the next clause
// boundary, registering parameters along the way.
func (p *parser) parseExpressionUntilTerminator() {
	p.skipTokensUntilTerminatorSet(whereExpressionTerminatorKeywords)
}

// skipTokensUntilTerminatorSet advances tokens until a top-level keyword from terminators
// or an unmatched closing parenthesis.
//
// Takes terminators (map[string]struct{}) which is the upper-case keyword set that ends
// the walk.
func (p *parser) skipTokensUntilTerminatorSet(terminators map[string]struct{}) {
	p.skipTokensUntilTerminator(terminators, false)
}

// skipTokensUntilTerminator advances tokens to a stopping point.
//
// It stops on a top-level keyword from terminators, an unmatched closing parenthesis, or
// (when stopAtTopLevelComma is set) a comma at depth zero. The comma stop lets the "LIMIT
// value, offset" comma form split its two operands even when the LIMIT value is a
// compound expression whose own commas sit inside parentheses.
//
// Takes terminators (map[string]struct{}) which is the upper-case keyword set that ends
// the walk.
// Takes stopAtTopLevelComma (bool) which stops the walk on a depth-zero comma when true.
func (p *parser) skipTokensUntilTerminator(terminators map[string]struct{}, stopAtTopLevelComma bool) {
	depth := 0
	for !p.AtEnd() {
		current := p.Current()

		nextDepth, stop, handled := p.handleExpressionNesting(current, depth)
		if stop {
			break
		}
		if handled {
			depth = nextDepth
			continue
		}
		if stopsAtTopLevelComma(current, depth, stopAtTopLevelComma) {
			break
		}
		if depth == 0 && p.isKeywordTerminator(current, terminators) && !p.isDistinctFromOperand(current) {
			if p.handleExpressionTerminator(current) {
				break
			}
			continue
		}
		if isParameterToken(current.Kind) {
			p.handleParameterInExpression()
			continue
		}

		p.Advance()
	}
}

// handleExpressionNesting processes a parenthesis, bracket or brace within an expression
// scan, as handleExpressionParen does for a parenthesis.
//
// Takes candidate (token) which is the token under consideration.
// Takes depth (int) which is the current nesting depth.
//
// Returns nextDepth (int) which is the updated nesting depth.
// Returns stop (bool) which is true when the outer loop should break.
// Returns handled (bool) which is true when the token was consumed here.
func (p *parser) handleExpressionNesting(candidate token, depth int) (nextDepth int, stop bool, handled bool) {
	if nextDepth, stop, handled = p.handleExpressionParen(candidate, depth); handled {
		return nextDepth, stop, handled
	}
	if nextDepth, handled = bracketDepth(candidate, depth); handled {
		p.Advance()
	}
	return nextDepth, false, handled
}

// isDistinctFromOperand reports whether candidate is the FROM of an IS [NOT] DISTINCT
// FROM comparison, which belongs to the predicate rather than starting a FROM clause.
//
// Takes candidate (token) which is the candidate terminator.
//
// Returns bool which is true when the FROM follows DISTINCT.
func (p *parser) isDistinctFromOperand(candidate token) bool {
	return candidate.IsKeyword(tokenIdentifier, keywordFROM) && p.PeekAt(-1).IsKeyword(tokenIdentifier, "DISTINCT")
}

// handleExpressionParen processes a parenthesis token within an expression scan. Returns
// the next depth value, whether to break the outer loop (unmatched right paren at depth
// zero), and whether the token was a parenthesis at all (and thus already consumed).
//
// Takes candidate (token) which is the token under consideration.
// Takes depth (int) which is the current parenthesis nesting depth.
//
// Returns nextDepth (int) which is the updated nesting depth.
// Returns stop (bool) which is true when the outer loop should break.
// Returns handled (bool) which is true when the token was consumed here.
func (p *parser) handleExpressionParen(candidate token, depth int) (nextDepth int, stop bool, handled bool) {
	if candidate.Kind == tokenLeftParen {
		if p.isSubqueryStart() {
			if innerAnalysis, analyseError := p.analyseSubqueryBody(); analyseError == nil {
				p.predicateSubqueries = append(p.predicateSubqueries, innerAnalysis)
			}
			return depth, false, true
		}
		p.Advance()
		return depth + 1, false, true
	}
	if candidate.Kind == tokenRightParen {
		if depth == 0 {
			return depth, true, true
		}
		p.Advance()
		return depth - 1, false, true
	}
	return depth, false, false
}

// handleExpressionTerminator distinguishes a real JOIN keyword from a function call
// sharing the same name (LEFT(), RIGHT(), etc.). Consumes the identifier when it is
// followed by an opening parenthesis, returning false so the caller continues; otherwise
// returns true so the caller breaks out of the expression scan.
//
// Takes candidate (token) which is the candidate terminator token.
//
// Returns bool which is true when the caller should break.
func (p *parser) handleExpressionTerminator(candidate token) bool {
	if _, isJoinKeyword := joinKeywordTerminators[candidate.Keyword(tokenIdentifier)]; isJoinKeyword && p.Peek().Kind == tokenLeftParen {
		p.Advance()
		return false
	}
	return true
}

// isKeywordTerminator reports whether candidate is an identifier whose upper-case value
// is in the terminator set.
//
// Takes candidate (token) which is the token under consideration.
// Takes terminators (map[string]struct{}) which is the upper-case keyword set to test.
//
// Returns bool which is true when the token is a terminator keyword.
func (*parser) isKeywordTerminator(candidate token, terminators map[string]struct{}) bool {
	if candidate.Kind != tokenIdentifier {
		return false
	}
	_, ok := terminators[candidate.Keyword(tokenIdentifier)]
	return ok
}

// parseGroupByClause parses the GROUP BY list at the current position: ALL, which groups
// by every column the projection does not aggregate, or an optional DISTINCT then
// grouping elements, each an expression, a parenthesised list, ROLLUP (...), CUBE (...)
// or GROUPING SETS (...).
//
// Takes outputs ([]ast.RawOutputColumn) which is the select list, whose items a key in a
// grouping modifier may name by position.
//
// Returns []ast.ColumnReference which lists the elements that are plain column
// references.
// Returns *ast.GroupingSets which holds the keys a grouping modifier leaves out of some
// rows, or nil when the list has no modifier.
func (p *parser) parseGroupByClause(outputs []ast.RawOutputColumn) ([]ast.ColumnReference, *ast.GroupingSets) {
	if p.MatchKeyword(keywordALL) {
		return nil, nil
	}
	p.MatchKeyword("DISTINCT")

	var columns []ast.ColumnReference
	var sets *ast.GroupingSets
	for {
		if p.isGroupingModifier() {
			if sets == nil {
				sets = &ast.GroupingSets{Keys: nil, FillsDefaults: false}
			}
			sets.Keys = append(sets.Keys, p.parseGroupingModifier(outputs, 0)...)
		} else if column, ok := p.parseGroupingElement(); ok {
			columns = append(columns, column)
		}

		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}

	return columns, sets
}

// isGroupingModifier reports whether the current token starts ROLLUP (...), CUBE (...) or
// GROUPING SETS (...).
//
// Returns bool which is true at a grouping modifier.
func (p *parser) isGroupingModifier() bool {
	switch {
	case p.IsKeyword("ROLLUP", "CUBE"):
		return p.Peek().Kind == tokenLeftParen
	case p.IsKeyword("GROUPING"):
		return p.Peek().IsKeyword(tokenIdentifier, "SETS")
	default:
		return false
	}
}

// parseGroupingModifier parses ROLLUP (...), CUBE (...) or GROUPING SETS (...), whose
// sets, nested modifiers included, each leave out of some rows every key they hold. Every
// key is recorded as a GROUP BY expression.
//
// Takes outputs ([]ast.RawOutputColumn) which is the select list a key may name by
// position.
// Takes depth (int) which bounds the nesting of GROUPING SETS.
//
// Returns []ast.Expression which holds the keys, a key naming a select item by position
// replaced by the item's expression.
func (p *parser) parseGroupingModifier(outputs []ast.RawOutputColumn, depth int) []ast.Expression {
	if p.IsKeyword("GROUPING") {
		p.Advance()
	}
	p.Advance()
	if p.Current().Kind != tokenLeftParen {
		return nil
	}
	p.Advance()
	var keys []ast.Expression
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		keys = append(keys, p.parseGroupingSet(outputs, depth)...)
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}
	return keys
}

// parseGroupingSet parses one element of a grouping modifier's list: a nested modifier, a
// parenthesised list of keys, possibly empty, or a single key.
//
// Takes outputs ([]ast.RawOutputColumn) which is the select list a key may name by
// position.
// Takes depth (int) which bounds the nesting of GROUPING SETS.
//
// Returns []ast.Expression which holds the element's keys.
func (p *parser) parseGroupingSet(outputs []ast.RawOutputColumn, depth int) []ast.Expression {
	switch {
	case p.isGroupingModifier() && depth < p.maxParseDepth:
		return p.parseGroupingModifier(outputs, depth+1)
	case p.Current().Kind == tokenLeftParen && !p.isSubqueryStart():
		p.Advance()
		var keys []ast.Expression
		for !p.AtEnd() && p.Current().Kind != tokenRightParen {
			keys = append(keys, p.parseGroupingKey(outputs))
			if p.Current().Kind != tokenComma {
				break
			}
			p.Advance()
		}
		if p.Current().Kind == tokenRightParen {
			p.Advance()
		}
		return keys
	default:
		return []ast.Expression{p.parseGroupingKey(outputs)}
	}
}

// parseGroupingKey parses one key of a grouping modifier and records it as a GROUP BY
// expression. A key that is an integer alone names the select item at that position, and
// stands for the item's expression.
//
// Takes outputs ([]ast.RawOutputColumn) which is the select list.
//
// Returns ast.Expression which is the key.
func (p *parser) parseGroupingKey(outputs []ast.RawOutputColumn) ast.Expression {
	position, isPosition := p.selectItemPosition(len(outputs))
	expression := p.parseExpression()
	p.recordClauseExpressions(ast.ClauseGroupBy, []ast.Expression{expression})
	if isPosition && outputs[position-1].Expression != nil {
		return outputs[position-1].Expression
	}
	return expression
}

// selectItemPosition reports whether the current token is an integer alone, followed by a
// comma or a closing parenthesis, that names a select item by its position.
//
// Takes items (int) which is the number of select items.
//
// Returns int which is the position, counted from one.
// Returns bool which is true when the token names an item.
func (p *parser) selectItemPosition(items int) (int, bool) {
	if p.Current().Kind != tokenNumber || (p.Peek().Kind != tokenComma && p.Peek().Kind != tokenRightParen) {
		return 0, false
	}
	position, err := strconv.Atoi(p.Current().Value)
	if err != nil || position < 1 || position > items {
		return 0, false
	}
	return position, true
}

// parseGroupingElement parses one GROUP BY element other than a grouping modifier.
//
// Returns ast.ColumnReference which is the element when it is a plain column.
// Returns bool which is true when the element was a plain column reference.
func (p *parser) parseGroupingElement() (ast.ColumnReference, bool) {
	if p.Current().Kind == tokenLeftParen && !p.isSubqueryStart() {
		p.recordClauseExpressions(ast.ClauseGroupBy, p.parseParenthesisedExpressionList())
		return ast.ColumnReference{}, false
	}
	expression := p.parseExpression()
	p.recordClauseExpressions(ast.ClauseGroupBy, []ast.Expression{expression})
	column, ok := expression.(*ast.ColumnRefExpression)
	if !ok {
		return ast.ColumnReference{}, false
	}
	return ast.ColumnReference{TableAlias: column.TableAlias, ColumnName: column.ColumnName}, true
}

// parseOrderByList skips ORDER BY expressions up to the next clause boundary.
func (p *parser) parseOrderByList() {
	p.skipTokensUntilTerminatorSet(orderByTerminators)
}

// consumeParameterOrAdvance parses a LIMIT or OFFSET value and registers its parameters.
//
// Bare placeholders retain the supplied context. Other values must form a valid
// expression.
//
// Takes context (sqlmodel.ParameterContext) which tags a bare-placeholder value.
func (p *parser) consumeParameterOrAdvance(context sqlmodel.ParameterContext) {
	if isParameterToken(p.Current().Kind) && p.Peek().Kind != tokenOperator && p.Peek().Kind != tokenStar &&
		p.Peek().Kind != tokenCast {
		parameterToken := p.Current()
		p.Advance()
		p.registerParameterFromToken(parameterToken, context, nil, nil)
		return
	}
	referenceCountBefore := len(p.parameterReferences)
	p.parseLimitExpression()
	p.retagLimitOffsetParameters(referenceCountBefore, context)
}

// consumeLimitValueOrAdvance consumes the LIMIT value, which in the "LIMIT value, offset"
// comma form ends at the top-level comma.
func (p *parser) consumeLimitValueOrAdvance() {
	p.consumeParameterOrAdvance(sqlmodel.ParameterContextLimit)
}

// parseLimitExpression parses a LIMIT or OFFSET value: a number followed by % for a
// percentage of the rows, or an expression.
func (p *parser) parseLimitExpression() {
	if p.Current().Kind == tokenNumber && p.Peek().Kind == tokenOperator && p.Peek().Value == "%" {
		p.Advance()
		p.Advance()
		return
	}
	p.parseExpression()
}

// retagLimitOffsetParameters re-tags every parameter registered while scanning a LIMIT or
// OFFSET value with the limit/offset context.
//
// The placeholder in LIMIT COALESCE($n, 100) is one example. Re-tagging overrides any
// incidental function-argument context the inner scan assigned. A placeholder in a
// LIMIT/OFFSET value is an integer row count, so it types as an integer rather than the
// polymorphic COALESCE argument type.
//
// Takes referenceCountBefore (int), the parameterReferences length before the value scan.
// Takes context (sqlmodel.ParameterContext), the limit or offset context to apply.
func (p *parser) retagLimitOffsetParameters(referenceCountBefore int, context sqlmodel.ParameterContext) {
	for index := referenceCountBefore; index < len(p.parameterReferences); index++ {
		p.parameterReferences[index].Context = context
		p.parameterReferences[index].EnclosingFunctionName = ""
		p.parameterReferences[index].ArgumentOrdinal = 0
	}
}

// parseLimitOffset dispatches over the LIMIT, OFFSET, and FETCH clause shapes accepted by
// DuckDB.
func (p *parser) parseLimitOffset() {
	p.parseLeadingOffset()

	if p.MatchKeyword(keywordLIMIT) {
		p.parseLimitClause()
		return
	}

	p.parseFetchClause()
	p.parseTrailingOffset()
}

// parseLeadingOffset consumes an OFFSET clause that appears before any LIMIT or FETCH
// clause.
func (p *parser) parseLeadingOffset() {
	if !p.IsKeyword(keywordOFFSET) {
		return
	}
	p.Advance()
	p.consumeParameterOrAdvance(sqlmodel.ParameterContextOffset)
	p.MatchKeyword(keywordROW)
	p.MatchKeyword(keywordROWS)
}

// parseLimitClause consumes a LIMIT expression and any trailing OFFSET or "limit, offset"
// comma form.
func (p *parser) parseLimitClause() {
	if !p.MatchKeyword(keywordALL) {
		p.consumeLimitValueOrAdvance()
	}

	if p.MatchKeyword(keywordOFFSET) {
		p.consumeParameterOrAdvance(sqlmodel.ParameterContextOffset)
		p.MatchKeyword(keywordROW)
		p.MatchKeyword(keywordROWS)
	} else if p.Current().Kind == tokenComma {
		p.Advance()
		p.consumeParameterOrAdvance(sqlmodel.ParameterContextOffset)
	}
}

// parseFetchClause consumes a FETCH FIRST/NEXT N ROWS [ONLY|WITH TIES] clause when one is
// present.
func (p *parser) parseFetchClause() {
	if !p.MatchKeyword(keywordFETCH) {
		return
	}
	p.MatchKeyword(keywordFIRST)
	p.MatchKeyword("NEXT")

	if isParameterToken(p.Current().Kind) {
		parameterToken := p.Current()
		p.Advance()
		p.registerParameterFromToken(parameterToken, sqlmodel.ParameterContextLimit, nil, nil)
	} else if p.Current().Kind == tokenNumber {
		p.Advance()
	} else {
		p.skipTokensUntilTerminatorSet(limitOffsetValueTerminators)
	}

	p.MatchKeyword(keywordROW)
	p.MatchKeyword(keywordROWS)
	p.MatchKeyword("ONLY")
	p.MatchKeyword(keywordWITH)
	p.MatchKeyword("TIES")
}

// parseTrailingOffset consumes an OFFSET clause that follows a FETCH clause, unless a
// LIMIT is about to start instead.
func (p *parser) parseTrailingOffset() {
	if p.IsKeyword(keywordLIMIT) || !p.MatchKeyword(keywordOFFSET) {
		return
	}
	p.consumeParameterOrAdvance(sqlmodel.ParameterContextOffset)
	p.MatchKeyword(keywordROW)
	p.MatchKeyword(keywordROWS)
}

// parseCompoundQuery consumes an optional UNION, INTERSECT, or EXCEPT operator, honouring
// the ALL modifier.
//
// Returns ast.CompoundOperator which is the operator that was consumed, or zero when none
// was present.
func (p *parser) parseCompoundQuery() ast.CompoundOperator {
	if p.MatchKeyword(keywordUNION) {
		if p.MatchKeyword(keywordALL) {
			return ast.CompoundUnionAll
		}
		return ast.CompoundUnion
	}
	if p.MatchKeyword(keywordINTERSECT) {
		p.MatchKeyword(keywordALL)
		return ast.CompoundIntersect
	}
	if p.MatchKeyword(keywordEXCEPT) {
		p.MatchKeyword(keywordALL)
		return ast.CompoundExcept
	}
	return 0
}

// rejectLockingClause records a syntax error for a FOR UPDATE, FOR SHARE or other
// row-locking clause, which DuckDB rejects: it has no row locks.
func (p *parser) rejectLockingClause() {
	if p.IsKeyword(keywordFOR) {
		p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "a SELECT locking clause is not supported by DuckDB"))
	}
}

// parseValuesClause walks a VALUES (...), (...), ... source attached to an INSERT,
// binding parameters to their target columns.
//
// Takes tableName (string) which is the INSERT target table.
// Takes columnNames ([]string) which is the explicit column list, or nil when none was
// provided.
func (p *parser) parseValuesClause(tableName string, columnNames []string) {
	for p.Current().Kind == tokenLeftParen {
		open := p.Position
		p.Advance()
		p.parseValuesRow(tableName, columnNames)

		if p.Current().Kind == tokenRightParen {
			p.recordValuesRow(open, p.Position)
			p.Advance()
		}
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
}

// columnRefForIndex builds the column reference associated with the index-th positional
// value in a VALUES row.
//
// Takes tableName (string) which is the target table for the reference.
// Takes columnNames ([]string) which is the declared column list.
// Takes index (int) which is the position within the row.
//
// Returns *ast.ColumnReference which is the column reference, or nil when index is past
// the declared list.
func (*parser) columnRefForIndex(tableName string, columnNames []string, index int) *ast.ColumnReference {
	if index >= len(columnNames) {
		return nil
	}
	return &ast.ColumnReference{
		TableAlias: tableName,
		ColumnName: columnNames[index],
	}
}

// parseValuesRow walks one parenthesised VALUES row's elements. An element that stops on
// a token it cannot take skips that token, so the row always ends.
//
// Takes tableName (string) which is the INSERT target table.
// Takes columnNames ([]string) which is the declared column list.
func (p *parser) parseValuesRow(tableName string, columnNames []string) {
	columnIndex := 0
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		start := p.Position
		p.parseValuesRowElement(tableName, columnNames, columnIndex)

		if p.Current().Kind == tokenComma {
			p.Advance()
			columnIndex++
		}
		if p.Position == start {
			p.Advance()
		}
	}
}

// parseValuesRowElement handles one element of a VALUES row, binding a parameter to its
// target column when present, and stepping over a parenthesised, bracketed or braced
// expression, such as [1, 2] or {'a': 1, 'b': 2}.a, whose commas separate no values of
// the row.
//
// Takes tableName (string) which is the INSERT target table.
// Takes columnNames ([]string) which is the declared column list.
// Takes columnIndex (int) which is the element's position in the row.
func (p *parser) parseValuesRowElement(tableName string, columnNames []string, columnIndex int) {
	if isParameterToken(p.Current().Kind) {
		parameterToken := p.Current()
		columnReference := p.columnRefForIndex(tableName, columnNames, columnIndex)
		p.Advance()
		var castType *sqlmodel.SQLType
		if p.Current().Kind == tokenCast {
			castType = p.consumeInlineCast()
		}
		p.registerParameterFromToken(parameterToken, sqlmodel.ParameterContextAssignment, columnReference, castType)
		return
	}
	if p.isAssignedSubqueryStart() {
		p.captureSetSubquery()
		return
	}
	if slices.Contains(valuesRowTokens.Openers, p.Current().Kind) {
		columnReference := p.columnRefForIndex(tableName, columnNames, columnIndex)
		p.scanInsertValueExpression(columnReference)
		return
	}
	if p.Current().Kind != tokenComma {
		p.Advance()
	}
}

// scanInsertValueExpression scans a parenthesised, bracketed or braced INSERT value,
// registering parameters with columnReference and any explicit cast type. It analyses
// nested subqueries for parameter types and stops after the matching closing token.
//
// Takes columnReference (*ast.ColumnReference) which is the INSERT column this VALUES
// position binds to.
func (p *parser) scanInsertValueExpression(columnReference *ast.ColumnReference) {
	if !slices.Contains(valuesRowTokens.Openers, p.Current().Kind) {
		return
	}
	p.Advance()
	depth := 1
	for !p.AtEnd() && depth > 0 {
		current := p.Current()
		switch {
		case current.Kind == tokenLeftParen && p.isAssignedSubqueryStart():
			p.captureSetSubquery()
		case slices.Contains(valuesRowTokens.Openers, current.Kind):
			depth++
			p.Advance()
		case slices.Contains(valuesRowTokens.Closers, current.Kind):
			depth--
			p.Advance()
		default:
			p.handleInsertValueDefaultToken(current, columnReference)
		}
	}
}

// handleInsertValueDefaultToken processes a non-parenthesis token encountered while
// scanning an INSERT VALUES expression. Parameter tokens register the supplied
// columnReference and pick up any inline cast that follows ($N::type); other tokens are
// skipped.
//
// Takes candidate (token) which is the token under consideration.
// Takes columnReference (*ast.ColumnReference) which is the column the value binds to.
func (p *parser) handleInsertValueDefaultToken(candidate token, columnReference *ast.ColumnReference) {
	if !isParameterToken(candidate.Kind) {
		p.Advance()
		return
	}
	parameterToken := candidate
	castType := p.castTypeAround(p.Position)
	p.Advance()
	if p.Current().Kind == tokenCast {
		castType = p.consumeInlineCast()
	}
	p.registerParameterFromToken(parameterToken, sqlmodel.ParameterContextAssignment, columnReference, castType)
}

// parseValuesRows parses a VALUES query, naming its columns col0, col1 and so on from the
// first row. It parses every row and registers all parameters.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the output columns.
//
// Returns error when a row is not enclosed in parentheses.
func (p *parser) parseValuesRows(analysis *ast.RawQueryAnalysis) error {
	if _, err := p.ExpectKeyword(keywordVALUES); err != nil {
		return err
	}
	for first := true; ; first = false {
		if p.Current().Kind != tokenLeftParen {
			return dialectkit.NewParseError(p.Current(), "expected '(' to open a VALUES row")
		}
		p.Advance()
		columns := p.parseValuesListRow()
		if first {
			analysis.OutputColumns = columns
		}
		if p.Current().Kind != tokenRightParen {
			return dialectkit.NewParseError(p.Current(), "expected ')' to close the VALUES row")
		}
		p.Advance()
		if p.Current().Kind != tokenComma {
			return nil
		}
		p.Advance()
	}
}

// parseValuesListRow parses the expressions of one row of a VALUES list, deriving one
// synthetic output column per element.
//
// Returns []ast.RawOutputColumn which holds the columns named col0, col1, and so on, in
// element order.
func (p *parser) parseValuesListRow() []ast.RawOutputColumn {
	var outputColumns []ast.RawOutputColumn

	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		expression := p.parseExpression()
		outputColumns = append(outputColumns, ast.RawOutputColumn{
			Name:       fmt.Sprintf("col%d", len(outputColumns)),
			Expression: expression,
			TableAlias: "",
			ColumnName: "",
			IsStar:     false,
		})
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}

	return outputColumns
}

// parseInsertSource skips the SELECT or query expression supplying an INSERT, stopping at
// ON CONFLICT or RETURNING.
func (p *parser) parseInsertSource() {
	p.skipTokensUntilTerminatorSet(insertSourceTerminators)
}

// parseOnConflict consumes an ON CONFLICT clause and analyses any DO UPDATE body's SET
// and WHERE expressions.
//
// Takes tableName (string) which is the target table for assignment column references.
//
// Returns ast.ConflictAction which is DoNothing for DO NOTHING, DoUpdate for DO UPDATE,
// and None when the clause gives neither.
// Returns error when the conflict target list is unbalanced.
func (p *parser) parseOnConflict(tableName string) (ast.ConflictAction, error) {
	p.MatchKeyword("CONFLICT")

	if p.Current().Kind == tokenLeftParen {
		if err := p.skipParenthesised(); err != nil {
			return ast.ConflictActionNone, err
		}
		p.skipConflictTargetPredicate()
	}

	if p.MatchKeyword(keywordON) {
		p.MatchKeyword(keywordCONSTRAINT)
		if p.Current().Kind == tokenIdentifier {
			p.Advance()
		}
	}

	if !p.MatchKeyword("DO") {
		return ast.ConflictActionNone, nil
	}
	if p.MatchKeyword("NOTHING") {
		return ast.ConflictActionDoNothing, nil
	}
	if !p.MatchKeyword("UPDATE") {
		return ast.ConflictActionNone, nil
	}
	if p.MatchKeyword(keywordSET) {
		p.parseSetClause(tableName)
	}
	if p.MatchKeyword(keywordWHERE) {
		p.parseWhereClause()
	}
	return ast.ConflictActionDoUpdate, nil
}

// skipConflictTargetPredicate consumes the optional WHERE index_predicate that follows a
// parenthesised ON CONFLICT target, used for partial-unique-index inference. The
// predicate ends at the DO action, leaving the conflict action and any trailing RETURNING
// clause for the caller to parse.
func (p *parser) skipConflictTargetPredicate() {
	if !p.MatchKeyword(keywordWHERE) {
		return
	}
	p.skipTokensUntilTerminatorSet(onConflictTargetPredicateTerminators)
}

// parseSetClause walks an UPDATE SET list, dispatching to single- or multi-column
// assignment parsers.
//
// Takes tableName (string) which is the target table for assignment column references.
func (p *parser) parseSetClause(tableName string) {
	for {
		if p.Current().Kind == tokenLeftParen {
			p.parseMultiColumnSetClause(tableName)
		} else {
			p.parseSingleColumnSetClause(tableName)
		}

		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
}

// parseSingleColumnSetClause parses one "column = expression" assignment, binding
// parameter targets to the column when possible.
//
// Takes tableName (string) which is the target table.
func (p *parser) parseSingleColumnSetClause(tableName string) {
	columnName := ""
	if p.Current().Kind == tokenIdentifier {
		p.recordSetTargets(p.Position, p.Position+1)
		columnName = p.Advance().Value
	}
	if p.Current().Kind == tokenDot {
		p.recordSyntaxError(dialectkit.NewParseError(p.Current(),
			"qualified column names in UPDATE .. SET are not supported by DuckDB: name the column alone"))
	}

	if p.Current().Kind == tokenOperator && p.Current().Value == "=" {
		p.Advance()
	}
	valueStart := p.Position
	defer func() { p.recordSetValues(valueStart, p.Position) }()

	var columnReference *ast.ColumnReference
	if columnName != "" {
		columnReference = &ast.ColumnReference{
			TableAlias: tableName,
			ColumnName: columnName,
		}
	}

	if isParameterToken(p.Current().Kind) {
		parameterToken := p.Current()
		p.Advance()
		var castType *sqlmodel.SQLType
		if p.Current().Kind == tokenCast {
			castType = p.consumeInlineCast()
		}
		p.registerParameterFromToken(parameterToken, sqlmodel.ParameterContextAssignment, columnReference, castType)
	}

	p.skipSetExpressionWithColumn(columnReference)
}

// parseMultiColumnSetClause parses a "(c1, c2, ...) = (v1, v2, ...)" SET assignment.
//
// Takes tableName (string) which is the target table.
func (p *parser) parseMultiColumnSetClause(tableName string) {
	listStart := p.Position
	columnNames, _ := p.parseColumnList()
	p.recordSetTargets(listStart, p.Position)

	if p.Current().Kind == tokenOperator && p.Current().Value == "=" {
		p.Advance()
	}
	valueStart := p.Position
	defer func() { p.recordSetValues(valueStart, p.Position) }()

	if p.Current().Kind != tokenLeftParen {
		p.skipSetExpression()
		return
	}
	if p.isSubqueryStart() {
		p.captureSetSubquery()
		return
	}

	p.Advance()
	p.parseMultiColumnSetValues(tableName, columnNames)
	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}
}

// isAssignedSubqueryStart reports whether the cursor sits on a "(" that opens a subquery
// straight away, as a value a VALUES row assigns can. A parenthesis around further
// parentheses, as in COALESCE((SELECT 1), 0), opens an expression whose subqueries are
// analysed one at a time.
//
// Returns bool which is true when the parenthesis opens the subquery itself.
func (p *parser) isAssignedSubqueryStart() bool {
	return p.isSubqueryStart() && p.Peek().Kind != tokenLeftParen
}

// captureSetSubquery analyses and consumes a subquery a statement assigns: one used in a
// SET assignment or as a value of a VALUES row.
//
// The subquery resolves its own relations within the statement's surrounding scope, and
// its parameters take their types from it rather than from the column assigned.
func (p *parser) captureSetSubquery() {
	if innerAnalysis, analyseError := p.analyseSubqueryBody(); analyseError == nil {
		p.predicateSubqueries = append(p.predicateSubqueries, innerAnalysis)
	}
}

// parseMultiColumnSetValues walks the value list inside a multi-column SET assignment,
// binding parameter targets by position. A value that stops on a token it cannot take,
// such as a stray FROM, skips that token, so the list always ends.
//
// Takes tableName (string) which is the target table.
// Takes columnNames ([]string) which is the LHS column list.
func (p *parser) parseMultiColumnSetValues(tableName string, columnNames []string) {
	columnIndex := 0
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		start := p.Position
		if isParameterToken(p.Current().Kind) {
			parameterToken := p.Current()
			columnReference := p.columnRefForIndex(tableName, columnNames, columnIndex)
			p.Advance()
			p.registerParameterFromToken(parameterToken, sqlmodel.ParameterContextAssignment, columnReference, nil)
		} else {
			p.skipSetExpression()
		}
		if p.Current().Kind == tokenComma {
			p.Advance()
			columnIndex++
		}
		if p.Position == start {
			p.Advance()
		}
	}
}

// skipSetExpression advances past a single SET assignment expression up to the next comma
// or terminator keyword.
func (p *parser) skipSetExpression() {
	p.skipSetExpressionWithColumn(nil)
}

// skipSetExpressionWithColumn scans one SET value to a top-level comma or clause
// boundary, analysing nested subqueries. Comparison parameters use the compared column;
// other parameters use the assignment target, unless an inline cast supplies their type.
//
// Takes columnReference (*ast.ColumnReference) which the parameters should be bound to as
// assignment values. nil leaves them with ParameterContextUnknown.
func (p *parser) skipSetExpressionWithColumn(columnReference *ast.ColumnReference) {
	tableAlias := ""
	if columnReference != nil {
		tableAlias = columnReference.TableAlias
	}
	depth := 0
	var comparisonColumn *ast.ColumnReference
	lastIdentifier := ""
	for !p.AtEnd() {
		current := p.Current()

		if current.Kind == tokenLeftParen && p.isSubqueryStart() {
			p.captureSetSubquery()
			continue
		}
		nextDepth, stop, handled := p.handleExpressionNesting(current, depth)
		if stop {
			break
		}
		if handled {
			depth = nextDepth
			continue
		}

		if depth == 0 && p.isSetExpressionTerminator(current) {
			break
		}
		if isParameterToken(current.Kind) {
			p.registerSetExpressionParameter(current, columnReference, comparisonColumn)
			comparisonColumn = nil
			lastIdentifier = ""
			continue
		}

		lastIdentifier, comparisonColumn = comparisonColumnForSetParameter(current, tableAlias, lastIdentifier)
		p.Advance()
	}
}

// registerSetExpressionParameter consumes a parameter token within a SET assignment RHS
// and records it with the column reference and context that match its position.
//
// When comparisonColumn is non-nil the parameter is an operand of a comparison (for
// example `WHEN attempt >= $1`), so it is recorded against that column with comparison
// context. Otherwise it contributes to the assigned value and is recorded against the
// SET-target columnReference as assignment context (or unknown context when no target is
// known). In either case an inline cast ($N::type) that follows is honoured. A compared
// name that a lambda around the parameter declares is the lambda's parameter, not a
// column, so the parameter is then recorded as compared with no column; and a parameter
// in a lambda's body stands for a value computed per element, not for the assigned value,
// so it is recorded against no column.
//
// Takes parameterToken (token) which is the placeholder token to record.
// Takes columnReference (*ast.ColumnReference) which links the parameter to its
// SET-target column when known.
// Takes comparisonColumn (*ast.ColumnReference) which is the column the parameter is
// compared against, or nil when not a comparison operand.
func (p *parser) registerSetExpressionParameter(
	parameterToken token,
	columnReference *ast.ColumnReference,
	comparisonColumn *ast.ColumnReference,
) {
	position := p.Position
	castType := p.castTypeAround(position)
	p.Advance()
	if p.Current().Kind == tokenCast {
		castType = p.consumeInlineCast()
	}
	if comparisonColumn != nil {
		if p.lambdaBindsReference(position, &ast.ColumnReference{TableAlias: "", ColumnName: comparisonColumn.ColumnName}) {
			comparisonColumn = nil
		}
		p.registerParameterFromToken(parameterToken, sqlmodel.ParameterContextComparison, comparisonColumn, castType)
		return
	}
	context := sqlmodel.ParameterContextUnknown
	if columnReference != nil && !p.insideLambdaBody(position) {
		context = sqlmodel.ParameterContextAssignment
	} else {
		columnReference = nil
	}
	p.registerParameterFromToken(parameterToken, context, columnReference, castType)
}

// isSetExpressionTerminator reports whether candidate ends one SET assignment expression.
//
// Takes candidate (token) which is the candidate token.
//
// Returns bool which is true at a comma or one of the terminator keywords.
func (*parser) isSetExpressionTerminator(candidate token) bool {
	if candidate.Kind == tokenComma {
		return true
	}
	if candidate.Kind == tokenIdentifier {
		_, ok := setExpressionTerminators[candidate.Keyword(tokenIdentifier)]
		return ok
	}
	return false
}

// bracketDepth tracks the brackets and braces of a list or struct literal, so a keyword
// inside one, such as the FOR of a list comprehension, never ends the expression.
//
// Takes candidate (token) which is the token under consideration.
// Takes depth (int) which is the current nesting depth.
//
// Returns int which is the depth after the token.
// Returns bool which is true when the token opens or closes a bracket or brace.
func bracketDepth(candidate token, depth int) (int, bool) {
	switch candidate.Kind {
	case tokenLeftBracket, tokenLeftBrace:
		return depth + 1, true
	case tokenRightBracket, tokenRightBrace:
		if depth > 0 {
			return depth - 1, true
		}
	default:
	}
	return depth, false
}

// stopsAtTopLevelComma reports whether the walk should stop on the given token: a
// depth-zero comma when the comma-stop mode is enabled. It isolates the comma-form split
// condition so the main scan loop stays flat.
//
// Takes candidate (token) which is the token under consideration.
// Takes depth (int) which is the current parenthesis nesting depth.
// Takes enabled (bool) which is true when the comma stop is active.
//
// Returns bool which is true when the walk should break on this token.
func stopsAtTopLevelComma(candidate token, depth int, enabled bool) bool {
	return enabled && depth == 0 && candidate.Kind == tokenComma
}

// comparisonColumnForSetParameter advances the comparison scan over a SET expression.
//
// When the previous identifier is immediately followed by a comparison operator, that
// column is reported so a following parameter is typed from it; for example the parameter
// in "WHEN attempt >= $1" is typed from attempt. Any other token clears the pending
// state.
//
// Takes candidate (token) which is the token being observed.
// Takes tableAlias (string) which qualifies the reported column reference.
// Takes lastIdentifier (string) which is the identifier from the previous step.
//
// Returns nextIdentifier (string) carried into the next step.
// Returns comparisonColumn (*ast.ColumnReference) which is the compared column, or nil
// when no identifier is followed by a comparison operator.
func comparisonColumnForSetParameter(
	candidate token,
	tableAlias, lastIdentifier string,
) (nextIdentifier string, comparisonColumn *ast.ColumnReference) {
	switch {
	case candidate.Kind == tokenIdentifier:
		return candidate.Value, nil
	case candidate.Kind == tokenOperator && isComparisonOperator(candidate.Value) && lastIdentifier != "":
		return "", &ast.ColumnReference{TableAlias: tableAlias, ColumnName: lastIdentifier}
	default:
		return "", nil
	}
}

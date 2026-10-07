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
	"fmt"
	"slices"
	"strconv"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// expressionScanContinue restarts the loop because the helper already advanced the
	// cursor (e.g. consumed `(`, parameter, or a join keyword that was actually a function
	// call).
	expressionScanContinue expressionScanAction = iota

	// expressionScanBreak stops the scan because a terminator was hit or the depth went
	// negative.
	expressionScanBreak

	// expressionScanAdvance steps over an inert token (an expression-interior identifier,
	// literal, operator, etc.).
	expressionScanAdvance
)

var (
	// expressionTerminatorKeywords lists keywords that terminate a free-form SQL expression
	// such as WHERE, ON, and HAVING.
	//
	// JOIN-introducing keywords are included so that a JOIN's ON expression stops at the
	// next JOIN rather than swallowing the subsequent JOIN clause (which would prevent the
	// next joined table from being added to the scope chain and leave its projected columns
	// typed as `any`).
	expressionTerminatorKeywords = map[string]struct{}{
		keywordGROUP: {}, keywordHAVING: {}, keywordORDER: {}, keywordLIMIT: {},
		keywordOFFSET: {}, keywordFETCH: {}, keywordFOR: {}, "WINDOW": {},
		keywordUNION: {}, keywordINTERSECT: {}, keywordEXCEPT: {},
		keywordRETURNING: {}, keywordSET: {}, keywordON: {},
		keywordFROM: {}, keywordWHERE: {}, "INTO": {},
		keywordJOIN: {}, "INNER": {}, "LEFT": {}, "RIGHT": {},
		"FULL": {}, "CROSS": {}, "NATURAL": {},
	}

	// onConflictTargetPredicateTerminators lists the keywords that end the optional WHERE
	// index-predicate of an ON CONFLICT target at depth zero. The predicate stops before the
	// DO action; RETURNING guards against a malformed clause that omits the action entirely.
	onConflictTargetPredicateTerminators = map[string]struct{}{
		"DO":             {},
		keywordRETURNING: {},
	}

	// joinKeywordTerminators is the subset of expressionTerminatorKeywords whose tokens are
	// also valid SQL function names such as LEFT() and RIGHT().
	//
	// When such a token is immediately followed by an opening parenthesis it is a function
	// call rather than a JOIN starter, so the terminator check must skip it to avoid
	// prematurely ending the surrounding expression and dropping any parameters that follow.
	joinKeywordTerminators = map[string]struct{}{
		"INNER": {}, "LEFT": {}, "RIGHT": {}, "FULL": {}, "CROSS": {}, "NATURAL": {},
	}

	// orderByTerminators lists the keywords that end an ORDER BY scan at the top level.
	orderByTerminators = map[string]struct{}{
		keywordLIMIT: {}, keywordOFFSET: {}, keywordFETCH: {}, keywordFOR: {},
		keywordUNION: {}, keywordINTERSECT: {}, keywordEXCEPT: {},
		keywordRETURNING: {}, "WINDOW": {},
	}

	// limitOffsetValueTerminators lists the keywords that end a LIMIT/OFFSET/FETCH value
	// expression at the top level.
	//
	// Scanning a compound value (a function call or parenthesised expression) stops at the
	// keyword introducing the next clause so the caller can resume.
	limitOffsetValueTerminators = map[string]struct{}{
		keywordLIMIT: {}, keywordOFFSET: {}, keywordFETCH: {}, keywordFOR: {},
		keywordUNION: {}, keywordINTERSECT: {}, keywordEXCEPT: {}, keywordRETURNING: {},
		keywordROW: {}, keywordROWS: {}, keywordWITH: {}, "ONLY": {}, "TIES": {},
		"NEXT": {}, "FIRST": {},
	}

	// setExpressionTerminators lists the keywords that end a SET clause's right-hand-side
	// expression at the top level.
	setExpressionTerminators = map[string]struct{}{
		keywordWHERE: {}, keywordFROM: {}, keywordRETURNING: {},
		keywordORDER: {}, keywordLIMIT: {},
	}
)

// expressionScanAction selects how skipTokensUntilTerminatorSet handles the current token
// after classifyExpressionToken has resolved its role. The classifier may already have
// advanced the parser, so the action distinguishes "stop", "continue without advancing",
// and "advance over a plain token".
type expressionScanAction int

// parseWhereClause reads a WHERE, HAVING or JOIN ON condition and tracks its parameters.
//
// The condition must form one complete expression; missing expressions and leftover
// tokens produce syntax errors.
//
// Returns ast.Expression which is the condition, or nil when there is none or it does not
// parse.
func (p *parser) parseWhereClause() ast.Expression {
	start := p.Position
	p.parseExpressionUntilTerminator()
	if p.Position == start {
		_ = p.syntaxError("expected a condition")
		return nil
	}
	return p.parseConditionTokens(start, p.Position)
}

// parseExpressionUntilTerminator scans a free-form expression up to the next top-level
// terminator keyword, registering any parameters seen along the way.
func (p *parser) parseExpressionUntilTerminator() {
	p.skipTokensUntilTerminatorSet(expressionTerminatorKeywords)
}

// skipTokensUntilTerminatorSet walks the token stream through balanced parentheses until
// a top-level terminator keyword or unbalanced ')' appears, registering any parameters
// seen along the way.
//
// Takes terminators (map[string]struct{}) which is the set of keywords that end the scan.
func (p *parser) skipTokensUntilTerminatorSet(terminators map[string]struct{}) {
	depth := 0
	for !p.AtEnd() {
		current := p.Current()
		newDepth, action := p.classifyExpressionToken(current, depth, terminators)
		depth = newDepth
		switch action {
		case expressionScanContinue:
			continue
		case expressionScanBreak:
			return
		case expressionScanAdvance:
			p.Advance()
		}
	}
}

// classifyExpressionToken inspects the cursor token and decides whether it adjusts the
// paren depth, terminates the scan, or advances past inert content.
//
// Parameter tokens and `(` are consumed in place because handleParameterInExpression and
// the depth bump must happen here; the caller's switch reflects that by returning
// expressionScanContinue without invoking advance again.
//
// Takes current (token) which is the current token.
// Takes depth (int) which is the current paren nesting depth.
// Takes terminators (map[string]struct{}) which is the keyword set that signals the end
// of the scan when met at depth zero.
//
// Returns int which is the updated paren depth.
// Returns expressionScanAction which tells the caller how to proceed.
func (p *parser) classifyExpressionToken(current token, depth int, terminators map[string]struct{}) (int, expressionScanAction) {
	if current.Kind == tokenLeftParen {
		if p.isSubqueryStart() {
			if innerAnalysis, analyseError := p.analyseSubqueryBody(); analyseError == nil {
				p.predicateSubqueries = append(p.predicateSubqueries, innerAnalysis)
			}
			return depth, expressionScanContinue
		}
		p.Advance()
		return depth + 1, expressionScanContinue
	}
	if current.Kind == tokenRightParen {
		if depth == 0 {
			return depth, expressionScanBreak
		}
		p.Advance()
		return depth - 1, expressionScanContinue
	}
	if depth == 0 {
		if action, decided := p.classifyTopLevelToken(current, terminators); decided {
			return depth, action
		}
	}
	if isParameterToken(current.Kind) {
		p.handleParameterInExpression()
		return depth, expressionScanContinue
	}
	return depth, expressionScanAdvance
}

// classifyTopLevelToken decides whether a token outside every parenthesis of an
// expression ends it: an AS OF SYSTEM TIME clause or a terminator keyword does, unless
// the keyword is a function's name or starts IS DISTINCT FROM, which is stepped over.
//
// Takes current (token) which is the current token.
// Takes terminators (map[string]struct{}) which is the keyword set that ends the scan.
//
// Returns expressionScanAction which tells the caller how to proceed.
// Returns bool which is false when the token neither ends the scan nor is stepped over.
func (p *parser) classifyTopLevelToken(current token, terminators map[string]struct{}) (expressionScanAction, bool) {
	if p.isAsOfSystemTime() {
		return expressionScanBreak, true
	}
	if !p.isKeywordTerminator(current, terminators) {
		return expressionScanAdvance, false
	}
	if p.isJoinKeywordFunctionCall(current) || p.isDistinctFrom(current) {
		p.Advance()
		return expressionScanContinue, true
	}
	return expressionScanBreak, true
}

// isJoinKeywordFunctionCall reports whether a JOIN-like terminator keyword is actually
// being used as a function name such as LEFT(...) or RIGHT(...).
//
// The check is the immediate `(` follower, since SQL only allows those identifiers to
// introduce a JOIN or a function call.
//
// Takes candidate (token) which is the candidate identifier.
//
// Returns bool which is true when the keyword is followed by `(`.
func (p *parser) isJoinKeywordFunctionCall(candidate token) bool {
	_, isJoinKeyword := joinKeywordTerminators[candidate.Keyword(tokenIdentifier)]
	if !isJoinKeyword {
		return false
	}
	return p.Peek().Kind == tokenLeftParen
}

// isDistinctFrom reports whether a FROM keyword belongs to an IS [NOT] DISTINCT FROM
// comparison rather than opening a FROM clause.
//
// Takes candidate (token) which is the candidate keyword.
//
// Returns bool which is true when candidate is FROM right after DISTINCT.
func (p *parser) isDistinctFrom(candidate token) bool {
	return candidate.IsKeyword(tokenIdentifier, keywordFROM) && p.Position > 0 &&
		p.Tokens[p.Position-1].IsKeyword(tokenIdentifier, "DISTINCT")
}

// isKeywordTerminator reports whether candidate is an identifier whose upper-cased value
// is a member of the supplied terminator keyword set.
//
// Takes candidate (token) which is the token to test.
// Takes terminators (map[string]struct{}) which is the terminator keyword set.
//
// Returns bool which is true when candidate is a terminator keyword.
func (*parser) isKeywordTerminator(candidate token, terminators map[string]struct{}) bool {
	_, ok := terminators[candidate.Keyword(tokenIdentifier)]
	return ok
}

// parseGroupByClause parses the GROUP BY list at the current position: an optional ALL or
// DISTINCT, then grouping elements, each an expression, a parenthesised list, ROLLUP
// (...), CUBE (...) or GROUPING SETS (...).
//
// Takes outputs ([]ast.RawOutputColumn) which is the select list, whose items a key in a
// grouping modifier may name by position.
//
// Returns []ast.ColumnReference which lists the elements that are plain column
// references.
// Returns *ast.GroupingSets which holds the keys a grouping modifier leaves out of some
// rows, or nil when the list has no modifier.
func (p *parser) parseGroupByClause(outputs []ast.RawOutputColumn) ([]ast.ColumnReference, *ast.GroupingSets) {
	var columns []ast.ColumnReference
	var sets *ast.GroupingSets

	if !p.MatchKeyword(keywordALL) {
		p.MatchKeyword("DISTINCT")
	}
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
// A modifier nested as deep as the parse depth limit fails the statement with a
// dialectkit.DepthError, and is skipped without being descended into.
//
// Takes outputs ([]ast.RawOutputColumn) which is the select list a key may name by
// position.
// Takes depth (int) which bounds the nesting of GROUPING SETS.
//
// Returns []ast.Expression which holds the element's keys.
func (p *parser) parseGroupingSet(outputs []ast.RawOutputColumn, depth int) []ast.Expression {
	switch {
	case p.isGroupingModifier() && depth >= p.maxParseDepth:
		_ = p.recordSyntaxError(dialectkit.DepthError(p.maxParseDepth))
		p.skipGroupingModifier()
		return nil
	case p.isGroupingModifier():
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

// skipGroupingModifier consumes a ROLLUP (...), CUBE (...) or GROUPING SETS (...) without
// reading its keys.
func (p *parser) skipGroupingModifier() {
	if p.IsKeyword("GROUPING") {
		p.Advance()
	}
	p.Advance()
	if p.Current().Kind == tokenLeftParen {
		_ = p.requireSkipParenthesised()
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

// parseOrderByList scans the ORDER BY tail through balanced parens until a terminator
// keyword appears.
func (p *parser) parseOrderByList() {
	p.skipTokensUntilTerminatorSet(orderByTerminators)
}

// consumeParameterOrAdvance consumes a LIMIT/OFFSET value.
//
// When the value is a bare placeholder it is registered with the given context
// (preserving the integer typing and the limit/offset name that context drives).
// Otherwise the value is an expression such as $1::int, :size * :page, COALESCE($1, 100)
// or a parenthesised subexpression, which is scanned in full so every placeholder in it
// is registered with the context too, a cast keeping the type it names.
//
// Takes context (sqlmodel.ParameterContext) which classifies a bare-placeholder value's
// syntactic role.
func (p *parser) consumeParameterOrAdvance(context sqlmodel.ParameterContext) {
	start := p.Position
	defer p.recordPaginationValue(context, start)
	if isParameterToken(p.Current().Kind) && p.endsPaginationValue(p.Position+1) {
		parameterToken := p.Current()
		p.Advance()
		p.registerParameterFromToken(parameterToken, context, nil, nil)
		return
	}
	referenceCountBefore := len(p.parameterReferences)
	p.skipTokensUntilTerminatorSet(limitOffsetValueTerminators)
	p.retagLimitOffsetParameters(referenceCountBefore, context)
}

// recordPaginationValue keeps the value a LIMIT or OFFSET clause gives, from start up to
// the cursor, ranked as PostgreSQL types its parameters: OFFSET before LIMIT, both after
// the rest of the query.
//
// Takes context (sqlmodel.ParameterContext) which says whether the value is a limit or an
// offset.
// Takes start (int) which is the index of the value's first token.
func (p *parser) recordPaginationValue(context sqlmodel.ParameterContext, start int) {
	rank := limitRank
	if context == sqlmodel.ParameterContextOffset {
		rank = offsetRank
	}
	for _, value := range p.reparse(start, p.Position, false, false) {
		p.paginationValues = append(p.paginationValues, rankedTree{expression: value, rank: rank})
	}
}

// endsPaginationValue reports whether the token at a position ends a LIMIT, OFFSET or
// FETCH value: the end of the statement or of its parentheses, or a keyword that starts
// what follows the value.
//
// Takes position (int) which is the index of the token after the value's first token.
//
// Returns bool which is true when the value ends before the token.
func (p *parser) endsPaginationValue(position int) bool {
	if position >= len(p.Tokens) {
		return true
	}
	next := p.Tokens[position]
	switch next.Kind {
	case tokenEOF, tokenSemicolon, tokenRightParen, tokenComma:
		return true
	default:
		return p.isKeywordTerminator(next, limitOffsetValueTerminators)
	}
}

// retagLimitOffsetParameters re-tags every parameter registered while scanning a LIMIT or
// OFFSET value with the limit/offset context.
//
// The placeholder in LIMIT COALESCE($n, 100) is one such parameter; re-tagging overrides
// any incidental function-argument context the inner scan assigned. A placeholder in a
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

// parseLimitOffset parses any combination of LIMIT, OFFSET, and FETCH clauses that appear
// at the end of a SELECT.
func (p *parser) parseLimitOffset() {
	p.parseLeadingOffset()

	if p.MatchKeyword(keywordLIMIT) {
		p.parseLimitClause()
		return
	}

	p.parseFetchClause()
	p.parseTrailingOffset()
}

// parseLeadingOffset consumes an OFFSET clause that appears before any LIMIT or FETCH.
func (p *parser) parseLeadingOffset() {
	if !p.IsKeyword(keywordOFFSET) {
		return
	}
	p.Advance()
	p.consumeParameterOrAdvance(sqlmodel.ParameterContextOffset)
	p.MatchKeyword(keywordROW)
	p.MatchKeyword(keywordROWS)
}

// parseLimitClause consumes the body of a LIMIT clause along with any following OFFSET or
// comma-separated offset value.
func (p *parser) parseLimitClause() {
	if !p.MatchKeyword(keywordALL) {
		p.consumeParameterOrAdvance(sqlmodel.ParameterContextLimit)
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

// parseFetchClause consumes a FETCH FIRST/NEXT ... ROWS ONLY/WITH TIES clause and tags
// any parameter as a limit parameter.
func (p *parser) parseFetchClause() {
	if !p.MatchKeyword(keywordFETCH) {
		return
	}
	if !p.MatchKeyword(keywordFIRST) {
		p.MatchKeyword("NEXT")
	}
	if p.IsKeyword(keywordROW, keywordROWS) {
		p.Advance()
		p.skipFetchEnding()
		return
	}

	if isParameterToken(p.Current().Kind) {
		parameterToken := p.Current()
		p.Advance()
		p.registerParameterFromToken(parameterToken, sqlmodel.ParameterContextLimit, nil, nil)
	} else if p.Current().Kind == tokenNumber {
		p.Advance()
	} else {
		referenceCountBefore := len(p.parameterReferences)
		p.skipTokensUntilTerminatorSet(limitOffsetValueTerminators)
		p.retagLimitOffsetParameters(referenceCountBefore, sqlmodel.ParameterContextLimit)
	}

	p.MatchKeyword(keywordROW)
	p.MatchKeyword(keywordROWS)
	p.skipFetchEnding()
}

// skipFetchEnding consumes the ONLY or WITH TIES that ends a FETCH clause.
func (p *parser) skipFetchEnding() {
	if p.MatchKeyword(keywordWITH) {
		p.MatchKeyword("TIES")
		return
	}
	p.MatchKeyword("ONLY")
}

// parseTrailingOffset consumes an OFFSET clause that follows a FETCH clause.
func (p *parser) parseTrailingOffset() {
	if p.IsKeyword(keywordLIMIT) || !p.MatchKeyword(keywordOFFSET) {
		return
	}
	p.consumeParameterOrAdvance(sqlmodel.ParameterContextOffset)
	p.MatchKeyword(keywordROW)
	p.MatchKeyword(keywordROWS)
}

// parseCompoundQuery consumes a compound query operator (UNION, INTERSECT, EXCEPT) and
// any ALL modifier.
//
// Returns ast.CompoundOperator which is the matched operator, or 0 when no compound
// keyword follows.
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

// skipForUpdateClause consumes a FOR UPDATE / NO KEY UPDATE / SHARE / KEY SHARE locking
// clause along with any OF list and NOWAIT / SKIP LOCKED modifiers.
func (p *parser) skipForUpdateClause() {
	if !p.MatchKeyword(keywordFOR) {
		return
	}
	*p.rowLocked = true
	if p.MatchKeyword("NO") {
		p.MatchKeyword(keywordKEY)
	}
	if !p.MatchKeyword("UPDATE") {
		p.MatchKeyword(keywordKEY)
		_ = p.requireKeyword("SHARE")
	}
	if p.MatchKeyword("OF") {
		for {
			if p.Current().Kind == tokenIdentifier {
				p.Advance()
			}
			if p.Current().Kind != tokenComma {
				break
			}
			p.Advance()
		}
	}
	p.MatchKeyword("NOWAIT")
	p.MatchKeyword("SKIP")
	p.MatchKeyword("LOCKED")
}

// parseValuesClause walks one or more parenthesised VALUES rows, associating each
// parameter with its target column.
//
// Takes tableName (string) which is the target table name.
// Takes columnNames ([]string) which is the target column list.
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

// columnReferenceForIndex builds a column reference for the column at index in
// columnNames within tableName.
//
// Takes tableName (string) which is the column's table alias.
// Takes columnNames ([]string) which is the column list.
// Takes index (int) which is the desired column index.
//
// Returns *ast.ColumnReference which is the column reference, or nil when index is out of
// range.
func (*parser) columnReferenceForIndex(tableName string, columnNames []string, index int) *ast.ColumnReference {
	if index >= len(columnNames) {
		return nil
	}
	return &ast.ColumnReference{
		TableAlias: tableName,
		ColumnName: columnNames[index],
	}
}

// parseValuesRow walks the elements of a single VALUES row, advancing the column index
// for each comma. An element that stops on a token it cannot take skips that token, so
// the row always ends.
//
// Takes tableName (string) which is the target table name.
// Takes columnNames ([]string) which is the target column list.
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

// parseValuesRowElement consumes a single VALUES row element, registering a parameter
// token or skipping a parenthesised or bracketed expression, such as ARRAY[1, 2], whose
// commas separate no values of the row.
//
// Takes tableName (string) which is the target table name.
// Takes columnNames ([]string) which is the target column list.
// Takes columnIndex (int) which is the current row column index.
func (p *parser) parseValuesRowElement(tableName string, columnNames []string, columnIndex int) {
	if isParameterToken(p.Current().Kind) {
		parameterToken := p.Current()
		columnReference := p.columnReferenceForIndex(tableName, columnNames, columnIndex)
		p.Advance()
		var castType *sqlmodel.SQLType
		if p.Current().Kind == tokenCast {
			castType = p.consumeInlineCast()
		}
		p.registerParameterFromToken(parameterToken, sqlmodel.ParameterContextAssignment, columnReference, castType)
		return
	}
	if p.Current().Kind == tokenLeftParen && p.isSubqueryStart() {
		p.captureSetSubquery()
		return
	}
	if slices.Contains(valuesRowTokens.Openers, p.Current().Kind) {
		columnReference := p.columnReferenceForIndex(tableName, columnNames, columnIndex)
		p.scanInsertValueExpression(columnReference)
		return
	}
	if p.Current().Kind != tokenComma {
		p.Advance()
	}
}

// scanInsertValueExpression walks the parenthesised or bracketed expression starting at
// the current token, registering every parameter it encounters with the supplied
// columnReference and an explicit cast type when one follows the parameter such as
// $1::int.
//
// Non-parameter tokens are advanced over so the cursor lands just past the matching
// closing paren or bracket, the commas inside separating no values of the row. This is
// used by parseValuesRowElement to keep INSERT parameters inside function calls like
// COALESCE($1::int, default) properly typed; without it those parameters would be dropped
// and the emitter would default them to *any in the generated params struct. A subquery
// inside the expression is analysed, so its parameters take their types from it.
//
// Takes columnReference (*ast.ColumnReference) which is the INSERT column the expression
// is the value for. Every parameter inside the expression is bound to it.
func (p *parser) scanInsertValueExpression(columnReference *ast.ColumnReference) {
	if !slices.Contains(valuesRowTokens.Openers, p.Current().Kind) {
		return
	}
	p.Advance()
	depth := 1
	for !p.AtEnd() && depth > 0 {
		current := p.Current()
		switch {
		case current.Kind == tokenLeftParen && p.isSubqueryStart():
			p.captureSetSubquery()
		case slices.Contains(valuesRowTokens.Openers, current.Kind):
			depth++
			p.Advance()
		case slices.Contains(valuesRowTokens.Closers, current.Kind):
			depth--
			p.Advance()
		default:
			p.handleInsertValueExpressionToken(current, columnReference)
		}
	}
}

// handleInsertValueExpressionToken processes a non-paren token inside
// scanInsertValueExpression.
//
// Parameter tokens are bound to columnReference and tagged with the type a CAST around
// the parameter alone or an inline `::type` cast after it declares; everything else is
// stepped over. Extracted so the surrounding scan keeps its nesting shallow.
//
// Takes current (token) which is the current token under inspection.
// Takes columnReference (*ast.ColumnReference) which is the INSERT column the surrounding
// parenthesised expression is the value for; every parameter is bound to it.
func (p *parser) handleInsertValueExpressionToken(current token, columnReference *ast.ColumnReference) {
	if !isParameterToken(current.Kind) {
		p.Advance()
		return
	}
	parameterToken := current
	castType := p.castTypeAround(p.Position)
	p.Advance()
	if p.Current().Kind == tokenCast {
		castType = p.consumeInlineCast()
	}
	p.registerParameterFromToken(parameterToken, sqlmodel.ParameterContextAssignment, columnReference, castType)
}

// parseValuesRows parses a VALUES query, naming its columns column1, column2 and so on
// from the first row. It parses every row and registers all parameters.
//
// Takes analysis (*ast.RawQueryAnalysis) which receives the output columns.
//
// Returns error when a row is not enclosed in parentheses.
func (p *parser) parseValuesRows(analysis *ast.RawQueryAnalysis) error {
	if err := p.requireKeyword(keywordVALUES); err != nil {
		return err
	}
	for first := true; ; first = false {
		if p.Current().Kind != tokenLeftParen {
			return p.syntaxError("expected '(' to open a VALUES row")
		}
		p.Advance()
		columns := p.parseValuesListRow()
		if first {
			analysis.OutputColumns = columns
		}
		if p.Current().Kind != tokenRightParen {
			return p.syntaxError("expected ')' to close the VALUES row")
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
// Returns []ast.RawOutputColumn which holds the columns named column1, column2, and so
// on, in element order.
func (p *parser) parseValuesListRow() []ast.RawOutputColumn {
	var outputColumns []ast.RawOutputColumn

	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		expression := p.parseExpression()
		outputColumns = append(outputColumns, ast.RawOutputColumn{
			Name:       fmt.Sprintf("column%d", len(outputColumns)+1),
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

// parseInsertSource rejects a parenthesis after an INSERT's target or column list that
// opens no query, such as INSERT INTO t (a) (1), which PostgreSQL rejects too.
//
// Returns error which is the syntax error at the parenthesis, recorded as the
// statement's.
func (p *parser) parseInsertSource() error {
	return p.syntaxError("expected a query, VALUES or DEFAULT VALUES giving the rows to insert")
}

// parseOnConflict consumes an ON CONFLICT clause including its target, DO NOTHING or DO
// UPDATE action, and any nested SET / WHERE clause.
//
// Takes tableName (string) which is the target table name.
//
// Returns ast.ConflictAction which is what the clause does with a conflicting row.
// Returns error when the conflict target's parentheses are unbalanced, or the clause
// names no action.
func (p *parser) parseOnConflict(tableName string) (ast.ConflictAction, error) {
	if err := p.requireKeyword("CONFLICT"); err != nil {
		return ast.ConflictActionUnrecorded, err
	}

	if p.Current().Kind == tokenLeftParen {
		if err := p.requireSkipParenthesised(); err != nil {
			return ast.ConflictActionUnrecorded, err
		}
		p.skipConflictTargetPredicate()
	}

	if p.MatchKeyword(keywordON) {
		p.MatchKeyword(keywordCONSTRAINT)
		if p.Current().Kind == tokenIdentifier {
			p.Advance()
		}
	}

	if err := p.requireKeyword("DO"); err != nil {
		return ast.ConflictActionUnrecorded, err
	}
	return p.parseConflictAction(tableName)
}

// parseConflictAction consumes the DO NOTHING or DO UPDATE action of an ON CONFLICT
// clause, including any nested SET / WHERE clause.
//
// Takes tableName (string) which is the target table name.
//
// Returns ast.ConflictAction which is ConflictActionDoNothing or ConflictActionDoUpdate.
// Returns error when the action is neither NOTHING nor UPDATE.
func (p *parser) parseConflictAction(tableName string) (ast.ConflictAction, error) {
	if p.MatchKeyword("NOTHING") {
		return ast.ConflictActionDoNothing, nil
	}
	if err := p.requireKeyword("UPDATE"); err != nil {
		return ast.ConflictActionUnrecorded, err
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
// parenthesised ON CONFLICT target, which Postgres permits for partial-unique-index
// inference.
func (p *parser) skipConflictTargetPredicate() {
	if !p.MatchKeyword(keywordWHERE) {
		return
	}
	start := p.Position
	p.skipTokensUntilTerminatorSet(onConflictTargetPredicateTerminators)
	if p.Position == start {
		_ = p.syntaxError("expected a condition")
		return
	}
	p.parseConditionTokens(start, p.Position)
}

// parseSetClause walks a SET clause of an UPDATE, dispatching between single-column and
// multi-column assignment forms.
//
// Takes tableName (string) which is the target table name.
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

// parseSingleColumnSetClause parses a column assignment and associates value parameters
// with the target column. Array-element assignments type subscript parameters as integers
// and assigned-value parameters as the array's element type.
//
// Takes tableName (string) which is the target table name.
func (p *parser) parseSingleColumnSetClause(tableName string) {
	columnName := ""
	if p.Current().Kind == tokenIdentifier {
		p.recordSetTargets(p.Position, p.Position+1)
		columnName = p.Advance().Value
	}
	var elementType *sqlmodel.SQLType
	if columnName != "" && p.Current().Kind == tokenLeftBracket {
		if element := p.parseSetTargetSubscripts(); element {
			elementType = p.arrayElementType(tableName, columnName)
		}
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
		castType := elementType
		if p.Current().Kind == tokenCast {
			castType = p.consumeInlineCast()
		}
		p.registerParameterFromToken(parameterToken, sqlmodel.ParameterContextAssignment, columnReference, castType)
	}

	p.skipSetExpressionWithColumn(columnReference)
}

// parseSetTargetSubscripts consumes the subscripts of an assignment to elements of an
// array column, typing each parameter they hold as an integer.
//
// Returns bool which is true when the subscripts select one element, and false for a
// slice, such as tags[1:2], which assigns an array.
func (p *parser) parseSetTargetSubscripts() bool {
	integer := new(p.types.normalise("integer"))
	element := true
	for p.Current().Kind == tokenLeftBracket {
		p.Advance()
		for depth := 1; !p.AtEnd() && depth > 0; {
			current := p.Current()
			switch {
			case current.Kind == tokenLeftBracket:
				depth++
			case current.Kind == tokenRightBracket:
				depth--
			case current.Kind == tokenOperator && current.Value == ":" && depth == 1:
				element = false
			case isParameterToken(current.Kind):
				p.Advance()
				p.registerParameterFromToken(current, sqlmodel.ParameterContextUnknown, nil, integer)
				continue
			}
			p.Advance()
		}
	}
	return element
}

// arrayElementType returns the element type of an array column of the statement's target
// table, as the catalogue holds it.
//
// Takes tableName (string) which is the target table's name.
// Takes columnName (string) which is the column's name.
//
// Returns *sqlmodel.SQLType which is the element type, or nil when the column is no array
// the catalogue knows.
func (p *parser) arrayElementType(tableName, columnName string) *sqlmodel.SQLType {
	column := p.catalogue.FindColumn("", tableName, columnName)
	if column == nil {
		return nil
	}
	value := column.ValueType()
	if value.Category != sqlmodel.TypeCategoryArray || value.ElementType == nil {
		return nil
	}
	return new(*value.ElementType)
}

// parseMultiColumnSetClause parses an `(col1, col2) = (val1, val2)` assignment, tagging
// each parameter with its target column.
//
// Takes tableName (string) which is the target table name.
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

// parseMultiColumnSetValues parses the value list inside a `(col1, col2) = (val1, val2)`
// SET assignment, registering each parameter with its target column. A value that stops
// on a token it cannot take, such as a stray FROM, skips that token, so the list always
// ends.
//
// Takes tableName (string) which is the target table name.
// Takes columnNames ([]string) which is the assigned column list.
func (p *parser) parseMultiColumnSetValues(tableName string, columnNames []string) {
	columnIndex := 0
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		start := p.Position
		if isParameterToken(p.Current().Kind) {
			parameterToken := p.Current()
			columnReference := p.columnReferenceForIndex(tableName, columnNames, columnIndex)
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

// skipSetExpression walks the right-hand side of a SET assignment through balanced parens
// until a comma or terminator keyword appears, registering any parameters seen along the
// way.
func (p *parser) skipSetExpression() {
	p.skipSetExpressionWithColumn(nil)
}

// skipSetExpressionWithColumn walks the RHS of a single SET assignment, stopping at a
// comma or a SET-clause terminator such as WHERE, FROM, or RETURNING at depth 0. A
// subquery in it is analysed as a subquery of the statement, as captureSetSubquery
// describes.
//
// Parameters encountered inside the RHS are registered with the supplied columnReference
// as assignment context so they pick up the column's Go type; inline casts (`$N::type`)
// are honoured too. Passing nil for columnReference preserves the no-context behaviour
// for sites that bind parameters without a column-aware path.
//
// A parameter that is the operand of a comparison inside the expression (for example the
// `$1` in `SET col = CASE WHEN other >= $1 THEN ... END`) takes its type from the
// compared column, not the assignment target: the scanner remembers the most recent
// `<column> <comparison-operator>` pair and binds the following parameter to that column
// with comparison context. Any other parameter (one that contributes to the assigned
// value, such as `COALESCE($1, col)`) keeps the assignment-target column reference so it
// still picks up that column's Go type.
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
		newDepth, stop, handled := p.handleSetExpressionParen(current, depth)
		if stop {
			return
		}
		if handled {
			depth = newDepth
			continue
		}
		if depth == 0 && p.isSetExpressionTerminator(current) {
			return
		}

		if isParameterToken(current.Kind) {
			p.bindSetExpressionParameter(current, columnReference, comparisonColumn)
			comparisonColumn = nil
			lastIdentifier = ""
			continue
		}

		lastIdentifier, comparisonColumn = comparisonColumnForSetParameter(current, tableAlias, lastIdentifier)
		p.Advance()
	}
}

// handleSetExpressionParen processes a parenthesis or bracket token within a SET
// expression scan, so the commas inside ARRAY[1, 2] end no assignment.
//
// It reports the next depth value, whether to stop the outer loop (unmatched right paren
// at depth zero), and whether the token was a parenthesis or bracket at all. The
// comparison state is left untouched so a `(` between a comparison operator and its
// parameter does not break the pairing.
//
// Takes current (token) which is the token under consideration.
// Takes depth (int) which is the current nesting depth.
//
// Returns nextDepth (int) which is the updated nesting depth.
// Returns stop (bool) which is true when the outer loop should break.
// Returns handled (bool) which is true when the token opened or closed a nesting and has
// already been consumed.
func (p *parser) handleSetExpressionParen(current token, depth int) (nextDepth int, stop bool, handled bool) {
	switch {
	case slices.Contains(valuesRowTokens.Openers, current.Kind):
		p.Advance()
		return depth + 1, false, true
	case current.Kind == tokenRightParen && depth == 0:
		return depth, true, true
	case slices.Contains(valuesRowTokens.Closers, current.Kind) && depth > 0:
		p.Advance()
		return depth - 1, false, true
	}
	return depth, false, false
}

// bindSetExpressionParameter records the parameter at the cursor with the column
// reference and context that match its position in the SET expression.
//
// When comparisonColumn is non-nil the parameter is an operand of a comparison (for
// example `WHEN attempt >= $1`), so it is recorded against that column with comparison
// context. Otherwise it contributes to the assigned value and is recorded against the
// SET-target columnReference with assignment context (or unknown context when no target
// is known). The type a CAST around the parameter alone or an inline `::type` cast
// immediately after it declares is recorded as the explicit cast type in both cases.
//
// Takes parameterToken (token) which is the parameter being recorded.
// Takes columnReference (*ast.ColumnReference) which is the SET target column, or nil
// when no assignment target is known.
// Takes comparisonColumn (*ast.ColumnReference) which is the column the parameter is
// compared against, or nil when it is not a comparison operand.
func (p *parser) bindSetExpressionParameter(
	parameterToken token,
	columnReference *ast.ColumnReference,
	comparisonColumn *ast.ColumnReference,
) {
	castType := p.castTypeAround(p.Position)
	p.Advance()
	if p.Current().Kind == tokenCast {
		castType = p.consumeInlineCast()
	}
	if comparisonColumn != nil {
		p.registerParameterFromToken(parameterToken, sqlmodel.ParameterContextComparison, comparisonColumn, castType)
		return
	}
	context := sqlmodel.ParameterContextUnknown
	if columnReference != nil {
		context = sqlmodel.ParameterContextAssignment
	}
	p.registerParameterFromToken(parameterToken, context, columnReference, castType)
}

// isSetExpressionTerminator reports whether candidate ends a SET clause's right-hand
// expression.
//
// Takes candidate (token) which is the token to test.
//
// Returns bool which is true for commas and SET terminator keywords.
func (*parser) isSetExpressionTerminator(candidate token) bool {
	if candidate.Kind == tokenComma {
		return true
	}
	_, ok := setExpressionTerminators[candidate.Keyword(tokenIdentifier)]
	return ok
}

// comparisonColumnForSetParameter advances the comparison scan over a SET expression.
//
// When the previous identifier is immediately followed by a comparison operator, that
// column is reported so a following parameter is typed from it; for example the parameter
// in "WHEN attempt >= $1" is typed from attempt. Any other token clears the pending
// state.
//
// Takes current (token) which is the token being observed.
// Takes tableAlias (string) which qualifies the reported column reference.
// Takes lastIdentifier (string) which is the identifier from the previous step.
//
// Returns nextIdentifier (string) carried into the next step.
// Returns comparisonColumn (*ast.ColumnReference) which is the compared column, or nil
// when no identifier is followed by a comparison operator.
func comparisonColumnForSetParameter(
	current token,
	tableAlias, lastIdentifier string,
) (nextIdentifier string, comparisonColumn *ast.ColumnReference) {
	switch {
	case current.Kind == tokenIdentifier:
		return current.Value, nil
	case current.Kind == tokenOperator && isComparisonOperator(current.Value) && lastIdentifier != "":
		return "", &ast.ColumnReference{TableAlias: tableAlias, ColumnName: lastIdentifier}
	default:
		return "", nil
	}
}

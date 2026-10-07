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
	"strconv"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

var (
	// expressionTerminatorKeywords lists keywords that terminate a free-form SQL expression.
	//
	// The expressions are WHERE, ON, and HAVING. JOIN-introducing keywords are included so
	// that a JOIN's ON expression stops at the next JOIN rather than swallowing the
	// subsequent JOIN clause (which would prevent the next joined table from being added to
	// the scope chain and leave its projected columns typed as `any`).
	expressionTerminatorKeywords = map[string]struct{}{
		keywordGROUP: {}, keywordHAVING: {}, keywordORDER: {}, keywordLIMIT: {},
		keywordOFFSET: {}, "FETCH": {}, keywordFOR: {}, "WINDOW": {},
		keywordUNION: {}, keywordINTERSECT: {}, keywordEXCEPT: {},
		keywordRETURNING: {}, keywordSET: {}, keywordON: {},
		keywordFROM: {}, keywordWHERE: {}, "INTO": {},
		"LOCK": {}, "PROCEDURE": {},
		keywordJOIN: {}, "INNER": {}, "LEFT": {}, "RIGHT": {},
		"FULL": {}, "CROSS": {}, "NATURAL": {},
	}

	// joinKeywordTerminators is the subset of expressionTerminatorKeywords whose tokens are
	// also valid SQL function names.
	//
	// Examples include LEFT() and RIGHT(). When such a token is immediately followed by an
	// opening parenthesis it is a function call rather than a JOIN starter, so the
	// terminator check must skip it to avoid prematurely ending the surrounding expression
	// and dropping any parameters that follow.
	joinKeywordTerminators = map[string]struct{}{
		"INNER": {}, "LEFT": {}, "RIGHT": {}, "FULL": {}, "CROSS": {}, "NATURAL": {},
	}

	// whereExpressionTerminatorKeywords is the terminator set used for WHERE and HAVING
	// expressions.
	//
	// It deliberately omits the JOIN-introducing keywords because those are legal unquoted
	// column names in MySQL and a WHERE or HAVING predicate never starts a JOIN at top
	// level. Including them treated a column named `left`, `right`, or `inner` as a clause
	// boundary and dropped every parameter that followed it. The ON-clause scan keeps the
	// JOIN keywords via expressionTerminatorKeywords.
	whereExpressionTerminatorKeywords = map[string]struct{}{
		keywordGROUP: {}, keywordHAVING: {}, keywordORDER: {}, keywordLIMIT: {},
		keywordOFFSET: {}, "FETCH": {}, keywordFOR: {}, "WINDOW": {},
		keywordUNION: {}, keywordINTERSECT: {}, keywordEXCEPT: {},
		keywordRETURNING: {}, keywordSET: {}, keywordON: {},
		keywordFROM: {}, keywordWHERE: {}, "INTO": {},
		"LOCK": {}, "PROCEDURE": {},
	}

	// orderByTerminators lists keywords that end an ORDER BY clause.
	orderByTerminators = map[string]struct{}{
		keywordLIMIT: {}, keywordOFFSET: {}, "FETCH": {}, keywordFOR: {},
		keywordUNION: {}, keywordINTERSECT: {}, keywordEXCEPT: {},
		keywordRETURNING: {}, "WINDOW": {}, "LOCK": {}, "PROCEDURE": {},
	}

	// setExpressionTerminators lists keywords that end a value expression in an UPDATE SET
	// clause.
	setExpressionTerminators = map[string]struct{}{
		keywordWHERE: {}, keywordFROM: {}, keywordRETURNING: {},
		keywordORDER: {}, keywordLIMIT: {}, keywordAS: {}, keywordON: {},
	}
)

// parseWhereClause consumes the predicate of a WHERE or HAVING clause.
func (p *parser) parseWhereClause() {
	p.parseExpressionUntilTerminator()
}

// parseExpressionUntilTerminator skips a WHERE or HAVING predicate up to its terminator
// set.
func (p *parser) parseExpressionUntilTerminator() {
	p.skipTokensUntilTerminatorSet(whereExpressionTerminatorKeywords)
}

// parseJoinConditionExpression skips a JOIN ON predicate, keeping the JOIN-introducing
// keywords as terminators (via expressionTerminatorKeywords) so the condition stops at
// the next JOIN.
func (p *parser) parseJoinConditionExpression() {
	p.skipTokensUntilTerminatorSet(expressionTerminatorKeywords)
}

// skipTokensUntilTerminatorSet walks tokens, balancing parens, until a member of
// terminators appears at depth zero or the stream ends.
//
// Takes terminators (map[string]struct{}) which lists upper-case keyword terminators to
// stop at.
func (p *parser) skipTokensUntilTerminatorSet(terminators map[string]struct{}) {
	depth := 0
	for !p.AtEnd() {
		current := p.Current()

		nextDepth, stop, handled := p.handleExpressionParen(current, depth)
		if stop {
			break
		}
		if handled {
			depth = nextDepth
			continue
		}
		if depth == 0 && p.isKeywordTerminator(current, terminators) {
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

// handleExpressionParen processes a parenthesis token within an expression scan. Returns
// the next depth value, whether to break the outer loop (unmatched right paren at depth
// zero), and whether the token was a parenthesis at all (and thus already consumed).
//
// Takes current (token) which is the token under consideration.
// Takes depth (int) which is the current parenthesis nesting depth.
//
// Returns nextDepth (int) which is the updated nesting depth.
// Returns stop (bool) which is true when the outer loop should break.
// Returns handled (bool) which is true when the token was consumed here.
func (p *parser) handleExpressionParen(current token, depth int) (nextDepth int, stop bool, handled bool) {
	if current.Kind == tokenLeftParen {
		if p.isSubqueryStart() {
			if innerAnalysis, err := p.analyseSubqueryBody(); err == nil {
				p.predicateSubqueries = append(p.predicateSubqueries, innerAnalysis)
			}
			return depth, false, true
		}
		p.Advance()
		return depth + 1, false, true
	}
	if current.Kind == tokenRightParen {
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
// Takes current (token) which is the candidate terminator token.
//
// Returns bool which is true when the caller should break.
func (p *parser) handleExpressionTerminator(current token) bool {
	if _, isJoinKeyword := joinKeywordTerminators[current.Keyword(tokenIdentifier)]; isJoinKeyword && p.Peek().Kind == tokenLeftParen {
		p.Advance()
		return false
	}
	return true
}

// isKeywordTerminator reports whether current is an identifier whose keyword is in
// terminators.
//
// Takes current (token) which is the token to inspect.
// Takes terminators (map[string]struct{}) which lists the upper-case keyword terminators.
//
// Returns bool which is true when the token's keyword is a terminator.
func (*parser) isKeywordTerminator(current token, terminators map[string]struct{}) bool {
	if current.Kind != tokenIdentifier {
		return false
	}
	_, ok := terminators[current.Keyword(tokenIdentifier)]
	return ok
}

// parseGroupByClause parses a GROUP BY list with an optional WITH ROLLUP.
//
// Each element is parsed as an expression, so a function call or arithmetic over columns
// is consumed whole and its parameters registered; only plain column references are
// returned. WITH ROLLUP adds subtotal rows that leave out the keys from the last
// backwards, and a grand total row that leaves out every key, so every key is one some
// row leaves out.
//
// Takes outputs ([]ast.RawOutputColumn) which is the select list, whose items a key may
// name by position.
//
// Returns []ast.ColumnReference which lists the grouped plain columns.
// Returns *ast.GroupingSets which holds the keys under WITH ROLLUP, or nil without it.
func (p *parser) parseGroupByClause(outputs []ast.RawOutputColumn) ([]ast.ColumnReference, *ast.GroupingSets) {
	var columns []ast.ColumnReference
	var keys []ast.Expression

	for {
		key, column, isColumn := p.parseGroupingElement(outputs)
		keys = append(keys, key)
		if isColumn {
			columns = append(columns, column)
		}
		p.MatchKeyword(keywordASC)
		p.MatchKeyword(keywordDESC)

		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}

	if p.IsKeyword(keywordWITH) && p.Peek().IsKeyword(tokenIdentifier, keywordROLLUP) {
		p.Advance()
		p.Advance()
		return columns, &ast.GroupingSets{Keys: keys, FillsDefaults: false}
	}

	return columns, nil
}

// parseGroupingElement parses one GROUP BY element. An element that is an integer alone
// names the select item at that position.
//
// Takes outputs ([]ast.RawOutputColumn) which is the select list.
//
// Returns ast.Expression which is the key: the element, or the expression of the select
// item it names by position.
// Returns ast.ColumnReference which is the element when it is a plain column.
// Returns bool which is true when the element was a plain column reference.
func (p *parser) parseGroupingElement(outputs []ast.RawOutputColumn) (ast.Expression, ast.ColumnReference, bool) {
	start := p.Position
	first := p.Current()
	expression := p.parseExpression()
	p.recordClauseExpressions(ast.ClauseGroupBy, []ast.Expression{expression})
	if p.Position == start+1 && first.Kind == tokenNumber {
		if position, err := strconv.Atoi(first.Value); err == nil && position >= 1 && position <= len(outputs) &&
			outputs[position-1].Expression != nil {
			return outputs[position-1].Expression, ast.ColumnReference{}, false
		}
	}
	column, ok := expression.(*ast.ColumnRefExpression)
	if !ok {
		return expression, ast.ColumnReference{}, false
	}
	return expression, ast.ColumnReference{TableAlias: column.TableAlias, ColumnName: column.ColumnName}, true
}

// parseOrderByList consumes the body of an ORDER BY clause.
func (p *parser) parseOrderByList() {
	p.skipTokensUntilTerminatorSet(orderByTerminators)
}

// consumeParameterOrAdvance registers a parameter token or advances past the current
// token when it is not a parameter.
//
// A single-token read is sufficient here. Unlike PostgreSQL, DuckDB, and SQLite, MySQL
// forbids an expression in LIMIT or OFFSET (only an integer literal or a ? placeholder is
// accepted), so a compound value such as COALESCE(?, 100) cannot occur and there is no
// nested parameter to scan for.
//
// Takes context (sqlmodel.ParameterContext) which annotates the registered parameter when
// one is consumed.
func (p *parser) consumeParameterOrAdvance(context sqlmodel.ParameterContext) {
	if isParameterToken(p.Current().Kind) {
		parameterToken := p.Current()
		p.Advance()
		p.registerParameterFromToken(parameterToken, context, nil, nil)
		return
	}
	p.Advance()
}

// parseLimitOffset parses LIMIT and OFFSET clauses in either order.
//
// Supports the MySQL `LIMIT a, b` shorthand by swapping the resulting parameter contexts.
func (p *parser) parseLimitOffset() {
	if p.MatchKeyword(keywordOFFSET) {
		p.consumeParameterOrAdvance(sqlmodel.ParameterContextOffset)
		if p.offsetFetch && !p.MatchKeyword("ROWS") {
			p.MatchKeyword(keywordROW)
		}
	}

	if p.offsetFetch && p.IsKeyword("FETCH") {
		p.parseFetchFirst()
		return
	}

	if !p.MatchKeyword(keywordLIMIT) {
		return
	}

	p.consumeParameterOrAdvance(sqlmodel.ParameterContextLimit)

	if p.Current().Kind == tokenComma {
		p.Advance()
		p.parameterReferences = p.swapLastTwoLimitParameters(p.parameterReferences)
		p.consumeParameterOrAdvance(sqlmodel.ParameterContextLimit)
		return
	}

	if p.MatchKeyword(keywordOFFSET) {
		p.consumeParameterOrAdvance(sqlmodel.ParameterContextOffset)
	}
}

// parseFetchFirst parses FETCH {FIRST | NEXT} [count] {ROW | ROWS} {ONLY | WITH TIES},
// the standard form of a row limit MariaDB takes after ORDER BY and an optional OFFSET n
// ROWS, whose count, one when left out, binds as a LIMIT parameter.
func (p *parser) parseFetchFirst() {
	p.Advance()
	if !p.MatchKeyword("FIRST") && !p.MatchKeyword("NEXT") {
		p.reportSyntaxError("expected FIRST or NEXT after FETCH")
		return
	}
	if !p.IsKeyword(keywordROW, "ROWS") {
		p.consumeParameterOrAdvance(sqlmodel.ParameterContextLimit)
	}
	if !p.MatchKeyword("ROWS") && !p.MatchKeyword(keywordROW) {
		p.reportSyntaxError("expected ROW or ROWS in FETCH")
		return
	}
	if p.MatchKeyword("ONLY") {
		return
	}
	if !p.MatchKeyword(keywordWITH) || !p.MatchKeyword("TIES") {
		p.reportSyntaxError("expected ONLY or WITH TIES to end FETCH")
	}
}

// swapLastTwoLimitParameters retags the trailing LIMIT parameter as an OFFSET to match
// the `LIMIT offset, count` MySQL form.
//
// Takes references ([]ast.RawParameterReference) which is the running list of parameter
// references.
//
// Returns []ast.RawParameterReference which is the modified list.
func (*parser) swapLastTwoLimitParameters(references []ast.RawParameterReference) []ast.RawParameterReference {
	if len(references) == 0 {
		return references
	}
	lastIndex := len(references) - 1
	if references[lastIndex].Context == sqlmodel.ParameterContextLimit {
		references[lastIndex].Context = sqlmodel.ParameterContextOffset
	}
	return references
}

// parseCompoundQuery matches a compound query operator if present.
//
// Returns ast.CompoundOperator which is the matched operator, or zero when no compound
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

// skipForUpdateClause consumes a trailing FOR UPDATE, FOR SHARE or LOCK IN SHARE MODE
// clause, recording for the whole statement that it locks rows.
func (p *parser) skipForUpdateClause() {
	if p.MatchKeyword(keywordFOR) {
		*p.rowLocked = true
		p.MatchKeyword("UPDATE")
		p.MatchKeyword("SHARE")
		if p.MatchKeyword("OF") {
			p.parseForUpdateTableList()
		}
		p.MatchKeyword("NOWAIT")
		p.MatchKeyword("SKIP")
		p.MatchKeyword("LOCKED")
		if p.MatchKeyword("WAIT") && p.Current().Kind == tokenNumber {
			p.Advance()
		}
		return
	}

	if p.MatchKeyword("LOCK") {
		*p.rowLocked = true
		if p.MatchKeyword("IN") {
			p.MatchKeyword("SHARE")
			p.MatchKeyword("MODE")
		}
	}
}

// parseForUpdateTableList consumes a comma-separated list of table names following a FOR
// UPDATE OF clause.
func (p *parser) parseForUpdateTableList() {
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

// parseValuesClause parses an INSERT ... VALUES row list.
//
// Takes tableName (string) which is the target table name.
// Takes columnNames ([]string) which are the target column names.
func (p *parser) parseValuesClause(tableName string, columnNames []string) {
	for {
		p.matchRowConstructor()
		if p.Current().Kind != tokenLeftParen {
			break
		}
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

// matchRowConstructor consumes the ROW keyword of a VALUES row written ROW(...), as
// MySQL's VALUES statement and INSERT ... VALUES ROW(...) allow.
func (p *parser) matchRowConstructor() {
	if p.IsKeyword(keywordROW) && p.Peek().Kind == tokenLeftParen {
		p.Advance()
	}
}

// columnReferenceForIndex builds a column reference for an index into columnNames.
//
// Takes tableName (string) which is the alias for the reference.
// Takes columnNames ([]string) which is the list of available column names.
// Takes index (int) which is the position within columnNames.
//
// Returns *ast.ColumnReference which is the built reference, or nil when index is out of
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

// parseValuesRow parses a single parenthesised VALUES row. An element that stops on a
// token it cannot take skips that token, so the row always ends.
//
// Takes tableName (string) which is the target table name.
// Takes columnNames ([]string) which are the target column names.
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

// parseValuesRowElement parses one element within a VALUES row.
//
// Takes tableName (string) which is the target table name.
// Takes columnNames ([]string) which are the target column names.
// Takes columnIndex (int) which is the column position in the row.
func (p *parser) parseValuesRowElement(tableName string, columnNames []string, columnIndex int) {
	if isParameterToken(p.Current().Kind) {
		parameterToken := p.Current()
		columnReference := p.columnReferenceForIndex(tableName, columnNames, columnIndex)
		p.Advance()
		p.registerParameterFromToken(parameterToken, sqlmodel.ParameterContextAssignment, columnReference, nil)
		return
	}
	if p.Current().Kind == tokenLeftParen && p.isSubqueryStart() {
		p.captureAssignedSubquery()
		return
	}
	if p.Current().Kind == tokenLeftParen {
		columnReference := p.columnReferenceForIndex(tableName, columnNames, columnIndex)
		p.scanInsertValueExpression(columnReference)
		return
	}
	if p.Current().Kind != tokenComma {
		p.Advance()
	}
}

// captureAssignedSubquery analyses and consumes a subquery a statement assigns: one used
// in a SET assignment or inside a value of a VALUES row. Its parameters take their types
// from it rather than from the column assigned.
func (p *parser) captureAssignedSubquery() {
	if innerAnalysis, analyseError := p.analyseSubqueryBody(); analyseError == nil {
		p.predicateSubqueries = append(p.predicateSubqueries, innerAnalysis)
	}
}

// scanInsertValueExpression walks the parenthesised expression starting at the current
// token, registering every parameter it encounters with the supplied columnReference and
// the type a CAST or CONVERT of the parameter alone declares.
//
// Non-parameter tokens are advanced over so the cursor lands just past the matching
// closing paren. parseValuesRowElement uses it to keep INSERT parameters inside function
// calls properly typed via assignment context. Without it those parameters would be
// dropped and the emitter would default them to *any. A subquery inside the expression is
// analysed, so its parameters take their types from it.
//
// Takes columnReference (*ast.ColumnReference) which is the INSERT column the
// parenthesised expression is the value for.
func (p *parser) scanInsertValueExpression(columnReference *ast.ColumnReference) {
	if p.Current().Kind != tokenLeftParen {
		return
	}
	p.Advance()
	depth := 1
	for !p.AtEnd() && depth > 0 {
		current := p.Current()
		switch current.Kind {
		case tokenLeftParen:
			if p.isSubqueryStart() {
				p.captureAssignedSubquery()
				continue
			}
			depth++
			p.Advance()
		case tokenRightParen:
			depth--
			p.Advance()
		default:
			if isParameterToken(current.Kind) {
				parameterToken := current
				castType := p.castTypeAround(p.Position)
				p.Advance()
				p.registerParameterFromToken(parameterToken, sqlmodel.ParameterContextAssignment, columnReference, castType)
				continue
			}
			p.Advance()
		}
	}
}

// parseInsertSource reports a parenthesis after an INSERT's column list that opens no
// query: MySQL takes the rows from VALUES, SET or a query, which may be parenthesised.
//
// Returns error which is the syntax error at the parenthesis, recorded as the
// statement's.
func (p *parser) parseInsertSource() error {
	return p.syntaxErrorAt("expected VALUES, SET or a query giving the rows to insert")
}

// parseOnDuplicateKeyUpdate parses the DUPLICATE KEY UPDATE assignments that follow the
// ON of an ON DUPLICATE KEY UPDATE clause.
//
// Takes tableName (string) which is the target table name for column reference resolution
// within the SET clause.
//
// Returns error when DUPLICATE KEY UPDATE does not follow the ON.
func (p *parser) parseOnDuplicateKeyUpdate(tableName string) error {
	for _, keyword := range []string{keywordDUPLICATE, keywordKEY, "UPDATE"} {
		if _, err := p.ExpectKeyword(keyword); err != nil {
			p.recordSyntaxError(err)
			return err
		}
	}
	p.parseSetClause(tableName)
	return nil
}

// parseSetClause parses the assignment list of an UPDATE SET clause.
//
// Takes tableName (string) which is the default table for unqualified column references.
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

// parseSingleColumnSetClause parses a `column = value` assignment.
//
// Takes tableName (string) which is the default table when the column is not qualified.
func (p *parser) parseSingleColumnSetClause(tableName string) {
	targetStart := p.Position
	columnName := ""
	if p.Current().Kind == tokenIdentifier {
		columnName = p.Advance().Value
	}

	if p.Current().Kind == tokenDot && p.Peek().Kind == tokenIdentifier {
		tableName = columnName
		p.Advance()
		columnName = p.Advance().Value
	}
	if columnName != "" {
		p.recordSetTarget(targetStart, p.Position)
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
		p.registerParameterFromToken(parameterToken, sqlmodel.ParameterContextAssignment, columnReference, nil)
	}

	p.skipSetExpressionWithColumn(columnReference)
}

// parseMultiColumnSetClause parses a `(c1, c2) = (v1, v2)` assignment, or one whose
// values a subquery gives, which is analysed as a subquery of the statement.
//
// Takes tableName (string) which is the default table for unqualified column references.
func (p *parser) parseMultiColumnSetClause(tableName string) {
	listStart := p.Position
	columnNames, err := p.parseColumnList()
	if err != nil {
		p.recordSyntaxError(err)
	}
	p.recordSetTargetList(listStart, p.Position)

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
		p.captureAssignedSubquery()
		return
	}

	p.Advance()
	p.parseMultiColumnSetValues(tableName, columnNames)
	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}
}

// parseMultiColumnSetValues parses the value tuple in a multi-column SET. A value that
// stops on a token it cannot take, such as a stray FROM, skips that token, so the tuple
// always ends.
//
// Takes tableName (string) which is the default table for unqualified column references.
// Takes columnNames ([]string) which is the parsed LHS column list.
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

// skipSetExpression skips the value expression of a SET assignment.
//
// Registers any parameter tokens encountered with an unknown context.
func (p *parser) skipSetExpression() {
	p.skipSetExpressionWithColumn(nil)
}

// skipSetExpressionWithColumn walks the RHS of a single SET assignment, stopping at a
// comma or a SET-clause terminator at depth 0.
//
// Terminators include WHERE, FROM, and RETURNING. Parameters encountered inside the RHS
// are registered with the supplied setTargetReference as assignment context so they pick
// up the column's Go type. Pass nil for setTargetReference to leave value parameters with
// no context.
//
// A parameter that is the operand of a comparison inside the expression (for example the
// `?` in `SET col = CASE WHEN other >= ? THEN ... END`) takes its type from the compared
// column, not the assignment target: the scanner remembers the most recent `<column>
// <comparison-operator>` pair and binds the following parameter to that column. Any other
// parameter (one that contributes to the assigned value, such as `COALESCE(?, col)`)
// keeps the assignment-target column reference so it still picks up that column's Go
// type.
//
// Takes setTargetReference (*ast.ColumnReference) which is the column on the left of the
// SET assignment; nil leaves value parameters with ParameterContextUnknown.
func (p *parser) skipSetExpressionWithColumn(setTargetReference *ast.ColumnReference) {
	tableAlias := ""
	if setTargetReference != nil {
		tableAlias = setTargetReference.TableAlias
	}
	depth := 0
	var comparisonColumn *ast.ColumnReference
	lastIdentifier := ""
	for !p.AtEnd() {
		current := p.Current()

		nextDepth, stop, handled := p.handleExpressionParen(current, depth)
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
			p.registerSetExpressionParameter(current, setTargetReference, comparisonColumn)
			comparisonColumn = nil
			lastIdentifier = ""
			continue
		}

		lastIdentifier, comparisonColumn = comparisonColumnForSetParameter(current, tableAlias, lastIdentifier)
		p.Advance()
	}
}

// registerSetExpressionParameter consumes a parameter token within a SET assignment RHS
// and attaches the column reference and context that match its position in the
// expression.
//
// When comparisonColumn is non-nil the parameter is an operand of a comparison (for
// example `WHEN attempt >= ?`), so it is recorded against that column with comparison
// context. Otherwise it is treated as contributing to the assigned value and recorded
// against the SET-target column with assignment context (or unknown context when no
// target is known). The type a CAST or CONVERT of the parameter alone declares is
// recorded as its cast type.
//
// Takes parameterToken (token) which is the placeholder token to record.
// Takes setTargetReference (*ast.ColumnReference) which is the SET-target column the
// assigned value is for, when known.
// Takes comparisonColumn (*ast.ColumnReference) which is the column the parameter is
// compared against, or nil when it is not a comparison operand.
func (p *parser) registerSetExpressionParameter(
	parameterToken token,
	setTargetReference *ast.ColumnReference,
	comparisonColumn *ast.ColumnReference,
) {
	castType := p.castTypeAround(p.Position)
	p.Advance()
	if comparisonColumn != nil {
		p.registerParameterFromToken(parameterToken, sqlmodel.ParameterContextComparison, comparisonColumn, castType)
		return
	}
	context := sqlmodel.ParameterContextUnknown
	if setTargetReference != nil {
		context = sqlmodel.ParameterContextAssignment
	}
	p.registerParameterFromToken(parameterToken, context, setTargetReference, castType)
}

// isSetExpressionTerminator reports whether current ends a SET expression.
//
// Takes current (token) which is the candidate token.
//
// Returns bool which is true for commas or terminator keywords.
func (*parser) isSetExpressionTerminator(current token) bool {
	if current.Kind == tokenComma {
		return true
	}
	if current.Kind == tokenIdentifier {
		_, ok := setExpressionTerminators[current.Keyword(tokenIdentifier)]
		return ok
	}
	return false
}

// comparisonColumnForSetParameter advances the comparison scan over a SET expression.
//
// When the previous identifier is immediately followed by a comparison operator, that
// column is reported so a following parameter is typed from it; for example the parameter
// in "WHEN attempt >= ?" is typed from attempt. Any other token clears the pending state.
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

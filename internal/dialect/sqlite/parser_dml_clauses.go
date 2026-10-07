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
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

var (
	// groupByItemTerminators lists the keywords that end a GROUP BY item at depth zero, so a
	// non-column group key (a function call or arithmetic expression) is scanned in full and
	// the cursor lands on the next clause keyword rather than mid-expression.
	groupByItemTerminators = map[string]bool{
		keywordHAVING: true, keywordORDER: true, keywordLIMIT: true,
		keywordUNION: true, keywordINTERSECT: true, keywordEXCEPT: true,
		keywordRETURNING: true,
	}

	// orderByTerminators lists keywords that end the ORDER BY clause at depth zero during
	// the forward scan.
	orderByTerminators = map[string]bool{
		keywordLIMIT: true, keywordUNION: true, keywordINTERSECT: true,
		keywordEXCEPT: true, keywordRETURNING: true,
	}

	// limitOffsetValueTerminators lists the keywords that end a LIMIT or OFFSET value at
	// depth zero. SQLite allows an arbitrary integer expression in LIMIT or OFFSET, so a
	// compound value is scanned up to one of these keywords (or a top-level comma) that
	// begins the next clause.
	limitOffsetValueTerminators = map[string]bool{
		"OFFSET": true, keywordLIMIT: true, keywordUNION: true, keywordINTERSECT: true,
		keywordEXCEPT: true, keywordRETURNING: true,
	}

	// setExpressionTerminators lists keywords that end a SET expression at depth zero during
	// the forward scan. ON ends the SET of an ON CONFLICT DO UPDATE that another ON CONFLICT
	// clause follows.
	setExpressionTerminators = map[string]bool{
		keywordWHERE: true, keywordFROM: true, keywordRETURNING: true,
		keywordORDER: true, keywordLIMIT: true, keywordON: true,
	}

	// onConflictTargetPredicateTerminators lists the keywords that end the optional WHERE
	// index-predicate of an ON CONFLICT target at depth zero. The predicate stops before the
	// DO action; RETURNING guards against a malformed clause that omits the action entirely.
	onConflictTargetPredicateTerminators = map[string]bool{
		"DO": true, keywordRETURNING: true,
	}
)

// matchCompoundOperator consumes a compound query operator when the cursor sits on UNION,
// INTERSECT, or EXCEPT.
//
// Returns ast.CompoundOperator which is the consumed operator, or zero when none is
// matched.
func (p *parser) matchCompoundOperator() ast.CompoundOperator {
	if p.MatchKeyword(keywordUNION) {
		if p.MatchKeyword("ALL") {
			return ast.CompoundUnionAll
		}
		return ast.CompoundUnion
	}
	if p.MatchKeyword(keywordINTERSECT) {
		return ast.CompoundIntersect
	}
	if p.MatchKeyword(keywordEXCEPT) {
		return ast.CompoundExcept
	}
	return 0
}

// resolveComparisonContext detects when a parameter sits on the right-hand side of a
// comparison operator.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns sqlmodel.ParameterContext which is ParameterContextComparison when a comparison
// context is found, else ParameterContextUnknown.
// Returns *ast.ColumnReference which is the LHS column when identifiable, else nil.
func (p *parser) resolveComparisonContext(parameterPosition int) (sqlmodel.ParameterContext, *ast.ColumnReference) {
	if parameterPosition < 2 {
		return sqlmodel.ParameterContextUnknown, nil
	}

	if context, columnReference := p.resolveIsComparisonContext(parameterPosition); context != sqlmodel.ParameterContextUnknown {
		return context, columnReference
	}

	previousToken := p.Tokens[parameterPosition-1]
	if previousToken.Kind != tokenOperator || !isComparisonOperator(previousToken.Value) {
		return sqlmodel.ParameterContextUnknown, nil
	}
	columnReference := p.extractColumnReference(parameterPosition - 2)
	if columnReference == nil {
		return sqlmodel.ParameterContextUnknown, nil
	}
	return sqlmodel.ParameterContextComparison, columnReference
}

// resolveIsComparisonContext detects SQLite null-safe equality where a placeholder
// follows IS or IS NOT.
//
// An example is f.version_parent_id IS ?1. SQLite treats "x IS y" as a null-safe
// comparison for arbitrary operands, so the placeholder is typed from the LHS column
// exactly as "col = ?" is. IS is a plain identifier rather than an operator token, so
// isComparisonOperator is deliberately not widened; the IS handling lives here. IS NULL
// and IS NOT NULL never reach this path because no placeholder follows the NULL literal,
// so they register no parameter.
//
// Takes parameterPosition (int) which is the placeholder's token index.
//
// Returns sqlmodel.ParameterContext which is ParameterContextComparison when an IS
// comparison is found, else ParameterContextUnknown.
// Returns *ast.ColumnReference which is the LHS column when identifiable, else nil.
func (p *parser) resolveIsComparisonContext(parameterPosition int) (sqlmodel.ParameterContext, *ast.ColumnReference) {
	isPosition := parameterPosition - 1
	if isPosition >= 0 && p.Tokens[isPosition].IsKeyword(tokenIdentifier, keywordNOT) {
		isPosition--
	}

	if isPosition < 1 || !p.Tokens[isPosition].IsKeyword(tokenIdentifier, "IS") {
		return sqlmodel.ParameterContextUnknown, nil
	}

	columnReference := p.extractColumnReference(isPosition - 1)
	if columnReference == nil {
		return sqlmodel.ParameterContextUnknown, nil
	}
	return sqlmodel.ParameterContextComparison, columnReference
}

// resolveLikeContext detects an enclosing pattern operator around a parameter.
//
// Uses the parenthesis scan index to find an enclosing LIKE, GLOB, REGEXP, or MATCH
// operator within the boolean and clause boundaries, and returns its LHS column
// reference, falling through to a wider scan of the LHS expression when the
// immediate-left token is not itself a column. NOT LIKE is captured because
// resolveLikeOperatorColumn skips a leading NOT token before resolving the column.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns sqlmodel.ParameterContext which is ParameterContextLike when the parameter sits
// inside a pattern operator's right-hand side, else ParameterContextUnknown.
// Returns *ast.ColumnReference which holds the LHS column when one can be identified,
// else nil.
func (p *parser) resolveLikeContext(parameterPosition int) (sqlmodel.ParameterContext, *ast.ColumnReference) {
	likePosition, found := p.findEnclosingLikeOperator(parameterPosition)
	if !found {
		return sqlmodel.ParameterContextUnknown, nil
	}
	return sqlmodel.ParameterContextLike, p.likeOperatorColumn(likePosition)
}

// findEnclosingLikeOperator uses the parenthesis scan index to find the enclosing pattern
// operator, respecting boolean and clause boundaries.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns int which is the LIKE-style operator's token index when found.
// Returns bool which is true when an operator was located.
func (p *parser) findEnclosingLikeOperator(parameterPosition int) (int, bool) {
	return p.parenthesisScanIndex().EnclosingLikeOperator(parameterPosition)
}

// resolveLikeOperatorColumn picks the column reference associated with a LIKE operator's
// left-hand side, first inspecting the immediately preceding token (skipping a single
// NOT) and falling back to a scan of the wider LHS expression for shapes like `(a || b)
// LIKE ?`.
//
// Takes likePosition (int) which is the LIKE-style operator's token index.
//
// Returns *ast.ColumnReference which is the inferred LHS column or nil when none can be
// identified.
func (p *parser) resolveLikeOperatorColumn(likePosition int) *ast.ColumnReference {
	columnPosition := likePosition - 1
	if columnPosition >= 0 && p.Tokens[columnPosition].IsKeyword(tokenIdentifier, "NOT") {
		columnPosition--
	}
	if columnReference := p.extractColumnReference(columnPosition); columnReference != nil && !isKeywordColumnReference(columnReference) {
		return columnReference
	}
	return p.findColumnInExpressionRange(p.findLikeExpressionStart(likePosition), likePosition-1)
}

// findLikeExpressionStart locates the first token of the LHS expression preceding a
// LIKE-style operator by walking back to the previous predicate boundary or the start of
// the token stream. Used to bound the scan when the LHS is a complex expression rather
// than a bare column.
//
// Takes likePosition (int) which is the LIKE keyword's token index.
//
// Returns int which is the token index where the LHS starts (0 when no boundary precedes
// it).
func (p *parser) findLikeExpressionStart(likePosition int) int {
	groups := p.parenthesisGroups()
	for index := likePosition - 1; index >= 0; index-- {
		current := p.Tokens[index]
		switch current.Kind {
		case tokenRightParen:
			index = groups.opening(index)
			if index < 0 {
				return 0
			}
			continue
		case tokenLeftParen:
			return index + 1
		default:
		}
		if current.Kind == tokenIdentifier && isLikeBoundaryKeyword(current.Keyword(tokenIdentifier)) {
			return index + 1
		}
	}
	return 0
}

// findColumnInExpressionRange scans [start, end] forward for the first identifier that
// looks like a column, skipping function names, SQL types, aliases, and reserved keywords
// so complex LHS expressions like `(name || ' ' || role)` or `CAST(COALESCE(...) AS
// TEXT)` still resolve to a meaningful column.
//
// Takes start (int) which is the inclusive start of the range.
// Takes end (int) which is the inclusive end of the range.
//
// Returns *ast.ColumnReference which is the first column found, or nil when none is
// plausible.
func (p *parser) findColumnInExpressionRange(start, end int) *ast.ColumnReference {
	if end < start || end >= len(p.Tokens) {
		return nil
	}
	for index := start; index <= end; index++ {
		current := p.Tokens[index]
		if current.Kind != tokenIdentifier {
			continue
		}
		if isLikelyColumnIdentifier(p.Tokens, index) {
			return p.extractColumnReference(index)
		}
		if isQualifiedColumnTail(p.Tokens, index) {
			return p.extractColumnReference(index)
		}
	}
	return nil
}

// detectParameterContext infers the surrounding clause context of a parameter token by
// inspecting its enclosing parenthesis.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns sqlmodel.ParameterContext which describes the inferred context, or
// ParameterContextUnknown when nothing matches.
// Returns *ast.ColumnReference which is the inferred LHS column, or nil when none
// applies.
// Returns *sqlmodel.SQLType which is the CAST target type when the parameter sits inside
// a CAST expression, else nil.
func (p *parser) detectParameterContext(parameterPosition int) (sqlmodel.ParameterContext, *ast.ColumnReference, *sqlmodel.SQLType) {
	enclosingParen := p.findEnclosingParen(parameterPosition)
	if enclosingParen < 0 {
		return sqlmodel.ParameterContextUnknown, nil, nil
	}

	if enclosingParen >= 2 &&
		p.Tokens[enclosingParen-1].IsKeyword(tokenIdentifier, "IN") {
		columnReference := p.extractColumnReferenceBeforeIN(enclosingParen - 1)
		return sqlmodel.ParameterContextInList, columnReference, nil
	}

	if enclosingParen >= 1 &&
		p.Tokens[enclosingParen-1].IsKeyword(tokenIdentifier, keywordCAST) {
		castType := p.extractCastType(parameterPosition)
		if castType != nil {
			return sqlmodel.ParameterContextCast, nil, castType
		}
	}

	if enclosingParen >= 1 && p.Tokens[enclosingParen-1].Kind == tokenIdentifier {
		functionName := p.Tokens[enclosingParen-1].Keyword(tokenIdentifier)
		if functionName != "IN" && functionName != keywordCAST &&
			functionName != keywordSELECT && functionName != keywordWHERE {
			return sqlmodel.ParameterContextFunctionArgument, nil, nil
		}
	}

	return sqlmodel.ParameterContextUnknown, nil, nil
}

// enclosingFunctionArgument resolves the enclosing function-call metadata for a
// placeholder the flat scan tagged as a function argument.
//
// The metadata is the lower-cased function name (the identifier immediately before the
// enclosing parenthesis) and the placeholder's 0-based argument ordinal. The ordinal
// counts top-level commas between the opening parenthesis and the placeholder, so each
// comma-separated argument slot is counted once regardless of how many tokens it spans.
// The lower-cased name matches how parseFunctionCall and parseTableValuedFunction record
// the function name elsewhere, so the analyser resolves the same overload.
//
// Takes parameterPosition (int) which is the placeholder's token index.
//
// Returns name (string) which is the lower-cased enclosing function name.
// Returns ordinal (int) which is the placeholder's 0-based argument slot.
// Returns ok (bool) which is true when an enclosing function call was identified.
func (p *parser) enclosingFunctionArgument(parameterPosition int) (name string, ordinal int, ok bool) {
	enclosingParen := p.findEnclosingParen(parameterPosition)
	if enclosingParen < 1 || p.Tokens[enclosingParen-1].Kind != tokenIdentifier {
		return "", 0, false
	}

	groups := p.parenthesisGroups()
	if groups.opening(parameterPosition) != enclosingParen {
		return "", 0, false
	}
	return strings.ToLower(p.Tokens[enclosingParen-1].Value), groups.commaCount(parameterPosition), true
}

// findEnclosingParen returns the token index of the parenthesis that encloses position.
//
// Takes position (int) which is the inner token index to search from.
//
// Returns int which is the enclosing `(` token index, or -1 when none encloses position.
func (p *parser) findEnclosingParen(position int) int {
	return p.parenthesisScanIndex().EnclosingParen(position)
}

// extractColumnReferenceBeforeIN returns the column referenced immediately before an IN
// keyword.
//
// The NOT of a NOT IN is skipped, so the column it negates is the one returned.
//
// Takes inPosition (int) which is the IN keyword's token index.
//
// Returns *ast.ColumnReference which is the column reference, or nil when none is
// available.
func (p *parser) extractColumnReferenceBeforeIN(inPosition int) *ast.ColumnReference {
	columnPosition := inPosition - 1
	if p.tokenAt(columnPosition).IsKeyword(tokenIdentifier, keywordNOT) {
		columnPosition--
	}
	if columnPosition < 0 {
		return nil
	}
	return p.extractColumnReference(columnPosition)
}

// castTypeAround returns the type a CAST declares for the parameter at parameterPosition
// when the CAST converts the parameter alone, as in CAST(?1 AS TEXT), and nil otherwise.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns *sqlmodel.SQLType which is the declared type, or nil.
func (p *parser) castTypeAround(parameterPosition int) *sqlmodel.SQLType {
	open := p.findEnclosingParen(parameterPosition)
	if open < 1 || open != parameterPosition-1 || !p.Tokens[open-1].IsKeyword(tokenIdentifier, keywordCAST) ||
		parameterPosition+1 >= len(p.Tokens) || !p.Tokens[parameterPosition+1].IsKeyword(tokenIdentifier, keywordAS) {
		return nil
	}
	return p.extractCastType(parameterPosition)
}

// extractCastType resolves the target type of a CAST expression around a parameter
// position.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns *sqlmodel.SQLType which is the target type, or nil when no AS keyword or type
// name is found.
func (p *parser) extractCastType(parameterPosition int) *sqlmodel.SQLType {
	asPosition := p.findASKeywordAfter(parameterPosition)
	if asPosition < 0 {
		return nil
	}
	typeStart := asPosition + 1
	if typeStart >= len(p.Tokens) || p.Tokens[typeStart].Kind != tokenIdentifier {
		return nil
	}
	return new(p.castTypeAt(typeStart))
}

// castTypeAt returns the type a placeholder takes from the CAST type name starting at
// typeStart and the modifiers written after it, working it out once per name so every
// placeholder of a long CAST operand shares it.
//
// Takes typeStart (int) which is the token index of the type name's first identifier.
//
// Returns sqlmodel.SQLType which is the type.
func (p *parser) castTypeAt(typeStart int) sqlmodel.SQLType {
	if castType, known := p.castTypes[typeStart]; known {
		return castType
	}
	if p.castTypes == nil {
		p.castTypes = make(map[int]sqlmodel.SQLType)
	}
	typeName, end := p.collectCastTypeName(typeStart)
	castType := p.types.parameterCastType(typeName, p.typeModifiersAt(end))
	p.castTypes[typeStart] = castType
	return castType
}

// findASKeywordAfter locates the next AS keyword after a position, stopping at the
// closing parenthesis of the surrounding CAST.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns int which is the AS keyword's token index, or -1 when none is found before the
// closing parenthesis.
func (p *parser) findASKeywordAfter(parameterPosition int) int {
	stop := p.castTypeStop(parameterPosition)
	if stop < 0 || p.Tokens[stop].Kind == tokenRightParen {
		return -1
	}
	return stop
}

// collectCastTypeName joins consecutive identifier tokens into a space-separated CAST
// type name.
//
// Takes start (int) which is the token index of the first identifier.
//
// Returns string which is the space-joined type name.
// Returns int which is the index of the token after the last word, such as the '(' that
// opens the type's modifiers.
func (p *parser) collectCastTypeName(start int) (string, int) {
	var builder strings.Builder
	builder.WriteString(p.Tokens[start].Value)
	index := start + 1
	for ; index < len(p.Tokens); index++ {
		if p.Tokens[index].Kind != tokenIdentifier || p.isCastTypeTerminator(index) {
			break
		}
		builder.WriteString(" ")
		builder.WriteString(p.Tokens[index].Value)
	}
	return builder.String(), index
}

// isCastTypeTerminator reports whether the token at position ends a CAST target type
// name.
//
// Takes position (int) which is the token index to inspect.
//
// Returns bool which is true at a closing parenthesis or a clause keyword.
func (p *parser) isCastTypeTerminator(position int) bool {
	if p.Tokens[position].Kind == tokenRightParen {
		return true
	}
	return p.isKeywordAt(
		position,
		keywordFROM, keywordWHERE, keywordGROUP, keywordHAVING,
		keywordORDER, keywordLIMIT,
	)
}

// isKeywordAt reports whether the identifier at position matches any of the supplied
// keywords ignoring case.
//
// Takes position (int) which is the token index to inspect.
// Takes keywords (...string) which are the candidate upper-case keywords.
//
// Returns bool which is true when the token matches any keyword.
func (p *parser) isKeywordAt(position int, keywords ...string) bool {
	if position >= len(p.Tokens) || p.Tokens[position].Kind != tokenIdentifier {
		return false
	}
	return slices.Contains(keywords, p.Tokens[position].Keyword(tokenIdentifier))
}

// extractColumnReference reads a bare or `alias.column` column reference at the given
// token position. A collation name, as in name COLLATE NOCASE, stands for the column it
// collates.
//
// Takes position (int) which is the column identifier's token index.
//
// Returns *ast.ColumnReference which is the column reference, or nil when the token is
// not an identifier.
func (p *parser) extractColumnReference(position int) *ast.ColumnReference {
	if position < 0 || position >= len(p.Tokens) {
		return nil
	}

	current := p.Tokens[position]
	if current.Kind != tokenIdentifier {
		return nil
	}

	if position >= 1 && p.Tokens[position-1].IsKeyword(tokenIdentifier, keywordCOLLATE) {
		return p.extractColumnReference(position - 2)
	}

	if position >= 2 && p.Tokens[position-1].Kind == tokenDot && p.Tokens[position-2].Kind == tokenIdentifier {
		return &ast.ColumnReference{
			TableAlias: p.Tokens[position-2].Value,
			ColumnName: current.Value,
		}
	}

	return &ast.ColumnReference{
		ColumnName: current.Value,
		TableAlias: "",
	}
}

// parseGroupByList parses a comma-separated GROUP BY column list.
//
// Returns []ast.ColumnReference which is the parsed column list.
func (p *parser) parseGroupByList() []ast.ColumnReference {
	var columns []ast.ColumnReference

	for {
		start := p.Position
		columns = append(columns, p.parseGroupByItem()...)
		p.recordClauseExpressions(ast.ClauseGroupBy, p.reparse(start, p.Position, false, true))

		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}

	return columns
}

// parseGroupByItem parses one GROUP BY item.
//
// A bare column or `alias.column` reference is captured as a column reference. Any other
// group key (such as DATE(created_at) or year + 1) is scanned as a full expression so the
// cursor stops on the next clause keyword and a following HAVING and its parameters are
// not abandoned.
//
// Returns []ast.ColumnReference which holds the parsed column reference when the item is
// a plain column, or nil for an expression group key.
func (p *parser) parseGroupByItem() []ast.ColumnReference {
	if column, ok := p.tryParseGroupByColumn(); ok {
		return column
	}

	p.scanGroupByExpression()
	return nil
}

// tryParseGroupByColumn consumes a plain `column` or `alias.column` group key when the
// next item boundary follows it directly, leaving the cursor unmoved otherwise so the
// caller can fall back to a full expression scan.
//
// Returns []ast.ColumnReference which holds the single parsed column reference.
// Returns bool which is true when a plain column reference was consumed.
func (p *parser) tryParseGroupByColumn() ([]ast.ColumnReference, bool) {
	if p.Current().Kind != tokenIdentifier {
		return nil, false
	}

	if isGroupByItemBoundary(p.Peek()) {
		return []ast.ColumnReference{{ColumnName: p.Advance().Value, TableAlias: ""}}, true
	}

	if p.Peek().Kind != tokenDot {
		return nil, false
	}

	if p.tokenAt(p.Position+2).Kind != tokenIdentifier || !isGroupByItemBoundary(p.tokenAt(p.Position+3)) {
		return nil, false
	}

	alias := p.Advance().Value
	p.Advance()
	return []ast.ColumnReference{{TableAlias: alias, ColumnName: p.Advance().Value}}, true
}

// scanGroupByExpression walks an expression group key at depth zero, registering any
// parameters, and stops at the first depth-zero comma or clause terminator so the next
// clause is parsed correctly.
func (p *parser) scanGroupByExpression() {
	depth := 0
	for !p.AtEnd() {
		current := p.Current()

		if current.Kind == tokenLeftParen {
			depth++
			p.Advance()
			continue
		}
		if current.Kind == tokenRightParen {
			if depth == 0 {
				break
			}
			depth--
			p.Advance()
			continue
		}

		if depth == 0 && (current.Kind == tokenComma || isGroupByItemTerminator(current)) {
			break
		}

		if isParameterToken(current.Kind) {
			p.handleParameterInExpression()
			continue
		}

		p.Advance()
	}
}

// parseOrderByList walks through the ORDER BY clause, registering any parameters and
// stopping at the first depth-zero terminator.
func (p *parser) parseOrderByList() {
	depth := 0
	for !p.AtEnd() {
		current := p.Current()

		if current.Kind == tokenLeftParen {
			depth++
			p.Advance()
			continue
		}
		if current.Kind == tokenRightParen {
			if depth == 0 {
				break
			}
			depth--
			p.Advance()
			continue
		}

		if depth == 0 && isOrderByTerminator(current) {
			break
		}

		if isParameterToken(current.Kind) {
			p.handleParameterInExpression()
			continue
		}

		p.Advance()
	}
}

// parseLimitOffset consumes a LIMIT value with an optional OFFSET or comma-separated
// second value, registering any parameter bindings.
func (p *parser) parseLimitOffset() {
	p.consumeLimitOffsetValue(sqlmodel.ParameterContextLimit)

	if p.MatchKeyword("OFFSET") {
		p.consumeLimitOffsetValue(sqlmodel.ParameterContextOffset)
	} else if p.Current().Kind == tokenComma {
		p.Advance()
		p.consumeLimitOffsetValue(sqlmodel.ParameterContextOffset)
	}
}

// consumeLimitOffsetValue consumes a single LIMIT or OFFSET value.
//
// A bare placeholder is registered with the given context (preserving the integer typing
// and limit or offset name that context drives). A compound value such as COALESCE(?1,
// 100) or a parenthesised subexpression is scanned in full so a nested placeholder is
// still registered rather than dropped; the scan stops at a top-level comma or a keyword
// that begins the next clause.
//
// Takes context (sqlmodel.ParameterContext) which tags a bare-placeholder value.
func (p *parser) consumeLimitOffsetValue(context sqlmodel.ParameterContext) {
	if isParameterToken(p.Current().Kind) {
		parameterToken := p.Current()
		p.Advance()
		p.registerParameterFromToken(parameterToken, context, nil, nil)
		return
	}

	referenceCountBefore := len(p.parameterReferences)
	depth := 0
	for !p.AtEnd() {
		current := p.Current()
		if current.Kind == tokenLeftParen {
			depth++
			p.Advance()
			continue
		}
		if current.Kind == tokenRightParen {
			if depth == 0 {
				break
			}
			depth--
			p.Advance()
			continue
		}
		if depth == 0 && (current.Kind == tokenComma || isLimitOffsetValueTerminator(current)) {
			break
		}
		if isParameterToken(current.Kind) {
			p.handleParameterInExpression()
			continue
		}
		p.Advance()
	}
	p.retagLimitOffsetParameters(referenceCountBefore, context)
}

// retagLimitOffsetParameters re-tags every parameter registered while scanning a compound
// LIMIT or OFFSET value with the limit or offset context.
//
// An example is the placeholder in LIMIT COALESCE(?n, 100). Re-tagging overrides any
// incidental function-argument context the inner scan assigned. A placeholder in a LIMIT
// or OFFSET value is an integer row count, so it types as an integer rather than the
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

// parseInsertValues dispatches on the INSERT payload: a VALUES row list, DEFAULT VALUES,
// a SELECT/WITH query source, or an opaque source.
//
// Takes tableName (string) which scopes column lookups in the VALUES clause.
// Takes columnNames ([]string) which lists the target columns for parameter binding.
//
// Returns *ast.RawQueryAnalysis which is the analysed SELECT body of an INSERT ... SELECT
// (nil for VALUES/DEFAULT VALUES/opaque sources). The body is analysed so its FROM/JOIN
// relations and parameters resolve against the body's own scope; analyseSelect stops at a
// trailing ON CONFLICT (keywordON is a WHERE terminator) or RETURNING.
// Returns error when the SELECT body fails to parse.
func (p *parser) parseInsertValues(tableName string, columnNames []string) (*ast.RawQueryAnalysis, error) {
	switch {
	case p.MatchKeyword(keywordVALUES):
		p.parseValuesClause(tableName, columnNames)
	case p.MatchKeyword("DEFAULT"):
		p.MatchKeyword(keywordVALUES)
	case p.IsKeyword(keywordSELECT) || p.IsKeyword(keywordWITH):

		p.insertTargetTable = tableName
		p.insertTargetColumns = columnNames
		return p.analyseSelect()
	default:
		p.parseInsertSource()
	}
	return nil, nil
}

// parseInsertSource walks the token stream of an INSERT source, registering any parameter
// bindings and tracking parenthesis depth.
func (p *parser) parseInsertSource() {
	depth := 0
	for !p.AtEnd() {
		current := p.Current()
		if isInsertSourceTerminator(current, depth) {
			break
		}

		switch current.Kind {
		case tokenLeftParen:
			depth++
		case tokenRightParen:
			depth--
		default:
		}

		if isParameterToken(current.Kind) {
			p.handleParameterInExpression()
			continue
		}

		p.Advance()
	}
}

// parseValuesClause parses a `VALUES (...), (...)` row list and associates each parameter
// with its target column.
//
// Takes tableName (string) which is the target table name used as the column reference
// alias.
// Takes columnNames ([]string) which are the target column names in positional order.
func (p *parser) parseValuesClause(tableName string, columnNames []string) {
	for p.Current().Kind == tokenLeftParen {
		open := p.Position
		p.Advance()
		p.parseOneValueRow(tableName, columnNames)

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

// parseOneValueRow parses a single `(...)` row inside a VALUES clause.
//
// Takes tableName (string) which is the target table name used as the column reference
// alias.
// Takes columnNames ([]string) which are the target column names in positional order.
func (p *parser) parseOneValueRow(tableName string, columnNames []string) {
	columnIndex := 0
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		columnIndex += p.parseOneValueElement(tableName, columnNames, columnIndex)
	}
}

// parseOneValueElement consumes one VALUES element and returns whether it crossed a comma
// into the next column.
//
// Takes tableName (string) which is the target table name used as the column reference
// alias.
// Takes columnNames ([]string) which are the target column names in positional order.
// Takes columnIndex (int) which is the current column ordinal.
//
// Returns int which is 1 when a trailing comma was consumed, else 0.
func (p *parser) parseOneValueElement(tableName string, columnNames []string, columnIndex int) int {
	p.consumeValueElement(tableName, columnNames, columnIndex)
	if p.Current().Kind == tokenComma {
		p.Advance()
		return 1
	}
	return 0
}

// consumeValueElement consumes the tokens of one VALUES element up to the comma or
// parenthesis that ends it: a parameter, which it registers against the column, a
// subquery, which it analyses, or a parenthesised expression, whose parameters it
// registers. Any other token is stepped over.
//
// Takes tableName (string) which is the target table name used as the column reference
// alias.
// Takes columnNames ([]string) which are the target column names in positional order.
// Takes columnIndex (int) which is the current column ordinal.
func (p *parser) consumeValueElement(tableName string, columnNames []string, columnIndex int) {
	switch {
	case isParameterToken(p.Current().Kind):
		columnReference := columnReferenceForIndex(tableName, columnNames, columnIndex)
		parameterToken := p.Current()
		p.Advance()
		p.registerParameterFromToken(parameterToken, sqlmodel.ParameterContextAssignment, columnReference, nil)
	case p.Current().Kind == tokenLeftParen && p.isSubqueryStart():
		p.captureSetSubquery()
	case p.Current().Kind == tokenLeftParen:
		p.scanInsertValueExpression(columnReferenceForIndex(tableName, columnNames, columnIndex))
	case p.Current().Kind != tokenComma:
		p.Advance()
	}
}

// scanInsertValueExpression scans a parenthesised INSERT value, registering parameters
// against columnReference and any cast of a parameter alone. It analyses nested
// subqueries for parameter types and stops after the matching closing parenthesis.
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
				p.captureSetSubquery()
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

// parseSetClause parses an UPDATE SET clause, registering each parameter against its
// target column.
//
// Takes tableName (string) which is the target table name used as the column reference
// alias.
func (p *parser) parseSetClause(tableName string) {
	for {
		assignmentStart := p.Position
		if columnNames, isRow := p.rowAssignmentColumns(); isRow {
			p.parseRowAssignmentValues(tableName, columnNames)
		} else {
			p.parseColumnAssignment(tableName)
		}
		p.recordAssignment(assignmentStart, p.Position)

		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
}

// parseColumnAssignment walks one assignment of a single column, column = value,
// registering the parameters of the value against the column.
//
// Takes tableName (string) which is the table the statement updates.
func (p *parser) parseColumnAssignment(tableName string) {
	columnName := ""
	if p.Current().Kind == tokenIdentifier {
		columnName = p.Advance().Value
	}

	if p.Current().Kind == tokenOperator && p.Current().Value == "=" {
		p.Advance()
	}

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

// rowAssignmentColumns reads the columns of a row assignment, SET (a, b) = ..., and the =
// after them, leaving the parser at the value.
//
// Returns []string which holds the assigned columns in order.
// Returns bool which is false, with the parser where it was, when no row assignment
// starts here.
func (p *parser) rowAssignmentColumns() ([]string, bool) {
	if p.Current().Kind != tokenLeftParen {
		return nil, false
	}
	index := p.Position + 1
	var columnNames []string
	for p.tokenAt(index).Kind == tokenIdentifier {
		columnNames = append(columnNames, p.tokenAt(index).Value)
		index++
		if p.tokenAt(index).Kind != tokenComma {
			break
		}
		index++
	}
	equals := p.tokenAt(index + 1)
	if len(columnNames) == 0 || p.tokenAt(index).Kind != tokenRightParen || equals.Kind != tokenOperator || equals.Value != "=" {
		return nil, false
	}
	p.Position = index + 2
	return columnNames, true
}

// parseRowAssignmentValues walks the value of a row assignment: a row of values, each a
// value of the column in its place, or a subquery giving the row.
//
// Takes tableName (string) which is the table the statement updates.
// Takes columnNames ([]string) which are the assigned columns in order.
func (p *parser) parseRowAssignmentValues(tableName string, columnNames []string) {
	if p.Current().Kind != tokenLeftParen || p.isSubqueryStart() {
		p.skipSetExpressionWithColumn(nil)
		return
	}
	p.Advance()
	for index := 0; !p.AtEnd(); index++ {
		var columnReference *ast.ColumnReference
		if index < len(columnNames) {
			columnReference = &ast.ColumnReference{TableAlias: tableName, ColumnName: columnNames[index]}
		}
		p.skipSetExpressionWithColumn(columnReference)
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}
}

// skipSetExpressionWithColumn walks the RHS of a single SET assignment, stopping at a
// comma or a SET-clause terminator (WHERE, FROM, RETURNING, etc.) at depth 0. A subquery
// in it is analysed as a subquery of the statement, so its parameters and columns resolve
// in a scope of its own relations.
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
// SET assignment; nil supplies no assignment context.
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

		if current.Kind == tokenLeftParen && p.isSubqueryStart() {
			p.captureSetSubquery()
			continue
		}
		nextDepth, stop, handled := p.handleSetExpressionParen(current, depth)
		if stop {
			break
		}
		if handled {
			depth = nextDepth
			continue
		}
		if depth == 0 && isSetExpressionTerminator(current) {
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

// captureSetSubquery analyses and consumes a subquery a statement assigns: one used in a
// SET assignment or as a value of a VALUES row.
//
// The subquery resolves its own relations within the statement's surrounding scope, and
// its parameters take their types from it rather than from the column assigned.
func (p *parser) captureSetSubquery() {
	if innerAnalysis, ok := p.analyseSubqueryBody(); ok {
		p.predicateSubqueries = append(p.predicateSubqueries, innerAnalysis)
	}
}

// handleSetExpressionParen processes a parenthesis token within a SET expression scan.
// Returns the next depth value, whether to stop the outer loop (unmatched right paren at
// depth zero), and whether the token was a parenthesis at all.
//
// Takes current (token) which is the token under consideration.
// Takes depth (int) which is the current parenthesis nesting depth.
//
// Returns nextDepth (int) which is the updated nesting depth.
// Returns stop (bool) which is true when the outer loop should break.
// Returns handled (bool) which is true when the token was a left or right paren and has
// already been consumed.
func (p *parser) handleSetExpressionParen(current token, depth int) (nextDepth int, stop bool, handled bool) {
	if current.Kind == tokenLeftParen {
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

// registerSetExpressionParameter consumes the parameter token and registers it with the
// column reference and context that match its position in the SET expression.
//
// When comparisonColumn is non-nil the parameter is an operand of a comparison (for
// example `WHEN attempt >= ?`), so it is recorded against that column with comparison
// context. Otherwise it is treated as contributing to the assigned value and recorded
// against the SET-target column with assignment context (or unknown context when no
// target is known). The type a CAST of the parameter alone declares is recorded as its
// cast type.
//
// Takes parameterToken (token) which is the placeholder token to record.
// Takes setTargetReference (*ast.ColumnReference) which is the SET-target column.
// Takes comparisonColumn (*ast.ColumnReference) which is the column the parameter is
// compared against, or nil when the parameter is not a comparison operand.
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

// skipOnConflict consumes an ON CONFLICT clause after its ON: the optional conflict
// target and the DO NOTHING or DO UPDATE SET action.
//
// Takes tableName (string) which is the target table name used when recursing into the DO
// UPDATE SET clause.
//
// Returns ast.ConflictAction which is the clause's action, DO NOTHING or DO UPDATE.
// Returns error when CONFLICT, DO, the action or the SET of DO UPDATE is missing, or the
// conflict target's parentheses are unmatched.
func (p *parser) skipOnConflict(tableName string) (ast.ConflictAction, error) {
	if _, err := p.ExpectKeyword(keywordCONFLICT); err != nil {
		return ast.ConflictActionUnrecorded, err
	}

	if p.Current().Kind == tokenLeftParen {
		if err := p.skipParenthesised(); err != nil {
			return ast.ConflictActionUnrecorded, err
		}
		p.skipConflictTargetPredicate()
	}

	if _, err := p.ExpectKeyword("DO"); err != nil {
		return ast.ConflictActionUnrecorded, err
	}
	written, err := p.ExpectKeyword("NOTHING", "UPDATE")
	if err != nil {
		return ast.ConflictActionUnrecorded, err
	}
	if written.IsKeyword(tokenIdentifier, "NOTHING") {
		return ast.ConflictActionDoNothing, nil
	}
	if _, err := p.ExpectKeyword(keywordSET); err != nil {
		return ast.ConflictActionUnrecorded, err
	}
	p.parseSetClause(tableName)
	if p.MatchKeyword(keywordWHERE) {
		start := p.Position
		p.parseWhereExpression()
		p.conditionExpressions(start, p.Position)
	}
	return ast.ConflictActionDoUpdate, nil
}

// skipConflictTargetPredicate consumes the optional WHERE index-predicate that follows a
// parenthesised ON CONFLICT target, which SQLite permits for partial-index UPSERT.
func (p *parser) skipConflictTargetPredicate() {
	if !p.MatchKeyword(keywordWHERE) {
		return
	}
	start := p.Position
	p.parseExpressionUntilTerminator(onConflictTargetPredicateTerminators)
	p.conditionExpressions(start, p.Position)
}

// isKeywordColumnReference reports whether a column reference's bare column name is
// actually a SQL keyword (LIKE, AND, etc.) rather than a column. Edge case for malformed
// input like "LIKE ?" at position 0 where the backward walk would otherwise treat the
// keyword itself as the LHS column.
//
// Takes columnReference (*ast.ColumnReference) which is the candidate column reference.
//
// Returns bool which is true when the bare column name is a keyword.
func isKeywordColumnReference(columnReference *ast.ColumnReference) bool {
	if columnReference == nil || columnReference.TableAlias != "" {
		return false
	}
	upper := strings.ToUpper(columnReference.ColumnName)
	return isLikePatternKeyword(upper) || isLikeBoundaryKeyword(upper) || isReservedNonColumnKeyword(upper)
}

// isLikelyColumnIdentifier checks whether the identifier at position looks like a column
// rather than a function, type, alias, or reserved keyword by inspecting neighbouring
// tokens.
//
// Takes tokens ([]token) which is the parser's token slice.
// Takes position (int) which is the candidate identifier's index.
//
// Returns bool which is true when the identifier looks like a column.
func isLikelyColumnIdentifier(tokens []token, position int) bool {
	if position < 0 || position >= len(tokens) {
		return false
	}
	current := tokens[position]
	if current.Kind != tokenIdentifier {
		return false
	}
	if position+1 < len(tokens) && tokens[position+1].Kind == tokenLeftParen {
		return false
	}
	if position+1 < len(tokens) && tokens[position+1].Kind == tokenDot {
		return false
	}
	if position > 0 && tokens[position-1].IsKeyword(tokenIdentifier, "AS") {
		return false
	}
	upper := current.Keyword(tokenIdentifier)
	if isLikeBoundaryKeyword(upper) || isLikePatternKeyword(upper) {
		return false
	}
	if isReservedNonColumnKeyword(upper) {
		return false
	}
	return true
}

// isQualifiedColumnTail reports whether the identifier at position is the column half of
// a qualified `<alias>.<column>` reference, so the leftmost-column scan does not skip it.
//
// Takes tokens ([]token) which is the parser's token slice.
// Takes position (int) which is the candidate identifier's index.
//
// Returns bool which is true for the column half of `<alias>.<column>`.
func isQualifiedColumnTail(tokens []token, position int) bool {
	if position < 2 {
		return false
	}
	if tokens[position-1].Kind != tokenDot {
		return false
	}
	if tokens[position-2].Kind != tokenIdentifier {
		return false
	}
	if position+1 < len(tokens) && tokens[position+1].Kind == tokenLeftParen {
		return false
	}
	return true
}

// isReservedNonColumnKeyword lists identifier values that must not be treated as column
// references when scanning a LIKE LHS: casts, type names, literals, and modifier keywords
// that lex as identifiers.
//
// Takes keyword (string) which is the upper-case identifier value.
//
// Returns bool which is true when the value is never a column.
func isReservedNonColumnKeyword(keyword string) bool {
	switch keyword {
	case "AS", "CAST", "COLLATE", "DISTINCT", "ALL", "NULL", "TRUE", "FALSE",
		"INTEGER", "INT", "BIGINT", "SMALLINT", "TINYINT", "REAL", "FLOAT",
		"DOUBLE", "NUMERIC", "DECIMAL", "TEXT", "VARCHAR", "CHAR", "BLOB",
		"BOOLEAN", "BOOL", "DATE", "TIME", "TIMESTAMP", "DATETIME",
		"ESCAPE", keywordBETWEEN, "IS", "IN", "EXISTS", "NOT", "ASC", "DESC":
		return true
	}
	return false
}

// isLikePatternKeyword reports whether a keyword introduces a SQLite string pattern
// match.
//
// Recognised operators are LIKE, GLOB, REGEXP, and MATCH. SQLite has no ILIKE or RLIKE;
// case-insensitive matching uses LIKE semantics configured at compile time. MATCH is the
// FTS5 virtual-table operator.
//
// Takes keyword (string) which is the upper-case identifier value.
//
// Returns bool which is true for LIKE, GLOB, REGEXP, or MATCH.
func isLikePatternKeyword(keyword string) bool {
	switch keyword {
	case "LIKE", "GLOB", "REGEXP", "MATCH":
		return true
	}
	return false
}

// isLikeBoundaryKeyword reports whether a keyword ends the predicate that could contain a
// LIKE pattern, so the backward walk should stop.
//
// Takes keyword (string) which is the upper-case identifier value.
//
// Returns bool which is true at any clause or boolean boundary.
func isLikeBoundaryKeyword(keyword string) bool {
	return dialectkit.IsClauseBoundaryKeyword(keyword)
}

// isGroupByItemBoundary reports whether current closes a GROUP BY item, marking the
// preceding tokens as a complete plain column reference.
//
// Takes current (token) which is the token to inspect.
//
// Returns bool which is true for a comma, a depth-zero clause terminator, or end of
// input.
func isGroupByItemBoundary(current token) bool {
	return current.Kind == tokenComma || current.Kind == tokenEOF || isGroupByItemTerminator(current)
}

// isGroupByItemTerminator reports whether current is a keyword that ends a GROUP BY item.
//
// Takes current (token) which is the token to inspect.
//
// Returns bool which is true for any keyword listed in groupByItemTerminators.
func isGroupByItemTerminator(current token) bool {
	return current.Kind == tokenIdentifier && groupByItemTerminators[current.Keyword(tokenIdentifier)]
}

// isOrderByTerminator reports whether current ends the ORDER BY clause.
//
// Takes current (token) which is the token to inspect.
//
// Returns bool which is true for any keyword listed in orderByTerminators.
func isOrderByTerminator(current token) bool {
	return current.Kind == tokenIdentifier && orderByTerminators[current.Keyword(tokenIdentifier)]
}

// isLimitOffsetValueTerminator reports whether current is a keyword that ends a
// LIMIT/OFFSET value at depth zero.
//
// Takes current (token) which is the token to inspect.
//
// Returns bool which is true for any keyword in limitOffsetValueTerminators.
func isLimitOffsetValueTerminator(current token) bool {
	return current.Kind == tokenIdentifier && limitOffsetValueTerminators[current.Keyword(tokenIdentifier)]
}

// isInsertSourceTerminator reports whether current ends the INSERT source scan at the
// given parenthesis depth.
//
// Takes current (token) which is the current token.
// Takes depth (int) which is the current parenthesis depth.
//
// Returns bool which is true at depth zero for `)`, ON, or RETURNING.
func isInsertSourceTerminator(current token, depth int) bool {
	if current.Kind == tokenRightParen && depth == 0 {
		return true
	}
	if depth == 0 && current.Kind == tokenIdentifier {
		upper := current.Keyword(tokenIdentifier)
		return upper == keywordON || upper == keywordRETURNING
	}
	return false
}

// columnReferenceForIndex resolves the target column reference for a positional VALUES
// element.
//
// Takes tableName (string) which is the target table name used as the column reference
// alias.
// Takes columnNames ([]string) which are the target column names in positional order.
// Takes columnIndex (int) which is the current column ordinal.
//
// Returns *ast.ColumnReference which is the column reference, or nil when the index is
// beyond the declared columns.
func columnReferenceForIndex(tableName string, columnNames []string, columnIndex int) *ast.ColumnReference {
	if columnIndex >= len(columnNames) {
		return nil
	}
	return &ast.ColumnReference{
		TableAlias: tableName,
		ColumnName: columnNames[columnIndex],
	}
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

// isSetExpressionTerminator reports whether current ends a SET expression scan at depth
// zero.
//
// Takes current (token) which is the token to inspect.
//
// Returns bool which is true for commas or keywords in setExpressionTerminators.
func isSetExpressionTerminator(current token) bool {
	if current.Kind == tokenComma {
		return true
	}
	return current.Kind == tokenIdentifier && setExpressionTerminators[current.Keyword(tokenIdentifier)]
}

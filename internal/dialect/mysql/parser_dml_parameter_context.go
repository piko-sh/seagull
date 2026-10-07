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
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

var (
	// keywordsBeforeParenthesis lists the keywords that may stand directly before a
	// parenthesised expression without naming a function, so a group they open is an operand
	// rather than a call. CAST and CONVERT are included because the compared value is the
	// converted column, which keeps its name.
	keywordsBeforeParenthesis = map[string]struct{}{
		keywordIN: {}, keywordEXISTS: {}, "ANY": {}, keywordALL: {}, "SOME": {}, keywordNOT: {},
		"XOR": {}, "BETWEEN": {}, "LIKE": {}, "REGEXP": {}, "RLIKE": {}, "IS": {},
		keywordSELECT: {}, keywordVALUES: {}, "VALUE": {}, "INTERVAL": {}, keywordCAST: {},
		"CONVERT": {}, "ROW": {}, keywordDIV: {}, keywordMOD: {}, "BINARY": {}, keywordAS: {}, keywordBY: {},
		keywordSET: {}, keywordUSING: {}, keywordRETURNING: {}, "AGAINST": {},
	}
)

// handleParameterInExpression registers a parameter found mid-expression.
//
// Inspects surrounding tokens to infer the parameter's context, column reference, and any
// cast type, then advances past the token. When the placeholder sits in a function
// argument list the enclosing function name and argument ordinal are stamped onto the
// reference so the analyser can type it from the matched function signature.
func (p *parser) handleParameterInExpression() {
	parameterPosition := p.Position
	parameterToken := p.Current()

	context, columnReference, castType, functionArgument := p.resolveParameterContext(parameterPosition)

	p.Advance()

	p.registerParameterFromTokenWithFunctionArgument(
		parameterToken, context, columnReference, castType, functionArgument)
}

// resolveParameterContext infers the context for a parameter token.
//
// Tries comparison/arithmetic operator detection first, then LIKE pattern resolution,
// then a wider context scan.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns sqlmodel.ParameterContext which is the inferred context.
// Returns *ast.ColumnReference which is the inferred LHS column when one was identified.
// Returns *sqlmodel.SQLType which is the cast target type when the parameter sits inside
// a CAST.
// Returns functionArgumentMetadata which carries the enclosing function name and argument
// ordinal when the parameter sits in a function argument list, empty otherwise.
func (p *parser) resolveParameterContext(parameterPosition int) (sqlmodel.ParameterContext, *ast.ColumnReference, *sqlmodel.SQLType, functionArgumentMetadata) {
	context, columnReference, resultType := p.resolveContextFromPrecedingOperator(parameterPosition)
	if context != sqlmodel.ParameterContextUnknown {
		return context, columnReference, resultType, functionArgumentMetadata{}
	}
	if likeContext, likeColumn := p.resolveLikeContext(parameterPosition); likeContext != sqlmodel.ParameterContextUnknown {
		return likeContext, likeColumn, nil, functionArgumentMetadata{}
	}
	context, columnReference, castType, functionArgument := p.detectParameterContext(parameterPosition)
	switch {
	case castType != nil:
	case p.searchesFullText(parameterPosition, functionArgument):
		castType = new(p.types.normalise(typeNameText))
	case p.countsIntervalUnits(parameterPosition):
		castType = new(p.types.normalise("bigint"))
	}
	return context, columnReference, castType, functionArgument
}

// countsIntervalUnits reports whether a placeholder is the whole amount of an INTERVAL of
// one simple unit, as in INTERVAL ? HOUR, which the engine takes as a number of the unit;
// the amount of a compound unit such as DAY_HOUR is a string and is not judged.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns bool which is true for such a placeholder.
func (p *parser) countsIntervalUnits(parameterPosition int) bool {
	if parameterPosition < 1 || parameterPosition+1 >= len(p.Tokens) || !p.Tokens[parameterPosition-1].IsKeyword(tokenIdentifier, "INTERVAL") {
		return false
	}
	return intervalUnitKeywords[p.Tokens[parameterPosition+1].Keyword(tokenIdentifier)]
}

// searchesFullText reports whether a placeholder is the whole search expression of a
// MATCH (...) AGAINST (...) full-text search, which the engine takes as a string: the
// first token inside AGAINST's parentheses, followed by their end or a search modifier.
//
// Takes parameterPosition (int) which is the parameter's token index.
// Takes functionArgument (functionArgumentMetadata) which says which call's argument list
// the parameter sits in.
//
// Returns bool which is true for such a placeholder.
func (p *parser) searchesFullText(parameterPosition int, functionArgument functionArgumentMetadata) bool {
	if !strings.EqualFold(functionArgument.enclosingFunctionName, "against") || functionArgument.argumentOrdinal != 0 ||
		parameterPosition < 1 || parameterPosition+1 >= len(p.Tokens) || p.Tokens[parameterPosition-1].Kind != tokenLeftParen {
		return false
	}
	next := p.Tokens[parameterPosition+1]
	return next.Kind == tokenRightParen || next.IsKeyword(tokenIdentifier, keywordIN) || next.IsKeyword(tokenIdentifier, keywordWITH)
}

// resolveLikeContext infers the column behind a LIKE-family pattern.
//
// Uses the cached parenthesis index to find an enclosing LIKE, REGEXP, or RLIKE operator
// within the current predicate. Falls through to a wider LHS scan when the immediate-left
// token is not itself a column. MATCH (...) AGAINST (...) full-text syntax is not
// detected here.
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
	return sqlmodel.ParameterContextLike, p.resolveLikeOperatorColumn(likePosition)
}

// findEnclosingLikeOperator uses the cached parenthesis index to locate the pattern
// operator enclosing parameterPosition, respecting boolean and clause boundaries.
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
	if columnPosition >= 0 && p.Tokens[columnPosition].Kind == tokenIdentifier &&
		p.Tokens[columnPosition].IsKeyword(tokenIdentifier, "NOT") {
		columnPosition--
	}
	if columnReference := p.extractColumnReference(columnPosition); columnReference != nil && !isKeywordColumnReference(columnReference) {
		return columnReference
	}
	return p.findColumnInExpressionRange(p.findLikeExpressionStart(likePosition), likePosition-1)
}

// findLikeExpressionStart locates the first token of the LHS expression preceding a
// LIKE-style operator by walking back to the previous predicate boundary or the start of
// the token stream.
//
// Takes likePosition (int) which is the LIKE keyword's token index.
//
// Returns int which is the token index where the LHS starts (0 when no boundary precedes
// it).
func (p *parser) findLikeExpressionStart(likePosition int) int {
	parenDepth := 0
	for index := likePosition - 1; index >= 0; index-- {
		current := p.Tokens[index]
		switch current.Kind {
		case tokenRightParen:
			parenDepth++
			continue
		case tokenLeftParen:
			if parenDepth > 0 {
				parenDepth--
				continue
			}
			return index + 1
		default:
		}
		if parenDepth > 0 {
			continue
		}
		if isLogicalOperator(current) || (current.Kind == tokenIdentifier && isLikeBoundaryKeyword(current.Keyword(tokenIdentifier))) {
			return index + 1
		}
	}
	return 0
}

// findColumnInExpressionRange scans [start, end] forward for the first identifier that
// looks like a column, skipping function names, SQL types, aliases, and reserved keywords
// so complex LHS expressions still resolve to a meaningful column.
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

// resolveContextFromPrecedingOperator infers context from the preceding comparison or
// arithmetic operator and its left operand.
//
// When the left operand of a comparison is a function call, such as JSON_UNQUOTE(doc) =
// ?, the parameter is compared with the call's result rather than with any column inside
// it, so it binds no column; it takes the result type of a built-in function whose
// overloads all return one type.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns sqlmodel.ParameterContext which is the inferred context.
// Returns *ast.ColumnReference which is the inferred LHS column when one was identified.
// Returns *sqlmodel.SQLType which is the result type of a compared function call, or nil.
func (p *parser) resolveContextFromPrecedingOperator(
	parameterPosition int,
) (sqlmodel.ParameterContext, *ast.ColumnReference, *sqlmodel.SQLType) {
	if parameterPosition < 2 {
		return sqlmodel.ParameterContextUnknown, nil, nil
	}

	previousToken := p.Tokens[parameterPosition-1]
	beforeOperator := parameterPosition - 2

	if previousToken.Kind == tokenOperator && isComparisonOperator(previousToken.Value) {
		if functionName, isCall := p.functionCallEndingAt(beforeOperator); isCall {
			return sqlmodel.ParameterContextComparison, nil, p.builtinResultType(functionName)
		}
		columnReference := p.extractColumnReferenceOrParenthesised(beforeOperator)
		if columnReference != nil {
			return sqlmodel.ParameterContextComparison, columnReference, nil
		}
	}

	if previousToken.Kind == tokenStar || (previousToken.Kind == tokenOperator && isArithmeticOperator(previousToken.Value)) ||
		previousToken.IsKeyword(tokenIdentifier, keywordDIV) || previousToken.IsKeyword(tokenIdentifier, keywordMOD) {
		columnReference := p.extractColumnReference(beforeOperator)
		if columnReference != nil {
			return sqlmodel.ParameterContextComparison, columnReference, nil
		}
	}

	return sqlmodel.ParameterContextUnknown, nil, nil
}

// functionCallEndingAt reports whether the token at position closes a function call's
// argument list, and names the function.
//
// Takes position (int) which is the token index of a candidate closing parenthesis.
//
// Returns string which is the lower-cased function name.
// Returns bool which is true when the parenthesis closes a call rather than a
// parenthesised expression.
func (p *parser) functionCallEndingAt(position int) (string, bool) {
	if position < 0 || position >= len(p.Tokens) || p.Tokens[position].Kind != tokenRightParen {
		return "", false
	}
	openParen := p.findMatchingLeftParen(position)
	if openParen < 1 {
		return "", false
	}
	name := p.Tokens[openParen-1]
	if name.Kind != tokenIdentifier {
		return "", false
	}
	if keyword := name.Keyword(tokenIdentifier); keyword != "" {
		if _, isKeyword := keywordsBeforeParenthesis[keyword]; isKeyword || isLikeBoundaryKeyword(keyword) {
			return "", false
		}
	}
	return strings.ToLower(name.Value), true
}

// builtinResultType returns the result type of a built-in function whose overloads all
// return the same known type, so a parameter compared with a call takes the call's type.
//
// Takes functionName (string) which is the lower-cased function name.
//
// Returns *sqlmodel.SQLType which is the shared result type, or nil when the function is
// not a built-in or its overloads disagree.
func (p *parser) builtinResultType(functionName string) *sqlmodel.SQLType {
	if p.functions == nil {
		return nil
	}
	signatures := p.functions.Functions[functionName]
	if len(signatures) == 0 {
		return nil
	}
	resultType := signatures[0].ReturnType
	if resultType.Category == sqlmodel.TypeCategoryUnknown {
		return nil
	}
	for _, signature := range signatures[1:] {
		if signature.ReturnType.Category != resultType.Category || signature.ReturnType.EngineName != resultType.EngineName {
			return nil
		}
	}
	return &resultType
}

// extractColumnReferenceOrParenthesised returns the column at position or scans inside a
// `(...)` group when position closes a paren.
//
// Takes position (int) which is the token index to inspect.
//
// Returns *ast.ColumnReference which is the resolved column or nil when none is
// identified.
func (p *parser) extractColumnReferenceOrParenthesised(position int) *ast.ColumnReference {
	columnReference := p.extractColumnReference(position)
	if columnReference != nil {
		return columnReference
	}
	if p.Tokens[position].Kind == tokenRightParen {
		return p.extractColumnReferenceFromParenthesised(position)
	}
	return nil
}

// detectParameterContext infers context from the enclosing parenthesis.
//
// Recognises IN lists, CAST expressions, and function argument lists by inspecting the
// identifier immediately before the opening paren. For a function argument it
// additionally records the enclosing function name and the placeholder's zero-based
// argument ordinal so the analyser can type the placeholder from the matched function
// signature.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns sqlmodel.ParameterContext which is the inferred context.
// Returns *ast.ColumnReference which is the inferred column for IN lists, or nil
// otherwise.
// Returns *sqlmodel.SQLType which is the cast target type for CAST contexts, or nil
// otherwise.
// Returns functionArgumentMetadata which carries the enclosing function name and argument
// ordinal for a function argument, empty otherwise.
func (p *parser) detectParameterContext(parameterPosition int) (sqlmodel.ParameterContext, *ast.ColumnReference, *sqlmodel.SQLType, functionArgumentMetadata) {
	enclosingParen := p.findEnclosingParen(parameterPosition)
	if enclosingParen < 0 {
		return sqlmodel.ParameterContextUnknown, nil, nil, functionArgumentMetadata{}
	}

	if enclosingParen >= 2 &&
		p.Tokens[enclosingParen-1].Kind == tokenIdentifier &&
		p.Tokens[enclosingParen-1].IsKeyword(tokenIdentifier, keywordIN) {
		columnReference := p.extractColumnReferenceBeforeIN(enclosingParen - 1)
		return sqlmodel.ParameterContextInList, columnReference, nil, functionArgumentMetadata{}
	}

	if enclosingParen >= 1 &&
		p.Tokens[enclosingParen-1].Kind == tokenIdentifier &&
		p.Tokens[enclosingParen-1].IsKeyword(tokenIdentifier, keywordCAST) {
		castType := p.extractCastType(parameterPosition)
		if castType != nil {
			return sqlmodel.ParameterContextCast, nil, castType, functionArgumentMetadata{}
		}
	}

	if castType := p.convertTypeAround(parameterPosition); castType != nil {
		return sqlmodel.ParameterContextCast, nil, castType, functionArgumentMetadata{}
	}

	if enclosingParen >= 1 && p.Tokens[enclosingParen-1].Kind == tokenIdentifier {
		functionName := p.Tokens[enclosingParen-1].Keyword(tokenIdentifier)
		if functionName != keywordIN && functionName != keywordCAST &&
			functionName != keywordSELECT && functionName != keywordWHERE {
			metadata := functionArgumentMetadata{
				enclosingFunctionName: p.enclosingFunctionName(enclosingParen - 1),
				argumentOrdinal:       p.argumentOrdinalAt(parameterPosition),
			}
			return sqlmodel.ParameterContextFunctionArgument, nil, nil, metadata
		}
	}

	return sqlmodel.ParameterContextUnknown, nil, nil, functionArgumentMetadata{}
}

// enclosingFunctionName reconstructs the lower-cased, optionally schema-qualified name of
// the function whose opening parenthesis the placeholder sits in.
//
// The name is read from the identifier at namePosition (the token immediately before the
// opening paren). A leading "schema ." qualifier is folded into the returned name so it
// matches the schema-qualified name the engine records on a function or TVF reference,
// which is how the analyser looks the function up.
//
// Takes namePosition (int) which is the token index of the function name identifier.
//
// Returns string which is the lower-cased function name, schema-qualified when
// applicable.
func (p *parser) enclosingFunctionName(namePosition int) string {
	if namePosition < 0 || namePosition >= len(p.Tokens) ||
		p.Tokens[namePosition].Kind != tokenIdentifier {
		return ""
	}

	name := p.Tokens[namePosition].Value
	if namePosition >= 2 &&
		p.Tokens[namePosition-1].Kind == tokenDot &&
		p.Tokens[namePosition-2].Kind == tokenIdentifier {
		name = p.Tokens[namePosition-2].Value + "." + name
	}

	return strings.ToLower(name)
}

// argumentOrdinalAt returns the zero-based ordinal of the argument slot the placeholder
// occupies within the call whose opening parenthesis encloses it.
//
// The ordinal counts the commas at the call's own parenthesis depth between the opening
// paren and the placeholder, so a literal, column, or nested expression argument still
// consumes a slot. The returned value is the literal argument slot and is NOT clamped for
// variadic functions; the analyser performs any variadic clamp. The counts come from the
// statement's argument scan index, so the lookup takes constant time.
//
// Takes parameterPosition (int) which is the placeholder's token index, enclosed by the
// call's opening parenthesis.
//
// Returns int which is the zero-based argument ordinal.
func (p *parser) argumentOrdinalAt(parameterPosition int) int {
	ordinals := p.argumentScan().ordinals
	if parameterPosition < 0 || parameterPosition >= len(ordinals) {
		return 0
	}
	return ordinals[parameterPosition]
}

// findEnclosingParen returns the index of the enclosing `(` at depth zero.
//
// Takes position (int) which is the inner token index to start from.
//
// Returns int which is the index of the enclosing `(`, or -1 when none.
func (p *parser) findEnclosingParen(position int) int {
	return p.parenthesisScanIndex().EnclosingParen(position)
}

// extractColumnReferenceBeforeIN returns the column before an IN keyword, looking past
// the NOT of a NOT IN so the list binds the column rather than the keyword.
//
// Takes inPosition (int) which is the IN keyword's token index.
//
// Returns *ast.ColumnReference which is the LHS column or nil.
func (p *parser) extractColumnReferenceBeforeIN(inPosition int) *ast.ColumnReference {
	columnPosition := inPosition - 1
	if columnPosition >= 0 && p.Tokens[columnPosition].IsKeyword(tokenIdentifier, keywordNOT) {
		columnPosition--
	}
	if columnPosition < 0 {
		return nil
	}
	return p.extractColumnReference(columnPosition)
}

// castTypeAround returns the type a CAST or CONVERT declares for the parameter at
// parameterPosition when it converts the parameter alone, as in CAST(? AS DATE) or
// CONVERT(?, DATE), and nil otherwise.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns *sqlmodel.SQLType which is the declared type, or nil.
func (p *parser) castTypeAround(parameterPosition int) *sqlmodel.SQLType {
	open := p.findEnclosingParen(parameterPosition)
	if open < 1 || open != parameterPosition-1 || parameterPosition+1 >= len(p.Tokens) ||
		!p.Tokens[open-1].IsKeyword(tokenIdentifier, keywordCAST) ||
		!p.Tokens[parameterPosition+1].IsKeyword(tokenIdentifier, keywordAS) {
		return p.convertTypeAround(parameterPosition)
	}
	return p.extractCastType(parameterPosition)
}

// convertTypeAround returns the type a CONVERT(?, type) declares for the parameter at
// parameterPosition when it converts the parameter alone, and nil otherwise, as for
// CONVERT(? USING charset), which converts a character set rather than a type.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns *sqlmodel.SQLType which is the declared type, or nil.
func (p *parser) convertTypeAround(parameterPosition int) *sqlmodel.SQLType {
	open := p.findEnclosingParen(parameterPosition)
	typeStart := parameterPosition + 2
	if open < 1 || open != parameterPosition-1 || typeStart >= len(p.Tokens) ||
		!p.Tokens[open-1].IsKeyword(tokenIdentifier, keywordCONVERT) || p.Tokens[parameterPosition+1].Kind != tokenComma ||
		p.Tokens[typeStart].Kind != tokenIdentifier {
		return nil
	}
	return p.castTypeAt(typeStart)
}

// extractCastType reads the AS-prefixed type name following a parameter.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns *sqlmodel.SQLType which is the normalised cast target type, or nil when no AS
// clause follows the parameter.
func (p *parser) extractCastType(parameterPosition int) *sqlmodel.SQLType {
	asPosition := p.findASKeywordAfter(parameterPosition)
	if asPosition < 0 {
		return nil
	}
	typeNameStart := asPosition + 1
	if typeNameStart >= len(p.Tokens) || p.Tokens[typeNameStart].Kind != tokenIdentifier {
		return nil
	}
	return p.castTypeAt(typeNameStart)
}

// castTypeAt reads the target type of a CAST or CONVERT whose name starts at
// startPosition.
//
// It follows parseCastTargetTypeName: it reads the words of the name and the optional
// list of one to maxTypeModifiers numeric modifiers. A target that is no column type is
// named by the column type the conversion yields.
//
// Takes startPosition (int) which is the index of the type's first word.
//
// Returns *sqlmodel.SQLType which is the normalised target type.
func (p *parser) castTypeAt(startPosition int) *sqlmodel.SQLType {
	words, end := p.collectCastTypeTokens(startPosition)
	typeName := castTargetColumnType(words)
	modifiers, numeric := p.typeModifiersAhead(end - p.Position)
	if !numeric {
		return new(p.types.normalise(typeName))
	}
	return new(p.types.normalise(typeName, modifiers...))
}

// findASKeywordAfter locates an AS keyword following parameterPosition.
//
// Stops when the closing paren of the enclosing call is reached.
//
// Takes parameterPosition (int) which is the starting token index.
//
// Returns int which is the AS keyword's index, or -1 when not found.
func (p *parser) findASKeywordAfter(parameterPosition int) int {
	for index := parameterPosition + 1; index < len(p.Tokens); index++ {
		if p.Tokens[index].Kind == tokenIdentifier && p.Tokens[index].IsKeyword(tokenIdentifier, keywordAS) {
			return index
		}
		if p.Tokens[index].Kind == tokenRightParen {
			break
		}
	}
	return -1
}

// collectCastTypeTokens joins consecutive identifier tokens for a type.
//
// Stops at anything that is not a word, such as a closing paren or the paren that opens
// the type's modifiers, or at any clause keyword that ends the type.
//
// Takes startPosition (int) which is the first identifier's index.
//
// Returns string which is the space-joined type name.
// Returns int which is the index of the token after the last word.
func (p *parser) collectCastTypeTokens(startPosition int) (string, int) {
	var builder strings.Builder
	builder.WriteString(p.Tokens[startPosition].Value)
	index := startPosition + 1
	for ; index < len(p.Tokens); index++ {
		if p.Tokens[index].Kind != tokenIdentifier ||
			p.isKeywordAt(index, keywordFROM, keywordWHERE, keywordGROUP, keywordHAVING, keywordORDER, keywordLIMIT) {
			break
		}
		builder.WriteByte(' ')
		builder.WriteString(p.Tokens[index].Value)
	}
	return builder.String(), index
}

// isKeywordAt reports whether the token at position matches a keyword.
//
// Takes position (int) which is the token index to check.
// Takes keywords (...string) which are the candidate upper-case keywords.
//
// Returns bool which is true when the token matches any keyword.
func (p *parser) isKeywordAt(position int, keywords ...string) bool {
	if position >= len(p.Tokens) || p.Tokens[position].Kind != tokenIdentifier {
		return false
	}
	return slices.Contains(keywords, p.Tokens[position].Keyword(tokenIdentifier))
}

// extractColumnReference reads a bare or qualified column at position.
//
// Takes position (int) which is the identifier's token index.
//
// Returns *ast.ColumnReference which is the resolved column or nil when position does not
// name an identifier.
func (p *parser) extractColumnReference(position int) *ast.ColumnReference {
	if position < 0 || position >= len(p.Tokens) {
		return nil
	}

	current := p.Tokens[position]
	if current.Kind != tokenIdentifier {
		return nil
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

// extractColumnReferenceFromParenthesised scans inside `(...)` for a column.
//
// Takes rightParenPosition (int) which is the closing paren's index.
//
// Returns *ast.ColumnReference which is the first column found inside the group, or nil
// when none is present.
func (p *parser) extractColumnReferenceFromParenthesised(rightParenPosition int) *ast.ColumnReference {
	leftParenPosition := p.findMatchingLeftParen(rightParenPosition)
	if leftParenPosition < 0 || p.opensSubquery(leftParenPosition) {
		return nil
	}
	return p.scanForColumnReference(leftParenPosition+1, rightParenPosition)
}

// opensSubquery reports whether the parenthesis at a position opens a subquery, as
// opensQueryAt decides, rather than an expression a column stands in.
//
// Takes leftParenPosition (int) which is the `(` token index.
//
// Returns bool which is true for a subquery.
func (p *parser) opensSubquery(leftParenPosition int) bool {
	return p.opensQueryAt(leftParenPosition)
}

// findMatchingLeftParen pairs a `)` with its matching `(`, in constant time through the
// statement's argument scan index.
//
// Takes rightParenPosition (int) which is the closing paren's index.
//
// Returns int which is the matching `(` index, or -1 when unmatched or when the position
// holds no `)`.
func (p *parser) findMatchingLeftParen(rightParenPosition int) int {
	matchingOpen := p.argumentScan().matchingOpen
	if rightParenPosition < 0 || rightParenPosition >= len(matchingOpen) {
		return -1
	}
	return matchingOpen[rightParenPosition]
}

// scanForColumnReference returns the first column found in a token range.
//
// Takes startPosition (int) which is the inclusive start index.
// Takes endPosition (int) which is the exclusive end index.
//
// Returns *ast.ColumnReference which is the first column found, or nil when the range
// contains none.
func (p *parser) scanForColumnReference(startPosition int, endPosition int) *ast.ColumnReference {
	for index := startPosition; index < endPosition; index++ {
		reference := p.extractColumnReference(index)
		if reference != nil {
			return reference
		}
	}
	return nil
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
	if position > 0 && tokens[position-1].Kind == tokenIdentifier &&
		tokens[position-1].IsKeyword(tokenIdentifier, "AS") {
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
	case "AS", "CAST", "CONVERT", "COLLATE", "DISTINCT", "ALL", "NULL", "TRUE", "FALSE",
		"INT", "INTEGER", "TINYINT", "SMALLINT", "MEDIUMINT", "BIGINT",
		"FLOAT", "DOUBLE", "DECIMAL", "NUMERIC",
		"CHAR", "VARCHAR", "TEXT", "TINYTEXT", "MEDIUMTEXT", "LONGTEXT",
		"BINARY", "VARBINARY", "BLOB", "TINYBLOB", "MEDIUMBLOB", "LONGBLOB",
		"BOOLEAN", "BOOL", "DATE", "DATETIME", "TIMESTAMP", "TIME", "YEAR",
		"JSON", "ENUM", "SET",
		"ESCAPE", "BETWEEN", "IS", "IN", "EXISTS", "NOT", "ASC", "DESC", "DIV", "MOD", "XOR":
		return true
	}
	return false
}

// isLikePatternKeyword reports whether a keyword introduces a pattern match.
//
// Matches MySQL's LIKE, REGEXP, and RLIKE. MySQL has no ILIKE or GLOB; case-insensitivity
// is collation-driven. MATCH is part of MATCH (...) AGAINST (...) full-text syntax and is
// not detected here.
//
// Takes keyword (string) which is the upper-case identifier value.
//
// Returns bool which is true for LIKE, REGEXP, or RLIKE.
func isLikePatternKeyword(keyword string) bool {
	switch keyword {
	case "LIKE", "REGEXP", "RLIKE":
		return true
	}
	return false
}

// isLogicalOperator reports whether a token is || or &&, which the server's default SQL
// mode reads as OR and AND, so either ends the predicate a LIKE pattern belongs to as
// those keywords do.
//
// Takes current (token) which is the token.
//
// Returns bool which is true for || and &&.
func isLogicalOperator(current token) bool {
	return current.Kind == tokenOperator && (current.Value == "||" || current.Value == "&&")
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

// isComparisonOperator reports whether operator is a SQL comparison.
//
// Takes operator (string) which is the operator text to test.
//
// Returns bool which is true for =, <>, !=, <, >, <=, >=, or <=>.
func isComparisonOperator(operator string) bool {
	switch operator {
	case "=", "<>", "!=", "<", ">", "<=", ">=", "<=>":
		return true
	}
	return false
}

// isArithmeticOperator reports whether operator is a SQL arithmetic op.
//
// Takes operator (string) which is the operator text to test.
//
// Returns bool which is true for +, -, /, or %.
func isArithmeticOperator(operator string) bool {
	switch operator {
	case "+", "-", "/", "%":
		return true
	}
	return false
}

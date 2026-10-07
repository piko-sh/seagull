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
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

// functionStructExtract is the name of the function that reads a field of a struct.
const functionStructExtract = "struct_extract"

// keywordsOpeningNoCall lists the keywords a parenthesised group follows that open no
// function call, so a comparison with the group is not a comparison with a call's result.
var keywordsOpeningNoCall = map[string]struct{}{
	keywordIN: {}, keywordCAST: {}, keywordSELECT: {}, keywordWHERE: {}, "ANY": {}, "ALL": {}, "SOME": {}, "EXISTS": {},
	"ARRAY": {}, "ROW": {}, "VALUES": {}, "OVER": {}, "FILTER": {}, "USING": {}, "ON": {}, "AND": {}, "OR": {}, "NOT": {},
	"WHEN": {}, "THEN": {}, "ELSE": {}, "LIKE": {}, "ILIKE": {}, "GLOB": {}, "IS": {}, "BETWEEN": {},
}

// handleParameterInExpression registers the current parameter token, inferring its
// context, target column, and inline cast type.
func (p *parser) handleParameterInExpression() {
	parameterPosition := p.Position
	parameterToken := p.Current()

	context, columnReference, castType := p.resolveParameterContext(parameterPosition)

	p.Advance()

	if p.Current().Kind == tokenCast {
		castType = p.consumeInlineCast()
		if context == sqlmodel.ParameterContextUnknown {
			context = sqlmodel.ParameterContextCast
		}
	}

	p.registerParameterFromToken(parameterToken, context, columnReference, castType)
	p.recordFunctionArgumentMetadata(parameterPosition)
}

// resolveParameterContext picks the best context, target column, and cast for the
// parameter at parameterPosition. A name the parameter is compared with that a lambda or
// list comprehension around it declares is the lambda's parameter, which DuckDB binds
// before any column of that name, so it names no target column.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns sqlmodel.ParameterContext which is the inferred context.
// Returns *ast.ColumnReference which is the inferred target column, or nil when none is
// known.
// Returns *sqlmodel.SQLType which is the inferred cast type, or nil when no inline cast
// applies.
func (p *parser) resolveParameterContext(parameterPosition int) (sqlmodel.ParameterContext, *ast.ColumnReference, *sqlmodel.SQLType) {
	context, columnReference, castType := p.inferParameterContext(parameterPosition)
	if p.lambdaBindsReference(parameterPosition, columnReference) {
		columnReference = nil
	}
	return context, columnReference, castType
}

// inferParameterContext infers the context, target column, and cast of the parameter at
// parameterPosition from the tokens around it.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns sqlmodel.ParameterContext which is the inferred context.
// Returns *ast.ColumnReference which is the inferred target column, or nil when none is
// known.
// Returns *sqlmodel.SQLType which is the inferred cast type, or nil when no inline cast
// applies.
func (p *parser) inferParameterContext(parameterPosition int) (sqlmodel.ParameterContext, *ast.ColumnReference, *sqlmodel.SQLType) {
	context, columnReference := p.resolveContextFromPrecedingOperator(parameterPosition)
	callType := p.comparedCallType(parameterPosition)
	if context != sqlmodel.ParameterContextUnknown {
		return context, columnReference, callType
	}
	if callType != nil {
		return sqlmodel.ParameterContextComparison, nil, callType
	}
	if likeContext, likeColumn := p.resolveLikeContext(parameterPosition); likeContext != sqlmodel.ParameterContextUnknown {
		return likeContext, likeColumn, nil
	}
	return p.detectParameterContext(parameterPosition)
}

// comparedCallType finds a built-in function's result type for parameter inference.
//
// It returns nil unless all matching overloads have one known result type.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns *sqlmodel.SQLType which is the call's result type, or nil.
func (p *parser) comparedCallType(parameterPosition int) *sqlmodel.SQLType {
	if parameterPosition < 2 || p.Tokens[parameterPosition-1].Kind != tokenOperator || !isComparisonOperator(p.Tokens[parameterPosition-1].Value) ||
		p.Tokens[parameterPosition-2].Kind != tokenRightParen {
		return nil
	}
	open := p.findMatchingLeftParen(parameterPosition - 2)
	if open < 1 || p.Tokens[open-1].Kind != tokenIdentifier || (open >= 2 && p.Tokens[open-2].Kind == tokenDot) {
		return nil
	}
	if _, keyword := keywordsOpeningNoCall[p.Tokens[open-1].Keyword(tokenIdentifier)]; keyword {
		return nil
	}
	return builtinResultType(p.functions, strings.ToLower(p.Tokens[open-1].Value))
}

// resolveLikeContext infers the LIKE-style context for a parameter.
//
// Uses the cached parenthesis index to find an enclosing LIKE, ILIKE, or GLOB pattern
// operator within the current predicate and returns its LHS column reference, falling
// through to a wider scan of the LHS expression when the immediate-left token is not
// itself a column. DuckDB's regex operators (the ~ family) are handled via the
// comparison-operator path; SIMILAR TO (two tokens) is not detected here.
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
		if current.Kind == tokenIdentifier && isLikeBoundaryKeyword(current.Keyword(tokenIdentifier)) {
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

// resolveContextFromPrecedingOperator infers a parameter's context from a comparison or
// arithmetic operator that precedes it.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns sqlmodel.ParameterContext which is the inferred context or
// ParameterContextUnknown when no operator pattern matches.
// Returns *ast.ColumnReference which is the LHS column when identified, else nil.
func (p *parser) resolveContextFromPrecedingOperator(parameterPosition int) (sqlmodel.ParameterContext, *ast.ColumnReference) {
	if parameterPosition < 2 {
		return sqlmodel.ParameterContextUnknown, nil
	}

	if context, columnReference := p.resolveIsComparisonContext(parameterPosition); context != sqlmodel.ParameterContextUnknown {
		return context, columnReference
	}

	previousToken := p.Tokens[parameterPosition-1]
	beforeOperator := parameterPosition - 2

	if previousToken.Kind == tokenOperator && isComparisonOperator(previousToken.Value) {
		columnReference := p.extractColumnReferenceOrParenthesised(beforeOperator)
		if columnReference != nil {
			return sqlmodel.ParameterContextComparison, columnReference
		}
	}

	if previousToken.Kind == tokenStar || (previousToken.Kind == tokenOperator && isArithmeticOperator(previousToken.Value)) {
		columnReference := p.extractColumnReference(beforeOperator)
		if columnReference != nil {
			return sqlmodel.ParameterContextComparison, columnReference
		}
	}

	return sqlmodel.ParameterContextUnknown, nil
}

// resolveIsComparisonContext recognises "col IS <param>", "col IS NOT <param>" and "col
// IS [NOT] DISTINCT FROM <param>" as null-safe comparisons.
//
// It attaches the left-hand column reference to the placeholder just as "col = <param>"
// does, typing the parameter on the right of IS from that column. IS NULL and IS NOT NULL
// never reach here because a NULL literal, not a parameter, follows the keyword.
//
// The IS keyword is a plain identifier token because the tokeniser only emits
// tokenOperator for the symbolic operator set, so it is detected by value, not by kind.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns sqlmodel.ParameterContext which is ParameterContextComparison on a match, else
// ParameterContextUnknown.
// Returns *ast.ColumnReference which is the left-hand column when found, else nil.
func (p *parser) resolveIsComparisonContext(parameterPosition int) (sqlmodel.ParameterContext, *ast.ColumnReference) {
	isPosition := parameterPosition - 1
	if isPosition >= 1 && p.Tokens[isPosition].IsKeyword(tokenIdentifier, keywordFROM) &&
		p.Tokens[isPosition-1].IsKeyword(tokenIdentifier, "DISTINCT") {
		isPosition -= 2
	}
	if isPosition >= 0 && p.Tokens[isPosition].IsKeyword(tokenIdentifier, keywordNOT) {
		isPosition--
	}
	if isPosition < 1 || p.Tokens[isPosition].Kind != tokenIdentifier ||
		!p.Tokens[isPosition].IsKeyword(tokenIdentifier, keywordIS) {
		return sqlmodel.ParameterContextUnknown, nil
	}

	columnReference := p.extractColumnReferenceOrParenthesised(isPosition - 1)
	if columnReference == nil {
		return sqlmodel.ParameterContextUnknown, nil
	}
	return sqlmodel.ParameterContextComparison, columnReference
}

// extractColumnReferenceOrParenthesised resolves a column reference from position,
// peeking inside a balanced parenthesised group when position is a closing parenthesis.
//
// Takes position (int) which is the candidate column position.
//
// Returns *ast.ColumnReference which is the column reference, or nil when none could be
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

// consumeInlineCast reads a "::type" cast following a parameter and returns the
// normalised type.
//
// Returns *sqlmodel.SQLType which is the normalised cast type.
func (p *parser) consumeInlineCast() *sqlmodel.SQLType {
	p.Advance()
	typeName := p.parseCastTypeName()
	return new(p.types.normalise(typeName))
}

// detectParameterContext infers a parameter's context from the keyword or call that
// encloses it.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns sqlmodel.ParameterContext which is the inferred context or
// ParameterContextUnknown.
// Returns *ast.ColumnReference which is the target column for IN-list patterns, else nil.
// Returns *sqlmodel.SQLType which is the cast type for CAST(...) usage, else nil.
func (p *parser) detectParameterContext(parameterPosition int) (sqlmodel.ParameterContext, *ast.ColumnReference, *sqlmodel.SQLType) {
	enclosingParen := p.findEnclosingParen(parameterPosition)
	if enclosingParen < 0 {
		return sqlmodel.ParameterContextUnknown, nil, nil
	}

	if enclosingParen >= 2 &&
		p.Tokens[enclosingParen-1].Kind == tokenIdentifier &&
		p.Tokens[enclosingParen-1].IsKeyword(tokenIdentifier, keywordIN) {
		columnReference := p.extractColumnReferenceBeforeIN(enclosingParen - 1)
		return sqlmodel.ParameterContextInList, columnReference, nil
	}

	if enclosingParen >= 1 &&
		p.Tokens[enclosingParen-1].Kind == tokenIdentifier &&
		p.Tokens[enclosingParen-1].IsKeyword(tokenIdentifier, keywordCAST) {
		castType := p.extractCastType(parameterPosition)
		if castType != nil {
			return sqlmodel.ParameterContextCast, nil, castType
		}
	}

	if enclosingParen >= 1 && p.Tokens[enclosingParen-1].Kind == tokenIdentifier &&
		isFunctionCallName(p.Tokens[enclosingParen-1].Value) {
		return sqlmodel.ParameterContextFunctionArgument, nil, nil
	}

	return sqlmodel.ParameterContextUnknown, nil, nil
}

// recordFunctionArgumentMetadata records the enclosing function/TVF name and the
// parameter's 0-based argument ordinal on the most recently registered parameter
// reference, so the shared resolver can type the placeholder from the matched signature's
// declared argument type.
//
// The metadata is computed purely from the token stream around parameterPosition, so it
// is valid for every registration path (the expression-tree scalar/TVF argument parser
// and the WHERE/HAVING flat scan alike). It is a no-op when the placeholder does not sit
// directly in a function/TVF argument list; the resolver only consumes the fields when
// the placeholder is also tagged ParameterContextFunctionArgument, so leaving them unset
// is safe.
//
// Takes parameterPosition (int) which is the parameter token's index in the token stream.
func (p *parser) recordFunctionArgumentMetadata(parameterPosition int) {
	name, ordinal, ok := p.enclosingFunctionArgument(parameterPosition)
	if !ok {
		return
	}
	lastIndex := len(p.parameterReferences) - 1
	if lastIndex < 0 {
		return
	}
	p.parameterReferences[lastIndex].EnclosingFunctionName = name
	p.parameterReferences[lastIndex].ArgumentOrdinal = ordinal
}

// enclosingFunctionArgument reconstructs the lower-cased, optionally schema-qualified
// name of the function or table-valued function whose argument list directly encloses the
// parameter at parameterPosition, together with the parameter's 0-based top-level
// argument ordinal.
//
// The enclosing call is the nearest opening parenthesis preceded by a function-name
// identifier (rejected when that identifier is IN, CAST, SELECT, or WHERE so that IN
// lists, casts and subqueries are not mistaken for calls). A schema qualifier (`schema .
// name`) is folded back into the qualified name to match the string the FROM-clause TVF
// reference is recorded under. The ordinal counts top-level commas between the opening
// parenthesis and the parameter, so a non-placeholder argument (a literal, a column, an
// expression) still consumes a slot.
//
// Takes parameterPosition (int) which is the parameter token's index in the token stream.
//
// Returns name (string) which is the qualified lower-cased function name.
// Returns ordinal (int) which is the parameter's 0-based argument slot.
// Returns ok (bool) which is true only when the parameter sits in a recognised call.
func (p *parser) enclosingFunctionArgument(parameterPosition int) (name string, ordinal int, ok bool) {
	enclosingParen := p.findEnclosingParen(parameterPosition)
	if enclosingParen < 1 || p.Tokens[enclosingParen-1].Kind != tokenIdentifier ||
		!isFunctionCallName(p.Tokens[enclosingParen-1].Value) {
		return "", 0, false
	}

	return p.qualifiedFunctionName(enclosingParen - 1), p.argumentOrdinal(enclosingParen, parameterPosition), true
}

// qualifiedFunctionName reads the function name at namePosition and folds a leading
// `schema .` qualifier into a lower-cased, dot-joined name.
//
// Takes namePosition (int) which is the index of the function-name identifier.
//
// Returns string which is the qualified lower-cased function name.
func (p *parser) qualifiedFunctionName(namePosition int) string {
	name := strings.ToLower(p.Tokens[namePosition].Value)
	if namePosition >= 2 && p.Tokens[namePosition-1].Kind == tokenDot &&
		p.Tokens[namePosition-2].Kind == tokenIdentifier {
		return strings.ToLower(p.Tokens[namePosition-2].Value) + "." + name
	}
	return name
}

// argumentOrdinal counts the top-level commas between an opening parenthesis and a
// parameter to derive the parameter's 0-based argument slot, ignoring commas nested
// inside deeper parentheses or brackets.
//
// Placeholders are registered left to right, so the scan for each call resumes from where
// the previous placeholder in the same call stopped; a call with n placeholder arguments
// is scanned once rather than n times.
//
// Takes openParen (int) which is the index of the call's opening parenthesis.
// Takes parameterPosition (int) which is the parameter token's index.
//
// Returns int which is the 0-based argument ordinal.
func (p *parser) argumentOrdinal(openParen int, parameterPosition int) int {
	cursor, resumable := p.argumentCursors[openParen]
	if !resumable || cursor.position > parameterPosition {
		cursor = argumentOrdinalCursor{position: openParen + 1, depth: 0, ordinal: 0}
	}
	for ; cursor.position < parameterPosition && cursor.position < len(p.Tokens); cursor.position++ {
		switch p.Tokens[cursor.position].Kind {
		case tokenLeftParen, tokenLeftBracket:
			cursor.depth++
		case tokenRightParen, tokenRightBracket:
			cursor.depth--
		case tokenComma:
			if cursor.depth == 0 {
				cursor.ordinal++
			}
		default:
		}
	}
	if p.argumentCursors == nil {
		p.argumentCursors = make(map[int]argumentOrdinalCursor)
	}
	p.argumentCursors[openParen] = cursor
	return cursor.ordinal
}

// findEnclosingParen uses the cached parenthesis index to find the opening parenthesis
// enclosing position.
//
// Takes position (int) which is the search starting index.
//
// Returns int which is the index of the enclosing '(' or -1 when none is found.
func (p *parser) findEnclosingParen(position int) int {
	return p.parenthesisScanIndex().EnclosingParen(position)
}

// extractColumnReferenceBeforeIN reads the column reference that precedes an IN keyword,
// which is the left-hand side of the IN predicate, stepping over the NOT of a NOT IN.
//
// Takes inPosition (int) which is the IN keyword's token index.
//
// Returns *ast.ColumnReference which is the LHS column, or nil when inPosition leaves no
// room for one.
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

// castTypeAround returns the type a CAST declares for the parameter at parameterPosition
// when the CAST converts the parameter alone, as in CAST($1 AS UUID), and nil otherwise.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns *sqlmodel.SQLType which is the declared type, or nil.
func (p *parser) castTypeAround(parameterPosition int) *sqlmodel.SQLType {
	open := p.findEnclosingParen(parameterPosition)
	if open < 1 || open != parameterPosition-1 || !p.Tokens[open-1].IsKeyword(tokenIdentifier, "CAST") ||
		parameterPosition+1 >= len(p.Tokens) || !p.Tokens[parameterPosition+1].IsKeyword(tokenIdentifier, keywordAS) {
		return nil
	}
	return p.extractCastType(parameterPosition)
}

// extractCastType finds the AS keyword after a CAST(...) parameter and returns the
// normalised target type.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns *sqlmodel.SQLType which is the cast type, or nil when no type follows AS.
func (p *parser) extractCastType(parameterPosition int) *sqlmodel.SQLType {
	asPosition := p.findASKeywordAfter(parameterPosition)
	if asPosition < 0 {
		return nil
	}
	typeNameStart := asPosition + 1
	if typeNameStart >= len(p.Tokens) || p.Tokens[typeNameStart].Kind != tokenIdentifier {
		return nil
	}
	typeName := p.collectCastTypeTokens(typeNameStart)
	return new(p.types.normalise(typeName))
}

// findASKeywordAfter searches forward from parameterPosition for an AS keyword, stopping
// at the next closing parenthesis.
//
// Takes parameterPosition (int) which is the search starting index.
//
// Returns int which is the AS keyword's token index, or -1 when none is found before the
// enclosing ')'.
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

// collectCastTypeTokens joins consecutive identifier tokens of a CAST target type into a
// single space-separated string.
//
// Takes startPosition (int) which is the index of the first identifier of the type name.
//
// Returns string which is the joined type name.
func (p *parser) collectCastTypeTokens(startPosition int) string {
	var builder strings.Builder
	builder.WriteString(p.Tokens[startPosition].Value)
	for scanIndex := startPosition + 1; scanIndex < len(p.Tokens); scanIndex++ {
		if p.Tokens[scanIndex].Kind != tokenIdentifier ||
			p.isKeywordAt(scanIndex, keywordFROM, keywordWHERE, keywordGROUP, keywordHAVING, keywordORDER, keywordLIMIT) {
			break
		}
		if p.Tokens[scanIndex].Kind == tokenRightParen {
			break
		}
		builder.WriteByte(' ')
		builder.WriteString(p.Tokens[scanIndex].Value)
	}
	return builder.String()
}

// isKeywordAt reports whether the token at position is an identifier matching any of the
// given keywords.
//
// Takes position (int) which is the token index to test.
// Takes keywords (...string) which is the upper-case keyword set to match against.
//
// Returns bool which is true when the token matches one of keywords.
func (p *parser) isKeywordAt(position int, keywords ...string) bool {
	if position >= len(p.Tokens) || p.Tokens[position].Kind != tokenIdentifier {
		return false
	}
	return slices.Contains(keywords, p.Tokens[position].Keyword(tokenIdentifier))
}

// extractColumnReference reads a bare or schema-qualified column reference at position.
// An identifier that follows the `::` cast operator names a type, not a column.
//
// Takes position (int) which is the identifier's token index.
//
// Returns *ast.ColumnReference which is the parsed reference, or nil when position is out
// of range or not an identifier.
func (p *parser) extractColumnReference(position int) *ast.ColumnReference {
	if position < 0 || position >= len(p.Tokens) {
		return nil
	}

	current := p.Tokens[position]
	if current.Kind != tokenIdentifier || (position >= 1 && p.Tokens[position-1].Kind == tokenCast) {
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

// extractColumnReferenceFromParenthesised resolves a column reference from within a
// parenthesised group ending at rightParenPosition.
//
// Takes rightParenPosition (int) which is the closing-parenthesis token index.
//
// Returns *ast.ColumnReference which is the first column found within the group, or nil
// when none can be identified.
func (p *parser) extractColumnReferenceFromParenthesised(rightParenPosition int) *ast.ColumnReference {
	leftParenPosition := p.findMatchingLeftParen(rightParenPosition)
	if leftParenPosition < 0 || p.opensSubquery(leftParenPosition) || p.extractsAStructField(leftParenPosition) {
		return nil
	}
	return p.scanForColumnReference(leftParenPosition+1, rightParenPosition)
}

// extractsAStructField reports whether the parenthesis at a position opens the arguments
// of struct_extract, whose value is a field of the struct it is given rather than the
// struct column, so a parameter compared with it takes the field's type from the
// expression and not the column's.
//
// Takes leftParenPosition (int) which is the `(` token index.
//
// Returns bool which is true for the arguments of an unqualified struct_extract call.
func (p *parser) extractsAStructField(leftParenPosition int) bool {
	if leftParenPosition < 1 || p.Tokens[leftParenPosition-1].Kind != tokenIdentifier ||
		!strings.EqualFold(p.Tokens[leftParenPosition-1].Value, functionStructExtract) {
		return false
	}
	return leftParenPosition < 2 || p.Tokens[leftParenPosition-2].Kind != tokenDot
}

// opensSubquery reports whether the parenthesis at a position opens a subquery, whose
// first word is SELECT, WITH or VALUES, rather than an expression a column stands in.
//
// Takes leftParenPosition (int) which is the `(` token index.
//
// Returns bool which is true for a subquery.
func (p *parser) opensSubquery(leftParenPosition int) bool {
	next := leftParenPosition + 1
	if next >= len(p.Tokens) {
		return false
	}
	return p.Tokens[next].IsKeyword(tokenIdentifier, keywordSELECT) || p.Tokens[next].IsKeyword(tokenIdentifier, keywordWITH) ||
		p.Tokens[next].IsKeyword(tokenIdentifier, keywordVALUES)
}

// findMatchingLeftParen walks back from rightParenPosition to find the matching opening
// parenthesis.
//
// Takes rightParenPosition (int) which is the closing-parenthesis token index.
//
// Returns int which is the matching '(' index, or -1 when nothing matches.
func (p *parser) findMatchingLeftParen(rightParenPosition int) int {
	depth := 0
	for index := rightParenPosition; index >= 0; index-- {
		switch p.Tokens[index].Kind {
		case tokenRightParen:
			depth++
		case tokenLeftParen:
			depth--
			if depth == 0 {
				return index
			}
		default:
		}
	}
	return -1
}

// scanForColumnReference scans tokens in [startPosition, endPosition) for the first
// plausible column reference.
//
// Takes startPosition (int) which is the inclusive start index.
// Takes endPosition (int) which is the exclusive end index.
//
// Returns *ast.ColumnReference which is the first column found, or nil when none is
// identified.
func (p *parser) scanForColumnReference(startPosition int, endPosition int) *ast.ColumnReference {
	for scanIndex := startPosition; scanIndex < endPosition; scanIndex++ {
		reference := p.extractColumnReference(scanIndex)
		if reference != nil {
			return reference
		}
	}
	return nil
}

// parseCastTypeName consumes a target type name following an inline cast, including
// schema qualifier, multi-word built-ins, parenthesised modifiers, and trailing array
// brackets.
//
// Returns string which is the canonical type spelling, empty when the current token is
// not an identifier.
func (p *parser) parseCastTypeName() string {
	if p.Current().Kind != tokenIdentifier {
		return ""
	}

	var builder strings.Builder
	builder.WriteString(p.Advance().Value)

	p.appendSchemaQualifier(&builder)
	p.appendMultiWordTypeKeywords(&builder)

	if p.Current().Kind == tokenLeftParen {
		builder.WriteString(p.parenthesisedTypeText())
	}

	p.appendTypeArrayBrackets(&builder)

	return builder.String()
}

// appendSchemaQualifier appends a ".name" suffix to builder when a schema-qualified
// continuation follows the current token.
//
// Takes builder (*strings.Builder) which receives the appended text.
func (p *parser) appendSchemaQualifier(builder *strings.Builder) {
	if p.Current().Kind == tokenDot && p.Peek().Kind == tokenIdentifier {
		p.Advance()
		builder.WriteByte('.')
		builder.WriteString(p.Advance().Value)
	}
}

// appendMultiWordTypeKeywords appends each consecutive multi-word type keyword to
// builder, separated by spaces.
//
// Takes builder (*strings.Builder) which receives the appended text.
func (p *parser) appendMultiWordTypeKeywords(builder *strings.Builder) {
	for p.Current().Kind == tokenIdentifier {
		if !isMultiWordTypeKeyword(p.Current().Keyword(tokenIdentifier)) {
			break
		}
		builder.WriteByte(' ')
		builder.WriteString(p.Advance().Value)
	}
}

// appendTypeArrayBrackets appends one array subscript suffix to builder per consumed []
// or [N] pair.
//
// Takes builder (*strings.Builder) which receives the appended text.
func (p *parser) appendTypeArrayBrackets(builder *strings.Builder) {
	for p.Current().Kind == tokenLeftBracket {
		p.Advance()
		if p.Current().Kind == tokenNumber {
			p.Advance()
		}
		if p.Current().Kind == tokenRightBracket {
			builder.WriteString(arraySubscriptSuffix)
			p.Advance()
		}
	}
}

// builtinResultType returns the result type of a built-in function whose overloads all
// return the same known type.
//
// Takes functions (*ast.FunctionCatalogue) which is the built-in catalogue, or nil.
// Takes name (string) which is the lower-cased function name.
//
// Returns *sqlmodel.SQLType which is the shared result type, or nil when the function is
// not a built-in or its overloads disagree.
func builtinResultType(functions *ast.FunctionCatalogue, name string) *sqlmodel.SQLType {
	if functions == nil {
		return nil
	}
	signatures := functions.Functions[name]
	if len(signatures) == 0 || signatures[0].ReturnType.Category == sqlmodel.TypeCategoryUnknown {
		return nil
	}
	resultType := signatures[0].ReturnType
	for _, signature := range signatures[1:] {
		if signature.ReturnType.Category != resultType.Category || signature.ReturnType.EngineName != resultType.EngineName {
			return nil
		}
	}
	return &resultType
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
	case "AS", "CAST", "TRY_CAST", "COLLATE", "DISTINCT", "ALL", "NULL", "TRUE", "FALSE",
		"INTEGER", "INT", "TINYINT", "SMALLINT", "BIGINT", "HUGEINT",
		"REAL", "FLOAT", "DOUBLE", "NUMERIC", "DECIMAL",
		"VARCHAR", "CHAR", "TEXT", "STRING", "BLOB",
		"BOOLEAN", "BOOL", "DATE", "TIME", "TIMESTAMP", "TIMESTAMPTZ",
		"INTERVAL", "JSON", "UUID",
		"ESCAPE", "BETWEEN", "IS", "IN", "EXISTS", "NOT", "ASC", "DESC":
		return true
	}
	return false
}

// isLikePatternKeyword reports whether a keyword introduces a DuckDB string pattern match
// (LIKE, ILIKE, or GLOB). DuckDB has no RLIKE or MATCH operator; SIMILAR TO is two tokens
// and is not detected here, and regex matching uses the ~ family of operators handled
// elsewhere.
//
// Takes keyword (string) which is the upper-case identifier value.
//
// Returns bool which is true for LIKE, ILIKE, or GLOB.
func isLikePatternKeyword(keyword string) bool {
	switch keyword {
	case "LIKE", "ILIKE", "GLOB":
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

// isFunctionCallName reports whether the identifier preceding a '(' names a callable
// function rather than introducing an IN list, a CAST, or a SELECT/WHERE subquery, so a
// parameter inside the parentheses can be treated as a function argument.
//
// Takes value (string) which is the identifier token value preceding the parenthesis.
//
// Returns bool which is true when the identifier names a function call.
func isFunctionCallName(value string) bool {
	switch strings.ToUpper(value) {
	case keywordIN, keywordCAST, keywordSELECT, keywordWHERE:
		return false
	}
	return true
}

// isComparisonOperator reports whether operator compares its operands to a boolean: the
// equality and ordering operators, the regular-expression and LIKE operators, ^@ (starts
// with), the list containment operators @> and <@, and && (overlaps).
//
// Takes operator (string) which is the operator literal.
//
// Returns bool which is true for a comparison operator.
func isComparisonOperator(operator string) bool {
	switch operator {
	case "=", "==", "<>", "!=", "<", ">", "<=", ">=",
		"~", "!~", "~*", "!~*", "~~", "!~~", "~~*", "!~~*", "^@", "@>", "<@", "&&":
		return true
	}
	return false
}

// isArithmeticOperator reports whether operator is a SQL arithmetic operator spelling
// other than multiplication.
//
// Takes operator (string) which is the operator literal.
//
// Returns bool which is true for +, -, /, or %.
func isArithmeticOperator(operator string) bool {
	switch operator {
	case "+", "-", "/", "%":
		return true
	}
	return false
}

// isMultiWordTypeKeyword reports whether upper is one of the keyword continuations
// recognised in multi-word built-in type names.
//
// Takes upper (string) which is the candidate keyword upper-cased.
//
// Returns bool which is true for keywords such as VARYING, PRECISION, or ZONE.
func isMultiWordTypeKeyword(upper string) bool {
	switch upper {
	case "VARYING", "PRECISION", "WITHOUT", keywordWITH, keywordTIME, keywordZONE,
		"CHARACTER", "DOUBLE", "TIMESTAMP", "INTERVAL":
		return true
	}
	return false
}

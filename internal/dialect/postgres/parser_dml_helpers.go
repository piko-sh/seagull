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
	"slices"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

// colonEqualsNameOffset is how many tokens before a parameter passed as `name := $1` the
// name sits: the ":" and "=" come between.
const colonEqualsNameOffset = 3

// nonFunctionCallers lists the keywords a parenthesised group follows that open no
// function call, so a parameter inside the group is not a function argument.
var nonFunctionCallers = map[string]struct{}{
	"IN": {}, keywordCAST: {}, keywordSELECT: {}, keywordWHERE: {}, "ANY": {}, keywordALL: {}, "SOME": {},
	keywordEXISTS: {}, "ARRAY": {}, keywordROW: {}, keywordVALUES: {}, "OVER": {}, "FILTER": {}, keywordUSING: {},
	keywordON: {}, keywordAND: {}, "OR": {}, keywordNOT: {}, "WHEN": {}, "THEN": {}, "ELSE": {},
}

// predicateArgument is where a parameter a predicate scan meets sits in a function call.
type predicateArgument struct {
	// function is the called function's canonical name, schema-qualified as written.
	function string

	// name is the parameter's name in the call when the argument is passed by name, as in
	// f(factor => $1), and empty otherwise.
	name string

	// ordinal is the zero-based position of the argument among the call's arguments.
	ordinal int
}

// handleParameterInExpression registers the current parameter token, resolving its
// context from preceding tokens and consuming any inline cast suffix.
func (p *parser) handleParameterInExpression() {
	parameterPosition := p.Position
	parameterToken := p.Current()

	argument, isArgument := p.functionArgumentAt(parameterPosition)
	context, columnReference, castType := p.resolveParameterContext(parameterPosition)
	if isArgument && argument.name != "" {
		context, columnReference = sqlmodel.ParameterContextFunctionArgument, nil
	}

	p.Advance()

	if p.Current().Kind == tokenCast {
		castType = p.consumeInlineCast()
		if context == sqlmodel.ParameterContextUnknown {
			context = sqlmodel.ParameterContextCast
		}
	}

	number := p.registerParameterFromToken(parameterToken, context, columnReference, castType)
	if isArgument && context == sqlmodel.ParameterContextFunctionArgument {
		reference := &p.parameterReferences[len(p.parameterReferences)-1]
		reference.EnclosingFunctionName = argument.function
		reference.ArgumentOrdinal = argument.ordinal
		if argument.name != "" {
			p.recordArgumentName(number, argument.name)
		}
	}
}

// functionArgumentAt reports the function call a parameter is an argument of, as the
// expression parser does for a call it parses: the call whose parentheses directly
// enclose the parameter, the argument's position among the call's arguments, and the name
// the argument is passed by, if any.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns predicateArgument which locates the argument.
// Returns bool which is false when the enclosing parentheses open no function call.
func (p *parser) functionArgumentAt(parameterPosition int) (predicateArgument, bool) {
	open := p.findEnclosingParen(parameterPosition)
	if open < 1 || p.Tokens[open-1].Kind != tokenIdentifier {
		return predicateArgument{}, false
	}
	if _, keyword := nonFunctionCallers[p.Tokens[open-1].Keyword(tokenIdentifier)]; keyword {
		return predicateArgument{}, false
	}
	nameStart := open - 1
	for nameStart >= 2 && p.Tokens[nameStart-1].Kind == tokenDot && p.Tokens[nameStart-2].Kind == tokenIdentifier {
		nameStart -= 2
	}
	parts := make([]string, 0, open-nameStart)
	for index := nameStart; index < open; index += 2 {
		parts = append(parts, p.Tokens[index].Value)
	}
	return predicateArgument{
		function: strings.Join(parts, "."),
		name:     p.argumentNameBefore(parameterPosition),
		ordinal:  p.argumentOrdinal(open, parameterPosition),
	}, true
}

// argumentOrdinal counts the commas outside every nested group between a call's opening
// parenthesis and a position inside it.
//
// Takes open (int) which is the index of the call's "(".
// Takes position (int) which is the index inside the call.
//
// Returns int which is the zero-based argument position.
func (p *parser) argumentOrdinal(open, position int) int {
	depth, ordinal := 0, 0
	for index := open + 1; index < position; index++ {
		kind := p.Tokens[index].Kind
		switch {
		case kind == tokenLeftParen || kind == tokenLeftBracket:
			depth++
		case kind == tokenRightParen || kind == tokenRightBracket:
			depth--
		case kind == tokenComma && depth == 0:
			ordinal++
		}
	}
	return ordinal
}

// argumentNameBefore returns the name an argument is passed by when a parameter directly
// follows `name =>` or `name :=`.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns string which is the name, or empty for a positional argument.
func (p *parser) argumentNameBefore(parameterPosition int) string {
	switch {
	case parameterPosition >= 2 && p.Tokens[parameterPosition-1].Kind == tokenOperator && p.Tokens[parameterPosition-1].Value == "=>" &&
		p.Tokens[parameterPosition-2].Kind == tokenIdentifier:
		return p.Tokens[parameterPosition-2].Value
	case parameterPosition >= colonEqualsNameOffset && p.Tokens[parameterPosition-1].Kind == tokenOperator &&
		p.Tokens[parameterPosition-1].Value == "=" && p.Tokens[parameterPosition-2].Kind == tokenOperator &&
		p.Tokens[parameterPosition-2].Value == ":" && p.Tokens[parameterPosition-2].End == p.Tokens[parameterPosition-1].Start &&
		p.Tokens[parameterPosition-colonEqualsNameOffset].Kind == tokenIdentifier:
		return p.Tokens[parameterPosition-colonEqualsNameOffset].Value
	default:
		return ""
	}
}

// comparedCallType finds the type of the call a parameter is compared with, for parameter
// inference: a built-in function's result type, or the type a CAST converts to, since
// PostgreSQL types the parameter as the value it is compared with, not as a column the
// call reads.
//
// It returns nil for a function unless all matching overloads have one known result type.
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
	if p.Tokens[open-1].IsKeyword(tokenIdentifier, keywordCAST) {
		return p.castTargetType(open, parameterPosition-2)
	}
	if _, keyword := nonFunctionCallers[p.Tokens[open-1].Keyword(tokenIdentifier)]; keyword {
		return nil
	}
	return builtinResultType(p.functions, strings.ToLower(p.Tokens[open-1].Value))
}

// castTargetType returns the type a CAST(expression AS type) converts to: the type named
// after the last AS its parentheses hold outside any nested parentheses.
//
// Takes open (int) which is the index of the CAST's `(`.
// Takes closing (int) which is the index of its `)`.
//
// Returns *sqlmodel.SQLType which is the normalised type, or nil when no AS and type name
// are found.
func (p *parser) castTargetType(open, closing int) *sqlmodel.SQLType {
	depth, asPosition := 0, -1
	for index := open + 1; index < closing; index++ {
		switch {
		case p.Tokens[index].Kind == tokenLeftParen:
			depth++
		case p.Tokens[index].Kind == tokenRightParen:
			depth--
		case depth == 0 && p.Tokens[index].IsKeyword(tokenIdentifier, keywordAS):
			asPosition = index
		}
	}
	if asPosition < 0 || asPosition+1 >= closing || p.Tokens[asPosition+1].Kind != tokenIdentifier {
		return nil
	}
	return new(p.types.normalise(p.collectCastTypeTokens(asPosition + 1)))
}

// resolveParameterContext resolves the syntactic context of a parameter at
// parameterPosition by consulting preceding operators, LIKE scope, and enclosing parens.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns sqlmodel.ParameterContext which is the inferred context.
// Returns *ast.ColumnReference which is the inferred column, or nil when none was found.
// Returns *sqlmodel.SQLType which is the inferred cast type, or nil when none was found.
func (p *parser) resolveParameterContext(parameterPosition int) (sqlmodel.ParameterContext, *ast.ColumnReference, *sqlmodel.SQLType) {
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

// resolveLikeContext resolves a parameter sitting inside a LIKE or ILIKE pattern,
// returning its LHS column reference.
//
// Uses the cached parenthesis index to find an enclosing LIKE or ILIKE operator within
// the current predicate and returns its LHS column reference, falling through to a wider
// scan of the LHS expression when the immediate-left token is not itself a column.
// Postgres' regex-match operators (~, ~*, !~, !~*) are handled via the
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
	if columnPosition >= 0 && p.Tokens[columnPosition].IsKeyword(tokenIdentifier, keywordNOT) {
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
		candidate := p.Tokens[index]
		switch candidate.Kind {
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
		if isLikeBoundaryKeyword(candidate.Keyword(tokenIdentifier)) {
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
		candidate := p.Tokens[index]
		if candidate.Kind != tokenIdentifier {
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

// resolveContextFromPrecedingOperator inspects the operator immediately before
// parameterPosition and infers a parameter context from a comparison, an arithmetic
// operator or an operator whose operands are of one type (oneTypeOperators).
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns sqlmodel.ParameterContext which is the inferred context, or
// ParameterContextUnknown when nothing matched.
// Returns *ast.ColumnReference which is the column appearing before the operator, or nil
// when none was found.
func (p *parser) resolveContextFromPrecedingOperator(parameterPosition int) (sqlmodel.ParameterContext, *ast.ColumnReference) {
	if parameterPosition < 2 {
		return sqlmodel.ParameterContextUnknown, nil
	}

	previousToken := p.Tokens[parameterPosition-1]
	beforeOperator := parameterPosition - 2

	if previousToken.Kind == tokenOperator && isComparisonOperator(previousToken.Value) {
		columnReference := p.extractColumnReferenceOrParenthesised(beforeOperator)
		if columnReference != nil {
			return sqlmodel.ParameterContextComparison, columnReference
		}
	}

	return p.resolvePartnerOperandContext(parameterPosition)
}

// resolvePartnerOperandContext uses a preceding column and arithmetic or same-type
// operator to infer a parameter's comparison context. PostgreSQL gives the parameter the
// column's type.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns sqlmodel.ParameterContext which is ParameterContextComparison when such an
// operator and column precede the parameter, or ParameterContextUnknown.
// Returns *ast.ColumnReference which is the column before the operator, or nil.
func (p *parser) resolvePartnerOperandContext(parameterPosition int) (sqlmodel.ParameterContext, *ast.ColumnReference) {
	if parameterPosition < 2 {
		return sqlmodel.ParameterContextUnknown, nil
	}
	previousToken := p.Tokens[parameterPosition-1]
	if previousToken.Kind == tokenStar || (previousToken.Kind == tokenOperator &&
		(isArithmeticOperator(previousToken.Value) || slices.Contains(oneTypeOperators, previousToken.Value))) {
		columnReference := p.extractColumnReference(parameterPosition - 2)
		if columnReference != nil {
			return sqlmodel.ParameterContextComparison, columnReference
		}
	}
	return sqlmodel.ParameterContextUnknown, nil
}

// extractColumnReferenceOrParenthesised extracts a column reference at position, falling
// back to scanning the enclosing parenthesised expression when position points at a `)`.
//
// Takes position (int) which is the token index to inspect.
//
// Returns *ast.ColumnReference which is the resolved column, or nil when none was found.
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

// consumeInlineCast consumes a `::type` cast suffix and returns the normalised SQL type.
//
// Returns *sqlmodel.SQLType which is the normalised cast type.
func (p *parser) consumeInlineCast() *sqlmodel.SQLType {
	p.Advance()
	typeName := p.parseCastTypeName()
	return new(p.types.normalise(typeName))
}

// detectParameterContext infers a parameter context by inspecting the keyword that
// introduces the enclosing parenthesised group, such as IN, CAST, or a function name.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns sqlmodel.ParameterContext which is the inferred context.
// Returns *ast.ColumnReference which is the inferred column, or nil when none was found.
// Returns *sqlmodel.SQLType which is the inferred cast type, or nil when none applies.
func (p *parser) detectParameterContext(parameterPosition int) (sqlmodel.ParameterContext, *ast.ColumnReference, *sqlmodel.SQLType) {
	enclosingParen := p.findEnclosingParen(parameterPosition)
	if enclosingParen < 0 {
		return sqlmodel.ParameterContextUnknown, nil, nil
	}

	if enclosingParen >= 2 && p.Tokens[enclosingParen-1].IsKeyword(tokenIdentifier, "IN") {
		columnReference := p.extractColumnReferenceBeforeIN(enclosingParen - 1)
		return sqlmodel.ParameterContextInList, columnReference, nil
	}

	if enclosingParen >= 1 && p.Tokens[enclosingParen-1].IsKeyword(tokenIdentifier, keywordCAST) {
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

// findEnclosingParen finds the index of the `(` that opens the parenthesised group
// enclosing position.
//
// Takes position (int) which is the token index to inspect.
//
// Returns int which is the enclosing `(` index, or -1 when position is not inside any
// group.
func (p *parser) findEnclosingParen(position int) int {
	return p.parenthesisScanIndex().EnclosingParen(position)
}

// extractColumnReferenceBeforeIN extracts the column reference before an IN keyword,
// which is the LHS of the IN predicate, stepping over the NOT of a NOT IN.
//
// Takes inPosition (int) which is the IN keyword's token index.
//
// Returns *ast.ColumnReference which is the LHS column, or nil when none was found.
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
// when the CAST converts the parameter alone, as in CAST($1 AS inet), and nil otherwise.
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

// extractCastType locates an AS keyword following a CAST parameter and returns the
// normalised target type.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns *sqlmodel.SQLType which is the cast target type, or nil when no AS or type
// tokens were found.
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

// findASKeywordAfter locates the index of an AS keyword that follows parameterPosition
// without crossing a `)`.
//
// Takes parameterPosition (int) which is the parameter's token index.
//
// Returns int which is the AS token index, or -1 when none was found.
func (p *parser) findASKeywordAfter(parameterPosition int) int {
	for index := parameterPosition + 1; index < len(p.Tokens); index++ {
		if p.Tokens[index].IsKeyword(tokenIdentifier, keywordAS) {
			return index
		}
		if p.Tokens[index].Kind == tokenRightParen {
			break
		}
	}
	return -1
}

// collectCastTypeTokens joins the tokens of a type name starting at startPosition into a
// possibly schema-qualified, multi-word type name with its modifier list, stopping at a
// closing paren, an array bracket or a major clause keyword.
//
// A quoted word is written in double quotes and the modifiers as the list (10,2), so the
// type normaliser reads both.
//
// Takes startPosition (int) which is the first type token's index.
//
// Returns string which is the joined type name.
func (p *parser) collectCastTypeTokens(startPosition int) string {
	var builder strings.Builder
	writeTypeWord(&builder, p.Tokens[startPosition])
	for index := startPosition + 1; index < len(p.Tokens); index++ {
		switch current := p.Tokens[index]; {
		case current.Kind == tokenDot && index+1 < len(p.Tokens) && p.Tokens[index+1].Kind == tokenIdentifier:
			index++
			builder.WriteByte('.')
			writeTypeWord(&builder, p.Tokens[index])
		case current.Kind == tokenLeftParen:
			closing := p.findMatchingRightParen(index)
			if closing < 0 {
				return builder.String()
			}
			writeTypeModifiers(&builder, readTypeModifiers(p.Tokens[index+1:closing]))
			index = closing
		case current.Kind == tokenIdentifier &&
			!p.isKeywordAt(index, keywordFROM, keywordWHERE, keywordGROUP, keywordHAVING, keywordORDER, keywordLIMIT):
			builder.WriteByte(' ')
			writeTypeWord(&builder, current)
		default:
			return builder.String()
		}
	}
	return builder.String()
}

// isKeywordAt reports whether the token at position is an identifier whose upper-case
// value matches one of keywords.
//
// Takes position (int) which is the token index to inspect.
// Takes keywords (...string) which is the list of keywords to test.
//
// Returns bool which is true when the token matches one of them.
func (p *parser) isKeywordAt(position int, keywords ...string) bool {
	if position >= len(p.Tokens) {
		return false
	}
	keyword := p.Tokens[position].Keyword(tokenIdentifier)
	return keyword != "" && slices.Contains(keywords, keyword)
}

// extractColumnReference reads a bare or alias-qualified column at position. It excludes
// type names following casts, composite-field names and identifiers qualifying the
// following name.
//
// Takes position (int) which is the column identifier's token index.
//
// Returns *ast.ColumnReference which is the parsed reference, or nil when position does
// not point at an identifier.
func (p *parser) extractColumnReference(position int) *ast.ColumnReference {
	if position < 0 || position >= len(p.Tokens) {
		return nil
	}

	candidate := p.Tokens[position]
	if candidate.Kind != tokenIdentifier || (position >= 1 && p.Tokens[position-1].Kind == tokenCast) {
		return nil
	}
	if position+1 < len(p.Tokens) && p.Tokens[position+1].Kind == tokenDot {
		return nil
	}
	if position >= 2 && p.Tokens[position-1].Kind == tokenDot && p.Tokens[position-2].Kind == tokenRightParen {
		return nil
	}

	if position >= 2 && p.Tokens[position-1].Kind == tokenDot && p.Tokens[position-2].Kind == tokenIdentifier {
		return &ast.ColumnReference{
			TableAlias: p.Tokens[position-2].Value,
			ColumnName: candidate.Value,
		}
	}

	return &ast.ColumnReference{
		ColumnName: candidate.Value,
		TableAlias: "",
	}
}

// extractColumnReferenceFromParenthesised resolves a column reference inside the
// parenthesised group whose closing token is at rightParenPosition.
//
// Takes rightParenPosition (int) which is the `)` token index.
//
// Returns *ast.ColumnReference which is the resolved column, or nil when none was found.
func (p *parser) extractColumnReferenceFromParenthesised(rightParenPosition int) *ast.ColumnReference {
	leftParenPosition := p.findMatchingLeftParen(rightParenPosition)
	if leftParenPosition < 0 || p.opensQueryAt(leftParenPosition) {
		return nil
	}
	return p.scanForColumnReference(leftParenPosition+1, rightParenPosition)
}

// findMatchingLeftParen locates the matching `(` for the `)` at rightParenPosition.
//
// Takes rightParenPosition (int) which is the `)` token index.
//
// Returns int which is the matching `(` index, or -1 when unmatched.
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

// findMatchingRightParen locates the matching `)` for the `(` at leftParenPosition.
//
// Takes leftParenPosition (int) which is the `(` token index.
//
// Returns int which is the matching `)` index, or -1 when unmatched.
func (p *parser) findMatchingRightParen(leftParenPosition int) int {
	depth := 0
	for index := leftParenPosition; index < len(p.Tokens); index++ {
		switch p.Tokens[index].Kind {
		case tokenLeftParen:
			depth++
		case tokenRightParen:
			depth--
			if depth == 0 {
				return index
			}
		default:
		}
	}
	return -1
}

// scanForColumnReference scans forward from startPosition for the first column reference
// before endPosition.
//
// Takes startPosition (int) which is the inclusive start of the scan.
// Takes endPosition (int) which is the exclusive end of the scan.
//
// Returns *ast.ColumnReference which is the first column reference, or nil when none was
// found.
func (p *parser) scanForColumnReference(startPosition int, endPosition int) *ast.ColumnReference {
	for index := startPosition; index < endPosition; index++ {
		reference := p.extractColumnReference(index)
		if reference != nil {
			return reference
		}
	}
	return nil
}

// parseCastTypeName parses a `::type` cast target, or the type a column definition list
// gives a column: an identifier, an optional schema qualifier, multi-word type keywords,
// a modifier list, a time zone qualifier after it, and array brackets.
//
// A quoted word is written in double quotes and the modifiers after the name, as in
// numeric(10,2), so the type normaliser reads both.
//
// Returns string which is the type name, or empty when no identifier starts it.
func (p *parser) parseCastTypeName() string {
	var builder strings.Builder
	if !p.appendTypeNameWords(&builder) {
		return ""
	}

	p.appendTypeModifiers(&builder)
	p.appendTimeZoneSuffix(&builder)
	p.appendTypeArrayBrackets(&builder)

	return builder.String()
}

// appendTypeNameWords consumes the words of a type name, its first word, an optional
// schema qualifier and any multi-word type keywords, and appends them to builder.
//
// Takes builder (*strings.Builder) which is the type-name accumulator.
//
// Returns bool which is false when the current token is not an identifier, leaving it
// unconsumed.
func (p *parser) appendTypeNameWords(builder *strings.Builder) bool {
	if p.Current().Kind != tokenIdentifier {
		return false
	}

	writeTypeWord(builder, p.Advance())
	p.appendSchemaQualifier(builder)
	p.appendMultiWordTypeKeywords(builder)
	return true
}

// appendTypeModifiers consumes a parenthesised modifier list after a type name, reading
// the modifiers it holds as readTypeModifiers does, and appends them as the list (10,2).
// The list is skipped as a balanced group, recording a syntax error when it is not
// closed.
//
// Takes builder (*strings.Builder) which is the type-name accumulator.
func (p *parser) appendTypeModifiers(builder *strings.Builder) {
	if p.Current().Kind != tokenLeftParen {
		return
	}
	open := p.Position
	_ = p.requireSkipParenthesised()
	writeTypeModifiers(builder, readTypeModifiers(p.Tokens[open+1:p.Position]))
}

// appendTimeZoneSuffix consumes a WITH TIME ZONE or WITHOUT TIME ZONE qualifier written
// after a type's modifier list, as in timestamp(3) with time zone, and appends it.
//
// Takes builder (*strings.Builder) which is the type-name accumulator.
func (p *parser) appendTimeZoneSuffix(builder *strings.Builder) {
	builder.WriteString(p.consumeTemporalZoneSuffix(""))
}

// appendSchemaQualifier consumes a `.identifier` schema qualifier after a type name and
// appends it to builder.
//
// Takes builder (*strings.Builder) which is the type-name accumulator.
func (p *parser) appendSchemaQualifier(builder *strings.Builder) {
	if p.Current().Kind == tokenDot && p.Peek().Kind == tokenIdentifier {
		p.Advance()
		builder.WriteByte('.')
		writeTypeWord(builder, p.Advance())
	}
}

// appendMultiWordTypeKeywords consumes trailing identifiers that form a multi-word type
// name (e.g. `DOUBLE PRECISION`) and appends them to builder.
//
// Takes builder (*strings.Builder) which is the type-name accumulator.
func (p *parser) appendMultiWordTypeKeywords(builder *strings.Builder) {
	for p.Current().Kind == tokenIdentifier {
		if !isMultiWordTypeKeyword(p.Current().Keyword(tokenIdentifier)) {
			break
		}
		builder.WriteByte(' ')
		builder.WriteString(p.Advance().Value)
	}
}

// appendTypeArrayBrackets consumes any `[N]` or `[]` array suffixes following a type name
// and appends the array marker to builder.
//
// Takes builder (*strings.Builder) which is the type-name accumulator.
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
	candidate := tokens[position]
	if candidate.Kind != tokenIdentifier {
		return false
	}
	if position+1 < len(tokens) && tokens[position+1].Kind == tokenLeftParen {
		return false
	}
	if position+1 < len(tokens) && tokens[position+1].Kind == tokenDot {
		return false
	}
	if position > 0 && tokens[position-1].IsKeyword(tokenIdentifier, keywordAS) {
		return false
	}
	upper := candidate.Keyword(tokenIdentifier)
	if upper == "" {
		return true
	}
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
		"INTEGER", "INT", "INT2", "INT4", "INT8", "BIGINT", "SMALLINT",
		"REAL", "FLOAT", "DOUBLE", "NUMERIC", "DECIMAL",
		"TEXT", "VARCHAR", "CHAR", "CHARACTER", "BYTEA",
		"BOOLEAN", "BOOL", "DATE", "TIME", "TIMESTAMP", "TIMESTAMPTZ",
		"INTERVAL", "JSON", "JSONB", "UUID",
		"ESCAPE", "BETWEEN", "IS", "IN", "EXISTS", "NOT", "ASC", "DESC":
		return true
	}
	return false
}

// isLikePatternKeyword reports whether a keyword introduces a Postgres string pattern
// match (LIKE or ILIKE). Postgres has no GLOB, RLIKE, or MATCH operator; its regex
// operators (~, ~*, !~, !~*) are tokenised as operators and handled elsewhere.
//
// Takes keyword (string) which is the upper-case identifier value.
//
// Returns bool which is true for LIKE or ILIKE.
func isLikePatternKeyword(keyword string) bool {
	switch keyword {
	case "LIKE", "ILIKE":
		return true
	}
	return false
}

// isLikeBoundaryKeyword reports whether a keyword ends the predicate that could contain a
// LIKE pattern, so the backward walk should stop. It defers to the shared clause-boundary
// keyword set so every engine recognises the same boundaries.
//
// Takes keyword (string) which is the upper-case identifier value.
//
// Returns bool which is true at any clause or boolean boundary.
func isLikeBoundaryKeyword(keyword string) bool {
	return dialectkit.IsClauseBoundaryKeyword(keyword)
}

// isComparisonOperator reports whether operator is one of the SQL comparison operators.
//
// Takes operator (string) which is the operator value to test.
//
// Returns bool which is true for `=`, `<>`, `!=`, `<`, `>`, `<=`, or `>=`.
func isComparisonOperator(operator string) bool {
	switch operator {
	case "=", "<>", "!=", "<", ">", "<=", ">=":
		return true
	}
	return false
}

// isPredicateOperator reports whether operator is one of PostgreSQL's boolean operators
// other than the comparisons: pattern matching (`~`, `~*`, `!~`, `!~*`), containment
// (`@>`, `<@`), overlap (`&&`), text search (`@@`) and the jsonb key and path tests (`?`,
// `?|`, `?&`, `@?`).
//
// Takes operator (string) which is the operator value to test.
//
// Returns bool which is true for those operators.
func isPredicateOperator(operator string) bool {
	switch operator {
	case "~", "~*", "!~", "!~*", "@>", "<@", "&&", "@@", "?", "?|", "?&", "@?":
		return true
	}
	return false
}

// isGenericBinaryOperator reports whether operator binds at the level of the bitwise
// operators: any operator symbol that is not a comparison, a predicate operator, an
// arithmetic or concatenation operator, the slice colon, or the argument-name arrow.
//
// Takes operator (string) which is the operator value to test.
//
// Returns bool which is true for the bitwise, exponent and other operators.
func isGenericBinaryOperator(operator string) bool {
	if isComparisonOperator(operator) || isPredicateOperator(operator) || isArithmeticOperator(operator) {
		return false
	}
	switch operator {
	case "||", ":", "=>":
		return false
	}
	return true
}

// isArithmeticOperator reports whether operator is a basic arithmetic operator.
//
// Takes operator (string) which is the operator value to test.
//
// Returns bool which is true for `+`, `-`, `/`, or `%`.
func isArithmeticOperator(operator string) bool {
	switch operator {
	case "+", "-", "/", "%":
		return true
	}
	return false
}

// isMultiWordTypeKeyword reports whether upper is the second or later word in a
// multi-word SQL type name.
//
// Takes upper (string) which is the upper-case identifier value.
//
// Returns bool which is true when the value belongs to a multi-word type form.
func isMultiWordTypeKeyword(upper string) bool {
	switch upper {
	case "VARYING", "PRECISION", "WITHOUT", keywordWITH, keywordTIME, keywordZONE,
		"CHARACTER", "DOUBLE", "TIMESTAMP", "INTERVAL":
		return true
	}
	return false
}

// writeTypeWord appends one word of a type name to builder as SQL: an unquoted word as
// the tokeniser folded it, and a quoted word in double quotes, each double quote it holds
// doubled, so the type normaliser keeps its letters.
//
// Takes builder (*strings.Builder) which is the type-name accumulator.
// Takes word (token) which is the word.
func writeTypeWord(builder *strings.Builder, word token) {
	if !word.Quoted {
		builder.WriteString(word.Value)
		return
	}
	builder.WriteByte('"')
	builder.WriteString(strings.ReplaceAll(word.Value, `"`, `""`))
	builder.WriteByte('"')
}

// writeTypeModifiers appends a type's modifiers to builder as the list (10,2), or nothing
// when there are none.
//
// Takes builder (*strings.Builder) which is the type-name accumulator.
// Takes modifiers ([]int) which are the modifiers in order.
func writeTypeModifiers(builder *strings.Builder, modifiers []int) {
	if len(modifiers) == 0 {
		return
	}
	builder.WriteByte('(')
	for index, modifier := range modifiers {
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(strconv.Itoa(modifier))
	}
	builder.WriteByte(')')
}

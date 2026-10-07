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
	"errors"
	"math"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

var (
	// implicitFunctionIdentifiers names SQL identifiers that act as zero-argument function
	// calls without trailing parentheses.
	implicitFunctionIdentifiers = map[string]struct{}{
		"CURRENT_TIMESTAMP": {}, "CURRENT_DATE": {}, "CURRENT_TIME": {},
		"CURRENT_USER": {}, "SESSION_USER": {}, "LOCALTIME": {},
		"LOCALTIMESTAMP": {}, "CURRENT_ROLE": {}, "CURRENT_CATALOG": {},
		"CURRENT_SCHEMA": {},
	}

	// intervalFieldKeywords lists the field keywords that may follow an INTERVAL literal's
	// value.
	intervalFieldKeywords = map[string]struct{}{
		"YEAR": {}, "MONTH": {}, "DAY": {},
		"HOUR": {}, "MINUTE": {}, "SECOND": {},
		"TO": {},
	}
)

// parseExpression parses a full SQL expression at the current position.
//
// Bounds parenthesis-driven recursion. A stack overflow here is a fatal, non-recoverable
// Go error, so the cap (not the engine's recover guard) is what keeps deeply nested input
// from crashing the host. On overflow it records an error wrapping
// dialectkit.ErrParseDepth, which fails the statement, and returns an unknown expression
// rather than descending further.
//
// Returns ast.Expression which is the parsed expression.
func (p *parser) parseExpression() ast.Expression {
	if !p.enterExpression() {
		return &ast.UnknownExpression{}
	}
	defer p.leaveExpression()
	return p.parseOrExpression()
}

// enterExpression counts one more level of expression nesting. Once the nesting reaches
// the parser's depth limit it records an error wrapping dialectkit.ErrParseDepth, which
// fails the statement, and from then on refuses every expression of the statement.
//
// Returns bool which is false when the limit is or was reached, so the caller must not
// descend further; a caller given true must call leaveExpression when it returns.
func (p *parser) enterExpression() bool {
	if p.syntax.depthReached {
		return false
	}
	if p.expressionDepth >= p.maxParseDepth {
		p.syntax.depthReached = true
		_ = p.recordSyntaxError(dialectkit.DepthError(p.maxParseDepth))
		return false
	}
	p.expressionDepth++
	return true
}

// leaveExpression ends a level of expression nesting enterExpression counted.
func (p *parser) leaveExpression() {
	p.expressionDepth--
}

// parseOrExpression parses a left-associative OR chain.
//
// Returns ast.Expression which is the OR expression, or the single operand when no OR
// follows.
func (p *parser) parseOrExpression() ast.Expression {
	left := p.parseAndExpression()
	for p.MatchKeyword("OR") {
		right := p.parseAndExpression()
		left = &ast.LogicalOpExpression{
			Operator: "OR",
			Operands: []ast.Expression{left, right},
		}
	}
	return left
}

// parseAndExpression parses a left-associative AND chain.
//
// Returns ast.Expression which is the AND expression, or the single operand when no AND
// follows.
func (p *parser) parseAndExpression() ast.Expression {
	left := p.parseNotExpression()
	for p.MatchKeyword(keywordAND) {
		right := p.parseNotExpression()
		left = &ast.LogicalOpExpression{
			Operator: "AND",
			Operands: []ast.Expression{left, right},
		}
	}
	return left
}

// parseNotExpression parses an optional leading NOT followed by a comparison expression.
//
// Returns ast.Expression which is the negated or bare comparison expression.
func (p *parser) parseNotExpression() ast.Expression {
	if p.MatchKeyword(keywordNOT) {
		operand := p.parseComparisonExpression()
		return &ast.UnaryOpExpression{
			Operator: keywordNOT,
			Operand:  operand,
		}
	}
	return p.parseComparisonExpression()
}

// parseComparisonExpression parses a comparison, IS, IN, BETWEEN, LIKE, or SIMILAR TO
// expression and any leading NOT.
//
// Returns ast.Expression which is the comparison expression.
func (p *parser) parseComparisonExpression() ast.Expression {
	left := p.parseBitwiseExpression()

	if p.Current().Kind == tokenOperator && (isComparisonOperator(p.Current().Value) || isPredicateOperator(p.Current().Value)) {
		return p.parseComparisonOperator(left)
	}

	if p.IsKeyword("IS") {
		return p.parseIsSuffix(left)
	}

	if p.IsKeyword("ISNULL", "NOTNULL") {
		negated := p.Advance().Keyword(tokenIdentifier) == "NOTNULL"
		return &ast.IsNullExpression{Inner: left, Negated: negated}
	}

	if p.MatchKeyword("OVERLAPS") {
		right := p.parseBitwiseExpression()
		return &ast.ComparisonExpression{Operator: "OVERLAPS", Left: left, Right: right, Quantifier: ""}
	}

	notNegated := p.MatchKeyword(keywordNOT)

	expression := p.parsePostfixComparisonSuffix(left)
	if expression != nil {
		return p.maybeNegate(notNegated, expression)
	}

	if notNegated {
		return &ast.UnaryOpExpression{Operator: keywordNOT, Operand: left}
	}

	return left
}

// parsePostfixComparisonSuffix dispatches to the IN, BETWEEN, LIKE, or SIMILAR TO suffix
// parser when one of those keywords follows.
//
// Takes left (ast.Expression) which is the left-hand side expression.
//
// Returns ast.Expression which is the wrapped expression, or nil when no postfix keyword
// follows.
func (p *parser) parsePostfixComparisonSuffix(left ast.Expression) ast.Expression {
	if p.IsKeyword("IN") {
		return p.parseInListSuffix(left)
	}
	if p.IsKeyword("BETWEEN") {
		return p.parseBetweenSuffix(left)
	}
	if p.IsKeyword("LIKE") || p.IsKeyword("ILIKE") {
		return p.parseLikeSuffix(left)
	}
	if p.IsKeyword("SIMILAR") {
		return p.parseSimilarToSuffix(left)
	}
	return nil
}

// maybeNegate wraps expression in a NOT when negated is true.
//
// Takes negated (bool) which signals whether negation applies.
// Takes expression (ast.Expression) which is the inner expression.
//
// Returns ast.Expression which is the negated or original expression.
func (*parser) maybeNegate(negated bool, expression ast.Expression) ast.Expression {
	if negated {
		return &ast.UnaryOpExpression{Operator: keywordNOT, Operand: expression}
	}
	return expression
}

// parseLikeSuffix parses the right-hand side of a LIKE or ILIKE comparison, including any
// ESCAPE clause.
//
// Takes left (ast.Expression) which is the LIKE expression's left-hand side.
//
// Returns ast.Expression which is the LIKE comparison.
func (p *parser) parseLikeSuffix(left ast.Expression) ast.Expression {
	keyword := strings.ToUpper(p.Advance().Value)
	right := p.parseBitwiseExpression()
	if p.MatchKeyword("ESCAPE") {
		p.parseBitwiseExpression()
	}
	return &ast.ComparisonExpression{Operator: keyword, Left: left, Right: right, Quantifier: ""}
}

// parseSimilarToSuffix parses the right-hand side of a SIMILAR TO comparison, including
// any ESCAPE clause.
//
// Takes left (ast.Expression) which is the LHS of the comparison.
//
// Returns ast.Expression which is the SIMILAR TO comparison.
func (p *parser) parseSimilarToSuffix(left ast.Expression) ast.Expression {
	p.Advance()
	p.MatchKeyword("TO")
	right := p.parseBitwiseExpression()
	if p.MatchKeyword("ESCAPE") {
		p.parseBitwiseExpression()
	}
	return &ast.ComparisonExpression{Operator: "SIMILAR TO", Left: left, Right: right, Quantifier: ""}
}

// parseComparisonOperator parses a binary comparison and an optional ANY, ALL or SOME
// suffix. Quantified comparisons use array elements or subquery rows; other parenthesised
// values retain their parameters but have unknown comparison types.
//
// Takes left (ast.Expression) which is the comparison's LHS.
//
// Returns ast.Expression which is the comparison expression.
func (p *parser) parseComparisonOperator(left ast.Expression) ast.Expression {
	operator := p.Advance().Value
	if p.IsKeyword("ANY", keywordALL, "SOME") {
		quantifier := strings.ToUpper(p.Advance().Value)
		var right ast.Expression = &ast.UnknownExpression{}
		if p.Current().Kind == tokenLeftParen {
			if operand := p.parseParenthesisedExpression(); !isRowValue(operand) {
				right = operand
			}
		}
		return &ast.ComparisonExpression{Operator: operator, Left: left, Right: right, Quantifier: quantifier}
	}
	right := p.parseBitwiseExpression()
	return &ast.ComparisonExpression{Operator: operator, Left: left, Right: right, Quantifier: ""}
}

// parseBitwiseExpression parses a left-associative chain of the operators that bind
// between the comparisons and the arithmetic: the bitwise operators (`&`, `|`, `#`, `<<`,
// `>>`), exponentiation (`^`) and any other operator symbol PostgreSQL accepts.
//
// Returns ast.Expression which is the bitwise expression.
func (p *parser) parseBitwiseExpression() ast.Expression {
	left := p.parseJSONExpression()

	for {
		switch {
		case p.Current().Kind == tokenOperator && isGenericBinaryOperator(p.Current().Value):
			operator := p.Advance().Value
			right := p.parseJSONExpression()
			left = &ast.BinaryOpExpression{Operator: operator, Left: left, Right: right, MayBeNull: false}
		case p.isOperatorConstruct():
			left = p.parseOperatorConstruct(left)
		default:
			return left
		}
	}
}

// isOperatorConstruct reports whether the cursor is on an OPERATOR(schema.symbol)
// construct, which names an operator by its schema.
//
// Returns bool which is true when OPERATOR and its '(' follow.
func (p *parser) isOperatorConstruct() bool {
	return p.IsKeyword("OPERATOR") && p.Peek().Kind == tokenLeftParen
}

// parseOperatorConstruct parses an OPERATOR(schema.symbol) construct and its right
// operand, as the operator the symbol names: a comparison or boolean operator becomes a
// comparison, and any other a binary operation.
//
// Takes left (ast.Expression) which is the left operand.
//
// Returns ast.Expression which is the operation.
func (p *parser) parseOperatorConstruct(left ast.Expression) ast.Expression {
	p.Advance()
	p.Advance()
	for p.Current().Kind == tokenIdentifier && p.Peek().Kind == tokenDot {
		p.Advance()
		p.Advance()
	}
	symbol := p.Current().Value
	if p.Current().Kind != tokenOperator && p.Current().Kind != tokenStar && p.Current().Kind != tokenArrow &&
		p.Current().Kind != tokenDoubleArrow && p.Current().Kind != tokenHashArrow && p.Current().Kind != tokenHashDoubleArrow {
		_ = p.syntaxError("expected an operator symbol inside OPERATOR()")
		return left
	}
	p.Advance()
	if p.Current().Kind != tokenRightParen {
		_ = p.syntaxError("expected ')' to close OPERATOR()")
		return left
	}
	p.Advance()
	right := p.parseJSONExpression()
	if isComparisonOperator(symbol) || isPredicateOperator(symbol) {
		return &ast.ComparisonExpression{Operator: symbol, Left: left, Right: right, Quantifier: ""}
	}
	return &ast.BinaryOpExpression{Operator: symbol, Left: left, Right: right, MayBeNull: false}
}

// parseJSONExpression parses a left-associative chain of JSON access operators (`->`,
// `->>`, `#>`, `#>>`).
//
// Returns ast.Expression which is the JSON expression.
func (p *parser) parseJSONExpression() ast.Expression {
	left := p.parseAddExpression()

	for p.Current().Kind == tokenArrow || p.Current().Kind == tokenDoubleArrow ||
		p.Current().Kind == tokenHashArrow || p.Current().Kind == tokenHashDoubleArrow {
		operator := p.Advance().Value
		right := p.parseAddExpression()
		left = &ast.BinaryOpExpression{Operator: operator, Left: left, Right: right, MayBeNull: false}
	}

	return left
}

// parseAddExpression parses a left-associative chain of `+` and `-` binary operators.
//
// Returns ast.Expression which is the additive expression.
func (p *parser) parseAddExpression() ast.Expression {
	left := p.parseMulExpression()

	for p.Current().Kind == tokenOperator &&
		(p.Current().Value == "+" || p.Current().Value == "-") {
		operator := p.Advance().Value
		right := p.parseMulExpression()
		left = &ast.BinaryOpExpression{Operator: operator, Left: left, Right: right, MayBeNull: false}
	}

	return left
}

// parseMulExpression parses a left-associative chain of `*`, `/`, and `%` operators.
//
// Returns ast.Expression which is the multiplicative expression.
func (p *parser) parseMulExpression() ast.Expression {
	left := p.parseConcatExpression()

	for p.isMulOperator() {
		operator := p.Advance().Value
		right := p.parseConcatExpression()
		left = &ast.BinaryOpExpression{Operator: operator, Left: left, Right: right, MayBeNull: false}
	}

	return left
}

// isMulOperator reports whether the current token is a multiplicative operator: `*`, `/`,
// `%`, or `//`, which PostgreSQL leaves undefined and CockroachDB defines as integer
// division.
//
// Returns bool which is true for a multiplicative operator.
func (p *parser) isMulOperator() bool {
	if p.Current().Kind == tokenStar {
		return true
	}
	return p.Current().Kind == tokenOperator &&
		(p.Current().Value == "/" || p.Current().Value == "%" || p.Current().Value == "//")
}

// parseConcatExpression parses a left-associative chain of `||` concatenation operators.
//
// Returns ast.Expression which is the concatenation expression.
func (p *parser) parseConcatExpression() ast.Expression {
	left := p.parseUnaryExpression()

	for p.Current().Kind == tokenOperator && p.Current().Value == "||" {
		p.Advance()
		right := p.parseUnaryExpression()
		left = &ast.BinaryOpExpression{Operator: "||", Left: left, Right: right, MayBeNull: false}
	}

	return left
}

// parseUnaryExpression parses a unary `-`, `+`, `~`, or NOT prefix and the following
// expression.
//
// Returns ast.Expression which is the unary expression, or the underlying expression when
// no unary prefix appears.
func (p *parser) parseUnaryExpression() ast.Expression {
	if p.Current().Kind == tokenOperator &&
		(p.Current().Value == "-" || p.Current().Value == "+" || p.Current().Value == "~") {
		operator := p.Advance().Value
		inner := p.parsePostfixExpression()
		if _, ok := inner.(*ast.LiteralExpression); ok {
			return inner
		}
		return &ast.UnaryOpExpression{Operator: operator, Operand: inner}
	}
	if p.MatchKeyword(keywordNOT) {
		inner := p.parsePostfixExpression()
		return &ast.UnaryOpExpression{Operator: keywordNOT, Operand: inner}
	}
	return p.parsePostfixExpression()
}

// parsePostfixExpression parses the postfix operators that bind just below the cast:
// COLLATE, which keeps its operand's type, and AT TIME ZONE, which is PostgreSQL's
// timezone(zone, value) function.
//
// Returns ast.Expression which is the operand with its postfix operators applied.
func (p *parser) parsePostfixExpression() ast.Expression {
	left := p.parseCastExpression()
	for {
		switch {
		case p.MatchKeyword("COLLATE"):
			_, _, _ = p.requireSchemaQualifiedName()
		case p.IsKeyword("AT") && p.Peek().IsKeyword(tokenIdentifier, keywordTIME) &&
			p.PeekAt(2).IsKeyword(tokenIdentifier, keywordZONE):
			p.Position += timeZoneClauseLength
			left = p.parseTimeZoneConversion(left)
		case p.IsKeyword("AT") && p.Peek().IsKeyword(tokenIdentifier, "LOCAL"):
			p.Advance()
			p.Advance()
		default:
			return left
		}
	}
}

// parseTimeZoneConversion parses the zone of an AT TIME ZONE and returns the conversion
// as a call of the timezone function, tagging a parameter zone as its first argument so
// it types as the zone.
//
// Takes value (ast.Expression) which is the value being converted.
//
// Returns ast.Expression which is the timezone(zone, value) call.
func (p *parser) parseTimeZoneConversion(value ast.Expression) ast.Expression {
	parameterCountBefore := p.parameterCount
	referenceCountBefore := len(p.parameterReferences)
	zone := p.parseCastExpression()
	slots := make(map[int]int, len(p.parameterReferences)-referenceCountBefore)
	for index := referenceCountBefore; index < len(p.parameterReferences); index++ {
		slots[index] = 0
	}
	p.markParametersAsFunctionArguments(parameterCountBefore, timezoneFunctionName, slots, nil)
	return &ast.FunctionCallExpression{
		FunctionName:     timezoneFunctionName,
		Schema:           "",
		Arguments:        []ast.Expression{zone, value},
		FilterExpression: nil,
		Span:             ast.Span{},
	}
}

// parseCastExpression parses any trailing `::type` casts after a subscript expression. A
// parameter the cast operand holds, untyped by its context, takes the cast's type; one
// registered before the operand, such as the parameter of an earlier comparison, does
// not.
//
// Returns ast.Expression which is the cast expression, or the underlying expression when
// no cast follows.
func (p *parser) parseCastExpression() ast.Expression {
	referencesBefore := len(p.parameterReferences)
	left := p.parseSubscriptExpression()

	for p.Current().Kind == tokenCast {
		p.Advance()
		typeName := p.parseCastTypeName()

		if len(p.parameterReferences) > referencesBefore {
			lastIndex := len(p.parameterReferences) - 1
			if p.parameterReferences[lastIndex].Context == sqlmodel.ParameterContextUnknown {
				p.parameterReferences[lastIndex].Context = sqlmodel.ParameterContextCast
				p.parameterReferences[lastIndex].CastType = new(p.types.normalise(typeName))
			}
		}

		left = &ast.CastExpression{
			TypeName: typeName,
			Inner:    left,
		}
	}

	return left
}

// parseSubscriptExpression parses array subscript or slice expressions after a primary
// expression. A subscript written with one string literal, as in settings['colour'],
// keeps the literal as its key.
//
// Returns ast.Expression which is the subscript expression, or the underlying primary
// when no subscript follows.
func (p *parser) parseSubscriptExpression() ast.Expression {
	left := p.parsePrimaryExpression()

	for p.Current().Kind == tokenLeftBracket {
		p.Advance()
		key := p.literalSubscriptKey()
		var indexExpression ast.Expression
		if !p.isSliceColon() {
			indexExpression = p.parseExpression()
		}
		isSlice := false
		if p.isSliceColon() {
			p.Advance()
			if p.Current().Kind != tokenRightBracket {
				p.parseExpression()
			}
			isSlice = true
		}
		if p.Current().Kind != tokenRightBracket {
			_ = p.syntaxError("expected ']' to close the subscript")
			return left
		}
		p.Advance()
		if isSlice {
			left = &ast.UnknownExpression{}
		} else {
			left = &ast.ArraySubscriptExpression{Array: left, Index: indexExpression, Key: key}
		}
	}

	return left
}

// literalSubscriptKey returns the text of the subscript at the cursor when it is one
// string literal, such as colour in settings['colour'].
//
// Returns string which is the literal's text, or the empty string for any other
// subscript.
func (p *parser) literalSubscriptKey() string {
	if p.Current().Kind != tokenString || p.Peek().Kind != tokenRightBracket {
		return ""
	}
	return p.Current().Value
}

// parsePrimaryExpression parses a literal, parameter, parenthesised expression, or
// identifier expression.
//
// Every primary expression counts as a level of expression nesting, so recursion that
// never passes through parseExpression, such as a chain of prefix operators or nested
// POSITION calls, is held to the parser's depth limit too. A parenthesised expression is
// counted by the parseExpression it opens instead.
//
// Returns ast.Expression which is the parsed primary expression, or UnknownExpression
// when the token is unrecognised or the depth limit is reached.
func (p *parser) parsePrimaryExpression() ast.Expression {
	current := p.Current()
	if current.Kind == tokenLeftParen {
		return p.parseParenthesisedExpression()
	}
	if !p.enterExpression() {
		return &ast.UnknownExpression{}
	}
	defer p.leaveExpression()

	switch current.Kind {
	case tokenNumber:
		return p.parseNumberLiteral(current)
	case tokenString, tokenEscapeString, tokenDollarString:
		p.Advance()
		return &ast.LiteralExpression{TypeName: "text"}
	case tokenBitString:
		p.Advance()
		return &ast.LiteralExpression{TypeName: "bit"}
	case tokenDollarParam, tokenNamedParam:
		return p.parseParameterExpression()
	case tokenIdentifier:
		return p.parseIdentifierExpression()
	case tokenOperator:
		p.Advance()
		p.parsePostfixExpression()
		return &ast.UnknownExpression{}
	case tokenEOF, tokenComma, tokenRightParen, tokenRightBracket, tokenSemicolon:
		_ = p.syntaxError("expected an expression")
		return &ast.UnknownExpression{}
	default:
		p.Advance()
		return &ast.UnknownExpression{}
	}
}

// isSliceColon reports whether the current token is the colon of an array slice.
//
// Returns bool which is true for a bare `:` operator.
func (p *parser) isSliceColon() bool {
	return p.Current().Kind == tokenOperator && p.Current().Value == ":"
}

// parseNumberLiteral consumes a numeric token and assigns the type numberLiteralType uses
// for its scanned shape.
//
// Takes current (token) which is the number token.
//
// Returns ast.Expression which is the literal expression.
func (p *parser) parseNumberLiteral(current token) ast.Expression {
	p.Advance()
	return &ast.LiteralExpression{TypeName: numberLiteralType(current.Value)}
}

// parseParameterExpression consumes and records a parameter, using a preceding column and
// arithmetic or same-type operator as comparison context. An explicit cast takes
// precedence; parameters without that context remain unknown.
//
// Returns ast.Expression which is the parameter, by the number it is registered under, so
// the analyser types it from what it is compared with when nothing else types it.
func (p *parser) parseParameterExpression() ast.Expression {
	parameterToken := p.Current()
	context, columnReference := sqlmodel.ParameterContextUnknown, (*ast.ColumnReference)(nil)
	if next := p.Position + 1; next >= len(p.Tokens) || p.Tokens[next].Kind != tokenCast {
		context, columnReference = p.resolvePartnerOperandContext(p.Position)
	}
	p.Advance()
	number := p.registerParameterFromToken(parameterToken, context, columnReference, nil)
	return &ast.ParameterExpression{Number: number}
}

// parseParenthesisedExpression parses a parenthesised expression, dispatching to
// scalar-subquery handling when a SELECT follows the opening paren.
//
// Returns ast.Expression which is the inner expression or scalar subquery.
func (p *parser) parseParenthesisedExpression() ast.Expression {
	if p.isSubqueryStart() {
		return p.parseScalarSubquery()
	}
	p.Advance()
	elements := []ast.Expression{p.parseExpression()}
	for p.Current().Kind == tokenComma {
		p.Advance()
		elements = append(elements, p.parseExpression())
	}
	if p.Current().Kind != tokenRightParen {
		_ = p.syntaxError("expected ')' to close the parenthesised expression")
		return elements[0]
	}
	p.Advance()
	if len(elements) > 1 {
		return &ast.RowValueExpression{Elements: elements}
	}
	return p.parseCompositeFieldSelection(elements[0])
}

// parseCompositeFieldSelection reads the field selections that may follow a parenthesised
// value of a composite type, such as (address).street, each a field access on the value
// before it. A selection of every field, such as (address).*, is no single value, so the
// expression it ends is unknown.
//
// Takes value (ast.Expression) which is the parenthesised value.
//
// Returns ast.Expression which is the value with its field selections applied, or the
// value itself when none follows.
func (p *parser) parseCompositeFieldSelection(value ast.Expression) ast.Expression {
	for p.Current().Kind == tokenDot && (p.Peek().Kind == tokenIdentifier || p.Peek().Kind == tokenStar) {
		p.Advance()
		field := p.Advance()
		if field.Kind == tokenStar {
			value = &ast.UnknownExpression{}
			continue
		}
		value = &ast.StructFieldAccessExpression{Struct: value, FieldName: field.Value}
	}
	return value
}

// parseIdentifierExpression parses an identifier-rooted expression, dispatching to
// specialised handlers for keywords such as NULL, CAST, and CASE.
//
// Returns ast.Expression which is the parsed expression.
func (p *parser) parseIdentifierExpression() ast.Expression {
	first := p.Position
	return p.locate(p.parseIdentifierRootedExpression(), first)
}

// parseIdentifierRootedExpression parses an identifier-rooted expression for
// parseIdentifierExpression, which records where its column or function was written.
//
// Returns ast.Expression which is the parsed expression.
func (p *parser) parseIdentifierRootedExpression() ast.Expression {
	upper := p.Current().Keyword(tokenIdentifier)

	if handler := p.identifierExpressionHandler(upper); handler != nil {
		return handler()
	}

	if p.Peek().Kind == tokenString && upper != "" {
		if _, isType := builtinTypeMap[strings.ToLower(upper)]; isType {
			typeName := strings.ToLower(p.Advance().Value)
			p.Advance()
			return &ast.CastExpression{TypeName: typeName, Inner: &ast.LiteralExpression{TypeName: "text"}}
		}
	}

	if _, isImplicit := implicitFunctionIdentifiers[upper]; isImplicit {
		p.Advance()
		if p.Current().Kind == tokenLeftParen {
			_ = p.requireSkipParenthesised()
		}
		return &ast.FunctionCallExpression{
			FunctionName:     strings.ToLower(upper),
			FilterExpression: nil,
			Schema:           "",
			Arguments:        nil,
			Span:             ast.Span{},
		}
	}

	return p.parseColumnOrFunctionReference()
}

// identifierExpressionHandler returns the parsing closure for a recognised identifier
// keyword such as NULL, TRUE, CAST, or CASE. ROW opens a row constructor only before a
// parenthesis; otherwise it is a name, such as a column or a table alias called row.
//
// Takes upper (string) which is the upper-case identifier value.
//
// Returns func() ast.Expression which is the matching sub-parser, or nil when the
// identifier is not recognised.
func (p *parser) identifierExpressionHandler(upper string) func() ast.Expression {
	switch upper {
	case keywordNULL:
		return p.parseNullLiteral
	case "TRUE", "FALSE":
		return p.parseBooleanLiteral
	case "CAST":
		return p.parseCastFunctionExpression
	case "COALESCE":
		return p.parseCoalesceExpression
	case "CASE":
		return p.parseCaseExpression
	case keywordEXISTS:
		return func() ast.Expression {
			p.Advance()
			return p.parseExistsSubquery()
		}
	case "ARRAY":
		return p.parseArrayExpression
	case keywordNOT:
		return func() ast.Expression { return &ast.UnknownExpression{} }
	case "INTERVAL":
		return p.parseIntervalLiteral
	case keywordROW:
		if p.Peek().Kind != tokenLeftParen {
			return nil
		}
		return p.parseRowConstructorExpression
	default:
		return nil
	}
}

// parseNullLiteral consumes the NULL keyword.
//
// Returns ast.Expression which is nil to represent SQL NULL.
func (p *parser) parseNullLiteral() ast.Expression {
	p.Advance()
	return nil
}

// parseBooleanLiteral consumes a TRUE or FALSE keyword.
//
// Returns ast.Expression which is the boolean literal.
func (p *parser) parseBooleanLiteral() ast.Expression {
	p.Advance()
	return &ast.LiteralExpression{TypeName: "boolean"}
}

// parseIntervalLiteral consumes an INTERVAL literal, its value, and any trailing field
// qualifiers.
//
// Returns ast.Expression which is the interval literal expression.
func (p *parser) parseIntervalLiteral() ast.Expression {
	p.Advance()
	if p.Current().Kind == tokenString {
		p.Advance()
	}
	for p.Current().Kind == tokenIdentifier {
		if _, ok := intervalFieldKeywords[p.Current().Keyword(tokenIdentifier)]; !ok {
			break
		}
		p.Advance()
	}
	return &ast.LiteralExpression{TypeName: "interval"}
}

// parseRowConstructorExpression consumes the ROW keyword and the trailing parenthesised
// list.
//
// Returns ast.Expression which is the row, or an unknown expression when no list follows.
func (p *parser) parseRowConstructorExpression() ast.Expression {
	p.Advance()
	if p.Current().Kind != tokenLeftParen {
		return &ast.UnknownExpression{}
	}
	p.Advance()
	var elements []ast.Expression
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		elements = append(elements, p.parseExpression())
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}
	return &ast.RowValueExpression{Elements: elements}
}

// parseColumnOrFunctionReference parses a bare identifier as a column reference or
// function call depending on the following token.
//
// Returns ast.Expression which is the column reference, function call, or qualified
// reference.
func (p *parser) parseColumnOrFunctionReference() ast.Expression {
	name := p.Advance().Value

	if p.Current().Kind == tokenDot {
		return p.parseDotQualifiedIdentifier(name)
	}

	if p.Current().Kind == tokenLeftParen {
		if special := p.parseSpecialFunctionCall(name); special != nil {
			return special
		}
		return p.parseFunctionCall(name, "")
	}

	return &ast.ColumnRefExpression{
		TableAlias: "",
		ColumnName: name,
		Span:       ast.Span{},
	}
}

// parseDotQualifiedIdentifier parses the tail of a dot-qualified reference such as
// `alias.column`, `alias.*`, or `schema.function(...)`.
//
// Takes name (string) which is the identifier before the dot.
//
// Returns ast.Expression which is the qualified column or function expression.
func (p *parser) parseDotQualifiedIdentifier(name string) ast.Expression {
	p.Advance()
	if p.Current().Kind == tokenStar {
		p.Advance()
		return &ast.UnknownExpression{}
	}
	if p.Current().Kind != tokenIdentifier {
		return &ast.ColumnRefExpression{TableAlias: "", ColumnName: name, Span: ast.Span{}}
	}
	second := p.Advance().Value

	if p.Current().Kind == tokenDot {
		return p.parseSchemaQualifiedReference(name, second)
	}
	if p.Current().Kind == tokenLeftParen {
		return p.parseFunctionCall(second, name)
	}
	return &ast.ColumnRefExpression{TableAlias: name, ColumnName: second, Span: ast.Span{}}
}

// parseSchemaQualifiedReference parses the tail of a three-part reference such as
// `schema.table.column`, `schema.table.*` or `schema.table.function(...)`.
//
// Takes schema (string) which is the leading schema name.
// Takes table (string) which is the middle identifier.
//
// Returns ast.Expression which is the qualified column or function expression.
func (p *parser) parseSchemaQualifiedReference(schema string, table string) ast.Expression {
	p.Advance()
	if p.Current().Kind == tokenStar {
		p.Advance()
		return &ast.UnknownExpression{}
	}
	if p.Current().Kind != tokenIdentifier {
		return &ast.ColumnRefExpression{TableAlias: schema, ColumnName: table, Span: ast.Span{}}
	}
	third := p.Advance().Value
	if p.Current().Kind == tokenLeftParen {
		return p.parseFunctionCall(third, schema+"."+table)
	}
	return &ast.ColumnRefExpression{TableAlias: table, ColumnName: third, Span: ast.Span{}}
}

// parseIsSuffix parses the tail of an IS predicate including IS NULL, IS DISTINCT FROM,
// and IS TRUE/FALSE/UNKNOWN.
//
// Takes left (ast.Expression) which is the IS predicate's LHS.
//
// Returns ast.Expression which is the IS expression.
func (p *parser) parseIsSuffix(left ast.Expression) ast.Expression {
	p.Advance()
	negated := p.MatchKeyword(keywordNOT)

	if p.MatchKeyword(keywordNULL) {
		return &ast.IsNullExpression{Inner: left, Negated: negated}
	}

	if p.MatchKeyword("DISTINCT") {
		p.MatchKeyword(keywordFROM)
		right := p.parseBitwiseExpression()
		operator := "IS DISTINCT FROM"
		if negated {
			operator = "IS NOT DISTINCT FROM"
		}
		return &ast.ComparisonExpression{Operator: operator, Left: left, Right: right, Quantifier: ""}
	}

	if p.MatchKeyword("TRUE") || p.MatchKeyword("FALSE") || p.MatchKeyword("UNKNOWN") || p.MatchKeyword("DOCUMENT") {
		return &ast.IsNullExpression{Inner: left, Negated: negated}
	}

	if p.MatchKeyword("JSON") {
		p.skipJSONPredicateTail()
		return &ast.IsNullExpression{Inner: left, Negated: negated}
	}

	if p.matchNormalisationTest() {
		return &ast.IsNullExpression{Inner: left, Negated: negated}
	}

	if p.MatchKeyword("OF") {
		_ = p.requireSkipParenthesised()
		return &ast.IsNullExpression{Inner: left, Negated: negated}
	}

	_ = p.syntaxError("expected NULL, TRUE, FALSE, UNKNOWN, DISTINCT FROM, JSON, DOCUMENT, NORMALIZED or OF after IS")
	return &ast.IsNullExpression{Inner: left, Negated: negated}
}

// skipJSONPredicateTail consumes what may follow IS [NOT] JSON: the kind of JSON value
// tested for and a unique-keys clause.
func (p *parser) skipJSONPredicateTail() {
	if !p.MatchKeyword("VALUE") && !p.MatchKeyword("SCALAR") && !p.MatchKeyword("ARRAY") {
		p.MatchKeyword("OBJECT")
	}
	if p.MatchKeyword(keywordWITH) || p.MatchKeyword("WITHOUT") {
		_ = p.requireKeyword(keywordUNIQUE)
		p.MatchKeyword("KEYS")
	}
}

// matchNormalisationTest consumes the tail of a Unicode normalisation test after IS
// [NOT]: an optional normal form, such as NFC, and the keyword that closes the test.
//
// Returns bool which is true when the test was consumed.
func (p *parser) matchNormalisationTest() bool {
	if p.IsKeyword("NFC", "NFD", "NFKC", "NFKD") && p.Peek().IsKeyword(tokenIdentifier, "NORMALIZED") {
		p.Advance()
	}
	return p.MatchKeyword("NORMALIZED")
}

// parseInListSuffix parses an IN clause containing either a subquery or an explicit list
// of values and tags any contained parameters with the inferred column reference.
//
// Takes left (ast.Expression) which is the IN predicate's LHS.
//
// Returns ast.Expression which is the IN-list expression.
func (p *parser) parseInListSuffix(left ast.Expression) ast.Expression {
	p.Advance()

	if p.isSubqueryStart() {
		innerAnalysis, analyseError := p.analyseSubqueryBody()
		if errors.Is(analyseError, errSubquerySkipped) {
			return &ast.InListExpression{Inner: left, Values: nil}
		}
		if analyseError != nil {
			return &ast.UnknownExpression{}
		}
		return &ast.InListExpression{
			Inner:  left,
			Values: []ast.Expression{&ast.ScalarSubqueryExpression{InnerQuery: innerAnalysis}},
		}
	}

	referenceCountBefore := len(p.parameterReferences)
	values := p.parseParenthesisedExpressionList()

	var columnReference *ast.ColumnReference
	if columnExpression, ok := left.(*ast.ColumnRefExpression); ok {
		columnReference = &ast.ColumnReference{
			TableAlias: columnExpression.TableAlias,
			ColumnName: columnExpression.ColumnName,
		}
	}
	for index := referenceCountBefore; index < len(p.parameterReferences); index++ {
		p.parameterReferences[index].Context = sqlmodel.ParameterContextInList
		if columnReference != nil {
			p.parameterReferences[index].ColumnReference = columnReference
		}
	}

	return &ast.InListExpression{Inner: left, Values: values}
}

// parseBetweenSuffix parses the low and high operands of a BETWEEN predicate and tags any
// contained parameters with the inferred column reference.
//
// Takes left (ast.Expression) which is the BETWEEN predicate's LHS.
//
// Returns ast.Expression which is the BETWEEN expression.
func (p *parser) parseBetweenSuffix(left ast.Expression) ast.Expression {
	p.Advance()
	p.MatchKeyword("SYMMETRIC")

	referenceCountBefore := len(p.parameterReferences)
	low := p.parseAddExpression()
	_ = p.requireKeyword(keywordAND)
	high := p.parseAddExpression()

	var columnReference *ast.ColumnReference
	if columnExpression, ok := left.(*ast.ColumnRefExpression); ok {
		columnReference = &ast.ColumnReference{
			TableAlias: columnExpression.TableAlias,
			ColumnName: columnExpression.ColumnName,
		}
	}
	for index := referenceCountBefore; index < len(p.parameterReferences); index++ {
		p.parameterReferences[index].Context = sqlmodel.ParameterContextBetween
		if columnReference != nil {
			p.parameterReferences[index].ColumnReference = columnReference
		}
	}

	return &ast.BetweenExpression{Inner: left, Low: low, High: high}
}

// parseParenthesisedExpressionList parses a comma-separated list of expressions enclosed
// in parentheses.
//
// Returns []ast.Expression which is the list of parsed expressions, or nil when the
// current token is not '('.
func (p *parser) parseParenthesisedExpressionList() []ast.Expression {
	if p.Current().Kind != tokenLeftParen {
		return nil
	}
	p.Advance()

	var values []ast.Expression
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		values = append(values, p.parseExpression())
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}

	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}
	return values
}

// isRowValue reports whether an expression is a row constructor.
//
// Takes expression (ast.Expression) which is the expression.
//
// Returns bool which is true for a row.
func isRowValue(expression ast.Expression) bool {
	_, isRow := expression.(*ast.RowValueExpression)
	return isRow
}

// numberLiteralType assigns an integer, bigint or numeric type by integer magnitude,
// numeric for a fraction, and double precision for an exponent. The exponent mapping
// differs from PostgreSQL, which initially types exponent literals as numeric.
//
// Takes text (string) which is the literal as written.
//
// Returns string which is the literal's type name.
func numberLiteralType(text string) string {
	switch lexicalRules.Numbers.ClassifyNumber(text) {
	case dialectkit.NumberApproximate:
		return "double precision"
	case dialectkit.NumberExact:
		return "numeric"
	default:
		value, err := strconv.ParseInt(text, 0, 64)
		switch {
		case err != nil:
			return "numeric"
		case value > math.MaxInt32:
			return "bigint"
		default:
			return "integer"
		}
	}
}

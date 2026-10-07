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
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// keywordOver introduces a window function's window.
	keywordOver = "OVER"

	// keywordDistinct is the DISTINCT modifier of an aggregate's arguments.
	keywordDistinct = "DISTINCT"

	// keywordNull is the NULL literal.
	keywordNull = "NULL"

	// functionArray builds an array from its arguments; `[a, b]` is its literal form.
	functionArray = "array"

	// functionTuple builds a tuple from its arguments; `(a, b)` is its literal form.
	functionTuple = "tuple"

	// functionArrayElement reads one element of an array or map; `a[i]` is its operator
	// form, and like ClickHouse it yields the element type's default value, not NULL, for an
	// index out of range.
	functionArrayElement = "arrayElement"

	// functionIntegerDivision is the integer division the DIV operator calls.
	functionIntegerDivision = "intDiv"

	// unsignedOneByteLimit is the largest value a UInt8 literal holds.
	unsignedOneByteLimit = 1<<8 - 1

	// unsignedTwoByteLimit is the largest value a UInt16 literal holds.
	unsignedTwoByteLimit = 1<<16 - 1

	// unsignedFourByteLimit is the largest value a UInt32 literal holds.
	unsignedFourByteLimit = 1<<32 - 1

	// signedOneByteMagnitude is the largest magnitude a negative Int8 literal holds.
	signedOneByteMagnitude = 1 << 7

	// signedTwoByteMagnitude is the largest magnitude a negative Int16 literal holds.
	signedTwoByteMagnitude = 1 << 15

	// signedFourByteMagnitude is the largest magnitude a negative Int32 literal holds.
	signedFourByteMagnitude = 1 << 31

	// signedEightByteMagnitude is the largest magnitude a negative Int64 literal holds.
	signedEightByteMagnitude = 1 << 63

	// binaryRadix is the base of a 0b literal.
	binaryRadix = 2

	// octalRadix is the base of a 0o literal.
	octalRadix = 8

	// hexadecimalRadix is the base of a 0x literal.
	hexadecimalRadix = 16

	// radixPrefixLength is the length of a base prefix such as 0x.
	radixPrefixLength = 2
)

var (
	// comparisonOperators are the comparison operators, which yield a boolean.
	comparisonOperators = map[string]bool{"=": true, "==": true, "<": true, ">": true, "<=": true, ">=": true, "<>": true, "!=": true}

	// additiveOperators are the operators at the additive level: addition, subtraction and
	// concatenation.
	additiveOperators = map[string]bool{"+": true, "-": true, "||": true}

	// bitwiseOperators are the bitwise operators ClickHouse's grammar shares with other
	// engines, kept at one level below the additive operators.
	bitwiseOperators = map[string]bool{"&": true, "|": true, "^": true, "<<": true, ">>": true}
)

// parseExpression parses one expression at the cursor into an ast.Expression, the tree
// the analyser types: the projection items of a SELECT and the body of a CREATE FUNCTION
// lambda.
//
// The grammar follows ClickHouse's operator precedence, from the lambda arrow through OR,
// AND, NOT, the comparison and membership forms, concatenation and arithmetic, unary
// signs and the postfix subscript, cast and element-access operators, down to literals,
// placeholders, columns, function and window calls, CASE, CAST, INTERVAL, EXISTS and
// subqueries. A form the grammar does not model yields an UnknownExpression, which the
// caller detects by comparing where parsing stopped with where the expression ends.
//
// The expressionDepth counter bounds the recursion; at the cap an error wrapping
// dialectkit.ErrParseDepth is recorded as the parser's syntax error, so an over-deep
// expression is reported rather than silently degraded.
//
// Returns ast.Expression which is the parsed tree.
func (p *parser) parseExpression() ast.Expression {
	if !p.enterExpression() {
		return &ast.UnknownExpression{}
	}
	defer p.leaveExpression()
	return p.parseLambdaExpression()
}

// enterExpression counts one level of expression nesting, recording a depth error at the
// cap.
//
// Returns bool which is false when the cap is reached and the caller must not recurse.
func (p *parser) enterExpression() bool {
	if p.expressionDepth >= p.maxParseDepth {
		p.recordSyntaxError(p.depthError())
		return false
	}
	p.expressionDepth++
	return true
}

// leaveExpression undoes one enterExpression.
func (p *parser) leaveExpression() {
	p.expressionDepth--
}

// parseLambdaExpression parses a lambda, `x -> body` or `(x, y) -> body`, the lowest
// precedence form, or else an OR expression.
//
// Returns ast.Expression which is the lambda or the underlying expression.
func (p *parser) parseLambdaExpression() ast.Expression {
	if parameters, isLambda := p.tryConsumeLambdaParameters(); isLambda {
		return &ast.LambdaExpression{Parameters: parameters, Body: p.parseOrExpression(), Bindings: nil}
	}
	return p.parseOrExpression()
}

// tryConsumeLambdaParameters consumes a lambda's parameter list and its arrow: a single
// identifier or a parenthesised list of identifiers, followed by `->`. On a miss the
// cursor is left unchanged.
//
// Returns []string which holds the parameter names.
// Returns bool which is true when a parameter list and arrow were consumed.
func (p *parser) tryConsumeLambdaParameters() ([]string, bool) {
	saved := p.Position
	if p.Current().Kind == tokenIdentifier && p.Peek().Kind == tokenArrow {
		name := p.Advance().Value
		p.Advance()
		return []string{name}, true
	}
	if p.Current().Kind != tokenLeftParen {
		return nil, false
	}
	p.Advance()
	var parameters []string
	for p.Current().Kind == tokenIdentifier {
		parameters = append(parameters, p.Advance().Value)
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	if p.Current().Kind == tokenRightParen && p.Peek().Kind == tokenArrow {
		p.Advance()
		p.Advance()
		return parameters, true
	}
	p.Position = saved
	return nil, false
}

// parseOrExpression parses a chain of OR operands.
//
// Returns ast.Expression which is the chain or its only operand.
func (p *parser) parseOrExpression() ast.Expression {
	return p.parseLogicalChain("OR", p.parseAndExpression)
}

// parseAndExpression parses a chain of AND operands.
//
// Returns ast.Expression which is the chain or its only operand.
func (p *parser) parseAndExpression() ast.Expression {
	return p.parseLogicalChain("AND", p.parseNotExpression)
}

// parseLogicalChain parses operands joined by one logical keyword into a single
// LogicalOpExpression.
//
// Takes keyword (string) which is AND or OR.
// Takes operand (func() ast.Expression) which parses one operand.
//
// Returns ast.Expression which is the chain, or the operand alone when no keyword follows
// it.
func (p *parser) parseLogicalChain(keyword string, operand func() ast.Expression) ast.Expression {
	first := operand()
	if !p.IsKeyword(keyword) {
		return first
	}
	operands := []ast.Expression{first}
	for p.MatchKeyword(keyword) {
		operands = append(operands, operand())
	}
	return &ast.LogicalOpExpression{Operator: keyword, Operands: operands}
}

// parseNotExpression parses a prefix NOT, which may repeat.
//
// Returns ast.Expression which is the negation or the underlying comparison.
func (p *parser) parseNotExpression() ast.Expression {
	if !p.IsKeyword(kwNot) {
		return p.parseComparisonExpression()
	}
	if !p.enterExpression() {
		return &ast.UnknownExpression{}
	}
	defer p.leaveExpression()
	p.Advance()
	return &ast.UnaryOpExpression{Operator: kwNot, Operand: p.parseNotExpression()}
}

// parseComparisonExpression parses a comparison or membership test on an operand: a
// comparison operator, IS [NOT] NULL, [NOT] BETWEEN, [NOT] LIKE or ILIKE, and [GLOBAL]
// [NOT] IN.
//
// Returns ast.Expression which is the test, or the operand alone when none follows.
func (p *parser) parseComparisonExpression() ast.Expression {
	left := p.parseAdditiveExpression()
	if p.MatchKeyword("IS") {
		negated := p.MatchKeyword(kwNot)
		if !p.MatchKeyword(keywordNull) {
			return &ast.UnknownExpression{}
		}
		return &ast.IsNullExpression{Inner: left, Negated: negated}
	}
	saved := p.Position
	p.MatchKeyword(kwGlobal)
	negated := p.MatchKeyword(kwNot)
	switch {
	case p.MatchKeyword("IN"):
		return p.parseInList(left)
	case p.MatchKeyword("BETWEEN"):
		return p.parseBetween(left)
	case p.IsKeyword("LIKE", "ILIKE"):
		operator := p.Advance().Keyword(tokenIdentifier)
		if negated {
			operator = kwNot + " " + operator
		}
		return &ast.ComparisonExpression{Left: left, Operator: operator, Right: p.parseAdditiveExpression(), Quantifier: ""}
	}
	p.Position = saved
	if current := p.Current(); current.Kind == tokenOperator && comparisonOperators[current.Value] {
		p.Advance()
		return &ast.ComparisonExpression{Left: left, Operator: current.Value, Right: p.parseAdditiveExpression(), Quantifier: ""}
	}
	return left
}

// parseInList parses the right-hand side of IN: a parenthesised list of values, a
// subquery, or a single expression such as an array or a placeholder.
//
// Takes left (ast.Expression) which is the tested operand.
//
// Returns ast.Expression which is the membership test.
func (p *parser) parseInList(left ast.Expression) ast.Expression {
	if p.Current().Kind != tokenLeftParen || p.startsSubquery(1) {
		return &ast.InListExpression{Inner: left, Values: []ast.Expression{p.parseAdditiveExpression()}}
	}
	p.Advance()
	values := p.parseExpressionList(tokenRightParen)
	if !p.expectKind(tokenRightParen) {
		return &ast.UnknownExpression{}
	}
	return &ast.InListExpression{Inner: left, Values: values}
}

// parseBetween parses the `low AND high` bounds of BETWEEN.
//
// Takes left (ast.Expression) which is the tested operand.
//
// Returns ast.Expression which is the range test.
func (p *parser) parseBetween(left ast.Expression) ast.Expression {
	low := p.parseAdditiveExpression()
	if !p.MatchKeyword("AND") {
		return &ast.UnknownExpression{}
	}
	return &ast.BetweenExpression{Inner: left, Low: low, High: p.parseAdditiveExpression()}
}

// parseAdditiveExpression parses addition, subtraction and concatenation, and the bitwise
// operators below them.
//
// Returns ast.Expression which is the operation or its only operand.
func (p *parser) parseAdditiveExpression() ast.Expression {
	left := p.parseMultiplicativeExpression()
	for {
		current := p.Current()
		if current.Kind != tokenOperator || !additiveOperators[current.Value] && !bitwiseOperators[current.Value] {
			return left
		}
		p.Advance()
		left = &ast.BinaryOpExpression{Left: left, Operator: current.Value, Right: p.parseMultiplicativeExpression(), MayBeNull: false}
	}
}

// parseMultiplicativeExpression parses multiplication, division and modulo, written as
// `*`, `/`, `%`, MOD or DIV, the last being ClickHouse's integer division.
//
// Returns ast.Expression which is the operation or its only operand.
func (p *parser) parseMultiplicativeExpression() ast.Expression {
	left := p.parseUnaryExpression()
	for {
		current := p.Current()
		switch {
		case current.Kind == tokenStar:
			p.Advance()
			left = &ast.BinaryOpExpression{Left: left, Operator: operatorMultiply, Right: p.parseUnaryExpression(), MayBeNull: false}
		case current.Kind == tokenOperator && (current.Value == operatorDivide || current.Value == operatorModulo):
			p.Advance()
			left = &ast.BinaryOpExpression{Left: left, Operator: current.Value, Right: p.parseUnaryExpression(), MayBeNull: false}
		case current.IsKeyword(tokenIdentifier, "MOD"):
			p.Advance()
			left = &ast.BinaryOpExpression{Left: left, Operator: operatorModulo, Right: p.parseUnaryExpression(), MayBeNull: false}
		case current.IsKeyword(tokenIdentifier, "DIV"):
			p.Advance()
			left = newFunctionCall(functionIntegerDivision, left, p.parseUnaryExpression())
		default:
			return left
		}
	}
}

// parseUnaryExpression parses a prefix sign. A minus before a number literal is part of
// the literal, as in ClickHouse, so -1 is an Int8 rather than the negation of a UInt8.
//
// Returns ast.Expression which is the signed expression or the underlying postfix term.
func (p *parser) parseUnaryExpression() ast.Expression {
	current := p.Current()
	if current.Kind != tokenOperator || current.Value != operatorMinus && current.Value != "+" {
		return p.parsePostfixExpression()
	}
	if !p.enterExpression() {
		return &ast.UnknownExpression{}
	}
	defer p.leaveExpression()
	p.Advance()
	if current.Value == operatorMinus && p.Current().Kind == tokenNumber {
		number := p.Advance()
		return p.parsePostfixOperators(&ast.LiteralExpression{TypeName: negativeLiteralTypeName(number.Value)})
	}
	operand := p.parseUnaryExpression()
	if current.Value == "+" {
		return operand
	}
	return &ast.UnaryOpExpression{Operator: operatorMinus, Operand: operand}
}

// parsePostfixExpression parses a primary term and the postfix operators that follow it.
//
// Returns ast.Expression which is the term with its postfix operators applied.
func (p *parser) parsePostfixExpression() ast.Expression {
	return p.parsePostfixOperators(p.parsePrimaryExpression())
}

// parsePostfixOperators applies the postfix operators after a term: a subscript `[i]`,
// read as arrayElement so an index out of range yields the element's default as in
// ClickHouse; a cast `::Type`; and an element access `.name` or `.N` on a tuple, map or
// JSON value.
//
// Takes left (ast.Expression) which is the term.
//
// Returns ast.Expression which is the term with its postfix operators applied.
func (p *parser) parsePostfixOperators(left ast.Expression) ast.Expression {
	for {
		switch p.Current().Kind {
		case tokenLeftBracket:
			p.Advance()
			index := p.parseExpression()
			if !p.expectKind(tokenRightBracket) {
				return &ast.UnknownExpression{}
			}
			left = newFunctionCall(functionArrayElement, left, index)
		case tokenCast:
			p.Advance()
			left = newCastExpression(left, p.readCastTargetType())
		case tokenDot:
			selector := p.Peek()
			if selector.Kind != tokenIdentifier && selector.Kind != tokenNumber {
				return left
			}
			p.Advance()
			p.Advance()
			for _, element := range elementFieldNames(selector) {
				left = subcolumnAccess(left, element)
			}
		default:
			return left
		}
	}
}

// parsePrimaryExpression parses a primary term: a number, string, placeholder,
// parenthesised expression, tuple or subquery, array literal, or a term that starts with
// a word.
//
// Returns ast.Expression which is the term, or an UnknownExpression for a token that
// starts none.
func (p *parser) parsePrimaryExpression() ast.Expression {
	current := p.Current()
	switch current.Kind {
	case tokenNumber:
		p.Advance()
		return &ast.LiteralExpression{TypeName: literalNumberType(current.Value).EngineName}
	case tokenString:
		p.Advance()
		return &ast.LiteralExpression{TypeName: engineNameString}
	case tokenClickHouseParam:
		p.Advance()
		return p.placeholderExpression(current)
	case tokenLeftParen:
		return p.parseParenthesisedExpression()
	case tokenLeftBracket:
		p.Advance()
		elements := p.parseExpressionList(tokenRightBracket)
		if !p.expectKind(tokenRightBracket) {
			return &ast.UnknownExpression{}
		}
		return newFunctionCall(functionArray, elements...)
	case tokenIdentifier:
		return p.parseWordExpression()
	default:
		return &ast.UnknownExpression{}
	}
}

// placeholderExpression reads a typed ClickHouse placeholder as a cast parameter.
//
// Its declared type determines whether it can be NULL. Parameter numbering is assigned
// after the statement is parsed.
//
// Takes candidate (token) which is the placeholder.
//
// Returns ast.Expression which is the typed placeholder.
func (p *parser) placeholderExpression(candidate token) ast.Expression {
	name, typeName := splitClickHouseParamBody(candidate.Value)
	parameter := &ast.ParameterExpression{Number: p.namedParameterMap[name]}
	p.placedParameters[parameter] = name
	if isNullableTypeName(typeName) {
		return &ast.CastExpression{Inner: parameter, TypeName: typeName}
	}
	return &ast.CastExpression{
		Inner:    &ast.CoalesceExpression{Arguments: []ast.Expression{parameter, &ast.LiteralExpression{TypeName: typeName}}},
		TypeName: typeName,
	}
}

// parseParenthesisedExpression parses `(expr)`, a tuple `(a, b)`, or a scalar subquery
// `(SELECT ...)`.
//
// Returns ast.Expression which is the inner expression, the tuple or the subquery.
func (p *parser) parseParenthesisedExpression() ast.Expression {
	if p.startsSubquery(1) {
		inner := p.parseNestedSelect()
		if inner == nil {
			return &ast.UnknownExpression{}
		}
		return &ast.ScalarSubqueryExpression{InnerQuery: inner}
	}
	p.Advance()
	elements := p.parseExpressionList(tokenRightParen)
	if !p.expectKind(tokenRightParen) || len(elements) == 0 {
		return &ast.UnknownExpression{}
	}
	if len(elements) == 1 {
		return elements[0]
	}
	return newFunctionCall(functionTuple, elements...)
}

// parseWordExpression parses a term that starts with a word: CASE, CAST, INTERVAL, a DATE
// or TIMESTAMP literal, EXISTS, TRUE, FALSE, NULL, a function or window call, or a column
// reference.
//
// Returns ast.Expression which is the term.
func (p *parser) parseWordExpression() ast.Expression {
	first := p.Position
	return p.locate(p.parseWordRootedExpression(), first)
}

// parseWordRootedExpression parses a term that starts with a word for
// parseWordExpression, which records where its column or function was written.
//
// Returns ast.Expression which is the term.
func (p *parser) parseWordRootedExpression() ast.Expression {
	switch p.Current().Keyword(tokenIdentifier) {
	case "CASE":
		p.Advance()
		return p.parseCaseExpression()
	case "CAST":
		if p.Peek().Kind == tokenLeftParen {
			p.Advance()
			return p.parseCastCall()
		}
	case "INTERVAL":
		p.Advance()
		return p.parseIntervalLiteral()
	case "DATE", "TIMESTAMP":
		if literal, isTypedLiteral := p.parseTypedLiteral(); isTypedLiteral {
			return literal
		}
	case "EXISTS":
		if p.startsSubquery(2) {
			p.Advance()
			inner := p.parseNestedSelect()
			if inner == nil {
				return &ast.UnknownExpression{}
			}
			return &ast.ExistsExpression{InnerQuery: inner}
		}
	case "TRUE", "FALSE":
		p.Advance()
		return &ast.LiteralExpression{TypeName: engineNameBool}
	case keywordNull:
		p.Advance()
		return newCastExpression(nil, "")
	}
	name := p.Advance().Value
	if p.Current().Kind == tokenLeftParen {
		return p.parseFunctionCall(name)
	}
	if p.Current().Kind == tokenDot && p.Peek().Kind == tokenIdentifier {
		p.Advance()
		column := p.Advance().Value
		if p.Current().Kind == tokenLeftParen {
			return p.parseFunctionCall(name + "." + column)
		}
		return &ast.ColumnRefExpression{TableAlias: name, ColumnName: column, Span: ast.Span{}}
	}
	return &ast.ColumnRefExpression{TableAlias: "", ColumnName: name, Span: ast.Span{}}
}

// parseTypedLiteral parses DATE 'yyyy-mm-dd' or TIMESTAMP 'yyyy-mm-dd hh:mm:ss', the two
// typed literals ClickHouse reads, as a Date and a DateTime. The cursor is on DATE or
// TIMESTAMP.
//
// Returns ast.Expression which is the literal.
// Returns bool which is false, having consumed nothing, when no string follows, so the
// word names a column or a function.
func (p *parser) parseTypedLiteral() (ast.Expression, bool) {
	if !p.startsTypedLiteralAt(p.Position) {
		return nil, false
	}
	typeName := engineNameDate
	if p.Advance().Keyword(tokenIdentifier) == "TIMESTAMP" {
		typeName = engineNameDateTime
	}
	p.Advance()
	return &ast.LiteralExpression{TypeName: typeName}, true
}

// startsTypedLiteralAt reports whether a DATE or TIMESTAMP literal starts at a token: the
// word followed by a string.
//
// Takes position (int) which is the token's position.
//
// Returns bool which is true when one does.
func (p *parser) startsTypedLiteralAt(position int) bool {
	if position+1 >= len(p.Tokens) || p.Tokens[position+1].Kind != tokenString {
		return false
	}
	word := p.Tokens[position].Keyword(tokenIdentifier)
	return word == "DATE" || word == "TIMESTAMP"
}

// parseFunctionCall parses a call's argument list, and the parameters of a parametric
// aggregate such as quantile(0.5)(x), its FILTER clause and its OVER window. The function
// name has been consumed and the cursor is on the opening paren.
//
// coalesce and ifNull become a CoalesceExpression, whose result is NULL only when every
// argument is. DISTINCT or ALL before the arguments, as in count(DISTINCT x), is a
// modifier of the aggregate and does not change its type.
//
// Takes name (string) which is the function name.
//
// Returns ast.Expression which is the call, the window call or the coalesce.
func (p *parser) parseFunctionCall(name string) ast.Expression {
	arguments, ok := p.parseCallArguments()
	if !ok {
		return &ast.UnknownExpression{}
	}
	if p.Current().Kind == tokenLeftParen {
		arguments, ok = p.parseCallArguments()
		if !ok {
			return &ast.UnknownExpression{}
		}
	}
	if isCoalesceName(name) {
		return &ast.CoalesceExpression{Arguments: arguments}
	}
	call := newFunctionCall(name, readUnitArgument(name, arguments)...)
	if p.IsKeyword("FILTER") && p.Peek().Kind == tokenLeftParen {
		p.Advance()
		call.FilterExpression = p.parseFilterClause()
	}
	p.skipNullTreatment()
	if !p.MatchKeyword(keywordOver) {
		return call
	}
	if p.Current().Kind == tokenLeftParen {
		if p.skipParenthesised() != nil {
			return &ast.UnknownExpression{}
		}
	} else if p.Current().Kind == tokenIdentifier {
		p.Advance()
	}
	return &ast.WindowFunctionExpression{Function: call}
}

// parseCallArguments parses a parenthesised argument list: empty, a lone `*`, or
// expressions, optionally led by DISTINCT or ALL.
//
// Returns []ast.Expression which holds the arguments.
// Returns bool which is false when the list is not closed.
func (p *parser) parseCallArguments() ([]ast.Expression, bool) {
	p.Advance()
	if p.Current().Kind == tokenStar && p.Peek().Kind == tokenRightParen {
		p.Advance()
		p.Advance()
		return nil, true
	}
	if p.IsKeyword(keywordDistinct, kwAll) && p.Peek().Kind != tokenLeftParen && p.Peek().Kind != tokenComma {
		p.Advance()
	}
	arguments := p.parseExpressionList(tokenRightParen)
	return arguments, p.expectKind(tokenRightParen)
}

// parseFilterClause parses an aggregate's `FILTER (WHERE condition)` clause. The FILTER
// keyword has been consumed.
//
// Returns ast.Expression which is the condition.
func (p *parser) parseFilterClause() ast.Expression {
	p.Advance()
	if !p.MatchKeyword(kwWhere) {
		return &ast.UnknownExpression{}
	}
	condition := p.parseExpression()
	if !p.expectKind(tokenRightParen) {
		return &ast.UnknownExpression{}
	}
	return condition
}

// skipNullTreatment consumes a RESPECT NULLS or IGNORE NULLS modifier of a window
// function.
func (p *parser) skipNullTreatment() {
	if p.IsKeyword("RESPECT", "IGNORE") && p.Peek().IsKeyword(tokenIdentifier, kwNulls) {
		p.Advance()
		p.Advance()
	}
}

// parseCastCall parses `CAST(expr AS Type)` or the function form `CAST(expr, 'Type')`.
// The cursor is on the opening paren.
//
// Returns ast.Expression which is the cast.
func (p *parser) parseCastCall() ast.Expression {
	p.Advance()
	inner := p.parseExpression()
	typeName := ""
	switch {
	case p.MatchKeyword(kwAs):
		typeName = p.readCastTargetType()
	case p.Current().Kind == tokenComma && p.Peek().Kind == tokenString:
		p.Advance()
		typeName = p.Advance().Value
	}
	if typeName == "" || !p.expectKind(tokenRightParen) {
		return &ast.UnknownExpression{}
	}
	return newCastExpression(inner, typeName)
}

// parseIntervalLiteral parses `INTERVAL n unit`, `INTERVAL 'n' unit` or `INTERVAL 'n
// unit'`. The INTERVAL keyword has been consumed.
//
// A unit ClickHouse does not know, such as foo in INTERVAL 1 foo, is recorded as a syntax
// error at the unit, or at the string that holds it. A string holding several values and
// units, which ClickHouse reads as a tuple of intervals, is left untyped.
//
// Returns ast.Expression which is the interval, typed as ClickHouse's Interval type for
// its unit.
func (p *parser) parseIntervalLiteral() ast.Expression {
	if current := p.Current(); current.Kind == tokenString {
		text := strings.Fields(current.Value)
		if len(text) == intervalStringFields {
			p.Advance()
			return p.intervalOfUnit(current, text[1])
		}
		if len(text) != 1 {
			p.Advance()
			return &ast.UnknownExpression{}
		}
	}
	p.parseUnaryExpression()
	unit := p.Current()
	if unit.Kind != tokenIdentifier {
		return &ast.UnknownExpression{}
	}
	p.Advance()
	if unit.Quoted {
		p.recordSyntaxError(dialectkit.NewParseError(unit, messageUnknownIntervalUnit))
		return &ast.UnknownExpression{}
	}
	return p.intervalOfUnit(unit, unit.Value)
}

// intervalOfUnit types an interval by the unit a word names, such as IntervalDay for DAY,
// days or dd.
//
// Takes at (token) which is where the unit is written, for an error.
// Takes word (string) which is the unit as written.
//
// Returns ast.Expression which is the interval, or an unknown expression, with a syntax
// error recorded, when the word names no unit.
func (p *parser) intervalOfUnit(at token, word string) ast.Expression {
	unit, known := intervalUnitOf(word)
	if !known {
		p.recordSyntaxError(dialectkit.NewParseError(at, messageUnknownIntervalUnit))
		return &ast.UnknownExpression{}
	}
	return &ast.LiteralExpression{TypeName: intervalTypePrefix + unit}
}

// parseCaseExpression parses a searched `CASE WHEN c THEN r ...
//
// [ELSE e] END` or a simple `CASE x WHEN v THEN r ... END`, whose conditions compare x
// with each value. The CASE keyword has been consumed.
//
// Returns ast.Expression which is the CASE expression.
func (p *parser) parseCaseExpression() ast.Expression {
	var subject ast.Expression
	if !p.IsKeyword("WHEN") {
		subject = p.parseExpression()
	}
	expression := &ast.CaseWhenExpression{Branches: nil, ElseResult: nil}
	for p.MatchKeyword("WHEN") {
		condition := p.parseExpression()
		if subject != nil {
			condition = &ast.ComparisonExpression{Left: subject, Operator: "=", Right: condition, Quantifier: ""}
		}
		if !p.MatchKeyword("THEN") {
			return &ast.UnknownExpression{}
		}
		expression.Branches = append(expression.Branches, ast.CaseWhenBranch{Condition: condition, Result: p.parseExpression()})
	}
	if p.MatchKeyword("ELSE") {
		expression.ElseResult = p.parseExpression()
	}
	if len(expression.Branches) == 0 || !p.MatchKeyword("END") {
		return &ast.UnknownExpression{}
	}
	return expression
}

// parseExpressionList parses comma-separated expressions up to, but not including, a
// closing token. Each may carry the `AS name` ClickHouse allows on an expression
// anywhere, as in tuple(1 AS x, 'a' AS y).
//
// Takes closing (tokenKind) which is the token that ends the list.
//
// Returns []ast.Expression which holds the expressions in order.
func (p *parser) parseExpressionList(closing tokenKind) []ast.Expression {
	var expressions []ast.Expression
	for !p.AtEnd() && p.Current().Kind != closing {
		expressions = append(expressions, p.parseExpression())
		p.skipElementAlias()
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	return expressions
}

// skipElementAlias consumes the `AS name` an expression in a list carries. The name
// leaves the expression's value and type as they are: ClickHouse 25.4 does not name a
// tuple's elements after it, so tuple(1 AS x, 'a' AS y) is a Tuple(UInt8, String) whose
// elements are read by position.
func (p *parser) skipElementAlias() {
	if p.IsKeyword(kwAs) && p.Peek().Kind == tokenIdentifier {
		p.Advance()
		p.Advance()
	}
}

// parseNestedSelect parses the parenthesised SELECT at the cursor on a child parser and
// returns its analysis, looking through further parentheses wrapped around the whole
// SELECT, as in id IN ((SELECT ...)).
//
// Each extra pair of parentheses counts towards the nesting limit. Syntax errors,
// leftover tokens and excessive nesting are recorded on the parent parser, so the
// statement reports the failure instead of silently leaving the expression untyped.
//
// Returns *ast.RawQueryAnalysis which is the subquery's analysis, or nil.
func (p *parser) parseNestedSelect() *ast.RawQueryAnalysis {
	body, err := p.collectParenthesised()
	if err != nil {
		p.recordSyntaxError(err)
		return nil
	}
	layers, inner := peelParentheses(body)
	if layers >= p.maxParseDepth-p.expressionDepth {
		p.recordSyntaxError(p.depthError())
		return nil
	}
	nested := p.newChildParser(inner)
	nested.expressionDepth += layers
	analysis, err := nested.analyseSelect()
	if err == nil {
		err = nested.requireEnd()
	}
	if err != nil {
		p.recordSyntaxError(err)
		return nil
	}
	return analysis
}

// startsSubquery reports whether the token offset places ahead opens a parenthesised
// SELECT.
//
// It looks through opening parentheses only as far as the remaining nesting budget
// allows. Deeper subqueries are rejected by the parser's depth checks.
//
// Takes offset (int) which is the distance from the current token to the first token
// inside the parenthesis.
//
// Returns bool which is true when SELECT or WITH starts the parenthesised body.
func (p *parser) startsSubquery(offset int) bool {
	if p.PeekAt(offset-1).Kind != tokenLeftParen {
		return false
	}
	room := p.maxParseDepth - p.expressionDepth
	for further := 0; p.PeekAt(offset).Kind == tokenLeftParen; further++ {
		if further >= room {
			return false
		}
		offset++
	}
	return p.PeekAt(offset).IsKeyword(tokenIdentifier, "SELECT") || p.PeekAt(offset).IsKeyword(tokenIdentifier, kwWith)
}

// expectKind consumes the current token when it has the given kind.
//
// Takes kind (tokenKind) which is the expected kind.
//
// Returns bool which is true when the token matched and was consumed.
func (p *parser) expectKind(kind tokenKind) bool {
	if p.Current().Kind != kind {
		return false
	}
	p.Advance()
	return true
}

// readCastTargetType reads the type after `::` or `AS` in a cast, such as String,
// Decimal(18, 4), Nullable(DateTime64(3, 'UTC')) or Tuple(s String, n UInt16), as source
// text the type parser reads.
//
// Two words in a row keep a space between them, so a named tuple element keeps its name
// apart from its type.
//
// Returns string which is the type's text, or empty when no type name follows.
func (p *parser) readCastTargetType() string {
	if p.Current().Kind != tokenIdentifier {
		return ""
	}
	var builder strings.Builder
	previous := p.Advance()
	builder.WriteString(previous.Value)
	if p.Current().Kind != tokenLeftParen {
		return builder.String()
	}
	depth := 0
	for !p.AtEnd() {
		current := p.Advance()
		switch current.Kind {
		case tokenLeftParen:
			depth++
		case tokenRightParen:
			depth--
		default:
		}
		if isTypeWord(previous) && isTypeWord(current) {
			builder.WriteByte(' ')
		}
		builder.WriteString(current.Raw)
		if current.Kind == tokenComma {
			builder.WriteByte(' ')
		}
		previous = current
		if depth == 0 {
			break
		}
	}
	return builder.String()
}

// closingParenthesis returns the index of the parenthesis closing the one a token run
// opens with.
//
// Takes tokens ([]token) which is the run, opening with a parenthesis.
//
// Returns int which is the index, or -1 when the run never closes it.
func closingParenthesis(tokens []token) int {
	depth := 0
	for index, current := range tokens {
		switch current.Kind {
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

// isTypeWord reports whether a token of a type's text is a word, a name or a number,
// which needs a space to stay apart from a word next to it.
//
// Takes candidate (token) which is the token.
//
// Returns bool which is true for a word.
func isTypeWord(candidate token) bool {
	return candidate.Kind == tokenIdentifier || candidate.Kind == tokenNumber
}

// newFunctionCall builds an unqualified function call.
//
// Takes name (string) which is the function name.
// Takes arguments (...ast.Expression) which are the arguments.
//
// Returns *ast.FunctionCallExpression which is the call.
func newFunctionCall(name string, arguments ...ast.Expression) *ast.FunctionCallExpression {
	return &ast.FunctionCallExpression{FunctionName: name, Arguments: arguments, FilterExpression: nil, Schema: "", Span: ast.Span{}}
}

// newCastExpression builds a cast to a type. A cast to Nullable(T) can yield NULL from a
// value that is never NULL, so it keeps no operand, which the analyser reads as a result
// that may be NULL.
//
// Takes inner (ast.Expression) which is the cast operand.
// Takes typeName (string) which is the target type.
//
// Returns *ast.CastExpression which is the cast.
func newCastExpression(inner ast.Expression, typeName string) *ast.CastExpression {
	if isNullableTypeName(typeName) {
		inner = nil
	}
	return &ast.CastExpression{Inner: inner, TypeName: typeName}
}

// isNullableTypeName reports whether a type name is a Nullable(T) wrapper.
//
// Takes typeName (string) which is the type name.
//
// Returns bool which is true for Nullable(T).
func isNullableTypeName(typeName string) bool {
	trimmed := strings.TrimSpace(typeName)
	return len(trimmed) > len(typeWrapperNullable) && strings.EqualFold(trimmed[:len(typeWrapperNullable)], typeWrapperNullable) &&
		strings.HasPrefix(strings.TrimSpace(trimmed[len(typeWrapperNullable):]), "(")
}

// isCoalesceName reports whether a function returns its first non-NULL argument, as
// coalesce and ifNull do.
//
// Takes name (string) which is the function name.
//
// Returns bool which is true for coalesce and ifNull.
func isCoalesceName(name string) bool {
	return strings.EqualFold(name, "coalesce") || strings.EqualFold(name, "ifNull")
}

// elementFieldNames names the elements an access `.selector` reads, in turn: the field's
// name, or for a position `.N` the name ClickHouse gives the N-th unnamed tuple element.
//
// The tokeniser reads `.2.1` after a value as the number 2.1, which ClickHouse reads as
// the first element of the second, so each part of a number names one element.
//
// Takes selector (token) which is the identifier or number after the dot.
//
// Returns []string which holds the field names, outermost first.
func elementFieldNames(selector token) []string {
	if selector.Kind != tokenNumber {
		return []string{selector.Value}
	}
	positions := strings.Split(selector.Value, ".")
	names := make([]string, 0, len(positions))
	for _, position := range positions {
		names = append(names, synthesiseAnonymousFieldName(parseDecimalInt(position)))
	}
	return names
}

// literalNumberType types a numeric literal as ClickHouse does, from its scanned shape:
// an integer takes the smallest unsigned type that holds it, UInt8 to UInt64, and a
// literal with a fraction or an exponent, or an integer too large for UInt64, is a
// Float64.
//
// Takes value (string) which is the literal's text.
//
// Returns sqlmodel.SQLType which is the literal's type.
func literalNumberType(value string) sqlmodel.SQLType {
	if lexicalRules.Numbers.ClassifyNumber(value) != dialectkit.NumberInteger {
		return primitiveType(engineNameFloat64)
	}
	magnitude, err := parseIntegerLiteral(value)
	switch {
	case err != nil:
		return primitiveType(engineNameFloat64)
	case magnitude <= unsignedOneByteLimit:
		return primitiveType("UInt8")
	case magnitude <= unsignedTwoByteLimit:
		return primitiveType("UInt16")
	case magnitude <= unsignedFourByteLimit:
		return primitiveType("UInt32")
	default:
		return primitiveType("UInt64")
	}
}

// negativeLiteralTypeName types a negated numeric literal as ClickHouse does: an integer
// takes the smallest signed type that holds its negation, Int8 to Int64, and anything
// else is a Float64.
//
// Takes value (string) which is the literal's text without its minus sign.
//
// Returns string which is the type's name.
func negativeLiteralTypeName(value string) string {
	if lexicalRules.Numbers.ClassifyNumber(value) != dialectkit.NumberInteger {
		return engineNameFloat64
	}
	magnitude, err := parseIntegerLiteral(value)
	switch {
	case err != nil || magnitude > signedEightByteMagnitude:
		return engineNameFloat64
	case magnitude <= signedOneByteMagnitude:
		return "Int8"
	case magnitude <= signedTwoByteMagnitude:
		return "Int16"
	case magnitude <= signedFourByteMagnitude:
		return "Int32"
	default:
		return "Int64"
	}
}

// parseIntegerLiteral reads an integer literal's value: decimal digits, or a 0x, 0o or 0b
// base-prefixed literal.
//
// Takes value (string) which is the literal's text.
//
// Returns uint64 which is the value.
// Returns error when the value does not fit in 64 bits.
func parseIntegerLiteral(value string) (uint64, error) {
	if len(value) > radixPrefixLength && value[0] == '0' {
		switch value[1] {
		case 'x', 'X':
			return strconv.ParseUint(value[radixPrefixLength:], hexadecimalRadix, 64)
		case 'o', 'O':
			return strconv.ParseUint(value[radixPrefixLength:], octalRadix, 64)
		case 'b', 'B':
			return strconv.ParseUint(value[radixPrefixLength:], binaryRadix, 64)
		}
	}
	return strconv.ParseUint(value, decimalRadix, 64)
}

// parseDecimalInt parses a decimal-integer literal into an int.
//
// Callers validate the kind beforehand so this only sees positive integers in practice.
// The magnitude is bounded by maxTypeModifierValue so an overlong run of digits cannot
// overflow the accumulator, and once the bound is reached the cap is returned, which
// fails every real index check.
//
// Takes literal (string) which is the decimal-integer text to parse.
//
// Returns int which is the parsed value, zero when malformed, or the cap when the bound
// is hit.
func parseDecimalInt(literal string) int {
	value := 0
	for index := range len(literal) {
		character := literal[index]
		if character < '0' || character > '9' {
			return value
		}
		digit := int(character - '0')
		if value > (maxTypeModifierValue-digit)/decimalRadix {
			return maxTypeModifierValue
		}
		value = value*decimalRadix + digit
	}
	return value
}

// peelParentheses removes every pair of parentheses wrapped around the whole of a token
// run, in one pass over the run.
//
// The leading opening parentheses close in turn from the innermost out, each the first
// time the nesting falls back below it, so one walk finds where each closes; the outer
// ones that close at the matching places at the end of the run wrap it whole.
//
// Takes tokens ([]token) which is the run.
//
// Returns int which is how many pairs were removed.
// Returns []token which is the run inside the outermost parentheses that do not wrap it
// whole.
func peelParentheses(tokens []token) (int, []token) {
	leading := 0
	for leading < len(tokens) && tokens[leading].Kind == tokenLeftParen {
		leading++
	}
	if leading == 0 {
		return 0, tokens
	}
	closes := make([]int, leading)
	depth := 0
	for index, current := range tokens {
		switch current.Kind {
		case tokenLeftParen:
			depth++
		case tokenRightParen:
			depth--
			if depth >= 0 && depth < leading && closes[depth] == 0 {
				closes[depth] = index
			}
		default:
		}
	}
	layers := 0
	for layers < leading && closes[layers] == len(tokens)-1-layers {
		layers++
	}
	end := len(tokens) - layers
	return layers, tokens[layers:end:end]
}

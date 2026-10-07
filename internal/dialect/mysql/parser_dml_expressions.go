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
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// typeNameText is the type of a string literal, and of a value converted to text.
	typeNameText = "text"

	// pipesAsOrWarning is the warning about a || whose operand is text, which the server's
	// default SQL mode reads as a logical OR rather than string concatenation.
	pipesAsOrWarning = "|| is a logical OR in the server's default sql_mode, not string concatenation, so its text " +
		"operand is read as a number and the expression is true or false; join strings with CONCAT() instead"

	// maxAdjacentOperandDepth bounds the walk to the operand beside a ||.
	maxAdjacentOperandDepth = 64
)

var (
	// implicitFunctionIdentifiers lists identifiers that name a function even when written
	// without a trailing parenthesis pair.
	implicitFunctionIdentifiers = map[string]struct{}{
		"CURRENT_TIMESTAMP": {}, "CURRENT_DATE": {}, "CURRENT_TIME": {},
		"CURRENT_USER": {}, "LOCALTIME": {}, "LOCALTIMESTAMP": {},
		"UTC_TIMESTAMP": {}, "UTC_DATE": {}, "UTC_TIME": {},
	}

	// typedLiteralKeywords maps the keywords that type a following string literal, as in
	// DATE '2026-01-01', to the literal's type.
	typedLiteralKeywords = map[string]string{
		"DATE": "date", "TIME": "time", "TIMESTAMP": "datetime",
	}

	// intervalFieldKeywords lists the unit identifiers accepted after the value expression
	// in a MySQL INTERVAL literal.
	intervalFieldKeywords = map[string]struct{}{
		"YEAR": {}, "MONTH": {}, "DAY": {},
		"HOUR": {}, "MINUTE": {}, "SECOND": {},
		"MICROSECOND": {}, "WEEK": {}, "QUARTER": {},
		"YEAR_MONTH": {}, "DAY_HOUR": {}, "DAY_MINUTE": {},
		"DAY_SECOND": {}, "DAY_MICROSECOND": {},
		"HOUR_MINUTE": {}, "HOUR_SECOND": {}, "HOUR_MICROSECOND": {},
		"MINUTE_SECOND": {}, "MINUTE_MICROSECOND": {},
		"SECOND_MICROSECOND": {},
	}
)

// parseExpression parses a top-level SQL expression.
//
// Returns ast.Expression which is the parsed expression tree.
func (p *parser) parseExpression() ast.Expression {
	if p.expressionDepth >= p.maxParseDepth {
		p.recordSyntaxError(dialectkit.DepthError(p.maxParseDepth))
		return &ast.UnknownExpression{}
	}
	p.expressionDepth++
	defer func() { p.expressionDepth-- }()
	return p.parseOrExpression()
}

// parseOrExpression parses left-associative OR or `||` chains.
//
// The server's default SQL mode reads || as OR, so a chain is one logical OR however it
// is written. A || beside an operand that is text by its form, such as a string literal,
// is most likely meant as the string concatenation it is on other engines, so the first
// in each chain is warned of, whether that operand stands alone or opens or ends a
// comparison the OR reads around it, as in name || 'x' = ?.
//
// Returns ast.Expression which is the parsed expression tree.
func (p *parser) parseOrExpression() ast.Expression {
	left := p.parseXorExpression()
	previous := left
	warned := false
	for {
		operator := p.Current()
		if !p.MatchKeyword("OR") && !p.matchOperator("||") {
			return left
		}
		right := p.parseXorExpression()
		if !warned && operator.Kind == tokenOperator &&
			(p.isTextByForm(adjacentOperand(previous, false)) || p.isTextByForm(adjacentOperand(right, true))) {
			p.warn(operator, pipesAsOrWarning)
			warned = true
		}
		left = &ast.LogicalOpExpression{
			Operator: "OR",
			Operands: []ast.Expression{left, right},
		}
		previous = right
	}
}

// isTextByForm reports whether an expression is text by its form, whatever it reads: a
// string literal, a conversion to a text type, or a call of a built-in function whose
// every overload returns text.
//
// Takes expression (ast.Expression) which is the expression.
//
// Returns bool which is true for an expression that is text by its form.
func (p *parser) isTextByForm(expression ast.Expression) bool {
	switch typed := expression.(type) {
	case *ast.LiteralExpression:
		return typed.TypeName == typeNameText
	case *ast.CastExpression:
		return p.types != nil && p.types.normaliseWritten(typed.TypeName).Category == sqlmodel.TypeCategoryText
	case *ast.FunctionCallExpression:
		resultType := p.builtinResultType(strings.ToLower(typed.FunctionName))
		return resultType != nil && resultType.Category == sqlmodel.TypeCategoryText
	default:
		return false
	}
}

// sortedWarnings returns the warnings the parser and its children recorded, in source
// order.
//
// Returns []ast.Warning which holds the warnings, or nil when there are none.
func (p *parser) sortedWarnings() []ast.Warning {
	if len(p.warnings) == 0 {
		return nil
	}
	offsets := slices.Sorted(maps.Keys(p.warnings))
	warnings := make([]ast.Warning, 0, len(offsets))
	for _, offset := range offsets {
		warnings = append(warnings, p.warnings[offset])
	}
	return warnings
}

// warn records a warning about the SQL a token starts, once however often the token is
// parsed.
//
// Takes at (token) which is the token the warning is about.
// Takes message (string) which says how the engine reads it and how to write it instead.
func (p *parser) warn(at token, message string) {
	if p.warnings == nil {
		return
	}
	p.warnings[at.Start] = ast.Warning{Message: message, Span: ast.Span{Start: at.Start, End: at.End}}
}

// parseXorExpression parses left-associative XOR chains, which bind tighter than OR and
// looser than AND.
//
// Returns ast.Expression which is the parsed expression tree.
func (p *parser) parseXorExpression() ast.Expression {
	left := p.parseAndExpression()
	for p.MatchKeyword("XOR") {
		right := p.parseAndExpression()
		left = &ast.LogicalOpExpression{
			Operator: "XOR",
			Operands: []ast.Expression{left, right},
		}
	}
	return left
}

// parseAndExpression parses left-associative AND or `&&` chains.
//
// Returns ast.Expression which is the parsed expression tree.
func (p *parser) parseAndExpression() ast.Expression {
	left := p.parseNotExpression()
	for p.MatchKeyword(keywordAND) || p.matchOperator("&&") {
		right := p.parseNotExpression()
		left = &ast.LogicalOpExpression{
			Operator: "AND",
			Operands: []ast.Expression{left, right},
		}
	}
	return left
}

// matchOperator consumes the current token when it is the operator symbol.
//
// Takes symbol (string) which is the operator to match.
//
// Returns bool which is true when the operator matched and was consumed.
func (p *parser) matchOperator(symbol string) bool {
	if p.Current().Kind != tokenOperator || p.Current().Value != symbol {
		return false
	}
	p.Advance()
	return true
}

// parseNotExpression parses a unary NOT or `!` prefix.
//
// Returns ast.Expression which is the parsed expression tree.
func (p *parser) parseNotExpression() ast.Expression {
	if p.MatchKeyword(keywordNOT) {
		operand := p.parseComparisonExpression()
		return &ast.UnaryOpExpression{
			Operator: keywordNOT,
			Operand:  operand,
		}
	}
	if p.Current().Kind == tokenOperator && p.Current().Value == "!" {
		p.Advance()
		operand := p.parseComparisonExpression()
		return &ast.UnaryOpExpression{
			Operator: keywordNOT,
			Operand:  operand,
		}
	}
	return p.parseComparisonExpression()
}

// parseComparisonExpression parses comparisons and postfix predicates.
//
// Returns ast.Expression which is the parsed expression tree.
func (p *parser) parseComparisonExpression() ast.Expression {
	left := p.parseBitwiseExpression()

	if p.Current().Kind == tokenOperator && isComparisonOperator(p.Current().Value) {
		return p.parseComparisonOperator(left)
	}

	if p.IsKeyword("IS") {
		return p.parseIsSuffix(left)
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

// parsePostfixComparisonSuffix matches postfix comparison predicates.
//
// Takes left (ast.Expression) which is the LHS of the predicate.
//
// Returns ast.Expression which is the parsed predicate, or nil when no postfix predicate
// keyword follows.
func (p *parser) parsePostfixComparisonSuffix(left ast.Expression) ast.Expression {
	if p.IsKeyword("IN") {
		return p.parseInListSuffix(left)
	}
	if p.IsKeyword("BETWEEN") {
		return p.parseBetweenSuffix(left)
	}
	if p.IsKeyword("LIKE") {
		return p.parseLikeSuffix(left)
	}
	if p.IsKeyword("REGEXP") || p.IsKeyword("RLIKE") {
		return p.parseRegexpSuffix(left)
	}
	if p.IsKeyword("SOUNDS") {
		return p.parseSoundsLikeSuffix(left)
	}
	if p.IsKeyword("MEMBER") && p.Peek().IsKeyword(tokenIdentifier, "OF") {
		return p.parseMemberOfSuffix(left)
	}
	return nil
}

// parseMemberOfSuffix parses a value MEMBER OF (json array) predicate.
//
// Takes left (ast.Expression) which is the value tested for membership.
//
// Returns ast.Expression which is the parsed comparison.
func (p *parser) parseMemberOfSuffix(left ast.Expression) ast.Expression {
	p.Advance()
	p.Advance()
	right := p.parsePrimaryExpression()
	return &ast.ComparisonExpression{Operator: "MEMBER OF", Left: left, Right: right, Quantifier: ""}
}

// maybeNegate wraps expression in a NOT when negated is true.
//
// Takes negated (bool) which indicates whether to negate.
// Takes expression (ast.Expression) which is the inner expression.
//
// Returns ast.Expression which is the negated or original expression.
func (*parser) maybeNegate(negated bool, expression ast.Expression) ast.Expression {
	if negated {
		return &ast.UnaryOpExpression{Operator: keywordNOT, Operand: expression}
	}
	return expression
}

// parseLikeSuffix parses a LIKE predicate with an optional ESCAPE clause.
//
// Takes left (ast.Expression) which is the LHS of the predicate.
//
// Returns ast.Expression which is the parsed comparison.
func (p *parser) parseLikeSuffix(left ast.Expression) ast.Expression {
	keyword := p.Advance().Keyword(tokenIdentifier)
	right := p.parseBitwiseExpression()
	if p.MatchKeyword("ESCAPE") {
		p.parseBitwiseExpression()
	}
	return &ast.ComparisonExpression{Operator: keyword, Left: left, Right: right, Quantifier: ""}
}

// parseRegexpSuffix parses a REGEXP or RLIKE predicate.
//
// Takes left (ast.Expression) which is the LHS of the predicate.
//
// Returns ast.Expression which is the parsed comparison.
func (p *parser) parseRegexpSuffix(left ast.Expression) ast.Expression {
	p.Advance()
	right := p.parseBitwiseExpression()
	return &ast.ComparisonExpression{Operator: "REGEXP", Left: left, Right: right, Quantifier: ""}
}

// parseSoundsLikeSuffix parses a SOUNDS LIKE predicate.
//
// Takes left (ast.Expression) which is the LHS of the predicate.
//
// Returns ast.Expression which is the parsed comparison.
func (p *parser) parseSoundsLikeSuffix(left ast.Expression) ast.Expression {
	p.Advance()
	p.MatchKeyword("LIKE")
	right := p.parseBitwiseExpression()
	return &ast.ComparisonExpression{Operator: "SOUNDS LIKE", Left: left, Right: right, Quantifier: ""}
}

// parseComparisonOperator parses a comparison with optional ANY/ALL/SOME.
//
// Takes left (ast.Expression) which is the LHS of the comparison.
//
// Returns ast.Expression which is the parsed comparison.
func (p *parser) parseComparisonOperator(left ast.Expression) ast.Expression {
	operator := p.Advance().Value
	if p.MatchKeyword("ANY") || p.MatchKeyword(keywordALL) || p.MatchKeyword("SOME") {
		if err := p.skipParenthesisedIfPresent(); err != nil {
			p.recordSyntaxError(fmt.Errorf("parsing %s ANY/ALL operand: %w", operator, err))
		}
		return &ast.ComparisonExpression{Operator: operator, Left: left, Right: &ast.UnknownExpression{}, Quantifier: ""}
	}
	right := p.parseBitwiseExpression()
	return &ast.ComparisonExpression{Operator: operator, Left: left, Right: right, Quantifier: ""}
}

// parseBitwiseExpression parses bitwise &, |, ^, <<, and >> chains.
//
// Returns ast.Expression which is the parsed expression tree.
func (p *parser) parseBitwiseExpression() ast.Expression {
	left := p.parseJSONExpression()

	for p.Current().Kind == tokenOperator &&
		(p.Current().Value == "&" || p.Current().Value == "|" || p.Current().Value == "^" ||
			p.Current().Value == "<<" || p.Current().Value == ">>") {
		operator := p.Advance().Value
		right := p.parseJSONExpression()
		left = &ast.BinaryOpExpression{Operator: operator, Left: left, Right: right, MayBeNull: false}
	}

	return left
}

// parseJSONExpression parses a JSON `->` or `->>` operator, which MySQL writes only as
// column->'path': a column on the left and a string literal path on the right, so a chain
// of them, or any other operand, is a syntax error.
//
// Returns ast.Expression which is the parsed expression tree.
func (p *parser) parseJSONExpression() ast.Expression {
	left := p.parseAddExpression()
	if p.Current().Kind != tokenArrow && p.Current().Kind != tokenDoubleArrow {
		return left
	}
	if _, column := left.(*ast.ColumnRefExpression); !column {
		p.reportSyntaxError("the " + p.Current().Value + " operator takes a column on its left")
	}
	operator := p.Advance().Value
	if p.Current().Kind != tokenString {
		p.reportSyntaxError("the " + operator + " operator takes a JSON path string on its right")
		return &ast.BinaryOpExpression{Operator: operator, Left: left, Right: p.parseAddExpression(), MayBeNull: false}
	}
	p.Advance()
	return &ast.BinaryOpExpression{Operator: operator, Left: left, Right: &ast.LiteralExpression{TypeName: typeNameText}, MayBeNull: false}
}

// parseAddExpression parses additive `+` and `-` chains.
//
// Returns ast.Expression which is the parsed expression tree.
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

// parseMulExpression parses multiplicative *, /, %, DIV, and MOD chains.
//
// The engine answers a division or remainder by zero with NULL, so one whose divisor is
// no constant other than zero may be NULL whatever its operands.
//
// Returns ast.Expression which is the parsed expression tree.
func (p *parser) parseMulExpression() ast.Expression {
	left := p.parseUnaryExpression()

	for {
		operator, isOperator := p.mulOperator()
		if !isOperator {
			break
		}
		p.Advance()
		divisorStart := p.Position
		right := p.parseUnaryExpression()
		divides := operator != "*" && !p.isNonZeroConstant(divisorStart, p.Position)
		left = &ast.BinaryOpExpression{Operator: operator, Left: left, Right: right, MayBeNull: divides}
	}

	return left
}

// isNonZeroConstant reports whether the tokens from start to end are a number other than
// zero, optionally signed, such as 2, -0.5 or 0x10.
//
// Takes start (int) which is the index of the first token.
// Takes end (int) which is one past the index of the last token.
//
// Returns bool which is true for a constant other than zero.
func (p *parser) isNonZeroConstant(start, end int) bool {
	if end-start == 2 && p.Tokens[start].Kind == tokenOperator &&
		(p.Tokens[start].Value == "-" || p.Tokens[start].Value == "+") {
		start++
	}
	if end-start != 1 || start >= len(p.Tokens) || p.Tokens[start].Kind != tokenNumber {
		return false
	}
	return !isZeroNumber(p.Tokens[start].Value)
}

// mulOperator reports whether the current token is a `*` family operator: *, /, % or the
// keyword operators DIV and MOD.
//
// Returns string which is the operator, with DIV and MOD upper-cased.
// Returns bool which is true when the current token is such an operator.
func (p *parser) mulOperator() (string, bool) {
	current := p.Current()
	switch {
	case current.Kind == tokenStar:
		return "*", true
	case current.Kind == tokenOperator && (current.Value == "/" || current.Value == "%"):
		return current.Value, true
	case current.IsKeyword(tokenIdentifier, keywordDIV), current.IsKeyword(tokenIdentifier, keywordMOD):
		return current.Keyword(tokenIdentifier), true
	default:
		return "", false
	}
}

// parseUnaryExpression parses unary -, +, ~, NOT, or BINARY prefixes.
//
// Returns ast.Expression which is the parsed expression tree.
func (p *parser) parseUnaryExpression() ast.Expression {
	if p.Current().Kind == tokenOperator &&
		(p.Current().Value == "-" || p.Current().Value == "+" || p.Current().Value == "~") {
		operator := p.Advance().Value
		inner := p.parseCollateExpression()
		if _, ok := inner.(*ast.LiteralExpression); ok {
			return inner
		}
		return &ast.UnaryOpExpression{Operator: operator, Operand: inner}
	}
	if p.MatchKeyword(keywordNOT) {
		inner := p.parseCollateExpression()
		return &ast.UnaryOpExpression{Operator: keywordNOT, Operand: inner}
	}
	if p.MatchKeyword("BINARY") {
		inner := p.parseCollateExpression()
		return &ast.CastExpression{TypeName: "binary", Inner: inner}
	}
	return p.parseCollateExpression()
}

// parseCollateExpression parses an optional COLLATE suffix.
//
// Returns ast.Expression which is the parsed expression tree.
func (p *parser) parseCollateExpression() ast.Expression {
	left := p.parsePrimaryExpression()

	if p.MatchKeyword(keywordCOLLATE) {
		if p.Current().Kind == tokenIdentifier || p.Current().Kind == tokenString {
			p.Advance()
		}
	}

	return left
}

// parsePrimaryExpression parses a primary expression atom. String literals written next
// to each other, as in 'a' "b", are one string, as MySQL joins them.
//
// Returns ast.Expression which is the parsed expression tree.
func (p *parser) parsePrimaryExpression() ast.Expression {
	current := p.Current()

	switch current.Kind {
	case tokenNumber:
		return p.parseNumberLiteral(current)
	case tokenString:
		for p.Current().Kind == tokenString {
			p.Advance()
		}
		return &ast.LiteralExpression{TypeName: typeNameText}
	case tokenHexString:
		p.Advance()
		return &ast.LiteralExpression{TypeName: "varbinary"}
	case tokenBitString:
		p.Advance()
		return &ast.LiteralExpression{TypeName: "bit"}
	case tokenQuestionMark, tokenNamedParameter:
		return p.parseParameterExpression()
	case tokenLeftParen:
		return p.parseParenthesisedExpression()
	case tokenIdentifier:
		return p.parseIdentifierExpression()
	case tokenUserVariable, tokenSystemVariable:
		p.Advance()
		return &ast.UnknownExpression{}
	default:
		p.requireStrictExpression("expected an expression")
		p.Advance()
		return &ast.UnknownExpression{}
	}
}

// parseNumberLiteral types a numeric literal from its scanned shape.
//
// Takes current (token) which is the number token being consumed.
//
// Returns ast.Expression which is the typed literal expression.
func (p *parser) parseNumberLiteral(current token) ast.Expression {
	p.Advance()
	return &ast.LiteralExpression{TypeName: numberLiteralType(current.Value)}
}

// parseParameterExpression registers a bare parameter token.
//
// When an INSERT ... SELECT projection context is active the placeholder is assigned the
// matching INSERT target column (by the projection ordinal) with
// ParameterContextAssignment, mirroring the VALUES path; this lets the analyser type the
// placeholder from the target column even though the SELECT body itself has no scope
// column for it. Otherwise the placeholder is registered as context-unknown and a later
// marker may retag it.
//
// Returns ast.Expression which is the parameter, by the number it is registered under, so
// the analyser types it from what it is compared with when nothing else types it.
func (p *parser) parseParameterExpression() ast.Expression {
	parameterToken := p.Current()
	p.Advance()

	context := sqlmodel.ParameterContextUnknown
	var columnReference *ast.ColumnReference
	if reference := p.insertProjectionColumnReference(); reference != nil {
		context = sqlmodel.ParameterContextAssignment
		columnReference = reference
	}

	number := p.registerParameterFromToken(parameterToken, context, columnReference, nil)
	return &ast.ParameterExpression{Number: number}
}

// insertProjectionColumnReference returns the INSERT target column reference for the
// projection item currently being parsed, or nil when no INSERT ... SELECT projection
// context is active or the ordinal is beyond the declared target column list.
//
// Returns *ast.ColumnReference which is the target column reference or nil.
func (p *parser) insertProjectionColumnReference() *ast.ColumnReference {
	if p.insertProjectionColumns == nil {
		return nil
	}
	return p.columnReferenceForIndex(
		p.insertProjectionTable, p.insertProjectionColumns, p.insertProjectionIndex)
}

// parseParenthesisedExpression parses a `(expr)`, a row constructor `(expr, expr, ...)`
// or a scalar subquery.
//
// Returns ast.Expression which is the parsed expression tree, a row for a row
// constructor.
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
	p.closeParenthesis()
	if len(elements) > 1 {
		return &ast.RowValueExpression{Elements: elements}
	}
	return elements[0]
}

// closeParenthesis consumes the ")" that closes a parenthesised expression, and records a
// syntax error when it is missing and the parser reads a condition strictly.
func (p *parser) closeParenthesis() {
	if p.Current().Kind == tokenRightParen {
		p.Advance()
		return
	}
	p.requireStrictExpression("expected ')'")
}

// parseIdentifierExpression dispatches an identifier to its handler.
//
// Returns ast.Expression which is the parsed expression tree.
func (p *parser) parseIdentifierExpression() ast.Expression {
	first := p.Position
	return p.locate(p.parseIdentifierRootedExpression(), first)
}

// parseIdentifierRootedExpression parses an identifier-rooted expression for
// parseIdentifierExpression, which records where its column or function was written.
//
// Returns ast.Expression which is the parsed expression tree.
func (p *parser) parseIdentifierRootedExpression() ast.Expression {
	if literal := p.parsePrefixedStringLiteral(); literal != nil {
		return literal
	}

	upper := p.Current().Keyword(tokenIdentifier)

	if handler := p.identifierExpressionHandler(upper); handler != nil {
		return handler()
	}

	if _, isImplicit := implicitFunctionIdentifiers[upper]; isImplicit {
		p.Advance()
		p.skipFractionalSecondsPrecision()
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

// skipFractionalSecondsPrecision consumes the parentheses that may follow a function
// written without them, such as CURRENT_TIMESTAMP, and the fractional seconds precision
// they may hold, as in UTC_TIMESTAMP(6), which changes the value's precision but not its
// type.
func (p *parser) skipFractionalSecondsPrecision() {
	if p.Current().Kind != tokenLeftParen {
		return
	}
	p.Advance()
	if p.Current().Kind == tokenNumber {
		p.Advance()
	}
	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}
}

// parsePrefixedStringLiteral parses a string literal written with a prefix: a character
// set introducer such as _utf8mb4'text', a national string N'text', or a typed temporal
// literal such as DATE '2026-01-01'.
//
// Returns ast.Expression which is the typed literal, or nil when the current token does
// not prefix a string literal.
func (p *parser) parsePrefixedStringLiteral() ast.Expression {
	current := p.Current()
	next := p.Peek()
	if current.Quoted || (next.Kind != tokenString && next.Kind != tokenHexString) {
		return nil
	}
	upper := current.Keyword(tokenIdentifier)
	typeName, typed := typedLiteralKeywords[upper]
	switch {
	case typed && next.Kind == tokenString:
	case strings.HasPrefix(current.Value, "_") || upper == "N":
		typeName = typeNameText
		if next.Kind == tokenHexString {
			typeName = "varbinary"
		}
	default:
		return nil
	}
	p.Advance()
	p.Advance()
	return &ast.LiteralExpression{TypeName: typeName}
}

// identifierExpressionHandler returns the parser for a special keyword.
//
// Takes upper (string) which is the upper-cased identifier value.
//
// Returns func() ast.Expression which is the matched parser, or nil when the identifier
// has no special handling.
func (p *parser) identifierExpressionHandler(upper string) func() ast.Expression {
	switch upper {
	case keywordNULL:
		return p.parseNullLiteral
	case "TRUE", "FALSE":
		return p.parseBooleanLiteral
	case "CAST":
		return p.parseCastFunctionExpression
	case "CONVERT":
		return p.parseConvertExpression
	case "COALESCE":
		return p.parseCoalesceExpression
	case "CASE":
		return p.parseCaseExpression
	case keywordEXISTS:
		return func() ast.Expression {
			p.Advance()
			return p.parseExistsSubquery()
		}
	case keywordNOT:
		return func() ast.Expression { return &ast.UnknownExpression{} }
	case "INTERVAL":
		return p.parseIntervalLiteral
	case "ROW":
		return p.parseRowConstructorExpression
	case "IF":
		return p.parseIfExpression
	case "MATCH":
		if p.Peek().Kind == tokenLeftParen {
			return p.parseMatchAgainstExpression
		}
		return nil
	case "NEXT", "PREVIOUS":
		if p.sequences && p.Peek().IsKeyword(tokenIdentifier, "VALUE") {
			return p.parseSequenceValueExpression
		}
		return nil
	default:
		return nil
	}
}

// parseSequenceValueExpression parses NEXT VALUE FOR name, which advances a sequence and
// reads its next value, as NEXTVAL(name) does, or PREVIOUS VALUE FOR name, which reads
// the value the session read last, as LASTVAL(name) does.
//
// Returns ast.Expression which is the equivalent call, the sequence a text argument.
func (p *parser) parseSequenceValueExpression() ast.Expression {
	functionName := "nextval"
	if p.Advance().Keyword(tokenIdentifier) == "PREVIOUS" {
		functionName = "lastval"
	}
	p.Advance()
	if !p.MatchKeyword(keywordFOR) {
		p.reportSyntaxError("expected FOR and a sequence after VALUE")
	}
	if _, _, err := p.parseSchemaQualifiedName(); err != nil {
		p.recordSyntaxError(err)
	}
	return &ast.FunctionCallExpression{
		FunctionName:     functionName,
		Schema:           "",
		Arguments:        []ast.Expression{&ast.LiteralExpression{TypeName: typeNameText}},
		FilterExpression: nil,
		Span:             ast.Span{},
	}
}

// parseMatchAgainstExpression parses a full-text search, MATCH (columns) AGAINST (expr
// [search modifier]), whose result is a DOUBLE relevance. A placeholder that is the whole
// search expression is typed text, as the engine takes the search as a string.
//
// Returns ast.Expression which is the relevance, typed as DOUBLE.
func (p *parser) parseMatchAgainstExpression() ast.Expression {
	p.Advance()
	p.parseParenthesisedExpressionList()
	if !p.MatchKeyword("AGAINST") {
		p.reportSyntaxError("expected AGAINST after MATCH (...)")
		return &ast.UnknownExpression{}
	}
	if p.Current().Kind != tokenLeftParen {
		p.reportSyntaxError("expected '(' after AGAINST")
		return &ast.UnknownExpression{}
	}
	p.Advance()
	referenceCountBefore := len(p.parameterReferences)
	if _, isParameter := p.parseBitwiseExpression().(*ast.ParameterExpression); isParameter {
		p.annotateCastParameter("text", referenceCountBefore)
	}
	for p.Current().Kind == tokenIdentifier && p.Current().Kind != tokenRightParen {
		if !p.IsKeyword(keywordIN, "NATURAL", "LANGUAGE", "MODE", "BOOLEAN", keywordWITH, "QUERY", "EXPANSION") {
			break
		}
		p.Advance()
	}
	if p.Current().Kind != tokenRightParen {
		p.reportSyntaxError("expected ')' to close AGAINST")
		return &ast.UnknownExpression{}
	}
	p.Advance()
	return &ast.LiteralExpression{TypeName: "double"}
}

// parseNullLiteral consumes a NULL token.
//
// Returns ast.Expression which is nil to represent SQL NULL.
func (p *parser) parseNullLiteral() ast.Expression {
	p.Advance()
	return nil
}

// parseBooleanLiteral consumes TRUE or FALSE as a tinyint literal.
//
// Returns ast.Expression which is the typed literal expression.
func (p *parser) parseBooleanLiteral() ast.Expression {
	p.Advance()
	return &ast.LiteralExpression{TypeName: "tinyint"}
}

// parseIntervalLiteral parses an INTERVAL value unit expression as its amount converted
// to an interval, so a parameter giving the amount stays in the expression; a placeholder
// that is the whole amount of a simple unit is a number of the unit.
//
// Returns ast.Expression which is the interval.
func (p *parser) parseIntervalLiteral() ast.Expression {
	p.Advance()
	referenceCountBefore := len(p.parameterReferences)
	amount := p.parseExpression()
	if p.Current().Kind == tokenIdentifier {
		unit := p.Current().Keyword(tokenIdentifier)
		if _, ok := intervalFieldKeywords[unit]; ok {
			p.Advance()
		}
		if _, isParameter := amount.(*ast.ParameterExpression); isParameter && intervalUnitKeywords[unit] {
			p.annotateCastParameter("bigint", referenceCountBefore)
		}
	}
	return &ast.CastExpression{Inner: amount, TypeName: "interval"}
}

// parseRowConstructorExpression parses a ROW(...) constructor.
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
	p.closeParenthesis()
	return &ast.RowValueExpression{Elements: elements}
}

// parseIfExpression parses IF(cond, then, else) as a function call.
//
// Returns ast.Expression which is the parsed function call.
func (p *parser) parseIfExpression() ast.Expression {
	p.Advance()
	if p.Current().Kind != tokenLeftParen {
		return &ast.FunctionCallExpression{
			FunctionName:     "if",
			FilterExpression: nil,
			Schema:           "",
			Arguments:        nil,
			Span:             ast.Span{},
		}
	}
	return p.parseFunctionCall("if", "")
}

// parseColumnOrFunctionReference parses an unqualified column or call.
//
// Returns ast.Expression which is the parsed expression tree.
func (p *parser) parseColumnOrFunctionReference() ast.Expression {
	name := p.Advance().Value

	if p.Current().Kind == tokenDot {
		return p.parseDotQualifiedIdentifier(name)
	}

	if p.Current().Kind == tokenLeftParen {
		return p.parseFunctionCall(name, "")
	}

	return &ast.ColumnRefExpression{
		TableAlias: "",
		ColumnName: name,
		Span:       ast.Span{},
	}
}

// parseDotQualifiedIdentifier parses `<alias>.<column>` or related forms.
//
// Takes name (string) which is the identifier preceding the dot.
//
// Returns ast.Expression which is the parsed expression tree.
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

// parseSchemaQualifiedReference parses `<schema>.<table>.<column>` references.
//
// Takes schema (string) which is the leading schema identifier.
// Takes table (string) which is the middle table identifier.
//
// Returns ast.Expression which is the parsed expression tree.
func (p *parser) parseSchemaQualifiedReference(schema string, table string) ast.Expression {
	p.Advance()
	if p.Current().Kind != tokenIdentifier {
		return &ast.ColumnRefExpression{TableAlias: schema, ColumnName: table, Span: ast.Span{}}
	}
	third := p.Advance().Value
	if p.Current().Kind == tokenLeftParen {
		return p.parseFunctionCall(third, schema+"."+table)
	}
	return &ast.ColumnRefExpression{TableAlias: table, ColumnName: third, Span: ast.Span{}}
}

// parseIsSuffix parses an IS NULL, IS TRUE, or related predicate.
//
// Takes left (ast.Expression) which is the LHS of the predicate.
//
// Returns ast.Expression which is the parsed IS predicate.
func (p *parser) parseIsSuffix(left ast.Expression) ast.Expression {
	p.Advance()
	negated := p.MatchKeyword(keywordNOT)

	if p.MatchKeyword(keywordNULL) {
		return &ast.IsNullExpression{Inner: left, Negated: negated}
	}

	if p.MatchKeyword("TRUE") || p.MatchKeyword("FALSE") || p.MatchKeyword("UNKNOWN") {
		return &ast.IsNullExpression{Inner: left, Negated: negated}
	}

	p.requireStrictExpression("expected NULL, TRUE, FALSE or UNKNOWN after IS")
	return &ast.IsNullExpression{Inner: left, Negated: negated}
}

// parseInListSuffix parses an IN(...) predicate, value list or subquery.
//
// Tags every parameter reference the IN list registered, whatever its number, with the
// LHS column when the LHS is a bare column reference.
//
// Takes left (ast.Expression) which is the LHS of the predicate.
//
// Returns ast.Expression which is the parsed IN expression.
func (p *parser) parseInListSuffix(left ast.Expression) ast.Expression {
	p.Advance()

	if p.isSubqueryStart() {
		innerAnalysis, err := p.analyseSubqueryBody()
		if errors.Is(err, errSubquerySkipped) {
			return &ast.InListExpression{Inner: left, Values: nil}
		}
		if err != nil {
			return &ast.UnknownExpression{}
		}
		return &ast.InListExpression{
			Inner:  left,
			Values: []ast.Expression{&ast.ScalarSubqueryExpression{InnerQuery: innerAnalysis}},
		}
	}

	referenceCountBefore := len(p.parameterReferences)
	values := p.parseParenthesisedExpressionList()

	p.retagParameterReferences(referenceCountBefore, sqlmodel.ParameterContextInList, left)

	return &ast.InListExpression{Inner: left, Values: values}
}

// parseBetweenSuffix parses a BETWEEN low AND high predicate.
//
// Tags every parameter reference the range registered, whatever its number, with the LHS
// column when the LHS is a bare column reference.
//
// Takes left (ast.Expression) which is the LHS of the predicate.
//
// Returns ast.Expression which is the parsed BETWEEN expression.
func (p *parser) parseBetweenSuffix(left ast.Expression) ast.Expression {
	p.Advance()

	referenceCountBefore := len(p.parameterReferences)
	low := p.parseAddExpression()
	if !p.MatchKeyword(keywordAND) {
		p.requireStrictExpression("expected AND in BETWEEN")
	}
	high := p.parseAddExpression()

	p.retagParameterReferences(referenceCountBefore, sqlmodel.ParameterContextBetween, left)

	return &ast.BetweenExpression{Inner: left, Low: low, High: high}
}

// retagParameterReferences gives every parameter reference registered since
// referenceCountBefore a context, and binds it to the predicate's left-hand column when
// that is a bare column reference. Only the references the predicate registered are
// visited, so the cost follows the predicate's own size.
//
// Takes referenceCountBefore (int) which is the number of parameter references before the
// predicate's operands were parsed.
// Takes context (sqlmodel.ParameterContext) which is the predicate's context.
// Takes left (ast.Expression) which is the predicate's left-hand side.
func (p *parser) retagParameterReferences(referenceCountBefore int, context sqlmodel.ParameterContext, left ast.Expression) {
	var columnReference *ast.ColumnReference
	if columnExpression, ok := left.(*ast.ColumnRefExpression); ok {
		columnReference = &ast.ColumnReference{
			TableAlias: columnExpression.TableAlias,
			ColumnName: columnExpression.ColumnName,
		}
	}
	for index := max(referenceCountBefore, 0); index < len(p.parameterReferences); index++ {
		p.parameterReferences[index].Context = context
		if columnReference != nil {
			p.parameterReferences[index].ColumnReference = columnReference
		}
	}
}

// parseParenthesisedExpressionList parses a `(expr, expr, ...)` tuple.
//
// Returns []ast.Expression which is the parsed expression list, or nil when no opening
// parenthesis is present.
func (p *parser) parseParenthesisedExpressionList() []ast.Expression {
	if p.Current().Kind != tokenLeftParen {
		p.requireStrictExpression("expected '('")
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

	p.closeParenthesis()
	return values
}

// parseColumnListTypeName parses the type of a column a table function's column list
// declares.
//
// Handles schema-qualified names, multi-word type keywords such as CHARACTER VARYING and
// DOUBLE PRECISION, and numeric modifiers, which stay in the name as appendTypeModifiers
// writes them.
//
// Returns string which is the assembled type name.
func (p *parser) parseColumnListTypeName() string {
	if p.Current().Kind != tokenIdentifier {
		return ""
	}

	var builder strings.Builder
	builder.WriteString(p.Advance().Value)

	p.appendSchemaQualifier(&builder)
	p.appendMultiWordTypeKeywords(&builder)

	if err := p.appendTypeModifiers(&builder); err != nil {
		p.recordSyntaxError(fmt.Errorf("parsing type modifiers of %s: %w", builder.String(), err))
	}

	return builder.String()
}

// appendTypeModifiers consumes the parenthesised modifier list that may follow a type
// name.
//
// One or two numeric modifiers are appended in a form NormaliseTypeName can read, such as
// (10,2) for DECIMAL or (20) for VARCHAR. Other lists, including ENUM values, are
// consumed but omitted. A list without a preceding type name is also omitted.
//
// Takes builder (*strings.Builder) which holds the type name and receives the modifiers.
//
// Returns error when the list is not closed.
func (p *parser) appendTypeModifiers(builder *strings.Builder) error {
	modifiers, numeric := p.typeModifiersAhead(0)
	if err := p.skipParenthesisedIfPresent(); err != nil {
		return err
	}
	if numeric && builder.Len() > 0 {
		builder.WriteString(formatTypeModifiers(modifiers))
	}
	return nil
}

// typeModifiersAhead reads, without consuming anything, the modifier list a '(' offset
// places after the current token opens when it holds one to maxTypeModifiers plain
// numbers separated by commas.
//
// Takes offset (int) which is the distance from the current token to the '('.
//
// Returns []int which holds the modifiers in order, as typeModifierMagnitude reads them.
// Returns bool which is false when the token at offset opens no such list.
func (p *parser) typeModifiersAhead(offset int) ([]int, bool) {
	if p.PeekAt(offset).Kind != tokenLeftParen {
		return nil, false
	}
	modifiers := make([]int, 0, maxTypeModifiers)
	for at := offset + 1; len(modifiers) < maxTypeModifiers; at += 2 {
		value, isModifier := typeModifierMagnitude(p.PeekAt(at))
		if !isModifier {
			return nil, false
		}
		modifiers = append(modifiers, value)
		switch p.PeekAt(at + 1).Kind {
		case tokenRightParen:
			return modifiers, true
		case tokenComma:
		default:
			return nil, false
		}
	}
	return nil, false
}

// appendSchemaQualifier appends `.identifier` when a dot follows.
//
// Takes builder (*strings.Builder) which receives the appended text.
func (p *parser) appendSchemaQualifier(builder *strings.Builder) {
	if p.Current().Kind == tokenDot && p.Peek().Kind == tokenIdentifier {
		p.Advance()
		builder.WriteByte('.')
		builder.WriteString(p.Advance().Value)
	}
}

// appendMultiWordTypeKeywords appends trailing words of a compound type.
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

// analyseSubqueryBody analyses a parenthesised subquery in a depth-inheriting child
// parser.
//
// It collects the parenthesised subquery and analyses it with analyseNestedSelect. The
// EXISTS, IN and scalar subquery parsers share it so the child setup lives in one place.
// A parser that does not analyse subqueries again, as when it records a clause's
// expressions after the clause scan, takes the analysis the scan made of the subquery.
//
// Returns *ast.RawQueryAnalysis which is the inner query's analysis.
// Returns error which is non-nil, and recorded as the statement's syntax error, when the
// subquery is unbalanced or malformed, so its parameters are never silently dropped.
func (p *parser) analyseSubqueryBody() (*ast.RawQueryAnalysis, error) {
	open := p.Current()
	innerTokens, collectError := p.collectParenthesised()
	if collectError != nil {
		p.recordSyntaxError(collectError)
		return nil, collectError
	}
	if scanned, found := p.scannedSubqueries[open.Start]; found && p.skipSubqueries {
		return scanned, nil
	}
	innerAnalysis, analyseError := p.analyseNestedSelect(innerTokens)
	if analyseError == nil {
		p.scannedSubqueries[open.Start] = innerAnalysis
	}
	return innerAnalysis, analyseError
}

// analyseNestedSelect analyses tokens as a complete query in a child parser that inherits
// the parameter and depth state, requires the query to end where the tokens do, and
// splices the child's parameters back into this parser.
//
// Takes innerTokens ([]token) which are the tokens between a subquery's parentheses.
//
// Returns *ast.RawQueryAnalysis which is the inner query's analysis.
// Returns error which is non-nil, and recorded as the statement's syntax error, when the
// query is malformed or leaves tokens unparsed.
func (p *parser) analyseNestedSelect(innerTokens []token) (*ast.RawQueryAnalysis, error) {
	if p.skipSubqueries {
		return nil, errSubquerySkipped
	}
	childParser := p.newNestedQueryParser(innerTokens)
	innerAnalysis, analyseError := childParser.analyseSelect()
	if analyseError == nil {
		analyseError = childParser.requireEnd()
	}
	p.adoptSyntaxError(childParser)
	if analyseError != nil {
		p.recordSyntaxError(analyseError)
		return nil, analyseError
	}
	p.parameterCount = childParser.parameterCount
	p.parameterReferences = append(p.parameterReferences, childParser.parameterReferences...)

	return innerAnalysis, nil
}

// parseExistsSubquery parses the `(SELECT ...)` of an EXISTS predicate.
//
// Returns ast.Expression which is the parsed EXISTS expression.
func (p *parser) parseExistsSubquery() ast.Expression {
	innerAnalysis, err := p.analyseSubqueryBody()
	if err != nil {
		return &ast.ExistsExpression{}
	}
	return &ast.ExistsExpression{InnerQuery: innerAnalysis}
}

// parseScalarSubquery parses a parenthesised scalar SELECT subquery.
//
// Returns ast.Expression which is the parsed scalar subquery.
func (p *parser) parseScalarSubquery() ast.Expression {
	innerAnalysis, err := p.analyseSubqueryBody()
	if err != nil {
		return &ast.UnknownExpression{}
	}
	return &ast.ScalarSubqueryExpression{InnerQuery: innerAnalysis}
}

// adjacentOperand returns the operand of an expression written next to an operator beside
// it: its first operand when the operator stands before it, its last when the operator
// follows it, looking through comparisons, arithmetic, AND and XOR, IS NULL, IN and
// BETWEEN, all of which bind tighter than OR.
//
// Takes expression (ast.Expression) which is the expression beside the operator.
// Takes first (bool) which is true when the operator stands before the expression.
//
// Returns ast.Expression which is the operand next to the operator.
func adjacentOperand(expression ast.Expression, first bool) ast.Expression {
	for range maxAdjacentOperandDepth {
		next := innerOperand(expression, first)
		if next == nil {
			return expression
		}
		expression = next
	}
	return expression
}

// innerOperand returns the operand of a compound expression on the side an operator
// beside it touches, or nil for an expression with no such operand.
//
// Takes expression (ast.Expression) which is the expression.
// Takes first (bool) which is true for its first operand, false for its last.
//
// Returns ast.Expression which is the operand, or nil.
func innerOperand(expression ast.Expression, first bool) ast.Expression {
	switch typed := expression.(type) {
	case *ast.ComparisonExpression:
		return pick(first, typed.Left, typed.Right)
	case *ast.BinaryOpExpression:
		return pick(first, typed.Left, typed.Right)
	case *ast.LogicalOpExpression:
		if len(typed.Operands) == 0 {
			return nil
		}
		return pick(first, typed.Operands[0], typed.Operands[len(typed.Operands)-1])
	case *ast.IsNullExpression:
		return pick(first, typed.Inner, nil)
	case *ast.InListExpression:
		return pick(first, typed.Inner, nil)
	case *ast.BetweenExpression:
		return pick(first, typed.Inner, nil)
	default:
		return nil
	}
}

// pick returns one of two operands.
//
// Takes first (bool) which chooses the first operand.
// Takes leading (ast.Expression) which is the first operand.
// Takes trailing (ast.Expression) which is the last operand, or nil.
//
// Returns ast.Expression which is the chosen operand.
func pick(first bool, leading, trailing ast.Expression) ast.Expression {
	if first {
		return leading
	}
	return trailing
}

// isZeroNumber reports whether a numeric literal's text is zero, in decimal, scientific,
// hexadecimal or binary notation.
//
// Takes text (string) which is the literal as written.
//
// Returns bool which is true for zero, and for text that is no number.
func isZeroNumber(text string) bool {
	lowered := strings.ToLower(text)
	if strings.HasPrefix(lowered, "0x") || strings.HasPrefix(lowered, "0b") {
		return strings.Trim(lowered[2:], "0") == ""
	}
	value, err := strconv.ParseFloat(lowered, 64)
	return err != nil || value == 0
}

// numberLiteralType types MySQL numeric literals from their scanned shape and value.
//
// Hexadecimal and binary literals are binary strings; exponent literals are DOUBLE;
// fractional literals are DECIMAL. Integer types widen to fit the value.
//
// Takes text (string) which is the literal as written.
//
// Returns string which is the literal's type name.
func numberLiteralType(text string) string {
	if _, prefixed, _ := lexicalRules.Numbers.TryReadPrefixedNumber(text, 0); prefixed {
		return "varbinary"
	}
	switch lexicalRules.Numbers.ClassifyNumber(text) {
	case dialectkit.NumberApproximate:
		return "double"
	case dialectkit.NumberExact:
		return "decimal"
	default:
		value, err := strconv.ParseInt(text, 10, 64)
		switch {
		case err != nil:
			return "decimal"
		case value > math.MaxInt32:
			return "bigint"
		default:
			return "int"
		}
	}
}

// isMultiWordTypeKeyword reports whether upper is a type continuation word.
//
// Takes upper (string) which is the upper-cased candidate keyword.
//
// Returns bool which is true when upper extends a compound type name.
func isMultiWordTypeKeyword(upper string) bool {
	switch upper {
	case "PRECISION", keywordUNSIGNED, "VARYING", "CHARACTER", "DOUBLE":
		return true
	}
	return false
}

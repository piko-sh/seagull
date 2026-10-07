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
	"errors"
	"math"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

var (
	// reservedWords lists DuckDB keywords that require quoting as column or table names.
	// Function-call syntax still allows reserved words as function names.
	reservedWords = map[string]struct{}{
		keywordALL: {}, "ANALYSE": {}, "ANALYZE": {}, keywordAND: {}, "ANY": {}, keywordAS: {}, keywordASC: {},
		"ASYMMETRIC": {}, "BOTH": {}, keywordCHECK: {}, "COLLATE": {}, keywordCOLUMN: {}, keywordCONSTRAINT: {},
		keywordCREATE: {}, keywordDEFAULT: {}, "DEFERRABLE": {}, keywordDESC: {}, "DESCRIBE": {}, "DISTINCT": {},
		"DO": {}, "ELSE": {}, "END": {}, keywordEXCEPT: {}, keywordFETCH: {}, keywordFOR: {}, "FOREIGN": {},
		keywordFROM: {}, keywordGROUP: {}, keywordHAVING: {}, "IN": {}, "INITIALLY": {}, keywordINTERSECT: {},
		"INTO": {}, keywordLAMBDA: {}, "LATERAL": {}, "LEADING": {}, keywordLIMIT: {}, keywordOFFSET: {}, keywordON: {}, "ONLY": {},
		"OR": {}, keywordORDER: {}, keywordPIVOT: {}, keywordPIVOTLONGER: {}, keywordPIVOTWIDER: {}, "PLACING": {},
		"PRIMARY": {}, keywordQUALIFY: {}, "REFERENCES": {}, keywordRETURNING: {}, keywordSELECT: {}, "SHOW": {},
		"SOME": {}, "SUMMARIZE": {}, "SYMMETRIC": {}, keywordTABLE: {}, "THEN": {}, "TO": {}, "TRAILING": {},
		keywordUNION: {}, keywordUNIQUE: {}, keywordUNPIVOT: {}, keywordUSING: {}, "VARIADIC": {}, "WHEN": {},
		keywordWHERE: {}, keywordWINDOW: {}, keywordWITH: {},
	}

	// implicitFunctionIdentifiers lists identifiers that act as zero-argument function calls
	// without parentheses, such as CURRENT_TIMESTAMP.
	implicitFunctionIdentifiers = map[string]struct{}{
		"CURRENT_TIMESTAMP": {}, "CURRENT_DATE": {}, "CURRENT_TIME": {},
		"CURRENT_USER": {}, "SESSION_USER": {}, "LOCALTIME": {},
		"LOCALTIMESTAMP": {}, "CURRENT_ROLE": {}, "CURRENT_CATALOG": {},
		"CURRENT_SCHEMA": {},
	}

	// intervalFieldKeywords lists the unit keywords accepted after an INTERVAL value, in
	// their singular and plural spellings, and the TO of a unit range such as YEAR TO MONTH.
	intervalFieldKeywords = map[string]struct{}{
		"YEAR": {}, "YEARS": {}, "MONTH": {}, "MONTHS": {}, "WEEK": {}, "WEEKS": {},
		"DAY": {}, "DAYS": {}, "HOUR": {}, "HOURS": {}, "MINUTE": {}, "MINUTES": {},
		"SECOND": {}, "SECONDS": {}, "MILLISECOND": {}, "MILLISECONDS": {},
		"MICROSECOND": {}, "MICROSECONDS": {}, "QUARTER": {}, "QUARTERS": {},
		"DECADE": {}, "DECADES": {}, "CENTURY": {}, "CENTURIES": {},
		"MILLENNIUM": {}, "MILLENNIA": {}, "TO": {},
	}
)

// parseExpression parses a SQL expression at the lowest precedence.
//
// Nesting beyond maxParseDepth records a dialectkit.ErrParseDepth error and yields an
// unknown expression rather than descending further, so deeply nested input cannot
// overflow the goroutine stack and is still reported.
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

// parseOrExpression parses one or more AND expressions joined by OR.
//
// Returns ast.Expression which is the resulting logical expression.
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

// parseAndExpression parses one or more NOT expressions joined by AND.
//
// Returns ast.Expression which is the resulting logical expression.
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

// parseNotExpression parses an optional NOT prefix followed by a comparison.
//
// Returns ast.Expression which is the negated or plain comparison.
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

// parseComparisonExpression parses a comparison or postfix predicate.
//
// Returns ast.Expression which is the resulting comparison tree.
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

// parsePostfixComparisonSuffix dispatches to IN, BETWEEN, LIKE, ILIKE, GLOB or SIMILAR,
// or reads a postfix ISNULL or NOTNULL test.
//
// Takes left (ast.Expression) which is the expression to the left of the postfix
// predicate.
//
// Returns ast.Expression which is the predicate or nil when no postfix keyword follows.
func (p *parser) parsePostfixComparisonSuffix(left ast.Expression) ast.Expression {
	if p.IsKeyword("IN") {
		return p.parseInListSuffix(left)
	}
	if p.IsKeyword("BETWEEN") {
		return p.parseBetweenSuffix(left)
	}
	if p.IsKeyword("LIKE", "ILIKE", "GLOB") {
		return p.parseLikeSuffix(left)
	}
	if p.IsKeyword("ISNULL", "NOTNULL") {
		negated := p.Advance().Keyword(tokenIdentifier) == "NOTNULL"
		return &ast.IsNullExpression{Inner: left, Negated: negated}
	}
	if p.IsKeyword("SIMILAR") {
		return p.parseSimilarToSuffix(left)
	}
	return nil
}

// maybeNegate wraps an expression in NOT when negated is true.
//
// Takes negated (bool) which indicates whether a NOT prefix was seen.
// Takes expression (ast.Expression) which is the inner expression.
//
// Returns ast.Expression which is the negated or original expression.
func (*parser) maybeNegate(negated bool, expression ast.Expression) ast.Expression {
	if negated {
		return &ast.UnaryOpExpression{Operator: keywordNOT, Operand: expression}
	}
	return expression
}

// parseLikeSuffix parses a LIKE, ILIKE or GLOB pattern match with optional ESCAPE.
//
// Takes left (ast.Expression) which is the value being matched.
//
// Returns ast.Expression which is the resulting comparison.
func (p *parser) parseLikeSuffix(left ast.Expression) ast.Expression {
	keyword := p.Advance().Keyword(tokenIdentifier)
	right := p.parsePatternOperand()
	if p.MatchKeyword("ESCAPE") {
		p.parseBitwiseExpression()
	}
	return &ast.ComparisonExpression{Operator: keyword, Left: left, Right: right, Quantifier: ""}
}

// parsePatternOperand parses the pattern a LIKE matches against: an expression, or ANY,
// ALL or SOME of a parenthesised list or subquery, which is no function call but whose
// parameters are still registered.
//
// Returns ast.Expression which is the pattern, unknown for a quantified list.
func (p *parser) parsePatternOperand() ast.Expression {
	if !p.IsKeyword("ANY", keywordALL, "SOME") || p.Peek().Kind != tokenLeftParen {
		return p.parseBitwiseExpression()
	}
	p.Advance()
	if p.isSubqueryStart() {
		p.parseScalarSubquery()
	} else {
		p.parseParenthesisedExpressionList()
	}
	return &ast.UnknownExpression{}
}

// parseSimilarToSuffix parses a SIMILAR TO pattern match with optional ESCAPE.
//
// Takes left (ast.Expression) which is the value being matched.
//
// Returns ast.Expression which is the resulting comparison.
func (p *parser) parseSimilarToSuffix(left ast.Expression) ast.Expression {
	p.Advance()
	p.MatchKeyword("TO")
	right := p.parseBitwiseExpression()
	if p.MatchKeyword("ESCAPE") {
		p.parseBitwiseExpression()
	}
	return &ast.ComparisonExpression{Operator: "SIMILAR TO", Left: left, Right: right, Quantifier: ""}
}

// parseComparisonOperator parses a binary comparison or ANY/ALL/SOME suffix.
//
// Takes left (ast.Expression) which is the left-hand side.
//
// Returns ast.Expression which is the resulting comparison.
func (p *parser) parseComparisonOperator(left ast.Expression) ast.Expression {
	operator := p.Advance().Value
	if p.MatchKeyword("ANY") || p.MatchKeyword(keywordALL) || p.MatchKeyword("SOME") {
		if p.Current().Kind == tokenLeftParen {
			p.skipParenthesisedOrRecord()
		}
		return &ast.ComparisonExpression{Operator: operator, Left: left, Right: &ast.UnknownExpression{}, Quantifier: ""}
	}
	right := p.parseBitwiseExpression()
	return &ast.ComparisonExpression{Operator: operator, Left: left, Right: right, Quantifier: ""}
}

// parseBitwiseExpression parses bitwise AND, OR, XOR and shift operators.
//
// Returns ast.Expression which is the parsed bitwise tree.
func (p *parser) parseBitwiseExpression() ast.Expression {
	left := p.parseJSONExpression()

	for p.Current().Kind == tokenOperator &&
		(p.Current().Value == "&" || p.Current().Value == "|" || p.Current().Value == "#" ||
			p.Current().Value == "<<" || p.Current().Value == ">>") {
		operator := p.Advance().Value
		right := p.parseJSONExpression()
		left = &ast.BinaryOpExpression{Operator: operator, Left: left, Right: right, MayBeNull: false}
	}

	return left
}

// parseJSONExpression parses JSON arrow operators (-> and ->>).
//
// Returns ast.Expression which is the parsed JSON access tree.
func (p *parser) parseJSONExpression() ast.Expression {
	left := p.parseAddExpression()

	for p.Current().Kind == tokenArrow || p.Current().Kind == tokenDoubleArrow {
		operator := p.Advance().Value
		right := p.parseAddExpression()
		left = &ast.BinaryOpExpression{Operator: operator, Left: left, Right: right, MayBeNull: false}
	}

	return left
}

// parseAddExpression parses additive operators + and -.
//
// Returns ast.Expression which is the parsed additive tree.
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

// parseMulExpression parses multiplicative operators *, / and %.
//
// Returns ast.Expression which is the parsed multiplicative tree.
func (p *parser) parseMulExpression() ast.Expression {
	left := p.parseConcatExpression()

	for p.isMulOperator() {
		operator := p.Advance().Value
		right := p.parseConcatExpression()
		left = &ast.BinaryOpExpression{Operator: operator, Left: left, Right: right, MayBeNull: false}
	}

	return left
}

// isMulOperator reports whether the current token is a multiplicative operator.
//
// Returns bool which is true when the current token is *, /, //, %, ** or ^.
func (p *parser) isMulOperator() bool {
	if p.Current().Kind == tokenStar {
		return true
	}
	if p.Current().Kind != tokenOperator {
		return false
	}
	switch p.Current().Value {
	case "/", "//", "%", "**", "^", "<->", "<=>":
		return true
	default:
		return false
	}
}

// parseConcatExpression parses the string concatenation operator (||).
//
// Returns ast.Expression which is the parsed concatenation tree.
func (p *parser) parseConcatExpression() ast.Expression {
	left := p.parseUnaryExpression()

	for p.Current().Kind == tokenOperator && p.Current().Value == "||" {
		p.Advance()
		right := p.parseUnaryExpression()
		left = &ast.BinaryOpExpression{Operator: "||", Left: left, Right: right, MayBeNull: false}
	}

	return left
}

// parseUnaryExpression parses unary prefix operators -, +, ~ and NOT.
//
// Returns ast.Expression which is the parsed unary tree.
func (p *parser) parseUnaryExpression() ast.Expression {
	if p.Current().Kind == tokenOperator &&
		(p.Current().Value == "-" || p.Current().Value == "+" || p.Current().Value == "~" || p.Current().Value == "@") {
		operator := p.Advance().Value
		inner := p.parseCastExpression()
		if _, ok := inner.(*ast.LiteralExpression); ok {
			return inner
		}
		return &ast.UnaryOpExpression{Operator: operator, Operand: inner}
	}
	if p.MatchKeyword(keywordNOT) {
		inner := p.parseCastExpression()
		return &ast.UnaryOpExpression{Operator: keywordNOT, Operand: inner}
	}
	return p.parseCastExpression()
}

// annotateInlineCast records the type of an inline `::type` cast on the parameter
// registered last: as a cast for a parameter of no known context, and as the declared
// type of a parameter alone the statement assigns to a column.
//
// Takes cast (ast.Expression) which is the expression the cast applies to.
// Takes typeName (string) which is the cast's type name.
func (p *parser) annotateInlineCast(cast ast.Expression, typeName string) {
	if len(p.parameterReferences) == 0 {
		return
	}
	last := &p.parameterReferences[len(p.parameterReferences)-1]
	switch {
	case last.Context == sqlmodel.ParameterContextUnknown:
		last.Context = sqlmodel.ParameterContextCast
		last.CastType = new(p.types.normalise(typeName))
	case castsAssignedParameter(cast, last):
		last.CastType = new(p.types.normalise(typeName))
	}
}

// parseCastExpression parses chained PostgreSQL-style cast operators (::).
//
// Returns ast.Expression which is the parsed cast expression tree.
func (p *parser) parseCastExpression() ast.Expression {
	left := p.parseSubscriptExpression()

	for p.Current().Kind == tokenCast {
		p.Advance()
		typeName := p.parseCastTypeName()

		p.annotateInlineCast(left, typeName)

		left = &ast.CastExpression{
			TypeName: strings.ToLower(typeName),
			Inner:    left,
		}
	}

	return left
}

// parseSubscriptExpression parses the postfix forms that follow a primary expression:
// list subscripts and slices, and the struct field access that may follow them, such as
// route[1].lat.
//
// Returns ast.Expression which is the parsed postfix expression.
func (p *parser) parseSubscriptExpression() ast.Expression {
	left := p.parsePrimaryExpression()

	for {
		switch {
		case p.Current().Kind == tokenLeftBracket:
			left = p.parseSubscriptSuffix(left)
		case p.Current().Kind == tokenDot && p.Peek().Kind == tokenIdentifier:
			p.Advance()
			left = &ast.StructFieldAccessExpression{Struct: left, FieldName: p.Advance().Value}
		default:
			return left
		}
	}
}

// parseSubscriptSuffix parses one [index] subscript or [start:end] slice of left. It
// preserves string literal keys for struct-field resolution and for naming parameters
// compared with map entries.
//
// Takes left (ast.Expression) which is the subscripted expression.
//
// Returns ast.Expression which is the subscript, or an unknown expression for a slice.
func (p *parser) parseSubscriptSuffix(left ast.Expression) ast.Expression {
	p.Advance()
	key, isKey := p.literalSubscriptKey()
	isSlice := false
	if p.Current().Kind != tokenOperator || p.Current().Value != operatorColon {
		p.parseSubscriptBound()
	}
	if p.Current().Kind == tokenOperator && p.Current().Value == operatorColon {
		p.Advance()
		isSlice = true
		if p.Current().Kind != tokenRightBracket {
			p.parseSubscriptBound()
		}
	}
	if p.Current().Kind != tokenRightBracket {
		p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected ']' to close the subscript"))
		return &ast.UnknownExpression{}
	}
	p.Advance()
	if isSlice {
		return left
	}
	subscript := &ast.ArraySubscriptExpression{Array: left, Index: &ast.LiteralExpression{TypeName: "integer"}, Key: ""}
	if isKey {
		subscript.Index = &ast.LiteralExpression{TypeName: "text"}
		subscript.Key = key
	}
	return subscript
}

// literalSubscriptKey reports whether the subscript at the cursor is a lone string
// literal, such as the 'pontoon' in berth['pontoon'].
//
// Returns string which is the literal's value.
// Returns bool which is true when the subscript is a lone string literal.
func (p *parser) literalSubscriptKey() (string, bool) {
	if p.Current().Kind != tokenString || p.Peek().Kind != tokenRightBracket {
		return "", false
	}
	return p.Current().Value, true
}

// parseSubscriptBound parses one bound of a subscript or slice, which may be a parameter,
// so the parameter is registered.
func (p *parser) parseSubscriptBound() {
	p.parseExpression()
}

// parsePrimaryExpression parses a primary expression at the top of precedence.
//
// Returns ast.Expression which is the parsed literal, parameter, parenthesised expression
// or identifier.
func (p *parser) parsePrimaryExpression() ast.Expression {
	current := p.Current()

	switch current.Kind {
	case tokenNumber:
		return p.parseNumberLiteral(current)
	case tokenString, tokenDollarString, tokenEscapeString:
		p.Advance()
		return &ast.LiteralExpression{TypeName: "text"}
	case tokenLeftBrace:
		return p.parseStructLiteral()
	case tokenLeftBracket:
		return p.parseListLiteral()
	case tokenBitString:
		p.Advance()
		return &ast.LiteralExpression{TypeName: "bit"}
	case tokenDollarParam, tokenNamedParam, tokenQuestionParam:
		return p.parseParameterExpression()
	case tokenLeftParen:
		return p.parseParenthesisedExpression()
	case tokenIdentifier:
		return p.parseIdentifierExpression()
	case tokenOperator:
		if current.Value == "#" && p.Peek().Kind == tokenNumber {
			p.Advance()
			p.Advance()
			return &ast.UnknownExpression{}
		}
	default:
	}
	p.recordSyntaxError(dialectkit.NewParseError(current, "expected an expression"))
	p.Advance()
	return &ast.UnknownExpression{}
}

// parseNumberLiteral parses a numeric literal token.
//
// Takes candidate (token) which holds the number literal text.
//
// Returns ast.Expression which is the literal, typed by numberLiteralType.
func (p *parser) parseNumberLiteral(candidate token) ast.Expression {
	p.Advance()
	return &ast.LiteralExpression{TypeName: numberLiteralType(candidate.Value)}
}

// parseParameterExpression parses and records a bind parameter reference.
//
// Returns ast.Expression which is the parameter, by the number it is registered under.
func (p *parser) parseParameterExpression() ast.Expression {
	parameterPosition := p.Position
	parameterToken := p.Current()
	p.Advance()

	context := sqlmodel.ParameterContextUnknown
	var columnReference *ast.ColumnReference
	if reference := p.insertProjectionColumnRef(); reference != nil {
		context = sqlmodel.ParameterContextAssignment
		columnReference = reference
	}

	number := p.registerParameterFromToken(parameterToken, context, columnReference, nil)
	p.recordFunctionArgumentMetadata(parameterPosition)
	return &ast.ParameterExpression{Number: number}
}

// insertProjectionColumnRef returns the INSERT target column reference for the projection
// item currently being parsed, or nil when no INSERT ... SELECT projection is active or
// the ordinal is past the declared column list.
//
// Returns *ast.ColumnReference which names the target table and column, or nil.
func (p *parser) insertProjectionColumnRef() *ast.ColumnReference {
	if p.insertProjectionColumns == nil {
		return nil
	}
	return p.columnRefForIndex(p.insertProjectionTable, p.insertProjectionColumns, p.insertProjectionIndex)
}

// parseParenthesisedExpression parses a parenthesised expression or subquery.
//
// Returns ast.Expression which is the inner expression or a scalar subquery placeholder.
func (p *parser) parseParenthesisedExpression() ast.Expression {
	if p.isSubqueryStart() {
		return p.parseScalarSubquery()
	}
	p.Advance()
	inner := p.parseExpression()
	values := []ast.Expression{inner}
	for p.Current().Kind == tokenComma {
		p.Advance()
		values = append(values, p.parseExpression())
	}
	if p.Current().Kind != tokenRightParen {
		p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected ')'"))
		return inner
	}
	p.Advance()
	if len(values) > 1 {
		return rowConstructor(values)
	}
	return inner
}

// parseIdentifierExpression parses an identifier as keyword, function or column.
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

	if _, isImplicit := implicitFunctionIdentifiers[upper]; isImplicit {
		p.Advance()
		return &ast.FunctionCallExpression{
			FunctionName:     strings.ToLower(upper),
			FilterExpression: nil,
			Schema:           "",
			Arguments:        nil,
			Span:             ast.Span{},
		}
	}

	if literal, isTypedLiteral := p.parseTypedLiteral(upper); isTypedLiteral {
		return literal
	}

	return p.parseColumnOrFunctionReference()
}

// parseTypedLiteral parses a typed literal, a type name followed by a string, such as
// DATE '2024-01-01' or TIMESTAMP WITH TIME ZONE '2024-01-01 10:00:00+00', which DuckDB
// reads as the string cast to the type.
//
// DuckDB reads any unquoted name that is not a reserved word as a type name when a string
// follows it.
//
// Takes upper (string) which is the current identifier upper-cased, empty when quoted.
//
// Returns ast.Expression which is a literal of the named type.
// Returns bool which is false, having consumed nothing, when no typed literal starts
// here.
func (p *parser) parseTypedLiteral(upper string) (ast.Expression, bool) {
	if _, reserved := reservedWords[upper]; reserved || upper == "" {
		return nil, false
	}
	if p.Peek().Kind == tokenString {
		p.Advance()
		p.Advance()
		return &ast.LiteralExpression{TypeName: strings.ToLower(upper)}, true
	}
	if (upper == keywordTIMESTAMP || upper == keywordTIME) && p.followedByTimeZone() {
		p.Advance()
		for range timeZoneSuffixTokens {
			p.Advance()
		}
		p.Advance()
		return &ast.LiteralExpression{TypeName: strings.ToLower(upper) + " with time zone"}, true
	}
	return nil, false
}

// followedByTimeZone reports whether WITH TIME ZONE and a string follow the current
// token, the rest of a TIMESTAMP WITH TIME ZONE or TIME WITH TIME ZONE literal.
//
// Returns bool which is true when they do.
func (p *parser) followedByTimeZone() bool {
	return p.PeekAt(1).Keyword(tokenIdentifier) == keywordWITH &&
		p.PeekAt(2).Keyword(tokenIdentifier) == keywordTIME &&
		p.PeekAt(timeZoneSuffixTokens).Keyword(tokenIdentifier) == keywordZONE &&
		p.PeekAt(timeZoneSuffixTokens+1).Kind == tokenString
}

// identifierExpressionHandler resolves a keyword identifier to a parser hook.
//
// Takes upper (string) which is the uppercase identifier text.
//
// Returns func() ast.Expression which produces the parsed expression, or nil when the
// identifier is not a recognised keyword.
func (p *parser) identifierExpressionHandler(upper string) func() ast.Expression {
	switch upper {
	case keywordNULL:
		return p.parseNullLiteral
	case "TRUE", "FALSE":
		return p.parseBooleanLiteral
	case "CAST", "TRY_CAST":
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
	case "MAP":
		if p.Peek().Kind == tokenLeftBrace {
			return p.parseMapLiteral
		}
		return nil
	case keywordNOT:
		return func() ast.Expression { return &ast.UnknownExpression{} }
	case "INTERVAL":
		return p.parseIntervalLiteral
	case keywordROW:
		return p.parseRowConstructorExpression
	default:
		return nil
	}
}

// parseNullLiteral consumes the NULL keyword and returns a nil expression.
//
// Returns ast.Expression which is always nil to signal a SQL NULL.
func (p *parser) parseNullLiteral() ast.Expression {
	p.Advance()
	return nil
}

// parseBooleanLiteral consumes a TRUE or FALSE keyword.
//
// Returns ast.Expression which is a boolean literal expression.
func (p *parser) parseBooleanLiteral() ast.Expression {
	p.Advance()
	return &ast.LiteralExpression{TypeName: "boolean"}
}

// parseIntervalLiteral parses an INTERVAL literal: a string such as '2 days', or a
// number, parameter or parenthesised expression followed by its unit, such as INTERVAL 1
// DAY or INTERVAL ($1) HOUR.
//
// Returns ast.Expression which is the interval literal expression.
func (p *parser) parseIntervalLiteral() ast.Expression {
	p.Advance()
	switch {
	case p.Current().Kind == tokenString:
		p.Advance()
	case p.Current().Kind == tokenNumber:
		p.Advance()
	case p.Current().Kind == tokenLeftParen || isParameterToken(p.Current().Kind):
		p.parsePrimaryExpression()
	}
	for p.Current().Kind == tokenIdentifier {
		if _, ok := intervalFieldKeywords[p.Current().Keyword(tokenIdentifier)]; !ok {
			break
		}
		p.Advance()
	}
	return &ast.LiteralExpression{TypeName: "interval"}
}

// parseRowConstructorExpression parses a ROW(...) constructor.
//
// Returns ast.Expression which is the unnamed struct it makes; see rowConstructor.
func (p *parser) parseRowConstructorExpression() ast.Expression {
	p.Advance()
	return rowConstructor(p.parseRowConstructorValues())
}

// parseColumnOrFunctionReference parses an identifier as column or function.
//
// Returns ast.Expression which is a column reference, function call or qualified
// identifier expression.
func (p *parser) parseColumnOrFunctionReference() ast.Expression {
	if _, reserved := reservedWords[p.Current().Keyword(tokenIdentifier)]; reserved && p.Peek().Kind != tokenLeftParen {
		p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected an expression"))
		p.Advance()
		return &ast.UnknownExpression{}
	}
	name := p.Advance().Value

	if p.Current().Kind == tokenDot {
		return p.parseDotQualifiedIdentifier(name)
	}

	if p.Current().Kind == tokenLeftParen {
		if call := p.parseSpecialFunctionCall(name); call != nil {
			return call
		}
		return p.parseFunctionCall(name, "")
	}

	return &ast.ColumnRefExpression{
		TableAlias: "",
		ColumnName: name,
		Span:       ast.Span{},
	}
}

// parseRowConstructorValues consumes the parenthesised list of ROW constructor values.
//
// Returns []ast.Expression which holds the values in order.
func (p *parser) parseRowConstructorValues() []ast.Expression {
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

// parseDotQualifiedIdentifier parses identifiers following a leading dot.
//
// Takes name (string) which is the identifier preceding the dot.
//
// Returns ast.Expression which is a column reference, function call or schema-qualified
// reference.
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
		return p.parseSchemaQualifiedRef(name, second)
	}
	if p.Current().Kind == tokenLeftParen {
		return p.parseFunctionCall(second, name)
	}
	return &ast.ColumnRefExpression{TableAlias: name, ColumnName: second, Span: ast.Span{}}
}

// parseSchemaQualifiedRef reads a three-part name or schema-qualified function call.
//
// After the query's relations are known, field resolution decides whether the name refers
// to a schema-qualified column or nested fields.
//
// Takes schema (string) which is the first name, a schema or relation.
// Takes table (string) which is the second name, a relation, column or function schema.
//
// Returns ast.Expression which is the field access or function call expression.
func (p *parser) parseSchemaQualifiedRef(schema string, table string) ast.Expression {
	p.Advance()
	if p.Current().Kind != tokenIdentifier {
		return &ast.ColumnRefExpression{TableAlias: schema, ColumnName: table, Span: ast.Span{}}
	}
	third := p.Advance().Value
	if p.Current().Kind == tokenLeftParen {
		return p.parseFunctionCall(third, schema+"."+table)
	}
	return &ast.StructFieldAccessExpression{
		Struct:    &ast.ColumnRefExpression{TableAlias: schema, ColumnName: table, Span: ast.Span{}},
		FieldName: third,
	}
}

// parseIsSuffix parses IS NULL, IS DISTINCT FROM and related predicates.
//
// Takes left (ast.Expression) which is the left-hand side of IS.
//
// Returns ast.Expression which is the resulting null or distinct comparison expression.
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

	if p.MatchKeyword("TRUE") || p.MatchKeyword("FALSE") || p.MatchKeyword("UNKNOWN") {
		return &ast.IsNullExpression{Inner: left, Negated: negated}
	}

	return p.parseIsOperandComparison(left, negated)
}

// parseIsOperandComparison accepts "col IS <value>" and "col IS NOT <value>" as null-safe
// comparisons in the parser.
//
// It parses the right-hand operand and attaches the left-hand column reference to any
// placeholder seen on the right, so a bare parameter takes the column's type, like "col =
// <param>" does. This mirrors the IN-list and BETWEEN suffixes; reached only when the
// token after IS [NOT] is not one of the recognised NULL / DISTINCT / TRUE / FALSE /
// UNKNOWN forms.
//
// Takes left (ast.Expression) which is the left-hand side of IS.
// Takes negated (bool) which is true when a NOT keyword followed IS.
//
// Returns ast.Expression which is the resulting null-safe comparison expression.
func (p *parser) parseIsOperandComparison(left ast.Expression, negated bool) ast.Expression {
	parameterCountBefore := p.parameterCount
	right := p.parseBitwiseExpression()

	var columnReference *ast.ColumnReference
	if columnExpression, ok := left.(*ast.ColumnRefExpression); ok {
		columnReference = &ast.ColumnReference{
			TableAlias: columnExpression.TableAlias,
			ColumnName: columnExpression.ColumnName,
		}
	}
	if columnReference != nil {
		for index := range p.parameterReferences {
			if p.parameterReferences[index].Number > parameterCountBefore && p.parameterReferences[index].ColumnReference == nil {
				p.parameterReferences[index].Context = sqlmodel.ParameterContextComparison
				p.parameterReferences[index].ColumnReference = columnReference
			}
		}
	}

	operator := "IS"
	if negated {
		operator = "IS NOT"
	}
	return &ast.ComparisonExpression{Operator: operator, Left: left, Right: right, Quantifier: ""}
}

// parseInListSuffix parses an IN (...) list or subquery predicate.
//
// Takes left (ast.Expression) which is the value being tested for membership.
//
// Returns ast.Expression which is the resulting IN predicate.
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

	parameterCountBefore := p.parameterCount
	values := p.parseParenthesisedExpressionList()

	var columnReference *ast.ColumnReference
	if columnExpression, ok := left.(*ast.ColumnRefExpression); ok {
		columnReference = &ast.ColumnReference{
			TableAlias: columnExpression.TableAlias,
			ColumnName: columnExpression.ColumnName,
		}
	}
	for index := range p.parameterReferences {
		if p.parameterReferences[index].Number > parameterCountBefore {
			p.parameterReferences[index].Context = sqlmodel.ParameterContextInList
			if columnReference != nil {
				p.parameterReferences[index].ColumnReference = columnReference
			}
		}
	}

	return &ast.InListExpression{Inner: left, Values: values}
}

// parseBetweenSuffix parses a BETWEEN low AND high range predicate.
//
// Takes left (ast.Expression) which is the value being tested for range membership.
//
// Returns ast.Expression which is the resulting BETWEEN predicate.
func (p *parser) parseBetweenSuffix(left ast.Expression) ast.Expression {
	p.Advance()
	p.MatchKeyword("SYMMETRIC")

	parameterCountBefore := p.parameterCount
	low := p.parseAddExpression()
	p.MatchKeyword(keywordAND)
	high := p.parseAddExpression()

	var columnReference *ast.ColumnReference
	if columnExpression, ok := left.(*ast.ColumnRefExpression); ok {
		columnReference = &ast.ColumnReference{
			TableAlias: columnExpression.TableAlias,
			ColumnName: columnExpression.ColumnName,
		}
	}
	for index := range p.parameterReferences {
		if p.parameterReferences[index].Number > parameterCountBefore {
			p.parameterReferences[index].Context = sqlmodel.ParameterContextBetween
			if columnReference != nil {
				p.parameterReferences[index].ColumnReference = columnReference
			}
		}
	}

	return &ast.BetweenExpression{Inner: left, Low: low, High: high}
}

// parseParenthesisedExpressionList parses a comma-separated expression list.
//
// Returns []ast.Expression which contains the parsed expressions inside the parentheses.
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

// parseStructLiteral parses a {key: value, ...} struct literal, whose keys are string
// literals or names, as the struct constructor of one field per entry, as struct_pack
// builds. DuckDB has no empty struct literal.
//
// Returns ast.Expression which builds the struct.
func (p *parser) parseStructLiteral() ast.Expression {
	opening, start := p.Current(), p.Position
	names, values := p.parseStructEntries()
	if len(values) == 0 {
		p.recordSyntaxError(dialectkit.NewParseError(opening, "expected a struct field in the struct literal"))
		return &ast.UnknownExpression{}
	}
	return &ast.StructExpression{Names: names, Values: values, Span: p.absoluteSpan(start, p.Position)}
}

// parseStructEntries parses the name: value entries of a struct literal, up to and
// including its closing brace.
//
// Returns []string which holds the entries' names.
// Returns []ast.Expression which holds the entries' values, one per name.
func (p *parser) parseStructEntries() ([]string, []ast.Expression) {
	if p.Current().Kind != tokenLeftBrace {
		p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected '{'"))
		return nil, nil
	}
	p.Advance()
	var names []string
	var values []ast.Expression
	for p.Current().Kind != tokenRightBrace {
		name, named := p.parseStructLiteralKey()
		if !named {
			return names, values
		}
		if p.Current().Kind != tokenOperator || p.Current().Value != operatorColon {
			p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected ':' after the key"))
			return names, values
		}
		p.Advance()
		names = append(names, name)
		values = append(values, p.parseExpression())
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	if p.Current().Kind != tokenRightBrace {
		p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected '}' to close the literal"))
		return names, values
	}
	p.Advance()
	return names, values
}

// parseMapLiteral parses a MAP {key: value, ...} literal as the map call it is shorthand
// for; the cursor is on the MAP keyword.
//
// Returns ast.Expression which is the map call over the literal's keys and values.
func (p *parser) parseMapLiteral() ast.Expression {
	p.Advance()
	return &ast.FunctionCallExpression{
		FunctionName:     functionMapLiteral,
		Schema:           "",
		Arguments:        p.parseMapEntries(),
		FilterExpression: nil,
		Span:             ast.Span{},
	}
}

// parseMapEntries parses the key: value entries of a map literal, whose keys are any
// expression, up to and including its closing brace.
//
// Returns []ast.Expression which holds each entry's key followed by its value.
func (p *parser) parseMapEntries() []ast.Expression {
	if p.Current().Kind != tokenLeftBrace {
		p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected '{'"))
		return nil
	}
	p.Advance()
	var entries []ast.Expression
	for p.Current().Kind != tokenRightBrace {
		entries = append(entries, p.parseExpression())
		if p.Current().Kind != tokenOperator || p.Current().Value != operatorColon {
			p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected ':' after the key"))
			return entries
		}
		p.Advance()
		entries = append(entries, p.parseExpression())
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	if p.Current().Kind != tokenRightBrace {
		p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected '}' to close the literal"))
		return entries
	}
	p.Advance()
	return entries
}

// parseStructLiteralKey consumes a struct literal's key: a string literal or a name, bare
// or quoted.
//
// Returns string which is the field's name.
// Returns bool which is false when no key is next, after recording the syntax error.
func (p *parser) parseStructLiteralKey() (string, bool) {
	if p.Current().Kind == tokenString || p.Current().Kind == tokenIdentifier {
		return p.Advance().Value, true
	}
	p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected a struct field name"))
	return "", false
}

// parseListLiteral parses a [value, ...] list literal as the list_value call it is
// shorthand for, or a [expression FOR name IN list IF condition] list comprehension as
// the list_transform (and list_filter) calls it is shorthand for.
//
// Returns ast.Expression which is the list_value or list_transform call.
func (p *parser) parseListLiteral() ast.Expression {
	p.Advance()
	var elements []ast.Expression
	for p.Current().Kind != tokenRightBracket {
		elements = append(elements, p.parseExpression())
		if len(elements) == 1 && p.IsKeyword(keywordFOR) {
			return p.parseListComprehension(elements[0])
		}
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	p.requireClosingBracket()
	return &ast.FunctionCallExpression{
		FunctionName:     "list_value",
		Schema:           "",
		Arguments:        elements,
		FilterExpression: nil,
		Span:             ast.Span{},
	}
}

// parseListComprehension parses the FOR name IN list [IF condition] tail of a list
// comprehension whose element expression has been parsed.
//
// Takes element (ast.Expression) which is the comprehension's element expression.
//
// Returns ast.Expression which is the list_transform call, over a list_filter call when
// the comprehension has a condition.
func (p *parser) parseListComprehension(element ast.Expression) ast.Expression {
	p.Advance()
	if p.Current().Kind != tokenIdentifier {
		p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected the comprehension's variable"))
		return &ast.UnknownExpression{}
	}
	variable := p.Advance().Value
	if _, err := p.ExpectKeyword(keywordIN); err != nil {
		p.recordSyntaxError(err)
		return &ast.UnknownExpression{}
	}
	source := p.parseExpression()
	if p.MatchKeyword("IF") {
		condition := p.parseExpression()
		source = &ast.FunctionCallExpression{
			FunctionName:     "list_filter",
			Schema:           "",
			Arguments:        []ast.Expression{source, &ast.LambdaExpression{Parameters: []string{variable}, Body: condition, Bindings: nil}},
			FilterExpression: nil,
			Span:             ast.Span{},
		}
	}
	p.requireClosingBracket()
	return &ast.FunctionCallExpression{
		FunctionName:     "list_transform",
		Schema:           "",
		Arguments:        []ast.Expression{source, &ast.LambdaExpression{Parameters: []string{variable}, Body: element, Bindings: nil}},
		FilterExpression: nil,
		Span:             ast.Span{},
	}
}

// requireClosingBracket consumes the ']' that closes a list, recording a syntax error
// when it is missing.
func (p *parser) requireClosingBracket() {
	if p.Current().Kind != tokenRightBracket {
		p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected ']' to close the list"))
		return
	}
	p.Advance()
}

// castsAssignedParameter reports whether an inline cast applies to a parameter alone that
// the statement assigns to a column, as in INSERT ... SELECT $1::UUID, and declares no
// type yet, so the cast declares the parameter's type.
//
// Takes cast (ast.Expression) which is the expression the cast applies to.
// Takes reference (*ast.RawParameterReference) which is the parameter registered last.
//
// Returns bool which is true when the cast declares that parameter's type.
func castsAssignedParameter(cast ast.Expression, reference *ast.RawParameterReference) bool {
	parameter, isParameter := cast.(*ast.ParameterExpression)
	return isParameter && parameter != nil && parameter.Number == reference.Number &&
		reference.Context == sqlmodel.ParameterContextAssignment && reference.CastType == nil
}

// numberLiteralType types a numeric literal by its scanned shape, as DuckDB does: a
// literal with an exponent is a DOUBLE, one with a decimal point an exact DECIMAL, and an
// integer the narrowest of INTEGER, BIGINT and HUGEINT that holds it.
//
// Takes text (string) which is the literal as written.
//
// Returns string which is the literal's type name.
func numberLiteralType(text string) string {
	switch lexicalRules.Numbers.ClassifyNumber(text) {
	case dialectkit.NumberApproximate:
		return "double"
	case dialectkit.NumberExact:
		return "decimal"
	default:
		value, err := strconv.ParseInt(text, 10, 64)
		switch {
		case err != nil:
			return "hugeint"
		case value > math.MaxInt32:
			return "bigint"
		default:
			return "integer"
		}
	}
}

// rowConstructor builds the unnamed struct ROW(...) or a parenthesised list of values
// makes, as a call of row the dialect types; see resolveRow.
//
// Takes values ([]ast.Expression) which are the row's values.
//
// Returns ast.Expression which is the call.
func rowConstructor(values []ast.Expression) ast.Expression {
	return &ast.FunctionCallExpression{FunctionName: functionRow, Schema: "", Arguments: values, FilterExpression: nil, Span: ast.Span{}}
}

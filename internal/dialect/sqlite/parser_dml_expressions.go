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
	"fmt"
	"iter"
	"math"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

var (
	// expressionTerminators lists keywords that terminate a free-form SQL expression.
	//
	// JOIN-introducing keywords are included so that a JOIN's ON expression stops at the
	// next JOIN rather than swallowing the subsequent JOIN clause, which would prevent the
	// next joined table from being added to the scope chain and leave its projected columns
	// typed as `any`.
	expressionTerminators = map[string]bool{
		keywordGROUP: true, keywordHAVING: true, keywordORDER: true, keywordLIMIT: true,
		keywordUNION: true, keywordINTERSECT: true, keywordEXCEPT: true,
		keywordRETURNING: true, keywordSET: true, keywordON: true,
		keywordFROM: true, keywordWHERE: true,
		keywordJOIN: true, keywordINNER: true, keywordLEFT: true, keywordRIGHT: true,
		keywordFULL: true, keywordCROSS: true, keywordNATURAL: true,
	}

	// joinKeywordTerminators is the subset of expressionTerminators whose tokens are also
	// valid SQL function names.
	//
	// LEFT(), RIGHT() and similar tokens are functions when immediately followed by an
	// opening parenthesis rather than JOIN starters, so the terminator check must skip them
	// to avoid prematurely ending the surrounding expression and quietly dropping any
	// parameters that follow.
	joinKeywordTerminators = map[string]bool{
		keywordINNER: true, keywordLEFT: true, keywordRIGHT: true, keywordFULL: true, keywordCROSS: true, keywordNATURAL: true,
	}

	// currentTimeKeywords are the keywords SQLite reads as the current date or time, as
	// text, rather than as a column.
	currentTimeKeywords = map[string]bool{"CURRENT_TIMESTAMP": true, "CURRENT_DATE": true, "CURRENT_TIME": true}

	// nonExpressionKeywords lists the keywords SQLite never reads as a column or the start
	// of an expression when written unquoted, such as FROM or WHERE, so an expression that
	// meets one in operand position is incomplete rather than naming a column.
	nonExpressionKeywords = map[string]bool{
		"ADD": true, "ALL": true, "ALTER": true, keywordAND: true, keywordAS: true, "AUTOINCREMENT": true,
		keywordBETWEEN: true, keywordCHECK: true, keywordCOLLATE: true, "COMMIT": true, keywordCONSTRAINT: true,
		keywordCREATE: true, "DEFAULT": true, "DEFERRABLE": true, "DELETE": true, "DISTINCT": true, keywordDROP: true,
		"ELSE": true, "ESCAPE": true, keywordEXCEPT: true, "FOREIGN": true, keywordFROM: true, keywordGROUP: true,
		keywordHAVING: true, "IN": true, "INDEX": true, "INSERT": true, keywordINTERSECT: true, "INTO": true, "IS": true,
		"ISNULL": true, keywordJOIN: true, keywordLIMIT: true, "NOTHING": true, "NOTNULL": true, keywordON: true,
		keywordOR: true, keywordORDER: true, "PRIMARY": true, "REFERENCES": true, keywordRETURNING: true,
		keywordSELECT: true, keywordSET: true, "TABLE": true, "THEN": true, "TO": true, "TRANSACTION": true,
		keywordUNION: true, "UNIQUE": true, "UPDATE": true, "USING": true, keywordVALUES: true, "WHEN": true,
		keywordWHERE: true,
	}

	// whereExpressionTerminators is the terminator set used for WHERE / HAVING expressions.
	//
	// It deliberately omits the JOIN-introducing keywords (JOIN, INNER, LEFT, RIGHT, FULL,
	// CROSS, NATURAL) because those are all legal unquoted column names in SQLite, and a
	// WHERE/HAVING predicate never starts a JOIN at top level. A column named `left`,
	// `right` or `inner` must therefore remain part of the predicate. The ON-clause scan
	// keeps the JOIN keywords via expressionTerminators.
	whereExpressionTerminators = map[string]bool{
		keywordGROUP: true, keywordHAVING: true, keywordORDER: true, keywordLIMIT: true,
		keywordUNION: true, keywordINTERSECT: true, keywordEXCEPT: true,
		keywordRETURNING: true, keywordSET: true, keywordON: true,
		keywordFROM: true, keywordWHERE: true,
	}
)

// parseWhereExpression skips a WHERE / HAVING predicate, stopping at the first
// clause-level terminator that does not double as a column name.
func (p *parser) parseWhereExpression() {
	p.parseExpressionUntilTerminator(whereExpressionTerminators)
}

// parseJoinConditionExpression skips a JOIN ON predicate. A join condition is genuinely
// followed by the next JOIN, so the JOIN-introducing keywords remain terminators here
// (via expressionTerminators); the handleExpressionTerminator function-call exemption
// still distinguishes LEFT()/RIGHT() calls from JOIN starters.
func (p *parser) parseJoinConditionExpression() {
	p.parseExpressionUntilTerminator(expressionTerminators)
}

// parseExpressionUntilTerminator advances over tokens until reaching a terminator from
// the supplied set at depth zero.
//
// Takes terminators (map[string]bool) which is the set of clause-level keywords that end
// the scan.
func (p *parser) parseExpressionUntilTerminator(terminators map[string]bool) {
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

		if depth == 0 && isExpressionTerminator(current, terminators) && !p.isDistinctFrom(current) {
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
			if innerAnalysis, ok := p.analyseSubqueryBody(); ok {
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

// isDistinctFrom reports whether a FROM keyword is part of IS [NOT] DISTINCT FROM rather
// than the start of a FROM clause.
//
// Takes current (token) which is the candidate terminator token.
//
// Returns bool which is true when the keyword is FROM and DISTINCT precedes it.
func (p *parser) isDistinctFrom(current token) bool {
	return current.IsKeyword(tokenIdentifier, keywordFROM) && p.PeekAt(-1).IsKeyword(tokenIdentifier, "DISTINCT")
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
	if joinKeywordTerminators[current.Keyword(tokenIdentifier)] && p.Peek().Kind == tokenLeftParen {
		p.Advance()
		return false
	}
	return true
}

// handleParameterInExpression registers a parameter token seen while scanning an
// expression, attaching context information when known.
//
// When the flat scan classifies the placeholder as a function argument it also records
// the enclosing function name and the 0-based argument ordinal on the freshly registered
// parameter, so the analyser can look the function up and back-propagate the declared
// argument type. The AST function-call path records the same metadata via
// markFunctionArgumentParameters; this branch covers the flat WHERE/ON scan path (for
// example LENGTH(?) > 0) which never descends into parseFunctionArguments.
func (p *parser) handleParameterInExpression() {
	parameterPosition := p.Position
	parameterToken := p.Current()

	context, columnReference, castType := p.resolveScannedParameterContext(parameterPosition)
	context, columnReference = p.fullTextQueryContext(context, columnReference)

	p.Advance()
	p.registerParameterFromToken(parameterToken, context, columnReference, castType)

	if context == sqlmodel.ParameterContextFunctionArgument {
		if name, ordinal, ok := p.enclosingFunctionArgument(parameterPosition); ok {
			lastIndex := len(p.parameterReferences) - 1
			if lastIndex >= 0 && p.parameterReferences[lastIndex].EnclosingFunctionName == "" {
				p.parameterReferences[lastIndex].EnclosingFunctionName = name
				p.parameterReferences[lastIndex].ArgumentOrdinal = ordinal
			}
		}
	}
}

// resolveScannedParameterContext infers where a placeholder met by a token scan sits,
// from the tokens around it: a comparison with a column, a pattern match, a BETWEEN
// bound, a comparison with a function result, or the enclosing IN list, CAST or call.
//
// Takes parameterPosition (int) which is the placeholder's token index.
//
// Returns sqlmodel.ParameterContext which describes the inferred context.
// Returns *ast.ColumnReference which is the column the placeholder takes its type from,
// or nil.
// Returns *sqlmodel.SQLType which is the type the placeholder takes, from a CAST or a
// function result, or nil.
func (p *parser) resolveScannedParameterContext(parameterPosition int) (sqlmodel.ParameterContext, *ast.ColumnReference, *sqlmodel.SQLType) {
	if context, columnReference := p.resolveComparisonContext(parameterPosition); context != sqlmodel.ParameterContextUnknown {
		return context, columnReference, nil
	}
	if context, columnReference := p.resolveLikeContext(parameterPosition); context != sqlmodel.ParameterContextUnknown {
		return context, columnReference, nil
	}
	if columnReference := p.resolveBetweenColumn(parameterPosition); columnReference != nil {
		return sqlmodel.ParameterContextBetween, columnReference, nil
	}
	if resultType := p.comparedFunctionResultType(parameterPosition); resultType != nil {
		return sqlmodel.ParameterContextComparison, nil, resultType
	}
	return p.detectParameterContext(parameterPosition)
}

// fullTextQueryContext recognises a placeholder matched against an FTS5 table's
// table-named column, as in `docs MATCH ?` or `docs = ?`.
//
// Both forms run a full-text query, which is text, so an equality is typed as the pattern
// a MATCH is. The column reference is kept so the placeholder is still named after the
// table.
//
// Takes context (sqlmodel.ParameterContext) which is the context inferred so far.
// Takes columnReference (*ast.ColumnReference) which is the column inferred so far.
//
// Returns sqlmodel.ParameterContext which is ParameterContextLike for a full-text query,
// else context.
// Returns *ast.ColumnReference which is columnReference unchanged.
func (p *parser) fullTextQueryContext(
	context sqlmodel.ParameterContext,
	columnReference *ast.ColumnReference,
) (sqlmodel.ParameterContext, *ast.ColumnReference) {
	if columnReference == nil || columnReference.TableAlias != "" || !p.fullTextSources[strings.ToLower(columnReference.ColumnName)] {
		return context, columnReference
	}
	if context != sqlmodel.ParameterContextComparison {
		return context, columnReference
	}
	return sqlmodel.ParameterContextLike, columnReference
}

// resolveBetweenColumn finds the column a BETWEEN bound is compared with, for a
// placeholder that is either bound of `column [NOT] BETWEEN low AND high`.
//
// Takes parameterPosition (int) which is the placeholder's token index.
//
// Returns *ast.ColumnReference which is the compared column, or nil when the placeholder
// is not a BETWEEN bound.
func (p *parser) resolveBetweenColumn(parameterPosition int) *ast.ColumnReference {
	betweenPosition := -1
	switch previous := p.tokenAt(parameterPosition - 1); {
	case previous.IsKeyword(tokenIdentifier, keywordBETWEEN):
		betweenPosition = parameterPosition - 1
	case previous.IsKeyword(tokenIdentifier, keywordAND):
		betweenPosition = p.findBetweenBeforeAnd(parameterPosition - 1)
	}
	if betweenPosition < 1 {
		return nil
	}
	columnPosition := betweenPosition - 1
	if p.tokenAt(columnPosition).IsKeyword(tokenIdentifier, keywordNOT) {
		columnPosition--
	}
	columnReference := p.extractColumnReference(columnPosition)
	if columnReference == nil || isKeywordColumnReference(columnReference) {
		return nil
	}
	return columnReference
}

// findBetweenBeforeAnd walks back from the AND of a `BETWEEN low AND high` to its
// BETWEEN, across a low bound that may hold parentheses, stopping at any other boolean or
// clause keyword.
//
// Takes andPosition (int) which is the AND keyword's token index.
//
// Returns int which is the BETWEEN keyword's token index, or -1 when the AND is not the
// one of a BETWEEN.
func (p *parser) findBetweenBeforeAnd(andPosition int) int {
	groups := p.parenthesisGroups()
	for index := andPosition - 1; index >= 0; index-- {
		current := p.Tokens[index]
		switch current.Kind {
		case tokenRightParen:
			index = groups.opening(index)
			if index < 0 {
				return -1
			}
			continue
		case tokenLeftParen:
			return -1
		default:
		}
		if current.IsKeyword(tokenIdentifier, keywordBETWEEN) {
			return index
		}
		if isLikeBoundaryKeyword(current.Keyword(tokenIdentifier)) {
			return -1
		}
	}
	return -1
}

// comparedFunctionResultType types a placeholder compared with a function call, as in
// `lower(name) = ?`, from the function's result type in the dialect's catalogue.
//
// Takes parameterPosition (int) which is the placeholder's token index.
//
// Returns *sqlmodel.SQLType which is the call's result type, or nil when the placeholder
// is not compared with a call or the call's result type depends on its arguments.
func (p *parser) comparedFunctionResultType(parameterPosition int) *sqlmodel.SQLType {
	operator := p.tokenAt(parameterPosition - 1)
	if operator.Kind != tokenOperator || !isComparisonOperator(operator.Value) {
		return nil
	}
	closing := parameterPosition - 2
	if p.tokenAt(closing).Kind != tokenRightParen {
		return nil
	}
	opening, arity := p.matchingCallParenthesis(closing)
	if opening < 1 {
		return nil
	}
	name := p.Tokens[opening-1]
	if name.Kind != tokenIdentifier || name.Quoted {
		return nil
	}
	return p.uniformResultType(strings.ToLower(name.Value), arity)
}

// matchingCallParenthesis finds the opening parenthesis of the call that closes at
// closing, and counts its top-level arguments.
//
// Takes closing (int) which is the closing parenthesis's token index.
//
// Returns opening (int) which is the opening parenthesis's token index, or -1 when
// unbalanced.
// Returns arity (int) which is the number of top-level arguments.
func (p *parser) matchingCallParenthesis(closing int) (opening int, arity int) {
	groups := p.parenthesisGroups()
	opening = groups.opening(closing)
	if opening < 0 {
		return -1, 0
	}
	return opening, callArity(opening, closing, groups.commaCount(closing))
}

// uniformResultType returns the result type every overload of a built-in or registered
// function shares for a call with the given number of arguments, so a value compared with
// the call can take that type.
//
// Takes name (string) which is the lower-cased function name.
// Takes arity (int) which is the number of arguments the call passes.
//
// Returns *sqlmodel.SQLType which is the shared result type, or nil when the function is
// unknown, its overloads disagree, or its result type depends on its arguments.
func (p *parser) uniformResultType(name string, arity int) *sqlmodel.SQLType {
	if p.functions == nil {
		return nil
	}
	var resolved *sqlmodel.SQLType
	for _, signature := range p.functions.Functions[name] {
		if !signatureAcceptsArity(signature, arity) {
			continue
		}
		returnType := signature.ReturnType
		if returnType.Category == sqlmodel.TypeCategoryUnknown {
			return nil
		}
		if resolved != nil && (resolved.Category != returnType.Category ||
			!strings.EqualFold(resolved.EngineName, returnType.EngineName)) {
			return nil
		}
		resolved = &returnType
	}
	return resolved
}

// bindComparisonOperand types the placeholders an expression comparison's right operand
// introduced from its left operand: a column gives its column, and a function call whose
// result type does not depend on its arguments gives that type.
//
// Takes left (ast.Expression) which is the comparison's left operand.
// Takes referenceCountBefore (int) which is len(parameterReferences) before the right
// operand was parsed.
func (p *parser) bindComparisonOperand(left ast.Expression, referenceCountBefore int) {
	var columnReference *ast.ColumnReference
	var castType *sqlmodel.SQLType
	switch operand := left.(type) {
	case *ast.ColumnRefExpression:
		columnReference = &ast.ColumnReference{TableAlias: operand.TableAlias, ColumnName: operand.ColumnName}
	case *ast.FunctionCallExpression:
		castType = p.uniformResultType(operand.FunctionName, len(operand.Arguments))
	}
	if columnReference == nil && castType == nil {
		return
	}
	for index := referenceCountBefore; index < len(p.parameterReferences); index++ {
		reference := &p.parameterReferences[index]
		if reference.Context != sqlmodel.ParameterContextUnknown || reference.ColumnReference != nil || reference.CastType != nil {
			continue
		}
		reference.Context = sqlmodel.ParameterContextComparison
		reference.ColumnReference = columnReference
		reference.CastType = castType
	}
}

// parseExpression parses a full SQL expression at the lowest precedence.
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

// parseOrExpression parses left-associative OR expressions.
//
// Returns ast.Expression which is the parsed expression tree.
func (p *parser) parseOrExpression() ast.Expression {
	left := p.parseAndExpression()
	for p.MatchKeyword(keywordOR) {
		right := p.parseAndExpression()
		left = &ast.LogicalOpExpression{
			Operator: "OR",
			Operands: []ast.Expression{left, right},
		}
	}
	return left
}

// parseAndExpression parses left-associative AND expressions.
//
// Returns ast.Expression which is the parsed expression tree.
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

// parseNotExpression parses an optional NOT prefix followed by a comparison expression.
//
// Returns ast.Expression which is the parsed expression tree.
func (p *parser) parseNotExpression() ast.Expression {
	if p.MatchKeyword(keywordNOT) {
		operand := p.parseComparisonExpression()
		return &ast.UnaryOpExpression{
			Operator: "NOT",
			Operand:  operand,
		}
	}
	return p.parseComparisonExpression()
}

// parseComparisonExpression parses comparison operators including IS, IN, BETWEEN, LIKE,
// GLOB, REGEXP, and MATCH.
//
// Returns ast.Expression which is the parsed expression tree.
func (p *parser) parseComparisonExpression() ast.Expression {
	left := p.parseBitwiseExpression()

	if p.Current().Kind == tokenOperator && isComparisonOperator(p.Current().Value) {
		return p.parseComparisonOperator(left)
	}

	if p.IsKeyword("IS") {
		return p.parseIsNullSuffix(left)
	}

	if p.MatchKeyword("ISNULL") {
		return &ast.IsNullExpression{Inner: left, Negated: false}
	}
	if p.MatchKeyword("NOTNULL") {
		return &ast.IsNullExpression{Inner: left, Negated: true}
	}

	notNegated := p.MatchKeyword(keywordNOT)
	if notNegated && p.MatchKeyword("NULL") {
		return &ast.IsNullExpression{Inner: left, Negated: true}
	}

	expression := p.parsePostfixComparisonSuffix(left)
	if expression != nil {
		return p.maybeNegate(notNegated, expression)
	}

	if notNegated {
		return &ast.UnaryOpExpression{Operator: keywordNOT, Operand: left}
	}

	return left
}

// parsePostfixComparisonSuffix attempts to parse a postfix comparison operator (IN,
// BETWEEN, LIKE, GLOB, REGEXP, MATCH) that follows the left operand. This is called after
// optionally consuming a NOT keyword so that constructs like NOT IN and NOT LIKE are
// handled.
//
// Takes left (ast.Expression) which holds the already-parsed left operand of the
// comparison.
//
// Returns ast.Expression which holds the parsed comparison expression, or nil if no
// postfix operator was found.
func (p *parser) parsePostfixComparisonSuffix(left ast.Expression) ast.Expression {
	if p.IsKeyword("IN") {
		return p.parseInListSuffix(left)
	}
	if p.IsKeyword(keywordBETWEEN) {
		return p.parseBetweenSuffix(left)
	}
	for _, keyword := range []string{"LIKE", "GLOB", "REGEXP", "MATCH"} {
		if p.MatchKeyword(keyword) {
			right := p.parseBitwiseExpression()
			if keyword == "LIKE" && p.MatchKeyword("ESCAPE") {
				p.parseBitwiseExpression()
			}
			return &ast.ComparisonExpression{Operator: keyword, Left: left, Right: right, Quantifier: ""}
		}
	}
	return nil
}

// maybeNegate wraps an expression in a NOT unary operator if the negated flag is set,
// otherwise returns the expression unchanged.
//
// Takes negated (bool) which indicates whether a NOT keyword was consumed before the
// expression.
// Takes expression (ast.Expression) which holds the expression to optionally negate.
//
// Returns ast.Expression which holds the original expression or a NOT-wrapped version.
func (*parser) maybeNegate(negated bool, expression ast.Expression) ast.Expression {
	if negated {
		return &ast.UnaryOpExpression{Operator: keywordNOT, Operand: expression}
	}
	return expression
}

// parseComparisonOperator parses an infix comparison operator and its right-hand operand.
//
// Takes left (ast.Expression) which holds the left operand already parsed.
//
// Returns ast.Expression which is the parsed comparison.
func (p *parser) parseComparisonOperator(left ast.Expression) ast.Expression {
	operator := p.Advance().Value
	referenceCountBefore := len(p.parameterReferences)
	right := p.parseBitwiseExpression()
	p.bindComparisonOperand(left, referenceCountBefore)
	return &ast.ComparisonExpression{Operator: operator, Left: left, Right: right, Quantifier: ""}
}

// parseBitwiseExpression parses left-associative bitwise operators (&, |, <<, >>).
//
// Returns ast.Expression which is the parsed expression tree.
func (p *parser) parseBitwiseExpression() ast.Expression {
	left := p.parseJSONAccessExpression()

	for p.Current().Kind == tokenOperator &&
		(p.Current().Value == "&" || p.Current().Value == "|" ||
			p.Current().Value == "<<" || p.Current().Value == ">>") {
		operator := p.Advance().Value
		right := p.parseJSONAccessExpression()
		left = &ast.BinaryOpExpression{Operator: operator, Left: left, Right: right, MayBeNull: false}
	}

	return left
}

// parseJSONAccessExpression parses left-associative JSON arrow operators (-> and ->>).
//
// Returns ast.Expression which is the parsed expression tree.
func (p *parser) parseJSONAccessExpression() ast.Expression {
	left := p.parseAddExpression()

	for p.Current().Kind == tokenArrow || p.Current().Kind == tokenDoubleArrow {
		operator := p.Advance().Value
		right := p.parseAddExpression()
		left = &ast.BinaryOpExpression{Operator: operator, Left: left, Right: right, MayBeNull: false}
	}

	return left
}

// parseIsNullSuffix parses the IS [NOT] NULL suffix after a left operand, or the SQLite
// null-safe equality form col IS [NOT] <operand> when the operand is not NULL.
//
// IS NULL / IS NOT NULL return an IsNullExpression. For the null-safe equality form (for
// example col IS ?) the operand is parsed so the token stream stays synchronised, and any
// placeholder it introduces is tagged with the LHS column reference exactly as col = ?
// is, so a col IS ? nested inside a function argument, subquery or CASE is typed from the
// column rather than left untyped. This mirrors the flat-scan IS handling.
//
// Takes left (ast.Expression) which holds the inner expression.
//
// Returns ast.Expression which is the parsed IsNull or comparison expression.
func (p *parser) parseIsNullSuffix(left ast.Expression) ast.Expression {
	p.Advance()
	negated := p.MatchKeyword(keywordNOT)
	if p.MatchKeyword("NULL") {
		return &ast.IsNullExpression{Inner: left, Negated: negated}
	}
	if p.IsKeyword("DISTINCT") && p.Peek().IsKeyword(tokenIdentifier, keywordFROM) {
		p.Advance()
		p.Advance()
	}

	parameterCountBefore := p.parameterCount
	referenceCountBefore := len(p.parameterReferences)
	right := p.parseBitwiseExpression()

	if columnExpression, ok := left.(*ast.ColumnRefExpression); ok {
		columnReference := &ast.ColumnReference{
			TableAlias: columnExpression.TableAlias,
			ColumnName: columnExpression.ColumnName,
		}
		for reference := range p.newParameterReferences(referenceCountBefore, parameterCountBefore) {
			if reference.ColumnReference == nil {
				reference.Context = sqlmodel.ParameterContextComparison
				reference.ColumnReference = columnReference
			}
		}
	}

	operator := "IS"
	if negated {
		operator = "IS NOT"
	}
	return &ast.ComparisonExpression{Operator: operator, Left: left, Right: right, Quantifier: ""}
}

// parseInListSuffix parses the IN (...) suffix and tags any parameters inside it with the
// IN-list context.
//
// Takes left (ast.Expression) which holds the inner expression.
//
// Returns ast.Expression which is the parsed InList expression.
func (p *parser) parseInListSuffix(left ast.Expression) ast.Expression {
	p.Advance()
	if p.isSubqueryStart() {
		innerAnalysis, ok := p.analyseSubqueryBody()
		if !ok {
			return &ast.InListExpression{Inner: left, Values: nil}
		}
		return &ast.InListExpression{
			Inner:  left,
			Values: []ast.Expression{&ast.ScalarSubqueryExpression{InnerQuery: innerAnalysis}},
		}
	}
	parameterCountBefore := p.parameterCount
	referenceCountBefore := len(p.parameterReferences)
	values := p.parseParenthesisedExpressionList()

	var columnReference *ast.ColumnReference
	if columnExpression, ok := left.(*ast.ColumnRefExpression); ok {
		columnReference = &ast.ColumnReference{
			TableAlias: columnExpression.TableAlias,
			ColumnName: columnExpression.ColumnName,
		}
	}
	for reference := range p.newParameterReferences(referenceCountBefore, parameterCountBefore) {
		reference.Context = sqlmodel.ParameterContextInList
		if columnReference != nil {
			reference.ColumnReference = columnReference
		}
	}

	return &ast.InListExpression{Inner: left, Values: values}
}

// parseBetweenSuffix parses a BETWEEN low AND high suffix and tags parameters inside it
// with the BETWEEN context.
//
// Takes left (ast.Expression) which holds the inner expression.
//
// Returns ast.Expression which is the parsed Between expression.
func (p *parser) parseBetweenSuffix(left ast.Expression) ast.Expression {
	p.Advance()
	parameterCountBefore := p.parameterCount
	referenceCountBefore := len(p.parameterReferences)
	low := p.parseAddExpression()
	p.requireExpressionKeyword(keywordAND)
	high := p.parseAddExpression()

	var columnReference *ast.ColumnReference
	if columnExpression, ok := left.(*ast.ColumnRefExpression); ok {
		columnReference = &ast.ColumnReference{
			TableAlias: columnExpression.TableAlias,
			ColumnName: columnExpression.ColumnName,
		}
	}
	for reference := range p.newParameterReferences(referenceCountBefore, parameterCountBefore) {
		reference.Context = sqlmodel.ParameterContextBetween
		if columnReference != nil {
			reference.ColumnReference = columnReference
		}
	}

	return &ast.BetweenExpression{Inner: left, Low: low, High: high}
}

// parseParenthesisedExpressionList parses a parenthesised, comma separated list of
// expressions.
//
// Returns []ast.Expression which holds the parsed list values.
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
	p.closeParenthesis()
	return values
}

// parseUnaryExpression parses an optional unary prefix (+, -, ~) and the following
// primary expression.
//
// Returns ast.Expression which is the parsed expression tree.
func (p *parser) parseUnaryExpression() ast.Expression {
	if p.Current().Kind == tokenOperator && (p.Current().Value == "-" || p.Current().Value == "+" || p.Current().Value == "~") {
		operator := p.Advance().Value
		inner := p.parsePrimaryExpression()
		if _, ok := inner.(*ast.LiteralExpression); ok {
			return p.parseCollateSuffix(inner)
		}
		return p.parseCollateSuffix(&ast.UnaryOpExpression{Operator: operator, Operand: inner})
	}
	return p.parseCollateSuffix(p.parsePrimaryExpression())
}

// parseCollateSuffix consumes an optional COLLATE clause after an expression.
//
// Takes expression (ast.Expression) which is the prior expression to attach to.
//
// Returns ast.Expression which is the original expression.
func (p *parser) parseCollateSuffix(expression ast.Expression) ast.Expression {
	if p.MatchKeyword(keywordCOLLATE) {
		if p.Current().Kind == tokenIdentifier {
			p.Advance()
		}
	}
	return expression
}

// parsePrimaryExpression parses the highest-precedence expression atom: literals,
// parameters, identifiers, function calls, and subqueries.
//
// Returns ast.Expression which is the parsed atom.
func (p *parser) parsePrimaryExpression() ast.Expression {
	current := p.Current()

	switch current.Kind {
	case tokenNumber:
		p.Advance()
		return &ast.LiteralExpression{TypeName: numberLiteralType(current.Value)}

	case tokenString:
		p.Advance()
		return &ast.LiteralExpression{TypeName: "text"}

	case tokenBlobLiteral:
		p.Advance()
		return &ast.LiteralExpression{TypeName: "blob"}

	case tokenQuestionMark, tokenNumberedParameter, tokenNamedParameter:
		parameterToken := p.Current()
		p.Advance()
		number := p.registerParameterFromToken(parameterToken, sqlmodel.ParameterContextUnknown, nil, nil)
		return &ast.ParameterExpression{Number: number}

	case tokenLeftParen:
		if p.isSubqueryStart() {
			return p.parseScalarSubquery()
		}
		return p.parseParenthesisedExpression()

	case tokenIdentifier:
		return p.parseIdentifierExpression()

	default:
		p.recordSyntaxError(dialectkit.NewParseError(current, messageExpectedExpression))
		p.Advance()
		return &ast.UnknownExpression{}
	}
}

// parseParenthesisedExpression parses an expression in parentheses, or a row value of
// several, (a, b), which SQLite compares element by element and tests for membership.
//
// Returns ast.Expression which is the expression, or a row for a row value.
func (p *parser) parseParenthesisedExpression() ast.Expression {
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

// closeParenthesis consumes the closing parenthesis of a group, recording a syntax error
// when the group is left open, so a condition missing it is never read as complete.
func (p *parser) closeParenthesis() {
	if p.Current().Kind == tokenRightParen {
		p.Advance()
		return
	}
	p.recordSyntaxError(dialectkit.NewParseError(p.Current(), messageUnmatchedParenthesis))
}

// requireExpressionKeyword consumes a keyword an expression must write at this point,
// such as THEN or END in a CASE expression, recording a syntax error when it is missing.
//
// Takes keyword (string) which is the keyword.
func (p *parser) requireExpressionKeyword(keyword string) {
	if !p.MatchKeyword(keyword) {
		p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected keyword "+keyword))
	}
}

// parseIdentifierExpression parses an identifier-led primary expression covering keywords
// (NULL, TRUE, CAST, etc.) and column references.
//
// Returns ast.Expression which is the parsed atom.
func (p *parser) parseIdentifierExpression() ast.Expression {
	first := p.Position
	return p.locate(p.parseIdentifierRootedExpression(), first)
}

// parseIdentifierRootedExpression parses an identifier-led primary expression for
// parseIdentifierExpression, which records where its column or function was written.
//
// Returns ast.Expression which is the parsed atom.
func (p *parser) parseIdentifierRootedExpression() ast.Expression {
	upper := p.Current().Keyword(tokenIdentifier)

	if nonExpressionKeywords[upper] {
		p.recordSyntaxError(dialectkit.NewParseError(p.Current(), messageExpectedExpression))
		return &ast.UnknownExpression{}
	}

	if upper == "NULL" {
		p.Advance()
		return nil
	}

	if currentTimeKeywords[upper] {
		p.Advance()
		return &ast.LiteralExpression{TypeName: literalTypeText}
	}

	if upper == "TRUE" || upper == "FALSE" {
		p.Advance()
		return &ast.LiteralExpression{TypeName: "boolean"}
	}

	if upper == "CAST" {
		return p.parseCastExpression()
	}

	if upper == "COALESCE" {
		return p.parseCoalesceExpression()
	}

	if upper == "CASE" {
		return p.parseCaseExpression()
	}

	if upper == keywordEXISTS {
		p.Advance()
		return p.parseExistsSubquery()
	}

	return p.parseColumnOrFunctionReference()
}

// parseColumnOrFunctionReference parses a bare or qualified column reference, a qualified
// star, or a function call, starting at a name.
//
// Returns ast.Expression which is the column reference, the call, or an unknown
// expression for a qualified star.
func (p *parser) parseColumnOrFunctionReference() ast.Expression {
	nameToken := p.Advance()
	name := nameToken.Value
	if p.readsAsString(nameToken) {
		return &ast.LiteralExpression{TypeName: literalTypeText}
	}

	if p.Current().Kind == tokenDot {
		p.Advance()
		if p.Current().Kind == tokenStar {
			p.Advance()
			return &ast.UnknownExpression{}
		}
		if p.Current().Kind == tokenIdentifier {
			columnName := p.Advance().Value
			if p.Current().Kind == tokenDot && p.Peek().Kind == tokenIdentifier {
				p.Advance()
				name, columnName = columnName, p.Advance().Value
			}
			return &ast.ColumnRefExpression{
				TableAlias: name,
				ColumnName: columnName,
				Span:       ast.Span{},
			}
		}
	}

	if p.Current().Kind == tokenLeftParen {
		return p.parseFunctionCall(name)
	}

	return &ast.ColumnRefExpression{
		TableAlias: "",
		ColumnName: name,
		Span:       ast.Span{},
	}
}

// parseFunctionCall parses a function-call expression including any FILTER clause and
// OVER window suffix.
//
// Takes functionName (string) which is the already-consumed function identifier.
//
// Returns ast.Expression which is the parsed call or window expression.
func (p *parser) parseFunctionCall(functionName string) ast.Expression {
	p.Advance()

	loweredName := strings.ToLower(functionName)
	arguments := p.parseFunctionArguments(loweredName)

	result := &ast.FunctionCallExpression{
		FunctionName:     loweredName,
		Schema:           "",
		Arguments:        arguments,
		FilterExpression: nil,
		Span:             ast.Span{},
	}

	if p.MatchKeyword("FILTER") {
		if p.Current().Kind == tokenLeftParen {
			p.Advance()
			p.requireExpressionKeyword(keywordWHERE)
			result.FilterExpression = p.parseExpression()
			p.closeParenthesis()
		}
	}

	if p.IsKeyword("OVER") {
		return p.parseWindowSuffix(result)
	}

	return result
}

// parseFunctionArguments parses the parenthesised arguments of a function call and tags
// any newly-seen parameters as function arguments.
//
// Each top-level argument expression consumes one 0-based ordinal slot (a literal, a
// column or a sub-expression still advances the slot), so a placeholder sitting directly
// in the call's argument list is tagged with the enclosing function name and the slot it
// occupied. A placeholder nested inside an inner call was already tagged by that inner
// call's parseFunctionArguments, so the Context==ParameterContextUnknown guard leaves it
// untouched and the inner metadata is not clobbered.
//
// Takes loweredName (string) which is the lower-cased enclosing function name recorded on
// each placeholder so the analyser can look the function up and back-propagate the
// declared argument type.
//
// Returns []ast.Expression which holds the parsed argument expressions.
func (p *parser) parseFunctionArguments(loweredName string) []ast.Expression {
	var arguments []ast.Expression

	if p.Current().Kind == tokenStar {
		p.Advance()
		p.closeParenthesis()
		return arguments
	}

	p.MatchKeyword("DISTINCT")
	p.MatchKeyword("ALL")

	if p.Current().Kind == tokenRightParen {
		p.Advance()
		return arguments
	}

	argumentOrdinal := 0
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		if p.IsKeyword(keywordORDER) {
			break
		}
		referenceCountBefore := len(p.parameterReferences)
		arguments = append(arguments, p.parseExpression())
		p.markFunctionArgumentParameters(referenceCountBefore, loweredName, argumentOrdinal)
		argumentOrdinal++
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}

	p.parseFunctionOrderByClause()
	p.closeParenthesis()

	return arguments
}

// markFunctionArgumentParameters tags every placeholder registered while parsing one
// top-level function argument with the function-argument context, the enclosing function
// name and the argument's 0-based ordinal slot. Placeholders that an inner call already
// tagged are skipped via the Context==ParameterContextUnknown guard, so a nested call's
// metadata is preserved.
//
// The boundary is the parameterReferences slice length rather than the parameter-count
// watermark: references are always appended, so referenceCountBefore..end is exactly the
// set added by this argument, which is correct even for out-of-order numbered
// placeholders (for example ?3 then ?1) where the watermark would not advance.
//
// Takes referenceCountBefore (int) which is len(parameterReferences) before the argument
// was parsed.
// Takes loweredName (string) which is the lower-cased enclosing function name.
// Takes argumentOrdinal (int) which is the 0-based slot the argument occupies.
func (p *parser) markFunctionArgumentParameters(referenceCountBefore int, loweredName string, argumentOrdinal int) {
	for index := referenceCountBefore; index < len(p.parameterReferences); index++ {
		if p.parameterReferences[index].Context == sqlmodel.ParameterContextUnknown {
			p.parameterReferences[index].Context = sqlmodel.ParameterContextFunctionArgument
			p.parameterReferences[index].EnclosingFunctionName = loweredName
			p.parameterReferences[index].ArgumentOrdinal = argumentOrdinal
		}
	}
}

// markFullTextQueryParameters tags every placeholder registered while parsing one
// argument of an FTS5 table's table-valued form, `docs(?)`, as a full-text query, which
// is text, in place of the function-argument context a real function would give it.
//
// Takes referenceCountBefore (int) which is len(parameterReferences) before the argument
// was parsed.
func (p *parser) markFullTextQueryParameters(referenceCountBefore int) {
	for index := referenceCountBefore; index < len(p.parameterReferences); index++ {
		if p.parameterReferences[index].Context == sqlmodel.ParameterContextUnknown {
			p.parameterReferences[index].Context = sqlmodel.ParameterContextLike
			p.parameterReferences[index].ColumnReference = nil
		}
	}
}

// markPragmaArgumentParameters types every placeholder registered while parsing one
// argument of a pragma table-valued function, such as pragma_table_info(?), as the text
// name of the table, index or schema it describes.
//
// Takes referenceCountBefore (int) which is len(parameterReferences) before the argument
// was parsed.
func (p *parser) markPragmaArgumentParameters(referenceCountBefore int) {
	for index := referenceCountBefore; index < len(p.parameterReferences); index++ {
		if p.parameterReferences[index].CastType == nil {
			p.parameterReferences[index].Context = sqlmodel.ParameterContextFunctionArgument
			p.parameterReferences[index].CastType = new(p.types.normalise(literalTypeText))
		}
	}
}

// newParameterReferences yields the parameter references recorded since a mark that
// introduced a parameter number above the mark's count: the placeholders an operand
// parsed after the mark wrote for the first time.
//
// Only the references after the mark are visited, so tagging an operand's placeholders
// costs no more than the operand holds.
//
// Takes referenceCountBefore (int) which is len(parameterReferences) at the mark.
// Takes parameterCountBefore (int) which is parameterCount at the mark.
//
// Returns iter.Seq[*ast.RawParameterReference] which yields each such reference, so the
// caller can change it in place.
func (p *parser) newParameterReferences(referenceCountBefore, parameterCountBefore int) iter.Seq[*ast.RawParameterReference] {
	return func(yield func(*ast.RawParameterReference) bool) {
		for index := referenceCountBefore; index < len(p.parameterReferences); index++ {
			if p.parameterReferences[index].Number > parameterCountBefore && !yield(&p.parameterReferences[index]) {
				return
			}
		}
	}
}

// parseFunctionOrderByClause consumes the optional ORDER BY clause that may appear inside
// an aggregate function call.
func (p *parser) parseFunctionOrderByClause() {
	if !p.MatchKeyword(keywordORDER) {
		return
	}
	p.MatchKeyword(keywordBY)
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		p.parseExpression()
		p.MatchKeyword("ASC")
		p.MatchKeyword("DESC")
		p.MatchKeyword("NULLS")
		p.MatchKeyword("FIRST")
		p.MatchKeyword("LAST")
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
}

// parseWindowSuffix parses the OVER clause that may follow an aggregate or window
// function call: a parenthesised window specification, or the name of a window the
// statement's WINDOW clause defines.
//
// Takes innerFunction (*ast.FunctionCallExpression) which is the inner function to wrap.
//
// Returns ast.Expression which is the window function expression.
func (p *parser) parseWindowSuffix(innerFunction *ast.FunctionCallExpression) ast.Expression {
	p.Advance()

	if p.Current().Kind != tokenLeftParen {
		if p.Current().Kind == tokenIdentifier && !nonExpressionKeywords[p.Current().Keyword(tokenIdentifier)] {
			p.Advance()
		} else {
			p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected a window name or specification"))
		}
		return &ast.WindowFunctionExpression{Function: innerFunction}
	}
	p.parseWindowSpecification()

	return &ast.WindowFunctionExpression{Function: innerFunction}
}

// parseWindowSpecification parses a parenthesised window specification: an optional base
// window name, PARTITION BY, ORDER BY and a frame.
func (p *parser) parseWindowSpecification() {
	p.Advance()

	if p.Current().Kind == tokenIdentifier && !p.IsKeyword("PARTITION", keywordORDER, "ROWS", "RANGE", "GROUPS") {
		p.Advance()
	}

	if p.MatchKeyword("PARTITION") {
		p.MatchKeyword("BY")
		p.parseExpression()
		for p.Current().Kind == tokenComma {
			p.Advance()
			p.parseExpression()
		}
	}

	if p.MatchKeyword(keywordORDER) {
		p.MatchKeyword("BY")
		p.parseExpression()
		p.skipSortModifiers()
		for p.Current().Kind == tokenComma {
			p.Advance()
			p.parseExpression()
			p.skipSortModifiers()
		}
	}

	if p.IsKeyword("ROWS", "RANGE", "GROUPS") {
		p.skipWindowFrame()
	}
	p.closeParenthesis()
}

// parseWindowClause parses the WINDOW clause of a SELECT, which names the window
// specifications its OVER clauses may refer to.
//
// Returns bool which is true when the statement writes a WINDOW clause.
func (p *parser) parseWindowClause() bool {
	if !p.MatchKeyword("WINDOW") {
		return false
	}
	for {
		if p.Current().Kind != tokenIdentifier {
			p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected a window name"))
			return true
		}
		p.Advance()
		p.requireExpressionKeyword(keywordAS)
		if p.Current().Kind != tokenLeftParen {
			p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected a window specification"))
			return true
		}
		p.parseWindowSpecification()
		if p.Current().Kind != tokenComma {
			return true
		}
		p.Advance()
	}
}

// skipWindowFrame consumes a ROWS, RANGE, or GROUPS window frame clause.
func (p *parser) skipWindowFrame() {
	p.Advance()
	if p.MatchKeyword(keywordBETWEEN) {
		p.skipFrameBound()
		p.MatchKeyword(keywordAND)
		p.skipFrameBound()
	} else {
		p.skipFrameBound()
	}
	p.MatchKeyword("EXCLUDE")
	p.MatchKeyword("CURRENT")
	p.MatchKeyword("ROW")
	p.MatchKeyword(keywordGROUP)
	p.MatchKeyword("TIES")
	p.IsKeyword("NO", "OTHERS")
	if p.MatchKeyword("NO") {
		p.MatchKeyword("OTHERS")
	}
}

// skipFrameBound consumes one bound of a window frame clause (CURRENT ROW, UNBOUNDED
// PRECEDING, expression PRECEDING, etc.).
func (p *parser) skipFrameBound() {
	if p.MatchKeyword("CURRENT") {
		p.MatchKeyword("ROW")
		return
	}
	if p.MatchKeyword("UNBOUNDED") {
		p.MatchKeyword("PRECEDING")
		p.MatchKeyword("FOLLOWING")
		return
	}
	p.parseExpression()
	p.MatchKeyword("PRECEDING")
	p.MatchKeyword("FOLLOWING")
}

// parseScalarSubquery parses a parenthesised SELECT used as a scalar expression, merging
// its parameter references back into the parent parser.
//
// Returns ast.Expression which is the parsed scalar subquery, or UnknownExpression on
// failure.
func (p *parser) parseScalarSubquery() ast.Expression {
	innerAnalysis, ok := p.analyseSubqueryBody()
	if !ok {
		return &ast.UnknownExpression{}
	}
	return &ast.ScalarSubqueryExpression{InnerQuery: innerAnalysis}
}

// parseExistsSubquery parses an EXISTS (SELECT ...) expression, merging its parameter
// references back into the parent parser.
//
// Returns ast.Expression which is the parsed EXISTS expression, empty on failure.
func (p *parser) parseExistsSubquery() ast.Expression {
	innerAnalysis, ok := p.analyseSubqueryBody()
	if !ok {
		return &ast.ExistsExpression{}
	}
	return &ast.ExistsExpression{InnerQuery: innerAnalysis}
}

// analyseSubqueryBody collects a parenthesised subquery, analyses it in a child parser
// that inherits the parameter and depth state, and splices the child's parameter results
// back.
//
// Shared by the scalar, EXISTS and predicate subquery parsers. A subquery that fails to
// parse records a syntax error for the whole statement, so the parameters it holds are
// never silently dropped. A parser that does not analyse subqueries again, as when it
// records a clause's expressions after the clause scan, takes the analysis the scan made
// of the subquery.
//
// Returns *ast.RawQueryAnalysis which describes the inner subquery.
// Returns bool which is true when the subquery was collected and analysed successfully.
func (p *parser) analyseSubqueryBody() (*ast.RawQueryAnalysis, bool) {
	open := p.Current()
	innerTokens, collectError := p.collectParenthesised()
	if collectError != nil {
		p.recordSyntaxError(collectError)
		return nil, false
	}
	if p.skipSubqueries {
		scanned, found := p.scannedSubqueries[open.Start]
		return scanned, found
	}

	childParser, innerAnalysis, analyseError := p.analyseNested(innerTokens)
	if analyseError != nil {
		p.recordSyntaxError(analyseError)
		return nil, false
	}
	p.parameterCount = childParser.parameterCount
	p.parameterReferences = append(p.parameterReferences, childParser.parameterReferences...)
	p.scannedSubqueries[open.Start] = innerAnalysis

	return innerAnalysis, true
}

// parseCastExpression parses a CAST(expression AS type) expression, typed as
// castTargetName names it, and tags the inner parameter, when present, with the type
// parameterCastType gives. The target's modifiers are read as a declared column's are, by
// typeModifiersAt.
//
// Returns ast.Expression which is the parsed CAST expression, or UnknownExpression on
// failure.
func (p *parser) parseCastExpression() ast.Expression {
	p.Advance()
	if p.Current().Kind != tokenLeftParen {
		return &ast.UnknownExpression{}
	}
	p.Advance()

	parameterCountBefore := p.parameterCount
	inner := p.parseExpression()

	p.requireExpressionKeyword(keywordAS)

	var typeParts []string
	for p.Current().Kind == tokenIdentifier {
		typeParts = append(typeParts, p.Advance().Value)
	}
	typeName := strings.Join(typeParts, " ")

	modifiers := p.typeModifiersAt(p.Position)
	if p.Current().Kind == tokenLeftParen {
		if err := p.skipParenthesised(); err != nil {
			p.recordSyntaxError(fmt.Errorf("parsing CAST type modifiers: %w", err))
		}
	}
	p.closeParenthesis()

	castName := p.types.castTargetName(typeName, modifiers)
	if typeName != "" && p.parameterCount == parameterCountBefore+1 {
		lastIndex := len(p.parameterReferences) - 1
		if lastIndex >= 0 {
			p.parameterReferences[lastIndex].Context = sqlmodel.ParameterContextCast
			p.parameterReferences[lastIndex].CastType = new(p.types.parameterCastType(typeName, modifiers))
		}
	}

	return &ast.CastExpression{
		TypeName: castName,
		Inner:    inner,
	}
}

// typeModifiersAt reads, without consuming anything, the modifiers of the list the '(' at
// open holds when parseTypeModifiers would read it as a declared column's, so a CAST's
// target and a column declared with the same type agree.
//
// Takes open (int) which is the index of the token that may open the list.
//
// Returns []int which holds the modifiers in order, or nil when the token at open opens
// no list parseTypeModifiers reads.
func (p *parser) typeModifiersAt(open int) []int {
	saved := p.Position
	defer func() { p.Position = saved }()
	p.Position = open
	modifiers, err := p.parseTypeModifiers()
	if err != nil {
		return nil
	}
	return modifiers
}

// parseCoalesceExpression parses a COALESCE(...) expression.
//
// Returns ast.Expression which is the parsed Coalesce expression, or UnknownExpression on
// failure.
func (p *parser) parseCoalesceExpression() ast.Expression {
	p.Advance()
	if p.Current().Kind != tokenLeftParen {
		return &ast.UnknownExpression{}
	}
	p.Advance()

	var arguments []ast.Expression
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		arguments = append(arguments, p.parseExpression())
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	p.closeParenthesis()

	return &ast.CoalesceExpression{
		Arguments: arguments,
	}
}

// parseCaseExpression parses a CASE expression with branches and else.
//
// Returns ast.Expression which is the parsed CASE expression.
func (p *parser) parseCaseExpression() ast.Expression {
	p.Advance()

	if !p.IsKeyword("WHEN") {
		p.parseExpression()
	}

	var branches []ast.CaseWhenBranch
	for p.MatchKeyword("WHEN") {
		condition := p.parseExpression()
		p.requireExpressionKeyword("THEN")
		result := p.parseExpression()
		branches = append(branches, ast.CaseWhenBranch{Condition: condition, Result: result})
	}

	expression := &ast.CaseWhenExpression{Branches: branches, ElseResult: nil}

	if p.MatchKeyword("ELSE") {
		expression.ElseResult = p.parseExpression()
	}

	p.requireExpressionKeyword("END")
	return expression
}

// isExpressionTerminator reports whether the token is a clause-level keyword that ends an
// expression scan.
//
// Takes current (token) which is the token to inspect.
// Takes terminators (map[string]bool) which identifies keywords that end the current
// expression.
//
// Returns bool which is true when the token ends the expression.
func isExpressionTerminator(current token, terminators map[string]bool) bool {
	return current.Kind == tokenIdentifier && terminators[current.Keyword(tokenIdentifier)]
}

// callArity counts a call's arguments from its parentheses and top-level commas.
//
// Takes opening (int) which is the opening parenthesis's token index.
// Takes closing (int) which is the closing parenthesis's token index.
// Takes commas (int) which is the number of top-level commas between them.
//
// Returns int which is zero for an empty argument list and one more than the commas
// otherwise.
func callArity(opening int, closing int, commas int) int {
	if opening+1 == closing {
		return 0
	}
	return commas + 1
}

// signatureAcceptsArity reports whether a call with the given number of arguments can
// match a signature.
//
// Takes signature (*sqlmodel.Function) which is the overload to check.
// Takes arity (int) which is the number of call-site arguments.
//
// Returns bool which is true when the arity lies within the signature's bounds.
func signatureAcceptsArity(signature *sqlmodel.Function, arity int) bool {
	if arity < signature.RequiredArguments() {
		return false
	}
	return signature.IsVariadic && len(signature.Arguments) > 0 || arity <= len(signature.Arguments)
}

// isComparisonOperator reports whether the supplied operator is a SQL comparison
// operator.
//
// Takes operator (string) which is the operator literal.
//
// Returns bool which is true for =, <>, !=, <, >, <=, and >=.
func isComparisonOperator(operator string) bool {
	switch operator {
	case "=", "==", "<>", "!=", "<", ">", "<=", ">=":
		return true
	}
	return false
}

// numberLiteralType types SQLite numeric literals from their scanned shape and value.
//
// Integers are named INTEGER when they fit in 32 bits and BIGINT otherwise; both names
// normalise to signed 64-bit integers. Out-of-range values, fractions and exponents are
// REAL.
//
// Takes text (string) which is the literal's source text.
//
// Returns string which is the type name the literal normalises from.
func numberLiteralType(text string) string {
	if lexicalRules.Numbers.ClassifyNumber(text) != dialectkit.NumberInteger {
		return literalTypeReal
	}
	digits, isHex := strings.CutPrefix(strings.ToLower(text), "0x")
	base, limit := decimalBase, uint64(math.MaxInt64)
	if isHex {
		base, limit = hexadecimalBase, math.MaxUint64
	}
	magnitude, err := strconv.ParseUint(digits, base, 64)
	switch {
	case err != nil || magnitude > limit:
		return literalTypeReal
	case magnitude > math.MaxInt32:
		return literalTypeBigint
	default:
		return literalTypeInteger
	}
}

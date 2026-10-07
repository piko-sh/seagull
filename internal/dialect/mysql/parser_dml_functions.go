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
	"fmt"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

var (
	// castTargetColumnTypes maps each conversion target that names no column type to the
	// column type MySQL converts to.
	castTargetColumnTypes = map[string]string{
		"SIGNED": "bigint", "SIGNED INTEGER": "bigint", "SIGNED INT": "bigint",
		"UNSIGNED": "bigint unsigned", "UNSIGNED INTEGER": "bigint unsigned", "UNSIGNED INT": "bigint unsigned",
	}
)

// specialFunctionParser is the signature of a custom parser for a function whose argument
// grammar diverges from a plain comma list.
type specialFunctionParser func(string, string) ast.Expression

// parseFunctionCall parses a function call beginning at the open paren.
//
// Takes name (string) which is the function name.
// Takes schema (string) which is the optional schema qualifier.
//
// Returns ast.Expression which is the parsed function call.
func (p *parser) parseFunctionCall(name string, schema string) ast.Expression {
	p.Advance()
	loweredName := strings.ToLower(name)

	if p.Current().Kind == tokenStar || p.Current().Kind == tokenRightParen {
		return p.parseFunctionCallWithoutArguments(loweredName, schema)
	}

	if handler := p.specialFunctionHandler(loweredName); handler != nil {
		return handler(loweredName, schema)
	}

	return p.parseFunctionCallWithArguments(loweredName, schema)
}

// parseFunctionCallWithoutArguments handles functions called with `*` or no args.
//
// Takes loweredName (string) which is the lower-cased function name.
// Takes schema (string) which is the optional schema qualifier.
//
// Returns ast.Expression which is the parsed function call.
func (p *parser) parseFunctionCallWithoutArguments(loweredName string, schema string) ast.Expression {
	if p.Current().Kind == tokenStar {
		p.Advance()
	}
	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}
	result := &ast.FunctionCallExpression{
		FunctionName:     loweredName,
		Schema:           schema,
		FilterExpression: nil,
		Arguments:        nil,
		Span:             ast.Span{},
	}
	return p.parseFunctionSuffix(result)
}

// parseFunctionCallWithArguments parses a function call with one or more arguments.
//
// Takes loweredName (string) which is the lower-cased function name.
// Takes schema (string) which is the optional schema qualifier.
//
// Returns ast.Expression which is the parsed function call.
func (p *parser) parseFunctionCallWithArguments(loweredName string, schema string) ast.Expression {
	p.MatchKeyword("DISTINCT")
	p.MatchKeyword(keywordALL)

	referenceCountBefore := len(p.parameterReferences)
	arguments, argumentBoundaries := p.parseFunctionArguments()
	arguments = readKeywordArgument(loweredName, arguments)
	p.parseFunctionOrderByClause()
	p.closeParenthesis()
	if sorted := p.parseWithinGroupIfPresent(); sorted != nil {
		arguments = append(arguments, sorted)
		argumentBoundaries = append(argumentBoundaries, len(p.parameterReferences))
	}

	p.markParametersAsFunctionArguments(
		referenceCountBefore, qualifiedFunctionName(loweredName, schema), argumentBoundaries)

	result := &ast.FunctionCallExpression{
		FunctionName:     loweredName,
		Schema:           schema,
		Arguments:        arguments,
		FilterExpression: nil,
		Span:             ast.Span{},
	}

	return p.parseFunctionSuffix(result)
}

// parseFunctionArguments parses a comma-separated argument list, recording how many
// parameter references the parser holds after each top-level argument, so callers can map
// each reference an argument registered to the argument's zero-based slot.
//
// Returns []ast.Expression which is the parsed argument list.
// Returns []int which holds, per top-level argument index, the number of parameter
// references once that argument has been fully parsed. The reference at index r belongs
// to argument slot i when r is below boundaries[i] and at or above boundaries[i-1].
func (p *parser) parseFunctionArguments() ([]ast.Expression, []int) {
	var arguments []ast.Expression
	var argumentBoundaries []int
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		if p.IsKeyword(keywordORDER, keywordLIMIT, keywordSEPARATOR) {
			break
		}
		arguments = append(arguments, p.parseExpression())
		argumentBoundaries = append(argumentBoundaries, len(p.parameterReferences))
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	return arguments, argumentBoundaries
}

// parseWithinGroupIfPresent parses the WITHIN GROUP (ORDER BY value) clause of an
// ordered-set function, such as MariaDB's PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY
// price) OVER (...), whose value the catalogue declares as the argument after the
// function's own.
//
// Returns ast.Expression which is the value the clause sorts, or nil when there is no
// such clause.
func (p *parser) parseWithinGroupIfPresent() ast.Expression {
	if !p.IsKeyword("WITHIN") || !p.Peek().IsKeyword(tokenIdentifier, keywordGROUP) {
		return nil
	}
	p.Advance()
	p.Advance()
	if p.Current().Kind != tokenLeftParen {
		p.reportSyntaxError("expected '(' after WITHIN GROUP")
		return &ast.UnknownExpression{}
	}
	p.Advance()
	if !p.MatchKeyword(keywordORDER) || !p.MatchKeyword(keywordBY) {
		p.reportSyntaxError("expected ORDER BY in WITHIN GROUP")
		return &ast.UnknownExpression{}
	}
	sorted := p.parseExpression()
	if !p.MatchKeyword("ASC") {
		p.MatchKeyword("DESC")
	}
	if p.Current().Kind != tokenRightParen {
		p.reportSyntaxError("expected ')' to close WITHIN GROUP")
		return sorted
	}
	p.Advance()
	return sorted
}

// parseFunctionOrderByClause consumes an ORDER BY inside a function call.
func (p *parser) parseFunctionOrderByClause() {
	if !p.MatchKeyword(keywordORDER) {
		return
	}
	p.MatchKeyword(keywordBY)
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		if p.IsKeyword(keywordSEPARATOR) {
			break
		}
		p.parseExpression()
		p.MatchKeyword(keywordASC)
		p.MatchKeyword(keywordDESC)
		p.MatchKeyword(keywordNULLS)
		p.MatchKeyword(keywordFIRST)
		p.MatchKeyword(keywordLAST)
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
}

// markParametersAsFunctionArguments retags the parameter references registered since
// referenceCountBefore as function argument contexts, stamping the enclosing function
// name and the zero-based argument ordinal so the analyser can type each placeholder from
// the matched function signature.
//
// The references an argument registered are those between its boundary and the one
// before, so a placeholder numbered out of order, as in LEFT(?2, ?1), or a named
// parameter used again keeps the slot it is written in. Only placeholders still in
// ParameterContextUnknown are retagged, which preserves any tag already set by an inner
// call (a placeholder nested in a sub-call keeps its inner-call name and ordinal rather
// than being clobbered by the outer call). A reference past the last boundary belongs to
// the last slot.
//
// Takes referenceCountBefore (int) which is the number of parameter references before the
// function arguments were parsed.
// Takes qualifiedName (string) which is the lower-cased, optionally schema-qualified
// enclosing function name.
// Takes argumentBoundaries ([]int) which holds the number of parameter references after
// each top-level argument.
func (p *parser) markParametersAsFunctionArguments(
	referenceCountBefore int,
	qualifiedName string,
	argumentBoundaries []int,
) {
	ordinal := 0
	for index := max(referenceCountBefore, 0); index < len(p.parameterReferences); index++ {
		for ordinal < len(argumentBoundaries)-1 && index >= argumentBoundaries[ordinal] {
			ordinal++
		}
		if p.parameterReferences[index].Context != sqlmodel.ParameterContextUnknown {
			continue
		}
		p.parameterReferences[index].Context = sqlmodel.ParameterContextFunctionArgument
		p.parameterReferences[index].EnclosingFunctionName = qualifiedName
		p.parameterReferences[index].ArgumentOrdinal = ordinal
	}
}

// parseFunctionSuffix wraps a function call in an OVER window if present.
//
// Takes result (*ast.FunctionCallExpression) which is the inner function call.
//
// Returns ast.Expression which is either result or a WindowFunctionExpression wrapping
// it.
func (p *parser) parseFunctionSuffix(result *ast.FunctionCallExpression) ast.Expression {
	if p.IsKeyword("OVER") {
		return p.parseWindowSuffix(result)
	}

	return result
}

// parseWindowSuffix parses the OVER (...) clause of a window function.
//
// Takes innerFunction (*ast.FunctionCallExpression) which is the function the window
// applies to.
//
// Returns ast.Expression which is the wrapped window expression.
func (p *parser) parseWindowSuffix(innerFunction *ast.FunctionCallExpression) ast.Expression {
	p.Advance()

	if p.Current().Kind == tokenIdentifier && p.Peek().Kind != tokenLeftParen &&
		!p.IsKeyword("PARTITION", keywordORDER, "ROWS", "RANGE") {
		p.Advance()
		return &ast.WindowFunctionExpression{Function: innerFunction}
	}

	if p.Current().Kind != tokenLeftParen {
		return &ast.WindowFunctionExpression{Function: innerFunction}
	}
	p.Advance()

	p.parseWindowSpec()

	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}

	return &ast.WindowFunctionExpression{Function: innerFunction}
}

// parseWindowSpec parses PARTITION BY, ORDER BY, and frame clauses.
func (p *parser) parseWindowSpec() {
	if p.Current().Kind == tokenIdentifier &&
		!p.IsKeyword("PARTITION", keywordORDER, "ROWS", "RANGE") &&
		p.Current().Kind != tokenRightParen {
		p.Advance()
	}

	if p.MatchKeyword("PARTITION") {
		p.MatchKeyword(keywordBY)
		p.parseExpression()
		for p.Current().Kind == tokenComma {
			p.Advance()
			p.parseExpression()
		}
	}

	if p.MatchKeyword(keywordORDER) {
		p.MatchKeyword(keywordBY)
		p.parseExpression()
		p.MatchKeyword(keywordASC)
		p.MatchKeyword(keywordDESC)
		p.MatchKeyword(keywordNULLS)
		p.MatchKeyword(keywordFIRST)
		p.MatchKeyword(keywordLAST)
		for p.Current().Kind == tokenComma {
			p.Advance()
			p.parseExpression()
			p.MatchKeyword(keywordASC)
			p.MatchKeyword(keywordDESC)
			p.MatchKeyword(keywordNULLS)
			p.MatchKeyword(keywordFIRST)
			p.MatchKeyword(keywordLAST)
		}
	}

	if p.IsKeyword("ROWS", "RANGE") {
		p.skipWindowFrame()
	}
}

// skipWindowFrame consumes a ROWS or RANGE window frame definition.
func (p *parser) skipWindowFrame() {
	p.Advance()
	if p.MatchKeyword("BETWEEN") {
		p.skipFrameBound()
		p.MatchKeyword(keywordAND)
		p.skipFrameBound()
	} else {
		p.skipFrameBound()
	}
}

// skipFrameBound consumes a single window frame bound expression.
func (p *parser) skipFrameBound() {
	if p.MatchKeyword(keywordCURRENT) {
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

// specialFunctionHandler returns the special parser for a function name.
//
// Takes loweredName (string) which is the lower-cased function name.
//
// Returns specialFunctionParser which is the matched parser, or nil when the function
// uses the default argument grammar.
func (p *parser) specialFunctionHandler(loweredName string) specialFunctionParser {
	switch loweredName {
	case "trim":
		return p.parseTrimFunction
	case "extract":
		return p.parseExtractFunction
	case "group_concat":
		return p.parseGroupConcatFunction
	case "convert":
		return p.parseConvertFunction
	case "json_value":
		return p.parseJSONValueFunction
	case "position":
		return p.parsePositionFunction
	case "substring", "substr", "mid":
		return p.parseSubstringFunction
	case "char", "weight_string":
		return p.parseConversionClauseFunction
	case "nextval", "lastval", "setval":
		if p.sequences {
			return p.parseSequenceFunction
		}
		return nil
	default:
		return nil
	}
}

// parseSequenceFunction parses NEXTVAL, LASTVAL or SETVAL, reading the first argument as
// a sequence name optionally qualified by its database. SETVAL requires numeric literals
// for its remaining arguments and rejects parameters there.
//
// Takes loweredName (string) which is the lower-cased function name.
// Takes schema (string) which is the optional schema qualifier.
//
// Returns ast.Expression which is the parsed call.
func (p *parser) parseSequenceFunction(loweredName string, schema string) ast.Expression {
	referenceCountBefore := len(p.parameterReferences)
	if _, _, err := p.parseSchemaQualifiedName(); err != nil {
		p.recordSyntaxError(err)
	}
	arguments := []ast.Expression{&ast.LiteralExpression{TypeName: typeNameText}}
	boundaries := []int{len(p.parameterReferences)}
	for p.Current().Kind == tokenComma {
		p.Advance()
		if isParameterToken(p.Current().Kind) {
			p.reportSyntaxError("SETVAL takes its value as a number written in the statement, not a parameter")
		}
		arguments = append(arguments, p.parseExpression())
		boundaries = append(boundaries, len(p.parameterReferences))
	}
	return p.finishSpecialCall(loweredName, schema, arguments, boundaries, referenceCountBefore)
}

// parsePositionFunction parses POSITION(substring IN string), reading the substring
// without its IN so the keyword is not taken for an IN predicate.
//
// Takes loweredName (string) which is the lower-cased function name.
// Takes schema (string) which is the optional schema qualifier.
//
// Returns ast.Expression which is the parsed call.
func (p *parser) parsePositionFunction(loweredName string, schema string) ast.Expression {
	referenceCountBefore := len(p.parameterReferences)
	substring := p.parseBitwiseExpression()
	substringEnd := len(p.parameterReferences)
	if !p.MatchKeyword(keywordIN) {
		p.requireStrictExpression("expected IN in POSITION")
	}
	searched := p.parseExpression()
	return p.finishSpecialCall(loweredName, schema, []ast.Expression{substring, searched},
		[]int{substringEnd, len(p.parameterReferences)}, referenceCountBefore)
}

// parseSubstringFunction parses SUBSTRING, SUBSTR or MID, written with commas or as
// SUBSTRING(string FROM position [FOR length]).
//
// Takes loweredName (string) which is the lower-cased function name.
// Takes schema (string) which is the optional schema qualifier.
//
// Returns ast.Expression which is the parsed call.
func (p *parser) parseSubstringFunction(loweredName string, schema string) ast.Expression {
	referenceCountBefore := len(p.parameterReferences)
	var arguments []ast.Expression
	var boundaries []int
	for {
		arguments = append(arguments, p.parseExpression())
		boundaries = append(boundaries, len(p.parameterReferences))
		if p.Current().Kind == tokenComma {
			p.Advance()
			continue
		}
		if !p.MatchKeyword(keywordFROM) && !p.MatchKeyword(keywordFOR) {
			break
		}
	}
	return p.finishSpecialCall(loweredName, schema, arguments, boundaries, referenceCountBefore)
}

// parseConversionClauseFunction parses a call whose arguments may be followed by a
// conversion clause: CHAR(code, ... USING charset), whose result is then text rather than
// a binary string, or WEIGHT_STRING(string AS CHAR(n)).
//
// Takes loweredName (string) which is the lower-cased function name.
// Takes schema (string) which is the optional schema qualifier.
//
// Returns ast.Expression which is the parsed call, cast to text when a character set is
// named.
func (p *parser) parseConversionClauseFunction(loweredName string, schema string) ast.Expression {
	referenceCountBefore := len(p.parameterReferences)
	arguments, boundaries := p.parseFunctionArguments()
	characterSet := false
	switch {
	case p.MatchKeyword(keywordUSING):
		characterSet = true
		if p.Current().Kind == tokenIdentifier || p.Current().Kind == tokenString {
			p.Advance()
		}
	case p.MatchKeyword(keywordAS):
		p.parseCastTargetTypeName()
	}
	call := p.finishSpecialCall(loweredName, schema, arguments, boundaries, referenceCountBefore)
	if characterSet {
		return &ast.CastExpression{TypeName: typeNameText, Inner: call}
	}
	return call
}

// finishSpecialCall closes a call a special parser read the arguments of, marks the
// parameters among them as its arguments and builds the call.
//
// Takes loweredName (string) which is the lower-cased function name.
// Takes schema (string) which is the optional schema qualifier.
// Takes arguments ([]ast.Expression) which are the call's arguments.
// Takes boundaries ([]int) which hold, per argument, the number of parameter references
// once it was read.
// Takes referenceCountBefore (int) which is the number of parameter references before the
// arguments.
//
// Returns ast.Expression which is the call.
func (p *parser) finishSpecialCall(loweredName string, schema string, arguments []ast.Expression, boundaries []int,
	referenceCountBefore int) ast.Expression {
	p.closeParenthesis()
	p.markParametersAsFunctionArguments(referenceCountBefore, qualifiedFunctionName(loweredName, schema), boundaries)
	return &ast.FunctionCallExpression{
		FunctionName:     loweredName,
		Schema:           schema,
		Arguments:        arguments,
		FilterExpression: nil,
		Span:             ast.Span{},
	}
}

// parseJSONValueFunction parses JSON_VALUE(json, path [RETURNING type] [on-empty]
// [on-error]), where each handler is NULL, ERROR or DEFAULT value followed by ON EMPTY or
// ON ERROR. A RETURNING clause types the result, so the call is wrapped in a cast to that
// type.
//
// Takes loweredName (string) which is the lower-cased function name.
// Takes schema (string) which is the optional schema qualifier.
//
// Returns ast.Expression which is the call, cast to its RETURNING type when given.
func (p *parser) parseJSONValueFunction(loweredName string, schema string) ast.Expression {
	referenceCountBefore := len(p.parameterReferences)
	arguments, argumentBoundaries := p.parseFunctionArguments()

	returningType := ""
	if p.IsKeyword(keywordRETURNING) && p.jsonValueReturningUnavailable {
		p.reportSyntaxError("JSON_VALUE takes no RETURNING clause in this dialect; wrap the call in CAST instead")
	}
	if p.MatchKeyword(keywordRETURNING) {
		returningType = p.parseCastTargetTypeName()
	}
	p.skipJSONValueHandlers()

	if p.Current().Kind != tokenRightParen {
		p.reportSyntaxError("expected ')' to close JSON_VALUE")
	} else {
		p.Advance()
	}

	p.markParametersAsFunctionArguments(
		referenceCountBefore, qualifiedFunctionName(loweredName, schema), argumentBoundaries)

	call := &ast.FunctionCallExpression{
		FunctionName:     loweredName,
		Schema:           schema,
		Arguments:        arguments,
		FilterExpression: nil,
		Span:             ast.Span{},
	}
	if returningType == "" {
		return call
	}
	return &ast.CastExpression{TypeName: strings.ToLower(returningType), Inner: call}
}

// skipJSONValueHandlers consumes the ON EMPTY and ON ERROR handlers of JSON_VALUE: NULL,
// ERROR or DEFAULT value, each followed by ON EMPTY or ON ERROR.
func (p *parser) skipJSONValueHandlers() {
	for {
		switch {
		case p.MatchKeyword(keywordNULL), p.MatchKeyword("ERROR"):
		case p.MatchKeyword(keywordDEFAULT):
			p.parsePrimaryExpression()
		default:
			return
		}
		if p.MatchKeyword(keywordON) && !p.MatchKeyword("EMPTY") {
			p.MatchKeyword("ERROR")
		}
	}
}

// parseTrimFunction parses TRIM with its LEADING/TRAILING/BOTH grammar.
//
// Takes loweredName (string) which is the lower-cased function name.
// Takes schema (string) which is the optional schema qualifier.
//
// Returns ast.Expression which is the parsed TRIM call.
func (p *parser) parseTrimFunction(loweredName string, schema string) ast.Expression {
	referenceCountBefore := len(p.parameterReferences)

	if p.IsKeyword("LEADING", "TRAILING", "BOTH") {
		p.Advance()
		if !p.IsKeyword(keywordFROM) {
			p.parseExpression()
		}
		p.MatchKeyword(keywordFROM)
	}

	arguments, argumentBoundaries := p.parseFunctionArguments()

	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}

	p.markParametersAsFunctionArguments(
		referenceCountBefore, qualifiedFunctionName(loweredName, schema), argumentBoundaries)

	return &ast.FunctionCallExpression{
		FunctionName:     loweredName,
		Schema:           schema,
		Arguments:        arguments,
		FilterExpression: nil,
		Span:             ast.Span{},
	}
}

// parseExtractFunction parses EXTRACT(field FROM expression).
//
// Takes loweredName (string) which is the lower-cased function name.
// Takes schema (string) which is the optional schema qualifier.
//
// Returns ast.Expression which is the parsed EXTRACT call.
func (p *parser) parseExtractFunction(loweredName string, schema string) ast.Expression {
	if p.Current().Kind == tokenIdentifier {
		p.Advance()
	}
	p.MatchKeyword(keywordFROM)

	referenceCountBefore := len(p.parameterReferences)
	arguments, argumentBoundaries := p.parseFunctionArguments()

	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}

	p.markParametersAsFunctionArguments(
		referenceCountBefore, qualifiedFunctionName(loweredName, schema), argumentBoundaries)

	return &ast.FunctionCallExpression{
		FunctionName:     loweredName,
		Schema:           schema,
		Arguments:        arguments,
		FilterExpression: nil,
		Span:             ast.Span{},
	}
}

// parseGroupConcatFunction parses GROUP_CONCAT with its SEPARATOR clause.
//
// Takes loweredName (string) which is the lower-cased function name.
// Takes schema (string) which is the optional schema qualifier.
//
// Returns ast.Expression which is the parsed GROUP_CONCAT call.
func (p *parser) parseGroupConcatFunction(loweredName string, schema string) ast.Expression {
	p.MatchKeyword("DISTINCT")

	referenceCountBefore := len(p.parameterReferences)
	var arguments []ast.Expression
	var argumentBoundaries []int
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		if p.IsKeyword(keywordORDER, keywordSEPARATOR) {
			break
		}
		arguments = append(arguments, p.parseExpression())
		argumentBoundaries = append(argumentBoundaries, len(p.parameterReferences))
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}

	p.parseFunctionOrderByClause()

	if p.MatchKeyword(keywordSEPARATOR) {
		p.parseExpression()
	}

	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}

	p.markParametersAsFunctionArguments(
		referenceCountBefore, qualifiedFunctionName(loweredName, schema), argumentBoundaries)

	result := &ast.FunctionCallExpression{
		FunctionName:     loweredName,
		Schema:           schema,
		Arguments:        arguments,
		FilterExpression: nil,
		Span:             ast.Span{},
	}

	return p.parseFunctionSuffix(result)
}

// parseConvertFunction parses CONVERT(expr, type) or CONVERT(expr USING).
//
// Takes loweredName (string) which is the lower-cased function name.
// Takes schema (string) which is the optional schema qualifier.
//
// Returns ast.Expression which is the parsed CONVERT call.
func (p *parser) parseConvertFunction(loweredName string, schema string) ast.Expression {
	referenceCountBefore := len(p.parameterReferences)
	inner := p.parseExpression()

	if p.MatchKeyword(keywordUSING) {
		if p.Current().Kind == tokenIdentifier || p.Current().Kind == tokenString {
			p.Advance()
		}
		p.closeCastParenthesis()
		p.annotateCastParameter(typeNameText, referenceCountBefore)
		return &ast.CastExpression{TypeName: typeNameText, Inner: inner}
	}

	if p.MatchKeyword(keywordAS) || p.Current().Kind == tokenComma {
		if p.Current().Kind == tokenComma {
			p.Advance()
		}
		typeName := p.parseCastTargetTypeName()
		p.skipCastTypeAttributes()
		p.closeCastParenthesis()

		p.annotateCastParameter(typeName, referenceCountBefore)

		return &ast.CastExpression{
			TypeName: strings.ToLower(typeName),
			Inner:    inner,
		}
	}

	p.closeCastParenthesis()

	return &ast.FunctionCallExpression{
		FunctionName:     loweredName,
		Schema:           schema,
		Arguments:        []ast.Expression{inner},
		FilterExpression: nil,
		Span:             ast.Span{},
	}
}

// skipCastTypeAttributes consumes what may follow a CAST or CONVERT target type: a
// CHARACTER SET, CHARSET or COLLATE clause, and the ARRAY of a multi-valued index cast.
func (p *parser) skipCastTypeAttributes() {
	for {
		switch {
		case p.isCharacterSetStart(), p.IsKeyword(keywordCOLLATE):
			if err := p.parseCharacterSetOrCollation(); err != nil {
				return
			}
		case p.MatchKeyword("ARRAY"):
		default:
			return
		}
	}
}

// closeCastParenthesis consumes the ')' that ends a CAST or CONVERT, recording a syntax
// error when anything else is there.
func (p *parser) closeCastParenthesis() {
	if p.Current().Kind != tokenRightParen {
		p.reportSyntaxError("expected ')' to close the conversion")
		return
	}
	p.Advance()
}

// parseCastFunctionExpression parses CAST(expr AS type).
//
// Returns ast.Expression which is the parsed CAST expression, or an unknown expression
// when the opening paren is missing.
func (p *parser) parseCastFunctionExpression() ast.Expression {
	p.Advance()
	if p.Current().Kind != tokenLeftParen {
		return &ast.UnknownExpression{}
	}
	p.Advance()

	referenceCountBefore := len(p.parameterReferences)
	inner := p.parseExpression()

	p.MatchKeyword(keywordAS)

	typeName := p.parseCastTargetTypeName()
	p.skipCastTypeAttributes()
	p.closeCastParenthesis()

	p.annotateCastParameter(typeName, referenceCountBefore)

	return &ast.CastExpression{
		TypeName: strings.ToLower(typeName),
		Inner:    inner,
	}
}

// parseConvertExpression dispatches to parseConvertFunction.
//
// Returns ast.Expression which is the parsed CONVERT call.
func (p *parser) parseConvertExpression() ast.Expression {
	p.Advance()
	if p.Current().Kind != tokenLeftParen {
		return &ast.UnknownExpression{}
	}
	p.Advance()
	return p.parseConvertFunction("convert", "")
}

// parseCastTargetTypeName parses the target type of a CAST, a CONVERT or the RETURNING
// clause of JSON_VALUE, naming a target that is no column type, SIGNED or UNSIGNED with
// an optional INTEGER or INT, by the column type the conversion yields.
//
// The numeric modifiers of the target, such as the precision and scale of DECIMAL(10, 2)
// or the length of CHAR(20), stay in the name as appendTypeModifiers writes them.
//
// Returns string which is the assembled type name.
func (p *parser) parseCastTargetTypeName() string {
	var typeName strings.Builder
	typeName.WriteString(castTargetColumnType(p.parseTypeNameWords()))
	p.refuseFloatPrecision(typeName.String())
	if err := p.appendTypeModifiers(&typeName); err != nil {
		p.recordSyntaxError(fmt.Errorf("parsing CAST type modifiers: %w", err))
	}
	return typeName.String()
}

// refuseFloatPrecision records a syntax error at the precision of a FLOAT conversion
// target whose modifier list, at the current token, gives more than maxDoublePrecision
// bits, which MySQL refuses. It consumes nothing.
//
// Takes typeName (string) which is the target's name, read before its modifiers.
func (p *parser) refuseFloatPrecision(typeName string) {
	modifiers, numeric := p.typeModifiersAhead(0)
	if numeric && floatPrecisionRefused(strings.ToLower(typeName), modifiers) {
		p.recordSyntaxError(dialectkit.NewParseError(p.PeekAt(1), floatPrecisionRefusal))
	}
}

// parseTypeNameWords parses a type name of one or more words, stopping before a clause
// keyword, any of stops or anything that is not a word, such as the parenthesis that
// opens the type's modifiers.
//
// Takes stops (...string) which are further keywords that end the type name, such as PATH
// in a JSON_TABLE column.
//
// Returns string which is the words joined by single spaces, or empty when the current
// token is not a word.
func (p *parser) parseTypeNameWords(stops ...string) string {
	typeName := ""
	if p.Current().Kind == tokenIdentifier {
		typeName = p.Advance().Value
		for p.Current().Kind == tokenIdentifier &&
			!p.IsKeyword(keywordFROM, keywordWHERE, keywordGROUP, keywordHAVING, keywordORDER, keywordLIMIT) &&
			!p.IsKeyword(stops...) {
			typeName += " " + p.Advance().Value
		}
	}
	return typeName
}

// annotateCastParameter tags the parameter reference the converted expression registered
// with a cast type, when it registered exactly one, whatever its number, so a named
// parameter used again or a placeholder numbered out of order is typed too.
//
// Takes typeName (string) which is the raw type name parsed from the CAST.
// Takes referenceCountBefore (int) which is the number of parameter references recorded
// before the inner expression was parsed.
func (p *parser) annotateCastParameter(typeName string, referenceCountBefore int) {
	if typeName == "" || len(p.parameterReferences) != referenceCountBefore+1 {
		return
	}
	lastIndex := len(p.parameterReferences) - 1
	if lastIndex < 0 {
		return
	}
	p.parameterReferences[lastIndex].Context = sqlmodel.ParameterContextCast
	p.parameterReferences[lastIndex].CastType = new(p.types.normaliseWritten(typeName))
}

// parseCoalesceExpression parses a COALESCE(...) call with annotation.
//
// Annotates parameters with the first column reference seen so the expected column types
// can be inferred later.
//
// Returns ast.Expression which is the parsed COALESCE expression.
func (p *parser) parseCoalesceExpression() ast.Expression {
	p.Advance()
	if p.Current().Kind != tokenLeftParen {
		return &ast.UnknownExpression{}
	}
	p.Advance()

	referenceCountBefore := len(p.parameterReferences)
	arguments, firstColumnReference := p.parseCoalesceArguments()

	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}

	p.annotateCoalesceParameters(firstColumnReference, referenceCountBefore)

	return &ast.CoalesceExpression{
		Arguments: arguments,
	}
}

// parseCoalesceArguments parses COALESCE arguments and tracks the first column reference
// encountered.
//
// Returns []ast.Expression which is the parsed argument list.
// Returns *ast.ColumnReference which is the first column seen, or nil when no argument is
// a bare column.
func (p *parser) parseCoalesceArguments() ([]ast.Expression, *ast.ColumnReference) {
	var arguments []ast.Expression
	var firstColumnReference *ast.ColumnReference
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		argument := p.parseExpression()
		arguments = append(arguments, argument)
		if columnReferenceExpression, ok := argument.(*ast.ColumnRefExpression); ok && firstColumnReference == nil {
			firstColumnReference = &ast.ColumnReference{
				TableAlias: columnReferenceExpression.TableAlias,
				ColumnName: columnReferenceExpression.ColumnName,
			}
		}
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	return arguments, firstColumnReference
}

// annotateCoalesceParameters tags unannotated COALESCE parameters with the column
// reference of the first non-parameter argument.
//
// Takes firstColumnReference (*ast.ColumnReference) which is the reference to apply to
// unannotated parameters.
// Takes referenceCountBefore (int) which is the parameterReferences length before
// COALESCE arguments were parsed.
func (p *parser) annotateCoalesceParameters(firstColumnReference *ast.ColumnReference, referenceCountBefore int) {
	if firstColumnReference == nil {
		return
	}
	for index := referenceCountBefore; index < len(p.parameterReferences); index++ {
		if p.parameterReferences[index].Context == sqlmodel.ParameterContextUnknown &&
			p.parameterReferences[index].ColumnReference == nil &&
			p.parameterReferences[index].CastType == nil {
			p.parameterReferences[index].ColumnReference = firstColumnReference
			p.parameterReferences[index].Context = sqlmodel.ParameterContextComparison
		}
	}
}

// parseCaseExpression parses a CASE WHEN ... END expression.
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
		p.MatchKeyword("THEN")
		result := p.parseExpression()
		branches = append(branches, ast.CaseWhenBranch{Condition: condition, Result: result})
	}

	expression := &ast.CaseWhenExpression{Branches: branches, ElseResult: nil}

	if p.MatchKeyword("ELSE") {
		expression.ElseResult = p.parseExpression()
	}

	p.MatchKeyword("END")
	return expression
}

// qualifiedFunctionName joins the optional schema and the lower-cased function name into
// the form the analyser looks the function up by (schema-qualified when a schema is
// present, bare otherwise). The schema is lower-cased so the whole name is lower-case,
// matching the EnclosingFunctionName contract the analyser relies on for its schema
// split.
//
// Takes loweredName (string) which is the lower-cased function name.
// Takes schema (string) which is the optional schema qualifier.
//
// Returns string which is the lower-cased qualified function name.
func qualifiedFunctionName(loweredName string, schema string) string {
	if schema == "" {
		return loweredName
	}
	return strings.ToLower(schema) + "." + loweredName
}

// castTargetColumnType names a conversion target by the column type the conversion
// yields: SIGNED and UNSIGNED, with an optional INTEGER or INT, name no column type.
//
// Takes typeName (string) which is the target as written.
//
// Returns string which is the column type, or typeName when it is one already.
func castTargetColumnType(typeName string) string {
	if columnType, conversion := castTargetColumnTypes[strings.ToUpper(typeName)]; conversion {
		return columnType
	}
	return typeName
}

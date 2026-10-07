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
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

// parseFunctionCall parses a function invocation after its name is consumed.
//
// Takes name (string) which is the original-case function name.
// Takes schema (string) which is the optional schema qualifier.
//
// Returns ast.Expression which is the resulting function call expression, optionally
// wrapped in a window or filter suffix.
func (p *parser) parseFunctionCall(name string, schema string) ast.Expression {
	loweredName := strings.ToLower(name)
	if loweredName == "struct_extract" && schema == "" {
		if field := p.tryParseStructExtract(); field != nil {
			return field
		}
	}
	p.Advance()

	if p.Current().Kind == tokenStar || p.Current().Kind == tokenRightParen {
		return p.parseFunctionCallNoArgs(loweredName, schema)
	}

	return p.parseFunctionCallWithArgs(loweredName, schema)
}

// tryParseStructExtract parses struct_extract(struct, 'field'), whose field is named by a
// string literal, as the field access it is, so it types as the field. A call naming its
// field any other way is left to the ordinary function call parser; the cursor is on the
// call's '('.
//
// Returns ast.Expression which is the field access, or nil when the call is not of this
// form, with the cursor left where it was.
func (p *parser) tryParseStructExtract() ast.Expression {
	closing := p.closingParenthesis(p.Position)
	if closing < p.Position+minimumStructExtractTokens || p.Tokens[closing-1].Kind != tokenString ||
		p.Tokens[closing-2].Kind != tokenComma {
		return nil
	}
	p.Advance()
	structExpression := p.parseExpression()
	if p.Position != closing-2 {
		p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected ',' before the struct field name"))
		return &ast.UnknownExpression{}
	}
	p.Advance()
	field := p.Advance().Value
	p.Advance()
	return &ast.StructFieldAccessExpression{Struct: structExpression, FieldName: field}
}

// closingParenthesis finds the ')' that closes the '(' at an index.
//
// Takes open (int) which is the index of the '('.
//
// Returns int which is the index of the matching ')', or -1 when it is not closed.
func (p *parser) closingParenthesis(open int) int {
	depth := 0
	for index := open; index < len(p.Tokens); index++ {
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

// parseFunctionCallNoArgs handles a function call with no arguments or star.
//
// Takes loweredName (string) which is the lowercase function name.
// Takes schema (string) which is the optional schema qualifier.
//
// Returns ast.Expression which is the resulting function call expression with any
// trailing suffix applied.
func (p *parser) parseFunctionCallNoArgs(loweredName string, schema string) ast.Expression {
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

// parseFunctionCallWithArgs parses a function call that has arguments.
//
// Takes loweredName (string) which is the lowercase function name.
// Takes schema (string) which is the optional schema qualifier.
//
// Returns ast.Expression which is the resulting function call expression with any
// trailing suffix applied.
func (p *parser) parseFunctionCallWithArgs(loweredName string, schema string) ast.Expression {
	p.MatchKeyword("DISTINCT")
	p.MatchKeyword(keywordALL)

	parameterCountBefore := p.parameterCount
	referenceCountBefore := len(p.parameterReferences)
	arguments := p.parseFunctionArguments()
	p.parseFunctionOrderByClause()

	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}

	p.markParametersAsFunctionArguments(parameterCountBefore)
	p.recordArgumentNames(qualifiedCallName(loweredName, schema), referenceCountBefore, arguments)

	if packed := packedStructCall(loweredName, schema, arguments); packed != nil {
		return packed
	}
	arguments = insertedStructArguments(loweredName, schema, arguments)
	bindLambdaParameters(loweredName, schema, arguments)

	result := &ast.FunctionCallExpression{
		FunctionName:     loweredName,
		Schema:           schema,
		Arguments:        arguments,
		FilterExpression: nil,
		Span:             ast.Span{},
	}

	return p.parseFunctionSuffix(result)
}

// parseFunctionArguments parses a comma-separated list of function arguments.
//
// Returns []ast.Expression which contains the parsed arguments including any lambda
// expressions.
func (p *parser) parseFunctionArguments() []ast.Expression {
	var arguments []ast.Expression
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		if p.IsKeyword(keywordORDER, keywordLIMIT) {
			break
		}
		argumentName := p.parseArgumentName()
		var argument ast.Expression
		if lambda := p.tryParseLambda(); lambda != nil {
			argument = lambda
		} else {
			argument = p.parseExpression()
		}
		if argumentName != "" {
			argument = &ast.NamedArgumentExpression{Name: argumentName, Value: argument}
		}
		arguments = append(arguments, argument)
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	return arguments
}

// parseArgumentName consumes the name of a named argument, such as the b in
// add_default(1, b := 2) or struct_pack(a => 1), leaving the cursor on its value.
//
// Returns string which is the argument's name, or empty for a positional argument.
func (p *parser) parseArgumentName() string {
	if p.Current().Kind != tokenIdentifier || p.Peek().Kind != tokenOperator {
		return ""
	}
	if operator := p.Peek().Value; operator != ":=" && operator != "=>" {
		return ""
	}
	name := p.Advance().Value
	p.Advance()
	return name
}

// recordArgumentNames records the name each parameter a call passes by name is passed as:
// a parameter registered while parsing the call's arguments whose nearest enclosing call
// is this one, in an argument written name := value or name => value.
//
// Takes qualifiedName (string) which is the call's qualified name.
// Takes referenceCountBefore (int) which is the count of parameter references before the
// call's arguments were parsed.
// Takes arguments ([]ast.Expression) which are the call's arguments.
func (p *parser) recordArgumentNames(qualifiedName string, referenceCountBefore int, arguments []ast.Expression) {
	names := make(map[int]string, len(arguments))
	for ordinal, argument := range arguments {
		if named, isNamed := argument.(*ast.NamedArgumentExpression); isNamed {
			names[ordinal] = named.Name
		}
	}
	if len(names) == 0 {
		return
	}
	for index := referenceCountBefore; index < len(p.parameterReferences); index++ {
		reference := p.parameterReferences[index]
		name, named := names[reference.ArgumentOrdinal]
		if !named || reference.EnclosingFunctionName != qualifiedName {
			continue
		}
		p.recordArgumentName(reference.Number, name)
	}
}

// recordArgumentName records that a call passes a parameter by name.
//
// Takes number (int) which is the parameter's number.
// Takes name (string) which is the name it is passed as.
func (p *parser) recordArgumentName(number int, name string) {
	if p.argumentNames == nil {
		p.argumentNames = make(map[int]string)
	}
	p.argumentNames[number] = name
}

// adoptArgumentNames records the parameters a child parser saw passed by name, so a
// nested query's named arguments reach the statement's analysis.
//
// Takes child (*parser) which is the child parser.
func (p *parser) adoptArgumentNames(child *parser) {
	for number, name := range child.argumentNames {
		p.recordArgumentName(number, name)
	}
}

// tryParseLambda parses a single or multi-parameter lambda when present, written with an
// arrow, as in x -> x + 1 or (x, y) -> x + y, or with the LAMBDA keyword, as in lambda x,
// y: x + y.
//
// Returns ast.Expression which is the parsed lambda, or nil when the current tokens do
// not form a lambda.
func (p *parser) tryParseLambda() ast.Expression {
	if p.IsKeyword(keywordLAMBDA) && p.Peek().Kind == tokenIdentifier {
		return p.parseKeywordLambda()
	}
	if p.Current().Kind == tokenIdentifier && p.Peek().Kind == tokenArrow {
		parameterName := p.Advance().Value
		p.Advance()
		body := p.parseFunctionArgumentExpression()
		return &ast.LambdaExpression{
			Parameters: []string{parameterName},
			Body:       body,
			Bindings:   nil,
		}
	}

	if p.Current().Kind == tokenLeftParen && p.looksLikeMultiParamLambda() {
		return p.parseMultiParamLambda()
	}

	return nil
}

// looksLikeMultiParamLambda peeks for a parenthesised lambda parameter list.
//
// Returns bool which is true when the upcoming tokens form (a, b, ...) ->.
func (p *parser) looksLikeMultiParamLambda() bool {
	savedPosition := p.Position
	defer func() { p.Position = savedPosition }()

	p.Advance()
	for !p.AtEnd() {
		if p.Current().Kind != tokenIdentifier {
			return false
		}
		p.Advance()
		if p.Current().Kind == tokenRightParen {
			p.Advance()
			return p.Current().Kind == tokenArrow
		}
		if p.Current().Kind != tokenComma {
			return false
		}
		p.Advance()
	}
	return false
}

// parseMultiParamLambda parses a multi-parameter lambda expression.
//
// Returns ast.Expression which is the parsed lambda expression.
func (p *parser) parseMultiParamLambda() ast.Expression {
	p.Advance()
	var parameters []string
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		if p.Current().Kind == tokenIdentifier {
			parameters = append(parameters, p.Advance().Value)
		}
		if p.Current().Kind == tokenComma {
			p.Advance()
		}
	}
	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}
	p.Advance()
	body := p.parseFunctionArgumentExpression()
	return &ast.LambdaExpression{
		Parameters: parameters,
		Body:       body,
		Bindings:   nil,
	}
}

// parseKeywordLambda parses a lambda written with the LAMBDA keyword: its parameters,
// separated by commas, a colon, and its body.
//
// Returns ast.Expression which is the parsed lambda expression.
func (p *parser) parseKeywordLambda() ast.Expression {
	p.Advance()
	var parameters []string
	for p.Current().Kind == tokenIdentifier {
		parameters = append(parameters, p.Advance().Value)
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	if p.Current().Kind != tokenOperator || p.Current().Value != operatorColon {
		p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected ':' after the lambda's parameters"))
		return &ast.UnknownExpression{}
	}
	p.Advance()
	return &ast.LambdaExpression{Parameters: parameters, Body: p.parseFunctionArgumentExpression(), Bindings: nil}
}

// parseFunctionArgumentExpression parses one expression used as a function argument.
//
// Returns ast.Expression which is the parsed argument expression.
func (p *parser) parseFunctionArgumentExpression() ast.Expression {
	return p.parseExpression()
}

// parseFunctionOrderByClause consumes a trailing ORDER BY clause in arguments.
func (p *parser) parseFunctionOrderByClause() {
	if !p.MatchKeyword(keywordORDER) {
		return
	}
	p.requireKeywordOrRecord(keywordBY)
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
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

// markParametersAsFunctionArguments tags new bind parameters as function arguments.
//
// Takes parameterCountBefore (int) which is the parameter count recorded before the
// function arguments were parsed.
func (p *parser) markParametersAsFunctionArguments(parameterCountBefore int) {
	for index := range p.parameterReferences {
		if p.parameterReferences[index].Number > parameterCountBefore &&
			p.parameterReferences[index].Context == sqlmodel.ParameterContextUnknown {
			p.parameterReferences[index].Context = sqlmodel.ParameterContextFunctionArgument
		}
	}
}

// parseWithinGroupClause parses a WITHIN GROUP (ORDER BY ...) clause.
func (p *parser) parseWithinGroupClause() {
	p.MatchKeyword(keywordGROUP)
	if p.Current().Kind != tokenLeftParen {
		return
	}
	p.Advance()
	if p.MatchKeyword(keywordORDER) {
		p.requireKeywordOrRecord(keywordBY)
		for !p.AtEnd() && p.Current().Kind != tokenRightParen {
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
	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}
}

// parseFilterClause parses a FILTER (WHERE ...) aggregate suffix.
//
// Takes result (*ast.FunctionCallExpression) which receives the parsed filter expression.
func (p *parser) parseFilterClause(result *ast.FunctionCallExpression) {
	if p.Current().Kind != tokenLeftParen {
		return
	}
	p.Advance()
	p.MatchKeyword(keywordWHERE)
	result.FilterExpression = p.parseExpression()
	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}
}

// parseFunctionSuffix parses WITHIN GROUP, FILTER and OVER suffixes.
//
// Takes result (*ast.FunctionCallExpression) which is the function call to decorate with
// suffix clauses.
//
// Returns ast.Expression which is the original call or a window function wrapper.
func (p *parser) parseFunctionSuffix(result *ast.FunctionCallExpression) ast.Expression {
	if p.MatchKeyword("WITHIN") {
		p.parseWithinGroupClause()
	}

	if p.MatchKeyword("FILTER") {
		p.parseFilterClause(result)
	}

	if p.IsKeyword("OVER") {
		return p.parseWindowSuffix(result)
	}

	return result
}

// parseWindowSuffix parses an OVER named window or inline window spec.
//
// Takes innerFunction (*ast.FunctionCallExpression) which is the function call being
// windowed.
//
// Returns ast.Expression which is the resulting window function expression.
func (p *parser) parseWindowSuffix(innerFunction *ast.FunctionCallExpression) ast.Expression {
	p.Advance()

	if p.Current().Kind == tokenIdentifier && p.Peek().Kind != tokenLeftParen &&
		!p.IsKeyword("PARTITION", keywordORDER, keywordROWS, "RANGE", "GROUPS") {
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

// parseWindowSpec parses PARTITION BY, ORDER BY and frame clauses.
func (p *parser) parseWindowSpec() {
	if p.Current().Kind == tokenIdentifier &&
		!p.IsKeyword("PARTITION", keywordORDER, keywordROWS, "RANGE", "GROUPS") &&
		p.Current().Kind != tokenRightParen {
		p.Advance()
	}

	if p.MatchKeyword("PARTITION") {
		p.requireKeywordOrRecord(keywordBY)
		p.parseExpression()
		for p.Current().Kind == tokenComma {
			p.Advance()
			p.parseExpression()
		}
	}

	if p.MatchKeyword(keywordORDER) {
		p.requireKeywordOrRecord(keywordBY)
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

	if p.IsKeyword(keywordROWS, "RANGE", "GROUPS") {
		p.skipWindowFrame()
	}
}

// skipWindowFrame consumes a ROWS, RANGE or GROUPS frame clause.
func (p *parser) skipWindowFrame() {
	p.Advance()
	if p.MatchKeyword("BETWEEN") {
		p.skipFrameBound()
		p.MatchKeyword(keywordAND)
		p.skipFrameBound()
	} else {
		p.skipFrameBound()
	}
	if p.MatchKeyword("EXCLUDE") {
		p.MatchKeyword(keywordCURRENT)
		p.MatchKeyword(keywordROW)
		p.MatchKeyword(keywordGROUP)
		p.MatchKeyword("TIES")
		if p.MatchKeyword("NO") {
			p.MatchKeyword("OTHERS")
		}
	}
}

// skipFrameBound consumes a single frame boundary keyword and operand.
func (p *parser) skipFrameBound() {
	if p.MatchKeyword(keywordCURRENT) {
		p.MatchKeyword(keywordROW)
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

// parseCastFunctionExpression parses a CAST(expr AS type) or TRY_CAST(expr AS type)
// function form. TRY_CAST gives NULL for a value it cannot convert, so it reads as a CASE
// without ELSE over the conversion: the converted value, or NULL.
//
// Returns ast.Expression which is the resulting cast expression.
func (p *parser) parseCastFunctionExpression() ast.Expression {
	tries := p.IsKeyword("TRY_CAST")
	p.Advance()
	if p.Current().Kind != tokenLeftParen {
		return &ast.UnknownExpression{}
	}
	p.Advance()

	parameterCountBefore := p.parameterCount
	inner := p.parseExpression()

	if !p.MatchKeyword(keywordAS) {
		p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected AS and the type to convert to"))
	}

	typeName := p.parseCastTargetTypeName()
	if typeName == "" {
		p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected a type"))
	}

	if p.Current().Kind != tokenRightParen {
		p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected ')' to close CAST"))
	} else {
		p.Advance()
	}

	p.annotateCastParameter(typeName, parameterCountBefore)

	cast := &ast.CastExpression{
		TypeName: strings.ToLower(typeName),
		Inner:    inner,
	}
	if !tries {
		return cast
	}
	return &ast.CaseWhenExpression{Branches: []ast.CaseWhenBranch{{Condition: nil, Result: cast}}, ElseResult: nil}
}

// parseCastTargetTypeName parses the type name on the right side of CAST.
//
// Returns string which is the assembled type name including array suffixes.
func (p *parser) parseCastTargetTypeName() string {
	typeName := ""
	if p.Current().Kind == tokenIdentifier {
		typeName = p.Advance().Value
		for p.Current().Kind == tokenIdentifier &&
			!p.IsKeyword(keywordFROM, keywordWHERE, keywordGROUP, keywordHAVING, keywordORDER, keywordLIMIT) {
			typeName += " " + p.Advance().Value
		}
	}

	if p.Current().Kind == tokenLeftParen {
		typeName += p.parenthesisedTypeText()
	}

	typeName = p.appendArrayBrackets(typeName)
	return typeName
}

// parenthesisedTypeText consumes the parenthesised part of a type name, such as the
// (VARCHAR, INTEGER) of MAP(VARCHAR, INTEGER) or the (10, 2) of DECIMAL(10, 2), and
// returns its text, so the dialect reads the type with its parts.
//
// Returns string which is the text, its tokens separated by spaces.
func (p *parser) parenthesisedTypeText() string {
	start := p.Position
	p.skipParenthesisedOrRecord()
	var builder strings.Builder
	for index := start; index < p.Position && index < len(p.Tokens); index++ {
		if index > start {
			builder.WriteByte(' ')
		}
		builder.WriteString(p.Tokens[index].Raw)
	}
	return builder.String()
}

// appendArrayBrackets appends a [] suffix for each list bracket pair and a [N] suffix for
// each fixed-size array bracket pair seen.
//
// Takes typeName (string) which is the base type name.
//
// Returns string which is the type name with array subscript suffixes appended.
func (p *parser) appendArrayBrackets(typeName string) string {
	for p.Current().Kind == tokenLeftBracket {
		p.Advance()
		size := ""
		if p.Current().Kind == tokenNumber {
			size = p.Advance().Raw
		}
		if p.Current().Kind == tokenRightBracket {
			typeName += "[" + size + "]"
			p.Advance()
		}
	}
	return typeName
}

// annotateCastParameter records the cast target type on the inner parameter.
//
// Takes typeName (string) which is the parsed cast target type name.
// Takes parameterCountBefore (int) which is the parameter count recorded before the cast
// operand was parsed.
func (p *parser) annotateCastParameter(typeName string, parameterCountBefore int) {
	if typeName == "" || p.parameterCount != parameterCountBefore+1 {
		return
	}
	lastIndex := len(p.parameterReferences) - 1
	if lastIndex < 0 {
		return
	}
	p.parameterReferences[lastIndex].Context = sqlmodel.ParameterContextCast
	p.parameterReferences[lastIndex].CastType = new(p.types.normalise(typeName))
}

// parseCoalesceExpression parses a COALESCE(...) function call.
//
// Returns ast.Expression which is the resulting coalesce expression or an unknown
// expression on syntax mismatch.
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

// parseCoalesceArguments parses comma-separated COALESCE arguments.
//
// Returns []ast.Expression which is the parsed argument list.
// Returns *ast.ColumnReference which is the first column reference argument, or nil when
// no column reference was found.
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

// annotateCoalesceParameters tags COALESCE parameters with the first column.
//
// Takes firstColumnReference (*ast.ColumnReference) which is the first column reference
// seen among the arguments.
// Takes referenceCountBefore (int) which is the parameter reference count recorded before
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

// parseCaseExpression parses a CASE expression with branches and else.
//
// Returns ast.Expression which is the resulting CASE expression.
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

// parseExistsSubquery parses an EXISTS (...) subquery predicate.
//
// Returns ast.Expression which is the resulting EXISTS expression.
func (p *parser) parseExistsSubquery() ast.Expression {
	innerAnalysis, analyseError := p.analyseSubqueryBody()
	if analyseError != nil {
		return &ast.ExistsExpression{}
	}
	return &ast.ExistsExpression{InnerQuery: innerAnalysis}
}

// parseScalarSubquery parses a parenthesised scalar subquery.
//
// Returns ast.Expression which is the resulting scalar subquery expression or an unknown
// expression on parse failure.
func (p *parser) parseScalarSubquery() ast.Expression {
	innerAnalysis, analyseError := p.analyseSubqueryBody()
	if analyseError != nil {
		return &ast.UnknownExpression{}
	}
	return &ast.ScalarSubqueryExpression{InnerQuery: innerAnalysis}
}

// analyseSubqueryBody collects a parenthesised subquery and analyses it as a nested
// query, splicing its parameters back into this parser. Shared by the subquery
// expressions and the WHERE/HAVING predicate scan.
//
// A parser that does not analyse subqueries again, as when it records a clause's
// expressions after the clause scan, takes the analysis the scan made of the subquery.
//
// Returns *ast.RawQueryAnalysis which is the inner query's analysis.
// Returns error which is non-nil, and recorded as the statement's syntax error, when the
// subquery is unbalanced, malformed or leaves tokens unparsed.
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
	_, innerAnalysis, err := p.analyseNestedQuery(innerTokens)
	if err == nil {
		p.scannedSubqueries[open.Start] = innerAnalysis
	}
	return innerAnalysis, err
}

// parseArrayExpression parses ARRAY[...] or ARRAY(subquery) constructors.
//
// Returns ast.Expression which is an UnknownExpression placeholder or a scalar subquery.
func (p *parser) parseArrayExpression() ast.Expression {
	p.Advance()

	if p.Current().Kind == tokenLeftBracket {
		p.Advance()
		for !p.AtEnd() && p.Current().Kind != tokenRightBracket {
			p.parseExpression()
			if p.Current().Kind != tokenComma {
				break
			}
			p.Advance()
		}
		if p.Current().Kind == tokenRightBracket {
			p.Advance()
		}
		return &ast.UnknownExpression{}
	}

	if p.Current().Kind == tokenLeftParen {
		return p.parseScalarSubquery()
	}

	return &ast.UnknownExpression{}
}

// packedStructCall builds the struct a struct_pack call makes, a field per argument named
// as the argument is, when every argument is passed by name, as DuckDB requires.
//
// Takes loweredName (string) which is the lower-cased function name.
// Takes schema (string) which is the call's schema qualifier, or empty.
// Takes arguments ([]ast.Expression) which are the call's arguments.
//
// Returns ast.Expression which is the struct constructor, or nil for any other call.
func packedStructCall(loweredName, schema string, arguments []ast.Expression) ast.Expression {
	if loweredName != functionStructPack || schema != "" || len(arguments) == 0 {
		return nil
	}
	structure, allNamed := namedArgumentsStruct(arguments)
	if !allNamed {
		return nil
	}
	return structure
}

// insertedStructArguments gives a struct_insert call that adds every field by name the
// arguments the dialect types it by: the struct it adds to, then a struct constructor of
// the fields it adds, in order, so the call's type is the first struct's fields followed
// by the second's.
//
// Any other call keeps its arguments.
//
// Takes loweredName (string) which is the lower-cased function name.
// Takes schema (string) which is the call's schema qualifier, or empty.
// Takes arguments ([]ast.Expression) which are the call's arguments.
//
// Returns []ast.Expression which holds the arguments the call is typed by.
func insertedStructArguments(loweredName, schema string, arguments []ast.Expression) []ast.Expression {
	if loweredName != functionStructInsert || schema != "" || len(arguments) < minimumStructInsertArguments {
		return arguments
	}
	if _, isNamed := arguments[0].(*ast.NamedArgumentExpression); isNamed {
		return arguments
	}
	added, allNamed := namedArgumentsStruct(arguments[1:])
	if !allNamed {
		return arguments
	}
	return []ast.Expression{arguments[0], added}
}

// namedArgumentsStruct builds the struct constructor of arguments that are all passed by
// name: a field per argument, named as the argument is and holding its value.
//
// Takes arguments ([]ast.Expression) which are the arguments.
//
// Returns *ast.StructExpression which is the constructor, or nil when an argument is not
// passed by name.
// Returns bool which is true when every argument is passed by name.
func namedArgumentsStruct(arguments []ast.Expression) (*ast.StructExpression, bool) {
	names := make([]string, 0, len(arguments))
	values := make([]ast.Expression, 0, len(arguments))
	for _, argument := range arguments {
		named, isNamed := argument.(*ast.NamedArgumentExpression)
		if !isNamed || named == nil {
			return nil, false
		}
		names = append(names, named.Name)
		values = append(values, named.Value)
	}
	return &ast.StructExpression{Names: names, Values: values, Span: ast.Span{}}, true
}

// qualifiedCallName returns the name a call's parameters record as their enclosing
// function: the lower-cased name, after its lower-cased schema when it has one.
//
// Takes loweredName (string) which is the lower-cased function name.
// Takes schema (string) which is the schema qualifier, or empty.
//
// Returns string which is the qualified name.
func qualifiedCallName(loweredName, schema string) string {
	if schema == "" {
		return loweredName
	}
	return strings.ToLower(schema) + "." + loweredName
}

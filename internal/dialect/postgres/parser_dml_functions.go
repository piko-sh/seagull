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
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// parseFunctionCall parses a function-call expression after the opening `(` has been
// peeked, dispatching to the form without arguments or the form with them.
//
// The name is kept as the tokeniser gives it: an unquoted name already folded to lower
// case, and a quoted one as written, so "Foo"() and foo() call different functions.
//
// Takes name (string) which is the function's canonical name.
// Takes schema (string) which is the optional schema qualifier.
//
// Returns ast.Expression which is the function call expression.
func (p *parser) parseFunctionCall(name string, schema string) ast.Expression {
	p.Advance()

	if p.Current().Kind == tokenStar || p.Current().Kind == tokenRightParen {
		return p.parseFunctionCallWithoutArguments(name, schema)
	}

	return p.parseFunctionCallWithArguments(name, schema)
}

// parseFunctionCallWithoutArguments consumes a function call whose argument list is empty
// or a single `*`.
//
// Takes name (string) which is the function's canonical name.
// Takes schema (string) which is the optional schema qualifier.
//
// Returns ast.Expression which is the function call expression.
func (p *parser) parseFunctionCallWithoutArguments(name string, schema string) ast.Expression {
	if p.Current().Kind == tokenStar {
		p.Advance()
	}
	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}
	result := &ast.FunctionCallExpression{
		FunctionName:     name,
		Schema:           schema,
		FilterExpression: nil,
		Arguments:        nil,
		Span:             ast.Span{},
	}
	return p.parseFunctionSuffix(result)
}

// parseFunctionCallWithArguments consumes the argument list of a function call including
// optional DISTINCT / ALL modifiers and ORDER BY clause.
//
// Takes name (string) which is the function's canonical name.
// Takes schema (string) which is the optional schema qualifier.
//
// Returns ast.Expression which is the function call expression.
func (p *parser) parseFunctionCallWithArguments(name string, schema string) ast.Expression {
	p.MatchKeyword("DISTINCT")
	p.MatchKeyword(keywordALL)

	parameterCountBefore := p.parameterCount
	arguments, argumentSlots := p.parseFunctionArguments()
	arguments = readKeywordArgument(name, arguments)
	p.parseFunctionOrderByClause()
	p.skipSpecialArgumentSyntax()

	if p.Current().Kind != tokenRightParen {
		_ = p.syntaxError("expected ')' to close the function call")
	} else {
		p.Advance()
	}

	qualifiedName := name
	if schema != "" {
		qualifiedName = schema + "." + name
	}
	p.markParametersAsFunctionArguments(parameterCountBefore, qualifiedName, argumentSlots, argumentNamesOf(arguments))

	result := &ast.FunctionCallExpression{
		FunctionName:     name,
		Schema:           schema,
		Arguments:        arguments,
		FilterExpression: nil,
		Span:             ast.Span{},
	}

	return p.parseFunctionSuffix(result)
}

// parseFunctionArguments parses the comma-separated argument list of a function call,
// stopping at ORDER, LIMIT, or `)`.
//
// Alongside the parsed expressions it returns, per registered parameter reference index,
// the zero-based ordinal of the top-level argument slot the parameter sits in. The
// ordinal map lets markParametersAsFunctionArguments record ArgumentOrdinal as the
// call-site argument slot rather than the parameter number, so a non-placeholder argument
// (literal, column, expression) still consumes its slot. Parameters registered by a
// nested call are recorded against the outer argument slot they appear in, but the caller
// only tags those still missing an enclosing function name, so the inner call's own
// tagging is never clobbered.
//
// Returns []ast.Expression which is the argument list.
// Returns map[int]int which maps each parameterReferences index registered while parsing
// the argument list to its top-level argument ordinal.
func (p *parser) parseFunctionArguments() ([]ast.Expression, map[int]int) {
	var arguments []ast.Expression
	argumentSlots := make(map[int]int)
	ordinal := 0
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		if p.IsKeyword(keywordORDER, keywordLIMIT) {
			break
		}
		argumentName := p.parseArgumentName()
		referenceCountBefore := len(p.parameterReferences)
		argument := p.parseExpression()
		if argumentName != "" {
			argument = &ast.NamedArgumentExpression{Name: argumentName, Value: argument}
		}
		arguments = append(arguments, argument)
		for index := referenceCountBefore; index < len(p.parameterReferences); index++ {
			argumentSlots[index] = ordinal
		}
		ordinal++
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	return arguments, argumentSlots
}

// parseArgumentName consumes what may precede a function argument: the VARIADIC keyword,
// and an argument name written `name => value` or `name := value`.
//
// Returns string which is the argument's name, or empty for a positional argument.
func (p *parser) parseArgumentName() string {
	p.MatchKeyword("VARIADIC")
	if p.Current().Kind != tokenIdentifier || p.Peek().Kind != tokenOperator {
		return ""
	}
	name := p.Current().Value
	switch {
	case p.Peek().Value == "=>":
		p.Advance()
		p.Advance()
		return name
	case p.Peek().Value == ":" && p.Position+2 < len(p.Tokens) &&
		p.Tokens[p.Position+2].Kind == tokenOperator && p.Tokens[p.Position+2].Value == "=" &&
		p.Tokens[p.Position+2].Start == p.Peek().End:
		p.Advance()
		p.Advance()
		p.Advance()
		return name
	default:
		return ""
	}
}

// skipSpecialArgumentSyntax steps over what a call's argument list holds after the
// arguments the expression grammar reads and before its ')': the keyword syntax some
// functions take, such as xmlexists(...
//
// PASSING BY REF x) or json_value(... RETURNING type ON ERROR ...), registering the
// parameters and analysing the subqueries it holds.
func (p *parser) skipSpecialArgumentSyntax() {
	if !p.AtEnd() && p.Current().Kind != tokenRightParen {
		p.skipTokensUntilTerminatorSet(nil)
	}
}

// parseFunctionOrderByClause consumes an ORDER BY clause that may appear inside an
// aggregate function call.
func (p *parser) parseFunctionOrderByClause() {
	if !p.MatchKeyword(keywordORDER) {
		return
	}
	p.MatchKeyword(keywordBY)
	p.parseSortItems()
}

// parseSortItems parses a comma-separated list of sort items, each an expression with an
// optional ASC, DESC or USING operator and an optional NULLS FIRST or NULLS LAST.
//
// Returns []ast.Expression which holds the sorted expressions in order.
func (p *parser) parseSortItems() []ast.Expression {
	var expressions []ast.Expression
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		expressions = append(expressions, p.parseExpression())
		if !p.MatchKeyword(keywordASC) && !p.MatchKeyword(keywordDESC) && p.MatchKeyword(keywordUSING) {
			p.Advance()
		}
		if p.MatchKeyword(keywordNULLS) {
			p.MatchKeyword(keywordFIRST)
			p.MatchKeyword(keywordLAST)
		}
		if p.Current().Kind != tokenComma {
			return expressions
		}
		p.Advance()
	}
	return expressions
}

// markParametersAsFunctionArguments tags any parameter added since parameterCountBefore
// with the FunctionArgument context when it still has the Unknown context, and records
// the enclosing function name and the call-site argument ordinal on the references parsed
// from the top-level argument list so the resolver can look up the matched signature's
// declared argument type.
//
// Only references that still carry the Unknown context and an empty EnclosingFunctionName
// are touched, so a parameter already claimed by a nested call (for example $2 in
// outer_fn($1, inner_fn($2))) keeps its inner call's name and ordinal and is never
// clobbered by the outer call's marker. The context tag still uses the parameter-number
// threshold, including a placeholder in an aggregate ORDER BY that is not in
// argumentSlots, while the name and ordinal come from argumentSlots, which identifies
// exactly the references appended for each top-level argument slot.
//
// Takes parameterCountBefore (int) which is the parameterCount snapshot taken before
// parsing the argument list.
// Takes qualifiedName (string) which is the canonical, optionally schema-qualified
// enclosing function name.
// Takes argumentSlots (map[int]int) which maps each parameterReferences index registered
// while parsing the argument list to its top-level argument ordinal.
// Takes argumentNames ([]string) which holds the name each argument is passed by, empty
// for a positional one, so a parameter passed by name is recorded under it.
func (p *parser) markParametersAsFunctionArguments(
	parameterCountBefore int,
	qualifiedName string,
	argumentSlots map[int]int,
	argumentNames []string,
) {
	for index := range p.parameterReferences {
		if p.parameterReferences[index].Number <= parameterCountBefore ||
			p.parameterReferences[index].Context != sqlmodel.ParameterContextUnknown ||
			p.parameterReferences[index].EnclosingFunctionName != "" {
			continue
		}
		p.parameterReferences[index].Context = sqlmodel.ParameterContextFunctionArgument
		p.parameterReferences[index].EnclosingFunctionName = qualifiedName
		ordinal, ok := argumentSlots[index]
		if !ok {
			continue
		}
		p.parameterReferences[index].ArgumentOrdinal = ordinal
		if ordinal < len(argumentNames) && argumentNames[ordinal] != "" {
			p.recordArgumentName(p.parameterReferences[index].Number, argumentNames[ordinal])
		}
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

// parseWithinGroupClause consumes a WITHIN GROUP (ORDER BY ...) clause that follows an
// ordered-set aggregate.
//
// Returns []ast.Expression which holds the values the clause sorts, which PostgreSQL
// passes to the aggregate after its direct arguments.
func (p *parser) parseWithinGroupClause() []ast.Expression {
	p.MatchKeyword(keywordGROUP)
	if p.Current().Kind != tokenLeftParen {
		return nil
	}
	p.Advance()
	var sorted []ast.Expression
	if p.MatchKeyword(keywordORDER) {
		p.MatchKeyword(keywordBY)
		sorted = p.parseSortItems()
	}
	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}
	return sorted
}

// parseFilterClause consumes a FILTER (WHERE ...) clause that may follow an aggregate
// function call; its condition must fill the parentheses.
//
// Takes result (*ast.FunctionCallExpression) which is the function call to attach the
// filter expression to.
func (p *parser) parseFilterClause(result *ast.FunctionCallExpression) {
	if p.Current().Kind != tokenLeftParen {
		return
	}
	p.Advance()
	_ = p.requireKeyword(keywordWHERE)
	result.FilterExpression = p.parseExpression()
	if p.Current().Kind != tokenRightParen {
		_ = p.syntaxError("expected ')' to close the FILTER clause")
		return
	}
	p.Advance()
}

// parseFunctionSuffix consumes optional WITHIN GROUP, FILTER, and OVER suffixes on a
// function call. The values a WITHIN GROUP clause sorts become the call's last arguments,
// as PostgreSQL declares an ordered-set aggregate's aggregated arguments after its direct
// ones, so percentile_cont(0.5) WITHIN GROUP (ORDER BY took) types by both.
//
// Takes result (*ast.FunctionCallExpression) which is the inner function call.
//
// Returns ast.Expression which is the wrapped expression, or the original function call
// when no suffix applies.
func (p *parser) parseFunctionSuffix(result *ast.FunctionCallExpression) ast.Expression {
	if p.MatchKeyword("WITHIN") {
		result.Arguments = append(result.Arguments, p.parseWithinGroupClause()...)
	}

	if p.MatchKeyword("FILTER") {
		p.parseFilterClause(result)
	}

	if p.IsKeyword("OVER") {
		return p.parseWindowSuffix(result)
	}

	return result
}

// parseWindowSuffix consumes an OVER (window-spec) or OVER named-window clause that turns
// a function call into a window function expression.
//
// Takes innerFunction (*ast.FunctionCallExpression) which is the inner aggregate or
// window function.
//
// Returns ast.Expression which is the window function expression.
func (p *parser) parseWindowSuffix(innerFunction *ast.FunctionCallExpression) ast.Expression {
	p.Advance()

	if p.Current().Kind == tokenIdentifier {
		p.Advance()
		return &ast.WindowFunctionExpression{Function: innerFunction}
	}

	_ = p.parseParenthesisedWindowSpecification()
	return &ast.WindowFunctionExpression{Function: innerFunction}
}

// parseParenthesisedWindowSpecification consumes a parenthesised window specification,
// recording a syntax error when it is not closed.
//
// Returns error when the opening or closing parenthesis is missing.
func (p *parser) parseParenthesisedWindowSpecification() error {
	if p.Current().Kind != tokenLeftParen {
		return p.syntaxError("expected '(' to open the window specification")
	}
	p.Advance()
	p.parseWindowSpecification()
	if p.Current().Kind != tokenRightParen {
		return p.syntaxError("expected ')' to close the window specification")
	}
	p.Advance()
	return nil
}

// parseWindowSpecification consumes the body of a window specification, covering an
// optional reference name, PARTITION BY, ORDER BY, and frame clause.
func (p *parser) parseWindowSpecification() {
	if p.Current().Kind == tokenIdentifier &&
		!p.IsKeyword("PARTITION", keywordORDER, keywordROWS, "RANGE", "GROUPS") &&
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
		p.parseSortItems()
	}

	if p.IsKeyword(keywordROWS, "RANGE", "GROUPS") {
		p.skipWindowFrame()
	}
}

// skipWindowFrame consumes a window frame clause including BETWEEN ... AND, frame bounds,
// and any EXCLUDE modifier.
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

// skipFrameBound consumes a single window frame bound such as CURRENT ROW, UNBOUNDED
// PRECEDING/FOLLOWING, or `expression PRECEDING`.
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

// parseCastFunctionExpression parses a CAST(expression AS type) form and annotates any
// parameter in the expression with the target type.
//
// Returns ast.Expression which is the cast expression.
func (p *parser) parseCastFunctionExpression() ast.Expression {
	p.Advance()
	if p.Current().Kind != tokenLeftParen {
		return &ast.UnknownExpression{}
	}
	p.Advance()

	parameterCountBefore := p.parameterCount
	inner := p.parseExpression()

	p.MatchKeyword(keywordAS)

	typeName := p.parseCastTargetTypeName()

	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}

	p.annotateCastParameter(typeName, parameterCountBefore)

	return &ast.CastExpression{
		TypeName: typeName,
		Inner:    inner,
	}
}

// parseCastTargetTypeName collects the type-name tokens that follow `CAST(... AS` until
// the closing paren or an unrelated keyword: a possibly schema-qualified, multi-word
// name, a modifier list, a time zone qualifier after it, and array brackets.
//
// A quoted word is written in double quotes and the modifiers after the name, as in
// numeric(10,2), so the type normaliser reads both.
//
// Returns string which is the joined type name including any `[]` array suffixes.
func (p *parser) parseCastTargetTypeName() string {
	var builder strings.Builder
	if p.Current().Kind == tokenIdentifier {
		writeTypeWord(&builder, p.Advance())
		p.appendSchemaQualifier(&builder)
		for p.Current().Kind == tokenIdentifier &&
			!p.IsKeyword(keywordFROM, keywordWHERE, keywordGROUP, keywordHAVING, keywordORDER, keywordLIMIT) {
			builder.WriteByte(' ')
			writeTypeWord(&builder, p.Advance())
		}
	}

	p.appendTypeModifiers(&builder)
	p.appendTimeZoneSuffix(&builder)
	p.appendTypeArrayBrackets(&builder)
	return builder.String()
}

// annotateCastParameter tags the most recently registered parameter with the cast target
// type when the cast contained exactly one new parameter.
//
// Takes typeName (string) which is the normalised cast type name.
// Takes parameterCountBefore (int) which is the parameter count captured before parsing
// the cast expression.
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

// parseCoalesceExpression parses a COALESCE(...) call and propagates any column reference
// among its arguments to parameter siblings.
//
// Returns ast.Expression which is the COALESCE expression.
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

// parseCoalesceArguments parses the comma-separated argument list of a COALESCE call,
// returning the first encountered column reference.
//
// Returns []ast.Expression which is the argument list.
// Returns *ast.ColumnReference which is the first column reference found in the
// arguments, or nil when none was present.
func (p *parser) parseCoalesceArguments() ([]ast.Expression, *ast.ColumnReference) {
	var arguments []ast.Expression
	var firstColumnReference *ast.ColumnReference
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		argument := p.parseExpression()
		arguments = append(arguments, argument)
		if columnRefExpression, ok := argument.(*ast.ColumnRefExpression); ok && firstColumnReference == nil {
			firstColumnReference = &ast.ColumnReference{
				TableAlias: columnRefExpression.TableAlias,
				ColumnName: columnRefExpression.ColumnName,
			}
		}
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	return arguments, firstColumnReference
}

// annotateCoalesceParameters tags any parameter added inside the COALESCE call with the
// first column reference seen among its arguments.
//
// Takes firstColumnReference (*ast.ColumnReference) which is the column inferred from the
// COALESCE arguments.
// Takes referenceCountBefore (int) which is the parameterReferences length captured
// before parsing the call.
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

// parseCaseExpression parses a CASE expression covering both the simple and searched
// forms with optional ELSE branch.
//
// Returns ast.Expression which is the CASE expression.
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

// parseExistsSubquery parses an EXISTS (subquery) expression.
//
// The subquery is analysed by a child parser that inherits this parser's depth and
// parameter state, so a deeply chained EXISTS cannot reset the recursion cap. A subquery
// that fails to parse fails the whole statement.
//
// Returns ast.Expression which is the EXISTS expression.
func (p *parser) parseExistsSubquery() ast.Expression {
	innerAnalysis, err := p.analyseSubqueryBody()
	if err != nil {
		return &ast.ExistsExpression{}
	}
	return &ast.ExistsExpression{InnerQuery: innerAnalysis}
}

// parseScalarSubquery parses a parenthesised scalar subquery.
//
// The subquery is analysed by a child parser that inherits this parser's depth and
// parameter state, so a deeply chained sequence of scalar subqueries cannot reset the
// recursion cap. A subquery that fails to parse fails the whole statement.
//
// Returns ast.Expression which is the scalar subquery expression.
func (p *parser) parseScalarSubquery() ast.Expression {
	innerAnalysis, analyseError := p.analyseSubqueryBody()
	if analyseError != nil {
		return &ast.UnknownExpression{}
	}
	return &ast.ScalarSubqueryExpression{InnerQuery: innerAnalysis}
}

// analyseSubqueryBody analyses a parenthesised subquery in a child parser and merges its
// parameters into the parent, reusing prior analysis during a second scan. Collection,
// analysis or leftover-token failures become statement syntax errors so parameters are
// never silently lost.
//
// Returns *ast.RawQueryAnalysis which is the inner SELECT analysis (nil on failure).
// Returns error which is non-nil when the subquery could not be collected or analysed.
func (p *parser) analyseSubqueryBody() (*ast.RawQueryAnalysis, error) {
	open := p.Current()
	innerTokens, collectError := p.collectParenthesised()
	if collectError != nil {
		return nil, p.recordSyntaxError(collectError)
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

// analyseNestedSelect analyses a complete nested query while sharing parameter and depth
// state.
//
// The child must consume all tokens; its parameters are merged back into the parent.
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
	childParser := p.newChildParser(innerTokens)
	childParser.namedParameterMap = p.namedParameterMap
	innerAnalysis, analyseError := childParser.analyseSelect()
	if analyseError == nil {
		analyseError = childParser.requireEnd()
	}
	if analyseError != nil {
		return nil, p.recordSyntaxError(analyseError)
	}
	p.parameterCount = childParser.parameterCount
	p.parameterReferences = append(p.parameterReferences, childParser.parameterReferences...)
	for number, name := range childParser.argumentNames {
		p.recordArgumentName(number, name)
	}

	return innerAnalysis, nil
}

// parseArrayExpression parses an ARRAY[...] literal or ARRAY(subquery) constructor.
//
// Returns ast.Expression which is a call of arrayConstructor, which the dialect's
// resolver types as an array of its arguments' common type: the literal's elements, an
// inner [...] list being an array of its own elements, as PostgreSQL reads a
// multidimensional literal, or the subquery of the ARRAY(subquery) form, whose rows are
// the elements.
func (p *parser) parseArrayExpression() ast.Expression {
	p.Advance()

	if p.Current().Kind == tokenLeftBracket {
		return p.parseArrayElements()
	}

	if p.Current().Kind == tokenLeftParen {
		return arrayConstructorCall([]ast.Expression{p.parseScalarSubquery()})
	}

	return &ast.UnknownExpression{}
}

// parseArrayElements parses the bracketed element list of an ARRAY[...] literal, in which
// an element that opens another bracket is an inner list.
//
// Returns ast.Expression which is a call of arrayConstructor with the elements as its
// arguments.
func (p *parser) parseArrayElements() ast.Expression {
	p.Advance()
	var elements []ast.Expression
	for !p.AtEnd() && p.Current().Kind != tokenRightBracket {
		if p.Current().Kind == tokenLeftBracket {
			elements = append(elements, p.parseArrayElements())
		} else {
			elements = append(elements, p.parseExpression())
		}
		if p.Current().Kind != tokenComma {
			break
		}
		p.Advance()
	}
	if p.Current().Kind == tokenRightBracket {
		p.Advance()
	}
	return arrayConstructorCall(elements)
}

// argumentNamesOf lists the name of each argument of a call, empty for a positional one.
//
// Takes arguments ([]ast.Expression) which are the call's arguments.
//
// Returns []string which holds one name per argument.
func argumentNamesOf(arguments []ast.Expression) []string {
	names := make([]string, len(arguments))
	for index, argument := range arguments {
		if named, isNamed := argument.(*ast.NamedArgumentExpression); isNamed {
			names[index] = named.Name
		}
	}
	return names
}

// arrayConstructorCall returns the call of arrayConstructor an array constructor reads
// as.
//
// Takes elements ([]ast.Expression) which are the constructor's elements.
//
// Returns *ast.FunctionCallExpression which is the call.
func arrayConstructorCall(elements []ast.Expression) *ast.FunctionCallExpression {
	return &ast.FunctionCallExpression{FunctionName: arrayConstructor, Schema: "", Arguments: elements, FilterExpression: nil, Span: ast.Span{}}
}

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
	"fmt"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

// messageRecursiveFunction is the syntax error at a call a function's own body makes of
// it.
const messageRecursiveFunction = "a function cannot call itself; ClickHouse refuses to create a recursive function"

// parseCreateFunction handles `CREATE FUNCTION [IF NOT EXISTS] name [ON CLUSTER c] AS
// lambda`.
//
// ClickHouse user-defined functions are lambda expressions over scalar arguments: they
// read no table and change no data, so the function is read-only, and it takes one
// argument per lambda parameter. The lambda body is structurally parsed into an
// ast.Expression so the shared function-body analyser can infer the function's ReturnType
// where its parameters do not decide it, and its source text is kept as the function's
// body, in the lambda language, so a call can be typed from its arguments; see
// functionResolver.resolveLambdaFunction.
//
// The signature's ReturnType is initialised to TypeCategoryUnknown so the analyser can
// detect that no engine-specific type has been declared and fold in its inferred type. A
// fallback handles inputs without `AS` so such migrations still parse, in which case the
// function is registered with no Body and downstream consumers see ReturnType of
// TypeCategoryUnknown. A lambda whose body calls the function itself is refused, as
// ClickHouse refuses to create a recursive function.
//
// Returns *ast.CatalogueMutation which describes the create-function mutation.
// Returns error when the identifier or lambda body fails to parse, or the body calls the
// function being created.
func (p *parser) parseCreateFunction() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword(keywordCreate); err != nil {
		return nil, err
	}
	p.skipCreatePrefixesInParser()
	if _, err := p.ExpectKeyword("FUNCTION"); err != nil {
		return nil, err
	}
	ifNotExists := p.matchIfNotExists()

	name, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}
	_ = p.matchOnCluster()

	signature := sqlmodel.NewFunction(
		nil,
		sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
		sqlmodel.FunctionNullableReturnsNullOnNull,
		sqlmodel.WithFunctionName(name),
	)
	signature.DataAccess = sqlmodel.DataAccessReadOnly
	if !p.MatchKeyword("AS") {
		p.consumeRemainder()
		return ast.NewCatalogueMutation(ast.MutationCreateFunction, "", "", ast.WithFunction(ast.NewFunctionDefinition(signature))), nil
	}

	lambdaStart := p.Position
	body, parameters, lambdaErr := p.parseCreateFunctionLambdaBody()
	if lambdaErr != nil {
		return nil, fmt.Errorf("create function %q body: %w", name, lambdaErr)
	}
	if call := callOf(body, name); call != nil {
		return nil, &dialectkit.ParseError{Token: call.FunctionName, Message: messageRecursiveFunction, Position: call.Span.Start}
	}
	signature.Language = lambdaFunctionLanguage
	signature.BodySQL = dialectkit.SourceBetween("", p.Tokens, lambdaStart, p.Position)
	signature.Arguments = lambdaArguments(parameters)
	p.consumeRemainder()

	definition := ast.NewFunctionDefinition(signature)
	definition.Body = body
	definition.BodyParameters = parameters
	return ast.NewCatalogueMutation(ast.MutationCreateFunction, "", "", ast.WithFunction(definition), ast.WithIfNotExists(ifNotExists)), nil
}

// parseDropFunction handles `DROP FUNCTION [IF EXISTS] name`.
//
// Returns the catalogue mutation that captures the function name so the catalogue builder
// can detach the matching signature, or an error when the identifier is missing.
func (p *parser) parseDropFunction() (*ast.CatalogueMutation, error) {
	if err := p.expectKeywordSequence(keywordDrop, "FUNCTION"); err != nil {
		return nil, err
	}
	ifExists := p.matchIfExists()
	name, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}
	_ = p.matchOnCluster()
	return ast.NewCatalogueMutation(ast.MutationDropFunction, "", "", ast.WithFunction(ast.NewFunctionReference("", name)),
		ast.WithIfExists(ifExists)), nil
}

// parseCreateFunctionLambdaBody parses the lambda body of a `CREATE FUNCTION name AS
// lambda` statement and produces a structured ast.Expression suitable for downstream
// catalogue-builder analysis. The cursor must be positioned at the first token of the
// lambda (either the single parameter identifier or the opening paren of a parameter
// tuple).
//
// On success it returns the body expression and the parameter list in lexical order. On
// failure it returns a non-nil error which the sole caller wraps and returns, so callers
// must check err before inspecting the results.
//
// Returns ast.Expression which is the body expression tree.
// Returns []string which is the lexical parameter list, possibly empty for `() -> body`.
// Returns error when the input is malformed, such as a missing arrow or no body.
func (p *parser) parseCreateFunctionLambdaBody() (ast.Expression, []string, error) {
	parameters, parameterErr := p.consumeLambdaParameterList()
	if parameterErr != nil {
		return nil, nil, parameterErr
	}
	if p.Current().Kind != tokenArrow {
		return nil, nil, dialectkit.NewParseError(p.Current(), "expected '->'")
	}
	p.Advance()
	body := p.parseExpression()
	if p.syntaxError != nil {
		return nil, nil, p.syntaxError
	}
	return body, parameters, nil
}

// consumeLambdaParameterList consumes the parameter list that introduces a lambda.
//
// It accepts a bare identifier (`x -> body`) or a parenthesised tuple (`(a, b) -> body`);
// the empty tuple `()` yields a nil parameter slice. An error is returned when the head
// token is neither an identifier nor a left paren.
//
// Returns []string which holds the parameter names in declaration order.
// Returns error when the parameter list cannot be parsed.
func (p *parser) consumeLambdaParameterList() ([]string, error) {
	if p.Current().Kind == tokenIdentifier {
		name := p.Current().Value
		p.Advance()
		return []string{name}, nil
	}
	if p.Current().Kind != tokenLeftParen {
		return nil, dialectkit.NewParseError(p.Current(), "expected lambda parameter list")
	}
	p.Advance()
	var parameters []string
	for p.Current().Kind != tokenRightParen && !p.AtEnd() {
		if p.Current().Kind != tokenIdentifier {
			return nil, dialectkit.NewParseError(p.Current(), "expected parameter name")
		}
		parameters = append(parameters, p.Current().Value)
		p.Advance()
		if p.Current().Kind == tokenComma {
			p.Advance()
			continue
		}
		break
	}
	if p.Current().Kind != tokenRightParen {
		return nil, dialectkit.NewParseError(p.Current(), "expected ')'")
	}
	p.Advance()
	return parameters, nil
}

// lambdaArguments declares one argument of any type per lambda parameter, since a
// ClickHouse lambda accepts whatever its body does.
//
// Takes parameters ([]string) which are the lambda's parameter names.
//
// Returns []sqlmodel.FunctionArgument which holds the arguments.
func lambdaArguments(parameters []string) []sqlmodel.FunctionArgument {
	arguments := make([]sqlmodel.FunctionArgument, len(parameters))
	for index, parameter := range parameters {
		arguments[index] = sqlmodel.FunctionArgument{Name: parameter, Type: unknownType(), IsOptional: false, NotNull: false}
	}
	return arguments
}

// callOf finds a call of a function, by its exact name, anywhere in an expression, nested
// lambdas included.
//
// Takes expression (ast.Expression) which is the expression to search.
// Takes name (string) which is the function's name.
//
// Returns *ast.FunctionCallExpression which is the first call found, or nil.
func callOf(expression ast.Expression, name string) *ast.FunctionCallExpression {
	for node := range ast.Preorder(expression) {
		if call, isCall := node.(*ast.FunctionCallExpression); isCall && call != nil && call.FunctionName == name {
			return call
		}
	}
	return nil
}

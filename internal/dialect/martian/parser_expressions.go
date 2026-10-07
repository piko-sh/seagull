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

package martian

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
)

var (
	// comparisonOperators lists the operators that compare two values.
	comparisonOperators = []string{"=", "<>", "<", ">", "<=", ">="}

	// additiveOperators lists the operators of the additive precedence level.
	additiveOperators = []string{"+", "-"}

	// multiplicativeOperators lists the operators of the multiplicative precedence level.
	multiplicativeOperators = []string{"*", "/"}
)

// parseExpression parses a full expression, the lowest precedence level being OR.
//
// Returns expression which is the parsed expression.
// Returns error when the expression is malformed or nests too deeply.
func (p *parser) parseExpression() (expression, error) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > maximumExpressionDepth {
		current := p.current()
		return nil, newSyntaxError(p.input, current.offset, current.end,
			fmt.Sprintf("expression nests deeper than %d levels", maximumExpressionDepth))
	}
	return p.parseLogical("OR", p.parseConjunction)
}

// parseConjunction parses the AND precedence level.
//
// Returns expression which is the parsed expression.
// Returns error when the expression is malformed.
func (p *parser) parseConjunction() (expression, error) {
	return p.parseLogical("AND", p.parseComparison)
}

// parseLogical parses a chain of operands joined by one logical keyword.
//
// Takes keyword (string) which is AND or OR.
// Takes operand (func() (expression, error)) which parses the next-higher level.
//
// Returns expression which is the parsed chain.
// Returns error when an operand is malformed.
func (p *parser) parseLogical(keyword string, operand func() (expression, error)) (expression, error) {
	left, err := operand()
	if err != nil {
		return nil, err
	}
	for p.acceptKeyword(keyword) {
		right, rightError := operand()
		if rightError != nil {
			return nil, rightError
		}
		left = &binaryExpression{left: left, right: right, operator: keyword}
	}
	return left, nil
}

// parseComparison parses a comparison or an IS [NOT] NULL test.
//
// Returns expression which is the parsed expression.
// Returns error when the expression is malformed.
func (p *parser) parseComparison() (expression, error) {
	left, err := p.parseBinaryLevel(additiveOperators, p.parseMultiplicative)
	if err != nil {
		return nil, err
	}
	if p.acceptKeyword("IS") {
		negated := p.acceptKeyword(keywordNot)
		if err := p.expectKeyword(keywordNull); err != nil {
			return nil, err
		}
		return &nullTestExpression{operand: left, negated: negated}, nil
	}
	if p.acceptKeyword("IN") {
		return p.parseInList(left)
	}
	current := p.current()
	if current.kind != tokenSymbol || !slices.Contains(comparisonOperators, current.value) {
		return left, nil
	}
	p.advance()
	right, err := p.parseBinaryLevel(additiveOperators, p.parseMultiplicative)
	if err != nil {
		return nil, err
	}
	return &binaryExpression{left: left, right: right, operator: current.value}, nil
}

// parseInList parses the parenthesised member list of IN.
//
// Takes operand (expression) which is the tested expression.
//
// Returns expression which is the IN test.
// Returns error when the list is malformed.
func (p *parser) parseInList(operand expression) (expression, error) {
	if err := p.expectSymbol(symbolOpen); err != nil {
		return nil, err
	}
	test := &inListExpression{operand: operand, values: nil}
	for {
		member, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		test.values = append(test.values, member)
		if !p.acceptSymbol(symbolComma) {
			break
		}
	}
	return test, p.expectSymbol(symbolClose)
}

// parseMultiplicative parses the multiplicative precedence level.
//
// Returns expression which is the parsed expression.
// Returns error when the expression is malformed.
func (p *parser) parseMultiplicative() (expression, error) {
	return p.parseBinaryLevel(multiplicativeOperators, p.parsePrimary)
}

// parseBinaryLevel parses a left-associative chain of operands joined by any of the given
// symbols.
//
// Takes operators ([]string) which are the symbols of this level.
// Takes operand (func() (expression, error)) which parses the next-higher level.
//
// Returns expression which is the parsed chain.
// Returns error when an operand is malformed.
func (p *parser) parseBinaryLevel(operators []string, operand func() (expression, error)) (expression, error) {
	left, err := operand()
	if err != nil {
		return nil, err
	}
	for p.current().kind == tokenSymbol && slices.Contains(operators, p.current().value) {
		operator := p.advance().value
		right, rightError := operand()
		if rightError != nil {
			return nil, rightError
		}
		left = &binaryExpression{left: left, right: right, operator: operator}
	}
	return left, nil
}

// parsePrimary parses a literal, placeholder, parenthesised expression, column reference
// or function call.
//
// Returns expression which is the parsed expression.
// Returns error when the expression is malformed.
func (p *parser) parsePrimary() (expression, error) {
	current := p.current()
	switch {
	case current.kind == tokenNumber:
		p.advance()
		if strings.Contains(current.value, ".") {
			return &literalExpression{typeName: typeReal64}, nil
		}
		return &literalExpression{typeName: typeWhole64}, nil
	case current.kind == tokenString:
		p.advance()
		return &literalExpression{typeName: typeTextual}, nil
	case current.kind == tokenPlaceholder:
		p.advance()
		number, err := strconv.Atoi(current.value)
		return &parameterExpression{number: number}, err
	case current.isSymbol(symbolOpen):
		p.advance()
		inner, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		return inner, p.expectSymbol(symbolClose)
	case current.isKeyword(keywordNull):
		p.advance()
		return &literalExpression{typeName: ""}, nil
	case current.isName():
		return p.parseNameExpression()
	default:
		return nil, p.unexpected("an expression")
	}
}

// parseNameExpression parses a column reference, a qualified column reference or a
// function call.
//
// Returns expression which is the parsed expression.
// Returns error when the expression is malformed.
func (p *parser) parseNameExpression() (expression, error) {
	name := p.advance()
	if p.current().isSymbol(symbolOpen) && name.isKeyword(keywordConvert) {
		return p.parseConversion()
	}
	if p.current().isSymbol(symbolOpen) && name.kind == tokenIdentifier {
		return p.parseFunctionArguments(name)
	}
	if !p.acceptSymbol(".") {
		return &columnExpression{qualifier: "", name: name.value, span: p.spanFrom(name.offset)}, nil
	}
	column, err := p.expectName()
	if err != nil {
		return nil, err
	}
	return &columnExpression{qualifier: name.value, name: column, span: p.spanFrom(name.offset)}, nil
}

// parseConversion parses the parenthesised body of CONVERT(operand TO type), its type
// optionally followed by parenthesised modifiers.
//
// Returns expression which is the conversion.
// Returns error when the body is malformed.
func (p *parser) parseConversion() (expression, error) {
	p.advance()
	operand, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	if err := p.expectKeyword(keywordTo); err != nil {
		return nil, err
	}
	if p.current().kind != tokenIdentifier {
		return nil, p.unexpected("a type name")
	}
	conversion := &convertExpression{operand: operand, typeName: p.advance().value, modifiers: nil}
	if p.current().isSymbol(symbolOpen) {
		if conversion.modifiers, err = p.parseIntegerList(); err != nil {
			return nil, err
		}
	}
	return conversion, p.expectSymbol(symbolClose)
}

// parseFunctionArguments parses the parenthesised argument list of a function call.
//
// Takes name (token) which is the function's name, its value upper-cased.
//
// Returns expression which is the function call.
// Returns error when the argument list is malformed.
func (p *parser) parseFunctionArguments(name token) (expression, error) {
	p.advance()
	call := &functionExpression{name: name.value, arguments: nil, nameSpan: ast.Span{Start: name.offset, End: name.end}, star: false}
	if p.acceptSymbol("*") {
		call.star = true
		return call, p.expectSymbol(symbolClose)
	}
	if p.acceptSymbol(symbolClose) {
		return call, nil
	}
	for {
		argument, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		call.arguments = append(call.arguments, argument)
		if !p.acceptSymbol(symbolComma) {
			break
		}
	}
	return call, p.expectSymbol(symbolClose)
}

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

// parseSpecialFunctionCall reads SQL functions whose arguments use keywords instead of
// commas.
//
// It maps them to DuckDB's underlying function calls for type resolution. Comma-separated
// calls use the ordinary parser.
//
// Takes name (string) which is the function name; the cursor is on its '('.
//
// Returns ast.Expression which is the call, or nil when name is not one of these
// functions or the call uses commas.
func (p *parser) parseSpecialFunctionCall(name string) ast.Expression {
	if p.usesCommaArguments() {
		return nil
	}
	referenceCountBefore := len(p.parameterReferences)
	var call ast.Expression
	switch strings.ToLower(name) {
	case "extract":
		call = p.parseExtractCall()
	case "position":
		call = p.parsePositionCall()
	case "substring":
		call = p.parseKeywordArgumentCall("substring", keywordFROM, keywordFOR)
	case "trim":
		call = p.parseTrimCall()
	default:
		return nil
	}
	for index := referenceCountBefore; index < len(p.parameterReferences); index++ {
		if p.parameterReferences[index].Context == sqlmodel.ParameterContextUnknown {
			p.parameterReferences[index].Context = sqlmodel.ParameterContextFunctionArgument
		}
	}
	return call
}

// usesCommaArguments reports whether the parenthesised argument list at the cursor
// separates its top-level arguments with commas.
//
// Returns bool which is true when a top-level comma appears before the closing ')'.
func (p *parser) usesCommaArguments() bool {
	depth := 0
	for index := p.Position; index < len(p.Tokens); index++ {
		switch p.Tokens[index].Kind {
		case tokenLeftParen:
			depth++
		case tokenRightParen:
			depth--
			if depth == 0 {
				return false
			}
		case tokenComma:
			if depth == 1 {
				return true
			}
		default:
		}
	}
	return false
}

// parseExtractCall parses EXTRACT(field FROM source); the field is a text argument.
//
// Returns ast.Expression which is the extract(field, source) call.
func (p *parser) parseExtractCall() ast.Expression {
	p.Advance()
	if p.Current().Kind == tokenIdentifier || p.Current().Kind == tokenString {
		p.Advance()
	} else {
		p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected the field to extract"))
	}
	p.requireKeywordOrRecord(keywordFROM)
	source := p.parseExpression()
	return p.finishSpecialCall("extract", []ast.Expression{&ast.LiteralExpression{TypeName: "text"}, source})
}

// parsePositionCall parses POSITION(substring IN string).
//
// Returns ast.Expression which is the position(substring, string) call.
func (p *parser) parsePositionCall() ast.Expression {
	p.Advance()
	substring := p.parseAddExpression()
	p.requireKeywordOrRecord(keywordIN)
	text := p.parseExpression()
	return p.finishSpecialCall("position", []ast.Expression{substring, text})
}

// parseKeywordArgumentCall parses a call whose first argument is followed by further
// arguments, each introduced by one of keywords, in order and each optional.
//
// Takes functionName (string) which is the function the call becomes.
// Takes keywords (...string) which introduce the arguments after the first.
//
// Returns ast.Expression which is the call.
func (p *parser) parseKeywordArgumentCall(functionName string, keywords ...string) ast.Expression {
	p.Advance()
	arguments := []ast.Expression{p.parseExpression()}
	for _, keyword := range keywords {
		if p.MatchKeyword(keyword) {
			arguments = append(arguments, p.parseExpression())
		}
	}
	return p.finishSpecialCall(functionName, arguments)
}

// parseTrimCall parses TRIM([BOTH | LEADING | TRAILING] [characters] FROM string) or
// TRIM([BOTH | LEADING | TRAILING] string), which become trim, ltrim or rtrim of the
// string and the characters.
//
// Returns ast.Expression which is the trimming call.
func (p *parser) parseTrimCall() ast.Expression {
	p.Advance()
	functionName := "trim"
	switch {
	case p.MatchKeyword("LEADING"):
		functionName = "ltrim"
	case p.MatchKeyword("TRAILING"):
		functionName = "rtrim"
	default:
		p.MatchKeyword("BOTH")
	}
	if p.MatchKeyword(keywordFROM) {
		return p.finishSpecialCall(functionName, []ast.Expression{p.parseExpression()})
	}
	first := p.parseExpression()
	if !p.MatchKeyword(keywordFROM) {
		return p.finishSpecialCall(functionName, []ast.Expression{first})
	}
	text := p.parseExpression()
	return p.finishSpecialCall(functionName, []ast.Expression{text, first})
}

// finishSpecialCall consumes the closing parenthesis of a keyword-argument call and
// builds the call.
//
// Takes functionName (string) which is the function the call becomes.
// Takes arguments ([]ast.Expression) which are the call's arguments.
//
// Returns ast.Expression which is the call.
func (p *parser) finishSpecialCall(functionName string, arguments []ast.Expression) ast.Expression {
	if p.Current().Kind != tokenRightParen {
		p.recordSyntaxError(dialectkit.NewParseError(p.Current(), "expected ')' to close the "+functionName+" call"))
	} else {
		p.Advance()
	}
	return &ast.FunctionCallExpression{
		FunctionName:     functionName,
		Schema:           "",
		Arguments:        arguments,
		FilterExpression: nil,
		Span:             ast.Span{},
	}
}

// requireKeywordOrRecord consumes keyword, recording a syntax error when the current
// token is not it.
//
// Takes keyword (string) which is the keyword that must follow.
func (p *parser) requireKeywordOrRecord(keyword string) {
	if _, err := p.ExpectKeyword(keyword); err != nil {
		p.recordSyntaxError(err)
	}
}

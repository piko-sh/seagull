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
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

// parseCreateFunction parses a CREATE FUNCTION or CREATE PROCEDURE statement and produces
// a corresponding catalogue mutation.
//
// Takes engine (*Dialect) which provides hooks used when parsing the argument and return
// types.
//
// Returns *ast.CatalogueMutation which describes the function creation.
// Returns error when the schema-qualified name or argument list cannot be parsed.
func (p *parser) parseCreateFunction(engine *Dialect) (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword(keywordCREATE); err != nil {
		return nil, err
	}

	if p.MatchKeyword(keywordDEFINER) {
		p.skipDefinerClause()
	}

	if _, err := p.ExpectKeyword(keywordFUNCTION, keywordPROCEDURE); err != nil {
		return nil, err
	}

	schema, functionName, nameError := p.parseSchemaQualifiedName()
	if nameError != nil {
		return nil, nameError
	}

	arguments, argumentsError := p.parseFunctionArgumentList(engine)
	if argumentsError != nil {
		return nil, argumentsError
	}

	definition := ast.NewFunctionReference(schema, functionName)
	definition.Arguments = arguments

	p.parseFunctionReturnsClause(engine, definition.Function)
	p.parseFunctionAttributes(definition.Function)
	p.parseFunctionBodyCapture(definition.Function)

	return ast.NewCatalogueMutation(
		ast.MutationCreateFunction,
		schema,
		"",
		ast.WithFunction(definition),
	), nil
}

// parseDropFunction parses a DROP FUNCTION or DROP PROCEDURE statement and produces a
// corresponding catalogue mutation.
//
// Returns *ast.CatalogueMutation which describes the function removal, marked IF EXISTS
// when the statement says so.
// Returns error when the schema-qualified name cannot be parsed.
func (p *parser) parseDropFunction() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword(keywordDROP); err != nil {
		return nil, err
	}
	if _, err := p.ExpectKeyword(keywordFUNCTION, keywordPROCEDURE); err != nil {
		return nil, err
	}

	ifExists := p.matchIfExists()

	schema, functionName, nameError := p.parseSchemaQualifiedName()
	if nameError != nil {
		return nil, nameError
	}

	if p.Current().Kind == tokenLeftParen {
		if err := p.skipParenthesised(); err != nil {
			return nil, err
		}
	}

	return ast.NewCatalogueMutation(
		ast.MutationDropFunction,
		schema,
		"",
		ast.WithFunction(ast.NewFunctionReference(schema, functionName)),
		ast.WithIfExists(ifExists),
	), nil
}

// parseFunctionArgumentList parses the parenthesised argument list of a CREATE FUNCTION
// or CREATE PROCEDURE statement.
//
// Takes engine (*Dialect) which provides hooks used when parsing argument types.
//
// Returns []sqlmodel.FunctionArgument which is the parsed argument list, or nil when the
// argument list is absent.
// Returns error when the argument list is malformed (a token where a type is expected
// leaves the parser unable to make progress).
func (p *parser) parseFunctionArgumentList(engine *Dialect) ([]sqlmodel.FunctionArgument, error) {
	if p.Current().Kind != tokenLeftParen {
		return nil, nil
	}
	p.Advance()

	var arguments []sqlmodel.FunctionArgument
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		startPosition := p.Position
		argument := p.parseFunctionArgument(engine)
		arguments = append(arguments, argument)

		if p.Current().Kind == tokenComma {
			p.Advance()
		}

		if p.Position == startPosition {
			return nil, p.syntaxErrorAt("malformed function argument list")
		}
	}
	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}

	return arguments, nil
}

// parseFunctionArgument parses a single argument from a CREATE FUNCTION argument list.
//
// MySQL stored-function parameters cannot declare a DEFAULT value, so none are optional
// and MinArguments is always the full argument count; and MySQL has no array argument
// types, so there is no array dimension to capture. Both the optional-argument and
// array-argument handling that PostgreSQL and DuckDB perform are therefore intentionally
// absent here.
//
// Takes engine (*Dialect) which provides hooks used when parsing the argument type.
//
// Returns sqlmodel.FunctionArgument which describes the parsed argument.
func (p *parser) parseFunctionArgument(engine *Dialect) sqlmodel.FunctionArgument {
	p.MatchKeyword("IN")
	p.MatchKeyword("OUT")
	p.MatchKeyword("INOUT")

	savedPosition := p.Position
	possibleName, _ := p.parseIdentifierOrKeyword()

	if p.Current().Kind == tokenIdentifier {
		argumentType := p.parseRoutineType(engine)
		return sqlmodel.FunctionArgument{
			Name:       possibleName,
			Type:       argumentType,
			IsOptional: false,
			NotNull:    false,
		}
	}

	p.Position = savedPosition
	argumentType := p.parseRoutineType(engine)
	return sqlmodel.FunctionArgument{
		Type:       argumentType,
		Name:       "",
		IsOptional: false,
		NotNull:    false,
	}
}

// parseRoutineType parses the type of a routine argument or result. A malformed character
// set or collation clause is recorded as the statement's syntax error, which ApplyDDL
// returns once the statement has been walked.
//
// Takes engine (*Dialect) which normalises the type.
//
// Returns sqlmodel.SQLType which is the parsed type, or the zero type after an error.
func (p *parser) parseRoutineType(engine *Dialect) sqlmodel.SQLType {
	sqlType, err := p.parseColumnType(engine)
	if err != nil {
		p.recordSyntaxError(err)
	}
	return sqlType
}

// parseFunctionReturnsClause parses an optional RETURNS clause and assigns the resulting
// type to the signature.
//
// Takes engine (*Dialect) which provides hooks used when parsing the return type.
// Takes signature (*sqlmodel.Function) which receives the parsed return type.
func (p *parser) parseFunctionReturnsClause(engine *Dialect, signature *sqlmodel.Function) {
	if !p.MatchKeyword(keywordRETURNS) {
		return
	}
	signature.ReturnType = p.parseRoutineType(engine)
}

// parseFunctionAttributes consumes the routine characteristic clauses that follow a
// function signature.
//
// Takes signature (*sqlmodel.Function) which is updated as attributes are recognised.
func (p *parser) parseFunctionAttributes(signature *sqlmodel.Function) {
	for !p.AtEnd() && p.Current().Kind != tokenEOF {
		upper := p.Current().Keyword(tokenIdentifier)
		if upper == "BEGIN" || upper == keywordRETURN {
			return
		}
		if !p.parseSingleFunctionAttribute(signature) {
			return
		}
	}
}

// parseSingleFunctionAttribute consumes one routine characteristic clause.
//
// Takes signature (*sqlmodel.Function) which is updated when a data-access clause is
// recognised.
//
// Returns bool reporting whether an attribute was consumed.
func (p *parser) parseSingleFunctionAttribute(signature *sqlmodel.Function) bool {
	if p.MatchKeyword(keywordDETERMINISTIC) {
		return true
	}
	if p.MatchKeyword(keywordNOT) {
		p.MatchKeyword(keywordDETERMINISTIC)
		return true
	}
	if p.MatchKeyword(keywordREADS) {
		p.MatchKeyword(keywordSQL)
		p.MatchKeyword(keywordDATA)
		signature.DataAccess = sqlmodel.DataAccessReadOnly
		return true
	}
	if p.MatchKeyword(keywordMODIFIES) {
		p.MatchKeyword(keywordSQL)
		p.MatchKeyword(keywordDATA)
		signature.DataAccess = sqlmodel.DataAccessModifiesData
		return true
	}
	if p.MatchKeyword(keywordNO) {
		p.MatchKeyword(keywordSQL)
		signature.DataAccess = sqlmodel.DataAccessReadOnly
		return true
	}
	if p.MatchKeyword("CONTAINS") {
		p.MatchKeyword(keywordSQL)
		return true
	}
	return p.parseFunctionMetaAttribute(signature)
}

// parseFunctionMetaAttribute consumes LANGUAGE, SECURITY, COMMENT and SQL SECURITY
// routine characteristic clauses.
//
// Takes signature (*sqlmodel.Function) which is updated when a LANGUAGE clause is
// recognised.
//
// Returns bool reporting whether an attribute was consumed.
func (p *parser) parseFunctionMetaAttribute(signature *sqlmodel.Function) bool {
	if p.MatchKeyword(keywordLANGUAGE) {
		if !p.AtEnd() {
			signature.Language = strings.ToLower(p.Advance().Value)
		}
		return true
	}
	if p.MatchKeyword(keywordSECURITY) {
		if !p.AtEnd() {
			p.Advance()
		}
		return true
	}
	if p.MatchKeyword(keywordCOMMENT) {
		if p.Current().Kind == tokenString {
			p.Advance()
		}
		return true
	}
	if p.MatchKeyword(keywordSQL) {
		p.MatchKeyword(keywordSECURITY)
		if !p.AtEnd() {
			p.Advance()
		}
		return true
	}
	return false
}

// parseFunctionBodyCapture keeps the routine body, which runs from the current token to
// the end of the statement, exactly as the source writes it, and consumes it.
//
// A BEGIN ... END block is kept without its BEGIN and its closing END, and the expression
// of a RETURN without the RETURN, marking the routine's language as SQL. Any other body,
// such as a procedure's single statement, is kept whole. Nothing is counted: sqllex has
// already found where the statement ends, so an END IF, END LOOP or CASE ... END inside
// the body never closes it early, and quotes stay on the body's strings and names.
//
// Takes signature (*sqlmodel.Function) which receives the captured body and inferred
// language.
func (p *parser) parseFunctionBodyCapture(signature *sqlmodel.Function) {
	start, end := p.Position, len(p.Tokens)
	p.Position = end
	for end > start && p.Tokens[end-1].Kind == tokenSemicolon {
		end--
	}
	if start >= end {
		return
	}

	switch {
	case p.Tokens[start].IsKeyword(tokenIdentifier, "BEGIN") && p.Tokens[end-1].IsKeyword(tokenIdentifier, keywordEND):
		start++
		end--
	case p.Tokens[start].IsKeyword(tokenIdentifier, keywordRETURN):
		start++
		signature.Language = "sql"
	}
	signature.BodySQL = dialectkit.SourceBetween(p.source, p.Tokens, start, end)
}

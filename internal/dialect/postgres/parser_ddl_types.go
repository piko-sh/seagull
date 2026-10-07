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
	"fmt"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// maxReturnsTableColumns is the most fields a composite type, and the most columns a
	// CREATE FUNCTION ... RETURNS TABLE(...), may declare.
	//
	// PostgreSQL caps composite types at 1664 attributes, so a statement declaring more
	// cannot describe a real schema; it fails at the first field past the cap rather than
	// being kept in part.
	maxReturnsTableColumns = 1664
)

// functionArgumentMode is the parameter mode a CREATE FUNCTION argument declares.
type functionArgumentMode uint8

const (
	// functionArgumentModeIn is an input-only parameter, the default when no mode is given.
	functionArgumentModeIn functionArgumentMode = iota

	// functionArgumentModeOut identifies an output-only parameter that forms part of the
	// result and is never passed by a caller.
	functionArgumentModeOut

	// functionArgumentModeInOut is a parameter that is both passed in and returned.
	functionArgumentModeInOut

	// functionArgumentModeVariadic is a trailing parameter that accepts any number of
	// values.
	functionArgumentModeVariadic
)

// functionParameters holds a CREATE FUNCTION or CREATE PROCEDURE parameter list split
// into call arguments and result parameters.
type functionParameters struct {
	// inputs are the parameters a caller passes (IN, INOUT, VARIADIC, and procedure OUT).
	inputs []sqlmodel.FunctionArgument

	// outputs are the parameters that form the result (OUT and INOUT).
	outputs []sqlmodel.FunctionArgument
}

// add files a parameter among the parameters a caller passes, unless it is an OUT
// parameter of a function, and among those that form the result when it is an OUT or
// INOUT parameter.
//
// Takes argument (sqlmodel.FunctionArgument) which is the parameter.
// Takes mode (functionArgumentMode) which is its declared mode.
// Takes procedure (bool) which is true for a parameter of a procedure, whose caller
// passes its OUT parameters too.
func (parameters *functionParameters) add(argument sqlmodel.FunctionArgument, mode functionArgumentMode, procedure bool) {
	if mode != functionArgumentModeOut || procedure {
		parameters.inputs = append(parameters.inputs, argument)
	}
	if mode == functionArgumentModeOut || mode == functionArgumentModeInOut {
		parameters.outputs = append(parameters.outputs, argument)
	}
}

// parseCreateType parses a CREATE TYPE or CREATE DOMAIN statement and dispatches by kind.
//
// Takes engine (*Dialect) which supplies type-resolution context.
//
// Returns *ast.CatalogueMutation which describes the type mutation, or nil when the body
// is not a recognised form.
// Returns error when the type name fails to parse.
func (p *parser) parseCreateType(engine *Dialect) (*ast.CatalogueMutation, error) {
	if err := p.requireKeyword(keywordCREATE); err != nil {
		return nil, err
	}
	domain := p.MatchKeyword("DOMAIN")
	if !domain {
		if err := p.requireKeyword(keywordTYPE); err != nil {
			return nil, err
		}
	}
	ifNotExists := p.matchIfNotExists()

	schema, typeName, err := p.parseSchemaQualifiedName()
	if err != nil {
		return nil, err
	}
	mutation, err := p.parseCreateTypeBody(engine, domain, schema, typeName)
	if mutation != nil {
		mutation.IfNotExists = ifNotExists
	}
	return mutation, err
}

// parseCreateTypeBody parses what follows the name in a CREATE TYPE or CREATE DOMAIN
// statement.
//
// Takes engine (*Dialect) which supplies type-resolution context.
// Takes domain (bool) which is true for CREATE DOMAIN.
// Takes schema (string) which is the schema name of the new type.
// Takes typeName (string) which is the type's name.
//
// Returns *ast.CatalogueMutation which describes the type mutation, or nil when the body
// is not a recognised form.
// Returns error when a composite type's field fails to parse.
func (p *parser) parseCreateTypeBody(engine *Dialect, domain bool, schema, typeName string) (*ast.CatalogueMutation, error) {
	if domain {
		return p.parseCreateDomain(engine, schema, typeName), nil
	}
	if !p.MatchKeyword(keywordAS) {
		return nil, nil
	}
	if p.MatchKeyword("ENUM") {
		return p.parseCreateEnum(schema, typeName)
	}
	if p.Current().Kind == tokenLeftParen {
		return p.parseCreateCompositeType(engine, schema, typeName)
	}
	return nil, nil
}

// parseCreateDomain parses what follows the name in a CREATE DOMAIN statement: the base
// type and the constraints. A NOT NULL constraint makes the domain NOT NULL; CHECK
// constraints, defaults and collations do not change the type and are skipped.
//
// Takes engine (*Dialect) which normalises the base type.
// Takes schema (string) which is the schema name of the new domain.
// Takes name (string) which is the domain name.
//
// Returns *ast.CatalogueMutation which describes the domain creation.
func (p *parser) parseCreateDomain(engine *Dialect, schema, name string) *ast.CatalogueMutation {
	p.MatchKeyword(keywordAS)
	baseType, arrayDimensions := p.parseColumnType(engine)
	notNull := false
	for !p.AtEnd() && p.Current().Kind != tokenSemicolon && p.Current().Kind != tokenEOF {
		switch {
		case p.Current().Kind == tokenLeftParen:
			if p.requireSkipParenthesised() != nil {
				return nil
			}
		case p.IsKeyword(keywordNOT) && p.Peek().IsKeyword(tokenIdentifier, keywordNULL):
			p.Advance()
			p.Advance()
			notNull = true
		default:
			p.Advance()
		}
	}
	return ast.NewCatalogueMutation(
		ast.MutationCreateDomain,
		schema,
		"",
		ast.WithDomain(name, baseType, arrayDimensions, notNull),
	)
}

// parseCreateEnum parses the value list of a CREATE TYPE ... AS ENUM.
//
// Takes schema (string) which is the schema name of the new enum.
// Takes typeName (string) which is the enum type name.
//
// Returns *ast.CatalogueMutation which describes the enum creation.
// Returns error which is always nil; declared for caller uniformity.
func (p *parser) parseCreateEnum(schema, typeName string) (*ast.CatalogueMutation, error) {
	values := p.parseEnumValues()
	return ast.NewCatalogueMutation(
		ast.MutationCreateEnum,
		schema,
		"",
		ast.WithEnum(typeName, values),
	), nil
}

// parseEnumValues collects string literals from a parenthesised enum list.
//
// Returns []string which holds the enum value labels in declaration order.
func (p *parser) parseEnumValues() []string {
	if p.Current().Kind != tokenLeftParen {
		return nil
	}
	p.Advance()

	var values []string
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		if p.Current().Kind == tokenString {
			values = append(values, p.Current().Value)
		}
		p.Advance()
	}
	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}
	return values
}

// parseCreateCompositeType parses a CREATE TYPE ... AS (field type, ...) body.
//
// A field past maxReturnsTableColumns fails the statement with a syntax error at it.
//
// Takes engine (*Dialect) which supplies type-resolution context.
// Takes schema (string) which is the schema name of the new type.
// Takes typeName (string) which is the composite type name.
//
// Returns *ast.CatalogueMutation which describes the type creation.
// Returns error when a field name fails to parse or the type declares too many fields.
func (p *parser) parseCreateCompositeType(
	engine *Dialect,
	schema, typeName string,
) (*ast.CatalogueMutation, error) {
	p.Advance()

	var columns []sqlmodel.Column
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		if len(columns) == maxReturnsTableColumns {
			return nil, p.syntaxError(fmt.Sprintf("a composite type holds at most %d fields", maxReturnsTableColumns))
		}
		fieldName, fieldError := p.parseIdentifierOrKeyword()
		if fieldError != nil {
			return nil, fieldError
		}
		fieldType, arrayDimensions := p.parseColumnType(engine)
		columns = append(columns, newNullableColumn(fieldName, fieldType, arrayDimensions))

		if p.Current().Kind == tokenComma {
			p.Advance()
		}
	}
	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}

	return ast.NewCatalogueMutation(
		ast.MutationCreateCompositeType,
		schema,
		"",
		ast.WithTypeName(typeName),
		ast.WithColumns(columns),
	), nil
}

// parseAlterType parses an ALTER TYPE or ALTER DOMAIN statement and dispatches by action:
// ADD VALUE, RENAME VALUE, RENAME TO and SET SCHEMA change the catalogue, and any other
// action leaves it as it is.
//
// Returns *ast.CatalogueMutation which describes the mutation, or nil when no recognised
// action follows.
// Returns error when the type name fails to parse.
func (p *parser) parseAlterType() (*ast.CatalogueMutation, error) {
	if err := p.requireKeyword("ALTER"); err != nil {
		return nil, err
	}
	if !p.MatchKeyword("DOMAIN") {
		if err := p.requireKeyword(keywordTYPE); err != nil {
			return nil, err
		}
	}

	schema, typeName, err := p.parseSchemaQualifiedName()
	if err != nil {
		return nil, err
	}

	switch {
	case p.MatchKeyword("ADD"):
		return p.parseAlterTypeAddValue(schema, typeName)
	case p.IsKeyword("RENAME") && p.Peek().IsKeyword(tokenIdentifier, "TO"):
		p.Advance()
		p.Advance()
		newName, nameError := p.parseIdentifierOrKeyword()
		if nameError != nil {
			return nil, nameError
		}
		return ast.NewCatalogueMutation(ast.MutationAlterTypeRename, schema, "", ast.WithTypeName(typeName), ast.WithNewName(newName)), nil
	case p.MatchKeyword("RENAME"):
		return p.parseAlterTypeRenameValue(schema, typeName)
	case p.IsKeyword(keywordSET) && p.Peek().IsKeyword(tokenIdentifier, keywordSCHEMA):
		p.Advance()
		p.Advance()
		newSchema, schemaError := p.parseIdentifierOrKeyword()
		if schemaError != nil {
			return nil, schemaError
		}
		return ast.NewCatalogueMutation(ast.MutationAlterTypeSetSchema, schema, "", ast.WithTypeName(typeName), ast.WithNewName(newSchema)), nil
	}

	return nil, nil
}

// parseAlterTypeAddValue parses an ALTER TYPE ... ADD VALUE action, its IF NOT EXISTS and
// where its BEFORE or AFTER puts the value.
//
// Takes schema (string) which is the schema name of the target type.
// Takes typeName (string) which is the target enum type name.
//
// Returns *ast.CatalogueMutation which describes the mutation, or nil when the action is
// not recognisable.
// Returns error which is always nil; declared for caller uniformity.
func (p *parser) parseAlterTypeAddValue(schema, typeName string) (*ast.CatalogueMutation, error) {
	if !p.MatchKeyword("VALUE") {
		return nil, nil
	}

	ifNotExists := p.matchIfNotExists()

	if p.Current().Kind != tokenString {
		return nil, nil
	}
	newValue := p.Advance().Value

	placement := ast.PlacementDefault
	switch {
	case p.MatchKeyword("BEFORE"):
		placement = ast.PlacementBefore
	case p.MatchKeyword("AFTER"):
		placement = ast.PlacementAfter
	}
	anchor := ""
	if placement != ast.PlacementDefault && p.Current().Kind == tokenString {
		anchor = p.Advance().Value
	}

	return ast.NewCatalogueMutation(
		ast.MutationAlterEnumAddValue,
		schema,
		"",
		ast.WithEnum(typeName, []string{newValue}),
		ast.WithIfNotExists(ifNotExists),
		ast.WithPlacement(placement, anchor),
	), nil
}

// parseAlterTypeRenameValue parses an enum RENAME VALUE action in ALTER TYPE. RENAME
// ATTRIBUTE actions leave the catalogue unchanged.
//
// Takes schema (string) which is the schema name of the target type.
// Takes typeName (string) which is the target enum type name.
//
// Returns *ast.CatalogueMutation which describes the mutation, or nil when the action is
// not recognisable.
// Returns error which is always nil; declared for caller uniformity.
func (p *parser) parseAlterTypeRenameValue(schema, typeName string) (*ast.CatalogueMutation, error) {
	if !p.MatchKeyword("VALUE") {
		return nil, nil
	}

	if p.Current().Kind != tokenString {
		return nil, nil
	}
	oldValue := p.Advance().Value

	if err := p.requireKeyword("TO"); err != nil {
		return nil, err
	}

	if p.Current().Kind != tokenString {
		return nil, nil
	}
	newValue := p.Advance().Value

	return ast.NewCatalogueMutation(
		ast.MutationAlterEnumRenameValue,
		schema,
		"",
		ast.WithEnum(typeName, []string{oldValue, newValue}),
	), nil
}

// parseDropType parses a DROP TYPE or DROP DOMAIN statement.
//
// Returns *ast.CatalogueMutation which describes the drop mutation.
// Returns error when the type name fails to parse.
func (p *parser) parseDropType() (*ast.CatalogueMutation, error) {
	if err := p.requireKeyword(keywordDROP); err != nil {
		return nil, err
	}
	if !p.MatchKeyword("DOMAIN") {
		if err := p.requireKeyword(keywordTYPE); err != nil {
			return nil, err
		}
	}

	ifExists := p.matchIfExists()

	schema, typeName, err := p.parseSchemaQualifiedName()
	if err != nil {
		return nil, err
	}

	p.MatchKeyword(keywordCASCADE)
	p.MatchKeyword(keywordRESTRICT)

	return ast.NewCatalogueMutation(
		ast.MutationDropType,
		schema,
		"",
		ast.WithTypeName(typeName),
		ast.WithIfExists(ifExists),
	), nil
}

// parseCreateFunction parses a CREATE FUNCTION or CREATE PROCEDURE statement.
//
// Takes engine (*Dialect) which supplies type-resolution context.
//
// Returns *ast.CatalogueMutation which describes the function mutation.
// Returns error when a name, argument, or clause fails to parse.
func (p *parser) parseCreateFunction(engine *Dialect) (*ast.CatalogueMutation, error) {
	if err := p.requireKeyword(keywordCREATE); err != nil {
		return nil, err
	}
	p.skipOrReplace()
	procedure := p.IsKeyword("PROCEDURE")
	if err := p.requireKeyword("FUNCTION", "PROCEDURE"); err != nil {
		return nil, err
	}

	schema, functionName, err := p.parseSchemaQualifiedName()
	if err != nil {
		return nil, err
	}

	parameters, parametersError := p.parseFunctionArgumentList(engine, procedure)
	if parametersError != nil {
		return nil, parametersError
	}

	definition := ast.NewFunctionReference(schema, functionName)
	definition.Arguments = parameters.inputs
	definition.IsVariadic = p.lastArgumentWasVariadic
	p.lastArgumentWasVariadic = false

	tableColumns := p.parseFunctionBody(engine, definition.Function)
	tableColumns = applyOutputParameters(definition.Function, parameters.outputs, tableColumns)
	if procedure && len(parameters.outputs) == 0 {
		definition.ReturnType = engine.NormaliseTypeName(voidTypeName)
	}

	return ast.NewCatalogueMutation(
		ast.MutationCreateFunction,
		schema,
		"",
		ast.WithColumns(tableColumns),
		ast.WithFunction(definition),
	), nil
}

// parseFunctionArgumentList parses the parenthesised function argument list, splitting it
// into the parameters a caller passes and the parameters that form the result. A
// procedure's caller passes its OUT parameters too, as PostgreSQL's CALL takes them.
//
// Takes engine (*Dialect) which supplies type-resolution context.
// Takes procedure (bool) which is true for the parameters of a procedure.
//
// Returns functionParameters which holds the input and output parameters.
// Returns error when an argument fails to parse.
func (p *parser) parseFunctionArgumentList(engine *Dialect, procedure bool) (functionParameters, error) {
	var parameters functionParameters
	if p.Current().Kind != tokenLeftParen {
		return parameters, nil
	}
	p.Advance()

	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		startPosition := p.Position
		argument, mode, argumentError := p.parseFunctionArgument(engine)
		if argumentError != nil {
			return functionParameters{}, argumentError
		}
		parameters.add(argument, mode, procedure)

		if p.Current().Kind == tokenComma {
			p.Advance()
		}

		if p.Position == startPosition {
			return functionParameters{}, dialectkit.NewParseError(p.Current(), "malformed function argument list: no progress")
		}
	}
	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}

	return parameters, nil
}

// parseFunctionBody scans the trailing portion of a CREATE FUNCTION statement (RETURNS
// clause, LANGUAGE, volatility, AS body, etc.).
//
// Takes engine (*Dialect) which supplies type-resolution context.
// Takes signature (*sqlmodel.Function) which is populated as clauses are recognised.
//
// Returns []sqlmodel.Column which holds the inline column definitions captured from a
// table-returning clause, or nil for scalar / SETOF composite return forms.
func (p *parser) parseFunctionBody(
	engine *Dialect,
	signature *sqlmodel.Function,
) []sqlmodel.Column {
	var tableColumns []sqlmodel.Column
	for !p.AtEnd() && p.Current().Kind != tokenSemicolon && p.Current().Kind != tokenEOF {
		columns, matched := p.parseFunctionBodyClause(engine, signature)
		if !matched {
			p.Advance()
			continue
		}
		if columns != nil {
			tableColumns = columns
		}
	}
	return tableColumns
}

// parseFunctionBodyClause parses one CREATE FUNCTION body clause.
//
// Takes engine (*Dialect) which supplies type-resolution context.
// Takes signature (*sqlmodel.Function) which is populated when a clause is recognised.
//
// Returns []sqlmodel.Column which holds the RETURNS TABLE column definitions when the
// clause was a RETURNS TABLE, else nil.
// Returns bool which is true when a clause was consumed.
func (p *parser) parseFunctionBodyClause(
	engine *Dialect,
	signature *sqlmodel.Function,
) ([]sqlmodel.Column, bool) {
	if p.MatchKeyword("RETURNS") {
		columns := p.parseFunctionReturnsOrStrict(engine, signature)
		return columns, true
	}
	if p.MatchKeyword("LANGUAGE") {
		if !p.AtEnd() {
			signature.Language = strings.ToLower(p.Advance().Value)
		}
		return nil, true
	}
	if p.parseFunctionVolatilityAttribute(signature) {
		return nil, true
	}
	if p.parseFunctionNullInputAttribute(signature) {
		return nil, true
	}
	if p.Current().Kind == tokenDollarString || p.Current().Kind == tokenString || p.Current().Kind == tokenEscapeString {
		signature.BodySQL = p.Advance().Value
		return nil, true
	}
	if p.captureStandardFunctionBody(signature) {
		return nil, true
	}
	return nil, false
}

// captureStandardFunctionBody captures a SQL-standard function body, a RETURN expression
// or a BEGIN ATOMIC ...
//
// END block, as the function's body text exactly as written, so the catalogue sees what
// the body calls. Such a body is SQL, so a function that names no language is taken to be
// a SQL function.
//
// Takes signature (*sqlmodel.Function) which receives the body.
//
// Returns bool which is true when a body was captured.
func (p *parser) captureStandardFunctionBody(signature *sqlmodel.Function) bool {
	atomic := p.IsKeyword("BEGIN") && p.Peek().IsKeyword(tokenIdentifier, "ATOMIC")
	if !atomic && !p.IsKeyword("RETURN") {
		return false
	}
	start := p.Position
	for !p.AtEnd() && p.Current().Kind != tokenEOF && (atomic || p.Current().Kind != tokenSemicolon) {
		p.Advance()
	}
	signature.BodySQL = p.sourceText(start, p.Position)
	if signature.Language == "" {
		signature.Language = "sql"
	}
	return true
}

// parseFunctionReturnsOrStrict parses RETURNS or RETURNS NULL ON NULL INPUT.
//
// Takes engine (*Dialect) which supplies type-resolution context.
// Takes signature (*sqlmodel.Function) which receives the parsed return information.
//
// Returns []sqlmodel.Column which holds the RETURNS TABLE column definitions, or nil for
// the strict form and scalar / SETOF return forms.
func (p *parser) parseFunctionReturnsOrStrict(
	engine *Dialect,
	signature *sqlmodel.Function,
) []sqlmodel.Column {
	if p.MatchKeyword(keywordNULL) {
		p.MatchKeyword(keywordON)
		p.MatchKeyword(keywordNULL)
		p.MatchKeyword("INPUT")
		signature.IsStrict = true
		return nil
	}
	return p.parseFunctionReturns(engine, signature)
}

// parseFunctionVolatilityAttribute parses IMMUTABLE, STABLE, or VOLATILE.
//
// IMMUTABLE and STABLE functions cannot change the database, so they are read-only.
// VOLATILE says only that a call may give a different result each time; it says nothing
// about writing, so it leaves the data access to the body, which the catalogue scans.
//
// Takes signature (*sqlmodel.Function) which receives the data access classification.
//
// Returns bool which is true when an attribute was consumed.
func (p *parser) parseFunctionVolatilityAttribute(signature *sqlmodel.Function) bool {
	if p.MatchKeyword("IMMUTABLE") || p.MatchKeyword("STABLE") {
		signature.DataAccess = sqlmodel.DataAccessReadOnly
		return true
	}
	return p.MatchKeyword("VOLATILE")
}

// parseFunctionNullInputAttribute parses STRICT or CALLED ON NULL INPUT.
//
// Takes signature (*sqlmodel.Function) which receives the strict classification.
//
// Returns bool which is true when an attribute was consumed.
func (p *parser) parseFunctionNullInputAttribute(signature *sqlmodel.Function) bool {
	if p.MatchKeyword("STRICT") {
		signature.IsStrict = true
		return true
	}
	if p.MatchKeyword("CALLED") {
		p.MatchKeyword(keywordON)
		p.MatchKeyword(keywordNULL)
		p.MatchKeyword("INPUT")
		return true
	}
	return false
}

// parseFunctionReturns parses the RETURNS clause of a CREATE FUNCTION statement.
//
// For RETURNS TABLE (col1 type1, ...) it captures the inline column definitions and
// returns them; for RETURNS SETOF type or RETURNS type it records the type on the
// signature and returns nil.
//
// Takes engine (*Dialect) which supplies type-resolution context.
// Takes signature (*sqlmodel.Function) which receives the return type and SETOF flag.
//
// Returns []sqlmodel.Column which holds the RETURNS TABLE column definitions, or nil for
// scalar and SETOF return forms.
func (p *parser) parseFunctionReturns(
	engine *Dialect,
	signature *sqlmodel.Function,
) []sqlmodel.Column {
	if p.MatchKeyword(keywordTABLE) {
		signature.ReturnsSet = true
		if p.Current().Kind == tokenLeftParen {
			return p.parseFunctionReturnsTableColumns(engine)
		}
		return nil
	}
	if p.MatchKeyword("SETOF") {
		signature.ReturnsSet = true
	}
	returnType, _ := p.parseColumnType(engine)
	signature.ReturnType = returnType
	return nil
}

// parseFunctionReturnsTableColumns parses the column list inside a RETURNS TABLE (col
// type, col type, ...) clause.
//
// A column whose name is malformed, or one past maxReturnsTableColumns, records a syntax
// error at it, which fails the statement, and ends the list.
//
// Takes engine (*Dialect) which supplies type-resolution context.
//
// Returns []sqlmodel.Column which holds the parsed RETURNS TABLE columns, or nil when the
// list is malformed.
func (p *parser) parseFunctionReturnsTableColumns(engine *Dialect) []sqlmodel.Column {
	if p.Current().Kind != tokenLeftParen {
		return nil
	}
	p.Advance()

	columns := make([]sqlmodel.Column, 0)
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		if len(columns) == maxReturnsTableColumns {
			_ = p.syntaxError(fmt.Sprintf("a function returns a table of at most %d columns", maxReturnsTableColumns))
			return nil
		}
		fieldName, fieldError := p.parseIdentifierOrKeyword()
		if fieldError != nil {
			_ = p.recordSyntaxError(fieldError)
			return nil
		}
		fieldType, arrayDimensions := p.parseColumnType(engine)
		columns = append(columns, newNullableColumn(fieldName, fieldType, arrayDimensions))

		if p.Current().Kind == tokenComma {
			p.Advance()
		}
	}
	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}
	return columns
}

// parseFunctionArgument parses a single CREATE FUNCTION argument, including any
// IN/OUT/INOUT or VARIADIC mode, an optional argument name, its type, and a default
// written after DEFAULT or an equals sign.
//
// Takes engine (*Dialect) which supplies type-resolution context.
//
// Returns sqlmodel.FunctionArgument which is the parsed argument.
// Returns functionArgumentMode which is the declared parameter mode.
// Returns error when the argument has no type.
func (p *parser) parseFunctionArgument(engine *Dialect) (sqlmodel.FunctionArgument, functionArgumentMode, error) {
	mode := p.parseFunctionArgumentMode()

	savedPosition := p.Position
	possibleName, _ := p.parseIdentifierOrKeyword()

	if p.Current().Kind == tokenIdentifier && !p.isPostgresColumnConstraintKeyword() &&
		!p.IsKeyword(keywordDEFAULT, "COMMA") &&
		p.Current().Kind != tokenComma && p.Current().Kind != tokenRightParen {
		argumentType, arrayDimensions := p.parseColumnType(engine)
		argument := sqlmodel.FunctionArgument{
			Name:       possibleName,
			Type:       arrayTypeOf(argumentType, arrayDimensions),
			IsOptional: false,
			NotNull:    false,
		}
		argument.IsOptional = p.skipArgumentDefaultIfPresent()
		return argument, mode, nil
	}

	p.Position = savedPosition
	argumentType, arrayDimensions := p.parseColumnType(engine)
	if p.Position == savedPosition {
		return sqlmodel.FunctionArgument{}, mode, dialectkit.NewParseError(p.Current(), "expected the argument's type")
	}
	argument := sqlmodel.FunctionArgument{
		Type:       arrayTypeOf(argumentType, arrayDimensions),
		Name:       "",
		IsOptional: false,
		NotNull:    false,
	}
	argument.IsOptional = p.skipArgumentDefaultIfPresent()
	return argument, mode, nil
}

// skipArgumentDefaultIfPresent consumes the default of a function argument, written after
// DEFAULT or an equals sign, as PostgreSQL accepts both.
//
// Returns bool which is true when the argument has a default, so a call may leave it out.
func (p *parser) skipArgumentDefaultIfPresent() bool {
	switch {
	case p.MatchKeyword(keywordDEFAULT):
	case p.Current().Kind == tokenOperator && p.Current().Value == "=":
		p.Advance()
	default:
		return false
	}
	p.skipFunctionDefault()
	return true
}

// parseFunctionArgumentMode consumes an optional IN, OUT, INOUT, or VARIADIC mode
// keyword, recording a VARIADIC parameter on the parser so the signature can be marked
// variadic.
//
// Returns functionArgumentMode which is the declared mode, functionArgumentModeIn when
// none is written.
func (p *parser) parseFunctionArgumentMode() functionArgumentMode {
	switch {
	case p.MatchKeyword("IN"):
		return functionArgumentModeIn
	case p.MatchKeyword("OUT"):
		return functionArgumentModeOut
	case p.MatchKeyword("INOUT"):
		return functionArgumentModeInOut
	case p.MatchKeyword("VARIADIC"):
		p.lastArgumentWasVariadic = true
		return functionArgumentModeVariadic
	}
	return functionArgumentModeIn
}

// skipFunctionDefault advances past a DEFAULT expression value.
func (p *parser) skipFunctionDefault() {
	depth := 0
	for !p.AtEnd() {
		if p.Current().Kind == tokenLeftParen {
			depth++
			p.Advance()
			continue
		}
		if p.Current().Kind == tokenRightParen {
			if depth == 0 {
				return
			}
			depth--
			p.Advance()
			continue
		}
		if depth == 0 && p.Current().Kind == tokenComma {
			return
		}
		p.Advance()
	}
}

// parseDropFunction parses a DROP FUNCTION or DROP PROCEDURE statement.
//
// Returns *ast.CatalogueMutation which describes the drop mutation.
// Returns error when the function name fails to parse.
func (p *parser) parseDropFunction() (*ast.CatalogueMutation, error) {
	if err := p.requireKeyword(keywordDROP); err != nil {
		return nil, err
	}
	if err := p.requireKeyword("FUNCTION", "PROCEDURE"); err != nil {
		return nil, err
	}

	ifExists := p.matchIfExists()

	schema, functionName, err := p.parseSchemaQualifiedName()
	if err != nil {
		return nil, err
	}

	if p.Current().Kind == tokenLeftParen {
		if err := p.requireSkipParenthesised(); err != nil {
			return nil, err
		}
	}

	p.MatchKeyword(keywordCASCADE)
	p.MatchKeyword(keywordRESTRICT)

	return ast.NewCatalogueMutation(
		ast.MutationDropFunction,
		schema,
		"",
		ast.WithFunction(ast.NewFunctionReference(schema, functionName)),
		ast.WithIfExists(ifExists),
	), nil
}

// parseCreateSchema parses a CREATE SCHEMA statement.
//
// Returns *ast.CatalogueMutation which describes the schema creation.
// Returns error when the schema or role name fails to parse.
func (p *parser) parseCreateSchema() (*ast.CatalogueMutation, error) {
	if err := p.requireKeywordSequence(keywordCREATE, keywordSCHEMA); err != nil {
		return nil, err
	}

	p.matchIfNotExists()

	if p.MatchKeyword("AUTHORIZATION") {
		roleName, roleError := p.parseIdentifierOrKeyword()
		if roleError != nil {
			return nil, roleError
		}
		return ast.NewCatalogueMutation(ast.MutationCreateSchema, roleName, ""), nil
	}

	schemaName, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}

	return ast.NewCatalogueMutation(ast.MutationCreateSchema, schemaName, ""), nil
}

// parseDropSchema parses a DROP SCHEMA statement.
//
// Returns *ast.CatalogueMutation which describes the drop mutation.
// Returns error when the schema name fails to parse.
func (p *parser) parseDropSchema() (*ast.CatalogueMutation, error) {
	if err := p.requireKeywordSequence(keywordDROP, keywordSCHEMA); err != nil {
		return nil, err
	}

	ifExists := p.matchIfExists()

	schemaName, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}

	p.MatchKeyword(keywordCASCADE)
	p.MatchKeyword(keywordRESTRICT)

	return ast.NewCatalogueMutation(ast.MutationDropSchema, schemaName, "", ast.WithIfExists(ifExists)), nil
}

// parseCreateExtension parses a CREATE EXTENSION statement.
//
// Returns *ast.CatalogueMutation which describes the extension creation.
// Returns error when the extension name fails to parse.
func (p *parser) parseCreateExtension() (*ast.CatalogueMutation, error) {
	if err := p.requireKeywordSequence(keywordCREATE, "EXTENSION"); err != nil {
		return nil, err
	}

	p.matchIfNotExists()

	extensionName, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}

	schemaName := ""
	p.MatchKeyword(keywordWITH)
	if p.MatchKeyword(keywordSCHEMA) {
		name, nameError := p.parseIdentifierOrKeyword()
		if nameError == nil {
			schemaName = name
		}
	}

	return ast.NewCatalogueMutation(ast.MutationCreateExtension, schemaName, extensionName), nil
}

// parseDropExtension parses a DROP EXTENSION statement.
//
// Returns *ast.CatalogueMutation which is always nil; the engine ignores extension drops.
// Returns error when the statement does not name an extension.
func (p *parser) parseDropExtension() (*ast.CatalogueMutation, error) {
	if err := p.requireKeywordSequence(keywordDROP, "EXTENSION"); err != nil {
		return nil, err
	}

	p.matchIfExists()

	if _, err := p.requireIdentifierOrKeyword(); err != nil {
		return nil, err
	}

	return nil, nil
}

// arrayTypeOf wraps a scalar type in an array type once per array dimension, such as the
// text[] or text[][] of a CREATE FUNCTION argument or a function's array result.
//
// Function arguments carry their array-ness in the SQLType itself, unlike table columns
// which record it on Column.IsArray/ArrayDimensions, so the overload resolver can tell a
// call to f(text) from a call to f(text[]). The element type is preserved verbatim, so a
// schema-qualified or modified base type keeps its identity. A zero dimension count
// returns the element type unchanged.
//
// Takes elementType (sqlmodel.SQLType) which is the scalar (non-array) type.
// Takes dimensions (int) which is the number of array dimensions to wrap.
//
// Returns sqlmodel.SQLType which is elementType wrapped in that many array layers.
func arrayTypeOf(elementType sqlmodel.SQLType, dimensions int) sqlmodel.SQLType {
	wrapped := elementType
	for range dimensions {
		element := wrapped
		wrapped = sqlmodel.SQLType{}
		wrapped.Category = sqlmodel.TypeCategoryArray
		wrapped.EngineName = element.EngineName + arraySubscriptSuffix
		wrapped.ElementType = &element
	}
	return wrapped
}

// applyOutputParameters folds a function's OUT and INOUT parameters into its result shape
// when the RETURNS clause leaves it open (absent, record, or SETOF record).
//
// One output parameter becomes the return type, and names the column a call in a FROM
// clause gives; several become the result columns, and the return type is cleared so the
// catalogue synthesises a row type from them, exactly as for RETURNS TABLE.
//
// Takes signature (*sqlmodel.Function) which receives the return type.
// Takes outputs ([]sqlmodel.FunctionArgument) which are the OUT and INOUT parameters.
// Takes tableColumns ([]sqlmodel.Column) which are any RETURNS TABLE columns.
//
// Returns []sqlmodel.Column which is the result column list to record.
func applyOutputParameters(
	signature *sqlmodel.Function,
	outputs []sqlmodel.FunctionArgument,
	tableColumns []sqlmodel.Column,
) []sqlmodel.Column {
	if len(outputs) == 0 || tableColumns != nil || !isOpenReturnType(signature.ReturnType) {
		return tableColumns
	}
	if len(outputs) == 1 {
		signature.ReturnType = outputs[0].Type
		signature.ResultName = outputs[0].Name
		return nil
	}
	columns := make([]sqlmodel.Column, 0, len(outputs))
	for index := range outputs {
		columns = append(columns, sqlmodel.NewColumn(outputs[index].Name, outputs[index].Type, true))
	}
	signature.ReturnType = sqlmodel.SQLType{}
	return columns
}

// isOpenReturnType reports whether a parsed return type leaves the result shape to the
// function's output parameters by omitting the RETURNS clause or using the generic record
// type.
//
// Takes returnType (sqlmodel.SQLType) which is the parsed return type.
//
// Returns bool which is true when output parameters define the result.
func isOpenReturnType(returnType sqlmodel.SQLType) bool {
	return returnType.EngineName == "" || strings.EqualFold(returnType.EngineName, "record")
}

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
	"seagull.sh/seagull/internal/sqlmodel"
)

// macroEngine is what parsing a macro needs from the dialect: resolving argument type
// names, and the built-in functions a macro body calls, whose data access the macro takes
// on.
type macroEngine interface {
	typeNormaliser

	// BuiltinFunctions returns the dialect's built-in function catalogue.
	//
	// Returns *ast.FunctionCatalogue which holds the built-in functions.
	BuiltinFunctions() *ast.FunctionCatalogue
}

// parseCreateType parses a CREATE TYPE statement.
//
// Takes engine (typeNormaliser) which normalises field type names.
//
// Returns *ast.CatalogueMutation which describes the catalogue change, or nil when the
// statement form is unrecognised.
// Returns error when parsing the type name fails.
func (p *parser) parseCreateType(engine typeNormaliser) (*ast.CatalogueMutation, error) {
	if err := p.expectKeywords(keywordCREATE, keywordTYPE); err != nil {
		return nil, err
	}

	schema, typeName, err := p.parseSchemaQualifiedName()
	if err != nil {
		return nil, err
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

// parseCreateEnum parses the body of a CREATE TYPE ... AS ENUM statement.
//
// Takes schema (string) which is the schema for the new enum.
// Takes typeName (string) which is the name of the new enum.
//
// Returns *ast.CatalogueMutation which describes the create-enum mutation.
// Returns error which is always nil.
func (p *parser) parseCreateEnum(schema, typeName string) (*ast.CatalogueMutation, error) {
	values := p.parseEnumValues()
	return ast.NewCatalogueMutation(
		ast.MutationCreateEnum,
		schema,
		"",
		ast.WithEnum(typeName, values),
	), nil
}

// parseEnumValues parses a parenthesised list of enum string literals.
//
// Returns []string which lists the parsed enum values in declared order.
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

// parseCreateCompositeType parses a CREATE TYPE ... AS (...) statement.
//
// Takes engine (typeNormaliser) which normalises field type names.
// Takes schema (string) which is the schema for the new composite type.
// Takes typeName (string) which is the name of the new composite type.
//
// Returns *ast.CatalogueMutation which describes the create-composite-type mutation.
// Returns error when a field name cannot be parsed.
func (p *parser) parseCreateCompositeType(
	engine typeNormaliser,
	schema, typeName string,
) (*ast.CatalogueMutation, error) {
	p.Advance()

	var columns []sqlmodel.Column
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		fieldName, fieldError := p.parseIdentifierOrKeyword()
		if fieldError != nil {
			return nil, fieldError
		}
		fieldType, arrayDimensions := p.parseColumnType(engine)
		column := sqlmodel.NewColumn(fieldName, fieldType, true)
		column.IsArray = arrayDimensions > 0
		column.ArrayDimensions = arrayDimensions
		columns = append(columns, column)

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

// parseAlterType parses an ALTER TYPE statement.
//
// Returns *ast.CatalogueMutation which describes the alter-type mutation, or nil when no
// recognised sub-clause follows.
// Returns error when parsing the type name fails.
func (p *parser) parseAlterType() (*ast.CatalogueMutation, error) {
	if err := p.expectKeywords("ALTER", keywordTYPE); err != nil {
		return nil, err
	}

	schema, typeName, err := p.parseSchemaQualifiedName()
	if err != nil {
		return nil, err
	}

	if p.MatchKeyword("ADD") {
		return p.parseAlterTypeAddValue(schema, typeName)
	}
	if p.MatchKeyword("RENAME") {
		return p.parseAlterTypeRenameValue(schema, typeName)
	}

	return nil, nil
}

// parseAlterTypeAddValue parses ALTER TYPE ... ADD VALUE.
//
// Takes schema (string) which is the schema of the target enum.
// Takes typeName (string) which is the name of the target enum.
//
// Returns *ast.CatalogueMutation which describes the add-value mutation, or nil when the
// form is unrecognised.
// Returns error which is always nil.
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

// parseAlterTypeRenameValue parses ALTER TYPE ... RENAME VALUE.
//
// Takes schema (string) which is the schema of the target enum.
// Takes typeName (string) which is the name of the target enum.
//
// Returns *ast.CatalogueMutation which describes the rename-value mutation, or nil when
// the form is unrecognised.
// Returns error when TO does not follow the old value.
func (p *parser) parseAlterTypeRenameValue(schema, typeName string) (*ast.CatalogueMutation, error) {
	if !p.MatchKeyword("VALUE") {
		return nil, nil
	}

	if p.Current().Kind != tokenString {
		return nil, nil
	}
	oldValue := p.Advance().Value

	if _, err := p.ExpectKeyword("TO"); err != nil {
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

// parseDropType parses a DROP TYPE statement.
//
// Returns *ast.CatalogueMutation which describes the drop-type mutation.
// Returns error when parsing the type name fails.
func (p *parser) parseDropType() (*ast.CatalogueMutation, error) {
	if err := p.expectKeywords(keywordDROP, keywordTYPE); err != nil {
		return nil, err
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

// parseCreateMacro parses a CREATE [OR REPLACE] [TEMP] MACRO or FUNCTION statement.
//
// A scalar macro's body is parsed as an expression, from which its return type is
// inferred where it can be, and its data access is taken from the functions the body
// calls. A table macro's body is recorded as SQL, so its data access is taken from the
// query and its columns can be typed against the catalogue.
//
// Takes engine (macroEngine) which normalises argument type names and holds the built-in
// functions.
//
// Returns *ast.CatalogueMutation which describes the create-function mutation.
// Returns error when parsing the name, arguments or body fails.
func (p *parser) parseCreateMacro(engine macroEngine) (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword(keywordCREATE); err != nil {
		return nil, err
	}
	p.skipOrReplace()
	p.skipTemporary()
	if _, err := p.ExpectKeyword(keywordMACRO, "FUNCTION"); err != nil {
		return nil, err
	}
	ifNotExists := p.matchIfNotExists()

	schema, macroName, err := p.parseSchemaQualifiedName()
	if err != nil {
		return nil, err
	}

	arguments, argumentsError := p.parseFunctionArgumentList(engine)
	if argumentsError != nil {
		return nil, argumentsError
	}

	definition := ast.NewFunctionReference(schema, macroName)
	definition.Arguments = arguments
	definition.IsVariadic = p.lastArgumentWasVariadic
	p.lastArgumentWasVariadic = false

	if _, err := p.ExpectKeyword(keywordAS); err != nil {
		p.recordSyntaxError(err)
		return nil, err
	}

	var tableColumns []sqlmodel.Column
	var unanalysed *ast.Warning
	if p.MatchKeyword(keywordTABLE) {
		definition.ReturnsSet = true
		definition.Language = "sql"
		definition.BodySQL = p.sourceText(p.Position, len(p.Tokens))
		var bodyError error
		if tableColumns, unanalysed, bodyError = p.captureTableMacroColumns(); bodyError != nil {
			return nil, bodyError
		}
	} else if err := p.captureMacroBody(definition, engine.BuiltinFunctions()); err != nil {
		return nil, err
	}

	return ast.NewCatalogueMutation(
		ast.MutationCreateFunction,
		schema,
		"",
		ast.WithColumns(tableColumns),
		ast.WithFunction(definition),
		ast.WithIfNotExists(ifNotExists),
		withUnanalysedBody(unanalysed),
	), nil
}

// captureTableMacroColumns analyses the query body of a CREATE MACRO ... AS TABLE macro
// so its projected columns are stored on the catalogue mutation.
//
// attachReturnsTableColumns then builds a synthetic composite return type from them, so a
// query that uses the macro in a FROM clause resolves its output columns instead of being
// reported as an unknown table-valued function; TableValuedFunctionColumnsFromCatalogue
// types them against the catalogue. When Seagull cannot analyse the body, as
// analyseQueryBody describes, the macro stays registered as a set-returning function with
// no columns, and the warning returned says why; a malformed body fails the statement.
//
// Returns []sqlmodel.Column which holds the macro's projected columns by name, or nil
// when the body is not analysed.
// Returns *ast.Warning which says why the body was not analysed, or nil.
// Returns error when the body is missing or malformed.
func (p *parser) captureTableMacroColumns() ([]sqlmodel.Column, *ast.Warning, error) {
	analysis, unanalysed, err := p.analyseQueryBody("the table macro's query")
	if analysis == nil {
		return nil, unanalysed, err
	}
	var columns []sqlmodel.Column
	for index := range analysis.OutputColumns {
		name := analysis.OutputColumns[index].Name
		if name == "" {
			name = analysis.OutputColumns[index].ColumnName
		}
		if name == "" {
			continue
		}
		columns = append(columns, sqlmodel.NewColumn(name, sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true))
	}
	return columns, nil, nil
}

// captureMacroBody records a scalar macro's expression and infers its type and data
// access.
//
// Types that depend on parameters are resolved later, using the arguments supplied at
// each call.
//
// Takes definition (*ast.FunctionDefinition) which receives the inferred return type and
// the data access.
// Takes builtins (*ast.FunctionCatalogue) which holds the built-in functions.
//
// Returns error when the body is malformed or leaves tokens unparsed.
func (p *parser) captureMacroBody(definition *ast.FunctionDefinition, builtins *ast.FunctionCatalogue) error {
	if p.AtEnd() {
		return p.syntaxErrorAt("expected the macro body")
	}
	bodyStart := p.Position
	expression := p.parseExpression()
	if err := p.requireEnd(); err != nil {
		return err
	}
	if syntaxErr := p.syntaxError(); syntaxErr != nil {
		return syntaxErr
	}
	definition.BodySQL = p.sourceText(bodyStart, p.Position)

	if expression != nil {
		definition.ReturnType = p.inferMacroReturnType(expression)
	}
	definition.DataAccess = macroDataAccess(expression, builtins)
	return nil
}

// inferMacroReturnType infers the return type of a macro body from its top-level
// expression.
//
// Takes expression (ast.Expression) which is the parsed body expression to inspect.
//
// Returns sqlmodel.SQLType which is the inferred return type, defaulting to the unknown
// category when inference is unavailable.
func (p *parser) inferMacroReturnType(expression ast.Expression) sqlmodel.SQLType {
	switch typed := expression.(type) {
	case *ast.LiteralExpression:
		return inferLiteralType(typed.TypeName)
	case *ast.BinaryOpExpression:
		if typed.Operator == "||" {
			return sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar")
		}
		return p.inferMacroReturnType(typed.Left)
	case *ast.CastExpression:
		normalised := p.types.normalise(typed.TypeName)
		return normalised
	default:
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	}
}

// parseFunctionArgumentList parses a parenthesised list of function or macro argument
// declarations.
//
// Takes engine (typeNormaliser) which normalises argument type names.
//
// Returns []sqlmodel.FunctionArgument which lists the parsed arguments in declared order,
// or nil when no argument list is present.
// Returns error when any individual argument fails to parse.
func (p *parser) parseFunctionArgumentList(engine typeNormaliser) ([]sqlmodel.FunctionArgument, error) {
	if p.Current().Kind != tokenLeftParen {
		return nil, nil
	}
	p.Advance()

	var arguments []sqlmodel.FunctionArgument
	for !p.AtEnd() && p.Current().Kind != tokenRightParen {
		positionBefore := p.Position
		argument, argumentError := p.parseFunctionArgument(engine)
		if argumentError != nil {
			return nil, argumentError
		}
		arguments = append(arguments, argument)

		if p.Current().Kind == tokenComma {
			p.Advance()
		}

		if p.Position == positionBefore {
			return nil, p.syntaxErrorAt("malformed function argument list")
		}
	}
	if p.Current().Kind == tokenRightParen {
		p.Advance()
	}

	return arguments, nil
}

// parseFunctionArgument parses a single function or macro argument declaration, including
// optional mode keywords and default value.
//
// A macro parameter written `name := default` is optional and, like every untyped macro
// parameter, is recorded by its name in place of a type; DEFAULT marks a typed argument
// optional.
//
// Takes engine (typeNormaliser) which normalises the argument's type name.
//
// Returns sqlmodel.FunctionArgument which describes the parsed argument.
// Returns error which is always nil.
func (p *parser) parseFunctionArgument(engine typeNormaliser) (sqlmodel.FunctionArgument, error) {
	p.MatchKeyword("IN")
	p.MatchKeyword("OUT")
	p.MatchKeyword("INOUT")
	if p.MatchKeyword("VARIADIC") {
		p.lastArgumentWasVariadic = true
	}

	savedPosition := p.Position
	possibleName, _ := p.parseIdentifierOrKeyword()

	if possibleName != "" && p.matchMacroDefaultAssignment() {
		argument := sqlmodel.FunctionArgument{
			Name:       possibleName,
			Type:       untypedArgumentType(),
			IsOptional: true,
			NotNull:    false,
		}
		p.skipFunctionDefault()
		return argument, nil
	}

	if possibleName != "" && (p.Current().Kind == tokenComma || p.Current().Kind == tokenRightParen) {
		return sqlmodel.FunctionArgument{
			Name:       possibleName,
			Type:       untypedArgumentType(),
			IsOptional: false,
			NotNull:    false,
		}, nil
	}

	if p.Current().Kind == tokenIdentifier && !p.isDuckDBColumnConstraintKeyword() &&
		!p.IsKeyword(keywordDEFAULT, "COMMA") &&
		p.Current().Kind != tokenComma && p.Current().Kind != tokenRightParen {
		return p.parseTypedFunctionArgument(possibleName, engine), nil
	}

	p.Position = savedPosition
	return p.parseTypedFunctionArgument("", engine), nil
}

// parseTypedFunctionArgument parses a function argument's type and an optional DEFAULT,
// which makes the argument optional.
//
// Takes name (string) which is the argument's name, or empty when it has none.
// Takes engine (typeNormaliser) which normalises the argument's type name.
//
// Returns sqlmodel.FunctionArgument which describes the argument.
func (p *parser) parseTypedFunctionArgument(name string, engine typeNormaliser) sqlmodel.FunctionArgument {
	argumentType, arrayDimensions := p.parseColumnType(engine)
	argument := sqlmodel.FunctionArgument{
		Name:       name,
		Type:       functionArgumentArrayType(argumentType, arrayDimensions),
		IsOptional: false,
		NotNull:    false,
	}

	if p.MatchKeyword(keywordDEFAULT) {
		argument.IsOptional = true
		p.skipFunctionDefault()
	}
	return argument
}

// matchMacroDefaultAssignment consumes the `:=` that introduces a macro parameter's
// default value, which the tokeniser emits as a ":" operator followed by an "=" operator.
//
// Returns bool which is true when `:=` was consumed.
func (p *parser) matchMacroDefaultAssignment() bool {
	if p.Current().Kind != tokenOperator || p.Current().Value != ":=" {
		return false
	}
	p.Advance()
	return true
}

// skipFunctionDefault advances the cursor past a DEFAULT expression while honouring
// nested parentheses.
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

// parseDropFunction parses a DROP FUNCTION, DROP PROCEDURE, DROP MACRO or DROP MACRO
// TABLE statement.
//
// Returns *ast.CatalogueMutation which describes the drop-function mutation.
// Returns error when the keywords, qualified name, or argument list are malformed.
func (p *parser) parseDropFunction() (*ast.CatalogueMutation, error) {
	if _, err := p.ExpectKeyword(keywordDROP); err != nil {
		return nil, err
	}
	if p.MatchKeyword(keywordMACRO) {
		p.MatchKeyword(keywordTABLE)
	} else if _, err := p.ExpectKeyword("FUNCTION", "PROCEDURE"); err != nil {
		return nil, err
	}

	ifExists := p.matchIfExists()

	schema, functionName, err := p.parseSchemaQualifiedName()
	if err != nil {
		return nil, err
	}

	if err := p.skipParenthesisedIfPresent(); err != nil {
		return nil, err
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

// macroDataAccess works out a scalar macro's data access from the functions its body
// calls: it modifies data when a built-in it calls does, such as nextval; it is unknown
// when it calls a function that is not built in, such as another macro; and it is read
// only otherwise.
//
// Takes body (ast.Expression) which is the macro's body.
// Takes builtins (*ast.FunctionCatalogue) which holds the built-in functions.
//
// Returns sqlmodel.FunctionDataAccess which is the macro's data access.
func macroDataAccess(body ast.Expression, builtins *ast.FunctionCatalogue) sqlmodel.FunctionDataAccess {
	access := sqlmodel.DataAccessReadOnly
	for _, name := range calledFunctionNames(body) {
		overloads, builtin := builtins.Functions[name]
		if !builtin {
			if _, polymorphic := polymorphicResolvers[name]; polymorphic {
				continue
			}
			access = sqlmodel.DataAccessUnknown
			continue
		}
		for _, overload := range overloads {
			if overload.DataAccess == sqlmodel.DataAccessModifiesData {
				return sqlmodel.DataAccessModifiesData
			}
		}
	}
	return access
}

// calledFunctionNames lists the lower-cased names of the functions an expression calls,
// window functions and the calls inside lambda bodies included.
//
// Takes expression (ast.Expression) which is the expression to search.
//
// Returns []string which holds the names in the order met.
func calledFunctionNames(expression ast.Expression) []string {
	var names []string
	for node := range ast.Preorder(expression) {
		if call, isCall := node.(*ast.FunctionCallExpression); isCall && call != nil {
			names = append(names, strings.ToLower(call.FunctionName))
		}
	}
	return names
}

// inferLiteralType maps a literal type-name keyword to a structured SQL type.
//
// Takes typeName (string) which is the literal's syntactic type name.
//
// Returns sqlmodel.SQLType which is the structured form of typeName, defaulting to the
// unknown category for unrecognised names.
func inferLiteralType(typeName string) sqlmodel.SQLType {
	switch typeName {
	case "integer":
		return builtinTypeMap["integer"]
	case "text":
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar")
	case "numeric":
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "numeric")
	case "boolean":
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryBoolean, "bool")
	default:
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	}
}

// functionArgumentArrayType wraps a parsed scalar argument type in an array type once per
// array dimension declared on a CREATE FUNCTION or MACRO argument.
//
// Examples include text[] or text[][]. Function arguments carry their array-ness in the
// SQLType itself, unlike table columns which record it on Column.IsArray and
// Column.ArrayDimensions, so the overload resolver can tell a call to f(text) from a call
// to f(text[]). The element type is preserved verbatim, so a modified or qualified base
// type keeps its identity. A zero dimension count returns the element type unchanged.
//
// Takes elementType (sqlmodel.SQLType) which is the scalar (non-array) argument type.
// Takes dimensions (int) which is the number of trailing [] suffixes on the argument.
//
// Returns sqlmodel.SQLType which is elementType wrapped in that many array layers.
func functionArgumentArrayType(elementType sqlmodel.SQLType, dimensions int) sqlmodel.SQLType {
	wrapped := elementType
	for range dimensions {
		wrapped = newArrayType(wrapped)
	}
	return wrapped
}

// untypedArgumentType returns the type of a macro parameter declared without one, which
// accepts an argument of any type.
//
// Returns sqlmodel.SQLType which is the unnamed unknown type.
func untypedArgumentType() sqlmodel.SQLType {
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
}

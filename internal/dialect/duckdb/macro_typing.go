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
	"context"
	"reflect"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// maxMacroTypingDepth bounds how deeply typing a macro call follows the macros its body
	// calls in turn.
	maxMacroTypingDepth = 8

	// maxMacroTypingCalls bounds how many macro calls one resolution types in all, across
	// every body it follows, so macros that each call many others cannot multiply the work;
	// once spent, every further call is left untyped.
	maxMacroTypingCalls = 256
)

// macroTyping is what one resolution shares across the macro bodies it types: the bodies
// parsed so far, the types already worked out for each macro and argument types, the
// macros being typed, and the calls typed so far.
type macroTyping struct {
	// engine supplies the type normaliser, the operator typing and the built-in functions.
	engine *Dialect

	// catalogue holds the macros a body may call.
	catalogue *sqlmodel.Catalogue

	// bodies holds each macro's parsed body, or a nil expression for a body that does not
	// read as one expression.
	bodies map[*sqlmodel.Function]ast.Expression

	// typed holds the types already worked out for each macro, one per list of argument
	// types it was called with.
	typed map[*sqlmodel.Function][]typedMacroCall

	// inProgress holds the macros whose bodies are being typed, so a macro that calls
	// itself, directly or through others, is left untyped instead of recursing.
	inProgress map[*sqlmodel.Function]bool

	// calls counts the macro calls typed so far, against maxMacroTypingCalls.
	calls int
}

// knownResult returns the type a macro's body was given before for the same argument
// types.
//
// Takes macro (*sqlmodel.Function) which is the macro.
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call's argument types.
//
// Returns typedExpression which is the type found.
// Returns bool which is false when the macro was not typed for these argument types.
func (typing *macroTyping) knownResult(macro *sqlmodel.Function, argumentTypes []sqlmodel.SQLType) (typedExpression, bool) {
	calls := typing.typed[macro]
	for index := range calls {
		if reflect.DeepEqual(calls[index].argumentTypes, argumentTypes) {
			return calls[index].result, true
		}
	}
	return typedExpression{}, false
}

// body returns a macro's parsed body, parsing it the first time the resolution asks.
//
// Takes macro (*sqlmodel.Function) which is the macro.
//
// Returns ast.Expression which is the body, or nil when it does not read as one
// expression.
func (typing *macroTyping) body(ctx context.Context, macro *sqlmodel.Function) ast.Expression {
	if body, parsed := typing.bodies[macro]; parsed {
		return body
	}
	body, read := typing.engine.parseMacroBody(ctx, macro.BodySQL)
	if !read {
		body = nil
	}
	typing.bodies[macro] = body
	return body
}

// typedMacroCall is the type a macro's body was given for one list of argument types.
type typedMacroCall struct {
	// argumentTypes are the argument types the macro was called with.
	argumentTypes []sqlmodel.SQLType

	// result is the type of the body for them.
	result typedExpression
}

// macroTyper resolves a scalar macro using the types supplied at each call.
//
// Expressions whose type cannot be inferred remain unknown.
type macroTyper struct {
	// typing is the state the resolution shares across every body it types.
	typing *macroTyping

	// parameters maps each parameter's lower-cased name to the type of its argument.
	parameters map[string]sqlmodel.SQLType

	// depth is how many macro calls the typing has followed.
	depth int
}

// typeCall types a call of a scalar macro by typing its body against the call's argument
// types.
//
// A macro already typed for the same argument types gives the type it was given then, and
// a macro whose body is being typed, because it calls itself directly or through others,
// is left untyped, as is every call once the resolution has typed maxMacroTypingCalls of
// them.
//
// Takes name (string) which is the macro's name.
// Takes schema (string) which is the call's schema qualifier, or empty.
// Takes argumentTypes ([]sqlmodel.SQLType) which are the types of the call's arguments.
//
// Returns *sqlmodel.Function which is the macro.
// Returns typedExpression which is the type of the macro's body for the call.
// Returns bool which is false when no scalar macro with a body takes the arguments, the
// macro is being typed already, the macro calls go deeper than maxMacroTypingDepth, or
// the resolution has typed its budget of calls.
func (typer *macroTyper) typeCall(
	ctx context.Context,
	name string,
	schema string,
	argumentTypes []sqlmodel.SQLType,
) (*sqlmodel.Function, typedExpression, bool) {
	typing := typer.typing
	if typer.depth >= maxMacroTypingDepth {
		return nil, typedExpression{}, false
	}
	macro := findScalarMacro(typing.catalogue, name, schema, len(argumentTypes))
	if macro == nil || typing.inProgress[macro] {
		return nil, typedExpression{}, false
	}
	if result, known := typing.knownResult(macro, argumentTypes); known {
		return macro, result, true
	}
	if typing.calls >= maxMacroTypingCalls {
		return nil, typedExpression{}, false
	}
	typing.calls++
	body := typing.body(ctx, macro)
	if body == nil {
		return nil, typedExpression{}, false
	}
	parameters := make(map[string]sqlmodel.SQLType, len(macro.Arguments))
	for index := range macro.Arguments {
		if index < len(argumentTypes) {
			parameters[strings.ToLower(macro.Arguments[index].Name)] = argumentTypes[index]
		}
	}
	typing.inProgress[macro] = true
	inner := &macroTyper{typing: typing, parameters: parameters, depth: typer.depth + 1}
	result := inner.typeOf(ctx, body)
	delete(typing.inProgress, macro)
	typing.typed[macro] = append(typing.typed[macro], typedMacroCall{argumentTypes: slices.Clone(argumentTypes), result: result})
	return macro, result, true
}

// typeOf types one expression of a macro body.
//
// Takes expression (ast.Expression) which is the expression.
//
// Returns typedExpression which is its type and strictness.
func (typer *macroTyper) typeOf(ctx context.Context, expression ast.Expression) typedExpression {
	switch typed := expression.(type) {
	case *ast.ColumnRefExpression:
		return typer.typeOfColumn(typed)
	case *ast.LiteralExpression:
		return typedExpression{sqlType: typer.typing.engine.typeResolver.normalise(typed.TypeName), strict: true}
	case *ast.CastExpression:
		inner := typer.typeOf(ctx, typed.Inner)
		return typedExpression{sqlType: typer.typing.engine.typeResolver.normalise(typed.TypeName), strict: inner.strict}
	case *ast.BinaryOpExpression:
		return typer.typeOfBinary(ctx, typed)
	case *ast.UnaryOpExpression:
		return typer.typeOfUnary(ctx, typed)
	case *ast.FunctionCallExpression:
		return typer.typeOfCall(ctx, typed)
	case *ast.ComparisonExpression:
		left, right := typer.typeOf(ctx, typed.Left), typer.typeOf(ctx, typed.Right)
		return typedExpression{sqlType: coreTypes.Boolean, strict: left.strict && right.strict}
	case *ast.LogicalOpExpression, *ast.IsNullExpression, *ast.InListExpression, *ast.BetweenExpression,
		*ast.ExistsExpression:
		return typedExpression{sqlType: coreTypes.Boolean, strict: false}
	case *ast.CoalesceExpression:
		return typer.firstKnown(ctx, typed.Arguments)
	case *ast.CaseWhenExpression:
		return typer.typeOfCase(ctx, typed)
	case *ast.StructExpression:
		return typer.typeOfStruct(ctx, typed)
	default:
		return typedExpression{sqlType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), strict: false}
	}
}

// typeOfStruct types a struct constructor in a macro body as the struct it builds: a
// field for each name, in order, of the type of the value at the same position, each of
// which may hold NULL. The struct itself is never NULL, so it is not strict.
//
// Takes structure (*ast.StructExpression) which is the constructor.
//
// Returns typedExpression which is its type and strictness.
func (typer *macroTyper) typeOfStruct(ctx context.Context, structure *ast.StructExpression) typedExpression {
	structType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "struct")
	structType.StructFields = make([]sqlmodel.StructField, 0, len(structure.Names))
	for index, value := range structure.Values {
		if index >= len(structure.Names) {
			break
		}
		fieldType := typer.typeOf(ctx, value).sqlType
		fieldType.Nullable = true
		structType.StructFields = append(structType.StructFields,
			sqlmodel.StructField{Name: structure.Names[index], SQLType: fieldType, Positional: false})
	}
	return typedExpression{sqlType: structType, strict: false}
}

// typeOfColumn types a name in a macro body: a parameter takes its argument's type, and
// any other name is unknown.
//
// Takes column (*ast.ColumnRefExpression) which is the name.
//
// Returns typedExpression which is its type and strictness.
func (typer *macroTyper) typeOfColumn(column *ast.ColumnRefExpression) typedExpression {
	if column.TableAlias == "" {
		if argumentType, isParameter := typer.parameters[strings.ToLower(column.ColumnName)]; isParameter {
			return typedExpression{sqlType: argumentType, strict: true}
		}
	}
	return typedExpression{sqlType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), strict: false}
}

// typeOfBinary types a binary operator: by DuckDB's operator rules where they fix the
// result, and otherwise by the wider of two numeric operands.
//
// Takes expression (*ast.BinaryOpExpression) which is the operator.
//
// Returns typedExpression which is its type and strictness.
func (typer *macroTyper) typeOfBinary(ctx context.Context, expression *ast.BinaryOpExpression) typedExpression {
	left, right := typer.typeOf(ctx, expression.Left), typer.typeOf(ctx, expression.Right)
	strict := left.strict && right.strict
	unknown := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	if left.sqlType.Category == sqlmodel.TypeCategoryUnknown || right.sqlType.Category == sqlmodel.TypeCategoryUnknown {
		return typedExpression{sqlType: unknown, strict: false}
	}
	if result, behaviour, typedByRule := binaryResult(expression.Operator, left.sqlType, right.sqlType); typedByRule {
		return typedExpression{sqlType: result, strict: strict && behaviour == sqlmodel.FunctionNullableReturnsNullOnNull}
	}
	if !isNumeric(left.sqlType) || !isNumeric(right.sqlType) {
		return typedExpression{sqlType: unknown, strict: false}
	}
	return typedExpression{sqlType: typer.widerNumeric(left.sqlType, right.sqlType), strict: strict}
}

// widerNumeric returns the type DuckDB gives arithmetic over two numeric operands: the
// operand of the wider category, a decimal or a float over an integer, and the promoted
// type within one category.
//
// Takes left (sqlmodel.SQLType) which is the left operand's type.
// Takes right (sqlmodel.SQLType) which is the right operand's type.
//
// Returns sqlmodel.SQLType which is the result's type.
func (typer *macroTyper) widerNumeric(left, right sqlmodel.SQLType) sqlmodel.SQLType {
	if left.Category == right.Category {
		return typer.typing.engine.PromoteType(left, right)
	}
	if numericRank(right) > numericRank(left) {
		return right
	}
	return left
}

// typeOfUnary types a unary operator: NOT as a boolean, and a sign as its operand.
//
// Takes expression (*ast.UnaryOpExpression) which is the operator.
//
// Returns typedExpression which is its type and strictness.
func (typer *macroTyper) typeOfUnary(ctx context.Context, expression *ast.UnaryOpExpression) typedExpression {
	operand := typer.typeOf(ctx, expression.Operand)
	if result, _, typedByRule := unaryResult(expression.Operator); typedByRule {
		return typedExpression{sqlType: result, strict: operand.strict}
	}
	return operand
}

// typeOfCall types a call in a macro body: of a built-in whose return type follows from
// its arguments, of a built-in by the overload its arguments match, or of another macro
// by its own body.
//
// Takes call (*ast.FunctionCallExpression) which is the call.
//
// Returns typedExpression which is its type and strictness.
func (typer *macroTyper) typeOfCall(ctx context.Context, call *ast.FunctionCallExpression) typedExpression {
	unknown := typedExpression{sqlType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), strict: false}
	argumentTypes := make([]sqlmodel.SQLType, len(call.Arguments))
	strict := true
	for index, argument := range call.Arguments {
		typed := typer.typeOf(ctx, argument)
		argumentTypes[index] = typed.sqlType
		strict = strict && typed.strict
	}
	name := strings.ToLower(call.FunctionName)
	if resolve, polymorphic := polymorphicResolvers[name]; polymorphic {
		resolution, err := resolve(argumentTypes)
		if err != nil || resolution == nil {
			return unknown
		}
		return typedExpression{sqlType: resolution.ReturnType,
			strict: strict && resolution.NullableBehaviour == sqlmodel.FunctionNullableReturnsNullOnNull}
	}
	if overload := builtinOverload(typer.typing.engine.functions, name, len(argumentTypes)); overload != nil {
		returnType := overload.ReturnType
		if position := overload.ReturnTypeFromArgument; position > 0 && position <= len(argumentTypes) {
			returnType = argumentTypes[position-1]
		}
		return typedExpression{sqlType: returnType,
			strict: strict && overload.NullableBehaviour == sqlmodel.FunctionNullableReturnsNullOnNull}
	}
	if _, typed, found := typer.typeCall(ctx, name, call.Schema, argumentTypes); found {
		return typedExpression{sqlType: typed.sqlType, strict: strict && typed.strict}
	}
	return unknown
}

// typeOfCase types a CASE expression as its first result of known type. A CASE may give
// NULL whatever its arguments, so it is never strict.
//
// Takes expression (*ast.CaseWhenExpression) which is the CASE expression.
//
// Returns typedExpression which is its type and strictness.
func (typer *macroTyper) typeOfCase(ctx context.Context, expression *ast.CaseWhenExpression) typedExpression {
	results := make([]ast.Expression, 0, len(expression.Branches)+1)
	for _, branch := range expression.Branches {
		results = append(results, branch.Result)
	}
	results = append(results, expression.ElseResult)
	return typer.firstKnown(ctx, results)
}

// firstKnown types a list of alternatives, such as the arguments of COALESCE, as the
// first of known type. The result is never strict.
//
// Takes alternatives ([]ast.Expression) which are the alternatives.
//
// Returns typedExpression which is the type of the first alternative of known type.
func (typer *macroTyper) firstKnown(ctx context.Context, alternatives []ast.Expression) typedExpression {
	for _, alternative := range alternatives {
		if alternative == nil {
			continue
		}
		if typed := typer.typeOf(ctx, alternative); typed.sqlType.Category != sqlmodel.TypeCategoryUnknown {
			return typedExpression{sqlType: typed.sqlType, strict: false}
		}
	}
	return typedExpression{sqlType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), strict: false}
}

// typedExpression is the type of a macro body's expression, and whether it is NULL only
// when an argument is.
type typedExpression struct {
	// sqlType is the expression's type, of unknown category when it cannot be typed.
	sqlType sqlmodel.SQLType

	// strict is true when the expression is NULL only when an argument the macro was given
	// is NULL.
	strict bool
}

// resolveMacroCall types a scalar macro call by resolving its body with the argument
// types. Macros with unknown data access use their catalogue signature.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the macro.
// Takes name (string) which is the macro's name.
// Takes schema (string) which is the call's schema qualifier, or empty.
// Takes argumentTypes ([]sqlmodel.SQLType) which are the types of the call's arguments.
//
// Returns *ast.FunctionResolution which types the call, or nil when the call is not one
// of a scalar macro whose body types it.
func (engine *Dialect) resolveMacroCall(
	ctx context.Context,
	catalogue *sqlmodel.Catalogue,
	name string,
	schema string,
	argumentTypes []sqlmodel.SQLType,
) *ast.FunctionResolution {
	typing := &macroTyping{
		engine:     engine,
		catalogue:  catalogue,
		bodies:     make(map[*sqlmodel.Function]ast.Expression),
		typed:      make(map[*sqlmodel.Function][]typedMacroCall),
		inProgress: make(map[*sqlmodel.Function]bool),
		calls:      0,
	}
	typer := &macroTyper{typing: typing, parameters: nil, depth: 0}
	macro, typed, found := typer.typeCall(ctx, name, schema, argumentTypes)
	if !found || typed.sqlType.Category == sqlmodel.TypeCategoryUnknown ||
		macro.DataAccess == sqlmodel.DataAccessUnknown {
		return nil
	}
	nullable := sqlmodel.FunctionNullableCalledOnNull
	if typed.strict {
		nullable = sqlmodel.FunctionNullableReturnsNullOnNull
	}
	return &ast.FunctionResolution{
		ReturnType:        typed.sqlType,
		NullableBehaviour: nullable,
		NullingArgument:   0,
		DataAccess:        macro.DataAccess,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}
}

// parseMacroBody parses the text of a scalar macro's body as one expression. The walk
// stops with the context's cause once ctx is done or the walk stops advancing, as
// dialectkit.WalkGuard describes.
//
// Takes body (string) which is the body's text.
//
// Returns ast.Expression which is the body.
// Returns bool which is false when the body does not read as one expression or holds more
// tokens than the dialect's per-statement budget.
func (engine *Dialect) parseMacroBody(ctx context.Context, body string) (ast.Expression, bool) {
	statements, err := engine.lexStatements(ctx, body)
	if err != nil || len(statements) != 1 || statements[0].OverBudget {
		return nil, false
	}
	tokens := statements[0].Tokens
	statementParser := engine.newBodyParser(dialectkit.WalkGuard(ctx, len(tokens)), tokens, body)
	expression := statementParser.parseExpression()
	if statementParser.syntaxError() != nil || !statementParser.AtEnd() || expression == nil {
		return nil, false
	}
	return expression, true
}

// findScalarMacro finds the scalar macro a call names that takes as many arguments as the
// call gives: in the call's schema, or, for an unqualified call, in the first schema on
// the catalogue's search path that holds one.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the macros; its schemas and functions
// are looked up under the keys it files them by.
// Takes name (string) which is the macro's name.
// Takes schema (string) which is the call's schema qualifier, or empty.
// Takes argumentCount (int) which is how many arguments the call gives.
//
// Returns *sqlmodel.Function which is the macro, or nil when there is none.
func findScalarMacro(catalogue *sqlmodel.Catalogue, name string, schema string, argumentCount int) *sqlmodel.Function {
	for _, overloads := range catalogue.FunctionsOnPath(schema, name) {
		for _, function := range overloads {
			if function != nil && function.BodySQL != "" && !function.ReturnsSet &&
				argumentCount >= function.MinArguments && argumentCount <= len(function.Arguments) {
				return function
			}
		}
	}
	return nil
}

// numericRank orders the numeric categories by how DuckDB widens arithmetic over them: an
// integer, then a decimal, then a float.
//
// Takes sqlType (sqlmodel.SQLType) which is a numeric type.
//
// Returns int which is the category's rank.
func numericRank(sqlType sqlmodel.SQLType) int {
	switch sqlType.Category {
	case sqlmodel.TypeCategoryFloat:
		return 2
	case sqlmodel.TypeCategoryDecimal:
		return 1
	default:
		return 0
	}
}

// builtinOverload returns the first overload of a built-in that takes as many arguments
// as a call gives.
//
// Takes functions (*ast.FunctionCatalogue) which holds the built-ins.
// Takes name (string) which is the lower-cased function name.
// Takes argumentCount (int) which is how many arguments the call gives.
//
// Returns *sqlmodel.Function which is the overload, or nil when there is none.
func builtinOverload(functions *ast.FunctionCatalogue, name string, argumentCount int) *sqlmodel.Function {
	if functions == nil {
		return nil
	}
	for _, overload := range functions.Functions[name] {
		if overload == nil {
			continue
		}
		if argumentCount >= overload.MinArguments && (overload.IsVariadic || argumentCount <= len(overload.Arguments)) {
			return overload
		}
	}
	return nil
}

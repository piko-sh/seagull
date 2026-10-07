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

package resolve

import (
	"context"
	"slices"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// lambdaParameter is a parameter of a lambda whose body is being typed, with the value it
// stands for: an element of the list the lambda is applied to.
type lambdaParameter struct {
	// name is the parameter's name as the lambda declares it.
	name string

	// sqlType is the type of the value the parameter stands for.
	sqlType sqlmodel.SQLType

	// nullable reports whether that value may be NULL.
	nullable bool
}

// listElement is the element of a list a call passes, which a lambda the call also passes
// is applied to.
type listElement struct {
	// sqlType is the element's type, or unknown when the list does not say.
	sqlType sqlmodel.SQLType

	// nullable reports whether an element may be NULL.
	nullable bool
}

// resolveLambdaExpression types a lambda from its body.
//
// Parameters use the dialect's bindings or the call's collection element types. They
// shadow columns only while the lambda body is resolved.
//
// Takes expression (*ast.LambdaExpression) which specifies the lambda to resolve.
// Takes scope (*ScopeChain) which specifies the scope chain for resolving the body.
// Takes dataModifying (*bool) which specifies a flag set to true if the body modifies
// data.
//
// Returns sqlmodel.SQLType which holds the type of the lambda body.
// Returns bool which indicates whether the result is nullable.
// Returns error which indicates a resolution failure for the body expression.
func (r *TypeResolver) resolveLambdaExpression(
	ctx context.Context,
	expression *ast.LambdaExpression,
	scope *ScopeChain,
	dataModifying *bool,
) (sqlmodel.SQLType, bool, error) {
	elements := r.offeredElements
	r.offeredElements = nil
	if expression.Body == nil {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true, nil
	}
	parameters := make([]lambdaParameter, len(expression.Parameters))
	next := 0
	for index, name := range expression.Parameters {
		parameters[index] = lambdaParameter{name: name, sqlType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), nullable: true}
		if binding := lambdaBinding(expression, index); binding != nil {
			parameters[index].sqlType, parameters[index].nullable, _ = r.ResolveExpressionType(ctx, binding, scope, nil)
			continue
		}
		if next < len(elements) {
			parameters[index].sqlType, parameters[index].nullable = elements[next].sqlType, elements[next].nullable
			next++
		}
	}
	enclosing := len(r.lambdaParameters)
	r.lambdaParameters = append(r.lambdaParameters, parameters...)
	defer func() { r.lambdaParameters = r.lambdaParameters[:enclosing] }()
	return r.resolveExpressionType(ctx, expression.Body, scope, dataModifying)
}

// boundLambdaParameter returns the parameter of the innermost lambda being typed that a
// name refers to, compared under the dialect's identifier rules.
//
// Takes scope (*ScopeChain) which supplies the identifier rules.
// Takes name (string) which is the name.
//
// Returns lambdaParameter which is the parameter.
// Returns bool which is true when the name refers to one.
func (r *TypeResolver) boundLambdaParameter(scope *ScopeChain, name string) (lambdaParameter, bool) {
	if name == "" {
		return lambdaParameter{}, false
	}
	identifiers := scope.Rules().Identifiers
	for index := range slices.Backward(r.lambdaParameters) {
		declared := r.lambdaParameters[index].name
		if identifiers.Equal(declared, name) || identifiers.Equal(identifiers.Fold(declared, false), name) {
			return r.lambdaParameters[index], true
		}
	}
	return lambdaParameter{}, false
}

// lambdaArgument returns the lambda an argument of a call passes, directly or by name.
//
// Takes argument (ast.Expression) which is the argument.
//
// Returns *ast.LambdaExpression which is the lambda, or nil when the argument is none.
func lambdaArgument(argument ast.Expression) *ast.LambdaExpression {
	if named, isNamed := argument.(*ast.NamedArgumentExpression); isNamed {
		argument = named.Value
	}
	lambda, isLambda := argument.(*ast.LambdaExpression)
	if !isLambda || lambda == nil {
		return nil
	}
	return lambda
}

// listElements returns the elements of the lists among a call's arguments, in argument
// order, which the parameters of a lambda the call passes stand for in turn: the first
// parameter for an element of the first list, the second for one of the second.
//
// Takes arguments ([]callArgument) which are the call's typed arguments; a lambda's slot
// is skipped.
// Takes lambdas ([]bool) which marks, parallel to arguments, the lambda arguments.
//
// Returns []listElement which holds the elements.
func listElements(arguments []callArgument, lambdas []bool) []listElement {
	var elements []listElement
	for index := range arguments {
		if lambdas[index] || arguments[index].sqlType.Category != sqlmodel.TypeCategoryArray {
			continue
		}
		element := listElement{sqlType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), nullable: true}
		if elementType := arguments[index].sqlType.ElementType; elementType != nil {
			element = listElement{sqlType: *elementType, nullable: elementType.Nullable}
		}
		elements = append(elements, element)
	}
	return elements
}

// lambdaBinding returns the value the dialect binds a lambda's parameter to.
//
// Takes expression (*ast.LambdaExpression) which is the lambda.
// Takes index (int) which is the parameter's position.
//
// Returns ast.Expression which is the binding, or nil when the parameter has none.
func lambdaBinding(expression *ast.LambdaExpression, index int) ast.Expression {
	if index >= len(expression.Bindings) {
		return nil
	}
	return expression.Bindings[index]
}

// lambdaParameterField resolves a lambda value's field type and nullability.
//
// Unknown value types remain unknown; missing fields on declared structs produce an
// unknown-column error.
//
// Takes parameter (lambdaParameter) which is the parameter.
// Takes expression (*ast.ColumnRefExpression) which names the field, qualified by the
// parameter.
//
// Returns sqlmodel.SQLType which holds the field's type.
// Returns bool which indicates whether the field may be NULL.
// Returns error which is the unknown-column error, or nil.
func lambdaParameterField(parameter lambdaParameter, expression *ast.ColumnRefExpression) (sqlmodel.SQLType, bool, error) {
	fields := parameter.sqlType.StructFields
	if len(fields) == 0 {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true, nil
	}
	names := make([]string, len(fields))
	for index := range fields {
		if fields[index].Name == expression.ColumnName {
			return fields[index].SQLType, parameter.nullable || fields[index].SQLType.Nullable, nil
		}
		names[index] = fields[index].Name
	}
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true,
		newError(diag.CodeUnknownColumn, "unknown field %q", expression.ColumnName).
			withSuggestion(diagbuild.ClosestSuggestion(expression.ColumnName, names))
}

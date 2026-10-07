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
	"strings"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/diagbuild"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// ResolveExpressionType infers the SQL type and nullability of a typed expression from
// the dialect. Dispatches on the concrete expression type via a type switch for
// compile-time safety.
//
// An operand that cannot be typed, such as an unknown column passed to a function or
// compared in a predicate, does not stop the expression typing; the problem is not
// reported here, and the error returned is the expression's own, such as an unknown
// function it calls. A select list reports its operands' problems through
// ResolveProjection, the one place an unknown column inside a select-list expression is
// reported.
//
// When the dialect's resolver is interrupted, as by the context being cancelled, the
// interruption is returned as the error; it matches context.Canceled or
// context.DeadlineExceeded and is no problem with the query.
//
// Takes expression (ast.Expression) which specifies the expression to resolve.
// Takes scope (*ScopeChain) which specifies the scope chain for column and CTE lookups.
// Takes dataModifying (*bool) which specifies a flag set to true if the expression
// modifies data.
//
// Returns sqlmodel.SQLType which holds the inferred SQL type of the expression.
// Returns bool which indicates whether the result is nullable.
// Returns error which indicates a resolution failure or an interruption.
func (r *TypeResolver) ResolveExpressionType(
	ctx context.Context,
	expression ast.Expression,
	scope *ScopeChain,
	dataModifying *bool,
) (sqlmodel.SQLType, bool, error) {
	enclosing := r.notices
	defer func() { r.notices = enclosing }()
	return r.resolveExpressionType(ctx, expression, scope, dataModifying)
}

// resolveExpressionType is ResolveExpressionType keeping the problems of the operands it
// types leniently as notices, each where the dialect located it, for the select list to
// report.
//
// Takes expression (ast.Expression) which specifies the expression to resolve.
// Takes scope (*ScopeChain) which specifies the scope chain for column and CTE lookups.
// Takes dataModifying (*bool) which specifies a flag set to true if the expression
// modifies data.
//
// Returns sqlmodel.SQLType which holds the inferred SQL type of the expression.
// Returns bool which indicates whether the result is nullable.
// Returns error which indicates a resolution failure.
func (r *TypeResolver) resolveExpressionType(
	ctx context.Context,
	expression ast.Expression,
	scope *ScopeChain,
	dataModifying *bool,
) (sqlmodel.SQLType, bool, error) {
	if expression == nil {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true, nil
	}
	if argument, typed := r.typedArguments[expression]; typed {
		return argument.sqlType, argument.nullable, nil
	}

	if r.expressionDepth >= MaxExpressionResolveDepth {
		r.noteExpressionDepthLimit()
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true, nil
	}
	r.expressionDepth++
	defer func() { r.expressionDepth-- }()

	feature := ast.RequiredFeature(expression, r.operatorFeatures)
	if feature != 0 && !r.expressions.Has(feature) {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true,
			newError(diag.CodeExpressionTypeError, "unsupported expression: %s", feature.String())
	}

	if isBooleanExpression(expression) {
		return r.resolvePredicate(ctx, expression, scope, dataModifying)
	}

	switch typed := expression.(type) {
	case *ast.ColumnRefExpression:
		return r.resolveColumnRefExpression(typed, scope)
	case *ast.FunctionCallExpression:
		return r.resolveFunctionCallExpression(ctx, typed, scope, dataModifying)
	case *ast.CoalesceExpression:
		return r.resolveCoalesceExpression(ctx, typed, scope, dataModifying)
	case *ast.CastExpression:
		return r.resolveCastExpression(ctx, typed, scope, dataModifying)
	case *ast.LiteralExpression:
		return r.resolveLiteralExpression(typed)
	case *ast.BinaryOpExpression:
		return r.resolveBinaryOpExpression(ctx, typed, scope, dataModifying)
	case *ast.UnaryOpExpression:
		return r.resolveUnaryOpExpression(ctx, typed, scope, dataModifying)
	case *ast.CaseWhenExpression:
		return r.resolveCaseWhenExpression(ctx, typed, scope, dataModifying)
	case *ast.WindowFunctionExpression:
		return r.resolveWindowFunctionExpression(ctx, typed, scope, dataModifying)
	case *ast.ScalarSubqueryExpression:
		return r.resolveScalarSubqueryExpression(ctx, typed, scope, dataModifying)
	case *ast.RowValueExpression:
		return r.resolveRowValueExpression(ctx, typed, scope, dataModifying)
	default:
		return r.resolveAccessExpression(ctx, expression, scope, dataModifying)
	}
}

// resolveRowValueExpression types a row constructor: its elements, for their problems,
// and the row as a value of unknown type, NULL where an element that is no parameter may
// be, as a comparison of rows can be.
//
// Takes expression (*ast.RowValueExpression) which is the row.
// Takes scope (*ScopeChain) which resolves its elements' names.
// Takes dataModifying (*bool) which is set to true if an element modifies data.
//
// Returns sqlmodel.SQLType which is of unknown type.
// Returns bool which is true when an element may be NULL.
// Returns error which is always nil: an element's problem is a notice.
func (r *TypeResolver) resolveRowValueExpression(
	ctx context.Context,
	expression *ast.RowValueExpression,
	scope *ScopeChain,
	dataModifying *bool,
) (sqlmodel.SQLType, bool, error) {
	nullable := false
	if expression != nil {
		for _, element := range expression.Elements {
			_, elementNullable, err := r.resolveExpressionType(ctx, element, scope, dataModifying)
			r.noteOperandProblem(err)
			if _, isParameter := parameterStoodFor(element, 0); elementNullable && !isParameter {
				nullable = true
			}
		}
	}
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), nullable, nil
}

// noteExpressionDepthLimit records that an expression nests more deeply than the resolver
// follows, so the part beyond the limit was typed as unknown. The notice is reported once
// however often the limit is reached, as takeNotices keeps one of each.
func (r *TypeResolver) noteExpressionDepthLimit() {
	r.notices = append(r.notices, depthLimitError(
		"an expression nests more than %d levels deep, so the part beyond the limit was typed as unknown",
		MaxExpressionResolveDepth))
}

// noteOperandProblem keeps the problem found typing an operand that the expression
// holding it still typed, such as an unknown column passed to a function, so the select
// list reports it where it is.
//
// Duplicate problems are reported once by takeNotices. Interruption errors are excluded
// because they do not indicate a problem with the query.
//
// Takes err (error) which is the operand's failure; nil notes nothing.
func (r *TypeResolver) noteOperandProblem(err error) {
	if err == nil || interrupted(err) {
		return
	}
	r.notices = append(r.notices, asError(err))
}

// resolveAccessExpression types the expressions that reach into another value or carry
// one: an array subscript, a lambda body, a struct field access, a struct constructor,
// and a named argument, which is typed by its value. Any other expression is of unknown
// type.
//
// Takes expression (ast.Expression) which specifies the expression to resolve.
// Takes scope (*ScopeChain) which specifies the scope chain for lookups.
// Takes dataModifying (*bool) which specifies a flag set to true if the expression
// modifies data.
//
// Returns sqlmodel.SQLType which holds the inferred SQL type of the expression.
// Returns bool which indicates whether the result is nullable.
// Returns error which indicates a resolution failure.
func (r *TypeResolver) resolveAccessExpression(
	ctx context.Context,
	expression ast.Expression,
	scope *ScopeChain,
	dataModifying *bool,
) (sqlmodel.SQLType, bool, error) {
	switch typed := expression.(type) {
	case *ast.ArraySubscriptExpression:
		return r.resolveArraySubscriptExpression(ctx, typed, scope, dataModifying)
	case *ast.LambdaExpression:
		return r.resolveLambdaExpression(ctx, typed, scope, dataModifying)
	case *ast.StructFieldAccessExpression:
		return r.resolveStructFieldAccessExpression(ctx, typed, scope, dataModifying)
	case *ast.StructExpression:
		return r.resolveStructExpression(ctx, typed, scope, dataModifying)
	case *ast.NamedArgumentExpression:
		return r.resolveExpressionType(ctx, typed.Value, scope, dataModifying)
	default:
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true, nil
	}
}

// resolveColumnRefExpression resolves a column reference's type and nullability.
//
// Lambda parameters take precedence within their body. Where supported, a relation name
// can also refer to its current row.
//
// Takes expression (*ast.ColumnRefExpression) which specifies the column reference to
// resolve.
// Takes scope (*ScopeChain) which specifies the scope chain for column lookups.
//
// Returns sqlmodel.SQLType which holds the column's SQL type.
// Returns bool which indicates whether the column is nullable.
// Returns error which indicates a scope resolution failure.
func (r *TypeResolver) resolveColumnRefExpression(
	expression *ast.ColumnRefExpression,
	scope *ScopeChain,
) (sqlmodel.SQLType, bool, error) {
	if expression.TableAlias == "" {
		if parameter, bound := r.boundLambdaParameter(scope, expression.ColumnName); bound {
			return parameter.sqlType, parameter.nullable, nil
		}
	} else if parameter, bound := r.boundLambdaParameter(scope, expression.TableAlias); bound {
		sqlType, nullable, err := lambdaParameterField(parameter, expression)
		return sqlType, nullable, locatedAt(err, expression.Span)
	}
	column, _, err := scope.ResolveColumn(expression.TableAlias, expression.ColumnName)
	if err != nil {
		if row, _, isRow := rowValueInstead(scope, expression.TableAlias, expression.ColumnName, err); isRow {
			return row.SQLType, row.Nullable, nil
		}
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true, locatedAt(err, expression.Span)
	}
	if column == nil {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true,
			newError(diag.CodeInternalNilGuard, "nil column resolved for %s", expression.ColumnName)
	}
	return column.SQLType, column.Nullable, nil
}

// resolveFunctionCallExpression resolves a function call by performing overload
// resolution against the function resolver and inferring nullability from the function's
// nullable behaviour and argument nullability.
//
// Problems in arguments and FILTER conditions are kept as notices. A problem in a call
// that still resolves is also kept, with its location set to the call.
//
// Takes expression (*ast.FunctionCallExpression) which specifies the function call to
// resolve.
// Takes scope (*ScopeChain) which specifies the scope chain for resolving argument
// expressions.
// Takes dataModifying (*bool) which specifies a flag set to true if the function modifies
// data; an interrupted resolution leaves it as it is.
//
// Returns sqlmodel.SQLType which holds the function's return type.
// Returns bool which indicates whether the result is nullable.
// Returns error which indicates a resolution or overload failure, or the interruption
// that stopped the dialect's resolver.
func (r *TypeResolver) resolveFunctionCallExpression(
	ctx context.Context,
	expression *ast.FunctionCallExpression,
	scope *ScopeChain,
	dataModifying *bool,
) (sqlmodel.SQLType, bool, error) {
	arguments := r.resolveCallArguments(ctx, expression.Arguments, scope, dataModifying)
	r.noteSharedArgumentParameters(expression, arguments)
	r.noteArityTypedParameters(expression, arguments)
	if expression.FilterExpression != nil {
		_, _, filterError := r.resolveExpressionType(ctx, expression.FilterExpression, scope, dataModifying)
		r.noteOperandProblem(filterError)
	}
	match, resolveError, err := r.functionResolver.resolveCall(ctx, expression.FunctionName, expression.Schema, arguments)
	if err != nil {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true, err
	}
	if resolveError != nil {
		if dataModifying != nil {
			*dataModifying = true
		}
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true, locatedAt(resolveError, expression.Span)
	}
	if match == nil {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true,
			newError(diag.CodeInternalNilGuard, "nil function match for %s during type resolution", expression.FunctionName)
	}

	if match.dataAccess != sqlmodel.DataAccessReadOnly && dataModifying != nil {
		*dataModifying = true
	}
	if match.notice != nil {
		r.notices = append(r.notices, asError(locatedAt(match.notice, expression.Span)))
	}
	if r.resolvedCalls != nil && (match.isAggregate || match.elementsKnown) {
		r.resolvedCalls[expression] = resolvedCall{aggregate: match.isAggregate, elementsKnown: match.elementsKnown}
	}

	return match.returnType, resultNullable(match.nullableBehaviour, match.nullingArgument, argumentNullability(arguments)), nil
}

// resolveCallArguments types a call's arguments: every argument that is not a lambda
// first, and then each lambda, whose parameters stand for the elements of the lists among
// the other arguments.
//
// Lambda bindings reuse the types already found for other arguments, including a list
// passed to reduce. This avoids repeatedly typing the same arguments as lambdas nest
// inside one another.
//
// Takes expressions ([]ast.Expression) which are the call's arguments; a nil argument is
// of unknown type and may be NULL.
// Takes scope (*ScopeChain) which resolves the arguments' names.
// Takes dataModifying (*bool) which is set to true if an argument modifies data.
//
// Returns []callArgument which holds, parallel to expressions, each argument's name, type
// and nullability.
func (r *TypeResolver) resolveCallArguments(ctx context.Context, expressions []ast.Expression, scope *ScopeChain, dataModifying *bool) []callArgument {
	arguments := make([]callArgument, len(expressions))
	lambdas := make([]bool, len(expressions))
	for index, argument := range expressions {
		lambdas[index] = lambdaArgument(argument) != nil
		if !lambdas[index] {
			arguments[index] = r.resolveCallArgument(ctx, argument, scope, dataModifying)
		}
	}
	elements := listElements(arguments, lambdas)
	enclosing := r.typedArguments
	defer func() { r.typedArguments = enclosing }()
	for index, argument := range expressions {
		if lambdas[index] {
			r.typedArguments = typedArgumentsOf(expressions, arguments, lambdas)
			r.offeredElements = elements
			arguments[index] = r.resolveCallArgument(ctx, argument, scope, dataModifying)
		}
	}
	r.offeredElements = nil
	return arguments
}

// resolveCallArgument types one argument of a call, passed by position or by name.
//
// Takes argument (ast.Expression) which is the argument; nil is of unknown type.
// Takes scope (*ScopeChain) which resolves the argument's names.
// Takes dataModifying (*bool) which is set to true if the argument modifies data.
//
// Returns callArgument which holds the argument's name, type and nullability.
func (r *TypeResolver) resolveCallArgument(ctx context.Context, argument ast.Expression, scope *ScopeChain, dataModifying *bool) callArgument {
	if argument == nil {
		return callArgument{name: "", sqlType: sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), nullable: true, literal: false}
	}
	name := ""
	value := argument
	if named, isNamed := argument.(*ast.NamedArgumentExpression); isNamed {
		name = named.Name
		value = named.Value
	}
	_, literal := value.(*ast.LiteralExpression)
	sqlType, nullable, err := r.resolveExpressionType(ctx, argument, scope, dataModifying)
	r.noteOperandProblem(err)
	return callArgument{name: name, sqlType: sqlType, nullable: nullable, literal: literal}
}

// resolveCoalesceExpression finds the common supertype of COALESCE arguments and applies
// it to parameter arguments.
//
// The result is nullable only when every argument is nullable. An engine that implements
// dialect.CoalesceTyperPort types the call by its arguments only up to the first that
// cannot be NULL.
//
// Takes expression (*ast.CoalesceExpression) which specifies the COALESCE to resolve.
// Takes scope (*ScopeChain) which specifies the scope chain for resolving argument
// expressions.
// Takes dataModifying (*bool) which specifies a flag set to true if any argument modifies
// data.
//
// Returns sqlmodel.SQLType which holds the common supertype of all arguments.
// Returns bool which indicates whether the result is nullable.
// Returns error which is always nil: an operand's problem is a notice.
func (r *TypeResolver) resolveCoalesceExpression(
	ctx context.Context,
	expression *ast.CoalesceExpression,
	scope *ScopeChain,
	dataModifying *bool,
) (sqlmodel.SQLType, bool, error) {
	resultType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	allNullable := true
	typer, stopsTyping := r.engine.(dialect.CoalesceTyperPort)
	stopsTyping = stopsTyping && typer.CoalesceTypedUpToNonNullable()
	typed := true

	for _, argument := range expression.Arguments {
		if argument == nil {
			continue
		}

		argumentType, argumentNullable, err := r.resolveExpressionType(ctx, argument, scope, dataModifying)
		r.noteOperandProblem(err)
		if typed {
			resultType = r.commonSupertype(resultType, argumentType)
		}
		if !argumentNullable {
			allNullable = false
			if stopsTyping && argumentType.Category != sqlmodel.TypeCategoryUnknown {
				typed = false
			}
		}
	}
	r.noteCoalescedParameters(expression.Arguments, resultType)

	return resultType, allNullable, nil
}

// resolveCastExpression resolves a CAST expression by normalising the target type name
// through the dialect and preserving the inner expression's nullability. A conversion the
// engine answers with NULL for a value it cannot convert, as castMayGiveNull decides, is
// nullable whatever its operand.
//
// Takes expression (*ast.CastExpression) which specifies the CAST to resolve.
// Takes scope (*ScopeChain) which specifies the scope chain for resolving the inner
// expression.
// Takes dataModifying (*bool) which specifies a flag set to true if the inner expression
// modifies data.
//
// Returns sqlmodel.SQLType which holds the normalised target type, resolved against the
// catalogue's domains, enums and composite types as a column's type is.
// Returns bool which indicates whether the result is nullable (inherits from the inner
// expression).
// Returns error which is always nil: an operand's problem is a notice.
func (r *TypeResolver) resolveCastExpression(
	ctx context.Context,
	expression *ast.CastExpression,
	scope *ScopeChain,
	dataModifying *bool,
) (sqlmodel.SQLType, bool, error) {
	innerType, innerNullable := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true
	if expression.Inner != nil {
		var err error
		innerType, innerNullable, err = r.resolveExpressionType(ctx, expression.Inner, scope, dataModifying)
		r.noteOperandProblem(err)
	}

	if expression.TypeName != "" {
		target := r.resolveCustomType(r.engine.NormaliseTypeName(expression.TypeName))
		return target, innerNullable || r.castMayGiveNull(expression.Inner, innerType, target), nil
	}
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), innerNullable, nil
}

// castMayGiveNull uses the dialect's ArgumentCoercerPort to detect conversions that can
// return NULL for unconvertible values. Implicit conversions and operands with unknown
// types do not qualify.
//
// Takes operand (ast.Expression) which is the converted expression.
// Takes operandType (sqlmodel.SQLType) which is the operand's type.
// Takes target (sqlmodel.SQLType) which is the type the operand is converted to.
//
// Returns bool which is true when the conversion may give NULL.
func (r *TypeResolver) castMayGiveNull(operand ast.Expression, operandType, target sqlmodel.SQLType) bool {
	coercer, coerces := r.engine.(dialect.ArgumentCoercerPort)
	if !coerces || operandType.Category == sqlmodel.TypeCategoryUnknown || r.engine.CanImplicitCast(operandType, target) {
		return false
	}
	_, literal := operand.(*ast.LiteralExpression)
	coercion, converts := coercer.CoerceArgument(dialect.CallArgument{Type: operandType, Literal: literal}, target)
	return converts && coercion.MayBeNull
}

// resolveLiteralExpression resolves a literal expression by normalising its type name
// through the dialect. Literals are never nullable.
//
// Takes expression (*ast.LiteralExpression) which specifies the literal to resolve.
//
// Returns sqlmodel.SQLType which holds the normalised literal type.
// Returns bool which indicates whether the result is nullable (always false for
// literals).
// Returns error which is always nil.
func (r *TypeResolver) resolveLiteralExpression(
	expression *ast.LiteralExpression,
) (sqlmodel.SQLType, bool, error) {
	if expression.TypeName != "" {
		return r.engine.NormaliseTypeName(expression.TypeName), false, nil
	}
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), false, nil
}

// resolveBinaryOpExpression uses the dialect's result type and nullability behaviour,
// falling back to the common operand type and operand nullability. A parser flag marking
// the operation as potentially returning NULL makes either result nullable.
//
// Takes expression (*ast.BinaryOpExpression) which specifies the binary operation to
// resolve.
// Takes scope (*ScopeChain) which specifies the scope chain for resolving operand
// expressions.
// Takes dataModifying (*bool) which specifies a flag set to true if either operand
// modifies data.
//
// Returns sqlmodel.SQLType which holds the inferred result type.
// Returns bool which indicates whether the result is nullable.
// Returns error which is always nil: an operand's problem is a notice.
func (r *TypeResolver) resolveBinaryOpExpression(
	ctx context.Context,
	expression *ast.BinaryOpExpression,
	scope *ScopeChain,
	dataModifying *bool,
) (sqlmodel.SQLType, bool, error) {
	leftType, leftNullable, leftError := r.resolveExpressionType(ctx, expression.Left, scope, dataModifying)
	r.noteOperandProblem(leftError)
	rightType, rightNullable, rightError := r.resolveExpressionType(ctx, expression.Right, scope, dataModifying)
	r.noteOperandProblem(rightError)
	operandNullability := []bool{leftNullable, rightNullable}

	if resultType, behaviour, typed := r.engine.BinaryResult(expression.Operator, leftType, rightType); typed {
		return resultType, expression.MayBeNull || resultNullable(behaviour, 0, operandNullability), nil
	}
	return r.commonSupertype(leftType, rightType), expression.MayBeNull || leftNullable || rightNullable, nil
}

// resolveUnaryOpExpression resolves a unary operator expression. The dialect types the
// operators whose results it knows, such as a logical negation; any other operator keeps
// its operand's type and nullability.
//
// Takes expression (*ast.UnaryOpExpression) which specifies the unary operation to
// resolve.
// Takes scope (*ScopeChain) which specifies the scope chain for resolving the operand.
// Takes dataModifying (*bool) which specifies a flag set to true if the operand modifies
// data.
//
// Returns sqlmodel.SQLType which holds the inferred result type.
// Returns bool which indicates whether the result is nullable.
// Returns error which is the problem typing the operand, or nil.
func (r *TypeResolver) resolveUnaryOpExpression(
	ctx context.Context,
	expression *ast.UnaryOpExpression,
	scope *ScopeChain,
	dataModifying *bool,
) (sqlmodel.SQLType, bool, error) {
	operandType, operandNullable, operandError := r.resolveExpressionType(ctx, expression.Operand, scope, dataModifying)
	r.noteNegatedParameter(expression)
	if resultType, behaviour, typed := r.engine.UnaryResult(expression.Operator, operandType); typed {
		return resultType, resultNullable(behaviour, 0, []bool{operandNullable}), operandError
	}
	return operandType, operandNullable, operandError
}

// resolveCaseWhenExpression resolves a CASE/WHEN expression by computing the common
// supertype across all branch results and the ELSE result.
//
// The result is nullable if any branch is nullable or the ELSE clause is absent. The
// branch conditions are typed for their problems, which are kept as notices.
//
// Takes expression (*ast.CaseWhenExpression) which specifies the CASE expression to
// resolve.
// Takes scope (*ScopeChain) which specifies the scope chain for resolving branch
// expressions.
// Takes dataModifying (*bool) which specifies a flag set to true if any branch modifies
// data.
//
// Returns sqlmodel.SQLType which holds the common supertype of all branches.
// Returns bool which indicates whether the result is nullable.
// Returns error which is always nil: an operand's problem is a notice.
func (r *TypeResolver) resolveCaseWhenExpression(
	ctx context.Context,
	expression *ast.CaseWhenExpression,
	scope *ScopeChain,
	dataModifying *bool,
) (sqlmodel.SQLType, bool, error) {
	resultType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	nullable := expression.ElseResult == nil

	for _, branch := range expression.Branches {
		if branch.Condition != nil {
			_, _, conditionError := r.resolveExpressionType(ctx, branch.Condition, scope, dataModifying)
			r.noteOperandProblem(conditionError)
			r.noteConditionParameters(branch.Condition)
		}
		if branch.Result == nil {
			nullable = true
			continue
		}

		branchType, branchNullable, err := r.resolveExpressionType(ctx, branch.Result, scope, dataModifying)
		r.noteOperandProblem(err)
		resultType = r.commonSupertype(resultType, branchType)
		if branchNullable {
			nullable = true
		}
	}

	if expression.ElseResult != nil {
		elseType, elseNullable, err := r.resolveExpressionType(ctx, expression.ElseResult, scope, dataModifying)
		r.noteOperandProblem(err)
		resultType = r.commonSupertype(resultType, elseType)
		if elseNullable {
			nullable = true
		}
	}

	return resultType, nullable, nil
}

// resolveWindowFunctionExpression resolves a window function expression by delegating to
// the underlying function call resolution.
//
// Takes expression (*ast.WindowFunctionExpression) which specifies the window function to
// resolve.
// Takes scope (*ScopeChain) which specifies the scope chain for resolving function
// arguments.
// Takes dataModifying (*bool) which specifies a flag set to true if the function modifies
// data.
//
// Returns sqlmodel.SQLType which holds the function's return type.
// Returns bool which indicates whether the result is nullable.
// Returns error which indicates Q030 for a window without a function call, or a function
// resolution failure.
func (r *TypeResolver) resolveWindowFunctionExpression(
	ctx context.Context,
	expression *ast.WindowFunctionExpression,
	scope *ScopeChain,
	dataModifying *bool,
) (sqlmodel.SQLType, bool, error) {
	if expression.Function == nil {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true,
			newError(diag.CodeInternalNilGuard, "window function without a function call during type resolution")
	}
	return r.resolveFunctionCallExpression(ctx, expression.Function, scope, dataModifying)
}

// resolveCustomType resolves an unknown SQL type against the catalogue's enums and
// composite types, as resolveNamedTypes does.
//
// Takes sqlType (sqlmodel.SQLType) which specifies the type to resolve.
//
// Returns sqlmodel.SQLType which holds the resolved type with category and metadata
// populated.
func (r *TypeResolver) resolveCustomType(sqlType sqlmodel.SQLType) sqlmodel.SQLType {
	return resolveNamedTypes(r.catalogue, sqlType, 0)
}

// resolveScalarSubqueryExpression resolves the first output column using an inner scope
// that supports correlated references. The result is nullable unless the subquery
// guarantees exactly one row and its column is non-null.
//
// Takes expression (*ast.ScalarSubqueryExpression) which specifies the subquery to
// resolve.
// Takes outerScope (*ScopeChain) which specifies the parent scope for correlated
// references.
// Takes dataModifying (*bool) which specifies a flag set to true if the subquery modifies
// data.
//
// Returns sqlmodel.SQLType which holds the type of the first output column.
// Returns bool which indicates whether the result is nullable.
// Returns error which indicates a resolution failure.
func (r *TypeResolver) resolveScalarSubqueryExpression(
	ctx context.Context,
	expression *ast.ScalarSubqueryExpression,
	outerScope *ScopeChain,
	dataModifying *bool,
) (sqlmodel.SQLType, bool, error) {
	enclosingCalls := r.resolvedCalls
	r.resolvedCalls = make(map[*ast.FunctionCallExpression]resolvedCall)
	defer func() { r.resolvedCalls = enclosingCalls }()
	column, innerScope, found := r.resolveSubqueryColumnIn(ctx, expression, outerScope, dataModifying)
	if !found {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true, nil
	}
	if r.givesOneRow(expression.InnerQuery, innerScope) {
		return column.SQLType, column.Nullable, nil
	}
	return column.SQLType, true, nil
}

// resolveSubqueryColumn types the first column of a subquery's select list in a scope of
// its own, chained to the enclosing one so a correlated reference resolves, keeping the
// column's own nullability: whether a row the subquery returns may hold NULL there.
//
// Takes expression (*ast.ScalarSubqueryExpression) which is the subquery.
// Takes outerScope (*ScopeChain) which is the enclosing scope.
// Takes dataModifying (*bool) which is set to true if the subquery modifies data; nil
// records nothing.
//
// Returns sqlmodel.OutputColumn which is the column.
// Returns bool which is false when the subquery selects no column that resolves.
func (r *TypeResolver) resolveSubqueryColumn(
	ctx context.Context,
	expression *ast.ScalarSubqueryExpression,
	outerScope *ScopeChain,
	dataModifying *bool,
) (sqlmodel.OutputColumn, bool) {
	column, _, found := r.resolveSubqueryColumnIn(ctx, expression, outerScope, dataModifying)
	return column, found
}

// resolveSubqueryColumnIn is resolveSubqueryColumn returning the subquery's scope too.
//
// Takes expression (*ast.ScalarSubqueryExpression) which is the subquery.
// Takes outerScope (*ScopeChain) which is the enclosing scope.
// Takes dataModifying (*bool) which is set to true if the subquery modifies data; nil
// records nothing.
//
// Returns sqlmodel.OutputColumn which is the column.
// Returns *ScopeChain which is the subquery's scope, one level deeper than the enclosing
// one; a subquery the enclosing scope is too deep to nest is not typed, and the limit is
// recorded for TakeDepthLimits.
// Returns bool which is false when the subquery selects no column that resolves.
func (r *TypeResolver) resolveSubqueryColumnIn(
	ctx context.Context,
	expression *ast.ScalarSubqueryExpression,
	outerScope *ScopeChain,
	dataModifying *bool,
) (sqlmodel.OutputColumn, *ScopeChain, bool) {
	if expression == nil || expression.InnerQuery == nil || len(expression.InnerQuery.OutputColumns) == 0 {
		return sqlmodel.OutputColumn{}, nil, false
	}

	if outerScope.nestedTooDeeply() {
		r.subqueryScopeBuilder().noteSilentDepthLimit()
		return sqlmodel.OutputColumn{}, nil, false
	}
	enclosing := r.notices
	innerScope := NewScopeChain(ScopeKindSubquery, outerScope)
	r.subqueryScopeBuilder().addRawTablesToScope(ctx, expression.InnerQuery, innerScope)

	if dataModifying == nil {
		dataModifying = new(bool)
	}
	columns, problem, err := r.resolveSingleOutputColumn(ctx, expression.InnerQuery.OutputColumns[0], innerScope, dataModifying)
	r.notices = enclosing
	if err != nil || problem != nil || len(columns) == 0 {
		return sqlmodel.OutputColumn{}, nil, false
	}
	return columns[0], innerScope, true
}

// resolveArraySubscriptExpression resolves an array subscript expression by extracting
// the element type from the array type.
//
// The result is always nullable since the subscript index may be out of bounds. The index
// is typed for its problems, which are kept as notices.
//
// Takes expression (*ast.ArraySubscriptExpression) which specifies the subscript to
// resolve.
// Takes scope (*ScopeChain) which specifies the scope chain for resolving the array
// expression.
// Takes dataModifying (*bool) which specifies a flag set to true if the array expression
// modifies data.
//
// Returns sqlmodel.SQLType which holds the array element type.
// Returns bool which indicates whether the result is nullable (always true).
// Returns error which indicates a resolution failure for the array expression.
func (r *TypeResolver) resolveArraySubscriptExpression(
	ctx context.Context,
	expression *ast.ArraySubscriptExpression,
	scope *ScopeChain,
	dataModifying *bool,
) (sqlmodel.SQLType, bool, error) {
	if expression == nil {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true, nil
	}
	arrayType, _, err := r.resolveExpressionType(ctx, expression.Array, scope, dataModifying)
	if expression.Index != nil {
		_, _, indexError := r.resolveExpressionType(ctx, expression.Index, scope, dataModifying)
		r.noteOperandProblem(indexError)
	}
	if err != nil {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true, err
	}
	if arrayType.ElementType != nil {
		return *arrayType.ElementType, true, nil
	}
	return arrayType, true, nil
}

// resolveStructFieldAccessExpression resolves a struct field access expression by
// resolving the struct expression and then looking up the named field within its struct
// fields.
//
// Takes expression (*ast.StructFieldAccessExpression) which specifies the field access to
// resolve.
// Takes scope (*ScopeChain) which specifies the scope chain for resolving the struct
// expression.
// Takes dataModifying (*bool) which specifies a flag set to true if the struct expression
// modifies data.
//
// A value whose type declares no fields takes its fields from the composite type the
// catalogue defines under its type's name, and otherwise, as a JSON document, has dynamic
// elements, which are typed unknown; a field a declared struct or composite type lacks is
// an unknown-column error.
//
// Returns sqlmodel.SQLType which holds the type of the accessed field.
// Returns bool which indicates whether the result is nullable: when the struct is, or the
// field's type says the field is.
// Returns error which indicates a resolution failure for the struct expression or an
// unknown field.
func (r *TypeResolver) resolveStructFieldAccessExpression(
	ctx context.Context,
	expression *ast.StructFieldAccessExpression,
	scope *ScopeChain,
	dataModifying *bool,
) (sqlmodel.SQLType, bool, error) {
	if expression == nil {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true, nil
	}
	structType, nullable, err := r.resolveExpressionType(ctx, expression.Struct, scope, dataModifying)
	if err != nil {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true, err
	}

	if len(structType.StructFields) == 0 {
		return r.resolveCompositeFieldAccess(structType, nullable, expression.FieldName)
	}
	fieldNames := make([]string, len(structType.StructFields))
	for index := range structType.StructFields {
		if structType.StructFields[index].Name == expression.FieldName {
			return structType.StructFields[index].SQLType, nullable || structType.StructFields[index].SQLType.Nullable, nil
		}
		fieldNames[index] = structType.StructFields[index].Name
	}

	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true,
		newError(diag.CodeUnknownColumn, "unknown field %q", expression.FieldName).
			withSuggestion(diagbuild.ClosestSuggestion(expression.FieldName, fieldNames))
}

// resolveStructExpression types a struct constructor as the struct it builds: a struct
// with one field for each name, in order, of the type of the value at the same position.
//
// Every field may hold NULL, whatever its value, and the struct itself is never NULL. The
// values' problems are kept as notices.
//
// Takes expression (*ast.StructExpression) which specifies the constructor to resolve.
// Takes scope (*ScopeChain) which specifies the scope chain for resolving the values.
// Takes dataModifying (*bool) which specifies a flag set to true if a value modifies
// data.
//
// Returns sqlmodel.SQLType which holds the struct type.
// Returns bool which is always false: the struct is never NULL.
// Returns error which is always nil: a value's problem is a notice.
func (r *TypeResolver) resolveStructExpression(
	ctx context.Context,
	expression *ast.StructExpression,
	scope *ScopeChain,
	dataModifying *bool,
) (sqlmodel.SQLType, bool, error) {
	fields := make([]sqlmodel.StructField, 0, len(expression.Names))
	for index, value := range expression.Values {
		valueType, _, err := r.resolveExpressionType(ctx, value, scope, dataModifying)
		r.noteOperandProblem(err)
		if index >= len(expression.Names) {
			continue
		}
		valueType.Nullable = true
		fields = append(fields, sqlmodel.StructField{Name: expression.Names[index], SQLType: valueType, Positional: false})
	}
	structType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "struct")
	structType.StructFields = fields
	return structType, false, nil
}

// resolveCompositeFieldAccess resolves a field through its catalogue-defined composite
// type. Missing composite fields produce an unknown-column error; fields of other values,
// such as JSON, have unknown types.
//
// Takes valueType (sqlmodel.SQLType) which is the accessed value's type.
// Takes nullable (bool) which reports whether the accessed value may be NULL.
// Takes fieldName (string) which is the field as the expression names it.
//
// Returns sqlmodel.SQLType which holds the type of the accessed field.
// Returns bool which indicates whether the result is nullable: when the value is, or the
// field may be.
// Returns error which is an unknown-column error for a field the composite type lacks.
func (r *TypeResolver) resolveCompositeFieldAccess(
	valueType sqlmodel.SQLType,
	nullable bool,
	fieldName string,
) (sqlmodel.SQLType, bool, error) {
	fieldType, fieldNullable, fieldNames, found := compositeField(r.catalogue, valueType, fieldName)
	switch {
	case found:
		return fieldType, nullable || fieldNullable, nil
	case fieldNames != nil:
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true,
			newError(diag.CodeUnknownColumn, "unknown field %q", fieldName).
				withSuggestion(diagbuild.ClosestSuggestion(fieldName, fieldNames))
	default:
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true, nil
	}
}

// commonSupertype returns the common supertype of two SQL types following the engine's
// promotion and implicit casting rules. Used for CASE/WHEN branches, UNION columns, and
// COALESCE arguments.
//
// Numbers of different categories meet where the engine's PromoteType puts them, since
// many engines cast numbers both ways. Implicit casts decide the other pairs.
//
// Takes left (sqlmodel.SQLType) which specifies the first type to merge.
// Takes right (sqlmodel.SQLType) which specifies the second type to merge.
//
// Returns sqlmodel.SQLType which holds the common supertype, or unknown if no promotion
// path exists.
func (r *TypeResolver) commonSupertype(left sqlmodel.SQLType, right sqlmodel.SQLType) sqlmodel.SQLType {
	if left.Category == sqlmodel.TypeCategoryUnknown {
		return right
	}
	if right.Category == sqlmodel.TypeCategoryUnknown {
		return left
	}

	if left.Category == right.Category && strings.EqualFold(left.EngineName, right.EngineName) {
		return left
	}

	if left.Category == right.Category || (isNumberType(left) && isNumberType(right)) {
		return r.engine.PromoteType(left, right)
	}

	if r.engine.CanImplicitCast(left, right) {
		return right
	}
	if r.engine.CanImplicitCast(right, left) {
		return left
	}

	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
}

// isNumberType reports whether a type is an integer, a binary float or an exact decimal.
//
// Takes sqlType (sqlmodel.SQLType) which is the type.
//
// Returns bool which is true for a number.
func isNumberType(sqlType sqlmodel.SQLType) bool {
	switch sqlType.Category {
	case sqlmodel.TypeCategoryInteger, sqlmodel.TypeCategoryFloat, sqlmodel.TypeCategoryDecimal:
		return true
	default:
		return false
	}
}

// argumentNullability lists whether each argument of a call may be NULL.
//
// Takes arguments ([]callArgument) which are the call's typed arguments.
//
// Returns []bool which holds, parallel to arguments, whether each may be NULL.
func argumentNullability(arguments []callArgument) []bool {
	nullability := make([]bool, len(arguments))
	for index := range arguments {
		nullability[index] = arguments[index].nullable
	}
	return nullability
}

// resultNullable decides whether the result of a function or an operator may be NULL from
// its NULL behaviour and the nullability of its arguments.
//
// Takes behaviour (sqlmodel.FunctionNullableBehaviour) which says when the result is
// NULL.
// Takes nullingArgument (int) which is the position of the argument whose NULL makes the
// result NULL, read for sqlmodel.FunctionNullableNullWhenArgumentNull.
// Takes argumentNullability ([]bool) which holds whether each argument may be NULL.
//
// Returns bool which is true when the result may be NULL.
func resultNullable(behaviour sqlmodel.FunctionNullableBehaviour, nullingArgument int, argumentNullability []bool) bool {
	switch behaviour {
	case sqlmodel.FunctionNullableNeverNull:
		return false
	case sqlmodel.FunctionNullableReturnsNullOnNull:
		return slices.Contains(argumentNullability, true)
	case sqlmodel.FunctionNullableNullWhenArgumentNull:
		return nullingArgument < 0 || nullingArgument >= len(argumentNullability) || argumentNullability[nullingArgument]
	default:
		return true
	}
}

// isBooleanExpression returns true for the predicates: comparisons, IS NULL, IN, BETWEEN,
// logical operators and EXISTS, which resolvePredicate types.
//
// Takes expression (ast.Expression) which specifies the expression to classify.
//
// Returns bool which is true when the expression is a predicate.
func isBooleanExpression(expression ast.Expression) bool {
	switch expression.(type) {
	case *ast.ComparisonExpression,
		*ast.IsNullExpression,
		*ast.InListExpression,
		*ast.BetweenExpression,
		*ast.LogicalOpExpression,
		*ast.ExistsExpression:
		return true
	default:
		return false
	}
}

// rowValueInstead resolves, after a reference failed to resolve as a column, an
// unqualified name that names no column as the current row of the relation it names,
// where the dialect has row values.
//
// Takes scope (*ScopeChain) which resolves the name.
// Takes tableAlias (string) which is the reference's qualifier; a qualified reference is
// never a row.
// Takes columnName (string) which is the name.
// Takes err (error) which is why the reference did not resolve as a column; only an
// unknown column may be a row.
//
// Returns *ast.ScopedColumn which holds the row.
// Returns *ScopedTable which holds the relation.
// Returns bool which is true when the name is a row.
func rowValueInstead(scope *ScopeChain, tableAlias, columnName string, err error) (*ast.ScopedColumn, *ScopedTable, bool) {
	if tableAlias != "" || ExtractErrorCode(err) != diag.CodeUnknownColumn {
		return nil, nil, false
	}
	return scope.ResolveRowValue(columnName)
}

// typedArgumentsOf indexes the arguments of a call that are not lambdas by the expression
// written for each, and by its value for an argument passed by name.
//
// Takes expressions ([]ast.Expression) which are the call's arguments as written.
// Takes arguments ([]callArgument) which holds, parallel to expressions, their types.
// Takes lambdas ([]bool) which marks, parallel to expressions, the lambda arguments.
//
// Returns map[ast.Expression]callArgument which holds the typed arguments.
func typedArgumentsOf(expressions []ast.Expression, arguments []callArgument, lambdas []bool) map[ast.Expression]callArgument {
	typed := make(map[ast.Expression]callArgument, len(expressions))
	for index, expression := range expressions {
		if lambdas[index] || expression == nil {
			continue
		}
		typed[expression] = arguments[index]
		if named, isNamed := expression.(*ast.NamedArgumentExpression); isNamed && named != nil && named.Value != nil {
			typed[named.Value] = arguments[index]
		}
	}
	return typed
}

// resolveNamedTypes resolves an unknown SQL type against the catalogue's domains, enums
// and composite types, through sqlmodel.Catalogue.ResolveNamedType.
//
// Array element types are resolved recursively, copying each array level. Resolution
// stops at sqlmodel.MaxTypeDepth to prevent deeply nested or cyclic types from exhausting
// the stack.
//
// Takes catalogue (*sqlmodel.Catalogue) which holds the named types, or nil to resolve
// none.
// Takes sqlType (sqlmodel.SQLType) which specifies the type to resolve.
// Takes depth (int) which is the depth of the array level being resolved.
//
// Returns sqlmodel.SQLType which holds the resolved type, unresolved past the limit.
func resolveNamedTypes(catalogue *sqlmodel.Catalogue, sqlType sqlmodel.SQLType, depth int) sqlmodel.SQLType {
	if depth >= sqlmodel.MaxTypeDepth {
		return sqlType
	}
	if sqlType.Category == sqlmodel.TypeCategoryArray && sqlType.ElementType != nil {
		sqlType.ElementType = new(resolveNamedTypes(catalogue, *sqlType.ElementType, depth+1))
		return sqlType
	}
	return catalogue.ResolveNamedType(sqlType)
}

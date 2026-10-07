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

package sqlite

import (
	"context"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

// ResolveFunctionCall resolves a polymorphic SQLite function call.
//
// Handles the functions the built-in catalogue declares as returning "any" because the
// actual return type propagates from an argument: the aggregates SUM, MIN and MAX, the
// window value functions LAG, LEAD, FIRST_VALUE, LAST_VALUE and NTH_VALUE, and the scalar
// IIF and its spelling IF, IFNULL, NULLIF, ABS and the likelihood hints.
//
// A panic while the call is resolved is recovered and returned as a
// dialectkit.PanicError, and its stack is logged once at Warn through the context's
// logger.
//
// Takes name (string) which holds the function name to resolve.
// Takes argumentTypes ([]sqlmodel.SQLType) which holds the resolved types of the
// call-site arguments.
//
// Returns resolution (*ast.FunctionResolution) which holds the resolved return type and
// metadata, or nil when no polymorphic resolution applies.
// Returns err (error) when the context is cancelled, when it is the context's cause, or
// resolving panics.
func (*Dialect) ResolveFunctionCall(
	ctx context.Context,
	_ *sqlmodel.Catalogue,
	name string,
	_ string,
	argumentTypes []sqlmodel.SQLType,
) (resolution *ast.FunctionResolution, err error) {
	defer func() { dialectkit.Recover(ctx, "sqlite: resolve function call", recover(), &resolution, &err) }()

	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	switch strings.ToLower(name) {
	case "min", "max":
		return resolveSQLiteIdentityAggregate(argumentTypes)
	case "sum":
		return resolveSQLiteSum(argumentTypes)
	case "coalesce":
		return resolveSQLiteCoalesce(argumentTypes)
	case "ifnull":
		return resolveFirstKnownArgument(argumentTypes, sqlmodel.FunctionNullableCalledOnNull, 0, 1)
	case "iif", "if":
		return resolveFirstKnownArgument(argumentTypes, sqlmodel.FunctionNullableCalledOnNull, conditionalValuePositions(len(argumentTypes))...)
	case "lag", "lead":
		return resolveFirstKnownArgument(argumentTypes, sqlmodel.FunctionNullableCalledOnNull, 0, 2)
	case "first_value", "last_value", "nth_value", "nullif":
		return resolveFirstKnownArgument(argumentTypes, sqlmodel.FunctionNullableCalledOnNull, 0)
	case "abs", "ceil", "ceiling", "floor", "trunc", "likely", "unlikely", "likelihood":
		return resolveFirstKnownArgument(argumentTypes, sqlmodel.FunctionNullableReturnsNullOnNull, 0)
	default:
		return nil, nil
	}
}

// conditionalValuePositions returns the positions of the values among the arguments of
// iif or if, which alternate conditions and the values they choose, with a last value
// chosen when no condition holds when the count is odd: iif(c, x), iif(c, x, y) and
// iif(c1, x1, c2, x2, y).
//
// Takes count (int) which is the number of arguments.
//
// Returns []int which holds the positions of the values, in order.
func conditionalValuePositions(count int) []int {
	positions := make([]int, 0, count/2+1)
	for position := 1; position < count; position += 2 {
		positions = append(positions, position)
	}
	if count > 2 && count%2 == 1 {
		positions = append(positions, count-1)
	}
	return positions
}

// resolveFirstKnownArgument resolves a scalar or window function whose result is one of
// its arguments: the first of the given argument positions whose type is known.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which holds the resolved types of the
// call-site arguments.
// Takes nullability (sqlmodel.FunctionNullableBehaviour) which says when the result is
// NULL.
// Takes positions (...int) which are the argument positions the result may come from, in
// order of preference.
//
// Returns *ast.FunctionResolution which holds the chosen argument's type, or nil when
// none of the positions has a known type, so the catalogue's "any" stands.
// Returns error which is always nil.
func resolveFirstKnownArgument(
	argumentTypes []sqlmodel.SQLType,
	nullability sqlmodel.FunctionNullableBehaviour,
	positions ...int,
) (*ast.FunctionResolution, error) {
	for _, position := range positions {
		if position >= len(argumentTypes) || argumentTypes[position].Category == sqlmodel.TypeCategoryUnknown {
			continue
		}
		return &ast.FunctionResolution{
			ReturnType:        argumentTypes[position],
			NullableBehaviour: nullability,
			NullingArgument:   0,
			DataAccess:        sqlmodel.DataAccessReadOnly,
			IsAggregate:       false,
			ReturnsSet:        false,
			ElementsKnown:     false,
		}, nil
	}
	return nil, nil
}

// resolveSQLiteIdentityAggregate resolves MIN and MAX which return the same type as their
// argument.
//
// SQLite exposes two overloads. The single-argument form is the aggregate that collapses
// a column down to its smallest or largest value. The two-or-more argument form is the
// scalar overload that returns the smallest or largest of its arguments per row, so it
// must not be flagged as an aggregate.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which holds the resolved types of the
// call-site arguments.
//
// Returns *ast.FunctionResolution which holds the argument type as the return type, or
// nil if no arguments were provided.
// Returns error which is always nil.
func resolveSQLiteIdentityAggregate(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < 1 {
		return nil, nil
	}

	return &ast.FunctionResolution{
		ReturnType:        argumentTypes[0],
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		NullingArgument:   0,
		IsAggregate:       len(argumentTypes) == 1,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveSQLiteSum resolves SUM which in SQLite returns integer for integer arguments and
// real for everything else.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which holds the resolved types of the
// call-site arguments.
//
// Returns *ast.FunctionResolution which holds the promoted return type, or nil if no
// arguments were provided.
// Returns error which is always nil.
func resolveSQLiteSum(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < 1 {
		return nil, nil
	}

	argumentType := argumentTypes[0]
	var returnType sqlmodel.SQLType

	switch argumentType.Category {
	case sqlmodel.TypeCategoryInteger:

		returnType = sqlmodel.NewIntegerType(typeNameBigint, widthInteger, false)
	default:
		returnType = sqlmodel.NewFloatType(typeNameReal, widthReal)
	}

	return &ast.FunctionResolution{
		ReturnType:        returnType,
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		NullingArgument:   0,
		IsAggregate:       true,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveSQLiteCoalesce resolves COALESCE which returns the type of the first non-unknown
// argument.
//
// The parser normally lowers COALESCE into a dedicated CoalesceExpression that the type
// resolver handles directly, computing nullability as "nullable only when every argument
// is nullable". This resolution is a defensive fallback for the rare path where COALESCE
// reaches function resolution as a plain call. It reports the first known argument type
// and leaves the conservative nullability behaviour to the resolver, since the dedicated
// expression path remains authoritative for COALESCE nullability.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which holds the resolved types of the
// call-site arguments.
//
// Returns *ast.FunctionResolution which holds the type of the first argument with a known
// category, or unknown if all arguments have unknown types.
// Returns error which is always nil.
func resolveSQLiteCoalesce(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	for index := range argumentTypes {
		if argumentTypes[index].Category != sqlmodel.TypeCategoryUnknown {
			return &ast.FunctionResolution{
				ReturnType:        argumentTypes[index],
				NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
				NullingArgument:   0,
				DataAccess:        sqlmodel.DataAccessReadOnly,
				IsAggregate:       false,
				ReturnsSet:        false,
				ElementsKnown:     false,
			}, nil
		}
	}

	return &ast.FunctionResolution{
		ReturnType:        sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

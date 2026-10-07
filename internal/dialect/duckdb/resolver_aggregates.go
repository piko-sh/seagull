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
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// rowsCountArguments is the number of arguments arg_min, arg_max and their aliases take
	// when the third is a count of rows, which makes the result a list.
	rowsCountArguments = 3

	// quantilePositionArgument is the position of a quantile's fraction, a list of fractions
	// making the result a list.
	quantilePositionArgument = 1

	// countEngineName is the engine name of the type DuckDB counts values with in a
	// histogram, UBIGINT.
	countEngineName = "ubigint"
)

// resolveArgumentAtExtreme resolves arg_min, arg_max and their aliases, which give the
// first argument of the row holding the least or the greatest key: of that argument's
// type, or, given a count of rows as a third argument, a list of the arguments of that
// many rows.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the argument, the key and an
// optional count.
//
// Returns *ast.FunctionResolution which types the aggregate, or nil when the call has no
// arguments.
// Returns error which is always nil.
func resolveArgumentAtExtreme(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < 1 {
		return nil, nil
	}
	if len(argumentTypes) >= rowsCountArguments {
		return aggregateResolution(newArrayType(argumentTypes[0])), nil
	}
	return aggregateResolution(argumentTypes[0]), nil
}

// resolveDiscreteQuantile resolves the quantiles DuckDB picks from the values themselves,
// approx_quantile, reservoir_quantile and quantile: of the values' type for one fraction,
// and a list of it for a list of fractions.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the values, the fraction or list of
// fractions, and an optional sample size.
//
// Returns *ast.FunctionResolution which types the aggregate, or nil when the call has no
// arguments.
// Returns error which is always nil.
func resolveDiscreteQuantile(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < 1 {
		return nil, nil
	}
	if len(argumentTypes) > quantilePositionArgument && argumentTypes[quantilePositionArgument].Category == sqlmodel.TypeCategoryArray {
		return aggregateResolution(newArrayType(argumentTypes[0])), nil
	}
	return aggregateResolution(argumentTypes[0]), nil
}

// resolveHistogram resolves the histogram aggregate, a map from each value to the number
// of times it occurs.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the values and optional bin
// boundaries.
//
// Returns *ast.FunctionResolution which types the aggregate, or nil when the call has no
// arguments.
// Returns error which is always nil.
func resolveHistogram(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < 1 {
		return nil, nil
	}
	return aggregateResolution(newMapType(argumentTypes[0], builtinTypeMap[countEngineName])), nil
}

// resolveTopValues resolves approx_top_k, a list of the most frequent values.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the values and the count.
//
// Returns *ast.FunctionResolution which types the aggregate, or nil when the call has no
// arguments.
// Returns error which is always nil.
func resolveTopValues(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < 1 {
		return nil, nil
	}
	return aggregateResolution(newArrayType(argumentTypes[0])), nil
}

// resolveListElementAggregate resolves the list aggregates that give one of the list's
// elements, list_min, list_max, list_first, list_last, list_any_value and list_mode: of
// the element type, NULL for an empty list.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the list and any further arguments.
//
// Returns *ast.FunctionResolution which types the call, unknown when the list's element
// type is not known.
// Returns error which is always nil.
func resolveListElementAggregate(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	return listFunctionResolution(listElementType(argumentTypes), sqlmodel.FunctionNullableCalledOnNull), nil
}

// resolveListSum resolves list_sum, which sums a list's elements as sum sums a column.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the list.
//
// Returns *ast.FunctionResolution which types the call, unknown when the list's element
// type is not known.
// Returns error which is always nil.
func resolveListSum(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	element := listElementType(argumentTypes)
	if element.Category == sqlmodel.TypeCategoryUnknown {
		return listFunctionResolution(element, sqlmodel.FunctionNullableCalledOnNull), nil
	}
	return listFunctionResolution(summedType(element), sqlmodel.FunctionNullableCalledOnNull), nil
}

// resolveListHistogram resolves list_histogram, a map from each of a list's elements to
// the number of times it occurs, NULL for an empty list.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the list.
//
// Returns *ast.FunctionResolution which types the call.
// Returns error which is always nil.
func resolveListHistogram(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	histogram := newMapType(listElementType(argumentTypes), builtinTypeMap[countEngineName])
	return listFunctionResolution(histogram, sqlmodel.FunctionNullableCalledOnNull), nil
}

// listElementType returns the element type of a call's first argument, a list, or the
// unknown type when it is not known.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the call's argument types.
//
// Returns sqlmodel.SQLType which is the element type.
func listElementType(argumentTypes []sqlmodel.SQLType) sqlmodel.SQLType {
	if len(argumentTypes) >= 1 && argumentTypes[0].ElementType != nil {
		return *argumentTypes[0].ElementType
	}
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
}

// aggregateResolution types an aggregate that reads only data and is NULL over a group of
// no values.
//
// Takes returnType (sqlmodel.SQLType) which is the aggregate's type.
//
// Returns *ast.FunctionResolution which types the aggregate.
func aggregateResolution(returnType sqlmodel.SQLType) *ast.FunctionResolution {
	return &ast.FunctionResolution{
		ReturnType:        returnType,
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       true,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}
}

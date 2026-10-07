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
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// rangeSuffix ends the name of every built-in range type, such as int4range.
	rangeSuffix = "range"

	// multirangeSuffix ends the name of every built-in multirange type, such as
	// int4multirange.
	multirangeSuffix = "multirange"
)

// rangeElementTypes names the element type of each built-in range type.
var rangeElementTypes = map[string]string{
	"int4range": "int4", "int8range": "int8", "numrange": "numeric",
	"tsrange": "timestamp", "tstzrange": "timestamptz", "daterange": "date",
}

// resolveEnumLabel types enum_first and enum_last as the enum type their argument names,
// such as NULL::mood, which the catalogue resolves. The label comes from the type, so it
// is never NULL, even for a NULL argument.
//
// Takes catalogue (*sqlmodel.Catalogue) which resolves a type the argument names.
// Takes argumentTypes ([]sqlmodel.SQLType) which holds the types of the call's arguments.
//
// Returns *ast.FunctionResolution which types the call, or nil when the argument's type
// is unknown.
// Returns error which is always nil.
func resolveEnumLabel(catalogue *sqlmodel.Catalogue, argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	enumType, known := namedArgumentType(catalogue, argumentTypes)
	if !known {
		return nil, nil
	}
	return enumResolution(enumType), nil
}

// resolveEnumRange types enum_range as an array of the enum type its argument names. The
// labels come from the type, so the array is never NULL, even for a NULL argument.
//
// Takes catalogue (*sqlmodel.Catalogue) which resolves a type the argument names.
// Takes argumentTypes ([]sqlmodel.SQLType) which holds the types of the call's arguments.
//
// Returns *ast.FunctionResolution which types the call, or nil when the argument's type
// is unknown.
// Returns error which is always nil.
func resolveEnumRange(catalogue *sqlmodel.Catalogue, argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	enumType, known := namedArgumentType(catalogue, argumentTypes)
	if !known {
		return nil, nil
	}
	return enumResolution(arrayTypeOf(enumType, 1)), nil
}

// namedArgumentType returns the type of a call's first argument, resolving a type it
// names through the catalogue, as a cast such as NULL::mood names one.
//
// Takes catalogue (*sqlmodel.Catalogue) which resolves a named type.
// Takes argumentTypes ([]sqlmodel.SQLType) which holds the types of the call's arguments.
//
// Returns sqlmodel.SQLType which is the first argument's type.
// Returns bool which is false when there is no argument or its type stays unknown.
func namedArgumentType(catalogue *sqlmodel.Catalogue, argumentTypes []sqlmodel.SQLType) (sqlmodel.SQLType, bool) {
	if len(argumentTypes) == 0 {
		return sqlmodel.SQLType{}, false
	}
	resolved := catalogue.ResolveNamedType(argumentTypes[0])
	return resolved, resolved.Category != sqlmodel.TypeCategoryUnknown
}

// enumResolution is the resolution of an enum support function returning a type: never
// NULL, read-only and neither an aggregate nor a set.
//
// Takes returnType (sqlmodel.SQLType) which is the type the called function returns.
//
// Returns *ast.FunctionResolution which is the resolution.
func enumResolution(returnType sqlmodel.SQLType) *ast.FunctionResolution {
	return &ast.FunctionResolution{
		ReturnType:        returnType,
		NullingArgument:   0,
		NullableBehaviour: sqlmodel.FunctionNullableNeverNull,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}
}

// ignoreCatalogue adapts a resolver that needs no catalogue to functionResolvers.
//
// Takes resolve (func([]sqlmodel.SQLType) (*ast.FunctionResolution, error)) which types a
// call from its argument types.
//
// Returns func(*sqlmodel.Catalogue, []sqlmodel.SQLType) (*ast.FunctionResolution, error)
// which calls resolve.
func ignoreCatalogue(
	resolve func([]sqlmodel.SQLType) (*ast.FunctionResolution, error),
) func(*sqlmodel.Catalogue, []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	return func(_ *sqlmodel.Catalogue, argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
		return resolve(argumentTypes)
	}
}

// resolveRangeBound types lower and upper of a range as its element type, such as a
// timestamptz for a tstzrange: NULL for an empty range or an unbounded side, so the bound
// may be NULL whatever the range. lower and upper of text are the catalogue's.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which holds the types of the call's arguments.
//
// Returns *ast.FunctionResolution which types the call, or nil when the argument is not a
// built-in range.
// Returns error which is always nil.
func resolveRangeBound(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) != 1 || argumentTypes[0].Category != sqlmodel.TypeCategoryRange {
		return nil, nil
	}
	element, known := rangeElementTypes[strings.ToLower(argumentTypes[0].EngineName)]
	if !known {
		return nil, nil
	}
	return &ast.FunctionResolution{
		ReturnType:        builtinTypeMap[element],
		NullingArgument:   0,
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveRangeAggregate types range_agg as the multirange of its ranges' type, such as
// int4multirange for int4range, NULL for a group with no rows.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which holds the types of the call's arguments.
//
// Returns *ast.FunctionResolution which types the call, or nil when the argument is not a
// built-in range.
// Returns error which is always nil.
func resolveRangeAggregate(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) == 0 || argumentTypes[0].Category != sqlmodel.TypeCategoryRange {
		return nil, nil
	}
	element, isRange := strings.CutSuffix(strings.ToLower(argumentTypes[0].EngineName), rangeSuffix)
	if !isRange || strings.HasSuffix(element, "multi") {
		return nil, nil
	}
	return &ast.FunctionResolution{
		ReturnType:        sqlmodel.NewSQLType(sqlmodel.TypeCategoryRange, element+multirangeSuffix),
		NullingArgument:   0,
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       true,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

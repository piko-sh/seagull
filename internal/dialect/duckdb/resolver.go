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
	"slices"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// functionRow is the name of the function a ROW(...) constructor, or a parenthesised
	// list of values, is read as.
	functionRow = "row"

	// functionMapLiteral is the name of the function a MAP {key: value, ...} literal is read
	// as, its arguments each entry's key followed by its value.
	//
	// It differs from map, which DuckDB's map(keys, values) call names, so a literal of one
	// entry whose key and value are lists is not read as that call.
	functionMapLiteral = "map_literal"

	// functionStructPack is the name of DuckDB's struct_pack(name := value, ...), which the
	// parser reads as the struct constructor it is.
	functionStructPack = "struct_pack"

	// functionStructInsert is the name of DuckDB's struct_insert(struct, name := value,
	// ...), which adds fields to a struct.
	functionStructInsert = "struct_insert"

	// structEngineName is the engine name of a struct DuckDB builds.
	structEngineName = "struct"

	// positionalFieldPrefix starts the name DuckDB gives a field of an unnamed struct,
	// followed by the field's position from one.
	positionalFieldPrefix = "element"
)

// functionResolver implements dialect.FunctionResolverPort for polymorphic DuckDB
// functions whose return types depend on their argument types.
type functionResolver struct{}

// newFunctionResolver creates a DuckDB function resolver.
//
// Returns *functionResolver which is the new resolver instance.
func newFunctionResolver() *functionResolver {
	return &functionResolver{}
}

// ResolveFunctionCall resolves a polymorphic DuckDB function call that the standard
// overload resolution could not match. It inspects the argument types to compute the
// correct return type for list, JSON, aggregate, conditional, and type-introspection
// functions.
//
// Takes name (string) which is the function name being called.
// Takes argumentTypes ([]sqlmodel.SQLType) which describes call argument types in
// declared order.
//
// Returns *ast.FunctionResolution which is the resolved overload, or nil when the
// function is not polymorphic so the caller falls back to the standard catalogue lookup.
// Returns error when resolution fails.
func (*functionResolver) ResolveFunctionCall(
	_ *sqlmodel.Catalogue,
	name string,
	_ string,
	argumentTypes []sqlmodel.SQLType,
) (*ast.FunctionResolution, error) {
	resolve, polymorphic := polymorphicResolvers[strings.ToLower(name)]
	if !polymorphic {
		return nil, nil
	}
	return resolve(argumentTypes)
}

// polymorphicResolver types a call of a polymorphic built-in from its argument types.
type polymorphicResolver func(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error)

var (
	// polymorphicResolvers maps each built-in whose return type depends on its arguments to
	// the resolver that types it. Every one of them only reads data.
	polymorphicResolvers = map[string]polymorphicResolver{
		"array_agg":          resolveArrayAgg,
		"ceil":               resolveRounding,
		"ceiling":            resolveRounding,
		"floor":              resolveRounding,
		"list":               resolveArrayAgg,
		"unnest":             resolveUnnest,
		"array_append":       passThroughArgument(0),
		"array_cat":          passThroughArgument(0),
		"array_remove":       passThroughArgument(0),
		"array_replace":      passThroughArgument(0),
		"array_prepend":      passThroughArgument(1),
		"min":                resolveIdentityAggregate,
		"max":                resolveIdentityAggregate,
		"sum":                resolveSum,
		"avg":                resolveAvg,
		"mean":               resolveAvg,
		"arg_min":            resolveArgumentAtExtreme,
		"argmin":             resolveArgumentAtExtreme,
		"min_by":             resolveArgumentAtExtreme,
		"arg_max":            resolveArgumentAtExtreme,
		"argmax":             resolveArgumentAtExtreme,
		"max_by":             resolveArgumentAtExtreme,
		"approx_quantile":    resolveDiscreteQuantile,
		"reservoir_quantile": resolveDiscreteQuantile,
		"quantile":           resolveDiscreteQuantile,
		"histogram":          resolveHistogram,
		"approx_top_k":       resolveTopValues,
		"list_min":           resolveListElementAggregate,
		"list_max":           resolveListElementAggregate,
		"list_first":         resolveListElementAggregate,
		"list_last":          resolveListElementAggregate,
		"list_any_value":     resolveListElementAggregate,
		"list_mode":          resolveListElementAggregate,
		"list_sum":           resolveListSum,
		"list_histogram":     resolveListHistogram,
		"typeof":             ignoringArguments(resolveTypeof),
		"struct_pack":        ignoringArguments(resolveStructPack),
		"struct_extract":     resolveStructExtract,
		"struct_insert":      resolveStructInsert,
		functionRow:          resolveRow,
		"map":                resolveMapFromLists,
		functionMapLiteral:   resolveMapLiteral,
		"map_from_entries":   resolveMapFromEntries,
		"map_keys":           resolveMapKeys,
		"map_values":         resolveMapValues,
		"map_entries":        ignoringArguments(resolveMapEntries),
		"element_at":         resolveElementAt,
		"map_extract":        resolveElementAt,
		"map_extract_value":  resolveExtractValue,
		"list_extract":       resolveExtractValue,
		"array_extract":      resolveExtractValue,
		"list_element":       resolveExtractValue,
		"list_value":         resolveListValue,
		"array_value":        resolveListValue,
		"list_transform":     resolveListTransform,
		"array_transform":    resolveListTransform,
		"list_apply":         resolveListTransform,
		"array_apply":        resolveListTransform,
		"apply":              resolveListTransform,
		"list_filter":        resolveListFilter,
		"array_filter":       resolveListFilter,
		"filter":             resolveListFilter,
		"list_reduce":        resolveListReduce,
		"array_reduce":       resolveListReduce,
		"reduce":             resolveListReduce,
	}
)

// resolveCommonArgument types COALESCE, GREATEST, LEAST and mod by the common type of
// their known arguments.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which describes the call argument types in
// declared order.
// Takes nullable (sqlmodel.FunctionNullableBehaviour) which says when the result is NULL.
//
// Returns *ast.FunctionResolution which describes the chosen return type.
func (engine *Dialect) resolveCommonArgument(argumentTypes []sqlmodel.SQLType, nullable sqlmodel.FunctionNullableBehaviour) *ast.FunctionResolution {
	common := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	for index := range argumentTypes {
		switch {
		case argumentTypes[index].Category == sqlmodel.TypeCategoryUnknown:
		case common.Category == sqlmodel.TypeCategoryUnknown:
			common = argumentTypes[index]
		default:
			common = engine.PromoteType(common, argumentTypes[index])
		}
	}
	return &ast.FunctionResolution{
		ReturnType:        common,
		NullableBehaviour: nullable,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}
}

// passThroughArgument returns a resolver that types a call as its argument at index, for
// array functions that return the array they are given.
//
// Takes index (int) which is the position of the array argument.
//
// Returns polymorphicResolver which types the call.
func passThroughArgument(index int) polymorphicResolver {
	return func(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
		return resolveArrayPassthrough(argumentTypes, index)
	}
}

// ignoringArguments returns a resolver for a function whose type does not depend on its
// arguments.
//
// Takes resolve (func() (*ast.FunctionResolution, error)) which types the call.
//
// Returns polymorphicResolver which types the call.
func ignoringArguments(resolve func() (*ast.FunctionResolution, error)) polymorphicResolver {
	return func([]sqlmodel.SQLType) (*ast.FunctionResolution, error) {
		return resolve()
	}
}

// resolveArrayAgg resolves array_agg and list aggregate return types.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which describes the call argument types in
// declared order.
//
// Returns *ast.FunctionResolution which wraps the element type in an array, or nil when
// the call has no arguments.
// Returns error which is always nil.
func resolveArrayAgg(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < 1 {
		return nil, nil
	}

	elementType := argumentTypes[0]

	return &ast.FunctionResolution{
		ReturnType:        newArrayType(elementType),
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       true,
		ReturnsSet:        false,
		ElementsKnown:     true,
	}, nil
}

// resolveUnnest resolves the unnest set-returning function.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which describes the call argument types in
// declared order.
//
// Returns *ast.FunctionResolution which describes the set-returning element or record
// type, or nil when the call has no arguments.
// Returns error which is always nil.
func resolveUnnest(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < 1 {
		return nil, nil
	}

	if len(argumentTypes) > 1 {
		return &ast.FunctionResolution{
			ReturnType:        sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "record"),
			NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
			NullingArgument:   0,
			DataAccess:        sqlmodel.DataAccessReadOnly,
			ReturnsSet:        true,
			IsAggregate:       false,
			ElementsKnown:     false,
		}, nil
	}

	arrayType := argumentTypes[0]
	if arrayType.Category == sqlmodel.TypeCategoryArray && arrayType.ElementType != nil {
		return &ast.FunctionResolution{
			ReturnType:        *arrayType.ElementType,
			NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
			NullingArgument:   0,
			DataAccess:        sqlmodel.DataAccessReadOnly,
			ReturnsSet:        true,
			IsAggregate:       false,
			ElementsKnown:     false,
		}, nil
	}

	return &ast.FunctionResolution{
		ReturnType:        sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		ReturnsSet:        true,
		IsAggregate:       false,
		ElementsKnown:     false,
	}, nil
}

// resolveArrayPassthrough returns the array argument's type unchanged for array_append,
// array_cat, array_remove, array_replace, and array_prepend.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which describes the call argument types in
// declared order.
// Takes arrayArgumentIndex (int) which is the position of the array argument to pass
// through.
//
// Returns *ast.FunctionResolution which mirrors the array argument type, or nil when too
// few arguments are provided.
// Returns error which is always nil.
func resolveArrayPassthrough(argumentTypes []sqlmodel.SQLType, arrayArgumentIndex int) (*ast.FunctionResolution, error) {
	if len(argumentTypes) <= arrayArgumentIndex {
		return nil, nil
	}

	return &ast.FunctionResolution{
		ReturnType:        argumentTypes[arrayArgumentIndex],
		NullableBehaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveIdentityAggregate returns the first argument's type for aggregates such as min
// and max where the result type matches the input type.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which describes the call argument types in
// declared order.
//
// Returns *ast.FunctionResolution which mirrors the first argument type, or nil when the
// call has no arguments.
// Returns error which is always nil.
func resolveIdentityAggregate(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < 1 {
		return nil, nil
	}

	return &ast.FunctionResolution{
		ReturnType:        argumentTypes[0],
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       true,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveSum resolves the sum aggregate return type. DuckDB promotes every integer input
// (regardless of width, including BIGINT/HUGEINT) to HUGEINT; floating-point input stays
// double precision (float8); and DECIMAL input keeps the decimal category.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which describes the call argument types in
// declared order.
//
// Returns *ast.FunctionResolution which describes the promoted return type, or nil when
// the call has no arguments.
// Returns error which is always nil.
func resolveSum(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < 1 {
		return nil, nil
	}

	return &ast.FunctionResolution{
		ReturnType:        summedType(argumentTypes[0]),
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       true,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// summedType returns the type DuckDB sums values of a type in: HUGEINT for every integer,
// DOUBLE for a floating-point value, and a DECIMAL otherwise.
//
// Takes argumentType (sqlmodel.SQLType) which is the type of the values summed.
//
// Returns sqlmodel.SQLType which is the sum's type.
func summedType(argumentType sqlmodel.SQLType) sqlmodel.SQLType {
	switch argumentType.Category {
	case sqlmodel.TypeCategoryInteger:
		return builtinTypeMap["hugeint"]
	case sqlmodel.TypeCategoryFloat:
		return builtinTypeMap["double"]
	default:
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "numeric")
	}
}

// resolveAvg resolves the avg aggregate return type. DuckDB returns double precision
// (float8) for every numeric input, including integer, floating-point, and decimal, so
// avg over any numeric column resolves to float8.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which describes the call argument types in
// declared order.
//
// Returns *ast.FunctionResolution which describes the return type, or nil when the call
// has no arguments.
// Returns error which is always nil.
func resolveAvg(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < 1 {
		return nil, nil
	}

	argumentType := argumentTypes[0]
	var returnType sqlmodel.SQLType

	switch argumentType.Category {
	case sqlmodel.TypeCategoryInteger, sqlmodel.TypeCategoryFloat, sqlmodel.TypeCategoryDecimal:
		returnType = builtinTypeMap["double"]
	default:
		returnType = sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "numeric")
	}

	return &ast.FunctionResolution{
		ReturnType:        returnType,
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       true,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveRounding types ceil, ceiling and floor as DuckDB does. A float or a decimal
// keeps its type, and an integer gives a DOUBLE.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which describes the call argument types.
//
// Returns *ast.FunctionResolution which describes the return type, or nil without an
// argument.
// Returns error which is always nil.
func resolveRounding(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) == 0 {
		return nil, nil
	}
	returnType := argumentTypes[0]
	if returnType.Category == sqlmodel.TypeCategoryInteger {
		returnType = builtinTypeMap[doublePrecisionName]
	}
	return &ast.FunctionResolution{
		ReturnType:        returnType,
		NullableBehaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveTypeof resolves the typeof introspection function, a VARCHAR naming its
// argument's type that is never NULL: typeof(NULL) is 'NULL'.
//
// Returns *ast.FunctionResolution which describes the varchar return type.
// Returns error which is always nil.
func resolveTypeof() (*ast.FunctionResolution, error) {
	return &ast.FunctionResolution{
		ReturnType:        sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar"),
		NullableBehaviour: sqlmodel.FunctionNullableNeverNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveStructPack resolves the struct_pack constructor.
//
// Returns *ast.FunctionResolution which describes the struct return type.
// Returns error which is always nil.
func resolveStructPack() (*ast.FunctionResolution, error) {
	return &ast.FunctionResolution{
		ReturnType:        sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "struct"),
		NullableBehaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveStructExtract resolves the struct_extract field accessor.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which describes the call argument types in
// declared order.
//
// Returns *ast.FunctionResolution which describes the unknown field return type.
// Returns error which is always nil.
func resolveStructExtract(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) >= 1 && argumentTypes[0].Category == sqlmodel.TypeCategoryStruct {
		return &ast.FunctionResolution{
			ReturnType:        sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
			NullableBehaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
			NullingArgument:   0,
			DataAccess:        sqlmodel.DataAccessReadOnly,
			IsAggregate:       false,
			ReturnsSet:        false,
			ElementsKnown:     false,
		}, nil
	}
	return &ast.FunctionResolution{
		ReturnType:        sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
		NullableBehaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveRow describes DuckDB's unnamed struct for a row expression.
//
// Fields retain their input order and are nullable. The struct itself is never NULL.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which are the values' types.
//
// Returns *ast.FunctionResolution which is the struct's type.
// Returns error which is always nil.
func resolveRow(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	row := sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "struct")
	for index := range argumentTypes {
		fieldType := argumentTypes[index]
		fieldType.Nullable = true
		row.StructFields = append(row.StructFields, sqlmodel.StructField{
			Name:       positionalFieldPrefix + strconv.Itoa(index+1),
			SQLType:    fieldType,
			Positional: true,
		})
	}
	return &ast.FunctionResolution{
		ReturnType:        row,
		NullableBehaviour: sqlmodel.FunctionNullableNeverNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveStructInsert resolves the struct_insert mutation.
//
// Named additions arrive as two arguments: the original struct and a struct containing
// the new fields. The result contains the original fields followed by the new ones and is
// never NULL. A non-struct original gives an unknown type. Other calls with arguments
// keep the first argument's type.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which describes the call argument types in
// declared order.
//
// Returns *ast.FunctionResolution which is the extended struct, mirrors the input struct
// type, or returns a generic struct when no arguments are supplied.
// Returns error which is always nil.
func resolveStructInsert(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) == minimumStructInsertArguments && argumentTypes[1].Category == sqlmodel.TypeCategoryStruct {
		return resolveStructFieldsInserted(argumentTypes[0], argumentTypes[1]), nil
	}
	if len(argumentTypes) >= 1 {
		return &ast.FunctionResolution{
			ReturnType:        argumentTypes[0],
			NullableBehaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
			NullingArgument:   0,
			DataAccess:        sqlmodel.DataAccessReadOnly,
			IsAggregate:       false,
			ReturnsSet:        false,
			ElementsKnown:     false,
		}, nil
	}
	return &ast.FunctionResolution{
		ReturnType:        sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "struct"),
		NullableBehaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveStructFieldsInserted types a struct_insert call that adds named fields: the
// fields of the struct it adds to followed by the added fields, in one pass.
//
// The result is never NULL, as a struct DuckDB builds is not; adding to a value that is
// no struct gives a value of unknown type.
//
// Takes base (sqlmodel.SQLType) which is the type of the struct added to.
// Takes added (sqlmodel.SQLType) which is the struct of the added fields.
//
// Returns *ast.FunctionResolution which describes the extended struct.
func resolveStructFieldsInserted(base, added sqlmodel.SQLType) *ast.FunctionResolution {
	resolution := &ast.FunctionResolution{
		ReturnType:        sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}
	if base.Category != sqlmodel.TypeCategoryStruct {
		return resolution
	}
	extended := sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, structEngineName)
	extended.StructFields = make([]sqlmodel.StructField, 0, len(base.StructFields)+len(added.StructFields))
	extended.StructFields = append(extended.StructFields, base.StructFields...)
	for index := range added.StructFields {
		field := &added.StructFields[index]
		fieldType := field.SQLType
		fieldType.Nullable = true
		extended.StructFields = append(extended.StructFields,
			sqlmodel.StructField{Name: field.Name, SQLType: fieldType, Positional: field.Positional})
	}
	resolution.ReturnType = extended
	resolution.NullableBehaviour = sqlmodel.FunctionNullableNeverNull
	return resolution
}

// resolveMapKeys resolves map_keys to an array of the map's key type, whose elements are
// never NULL, as a map holds no NULL key.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which describes the call argument types in
// declared order.
//
// Returns *ast.FunctionResolution which describes the array return type.
// Returns error which is always nil.
func resolveMapKeys(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) >= 1 && argumentTypes[0].KeyType != nil {
		key := *argumentTypes[0].KeyType
		key.Nullable = false
		resolution, err := resolveMapComponent(&key)
		resolution.ElementsKnown = true
		return resolution, err
	}
	return resolveMapComponent(nil)
}

// resolveMapValues resolves map_values to an array of the map's value type.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which describes the call argument types in
// declared order.
//
// Returns *ast.FunctionResolution which describes the array return type.
// Returns error which is always nil.
func resolveMapValues(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) >= 1 {
		return resolveMapComponent(argumentTypes[0].ElementType)
	}
	return resolveMapComponent(nil)
}

// resolveMapComponent wraps a map's key or value type in an array resolution, falling
// back to a generic list when no type is known.
//
// Takes extractedType (*sqlmodel.SQLType) which is the key or value type pulled from the
// source map.
//
// Returns *ast.FunctionResolution which describes the array return type.
// Returns error which is always nil.
func resolveMapComponent(extractedType *sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if extractedType != nil {
		return &ast.FunctionResolution{
			ReturnType:        newArrayType(*extractedType),
			NullableBehaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
			NullingArgument:   0,
			DataAccess:        sqlmodel.DataAccessReadOnly,
			IsAggregate:       false,
			ReturnsSet:        false,
			ElementsKnown:     false,
		}, nil
	}
	return &ast.FunctionResolution{
		ReturnType:        sqlmodel.NewSQLType(sqlmodel.TypeCategoryArray, fallbackListEngineName),
		NullableBehaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveMapEntries resolves map_entries to a generic list of entries.
//
// Returns *ast.FunctionResolution which describes the array return type.
// Returns error which is always nil.
func resolveMapEntries() (*ast.FunctionResolution, error) {
	return &ast.FunctionResolution{
		ReturnType:        sqlmodel.NewSQLType(sqlmodel.TypeCategoryArray, fallbackListEngineName),
		NullableBehaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveElementAt resolves element_at and map_extract, which look a key up in a map and
// return a list holding its value, or an empty list when the map has no such key.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which describes the call argument types in
// declared order.
//
// Returns *ast.FunctionResolution which describes the list of the map's value type, or a
// list of unknown elements when the map's value type is not known.
// Returns error which is always nil.
func resolveElementAt(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) >= 1 && argumentTypes[0].Category == sqlmodel.TypeCategoryMap {
		return resolveMapComponent(argumentTypes[0].ElementType)
	}
	return resolveMapComponent(nil)
}

// resolveExtractValue resolves map_extract_value, list_extract, array_extract and
// list_element, which return the single value a map holds for a key or a list holds at an
// index, NULL when there is none.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which describes the call argument types in
// declared order.
//
// Returns *ast.FunctionResolution which describes the element type, which is unknown when
// the container's element type is not known.
// Returns error which is always nil.
func resolveExtractValue(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	elementType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	if len(argumentTypes) >= 1 && argumentTypes[0].ElementType != nil {
		elementType = *argumentTypes[0].ElementType
	}
	return &ast.FunctionResolution{
		ReturnType:        elementType,
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveListValue resolves list_value and array_value, which build a list of their
// arguments, typed as a list of the first argument's type whose elements may be NULL when
// an argument may be, a NULL literal included.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which describes the call argument types in
// declared order.
//
// Returns *ast.FunctionResolution which describes the list type, a list of unknown
// elements when no argument's type is known.
// Returns error which is always nil.
func resolveListValue(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	listType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryArray, fallbackListEngineName)
	for index := range argumentTypes {
		if argumentTypes[index].Category != sqlmodel.TypeCategoryUnknown {
			element := argumentTypes[index]
			element.Nullable = slices.ContainsFunc(argumentTypes, func(argument sqlmodel.SQLType) bool { return argument.Nullable })
			listType = newArrayType(element)
			break
		}
	}
	return &ast.FunctionResolution{
		ReturnType:        listType,
		NullableBehaviour: sqlmodel.FunctionNullableNeverNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     listType.ElementType != nil,
	}, nil
}

// resolveListFilter resolves list_filter and its aliases to the type of the list it
// filters.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which describes the call argument types in
// declared order: the list, then the lambda.
//
// Returns *ast.FunctionResolution which mirrors the input list type, falling back to a
// generic list when no input is supplied.
// Returns error which is always nil.
func resolveListFilter(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) >= 1 {
		return listFunctionResolution(argumentTypes[0], sqlmodel.FunctionNullableNullWhenArgumentNull), nil
	}
	return listFunctionResolution(sqlmodel.NewSQLType(sqlmodel.TypeCategoryArray, fallbackListEngineName),
		sqlmodel.FunctionNullableNullWhenArgumentNull), nil
}

// resolveListTransform resolves list_transform and its aliases to a list of what the
// lambda gives for each element: the lambda's type when the body's type is known, and
// otherwise the input list's type, as for a body that returns its element unchanged.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which describes the call argument types in
// declared order: the list, then the lambda.
//
// Returns *ast.FunctionResolution which is the transformed list type.
// Returns error which is always nil.
func resolveListTransform(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) >= 2 && argumentTypes[1].Category != sqlmodel.TypeCategoryUnknown {
		return listFunctionResolution(newArrayType(argumentTypes[1]), sqlmodel.FunctionNullableNullWhenArgumentNull), nil
	}
	return resolveListFilter(argumentTypes)
}

// resolveListReduce types a list reduction from its accumulator and starting value.
//
// The result uses the accumulator's type, falling back to the lambda type when unknown,
// and may be NULL.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which describes the call argument types in
// declared order: the list, the lambda, and an optional initial value.
//
// Returns *ast.FunctionResolution which is the folded value's type.
// Returns error which is always nil.
func resolveListReduce(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	accumulator := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	if len(argumentTypes) >= 1 && argumentTypes[0].ElementType != nil {
		accumulator = *argumentTypes[0].ElementType
	}
	if len(argumentTypes) > reduceInitialArgument {
		accumulator = widerReduceType(accumulator, argumentTypes[reduceInitialArgument])
	}
	if accumulator.Category == sqlmodel.TypeCategoryUnknown && len(argumentTypes) >= 2 {
		accumulator = argumentTypes[1]
	}
	return listFunctionResolution(accumulator, sqlmodel.FunctionNullableCalledOnNull), nil
}

// widerReduceType returns the type a reduce accumulates in when the call gives a starting
// value: the starting value's type when the element's is unknown or narrower in the same
// category, and the element's otherwise, as DuckDB requires the starting value to be of
// the element's type or a wider one.
//
// Takes element (sqlmodel.SQLType) which is the list's element type.
// Takes initial (sqlmodel.SQLType) which is the starting value's type.
//
// Returns sqlmodel.SQLType which is the accumulator's type.
func widerReduceType(element, initial sqlmodel.SQLType) sqlmodel.SQLType {
	switch {
	case initial.Category == sqlmodel.TypeCategoryUnknown:
		return element
	case element.Category == sqlmodel.TypeCategoryUnknown:
		return initial
	case element.Category != initial.Category:
		return element
	case element.Category == sqlmodel.TypeCategoryInteger && promotionRank(initial) > promotionRank(element):
		return initial
	case element.Category == sqlmodel.TypeCategoryFloat && initial.Width > element.Width:
		return initial
	default:
		return element
	}
}

// listFunctionResolution types a call of a list function that reads its list as the first
// argument, so a result that is NULL only when the list is NULL names that argument.
//
// Takes returnType (sqlmodel.SQLType) which is the call's type.
// Takes nullable (sqlmodel.FunctionNullableBehaviour) which says when the call is NULL.
//
// Returns *ast.FunctionResolution which types the call.
func listFunctionResolution(returnType sqlmodel.SQLType, nullable sqlmodel.FunctionNullableBehaviour) *ast.FunctionResolution {
	return &ast.FunctionResolution{
		ReturnType:        returnType,
		NullableBehaviour: nullable,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}
}

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
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// arrayConstructor is the name the parser gives the call an ARRAY[...] constructor reads
	// as. ARRAY is a reserved word, so no function of the catalogue has the name, and
	// PostgreSQL names the column of an unaliased constructor the same.
	arrayConstructor = "array"

	// typeNameNumeric is the type an aggregate or an array of mixed integers and decimals
	// widens to.
	typeNameNumeric = "numeric"
)

// functionResolvers maps each polymorphic PostgreSQL function, by lower-cased name, to
// the resolver that types a call to it from its argument types and, for the enum support
// functions, the catalogue.
var functionResolvers = map[string]func(*sqlmodel.Catalogue, []sqlmodel.SQLType) (*ast.FunctionResolution, error){
	"array_agg": ignoreCatalogue(resolveArrayAgg),
	arrayConstructor: ignoreCatalogue(func(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
		return resolveArrayConstructor(argumentTypes), nil
	}),
	"unnest":                   ignoreCatalogue(resolveUnnest),
	"array_append":             arrayPassthrough(0),
	"array_cat":                arrayPassthrough(0),
	"array_remove":             arrayPassthrough(0),
	"array_replace":            arrayPassthrough(0),
	"array_prepend":            arrayPassthrough(1),
	"jsonb_populate_record":    populateRecord(false),
	"json_populate_record":     populateRecord(false),
	"jsonb_populate_recordset": populateRecord(true),
	"json_populate_recordset":  populateRecord(true),
	"jsonb_to_record":          toRecord(false),
	"json_to_record":           toRecord(false),
	"jsonb_to_recordset":       toRecord(true),
	"json_to_recordset":        toRecord(true),
	"min":                      ignoreCatalogue(resolveIdentityAggregate),
	"max":                      ignoreCatalogue(resolveIdentityAggregate),
	"sum":                      ignoreCatalogue(resolveSum),
	"avg":                      ignoreCatalogue(resolveAvg),
	"percentile_cont":          ignoreCatalogue(resolvePercentileCont),
	"percentile_disc":          orderedSetValue(1),
	"mode":                     orderedSetValue(0),
	"lag":                      ignoreCatalogue(resolveFirstArgumentType),
	"lead":                     ignoreCatalogue(resolveFirstArgumentType),
	"first_value":              ignoreCatalogue(resolveFirstArgumentType),
	"last_value":               ignoreCatalogue(resolveFirstArgumentType),
	"nth_value":                ignoreCatalogue(resolveFirstArgumentType),
	"nullif":                   ignoreCatalogue(resolveFirstArgumentType),
	"pg_typeof": func(*sqlmodel.Catalogue, []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
		return resolvePgTypeof()
	},
	"enum_first": resolveEnumLabel,
	"enum_last":  resolveEnumLabel,
	"enum_range": resolveEnumRange,
	"lower":      ignoreCatalogue(resolveRangeBound),
	"upper":      ignoreCatalogue(resolveRangeBound),
	"range_agg":  ignoreCatalogue(resolveRangeAggregate),
}

// resolveCommonArgument returns the signature of COALESCE, GREATEST or LEAST, typed by
// the common type of their known arguments.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which is the call's argument types.
//
// Returns *ast.FunctionResolution which is the resolved signature.
func (engine *Dialect) resolveCommonArgument(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
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
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}
}

// resolveArrayAgg returns the array_agg signature, producing an array whose element type
// matches the first argument's type, its elements NULL only where the argument may be.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which is the call's argument types.
//
// Returns *ast.FunctionResolution which is the resolved signature, or nil when no
// argument was provided.
// Returns error when resolution fails.
func resolveArrayAgg(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < 1 {
		return nil, nil
	}

	return &ast.FunctionResolution{
		ReturnType:        arrayTypeOf(argumentTypes[0], 1),
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		NullingArgument:   0,
		IsAggregate:       true,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		ReturnsSet:        false,
		ElementsKnown:     true,
	}, nil
}

// resolveArrayConstructor types an ARRAY constructor from its elements and their
// nullability.
//
// The array itself is never NULL. Element nullability follows its inputs; incompatible or
// wholly unknown elements yield an unknown type.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which is the types of the elements.
//
// Returns *ast.FunctionResolution which is the resolved signature.
func resolveArrayConstructor(argumentTypes []sqlmodel.SQLType) *ast.FunctionResolution {
	element, known := commonElementType(argumentTypes)
	returnType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	if known {
		element.Nullable = slices.ContainsFunc(argumentTypes, func(argument sqlmodel.SQLType) bool { return argument.Nullable })
		returnType = arrayTypeOf(element, 1)
	}
	return &ast.FunctionResolution{
		ReturnType:        returnType,
		NullableBehaviour: sqlmodel.FunctionNullableNeverNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     known,
	}
}

// commonElementType returns the type an array constructor's elements meet in: the type of
// every element of known type when they share it, the wider of two integers, and numeric
// or float8 where integers, decimals and floats meet.
//
// Takes elements ([]sqlmodel.SQLType) which are the elements' types.
//
// Returns sqlmodel.SQLType which is the common type.
// Returns bool which is false when no element's type is known or two of them meet in no
// type.
func commonElementType(elements []sqlmodel.SQLType) (sqlmodel.SQLType, bool) {
	var common sqlmodel.SQLType
	known := false
	for index := range elements {
		if elements[index].Category == sqlmodel.TypeCategoryUnknown {
			continue
		}
		if !known {
			common, known = elements[index], true
			continue
		}
		merged, meets := meetingType(common, elements[index])
		if !meets {
			return sqlmodel.SQLType{}, false
		}
		common = merged
	}
	return common, known
}

// meetingType returns the type two array elements meet in.
//
// Takes left (sqlmodel.SQLType) which is one element's type.
// Takes right (sqlmodel.SQLType) which is the other's.
//
// Returns sqlmodel.SQLType which is the common type.
// Returns bool which is false when they meet in no type.
func meetingType(left, right sqlmodel.SQLType) (sqlmodel.SQLType, bool) {
	switch {
	case left.Category == sqlmodel.TypeCategoryInteger && right.Category == sqlmodel.TypeCategoryInteger:
		if right.Width > left.Width {
			return right, true
		}
		return left, true
	case left.Category == right.Category && left.EngineName == right.EngineName:
		return left, true
	case isNumber(left) && isNumber(right):
		if left.Category == sqlmodel.TypeCategoryFloat || right.Category == sqlmodel.TypeCategoryFloat {
			return typeFloat8, true
		}
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, typeNameNumeric), true
	default:
		return sqlmodel.SQLType{}, false
	}
}

// resolveUnnest returns the unnest signature whose return type is the element type of the
// first argument's array type.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which is the call's argument types.
//
// Returns *ast.FunctionResolution which is the resolved signature, or nil when no
// argument was provided.
// Returns error when resolution fails.
func resolveUnnest(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < 1 {
		return nil, nil
	}

	if len(argumentTypes) > 1 {
		return &ast.FunctionResolution{
			ReturnType:        sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "record"),
			NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
			NullingArgument:   0,
			ReturnsSet:        true,
			DataAccess:        sqlmodel.DataAccessReadOnly,
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
			ReturnsSet:        true,
			DataAccess:        sqlmodel.DataAccessReadOnly,
			IsAggregate:       false,
			ElementsKnown:     false,
		}, nil
	}

	return &ast.FunctionResolution{
		ReturnType:        sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		NullingArgument:   0,
		ReturnsSet:        true,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ElementsKnown:     false,
	}, nil
}

// resolveArrayPassthrough returns the signature for array functions whose return type
// matches the array argument at arrayArgumentIndex.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which is the call's argument types.
// Takes arrayArgumentIndex (int) which is the index of the array argument.
//
// Returns *ast.FunctionResolution which is the resolved signature, or nil when not enough
// arguments were provided.
// Returns error when resolution fails.
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

// resolvePopulateRecord returns the signature for json_populate_record and its
// set-returning variant, mapping the return type to the first argument's row type.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which is the call's argument types.
// Takes returnsSet (bool) which signals the set-returning form.
//
// Returns *ast.FunctionResolution which is the resolved signature, or nil when no
// argument was provided.
// Returns error when resolution fails.
func resolvePopulateRecord(argumentTypes []sqlmodel.SQLType, returnsSet bool) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < 1 {
		return nil, nil
	}

	return &ast.FunctionResolution{
		ReturnType:        argumentTypes[0],
		NullableBehaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
		NullingArgument:   0,
		ReturnsSet:        returnsSet,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ElementsKnown:     false,
	}, nil
}

// resolveToRecord returns the signature for json_to_record and json_to_recordset, both of
// which return a generic record type.
//
// Takes returnsSet (bool) which signals the set-returning form.
//
// Returns *ast.FunctionResolution which is the resolved signature.
// Returns error when resolution fails.
func resolveToRecord(returnsSet bool) (*ast.FunctionResolution, error) {
	return &ast.FunctionResolution{
		ReturnType:        sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "record"),
		NullableBehaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
		NullingArgument:   0,
		ReturnsSet:        returnsSet,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ElementsKnown:     false,
	}, nil
}

// resolveIdentityAggregate returns the signature for MIN and MAX whose return type
// matches the first argument's type.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which is the call's argument types.
//
// Returns *ast.FunctionResolution which is the resolved signature, or nil when no
// argument was provided.
// Returns error when resolution fails.
func resolveIdentityAggregate(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < 1 {
		return nil, nil
	}

	return &ast.FunctionResolution{
		ReturnType:        argumentTypes[0],
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		NullingArgument:   0,
		IsAggregate:       true,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveFirstArgumentType returns the signature of a function whose result has the type
// of its first argument: the window value functions lag, lead, first_value, last_value
// and nth_value, and nullif. The result may be NULL whatever the arguments, as when lag
// runs off the start of its partition.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which is the call's argument types.
//
// Returns *ast.FunctionResolution which is the resolved signature, or nil when no
// argument was provided.
// Returns error when resolution fails.
func resolveFirstArgumentType(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < 1 {
		return nil, nil
	}

	return &ast.FunctionResolution{
		ReturnType:        argumentTypes[0],
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		NullingArgument:   0,
		IsAggregate:       false,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveSum returns the SUM signature, widening small integers to int8 and other
// numerics to numeric or float8 per Postgres rules, and summing intervals to an interval.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which is the call's argument types.
//
// Returns *ast.FunctionResolution which is the resolved signature, or nil when no
// argument was provided.
// Returns error when resolution fails.
func resolveSum(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < 1 {
		return nil, nil
	}

	argumentType := argumentTypes[0]
	var returnType sqlmodel.SQLType

	switch {
	case argumentType.Temporal == sqlmodel.TemporalKindInterval:
		returnType = argumentType
	case argumentType.Category == sqlmodel.TypeCategoryInteger:
		if isSmallInteger(argumentType.EngineName) {
			returnType = sqlmodel.NewIntegerType("int8", widthBigint, false)
		} else {
			returnType = sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, typeNameNumeric)
		}
	case argumentType.Category == sqlmodel.TypeCategoryFloat:
		returnType = typeFloat8
	default:
		returnType = sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, typeNameNumeric)
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

// resolveAvg returns the AVG signature, producing an interval for intervals, float8 for
// floating inputs and numeric otherwise.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which is the call's argument types.
//
// Returns *ast.FunctionResolution which is the resolved signature, or nil when no
// argument was provided.
// Returns error when resolution fails.
func resolveAvg(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < 1 {
		return nil, nil
	}

	argumentType := argumentTypes[0]
	var returnType sqlmodel.SQLType

	switch {
	case argumentType.Temporal == sqlmodel.TemporalKindInterval:
		returnType = argumentType
	case argumentType.Category == sqlmodel.TypeCategoryFloat:
		returnType = typeFloat8
	default:
		returnType = sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, typeNameNumeric)
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

// resolvePercentileCont returns the signature of percentile_cont, an ordered-set
// aggregate whose arguments are its fraction and then the value its WITHIN GROUP clause
// sorts: it interpolates an interval between intervals and a float8 between any other
// values, and gives an array of them when the fraction is an array of fractions.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which is the call's argument types.
//
// Returns *ast.FunctionResolution which is the resolved signature, or nil when the call
// has no sorted value or the fraction's type is unknown, so whether it is an array is.
// Returns error when resolution fails.
func resolvePercentileCont(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < 2 || argumentTypes[0].Category == sqlmodel.TypeCategoryUnknown {
		return nil, nil
	}
	interpolated := typeFloat8
	if argumentTypes[1].Temporal == sqlmodel.TemporalKindInterval {
		interpolated = argumentTypes[1]
	}
	return orderedSetResolution(perFraction(argumentTypes[0], interpolated)), nil
}

// resolveOrderedSetValue returns the signature of an ordered-set aggregate that picks one
// of the values its WITHIN GROUP clause sorts, percentile_disc or mode, which therefore
// has their type; percentile_disc gives an array of them when its fraction is an array of
// fractions.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which is the call's argument types.
// Takes valueIndex (int) which is the position of the sorted value among them.
//
// Returns *ast.FunctionResolution which is the resolved signature, or nil when the call
// has no sorted value or percentile_disc's fraction is of unknown type, so whether it is
// an array is.
// Returns error when resolution fails.
func resolveOrderedSetValue(argumentTypes []sqlmodel.SQLType, valueIndex int) (*ast.FunctionResolution, error) {
	if len(argumentTypes) <= valueIndex || valueIndex > 0 && argumentTypes[0].Category == sqlmodel.TypeCategoryUnknown {
		return nil, nil
	}
	if valueIndex == 0 {
		return orderedSetResolution(argumentTypes[0]), nil
	}
	return orderedSetResolution(perFraction(argumentTypes[0], argumentTypes[valueIndex])), nil
}

// perFraction returns the type of a percentile: the value's type for one fraction, and an
// array of it for an array of fractions.
//
// Takes fraction (sqlmodel.SQLType) which is the type of the fraction argument.
// Takes value (sqlmodel.SQLType) which is the type of one percentile.
//
// Returns sqlmodel.SQLType which is the result's type.
func perFraction(fraction, value sqlmodel.SQLType) sqlmodel.SQLType {
	if fraction.Category == sqlmodel.TypeCategoryArray {
		return arrayTypeOf(value, 1)
	}
	return value
}

// orderedSetResolution returns the resolution of an ordered-set aggregate: NULL when its
// group has no row, and reading no data.
//
// Takes returnType (sqlmodel.SQLType) which is the aggregate's result type.
//
// Returns *ast.FunctionResolution which is the resolution.
func orderedSetResolution(returnType sqlmodel.SQLType) *ast.FunctionResolution {
	return &ast.FunctionResolution{
		ReturnType:        returnType,
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		NullingArgument:   0,
		IsAggregate:       true,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}
}

// resolvePgTypeof returns the pg_typeof signature, which always reports text.
//
// Returns *ast.FunctionResolution which is the resolved signature.
// Returns error when resolution fails.
func resolvePgTypeof() (*ast.FunctionResolution, error) {
	return &ast.FunctionResolution{
		ReturnType:        sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"),
		NullableBehaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// isSmallInteger reports whether engineName names a small Postgres integer (int2 or int4)
// that SUM widens to int8.
//
// Takes engineName (string) which is the Postgres type's engine name.
//
// Returns bool which is true for int2 or int4.
func isSmallInteger(engineName string) bool {
	switch engineName {
	case "int2", "int4":
		return true
	default:
		return false
	}
}

// resolveFunctionCall resolves a call to a polymorphic PostgreSQL function, whose return
// type depends on its argument types, through functionResolvers.
//
// It computes the return type of array, JSON, aggregate, conditional, range, enum and
// type-introspection functions the standard overload resolution cannot type, and returns
// nil, nil for any other function so the caller falls back to the standard catalogue
// lookup.
//
// Takes catalogue (*sqlmodel.Catalogue) which supplies the schema objects available to
// the query.
// Takes name (string) which is the function name.
// Takes argumentTypes ([]sqlmodel.SQLType) which is the list of argument types.
//
// Returns *ast.FunctionResolution which is the resolved signature, or nil when no
// polymorphic rule applies.
// Returns error when resolution fails.
func resolveFunctionCall(
	catalogue *sqlmodel.Catalogue,
	name string,
	argumentTypes []sqlmodel.SQLType,
) (*ast.FunctionResolution, error) {
	resolve, known := functionResolvers[strings.ToLower(name)]
	if !known {
		return nil, nil
	}
	return resolve(catalogue, argumentTypes)
}

// arrayPassthrough adapts resolveArrayPassthrough to functionResolvers for a function
// whose array is the argument at an index.
//
// Takes arrayArgumentIndex (int) which is the index of the array argument.
//
// Returns func(*sqlmodel.Catalogue, []sqlmodel.SQLType) (*ast.FunctionResolution, error)
// which types the call.
func arrayPassthrough(arrayArgumentIndex int) func(*sqlmodel.Catalogue, []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	return ignoreCatalogue(func(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
		return resolveArrayPassthrough(argumentTypes, arrayArgumentIndex)
	})
}

// populateRecord adapts resolvePopulateRecord to functionResolvers.
//
// Takes returnsSet (bool) which is true for the recordset forms.
//
// Returns func(*sqlmodel.Catalogue, []sqlmodel.SQLType) (*ast.FunctionResolution, error)
// which types the call.
func populateRecord(returnsSet bool) func(*sqlmodel.Catalogue, []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	return ignoreCatalogue(func(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
		return resolvePopulateRecord(argumentTypes, returnsSet)
	})
}

// toRecord adapts resolveToRecord to functionResolvers.
//
// Takes returnsSet (bool) which is true for the recordset forms.
//
// Returns func(*sqlmodel.Catalogue, []sqlmodel.SQLType) (*ast.FunctionResolution, error)
// which types the call.
func toRecord(returnsSet bool) func(*sqlmodel.Catalogue, []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	return func(*sqlmodel.Catalogue, []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
		return resolveToRecord(returnsSet)
	}
}

// orderedSetValue adapts resolveOrderedSetValue to functionResolvers for an ordered-set
// aggregate whose value is the argument at an index.
//
// Takes valueIndex (int) which is the index of the argument the result takes its type
// from.
//
// Returns func(*sqlmodel.Catalogue, []sqlmodel.SQLType) (*ast.FunctionResolution, error)
// which types the call.
func orderedSetValue(valueIndex int) func(*sqlmodel.Catalogue, []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	return ignoreCatalogue(func(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
		return resolveOrderedSetValue(argumentTypes, valueIndex)
	})
}

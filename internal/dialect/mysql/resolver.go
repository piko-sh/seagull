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

package mysql

import (
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// minArgumentsIf is the minimum argument count for the IF function.
	minArgumentsIf = 3

	// minArgumentsIfNull is the minimum argument count for IFNULL.
	minArgumentsIfNull = 2

	// minArgumentsSingleArgumentFunction caps single-argument resolver entry.
	minArgumentsSingleArgumentFunction = 1

	// promotionRankText ranks text categories highest during promotion.
	promotionRankText = 4

	// promotionRankFloat ranks float below text but above decimal.
	promotionRankFloat = 3

	// promotionRankDecimal ranks decimal above integer in promotion order.
	promotionRankDecimal = 2

	// promotionRankInteger is the lowest numeric promotion rank.
	promotionRankInteger = 1

	// promotionRankDefault applies to unrecognised type categories.
	promotionRankDefault = 0

	// doubleTypeName names DOUBLE in the type table, the type MySQL reads a value of no
	// numeric type as in a numeric function.
	doubleTypeName = "double"

	// bigintTypeName names BIGINT in the type table.
	bigintTypeName = "bigint"

	// timestampAddShiftedArgument is the position of the value TIMESTAMPADD shifts, after
	// its unit and interval.
	timestampAddShiftedArgument = 2
)

// shiftedDateArguments maps each function that shifts a date and time to the position of
// the value it shifts: the first argument, or for TIMESTAMPADD the one after its unit and
// interval.
var shiftedDateArguments = map[string]int{
	"date_add": 0, "date_sub": 0, "adddate": 0, "subdate": 0, "addtime": 0, "subtime": 0,
	"timestampadd": timestampAddShiftedArgument,
}

// functionResolver implements dialect.FunctionResolverPort for polymorphic MySQL
// functions whose return types depend on their argument types.
type functionResolver struct{}

// newFunctionResolver creates a new MySQL function resolver.
//
// Returns *functionResolver which resolves polymorphic call sites.
func newFunctionResolver() *functionResolver {
	return &functionResolver{}
}

// ResolveFunctionCall resolves a polymorphic MySQL function call.
//
// Inspects argument types to compute return types for conditional, aggregate, JSON, and
// string functions. Returns nil for non-polymorphic functions so the caller falls back to
// the standard catalogue lookup.
//
// Takes _ (*sqlmodel.Catalogue) which is the catalogue context (unused).
// Takes name (string) which is the function name to resolve.
// Takes _ (string) which is the schema scope (unused).
// Takes argumentTypes ([]sqlmodel.SQLType) which carries the actual argument types
// provided at the call site.
//
// Returns *ast.FunctionResolution which describes the chosen signature, or nil when no
// polymorphic rule applies.
// Returns error when resolution fails.
func (*functionResolver) ResolveFunctionCall(
	_ *sqlmodel.Catalogue,
	name string,
	_ string,
	argumentTypes []sqlmodel.SQLType,
) (*ast.FunctionResolution, error) {
	switch strings.ToLower(name) {
	case "if":
		return resolveIf(argumentTypes)
	case "ifnull", "nvl":
		return resolveIfNull(argumentTypes)
	case "nvl2":
		return resolveNvl2(argumentTypes)
	case "decode_oracle":
		return resolveDecodeOracle(argumentTypes)
	case "abs":
		return resolveAbs(argumentTypes)
	case "round", "truncate":
		return resolveRound(argumentTypes)
	case "coalesce":
		return resolveCoalesce(argumentTypes)
	case "greatest", "least":
		return resolveGreatestLeast(argumentTypes)
	case "group_concat":
		return resolveGroupConcat()
	case "json_extract":
		return resolveJSONExtract()
	case "json_unquote", "concat", "concat_ws":
		return resolveTextReturn()
	case "floor", "ceil", "ceiling":
		return resolveFloorCeil(argumentTypes)
	case "mod":
		return resolveMod(argumentTypes)
	case "sum":
		return resolveSum(argumentTypes)
	case "avg":
		return resolveAvg()
	case "min", "max", "bit_and", "bit_or", "bit_xor":
		return resolveIdentityAggregate(argumentTypes)
	case "count":
		return resolveCount()
	case "nullif", "first_value", "last_value", "lag", "lead":
		return resolveIdentityFunction(argumentTypes)
	default:
		return resolveTemporalFunction(strings.ToLower(name), argumentTypes)
	}
}

// ShiftedDateArgument reports the position of the date and time value a date arithmetic
// function shifts, such as 0 for DATE_ADD and ADDTIME and 2 for TIMESTAMPADD, whose
// result follows that value's type. A flavour such as MariaDB reads it to type the same
// calls its own way.
//
// Takes name (string) which is the lower-cased function name.
//
// Returns position (int) which is the zero-based argument the function shifts.
// Returns shifts (bool) which is false for a function that shifts no date and time.
func ShiftedDateArgument(name string) (position int, shifts bool) {
	position, shifts = shiftedDateArguments[name]
	return position, shifts
}

// resolveTemporalFunction types date and time calls whose result depends on their
// arguments.
//
// Takes name (string) which is the lower-cased function name.
// Takes argumentTypes ([]sqlmodel.SQLType) which carries the actual argument types at the
// call site.
//
// Returns *ast.FunctionResolution which describes the result, or nil for a function it
// does not type.
// Returns error which is always nil.
func resolveTemporalFunction(name string, argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if shifted, shifts := ShiftedDateArgument(name); shifts {
		return resolveDateArithmetic(argumentTypes, shifted)
	}
	switch name {
	case "unix_timestamp":
		return resolveUnixTimestamp(argumentTypes)
	case "extract":
		return resolveExtract(argumentTypes)
	default:
		return nil, nil
	}
}

// resolveExtract types EXTRACT(unit FROM value) as a BIGINT, which holds every unit. It
// is NULL when the value is, and may be NULL whatever its value when that is a string or
// a number, which MySQL reads as a date and time and which may hold none.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which carries the value's type; the parser
// leaves the unit out of the arguments.
//
// Returns *ast.FunctionResolution which describes the BIGINT result, or nil when the call
// has no value.
// Returns error which is always nil.
func resolveExtract(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < minArgumentsSingleArgumentFunction {
		return nil, nil
	}
	behaviour := sqlmodel.FunctionNullableReturnsNullOnNull
	if category := argumentTypes[0].Category; category != sqlmodel.TypeCategoryTemporal && category != sqlmodel.TypeCategoryUnknown {
		behaviour = sqlmodel.FunctionNullableCalledOnNull
	}
	return &ast.FunctionResolution{
		ReturnType:        sqlmodel.NewIntegerType(bigintTypeName, widthBigint, false),
		NullableBehaviour: behaviour,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveIf computes the return type for the IF conditional function.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which carries the actual argument types at the
// call site.
//
// Returns *ast.FunctionResolution which describes the promoted branch type, or nil when
// too few arguments are present.
// Returns error when resolution fails.
func resolveIf(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < minArgumentsIf {
		return nil, nil
	}

	returnType := promoteTypes(argumentTypes[1], argumentTypes[2])

	return &ast.FunctionResolution{
		ReturnType:        returnType,
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveIfNull computes the return type for IFNULL, which is NULL only when both of its
// arguments are, as MySQL and MariaDB report for IFNULL(nullable, 0).
//
// Takes argumentTypes ([]sqlmodel.SQLType) which carries the actual argument types at the
// call site, each marked nullable when the argument may be NULL.
//
// Returns *ast.FunctionResolution which describes the promoted type, or nil when too few
// arguments are present.
// Returns error when resolution fails.
func resolveIfNull(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < minArgumentsIfNull {
		return nil, nil
	}

	returnType := promoteTypes(argumentTypes[0], argumentTypes[1])
	returnType.Nullable = argumentTypes[0].Nullable && argumentTypes[1].Nullable

	return &ast.FunctionResolution{
		ReturnType:        returnType,
		NullableBehaviour: nullableUnless(returnType.Nullable),
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveNvl2 computes the return type for MariaDB's NVL2(test, if_not_null, if_null):
// the promoted type of the two results. MariaDB reports it as NULL when any argument may
// be, its test included.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which carries the actual argument types at the
// call site, each marked nullable when the argument may be NULL.
//
// Returns *ast.FunctionResolution which describes the promoted type, or nil when fewer
// than three arguments are present.
// Returns error when resolution fails.
func resolveNvl2(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < minArgumentsIf {
		return nil, nil
	}

	returnType := promoteTypes(argumentTypes[1], argumentTypes[2])
	returnType.Nullable = argumentTypes[0].Nullable || argumentTypes[1].Nullable || argumentTypes[2].Nullable

	return &ast.FunctionResolution{
		ReturnType:        returnType,
		NullableBehaviour: nullableUnless(returnType.Nullable),
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveDecodeOracle computes the return type for MariaDB's DECODE_ORACLE(value, search,
// result [, search, result ...] [, default]): the promoted type of its results and
// default. Without a default it is NULL when no search matches; with one it is NULL only
// when a result or the default may be.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which carries the actual argument types at the
// call site, each marked nullable when the argument may be NULL.
//
// Returns *ast.FunctionResolution which describes the promoted type, or nil when fewer
// than three arguments are present.
// Returns error when resolution fails.
func resolveDecodeOracle(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < minArgumentsIf {
		return nil, nil
	}

	results := make([]sqlmodel.SQLType, 0, len(argumentTypes)/2)
	for index := 2; index < len(argumentTypes); index += 2 {
		results = append(results, argumentTypes[index])
	}
	hasDefault := len(argumentTypes)%2 == 0
	if hasDefault {
		results = append(results, argumentTypes[len(argumentTypes)-1])
	}
	returnType := promoteArgumentTypes(results)
	returnType.Nullable = !hasDefault
	for index := range results {
		returnType.Nullable = returnType.Nullable || results[index].Nullable
	}

	return &ast.FunctionResolution{
		ReturnType:        returnType,
		NullableBehaviour: nullableUnless(returnType.Nullable),
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// nullableUnless returns the NULL behaviour of a result whose nullability its arguments
// decide: never NULL when they make it so, and otherwise left to the call.
//
// Takes nullable (bool) which is true when the result may be NULL.
//
// Returns sqlmodel.FunctionNullableBehaviour which is FunctionNullableNeverNull for a
// result that is never NULL and FunctionNullableCalledOnNull otherwise.
func nullableUnless(nullable bool) sqlmodel.FunctionNullableBehaviour {
	if nullable {
		return sqlmodel.FunctionNullableCalledOnNull
	}
	return sqlmodel.FunctionNullableNeverNull
}

// promoteArgumentTypes promotes all non-unknown argument types to a single common type.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which carries the actual argument types at the
// call site.
//
// Returns sqlmodel.SQLType which is the promoted type, or an unknown type when every
// argument is unknown.
func promoteArgumentTypes(argumentTypes []sqlmodel.SQLType) sqlmodel.SQLType {
	var result sqlmodel.SQLType
	initialised := false

	for index := range argumentTypes {
		if argumentTypes[index].Category == sqlmodel.TypeCategoryUnknown {
			continue
		}

		if !initialised {
			result = argumentTypes[index]
			initialised = true

			continue
		}

		result = promoteTypes(result, argumentTypes[index])
	}

	if !initialised {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "")
	}

	return result
}

// resolveCoalesce computes the return type for COALESCE.
//
// COALESCE returns its first non-NULL argument, so it is called even when arguments are
// NULL.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which carries the actual argument types at the
// call site.
//
// Returns *ast.FunctionResolution which describes the promoted type across all
// non-unknown arguments.
// Returns error when resolution fails.
func resolveCoalesce(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	return &ast.FunctionResolution{
		ReturnType:        promoteArgumentTypes(argumentTypes),
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveGreatestLeast computes the return type for GREATEST/LEAST.
//
// Unlike COALESCE, GREATEST and LEAST return NULL whenever any argument is NULL, so the
// resolution is modelled as returns-null-on-null.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which carries the actual argument types at the
// call site.
//
// Returns *ast.FunctionResolution which describes the promoted type across all
// non-unknown arguments.
// Returns error when resolution fails.
func resolveGreatestLeast(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	return &ast.FunctionResolution{
		ReturnType:        promoteArgumentTypes(argumentTypes),
		NullableBehaviour: sqlmodel.FunctionNullableReturnsNullOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveGroupConcat computes the return type for GROUP_CONCAT.
//
// Returns *ast.FunctionResolution which describes the text-typed aggregate result.
// Returns error when resolution fails.
func resolveGroupConcat() (*ast.FunctionResolution, error) {
	return &ast.FunctionResolution{
		ReturnType:        sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"),
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       true,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveJSONExtract computes the return type for JSON_EXTRACT, which is NULL whenever no
// path matches the document, so it may be NULL even over a non-null document.
//
// Returns *ast.FunctionResolution which describes the JSON-typed result.
// Returns error when resolution fails.
func resolveJSONExtract() (*ast.FunctionResolution, error) {
	return &ast.FunctionResolution{
		ReturnType:        sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "json"),
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveTextReturn yields a text-typed resolution used by string builders.
//
// Returns *ast.FunctionResolution which describes the text-typed result.
// Returns error when resolution fails.
func resolveTextReturn() (*ast.FunctionResolution, error) {
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

// resolveFloorCeil determines the result type of MySQL's rounding functions.
//
// Floating-point and integer arguments retain their types, while DECIMAL produces BIGINT.
// Other arguments produce DOUBLE.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which carries the actual argument types at the
// call site.
//
// Returns *ast.FunctionResolution which describes the result, or nil when no argument is
// present.
// Returns error when resolution fails.
func resolveFloorCeil(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < minArgumentsSingleArgumentFunction {
		return nil, nil
	}

	returnType := builtinTypeMap[doubleTypeName]
	if category := argumentTypes[0].Category; category == sqlmodel.TypeCategoryFloat || category == sqlmodel.TypeCategoryInteger {
		returnType = argumentTypes[0]
	} else if category == sqlmodel.TypeCategoryDecimal {
		returnType = builtinTypeMap[bigintTypeName]
	}

	return numericResolution(returnType, sqlmodel.FunctionNullableReturnsNullOnNull), nil
}

// resolveAbs computes the return type for ABS, which MySQL types as its argument's
// numeric type: an INT stays an INT and a BIGINT a BIGINT, so a BIGINT is never read into
// a 32-bit integer; any other argument, read as a number, gives a DOUBLE.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which carries the actual argument types at the
// call site.
//
// Returns *ast.FunctionResolution which describes the result, or nil when no argument is
// present.
// Returns error when resolution fails.
func resolveAbs(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < minArgumentsSingleArgumentFunction {
		return nil, nil
	}

	returnType := builtinTypeMap[doubleTypeName]
	if isNumericCategory(argumentTypes[0].Category) {
		returnType = argumentTypes[0]
	}

	return numericResolution(returnType, sqlmodel.FunctionNullableReturnsNullOnNull), nil
}

// resolveRound computes the return type for ROUND and TRUNCATE: a DECIMAL argument keeps
// its DECIMAL type, as MySQL rounds it exactly, an integer gives a BIGINT, and any other
// argument gives a DOUBLE.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which carries the actual argument types at the
// call site.
//
// Returns *ast.FunctionResolution which describes the result, or nil when no argument is
// present.
// Returns error when resolution fails.
func resolveRound(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < minArgumentsSingleArgumentFunction {
		return nil, nil
	}

	returnType := builtinTypeMap[doubleTypeName]
	switch argumentTypes[0].Category {
	case sqlmodel.TypeCategoryDecimal:
		returnType = argumentTypes[0]
	case sqlmodel.TypeCategoryInteger:
		returnType = builtinTypeMap[bigintTypeName]
	default:
	}

	return numericResolution(returnType, sqlmodel.FunctionNullableReturnsNullOnNull), nil
}

// isNumericCategory reports whether a category holds numbers: integers, decimals and
// floating-point values.
//
// Takes category (sqlmodel.SQLTypeCategory) which is the category.
//
// Returns bool which is true for a numeric category.
func isNumericCategory(category sqlmodel.SQLTypeCategory) bool {
	return category == sqlmodel.TypeCategoryInteger || category == sqlmodel.TypeCategoryDecimal ||
		category == sqlmodel.TypeCategoryFloat
}

// numericResolution builds the resolution of a read-only scalar function.
//
// Takes returnType (sqlmodel.SQLType) which is the result's type; its nullability is
// cleared, as the behaviour decides it.
// Takes behaviour (sqlmodel.FunctionNullableBehaviour) which says when the result is
// NULL.
//
// Returns *ast.FunctionResolution which is the resolution.
func numericResolution(returnType sqlmodel.SQLType, behaviour sqlmodel.FunctionNullableBehaviour) *ast.FunctionResolution {
	returnType.Nullable = false
	return &ast.FunctionResolution{
		ReturnType:        returnType,
		NullableBehaviour: behaviour,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}
}

// resolveMod determines the type and nullability of MySQL's MOD result.
//
// Numeric operands retain an appropriate numeric type. The result is nullable because
// division by zero yields NULL.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which carries the actual argument types at the
// call site.
//
// Returns *ast.FunctionResolution which describes the result, or nil when fewer than two
// arguments are present.
// Returns error when resolution fails.
func resolveMod(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < minArgumentsIfNull {
		return nil, nil
	}

	left, right := argumentTypes[0], argumentTypes[1]
	var returnType sqlmodel.SQLType
	switch {
	case left.Category == sqlmodel.TypeCategoryFloat || right.Category == sqlmodel.TypeCategoryFloat:
		returnType = widerFloat(left, right)
	case left.Category == sqlmodel.TypeCategoryDecimal:
		returnType = left
	case right.Category == sqlmodel.TypeCategoryDecimal:
		returnType = right
	case left.Category == sqlmodel.TypeCategoryInteger && right.Category == sqlmodel.TypeCategoryInteger:
		returnType = widerInteger(left, right)
	default:
		returnType = builtinTypeMap[doubleTypeName]
	}

	return numericResolution(returnType, sqlmodel.FunctionNullableCalledOnNull), nil
}

// widerInteger returns the wider of two integer types, the left on a tie.
//
// Takes left (sqlmodel.SQLType) which is the left operand type.
// Takes right (sqlmodel.SQLType) which is the right operand type.
//
// Returns sqlmodel.SQLType which is the wider type.
func widerInteger(left sqlmodel.SQLType, right sqlmodel.SQLType) sqlmodel.SQLType {
	if right.Width > left.Width {
		return right
	}
	return left
}

// widerFloat returns the wider float result for a MOD whose operands include at least one
// float. Any non-float operand contributes the DOUBLE width MySQL uses when mixing a
// float with an integer, so the result is the wider of the two float widths (DOUBLE
// unless both floats are FLOAT).
//
// Takes left (sqlmodel.SQLType) which is the left operand type.
// Takes right (sqlmodel.SQLType) which is the right operand type.
//
// Returns sqlmodel.SQLType which is the float type to use for the result.
func widerFloat(left sqlmodel.SQLType, right sqlmodel.SQLType) sqlmodel.SQLType {
	bothFloat := left.Category == sqlmodel.TypeCategoryFloat &&
		right.Category == sqlmodel.TypeCategoryFloat
	if bothFloat && left.EngineName == "float" && right.EngineName == "float" {
		return sqlmodel.NewFloatType("float", widthFloat)
	}
	return sqlmodel.NewFloatType("double", widthDouble)
}

// resolveSum computes the return type for the SUM aggregate.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which carries the actual argument types at the
// call site.
//
// Returns *ast.FunctionResolution which describes the aggregate result type, or nil when
// no argument is present.
// Returns error when resolution fails.
func resolveSum(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < minArgumentsSingleArgumentFunction {
		return nil, nil
	}

	argumentType := argumentTypes[0]
	var returnType sqlmodel.SQLType

	switch argumentType.Category {
	case sqlmodel.TypeCategoryFloat:
		returnType = sqlmodel.NewFloatType("double", widthDouble)
	default:
		returnType = sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "decimal")
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

// resolveAvg computes the return type for the AVG aggregate.
//
// Returns *ast.FunctionResolution which describes the double-typed aggregate result.
// Returns error when resolution fails.
func resolveAvg() (*ast.FunctionResolution, error) {
	return &ast.FunctionResolution{
		ReturnType:        sqlmodel.NewFloatType("double", widthDouble),
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       true,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveIdentityAggregate returns the argument type for MIN/MAX.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which carries the actual argument types at the
// call site.
//
// Returns *ast.FunctionResolution which echoes the first argument type as the aggregate
// result, or nil when no argument is present.
// Returns error when resolution fails.
func resolveIdentityAggregate(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < minArgumentsSingleArgumentFunction {
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

// resolveIdentityFunction returns the first argument type for non-aggregate identity
// functions such as NULLIF and the value window functions FIRST_VALUE, LAST_VALUE, LAG,
// and LEAD.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which carries the actual argument types at the
// call site.
//
// Returns *ast.FunctionResolution which echoes the first argument type as the result, or
// nil when no argument is present.
// Returns error when resolution fails.
func resolveIdentityFunction(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < minArgumentsSingleArgumentFunction {
		return nil, nil
	}

	return &ast.FunctionResolution{
		ReturnType:        argumentTypes[0],
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveDateArithmetic determines the result type of date and time arithmetic.
//
// It follows the input's temporal type. String or numeric inputs produce strings, which
// may be NULL when they do not represent a valid date or time.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which carries the actual argument types at the
// call site.
// Takes shifted (int) which is the position of the value the call shifts.
//
// Returns *ast.FunctionResolution which describes the shifted value's type, or nil when
// the call has no value at that position.
// Returns error which is always nil.
func resolveDateArithmetic(argumentTypes []sqlmodel.SQLType, shifted int) (*ast.FunctionResolution, error) {
	if len(argumentTypes) <= shifted {
		return nil, nil
	}
	value := argumentTypes[shifted]
	returnType := sqlmodel.NewTemporalType("datetime", sqlmodel.TemporalKindTimestamp, false)
	behaviour := sqlmodel.FunctionNullableReturnsNullOnNull
	switch {
	case value.Category == sqlmodel.TypeCategoryTemporal && value.Temporal == sqlmodel.TemporalKindTime:
		returnType = value
		returnType.Nullable = false
	case value.Category != sqlmodel.TypeCategoryTemporal && value.Category != sqlmodel.TypeCategoryUnknown:
		returnType = sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar")
		behaviour = sqlmodel.FunctionNullableCalledOnNull
	}
	return &ast.FunctionResolution{
		ReturnType:        returnType,
		NullableBehaviour: behaviour,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveUnixTimestamp types UNIX_TIMESTAMP from the argument's fractional-second
// precision.
//
// Whole seconds yield BIGINT; possible fractional seconds yield DECIMAL. The result may
// be NULL for out-of-range dates.
//
// Takes argumentTypes ([]sqlmodel.SQLType) which carries the actual argument types at the
// call site.
//
// Returns *ast.FunctionResolution which describes the result type, or nil when the call
// has no argument.
// Returns error which is always nil.
func resolveUnixTimestamp(argumentTypes []sqlmodel.SQLType) (*ast.FunctionResolution, error) {
	if len(argumentTypes) < minArgumentsSingleArgumentFunction {
		return nil, nil
	}
	value := argumentTypes[0]
	returnType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "decimal")
	if value.Category == sqlmodel.TypeCategoryTemporal && (value.Precision == nil || *value.Precision == 0) {
		returnType = sqlmodel.NewIntegerType(bigintTypeName, widthBigint, false)
	}
	return &ast.FunctionResolution{
		ReturnType:        returnType,
		NullableBehaviour: sqlmodel.FunctionNullableCalledOnNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       false,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// resolveCount returns the BIGINT non-null resolution used by COUNT.
//
// Returns *ast.FunctionResolution which describes the bigint aggregate result.
// Returns error when resolution fails.
func resolveCount() (*ast.FunctionResolution, error) {
	return &ast.FunctionResolution{
		ReturnType:        sqlmodel.NewIntegerType("bigint", widthBigint, false),
		NullableBehaviour: sqlmodel.FunctionNullableNeverNull,
		NullingArgument:   0,
		DataAccess:        sqlmodel.DataAccessReadOnly,
		IsAggregate:       true,
		ReturnsSet:        false,
		ElementsKnown:     false,
	}, nil
}

// promoteTypes returns the wider of two SQL types under MySQL rules.
//
// When both types share the same category the wider integer or float is returned, and the
// first of any other pair. Otherwise the category hierarchy (text > float > decimal >
// integer) determines the result.
//
// Takes left (sqlmodel.SQLType) which is the left operand type.
// Takes right (sqlmodel.SQLType) which is the right operand type.
//
// Returns sqlmodel.SQLType which is the promoted SQL type.
func promoteTypes(left sqlmodel.SQLType, right sqlmodel.SQLType) sqlmodel.SQLType {
	if left.Category == sqlmodel.TypeCategoryUnknown {
		return right
	}

	if right.Category == sqlmodel.TypeCategoryUnknown {
		return left
	}

	if left.Category == right.Category {
		switch {
		case left.Category == sqlmodel.TypeCategoryInteger && promotionRank(right) > promotionRank(left):
			return right
		case left.Category == sqlmodel.TypeCategoryFloat && right.Width > left.Width:
			return right
		default:
			return left
		}
	}

	leftRank := typePromotionRank(left.Category)
	rightRank := typePromotionRank(right.Category)

	if leftRank >= rightRank {
		return left
	}

	return right
}

// typePromotionRank returns the comparison rank for a type category.
//
// Takes category (sqlmodel.SQLTypeCategory) which is the category to rank.
//
// Returns int which is the rank used for promotion comparisons.
func typePromotionRank(category sqlmodel.SQLTypeCategory) int {
	switch category {
	case sqlmodel.TypeCategoryText:
		return promotionRankText
	case sqlmodel.TypeCategoryDecimal:
		return promotionRankDecimal
	case sqlmodel.TypeCategoryFloat:
		return promotionRankFloat
	case sqlmodel.TypeCategoryInteger:
		return promotionRankInteger
	default:
		return promotionRankDefault
	}
}

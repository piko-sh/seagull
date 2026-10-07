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
	"maps"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// operatorConcatenate joins text, arrays or jsonb documents.
	operatorConcatenate = "||"

	// operatorShiftLeft shifts an integer or a bit string left, and tests whether one
	// network or range is strictly contained by another.
	operatorShiftLeft = "<<"

	// operatorShiftRight shifts an integer or a bit string right, and tests whether one
	// network or range strictly contains another.
	operatorShiftRight = ">>"

	// accessorArrow is the operator that applies an accessor to a value under
	// WithAccessorArrows.
	accessorArrow = "->"

	// operatorDistance is the distance operator: between geometric shapes, vectors and
	// geometries a double precision distance, between texts pg_trgm's trigram distance and
	// between text search queries a query matching one followed by the other.
	operatorDistance = "<->"
)

var (
	// operatorFeatures names the expression feature each PostgreSQL operator that needs one
	// requires. IS [NOT] DISTINCT FROM compares NULL as a value, so it is a NULL test.
	operatorFeatures = map[string]ast.ExpressionFeature{
		"IS DISTINCT FROM":     ast.SQLFeatureIsNull,
		"IS NOT DISTINCT FROM": ast.SQLFeatureIsNull,
		operatorConcatenate:    ast.SQLFeatureStringConcat,
		"->":                   ast.SQLFeatureJSONOp,
		"->>":                  ast.SQLFeatureJSONOp,
		"#>":                   ast.SQLFeatureJSONOp,
		"#>>":                  ast.SQLFeatureJSONOp,
		"&":                    ast.SQLFeatureBitwiseOp,
		"|":                    ast.SQLFeatureBitwiseOp,
		"#":                    ast.SQLFeatureBitwiseOp,
		operatorShiftLeft:      ast.SQLFeatureBitwiseOp,
		operatorShiftRight:     ast.SQLFeatureBitwiseOp,
		"LIKE":                 ast.SQLFeaturePatternMatch,
		"ILIKE":                ast.SQLFeaturePatternMatch,
		"SIMILAR TO":           ast.SQLFeaturePatternMatch,
		"~":                    ast.SQLFeaturePatternMatch,
		"~*":                   ast.SQLFeaturePatternMatch,
		"!~":                   ast.SQLFeaturePatternMatch,
		"!~*":                  ast.SQLFeaturePatternMatch,
	}

	// jsonDocumentOperators extract a JSON document from a document by key, index or path.
	jsonDocumentOperators = []string{"->", "#>"}

	// jsonTextOperators extract the text of a JSON value by key, index or path.
	jsonTextOperators = []string{"->>", "#>>"}

	// booleanOperators are the generic operators that test something and yield a boolean:
	// jsonb key existence, range adjacency and positioning, and the network and geometric
	// relations.
	booleanOperators = []string{"?", "?|", "?&", "@?", "-|-", "&<", "&>", "<<|", "|>>", "&<|", "|&>", "~="}

	// trigramDistanceOperators give the distance between two texts that pg_trgm's similarity
	// functions measure, a real number.
	trigramDistanceOperators = []string{"<<->", "<->>", "<<<->", "<->>>"}

	// distanceOperators give a distance in double precision: between geometric shapes,
	// pgvector's vectors and PostGIS geometries.
	distanceOperators = []string{"<#>", "<=>", "<+>", "<~>", "<%>", "<<->>", "<<#>>", "|=|"}

	// oneTypeOperators lists generic distance and bitwise operators whose operands share a
	// type, letting a parameter inherit the other operand's type. Shifts are excluded
	// because their operand types can differ.
	oneTypeOperators = slices.Concat([]string{operatorDistance, "&", "|", "#"}, distanceOperators, trigramDistanceOperators)
)

// coreTypes returns PostgreSQL's core types, each through the dialect's normaliser:
// boolean, text, bigint for counts (what COUNT returns), json, integer for a LIMIT or
// OFFSET parameter, no implicit row identifier, and void, which a function such as
// pg_advisory_lock returns.
//
// PostgreSQL itself infers bigint for a LIMIT or OFFSET parameter; integer keeps the
// generated signatures that callers already use and is accepted by the server.
//
// Returns dialect.CoreTypes which holds the core types.
func (engine *Dialect) coreTypes() dialect.CoreTypes {
	return dialect.CoreTypes{
		Boolean:  engine.NormaliseTypeName("boolean"),
		Text:     engine.NormaliseTypeName("text"),
		Count:    engine.NormaliseTypeName("bigint"),
		JSON:     engine.NormaliseTypeName("json"),
		RowCount: engine.NormaliseTypeName("integer"),
		RowID:    sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""),
		Void:     engine.NormaliseTypeName("void"),
	}
}

// binaryResult types PostgreSQL's operators whose result is not the common type of their
// operands: temporal arithmetic, a division of integers where the flavour makes it exact,
// arithmetic between a float and another kind of number, concatenation, JSON extraction,
// the operators that test and yield a boolean, and the shifts.
//
// Takes operator (string) which is the operator.
// Takes left (sqlmodel.SQLType) which is the left operand's type.
// Takes right (sqlmodel.SQLType) which is the right operand's type.
//
// Returns sqlmodel.SQLType which is the result's type.
// Returns sqlmodel.FunctionNullableBehaviour which says when the result is NULL.
// Returns bool which is false for an operator left to the structural default.
func (engine *Dialect) binaryResult(operator string, left, right sqlmodel.SQLType) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
	if result, ok := engine.temporalArithmeticResult(operator, left, right); ok {
		return result, sqlmodel.FunctionNullableReturnsNullOnNull, true
	}
	if result, ok := engine.integerDivisionResult(operator, left, right); ok {
		return result, sqlmodel.FunctionNullableReturnsNullOnNull, true
	}
	if result, ok := engine.mixedFloatArithmeticResult(operator, left, right); ok {
		return result, sqlmodel.FunctionNullableReturnsNullOnNull, true
	}
	if result, applies := engine.accessorArrowResult(operator, left, right); applies {
		return result, sqlmodel.FunctionNullableReturnsNullOnNull, true
	}
	if result, applies := engine.distanceResult(operator, left); applies {
		return result, sqlmodel.FunctionNullableReturnsNullOnNull, true
	}
	switch {
	case operator == operatorConcatenate:
		return engine.concatenationResult(left, right), sqlmodel.FunctionNullableReturnsNullOnNull, true
	case slices.Contains(jsonDocumentOperators, operator):
		if left.Category == sqlmodel.TypeCategoryJSON {
			return left, sqlmodel.FunctionNullableCalledOnNull, true
		}
		return engine.NormaliseTypeName("json"), sqlmodel.FunctionNullableCalledOnNull, true
	case slices.Contains(jsonTextOperators, operator):
		return engine.NormaliseTypeName("text"), sqlmodel.FunctionNullableCalledOnNull, true
	case slices.Contains(booleanOperators, operator), isRelationalShift(operator, left):
		return engine.NormaliseTypeName("boolean"), sqlmodel.FunctionNullableReturnsNullOnNull, true
	case operator == operatorShiftLeft, operator == operatorShiftRight:
		return left, sqlmodel.FunctionNullableReturnsNullOnNull, true
	default:
		return sqlmodel.SQLType{}, sqlmodel.FunctionNullableReturnsNullOnNull, false
	}
}

// mixedFloatArithmeticResult types +, -, * and / between a float and a number of another
// category as double precision, even when the float is a real.
//
// Takes operator (string) which is the operator.
// Takes left (sqlmodel.SQLType) which is the left operand's type.
// Takes right (sqlmodel.SQLType) which is the right operand's type.
//
// Returns sqlmodel.SQLType which is double precision.
// Returns bool which is false unless a float meets a number of another category.
func (engine *Dialect) mixedFloatArithmeticResult(operator string, left, right sqlmodel.SQLType) (sqlmodel.SQLType, bool) {
	switch operator {
	case operatorAdd, operatorSubtract, operatorMultiply, operatorDivide:
	default:
		return sqlmodel.SQLType{}, false
	}
	if left.Category == right.Category || !dialectkit.IsNumericCategory(left) || !dialectkit.IsNumericCategory(right) {
		return sqlmodel.SQLType{}, false
	}
	if left.Category != sqlmodel.TypeCategoryFloat && right.Category != sqlmodel.TypeCategoryFloat {
		return sqlmodel.SQLType{}, false
	}
	return engine.NormaliseTypeName("double precision"), true
}

// distanceResult types the distance operators: <-> between text search queries is the
// query matching the first followed by the second, between texts pg_trgm's trigram
// distance, a real number, as the other trigram distances are, and every other distance a
// double precision number.
//
// Takes operator (string) which is the operator.
// Takes left (sqlmodel.SQLType) which is the left operand's type.
//
// Returns sqlmodel.SQLType which is the result's type.
// Returns bool which is false when the operator is no distance operator.
func (engine *Dialect) distanceResult(operator string, left sqlmodel.SQLType) (sqlmodel.SQLType, bool) {
	switch {
	case operator == operatorDistance && left.EngineName == "tsquery":
		return left, true
	case operator == operatorDistance && left.Category == sqlmodel.TypeCategoryText,
		slices.Contains(trigramDistanceOperators, operator):
		return engine.NormaliseTypeName("real"), true
	case operator == operatorDistance, slices.Contains(distanceOperators, operator):
		return engine.NormaliseTypeName("double precision"), true
	default:
		return sqlmodel.SQLType{}, false
	}
}

// integerDivisionResult types / between two integers as numeric when the dialect was
// built WithDecimalIntegerDivision, as CockroachDB divides them; PostgreSQL truncates the
// quotient to an integer, which the structural default already gives.
//
// Takes operator (string) which is the operator.
// Takes left (sqlmodel.SQLType) which is the dividend's type.
// Takes right (sqlmodel.SQLType) which is the divisor's type.
//
// Returns sqlmodel.SQLType which is the quotient's type.
// Returns bool which is false when the rule does not apply.
func (engine *Dialect) integerDivisionResult(operator string, left, right sqlmodel.SQLType) (sqlmodel.SQLType, bool) {
	if !engine.settings.DecimalIntegerDivision || operator != operatorDivide ||
		left.Category != sqlmodel.TypeCategoryInteger || right.Category != sqlmodel.TypeCategoryInteger {
		return sqlmodel.SQLType{}, false
	}
	return engine.NormaliseTypeName("numeric"), true
}

// concatenationResult types ||: an array when either side is one, a jsonb document when
// both are JSON, and text otherwise.
//
// Takes left (sqlmodel.SQLType) which is the left operand's type.
// Takes right (sqlmodel.SQLType) which is the right operand's type.
//
// Returns sqlmodel.SQLType which is the result's type.
func (engine *Dialect) concatenationResult(left, right sqlmodel.SQLType) sqlmodel.SQLType {
	switch {
	case left.Category == sqlmodel.TypeCategoryArray:
		return left
	case right.Category == sqlmodel.TypeCategoryArray:
		return right
	case left.Category == sqlmodel.TypeCategoryJSON && right.Category == sqlmodel.TypeCategoryJSON:
		return left
	default:
		return engine.NormaliseTypeName("text")
	}
}

// unaryResult types NOT as a boolean that is NULL when its operand is and leaves every
// other prefix operator to the structural default.
//
// Takes operator (string) which is the operator.
//
// Returns sqlmodel.SQLType which is the result's type.
// Returns sqlmodel.FunctionNullableBehaviour which says when the result is NULL.
// Returns bool which is true only for NOT.
func (engine *Dialect) unaryResult(operator string) (sqlmodel.SQLType, sqlmodel.FunctionNullableBehaviour, bool) {
	if strings.EqualFold(operator, keywordNOT) {
		return engine.NormaliseTypeName("boolean"), sqlmodel.FunctionNullableReturnsNullOnNull, true
	}
	return sqlmodel.SQLType{}, sqlmodel.FunctionNullableReturnsNullOnNull, false
}

// accessorArrowResult types -> between a value and an accessor, as WithAccessorArrows
// describes: the return type of the overload of the accessor's function that takes the
// value's type, other than an accessor form, which may take a state of the same type as
// an argument of its own.
//
// Takes operator (string) which is the operator.
// Takes left (sqlmodel.SQLType) which is the value's type.
// Takes right (sqlmodel.SQLType) which is the accessor's type.
//
// Returns sqlmodel.SQLType which is the result's type, unknown when no overload, or
// overloads of different types, take the value.
// Returns bool which is false unless the operator is -> and its right operand is of an
// accessor type WithAccessorArrows named, while the left is no JSON document.
func (engine *Dialect) accessorArrowResult(operator string, left, right sqlmodel.SQLType) (sqlmodel.SQLType, bool) {
	if operator != accessorArrow || engine.accessors == nil || left.Category == sqlmodel.TypeCategoryJSON {
		return sqlmodel.SQLType{}, false
	}
	names, isAccessor := engine.accessors[strings.ToLower(right.EngineName)]
	if !isAccessor || engine.isAccessor(left) {
		return sqlmodel.SQLType{}, false
	}
	var result *sqlmodel.SQLType
	for _, signature := range engine.accessorTargets(names, left) {
		if result != nil && !sameType(*result, signature.ReturnType) {
			return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true
		}
		result = &signature.ReturnType
	}
	if result == nil {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""), true
	}
	return *result, true
}

// accessorTargets returns the overloads of an accessor's functions that take a value of
// the type an accessor arrow applies them to, other than the accessor forms.
//
// Takes names ([]string) which name the functions that return the accessor.
// Takes value (sqlmodel.SQLType) which is the type of the value on the arrow's left.
//
// Returns []*sqlmodel.Function which holds the overloads, in order.
func (engine *Dialect) accessorTargets(names []string, value sqlmodel.SQLType) []*sqlmodel.Function {
	var targets []*sqlmodel.Function
	for _, name := range names {
		for _, signature := range engine.functions.Functions[name] {
			if signature != nil && takesType(signature, value) && !engine.isAccessor(signature.ReturnType) {
				targets = append(targets, signature)
			}
		}
	}
	return targets
}

// isAccessor reports whether a type is one of the accessor types WithAccessorArrows
// named.
//
// Takes sqlType (sqlmodel.SQLType) which is the type.
//
// Returns bool which is true for an accessor type.
func (engine *Dialect) isAccessor(sqlType sqlmodel.SQLType) bool {
	_, accessor := engine.accessors[strings.ToLower(sqlType.EngineName)]
	return accessor
}

// isRelationalShift reports whether << or >> tests containment rather than shifting:
// between networks, ranges or geometric shapes it does.
//
// Takes operator (string) which is the operator.
// Takes left (sqlmodel.SQLType) which is the left operand's type.
//
// Returns bool which is true for a containment test.
func isRelationalShift(operator string, left sqlmodel.SQLType) bool {
	if operator != operatorShiftLeft && operator != operatorShiftRight {
		return false
	}
	switch left.Category {
	case sqlmodel.TypeCategoryNetwork, sqlmodel.TypeCategoryRange, sqlmodel.TypeCategoryGeometric:
		return true
	default:
		return false
	}
}

// takesType reports whether a signature declares an argument of a named type.
//
// Takes signature (*sqlmodel.Function) which is the signature.
// Takes argumentType (sqlmodel.SQLType) which is the type.
//
// Returns bool which is true when an argument is declared with the type's name.
func takesType(signature *sqlmodel.Function, argumentType sqlmodel.SQLType) bool {
	if argumentType.EngineName == "" {
		return false
	}
	return slices.ContainsFunc(signature.Arguments, func(argument sqlmodel.FunctionArgument) bool {
		return strings.EqualFold(argument.Type.EngineName, argumentType.EngineName)
	})
}

// accessorFunctions maps each accessor type, by lower-cased name, to the names of the
// functions that return it, in name order.
//
// Takes functions (*ast.FunctionCatalogue) which holds the functions.
// Takes accessorTypes ([]string) which name the accessor types.
//
// Returns map[string][]string which maps the accessor types to their functions.
func accessorFunctions(functions *ast.FunctionCatalogue, accessorTypes []string) map[string][]string {
	accessors := make(map[string][]string, len(accessorTypes))
	for _, typeName := range accessorTypes {
		accessors[strings.ToLower(typeName)] = nil
	}
	for _, name := range slices.Sorted(maps.Keys(functions.Functions)) {
		for _, signature := range functions.Functions[name] {
			if signature == nil {
				continue
			}
			typeName := strings.ToLower(signature.ReturnType.EngineName)
			if names, accessor := accessors[typeName]; accessor && !slices.Contains(names, name) {
				accessors[typeName] = append(names, name)
			}
		}
	}
	return accessors
}

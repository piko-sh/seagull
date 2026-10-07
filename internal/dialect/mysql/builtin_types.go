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
	"fmt"
	"maps"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// widthTinyint is the width in bits of TINYINT.
	widthTinyint = 8

	// widthSmallint is the width in bits of SMALLINT.
	widthSmallint = 16

	// widthMediumint is the width in bits of MEDIUMINT.
	widthMediumint = 24

	// widthInt is the width in bits of INT.
	widthInt = 32

	// widthBigint is the width in bits of BIGINT, and the most a BIT value holds.
	widthBigint = 64

	// widthFloat is the width in bits of FLOAT, a single-precision float.
	widthFloat = 32

	// widthDouble is the width in bits of DOUBLE.
	widthDouble = 64

	// maxFloatPrecision is the most bits of precision FLOAT(p) keeps a single-precision
	// FLOAT for; a larger p makes the type a DOUBLE.
	maxFloatPrecision = 24

	// maxDoublePrecision is the most bits of precision FLOAT(p) takes; MySQL refuses a
	// larger p.
	maxDoublePrecision = 53
)

var (
	// floatPrecisionRefusal says what is wrong with FLOAT(p) for a p MySQL refuses.
	floatPrecisionRefusal = fmt.Sprintf("FLOAT takes a precision of at most %d bits", maxDoublePrecision)

	// builtinTypeMap maps lowercase MySQL type names to their structured SQLType
	// descriptors. FLOAT4 and FLOAT8 are MySQL's synonyms for FLOAT and DOUBLE.
	builtinTypeMap = map[string]sqlmodel.SQLType{
		// Signed integer types
		"tinyint":   sqlmodel.NewIntegerType("tinyint", widthTinyint, false),
		"smallint":  sqlmodel.NewIntegerType("smallint", widthSmallint, false),
		"mediumint": sqlmodel.NewIntegerType("mediumint", widthMediumint, false),
		"int":       sqlmodel.NewIntegerType("int", widthInt, false),
		"integer":   sqlmodel.NewIntegerType("int", widthInt, false),
		"bigint":    sqlmodel.NewIntegerType("bigint", widthBigint, false),
		// Unsigned integer types
		"tinyint unsigned":   sqlmodel.NewIntegerType("tinyint unsigned", widthTinyint, true),
		"smallint unsigned":  sqlmodel.NewIntegerType("smallint unsigned", widthSmallint, true),
		"mediumint unsigned": sqlmodel.NewIntegerType("mediumint unsigned", widthMediumint, true),
		"int unsigned":       sqlmodel.NewIntegerType("int unsigned", widthInt, true),
		"integer unsigned":   sqlmodel.NewIntegerType("int unsigned", widthInt, true),
		"bigint unsigned":    sqlmodel.NewIntegerType("bigint unsigned", widthBigint, true),
		// Float types
		"float":            sqlmodel.NewFloatType("float", widthFloat),
		"double":           sqlmodel.NewFloatType("double", widthDouble),
		"double precision": sqlmodel.NewFloatType("double", widthDouble),
		"real":             sqlmodel.NewFloatType("double", widthDouble),
		"float4":           sqlmodel.NewFloatType("float", widthFloat),
		"float8":           sqlmodel.NewFloatType("double", widthDouble),
		// Decimal types
		"decimal": sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "decimal"),
		"dec":     sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "decimal"),
		"numeric": sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "decimal"),
		"fixed":   sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "decimal"),
		// Boolean
		"boolean": sqlmodel.NewSQLType(sqlmodel.TypeCategoryBoolean, "tinyint"),
		"bool":    sqlmodel.NewSQLType(sqlmodel.TypeCategoryBoolean, "tinyint"),
		// Text types
		"char":       sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "char"),
		"varchar":    sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar"),
		"tinytext":   sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "tinytext"),
		"text":       sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"),
		"mediumtext": sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "mediumtext"),
		"longtext":   sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "longtext"),
		// Binary types
		"binary":     sqlmodel.NewSQLType(sqlmodel.TypeCategoryBinary, "binary"),
		"varbinary":  sqlmodel.NewSQLType(sqlmodel.TypeCategoryBinary, "varbinary"),
		"tinyblob":   sqlmodel.NewSQLType(sqlmodel.TypeCategoryBinary, "tinyblob"),
		"blob":       sqlmodel.NewSQLType(sqlmodel.TypeCategoryBinary, "blob"),
		"mediumblob": sqlmodel.NewSQLType(sqlmodel.TypeCategoryBinary, "mediumblob"),
		"longblob":   sqlmodel.NewSQLType(sqlmodel.TypeCategoryBinary, "longblob"),
		// Temporal types
		"date":      sqlmodel.NewTemporalType("date", sqlmodel.TemporalKindDate, false),
		"time":      sqlmodel.NewTemporalType("time", sqlmodel.TemporalKindTime, false),
		"datetime":  sqlmodel.NewTemporalType("datetime", sqlmodel.TemporalKindTimestamp, false),
		"timestamp": sqlmodel.NewTemporalType("timestamp", sqlmodel.TemporalKindTimestamp, true),
		"year":      sqlmodel.NewIntegerType("year", widthSmallint, false),
		// JSON
		"json": sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "json"),
		// Geometric types
		"geometry":           sqlmodel.NewSQLType(sqlmodel.TypeCategoryGeometric, "geometry"),
		"point":              sqlmodel.NewSQLType(sqlmodel.TypeCategoryGeometric, "point"),
		"linestring":         sqlmodel.NewSQLType(sqlmodel.TypeCategoryGeometric, "linestring"),
		"polygon":            sqlmodel.NewSQLType(sqlmodel.TypeCategoryGeometric, "polygon"),
		"multipoint":         sqlmodel.NewSQLType(sqlmodel.TypeCategoryGeometric, "multipoint"),
		"multilinestring":    sqlmodel.NewSQLType(sqlmodel.TypeCategoryGeometric, "multilinestring"),
		"multipolygon":       sqlmodel.NewSQLType(sqlmodel.TypeCategoryGeometric, "multipolygon"),
		"geometrycollection": sqlmodel.NewSQLType(sqlmodel.TypeCategoryGeometric, "geometrycollection"),
		// Other types
		"enum": sqlmodel.NewSQLType(sqlmodel.TypeCategoryEnum, "enum"),
		"set":  sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "set"),
		"bit":  sqlmodel.NewSQLType(sqlmodel.TypeCategoryBinary, "bit"),
	}
)

// typeResolver resolves type names: through the dialect's hooks, most recently added
// first, then its extra types, then the built-in MySQL types. A nil typeResolver resolves
// through the built-in types alone.
type typeResolver struct {
	// extraTypes holds the extra type definitions keyed by lower-cased name.
	extraTypes map[string]sqlmodel.SQLType

	// hooks holds the type normaliser hooks in registration order.
	hooks []func(string, []int) *sqlmodel.SQLType
}

// newTypeResolver returns a typeResolver over the dialect's hooks and extra types.
//
// Takes hooks ([]func(string, []int) *sqlmodel.SQLType) which are the hooks in
// registration order.
// Takes extraTypes (map[string]sqlmodel.SQLType) which are keyed by lower-cased name.
//
// Returns *typeResolver which resolves type names.
func newTypeResolver(hooks []func(string, []int) *sqlmodel.SQLType, extraTypes map[string]sqlmodel.SQLType) *typeResolver {
	return &typeResolver{extraTypes: extraTypes, hooks: hooks}
}

// normalise resolves a raw SQL type name to a structured SQLType. A FLOAT given a
// precision past maxFloatPrecision bits resolves as the DOUBLE MySQL makes it.
//
// Takes name (string) which is the raw type name to resolve.
// Takes modifiers (...int) which carry precision, scale, or length values to apply to the
// resolved type.
//
// Returns sqlmodel.SQLType which is the resolved type, with modifiers applied where
// applicable, or an unknown type carrying the lower-cased name when nothing resolves it.
func (r *typeResolver) normalise(name string, modifiers ...int) sqlmodel.SQLType {
	lowered := strings.ToLower(strings.TrimSpace(name))

	if r != nil {
		if resolved, answered := dialectkit.FirstHookAnswer(r.hooks,
			func(hook func(string, []int) *sqlmodel.SQLType) *sqlmodel.SQLType { return hook(lowered, modifiers) }); answered {
			return resolved
		}
	}

	if lowered == "" {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	}

	lowered = floatTypeForPrecision(lowered, modifiers)
	if sqlType, exists := r.lookup(lowered); exists {
		applyModifiers(&sqlType, modifiers)
		return sqlType
	}

	if base, found := strings.CutSuffix(lowered, " unsigned"); found {
		if sqlType, exists := r.lookup(base); exists {
			applyModifiers(&sqlType, modifiers)
			return sqlType
		}
	}

	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, lowered)
}

// normaliseWritten resolves a type name that may carry its modifiers written in it: a
// list of one to maxTypeModifiers numbers at the end of the name, as in decimal(10,2) or
// varchar(20), is read as the type's modifiers, and any other name is resolved whole.
//
// Takes name (string) which is the type name, with or without a modifier list.
//
// Returns sqlmodel.SQLType which is the resolved type, as normalise gives it.
func (r *typeResolver) normaliseWritten(name string) sqlmodel.SQLType {
	if base, written, found := splitTypeModifiers(name); found {
		return r.normalise(base, written...)
	}
	return r.normalise(name)
}

// lookup finds a lower-cased type name among the extra types, then the built-in types.
//
// Takes lowered (string) which is the lower-cased type name.
//
// Returns sqlmodel.SQLType which is a copy of the registered type.
// Returns bool which is true when the name is registered.
func (r *typeResolver) lookup(lowered string) (sqlmodel.SQLType, bool) {
	if r != nil {
		if sqlType, exists := r.extraTypes[lowered]; exists {
			return sqlType, true
		}
	}
	sqlType, exists := builtinTypeMap[lowered]
	return sqlType, exists
}

// buildTypeCatalogue constructs a TypeCatalogue from the built-in MySQL types merged with
// any extra types, which take precedence over a built-in type of the same name.
//
// Takes extraTypes (map[string]sqlmodel.SQLType) which provides additional type mappings,
// keyed by lower-cased name.
//
// Returns *ast.TypeCatalogue which is the merged catalogue.
func buildTypeCatalogue(extraTypes map[string]sqlmodel.SQLType) *ast.TypeCatalogue {
	catalogue := &ast.TypeCatalogue{
		Types: make(map[string]sqlmodel.SQLType, len(builtinTypeMap)+len(extraTypes)),
	}
	maps.Copy(catalogue.Types, builtinTypeMap)
	maps.Copy(catalogue.Types, extraTypes)
	return catalogue
}

// applyModifiers sets precision, scale, or length on the given SQLType based on the type
// category and the provided modifier values.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type to mutate.
// Takes modifiers ([]int) which is the slice of modifier values to apply.
func applyModifiers(sqlType *sqlmodel.SQLType, modifiers []int) {
	if len(modifiers) == 0 {
		return
	}
	switch sqlType.Category {
	case sqlmodel.TypeCategoryDecimal:
		if len(modifiers) >= 1 {
			sqlType.Precision = new(modifiers[0])
		}
		if len(modifiers) >= 2 {
			sqlType.Scale = new(modifiers[1])
		}
	case sqlmodel.TypeCategoryText:
		if len(modifiers) >= 1 {
			sqlType.Length = new(modifiers[0])
		}
	case sqlmodel.TypeCategoryTemporal:
		if len(modifiers) >= 1 {
			sqlType.Precision = new(modifiers[0])
		}
	default:
	}
}

// floatTypeForPrecision names the type FLOAT(p) makes, as MySQL and MariaDB do: a FLOAT
// for a precision of at most maxFloatPrecision bits and a DOUBLE past it. FLOAT(M, D),
// which gives a scale too, and every other type keep their names.
//
// Takes lowered (string) which is the lower-cased type name, ending in unsigned when the
// type is declared UNSIGNED.
// Takes modifiers ([]int) which are the type's modifiers.
//
// Returns string which is the name to resolve the type by.
func floatTypeForPrecision(lowered string, modifiers []int) string {
	if !namesFloat(lowered) || len(modifiers) != 1 || modifiers[0] <= maxFloatPrecision {
		return lowered
	}
	if strings.HasSuffix(lowered, " unsigned") {
		return "double unsigned"
	}
	return "double"
}

// floatPrecisionRefused reports whether a type is FLOAT(p) with a precision MySQL and
// MariaDB refuse: more than maxDoublePrecision bits.
//
// Takes lowered (string) which is the lower-cased type name.
// Takes modifiers ([]int) which are the type's modifiers.
//
// Returns bool which is true for FLOAT(p) or FLOAT4(p) with p past maxDoublePrecision.
func floatPrecisionRefused(lowered string, modifiers []int) bool {
	return namesFloat(lowered) && len(modifiers) == 1 && modifiers[0] > maxDoublePrecision
}

// namesFloat reports whether a lower-cased type name is FLOAT or its synonym FLOAT4, with
// or without UNSIGNED.
//
// Takes lowered (string) which is the lower-cased type name.
//
// Returns bool which is true for float, float4 and either with unsigned.
func namesFloat(lowered string) bool {
	base, _ := strings.CutSuffix(lowered, " unsigned")
	return base == "float" || base == "float4"
}

// formatTypeModifiers writes type modifiers as the parenthesised list splitTypeModifiers
// reads, such as (10,2).
//
// Takes modifiers ([]int) which are the modifiers in order.
//
// Returns string which is the list, or empty when there are no modifiers.
func formatTypeModifiers(modifiers []int) string {
	if len(modifiers) == 0 {
		return ""
	}
	written := make([]string, 0, len(modifiers))
	for _, modifier := range modifiers {
		written = append(written, strconv.Itoa(modifier))
	}
	return "(" + strings.Join(written, ",") + ")"
}

// splitTypeModifiers separates a list of one to maxTypeModifiers numbers written at the
// end of a type name, as in decimal(10, 2), from the name.
//
// Takes name (string) which is the type name.
//
// Returns string which is the name before the list, without surrounding spaces.
// Returns []int which holds the modifiers in order, each read as a modifier's number
// token is, and held at dialectkit.MaxTypeModifier.
// Returns bool which is false when the name ends in no such list.
func splitTypeModifiers(name string) (string, []int, bool) {
	opened, found := strings.CutSuffix(strings.TrimSpace(name), ")")
	if !found {
		return "", nil, false
	}
	base, list, found := strings.Cut(opened, "(")
	base = strings.TrimSpace(base)
	if !found || base == "" {
		return "", nil, false
	}
	parts := strings.SplitN(list, ",", maxTypeModifiers+1)
	if len(parts) > maxTypeModifiers {
		return "", nil, false
	}
	modifiers := make([]int, 0, len(parts))
	for _, part := range parts {
		modifier, isModifier := lexicalRules.Numbers.TypeModifierMagnitude(strings.TrimSpace(part))
		if !isModifier {
			return "", nil, false
		}
		modifiers = append(modifiers, modifier)
	}
	return base, modifiers, true
}

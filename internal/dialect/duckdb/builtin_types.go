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
	"maps"
	"strings"
	"unicode/utf8"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// widthTinyint is the width in bits of TINYINT and UTINYINT.
	widthTinyint = 8

	// widthSmallint is the width in bits of SMALLINT and USMALLINT.
	widthSmallint = 16

	// widthInteger is the width in bits of INTEGER and UINTEGER.
	widthInteger = 32

	// widthBigint is the width in bits of BIGINT and UBIGINT.
	widthBigint = 64

	// widthHugeint is the width in bits of HUGEINT and UHUGEINT.
	widthHugeint = 128

	// widthFloat is the width in bits of FLOAT, a single-precision float.
	widthFloat = 32

	// widthDouble is the width in bits of DOUBLE.
	widthDouble = 64

	// singlePrecisionName is the type FLOAT names: the single-precision FLOAT DuckDB stores
	// in four bytes, also written REAL or FLOAT4.
	singlePrecisionName = "float"

	// doublePrecisionName is the type DuckDB stores a FLOAT in when its precision needs a
	// DOUBLE.
	doublePrecisionName = "double"

	// maxSinglePrecisionBits is the most binary digits of precision a FLOAT(p) declares that
	// DuckDB stores as the single-precision FLOAT; FLOAT(25) to FLOAT(53) are DOUBLE.
	maxSinglePrecisionBits = 24

	// maxWrittenTypeRunes is the longest type written with its parts, such as MAP(VARCHAR,
	// STRUCT(a INTEGER)), that the resolver reads; a longer one is left unread.
	maxWrittenTypeRunes = 4096

	// writtenTypeReadsPerToken is how many reads of each of its tokens the parse of a
	// written type may make, far more than a parse that keeps advancing needs.
	writtenTypeReadsPerToken = 256
)

var (
	// builtinTypeMap maps lower-case DuckDB type names (including Postgres aliases) to their
	// normalised SQLType representations.
	builtinTypeMap = map[string]sqlmodel.SQLType{
		// Integer types (signed)
		"tinyint":  sqlmodel.NewIntegerType("int1", widthTinyint, false),
		"int1":     sqlmodel.NewIntegerType("int1", widthTinyint, false),
		"smallint": sqlmodel.NewIntegerType("int2", widthSmallint, false),
		"int2":     sqlmodel.NewIntegerType("int2", widthSmallint, false),
		"integer":  sqlmodel.NewIntegerType("int4", widthInteger, false),
		"int":      sqlmodel.NewIntegerType("int4", widthInteger, false),
		"int4":     sqlmodel.NewIntegerType("int4", widthInteger, false),
		"bigint":   sqlmodel.NewIntegerType("int8", widthBigint, false),
		"int8":     sqlmodel.NewIntegerType("int8", widthBigint, false),
		"hugeint":  sqlmodel.NewIntegerType("hugeint", widthHugeint, false),
		// Integer types (unsigned)
		"utinyint":  sqlmodel.NewIntegerType("utinyint", widthTinyint, true),
		"usmallint": sqlmodel.NewIntegerType("usmallint", widthSmallint, true),
		"uinteger":  sqlmodel.NewIntegerType("uinteger", widthInteger, true),
		"ubigint":   sqlmodel.NewIntegerType("ubigint", widthBigint, true),
		"uhugeint":  sqlmodel.NewIntegerType("uhugeint", widthHugeint, true),
		// Serial types (normalise to underlying integer)
		"smallserial": sqlmodel.NewIntegerType("int2", widthSmallint, false),
		"serial2":     sqlmodel.NewIntegerType("int2", widthSmallint, false),
		"serial":      sqlmodel.NewIntegerType("int4", widthInteger, false),
		"serial4":     sqlmodel.NewIntegerType("int4", widthInteger, false),
		"bigserial":   sqlmodel.NewIntegerType("int8", widthBigint, false),
		"serial8":     sqlmodel.NewIntegerType("int8", widthBigint, false),
		// Float types
		"real":             sqlmodel.NewFloatType("float4", widthFloat),
		"float4":           sqlmodel.NewFloatType("float4", widthFloat),
		"double precision": sqlmodel.NewFloatType("float8", widthDouble),
		"double":           sqlmodel.NewFloatType("float8", widthDouble),
		"float8":           sqlmodel.NewFloatType("float8", widthDouble),
		"float":            sqlmodel.NewFloatType("float4", widthFloat),
		// Decimal types
		"numeric": sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "numeric"),
		"decimal": sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "numeric"),
		// Boolean
		"boolean": sqlmodel.NewSQLType(sqlmodel.TypeCategoryBoolean, "bool"),
		"bool":    sqlmodel.NewSQLType(sqlmodel.TypeCategoryBoolean, "bool"),
		// Text types (DuckDB canonical text type is varchar)
		"text":              sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar"),
		"varchar":           sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar"),
		"character varying": sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar"),
		"character":         sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "char"),
		"char":              sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "char"),
		"bpchar":            sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "char"),
		"name":              sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar"),
		// Binary types
		"bytea": sqlmodel.NewSQLType(sqlmodel.TypeCategoryBinary, "blob"),
		"blob":  sqlmodel.NewSQLType(sqlmodel.TypeCategoryBinary, "blob"),
		// Temporal types
		"timestamp without time zone": sqlmodel.NewTemporalType("timestamp", sqlmodel.TemporalKindTimestamp, false),
		"timestamp":                   sqlmodel.NewTemporalType("timestamp", sqlmodel.TemporalKindTimestamp, false),
		"timestamp with time zone":    sqlmodel.NewTemporalType("timestamptz", sqlmodel.TemporalKindTimestamp, true),
		"timestamptz":                 sqlmodel.NewTemporalType("timestamptz", sqlmodel.TemporalKindTimestamp, true),
		"timestamp_s":                 sqlmodel.NewTemporalType("timestamp_s", sqlmodel.TemporalKindTimestamp, false),
		"timestamp_ms":                sqlmodel.NewTemporalType("timestamp_ms", sqlmodel.TemporalKindTimestamp, false),
		"timestamp_ns":                sqlmodel.NewTemporalType("timestamp_ns", sqlmodel.TemporalKindTimestamp, false),
		"date":                        sqlmodel.NewTemporalType("date", sqlmodel.TemporalKindDate, false),
		"time without time zone":      sqlmodel.NewTemporalType("time", sqlmodel.TemporalKindTime, false),
		"time":                        sqlmodel.NewTemporalType("time", sqlmodel.TemporalKindTime, false),
		"time with time zone":         sqlmodel.NewTemporalType("timetz", sqlmodel.TemporalKindTime, true),
		"timetz":                      sqlmodel.NewTemporalType("timetz", sqlmodel.TemporalKindTime, true),
		"interval":                    sqlmodel.NewTemporalType("interval", sqlmodel.TemporalKindInterval, false),
		// JSON types
		"json": sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "json"),
		// UUID
		"uuid": sqlmodel.NewSQLType(sqlmodel.TypeCategoryUUID, "uuid"),
		// Compound types (bare keywords - compound type parsing provides fields)
		"struct": sqlmodel.NewSQLType(sqlmodel.TypeCategoryStruct, "struct"),
		"map":    sqlmodel.NewSQLType(sqlmodel.TypeCategoryMap, "map"),
		"union":  sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnion, "union"),
		// Void
		"void": sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "void"),
	}

	// openingBrackets maps each closing bracket of a written type to the bracket it closes.
	openingBrackets = map[tokenKind]tokenKind{tokenRightParen: tokenLeftParen, tokenRightBracket: tokenLeftBracket}
)

// typeResolver resolves type names: through the dialect's hooks, most recently added
// first, then its extra types, then the built-in DuckDB types. A nil typeResolver
// resolves through the built-in types alone.
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

// normalise resolves a raw type name into a structured SQLType: the hooks first, then a
// name written with its parts, then a list suffix, then the extra types and finally the
// built-in types.
//
// Takes name (string) which is the raw type name as written.
// Takes modifiers (...int) which holds numeric modifiers such as precision and scale.
//
// Returns sqlmodel.SQLType which is the normalised type, or an unknown type carrying the
// lower-cased name when nothing resolves it.
func (r *typeResolver) normalise(name string, modifiers ...int) sqlmodel.SQLType {
	lowered := strings.ToLower(strings.TrimSpace(name))

	if resolved := r.fromHooks(lowered, modifiers); resolved != nil {
		return *resolved
	}

	if lowered == "" {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar")
	}

	if strings.ContainsAny(lowered, "([") {
		if result, read := r.normaliseWrittenType(lowered); read {
			return result
		}
	}

	if result, matched := r.normaliseArrayType(lowered, modifiers); matched {
		return result
	}

	if r != nil {
		if sqlType, exists := r.extraTypes[lowered]; exists {
			result := sqlType
			applyModifiers(&result, modifiers)
			return result
		}
	}

	return lookupBuiltinType(lowered, modifiers)
}

// normaliseWrittenType resolves a declared DuckDB type for type directives and overrides.
// It supports nested MAP, STRUCT and UNION types, modifiers, lists and fixed-size arrays.
//
// A type longer than maxWrittenTypeRunes is left unread. Its parse may read each token at
// most writtenTypeReadsPerToken times; a parse that stops advancing past that panics with
// an interrupt carrying dialectkit.ErrWalkBudget, which dialectkit.Recover or the
// caller's own guard turns back into the error.
//
// Takes lowered (string) which is the lower-cased type name.
//
// Returns sqlmodel.SQLType which is the type read.
// Returns bool which is false when the name is no type DuckDB can declare.
func (r *typeResolver) normaliseWrittenType(lowered string) (sqlmodel.SQLType, bool) {
	if utf8.RuneCountInString(lowered) > maxWrittenTypeRunes {
		return sqlmodel.SQLType{}, false
	}
	tokens, err := tokeniseSpan(lowered, 0, len(lowered))
	if err != nil || !balancedBrackets(tokens) {
		return sqlmodel.SQLType{}, false
	}
	typeParser := newParser(tokens)
	typeParser.Interrupt = writtenTypeReadBudget(len(tokens))
	sqlType, dimensions := typeParser.parseColumnType(writtenTypeNormaliser{resolver: r})
	if typeParser.statement.firstSyntaxError != nil || !typeParser.AtEnd() {
		return sqlmodel.SQLType{}, false
	}
	return functionArgumentArrayType(sqlType, dimensions), true
}

// fromHooks consults the hooks, most recently added first.
//
// Takes lowered (string) which is the lower-cased type name.
// Takes modifiers ([]int) which are the type's modifiers.
//
// Returns *sqlmodel.SQLType which is the first non-nil hook result, or nil.
func (r *typeResolver) fromHooks(lowered string, modifiers []int) *sqlmodel.SQLType {
	if r == nil {
		return nil
	}
	resolved, answered := dialectkit.FirstHookAnswer(r.hooks, func(hook func(string, []int) *sqlmodel.SQLType) *sqlmodel.SQLType {
		return hook(lowered, modifiers)
	})
	if !answered {
		return nil
	}
	return &resolved
}

// normaliseArrayType strips any trailing [] suffixes and resolves the element type
// recursively.
//
// Takes lowered (string) which is the lower-cased type name.
// Takes modifiers ([]int) which holds numeric modifiers to apply to the element type.
//
// Returns sqlmodel.SQLType which is the array type when matched.
// Returns bool which is true when lowered carried at least one array suffix.
func (r *typeResolver) normaliseArrayType(lowered string, modifiers []int) (sqlmodel.SQLType, bool) {
	baseName, found := strings.CutSuffix(lowered, arraySubscriptSuffix)
	if !found {
		return sqlmodel.SQLType{}, false
	}
	for {
		trimmed, more := strings.CutSuffix(baseName, arraySubscriptSuffix)
		if !more {
			break
		}
		baseName = trimmed
	}
	arrayType := sqlmodel.SQLType{}
	arrayType.Category = sqlmodel.TypeCategoryArray
	arrayType.EngineName = lowered
	arrayType.ElementType = new(r.normalise(baseName, modifiers...))
	return arrayType, true
}

// writtenTypeNormaliser resolves the names inside a type written with its parts through
// the resolver reading it.
type writtenTypeNormaliser struct {
	// resolver is the resolver reading the written type.
	resolver *typeResolver
}

// NormaliseTypeName resolves a type name inside a written type.
//
// Takes name (string) which is the raw type name.
// Takes modifiers (...int) which holds its numeric modifiers.
//
// Returns sqlmodel.SQLType which is the resolver's type for the name.
func (normaliser writtenTypeNormaliser) NormaliseTypeName(name string, modifiers ...int) sqlmodel.SQLType {
	return normaliser.resolver.normalise(name, modifiers...)
}

// buildTypeCatalogue combines the built-in DuckDB type set with any extension-supplied
// additions into a single TypeCatalogue.
//
// Takes extraTypes (map[string]sqlmodel.SQLType) which holds additional named types to
// merge over the built-in set.
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

// balancedBrackets reports whether every parenthesis and square bracket of a written type
// closes, in order.
//
// Takes tokens ([]token) which are the written type's tokens.
//
// Returns bool which is true when they balance.
func balancedBrackets(tokens []token) bool {
	var open []tokenKind
	for index := range tokens {
		kind := tokens[index].Kind
		if kind == tokenLeftParen || kind == tokenLeftBracket {
			open = append(open, kind)
			continue
		}
		opening, closes := openingBrackets[kind]
		if !closes {
			continue
		}
		if len(open) == 0 || open[len(open)-1] != opening {
			return false
		}
		open = open[:len(open)-1]
	}
	return len(open) == 0
}

// lookupBuiltinType resolves a known DuckDB type name and applies any numeric modifiers.
// A FLOAT whose precision needs more binary digits than the single-precision FLOAT holds,
// such as FLOAT(53), is a DOUBLE, as DuckDB stores it.
//
// Takes lowered (string) which is the lower-cased type name.
// Takes modifiers ([]int) which holds numeric modifiers.
//
// Returns sqlmodel.SQLType which is the resolved type, or an unknown-category fallback
// when lowered is not in the map.
func lookupBuiltinType(lowered string, modifiers []int) sqlmodel.SQLType {
	if lowered == singlePrecisionName && len(modifiers) > 0 && modifiers[0] > maxSinglePrecisionBits {
		lowered = doublePrecisionName
	}
	if sqlType, exists := builtinTypeMap[lowered]; exists {
		result := sqlType
		applyModifiers(&result, modifiers)
		return result
	}
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, lowered)
}

// applyModifiers writes numeric modifier values onto a SQLType according to its category.
// A precision or length must be at least one and a scale at least zero, and every value
// must be within maxTypeModifierValue; out-of-range modifiers are ignored rather than
// written into the catalogue.
//
// Takes sqlType (*sqlmodel.SQLType) which is mutated in place.
// Takes modifiers ([]int) which holds the modifier values to apply.
func applyModifiers(sqlType *sqlmodel.SQLType, modifiers []int) {
	if len(modifiers) == 0 {
		return
	}
	switch sqlType.Category {
	case sqlmodel.TypeCategoryDecimal:
		assignPositiveModifier(&sqlType.Precision, modifiers, 0)
		assignScaleModifier(&sqlType.Scale, modifiers, 1)
	case sqlmodel.TypeCategoryText:
		assignPositiveModifier(&sqlType.Length, modifiers, 0)
	case sqlmodel.TypeCategoryTemporal:
		assignPositiveModifier(&sqlType.Precision, modifiers, 0)
	default:
	}
}

// assignPositiveModifier writes the modifier at index into target when it is present and
// is a positive value within the accepted ceiling.
//
// Takes target (**int) which receives a pointer to the validated value.
// Takes modifiers ([]int) which holds the candidate values.
// Takes index (int) which is the position of the candidate within modifiers.
func assignPositiveModifier(target **int, modifiers []int, index int) {
	if index >= len(modifiers) {
		return
	}
	value := modifiers[index]
	if value < 1 || value > maxTypeModifierValue {
		return
	}
	*target = new(value)
}

// assignScaleModifier writes the modifier at index into target when it is present and is
// a non-negative scale within the accepted ceiling.
//
// Takes target (**int) which receives a pointer to the validated value.
// Takes modifiers ([]int) which holds the candidate values.
// Takes index (int) which is the position of the candidate within modifiers.
func assignScaleModifier(target **int, modifiers []int, index int) {
	if index >= len(modifiers) {
		return
	}
	value := modifiers[index]
	if value < 0 || value > maxTypeModifierValue {
		return
	}
	*target = new(value)
}

// promotionRank orders the integer types for promotion: by width, an unsigned type
// ranking one step above the signed type of its width, so it promotes to the next wider
// signed type.
//
// Takes sqlType (sqlmodel.SQLType) which is an integer type.
//
// Returns int which is the rank.
func promotionRank(sqlType sqlmodel.SQLType) int {
	rank := sqlType.Width * 2
	if sqlType.Unsigned {
		rank++
	}
	return rank
}

// newArrayType returns the DuckDB list type whose elements are element, named with the
// element's name and a [] suffix.
//
// Takes element (sqlmodel.SQLType) which is the element type.
//
// Returns sqlmodel.SQLType which is the array type.
func newArrayType(element sqlmodel.SQLType) sqlmodel.SQLType {
	arrayType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryArray, element.EngineName+arraySubscriptSuffix)
	arrayType.ElementType = &element
	return arrayType
}

// writtenTypeReadBudget returns an interrupt that stops the parse of a written type once
// it has read its tokens writtenTypeReadsPerToken times each, as a parse that has stopped
// advancing does. A written type is read where no context is at hand, so the interrupt
// counts reads alone.
//
// Takes tokens (int) which is how many tokens the written type holds.
//
// Returns func() error which is the Interrupt, returning dialectkit.ErrWalkBudget once
// the reads are spent.
func writtenTypeReadBudget(tokens int) func() error {
	remaining := writtenTypeReadsPerToken * (tokens + 1)
	return func() error {
		if remaining <= 0 {
			return dialectkit.ErrWalkBudget
		}
		remaining--
		return nil
	}
}

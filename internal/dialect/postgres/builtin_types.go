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
	"strings"
	"unicode/utf8"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// widthSmallint is the width in bits of smallint.
	widthSmallint = 16

	// widthInteger is the width in bits of integer, and of the unsigned object identifier
	// types.
	widthInteger = 32

	// widthBigint is the width in bits of bigint.
	widthBigint = 64

	// widthReal is the width in bits of real, a single-precision float.
	widthReal = 32

	// widthDouble is the width in bits of double precision.
	widthDouble = 64

	// writtenTypeMarks are the characters that mark a type name written as SQL, with a
	// double-quoted word or a modifier list, which the resolver reads as a column type.
	writtenTypeMarks = `"(`
	// maxWrittenTypeRunes is the longest type name written as SQL that the resolver reads as
	// a column type; a longer one is resolved as a bare name.
	maxWrittenTypeRunes = 1024
	// maxWrittenTypeTokens is the most tokens a type name written as SQL may hold for the
	// resolver to read it as a column type; one holding more is resolved as a bare name.
	maxWrittenTypeTokens = 64
)

var (
	// builtinTypeMap holds the Postgres builtin SQL types keyed by their canonical lowercase
	// spelling.
	builtinTypeMap = map[string]sqlmodel.SQLType{
		// Integer types
		"smallint":    sqlmodel.NewIntegerType("int2", widthSmallint, false),
		"int2":        sqlmodel.NewIntegerType("int2", widthSmallint, false),
		"integer":     sqlmodel.NewIntegerType("int4", widthInteger, false),
		"int":         sqlmodel.NewIntegerType("int4", widthInteger, false),
		"int4":        sqlmodel.NewIntegerType("int4", widthInteger, false),
		"bigint":      sqlmodel.NewIntegerType("int8", widthBigint, false),
		"int8":        sqlmodel.NewIntegerType("int8", widthBigint, false),
		"smallserial": sqlmodel.NewIntegerType("int2", widthSmallint, false),
		"serial2":     sqlmodel.NewIntegerType("int2", widthSmallint, false),
		"serial":      sqlmodel.NewIntegerType("int4", widthInteger, false),
		"serial4":     sqlmodel.NewIntegerType("int4", widthInteger, false),
		"bigserial":   sqlmodel.NewIntegerType("int8", widthBigint, false),
		"serial8":     sqlmodel.NewIntegerType("int8", widthBigint, false),
		// Float types
		"real":             sqlmodel.NewFloatType("float4", widthReal),
		"float4":           sqlmodel.NewFloatType("float4", widthReal),
		"double precision": sqlmodel.NewFloatType("float8", widthDouble),
		"float8":           sqlmodel.NewFloatType("float8", widthDouble),
		"float":            sqlmodel.NewFloatType("float8", widthDouble),
		// Decimal types
		"numeric": sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "numeric"),
		"decimal": sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "numeric"),
		// Boolean
		"boolean": sqlmodel.NewSQLType(sqlmodel.TypeCategoryBoolean, "bool"),
		"bool":    sqlmodel.NewSQLType(sqlmodel.TypeCategoryBoolean, "bool"),
		// Text types
		"text":              sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"),
		"character varying": sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar"),
		"varchar":           sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "varchar"),
		"character":         sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "char"),
		"char":              sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "char"),
		"bpchar":            sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "char"),
		"name":              sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "name"),
		"citext":            sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "citext"),
		// Bytea
		"bytea": sqlmodel.NewSQLType(sqlmodel.TypeCategoryBinary, "bytea"),
		// Bit string types (represented as Go strings of '0'/'1')
		"bit":         binaryFormType("bit"),
		"bit varying": binaryFormType("varbit"),
		"varbit":      binaryFormType("varbit"),
		// Temporal types
		"timestamp without time zone": sqlmodel.NewTemporalType("timestamp", sqlmodel.TemporalKindTimestamp, false),
		"timestamp":                   sqlmodel.NewTemporalType("timestamp", sqlmodel.TemporalKindTimestamp, false),
		"timestamp with time zone":    sqlmodel.NewTemporalType("timestamptz", sqlmodel.TemporalKindTimestamp, true),
		"timestamptz":                 sqlmodel.NewTemporalType("timestamptz", sqlmodel.TemporalKindTimestamp, true),
		"date":                        sqlmodel.NewTemporalType("date", sqlmodel.TemporalKindDate, false),
		"time without time zone":      sqlmodel.NewTemporalType("time", sqlmodel.TemporalKindTime, false),
		"time":                        sqlmodel.NewTemporalType("time", sqlmodel.TemporalKindTime, false),
		"time with time zone":         sqlmodel.NewTemporalType("timetz", sqlmodel.TemporalKindTime, true),
		"timetz":                      sqlmodel.NewTemporalType("timetz", sqlmodel.TemporalKindTime, true),
		"interval":                    sqlmodel.NewTemporalType("interval", sqlmodel.TemporalKindInterval, false),
		// JSON types
		"json":     sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "json"),
		"jsonb":    sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "jsonb"),
		"jsonpath": sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "jsonpath"),
		// UUID
		"uuid": sqlmodel.NewSQLType(sqlmodel.TypeCategoryUUID, "uuid"),
		// Network types
		"inet":     sqlmodel.NewNetworkType("inet", sqlmodel.NetworkKindPrefix),
		"cidr":     sqlmodel.NewNetworkType("cidr", sqlmodel.NetworkKindPrefix),
		"macaddr":  sqlmodel.NewNetworkType("macaddr", sqlmodel.NetworkKindHardware),
		"macaddr8": sqlmodel.NewNetworkType("macaddr8", sqlmodel.NetworkKindHardware),
		// Geometric types
		"point":   sqlmodel.NewSQLType(sqlmodel.TypeCategoryGeometric, "point"),
		"line":    sqlmodel.NewSQLType(sqlmodel.TypeCategoryGeometric, "line"),
		"lseg":    sqlmodel.NewSQLType(sqlmodel.TypeCategoryGeometric, "lseg"),
		"box":     sqlmodel.NewSQLType(sqlmodel.TypeCategoryGeometric, "box"),
		"path":    sqlmodel.NewSQLType(sqlmodel.TypeCategoryGeometric, "path"),
		"polygon": sqlmodel.NewSQLType(sqlmodel.TypeCategoryGeometric, "polygon"),
		"circle":  sqlmodel.NewSQLType(sqlmodel.TypeCategoryGeometric, "circle"),
		// Range types
		"int4range":      sqlmodel.NewSQLType(sqlmodel.TypeCategoryRange, "int4range"),
		"int8range":      sqlmodel.NewSQLType(sqlmodel.TypeCategoryRange, "int8range"),
		"numrange":       sqlmodel.NewSQLType(sqlmodel.TypeCategoryRange, "numrange"),
		"tsrange":        sqlmodel.NewSQLType(sqlmodel.TypeCategoryRange, "tsrange"),
		"tstzrange":      sqlmodel.NewSQLType(sqlmodel.TypeCategoryRange, "tstzrange"),
		"daterange":      sqlmodel.NewSQLType(sqlmodel.TypeCategoryRange, "daterange"),
		"int4multirange": sqlmodel.NewSQLType(sqlmodel.TypeCategoryRange, "int4multirange"),
		"int8multirange": sqlmodel.NewSQLType(sqlmodel.TypeCategoryRange, "int8multirange"),
		"nummultirange":  sqlmodel.NewSQLType(sqlmodel.TypeCategoryRange, "nummultirange"),
		"tsmultirange":   sqlmodel.NewSQLType(sqlmodel.TypeCategoryRange, "tsmultirange"),
		"tstzmultirange": sqlmodel.NewSQLType(sqlmodel.TypeCategoryRange, "tstzmultirange"),
		"datemultirange": sqlmodel.NewSQLType(sqlmodel.TypeCategoryRange, "datemultirange"),
		// Other system types
		"oid":      sqlmodel.NewIntegerType("oid", widthInteger, true),
		"xid8":     sqlmodel.NewIntegerType("xid8", widthBigint, true),
		"money":    moneyType(),
		"xml":      sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "xml"),
		"tsvector": binaryFormType("tsvector"),
		"tsquery":  sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "tsquery"),
		"regtype":  sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "regtype"),
		"regclass": sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "regclass"),
		"pg_lsn":   sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "pg_lsn"),
		"void":     sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "void"),
	}
)

// typeResolver resolves type names: through the dialect's hooks, most recently added
// first, then its extra types, then the built-in PostgreSQL types. A nil typeResolver
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

// normalise resolves a Postgres type name to its canonical SQLType.
//
// The name is read as one written outside a statement: unquoted, so it matches a type
// whatever its letter case and an unknown type carries it in lower case. When no
// modifiers are passed, a name holding a double-quoted word or a modifier list, such as
// "Shapes"."Pair" or numeric(10, -2), is read as a column type is: a quoted word keeps
// its letters and names a built-in, extra or hooked type only when written in lower case,
// the hooks are asked with the name and the modifiers the list holds, and the list gives
// the type its modifiers.
//
// Takes name (string) which is the raw type spelling to resolve.
// Takes modifiers (...int) which provides precision, scale, or length modifiers applied
// to the resolved type.
//
// Returns sqlmodel.SQLType which describes the resolved type, or an unknown type carrying
// the name when nothing resolves it.
func (r *typeResolver) normalise(name string, modifiers ...int) sqlmodel.SQLType {
	written := strings.TrimSpace(name)
	if len(modifiers) == 0 && strings.ContainsAny(written, writtenTypeMarks) {
		if sqlType, read := r.normaliseWrittenType(written); read {
			return sqlType
		}
	}

	lowered := strings.ToLower(written)
	if resolved := r.fromHooks(lowered, modifiers); resolved != nil {
		return *resolved
	}

	if lowered == "" {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text")
	}

	if baseName, found := strings.CutSuffix(lowered, arraySubscriptSuffix); found {
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
		return arrayType
	}

	if sqlType, found := r.lookup(lowered, modifiers); found {
		return sqlType
	}
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, lowered)
}

// resolveName resolves a type name as a statement's parser reads it: folded to lower case
// when it was written unquoted, and exactly as written when it was quoted.
//
// Unquoted names and quoted lower-case names are resolved through hooks, extra types and
// built-ins; built-ins retain their canonical spelling. Quoted names containing
// upper-case letters bypass this lookup: PostgreSQL distinguishes "Point" from point.
// Unresolved names retain their spelling so catalogue lookup can find quoted enums and
// composite types.
//
// Takes name (string) which is the type name as the parser read it.
// Takes quoted (bool) which is true when the name was written in double quotes.
// Takes modifiers ([]int) which are the type's modifiers.
//
// Returns sqlmodel.SQLType which is the resolved type, or an unknown type carrying name.
func (r *typeResolver) resolveName(name string, quoted bool, modifiers []int) sqlmodel.SQLType {
	lowered := strings.ToLower(name)
	if quoted && lowered != name {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, name)
	}
	if resolved := r.fromHooks(lowered, modifiers); resolved != nil {
		return *resolved
	}
	if sqlType, found := r.lookup(lowered, modifiers); found {
		return sqlType
	}
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, name)
}

// lookup finds a lower-cased type name among the extra types, then the built-in types,
// and applies modifiers to what it finds.
//
// Takes lowered (string) which is the lower-cased type name.
// Takes modifiers ([]int) which are the type's modifiers.
//
// Returns sqlmodel.SQLType which is the type found, with its modifiers applied.
// Returns bool which is false when neither the extra nor the built-in types hold it.
func (r *typeResolver) lookup(lowered string, modifiers []int) (sqlmodel.SQLType, bool) {
	if r != nil {
		if sqlType, exists := r.extraTypes[lowered]; exists {
			result := sqlType
			applyModifiers(&result, modifiers)
			return result, true
		}
	}

	if sqlType, exists := builtinTypeMap[lowered]; exists {
		result := sqlType
		applyModifiers(&result, modifiers)
		return result, true
	}

	return sqlmodel.SQLType{}, false
}

// normaliseWrittenType reads a type name written as SQL as a column type is read: a
// possibly schema-qualified name whose quoted words keep their letters, its modifier list
// and its array brackets.
//
// An array is named by its element type's name followed by one [] for each pair of
// brackets, and holds elements of the type the name names.
//
// A name longer than maxWrittenTypeRunes, or holding more than maxWrittenTypeTokens
// tokens, is left unread.
//
// Takes written (string) which is the type name, without surrounding spaces.
//
// Returns sqlmodel.SQLType which is the type read.
// Returns bool which is false when the name is too long, does not tokenise, or is not one
// whole column type.
func (r *typeResolver) normaliseWrittenType(written string) (sqlmodel.SQLType, bool) {
	if utf8.RuneCountInString(written) > maxWrittenTypeRunes {
		return sqlmodel.SQLType{}, false
	}
	tokens, read := tokeniseWrittenType(written)
	if !read {
		return sqlmodel.SQLType{}, false
	}
	typeParser := newParser(tokens)
	sqlType, dimensions := typeParser.parseColumnTypeInner(r)
	if typeParser.syntax.err != nil || !typeParser.AtEnd() {
		return sqlmodel.SQLType{}, false
	}
	if dimensions == 0 {
		return sqlType, true
	}
	arrayType := sqlmodel.SQLType{}
	arrayType.Category = sqlmodel.TypeCategoryArray
	arrayType.EngineName = sqlType.EngineName + strings.Repeat(arraySubscriptSuffix, dimensions)
	arrayType.ElementType = &sqlType
	return arrayType, true
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
	result, answered := dialectkit.FirstHookAnswer(r.hooks,
		func(hook func(string, []int) *sqlmodel.SQLType) *sqlmodel.SQLType { return hook(lowered, modifiers) })
	if !answered {
		return nil
	}
	return &result
}

// buildTypeCatalogue assembles a type catalogue from builtin and extra types.
//
// Takes extraTypes (map[string]sqlmodel.SQLType) which provides additional types layered
// over the builtins.
//
// Returns *ast.TypeCatalogue which holds the merged type map.
func buildTypeCatalogue(extraTypes map[string]sqlmodel.SQLType) *ast.TypeCatalogue {
	catalogue := &ast.TypeCatalogue{
		Types: make(map[string]sqlmodel.SQLType, len(builtinTypeMap)+len(extraTypes)),
	}
	maps.Copy(catalogue.Types, builtinTypeMap)
	maps.Copy(catalogue.Types, extraTypes)
	return catalogue
}

// applyModifiers writes precision, scale, or length modifiers onto sqlType.
//
// Takes sqlType (*sqlmodel.SQLType) which is the target type to mutate.
// Takes modifiers ([]int) which provides the modifier values in declaration order.
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

// binaryFormType returns a text type for bit, varbit or tsvector values whose binary
// representations pgx decodes only into its own types.
//
// Takes name (string) which is the type's engine name.
//
// Returns sqlmodel.SQLType which is the text type.
func binaryFormType(name string) sqlmodel.SQLType {
	text := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, name)
	text.BinaryForm = true
	return text
}

// moneyType returns the money type: an exact decimal holding an amount in the database's
// currency.
//
// Returns sqlmodel.SQLType which is the money type.
func moneyType() sqlmodel.SQLType {
	money := sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "money")
	money.Monetary = true
	return money
}

// tokeniseWrittenType reads the tokens of a type name written as SQL, ending with the
// end-of-input token.
//
// Takes written (string) which is the type name.
//
// Returns []token which holds the tokens.
// Returns bool which is false when the name does not tokenise or holds more than
// maxWrittenTypeTokens tokens.
func tokeniseWrittenType(written string) ([]token, bool) {
	next := readStatementTokens(written, 0, len(written))
	var tokens []token
	for range maxWrittenTypeTokens + 1 {
		current, err := next()
		if err != nil {
			return nil, false
		}
		tokens = append(tokens, current)
		if current.Kind == tokenEOF {
			return tokens, true
		}
	}
	return nil, false
}

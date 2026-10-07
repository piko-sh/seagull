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
	"maps"
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// typeNameInteger is the name of SQLite's INTEGER type and of its INT spelling.
	typeNameInteger = "integer"

	// typeNameBigint is the name of the BIGINT spelling, which the dialect also gives the
	// integers it knows hold any 64-bit value: a rowid, a STRICT table's integers, and the
	// results of counts and sums.
	typeNameBigint = "bigint"

	// typeNameReal is the name of SQLite's REAL type.
	typeNameReal = "real"

	// typeNameText is the name of SQLite's TEXT type and of the text affinity.
	typeNameText = "text"

	// typeNameBlob is the name of SQLite's BLOB type and of the blob affinity.
	typeNameBlob = "blob"

	// typeNameNumeric is the name of SQLite's NUMERIC type and of the numeric affinity.
	typeNameNumeric = "numeric"

	// widthInteger is the width in bits of every SQLite integer.
	widthInteger = 64

	// widthReal is the width in bits of REAL, an eight-byte float.
	widthReal = 64

	// rtreeCoordinateWidth is the width in bits of an rtree_i32 coordinate, which the module
	// stores as a 32-bit signed integer.
	rtreeCoordinateWidth = 32
)

var (
	// builtinTypeMap maps SQLite type spellings to normalised SQL types.
	//
	// This includes all SQLite type-affinity aliases. Every spelling with INTEGER affinity
	// is a signed 64-bit integer, because SQLite stores every integer in up to eight bytes
	// whatever width the declaration suggests. The spellings keep their own names, so a type
	// override can still single one out. JSONB and STRING have NUMERIC affinity, but a JSONB
	// column holds a JSON document, as text or as SQLite's binary JSONB, and a STRING column
	// holds text that SQLite stores as a number only when it reads as one, so neither is the
	// decimal type the affinity would give. DATE, DATETIME and TIMESTAMP have NUMERIC
	// affinity too, but both drivers read a column declared exactly so as a time.Time; a
	// declaration with a modifier, such as TIMESTAMP(3), they read as the value it stores,
	// so normaliseTypeName gives it text, and a CAST to one of them converts by the
	// affinity, so castTypeName names the affinity instead.
	builtinTypeMap = map[string]sqlmodel.SQLType{
		"integer":   sqlmodel.NewIntegerType(typeNameInteger, widthInteger, false),
		"int":       sqlmodel.NewIntegerType(typeNameInteger, widthInteger, false),
		"mediumint": sqlmodel.NewIntegerType("mediumint", widthInteger, false),
		"tinyint":   sqlmodel.NewIntegerType("tinyint", widthInteger, false),
		"smallint":  sqlmodel.NewIntegerType("smallint", widthInteger, false),
		"int2":      sqlmodel.NewIntegerType("smallint", widthInteger, false),
		"bigint":    sqlmodel.NewIntegerType(typeNameBigint, widthInteger, false),
		"int8":      sqlmodel.NewIntegerType(typeNameBigint, widthInteger, false),
		"text":      sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"),
		"clob":      sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"),
		"varchar":   sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"),
		"nchar":     sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"),
		"nvarchar":  sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"),
		"char":      sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"),
		"character": sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"),
		"string":    sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "text"),
		"real":      sqlmodel.NewFloatType(typeNameReal, widthReal),
		"double":    sqlmodel.NewFloatType(typeNameReal, widthReal),
		"float":     sqlmodel.NewFloatType(typeNameReal, widthReal),
		"blob":      sqlmodel.NewSQLType(sqlmodel.TypeCategoryBinary, "blob"),
		"any":       sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "any"),
		"numeric":   sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "numeric"),
		"decimal":   sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "numeric"),
		"boolean":   sqlmodel.NewSQLType(sqlmodel.TypeCategoryBoolean, "boolean"),
		"bool":      sqlmodel.NewSQLType(sqlmodel.TypeCategoryBoolean, "boolean"),
		"date":      sqlmodel.NewTemporalType("date", sqlmodel.TemporalKindDate, false),
		"datetime":  sqlmodel.NewTemporalType("datetime", sqlmodel.TemporalKindTimestamp, false),
		"timestamp": sqlmodel.NewTemporalType("timestamp", sqlmodel.TemporalKindTimestamp, false),
		"json":      sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "json"),
		"jsonb":     sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "jsonb"),
	}

	// storedValueTypeMap maps the spellings whose columns hold values the affinity SQLite
	// gives them does not describe.
	//
	// SQLite gives UUID, TIME, TIMESTAMPTZ, BYTEA and their like the numeric affinity, and
	// INTERVAL the integer one, but it stores a value that reads as no number, such as a
	// UUID's or a time's text, as it is written, and both drivers return that value as it is
	// stored: they read a column as a time.Time only when it is declared exactly DATE,
	// DATETIME or TIMESTAMP. So a UUID column is the UUID type, which reads a UUID's text or
	// its 16 bytes; a time of day, a zone-aware time, an interval, a document or an address
	// is text; and a column of bytes is binary. Each spelling keeps its own name, so a type
	// override can name it. A CAST to one of these spellings converts by the affinity all
	// the same, so it is typed by castTypeName.
	storedValueTypeMap = map[string]sqlmodel.SQLType{
		"uuid":                        sqlmodel.NewSQLType(sqlmodel.TypeCategoryUUID, "uuid"),
		"time":                        sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "time"),
		"timetz":                      sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "timetz"),
		"time with time zone":         sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "time with time zone"),
		"time without time zone":      sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "time without time zone"),
		"timestamptz":                 sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "timestamptz"),
		"timestamp with time zone":    sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "timestamp with time zone"),
		"timestamp without time zone": sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "timestamp without time zone"),
		"interval":                    sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "interval"),
		"xml":                         sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "xml"),
		"inet":                        sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "inet"),
		"cidr":                        sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "cidr"),
		"macaddr":                     sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "macaddr"),
		"bytea":                       sqlmodel.NewSQLType(sqlmodel.TypeCategoryBinary, "bytea"),
		"varbinary":                   sqlmodel.NewSQLType(sqlmodel.TypeCategoryBinary, "varbinary"),
		"binary":                      sqlmodel.NewSQLType(sqlmodel.TypeCategoryBinary, "binary"),
	}
)

// typeResolver resolves type names: through the dialect's hooks, most recently added
// first, then its extra types, then the built-in SQLite spellings and affinity rules. A
// nil typeResolver resolves through the built-in rules alone.
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

// normalise resolves a raw SQLite type spelling to a SQL type.
//
// Takes name (string) which is the raw type spelling from the source.
// Takes modifiers (...int) which holds optional precision and scale digits parsed from
// the type parentheses.
//
// Returns sqlmodel.SQLType which is a hook's result, an extra type, or the built-in
// normalisation of the name.
func (r *typeResolver) normalise(name string, modifiers ...int) sqlmodel.SQLType {
	if r == nil {
		return normaliseTypeName(name, modifiers...)
	}
	lowered := strings.ToLower(strings.TrimSpace(name))
	resolved, answered := dialectkit.FirstHookAnswer(r.hooks,
		func(hook func(string, []int) *sqlmodel.SQLType) *sqlmodel.SQLType { return hook(lowered, modifiers) })
	if answered {
		return resolved
	}
	if sqlType, exists := r.extraTypes[lowered]; exists {
		result := sqlType
		applyModifiers(&result, modifiers)
		return result
	}
	return normaliseTypeName(name, modifiers...)
}

// castTypeName resolves a CAST spelling through its SQLite affinity when conversion
// changes the declared type category. Types resolved by hooks or extra-type definitions
// retain their names.
//
// Takes name (string) which is the spelling the CAST names.
//
// Returns string which is the lower-cased name to resolve the cast's type by.
func (r *typeResolver) castTypeName(name string) string {
	lowered := strings.ToLower(strings.TrimSpace(name))
	if !convertsByAffinity(lowered) || r.resolvesItself(lowered) {
		return lowered
	}
	return affinityName(lowered)
}

// parameterCastTypeName resolves a cast parameter like castTypeName, except BOOLEAN
// remains bool because drivers bind it as 0 or 1 and numeric affinity preserves those
// values.
//
// Takes name (string) which is the spelling the CAST names.
//
// Returns string which is the lower-cased name to resolve the parameter's type by.
func (r *typeResolver) parameterCastTypeName(name string) string {
	lowered := strings.ToLower(strings.TrimSpace(name))
	if r.normalise(lowered).Category == sqlmodel.TypeCategoryBoolean {
		return lowered
	}
	return r.castTypeName(name)
}

// resolvesItself reports whether a hook or an extra type resolves a spelling, which then
// means what the dialect's caller made it mean.
//
// Takes lowered (string) which is the lower-cased spelling.
//
// Returns bool which is true when a hook or an extra type resolves it.
func (r *typeResolver) resolvesItself(lowered string) bool {
	if r == nil {
		return false
	}
	if _, exists := r.extraTypes[lowered]; exists {
		return true
	}
	_, answered := dialectkit.FirstHookAnswer(r.hooks,
		func(hook func(string, []int) *sqlmodel.SQLType) *sqlmodel.SQLType { return hook(lowered, nil) })
	return answered
}

// castTargetName names the type of a CAST to a spelling written with modifiers: the name
// castTypeName gives, followed by the modifiers as formatTypeModifiers writes them when
// the cast is typed by the spelling itself, so the cast has the type a column declared
// with the spelling and its modifiers has.
//
// A cast typed by the spelling's affinity takes no modifiers, as the affinity gives the
// converted value none.
//
// Takes name (string) which is the spelling the CAST names.
// Takes modifiers ([]int) which are the modifiers written after it, or nil.
//
// Returns string which is the name to resolve the cast's type by.
func (r *typeResolver) castTargetName(name string, modifiers []int) string {
	castName := r.castTypeName(name)
	if castName != strings.ToLower(strings.TrimSpace(name)) {
		return castName
	}
	return castName + formatTypeModifiers(modifiers)
}

// parameterCastType resolves the type a placeholder takes from a CAST to a spelling
// written with modifiers: the type parameterCastTypeName names, with the modifiers when
// that name is the spelling itself, as castTargetName keeps them.
//
// Takes name (string) which is the spelling the CAST names.
// Takes modifiers ([]int) which are the modifiers written after it, or nil.
//
// Returns sqlmodel.SQLType which is the placeholder's type.
func (r *typeResolver) parameterCastType(name string, modifiers []int) sqlmodel.SQLType {
	castName := r.parameterCastTypeName(name)
	if castName != strings.ToLower(strings.TrimSpace(name)) {
		return r.normalise(castName)
	}
	return r.normalise(castName, modifiers...)
}

// buildTypeCatalogue constructs the SQLite type catalogue.
//
// Takes extraTypes (map[string]sqlmodel.SQLType) which are the extra types, keyed by
// lower-cased name, that take precedence over a built-in type of the same name.
//
// Returns *ast.TypeCatalogue which holds every built-in SQLite type spelling, every
// spelling whose values take their own type, and every extra type, keyed by lowercase
// name.
func buildTypeCatalogue(extraTypes map[string]sqlmodel.SQLType) *ast.TypeCatalogue {
	catalogue := &ast.TypeCatalogue{
		Types: make(map[string]sqlmodel.SQLType, len(builtinTypeMap)+len(storedValueTypeMap)+len(extraTypes)),
	}
	maps.Copy(catalogue.Types, builtinTypeMap)
	maps.Copy(catalogue.Types, storedValueTypeMap)
	maps.Copy(catalogue.Types, extraTypes)
	return catalogue
}

// convertsByAffinity reports whether CAST uses a spelling's affinity to produce a
// different type category. This includes temporal names and BOOLEAN, JSON, JSONB, STRING
// and ANY.
//
// Takes lowered (string) which is the lower-cased spelling.
//
// Returns bool which is true when the cast is typed by the spelling's affinity.
func convertsByAffinity(lowered string) bool {
	return normaliseTypeName(lowered).Category != normaliseByAffinity(affinityName(lowered), nil).Category
}

// normaliseTypeName resolves a raw SQLite type spelling. Temporal names with modifiers
// remain text because drivers decode times only for exact DATE, DATETIME or TIMESTAMP
// declarations.
//
// Takes name (string) which is the raw type spelling from the source.
// Takes modifiers (...int) which holds optional precision and scale digits parsed from
// the type parentheses.
//
// Returns sqlmodel.SQLType which is the normalised SQL type.
func normaliseTypeName(name string, modifiers ...int) sqlmodel.SQLType {
	lowered := strings.ToLower(strings.TrimSpace(name))

	if lowered == "" {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryBinary, "blob")
	}

	if sqlType, exists := builtinTypeMap[lowered]; exists {
		if sqlType.Category == sqlmodel.TypeCategoryTemporal && len(modifiers) > 0 {
			return sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, sqlType.EngineName)
		}
		result := sqlType
		applyModifiers(&result, modifiers)
		return result
	}
	if sqlType, exists := storedValueTypeMap[lowered]; exists {
		result := sqlType
		applyModifiers(&result, modifiers)
		return result
	}

	return normaliseByAffinity(lowered, modifiers)
}

// normaliseByAffinity falls back to the SQLite affinity rules.
//
// Takes lowered (string) which is the lowercase trimmed type name.
// Takes modifiers ([]int) which holds optional precision and scale digits.
//
// Returns sqlmodel.SQLType which is the SQL type chosen by affinity match.
func normaliseByAffinity(lowered string, modifiers []int) sqlmodel.SQLType {
	switch affinityName(lowered) {
	case typeNameInteger:
		return sqlmodel.NewIntegerType(typeNameInteger, widthInteger, false)
	case typeNameText:
		result := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, typeNameText)
		applyModifiers(&result, modifiers)
		return result
	case typeNameBlob:
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryBinary, typeNameBlob)
	case typeNameReal:
		return sqlmodel.NewFloatType(typeNameReal, widthReal)
	default:
		result := sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, typeNameNumeric)
		applyModifiers(&result, modifiers)
		return result
	}
}

// affinityName returns the name of the affinity SQLite's rules give a type spelling, in
// their order: INT gives the integer affinity, CHAR, CLOB or TEXT the text one, BLOB the
// blob one, REAL, FLOA or DOUB the real one, and anything else the numeric one.
//
// Takes lowered (string) which is the lowercase trimmed type name.
//
// Returns string which is integer, text, blob, real or numeric.
func affinityName(lowered string) string {
	upper := strings.ToUpper(lowered)
	switch {
	case strings.Contains(upper, "INT"):
		return typeNameInteger
	case strings.Contains(upper, "CHAR") || strings.Contains(upper, "CLOB") || strings.Contains(upper, "TEXT"):
		return typeNameText
	case strings.Contains(upper, "BLOB"):
		return typeNameBlob
	case strings.Contains(upper, "REAL") || strings.Contains(upper, "FLOA") || strings.Contains(upper, "DOUB"):
		return typeNameReal
	default:
		return typeNameNumeric
	}
}

// applyModifiers attaches precision, length, and scale to a SQL type.
//
// Takes sqlType (*sqlmodel.SQLType) which receives the modifier values.
// Takes modifiers ([]int) which holds precision and scale digits in order.
func applyModifiers(sqlType *sqlmodel.SQLType, modifiers []int) {
	if sqlType.Category != sqlmodel.TypeCategoryDecimal && sqlType.Category != sqlmodel.TypeCategoryText {
		return
	}
	if len(modifiers) >= 1 {
		precision := modifiers[0]
		if sqlType.Category == sqlmodel.TypeCategoryText {
			sqlType.Length = &precision
		} else {
			sqlType.Precision = &precision
		}
	}
	if len(modifiers) >= 2 && sqlType.Category == sqlmodel.TypeCategoryDecimal {
		sqlType.Scale = new(modifiers[1])
	}
}

// formatTypeModifiers writes type modifiers as the parenthesised list splitTypeModifiers
// reads, such as (10,-2).
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

// splitTypeModifiers separates a list of one to maxTypeModifiers modifiers written at the
// end of a type name, as in decimal(10, 2) or varchar(255), from the name, reading each
// modifier as parseTypeModifiers reads a declared column's.
//
// Takes name (string) which is the type name.
//
// Returns string which is the name before the list, without surrounding spaces.
// Returns []int which holds the modifiers in order, each magnitude held at
// dialectkit.MaxTypeModifier.
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
		modifier, isModifier := writtenTypeModifier(part)
		if !isModifier {
			return "", nil, false
		}
		modifiers = append(modifiers, modifier)
	}
	return base, modifiers, true
}

// writtenTypeModifier reads one modifier written in a type name as parseTypeModifier
// reads its tokens: a number with an optional + or - sign, its magnitude read as
// typeModifierMagnitude reads it.
//
// Takes written (string) which is the modifier's text, spaces included.
//
// Returns int which is the modifier's value.
// Returns bool which is false when the text is not a signed number.
func writtenTypeModifier(written string) (int, bool) {
	number := strings.TrimSpace(written)
	negative := false
	if unsigned, cut := strings.CutPrefix(number, "-"); cut {
		negative, number = true, strings.TrimSpace(unsigned)
	} else if unsigned, cut := strings.CutPrefix(number, "+"); cut {
		number = strings.TrimSpace(unsigned)
	}
	magnitude, isNumber := lexicalRules.Numbers.TypeModifierMagnitude(number)
	if !isNumber {
		return 0, false
	}
	if negative {
		return -magnitude, true
	}
	return magnitude, true
}

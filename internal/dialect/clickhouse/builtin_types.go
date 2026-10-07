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

package clickhouse

import (
	"maps"
	"strconv"
	"strings"
	"unicode/utf8"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// intervalTypePrefix starts the name of every Interval type, such as IntervalDay.
	intervalTypePrefix = "Interval"

	// engineNameTime is the Time type, a time of day or signed span measured in seconds.
	engineNameTime = "Time"

	// engineNameTime64 is the Time64(precision) type, a time of day with fraction digits of
	// a second.
	engineNameTime64 = "Time64"

	// decimalRadix is the radix used by parseIntegerModifier.
	decimalRadix = 10

	// decimal32ImpliedPrecision is the precision implied by Decimal32(S).
	decimal32ImpliedPrecision = 9

	// decimal64ImpliedPrecision is the precision implied by Decimal64(S).
	decimal64ImpliedPrecision = 18

	// decimal128ImpliedPrecision is the precision implied by Decimal128(S).
	decimal128ImpliedPrecision = 38

	// decimal256ImpliedPrecision is the precision implied by Decimal256(S).
	decimal256ImpliedPrecision = 76

	// defaultMaxTypeParseDepth caps recursion through the type-name parser when descending
	// into nested wrappers (Array(Array(Tuple(...))), Nullable(LowCardinality(Array(...))),
	// etc.) when no WithMaxTypeParseDepth option is supplied. The cap keeps adversarial type
	// names from exhausting the goroutine stack while sitting far above realistic nesting,
	// and is the depth to which Seagull follows a type's parts.
	defaultMaxTypeParseDepth = sqlmodel.MaxTypeDepth

	// maxTypeNameErrorRunes bounds how much of an offending type name an error message
	// quotes, so a huge malformed input cannot balloon every diagnostic that reports it.
	maxTypeNameErrorRunes = 64

	// widthByte is the width in bits of Int8 and UInt8.
	widthByte = 8

	// widthShort is the width in bits of Int16, UInt16 and BFloat16.
	widthShort = 16

	// widthWord is the width in bits of Int32, UInt32 and Float32.
	widthWord = 32

	// widthLong is the width in bits of Int64, UInt64 and Float64.
	widthLong = 64

	// widthWide is the width in bits of Int128 and UInt128.
	widthWide = 128

	// widthWidest is the width in bits of Int256 and UInt256.
	widthWidest = 256

	// maxTypeModifierValue bounds a type-modifier integer (FixedString length, Decimal
	// precision/scale, DateTime64 scale). It is generous enough for any realistic type while
	// rejecting absurd inputs and preventing int overflow during accumulation.
	maxTypeModifierValue = 1 << 30
)

var (
	// intervalUnits are the units ClickHouse has an Interval type for, such as IntervalDay.
	intervalUnits = []string{"Nanosecond", "Microsecond", "Millisecond", "Second", "Minute", "Hour", "Day", "Week", "Month", "Quarter", "Year"}

	// clickhousePrimitiveTypes maps ClickHouse type names to their structured SQLType.
	// Wrapper types (Nullable, Array, LowCardinality, Tuple, Map, Nested, Enum) are NOT in
	// this table; they are parsed by parseClickHouseType and built compositionally from the
	// inner type's catalogue entry.
	//
	// Keys are lower-cased for the analyser's case-insensitive lookup; the SQLType values
	// retain the canonical spelling. buildTypeCatalogue layers extra types over these
	// entries.
	clickhousePrimitiveTypes = primitiveTypes()
)

// typeParseResult carries the structured result of parsing a ClickHouse type-name string.
// The outer wrappers (Nullable, LowCardinality) are stripped during parsing; the result
// reports what was stripped so the caller can apply nullability / cardinality flags to
// the consumer (column, parameter, function return).
type typeParseResult struct {
	// SQLType is the inner type after wrapper stripping.
	SQLType sqlmodel.SQLType

	// Nullable is true when the outermost wrapper was Nullable(T). Inner Nullable wrappers
	// (inside Array(Nullable(T)) etc.) are preserved on the inner SQLType so the consumer
	// can introspect.
	Nullable bool

	// LowCardinality is true when LowCardinality(T) appeared as an outer wrapper. Currently
	// the analyser treats it as a no-op because LowCardinality is a storage hint with no
	// Go-side impact.
	LowCardinality bool
}

// newTypeParseResult wraps a parsed type in a result that reports no stripped outer
// wrappers.
//
// Takes sqlType (sqlmodel.SQLType) which is the parsed type.
//
// Returns typeParseResult which carries sqlType with the Nullable and LowCardinality
// flags unset.
func newTypeParseResult(sqlType sqlmodel.SQLType) typeParseResult {
	return typeParseResult{
		SQLType:        sqlType,
		Nullable:       false,
		LowCardinality: false,
	}
}

// typeNameParser is a small recursive-descent parser over a ClickHouse type-name string.
//
// The parser is deliberately lightweight: it does not tokenise via the SQL tokeniser
// because type names are a closed sub-grammar and reusing the SQL lexer would carry far
// more machinery than the few productions need. The depth field bounds recursion through
// nested wrappers so adversarial inputs cannot blow the Go stack.
type typeNameParser struct {
	// resolver supplies the hooks and extra types a bare type name is looked up through
	// before the built-in primitives; nil means the built-ins alone.
	resolver *typeResolver

	// input is the type-name string being parsed.
	input string

	// position is the cursor offset into input.
	position int

	// depth is the current recursion depth through nested wrappers.
	depth int

	// maxDepth is the cap on depth; parseType fails with an error wrapping
	// dialectkit.ErrParseDepth beyond it.
	maxDepth int
}

// skipWhitespace advances the cursor past any spaces, tabs, and line breaks.
func (p *typeNameParser) skipWhitespace() {
	for p.position < len(p.input) {
		character := p.input[p.position]
		if character == ' ' || character == '\t' || character == '\n' || character == '\r' {
			p.position++
			continue
		}
		break
	}
}

// peek returns the character at the current cursor, or zero on EOF.
//
// Returns byte which is the current character, or zero at end of input.
func (p *typeNameParser) peek() byte {
	if p.position >= len(p.input) {
		return 0
	}
	return p.input[p.position]
}

// consume advances the cursor and returns the consumed character.
//
// Returns byte which is the consumed character.
func (p *typeNameParser) consume() byte {
	character := p.peek()
	p.position++
	return character
}

// skipSingleQuotedString consumes a single-quoted string literal.
//
// The literal is consumed including its quotes. It honours backslash escapes so a quote,
// comma, or parenthesis inside the literal does not prematurely terminate an enclosing
// scan. The cursor must be positioned on the opening quote. On an unterminated literal it
// consumes to end of input.
func (p *typeNameParser) skipSingleQuotedString() {
	if p.peek() != '\'' {
		return
	}
	p.consume()
	for p.position < len(p.input) {
		switch p.peek() {
		case '\\':
			p.consume()
			p.consume()
		case '\'':
			p.consume()
			return
		default:
			p.consume()
		}
	}
}

// match consumes one expected character and returns true on success.
//
// Takes character (byte) which is the character expected at the cursor.
//
// Returns bool which is true when the character matched and was consumed.
func (p *typeNameParser) match(character byte) bool {
	if p.peek() != character {
		return false
	}
	p.position++
	return true
}

// parseIdentifier reads a bare identifier, since the type-name production does not need
// quoted identifiers.
//
// The scan advances by UTF-8 rune width so multi-byte codepoints inside an identifier
// body are not truncated mid-codepoint.
//
// Returns string which is the identifier text, or empty when none is at the cursor.
func (p *typeNameParser) parseIdentifier() string {
	start := p.position
	for p.position < len(p.input) {
		character, width := utf8.DecodeRuneInString(p.input[p.position:])
		if !isIdentPart(character) {
			break
		}
		p.position += width
	}
	return p.input[start:p.position]
}

// parseType is the top-level production that dispatches on the leading identifier to the
// appropriate wrapper handler or primitive lookup.
//
// The depth counter is incremented on entry and decremented on exit so nested wrappers
// (Array(Array(Tuple(...))) and similar) cannot drive the parser past maxDepth recursive
// frames.
//
// Returns typeParseResult which holds the parsed type.
// Returns error when the type-name is malformed or recursion exceeds maxDepth.
func (p *typeNameParser) parseType() (typeParseResult, error) {
	if p.depth >= p.maxDepth {
		return typeParseResult{}, dialectkit.DepthError(p.maxDepth)
	}
	p.depth++
	defer func() { p.depth-- }()

	p.skipWhitespace()
	identifier := p.parseIdentifier()
	if identifier == "" {
		return typeParseResult{}, &typeNameParseError{
			input:    p.input,
			position: p.position,
			message:  "expected type identifier",
		}
	}
	if result, dispatched, err := p.dispatchWrapperType(identifier); dispatched {
		return result, err
	}
	return p.dispatchScalarType(identifier)
}

// dispatchWrapperType handles the wrapper-type identifiers (Nullable, LowCardinality,
// Array, Tuple, Map, Nested, Enum / Enum8 / Enum16, JSON, Dynamic, Object, Variant,
// AggregateFunction / SimpleAggregateFunction). Returns dispatched=false when the
// identifier is not a wrapper so the caller can fall through to the scalar dispatch.
//
// Wrapper dispatch precedes scalar lookup so the inner types and modifiers are parsed
// before a name can fall through to the primitive catalogue.
//
// Takes identifier (string), the leading type identifier text.
//
// Returns result (typeParseResult), the parsed type when dispatched.
// Returns dispatched (bool), true when this helper handled the identifier.
// Returns err (error), any parse error from the wrapper body.
func (p *typeNameParser) dispatchWrapperType(identifier string) (result typeParseResult, dispatched bool, err error) {
	switch strings.ToLower(identifier) {
	case "nullable":
		result, err = p.parseNullableWrapper()
		return result, true, err
	case "lowcardinality":
		result, err = p.parseLowCardinalityWrapper()
		return result, true, err
	case "array":
		result, err = p.parseArrayType()
		return result, true, err
	case "tuple":
		result, err = p.parseTupleType()
		return result, true, err
	case "map":
		result, err = p.parseMapType()
		return result, true, err
	case "nested":
		result, err = p.parseNestedType()
		return result, true, err
	}
	return p.dispatchEnumOrSpecialType(identifier)
}

// dispatchEnumOrSpecialType continues the wrapper-type dispatch with the enum /
// parametrised / specialised wrapper identifiers (Enum, Enum8, Enum16, JSON, Dynamic,
// FixedString, Decimal variants, DateTime / DateTime64, Time / Time64, Variant, Object,
// AggregateFunction, SimpleAggregateFunction). Returns dispatched=false on miss so
// dispatchWrapperType can fall through.
//
// Takes identifier (string), the leading type identifier text.
//
// Returns result, dispatched, err with the same semantics as dispatchWrapperType.
func (p *typeNameParser) dispatchEnumOrSpecialType(identifier string) (result typeParseResult, dispatched bool, err error) {
	switch strings.ToLower(identifier) {
	case "enum":

		result, err = p.parseEnumType("Enum8")
		return result, true, err
	case "enum8", "enum16":
		result, err = p.parseEnumType(identifier)
		return result, true, err
	case "json":
		result, err = p.parseJSONType()
		return result, true, err
	case "dynamic":
		result, err = p.parseDynamicType()
		return result, true, err
	case "fixedstring":
		result, err = p.parseFixedStringType()
		return result, true, err
	case "decimal", "decimal32", "decimal64", "decimal128", "decimal256":
		result, err = p.parseDecimalType(identifier)
		return result, true, err
	case "datetime", "datetime64", "time", "time64":
		return p.dispatchTemporalType(identifier)
	case "variant":
		result, err = p.parseVariantType()
		return result, true, err
	case "object":
		result, err = p.parseObjectType()
		return result, true, err
	case "aggregatefunction", "simpleaggregatefunction":
		result, err = p.parseAggregateFunctionType(identifier)
		return result, true, err
	}
	return typeParseResult{}, false, nil
}

// dispatchTemporalType parses the temporal types that take modifiers or none: DateTime,
// DateTime64, Time and Time64.
//
// Takes identifier (string), the leading type identifier text.
//
// Returns result, dispatched, err with the same semantics as dispatchWrapperType.
func (p *typeNameParser) dispatchTemporalType(identifier string) (result typeParseResult, dispatched bool, err error) {
	switch strings.ToLower(identifier) {
	case "datetime":
		result, err = p.parseDateTimeType()
	case "datetime64":
		result, err = p.parseDateTime64Type()
	case "time":
		result = newTypeParseResult(sqlmodel.NewTemporalType(engineNameTime, sqlmodel.TemporalKindTime, false))
	default:
		result, err = p.parseTime64Type()
	}
	return result, true, err
}

// parseTime64Type handles Time64(precision), a time of day, or a span of up to 999 hours
// either side of zero, with fraction digits of a second. Both clickhouse-go drivers read
// it as a time.Duration.
//
// Returns typeParseResult which holds the time type with its precision.
// Returns error when the Time64 body is not well-formed.
func (p *typeNameParser) parseTime64Type() (typeParseResult, error) {
	if !p.match('(') {
		return typeParseResult{}, p.errorAt("expected '(' after Time64")
	}
	precision, err := p.parseIntegerModifier()
	if err != nil {
		return typeParseResult{}, err
	}
	p.skipWhitespace()
	if !p.match(')') {
		return typeParseResult{}, p.errorAt("expected ')' to close Time64")
	}
	sqlType := sqlmodel.NewTemporalType(engineNameTime64, sqlmodel.TemporalKindTime, false)
	sqlType.Precision = &precision
	return newTypeParseResult(sqlType), nil
}

// dispatchScalarType handles the residual scalar / primitive identifiers that did not
// match the wrapper dispatch. Currently every non-wrapper name is looked up through
// parsePrimitiveByName.
//
// Takes identifier (string), the leading type identifier text.
//
// Returns the parsed type and any error from the primitive lookup.
func (p *typeNameParser) dispatchScalarType(identifier string) (typeParseResult, error) {
	return p.parsePrimitiveByName(identifier)
}

// parseObjectType handles the legacy ClickHouse Object('json') declaration that predates
// the dedicated JSON type.
//
// The form takes a single string-literal argument naming the underlying object kind; the
// only value ClickHouse ever shipped is 'json', which is the alias of the JSON type. The
// parser treats any payload as the JSON type so other Object variants still resolve to
// the JSON surface; consumers that need to distinguish the literal argument can do so by
// inspecting the source SQL.
//
// Returns typeParseResult which holds the JSON type.
// Returns error when the Object body is not well-formed.
func (p *typeNameParser) parseObjectType() (typeParseResult, error) {
	if !p.match('(') {
		return typeParseResult{}, p.errorAt("expected '(' after Object")
	}
	p.skipWhitespace()

	for p.position < len(p.input) && p.peek() != ')' {
		if p.peek() == '\'' {
			p.skipSingleQuotedString()
			continue
		}
		p.consume()
	}
	if !p.match(')') {
		return typeParseResult{}, p.errorAt("expected ')' to close Object")
	}
	return newTypeParseResult(sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "JSON")), nil
}

// parseNullableWrapper handles Nullable(T). Sets the result's Nullable flag and returns
// the inner type unwrapped.
//
// Returns typeParseResult which holds the inner type with the Nullable flag set.
// Returns error when the Nullable body is not well-formed.
func (p *typeNameParser) parseNullableWrapper() (typeParseResult, error) {
	if !p.match('(') {
		return typeParseResult{}, p.errorAt("expected '(' after Nullable")
	}
	inner, err := p.parseType()
	if err != nil {
		return typeParseResult{}, err
	}
	p.skipWhitespace()
	if !p.match(')') {
		return typeParseResult{}, p.errorAt("expected ')' to close Nullable")
	}
	inner.Nullable = true
	return inner, nil
}

// parseLowCardinalityWrapper handles LowCardinality(T). The wrapper is a storage hint
// with no Go-side impact, so the parser strips it and propagates the inner type's flags
// through.
//
// Returns typeParseResult which holds the inner type with the LowCardinality flag set.
// Returns error when the LowCardinality body is not well-formed.
func (p *typeNameParser) parseLowCardinalityWrapper() (typeParseResult, error) {
	if !p.match('(') {
		return typeParseResult{}, p.errorAt("expected '(' after LowCardinality")
	}
	inner, err := p.parseType()
	if err != nil {
		return typeParseResult{}, err
	}
	p.skipWhitespace()
	if !p.match(')') {
		return typeParseResult{}, p.errorAt("expected ')' to close LowCardinality")
	}
	inner.LowCardinality = true
	return inner, nil
}

// parseArrayType handles Array(T). The result's SQLType is TypeCategoryArray with
// ElementType pointing at the inner.
//
// Returns typeParseResult which holds the array type with its element type.
// Returns error when the Array body is not well-formed.
func (p *typeNameParser) parseArrayType() (typeParseResult, error) {
	if !p.match('(') {
		return typeParseResult{}, p.errorAt("expected '(' after Array")
	}
	inner, err := p.parseType()
	if err != nil {
		return typeParseResult{}, err
	}
	p.skipWhitespace()
	if !p.match(')') {
		return typeParseResult{}, p.errorAt("expected ')' to close Array")
	}
	element := inner.SQLType
	element.Nullable = element.Nullable || inner.Nullable
	return newTypeParseResult(arrayOf(element)), nil
}

// parseTupleType handles the anonymous Tuple(T1, T2, ...) form and the named Tuple(name1
// T1, name2 T2, ...) form.
//
// Anonymous fields are synthesised as _1, _2, and so on to keep the StructFields slice
// homogeneous so downstream consumers can index fields uniformly regardless of whether
// the tuple was declared with names.
//
// Returns typeParseResult which holds the struct type with its fields.
// Returns error when the Tuple body is not well-formed.
func (p *typeNameParser) parseTupleType() (typeParseResult, error) {
	if !p.match('(') {
		return typeParseResult{}, p.errorAt("expected '(' after Tuple")
	}
	var fields []sqlmodel.StructField
	fieldIndex := 1
	for {
		p.skipWhitespace()
		field, err := p.parseTupleField(fieldIndex)
		if err != nil {
			return typeParseResult{}, err
		}
		fields = append(fields, field)
		fieldIndex++
		p.skipWhitespace()
		if !p.match(',') {
			break
		}
	}
	p.skipWhitespace()
	if !p.match(')') {
		return typeParseResult{}, p.errorAt("expected ')' to close Tuple")
	}
	return newTypeParseResult(tupleOf(fields)), nil
}

// parseTupleField reads one field of a Tuple body.
//
// ClickHouse allows both anonymous (Tuple(String, UInt64)) and named (Tuple(s String, u
// UInt64)) fields; the parser looks two identifiers deep to disambiguate. The returned
// StructField.Name carries either the explicit name (named form) or the synthesised _N
// placeholder (anonymous form). For example, parseTupleField(1) on "name String" returns
// a field with Name="name"; on "String" alone it returns a field with Name="_1".
//
// Takes anonymousIndex (int) which is the 1-based position used to synthesise an
// anonymous field name.
//
// Returns sqlmodel.StructField which is the parsed field with its name and type.
// Returns error when the field is not well-formed.
func (p *typeNameParser) parseTupleField(anonymousIndex int) (sqlmodel.StructField, error) {
	savedPosition := p.position
	first := p.parseIdentifier()
	p.skipWhitespace()

	nextRune, _ := utf8.DecodeRuneInString(p.input[p.position:])
	if first != "" && p.position < len(p.input) && isIdentStart(nextRune) {
		fieldType, err := p.parseType()
		if err != nil {
			return sqlmodel.StructField{}, err
		}
		merged := fieldType.SQLType
		merged.Nullable = merged.Nullable || fieldType.Nullable
		return sqlmodel.StructField{
			Name:       first,
			SQLType:    merged,
			Positional: false,
		}, nil
	}

	p.position = savedPosition
	fieldType, err := p.parseType()
	if err != nil {
		return sqlmodel.StructField{}, err
	}
	merged := fieldType.SQLType
	merged.Nullable = merged.Nullable || fieldType.Nullable
	return sqlmodel.StructField{
		Name:       synthesiseAnonymousFieldName(anonymousIndex),
		SQLType:    merged,
		Positional: true,
	}, nil
}

// parseMapType handles Map(K, V). The K type becomes KeyType, the V type becomes
// ElementType (matching the existing TypeCategoryMap convention used by duckdb).
//
// Returns typeParseResult which holds the map type with its key and value types.
// Returns error when the Map body is not well-formed.
func (p *typeNameParser) parseMapType() (typeParseResult, error) {
	if !p.match('(') {
		return typeParseResult{}, p.errorAt("expected '(' after Map")
	}
	keyType, err := p.parseType()
	if err != nil {
		return typeParseResult{}, err
	}
	p.skipWhitespace()
	if !p.match(',') {
		return typeParseResult{}, p.errorAt("expected ',' between Map key and value types")
	}
	valueType, err := p.parseType()
	if err != nil {
		return typeParseResult{}, err
	}
	p.skipWhitespace()
	if !p.match(')') {
		return typeParseResult{}, p.errorAt("expected ')' to close Map")
	}
	key := keyType.SQLType
	key.Nullable = key.Nullable || keyType.Nullable
	value := valueType.SQLType
	value.Nullable = value.Nullable || valueType.Nullable
	return newTypeParseResult(mapOf(key, value)), nil
}

// parseNestedType handles Nested(field1 T1, field2 T2, ...). The parser desugars to
// Array(Tuple(field1 T1, field2 T2, ...)) to match the underlying ClickHouse storage
// model and let downstream consumers treat nested columns uniformly with array-of-tuple.
//
// Returns typeParseResult which holds the array-of-tuple type.
// Returns error when the Nested body is not well-formed.
func (p *typeNameParser) parseNestedType() (typeParseResult, error) {
	if !p.match('(') {
		return typeParseResult{}, p.errorAt("expected '(' after Nested")
	}
	var fields []sqlmodel.StructField
	for {
		p.skipWhitespace()
		fieldName := p.parseIdentifier()
		if fieldName == "" {
			return typeParseResult{}, p.errorAt("expected field name in Nested(...)")
		}
		p.skipWhitespace()
		fieldType, err := p.parseType()
		if err != nil {
			return typeParseResult{}, err
		}
		merged := fieldType.SQLType
		merged.Nullable = merged.Nullable || fieldType.Nullable
		fields = append(fields, sqlmodel.StructField{
			Name:       fieldName,
			SQLType:    merged,
			Positional: false,
		})
		p.skipWhitespace()
		if !p.match(',') {
			break
		}
	}
	p.skipWhitespace()
	if !p.match(')') {
		return typeParseResult{}, p.errorAt("expected ')' to close Nested")
	}
	tuple := tupleOf(fields)
	return newTypeParseResult(arrayOf(tuple)), nil
}

// parseEnumType handles Enum8('a' = 1, 'b' = 2, ...) and the Enum16 variant. The numeric
// tag is parsed but discarded because the framework treats Enum8 and Enum16 as
// user-defined enums and only the value list matters for Go-side codegen.
//
// Takes name (string) which is the original enum type name (Enum8 or Enum16).
//
// Returns typeParseResult which holds the parsed Enum type with its value list.
// Returns error when the enum body is not well-formed.
func (p *typeNameParser) parseEnumType(name string) (typeParseResult, error) {
	if !p.match('(') {
		return typeParseResult{}, p.errorAt("expected '(' after " + name)
	}
	var values []string
	for {
		value, parseErr := p.parseEnumEntry()
		if parseErr != nil {
			return typeParseResult{}, parseErr
		}
		values = append(values, value)
		p.skipWhitespace()
		if !p.match(',') {
			break
		}
	}
	p.skipWhitespace()
	if !p.match(')') {
		return typeParseResult{}, p.errorAt("expected ')' to close " + name)
	}
	sqlType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryEnum, name)
	sqlType.EnumValues = values
	return newTypeParseResult(sqlType), nil
}

// parseEnumEntry consumes a single 'value' [ = N ] enum entry from the input cursor.
//
// The label is decoded as the tokeniser decodes a string literal, so a doubled quote
// stands for one quote and a backslash starts an escape sequence, and the label holds the
// text the server returns for it. The numeric tag is parsed but discarded so the caller
// need only collect the labels.
//
// Returns string which is the decoded enum label.
// Returns error when the entry is malformed, such as an unterminated quote or a missing
// leading apostrophe.
func (p *typeNameParser) parseEnumEntry() (string, error) {
	p.skipWhitespace()
	if p.peek() != '\'' {
		return "", p.errorAt("expected single-quoted enum value")
	}
	value, end, terminated := decodeQuoted(p.input, p.position, '\'')
	if !terminated {
		p.position = end
		return "", p.errorAt("unterminated enum value")
	}
	p.position = end
	p.skipWhitespace()
	if p.match('=') {
		p.skipNumericTag()
	}
	return value, nil
}

// skipNumericTag advances over the numeric tag that follows the `=` separator in an enum
// entry. The tag may be negative.
func (p *typeNameParser) skipNumericTag() {
	p.skipWhitespace()
	if p.peek() == '-' {
		p.consume()
	}
	for p.position < len(p.input) && isDigit(p.input[p.position]) {
		p.position++
	}
}

// parseFixedStringType handles FixedString(N). The N modifier becomes the Length field on
// the resulting SQLType.
//
// Returns typeParseResult which holds the text type with its length.
// Returns error when the FixedString body is not well-formed.
func (p *typeNameParser) parseFixedStringType() (typeParseResult, error) {
	if !p.match('(') {
		return typeParseResult{}, p.errorAt("expected '(' after FixedString")
	}
	length, err := p.parseIntegerModifier()
	if err != nil {
		return typeParseResult{}, err
	}
	p.skipWhitespace()
	if !p.match(')') {
		return typeParseResult{}, p.errorAt("expected ')' to close FixedString")
	}
	sqlType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, "FixedString")
	sqlType.Length = &length
	return newTypeParseResult(sqlType), nil
}

// parseDecimalType handles Decimal(P, S), Decimal32(S), Decimal64(S), Decimal128(S), and
// Decimal256(S). The Px variants hard-code precision via the type name; only S is
// parameterised.
//
// Takes name (string) which is the original Decimal type name.
//
// Returns typeParseResult which holds the decimal type with its precision and scale.
// Returns error when the Decimal body is not well-formed.
func (p *typeNameParser) parseDecimalType(name string) (typeParseResult, error) {
	if !p.match('(') {
		return typeParseResult{}, p.errorAt("expected '(' after " + name)
	}
	first, err := p.parseIntegerModifier()
	if err != nil {
		return typeParseResult{}, err
	}
	p.skipWhitespace()
	var precision *int
	var scale *int
	switch strings.ToLower(name) {
	case "decimal":
		if !p.match(',') {
			return typeParseResult{}, p.errorAt("expected ',' between Decimal precision and scale")
		}
		second, err := p.parseIntegerModifier()
		if err != nil {
			return typeParseResult{}, err
		}
		precision = &first
		scale = &second
	case "decimal32":
		precision = new(decimal32ImpliedPrecision)
		scale = &first
	case "decimal64":
		precision = new(decimal64ImpliedPrecision)
		scale = &first
	case "decimal128":
		precision = new(decimal128ImpliedPrecision)
		scale = &first
	case "decimal256":
		precision = new(decimal256ImpliedPrecision)
		scale = &first
	}
	p.skipWhitespace()
	if !p.match(')') {
		return typeParseResult{}, p.errorAt("expected ')' to close " + name)
	}
	sqlType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, name)
	sqlType.Precision = precision
	sqlType.Scale = scale
	return newTypeParseResult(sqlType), nil
}

// parseDateTimeType handles DateTime (no modifier) and DateTime('TZ') (with timezone).
//
// The timezone is captured but not retained on the SQLType because the codegen target
// (time.Time) carries its own timezone awareness.
//
// Returns typeParseResult which holds the temporal type.
// Returns error when the DateTime body is not well-formed.
func (p *typeNameParser) parseDateTimeType() (typeParseResult, error) {
	result := newTypeParseResult(sqlmodel.NewTemporalType("DateTime", sqlmodel.TemporalKindTimestamp, true))
	if p.peek() != '(' {
		return result, nil
	}
	p.consume()
	p.skipWhitespace()
	if p.match('\'') {
		for p.position < len(p.input) && p.input[p.position] != '\'' {
			p.position++
		}
		if p.position < len(p.input) {
			p.consume()
		}
	}
	p.skipWhitespace()
	if !p.match(')') {
		return typeParseResult{}, p.errorAt("expected ')' to close DateTime")
	}
	return result, nil
}

// parseDateTime64Type handles DateTime64(precision) and DateTime64(precision, 'TZ').
//
// The precision becomes the Precision field; any timezone is parsed and discarded for the
// reason described on parseDateTimeType.
//
// Returns typeParseResult which holds the temporal type with its precision.
// Returns error when the DateTime64 body is not well-formed.
func (p *typeNameParser) parseDateTime64Type() (typeParseResult, error) {
	if !p.match('(') {
		return typeParseResult{}, p.errorAt("expected '(' after DateTime64")
	}
	precision, err := p.parseIntegerModifier()
	if err != nil {
		return typeParseResult{}, err
	}
	p.skipWhitespace()
	if p.match(',') {
		p.skipWhitespace()
		if p.match('\'') {
			for p.position < len(p.input) && p.input[p.position] != '\'' {
				p.position++
			}
			if p.position < len(p.input) {
				p.consume()
			}
		}
	}
	p.skipWhitespace()
	if !p.match(')') {
		return typeParseResult{}, p.errorAt("expected ')' to close DateTime64")
	}
	sqlType := sqlmodel.NewTemporalType("DateTime64", sqlmodel.TemporalKindTimestamp, true)
	sqlType.Precision = &precision
	return newTypeParseResult(sqlType), nil
}

// parseJSONType handles the bare JSON form and the parametrised form.
//
// The parametrised JSON(max_dynamic_paths=N, path.to TypeName, SKIP path) body is
// consumed opaquely, since its contents do not affect the Go-side type, which is always
// any-like, but the parser must still detect and balance the parens so any trailing input
// can be surfaced as a trailing-junk error by the top-level driver.
//
// Returns typeParseResult which holds the JSON type.
// Returns error when the parametrised body is not well-formed.
func (p *typeNameParser) parseJSONType() (typeParseResult, error) {
	result := newTypeParseResult(sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, "JSON"))
	if p.peek() != '(' {
		return result, nil
	}
	if err := p.skipBalancedParens(); err != nil {
		return typeParseResult{}, err
	}
	return result, nil
}

// parseDynamicType handles both the bare Dynamic form and the parametrised
// Dynamic(max_types=N) form. Like JSON, the body is consumed opaquely; only the wrapper's
// presence matters at the catalogue layer because Dynamic is always type-erased on the Go
// side.
//
// Returns typeParseResult which holds the Dynamic type.
// Returns error when the parametrised body is not well-formed.
func (p *typeNameParser) parseDynamicType() (typeParseResult, error) {
	result := newTypeParseResult(sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, "Dynamic"))
	if p.peek() != '(' {
		return result, nil
	}
	if err := p.skipBalancedParens(); err != nil {
		return typeParseResult{}, err
	}
	return result, nil
}

// parseVariantType handles Variant(T1, T2, ...). Modelled as a union over the member
// types so the emitter can generate a tagged-variant Go destination where wanted
// (defaults to any).
//
// Returns typeParseResult which holds the union type with its members.
// Returns error when the Variant body is not well-formed.
func (p *typeNameParser) parseVariantType() (typeParseResult, error) {
	if !p.match('(') {
		return typeParseResult{}, p.errorAt("expected '(' after Variant")
	}
	var members []sqlmodel.UnionMember
	memberIndex := 1
	for {
		p.skipWhitespace()
		memberType, err := p.parseType()
		if err != nil {
			return typeParseResult{}, err
		}
		merged := memberType.SQLType
		merged.Nullable = merged.Nullable || memberType.Nullable
		members = append(members, sqlmodel.UnionMember{
			Tag:     synthesiseAnonymousFieldName(memberIndex),
			SQLType: merged,
		})
		memberIndex++
		p.skipWhitespace()
		if !p.match(',') {
			break
		}
	}
	p.skipWhitespace()
	if !p.match(')') {
		return typeParseResult{}, p.errorAt("expected ')' to close Variant")
	}
	sqlType := sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnion, "Variant")
	sqlType.UnionMembers = members
	return newTypeParseResult(sqlType), nil
}

// parseAggregateFunctionType handles `AggregateFunction(name, argType1, ...)` and the
// SimpleAggregateFunction variant.
//
// A SimpleAggregateFunction(name, T) column holds plain values of T, which ClickHouse and
// both clickhouse-go drivers read as T, so it is typed as T itself.
//
// The aggregate's name and argument-type list are captured into a reconstructed
// EngineName ("AggregateFunction(uniq, UInt64)") so downstream codegen can distinguish
// aggregate-state columns from finalised aggregate results; the carried EngineName also
// feeds the resolver when it needs to know which aggregate produced the state. The result
// Category is TypeCategoryAggregateState so consumers can branch on the category without
// parsing the EngineName again.
//
// The inner argument type remains accessible via ElementType for the (common) case where
// only the first argument is meaningful for codegen. An aggregate that takes no argument,
// such as AggregateFunction(count), holds an unknown ElementType.
//
// Takes name (string), the wrapper name (AggregateFunction or SimpleAggregateFunction).
//
// Returns the reconstructed typeParseResult; returns error when the body is malformed
// (missing comma, unterminated paren).
func (p *typeNameParser) parseAggregateFunctionType(name string) (typeParseResult, error) {
	if !p.match('(') {
		return typeParseResult{}, p.errorAt("expected '(' after " + name)
	}
	p.skipWhitespace()
	aggregateName := p.parseIdentifier()
	if aggregateName == "" {
		return typeParseResult{}, p.errorAt("expected aggregate name")
	}

	p.skipWhitespace()
	if p.peek() == '(' {
		if err := p.skipBalancedParens(); err != nil {
			return typeParseResult{}, err
		}
	}
	p.skipWhitespace()
	if p.match(')') {
		reconstructedName := name + "(" + aggregateName + ")"
		return newTypeParseResult(aggregateStateOf(reconstructedName, unknownType())), nil
	}
	if !p.match(',') {
		return typeParseResult{}, p.errorAt("expected ',' between aggregate name and arg type")
	}
	innerType, argumentTypeNames, err := p.parseAggregateArgumentTypes(name)
	if err != nil {
		return typeParseResult{}, err
	}
	if strings.EqualFold(name, engineNameSimpleAggregateFunction) {
		return innerType, nil
	}
	innerCopy := innerType.SQLType
	innerCopy.Nullable = innerCopy.Nullable || innerType.Nullable
	reconstructedName := name + "(" + aggregateName + ", " + strings.Join(argumentTypeNames, ", ") + ")"
	return newTypeParseResult(aggregateStateOf(reconstructedName, innerCopy)), nil
}

// parseAggregateArgumentTypes reads the argument types of an AggregateFunction or
// SimpleAggregateFunction type after the comma that follows the aggregate's name, up to
// and including the closing paren.
//
// Takes name (string), the wrapper name, which an error names.
//
// Returns typeParseResult which is the first argument's type.
// Returns []string which holds the engine names of every argument's type, in order.
// Returns error when a type is malformed or the closing paren is missing.
func (p *typeNameParser) parseAggregateArgumentTypes(name string) (typeParseResult, []string, error) {
	first, err := p.parseType()
	if err != nil {
		return typeParseResult{}, nil, err
	}
	names := []string{strings.TrimSpace(first.SQLType.EngineName)}
	for {
		p.skipWhitespace()
		if !p.match(',') {
			break
		}
		extra, extraErr := p.parseType()
		if extraErr != nil {
			return typeParseResult{}, nil, extraErr
		}
		names = append(names, strings.TrimSpace(extra.SQLType.EngineName))
	}
	p.skipWhitespace()
	if !p.match(')') {
		return typeParseResult{}, nil, p.errorAt("expected ')' to close " + name)
	}
	return first, names, nil
}

// skipBalancedParens consumes a parenthesised body starting at the current cursor
// position, where the cursor must point at the opening paren.
//
// All inner characters are discarded; the cursor lands just past the matching close
// paren. parseAggregateFunctionType uses it to swallow the aggregate's call-parameter
// list, such as quantile(0.5), when present.
//
// Returns error when the parens are unbalanced or the input ends before the close paren
// is reached.
func (p *typeNameParser) skipBalancedParens() error {
	if !p.match('(') {
		return p.errorAt("expected '(' for balanced skip")
	}
	depth := 1
	for depth > 0 && p.position < len(p.input) {
		character := p.consume()
		switch character {
		case '(':
			depth++
		case ')':
			depth--
		default:
		}
	}
	if depth != 0 {
		return p.errorAt("unbalanced parens in aggregate call-parameter list")
	}
	return nil
}

// parsePrimitiveByName looks up a bare type name in the primitive catalogue.
//
// An unrecognised name yields an Unknown-category SQLType so downstream consumers can
// still proceed with a placeholder; the analyser surfaces the unknown type via a
// diagnostic later.
//
// Takes identifier (string) which is the bare type name to look up.
//
// Returns typeParseResult which holds the matched type, or an Unknown type when no match.
// Returns error which is always nil.
func (p *typeNameParser) parsePrimitiveByName(identifier string) (typeParseResult, error) {
	if sqlType, found := p.resolver.lookup(identifier, nil); found {
		return newTypeParseResult(sqlType), nil
	}
	canonical := canonicaliseTypeName(identifier)
	if sqlType, found := clickhousePrimitiveTypes[canonical]; found {
		return newTypeParseResult(sqlType), nil
	}
	return newTypeParseResult(sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, identifier)), nil
}

// parseIntegerModifier consumes a non-negative decimal integer and returns its value.
//
// FixedString(N), Decimal(P, S), and DateTime64(N) use it. A leading minus sign is
// rejected because none of these modifiers may be negative, and the magnitude is bounded
// by maxTypeModifierValue so an overlong run of digits cannot overflow.
//
// Returns int which is the parsed modifier value.
// Returns error when no digits are present, the value is negative, or it exceeds
// maxTypeModifierValue.
func (p *typeNameParser) parseIntegerModifier() (int, error) {
	p.skipWhitespace()
	if p.peek() == '-' {
		return 0, p.errorAt("integer modifier must not be negative")
	}
	start := p.position
	for p.position < len(p.input) && isDigit(p.input[p.position]) {
		p.position++
	}
	if start == p.position {
		return 0, p.errorAt("expected integer modifier")
	}
	value := 0
	for index := start; index < p.position; index++ {
		digit := int(p.input[index] - '0')
		if value > (maxTypeModifierValue-digit)/decimalRadix {
			return 0, p.errorAt("integer modifier out of range")
		}
		value = value*decimalRadix + digit
	}
	return value, nil
}

// errorAt is a shorthand for constructing a typeNameParseError at the current cursor
// position.
//
// Takes message (string) which is the diagnostic text.
//
// Returns error which is the typeNameParseError at the current position.
func (p *typeNameParser) errorAt(message string) error {
	return &typeNameParseError{
		input:    p.input,
		position: p.position,
		message:  message,
	}
}

// typeNameParseError is the error returned for malformed type-name strings. Carries the
// offending position so diagnostics can highlight it.
type typeNameParseError struct {
	// input is the type-name string that failed to parse.
	input string

	// message is the diagnostic describing the failure.
	message string

	// position is the cursor offset where the failure was detected.
	position int
}

// Error returns the formatted diagnostic for the parse failure.
//
// The offending type name is quoted up to maxTypeNameErrorRunes runes; a longer name is
// cut on a rune boundary and the message says how long the full name was, so a huge
// malformed input cannot balloon the diagnostic.
//
// Returns string which is the diagnostic including the message, type name, and position.
func (e *typeNameParseError) Error() string {
	return "clickhouse: " + e.message + " in type name " + quoteTypeNameForError(e.input) + " at position " + strconv.Itoa(e.position)
}

// Unwrap returns the generator's parse sentinel, so errors.Is matches a malformed type
// name as a syntax error.
//
// Returns error which is the parse sentinel.
func (*typeNameParseError) Unwrap() error {
	return new(dialectkit.ParseError).Unwrap()
}

// typeResolver resolves ClickHouse type names through hooks, extra types and built-ins.
//
// Hooks and extra types apply to DDL, placeholders and cast targets, including nested
// wrappers.
type typeResolver struct {
	// extras holds the extra types, keyed by lower-cased name.
	extras map[string]sqlmodel.SQLType

	// hooks holds the normaliser hooks in registration order; the last is consulted first.
	hooks []func(string, []int) *sqlmodel.SQLType

	// maxDepth caps wrapper nesting inside a type name.
	maxDepth int
}

// newTypeResolver returns a resolver over the given hooks and extra types.
//
// Takes hooks ([]func(string, []int) *sqlmodel.SQLType) which are the normaliser hooks in
// registration order.
// Takes extras (map[string]sqlmodel.SQLType) which holds the extra types, keyed by
// lower-cased name.
// Takes maxDepth (int) which caps wrapper nesting inside a type name.
//
// Returns *typeResolver which resolves type names.
func newTypeResolver(hooks []func(string, []int) *sqlmodel.SQLType, extras map[string]sqlmodel.SQLType, maxDepth int) *typeResolver {
	return &typeResolver{hooks: hooks, extras: extras, maxDepth: maxDepth}
}

// lookup resolves a name through the hooks, most recent first, and then the extra types.
// A nil resolver resolves nothing.
//
// Takes name (string) which is the type name.
// Takes modifiers ([]int) which are the type's modifiers, passed to the hooks.
//
// Returns sqlmodel.SQLType which is the resolved type.
// Returns bool which is true when a hook or an extra type resolved the name.
func (r *typeResolver) lookup(name string, modifiers []int) (sqlmodel.SQLType, bool) {
	if r == nil {
		return sqlmodel.SQLType{}, false
	}
	resolved, answered := dialectkit.FirstHookAnswer(r.hooks, func(hook func(string, []int) *sqlmodel.SQLType) *sqlmodel.SQLType {
		return hook(name, modifiers)
	})
	if answered {
		return resolved, true
	}
	if sqlType, found := r.extras[canonicaliseTypeName(name)]; found {
		return sqlType, true
	}
	return sqlmodel.SQLType{}, false
}

// parse parses a type name, keeping its Nullable and LowCardinality wrappers as flags. A
// name a hook or an extra type resolves whole is returned without wrappers.
//
// Takes input (string) which is the type name.
//
// Returns typeParseResult which holds the parsed type and its wrapper flags.
// Returns error when the type name is malformed or nests too deeply.
func (r *typeResolver) parse(input string) (typeParseResult, error) {
	if sqlType, found := r.lookup(strings.TrimSpace(input), nil); found {
		return newTypeParseResult(sqlType), nil
	}
	return parseClickHouseTypeWith(input, r)
}

// normalise resolves a type name to its structured SQLType, discarding the Nullable and
// LowCardinality outer wrapper flags returned by the parser.
//
// Callers that need those flags use parse. A name that does not parse resolves to an
// unknown type carrying the name.
//
// Takes name (string) which is the ClickHouse type-name spelling.
// Takes modifiers (...int) which are passed to the hooks.
//
// Returns sqlmodel.SQLType which is the structured, unwrapped type.
func (r *typeResolver) normalise(name string, modifiers ...int) sqlmodel.SQLType {
	if sqlType, found := r.lookup(name, modifiers); found {
		return sqlType
	}
	result, err := parseClickHouseTypeWith(name, r)
	if err != nil {
		return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, name)
	}
	return result.SQLType
}

// primitiveTypes builds the map of ClickHouse primitive types, keyed by lower-cased name.
//
// Returns map[string]sqlmodel.SQLType which holds every primitive type.
func primitiveTypes() map[string]sqlmodel.SQLType {
	types := make(map[string]sqlmodel.SQLType)
	registerPrimitiveNumericTypes(types)
	registerPrimitiveScalarTypes(types)
	registerPrimitiveSpecialisedTypes(types)
	return types
}

// registerPrimitive lower-cases the type name, stamps it onto the SQLType, and inserts it
// into the primitives map being built. The helper is used by the registerPrimitive* group
// functions so the boilerplate stays out of the registration list itself.
//
// Takes types (map[string]sqlmodel.SQLType), the primitives map being built.
// Takes name (string), the canonical ClickHouse type spelling.
// Takes sqlType (sqlmodel.SQLType), the structured type without engine name.
func registerPrimitive(types map[string]sqlmodel.SQLType, name string, sqlType sqlmodel.SQLType) {
	sqlType.EngineName = name
	types[strings.ToLower(name)] = sqlType
}

// registerPrimitiveNumericTypes registers the integer and floating point primitive types.
//
// Unsigned integers (UInt8, UInt16, UInt32, UInt64, UInt128, UInt256), signed integers
// (Int8, Int16, Int32, Int64, Int128, Int256), and floats (Float32, Float64, BFloat16)
// all map to integer or float categories; the engine's type mapping table picks the
// Go-side representation.
//
// Takes types (map[string]sqlmodel.SQLType) which receives the types.
func registerPrimitiveNumericTypes(types map[string]sqlmodel.SQLType) {
	registerPrimitive(types, "UInt8", sqlmodel.NewIntegerType("", widthByte, true))
	registerPrimitive(types, "UInt16", sqlmodel.NewIntegerType("", widthShort, true))
	registerPrimitive(types, "UInt32", sqlmodel.NewIntegerType("", widthWord, true))
	registerPrimitive(types, "UInt64", sqlmodel.NewIntegerType("", widthLong, true))
	registerPrimitive(types, "UInt128", sqlmodel.NewIntegerType("", widthWide, true))
	registerPrimitive(types, "UInt256", sqlmodel.NewIntegerType("", widthWidest, true))

	registerPrimitive(types, "Int8", sqlmodel.NewIntegerType("", widthByte, false))
	registerPrimitive(types, "Int16", sqlmodel.NewIntegerType("", widthShort, false))
	registerPrimitive(types, "Int32", sqlmodel.NewIntegerType("", widthWord, false))
	registerPrimitive(types, "Int64", sqlmodel.NewIntegerType("", widthLong, false))
	registerPrimitive(types, "Int128", sqlmodel.NewIntegerType("", widthWide, false))
	registerPrimitive(types, "Int256", sqlmodel.NewIntegerType("", widthWidest, false))

	registerPrimitive(types, "Float32", sqlmodel.NewFloatType("", widthWord))
	registerPrimitive(types, "Float64", sqlmodel.NewFloatType("", widthLong))
	registerPrimitive(types, "BFloat16", sqlmodel.NewFloatType("", widthShort))
}

// registerPrimitiveScalarTypes registers booleans, the canonical String type, the Date /
// DateTime variants and the Interval types, one per unit, which INTERVAL literals and the
// toInterval functions produce. FixedString(N) and DateTime64(N) are not registered here;
// they require a precision modifier and are handled by parseClickHouseType.
//
// DateTime stores an instant, shown in the column's or the server's time zone, so it is
// zone-aware.
//
// Takes types (map[string]sqlmodel.SQLType) which receives the types.
func registerPrimitiveScalarTypes(types map[string]sqlmodel.SQLType) {
	registerPrimitive(types, "Bool", sqlmodel.NewSQLType(sqlmodel.TypeCategoryBoolean, ""))
	registerPrimitive(types, "Boolean", sqlmodel.NewSQLType(sqlmodel.TypeCategoryBoolean, ""))

	registerPrimitive(types, "String", sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, ""))

	registerPrimitive(types, "Date", sqlmodel.NewTemporalType("", sqlmodel.TemporalKindDate, false))
	registerPrimitive(types, "Date32", sqlmodel.NewTemporalType("", sqlmodel.TemporalKindDate, false))
	registerPrimitive(types, "DateTime", sqlmodel.NewTemporalType("", sqlmodel.TemporalKindTimestamp, true))

	for _, unit := range intervalUnits {
		registerPrimitive(types, intervalTypePrefix+unit, sqlmodel.NewTemporalType("", sqlmodel.TemporalKindInterval, false))
	}
}

// registerPrimitiveSpecialisedTypes registers UUID, network address types (IPv4 / IPv6),
// JSON, the catch-all Dynamic / Nothing types, and the geometric shapes (Point / Ring /
// Polygon / MultiPolygon). These are domain-specific categories whose Go-side
// representation is picked by the codegen target.
//
// Takes types (map[string]sqlmodel.SQLType) which receives the types.
func registerPrimitiveSpecialisedTypes(types map[string]sqlmodel.SQLType) {
	registerPrimitive(types, "UUID", sqlmodel.NewSQLType(sqlmodel.TypeCategoryUUID, ""))

	registerPrimitive(types, "IPv4", addressType(widthWord))
	registerPrimitive(types, "IPv6", addressType(widthWide))

	registerPrimitive(types, "JSON", sqlmodel.NewSQLType(sqlmodel.TypeCategoryJSON, ""))

	registerPrimitive(types, "Dynamic", sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""))
	registerPrimitive(types, "Nothing", sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, ""))

	registerPrimitive(types, "Point", sqlmodel.NewSQLType(sqlmodel.TypeCategoryGeometric, ""))
	registerPrimitive(types, "Ring", sqlmodel.NewSQLType(sqlmodel.TypeCategoryGeometric, ""))
	registerPrimitive(types, "Polygon", sqlmodel.NewSQLType(sqlmodel.TypeCategoryGeometric, ""))
	registerPrimitive(types, "MultiPolygon", sqlmodel.NewSQLType(sqlmodel.TypeCategoryGeometric, ""))
	registerPrimitive(types, "LineString", sqlmodel.NewSQLType(sqlmodel.TypeCategoryGeometric, ""))
	registerPrimitive(types, "MultiLineString", sqlmodel.NewSQLType(sqlmodel.TypeCategoryGeometric, ""))
	registerPrimitive(types, "Geometry", sqlmodel.NewSQLType(sqlmodel.TypeCategoryGeometric, ""))

	registerPrimitive(types, "Identifier", sqlmodel.NewSQLType(sqlmodel.TypeCategoryText, ""))
}

// addressType returns the IP address type for a given bit width.
//
// Takes width (int) which is the size in bits the SQL type stores an address in.
//
// Returns sqlmodel.SQLType which is the address type.
func addressType(width int) sqlmodel.SQLType {
	sqlType := sqlmodel.NewNetworkType("", sqlmodel.NetworkKindAddress)
	sqlType.Width = width
	return sqlType
}

// canonicaliseTypeName lowercases the input for the analyser's catalogue lookup. The
// server's case-sensitivity rules vary by type; this lookup accepts any case.
//
// Takes name (string) which is the ClickHouse type-name spelling.
//
// Returns string which is the lowercased catalogue key.
func canonicaliseTypeName(name string) string {
	return strings.ToLower(name)
}

// primitiveType returns the built-in primitive type of a name, carrying its structural
// attributes, or an unknown type of that name when it is not a primitive.
//
// Takes name (string) which is the ClickHouse type name in any case.
//
// Returns sqlmodel.SQLType which is the primitive type.
func primitiveType(name string) sqlmodel.SQLType {
	if sqlType, found := clickhousePrimitiveTypes[canonicaliseTypeName(name)]; found {
		return sqlType
	}
	return sqlmodel.NewSQLType(sqlmodel.TypeCategoryUnknown, name)
}

// buildTypeCatalogue assembles the ClickHouse built-in type catalogue.
//
// Extras supplied via WithExtraTypes are merged after the built-ins so the caller can
// override the engine's defaults for niche dialects (ClickHouse Cloud or Altinity).
//
// Takes extras (map[string]sqlmodel.SQLType) which holds type overrides to layer in.
//
// Returns *ast.TypeCatalogue which holds the assembled catalogue.
func buildTypeCatalogue(extras map[string]sqlmodel.SQLType) *ast.TypeCatalogue {
	types := make(map[string]sqlmodel.SQLType, len(clickhousePrimitiveTypes)+len(extras))
	maps.Copy(types, clickhousePrimitiveTypes)
	for name := range extras {
		types[strings.ToLower(name)] = extras[name]
	}
	return &ast.TypeCatalogue{Types: types}
}

// parseClickHouseType parses a ClickHouse type-name string into a typeParseResult. The
// parser is recursive-descent over the input characters; ClickHouse type names form a
// context-free grammar over the alphabet of identifiers, parens, commas, single-quoted
// literals, integers, and equals signs.
//
// Takes input (string) which is the ClickHouse type-name to parse.
// Takes maxDepth (int) which caps wrapper nesting; deeper input fails with an error
// wrapping dialectkit.ErrParseDepth.
//
// Returns typeParseResult which holds the parsed structure with wrapper flags.
// Returns error when the type-name is malformed (unterminated parens, missing comma,
// unknown wrapper) or nests deeper than maxDepth.
func parseClickHouseType(input string, maxDepth int) (typeParseResult, error) {
	return parseClickHouseTypeWith(input, newTypeResolver(nil, nil, maxDepth))
}

// parseClickHouseTypeWith parses a ClickHouse type-name string, looking each bare type
// name up through the resolver's hooks and extra types before the built-in primitives.
//
// Takes input (string) which is the ClickHouse type-name to parse.
// Takes resolver (*typeResolver) which supplies the hooks, the extra types and the
// nesting cap.
//
// Returns typeParseResult which holds the parsed structure with wrapper flags.
// Returns error when the type-name is malformed or nests deeper than the cap.
func parseClickHouseTypeWith(input string, resolver *typeResolver) (typeParseResult, error) {
	parser := &typeNameParser{input: input, position: 0, depth: 0, maxDepth: resolver.maxDepth, resolver: resolver}
	parser.skipWhitespace()
	result, err := parser.parseType()
	if err != nil {
		return typeParseResult{}, err
	}
	parser.skipWhitespace()
	if parser.position < len(parser.input) {
		return typeParseResult{}, &typeNameParseError{
			input:    input,
			position: parser.position,
			message:  "trailing characters after type",
		}
	}
	return result, nil
}

// synthesiseAnonymousFieldName returns the conventional name for anonymous tuple fields,
// such as _1, _2, and _3.
//
// Takes index (int) which is the 1-based field position.
//
// Returns string which is the synthesised field name.
func synthesiseAnonymousFieldName(index int) string {
	return "_" + strconv.Itoa(index)
}

// quoteTypeNameForError quotes a type name for an error message, truncating it on a rune
// boundary after maxTypeNameErrorRunes runes.
//
// Takes input (string) which is the type name to quote.
//
// Returns string which is the quoted name, followed by a truncation note when the name
// was cut.
func quoteTypeNameForError(input string) string {
	runeCount := 0
	for byteIndex := range input {
		if runeCount == maxTypeNameErrorRunes {
			return strconv.Quote(input[:byteIndex]) + "... (truncated, " + strconv.Itoa(utf8.RuneCountInString(input)) + " runes in total)"
		}
		runeCount++
	}
	return strconv.Quote(input)
}

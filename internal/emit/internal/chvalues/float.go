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

package chvalues

import (
	"go/ast"
	"strconv"
	"strings"

	"piko.sh/goastutil"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/gogen/helpers"
	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// floatWidth16 identifies BFloat16, which has a different native encoding.
	floatWidth16 = 16

	// floatWidth32 identifies IEEE single precision.
	floatWidth32 = 32

	// floatWidth64 identifies IEEE double precision.
	floatWidth64 = 64

	// floatClose closes a generated SQL expression.
	floatClose = ")"

	// floatSeparator separates generated SQL arguments.
	floatSeparator = ", "

	// floatSpace separates a lambda's arrow from its parameter and body.
	floatSpace = " "

	// floatWhitespace lists the bytes skipped between the words of a native type.
	floatWhitespace = " \t\r\n"

	// floatPrecisionText is the second word of DOUBLE PRECISION.
	floatPrecisionText = "precision"

	// floatReinterpretPrefix starts the ClickHouse function that reads a float from its
	// bits. The width follows it.
	floatReinterpretPrefix = "reinterpretAsFloat"

	// floatShapeDouble marks a Float64 leaf in a float value's shape.
	floatShapeDouble = "f"

	// floatShapeSingle marks a Float32 leaf in a float value's shape.
	floatShapeSingle = "F"

	// floatOuterDepth numbers the outermost lambda local of a rebuilt value.
	floatOuterDepth = 0

	// suffixFloatValue names the helper that binds a float value as its bits.
	suffixFloatValue = "ClickHouseFloatValue"
)

// FloatTransport carries the dialect syntax and local names that rebuild exact
// floating-point parameters.
type FloatTransport struct {
	// Syntax names the native bit types and lambda arrow.
	Syntax dialect.FloatBitsSyntax

	// Local prefixes the lambda parameters the rebuilt expressions declare.
	Local string
}

// NewFloatTransport reads the transport from the dialect facts and the helper prefix.
//
// Takes facts (*dialect.Facts) which describes the dialect.
// Takes helperPrefix (string) which keeps the local names in the generator's vocabulary.
//
// Returns FloatTransport which is disabled when the dialect declares no bit types.
func NewFloatTransport(facts *dialect.Facts, helperPrefix string) FloatTransport {
	return FloatTransport{Syntax: facts.FloatBits, Local: helperPrefix + "_float"}
}

// Enabled reports whether the dialect declares the native bit types.
//
// Returns bool which is false for a dialect that needs no exact transport.
func (transport FloatTransport) Enabled() bool {
	return transport.Syntax.Single != "" && transport.Syntax.Double != "" && transport.Syntax.Arrow != ""
}

// holdsFloatValue reports whether a SQL type contains floating-point values.
//
// Takes sqlType (*sqlmodel.SQLType) which describes a scalar or composite value.
//
// Returns bool which selects exact positional binding with a native cast.
func holdsFloatValue(sqlType *sqlmodel.SQLType) bool {
	return holdsFloat(sqlType, 0)
}

// holdsFloat bounds traversal through nested SQL types.
//
// Takes sqlType (*sqlmodel.SQLType) which describes the current type.
// Takes depth (int) which bounds nesting and cyclic input.
//
// Returns bool which reports an encountered floating-point type.
func holdsFloat(sqlType *sqlmodel.SQLType, depth int) bool {
	if sqlType == nil || depth >= sqlmodel.MaxTypeDepth {
		return false
	}
	if isExactFloat(sqlType) || holdsFloat(sqlType.ElementType, depth+1) || holdsFloat(sqlType.KeyType, depth+1) {
		return true
	}
	for index := range sqlType.StructFields {
		field := &sqlType.StructFields[index]
		if holdsFloat(&field.SQLType, depth+1) {
			return true
		}
	}
	return false
}

// isExactFloat reports whether a SQL type is a float the transport sends as its bits.
//
// Takes sqlType (*sqlmodel.SQLType) which is the type, not nil.
//
// Returns bool which is false for BFloat16, whose native encoding differs.
func isExactFloat(sqlType *sqlmodel.SQLType) bool {
	return sqlType.Category == sqlmodel.TypeCategoryFloat && sqlType.Width != floatWidth16
}

// bindPositionalFloat encodes a floating-point parameter bound by position as exact
// integer bits.
//
// Takes vocabulary (helpers.Vocabulary) which names the generated helper.
// Takes access (ast.Expr) which reads the parameter value.
// Takes parameter (*sqlmodel.QueryParameter) which describes its native shape.
//
// Returns ast.Expr which binds a lossless wire representation.
func bindPositionalFloat(vocabulary helpers.Vocabulary, access ast.Expr, parameter *sqlmodel.QueryParameter) ast.Expr {
	return bindFloat(vocabulary, access, parameter, false)
}

// bindNamedFloat encodes a floating-point parameter bound by name as exact integer bits,
// in the named String parameter format.
//
// Takes vocabulary (helpers.Vocabulary) which names the generated helper.
// Takes access (ast.Expr) which reads the parameter value.
// Takes parameter (*sqlmodel.QueryParameter) which describes its native shape.
//
// Returns ast.Expr which binds a lossless wire representation.
func bindNamedFloat(vocabulary helpers.Vocabulary, access ast.Expr, parameter *sqlmodel.QueryParameter) ast.Expr {
	return bindFloat(vocabulary, access, parameter, true)
}

// bindFloat encodes a floating-point parameter as exact integer bits.
//
// Takes vocabulary (helpers.Vocabulary) which names the generated helper.
// Takes access (ast.Expr) which reads the parameter value.
// Takes parameter (*sqlmodel.QueryParameter) which describes its native shape.
// Takes named (bool) which becomes the helper's argument selecting the named format.
//
// Returns ast.Expr which binds a lossless wire representation.
func bindFloat(vocabulary helpers.Vocabulary, access ast.Expr, parameter *sqlmodel.QueryParameter, named bool) ast.Expr {
	shape := floatShape(&parameter.SQLType)
	if parameter.IsSlice {
		shape = "[" + shape + "]"
	}
	return goastutil.CallExpr(goastutil.CachedIdent(vocabulary.Helper(suffixFloatValue)), access,
		goastutil.StrLit(shape), goastutil.CachedIdent(strconv.FormatBool(named)))
}

// floatCast reconstructs native floating-point values from integer bits.
//
// Takes parameter (*sqlmodel.QueryParameter) which supplies resolved type structure.
// Takes typeText (string) which retains native type details and tuple names.
// Takes transport (FloatTransport) which names the native bit types and lambda locals.
//
// Returns string which declares a named wire parameter as text.
// Returns dialect.Affix which reconstructs the native scalar or composite value.
func floatCast(parameter *sqlmodel.QueryParameter, typeText string, transport FloatTransport) (string, dialect.Affix) {
	if typeText == "" || !transport.Enabled() || !holdsFloatValue(&parameter.SQLType) {
		return typeText, dialect.Affix{Before: "", After: ""}
	}
	sqlType := parameter.SQLType
	if parameter.IsSlice {
		element := sqlType
		sqlType = sqlmodel.NewSQLType(sqlmodel.TypeCategoryArray, "Array")
		sqlType.ElementType = &element
	}
	local := transport.Local + strconv.Itoa(floatOuterDepth)
	before := "CAST(arrayMap(" + local + floatSpace + transport.Syntax.Arrow + floatSpace + floatExpression(&sqlType, local, floatOuterDepth+1, transport) + ", CAST(concat('[', "
	after := ", ']') AS Array(" + floatTransportType(typeText, transport) + ")))[1] AS " + typeText + floatClose
	if parameter.IsSlice {
		before = "(SELECT arrayJoin(" + before
		after += "))"
	}
	return "String", dialect.Affix{Before: before, After: after}
}

// floatShape describes exact floating-point leaves in a parameter.
//
// Takes sqlType (*sqlmodel.SQLType) which describes the resolved native type.
//
// Returns string which uses f and F for Float64 and Float32 bit patterns.
func floatShape(sqlType *sqlmodel.SQLType) string {
	if !holdsFloatValue(sqlType) {
		return elementShape(sqlType)
	}
	switch sqlType.Category {
	case sqlmodel.TypeCategoryFloat:
		if sqlType.Width == floatWidth32 {
			return floatShapeSingle
		}
		return floatShapeDouble
	case sqlmodel.TypeCategoryArray:
		return "[" + floatShape(sqlType.ElementType) + "]"
	case sqlmodel.TypeCategoryMap:
		return "{" + floatShape(sqlType.KeyType) + ":" + floatShape(sqlType.ElementType) + "}"
	case sqlmodel.TypeCategoryStruct:
		parts := make([]string, len(sqlType.StructFields))
		for index := range sqlType.StructFields {
			field := &sqlType.StructFields[index]
			parts[index] = floatShape(&field.SQLType)
			if field.Name != "" {
				parts[index] = quotedName(field.Name) + "=" + parts[index]
			}
		}
		return "(" + strings.Join(parts, ",") + floatClose
	default:
		return Shape(sqlType)
	}
}

// floatExpression reconstructs floating-point leaves while retaining other values.
//
// Takes sqlType (*sqlmodel.SQLType) which describes the current native value.
// Takes value (string) which is its SQL expression.
// Takes depth (int) which gives nested lambdas distinct local names.
// Takes transport (FloatTransport) which names the lambda locals and arrow.
//
// Returns string which reconstructs the original scalar or composite value.
func floatExpression(sqlType *sqlmodel.SQLType, value string, depth int, transport FloatTransport) string {
	if !holdsFloatValue(sqlType) {
		return value
	}
	name := transport.Local + strconv.Itoa(depth)
	arrow := floatSpace + transport.Syntax.Arrow + floatSpace
	switch sqlType.Category {
	case sqlmodel.TypeCategoryFloat:
		width := floatWidth64
		if sqlType.Width == floatWidth32 {
			width = floatWidth32
		}
		return floatReinterpretPrefix + strconv.Itoa(width) + "(" + value + floatClose
	case sqlmodel.TypeCategoryArray:
		return "arrayMap(" + name + arrow + floatExpression(sqlType.ElementType, name, depth+1, transport) + floatSeparator + value + floatClose
	case sqlmodel.TypeCategoryMap:
		key := "arrayMap(" + name + arrow + floatExpression(sqlType.KeyType, name, depth+1, transport) + ", mapKeys(" + value + "))"
		element := "arrayMap(" + name + arrow + floatExpression(sqlType.ElementType, name, depth+1, transport) + ", mapValues(" + value + "))"
		return "mapFromArrays(" + key + floatSeparator + element + floatClose
	case sqlmodel.TypeCategoryStruct:
		fields := make([]string, len(sqlType.StructFields))
		for index := range sqlType.StructFields {
			field := &sqlType.StructFields[index]
			fields[index] = floatExpression(&field.SQLType, "tupleElement("+value+", "+strconv.Itoa(index+1)+")", depth+1, transport)
		}
		return "tuple(" + strings.Join(fields, floatSeparator) + floatClose
	default:
		return value
	}
}

// floatTransportType replaces floating-point type tokens with unsigned storage types.
//
// Takes native (string) which is a validated native type declaration.
// Takes transport (FloatTransport) which names the unsigned bit types.
//
// Returns string which preserves quoted names, enum values and unrelated native types.
func floatTransportType(native string, transport FloatTransport) string {
	var result strings.Builder
	for index := 0; index < len(native); {
		end := floatTypeTokenEnd(native, index)
		token := native[index:end]
		rest := strings.TrimLeft(native[end:], floatWhitespace)
		if strings.EqualFold(token, "double") && floatPrecisionWord(rest) {
			end = len(native) - len(rest) + len(floatPrecisionText)
			token = transport.Syntax.Double
			rest = strings.TrimLeft(native[end:], floatWhitespace)
		}
		if rest == "" || !floatTypeLetter(rest[0]) {
			switch strings.ToLower(token) {
			case "float32", "float", "real", "single":
				token = transport.Syntax.Single
			case "float64", "double":
				token = transport.Syntax.Double
			}
		}
		result.WriteString(token)
		index = end
	}
	return result.String()
}

// floatTypeLetter identifies a byte that starts a bare native type token.
//
// Takes value (byte) which is the current source byte.
//
// Returns bool which accepts ASCII type names and tuple field names.
func floatTypeLetter(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z' || value == '_'
}

// floatTypeTokenEnd advances past a quoted value, identifier or punctuation byte.
//
// Takes native (string) which is the original validated type declaration.
// Takes start (int) which indexes the next token.
//
// Returns int which is the first byte after the token.
func floatTypeTokenEnd(native string, start int) int {
	quote := native[start]
	if quote == '\'' || quote == '"' || quote == '`' {
		return floatQuotedEnd(native, start)
	}
	end := start + 1
	if !floatTypeLetter(quote) {
		return end
	}
	for end < len(native) && (floatTypeLetter(native[end]) || native[end] >= '0' && native[end] <= '9') {
		end++
	}
	return end
}

// floatQuotedEnd retains quoted names and enum values without inspecting their contents.
//
// Takes native (string) which is the original type declaration.
// Takes start (int) which indexes the opening quote.
//
// Returns int which is the first byte after the quoted token.
func floatQuotedEnd(native string, start int) int {
	quote := native[start]
	for index := start + 1; index < len(native); index++ {
		if native[index] == '\\' {
			index++
			continue
		}
		if native[index] != quote {
			continue
		}
		if index+1 < len(native) && native[index+1] == quote {
			index++
			continue
		}
		return index + 1
	}
	return len(native)
}

// floatPrecisionWord identifies the second word of DOUBLE PRECISION.
//
// Takes text (string) which begins after the first word and intervening whitespace.
//
// Returns bool which requires a complete native type word.
func floatPrecisionWord(text string) bool {
	word := floatPrecisionText
	return len(text) >= len(word) && strings.EqualFold(text[:len(word)], word) &&
		(len(text) == len(word) || !floatTypeLetter(text[len(word)]))
}

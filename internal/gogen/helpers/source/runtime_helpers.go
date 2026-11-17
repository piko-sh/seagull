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

package source

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// sglBindPrefix is the dialect's text before a binding number.
//
//sgl:inline prefix
const sglBindPrefix = "#"

// sglBindSuffix is the dialect's text after a binding number.
//
//sgl:inline suffix
const sglBindSuffix = ""

// sglBindNumbered enables numbered placeholders in runtime SQL.
//
//sgl:inline numbered
const sglBindNumbered = true

// sglBindFloatAsText preserves subnormal floats with a native cast from text.
//
//sgl:inline castFloats
const sglBindFloatAsText = true

// sglFilterFloatDepth bounds pointer traversal in runtime filter values.
const sglFilterFloatDepth = 32

// sglFilterFloat32Bits is the native width of a Go float32.
const sglFilterFloat32Bits = 32

// sglDecimalBase is the base placeholder numbers and float bits are written in.
const sglDecimalBase = 10

// sglIntDigits is the most bytes an int is written in, the length of the smallest int64.
const sglIntDigits = len("-9223372036854775808")

// sglOffsetFirst writes the offset clause before the limit clause.
//
//sgl:inline offsetFirst
const sglOffsetFirst = false

// sglNullsFirstAscending reports whether NULL sorts first in an ascending key with no
// placement.
//
//sgl:inline nullsFirstAscending
const sglNullsFirstAscending = false

// sglNullsFirstDescending reports whether NULL sorts first in a descending key with no
// placement.
//
//sgl:inline nullsFirstDescending
const sglNullsFirstDescending = true

// sglNullsByIsNull reports an engine without NULLS FIRST and NULLS LAST, whose placement
// other than its own is written as a key on whether the column is NULL.
//
//sgl:inline nullsByIsNull
const sglNullsByIsNull = false

// sglDistinctNullsDefaultOnly reports an engine that refuses a placement other than its
// own in a SELECT DISTINCT.
//
//sgl:inline distinctNullsDefaultOnly
const sglDistinctNullsDefaultOnly = false

const (
	// sglOrderAscending is the SQL ascending direction.
	sglOrderAscending = "ASC"

	// sglOrderDescending is the SQL descending direction.
	sglOrderDescending = "DESC"

	// sglOrderGap separates an ordering key from its direction.
	sglOrderGap = " "

	// sglOrderSeparator separates ordering keys.
	sglOrderSeparator = ", "
)

// sglMaxColumnExpressionLength bounds the column-expression string the validator regex
// evaluates. RE2 is linear-time but the bare-identifier alternation tolerates arbitrary
// repetition of JSON arrow segments, so a developer-friendly cap prevents pathological
// inputs from costing CPU before the allow-list check rejects them.
const sglMaxColumnExpressionLength = 256

// sglEmptyMembership replaces IN and its list when a caller filters on an empty list: a
// test no row passes.
//
//sgl:data emptyMembership
const sglEmptyMembership = "IN (SELECT NULL WHERE 1 = 0)"

// sglEmptyExclusion replaces NOT IN and its list when a caller filters on an empty list:
// a test every row passes.
//
//sgl:data emptyExclusion
const sglEmptyExclusion = "NOT IN (SELECT NULL WHERE 1 = 0)"

// sglLimitBefore and sglLimitAfter are written around the row limit.
//
//sgl:data limitBefore
const sglLimitBefore = "FETCH NEXT "

// sglLimitAfter is the dialect's text after a limit placeholder.
//
//sgl:data limitAfter
const sglLimitAfter = " ROWS ONLY"

// sglOffsetBefore and sglOffsetAfter are written around the rows skipped.
//
//sgl:data offsetBefore
const sglOffsetBefore = "OFFSET "

// sglOffsetAfter is the dialect's text after an offset placeholder.
//
//sgl:data offsetAfter
const sglOffsetAfter = " ROWS"

// sglUnboundedLimit is the limit written before an offset that comes without one, for a
// dialect that skips rows only after a limit, or empty when an offset may stand alone.
//
//sgl:data unboundedLimit
const sglUnboundedLimit = ""

// sglPredicateShape says what a condition writes after its operator.
type sglPredicateShape uint8

const (
	// sglPredicateBare writes nothing after the operator. It is a unary operator, or the
	// test written for an empty list.
	sglPredicateBare sglPredicateShape = iota

	// sglPredicateScalar writes one placeholder for its value.
	sglPredicateScalar

	// sglPredicateList writes a placeholder for each of its values, in parentheses.
	sglPredicateList
)

// ErrSglUnknownColumn is returned by a runtime builder's All, One or Count terminal when
// Where or OrderBy was given a column the query does not select. Callers can match it
// with errors.Is.
var ErrSglUnknownColumn = errors.New("sgl: unknown column in runtime query filter")

// ErrSglUnknownOperator is returned by a runtime builder's All, One or Count terminal
// when Where was given an operator outside the allow-list. Callers can match it with
// errors.Is.
var ErrSglUnknownOperator = errors.New("sgl: unknown operator in runtime query filter")

// ErrSglUnknownDirection is returned by a runtime builder's All, One or Count terminal
// when OrderBy was given a sort direction outside the allow-list. Callers can match it
// with errors.Is.
var ErrSglUnknownDirection = errors.New("sgl: unknown sort direction in runtime query order")

// sglJSONPathOpenings lists prefixes that identify supported JSON path expressions.
//
//sgl:inline jsonPathOpenings
var sglJSONPathOpenings = []string{"path("}

// sglRootOperators lists operators that follow a JSON root column.
//
//sgl:inline rootOperators
var sglRootOperators = []string{"~>"}

// sglAllowedOperators records the dialect's permitted runtime comparison operators.
//
//sgl:data operators
var sglAllowedOperators = map[string]bool{
	"=":           true,
	"IN":          true,
	"NOT IN":      true,
	"IS NULL":     true,
	"IS NOT NULL": true,
}

// sglAllowedDirections is the exact-match allow-list consulted by every runtime-builder
// OrderBy() call after the supplied direction has been trimmed and uppercased. Storing
// only the upper-cased forms keeps the allow-list small while letting callers pass
// mixed-case shorthand like "Asc" or "desc nulls last" without surfacing a runtime panic.
var sglAllowedDirections = map[string]bool{
	"ASC":              true,
	"DESC":             true,
	"ASC NULLS FIRST":  true,
	"ASC NULLS LAST":   true,
	"DESC NULLS FIRST": true,
	"DESC NULLS LAST":  true,
}

// sglColumnExpressionRegex validates the dialect's permitted column expression grammar.
//
//sgl:data columnExpression
var sglColumnExpressionRegex = regexp.MustCompile(`^(?i:[\pL_][\pL\pN_]*(?:~>(?:'[^'\\]*'|[0-9]+))*|path\([\pL_][\pL\pN_]*,\s*'[^'\\]*'\)|convert\(\s*[\pL_][\pL\pN_]*\s+to\s+(?:whole|text)\s*\))$`)

// sglCastPatterns match each form in which a caller may convert a filtered or sorted
// column, capturing the column and the type it is converted to.
//
//sgl:data castPatterns
var sglCastPatterns = []*regexp.Regexp{regexp.MustCompile(`^(?i:convert\(\s*(.*?)\s+to\s+(whole|text)\s*\))$`)}

// sglCastBefore, sglCastMiddle and sglCastAfter write each cast form, in the order of
// sglCastPatterns, around the column and its type.
//
//sgl:data castBefore
var sglCastBefore = []string{"CONVERT("}

// sglCastMiddle is the dialect's separator between a column and its cast type.
//
//sgl:data castMiddle
var sglCastMiddle = []string{" TO "}

// sglCastAfter closes a dialect cast expression.
//
//sgl:data castAfter
var sglCastAfter = []string{")"}

// sglCastTypes lists the types a column may be converted to, as the dialect spells them;
// a caller's type is matched whatever its case and written so.
//
//sgl:data castTypes
var sglCastTypes = []string{"WHOLE", "TEXT"}

// sglPredicate holds one condition added by a runtime builder's Where.
//
// A scalar value stays outside a slice so adding a scalar condition does not allocate.
type sglPredicate struct {
	// value is the argument a scalar condition binds.
	value any

	// column is the validated column expression.
	column string

	// operator is the operator as the caller wrote it, or the dialect's test for an empty
	// list.
	operator string

	// values holds the arguments a list condition binds.
	values []any

	// shape says what the condition writes after its operator.
	shape sglPredicateShape
}

// bound returns how many arguments a condition binds.
//
// Returns int which is one for a scalar, the list's length for a list, and zero
// otherwise.
func (predicate *sglPredicate) bound() int {
	switch predicate.shape {
	case sglPredicateScalar:
		return 1
	case sglPredicateList:
		return len(predicate.values)
	default:
		return 0
	}
}

// sglNormaliseDirection trims surrounding whitespace and folds the caller-supplied
// direction string to upper case before the allow-list lookup. Centralised so the
// validator and the eventual emit path agree on the canonical form.
//
// Takes direction (string) which is the requested ordering direction.
//
// Returns string which is the direction normalised to an SQL ordering keyword.
func sglNormaliseDirection(direction string) string {
	return strings.ToUpper(strings.TrimSpace(direction))
}

// sglResolveColumn checks a caller's column expression against the grammar the dialect
// allows and the columns the query selects, and returns the SQL to write for it: the
// expression with its column replaced by the column's qualified source, and any
// conversion written in the dialect's own spelling.
//
// A selected column's exact name resolves to that column before the grammar applies, so a
// column whose name holds a dot or another character the grammar reads is still found. An
// expression holding a backslash or a NUL is refused whatever the grammar allows.
//
// Takes column (string) which is the requested column expression.
// Takes allowed (map[string]string) which maps permitted column names to their SQL
// expressions.
//
// Returns string which is the validated SQL expression when recognised.
// Returns bool which reports whether the expression names a permitted column.
func sglResolveColumn(column string, allowed map[string]string) (string, bool) {
	const unsafeText = "\\\x00"
	trimmed := strings.TrimSpace(column)
	if len(trimmed) > sglMaxColumnExpressionLength || strings.ContainsAny(trimmed, unsafeText) {
		return "", false
	}
	if resolved, ok := allowed[trimmed]; ok || sglSimpleColumn(trimmed) {
		return resolved, ok
	}
	if !sglColumnExpressionRegex.MatchString(trimmed) {
		return "", false
	}
	inner, castType, form := sglSplitCast(trimmed)
	root, start := sglExtractColumnRoot(inner)
	resolved, ok := allowed[root]
	if !ok {
		return "", false
	}
	expression := inner[:start] + resolved + inner[start+len(root):]
	if form < 0 {
		return expression, true
	}
	return sglCastBefore[form] + expression + sglCastMiddle[form] + castType + sglCastAfter[form], true
}

// sglSimpleColumn recognises precisely the bare identifier branch of the grammar.
//
// Takes name (string) which is the candidate unquoted identifier.
//
// Returns bool which reports whether name uses the supported identifier characters.
func sglSimpleColumn(name string) bool {
	if name == "" {
		return false
	}
	for index, character := range name {
		if character != '_' && !unicode.IsLetter(character) && (index <= 0 || !unicode.IsDigit(character)) {
			return false
		}
	}
	return true
}

// sglSplitCast returns the column a cast converts, the type it converts it to as
// sglCastTypes spells it, and the cast form's index, or the expression itself and -1 when
// it is no cast.
//
// Takes expression (string) which is the column expression with an optional cast.
//
// Returns column (string) which is the column expression without the cast.
// Returns castType (string) which is the cast target, or empty when absent.
// Returns form (int) which identifies the recognised cast syntax, or -1 when absent.
func sglSplitCast(expression string) (column, castType string, form int) {
	for index, pattern := range sglCastPatterns {
		match := pattern.FindStringSubmatch(expression)
		if match == nil {
			continue
		}
		written := strings.Fields(match[2])
		for _, spelled := range sglCastTypes {
			if slices.EqualFunc(strings.Fields(spelled), written, strings.EqualFold) {
				return strings.TrimSpace(match[1]), spelled, index
			}
		}
	}
	return expression, "", -1
}

// sglExtractColumnRoot returns the column a column expression reads, and where it starts
// in the expression: the first argument of a JSON path function, the text before the
// first JSON path operator, or the expression itself, without the white space around it.
//
// Takes column (string) which is the requested column expression.
//
// Returns root (string) which is the column name before any cast or JSON path.
// Returns start (int) which is the byte offset of root in column.
func sglExtractColumnRoot(column string) (root string, start int) {
	start = len(column) - len(strings.TrimLeftFunc(column, unicode.IsSpace))
	trimmed := strings.TrimSpace(column)
	for _, opening := range sglJSONPathOpenings {
		if len(trimmed) > len(opening) && strings.EqualFold(trimmed[:len(opening)], opening) {
			if argument, _, found := strings.Cut(trimmed[len(opening):], ","); found && argument != "" {
				leading := len(argument) - len(strings.TrimLeftFunc(argument, unicode.IsSpace))
				return strings.TrimSpace(argument), start + len(opening) + leading
			}
		}
	}
	for _, operator := range sglRootOperators {
		if arrowIndex := strings.Index(trimmed, operator); arrowIndex > 0 {
			return strings.TrimSpace(trimmed[:arrowIndex]), start
		}
	}
	return trimmed, start
}

// sglIsUnaryOperator reports whether op is one of the operators that take no right-hand
// value (IS NULL / IS NOT NULL).
//
// Takes op (string) which is the normalised SQL operator.
//
// Returns bool which reports whether the operator binds no value.
func sglIsUnaryOperator(op string) bool {
	switch strings.ToUpper(strings.TrimSpace(op)) {
	case "IS NULL", "IS NOT NULL":
		return true
	}
	return false
}

// sglIsMultiOperator reports whether op consumes a slice value (IN / NOT IN), whose
// elements each take a placeholder of their own.
//
// Takes op (string) which is the normalised SQL operator.
//
// Returns bool which reports whether the operator binds a list of values.
func sglIsMultiOperator(op string) bool {
	switch strings.ToUpper(strings.TrimSpace(op)) {
	case "IN", "NOT IN":
		return true
	}
	return false
}

// sglReflectSlice expands list values into a uniform argument slice.
//
// It flattens any slice / array into []any so the multi-value expander can drive a
// uniform append loop. A non-slice value is wrapped in a one-element slice so callers can
// still write .Where(col, "IN", "single"). A []byte is treated as a single BLOB value
// (one placeholder), not fanned out per byte. A nil value yields nil, an empty list.
//
// Takes value (any) which is the caller-supplied value to adapt.
//
// Returns []any which holds the elements, one scalar value, or no values for nil.
func sglReflectSlice(value any) []any {
	switch values := value.(type) {
	case []string:
		return sglBoxSlice(values)
	case []int:
		return sglBoxSlice(values)
	case []int8:
		return sglBoxSlice(values)
	case []int16:
		return sglBoxSlice(values)
	case []int32:
		return sglBoxSlice(values)
	case []int64:
		return sglBoxSlice(values)
	case []uint:
		return sglBoxSlice(values)
	case []uint16:
		return sglBoxSlice(values)
	case []uint32:
		return sglBoxSlice(values)
	case []uint64:
		return sglBoxSlice(values)
	case []float32:
		return sglBoxSlice(values)
	case []float64:
		return sglBoxSlice(values)
	case []bool:
		return sglBoxSlice(values)
	}
	if value == nil {
		return nil
	}
	if boxed, ok := value.([]any); ok {
		return boxed
	}
	if blob, ok := value.([]byte); ok {
		return []any{blob}
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() != reflect.Slice && reflected.Kind() != reflect.Array {
		return []any{value}
	}
	result := make([]any, reflected.Len())
	for index := range reflected.Len() {
		result[index] = reflected.Index(index).Interface()
	}
	return result
}

// sglBoxSlice boxes ordinary slice elements without reflect.Value allocations.
//
// Takes values ([]T) which holds the typed slice elements.
//
// Returns []any which holds the same elements as interface values.
func sglBoxSlice[T any](values []T) []any {
	result := make([]any, len(values))
	for index, value := range values {
		result[index] = value
	}
	return result
}

// sglMultiValueLen reports how many bind values an IN / NOT IN value expands to without
// materialising or boxing them, mirroring sglReflectSlice's length semantics, so an
// over-limit list is refused before it is flattened.
//
// Takes value (any) which is the caller-supplied value to adapt.
//
// Returns int which is the number of arguments the value would bind as a list.
func sglMultiValueLen(value any) int {
	if value == nil {
		return 0
	}
	if boxed, ok := value.([]any); ok {
		return len(boxed)
	}
	if _, ok := value.([]byte); ok {
		return 1
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() != reflect.Slice && reflected.Kind() != reflect.Array {
		return 1
	}
	return reflected.Len()
}

// sglNewPredicate validates a condition and prepares its bound values.
//
// It checks a caller's condition and returns it as a predicate: the column must be one
// the query selects, written in the grammar the dialect allows, and the operator one the
// allow-list holds. A unary operator binds nothing; IN and NOT IN bind each element of a
// slice value, and an empty list becomes the dialect's empty test; any other operator
// binds the value. The error is ErrSglUnknownColumn, ErrSglUnknownOperator or, for a list
// longer than sglMaxBindVariables, ErrSglTooManyBindVariables wrapped with the column.
//
// Takes column (string) which is the requested column expression.
// Takes operator (string) which is the requested comparison operator.
// Takes value (any) which is the caller-supplied value to adapt.
// Takes allowed (map[string]string) which maps permitted column names to their SQL
// expressions.
//
// Returns sglPredicate which holds the validated condition and its argument values.
// Returns error when the column, operator or list length is not allowed.
func sglNewPredicate(column, operator string, value any, allowed map[string]string) (sglPredicate, error) {
	resolved, ok := sglResolveColumn(column, allowed)
	if !ok {
		return sglPredicate{}, ErrSglUnknownColumn
	}
	if !sglAllowedOperators[operator] {
		return sglPredicate{}, ErrSglUnknownOperator
	}
	switch {
	case sglIsUnaryOperator(operator):
		return sglPredicate{value: nil, column: resolved, operator: operator, values: nil, shape: sglPredicateBare}, nil
	case sglIsMultiOperator(operator):
		count := sglMultiValueLen(value)
		if count > sglMaxBindVariables {
			return sglPredicate{}, fmt.Errorf("sgl: column %q IN/NOT IN list of %d exceeds the limit of %d: %w", column, count, sglMaxBindVariables, ErrSglTooManyBindVariables)
		}
		if count == 0 {
			empty := sglEmptyMembership
			if strings.EqualFold(strings.TrimSpace(operator), "NOT IN") {
				empty = sglEmptyExclusion
			}
			return sglPredicate{value: nil, column: resolved, operator: empty, values: nil, shape: sglPredicateBare}, nil
		}
		return sglPredicate{value: nil, column: resolved, operator: operator, values: sglReflectSlice(value), shape: sglPredicateList}, nil
	default:
		return sglPredicate{value: value, column: resolved, operator: operator, values: nil, shape: sglPredicateScalar}, nil
	}
}

// sglNewOrder validates an ordering and returns its SQL expression.
//
// The column must be selected by the query and the direction must be allowed, regardless
// of case. A failed check returns ErrSglUnknownColumn or ErrSglUnknownDirection.
//
// A NULL placement the engine gives anyway is left out, so it runs everywhere, a SELECT
// DISTINCT included. Another is written with NULLS FIRST or NULLS LAST. An engine without
// them gets a key on whether the column is NULL. An engine that refuses such a placement
// in a SELECT DISTINCT gets ErrSglUnknownDirection there.
//
// Takes column (string) which is the requested column expression.
// Takes direction (string) which is the requested ordering direction.
// Takes allowed (map[string]string) which maps permitted column names to their SQL
// expressions.
// Takes distinct (bool) which is true for a query that selects DISTINCT rows.
//
// Returns string which is the validated ordering expression.
// Returns error when the column or direction is not allowed.
func sglNewOrder(column, direction string, allowed map[string]string, distinct bool) (string, error) {
	resolved, ok := sglResolveColumn(column, allowed)
	if !ok {
		return "", ErrSglUnknownColumn
	}
	normalised := sglNormaliseDirection(direction)
	if !sglAllowedDirections[normalised] {
		return "", ErrSglUnknownDirection
	}
	order, placement, placed := strings.Cut(normalised, " NULLS ")
	if !placed {
		return resolved + sglOrderGap + order, nil
	}
	nullsFirst := placement == "FIRST"
	if nullsFirst == sglNullsFirstByDefault(order) {
		return resolved + sglOrderGap + order, nil
	}
	switch {
	case distinct && sglDistinctNullsDefaultOnly:
		return "", fmt.Errorf("%w: the engine cannot place NULLs %s in a SELECT DISTINCT, as the placement is not in its select list",
			ErrSglUnknownDirection, strings.ToLower(placement))
	case sglNullsByIsNull:
		nullOrder := sglOrderAscending
		if nullsFirst {
			nullOrder = sglOrderDescending
		}
		return "(" + resolved + " IS NULL) " + nullOrder + sglOrderSeparator + resolved + sglOrderGap + order, nil
	default:
		return resolved + sglOrderGap + normalised, nil
	}
}

// sglNullsFirstByDefault reports whether the engine sorts NULL first in a key with a
// direction and no placement.
//
// Takes order (string) which is ASC or DESC.
//
// Returns bool which is true when NULL comes first.
func sglNullsFirstByDefault(order string) bool {
	if order == sglOrderAscending {
		return sglNullsFirstAscending
	}
	return order == sglOrderDescending && sglNullsFirstDescending
}

// sglWriteBindPlaceholder appends a placeholder without allocating its digits.
//
// Takes builder (*strings.Builder) which receives the placeholder text.
// Takes number (int) which is the placeholder's one-based binding number.
func sglWriteBindPlaceholder(builder *strings.Builder, number int) {
	builder.WriteString(sglBindPrefix)
	if sglBindNumbered {
		var digits [sglIntDigits]byte
		builder.Write(strconv.AppendInt(digits[:0], int64(number), sglDecimalBase))
	}
	builder.WriteString(sglBindSuffix)
}

// sglFilterCount counts the arguments a filter will append.
//
// Takes predicates ([]sglPredicate) which holds validated conditions in binding order.
//
// Returns int which is the total number of filter arguments.
func sglFilterCount(predicates []sglPredicate) int {
	count := 0
	for index := range predicates {
		count += predicates[index].bound()
	}
	return count
}

// sglFilter writes predicates joined with AND, their placeholders numbered from next, and
// returns the text and the values it binds, in order. It writes nothing for no predicate.
//
// Takes predicates ([]sglPredicate) which holds validated conditions in binding order.
// Takes next (int) which is the first placeholder number to write.
//
// Returns string which is the conditions joined with AND, or empty for no conditions.
// Returns []any which holds the argument buffer with filter values appended.
func sglFilter(predicates []sglPredicate, next int) (string, []any) {
	var values []any
	if count := sglFilterCount(predicates); count > 0 {
		values = make([]any, 0, count)
	}
	return sglFilterInto(predicates, next, values)
}

// sglFilterInto appends filter arguments to the final query's argument buffer.
//
// Takes predicates ([]sglPredicate) which holds validated conditions in binding order.
// Takes next (int) which is the first placeholder number to write.
// Takes values ([]any) which is the final argument buffer to append to.
//
// Returns string which is the conditions joined with AND, or empty for no conditions.
// Returns []any which holds the argument buffer with filter values appended.
func sglFilterInto(predicates []sglPredicate, next int, values []any) (string, []any) {
	var builder strings.Builder
	start := len(values)
	size := 0
	width := len(sglBindPrefix) + len(sglBindSuffix) + 2
	if sglBindNumbered {
		var digits [sglIntDigits]byte
		width += len(strconv.AppendInt(digits[:0], int64(next+sglFilterCount(predicates)-1), sglDecimalBase))
	}
	for index := range predicates {
		predicate := &predicates[index]
		size += len(predicate.column) + len(predicate.operator) + len(" AND   ()") + predicate.bound()*width
	}
	builder.Grow(size)
	for index := range predicates {
		predicate := &predicates[index]
		if index > 0 {
			builder.WriteString(" AND ")
		}
		builder.WriteString(predicate.column)
		builder.WriteByte(' ')
		builder.WriteString(predicate.operator)
		switch predicate.shape {
		case sglPredicateScalar:
			builder.WriteByte(' ')
			values = append(values, sglWriteFilterValue(&builder, next+len(values)-start, predicate.value))
		case sglPredicateList:
			builder.WriteString(" (")
			for position, value := range predicate.values {
				if position > 0 {
					builder.WriteString(", ")
				}
				values = append(values, sglWriteFilterValue(&builder, next+len(values)-start, value))
			}
			builder.WriteByte(')')
		default:
		}
	}
	return builder.String(), values
}

// sglWriteFilterValue writes a placeholder and adapts its floating-point value.
//
// Takes builder (*strings.Builder) which receives the SQL expression.
// Takes next (int) which numbers the placeholder.
// Takes value (any) which holds the filter argument.
//
// Returns any which preserves the value through the native driver.
func sglWriteFilterValue(builder *strings.Builder, next int, value any) any {
	if sglBindFloatAsText {
		current, original := sglFilterFloat(value)
		value = original
		if sglIsFloat(current) {
			bits := current.Type().Bits()
			number := math.Float64bits(current.Float())
			if bits == sglFilterFloat32Bits {
				number = uint64(math.Float32bits(float32(current.Float())))
			}
			width := strconv.Itoa(bits)
			builder.WriteString("reinterpretAsFloat" + width + "(CAST(")
			sglWriteBindPlaceholder(builder, next)
			builder.WriteString(" AS UInt" + width + "))")
			return strconv.FormatUint(number, sglDecimalBase)
		}
	}
	sglWriteBindPlaceholder(builder, next)
	return value
}

// sglWhereOpen returns the edit that opens the parentheses around the condition a query
// writes, at its start, when a filter joins it, so the filter binds every branch of it.
//
// Takes filter (string) which is the additional filter SQL, or empty for no filter.
// Takes start (int) which is the clause start or insertion offset in the original SQL.
// Takes present (bool) which reports whether the original query already has the clause.
//
// Returns []sglEdit which holds the ordered insertions or replacements, or nil when
// unnecessary.
func sglWhereOpen(filter string, start int, present bool) []sglEdit {
	if filter == "" || !present {
		return nil
	}
	return []sglEdit{{Text: "(", Start: start, End: start}}
}

// sglWhereClose returns the edit that adds a filter at the end of the condition a query
// writes, closing its parentheses, or as a WHERE clause at the place one goes when the
// query writes none.
//
// Takes filter (string) which is the additional filter SQL, or empty for no filter.
// Takes end (int) which is the clause end or insertion offset in the original SQL.
// Takes present (bool) which reports whether the original query already has the clause.
//
// Returns []sglEdit which holds the ordered insertions or replacements, or nil when
// unnecessary.
func sglWhereClose(filter string, end int, present bool) []sglEdit {
	if filter == "" {
		return nil
	}
	prefix := " WHERE "
	if present {
		prefix = ") AND "
	}
	return []sglEdit{{Text: prefix, Start: end, End: end}, {Text: filter, Start: end, End: end}}
}

// sglOrder returns the edit that adds orderings: after the keys of the ORDER BY a query
// writes, which ends at end, or as an ORDER BY at the place one goes, start, when the
// query writes none.
//
// Takes orders ([]string) which holds validated ordering expressions.
// Takes start (int) which is the clause start or insertion offset in the original SQL.
// Takes end (int) which is the clause end or insertion offset in the original SQL.
// Takes present (bool) which reports whether the original query already has the clause.
//
// Returns []sglEdit which holds the ordered insertions or replacements, or nil when
// unnecessary.
func sglOrder(orders []string, start, end int, present bool) []sglEdit {
	if len(orders) == 0 {
		return nil
	}
	prefix, at := " ORDER BY ", start
	if present {
		prefix, at = ", ", end
	}
	return []sglEdit{{Text: prefix, Start: at, End: at}, {Text: strings.Join(orders, ", "), Start: at, End: at}}
}

// sglPagination writes the rows a caller limits and skips in the dialect's syntax, their
// placeholders numbered from next, and returns the text and the values it binds, in
// order: nothing for neither, the dialect's unbounded limit before an offset that comes
// without a limit when the dialect needs one.
//
// Takes limit (int) which is the row limit, omitted when nonpositive.
// Takes offset (int) which is the row offset, omitted when nonpositive.
// Takes next (int) which is the first placeholder number to write.
//
// Returns string which is the pagination SQL, or empty when neither limit nor offset
// applies.
// Returns []any which holds the argument buffer with pagination values appended.
func sglPagination(limit, offset, next int) (string, []any) {
	if limit <= 0 && offset <= 0 {
		return "", nil
	}
	return sglPaginationInto(limit, offset, next, make([]any, 0, sglPaginationCount(limit, offset)))
}

// sglPaginationCount counts the arguments a pagination clause will append.
//
// Takes limit (int) which is the row limit, omitted when nonpositive.
// Takes offset (int) which is the row offset, omitted when nonpositive.
//
// Returns int which is the number of positive pagination arguments.
func sglPaginationCount(limit, offset int) int {
	count := 0
	if limit > 0 {
		count++
	}
	if offset > 0 {
		count++
	}
	return count
}

// sglPaginationInto writes pagination and appends to the query's argument buffer.
//
// Takes limit (int) which is the row limit, omitted when nonpositive.
// Takes offset (int) which is the row offset, omitted when nonpositive.
// Takes next (int) which is the first placeholder number to write.
// Takes values ([]any) which is the final argument buffer to append to.
//
// Returns string which is the pagination SQL, or empty when neither limit nor offset
// applies.
// Returns []any which holds the argument buffer with pagination values appended.
func sglPaginationInto(limit, offset, next int, values []any) (string, []any) {
	if limit <= 0 && offset <= 0 {
		return "", values
	}
	var builder strings.Builder
	clause := len(" ") + len(sglBindPrefix) + len(sglBindSuffix) + sglIntDigits
	builder.Grow(len(sglLimitBefore) + len(sglLimitAfter) + len(sglOffsetBefore) + len(sglOffsetAfter) + len(sglUnboundedLimit) + 2*clause)
	start := len(values)
	write := func(before, after string, value int) {
		builder.WriteByte(' ')
		builder.WriteString(before)
		sglWriteBindPlaceholder(&builder, next+len(values)-start)
		builder.WriteString(after)
		values = append(values, value)
	}
	writeLimit := func() {
		switch {
		case limit > 0:
			write(sglLimitBefore, sglLimitAfter, limit)
		case offset > 0 && sglUnboundedLimit != "":
			builder.WriteByte(' ')
			builder.WriteString(sglLimitBefore)
			builder.WriteString(sglUnboundedLimit)
			builder.WriteString(sglLimitAfter)
		}
	}
	writeOffset := func() {
		if offset > 0 {
			write(sglOffsetBefore, sglOffsetAfter, offset)
		}
	}
	if sglOffsetFirst {
		writeOffset()
		writeLimit()
	} else {
		writeLimit()
		writeOffset()
	}
	return builder.String(), values
}

// sglAt returns the edit that inserts text at an offset, or none when the text is empty.
//
// Takes text (string) which is the SQL to insert, or empty for no edit.
// Takes at (int) which is the insertion offset in the original SQL.
//
// Returns []sglEdit which holds the ordered insertions or replacements, or nil when
// unnecessary.
func sglAt(text string, at int) []sglEdit {
	if text == "" {
		return nil
	}
	return []sglEdit{{Text: text, Start: at, End: at}}
}

// sglIsFloat reports whether a filter value is a floating-point number.
//
// Takes value (reflect.Value) which is the value, or an invalid one for NULL.
//
// Returns bool which is true for a float32 or a float64 kind.
func sglIsFloat(value reflect.Value) bool {
	if !value.IsValid() {
		return false
	}
	kind := value.Kind()
	return kind == reflect.Float32 || kind == reflect.Float64
}

// sglFilterFloat follows scalar pointers without changing unrelated argument values.
//
// Takes value (any) which is the supplied runtime filter value.
//
// Returns reflect.Value which exposes a scalar or an invalid value for NULL.
// Returns any which preserves the original argument or normalises a nil pointer.
func sglFilterFloat(value any) (reflect.Value, any) {
	current := reflect.ValueOf(value)
	for depth := 0; current.IsValid() && depth < sglFilterFloatDepth; depth++ {
		pointer := current.Kind() == reflect.Pointer || current.Kind() == reflect.Interface
		if pointer && current.IsNil() {
			return reflect.Value{}, nil
		}
		if _, ok := reflect.TypeAssert[driver.Valuer](current); ok {
			return reflect.Value{}, value
		}
		if !pointer {
			break
		}
		current = current.Elem()
	}
	return current, value
}

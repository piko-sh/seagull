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
	"errors"
	"fmt"
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

// sglOffsetFirst writes the offset clause before the limit clause.
//
//sgl:inline offsetFirst
const sglOffsetFirst = false

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

// sglPredicate is one condition a runtime builder's Where adds: its text up to its value,
// and the values it binds, written after it as one placeholder, as a list of them in
// parentheses, or not at all.
type sglPredicate struct {
	// text is the validated condition prefix or complete unary expression.
	text string

	// values holds the arguments bound by the condition.
	values []any

	// list reports whether the values require a parenthesised list.
	list bool
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
// An expression holding a backslash or a NUL is refused whatever the grammar allows.
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
	if sglSimpleColumn(trimmed) {
		resolved, ok := allowed[trimmed]
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
	const space = " "
	switch {
	case sglIsUnaryOperator(operator):
		return sglPredicate{text: resolved + space + operator, values: nil, list: false}, nil
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
			return sglPredicate{text: resolved + space + empty, values: nil, list: false}, nil
		}
		return sglPredicate{text: resolved + space + operator + space, values: sglReflectSlice(value), list: true}, nil
	default:
		return sglPredicate{text: resolved + space + operator + space, values: []any{value}, list: false}, nil
	}
}

// sglNewOrder checks a caller's ordering and returns the SQL to write for it: the column
// must be one the query selects and the direction one the allow-list holds, whatever its
// case. The error is ErrSglUnknownColumn or ErrSglUnknownDirection.
//
// Takes column (string) which is the requested column expression.
// Takes direction (string) which is the requested ordering direction.
// Takes allowed (map[string]string) which maps permitted column names to their SQL
// expressions.
//
// Returns string which is the validated ordering expression.
// Returns error when the column or direction is not allowed.
func sglNewOrder(column, direction string, allowed map[string]string) (string, error) {
	resolved, ok := sglResolveColumn(column, allowed)
	if !ok {
		return "", ErrSglUnknownColumn
	}
	normalised := sglNormaliseDirection(direction)
	if !sglAllowedDirections[normalised] {
		return "", ErrSglUnknownDirection
	}
	return resolved + " " + normalised, nil
}

// sglWriteBindPlaceholder appends a placeholder without allocating its digits.
//
// Takes builder (*strings.Builder) which receives the placeholder text.
// Takes number (int) which is the placeholder's one-based binding number.
func sglWriteBindPlaceholder(builder *strings.Builder, number int) {
	const decimalBase = 10
	builder.WriteString(sglBindPrefix)
	if sglBindNumbered {
		var digits [20]byte
		builder.Write(strconv.AppendInt(digits[:0], int64(number), decimalBase))
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
	for _, predicate := range predicates {
		count += len(predicate.values)
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
	const decimalBase = 10
	var builder strings.Builder
	start := len(values)
	size := 0
	width := len(sglBindPrefix) + len(sglBindSuffix) + 2
	if sglBindNumbered {
		var digits [20]byte
		width += len(strconv.AppendInt(digits[:0], int64(next+sglFilterCount(predicates)-1), decimalBase))
	}
	for _, predicate := range predicates {
		size += len(predicate.text) + len(" AND ()") + len(predicate.values)*width
	}
	builder.Grow(size)
	for index, predicate := range predicates {
		if index > 0 {
			builder.WriteString(" AND ")
		}
		builder.WriteString(predicate.text)
		if predicate.list {
			builder.WriteString("(")
		}
		for position, value := range predicate.values {
			if position > 0 {
				builder.WriteString(", ")
			}
			sglWriteBindPlaceholder(&builder, next+len(values)-start)
			values = append(values, value)
		}
		if predicate.list {
			builder.WriteString(")")
		}
	}
	return builder.String(), values
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
	builder.Grow(len(sglLimitBefore) + len(sglLimitAfter) + len(sglOffsetBefore) + len(sglOffsetAfter) + len(sglUnboundedLimit) + 2*(len(sglBindPrefix)+len(sglBindSuffix)+21))
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

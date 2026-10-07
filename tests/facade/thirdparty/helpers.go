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

package thirdparty

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/emit"
)

const (
	// spliceSource declares the edit type, the splicer, the bind limit and the list writer
	// every query edited at run time uses.
	spliceSource = `type __edit__ struct {
	Text  string
	Start int
	End   int
}
const __maxBinds__ = __limit__
var __tooManyBinds__ = __errors__.New("__prefix__: too many bind variables")
func __checkBinds__(count int) error {
	if count > __maxBinds__ {
		return __fmt__.Errorf("%w: %d values, the limit is %d", __tooManyBinds__, count, __maxBinds__)
	}
	return nil
}
func __splice__(query string, groups ...[]__edit__) string {
	var builder __strings__.Builder
	cursor := 0
	for _, group := range groups {
		for _, edit := range group {
			builder.WriteString(query[cursor:edit.Start])
			builder.WriteString(edit.Text)
			cursor = edit.End
		}
	}
	builder.WriteString(query[cursor:])
	return builder.String()
}
func __list__(count, first, operator, start, end int, empty string) []__edit__ {
	if count == 0 {
		return []__edit__{{Text: empty, Start: operator, End: end}}
	}
	placeholders := make([]string, count)
	for index := range placeholders {
		placeholders[index] = __placeholder__(first + index)
	}
	return []__edit__{{Text: "(" + __strings__.Join(placeholders, ", ") + ")", Start: start, End: end}}
}
type OrderDirection string
const (
	OrderAsc  OrderDirection = "ASC"
	OrderDesc OrderDirection = "DESC"
)
func __sort__(column string, direction OrderDirection, at int, allowed ...string) []__edit__ {
	if !__slices__.Contains(allowed, column) {
		return nil
	}
	text := " ORDER BY " + column
	if direction == OrderAsc || direction == OrderDesc {
		text += " " + string(direction)
	}
	return []__edit__{{Text: text, Start: at, End: at}}
}
`

	// builderSource declares the runtime builder's errors, filters, orderings and
	// pagination.
	builderSource = `var __unknownColumn__ = __errors__.New("__prefix__: unknown column")
var __unknownOperator__ = __errors__.New("__prefix__: unknown operator")
var __unknownDirection__ = __errors__.New("__prefix__: unknown direction")
type __predicate__ struct {
	Value any
	Text  string
	Unary bool
}
var __operators__ = map[string]bool{__operatorEntries__}
func __newPredicate__(column, operator string, value any, allowed map[string]string) (__predicate__, error) {
	expression, ok := allowed[column]
	if !ok {
		return __predicate__{}, __fmt__.Errorf("%w: %q", __unknownColumn__, column)
	}
	normalised := __strings__.ToUpper(__strings__.TrimSpace(operator))
	unary, ok := __operators__[normalised]
	if !ok {
		return __predicate__{}, __fmt__.Errorf("%w: %q", __unknownOperator__, operator)
	}
	return __predicate__{Value: value, Text: expression + " " + normalised, Unary: unary}, nil
}
func __newOrder__(column, direction string, allowed map[string]string) (string, error) {
	expression, ok := allowed[column]
	if !ok {
		return "", __fmt__.Errorf("%w: %q", __unknownColumn__, column)
	}
	normalised := __strings__.ToUpper(__strings__.TrimSpace(direction))
	if normalised != "ASC" && normalised != "DESC" {
		return "", __fmt__.Errorf("%w: %q", __unknownDirection__, direction)
	}
	return expression + " " + normalised, nil
}
func __filter__(predicates []__predicate__, args []any) (string, []any) {
	parts := make([]string, 0, len(predicates))
	for _, predicate := range predicates {
		if predicate.Unary {
			parts = append(parts, predicate.Text)
			continue
		}
		args = append(args, predicate.Value)
		parts = append(parts, predicate.Text+" "+__placeholder__(len(args)))
	}
	return __strings__.Join(parts, " AND "), args
}
func __where__(filter string, start, end int, present bool) []__edit__ {
	switch {
	case filter == "":
		return nil
	case present:
		return []__edit__{{Text: "(", Start: start, End: start}, {Text: ") AND " + filter, Start: end, End: end}}
	default:
		return []__edit__{{Text: " WHERE " + filter, Start: start, End: start}}
	}
}
func __orders__(orders []string, start, end int, present bool) []__edit__ {
	if len(orders) == 0 {
		return nil
	}
	if present {
		return []__edit__{{Text: ", " + __strings__.Join(orders, ", "), Start: end, End: end}}
	}
	return []__edit__{{Text: " ORDER BY " + __strings__.Join(orders, ", "), Start: start, End: start}}
}
func __pagination__(limit, offset, at int, args []any) ([]__edit__, []any) {
	var parts []string
__paginationBody__	if len(parts) == 0 {
		return nil, args
	}
	return []__edit__{{Text: " " + __strings__.Join(parts, " "), Start: at, End: at}}, args
}
`

	// limitSource writes a limit, or the unbounded limit before an offset that comes alone.
	limitSource = "\tif limit > 0 {\n\t\targs = append(args, limit)\n\t\tparts = append(parts, %s+__placeholder__(len(args))+%s)\n\t}%s\n"

	// unboundedSource writes the unbounded limit before an offset that comes alone.
	unboundedSource = " else if offset > 0 {\n\t\tparts = append(parts, %s)\n\t}"

	// offsetSource writes an offset.
	offsetSource = "\tif offset > 0 {\n\t\targs = append(args, offset)\n\t\tparts = append(parts, %s+__placeholder__(len(args))+%s)\n\t}\n"

	// coreOperators lists the comparisons every dialect accepts, as a filter writes them.
	coreOperators = "=,<>,!=,<,>,<=,>=,LIKE"
)

var (
	// unaryOperators lists the operators that take no value.
	unaryOperators = []string{"IS NULL", "IS NOT NULL"}

	// helperPackages maps each package token of the helper sources to its import path.
	helperPackages = map[string]string{
		"__errors__": errorsPackage, "__fmt__": "fmt", "__slices__": "slices", "__strconv__": strconvPackage, "__strings__": stringsPackage,
	}
)

// helpersFile writes helpers.go, which the queries whose SQL is edited at run time call:
// the splicer, the list writer and the bind limit, the sortable direction, and the
// runtime builder's filters, orderings and pagination. A package with no such query gets
// no file.
//
// Returns emit.GeneratedFile which is the file.
// Returns bool which is false when the package needs no file.
// Returns error when the file does not format.
func (current *generation) helpersFile() (emit.GeneratedFile, bool, error) {
	if !slices.ContainsFunc(current.queries, editedAtRunTime) {
		return emit.GeneratedFile{}, false, nil
	}
	info := &current.request.Dialect
	text := spliceSource + placeholderSource(info) + builderSource
	text = strings.ReplaceAll(text, "__paginationBody__", paginationSource(info))
	text = strings.ReplaceAll(text, "__operatorEntries__", operatorEntries(info))
	names := current.helpers
	text = strings.NewReplacer(
		"__edit__", names.edit, "__splice__", names.splice, "__placeholder__", names.placeholder, "__list__", names.list,
		"__sort__", names.sort, "__checkBinds__", names.checkBinds, "__maxBinds__", names.maxBinds, "__predicate__", names.predicate,
		"__operators__", names.operators, "__newPredicate__", names.newPredicate, "__newOrder__", names.newOrder,
		"__filter__", names.filter, "__where__", names.where, "__orders__", names.orders, "__pagination__", names.pagination,
		"__unknownColumn__", names.unknownColumn, "__unknownOperator__", names.unknownOperator,
		"__unknownDirection__", names.unknownDirection, "__tooManyBinds__", names.tooManyBinds,
		"__prefix__", current.request.Style.ErrorPrefix, "__limit__", strconv.Itoa(current.maxBinds()),
	).Replace(text)
	file := newSource(&current.request.Naming)
	for token, importPath := range helperPackages {
		if strings.Contains(text, token) {
			text = strings.ReplaceAll(text, token, file.qualifier(importPath))
		}
	}
	file.write(text)
	generated, err := file.generated(helpersFile, current.request)
	return generated, true, err
}

// editedAtRunTime reports whether a query's method edits its SQL at run time, and so
// calls the helpers: one with a slice list, a sortable query or a runtime builder.
//
// Takes rendered (*query) which is the query.
//
// Returns bool which is true when the query calls the helpers.
func editedAtRunTime(rendered *query) bool {
	return len(rendered.rendered.Marks.Lists) > 0 || sortableParameter(rendered.analysed) >= 0 || rendered.analysed.DynamicRuntime
}

// placeholderSource declares the function writing the placeholder of an argument's number
// in the dialect's positional form.
//
// Takes info (*dialect.Facts) which describes the dialect.
//
// Returns string which is the declaration.
func placeholderSource(info *dialect.Facts) string {
	form := info.Placeholders.PositionalForm()
	if form.Kind != dialect.PlaceholderNumbered {
		return "func __placeholder__(int) string {\n\treturn " + strconv.Quote(form.Prefix+form.Suffix) + "\n}\n"
	}
	return "func __placeholder__(number int) string {\n\treturn " + strconv.Quote(form.Prefix) + " + __strconv__.Itoa(number) + " + strconv.Quote(form.Suffix) + "\n}\n"
}

// paginationSource writes the body of the pagination helper in the dialect's syntax: the
// offset first or last, and an unbounded limit before an offset that comes alone where
// the dialect needs one.
//
// Takes info (*dialect.Facts) which describes the dialect.
//
// Returns string which is the body.
func paginationSource(info *dialect.Facts) string {
	syntax := info.Pagination
	unbounded := ""
	if syntax.UnboundedLimit != "" {
		unbounded = fmt.Sprintf(unboundedSource, strconv.Quote(syntax.Limit.Before+syntax.UnboundedLimit+syntax.Limit.After))
	}
	limit := fmt.Sprintf(limitSource, strconv.Quote(syntax.Limit.Before), strconv.Quote(syntax.Limit.After), unbounded)
	offset := fmt.Sprintf(offsetSource, strconv.Quote(syntax.Offset.Before), strconv.Quote(syntax.Offset.After))
	if syntax.OffsetFirst {
		return offset + limit
	}
	return limit + offset
}

// operatorEntries writes the entries of the operator map: the comparisons every dialect
// accepts, IS NULL and IS NOT NULL, and the dialect's own, upper-cased, leaving out an
// operator holding the text that opens a placeholder.
//
// Takes info (*dialect.Facts) which describes the dialect.
//
// Returns string which is the entries.
func operatorEntries(info *dialect.Facts) string {
	prefix := info.Placeholders.PositionalForm().Prefix
	var entries []string
	seen := make(map[string]bool)
	for _, operator := range slices.Concat(strings.Split(coreOperators, ","), info.Filters.Operators) {
		normalised := strings.ToUpper(strings.TrimSpace(operator))
		if !seen[normalised] && !slices.Contains(unaryOperators, normalised) && (prefix == "" || !strings.Contains(normalised, prefix)) {
			entries = append(entries, strconv.Quote(normalised)+": false")
		}
		seen[normalised] = true
	}
	for _, operator := range unaryOperators {
		entries = append(entries, strconv.Quote(operator)+": true")
	}
	return strings.Join(entries, argumentSeparator)
}

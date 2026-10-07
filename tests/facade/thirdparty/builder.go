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
	"cmp"
	"slices"
	"strconv"
	"strings"

	"seagull.sh/seagull/sdk/emit/emit_kit"
)

const (
	// builderCaller is the expression holding the Queries a builder runs on.
	builderCaller = "builder.queries"

	// builderParams is the expression holding a builder's parameters.
	builderParams = "builder.params"

	// builderFailure records the first error a builder's setter met.
	builderFailure = "if err != nil {\nbuilder.failure = %s(builder.failure, err)\nreturn builder\n}\n"

	// builderReturn ends a builder's setter.
	builderReturn = "return builder\n"

	// builderMethod opens a method of a builder.
	builderMethod = "func (builder *%s) %s(%s) *%s {\n"
)

// writeBuilder writes a dynamic: runtime query: the columns a caller may filter and order
// on, the builder type, the method returning a builder, the setters the query has a place
// for, and the All and Count terminals.
//
// Takes file (*source) which is the query file.
// Takes rendered (*query) which is the query.
func (current *generation) writeBuilder(file *source, rendered *query) {
	naming := rendered.naming
	current.writeAllowedColumns(file, rendered)
	file.printf("type %s struct {\nqueries *Queries\n", naming.Builder)
	if holder := rendered.holder(); holder != "" {
		file.printf("params %s\n", current.builderParamsType(file, rendered))
	}
	file.printf("predicates []%s\norders []string\nlimit int\noffset int\nfailure error\n%s\n", current.helpers.predicate, closeBlock)
	current.writeDescription(file, rendered)
	file.printf("func (%s *Queries) %s(%s) *%s {\n", receiver, naming.Method, current.parameters(file, rendered), naming.Builder)
	if holder := rendered.holder(); holder != "" {
		file.printf("return &%s{queries: %s, params: %s}\n%s\n", naming.Builder, receiver, holder, closeBlock)
	} else {
		file.printf("return &%s{queries: %s}\n%s\n", naming.Builder, receiver, closeBlock)
	}
	current.writeSetters(file, rendered)
	current.writeBuild(file, rendered, "build", &rendered.rendered, naming.SQLConstant)
	run := statement{caller: builderCaller, query: localQuery, arguments: spreadArguments}
	file.printf("func (builder *%s) All(ctx %s) (%s%s, error) {\n", naming.Builder, file.qualified(contextPackage, "Context"), slicePrefix, naming.Row)
	file.printf("query, args, err := builder.build()\n"+failWithError, failNil)
	writeMany(file, rendered, run, current.request.Style.EmptySlices)
	file.printf(closeBlock + "\n")
	if rendered.count == nil {
		return
	}
	current.writeBuild(file, rendered, "buildCount", rendered.count, naming.CountConstant)
	file.printf("func (builder *%s) Count(ctx %s) (int64, error) {\n", naming.Builder, file.qualified(contextPackage, "Context"))
	file.printf("query, args, err := builder.buildCount()\nif err != nil {\nreturn 0, err\n}\nvar count int64\n")
	file.printf("err = %s.%s.QueryRowContext(ctx, query, args...).Scan(&count)\nreturn count, err\n%s\n", builderCaller, connection, closeBlock)
}

// builderParamsType writes the type a builder keeps its query's parameters in: the Params
// struct, or the lone parameter's type.
//
// Takes file (*source) which is the query file.
// Takes rendered (*query) which is the query.
//
// Returns string which is the type's Go source.
func (current *generation) builderParamsType(file *source, rendered *query) string {
	if rendered.naming.Params != "" {
		return rendered.naming.Params
	}
	return current.parameterSource(file, rendered, 0)
}

// writeAllowedColumns writes the map from each column a caller may name to the SQL the
// builder writes for it: its qualified source, each part quoted as the dialect quotes
// identifiers.
//
// Takes file (*source) which is the query file.
// Takes rendered (*query) which is the query.
func (current *generation) writeAllowedColumns(file *source, rendered *query) {
	quote := current.request.Dialect.IdentifierQuote
	entries := make([]string, 0, len(rendered.analysed.AllowedColumns))
	for index := range rendered.analysed.AllowedColumns {
		column := &rendered.analysed.AllowedColumns[index]
		source := column.SourceExpression
		if source == "" {
			source = column.Name
		}
		parts := strings.Split(source, fieldSeparator)
		for index, part := range parts {
			parts[index] = quote.Quote(part)
		}
		entries = append(entries, strconv.Quote(column.Name)+": "+strconv.Quote(strings.Join(parts, fieldSeparator)))
	}
	file.printf("var %s = map[string]string{%s}\n\n", rendered.naming.AllowedColumns, strings.Join(entries, argumentSeparator))
}

// writeSetters writes the builder methods the query has a place for: Where where it may
// be filtered, OrderBy where it may be ordered, and Limit and Offset where it does not
// paginate itself.
//
// Takes file (*source) which is the query file.
// Takes rendered (*query) which is the query.
func (current *generation) writeSetters(file *source, rendered *query) {
	builder, allowed := rendered.naming.Builder, rendered.naming.AllowedColumns
	join := file.qualified(errorsPackage, "Join")
	marks := &rendered.rendered.Marks
	if marks.Filter != nil {
		file.printf(builderMethod, builder, "Where", "column string, operator string, value any", builder)
		file.printf("predicate, err := %s(column, operator, value, %s)\n"+builderFailure, current.helpers.newPredicate, allowed, join)
		file.printf("builder.predicates = append(builder.predicates, predicate)\n" + builderReturn + closeBlock + "\n")
	}
	if marks.OrderBy != nil {
		file.printf(builderMethod, builder, "OrderBy", "column string, direction string", builder)
		file.printf("order, err := %s(column, direction, %s)\n"+builderFailure, current.helpers.newOrder, allowed, join)
		file.printf("builder.orders = append(builder.orders, order)\n" + builderReturn + closeBlock + "\n")
	}
	if marks.Pagination != nil && marks.Pagination.Start == marks.Pagination.End {
		for _, setter := range []string{"Limit", "Offset"} {
			file.printf(builderMethod, builder, setter, "n int", builder)
			file.printf("builder.%s = n\n"+builderReturn+closeBlock+"\n", strings.ToLower(setter))
		}
	}
}

// writeBuild writes the builder method that assembles a rendering's SQL and arguments:
// the query's own arguments, then the caller's filters, joined to a condition the query
// writes inside parentheses or written as a WHERE clause, its orderings, and its limit
// and offset where the query has a place for them.
//
// Takes file (*source) which is the query file.
// Takes rendered (*query) which is the query.
// Takes name (string) which names the method.
// Takes rendering (*emit_kit.Rendered) which is the rendering, the query's or its
// count's.
// Takes constant (string) which is the constant holding the rendering's SQL.
func (current *generation) writeBuild(file *source, rendered *query, name string, rendering *emit_kit.Rendered, constant string) {
	file.printf("func (builder *%s) %s() (string, []any, error) {\n", rendered.naming.Builder, name)
	file.printf("if builder.failure != nil {\nreturn \"\", nil, builder.failure\n}\n")
	arguments := make([]string, 0, len(rendering.Arguments))
	for _, argument := range rendering.Arguments {
		arguments = append(arguments, rendered.access(builderParams, argument.Parameter))
	}
	file.printf("args := []any{%s}\n", strings.Join(arguments, argumentSeparator))
	edits := current.builderEdits(file, rendering, rendering != &rendered.rendered)
	if len(edits) == 0 {
		file.printf("return %s, args, nil\n%s\n", constant, closeBlock)
		return
	}
	sources := make([]string, 0, len(edits))
	for _, edit := range edits {
		sources = append(sources, edit.source)
	}
	file.printf("return %s(%s, %s), args, nil\n%s\n", current.helpers.splice, constant, strings.Join(sources, argumentSeparator), closeBlock)
}

// builderEdits writes the lines that number a rendering's filters and pagination, and
// returns the edits that place them and the orderings, in the order of their offsets. A
// count takes the filters alone, since ordering or paginating it would change no count or
// lose it.
//
// Takes file (*source) which is the query file.
// Takes rendering (*emit_kit.Rendered) which is the rendering.
// Takes counting (bool) which is true for the rendering of the query's count.
//
// Returns []placedEdit which holds the edits.
func (current *generation) builderEdits(file *source, rendering *emit_kit.Rendered, counting bool) []placedEdit {
	marks := &rendering.Marks
	var edits []placedEdit
	if filter := marks.Filter; filter != nil {
		file.printf("filter, args := %s(builder.predicates, args)\n", current.helpers.filter)
		start, end := filter.InsertAt, filter.InsertAt
		if filter.Present {
			start, end = filter.Condition.Start, filter.Condition.End
		}
		edits = append(edits, placedEdit{source: current.helpers.where + "(filter, " + strconv.Itoa(start) + argumentSeparator + strconv.Itoa(end) +
			argumentSeparator + strconv.FormatBool(filter.Present) + callClose, at: start})
	}
	if counting {
		return edits
	}
	if order := marks.OrderBy; order != nil {
		present := order.Start != order.End
		edits = append(edits, placedEdit{source: current.helpers.orders + "(builder.orders, " + strconv.Itoa(order.Start) + argumentSeparator +
			strconv.Itoa(order.End) + argumentSeparator + strconv.FormatBool(present) + callClose, at: order.End})
	}
	if place := marks.Pagination; place != nil && place.Start == place.End {
		file.printf("pagination, args := %s(builder.limit, builder.offset, %d, args)\n", current.helpers.pagination, place.Start)
		edits = append(edits, placedEdit{source: "pagination", at: place.Start})
	}
	slices.SortStableFunc(edits, func(left, right placedEdit) int { return cmp.Compare(left.at, right.at) })
	return edits
}

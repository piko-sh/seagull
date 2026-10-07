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
	"fmt"
	"slices"
	"strconv"
	"strings"

	"seagull.sh/seagull/sdk/dialect"
)

const (
	// argumentSeparator separates the arguments of a call.
	argumentSeparator = ", "

	// lengthOpen opens the length of a slice.
	lengthOpen = "len("

	// callClose closes a call.
	callClose = ")"

	// fieldSeparator separates a value from a field it holds.
	fieldSeparator = "."

	// localQuery is the local a method splices its SQL into.
	localQuery = "query"

	// spreadArguments passes the arguments local to a driver call.
	spreadArguments = ", args..."
)

// statement is how a method passes a query's SQL and arguments to the driver.
type statement struct {
	// caller is the expression holding the Queries the statement runs on.
	caller string

	// query is the expression holding the SQL: the query's constant, or the local the method
	// spliced it into.
	query string

	// arguments is what follows the SQL in the driver call: each argument after a comma, or
	// the arguments local spread, or nothing.
	arguments string
}

// placedEdit is one run-time edit of a query's SQL, at its offset.
type placedEdit struct {
	// source is the call that returns the edit.
	source string

	// at is the offset the edit is made at, which orders it among the others.
	at int
}

// access returns the expression reading a parameter: a field of the value holding the
// query's Params struct, or that value itself when the query passes its one parameter
// alone.
//
// Takes holder (string) which is the expression holding the parameters.
// Takes index (int) which is the parameter's index.
//
// Returns string which is the expression.
func (rendered *query) access(holder string, index int) string {
	if rendered.naming.Params == "" {
		return holder
	}
	return holder + fieldSeparator + rendered.naming.ParameterFields[index]
}

// holder returns the expression a method reads its parameters from: the Params argument,
// or the argument of a lone parameter.
//
// Returns string which is the expression, or empty for a query without parameters.
func (rendered *query) holder() string {
	if rendered.naming.Params != "" {
		return rendered.naming.ParamsArgument
	}
	if len(rendered.naming.ParameterFields) > 0 {
		return rendered.naming.ParameterFields[0]
	}
	return ""
}

// prepareStatement writes any SQL edits needed before the driver call and determines how
// to pass the SQL and arguments.
//
// Takes file (*source) which is the query file.
// Takes rendered (*query) which is the query.
// Takes failure (string) which is the code a method runs when the statement would bind
// too many values, with err set.
//
// Returns statement which says how to pass the SQL and the arguments.
func (current *generation) prepareStatement(file *source, rendered *query, failure string) statement {
	holder := rendered.holder()
	edits := slices.Concat(current.listEdits(rendered, holder), current.sortEdits(rendered, holder))
	if len(edits) == 0 {
		return statement{caller: receiver, query: rendered.naming.SQLConstant, arguments: inlineArguments(rendered, holder)}
	}
	slices.SortStableFunc(edits, func(left, right placedEdit) int { return cmp.Compare(left.at, right.at) })
	sources := make([]string, 0, len(edits))
	for _, edit := range edits {
		sources = append(sources, edit.source)
	}
	splice := fmt.Sprintf("query := %s(%s, %s)\n", current.helpers.splice, rendered.naming.SQLConstant, strings.Join(sources, argumentSeparator))
	if len(rendered.rendered.Marks.Lists) == 0 {
		file.write(splice)
		return statement{caller: receiver, query: localQuery, arguments: inlineArguments(rendered, holder)}
	}
	count := argumentCount(rendered, holder)
	file.printf("if err := %s(%s); err != nil {\n%s\n}\n", current.helpers.checkBinds, count, failure)
	file.write(splice)
	file.printf("args := make([]any, 0, %s)\n", count)
	for _, argument := range rendered.rendered.Arguments {
		if argument.Expand {
			file.printf("for _, value := range %s {\nargs = append(args, value)\n}\n", rendered.access(holder, argument.Parameter))
			continue
		}
		file.printf("args = append(args, %s)\n", rendered.access(holder, argument.Parameter))
	}
	return statement{caller: receiver, query: localQuery, arguments: spreadArguments}
}

// listEdits returns the edit of each slice parameter's list: one placeholder per element,
// numbered on from the list's first number, or the dialect's test for an empty list in
// place of the operator and the list.
//
// Takes rendered (*query) which is the query.
// Takes holder (string) which is the expression holding the parameters.
//
// Returns []placedEdit which holds an edit per list.
func (current *generation) listEdits(rendered *query, holder string) []placedEdit {
	lists := current.request.Dialect.Lists
	firsts := firstNumbers(rendered, holder, rendered.form.Kind)
	edits := make([]placedEdit, 0, len(rendered.rendered.Marks.Lists))
	for _, mark := range rendered.rendered.Marks.Lists {
		empty := lists.EmptyMembership
		if mark.Negated {
			empty = lists.EmptyExclusion
		}
		source := current.helpers.list + "(" + strings.Join([]string{
			lengthOpen + rendered.access(holder, mark.Parameter) + callClose, firsts[mark.Parameter],
			strconv.Itoa(mark.Operator.Start), strconv.Itoa(mark.Range.Start), strconv.Itoa(mark.Range.End), strconv.Quote(empty),
		}, argumentSeparator) + callClose
		edits = append(edits, placedEdit{source: source, at: mark.Operator.Start})
	}
	return edits
}

// sortEdits returns the edit writing a sortable query's ordering at the place of the
// ORDER BY the renderer left out.
//
// Takes rendered (*query) which is the query.
// Takes holder (string) which is the expression holding the parameters.
//
// Returns []placedEdit which holds the edit, or nothing for a query that is not sortable.
func (current *generation) sortEdits(rendered *query, holder string) []placedEdit {
	sortable := sortableParameter(rendered.analysed)
	place := rendered.rendered.Marks.OrderBy
	if sortable < 0 || place == nil {
		return nil
	}
	arguments := []string{
		"string(" + rendered.access(holder, sortable) + callClose,
		holder + fieldSeparator + rendered.naming.DirectionField,
		strconv.Itoa(place.Start),
	}
	for _, column := range rendered.analysed.Parameters[sortable].SortableColumns {
		arguments = append(arguments, strconv.Quote(column))
	}
	return []placedEdit{{source: current.helpers.sort + "(" + strings.Join(arguments, argumentSeparator) + callClose, at: place.Start}}
}

// firstNumbers returns, for each slice parameter, the expression of the number its first
// element takes: slices are numbered after every other argument, each after the elements
// of the slices before it. An anonymous form numbers nothing, so every list starts at
// zero.
//
// Takes rendered (*query) which is the query.
// Takes holder (string) which is the expression holding the parameters.
// Takes kind (dialect.PlaceholderKind) which is the kind of the form the SQL is in.
//
// Returns map[int]string which maps a slice parameter's index to the expression.
func firstNumbers(rendered *query, holder string, kind dialect.PlaceholderKind) map[int]string {
	firsts := make(map[int]string)
	single := 1
	var lengths []string
	for _, argument := range rendered.rendered.Arguments {
		if !argument.Expand {
			single++
			continue
		}
		firsts[argument.Parameter] = strings.Join(append([]string{strconv.Itoa(single)}, lengths...), "+")
		if kind != dialect.PlaceholderNumbered {
			firsts[argument.Parameter] = "0"
		}
		lengths = append(lengths, lengthOpen+rendered.access(holder, argument.Parameter)+callClose)
	}
	return firsts
}

// argumentCount returns the expression of how many values a statement binds: one per
// argument, and one per element of a slice.
//
// Takes rendered (*query) which is the query.
// Takes holder (string) which is the expression holding the parameters.
//
// Returns string which is the expression.
func argumentCount(rendered *query, holder string) string {
	single := 0
	var lengths []string
	for _, argument := range rendered.rendered.Arguments {
		if argument.Expand {
			lengths = append(lengths, lengthOpen+rendered.access(holder, argument.Parameter)+callClose)
			continue
		}
		single++
	}
	return strings.Join(append([]string{strconv.Itoa(single)}, lengths...), "+")
}

// inlineArguments returns the arguments a driver call passes after the SQL, in the order
// the rendering binds them, each after a comma.
//
// Takes rendered (*query) which is the query.
// Takes holder (string) which is the expression holding the parameters.
//
// Returns string which is the arguments, or empty when the SQL binds none.
func inlineArguments(rendered *query, holder string) string {
	var builder strings.Builder
	for _, argument := range rendered.rendered.Arguments {
		builder.WriteString(argumentSeparator + rendered.access(holder, argument.Parameter))
	}
	return builder.String()
}

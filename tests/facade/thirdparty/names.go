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
	"slices"
	"strings"
	"unicode"

	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/emit"
)

const (
	// receiver is the receiver of every Queries method.
	receiver = "queries"

	// connection is the Queries field every query runs on.
	connection = "db"

	// querierFile is the file holding the querier.
	querierFile = "querier.go"

	// modelsFile is the file holding the models and the named types.
	modelsFile = "models.go"

	// helpersFile is the file holding the helpers the generated methods call.
	helpersFile = "helpers.go"
)

var (
	// locals lists every local and argument a generated method body declares besides the
	// query's own, so the plan names no argument, local or package-level value after one.
	locals = []string{
		"args", "builder", "column", "count", "ctx", "direction", "end", "err", "filter", "found",
		"group", "groups", "index", "item", "key", "n", "offset", "operator", "order", "ordered",
		"pagination", "params", "predicate", "queries", "query", "result", "results", "row", "rows",
		"value", "values", "yield",
	}

	// imports lists the packages the generated code imports itself, besides those the mapped
	// Go types come from.
	imports = []string{"context", "database/sql", "errors", "fmt", "iter", "slices", "strconv", "strings"}

	// declarations lists the package-level names the generated code declares whatever the
	// style: the querier's and the sortable direction type's.
	declarations = []string{"DBTX", "New", "OrderAsc", "OrderDesc", "OrderDirection", "Queries"}

	// methods lists the methods and fields the querier declares on Queries.
	methods = []string{"RunInTx", "WithTx", connection}
)

// helperNames names the helpers the generated code declares, each starting with the
// style's helper prefix, and the error values the runtime builder returns, each named
// after the style's error prefix.
type helperNames struct {
	// edit is the type of one edit of a query's text.
	edit string

	// splice makes a query's edits.
	splice string

	// placeholder writes the placeholder of a numbered argument.
	placeholder string

	// list writes a slice parameter's list.
	list string

	// sort writes a sortable query's ORDER BY.
	sort string

	// checkBinds refuses a statement binding more values than the engine takes.
	checkBinds string

	// maxBinds is the most values one statement binds.
	maxBinds string

	// predicate is the type of one runtime filter.
	predicate string

	// operators lists the operators a runtime filter may use.
	operators string

	// newPredicate checks and builds a runtime filter.
	newPredicate string

	// newOrder checks and builds a runtime ordering.
	newOrder string

	// filter writes runtime filters.
	filter string

	// where places runtime filters in a query.
	where string

	// orders places runtime orderings in a query.
	orders string

	// pagination writes and places a runtime limit and offset.
	pagination string

	// queryNames maps a query's SQL to its name.
	queryNames string

	// queryNamePrefixes names a query by the start of its SQL.
	queryNamePrefixes string

	// unknownColumn is the error a runtime builder returns for a column it does not allow.
	unknownColumn string

	// unknownOperator is the error a runtime builder returns for an operator it does not
	// allow.
	unknownOperator string

	// unknownDirection is the error a runtime builder returns for a direction it does not
	// allow.
	unknownDirection string

	// tooManyBinds is the error a statement binding too many values returns.
	tooManyBinds string
}

// newHelperNames names the helpers after a style.
//
// Takes style (*emit.Style) which holds the helper and error prefixes.
//
// Returns helperNames which holds the names.
func newHelperNames(style *emit.Style) helperNames {
	helper := func(suffix string) string { return style.HelperPrefix + suffix }
	failure := func(suffix string) string { return "Err" + pascalCase(style.ErrorPrefix) + suffix }
	return helperNames{
		edit:              helper("Edit"),
		splice:            helper("Splice"),
		placeholder:       helper("Placeholder"),
		list:              helper("List"),
		sort:              helper("Sort"),
		checkBinds:        helper("CheckBinds"),
		maxBinds:          helper("MaxBinds"),
		predicate:         helper("Predicate"),
		operators:         helper("Operators"),
		newPredicate:      helper("NewPredicate"),
		newOrder:          helper("NewOrder"),
		filter:            helper("Filter"),
		where:             helper("Where"),
		orders:            helper("Orders"),
		pagination:        helper("Pagination"),
		queryNames:        helper("QueryNames"),
		queryNamePrefixes: helper("QueryNamePrefixes"),
		unknownColumn:     failure("UnknownColumn"),
		unknownOperator:   failure("UnknownOperator"),
		unknownDirection:  failure("UnknownDirection"),
		tooManyBinds:      failure("TooManyBindVariables"),
	}
}

// all lists every name.
//
// Returns []string which holds the names.
func (names *helperNames) all() []string {
	return []string{
		names.edit, names.splice, names.placeholder, names.list, names.sort, names.checkBinds, names.maxBinds,
		names.predicate, names.operators, names.newPredicate, names.newOrder, names.filter, names.where,
		names.orders, names.pagination, names.queryNames, names.queryNamePrefixes, names.unknownColumn,
		names.unknownOperator, names.unknownDirection, names.tooManyBinds,
	}
}

// Reserve returns the names the generated code uses whatever the queries are: the
// receiver, the locals of every method body, the packages the code imports, the querier's
// declarations, the helpers and the files.
//
// Takes style (*emit.Style) which holds the helper and error prefixes. Reserve does not
// change it.
//
// Returns emit.Reservation which holds the names.
func (*Emitter) Reserve(_ *dialect.Facts, style *emit.Style) emit.Reservation {
	helpers := newHelperNames(style)
	return emit.Reservation{
		Receiver:     receiver,
		Locals:       slices.Clone(locals),
		Imports:      slices.Clone(imports),
		Declarations: append(slices.Clone(declarations), helpers.all()...),
		Methods:      slices.Clone(methods),
		Files:        []string{helpersFile, modelsFile, querierFile},
	}
}

// pascalCase writes text in Pascal case, each run of letters and digits starting with a
// capital, so acme_store gives AcmeStore.
//
// Takes text (string) which is the text.
//
// Returns string which is the Pascal-cased text.
func pascalCase(text string) string {
	words := strings.FieldsFunc(text, func(character rune) bool {
		return !unicode.IsLetter(character) && !unicode.IsDigit(character)
	})
	for index, word := range words {
		runes := []rune(word)
		runes[0] = unicode.ToUpper(runes[0])
		words[index] = string(runes)
	}
	return strings.Join(words, "")
}

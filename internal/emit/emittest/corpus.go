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

package emittest

import (
	"slices"

	"seagull.sh/seagull/internal/sqlmodel"
)

const (
	// caseCommands holds a query of every common command.
	caseCommands = "commands"

	// caseResults holds the commands that report what a statement changed, which an emitter
	// whose driver cannot report it may refuse.
	caseResults = "results"

	// caseNamed holds a named parameter written twice.
	caseNamed = "named"

	// caseSlices holds slice parameters in IN and NOT IN lists.
	caseSlices = "slices"

	// caseBatch holds a multi-row insert whose row holds an expression and a literal that
	// looks like placeholders.
	caseBatch = "batch"

	// caseBatchSuffix holds a batched insert with a parameter after its VALUES row.
	caseBatchSuffix = "batchsuffix"

	// caseBatchUpsert holds a batched insert whose clause after its VALUES row updates a row
	// the insert meets, with no parameter of its own, which may meet one key twice in a
	// batch.
	caseBatchUpsert = "batchupsert"

	// caseSortable holds sortable queries with and without a base ORDER BY and LIMIT.
	caseSortable = "sortable"

	// caseSharedSQL holds queries whose SQL another query of the package runs too: two
	// sortable queries over one base SELECT, and two plain queries written alike.
	caseSharedSQL = "sharedsql"

	// caseBuilder holds a runtime-built query with a parameter of its own.
	caseBuilder = "builder"

	// caseBuilderClauses holds a runtime-built query whose base SQL orders and limits.
	caseBuilderClauses = "builderclauses"

	// caseBuilderFilter holds a runtime-built query whose base SQL filters with OR.
	caseBuilderFilter = "builderfilter"

	// caseFolded holds two parameters whose names fold to one Go name.
	caseFolded = "folded"

	// caseFoldedSortable holds folding parameter names in a sortable query.
	caseFoldedSortable = "foldedsortable"

	// caseFoldedSlice holds folding parameter names beside a slice.
	caseFoldedSlice = "foldedslice"

	// caseFoldedBatch holds folding parameter names in a batch.
	caseFoldedBatch = "foldedbatch"

	// caseReservedParameters holds parameters named like the generated code's locals,
	// receiver and imports.
	caseReservedParameters = "reservedparameters"

	// caseReservedQueries holds queries named like the generated package's declarations.
	caseReservedQueries = "reservedqueries"

	// caseReservedTables holds tables named like the generated package's types.
	caseReservedTables = "reservedtables"

	// caseGroupedColumns holds a grouped query selecting two columns of one name.
	caseGroupedColumns = "groupedcolumns"

	// caseTwoSortableFiles holds a sortable query in each of two files.
	caseTwoSortableFiles = "twosortablefiles"

	// caseFileNames holds query files whose names Go would treat specially.
	caseFileNames = "filenames"

	// caseEnums holds an enum parameter, an enum slice, an enum column and an enum in a
	// batch row, using the named types the plan declares and any required driver
	// conversions.
	caseEnums = "enums"

	// caseNullableEnums reads and writes an array of nullable enums.
	caseNullableEnums = "nullableenums"

	// caseEnumMaps reads and writes a map of enums.
	caseEnumMaps = "enummaps"

	// caseEnumGrids reads and writes an array of arrays of enums.
	caseEnumGrids = "enumgrids"

	// caseOverrides reads and writes columns whose Go type a column override chose, a
	// defined string type and a nullable defined integer type, exercising any conversions
	// needed at the driver boundary.
	caseOverrides = "overrides"

	// caseJSON writes JSON documents, in a statement and in batch rows, which reach the
	// driver as the text they hold.
	caseJSON = "json"

	// caseJSONReads reads a column of JSON documents, one row, many and as a stream, into a
	// destination the driver fills.
	caseJSONReads = "jsonreads"

	// caseDescribed holds queries whose headers describe them, which their methods' doc
	// comments carry.
	caseDescribed = "described"

	// caseRowTypes holds queries reading their rows into a table's model and into a row type
	// two of them share.
	caseRowTypes = "rowtypes"

	// tableItems is the table most cases query.
	tableItems = "items"

	// tableOrders is the table grouped and joined queries add.
	tableOrders = "orders"

	// tableFeelings is the table whose column is an enum.
	tableFeelings = "feelings"

	// tableMoodSets is the table whose column holds the enum in a nested shape.
	tableMoodSets = "moodsets"

	// tableNotes is the table whose columns a column override types.
	tableNotes = "notes"

	// tableDocuments is the table whose column holds JSON documents.
	tableDocuments = "documents"

	// enumName names the enum the enum case's schema declares.
	enumName = "feeling"

	// commandMany is the query command returning every row.
	commandMany = "many"

	// commandOne is the query command returning one row.
	commandOne = "one"

	// commandExec is the query command returning nothing.
	commandExec = "exec"

	// commandBatch is the query command writing many rows.
	commandBatch = "batch"

	// confusableLiteral is a SQL string literal holding text that looks like the
	// placeholders of many dialects, which code that scans SQL text for placeholders
	// mistakes for parameters.
	confusableLiteral = "'x $1 ? :n @p1 {n:T} $'"
)

const (
	// columnInteger types a column with the dialect's row-count type, its general integer.
	columnInteger columnKind = iota

	// columnText types a column with the dialect's general text type.
	columnText

	// columnOverriddenText types a column with the dialect's general text type and a column
	// override naming a defined string type, html/template.HTML, which the generated code
	// binds and reads as that type.
	columnOverriddenText

	// columnOverriddenInteger types a column with the dialect's row-count type and a column
	// override naming a defined integer type, go/token.Pos.
	columnOverriddenInteger

	// columnJSON types a column with the dialect's JSON type, which the generated code holds
	// in its JSON type.
	columnJSON

	// columnEnum types a column with the enum the case's schema declares. A typed
	// placeholder carries the dialect's general text type for it, which every engine
	// compares with an enum.
	columnEnum

	// columnNullableEnums types a column as an array of the enum the case's schema declares
	// whose elements may be NULL. A typed placeholder carries the dialect's general text
	// type for it, as for the enum.
	columnNullableEnums

	// columnEnumMap types a column as a map from the dialect's general text type to the enum
	// the case's schema declares.
	columnEnumMap

	// columnEnumGrid types a column as an array of arrays of the enum the case's schema
	// declares.
	columnEnumGrid
)

const (
	// scenarioNone leaves the query out of the runtime lane.
	scenarioNone scenario = iota

	// scenarioPlain calls the method once with every argument set.
	scenarioPlain

	// scenarioSlices calls the method with each slice argument nil, empty, of one element
	// and of three.
	scenarioSlices

	// scenarioBatch calls the method with three items.
	scenarioBatch

	// scenarioSortable calls the method sorted by the first column it may be sorted by.
	scenarioSortable

	// scenarioBuilder runs the builder the generated method returns twice: All with a
	// filter, an order, a limit and an offset, and Count with the filter.
	scenarioBuilder
)

// enumValueList holds the values of the enum the enum case's schema declares.
var enumValueList = []string{"calm", "lively"}

// standardTables holds the schema most cases share: items and orders.
var standardTables = []corpusTable{
	{name: tableItems, quoted: false, columns: []corpusColumn{
		{name: columnID, kind: columnInteger, nullable: false},
		{name: columnKindName, kind: columnText, nullable: false},
		{name: "name", kind: columnText, nullable: true},
		{name: columnRank, kind: columnInteger, nullable: false},
	}},
	{name: tableOrders, quoted: false, columns: []corpusColumn{
		{name: columnID, kind: columnInteger, nullable: false},
		{name: "item_id", kind: columnInteger, nullable: false},
		{name: "total", kind: columnInteger, nullable: false},
	}},
}

// corpusCases holds every case of the neutral corpus.
var corpusCases = slices.Concat(generalCases, findingCases, byteCases())

// columnKind says which of the dialect's core types a corpus column takes.
type columnKind uint8

// scenario says how the runtime lane calls a query's method.
type scenario uint8

// corpusColumn is one column of a corpus table.
type corpusColumn struct {
	// name is the column's name, written unquoted.
	name string

	// kind chooses the column's type.
	kind columnKind

	// nullable is true when the column accepts NULL.
	nullable bool
}

// corpusTable is one table of a case's schema.
type corpusTable struct {
	// name is the table's name.
	name string

	// columns lists the table's columns in order.
	columns []corpusColumn

	// quoted is true when the name is kept exactly as written, and queries write it quoted.
	quoted bool
}

// corpusParameter is one parameter a corpus query declares.
type corpusParameter struct {
	// name is the parameter's name: the name a directive gives it, and the name a named
	// placeholder carries.
	name string

	// kind chooses the type a typed placeholder writes.
	kind columnKind

	// slice is true for a kind: slice parameter.
	slice bool

	// optional is true for an optional: true parameter.
	optional bool
}

// corpusQuery is one query of the corpus, written as a template the suite renders in a
// dialect's syntax: {name} stands for the placeholder of the parameter of that name, and
// [[name]] for a quoted identifier.
type corpusQuery struct {
	// name is the query's name.
	name string

	// command is the query's command.
	command string

	// options holds further arguments of the query directive, such as dynamic: runtime.
	options string

	// description is the prose the query's header carries above it, which its method's doc
	// comment holds; empty for none.
	description string

	// headers lists further header directives without their namespace, such as
	// sortable(order_by, [id, kind]).
	headers []string

	// parameters declares the parameters the template writes.
	parameters []corpusParameter

	// variants lists the query's SQL; the first the dialect analyses is used.
	variants []string

	// uncounted names the parameters a runtime builder's Count need not bind, such as one in
	// the base query's pagination, which counting leaves out.
	uncounted []string

	// scenario says how the runtime lane calls the query.
	scenario scenario
}

// corpusFile is one query file of a case.
type corpusFile struct {
	// name is the file's name.
	name string

	// queries lists the file's queries in order.
	queries []corpusQuery
}

// corpusCase is one package of the neutral corpus: query files generated together.
type corpusCase struct {
	// name identifies the case.
	name string

	// files lists the case's query files.
	files []corpusFile

	// tables is the case's schema; nil means the standard tables.
	tables []corpusTable

	// general is true when every general check examines the case, rather than only the
	// checks that name it.
	general bool

	// runtime is true when the runtime lane calls the case's methods.
	runtime bool

	// refusable is true when an emitter whose driver cannot run the case's queries may
	// refuse them with a diagnostic.
	refusable bool
}

// text declares a text parameter.
//
// Takes name (string) which is the parameter's name.
//
// Returns corpusParameter which is the parameter.
func text(name string) corpusParameter {
	return corpusParameter{name: name, kind: columnText, slice: false, optional: false}
}

// integer declares an integer parameter.
//
// Takes name (string) which is the parameter's name.
//
// Returns corpusParameter which is the parameter.
func integer(name string) corpusParameter {
	return corpusParameter{name: name, kind: columnInteger, slice: false, optional: false}
}

// integers declares an integer kind: slice parameter.
//
// Takes name (string) which is the parameter's name.
//
// Returns corpusParameter which is the parameter.
func integers(name string) corpusParameter {
	return corpusParameter{name: name, kind: columnInteger, slice: true, optional: false}
}

// enumValue declares an enum parameter.
//
// Takes name (string) which is the parameter's name.
//
// Returns corpusParameter which is the parameter.
func enumValue(name string) corpusParameter {
	return corpusParameter{name: name, kind: columnEnum, slice: false, optional: false}
}

// enumValues declares an enum kind: slice parameter.
//
// Takes name (string) which is the parameter's name.
//
// Returns corpusParameter which is the parameter.
func enumValues(name string) corpusParameter {
	return corpusParameter{name: name, kind: columnEnum, slice: true, optional: false}
}

// holdsEnum reports whether a column of a kind holds the enum the case's schema declares,
// alone or in a nested shape.
//
// Takes kind (columnKind) which is the kind.
//
// Returns bool which is true when the column holds the enum.
func holdsEnum(kind columnKind) bool {
	return kind >= columnEnum
}

// overrideOf returns the Go type a column override chooses for a column of a kind, or nil
// for a kind no override types.
//
// Takes kind (columnKind) which is the kind.
//
// Returns *sqlmodel.GoType which is the override's Go type, or nil.
func overrideOf(kind columnKind) *sqlmodel.GoType {
	switch kind {
	case columnOverriddenText:
		return &sqlmodel.GoType{Composite: nil, Package: "html/template", Name: "HTML"}
	case columnOverriddenInteger:
		return &sqlmodel.GoType{Composite: nil, Package: "go/token", Name: "Pos"}
	default:
		return nil
	}
}

// optionalText declares an optional text parameter.
//
// Takes name (string) which is the parameter's name.
//
// Returns corpusParameter which is the parameter.
func optionalText(name string) corpusParameter {
	return corpusParameter{name: name, kind: columnText, slice: false, optional: true}
}

// query declares a query with one SQL variant and no further directives.
//
// Takes name (string) which is the query's name.
// Takes command (string) which is the query's command.
// Takes sql (string) which is the query's template.
// Takes calls (scenario) which says how the runtime lane calls it.
// Takes parameters (...corpusParameter) which declares its parameters.
//
// Returns corpusQuery which is the query.
func query(name, command, sql string, calls scenario, parameters ...corpusParameter) corpusQuery {
	return corpusQuery{
		name:        name,
		command:     command,
		options:     "",
		headers:     nil,
		parameters:  parameters,
		variants:    []string{sql},
		uncounted:   nil,
		description: "",
		scenario:    calls,
	}
}

// singleFile returns a case's only query file.
//
// Takes queries (...corpusQuery) which are the file's queries.
//
// Returns []corpusFile which holds the file.
func singleFile(queries ...corpusQuery) []corpusFile {
	return []corpusFile{{name: "queries.sql", queries: queries}}
}

// findCase returns the case of a name.
//
// Takes name (string) which is the case's name.
//
// Returns corpusCase which is the case.
// Returns bool which is false when the corpus has no such case.
func findCase(name string) (corpusCase, bool) {
	for _, current := range corpusCases {
		if current.name == name {
			return current, true
		}
	}
	return corpusCase{}, false
}

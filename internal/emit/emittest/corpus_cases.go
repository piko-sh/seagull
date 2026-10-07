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

import "slices"

const (
	// columnID names the key column of every corpus table.
	columnID = "id"

	// columnKindName names the text column most queries filter on.
	columnKindName = "kind"

	// columnRank names the integer column most queries compare.
	columnRank = "rank"

	// columnLabel names the text column a column override types.
	columnLabel = "label"

	// columnBody names the column holding a JSON document.
	columnBody = "body"

	// foldingName and foldingTwin are parameter names that fold to one Go name.
	foldingName = "foo_bar"

	// foldingTwin is the parameter name that folds to foldingName's Go name.
	foldingTwin = "foo__bar"

	// modelOfItems is the model the items table gives.
	modelOfItems = "Items"

	// sharedKindRow is the row type two queries of the row types case share.
	sharedKindRow = "ItemKind"
)

// generalCases holds the cases every general check examines: one feature each, written so
// every emitter should generate them.
var generalCases = []corpusCase{
	{name: caseCommands, general: true, runtime: true, refusable: false, tables: nil, files: singleFile(
		query("GetItem", commandOne, "SELECT id, kind, name FROM items WHERE id = {id};", scenarioPlain, integer(columnID)),
		query("ListItems", commandMany, "SELECT id, kind, name, rank FROM items WHERE kind = {kind} ORDER BY id;", scenarioPlain, text(columnKindName)),
		query("StreamItems", "stream", "SELECT id, name FROM items WHERE rank > {rank};", scenarioPlain, integer(columnRank)),
		query("CreateItem", commandExec, "INSERT INTO items (id, kind, name, rank) VALUES ({id}, {kind}, {name}, {rank});", scenarioPlain,
			integer(columnID), text(columnKindName), text("name"), integer(columnRank)),
		query("RenameItem", commandExec, "UPDATE items SET name = {name} WHERE id = {id};", scenarioPlain, text("name"), integer(columnID)),
		query("DeleteItem", commandExec, "DELETE FROM items WHERE id = {id};", scenarioPlain, integer(columnID)),
		query("FilterItems", commandMany,
			"SELECT id FROM items WHERE kind = {kind} AND rank >= {lowest} AND rank <= {highest} AND id <> {excluded} AND name = {label};",
			scenarioPlain, text(columnKindName), integer("lowest"), integer("highest"), integer("excluded"), text("label")),
		optionalItem(),
		query("FindByLabel", commandMany, "SELECT id FROM items WHERE name = {label};", scenarioPlain, optionalText("label")),
	)},
	{name: caseResults, general: true, runtime: true, refusable: true, tables: nil, files: singleFile(
		query("DeleteCounted", "execrows", "DELETE FROM items WHERE kind = {kind};", scenarioPlain, text(columnKindName)),
		query("DeleteReported", "execresult", "DELETE FROM items WHERE rank = {rank};", scenarioPlain, integer(columnRank)),
	)},
	{name: caseNamed, general: true, runtime: true, refusable: false, tables: nil, files: singleFile(
		query("RepeatedName", commandMany, "SELECT id FROM items WHERE kind = {kind} OR name = {kind};", scenarioPlain, text(columnKindName)),
	)},
	{name: caseSlices, general: true, runtime: true, refusable: false, tables: nil, files: singleFile(
		query("ItemsIn", commandMany, "SELECT id FROM items WHERE id IN ({ids}) AND kind = {kind};", scenarioSlices,
			integers("ids"), text(columnKindName)),
		query("ItemsNotIn", commandMany, "SELECT id FROM items WHERE kind = {kind} AND id NOT IN ({ids});", scenarioSlices,
			text(columnKindName), integers("ids")),
	)},
	{name: caseBatch, general: true, runtime: true, refusable: false, tables: nil, files: singleFile(
		query("InsertItems", commandBatch,
			"INSERT INTO items (id, kind, rank, name) VALUES ({id}, {kind}, {rank} + 1, "+confusableLiteral+");",
			scenarioBatch, integer(columnID), text(columnKindName), integer(columnRank)),
	)},
	{name: caseSortable, general: true, runtime: true, refusable: false, tables: nil, files: singleFile(
		sortableQuery("SortedLimited", "SELECT id, kind FROM items WHERE kind = {kind} ORDER BY id LIMIT 5;", text(columnKindName)),
		sortableQuery("SortedTerminated", "SELECT id, kind FROM items WHERE rank > {rank};", integer(columnRank)),
	)},
	{name: caseBuilder, general: true, runtime: true, refusable: false, tables: nil, files: singleFile(searchItems())},
}

// findingCases holds cases that target individual classes of regression, each examined by
// a dedicated check.
var findingCases = []corpusCase{
	{name: caseSharedSQL, general: false, runtime: true, refusable: false, tables: nil, files: singleFile(
		sortableQuery("SortedByID", "SELECT id, kind FROM items WHERE rank > {rank} ORDER BY id;", integer(columnRank)),
		sortableQuery("SortedByKind", "SELECT id, kind FROM items WHERE rank > {rank} ORDER BY kind;", integer(columnRank)),
		query("ItemsOfKind", commandMany, "SELECT id, name FROM items WHERE kind = {kind};", scenarioPlain, text(columnKindName)),
		query("ItemsOfKindAgain", commandMany, "SELECT id, name FROM items WHERE kind = {kind};", scenarioPlain, text(columnKindName)),
	)},
	{name: caseBatchSuffix, general: false, runtime: true, refusable: true, tables: nil, files: singleFile(corpusQuery{
		name: "UpsertItems", command: commandBatch, options: "", headers: nil, uncounted: nil, description: "", scenario: scenarioBatch,
		parameters: []corpusParameter{integer(columnID), text(columnKindName), text("replacement")},
		variants: []string{
			"INSERT INTO items (id, kind) VALUES ({id}, {kind}) ON CONFLICT (id) DO UPDATE SET kind = {replacement};",
			"INSERT INTO items (id, kind) VALUES ({id}, {kind}) ON DUPLICATE KEY UPDATE kind = {replacement};",
		},
	})},
	{name: caseBatchUpsert, general: false, runtime: true, refusable: true, tables: nil, files: singleFile(corpusQuery{
		name: "MergeItems", command: commandBatch, options: "", headers: nil, uncounted: nil, description: "", scenario: scenarioBatch,
		parameters: []corpusParameter{integer(columnID), text(columnKindName)},
		variants: []string{
			"INSERT INTO items (id, kind) VALUES ({id}, {kind}) ON CONFLICT (id) DO UPDATE SET kind = excluded.kind;",
			"INSERT INTO items (id, kind) VALUES ({id}, {kind}) ON DUPLICATE KEY UPDATE kind = VALUES(kind);",
		},
	})},
	{name: caseBuilderClauses, general: false, runtime: true, refusable: false, tables: nil, files: singleFile(searchOrdered())},
	{name: caseBuilderFilter, general: false, runtime: true, refusable: false, tables: nil, files: singleFile(
		builderQuery("SearchFiltered", "SELECT id, kind FROM items WHERE kind = {kind} OR name = {name}", text(columnKindName), text("name")),
	)},
	{name: caseFolded, general: false, runtime: false, refusable: false, tables: nil, files: singleFile(
		query("FoldedNames", commandMany, "SELECT id FROM items WHERE kind = {foo_bar} AND name = {foo__bar};", scenarioNone,
			text(foldingName), text(foldingTwin)),
	)},
	{name: caseFoldedSortable, general: false, runtime: false, refusable: false, tables: nil, files: singleFile(
		sortableQuery("FoldedSorted", "SELECT id, kind FROM items WHERE kind = {foo_bar} AND name = {foo__bar};",
			text(foldingName), text(foldingTwin)),
	)},
	{name: caseFoldedSlice, general: false, runtime: false, refusable: false, tables: nil, files: singleFile(
		query("FoldedIn", commandMany, "SELECT id FROM items WHERE id IN ({ids}) AND kind = {foo_bar} AND name = {foo__bar};",
			scenarioNone, integers("ids"), text(foldingName), text(foldingTwin)),
	)},
	{name: caseFoldedBatch, general: false, runtime: false, refusable: false, tables: nil, files: singleFile(
		query("FoldedInsert", commandBatch, "INSERT INTO items (id, kind, name) VALUES ({id}, {foo_bar}, {foo__bar});",
			scenarioNone, integer(columnID), text(foldingName), text(foldingTwin)),
	)},
	reservedParametersCase(),
	{name: caseReservedQueries, general: false, runtime: false, refusable: false, tables: nil, files: singleFile(
		query("Context", commandMany, "SELECT id FROM items WHERE rank = {rank};", scenarioNone, integer(columnRank)),
		query("New", commandMany, "SELECT id FROM items WHERE kind = {kind};", scenarioNone, text(columnKindName)),
		query("WithTx", commandExec, "DELETE FROM items WHERE id = {id};", scenarioNone, integer(columnID)),
	)},
	reservedTablesCase(),
	groupedColumnsCase(),
	{name: caseTwoSortableFiles, general: false, runtime: false, refusable: false, tables: nil, files: []corpusFile{
		{name: "first.sql", queries: []corpusQuery{sortableQuery("SortedFirst", "SELECT id, kind FROM items WHERE rank > {rank};", integer(columnRank))}},
		{name: "second.sql", queries: []corpusQuery{sortableQuery("SortedSecond", "SELECT id, kind FROM items WHERE kind = {kind};", text(columnKindName))}},
	}},
	enumsCase(),
	nestedEnumCase(caseNullableEnums, "maybes", "Maybes", columnNullableEnums),
	nestedEnumCase(caseEnumMaps, "labels", "Labels", columnEnumMap),
	nestedEnumCase(caseEnumGrids, "grid", "Grid", columnEnumGrid),
	overridesCase(),
	jsonCase(),
	jsonReadsCase(),
	{name: caseFileNames, general: false, runtime: false, refusable: false, tables: nil, files: []corpusFile{
		{name: "devices_ios.sql", queries: []corpusQuery{query("ListDevices", commandMany, "SELECT id FROM items;", scenarioNone)}},
		{name: "reports_windows_amd64.sql", queries: []corpusQuery{query("ListReports", commandMany, "SELECT kind FROM items;", scenarioNone)}},
		{name: "_private.sql", queries: []corpusQuery{query("ListPrivate", commandMany, "SELECT name FROM items;", scenarioNone)}},
	}},
	describedCase(),
	rowTypesCase(),
}

// rowTypeReturns maps each query of the row types case to the struct its method's rows
// are read into.
var rowTypeReturns = map[string]string{
	"ListItemModels":   modelOfItems,
	"FirstItemModel":   modelOfItems,
	"StreamItemModels": modelOfItems,
	"ListKinds":        sharedKindRow,
	"FirstKind":        sharedKindRow,
}

// nestedEnumCaseNames lists the cases whose column holds the enum in a nested shape.
var nestedEnumCaseNames = []string{caseNullableEnums, caseEnumMaps, caseEnumGrids}

// byteClass is one kind of byte a SQL literal may hold that a Go raw string cannot.
type byteClass struct {
	// name names the class in the case's name.
	name string

	// content is the text between the literal's quotes.
	content string
}

// byteClasses holds the bytes the SQL constants must carry unchanged.
var byteClasses = []byteClass{
	{name: "bom", content: "x\ufeffy"},
	{name: "nul", content: "x\x00y"},
	{name: "carriagereturn", content: "x\ry"},
	{name: "backtick", content: "x`y"},
	{name: "invalidutf8", content: "x\xff\xfey"},
}

// searchItems returns a runtime-built query with a parameter of its own: in a join
// condition, or in the base filter where the dialect has no join.
//
// Returns corpusQuery which is the query.
func searchItems() corpusQuery {
	search := builderQuery("SearchItems",
		"SELECT items.id, items.kind FROM items JOIN orders ON orders.item_id = items.id AND orders.total > {minimum}",
		integer("minimum"))
	search.variants = append(search.variants, "SELECT id, kind FROM items WHERE rank > {minimum}")
	return search
}

// optionalItem returns a one-row query that reports a miss instead of failing.
//
// Returns corpusQuery which is the query.
func optionalItem() corpusQuery {
	optional := query("FindItem", commandOne, "SELECT id, name FROM items WHERE kind = {kind};", scenarioPlain, text(columnKindName))
	optional.options = "optional: true"
	return optional
}

// sortableQuery returns a query sortable by id and kind.
//
// Takes name (string) which is the query's name.
// Takes sql (string) which is the query's template.
// Takes parameters (...corpusParameter) which declares its parameters.
//
// Returns corpusQuery which is the query.
func sortableQuery(name, sql string, parameters ...corpusParameter) corpusQuery {
	sorted := query(name, commandMany, sql, scenarioSortable, parameters...)
	sorted.headers = []string{"sortable(order_by, [id, kind])"}
	return sorted
}

// builderQuery returns a runtime-built query.
//
// Takes name (string) which is the query's name.
// Takes sql (string) which is the query's template.
// Takes parameters (...corpusParameter) which declares its parameters.
//
// Returns corpusQuery which is the query.
func builderQuery(name, sql string, parameters ...corpusParameter) corpusQuery {
	built := query(name, commandMany, sql, scenarioBuilder, parameters...)
	built.options = "dynamic: runtime"
	return built
}

// rowTypesCase returns queries reading their rows into the items table's model, the
// columns in the model's order or another, one at a time, all at once, as a stream and
// through a runtime builder, and two queries sharing a row type, whose columns come in
// different orders.
//
// Returns corpusCase which is the case.
func rowTypesCase() corpusCase {
	listed := query("ListItemModels", commandMany, "SELECT id, kind, name, rank FROM items WHERE kind = {kind};", scenarioPlain, text(columnKindName))
	listed.options = "model: items"
	first := query("FirstItemModel", commandOne, "SELECT rank, name, kind, id FROM items WHERE id = {id};", scenarioPlain, integer(columnID))
	first.options = "model: items"
	streamed := query("StreamItemModels", "stream", "SELECT id, kind, name, rank FROM items WHERE rank > {rank};", scenarioPlain, integer(columnRank))
	streamed.options = "model: items"
	searched := builderQuery("SearchItemModels", "SELECT id, kind, name, rank FROM items WHERE rank > {rank}", integer(columnRank))
	searched.options = "dynamic: runtime, model: items"
	kinds := query("ListKinds", commandMany, "SELECT id, kind FROM items WHERE rank > {rank};", scenarioPlain, integer(columnRank))
	kinds.options = "row: " + sharedKindRow
	kind := query("FirstKind", commandOne, "SELECT kind, id FROM items WHERE id = {id};", scenarioPlain, integer(columnID))
	kind.options = "row: " + sharedKindRow
	return corpusCase{name: caseRowTypes, general: false, runtime: true, refusable: false, tables: nil, files: singleFile(
		listed, first, streamed, searched, kinds, kind,
	)}
}

// describedCase returns queries whose headers describe them: one in two paragraphs, one a
// line below its header, and one without a description.
//
// Returns corpusCase which is the case.
func describedCase() corpusCase {
	listed := query("ListDescribed", commandMany, "SELECT id, kind FROM items WHERE kind = {kind};", scenarioNone, text(columnKindName))
	listed.description = "ListDescribed lists the items of one kind.\n\nIt reads them in no particular order."
	removed := query("DeleteDescribed", commandExec, "DELETE FROM items WHERE id = {id};", scenarioNone, integer(columnID))
	removed.description = "DeleteDescribed deletes one item."
	return corpusCase{name: caseDescribed, general: false, runtime: false, refusable: false, tables: nil, files: singleFile(
		listed,
		removed,
		query("CountDescribed", commandOne, "SELECT count(*) AS total FROM items;", scenarioNone),
	)}
}

// enumsCase returns queries over a table whose column is an enum: a lookup by one value,
// a slice of values, and a batch writing one per row.
//
// Returns corpusCase which is the case.
func enumsCase() corpusCase {
	tables := []corpusTable{{name: tableFeelings, quoted: false, columns: []corpusColumn{
		{name: columnID, kind: columnInteger, nullable: false},
		{name: "mood", kind: columnEnum, nullable: false},
	}}}
	return corpusCase{name: caseEnums, general: false, runtime: true, refusable: false, tables: tables, files: singleFile(
		query("FindFeeling", commandOne, "SELECT id, mood FROM feelings WHERE mood = {mood};", scenarioPlain, enumValue("mood")),
		query("FeelingsIn", commandMany, "SELECT id, mood FROM feelings WHERE mood IN ({moods});", scenarioSlices, enumValues("moods")),
		query("RecordFeelings", commandBatch, "INSERT INTO feelings (id, mood) VALUES ({id}, {mood});", scenarioBatch,
			integer(columnID), enumValue("mood")),
	)}
}

// overridesCase returns queries over a table whose columns a column override types with
// defined types over a string and a nullable integer: a lookup by the string reading
// both, and a batch writing one row each.
//
// Returns corpusCase which is the case.
func overridesCase() corpusCase {
	tables := []corpusTable{{name: tableNotes, quoted: false, columns: []corpusColumn{
		{name: columnID, kind: columnInteger, nullable: false},
		{name: columnLabel, kind: columnOverriddenText, nullable: false},
		{name: "position", kind: columnOverriddenInteger, nullable: true},
	}}}
	label := corpusParameter{name: columnLabel, kind: columnOverriddenText, slice: false, optional: false}
	position := corpusParameter{name: "position", kind: columnOverriddenInteger, slice: false, optional: false}
	return corpusCase{name: caseOverrides, general: false, runtime: true, refusable: false, tables: tables, files: singleFile(
		query("FindNote", commandOne, "SELECT id, label, position FROM notes WHERE label = {label};", scenarioPlain, label),
		query("RecordNotes", commandBatch, "INSERT INTO notes (id, label, position) VALUES ({id}, {label}, {position});", scenarioBatch,
			integer(columnID), label, position),
	)}
}

// jsonCase returns statements writing a JSON document: one row, and a batch of rows.
//
// Returns corpusCase which is the case.
func jsonCase() corpusCase {
	tables := []corpusTable{{name: tableDocuments, quoted: false, columns: []corpusColumn{
		{name: columnID, kind: columnInteger, nullable: false},
		{name: columnBody, kind: columnJSON, nullable: true},
	}}}
	body := corpusParameter{name: columnBody, kind: columnJSON, slice: false, optional: false}
	return corpusCase{name: caseJSON, general: false, runtime: true, refusable: false, tables: tables, files: singleFile(
		query("FileDocument", commandExec, "INSERT INTO documents (id, body) VALUES ({id}, {body});", scenarioPlain, integer(columnID), body),
		query("FileDocuments", commandBatch, "INSERT INTO documents (body, id) VALUES ({body}, {id});", scenarioBatch,
			body, integer(columnID)),
	)}
}

// jsonReadsCase returns queries reading a column of JSON documents: one row, many rows,
// and a stream of rows.
//
// Returns corpusCase which is the case.
func jsonReadsCase() corpusCase {
	tables := []corpusTable{{name: tableDocuments, quoted: false, columns: []corpusColumn{
		{name: columnID, kind: columnInteger, nullable: false},
		{name: columnBody, kind: columnJSON, nullable: true},
	}}}
	return corpusCase{name: caseJSONReads, general: false, runtime: true, refusable: false, tables: tables, files: singleFile(
		query("FindDocument", commandOne, "SELECT id, body FROM documents WHERE id = {id};", scenarioPlain, integer(columnID)),
		query("ListDocuments", commandMany, "SELECT id, body FROM documents WHERE id > {id};", scenarioPlain, integer(columnID)),
		query("StreamDocuments", "stream", "SELECT id, body FROM documents WHERE id < {id};", scenarioPlain, integer(columnID)),
	)}
}

// nestedEnumCase returns queries over a table whose column holds the enum in a nested
// shape: a lookup reading the column, and a write and a batch binding it. An emitter
// whose driver cannot take the shape may refuse the case under Q059.
//
// Takes name (string) which is the case's name.
// Takes column (string) which names the column and its parameter.
// Takes label (string) which ends the queries' names.
// Takes kind (columnKind) which is the column's shape.
//
// Returns corpusCase which is the case.
func nestedEnumCase(name, column, label string, kind columnKind) corpusCase {
	tables := []corpusTable{{name: tableMoodSets, quoted: false, columns: []corpusColumn{
		{name: columnID, kind: columnInteger, nullable: false},
		{name: column, kind: kind, nullable: false},
	}}}
	value := corpusParameter{name: column, kind: kind, slice: false, optional: false}
	return corpusCase{name: name, general: false, runtime: true, refusable: true, tables: tables, files: singleFile(
		query("Get"+label, commandOne, "SELECT id, "+column+" FROM moodsets WHERE id = {id};", scenarioPlain, integer(columnID)),
		query("Set"+label, commandExec, "INSERT INTO moodsets (id, "+column+") VALUES ({id}, {"+column+"});", scenarioPlain,
			integer(columnID), value),
		query("Record"+label, commandBatch, "INSERT INTO moodsets ("+column+", id) VALUES ({"+column+"}, {id});", scenarioBatch,
			value, integer(columnID)),
	)}
}

// searchOrdered returns a runtime-built query whose base SQL orders and limits, the limit
// a parameter a count need not bind.
//
// Returns corpusQuery which is the query.
func searchOrdered() corpusQuery {
	ordered := builderQuery("SearchOrdered", "SELECT id, kind FROM items ORDER BY id LIMIT {page}", integer("page"))
	ordered.uncounted = []string{"page"}
	return ordered
}

// reservedParametersCase returns queries whose parameters are named like the locals,
// receiver and imports of generated method bodies.
//
// Returns corpusCase which is the case.
func reservedParametersCase() corpusCase {
	names := []string{"queries", "row", "rows", "ctx", "err", "query", "args", "context", "sql", "uuid"}
	commands := []string{commandMany, commandOne, commandMany, commandExec, commandOne, commandMany, commandExec, commandMany, commandOne, commandExec}
	queries := make([]corpusQuery, 0, len(names))
	for index, name := range names {
		queries = append(queries, query("By"+capitalised(name), commands[index], "SELECT id FROM items WHERE rank = {"+name+"};",
			scenarioNone, integer(name)))
		if commands[index] == commandExec {
			queries[index].variants = []string{"DELETE FROM items WHERE rank = {" + name + "};"}
		}
	}
	return corpusCase{name: caseReservedParameters, general: false, runtime: false, refusable: false, tables: nil, files: singleFile(queries...)}
}

// reservedTablesCase returns queries over tables named like the generated package's own
// types, the interface of the query methods among them.
//
// Returns corpusCase which is the case.
func reservedTablesCase() corpusCase {
	tables := slices.Clone(standardTables)
	for _, name := range []string{"queries", "DBTX", "json", corpusInterfaceName} {
		tables = append(tables, corpusTable{name: name, quoted: true, columns: []corpusColumn{{name: columnID, kind: columnInteger, nullable: false}}})
	}
	return corpusCase{name: caseReservedTables, general: false, runtime: false, refusable: false, tables: tables, files: singleFile(
		query("ListQueries", commandMany, "SELECT id FROM [[queries]];", scenarioNone),
		query("ListDBTX", commandMany, "SELECT id FROM [[DBTX]];", scenarioNone),
		query("ListJSON", commandMany, "SELECT id FROM [[json]];", scenarioNone),
		query("ListQuerier", commandMany, "SELECT id FROM [["+corpusInterfaceName+"]];", scenarioNone),
	)}
}

// groupedColumnsCase returns a grouped query whose flat columns share a name.
//
// Returns corpusCase which is the case.
func groupedColumnsCase() corpusCase {
	grouped := query("ListOrders", commandMany,
		"SELECT orders.id, orders.total, items.id, i.id, i.kind FROM orders JOIN items ON items.id = orders.item_id JOIN items i ON i.id = orders.item_id;",
		scenarioNone)
	grouped.options = "group_by: orders.id"
	grouped.headers = []string{"embed(items, from: i)"}
	return corpusCase{name: caseGroupedColumns, general: false, runtime: false, refusable: false, tables: nil, files: singleFile(grouped)}
}

// byteCaseName names the case of a byte class.
//
// Takes class (byteClass) which is the class.
//
// Returns string which is the case's name.
func byteCaseName(class byteClass) string {
	return "bytes" + class.name
}

// byteCases returns one case for each byte class, a query comparing with a literal that
// holds the bytes.
//
// Returns []corpusCase which holds the cases.
func byteCases() []corpusCase {
	cases := make([]corpusCase, 0, len(byteClasses))
	for _, class := range byteClasses {
		cases = append(cases, corpusCase{name: byteCaseName(class), general: false, runtime: false, refusable: false, tables: nil,
			files: singleFile(query("Bytes", commandMany, "SELECT id FROM items WHERE name = '"+class.content+"';", scenarioNone))})
	}
	return cases
}

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

package fuzzbuilder

import (
	"fmt"
	"strings"
)

const (
	// engineClickHouse names the ClickHouse dialect.
	engineClickHouse = "clickhouse"

	// engineSQLite names the SQLite dialect.
	engineSQLite = "sqlite"

	// engineMySQL names the MySQL dialect.
	engineMySQL = "mysql"

	// engineMariaDB names the MariaDB dialect.
	engineMariaDB = "mariadb"

	// enginePostgres names the PostgreSQL dialect.
	enginePostgres = "postgres"

	// engineTimescale names the TimescaleDB dialect.
	engineTimescale = "timescaledb"

	// engineCockroach names the CockroachDB dialect.
	engineCockroach = "cockroachdb"

	// engineDuckDB names the DuckDB dialect.
	engineDuckDB = "duckdb"

	// decimalBase is the base of integer literals.
	decimalBase = 10

	// columnSeparator separates SQL list items.
	columnSeparator = ", "

	// builderTable names the modelled table.
	builderTable = "fuzz_builder"

	// peerTable names the table the join and compound queries read.
	peerTable = "fuzz_builder_peer"
)

// field identifies one modelled value a query projects.
type field uint8

const (
	// fieldID is the unique row identifier.
	fieldID field = iota

	// fieldGroup is a small repeated category.
	fieldGroup

	// fieldScore is a nullable integer.
	fieldScore

	// fieldLabel is a short lower-case text.
	fieldLabel

	// fieldMixed is the integer column named Mixed.
	fieldMixed

	// fieldSelect is the text column named after a reserved word.
	fieldSelect

	// fieldDotted is the nullable integer column whose name holds a dot.
	fieldDotted

	// fieldNote is the nullable peer text a LEFT JOIN reads.
	fieldNote

	// fieldScoreText is the score converted to text.
	fieldScoreText

	// fieldTotal is a computed or aggregated integer.
	fieldTotal
)

// queryKind identifies one catalogue query's evaluation rule.
type queryKind uint8

const (
	// kindPlain selects every column, including the awkwardly named ones.
	kindPlain queryKind = iota

	// kindAliased renames columns to mixed-case and reserved aliases.
	kindAliased

	// kindCast converts and computes columns.
	kindCast

	// kindParameter holds its own parameter in a disjunctive WHERE.
	kindParameter

	// kindOrdered holds its own ORDER BY.
	kindOrdered

	// kindGrouped groups and counts.
	kindGrouped

	// kindDistinct removes duplicate rows.
	kindDistinct

	// kindDerived reads aliases through a derived table.
	kindDerived

	// kindCommon reads a quoted alias through a common table expression.
	kindCommon

	// kindUnion concatenates two branches.
	kindUnion

	// kindJoined reads a nullable column through a LEFT JOIN.
	kindJoined

	// queryCount bounds the catalogue.
	queryCount
)

// column describes one output column of a catalogue query.
type column struct {
	// name is the output name the builder's allow-list must use.
	name string

	// source names the query's underlying column when its output name differs.
	source string

	// value selects the modelled value.
	value field

	// text reports a textual Go value.
	text bool

	// refused reports a computed column the builder must not filter or order by.
	refused bool
}

// query describes one catalogue entry and the builder methods it must have.
type query struct {
	// name is the generated method name.
	name string

	// columns lists the output columns in order.
	columns []column

	// kind selects the evaluation rule.
	kind queryKind

	// where reports that the builder must offer Where.
	where bool
}

// catalogue lists every query in a stable order that inputs index.
var catalogue = [queryCount]query{
	{name: "BuilderPlain", kind: kindPlain, where: true, columns: []column{
		integerColumn("id", fieldID), integerColumn("grp", fieldGroup), integerColumn("score", fieldScore),
		textColumn("label", fieldLabel), integerColumn("Mixed", fieldMixed), textColumn("select", fieldSelect),
		integerColumn("a.b", fieldDotted)}},
	{name: "BuilderAliased", kind: kindAliased, where: true, columns: []column{
		aliasColumn("ident", "id", fieldID, false), aliasColumn("Group", "grp", fieldGroup, false),
		aliasColumn("points", "score", fieldScore, false), aliasColumn("order", "label", fieldLabel, true)}},
	{name: "BuilderCast", kind: kindCast, where: true, columns: []column{
		integerColumn("id", fieldID), computedColumn("score_text", fieldScoreText, true),
		aliasColumn("grp_same", "grp", fieldGroup, false), computedColumn("total", fieldTotal, false)}},
	{name: "BuilderParameter", kind: kindParameter, where: true, columns: []column{
		integerColumn("id", fieldID), integerColumn("grp", fieldGroup), integerColumn("score", fieldScore)}},
	{name: "BuilderOrdered", kind: kindOrdered, where: true, columns: []column{
		integerColumn("id", fieldID), integerColumn("grp", fieldGroup), integerColumn("score", fieldScore)}},
	{name: "BuilderGrouped", kind: kindGrouped, where: true, columns: []column{
		integerColumn("grp", fieldGroup), computedColumn("total", fieldTotal, false)}},
	{name: "BuilderDistinct", kind: kindDistinct, where: true, columns: []column{
		integerColumn("grp", fieldGroup), textColumn("label", fieldLabel)}},
	{name: "BuilderDerived", kind: kindDerived, where: true, columns: []column{
		aliasColumn("ident", "id", fieldID, false), aliasColumn("points", "score", fieldScore, false)}},
	{name: "BuilderCommon", kind: kindCommon, where: true, columns: []column{
		integerColumn("id", fieldID), aliasColumn("Points", "score", fieldScore, false)}},
	{name: "BuilderUnion", kind: kindUnion, where: false, columns: []column{
		integerColumn("id", fieldID), integerColumn("score", fieldScore)}},
	{name: "BuilderJoined", kind: kindJoined, where: true, columns: []column{
		integerColumn("id", fieldID), textColumn("note", fieldNote)}},
}

// integerColumn describes a plain integer output column.
//
// Takes name (string) which is the output and source name.
// Takes value (field) which selects the modelled value.
//
// Returns column which is filterable and orderable.
func integerColumn(name string, value field) column {
	return column{name: name, source: "", value: value, text: false, refused: false}
}

// textColumn describes a plain text output column.
//
// Takes name (string) which is the output and source name.
// Takes value (field) which selects the modelled value.
//
// Returns column which is filterable and orderable.
func textColumn(name string, value field) column {
	return column{name: name, source: "", value: value, text: true, refused: false}
}

// aliasColumn describes a column renamed by an alias.
//
// Takes name (string) which is the alias.
// Takes source (string) which is the underlying column the alias hides.
// Takes value (field) which selects the modelled value.
// Takes text (bool) which reports a textual value.
//
// Returns column which is filterable by its alias only.
func aliasColumn(name, source string, value field, text bool) column {
	return column{name: name, source: source, value: value, text: text, refused: false}
}

// computedColumn describes a converted, computed or aggregated column.
//
// Takes name (string) which is the alias.
// Takes value (field) which selects the modelled value.
// Takes text (bool) which reports a textual value.
//
// Returns column which the documented allow-list leaves out.
func computedColumn(name string, value field, text bool) column {
	return column{name: name, source: "", value: value, text: text, refused: true}
}

// Supported reports whether the package models an engine.
//
// Takes engine (string) which names the dialect.
//
// Returns bool which is true for the eight modelled dialects.
func Supported(engine string) bool {
	switch engine {
	case enginePostgres, engineTimescale, engineCockroach, engineMySQL, engineMariaDB, engineSQLite, engineDuckDB, engineClickHouse:
		return true
	default:
		return false
	}
}

// Schema returns the modelled tables in the engine's syntax.
//
// Takes engine (string) which names the dialect.
//
// Returns []string which holds one CREATE TABLE statement per table.
func Schema(engine string) []string {
	integer, nullable, text, nullableText, suffix := "BIGINT NOT NULL", "BIGINT", "TEXT NOT NULL", "TEXT", ""
	switch engine {
	case engineSQLite:
		integer, nullable = "INTEGER NOT NULL", "INTEGER"
	case engineClickHouse:
		integer, nullable, text, nullableText, suffix = "Int64", "Nullable(Int64)", "String", "Nullable(String)", " ENGINE = Memory"
	}
	quote := quoter(engine)
	return []string{
		fmt.Sprintf("CREATE TABLE %s (id %s, grp %s, score %s, label %s, %s %s, %s %s, %s %s)%s;", builderTable, integer, integer, nullable, text,
			quote("Mixed"), integer, quote("select"), text, quote("a.b"), nullable, suffix),
		fmt.Sprintf("CREATE TABLE %s (id %s, note %s)%s;", peerTable, integer, nullableText, suffix),
	}
}

// Queries returns the catalogue as one query file in the engine's syntax.
//
// Takes engine (string) which names the dialect.
//
// Returns string which holds every catalogue query with its directive.
func Queries(engine string) string {
	var file strings.Builder
	for index := range catalogue {
		entry := &catalogue[index]
		fmt.Fprintf(&file, "-- sgl.query(%s, many, dynamic: runtime)\n%s;\n\n", entry.name, querySQL(engine, entry.kind))
	}
	return file.String()
}

// QueryCount returns the number of catalogue queries.
//
// Returns int which is the number of generated builder methods required.
func QueryCount() int {
	return int(queryCount)
}

// querySQL writes one catalogue query.
//
// Takes engine (string) which names the dialect.
// Takes kind (queryKind) which selects the query.
//
// Returns string which is the query without its terminator.
func querySQL(engine string, kind queryKind) string {
	quote := quoter(engine)
	from := " FROM " + builderTable
	switch kind {
	case kindPlain:
		return "SELECT id, grp, score, label, " + quote("Mixed") + columnSeparator + quote("select") + columnSeparator + quote("a.b") + from
	case kindAliased:
		return "SELECT id AS ident, grp AS " + quote("Group") + ", score AS points, label AS " + quote("order") + from
	case kindCast:
		textType, integerType := castTypes(engine)
		return "SELECT id, CAST(score AS " + textType + ") AS score_text, CAST(grp AS " + integerType + ") AS grp_same, score + grp AS total" + from
	case kindParameter:
		return "SELECT id, grp, score" + from + " WHERE grp = " + placeholder(engine) + " OR score IS NULL"
	case kindOrdered:
		return "SELECT id, grp, score" + from + " ORDER BY grp DESC"
	case kindGrouped:
		return "SELECT grp, COUNT(*) AS total" + from + " GROUP BY grp"
	case kindDistinct:
		return "SELECT DISTINCT grp, label" + from
	case kindDerived:
		return "SELECT d.ident, d.points FROM (SELECT id AS ident, score AS points" + from + ") AS d"
	case kindCommon:
		return "WITH c AS (SELECT id, score AS " + quote("Points") + from + ") SELECT c.id, c." + quote("Points") + " FROM c"
	case kindUnion:
		return "SELECT id, score" + from + " UNION ALL SELECT id, NULL FROM " + peerTable
	default:
		return "SELECT b.id, p.note FROM " + builderTable + " b LEFT JOIN " + peerTable + " p ON p.id = b.id"
	}
}

// castTypes names the engine's converting text type and its value-preserving integer
// type.
//
// Takes engine (string) which names the dialect.
//
// Returns textType (string) which converts an integer to text.
// Returns integerType (string) which keeps a 64-bit integer unchanged.
func castTypes(engine string) (textType, integerType string) {
	switch engine {
	case engineMySQL, engineMariaDB:
		return "CHAR", "SIGNED"
	case engineClickHouse:
		return "Nullable(String)", "Int64"
	case engineSQLite:
		return "TEXT", "INTEGER"
	default:
		return "TEXT", "BIGINT"
	}
}

// placeholder writes the engine's first parameter.
//
// Takes engine (string) which names the dialect.
//
// Returns string which is the native placeholder of an integer parameter.
func placeholder(engine string) string {
	switch engine {
	case engineMySQL, engineMariaDB:
		return "?"
	case engineSQLite:
		return "?1"
	case engineClickHouse:
		return "{grp:Int64}"
	default:
		return "$1"
	}
}

// quoter returns the engine's identifier quoting.
//
// Takes engine (string) which names the dialect.
//
// Returns func(string) string which quotes one identifier.
func quoter(engine string) func(string) string {
	if engine == engineMySQL || engine == engineMariaDB {
		return func(name string) string { return "`" + name + "`" }
	}
	return func(name string) string { return `"` + name + `"` }
}

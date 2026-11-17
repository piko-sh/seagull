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

// Package fuzzcase supplies deterministic inputs independent of the SQL compiler.
package fuzzcase

import (
	"fmt"
	"strconv"
	"strings"
)

// MaxBytes bounds a structured input.
const MaxBytes = 4096

// Version is the stable decoder version stored in corpus inputs.
const Version = 1

// shapeCount is the number of permanently assigned query shapes.
const shapeCount = shapeNotIn + 1

const (
	// shapeSelect selects the select query family.
	shapeSelect = iota

	// shapeBoolean selects the boolean query family.
	shapeBoolean

	// shapeDistinct selects the distinct query family.
	shapeDistinct

	// shapeGrouped selects the grouped query family.
	shapeGrouped

	// shapeAggregate selects the aggregate query family.
	shapeAggregate

	// shapeLeftJoin selects the leftjoin query family.
	shapeLeftJoin

	// shapeInnerJoin selects the innerjoin query family.
	shapeInnerJoin

	// shapeWindow selects the window query family.
	shapeWindow

	// shapeUnion selects the union query family.
	shapeUnion

	// shapeCTE selects the cte query family.
	shapeCTE

	// shapeCorrelated selects the correlated query family.
	shapeCorrelated

	// shapeIn selects the in query family.
	shapeIn

	// shapeCase selects the case query family.
	shapeCase

	// shapeStar selects the star query family.
	shapeStar

	// shapeInsert selects the insert query family.
	shapeInsert

	// shapeUpdate selects the update query family.
	shapeUpdate

	// shapeDelete selects the delete query family.
	shapeDelete

	// shapeHaving selects the having query family.
	shapeHaving

	// shapeDerived selects the derived query family.
	shapeDerived

	// shapeSetReturning selects the setreturning query family.
	shapeSetReturning

	// shapeQualify selects the qualify query family.
	shapeQualify

	// shapeSettings selects the settings query family.
	shapeSettings

	// shapeCoalesce selects the coalesce query family.
	shapeCoalesce

	// shapeNotIn selects the notin query family.
	shapeNotIn
)

// maxRows bounds each input table.
const maxRows = 8

// categoryCount bounds repeated category values.
const categoryCount = 3

// rowCountByte locates the row count.
const rowCountByte = 3

// valueByte locates the first parameter value.
const valueByte = 4

// limitByte locates the pagination limit.
const limitByte = 6

// offsetByte locates the pagination offset.
const offsetByte = 7

// scoreRange bounds nullable score values.
const scoreRange = 13

// scoreOffset allows negative scores.
const scoreOffset = 4

// parameterRange bounds parameter magnitudes.
const parameterRange = 11

// seedLower seeds a lower predicate value.
const seedLower = 3

// seedUpper seeds a distinct upper predicate value.
const seedUpper = 7

// tableColumns records the model table width.
const tableColumns = 3

const (
	// engineClickHouse names the ClickHouse dialect.
	engineClickHouse = "clickhouse"

	// engineDuckDB names the DuckDB dialect.
	engineDuckDB = "duckdb"

	// enginePostgres names the PostgreSQL dialect.
	enginePostgres = "postgres"

	// engineCockroach names the CockroachDB dialect, which qualifies index drops by table.
	engineCockroach = "cockroachdb"

	// engineTimescale names the TimescaleDB dialect.
	engineTimescale = "timescaledb"

	// engineSQLite names the SQLite dialect.
	engineSQLite = "sqlite"

	// engineMySQL names the dialect without conditional CREATE INDEX syntax.
	engineMySQL = "mysql"

	// engineMariaDB names the dialect that preserves compound unique keys.
	engineMariaDB = "mariadb"
)

// selectID starts single-column projections.
const selectID = "SELECT i.id"

// columnSeparator separates SQL columns.
const columnSeparator = ", "

// Model describes a bounded SQL case and its data.
type Model struct {
	// Engine names the SQL dialect.
	Engine string

	// Data retains the bounded bytes used to choose values and operations.
	Data []byte

	// Shape identifies a permanently assigned query family.
	Shape int

	// Flags selects pagination, comments and predicate variants.
	Flags byte

	// Rows is the number of rows in each input table.
	Rows int
}

// Query holds SQL and independent expectations for one model.
type Query struct {
	// SQL includes the Seagull query directive.
	SQL string

	// Reference contains literal values instead of placeholders.
	Reference string

	// CountReference returns the rows that Count must count.
	CountReference string

	// Values holds parameter values in input binding order.
	Values []Value

	// Occurrences records the value at every placeholder occurrence.
	Occurrences []Value

	// Columns is the expected number of output columns.
	Columns int

	// Writes reports a data mutation.
	Writes bool

	// Ordered reports ordering with a unique key in the modelled data.
	Ordered bool
}

// Migrations returns the schema used by the model.
//
// Returns []string which holds ordered SQL statements.
func (m Model) Migrations() []string {
	integer, nullable, suffix := scratchColumnTypes(m.Engine)
	statements := []string{
		fmt.Sprintf("CREATE TABLE fuzz_items (id %s NOT NULL, score %s, category %s NOT NULL)%s;", integer, nullable, integer, suffix),
		fmt.Sprintf("CREATE TABLE fuzz_peers (id %s NOT NULL, score %s)%s;", integer, nullable, suffix),
	}
	if m.at(0) == valueVersion {
		statements = append(statements, m.valueMigration())
	}
	return statements
}

// Inserts returns deterministic data with duplicates and nullable values.
//
// Returns []string which holds individual INSERT statements.
func (m Model) Inserts() []string {
	var result []string
	for row := range m.Rows {
		score := strconv.Itoa(int(m.at(valueByte+row)%scoreRange) - scoreOffset)
		if row%categoryCount == 0 {
			score = "NULL"
		}
		result = append(result, fmt.Sprintf("INSERT INTO fuzz_items (id, score, category) VALUES (%d, %s, %d)", row+1, score, row%categoryCount))
		if row%2 == 0 {
			result = append(result, fmt.Sprintf("INSERT INTO fuzz_peers (id, score) VALUES (%d, %s)", row+1, score))
		}
	}
	if m.at(0) == valueVersion {
		result = append(result, m.valueInserts()...)
	}
	return result
}

// Query renders the model using independent literal and placeholder writers.
//
// Returns Query which includes reference results and binding expectations.
func (m Model) Query() Query {
	if m.at(0) == valueVersion {
		return m.valueQuery()
	}
	if m.at(0) == grammarVersion {
		return m.grammarQuery(false, false)
	}
	return m.versionOneQuery()
}

// versionOneQuery preserves the original corpus's query and binding interpretation.
//
// Returns Query which follows the original fixed-family decoder.
func (m Model) versionOneQuery() Query {
	values := []int64{int64(m.at(valueByte)%parameterRange) - seedLower, int64(m.at(valueByte+1)%parameterRange) + 2}
	var bindings, occurrences []int64
	seen := make(map[int]bool)
	parameter := func(index int) string {
		value := values[index]
		occurrences = append(occurrences, value)
		if mysqlFamily(m.Engine) {
			bindings = append(bindings, value)
			return "?"
		}
		if !seen[index] {
			seen[index] = true
			bindings = append(bindings, value)
		}
		return integerPlaceholder(m.Engine, index+1)
	}
	literal := func(index int) string { return strconv.FormatInt(values[index], 10) }
	sql, columns, writes := m.body(parameter)
	reference, _, _ := m.body(literal)
	countReference := reference
	if !writes && m.Shape != shapeSettings {
		ending := m.ending(columns)
		sql += ending
		reference += ending
		if m.Shape == shapeUnion || m.Shape == shapeBranchClauses || m.Flags&1 != 0 {
			countReference += ending
		}
	}
	sql += m.trailingSQL(false)
	reference += m.trailingSQL(false)
	countReference += m.trailingSQL(true)

	header := "-- sgl.query(name: FuzzQuery, command: many, dynamic: runtime)\n"
	if writes {
		header = "-- sgl.query(name: FuzzQuery, command: exec)\n"
	}
	return Query{SQL: header + sql + ";", Reference: reference, CountReference: countReference,
		Values: integerValues(bindings), Occurrences: integerValues(occurrences), Columns: columns, Writes: writes,
		Ordered: !writes && m.Shape != shapeSettings && m.Shape != shapeUnion && m.Shape != shapeBranchClauses}
}

// at reads a choice byte, defaulting missing choices to zero.
//
// Takes index (int) which locates the choice.
//
// Returns byte which is the choice.
func (m Model) at(index int) byte {
	if index < len(m.Data) {
		return m.Data[index]
	}
	return 0
}

// ending writes deterministic ordering and optional pagination.
//
// Takes columns (int) which is the output width.
//
// Returns string which is the query suffix.
func (m Model) ending(columns int) string {
	indexes := make([]string, 0, columns)
	for column := range columns {
		indexes = append(indexes, strconv.Itoa(column+1))
	}
	ending := " ORDER BY " + strings.Join(indexes, columnSeparator)
	if m.Flags&1 != 0 {
		ending += fmt.Sprintf(" LIMIT %d OFFSET %d", 1+int(m.at(limitByte)%maxRows), m.at(offsetByte)%categoryCount)
	}
	return ending
}

// body writes one query family with caller-supplied parameter expressions.
//
// Takes parameter (func(int) string) which renders a logical parameter.
//
// Returns string which is the query without outer pagination.
// Returns int which is its output width.
// Returns bool which reports a data mutation.
func (m Model) body(parameter func(int) string) (string, int, bool) {
	if m.at(0) == extendedVersion {
		return m.extendedBody(parameter)
	}
	from := " FROM fuzz_items i"
	switch m.Shape {
	case shapeSelect:
		return "SELECT i.id, i.score" + from + " WHERE i.id >= " + parameter(0), 2, false
	case shapeBoolean:
		return "SELECT i.id, i.category" + from + " WHERE (i.id >= " + parameter(0) + " AND i.category < " + parameter(1) + ") OR i.id = " + parameter(0), 2, false
	case shapeDistinct:
		return "SELECT DISTINCT i.category" + from, 1, false
	case shapeGrouped:
		return "SELECT i.category, COUNT(*) AS total" + from + " GROUP BY i.category", 2, false
	case shapeAggregate:
		return "SELECT COUNT(*) AS total" + from + " WHERE i.id > " + parameter(0), 1, false
	case shapeLeftJoin:
		return "SELECT i.id, p.score" + from + " LEFT JOIN fuzz_peers p ON p.id = i.id", 2, false
	case shapeInnerJoin:
		return "SELECT i.id, p.score" + from + " INNER JOIN fuzz_peers p ON p.id = i.id WHERE i.id > " + parameter(0), 2, false
	case shapeWindow:
		return "SELECT i.id, ROW_NUMBER() OVER (PARTITION BY i.category ORDER BY i.id) AS position" + from, 2, false
	case shapeUnion:
		return "SELECT id FROM fuzz_items UNION ALL SELECT id FROM fuzz_peers", 1, false
	case shapeCTE:
		return "WITH chosen AS (SELECT id, score FROM fuzz_items WHERE id > " + parameter(0) + ") SELECT id, score FROM chosen", 2, false
	case shapeCorrelated:
		return selectID + from + " WHERE EXISTS (SELECT p.id FROM fuzz_peers p WHERE p.id = i.id)", 1, false
	case shapeIn:
		return selectID + from + " WHERE i.id IN (" + parameter(0) + columnSeparator + parameter(1) + ")", 1, false
	default:
		return m.advancedBody(parameter)
	}
}

// advancedBody writes query families with nested expressions or data changes.
//
// Takes parameter (func(int) string) which renders a logical parameter.
//
// Returns string which is SQL without outer pagination.
// Returns int which is the output width.
// Returns bool which reports a data mutation.
func (m Model) advancedBody(parameter func(int) string) (string, int, bool) {
	from := " FROM fuzz_items i"
	switch m.Shape {
	case shapeCase:
		return "SELECT i.id, CASE WHEN i.score IS NULL THEN 0 ELSE i.score END AS score" + from, 2, false
	case shapeStar:
		return "SELECT i.*" + from, tableColumns, false
	case shapeInsert:
		return "INSERT INTO fuzz_items (id, score, category) VALUES (99, " + parameter(0) + columnSeparator + parameter(1) + ")", 0, true
	case shapeUpdate:
		return "UPDATE fuzz_items SET score = " + parameter(0) + " WHERE id > " + parameter(1), 0, true
	case shapeDelete:
		return "DELETE FROM fuzz_items WHERE id > " + parameter(0), 0, true
	case shapeHaving:
		return "SELECT i.category, COUNT(*) AS total" + from + " GROUP BY i.category HAVING COUNT(*) > 1", 2, false
	case shapeDerived:
		return "SELECT chosen.id FROM (SELECT id FROM fuzz_items WHERE id > " + parameter(0) + ") chosen", 1, false
	case shapeSetReturning:
		call := "generate_series(0, 2)"
		if m.Engine == engineDuckDB {
			call = "unnest([0, 1, 2])"
		}
		if m.Engine == engineClickHouse {
			call = "arrayJoin([0, 1, 2])"
		}
		return "SELECT i.id, " + call + " AS expanded" + from, 2, false
	case shapeQualify:
		return "SELECT i.id, ROW_NUMBER() OVER (PARTITION BY i.category ORDER BY i.id) AS position" + from + " QUALIFY position = 1", 2, false
	case shapeSettings:
		return selectID + from + " SETTINGS max_threads = 1", 1, false
	case shapeCoalesce:
		return "SELECT i.id, COALESCE(i.score, 0) + i.category AS score" + from, 2, false
	default:
		return selectID + from + " WHERE i.id NOT IN (" + parameter(0) + columnSeparator + parameter(1) + ")", 1, false
	}
}

// trailingSQL writes engine-specific clauses under the count contract.
//
// Takes count (bool) which removes row locks from count references.
//
// Returns string which holds the required trailing SQL.
func (m Model) trailingSQL(count bool) string {
	if m.Shape == shapeLocking && !count {
		return " FOR UPDATE"
	}
	if m.Engine == engineClickHouse && m.Shape == shapeCorrelated {
		return " SETTINGS allow_experimental_correlated_subqueries = 1"
	}
	return ""
}

// Decode reads a versioned input without consulting a fixture directory.
//
// Takes engine (string) which names the dialect.
// Takes data ([]byte) which holds the version and model choices.
//
// Returns Model which owns a copy of the input.
// Returns bool which is false for an unsupported version, shape or size.
func Decode(engine string, data []byte) (Model, bool) {
	if len(data) == 0 || len(data) > MaxBytes || !knownModelVersion(data[0]) {
		return Model{}, false
	}
	m := Model{Engine: engine, Data: append([]byte(nil), data...), Shape: 0, Flags: 0, Rows: 0}
	m.Shape = int(m.at(1)) % shapeCount
	m.Flags = m.at(2)
	m.Rows = int(m.at(rowCountByte)) % (maxRows + 1)
	if data[0] == grammarVersion {
		m.Shape = int(m.at(1)) % grammarSourceCount
		return m, supportsShape(engine, shapeSelect)
	}
	if data[0] == extendedVersion {
		m.Shape = extendedShapeStart + int(m.at(1))%extendedShapeCount
	}
	if data[0] == valueVersion {
		m.Shape = valueShapeStart + int(m.at(1))%valueShapeCount
	}
	return m, supportsShape(engine, m.Shape)
}

// supportsShape reports whether an engine supports a modelled query family.
//
// Takes engine (string) which names the dialect.
// Takes shape (int) which is a stable query family.
//
// Returns bool which reports support in both the engine and Seagull.
func supportsShape(engine string, shape int) bool {
	capability, ok := capabilities[engine]
	if !ok {
		return false
	}
	switch shape {
	case shapeUpdate, shapeDelete:
		return capability.writes
	case shapeSetReturning, shapeVariableExpansion:
		return capability.expansion
	case shapeLocking:
		return capability.locking
	case shapeSQLiteAffinity:
		return engine == engineSQLite
	case shapeExactValues, shapeNullableValues, shapeBranchClauses, shapeBulkInsert:
		return true
	case shapeQualify:
		return capability.qualify
	case shapeSettings:
		return capability.settings
	default:
		return shape >= 0 && shape < shapeCount
	}
}

// Seeds returns positive examples for every supported query family.
//
// Takes engine (string) which names the dialect.
//
// Returns [][]byte which includes empty, singleton and populated data sets.
func Seeds(engine string) [][]byte {
	var seeds [][]byte
	for shape := range shapeCount {
		if supportsShape(engine, shape) {
			for _, rows := range []byte{0, 1, maxRows} {
				seeds = append(seeds, []byte{Version, byte(shape), rows % categoryCount, rows, seedLower, seedUpper, 1, 2, scoreOffset})
			}
		}
	}
	seeds = append(seeds, grammarSeeds(engine)...)
	seeds = append(seeds, extendedSeeds(engine)...)
	return append(seeds, valueSeeds(engine)...)
}

// mysqlFamily reports whether an engine speaks the MySQL protocol and SQL dialect.
//
// Takes engine (string) which names the dialect.
//
// Returns bool which is true for MySQL and MariaDB.
func mysqlFamily(engine string) bool {
	return engine == engineMySQL || engine == engineMariaDB
}

// postgresFamily reports whether an engine speaks the PostgreSQL dialect.
//
// Takes engine (string) which names the dialect.
//
// Returns bool which is true for PostgreSQL, CockroachDB and TimescaleDB.
func postgresFamily(engine string) bool {
	return engine == enginePostgres || engine == engineCockroach || engine == engineTimescale
}

// knownModelVersion reports whether a version byte selects a Model decoder.
//
// Takes version (byte) which is the first input byte.
//
// Returns bool which is true for the original, grammar, extended and value versions.
func knownModelVersion(version byte) bool {
	return version == Version || version == grammarVersion || version == extendedVersion || version == valueVersion
}

// scratchColumnTypes names the integer column types and table suffix of a fixture table.
//
// Takes engine (string) which names the dialect.
//
// Returns integer (string) which is the non-null integer type.
// Returns nullable (string) which is the nullable integer type.
// Returns suffix (string) which follows the column list.
func scratchColumnTypes(engine string) (integer, nullable, suffix string) {
	switch engine {
	case engineSQLite:
		return "INTEGER", "INTEGER", ""
	case engineClickHouse:
		return "Int64", "Nullable(Int64)", " ENGINE = Memory"
	default:
		return "BIGINT", "BIGINT", ""
	}
}

// integerPlaceholder spells a numbered integer placeholder for an engine.
//
// Takes engine (string) which names the dialect.
// Takes number (int) which is the placeholder number, counted from one.
//
// Returns string which is the native placeholder.
func integerPlaceholder(engine string, number int) string {
	switch engine {
	case engineClickHouse:
		return fmt.Sprintf("{p%d:Int64}", number)
	case engineSQLite:
		return fmt.Sprintf("?%d", number)
	default:
		return fmt.Sprintf("$%d", number)
	}
}

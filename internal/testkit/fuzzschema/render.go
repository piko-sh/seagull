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

package fuzzschema

import (
	"fmt"
	"slices"
	"strings"
)

const (
	// generatedExpression is every generated column's expression.
	generatedExpression = "id * 2"

	// checkExpression is every CHECK constraint's condition.
	checkExpression = "id >= 0"

	// identifier is the integer column every table starts with.
	identifier = "id"

	// notNull is the NOT NULL clause.
	notNull = " NOT NULL"

	// alterTable starts every ALTER TABLE statement.
	alterTable = "ALTER TABLE "

	// mergeTree is the ClickHouse table engine clause.
	mergeTree = " ENGINE = MergeTree"

	// orderByID is the ClickHouse sorting key clause.
	orderByID = " ORDER BY id"
)

// allFamilies lists every type family in a stable order.
var allFamilies = []string{FamilyInteger, FamilyFloat, FamilyDecimal, FamilyBoolean, FamilyText, FamilyDate, FamilyTimestamp,
	FamilyTimestampTZ, FamilyUUID, FamilyJSON, FamilyArray, FamilyBinary, FamilyEnum}

// enumValues holds the values every enum starts with.
var enumValues = []string{"a", "b"}

// postgresTypes spells each family on PostgreSQL, TimescaleDB and CockroachDB.
var postgresTypes = map[string]string{FamilyInteger: "BIGINT", FamilyFloat: "DOUBLE PRECISION", FamilyDecimal: "NUMERIC(18,4)",
	FamilyBoolean: "BOOLEAN", FamilyText: "TEXT", FamilyDate: "DATE", FamilyTimestamp: "TIMESTAMP", FamilyTimestampTZ: "TIMESTAMPTZ",
	FamilyUUID: "UUID", FamilyJSON: "JSONB", FamilyArray: "BIGINT[]", FamilyBinary: "BYTEA"}

// mysqlTypes spells each family on MySQL and MariaDB.
var mysqlTypes = map[string]string{FamilyInteger: "BIGINT", FamilyFloat: "DOUBLE", FamilyDecimal: "DECIMAL(18,4)",
	FamilyBoolean: "BOOLEAN", FamilyText: "VARCHAR(64)", FamilyDate: "DATE", FamilyTimestamp: "DATETIME(6)", FamilyUUID: "UUID",
	FamilyJSON: "JSON", FamilyBinary: "VARBINARY(64)"}

// sqliteTypes spells each family on SQLite.
var sqliteTypes = map[string]string{FamilyInteger: "INTEGER", FamilyFloat: "REAL", FamilyDecimal: "NUMERIC(18,4)",
	FamilyBoolean: "BOOLEAN", FamilyText: "TEXT", FamilyDate: "DATE", FamilyTimestamp: "TIMESTAMP", FamilyBinary: "BLOB"}

// duckdbTypes spells each family on DuckDB.
var duckdbTypes = map[string]string{FamilyInteger: "BIGINT", FamilyFloat: "DOUBLE", FamilyDecimal: "DECIMAL(18,4)",
	FamilyBoolean: "BOOLEAN", FamilyText: "VARCHAR", FamilyDate: "DATE", FamilyTimestamp: "TIMESTAMP", FamilyTimestampTZ: "TIMESTAMPTZ",
	FamilyUUID: "UUID", FamilyJSON: "JSON", FamilyArray: "BIGINT[]", FamilyBinary: "BLOB"}

// clickhouseTypes spells each family on ClickHouse.
var clickhouseTypes = map[string]string{FamilyInteger: "Int64", FamilyFloat: "Float64", FamilyDecimal: "Decimal(18, 4)",
	FamilyBoolean: "Bool", FamilyText: "String", FamilyDate: "Date32", FamilyTimestampTZ: "DateTime64(6, 'UTC')", FamilyUUID: "UUID",
	FamilyArray: "Array(Int64)", FamilyEnum: "Enum8('a' = 1, 'b' = 2)"}

// defaults spells each defaultable family's default literal.
var defaults = map[string]string{FamilyInteger: "7", FamilyFloat: "1.5", FamilyDecimal: "2.5", FamilyBoolean: "TRUE",
	FamilyText: "'x'", FamilyDate: "'2020-01-02'", FamilyTimestamp: "'2020-01-02 03:04:05'", FamilyTimestampTZ: "'2020-01-02 03:04:05+00'",
	FamilyUUID: "'00000000-0000-0000-0000-000000000001'", FamilyEnum: "'a'"}

// families lists the type families an engine's histories use.
//
// Takes engine (string) which names the dialect.
//
// Returns []string which keeps allFamilies' order.
func families(engine string) []string {
	var result []string
	for _, family := range allFamilies {
		if supports(engine, familyFeature(family)) {
			result = append(result, family)
		}
	}
	return result
}

// defaultable reports whether columns of a family take a literal default.
//
// Takes family (string) which is the family.
//
// Returns bool which is true when the model writes defaults for it.
func defaultable(family string) bool {
	_, ok := defaults[family]
	return ok
}

// defaultLiteral spells a family's default on an engine.
//
// Takes engine (string) which names the dialect.
// Takes family (string) which is the family.
//
// Returns string which is the literal.
func defaultLiteral(engine, family string) string {
	if engine == engineClickHouse && family == FamilyTimestampTZ {
		return defaults[FamilyTimestamp]
	}
	if engine == engineClickHouse && family == FamilyBoolean {
		return "true"
	}
	return defaults[family]
}

// isPostgresFamily reports engines speaking PostgreSQL DDL.
//
// Takes engine (string) which names the dialect.
//
// Returns bool which is true for PostgreSQL, TimescaleDB and CockroachDB.
func isPostgresFamily(engine string) bool {
	return engine == enginePostgres || engine == engineTimescale || engine == engineCockroachDB
}

// isMySQLFamily reports engines speaking MySQL DDL.
//
// Takes engine (string) which names the dialect.
//
// Returns bool which is true for MySQL and MariaDB.
func isMySQLFamily(engine string) bool {
	return engine == engineMySQL || engine == engineMariaDB
}

// nativeType spells a column's base type without nullability.
//
// Takes engine (string) which names the dialect.
// Takes target (*column) which is the column.
//
// Returns string which is the native type.
func nativeType(engine string, target *column) string {
	if target.family == FamilyEnum && target.enum != "" {
		return target.enum
	}
	switch {
	case isPostgresFamily(engine):
		return postgresTypes[target.family]
	case isMySQLFamily(engine):
		if target.family == FamilyEnum {
			return "ENUM('a', 'b')"
		}
		return mysqlTypes[target.family]
	case engine == engineSQLite:
		return sqliteTypes[target.family]
	case engine == engineDuckDB:
		return duckdbTypes[target.family]
	default:
		return clickhouseTypes[target.family]
	}
}

// columnType spells a column's type, wrapped in Nullable on ClickHouse.
//
// Takes engine (string) which names the dialect.
// Takes target (*column) which is the column.
//
// Returns string which is the type a definition or MODIFY COLUMN names.
func columnType(engine string, target *column) string {
	native := nativeType(engine, target)
	if engine == engineClickHouse && target.nullable {
		return "Nullable(" + native + ")"
	}
	return native
}

// definition spells a column definition.
//
// Takes engine (string) which names the dialect.
// Takes target (*column) which is the column.
// Takes stored (bool) which stores a generated column where the engine offers a choice.
//
// Returns string which is the definition.
func definition(engine string, target *column, stored bool) string {
	if target.generated {
		return target.name + " " + generatedType(engine, stored)
	}
	text := target.name + " " + columnType(engine, target)
	if target.hasDefault {
		text += " DEFAULT " + defaultLiteral(engine, target.family)
	}
	if !target.nullable && engine != engineClickHouse {
		text += notNull
	}
	return text
}

// generatedType spells the type and generation clause of a generated column.
//
// Takes engine (string) which names the dialect.
// Takes stored (bool) which stores the column where the engine offers a choice.
//
// Returns string which follows the column's name.
func generatedType(engine string, stored bool) string {
	storage := map[bool]string{true: "STORED", false: "VIRTUAL"}[stored]
	switch {
	case engine == enginePostgres || engine == engineTimescale:
		return "BIGINT GENERATED ALWAYS AS (" + generatedExpression + ") STORED"
	case engine == engineCockroachDB:
		return "BIGINT AS (" + generatedExpression + ") " + storage
	case isMySQLFamily(engine):
		return "BIGINT AS (" + generatedExpression + ") " + storage
	case engine == engineSQLite:
		return "INTEGER GENERATED ALWAYS AS (" + generatedExpression + ") " + storage
	case engine == engineDuckDB:
		return "BIGINT GENERATED ALWAYS AS (" + generatedExpression + ") VIRTUAL"
	default:
		if stored {
			return "Int64 MATERIALIZED " + generatedExpression
		}
		return "Int64 ALIAS " + generatedExpression
	}
}

// checkClause spells a named CHECK constraint.
//
// Takes engine (string) which names the dialect.
// Takes name (string) which is the constraint's name.
//
// Returns string which is the constraint clause.
func checkClause(engine, name string) string {
	if engine == engineClickHouse {
		return "CONSTRAINT " + name + " CHECK " + checkExpression
	}
	return "CONSTRAINT " + name + " CHECK (" + checkExpression + ")"
}

// qualified spells a relation's name for a statement.
//
// Takes table (*relation) which is the relation.
//
// Returns string which is the possibly qualified name.
func qualified(table *relation) string {
	return table.key()
}

// list spells names separated by commas.
//
// Takes names ([]string) which are the names.
//
// Returns string which joins them.
func list(names []string) string {
	return strings.Join(names, ", ")
}

// createTable spells CREATE TABLE for a new table and its definitions.
//
// Takes engine (string) which names the dialect.
// Takes table (*relation) which is the table.
// Takes definitions ([]string) which hold the column and constraint definitions.
//
// Returns string which is the statement.
func createTable(engine string, table *relation, definitions []string) string {
	statement := fmt.Sprintf("CREATE TABLE %s (%s)", qualified(table), list(definitions))
	switch {
	case engine == engineClickHouse && table.partitioned:
		return statement + mergeTree + " PARTITION BY id % 4" + orderByID
	case engine == engineClickHouse:
		return statement + mergeTree + orderByID
	case table.partitioned && isMySQLFamily(engine):
		return statement + " PARTITION BY HASH (id) PARTITIONS 2"
	case table.partitioned:
		return statement + " PARTITION BY RANGE (id)"
	default:
		return statement
	}
}

// partitionStatement spells CREATE TABLE for a PostgreSQL range partition.
//
// Takes parent (*relation) which is the partitioned table.
// Takes index (int) which numbers the partition and its range.
//
// Returns string which is the statement.
func partitionStatement(parent *relation, index int) string {
	const width = 1000
	return fmt.Sprintf("CREATE TABLE %s PARTITION OF %s FOR VALUES FROM (%d) TO (%d)", qualify(parent.namespace, partitionName(index)),
		qualified(parent), index*width, (index+1)*width)
}

// sortedCopy returns a sorted copy of names.
//
// Takes names ([]string) which are the names.
//
// Returns []string which is sorted.
func sortedCopy(names []string) []string {
	result := slices.Clone(names)
	slices.Sort(result)
	return result
}

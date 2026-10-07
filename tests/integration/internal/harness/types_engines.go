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

package harness

import (
	"strings"

	"seagull.sh/seagull/sdk/dialect"
)

const (
	// narrowestIntegerWidth is the width in bits of the first integer a sized type table
	// names, each next one doubling it.
	narrowestIntegerWidth = 8

	// narrowestFloatWidth is the width in bits of the first float a sized type table names,
	// the next one doubling it.
	narrowestFloatWidth = 32
)

var (
	// mySQLNumericFunctions are the maths functions the MySQL-family probes apply to each
	// column.
	mySQLNumericFunctions = []string{
		"ABS(%[1]s)", "CEIL(%[1]s)", "CEILING(%[1]s)", "FLOOR(%[1]s)", "ROUND(%[1]s)", "TRUNCATE(%[1]s, 0)",
		"SIGN(%[1]s)", "SQRT(%[1]s)", "EXP(%[1]s)", "LN(%[1]s)", "LOG(%[1]s)", "LOG2(%[1]s)", "LOG10(%[1]s)",
		"POWER(%[1]s, %[1]s)", "POW(%[1]s, %[1]s)", "MOD(%[1]s, %[1]s)",
	}

	// mySQLNumericPairs combine two columns of the MySQL-family fixture.
	mySQLNumericPairs = []string{
		"%[1]s + %[2]s",
		"%[1]s * %[2]s",
		"COALESCE(%[1]s, %[2]s)",
		"IFNULL(%[1]s, %[2]s)",
		"IF(%[1]s > %[2]s, %[1]s, %[2]s)",
		"CASE WHEN %[1]s > %[2]s THEN %[1]s ELSE %[2]s END",
		"GREATEST(%[1]s, %[2]s)",
	}

	// mySQLTypeNames reads the names the MySQL driver gives the fixture's numeric results.
	mySQLTypeNames = map[string]ReportedType{
		"TINYINT":   {Category: dialect.TypeCategoryInteger, Width: 8, Unsigned: false},
		"SMALLINT":  {Category: dialect.TypeCategoryInteger, Width: 16, Unsigned: false},
		"MEDIUMINT": {Category: dialect.TypeCategoryInteger, Width: 24, Unsigned: false},
		"INT":       {Category: dialect.TypeCategoryInteger, Width: 32, Unsigned: false},
		"BIGINT":    {Category: dialect.TypeCategoryInteger, Width: 64, Unsigned: false},
		"FLOAT":     {Category: dialect.TypeCategoryFloat, Width: 32, Unsigned: false},
		"DOUBLE":    {Category: dialect.TypeCategoryFloat, Width: 64, Unsigned: false},
		"DECIMAL":   {Category: dialect.TypeCategoryDecimal, Width: 0, Unsigned: false},
	}

	// duckDBNumericFunctions are the maths functions the DuckDB probes apply to each column.
	duckDBNumericFunctions = []string{
		"abs(%[1]s)", "ceil(%[1]s)", "ceiling(%[1]s)", "floor(%[1]s)", "round(%[1]s)", "trunc(%[1]s)",
		"sign(%[1]s)", "sqrt(%[1]s)", "exp(%[1]s)", "ln(%[1]s)", "log(%[1]s)", "log2(%[1]s)", "log10(%[1]s)",
		"power(%[1]s, %[1]s)", "pow(%[1]s, %[1]s)", "mod(%[1]s, %[1]s)", "gcd(%[1]s, %[1]s)", "lcm(%[1]s, %[1]s)",
	}

	// duckDBNumericPairs combine two columns of the DuckDB fixture.
	duckDBNumericPairs = []string{
		"%[1]s + %[2]s",
		"COALESCE(%[1]s, %[2]s)",
		"CASE WHEN %[1]s > %[2]s THEN %[1]s ELSE %[2]s END",
		"greatest(%[1]s, %[2]s)",
	}

	// duckDBTypeNames reads the names typeof gives the DuckDB fixture's numeric results.
	duckDBTypeNames = sizedNumericTypeNames(
		[]string{"TINYINT", "SMALLINT", "INTEGER", "BIGINT", "HUGEINT"},
		[]string{"UTINYINT", "USMALLINT", "UINTEGER", "UBIGINT"},
		[]string{"FLOAT", "DOUBLE"},
	)

	// sqLiteNumericFunctions are the maths functions the SQLite probes apply to each column.
	sqLiteNumericFunctions = []string{
		"abs(%[1]s)", "ceil(%[1]s)", "ceiling(%[1]s)", "floor(%[1]s)", "round(%[1]s)", "trunc(%[1]s)",
		"sign(%[1]s)", "sqrt(%[1]s)", "exp(%[1]s)", "ln(%[1]s)", "log(%[1]s)", "log2(%[1]s)", "log10(%[1]s)",
		"power(%[1]s, %[1]s)", "pow(%[1]s, %[1]s)", "mod(%[1]s, %[1]s)",
	}

	// sqLiteTypeNames reads the storage classes typeof gives the SQLite fixture's results.
	sqLiteTypeNames = map[string]ReportedType{
		"integer": {Category: dialect.TypeCategoryInteger, Width: 64, Unsigned: false},
		"real":    {Category: dialect.TypeCategoryFloat, Width: 64, Unsigned: false},
	}

	// clickHouseNumericFunctions are the maths functions the ClickHouse probes apply to each
	// column.
	clickHouseNumericFunctions = []string{
		"abs(%[1]s)", "ceil(%[1]s)", "ceiling(%[1]s)", "floor(%[1]s)", "round(%[1]s)", "trunc(%[1]s)",
		"sign(%[1]s)", "sqrt(%[1]s)", "exp(%[1]s)", "log(%[1]s)", "ln(%[1]s)", "log2(%[1]s)", "log10(%[1]s)",
		"pow(%[1]s, %[1]s)", "power(%[1]s, %[1]s)", "mod(%[1]s, %[1]s)", "modulo(%[1]s, %[1]s)",
	}

	// clickHouseNumericPairs combine two columns of the ClickHouse fixture.
	clickHouseNumericPairs = []string{
		"%[1]s + %[2]s",
		"if(%[1]s > %[2]s, %[1]s, %[2]s)",
		"coalesce(%[1]s, %[2]s)",
		"greatest(%[1]s, %[2]s)",
	}

	// clickHouseTypeNames reads the names toTypeName gives the ClickHouse fixture's results.
	clickHouseTypeNames = sizedNumericTypeNames(
		[]string{"Int8", "Int16", "Int32", "Int64", "Int128"},
		[]string{"UInt8", "UInt16", "UInt32", "UInt64"},
		[]string{"Float32", "Float64"},
	)
)

// MySQLNumericSchema returns the statements creating the MySQL-family fixture, with one
// non-null column of each numeric type.
//
// Returns []string which holds the statements.
func MySQLNumericSchema() []string {
	return []string{"CREATE TABLE numbers (i2 SMALLINT NOT NULL, i4 INT NOT NULL, i8 BIGINT NOT NULL, " +
		"f4 FLOAT NOT NULL, f8 DOUBLE NOT NULL, n DECIMAL(12, 3) NOT NULL)"}
}

// MySQLNumericRows returns the statements filling the MySQL-family fixture.
//
// Returns []string which holds the statements.
func MySQLNumericRows() []string {
	return []string{"INSERT INTO numbers VALUES (2, 4, 8, 1.5, 2.5, 3.25)"}
}

// MySQLNumericProbes returns the probe queries over the MySQL-family fixture.
//
// Returns []string which holds the queries.
func MySQLNumericProbes() []string {
	return NumericProbes(numericFixtureTable, postgresNumericColumns, mySQLNumericFunctions, mySQLNumericPairs, true)
}

// MySQLTypeName reads a type name the MySQL driver gives a numeric result, ignoring an
// UNSIGNED prefix the comparison does not read for the MySQL family.
//
// Takes name (string) which is the name.
//
// Returns ReportedType which is the type.
// Returns bool which is false for a name it does not know.
func MySQLTypeName(name string) (ReportedType, bool) {
	reported, known := mySQLTypeNames[strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(name)), "UNSIGNED ")]
	return reported, known
}

// DuckDBNumericSchema returns the statements creating the DuckDB fixture.
//
// Returns []string which holds the statements.
func DuckDBNumericSchema() []string {
	return []string{"CREATE TABLE numbers (i2 SMALLINT NOT NULL, i4 INTEGER NOT NULL, i8 BIGINT NOT NULL, " +
		"f4 REAL NOT NULL, f8 DOUBLE NOT NULL, n DECIMAL(12, 3) NOT NULL)"}
}

// DuckDBNumericRows returns the statements filling the DuckDB fixture.
//
// Returns []string which holds the statements.
func DuckDBNumericRows() []string {
	return []string{"INSERT INTO numbers VALUES (2, 4, 8, 1.5, 2.5, 3.25)"}
}

// DuckDBNumericProbes returns the probe queries over the DuckDB fixture.
//
// Returns []string which holds the queries.
func DuckDBNumericProbes() []string {
	return NumericProbes(numericFixtureTable, postgresNumericColumns, duckDBNumericFunctions, duckDBNumericPairs, true)
}

// DuckDBTypeName reads a type name typeof gives a DuckDB numeric result.
//
// Takes name (string) which is the name.
//
// Returns ReportedType which is the type.
// Returns bool which is false for a name it does not know.
func DuckDBTypeName(name string) (ReportedType, bool) {
	normalised := strings.ToUpper(strings.TrimSpace(name))
	if strings.HasPrefix(normalised, "DECIMAL") {
		return ReportedType{Category: dialect.TypeCategoryDecimal, Width: 0, Unsigned: false}, true
	}
	reported, known := duckDBTypeNames[normalised]
	return reported, known
}

// SQLiteNumericSchema returns the statements creating the SQLite fixture, with an integer
// and a real column for SQLite's two numeric storage classes.
//
// Returns []string which holds the statements.
func SQLiteNumericSchema() []string {
	return []string{"CREATE TABLE numbers (i8 INTEGER NOT NULL, f8 REAL NOT NULL)"}
}

// SQLiteNumericRows returns the statements filling the SQLite fixture.
//
// Returns []string which holds the statements.
func SQLiteNumericRows() []string {
	return []string{"INSERT INTO numbers VALUES (8, 2.5)"}
}

// SQLiteNumericProbes returns the probe queries over the SQLite fixture. They compare
// only functions and arithmetic, since SQLite types COALESCE, CASE and UNION results per
// value.
//
// Returns []string which holds the queries.
func SQLiteNumericProbes() []string {
	return NumericProbes(numericFixtureTable, []string{"i8", "f8"}, sqLiteNumericFunctions, []string{"%[1]s + %[2]s"}, false)
}

// SQLiteTypeName reads a storage class typeof gives a SQLite numeric result.
//
// Takes name (string) which is the storage class.
//
// Returns ReportedType which is the type.
// Returns bool which is false for a storage class it does not know.
func SQLiteTypeName(name string) (ReportedType, bool) {
	reported, known := sqLiteTypeNames[strings.ToLower(strings.TrimSpace(name))]
	return reported, known
}

// ClickHouseNumericSchema returns the statements creating the ClickHouse fixture.
//
// Returns []string which holds the statements.
func ClickHouseNumericSchema() []string {
	return []string{"CREATE TABLE numbers (i2 Int16, i4 Int32, i8 Int64, f4 Float32, f8 Float64, " +
		"n Decimal(12, 3)) ENGINE = Memory"}
}

// ClickHouseNumericRows returns the statements filling the ClickHouse fixture.
//
// Returns []string which holds the statements.
func ClickHouseNumericRows() []string {
	return []string{"INSERT INTO numbers VALUES (2, 4, 8, 1.5, 2.5, 3.25)"}
}

// ClickHouseNumericProbes returns the probe queries over the ClickHouse fixture.
//
// Returns []string which holds the queries.
func ClickHouseNumericProbes() []string {
	return NumericProbes(numericFixtureTable, postgresNumericColumns, clickHouseNumericFunctions, clickHouseNumericPairs, true)
}

// ClickHouseTypeName reads a type name toTypeName gives a ClickHouse numeric result,
// looking through Nullable.
//
// Takes name (string) which is the name.
//
// Returns ReportedType which is the type.
// Returns bool which is false for a name it does not know.
func ClickHouseTypeName(name string) (ReportedType, bool) {
	normalised := strings.TrimSpace(name)
	if inner, nullable := strings.CutPrefix(normalised, "Nullable("); nullable {
		normalised = strings.TrimSuffix(inner, ")")
	}
	if strings.HasPrefix(normalised, "Decimal") {
		return ReportedType{Category: dialect.TypeCategoryDecimal, Width: 0, Unsigned: false}, true
	}
	reported, known := clickHouseTypeNames[normalised]
	return reported, known
}

// sizedNumericTypeNames builds a type table for an engine whose numeric type names grow
// in width by doubling, as DuckDB's and ClickHouse's do.
//
// Takes signed ([]string) which names the signed integers, from 8 bits up.
// Takes unsigned ([]string) which names the unsigned integers, from 8 bits up.
// Takes floats ([]string) which names the floats, from 32 bits up.
//
// Returns map[string]ReportedType which reads each name.
func sizedNumericTypeNames(signed, unsigned, floats []string) map[string]ReportedType {
	names := make(map[string]ReportedType, len(signed)+len(unsigned)+len(floats))
	for index, name := range signed {
		names[name] = ReportedType{Width: narrowestIntegerWidth << index, Category: dialect.TypeCategoryInteger, Unsigned: false}
	}
	for index, name := range unsigned {
		names[name] = ReportedType{Width: narrowestIntegerWidth << index, Category: dialect.TypeCategoryInteger, Unsigned: true}
	}
	for index, name := range floats {
		names[name] = ReportedType{Width: narrowestFloatWidth << index, Category: dialect.TypeCategoryFloat, Unsigned: false}
	}
	return names
}

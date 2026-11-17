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

package fuzztyped

import (
	"math"
	"strings"
	"time"
)

const (
	// enginePostgres names the PostgreSQL dialect.
	enginePostgres = "postgres"

	// engineTimescale names the TimescaleDB dialect.
	engineTimescale = "timescaledb"

	// engineCockroach names the CockroachDB dialect.
	engineCockroach = "cockroachdb"

	// engineMySQL names the MySQL dialect.
	engineMySQL = "mysql"

	// engineMariaDB names the MariaDB dialect.
	engineMariaDB = "mariadb"

	// engineSQLite names the SQLite dialect.
	engineSQLite = "sqlite"

	// engineDuckDB names the DuckDB dialect.
	engineDuckDB = "duckdb"

	// engineClickHouse names the ClickHouse dialect.
	engineClickHouse = "clickhouse"

	// wallLayout spells a wall-clock timestamp with up to nine fractional digits.
	wallLayout = "2006-01-02T15:04:05.999999999"

	// microseconds is the number of nanoseconds in a microsecond.
	microseconds = int64(time.Microsecond)

	// longTextRepeats makes the long text value 64 KiB.
	longTextRepeats = 8192
)

// family describes one value family: its column, its boundary values and the rules that
// give the value each engine returns and the rows an equality filter admits.
type family struct {
	// native gives the column type, empty where the engine has none.
	native func(engine string) string

	// stored gives the value a round trip returns, false when the engine refuses it.
	stored func(engine string, value Value) (Value, bool)

	// equal reports whether a stored value equals a filter value under the engine's rules.
	equal func(engine string, stored, filter Value) bool

	// name is the method and table suffix.
	name string

	// excluded explains, per engine, why the family is left out.
	excluded map[string]string

	// values lists the boundary values inputs choose from.
	values []Value

	// filtered reports that the family runs the WHERE and JOIN parameter checks.
	filtered bool

	// required reports a NOT NULL column, which receives an empty value for NULL.
	required bool
}

// postgresFamily lists the engines that share PostgreSQL's types.
var postgresFamily = []string{enginePostgres, engineTimescale, engineCockroach}

// families lists every value family in a stable order inputs index.
var families = []family{
	{name: "Float", native: floatType, stored: storedFloat, equal: equalFloat, excluded: nil, filtered: true, required: false,
		values: []Value{Float(0), Float(math.Copysign(0, -1)), Float(1.5), Float(-2.25), Float(0.1), Float(math.MaxFloat64),
			Float(math.SmallestNonzeroFloat64), Float(-1e-300), Float(math.NaN()), Float(math.Inf(1)), Float(math.Inf(-1)), Null()}},
	{name: "Wall", native: wallType, stored: storedWall, equal: equalExact, excluded: nil, filtered: true, required: false,
		values: []Value{wall("1970-01-01T00:00:00"), wall("1969-12-31T23:59:59.999999"), wall("1969-12-31T23:59:59.9999995"),
			wall("2024-02-29T12:34:56.123456789"), wall("1900-01-01T00:00:00"), wall("2262-04-11T23:47:16.854775807"),
			wall("1000-01-01T00:00:00.5"), wall("9999-12-31T23:59:59.999999"), wall("2038-01-19T03:14:08"), Null()}},
	{name: "Instant", native: instantType, stored: storedInstant, equal: equalExact, filtered: true, required: false,
		excluded: map[string]string{engineSQLite: "SQLite has no zoned timestamp type. The drivers store text."},
		values: []Value{instant("2024-01-01T00:30:00.123456789+14:00"), instant("2023-12-31T23:59:59-12:00"), instant("1969-12-31T23:59:59.9999995Z"),
			instant("1970-01-01T00:00:01+05:45"), instant("2038-01-19T03:14:07.999999Z"), instant("1950-06-15T08:00:00.000001-03:30"),
			instant("2262-04-11T23:47:16.854775807Z"), Null()}},
	{name: "Text", native: textType, stored: storedText, equal: equalExact, excluded: nil, filtered: true, required: false,
		values: []Value{Text(""), Text("a"), Text("a b "), Text("tab\tline\nfeed\r"), Text("\x01\x1f\x7f"), Text("a\x00b"),
			Text("\U0001F600\U0001D11E"), Text("é"), Text(strings.Repeat("seagull ", longTextRepeats)), Text("AbC"), Null()}},
	{name: "Decimal", native: decimalType, stored: storedSame, equal: equalExact, filtered: true, required: false,
		excluded: map[string]string{engineSQLite: "SQLite numeric affinity keeps no exact 38-digit decimal."},
		values: []Value{Scalar(KindDecimal, "0"), Scalar(KindDecimal, "1.5"), Scalar(KindDecimal, "-0.0000000001"),
			Scalar(KindDecimal, "9999999999999999999999999999.9999999999"), Scalar(KindDecimal, "-9999999999999999999999999999.9999999999"),
			Scalar(KindDecimal, "12345678901234567890.0123456789"), Scalar(KindDecimal, "0.1"), Null()}},
	{name: "Array", native: arrayType, stored: storedArray, equal: equalExact, filtered: false, required: false,
		excluded: arrayExclusions(""),
		values: []Value{Null(), List(), List(Null()), List(Integer(1), Null(), Integer(-3)), List(Integer(math.MaxInt64), Integer(math.MinInt64)),
			List(Integer(0))}},
	{name: "Required", native: arrayType, stored: storedArray, equal: equalExact, filtered: false, required: true,
		excluded: arrayExclusions(""),
		values:   []Value{Null(), List(), List(Integer(7)), List(Integer(1), Null(), Integer(-3))}},
	{name: "Nested", native: nestedType, stored: storedArray, equal: equalExact, filtered: false, required: false,
		excluded: arrayExclusions("CockroachDB has no arrays of arrays."),
		values: []Value{List(), List(List(Integer(1), Integer(2)), List(Integer(3), Integer(4))), List(List(Null())), List(List(Integer(5))),
			List(List(Integer(1)), List(Integer(2), Integer(3))), List(List()), Null()}},
	{name: "Enum", native: enumType, stored: storedSame, equal: equalExact, filtered: true, required: false,
		excluded: map[string]string{engineSQLite: "SQLite has no enumerated type."},
		values:   []Value{Text("sad"), Text("ok"), Text("happy"), Null()}},
}

// wall builds a wall-clock timestamp value.
//
// Takes text (string) which is the timestamp in wallLayout.
//
// Returns Value which holds it.
func wall(text string) Value {
	return Scalar(KindTime, text)
}

// instant builds an instant from RFC 3339 text with any offset.
//
// Takes text (string) which is the instant with its offset.
//
// Returns Value which holds the instant with the offset the input names.
func instant(text string) Value {
	return Scalar(KindInstant, text)
}

// arrayExclusions lists the engines without one-dimensional arrays.
//
// Takes cockroach (string) which explains a CockroachDB exclusion, empty when none.
//
// Returns map[string]string which explains each exclusion.
func arrayExclusions(cockroach string) map[string]string {
	result := map[string]string{
		engineMySQL:   "MySQL has no array type.",
		engineMariaDB: "MariaDB has no array type.",
		engineSQLite:  "SQLite has no array type.",
	}
	if cockroach != "" {
		result[engineCockroach] = cockroach
	}
	return result
}

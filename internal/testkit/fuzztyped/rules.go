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
	"slices"
	"strings"
	"time"
)

// timeRange records the instants an engine's timestamp column holds.
type timeRange struct {
	// low is the earliest instant.
	low time.Time

	// high is the latest instant.
	high time.Time
}

// timeRanges gives each engine's documented range where Go's time.Time is wider.
var timeRanges = map[string]timeRange{
	engineMySQL:      {low: time.Date(1000, 1, 1, 0, 0, 0, 0, time.UTC), high: time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)},
	engineMariaDB:    {low: time.Date(1000, 1, 1, 0, 0, 0, 0, time.UTC), high: time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)},
	engineClickHouse: {low: time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC), high: time.Date(2262, 4, 11, 23, 47, 16, 0, time.UTC)},
}

// zonedRanges gives MySQL's and MariaDB's TIMESTAMP range, which is narrower.
var zonedRanges = map[string]timeRange{
	engineMySQL:   {low: time.Date(1970, 1, 1, 0, 0, 1, 0, time.UTC), high: time.Date(2038, 1, 19, 3, 14, 7, 999999999, time.UTC)},
	engineMariaDB: {low: time.Date(1970, 1, 1, 0, 0, 1, 0, time.UTC), high: time.Date(2038, 1, 19, 3, 14, 7, 999999999, time.UTC)},
}

// microsecondEngines store timestamps to the microsecond. Their Go drivers send
// microseconds, dropping the rest of the nanoseconds toward the earlier instant, except
// MySQL's, whose server receives the nanoseconds and rounds half up.
var microsecondEngines = []string{enginePostgres, engineTimescale, engineCockroach, engineMySQL, engineMariaDB, engineDuckDB}

// floatType gives the engine's binary64 column type.
//
// Takes engine (string) which names the dialect.
//
// Returns string which is the column type.
func floatType(engine string) string {
	switch engine {
	case enginePostgres, engineTimescale:
		return "DOUBLE PRECISION"
	case engineCockroach:
		return "FLOAT8"
	case engineSQLite:
		return "REAL"
	case engineClickHouse:
		return "Nullable(Float64)"
	default:
		return "DOUBLE"
	}
}

// storedFloat gives the float a round trip returns.
//
// MySQL and MariaDB refuse NaN and the infinities, SQLite stores NaN as NULL, and SQLite
// and MariaDB store negative zero as zero.
//
// Takes engine (string) which names the dialect.
// Takes value (Value) which is the float written.
//
// Returns Value which is the float read back.
// Returns bool which is false when the engine refuses the value.
func storedFloat(engine string, value Value) (Value, bool) {
	if value.Kind == KindNull {
		return value, true
	}
	number := value.Float()
	special := math.IsNaN(number) || math.IsInf(number, 0)
	switch {
	case special && (engine == engineMySQL || engine == engineMariaDB):
		return value, false
	case math.IsNaN(number) && engine == engineSQLite:
		return Null(), true
	case number == 0 && (engine == engineSQLite || engine == engineMariaDB):
		return Float(0), true
	}
	return value, true
}

// equalFloat applies the engine's float equality, under which zero equals negative zero.
//
// PostgreSQL, CockroachDB and DuckDB treat NaN as equal to NaN. ClickHouse follows IEEE
// 754, under which NaN equals nothing.
//
// Takes engine (string) which names the dialect.
// Takes stored (Value) which is a row's value.
// Takes filter (Value) which is the parameter's stored value.
//
// Returns bool which reports a match.
func equalFloat(engine string, stored, filter Value) bool {
	if stored.Kind == KindNull || filter.Kind == KindNull {
		return false
	}
	left, right := stored.Float(), filter.Float()
	if math.IsNaN(left) || math.IsNaN(right) {
		return math.IsNaN(left) && math.IsNaN(right) && engine != engineClickHouse
	}
	return left == right
}

// equalExact matches non-NULL values that are identical.
//
// Takes stored (Value) which is a row's value.
// Takes filter (Value) which is the parameter's stored value.
//
// Returns bool which reports a match.
func equalExact(_ string, stored, filter Value) bool {
	return stored.Kind != KindNull && filter.Kind != KindNull && stored.Equal(filter)
}

// storedSame returns every value unchanged.
//
// Takes value (Value) which is the value written.
//
// Returns Value which is the value read back.
// Returns bool which is always true.
func storedSame(_ string, value Value) (Value, bool) {
	return value, true
}

// wallType gives the engine's timestamp column type without a zone.
//
// Takes engine (string) which names the dialect.
//
// Returns string which is the column type.
func wallType(engine string) string {
	switch engine {
	case engineMySQL, engineMariaDB:
		return "DATETIME(6)"
	case engineSQLite:
		return "DATETIME"
	case engineClickHouse:
		return "Nullable(DateTime64(9))"
	default:
		return "TIMESTAMP"
	}
}

// instantType gives the engine's zoned timestamp column type.
//
// Takes engine (string) which names the dialect.
//
// Returns string which is the column type.
func instantType(engine string) string {
	switch engine {
	case engineMySQL, engineMariaDB:
		return "TIMESTAMP(6) NULL"
	case engineClickHouse:
		return "Nullable(DateTime64(9, 'UTC'))"
	default:
		return "TIMESTAMPTZ"
	}
}

// storedWall gives the wall-clock timestamp a round trip returns.
//
// Takes engine (string) which names the dialect.
// Takes value (Value) which is the timestamp written.
//
// Returns Value which is the timestamp read back.
// Returns bool which is false outside the engine's range.
func storedWall(engine string, value Value) (Value, bool) {
	if value.Kind == KindNull {
		return value, true
	}
	moment, err := time.Parse(wallLayout, value.Text)
	if err != nil {
		return value, false
	}
	moment, ok := storedMoment(engine, moment, timeRanges)
	return wall(moment.Format(wallLayout)), ok
}

// storedInstant gives the instant a round trip returns, in UTC.
//
// Takes engine (string) which names the dialect.
// Takes value (Value) which is the instant written with its offset.
//
// Returns Value which is the instant read back.
// Returns bool which is false outside the engine's range.
func storedInstant(engine string, value Value) (Value, bool) {
	if value.Kind == KindNull {
		return value, true
	}
	moment, err := time.Parse(time.RFC3339Nano, value.Text)
	if err != nil {
		return value, false
	}
	ranges := zonedRanges
	if engine == engineClickHouse {
		ranges = timeRanges
	}
	moment, ok := storedMoment(engine, moment.UTC(), ranges)
	return instant(moment.Format(time.RFC3339Nano)), ok
}

// storedMoment applies an engine's precision and range.
//
// Takes engine (string) which names the dialect.
// Takes moment (time.Time) which is the timestamp in UTC.
// Takes ranges (map[string]timeRange) which holds the engine's range, if limited.
//
// Returns time.Time which is the stored timestamp.
// Returns bool which is false outside the range.
func storedMoment(engine string, moment time.Time, ranges map[string]timeRange) (time.Time, bool) {
	if engine == engineMySQL {
		moment = moment.Add(time.Duration(microseconds / 2))
	}
	if slices.Contains(microsecondEngines, engine) {
		moment = moment.Add(-time.Duration(int64(moment.Nanosecond()) % microseconds))
	}
	if limit, ok := ranges[engine]; ok && (moment.Before(limit.low) || moment.After(limit.high)) {
		return moment, false
	}
	return moment, true
}

// textType gives the engine's text column type, with a binary collation where the default
// would compare text loosely.
//
// Takes engine (string) which names the dialect.
//
// Returns string which is the column type.
func textType(engine string) string {
	switch engine {
	case engineCockroach:
		return "STRING"
	case engineMySQL, engineMariaDB:
		return "LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_bin"
	case engineDuckDB:
		return "VARCHAR"
	case engineClickHouse:
		return "Nullable(String)"
	default:
		return "TEXT"
	}
}

// storedText gives the text a round trip returns.
//
// PostgreSQL's family refuses NUL in text.
//
// Takes engine (string) which names the dialect.
// Takes value (Value) which is the text written.
//
// Returns Value which is the text read back.
// Returns bool which is false when the engine refuses the text.
func storedText(engine string, value Value) (Value, bool) {
	if value.Kind == KindText && strings.Contains(value.Text, "\x00") && slices.Contains(postgresFamily, engine) {
		return value, false
	}
	return value, true
}

// decimalType gives the engine's DECIMAL(38, 10) column type.
//
// Takes engine (string) which names the dialect.
//
// Returns string which is the column type.
func decimalType(engine string) string {
	switch engine {
	case enginePostgres, engineTimescale:
		return "NUMERIC(38, 10)"
	case engineClickHouse:
		return "Nullable(Decimal(38, 10))"
	default:
		return "DECIMAL(38, 10)"
	}
}

// arrayType gives the engine's integer array column type with nullable elements.
//
// Takes engine (string) which names the dialect.
//
// Returns string which is the column type.
func arrayType(engine string) string {
	switch engine {
	case engineCockroach:
		return "INT8[]"
	case engineClickHouse:
		return "Array(Nullable(Int64))"
	default:
		return "BIGINT[]"
	}
}

// nestedType gives the engine's array of integer arrays column type.
//
// Takes engine (string) which names the dialect.
//
// Returns string which is the column type.
func nestedType(engine string) string {
	if engine == engineClickHouse {
		return "Array(Array(Int64))"
	}
	return "BIGINT[][]"
}

// storedArray gives the array a round trip returns.
//
// ClickHouse arrays are never NULL and its nested arrays hold no NULL. duckdb-go cannot
// bind a list holding NULL, so generated DuckDB parameters cannot. PostgreSQL arrays are
// rectangular, and it reads an array of empty arrays as one empty array.
//
// Takes engine (string) which names the dialect.
// Takes value (Value) which is the array written.
//
// Returns Value which is the array read back.
// Returns bool which is false when the engine refuses the array.
func storedArray(engine string, value Value) (Value, bool) {
	if value.Kind == KindNull {
		if engine == engineClickHouse {
			return List(), true
		}
		return value, true
	}
	if engine == engineDuckDB && holdsNull(value) {
		return value, false
	}
	if len(value.Items) == 0 || value.Items[0].Kind != KindList {
		return value, true
	}
	return storedNested(engine, value)
}

// holdsNull reports a NULL member at any depth of a list.
//
// Takes value (Value) which is a list.
//
// Returns bool which is true when some member is NULL.
func holdsNull(value Value) bool {
	return slices.ContainsFunc(value.Items, func(member Value) bool { return member.Kind == KindNull || holdsNull(member) })
}

// storedNested gives the array of arrays a round trip returns.
//
// Takes engine (string) which names the dialect.
// Takes value (Value) which is a non-empty array of arrays.
//
// Returns Value which is the array read back.
// Returns bool which is false when the engine refuses the array.
func storedNested(engine string, value Value) (Value, bool) {
	width := len(value.Items[0].Items)
	for _, item := range value.Items {
		hasNull := slices.ContainsFunc(item.Items, func(member Value) bool { return member.Kind == KindNull })
		switch {
		case engine == engineClickHouse && hasNull:
			return value, false
		case (engine == enginePostgres || engine == engineTimescale) && len(item.Items) != width:
			return value, false
		}
	}
	if width == 0 && (engine == enginePostgres || engine == engineTimescale) {
		return List(), true
	}
	return value, true
}

// enumType gives the engine's enumerated column type over sad, ok and happy.
//
// Takes engine (string) which names the dialect.
//
// Returns string which is the column type, the type created in the schema elsewhere.
func enumType(engine string) string {
	switch engine {
	case engineMySQL, engineMariaDB:
		return "ENUM('sad', 'ok', 'happy')"
	case engineClickHouse:
		return "Nullable(Enum8('sad' = 1, 'ok' = 2, 'happy' = 3))"
	default:
		return "typed_mood"
	}
}

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

package fuzzcase

import "fmt"

// valueVersion selects exact text, binary, NULL and SQLite storage models.
const valueVersion = 5

// valueShapeStart follows the original and extended query identifiers.
const valueShapeStart = 28

const (
	// shapeExactValues compares text and binary parameters.
	shapeExactValues = valueShapeStart + iota

	// shapeNullableValues includes NULL-aware comparisons with repeated parameters.
	shapeNullableValues

	// shapeSQLiteAffinity observes mixed storage under INTEGER affinity.
	shapeSQLiteAffinity
)

// valueShapeCount fixes the assigned value-family identifiers.
const valueShapeCount = 3

// valuePayloadByte starts the bounded binary and text payload.
const valuePayloadByte = 8

// valuePayloadBound limits each exact scalar payload.
const valuePayloadBound = 16

const (
	// valueNullLabel selects a nil text parameter.
	valueNullLabel byte = 2

	// valueNullPayload selects a nil binary parameter.
	valueNullPayload byte = 4

	// valueEmptyText selects an empty text value independently of NULL.
	valueEmptyText byte = 8

	// valueNullText selects the literal text NULL.
	valueNullText byte = 16

	// valueInvalidByte exercises arbitrary bytes outside valid UTF-8.
	valueInvalidByte byte = 255

	// valueSecondInvalidByte distinguishes two invalid UTF-8 values.
	valueSecondInvalidByte byte = 254

	// valueAffinityInteger seeds an exact integer storage class.
	valueAffinityInteger = 5
)

// valueFeatures names the stable exact-value families.
var valueFeatures = [...]string{"text-binary-values", "nullable-values", "sqlite-affinity"}

// valueMigration adds the third bounded table for exact value observations.
//
// Returns string which declares nullable text and binary columns.
func (m Model) valueMigration() string {
	integer, text, binary, suffix := "BIGINT", "TEXT", "BYTEA", ""
	switch m.Engine {
	case engineSQLite:
		integer, binary = "INTEGER", "BLOB"
	case engineMySQL, engineMariaDB:
		binary = "VARBINARY(32)"
	case engineDuckDB:
		binary = "BLOB"
	case engineClickHouse:
		integer, text, binary, suffix = "Int64", "Nullable(String)", "Nullable(String)", " ENGINE = Memory"
	}
	if m.Shape == shapeSQLiteAffinity {
		return "CREATE TABLE fuzz_values (id INTEGER NOT NULL, flexible INTEGER, payload BLOB);"
	}
	return fmt.Sprintf("CREATE TABLE fuzz_values (id %s NOT NULL, label %s, payload %s)%s;", integer, text, binary, suffix)
}

// exactValues chooses distinguishable text and arbitrary byte parameters.
//
// Returns []Value which retains SQL NULL independently of empty values.
func (m Model) exactValues() []Value {
	if m.Shape == shapeSQLiteAffinity {
		return []Value{{Integer: 0, Kind: "", Text: ""}}
	}
	raw := make([]byte, 0, valuePayloadBound)
	for index := valuePayloadByte; index < len(m.Data) && len(raw) < valuePayloadBound; index++ {
		raw = append(raw, m.Data[index])
	}
	label := fmt.Sprintf("value %x 'é' \\ \n --", raw)
	if m.Flags&valueEmptyText != 0 {
		label = ""
	}
	if m.Flags&valueNullText != 0 {
		label = "NULL"
	}
	values := []Value{{Kind: valueText, Text: label, Integer: 0}, {Kind: valueBinary, Text: string(raw), Integer: 0}}
	if m.Flags&valueNullLabel != 0 {
		values[0] = Value{Kind: valueNull, Text: "", Integer: 0}
	}
	if m.Flags&valueNullPayload != 0 {
		values[1] = Value{Kind: valueNull, Text: "", Integer: 0}
	}
	return values
}

// valueInserts writes asymmetric rows containing exact values and NULLs.
//
// Returns []string which inserts at most eight rows into the third table.
func (m Model) valueInserts() []string {
	var statements []string
	for row := range m.Rows {
		if m.Shape == shapeSQLiteAffinity {
			value := sqliteAffinityValue(row)
			statements = append(statements, fmt.Sprintf("INSERT INTO fuzz_values (id, flexible, payload) VALUES (%d, %s, X'00ff');", row+1, value.literal(m.Engine)))
			continue
		}
		values := m.exactValues()
		if row > 0 {
			values[0] = Value{Kind: valueText, Text: fmt.Sprintf("other %d", row), Integer: 0}
			values[1] = Value{Kind: valueBinary, Text: string([]byte{byte(row), 0, valueInvalidByte}), Integer: 0}
			if row%2 == 0 {
				values[0] = Value{Kind: valueNull, Text: "", Integer: 0}
			}
		}
		statements = append(statements, fmt.Sprintf("INSERT INTO fuzz_values (id, label, payload) VALUES (%d, %s, %s);", row+1, values[0].literal(m.Engine), values[1].literal(m.Engine)))
	}
	return statements
}

// valueQuery renders an exact-value query and its independent reference.
//
// Returns Query which records each typed binding and repeated occurrence.
func (m Model) valueQuery() Query {
	values := m.exactValues()
	var bindings, occurrences []Value
	seen := make(map[int]bool)
	parameter := func(identity int) string {
		value := values[identity]
		occurrences = append(occurrences, value)
		if !seen[identity] {
			bindings = append(bindings, value)
			seen[identity] = true
		}
		switch m.Engine {
		case engineClickHouse:
			return fmt.Sprintf("{p%d:Nullable(String)}", identity+1)
		case engineSQLite, engineMySQL, engineMariaDB:
			return fmt.Sprintf("?%d", identity+1)
		default:
			return fmt.Sprintf("$%d", identity+1)
		}
	}
	sql := m.valueBody(parameter)
	reference := m.valueBody(func(identity int) string { return values[identity].literal(m.Engine) })
	ending := " ORDER BY v.id"
	countReference := reference
	if m.Flags&1 != 0 {
		ending += " LIMIT 2 OFFSET 1"
		countReference += ending
	}
	return Query{SQL: "-- sgl.query(name: FuzzQuery, command: many, dynamic: runtime)\n" + sql + ending + ";", Reference: reference + ending, CountReference: countReference,
		Values: bindings, Occurrences: occurrences, Columns: tableColumns, Writes: false, Ordered: true}
}

// valueBody writes comparisons independently of placeholder spelling and values.
//
// Takes parameter (func(int) string) which writes an identity or literal.
//
// Returns string which retains exact-value or NULL-aware predicates.
func (m Model) valueBody(parameter func(int) string) string {
	if m.Shape == shapeSQLiteAffinity {
		return "SELECT v.id, v.flexible, v.payload FROM fuzz_values v WHERE v.id >= " + parameter(0)
	}
	var predicate string
	if m.Shape == shapeNullableValues {
		predicate = "(v.label = " + parameter(0) + " OR " + parameter(0) + " IS NULL) AND (v.payload = " + parameter(1) + " OR " + parameter(1) + " IS NULL)"
	} else {
		predicate = "v.label = " + parameter(0) + " AND v.payload = " + parameter(1)
	}
	return "SELECT v.id, v.label, v.payload FROM fuzz_values v WHERE " + predicate
}

// SQLiteAffinity reports whether runtime storage may differ from declared column types.
//
// Returns bool which enables the independent SQLite storage-class oracle.
func (m Model) SQLiteAffinity() bool {
	return m.at(0) == valueVersion && m.Shape == shapeSQLiteAffinity
}

// ExactValues reports whether output storage kinds belong to the value contract.
//
// Returns bool which distinguishes numeric, text and binary result representations.
func (m Model) ExactValues() bool {
	return m.at(0) == valueVersion
}

// SQLiteStorageClasses supplies independent expected storage for every inserted row.
//
// Returns []string which accounts for numeric text converting under INTEGER affinity.
func (m Model) SQLiteStorageClasses() []string {
	if !m.SQLiteAffinity() {
		return nil
	}
	classes := []string{"integer", "text", "blob", "null", "integer", "text", "integer", "blob"}
	return classes[:m.Rows]
}

// valueSeeds covers empty data, exact bytes and every nullable parameter combination.
//
// Takes engine (string) which selects supported value families.
//
// Returns [][]byte which reaches every enabled value oracle.
func valueSeeds(engine string) [][]byte {
	var seeds [][]byte
	for shape := range valueShapeCount {
		if !supportsShape(engine, valueShapeStart+shape) {
			continue
		}
		for _, rows := range []byte{0, 1, maxRows} {
			for _, flags := range []byte{0, valueNullLabel, valueNullPayload, valueNullLabel | valueNullPayload} {
				seeds = append(seeds, []byte{valueVersion, byte(shape), flags, rows, 0, 0, 2, 1, 0, valueInvalidByte, valueSecondInvalidByte, '\'', '\\'})
			}
		}
		seeds = append(seeds, []byte{valueVersion, byte(shape), valueEmptyText, maxRows}, []byte{valueVersion, byte(shape), valueNullText, maxRows},
			[]byte{valueVersion, byte(shape), 1, maxRows})
	}
	return seeds
}

// sqliteAffinityValue supplies storage classes that differ from declared affinity.
//
// Takes row (int) which identifies a bounded fixture row.
//
// Returns Value which SQLite stores under INTEGER affinity.
func sqliteAffinityValue(row int) Value {
	values := [...]Value{
		{Integer: valueAffinityInteger, Kind: "", Text: ""}, {Kind: valueText, Text: "NULL", Integer: 0},
		{Kind: valueBinary, Text: string([]byte{valueInvalidByte, 0}), Integer: 0}, {Kind: valueNull, Text: "", Integer: 0},
		{Kind: valueText, Text: "13", Integer: 0}, {Kind: valueText, Text: "hello", Integer: 0}, {Integer: -seedUpper, Kind: "", Text: ""}, {Kind: valueBinary, Text: "", Integer: 0},
	}
	return values[row%len(values)]
}

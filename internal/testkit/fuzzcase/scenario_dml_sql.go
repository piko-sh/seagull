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

import (
	"fmt"
	"strconv"
	"strings"
)

// DMLTable names the modelled table.
const DMLTable = "dml_items"

const (
	// dmlExecRows is the command reporting an affected-row count.
	dmlExecRows = "execrows"

	// dmlKeyColumn is the primary key column.
	dmlKeyColumn = "id"

	// dmlClose closes a parenthesised list.
	dmlClose = ")"

	// dmlSeparator separates listed columns and values.
	dmlSeparator = ", "
)

// DMLRowsMethod names the generated method reading the table ordered by identifier.
const DMLRowsMethod = "DmlRows"

// dmlMethods names the generated method serving each operation.
var dmlMethods = map[string]string{DMLInsert: "DmlInsert", DMLInsertCount: "DmlInsertCount", DMLInsertReturning: "DmlInsertReturning",
	DMLInsertPair: "DmlInsertPair", DMLUpdate: "DmlUpdate", DMLUpdateNote: "DmlUpdateNote", DMLUpdateReturning: "DmlUpdateReturning",
	DMLDelete: "DmlDelete", DMLDeleteReturning: "DmlDeleteReturning", DMLUpsert: "DmlUpsert", DMLBatch: "DmlBatch",
	DMLUpsertBatch: "DmlUpsertBatch", DMLCopy: "DmlCopy"}

// dmlStatement builds one catalogue statement on demand, so that its placeholders are
// numbered from the first.
type dmlStatement struct {
	// build writes the statement.
	build func() string

	// operation names the statement family.
	operation string

	// command is the query command.
	command string
}

// dmlPlaceholders writes each engine's placeholders in order.
type dmlPlaceholders struct {
	// engine selects the placeholder syntax.
	engine string

	// columns supplies the ClickHouse placeholder types.
	columns []DMLColumn

	// number counts the placeholders of the current statement.
	number int
}

// next writes one placeholder for a column.
//
// Takes column (string) which names the column the value is compared with or stored in.
//
// Returns string which is the native placeholder.
func (placeholders *dmlPlaceholders) next(column string) string {
	placeholders.number++
	switch placeholders.engine {
	case engineMySQL, engineMariaDB:
		return "?"
	case engineSQLite:
		return "?" + strconv.Itoa(placeholders.number)
	case engineClickHouse:
		native := "Int64"
		for _, candidate := range placeholders.columns {
			if candidate.Name == column {
				native = candidate.Native
				if candidate.Nullable {
					native = clickHouseNullable(native)
				}
			}
		}
		return "{" + column + strconv.Itoa(placeholders.number) + ":" + native + "}"
	default:
		return "$" + strconv.Itoa(placeholders.number)
	}
}

// row writes the placeholders of one VALUES row.
//
// Takes operation (string) which selects the columns written.
//
// Returns string which lists the identifier and each written column's placeholder.
func (placeholders *dmlPlaceholders) row(operation string) string {
	values := make([]string, 0, 1+len(placeholders.columns))
	values = append(values, placeholders.next(dmlKeyColumn))
	for _, column := range placeholders.columns {
		if DMLWritten(operation, column) {
			values = append(values, placeholders.next(column.Name))
		}
	}
	return strings.Join(values, dmlSeparator)
}

// DMLMethod names the generated method serving an operation.
//
// Takes operation (string) which names the statement family.
//
// Returns string which is empty for a transaction.
func DMLMethod(operation string) string {
	return dmlMethods[operation]
}

// DMLSchema creates the modelled table.
//
// Takes engine (string) which selects native types and table syntax.
//
// Returns []string which holds the migration statements.
func DMLSchema(engine string) []string {
	var definition strings.Builder
	if engine == engineClickHouse {
		definition.WriteString("CREATE TABLE " + DMLTable + " (id Int64")
	} else {
		definition.WriteString("CREATE TABLE " + DMLTable + " (id BIGINT NOT NULL PRIMARY KEY")
	}
	for _, column := range dmlColumns(engine) {
		native := column.Native
		switch {
		case engine == engineClickHouse && column.Nullable:
			native = clickHouseNullable(native)
		case !column.Nullable && engine != engineClickHouse:
			native += " NOT NULL"
		}
		definition.WriteString(dmlSeparator + column.Name + " " + native)
	}
	definition.WriteString(dmlClose)
	if engine == engineClickHouse {
		definition.WriteString(" ENGINE = MergeTree ORDER BY id")
	}
	return []string{definition.String() + ";"}
}

// DMLQueries builds the generated method catalogue for an engine.
//
// Takes engine (string) which selects placeholders and admitted operations.
//
// Returns string which holds every admitted statement and the ordered read.
func DMLQueries(engine string) string {
	columns := dmlColumns(engine)
	placeholders := &dmlPlaceholders{engine: engine, columns: columns, number: 0}
	var queries strings.Builder
	for _, statement := range dmlStatements(engine, placeholders) {
		placeholders.number = 0
		if supported, _ := dmlSupported(engine, statement.operation); !supported {
			continue
		}
		command := statement.command
		if engine == engineClickHouse && command == dmlExecRows {
			command = "exec"
		}
		fmt.Fprintf(&queries, "-- sgl.query(%s, %s)\n%s;\n\n", dmlMethods[statement.operation], command, statement.build())
	}
	fmt.Fprintf(&queries, "-- sgl.query(%s, many)\nSELECT %s FROM %s ORDER BY id;\n", DMLRowsMethod, dmlColumnList(DMLInsert, columns), DMLTable)
	return queries.String()
}

// dmlColumnList lists the identifier and the columns an operation writes.
//
// Takes operation (string) which selects the columns.
// Takes columns ([]DMLColumn) which describes the non-key columns.
//
// Returns string which is the comma-separated list.
func dmlColumnList(operation string, columns []DMLColumn) string {
	names := make([]string, 0, 1+len(columns))
	names = append(names, dmlKeyColumn)
	for _, column := range columns {
		if DMLWritten(operation, column) {
			names = append(names, column.Name)
		}
	}
	return strings.Join(names, dmlSeparator)
}

// dmlStatements lists every statement of the catalogue.
//
// Takes engine (string) which selects the upsert syntax.
// Takes placeholders (*dmlPlaceholders) which numbers each statement's placeholders.
//
// Returns []dmlStatement which holds one entry per operation.
func dmlStatements(engine string, placeholders *dmlPlaceholders) []dmlStatement {
	list := dmlColumnList(DMLInsert, placeholders.columns)
	row := func() string { return "(" + placeholders.row(DMLInsert) + dmlClose }
	insert := "INSERT INTO " + DMLTable + " (" + list + ") VALUES "
	batch := func() string {
		return "INSERT INTO " + DMLTable + " (" + dmlColumnList(DMLBatch, placeholders.columns) + ") VALUES (" + placeholders.row(DMLBatch) + dmlClose
	}
	upsertTail := " ON CONFLICT (id) DO UPDATE SET qty = EXCLUDED.qty, note = EXCLUDED.note"
	if mysqlFamily(engine) {
		upsertTail = " ON DUPLICATE KEY UPDATE qty = VALUES(qty), note = VALUES(note)"
	}
	return []dmlStatement{
		{operation: DMLInsert, command: "exec", build: func() string { return insert + row() }},
		{operation: DMLInsertCount, command: dmlExecRows, build: func() string { return insert + row() }},
		{operation: DMLInsertReturning, command: "one", build: func() string { return insert + row() + " RETURNING " + list }},
		{operation: DMLInsertPair, command: dmlExecRows, build: func() string { return insert + row() + dmlSeparator + row() }},
		{operation: DMLUpdate, command: dmlExecRows, build: func() string {
			return "UPDATE " + DMLTable + " SET qty = " + placeholders.next("qty") + " WHERE id = " + placeholders.next(dmlKeyColumn)
		}},
		{operation: DMLUpdateNote, command: dmlExecRows, build: func() string {
			return "UPDATE " + DMLTable + " SET note = " + placeholders.next("note") + " WHERE qty >= " + placeholders.next("qty")
		}},
		{operation: DMLUpdateReturning, command: "many", build: func() string {
			return "UPDATE " + DMLTable + " SET qty = " + placeholders.next("qty") + " WHERE id >= " +
				placeholders.next(dmlKeyColumn) + " RETURNING id, qty"
		}},
		{operation: DMLDelete, command: dmlExecRows, build: func() string { return "DELETE FROM " + DMLTable + " WHERE id = " + placeholders.next(dmlKeyColumn) }},
		{operation: DMLDeleteReturning, command: "many", build: func() string {
			return "DELETE FROM " + DMLTable + " WHERE id >= " + placeholders.next(dmlKeyColumn) + " RETURNING id"
		}},
		{operation: DMLUpsert, command: dmlExecRows, build: func() string { return insert + row() + upsertTail }},
		{operation: DMLBatch, command: "batch", build: batch},
		{operation: DMLUpsertBatch, command: "batch", build: func() string { return insert + row() + upsertTail }},
		{operation: DMLCopy, command: "copyfrom", build: func() string { return insert + row() }},
	}
}

// clickHouseNullable wraps a ClickHouse type in Nullable.
//
// Takes native (string) which is the inner type.
//
// Returns string which admits NULL.
func clickHouseNullable(native string) string {
	return "Nullable(" + native + dmlClose
}

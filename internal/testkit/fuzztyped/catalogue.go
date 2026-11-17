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
	"fmt"
	"strings"
)

const (
	// tablePrefix starts every value family's table name.
	tablePrefix = "typed_"

	// enumDeclaration creates the enumerated type where it is a separate type.
	enumDeclaration = "CREATE TYPE typed_mood AS ENUM ('sad', 'ok', 'happy');"
)

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

// admitted lists the families an engine stores, in stable order.
//
// Takes engine (string) which names the dialect.
//
// Returns []*family which excludes the families the engine lacks.
func admitted(engine string) []*family {
	var result []*family
	for index := range families {
		if _, excluded := families[index].excluded[engine]; !excluded {
			result = append(result, &families[index])
		}
	}
	return result
}

// Schema creates every admitted family's table and the expression table.
//
// Takes engine (string) which names the dialect.
//
// Returns []string which holds the statements in order.
func Schema(engine string) []string {
	var statements []string
	identifier, suffix := "BIGINT NOT NULL", ""
	switch engine {
	case engineSQLite:
		identifier = "INTEGER NOT NULL"
	case engineClickHouse:
		identifier, suffix = "Int64", " ENGINE = Memory"
	}
	for _, entry := range admitted(engine) {
		native := entry.native(engine)
		if entry.name == "Enum" && native == "typed_mood" {
			statements = append(statements, enumDeclaration)
		}
		if entry.required && engine != engineClickHouse {
			native += " NOT NULL"
		}
		statements = append(statements, fmt.Sprintf("CREATE TABLE %s%s (id %s, v %s)%s;", tablePrefix, strings.ToLower(entry.name), identifier, native, suffix))
	}
	return append(statements, expressionSchema(engine, identifier, suffix))
}

// Queries writes every admitted family's methods and the expression queries.
//
// Takes engine (string) which names the dialect.
//
// Returns string which holds the query file.
func Queries(engine string) string {
	var file strings.Builder
	for _, entry := range admitted(engine) {
		table := tablePrefix + strings.ToLower(entry.name)
		identifier, value := placeholders(engine, entry)
		fmt.Fprintf(&file, "-- sgl.query(Insert%s, exec)\nINSERT INTO %s (id, v) VALUES (%s, %s);\n\n", entry.name, table, identifier, value)
		fmt.Fprintf(&file, "-- sgl.query(Read%s, many)\nSELECT id, v FROM %s ORDER BY id;\n\n", entry.name, table)
		if entry.filtered {
			only := parameter(engine, 1, entry.native(engine))
			fmt.Fprintf(&file, "-- sgl.query(Where%s, many)\nSELECT id FROM %s WHERE v = %s ORDER BY id;\n\n", entry.name, table, only)
			fmt.Fprintf(&file, "-- sgl.query(Join%s, many)\nSELECT a.id FROM %s a JOIN %s b ON b.id = a.id AND b.v = %s ORDER BY a.id;\n\n",
				entry.name, table, table, only)
		}
		if engine != engineClickHouse {
			fmt.Fprintf(&file, "-- sgl.query(Update%s, exec)\nUPDATE %s SET v = %s WHERE id = %s;\n\n", entry.name, table, value, identifier)
		}
	}
	file.WriteString(expressionQueries(engine))
	return file.String()
}

// QueryCount returns the number of queries Queries writes.
//
// Takes engine (string) which names the dialect.
//
// Returns int which is the number of generated methods required.
func QueryCount(engine string) int {
	count := len(admittedExpressions(engine))
	for _, entry := range admitted(engine) {
		count += 2
		if entry.filtered {
			count += 2
		}
		if engine != engineClickHouse {
			count++
		}
	}
	return count
}

// parameter writes the engine's placeholder for the numbered parameter.
//
// Takes engine (string) which names the dialect.
// Takes number (int) which is the parameter's position, from one.
// Takes native (string) which is the parameter's ClickHouse type.
//
// Returns string which is the placeholder.
func parameter(engine string, number int, native string) string {
	switch engine {
	case engineMySQL, engineMariaDB:
		return "?"
	case engineSQLite:
		return fmt.Sprintf("?%d", number)
	case engineClickHouse:
		return fmt.Sprintf("{p%d:%s}", number, native)
	default:
		return fmt.Sprintf("$%d", number)
	}
}

// placeholders writes the identifier and value parameters of a family's queries.
//
// Takes engine (string) which names the dialect.
// Takes entry (*family) which selects the value's type.
//
// Returns identifier (string) which is the identifier parameter.
// Returns value (string) which is the value parameter.
func placeholders(engine string, entry *family) (identifier, value string) {
	switch engine {
	case engineMySQL, engineMariaDB:
		return "?", "?"
	case engineSQLite:
		return "?1", "?2"
	case engineClickHouse:
		return "{id:Int64}", "{v:" + entry.native(engine) + "}"
	default:
		return "$1", "$2"
	}
}

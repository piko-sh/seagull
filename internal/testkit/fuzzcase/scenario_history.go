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

const (
	// historyRenameColumn renames a column into its final name.
	historyRenameColumn = iota

	// historyAddColumn adds the final column to a narrower table.
	historyAddColumn

	// historyRenameTable renames a table into its final name.
	historyRenameTable
)

const (
	// historyRenameColumnFeature names the column rename history.
	historyRenameColumnFeature = "rename-column"

	// historyRenameTableFeature names the table rename history.
	historyRenameTableFeature = "rename-table"
)

// SchemaHistory pairs a direct declaration with a data-preserving DDL history.
type SchemaHistory struct {
	// Feature names the independent history coverage obligation.
	Feature string

	// Direct constructs the expected final schema and query.
	Direct Scenario

	// Initial holds the first declaration in the alternative history.
	Initial []string

	// Inserts populates the initial schema before any alterations.
	Inserts []string

	// Steps preserves the order of observable schema changes.
	Steps []string
}

// HistorySeeds reaches each admitted schema equivalence family.
//
// Takes engine (string) which selects native capabilities.
//
// Returns [][]byte which covers empty and populated histories.
func HistorySeeds(engine string) [][]byte {
	if !supportsShape(engine, shapeSelect) {
		return nil
	}
	var result [][]byte
	for operation := range historyCount {
		for _, rows := range []byte{0, 1, historyRowsSeed} {
			result = append(result, []byte{HistoryVersion, byte(operation), rows, historyValueSeed})
		}
	}
	return result
}

// DecodeHistory constructs equivalent schemas with independently retained data.
//
// Takes engine (string) which selects native DDL spelling.
// Takes data ([]byte) which holds bounded operation choices.
//
// Returns SchemaHistory which specifies both construction paths.
// Returns bool which rejects unsupported envelopes.
func DecodeHistory(engine string, data []byte) (SchemaHistory, bool) {
	if len(data) == 0 || len(data) > ScenarioMaxBytes || data[0] != HistoryVersion || !supportsShape(engine, shapeSelect) {
		return SchemaHistory{}, false
	}
	integer, nullable, suffix := "BIGINT", "BIGINT", ""
	if engine == engineSQLite {
		integer, nullable = "INTEGER", "INTEGER"
	}
	if engine == engineClickHouse {
		integer, nullable, suffix = "Int64", "Nullable(Int64)", " ENGINE = MergeTree ORDER BY id"
	}
	schema := fmt.Sprintf("CREATE TABLE fuzz_items (id %s NOT NULL, score %s, category %s NOT NULL DEFAULT 0)%s", integer, nullable, integer, suffix)
	initial, steps, feature := historyStatements(engine, data[1:], schema, integer)
	inserts := historyInserts(data)
	initialInserts := append([]string(nil), inserts...)
	for index, statement := range initialInserts {
		if feature == historyRenameColumnFeature {
			statement = strings.ReplaceAll(statement, "score", "previous")
		}
		if feature == historyRenameTableFeature {
			statement = strings.ReplaceAll(statement, "fuzz_items", "fuzz_stage")
		}
		initialInserts[index] = statement
	}
	query := scenarioQuery("HistoryRows", "SELECT id, score, category FROM fuzz_items ORDER BY id", nil, nil, tableColumns, true)
	direct := Scenario{Engine: engine, Data: append([]byte(nil), data...), Schema: []string{schema}, Inserts: inserts,
		Queries: []ScenarioQuery{query}, Relations: nil, Features: []string{"history/" + feature}}
	return SchemaHistory{Direct: direct, Initial: []string{initial}, Inserts: initialInserts, Steps: steps, Feature: feature}, true
}

// historyStatements writes a valid route to the same final declaration.
//
// Takes engine (string) which selects native syntax and binding rules.
// Takes data ([]byte) which supplies bounded versioned choices.
// Takes schema (string) which declares the expected final table.
// Takes integer (string) which names its native integer type.
//
// Returns string which declares the initial table.
// Returns []string which contains the ordered schema changes.
// Returns string which identifies the history family.
func historyStatements(engine string, data []byte, schema, integer string) (string, []string, string) {
	switch scenarioByte(data, 0) % historyCount {
	case historyRenameColumn:
		return strings.Replace(schema, "score ", "previous ", 1), []string{"ALTER TABLE fuzz_items RENAME COLUMN previous TO score"}, historyRenameColumnFeature
	case historyAddColumn:
		initial := strings.Replace(schema, ", category "+integer+" NOT NULL DEFAULT 0", "", 1)
		if engine == engineClickHouse {
			return initial, []string{"ALTER TABLE fuzz_items ADD COLUMN category Int64 DEFAULT 0"}, "add-column"
		}
		if engine == engineDuckDB {
			return initial, []string{"ALTER TABLE fuzz_items ADD COLUMN category " + integer + " DEFAULT 0", "ALTER TABLE fuzz_items ALTER COLUMN category SET NOT NULL"}, "add-column"
		}
		return initial, []string{"ALTER TABLE fuzz_items ADD COLUMN category " + integer + " NOT NULL DEFAULT 0"}, "add-column"
	case historyRenameTable:
		rename := "ALTER TABLE fuzz_stage RENAME TO fuzz_items"
		if engine == engineClickHouse {
			rename = "RENAME TABLE fuzz_stage TO fuzz_items"
		}
		return strings.Replace(schema, "fuzz_items", "fuzz_stage", 1), []string{rename}, historyRenameTableFeature
	case historyViewLifecycle:
		return schema, []string{"CREATE VIEW fuzz_history_view AS SELECT id, score FROM fuzz_items", "DROP VIEW fuzz_history_view"}, "view-lifecycle"
	case historyIndexLifecycle:
		if engine != engineClickHouse {
			drop := "DROP INDEX fuzz_history_index"
			if mysqlFamily(engine) {
				drop += " ON fuzz_items"
			}
			return schema, []string{"CREATE INDEX fuzz_history_index ON fuzz_items (category, id)", drop}, "index-lifecycle"
		}
		return schema, []string{"ALTER TABLE fuzz_items ADD COLUMN extra " + integer, "ALTER TABLE fuzz_items DROP COLUMN extra"}, "column-lifecycle"
	default:
		return schema, []string{"ALTER TABLE fuzz_items RENAME COLUMN score TO previous", "ALTER TABLE fuzz_items RENAME COLUMN previous TO score"}, "rename-roundtrip"
	}
}

// historyInserts includes SQL NULL and signed values before the first alteration.
//
// Takes data ([]byte) which supplies bounded versioned choices.
//
// Returns []string which populates rows before schema changes.
func historyInserts(data []byte) []string {
	count := int(scenarioByte(data, 2) % (maxRows + 1))
	inserts := make([]string, 0, count)
	for index := range count {
		value := strconv.Itoa(int(scenarioByte(data, scenarioValueByte+index)%historyScoreRange) - historyScoreOffset)
		if index%historyNullPeriod == 0 {
			value = "NULL"
		}
		inserts = append(inserts, fmt.Sprintf("INSERT INTO fuzz_items (id, score) VALUES (%d, %s)", index+1, value))
	}
	return inserts
}

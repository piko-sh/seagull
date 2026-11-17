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

import "slices"

// DMLVersion identifies data-changing statement sequences run through generated methods.
const DMLVersion = 17

const (
	// dmlAmountColumn is the decimal column a batch leaves to its default.
	dmlAmountColumn = "amount"

	// dmlStepBound bounds the statements of one input.
	dmlStepBound = 10

	// dmlInnerBound bounds the statements of one transaction.
	dmlInnerBound = 4

	// dmlKeySpace keeps ordinary identifiers small so statements meet existing rows.
	dmlKeySpace = 8

	// dmlBatchBound bounds the rows of an ordinary batch.
	dmlBatchBound = 6

	// dmlBulkBase starts the identifiers of a bulk batch beyond the key space.
	dmlBulkBase = 1000

	// dmlBulkBound bounds the rows of a bulk batch. It is enough to cross ClickHouse's 1024
	// bound values per statement with six columns.
	dmlBulkBound = 240

	// dmlBulkWrites bounds the bulk writes of one input, so that the table stays within the
	// helper's row and frame bounds. Later bulk choices write ordinary batches.
	dmlBulkWrites = 1

	// dmlFreshBase starts the identifiers of ClickHouse rows, which never conflict.
	dmlFreshBase = 100

	// dmlNilFlag sends the empty binary value of a row as a nil slice.
	dmlNilFlag = 64

	// dmlConflictFlag repeats an existing identifier in a batch or copy.
	dmlConflictFlag = 128

	// dmlBulkFlag turns a batch or copy into a bulk write.
	dmlBulkFlag = 64

	// dmlCommitFlag commits a transaction instead of rolling it back.
	dmlCommitFlag = 1
)

const (
	// DMLInsert inserts one row through an exec method.
	DMLInsert = "insert"

	// DMLInsertCount inserts one row through an execrows method.
	DMLInsertCount = "insert-count"

	// DMLInsertReturning inserts one row and returns it through a one method.
	DMLInsertReturning = "insert-returning"

	// DMLInsertPair inserts two rows with one multi-row VALUES statement.
	DMLInsertPair = "insert-pair"

	// DMLUpdate sets the quantity of one row.
	DMLUpdate = "update"

	// DMLUpdateNote sets the note of every row at or above a quantity.
	DMLUpdateNote = "update-note"

	// DMLUpdateReturning sets the quantity of rows from an identifier and returns them.
	DMLUpdateReturning = "update-returning"

	// DMLDelete deletes one row.
	DMLDelete = "delete"

	// DMLDeleteReturning deletes rows from an identifier and returns their identifiers.
	DMLDeleteReturning = "delete-returning"

	// DMLUpsert inserts a row or updates its quantity and note.
	DMLUpsert = "upsert"

	// DMLBatch inserts rows through a batch method.
	DMLBatch = "batch"

	// DMLUpsertBatch upserts rows through a batch method.
	DMLUpsertBatch = "upsert-batch"

	// DMLCopy inserts rows through a copyfrom method.
	DMLCopy = "copy"

	// DMLTransaction runs statements in a transaction and commits or rolls back.
	DMLTransaction = "transaction"
)

// dmlNoReturning explains the MySQL RETURNING exclusions.
const dmlNoReturning = "MySQL has no RETURNING clause."

// dmlClickHouseMutation explains the ClickHouse UPDATE, DELETE and RETURNING exclusions.
const dmlClickHouseMutation = "ClickHouse has no RETURNING, and UPDATE and DELETE are asynchronous mutations without row counts (asyncexec, Q041)."

const (
	// dmlSeedSetup inserts rows 1, 2 and 3. The third has NULLs and sends its empty binary
	// value as a nil slice.
	dmlSeedSetup = "\x00\x00\x01\x00\x00\x01\x02\x00\x00\x42\x03\x1e"

	// dmlSeedPlain supplies ordinary choices.
	dmlSeedPlain = "\x01\x02\x03\x04\x05\x06\x07"

	// dmlSeedNulls supplies nil slices and NULLs.
	dmlSeedNulls = "\x43\x03\x1e\x44\x08\x1e\x00"

	// dmlSeedBatchNulls selects a batch of three rows with nil slices and NULLs.
	dmlSeedBatchNulls = "\x02\x43\x03\x1e\x44\x08\x1e\x00\x03\x1e"

	// dmlSeedBulk selects a bulk write.
	dmlSeedBulk = "\x43"

	// dmlSeedConflict selects a batch whose first row repeats row 2.
	dmlSeedConflict = "\x81\x01\x02\x00\x05\x03\x1e"

	// dmlSeedTransaction holds four statements. They insert row 5, update row 2, delete row
	// 1 and start a batch of two rows, whose operation byte follows.
	dmlSeedTransaction = "\x03\x00\x04\x01\x00\x04\x02\x01\x07\x00"

	// dmlSeedTransactionBatch holds the rows of the transaction's batch.
	dmlSeedTransactionBatch = "\x01\x05\x03\x00\x06\x04\x1e"
)

// dmlOperations fixes the decoding order of operations for every engine.
var dmlOperations = []string{DMLInsert, DMLInsertCount, DMLInsertReturning, DMLInsertPair, DMLUpdate, DMLUpdateNote,
	DMLUpdateReturning, DMLDelete, DMLDeleteReturning, DMLUpsert, DMLBatch, DMLUpsertBatch, DMLCopy, DMLTransaction}

// dmlExclusions records each engine's excluded operations with the engine, driver or
// Seagull reason.
var dmlExclusions = map[string]map[string]string{
	engineMySQL:   {DMLInsertReturning: dmlNoReturning, DMLDeleteReturning: dmlNoReturning, DMLUpdateReturning: dmlNoReturning},
	engineMariaDB: {DMLUpdateReturning: "MariaDB has RETURNING on INSERT and DELETE only."},
	engineClickHouse: {
		DMLInsertCount: "Seagull refuses execrows on ClickHouse (Q059) because clickhouse-go reports no affected-row count.",
		DMLTransaction: "ClickHouse has no interactive transactions.",
		DMLUpsert:      "ClickHouse has no upsert and no unique keys.", DMLUpsertBatch: "ClickHouse has no upsert and no unique keys.",
		DMLInsertReturning: dmlClickHouseMutation, DMLUpdate: dmlClickHouseMutation, DMLUpdateNote: dmlClickHouseMutation,
		DMLUpdateReturning: dmlClickHouseMutation, DMLDelete: dmlClickHouseMutation, DMLDeleteReturning: dmlClickHouseMutation,
	},
}

// DMLRow is one independently modelled table row.
type DMLRow struct {
	// Values holds the non-key columns in dmlColumns order.
	Values []TypedValue

	// ID is the primary key.
	ID int64

	// NilBytes sends an empty binary column as a nil Go slice.
	NilBytes bool
}

// DMLStep is one statement with its independently computed outcome.
type DMLStep struct {
	// Operation names the statement family.
	Operation string

	// Rows holds the rows a statement writes.
	Rows []DMLRow

	// Inner holds the statements of a transaction.
	Inner []DMLStep

	// Returned holds the rows RETURNING yields, ordered by identifier.
	Returned []DMLRow

	// State holds the table after the statement, ordered by identifier.
	State []DMLRow

	// Value supplies the quantity or note a statement sets.
	Value TypedValue

	// Key selects the row or lower identifier bound a statement filters on.
	Key int64

	// Affected is the engine's affected-row count when HasAffected is set.
	Affected int64

	// HasAffected records that the method reports a count to check.
	HasAffected bool

	// Fails records that the engine refuses the statement and leaves the table as it was.
	Fails bool

	// Commit commits a transaction instead of rolling it back.
	Commit bool
}

// DMLCase is a decoded version 17 input with its expected outcomes.
type DMLCase struct {
	// Engine selects syntax and engine contracts.
	Engine string

	// Data retains the replay input.
	Data []byte

	// Steps holds the statements in order.
	Steps []DMLStep

	// Columns names the value families of the non-key columns.
	Columns []DMLColumn
}

// DMLColumn describes one non-key column of the modelled table.
type DMLColumn struct {
	// Name is the column's SQL name.
	Name string

	// Kind is the independent value family.
	Kind string

	// Native is the engine's column type.
	Native string

	// Nullable admits SQL NULL.
	Nullable bool
}

// dmlColumns lists the table's non-key columns for an engine.
//
// Takes engine (string) which selects the families its fixture admits.
//
// Returns []DMLColumn which starts with qty, note and data, then amount and seen_at where
// admitted.
func dmlColumns(engine string) []DMLColumn {
	var columns []DMLColumn
	for _, column := range []DMLColumn{{Name: "qty", Kind: typedSigned, Native: "", Nullable: false}, {Name: "note", Kind: typedText, Native: "", Nullable: true},
		{Name: "data", Kind: typedBinary, Native: "", Nullable: false}, {Name: dmlAmountColumn, Kind: typedDecimal, Native: "", Nullable: true},
		{Name: "seen_at", Kind: typedTimestamp, Native: "", Nullable: true}} {
		for _, capability := range RuntimeCapabilities(engine) {
			if capability.Feature == typeFeaturePrefix+column.Kind && capability.Status == capabilitySupported {
				column.Native = capability.NativeType
				columns = append(columns, column)
			}
		}
	}
	return columns
}

// DMLWritten reports whether an operation writes a column. A batch leaves amount to its
// NULL default, so that its row holds only values the DuckDB appender and the ClickHouse
// batch insert store as they are and the generated method takes those paths.
//
// Takes operation (string) which names the statement family.
// Takes column (DMLColumn) which is a non-key column.
//
// Returns bool which is false only for a batch's amount.
func DMLWritten(operation string, column DMLColumn) bool {
	return operation != DMLBatch || column.Name != dmlAmountColumn
}

// dmlSupported reports whether an engine runs an operation, with the reason when not.
//
// Takes engine (string) which selects the engine contract.
// Takes operation (string) which names the statement family.
//
// Returns bool which admits the operation.
// Returns string which records the engine, driver or Seagull reason for an exclusion.
func dmlSupported(engine, operation string) (bool, string) {
	if !supportsShape(engine, shapeSelect) {
		return false, "The engine has no native fixture."
	}
	if reason, excluded := dmlExclusions[engine][operation]; excluded {
		return false, reason
	}
	return true, ""
}

// DMLCapabilities records every operation's admission for audit reports.
//
// Takes engine (string) which selects the engine contract.
//
// Returns []Capability which lists admitted operations and exclusions with reasons.
func DMLCapabilities(engine string) []Capability {
	capabilities := make([]Capability, 0, len(dmlOperations)+1)
	for _, operation := range dmlOperations {
		supported, reason := dmlSupported(engine, operation)
		capability := Capability{Feature: "dml/" + operation, NativeType: "", CastType: "", Status: capabilitySupported, Reason: ""}
		if !supported {
			capability.Status, capability.Reason = capabilityUnsupportedNative, reason
		}
		capabilities = append(capabilities, capability)
	}
	affected := Capability{Feature: "dml/affected-rows", NativeType: "", CastType: "", Status: capabilitySupported,
		Reason: "MySQL and MariaDB count changed rows because go-sql-driver/mysql does not set CLIENT_FOUND_ROWS by default."}
	if engine == engineClickHouse {
		affected.Status, affected.Reason = capabilityUnsupportedNative, "clickhouse-go reports no affected-row count."
	}
	return append(capabilities, affected)
}

// DMLSeeds reaches every admitted operation, NULL and nil value and failure.
//
// Takes engine (string) which selects admitted operations.
//
// Returns [][]byte which supplies deterministic version 17 inputs.
func DMLSeeds(engine string) [][]byte {
	if !supportsShape(engine, shapeSelect) {
		return nil
	}
	var seeds [][]byte
	prefix := append([]byte{DMLVersion, 0}, dmlSeedSetup...)
	for index, operation := range dmlOperations {
		if supported, _ := dmlSupported(engine, operation); !supported || operation == DMLTransaction {
			continue
		}
		step := []byte{byte(index)}
		seeds = append(seeds, slices.Concat(prefix, step, []byte(dmlSeedPlain)), slices.Concat(prefix, step, []byte(dmlSeedNulls)))
	}
	for _, operation := range []string{DMLBatch, DMLCopy, DMLUpsertBatch} {
		if supported, _ := dmlSupported(engine, operation); supported {
			step := []byte{dmlOperationChoice(operation)}
			seeds = append(seeds, slices.Concat(prefix, step, []byte(dmlSeedBulk)), slices.Concat(prefix, step, []byte(dmlSeedConflict)),
				slices.Concat(prefix, step, []byte(dmlSeedBatchNulls)))
		}
	}
	if supported, _ := dmlSupported(engine, DMLTransaction); supported {
		transaction := dmlOperationChoice(DMLTransaction)
		batch := dmlOperationChoice(DMLBatch)
		for _, commit := range []byte{0, dmlCommitFlag} {
			seeds = append(seeds, slices.Concat(prefix, []byte{transaction, commit}, []byte(dmlSeedTransaction), []byte{batch}, []byte(dmlSeedTransactionBatch)))
		}
	}
	return seeds
}

// dmlOperationChoice finds the choice that selects an operation.
//
// Takes operation (string) which names the statement family.
//
// Returns byte which decodes to that operation where the engine admits it.
func dmlOperationChoice(operation string) byte {
	for index, candidate := range dmlOperations {
		if candidate == operation {
			return byte(index)
		}
	}
	return 0
}

// DecodeDML decodes a version 17 input and computes its outcomes independently.
//
// Takes engine (string) which selects syntax and engine contracts.
// Takes data ([]byte) which holds the versioned choices.
//
// Returns DMLCase which supplies the statements and their expected outcomes.
// Returns bool which rejects other versions, oversized inputs and unsupported engines.
func DecodeDML(engine string, data []byte) (DMLCase, bool) {
	if len(data) < 2 || len(data) > ScenarioMaxBytes || data[0] != DMLVersion || !supportsShape(engine, shapeSelect) {
		return DMLCase{}, false
	}
	decoder := &dmlDecoder{data: data, position: 2, engine: engine, fresh: dmlFreshBase, bulk: 0, columns: dmlColumns(engine)}
	model := &dmlModel{engine: engine, rows: map[int64]DMLRow{}}
	result := DMLCase{Engine: engine, Data: append([]byte(nil), data...), Steps: nil, Columns: decoder.columns}
	for len(result.Steps) < dmlStepBound && decoder.position < len(data) {
		step := decoder.step(false)
		model.apply(&step)
		result.Steps = append(result.Steps, step)
	}
	return result, true
}

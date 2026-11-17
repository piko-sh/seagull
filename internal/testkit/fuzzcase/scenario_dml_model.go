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
	"maps"
	"slices"
	"strconv"
)

const (
	// dmlQuantityColumn locates qty among the non-key values.
	dmlQuantityColumn = 0

	// dmlNoteColumn locates note among the non-key values.
	dmlNoteColumn = 1

	// dmlUpsertChangedCount is what MySQL and MariaDB report for an upsert that changes a
	// row.
	dmlUpsertChangedCount = 2
)

// dmlDecoder reads version 17 choices with zero defaults past the input.
type dmlDecoder struct {
	// engine selects admitted operations and identifier rules.
	engine string

	// data holds the replay input.
	data []byte

	// columns lists the non-key columns.
	columns []DMLColumn

	// position is the next unread byte.
	position int

	// fresh is the next identifier that no ordinary statement chooses.
	fresh int64

	// bulk counts the bulk writes decoded so far.
	bulk int64
}

// dmlCounted lists the operations whose methods report an affected-row count. A copyfrom
// method reports one only on pgx, where it is the number of rows copied.
var dmlCounted = []string{DMLInsertCount, DMLInsertPair, DMLUpdate, DMLUpdateNote, DMLDelete, DMLUpsert, DMLCopy}

// dmlModel holds the independent table state.
type dmlModel struct {
	// rows maps each identifier to its row.
	rows map[int64]DMLRow

	// engine selects the affected-row contract.
	engine string
}

// next reads one choice.
//
// Returns byte which is zero past the input.
func (decoder *dmlDecoder) next() byte {
	value := scenarioByte(decoder.data, decoder.position)
	decoder.position++
	return value
}

// step decodes one statement.
//
// Takes inner (bool) which excludes nested transactions.
//
// Returns DMLStep which holds the statement without its outcome.
func (decoder *dmlDecoder) step(inner bool) DMLStep {
	choice := int(decoder.next())
	operation := ""
	for offset := range dmlOperations {
		candidate := dmlOperations[(choice+offset)%len(dmlOperations)]
		if supported, _ := dmlSupported(decoder.engine, candidate); supported && (!inner || candidate != DMLTransaction) {
			operation = candidate
			break
		}
	}
	step := DMLStep{Operation: operation, Rows: nil, Inner: nil, Returned: nil, State: nil, Value: TypedValue{}, Key: 0,
		Affected: 0, HasAffected: false, Fails: false, Commit: false}
	switch operation {
	case DMLInsert, DMLInsertCount, DMLInsertReturning, DMLUpsert:
		step.Rows = []DMLRow{decoder.row()}
	case DMLInsertPair:
		step.Rows = []DMLRow{decoder.row(), decoder.row()}
	case DMLUpdate, DMLUpdateReturning:
		step.Value = scenarioValue(typedSigned, decoder.next()&^scenarioNullFlag)
		step.Key = decoder.key()
	case DMLUpdateNote:
		step.Value = scenarioValue(typedText, decoder.next())
		step.Key = dmlNumber(scenarioValue(typedSigned, decoder.next()&^scenarioNullFlag))
	case DMLDelete, DMLDeleteReturning:
		step.Key = decoder.key()
	case DMLBatch, DMLUpsertBatch, DMLCopy:
		step.Rows = decoder.rows(operation)
	case DMLTransaction:
		step.Commit = decoder.next()&dmlCommitFlag != 0
		for range 1 + int(decoder.next())%dmlInnerBound {
			step.Inner = append(step.Inner, decoder.step(true))
		}
	}
	return step
}

// key reads an identifier from the small key space.
//
// Returns int64 which lies between one and the key space.
func (decoder *dmlDecoder) key() int64 {
	return 1 + int64(decoder.next()&^(dmlNilFlag|scenarioNullFlag))%dmlKeySpace
}

// row decodes a row from an identifier, a value and a NULL mask choice.
//
// Returns DMLRow which has a fresh identifier on ClickHouse, which has no unique keys.
func (decoder *dmlDecoder) row() DMLRow {
	identity := decoder.next()
	row := decoder.values(decoder.next(), decoder.next())
	row.NilBytes = identity&dmlNilFlag != 0
	row.ID = 1 + int64(identity&^(dmlNilFlag|scenarioNullFlag))%dmlKeySpace
	if decoder.engine == engineClickHouse {
		row.ID = decoder.fresh
		decoder.fresh++
	}
	return row
}

// values builds the non-key values of a row.
//
// Takes choice (byte) which selects each column's boundary.
// Takes nulls (byte) which holds one NULL bit per nullable column.
//
// Returns DMLRow which has no identifier yet.
func (decoder *dmlDecoder) values(choice, nulls byte) DMLRow {
	row := DMLRow{Values: make([]TypedValue, len(decoder.columns)), ID: 0, NilBytes: false}
	for index, column := range decoder.columns {
		value := scenarioValue(column.Kind, (choice+byte(index))&^scenarioNullFlag)
		if column.Nullable && nulls&(1<<index) != 0 {
			value = TypedNumber(column.Kind, typedZero, 0)
			value.Null = true
		}
		row.Values[index] = value
	}
	return row
}

// rows decodes the rows of a batch or copy.
//
// Takes operation (string) which names the statement. An upsert batch draws identifiers
// from the key space, as upserts meet rows.
//
// Returns []DMLRow which has distinct fresh identifiers unless upserted or conflicting,
// and NULL in each column the operation leaves to its default.
func (decoder *dmlDecoder) rows(operation string) []DMLRow {
	rows := decoder.batchRows(operation == DMLUpsertBatch)
	for index, column := range decoder.columns {
		if DMLWritten(operation, column) {
			continue
		}
		for position := range rows {
			rows[position].Values[index] = TypedNumber(column.Kind, typedZero, 0)
			rows[position].Values[index].Null = true
		}
	}
	return rows
}

// batchRows decodes the rows of a batch or copy with every column chosen.
//
// Takes keyed (bool) which draws identifiers from the key space.
//
// Returns []DMLRow which has distinct fresh identifiers unless keyed or conflicting.
func (decoder *dmlDecoder) batchRows(keyed bool) []DMLRow {
	shape := decoder.next()
	if shape&dmlBulkFlag != 0 && decoder.bulk < dmlBulkWrites {
		rows := make([]DMLRow, dmlBulkBound)
		for index := range rows {
			rows[index] = decoder.values(byte(index), byte(index))
			rows[index].ID = dmlBulkBase + decoder.bulk*dmlBulkBound + int64(index)
		}
		decoder.bulk++
		return rows
	}
	rows := make([]DMLRow, 1+int(shape)%dmlBatchBound)
	for index := range rows {
		rows[index] = decoder.row()
		if !keyed && decoder.engine != engineClickHouse {
			rows[index].ID = decoder.fresh
			decoder.fresh++
		}
	}
	if shape&dmlConflictFlag != 0 && decoder.engine != engineClickHouse {
		rows[0].ID = 1 + int64(shape&^(dmlConflictFlag|dmlBulkFlag))%dmlKeySpace
	}
	return rows
}

// apply computes a statement's outcome and the table after it.
//
// Takes step (*DMLStep) which receives its outcome and state.
func (model *dmlModel) apply(step *DMLStep) {
	step.HasAffected = engineCountsRows(model.engine) && slices.Contains(dmlCounted, step.Operation)
	switch step.Operation {
	case DMLInsert, DMLInsertCount, DMLInsertReturning, DMLInsertPair, DMLBatch, DMLCopy:
		model.insert(step)
	case DMLUpsert, DMLUpsertBatch:
		for _, row := range step.Rows {
			step.Affected += model.upsert(row)
		}
	case DMLUpdate, DMLUpdateReturning, DMLUpdateNote:
		model.update(step)
	case DMLDelete, DMLDeleteReturning:
		for _, identifier := range slices.Sorted(maps.Keys(model.rows)) {
			if identifier == step.Key || (step.Operation == DMLDeleteReturning && identifier >= step.Key) {
				if step.Operation == DMLDeleteReturning {
					step.Returned = append(step.Returned, DMLRow{Values: nil, ID: identifier, NilBytes: false})
				}
				delete(model.rows, identifier)
				step.Affected++
			}
		}
	case DMLTransaction:
		model.transaction(step)
	}
	step.State = model.state()
}

// insert adds rows unless a key exists or repeats, when the whole statement fails.
//
// Takes step (*DMLStep) which supplies the rows and receives the outcome.
func (model *dmlModel) insert(step *DMLStep) {
	seen := map[int64]bool{}
	for _, row := range step.Rows {
		_, exists := model.rows[row.ID]
		if (exists || seen[row.ID]) && model.engine != engineClickHouse {
			step.Fails = true
			return
		}
		seen[row.ID] = true
	}
	for _, row := range step.Rows {
		model.rows[row.ID] = row
	}
	step.Affected = int64(len(step.Rows))
	if step.Operation == DMLInsertReturning {
		step.Returned = []DMLRow{step.Rows[0]}
	}
}

// upsert inserts a row or replaces the quantity and note of the existing one.
//
// Takes row (DMLRow) which supplies the values.
//
// Returns int64 which follows the engine's count. MySQL and MariaDB report two for a
// changed row and zero for an unchanged one.
func (model *dmlModel) upsert(row DMLRow) int64 {
	existing, exists := model.rows[row.ID]
	if !exists {
		model.rows[row.ID] = row
		return 1
	}
	updated := existing
	updated.Values = slices.Clone(existing.Values)
	updated.Values[dmlQuantityColumn], updated.Values[dmlNoteColumn] = row.Values[dmlQuantityColumn], row.Values[dmlNoteColumn]
	model.rows[row.ID] = updated
	if !countsChangedRows(model.engine) {
		return 1
	}
	if sameValue(existing.Values[dmlQuantityColumn], row.Values[dmlQuantityColumn]) && sameValue(existing.Values[dmlNoteColumn], row.Values[dmlNoteColumn]) {
		return 0
	}
	return dmlUpsertChangedCount
}

// update sets the quantity or note of the selected rows.
//
// Takes step (*DMLStep) which supplies the filter and receives the outcome.
func (model *dmlModel) update(step *DMLStep) {
	for _, identifier := range slices.Sorted(maps.Keys(model.rows)) {
		row := model.rows[identifier]
		column, selected := dmlQuantityColumn, identifier == step.Key
		switch step.Operation {
		case DMLUpdateReturning:
			selected = identifier >= step.Key
		case DMLUpdateNote:
			column, selected = dmlNoteColumn, dmlNumber(row.Values[dmlQuantityColumn]) >= step.Key
		}
		if !selected {
			continue
		}
		if !countsChangedRows(model.engine) || !sameValue(row.Values[column], step.Value) {
			step.Affected++
		}
		row.Values = slices.Clone(row.Values)
		row.Values[column] = step.Value
		model.rows[identifier] = row
		if step.Operation == DMLUpdateReturning {
			step.Returned = append(step.Returned, DMLRow{Values: []TypedValue{step.Value}, ID: identifier, NilBytes: false})
		}
	}
}

// transaction applies the statements to a copy, dropping those the engine refuses so the
// transaction stays usable, and keeps the copy only on commit.
//
// Takes step (*DMLStep) which supplies the statements and receives their outcomes.
func (model *dmlModel) transaction(step *DMLStep) {
	inner := &dmlModel{rows: maps.Clone(model.rows), engine: model.engine}
	var kept []DMLStep
	for index := range step.Inner {
		statement := &step.Inner[index]
		inner.apply(statement)
		if !statement.Fails {
			kept = append(kept, *statement)
		}
	}
	step.Inner = kept
	if step.Commit {
		model.rows = inner.rows
	}
}

// state lists the rows by identifier.
//
// Returns []DMLRow which is a snapshot of the table.
func (model *dmlModel) state() []DMLRow {
	rows := make([]DMLRow, 0, len(model.rows))
	for _, identifier := range slices.Sorted(maps.Keys(model.rows)) {
		rows = append(rows, model.rows[identifier])
	}
	return rows
}

// engineCountsRows reports whether the driver returns affected-row counts.
//
// Takes engine (string) which selects the driver contract.
//
// Returns bool which is false for ClickHouse, whose driver reports none.
func engineCountsRows(engine string) bool {
	return engine != engineClickHouse
}

// countsChangedRows reports whether the driver counts changed rather than matched rows.
//
// Takes engine (string) which selects the driver contract.
//
// Returns bool which is true for MySQL and MariaDB without CLIENT_FOUND_ROWS.
func countsChangedRows(engine string) bool {
	return mysqlFamily(engine)
}

// sameValue compares two modelled values as the engine compares stored values.
//
// Takes left (TypedValue) which is one value.
// Takes right (TypedValue) which is the other value.
//
// Returns bool which treats two NULLs as the same stored value.
func sameValue(left, right TypedValue) bool {
	if left.Null || right.Null {
		return left.Null == right.Null
	}
	return left.Number == right.Number && left.Text == right.Text && slices.Equal(left.Bytes, right.Bytes) && left.Scale == right.Scale
}

// dmlNumber reads a signed modelled value.
//
// Takes value (TypedValue) which holds a signed boundary.
//
// Returns int64 which is the value, or zero when it is not a signed integer.
func dmlNumber(value TypedValue) int64 {
	number, err := strconv.ParseInt(value.Number, decimalBase, scalarBitWidth)
	if err != nil {
		return 0
	}
	return number
}

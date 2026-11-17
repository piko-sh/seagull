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

package fuzznative

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

// dmlStateRows bounds the table rows recorded after a statement other than the last.
const dmlStateRows = 32

// DMLObservation records what one version 17 statement did through generated code.
type DMLObservation struct {
	// Error describes the statement's failure.
	Error string

	// StateError describes a failed read of the table after the statement.
	StateError string

	// Returned holds the rows RETURNING yielded, ordered by identifier.
	Returned []DMLObservedRow

	// State holds the table after the statement, read through the generated read.
	State []DMLObservedRow

	// Inner holds the observations of a transaction's statements.
	Inner []DMLObservation

	// Rows counts the table's rows after the statement.
	Rows int

	// Affected is the reported count when Counted is set.
	Affected int64

	// Counted records that the method returned a count.
	Counted bool

	// Partial records that State was left out to keep the response within its bound. Rows
	// still counts the table.
	Partial bool
}

// DMLObservedRow is one generated row.
type DMLObservedRow struct {
	// Values holds the non-key fields observed.
	Values []fuzzcase.TypedValue

	// ID is the row's identifier.
	ID int64
}

// dmlExecution holds one sequence's session and columns.
type dmlExecution struct {
	// session owns the connection and transactions.
	session *Session

	// columns describes the non-key columns.
	columns []fuzzcase.DMLColumn
}

// countRows counts the table read after the statement.
func (observation *DMLObservation) countRows() {
	observation.Rows = len(observation.State)
}

// omitLargeState counts the table and leaves out a large table's rows. The oracle
// compares the table after the last statement whole, so only earlier ones use this.
func (observation *DMLObservation) omitLargeState() {
	observation.countRows()
	if observation.Rows > dmlStateRows {
		observation.State, observation.Partial = nil, true
	}
}

// transaction runs statements through a transaction receiver and ends it.
//
// Takes step (*fuzzcase.DMLStep) which supplies the statements and the ending.
//
// Returns DMLObservation which records the inner observations and any failure.
func (execution *dmlExecution) transaction(ctx context.Context, step *fuzzcase.DMLStep) DMLObservation {
	observation := DMLObservation{}
	if execution.session.Begin == nil {
		observation.Error = "the mode has no transactions"
		return observation
	}
	transaction, err := execution.session.Begin(ctx)
	if err != nil {
		observation.Error = "begin: " + err.Error()
		return observation
	}
	if !transaction.complete() {
		observation.Error = "begin: the transaction factory returned incomplete operations"
		return observation
	}
	receiver := reflect.ValueOf(transaction.Queries)
	for index := range step.Inner {
		result := execution.statement(ctx, receiver, &step.Inner[index])
		result.State, result.StateError = execution.state(ctx, receiver)
		result.omitLargeState()
		observation.Inner = append(observation.Inner, result)
	}
	end := transaction.Rollback
	if step.Commit {
		end = transaction.Commit
	}
	if err := end(ctx); err != nil {
		observation.Error = "end: " + err.Error()
	}
	return observation
}

// statement runs one generated method.
//
// Takes receiver (reflect.Value) which is the generated receiver.
// Takes step (*fuzzcase.DMLStep) which supplies the statement.
//
// Returns DMLObservation which records the count, returned rows or failure.
func (execution *dmlExecution) statement(ctx context.Context, receiver reflect.Value, step *fuzzcase.DMLStep) DMLObservation {
	observation := DMLObservation{}
	method := receiver.MethodByName(fuzzcase.DMLMethod(step.Operation))
	if !method.IsValid() {
		observation.Error = "required generated method is missing"
		return observation
	}
	arguments, err := execution.arguments(ctx, method.Type(), step)
	if err != nil {
		observation.Error = "arguments: " + err.Error()
		return observation
	}
	values, err := call(method, arguments...)
	if err != nil {
		observation.Error = err.Error()
		return observation
	}
	if len(values) == 1 && values[0].CanInt() {
		observation.Affected, observation.Counted = values[0].Int(), true
	} else if len(values) == 1 {
		observation.Returned, err = execution.rows(values[0])
		if err != nil {
			observation.Error = "returned rows: " + err.Error()
		}
	}
	return observation
}

// arguments binds a statement's values to the generated method.
//
// Takes method (reflect.Type) which is the generated method's type.
// Takes step (*fuzzcase.DMLStep) which supplies the rows or the value and key.
//
// Returns []reflect.Value which can be passed to the method.
// Returns error when the generated parameters do not fit the model.
func (execution *dmlExecution) arguments(ctx context.Context, method reflect.Type, step *fuzzcase.DMLStep) ([]reflect.Value, error) {
	key := fuzzcase.TypedNumber(kindSigned, strconv.FormatInt(step.Key, scalarBase), 0)
	switch step.Operation {
	case fuzzcase.DMLUpdate, fuzzcase.DMLUpdateNote, fuzzcase.DMLUpdateReturning:
		return callArguments(ctx, method, []fuzzcase.TypedValue{step.Value, key}, nil)
	case fuzzcase.DMLDelete, fuzzcase.DMLDeleteReturning:
		return callArguments(ctx, method, []fuzzcase.TypedValue{key}, nil)
	case fuzzcase.DMLBatch, fuzzcase.DMLUpsertBatch, fuzzcase.DMLCopy:
		return execution.batchArguments(ctx, method, step)
	default:
		return execution.rowArguments(ctx, method, step)
	}
}

// batchArguments binds a batch statement's rows as one slice of parameter structs.
//
// Takes method (reflect.Type) which is the generated batch method's type.
// Takes step (*fuzzcase.DMLStep) which supplies the rows.
//
// Returns []reflect.Value which can be passed to the method.
// Returns error when the generated parameters do not fit the model.
func (execution *dmlExecution) batchArguments(ctx context.Context, method reflect.Type, step *fuzzcase.DMLStep) ([]reflect.Value, error) {
	if method.NumIn() != 2 || method.In(1).Kind() != reflect.Slice {
		return nil, errors.New("generated batch method does not take a slice")
	}
	items := reflect.MakeSlice(method.In(1), 0, len(step.Rows))
	for _, row := range step.Rows {
		values, nilBytes, err := rowValues(row, execution.columns, step.Operation)
		if err != nil {
			return nil, err
		}
		item, err := fillParameters(method.In(1).Elem(), values, nilBytes)
		if err != nil {
			return nil, err
		}
		items = reflect.Append(items, item)
	}
	return []reflect.Value{reflect.ValueOf(ctx), items}, nil
}

// rowArguments binds every row's values in order to one generated call.
//
// Takes method (reflect.Type) which is the generated method's type.
// Takes step (*fuzzcase.DMLStep) which supplies the rows.
//
// Returns []reflect.Value which can be passed to the method.
// Returns error when the generated parameters do not fit the model.
func (execution *dmlExecution) rowArguments(ctx context.Context, method reflect.Type, step *fuzzcase.DMLStep) ([]reflect.Value, error) {
	var values []fuzzcase.TypedValue
	var nilBytes []bool
	for _, row := range step.Rows {
		written, nils, err := rowValues(row, execution.columns, step.Operation)
		if err != nil {
			return nil, err
		}
		values, nilBytes = append(values, written...), append(nilBytes, nils...)
	}
	return callArguments(ctx, method, values, nilBytes)
}

// state reads the table through the generated read.
//
// Takes receiver (reflect.Value) which is the generated receiver.
//
// Returns []DMLObservedRow which holds the rows in identifier order.
// Returns string which describes a failed read.
func (execution *dmlExecution) state(ctx context.Context, receiver reflect.Value) ([]DMLObservedRow, string) {
	values, err := call(receiver.MethodByName(fuzzcase.DMLRowsMethod), reflect.ValueOf(ctx))
	if err != nil {
		return nil, err.Error()
	}
	if len(values) != 1 {
		return nil, "the generated read returned the wrong shape"
	}
	rows, err := execution.rows(values[0])
	if err != nil {
		return nil, err.Error()
	}
	return rows, ""
}

// rows observes a generated row or slice of rows, ordered by identifier.
//
// Takes value (reflect.Value) which is a row struct or a slice of them.
//
// Returns []DMLObservedRow which holds the identifier and each further field.
// Returns error when a field does not represent its column's family.
func (execution *dmlExecution) rows(value reflect.Value) ([]DMLObservedRow, error) {
	items := []reflect.Value{value}
	if value.Kind() == reflect.Slice {
		if value.Len() > nativeRowBound {
			return nil, errors.New("generated rows exceed the row bound")
		}
		items = items[:0]
		for index := range value.Len() {
			items = append(items, value.Index(index))
		}
	}
	rows := make([]DMLObservedRow, 0, len(items))
	for _, item := range items {
		row, err := execution.row(followValue(item))
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	slices.SortStableFunc(rows, func(left, right DMLObservedRow) int { return cmp.Compare(left.ID, right.ID) })
	return rows, nil
}

// row observes one generated row whose first field is the identifier and whose further
// fields follow the table's columns.
//
// Takes value (reflect.Value) which is a generated row struct.
//
// Returns DMLObservedRow which holds the observed values.
// Returns error when the row's shape or a field does not fit.
func (execution *dmlExecution) row(value reflect.Value) (DMLObservedRow, error) {
	if !value.IsValid() || value.Kind() != reflect.Struct || value.NumField() == 0 || value.NumField() > len(execution.columns)+1 {
		return DMLObservedRow{}, errors.New("generated row has the wrong shape")
	}
	identifier := followValue(value.Field(0))
	if !identifier.IsValid() || !identifier.CanInt() {
		return DMLObservedRow{}, errors.New("generated row identifier is not an integer")
	}
	row := DMLObservedRow{ID: identifier.Int(), Values: nil}
	for index := 1; index < value.NumField(); index++ {
		column := execution.columns[index-1]
		field := value.Field(index)
		null := column.Nullable && nilValue(followValue(field))
		observed, err := Observed(field, column.Kind, null)
		if err != nil {
			return row, fmt.Errorf("%s: %w", column.Name, err)
		}
		row.Values = append(row.Values, observed)
	}
	return row, nil
}

// runDML runs every statement of a sequence and observes the table after each.
//
// Takes session (*Session) which owns the generated API.
// Takes model (fuzzcase.DMLCase) which supplies the statements.
//
// Returns []DMLObservation which holds one observation per statement.
func runDML(ctx context.Context, session *Session, model fuzzcase.DMLCase) []DMLObservation {
	execution := &dmlExecution{session: session, columns: model.Columns}
	queries := reflect.ValueOf(session.Queries)
	observations := make([]DMLObservation, 0, len(model.Steps))
	for index := range model.Steps {
		step := &model.Steps[index]
		var observation DMLObservation
		if step.Operation == fuzzcase.DMLTransaction {
			observation = execution.transaction(ctx, step)
		} else {
			observation = execution.statement(ctx, queries, step)
		}
		observation.State, observation.StateError = execution.state(ctx, queries)
		if index == len(model.Steps)-1 {
			observation.countRows()
		} else {
			observation.omitLargeState()
		}
		observations = append(observations, observation)
	}
	return observations
}

// rowValues lists a row's identifier and values with the nil-slice choices.
//
// Takes row (fuzzcase.DMLRow) which supplies the values.
// Takes columns ([]fuzzcase.DMLColumn) which describes the non-key columns.
// Takes operation (string) which selects the columns the statement writes.
//
// Returns []fuzzcase.TypedValue which starts with the identifier.
// Returns []bool which marks the empty binary values to send as nil slices.
// Returns error when the row has more values than the table has columns.
func rowValues(row fuzzcase.DMLRow, columns []fuzzcase.DMLColumn, operation string) ([]fuzzcase.TypedValue, []bool, error) {
	if len(row.Values) > len(columns) {
		return nil, nil, fmt.Errorf("row %d has %d values for %d columns", row.ID, len(row.Values), len(columns))
	}
	values := []fuzzcase.TypedValue{fuzzcase.TypedNumber(kindSigned, strconv.FormatInt(row.ID, scalarBase), 0)}
	for index, value := range row.Values {
		if fuzzcase.DMLWritten(operation, columns[index]) {
			values = append(values, value)
		}
	}
	nilBytes := make([]bool, len(values))
	for index, value := range values {
		nilBytes[index] = row.NilBytes && value.Kind == kindBinary
	}
	return values, nilBytes, nil
}

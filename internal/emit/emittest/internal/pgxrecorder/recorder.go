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

// Package pgxrecorder records generated code's interactions with pgx.
//
// Connections record statements and arguments, and queries return no rows. Batches and
// CopyFrom record their statements or rows. The harness reproduces pgx's restrictions on
// unregistered enum containers so tests catch unsupported bindings and scans.
package pgxrecorder

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"seagull.sh/seagull/internal/emit/emittest/internal/stubs/pgx"
	"seagull.sh/seagull/internal/emit/emittest/internal/stubs/pgx/pgconn"
)

const (
	// valueMark stands, in the INSERT a CopyFrom row is recorded as, before the number of
	// the row's value whose placeholder belongs there. CopyFrom sends no SQL of its own, so
	// the suite writes the dialect's own placeholder in place of each mark and its number.
	valueMark = "\x00"

	// maxTypeDepth bounds the walk through a type's elements and fields.
	maxTypeDepth = 16
)

// Statement is one statement the connection received: its SQL and its arguments, each as
// the generated code passed it.
type Statement struct {
	// SQL is the statement's text.
	SQL string

	// Arguments holds the arguments in order.
	Arguments []any
}

var (
	// errUnregisteredType reports a named string type held in a slice, an array, a map or a
	// struct, whose type pgx does not know without registering it on the connection.
	errUnregisteredType = errors.New("a named string type held in a container has no registered type; bind or scan plain strings")

	// recordedMutex guards recorded.
	recordedMutex sync.Mutex

	// recorded holds the statements received since the last Take.
	recorded []Statement
)

// Connection is the recording connection, which is also a transaction.
type Connection struct{}

// Begin starts a transaction on the recording connection.
//
// Returns pgx.Tx which is the transaction.
// Returns error which is always nil.
func (connection *Connection) Begin(context.Context) (pgx.Tx, error) {
	return connection, nil
}

// Commit ends a transaction.
//
// Returns error which is always nil.
func (*Connection) Commit(context.Context) error {
	return nil
}

// Rollback ends a transaction.
//
// Returns error which is always nil.
func (*Connection) Rollback(context.Context) error {
	return nil
}

// Exec records a statement that changes nothing.
//
// Takes query (string) which is the statement's text.
// Takes arguments (...any) which are its arguments.
//
// Returns pgconn.CommandTag which reports no rows changed.
// Returns error when an argument cannot be encoded.
func (*Connection) Exec(_ context.Context, query string, arguments ...any) (pgconn.CommandTag, error) {
	if err := encode(arguments); err != nil {
		return pgconn.NewCommandTag(0), err
	}
	record(query, arguments)
	return pgconn.NewCommandTag(0), nil
}

// Query records a query and answers it with no rows.
//
// Takes query (string) which is the statement's text.
// Takes arguments (...any) which are its arguments.
//
// Returns pgx.Rows which holds no rows.
// Returns error when an argument cannot be encoded.
func (*Connection) Query(_ context.Context, query string, arguments ...any) (pgx.Rows, error) {
	if err := encode(arguments); err != nil {
		return nil, err
	}
	record(query, arguments)
	return emptyRows{}, nil
}

// QueryRow records a query read for one row, which it does not find.
//
// Takes query (string) which is the statement's text.
// Takes arguments (...any) which are its arguments.
//
// Returns pgx.Row which reports pgx.ErrNoRows, or the error encoding an argument met.
func (*Connection) QueryRow(_ context.Context, query string, arguments ...any) pgx.Row {
	if err := encode(arguments); err != nil {
		return failedRow{err: err}
	}
	record(query, arguments)
	return missingRow{}
}

// SendBatch records each statement of a batch, or none when an argument of one cannot be
// encoded.
//
// Takes batch (*pgx.Batch) which holds the statements.
//
// Returns pgx.BatchResults which reports no rows for each, or the encoding error.
func (*Connection) SendBatch(_ context.Context, batch *pgx.Batch) pgx.BatchResults {
	for _, queued := range batch.QueuedQueries {
		if err := encode(queued.Arguments); err != nil {
			return batchResults{err: err}
		}
	}
	for _, queued := range batch.QueuedQueries {
		record(queued.SQL, queued.Arguments)
	}
	return batchResults{err: nil}
}

// CopyFrom records each row it sends as the INSERT the row stands for, with a value mark
// and the value's number where each placeholder goes.
//
// Takes tableName (pgx.Identifier) which names the table.
// Takes columnNames ([]string) which name the columns.
// Takes rowSource (pgx.CopyFromSource) which yields the rows.
//
// Returns int64 which is the number of rows sent.
// Returns error when a row reports one.
func (*Connection) CopyFrom(_ context.Context, tableName pgx.Identifier, columnNames []string, rowSource pgx.CopyFromSource) (int64, error) {
	placeholders := make([]string, len(columnNames))
	for index := range placeholders {
		placeholders[index] = valueMark + strconv.Itoa(index+1)
	}
	insert := "INSERT INTO " + strings.Join(tableName, ".") + " (" + strings.Join(columnNames, ", ") + ") VALUES (" + strings.Join(placeholders, ", ") + ")"
	var count int64
	for rowSource.Next() {
		values, err := rowSource.Values()
		if err == nil {
			err = encode(values)
		}
		if err != nil {
			return count, err
		}
		record(insert, values)
		count++
	}
	return count, rowSource.Err()
}

// emptyRows is a result with no rows.
type emptyRows struct{}

// Close ends the result.
func (emptyRows) Close() {}

// Err reports no error.
//
// Returns error which is always nil.
func (emptyRows) Err() error {
	return nil
}

// Next reports the end of the result.
//
// Returns bool which is always false.
func (emptyRows) Next() bool {
	return false
}

// Scan reads no row.
//
// Returns error which is always pgx.ErrNoRows.
func (emptyRows) Scan(_ ...any) error {
	return pgx.ErrNoRows
}

// Values returns no values.
//
// Returns []any which is nil.
// Returns error which is always pgx.ErrNoRows.
func (emptyRows) Values() ([]any, error) {
	return nil, pgx.ErrNoRows
}

// missingRow is a row read from a result with no rows.
type missingRow struct{}

// Scan reports that the row was not found, after checking that pgx could decode into
// every destination.
//
// Takes destinations (...any) which are the destinations.
//
// Returns error which is pgx.ErrNoRows, or the error a destination meets.
func (missingRow) Scan(destinations ...any) error {
	if err := decode(destinations); err != nil {
		return err
	}
	return pgx.ErrNoRows
}

// failedRow is a row read from a query whose arguments could not be encoded.
type failedRow struct {
	// err is the encoding error.
	err error
}

// Scan reports the error encoding the query's arguments met.
//
// Returns error which is the encoding error.
func (row failedRow) Scan(_ ...any) error {
	return row.err
}

// batchResults reads the results of a recorded batch.
type batchResults struct {
	// err is the error that refused the batch, or nil.
	err error
}

// Exec reads a statement's tag.
//
// Returns pgconn.CommandTag which reports no rows changed.
// Returns error which is the error that refused the batch, or nil.
func (results batchResults) Exec() (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag(0), results.err
}

// Query reads a statement's rows.
//
// Returns pgx.Rows which holds no rows.
// Returns error which is the error that refused the batch, or nil.
func (results batchResults) Query() (pgx.Rows, error) {
	if results.err != nil {
		return nil, results.err
	}
	return emptyRows{}, nil
}

// QueryRow reads a statement's row.
//
// Returns pgx.Row which reports pgx.ErrNoRows, or the error that refused the batch.
func (results batchResults) QueryRow() pgx.Row {
	if results.err != nil {
		return failedRow(results)
	}
	return missingRow{}
}

// Close ends the batch.
//
// Returns error which is always nil.
func (batchResults) Close() error {
	return nil
}

// Connect returns a recording connection.
//
// Returns *Connection which records every statement run on it.
func Connect() *Connection {
	return &Connection{}
}

// Take returns the statements received since the last call, in order, and forgets them.
//
// Returns []Statement which holds the statements.
//
// Concurrency: atomically drains the shared recording under its mutex.
func Take() []Statement {
	recordedMutex.Lock()
	defer recordedMutex.Unlock()
	taken := recorded
	recorded = nil
	return taken
}

// unregisteredType reports whether a type, after any pointers, is a slice, an array, a
// map or a struct holding a named string type anywhere within it.
//
// Takes candidate (reflect.Type) which is the type, or nil.
//
// Returns bool which is true for such a type.
func unregisteredType(candidate reflect.Type) bool {
	for candidate != nil && candidate.Kind() == reflect.Pointer {
		candidate = candidate.Elem()
	}
	if candidate == nil {
		return false
	}
	switch candidate.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map, reflect.Struct:
		return holdsNamedString(candidate, 0)
	default:
		return false
	}
}

// holdsNamedString reports whether a type is a named string type or holds one, through
// pointers, slices, arrays, map keys and values and struct fields.
//
// Takes candidate (reflect.Type) which is the type.
// Takes depth (int) which is how deep the walk is, bounded so a recursive type ends it.
//
// Returns bool which is true when the Go type contains a named string type.
func holdsNamedString(candidate reflect.Type, depth int) bool {
	if depth > maxTypeDepth {
		return false
	}
	switch candidate.Kind() {
	case reflect.String:
		return candidate != reflect.TypeFor[string]()
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return holdsNamedString(candidate.Elem(), depth+1)
	case reflect.Map:
		return holdsNamedString(candidate.Key(), depth+1) || holdsNamedString(candidate.Elem(), depth+1)
	case reflect.Struct:
		for field := range candidate.Fields() {
			if holdsNamedString(field.Type, depth+1) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// encode reports the first argument containing an unregistered enum container, or nil.
//
// Takes arguments ([]any) which are the arguments.
//
// Returns error which names the argument, or nil.
func encode(arguments []any) error {
	for index, argument := range arguments {
		if unregisteredType(reflect.TypeOf(argument)) {
			return fmt.Errorf("failed to encode args[%d] of type %T: %w", index, argument, errUnregisteredType)
		}
	}
	return nil
}

// decode reports the first destination containing an unregistered enum container, or nil.
//
// Takes destinations ([]any) which are the destinations.
//
// Returns error which names the destination, or nil.
func decode(destinations []any) error {
	for index, destination := range destinations {
		if unregisteredType(reflect.TypeOf(destination)) {
			return fmt.Errorf("can't scan into dest[%d] of type %T: %w", index, destination, errUnregisteredType)
		}
	}
	return nil
}

// record keeps one statement.
//
// Takes query (string) which is the statement's text.
// Takes arguments ([]any) which are its arguments.
//
// Concurrency: appends to the shared recording under its mutex.
func record(query string, arguments []any) {
	recordedMutex.Lock()
	defer recordedMutex.Unlock()
	recorded = append(recorded, Statement{SQL: query, Arguments: arguments})
}

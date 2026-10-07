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

// Package pgx stands in for github.com/jackc/pgx/v5 in the runtime lane of emittest: it
// declares the types and functions generated pgx code refers to, with the methods it
// calls, so the lane builds without a network. The harness's recorder implements the
// connection.
package pgx

import (
	"context"
	"errors"

	"seagull.sh/seagull/internal/emit/emittest/internal/stubs/pgx/pgconn"
)

var (
	// ErrNoRows is the error a row with no result reports when scanned.
	ErrNoRows = errors.New("no rows in result set")
)

// Identifier is a table's name, its parts in order, such as schema and table.
type Identifier []string

// Rows is the result of a query.
type Rows interface {
	// Close ends the result.
	Close()

	// Err returns the error that ended the result early, or nil.
	//
	// Returns error which is the recorded result error, or nil.
	Err() error

	// Next moves to the next row, reporting false when there is none.
	//
	// Returns bool which is true when a row is available.
	Next() bool

	// Scan reads the current row into destinations.
	//
	// Takes destinations (...any) which receives the row values in column order.
	//
	// Returns error when the source cannot be read into the destination.
	Scan(destinations ...any) error

	// Values returns the current row's values.
	//
	// Returns []any which holds values in entry order.
	// Returns error when the row values cannot be read.
	Values() ([]any, error)
}

// Row is the result of a query read for one row.
type Row interface {
	// Scan reads the row into destinations, or reports ErrNoRows.
	//
	// Takes destinations (...any) which receives the row values in column order.
	//
	// Returns error when the source cannot be read into the destination.
	Scan(destinations ...any) error
}

// QueuedQuery is one statement queued on a batch.
type QueuedQuery struct {
	// SQL is the statement's text.
	SQL string

	// Arguments holds its arguments.
	Arguments []any
}

// Batch holds statements sent together.
type Batch struct {
	// QueuedQueries holds the statements queued, in order.
	QueuedQueries []*QueuedQuery
}

// Queue adds a statement to the batch.
//
// Takes query (string) which is the statement's text.
// Takes arguments (...any) which are its arguments.
//
// Returns *QueuedQuery which is the queued statement.
func (batch *Batch) Queue(query string, arguments ...any) *QueuedQuery {
	queued := &QueuedQuery{SQL: query, Arguments: arguments}
	batch.QueuedQueries = append(batch.QueuedQueries, queued)
	return queued
}

// BatchResults reads the results of a batch, one statement at a time.
type BatchResults interface {
	// Exec reads the next statement's command tag.
	//
	// Returns pgconn.CommandTag which reports the command result.
	// Returns error when execution fails.
	Exec() (pgconn.CommandTag, error)

	// Query reads the next statement's rows.
	//
	// Returns Rows which holds the statement rows.
	// Returns error when the query fails.
	Query() (Rows, error)

	// QueryRow reads the next statement's row.
	//
	// Returns Row which holds the statement row.
	QueryRow() Row

	// Close ends the batch.
	//
	// Returns error when closing the result or batch fails.
	Close() error
}

// CopyFromSource yields the rows CopyFrom sends.
type CopyFromSource interface {
	// Next moves to the next row, reporting false when there is none.
	//
	// Returns bool which is true when a row is available.
	Next() bool

	// Values returns the current row's values.
	//
	// Returns []any which holds values in entry order.
	// Returns error when the row values cannot be read.
	Values() ([]any, error)

	// Err returns the error that ended the rows early, or nil.
	//
	// Returns error which is the recorded result error, or nil.
	Err() error
}

// Tx is a transaction: a connection that commits or rolls back.
type Tx interface {
	// Begin starts a nested transaction.
	//
	// Returns Tx which is the nested transaction.
	// Returns error when it cannot be started.
	Begin(ctx context.Context) (Tx, error)

	// Commit ends the transaction, keeping its changes.
	//
	// Returns error when committing fails.
	Commit(ctx context.Context) error

	// Rollback ends the transaction, dropping its changes.
	//
	// Returns error when rollback fails.
	Rollback(ctx context.Context) error

	// CopyFrom sends rows into a table.
	//
	// Takes tableName (Identifier) which identifies the destination table.
	// Takes columnNames ([]string) which lists the destination columns in row-value order.
	// Takes rowSource (CopyFromSource) which supplies the rows to copy.
	//
	// Returns int64 which is the number of copied rows.
	// Returns error when copying fails.
	CopyFrom(ctx context.Context, tableName Identifier, columnNames []string, rowSource CopyFromSource) (int64, error)

	// SendBatch sends a batch.
	//
	// Takes batch (*Batch) which holds the statements to send.
	//
	// Returns BatchResults which provides results in statement order.
	SendBatch(ctx context.Context, batch *Batch) BatchResults

	// Exec runs a statement that returns no rows.
	//
	// Takes sql (string) which is the statement text.
	// Takes arguments (...any) which supplies bind values in placeholder order.
	//
	// Returns pgconn.CommandTag which reports the command result.
	// Returns error when execution fails.
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)

	// Query runs a statement that returns rows.
	//
	// Takes sql (string) which is the statement text.
	// Takes arguments (...any) which supplies bind values in placeholder order.
	//
	// Returns Rows which holds the statement rows.
	// Returns error when the query fails.
	Query(ctx context.Context, sql string, arguments ...any) (Rows, error)

	// QueryRow runs a statement read for one row.
	//
	// Takes sql (string) which is the statement text.
	// Takes arguments (...any) which supplies bind values in placeholder order.
	//
	// Returns Row which holds the statement row.
	QueryRow(ctx context.Context, sql string, arguments ...any) Row
}

// sliceSource yields the rows of a slice through a function.
type sliceSource struct {
	// next returns the values of a row.
	next func(index int) ([]any, error)

	// err holds the error a row reported.
	err error

	// length is the number of rows.
	length int

	// index is the index of the current row.
	index int
}

// Next moves to the next row.
//
// Returns bool which is false when there is none.
func (source *sliceSource) Next() bool {
	source.index++
	return source.index < source.length
}

// Values returns the current row's values.
//
// Returns []any which holds the values.
// Returns error when the row reports one.
func (source *sliceSource) Values() ([]any, error) {
	values, err := source.next(source.index)
	if err != nil {
		source.err = err
	}
	return values, err
}

// Err returns the error a row reported.
//
// Returns error which is the error, or nil.
func (source *sliceSource) Err() error {
	return source.err
}

// CopyFromSlice yields length rows, each made by next.
//
// Takes length (int) which is the number of rows.
// Takes next (func(int) ([]any, error)) which makes the values of a row.
//
// Returns CopyFromSource which yields the rows.
func CopyFromSlice(length int, next func(int) ([]any, error)) CopyFromSource {
	return &sliceSource{next: next, err: nil, length: length, index: -1}
}

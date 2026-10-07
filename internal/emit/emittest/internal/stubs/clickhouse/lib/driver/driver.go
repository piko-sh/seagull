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

// Package driver stands in for github.com/ClickHouse/clickhouse-go/v2/lib/driver in the
// runtime lane of emittest: it declares the types generated native ClickHouse code refers
// to, with the methods it calls.
package driver

// NamedValue is a value bound to a named query parameter.
type NamedValue struct {
	// Value is the value.
	Value any

	// Name is the parameter's name.
	Name string
}

// PrepareBatchOption configures a batch.
type PrepareBatchOption func(options *PrepareBatchOptions)

// PrepareBatchOptions holds what the options of a batch chose.
type PrepareBatchOptions struct{}

// Rows is the result of a query.
type Rows interface {
	// Close ends the result.
	//
	// Returns error when closing the result or batch fails.
	Close() error

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
}

// Row is the result of a query read for one row.
type Row interface {
	// Err returns the error the query reported, or nil.
	//
	// Returns error which is the recorded result error, or nil.
	Err() error

	// Scan reads the row into destinations.
	//
	// Takes destinations (...any) which receives the row values in column order.
	//
	// Returns error when the source cannot be read into the destination.
	Scan(destinations ...any) error
}

// Batch is a block of rows sent to one INSERT.
type Batch interface {
	// Append adds a row, its values in column order.
	//
	// Takes values (...any) which holds the values to convert, in order.
	//
	// Returns error when the row cannot be appended.
	Append(values ...any) error

	// Close ends the batch.
	//
	// Returns error when closing the result or batch fails.
	Close() error

	// Send sends the rows.
	//
	// Returns error when sending the batch fails.
	Send() error
}

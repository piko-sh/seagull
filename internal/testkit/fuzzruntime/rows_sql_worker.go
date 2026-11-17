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

//go:build ignore

package main

import (
	"database/sql/driver"
	"io"
)

// fuzzRows supplies one bounded database/sql driver result set.
type fuzzRows struct {
	// query selects the row shape.
	query string

	// sent reports whether the single row was read.
	sent bool

	// closed prevents counting a result set twice.
	closed bool
}

// newFuzzRows opens a result set for the query.
//
// Takes query (string) which selects the row shape.
//
// Returns *fuzzRows which must be closed after use.
func newFuzzRows(query string) *fuzzRows {
	fuzzOpenRows++
	return &fuzzRows{query: query}
}

// Columns names each selected result column.
//
// Returns []string which contains one "value" name per column.
func (rows *fuzzRows) Columns() []string {
	values := fuzzRowValues(rows.query)
	columns := make([]string, len(values))
	for index := range columns {
		columns[index] = "value"
	}
	return columns
}

// Close releases the result set and is safe to call more than once.
//
// Returns error which is always nil.
func (rows *fuzzRows) Close() error {
	if !rows.closed {
		rows.closed = true
		fuzzOpenRows--
	}
	return nil
}

// Next copies the single row into destination or reports its end or failure.
//
// Takes destination ([]driver.Value) which receives values in column order.
//
// Returns error which is nil for the row, io.EOF afterward, or the injected failure.
func (rows *fuzzRows) Next(destination []driver.Value) error {
	if fuzzRowMode == 4 {
		return fuzzReadError
	}
	if rows.sent || fuzzRowMode == 0 {
		return io.EOF
	}
	rows.sent = true
	values := fuzzRowValues(rows.query)
	for index, value := range values {
		destination[index] = value
	}
	if fuzzRowMode == 3 {
		destination[0] = "injected invalid integer"
	}
	return nil
}

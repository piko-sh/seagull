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

import "strings"

// fuzzRows supplies one bounded native driver result set.
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

// Close releases the result set and is safe to call more than once.
//
// Returns CLOSE_TYPE which is nil after closure.
func (rows *fuzzRows) Close() CLOSE_TYPE {
	if !rows.closed {
		rows.closed = true
		fuzzOpenRows--
	}
	CLOSE_RETURN
}

// Next advances to the single row unless the selected mode has no row.
//
// Returns bool which reports whether a row is ready.
func (rows *fuzzRows) Next() bool {
	if rows.sent || fuzzRowMode == 0 || fuzzRowMode == 4 {
		return false
	}
	rows.sent = true
	return true
}

// Err reports the injected read failure in the failing row mode.
//
// Returns error which is nil in other modes.
func (rows *fuzzRows) Err() error {
	if fuzzRowMode == 4 {
		return fuzzReadError
	}
	return nil
}

// Values supplies the selected row values.
//
// Returns []any which holds the values in column order.
// Returns error which is always nil.
func (rows *fuzzRows) Values() ([]any, error) { return fuzzRowValues(rows.query), nil }

// Scan copies the selected row into destinations or reports the injected failure.
// It closes count results after scanning, including failed scans.
//
// Takes destinations (...any) which holds scan targets in column order.
//
// Returns error for an empty or failing row, a width mismatch or a scan failure.
func (rows *fuzzRows) Scan(destinations ...any) error {
	if strings.Contains(strings.ToUpper(rows.query), "COUNT(") {
		defer rows.Close()
	}
	if fuzzRowMode == 0 {
		return fuzzNoRowsError
	}
	if fuzzRowMode == 3 || fuzzRowMode == 4 {
		return fuzzReadError
	}
	return fuzzAssignRow(destinations, fuzzRowValues(rows.query))
}

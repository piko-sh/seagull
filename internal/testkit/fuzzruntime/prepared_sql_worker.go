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
	"context"
	"database/sql"
	"errors"
	"sync"

	generated "seagull.sh/compiletarget/generated"
)

// fuzzPreparedMutex guards preparation state and recorded events.
var fuzzPreparedMutex sync.Mutex

// fuzzPreparedEvents records statement lifecycle events in order.
var fuzzPreparedEvents []preparedEvent

// fuzzPreparationAttempt counts preparation calls for the current request.
var fuzzPreparationAttempt int

// fuzzPreparationFailure selects the preparation call that fails, or zero for none.
var fuzzPreparationFailure int

// fuzzNewQueries prepares statements and resets the recorder for one request.
//
// A first data byte of 252 or 253 makes the first or second preparation fail.
// The cleanup function closes the prepared queries twice to check repeated closure.
//
// Takes connection (*sql.DB) which supplies the statements to prepare.
// Takes data ([]byte) which selects an optional preparation failure.
//
// Returns *generated.Queries which uses the prepared statements on success.
// Returns func() error which is nil on preparation failure. Otherwise it closes
// prepared queries twice and joins both close errors.
// Returns error when preparing a statement fails.
//
// Concurrency: fuzzPreparedMutex protects failure selection and recorded events.
// Requests must run sequentially because each request resets the shared state.
func fuzzNewQueries(ctx context.Context, connection *sql.DB, data []byte) (*generated.Queries, func() error, error) {
	fuzzPreparedMutex.Lock()
	fuzzPreparedEvents = nil
	fuzzPreparationAttempt, fuzzPreparationFailure = 0, 0
	if len(data) > 0 && (data[0] == 252 || data[0] == 253) {
		fuzzPreparationFailure = int(data[0]) - 251
	}
	fuzzPreparedMutex.Unlock()
	prepared, err := generated.Prepare(ctx, connection)
	if err != nil {
		return nil, nil, err
	}
	return generated.New(prepared), func() error {
		first := prepared.Close()
		second := prepared.Close()
		return errors.Join(first, second)
	}, nil
}

// fuzzPrepare records one preparation and injects the selected failure.
//
// Takes query (string) which is the SQL being prepared.
//
// Returns error when this preparation is selected to fail.
//
// Concurrency: fuzzPreparedMutex serialises the attempt count and event record.
func fuzzPrepare(query string) error {
	fuzzPreparedMutex.Lock()
	defer fuzzPreparedMutex.Unlock()
	fuzzPreparationAttempt++
	if fuzzPreparationAttempt == fuzzPreparationFailure {
		fuzzPreparedEvents = append(fuzzPreparedEvents, preparedEvent{Operation: "prepare-error", SQL: query})
		return errors.New("injected preparation failure")
	}
	fuzzPreparedEvents = append(fuzzPreparedEvents, preparedEvent{Operation: "prepare", SQL: query})
	return nil
}

// fuzzPreparedEvent records one statement operation.
//
// Takes operation (string) which names the operation.
// Takes query (string) which is the statement's SQL.
//
// Concurrency: fuzzPreparedMutex serialises writes to the event record.
func fuzzPreparedEvent(operation, query string) {
	fuzzPreparedMutex.Lock()
	defer fuzzPreparedMutex.Unlock()
	fuzzPreparedEvents = append(fuzzPreparedEvents, preparedEvent{Operation: operation, SQL: query})
}

// fuzzTakePrepared returns the recorded events and clears the recorder.
//
// Returns []preparedEvent which holds the events in operation order.
//
// Concurrency: fuzzPreparedMutex protects the snapshot and reset.
func fuzzTakePrepared() []preparedEvent {
	fuzzPreparedMutex.Lock()
	defer fuzzPreparedMutex.Unlock()
	result := fuzzPreparedEvents
	fuzzPreparedEvents = nil
	return result
}

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

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"

	"seagull.sh/seagull/examples/sqlite/024_read_replicas/internal/store"
)

// connection is a store.DBTX that prints its name and each statement before running it on
// the embedded database, so the output shows which connection served every query.
type connection struct {
	*sql.DB

	// output receives a line per statement.
	output io.Writer

	// name labels the connection in the output.
	name string
}

// ExecContext prints the statement, then runs it on the database.
//
// Takes query (string) which is the SQL to run.
// Takes arguments (...any) which are its bind values.
//
// Returns sql.Result which is the database's result.
// Returns error when the statement fails.
func (c connection) ExecContext(ctx context.Context, query string, arguments ...any) (sql.Result, error) {
	fmt.Fprintf(c.output, "%s  %s\n", c.name, query)
	return c.DB.ExecContext(ctx, query, arguments...)
}

// QueryContext prints the query, then runs it on the database.
//
// Takes query (string) which is the SQL to run.
// Takes arguments (...any) which are its bind values.
//
// Returns *sql.Rows which holds the result rows.
// Returns error when the query fails.
func (c connection) QueryContext(ctx context.Context, query string, arguments ...any) (*sql.Rows, error) {
	fmt.Fprintf(c.output, "%s  %s\n", c.name, query)
	return c.DB.QueryContext(ctx, query, arguments...)
}

// QueryRowContext prints the query, then runs it on the database.
//
// Takes query (string) which is the SQL to run.
// Takes arguments (...any) which are its bind values.
//
// Returns *sql.Row which holds the result row or the error.
func (c connection) QueryRowContext(ctx context.Context, query string, arguments ...any) *sql.Row {
	fmt.Fprintf(c.output, "%s  %s\n", c.name, query)
	return c.DB.QueryRowContext(ctx, query, arguments...)
}

// run records two sightings and reads them back through Queries built with
// NewWithReplica, printing which connection each statement went to. It then shows that
// the reader refuses writes, which is why the routing matters.
//
// Takes writer (*sql.DB) which is the migrated primary database.
// Takes reader (*sql.DB) which is a read-only connection to the same data.
// Takes output (io.Writer) which receives the routing and the results.
//
// Returns error when a query fails other than as the example expects.
func run(ctx context.Context, writer, reader *sql.DB, output io.Writer) error {
	// Seagull marks each query read-only or not when it analyses it, and each generated
	// method uses the writer or the reader to match: INSERT, UPDATE and DELETE go to the
	// writer, SELECT to the reader.
	queries := store.NewWithReplica(
		connection{DB: writer, output: output, name: "writer"},
		connection{DB: reader, output: output, name: "reader"},
	)

	sightings := []store.RecordSightingParams{
		{Species: "herring gull", Place: "the pier"},
		{Species: "kittiwake", Place: "the harbour wall"},
	}
	for _, sighting := range sightings {
		if _, err := queries.RecordSighting(ctx, sighting); err != nil {
			return fmt.Errorf("recording a %s: %w", sighting.Species, err)
		}
	}

	listed, err := queries.ListSightings(ctx)
	if err != nil {
		return fmt.Errorf("listing sightings: %w", err)
	}
	for _, sighting := range listed {
		fmt.Fprintf(output, "        %d %s at %s\n", sighting.ID, sighting.Species, sighting.Place)
	}

	// CountSightings is a SELECT, but its header says readonly: false, so it reads from the
	// writer. A real replica lags behind the primary; a read that must see a write made a
	// moment ago belongs on the writer.
	count, err := queries.CountSightings(ctx)
	if err != nil {
		return fmt.Errorf("counting sightings: %w", err)
	}
	fmt.Fprintf(output, "        %d sightings on the primary\n", count.Total)

	// The reader was opened with mode=ro, so a write sent to it fails.
	_, err = store.New(reader).RecordSighting(ctx, store.RecordSightingParams{Species: "puffin", Place: "the cliffs"})
	if err == nil {
		return errors.New("the read-only connection accepted a write")
	}
	fmt.Fprintf(output, "a write on the reader fails: %v\n", err)
	return nil
}

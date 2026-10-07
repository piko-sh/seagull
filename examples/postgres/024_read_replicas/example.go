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

	"seagull.sh/seagull/examples/postgres/024_read_replicas/internal/store"
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

// sqlStateError is a server error that carries a SQLSTATE code, as pgx's *pgconn.PgError
// does.
type sqlStateError interface {
	error

	// SQLState returns the five-character SQLSTATE code.
	SQLState() string
}

// run records sightings and reads them back through Queries built with NewWithReplica,
// printing which connection each statement went to. It then sends the queries that only
// look like reads straight to the reader, to show why Seagull routes them to the writer.
//
// Takes writer (*sql.DB) which is the migrated primary database.
// Takes reader (*sql.DB) which is a read-only connection to the same data.
// Takes output (io.Writer) which receives the routing and the results.
//
// Returns error when a query fails other than as the example expects.
func run(ctx context.Context, writer, reader *sql.DB, output io.Writer) error {
	// Seagull decides whether each query is read-only when it analyses it, and each
	// generated method uses the writer or the reader to match. On PostgreSQL a SELECT goes
	// to the writer if it locks rows (FOR UPDATE), modifies data in a CTE, or calls a
	// function whose data access is writing or unknown.
	queries := store.NewWithReplica(
		connection{DB: writer, output: output, name: "writer"},
		connection{DB: reader, output: output, name: "reader"},
	)

	herring, err := queries.RecordSighting(ctx, store.RecordSightingParams{Species: "herring gull", Place: "the pier"})
	if err != nil {
		return fmt.Errorf("recording a herring gull: %w", err)
	}
	if _, err := queries.RecordSighting(ctx, store.RecordSightingParams{Species: "kittiwake", Place: "the harbour wall"}); err != nil {
		return fmt.Errorf("recording a kittiwake: %w", err)
	}

	// LogSighting is a SELECT, but log_sighting inserts a row, so it goes to the writer. A
	// plpgsql function may return NULL, so ID is a pointer.
	logged, err := queries.LogSighting(ctx, store.LogSightingParams{Species: "puffin", Place: "the cliffs"})
	if err != nil {
		return fmt.Errorf("logging a puffin: %w", err)
	}
	if logged.ID == nil {
		return errors.New("log_sighting returned NULL")
	}
	fmt.Fprintf(output, "        logged sighting %d\n", *logged.ID)

	// ListSightings calls sighting_label, whose body only concatenates its arguments, so it
	// stays read-only and goes to the reader. With these non-NULL species and place columns,
	// the concatenation produces a non-NULL Label string.
	listed, err := queries.ListSightings(ctx)
	if err != nil {
		return fmt.Errorf("listing sightings: %w", err)
	}
	for _, sighting := range listed {
		fmt.Fprintf(output, "        %d %s\n", sighting.ID, sighting.Label)
	}

	// CountSightings is a SELECT, but its header says readonly: false, so it reads from the
	// writer. A real replica lags behind the primary; a read that must see a write made a
	// moment ago belongs on the writer.
	count, err := queries.CountSightings(ctx)
	if err != nil {
		return fmt.Errorf("counting sightings: %w", err)
	}
	fmt.Fprintf(output, "        %d sightings on the primary\n", count.Total)

	// FOR UPDATE locks the row, which only the primary can do.
	claimed, err := queries.ClaimSighting(ctx, herring.ID)
	if err != nil {
		return fmt.Errorf("claiming sighting %d: %w", herring.ID, err)
	}
	fmt.Fprintf(output, "        locked sighting %d, a %s\n", claimed.ID, claimed.Species)

	// The DELETE inside the CTE makes the whole statement a write.
	forgotten, err := queries.ForgetPlace(ctx, "the cliffs")
	if err != nil {
		return fmt.Errorf("forgetting the cliffs: %w", err)
	}
	fmt.Fprintf(output, "        forgot %d sighting at the cliffs\n", len(forgotten))

	return tryOnReader(ctx, store.New(reader), herring.ID, output)
}

// tryOnReader sends three queries that write or lock straight to the reader, which
// refuses each one, as a replica would, with SQLSTATE 25006, read_only_sql_transaction.
// pgx reports a server error as a *pgconn.PgError, and its SQLState method gives the
// code, which stays the same where the wording of the message does not.
//
// Takes onReader (*store.Queries) which runs everything on the reader.
// Takes sighting (int64) which is a sighting to try to lock.
// Takes output (io.Writer) which receives each refusal.
//
// Returns error when the reader does not refuse one of them.
func tryOnReader(ctx context.Context, onReader *store.Queries, sighting int64, output io.Writer) error {
	attempts := []struct {
		name string
		call func() error
	}{
		{name: "RecordSighting", call: func() error {
			_, err := onReader.RecordSighting(ctx, store.RecordSightingParams{Species: "gannet", Place: "the bay"})
			return err
		}},
		{name: "LogSighting", call: func() error {
			_, err := onReader.LogSighting(ctx, store.LogSightingParams{Species: "gannet", Place: "the bay"})
			return err
		}},
		{name: "ClaimSighting", call: func() error {
			_, err := onReader.ClaimSighting(ctx, sighting)
			return err
		}},
	}
	fmt.Fprintln(output, "on the reader:")
	for _, attempt := range attempts {
		err := attempt.call()
		if err == nil {
			return fmt.Errorf("%s on the reader: want a refusal from the server, got no error", attempt.name)
		}
		refusal, hasState := errors.AsType[sqlStateError](err)
		if !hasState {
			return fmt.Errorf("%s on the reader: want a refusal from the server, got %w", attempt.name, err)
		}
		fmt.Fprintf(output, "  %s refused: SQLSTATE %s\n", attempt.name, refusal.SQLState())
	}
	return nil
}

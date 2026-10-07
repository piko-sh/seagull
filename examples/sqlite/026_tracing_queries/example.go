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
	"fmt"
	"io"

	"seagull.sh/seagull/examples/sqlite/026_tracing_queries/internal/store"
)

// tracer is a store.DBTX that logs the name of every query before running it on the
// connection it wraps. A real tracer would start a span or record a metric under the same
// name.
type tracer struct {
	// inner is the connection the queries run on: a *sql.DB, or a *sql.Tx once WrapDBTX has
	// wrapped one.
	inner store.DBTX

	// output receives a line per query.
	output io.Writer

	// scope labels each line with where the query ran: db or tx.
	scope string
}

// ExecContext traces the query, then runs it on the wrapped connection.
//
// Takes query (string) which is the SQL to run.
// Takes arguments (...any) which are its bind values.
//
// Returns sql.Result which is the database's result.
// Returns error when the statement fails.
func (t tracer) ExecContext(ctx context.Context, query string, arguments ...any) (sql.Result, error) {
	t.trace(query)
	return t.inner.ExecContext(ctx, query, arguments...)
}

// QueryContext traces the query, then runs it on the wrapped connection.
//
// Takes query (string) which is the SQL to run.
// Takes arguments (...any) which are its bind values.
//
// Returns *sql.Rows which holds the result rows.
// Returns error when the query fails.
func (t tracer) QueryContext(ctx context.Context, query string, arguments ...any) (*sql.Rows, error) {
	t.trace(query)
	return t.inner.QueryContext(ctx, query, arguments...)
}

// QueryRowContext traces the query, then runs it on the wrapped connection.
//
// Takes query (string) which is the SQL to run.
// Takes arguments (...any) which are its bind values.
//
// Returns *sql.Row which holds the result row or the error.
func (t tracer) QueryRowContext(ctx context.Context, query string, arguments ...any) *sql.Row {
	t.trace(query)
	return t.inner.QueryRowContext(ctx, query, arguments...)
}

// WrapDBTX returns a tracer around another connection. The generated WithTx, which
// RunInTx uses, calls it with the *sql.Tx, so queries in a transaction are traced too.
// Without it they would run on the bare *sql.Tx and go untraced.
//
// Takes inner (any) which is the connection to wrap.
//
// Returns any which is the wrapped connection, or inner unchanged when it is not a DBTX.
func (t tracer) WrapDBTX(inner any) any {
	connection, ok := inner.(store.DBTX)
	if !ok {
		return inner
	}
	return tracer{inner: connection, output: t.output, scope: "tx"}
}

// trace writes the query's name. go.query_names generates store.QueryName, which maps the
// SQL text every generated method passes to its DBTX back to the query's name.
//
// Takes query (string) which is the SQL about to run.
func (t tracer) trace(query string) {
	name := store.QueryName(query)
	if name == "" {
		name = "(not a generated query)"
	}
	fmt.Fprintf(t.output, "%s  %s\n", t.scope, name)
}

// run adds two shifts to a rota, then covers one and adds another in a transaction,
// tracing every query by name, and prints the rota.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives the trace and the rota.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(tracer{inner: database, output: output, scope: "db"})

	monday, err := queries.AddShift(ctx, store.AddShiftParams{Keeper: "Bryn", Day: "Monday"})
	if err != nil {
		return fmt.Errorf("adding Monday's shift: %w", err)
	}
	if _, err := queries.AddShift(ctx, store.AddShiftParams{Keeper: "Morwenna", Day: "Tuesday"}); err != nil {
		return fmt.Errorf("adding Tuesday's shift: %w", err)
	}

	err = queries.RunInTx(ctx, database, func(inTransaction *store.Queries) error {
		if _, err := inTransaction.CoverShift(ctx, monday.ID); err != nil {
			return err
		}
		_, err := inTransaction.AddShift(ctx, store.AddShiftParams{Keeper: "Bryn", Day: "Wednesday"})
		return err
	})
	if err != nil {
		return fmt.Errorf("swapping shifts: %w", err)
	}

	shifts, err := queries.ListShifts(ctx)
	if err != nil {
		return fmt.Errorf("listing shifts: %w", err)
	}
	for _, shift := range shifts {
		fmt.Fprintf(output, "%d %-9s %-8s covered=%d\n", shift.ID, shift.Day, shift.Keeper, shift.Covered)
	}
	return nil
}

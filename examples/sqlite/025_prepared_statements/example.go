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

	"seagull.sh/seagull/examples/sqlite/025_prepared_statements/internal/store"
)

const (
	// days is how many days of tide times the example loads for each port.
	days = 3

	// minutesPerDay wraps tide times past midnight.
	minutesPerDay = 24 * 60

	// tideDrift is roughly how much later high tide comes each day, in minutes.
	tideDrift = 50
)

// ports lists the harbours and the minute of their first high tide.
var ports = []struct {
	name  string
	first int
}{
	{name: "Padstow", first: 5*60 + 12},
	{name: "Tenby", first: 6*60 + 40},
	{name: "Whitby", first: 11*60 + 3},
}

// run prepares every query once, loads a few days of tide times in a transaction, then
// looks up each port's high tide for each day through the prepared statements.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives the tide table.
//
// Returns error when preparing or a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	// With go.prepared: true, sgl generates Prepare. It prepares every static query in the
	// package up front, so a query that no longer matches the schema fails here, at
	// start-up, rather than on first use. Close releases the statements.
	prepared, err := store.Prepare(ctx, database)
	if err != nil {
		return fmt.Errorf("preparing queries: %w", err)
	}
	defer func() { _ = prepared.Close() }()

	// *store.PreparedDBTX is a DBTX: every method of these Queries runs its prepared
	// statement instead of sending the SQL text again.
	queries := store.New(prepared)

	// RunInTx needs the *sql.DB itself, which UnderlyingDB returns. PreparedDBTX also has a
	// WrapDBTX method, so the Queries that RunInTx binds to the transaction keep using the
	// prepared statements, each bound to the *sql.Tx with StmtContext.
	err = queries.RunInTx(ctx, prepared.UnderlyingDB(), func(inTransaction *store.Queries) error {
		return loadTideTimes(ctx, inTransaction)
	})
	if err != nil {
		return fmt.Errorf("loading tide times: %w", err)
	}

	fmt.Fprint(output, "         ")
	for day := 1; day <= days; day++ {
		fmt.Fprintf(output, "  day %d", day)
	}
	fmt.Fprintln(output)

	// The same HighTide statement runs once per port and day.
	for _, port := range ports {
		fmt.Fprintf(output, "%-9s", port.name)
		for day := int64(1); day <= days; day++ {
			tide, err := queries.HighTide(ctx, store.HighTideParams{Port: port.name, Day: day})
			if err != nil {
				return fmt.Errorf("looking up %s on day %d: %w", port.name, day, err)
			}
			fmt.Fprintf(output, "  %s", tide.HighAt)
		}
		fmt.Fprintln(output)
	}
	return nil
}

// loadTideTimes adds a high tide for every port on every day, each a little later than
// the day before.
//
// Takes queries (*store.Queries) which add the rows.
//
// Returns error when a row cannot be added.
func loadTideTimes(ctx context.Context, queries *store.Queries) error {
	for _, port := range ports {
		for day := range days {
			minute := (port.first + day*tideDrift) % minutesPerDay
			err := queries.AddTideTime(ctx, store.AddTideTimeParams{
				Port:   port.name,
				Day:    int64(day + 1),
				HighAt: fmt.Sprintf("%02d:%02d", minute/60, minute%60),
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

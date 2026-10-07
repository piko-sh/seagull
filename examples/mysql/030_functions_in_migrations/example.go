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

	"seagull.sh/seagull/examples/mysql/030_functions_in_migrations/internal/store"
)

// connection is a store.DBTX that notes its name before each statement, so the example
// can say whether Seagull sent a query to the writer or the reader. Both connections wrap
// the same database here; concept 24 puts the reader on a replica.
type connection struct {
	*sql.DB

	// last receives the name of the connection that ran the latest statement.
	last *string

	// name labels the connection.
	name string
}

// ExecContext notes the connection, then runs the statement on the database.
//
// Takes query (string) which is the SQL to run.
// Takes arguments (...any) which are its bind values.
//
// Returns sql.Result which is the database's result.
// Returns error when the statement fails.
func (c connection) ExecContext(ctx context.Context, query string, arguments ...any) (sql.Result, error) {
	*c.last = c.name
	return c.DB.ExecContext(ctx, query, arguments...)
}

// QueryContext notes the connection, then runs the query on the database.
//
// Takes query (string) which is the SQL to run.
// Takes arguments (...any) which are its bind values.
//
// Returns *sql.Rows which holds the result rows.
// Returns error when the query fails.
func (c connection) QueryContext(ctx context.Context, query string, arguments ...any) (*sql.Rows, error) {
	*c.last = c.name
	return c.DB.QueryContext(ctx, query, arguments...)
}

// QueryRowContext notes the connection, then runs the query on the database.
//
// Takes query (string) which is the SQL to run.
// Takes arguments (...any) which are its bind values.
//
// Returns *sql.Row which holds the result row or the error.
func (c connection) QueryRowContext(ctx context.Context, query string, arguments ...any) *sql.Row {
	*c.last = c.name
	return c.DB.QueryRowContext(ctx, query, arguments...)
}

// run prices the fish, lands three catches and reads the market back, all through the SQL
// functions that migration 0002 creates. Seagull reads each CREATE FUNCTION when it
// builds the schema: the argument and return types type the queries that call it, and
// what it says about data access, along with what its body does, decides whether those
// queries are read-only, which picks the writer or the reader.
//
//   - land_catch says MODIFIES SQL DATA and its body inserts a row, so LandCatch goes to
//     the writer. Its body is a BEGIN ... END block, which a migration can hold without
//     DELIMITER, as Seagull splits the file into statements itself;
//   - price_pence says NO SQL, so BoatHaul, which calls it, goes to the reader;
//   - species_price says MODIFIES SQL DATA, which Seagull takes to mean it writes, but
//     its body only reads. MySQL never checks these characteristics against the body. The
//     sgl.migration(readonly: true) line above it in the migration says so, and
//     SpeciesPrice goes to the reader.
//
// With binary logging on, as it is by default, MySQL refuses a function that is not
// DETERMINISTIC, NO SQL or READS SQL DATA, so the two MODIFIES SQL DATA functions are
// declared DETERMINISTIC too.
//
// A function's result can be NULL, so the columns are pointers: species_price finds no
// row for a fish with no price.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives each call's connection and result.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	var last string
	queries := store.NewWithReplica(
		connection{DB: database, last: &last, name: "writer"},
		connection{DB: database, last: &last, name: "reader"},
	)

	prices := []store.SetPriceParams{
		{Species: "crab", PencePerKilo: 1200},
		{Species: "mackerel", PencePerKilo: 450},
		{Species: "pollock", PencePerKilo: 800},
	}
	for _, price := range prices {
		if err := queries.SetPrice(ctx, price); err != nil {
			return fmt.Errorf("pricing %s: %w", price.Species, err)
		}
	}

	catches := []store.LandCatchParams{
		{Boat: "Kittiwake", Species: "mackerel", Grams: 12000},
		{Boat: "Kittiwake", Species: "crab", Grams: 3500},
		{Boat: "Puffin", Species: "pollock", Grams: 8000},
	}
	for _, catch := range catches {
		landed, err := queries.LandCatch(ctx, catch)
		if err != nil {
			return fmt.Errorf("landing %s: %w", catch.Species, err)
		}
		fmt.Fprintf(output, "LandCatch    on the %s: catch %s, %dg of %s from the %s\n", last, orNull(landed.ID), catch.Grams, catch.Species, catch.Boat)
	}

	for _, species := range []string{"crab", "halibut"} {
		price, err := queries.SpeciesPrice(ctx, species)
		if err != nil {
			return fmt.Errorf("pricing %s: %w", species, err)
		}
		if price.PencePerKilo == nil {
			fmt.Fprintf(output, "SpeciesPrice on the %s: %s has no price\n", last, species)
			continue
		}
		fmt.Fprintf(output, "SpeciesPrice on the %s: %s fetches %dp a kilo\n", last, species, *price.PencePerKilo)
	}

	haul, err := queries.BoatHaul(ctx, "Kittiwake")
	if err != nil {
		return fmt.Errorf("reading the Kittiwake's haul: %w", err)
	}
	fmt.Fprintf(output, "BoatHaul     on the %s: the Kittiwake's haul\n", last)
	for _, row := range haul {
		fmt.Fprintf(output, "  %-9s %6dg %5sp\n", row.Species, row.WeightGrams, orNull(row.ValuePence))
	}
	return nil
}

// orNull formats a nullable field, or returns NULL when the field is nil.
//
// Takes value (*T) which is a nullable field.
//
// Returns string which is the text to print.
func orNull[T any](value *T) string {
	if value == nil {
		return "NULL"
	}
	return fmt.Sprint(*value)
}

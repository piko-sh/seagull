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
	"time"

	"seagull.sh/seagull/examples/duckdb/038_duckdb_analytics/internal/store"
)

// run lands a week of fish at two harbours and analyses it with DuckDB's own SQL, every
// result typed by Seagull:
//
//   - BiggestLandingPerHarbour filters on a window function with QUALIFY, which keeps the
//     table's types, so no subquery is needed to pick the top row per harbour;
//   - CatchBySpecies builds one column per species with sum(...) FILTER (WHERE ...); SUM
//     of an INTEGER is a HUGEINT in DuckDB, so each arrives as *big.Int;
//   - CatchPivot builds the same columns with PIVOT ... ON species IN (...), one column
//     per listed species, each a nullable HUGEINT, as a harbour may land none of one;
//   - TonnesPerBoat calls tonnes, a macro from the migration, and its CAST gives the
//     result a type, *float64;
//   - HarbourCatch reads from harbour_landings, a table macro, whose columns Seagull
//     types from the macro's body, so species is a string, NOT NULL like the landings
//     column it comes from, and only the total is cast, to a BIGINT, for an *int64;
//   - AuctionPrices and LandingValues read the auction house's Parquet file with
//     read_parquet, and the path is a bound parameter. Seagull cannot look inside the
//     file when it generates, so sgl vet warns Q001 for a column it cannot find, and a
//     CAST gives each column its type; such columns are pointers, as nothing says they
//     are NOT NULL.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes priceList (string) which is the path of the auction house's Parquet price list.
// Takes output (io.Writer) which receives each analysis.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, priceList string, output io.Writer) error {
	queries := store.New(database)

	landings := []store.LandFishParams{
		{Harbour: "Newlyn", Boat: "Kittiwake", Species: "cod", Kg: 120, LandedOn: day(1)},
		{Harbour: "Newlyn", Boat: "Kittiwake", Species: "mackerel", Kg: 300, LandedOn: day(2)},
		{Harbour: "Newlyn", Boat: "Salty Chip", Species: "hake", Kg: 80, LandedOn: day(1)},
		{Harbour: "Newlyn", Boat: "Salty Chip", Species: "cod", Kg: 60, LandedOn: day(3)},
		{Harbour: "Brixham", Boat: "Gull Wing", Species: "cod", Kg: 150, LandedOn: day(2)},
		{Harbour: "Brixham", Boat: "Gull Wing", Species: "hake", Kg: 40, LandedOn: day(3)},
		{Harbour: "Brixham", Boat: "Driftwood", Species: "mackerel", Kg: 220, LandedOn: day(1)},
	}
	for _, landing := range landings {
		if err := queries.LandFish(ctx, landing); err != nil {
			return fmt.Errorf("landing %s's %s: %w", landing.Boat, landing.Species, err)
		}
	}

	biggest, err := queries.BiggestLandingPerHarbour(ctx)
	if err != nil {
		return fmt.Errorf("finding the biggest landings: %w", err)
	}
	fmt.Fprintln(output, "biggest landing per harbour (QUALIFY):")
	for _, landing := range biggest {
		fmt.Fprintf(output, "  %-8s %-10s %4dkg of %s on %s\n", landing.Harbour, landing.Boat, landing.Kg, landing.Species, landing.LandedOn.Format(time.DateOnly))
	}

	bySpecies, err := queries.CatchBySpecies(ctx)
	if err != nil {
		return fmt.Errorf("totalling the catch by species: %w", err)
	}
	fmt.Fprintln(output, "kg by species (FILTER):")
	for _, harbour := range bySpecies {
		fmt.Fprintf(output, "  %-8s cod %v, hake %v, mackerel %v\n", harbour.Harbour, harbour.CodKg, harbour.HakeKg, harbour.MackerelKg)
	}

	pivoted, err := queries.CatchPivot(ctx)
	if err != nil {
		return fmt.Errorf("pivoting the catch by species: %w", err)
	}
	fmt.Fprintln(output, "kg by species (PIVOT):")
	for _, harbour := range pivoted {
		fmt.Fprintf(output, "  %-8s cod %v, hake %v, mackerel %v\n", harbour.Harbour, harbour.Cod, harbour.Hake, harbour.Mackerel)
	}

	perBoat, err := queries.TonnesPerBoat(ctx)
	if err != nil {
		return fmt.Errorf("totalling each boat: %w", err)
	}
	fmt.Fprintln(output, "tonnes per boat (macro):")
	for _, boat := range perBoat {
		fmt.Fprintf(output, "  %-10s %.3f t\n", boat.Boat, *boat.Tonnes)
	}

	newlyn, err := queries.HarbourCatch(ctx, "Newlyn")
	if err != nil {
		return fmt.Errorf("reading Newlyn's catch: %w", err)
	}
	fmt.Fprintln(output, "Newlyn's catch (table macro):")
	for _, catch := range newlyn {
		fmt.Fprintf(output, "  %-8s %dkg\n", catch.Species, *catch.Kg)
	}

	prices, err := queries.AuctionPrices(ctx, priceList)
	if err != nil {
		return fmt.Errorf("reading the price list: %w", err)
	}
	fmt.Fprintln(output, "auction prices (read_parquet):")
	for _, price := range prices {
		fmt.Fprintf(output, "  %-8s %dp a kg\n", *price.Species, *price.PencePerKg)
	}

	values, err := queries.LandingValues(ctx, priceList)
	if err != nil {
		return fmt.Errorf("valuing the landings: %w", err)
	}
	fmt.Fprintln(output, "value per boat (landings joined to the Parquet file):")
	for _, boat := range values {
		fmt.Fprintf(output, "  %-10s £%d.%02d\n", boat.Boat, *boat.ValuePence/100, *boat.ValuePence%100)
	}
	return nil
}

// day returns a date in May 2026, the week the landings were made.
//
// Takes dayOfMonth (int) which is the day.
//
// Returns time.Time which is midnight UTC on that day.
func day(dayOfMonth int) time.Time {
	return time.Date(2026, time.May, dayOfMonth, 0, 0, 0, 0, time.UTC)
}

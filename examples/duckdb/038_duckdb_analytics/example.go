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

// run analyzes fish landings with DuckDB queries.
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
		fmt.Fprintf(output, "  %-10s %.3f t\n", boat.Boat, boat.Tonnes.V)
	}

	newlyn, err := queries.HarbourCatch(ctx, "Newlyn")
	if err != nil {
		return fmt.Errorf("reading Newlyn's catch: %w", err)
	}
	fmt.Fprintln(output, "Newlyn's catch (table macro):")
	for _, catch := range newlyn {
		fmt.Fprintf(output, "  %-8s %dkg\n", catch.Species, catch.Kg.V)
	}

	prices, err := queries.AuctionPrices(ctx, priceList)
	if err != nil {
		return fmt.Errorf("reading the price list: %w", err)
	}
	fmt.Fprintln(output, "auction prices (read_parquet):")
	for _, price := range prices {
		fmt.Fprintf(output, "  %-8s %dp a kg\n", price.Species.V, price.PencePerKg.V)
	}

	values, err := queries.LandingValues(ctx, priceList)
	if err != nil {
		return fmt.Errorf("valuing the landings: %w", err)
	}
	fmt.Fprintln(output, "value per boat (landings joined to the Parquet file):")
	for _, boat := range values {
		fmt.Fprintf(output, "  %-10s £%d.%02d\n", boat.Boat, boat.ValuePence.V/100, boat.ValuePence.V%100)
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

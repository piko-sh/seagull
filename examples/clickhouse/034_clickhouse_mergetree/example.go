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

	"seagull.sh/seagull/examples/clickhouse/034_clickhouse_mergetree/internal/store"
)

// run keeps a fishing harbour's records in four tables, one for each way a MergeTree
// engine treats rows that share a sorting key. Every INSERT writes a new part, and
// ClickHouse merges parts in the background at a time of its choosing:
//
//   - berths is a ReplacingMergeTree(version): a merge keeps only the row with the
//     highest version for each boat, so moving a boat is an INSERT, not an UPDATE. FINAL
//     applies that merge as the query reads, so CurrentBerths sees one row a boat
//     straight away;
//   - daily_catch is a SummingMergeTree(kilos): a merge adds up kilos across rows with
//     the same day and species, and FINAL again does it at read time;
//   - species_stats is an AggregatingMergeTree whose columns hold partial aggregates.
//     RollUpDay stores sumState and maxState for one day's landings; SpeciesStats
//     combines every day's states with sumMerge and maxMerge;
//   - landings is a plain MergeTree, and BigLandings filters it with PREWHERE, which
//     reads the species column first and only then the rest of the rows that match.
//
// Seagull reads each engine, FINAL and PREWHERE from the SQL and types the results as
// usual. A -Merge combinator returns what its function would: the kilos column holds
// AggregateFunction(sum, UInt64) states, so sumMerge gives a UInt64, and heaviest holds
// AggregateFunction(max, UInt32) states, so maxMerge gives a UInt32. Neither is NULL, so
// Kilos is a uint64 and Heaviest a uint32.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives each table's view.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	moves := []store.MoveBoatParams{
		{Boat: "Kittiwake", Berth: "north quay", Version: 1},
		{Boat: "Salty Chip", Berth: "slipway", Version: 1},
		{Boat: "Kittiwake", Berth: "fish market", Version: 2},
		{Boat: "Gull Wing", Berth: "north quay", Version: 1},
		{Boat: "Kittiwake", Berth: "outer harbour", Version: 3},
	}
	for _, move := range moves {
		if err := queries.MoveBoat(ctx, move); err != nil {
			return fmt.Errorf("moving %s: %w", move.Boat, err)
		}
	}
	berths, err := queries.CurrentBerths(ctx)
	if err != nil {
		return fmt.Errorf("reading berths: %w", err)
	}
	fmt.Fprintln(output, "berths, latest version of each boat:")
	for _, berth := range berths {
		fmt.Fprintf(output, "  %-10s %-13s v%d\n", berth.Boat, berth.Berth, berth.Version)
	}

	landings := []store.LandCatchParams{
		{ID: 1, Day: day(1), Boat: "Kittiwake", Species: "mackerel", Kilos: 120},
		{ID: 2, Day: day(1), Boat: "Salty Chip", Species: "crab", Kilos: 30},
		{ID: 3, Day: day(1), Boat: "Gull Wing", Species: "mackerel", Kilos: 80},
		{ID: 4, Day: day(2), Boat: "Kittiwake", Species: "mackerel", Kilos: 95},
		{ID: 5, Day: day(2), Boat: "Kittiwake", Species: "pollock", Kilos: 45},
		{ID: 6, Day: day(2), Boat: "Salty Chip", Species: "crab", Kilos: 25},
	}
	for _, landing := range landings {
		if err := queries.LandCatch(ctx, landing); err != nil {
			return fmt.Errorf("landing catch %d: %w", landing.ID, err)
		}
		tally := store.TallyCatchParams{Day: landing.Day, Species: landing.Species, Kilos: uint64(landing.Kilos)}
		if err := queries.TallyCatch(ctx, tally); err != nil {
			return fmt.Errorf("tallying catch %d: %w", landing.ID, err)
		}
	}
	daily, err := queries.DailyCatch(ctx)
	if err != nil {
		return fmt.Errorf("reading the daily catch: %w", err)
	}
	fmt.Fprintln(output, "daily catch, summed by day and species:")
	for _, row := range daily {
		fmt.Fprintf(output, "  %s %-9s %3d kg\n", row.Day.Format(time.DateOnly), row.Species, row.Kilos)
	}

	for _, date := range []int{1, 2} {
		if err := queries.RollUpDay(ctx, day(date)); err != nil {
			return fmt.Errorf("rolling up day %d: %w", date, err)
		}
	}
	stats, err := queries.SpeciesStats(ctx)
	if err != nil {
		return fmt.Errorf("reading species stats: %w", err)
	}
	fmt.Fprintln(output, "species stats, merged from both days' states:")
	for _, row := range stats {
		fmt.Fprintf(output, "  %-9s %3d kg in all, heaviest landing %d kg\n", row.Species, row.Kilos, row.Heaviest)
	}

	big, err := queries.BigLandings(ctx, store.BigLandingsParams{Species: "mackerel", MinKilos: 90})
	if err != nil {
		return fmt.Errorf("finding big landings: %w", err)
	}
	fmt.Fprintln(output, "mackerel landings of 90 kg or more:")
	for _, landing := range big {
		fmt.Fprintf(output, "  #%d %-10s %d kg\n", landing.ID, landing.Boat, landing.Kilos)
	}
	return nil
}

// day returns a date in August 2026, at midnight UTC. A Date parameter binds the calendar
// date a time has in its own location.
//
// Takes date (int) which is the day of the month.
//
// Returns time.Time which is the date.
func day(date int) time.Time {
	return time.Date(2026, time.August, date, 0, 0, 0, 0, time.UTC)
}

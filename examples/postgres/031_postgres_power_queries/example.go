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
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"io"
	"slices"
	"time"

	"seagull.sh/seagull/examples/postgres/031_postgres_power_queries/internal/store"
)

// run records a week of landings at a small harbour and reads them back with five queries
// that lean on PostgreSQL's own SQL. Seagull types each of them:
//
//   - LatestLandings uses DISTINCT ON to keep the first row of each boat in ORDER BY
//     order, which is its latest landing;
//   - BiggestLandings runs a LIMIT 2 subquery once per boat with LEFT JOIN LATERAL, so
//     the subquery can see boats.id; a boat with no landings still appears, which makes
//     the subquery's columns pointers;
//   - SearchNotes matches a generated tsvector column against websearch_to_tsquery, which
//     takes the syntax people type into search boxes, and ranks with ts_rank;
//   - BoatGear expands each boat's JSONB gear into key and value rows with jsonb_each, a
//     function used as a table; value stays store.JSON;
//   - ArchiveLandings moves old landings in one statement: the DELETE inside the CTE
//     hands its RETURNING rows to the INSERT, and the INSERT's RETURNING comes back.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives each query's result.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	if err := recordWeek(ctx, queries); err != nil {
		return err
	}

	latest, err := queries.LatestLandings(ctx)
	if err != nil {
		return fmt.Errorf("finding the latest landings: %w", err)
	}
	fmt.Fprintln(output, "latest landing per boat (DISTINCT ON):")
	for _, landing := range latest {
		fmt.Fprintf(output, "  %-9s %-8s %s\n", landing.Name, landing.Species, landing.LandedOn.Format(time.DateOnly))
	}

	biggest, err := queries.BiggestLandings(ctx)
	if err != nil {
		return fmt.Errorf("finding the biggest landings: %w", err)
	}
	fmt.Fprintln(output, "two biggest landings per boat (LEFT JOIN LATERAL):")
	for _, landing := range biggest {
		if landing.Species == nil || landing.Kilos == nil {
			fmt.Fprintf(output, "  %-9s nothing landed\n", landing.Name)
			continue
		}
		fmt.Fprintf(output, "  %-9s %-8s %3dkg\n", landing.Name, *landing.Species, *landing.Kilos)
	}

	for _, terms := range []string{"mackerel", "crab or pollock"} {
		found, err := queries.SearchNotes(ctx, terms)
		if err != nil {
			return fmt.Errorf("searching for %q: %w", terms, err)
		}
		fmt.Fprintf(output, "search %q (full-text):\n", terms)
		for _, match := range found {
			fmt.Fprintf(output, "  #%d rank %s  %s\n", match.ID, formatRank(match.Rank), match.Notes)
		}
	}

	gear, err := queries.BoatGear(ctx)
	if err != nil {
		return fmt.Errorf("listing gear: %w", err)
	}
	fmt.Fprintln(output, "gear (jsonb_each):")
	for _, item := range gear {
		fmt.Fprintf(output, "  %-9s %-5s %s\n", item.Name, item.Key, item.Value)
	}

	cutoff := july(3)
	archived, err := queries.ArchiveLandings(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("archiving landings: %w", err)
	}
	// RETURNING rows come back in no promised order, so sort them.
	slices.SortFunc(archived, func(a, b store.ArchiveLandingsRow) int { return cmp.Compare(a.ID, b.ID) })
	fmt.Fprintf(output, "archived before %s (DELETE and INSERT in one statement):\n", cutoff.Format(time.DateOnly))
	for _, landing := range archived {
		fmt.Fprintf(output, "  #%d %-8s %s\n", landing.ID, landing.Species, landing.LandedOn.Format(time.DateOnly))
	}
	return nil
}

// recordWeek adds three boats and five landings.
//
// Takes queries (*store.Queries) which add the rows.
//
// Returns error when a row cannot be added.
func recordWeek(ctx context.Context, queries *store.Queries) error {
	boats := []store.AddBoatParams{
		{Name: "Kittiwake", Gear: store.JSON(`{"nets": 2, "pots": 40}`)},
		{Name: "Puffin", Gear: store.JSON(`{"lines": 12}`)},
		{Name: "Gannet", Gear: store.JSON(`{}`)},
	}
	ids := make(map[string]int64, len(boats))
	for _, boat := range boats {
		added, err := queries.AddBoat(ctx, boat)
		if err != nil {
			return fmt.Errorf("adding %s: %w", boat.Name, err)
		}
		ids[boat.Name] = added.ID
	}
	landings := []store.RecordLandingParams{
		{BoatID: ids["Kittiwake"], Species: "mackerel", Kilos: 120, LandedOn: july(1), Notes: "a big shoal of mackerel off the point"},
		{BoatID: ids["Kittiwake"], Species: "crab", Kilos: 35, LandedOn: july(2), Notes: "pots hauled in a heavy swell"},
		{BoatID: ids["Puffin"], Species: "pollock", Kilos: 60, LandedOn: july(1), Notes: "lines over the wrecks"},
		{BoatID: ids["Kittiwake"], Species: "mackerel", Kilos: 80, LandedOn: july(4), Notes: "feathers out, gulls everywhere"},
		{BoatID: ids["Puffin"], Species: "mackerel", Kilos: 25, LandedOn: july(3), Notes: "a few mackerel by the harbour wall"},
	}
	for _, landing := range landings {
		if err := queries.RecordLanding(ctx, landing); err != nil {
			return fmt.Errorf("recording %s: %w", landing.Species, err)
		}
	}
	return nil
}

// july returns a day in July 2026, the week the landings happen in.
//
// Takes day (int) which is the day of the month.
//
// Returns time.Time which is midnight UTC on that day, as a DATE column holds it.
func july(day int) time.Time {
	return time.Date(2026, time.July, day, 0, 0, 0, 0, time.UTC)
}

// formatRank formats a ts_rank score to four places, or returns NULL when it is nil.
//
// Takes rank (*float32) which is the score.
//
// Returns string which is the text to print.
func formatRank(rank *float32) string {
	if rank == nil {
		return "NULL"
	}
	return fmt.Sprintf("%.4f", *rank)
}

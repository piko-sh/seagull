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

	"seagull.sh/seagull/examples/sqlite/003_parameters/internal/store"
)

// run fills a ferry timetable, searches it two ways and books seats. Every query has two
// or more parameters, so each method takes a Params struct, and the struct's field names
// come from the SQL:
//
//   - AddSailing and FindSailings name each ? after the column it is inserted into or
//     compared with: Origin, Destination, Departs, SeatsFree;
//   - SailingsDeparting compares departs twice, which would give Departs and Departs2, so
//     its binding lines (?1 as sgl.param(earliest)) choose better names;
//   - BookSeats uses named placeholders; :seats appears twice but is one field.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives the search results and bookings.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	timetable := []store.AddSailingParams{
		{Origin: "Penzance", Destination: "St Mary's", Departs: "09:15", SeatsFree: 40},
		{Origin: "St Mary's", Destination: "Tresco", Departs: "11:00", SeatsFree: 12},
		{Origin: "Penzance", Destination: "St Mary's", Departs: "14:30", SeatsFree: 4},
		{Origin: "St Mary's", Destination: "Penzance", Departs: "16:30", SeatsFree: 60},
	}
	for _, sailing := range timetable {
		if err := queries.AddSailing(ctx, sailing); err != nil {
			return fmt.Errorf("adding the %s sailing: %w", sailing.Departs, err)
		}
	}

	route, err := queries.FindSailings(ctx, store.FindSailingsParams{
		Origin:      "Penzance",
		Destination: "St Mary's",
	})
	if err != nil {
		return fmt.Errorf("finding sailings: %w", err)
	}
	fmt.Fprintln(output, "Penzance to St Mary's:")
	for _, sailing := range route {
		fmt.Fprintf(output, "  sailing %d at %s, %d seats free\n", sailing.ID, sailing.Departs, sailing.SeatsFree)
	}

	midday, err := queries.SailingsDeparting(ctx, store.SailingsDepartingParams{
		Earliest: "10:00",
		Latest:   "15:00",
	})
	if err != nil {
		return fmt.Errorf("finding midday sailings: %w", err)
	}
	fmt.Fprintln(output, "Departing between 10:00 and 15:00:")
	for _, sailing := range midday {
		fmt.Fprintf(output, "  sailing %d at %s, %s to %s\n", sailing.ID, sailing.Departs, sailing.Origin, sailing.Destination)
	}

	// The same booking twice: the first takes 3 of the 4 free seats, so the second changes
	// no row because seats_free >= :seats is now false.
	booking := store.BookSeatsParams{Seats: 3, SailingID: 3}
	for range 2 {
		booked, err := queries.BookSeats(ctx, booking)
		if err != nil {
			return fmt.Errorf("booking sailing %d: %w", booking.SailingID, err)
		}
		if booked == 0 {
			fmt.Fprintf(output, "sailing %d has fewer than %d seats free\n", booking.SailingID, booking.Seats)
			continue
		}
		fmt.Fprintf(output, "booked %d seats on sailing %d\n", booking.Seats, booking.SailingID)
	}
	return nil
}

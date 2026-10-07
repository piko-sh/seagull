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
	"fmt"
	"io"

	"seagull.sh/seagull/examples/clickhouse/003_parameters_native/internal/store"
)

// run fills a ferry timetable, searches it two ways and books seats, over a native
// clickhouse-go connection. A ClickHouse placeholder carries its own name and type, as in
// {origin:String}, so each Params field is named after the placeholder and typed from it:
// String gives string, Int32 int32 and UInt64 uint64. A query with two or more parameters
// takes a Params struct:
//
//   - AddSailing and FindSailings name their fields ID, Origin, Destination, Departs and
//     SeatsFree from {id:UInt64}, {origin:String} and the rest;
//   - SailingsDeparting compares departs twice, so it names the two bounds itself with
//     {earliest:String} and {latest:String}, and needs no binding line;
//   - BookSeats uses {seats:Int32} twice, but it is one field, bound once.
//
// Seagull sends each placeholder as p_<name>, as in {p_seats:Int32}, so a parameter may
// be called limit or offset without the server reading it as a keyword. The generated
// method binds each value with clickhouse.Named("p_seats", ...), after the generated
// clickhouse_values.go has turned it into the text ClickHouse parses, escaping
// backslashes, tabs, newlines, carriage returns and NULs.
//
// ALTER TABLE ... UPDATE reports no changed rows, so BookSeats is asyncexec and the
// booking reads seats_free before and after to see whether the guard let it through.
//
// Takes connection (store.DBTX) which is a native connection to a migrated database.
// Takes output (io.Writer) which receives the search results and bookings.
//
// Returns error when a query fails.
func run(ctx context.Context, connection store.DBTX, output io.Writer) error {
	queries := store.New(connection)

	timetable := []store.AddSailingParams{
		{ID: 1, Origin: "Penzance", Destination: "St Mary's", Departs: "09:15", SeatsFree: 40},
		{ID: 2, Origin: "St Mary's", Destination: "Tresco", Departs: "11:00", SeatsFree: 12},
		{ID: 3, Origin: "Penzance", Destination: "St Mary's", Departs: "14:30", SeatsFree: 4},
		{ID: 4, Origin: "St Mary's", Destination: "Penzance", Departs: "16:30", SeatsFree: 60},
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
	// nothing because seats_free >= {seats:Int32} is now false.
	booking := store.BookSeatsParams{Seats: 3, SailingID: 3}
	for range 2 {
		booked, err := bookSeats(ctx, queries, booking)
		if err != nil {
			return err
		}
		if !booked {
			fmt.Fprintf(output, "sailing %d has fewer than %d seats free\n", booking.SailingID, booking.Seats)
			continue
		}
		fmt.Fprintf(output, "booked %d seats on sailing %d\n", booking.Seats, booking.SailingID)
	}
	return nil
}

// bookSeats runs BookSeats and reports whether it took the seats, by comparing seats_free
// before and after. The connection sets mutations_sync=2, so the mutation has finished
// when BookSeats returns.
//
// Takes queries (*store.Queries) which runs the queries.
// Takes booking (store.BookSeatsParams) which is the sailing and the number of seats.
//
// Returns bool which is true when the seats were booked.
// Returns error when a query fails.
func bookSeats(ctx context.Context, queries *store.Queries, booking store.BookSeatsParams) (bool, error) {
	before, err := queries.SeatsFree(ctx, booking.SailingID)
	if err != nil {
		return false, fmt.Errorf("reading seats on sailing %d: %w", booking.SailingID, err)
	}
	if err := queries.BookSeats(ctx, booking); err != nil {
		return false, fmt.Errorf("booking sailing %d: %w", booking.SailingID, err)
	}
	after, err := queries.SeatsFree(ctx, booking.SailingID)
	if err != nil {
		return false, fmt.Errorf("reading seats on sailing %d: %w", booking.SailingID, err)
	}
	return after.SeatsFree != before.SeatsFree, nil
}

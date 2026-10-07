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
	"encoding/json"
	"fmt"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"case_runner/db"
)

func fail(step string, err error) {
	fmt.Fprintln(os.Stderr, step+":", err)
	os.Exit(1)
}

func main() {
	conn, err := sql.Open("mysql", os.Getenv("DATABASE_URL"))
	if err != nil {
		fail("open", err)
	}
	defer conn.Close()

	ctx := context.Background()
	queries := db.New(conn)

	bookings := []db.InsertBookingParams{
		{
			ID:        1,
			Label:     "valid",
			StartsOn:  time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
			StartsAt:  time.Date(2024, 2, 1, 10, 0, 0, 0, time.UTC),
			StampedAt: time.Date(2024, 2, 1, 10, 0, 0, 250_000_000, time.UTC),
			Opens:     "09:30:00",
			RawDate:   "2024-01-31",
		},
		{
			ID:        2,
			Label:     "junk",
			StartsOn:  time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC),
			StartsAt:  time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
			StampedAt: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC),
			Opens:     "18:00:00",
			RawDate:   "not a date",
		},
	}
	for _, booking := range bookings {
		if err := queries.InsertBooking(ctx, booking); err != nil {
			fail("InsertBooking", err)
		}
	}

	read, err := queries.ReadStringDates(ctx)
	if err != nil {
		fail("ReadStringDates", err)
	}

	counted, err := queries.CountBookingsInYear(ctx, 2024)
	if err != nil {
		fail("CountBookingsInYear", err)
	}

	shifted, err := queries.ShiftParameter(ctx, time.Date(2024, 2, 28, 12, 0, 0, 0, time.UTC))
	if err != nil {
		fail("ShiftParameter", err)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(map[string]any{"read": read, "counted": counted, "shifted": shifted}); err != nil {
		fail("encode", err)
	}
}

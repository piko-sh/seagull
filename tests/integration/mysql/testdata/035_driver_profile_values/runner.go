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

	_ "github.com/go-sql-driver/mysql"

	"case_runner/db"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	conn, err := sql.Open("mysql", os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx := context.Background()
	queries := db.New(conn)
	err = queries.AddShift(ctx, db.AddShiftParams{
		StartsAt: "09:30:00",
		Season:   2024,
		Flags:    []byte{0x05},
		EndsAt:   nil,
	})
	if err != nil {
		return fmt.Errorf("AddShift: %w", err)
	}

	shift, err := queries.GetShift(ctx, 1)
	if err != nil {
		return fmt.Errorf("GetShift: %w", err)
	}
	inSeason, err := queries.ShiftsInSeason(ctx, 2024)
	if err != nil {
		return fmt.Errorf("ShiftsInSeason: %w", err)
	}

	result := map[string]any{
		"starts_at":   shift.StartsAt,
		"season":      shift.Season,
		"flags":       fmt.Sprintf("%08b", shift.Flags),
		"ends_at_nil": shift.EndsAt == nil,
		"in_season":   inSeason,
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}

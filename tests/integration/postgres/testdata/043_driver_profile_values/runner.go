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

	_ "github.com/jackc/pgx/v5/stdlib"

	"case_runner/db"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	conn, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	ctx := context.Background()
	if _, err = conn.ExecContext(ctx, fmt.Sprintf("SET search_path TO %s", os.Getenv("DATABASE_SCHEMA"))); err != nil {
		return err
	}
	queries := db.New(conn)
	added, err := queries.AddShift(ctx, db.AddShiftParams{StartsAt: "09:30:00", Length: "01:30:00", EndsAt: nil})
	if err != nil {
		return fmt.Errorf("AddShift: %w", err)
	}
	shift, err := queries.GetShift(ctx, added.ID)
	if err != nil {
		return fmt.Errorf("GetShift: %w", err)
	}
	longer, err := queries.ShiftsLongerThan(ctx, "01:00:00")
	if err != nil {
		return fmt.Errorf("ShiftsLongerThan: %w", err)
	}

	result := map[string]any{
		"starts_at":   shift.StartsAt,
		"length":      shift.Length,
		"ends_at_nil": shift.EndsAt == nil,
		"longer":      longer,
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}

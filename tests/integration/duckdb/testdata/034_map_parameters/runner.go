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

	_ "github.com/duckdb/duckdb-go/v2"

	"case_runner/db"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	conn, err := sql.Open("duckdb", "")
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx := context.Background()
	_, err = conn.ExecContext(ctx, `CREATE TABLE fleets (
		id     INTEGER PRIMARY KEY,
		fees   MAP(VARCHAR, INTEGER) NOT NULL,
		extras MAP(VARCHAR, VARCHAR)
	)`)
	if err != nil {
		return err
	}

	queries := db.New(conn)
	err = queries.AddFleet(ctx, db.AddFleetParams{
		ID:     1,
		Fees:   map[string]int32{"July": 300, "August": 350},
		Extras: map[string]string{"skipper": "Morwenna"},
	})
	if err != nil {
		return fmt.Errorf("AddFleet with extras: %w", err)
	}
	err = queries.AddFleet(ctx, db.AddFleetParams{ID: 2, Fees: map[string]int32{"July": 120}, Extras: nil})
	if err != nil {
		return fmt.Errorf("AddFleet without extras: %w", err)
	}

	first, err := queries.GetFleet(ctx, 1)
	if err != nil {
		return fmt.Errorf("GetFleet 1: %w", err)
	}
	second, err := queries.GetFleet(ctx, 2)
	if err != nil {
		return fmt.Errorf("GetFleet 2: %w", err)
	}
	matching, err := queries.FleetsWithFees(ctx, map[string]int32{"July": 120})
	if err != nil {
		return fmt.Errorf("FleetsWithFees: %w", err)
	}

	result := map[string]any{
		"first_fees":           first.Fees,
		"first_extras":         first.Extras,
		"second_fees":          second.Fees,
		"second_extras_null":   second.Extras == nil,
		"matching_fleet_count": len(matching),
		"matching_fleet_id":    matching[0].ID,
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}

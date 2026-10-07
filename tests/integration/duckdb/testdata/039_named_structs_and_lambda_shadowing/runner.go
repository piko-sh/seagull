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
	ctx := context.Background()
	conn, err := sql.Open("duckdb", "")
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	if _, err = conn.ExecContext(ctx, `CREATE TABLE boats ( id INTEGER PRIMARY KEY, name VARCHAR NOT NULL, score VARCHAR, scores INTEGER[] NOT NULL )`); err != nil {
		return err
	}
	queries := db.New(conn)
	high, low := "high", "low"
	for _, boat := range []db.InsertBoatParams{
		{ID: 1, Name: "Swift", Score: &high, Scores: []int32{1, 5, 9}},
		{ID: 2, Name: "Gull", Score: &low, Scores: []int32{2, 3}},
		{ID: 3, Name: "Tern", Score: &high, Scores: []int32{7}},
	} {
		if err := queries.InsertBoat(ctx, boat); err != nil {
			return fmt.Errorf("InsertBoat %d: %w", boat.ID, err)
		}
	}

	structs, err := queries.BoatStructs(ctx)
	if err != nil {
		return fmt.Errorf("BoatStructs: %w", err)
	}
	highScoring, err := queries.HighScoringBoats(ctx, int32(4))
	if err != nil {
		return fmt.Errorf("HighScoringBoats: %w", err)
	}
	scoredAbove, err := queries.BoatsScoredAbove(ctx, db.BoatsScoredAboveParams{P1: int32(6), Score: &high})
	if err != nil {
		return fmt.Errorf("BoatsScoredAbove: %w", err)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]any{
		"boat_structs":       structs,
		"high_scoring_boats": highScoring,
		"boats_scored_above": scoredAbove,
	})
}

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
	_, err = conn.ExecContext(ctx, `CREATE TABLE crews (
		id    INTEGER PRIMARY KEY,
		names VARCHAR[],
		ranks INTEGER[] NOT NULL
	)`)
	if err != nil {
		return err
	}

	queries := db.New(conn)
	crews := []db.AddCrewParams{
		{ID: 1, Names: nil, Ranks: []int32{1}},
		{ID: 2, Names: []string{}, Ranks: []int32{}},
		{ID: 3, Names: []string{"Morwenna", "Tamsin"}, Ranks: []int32{2, 3}},
	}
	output := map[string]any{}
	for _, crew := range crews {
		if err := queries.AddCrew(ctx, crew); err != nil {
			return fmt.Errorf("AddCrew %d: %w", crew.ID, err)
		}
		stored, err := queries.GetCrew(ctx, crew.ID)
		if err != nil {
			return fmt.Errorf("GetCrew %d: %w", crew.ID, err)
		}
		output[fmt.Sprintf("crew_%d", crew.ID)] = stored
	}

	encoded, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}

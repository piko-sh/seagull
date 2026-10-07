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

func sized[S ~[]E, E any](_ S, length int) S {
	return make(S, length)
}

func allocated[T any](_ *T) *T {
	return new(T)
}

func run() error {
	conn, err := sql.Open("duckdb", "")
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx := context.Background()
	_, err = conn.ExecContext(ctx, `CREATE TABLE boats (
		id     INTEGER PRIMARY KEY,
		berth  STRUCT(pontoon VARCHAR, number INTEGER) NOT NULL,
		route  STRUCT(lat DOUBLE, lon DOUBLE)[] NOT NULL,
		extra  STRUCT(note VARCHAR, fees MAP(VARCHAR, INTEGER))
	)`)
	if err != nil {
		return err
	}

	queries := db.New(conn)

	kittiwake := db.MoorBoatParams{ID: 1}
	kittiwake.Berth.Pontoon = new("A")
	kittiwake.Berth.Number = new(int32(12))
	kittiwake.Route = sized(kittiwake.Route, 2)
	kittiwake.Route[0].Lat = new(50.15)
	kittiwake.Route[0].Lon = new(-5.07)
	kittiwake.Route[1].Lat = new(50.1)
	kittiwake.Route[1].Lon = new(-5.55)
	kittiwake.Extra = allocated(kittiwake.Extra)
	kittiwake.Extra.Note = new("winter mooring")
	kittiwake.Extra.Fees = map[string]int32{"July": 300}
	if err := queries.MoorBoat(ctx, kittiwake); err != nil {
		return fmt.Errorf("MoorBoat with extra: %w", err)
	}

	puffin := db.MoorBoatParams{ID: 2}
	puffin.Berth.Pontoon = new("B")
	puffin.Berth.Number = new(int32(3))
	puffin.Route = sized(puffin.Route, 0)
	if err := queries.MoorBoat(ctx, puffin); err != nil {
		return fmt.Errorf("MoorBoat without extra: %w", err)
	}

	first, err := queries.GetBoat(ctx, 1)
	if err != nil {
		return fmt.Errorf("GetBoat 1: %w", err)
	}
	second, err := queries.GetBoat(ctx, 2)
	if err != nil {
		return fmt.Errorf("GetBoat 2: %w", err)
	}
	atBerth, err := queries.BoatsAtBerth(ctx, puffin.Berth)
	if err != nil {
		return fmt.Errorf("BoatsAtBerth: %w", err)
	}

	result := map[string]any{
		"first_berth":       first.Berth,
		"first_route":       first.Route,
		"first_extra":       first.Extra,
		"second_route_size": len(second.Route),
		"second_extra_null": second.Extra == nil,
		"at_berth_count":    len(atBerth),
		"at_berth_id":       atBerth[0].ID,
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}

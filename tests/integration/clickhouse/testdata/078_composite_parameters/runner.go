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

	_ "github.com/ClickHouse/clickhouse-go/v2"

	"case_runner/db"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	conn, err := sql.Open("clickhouse", os.Getenv("DATABASE_URL"))
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer conn.Close()

	ctx := context.Background()
	queries := db.New(conn)

	location := map[string]any{"lon": -4.9389, "lat": 50.5413}
	stock := map[string]uint32{"crab": 3, "mackerel": 12}
	shelves := []map[string]uint32{{"top": 1}, {"middle": 2, "bottom": 3}}
	err = queries.OpenKiosk(ctx, db.OpenKioskParams{ID: 1, Location: location, Stock: stock, Shelves: shelves})
	if err != nil {
		return fmt.Errorf("OpenKiosk: %w", err)
	}
	err = queries.OpenKiosk(ctx, db.OpenKioskParams{
		ID:       2,
		Location: map[string]any{"lat": 51.0, "lon": -3.0},
		Stock:    map[string]uint32{"crab": 1},
		Shelves:  []map[string]uint32{},
	})
	if err != nil {
		return fmt.Errorf("OpenKiosk 2: %w", err)
	}

	at, err := queries.KioskAt(ctx, location)
	if err != nil {
		return fmt.Errorf("KioskAt: %w", err)
	}
	stocking, err := queries.KioskStocking(ctx, stock)
	if err != nil {
		return fmt.Errorf("KioskStocking: %w", err)
	}
	shelved, err := queries.KioskShelves(ctx, shelves)
	if err != nil {
		return fmt.Errorf("KioskShelves: %w", err)
	}

	encoded, err := json.Marshal(map[string]any{"at": at, "stocking": stocking, "shelved": shelved})
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}

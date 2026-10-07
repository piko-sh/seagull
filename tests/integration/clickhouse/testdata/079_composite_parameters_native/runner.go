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
	"encoding/json"
	"fmt"
	"os"

	"github.com/ClickHouse/clickhouse-go/v2"

	"case_runner/db"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	options, err := clickhouse.ParseDSN(os.Getenv("DATABASE_URL"))
	if err != nil {
		return fmt.Errorf("parse dsn: %w", err)
	}
	conn, err := clickhouse.Open(options)
	if err != nil {
		return fmt.Errorf("open native connection: %w", err)
	}
	defer conn.Close()

	ctx := context.Background()
	queries := db.New(conn)

	first := db.OpenKioskParams{ID: 1}
	first.Location.Lat = 50.5413
	first.Location.Lon = -4.9389
	first.Stock = map[string]uint32{"crab": 3, "mackerel": 12}
	first.Shelves = []map[string]uint32{{"top": 1}, {"middle": 2, "bottom": 3}}
	if err := queries.OpenKiosk(ctx, first); err != nil {
		return fmt.Errorf("OpenKiosk: %w", err)
	}
	second := db.OpenKioskParams{ID: 2}
	second.Location.Lat = 51.0
	second.Location.Lon = -3.0
	second.Stock = map[string]uint32{"crab": 1}
	second.Shelves = []map[string]uint32{}
	if err := queries.OpenKiosk(ctx, second); err != nil {
		return fmt.Errorf("OpenKiosk 2: %w", err)
	}

	at, err := queries.KioskAt(ctx, first.Location)
	if err != nil {
		return fmt.Errorf("KioskAt: %w", err)
	}
	stocking, err := queries.KioskStocking(ctx, first.Stock)
	if err != nil {
		return fmt.Errorf("KioskStocking: %w", err)
	}
	shelved, err := queries.KioskShelves(ctx, first.Shelves)
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

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
	if _, err = conn.ExecContext(ctx, `CREATE TABLE stations (
		id INTEGER PRIMARY KEY,
		name VARCHAR NOT NULL,
		region VARCHAR,
		reading DOUBLE NOT NULL,
		names VARCHAR[] NOT NULL,
		readings INTEGER[] NOT NULL
	)`); err != nil {
		return err
	}
	queries := db.New(conn)
	north := "north"
	for _, station := range []db.InsertStationParams{
		{ID: 1, Name: "Swift", Region: &north, Reading: 1.5, Names: []string{"a", "b"}, Readings: []int32{1, 2}},
		{ID: 2, Name: "Gull", Region: nil, Reading: 4, Names: []string{}, Readings: []int32{}},
	} {
		if err := queries.InsertStation(ctx, station); err != nil {
			return fmt.Errorf("InsertStation %d: %w", station.ID, err)
		}
	}
	maps, err := queries.StationMaps(ctx)
	if err != nil {
		return fmt.Errorf("StationMaps: %w", err)
	}
	var regionTypes []string
	for _, row := range maps {
		regionTypes = append(regionTypes, fmt.Sprintf("%T %T %T", row.ByName, row.Regions, row.NamedStructs))
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]any{"station_maps": maps, "go_types": regionTypes})
}

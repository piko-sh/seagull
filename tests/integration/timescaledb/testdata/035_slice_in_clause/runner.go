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
	connectionString := os.Getenv("DATABASE_URL")
	schemaName := os.Getenv("DATABASE_SCHEMA")

	conn, err := sql.Open("pgx", connectionString)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx := context.Background()

	if _, err = conn.ExecContext(ctx, fmt.Sprintf("SET search_path TO %s, public", schemaName)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)

	for _, device := range []struct {
		id   int64
		name string
	}{{1, "Alpha"}, {2, "Beta"}, {3, "Gamma"}} {
		if err = queries.InsertDevice(ctx, db.InsertDeviceParams{ID: device.id, Name: device.name}); err != nil {
			fmt.Fprintln(os.Stderr, "InsertDevice:", err)
			os.Exit(1)
		}
	}

	byIDs, err := queries.GetDevicesByIDs(ctx, db.GetDevicesByIDsParams{IDs: []int64{1, 3}})
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetDevicesByIDs:", err)
		os.Exit(1)
	}

	byEmpty, err := queries.GetDevicesByIDs(ctx, db.GetDevicesByIDsParams{IDs: []int64{}})
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetDevicesByIDs (empty):", err)
		os.Exit(1)
	}

	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
		"by_ids":   byIDs,
		"by_empty": byEmpty,
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

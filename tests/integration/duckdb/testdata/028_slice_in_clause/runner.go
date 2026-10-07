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
	conn, err := sql.Open("duckdb", "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx := context.Background()

	_, err = conn.ExecContext(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	_, err = conn.ExecContext(ctx, `INSERT INTO users (id, name) VALUES (1, 'Alice'), (2, 'Bob'), (3, 'Carol')`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)

	byIDs, err := queries.GetUsersByIDs(ctx, db.GetUsersByIDsParams{IDs: []int32{1, 3}})
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetUsersByIDs:", err)
		os.Exit(1)
	}

	byEmpty, err := queries.GetUsersByIDs(ctx, db.GetUsersByIDsParams{IDs: []int32{}})
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetUsersByIDs (empty):", err)
		os.Exit(1)
	}

	result := map[string]any{
		"by_ids":   byIDs,
		"by_empty": byEmpty,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

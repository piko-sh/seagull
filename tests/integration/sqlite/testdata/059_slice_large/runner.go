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

	"case_runner/db"
)

func main() {
	conn, err := sql.Open(databaseDriver, ":memory:")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx := context.Background()

	_, err = conn.ExecContext(ctx, `CREATE TABLE items (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL
	)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	for index := range int64(200) {
		id := index + 1
		_, err = conn.ExecContext(ctx, `INSERT INTO items (id, name) VALUES (?, ?)`, id, fmt.Sprintf("item_%d", id))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	queries := db.New(conn)

	ids := make([]int64, 150)
	for index := range ids {
		ids[index] = int64(index + 1)
	}

	rows, err := queries.FetchByIDs(ctx, db.FetchByIDsParams{
		IDs: ids,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "FetchByIDs:", err)
		os.Exit(1)
	}

	result := map[string]any{
		"count": len(rows),
		"first": rows[0],
		"last":  rows[len(rows)-1],
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

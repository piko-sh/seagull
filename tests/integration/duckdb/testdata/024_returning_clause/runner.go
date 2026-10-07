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

	_, err = conn.ExecContext(ctx, `CREATE TABLE items (
		id INTEGER PRIMARY KEY,
		name VARCHAR NOT NULL,
		price INTEGER NOT NULL
	)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)

	inserted, err := queries.InsertReturning(ctx, db.InsertReturningParams{ID: int32(1), Name: "Widget", Price: int32(1999)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "InsertReturning:", err)
		os.Exit(1)
	}

	updated, err := queries.UpdateReturning(ctx, db.UpdateReturningParams{ID: int32(1), Price: int32(2499)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "UpdateReturning:", err)
		os.Exit(1)
	}

	deleted, err := queries.DeleteReturning(ctx, int32(1))
	if err != nil {
		fmt.Fprintln(os.Stderr, "DeleteReturning:", err)
		os.Exit(1)
	}

	result := map[string]any{
		"insert_returning": inserted,
		"update_returning": updated,
		"delete_returning": deleted,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

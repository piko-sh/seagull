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

	_, err = conn.ExecContext(ctx, `CREATE TYPE mood AS ENUM ('happy', 'neutral', 'sad')`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	_, err = conn.ExecContext(ctx, `CREATE TABLE people (
		id INTEGER PRIMARY KEY,
		name VARCHAR NOT NULL,
		current_mood mood NOT NULL
	)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)

	if err := queries.InsertPerson(ctx, db.InsertPersonParams{ID: int32(1), Name: "Alice", CurrentMood: "happy"}); err != nil {
		fmt.Fprintln(os.Stderr, "InsertPerson 1:", err)
		os.Exit(1)
	}
	if err := queries.InsertPerson(ctx, db.InsertPersonParams{ID: int32(2), Name: "Bob", CurrentMood: "sad"}); err != nil {
		fmt.Fprintln(os.Stderr, "InsertPerson 2:", err)
		os.Exit(1)
	}
	if err := queries.InsertPerson(ctx, db.InsertPersonParams{ID: int32(3), Name: "Charlie", CurrentMood: "happy"}); err != nil {
		fmt.Fprintln(os.Stderr, "InsertPerson 3:", err)
		os.Exit(1)
	}

	person, err := queries.GetPerson(ctx, int32(1))
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetPerson:", err)
		os.Exit(1)
	}

	byMood, err := queries.ListByMood(ctx, "happy")
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListByMood:", err)
		os.Exit(1)
	}

	result := map[string]any{
		"get_person":   person,
		"list_by_mood": byMood,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

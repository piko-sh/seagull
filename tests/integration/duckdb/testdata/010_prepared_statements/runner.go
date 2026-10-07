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

	_, err = conn.ExecContext(ctx, `CREATE TABLE notes (id INTEGER PRIMARY KEY, title TEXT NOT NULL, body TEXT NOT NULL)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	directQueries := db.New(conn)
	if err := directQueries.InsertNote(ctx, db.InsertNoteParams{ID: int32(1), Title: "First", Body: "Body one"}); err != nil {
		fmt.Fprintln(os.Stderr, "Insert 1:", err)
		os.Exit(1)
	}
	if err := directQueries.InsertNote(ctx, db.InsertNoteParams{ID: int32(2), Title: "Second", Body: "Body two"}); err != nil {
		fmt.Fprintln(os.Stderr, "Insert 2:", err)
		os.Exit(1)
	}
	prepared, err := db.Prepare(ctx, conn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Prepare:", err)
		os.Exit(1)
	}
	defer prepared.Close()

	preparedQueries := db.New(prepared)
	note, err := preparedQueries.GetNote(ctx, int32(1))
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetNote:", err)
		os.Exit(1)
	}
	allNotes, err := preparedQueries.ListNotes(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListNotes:", err)
		os.Exit(1)
	}

	result := map[string]any{
		"note":      note,
		"all_notes": allNotes,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

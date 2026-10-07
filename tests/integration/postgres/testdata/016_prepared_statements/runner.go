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

	_, err = conn.ExecContext(ctx, fmt.Sprintf("SET search_path TO %s", schemaName))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	prepared, err := db.Prepare(ctx, conn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Prepare:", err)
		os.Exit(1)
	}
	defer prepared.Close()

	queries := db.New(prepared)

	err = queries.InsertNote(ctx, db.InsertNoteParams{
		Title: "First Note",
		Body:  "This is the body of the first note.",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "InsertNote 1:", err)
		os.Exit(1)
	}

	err = queries.InsertNote(ctx, db.InsertNoteParams{
		Title: "Second Note",
		Body:  "This is the body of the second note.",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "InsertNote 2:", err)
		os.Exit(1)
	}

	note, err := queries.GetNote(ctx, int32(1))
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetNote:", err)
		os.Exit(1)
	}

	allNotes, err := queries.ListNotes(ctx)
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

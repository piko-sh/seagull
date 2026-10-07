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

type results map[string]any

func (r results) record(name string, read func() (any, error)) {
	value, err := read()
	if err != nil {
		r[name] = "error: " + err.Error()
		return
	}
	r[name] = value
}

func main() {
	conn, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("SET search_path TO %s", os.Getenv("DATABASE_SCHEMA"))); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)
	kind, rowID, label, colour := int64(3), int64(42), "pinned", "red"
	output := results{}
	output.record("add_notes", func() (any, error) {
		if err := queries.AddNote(ctx, db.AddNoteParams{Body: "first", Kind: &kind}); err != nil {
			return nil, err
		}
		return "added", queries.AddNote(ctx, db.AddNoteParams{Body: "second", Kind: nil})
	})
	output.record("notes", func() (any, error) { return queries.ListNotes(ctx) })
	output.record("keyed_notes", func() (any, error) { return queries.ListKeyedNotes(ctx) })
	output.record("add_mark", func() (any, error) {
		return "added", queries.AddMark(ctx, db.AddMarkParams{Rowid: &rowID, Label: &label})
	})
	output.record("marks", func() (any, error) { return queries.ListMarks(ctx) })
	output.record("add_tag", func() (any, error) {
		return "added", queries.AddTag(ctx, db.AddTagParams{Label: "urgent", Colour: &colour})
	})
	output.record("tags", func() (any, error) { return queries.ListTags(ctx) })
	output.record("labels", func() (any, error) { return queries.ListLabels(ctx) })
	output.record("next_note_number", func() (any, error) { return queries.NextNoteNumber(ctx) })
	output.record("note_numbers", func() (any, error) { return queries.NoteNumbers(ctx) })

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

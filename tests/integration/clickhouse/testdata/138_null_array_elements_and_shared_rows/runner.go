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
	conn, err := sql.Open("clickhouse", os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx := context.Background()
	queries := db.New(conn)
	output := results{}
	output.record("add_with_null_elements", func() (any, error) {
		return "added", queries.AddNote(ctx, db.AddNoteParams{ID: 1, Tags: []*string{new("a"), nil, new("c")},
			Scores: []*int32{new(int32(1)), nil}, Plain: []string{"p"}, Limits: map[string]*int32{"x": new(int32(1)), "y": nil}})
	})
	output.record("add_empty", func() (any, error) {
		return "added", queries.AddNote(ctx, db.AddNoteParams{ID: 2, Tags: []*string{}, Scores: []*int32{nil}, Plain: []string{},
			Limits: map[string]*int32{}})
	})
	output.record("stored", func() (any, error) { return queries.StoredNotes(ctx) })
	output.record("model_note", func() (any, error) {
		var note db.Notes
		note, err := queries.GetNote(ctx, 1)
		return note, err
	})
	output.record("reused_model_notes", func() (any, error) {
		var notes []db.Notes
		notes, err := queries.ListNotes(ctx)
		return notes, err
	})
	output.record("add_authors", func() (any, error) {
		if err := queries.AddAuthor(ctx, db.AddAuthorParams{ID: 1, Name: "Agatha", Bio: new("crime")}); err != nil {
			return nil, err
		}
		return "added", queries.AddAuthor(ctx, db.AddAuthorParams{ID: 2, Name: "Dorothy", Bio: nil})
	})
	output.record("shared_row_authors", func() (any, error) {
		var authors []db.AuthorSummary
		authors, err := queries.ListAuthors(ctx)
		return authors, err
	})
	output.record("shared_row_named", func() (any, error) {
		var author db.AuthorSummary
		author, err := queries.AuthorNamed(ctx, "Dorothy")
		return author, err
	})

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

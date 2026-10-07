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

	"encoding/json"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"

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
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "SET search_path TO "+os.Getenv("DATABASE_SCHEMA")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)
	output := results{}
	output.record("add_with_null_elements", func() (any, error) {
		return "added", queries.AddNote(ctx, db.AddNoteParams{ID: 1, Tags: []*string{new("a"), nil, new("c")},
			Scores: []*int32{new(int32(1)), nil}, Moods: []*db.Mood{new(db.MoodCalm), nil}})
	})
	output.record("add_empty_and_null", func() (any, error) {
		return "added", queries.AddNote(ctx, db.AddNoteParams{ID: 2, Tags: []*string{}, Scores: nil, Moods: nil})
	})
	output.record("add_only_null", func() (any, error) {
		return "added", queries.AddNote(ctx, db.AddNoteParams{ID: 3, Tags: []*string{new("x")}, Scores: []*int32{nil},
			Moods: []*db.Mood{new(db.MoodStormy)}})
	})
	output.record("set_tags", func() (any, error) {
		return "set", queries.SetTags(ctx, db.SetTagsParams{Tags: []*string{nil, new("b")}, ID: 2})
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
	output.record("notes_by_id", func() (any, error) { return queries.NotesByID(ctx, []int32{1, 3}) })
	output.record("notes_without_tags", func() (any, error) { return queries.NotesWithoutTags(ctx, []string{"a", "x"}) })
	output.record("upsert_authors", func() (any, error) {
		return "upserted", queries.UpsertAuthors(ctx, db.UpsertAuthorsParams{IDs: []int64{1, 2, 3},
			Names: []string{"Agatha", "Dorothy", "Ngaio"}, Bios: []*string{new("crime"), nil, new("theatre")}})
	})
	output.record("upsert_again", func() (any, error) {
		return "upserted", queries.UpsertAuthors(ctx, db.UpsertAuthorsParams{IDs: []int64{3},
			Names: []string{"Ngaio Marsh"}, Bios: []*string{nil}})
	})
	output.record("shared_row_authors", func() (any, error) {
		var authors []db.AuthorSummary
		authors, err := queries.ListAuthors(ctx)
		return authors, err
	})
	output.record("shared_row_named", func() (any, error) {
		var authors []db.AuthorSummary
		authors, err := queries.AuthorsNamed(ctx, []string{"Dorothy", "Ngaio Marsh"})
		return authors, err
	})

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

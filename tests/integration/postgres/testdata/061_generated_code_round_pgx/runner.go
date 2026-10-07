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
	"case_runner/kinds"
)

const seed = `INSERT INTO authors VALUES (1, 'Agatha', ARRAY['Mary', NULL])`

const migration = `ALTER TABLE shows ADD COLUMN rating integer; ALTER TABLE authors ADD COLUMN born integer; ALTER TABLE books ADD COLUMN isbn text`

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

	for _, statement := range []string{fmt.Sprintf("SET search_path TO %s", os.Getenv("DATABASE_SCHEMA")), seed} {
		if _, err := conn.Exec(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	queries := db.New(conn)
	output := results{}
	tags := []string{"crime", "village"}
	output.record("insert_shows", func() (any, error) {
		if err := queries.InsertShow(ctx, db.InsertShowParams{ID: 1, Data: kinds.Show{Title: "Marple", Seasons: 6}, Tags: &tags,
			Colours: pointers(db.ColourRed)}); err != nil {
			return nil, err
		}
		return "inserted", queries.InsertShow(ctx, db.InsertShowParams{ID: 2, Data: kinds.Show{Title: "Poirot", Seasons: 13}, Tags: nil,
			Colours: pointers(db.ColourGreen, db.ColourBlue)})
	})
	output.record("show_with_tags", func() (any, error) { return queries.GetShow(ctx, 1) })
	output.record("show_without_tags", func() (any, error) { return queries.GetShow(ctx, 2) })
	output.record("shows_in_any_colour", func() (any, error) { return queries.ShowsInColours(ctx, db.ShowsInColoursParams{Wanted: nil}) })
	output.record("shows_in_blue", func() (any, error) {
		return queries.ShowsInColours(ctx, db.ShowsInColoursParams{Wanted: new(pointers(db.ColourBlue))})
	})
	output.record("upsert_books_repeating_a_key", func() (any, error) {
		return "upserted", queries.UpsertBooks(ctx, []db.UpsertBooksParams{
			{ID: 1, AuthorID: 1, Title: "Draft", Labels: pointers("first")},
			{ID: 2, AuthorID: 1, Title: "Sequel", Labels: []*string{}},
			{ID: 1, AuthorID: 1, Title: "The Murder at the Vicarage", Labels: pointers("first", "final")},
		})
	})
	output.record("books_with_authors", func() (any, error) { return queries.BooksWithAuthors(ctx) })
	output.record("shows_before_migration", func() (any, error) { return queries.ListShows(ctx) })
	output.record("retitled_before_migration", func() (any, error) {
		return queries.RetitleBook(ctx, db.RetitleBookParams{ID: 2, Title: "Nemesis"})
	})
	if _, err := conn.Exec(ctx, migration); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	output.record("shows_after_migration", func() (any, error) { return queries.ListShows(ctx) })
	output.record("books_with_authors_after_migration", func() (any, error) { return queries.BooksWithAuthors(ctx) })
	output.record("retitled_after_migration", func() (any, error) {
		return queries.RetitleBook(ctx, db.RetitleBookParams{ID: 2, Title: "Sleeping Murder"})
	})

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func pointers[T any](values ...T) []*T {
	pointed := make([]*T, len(values))
	for index := range values {
		pointed[index] = &values[index]
	}
	return pointed
}

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
	"case_runner/kinds"
)

const schema = `CREATE TABLE shows (
    id INTEGER PRIMARY KEY,
    data JSON NOT NULL,
    tags JSON,
    payload BLOB
);

CREATE TABLE books (
    id INTEGER PRIMARY KEY,
    title TEXT NOT NULL
);`

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
	conn, err := sql.Open(databaseDriver, ":memory:")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, schema); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)
	output := results{}
	tags := []string{"crime", "village"}
	output.record("insert_shows", func() (any, error) {
		if err := queries.InsertShow(ctx, db.InsertShowParams{ID: 1, Data: kinds.Show{Title: "Marple", Seasons: 6}, Tags: &tags, Payload: []byte(`{"n":1}`)}); err != nil {
			return nil, err
		}
		return "inserted", queries.InsertShow(ctx, db.InsertShowParams{ID: 2, Data: kinds.Show{Title: "Poirot", Seasons: 13}, Tags: nil, Payload: []byte(`[2,3]`)})
	})
	output.record("show_with_tags", func() (any, error) { return queries.GetShow(ctx, 1) })
	output.record("show_without_tags", func() (any, error) { return queries.GetShow(ctx, 2) })
	output.record("extracted_payloads", func() (any, error) { return queries.ExtractPayloads(ctx) })
	output.record("upsert_books_repeating_a_key", func() (any, error) {
		return "upserted", queries.UpsertBooks(ctx, []db.UpsertBooksParams{
			{ID: 1, Title: "Draft"}, {ID: 2, Title: "Sequel"}, {ID: 1, Title: "The Murder at the Vicarage"},
		})
	})
	output.record("shows_before_migration", func() (any, error) { return queries.ListShows(ctx) })
	output.record("books_before_migration", func() (any, error) { return queries.ListBooks(ctx) })
	output.record("retitled_before_migration", func() (any, error) {
		return queries.RetitleBook(ctx, db.RetitleBookParams{Title: "Nemesis", ID: 2})
	})
	for _, statement := range []string{`ALTER TABLE shows ADD COLUMN rating INTEGER`, `ALTER TABLE books ADD COLUMN isbn TEXT`} {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	output.record("shows_after_migration", func() (any, error) { return queries.ListShows(ctx) })
	output.record("books_after_migration", func() (any, error) { return queries.ListBooks(ctx) })
	output.record("retitled_after_migration", func() (any, error) {
		return queries.RetitleBook(ctx, db.RetitleBookParams{Title: "Sleeping Murder", ID: 2})
	})

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

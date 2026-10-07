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

const schema = `CREATE TABLE authors (id INTEGER PRIMARY KEY, name TEXT NOT NULL);
CREATE TABLE books (id INTEGER PRIMARY KEY, author_id INTEGER, title TEXT NOT NULL, price INTEGER, published TEXT NOT NULL);
INSERT INTO authors VALUES (1, 'Agatha'), (2, 'Dorothy'), (3, 'Ngaio');
INSERT INTO books VALUES (1, 1, 'Nemesis', 12, '2020-01-01'), (2, 2, 'Gaudy Night', NULL, '2021-01-01'), (3, NULL, 'Anonymous', 9, '2022-01-01');`

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
	output.record("book_flags", func() (any, error) { return queries.BookFlags(ctx) })
	output.record("authors_of_books", func() (any, error) { return queries.AuthorsOfBooks(ctx) })
	output.record("authors_beside_books", func() (any, error) { return queries.AuthorsBesideBooks(ctx) })
	output.record("book_count", func() (any, error) { return queries.BookCount(ctx) })
	output.record("ungrouped_title", func() (any, error) { return queries.UngroupedTitle(ctx) })
	output.record("add_book", func() (any, error) {
		return "added", queries.AddBook(ctx, db.AddBookParams{ID: 4, Name: "Ngaio", Title: "a man lay dead", Published: "3"})
	})
	output.record("added_book", func() (any, error) { return queries.GetBook(ctx, 4) })

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

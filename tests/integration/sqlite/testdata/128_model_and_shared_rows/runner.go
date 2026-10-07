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
	"time"

	"case_runner/db"
)

const schema = `CREATE TABLE books (id INTEGER PRIMARY KEY, title TEXT NOT NULL, pages INTEGER, published DATE);`

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

	pages := int64(288)
	published := time.Date(1930, time.October, 1, 0, 0, 0, 0, time.UTC)
	queries := db.New(conn)
	output := results{}
	output.record("add_books", func() (any, error) {
		if err := queries.AddBook(ctx, db.AddBookParams{ID: 1, Title: "The Murder at the Vicarage", Pages: &pages, Published: &published}); err != nil {
			return nil, err
		}
		return "added", queries.AddBook(ctx, db.AddBookParams{ID: 2, Title: "Strong Poison", Pages: nil, Published: nil})
	})
	output.record("model_book", func() (any, error) {
		var book db.Books
		book, err := queries.GetBook(ctx, 1)
		return book, err
	})
	output.record("reused_model_books", func() (any, error) {
		var books []db.Books
		books, err := queries.ListBooks(ctx)
		return books, err
	})
	output.record("streamed_model_books", func() (any, error) {
		var books []db.Books
		for book, err := range queries.StreamBooks(ctx) {
			if err != nil {
				return nil, err
			}
			books = append(books, book)
		}
		return books, nil
	})
	output.record("built_model_books", func() (any, error) {
		var books []db.Books
		books, err := queries.SearchBooks().Where("title", "LIKE", "Strong%").All(ctx)
		return books, err
	})
	output.record("shared_row_titles", func() (any, error) {
		var titles []db.BookTitle
		titles, err := queries.ListTitles(ctx)
		return titles, err
	})
	output.record("shared_row_title", func() (any, error) {
		var title db.BookTitle
		title, err := queries.TitleOf(ctx, 2)
		return title, err
	})

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

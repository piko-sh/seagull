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

	_ "github.com/jackc/pgx/v5/stdlib"

	"case_runner/db"
)

const seed = `INSERT INTO authors VALUES (1, 'Agatha'), (2, 'Dorothy'), (3, 'Ngaio');
INSERT INTO books VALUES (1, 1, 'Nemesis', 12, '{crime}', '2020-01-01'), (2, 2, 'Gaudy Night', NULL, '{crime,oxford}', '2021-01-01'),
    (3, NULL, 'Anonymous', 9, '{}', '2022-01-01')`

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
	for _, statement := range []string{fmt.Sprintf("SET search_path TO %s", os.Getenv("DATABASE_SCHEMA")), seed} {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	queries := db.New(conn)
	output := results{}
	output.record("book_flags", func() (any, error) { return queries.BookFlags(ctx) })
	output.record("authors_of_books", func() (any, error) { return queries.AuthorsOfBooks(ctx) })
	output.record("authors_beside_books", func() (any, error) { return queries.AuthorsBesideBooks(ctx) })
	output.record("book_count", func() (any, error) { return queries.BookCount(ctx) })
	output.record("add_book", func() (any, error) {
		return "added", queries.AddBook(ctx, db.AddBookParams{ID: 4, Name: "Ngaio", Title: "a man lay dead", Published: 3, Tags: pointers("crime")})
	})
	output.record("retag_book", func() (any, error) {
		return "retagged", queries.RetagBook(ctx, db.RetagBookParams{P1: 1, Tags: "classic", ID: 4})
	})
	output.record("added_book", func() (any, error) { return queries.GetBook(ctx, 4) })
	output.record("books_after", func() (any, error) {
		return queries.BooksAfter(ctx, db.BooksAfterParams{Published: time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC), ID: 1})
	})
	output.record("has_tag", func() (any, error) { return queries.HasTag(ctx, []string{"crime", "oxford"}) })

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

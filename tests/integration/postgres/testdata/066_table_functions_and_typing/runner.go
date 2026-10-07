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
	"case_runner/kinds"
)

const seed = `INSERT INTO users (id, name, bar, doc) VALUES (10, 'Ada', ROW(7, 'seven'), to_tsvector('simple', 'analytical engine')),
    (11, 'Grace', NULL, NULL)`

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
	output.record("users_named", func() (any, error) { return queries.UsersNamed(ctx, "Ada") })
	output.record("one_user", func() (any, error) { return queries.OneUser(ctx, 10) })
	output.record("one_user_missing", func() (any, error) { return queries.OneUser(ctx, 99) })
	output.record("two_outs", func() (any, error) { return queries.TwoOuts(ctx, 21) })
	output.record("author_id", func() (any, error) { return queries.AuthorID(ctx, "Ngaio") })
	output.record("twice", func() (any, error) { return queries.Twice(ctx, 4) })
	output.record("builtins", func() (any, error) {
		row, err := queries.Builtins(ctx)
		return map[string]any{"l": row.L, "now_read": !row.Now.IsZero()}, err
	})
	output.record("today", func() (any, error) {
		row, err := queries.Today(ctx)
		return map[string]any{"current_date_read": !row.CurrentDate.IsZero()}, err
	})
	output.record("name_lengths", func() (any, error) { return queries.NameLengths(ctx) })
	output.record("made_pair", func() (any, error) { return queries.MadePair(ctx) })
	output.record("create_user", func() (any, error) { return queries.CreateUser(ctx, "Hopper") })
	output.record("tidy", func() (any, error) { return "tidied", queries.Tidy(ctx, 2) })
	output.record("users_after_tidy", func() (any, error) { return queries.NameLengths(ctx) })
	output.record("populate", func() (any, error) { return queries.Populate(ctx, db.JSON(`{"id": 5, "label": "five"}`)) })
	output.record("populate_pairs", func() (any, error) {
		return queries.PopulatePairs(ctx, db.JSON(`[{"a": 1, "b": "one"}, {"a": 2}]`))
	})
	output.record("numbered", func() (any, error) {
		return queries.Numbered(ctx, db.NumberedParams{P1: pointers[int32](3, 4), P2: pointers("three", "four")})
	})
	output.record("arrays", func() (any, error) { return queries.Arrays(ctx) })
	output.record("fields", func() (any, error) { return queries.Fields(ctx, "seven") })
	output.record("rename", func() (any, error) {
		return "renamed", queries.Rename(ctx, db.RenameParams{ID: 11, NewName: "Grace H", OldName: "Grace"})
	})
	output.record("renamed", func() (any, error) { return queries.UsersNamed(ctx, "Grace H") })
	output.record("documents", func() (any, error) { return queries.Documents(ctx) })
	output.record("add_token", func() (any, error) {
		return "added", queries.AddToken(ctx, db.AddTokenParams{ID: kinds.Token{1, 2, 3, 4}, Label: "first"})
	})
	output.record("token_sizes", func() (any, error) { return queries.TokenSizes(ctx) })

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

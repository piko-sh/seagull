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
	for _, statement := range []string{fmt.Sprintf("SET search_path TO %s", os.Getenv("DATABASE_SCHEMA")), "SET TIME ZONE 'UTC'"} {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	queries := db.New(conn)
	output := results{}
	output.record("create_ada", func() (any, error) {
		return queries.CreateUser(ctx, db.CreateUserParams{Email: "Ada@Example.com", PasswordHash: "secret", Tags: pointers("admin", "early")})
	})
	output.record("create_bob", func() (any, error) {
		return queries.CreateUser(ctx, db.CreateUserParams{Email: "bob@example.org", PasswordHash: "hunter2", Tags: pointers("early")})
	})
	output.record("list_users", func() (any, error) {
		rows, err := queries.ListUsers(ctx)
		for index := range rows {
			rows[index].ID = [16]byte{}
		}
		return rows, err
	})
	output.record("digest", func() (any, error) { return queries.Digest(ctx, "bob@example.org") })
	output.record("similar", func() (any, error) { return queries.Similar(ctx, "bob@example.com") })
	output.record("identifiers", func() (any, error) { return queries.Identifiers(ctx) })
	output.record("all_tags", func() (any, error) { return queries.AllTags(ctx) })

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

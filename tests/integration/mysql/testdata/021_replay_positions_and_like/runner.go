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

	_ "github.com/go-sql-driver/mysql"

	"case_runner/db"
)

var seed = []string{
	`INSERT INTO t (z, a, m, b2, c, d) VALUES (NULL, 1, 2, 3, 'four', 5), (6, 7, 8, 9, 'ten', 11)`,
	`INSERT INTO users_copy (name, bio) VALUES ('ada', NULL)`,
	`INSERT INTO users_copy_parenthesised (name, bio) VALUES ('grace', 'admiral')`,
	`INSERT INTO pairs VALUES (1, 2, 'first')`,
	`INSERT INTO later_pairs VALUES (3, NULL, NULL)`,
	`INSERT INTO new_name VALUES (4, 'renamed')`,
	`INSERT INTO base_labels VALUES (5, 'viewed')`,
}

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
	conn, err := sql.Open("mysql", os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	ctx := context.Background()
	for _, statement := range seed {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	queries := db.New(conn)
	output := results{}
	output.record("t", func() (any, error) { return queries.ListT(ctx) })
	output.record("users_copy", func() (any, error) { return queries.ListUserCopies(ctx) })
	output.record("users_copy_parenthesised", func() (any, error) { return queries.ListUserParenthesisedCopies(ctx) })
	output.record("pairs", func() (any, error) { return queries.ListPairs(ctx) })
	output.record("later_pairs", func() (any, error) { return queries.ListLaterPairs(ctx) })
	output.record("renamed", func() (any, error) { return queries.ListRenamed(ctx) })
	output.record("renamed_view", func() (any, error) { return queries.ListRenamedView(ctx) })

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

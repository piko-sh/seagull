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

var seed = []string{
	`INSERT INTO items (id, name, price) VALUES (1, 'kettle', 30), (2, 'teapot', NULL)`,
	`INSERT INTO diary VALUES (1, 'calm'), (2, 'happy')`,
	`INSERT INTO pairs VALUES (1, 2, 'first'), (3, 4, NULL)`,
	`INSERT INTO later_pairs VALUES (5, 6, NULL)`,
	`INSERT INTO child (id, version, extra, label) VALUES (7, 3, 'young', 'child')`,
	`INSERT INTO copied (code, id, name, price, note) VALUES ('c', 8, 'mug', NULL, 'copied')`,
	`REFRESH MATERIALIZED VIEW snapshot`,
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
	conn, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	ctx := context.Background()
	for _, statement := range append([]string{fmt.Sprintf("SET search_path TO %s", os.Getenv("DATABASE_SCHEMA")), "SET TIME ZONE 'UTC'"}, seed...) {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	queries := db.New(conn)
	output := results{}
	output.record("items", func() (any, error) { return queries.ListItems(ctx) })
	output.record("diary", func() (any, error) { return queries.ListDiary(ctx) })
	output.record("pairs", func() (any, error) { return queries.ListPairs(ctx) })
	output.record("later_pairs", func() (any, error) { return queries.ListLaterPairs(ctx) })
	output.record("children", func() (any, error) { return queries.ListChildren(ctx) })
	output.record("copied", func() (any, error) { return queries.ListCopied(ctx) })
	output.record("star_view", func() (any, error) { return queries.ListStarView(ctx) })
	output.record("named_view", func() (any, error) { return queries.ListNamedView(ctx) })
	output.record("snapshot", func() (any, error) { return queries.ListSnapshot(ctx) })

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

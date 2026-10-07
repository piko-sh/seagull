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

const seed = `INSERT INTO signups (id, created_at, tags) VALUES (1, '2024-01-01 09:00:00+00', ARRAY['a', 'b']), (2, '2024-01-03 18:00:00+00', ARRAY[]::text[])`

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
	for _, statement := range []string{fmt.Sprintf("SET search_path TO %s", os.Getenv("DATABASE_SCHEMA")), "SET TIME ZONE 'UTC'", seed} {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	queries := db.New(conn)
	output := results{}
	output.record("daily_spine", func() (any, error) { return queries.DailySpine(ctx) })
	output.record("local_spine", func() (any, error) {
		return queries.LocalSpine(ctx, db.LocalSpineParams{P1: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), P2: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)})
	})
	output.record("date_spine", func() (any, error) { return queries.DateSpine(ctx) })
	output.record("counted", func() (any, error) { return queries.Counted(ctx) })
	output.record("tags", func() (any, error) { return queries.Tags(ctx) })

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

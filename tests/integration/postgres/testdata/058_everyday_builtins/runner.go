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

const seed = `INSERT INTO events (id, body, raw, payload, created_at, happened, amount, ratio, tags, booked, feeling) VALUES
(1, 'order  12, a  b', '\x6869'::bytea, '{"items": [{"id": 7}], "kind": "a"}', '2024-03-01 12:00:00+00', '2024-03-01 12:07:00', 16, 0.5, ARRAY['kind'], '[2024-03-01 00:00:00+00, 2024-03-02 00:00:00+00)', 'ok'),
(2, 'no digits', '\x'::bytea, '{"kind": "b"}', '2024-02-01 00:00:00+00', '2024-02-01 00:29:59', 16, 1.5, ARRAY['kind'], '[2024-02-01 00:00:00+00,)', 'happy')`

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
	output.record("ages", func() (any, error) { return queries.Ages(ctx) })
	output.record("numbers", func() (any, error) { return queries.Numbers(ctx) })
	output.record("exact_root", func() (any, error) {
		row, err := queries.Numbers(ctx)
		return row.ExactRoot.String(), err
	})
	output.record("texts", func() (any, error) { return queries.Texts(ctx) })
	output.record("documents", func() (any, error) { return queries.Documents(ctx) })
	output.record("mark_done", func() (any, error) { return "ok", queries.MarkDone(ctx, 1) })
	output.record("statuses", func() (any, error) { return queries.Statuses(ctx) })
	output.record("series", func() (any, error) { return queries.Series(ctx) })
	output.record("moods", func() (any, error) { return queries.Moods(ctx, 1) })
	output.record("new_id", func() (any, error) { return queries.NewID(ctx) })
	output.record("lock_account", func() (any, error) { return "ok", queries.LockAccount(ctx, 42) })
	output.record("try_lock", func() (any, error) { return queries.TryLock(ctx, 42) })

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

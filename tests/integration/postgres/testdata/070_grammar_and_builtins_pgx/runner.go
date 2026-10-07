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
	"encoding/json"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"

	"case_runner/db"
)

const seed = `INSERT INTO places (id, label, location, body, doc, created, tags) VALUES
    (1, 'harbour', point(0, 0), '\x0007'::bytea, '{"a": 3}', '2025-03-01 10:00:00+00', ARRAY['a', 'b']),
    (2, 'quay', point(3, 4), '\x01'::bytea, '{"a": 1}', '2025-03-02 10:00:00+00', ARRAY['c']),
    (3, 'pier', point(6, 8), '\x0203'::bytea, '{}', '2025-03-03 10:00:00+00', ARRAY[]::text[]);
INSERT INTO marks (id, row) VALUES (1, 7), (2, 8), (3, 7)`

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
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close(ctx)

	for _, statement := range []string{fmt.Sprintf("SET search_path TO %s", os.Getenv("DATABASE_SCHEMA")), seed} {
		if _, err := conn.Exec(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	queries := db.New(conn)
	output := results{}
	output.record("page", func() (any, error) { return queries.Page(ctx, db.PageParams{Size: 2, Page: 2}) })
	output.record("labels", func() (any, error) { return queries.Labels(ctx) })
	output.record("extremes", func() (any, error) { return queries.Extremes(ctx) })
	output.record("every_mark", func() (any, error) { return queries.EveryMark(ctx) })
	output.record("mark_rows", func() (any, error) { return queries.MarkRows(ctx, 7) })
	output.record("renamed", func() (any, error) { return queries.Renamed(ctx, 2) })
	output.record("nearest", func() (any, error) { return queries.Nearest(ctx, db.NearestParams{X: 5, Y: 7}) })
	output.record("everyday", func() (any, error) {
		row, err := queries.Everyday(ctx, db.EverydayParams{Vars: db.JSON(`{"min": 2}`), ID: 1})
		if err != nil {
			return nil, err
		}
		assigned := row.Transaction > 0
		row.Transaction = 0
		return map[string]any{"row": row, "transaction_assigned": assigned}, nil
	})

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

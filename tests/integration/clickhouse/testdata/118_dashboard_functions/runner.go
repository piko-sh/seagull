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

	_ "github.com/ClickHouse/clickhouse-go/v2"

	"case_runner/db"
)

const seed = `INSERT INTO events (id, user_id, kind, path, bytes, flags, duration, at, at_precise, day) VALUES (1, 7, 'view', '/a/b', 1536, 6, 1.5, '2024-03-05 10:07:00', '2024-03-05 10:07:00.250', '2024-03-05'), (2, 7, 'buy', '/c', 2048, 3, 2.5, '2024-03-05 10:20:00', '2024-03-05 10:20:00.999', '2024-03-05'), (3, 8, 'click', '/d/e/f', 10, 1, 4.0, '2024-03-06 23:59:59', '2024-03-06 23:59:59.001', '2024-03-06')`

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
	conn, err := sql.Open("clickhouse", os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	ctx := context.Background()
	for _, statement := range []string{seed} {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	queries := db.New(conn)
	output := results{}
	output.record("buckets", func() (any, error) { return queries.Buckets(ctx) })
	output.record("calendar", func() (any, error) { return queries.Calendar(ctx) })
	output.record("labels", func() (any, error) { return queries.Labels(ctx) })
	output.record("per_user", func() (any, error) { return queries.PerUser(ctx) })

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

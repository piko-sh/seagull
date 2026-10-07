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

const seed = `INSERT INTO conditions VALUES ('2024-01-01 00:00:00+00', 'kitchen', 20), ('2024-01-01 01:00:00+00', 'kitchen', 22),
    ('2024-01-01 02:00:00+00', 'hall', 15), ('2024-01-01 03:00:00+00', 'kitchen', 24)`

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

	for _, statement := range []string{fmt.Sprintf("SET search_path TO %s, public", os.Getenv("DATABASE_SCHEMA")), seed} {
		if _, err := conn.Exec(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	queries := db.New(conn)
	output := results{}
	output.record("mean_readings", func() (any, error) { return queries.MeanReadings(ctx) })
	output.record("percentiles", func() (any, error) { return queries.Percentiles(ctx) })
	output.record("points", func() (any, error) { return queries.Points(ctx, "kitchen") })
	output.record("durations", func() (any, error) { return queries.Durations(ctx) })

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

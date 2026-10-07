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
	"math"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"

	"case_runner/db"
)

const seed = `INSERT INTO conditions (time, device_id, temperature) VALUES ('2024-01-01 10:05:00+00', 'a', 20.0), ('2024-01-01 10:20:00+00', 'a', 22.0), ('2024-01-01 11:40:00+00', 'a', 24.0), ('2024-01-02 09:00:00+00', 'b', 18.0)`

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
	for _, statement := range []string{fmt.Sprintf("SET search_path TO %s, public", os.Getenv("DATABASE_SCHEMA")), "SET TIME ZONE 'UTC'", seed, "CALL refresh_continuous_aggregate('conditions_daily', NULL, NULL)"} {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	queries := db.New(conn)
	output := results{}
	output.record("hourly", func() (any, error) { return queries.Hourly(ctx) })
	output.record("aligned", func() (any, error) { return queries.Aligned(ctx) })
	output.record("daily", func() (any, error) { return queries.Daily(ctx) })
	output.record("percentiles", func() (any, error) {
		row, err := queries.Percentiles(ctx, "a")
		if err != nil || row.Median == nil || row.SketchMedian == nil {
			return row, err
		}
		return map[string]float64{"median": math.Round(*row.Median), "sketch_median": math.Round(*row.SketchMedian)}, nil
	})

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

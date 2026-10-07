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

	"case_runner/db"
)

type results map[string]any

const schema = `CREATE TABLE gauges (
  id INTEGER PRIMARY KEY,
  reading FLOAT NOT NULL,
  spare FLOAT4,
  narrow FLOAT(10) NOT NULL,
  wide FLOAT(53) NOT NULL,
  samples FLOAT[] NOT NULL,
  extremes STRUCT(low FLOAT, high DOUBLE)
);`

func (r results) record(name string, read func() (any, error)) {
	value, err := read()
	if err != nil {
		r[name] = "error: " + err.Error()
		return
	}
	r[name] = value
}

func main() {
	conn, err := sql.Open("duckdb", "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, schema); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)
	output := results{}
	output.record("add_gauge", func() (any, error) {
		return "added", queries.AddGauge(ctx, db.AddGaugeParams{ID: 1, Reading: 1.5, Spare: new(float32(0.5)), Narrow: 2.25,
			Wide: 0.1, Samples: []float32{0.5, 1.25}, Low: 0.75, High: 1e-9})
	})
	output.record("add_gauge_without_spare", func() (any, error) {
		return "added", queries.AddGauge(ctx, db.AddGaugeParams{ID: 2, Reading: -3.5, Spare: nil, Narrow: 0,
			Wide: 1e300, Samples: []float32{}, Low: 0, High: 0})
	})
	output.record("set_samples", func() (any, error) {
		return "set", queries.SetSamples(ctx, db.SetSamplesParams{Samples: []float32{8, 0.125}, ID: 2})
	})
	output.record("gauges_sampling_an_eighth", func() (any, error) {
		rows, err := queries.GaugesSampling(ctx, []float32{0.125, 42})
		return rows, err
	})
	for _, id := range []int32{1, 2} {
		output.record(fmt.Sprintf("gauge_%d", id), func() (any, error) {
			row, err := queries.GetGauge(ctx, id)
			return row, err
		})
	}
	output.record("go_types", func() (any, error) {
		row, err := queries.GetGauge(ctx, 1)
		return []string{fmt.Sprintf("%T", row.Reading), fmt.Sprintf("%T", row.Spare), fmt.Sprintf("%T", row.Narrow),
			fmt.Sprintf("%T", row.Wide), fmt.Sprintf("%T", row.Samples), fmt.Sprintf("%T", row.Bumped),
			fmt.Sprintf("%T", row.Quarter)}, err
	})

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

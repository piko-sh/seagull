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
	"strings"

	"github.com/duckdb/duckdb-go/v2"

	"case_runner/db"
)

type results map[string]any

const schema = `CREATE TYPE mood AS ENUM ('calm', 'stormy');
CREATE TABLE logs (
  id INTEGER PRIMARY KEY,
  moods MAP(VARCHAR, mood),
  counts MAP(mood, INTEGER),
  entry STRUCT(note VARCHAR, feeling mood),
  history STRUCT(feelings mood[]),
  entries STRUCT(feeling mood)[]
);`

func (r results) record(name string, read func() (any, error)) {
	value, err := read()
	if err != nil {
		r[name] = "error: " + err.Error()
		return
	}
	r[name] = value
}

func refusedByTheDriver(err error) bool {
	return err != nil && strings.Contains(err.Error(), "unsupported data type")
}

func feelingOf(row db.GetLogRow) db.Mood {
	if row.Entry == nil || row.Entry.Feeling == nil {
		return ""
	}
	return *row.Entry.Feeling
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
	output.record("add_log", func() (any, error) {
		return "added", queries.AddLog(ctx, db.AddLogParams{
			ID:          1,
			MoodNames:   []string{"morning", "evening"},
			MoodValues:  []db.Mood{db.MoodCalm, db.MoodStormy},
			CountMoods:  []db.Mood{db.MoodStormy},
			CountValues: []int32{3},
			Note:        "fair winds",
			Feeling:     db.MoodStormy,
			Feelings:    []db.Mood{db.MoodCalm, db.MoodStormy},
		})
	})
	output.record("add_empty_log", func() (any, error) {
		return "added", queries.AddEmptyLog(ctx, 2)
	})
	for _, id := range []int32{1, 2} {
		output.record(fmt.Sprintf("log_%d", id), func() (any, error) {
			return queries.GetLog(ctx, id)
		})
		output.record(fmt.Sprintf("log_%d_feelings", id), func() (any, error) {
			return queries.LogFeelings(ctx, id)
		})
	}
	output.record("feeling_of_log_1", func() (any, error) {
		row, err := queries.GetLog(ctx, 1)
		return feelingOf(row), err
	})
	output.record("stormy_logs", func() (any, error) {
		return queries.LogsFeeling(ctx, db.MoodStormy)
	})
	output.record("stormy_logs_by_key", func() (any, error) {
		return queries.LogsFeelingByKey(ctx, db.MoodStormy)
	})
	output.record("stormy_logs_extracted", func() (any, error) {
		return queries.LogsFeelingExtracted(ctx, db.MoodStormy)
	})
	output.record("calm_mornings", func() (any, error) {
		return queries.LogsWithMorning(ctx, db.MoodCalm)
	})
	output.record("driver_refuses_a_map_holding_an_enum", func() (any, error) {
		_, err := conn.ExecContext(ctx, "UPDATE logs SET moods = $1 WHERE id = 1", duckdb.Map{"evening": "calm"})
		return refusedByTheDriver(err), nil
	})
	output.record("driver_refuses_a_map_keyed_by_an_enum", func() (any, error) {
		_, err := conn.ExecContext(ctx, "UPDATE logs SET counts = $1 WHERE id = 1", duckdb.Map{"calm": int32(1)})
		return refusedByTheDriver(err), nil
	})
	output.record("driver_refuses_a_struct_holding_an_enum", func() (any, error) {
		_, err := conn.ExecContext(ctx, "UPDATE logs SET entry = $1 WHERE id = 1", map[string]any{"note": "squall", "feeling": "calm"})
		return refusedByTheDriver(err), nil
	})

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

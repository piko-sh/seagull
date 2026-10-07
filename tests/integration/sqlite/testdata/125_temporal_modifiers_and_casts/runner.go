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

	"github.com/cockroachdb/apd/v3"

	"case_runner/db"
)

const schema = `CREATE TABLE events (
  id INTEGER PRIMARY KEY,
  happened DATETIME NOT NULL,
  precise TIMESTAMP(3) NOT NULL,
  micro DATETIME(6),
  day DATE(10)
);`

type results map[string]any

func (r results) record(name string, read func() (any, error)) {
	value, err := read()
	if err != nil {
		r[name] = "error: " + err.Error()
		return
	}
	r[name] = value
}

func text(value *apd.Decimal) any {
	if value == nil {
		return nil
	}
	return value.String()
}

func main() {
	conn, err := sql.Open(databaseDriver, ":memory:")
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
	happened := time.Date(2024, 3, 1, 12, 30, 0, 0, time.UTC)
	micro := "2024-03-01 12:30:00.123456"
	day := "2024-03-01"
	output.record("add_event", func() (any, error) {
		return "added", queries.AddEvent(ctx, db.AddEventParams{ID: 1, Happened: happened, Precise: "2024-03-01 12:30:00.123", Micro: &micro, Day: &day})
	})
	output.record("add_event_without_options", func() (any, error) {
		return "added", queries.AddEvent(ctx, db.AddEventParams{ID: 2, Happened: happened, Precise: "2024-03-02 08:00:00.000", Micro: nil, Day: nil})
	})
	output.record("event", func() (any, error) {
		event, err := queries.GetEvent(ctx, 1)
		return &event, err
	})
	output.record("event_without_options", func() (any, error) {
		event, err := queries.GetEvent(ctx, 2)
		return &event, err
	})
	output.record("events_at", func() (any, error) { return queries.EventsAt(ctx, "2024-03-02 08:00:00.000") })
	output.record("casts", func() (any, error) {
		casts, err := queries.EventCasts(ctx, 1)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"precise_year": casts.PreciseYear.String(), "day_year": text(casts.DayYear), "happened_year": casts.HappenedYear.String(),
			"fraction": casts.Fraction.String(), "micro_year": text(casts.MicroYear),
		}, nil
	})
	output.record("casts_without_options", func() (any, error) {
		casts, err := queries.EventCasts(ctx, 2)
		if err != nil {
			return nil, err
		}
		return map[string]any{"day_year": text(casts.DayYear), "micro_year": text(casts.MicroYear)}, nil
	})
	output.record("stored_classes", func() (any, error) {
		row := conn.QueryRowContext(ctx, "SELECT typeof(happened) || ',' || typeof(precise) || ',' || typeof(micro) || ',' || typeof(day) FROM events WHERE id = 1")
		var classes string
		return classes, row.Scan(&classes)
	})

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

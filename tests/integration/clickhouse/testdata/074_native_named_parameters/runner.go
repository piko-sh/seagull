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
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/shopspring/decimal"

	"case_runner/db"
)

type eventOutput struct {
	Note    *string `json:"note"`
	Host    string  `json:"host"`
	Day     string  `json:"day"`
	SeenAt  string  `json:"seen_at"`
	Amount  string  `json:"amount"`
	ID      uint64  `json:"id"`
	InStock bool    `json:"in_stock"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	options, err := clickhouse.ParseDSN(os.Getenv("DATABASE_URL"))
	if err != nil {
		return fmt.Errorf("parse dsn: %w", err)
	}
	conn, err := clickhouse.Open(options)
	if err != nil {
		return fmt.Errorf("open native connection: %w", err)
	}
	defer conn.Close()

	ctx := context.Background()
	queries := db.New(conn)

	if err := seed(ctx, queries); err != nil {
		return err
	}

	results := map[string]any{}
	for _, read := range []struct {
		name string
		run  func() (any, error)
	}{
		{name: "event_1", run: func() (any, error) { return getEvent(ctx, queries, 1) }},
		{name: "event_2", run: func() (any, error) { return getEvent(ctx, queries, 2) }},
		{name: "event_3", run: func() (any, error) { return getEvent(ctx, queries, 3) }},
		{name: "event_5", run: func() (any, error) { return getEvent(ctx, queries, 5) }},
		{name: "by_ids", run: func() (any, error) { return queries.EventsByIDs(ctx, []uint64{1, 3, 5}) }},
		{name: "page", run: func() (any, error) {
			return queries.PageEvents(ctx, db.PageEventsParams{Limit: 2, Offset: 1})
		}},
		{name: "on_day", run: func() (any, error) {
			return queries.EventsOnDay(ctx, db.EventsOnDayParams{
				Day:       time.Date(2024, 3, 5, 0, 0, 0, 0, time.UTC),
				MinAmount: decimal.RequireFromString("10.5"),
			})
		}},
		{name: "seen_since", run: func() (any, error) {
			return queries.EventsSeenSince(ctx, time.Date(2024, 3, 5, 10, 0, 0, 250_000_000, time.UTC))
		}},
		{name: "host_or_note", run: func() (any, error) { return queries.EventsByHostOrNote(ctx, "beta") }},
		{name: "without_note", run: func() (any, error) { return queries.EventsWithoutNote(ctx, true) }},
		{name: "runtime_builder", run: func() (any, error) {
			return queries.SearchEvents(true).Where("host", "!=", "gamma").OrderBy("id", "DESC").Limit(2).All(ctx)
		}},
	} {
		value, readErr := read.run()
		if readErr != nil {
			return fmt.Errorf("%s: %w", read.name, readErr)
		}
		results[read.name] = value
	}
	return json.NewEncoder(os.Stdout).Encode(results)
}

func seed(ctx context.Context, queries *db.Queries) error {
	tricky := "back\\slash, tab\there, newline\nthere, quote's and \\N"
	note := "beta"
	day := time.Date(2024, 3, 5, 0, 0, 0, 0, time.UTC)
	for _, event := range []db.InsertEventParams{
		{ID: 1, Host: tricky, Note: nil, Day: day, SeenAt: time.Date(2024, 3, 5, 9, 59, 59, 999_000_000, time.UTC), InStock: true, Amount: decimal.RequireFromString("10.5")},
		{ID: 2, Host: "alpha", Note: &note, Day: day, SeenAt: time.Date(2024, 3, 5, 10, 0, 0, 250_000_000, time.UTC), InStock: false, Amount: decimal.RequireFromString("3.1415")},
		{ID: 3, Host: "beta", Note: nil, Day: day.AddDate(0, 0, 1), SeenAt: time.Date(2024, 3, 6, 8, 0, 0, 0, time.UTC), InStock: true, Amount: decimal.RequireFromString("99.9999")},
	} {
		if err := queries.InsertEvent(ctx, event); err != nil {
			return fmt.Errorf("insert event %d: %w", event.ID, err)
		}
	}
	return queries.InsertEvents(ctx, []db.InsertEventsParams{
		{ID: 4, Host: "gamma", Note: nil, Day: day, SeenAt: time.Date(2024, 3, 5, 12, 0, 0, 0, time.UTC), InStock: true, Amount: decimal.RequireFromString("12")},
		{ID: 5, Host: "delta", Note: &note, Day: day, SeenAt: time.Date(2024, 3, 5, 13, 0, 0, 0, time.UTC), InStock: true, Amount: decimal.RequireFromString("0.0001")},
	})
}

func getEvent(ctx context.Context, queries *db.Queries, id uint64) (eventOutput, error) {
	row, err := queries.GetEvent(ctx, id)
	if err != nil {
		return eventOutput{}, err
	}
	return eventOutput{
		Note:    row.Note,
		Host:    row.Host,
		Day:     row.Day.UTC().Format(time.DateOnly),
		SeenAt:  row.SeenAt.UTC().Format(time.RFC3339Nano),
		Amount:  row.Amount.String(),
		ID:      row.ID,
		InStock: row.InStock,
	}, nil
}

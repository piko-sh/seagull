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
	"strconv"
	"time"

	_ "github.com/ClickHouse/clickhouse-go/v2"

	"case_runner/db"
)

const copiedRows = 600

type eventOutput struct {
	Note *string `json:"note"`
	Host string  `json:"host"`
	Day  string  `json:"day"`
	ID   uint64  `json:"id"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	conn, err := sql.Open("clickhouse", os.Getenv("DATABASE_URL"))
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer conn.Close()

	ctx := context.Background()
	queries := db.New(conn)

	note := "with\\backslash"
	day := time.Date(2024, 3, 5, 0, 0, 0, 0, time.UTC)
	if err := queries.InsertEvents(ctx, []db.InsertEventsParams{
		{ID: 1, Host: "ALPHA", Note: nil, Day: day},
		{ID: 2, Host: "Beta", Note: &note, Day: day.AddDate(0, 0, 1)},
		{ID: 3, Host: "gamma", Note: nil, Day: day.AddDate(0, 0, 2)},
	}); err != nil {
		return fmt.Errorf("insert events: %w", err)
	}

	copied := make([]db.CopyEventsParams, 0, copiedRows)
	for index := range copiedRows {
		row := db.CopyEventsParams{ID: uint64(1000 + index), Host: "host-" + strconv.Itoa(index%7), Note: nil, Day: day}
		if index%2 == 0 {
			row.Note = &note
		}
		copied = append(copied, row)
	}
	if err := queries.CopyEvents(ctx, copied); err != nil {
		return fmt.Errorf("copy events: %w", err)
	}

	listed, err := queries.ListEventsBelow(ctx, 1000)
	if err != nil {
		return fmt.Errorf("list events: %w", err)
	}
	batched := make([]eventOutput, 0, len(listed))
	for _, row := range listed {
		batched = append(batched, eventOutput{Note: row.Note, Host: row.Host, Day: row.Day.UTC().Format(time.DateOnly), ID: row.ID})
	}

	summary, err := queries.SummariseEvents(ctx, 1000)
	if err != nil {
		return fmt.Errorf("summarise events: %w", err)
	}

	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"batched": batched,
		"copied":  summary,
	})
}

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

	_ "github.com/ClickHouse/clickhouse-go/v2"

	"case_runner/db"
)

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

	if err := queries.RecordVisits(ctx); err != nil {
		return fmt.Errorf("record visits: %w", err)
	}
	if err := queries.RecordQuietDay(ctx); err != nil {
		return fmt.Errorf("record quiet day: %w", err)
	}
	if err := queries.AddHits(ctx, db.AddHitsParams{Day: time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC), Hits: 2}); err != nil {
		return fmt.Errorf("add hits: %w", err)
	}
	simple, err := queries.Simple(ctx)
	if err != nil {
		return fmt.Errorf("simple: %w", err)
	}
	finalised, err := queries.Finalised(ctx)
	if err != nil {
		return fmt.Errorf("finalised: %w", err)
	}
	merged, err := queries.Merged(ctx)
	if err != nil {
		return fmt.Errorf("merged: %w", err)
	}
	checks := make([]map[string]any, 0, len(simple))
	for index, row := range simple {
		checks = append(checks, map[string]any{
			"hits_plus_one": row.Hits + 1, "has_longest": row.Longest != nil, "pages": len(row.Pages),
			"visitors_plus_total": finalised[index].Visitors + finalised[index].Total, "last_page": finalised[index].LastPage + "!",
		})
	}

	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"simple": simple, "finalised": finalised, "merged": merged, "checks": checks,
		"merged_sum": merged.Visitors + merged.Total + merged.Hits,
	})
}

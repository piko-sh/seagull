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

func main() {
	conn, err := sql.Open(databaseDriver, ":memory:")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx := context.Background()

	_, err = conn.ExecContext(ctx, `CREATE TABLE logs (
		id INTEGER PRIMARY KEY,
		message TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		unix_ts INTEGER NOT NULL
	)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)

	err = queries.InsertLog(ctx, db.InsertLogParams{
		ID:        int64(1),
		Message:   "start",
		CreatedAt: "2025-01-15 10:00:00",
		UnixTs:    int64(1736935200),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "InsertLog 1:", err)
		os.Exit(1)
	}

	err = queries.InsertLog(ctx, db.InsertLogParams{
		ID:        int64(2),
		Message:   "middle",
		CreatedAt: "2025-06-15 12:00:00",
		UnixTs:    int64(1750075200),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "InsertLog 2:", err)
		os.Exit(1)
	}

	err = queries.InsertLog(ctx, db.InsertLogParams{
		ID:        int64(3),
		Message:   "end",
		CreatedAt: "2025-12-01 08:00:00",
		UnixTs:    int64(1764576000),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "InsertLog 3:", err)
		os.Exit(1)
	}

	log, err := queries.GetLog(ctx, int64(1))
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetLog:", err)
		os.Exit(1)
	}

	byRange, err := queries.ListByDateRange(ctx, db.ListByDateRangeParams{
		CreatedAtFrom: "2025-01-01 00:00:00",
		CreatedAtTo:   "2025-07-01 00:00:00",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListByDateRange:", err)
		os.Exit(1)
	}

	formatted, err := queries.FormatDate(ctx, int64(2))
	if err != nil {
		fmt.Fprintln(os.Stderr, "FormatDate:", err)
		os.Exit(1)
	}

	result := map[string]any{
		"log":       log,
		"by_range":  byRange,
		"formatted": formatted,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

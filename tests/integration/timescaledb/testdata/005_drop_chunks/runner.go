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

	_ "github.com/jackc/pgx/v5/stdlib"

	"case_runner/db"
)

func main() {
	connectionString := os.Getenv("DATABASE_URL")
	schemaName := os.Getenv("DATABASE_SCHEMA")

	conn, err := sql.Open("pgx", connectionString)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx := context.Background()

	_, err = conn.ExecContext(ctx, fmt.Sprintf("SET search_path TO %s, public", schemaName))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)

	baseline, _ := time.Parse(time.RFC3339, "2026-01-01T00:00:00Z")
	for day := range 5 {
		err = queries.InsertChunkEvent(ctx, db.InsertChunkEventParams{
			Ts:      baseline.AddDate(0, 0, day),
			EventID: int64(day + 1),
			Payload: new(fmt.Sprintf("event %d", day+1)),
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	totalBefore, err := queries.CountEvents(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	chunksBefore, err := countChunks(ctx, conn)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	cutoff := baseline.AddDate(0, 0, 2)
	_, err = conn.ExecContext(ctx,
		"SELECT drop_chunks('chunk_events', $1::timestamptz)", cutoff)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	totalAfter, err := queries.CountEvents(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	chunksAfter, err := countChunks(ctx, conn)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
		"row_count_before":   totalBefore,
		"row_count_after":    totalAfter,
		"chunk_count_before": chunksBefore,
		"chunk_count_after":  chunksAfter,
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func countChunks(ctx context.Context, conn *sql.DB) (int64, error) {
	var count int64
	err := conn.QueryRowContext(ctx,
		"SELECT count(*) FROM show_chunks('chunk_events')").Scan(&count)
	return count, err
}

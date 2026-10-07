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

	_ "github.com/jackc/pgx/v5/stdlib"

	"case_runner/db"
)

func main() {
	conn, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx := context.Background()
	if _, err = conn.ExecContext(ctx, fmt.Sprintf("SET search_path TO %s", os.Getenv("DATABASE_SCHEMA"))); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)

	uniqueKey := "job-unique-key"
	correlationID := "corr-1"
	row, err := queries.InsertJobRootWithUniqueKey(ctx, db.InsertJobRootWithUniqueKeyParams{
		ID:             "job-1",
		Kind:           "email",
		Queue:          "default",
		Payload:        "{}",
		UniqueKey:      &uniqueKey,
		CorrelationID:  &correlationID,
		MaxAttempts:    3,
		TimeoutSeconds: 30,
		CreatedAt:      "2026-07-10T00:00:00Z",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(map[string]any{"inserted_id": row.ID}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

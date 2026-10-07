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

	_ "github.com/go-sql-driver/mysql"

	"case_runner/db"
)

func main() {
	connectionString := os.Getenv("DATABASE_URL")

	conn, err := sql.Open("mysql", connectionString)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx := context.Background()

	_, err = conn.ExecContext(ctx, `INSERT INTO tasks (id, status, priority, title) VALUES
		('t1', 'PENDING', 1, 'Low pending'),
		('t2', 'PENDING', 3, 'High pending'),
		('t3', 'PROCESSING', 2, 'Mid processing'),
		('t4', 'COMPLETE', 1, 'Done task'),
		('t5', 'FAILED', 3, 'High failed'),
		('t6', 'RETRYING', 2, 'Mid retrying')`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)
	fetched, err := queries.FetchByStatusesAndPriorities(ctx, db.FetchByStatusesAndPrioritiesParams{
		Statuses:   []string{"PENDING", "RETRYING"},
		Priorities: []int32{2, 3},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "FetchByStatusesAndPriorities:", err)
		os.Exit(1)
	}

	result := map[string]any{
		"fetch_by_statuses_and_priorities": fetched,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

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

	_, err = conn.ExecContext(ctx, `CREATE TABLE sales (
		id INTEGER PRIMARY KEY,
		employee TEXT NOT NULL,
		amount INTEGER NOT NULL,
		sale_date TEXT NOT NULL
	)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	_, err = conn.ExecContext(ctx, `INSERT INTO sales (id, employee, amount, sale_date) VALUES
		(1, 'Alice', 100, '2025-01'),
		(2, 'Bob', 200, '2025-02'),
		(3, 'Alice', 150, '2025-03'),
		(4, 'Bob', 50, '2025-04'),
		(5, 'Alice', 300, '2025-05'),
		(6, 'Bob', 175, '2025-06')`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)

	runningTotal, err := queries.ListWithRunningTotal(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListWithRunningTotal:", err)
		os.Exit(1)
	}

	lagLead, err := queries.ListWithLagLead(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListWithLagLead:", err)
		os.Exit(1)
	}

	rankByEmployee, err := queries.ListWithRankByEmployee(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListWithRankByEmployee:", err)
		os.Exit(1)
	}

	ntile, err := queries.ListWithNtile(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListWithNtile:", err)
		os.Exit(1)
	}

	result := map[string]any{
		"running_total":    runningTotal,
		"lag_lead":         lagLead,
		"rank_by_employee": rankByEmployee,
		"ntile":            ntile,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

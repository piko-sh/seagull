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

	_, err = conn.ExecContext(ctx, `CREATE TABLE employees (id INTEGER PRIMARY KEY, name TEXT NOT NULL, manager_id INTEGER REFERENCES employees(id))`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	_, err = conn.ExecContext(ctx, `INSERT INTO employees (id, name, manager_id) VALUES (1, 'Ada', NULL), (2, 'Grace', 1), (3, 'Linus', 2)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)

	withManager, err := queries.ListEmployeesWithManager(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListEmployeesWithManager:", err)
		os.Exit(1)
	}

	reportingLines, err := queries.ListReportingLines(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListReportingLines:", err)
		os.Exit(1)
	}

	var topLevel db.ListEmployeesWithManagerRow = withManager[0]
	var manager *db.ListEmployeesWithManagerManager = withManager[1].Manager
	var report db.ListReportingLinesReport = reportingLines[0].Report

	result := map[string]any{
		"with_manager":             withManager,
		"reporting_lines":          reportingLines,
		"top_level_has_no_manager": topLevel.Manager == nil,
		"second_manager_name":      manager.Name,
		"first_report_name":        report.Name,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

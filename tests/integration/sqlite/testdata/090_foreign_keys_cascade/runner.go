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
	_, err = conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`)
	if err != nil {
		fmt.Fprintln(os.Stderr, "PRAGMA foreign_keys:", err)
		os.Exit(1)
	}

	_, err = conn.ExecContext(ctx, `CREATE TABLE departments (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL
	)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	_, err = conn.ExecContext(ctx, `CREATE TABLE employees (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		dept_id INTEGER NOT NULL REFERENCES departments(id) ON DELETE CASCADE ON UPDATE CASCADE
	)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)
	err = queries.InsertDepartment(ctx, db.InsertDepartmentParams{
		ID:   int64(1),
		Name: "Engineering",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "insert dept 1:", err)
		os.Exit(1)
	}

	err = queries.InsertDepartment(ctx, db.InsertDepartmentParams{
		ID:   int64(2),
		Name: "Sales",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "insert dept 2:", err)
		os.Exit(1)
	}
	err = queries.InsertEmployee(ctx, db.InsertEmployeeParams{
		ID:     int64(1),
		Name:   "Alice",
		DeptID: int64(1),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "insert emp 1:", err)
		os.Exit(1)
	}

	err = queries.InsertEmployee(ctx, db.InsertEmployeeParams{
		ID:     int64(2),
		Name:   "Bob",
		DeptID: int64(1),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "insert emp 2:", err)
		os.Exit(1)
	}

	err = queries.InsertEmployee(ctx, db.InsertEmployeeParams{
		ID:     int64(3),
		Name:   "Charlie",
		DeptID: int64(2),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "insert emp 3:", err)
		os.Exit(1)
	}
	beforeDelete, err := queries.ListEmployees(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListEmployees before:", err)
		os.Exit(1)
	}
	err = queries.DeleteDepartment(ctx, int64(1))
	if err != nil {
		fmt.Fprintln(os.Stderr, "DeleteDepartment:", err)
		os.Exit(1)
	}
	afterDelete, err := queries.ListEmployees(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListEmployees after:", err)
		os.Exit(1)
	}
	countAfter, err := queries.CountEmployees(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "CountEmployees:", err)
		os.Exit(1)
	}

	result := map[string]any{
		"before_delete": beforeDelete,
		"after_delete":  afterDelete,
		"count_after":   countAfter,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

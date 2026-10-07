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
	connectionString := os.Getenv("DATABASE_URL")
	schemaName := os.Getenv("DATABASE_SCHEMA")

	conn, err := sql.Open("pgx", connectionString)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx := context.Background()

	_, err = conn.ExecContext(ctx, fmt.Sprintf("SET search_path TO %s", schemaName))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	_, err = conn.ExecContext(ctx, `INSERT INTO employees (name, department, level, active) VALUES
		('Alice', 'eng', 3, 1),
		('Bob', 'eng', 1, 1),
		('Charlie', 'sales', 2, 1),
		('Dave', 'eng', 2, 0),
		('Eve', 'sales', 3, 1)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)
	noFilter, err := queries.SearchEmployees(ctx, db.SearchEmployeesParams{})
	if err != nil {
		fmt.Fprintln(os.Stderr, "SearchEmployees (no filter):", err)
		os.Exit(1)
	}
	byDepartment, err := queries.SearchEmployees(ctx, db.SearchEmployeesParams{
		Department: new("eng"),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "SearchEmployees (by department):", err)
		os.Exit(1)
	}
	byMinLevel, err := queries.SearchEmployees(ctx, db.SearchEmployeesParams{
		MinLevel: new(int32(2)),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "SearchEmployees (by min level):", err)
		os.Exit(1)
	}
	byActive, err := queries.SearchEmployees(ctx, db.SearchEmployeesParams{
		Active: new(int32(1)),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "SearchEmployees (by active):", err)
		os.Exit(1)
	}
	byDeptAndLevel, err := queries.SearchEmployees(ctx, db.SearchEmployeesParams{
		Department: new("eng"),
		MinLevel:   new(int32(2)),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "SearchEmployees (by dept and level):", err)
		os.Exit(1)
	}
	allFilters, err := queries.SearchEmployees(ctx, db.SearchEmployeesParams{
		Department: new("eng"),
		MinLevel:   new(int32(2)),
		Active:     new(int32(1)),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "SearchEmployees (all filters):", err)
		os.Exit(1)
	}

	result := map[string]any{
		"no_filter":         noFilter,
		"by_department":     byDepartment,
		"by_min_level":      byMinLevel,
		"by_active":         byActive,
		"by_dept_and_level": byDeptAndLevel,
		"all_filters":       allFilters,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

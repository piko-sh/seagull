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
	"slices"
	"strings"

	_ "github.com/go-sql-driver/mysql"

	"case_runner/db"
)

var seed = []string{
	"INSERT INTO people VALUES (1, 'Ada', 'Lovelace', '1815-12-10', '2024-01-02 03:04:05', 3, 30.00, x'0102'), " +
		"(2, 'Grace', 'Hopper', 'not a date', '2024-02-03 00:00:00', 0, 0.00, NULL), " +
		"(3, 'Ada', 'Byron', '1816-01-16', '2024-03-04 00:00:00', 2, 9.99, NULL)",
	"INSERT INTO archived_people VALUES (10, 'Alan')",
}

type results map[string]any

func (r results) record(name string, read func() (any, error)) {
	value, err := read()
	if err != nil {
		r[name] = "error: " + err.Error()
		return
	}
	r[name] = value
}

func main() {
	conn, err := sql.Open("mysql", os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	ctx := context.Background()
	for _, statement := range seed {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	queries := db.New(conn)
	output := results{}
	output.record("labels", func() (any, error) { return queries.Labels(ctx, "x") })
	output.record("ratios", func() (any, error) { return queries.Ratios(ctx) })
	output.record("dates", func() (any, error) { return queries.Dates(ctx) })
	output.record("search_pipes", func() (any, error) { return queries.SearchPipes(ctx, false) })
	output.record("busy_names", func() (any, error) {
		return queries.BusyNames(ctx, db.BusyNamesParams{Total: 0, P2: 0})
	})
	output.record("newest_and_oldest", func() (any, error) { return queries.NewestAndOldest(ctx, 1) })
	output.record("with_defaults", func() (any, error) { return queries.WithDefaults(ctx, 1) })
	output.record("countdown", func() (any, error) { return queries.Countdown(ctx) })
	output.record("everyone", func() (any, error) { return queries.Everyone(ctx, 1) })
	output.record("from_the_view", func() (any, error) { return queries.FromTheView(ctx) })
	output.record("divide_by_zero", func() (any, error) { return queries.DivideByZero(ctx) })
	output.record("warnings", func() (any, error) { return queries.Warnings(ctx) })
	output.record("warning_count", func() (any, error) { return queries.WarningCount(ctx) })
	output.record("setting", func() (any, error) { return queries.Setting(ctx) })
	output.record("tables", func() (any, error) {
		tables, err := queries.Tables(ctx)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(tables))
		for _, table := range tables {
			names = append(names, table.TablesInDatabase+" "+table.TableType)
		}
		slices.Sort(names)
		return names, nil
	})
	output.record("definition", func() (any, error) {
		definition, err := queries.Definition(ctx)
		if err != nil {
			return nil, err
		}
		return []any{definition.Table, strings.HasPrefix(definition.CreateTable, "CREATE TABLE `archived_people`")}, nil
	})

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

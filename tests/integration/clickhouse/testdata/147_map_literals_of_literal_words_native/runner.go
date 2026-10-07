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
	"encoding/json"
	"fmt"
	"os"

	"github.com/ClickHouse/clickhouse-go/v2"

	"case_runner/db"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	options, err := clickhouse.ParseDSN(os.Getenv("DATABASE_URL"))
	if err != nil {
		return fmt.Errorf("parse dsn: %w", err)
	}
	conn, err := clickhouse.Open(options)
	if err != nil {
		return fmt.Errorf("open native connection: %w", err)
	}
	defer conn.Close()

	ctx := context.Background()
	queries := db.New(conn)

	if err := queries.RecordSwitches(ctx, 1); err != nil {
		return fmt.Errorf("record switches: %w", err)
	}
	switches, err := queries.Switches(ctx)
	if err != nil {
		return fmt.Errorf("switches: %w", err)
	}
	return json.NewEncoder(os.Stdout).Encode(describeSwitches(switches))
}

func describeSwitches(switches []db.SwitchesRow) []map[string]any {
	described := make([]map[string]any, 0, len(switches))
	for _, row := range switches {
		ratios := make(map[string]string, len(row.Ratios))
		for key, value := range row.Ratios {
			ratios[fmt.Sprint(key)] = "NULL"
			if value != nil {
				ratios[fmt.Sprint(key)] = fmt.Sprint(*value)
			}
		}
		nested := make(map[string]map[string]bool, len(row.Nested))
		for key, inner := range row.Nested {
			nested[fmt.Sprint(key)] = textKeys(inner)
		}
		described = append(described, map[string]any{"id": row.ID, "flags": textKeys(row.Flags), "ratios": ratios, "nested": nested})
	}
	return described
}

func textKeys(values map[bool]bool) map[string]bool {
	texts := make(map[string]bool, len(values))
	for key, value := range values {
		texts[fmt.Sprint(key)] = value
	}
	return texts
}

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

	_ "github.com/ClickHouse/clickhouse-go/v2"

	"case_runner/db"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	conn, err := sql.Open("clickhouse", os.Getenv("DATABASE_URL"))
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer conn.Close()

	ctx := context.Background()
	queries := db.New(conn)

	if err := queries.RecordGulls(ctx); err != nil {
		return fmt.Errorf("record gulls: %w", err)
	}
	built, err := queries.BuiltTuples(ctx)
	if err != nil {
		return fmt.Errorf("built tuples: %w", err)
	}
	widths := make([]int, 0, len(built))
	for _, row := range built {
		widths = append(widths, len(row.Called)+len(row.Parenthesised)+len(row.Single)+len(row.Partly))
	}
	nested, err := queries.NestedTuples(ctx, 1)
	if err != nil {
		return fmt.Errorf("nested tuples: %w", err)
	}
	labels := make([]any, 0, len(nested))
	for _, row := range nested {
		labels = append(labels, row.Listed[0][1], row.Keyed["herring"][1], row.Named["label"])
	}
	aliased, err := queries.AliasedArguments(ctx, 2)
	if err != nil {
		return fmt.Errorf("aliased arguments: %w", err)
	}

	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"built": built, "widths": widths, "nested": nested, "labels": labels,
		"aliased": aliased, "sum": aliased.Next + aliased.Doubled + uint64(aliased.Pair[0]),
	})
}

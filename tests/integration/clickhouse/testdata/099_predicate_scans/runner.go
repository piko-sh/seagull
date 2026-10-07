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

	span := int32(100)
	tagged := true
	gulls := []db.InsertGullParams{
		{ID: 1, Wing: 42, Span: &span, Ringed: true, Tagged: nil},
		{ID: 2, Wing: 38, Span: nil, Ringed: false, Tagged: &tagged},
	}
	for index, gull := range gulls {
		if err := queries.InsertGull(ctx, gull); err != nil {
			return fmt.Errorf("insert gull %d: %w", index, err)
		}
	}

	predicates, err := queries.GullPredicates(ctx)
	if err != nil {
		return fmt.Errorf("gull predicates: %w", err)
	}

	return json.NewEncoder(os.Stdout).Encode(map[string]any{"predicates": predicates})
}

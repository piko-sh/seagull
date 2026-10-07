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

	pages := map[string]db.ListFindsParams{
		"unset_takes_the_default": {PageSize: 0, PageOffset: 0},
		"over_the_max_is_clamped": {PageSize: 50, PageOffset: 0},
		"within_the_bounds":       {PageSize: 2, PageOffset: 1},
	}
	result := map[string][]uint64{}
	for name, page := range pages {
		rows, err := queries.ListFinds(ctx, page)
		if err != nil {
			return fmt.Errorf("ListFinds %s: %w", name, err)
		}
		identifiers := []uint64{}
		for _, row := range rows {
			identifiers = append(identifiers, row.ID)
		}
		result[name] = identifiers
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

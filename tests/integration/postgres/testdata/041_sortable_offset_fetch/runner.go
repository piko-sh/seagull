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
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	conn, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	ctx := context.Background()
	if _, err = conn.ExecContext(ctx, fmt.Sprintf("SET search_path TO %s", os.Getenv("DATABASE_SCHEMA"))); err != nil {
		return err
	}

	queries := db.New(conn)

	limited, err := queries.ListProductsPage(ctx, db.ListProductsPageParams{
		MinPrice:         60,
		PageSize:         2,
		PageOffset:       1,
		OrderBy:          db.ListProductsPageOrderByPrice,
		OrderByDirection: db.OrderDesc,
	})
	if err != nil {
		return fmt.Errorf("ListProductsPage: %w", err)
	}

	fetched, err := queries.FetchProductsPage(ctx, db.FetchProductsPageParams{
		PageOffset:       1,
		PageSize:         2,
		OrderBy:          db.FetchProductsPageOrderByName,
		OrderByDirection: db.OrderAsc,
	})
	if err != nil {
		return fmt.Errorf("FetchProductsPage: %w", err)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]any{
		"limit_offset_by_price_desc": limited,
		"offset_fetch_by_name":       fetched,
	})
}

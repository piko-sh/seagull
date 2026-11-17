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

	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("SET search_path TO %s", os.Getenv("DATABASE_SCHEMA"))); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO items (id, grp, score) VALUES
		(1, 1, 50), (2, 1, 90), (3, 2, 70), (4, 2, 80), (5, 3, 60), (6, 1, 85)`); err != nil {
		return err
	}

	queries := db.New(conn)
	result := map[string]any{}
	reads := map[string]func() (any, error){
		"top_all":            func() (any, error) { return queries.TopScores().All(ctx) },
		"top_limit_above":    func() (any, error) { return queries.TopScores().Limit(20).All(ctx) },
		"top_offset_past":    func() (any, error) { return queries.TopScores().Offset(3).All(ctx) },
		"top_count":          func() (any, error) { return queries.TopScores().Count(ctx) },
		"top_where":          func() (any, error) { return queries.TopScores().Where("grp", "=", 1).All(ctx) },
		"top_where_count":    func() (any, error) { return queries.TopScores().Where("grp", "=", 1).Count(ctx) },
		"top_ordered_page":   func() (any, error) { return queries.TopScores().OrderBy("id", "ASC").Limit(2).Offset(1).All(ctx) },
		"best_all":           func() (any, error) { return queries.BestPerGroup().All(ctx) },
		"best_where":         func() (any, error) { return queries.BestPerGroup().Where("score", "<", 85).All(ctx) },
		"best_where_count":   func() (any, error) { return queries.BestPerGroup().Where("score", "<", 85).Count(ctx) },
		"best_by_id_desc":    func() (any, error) { return queries.BestPerGroup().OrderBy("id", "DESC").All(ctx) },
	}
	for name, read := range reads {
		value, err := read()
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		result[name] = value
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

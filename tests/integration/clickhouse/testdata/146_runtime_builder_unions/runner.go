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
	"case_runner/db"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	connection, err := sql.Open(databaseDriver, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer connection.Close()
	connection.SetMaxOpenConns(1)
	ctx := context.Background()
	if _, err := connection.ExecContext(ctx, `INSERT INTO scores (id, score) VALUES (1, 30), (2, 10)`); err != nil {
		return err
	}
	if _, err := connection.ExecContext(ctx, `INSERT INTO guests (id) VALUES (3), (4)`); err != nil {
		return err
	}
	queries := db.New(connection)
	result := map[string]any{}
	reads := map[string]func() (any, error){
		"by_id": func() (any, error) {
			return queries.ScoresAndGuests().OrderBy("id", "ASC").All(ctx)
		},
		"first_by_id": func() (any, error) {
			return queries.ScoresAndGuests().OrderBy("id", "ASC").Limit(1).All(ctx)
		},
		"page_by_id_desc": func() (any, error) {
			return queries.ScoresAndGuests().OrderBy("id", "DESC").Limit(2).Offset(1).All(ctx)
		},
		"rows_under_limit_one": func() (any, error) {
			rows, err := queries.ScoresAndGuests().Limit(1).All(ctx)
			return len(rows), err
		},
		"count": func() (any, error) {
			return queries.ScoresAndGuests().Count(ctx)
		},
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

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
	if _, err := connection.ExecContext(ctx, `INSERT INTO items (id, score) VALUES (1, 20), (2, NULL), (3, 10), (4, NULL), (5, 10)`); err != nil {
		return err
	}
	queries := db.New(connection)
	result := map[string]any{}
	reads := map[string]func() (any, error){
		"asc_nulls_first": func() (any, error) {
			return queries.ListItems().OrderBy("score", "ASC NULLS FIRST").OrderBy("id", "ASC").All(ctx)
		},
		"asc_nulls_last": func() (any, error) {
			return queries.ListItems().OrderBy("score", "ASC NULLS LAST").OrderBy("id", "ASC").All(ctx)
		},
		"desc_nulls_first": func() (any, error) {
			return queries.ListItems().OrderBy("score", "DESC NULLS FIRST").OrderBy("id", "ASC").All(ctx)
		},
		"desc_nulls_last": func() (any, error) {
			return queries.ListItems().OrderBy("score", "DESC NULLS LAST").OrderBy("id", "ASC").All(ctx)
		},
		"distinct_asc_nulls_last": func() (any, error) {
			return queries.ListScores().OrderBy("score", "ASC NULLS LAST").All(ctx)
		},
		"distinct_desc_nulls_first": func() (any, error) {
			return queries.ListScores().OrderBy("score", "DESC NULLS FIRST").All(ctx)
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

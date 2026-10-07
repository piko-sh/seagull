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

	_ "github.com/duckdb/duckdb-go/v2"

	"case_runner/db"
)

func main() {
	conn, err := sql.Open("duckdb", "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx := context.Background()

	_, err = conn.ExecContext(ctx, `CREATE TABLE kv (
		key VARCHAR PRIMARY KEY,
		value VARCHAR NOT NULL,
		version INTEGER NOT NULL DEFAULT 1
	)`)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)
	if err := queries.Upsert(ctx, db.UpsertParams{Key: "foo", Value: "bar"}); err != nil {
		fmt.Fprintln(os.Stderr, "Upsert 1:", err)
		os.Exit(1)
	}

	afterInsert, err := queries.Get(ctx, "foo")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Get after insert:", err)
		os.Exit(1)
	}
	if err := queries.Upsert(ctx, db.UpsertParams{Key: "foo", Value: "baz"}); err != nil {
		fmt.Fprintln(os.Stderr, "Upsert 2:", err)
		os.Exit(1)
	}

	afterUpsert, err := queries.Get(ctx, "foo")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Get after upsert:", err)
		os.Exit(1)
	}

	result := map[string]any{
		"after_insert": afterInsert,
		"after_upsert": afterUpsert,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

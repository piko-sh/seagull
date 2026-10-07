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
	conn, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()
	ctx := context.Background()
	if _, err = conn.ExecContext(ctx, fmt.Sprintf("SET search_path TO %s", os.Getenv("DATABASE_SCHEMA"))); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err = conn.ExecContext(ctx, `INSERT INTO docs (id, payload, tags) VALUES (1, NULL, NULL), (2, '{"k":"v"}', ARRAY['a','b'])`); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	queries := db.New(conn)
	nullRow, err := queries.GetDoc(ctx, int64(1))
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetDoc(1):", err)
		os.Exit(1)
	}
	popRow, err := queries.GetDoc(ctx, int64(2))
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetDoc(2):", err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{"null_row": nullRow, "populated_row": popRow})
}

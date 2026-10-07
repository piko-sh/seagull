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

	"github.com/google/uuid"

	"case_runner/db"
)

const schema = `CREATE TABLE tokens (id BIGINT PRIMARY KEY, owner UUID, label VARCHAR NOT NULL);`

type results map[string]any

func (r results) record(name string, read func() (any, error)) {
	value, err := read()
	if err != nil {
		r[name] = "error: " + err.Error()
		return
	}
	r[name] = value
}

func main() {
	conn, err := sql.Open("duckdb", "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, schema); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	owner := uuid.MustParse("0b9f8e2c-6d1a-4f3e-9a7b-5c4d3e2f1a0b")
	queries := db.New(conn)
	output := results{}
	output.record("add_owned", func() (any, error) {
		return "added", queries.AddToken(ctx, db.AddTokenParams{ID: 1, Owner: &owner, Label: "owned"})
	})
	output.record("add_unowned", func() (any, error) {
		return "added", queries.AddToken(ctx, db.AddTokenParams{ID: 2, Owner: nil, Label: "unowned"})
	})
	output.record("search_any_owner", func() (any, error) { return queries.SearchTokens(ctx, db.SearchTokensParams{Owner: nil}) })
	output.record("search_owner", func() (any, error) { return queries.SearchTokens(ctx, db.SearchTokensParams{Owner: &owner}) })
	output.record("of_no_owner", func() (any, error) { return queries.TokensOf(nil).Count(ctx) })
	output.record("of_owner", func() (any, error) { return queries.TokensOf(&owner).All(ctx) })
	output.record("tokens", func() (any, error) { return queries.ListTokens(ctx) })

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

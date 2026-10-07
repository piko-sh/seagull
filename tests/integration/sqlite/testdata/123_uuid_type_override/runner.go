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

	"case_runner/db"
)

const schema = `CREATE TABLE accounts (id UUID NOT NULL PRIMARY KEY, owner_id uuid, name TEXT NOT NULL);`

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
	conn, err := sql.Open(databaseDriver, ":memory:")
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

	queries := db.New(conn)
	output := results{}
	identifier := "0b9f8e2c-6d1a-4f3e-9a7b-5c4d3e2f1a0b"
	owner := "7d444840-9dc0-11d1-b245-5ffdce74fad2"
	output.record("add_accounts", func() (any, error) {
		if err := queries.AddAccount(ctx, db.AddAccountParams{ID: identifier, OwnerID: &owner, Name: "primary"}); err != nil {
			return nil, err
		}
		return "added", queries.AddAccount(ctx, db.AddAccountParams{ID: owner, OwnerID: nil, Name: "owner"})
	})
	output.record("account_by_id", func() (any, error) { return queries.GetAccount(ctx, identifier) })
	output.record("owner_by_id", func() (any, error) { return queries.GetAccount(ctx, owner) })

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

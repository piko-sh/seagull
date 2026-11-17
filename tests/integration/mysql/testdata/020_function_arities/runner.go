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

	_ "github.com/go-sql-driver/mysql"

	"case_runner/db"
)

const seed = `INSERT INTO counters (name, value) VALUES ('invoice', 41)`

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
	conn, err := sql.Open("mysql", os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	ctx := context.Background()
	for _, statement := range []string{seed} {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	queries := db.New(conn)
	output := results{}
	sessions := []db.CreateSessionParams{
		{ID: "6ccd780c-baba-1026-9564-5b8c656024db", UserID: 1, Device: "phone", IP: "192.0.2.1"},
		{ID: "6ccd780c-baba-1026-9564-5b8c656024dc", UserID: 1, Device: "laptop", IP: "2001:db8::1"},
		{ID: "6ccd780c-baba-1026-9564-5b8c656024dd", UserID: 2, Device: "phone", IP: "not an address"},
	}
	for index, session := range sessions {
		output.record(fmt.Sprintf("create_session_%d", index), func() (any, error) { return "ok", queries.CreateSession(ctx, session) })
	}
	output.record("list_sessions", func() (any, error) { return queries.ListSessions(ctx) })
	output.record("distinct_pairs", func() (any, error) { return queries.DistinctPairs(ctx) })
	output.record("advance", func() (any, error) { return "ok", queries.Advance(ctx, "invoice") })
	output.record("current", func() (any, error) { return queries.Current(ctx) })

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

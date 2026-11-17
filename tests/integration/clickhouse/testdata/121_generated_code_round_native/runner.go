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
	"case_runner/kinds"
)

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
	options, err := clickhouse.ParseDSN(os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	conn, err := clickhouse.Open(options)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx := context.Background()
	for _, statement := range []string{`INSERT INTO shows_native (id, title, seasons) VALUES (1, 'Marple', 6), (2, 'Poirot', 13)`} {
		if err := conn.Exec(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	queries := db.New(conn)
	output := results{}
	output.record("insert_document", func() (any, error) {
		return "inserted", queries.InsertDocument(ctx, db.InsertDocumentParams{ID: 1, Data: kinds.Show{Title: "Marple", Seasons: 6}})
	})
	output.record("document", func() (any, error) { return queries.GetDocument(ctx, 1) })
	output.record("shows_before_migration", func() (any, error) { return queries.ListShows(ctx) })
	output.record("titles_before_migration", func() (any, error) { return queries.ListTitles(ctx) })
	for _, statement := range []string{`ALTER TABLE shows_native ADD COLUMN rating Int32 DEFAULT 0`} {
		if err := conn.Exec(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	output.record("shows_after_migration", func() (any, error) { return queries.ListShows(ctx) })
	output.record("titles_after_migration", func() (any, error) { return queries.ListTitles(ctx) })

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

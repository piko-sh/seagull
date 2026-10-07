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

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"case_runner/db"
)

const schema = `CREATE TABLE things (
  id INTEGER PRIMARY KEY,
  external_id UUID NOT NULL,
  other_id UUID,
  starts_at TIME,
  zoned TIMESTAMPTZ,
  span INTERVAL,
  raw BYTEA,
  price MONEY NOT NULL,
  label TEXT
);`

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
	external := uuid.MustParse("0b9f8e2c-6d1a-4f3e-9a7b-5c4d3e2f1a0b")
	other := uuid.MustParse("7d444840-9dc0-11d1-b245-5ffdce74fad2")
	startsAt := "09:30:00"
	zoned := "2024-03-01 12:00:00+01:00"
	span := "1 day 02:00:00"
	price := apd.New(1250, -2)
	label := "42 labels"
	output.record("add_thing", func() (any, error) {
		return "added", queries.AddThing(ctx, db.AddThingParams{
			ID: 1, ExternalID: external, OtherID: &other, StartsAt: &startsAt, Zoned: &zoned, Span: &span,
			Raw: []byte{0x00, 0xff, 0x10}, Price: *price, Label: &label,
		})
	})
	output.record("thing_by_uuid", func() (any, error) {
		thing, err := queries.GetThing(ctx, external)
		return &thing, err
	})
	output.record("thing_text", func() (any, error) { return queries.ThingText(ctx, 1) })
	output.record("stored_classes", func() (any, error) {
		row := conn.QueryRowContext(ctx, "SELECT typeof(external_id) || ',' || typeof(starts_at) || ',' || typeof(zoned) || ','"+
			" || typeof(span) || ',' || typeof(raw) || ',' || typeof(price) FROM things WHERE id = 1")
		var classes string
		return classes, row.Scan(&classes)
	})

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

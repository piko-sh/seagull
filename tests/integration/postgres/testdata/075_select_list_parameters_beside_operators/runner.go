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
	"reflect"

	"github.com/cockroachdb/apd/v3"

	"case_runner/db"
)

const seed = `INSERT INTO items (id, n, price, mask) VALUES (1, 10, 2.50, 3), (2, 20, 4.00, 6)`

type results map[string]any

func (r results) record(name string, read func() (any, error)) {
	value, err := read()
	if err != nil {
		r[name] = "error: " + err.Error()
		return
	}
	r[name] = value
}

func parameterType(method any) (any, error) {
	return reflect.TypeOf(method).In(1).String(), nil
}

func main() {
	conn, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	ctx := context.Background()
	for _, statement := range []string{"SET search_path TO " + os.Getenv("DATABASE_SCHEMA"), seed} {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	queries := db.New(conn)
	output := results{}
	output.record("bumped", func() (any, error) { return queries.Bumped(ctx, 5) })
	output.record("bumped_parameter", func() (any, error) { return parameterType(queries.Bumped) })
	output.record("scaled", func() (any, error) { return queries.Scaled(ctx, *apd.New(15, -1)) })
	output.record("scaled_parameter", func() (any, error) { return parameterType(queries.Scaled) })
	output.record("masked", func() (any, error) { return queries.MaskedValues(ctx, 2) })
	output.record("masked_parameter", func() (any, error) { return parameterType(queries.MaskedValues) })
	output.record("shrink", func() (any, error) { return queries.Shrink(ctx, db.ShrinkParams{ID: 2, N: 1}) })

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

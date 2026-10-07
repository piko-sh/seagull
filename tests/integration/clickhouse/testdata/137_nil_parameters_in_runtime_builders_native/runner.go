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
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"case_runner/db"
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
	owner := uuid.MustParse("0b9f8e2c-6d1a-4f3e-9a7b-5c4d3e2f1a0b")
	amount := decimal.RequireFromString("2.5")
	note := "kept"
	queries := db.New(conn)
	output := results{}
	output.record("add_token", func() (any, error) {
		return "added", queries.AddToken(ctx, db.AddTokenParams{ID: 1, Owner: &owner, Amount: &amount, Note: &note})
	})
	output.record("add_tokens", func() (any, error) {
		return "added", queries.AddTokens(ctx, []db.AddTokensParams{{ID: 2, Owner: nil, Amount: nil, Note: nil}})
	})
	output.record("of_no_owner", func() (any, error) { return queries.TokensOf(nil).Count(ctx) })
	output.record("of_owner", func() (any, error) { return queries.TokensOf(&owner).All(ctx) })
	output.record("worth_no_amount", func() (any, error) { return queries.TokensWorth(nil).Count(ctx) })
	output.record("worth_amount", func() (any, error) { return queries.TokensWorth(&amount).All(ctx) })
	output.record("noted_no_note", func() (any, error) { return queries.TokensNoted(nil).Count(ctx) })
	output.record("noted_note", func() (any, error) { return queries.TokensNoted(&note).All(ctx) })
	output.record("tokens", func() (any, error) { return queries.ListTokens(ctx) })

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

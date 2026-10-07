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
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"case_runner/db"
)

const followerReadLag = 6 * time.Second

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
	conn, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("SET search_path TO %s", os.Getenv("DATABASE_SCHEMA"))); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)
	balance := int64(1250)
	output := results{}
	output.record("add", func() (any, error) {
		for id, label := range map[int64]string{1: "ada", 2: "grace"} {
			if err := queries.AddOwner(ctx, db.AddOwnerParams{ID: id, Label: label}); err != nil {
				return nil, err
			}
		}
		for _, account := range []db.AddAccountParams{
			{ID: 10, Name: "current", Balance: &balance, OwnerID: 1},
			{ID: 11, Name: "savings", Balance: nil, OwnerID: 2},
			{ID: 12, Name: "loan", Balance: &balance, OwnerID: 1},
		} {
			if err := queries.AddAccount(ctx, account); err != nil {
				return nil, err
			}
		}
		return "added", nil
	})
	time.Sleep(followerReadLag)
	output.record("historical_balance", func() (any, error) { return queries.HistoricalBalance(ctx, 10) })
	output.record("follower_owners", func() (any, error) {
		return queries.FollowerOwners(ctx, db.FollowerOwnersParams{Limit: 2, Offset: 2})
	})
	output.record("renamed", func() (any, error) { return queries.Renamed(ctx, 11) })
	output.record("with_none", func() (any, error) { return queries.WithNone(ctx) })

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

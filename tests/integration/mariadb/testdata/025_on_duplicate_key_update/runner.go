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

func main() {
	connectionString := os.Getenv("DATABASE_URL")

	conn, err := sql.Open("mysql", connectionString)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx := context.Background()

	queries := db.New(conn)
	err = queries.UpsertEntry(ctx, db.UpsertEntryParams{
		LookupKey: "colour",
		Value:     "red",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "UpsertEntry 1:", err)
		os.Exit(1)
	}

	err = queries.UpsertEntry(ctx, db.UpsertEntryParams{
		LookupKey: "language",
		Value:     "Go",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "UpsertEntry 2:", err)
		os.Exit(1)
	}

	afterInsert, err := queries.ListEntries(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListEntries after insert:", err)
		os.Exit(1)
	}
	err = queries.UpsertEntry(ctx, db.UpsertEntryParams{
		LookupKey: "colour",
		Value:     "blue",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "UpsertEntry update:", err)
		os.Exit(1)
	}

	afterUpsert, err := queries.GetEntry(ctx, "colour")
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetEntry after upsert:", err)
		os.Exit(1)
	}
	err = queries.UpsertEntry(ctx, db.UpsertEntryParams{
		LookupKey: "colour",
		Value:     "green",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "UpsertEntry update 2:", err)
		os.Exit(1)
	}

	afterSecondUpsert, err := queries.GetEntry(ctx, "colour")
	if err != nil {
		fmt.Fprintln(os.Stderr, "GetEntry after second upsert:", err)
		os.Exit(1)
	}

	allEntries, err := queries.ListEntries(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ListEntries final:", err)
		os.Exit(1)
	}

	result := map[string]any{
		"after_insert":        afterInsert,
		"after_upsert":        afterUpsert,
		"after_second_upsert": afterSecondUpsert,
		"all_entries":         allEntries,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

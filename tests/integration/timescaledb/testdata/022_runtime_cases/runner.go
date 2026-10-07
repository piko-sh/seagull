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
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	conn, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	if _, err = conn.ExecContext(ctx, "SET search_path TO "+os.Getenv("DATABASE_SCHEMA")); err != nil {
		return err
	}
	queries := db.New(conn)

	err = queries.UpsertItems(ctx, []db.UpsertItemsParams{{ID: 1, Kind: "Alpha", Rank: 1}, {ID: 2, Kind: "Beta", Rank: 2}, {ID: 3, Kind: "Gamma", Rank: 3}})
	if err != nil {
		return fmt.Errorf("UpsertItems: %w", err)
	}
	err = queries.UpsertItems(ctx, []db.UpsertItemsParams{{ID: 2, Kind: "BETA TWO", Rank: 10}})
	if err != nil {
		return fmt.Errorf("UpsertItems again: %w", err)
	}
	calm, stormy := db.MoodCalm, db.MoodStormy
	for _, change := range []db.SetMoodParams{{ID: 1, Mood: &calm}, {ID: 2, Mood: &stormy}, {ID: 3, Mood: nil}} {
		if _, err := queries.SetMood(ctx, change); err != nil {
			return fmt.Errorf("SetMood %d: %w", change.ID, err)
		}
	}

	output := map[string]any{}
	for _, id := range []int64{1, 2, 3} {
		item, err := queries.GetItem(ctx, id)
		if err != nil {
			return fmt.Errorf("GetItem %d: %w", id, err)
		}
		output[fmt.Sprintf("item_%d", id)] = item
	}
	type lookup struct {
		run  func() (any, error)
		name string
	}
	lookups := []lookup{
		{name: "in_nil", run: func() (any, error) { return queries.ItemsIn(ctx, db.ItemsInParams{IDs: nil}) }},
		{name: "in_empty", run: func() (any, error) { return queries.ItemsIn(ctx, db.ItemsInParams{IDs: []int64{}}) }},
		{name: "in_one", run: func() (any, error) { return queries.ItemsIn(ctx, db.ItemsInParams{IDs: []int64{2}}) }},
		{name: "in_many", run: func() (any, error) { return queries.ItemsIn(ctx, db.ItemsInParams{IDs: []int64{1, 3, 4}}) }},
		{name: "not_in_nil", run: func() (any, error) { return queries.ItemsNotIn(ctx, db.ItemsNotInParams{IDs: nil}) }},
		{name: "not_in_empty", run: func() (any, error) { return queries.ItemsNotIn(ctx, db.ItemsNotInParams{IDs: []int64{}}) }},
		{name: "not_in_one", run: func() (any, error) { return queries.ItemsNotIn(ctx, db.ItemsNotInParams{IDs: []int64{2}}) }},
		{name: "not_in_many", run: func() (any, error) { return queries.ItemsNotIn(ctx, db.ItemsNotInParams{IDs: []int64{1, 3}}) }},
		{name: "moods_in", run: func() (any, error) {
			return queries.ItemsWithMoods(ctx, db.ItemsWithMoodsParams{Moods: []db.Mood{db.MoodCalm, db.MoodFair}})
		}},
		{name: "moods_in_nil", run: func() (any, error) { return queries.ItemsWithMoods(ctx, db.ItemsWithMoodsParams{Moods: nil}) }},
		{name: "moods_not_in", run: func() (any, error) {
			return queries.ItemsWithoutMoods(ctx, db.ItemsWithoutMoodsParams{Moods: []db.Mood{db.MoodCalm}})
		}},
		{name: "moods_not_in_nil", run: func() (any, error) { return queries.ItemsWithoutMoods(ctx, db.ItemsWithoutMoodsParams{Moods: nil}) }},
	}
	for _, current := range lookups {
		rows, err := current.run()
		if err != nil {
			return fmt.Errorf("%s: %w", current.name, err)
		}
		output[current.name] = rows
	}

	err = queries.CopyEntries(ctx, []db.CopyEntriesParams{{Actor: "ann", Action: "create", ID: 1}, {Actor: "bob", Action: "update", ID: 2}})
	if err != nil {
		return fmt.Errorf("CopyEntries: %w", err)
	}
	entries, err := queries.ListEntries(ctx)
	if err != nil {
		return fmt.Errorf("ListEntries: %w", err)
	}
	output["entries"] = entries

	encoded, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}

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

	_ "github.com/ClickHouse/clickhouse-go/v2"

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
	conn, err := sql.Open("clickhouse", os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer conn.Close()
	queries := db.New(conn)

	calm, stormy := db.ItemsMoodCalm, db.ItemsMoodStormy
	first := db.InsertItemParams{
		ID: 1, Mood: db.ItemsMoodCalm, Maybe: &stormy, Moods: []db.ItemsMood{db.ItemsMoodFair, db.ItemsMoodCalm},
		Maybes: []*db.ItemsMood{nil, &calm}, Labels: map[string]db.ItemsMood{"morning": db.ItemsMoodCalm, "night": db.ItemsMoodStormy},
	}
	if err := queries.InsertItem(ctx, first); err != nil {
		return fmt.Errorf("InsertItem 1: %w", err)
	}
	if err := queries.InsertItem(ctx, db.InsertItemParams{ID: 2, Mood: db.ItemsMoodStormy, Maybe: nil, Moods: nil, Maybes: nil, Labels: nil}); err != nil {
		return fmt.Errorf("InsertItem 2: %w", err)
	}
	batch := []db.InsertItemsParams{
		{ID: 3, Mood: db.ItemsMoodFair, Maybe: &calm, Moods: []db.ItemsMood{db.ItemsMoodStormy}, Maybes: []*db.ItemsMood{&stormy}, Labels: map[string]db.ItemsMood{"noon": db.ItemsMoodFair}},
		{ID: 4, Mood: db.ItemsMoodCalm, Maybe: nil, Moods: []db.ItemsMood{}, Maybes: []*db.ItemsMood{}, Labels: map[string]db.ItemsMood{}},
	}
	if err := queries.InsertItems(ctx, batch); err != nil {
		return fmt.Errorf("InsertItems: %w", err)
	}

	output := map[string]any{}
	for _, id := range []uint64{1, 2, 3, 4} {
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
		{name: "with_mood", run: func() (any, error) { return queries.ItemsWithMood(ctx, db.ItemsMoodCalm) }},
		{name: "moods_in_nil", run: func() (any, error) { return queries.ItemsWithMoods(ctx, db.ItemsWithMoodsParams{Moods: nil}) }},
		{name: "moods_in_empty", run: func() (any, error) {
			return queries.ItemsWithMoods(ctx, db.ItemsWithMoodsParams{Moods: []db.ItemsMood{}})
		}},
		{name: "moods_in_one", run: func() (any, error) {
			return queries.ItemsWithMoods(ctx, db.ItemsWithMoodsParams{Moods: []db.ItemsMood{db.ItemsMoodFair}})
		}},
		{name: "moods_in_many", run: func() (any, error) {
			return queries.ItemsWithMoods(ctx, db.ItemsWithMoodsParams{Moods: []db.ItemsMood{db.ItemsMoodCalm, db.ItemsMoodFair}})
		}},
		{name: "moods_not_in_nil", run: func() (any, error) {
			return queries.ItemsWithoutMoods(ctx, db.ItemsWithoutMoodsParams{Moods: nil})
		}},
		{name: "moods_not_in_many", run: func() (any, error) {
			return queries.ItemsWithoutMoods(ctx, db.ItemsWithoutMoodsParams{Moods: []db.ItemsMood{db.ItemsMoodCalm, db.ItemsMoodFair}})
		}},
		{name: "maybe_calm", run: func() (any, error) { return queries.ItemsMaybe(ctx, &calm) }},
		{name: "maybe_nil", run: func() (any, error) { return queries.ItemsMaybe(ctx, nil) }},
	}
	for _, current := range lookups {
		rows, err := current.run()
		if err != nil {
			return fmt.Errorf("%s: %w", current.name, err)
		}
		output[current.name] = rows
	}

	encoded, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}

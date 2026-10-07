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
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"

	"case_runner/db"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	options, err := clickhouse.ParseDSN(os.Getenv("DATABASE_URL"))
	if err != nil {
		return fmt.Errorf("parse dsn: %w", err)
	}
	conn, err := clickhouse.Open(options)
	if err != nil {
		return fmt.Errorf("open native connection: %w", err)
	}
	defer conn.Close()

	ctx := context.Background()
	queries := db.New(conn)

	three := uint16(3)
	calm := "calm"
	sightings := []db.InsertSightingParams{
		{
			ID: 1, Colonies: []string{"cod", "ling"}, Minutes: []*uint16{&three, nil}, Tags: []string{"red", "blue"},
			Started: time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC), Departed: time.Date(2024, 1, 31, 10, 0, 0, 0, time.UTC), Note: &calm,
		},
		{
			ID: 2, Colonies: []string{}, Minutes: []*uint16{}, Tags: []string{},
			Started: time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC), Departed: time.Date(2024, 2, 29, 23, 59, 59, 0, time.UTC), Note: nil,
		},
	}
	for index, sighting := range sightings {
		if err := queries.InsertSighting(ctx, sighting); err != nil {
			return fmt.Errorf("insert sighting %d: %w", index, err)
		}
	}

	legs, err := queries.ListLegs(ctx)
	if err != nil {
		return fmt.Errorf("list legs: %w", err)
	}
	legTuples, err := queries.ListLegTuples(ctx)
	if err != nil {
		return fmt.Errorf("list leg tuples: %w", err)
	}
	tagsOrEmpty, err := queries.ListTagsOrEmpty(ctx)
	if err != nil {
		return fmt.Errorf("list tags or empty: %w", err)
	}
	allTags, err := queries.ListAllTags(ctx)
	if err != nil {
		return fmt.Errorf("list all tags: %w", err)
	}
	unfoldedTags := make([]map[string]any, 0, len(allTags))
	for _, row := range allTags {
		unfoldedTags = append(unfoldedTags, map[string]any{
			"id": row.ID, "tags": row.Tags, "legs.colony": row.LegsColony, "legs.minutes": row.LegsMinutes,
			"started": row.Started.UTC().Format(time.RFC3339Nano), "note": row.Note,
		})
	}
	moved := make([]map[string]any, 0, len(sightings))
	for _, sighting := range sightings {
		row, err := queries.MovedDates(ctx, sighting.ID)
		if err != nil {
			return fmt.Errorf("moved dates %d: %w", sighting.ID, err)
		}
		moved = append(moved, map[string]any{
			"next_day":  row.NextDay.UTC().Format(time.RFC3339Nano),
			"next_hour": row.NextHour.UTC().Format(time.RFC3339Nano),
			"nudged":    row.Nudged.UTC().Format(time.RFC3339Nano),
			"half_past": row.HalfPast.UTC().Format(time.RFC3339Nano),
		})
	}

	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"legs":          legs,
		"leg_tuples":    legTuples,
		"tags_or_empty": tagsOrEmpty,
		"all_tags":      unfoldedTags,
		"moved":         moved,
	})
}

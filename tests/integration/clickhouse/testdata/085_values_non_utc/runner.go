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
	"math/big"
	"net"
	"os"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"

	"case_runner/db"
)

var (
	kolkata = time.FixedZone("Asia/Kolkata", 5*3600+1800)

	firstSensor  = uuid.MustParse("6f1c2a52-1d40-4b8e-9b54-6ad0d1c3a001")
	secondSensor = uuid.MustParse("6f1c2a52-1d40-4b8e-9b54-6ad0d1c3a002")
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	conn, err := sql.Open("clickhouse", os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer conn.Close()
	ctx := clickhouse.Context(context.Background(), clickhouse.WithSettings(clickhouse.Settings{"session_timezone": "Asia/Kolkata"}))
	queries := db.New(conn)

	huge, _ := new(big.Int).SetString("-170141183460469231731687303715884105728", 10)
	first := db.InsertReadingParams{
		ID:        1,
		Day:       time.Date(2024, 3, 5, 0, 30, 0, 0, kolkata),
		Day32:     time.Date(1950, 7, 9, 1, 0, 0, 0, kolkata),
		TakenAt:   time.Date(2024, 3, 5, 0, 30, 15, 0, kolkata),
		TakenAtMs: time.Date(2024, 3, 5, 0, 30, 15, 250_000_000, kolkata),
		Sensors:   []uuid.UUID{firstSensor, secondSensor},
		Gateways:  []net.IP{net.ParseIP("10.0.0.1").To4(), net.ParseIP("192.168.1.20").To4()},
		Frames:    [][]uint8{{1, 2}, {3}},
		Totals:    []*big.Int{big.NewInt(-3), huge},
		Note:      "a\\b\tc\nd\re\x00f'g",
		Position:  map[string]any{"y": -2.25, "x": 1.5},
		Tags:      map[string]uint32{"tern": 2, "auk": 1, "gannet": 3},
	}
	second := db.InsertReadingParams{
		ID:        2,
		Day:       time.Date(2024, 3, 6, 0, 0, 0, 0, time.UTC),
		Day32:     time.Date(2024, 3, 6, 0, 0, 0, 0, time.UTC),
		TakenAt:   time.Date(2024, 3, 6, 12, 0, 0, 0, time.UTC),
		TakenAtMs: time.Date(2024, 3, 6, 12, 0, 0, 0, time.UTC),
		Sensors:   []uuid.UUID{},
		Gateways:  nil,
		Frames:    [][]uint8{},
		Totals:    nil,
		Note:      "plain",
		Position:  map[string]any{"x": 0.0, "y": 0.0},
		Tags:      map[string]uint32{},
	}
	for _, reading := range []db.InsertReadingParams{first, second} {
		if err := queries.InsertReading(ctx, reading); err != nil {
			return fmt.Errorf("InsertReading %d: %w", reading.ID, err)
		}
	}

	output := map[string]any{}
	for _, id := range []uint64{1, 2} {
		row, err := queries.GetReading(ctx, id)
		if err != nil {
			return fmt.Errorf("GetReading %d: %w", id, err)
		}
		output[fmt.Sprintf("reading_%d", id)] = row
	}

	type lookup struct {
		run  func() (any, error)
		name string
	}
	lookups := []lookup{
		{name: "taken_between", run: func() (any, error) {
			return queries.ReadingsTakenBetween(ctx, db.ReadingsTakenBetweenParams{From: first.TakenAt.Add(-time.Second), To: first.TakenAt.Add(time.Second)})
		}},
		{name: "taken_at_milli", run: func() (any, error) { return queries.ReadingsTakenAtMilli(ctx, first.TakenAtMs.In(time.UTC)) }},
		{name: "on_day", run: func() (any, error) {
			return queries.ReadingsOnDay(ctx, db.ReadingsOnDayParams{Day: first.Day, Day32: first.Day32})
		}},
		{name: "with_sensor", run: func() (any, error) { return queries.ReadingsWithSensor(ctx, secondSensor) }},
		{name: "with_gateways", run: func() (any, error) { return queries.ReadingsWithGateways(ctx, first.Gateways) }},
		{name: "with_frames", run: func() (any, error) {
			return queries.ReadingsWithFrames(ctx, db.ReadingsWithFramesParams{Frames: first.Frames, Totals: first.Totals})
		}},
		{name: "with_tags", run: func() (any, error) {
			return queries.ReadingsWithTags(ctx, map[string]uint32{"gannet": 3, "tern": 2, "auk": 1})
		}},
		{name: "with_note", run: func() (any, error) { return queries.ReadingsWithNote(ctx, first.Note) }},
		{name: "in_nil", run: func() (any, error) { return queries.ReadingsIn(ctx, db.ReadingsInParams{IDs: nil}) }},
		{name: "in_empty", run: func() (any, error) { return queries.ReadingsIn(ctx, db.ReadingsInParams{IDs: []uint64{}}) }},
		{name: "in_one", run: func() (any, error) { return queries.ReadingsIn(ctx, db.ReadingsInParams{IDs: []uint64{2}}) }},
		{name: "in_many", run: func() (any, error) { return queries.ReadingsIn(ctx, db.ReadingsInParams{IDs: []uint64{1, 2, 3}}) }},
		{name: "not_in_nil", run: func() (any, error) { return queries.ReadingsNotIn(ctx, db.ReadingsNotInParams{IDs: nil}) }},
		{name: "not_in_empty", run: func() (any, error) { return queries.ReadingsNotIn(ctx, db.ReadingsNotInParams{IDs: []uint64{}}) }},
		{name: "not_in_one", run: func() (any, error) { return queries.ReadingsNotIn(ctx, db.ReadingsNotInParams{IDs: []uint64{2}}) }},
		{name: "not_in_many", run: func() (any, error) { return queries.ReadingsNotIn(ctx, db.ReadingsNotInParams{IDs: []uint64{1, 2}}) }},
	}
	for _, current := range lookups {
		rows, err := current.run()
		if err != nil {
			return fmt.Errorf("%s: %w", current.name, err)
		}
		output[current.name] = rows
	}

	if err := queries.InsertNotes(ctx, nil); err != nil {
		return fmt.Errorf("InsertNotes without rows: %w", err)
	}
	if err := queries.InsertNotes(ctx, []db.InsertNotesParams{{ID: 1, Body: "first"}, {ID: 2, Body: "it's"}, {ID: 3, Body: `back\slash`}}); err != nil {
		return fmt.Errorf("InsertNotes: %w", err)
	}
	notes, err := queries.ListNotes(ctx)
	if err != nil {
		return fmt.Errorf("ListNotes: %w", err)
	}
	output["notes"] = notes

	encoded, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(encoded))
	return nil
}

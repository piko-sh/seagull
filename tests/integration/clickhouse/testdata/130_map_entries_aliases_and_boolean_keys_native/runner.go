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

	if err := queries.RecordReadings(ctx); err != nil {
		return fmt.Errorf("record readings: %w", err)
	}
	entries, err := queries.Entries(ctx)
	if err != nil {
		return fmt.Errorf("entries: %w", err)
	}
	inPlace, err := queries.EntriesInPlace(ctx)
	if err != nil {
		return fmt.Errorf("entries in place: %w", err)
	}
	shadowing, err := queries.ShadowingAliases(ctx)
	if err != nil {
		return fmt.Errorf("shadowing aliases: %w", err)
	}
	flags, err := queries.Flags(ctx)
	if err != nil {
		return fmt.Errorf("flags: %w", err)
	}
	sums := make([]map[string]any, 0, len(shadowing))
	for _, row := range shadowing {
		sums = append(sums, map[string]any{"sum": row.Name + row.Again, "original": row.Original + "!"})
	}
	flagTexts := make([]map[string]uint8, 0, len(flags))
	for _, row := range flags {
		texts := make(map[string]uint8, len(row.Flags))
		for key, value := range row.Flags {
			texts[fmt.Sprint(key)] = value
		}
		flagTexts = append(flagTexts, texts)
	}
	checks := make([]map[string]any, 0, len(inPlace))
	for _, row := range inPlace {
		checks = append(checks, map[string]any{"label": row.Label + "?", "doubled": row.Amount * 2, "key": row.Attributes.Keys + "?"})
	}

	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"entries": entries, "in_place": inPlace, "checks": checks, "shadowing": shadowing, "sums": sums, "flags": flagTexts,
	})
}

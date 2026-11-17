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
	"case_runner/db"
	"context"
	"encoding/json"
	"fmt"
	"github.com/ClickHouse/clickhouse-go/v2"
	"os"
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
		return err
	}
	connection, err := clickhouse.Open(options)
	if err != nil {
		return err
	}
	defer connection.Close()
	ctx := context.Background()
	queries := db.New(connection)
	output := map[string]any{}
	mixedColumns, err := queries.MixedColumns(ctx)
	if err != nil {
		return fmt.Errorf("MixedColumns: %w", err)
	}
	output["MixedColumns"] = mixedColumns
	extremes, err := queries.Extremes(ctx)
	if err != nil {
		return fmt.Errorf("Extremes: %w", err)
	}
	output["Extremes"] = extremes
	conversions, err := queries.Conversions(ctx)
	if err != nil {
		return fmt.Errorf("Conversions: %w", err)
	}
	output["Conversions"] = conversions
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

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
	conn, err := sql.Open("clickhouse", os.Getenv("DATABASE_URL"))
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer conn.Close()

	ctx := context.Background()
	queries := db.New(conn)

	if err := queries.RecordStations(ctx); err != nil {
		return fmt.Errorf("record stations: %w", err)
	}
	stations, err := queries.ListStations(ctx)
	if err != nil {
		return fmt.Errorf("list stations: %w", err)
	}
	station, err := queries.GetStation(ctx, 1)
	if err != nil {
		return fmt.Errorf("get station: %w", err)
	}
	streamed := make([]any, 0, len(stations))
	for row, err := range queries.StreamStations(ctx) {
		if err != nil {
			return fmt.Errorf("stream stations: %w", err)
		}
		streamed = append(streamed, map[string]any{"id": row.ID, "ip": fmt.Sprint(row.Addressed["ip"]), "huge": fmt.Sprint(row.Addressed["huge"])})
	}
	first := stations[0]
	typed := map[string]any{
		"listed":  fmt.Sprint(first.Listed[0]["ip"], " ", first.Listed[0]["big"]),
		"nested":  first.NestedAddresses[0][0].String(),
		"address": first.Address.String(),
		"wide":    first.Wide.String(),
	}

	return json.NewEncoder(os.Stdout).Encode(map[string]any{"stations": stations, "station": station, "streamed": streamed, "typed": typed})
}

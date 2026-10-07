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
	"fmt"
	"io"
	"time"

	"seagull.sh/seagull/examples/clickhouse/005_type_mapping/internal/store"
)

// run records a landing of fish and reads it back, printing the Go type sgl chose for
// each column. ClickHouse types are exact about width and sign, and Seagull follows them:
// UInt16, UInt32 and UInt64 give uint16, uint32 and uint64, Int64 gives int64, Float32
// gives float32, and Bool gives bool. DateTime('UTC') gives time.Time; the generated code
// sends a time as Unix seconds, so the server keeps its moment whatever the zone, and the
// column's time zone makes it read back in UTC. FixedString(4) and String both give
// string, which in ClickHouse holds any bytes, and Nullable(String) gives *string.
//
// UUID gives uuid.UUID from github.com/google/uuid and Decimal(9, 2) gives
// decimal.Decimal from github.com/shopspring/decimal, the types clickhouse-go scans them
// into. A {price:Decimal(9, 2)} placeholder would make the parameter a decimal.Decimal
// too; this example passes the price and catch id as text and converts them in SQL with
// toDecimal32 and toUUID, so it needs nothing beyond the standard library.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives one line per column.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	if err := queries.RecordLanding(ctx, store.RecordLandingParams{
		ID:       1,
		CatchID:  "5f0c6c7e-8d0e-4b77-9a51-2c4a6f9e3b10",
		Species:  "mackerel",
		Boxes:    12,
		Fish:     480,
		WeightG:  163_200,
		LengthCm: 31.5,
		Price:    "412.50",
		OnIce:    true,
		LandedAt: time.Date(2026, time.May, 1, 5, 30, 0, 0, time.UTC),
		Tag:      string([]byte{0x04, 0xa2, 0x19, 0xf3}),
		Notes:    new("landed at Newlyn"),
	}); err != nil {
		return fmt.Errorf("recording a landing: %w", err)
	}

	landing, err := queries.GetLanding(ctx, 1)
	if err != nil {
		return fmt.Errorf("getting landing 1: %w", err)
	}
	fmt.Fprintf(output, "id         %-16T %d\n", landing.ID, landing.ID)
	fmt.Fprintf(output, "catch_id   %-16T %s\n", landing.CatchID, landing.CatchID)
	fmt.Fprintf(output, "species    %-16T %s\n", landing.Species, landing.Species)
	fmt.Fprintf(output, "boxes      %-16T %d\n", landing.Boxes, landing.Boxes)
	fmt.Fprintf(output, "fish       %-16T %d\n", landing.Fish, landing.Fish)
	fmt.Fprintf(output, "weight_g   %-16T %d\n", landing.WeightG, landing.WeightG)
	fmt.Fprintf(output, "length_cm  %-16T %.1f\n", landing.LengthCm, landing.LengthCm)
	fmt.Fprintf(output, "price      %-16T %s\n", landing.Price, landing.Price.StringFixed(2))
	fmt.Fprintf(output, "on_ice     %-16T %t\n", landing.OnIce, landing.OnIce)
	fmt.Fprintf(output, "landed_at  %-16T %s\n", landing.LandedAt, landing.LandedAt.Format(time.RFC3339))
	fmt.Fprintf(output, "tag        %-16T % x\n", landing.Tag, landing.Tag)
	fmt.Fprintf(output, "notes      %-16T %s\n", landing.Notes, *landing.Notes)
	return nil
}

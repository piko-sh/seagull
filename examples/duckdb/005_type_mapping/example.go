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
	"errors"
	"fmt"
	"io"
	"math/big"
	"time"

	"github.com/google/uuid"

	"seagull.sh/seagull/examples/duckdb/005_type_mapping/internal/store"
)

// run records a landing of fish and reads it back, printing the Go type sgl chose for
// each column. DuckDB declares every column's type exactly, and Seagull maps each one:
//
//   - SMALLINT, INTEGER and BIGINT give int16, int32 and int64, and the unsigned UTINYINT
//     gives uint8;
//   - DOUBLE gives float64, but REAL is DuckDB's 4-byte float, so it gives float32;
//   - TIMESTAMP and DATE give time.Time, and UUID gives uuid.UUID from
//     github.com/google/uuid;
//   - HUGEINT is a 128-bit integer with no Go equivalent, so it gives *big.Int, the type
//     duckdb-go returns, even when the column is NOT NULL;
//   - BLOB gives []byte, and a nullable VARCHAR gives *string.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives one line per column.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	// The auction lot number has 20 digits, too many for int64 or uint64.
	lot, ok := new(big.Int).SetString("20260501000000000001", 10)
	if !ok {
		return errors.New("parsing the auction lot number")
	}
	recorded, err := queries.RecordLanding(ctx, store.RecordLandingParams{
		Species:    "mackerel",
		Quay:       3,
		Boxes:      12,
		Fish:       480,
		WeightG:    163_200,
		LengthCm:   31.5,
		WaterTemp:  11.25,
		OnIce:      true,
		LandedAt:   time.Date(2026, time.May, 1, 5, 30, 0, 0, time.UTC),
		LandedOn:   time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC),
		CatchID:    uuid.MustParse("6f1c2a5e-4d3b-4c2a-9e8f-1a2b3c4d5e6f"),
		AuctionLot: lot,
		Tag:        []byte{0x04, 0xa2, 0x19, 0xf3},
		Notes:      new("landed at Newlyn"),
	})
	if err != nil {
		return fmt.Errorf("recording a landing: %w", err)
	}

	landing, err := queries.GetLanding(ctx, recorded.ID)
	if err != nil {
		return fmt.Errorf("getting landing %d: %w", recorded.ID, err)
	}
	fmt.Fprintf(output, "id           %-10T %d\n", landing.ID, landing.ID)
	fmt.Fprintf(output, "species      %-10T %s\n", landing.Species, landing.Species)
	fmt.Fprintf(output, "quay         %-10T %d\n", landing.Quay, landing.Quay)
	fmt.Fprintf(output, "boxes        %-10T %d\n", landing.Boxes, landing.Boxes)
	fmt.Fprintf(output, "fish         %-10T %d\n", landing.Fish, landing.Fish)
	fmt.Fprintf(output, "weight_g     %-10T %d\n", landing.WeightG, landing.WeightG)
	fmt.Fprintf(output, "length_cm    %-10T %.1f\n", landing.LengthCm, landing.LengthCm)
	fmt.Fprintf(output, "water_temp   %-10T %.2f\n", landing.WaterTemp, landing.WaterTemp)
	fmt.Fprintf(output, "on_ice       %-10T %t\n", landing.OnIce, landing.OnIce)
	fmt.Fprintf(output, "landed_at    %-10T %s\n", landing.LandedAt, landing.LandedAt.Format(time.RFC3339))
	fmt.Fprintf(output, "landed_on    %-10T %s\n", landing.LandedOn, landing.LandedOn.Format(time.DateOnly))
	fmt.Fprintf(output, "catch_id     %-10T %s\n", landing.CatchID, landing.CatchID)
	fmt.Fprintf(output, "auction_lot  %-10T %s\n", landing.AuctionLot, landing.AuctionLot)
	// %T prints []byte as []uint8, since byte is an alias for uint8.
	fmt.Fprintf(output, "tag          %-10T % x\n", landing.Tag, landing.Tag)
	fmt.Fprintf(output, "notes        %-10T %s\n", landing.Notes, *landing.Notes)
	return nil
}

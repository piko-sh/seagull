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
	"fmt"
	"io"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"seagull.sh/seagull/examples/postgres/005_type_mapping_pgx/internal/store"
)

// run records a landing of fish and reads it back through pgx, printing the Go type sgl
// chose for each column of db/migrations. Most field types match the database/sql
// emitter's, but the native pgx emitter maps INTERVAL to time.Duration rather than
// string. pgx reads PostgreSQL's binary formats itself:
//
//   - SMALLINT, INTEGER and BIGINT give int16, int32 and int64; REAL gives float32 and
//     DOUBLE PRECISION gives float64;
//   - NUMERIC gives apd.Decimal and UUID gives uuid.UUID, which pgx fills through their
//     Scan methods;
//   - DATE and TIMESTAMPTZ give time.Time, and INTERVAL gives time.Duration, which pgx
//     decodes itself, counting a month as 30 days;
//   - INET has no mapping in Seagull and gives any, which pgx fills with a netip.Prefix,
//     where database/sql would give a string.
//
// Takes pool (*pgxpool.Pool) which is a pool on a migrated database.
// Takes output (io.Writer) which receives one line per column.
//
// Returns error when a query fails.
func run(ctx context.Context, pool *pgxpool.Pool, output io.Writer) error {
	queries := store.New(pool)

	recorded, err := queries.RecordLanding(ctx, store.RecordLandingParams{
		CatchID:   uuid.MustParse("6f1c0b8e-4b2a-4c55-9d7e-2a3f5e8c1d40"),
		Species:   "mackerel",
		Boxes:     12,
		Fish:      480,
		WeightG:   163_200,
		LengthCm:  31.5,
		SeaTemp:   12.25,
		Price:     *apd.New(41_280, -2),
		OnIce:     true,
		LandedOn:  time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC),
		LandedAt:  time.Date(2026, time.May, 1, 5, 30, 0, 0, time.UTC),
		AtSea:     14*time.Hour + 45*time.Minute,
		HarbourIP: "192.0.2.17",
		Tag:       []byte{0x04, 0xa2, 0x19, 0xf3},
		Notes:     new("landed at Newlyn"),
	})
	if err != nil {
		return fmt.Errorf("recording a landing: %w", err)
	}

	landing, err := queries.GetLanding(ctx, recorded.ID)
	if err != nil {
		return fmt.Errorf("getting landing %d: %w", recorded.ID, err)
	}
	fmt.Fprintf(output, "id          %-14T %d\n", landing.ID, landing.ID)
	fmt.Fprintf(output, "catch_id    %-14T %s\n", landing.CatchID, landing.CatchID)
	fmt.Fprintf(output, "species     %-14T %s\n", landing.Species, landing.Species)
	fmt.Fprintf(output, "boxes       %-14T %d\n", landing.Boxes, landing.Boxes)
	fmt.Fprintf(output, "fish        %-14T %d\n", landing.Fish, landing.Fish)
	fmt.Fprintf(output, "weight_g    %-14T %d\n", landing.WeightG, landing.WeightG)
	fmt.Fprintf(output, "length_cm   %-14T %.1f\n", landing.LengthCm, landing.LengthCm)
	fmt.Fprintf(output, "sea_temp    %-14T %.2f\n", landing.SeaTemp, landing.SeaTemp)
	fmt.Fprintf(output, "price       %-14T %s\n", landing.Price, landing.Price.String())
	fmt.Fprintf(output, "on_ice      %-14T %t\n", landing.OnIce, landing.OnIce)
	fmt.Fprintf(output, "landed_on   %-14T %s\n", landing.LandedOn, landing.LandedOn.Format(time.DateOnly))
	// TIMESTAMPTZ arrives in the connection's time zone, so print it in UTC.
	fmt.Fprintf(output, "landed_at   %-14T %s\n", landing.LandedAt, landing.LandedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(output, "at_sea      %-14T %s\n", landing.AtSea, landing.AtSea)
	fmt.Fprintf(output, "harbour_ip  %-14T %v\n", landing.HarbourIP, landing.HarbourIP)
	// %T prints []byte as []uint8, since byte is an alias for uint8.
	fmt.Fprintf(output, "tag         %-14T % x\n", landing.Tag, landing.Tag)
	fmt.Fprintf(output, "notes       %-14T %s\n", landing.Notes, *landing.Notes)
	return nil
}

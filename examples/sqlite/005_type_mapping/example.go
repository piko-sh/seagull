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

	"seagull.sh/seagull/examples/sqlite/005_type_mapping/internal/store"
)

// run records a landing of fish and reads it back, printing the Go type sgl chose for
// each column. SQLite stores every value as one of five storage classes, so Seagull maps
// the type each column declares in db/migrations. SMALLINT, INTEGER and BIGINT all have
// INTEGER affinity, which SQLite stores in up to eight bytes whatever width the column
// declares, so each gives int64, as the INTEGER PRIMARY KEY rowid does. REAL gives
// float64, and DATETIME gives time.Time, which the driver parses back from the text it
// stored. SQLite has no exact decimal type, so money is best kept as INTEGER pence; a
// DECIMAL column maps to apd.Decimal from github.com/cockroachdb/apd/v3.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives one line per column.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	recorded, err := queries.RecordLanding(ctx, store.RecordLandingParams{
		Species:  "mackerel",
		Boxes:    12,
		Fish:     480,
		WeightG:  163_200,
		LengthCm: 31.5,
		OnIce:    true,
		LandedAt: time.Date(2026, time.May, 1, 5, 30, 0, 0, time.UTC),
		Tag:      []byte{0x04, 0xa2, 0x19, 0xf3},
		Notes:    new("landed at Newlyn"),
	})
	if err != nil {
		return fmt.Errorf("recording a landing: %w", err)
	}

	landing, err := queries.GetLanding(ctx, recorded.ID)
	if err != nil {
		return fmt.Errorf("getting landing %d: %w", recorded.ID, err)
	}
	fmt.Fprintf(output, "id         %-10T %d\n", landing.ID, landing.ID)
	fmt.Fprintf(output, "species    %-10T %s\n", landing.Species, landing.Species)
	fmt.Fprintf(output, "boxes      %-10T %d\n", landing.Boxes, landing.Boxes)
	fmt.Fprintf(output, "fish       %-10T %d\n", landing.Fish, landing.Fish)
	fmt.Fprintf(output, "weight_g   %-10T %d\n", landing.WeightG, landing.WeightG)
	fmt.Fprintf(output, "length_cm  %-10T %.1f\n", landing.LengthCm, landing.LengthCm)
	fmt.Fprintf(output, "on_ice     %-10T %t\n", landing.OnIce, landing.OnIce)
	fmt.Fprintf(output, "landed_at  %-10T %s\n", landing.LandedAt, landing.LandedAt.Format(time.RFC3339))
	// %T prints []byte as []uint8, since byte is an alias for uint8.
	fmt.Fprintf(output, "tag        %-10T % x\n", landing.Tag, landing.Tag)
	fmt.Fprintf(output, "notes      %-10T %s\n", landing.Notes, *landing.Notes)
	return nil
}

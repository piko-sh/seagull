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
	"net"

	"seagull.sh/seagull/examples/clickhouse/035_clickhouse_arrays_maps/internal/store"
)

// run opens three kiosks along the promenade and reads them back. One kiosks row holds
// ClickHouse's composite types:
//
//   - town is LowCardinality(String), a storage hint that dictionary-encodes repeated
//     values; in Go it is a plain string;
//   - tags is Array(String), a []string, and KiosksByTag unrolls it with ARRAY JOIN,
//     which gives one row per tag, so the tag column is a plain string;
//   - prices is Map(String, UInt32), bound from a Go map as one {prices:Map(String,
//     UInt32)} value;
//   - location is Tuple(lat Float64, lon Float64), written from two Float64 parameters as
//     the tuple literal ({lat:Float64}, {lon:Float64}); a Tuple parameter would take a
//     map[string]any keyed by element name;
//   - till is IPv4, bound from a net.IP.
//
// Through database/sql, Seagull gives each the Go type clickhouse-go's bridge scans it
// into: Map(String, UInt32) is a map[string]uint32, the named Tuple is a map[string]any
// keyed by element name, and IPv4 is a net.IP. The native driver would give the Tuple a
// struct and the address a netip.Addr instead. The listing prints the Go type of each.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives the kiosks.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	kiosks := []store.OpenKioskParams{
		{
			ID:     1,
			Name:   "Chips Ahoy",
			Town:   "Padstow",
			Till:   net.ParseIP("10.0.0.11"),
			Prices: map[string]uint32{"chips": 350, "mushy peas": 150},
			Tags:   []string{"chips", "takeaway"},
			Lat:    50.5413,
			Lon:    -4.9389,
		},
		{
			ID:     2,
			Name:   "The Ice Cream Hut",
			Town:   "Padstow",
			Till:   net.ParseIP("10.0.0.12"),
			Prices: map[string]uint32{"99 flake": 300},
			Tags:   []string{"ice cream", "takeaway"},
			Lat:    50.5420,
			Lon:    -4.9375,
		},
		{
			ID:     3,
			Name:   "Gull's Rest",
			Town:   "Rock",
			Till:   net.ParseIP("10.0.1.20"),
			Prices: map[string]uint32{"crab sandwich": 650, "tea": 220},
			Tags:   []string{"cafe", "seating"},
			Lat:    50.5465,
			Lon:    -4.9230,
		},
	}
	for _, kiosk := range kiosks {
		if err := queries.OpenKiosk(ctx, kiosk); err != nil {
			return fmt.Errorf("opening %s: %w", kiosk.Name, err)
		}
	}

	first, err := queries.GetKiosk(ctx, 1)
	if err != nil {
		return fmt.Errorf("getting kiosk 1: %w", err)
	}
	fmt.Fprintf(output, "town      %-24T %s\n", first.Town, first.Town)
	fmt.Fprintf(output, "tags      %-24T %v\n", first.Tags, first.Tags)
	fmt.Fprintf(output, "prices    %-24T %v\n", first.Prices, first.Prices)
	fmt.Fprintf(output, "location  %-24T %v\n", first.Location, first.Location)
	fmt.Fprintf(output, "till      %-24T %v\n", first.Till, first.Till)

	tagged, err := queries.KiosksByTag(ctx)
	if err != nil {
		return fmt.Errorf("listing kiosks by tag: %w", err)
	}
	fmt.Fprintln(output, "kiosks by tag, one row per tag:")
	for _, row := range tagged {
		fmt.Fprintf(output, "  %-10s %s\n", row.Tag, row.Name)
	}

	padstow, err := queries.KiosksInTown(ctx, "Padstow")
	if err != nil {
		return fmt.Errorf("listing Padstow's kiosks: %w", err)
	}
	fmt.Fprintln(output, "kiosks in Padstow:")
	for _, kiosk := range padstow {
		fmt.Fprintf(output, "  #%d %s\n", kiosk.ID, kiosk.Name)
	}
	return nil
}

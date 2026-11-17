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
	"io"
	"slices"

	"seagull.sh/seagull/examples/duckdb/037_duckdb_nested_types/internal/store"
)

// contact is the document the generated JSON type holds for a UNION value: the member's
// tag and its value.
type contact struct {
	// Tag names the union member that is set, phone or vhf_channel.
	Tag string `json:"tag"`

	// Value is the member's value.
	Value any `json:"value"`
}

// run reads and writes DuckDB nested column types.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives each boat and the summaries.
//
// Returns error when a query fails or a value has an unexpected shape.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	boats := []store.MoorBoatParams{
		{
			Name:  "Kittiwake",
			Berth: store.BoatsBerth{Pontoon: new("A"), Number: new(int32(12))},
			Crew:  []string{"Morwenna", "Jago"},
			Route: []store.BoatsRouteItem{{Lat: new(50.10), Lon: new(-5.54)}, {Lat: new(50.08), Lon: new(-5.55)}},
			Fees:  map[string]int32{"July": 300, "August": 450},
		},
		{
			Name:  "Salty Chip",
			Berth: store.BoatsBerth{Pontoon: new("B"), Number: new(int32(3))},
			Crew:  []string{"Gwen"},
			Route: []store.BoatsRouteItem{},
			Fees:  map[string]int32{"July": 180},
		},
	}
	for _, boat := range boats {
		if err := queries.MoorBoat(ctx, boat); err != nil {
			return fmt.Errorf("mooring %s: %w", boat.Name, err)
		}
	}
	if _, err := queries.ContactByPhone(ctx, store.ContactByPhoneParams{Phone: "07700 900123", Name: "Kittiwake"}); err != nil {
		return fmt.Errorf("setting Kittiwake's phone: %w", err)
	}
	if _, err := queries.ContactByRadio(ctx, store.ContactByRadioParams{Channel: 16, Name: "Salty Chip"}); err != nil {
		return fmt.Errorf("setting Salty Chip's radio channel: %w", err)
	}

	moored, err := queries.ListBoats(ctx)
	if err != nil {
		return fmt.Errorf("listing boats: %w", err)
	}
	for _, boat := range moored {
		reached := contact{Tag: "nothing", Value: "no contact"}
		if boat.Contact != nil {
			if err := json.Unmarshal(boat.Contact, &reached); err != nil {
				return fmt.Errorf("decoding %s's contact: %w", boat.Name, err)
			}
		}
		fmt.Fprintln(output, boat.Name)
		fmt.Fprintf(output, "  berth    %-23T pontoon %s, berth %s\n", boat.Berth, nullable(boat.Berth.Pontoon), nullable(boat.Berth.Number))
		fmt.Fprintf(output, "  crew     %-23T %v\n", boat.Crew, boat.Crew)
		fmt.Fprintf(output, "  route    %-23T %v\n", boat.Route, stops(boat.Route))
		fmt.Fprintf(output, "  fees     %-23T %v\n", boat.Fees, boat.Fees)
		fmt.Fprintf(output, "  contact  %-23T %s, so by %s: %v\n", boat.Contact, boat.Contact, reached.Tag, reached.Value)
	}

	summaries, err := queries.BoatSummaries(ctx)
	if err != nil {
		return fmt.Errorf("summarising boats: %w", err)
	}
	fmt.Fprintln(output, "summaries:")
	for _, summary := range summaries {
		slices.Sort(summary.Months)
		augustFee := "none"
		if summary.AugustFee.Valid {
			augustFee = fmt.Sprint(summary.AugustFee.V)
		}
		fmt.Fprintf(output, "  %-10s skipper %s, crew of %d, fees for %v, August %s\n", summary.Name, summary.Skipper.V, summary.CrewSize, summary.Months, augustFee)
	}
	return nil
}

// stops returns a route's stops, each as its latitude and longitude.
//
// Takes route ([]store.BoatsRouteItem) which is the route to describe.
//
// Returns []string which holds one latitude,longitude pair per stop.
func stops(route []store.BoatsRouteItem) []string {
	pairs := make([]string, 0, len(route))
	for _, stop := range route {
		pairs = append(pairs, nullable(stop.Lat)+","+nullable(stop.Lon))
	}
	return pairs
}

// nullable returns the value a nullable STRUCT field holds, or NULL when it holds none.
//
// Takes value (*T) which is the field.
//
// Returns string which is the value as text.
func nullable[T any](value *T) string {
	if value == nil {
		return "NULL"
	}
	return fmt.Sprint(*value)
}

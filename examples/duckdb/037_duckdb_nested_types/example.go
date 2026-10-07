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

// run moors two boats in a marina whose boats table has a column of each nested DuckDB
// type, then reads them back whole and through typed expressions. Seagull types each one
// from its declaration:
//
//   - a LIST of a scalar, crew VARCHAR[], is a typed slice, []string;
//   - a STRUCT, berth, is a struct type that models.go declares, named after the table
//     and column that hold it, store.BoatsBerth, with a field for each of its fields.
//     DuckDB lets any field of a STRUCT be NULL, so each field is a pointer, which the
//     program fills with new;
//   - a LIST of STRUCT, route, is a slice of such a type, []store.BoatsRouteItem;
//   - a MAP, fees MAP(VARCHAR, INTEGER), is map[string]int32. The generated code copies a
//     map into the driver's own map type when it binds one, and a nil map binds NULL;
//   - a UNION, contact, is the generated JSON type: duckdb-go reads a duckdb.Union, which
//     JSON holds encoded as {"value": ..., "tag": ...}, and a nil JSON is NULL. The JSON
//     type cannot bind a union, so ContactByPhone and ContactByRadio write one by casting
//     the value to a member's type, VARCHAR or INTEGER, which DuckDB converts to that
//     member.
//
// DuckDB can hold NULL in any list element or map value, so these would be pointers, as
// 008_arrays shows; this marina never stores one there, so sgl.yaml declares them
// non-null with go.types.non_null_elements: true, and they read as plain values.
//
// The generated SQL selects the LIST, STRUCT and MAP columns as they are, and the
// generated code reads the values duckdb-go returns for them into the typed fields.
//
// BoatSummaries reaches inside the columns, and those results are typed: crew[1] is
// *string, as a list may be shorter, array_length is an int64, map_keys gives []string,
// and fees['August'] is *int32, NULL when the map has no such key. A Go map keeps no
// order, so the order DuckDB stores its keys in differs from one write to the next, and
// the program sorts the months before printing them.
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
		if summary.AugustFee != nil {
			augustFee = fmt.Sprint(*summary.AugustFee)
		}
		fmt.Fprintf(output, "  %-10s skipper %s, crew of %d, fees for %v, August %s\n", summary.Name, *summary.Skipper, summary.CrewSize, summary.Months, augustFee)
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

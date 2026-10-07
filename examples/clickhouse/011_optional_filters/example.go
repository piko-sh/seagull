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
	"strings"

	"seagull.sh/seagull/examples/clickhouse/011_optional_filters/internal/store"
)

// run searches boat trips four ways with one query. Each filter in SearchTrips is written
// as ({destination:Nullable(String)} IS NULL OR destination =
// {destination:Nullable(String)}), and its binding line says optional: true, which makes
// the Params field a pointer. A nil pointer is sent as NULL, so the IS NULL test is true
// and that filter matches every row; a set pointer applies the filter. The SQL never
// changes, only the values bound to it.
//
// The placeholder's type must be Nullable(T). With {destination:String} the server reads
// the NULL marker as text, the IS NULL test is false, and a search left open quietly
// matches nothing. Nullable(T) alone already makes the field a pointer; optional: true
// says the filter may be left out on purpose.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives each search's trips.
//
// Returns error when a query fails.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	trips := []store.AddTripParams{
		{ID: 1, Name: "Seal spotting", Destination: "Gull Rock", PricePence: 1500, DogsWelcome: true},
		{ID: 2, Name: "Puffin cruise", Destination: "Puffin Island", PricePence: 2500, DogsWelcome: false},
		{ID: 3, Name: "Mackerel fishing", Destination: "Gull Rock", PricePence: 2000, DogsWelcome: false},
		{ID: 4, Name: "Sunset sail", Destination: "the bay", PricePence: 1200, DogsWelcome: true},
	}
	for _, trip := range trips {
		if err := queries.AddTrip(ctx, trip); err != nil {
			return fmt.Errorf("adding %s: %w", trip.Name, err)
		}
	}

	searches := []struct {
		label      string
		parameters store.SearchTripsParams
	}{
		{label: "no filters", parameters: store.SearchTripsParams{}},
		{label: "to Gull Rock", parameters: store.SearchTripsParams{Destination: new("Gull Rock")}},
		{label: "£15 or less", parameters: store.SearchTripsParams{MaxPrice: new(uint32(1500))}},
		{label: "to Gull Rock with a dog", parameters: store.SearchTripsParams{Destination: new("Gull Rock"), DogsWelcome: new(true)}},
	}
	for _, search := range searches {
		found, err := queries.SearchTrips(ctx, search.parameters)
		if err != nil {
			return fmt.Errorf("searching %s: %w", search.label, err)
		}
		names := make([]string, 0, len(found))
		for _, trip := range found {
			names = append(names, trip.Name)
		}
		fmt.Fprintf(output, "%-24s %s\n", search.label+":", strings.Join(names, ", "))
	}
	return nil
}

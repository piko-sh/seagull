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
	"strings"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"case_runner/db"
)

type results map[string]any

const schema = `CREATE TYPE mood AS ENUM ('calm', 'stormy');
CREATE TABLE voyages (
  id INTEGER PRIMARY KEY,
  moods mood[] NOT NULL,
  fares DECIMAL(10, 2)[] NOT NULL,
  photos BLOB[] NOT NULL,
  crew UUID[] NOT NULL,
  legs INTERVAL[] NOT NULL,
  gusts UTINYINT[] NOT NULL,
  watches mood[][] NOT NULL,
  pair mood[2] NOT NULL,
  forecast mood[],
  price STRUCT(amount DECIMAL(10, 2), currency VARCHAR),
  rates MAP(VARCHAR, DECIMAL(10, 2))
);`

func (r results) record(name string, read func() (any, error)) {
	value, err := read()
	if err != nil {
		r[name] = "error: " + err.Error()
		return
	}
	r[name] = value
}

func decimals(texts ...string) []apd.Decimal {
	values := make([]apd.Decimal, len(texts))
	for index, text := range texts {
		value, _, err := apd.NewFromString(text)
		if err != nil {
			panic(err)
		}
		values[index] = *value
	}
	return values
}

func refusedByTheDriver(err error) bool {
	return err != nil && strings.Contains(err.Error(), "unsupported data type")
}

func main() {
	conn, err := sql.Open("duckdb", "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, schema); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)
	output := results{}
	output.record("add_voyage", func() (any, error) {
		return "added", queries.AddVoyage(ctx, db.AddVoyageParams{
			ID:       1,
			Moods:    []db.Mood{db.MoodCalm, db.MoodStormy},
			Fares:    decimals("12.50", "0.05"),
			Photos:   [][]byte{[]byte("bow"), {}},
			Crew:     []uuid.UUID{uuid.MustParse("0b5b1f0e-7c2a-4d4e-9f00-000000000001")},
			Legs:     []string{"1 day", "02:30:00"},
			Gusts:    []uint8{0, 17, 255},
			Watches:  [][]db.Mood{{db.MoodCalm}, {db.MoodStormy, db.MoodCalm}},
			Pair:     []db.Mood{db.MoodStormy, db.MoodStormy},
			Forecast: []db.Mood{db.MoodCalm},
		})
	})
	output.record("add_voyages", func() (any, error) {
		return "added", queries.AddVoyages(ctx, []db.AddVoyagesParams{
			{ID: 2, Moods: []db.Mood{}, Fares: []apd.Decimal{}, Photos: [][]byte{}, Crew: []uuid.UUID{}, Legs: []string{},
				Gusts: []uint8{}, Watches: [][]db.Mood{}, Pair: []db.Mood{db.MoodCalm, db.MoodCalm}, Forecast: nil},
			{ID: 3, Moods: []db.Mood{db.MoodStormy}, Fares: decimals("99.99"), Photos: [][]byte{[]byte("stern")},
				Crew: []uuid.UUID{uuid.MustParse("0b5b1f0e-7c2a-4d4e-9f00-000000000002")}, Legs: []string{"3 hours"},
				Gusts: []uint8{4}, Watches: [][]db.Mood{{}}, Pair: []db.Mood{db.MoodCalm, db.MoodStormy},
				Forecast: []db.Mood{db.MoodStormy, db.MoodStormy}},
		})
	})
	output.record("clear_forecast", func() (any, error) {
		return "cleared", queries.SetForecast(ctx, db.SetForecastParams{Forecast: nil, ID: 3})
	})
	output.record("sharing_a_stormy_mood", func() (any, error) {
		rows, err := queries.VoyagesSharingAMood(ctx, []db.Mood{db.MoodStormy})
		return rows, err
	})
	output.record("set_price", func() (any, error) {
		return "set", queries.SetPrice(ctx, db.SetPriceParams{Amount: decimals("42.10")[0], Currency: "GBP", ID: 1})
	})
	output.record("set_rates", func() (any, error) {
		return "set", queries.SetRates(ctx, db.SetRatesParams{FareNames: []string{"adult", "child"}, Fares: decimals("30.00", "15.25"), ID: 1})
	})
	for _, id := range []int32{1, 2, 3} {
		output.record(fmt.Sprintf("voyage_%d", id), func() (any, error) {
			row, err := queries.GetVoyage(ctx, id)
			return row, err
		})
	}
	output.record("driver_refuses_a_typed_enum_list", func() (any, error) {
		_, err := conn.ExecContext(ctx, "UPDATE voyages SET moods = $1 WHERE id = 1", []string{"calm"})
		return refusedByTheDriver(err), nil
	})
	output.record("driver_refuses_a_struct_holding_a_decimal", func() (any, error) {
		_, err := conn.ExecContext(ctx, "UPDATE voyages SET price = $1 WHERE id = 1", map[string]any{"amount": "1.00", "currency": "GBP"})
		return refusedByTheDriver(err), nil
	})

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

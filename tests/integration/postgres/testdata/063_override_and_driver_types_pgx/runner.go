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
	"encoding/json"
	"fmt"
	"os"
	"time"
	"uuid"

	"github.com/cockroachdb/apd/v3"
	"github.com/jackc/pgx/v5"

	"case_runner/db"
	"case_runner/kinds"
)

type results map[string]any

func (r results) record(name string, read func() (any, error)) {
	value, err := read()
	if err != nil {
		r[name] = "error: " + err.Error()
		return
	}
	r[name] = value
}

func main() {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close(ctx)

	for _, statement := range []string{fmt.Sprintf("SET search_path TO %s", os.Getenv("DATABASE_SCHEMA")), "SET lc_monetary TO 'C'"} {
		if _, err := conn.Exec(ctx, statement); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	queries := db.New(conn)
	output := results{}
	moment := time.Date(2024, 1, 2, 3, 4, 5, 123456000, time.UTC)
	day := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	refund := "1.25"
	closes := "18:30:00-05"
	mask := "1011"
	first := uuid.MustParse("0b9f8e2c-6d1a-4f3e-9a7b-5c4d3e2f1a0b")
	output.record("add_full_ledger", func() (any, error) {
		return "added", queries.AddLedger(ctx, db.AddLedgerParams{
			ID: 1, Ident: first, Price: "12.50", Refund: &refund, Opens: "09:15:00+02", Closes: &closes, Flags: "101", Mask: &mask,
			Span: "1 day 02:03:04", Moments: pointers(moment, day), Days: pointers(day),
			Amounts: pointers(*apd.New(150, -2), *apd.New(-7, 0)), Blobs: [][]byte{{0x01, 0x02, 0xff}, {}},
			Rates: pointers[kinds.Rate]("0.125", "3"),
		})
	})
	output.record("add_sparse_ledger", func() (any, error) {
		return "added", queries.AddLedger(ctx, db.AddLedgerParams{
			ID: 2, Ident: uuid.MustParse("7d3c2b1a-0f9e-4d8c-8b7a-6f5e4d3c2b1a"), Price: "12.50", Refund: nil, Opens: "00:00:00+00", Closes: nil, Flags: "000", Mask: nil,
			Span: "00:00:01", Moments: []*time.Time{}, Days: nil, Amounts: []*apd.Decimal{}, Blobs: nil, Rates: []*kinds.Rate{},
		})
	})
	output.record("full_ledger", func() (any, error) { return queries.GetLedger(ctx, 1) })
	output.record("sparse_ledger", func() (any, error) { return queries.GetLedger(ctx, 2) })
	output.record("set_rates", func() (any, error) {
		return "set", queries.SetRates(ctx, db.SetRatesParams{Rates: pointers[kinds.Rate]("9.99", "-0.5"), ID: 1})
	})
	output.record("rates_after_update", func() (any, error) {
		ledger, err := queries.GetLedger(ctx, 1)
		return ledger.Rates, err
	})
	output.record("ledger_by_ident", func() (any, error) { return queries.LedgerByIdent(ctx, first) })
	output.record("ledgers_priced_at", func() (any, error) { return queries.LedgersPricedAt(ctx, "12.50") })

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func pointers[T any](values ...T) []*T {
	pointed := make([]*T, len(values))
	for index := range values {
		pointed[index] = &values[index]
	}
	return pointed
}

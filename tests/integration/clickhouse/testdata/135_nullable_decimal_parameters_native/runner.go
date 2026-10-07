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

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/shopspring/decimal"

	"case_runner/db"
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

func number(text string) *decimal.Decimal {
	return new(decimal.RequireFromString(text))
}

func text(value *decimal.Decimal) any {
	if value == nil {
		return nil
	}
	return value.String()
}

func main() {
	options, err := clickhouse.ParseDSN(os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	conn, err := clickhouse.Open(options)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()

	ctx := context.Background()
	queries := db.New(conn)
	output := results{}
	output.record("add_without_discount", func() (any, error) {
		return "added", queries.AddPrice(ctx, db.AddPriceParams{ID: 1, Amount: *number("10.00"), Discount: nil, Label: new("plain")})
	})
	output.record("add_with_discount", func() (any, error) {
		return "added", queries.AddPrice(ctx, db.AddPriceParams{ID: 2, Amount: *number("20.00"), Discount: number("2.50"), Label: new("reduced")})
	})
	output.record("add_batch", func() (any, error) {
		return "added", queries.AddPrices(ctx, []db.AddPricesParams{
			{ID: 3, Amount: *number("30.00"), Discount: nil, Label: nil},
			{ID: 4, Amount: *number("40.00"), Discount: number("4.00"), Label: nil},
		})
	})
	output.record("copy_rows", func() (any, error) {
		return "copied", queries.CopyPrices(ctx, []db.CopyPricesParams{
			{ID: 5, Amount: *number("50.00"), Discount: nil, Label: nil},
			{ID: 6, Amount: *number("60.00"), Discount: number("6.25"), Label: nil},
		})
	})
	output.record("stored_discounts", func() (any, error) {
		var stored string
		return stored, conn.QueryRow(ctx, "SELECT arrayStringConcat(groupArray(ifNull(toString(discount), 'NULL')), ',') FROM (SELECT discount FROM prices ORDER BY id)").Scan(&stored)
	})
	output.record("with_discount", func() (any, error) { return queries.PricesWithDiscount(ctx, number("2.50")) })
	output.record("with_no_discount", func() (any, error) { return queries.PricesWithDiscount(ctx, nil) })
	output.record("amounts_in", func() (any, error) {
		return queries.PricesIn(ctx, db.PricesInParams{Amounts: []decimal.Decimal{*number("20.00"), *number("60.00")}})
	})
	output.record("search_unfiltered", func() (any, error) {
		return queries.SearchPrices(ctx, db.SearchPricesParams{MaxAmount: nil, MaxDiscount: nil})
	})
	output.record("search_by_amount", func() (any, error) {
		return queries.SearchPrices(ctx, db.SearchPricesParams{MaxAmount: number("30"), MaxDiscount: nil})
	})
	output.record("search_by_discount", func() (any, error) {
		return queries.SearchPrices(ctx, db.SearchPricesParams{MaxAmount: nil, MaxDiscount: number("2.5")})
	})
	output.record("browse_from_discount", func() (any, error) {
		rows, err := queries.BrowsePrices(number("2.00")).OrderBy("id", "ASC").All(ctx)
		if err != nil {
			return nil, err
		}
		listed := make([]map[string]any, 0, len(rows))
		for index := range rows {
			listed = append(listed, map[string]any{"id": rows[index].ID, "amount": rows[index].Amount.String(), "discount": text(rows[index].Discount)})
		}
		return listed, nil
	})
	output.record("browse_below_amount", func() (any, error) {
		rows, err := queries.BrowsePrices(number("2.00")).Where("amount", "<", *number("30")).All(ctx)
		if err != nil {
			return nil, err
		}
		identifiers := make([]any, 0, len(rows))
		for index := range rows {
			identifiers = append(identifiers, rows[index].ID)
		}
		return identifiers, nil
	})
	output.record("browse_from_no_discount", func() (any, error) { return queries.BrowsePrices(nil).Count(ctx) })
	output.record("list_prices", func() (any, error) {
		rows, err := queries.ListPrices(ctx, number("0"))
		if err != nil {
			return nil, err
		}
		listed := make([]map[string]any, 0, len(rows))
		for index := range rows {
			listed = append(listed, map[string]any{
				"id": rows[index].ID, "amount": rows[index].Amount.String(), "discount": text(rows[index].Discount),
				"effective": text(rows[index].Effective),
			})
		}
		return listed, nil
	})

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

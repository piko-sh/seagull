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

	"github.com/cockroachdb/apd/v3"

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

func decimal(text string) *apd.Decimal {
	value, _, err := apd.NewFromString(text)
	if err != nil {
		panic(err)
	}
	return value
}

func text(value *apd.Decimal) any {
	if value == nil {
		return nil
	}
	return value.String()
}

func main() {
	conn, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+os.Getenv("DATABASE_SCHEMA")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	queries := db.New(conn)
	output := results{}
	output.record("add_without_discount", func() (any, error) {
		return "added", queries.AddPrice(ctx, db.AddPriceParams{ID: 1, Amount: *decimal("10.00"), Discount: nil, Label: new("plain")})
	})
	output.record("add_with_discount", func() (any, error) {
		return "added", queries.AddPrice(ctx, db.AddPriceParams{ID: 2, Amount: *decimal("20.00"), Discount: decimal("2.50"), Label: new("reduced")})
	})
	output.record("add_batch", func() (any, error) {
		return "added", queries.AddPrices(ctx, []db.AddPricesParams{
			{ID: 3, Amount: *decimal("30.00"), Discount: nil, Label: nil},
			{ID: 4, Amount: *decimal("40.00"), Discount: decimal("4.00"), Label: nil},
		})
	})
	output.record("copy_rows", func() (any, error) {
		return "copied", queries.CopyPrices(ctx, []db.CopyPricesParams{
			{ID: 5, Amount: *decimal("50.00"), Discount: nil, Label: nil},
			{ID: 6, Amount: *decimal("60.00"), Discount: decimal("6.25"), Label: nil},
		})
	})
	output.record("set_discount", func() (any, error) {
		return queries.SetDiscount(ctx, db.SetDiscountParams{Discount: decimal("1.25"), ID: 1})
	})
	output.record("clear_discount", func() (any, error) {
		return queries.SetDiscount(ctx, db.SetDiscountParams{Discount: nil, ID: 4})
	})
	output.record("stored_discounts", func() (any, error) {
		var stored string
		return stored, conn.QueryRowContext(ctx, "SELECT string_agg(coalesce(discount::text, 'NULL'), ',' ORDER BY id) FROM prices").Scan(&stored)
	})
	output.record("with_discount", func() (any, error) { return queries.PricesWithDiscount(ctx, decimal("2.50")) })
	output.record("with_no_discount", func() (any, error) { return queries.PricesWithDiscount(ctx, nil) })
	output.record("amounts_in", func() (any, error) {
		return queries.PricesIn(ctx, db.PricesInParams{Amounts: []apd.Decimal{*decimal("20.00"), *decimal("60.00")}})
	})
	output.record("search_unfiltered", func() (any, error) {
		return queries.SearchPrices(ctx, db.SearchPricesParams{MaxAmount: nil, MaxDiscount: nil})
	})
	output.record("search_by_amount", func() (any, error) {
		return queries.SearchPrices(ctx, db.SearchPricesParams{MaxAmount: decimal("30"), MaxDiscount: nil})
	})
	output.record("search_by_discount", func() (any, error) {
		return queries.SearchPrices(ctx, db.SearchPricesParams{MaxAmount: nil, MaxDiscount: decimal("2.5")})
	})
	output.record("browse_from_discount", func() (any, error) {
		rows, err := queries.BrowsePrices(decimal("2.00")).OrderBy("id", "ASC").All(ctx)
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
		rows, err := queries.BrowsePrices(decimal("2.00")).Where("amount", "<", *decimal("30")).All(ctx)
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
		rows, err := queries.ListPrices(ctx, decimal("0"))
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

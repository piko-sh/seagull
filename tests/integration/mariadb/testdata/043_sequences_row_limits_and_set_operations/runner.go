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
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"slices"

	_ "github.com/go-sql-driver/mysql"

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

func main() {
	conn, err := sql.Open("mysql", os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)

	ctx := context.Background()
	queries := db.New(conn)
	output := results{}
	output.record("last_invoice_number", func() (any, error) { return queries.LastInvoiceNumber(ctx) })
	output.record("next_invoice_numbers", func() (any, error) { return queries.NextInvoiceNumbers(ctx) })
	output.record("move_invoice_numbers", func() (any, error) { return queries.MoveInvoiceNumbers(ctx) })
	output.record("move_back", func() (any, error) { return queries.MoveInvoiceNumbersBack(ctx) })
	for _, invoice := range []db.AddInvoiceParams{
		{Shop: "north", Items: 2, Issued: "2024-01-02"},
		{Shop: "north", Items: 4, Issued: "not a date"},
		{Shop: "south", Items: 4, Issued: "2024-03-04"},
	} {
		if err := queries.AddInvoice(ctx, invoice); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	output.record("invoice_ids", func() (any, error) { return queries.InvoiceIDs(ctx) })
	output.record("invoice_page", func() (any, error) {
		return queries.InvoicePage(ctx, db.InvoicePageParams{Offset: 1, Limit: 1})
	})
	output.record("most_items", func() (any, error) {
		rows, err := queries.MostItems(ctx)
		slices.SortFunc(rows, func(left, right db.MostItemsRow) int { return cmp.Compare(left.ID, right.ID) })
		return rows, err
	})
	output.record("shop_medians", func() (any, error) { return queries.ShopMedians(ctx) })
	output.record("shops_with_default", func() (any, error) { return queries.ShopsWithDefault(ctx, 3) })
	output.record("fewest_and_most", func() (any, error) { return queries.FewestAndMost(ctx) })
	output.record("per_item", func() (any, error) { return queries.PerItem(ctx) })

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

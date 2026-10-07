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

	"seagull.sh/seagull/examples/clickhouse/006_json/internal/store"
)

// basket is the document an order's basket column holds.
type basket struct {
	// Fish is the fish the order is for.
	Fish string `json:"fish"`

	// Portions is how many portions of fish and chips.
	Portions int `json:"portions"`

	// Extras lists the sides.
	Extras []string `json:"extras"`
}

// delivery is the document an order's delivery column holds, when the order is delivered.
type delivery struct {
	// Address is where the order goes.
	Address string `json:"address"`
}

// run places three chip shop orders and reads them back. The documents live in String
// columns, the usual home for JSON text in ClickHouse, so Basket is a string and
// Delivery, a Nullable(String), is *string: encode a value into it with json.Marshal and
// decode it with json.Unmarshal. A nil Delivery is SQL NULL.
//
// OrdersWithFish reads inside the documents with ClickHouse's JSON functions instead of
// operators: JSONExtractString(basket, 'fish') extracts a value as text, and
// JSONExtractRaw(basket, 'extras') extracts the fragment's JSON text. Both return a
// String, which is empty for a missing key rather than NULL, so Extras is a string.
// Address is *string because the delivery column is Nullable(String), and a JSON function
// given NULL returns NULL.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives the decoded orders and the search.
//
// Returns error when a query fails or a document does not encode or decode.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	orders := []struct {
		customer string
		basket   basket
		delivery *delivery
	}{
		{customer: "Steven", basket: basket{Fish: "cod", Portions: 2, Extras: []string{"mushy peas", "curry sauce"}}},
		{customer: "Gwen", basket: basket{Fish: "haddock", Portions: 1, Extras: []string{}}},
		{customer: "Morwenna", basket: basket{Fish: "cod", Portions: 1, Extras: []string{"pickled egg"}}, delivery: &delivery{Address: "4 Harbour Row"}},
	}
	for index, order := range orders {
		encodedBasket, err := json.Marshal(order.basket)
		if err != nil {
			return fmt.Errorf("encoding %s's basket: %w", order.customer, err)
		}
		parameters := store.PlaceOrderParams{ID: uint64(index + 1), Customer: order.customer, Basket: string(encodedBasket), Delivery: nil}
		if order.delivery != nil {
			encodedDelivery, err := json.Marshal(order.delivery)
			if err != nil {
				return fmt.Errorf("encoding %s's delivery: %w", order.customer, err)
			}
			parameters.Delivery = new(string(encodedDelivery))
		}
		if err := queries.PlaceOrder(ctx, parameters); err != nil {
			return fmt.Errorf("placing %s's order: %w", order.customer, err)
		}
	}

	first, err := queries.GetOrder(ctx, 1)
	if err != nil {
		return fmt.Errorf("getting order 1: %w", err)
	}
	var decoded basket
	if err := json.Unmarshal([]byte(first.Basket), &decoded); err != nil {
		return fmt.Errorf("decoding order 1's basket: %w", err)
	}
	fmt.Fprintf(output, "order 1 stored %s\n", first.Basket)
	fmt.Fprintf(output, "decoded: %d x %s with %v, delivery is NULL: %t\n", decoded.Portions, decoded.Fish, decoded.Extras, first.Delivery == nil)

	cod, err := queries.OrdersWithFish(ctx, "cod")
	if err != nil {
		return fmt.Errorf("finding cod orders: %w", err)
	}
	fmt.Fprintln(output, "\ncod orders:")
	for _, order := range cod {
		address := "collecting"
		if order.Address != nil {
			address = "to " + *order.Address
		}
		fmt.Fprintf(output, "  order %d  %-9s extras %-30s %s\n", order.ID, order.Customer, order.Extras, address)
	}
	return nil
}

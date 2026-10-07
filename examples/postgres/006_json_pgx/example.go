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
	"io"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"seagull.sh/seagull/examples/postgres/006_json_pgx/internal/chips"
	"seagull.sh/seagull/examples/postgres/006_json_pgx/internal/store"
)

// run places three chip shop orders and reads them back through pgx. A JSONB column
// becomes the generated store.JSON type, which holds the raw document as bytes; it
// implements driver.Valuer and sql.Scanner, and pgx calls both, so the same type works
// with either driver. A nil store.JSON is SQL NULL.
//
// pgx can also decode JSONB into any Go type itself. GetOrder's sgl.column(basket,
// go_type: ...) makes Basket a chips.Basket, and pgx unmarshals the document into it, so
// there is no json.Unmarshal to write. Through database/sql the same override fails,
// unless the type implements sql.Scanner. A query's sgl.column override changes only that
// query's returned column, so PlaceOrderParams still takes store.JSON, encoded with
// json.Marshal.
//
// JSONB returns documents normalised, with a space after each comma and colon.
// OrdersWithFish filters with ->>, which extracts text, and selects with ->, which
// extracts a JSONB fragment; OrdersContaining uses @>, which matches a document
// containing the one bound to $1::jsonb.
//
// Takes pool (*pgxpool.Pool) which is a pool on a migrated database.
// Takes output (io.Writer) which receives the decoded order and the searches.
//
// Returns error when a query fails or a document does not encode.
func run(ctx context.Context, pool *pgxpool.Pool, output io.Writer) error {
	queries := store.New(pool)

	orders := []struct {
		customer string
		basket   chips.Basket
		delivery *chips.Delivery
	}{
		{customer: "Steven", basket: chips.Basket{Fish: "cod", Portions: 2, Extras: []string{"mushy peas", "curry sauce"}}},
		{customer: "Gwen", basket: chips.Basket{Fish: "haddock", Portions: 1, Extras: []string{}}},
		{customer: "Morwenna", basket: chips.Basket{Fish: "cod", Portions: 1, Extras: []string{"pickled egg"}}, delivery: &chips.Delivery{Address: "4 Harbour Row"}},
	}
	for _, order := range orders {
		encodedBasket, err := json.Marshal(order.basket)
		if err != nil {
			return fmt.Errorf("encoding %s's basket: %w", order.customer, err)
		}
		parameters := store.PlaceOrderParams{Customer: order.customer, Basket: encodedBasket, Delivery: nil}
		if order.delivery != nil {
			encodedDelivery, err := json.Marshal(order.delivery)
			if err != nil {
				return fmt.Errorf("encoding %s's delivery: %w", order.customer, err)
			}
			parameters.Delivery = encodedDelivery
		}
		if err := queries.PlaceOrder(ctx, parameters); err != nil {
			return fmt.Errorf("placing %s's order: %w", order.customer, err)
		}
	}

	first, err := queries.GetOrder(ctx, 1)
	if err != nil {
		return fmt.Errorf("getting order 1: %w", err)
	}
	fmt.Fprintf(output, "order 1 decoded: %d x %s with %v, delivery is NULL: %t\n", first.Basket.Portions, first.Basket.Fish, first.Basket.Extras, first.Delivery == nil)

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

	subset := store.JSON(`{"extras": ["pickled egg"]}`)
	containing, err := queries.OrdersContaining(ctx, subset)
	if err != nil {
		return fmt.Errorf("finding orders containing %s: %w", subset, err)
	}
	customers := make([]string, 0, len(containing))
	for _, order := range containing {
		customers = append(customers, order.Customer)
	}
	fmt.Fprintf(output, "\norders containing %s: %s\n", subset, strings.Join(customers, ", "))
	return nil
}

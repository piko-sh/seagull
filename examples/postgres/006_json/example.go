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
	"reflect"
	"strings"

	"seagull.sh/seagull/examples/postgres/006_json/internal/store"
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

// run places three chip shop orders and reads them back. A JSONB column becomes the
// generated store.JSON type, which holds the raw document as bytes: encode a value into
// it with json.Marshal and decode it with json.Unmarshal. A nil store.JSON is SQL NULL.
//
// JSONB stores a parsed document, not the text it was sent, and writes it back in its own
// form: a space after each comma and colon, keys reordered and duplicate keys dropped, so
// compare documents by decoding them, not by their bytes.
//
// OrdersWithFish filters with ->>, which extracts a value as text, and selects with ->,
// which extracts a JSONB fragment, so Extras is store.JSON again. DeliveryAddress is
// *string because ->> gives NULL when the delivery column or its address is missing. The
// ->> comparison's parameter is pinned with sgl.param(fish, type: text), as inference
// leaves it as any. OrdersContaining uses @>, which matches a document containing the one
// bound to $1::jsonb.
//
// The rows go straight out as JSON, as an HTTP handler would send them, so sgl.yaml
// shapes the generated structs for that: go.json_tags writes each field's json tag in
// camel case with omitempty, so delivery_address is "deliveryAddress" and a nil one is
// left out; go.struct_tags adds a db tag holding the column's name, {name}, for libraries
// that map columns by tag; and go.empty_slices makes a many method return an empty slice
// when no row matches, which encodes as [] rather than null.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives the decoded orders and the searches.
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
	var decoded basket
	if err := json.Unmarshal(first.Basket, &decoded); err != nil {
		return fmt.Errorf("decoding order 1's basket: %w", err)
	}
	fmt.Fprintf(output, "order 1 decoded: %d x %s with %v, delivery is NULL: %t\n", decoded.Portions, decoded.Fish, decoded.Extras, first.Delivery == nil)

	cod, err := queries.OrdersWithFish(ctx, "cod")
	if err != nil {
		return fmt.Errorf("finding cod orders: %w", err)
	}
	fmt.Fprintln(output, "\ncod orders:")
	for _, order := range cod {
		address := "collecting"
		if order.DeliveryAddress != nil {
			address = "to " + *order.DeliveryAddress
		}
		fmt.Fprintf(output, "  order %d  %-9s extras %-30s %s\n", order.ID, order.Customer, order.Extras, address)
	}
	if err := writeJSON(output, "cod orders as JSON", cod); err != nil {
		return err
	}

	plaice, err := queries.OrdersWithFish(ctx, "plaice")
	if err != nil {
		return fmt.Errorf("finding plaice orders: %w", err)
	}
	if err := writeJSON(output, "plaice orders as JSON", plaice); err != nil {
		return err
	}
	field, _ := reflect.TypeFor[store.OrdersWithFishRow]().FieldByName("DeliveryAddress")
	fmt.Fprintf(output, "DeliveryAddress is tagged %s\n", field.Tag)

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

// writeJSON writes rows encoded as JSON, as an HTTP handler would send them, after a
// label.
//
// Takes output (io.Writer) which receives the label and the document.
// Takes label (string) which says what the rows are.
// Takes rows (any) which are the rows to encode.
//
// Returns error when the rows do not encode.
func writeJSON(output io.Writer, label string, rows any) error {
	encoded, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("encoding %s: %w", label, err)
	}
	fmt.Fprintf(output, "%s: %s\n", label, encoded)
	return nil
}

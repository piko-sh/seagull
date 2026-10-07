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
	"errors"
	"fmt"
	"io"

	"seagull.sh/seagull/examples/mysql/014_runtime_builder/internal/store"
)

// run searches the pier's unclaimed lost property. dynamic: runtime turns SearchUnclaimed
// into a builder: Where, OrderBy, Limit and Offset add to the SQL, and All, One or Count
// run it. The base query already has WHERE claimed = FALSE, so each Where is joined with
// AND, which sgl vet points out as hint Q022. The builder inserts its filters, order and
// limit at the places the query leaves for them, before any LIMIT or trailing semicolon,
// and Count replaces the SELECT list with COUNT(*), wrapping the whole query instead when
// it groups or is DISTINCT (Q023).
//
// The builder is safe with untrusted input. Where and OrderBy accept only the columns the
// query selects, so owner_phone, which the table has but the query leaves out, cannot be
// searched. Operators and directions come from fixed allow-lists, and every value is
// bound as a parameter, never written into the SQL. A refused input does not panic: the
// terminal returns ErrSglUnknownColumn, ErrSglUnknownOperator or ErrSglUnknownDirection.
//
// Takes database (*sql.DB) which is a migrated database.
// Takes output (io.Writer) which receives the searches and the refusals.
//
// Returns error when a query fails or an unsafe input is not refused.
func run(ctx context.Context, database *sql.DB, output io.Writer) error {
	queries := store.New(database)

	items := []store.HandInParams{
		{Item: "umbrella", Colour: "red", FoundAt: "the pier", DaysHeld: 40},
		{Item: "sunglasses", Colour: "black", FoundAt: "the beach", DaysHeld: 12},
		{Item: "bucket and spade", Colour: "red", FoundAt: "the beach", DaysHeld: 3},
		{Item: "umbrella", Colour: "blue", FoundAt: "the cafe", DaysHeld: 33},
		{Item: "phone", Colour: "black", FoundAt: "the pier", DaysHeld: 1},
	}
	for _, item := range items {
		if err := queries.HandIn(ctx, item); err != nil {
			return fmt.Errorf("handing in a %s: %w", item.Item, err)
		}
	}
	if _, err := queries.Claim(ctx, store.ClaimParams{OwnerPhone: new("07700 900123"), ID: 5}); err != nil {
		return fmt.Errorf("claiming the phone: %w", err)
	}

	red, err := queries.SearchUnclaimed().Where("colour", "=", "red").All(ctx)
	if err != nil {
		return fmt.Errorf("finding red items: %w", err)
	}
	fmt.Fprintln(output, "red:")
	printItems(output, red)

	longest, err := queries.SearchUnclaimed().
		Where("days_held", ">=", 30).
		OrderBy("days_held", "DESC").
		Limit(2).
		All(ctx)
	if err != nil {
		return fmt.Errorf("finding long-held items: %w", err)
	}
	fmt.Fprintln(output, "held 30 days or more, longest first:")
	printItems(output, longest)

	umbrellas, err := queries.SearchUnclaimed().Where("item", "=", "umbrella").Count(ctx)
	if err != nil {
		return fmt.Errorf("counting umbrellas: %w", err)
	}
	fmt.Fprintf(output, "unclaimed umbrellas: %d\n", umbrellas)

	tautology, err := queries.SearchUnclaimed().Where("item", "=", "' OR '1'='1").All(ctx)
	if err != nil {
		return fmt.Errorf("searching for a quoted value: %w", err)
	}
	fmt.Fprintf(output, "items named ' OR '1'='1: %d, as the value is bound, not run\n", len(tautology))

	attempts := []struct {
		label  string
		search *store.SearchUnclaimedBuilder
	}{
		{label: "a column the query does not select", search: queries.SearchUnclaimed().Where("owner_phone", "IS NOT NULL", nil)},
		{label: "SQL in a column name", search: queries.SearchUnclaimed().OrderBy("id; DROP TABLE lost_property", "ASC")},
		{label: "an operator outside the allow-list", search: queries.SearchUnclaimed().Where("colour", "= 'red' OR 1=1 --", "red")},
		{label: "a direction outside the allow-list", search: queries.SearchUnclaimed().OrderBy("days_held", "DESC; DROP TABLE lost_property")},
	}
	for _, attempt := range attempts {
		_, err := attempt.search.All(ctx)
		switch {
		case errors.Is(err, store.ErrSglUnknownColumn), errors.Is(err, store.ErrSglUnknownOperator), errors.Is(err, store.ErrSglUnknownDirection):
			fmt.Fprintf(output, "refused %s: %v\n", attempt.label, err)
		case err == nil:
			return fmt.Errorf("%s: got no error, want a refusal", attempt.label)
		default:
			return fmt.Errorf("%s: got %w, want a refusal", attempt.label, err)
		}
	}
	return nil
}

// printItems writes one line per item.
//
// Takes output (io.Writer) which receives the lines.
// Takes items ([]store.SearchUnclaimedRow) which are the items to print.
func printItems(output io.Writer, items []store.SearchUnclaimedRow) {
	for _, item := range items {
		fmt.Fprintf(output, "  #%d %s %s, found at %s, held %d days\n", item.ID, item.Colour, item.Item, item.FoundAt, item.DaysHeld)
	}
}

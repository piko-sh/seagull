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

// Package menu holds the chip shop's own types. The generated store returns them in place
// of the inferred strings, through the overrides in sgl.yaml, the migration and a query.
package menu

// Status is whether a menu item can be ordered. The menu_items table's CHECK constraint
// allows the same values.
type Status string

const (
	// Available marks an item that can be ordered.
	Available Status = "available"

	// SoldOut marks an item that has run out for the day.
	SoldOut Status = "sold_out"
)

// Price is an amount of sterling as PostgreSQL's NUMERIC(5, 2) writes it, such as 3.50.
// Keeping the server's exact text, rather than a float, keeps prices exact.
type Price string

// String formats the amount in pounds, such as £3.50.
//
// Returns string which is the formatted amount.
func (p Price) String() string {
	return "£" + string(p)
}

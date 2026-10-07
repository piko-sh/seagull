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

// Package menu holds the chip shop's own types. The generated store uses them in place of
// the enum types it would declare, through the override in sgl.yaml and the queries.
package menu

// Section is the part of the menu an item is listed in, matching the section enum.
type Section string

const (
	// Fish lists the fried fish.
	Fish Section = "fish"

	// Sides lists chips, peas and sauces.
	Sides Section = "sides"

	// Puddings lists the desserts.
	Puddings Section = "puddings"
)

// Status is whether a menu item can be ordered, matching the item_status enum.
type Status string

const (
	// Available marks an item that can be ordered.
	Available Status = "available"

	// SoldOut marks an item that has run out for the day.
	SoldOut Status = "sold_out"
)

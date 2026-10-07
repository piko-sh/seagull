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

// Package chips holds the chip shop's order documents. GetOrder's go_type override names
// Basket, so pgx decodes the basket column's JSONB straight into it.
package chips

// Basket is the document an order's basket column holds.
type Basket struct {
	// Fish is the fish the order is for.
	Fish string `json:"fish"`

	// Portions is how many portions of fish and chips.
	Portions int `json:"portions"`

	// Extras lists the sides.
	Extras []string `json:"extras"`
}

// Delivery is the document an order's delivery column holds, when the order is delivered.
type Delivery struct {
	// Address is where the order goes.
	Address string `json:"address"`
}

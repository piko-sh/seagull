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

// Package domain holds the harbour's own types, which the generated store uses through
// go_type overrides.
package domain

// Status is where a boat is: the labels of the boats.status Enum8 column.
type Status string

const (
	// StatusMoored is a boat tied up in the harbour.
	StatusMoored Status = "moored"

	// StatusFishing is a boat out at sea.
	StatusFishing Status = "fishing"

	// StatusInDock is a boat out of the water for repairs.
	StatusInDock Status = "in_dock"
)

// AtSea reports whether a boat with this status is out of the harbour.
//
// Returns bool which is true for StatusFishing.
func (status Status) AtSea() bool {
	return status == StatusFishing
}

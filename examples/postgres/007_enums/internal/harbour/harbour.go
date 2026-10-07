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

// Package harbour holds the harbour's own types. BoatsWithStatus's go_type override
// returns Status in place of the enum type Seagull generates by default.
package harbour

// Status is where a boat is. Its values are the labels of the boat_status enum in
// db/migrations, which the server enforces.
type Status string

const (
	// Moored marks a boat tied up in the harbour.
	Moored Status = "moored"

	// AtSea marks a boat out of the harbour.
	AtSea Status = "at_sea"

	// DryDock marks a boat out of the water for repair.
	DryDock Status = "dry_dock"
)

// Describe returns the status in words, for printing.
//
// Returns string which describes where the boat is.
func (s Status) Describe() string {
	switch s {
	case Moored:
		return "in the harbour"
	case AtSea:
		return "out at sea"
	case DryDock:
		return "in dry dock"
	default:
		return "somewhere unknown"
	}
}

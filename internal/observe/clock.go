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

package observe

import "time"

// Clock supplies the current time for generation trace timing, recorded migration and
// seed durations, and the applied timestamp of migration history rows created without
// executing a migration.
type Clock interface {
	// Now returns the current time.
	//
	// Returns time.Time which is the current time.
	Now() time.Time
}

// systemClock is the Clock backed by the system time.
type systemClock struct{}

// Now returns the system time.
//
// Returns time.Time which is time.Now().
func (systemClock) Now() time.Time {
	return time.Now()
}

// SystemClock returns the Clock backed by the system time, the one Seagull uses when no
// Clock is set.
//
// Returns Clock which reads time.Now.
func SystemClock() Clock {
	return systemClock{}
}

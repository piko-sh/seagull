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

// Package fuzzruntime shares recording drivers with local generated-code fuzz tests.
package fuzzruntime

import (
	"seagull.sh/seagull/internal/emit/emittest"
)

// Driver returns an existing recording harness for a built-in emitter.
//
// Any name other than pgx or clickhouse selects the database/sql harness, which also
// serves the prepared database/sql mode.
//
// Takes name (string) which names the emitter.
//
// Returns *emittest.Harness which holds recorder source and dependency stubs.
func Driver(name string) *emittest.Harness {
	switch name {
	case "pgx":
		return emittest.Pgx()
	case "clickhouse":
		return emittest.ClickHouseNative()
	default:
		return emittest.DatabaseSQL()
	}
}

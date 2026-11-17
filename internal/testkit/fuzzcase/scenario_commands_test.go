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

package fuzzcase

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommandsRetainSelectedAndUntouchedRows(t *testing.T) {
	data := []byte{commandVersion, 4, 0, 0, 16, 3, 0, 0, 1, 0, 128, 0, 0, 12, 2, 8, 0}
	sequence, ok := DecodeCommands("postgres", data)
	require.True(t, ok, "command sequence rejected")
	require.Len(t, sequence.Steps, 5, "command sequence rejected")
	matches := []int{0, 0, 1, 0, 1}
	counts := []int{1, 2, 2, 3, 2}
	for index, step := range sequence.Steps {
		require.Equal(t, matches[index], step.Matches, "step %d: %#v", index, step)
		require.Len(t, step.Rows, counts[index], "step %d: %#v", index, step)
	}
	final := sequence.Steps[4].Rows
	require.True(t, final[0][0].Number == "1" && final[0][1].Null && final[1][0].Number == "2" && final[1][1].Null,
		"untouched NULL rows changed: %#v", final)
	require.False(t, sequence.Steps[0].Rows[0][1].Null, "later updates rewrote an earlier snapshot")
}

func TestCommandsBoundSequencesAndPreserveStableSeeds(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres", "mysql", "mariadb", "duckdb", "clickhouse", "cockroachdb", "timescaledb"} {
		for _, seed := range CommandSeeds(engine) {
			first, ok := DecodeCommands(engine, seed)
			second, again := DecodeCommands(engine, seed)
			require.True(t, ok && again, "rejected %s command seed %v", engine, seed)
			require.Equal(t, first, second, "unstable %s command seed %v", engine, seed)
			require.LessOrEqual(t, len(first.Steps), commandLimit, "unbounded %s command seed %v", engine, seed)
			for index, step := range first.Steps {
				require.LessOrEqual(t, len(step.Rows), index+1, "state exceeded the insertion bound")
				require.Len(t, first.Scenario.Queries[index].Bindings, len(first.Scenario.Queries[index].Values), "binding identities drifted")
			}
		}
	}
	_, ok := DecodeCommands("unknown", []byte{commandVersion})
	require.False(t, ok, "unknown engine admitted")
	_, ok = DecodeCommands("postgres", make([]byte, ScenarioMaxBytes+1))
	require.False(t, ok, "oversized envelope admitted")
}

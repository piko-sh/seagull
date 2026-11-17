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

func TestExecutionSeedsCoverLegalNativeLifecycles(t *testing.T) {
	for _, engine := range []string{"postgres", "sqlite", "mysql", "mariadb", "duckdb", "clickhouse", "cockroachdb", "timescaledb"} {
		operations := make(map[string]bool)
		for _, seed := range ExecutionSeeds() {
			model, ok := DecodeExecution(engine, seed)
			require.True(t, ok, "seed rejected %s %v", engine, seed)
			require.Len(t, model.Files(), 7, "incomplete migration and seed bundle")
			var state ExecutionState
			for _, step := range model.Steps {
				requiresTable := step.Operation == "seed" || step.Operation == "reseed" || step.Operation == "changed" || step.Operation == "repair"
				require.False(t, requiresTable && state.Level == 0, "operation has no required table")
				operations[step.Operation] = true
				state.Advance(step)
				require.True(t, state.Level >= 0 && state.Level <= ExecutionMigrations, "state escaped its bound")
			}
		}
		require.Len(t, operations, executionOperationCount, "missing lifecycle coverage %s %v", engine, operations)
	}
	for _, data := range [][]byte{nil, {12}, {14}, {128}, make([]byte, ScenarioMaxBytes+1)} {
		_, ok := DecodeExecution("sqlite", data)
		require.False(t, ok, "incompatible input accepted")
	}
	_, ok := DecodeExecution("unknown", []byte{13})
	require.False(t, ok, "unknown engine accepted")
}

func TestExecutionModelPreservesSeedHistoryAcrossRollback(t *testing.T) {
	model, ok := DecodeExecution("sqlite", []byte{13, 0, 0, 4, 3, 0, 4, 5})
	require.True(t, ok, "model rejected")
	state := ExecutionState{}
	for index, step := range model.Steps {
		changed := state.Advance(step)
		switch index {
		case 1:
			require.True(t, changed == 1 && state.Seeded && state.SeedApplied, "seed was not modelled")
		case 2:
			require.True(t, changed == 3 && state.Level == 0 && !state.Seeded && state.SeedApplied, "rollback discarded seed history")
		case 4:
			require.True(t, changed == 0 && !state.Seeded, "previously applied seed ran again")
		case 5:
			require.True(t, changed == 1 && state.Seeded, "reseed did not restore data")
		}
	}
	rows := ExecutionRows(model, state, 1)
	require.Len(t, rows, 2, "wrong expected seeded rows")
	require.True(t, rows[0][0] == 7 && rows[1][0] == 8, "wrong expected seeded rows: %v", rows)
}

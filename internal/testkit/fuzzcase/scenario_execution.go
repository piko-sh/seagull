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
	"fmt"
	"strconv"
)

const (
	// ExecutionMigrations bounds the independent migration catalogue.
	ExecutionMigrations = 3

	// executionOperationCount freezes the lifecycle operation encoding.
	executionOperationCount = 12

	// executionStepBound limits each native workflow history.
	executionStepBound = 16
)

const (
	// executionUp fixes the up operation byte.
	executionUp = iota

	// executionUpTo fixes the upto operation byte.
	executionUpTo

	// executionDown fixes the down operation byte.
	executionDown

	// executionDownTo fixes the downto operation byte.
	executionDownTo

	// executionSeed fixes the seed operation byte.
	executionSeed

	// executionReseed fixes the reseed operation byte.
	executionReseed

	// executionStatus fixes the status operation byte.
	executionStatus

	// executionValidate fixes the validate operation byte.
	executionValidate

	// executionRestart fixes the restart operation byte.
	executionRestart

	// executionChanged fixes the changed operation byte.
	executionChanged

	// executionRepair fixes the repair operation byte.
	executionRepair

	// executionCancel fixes the cancel operation byte.
	executionCancel
)

const (
	// executionValueBias centres the generated integer range.
	executionValueBias = 128

	// executionSeedFirst identifies the first seeded row.
	executionSeedFirst = 7

	// executionSeedSecond identifies the second seeded row.
	executionSeedSecond = 8
)

// executionOperationNames freezes operation names independently of migration services.
var executionOperationNames = [executionOperationCount]string{"up", "up-to", "down", "down-to", "seed", "reseed", "status", "validate",
	"restart", "changed", "repair", "cancel"}

// executionNeedsTable marks the operations that need the first migration's table.
var executionNeedsTable = map[string]bool{"seed": true, "reseed": true, "changed": true, "repair": true}

// ExecutionCase models migration and seed history independently of their executors.
type ExecutionCase struct {
	// Engine selects native table and clearing syntax.
	Engine string

	// Steps supplies bounded lifecycle operations.
	Steps []ExecutionStep

	// Data retains the exact versioned replay input.
	Data []byte

	// Value gives each migration independently predictable data.
	Value int

	// Unwrapped requires explicit non-transactional migration execution.
	Unwrapped bool

	// Recovery checks interrupted migrations and seeds before the lifecycle.
	Recovery bool
}

// ExecutionStep selects one public migration or seed operation.
type ExecutionStep struct {
	// Operation names the public lifecycle action.
	Operation string

	// Target supplies a bounded version or rollback count.
	Target int
}

// ExecutionState predicts persisted history and table contents.
type ExecutionState struct {
	// Level counts the contiguous applied migration prefix.
	Level int

	// SeedApplied records the persistent seed history independently of tables.
	SeedApplied bool

	// Seeded records whether the first table currently contains seeded data.
	Seeded bool
}

// Advance predicts an operation's affected count and resulting persisted state.
//
// Takes step (ExecutionStep) which was decoded with valid bounded arguments.
//
// Returns int which counts migrations or seeds changed by the operation.
func (state *ExecutionState) Advance(step ExecutionStep) int {
	before := state.Level
	switch step.Operation {
	case "up":
		state.Level = ExecutionMigrations
	case "up-to":
		state.Level = max(state.Level, step.Target)
	case "down":
		state.Level = max(0, state.Level-step.Target)
	case "down-to":
		state.Level = min(state.Level, step.Target)
	case "seed":
		if state.SeedApplied {
			return 0
		}
		state.SeedApplied, state.Seeded = true, true
		return 1
	case "reseed":
		state.SeedApplied, state.Seeded = true, true
		return 1
	}
	if state.Level == 0 {
		state.Seeded = false
	}
	if state.Level < before {
		return before - state.Level
	}
	return state.Level - before
}

// Files builds independent migration and seed SQL with predictable row identities.
//
// Returns map[string][]byte which contains versioned migrations and one seed file.
func (model ExecutionCase) Files() map[string][]byte {
	files := make(map[string][]byte)
	for version := 1; version <= ExecutionMigrations; version++ {
		table := "fuzz_execution_" + strconv.Itoa(version)
		declaration := "(id BIGINT NOT NULL, value BIGINT NOT NULL)"
		if model.Engine == engineClickHouse {
			declaration = "(id Int64, value Int64) ENGINE = MergeTree ORDER BY id"
		}
		prefix := "-- Statement separators inside comments do not end a statement. ;\n"
		if model.Unwrapped {
			prefix += "-- sgl.migration(no_transaction: true)\n"
		}
		up := fmt.Sprintf("%sCREATE TABLE %s %s;\nINSERT INTO %s VALUES (1, %d);\n", prefix, table, declaration, table, model.Value+version)
		stem := fmt.Sprintf("migrations/%04d_step", version)
		files[stem+".up.sql"] = []byte(up)
		files[stem+".down.sql"] = []byte("DROP TABLE " + table + ";\n")
	}
	clearing := "DELETE FROM fuzz_execution_1;"
	if model.Engine == engineClickHouse {
		clearing = "TRUNCATE TABLE fuzz_execution_1;"
	}
	files["seeds/0001_rows.sql"] = fmt.Appendf(nil, "%s\nINSERT INTO fuzz_execution_1 VALUES (7, %d);\nINSERT INTO fuzz_execution_1 VALUES (8, %d);\n",
		clearing, model.Value+executionSeedFirst, model.Value+executionSeedSecond)
	return files
}

// DecodeExecution decodes bounded native migration and seed workflows.
//
// Takes engine (string) which selects independent native contracts.
// Takes data ([]byte) which contains version 13 and bounded operation choices.
//
// Returns ExecutionCase which supplies legal histories and predictable data.
// Returns bool which rejects incompatible envelopes or engines.
func DecodeExecution(engine string, data []byte) (ExecutionCase, bool) {
	if len(data) == 0 || len(data) > ScenarioMaxBytes || data[0] != ExecutionVersion || !supportsShape(engine, shapeSelect) {
		return ExecutionCase{}, false
	}
	model := ExecutionCase{Engine: engine, Steps: nil, Data: append([]byte(nil), data...), Value: 0, Unwrapped: false, Recovery: false}
	if len(data) > 1 {
		model.Value = int(data[1]) - executionValueBias
		model.Unwrapped = data[1]&1 != 0
		model.Recovery = data[1]&2 != 0
	}
	operations := data[min(2, len(data)):]
	if len(operations) == 0 {
		operations = []byte{executionUp, executionStatus, executionSeed, executionSeed, executionReseed, executionChanged, executionRepair, executionCancel,
			executionRestart, executionDownTo, executionUp, executionSeed, executionReseed, executionValidate}
	}
	operations = operations[:min(len(operations), executionStepBound)]
	state := ExecutionState{Level: 0, SeedApplied: false, Seeded: false}
	for _, choice := range operations {
		step := ExecutionStep{Operation: executionOperationNames[int(choice)%len(executionOperationNames)], Target: int(choice/executionOperationCount) % (ExecutionMigrations + 1)}
		if state.Level == 0 && executionNeedsTable[step.Operation] {
			prerequisite := ExecutionStep{Operation: "up-to", Target: 1}
			model.Steps = append(model.Steps, prerequisite)
			state.Advance(prerequisite)
		}
		model.Steps = append(model.Steps, step)
		state.Advance(step)
	}
	return model, true
}

// ExecutionSeeds covers every operation, restart and retained seed history.
//
// Returns [][]byte which provides deterministic lifecycle coverage before mutation.
func ExecutionSeeds() [][]byte {
	seeds := [][]byte{{ExecutionVersion}, {ExecutionVersion, 1}, {ExecutionVersion, 2}, {ExecutionVersion, 1 | 2}}
	for operation := range executionOperationCount {
		seeds = append(seeds, []byte{ExecutionVersion, 0, 0, byte(operation) + executionOperationCount, executionRestart, executionValidate})
	}
	return append(seeds, []byte{ExecutionVersion, 0, executionUp, executionSeed, executionDownTo, executionUp, executionSeed, executionReseed},
		[]byte{ExecutionVersion, 1, executionUpTo + executionOperationCount, executionUpTo + 2*executionOperationCount,
			executionUpTo + ExecutionMigrations*executionOperationCount, executionDown + executionOperationCount, executionDownTo + executionOperationCount,
			executionDownTo + 2*executionOperationCount, executionUp})
}

// ExecutionCapabilities records explicit migration lifecycle contracts.
//
// Takes engine (string) which selects native execution guarantees.
//
// Returns []Capability which records supported operations and partial execution limits.
func ExecutionCapabilities(engine string) []Capability {
	if !supportsShape(engine, shapeSelect) {
		return nil
	}
	var capabilities []Capability
	for _, operation := range executionOperationNames {
		capabilities = append(capabilities, Capability{Feature: "execution/" + operation, NativeType: "", CastType: "", Status: capabilitySupported, Reason: ""})
	}
	progress := Capability{Feature: "execution/partial-resume", NativeType: "", CastType: "", Status: capabilitySupported, Reason: ""}
	if engine == engineClickHouse {
		progress.Status, progress.Reason = capabilityUnsupportedNative, "The append-only ClickHouse backend records completed migrations without statement checkpoints."
	}
	capabilities = append(capabilities, progress)
	for _, contract := range []struct {
		name      string
		reason    string
		supported bool
	}{
		{name: "atomic-ddl", reason: "Native schema changes do not form an atomic transaction on this engine.", supported: ExecutionAtomicDDL(engine)},
		{name: "atomic-seeds", reason: "ClickHouse seed writes do not form an atomic transaction.", supported: engine != engineClickHouse},
		{name: "failure-retry", reason: "", supported: true},
	} {
		capability := Capability{Feature: "execution/" + contract.name, NativeType: "", CastType: "", Status: capabilitySupported, Reason: ""}
		if !contract.supported {
			capability.Status, capability.Reason = capabilityUnsupportedNative, contract.reason
		}
		capabilities = append(capabilities, capability)
	}
	return capabilities
}

// ExecutionRows predicts ordered native values without reading executor results.
//
// Takes model (ExecutionCase) which supplies independently generated data.
// Takes state (ExecutionState) which supplies the expected lifecycle state.
// Takes version (int) which selects one currently applied migration table.
//
// Returns [][2]int64 which retains exact ordered identities and values.
func ExecutionRows(model ExecutionCase, state ExecutionState, version int) [][2]int64 {
	if version > state.Level {
		return nil
	}
	if version == 1 && state.Seeded {
		return [][2]int64{{executionSeedFirst, int64(model.Value + executionSeedFirst)}, {executionSeedSecond, int64(model.Value + executionSeedSecond)}}
	}
	return [][2]int64{{1, int64(model.Value + version)}}
}

// ExecutionAtomicDDL records engines that can roll back this migration's DDL and data.
//
// Takes engine (string) which selects native transactional semantics.
//
// Returns bool which excludes implicit commits and non-transactional schema changes.
func ExecutionAtomicDDL(engine string) bool {
	switch engine {
	case enginePostgres, engineTimescale, engineSQLite, engineDuckDB:
		return true
	default:
		return false
	}
}

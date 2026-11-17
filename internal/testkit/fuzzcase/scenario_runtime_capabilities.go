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

// RuntimePrepared admits native drivers with general prepared-query support.
//
// Takes engine (string) which selects the independent native contract.
//
// Returns bool which excludes ClickHouse's insert-only preparation interface.
func RuntimePrepared(engine string) bool {
	return supportsShape(engine, shapeSelect) && engine != engineClickHouse
}

// RuntimeOperationCapabilities records every generated native operation contract.
//
// Takes engine (string) which selects native support independently of generation.
//
// Returns []Capability which records admitted operations and explicit limitations.
func RuntimeOperationCapabilities(engine string) []Capability {
	if !supportsShape(engine, shapeSelect) {
		return nil
	}
	var capabilities []Capability
	for _, operation := range []string{"insert", "plain", "stream", "early-stop", "builder", "count", "pagination", "reuse", "fresh",
		"prepared", "native-driver", "transactions", "cancelled-context"} {
		capability := Capability{Feature: "runtime/" + operation, NativeType: "", CastType: "", Status: capabilitySupported, Reason: ""}
		if operation == "prepared" && !RuntimePrepared(engine) {
			capability.Status = capabilityUnsupportedNative
			capability.Reason = "The ClickHouse database/sql driver prepares INSERT batches instead of general queries."
		}
		if operation == "native-driver" && !RuntimeNative(engine) {
			capability.Status, capability.Reason = capabilityUnsupportedNative, "This engine uses its database/sql driver API."
		}
		if operation == "transactions" && !RuntimeTransactions(engine) {
			capability.Status, capability.Reason = capabilityUnsupportedNative, "ClickHouse does not expose interactive transactions."
		}
		capabilities = append(capabilities, capability)
	}
	return capabilities
}

// RuntimeNative identifies engines with a dedicated generated driver API.
//
// Takes engine (string) which selects the native protocol.
//
// Returns bool which admits pgx and native ClickHouse generated methods.
func RuntimeNative(engine string) bool {
	switch engine {
	case enginePostgres, engineCockroach, engineTimescale, engineClickHouse:
		return true
	default:
		return false
	}
}

// RuntimeTransactions identifies engines with interactive transaction support.
//
// Takes engine (string) which selects the independent native contract.
//
// Returns bool which excludes ClickHouse's non-transactional query interface.
func RuntimeTransactions(engine string) bool { return RuntimePrepared(engine) }

// RuntimeTransactionValue selects the independent value inserted inside transactions.
//
// Takes model (RuntimeCase) which supplies the stable family and replay input.
//
// Returns TypedValue which includes nullable boundaries without reusing stored results.
func RuntimeTransactionValue(model RuntimeCase) TypedValue {
	choice := byte(0)
	if len(model.Data) > runtimeBoundaryByte {
		choice = model.Data[runtimeBoundaryByte] + 1
	}
	return scenarioValue(model.Kind, choice)
}

// runtimeOperationSeeds covers native drivers and transaction lifecycle boundaries.
//
// Takes engine (string) which selects supported operations.
// Takes seeds ([][]byte) which already contains ordinary and prepared boundaries.
//
// Returns [][]byte which preserves existing seeds and extends required coverage.
func runtimeOperationSeeds(engine string, seeds [][]byte) [][]byte {
	if RuntimeNative(engine) {
		seeds = append(seeds, runtimeNativeSeeds(engine, seeds)...)
	}
	if RuntimeTransactions(engine) {
		seeds = append(seeds, runtimeTransactionSeeds(engine)...)
	}
	return seeds
}

// runtimeNativeSeeds repeats ordinary boundaries through the dedicated native driver.
//
// Takes engine (string) which supplies independent native contracts.
// Takes seeds ([][]byte) which contains ordinary and prepared boundaries.
//
// Returns [][]byte which excludes unsupported explicit preparation combinations.
func runtimeNativeSeeds(engine string, seeds [][]byte) [][]byte {
	var result [][]byte
	for _, seed := range seeds {
		model, ok := DecodeRuntime(engine, seed)
		if !ok || model.Prepared {
			continue
		}
		native := make([]byte, max(len(seed), runtimeShapeByte+1))
		copy(native, seed)
		native[runtimeShapeByte] |= runtimeNativeFlag
		result = append(result, native)
	}
	return result
}

// runtimeTransactionSeeds covers each generated transaction binding mode.
//
// Takes engine (string) which supplies native operation contracts.
//
// Returns [][]byte which tests populated and initially empty transaction histories.
func runtimeTransactionSeeds(engine string) [][]byte {
	var seeds [][]byte
	modes := []byte{runtimeTransactionFlag, runtimeTransactionFlag | runtimeShapeCount}
	if RuntimeNative(engine) {
		modes = append(modes, runtimeTransactionFlag|runtimeNativeFlag)
	}
	for index, capability := range RuntimeCapabilities(engine) {
		if capability.Status != capabilitySupported {
			continue
		}
		for _, mode := range modes {
			seeds = append(seeds, []byte{RuntimeVersion, byte(index), 2, 1, mode}, []byte{RuntimeVersion, byte(index), 0, scenarioNullFlag, mode})
		}
	}
	return seeds
}

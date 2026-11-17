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
	"encoding/binary"
	"math"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRuntimeSeedsCoverEveryAdmittedFamilyAndShape(t *testing.T) {
	for _, engine := range []string{"postgres", "sqlite", "mysql", "mariadb", "duckdb", "clickhouse", "cockroachdb", "timescaledb"} {
		t.Run(engine, func(t *testing.T) {
			covered := make(map[string]map[int]bool)
			nulls, empty := make(map[string]bool), make(map[string]bool)
			for _, seed := range RuntimeShapeSeeds(engine) {
				model, ok := DecodeRuntime(engine, seed)
				require.True(t, ok, "seed rejected: %v", seed)
				if covered[model.Kind] == nil {
					covered[model.Kind] = make(map[int]bool)
				}
				for _, value := range model.Values {
					require.NoError(t, value.Validate())
					if value.Null {
						nulls[model.Kind] = true
					} else {
						covered[model.Kind][model.Shape] = true
					}
				}
				if len(model.Values) == 0 {
					empty[model.Kind] = true
				}
			}
			for _, capability := range RuntimeCapabilities(engine) {
				if capability.Status != "supported" {
					continue
				}
				kind := strings.TrimPrefix(capability.Feature, "type/")
				require.True(t, len(covered[kind]) == 4 && empty[kind], "missing populated shape or empty result coverage for %s", kind)
				if engine != "clickhouse" || kind != "array" {
					require.True(t, nulls[kind], "missing NULL coverage for %s", kind)
				}
			}
		})
	}
}

func TestRuntimePayloadsRemainBoundedAndExact(t *testing.T) {
	random := rand.New(rand.NewPCG(19, 31))
	for choice := range 256 {
		data := make([]byte, 1024)
		for index := range data {
			data[index] = byte(random.UintN(256))
		}
		data[0], data[1], data[2], data[3] = RuntimeVersion, byte(choice), 8, 0
		model, ok := DecodeRuntime("postgres", data)
		require.True(t, ok, "valid envelope rejected")
		for _, value := range model.Values {
			require.NoError(t, value.Validate(), "choice %d", choice)
		}
	}
	expected := map[uint64]string{0: "0", math.MaxInt64: "9223372036854775807", 1 << 63: "-9223372036854775808", math.MaxUint64: "-1"}
	for bits, want := range expected {
		data := make([]byte, 16)
		data[0], data[2] = RuntimeVersion, 1
		binary.LittleEndian.PutUint64(data[8:], bits)
		model, ok := DecodeRuntime("postgres", data)
		require.True(t, ok, "signed payload rejected")
		require.Len(t, model.Values, 1, "signed payload rejected")
		require.Equal(t, want, model.Values[0].Number, "signed bit pattern changed")
	}
	for _, data := range [][]byte{nil, {1}, {7}, {8}, {11}, {13}, {128}, make([]byte, ScenarioMaxBytes+1)} {
		_, ok := DecodeRuntime("postgres", data)
		require.False(t, ok, "incompatible envelope accepted")
	}
	_, ok := DecodeRuntime("unknown", []byte{12})
	require.False(t, ok, "unknown engine accepted")
}

func TestRuntimeSeedsReachEveryDriverAndTransactionMode(t *testing.T) {
	for _, engine := range []string{"postgres", "sqlite", "mysql", "mariadb", "duckdb", "clickhouse", "cockroachdb", "timescaledb"} {
		modes := make(map[[3]bool]bool)
		for _, data := range RuntimeSeeds(engine) {
			model, ok := DecodeRuntime(engine, data)
			require.True(t, ok, "seed rejected %s %v", engine, data)
			require.False(t, model.Native && model.Prepared, "native emitter has no explicit prepared constructor")
			if model.Transactions {
				require.NoError(t, RuntimeTransactionValue(model).Validate())
			}
			modes[[3]bool{model.Native, model.Prepared, model.Transactions}] = true
		}
		require.Len(t, modes, expectedRuntimeModes(engine), "%s reached the wrong number of modes", engine)
	}
}

func expectedRuntimeModes(engine string) int {
	expected := 1
	if RuntimePrepared(engine) {
		expected++
	}
	if RuntimeNative(engine) {
		expected++
	}
	if RuntimeTransactions(engine) {
		expected += 2
		if RuntimeNative(engine) {
			expected++
		}
	}
	return expected
}

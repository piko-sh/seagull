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
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var dmlEngines = []string{"postgres", "sqlite", "mysql", "mariadb", "duckdb", "clickhouse", "cockroachdb", "timescaledb"}

func TestDMLSeedsReachEveryAdmittedOperation(t *testing.T) {
	t.Parallel()
	for _, engine := range dmlEngines {
		reached := map[string]bool{}
		failures, nils, nulls := 0, 0, 0
		batchNils := map[string]bool{}
		for _, seed := range DMLSeeds(engine) {
			model, ok := DecodeDML(engine, seed)
			require.True(t, ok, "%s seed %v", engine, seed)
			for _, step := range model.Steps {
				reached[step.Operation] = true
				for _, inner := range step.Inner {
					reached[inner.Operation] = true
				}
				if step.Fails {
					failures++
				}
				for _, row := range step.Rows {
					if row.NilBytes {
						nils++
						batchNils[step.Operation] = true
					}
					if slices.ContainsFunc(row.Values, func(value TypedValue) bool { return value.Null }) {
						nulls++
					}
				}
			}
		}
		for _, capability := range DMLCapabilities(engine) {
			operation := strings.TrimPrefix(capability.Feature, "dml/")
			if capability.Status == capabilitySupported && operation != "affected-rows" {
				require.True(t, reached[operation], "%s seeds never reach %s", engine, operation)
			}
			if capability.Status != capabilitySupported {
				require.NotEmpty(t, capability.Reason, "%s excludes %s without a reason", engine, operation)
			}
		}
		require.Positive(t, nils, "%s seeds never send a nil slice", engine)
		for _, operation := range []string{DMLBatch, DMLCopy, DMLUpsertBatch} {
			if supported, _ := dmlSupported(engine, operation); supported {
				require.True(t, batchNils[operation], "%s seeds never send a nil slice through %s", engine, operation)
			}
		}
		require.Positive(t, nulls, "%s seeds never write NULL", engine)
		if engine != engineClickHouse {
			require.Positive(t, failures, "%s seeds never expect a refused statement", engine)
		}
	}
}

func TestDecodeDMLRejectsOtherVersionsAndBoundsItsSteps(t *testing.T) {
	t.Parallel()
	_, ok := DecodeDML("postgres", []byte{RuntimeVersion, 0, 0})
	require.False(t, ok)
	_, ok = DecodeDML("oracle", []byte{DMLVersion, 0, 0})
	require.False(t, ok)
	long := append([]byte{DMLVersion, 0}, slices.Repeat([]byte{0, 1, 2, 3}, 100)...)
	model, ok := DecodeDML("postgres", long)
	require.True(t, ok)
	require.Len(t, model.Steps, dmlStepBound)
}

func TestDMLModelFollowsEachEngineCountContract(t *testing.T) {
	t.Parallel()
	update := byte(slices.Index(dmlOperations, DMLUpdate))
	upsert := byte(slices.Index(dmlOperations, DMLUpsert))

	data := []byte{DMLVersion, 0, 0, 0, 0, 0, update, 0, 0, upsert, 0, 0, 0}
	for engine, want := range map[string][]int64{"postgres": {1, 1, 1}, "mysql": {1, 0, 0}, "mariadb": {1, 0, 0}, "sqlite": {1, 1, 1}} {
		model, ok := DecodeDML(engine, data)
		require.True(t, ok)
		require.Len(t, model.Steps, 3)
		require.Equal(t, want, []int64{model.Steps[0].Affected, model.Steps[1].Affected, model.Steps[2].Affected}, engine)
	}
	clickhouse, ok := DecodeDML(engineClickHouse, data)
	require.True(t, ok)
	for _, step := range clickhouse.Steps {
		require.False(t, step.HasAffected, "ClickHouse reports no affected rows")
	}
}

func TestDMLModelLeavesTheTableUnchangedByARefusedOrRolledBackStatement(t *testing.T) {
	t.Parallel()
	pair := byte(slices.Index(dmlOperations, DMLInsertPair))
	transaction := byte(slices.Index(dmlOperations, DMLTransaction))
	data := []byte{DMLVersion, 0, 0, 0, 0, 0, pair, 1, 0, 0, 0, 0, 0, transaction, 0, 0, 0, 2, 0, 0}
	model, ok := DecodeDML("postgres", data)
	require.True(t, ok)
	require.Len(t, model.Steps, 3)
	require.True(t, model.Steps[1].Fails, "a pair repeating an existing key fails whole")
	require.Len(t, model.Steps[1].State, 1)
	require.False(t, model.Steps[2].Commit)
	require.Len(t, model.Steps[2].Inner, 1)
	require.Len(t, model.Steps[2].Inner[0].State, 2, "the transaction sees its own insert")
	require.Len(t, model.Steps[2].State, 1, "rolling back discards the insert")
}

func TestDMLQueriesUseOnlyAdmittedOperations(t *testing.T) {
	t.Parallel()
	for _, engine := range dmlEngines {
		queries := DMLQueries(engine)
		for _, operation := range dmlOperations {
			supported, _ := dmlSupported(engine, operation)
			if operation == DMLTransaction {
				continue
			}
			require.Equal(t, supported, strings.Contains(queries, "sgl.query("+DMLMethod(operation)+","), "%s %s", engine, operation)
		}
		require.Contains(t, queries, "sgl.query("+DMLRowsMethod+", many)")
		require.Len(t, DMLSchema(engine), 1)
	}
}

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

package harness

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

// historyTerminator separates replayed DDL statements.
const historyTerminator = ";"

// FuzzSchemaEquivalence checks populated schema histories against direct declarations.
//
// Takes f (*testing.F) which registers and runs independent histories.
// Takes suite (FuzzSuite) which supplies an isolated native database.
func FuzzSchemaEquivalence(f *testing.F, suite FuzzSuite) {
	f.Helper()
	for _, seed := range fuzzcase.HistorySeeds(suite.Engine) {
		f.Add(seed)
	}
	var metadata sync.Once
	f.Fuzz(func(t *testing.T, data []byte) {
		history, ok := fuzzcase.DecodeHistory(suite.Engine, data)
		if !ok {
			t.Skip("unsupported history envelope")
		}
		runFuzzSchemaHistory(t, suite, history, &metadata)
	})
}

// runFuzzSchemaHistory observes every prefix and compares the final data and schema.
//
// Takes t (*testing.T) which reports failed oracle checks.
// Takes suite (FuzzSuite) which supplies the dialect and isolated native database.
// Takes history (fuzzcase.SchemaHistory) which pairs the direct schema with its populated
// alternative.
// Takes metadata (*sync.Once) which records native engine metadata once.
//
// Panics if native work ignores the cancellation deadline.
func runFuzzSchemaHistory(t *testing.T, suite FuzzSuite, history fuzzcase.SchemaHistory, metadata *sync.Once) {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(t.Context(), scaled(databaseInputDeadline), errFuzzInputDeadline)
	defer cancel()
	watchdog := time.AfterFunc(scaled(databaseWatchdogDeadline), func() { panic("schema history ignored cancellation: " + t.Name()) })
	defer watchdog.Stop()
	direct := suite.Open(t)
	direct.SetMaxOpenConns(1)
	metadata.Do(func() { writeFuzzEngineMetadata(ctx, t, direct, suite.Engine) })
	fuzzApply(ctx, t, direct, history.Direct.Schema)
	fuzzApply(ctx, t, direct, history.Direct.Inserts)
	checkFuzzMigrationSequence(ctx, t, direct, suite, history.Direct.Schema)
	expectedSchema := observeFuzzMigration(ctx, t, direct, suite)
	expectedRows := observeFuzzScenario(ctx, t, direct, suite, OracleTypes, history.Direct)
	require.NoError(t, direct.Close())
	alternative := suite.Open(t)
	alternative.SetMaxOpenConns(1)
	fuzzApply(ctx, t, alternative, history.Initial)
	fuzzApply(ctx, t, alternative, history.Inserts)
	applied := make([]string, len(history.Initial), len(history.Initial)+len(history.Steps))
	for index, statement := range history.Initial {
		applied[index] = strings.TrimSuffix(statement, historyTerminator) + historyTerminator
	}
	checkFuzzMigrationSequence(ctx, t, alternative, suite, applied)
	for _, statement := range history.Steps {
		fuzzExec(ctx, t, alternative, statement, nil)
		applied = append(applied, strings.TrimSuffix(statement, historyTerminator)+historyTerminator)
		checkFuzzMigrationSequence(ctx, t, alternative, suite, applied)
	}
	observedSchema := observeFuzzMigration(ctx, t, alternative, suite)
	checkHistoryMetadata(t, expectedSchema, observedSchema)
	scenario := history.Direct
	scenario.Schema = applied
	observedRows := observeFuzzScenario(ctx, t, alternative, suite, OracleTypes, scenario)
	require.Len(t, observedRows, len(expectedRows), "schema history changed the number of results")
	for index, rows := range expectedRows {
		require.True(t, fuzzExactResultsEqual(rows, observedRows[index], true),
			"schema history changed data for %s input %v: expected %#v, observed %#v", history.Feature, history.Direct.Data, rows, observedRows[index])
	}
}

// checkHistoryMetadata compares equivalent native schemas without origin information.
//
// Takes t (*testing.T) which reports failed oracle checks.
// Takes expected (map[string]fuzzSchemaRelation) which holds independently expected
// observations.
// Takes observed (map[string]fuzzSchemaRelation) which holds the actual native
// observation.
func checkHistoryMetadata(t *testing.T, expected, observed map[string]fuzzSchemaRelation) {
	t.Helper()
	for _, problem := range fuzzSchemaProblems(expected, observed) {
		assert.Fail(t, problem)
	}
	for name, relation := range expected {
		actual, ok := observed[name]
		if !ok || len(relation.columns) != len(actual.columns) {
			continue
		}
		for index := range relation.columns {
			column := &relation.columns[index]
			assert.True(t, strings.EqualFold(column.typeName, actual.columns[index].typeName),
				"history changed native type for %s.%s from %s to %s", name, column.name, column.typeName, actual.columns[index].typeName)
		}
	}
}

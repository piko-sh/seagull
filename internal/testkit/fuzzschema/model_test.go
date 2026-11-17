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

package fuzzschema

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/testkit/fuzzbuilder"
	"seagull.sh/seagull/internal/testkit/fuzzcase"
	"seagull.sh/seagull/internal/testkit/fuzztyped"
)

var testEngines = []string{enginePostgres, engineCockroachDB, engineTimescale, engineMySQL, engineMariaDB, engineSQLite, engineDuckDB, engineClickHouse}

func randomInputs(seed uint64, count int) [][]byte {
	random := rand.New(rand.NewPCG(seed, SchemaHistoryVersion))
	inputs := make([][]byte, 0, count)
	for range count {
		data := make([]byte, 1+random.IntN(160))
		data[0] = SchemaHistoryVersion
		for index := 1; index < len(data); index++ {
			data[index] = byte(random.UintN(256))
		}
		inputs = append(inputs, data)
	}
	return inputs
}

func TestDecodeRejectsOtherEnvelopes(t *testing.T) {
	t.Parallel()
	for _, data := range [][]byte{nil, {}, {17}, {19, 1, 2}, {fuzzcase.HistoryVersion}, make([]byte, MaxBytes+1)} {
		_, ok := Decode(enginePostgres, data)
		assert.False(t, ok, "decoded %v", data)
	}
	_, ok := Decode("oracle", []byte{SchemaHistoryVersion})
	assert.False(t, ok, "decoded an unmodelled engine")
	assert.Nil(t, Seeds("oracle"))
	assert.Nil(t, Capabilities("oracle"))
}

func TestOtherDecodersRejectVersion18(t *testing.T) {
	t.Parallel()
	inputs := append(randomInputs(1, 256), []byte{SchemaHistoryVersion})
	for _, engine := range testEngines {
		for _, data := range inputs {
			rejected := map[string]bool{}
			_, rejected["fuzzcase.Decode"] = fuzzcase.Decode(engine, data)
			_, rejected["fuzzcase.DecodeMigration"] = fuzzcase.DecodeMigration(engine, data)
			_, rejected["fuzzcase.DecodeRejection"] = fuzzcase.DecodeRejection(engine, data)
			_, rejected["fuzzcase.DecodeScenario"] = fuzzcase.DecodeScenario(engine, data)
			_, rejected["fuzzcase.DecodeHistory"] = fuzzcase.DecodeHistory(engine, data)
			_, rejected["fuzzcase.DecodeCommands"] = fuzzcase.DecodeCommands(engine, data)
			_, rejected["fuzzcase.DecodeRuntime"] = fuzzcase.DecodeRuntime(engine, data)
			_, rejected["fuzzcase.DecodeExecution"] = fuzzcase.DecodeExecution(engine, data)
			_, rejected["fuzzcase.DecodeLifecycle"] = fuzzcase.DecodeLifecycle(engine, data)
			_, rejected["fuzztyped.Decode"] = fuzztyped.Decode(engine, data)
			_, rejected["fuzzbuilder.Decode"] = fuzzbuilder.Decode(engine, data)
			for decoder, accepted := range rejected {
				require.False(t, accepted, "%s accepted a version 18 input on %s: %v", decoder, engine, data)
			}
		}
	}
}

func TestSeedsCoverEverySupportedFeature(t *testing.T) {
	t.Parallel()
	for _, engine := range testEngines {
		covered := map[string]bool{}
		for _, seed := range Seeds(engine) {
			model, ok := Decode(engine, seed)
			require.True(t, ok, "%s rejected seed %v", engine, seed)
			for _, feature := range model.Features {
				covered[feature] = true
			}
		}
		for _, capability := range Capabilities(engine) {
			supported := capability.Status == statusSupported
			assert.Equal(t, supported, covered[capability.Feature], "%s seeds and %s, which is %s: %s", engine, capability.Feature, capability.Status,
				capability.Reason)
			assert.True(t, supported || capability.Reason != "", "%s leaves out %s without a reason", engine, capability.Feature)
		}
	}
}

func TestDecodeIsDeterministicAndConsistent(t *testing.T) {
	t.Parallel()
	for _, engine := range testEngines {
		for _, data := range randomInputs(2, 400) {
			first, ok := Decode(engine, data)
			if !ok {
				continue
			}
			second, ok := Decode(engine, data)
			require.True(t, ok)
			require.Equal(t, first.Describe(), second.Describe(), "%s decoded %v two ways", engine, data)
			require.Equal(t, first.Features, second.Features, "%s decoded %v two ways", engine, data)
			checkSchemaInvariants(t, engine, &first)
		}
	}
}

func checkSchemaInvariants(t *testing.T, engine string, model *Case) {
	t.Helper()
	keys := map[string]*Relation{}
	for index := range model.Final.Relations {
		relation := &model.Final.Relations[index]
		require.Nil(t, keys[relation.Key()], "%s has two relations %s:\n%s", engine, relation.Key(), model.Describe())
		keys[relation.Key()] = relation
		names := map[string]bool{}
		for _, column := range relation.Columns {
			require.False(t, names[column.Name], "%s relation %s repeats column %s:\n%s", engine, relation.Key(), column.Name, model.Describe())
			names[column.Name] = true
			require.Contains(t, families(engine), column.Family, "%s uses an unsupported family:\n%s", engine, model.Describe())
		}
		for _, key := range relation.ForeignKeys {
			for _, name := range key.Columns {
				require.True(t, names[name], "%s foreign key of %s names missing column %s:\n%s", engine, relation.Key(), name, model.Describe())
			}
		}
	}
	for index := range model.Final.Relations {
		for _, key := range model.Final.Relations[index].ForeignKeys {
			parent := keys[key.Table]
			require.NotNil(t, parent, "%s foreign key references missing %s:\n%s", engine, key.Table, model.Describe())
			require.Equal(t, []string{"id"}, parent.PrimaryKey, "%s foreign key references %s, which has no key on id:\n%s", engine, key.Table,
				model.Describe())
		}
	}
	files := model.Files()
	require.Len(t, files, len(model.Migrations), "%s wrote one file per migration", engine)
	for name, content := range files {
		require.True(t, strings.HasPrefix(name, "migrations/") && strings.HasSuffix(name, ".up.sql"), "%s wrote file %s", engine, name)
		require.NotEmpty(t, content, "%s wrote an empty file %s", engine, name)
	}
}

func TestEngineSpellings(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		enginePostgres:    "CREATE TABLE fuzz_a (id BIGINT NOT NULL",
		engineMySQL:       "CREATE TABLE fuzz_a (id BIGINT NOT NULL",
		engineSQLite:      "CREATE TABLE fuzz_a (id INTEGER NOT NULL",
		engineClickHouse:  "CREATE TABLE fuzz_a (id Int64",
		engineDuckDB:      "CREATE TABLE fuzz_a (id BIGINT NOT NULL",
		engineCockroachDB: "CREATE TABLE fuzz_a (id BIGINT NOT NULL",
	}
	for engine, prefix := range cases {
		model, ok := Decode(engine, []byte{SchemaHistoryVersion, 0, 0, 0, 0, 0, 0, 0, 0})
		require.True(t, ok)
		assert.True(t, strings.HasPrefix(model.Migrations[0].Statements[0], prefix), "%s spelled %+v", engine, model.Migrations)
	}
	model, ok := Decode(engineClickHouse, []byte{SchemaHistoryVersion, 0, 0, 0, 0, 0, 0, 0, 0})
	require.True(t, ok)
	assert.True(t, strings.HasSuffix(model.Migrations[0].Statements[0], "ENGINE = MergeTree ORDER BY id"), "ClickHouse table without its engine")
	assert.Equal(t, []string{"id"}, model.Final.Relations[0].PrimaryKey, "ClickHouse sorting key is not the primary key")
}

func TestModelFollowsRedefinitionAndPartitions(t *testing.T) {
	t.Parallel()

	state := newHistory(engineMySQL, nil)
	state.buildTable("", "fuzz_a", choice{table: 0, column: 0, variant: 0, flags: 0x02, extra: 0}, false)
	statements := operationAlterType(state, choice{table: 0, column: 0, variant: 0, flags: 0, extra: 0})
	member := state.relations[0].columns[1]
	require.Len(t, statements, 1)
	require.Contains(t, statements[0], "MODIFY COLUMN c1 VARCHAR(64) NULL")
	require.False(t, member.hasDefault)
	require.True(t, member.nullable)

	state = newHistory(enginePostgres, nil)
	state.buildTable("", tablePartitioned, choice{table: 0, column: 0x30, variant: 4, flags: 0, extra: 0}, true)
	operationCheck(state, choice{table: 0, column: 0, variant: 0, flags: 0, extra: 0})
	schema := state.final()
	require.Len(t, schema.Relations, 2)
	require.Equal(t, tablePartitioned, schema.Relations[1].PartitionOf)
	require.Len(t, schema.Relations[1].Checks, 2)
	require.Contains(t, state.detach(state.relations[0])[0], "DETACH PARTITION fuzz_p_0")
	schema = state.final()
	require.Len(t, schema.Relations, 2)
	require.Empty(t, schema.Relations[1].PartitionOf)
	require.Equal(t, []string{"id"}, schema.Relations[1].PrimaryKey)
}

func TestRegressionMarkerIsIgnored(t *testing.T) {
	t.Parallel()
	for _, engine := range testEngines {
		for _, data := range randomInputs(3, 100) {
			plain, ok := Decode(engine, data)
			marked, markedOK := Decode(engine, append(slices.Clone(data), RegressionMarker...))
			require.Equal(t, ok, markedOK, "%s decodes %v only with or without the marker", engine, data)
			require.Equal(t, plain.Describe(), marked.Describe(), "%s marker changed %v", engine, data)
		}
	}
}

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

//go:build integration

package clickhouse_test

import (
	"encoding/json"
	"io/fs"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_clickhouse"
	"seagull.sh/seagull/sdk/emit"
	"seagull.sh/seagull/sdk/emit/emit_driver_clickhouse"
	"seagull.sh/seagull/sdk/emit/emit_driver_databasesql"
	"seagull.sh/seagull/tests/integration/internal/harness"
)

const nestedTuplesSchema = `CREATE TABLE nests (
    id UInt16,
    pairs_in_pair Tuple(UInt8, Tuple(String, UInt16)),
    fields_in_pair Tuple(UInt8, Tuple(s String, n UInt16)),
    pair_in_fields Tuple(a UInt8, b Tuple(String, UInt16)),
    fields_in_fields Tuple(a UInt8, b Tuple(s String, n UInt16)),
    listed Array(Tuple(UInt8, Tuple(String, UInt16))),
    keyed Map(String, Tuple(UInt8, Tuple(String, UInt16))),
    pair_list_in_pair Tuple(UInt8, Array(Tuple(String, UInt16))),
    keyed_pairs_in_pair Tuple(UInt8, Map(String, Tuple(String, UInt16))),
    pair_list_in_fields Tuple(a UInt8, b Array(Tuple(String, UInt16))),
    keyed_pairs_in_fields Tuple(a UInt8, b Map(String, Tuple(String, UInt16))),
    listed_fields Array(Tuple(a UInt8, b Tuple(String, UInt16))),
    pair_in_fields_in_pair Tuple(UInt8, Tuple(s String, n Tuple(UInt8, String))),
    pair_in_fields_in_fields Tuple(a UInt8, b Tuple(c UInt8, d Tuple(String, UInt16))),
    listed_lists Array(Array(Tuple(UInt8, String))),
    field_list_in_pair Tuple(UInt8, Array(Tuple(s String, n UInt16))),
    keyed_lists Map(String, Array(Tuple(UInt8, String))),
    numbers_in_pair Tuple(UInt8, Array(UInt16), Nullable(String))
) ENGINE = MergeTree ORDER BY id`

const nestedTuplesRow = `INSERT INTO nests VALUES (1, (1, ('x', 2)), (1, ('x', 2)), (1, ('x', 2)), (1, ('x', 2)),
    [(1, ('x', 2))], {'k': (1, ('x', 2))}, (1, [('x', 2)]), (1, {'k': ('x', 2)}), (1, [('x', 2)]), (1, {'k': ('x', 2)}),
    [(1, ('x', 2))], (1, ('x', (3, 'y'))), (1, (2, ('x', 3))), [[(1, 'x')]], (1, [('x', 2)]), {'k': [(1, 'x')]},
    (1, [3, 4], NULL))`

type nestedPairInFields struct {
	A uint8 `json:"a"`
	B []any `json:"b"`
}

type nestedFieldsInner struct {
	S string `json:"s"`
	N uint16 `json:"n"`
}

type nestedFieldsInFields struct {
	A uint8             `json:"a"`
	B nestedFieldsInner `json:"b"`
}

type nestedPairListInFields struct {
	A uint8   `json:"a"`
	B [][]any `json:"b"`
}

type nestedKeyedPairsInFields struct {
	A uint8            `json:"a"`
	B map[string][]any `json:"b"`
}

type nestedPairInFieldsInner struct {
	C uint8 `json:"c"`
	D []any `json:"d"`
}

type nestedPairInFieldsInFields struct {
	A uint8                   `json:"a"`
	B nestedPairInFieldsInner `json:"b"`
}

func TestNestedUnnamedTuplesAreRefusedWhereNoDriverReadsThem(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	standard, dsn := harness.IsolatedDatabase(t, "clickhouse", testConnectionString)
	options, err := clickhouse.ParseDSN(dsn)
	require.NoError(t, err)
	native, err := clickhouse.Open(options)
	require.NoError(t, err)
	t.Cleanup(func() { _ = native.Close() })
	for _, statement := range []string{nestedTuplesSchema, nestedTuplesRow} {
		require.NoError(t, native.Exec(t.Context(), statement))
	}

	slice := func() any { return new([]any) }
	testCases := []struct {
		name          string
		value         string
		destination   func() any
		refused       bool
		nativeReads   bool
		standardReads bool
	}{
		{name: "an unnamed tuple inside an unnamed tuple", value: "pairs_in_pair", destination: slice, refused: true},
		{name: "a named tuple inside an unnamed tuple", value: "fields_in_pair", destination: slice, nativeReads: true, standardReads: true},
		{name: "an unnamed tuple inside a named tuple", value: "pair_in_fields", destination: func() any { return new(nestedPairInFields) }, refused: true},
		{name: "a named tuple inside a named tuple", value: "fields_in_fields", destination: func() any { return new(nestedFieldsInFields) }, nativeReads: true, standardReads: true},
		{name: "an array of unnamed tuples holding one", value: "listed", destination: func() any { return new([][]any) }, refused: true},
		{name: "a map to unnamed tuples holding one", value: "keyed", destination: func() any { return new(map[string][]any) }, refused: true},
		{name: "an array of unnamed tuples inside an unnamed tuple", value: "pair_list_in_pair", destination: slice, refused: true},
		{name: "a map to unnamed tuples inside an unnamed tuple", value: "keyed_pairs_in_pair", destination: slice, nativeReads: true, standardReads: true},
		{name: "an array of unnamed tuples inside a named tuple", value: "pair_list_in_fields", destination: func() any { return new(nestedPairListInFields) }, nativeReads: true},
		{name: "a map to unnamed tuples inside a named tuple", value: "keyed_pairs_in_fields", destination: func() any { return new(nestedKeyedPairsInFields) }, nativeReads: true, standardReads: true},
		{name: "an array of named tuples holding an unnamed one", value: "listed_fields", destination: func() any { return new([]nestedPairInFields) }, refused: true},
		{name: "an unnamed tuple inside a named one inside an unnamed one", value: "pair_in_fields_in_pair", destination: slice, refused: true},
		{name: "an unnamed tuple inside a named one inside a named one", value: "pair_in_fields_in_fields", destination: func() any { return new(nestedPairInFieldsInFields) }, refused: true},
		{name: "an array of arrays of unnamed tuples", value: "listed_lists", destination: func() any { return new([][][]any) }, nativeReads: true, standardReads: true},
		{name: "an array of named tuples inside an unnamed tuple", value: "field_list_in_pair", destination: slice, nativeReads: true, standardReads: true},
		{name: "a map to arrays of unnamed tuples", value: "keyed_lists", destination: func() any { return new(map[string][][]any) }, nativeReads: true, standardReads: true},
		{name: "plain values inside an unnamed tuple", value: "numbers_in_pair", destination: slice, nativeReads: true, standardReads: true},
		{name: "a built tuple inside a built tuple", value: "(id, (id, 'gull'))", destination: slice, refused: true},
		{name: "a nested tuple cast to named elements", value: "CAST(pairs_in_pair AS Tuple(UInt8, Tuple(s String, n UInt16)))", destination: slice, nativeReads: true, standardReads: true},
		{name: "an element of a nested tuple", value: "pairs_in_pair.2.1", destination: func() any { return new(string) }, nativeReads: true, standardReads: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var want string
			require.NoError(t, standard.QueryRowContext(t.Context(), "SELECT toJSONString("+testCase.value+") FROM nests").Scan(&want))
			query := "SELECT " + testCase.value + " AS value FROM nests"

			nativeReads := readsNestedValue(want, func(destination any) error {
				return native.QueryRow(t.Context(), query).Scan(destination)
			}, testCase.destination)
			standardReads := readsNestedValue(want, func(destination any) error {
				return standard.QueryRowContext(t.Context(), query).Scan(destination)
			}, func() any { return new(any) })

			require.Equal(t, testCase.nativeReads, nativeReads, "native driver")
			require.Equal(t, testCase.standardReads, standardReads, "database/sql")
			require.Equal(t, testCase.refused, refusesNestedValue(t, query, emit_driver_clickhouse.New()))
			require.Equal(t, testCase.refused, !nativeReads && !standardReads)
			require.Equal(t, !standardReads, refusesNestedValue(t, query, emit_driver_databasesql.New()))
		})
	}
}

func readsNestedValue(want string, scan func(destination any) error, destination func() any) (reads bool) {
	defer func() {
		if recover() != nil {
			reads = false
		}
	}()
	value := destination()
	if err := scan(value); err != nil {
		return false
	}
	return sameJSON(want, value)
}

func sameJSON(want string, value any) bool {
	encoded, err := json.Marshal(value)
	if err != nil {
		return false
	}
	var wanted, got any
	if json.Unmarshal([]byte(want), &wanted) != nil || json.Unmarshal(encoded, &got) != nil {
		return false
	}
	return reflect.DeepEqual(wanted, got)
}

func refusesNestedValue(t *testing.T, query string, emitter emit.EmitterPort) bool {
	t.Helper()

	generator, err := seagull.New(dialect_engine_clickhouse.New(), seagull.WithEmitter(emitter))
	require.NoError(t, err)
	files := fstest.MapFS{
		"migrations":                  &fstest.MapFile{Mode: fs.ModeDir},
		"migrations/001_nests.up.sql": &fstest.MapFile{Data: []byte(nestedTuplesSchema + ";\n")},
		"queries/nests.sql":           &fstest.MapFile{Data: []byte("-- sgl.query(Probe, many)\n" + query + ";\n")},
	}
	result, err := generator.Generate(t.Context(), seagull.Input{Package: "db", Migrations: files, MigrationDir: "migrations", QueryDir: "queries"})
	require.NoError(t, err)
	refused := false
	for _, diagnostic := range result.Diagnostics {
		require.Equal(t, seagull.CodeEmitterUnsupported, diagnostic.Code, diagnostic.Message)
		refused = true
	}
	return refused
}

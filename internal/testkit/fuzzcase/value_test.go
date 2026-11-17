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
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExactValueKindsAndLiteralEncodings(t *testing.T) {
	binary := Value{Kind: valueBinary, Text: string([]byte{0, 255, 254})}
	require.Equal(t, []byte{0, 255, 254}, binary.DriverValue("sqlite"), "driver binding changed exact bytes")
	require.Equal(t, string([]byte{0, 255, 254}), binary.DriverValue("clickhouse"), "driver binding changed exact bytes")
	expected := map[string]string{
		"sqlite": "X'00fffe'", "mysql": "X'00fffe'", "mariadb": "X'00fffe'", "postgres": "decode('00fffe', 'hex')",
		"cockroachdb": "decode('00fffe', 'hex')", "timescaledb": "decode('00fffe', 'hex')", "duckdb": "from_hex('00fffe')", "clickhouse": "unhex('00fffe')",
	}
	for engine, literal := range expected {
		require.Equal(t, literal, binary.literal(engine), "changed binary reference for %s", engine)
		text := Value{Kind: valueText, Text: "NULL"}
		null := Value{Kind: valueNull}
		require.NotNil(t, text.DriverValue(engine), "text and SQL NULL became indistinguishable")
		require.Nil(t, null.DriverValue(engine), "text and SQL NULL became indistinguishable")
		require.Equal(t, "'NULL'", text.literal(engine), "text and SQL NULL became indistinguishable")
		require.Equal(t, "NULL", null.literal(engine), "text and SQL NULL became indistinguishable")
	}
	text := Value{Kind: valueText, Text: "a'b"}
	require.Equal(t, "'a''b'", text.literal("postgres"), "text reference lost quote escaping")
	require.Equal(t, "-9223372036854775807", (Value{Integer: -9223372036854775807}).literal("sqlite"), "integer reference lost precision")
}

func TestValueModelBoundsIdentitiesAndStableFamilies(t *testing.T) {
	require.Equal(t, []int{5, 28, 29, 30}, []int{valueVersion, shapeExactValues, shapeNullableValues, shapeSQLiteAffinity}, "changed a stable value family")
	for engine := range capabilities {
		for _, seed := range valueSeeds(engine) {
			model, ok := Decode(engine, seed)
			require.True(t, ok, "rejected value seed %s %v", engine, seed)
			query := model.Query()
			wantOccurrences := 2
			wantBindings := 2
			if model.Shape == shapeNullableValues {
				wantOccurrences = 4
			}
			if model.Shape == shapeSQLiteAffinity {
				wantOccurrences, wantBindings = 1, 1
			}
			require.Len(t, query.Values, wantBindings, "value model lost a binding identity: %+v", model)
			require.Len(t, query.Occurrences, wantOccurrences, "value model lost a binding identity: %+v", model)
			require.Len(t, model.Migrations(), 3, "value model lost a migration: %+v", model)
			require.LessOrEqual(t, model.Rows, 8, "value model exceeded its row bound: %+v", model)
			if model.Shape == shapeNullableValues {
				require.Equal(t, query.Occurrences[0], query.Occurrences[1], "repeated logical parameters changed value")
				require.Equal(t, query.Occurrences[2], query.Occurrences[3], "repeated logical parameters changed value")
			}
			require.Equal(t, query, model.Query(), "value query changed between calls")
			for _, value := range query.Values {
				require.False(t, value.Kind == valueBinary && len(value.Text) > 16, "binary value exceeded its bound")
			}
		}
	}
	_, ok := Decode("postgres", []byte{5, 2})
	require.False(t, ok, "SQLite affinity escaped its capability restriction")
	longest := bytes.Repeat([]byte{255}, MaxBytes)
	longest[0] = valueVersion
	model, ok := Decode("sqlite", longest)
	require.True(t, ok, "rejected a maximum-size value model")
	require.LessOrEqual(t, len(model.Query().Values), 8, "maximum-size value model exceeded its bounds")
	_, ok = Decode("sqlite", append(longest, 0))
	require.False(t, ok, "accepted an oversized value model")
}

func TestSQLiteAffinitySeparatesDeclaredTypeAndRuntimeStorage(t *testing.T) {
	model, ok := Decode("sqlite", []byte{5, 2, 0, 8})
	require.True(t, ok && model.SQLiteAffinity(), "SQLite affinity model was rejected")
	want := []string{"integer", "text", "blob", "null", "integer", "text", "integer", "blob"}
	require.Equal(t, want, model.SQLiteStorageClasses(), "changed independent SQLite storage expectations")
	require.Len(t, model.valueInserts(), 8, "affinity model lost heterogeneous rows")
}

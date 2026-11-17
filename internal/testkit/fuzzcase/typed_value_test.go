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
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTypedValuesPreserveRepresentations(t *testing.T) {
	values := make([]TypedValue, 0, 9)
	values = append(values, TypedNumber("signed", "-9223372036854775808", 0), TypedNumber("unsigned", "18446744073709551615", 0),
		TypedNumber("decimal", "-9223372036854775808000001", 6))
	for _, bits := range []uint64{0, 1, 1 << 63, math.Float64bits(math.Inf(1)), 0x7ff8000000000042} {
		value := TypedNumber("float", "", 0)
		value.Bits = bits
		values = append(values, value)
	}
	binary := TypedNumber("binary", "", 0)
	binary.Bytes = []byte{0, 255, 128, 39, 92}
	values = append(values, binary)
	for _, original := range values {
		require.NoError(t, original.Validate())
		encoded, err := json.Marshal(original)
		require.NoError(t, err)
		var decoded TypedValue
		require.NoError(t, json.Unmarshal(encoded, &decoded))
		require.Equal(t, original, decoded, "value changed during transport")
	}
	require.Equal(t, "-9223372036854775808.000001", values[2].Decimal(), "decimal changed")
}

func TestTypedValuesRejectMalformedRepresentations(t *testing.T) {
	for _, value := range []TypedValue{TypedNumber("signed", "9223372036854775808", 0), TypedNumber("unsigned", "-1", 0),
		TypedNumber("decimal", "1.2", 0), TypedNumber("decimal", "12", 39), TypedNumber("boolean", "2", 0), TypedNumber("unknown", "", 0)} {
		require.Error(t, value.Validate(), "accepted malformed value: %#v", value)
	}
	unknown := TypedNumber("unknown", "", 0)
	unknown.Null = true
	require.Error(t, unknown.Validate(), "NULL concealed an unknown type")
}

func TestScenarioSeedsReachEveryDeclaredType(t *testing.T) {
	for _, engine := range []string{"postgres", "sqlite", "mysql", "mariadb", "duckdb", "clickhouse", "cockroachdb", "timescaledb"} {
		covered := make(map[string]bool)
		for _, seed := range ScenarioSeeds(engine) {
			scenario, ok := DecodeScenario(engine, seed)
			require.True(t, ok, "invalid scenario seed for %s: %v", engine, seed)
			require.NotEmpty(t, scenario.Queries, "invalid scenario seed for %s: %v", engine, seed)
			for _, feature := range scenario.Features {
				covered[feature] = true
			}
			for _, query := range scenario.Queries {
				for _, value := range query.Values {
					_, err := value.Binding(engine)
					require.NoError(t, err, "invalid binding for %s", engine)
				}
			}
		}
		for _, capability := range typeCapabilities(engine) {
			if capability.Status == "supported" {
				assert.True(t, covered[capability.Feature], "%s has no seed for %s", engine, capability.Feature)
			}
		}
	}
}

func TestTypedBindingsRetainNativeBoundaries(t *testing.T) {
	text := TypedNumber("text", "", 0)
	text.Text = "\\\t\n\r\x00'船"
	bound, err := text.Binding("clickhouse")
	require.NoError(t, err)
	require.Equal(t, `\\\t\n\r\0'船`, bound, "incorrect escaped parameter")
	binary := TypedNumber("binary", "", 0)
	binary.Bytes = []byte{'\\', 0, 255}
	bound, err = binary.Binding("clickhouse")
	require.NoError(t, err)
	require.Equal(t, "\\\\\\0\xff", bound, "binary parameter lost bytes")
	wide := TypedNumber("unsigned", "18446744073709551615", 0)
	for _, engine := range []string{"duckdb", "postgres", "cockroachdb", "timescaledb"} {
		bound, err = wide.Binding(engine)
		require.NoError(t, err, "%s rejected an unsigned value", engine)
		require.Equal(t, wide.Number, bound, "%s lost unsigned precision", engine)
	}
	small := TypedNumber("float", "", 0)
	small.Bits = 1
	literal, err := small.Literal("clickhouse")
	require.NoError(t, err)
	require.Equal(t, "reinterpretAsFloat64(toUInt64('1'))", literal, "subnormal literal lost bits")
}

func TestTypedBindingsKeepNamedAndPositionalEscapingSeparate(t *testing.T) {
	value := TypedNumber("text", "", 0)
	value.Text = "\\\x00"
	named, err := value.Binding("clickhouse")
	require.NoError(t, err)
	positional, err := value.PositionalBinding("clickhouse")
	require.NoError(t, err)
	require.Equal(t, value.Text, positional, "positional binding changed the raw text")
	require.NotEqual(t, named, positional, "binding formats conflated")
}

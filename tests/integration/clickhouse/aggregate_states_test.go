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
	"fmt"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/tests/integration/internal/harness"
)

func TestAggregateStatesAndVariantsAreRefusedWhereNoDriverReadsThem(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	_, dsn := harness.IsolatedDatabase(t, "clickhouse", testConnectionString)
	options, err := clickhouse.ParseDSN(dsn)
	require.NoError(t, err)

	testCases := []struct {
		name     string
		column   string
		value    string
		readable bool
	}{
		{name: "an aggregate state", column: "AggregateFunction(uniq, String)", value: "uniqState('gull')"},
		{name: "an aggregate state without arguments", column: "AggregateFunction(count)", value: "countState()"},
		{name: "a variant holding text", column: "Variant(Int32, String)", value: "'gull'"},
		{name: "a variant holding a number", column: "Variant(Int32, String)", value: "CAST(42 AS Int32)"},
		{name: "a simple aggregate", column: "SimpleAggregateFunction(sum, UInt64)", value: "5", readable: true},
		{name: "a dynamic value", column: "Dynamic", value: "'gull'", readable: true},
	}
	for index, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			table := fmt.Sprintf("states_%d", index)
			native, err := clickhouse.Open(options)
			require.NoError(t, err)
			t.Cleanup(func() { _ = native.Close() })
			require.NoError(t, native.Exec(t.Context(), "CREATE TABLE "+table+" (id UInt8, v "+testCase.column+") ENGINE = Memory"))
			require.NoError(t, native.Exec(t.Context(), "INSERT INTO "+table+" SELECT 1, "+testCase.value))

			standard := clickhouse.OpenDB(options)
			t.Cleanup(func() { _ = standard.Close() })
			var nativeValue, standardValue json.RawMessage
			nativeErr := native.QueryRow(t.Context(), "SELECT v FROM "+table).Scan(&nativeValue)
			standardErr := standard.QueryRowContext(t.Context(), "SELECT v FROM "+table).Scan(&standardValue)

			if !testCase.readable {
				require.False(t, nativeErr == nil && json.Valid(nativeValue), "the native driver reads the column as a JSON document")
				require.False(t, standardErr == nil && json.Valid(standardValue), "database/sql reads the column as a JSON document")
			}
			require.Equal(t, !testCase.readable, readRefused(t, testCase.column), "Seagull refuses reading the column")
		})
	}
}

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
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/stretchr/testify/require"
)

func TestServerRefusesAValueTheHelperCouldNotWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	options, err := clickhouse.ParseDSN(testConnectionString)
	require.NoError(t, err)
	conn, err := clickhouse.Open(options)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	refusal := "sgl: cannot write the value of a ClickHouse parameter: it nests more than 32 levels deep or refers to itself\t"
	testCases := []struct {
		name     string
		typeName string
	}{
		{name: "text", typeName: "String"},
		{name: "nullable text", typeName: "Nullable(String)"},
		{name: "an enum", typeName: "Enum8('a' = 1, 'b' = 2)"},
		{name: "a fixed string", typeName: "FixedString(200)"},
		{name: "a number", typeName: "UInt64"},
		{name: "an array", typeName: "Array(String)"},
		{name: "a map", typeName: "Map(String, String)"},
		{name: "a date and time", typeName: "DateTime"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var value any
			err := conn.QueryRow(t.Context(), "SELECT {p:"+testCase.typeName+"}", clickhouse.Named("p", refusal)).Scan(&value)
			require.Error(t, err, "the server must refuse the text a refused value is bound as")
			require.Contains(t, err.Error(), "sgl: cannot write the value of a ClickHouse parameter")
		})
	}
}

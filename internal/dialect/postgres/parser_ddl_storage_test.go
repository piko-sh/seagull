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

package postgres

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/postgres/pgext"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func applyWith(t *testing.T, engine *Dialect, sql string) error {
	t.Helper()

	_, err := applyWithContext(t.Context(), engine, sql)
	require.NotErrorIs(t, err, fault.ErrPanic)
	return err
}

func TestStorageParametersAreRecordedOnTheTablesMutation(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		want map[string]string
		name string
		sql  string
	}{
		{
			name: "numbers, words, strings and dotted keys",
			sql:  "CREATE TABLE t (id int) WITH (fillfactor = 70, autovacuum_enabled = false, toast.Tuple_Target = '128', flag)",
			want: map[string]string{
				pgext.EngineSpecificStorageParameterPrefix + "fillfactor":         "70",
				pgext.EngineSpecificStorageParameterPrefix + "autovacuum_enabled": "false",
				pgext.EngineSpecificStorageParameterPrefix + "toast.tuple_target": "128",
				pgext.EngineSpecificStorageParameterPrefix + "flag":               "true",
			},
		},
		{
			name: "a value of several tokens keeps its spelling",
			sql:  "CREATE TABLE t (id int) PARTITION BY RANGE (id) WITH (ttl_expire_after = INTERVAL  '3 days', x = -1)",
			want: map[string]string{
				pgext.EngineSpecificStorageParameterPrefix + "ttl_expire_after": "INTERVAL  '3 days'",
				pgext.EngineSpecificStorageParameterPrefix + "x":                "-1",
			},
		},
		{
			name: "a table without storage parameters records none",
			sql:  "CREATE TABLE t (id int) TABLESPACE fast",
			want: nil,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyDDL(t, testCase.sql)

			assert.Equal(t, testCase.want, mutation.EngineSpecific)
		})
	}
}

func TestAMalformedStorageParameterListFailsTheStatement(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantMessage string
	}{
		{name: "a key without = before its value", sql: "CREATE TABLE t (id int) WITH (fillfactor 70)", wantMessage: `expected '=' after reloption key "fillfactor"`},
		{name: "a list that is never closed", sql: "CREATE TABLE t (id int) WITH (fillfactor = 70", wantMessage: "expected ')'"},
		{name: "a value that opens a group it never closes", sql: "CREATE TABLE t (id int) WITH (x = f((1)", wantMessage: "unmatched parenthesis"},
		{name: "a key with no value after =", sql: "CREATE TABLE t (id int) WITH (x = )", wantMessage: "expected a value for the reloption"},
		{name: "a hash-sharded key's malformed parameters", sql: "CREATE TABLE t (id int PRIMARY KEY USING HASH WITH (bucket_count 8))", wantMessage: `expected '=' after reloption key "bucket_count"`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := applyWith(t, New(WithInlineIndexElements()), testCase.sql)

			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, testCase.wantMessage, parseError.Message)
		})
	}
}

func TestAHashShardedPrimaryKeyIsRecordedOnTheTablesMutation(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantBuckets string
		wantSharded bool
	}{
		{name: "a table key sharded with the default buckets", sql: "CREATE TABLE t (id int8, ts timestamptz, PRIMARY KEY (ts, id) USING HASH)", wantBuckets: "", wantSharded: true},
		{name: "a column key with a bucket count", sql: "CREATE TABLE t (id uuid PRIMARY KEY USING HASH WITH (bucket_count = 8), v text)", wantBuckets: "8", wantSharded: true},
		{name: "a named key whose storage parameters give no bucket count", sql: "CREATE TABLE t (id int, CONSTRAINT pk PRIMARY KEY (id) USING HASH WITH (fillfactor = 70))", wantBuckets: "", wantSharded: true},
		{name: "a sharded unique constraint shards no key", sql: "CREATE TABLE t (id int PRIMARY KEY, code text UNIQUE USING HASH)", wantSharded: false},
		{name: "a sharded index shards no key", sql: "CREATE TABLE t (id int PRIMARY KEY, ts timestamptz, INDEX (ts) USING HASH WITH (bucket_count = 4))", wantSharded: false},
		{name: "a key that is not sharded", sql: "CREATE TABLE t (id int PRIMARY KEY)", wantSharded: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyWithContext(t.Context(), New(WithInlineIndexElements()), testCase.sql)

			require.NoError(t, err)
			buckets, sharded := mutation.EngineSpecific[pgext.EngineSpecificHashShardBuckets]
			assert.Equal(t, testCase.wantSharded, sharded)
			assert.Equal(t, testCase.wantBuckets, buckets)
		})
	}
}

func TestStorageParameterValuesAreReadInLinearTime(t *testing.T) {
	t.Parallel()

	statement := func(depth int) string {
		return "CREATE TABLE t (id int) WITH (x = " + strings.Repeat("(", depth) + "1" + strings.Repeat(")", depth) + ")"
	}
	engine := New(WithMaxTokensPerStatement(dialectkit.MaxTokensPerStatementCeiling))
	apply := func(sql string) func() {
		return func() {
			require.NoError(t, applyWith(t, engine, sql))
		}
	}

	scaling.Linear(t, apply(statement(5_000)), apply(statement(20_000)))
}

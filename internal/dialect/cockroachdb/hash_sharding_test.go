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

package cockroachdb

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/postgres/pgext"
)

func TestHashShardedPrimaryKeyGetsTheHiddenShardColumn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		wantColumn string
		wantKey    []string
	}{
		{
			name:       "a hash-sharded table primary key",
			sql:        "CREATE TABLE t (ts TIMESTAMPTZ NOT NULL, id INT8 NOT NULL, PRIMARY KEY (ts, id) USING HASH)",
			wantColumn: "crdb_internal_ts_id_shard_16",
			wantKey:    []string{"crdb_internal_ts_id_shard_16", "ts", "id"},
		},
		{
			name:       "a hash-sharded column primary key with a bucket count",
			sql:        "CREATE TABLE t (id UUID NOT NULL PRIMARY KEY USING HASH WITH (bucket_count = 8), v STRING)",
			wantColumn: "crdb_internal_id_shard_8",
			wantKey:    []string{"crdb_internal_id_shard_8", "id"},
		},
		{
			name:    "a primary key that is not hash-sharded",
			sql:     "CREATE TABLE t (id INT8 PRIMARY KEY, ts TIMESTAMPTZ, INDEX (ts) USING HASH)",
			wantKey: []string{"id"},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			mutation, err := engine.ApplyDDL(t.Context(), statements[0])
			require.NoError(t, err)

			require.Equal(t, testCase.wantKey, mutation.PrimaryKey)
			last := mutation.Columns[len(mutation.Columns)-1]
			if testCase.wantColumn == "" {
				require.NotContains(t, last.Name, "_shard_")
				return
			}
			require.Equal(t, testCase.wantColumn, last.Name)
			require.True(t, last.Hidden)
			require.True(t, last.IsGenerated)
			require.False(t, last.Nullable)
		})
	}
}

func TestHashShardingIsReadFromTheMutationTheParserRecorded(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		mutation   *ast.CatalogueMutation
		name       string
		wantColumn string
		wantKey    []string
	}{
		{
			name: "a sharded key with its bucket count",
			mutation: ast.NewCatalogueMutation(ast.MutationCreateTable, "", "t", ast.WithPrimaryKey([]string{"id"}),
				ast.WithEngineSpecific(map[string]string{pgext.EngineSpecificHashShardBuckets: "32"})),
			wantColumn: "crdb_internal_id_shard_32",
			wantKey:    []string{"crdb_internal_id_shard_32", "id"},
		},
		{
			name: "a sharded key whose bucket count is no number",
			mutation: ast.NewCatalogueMutation(ast.MutationCreateTable, "", "t", ast.WithPrimaryKey([]string{"a", "b"}),
				ast.WithEngineSpecific(map[string]string{pgext.EngineSpecificHashShardBuckets: "lots"})),
			wantColumn: "crdb_internal_a_b_shard_16",
			wantKey:    []string{"crdb_internal_a_b_shard_16", "a", "b"},
		},
		{
			name:     "a key the parser did not record as sharded",
			mutation: ast.NewCatalogueMutation(ast.MutationCreateTable, "", "t", ast.WithPrimaryKey([]string{"id"})),
			wantKey:  []string{"id"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.NoError(t, addHashShardColumn(nil, 0, testCase.mutation))

			assert.Equal(t, testCase.wantKey, testCase.mutation.PrimaryKey)
			if testCase.wantColumn == "" {
				assert.Empty(t, testCase.mutation.Columns)
				return
			}
			require.Len(t, testCase.mutation.Columns, 1)
			assert.Equal(t, testCase.wantColumn, testCase.mutation.Columns[0].Name)
		})
	}
}

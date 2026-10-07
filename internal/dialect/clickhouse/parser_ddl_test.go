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

package clickhouse

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/sqlmodel"
)

func applyDDL(t *testing.T, sql string) (*ast.CatalogueMutation, error) {
	t.Helper()
	engine := New()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.Len(t, statements, 1)
	return engine.ApplyDDL(t.Context(), statements[0])
}

func TestDDL_CreateTableSimpleMergeTree(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, `
		CREATE TABLE users (
			id UInt64,
			email String,
			created_at DateTime
		) ENGINE = MergeTree() ORDER BY id
	`)
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
	assert.Equal(t, "users", mutation.TableName)
	require.Len(t, mutation.Columns, 3)
	assert.Equal(t, "id", mutation.Columns[0].Name)
	assert.Equal(t, sqlmodel.TypeCategoryInteger, mutation.Columns[0].SQLType.Category)
	assert.False(t, mutation.Columns[0].Nullable)
	assert.Equal(t, "email", mutation.Columns[1].Name)
	assert.Equal(t, sqlmodel.TypeCategoryText, mutation.Columns[1].SQLType.Category)

	assert.Equal(t, []string{"id"}, mutation.PrimaryKey)
	assert.Contains(t, mutation.EngineSpecific, "ENGINE")
	assert.Contains(t, mutation.EngineSpecific, "ORDER_BY")
}

func TestDDL_CreateTableWithNullableColumns(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, `
		CREATE TABLE accounts (
			id UInt64,
			label Nullable(String),
			balance Decimal(18, 4)
		) ENGINE = MergeTree() ORDER BY id
	`)
	require.NoError(t, err)
	require.Len(t, mutation.Columns, 3)
	assert.False(t, mutation.Columns[0].Nullable, "id not nullable")
	assert.True(t, mutation.Columns[1].Nullable, "Nullable(String) sets the flag")
	assert.Equal(t, sqlmodel.TypeCategoryText, mutation.Columns[1].SQLType.Category)
	assert.Equal(t, sqlmodel.TypeCategoryDecimal, mutation.Columns[2].SQLType.Category)
}

func TestDDL_CreateTableWithArrayColumn(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, `
		CREATE TABLE events (
			id UUID,
			tags Array(String)
		) ENGINE = MergeTree() ORDER BY id
	`)
	require.NoError(t, err)
	require.Len(t, mutation.Columns, 2)
	tagsColumn := mutation.Columns[1]
	assert.Equal(t, sqlmodel.TypeCategoryArray, tagsColumn.SQLType.Category)
	require.NotNil(t, tagsColumn.SQLType.ElementType)
	assert.Equal(t, sqlmodel.TypeCategoryText, tagsColumn.SQLType.ElementType.Category)
}

func TestDDL_CreateTableWithSchemaQualification(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, `
		CREATE TABLE analytics.events (
			id UInt64,
			ts DateTime
		) ENGINE = MergeTree() ORDER BY (ts, id)
	`)
	require.NoError(t, err)
	assert.Equal(t, "analytics", mutation.SchemaName)
	assert.Equal(t, "events", mutation.TableName)
	assert.Equal(t, []string{"ts", "id"}, mutation.PrimaryKey)
}

func TestDDL_CreateTableWithPartitionByAndTTL(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, `
		CREATE TABLE logs (
			id UInt64,
			ts DateTime
		) ENGINE = MergeTree()
		PARTITION BY toYYYYMM(ts)
		ORDER BY id
		TTL ts + INTERVAL 30 DAY
	`)
	require.NoError(t, err)
	assert.Contains(t, mutation.EngineSpecific, "PARTITION_BY")
	assert.Contains(t, mutation.EngineSpecific, "TTL")
	assert.Contains(t, mutation.EngineSpecific, "ORDER_BY")
}

func TestDDL_CreateTableExplicitPrimaryKey(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, `
		CREATE TABLE products (
			id UInt64,
			sku String,
			PRIMARY KEY (id)
		) ENGINE = MergeTree() ORDER BY id
	`)
	require.NoError(t, err)
	assert.Equal(t, []string{"id"}, mutation.PrimaryKey)
}

func TestDDL_CreateTableIfNotExists(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, `
		CREATE TABLE IF NOT EXISTS items (
			id UInt64
		) ENGINE = Log
	`)
	require.NoError(t, err)
	assert.Equal(t, "items", mutation.TableName)
}

func TestDDL_CreateTableWithDefault(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, `
		CREATE TABLE settings (
			id UInt64,
			created_at DateTime DEFAULT now(),
			label String DEFAULT 'unset'
		) ENGINE = MergeTree() ORDER BY id
	`)
	require.NoError(t, err)
	require.Len(t, mutation.Columns, 3)
	assert.False(t, mutation.Columns[0].HasDefault)
	assert.True(t, mutation.Columns[1].HasDefault)
	assert.True(t, mutation.Columns[2].HasDefault)
}

func TestDDL_DropTable(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "DROP TABLE IF EXISTS users")
	require.NoError(t, err)
	assert.Equal(t, ast.MutationDropTable, mutation.Kind)
	assert.Equal(t, "users", mutation.TableName)
}

func TestDDL_AlterAddColumn(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE users ADD COLUMN email Nullable(String)")
	require.NoError(t, err)
	assert.Equal(t, ast.MutationAlterTableAddColumn, mutation.Kind)
	require.Len(t, mutation.Columns, 1)
	assert.Equal(t, "email", mutation.Columns[0].Name)
	assert.True(t, mutation.Columns[0].Nullable)
}

func TestDDL_AlterDropColumn(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE users DROP COLUMN email")
	require.NoError(t, err)
	assert.Equal(t, ast.MutationAlterTableDropColumn, mutation.Kind)
	assert.Equal(t, "email", mutation.ColumnName)
}

func TestDDL_AlterModifyColumn(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE users MODIFY COLUMN id UInt64")
	require.NoError(t, err)
	assert.Equal(t, ast.MutationAlterTableAlterColumn, mutation.Kind)
	assert.Equal(t, "id", mutation.ColumnName)
}

func TestDDL_AlterRenameColumn(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE users RENAME COLUMN old_name TO new_name")
	require.NoError(t, err)
	assert.Equal(t, ast.MutationAlterTableRenameColumn, mutation.Kind)
	assert.Equal(t, "old_name", mutation.ColumnName)
	assert.Equal(t, "new_name", mutation.NewName)
}

func TestDDL_AlterUpdate(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE users UPDATE name = 'unknown' WHERE id = 1")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationAsyncDataUpdate, mutation.Kind)
	assert.Equal(t, "users", mutation.TableName)
	asyncBody := mutation.EngineSpecific["ASYNC_BODY"]
	assert.Contains(t, asyncBody, "name")
	assert.Contains(t, asyncBody, "WHERE")
}

func TestDDL_AlterDelete(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE users DELETE WHERE id = 1")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationAsyncDataDelete, mutation.Kind)
	assert.Equal(t, "users", mutation.TableName)
	assert.Contains(t, mutation.EngineSpecific["ASYNC_BODY"], "WHERE")
}

func TestDDL_AlterTableOnCluster(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE users ON CLUSTER my_cluster ADD COLUMN age UInt8")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "my_cluster", mutation.EngineSpecific["ON_CLUSTER"])
}

func TestDDL_CreateTableOnCluster(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "CREATE TABLE replicated_users ON CLUSTER prod (id UInt64) ENGINE = MergeTree ORDER BY id")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "prod", mutation.EngineSpecific["ON_CLUSTER"])
}

func TestDDL_DropTableOnClusterWithSync(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "DROP TABLE IF EXISTS x ON CLUSTER c SYNC")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "c", mutation.EngineSpecific["ON_CLUSTER"])
}

func TestDDL_AlterTablePartitionOps(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		"ALTER TABLE events FREEZE PARTITION '2026-01'",
		"ALTER TABLE events UNFREEZE PARTITION '2026-01' WITH NAME 'backup'",
		"ALTER TABLE events DETACH PARTITION '2026-01'",
		"ALTER TABLE events ATTACH PARTITION '2026-01'",
		"ALTER TABLE events DROP PARTITION '2026-01'",
		"ALTER TABLE events MOVE PARTITION '2026-01' TO DISK 'cold'",
		"ALTER TABLE events FETCH PARTITION '2026-01' FROM '/clickhouse/tables/events'",
	} {
		_, err := applyDDL(t, source)
		assert.NoError(t, err, "failed to parse: %s", source)
	}
}

func TestDDL_AlterMaterializeVariants(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		"ALTER TABLE t MATERIALIZE COLUMN c",
		"ALTER TABLE t MATERIALIZE INDEX i",
		"ALTER TABLE t MATERIALIZE PROJECTION p",
		"ALTER TABLE t MATERIALIZE TTL",
		"ALTER TABLE t MATERIALIZE STATISTICS s",
	} {
		_, err := applyDDL(t, source)
		assert.NoError(t, err, "failed to parse: %s", source)
	}
}

func TestDDL_AlterTableMultiActionIndexAndColumn(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "ALTER TABLE t ADD INDEX i col TYPE bloom_filter GRANULARITY 1, ADD COLUMN new_col UInt32")
	require.NoError(t, err)
	require.NotNil(t, mutation)

	allMutations := append([]*ast.CatalogueMutation{mutation}, mutation.AdditionalMutations...)
	foundColumn := false
	for _, candidate := range allMutations {
		if candidate == nil {
			continue
		}
		if candidate.Kind == ast.MutationAlterTableAddColumn {
			foundColumn = true
			break
		}
	}
	assert.True(t, foundColumn, "ADD COLUMN action lost after ADD INDEX")
}

func TestDDL_CreateView(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "CREATE VIEW v AS SELECT id FROM t")
	require.NoError(t, err)
	assert.Equal(t, ast.MutationCreateView, mutation.Kind)
	assert.Equal(t, "v", mutation.TableName)
}

func TestDDL_CreateMaterializedView(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, `
		CREATE MATERIALIZED VIEW mv
		ENGINE = MergeTree() ORDER BY id
		AS SELECT id FROM t
	`)
	require.NoError(t, err)
	assert.Equal(t, ast.MutationCreateView, mutation.Kind)
	assert.Equal(t, "true", mutation.EngineSpecific["MATERIALIZED"])
}

func TestDDL_CreateDatabase(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "CREATE DATABASE analytics")
	require.NoError(t, err)
	assert.Equal(t, ast.MutationCreateSchema, mutation.Kind)
	assert.Equal(t, "analytics", mutation.SchemaName)
}

func TestDDL_DropDatabase(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "DROP DATABASE IF EXISTS analytics")
	require.NoError(t, err)
	assert.Equal(t, ast.MutationDropSchema, mutation.Kind)
	assert.Equal(t, "analytics", mutation.SchemaName)
}

func TestDDL_RenameTable(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "RENAME TABLE old TO new")
	require.NoError(t, err)
	assert.Equal(t, ast.MutationAlterTableRenameTable, mutation.Kind)
	assert.Equal(t, "old", mutation.TableName)
	assert.Equal(t, "new", mutation.NewName)
}

func TestDDL_TruncateOptimizeSystemNoOps(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"TRUNCATE TABLE t",
		"OPTIMIZE TABLE t FINAL",
		"SYSTEM FLUSH LOGS",
		"USE analytics",
		"SHOW TABLES",
		"SET max_threads = 4",
	} {
		t.Run(sql, func(t *testing.T) {
			t.Parallel()
			mutation, err := applyDDL(t, sql)
			require.NoError(t, err)
			assert.Nil(t, mutation, "expected catalogue no-op")
		})
	}
}

func TestDDL_CreateTableColumnNotNullSuffix(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, `
		CREATE TABLE accounts (
			id UInt64 NOT NULL,
			label String NULL,
			email String
		) ENGINE = MergeTree() ORDER BY id
	`)
	require.NoError(t, err)
	require.Len(t, mutation.Columns, 3)
	assert.False(t, mutation.Columns[0].Nullable, "Int64 NOT NULL stays non-nullable")
	assert.True(t, mutation.Columns[1].Nullable, "String NULL promotes to nullable")
	assert.False(t, mutation.Columns[2].Nullable, "bare String remains non-nullable")
}

func TestDDL_CreateTableAsSelect(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "CREATE TABLE summary AS SELECT id, name FROM users")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "summary", mutation.TableName)
	require.Len(t, mutation.Columns, 2, "CTAS body should populate columns from SELECT")
	assert.Equal(t, "id", mutation.Columns[0].Name)
	assert.Equal(t, "name", mutation.Columns[1].Name)
}

func TestDDL_CreateTableAsSourceTable(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "CREATE TABLE clone AS analytics.events ENGINE = MergeTree() ORDER BY id")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "events", mutation.EngineSpecific["CTAS_SOURCE_TABLE"])
	assert.Equal(t, "analytics", mutation.EngineSpecific["CTAS_SOURCE_SCHEMA"])
}

func TestDDL_CreateTableReplacingMergeTreeCapturesVersion(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, `
		CREATE TABLE events (
			id UInt64,
			ts DateTime,
			version UInt64,
			is_deleted UInt8
		) ENGINE = ReplacingMergeTree(version, is_deleted) ORDER BY id
	`)
	require.NoError(t, err)
	assert.Equal(t, "version", mutation.EngineSpecific["MERGETREE_VERSION_COLUMN"])
	assert.Equal(t, "is_deleted", mutation.EngineSpecific["MERGETREE_IS_DELETED_COLUMN"])
}

func TestDDL_CreateTableCollapsingMergeTreeCapturesSign(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, `
		CREATE TABLE transactions (
			id UInt64,
			sign Int8
		) ENGINE = CollapsingMergeTree(sign) ORDER BY id
	`)
	require.NoError(t, err)
	assert.Equal(t, "sign", mutation.EngineSpecific["MERGETREE_SIGN_COLUMN"])
}

func TestDDL_CreateTableVersionedCollapsingMergeTree(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, `
		CREATE TABLE transactions (
			id UInt64,
			sign Int8,
			ver UInt32
		) ENGINE = VersionedCollapsingMergeTree(sign, ver) ORDER BY id
	`)
	require.NoError(t, err)
	assert.Equal(t, "sign", mutation.EngineSpecific["MERGETREE_SIGN_COLUMN"])
	assert.Equal(t, "ver", mutation.EngineSpecific["MERGETREE_VERSION_COLUMN"])
}

func TestDDL_CreateTableSummingMergeTree(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, `
		CREATE TABLE totals (
			id UInt64,
			revenue UInt64,
			impressions UInt64
		) ENGINE = SummingMergeTree((revenue, impressions)) ORDER BY id
	`)
	require.NoError(t, err)
	assert.Contains(t, mutation.EngineSpecific["MERGETREE_SUMMING_COLUMNS"], "revenue")
}

func TestDDL_CreateTableReplicatedMergeTree(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, `
		CREATE TABLE shards (
			id UInt64
		) ENGINE = ReplicatedMergeTree('/clickhouse/tables/shard1', 'replica_a') ORDER BY id
	`)
	require.NoError(t, err)
	assert.Equal(t, "'/clickhouse/tables/shard1'", mutation.EngineSpecific["MERGETREE_ZOO_PATH"])
	assert.Equal(t, "'replica_a'", mutation.EngineSpecific["MERGETREE_REPLICA_NAME"])
}

func TestDDL_CreateDictionaryStructured(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, `
		CREATE DICTIONARY country_codes (
			code UInt16,
			name String
		)
		PRIMARY KEY code
		SOURCE(HTTP(url 'http://example.com/codes'))
		LAYOUT(HASHED())
		LIFETIME(MIN 60 MAX 300)
	`)
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationCreateDictionary, mutation.Kind)
	assert.Equal(t, "country_codes", mutation.TableName)
	require.Len(t, mutation.Columns, 2)
	assert.Equal(t, "code", mutation.Columns[0].Name)
	assert.Equal(t, []string{"code"}, mutation.PrimaryKey)
	assert.Equal(t, "code", mutation.EngineSpecific["DICTIONARY_PRIMARY_KEY"])
	assert.Contains(t, mutation.EngineSpecific["DICTIONARY_SOURCE"], "HTTP")
	assert.Contains(t, mutation.EngineSpecific["DICTIONARY_LAYOUT"], "HASHED")
	assert.Equal(t, "MIN 60 MAX 300", mutation.EngineSpecific["DICTIONARY_LIFETIME_SECONDS"])
}

func TestDDL_CreateDictionarySimpleLifetime(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, `
		CREATE DICTIONARY codes (id UInt64, name String)
		PRIMARY KEY id
		SOURCE(FILE(path '/var/data.csv'))
		LAYOUT(FLAT())
		LIFETIME(300)
	`)
	require.NoError(t, err)
	assert.Equal(t, "300", mutation.EngineSpecific["DICTIONARY_LIFETIME_SECONDS"])
}

func TestDDL_DropDictionary(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "DROP DICTIONARY IF EXISTS country_codes")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationDropDictionary, mutation.Kind)
	assert.Equal(t, "country_codes", mutation.TableName)
}

func TestDDL_ExchangeTables(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "EXCHANGE TABLES analytics.events_new AND analytics.events_old")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationExchangeTables, mutation.Kind)
	assert.Equal(t, "analytics", mutation.SchemaName)
	assert.Equal(t, "events_new", mutation.TableName)
	assert.Equal(t, "analytics.events_old", mutation.EngineSpecific["EXCHANGE_TARGET"])
}

func TestDDL_ExchangeTablesOnCluster(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "EXCHANGE TABLES a AND b ON CLUSTER c")
	require.NoError(t, err)
	assert.Equal(t, "a", mutation.TableName)
	assert.Equal(t, "b", mutation.EngineSpecific["EXCHANGE_TARGET"])
	assert.Equal(t, "c", mutation.EngineSpecific["ON_CLUSTER"])
}

func TestCreateFunction_LambdaBody_Literal(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "CREATE FUNCTION fortytwo AS () -> 42")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	require.Equal(t, ast.MutationCreateFunction, mutation.Kind)
	require.NotNil(t, mutation.Function)
	assert.Equal(t, "fortytwo", mutation.Function.Name)
	assert.NotNil(t, mutation.Function.Body)
	assert.Empty(t, mutation.Function.BodyParameters)

	assert.Equal(t, sqlmodel.TypeCategoryUnknown, mutation.Function.ReturnType.Category)
}

func TestCreateFunction_LambdaBody_SingleParam(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "CREATE FUNCTION twice AS x -> x * 2")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	require.NotNil(t, mutation.Function)
	assert.Equal(t, "twice", mutation.Function.Name)
	assert.NotNil(t, mutation.Function.Body)
	assert.Equal(t, []string{"x"}, mutation.Function.BodyParameters)
}

func TestCreateFunction_LambdaBody_MultiParam(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "CREATE FUNCTION addxy AS (x, y) -> x + y")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	require.NotNil(t, mutation.Function)
	assert.Equal(t, "addxy", mutation.Function.Name)
	assert.NotNil(t, mutation.Function.Body)
	assert.Equal(t, []string{"x", "y"}, mutation.Function.BodyParameters)
}

func TestCreateFunction_LegacyNoAS_Backwards(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "CREATE FUNCTION legacy")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	require.NotNil(t, mutation.Function)
	assert.Equal(t, "legacy", mutation.Function.Name)
	assert.Nil(t, mutation.Function.Body)
}

func TestCreateFunction_FunctionCallInBody(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "CREATE FUNCTION lengthof AS arr -> arrayLength(arr)")
	require.NoError(t, err)
	require.NotNil(t, mutation)
	require.NotNil(t, mutation.Function)
	require.NotNil(t, mutation.Function.Body)
	assert.Equal(t, []string{"arr"}, mutation.Function.BodyParameters)
	body, ok := mutation.Function.Body.(*ast.FunctionCallExpression)
	require.True(t, ok, "body should be a function call expression")
	assert.Equal(t, "arrayLength", body.FunctionName)
	require.Len(t, body.Arguments, 1)
	columnReference, ok := body.Arguments[0].(*ast.ColumnRefExpression)
	require.True(t, ok, "argument should be a column reference")
	assert.Equal(t, "arr", columnReference.ColumnName)
}

func TestInferPrimaryKeyFromOrderBy_ParenAwareSplit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		orderBy  string
		expected []string
	}{
		{
			name:     "single column",
			orderBy:  "id",
			expected: []string{"id"},
		},
		{
			name:     "plain column list",
			orderBy:  "(a, b, c)",
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "multi-arg function call kept intact",
			orderBy:  "(cityHash64(a, b), c)",
			expected: []string{"cityHash64(a,", "c"},
		},
		{
			name:     "direction qualifier dropped",
			orderBy:  "(a DESC, b ASC)",
			expected: []string{"a", "b"},
		},
		{
			name:     "nested function calls",
			orderBy:  "(toStartOfHour(ts), cityHash64(a, b))",
			expected: []string{"toStartOfHour(ts)", "cityHash64(a,"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(subtest *testing.T) {
			subtest.Parallel()
			assert.Equal(subtest, testCase.expected, inferPrimaryKeyFromOrderBy(testCase.orderBy))
		})
	}
}

func TestModifyColumn_MalformedSubActionErrors(t *testing.T) {
	t.Parallel()

	_, err := applyDDL(t, `ALTER TABLE t MODIFY COLUMN c RESET notsetting`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SETTING")
}

func TestDictionaryLifetime_MinWithoutMax(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, `
		CREATE DICTIONARY d (id UInt64, name String)
		PRIMARY KEY id
		SOURCE(HTTP(url 'http://example.com/codes'))
		LAYOUT(HASHED())
		LIFETIME(MIN 300)
	`)
	require.NoError(t, err)
	require.NotNil(t, mutation)
	lifetime := mutation.EngineSpecific["DICTIONARY_LIFETIME_SECONDS"]
	assert.Equal(t, "MIN 300", lifetime)
	assert.NotContains(t, lifetime, "MAX ")
}

func TestDDL_CreateTableWithoutColumnList(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		wantEngine     map[string]string
		name           string
		sql            string
		wantSourceName string
		wantColumns    []string
	}{
		{
			name:        "engine clauses before AS SELECT",
			sql:         "CREATE TABLE summary ENGINE = MergeTree() ORDER BY id AS SELECT id, name FROM users",
			wantColumns: []string{"id", "name"},
			wantEngine:  map[string]string{"ENGINE": "MergeTree()"},
		},
		{
			name:        "EMPTY before AS SELECT",
			sql:         "CREATE TABLE summary ENGINE = Memory EMPTY AS SELECT id FROM users",
			wantColumns: []string{"id"},
			wantEngine:  map[string]string{"ENGINE": "Memory", "EMPTY": "true"},
		},
		{
			name:        "AS WITH body",
			sql:         "CREATE TABLE summary ENGINE = Memory AS WITH active AS (SELECT id FROM users) SELECT id FROM active",
			wantColumns: []string{"id"},
			wantEngine:  map[string]string{"ENGINE": "Memory"},
		},
		{
			name:           "AS source table followed by engine clauses",
			sql:            "CREATE TABLE clone AS events ENGINE = Memory",
			wantColumns:    nil,
			wantSourceName: "events",
			wantEngine:     map[string]string{"ENGINE": "Memory"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyDDL(t, testCase.sql)
			require.NoError(t, err)
			require.NotNil(t, mutation)

			names := make([]string, 0, len(mutation.Columns))
			for _, column := range mutation.Columns {
				names = append(names, column.Name)
				assert.True(t, column.Nullable)
				assert.Equal(t, sqlmodel.TypeCategoryUnknown, column.SQLType.Category)
			}
			if testCase.wantColumns == nil {
				assert.Empty(t, names)
			} else {
				assert.Equal(t, testCase.wantColumns, names)
			}
			assert.Equal(t, testCase.wantSourceName, mutation.EngineSpecific["CTAS_SOURCE_TABLE"])
			for key, value := range testCase.wantEngine {
				assert.Equal(t, value, mutation.EngineSpecific[key], "engine clause %s", key)
			}
		})
	}
}

func TestDDL_CreateTableWithoutColumnListOrBodyFails(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		sql     string
		wantErr string
	}{
		{name: "engine clauses without AS", sql: "CREATE TABLE t ENGINE = Memory", wantErr: "expected '(' or AS"},
		{name: "malformed AS SELECT body", sql: "CREATE TABLE t ENGINE = Memory AS SELECT id FROM", wantErr: "analysing CREATE TABLE AS body"},
		{name: "AS SELECT body with an unclosed parenthesis", sql: "CREATE TABLE t ENGINE = Memory AS SELECT id FROM (SELECT 1", wantErr: "unmatched parenthesis"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyDDL(t, testCase.sql)
			require.Error(t, err)
			assert.Nil(t, mutation)
			assert.Contains(t, err.Error(), testCase.wantErr)
		})
	}
}

func TestDDL_CreateTableWithColumnListIgnoresAsBody(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "CREATE TABLE t (id UInt64, label String) ENGINE = MergeTree() ORDER BY id AS SELECT 1, 'x'")
	require.NoError(t, err)
	require.Len(t, mutation.Columns, 2)
	assert.Equal(t, "id", mutation.Columns[0].Name)
	assert.Equal(t, "label", mutation.Columns[1].Name)
	assert.Equal(t, "id", mutation.EngineSpecific["ORDER_BY"])
}

func TestDDL_MaterializedViewWithInlineEngineKeepsItsBody(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "CREATE MATERIALIZED VIEW mv ENGINE = MergeTree() ORDER BY id AS SELECT id, val * 2 AS doubled FROM src")
	require.NoError(t, err)
	require.NotNil(t, mutation.ViewDefinition)
	require.Len(t, mutation.ViewDefinition.OutputColumns, 2)
	require.Equal(t, "id", mutation.ViewDefinition.OutputColumns[0].ColumnName)
	require.Equal(t, "doubled", mutation.ViewDefinition.OutputColumns[1].Name)
	require.Empty(t, mutation.Columns, "a view that declares no column names carries none")
	require.Equal(t, "MergeTree()", mutation.EngineSpecific["ENGINE"])
}

func TestDDL_OverDeepViewBodyIsReported(t *testing.T) {
	t.Parallel()

	const nesting = 40
	engine := New(WithMaxParseDepth(nesting / 2))
	sql := "CREATE VIEW v AS SELECT " + strings.Repeat("(", nesting) + "1" + strings.Repeat(")", nesting) + " AS x FROM t"
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.Len(t, statements, 1)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.ErrorIs(t, err, dialectkit.ErrParseDepth)
	assert.Nil(t, mutation)
}

func TestDDL_DropFunction(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		sql      string
		wantName string
		wantErr  string
	}{
		{name: "plain", sql: "DROP FUNCTION f", wantName: "f"},
		{name: "IF EXISTS with cluster", sql: "DROP FUNCTION IF EXISTS g ON CLUSTER main", wantName: "g"},
		{name: "missing name", sql: "DROP FUNCTION", wantErr: "expected an identifier"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyDDL(t, testCase.sql)
			if testCase.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, ast.MutationDropFunction, mutation.Kind)
			require.NotNil(t, mutation.Function)
			assert.Equal(t, testCase.wantName, mutation.Function.Name)
			assert.Empty(t, mutation.Function.Schema)
		})
	}
}

func TestDDL_ViewBodiesThatAreNotSelectsKeepNoDefinition(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
	}{
		{name: "view without a body", sql: "CREATE VIEW v AS"},
		{name: "view over a non-SELECT body", sql: "CREATE VIEW v AS VALUES (1)"},
		{name: "view over a malformed SELECT", sql: "CREATE VIEW v AS SELECT id FROM"},
		{name: "materialised view without AS", sql: "CREATE MATERIALIZED VIEW mv TO target"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyDDL(t, testCase.sql)
			require.NoError(t, err)
			assert.Nil(t, mutation.ViewDefinition)
			assert.Empty(t, mutation.Columns)
		})
	}
}

func TestColumnsTheEngineLeavesOutOfAStarAreHidden(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, `CREATE TABLE p (
		id UInt64,
		s String,
		doubled UInt64 ALIAS id * 2,
		loud String MATERIALIZED upper(s),
		raw String EPHEMERAL,
		fallback String DEFAULT 'x'
	) ENGINE = MergeTree ORDER BY id`)
	require.NoError(t, err)

	hidden := map[string]bool{}
	for index := range mutation.Columns {
		hidden[mutation.Columns[index].Name] = mutation.Columns[index].Hidden
	}
	require.Equal(t, map[string]bool{"id": false, "s": false, "doubled": true, "loud": true, "raw": true, "fallback": false}, hidden)
}

func TestDDL_RecordsHowAStatementReplays(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		sql   string
		check func(t *testing.T, mutation *ast.CatalogueMutation)
	}{
		{
			name: "ADD COLUMN IF NOT EXISTS AFTER",
			sql:  "ALTER TABLE t ADD COLUMN IF NOT EXISTS m Int32 AFTER a",
			check: func(t *testing.T, mutation *ast.CatalogueMutation) {
				t.Helper()
				require.True(t, mutation.IfNotExists)
				require.Equal(t, ast.Placement{Anchor: "a", Kind: ast.PlacementAfter}, mutation.Placement)
			},
		},
		{
			name: "MODIFY COLUMN IF EXISTS FIRST",
			sql:  "ALTER TABLE t MODIFY COLUMN IF EXISTS c Int64 FIRST",
			check: func(t *testing.T, mutation *ast.CatalogueMutation) {
				t.Helper()
				require.Equal(t, ast.MutationAlterTableAlterColumn, mutation.Kind)
				require.True(t, mutation.IfColumnExists)
				require.Equal(t, ast.Placement{Anchor: "", Kind: ast.PlacementFirst}, mutation.Placement)
			},
		},
		{
			name: "RENAME TABLE across databases moves the table",
			sql:  "RENAME TABLE app.a TO archive.b",
			check: func(t *testing.T, mutation *ast.CatalogueMutation) {
				t.Helper()
				require.Equal(t, ast.MutationAlterTableRenameTable, mutation.Kind)
				require.Equal(t, "b", mutation.NewName)
				require.Len(t, mutation.AdditionalMutations, 1)
				move := mutation.AdditionalMutations[0]
				require.Equal(t, ast.MutationAlterTableSetSchema, move.Kind)
				require.Equal(t, "b", move.TableName)
				require.Equal(t, "archive", move.NewName)
			},
		},
		{
			name: "CREATE TABLE IF NOT EXISTS",
			sql:  "CREATE TABLE IF NOT EXISTS t (id Int32) ENGINE = MergeTree ORDER BY id",
			check: func(t *testing.T, mutation *ast.CatalogueMutation) {
				t.Helper()
				require.True(t, mutation.IfNotExists)
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyDDL(t, testCase.sql)
			require.NoError(t, err)
			testCase.check(t, mutation)
		})
	}
}

func TestTimeColumnsAreAcceptedInDDL(t *testing.T) {
	t.Parallel()

	mutation, err := applyDDL(t, "CREATE TABLE shifts (id UInt64, starts Time, ends Time64(6), paused Nullable(Time), "+
		"laps Array(Time64(3))) ENGINE = MergeTree ORDER BY id")
	require.NoError(t, err)
	require.Len(t, mutation.Columns, 5)
	for _, column := range mutation.Columns[1:3] {
		require.Equal(t, sqlmodel.TemporalKindTime, column.SQLType.Temporal, column.Name)
	}
	require.True(t, mutation.Columns[3].Nullable)
	require.Equal(t, sqlmodel.TemporalKindTime, mutation.Columns[3].SQLType.Temporal)
	require.NotNil(t, mutation.Columns[4].SQLType.ElementType)
	require.Equal(t, sqlmodel.TemporalKindTime, mutation.Columns[4].SQLType.ElementType.Temporal)
}

func TestDropStatementsRecordWhetherTheySaidIfExists(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		sql          string
		wantKind     ast.MutationKind
		wantIfExists bool
	}{
		{name: "a table", sql: "DROP TABLE events", wantKind: ast.MutationDropTable, wantIfExists: false},
		{name: "a table IF EXISTS", sql: "DROP TABLE IF EXISTS db.events ON CLUSTER main SYNC", wantKind: ast.MutationDropTable, wantIfExists: true},
		{name: "a view", sql: "DROP VIEW recent", wantKind: ast.MutationDropView, wantIfExists: false},
		{name: "a view IF EXISTS", sql: "DROP VIEW IF EXISTS recent", wantKind: ast.MutationDropView, wantIfExists: true},
		{name: "a database", sql: "DROP DATABASE analytics", wantKind: ast.MutationDropSchema, wantIfExists: false},
		{name: "a database IF EXISTS", sql: "DROP DATABASE IF EXISTS analytics", wantKind: ast.MutationDropSchema, wantIfExists: true},
		{name: "a function", sql: "DROP FUNCTION double", wantKind: ast.MutationDropFunction, wantIfExists: false},
		{name: "a function IF EXISTS", sql: "DROP FUNCTION IF EXISTS double ON CLUSTER main", wantKind: ast.MutationDropFunction, wantIfExists: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation, err := applyDDL(t, testCase.sql)
			require.NoError(t, err)
			require.NotNil(t, mutation)
			assert.Equal(t, testCase.wantKind, mutation.Kind)
			assert.Equal(t, testCase.wantIfExists, mutation.IfExists)
		})
	}
}

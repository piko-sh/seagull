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

package timescaledb_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/dialect/timescaledb"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestTimescaleDBEngine_SatisfiesTheDialectContract(t *testing.T) {
	t.Parallel()

	var _ dialect.DialectPort = timescaledb.New()
}

func TestTimescaleDBEngine_DialectName(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	assert.Equal(t, "timescaledb", engine.Info().Name)
}

func TestTimescaleDBEngine_PostgresQueriesPassThrough(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), "SELECT id FROM users")
	require.NoError(t, err)
	require.Len(t, statements, 1)
}

func TestNewTimescaleDBEngineAppliesPostgresOptions(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		options []postgres.Option
		wantErr bool
	}{
		{name: "default token budget accepts the statement", options: nil, wantErr: false},
		{name: "tight token budget rejects the statement", options: []postgres.Option{postgres.WithMaxTokensPerStatement(3)}, wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := timescaledb.New(testCase.options...)
			statements, err := engine.ParseStatements(t.Context(), "CREATE HYPERTABLE readings (ts TIMESTAMPTZ NOT NULL, value DOUBLE PRECISION)")
			require.NoError(t, err)

			_, err = engine.ApplyDDL(t.Context(), statements[0])

			assert.Equal(t, "timescaledb", engine.Info().Name)
			if testCase.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "token budget")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestTimescaleDBKeepsItsSettingsAlongsideCallerOptions(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New(
		postgres.WithExtraFunctions(func(builder *postgres.FunctionCatalogueBuilder) {
			builder.NeverNull("caller_function", nil, builder.Text())
		}),
		postgres.WithExtraTypes(map[string]sqlmodel.SQLType{"caller_type": sqlmodel.NewSQLType(sqlmodel.TypeCategoryUUID, "caller_type")}),
		postgres.WithTypeNormaliserHook(func(string, []int) *sqlmodel.SQLType { return nil }),
	)

	functions := engine.BuiltinFunctions().Functions
	assert.NotEmpty(t, functions["caller_function"])
	assert.NotEmpty(t, functions["time_bucket"])
	assert.Equal(t, sqlmodel.TypeCategoryInteger, engine.NormaliseTypeName("regproc").Category)
	assert.Equal(t, sqlmodel.TypeCategoryUUID, engine.NormaliseTypeName("caller_type").Category)
}

func TestTimescaleDBReadsPostgresPartitionStatements(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		sql      string
		wantKind ast.MutationKind
	}{
		{sql: "CREATE TABLE m1 PARTITION OF m FOR VALUES FROM ('2026-01-01') TO ('2027-01-01')", wantKind: ast.MutationCreateTable},
		{sql: "ALTER TABLE m ATTACH PARTITION m1 FOR VALUES FROM ('2026-01-01') TO ('2027-01-01')", wantKind: ast.MutationAttachPartition},
		{sql: "ALTER TABLE m DETACH PARTITION m1", wantKind: ast.MutationDetachPartition},
	}
	for _, testCase := range testCases {
		t.Run(testCase.sql, func(t *testing.T) {
			t.Parallel()

			engine := timescaledb.New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			mutation, err := engine.ApplyDDL(t.Context(), statements[0])
			require.NoError(t, err)
			require.Equal(t, testCase.wantKind, mutation.Kind)
			require.Equal(t, "m1", mutation.TableName)
			require.NotNil(t, mutation.PartitionOf)
			require.Equal(t, "m", mutation.PartitionOf.Name)
		})
	}
}

func applyTimescaleDDL(t *testing.T, sql string) (*ast.CatalogueMutation, error) {
	t.Helper()
	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.Len(t, statements, 1)
	return engine.ApplyDDL(t.Context(), statements[0])
}

func TestCreateHypertable_RegclassCast(t *testing.T) {
	t.Parallel()

	mutation, err := applyTimescaleDDL(t, `SELECT create_hypertable('readings'::regclass, 'ts'::name)`)
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "readings", mutation.TableName)
	assert.Equal(t, "ts", mutation.EngineSpecific["TIMESCALE_TIME_COLUMN"])
}

func TestRetentionPolicy_RegclassCast(t *testing.T) {
	t.Parallel()

	mutation, err := applyTimescaleDDL(t, `SELECT add_retention_policy('conditions'::regclass, drop_after => INTERVAL '24 hours')`)
	require.NoError(t, err)
	require.NotNil(t, mutation)
}

func TestCompressionPolicy_RegclassCast(t *testing.T) {
	t.Parallel()

	mutation, err := applyTimescaleDDL(t, `SELECT add_compression_policy('conditions'::regclass, INTERVAL '7 days')`)
	require.NoError(t, err)
	require.NotNil(t, mutation)
}

func TestCreateHypertable_InlineNamedConstraint(t *testing.T) {
	t.Parallel()

	mutation, err := applyTimescaleDDL(t, `CREATE HYPERTABLE t (val INTEGER CONSTRAINT pos CHECK (val > 0), ts TIMESTAMPTZ NOT NULL)`)
	require.NoError(t, err)
	require.NotNil(t, mutation)
	require.GreaterOrEqual(t, len(mutation.Columns), 2)
	assert.Equal(t, "val", mutation.Columns[0].Name)

	assert.Equal(t, "int4", mutation.Columns[0].SQLType.EngineName)
	assert.Equal(t, sqlmodel.TypeCategoryInteger, mutation.Columns[0].SQLType.Category)
	assert.NotContains(t, mutation.Columns[0].SQLType.EngineName, "CONSTRAINT")

	assert.Equal(t, "ts", mutation.Columns[1].Name)
	assert.Equal(t, "timestamptz", mutation.Columns[1].SQLType.EngineName)
	assert.Equal(t, sqlmodel.TypeCategoryTemporal, mutation.Columns[1].SQLType.Category)
}

func TestApplyDDL_RejectsMalformedTimescaleStatements(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		sql  string
	}{
		{name: "a continuous aggregate missing VIEW", sql: `CREATE MATERIALIZED daily WITH (timescaledb.continuous) AS SELECT 1`},
		{name: "a primary key constraint missing KEY", sql: `CREATE HYPERTABLE readings (ts TIMESTAMPTZ, PRIMARY (ts))`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := timescaledb.New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)

			_, err = engine.ApplyDDL(t.Context(), statements[0])

			assert.Error(t, err)
		})
	}
}

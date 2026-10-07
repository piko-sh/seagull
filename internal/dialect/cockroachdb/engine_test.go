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
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/scaling"
)

type recordingEmitter struct {
	queries []*sqlmodel.AnalysedQuery
}

func (emitter *recordingEmitter) Emit(_ context.Context, request *emit.Request) ([]sqlmodel.GeneratedFile, error) {
	emitter.queries = request.Queries
	return nil, nil
}

func TestNewCockroachDBEngine(t *testing.T) {
	t.Parallel()

	engine := New()

	require.NotNil(t, engine, "engine should not be nil")
	assert.Equal(t, "cockroachdb", engine.Info().Name, "dialect should be cockroachdb")
}

func TestCockroachDB_DDLColumnTypes_StringAndBytes(t *testing.T) {
	t.Parallel()

	engine := New()
	statements, parseError := engine.ParseStatements(t.Context(), "CREATE TABLE t (a STRING, b BYTES, c STRING(50))")
	require.NoError(t, parseError)
	require.NotEmpty(t, statements)

	mutation, applyError := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, applyError)
	require.Len(t, mutation.Columns, 4, "a table without a primary key gains the hidden rowid column")
	assert.True(t, mutation.Columns[3].Hidden)

	assert.Equal(t, sqlmodel.TypeCategoryText, mutation.Columns[0].SQLType.Category, "STRING should be text")
	assert.Equal(t, sqlmodel.TypeCategoryBinary, mutation.Columns[1].SQLType.Category, "BYTES should be bytea")
	assert.Equal(t, sqlmodel.TypeCategoryText, mutation.Columns[2].SQLType.Category, "STRING(50) should be text")
}

func TestCockroachDB_NormaliseTypeName_ExtraTypes(t *testing.T) {
	t.Parallel()

	engine := New()

	catalogue := engine.BuiltinTypes()
	require.NotNil(t, catalogue, "type catalogue should not be nil")

	t.Run("string resolves to text", func(t *testing.T) {
		t.Parallel()

		sqlType, exists := catalogue.Types["string"]
		require.True(t, exists, "type alias 'string' should exist in the type catalogue")
		assert.Equal(t, sqlmodel.TypeCategoryText, sqlType.Category,
			"'string' should resolve to the text category")
		assert.Equal(t, "text", sqlType.EngineName,
			"'string' should resolve to engine name 'text'")
	})

	t.Run("bytes resolves to bytea", func(t *testing.T) {
		t.Parallel()

		sqlType, exists := catalogue.Types["bytes"]
		require.True(t, exists, "type alias 'bytes' should exist in the type catalogue")
		assert.Equal(t, sqlmodel.TypeCategoryBinary, sqlType.Category,
			"'bytes' should resolve to the bytea category")
		assert.Equal(t, "bytea", sqlType.EngineName,
			"'bytes' should resolve to engine name 'bytea'")
	})
}

func TestCockroachDB_SupportsReturning(t *testing.T) {
	t.Parallel()

	engine := New()

	assert.NotZero(t, engine.Info().Returning, "CockroachDB should support RETURNING clauses")
}

func TestCockroachDB_ParameterStyle(t *testing.T) {
	t.Parallel()

	engine := New()

	assert.Equal(t, dialect.PlaceholderSigil{Prefix: "$", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered}, engine.Info().Placeholders.Execution,
		"CockroachDB should use dollar-sign parameter style")
}

func TestCockroachDB_DefaultSchema(t *testing.T) {
	t.Parallel()

	engine := New()

	assert.Equal(t, []string{"public"}, engine.Info().SchemaSearchPath,
		"CockroachDB should have 'public' as the default schema")
}

func TestCockroachDB_BuiltinFunctions_ExtraFunctions(t *testing.T) {
	t.Parallel()

	engine := New()

	catalogue := engine.BuiltinFunctions()
	require.NotNil(t, catalogue, "function catalogue should not be nil")

	type expectedFunction struct {
		key    string
		schema string
	}

	expectedFunctions := []expectedFunction{
		{key: "unique_rowid", schema: ""},
		{key: "cluster_logical_timestamp", schema: ""},
		{key: "cluster_id", schema: crdbInternalSchema},
		{key: "gateway_region", schema: ""},
		{key: "rehome_row", schema: ""},
		{key: "node_id", schema: crdbInternalSchema},
		{key: "locality_value", schema: crdbInternalSchema},
		{key: "from_ip", schema: ""},
		{key: "to_ip", schema: ""},
		{key: "experimental_strftime", schema: ""},
		{key: "experimental_strptime", schema: ""},
		{key: "gen_random_ulid", schema: ""},
		{key: "unordered_unique_rowid", schema: ""},
		{key: "uuid_generate_v4", schema: ""},
		{key: "levenshtein", schema: ""},
		{key: "array_cat_agg", schema: ""},
	}

	for _, function := range expectedFunctions {
		t.Run(function.key, func(t *testing.T) {
			t.Parallel()

			signatures, exists := catalogue.Functions[function.key]
			require.True(t, exists, "function %q should exist in the catalogue", function.key)
			require.NotEmpty(t, signatures, "function %q should have at least one signature", function.key)
			assert.Equal(t, function.schema, signatures[0].Schema,
				"function %q should be registered with schema %q", function.key, function.schema)
		})
	}
}

func TestNewCockroachDBEngineAppliesPostgresOptions(t *testing.T) {
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

			engine := New(testCase.options...)
			statements, err := engine.ParseStatements(t.Context(), "CREATE TABLE t (id INT PRIMARY KEY, name STRING)")
			require.NoError(t, err)

			_, err = engine.ApplyDDL(t.Context(), statements[0])

			assert.Equal(t, "cockroachdb", engine.Info().Name)
			if testCase.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "token budget")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestCockroachDBKeepsItsSettingsAlongsideCallerOptions(t *testing.T) {
	t.Parallel()

	engine := New(
		postgres.WithExtraFunctions(func(builder *postgres.FunctionCatalogueBuilder) {
			builder.NeverNull("caller_function", nil, builder.Text())
		}),
		postgres.WithTypeNormaliserHook(func(string, []int) *sqlmodel.SQLType { return nil }),
	)

	functions := engine.BuiltinFunctions().Functions
	assert.NotEmpty(t, functions["caller_function"])
	assert.NotEmpty(t, functions["unique_rowid"])
	assert.Equal(t, sqlmodel.TypeCategoryText, engine.NormaliseTypeName("STRING").Category)
	assert.Equal(t, sqlmodel.TypeCategoryBinary, engine.NormaliseTypeName("bytes").Category)
	assert.Equal(t, "int8", engine.NormaliseTypeName("INT").EngineName)
}

func TestCockroachDBAddsAHiddenRowIDOnlyWithoutAPrimaryKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sql        string
		wantRowID  bool
		wantColumn int
	}{
		{name: "no primary key", sql: "CREATE TABLE t (a STRING)", wantRowID: true, wantColumn: 2},
		{name: "inline primary key", sql: "CREATE TABLE t (id INT PRIMARY KEY, a STRING)", wantColumn: 2},
		{name: "table primary key", sql: "CREATE TABLE t (id INT, a STRING, PRIMARY KEY (id))", wantColumn: 2},
		{name: "named primary key constraint", sql: "CREATE TABLE t (id INT, CONSTRAINT t_pk PRIMARY KEY (id))", wantColumn: 1},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			mutation, err := engine.ApplyDDL(t.Context(), statements[0])
			require.NoError(t, err)
			require.Len(t, mutation.Columns, testCase.wantColumn)
			last := mutation.Columns[len(mutation.Columns)-1]
			assert.Equal(t, testCase.wantRowID, last.Hidden && last.Name == "rowid")
			if testCase.wantRowID {
				assert.True(t, last.HasDefault)
				assert.False(t, last.Nullable)
				assert.Equal(t, "int8", last.SQLType.EngineName)
			}
		})
	}
}

func TestCockroachDBKeysATableWithoutAPrimaryKeyByItsHiddenRowID(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		wantHidden     string
		wantPrimaryKey []string
	}{
		{name: "no primary key", sql: "CREATE TABLE t (a STRING)", wantHidden: "rowid", wantPrimaryKey: []string{"rowid"}},
		{name: "a declared rowid column", sql: "CREATE TABLE t (rowid INT8, a STRING)",
			wantHidden: "rowid_1", wantPrimaryKey: []string{"rowid_1"}},
		{name: "declared rowid columns", sql: "CREATE TABLE t (a STRING, rowid_1 INT8, ROWID INT8)",
			wantHidden: "rowid_2", wantPrimaryKey: []string{"rowid_2"}},
		{name: "a primary key", sql: "CREATE TABLE t (id INT8 PRIMARY KEY, rowid INT8)", wantHidden: "", wantPrimaryKey: []string{"id"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			mutation, err := engine.ApplyDDL(t.Context(), statements[0])
			require.NoError(t, err)

			hidden := ""
			for index := range mutation.Columns {
				if mutation.Columns[index].Hidden {
					hidden = mutation.Columns[index].Name
				}
			}
			require.Equal(t, testCase.wantHidden, hidden)
			require.Equal(t, testCase.wantPrimaryKey, mutation.PrimaryKey)
		})
	}
}

func TestCockroachDBReplacesTheHiddenKeyWithTheKeyAnAlterTableGives(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		sql            string
		wantPrimaryKey []string
	}{
		{name: "add primary key", sql: "ALTER TABLE t ADD PRIMARY KEY (a)", wantPrimaryKey: []string{"a"}},
		{name: "add a named primary key", sql: "ALTER TABLE t ADD CONSTRAINT t_pkey PRIMARY KEY (a, b)", wantPrimaryKey: []string{"a", "b"}},
		{name: "alter primary key", sql: "ALTER TABLE t ALTER PRIMARY KEY USING COLUMNS (b, a DESC)", wantPrimaryKey: []string{"b", "a"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			mutation, err := engine.ApplyDDL(t.Context(), statements[0])
			require.NoError(t, err)

			require.Equal(t, ast.MutationAlterTableAddConstraint, mutation.Kind)
			require.Equal(t, testCase.wantPrimaryKey, mutation.PrimaryKey)
			require.True(t, mutation.ReplacesHiddenKey)
		})
	}
}

func TestCockroachDBRejectsPostgresPartitionStatements(t *testing.T) {
	t.Parallel()

	for _, sql := range []string{
		"CREATE TABLE m1 PARTITION OF m FOR VALUES FROM ('2026-01-01') TO ('2027-01-01')",
		"ALTER TABLE m ATTACH PARTITION m1 FOR VALUES FROM ('2026-01-01') TO ('2027-01-01')",
		"ALTER TABLE m DETACH PARTITION m1",
	} {
		t.Run(sql, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), sql)
			require.NoError(t, err)
			_, err = engine.ApplyDDL(t.Context(), statements[0])
			require.ErrorContains(t, err, "CockroachDB has no PARTITION OF, ATTACH PARTITION or DETACH PARTITION")
		})
	}
}

func TestCockroachDBKeepsItsOwnPartitioningOnTheTable(t *testing.T) {
	t.Parallel()

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), "CREATE TABLE t (region STRING NOT NULL, id INT NOT NULL, "+
		"PRIMARY KEY (region, id)) PARTITION BY LIST (region) (PARTITION eu VALUES IN ('eu'), PARTITION rest VALUES IN (DEFAULT))")
	require.NoError(t, err)
	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.Equal(t, ast.MutationCreateTable, mutation.Kind)
	require.Nil(t, mutation.PartitionOf)
	require.Len(t, mutation.Columns, 2)
}

func TestCockroachDBAnalysesUpsertAsAnInsert(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		sql        string
		columns    []string
		returning  int
		parameters int
		values     int
	}{
		{name: "rows with a column list and RETURNING", sql: "UPSERT INTO items AS i (id, name) VALUES ($1, $2), ($3, $4) RETURNING i.id, name",
			columns: []string{"id", "name"}, returning: 2, parameters: 4, values: 2},
		{name: "rows without a column list", sql: "upsert into items values ($1, $2, $3)", columns: nil, returning: 0, parameters: 3, values: 1},
		{name: "a SELECT", sql: "UPSERT INTO items (id, name) SELECT id + 10, name FROM items WHERE id = $1 RETURNING id",
			columns: []string{"id", "name"}, returning: 1, parameters: 1, values: 0},
		{name: "after a WITH list", sql: "WITH x AS (SELECT $1::INT8 AS id) UPSERT INTO items (id, name) SELECT id, $2 FROM x RETURNING id",
			columns: []string{"id", "name"}, returning: 1, parameters: 2, values: 0},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)
			analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])
			require.NoError(t, err)
			require.Equal(t, ast.DataChangeInsert, analysis.DataChange)
			require.Equal(t, ast.ConflictActionDoUpdate, analysis.ConflictAction)
			require.Equal(t, "items", analysis.InsertTarget.Name)
			require.Equal(t, testCase.columns, analysis.InsertColumns)
			require.Len(t, analysis.OutputColumns, testCase.returning)
			require.Equal(t, testCase.returning > 0, analysis.HasReturning)
			require.Len(t, analysis.ParameterReferences, testCase.parameters)
			require.Len(t, analysis.Spans.Values, testCase.values)
		})
	}
}

func TestCockroachDBUpsertTakesNoOnConflict(t *testing.T) {
	t.Parallel()

	engine := New()
	statements, err := engine.ParseStatements(t.Context(), "UPSERT INTO items (id) VALUES ($1) ON CONFLICT (id) DO NOTHING")
	require.NoError(t, err)
	_, err = engine.AnalyseQuery(t.Context(), nil, statements[0])
	require.ErrorContains(t, err, "UPSERT takes no ON CONFLICT clause")
}

func TestCockroachDBUpsertParametersTakeTheTargetColumnTypes(t *testing.T) {
	t.Parallel()

	emitter := &recordingEmitter{}
	generator, err := app.New(New(), app.WithEmitter(emitter))
	require.NoError(t, err)
	result, err := generator.Generate(t.Context(), app.Input{
		Package: "crdb",
		Migrations: fstest.MapFS{
			"migrations/001_items.up.sql": {Data: []byte("CREATE TABLE items (id INT8 PRIMARY KEY, name STRING NOT NULL);")},
			"queries/items.sql": {Data: []byte("-- sgl.query(name: SaveItem, command: one)\n" +
				"UPSERT INTO items (id, name) VALUES ($1, $2) RETURNING id, name;")},
		},
		MigrationDir: "migrations",
		QueryDir:     "queries",
	})
	require.NoError(t, err)
	require.Empty(t, result.Diagnostics)
	require.Len(t, emitter.queries, 1)
	query := emitter.queries[0]
	require.Len(t, query.Parameters, 2)
	require.Equal(t, "id", query.Parameters[0].Name)
	require.Equal(t, "int8", query.Parameters[0].SQLType.EngineName)
	require.Equal(t, "name", query.Parameters[1].Name)
	require.Equal(t, sqlmodel.TypeCategoryText, query.Parameters[1].SQLType.Category)
	require.Len(t, query.OutputColumns, 2)
}

func TestCockroachDBDividesIntegersExactly(t *testing.T) {
	t.Parallel()

	engine := New()
	integer := engine.NormaliseTypeName("int")

	result, behaviour, typed := engine.BinaryResult("/", integer, integer)
	require.True(t, typed)
	require.Equal(t, sqlmodel.TypeCategoryDecimal, result.Category)
	require.Equal(t, sqlmodel.FunctionNullableReturnsNullOnNull, behaviour)

	_, _, typed = postgres.New().BinaryResult("/", integer, integer)
	require.False(t, typed, "PostgreSQL truncates an integer quotient, the structural default")
}

func TestCockroachDBDateDifferenceIsItsIntegerType(t *testing.T) {
	t.Parallel()

	engine := New()
	date := engine.NormaliseTypeName("date")

	result, _, typed := engine.BinaryResult("-", date, date)
	require.True(t, typed)
	require.Equal(t, "int8", result.EngineName)
}

func TestCockroachDBReadsInlineIndexAndFamilyElementsAsNoColumns(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		sql             string
		expectedColumns []string
		uniqueColumns   []string
	}{
		{
			name:            "named index",
			sql:             "CREATE TABLE t (id INT PRIMARY KEY, a STRING NOT NULL, INDEX t_a_idx (a))",
			expectedColumns: []string{"id", "a"},
		},
		{
			name:            "unnamed index with storing, a predicate and visibility",
			sql:             "CREATE TABLE t (id INT PRIMARY KEY, a INT, b STRING, INDEX (a DESC) STORING (b) WHERE a > 0 NOT VISIBLE)",
			expectedColumns: []string{"id", "a", "b"},
		},
		{
			name:            "hash-sharded index",
			sql:             "CREATE TABLE t (id INT PRIMARY KEY, ts TIMESTAMP, INDEX t_ts_idx (ts) USING HASH WITH (bucket_count = 8))",
			expectedColumns: []string{"id", "ts"},
		},
		{
			name:            "unique index",
			sql:             "CREATE TABLE t (id INT PRIMARY KEY, email STRING NOT NULL, UNIQUE INDEX t_email_key (email))",
			expectedColumns: []string{"id", "email"},
			uniqueColumns:   []string{"email"},
		},
		{
			name:            "inverted index",
			sql:             "CREATE TABLE t (id INT PRIMARY KEY, doc JSONB, INVERTED INDEX t_doc_idx (doc))",
			expectedColumns: []string{"id", "doc"},
		},
		{
			name:            "column families",
			sql:             `CREATE TABLE t (id INT PRIMARY KEY, a STRING, b STRING, FAMILY "primary" (id, a), FAMILY f2 (b))`,
			expectedColumns: []string{"id", "a", "b"},
		},
		{
			name:            "columns named index and family",
			sql:             `CREATE TABLE t (id INT PRIMARY KEY, "index" INT, family STRING)`,
			expectedColumns: []string{"id", "index", "family"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)
			mutation, err := engine.ApplyDDL(t.Context(), statements[0])
			require.NoError(t, err)

			var names []string
			for index := range mutation.Columns {
				names = append(names, mutation.Columns[index].Name)
			}
			require.Equal(t, testCase.expectedColumns, names)
			if testCase.uniqueColumns != nil {
				require.Len(t, mutation.Constraints, 1)
				require.Equal(t, sqlmodel.ConstraintUnique, mutation.Constraints[0].Kind)
				require.Equal(t, testCase.uniqueColumns, mutation.Constraints[0].Columns)
			}
		})
	}
}

func TestPostgreSQLKeepsColumnsNamedIndexAndFamily(t *testing.T) {
	t.Parallel()

	engine := postgres.New()
	statements, err := engine.ParseStatements(t.Context(), "CREATE TABLE t (id int PRIMARY KEY, index int, family text)")
	require.NoError(t, err)
	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.Len(t, mutation.Columns, 3)
	require.Equal(t, "index", mutation.Columns[1].Name)
	require.Equal(t, "family", mutation.Columns[2].Name)
}

func TestCockroachDBLeavesOutFunctionsItLacks(t *testing.T) {
	t.Parallel()

	functions := New().BuiltinFunctions().Functions
	for _, name := range []string{"regexp_count", "pg_advisory_lock", "pg_notify", "date_bin", "make_interval", "int4range"} {
		require.NotContains(t, functions, name, "CockroachDB has no %s", name)
	}
	for _, name := range []string{"regexp_replace", "gen_random_uuid", "pg_try_advisory_lock", "jsonb_array_elements"} {
		require.Contains(t, functions, name, "CockroachDB has %s", name)
	}
}

func TestCockroachDBTypesFunctionsAsItReturnsThem(t *testing.T) {
	t.Parallel()

	functions := New().BuiltinFunctions().Functions
	testCases := []struct {
		name     string
		function string
		want     string
	}{
		{name: "a SHA digest is hexadecimal text", function: "sha256", want: "text"},
		{name: "a NULL count is an INT8", function: "num_nulls", want: "int8"},
		{name: "pg_sleep returns a boolean", function: "pg_sleep", want: "bool"},
		{name: "levenshtein is an INT8", function: "levenshtein", want: "int8"},
		{name: "gen_random_ulid is a UUID", function: "gen_random_ulid", want: "uuid"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.NotEmpty(t, functions[testCase.function])
			for _, signature := range functions[testCase.function] {
				require.Equal(t, testCase.want, signature.ReturnType.EngineName)
			}
		})
	}
}

func TestCockroachDBReadsAsOfSystemTimeAfterTheFromClause(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		sql        string
		parameters int
	}{
		{name: "a historical read", sql: "SELECT id, name FROM items AS OF SYSTEM TIME '-10s' WHERE id = $1", parameters: 1},
		{name: "a follower read", sql: "SELECT count(*) FROM items AS OF SYSTEM TIME follower_read_timestamp()", parameters: 0},
		{name: "a bounded staleness read", sql: "SELECT name FROM items AS OF SYSTEM TIME with_max_staleness('10s') WHERE id = $1", parameters: 1},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)
			analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])
			require.NoError(t, err)
			require.True(t, analysis.ReadOnly)
			require.Equal(t, []ast.TableReference{{Schema: "", Name: "items", Alias: ""}}, analysis.FromTables)
			require.Len(t, analysis.ParameterReferences, testCase.parameters)
		})
	}
}

func TestCockroachDBWarnsOnlyOfAParameterMetFirstInATestForNull(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		sql   string
		warns int
	}{
		{name: "an optional filter tested before it is compared", sql: "SELECT id FROM items WHERE ($1 IS NULL OR name = $1)", warns: 1},
		{name: "a pattern built by concat", sql: "SELECT id FROM items WHERE name LIKE concat('%', $1, '%')", warns: 0},
		{name: "an offset multiplying two parameters", sql: "SELECT id FROM items ORDER BY id LIMIT $1 OFFSET $1 * $2", warns: 0},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)
			analysis, err := engine.AnalyseQuery(t.Context(), nil, statements[0])
			require.NoError(t, err)
			require.Len(t, analysis.Warnings, testCase.warns)
		})
	}
}

func TestCockroachDB_CrdbInternalFunctions_Resolve(t *testing.T) {
	t.Parallel()

	const queryContent = "-- sgl.query(name: NodeInfo, command: one)\n" +
		"SELECT crdb_internal.node_id() AS node, crdb_internal.cluster_id() AS cluster;\n"

	caseDirectory := writeResolutionFixture(t, "-- no schema needed\n", queryContent)

	queries, diagnostics := analyseResolutionFixture(t, caseDirectory)

	for _, diagnostic := range diagnostics {
		assert.NotEqual(t, diag.CodeUnknownFunction, diagnostic.Code,
			"crdb_internal builtins must resolve; got unknown-function diagnostic: %s", diagnostic.Message)
	}

	require.Len(t, queries, 1, "exactly one analysed query expected")
	columns := queries[0].OutputColumns
	require.Len(t, columns, 2, "two output columns expected")

	nodeColumn := findColumn(t, columns, "node")
	assert.Equal(t, sqlmodel.TypeCategoryInteger, nodeColumn.SQLType.Category,
		"crdb_internal.node_id() should resolve to an integer column, not Unknown/any")
	assert.Equal(t, "int8", nodeColumn.SQLType.EngineName,
		"crdb_internal.node_id() should resolve to int8")

	clusterColumn := findColumn(t, columns, "cluster")
	assert.Equal(t, sqlmodel.TypeCategoryUUID, clusterColumn.SQLType.Category,
		"crdb_internal.cluster_id() should resolve to a uuid column, not Unknown/any")
	assert.Equal(t, "uuid", clusterColumn.SQLType.EngineName,
		"crdb_internal.cluster_id() should resolve to uuid")
}

func writeResolutionFixture(t *testing.T, migrationContent string, queryContent string) string {
	t.Helper()

	caseDirectory := t.TempDir()
	migrationDirectory := filepath.Join(caseDirectory, "migrations")
	queryDirectory := filepath.Join(caseDirectory, "queries")
	require.NoError(t, os.MkdirAll(migrationDirectory, 0o755))
	require.NoError(t, os.MkdirAll(queryDirectory, 0o755))

	require.NoError(t, os.WriteFile(
		filepath.Join(migrationDirectory, "001_schema.up.sql"), []byte(migrationContent), 0o644))
	require.NoError(t, os.WriteFile(
		filepath.Join(queryDirectory, "queries.sql"), []byte(queryContent), 0o644))

	return caseDirectory
}

func analyseResolutionFixture(
	t *testing.T,
	caseDirectory string,
) ([]*sqlmodel.AnalysedQuery, []diag.SourceError) {
	t.Helper()

	generator, err := app.New(New())
	require.NoError(t, err)

	report, err := generator.Vet(t.Context(), app.Input{
		Migrations:   os.DirFS(caseDirectory),
		MigrationDir: "migrations",
		QueryDir:     "queries",
	})
	require.NoError(t, err)

	return report.Queries, report.Diagnostics
}

func findColumn(t *testing.T, columns []sqlmodel.OutputColumn, name string) sqlmodel.OutputColumn {
	t.Helper()

	for _, column := range columns {
		if column.Name == name {
			return column
		}
	}

	require.FailNowf(t, "output column not found", "no output column is named %q", name)
	return sqlmodel.OutputColumn{}
}

func TestAddedPrimaryKeysReplaceTheHiddenOne(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantReplace bool
	}{
		{name: "ALTER PRIMARY KEY", sql: "ALTER TABLE t ALTER PRIMARY KEY USING COLUMNS (a)", wantReplace: true},
		{name: "ADD PRIMARY KEY", sql: "ALTER TABLE t ADD PRIMARY KEY (a)", wantReplace: true},
		{name: "ADD of a unique constraint", sql: "ALTER TABLE t ADD UNIQUE (a)", wantReplace: false},
		{name: "ALTER of a column", sql: "ALTER TABLE t ALTER COLUMN a SET NOT NULL", wantReplace: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			mutation, err := engine.ApplyDDL(t.Context(), statements[0])
			require.NoError(t, err)

			assert.Equal(t, testCase.wantReplace, mutation.ReplacesHiddenKey)
		})
	}
}

func TestNamingTheHiddenRowIDIsLinearInTheDeclaredColumns(t *testing.T) {
	t.Parallel()

	statement := func(columns int) string {
		var builder strings.Builder
		builder.WriteString("CREATE TABLE t (rowid INT8")
		for suffix := 1; suffix < columns; suffix++ {
			builder.WriteString(", rowid_" + strconv.Itoa(suffix) + " INT8")
		}
		builder.WriteString(")")
		return builder.String()
	}
	engine := New()
	apply := func(sql string, wantName string) func() {
		return func() {
			statements, err := engine.ParseStatements(t.Context(), sql)
			require.NoError(t, err)
			mutation, err := engine.ApplyDDL(t.Context(), statements[0])
			require.NoError(t, err)
			require.Equal(t, []string{wantName}, mutation.PrimaryKey)
		}
	}

	scaling.Linear(t, apply(statement(2_000), "rowid_2000"), apply(statement(8_000), "rowid_8000"))
}

func TestWidenRealAcrossCategoriesGivesFloat8(t *testing.T) {
	t.Parallel()

	realType := sqlmodel.NewFloatType("float4", 32)
	double := sqlmodel.NewFloatType(engineNameFloat8, widthFloat8)
	integer := sqlmodel.NewIntegerType(engineNameInt8, widthInt8, false)
	numeric := sqlmodel.NewSQLType(sqlmodel.TypeCategoryDecimal, "numeric")

	widened := widenRealAcrossCategories(integer, realType)
	require.NotNil(t, widened)
	assert.Equal(t, "float8", widened.EngineName)
	require.NotNil(t, widenRealAcrossCategories(realType, numeric))
	assert.Nil(t, widenRealAcrossCategories(double, numeric), "a FLOAT8 needs no widening")
	assert.Nil(t, widenRealAcrossCategories(integer, numeric), "no float, no widening")
	assert.Nil(t, widenRealAcrossCategories(realType, realType), "the same category is left to PostgreSQL's rule")
}

func TestRetypedMathematicalFunctionsComputeInCockroachDBTypes(t *testing.T) {
	t.Parallel()

	engine := New()
	smallint := engine.NormaliseTypeName("smallint")
	realType := engine.NormaliseTypeName("real")
	functions := engine.BuiltinFunctions().Functions

	for _, name := range []string{"abs", "sign", "mod", "power", "pow", "cbrt"} {
		require.NotEmpty(t, functions[name], name)
		for _, overload := range functions[name] {
			assert.NotEqual(t, smallint.EngineName, overload.ReturnType.EngineName, "%s returns no SMALLINT", name)
			assert.NotEqual(t, realType.EngineName, overload.ReturnType.EngineName, "%s returns no REAL", name)
		}
	}
}

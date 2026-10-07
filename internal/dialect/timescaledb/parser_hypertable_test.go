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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/dialect/timescaledb"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/testkit/scaling"
)

func TestParseCreateHypertable_Keyword_Basic(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), "CREATE HYPERTABLE readings (ts TIMESTAMPTZ NOT NULL, device_id BIGINT, temperature DOUBLE PRECISION)")
	require.NoError(t, err)
	require.Len(t, statements, 1)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
	assert.Equal(t, "readings", mutation.TableName)
	assert.Equal(t, "true", mutation.EngineSpecific["TIMESCALE_HYPERTABLE"])

	require.Len(t, mutation.Columns, 3)
	assert.Equal(t, "ts", mutation.Columns[0].Name)
	assert.False(t, mutation.Columns[0].Nullable, "ts is declared NOT NULL")
	assert.Equal(t, "device_id", mutation.Columns[1].Name)
	assert.Equal(t, "temperature", mutation.Columns[2].Name)
}

func TestParseCreateHypertable_Keyword_IfNotExists(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), "CREATE HYPERTABLE IF NOT EXISTS metrics (ts TIMESTAMPTZ, value DOUBLE PRECISION)")
	require.NoError(t, err)
	require.Len(t, statements, 1)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "metrics", mutation.TableName)
	assert.Equal(t, "true", mutation.EngineSpecific["TIMESCALE_IF_NOT_EXISTS"])
}

func TestParseCreateHypertable_Keyword_SchemaQualified(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), "CREATE HYPERTABLE analytics.events (ts TIMESTAMPTZ, kind TEXT)")
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "analytics", mutation.SchemaName)
	assert.Equal(t, "events", mutation.TableName)
}

func TestParseCreateHypertable_FunctionCallForm(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), "SELECT create_hypertable('readings', 'ts')")
	require.NoError(t, err)
	require.Len(t, statements, 1)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)

	assert.Equal(t, ast.MutationAlterTableAlterColumn, mutation.Kind)
	assert.Equal(t, "readings", mutation.TableName)
	assert.Equal(t, "ts", mutation.EngineSpecific["TIMESCALE_TIME_COLUMN"])
	assert.Equal(t, "true", mutation.EngineSpecific["TIMESCALE_ANNOTATE_ONLY"])
}

func TestParseCreateHypertable_FunctionCallForm_WithExtras(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), `SELECT create_hypertable('readings', 'ts', chunk_time_interval => INTERVAL '1 day')`)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "readings", mutation.TableName)
	assert.NotEmpty(t, mutation.EngineSpecific["TIMESCALE_CALL_EXTRAS"], "extras should capture chunk_time_interval clause")
}

func TestParseCreateHypertable_CallExtrasRoundTripsNonPlainLiterals(t *testing.T) {
	t.Parallel()

	cases := []struct {
		description string
		extras      string
		mustContain string
	}{
		{
			description: "bit-string literal keeps its B prefix and quotes",
			extras:      `'readings', 'ts', flags => B'1010'`,
			mustContain: "B'1010'",
		},
		{
			description: "dollar-quoted literal keeps its own tag",
			extras:      `'readings', 'ts', note => $tag$body$tag$`,
			mustContain: "$tag$body$tag$",
		},
		{
			description: "quoted identifier keeps its double quotes",
			extras:      `'readings', 'ts', "Weird Param" => 1`,
			mustContain: `"Weird Param"`,
		},
	}

	engine := timescaledb.New()

	for _, testCase := range cases {
		t.Run(testCase.description, func(t *testing.T) {
			t.Parallel()

			statements, err := engine.ParseStatements(t.Context(), "SELECT create_hypertable("+testCase.extras+")")
			require.NoError(t, err)
			require.Len(t, statements, 1)

			mutation, err := engine.ApplyDDL(t.Context(), statements[0])
			require.NoError(t, err)
			require.NotNil(t, mutation)

			extras := mutation.EngineSpecific["TIMESCALE_CALL_EXTRAS"]
			assert.Contains(t, extras, testCase.mustContain,
				"captured call extras must re-wrap non-plain literals with their delimiters, got %q", extras)
		})
	}
}

func TestParseCreateHypertable_InlinePrimaryKey(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), "CREATE HYPERTABLE readings (id BIGINT PRIMARY KEY, ts TIMESTAMPTZ NOT NULL)")
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, []string{"id"}, mutation.PrimaryKey)
	require.Len(t, mutation.Columns, 2)
	assert.False(t, mutation.Columns[0].Nullable, "PRIMARY KEY implies NOT NULL")
}

func TestParseCreateHypertable_InlineConstraintsAfterDefaultAndReferences(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()

	statements, err := engine.ParseStatements(t.Context(),
		"CREATE HYPERTABLE readings ("+
			"id BIGINT DEFAULT 1 PRIMARY KEY, "+
			"ts TIMESTAMPTZ DEFAULT now() NOT NULL, "+
			"device_id BIGINT REFERENCES devices(id) ON DELETE SET NULL NOT NULL, "+
			"score INT CHECK (score >= 0) NOT NULL)",
	)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	require.Len(t, mutation.Columns, 4)

	assert.Equal(t, "id", mutation.Columns[0].Name)
	assert.Equal(t, []string{"id"}, mutation.PrimaryKey, "DEFAULT 1 PRIMARY KEY must keep the PK")
	assert.False(t, mutation.Columns[0].Nullable, "PRIMARY KEY implies NOT NULL")

	assert.Equal(t, "ts", mutation.Columns[1].Name)
	assert.True(t, mutation.Columns[1].HasDefault)
	assert.False(t, mutation.Columns[1].Nullable, "DEFAULT now() NOT NULL must record NOT NULL")

	assert.Equal(t, "device_id", mutation.Columns[2].Name)
	assert.False(t, mutation.Columns[2].Nullable, "trailing NOT NULL after ON DELETE SET NULL must survive")

	assert.Equal(t, "score", mutation.Columns[3].Name)
	assert.False(t, mutation.Columns[3].Nullable, "trailing NOT NULL after CHECK(...) must survive")
}

func TestParseCreateHypertable_TablePrimaryKeyMultiColumn(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), "CREATE HYPERTABLE readings (ts TIMESTAMPTZ, device_id BIGINT, PRIMARY KEY (ts, device_id))")
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, []string{"ts", "device_id"}, mutation.PrimaryKey)
}

func TestParseCreateHypertable_NamedPrimaryKeyConstraint(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), "CREATE HYPERTABLE readings (ts TIMESTAMPTZ, device_id BIGINT, CONSTRAINT readings_pk PRIMARY KEY (ts, device_id))")
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, []string{"ts", "device_id"}, mutation.PrimaryKey)
}

func TestParseCreateHypertable_TablePKOverridesInlinePK(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), "CREATE HYPERTABLE readings (id BIGINT PRIMARY KEY, ts TIMESTAMPTZ, PRIMARY KEY (ts))")
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, []string{"ts"}, mutation.PrimaryKey)
}

func TestParseCreateHypertable_TrailingPartitionBy(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), "CREATE HYPERTABLE readings (ts TIMESTAMPTZ, value DOUBLE PRECISION) PARTITION BY RANGE (ts)")
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Contains(t, mutation.EngineSpecific["TIMESCALE_TRAILING"], "PARTITION")
}

func TestParseCreateHypertable_FunctionCallForm_SchemaQualified(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), "SELECT create_hypertable('analytics.readings', 'ts')")
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "analytics", mutation.SchemaName)
	assert.Equal(t, "readings", mutation.TableName)
}

func TestParseCreateHypertable_FunctionCallForm_DoesNotClaimBareIdentifier(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), "SELECT create_hypertable FROM hypertable_registry")
	require.NoError(t, err)
	require.NotEmpty(t, statements)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])

	require.NoError(t, err)
	if mutation != nil {
		assert.Empty(t, mutation.EngineSpecific["TIMESCALE_HYPERTABLE"])
	}
}

func TestParseCreateHypertable_DoesNotAffectRegularCreateTable(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), "CREATE TABLE widgets (id BIGINT, name TEXT)")
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, ast.MutationCreateTable, mutation.Kind)
	assert.Equal(t, "widgets", mutation.TableName)
	assert.Empty(t, mutation.EngineSpecific["TIMESCALE_HYPERTABLE"], "regular CREATE TABLE should not carry hypertable marker")
}

func TestCreateHypertable_PrimaryKeyWithoutKeyKeywordIsAnError(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantMessage string
	}{
		{name: "bare PRIMARY", sql: "CREATE HYPERTABLE readings (ts TIMESTAMPTZ NOT NULL, PRIMARY (ts))", wantMessage: "expected ',' or ')' after the table element"},
		{name: "named constraint PRIMARY", sql: "CREATE HYPERTABLE readings (ts TIMESTAMPTZ NOT NULL, CONSTRAINT pk PRIMARY (ts))", wantMessage: "expected keyword KEY"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := timescaledb.New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)

			_, err = engine.ApplyDDL(t.Context(), statements[0])

			require.NotErrorIs(t, err, fault.ErrPanic)
			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, testCase.wantMessage, parseError.Message)
		})
	}
}

func TestCreateHypertableDeclaresTheTableCreateTableDeclares(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		elements string
	}{
		{name: "serial columns", elements: "(id serial, small smallserial, big bigserial PRIMARY KEY, ts timestamptz NOT NULL)"},
		{name: "identity columns", elements: "(id bigint GENERATED BY DEFAULT AS IDENTITY, other int GENERATED ALWAYS AS IDENTITY (START WITH 5 INCREMENT BY 2) PRIMARY KEY, ts timestamptz)"},
		{name: "stored generated columns", elements: "(celsius float8, fahrenheit float8 GENERATED ALWAYS AS (celsius * 9 / 5 + 32) STORED, ts timestamptz)"},
		{name: "defaults followed by constraints", elements: "(ts timestamptz DEFAULT now() NOT NULL, n int DEFAULT (1 + 2) CHECK (n > 0) NOT NULL, label text DEFAULT NULL COLLATE \"C\")"},
		{name: "foreign keys and their actions", elements: "(device int REFERENCES devices (id) MATCH SIMPLE ON DELETE SET NULL ON UPDATE NO ACTION NOT DEFERRABLE INITIALLY IMMEDIATE NOT NULL, site int REFERENCES sites ON DELETE SET DEFAULT (site) ON UPDATE RESTRICT, ts timestamptz)"},
		{name: "table constraints", elements: "(ts timestamptz, device int, CONSTRAINT readings_pk PRIMARY KEY (ts, device), UNIQUE (device, ts), CHECK (device > 0), FOREIGN KEY (device) REFERENCES devices (id) ON DELETE CASCADE, EXCLUDE USING gist (device WITH =))"},
		{name: "a table key over an inline key", elements: "(id bigint PRIMARY KEY, ts timestamptz, PRIMARY KEY (ts))"},
		{name: "a LIKE element including everything", elements: "(LIKE template INCLUDING ALL, extra text)"},
		{name: "a key over a column a LIKE element copies", elements: "(LIKE template EXCLUDING CONSTRAINTS, PRIMARY KEY (ts))"},
		{name: "arrays, qualified and multi-word types", elements: "(tags text[], grid int[][], amount numeric(10, 2), at timestamp (3) with time zone, kind public.mood, ratio double precision)"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := timescaledb.New()
			table := applyOne(t, engine, "CREATE TABLE metrics.readings "+testCase.elements)
			hypertable := applyOne(t, engine, "CREATE HYPERTABLE metrics.readings "+testCase.elements)

			assert.Equal(t, table.Columns, hypertable.Columns)
			assert.Equal(t, table.PrimaryKey, hypertable.PrimaryKey)
			assert.Equal(t, table.Constraints, hypertable.Constraints)
			assert.Equal(t, table.CopiedTables, hypertable.CopiedTables)
			assert.Equal(t, table.AdditionalMutations, hypertable.AdditionalMutations)
			assert.Equal(t, "true", hypertable.EngineSpecific["TIMESCALE_HYPERTABLE"])
		})
	}
}

func TestCreateHypertableTypesSerialAndIdentityKeysAsNotNullWithDefaults(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		sql             string
		wantNullable    bool
		wantHasDefault  bool
		wantIsGenerated bool
	}{
		{name: "a serial key", sql: "CREATE HYPERTABLE readings (id serial PRIMARY KEY, ts timestamptz)", wantNullable: false, wantHasDefault: true},
		{name: "a bigserial column", sql: "CREATE HYPERTABLE readings (id bigserial, ts timestamptz)", wantNullable: false, wantHasDefault: true},
		{name: "a by-default identity", sql: "CREATE HYPERTABLE readings (id bigint GENERATED BY DEFAULT AS IDENTITY, ts timestamptz)", wantNullable: false, wantHasDefault: true},
		{name: "an always identity", sql: "CREATE HYPERTABLE readings (id bigint GENERATED ALWAYS AS IDENTITY, ts timestamptz)", wantNullable: false, wantHasDefault: true},
		{name: "a stored generated column", sql: "CREATE HYPERTABLE readings (id bigint GENERATED ALWAYS AS (1) STORED, ts timestamptz)", wantNullable: true, wantIsGenerated: true},
		{name: "a plain column", sql: "CREATE HYPERTABLE readings (id bigint, ts timestamptz)", wantNullable: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyOne(t, timescaledb.New(), testCase.sql)

			require.NotEmpty(t, mutation.Columns)
			column := mutation.Columns[0]
			assert.Equal(t, testCase.wantNullable, column.Nullable)
			assert.Equal(t, testCase.wantHasDefault, column.HasDefault)
			assert.Equal(t, testCase.wantIsGenerated, column.IsGenerated)
		})
	}
}

func TestCreateHypertableKeepsWhatFollowsItsElementList(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		sql              string
		wantTrailing     string
		wantBody         string
		wantIfNotExists  bool
		wantColumnsCount int
	}{
		{name: "storage parameters", sql: "CREATE HYPERTABLE r (ts timestamptz) WITH (fillfactor = 70, tsdb.chunk_interval = '1 day')", wantTrailing: "WITH (fillfactor = 70, tsdb.chunk_interval = '1 day')", wantColumnsCount: 1},
		{name: "a partitioning clause", sql: "CREATE HYPERTABLE r (ts timestamptz) PARTITION BY RANGE (ts)", wantTrailing: "PARTITION BY RANGE (ts)", wantColumnsCount: 1},
		{name: "IF NOT EXISTS after the keyword", sql: "CREATE HYPERTABLE IF NOT EXISTS r (ts timestamptz)", wantIfNotExists: true, wantColumnsCount: 1},
		{name: "IF NOT EXISTS before the keyword", sql: "CREATE IF NOT EXISTS HYPERTABLE r (ts timestamptz)", wantIfNotExists: true, wantColumnsCount: 1},
		{name: "a clone of another table", sql: "CREATE HYPERTABLE r FROM source_table", wantBody: "FROM source_table"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mutation := applyOne(t, timescaledb.New(), testCase.sql)

			assert.Equal(t, testCase.wantTrailing, mutation.EngineSpecific["TIMESCALE_TRAILING"])
			assert.Equal(t, testCase.wantBody, mutation.EngineSpecific["TIMESCALE_BODY"])
			assert.Equal(t, testCase.wantIfNotExists, mutation.IfNotExists)
			assert.Len(t, mutation.Columns, testCase.wantColumnsCount)
		})
	}
}

func TestMalformedCreateHypertableListsAreLocatedSyntaxErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		sql         string
		wantMessage string
	}{
		{name: "a list cut off after a column", sql: "CREATE HYPERTABLE r (ts timestamptz,", wantMessage: "expected a column definition"},
		{name: "a list cut off inside a type", sql: "CREATE HYPERTABLE r (ts timestamptz NOT", wantMessage: "expected ',' or ')' after the table element"},
		{name: "a check that is never closed", sql: "CREATE HYPERTABLE r (n int CHECK ((n > 0)", wantMessage: "unmatched parenthesis"},
		{name: "a generated column without its expression", sql: "CREATE HYPERTABLE r (n int GENERATED ALWAYS AS, ts timestamptz)", wantMessage: "expected '('"},
		{name: "an identity without BY DEFAULT or ALWAYS", sql: "CREATE HYPERTABLE r (n int GENERATED AS IDENTITY)", wantMessage: "expected keyword BY"},
		{name: "a foreign key action that is no action", sql: "CREATE HYPERTABLE r (d int REFERENCES devices ON DELETE EXPLODE)", wantMessage: "expected a referential action"},
		{name: "a foreign key that names no table", sql: "CREATE HYPERTABLE r (d int REFERENCES)", wantMessage: "expected identifier"},
		{name: "a table constraint that is unbalanced", sql: "CREATE HYPERTABLE r (d int, UNIQUE (d)", wantMessage: "expected ',' or ')' after the table element"},
		{name: "a primary key list that is never closed", sql: "CREATE HYPERTABLE r (d int, PRIMARY KEY (d", wantMessage: "expected ')' to close the column list"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := timescaledb.New()
			statements, err := engine.ParseStatements(t.Context(), testCase.sql)
			require.NoError(t, err)
			require.Len(t, statements, 1)

			_, err = engine.ApplyDDL(t.Context(), statements[0])

			require.NotErrorIs(t, err, fault.ErrPanic)
			var parseError *dialectkit.ParseError
			require.ErrorAs(t, err, &parseError)
			assert.Equal(t, testCase.wantMessage, parseError.Message)
		})
	}
}

func TestCreateHypertableAcceptsChecksNestedDeeperThanAnyCaptureLimit(t *testing.T) {
	t.Parallel()

	statement := func(depth int) string {
		return "CREATE HYPERTABLE r (n int CHECK " + strings.Repeat("(", depth) + "n > 0" + strings.Repeat(")", depth) + ", ts timestamptz)"
	}
	engine := timescaledb.New()
	apply := func(sql string) func() {
		return func() {
			mutation := applyOne(t, engine, sql)
			require.Len(t, mutation.Columns, 2)
		}
	}

	apply(statement(80))()
	scaling.Linear(t, apply(statement(4_000)), apply(statement(16_000)))
}

func applyOne(t *testing.T, engine *postgres.Dialect, sql string) *ast.CatalogueMutation {
	t.Helper()

	statements, err := engine.ParseStatements(t.Context(), sql)
	require.NoError(t, err)
	require.Len(t, statements, 1)
	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	return mutation
}

func TestCreateHypertable_ByRangeBuilder(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), `SELECT create_hypertable('readings', by_range('time', INTERVAL '1 day'))`)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "by_range", mutation.EngineSpecific["TIMESCALE_DIMENSION_BUILDER"])
	assert.Equal(t, "time", mutation.EngineSpecific["TIMESCALE_TIME_COLUMN"])
	assert.Equal(t, "readings", mutation.TableName)
}

func TestCreateHypertable_ByHashBuilder(t *testing.T) {
	t.Parallel()

	engine := timescaledb.New()
	statements, err := engine.ParseStatements(t.Context(), `SELECT create_hypertable('readings', by_hash('device_id', 4))`)
	require.NoError(t, err)

	mutation, err := engine.ApplyDDL(t.Context(), statements[0])
	require.NoError(t, err)
	require.NotNil(t, mutation)
	assert.Equal(t, "by_hash", mutation.EngineSpecific["TIMESCALE_DIMENSION_BUILDER"])
	assert.Equal(t, "device_id", mutation.EngineSpecific["TIMESCALE_TIME_COLUMN"])
}

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

package facade_test

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_clickhouse"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_duckdb"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_mysql"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_postgres"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
	"seagull.sh/seagull/sdk/emit"
)

const ordersMigration = `CREATE TABLE orders (
    id       INTEGER PRIMARY KEY,
    customer TEXT NOT NULL,
    title    TEXT
);`

var namedParameterDialects = map[string]func() dialect.Dialect{
	"sqlite":   func() dialect.Dialect { return dialect_engine_sqlite.New() },
	"postgres": func() dialect.Dialect { return dialect_engine_postgres.New() },
	"mysql":    func() dialect.Dialect { return dialect_engine_mysql.New() },
	"duckdb":   func() dialect.Dialect { return dialect_engine_duckdb.New() },
}

func ordersInput(query string) seagull.Input {
	return seagull.Input{
		Package: "store",
		Migrations: fstest.MapFS{
			"db/migrations/0001_init.up.sql": &fstest.MapFile{Data: []byte(ordersMigration)},
			"db/queries/orders.sql":          &fstest.MapFile{Data: []byte(query)},
		},
		MigrationDir: "db/migrations",
		QueryDir:     "db/queries",
	}
}

func vetOrders(t *testing.T, engine dialect.Dialect, query string) *seagull.Report {
	t.Helper()
	generator, err := seagull.New(engine)
	require.NoError(t, err)
	report, err := generator.Vet(t.Context(), ordersInput(query))
	require.NoError(t, err)
	return report
}

func diagnosticCodesOf(diagnostics []seagull.SourceError) []string {
	codes := make([]string, 0, len(diagnostics))
	for index := range diagnostics {
		codes = append(codes, diagnostics[index].Code+" "+diagnostics[index].Message)
	}
	return codes
}

func parametersByName(t *testing.T, report *seagull.Report) map[string]emit.QueryParameter {
	t.Helper()
	require.Len(t, report.Queries, 1)
	parameters := make(map[string]emit.QueryParameter, len(report.Queries[0].Parameters))
	for _, parameter := range report.Queries[0].Parameters {
		parameters[parameter.Name] = parameter
	}
	return parameters
}

func TestNamedPlaceholderRenamedByParamIsReferenced(t *testing.T) {
	t.Parallel()

	for name, newDialect := range namedParameterDialects {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			report := vetOrders(t, newDialect(), `-- sgl.query(OrdersFor, many)
-- :customer as sgl.param(buyer)
SELECT id, customer FROM orders WHERE customer = :customer;
`)

			require.Empty(t, diagnosticCodesOf(report.Diagnostics))
			parameters := parametersByName(t, report)
			require.Len(t, parameters, 1)
			require.Contains(t, parameters, "buyer")
			require.Equal(t, dialect.TypeCategoryText, parameters["buyer"].SQLType.Category)
		})
	}
}

func TestNamedPlaceholderInfersItsNameBesideKeywordArguments(t *testing.T) {
	t.Parallel()

	for name, newDialect := range namedParameterDialects {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			report := vetOrders(t, newDialect(), `-- sgl.query(OrdersByTitle, many)
-- :title as sgl.param(optional: true)
SELECT id, customer FROM orders WHERE title = :title;
`)

			require.Empty(t, diagnosticCodesOf(report.Diagnostics))
			parameters := parametersByName(t, report)
			require.Len(t, parameters, 1)
			require.Contains(t, parameters, "title")
			require.True(t, parameters["title"].IsOptional)
		})
	}
}

func TestNamedPlaceholdersBindByNameWhateverTheDeclarationOrder(t *testing.T) {
	t.Parallel()

	queries := map[string]string{
		"renamed": `-- sgl.query(OrdersMatching, many)
-- :title as sgl.param(heading)
-- :customer as sgl.param(buyer)
SELECT id, customer FROM orders WHERE customer = :customer AND title = :title;
`,
		"inferred": `-- sgl.query(OrdersMatching, many)
-- :title as sgl.param
-- :customer as sgl.param
SELECT id, customer FROM orders WHERE customer = :customer AND title = :title;
`,
	}
	expectedNames := map[string][2]string{
		"renamed":  {"buyer", "heading"},
		"inferred": {"customer", "title"},
	}

	for name, newDialect := range namedParameterDialects {
		for variant, query := range queries {
			t.Run(name+"/"+variant, func(t *testing.T) {
				t.Parallel()

				report := vetOrders(t, newDialect(), query)

				require.Empty(t, diagnosticCodesOf(report.Diagnostics))
				require.Len(t, report.Queries, 1)
				parameters := report.Queries[0].Parameters
				require.Len(t, parameters, 2)
				require.Equal(t, expectedNames[variant][0], parameters[0].Name)
				require.Equal(t, 1, parameters[0].Number)
				require.False(t, parameters[0].Nullable)
				require.Equal(t, expectedNames[variant][1], parameters[1].Name)
				require.Equal(t, 2, parameters[1].Number)
				require.True(t, parameters[1].Nullable)
			})
		}
	}
}

func TestNamedPlaceholderRenamedByParamIsRewrittenInTheGeneratedSQL(t *testing.T) {
	t.Parallel()

	for name, newDialect := range namedParameterDialects {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			generator, err := seagull.New(newDialect())
			require.NoError(t, err)
			result, err := generator.Generate(t.Context(), ordersInput(`-- sgl.query(OrdersFor, many)
-- :customer as sgl.param(buyer)
SELECT id, customer FROM orders WHERE customer = :customer;
`))
			require.NoError(t, err)
			require.NoError(t, result.Err())

			source := fileContent(t, result.Files, "orders.sql.go")
			require.NotContains(t, source, ":customer")
			require.Contains(t, source, "buyer string")
		})
	}
}

func TestBracedPlaceholderRenamedByParamBindsUnderTheParameterName(t *testing.T) {
	t.Parallel()

	generator, err := seagull.New(dialect_engine_clickhouse.New())
	require.NoError(t, err)
	result, err := generator.Generate(t.Context(), seagull.Input{
		Package: "store",
		Migrations: fstest.MapFS{
			"db/migrations/0001_init.up.sql": &fstest.MapFile{Data: []byte(
				"CREATE TABLE orders (id UInt64, customer String, title Nullable(String)) ENGINE = MergeTree ORDER BY id;")},
			"db/queries/orders.sql": &fstest.MapFile{Data: []byte(`-- sgl.query(OrdersFor, many)
-- {customer:String} as sgl.param(buyer)
SELECT id, customer FROM orders WHERE customer = {customer:String};

-- sgl.query(OrdersByTitle, many)
-- {title:Nullable(String)} as sgl.param(optional: true)
SELECT id, customer FROM orders WHERE title = {title:Nullable(String)};
`)},
		},
		MigrationDir: "db/migrations",
		QueryDir:     "db/queries",
	})
	require.NoError(t, err)
	require.Empty(t, diagnosticCodesOf(result.Diagnostics))

	source := fileContent(t, result.Files, "orders.sql.go")
	require.Contains(t, source, "customer = {p_buyer:String}")
	require.Contains(t, source, `"p_buyer"`)
	require.Contains(t, source, "title = {p_title:Nullable(String)}")
}

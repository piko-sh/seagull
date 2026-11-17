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

package dialecttest

import (
	"path"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/dialects"
	"seagull.sh/seagull/internal/dialect/mariadb"
	"seagull.sh/seagull/internal/dialect/mysql"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

func TestNegativeCastResultTypesAcrossDialects(t *testing.T) {
	for _, name := range dialects.Names() {
		t.Run(name, func(t *testing.T) {
			engine, err := dialects.New(name, dialects.Options{})
			require.NoError(t, err)
			model, ok := fuzzcase.Decode(name, []byte("\x0200000000071"))
			require.True(t, ok)
			query := expressionQuery(t, engine, strings.Join(model.Migrations(), "\n"), model.Query().SQL)
			require.Len(t, query.OutputColumns, 2)
			want := sqlmodel.TypeCategoryInteger
			if name == "mysql" || name == "mariadb" {
				want = sqlmodel.TypeCategoryDecimal
			}
			require.Equal(t, want, query.OutputColumns[1].SQLType.Category)
		})
	}
}

func TestNumericPrefixesPreserveConstantAndRuntimeTypes(t *testing.T) {
	for _, engine := range []dialect.DialectPort{mysql.New(), mariadb.New(), mariadb.New(mysql.WithName("custom"))} {
		t.Run(engine.Info().Name, func(t *testing.T) {
			for _, expression := range []string{
				"-CAST(-8.0 AS SIGNED)", "-CAST(-8e0 AS SIGNED)", "-CAST(-8.5 AS SIGNED)",
				"-CAST(-0.5 AS SIGNED)", "-CAST(-8.5e0 AS SIGNED)",
				"-CAST((-8.5+0.1) AS SIGNED)", "-CAST((-8.5e0+0.1) AS SIGNED)",
				"-COALESCE(NULL,CAST(-8 AS SIGNED))",
				"-CASE WHEN TRUE THEN CAST(-8 AS SIGNED) ELSE 1 END",
				"-CASE WHEN TRUE THEN CAST(-8 AS SIGNED) END",
				"-CASE WHEN FALSE THEN NULL ELSE CAST(-8 AS SIGNED) END",
				"-CAST(-8 AS SIGNED)", "-(1-2)", "-(CAST(1 AS SIGNED)-CAST(2 AS SIGNED))",
				"-COALESCE(CAST(-8 AS SIGNED), 1)", "-CASE WHEN 1=1 THEN CAST(-8 AS SIGNED) ELSE 1 END",
				"-CAST(-9223372036854775808 AS SIGNED)", "-(-9223372036854775808)",
				"-CAST(18446744073709551615 AS UNSIGNED)", "-CAST(-8 AS UNSIGNED)",
				"-CAST(8 AS DECIMAL(20,0))", "-CAST(-8 AS DECIMAL(20,0))",
			} {
				t.Run(expression, func(t *testing.T) {
					query := expressionQuery(t, engine, "CREATE TABLE items (id BIGINT);", "-- sgl.query(Constant, one)\nSELECT "+expression+" AS value;")
					require.Equal(t, sqlmodel.TypeCategoryDecimal, query.OutputColumns[0].SQLType.Category)
					require.False(t, query.OutputColumns[0].Nullable)
				})
			}
			for _, expression := range []string{
				"-CAST(-0.49 AS SIGNED)", "-CAST(-0.5e0 AS SIGNED)",
				"-CAST(0.5 AS SIGNED)", "-CAST(0.5e0 AS SIGNED)",
				"-COALESCE(NULL,CAST(-8 AS SIGNED),id)",
				"-CAST(8 AS SIGNED)", "-(2-1)", "-CAST(8 AS UNSIGNED)",
				"-COALESCE(CAST(-8 AS SIGNED), id)", "-CASE WHEN id=1 THEN CAST(-8 AS SIGNED) ELSE 1 END",
			} {
				t.Run(expression, func(t *testing.T) {
					query := expressionQuery(t, engine, "CREATE TABLE items (id BIGINT);", "-- sgl.query(Dynamic, many)\nSELECT "+expression+" AS value FROM items;")
					require.Equal(t, sqlmodel.TypeCategoryInteger, query.OutputColumns[0].SQLType.Category)
					require.False(t, query.OutputColumns[0].SQLType.Unsigned)
				})
			}
			query := expressionQuery(t, engine, "CREATE TABLE items (id BIGINT);", "-- sgl.query(Literal, one)\nSELECT -(-8) AS value;")
			want := sqlmodel.TypeCategoryInteger
			if engine.Info().Name == "mysql" {
				want = sqlmodel.TypeCategoryDecimal
			}
			require.Equal(t, want, query.OutputColumns[0].SQLType.Category)
		})
	}
}

func expressionQuery(t *testing.T, engine dialect.DialectPort, schema, source string) *sqlmodel.AnalysedQuery {
	t.Helper()
	generator, err := app.New(engine)
	require.NoError(t, err)
	files := fstest.MapFS{
		path.Join(migrationDirectory, "001_schema.up.sql"): &fstest.MapFile{Data: []byte(schema)},
		path.Join(queryDirectory, "query.sql"):             &fstest.MapFile{Data: []byte(source)},
	}
	report, err := generator.Vet(t.Context(), fuzzInput(files))
	require.NoError(t, err)
	for _, diagnostic := range report.Diagnostics {
		require.NotEqual(t, diag.SeverityError, diagnostic.Severity, "%+v", diagnostic)
	}
	require.Len(t, report.Queries, 1)
	return report.Queries[0]
}

func TestBoundNegationPreservesParameterTypes(t *testing.T) {
	for _, engine := range []dialect.DialectPort{mysql.New(), mariadb.New(), mariadb.New(mysql.WithName("renamed"))} {
		t.Run(engine.Info().Name, func(t *testing.T) {
			for _, expression := range []string{
				"-CAST(? AS SIGNED)", "-CAST(? AS UNSIGNED)", "-(CAST(? AS SIGNED)-2)",
				"-COALESCE(CAST(? AS SIGNED),1)", "-CASE WHEN 1=1 THEN CAST(? AS SIGNED) ELSE 1 END",
			} {
				query := expressionQuery(t, engine, "CREATE TABLE items (id BIGINT);", "-- sgl.query(Bound, one)\nSELECT "+expression+" AS value;")
				want := sqlmodel.TypeCategoryDecimal
				if engine.Info().Name == "mysql" {
					want = sqlmodel.TypeCategoryInteger
				}
				require.Equal(t, want, query.OutputColumns[0].SQLType.Category, expression)
				require.Len(t, query.Parameters, 1)
				require.Equal(t, sqlmodel.TypeCategoryInteger, query.Parameters[0].SQLType.Category)
			}
			query := expressionQuery(t, engine, "CREATE TABLE items (id BIGINT);", "-- sgl.query(BoundRows, many)\nSELECT -COALESCE(CAST(? AS SIGNED),id) AS value FROM items;")
			require.Equal(t, sqlmodel.TypeCategoryInteger, query.OutputColumns[0].SQLType.Category)
		})
	}
}

func TestLiteralBitwisePrefixAcrossDialects(t *testing.T) {
	for _, name := range dialects.Names() {
		t.Run(name, func(t *testing.T) {
			engine, err := dialects.New(name, dialects.Options{})
			require.NoError(t, err)
			expression := "~8"
			if name == "clickhouse" {
				expression = "bitNot(CAST(8 AS Int64))"
			}
			query := expressionQuery(t, engine, "CREATE TABLE items (id BIGINT);", "-- sgl.query(Bitwise, one)\nSELECT "+expression+" AS value;")
			require.Equal(t, sqlmodel.TypeCategoryInteger, query.OutputColumns[0].SQLType.Category)
			require.Equal(t, name == "mysql" || name == "mariadb", query.OutputColumns[0].SQLType.Unsigned)
		})
	}
}

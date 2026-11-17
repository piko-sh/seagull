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

package emittest

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/dialects"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/clickhouse"
	"seagull.sh/seagull/internal/emit/databasesql"
	"seagull.sh/seagull/internal/emit/pgx"
	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

func TestParameterlessBatchGenerationAcrossDialects(t *testing.T) {
	for _, engine := range []string{"postgres", "sqlite", "mysql", "mariadb", "duckdb", "clickhouse", "cockroachdb", "timescaledb"} {
		for driver, factory := range parameterlessBatchEmitters(engine) {
			for _, command := range []string{"batch", "copyfrom"} {
				t.Run(engine+"/"+driver+"/"+command, func(t *testing.T) {
					checkParameterlessBatch(t, engine, driver, command, factory)
				})
			}
		}
	}
}

func parameterlessBatchEmitters(engine string) map[string]func() emit.EmitterPort {
	factories := map[string]func() emit.EmitterPort{"databasesql": func() emit.EmitterPort { return databasesql.New() }}
	if engine == "postgres" || engine == "cockroachdb" || engine == "timescaledb" {
		factories["pgx"] = func() emit.EmitterPort { return pgx.New() }
	}
	if engine == "clickhouse" {
		factories["clickhouse"] = func() emit.EmitterPort { return clickhouse.New() }
	}
	return factories
}

func checkParameterlessBatch(t *testing.T, name, driver, command string, factory func() emit.EmitterPort) {
	t.Helper()
	for _, source := range []struct {
		name, sql string
		valid     bool
	}{
		{name: "literal values", sql: "INSERT INTO fuzz_items (id, category) VALUES (1, 2);", valid: true},
		{name: "empty tuple", sql: "INSERT fuzz_items () VALUES ();", valid: false},
	} {
		t.Run(source.name, func(t *testing.T) {
			engine, err := dialects.New(name, dialects.Options{})
			require.NoError(t, err)
			model, ok := fuzzcase.Decode(name, []byte{fuzzcase.Version})
			require.True(t, ok)
			files := fstest.MapFS{
				"migrations/001_schema.up.sql": &fstest.MapFile{Data: []byte(strings.Join(model.Migrations(), "\n"))},
				"queries/batch.sql":            &fstest.MapFile{Data: []byte("-- sgl.query(InsertRows, " + command + ")\n" + source.sql)},
			}
			generator, err := app.New(engine, app.WithEmitter(factory()))
			require.NoError(t, err)
			result, err := generator.GenerateDetailed(t.Context(), app.Input{Package: "generated", Migrations: files, MigrationDir: "migrations", QueryDir: "queries"})
			require.NoError(t, err)
			for _, diagnostic := range result.Result.Diagnostics {
				require.NotEqual(t, diag.CodeRecoveredPanic, diagnostic.Code)
			}
			if source.valid && command == "copyfrom" && driver != "databasesql" {
				require.Error(t, result.Result.Err())
				require.Contains(t, result.Result.Err().Error(), diag.CodeBatchShape)
				return
			}
			if source.valid {
				require.NoError(t, result.Result.Err())
				require.NotEmpty(t, result.Result.Files)
				require.Len(t, result.Report.Queries, 1)
				require.Empty(t, result.Report.Queries[0].Parameters)
			}
			if result.Result.Err() == nil {
				require.Empty(t, typeCheck(result.Result.Files).typeProblems("parameterless batch"))
			}
		})
	}
}

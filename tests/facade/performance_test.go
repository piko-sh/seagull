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
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_clickhouse"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_mysql"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_postgres"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
	"seagull.sh/seagull/sdk/emit"
	"seagull.sh/seagull/sdk/emit/emit_driver_clickhouse"
	"seagull.sh/seagull/sdk/emit/emit_driver_databasesql"
	"seagull.sh/seagull/sdk/emit/emit_driver_pgx"
)

var (
	resultSink    *seagull.Result
	generatorSink *seagull.Generator
)

func performanceFixture(queryCount, tableCount, perFile int, marker string) seagull.Input {
	var ddl strings.Builder
	for table := range tableCount {
		fmt.Fprintf(&ddl, "CREATE TABLE users%d (id BIGINT NOT NULL, name TEXT NOT NULL, age INTEGER NOT NULL);\n", table)
	}
	files := fstest.MapFS{"migrations/001_schema.up.sql": &fstest.MapFile{Data: []byte(ddl.String())}}
	for start := 0; start < queryCount; start += perFile {
		var queries strings.Builder
		for query := start; query < min(queryCount, start+perFile); query++ {
			fmt.Fprintf(&queries, "-- sgl.query(GetUser%d, many)\nSELECT id, name, age FROM users%d WHERE id = %s;\n\n", query, query%tableCount, marker)
		}
		files[fmt.Sprintf("queries/q%05d.sql", start)] = &fstest.MapFile{Data: []byte(queries.String())}
	}
	return seagull.Input{Migrations: files, Queries: files, MigrationDir: "migrations", QueryDir: "queries", Package: "store"}
}

func benchmarkGeneration(b *testing.B, engine dialect.Dialect, emitter emit.EmitterPort, input seagull.Input) {
	b.Helper()

	generator, err := seagull.New(engine, seagull.WithEmitter(emitter))
	require.NoError(b, err)
	result, err := generator.Generate(b.Context(), input)
	require.NoError(b, err)
	require.NoError(b, result.Err())
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		resultSink, err = generator.Generate(b.Context(), input)
		require.NoError(b, err)
	}
}

func BenchmarkGenerate(b *testing.B) {
	for _, queryCount := range []int{1, 100, 1000} {
		for _, perFile := range []int{1, 100} {
			b.Run(fmt.Sprintf("Q%d_PerFile%d", queryCount, perFile), func(b *testing.B) {
				benchmarkGeneration(b, dialect_engine_postgres.New(), emit_driver_pgx.New(), performanceFixture(queryCount, 1, perFile, "$1"))
			})
		}
	}
}

func BenchmarkSchemaSize(b *testing.B) {
	for _, tableCount := range []int{1, 100, 1000} {
		b.Run(fmt.Sprintf("Tables%d", tableCount), func(b *testing.B) {
			benchmarkGeneration(b, dialect_engine_postgres.New(), emit_driver_pgx.New(), performanceFixture(100, tableCount, 100, "$1"))
		})
	}
}

func BenchmarkDialects(b *testing.B) {
	for _, benchmarkCase := range []struct {
		engine  dialect.Dialect
		emitter emit.EmitterPort
		name    string
		marker  string
	}{
		{name: "pgx", engine: dialect_engine_postgres.New(), emitter: emit_driver_pgx.New(), marker: "$1"},
		{name: "sqlite", engine: dialect_engine_sqlite.New(), emitter: emit_driver_databasesql.New(), marker: "?"},
		{name: "mysql", engine: dialect_engine_mysql.New(), emitter: emit_driver_databasesql.New(), marker: "?"},
		{name: "clickhouse", engine: dialect_engine_clickhouse.New(), emitter: emit_driver_clickhouse.New(), marker: "{id:Int64}"},
	} {
		b.Run(benchmarkCase.name, func(b *testing.B) {
			benchmarkGeneration(b, benchmarkCase.engine, benchmarkCase.emitter, performanceFixture(100, 1, 100, benchmarkCase.marker))
		})
	}
}

func BenchmarkConstruct(b *testing.B) {
	for _, benchmarkCase := range []struct {
		newDialect func() dialect.Dialect
		name       string
	}{
		{name: "postgres", newDialect: func() dialect.Dialect { return dialect_engine_postgres.New() }},
		{name: "sqlite", newDialect: func() dialect.Dialect { return dialect_engine_sqlite.New() }},
		{name: "mysql", newDialect: func() dialect.Dialect { return dialect_engine_mysql.New() }},
		{name: "clickhouse", newDialect: func() dialect.Dialect { return dialect_engine_clickhouse.New() }},
	} {
		b.Run(benchmarkCase.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				generator, err := seagull.New(benchmarkCase.newDialect())
				require.NoError(b, err)
				generatorSink = generator
			}
		})
	}
}

func BenchmarkGenerateWideSchema(b *testing.B) {
	benchmarkGeneration(b, dialect_engine_postgres.New(), emit_driver_pgx.New(), performanceFixture(1000, 1000, 100, "$1"))
}

func BenchmarkStages(b *testing.B) {
	input := performanceFixture(1000, 1, 100, "$1")
	generator, err := seagull.New(dialect_engine_postgres.New(), seagull.WithEmitter(emit_driver_pgx.New()))
	require.NoError(b, err)
	b.Run("Vet", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			report, err := generator.Vet(b.Context(), input)
			require.NoError(b, err)
			require.NoError(b, report.Err())
		}
	})
	b.Run("Catalogue", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, _, err := generator.Catalogue(b.Context(), input)
			require.NoError(b, err)
		}
	})
}

func BenchmarkDetailedGeneration(b *testing.B) {
	input := performanceFixture(1000, 1, 100, "$1")
	generator, err := seagull.New(dialect_engine_postgres.New(), seagull.WithEmitter(emit_driver_pgx.New()))
	require.NoError(b, err)
	b.Run("Detailed", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			result, err := generator.GenerateDetailed(b.Context(), input)
			require.NoError(b, err)
			require.NoError(b, result.Result.Err())
		}
	})
	b.Run("Separate", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, err := generator.Generate(b.Context(), input)
			require.NoError(b, err)
			_, err = generator.Vet(b.Context(), input)
			require.NoError(b, err)
			_, _, err = generator.Catalogue(b.Context(), input)
			require.NoError(b, err)
		}
	})
}

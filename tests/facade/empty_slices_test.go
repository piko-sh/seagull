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
	"seagull.sh/seagull/sdk/dialect/dialect_engine_postgres"
	"seagull.sh/seagull/sdk/emit"
	"seagull.sh/seagull/sdk/emit/emit_driver_clickhouse"
	"seagull.sh/seagull/sdk/emit/emit_driver_databasesql"
	"seagull.sh/seagull/sdk/emit/emit_driver_pgx"
)

const featurePostgresMigration = `CREATE TABLE posts (
    id BIGINT PRIMARY KEY,
    title TEXT NOT NULL,
    author_id BIGINT
);
`

const featureClickHouseMigration = `CREATE TABLE posts (
    id UInt64,
    title String,
    author_id Nullable(UInt64)
) ENGINE = MergeTree ORDER BY id;
`

const featureQueries = `-- sgl.query(ListPosts, many)
SELECT id, title FROM posts;

-- sgl.query(SearchPosts, many, dynamic: runtime)
SELECT id, title FROM posts;
`

type featureTarget struct {
	engine    func() dialect.Dialect
	emitter   func() emit.EmitterPort
	name      string
	migration string
}

var featureTargets = []featureTarget{
	{name: "database/sql on PostgreSQL", engine: func() dialect.Dialect { return dialect_engine_postgres.New() }, emitter: func() emit.EmitterPort { return emit_driver_databasesql.New() }, migration: featurePostgresMigration},
	{name: "pgx on PostgreSQL", engine: func() dialect.Dialect { return dialect_engine_postgres.New() }, emitter: func() emit.EmitterPort { return emit_driver_pgx.New() }, migration: featurePostgresMigration},
	{name: "native ClickHouse", engine: func() dialect.Dialect { return dialect_engine_clickhouse.New() },
		emitter: func() emit.EmitterPort { return emit_driver_clickhouse.New() }, migration: featureClickHouseMigration},
	{name: "database/sql on ClickHouse", engine: func() dialect.Dialect { return dialect_engine_clickhouse.New() },
		emitter: func() emit.EmitterPort { return emit_driver_databasesql.New() }, migration: featureClickHouseMigration},
}

func featureProjectInput(target featureTarget, queries string) seagull.Input {
	project := fstest.MapFS{
		"db/migrations/0001_init.up.sql": &fstest.MapFile{Data: []byte(target.migration)},
		"db/queries/posts.sql":           &fstest.MapFile{Data: []byte(queries)},
	}
	return seagull.Input{
		Package:      "store",
		Migrations:   project,
		MigrationDir: "db/migrations",
		QueryDir:     "db/queries",
	}
}

func generateFeatureProject(t *testing.T, target featureTarget, queries string, chosen seagull.Preset, options ...seagull.Option) map[string]string {
	t.Helper()

	options = append([]seagull.Option{seagull.WithEmitter(target.emitter()), seagull.WithPreset(chosen)}, options...)
	generator, err := seagull.New(target.engine(), options...)
	require.NoError(t, err)
	result, err := generator.Generate(t.Context(), featureProjectInput(target, queries))
	require.NoError(t, err)
	require.NoError(t, result.Err())
	files := make(map[string]string, len(result.Files))
	for _, file := range result.Files {
		files[file.Name] = string(file.Content)
	}
	return files
}

func TestEmptySlicesMakeManyMethodsReturnAnEmptySliceForNoRows(t *testing.T) {
	t.Parallel()

	for _, target := range featureTargets {
		t.Run(target.name, func(t *testing.T) {
			t.Parallel()

			defaults := generateFeatureProject(t, target, featureQueries, seagull.DefaultPreset())["posts.sql.go"]
			require.Contains(t, defaults, "var results []ListPostsRow")
			require.Contains(t, defaults, "var items []SearchPostsRow")

			chosen := seagull.DefaultPreset()
			chosen.EmptySlices = true
			empty := generateFeatureProject(t, target, featureQueries, chosen)["posts.sql.go"]
			require.Contains(t, empty, "results := []ListPostsRow{}")
			require.Contains(t, empty, "items := []SearchPostsRow{}")
			require.NotContains(t, empty, "var results []ListPostsRow")
			require.NotContains(t, empty, "var items []SearchPostsRow")
		})
	}
}

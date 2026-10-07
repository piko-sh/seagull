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
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_clickhouse"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_duckdb"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_postgres"
)

const elementPostsMigration = `CREATE TABLE posts (
    id INT PRIMARY KEY,
    tags TEXT[] NOT NULL,
    scores INT4[] NOT NULL
);
`

const elementPostsQueries = `-- sgl.query(ListPosts, many)
SELECT id, tags, scores FROM posts;

-- sgl.query(SetTags, exec)
UPDATE posts SET tags = $1 WHERE id = $2;
`

const elementReadingsMigration = `CREATE TABLE readings (
    id INTEGER PRIMARY KEY,
    scores INTEGER[] NOT NULL,
    limits MAP(VARCHAR, INTEGER) NOT NULL
);
`

func generateElementProject(t *testing.T, engine dialect.Dialect, files map[string]string, options ...seagull.Option) string {
	t.Helper()

	project := fstest.MapFS{}
	for name, content := range files {
		project[name] = &fstest.MapFile{Data: []byte(content)}
	}
	generator, err := seagull.New(engine, options...)
	require.NoError(t, err)
	result, err := generator.Generate(t.Context(), seagull.Input{
		Package:      "store",
		Migrations:   project,
		MigrationDir: "db/migrations",
		QueryDir:     "db/queries",
	})
	require.NoError(t, err)
	require.Empty(t, result.Diagnostics)
	var source strings.Builder
	for _, file := range result.Files {
		source.WriteString(strings.Join(strings.Fields(string(file.Content)), " "))
		source.WriteString("\n")
	}
	return source.String()
}

func elementPreset(nonNull bool) seagull.Option {
	chosen := seagull.DefaultPreset()
	chosen.NonNullElements = nonNull
	return seagull.WithPreset(chosen)
}

func TestElementsTheEngineCanHoldNullInArePointersUnlessDeclaredNonNull(t *testing.T) {
	t.Parallel()

	declared := func(value bool) *bool { return &value }

	for _, testCase := range []struct {
		engine     func() dialect.Dialect
		files      map[string]string
		name       string
		options    []seagull.Option
		contains   []string
		notContain []string
	}{
		{
			name:     "PostgreSQL arrays hold pointer elements by default, in parameters too",
			engine:   func() dialect.Dialect { return dialect_engine_postgres.New() },
			files:    map[string]string{"db/migrations/0001_init.up.sql": elementPostsMigration, "db/queries/posts.sql": elementPostsQueries},
			contains: []string{"Tags []*string `json:\"tags\"`", "Scores []*int32 `json:\"scores\"`", "Tags []*string ID int32"},
		},
		{
			name:     "the preset declares them non-null",
			engine:   func() dialect.Dialect { return dialect_engine_postgres.New() },
			files:    map[string]string{"db/migrations/0001_init.up.sql": elementPostsMigration, "db/queries/posts.sql": elementPostsQueries},
			options:  []seagull.Option{elementPreset(true)},
			contains: []string{"Tags []string `json:\"tags\"`", "Scores []int32 `json:\"scores\"`"},
			notContain: []string{
				"[]*string", "[]*int32",
			},
		},
		{
			name:   "a query column directive declares one column's elements non-null",
			engine: func() dialect.Dialect { return dialect_engine_postgres.New() },
			files: map[string]string{
				"db/migrations/0001_init.up.sql": elementPostsMigration,
				"db/queries/posts.sql":           "-- sgl.query(ListPosts, many)\n-- sgl.column(tags, non_null_elements: true)\nSELECT id, tags, scores FROM posts;\n",
			},
			contains: []string{"type ListPostsRow struct { ID int32 `json:\"id\"` Tags []string `json:\"tags\"` Scores []*int32 `json:\"scores\"` }", "type Posts struct { ID int32 `json:\"id\"` Tags []*string"},
		},
		{
			name:   "a migration column directive declares them non-null for the model and the queries",
			engine: func() dialect.Dialect { return dialect_engine_postgres.New() },
			files: map[string]string{
				"db/migrations/0001_init.up.sql": "-- sgl.column(posts.tags, non_null_elements: true)\n" + elementPostsMigration,
				"db/queries/posts.sql":           elementPostsQueries,
			},
			contains:   []string{"Tags []string `json:\"tags\"` Scores []*int32 `json:\"scores\"`"},
			notContain: []string{"[]*string"},
		},
		{
			name:       "a column type override declares them non-null without a Go type",
			engine:     func() dialect.Dialect { return dialect_engine_postgres.New() },
			files:      map[string]string{"db/migrations/0001_init.up.sql": elementPostsMigration, "db/queries/posts.sql": elementPostsQueries},
			options:    []seagull.Option{seagull.WithTypeOverrides(seagull.TypeOverride{Column: "posts.tags", NonNullElements: declared(true)})},
			contains:   []string{"Tags []string `json:\"tags\"` Scores []*int32 `json:\"scores\"`"},
			notContain: []string{"[]*string"},
		},
		{
			name:       "a SQL type override declares the elements of every value of the type non-null",
			engine:     func() dialect.Dialect { return dialect_engine_postgres.New() },
			files:      map[string]string{"db/migrations/0001_init.up.sql": elementPostsMigration, "db/queries/posts.sql": elementPostsQueries},
			options:    []seagull.Option{seagull.WithTypeOverrides(seagull.TypeOverride{SQLTypeName: "text[]", NonNullElements: declared(true)})},
			contains:   []string{"Tags []string `json:\"tags\"` Scores []*int32 `json:\"scores\"`"},
			notContain: []string{"[]*string"},
		},
		{
			name:   "a column declares them nullable over the preset",
			engine: func() dialect.Dialect { return dialect_engine_postgres.New() },
			files: map[string]string{
				"db/migrations/0001_init.up.sql": elementPostsMigration,
				"db/queries/posts.sql":           "-- sgl.query(ListPosts, many)\n-- sgl.column(scores, non_null_elements: false)\nSELECT id, tags, scores FROM posts;\n",
			},
			options:  []seagull.Option{elementPreset(true)},
			contains: []string{"Tags []string `json:\"tags\"` Scores []*int32 `json:\"scores\"`"},
		},
		{
			name:   "elements the analysis proves never NULL stay plain by default",
			engine: func() dialect.Dialect { return dialect_engine_postgres.New() },
			files: map[string]string{
				"db/migrations/0001_init.up.sql": elementPostsMigration,
				"db/queries/posts.sql": "-- sgl.query(Collected, one)\nSELECT array_agg(id) AS ids, ARRAY[id, 2] AS pair, " +
					"array_agg(tags[1]) AS firsts FROM posts GROUP BY id;\n",
			},
			contains: []string{"IDs []int32 `json:\"ids\"`", "Pair []int32 `json:\"pair\"`", "Firsts []*string `json:\"firsts\"`"},
		},
		{
			name:   "an array the analysis shows holds NULL keeps pointer elements under the preset",
			engine: func() dialect.Dialect { return dialect_engine_postgres.New() },
			files: map[string]string{
				"db/migrations/0001_init.up.sql": elementPostsMigration,
				"db/queries/posts.sql":           "-- sgl.query(ListPairs, many)\nSELECT id, ARRAY['a', NULL] AS pair FROM posts;\n",
			},
			options:  []seagull.Option{elementPreset(true)},
			contains: []string{"Pair []*string `json:\"pair\"`"},
		},
		{
			name:   "DuckDB lists hold pointer elements and maps pointer values, keys never",
			engine: func() dialect.Dialect { return dialect_engine_duckdb.New() },
			files: map[string]string{
				"db/migrations/0001_init.up.sql": "CREATE TABLE readings (id INTEGER PRIMARY KEY, scores INTEGER[] NOT NULL, limits MAP(VARCHAR, INTEGER) NOT NULL);\n",
				"db/queries/readings.sql":        "-- sgl.query(ListReadings, many)\nSELECT id, scores, limits FROM readings;\n",
			},
			contains: []string{"Scores []*int32 `json:\"scores\"` Limits map[string]*int32 `json:\"limits\"`"},
		},
		{
			name:   "ClickHouse arrays follow their element type",
			engine: func() dialect.Dialect { return dialect_engine_clickhouse.New() },
			files: map[string]string{
				"db/migrations/0001_init.up.sql": "CREATE TABLE readings (id UInt64, scores Array(Int32), notes Array(Nullable(String))) ENGINE = MergeTree ORDER BY id;\n",
				"db/queries/readings.sql":        "-- sgl.query(ListReadings, many)\nSELECT id, scores, notes FROM readings;\n",
			},
			contains: []string{"Scores []int32 `json:\"scores\"` Notes []*string `json:\"notes\"`"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			source := generateElementProject(t, testCase.engine(), testCase.files, testCase.options...)
			for _, want := range testCase.contains {
				require.Contains(t, source, want)
			}
			for _, unwanted := range testCase.notContain {
				require.NotContains(t, source, unwanted)
			}
		})
	}
}

func TestArrayParametersHoldNullElementsUnlessDeclaredNonNull(t *testing.T) {
	t.Parallel()

	declared := func(value bool) *bool { return &value }
	postsProject := func(queries string) map[string]string {
		return map[string]string{"db/migrations/0001_init.up.sql": elementPostsMigration, "db/queries/posts.sql": queries}
	}

	for _, testCase := range []struct {
		engine     func() dialect.Dialect
		files      map[string]string
		name       string
		options    []seagull.Option
		contains   []string
		notContain []string
	}{
		{
			name:     "a parameter written to an array column holds pointer elements, as the column's model does",
			engine:   func() dialect.Dialect { return dialect_engine_postgres.New() },
			files:    postsProject("-- sgl.query(SetTags, exec)\nUPDATE posts SET tags = $1 WHERE id = $2;\n"),
			contains: []string{"type SetTagsParams struct { Tags []*string ID int32 }", "Tags []*string `json:\"tags\"`"},
		},
		{
			name:     "a parameter cast to an array holds pointer elements",
			engine:   func() dialect.Dialect { return dialect_engine_postgres.New() },
			files:    postsProject("-- sgl.query(PostsSharingTags, many)\nSELECT id FROM posts WHERE tags && $1::text[];\n"),
			contains: []string{"PostsSharingTags(ctx context.Context, tags []*string)"},
		},
		{
			name:     "a parameter unnested into rows holds pointer elements",
			engine:   func() dialect.Dialect { return dialect_engine_postgres.New() },
			files:    postsProject("-- sgl.query(AddPosts, exec)\nINSERT INTO posts (id, tags, scores) SELECT unnest($1::int4[]), '{}', '{}';\n"),
			contains: []string{"AddPosts(ctx context.Context, p1 []*int32)"},
		},
		{
			name:       "an array a quantified comparison reads keeps plain elements",
			engine:     func() dialect.Dialect { return dialect_engine_postgres.New() },
			files:      postsProject("-- sgl.query(PostsByID, many)\nSELECT id FROM posts WHERE id = ANY($1) OR id <> ALL($1::int4[]);\n"),
			contains:   []string{"PostsByID(ctx context.Context, id []int32)"},
			notContain: []string{"[]*int32 )"},
		},
		{
			name:   "an array a quantified comparison reads holds pointer elements when the parameter declares them nullable",
			engine: func() dialect.Dialect { return dialect_engine_postgres.New() },
			files: postsProject("-- sgl.query(PostsByID, many)\n-- $1 as sgl.param(ids, non_null_elements: false)\n" +
				"SELECT id FROM posts WHERE id = ANY($1);\n"),
			contains: []string{"PostsByID(ctx context.Context, ids []*int32)"},
		},
		{
			name:     "a slice parameter keeps plain elements",
			engine:   func() dialect.Dialect { return dialect_engine_postgres.New() },
			files:    postsProject("-- sgl.query(PostsIn, many)\n-- $1 as sgl.param(ids, kind: slice)\nSELECT id FROM posts WHERE id IN ($1);\n"),
			contains: []string{"type PostsInParams struct { IDs []int32 }"},
		},
		{
			name:     "the parameter declares its elements non-null",
			engine:   func() dialect.Dialect { return dialect_engine_postgres.New() },
			files:    postsProject("-- sgl.query(SetTags, exec)\n-- $1 as sgl.param(tags, non_null_elements: true)\nUPDATE posts SET tags = $1 WHERE id = $2;\n"),
			contains: []string{"type SetTagsParams struct { Tags []string ID int32 }"},
		},
		{
			name:     "the preset declares them non-null",
			engine:   func() dialect.Dialect { return dialect_engine_postgres.New() },
			files:    postsProject("-- sgl.query(SetTags, exec)\nUPDATE posts SET tags = $1 WHERE id = $2;\n"),
			options:  []seagull.Option{elementPreset(true)},
			contains: []string{"type SetTagsParams struct { Tags []string ID int32 }"},
		},
		{
			name:   "the parameter declares them nullable over the preset",
			engine: func() dialect.Dialect { return dialect_engine_postgres.New() },
			files: postsProject("-- sgl.query(SetTags, exec)\n-- $1 as sgl.param(tags, non_null_elements: false)\n" +
				"UPDATE posts SET tags = $1 WHERE id = $2;\n"),
			options:  []seagull.Option{elementPreset(true)},
			contains: []string{"type SetTagsParams struct { Tags []*string ID int32 }"},
		},
		{
			name:   "a migration column directive declares them non-null for the parameters written to the column",
			engine: func() dialect.Dialect { return dialect_engine_postgres.New() },
			files: map[string]string{
				"db/migrations/0001_init.up.sql": "-- sgl.column(posts.tags, non_null_elements: true)\n" + elementPostsMigration,
				"db/queries/posts.sql":           "-- sgl.query(SetTags, exec)\nUPDATE posts SET tags = $1 WHERE id = $2;\n",
			},
			contains: []string{"type SetTagsParams struct { Tags []string ID int32 }"},
		},
		{
			name:     "a column type override declares them non-null for the parameters written to the column",
			engine:   func() dialect.Dialect { return dialect_engine_postgres.New() },
			files:    postsProject("-- sgl.query(SetTags, exec)\nUPDATE posts SET tags = $1 WHERE id = $2;\n"),
			options:  []seagull.Option{seagull.WithTypeOverrides(seagull.TypeOverride{Column: "posts.tags", NonNullElements: declared(true)})},
			contains: []string{"type SetTagsParams struct { Tags []string ID int32 }"},
		},
		{
			name:     "a SQL type override declares them non-null for every parameter of the type",
			engine:   func() dialect.Dialect { return dialect_engine_postgres.New() },
			files:    postsProject("-- sgl.query(PostsSharingTags, many)\nSELECT id FROM posts WHERE tags && $1::text[];\n"),
			options:  []seagull.Option{seagull.WithTypeOverrides(seagull.TypeOverride{SQLTypeName: "text[]", NonNullElements: declared(true)})},
			contains: []string{"PostsSharingTags(ctx context.Context, tags []string)"},
		},
		{
			name:   "DuckDB list and map parameters keep plain elements and values, which its driver cannot bind NULL in",
			engine: func() dialect.Dialect { return dialect_engine_duckdb.New() },
			files: map[string]string{
				"db/migrations/0001_init.up.sql": elementReadingsMigration,
				"db/queries/readings.sql":        "-- sgl.query(SetReading, exec)\nUPDATE readings SET scores = $1, limits = $2 WHERE id = $3;\n",
			},
			contains: []string{
				"type SetReadingParams struct { Scores []int32 Limits map[string]int32 ID int32 }",
				"Scores []*int32 `json:\"scores\"` Limits map[string]*int32 `json:\"limits\"`",
			},
		},
		{
			name:   "ClickHouse array parameters follow their element type",
			engine: func() dialect.Dialect { return dialect_engine_clickhouse.New() },
			files: map[string]string{
				"db/migrations/0001_init.up.sql": "CREATE TABLE readings (id UInt64, scores Array(Int32), notes Array(Nullable(String))) ENGINE = MergeTree ORDER BY id;\n",
				"db/queries/readings.sql": "-- sgl.query(AddReading, exec)\n" +
					"INSERT INTO readings (id, scores, notes) VALUES ({id:UInt64}, {scores:Array(Int32)}, {notes:Array(Nullable(String))});\n",
			},
			contains: []string{"type AddReadingParams struct { ID uint64 Scores []int32 Notes []*string }"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			source := generateElementProject(t, testCase.engine(), testCase.files, testCase.options...)
			for _, want := range testCase.contains {
				require.Contains(t, source, want)
			}
			for _, unwanted := range testCase.notContain {
				require.NotContains(t, source, unwanted)
			}
		})
	}
}

func TestDuckDBRefusesAParameterDeclaringNullableElements(t *testing.T) {
	t.Parallel()

	project := fstest.MapFS{
		"db/migrations/0001_init.up.sql": &fstest.MapFile{Data: []byte(elementReadingsMigration)},
		"db/queries/readings.sql": &fstest.MapFile{Data: []byte("-- sgl.query(SetScores, exec)\n" +
			"-- $1 as sgl.param(scores, non_null_elements: false)\nUPDATE readings SET scores = $1 WHERE id = $2;\n")},
	}
	generator, err := seagull.New(dialect_engine_duckdb.New())
	require.NoError(t, err)
	result, err := generator.Generate(t.Context(), seagull.Input{
		Package: "store", Migrations: project, MigrationDir: "db/migrations", QueryDir: "db/queries",
	})
	require.NoError(t, err)
	require.Len(t, result.Diagnostics, 1)
	require.Equal(t, seagull.CodeEmitterUnsupported, result.Diagnostics[0].Code)
	require.Contains(t, result.Diagnostics[0].Message, "parameter scores declares elements that may be NULL")
}

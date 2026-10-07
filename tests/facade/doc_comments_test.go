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

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_duckdb"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_mysql"
	"seagull.sh/seagull/sdk/emit"
	"seagull.sh/seagull/sdk/emit/emit_driver_databasesql"
)

const describedQueries = `-- ListPosts lists every post.
--
-- It reads the replica.
-- sgl.query(ListPosts, many)
SELECT id, title FROM posts;

-- sgl.query(SearchPosts, many, dynamic: runtime)
-- Searches the posts.
SELECT id, title FROM posts;

-- sgl.query(CountPosts, one)
SELECT count(*) AS total FROM posts;
`

const commentedPostgresMigration = `CREATE TABLE posts (
    id BIGINT PRIMARY KEY,
    title TEXT NOT NULL,
    author_id BIGINT
);
COMMENT ON TABLE posts IS 'Posts are the articles authors publish.';
COMMENT ON COLUMN posts.title IS 'The headline,
shown in lists.';
COMMENT ON COLUMN posts.author_id IS 'Removed below.';
COMMENT ON COLUMN posts.author_id IS NULL;
`

const commentedMySQLMigration = "CREATE TABLE posts (\n" +
	"    id BIGINT PRIMARY KEY,\n" +
	"    title VARCHAR(200) NOT NULL COMMENT 'The headline, shown in lists.',\n" +
	"    author_id BIGINT\n" +
	") ENGINE=InnoDB COMMENT='Posts are the articles authors publish.';\n"

const commentedClickHouseMigration = `CREATE TABLE posts (
    id UInt64,
    title String COMMENT 'The headline, shown in lists.',
    author_id Nullable(UInt64)
) ENGINE = MergeTree ORDER BY id COMMENT 'Posts are the articles authors publish.';
`

const commentedDuckDBMigration = `CREATE TABLE posts (
    id BIGINT PRIMARY KEY,
    title VARCHAR NOT NULL,
    author_id BIGINT
);
COMMENT ON TABLE posts IS 'Posts are the articles authors publish.';
COMMENT ON COLUMN posts.title IS 'The headline, shown in lists.';
`

func docComments(enabled bool) seagull.Preset {
	chosen := seagull.DefaultPreset()
	chosen.DocComments = enabled
	chosen.Interface.Enabled = true
	return chosen
}

func TestAQueryDescriptionBecomesItsMethodsDocComment(t *testing.T) {
	t.Parallel()

	for _, target := range featureTargets {
		t.Run(target.name, func(t *testing.T) {
			t.Parallel()

			defaults := generateFeatureProject(t, target, describedQueries, docComments(false))
			require.NotContains(t, defaults["posts.sql.go"], "// ListPosts lists every post.\n")
			require.NotContains(t, defaults["querier_interface.go"], "lists every post")

			documented := generateFeatureProject(t, target, describedQueries, docComments(true))
			source := documented["posts.sql.go"]
			require.Contains(t, source, "// ListPosts lists every post.\n//\n// It reads the replica.\nfunc (queries *Queries) ListPosts(")
			require.Contains(t, source, "// Searches the posts.\nfunc (queries *Queries) SearchPosts(")
			require.Contains(t, source, "}\n\nfunc (queries *Queries) CountPosts(", "a query without a description gets no doc comment")
			require.Contains(t, documented["querier_interface.go"], "\t// ListPosts lists every post.\n\t//\n\t// It reads the replica.\n\tListPosts(")
			require.Contains(t, documented["querier_interface.go"], "\t// Searches the posts.\n\tSearchPosts(")
		})
	}
}

func TestTableAndColumnCommentsBecomeTheModelsDocComments(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		engine    func() dialect.Dialect
		emitter   func() emit.EmitterPort
		name      string
		migration string
		field     string
	}{
		{name: "PostgreSQL COMMENT ON", engine: featureTargets[0].engine, emitter: featureTargets[0].emitter, migration: commentedPostgresMigration,
			field: "\t// The headline,\n\t// shown in lists.\n\tTitle "},
		{name: "PostgreSQL COMMENT ON with pgx", engine: featureTargets[1].engine, emitter: featureTargets[1].emitter, migration: commentedPostgresMigration,
			field: "\t// The headline,\n\t// shown in lists.\n\tTitle "},
		{name: "MySQL COMMENT clauses", engine: func() dialect.Dialect { return dialect_engine_mysql.New() }, emitter: func() emit.EmitterPort { return emit_driver_databasesql.New() },
			migration: commentedMySQLMigration, field: "\t// The headline, shown in lists.\n\tTitle "},
		{name: "ClickHouse COMMENT clauses", engine: featureTargets[2].engine, emitter: featureTargets[2].emitter, migration: commentedClickHouseMigration,
			field: "\t// The headline, shown in lists.\n\tTitle "},
		{name: "DuckDB COMMENT ON", engine: func() dialect.Dialect { return dialect_engine_duckdb.New() }, emitter: func() emit.EmitterPort { return emit_driver_databasesql.New() },
			migration: commentedDuckDBMigration, field: "\t// The headline, shown in lists.\n\tTitle "},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			target := featureTarget{engine: testCase.engine, emitter: testCase.emitter, name: testCase.name, migration: testCase.migration}
			queries := "-- sgl.query(ListPosts, many)\nSELECT id, title FROM posts;\n"

			defaults := generateFeatureProject(t, target, queries, docComments(false))
			require.NotContains(t, defaults["models.go"], "Posts are the articles")
			require.NotContains(t, defaults["models.go"], "The headline")

			documented := generateFeatureProject(t, target, queries, docComments(true))
			require.Contains(t, documented["models.go"], "// Posts are the articles authors publish.\ntype Posts struct {")
			require.Contains(t, documented["models.go"], testCase.field)
			require.NotContains(t, documented["models.go"], "Removed below.")
			require.NotContains(t, documented["posts.sql.go"], "The headline", "a row's fields carry no column comment")
		})
	}
}

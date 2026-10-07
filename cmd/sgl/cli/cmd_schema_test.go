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

package cli_test

import (
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/sgl/internal/clitest"
)

const viewMigration = "CREATE TABLE posts (id INTEGER PRIMARY KEY, author_id INTEGER NOT NULL REFERENCES authors (id), title TEXT NOT NULL);\n" +
	"CREATE VIEW author_posts (author, title, total) AS\n" +
	"  SELECT a.name, p.title, (SELECT count(*) FROM posts) FROM authors AS a LEFT JOIN posts AS p ON p.author_id = a.id;\n"

type dumpedSchema struct {
	Databases []struct {
		Database string `json:"database"`
		Tables   []struct {
			Schema  string `json:"schema"`
			Name    string `json:"name"`
			Columns []struct {
				Name     string `json:"name"`
				Type     string `json:"type"`
				Nullable bool   `json:"nullable"`
			} `json:"columns"`
			PrimaryKey []string `json:"primary_key"`
		} `json:"tables"`
	} `json:"databases"`
}

type dumpedViews struct {
	Databases []struct {
		Views []struct {
			Name    string `json:"name"`
			Columns []struct {
				Name     string `json:"name"`
				Type     string `json:"type"`
				Nullable bool   `json:"nullable"`
			} `json:"columns"`
		} `json:"views"`
	} `json:"databases"`
}

func TestSchemaDumpFromMigrations(t *testing.T) {
	t.Parallel()

	project := sqliteProject(t)
	project.Write("db/migrations/0002_view.up.sql", "CREATE VIEW author_names AS SELECT id, name FROM authors;\n")
	result := project.Run("schema", "dump")
	requireCode(t, result, 0)
	var dumped dumpedSchema
	require.NoError(t, json.Unmarshal([]byte(result.Stdout), &dumped))
	require.Len(t, dumped.Databases, 1)
	require.Len(t, dumped.Databases[0].Tables, 1)
	table := dumped.Databases[0].Tables[0]
	assert.Equal(t, "authors", table.Name)
	assert.Equal(t, []string{"id"}, table.PrimaryKey)
	require.Len(t, table.Columns, 3)
	assert.Equal(t, "bigint", table.Columns[0].Type)
	assert.True(t, table.Columns[2].Nullable)

	result = project.Run("schema", "dump", "--format", "sql")
	requireCode(t, result, 0)
	assert.Contains(t, result.Stdout, "CREATE TABLE main.authors (\n    id bigint NOT NULL,\n    name text NOT NULL,\n    bio text,\n    PRIMARY KEY (id)\n);")
	assert.Contains(t, result.Stdout, "-- view main.author_names: id bigint, name text")

	requireCode(t, project.Run("schema", "dump", "--format", "yaml"), 2)
	requireCode(t, project.Run("schema", "dump", "extra"), 2)
	requireCode(t, project.Run("schema"), 2)
	requireCode(t, project.Run("schema", "draw"), 2)
}

func TestSchemaDumpFromALiveDatabaseAndDiff(t *testing.T) {
	t.Parallel()

	project, dsn := databaseProject(t)
	requireCode(t, project.Run("migrate", "up", "--dsn", dsn), 0)

	result := project.Run("schema", "dump", "--from-db", dsn, "--format", "sql")
	requireCode(t, result, 0)
	assert.Contains(t, result.Stdout, "CREATE TABLE main.authors")
	assert.NotContains(t, result.Stdout, "sgl_migrations", "the runtime's own tables are not part of the schema")

	result = project.Run("schema", "diff", "--dsn", dsn)
	requireCode(t, result, 0)
	assert.Contains(t, result.Stdout, "the database matches the migrations")

	database, err := sql.Open("sqlite", project.Path("app.db"))
	require.NoError(t, err)
	_, err = database.Exec("ALTER TABLE authors ADD COLUMN email TEXT; CREATE TABLE extra (id INTEGER); ALTER TABLE authors DROP COLUMN bio;")
	require.NoError(t, err)
	require.NoError(t, database.Close())
	project.Write("db/migrations/0002_more.up.sql", "CREATE TABLE planned (id INTEGER);\nALTER TABLE authors ADD COLUMN age INTEGER NOT NULL DEFAULT 0;\n")

	result = project.Run("schema", "diff", "--dsn", dsn)
	requireCode(t, result, 1)
	assert.Contains(t, result.Stdout, "+ column main.authors.email is in the database but not the migrations")
	assert.Contains(t, result.Stdout, "- column main.authors.bio is in the migrations but not the database")
	assert.Contains(t, result.Stdout, "- column main.authors.age is in the migrations but not the database")
	assert.Contains(t, result.Stdout, "+ table main.extra is in the database but not the migrations")
	assert.Contains(t, result.Stdout, "- table main.planned is in the migrations but not the database")
	assert.NotContains(t, result.Stdout, "sgl_migrations")

	requireCode(t, project.Run("schema", "diff", "extra", "--dsn", dsn), 2)
}

func TestSchemaDiffNeedsAnIntrospectableDialect(t *testing.T) {
	t.Parallel()

	project := clitest.NewProject(t)
	project.Write("sgl.yaml", "databases:\n  - name: app\n    dialect: mysql\n")
	requireCode(t, project.Run("schema", "diff", "--dsn", "user:pass@tcp(127.0.0.1:1)/app?timeout=1s"), 4)

	project.Write("sgl.yaml", "databases:\n  - name: app\n    dialect: mysql\n    schema_from: user:pass@tcp(127.0.0.1:1)/app\n")
	result := project.Run("schema", "dump")
	requireCode(t, result, 2)
	assert.Contains(t, result.Stderr, "cannot be introspected")

	project.Write("sgl.yaml", "databases:\n  - name: one\n    dialect: sqlite\n    go: { out: gen/one }\n  - name: two\n    dialect: sqlite\n    go: { out: gen/two }\n")
	requireCode(t, project.Run("schema", "dump", "--from-db", "file:x.db"), 2)
}

func TestSchemaFromGeneratesFromALiveDatabase(t *testing.T) {
	t.Parallel()

	project, dsn := databaseProject(t)
	requireCode(t, project.Run("migrate", "up", "--dsn", dsn), 0)
	project.Write("sgl.yaml", "databases:\n  - name: app\n    dialect: sqlite\n    schema_from: "+dsn+"\n    migrations: nowhere\n")
	result := project.Run("generate")
	requireCode(t, result, 0)
	assert.Contains(t, project.Read("internal/store/example.sql.go"), "ID   int64")
}

func TestSchemaDumpFromADatabaseTypesViewsAsTheMigrationsDo(t *testing.T) {
	t.Parallel()

	project, dsn := databaseProject(t)
	project.Write("db/migrations/0002_posts.up.sql", viewMigration)
	requireCode(t, project.Run("migrate", "up", "--dsn", dsn), 0)

	dumps := map[string]dumpedViews{}
	for name, arguments := range map[string][]string{
		"migrations": {"schema", "dump"},
		"database":   {"schema", "dump", "--from-db", dsn},
	} {
		result := project.Run(arguments...)
		requireCode(t, result, 0)
		var dumped dumpedViews
		require.NoError(t, json.Unmarshal([]byte(result.Stdout), &dumped))
		dumps[name] = dumped
	}

	views := dumps["database"].Databases[0].Views
	require.Len(t, views, 1)
	assert.Equal(t, dumps["migrations"].Databases[0].Views, views)
	assert.Equal(t, "author", views[0].Columns[0].Name, "the view's own column names are kept")
	assert.False(t, views[0].Columns[0].Nullable, "authors.name is NOT NULL")
	assert.True(t, views[0].Columns[1].Nullable, "posts.title is nullable through the LEFT JOIN")
	assert.Equal(t, "bigint", views[0].Columns[2].Type, "SQLite declares no type for an expression")
}

func TestSchemaDumpFromADatabaseLeavesOutTheConfiguredRuntimeTables(t *testing.T) {
	t.Parallel()

	project, dsn := databaseProject(t)
	project.Write("sgl.yaml", project.Read("sgl.yaml")+"    migrate:\n      namespace: harbour\n")
	requireCode(t, project.Run("migrate", "up", "--dsn", dsn), 0)
	requireCode(t, project.Run("seed", "apply", "--dsn", dsn), 0)
	database, err := sql.Open("sqlite", project.Path("app.db"))
	require.NoError(t, err)
	_, err = database.Exec("CREATE TABLE sgl_migrations (id INTEGER)")
	require.NoError(t, err)
	require.NoError(t, database.Close())

	result := project.Run("schema", "dump", "--from-db", dsn, "--format", "sql")
	requireCode(t, result, 0)
	assert.NotContains(t, result.Stdout, "harbour_migrations")
	assert.NotContains(t, result.Stdout, "harbour_seeds")
	assert.Contains(t, result.Stdout, "CREATE TABLE main.sgl_migrations", "a table that only looks like the default names is the user's")
}

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

package starter_test

import (
	"io/fs"
	"path"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/cmd/internal/assemble"
	"seagull.sh/seagull/cmd/internal/config"
	"seagull.sh/seagull/cmd/internal/starter"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_postgres"
	"seagull.sh/seagull/sdk/dialect/dialect_registry"
)

func TestFilesLayout(t *testing.T) {
	t.Parallel()

	files, err := starter.Files("postgres", dialect_engine_postgres.Family)
	require.NoError(t, err)

	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
		require.True(t, strings.HasSuffix(file.Content, "\n"), file.Path)
	}
	require.Equal(t, []string{
		"sgl.yaml",
		"db/migrations/0001_init.up.sql",
		"db/migrations/0001_init.down.sql",
		"db/queries/example.sql",
		"db/seeds/0001_authors.sql",
	}, paths)
	require.Contains(t, files[0].Content, "    dialect: postgres\n")
	require.Equal(t, "CREATE TABLE authors (\n    id   BIGSERIAL PRIMARY KEY,\n    name TEXT NOT NULL,\n    bio  TEXT\n);\n", files[1].Content)
	require.Equal(t, "DROP TABLE authors;\n", files[2].Content)
	require.Equal(t, "-- sgl.query(GetAuthor, one)\nSELECT id, name, bio FROM authors WHERE id = $1;\n\n"+
		"-- sgl.query(ListAuthors, many)\nSELECT id, name, bio FROM authors ORDER BY name;\n\n"+
		"-- sgl.query(CreateAuthor, exec)\nINSERT INTO authors (name, bio) VALUES ($1, $2);\n", files[3].Content)
}

func TestFilesNameTheDialectGiven(t *testing.T) {
	t.Parallel()

	files, err := starter.Files("timescaledb", dialect_engine_postgres.Family)
	require.NoError(t, err)
	require.Contains(t, files[0].Content, "    dialect: timescaledb\n")
}

func TestFilesRejectAFamilyWithoutAStarter(t *testing.T) {
	t.Parallel()

	_, err := starter.Files("exotic", "exotic")
	require.ErrorIs(t, err, starter.ErrNoStarter)
	require.ErrorContains(t, err, "dialect exotic")
}

func TestEveryStockDialectHasAStarterThatGenerates(t *testing.T) {
	t.Parallel()

	for _, name := range dialect_registry.Names() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			built, err := dialect_registry.New(name, dialect_registry.Options{})
			require.NoError(t, err)
			files, err := starter.Files(name, built.Info().Family)
			require.NoError(t, err)

			project := fstest.MapFS{}
			for _, file := range files {
				project[file.Path] = &fstest.MapFile{Data: []byte(file.Content)}
			}
			configuration, err := config.ReadFS(project, "sgl.yaml")
			require.NoError(t, err)
			database := &configuration.Databases[0]
			require.Equal(t, name, database.Dialect)

			options, generatorDialect, err := assemble.Options(configuration, database, assemble.Factories{})
			require.NoError(t, err)
			generator, err := seagull.New(generatorDialect, options...)
			require.NoError(t, err)
			migrations, err := fs.Sub(project, path.Clean(database.Migrations))
			require.NoError(t, err)
			queries, err := fs.Sub(project, path.Clean(database.Queries))
			require.NoError(t, err)
			result, err := generator.Generate(t.Context(), seagull.Input{Migrations: migrations, Queries: queries, Package: database.Go.Package})
			require.NoError(t, err)
			require.NoError(t, result.Err())
			require.Empty(t, result.Diagnostics)
			require.Equal(t, 3, result.QueryCount)
		})
	}
}

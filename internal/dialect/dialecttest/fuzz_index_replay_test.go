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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/dialects"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

func TestIndexReplayPreservesDefinitionsAndDropsAcrossDialects(t *testing.T) {
	for _, name := range []string{"postgres", "sqlite", "duckdb", "mysql", "mariadb", "cockroachdb", "timescaledb"} {
		t.Run(name, func(t *testing.T) {
			for _, unique := range []bool{false, true} {
				qualifier := ""
				if unique {
					qualifier = "UNIQUE "
				}
				model, ok := fuzzcase.Decode(name, []byte{1})
				require.True(t, ok)
				migration := strings.Join(model.Migrations(), "\n") + "\nCREATE " + qualifier + "INDEX fuzz_model_index ON fuzz_items (category, id);"
				engine, err := dialects.New(name, dialects.Options{})
				require.NoError(t, err)
				generator, err := app.New(engine)
				require.NoError(t, err)
				for _, dropped := range []bool{false, true} {
					source := migration
					if dropped {
						source += "\nDROP INDEX fuzz_model_index"
						if name == "mysql" || name == "mariadb" {
							source += " ON fuzz_items"
						}
						source += ";"
					}
					catalogue, diagnostics := catalogueFromMigration(t, generator, source)
					require.Empty(t, diagnostics)
					table := catalogue.LookupTable("", "fuzz_items")
					require.NotNil(t, table)
					if dropped {
						assert.Empty(t, table.Indexes)
						continue
					}
					require.Len(t, table.Indexes, 1)
					index := table.Indexes[0]
					assert.Equal(t, "fuzz_model_index", index.Name)
					assert.Equal(t, []string{"category", "id"}, index.Columns)
					assert.Equal(t, unique, index.IsUnique)
				}
			}
		})
	}
}

func TestIndexReplayTracksRenamedColumnsAcrossDialects(t *testing.T) {
	for _, name := range []string{"postgres", "sqlite", "mysql", "mariadb", "cockroachdb", "timescaledb"} {
		t.Run(name, func(t *testing.T) {
			model, ok := fuzzcase.Decode(name, []byte{1})
			require.True(t, ok)
			source := strings.Join(model.Migrations(), "\n") + `
CREATE UNIQUE INDEX fuzz_model_index ON fuzz_items (category, id);
ALTER TABLE fuzz_items RENAME COLUMN category TO renamed;
ALTER TABLE fuzz_items RENAME TO renamed_items;`
			catalogue := replayIndexMigration(t, name, source)
			table := catalogue.LookupTable("", "renamed_items")
			require.NotNil(t, table)
			require.Len(t, table.Indexes, 1)
			assert.Equal(t, []string{"renamed", "id"}, table.Indexes[0].Columns)
			assert.True(t, table.Indexes[0].IsUnique)
		})
	}
}

func TestIndexReplayExcludesExpressionsFromPlainKeyMetadata(t *testing.T) {
	for _, name := range []string{"postgres", "sqlite", "duckdb", "mysql", "mariadb", "cockroachdb", "timescaledb"} {
		t.Run(name, func(t *testing.T) {
			model, ok := fuzzcase.Decode(name, []byte{1})
			require.True(t, ok)
			source := strings.Join(model.Migrations(), "\n") + `
CREATE INDEX fuzz_expression_index ON fuzz_items ((category + id), score DESC);`
			catalogue := replayIndexMigration(t, name, source)
			table := catalogue.LookupTable("", "fuzz_items")
			require.NotNil(t, table)
			require.Len(t, table.Indexes, 1)
			assert.Equal(t, []string{"score"}, table.Indexes[0].Columns)
		})
	}
}

func replayIndexMigration(t *testing.T, name, source string) *sqlmodel.Catalogue {
	t.Helper()
	catalogue, diagnostics := replayDialectCatalogue(t, name, source)
	require.Empty(t, diagnostics)
	return catalogue
}

func replayDialectCatalogue(t *testing.T, name, source string) (*sqlmodel.Catalogue, []diag.SourceError) {
	t.Helper()
	engine, err := dialects.New(name, dialects.Options{})
	require.NoError(t, err)
	generator, err := app.New(engine)
	require.NoError(t, err)
	return catalogueFromMigration(t, generator, source)
}

func catalogueFromMigration(t *testing.T, generator *app.Generator, source string) (*sqlmodel.Catalogue, []diag.SourceError) {
	t.Helper()
	files := fstest.MapFS{path.Join(migrationDirectory, "001_schema.up.sql"): &fstest.MapFile{Data: []byte(source)}}
	catalogue, diagnostics, err := generator.Catalogue(t.Context(), app.Input{Migrations: files, MigrationDir: migrationDirectory})
	require.NoError(t, err)
	return catalogue, diagnostics
}

func TestIndexReplayUsesTheDialectIndexNamespace(t *testing.T) {
	for _, name := range []string{"postgres", "sqlite", "duckdb", "mysql", "mariadb", "cockroachdb", "timescaledb"} {
		t.Run(name, func(t *testing.T) {
			model, ok := fuzzcase.Decode(name, []byte{1})
			require.True(t, ok)
			clause := "IF NOT EXISTS "
			if name == "mysql" {
				clause = ""
			}
			source := strings.Join(model.Migrations(), "\n") + "\nCREATE INDEX fuzz_model_index ON fuzz_items (category, id);\nCREATE INDEX " + clause + "fuzz_model_index ON fuzz_peers (id);"
			catalogue := replayIndexMigration(t, name, source)
			require.Len(t, catalogue.LookupTable("", "fuzz_items").Indexes, 1)
			expected := 0
			if name == "mysql" || name == "mariadb" || name == "cockroachdb" {
				expected = 1
			}
			require.Len(t, catalogue.LookupTable("", "fuzz_peers").Indexes, expected)
		})
	}
}

func TestIndexReplayDropsDependentKeysUsingDialectRules(t *testing.T) {
	for _, name := range []string{"postgres", "mysql", "mariadb", "cockroachdb", "timescaledb"} {
		for _, keys := range []string{"category", "category, id", "id, category"} {
			for _, qualifier := range []string{"", "UNIQUE "} {
				t.Run(name+"/"+qualifier+keys, func(t *testing.T) {
					model, ok := fuzzcase.Decode(name, []byte{1})
					require.True(t, ok)
					source := strings.Join(model.Migrations(), "\n") + "\nCREATE " + qualifier + "INDEX fuzz_model_index ON fuzz_items (" + keys + ");\nALTER TABLE fuzz_items DROP COLUMN category;"
					if name == "mariadb" && qualifier != "" && keys != "category" {
						catalogue := replayRejectedIndexMigration(t, name, source)
						table := catalogue.LookupTable("", "fuzz_items")
						require.Len(t, table.Columns, 3)
						require.Equal(t, strings.Split(keys, ", "), table.Indexes[0].Columns)
						require.True(t, table.Indexes[0].IsUnique)
						return
					}
					catalogue := replayIndexMigration(t, name, source)
					table := catalogue.LookupTable("", "fuzz_items")
					var visible []string
					for _, column := range table.Columns {
						if !column.Hidden {
							visible = append(visible, column.Name)
						}
					}
					require.Equal(t, []string{"id", "score"}, visible)
					if keys != "category" && (name == "mysql" || name == "mariadb") {
						require.Len(t, table.Indexes, 1)
						require.Equal(t, []string{"id"}, table.Indexes[0].Columns)
						require.Equal(t, qualifier != "", table.Indexes[0].IsUnique)
					} else {
						require.Empty(t, table.Indexes)
					}
				})
			}
		}
	}
}

func TestIndexReplayRejectsRestrictedColumnDropsWithoutChangingTheTable(t *testing.T) {
	for _, name := range []string{"sqlite", "duckdb"} {
		t.Run(name, func(t *testing.T) {
			model, ok := fuzzcase.Decode(name, []byte{1})
			require.True(t, ok)
			source := strings.Join(model.Migrations(), "\n") + "\nCREATE INDEX fuzz_model_index ON fuzz_items (category, id);\nALTER TABLE fuzz_items DROP COLUMN category;"
			catalogue := replayRejectedIndexMigration(t, name, source)
			table := catalogue.LookupTable("", "fuzz_items")
			require.Len(t, table.Columns, 3)
			require.Equal(t, []string{"category", "id"}, table.Indexes[0].Columns)
		})
	}
}

func replayRejectedIndexMigration(t *testing.T, name, source string) *sqlmodel.Catalogue {
	t.Helper()
	catalogue, diagnostics := replayDialectCatalogue(t, name, source)
	require.Len(t, diagnostics, 1)
	require.Contains(t, diagnostics[0].Message, "cannot drop column category while index fuzz_model_index depends on it")
	return catalogue
}

func TestCockroachIndexDropSelectsTheNamedOwner(t *testing.T) {
	for _, suffix := range []string{"", "IF EXISTS "} {
		model, ok := fuzzcase.Decode("cockroachdb", []byte{1})
		require.True(t, ok)
		source := strings.Join(model.Migrations(), "\n") + "\nCREATE INDEX fuzz_model_index ON fuzz_items (category, id);\nCREATE INDEX fuzz_model_index ON fuzz_peers (id);\nDROP INDEX " + suffix + "fuzz_peers@fuzz_model_index;"
		catalogue := replayIndexMigration(t, "cockroachdb", source)
		require.Len(t, catalogue.LookupTable("", "fuzz_items").Indexes, 1)
		require.Empty(t, catalogue.LookupTable("", "fuzz_peers").Indexes)
	}
}

func TestIndexReplayDropsEveryListedIndex(t *testing.T) {
	for _, name := range []string{"postgres", "cockroachdb", "timescaledb"} {
		for _, prefix := range []string{"", "IF EXISTS "} {
			t.Run(name+"/"+prefix, func(t *testing.T) {
				model, ok := fuzzcase.Decode(name, []byte{1})
				require.True(t, ok)
				source := strings.Join(model.Migrations(), "\n") + `
CREATE INDEX fuzz_first_index ON fuzz_items (category);
CREATE INDEX fuzz_second_index ON fuzz_items (id);
CREATE INDEX fuzz_kept_index ON fuzz_items (score);
DROP INDEX ` + prefix + `fuzz_first_index, fuzz_second_index;`
				catalogue := replayIndexMigration(t, name, source)
				indexes := catalogue.LookupTable("", "fuzz_items").Indexes
				require.Len(t, indexes, 1)
				require.Equal(t, "fuzz_kept_index", indexes[0].Name)
			})
		}
	}
}

func TestIndexReplayKeepsEachQualifiedDropOwner(t *testing.T) {
	for _, suffix := range []string{"", "IF EXISTS "} {
		source := `
CREATE SCHEMA fuzz_scope;
CREATE TABLE fuzz_scope.fuzz_items (id BIGINT);
CREATE TABLE fuzz_scope.fuzz_peers (id BIGINT);
CREATE INDEX "fuzz Key" ON fuzz_scope.fuzz_items (id);
CREATE INDEX "fuzz Key" ON fuzz_scope.fuzz_peers (id);
DROP INDEX ` + suffix + `fuzz_scope.fuzz_items@"fuzz Key", fuzz_scope.fuzz_peers@"fuzz Key";`
		catalogue := replayIndexMigration(t, "cockroachdb", source)
		require.Empty(t, catalogue.LookupTable("fuzz_scope", "fuzz_items").Indexes)
		require.Empty(t, catalogue.LookupTable("fuzz_scope", "fuzz_peers").Indexes)
	}
}

func TestIndexReplayHonoursNamesSharedWithRelations(t *testing.T) {
	for _, name := range []string{"postgres", "timescaledb", "cockroachdb", "duckdb", "mysql", "mariadb"} {
		t.Run(name, func(t *testing.T) {
			model, ok := fuzzcase.Decode(name, []byte{1})
			require.True(t, ok)
			conditional := "IF NOT EXISTS "
			if name == "mysql" {
				conditional = ""
			}
			source := strings.Join(model.Migrations(), "\n") + "\nCREATE TABLE fuzz_model_index (id BIGINT);\nCREATE INDEX " + conditional + "fuzz_model_index ON fuzz_items (category);"
			catalogue := replayIndexMigration(t, name, source)
			expected := 1
			if name == "postgres" || name == "timescaledb" {
				expected = 0
			}
			require.Len(t, catalogue.LookupTable("", "fuzz_items").Indexes, expected)
		})
	}
}

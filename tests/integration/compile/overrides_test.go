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

//go:build integration

package compile_test

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_postgres"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
	"seagull.sh/seagull/sdk/emit"
	"seagull.sh/seagull/sdk/emit/emit_driver_databasesql"
	"seagull.sh/seagull/sdk/emit/emit_driver_pgx"
)

const (
	ownTypeSource     = "package " + generatedPackage + "\n\ntype Mark string\n"
	overrideMigration = "CREATE TABLE items (id int PRIMARY KEY, label text NOT NULL, kind text, code text NOT NULL, " +
		"shape text, mark text NOT NULL, spare text, ident text NOT NULL, maybe text);\n" +
		"-- sgl.column(items.shape, go_type: \"" + targetModule + "/go-shapes.Point\")\n" +
		"-- sgl.column(items.spare, go_type: \"%s.Mark\")\n"
	overrideQueries = "-- sgl.query(GetItem, one)\nSELECT id, label, kind, code, shape, mark, spare, ident, maybe FROM items WHERE id = $1;\n\n" +
		"-- sgl.query(AddItem, exec)\nINSERT INTO items (id, label, kind, code, shape, mark, spare, ident, maybe) " +
		"VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);\n"
)

type overrideTarget struct {
	newDialect func() dialect.Dialect
	newEmitter func() emit.EmitterPort
	directory  string
}

func TestOverridePackagesCompile(t *testing.T) {
	requirements := targetRequirements(t)
	checksums, err := os.ReadFile("go.sum")
	require.NoError(t, err)
	moduleDirectory := t.TempDir()
	for name, content := range map[string]string{
		"null.v4/null.go":     "package null\n\ntype String string\n",
		"go-shapes/shapes.go": "package shapes\n\ntype Point string\n",
		"pkgdir/code.go":      "package otherpkg\n\ntype Code string\n",
		"opt/opt.go":          "package opt\n\ntype Option[T any] struct {\n\tValue T\n\tValid bool\n}\n",
	} {
		path := filepath.Join(moduleDirectory, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}

	targets := []overrideTarget{
		{directory: "postgres_databasesql", newDialect: func() dialect.Dialect { return dialect_engine_postgres.New() }, newEmitter: func() emit.EmitterPort { return emit_driver_databasesql.New() }},
		{directory: "postgres_pgx", newDialect: func() dialect.Dialect { return dialect_engine_postgres.New() }, newEmitter: func() emit.EmitterPort { return emit_driver_pgx.New() }},
		{directory: "sqlite_databasesql", newDialect: func() dialect.Dialect { return dialect_engine_sqlite.New() }, newEmitter: func() emit.EmitterPort { return emit_driver_databasesql.New() }},
	}
	for _, target := range targets {
		packagePath := targetModule + "/" + target.directory
		generator, err := seagull.New(target.newDialect(),
			seagull.WithEmitter(target.newEmitter()),
			seagull.WithLogger(slog.New(slog.DiscardHandler)),
			seagull.WithPackageNames(map[string]string{targetModule + "/pkgdir": "otherpkg"}),
			seagull.WithTypeOverrides(
				seagull.TypeOverride{Column: "items.label", GoPackage: targetModule + "/null.v4", GoName: "String"},
				seagull.TypeOverride{Column: "items.kind", GoPackage: targetModule + "/go-shapes", GoName: "Point"},
				seagull.TypeOverride{Column: "items.code", GoPackage: targetModule + "/pkgdir", GoName: "Code"},
				seagull.TypeOverride{Column: "items.mark", GoPackage: packagePath, GoName: "Mark"},
				seagull.TypeOverride{Column: "items.ident", GoPackage: "uuid", GoName: "UUID"},
				seagull.TypeOverride{Column: "items.maybe", GoPackage: targetModule + "/opt", GoName: "Option[" + targetModule + "/null.v4.String]"},
			),
		)
		require.NoError(t, err)
		project := fstest.MapFS{
			"migrations/0001_items.up.sql": &fstest.MapFile{Data: []byte(fmt.Sprintf(overrideMigration, packagePath))},
			"queries/items.sql":            &fstest.MapFile{Data: []byte(placeholders(target.directory, overrideQueries))},
		}
		result, err := generator.Generate(t.Context(), seagull.Input{
			Migrations: project, Package: generatedPackage, PackagePath: packagePath, MigrationDir: "migrations", QueryDir: "queries",
		})
		require.NoError(t, err)
		require.NoError(t, result.Err(), "%s: %v", target.directory, result.Diagnostics)
		directory := filepath.Join(moduleDirectory, target.directory)
		writePackage(t, directory, result.Files)
		require.NoError(t, os.WriteFile(filepath.Join(directory, "own.go"), []byte(ownTypeSource), 0o644))
	}

	writeTargetModule(t, moduleDirectory, requirements, checksums)
	runGo(t, moduleDirectory, nil, "build", "./...")
	runGo(t, moduleDirectory, nil, "vet", "./...")
}

func placeholders(directory, queries string) string {
	if directory != "sqlite_databasesql" {
		return queries
	}
	return strings.NewReplacer("$1", "?", "$2", "?", "$3", "?", "$4", "?", "$5", "?", "$6", "?", "$7", "?", "$8", "?", "$9", "?").Replace(queries)
}

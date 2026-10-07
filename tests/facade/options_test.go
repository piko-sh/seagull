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
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
	"seagull.sh/seagull/sdk/migrate"
)

func liveTables(names ...string) staticSchema {
	tables := make(map[string]*dialect.Table, len(names))
	for _, name := range names {
		tables[name] = &dialect.Table{Name: name, Schema: "main", Columns: []dialect.Column{
			dialect.NewColumn("id", dialect_engine_sqlite.New().NormaliseTypeName("INTEGER"), false),
		}}
	}
	return staticSchema{catalogue: &dialect.Catalogue{Schemas: map[string]*dialect.Schema{"main": {Name: "main", Tables: tables}}}}
}

func TestWithRuntimeTablesLeavesTheRuntimeTablesOutOfASchemaSource(t *testing.T) {
	t.Parallel()

	defaults := migrate.Backend{}.Names()
	tests := []struct {
		name     string
		tables   []string
		options  []seagull.Option
		expected []string
	}{
		{
			name:     "the runtime's default names are left out",
			tables:   []string{"authors", defaults.History, defaults.Seeds, defaults.MigrationLock, defaults.SeedLock},
			expected: []string{"authors"},
		},
		{
			name:     "names are matched without regard to case",
			tables:   []string{"authors", "SGL_Migrations"},
			expected: []string{"authors"},
		},
		{
			name:     "configured names replace the defaults",
			tables:   []string{"authors", "harbour_migrations", "sgl_migrations"},
			options:  []seagull.Option{seagull.WithRuntimeTables("harbour_migrations")},
			expected: []string{"authors", "sgl_migrations"},
		},
		{
			name:     "no names keep every table",
			tables:   []string{"authors", "sgl_migrations"},
			options:  []seagull.Option{seagull.WithRuntimeTables()},
			expected: []string{"authors", "sgl_migrations"},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			source := liveTables(testCase.tables...)
			generator, err := seagull.New(dialect_engine_sqlite.New(), append(testCase.options, seagull.WithSchemaSource(source))...)
			require.NoError(t, err)
			catalogue, _, err := generator.Catalogue(t.Context(), seagull.Input{Queries: fstest.MapFS{}})
			require.NoError(t, err)

			var names []string
			for _, table := range catalogue.Schemas["main"].Tables {
				names = append(names, table.Name)
			}
			require.ElementsMatch(t, testCase.expected, names)
			require.Len(t, source.catalogue.Schemas["main"].Tables, len(testCase.tables), "the source's own catalogue is left unchanged")
		})
	}
}

func TestWithRuntimeTablesLeavesMigrationsAlone(t *testing.T) {
	t.Parallel()

	project := tasksProject()
	project["db/migrations/0002_history.up.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE sgl_migrations (id INTEGER PRIMARY KEY);\n")}
	catalogue, _, err := mustGenerator(t).Catalogue(t.Context(), seagull.Input{Migrations: project, MigrationDir: "db/migrations"})
	require.NoError(t, err)

	require.Contains(t, catalogue.Schemas["main"].Tables, "sgl_migrations", "a table a migration creates is the user's")
}

var repositoryRoot = filepath.Join("..", "..")

var defaultStatement = regexp.MustCompile(`The default is [^.]+\.|at most \d[^,.;]*`)

func optionDocs(t *testing.T, path string) map[string]string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
	require.NoError(t, err)
	docs := make(map[string]string)
	for _, declaration := range file.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction || function.Doc == nil {
			continue
		}
		docs[function.Name.Name] = strings.Join(strings.Fields(function.Doc.Text()), " ")
	}
	require.NotEmpty(t, docs, "%s holds no documented function", path)
	return docs
}

func TestOptionDocsGiveTheDefaultsTheGeneratorApplies(t *testing.T) {
	t.Parallel()

	facade := optionDocs(t, filepath.Join(repositoryRoot, "options.go"))
	implementation := optionDocs(t, filepath.Join(repositoryRoot, "internal", "app", "options.go"))
	for _, option := range []string{"WithMaxFileBytes", "WithMaxTotalBytes", "WithMaxFiles", "WithMaxQueries", "WithMaxDiagnostics"} {
		t.Run(option, func(t *testing.T) {
			t.Parallel()

			statements := defaultStatement.FindAllString(implementation[option], -1)
			require.NotEmpty(t, statements, "the implementation's %s doc states no default", option)
			for _, statement := range statements {
				assert.Contains(t, facade[option], statement)
			}
		})
	}
}

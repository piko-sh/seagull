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

package thirdparty_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
	"seagull.sh/seagull/sdk/emit"
	"seagull.sh/seagull/sdk/emit/emit_driver_databasesql"
	"seagull.sh/seagull/tests/facade/thirdparty"
)

func sqliteCases(t *testing.T) []string {
	t.Helper()

	var cases []string
	for _, root := range sqliteTestdata() {
		matches, err := filepath.Glob(filepath.Join(root, "*", "testspec.json"))
		require.NoError(t, err)
		for _, match := range matches {
			cases = append(cases, filepath.Dir(match))
		}
	}
	require.NotEmpty(t, cases)
	return cases
}

func generateWith(t *testing.T, directory string, emitter emit.EmitterPort) *seagull.Result {
	t.Helper()

	generator, err := seagull.New(dialect_engine_sqlite.New(), seagull.WithEmitter(emitter), seagull.WithLogger(slog.New(slog.DiscardHandler)))
	require.NoError(t, err)
	project := os.DirFS(directory)
	result, err := generator.Generate(t.Context(), seagull.Input{Migrations: project, Queries: project, Package: "store", MigrationDir: "migrations", QueryDir: "queries"})
	require.NoError(t, err, directory)
	return result
}

func sqlConstants(t *testing.T, result *seagull.Result) map[string]string {
	t.Helper()

	constants := make(map[string]string)
	for _, file := range result.Files {
		if !strings.HasSuffix(file.Name, ".sql.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file.Name, file.Content, 0)
		require.NoError(t, err)
		for _, declaration := range parsed.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.CONST {
				continue
			}
			for _, specification := range general.Specs {
				value, isValue := specification.(*ast.ValueSpec)
				if !isValue {
					continue
				}
				literal, ok := value.Values[0].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				text, err := strconv.Unquote(literal.Value)
				require.NoError(t, err)
				constants[value.Names[0].Name] = text
			}
		}
	}
	return constants
}

func TestTheEmitterEmbedsTheSQLTheBuiltInEmitterEmbeds(t *testing.T) {
	t.Parallel()

	compared := 0
	for _, directory := range sqliteCases(t) {
		ours := sqlConstants(t, generateWith(t, directory, thirdparty.New()))
		if len(ours) == 0 {
			continue
		}
		theirs := sqlConstants(t, generateWith(t, directory, emit_driver_databasesql.New(emit_driver_databasesql.WithPrepared(false))))
		for name, text := range ours {
			require.Contains(t, theirs, name, directory)
			require.Equal(t, theirs[name], text, "%s: %s", directory, name)
			compared++
		}
	}
	t.Logf("%d SQL constants equal the built-in emitter's", compared)
	require.Greater(t, compared, 100)
}

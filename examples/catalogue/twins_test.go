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

package catalogue

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// twinConfig is an sgl.yaml with a comment, a quoted dialect and dialect_options, the
// key a careless rewrite would also match.
const twinConfig = `version: 1

# The orders database.
databases:
  - name: orders
    dialect: "postgres"
    dialect_options:
      max_parse_depth: 128
    migrations: db/migrations
    queries: db/queries
    go:
      package: store
      out: internal/store
      driver: database/sql
`

func TestRewriteDialectChangesOnlyTheDialect(t *testing.T) {
	rewritten, err := RewriteDialect([]byte(twinConfig), "cockroachdb")
	require.NoError(t, err)

	var before, after map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(twinConfig), &before))
	require.NoError(t, yaml.Unmarshal(rewritten, &after))

	// Setting the dialect back on the rewritten file must give the original back, so
	// nothing but the dialect changed.
	databases, ok := after["databases"].([]any)
	require.True(t, ok)
	require.Len(t, databases, 1)
	database, ok := databases[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "cockroachdb", database["dialect"])
	database["dialect"] = "postgres"
	require.Equal(t, before, after)

	// The comment survives the rewrite.
	require.Contains(t, string(rewritten), "# The orders database.")
}

func TestRewriteDialectChangesEveryDatabase(t *testing.T) {
	config := "version: 1\ndatabases:\n  - name: a\n    dialect: mysql\n  - name: b\n    dialect: mysql\n"
	rewritten, err := RewriteDialect([]byte(config), "mariadb")
	require.NoError(t, err)

	var parsed projectConfig
	require.NoError(t, yaml.Unmarshal(rewritten, &parsed))
	require.Len(t, parsed.Databases, 2)
	for _, database := range parsed.Databases {
		require.Equal(t, "mariadb", database.Dialect)
	}
}

func TestRewriteDialectRejectsConfigWithoutADialect(t *testing.T) {
	for name, config := range map[string]string{
		"empty":                        "",
		"no databases":                 "version: 1\n",
		"not a list":                   "version: 1\ndatabases: orders\n",
		"an empty list":                "version: 1\ndatabases: []\n",
		"no dialect":                   "version: 1\ndatabases:\n  - name: orders\n",
		"one database with no dialect": "version: 1\ndatabases:\n  - name: a\n    dialect: postgres\n  - name: b\n    dialet: postgres\n",
		"not yaml":                     "databases: [\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := RewriteDialect([]byte(config), "cockroachdb")
			require.Error(t, err)
		})
	}
}

func TestRewriteDialectKeepsToOneDocument(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config string
	}{
		{name: "an empty file", config: ""},
		{name: "a file of comments", config: "# no settings\n"},
		{name: "a second document the rewrite would drop", config: "databases:\n  - dialect: postgres\n---\ndatabases:\n  - dialect: mysql\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := RewriteDialect([]byte(test.config), "cockroachdb")
			require.ErrorIs(t, err, ErrDocumentCount)
		})
	}
}

func TestEquivalentsRewriteTheirTwinsConfig(t *testing.T) {
	curriculum, err := Load("..")
	require.NoError(t, err)
	cells := curriculum.Equivalents()
	require.NotEmpty(t, cells)

	for _, cell := range cells {
		twinDialect := curriculum.Engines[cell.Twin.Engine].Dialect
		require.NotEqual(t, twinDialect, cell.Dialect, "%s", cell)
		require.Equal(t, cell.Twin.Driver, cell.Column.Driver, "%s: an equivalent cell must use its twin's driver", cell)

		data, err := os.ReadFile(filepath.Join("..", cell.TwinDirectory, configFile))
		require.NoError(t, err, "%s", cell)
		rewritten, err := RewriteDialect(data, cell.Dialect)
		require.NoError(t, err, "%s", cell)

		var parsed projectConfig
		require.NoError(t, yaml.Unmarshal(rewritten, &parsed), "%s", cell)
		require.NotEmpty(t, parsed.Databases, "%s", cell)
		require.Equal(t, cell.Dialect, parsed.Databases[0].Dialect, "%s", cell)
	}
}

func TestEquivalentsForNamesTheTwinPackages(t *testing.T) {
	curriculum, err := Load("..")
	require.NoError(t, err)

	cells := curriculum.EquivalentsFor("cockroachdb")
	require.NotEmpty(t, cells)
	for _, cell := range cells {
		require.Equal(t, "cockroachdb", cell.Column.Engine)
		require.Equal(t, "postgres", cell.Module())
		require.DirExists(t, filepath.Join("..", cell.Module(), cell.Package()))
	}
}

// stubGenerate is a CommandRunner for sgl generate --check that reports the dialect it
// finds in the copied sgl.yaml, and exits with status when status is not nil.
//
// Takes status (error) which the run returns.
// Takes runs (*[][]string) which receives each run's arguments.
//
// Returns CommandRunner which is the stub.
func stubGenerate(t *testing.T, status error, runs *[][]string) CommandRunner {
	t.Helper()
	return func(_ context.Context, directory string, arguments []string) ([]byte, []byte, error) {
		*runs = append(*runs, arguments)
		data, err := os.ReadFile(filepath.Join(directory, configFile))
		if err != nil {
			return nil, nil, err
		}
		var config projectConfig
		if err := yaml.Unmarshal(data, &config); err != nil || len(config.Databases) == 0 {
			return nil, nil, errors.New("no databases")
		}
		return []byte("dialect " + config.Databases[0].Dialect + "\n"), []byte("checked\n"), status
	}
}

func TestCheckTwinsRunsSglOnEachTwinUnderTheCellsDialect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status        error
		name          string
		extra         []string
		wantArguments []string
		wantSame      bool
	}{
		{name: "a twin that generates the same code", status: nil, extra: nil, wantArguments: []string{"sgl", "generate", "--check"}, wantSame: true},
		{name: "a twin that differs, with --diff", status: exitedWith(1), extra: []string{"--diff"}, wantArguments: []string{"sgl", "generate", "--check", "--diff"}, wantSame: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			examples := writeExamples(t, nil)
			var runs [][]string
			results, err := CheckTwins(t.Context(), examples, t.TempDir(), "sgl", test.extra, stubGenerate(t, test.status, &runs))
			require.NoError(t, err)

			require.Equal(t, [][]string{test.wantArguments}, runs)
			require.Len(t, results, 1)
			require.Equal(t, "cockroachdb", results[0].Cell.Column.ID)
			require.Equal(t, filepath.Join("postgres", "001_first_query"), results[0].Cell.TwinDirectory)
			require.Equal(t, test.wantSame, results[0].Same)
			require.Equal(t, "dialect cockroachdb\nchecked\n", results[0].Output)

			committed, err := os.ReadFile(filepath.Join(examples, "postgres", "001_first_query", configFile))
			require.NoError(t, err)
			require.Contains(t, string(committed), "dialect: postgres")
		})
	}
}

func TestCheckTwinsRefusesWhatItCannotCheck(t *testing.T) {
	t.Parallel()

	stopped := errors.New("the check was stopped")
	tests := []struct {
		run      CommandRunner
		want     error
		prepare  func(t *testing.T, examples, scratch string)
		name     string
		wantText string
		cancel   bool
	}{
		{
			name:     "a scratch directory that is not empty",
			prepare:  func(t *testing.T, _, scratch string) { writeFiles(t, scratch, map[string]string{"0/sgl.yaml": "x"}) },
			want:     ErrNotEmpty,
			wantText: "",
		},
		{
			name:     "a context already done",
			prepare:  func(*testing.T, string, string) {},
			cancel:   true,
			want:     stopped,
			wantText: "1 first-query (cockroachdb, as postgres/001_first_query)",
		},
		{
			name:    "an sgl that cannot start",
			prepare: func(*testing.T, string, string) {},
			run: func(context.Context, string, []string) ([]byte, []byte, error) {
				return nil, nil, errors.New("no such file")
			},
			want:     nil,
			wantText: "running sgl: no such file",
		},
		{
			name: "a twin whose sgl.yaml names no dialect",
			prepare: func(t *testing.T, examples, _ string) {
				writeFiles(t, examples, map[string]string{"postgres/001_first_query/" + configFile: "version: 1\ndatabases:\n  - name: tasks\n"})
			},
			want:     nil,
			wantText: "database 1 names no dialect",
		},
		{
			name: "a curriculum that cannot be read",
			prepare: func(t *testing.T, examples, _ string) {
				writeFiles(t, examples, map[string]string{ConceptsFile: "revision: 1\n"})
			},
			want:     nil,
			wantText: "field revision not found",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			examples, scratch := writeExamples(t, nil), t.TempDir()
			test.prepare(t, examples, scratch)
			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			if test.cancel {
				cancel(stopped)
			}
			var runs [][]string
			run := test.run
			if run == nil {
				run = stubGenerate(t, nil, &runs)
			}
			_, err := CheckTwins(ctx, examples, scratch, "sgl", nil, run)
			require.Error(t, err)
			if test.want != nil {
				require.ErrorIs(t, err, test.want)
			}
			require.ErrorContains(t, err, test.wantText)
			require.Empty(t, runs)
		})
	}
}

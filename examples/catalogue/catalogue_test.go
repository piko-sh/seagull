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
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	// licensedGo is a hand-written Go file with the licence marker Validate looks for.
	licensedGo = "// Licensed under the Apache License, Version 2.0\n\npackage main\n"

	// fixtureConcepts is a small curriculum: SQLite and PostgreSQL variants of one
	// concept, and a CockroachDB cell equivalent to the PostgreSQL one.
	fixtureConcepts = `version: 1
engines:
  sqlite: {title: SQLite, dialect: sqlite, module: sqlite}
  postgres: {title: PostgreSQL, dialect: postgres, module: postgres}
  cockroachdb: {title: CockroachDB, dialect: cockroachdb, module: cockroachdb, twin: postgres}
columns:
  - {id: sqlite, engine: sqlite, driver: database/sql, suffix: ""}
  - {id: postgres, engine: postgres, driver: database/sql, suffix: ""}
  - {id: cockroachdb, engine: cockroachdb, driver: database/sql, suffix: ""}
tracks:
  - {id: start, title: Getting started, tone: harbour}
concepts:
  - number: 1
    slug: first-query
    title: Your first query
    track: start
    summary: One query.
    cells:
      sqlite: {status: variant}
      postgres: {status: variant}
      cockroachdb: {status: equivalent, twin: postgres}
`
)

// fixtureVariant returns the files of a finished variant of the fixture's first
// concept, relative to its directory.
//
// Takes column (string) which is the variant's column and engine.
// Takes expected (string) which is its expected_diagnostics flow list.
//
// Returns map[string]string which maps each file to its contents.
func fixtureVariant(column, expected string) map[string]string {
	live := "no"
	if column == "sqlite" {
		live = `"yes"`
	}
	return map[string]string{
		VariantFile: "summary: A first query.\n" +
			"entry: {file: db/queries/tasks.sql, query: CreateTask}\nlive: " + live + "\n" +
			"test: in-process\nexpected_diagnostics: " + expected + "\n",
		configFile: "version: 1\ndatabases:\n  - name: tasks\n    dialect: " + column + "\n" +
			"    migrations: db/migrations\n    queries: db/queries\n    go:\n      package: store\n      out: internal/store\n",
		"main.go":                          licensedGo,
		"main_test.go":                     licensedGo,
		"example.go":                       licensedGo,
		"output.txt":                       "created task 1\n",
		"db/migrations/0001_init.up.sql":   "CREATE TABLE tasks (id INTEGER PRIMARY KEY);\n",
		"db/queries/tasks.sql":             "-- sgl.query(CreateTask, one)\nINSERT INTO tasks DEFAULT VALUES RETURNING id;\n",
		"internal/store/models.go":         generatedMarker + "\n\npackage store\n",
		"internal/store/tasks.sql.go":      generatedMarker + "\n\npackage store\n",
		"db/seeds/0001_tasks.sql":          "INSERT INTO tasks DEFAULT VALUES;\n",
		"transcript.txt":                   "$ sgl generate\n",
		"README":                           "notes\n",
		"db/migrations/0001_init.down.sql": "DROP TABLE tasks;\n",
	}
}

// writeExamples writes the fixture examples directory under a temporary directory: the
// curriculum, its two variants, and the variants the diagnostics check treats
// specially. Each entry of overrides replaces or adds a file, by its path relative to
// the examples directory.
//
// Takes overrides (map[string]string) which replaces or adds files.
//
// Returns string which is the examples directory.
func writeExamples(t *testing.T, overrides map[string]string) string {
	t.Helper()
	examples := filepath.Join(t.TempDir(), "examples")
	files := map[string]string{
		ConceptsFile: fixtureConcepts,
		"embedding/050_schema_from_live_database/" + configFile:  "version: 1\n",
		"embedding/050_schema_from_live_database/" + VariantFile: "summary: A live schema.\n",
		"embedding/053_custom_sgl_binary/" + configFile:          "version: 1\n",
		"embedding/053_custom_sgl_binary/broken/" + configFile:   "version: 1\n",
		"embedding/053_custom_sgl_binary/" + VariantFile:         "expected_diagnostics: [Q001]\n",
		"embedding/053_custom_sgl_binary/main.go":                licensedGo,
		"tooling/042_ci_pipeline/" + configFile:                  "version: 1\n",
		"tooling/042_ci_pipeline/" + VariantFile:                 "expected_diagnostics: [Q046]\n",
		"tooling/042_ci_pipeline/db/queries/tasks.sql":           "before\n",
		"tooling/042_ci_pipeline/change/tasks.sql":               "after\n",
	}
	for _, column := range []string{"sqlite", "postgres"} {
		expected := "[]"
		if column == "sqlite" {
			expected = "[Q022]"
		}
		for name, content := range fixtureVariant(column, expected) {
			files[column+"/001_first_query/"+name] = content
		}
	}
	maps.Copy(files, overrides)
	writeFiles(t, examples, files)
	return examples
}

// writeFiles writes each file under root, making the directories it needs.
//
// Takes root (string) which is the directory the paths are relative to.
// Takes files (map[string]string) which maps slash-separated paths to contents.
func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
}

func TestCurriculumIsValid(t *testing.T) {
	require.NoError(t, Validate(".."))
}

func TestVariantDirectoryWritesTheSlugWithUnderscores(t *testing.T) {
	t.Parallel()

	curriculum := &Curriculum{Engines: map[string]Engine{"postgres": {Title: "PostgreSQL", Dialect: "postgres", Module: "postgres"}}}
	concept := &Concept{Number: 16, Slug: "one-to-many"}
	column := Column{ID: "postgres-pgx", Engine: "postgres", Driver: "pgx", Suffix: "_pgx"}

	require.Equal(t, filepath.Join("postgres", "016_one_to_many_pgx"), curriculum.VariantDirectory(concept, column))
}

func TestValidateReportsAVariantDirectoryNoCellNames(t *testing.T) {
	t.Parallel()

	examples := writeExamples(t, map[string]string{"sqlite/002_stray/" + VariantFile: "summary: Stray.\n"})

	require.ErrorContains(t, Validate(examples), filepath.Join("sqlite", "002_stray")+": no concept cell names this directory")
}

func TestTheFixtureExamplesAreValid(t *testing.T) {
	t.Parallel()

	require.NoError(t, Validate(writeExamples(t, nil)))
}

func TestLoadRefusesAnUnknownKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want     error
		name     string
		concepts string
		wantText string
	}{
		{name: "the curriculum as written", concepts: fixtureConcepts, want: nil, wantText: ""},
		{name: "a mistyped top-level key", concepts: "versions: 1\n", want: nil, wantText: "field versions not found"},
		{name: "a misspelt cell key", concepts: "version: 1\nconcepts:\n  - number: 1\n    cells:\n      sqlite: {stauts: variant}\n", want: nil, wantText: "field stauts not found"},
		{name: "a misspelt engine key", concepts: "version: 1\nengines:\n  sqlite: {dialet: sqlite}\n", want: nil, wantText: "field dialet not found"},
		{name: "no document", concepts: "# nothing yet\n", want: ErrDocumentCount, wantText: "holds none"},
		{name: "a second document", concepts: "version: 1\n---\nversion: 2\n", want: ErrDocumentCount, wantText: "more than one"},
		{name: "malformed YAML", concepts: "version: [\n", want: nil, wantText: ConceptsFile},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			examples := t.TempDir()
			writeFiles(t, examples, map[string]string{ConceptsFile: test.concepts})
			curriculum, err := Load(examples)
			if test.wantText == "" {
				require.NoError(t, err)
				require.Equal(t, 1, curriculum.Version)
				return
			}
			require.ErrorContains(t, err, test.wantText)
			if test.want != nil {
				require.ErrorIs(t, err, test.want)
			}
		})
	}
}

func TestLoadVariantRefusesAnUnknownKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		manifest string
		wantText string
	}{
		{name: "a manifest as written", manifest: "summary: A.\nexpected_diagnostics: [Q022]\nentry: {file: a.sql, query: A}\n", wantText: ""},
		{name: "expected_diagnostic for expected_diagnostics", manifest: "summary: A.\nexpected_diagnostic: [Q022]\n", wantText: "field expected_diagnostic not found"},
		{name: "the camel-case spelling the website uses", manifest: "summary: A.\nexpectedDiagnostics: [Q022]\n", wantText: "field expectedDiagnostics not found"},
		{name: "the concept, which the directory gives", manifest: "concept: first-query\n", wantText: "field concept not found"},
		{name: "a misspelt entry key", manifest: "entry: {file: a.sql, qeury: A}\n", wantText: "field qeury not found"},
		{name: "a second document", manifest: "summary: a\n---\nsummary: b\n", wantText: "more than one"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			directory := t.TempDir()
			writeFiles(t, directory, map[string]string{VariantFile: test.manifest})
			variant, err := LoadVariant(directory)
			if test.wantText == "" {
				require.NoError(t, err)
				require.Equal(t, []string{"Q022"}, variant.ExpectedDiagnostics)
				return
			}
			require.ErrorContains(t, err, test.wantText)
			require.ErrorContains(t, err, filepath.Join(directory, VariantFile))
		})
	}
}

func TestReadInReadsOnlyBoundedRegularFiles(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	writeFiles(t, directory, map[string]string{
		"small.yaml":        "version: 1\n",
		"directory/a.yaml":  "a: 1\n",
		"at-the-limit.yaml": string(bytes.Repeat([]byte("x"), maxFileBytes)),
		"too-large.yaml":    string(bytes.Repeat([]byte("x"), maxFileBytes+1)),
	})
	tests := []struct {
		want     error
		name     string
		file     string
		wantSize int
	}{
		{name: "a small file", file: "small.yaml", want: nil, wantSize: len("version: 1\n")},
		{name: "a file at the limit", file: "at-the-limit.yaml", want: nil, wantSize: maxFileBytes},
		{name: "a file over the limit", file: "too-large.yaml", want: ErrFileTooLarge, wantSize: 0},
		{name: "a directory", file: "directory", want: ErrNotRegular, wantSize: 0},
		{name: "a missing file", file: "missing.yaml", want: os.ErrNotExist, wantSize: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			data, err := readIn(directory, test.file)
			if test.want != nil {
				require.ErrorIs(t, err, test.want)
				return
			}
			require.NoError(t, err)
			require.Len(t, data, test.wantSize)
		})
	}
}

func TestReadInRefusesAPathOutsideTheDirectory(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	writeFiles(t, parent, map[string]string{"secret.yaml": "a: 1\n", "inner/" + VariantFile: "a: 1\n"})
	_, err := readIn(filepath.Join(parent, "inner"), "../secret.yaml")
	require.Error(t, err)
}

func TestRequireEmptyAcceptsOnlyAnEmptyOrAbsentDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFiles(t, root, map[string]string{"full/a.txt": "a", "file.txt": "b"})
	require.NoError(t, os.Mkdir(filepath.Join(root, "empty"), 0o750))
	tests := []struct {
		want      error
		name      string
		directory string
		wantErr   bool
	}{
		{name: "an absent directory", directory: "absent", want: nil, wantErr: false},
		{name: "an empty directory", directory: "empty", want: nil, wantErr: false},
		{name: "a directory with a file", directory: "full", want: ErrNotEmpty, wantErr: true},
		{name: "a file", directory: "file.txt", want: nil, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := requireEmpty(filepath.Join(root, test.directory))
			if !test.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			if test.want != nil {
				require.ErrorIs(t, err, test.want)
			}
		})
	}
}

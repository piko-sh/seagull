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

package config

import (
	"os"
	"path/filepath"
	"testing"

	"io/fs"
	"strings"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

const minimalYAML = `
databases:
  - name: app
    dialect: postgres
`

type endlessFS struct {
	read int64
}

type endlessFile struct {
	filesystem *endlessFS
}

func (filesystem *endlessFS) Open(string) (fs.File, error) {
	return &endlessFile{filesystem: filesystem}, nil
}

func (*endlessFile) Stat() (fs.FileInfo, error) {
	return nil, fs.ErrInvalid
}

func (file *endlessFile) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = '#'
	}
	file.filesystem.read += int64(len(buffer))
	return len(buffer), nil
}

func (*endlessFile) Close() error {
	return nil
}

func TestParseRejectsDataAfterTheConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		data    string
		isJSON  bool
		problem string
	}{
		{name: "a second YAML document", data: minimalYAML + "---\ndatabases:\n  - name: lost\n    dialect: sqlite\n", problem: "more than one document"},
		{name: "a second YAML document with an unknown key", data: minimalYAML + "---\nunknown: 1\n", problem: "more than one document"},
		{name: "an empty second YAML document", data: minimalYAML + "---\n", problem: "more than one document"},
		{name: "a second JSON object", data: `{"databases":[{"name":"app","dialect":"postgres"}]} {"databases":[]}`, isJSON: true, problem: "trailing data"},
		{name: "trailing garbage after JSON", data: `{"databases":[{"name":"app","dialect":"postgres"}]} trailing garbage`, isJSON: true, problem: "trailing data"},
		{name: "a YAML document end marker", data: minimalYAML + "...\n"},
		{name: "a YAML document start marker", data: "---\n" + minimalYAML},
		{name: "white space after JSON", data: "{\"databases\":[{\"name\":\"app\",\"dialect\":\"postgres\"}]}\n\n  \n", isJSON: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			config, err := Parse([]byte(testCase.data), testCase.isJSON)

			if testCase.problem == "" {
				require.NoError(t, err)
				require.Len(t, config.Databases, 1)
				return
			}
			require.ErrorIs(t, err, ErrInvalid)
			require.ErrorContains(t, err, testCase.problem)
		})
	}
}

func TestExpandEnvironmentReportsEachUnsetVariable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		database    Database
		wantDSN     string
		wantSchema  string
		wantProblem []string
	}{
		{
			name:        "an unset schema_from",
			database:    Database{Name: "app", SchemaFrom: "${env:SCHEMA_DSN}"},
			wantSchema:  "${env:SCHEMA_DSN}",
			wantProblem: []string{`database "app": schema_from refers to ${env:SCHEMA_DSN}, which is not set`},
		},
		{
			name:        "a partly expanded connection string keeps its unset part",
			database:    Database{Name: "app", Migrate: Migrate{DSN: "postgres://${env:USER_NAME}:${env:PASSWORD}@localhost/app"}},
			wantDSN:     "postgres://sgl:${env:PASSWORD}@localhost/app",
			wantProblem: []string{`database "app": migrate.dsn refers to ${env:PASSWORD}, which is not set`},
		},
		{
			name:     "every reference set",
			database: Database{Name: "app", SchemaFrom: "${env:USER_NAME}", Migrate: Migrate{DSN: "${env:USER_NAME}"}},
			wantDSN:  "sgl", wantSchema: "sgl",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			config := &Config{Databases: []Database{testCase.database}}
			err := config.ExpandEnvironment(func(name string) (string, bool) {
				if name == "USER_NAME" {
					return "sgl", true
				}
				return "", false
			})

			require.Equal(t, testCase.wantDSN, config.Databases[0].Migrate.DSN)
			require.Equal(t, testCase.wantSchema, config.Databases[0].SchemaFrom)
			if len(testCase.wantProblem) == 0 {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrUnsetVariable)
			for _, problem := range testCase.wantProblem {
				require.ErrorContains(t, err, problem)
			}
		})
	}
}

func TestLoadLeavesAnUnsetVariableForTheCommandThatUsesIt(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "sgl.yaml")
	data := minimalYAML + "    schema_from: ${env:SGL_TEST_VARIABLE_THAT_IS_NEVER_SET}\n"
	require.NoError(t, os.WriteFile(path, []byte(data), 0o600))

	config, err := Load(path)

	require.NoError(t, err)
	require.Equal(t, "${env:SGL_TEST_VARIABLE_THAT_IS_NEVER_SET}", config.Databases[0].SchemaFrom)
	err = RequireExpanded("schema_from", config.Databases[0].SchemaFrom)
	require.ErrorIs(t, err, ErrUnsetVariable)
	require.ErrorContains(t, err, "schema_from refers to ${env:SGL_TEST_VARIABLE_THAT_IS_NEVER_SET}, which is not set")
}

func TestRequireExpandedNamesTheFirstUnsetVariable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		problem string
	}{
		{name: "an expanded connection string", value: "postgres://sgl@localhost/app"},
		{name: "no connection string", value: ""},
		{name: "one unset variable", value: "${env:DATABASE_URL}", problem: "migrate.dsn refers to ${env:DATABASE_URL}, which is not set"},
		{name: "two unset variables", value: "postgres://${env:USER_NAME}:${env:PASSWORD}@localhost/app", problem: "migrate.dsn refers to ${env:USER_NAME}, which is not set"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := RequireExpanded("migrate.dsn", testCase.value)

			if testCase.problem == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrUnsetVariable)
			require.ErrorContains(t, err, testCase.problem)
		})
	}
}

func TestExportedListsAreCopies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		list  func() []string
		first string
	}{
		{name: "file names", list: FileNames, first: "sgl.yaml"},
		{name: "default prefixes", list: DefaultPrefixes, first: "sgl"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			changed := testCase.list()
			require.Equal(t, testCase.first, changed[0])
			changed[0] = "changed"

			require.Equal(t, testCase.first, testCase.list()[0])
		})
	}
}

func TestParseAppliesDefaults(t *testing.T) {
	t.Parallel()

	config, err := Parse([]byte(minimalYAML), false)

	require.NoError(t, err)
	require.Equal(t, CurrentVersion, config.Version)
	require.Equal(t, []string{"sgl", "seagull"}, config.Directives.Prefixes)
	database := config.Databases[0]
	require.Equal(t, DefaultMigrationsDirectory, database.Migrations)
	require.Equal(t, DefaultQueriesDirectory, database.Queries)
	require.Equal(t, DefaultPackage, database.Go.Package)
	require.Equal(t, DefaultOutputDirectory, database.Go.Out)
	require.Equal(t, DefaultDriver, database.Go.Driver)
	require.Equal(t, DefaultNamespace, database.Migrate.Namespace)
	require.Equal(t, DefaultQueryNamesFile, database.Go.QueryNames.File)
	require.Equal(t, DefaultQueryNamesFunction, database.Go.QueryNames.Function)
	require.Equal(t, DefaultInterfaceName, database.Go.Interface.Name)
	require.False(t, database.Go.Interface.Enabled)
	require.Equal(t, JSONTags{Case: "", Omit: false, OmitEmpty: false}, database.Go.JSONTags)
	require.Nil(t, database.Go.StructTags)
	require.False(t, database.Go.EmptySlices)
	require.False(t, database.Go.DocComments)
	require.False(t, database.Go.ReuseModels)
	require.False(t, database.Go.WrapErrors)
}

func TestParseReadsTheGeneratedCodeOptions(t *testing.T) {
	t.Parallel()

	data := `
databases:
  - name: app
    dialect: postgres
    go:
      interface: { enabled: true, name: Store }
      json_tags: { case: camel, omitempty: true }
      struct_tags: { db: "{name}", yaml: "{name},omitempty" }
      empty_slices: true
      doc_comments: true
      reuse_models: true
      wrap_errors: true
      types:
        overrides:
          - column: users.email
            struct_tags: { validate: "required,email" }
`
	config, err := Parse([]byte(data), false)

	require.NoError(t, err)
	settings := config.Databases[0].Go
	require.Equal(t, Interface{Name: "Store", Enabled: true}, settings.Interface)
	require.Equal(t, JSONTags{Case: "camel", Omit: false, OmitEmpty: true}, settings.JSONTags)
	require.Equal(t, map[string]string{"db": "{name}", "yaml": "{name},omitempty"}, settings.StructTags)
	require.True(t, settings.EmptySlices)
	require.True(t, settings.DocComments)
	require.True(t, settings.ReuseModels)
	require.True(t, settings.WrapErrors)
	require.Equal(t, map[string]string{"validate": "required,email"}, settings.Types.Overrides[0].StructTags)
}

func TestParseFullYAML(t *testing.T) {
	t.Parallel()

	data := `
version: 1
directives:
  prefixes: [sgl]
  require_canonical: true
databases:
  - name: app
    dialect: mysql
    dialect_options: { max_parse_depth: 64, returning: true }
    migrations: sql/migrations
    queries: sql/queries
    seeds: sql/seeds
    custom_functions:
      - { name: similarity, returns: real, args: [text, text], nullable: null_on_null, min_args: 1 }
    go:
      package: store
      out: gen
      driver: database/sql
      prepared: true
      query_names: { enabled: true }
      types:
        decimal: string
        enum: string
        non_null_elements: true
        overrides:
          - { sql: public.mood, go: example.com/domain.Mood }
          - { column: users.settings, go: example.com/settings.Settings }
          - { column: posts.tags, non_null_elements: false }
    migrate:
      namespace: app
      history_table: schema_history
      lock: table
`
	config, err := Parse([]byte(data), false)

	require.NoError(t, err)
	database := config.Databases[0]
	require.True(t, config.Directives.RequireCanonical)
	require.Equal(t, 64, database.DialectOptions.MaxParseDepth)
	require.True(t, *database.DialectOptions.Returning)
	require.Equal(t, "string", database.Go.Types.Decimal)
	require.Equal(t, "string", database.Go.Types.Enum)
	require.Len(t, database.Go.Types.Overrides, 3)
	require.True(t, database.Go.Types.NonNullElements)
	require.Equal(t, new(false), database.Go.Types.Overrides[2].NonNullElements)
	require.Equal(t, "schema_history", database.Migrate.HistoryTable)
	require.True(t, database.Go.QueryNames.Enabled)
}

func TestParseJSON(t *testing.T) {
	t.Parallel()

	config, err := Parse([]byte(`{"databases":[{"name":"app","dialect":"sqlite"}]}`), true)

	require.NoError(t, err)
	require.Equal(t, "sqlite", config.Databases[0].Dialect)
}

func TestParseAcceptsTheSchemaAnEditorValidatesAgainst(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		data   string
		isJSON bool
	}{
		{name: "json", data: `{"$schema": "https://seagull.sh/sgl.schema.json", "databases":[{"name":"app","dialect":"sqlite"}]}`, isJSON: true},
		{name: "yaml", data: "$schema: https://seagull.sh/sgl.schema.json\ndatabases:\n  - name: app\n    dialect: sqlite\n", isJSON: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			config, err := Parse([]byte(testCase.data), testCase.isJSON)

			require.NoError(t, err)
			require.Equal(t, "https://seagull.sh/sgl.schema.json", config.Schema)
			require.Equal(t, "sqlite", config.Databases[0].Dialect)
		})
	}
}

func TestParseUnknownKeySuggestsNearest(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte("databases:\n  - name: app\n    dialect: postgres\n    migratons: db/migrations\n"), false)

	require.ErrorIs(t, err, ErrInvalid)
	require.Contains(t, err.Error(), `did you mean "migrations"?`)
}

func TestParseUnknownJSONKey(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`{"databses":[]}`), true)

	require.ErrorIs(t, err, ErrInvalid)
	require.Contains(t, err.Error(), `did you mean "databases"?`)
}

func TestParseSuggestsAKeyForEachUnknownKey(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte("databases:\n  - name: app\n    dialect: sqlite\n    querys: db/queries\n    go:\n      pakage: store\n"), false)

	require.ErrorIs(t, err, ErrInvalid)
	lines := strings.Split(err.Error(), "\n")
	require.Len(t, lines, 3)
	require.True(t, strings.HasSuffix(lines[1], `field querys not found in type config.Database; did you mean "queries"?`), lines[1])
	require.True(t, strings.HasSuffix(lines[2], `field pakage not found in type config.Go; did you mean "package"?`), lines[2])
}

func TestParseSuggestsOnlyKeysValidWhereTheUnknownKeyIs(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte("out: gen\ndatabases:\n  - name: app\n    dialect: sqlite\n"), false)

	require.ErrorIs(t, err, ErrInvalid)
	require.ErrorContains(t, err, "field out not found")
	require.NotContains(t, err.Error(), "did you mean")
}

func TestParseRejectsMalformedYAML(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte("databases: [\n"), false)

	require.ErrorIs(t, err, ErrInvalid)
}

func TestParseLeavesEnvironmentReferencesAlone(t *testing.T) {
	t.Setenv("SGL_TEST_DSN", "postgres://user:secret@localhost/app")

	data := "databases:\n  - name: app\n    dialect: postgres\n    schema_from: ${env:SGL_TEST_DSN}\n    migrate: { dsn: \"${env:SGL_TEST_DSN}\" }\n"
	config, err := Parse([]byte(data), false)

	require.NoError(t, err)
	require.Equal(t, "${env:SGL_TEST_DSN}", config.Databases[0].Migrate.DSN)
	require.Equal(t, "${env:SGL_TEST_DSN}", config.Databases[0].SchemaFrom)
}

func TestExpandEnvironmentOnlyInConnectionStrings(t *testing.T) {
	t.Parallel()

	data := "databases:\n  - name: app\n    dialect: postgres\n    queries: ${env:SGL_TEST_DSN}\n    schema_from: ${env:SGL_TEST_DSN}\n    migrate: { dsn: \"${env:SGL_TEST_DSN}?sslmode=disable\" }\n"
	config, err := Parse([]byte(data), false)
	require.NoError(t, err)

	require.NoError(t, config.ExpandEnvironment(func(name string) (string, bool) {
		if name == "SGL_TEST_DSN" {
			return "postgres://user:secret@localhost/app", true
		}
		return "", false
	}))

	database := config.Databases[0]
	require.Equal(t, "postgres://user:secret@localhost/app?sslmode=disable", database.Migrate.DSN)
	require.Equal(t, "postgres://user:secret@localhost/app", database.SchemaFrom)
	require.Equal(t, "${env:SGL_TEST_DSN}", database.Queries)
}

func TestLoadExpandsEnvironment(t *testing.T) {
	t.Setenv("SGL_TEST_LOAD_DSN", "file:app.db")

	path := filepath.Join(t.TempDir(), "sgl.yaml")
	require.NoError(t, os.WriteFile(path, []byte(minimalYAML+"    migrate: { dsn: \"${env:SGL_TEST_LOAD_DSN}\" }\n"), 0o600))

	config, err := Load(path)

	require.NoError(t, err)
	require.Equal(t, "file:app.db", config.Databases[0].Migrate.DSN)
}

func TestLoadRejectsOutputDirectoriesThatMeetAfterResolving(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "sgl.yaml")
	data := "databases:\n" +
		"  - { name: one, dialect: sqlite, go: { out: gen } }\n" +
		"  - { name: two, dialect: sqlite, go: { out: \"" + filepath.ToSlash(filepath.Join(root, "gen")) + "\" } }\n"
	require.NoError(t, os.WriteFile(path, []byte(data), 0o600))

	_, err := Load(path)

	require.ErrorIs(t, err, ErrInvalid)
	require.ErrorContains(t, err, "both write generated code")
}

func TestLoadNamesOutputDirectoriesRelativeToTheFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	elsewhere := filepath.ToSlash(filepath.Join(t.TempDir(), "elsewhere"))
	path := filepath.Join(root, "sgl.yaml")
	data := "databases:\n" +
		"  - { name: one, dialect: sqlite, go: { out: gen } }\n" +
		"  - { name: two, dialect: sqlite, go: { out: gen/two } }\n" +
		"  - { name: three, dialect: sqlite, go: { out: \"" + elsewhere + "\" } }\n" +
		"  - { name: four, dialect: sqlite, go: { out: \"" + elsewhere + "\" } }\n"
	require.NoError(t, os.WriteFile(path, []byte(data), 0o600))

	_, err := Load(path)

	var fileErr *FileError
	require.ErrorAs(t, err, &fileErr)
	require.ErrorContains(t, fileErr.Err, `database "two" go.out "gen/two" is inside database "one" go.out "gen"`)
	require.ErrorContains(t, fileErr.Err, `databases "three" and "four" both write generated code to go.out "`+elsewhere+`"`)
	require.NotContains(t, fileErr.Err.Error(), root)
}

func TestDiscoverSearchesParents(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "seagull.yaml"), []byte(minimalYAML), 0o600))

	found, err := Discover(nested)

	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "seagull.yaml"), found)
}

func TestDiscoverPrefersSglYAML(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "seagull.yaml"), []byte(minimalYAML), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "sgl.yaml"), []byte(minimalYAML), 0o600))

	found, err := Discover(root)

	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "sgl.yaml"), found)
}

func TestDiscoverNotFound(t *testing.T) {
	t.Parallel()

	_, err := Discover(t.TempDir())

	require.ErrorIs(t, err, ErrNotFound)
}

func TestLoadResolvesPathsAgainstTheFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "sgl.yaml")
	require.NoError(t, os.WriteFile(path, []byte(minimalYAML+"    seeds: db/seeds\n    go: { out: /abs/out }\n"), 0o600))

	config, err := Load(path)

	require.NoError(t, err)
	database := config.Databases[0]
	require.Equal(t, filepath.Join(root, DefaultMigrationsDirectory), database.Migrations)
	require.Equal(t, filepath.Join(root, "db/seeds"), database.Seeds)
	require.Equal(t, "/abs/out", database.Go.Out)
}

func TestLoadRefusesLargeFiles(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "sgl.yaml")
	require.NoError(t, os.WriteFile(path, []byte("#"+strings.Repeat("x", MaxFileBytes)), 0o600))

	_, err := Load(path)

	require.ErrorIs(t, err, ErrTooLarge)
}

func TestLoadMissingFile(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	for _, path := range []string{filepath.Join(directory, "missing.yaml"), filepath.Join(directory, "missing", "sgl.yaml")} {
		_, err := Load(path)

		require.ErrorIs(t, err, fs.ErrNotExist, path)
		var fileErr *FileError
		require.ErrorAs(t, err, &fileErr, path)
		require.Equal(t, path, fileErr.Path)
		require.Equal(t, path+": "+fileErr.Err.Error(), err.Error())
		require.NotContains(t, fileErr.Err.Error(), directory)
		require.NotContains(t, err.Error(), "statat")
	}
}

func TestLoadReportsPathOnInvalidFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "sgl.yaml")
	require.NoError(t, os.WriteFile(path, []byte("version: 1\n"), 0o600))

	_, err := Load(path)

	require.ErrorContains(t, err, path)
	var fileErr *FileError
	require.ErrorAs(t, err, &fileErr)
	require.Equal(t, path, fileErr.Path)
	require.ErrorIs(t, fileErr, ErrInvalid)
	require.NotContains(t, fileErr.Err.Error(), path)
}

func TestReadFS(t *testing.T) {
	t.Parallel()

	filesystem := fstest.MapFS{
		"sgl.yaml":  {Data: []byte(minimalYAML)},
		"large.yml": {Data: []byte(strings.Repeat("x", MaxFileBytes+1))},
	}

	config, err := ReadFS(filesystem, "sgl.yaml")
	require.NoError(t, err)
	require.Equal(t, DefaultMigrationsDirectory, config.Databases[0].Migrations)

	_, err = ReadFS(filesystem, "large.yml")
	require.ErrorIs(t, err, ErrTooLarge)

	_, err = ReadFS(filesystem, "missing.yaml")
	require.Error(t, err)
}

func TestReadFSStopsReadingPastTheLimit(t *testing.T) {
	t.Parallel()

	filesystem := &endlessFS{read: 0}

	_, err := ReadFS(filesystem, "sgl.yaml")

	require.ErrorIs(t, err, ErrTooLarge)
	require.LessOrEqual(t, filesystem.read, int64(MaxFileBytes+1))
}

func TestReadFSLeavesEnvironmentReferencesAlone(t *testing.T) {
	t.Setenv("SGL_TEST_READFS_DSN", "file:app.db")

	filesystem := fstest.MapFS{"sgl.yaml": {Data: []byte(minimalYAML + "    migrate: { dsn: \"${env:SGL_TEST_READFS_DSN}\" }\n")}}

	config, err := ReadFS(filesystem, "sgl.yaml")

	require.NoError(t, err)
	require.Equal(t, "${env:SGL_TEST_READFS_DSN}", config.Databases[0].Migrate.DSN)
}

func TestDatabaseLookup(t *testing.T) {
	t.Parallel()

	config, err := Parse([]byte(minimalYAML), false)
	require.NoError(t, err)

	database, ok := config.Database("app")
	require.True(t, ok)
	require.Equal(t, "postgres", database.Dialect)

	_, ok = config.Database("missing")
	require.False(t, ok)
}

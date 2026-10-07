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
	"context"
	"database/sql"
	"errors"
	"regexp"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/cmd/sgl/cli"
	"seagull.sh/seagull/cmd/sgl/internal/clitest"
	"seagull.sh/seagull/internal/testkit/fakedialect"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
	"seagull.sh/seagull/sdk/dialect/dialect_registry"
	"seagull.sh/seagull/sdk/emit"
	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_sqlite"
)

var stockProgramName = regexp.MustCompile("(?m)(^|[\\s\"`])sgl([\\s:`\"]|$)")

type recordingEmitter struct {
	family  string
	queries int
}

func exoticFamilyDialect() dialect.Dialect {
	sqlite := dialect_engine_sqlite.New()
	return fakedialect.Renamed(sqlite, sqlite.Info().Name, "exotic")
}

func (r *recordingEmitter) Emit(_ context.Context, request *emit.Request) ([]emit.GeneratedFile, error) {
	r.queries = len(request.Queries)
	return []emit.GeneratedFile{{Name: "recorded.go", Content: []byte("// recorded\n")}}, nil
}

func TestWithMigrationBackendServesAThirdPartyDialect(t *testing.T) {
	t.Parallel()

	project := sqliteProject(t)
	project.Write("sgl.yaml", "databases:\n  - name: app\n    dialect: acme\n    migrate:\n      namespace: acme\n")
	dsn := "file:" + project.Path("acme.db")
	options := []cli.Option{
		cli.WithDialect("acme", func(dialect_registry.Options) (dialect.Dialect, error) {
			return fakedialect.Renamed(dialect_engine_sqlite.New(), "acme", "acme"), nil
		}),
		cli.WithDriver("acme", "sqlite"),
	}

	result := project.RunWith(t.Context(), options, "migrate", "up", "--dsn", dsn)
	requireCode(t, result, 2)
	assert.Contains(t, result.Stderr, `dialect acme (named "acme", family "acme") has no migration backend`)
	assert.Contains(t, result.Stderr, "cli.WithMigrationBackend")

	result = project.RunWith(t.Context(), options, "dialects")
	requireCode(t, result, 0)
	assert.Regexp(t, `acme\s+acme\s+\?1\s+\S.*\s+no\s+no\s+sqlite`, result.Stdout)

	built := 0
	withBackend := slices.Concat(options, []cli.Option{cli.WithMigrationBackend("acme", func(backendOptions ...migrate.BackendOption) migrate.Backend {
		built++
		return migrate_backend_sqlite.New(backendOptions...)
	})})
	result = project.RunWith(t.Context(), withBackend, "migrate", "up", "--dsn", dsn)
	requireCode(t, result, 0)
	assert.Equal(t, 1, built)
	assert.Contains(t, result.Stdout, "1 applied")

	database, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, database.Close()) })
	var tables int
	require.NoError(t, database.QueryRowContext(t.Context(), "SELECT count(*) FROM sqlite_master WHERE name = 'acme_migrations'").Scan(&tables))
	assert.Equal(t, 1, tables, "the backend received the configured namespace")

	result = project.RunWith(t.Context(), withBackend, "dialects")
	requireCode(t, result, 0)
	assert.Regexp(t, `acme\s+acme\s+\?1\s+\S.*\s+yes\s+no\s+sqlite`, result.Stdout)
}

func TestWithMigrationBackendReplacesTheStockBackendOfAName(t *testing.T) {
	t.Parallel()

	project, dsn := databaseProject(t)
	replaced := 0
	options := []cli.Option{cli.WithMigrationBackend("sqlite", func(backendOptions ...migrate.BackendOption) migrate.Backend {
		replaced++
		return migrate_backend_sqlite.New(backendOptions...)
	})}

	requireCode(t, project.RunWith(t.Context(), options, "migrate", "up", "--dsn", dsn), 0)
	requireCode(t, project.RunWith(t.Context(), options, "seed", "status", "--dsn", dsn), 0)
	assert.Equal(t, 2, replaced)
}

func TestOptionsRefuseValuesTheyCannotUse(t *testing.T) {
	t.Parallel()

	stockDialect := func(options dialect_registry.Options) (dialect.Dialect, error) {
		return dialect_registry.New("sqlite", options)
	}
	stockBackend := func(backendOptions ...migrate.BackendOption) migrate.Backend {
		return migrate_backend_sqlite.New(backendOptions...)
	}
	tests := []struct {
		name    string
		option  cli.Option
		message string
	}{
		{name: "a dialect without a name", option: cli.WithDialect("", stockDialect), message: "WithDialect needs a name"},
		{name: "a dialect without a factory", option: cli.WithDialect("acme", nil), message: `WithDialect("acme") needs a value it can use`},
		{name: "an emitter without a name", option: cli.WithEmitter("", func(*cli.EmitterRequest) (emit.EmitterPort, error) { return nil, nil }), message: "WithEmitter needs a name"},
		{name: "an emitter without a factory", option: cli.WithEmitter("acme", nil), message: `WithEmitter("acme") needs a value it can use`},
		{name: "a migration backend without a name", option: cli.WithMigrationBackend("", stockBackend), message: "WithMigrationBackend needs a name"},
		{name: "a migration backend without a factory", option: cli.WithMigrationBackend("acme", nil), message: `WithMigrationBackend("acme") needs a value it can use`},
		{name: "a driver for no dialect", option: cli.WithDriver("", "sqlite"), message: "WithDriver needs a name"},
		{name: "a driver without a name", option: cli.WithDriver("acme", ""), message: `WithDriver("acme") needs a value it can use`},
		{name: "an empty program name", option: cli.WithName(""), message: "WithName needs a name"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := clitest.NewProject(t).RunWith(t.Context(), []cli.Option{testCase.option}, "version")

			requireCode(t, result, 2)
			assert.Contains(t, result.Stderr, "invalid option: "+testCase.message)
			assert.Empty(t, result.Stdout, "nothing runs")
		})
	}
}

func TestWithNameNamesTheProgramInWhatItPrints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		arguments []string
		want      []string
		code      int
	}{
		{name: "the help", arguments: []string{"help"}, code: 0, want: []string{
			"seagull turns plain SQL into type-safe Go.", "\n  seagull [global flags] <command> [flags] [args]\n", `Run "seagull <command> -h"`}},
		{name: "a command's usage", arguments: []string{"init", "-h"}, code: 0, want: []string{
			"Usage: seagull init [--dialect name]\n", `"seagull dialects" lists them`}},
		{name: "a group's usage", arguments: []string{"schema"}, code: 2, want: []string{
			"Usage: seagull schema dump ", " | seagull schema diff ", `Run "seagull schema <subcommand> -h"`}},
		{name: "an unknown command", arguments: []string{"no-such-command"}, code: 2, want: []string{
			"seagull: unknown command \"no-such-command\"; run `seagull help`\n"}},
		{name: "an unknown dialect", arguments: []string{"init", "--dialect", "oracle"}, code: 2, want: []string{
			`seagull: database "app": unknown dialect "oracle"; this seagull knows clickhouse, `}},
		{name: "the version", arguments: []string{"version"}, code: 0, want: []string{"seagull        "}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := clitest.NewProject(t).RunWith(t.Context(), []cli.Option{cli.WithName("seagull")}, testCase.arguments...)

			requireCode(t, result, testCase.code)
			printed := result.Stdout + result.Stderr
			for _, want := range testCase.want {
				assert.Contains(t, printed, want)
			}
			assert.NotRegexp(t, stockProgramName, printed)
		})
	}
}

func TestWithNameNamesTheProgramInTheHintUnderAnError(t *testing.T) {
	t.Parallel()

	project, dsn := databaseProject(t)
	project.Write("db/migrations/0002_harbours.up.sql", "CREATE TABLE harbours (id INTEGER PRIMARY KEY);\n")
	named := []cli.Option{cli.WithName("seagull")}

	result := project.RunWith(t.Context(), named, "migrate", "force", "2", "--yes", "--dsn", dsn)
	requireCode(t, result, 0)
	assert.Contains(t, result.Stderr, "Migration 2 (harbours) is pending. seagull will record it as applied")

	result = project.RunWith(t.Context(), named, "migrate", "force", "1", "--yes", "--dsn", dsn)
	requireCode(t, result, 1)
	assert.Contains(t, result.Stderr, "seagull: cannot force migration 1: migration 2 after it is applied")
	assert.Contains(t, result.Stderr, "\nhint: run `seagull migrate down` first, or `seagull migrate force 2` instead\n")
	assert.NotRegexp(t, stockProgramName, result.Stdout+result.Stderr)
}

func TestMainContextSkipsANilOption(t *testing.T) {
	t.Parallel()

	result := clitest.NewProject(t).RunWith(t.Context(), []cli.Option{nil}, "version")

	requireCode(t, result, 0)
	assert.Contains(t, result.Stdout, "library")
}

func TestCustomDialectEmitterAndDriver(t *testing.T) {
	t.Parallel()

	project := sqliteProject(t)
	project.Write("sgl.yaml", "databases:\n  - name: app\n    dialect: lite\n    go:\n      driver: recording\n")
	recorder := &recordingEmitter{}
	options := []cli.Option{
		cli.WithDialect("lite", func(options dialect_registry.Options) (dialect.Dialect, error) {
			return dialect_engine_sqlite.New(dialect_engine_sqlite.WithName("lite")), nil
		}),
		cli.WithEmitter("recording", func(request *cli.EmitterRequest) (emit.EmitterPort, error) {
			recorder.family = request.Dialect.Family
			return recorder, nil
		}),
		cli.WithDriver("lite", "sqlite"),
		cli.WithDriver("duckdb", "duckdb-driver"),
	}

	result := project.RunWith(t.Context(), options, "generate")
	requireCode(t, result, 0)
	assert.Equal(t, "sqlite", recorder.family)
	assert.Equal(t, 3, recorder.queries)
	assert.Equal(t, "// recorded\n", project.Read("internal/store/recorded.go"))

	result = project.RunWith(t.Context(), options, "dialects")
	requireCode(t, result, 0)
	assert.Contains(t, result.Stdout, "lite ")
	assert.Regexp(t, `duckdb\s+duckdb\s+\$1\s+INSERT, UPDATE, DELETE\s+yes\s+no\s+duckdb-driver`, result.Stdout)

	dsn := "file:" + project.Path("custom.db")
	requireCode(t, project.RunWith(t.Context(), options, "migrate", "up", "--dsn", dsn), 0)

	failing := []cli.Option{
		cli.WithDialect("lite", func(dialect_registry.Options) (dialect.Dialect, error) {
			return dialect.Dialect{}, errors.New("no lite today")
		}),
	}
	result = project.RunWith(t.Context(), failing, "vet")
	requireCode(t, result, 2)
	assert.Contains(t, result.Stderr, "no lite today")

	project.Write("sgl.yaml", "databases:\n  - name: app\n    dialect: sqlite\n    go:\n      driver: recording\n")
	failing = []cli.Option{
		cli.WithEmitter("recording", func(*cli.EmitterRequest) (emit.EmitterPort, error) { return nil, errors.New("no emitter today") }),
	}
	result = project.RunWith(t.Context(), failing, "generate")
	requireCode(t, result, 2)
	assert.Contains(t, result.Stderr, "no emitter today")

	project.Write("sgl.yaml", "databases:\n  - name: app\n    dialect: lite\n")
	result = project.RunWith(t.Context(), options[:1], "migrate", "status", "--dsn", dsn)
	requireCode(t, result, 0)
	result = project.RunWith(t.Context(), []cli.Option{
		cli.WithDialect("lite", func(dialect_registry.Options) (dialect.Dialect, error) {
			return exoticFamilyDialect(), nil
		}),
	}, "migrate", "status", "--dsn", dsn)
	requireCode(t, result, 2)
	assert.Contains(t, result.Stderr, "no database driver")
}

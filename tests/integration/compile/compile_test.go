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
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull"
	"seagull.sh/seagull/internal/testkit"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_clickhouse"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_cockroachdb"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_duckdb"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_mariadb"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_mysql"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_postgres"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_timescaledb"
	"seagull.sh/seagull/sdk/emit"
	"seagull.sh/seagull/sdk/emit/emit_driver_clickhouse"
	"seagull.sh/seagull/sdk/emit/emit_driver_databasesql"
	"seagull.sh/seagull/sdk/emit/emit_driver_pgx"
	"seagull.sh/seagull/tests/integration/internal/harness"
)

const (
	generatedPackage = "generated"
	targetModule     = "seagull.sh/compiletarget"
	specFile         = "testspec.json"
	commandWaitDelay = 10 * time.Second
	cgoOff           = "CGO_ENABLED=0"
	cgoOn            = "CGO_ENABLED=1"
	moduleMode       = "GOFLAGS=-mod=mod"
)

type emitterTarget struct {
	newEmitter func(info dialect.Facts) emit.EmitterPort
	preset     func() seagull.Preset
	name       string
}

type dialectTarget struct {
	newDialect       func() dialect.Dialect
	name             string
	directories      []string
	emitters         []emitterTarget
	buildEnvironment []string
}

type caseSpec struct {
	CustomFunctions []seagull.CustomFunction `json:"customFunctions"`
	TypeOverrides   []seagull.TypeOverride   `json:"typeOverrides"`
	ShouldError     bool                     `json:"shouldError"`
	Skip            bool                     `json:"skip"`
}

type generationTally struct {
	packages       int
	withFindings   int
	withoutOutput  int
	fatalSkipped   int
	casesConsulted int
}

var (
	workspaceReplacedModules = []string{"piko.sh/goastutil", "piko.sh/safeconv", "piko.sh/safedisk"}
	runnerPackage            = regexp.MustCompile("^" + regexp.QuoteMeta(harness.RunnerModule) + "/")
	databaseSQLEmitter       = emitterTarget{
		name: "databasesql",
		newEmitter: func(dialect.Facts) emit.EmitterPort {
			return emit_driver_databasesql.New()
		},
		preset: seagull.DefaultPreset,
	}
	preparedEmitter = emitterTarget{
		name: "databasesql_prepared",
		newEmitter: func(dialect.Facts) emit.EmitterPort {
			return emit_driver_databasesql.New(emit_driver_databasesql.WithPrepared(true))
		},
		preset: seagull.DefaultPreset,
	}
	pgxEmitter = emitterTarget{
		name:       "pgx",
		newEmitter: func(dialect.Facts) emit.EmitterPort { return emit_driver_pgx.New() },
		preset:     seagull.DefaultPreset,
	}
	clickHouseEmitter = emitterTarget{
		name:       "clickhouse",
		newEmitter: func(dialect.Facts) emit.EmitterPort { return emit_driver_clickhouse.New() },
		preset:     seagull.DefaultPreset,
	}
	styledDatabaseSQLEmitter = styled(databaseSQLEmitter)
	styledPgxEmitter         = styled(pgxEmitter)
	styledClickHouseEmitter  = styled(clickHouseEmitter)
)

type caseOutcome struct {
	files    []emit.GeneratedFile
	findings bool
}

func styled(target emitterTarget) emitterTarget {
	target.name += "_styled"
	target.preset = func() seagull.Preset {
		chosen := seagull.DefaultPreset()
		chosen.Interface = seagull.Interface{Enabled: true, Name: "Querier"}
		chosen.EmptySlices = true
		chosen.DocComments = true
		chosen.JSONTags = seagull.JSONTags{Case: seagull.JSONCaseCamel, Omit: false, OmitEmpty: true}
		chosen.StructTags = map[string]string{"db": "{name}"}
		chosen.ReuseModels = true
		return chosen
	}
	return target
}

func targets() []dialectTarget {
	postgresEmitters := []emitterTarget{databaseSQLEmitter, preparedEmitter, pgxEmitter, styledDatabaseSQLEmitter, styledPgxEmitter}
	sqlEmitters := []emitterTarget{databaseSQLEmitter, preparedEmitter, styledDatabaseSQLEmitter}
	return []dialectTarget{
		{name: "postgres", newDialect: func() dialect.Dialect { return dialect_engine_postgres.New() }, emitters: postgresEmitters,
			directories: []string{"internal/dialect/postgres/testdata", "tests/integration/postgres/testdata"}},
		{name: "cockroachdb", newDialect: func() dialect.Dialect { return dialect_engine_cockroachdb.New() }, emitters: postgresEmitters,
			directories: []string{"internal/dialect/cockroachdb/testdata", "tests/integration/cockroachdb/testdata"}},
		{name: "timescaledb", newDialect: func() dialect.Dialect { return dialect_engine_timescaledb.New() }, emitters: postgresEmitters,
			directories: []string{"tests/integration/timescaledb/testdata"}},
		{name: "mysql", newDialect: func() dialect.Dialect { return dialect_engine_mysql.New() }, emitters: sqlEmitters,
			directories: []string{"internal/dialect/mysql/testdata", "tests/integration/mysql/testdata"}},
		{name: "mariadb", newDialect: func() dialect.Dialect { return dialect_engine_mariadb.New() }, emitters: sqlEmitters,
			directories: []string{"internal/dialect/mariadb/testdata", "tests/integration/mariadb/testdata"}},
		{name: "sqlite", newDialect: func() dialect.Dialect { return dialect_engine_sqlite.New() }, emitters: sqlEmitters,
			directories: []string{"internal/dialect/sqlite/testdata", "tests/integration/sqlite/testdata"}},
		{name: "duckdb", newDialect: func() dialect.Dialect { return dialect_engine_duckdb.New() }, emitters: sqlEmitters,
			directories: []string{"internal/dialect/duckdb/testdata", "tests/integration/duckdb/testdata"}, buildEnvironment: []string{cgoOn}},
		{name: "clickhouse", newDialect: func() dialect.Dialect { return dialect_engine_clickhouse.New() },
			emitters:    []emitterTarget{databaseSQLEmitter, clickHouseEmitter, styledDatabaseSQLEmitter, styledClickHouseEmitter},
			directories: []string{"internal/dialect/clickhouse/testdata", "tests/integration/clickhouse/testdata"}},
	}
}

func TestGeneratedCodeCompiles(t *testing.T) {
	repositoryRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	require.NoError(t, err)
	requirements := targetRequirements(t)
	checksums, err := os.ReadFile("go.sum")
	require.NoError(t, err)

	for _, target := range targets() {
		t.Run(target.name, func(t *testing.T) {
			t.Parallel()
			moduleDirectory := t.TempDir()
			tally := generateTarget(t, repositoryRoot, moduleDirectory, target)
			t.Logf("%s: %d packages from %d cases (%d with error findings, %d without output, %d expected to fail)",
				target.name, tally.packages, tally.casesConsulted, tally.withFindings, tally.withoutOutput, tally.fatalSkipped)
			require.Positive(t, tally.packages, "no case of %s generated any code", target.name)

			writeTargetModule(t, moduleDirectory, requirements, checksums)
			runGo(t, moduleDirectory, target.buildEnvironment, "build", "./...")
			runGo(t, moduleDirectory, target.buildEnvironment, "vet", "./...")
		})
	}
}

func generateTarget(t *testing.T, repositoryRoot, moduleDirectory string, target dialectTarget) generationTally {
	t.Helper()
	var tally generationTally
	for _, relative := range target.directories {
		casesDirectory := filepath.Join(repositoryRoot, filepath.FromSlash(relative))
		for _, caseName := range listCases(t, casesDirectory) {
			tally.casesConsulted++
			caseDirectory := filepath.Join(casesDirectory, caseName)
			spec := loadSpec(t, caseDirectory)
			if spec.ShouldError || spec.Skip {
				tally.fatalSkipped++
				continue
			}
			origin := strings.ReplaceAll(strings.TrimSuffix(relative, "/testdata"), "/", "_")
			spec = localiseRunnerPackages(t, spec, caseDirectory, filepath.Join(moduleDirectory, origin, caseName),
				targetModule+"/"+origin+"/"+caseName)
			for _, emitter := range target.emitters {
				outcome := generateCase(t, target, emitter, spec, caseDirectory)
				switch {
				case outcome.findings:
					tally.withFindings++
				case len(outcome.files) == 0:
					tally.withoutOutput++
				default:
					writePackage(t, filepath.Join(moduleDirectory, origin, caseName, emitter.name), outcome.files)
					tally.packages++
				}
			}
		}
	}
	return tally
}

func localiseRunnerPackages(t *testing.T, spec caseSpec, caseDirectory, packageDirectory, importPrefix string) caseSpec {
	t.Helper()
	overrides := slices.Clone(spec.TypeOverrides)
	for index := range overrides {
		runnerModule := runnerPackage.FindString(overrides[index].GoPackage)
		if runnerModule == "" {
			continue
		}
		relative := strings.TrimPrefix(overrides[index].GoPackage, runnerModule)
		sources, err := filepath.Glob(filepath.Join(caseDirectory, filepath.FromSlash(relative), "*.go"))
		require.NoError(t, err)
		require.NotEmptyf(t, sources, "%s names %s, which the case does not hold", caseDirectory, overrides[index].GoPackage)
		for _, source := range sources {
			content, readErr := os.ReadFile(source)
			require.NoError(t, readErr)
			destination := filepath.Join(packageDirectory, filepath.FromSlash(relative), filepath.Base(source))
			require.NoError(t, os.MkdirAll(filepath.Dir(destination), 0o755))
			require.NoError(t, os.WriteFile(destination, content, 0o644))
		}
		overrides[index].GoPackage = importPrefix + "/" + relative
	}
	spec.TypeOverrides = overrides
	return spec
}

func generateCase(t *testing.T, target dialectTarget, emitter emitterTarget, spec caseSpec, caseDirectory string) caseOutcome {
	t.Helper()
	chosen := target.newDialect()
	generator, err := seagull.New(chosen,
		seagull.WithEmitter(emitter.newEmitter(chosen.Info())),
		seagull.WithPreset(emitter.preset()),
		seagull.WithCustomFunctions(spec.CustomFunctions...),
		seagull.WithTypeOverrides(spec.TypeOverrides...),
		seagull.WithLogger(slog.New(slog.DiscardHandler)),
	)
	require.NoError(t, err)

	result, err := generator.Generate(t.Context(), seagull.Input{
		Migrations:   os.DirFS(caseDirectory),
		Queries:      nil,
		Package:      generatedPackage,
		MigrationDir: "migrations",
		QueryDir:     "queries",
	})
	require.NoErrorf(t, err, "generating %s with %s", caseDirectory, emitter.name)
	if result.Err() != nil {
		return caseOutcome{files: nil, findings: true}
	}
	return caseOutcome{files: result.Files, findings: false}
}

func listCases(t *testing.T, directory string) []string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	var cases []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, statErr := os.Stat(filepath.Join(directory, entry.Name(), specFile)); statErr != nil {
			continue
		}
		if _, statErr := os.Stat(filepath.Join(directory, entry.Name(), "queries")); statErr != nil {
			continue
		}
		cases = append(cases, entry.Name())
	}
	slices.Sort(cases)
	return cases
}

func loadSpec(t *testing.T, caseDirectory string) caseSpec {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(caseDirectory, specFile))
	require.NoError(t, err)
	var spec caseSpec
	require.NoErrorf(t, json.Unmarshal(content, &spec), "parsing %s", caseDirectory)
	return spec
}

func writePackage(t *testing.T, directory string, files []emit.GeneratedFile) {
	t.Helper()
	require.NoError(t, os.MkdirAll(directory, 0o755))
	for _, file := range files {
		path := filepath.Join(directory, filepath.FromSlash(file.Name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, file.Content, 0o644))
	}
}

func targetRequirements(t *testing.T) []string {
	t.Helper()
	content, err := os.ReadFile("go.mod")
	require.NoError(t, err)
	required, err := harness.Requirements(content)
	require.NoError(t, err)
	var requirements []string
	for _, requirement := range required {
		if isWorkspaceModule(requirement.Mod.Path) {
			continue
		}
		requirements = append(requirements, requirement.Mod.Path+" "+requirement.Mod.Version)
	}
	require.NotEmpty(t, requirements)
	return requirements
}

func isWorkspaceModule(modulePath string) bool {
	return strings.HasPrefix(modulePath, "seagull.sh/") || slices.Contains(workspaceReplacedModules, modulePath)
}

func writeTargetModule(t *testing.T, directory string, requirements []string, checksums []byte) {
	t.Helper()
	var manifest strings.Builder
	manifest.WriteString("module " + targetModule + "\n\ngo 1.27.0\n\nrequire (\n")
	for _, requirement := range requirements {
		manifest.WriteString("\t" + requirement + "\n")
	}
	manifest.WriteString(")\n")
	require.NoError(t, os.WriteFile(filepath.Join(directory, "go.mod"), []byte(manifest.String()), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "go.sum"), checksums, 0o644))
}

func runGo(t *testing.T, directory string, environment []string, arguments ...string) {
	t.Helper()
	settings := append([]string{moduleMode, cgoOff}, environment...)
	command := testkit.GoCommand(t, directory, settings, arguments...)
	command.WaitDelay = commandWaitDelay
	output, err := command.CombinedOutput()
	require.NoError(t, err, "go %s failed in %s:\n%s", strings.Join(arguments, " "), directory, output)
}

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

package pipeline

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/databasesql"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/observe"
	"seagull.sh/seagull/internal/preset"
	"seagull.sh/seagull/internal/sqlfile"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fakedialect"
	"seagull.sh/seagull/internal/testkit/scaling"
)

type mockCatalogueProvider struct {
	buildCatalogue func(ctx context.Context) (*sqlmodel.Catalogue, []diag.SourceError, error)
}

func (m *mockCatalogueProvider) BuildCatalogue(ctx context.Context) (*sqlmodel.Catalogue, []diag.SourceError, error) {
	if m.buildCatalogue != nil {
		return m.buildCatalogue(ctx)
	}
	return fakedialect.NewCatalogue("public"), nil, nil
}

type testRequest struct {
	migrations         fs.FS
	queries            fs.FS
	packageName        string
	packagePath        string
	migrationDirectory string
	queryDirectory     string
}

func (r testRequest) MigrationSource() (fs.FS, string) {
	return r.migrations, r.migrationDirectory
}

func (r testRequest) QuerySource() (fs.FS, string) {
	return r.queries, r.queryDirectory
}

func (r testRequest) PackageName() string {
	return r.packageName
}

func (r testRequest) PackageImportPath() string {
	return r.packagePath
}

var (
	_ Request          = testRequest{}
	_ CatalogueBuilder = (&mockCatalogueProvider{}).BuildCatalogue
)

type fixedClock struct {
	times []time.Time
}

func (c *fixedClock) Now() time.Time {
	now := c.times[0]
	if len(c.times) > 1 {
		c.times = c.times[1:]
	}
	return now
}

type recordingObserver struct {
	stages []observe.Stage
}

func (o *recordingObserver) Start(ctx context.Context, stage observe.Stage, _ ...slog.Attr) (context.Context, func(error)) {
	o.stages = append(o.stages, stage)
	return ctx, func(error) {}
}

type failingFS struct {
	err error
}

func (f failingFS) Open(string) (fs.File, error) {
	return nil, f.err
}

var emptyQueries = fstest.MapFS{"queries": &fstest.MapFile{Mode: fs.ModeDir}}

func TestNewRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	invalidSettings := preset.DefaultSettings()
	invalidSettings.Style.Header = "not a header"

	tests := []struct {
		name    string
		config  Config
		wantErr error
	}{
		{name: "missing dialect", config: Config{}, wantErr: fault.ErrNoSource},
		{name: "missing emitter", config: Config{Dialect: &fakedialect.Engine{}}, wantErr: fault.ErrInvalidConfig},
		{name: "invalid settings", config: Config{Dialect: &fakedialect.Engine{}, Settings: &invalidSettings}, wantErr: fault.ErrInvalidConfig},
		{name: "negative file limit", config: Config{Dialect: &fakedialect.Engine{}, Limits: Limits{MaxFileBytes: -1}}, wantErr: fault.ErrInvalidConfig},
		{name: "negative query limit", config: Config{Dialect: &fakedialect.Engine{}, Limits: Limits{MaxQueries: -1}}, wantErr: fault.ErrInvalidConfig},
		{name: "negative diagnostic limit", config: Config{Dialect: &fakedialect.Engine{}, Limits: Limits{MaxDiagnostics: -1}}, wantErr: fault.ErrInvalidConfig},
		{name: "file limit above the ceiling", config: Config{Dialect: &fakedialect.Engine{}, Limits: Limits{MaxFileBytes: sqlfile.MaxFileBytesCeiling + 1}}, wantErr: fault.ErrInvalidConfig},
		{name: "unusable custom function", config: Config{Dialect: &fakedialect.Engine{}, CustomFunctions: []sqlmodel.CustomFunctionConfig{{Name: "f"}}}, wantErr: fault.ErrInvalidConfig},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			generator, err := New(testCase.config)
			require.Error(t, err)
			assert.ErrorIs(t, err, testCase.wantErr)
			assert.Nil(t, generator)
		})
	}
}

func TestNewAppliesDefaults(t *testing.T) {
	t.Parallel()

	emitter := databasesql.New()
	generator, err := New(Config{Dialect: &fakedialect.Engine{}, Emitter: emitter})
	require.NoError(t, err)

	assert.Same(t, emitter, generator.emitter)
	assert.Equal(t, observe.SystemClock(), generator.clock)
	assert.Equal(t, sqlfile.DefaultMaxFileBytes, generator.limits.MaxFileBytes)
	assert.Equal(t, sqlfile.DefaultMaxFiles, generator.limits.MaxFiles)
	assert.Equal(t, DefaultMaxQueries, generator.limits.MaxQueries)
	assert.Equal(t, sqlfile.DefaultMaxTotalBytes, generator.limits.MaxTotalBytes)
	assert.Equal(t, DefaultMaxDiagnostics, generator.limits.MaxDiagnostics)
	assert.Equal(t, "// Code generated by sgl. DO NOT EDIT.", generator.Header())
}

func TestGenerate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		source         *mockCatalogueProvider
		emitter        *fakedialect.Emitter
		input          testRequest
		wantErrMessage string
		wantErr        error
		wantFiles      int
	}{
		{
			name:   "empty query directory succeeds with an empty catalogue",
			source: &mockCatalogueProvider{},
			emitter: &fakedialect.Emitter{
				EmitFunc: func(_ context.Context, _ *emit.Request) ([]sqlmodel.GeneratedFile, error) {
					return []sqlmodel.GeneratedFile{{Name: "querier.go"}, {Name: "prepared.go"}, {Name: "query_names.go"}}, nil
				},
			},
			input:     testRequest{packageName: "testdb", queries: emptyQueries, queryDirectory: "queries"},
			wantFiles: 3,
		},
		{
			name: "catalogue build error is propagated",
			source: &mockCatalogueProvider{
				buildCatalogue: func(_ context.Context) (*sqlmodel.Catalogue, []diag.SourceError, error) {
					return nil, nil, errors.New("parse error in migration")
				},
			},
			emitter:        &fakedialect.Emitter{},
			input:          testRequest{packageName: "testdb", queries: emptyQueries, queryDirectory: "queries"},
			wantErrMessage: "building catalogue",
		},
		{
			name: "a schema source without a catalogue is refused",
			source: &mockCatalogueProvider{
				buildCatalogue: func(_ context.Context) (*sqlmodel.Catalogue, []diag.SourceError, error) {
					return nil, nil, nil
				},
			},
			emitter: &fakedialect.Emitter{},
			input:   testRequest{packageName: "testdb", queries: emptyQueries, queryDirectory: "queries"},
			wantErr: fault.ErrNoSource,
		},
		{
			name:   "emitter error is propagated",
			source: &mockCatalogueProvider{},
			emitter: &fakedialect.Emitter{
				EmitFunc: func(_ context.Context, request *emit.Request) ([]sqlmodel.GeneratedFile, error) {
					return nil, fmt.Errorf("emitting models for %s: %w", request.Package, errors.New("emitter failed"))
				},
			},
			input:          testRequest{packageName: "testdb", queries: emptyQueries, queryDirectory: "queries"},
			wantErrMessage: "emitting models",
		},
		{
			name:           "query read error is propagated",
			source:         &mockCatalogueProvider{},
			emitter:        &fakedialect.Emitter{},
			input:          testRequest{packageName: "testdb", queries: failingFS{err: fs.ErrPermission}, queryDirectory: "queries"},
			wantErrMessage: "analysing queries",
			wantErr:        fs.ErrPermission,
		},
		{
			name:           "invalid package name",
			source:         &mockCatalogueProvider{},
			emitter:        &fakedialect.Emitter{},
			input:          testRequest{packageName: "not-a-package", queries: emptyQueries, queryDirectory: "queries"},
			wantErr:        fault.ErrInvalidConfig,
			wantErrMessage: "not-a-package",
		},
		{
			name:           "invalid query directory",
			source:         &mockCatalogueProvider{},
			emitter:        &fakedialect.Emitter{},
			input:          testRequest{packageName: "testdb", queries: emptyQueries, queryDirectory: "/absolute"},
			wantErr:        fault.ErrInvalidConfig,
			wantErrMessage: "query directory",
		},
		{
			name:           "no query file system",
			source:         &mockCatalogueProvider{},
			emitter:        &fakedialect.Emitter{},
			input:          testRequest{packageName: "testdb"},
			wantErr:        fault.ErrNoSource,
			wantErrMessage: "no query files",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			generator, err := New(Config{Dialect: &fakedialect.Engine{}, Emitter: testCase.emitter, BuildCatalogue: testCase.source.BuildCatalogue})
			require.NoError(t, err)

			generation, err := generator.Generate(t.Context(), testCase.input)
			files := generation.Files
			if testCase.wantErrMessage != "" || testCase.wantErr != nil {
				require.Error(t, err)
				assert.Nil(t, generation.Files)
				assert.Nil(t, generation.Queries)
				assert.Nil(t, generation.Diagnostics)
				assert.Contains(t, err.Error(), testCase.wantErrMessage)
				if testCase.wantErr != nil {
					assert.ErrorIs(t, err, testCase.wantErr)
				}
				return
			}

			require.NoError(t, err)
			assert.Len(t, files, testCase.wantFiles)
		})
	}
}

func TestGenerateWithoutMigrationsOrSchemaSource(t *testing.T) {
	t.Parallel()

	generator, err := New(Config{Dialect: &fakedialect.Engine{}, Emitter: &fakedialect.Emitter{}})
	require.NoError(t, err)

	_, err = generator.Generate(t.Context(), testRequest{packageName: "testdb", queries: emptyQueries, queryDirectory: "queries"})
	require.ErrorIs(t, err, fault.ErrNoSource)
}

func TestGenerateStopsBeforeEmissionOnErrorDiagnostics(t *testing.T) {
	t.Parallel()

	emitter := &fakedialect.Emitter{}
	source := &mockCatalogueProvider{
		buildCatalogue: func(_ context.Context) (*sqlmodel.Catalogue, []diag.SourceError, error) {
			return fakedialect.NewCatalogue("public"), []diag.SourceError{{Message: "broken", Severity: diag.SeverityError}}, nil
		},
	}
	generator, err := New(Config{Dialect: &fakedialect.Engine{}, Emitter: emitter, BuildCatalogue: source.BuildCatalogue})
	require.NoError(t, err)

	generation, err := generator.Generate(t.Context(), testRequest{packageName: "testdb", queries: emptyQueries, queryDirectory: "queries"})
	require.NoError(t, err)
	assert.Empty(t, generation.Files)
	assert.Len(t, generation.Diagnostics, 1)
	assert.Empty(t, emitter.Requests)
}

func TestGenerateReadsMigrationsAndQueriesFromOneFileSystem(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"db/migrations/001_init.up.sql": &fstest.MapFile{Data: []byte("CREATE TABLE t (id int);")},
		"db/queries/q.sql":              &fstest.MapFile{Data: []byte("-- sgl.query(Get, one)\nSELECT 1;\n")},
	}
	engine := &fakedialect.Engine{
		ParseStatementsFunc: func(_ context.Context, sql string) ([]ast.ParsedStatement, error) {
			return []ast.ParsedStatement{{Raw: nil, Location: 0, Length: len(sql)}}, nil
		},
		AnalyseQueryFunc: func(context.Context, *sqlmodel.Catalogue, ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
			return &ast.RawQueryAnalysis{}, nil
		},
	}
	emitter := &fakedialect.Emitter{}
	generator, err := New(Config{Dialect: engine, Emitter: emitter})
	require.NoError(t, err)

	generation, err := generator.Generate(t.Context(), testRequest{
		packageName:        "store",
		migrations:         files,
		migrationDirectory: "db/migrations/",
		queryDirectory:     "db/queries",
	})
	require.NoError(t, err)
	assert.Len(t, generation.Queries, 1)
	require.Len(t, emitter.Requests, 1)
	assert.Equal(t, "store", emitter.Requests[0].Package)
	assert.Equal(t, "mock", emitter.Requests[0].Dialect.Name)
}

func TestGenerateEnforcesLimits(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"migrations/001_init.up.sql": &fstest.MapFile{Data: []byte(strings.Repeat("-", 64))},
		"migrations/002_more.up.sql": &fstest.MapFile{Data: []byte("-- empty")},
		"queries/q.sql":              &fstest.MapFile{Data: []byte("-- sgl.query(One, one)\nSELECT 1;\n-- sgl.query(Two, one)\nSELECT 2;\n")},
	}
	engine := &fakedialect.Engine{
		ParseStatementsFunc: func(_ context.Context, sql string) ([]ast.ParsedStatement, error) {
			return []ast.ParsedStatement{{Raw: nil, Location: 0, Length: len(sql)}}, nil
		},
		AnalyseQueryFunc: func(context.Context, *sqlmodel.Catalogue, ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
			return &ast.RawQueryAnalysis{}, nil
		},
	}
	input := testRequest{packageName: "store", migrations: files, migrationDirectory: "migrations", queryDirectory: "queries"}

	tests := []struct {
		name    string
		limits  Limits
		wantErr error
	}{
		{name: "file size", limits: Limits{MaxFileBytes: 16}, wantErr: fault.ErrFileTooLarge},
		{name: "directory entries", limits: Limits{MaxFiles: 1}, wantErr: fault.ErrLimit},
		{name: "query count", limits: Limits{MaxQueries: 1}, wantErr: fault.ErrLimit},
		{name: "total bytes", limits: Limits{MaxTotalBytes: 70}, wantErr: fault.ErrLimit},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			generator, err := New(Config{Dialect: engine, Emitter: &fakedialect.Emitter{}, Limits: testCase.limits})
			require.NoError(t, err)
			_, err = generator.Generate(t.Context(), input)
			require.ErrorIs(t, err, testCase.wantErr)
			assert.ErrorIs(t, err, fault.ErrLimit)
		})
	}
}

func TestVetReportsAMisspeltHeaderWithoutMergingTheQueries(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"migrations/001_init.up.sql": &fstest.MapFile{Data: []byte("CREATE TABLE items (id int);")},
		"queries/items.sql": &fstest.MapFile{Data: []byte(
			"\uFEFF-- Items.\n\n-- sgl.query(Both, many)\nSELECT id FROM items;\n\n-- Purges one item.\n-- sgl.qeury(Purge, exec)\nDELETE FROM items WHERE id = $1;\n")},
	}
	var analysed []string
	engine := &fakedialect.Engine{
		ParseStatementsFunc: func(_ context.Context, sql string) ([]ast.ParsedStatement, error) {
			analysed = append(analysed, sql)
			return []ast.ParsedStatement{{Raw: nil, Location: 0, Length: len(sql)}}, nil
		},
		AnalyseQueryFunc: func(context.Context, *sqlmodel.Catalogue, ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
			return &ast.RawQueryAnalysis{}, nil
		},
	}
	generator, err := New(Config{Dialect: engine, Emitter: &fakedialect.Emitter{}})
	require.NoError(t, err)

	queries, diagnostics, err := generator.Vet(t.Context(), testRequest{
		packageName: "store", migrations: files, migrationDirectory: "migrations", queryDirectory: "queries",
	})
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "Both", queries[0].Name)
	assert.Equal(t, 3, queries[0].Line)
	assert.NotContains(t, queries[0].SQL, "DELETE")
	var unknown []diag.SourceError
	for _, diagnostic := range diagnostics {
		assert.NotContains(t, []string{diag.CodeMissingDirective, diag.CodeMultipleStatements}, diagnostic.Code)
		if diagnostic.Code == diag.CodeUnknownDirective {
			unknown = append(unknown, diagnostic)
		}
	}
	require.Len(t, unknown, 1, "%v", diagnostics)
	assert.Equal(t, diag.SeverityError, unknown[0].Severity)
	assert.Equal(t, 7, unknown[0].Line)
	assert.Equal(t, "items.sql", unknown[0].Filename)
}

func TestVetAndCatalogue(t *testing.T) {
	t.Parallel()

	emitter := &fakedialect.Emitter{}
	observer := &recordingObserver{}
	generator, err := New(Config{Dialect: &fakedialect.Engine{}, Emitter: emitter, BuildCatalogue: (&mockCatalogueProvider{}).BuildCatalogue, Observer: observer})
	require.NoError(t, err)

	queries, diagnostics, err := generator.Vet(t.Context(), testRequest{queries: emptyQueries, queryDirectory: "queries"})
	require.NoError(t, err)
	assert.Empty(t, queries)
	assert.Empty(t, diagnostics)
	assert.Equal(t, []observe.Stage{observe.StageVet, observe.StageCatalogue, observe.StageAnalyse}, observer.stages)
	assert.Empty(t, emitter.Requests, "Vet never emits")

	schema, diagnostics, err := generator.Catalogue(t.Context(), testRequest{})
	require.NoError(t, err)
	assert.Equal(t, "public", schema.DefaultSchema)
	assert.Empty(t, diagnostics)
}

func TestGenerateUsesConfiguredLoggerObserverAndClock(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.Level(-8)}))
	observer := &recordingObserver{}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &fixedClock{times: []time.Time{start, start.Add(3 * time.Second)}}

	generator, err := New(Config{
		Dialect:        &fakedialect.Engine{},
		Emitter:        &fakedialect.Emitter{},
		BuildCatalogue: (&mockCatalogueProvider{}).BuildCatalogue,
		Logger:         logger,
		Observer:       observer,
		Clock:          clock,
	})
	require.NoError(t, err)

	_, err = generator.Generate(t.Context(), testRequest{packageName: "testdb", queries: emptyQueries, queryDirectory: "queries"})
	require.NoError(t, err)
	assert.Equal(t, []observe.Stage{observe.StageGenerate, observe.StageCatalogue, observe.StageAnalyse, observe.StagePlan, observe.StageEmit}, observer.stages)
	assert.Contains(t, logs.String(), "generation complete")
	assert.Contains(t, logs.String(), "elapsed=3s")
}

func TestGenerateHonoursCancellation(t *testing.T) {
	t.Parallel()

	generator, err := New(Config{Dialect: &fakedialect.Engine{}, Emitter: &fakedialect.Emitter{}, BuildCatalogue: (&mockCatalogueProvider{}).BuildCatalogue})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = generator.Generate(ctx, testRequest{packageName: "testdb", queries: emptyQueries, queryDirectory: "queries"})
	require.ErrorIs(t, err, context.Canceled)
}

func malformedHeaders(count int) []byte {
	return []byte(strings.Repeat("-- sgl.query(\n", count))
}

func queryFiles(content []byte) testRequest {
	return testRequest{
		packageName:    "testdb",
		queries:        fstest.MapFS{"queries/q.sql": &fstest.MapFile{Data: content}},
		queryDirectory: "queries",
	}
}

func TestGenerateCountsEveryQueryBlockAgainstTheQueryLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		content    []byte
		maxQueries int
		wantErr    error
	}{
		{name: "malformed headers past the limit", content: malformedHeaders(3), maxQueries: 2, wantErr: fault.ErrLimit},
		{name: "malformed headers and queries past the limit", content: append(malformedHeaders(1), "-- sgl.query(Touch, exec)\nUPDATE t SET a = 1;\n"...), maxQueries: 1, wantErr: fault.ErrLimit},
		{name: "malformed headers within the limit", content: malformedHeaders(2), maxQueries: 2, wantErr: nil},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			generator, err := New(Config{Dialect: queryingEngine(), Emitter: &fakedialect.Emitter{},
				BuildCatalogue: (&mockCatalogueProvider{}).BuildCatalogue, Limits: Limits{MaxQueries: testCase.maxQueries}})
			require.NoError(t, err)

			generation, err := generator.Generate(t.Context(), queryFiles(testCase.content))
			if testCase.wantErr != nil {
				require.ErrorIs(t, err, testCase.wantErr)
				assert.ErrorContains(t, err, "query blocks")
				return
			}
			require.NoError(t, err)
			assert.True(t, fault.HasErrors(generation.Diagnostics))
		})
	}
}

func TestGenerateRefusesAFloodOfMalformedHeadersWithinBudget(t *testing.T) {
	t.Parallel()

	generator, err := New(Config{Dialect: queryingEngine(), Emitter: &fakedialect.Emitter{}, BuildCatalogue: (&mockCatalogueProvider{}).BuildCatalogue})
	require.NoError(t, err)
	flood := queryFiles(malformedHeaders(2 * DefaultMaxQueries))

	scaling.Within(t, 10*time.Second, func(ctx context.Context) {
		_, err := generator.Generate(ctx, flood)
		require.ErrorIs(t, err, fault.ErrLimit)
	})
}

func TestRunsCapTheDiagnosticsTheyReport(t *testing.T) {
	t.Parallel()

	generator, err := New(Config{Dialect: queryingEngine(), Emitter: &fakedialect.Emitter{},
		BuildCatalogue: (&mockCatalogueProvider{}).BuildCatalogue, Limits: Limits{MaxDiagnostics: 5}})
	require.NoError(t, err)
	request := queryFiles(malformedHeaders(20))

	tests := []struct {
		name string
		run  func(t *testing.T) []diag.SourceError
	}{
		{name: "Generate", run: func(t *testing.T) []diag.SourceError {
			generation, runErr := generator.Generate(t.Context(), request)
			require.NoError(t, runErr)
			return generation.Diagnostics
		}},
		{name: "GenerateDetailed", run: func(t *testing.T) []diag.SourceError {
			details, runErr := generator.GenerateDetailed(t.Context(), request)
			require.NoError(t, runErr)
			assert.Equal(t, details.Generation.Diagnostics, details.Diagnostics)
			return details.Generation.Diagnostics
		}},
		{name: "Vet", run: func(t *testing.T) []diag.SourceError {
			_, diagnostics, runErr := generator.Vet(t.Context(), request)
			require.NoError(t, runErr)
			return diagnostics
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			diagnostics := testCase.run(t)
			require.Len(t, diagnostics, 6)
			notice := diagnostics[5]
			assert.Equal(t, diag.CodeDiagnosticLimit, notice.Code)
			assert.Equal(t, diag.SeverityError, notice.Severity)
			assert.Regexp(t, `^and \d+ more diagnostics were left out$`, notice.Message)
		})
	}
}

func TestGenerateAdoptsASchemaSourceThatHoldsNilEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		schemas map[string]*sqlmodel.Schema
	}{
		{name: "a nil schema", schemas: map[string]*sqlmodel.Schema{"main": nil}},
		{name: "a nil table", schemas: map[string]*sqlmodel.Schema{"public": {Name: "public", Tables: map[string]*sqlmodel.Table{"t": nil}}}},
		{name: "a nil view", schemas: map[string]*sqlmodel.Schema{"public": {Name: "public", Views: map[string]*sqlmodel.View{"v": nil}}}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			source := &mockCatalogueProvider{buildCatalogue: func(context.Context) (*sqlmodel.Catalogue, []diag.SourceError, error) {
				catalogue := fakedialect.NewCatalogue("public")
				maps.Copy(catalogue.Schemas, testCase.schemas)
				return catalogue, nil, nil
			}}
			generator, err := New(Config{Dialect: queryingEngine(), Emitter: &fakedialect.Emitter{}, BuildCatalogue: source.BuildCatalogue,
				TypeOverrides: []sqlmodel.TypeOverride{{SQLTypeName: "citext", GoName: "string"}}})
			require.NoError(t, err)

			generation, err := generator.Generate(t.Context(), oneQueryRequest())
			require.NoError(t, err)
			assert.Len(t, generation.Queries, 1)
		})
	}
}

func TestGenerateStopsWhenTheCheckerRefusesOnlyQueriesTheRequestDoesNotHold(t *testing.T) {
	t.Parallel()

	emitter := &refusingEmitter{
		EmitFunc: generatedFiles,
		refuse: func(query *sqlmodel.AnalysedQuery) (emit.Problem, bool) {
			copied := *query
			return emit.Problem{Query: &copied, Code: diag.CodeEmitterUnsupported, Message: "unsupported", Severity: diag.SeverityError}, true
		},
	}
	generator, err := New(Config{Dialect: queryingEngine(), Emitter: emitter, BuildCatalogue: (&mockCatalogueProvider{}).BuildCatalogue, SkipFailingQueries: true})
	require.NoError(t, err)

	scaling.Within(t, 2*time.Second, func(ctx context.Context) {
		generation, err := generator.Generate(ctx, oneQueryRequest())
		require.NoError(t, err)
		assert.Empty(t, generation.Files)
		assert.False(t, generation.Partial)
		assert.Empty(t, emitter.Requests)
		require.Len(t, generation.Diagnostics, 1)
		assert.Equal(t, diag.CodeEmitterUnsupported, generation.Diagnostics[0].Code)
	})
}

func TestGenerateNamesThePackageWhoseEmissionFailed(t *testing.T) {
	t.Parallel()

	failure := errors.New("the template is broken")
	emitter := &fakedialect.Emitter{EmitFunc: func(context.Context, *emit.Request) ([]sqlmodel.GeneratedFile, error) {
		return nil, failure
	}}
	generator, err := New(Config{Dialect: queryingEngine(), Emitter: emitter, BuildCatalogue: (&mockCatalogueProvider{}).BuildCatalogue})
	require.NoError(t, err)

	_, err = generator.Generate(t.Context(), oneQueryRequest())
	require.ErrorIs(t, err, failure)
	assert.EqualError(t, err, "emitting package testdb: the template is broken")
}

func TestGenerateLeavesTheDiagnosticsThatStopItToTheCaller(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		level      slog.Level
		wantLogged bool
	}{
		{name: "nothing is logged at warning level", level: slog.LevelWarn, wantLogged: false},
		{name: "nothing is logged at debug level", level: slog.LevelDebug, wantLogged: false},
		{name: "the stop is logged at the internal level", level: logging.LevelInternal, wantLogged: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var logs bytes.Buffer
			source := &mockCatalogueProvider{buildCatalogue: func(context.Context) (*sqlmodel.Catalogue, []diag.SourceError, error) {
				return fakedialect.NewCatalogue("public"), []diag.SourceError{{Message: "broken", Severity: diag.SeverityError}}, nil
			}}
			generator, err := New(Config{Dialect: queryingEngine(), Emitter: &fakedialect.Emitter{}, BuildCatalogue: source.BuildCatalogue,
				Logger: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: testCase.level}))})
			require.NoError(t, err)

			generation, err := generator.Generate(t.Context(), oneQueryRequest())
			require.NoError(t, err)
			assert.True(t, fault.HasErrors(generation.Diagnostics))
			assert.Equal(t, testCase.wantLogged, strings.Contains(logs.String(), "generation stopped by error diagnostics"), logs.String())
		})
	}
}

func TestGenerateAndVetRefuseTheBlankIdentifierAsThePackageName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		packageName string
	}{
		{name: "the blank identifier", packageName: "_"},
		{name: "a name that is no identifier", packageName: "not-a-package"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			generator, err := New(Config{Dialect: queryingEngine(), Emitter: &panickingEmitter{panicsIn: panicsInCheck},
				BuildCatalogue: (&mockCatalogueProvider{}).BuildCatalogue})
			require.NoError(t, err)
			request := oneQueryRequest()
			request.packageName = testCase.packageName

			_, err = generator.Generate(t.Context(), request)
			require.ErrorIs(t, err, fault.ErrInvalidConfig)
			assert.ErrorContains(t, err, testCase.packageName)

			queries, diagnostics, err := generator.Vet(t.Context(), request)
			require.NoError(t, err)
			assert.Len(t, queries, 1)
			assert.Empty(t, diagnostics, "the emitter's CheckerPort is not asked about a package that cannot be named")
		})
	}
}

func TestASchemaSourcesViewsThatCannotBeTypedAreReported(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		analyse  func(context.Context, *sqlmodel.Catalogue, ast.ParsedStatement) (*ast.RawQueryAnalysis, error)
		wantCode string
	}{
		{
			name: "an analysis holding a nil node",
			analyse: func(context.Context, *sqlmodel.Catalogue, ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
				return &ast.RawQueryAnalysis{OutputColumns: []ast.RawOutputColumn{{Name: "v", Expression: (*ast.CastExpression)(nil)}}}, nil
			},
			wantCode: diag.CodeInternalNilGuard,
		},
		{
			name: "an analysis that panics",
			analyse: func(context.Context, *sqlmodel.Catalogue, ast.ParsedStatement) (*ast.RawQueryAnalysis, error) {
				panic("the view broke")
			},
			wantCode: diag.CodeRecoveredPanic,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			engine := queryingEngine()
			engine.AnalyseQueryFunc = testCase.analyse
			source := &mockCatalogueProvider{buildCatalogue: func(context.Context) (*sqlmodel.Catalogue, []diag.SourceError, error) {
				catalogue := fakedialect.NewCatalogue("public")
				catalogue.Schemas["public"].Views = map[string]*sqlmodel.View{"totals": {Name: "totals", Schema: "public",
					Definition: "SELECT v FROM t", Columns: []sqlmodel.Column{{Name: "v"}}}}
				return catalogue, nil, nil
			}}
			generator, err := New(Config{Dialect: engine, Emitter: &fakedialect.Emitter{}, BuildCatalogue: source.BuildCatalogue})
			require.NoError(t, err)

			_, diagnostics, err := generator.Catalogue(t.Context(), oneQueryRequest())

			require.NoError(t, err)
			require.Len(t, diagnostics, 1)
			assert.Equal(t, testCase.wantCode, diagnostics[0].Code)
			assert.Contains(t, diagnostics[0].Message, "totals")
		})
	}
}

func TestAQueryFileReadOnlyInPartIsReported(t *testing.T) {
	t.Parallel()

	content := "-- sgl.query(Touch, exec)\nUPDATE t SET a = 1;\n" + strings.Repeat("SELECT 1; /* open\n", 65) +
		"-- sgl.query(Later, exec)\nUPDATE t SET a = 2;\n"
	request := testRequest{
		packageName:    "testdb",
		queries:        fstest.MapFS{"queries/q.sql": &fstest.MapFile{Data: []byte(content)}},
		queryDirectory: "queries",
	}
	generator, err := New(Config{Dialect: queryingEngine(), Emitter: &fakedialect.Emitter{},
		BuildCatalogue: (&mockCatalogueProvider{}).BuildCatalogue})
	require.NoError(t, err)

	_, diagnostics, err := generator.Vet(t.Context(), request)

	require.NoError(t, err)
	var truncated []diag.SourceError
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == diag.CodeDirectiveLineLimit {
			truncated = append(truncated, diagnostic)
		}
	}
	require.Len(t, truncated, 1)
	assert.Equal(t, "q.sql", truncated[0].Filename)
	assert.Equal(t, 67, truncated[0].Line)
	assert.Equal(t, diag.SeverityError, truncated[0].Severity)
	assert.Contains(t, truncated[0].Message, "the query file holds more than 64")
}

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

package app

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/logging"
	"seagull.sh/seagull/internal/panicguard"
	"seagull.sh/seagull/internal/pipeline"
	"seagull.sh/seagull/internal/sqlmodel"
)

// Input names the SQL one run reads.
type Input struct {
	// Migrations holds the migration files, such as an embed.FS or os.DirFS. It may be nil
	// when the generator has a SchemaSourcePort.
	Migrations fs.FS

	// Queries holds the query files. It may be the same file system as Migrations; nil reads
	// the queries from Migrations.
	Queries fs.FS

	// Package is the Go package name of the generated files. Only Generate needs it.
	Package string

	// PackagePath is the generated package's import path, used to leave its own override
	// types unqualified. Only Generate uses it; empty keeps overrides as named.
	PackagePath string

	// MigrationDir is the slash-separated directory of the migration files within
	// Migrations; empty means its root.
	MigrationDir string

	// QueryDir is the slash-separated directory of the query files within Queries; empty
	// means its root.
	QueryDir string
}

// Result is the outcome of Generate.
type Result struct {
	// Header is the first line of every generated file. WriteFiles uses it to recognise
	// files a previous run generated.
	Header string

	// Methods maps query names to generated method names, including any renaming needed to
	// avoid conflicts. It is nil when Files is empty.
	Methods map[string]string

	// Files holds the generated files. It is empty when any diagnostic has error severity,
	// except under WithSkipFailingQueries, where it holds every query's but those with an
	// error of their own, unless an error that is no one query's own stopped generation.
	Files []sqlmodel.GeneratedFile

	// Diagnostics holds every diagnostic of the run, in source order.
	//
	// Diagnostics with the same code, severity, file, span and message are reported once.
	// Beyond the WithMaxDiagnostics cap, a closing Q066 diagnostic summarises the rest and
	// carries their highest severity.
	Diagnostics []diag.SourceError

	// QueryCount is the number of queries analysed.
	QueryCount int

	// Partial reports a result generated under WithSkipFailingQueries that leaves out at
	// least one query with an error diagnostic of its own: Files holds the rest of the
	// package, which WriteFiles writes, and Err still reports the errors.
	Partial bool
}

// Err returns a *DiagnosticsError when any diagnostic has error severity, and nil
// otherwise.
//
// Returns error which is a *DiagnosticsError wrapping ErrDiagnostics, or nil.
func (r *Result) Err() error {
	return diagnosticsErr(r.Diagnostics)
}

// DetailedResult is the outcome of GenerateDetailed: generated files, the first checker
// report and the schema catalogue from one analysis. Each result owns its mutable data
// independently of other calls and the schema source.
type DetailedResult struct {
	// Result holds the same outcome as Generate, including partial generation.
	Result *Result

	// Report holds the same first-pass analysis and checker diagnostics as Vet.
	Report *Report

	// Catalogue holds the schema before emission.
	Catalogue *sqlmodel.Catalogue

	// CatalogueDiagnostics holds diagnostics from building that schema alone.
	CatalogueDiagnostics []diag.SourceError
}

// Report is the outcome of Vet: the analysed queries and every diagnostic, without
// generated code.
type Report struct {
	// Diagnostics holds every diagnostic of the run, in source order.
	//
	// Diagnostics with the same code, severity, file, span and message are reported once.
	// Beyond the WithMaxDiagnostics cap, a closing Q066 diagnostic summarises the rest and
	// carries their highest severity.
	Diagnostics []diag.SourceError

	// Queries holds the analysed queries: their parameters, output columns, nullability and
	// directives.
	Queries []*sqlmodel.AnalysedQuery
}

// Err returns a *DiagnosticsError when any diagnostic has error severity, and nil
// otherwise.
//
// Returns error which is a *DiagnosticsError wrapping ErrDiagnostics, or nil.
func (r *Report) Err() error {
	return diagnosticsErr(r.Diagnostics)
}

// TypeOverride maps a SQL type, or one column, to a Go type, ahead of the preset's type
// slots and the emitter's and built-in defaults.
//
// Set SQLTypeName to override every column of that type, or Column ("table.column" or
// "schema.table.column") to override one column as a migration go_type directive does; a
// go_type directive on that column still wins.
type TypeOverride = sqlmodel.TypeOverride

// CustomFunction declares a SQL function the dialect does not know, such as one from a
// loadable extension, so queries and views that call it can be analysed. New rejects a
// declaration it cannot use with ErrInvalidConfig.
type CustomFunction = sqlmodel.CustomFunctionConfig

// Generator turns SQL into Go for one dialect. It holds no state between runs and is safe
// for concurrent use.
type Generator struct {
	// pipeline runs each stage of a generation.
	pipeline *pipeline.Pipeline
}

// New creates a generator for a dialect.
//
// Takes engine (dialect.DialectPort) which parses and analyses the SQL, such as
// dialect_engine_postgres.New() or a dialect from dialect_registry.New.
// Takes options (...Option) which configure the emitter, preset, overrides, schema
// source, logging, observation and limits.
//
// Returns *Generator which is ready to run.
// Returns error which wraps ErrNoSource when engine is nil, ErrInvalidConfig when an
// option is nil, or an option, a custom function, a type override or the preset cannot be
// used or the dialect's Info lacks a required field, or ErrPanic when the dialect's Info
// or the emitter's naming reservation panicked.
func New(engine dialect.DialectPort, options ...Option) (*Generator, error) {
	settings := newSettings()
	for index, option := range options {
		if option == nil {
			settings.reject("New: option %d is nil", index)
			continue
		}
		option(settings)
	}
	config, err := settings.pipelineConfig(engine)
	if err != nil {
		return nil, err
	}
	info, err := dialectInfo(logging.ContextWithLogger(context.Background(), settings.logger), engine)
	if err != nil {
		return nil, err
	}
	if err := validateDialectInfo(&info); err != nil {
		return nil, err
	}
	built, err := pipeline.New(config)
	if err != nil {
		return nil, err
	}
	return &Generator{pipeline: built}, nil
}

// Generate analyses queries against the migration schema and emits Go source when
// diagnostics permit it. WithSkipFailingQueries allows generation to continue without
// queries that fail analysis.
//
// Takes input (Input) which names the migrations, the queries and the package.
//
// Returns *Result which holds the generated files and every diagnostic; Result.Err
// reports error-severity diagnostics.
// Returns error when there is a fatal problem: invalid input, unreadable files, a limit
// exceeded, cancellation, an emitter failure, a panic recovered from a dialect or host
// call (ErrPanic), or a generator not made by New (ErrInvalidConfig).
func (g *Generator) Generate(ctx context.Context, input Input) (*Result, error) {
	if err := g.ready(); err != nil {
		return nil, err
	}
	generation, err := g.pipeline.Generate(ctx, pipelineRequest(input))
	if err != nil {
		return nil, err
	}
	return &Result{
		Header:      g.pipeline.Header(),
		Files:       generation.Files,
		Diagnostics: generation.Diagnostics,
		Methods:     generation.Methods,
		QueryCount:  len(generation.Queries),
		Partial:     generation.Partial,
	}, nil
}

// GenerateDetailed generates code and returns the report and catalogue from the same run,
// avoiding separate Generate, Vet and Catalogue passes. The report reflects the first
// checker pass even when partial generation retries with fewer queries.
//
// Takes input (Input) which names the migrations, queries and generated package.
//
// Returns *DetailedResult which owns the generation, report and schema data.
// Returns error when a fatal problem stops generation, as Generate does.
func (g *Generator) GenerateDetailed(ctx context.Context, input Input) (*DetailedResult, error) {
	if err := g.ready(); err != nil {
		return nil, err
	}
	details, err := g.pipeline.GenerateDetailed(ctx, pipelineRequest(input))
	if err != nil {
		return nil, err
	}
	generation := details.Generation
	return &DetailedResult{
		Result: &Result{Header: g.pipeline.Header(), Files: generation.Files, Diagnostics: generation.Diagnostics,
			Methods: generation.Methods, QueryCount: len(generation.Queries), Partial: generation.Partial},
		Report:    &Report{Queries: details.Queries, Diagnostics: details.Diagnostics},
		Catalogue: details.Catalogue, CatalogueDiagnostics: details.CatalogueDiagnostics,
	}, nil
}

// Vet analyses every query as Generate does but emits no code, for checks in CI and
// editors.
//
// Takes input (Input) which names the migrations and queries. A valid Package enables
// emitter and generated-name checks too.
//
// Returns *Report which holds the analysed queries and every diagnostic.
// Returns error when there is a fatal problem, as Generate does.
func (g *Generator) Vet(ctx context.Context, input Input) (*Report, error) {
	if err := g.ready(); err != nil {
		return nil, err
	}
	queries, diagnostics, err := g.pipeline.Vet(ctx, pipelineRequest(input))
	if err != nil {
		return nil, err
	}
	return &Report{Diagnostics: diagnostics, Queries: queries}, nil
}

// Catalogue builds the schema catalogue alone: from the schema source when the generator
// has one, and by replaying the migrations otherwise. It holds the custom functions and
// column type overrides, as the analysis sees it.
//
// Takes input (Input) which names the migrations; the queries and package are ignored.
//
// Returns *sqlmodel.Catalogue which holds the schema.
// Returns []diag.SourceError which holds the diagnostics found while building it.
// Returns error when there is a fatal problem.
func (g *Generator) Catalogue(ctx context.Context, input Input) (*sqlmodel.Catalogue, []diag.SourceError, error) {
	if err := g.ready(); err != nil {
		return nil, nil, err
	}
	return g.pipeline.Catalogue(ctx, pipelineRequest(input))
}

// ready reports a generator that New did not make, such as a nil or zero Generator, which
// has no pipeline to run.
//
// Returns error which wraps ErrInvalidConfig when the generator cannot run, or nil.
func (g *Generator) ready() error {
	if g == nil || g.pipeline == nil {
		return fmt.Errorf("the generator was not made by seagull.New: %w", fault.ErrInvalidConfig)
	}
	return nil
}

// pipelineRequest adapts an Input to the request the internal pipeline reads, so the
// pipeline declares no copy of the public type.
type pipelineRequest Input

// MigrationSource returns the migration files and their directory.
//
// Returns fs.FS which holds the migration files, or nil.
// Returns string which is the directory within them.
func (r pipelineRequest) MigrationSource() (fs.FS, string) {
	return r.Migrations, r.MigrationDir
}

// QuerySource returns the query files and their directory.
//
// Returns fs.FS which holds the query files, or nil to read them from the migrations.
// Returns string which is the directory within them.
func (r pipelineRequest) QuerySource() (fs.FS, string) {
	return r.Queries, r.QueryDir
}

// PackageName returns the Go package name of the generated files.
//
// Returns string which is the package name.
func (r pipelineRequest) PackageName() string {
	return r.Package
}

// PackageImportPath returns the import path of the generated package.
//
// Returns string which is the import path, or empty when it is not known.
func (r pipelineRequest) PackageImportPath() string {
	return r.PackagePath
}

// ContextWithLogger returns a copy of ctx that carries logger, so a generation run with
// that context logs through it. WithLogger sets a logger for every run instead.
//
// Takes logger (*slog.Logger) which receives the log records; nil returns ctx unchanged.
//
// Returns context.Context which carries the logger.
func ContextWithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return logging.ContextWithLogger(ctx, logger)
}

// requireDialect reports a missing dialect.
//
// Takes engine (dialect.DialectPort) which is the dialect passed to New.
//
// Returns error which wraps ErrNoSource when engine is nil.
func requireDialect(engine dialect.DialectPort) error {
	if engine == nil {
		return fmt.Errorf("seagull.New needs a dialect: %w", fault.ErrNoSource)
	}
	return nil
}

// dialectInfo asks a dialect for its Info, turning a panic into an error.
//
// Takes engine (dialect.DialectPort) which is the dialect.
//
// Returns dialect.Info which describes the dialect.
// Returns error which wraps ErrPanic when the dialect panicked.
func dialectInfo(ctx context.Context, engine dialect.DialectPort) (dialect.Info, error) {
	return panicguard.RunValue(ctx, "dialect Info", func() (dialect.Info, error) {
		return engine.Info(), nil
	})
}

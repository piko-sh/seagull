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

package seagull

import (
	"context"
	"log/slog"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqlmodel"
)

// Generator turns SQL into Go for one dialect. New makes one; it holds no state between
// runs and is safe for concurrent use.
type Generator struct {
	// generator runs each generation; nil for a Generator that New did not make.
	generator *app.Generator
}

// New creates a generator for a dialect.
//
// The dialect comes from sdk/dialect: each built-in engine is a dialect_engine_*
// sub-package (postgres, cockroachdb, timescaledb, mysql, mariadb, sqlite, duckdb or
// clickhouse) whose New returns one, and dialect_registry.New builds one by name from
// configuration.
//
// Takes engine (dialect.Dialect) which parses and analyses the SQL, such as
// dialect_engine_postgres.New().
// Takes options (...Option) which configure the emitter, preset, overrides, schema
// source, logging, observation and limits.
//
// Returns *Generator which is ready to run.
// Returns error which wraps ErrNoSource for the zero Dialect, ErrInvalidConfig when an
// option is nil, or an option, a custom function, a type override or the preset cannot be
// used or the dialect does not describe itself fully, or ErrPanic when the dialect
// panicked describing itself or the emitter's naming reservation panicked.
func New(engine dialect.Dialect, options ...Option) (*Generator, error) {
	generator, err := app.New(dialect.Unwrap(engine), options...)
	if err != nil {
		return nil, err
	}
	return &Generator{generator: generator}, nil
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
	return g.implementation().Generate(ctx, input)
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
	return g.implementation().GenerateDetailed(ctx, input)
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
	return g.implementation().Vet(ctx, input)
}

// Catalogue builds the schema catalogue alone: from the schema source when the generator
// has one, and by replaying the migrations otherwise. It holds the custom functions and
// column type overrides, as the analysis sees it.
//
// Takes input (Input) which names the migrations; the queries and package are ignored.
//
// Returns *Catalogue which holds the schema: the Catalogue type of sdk/dialect, which
// sdk/schema re-exports.
// Returns []SourceError which holds the diagnostics found while building it.
// Returns error when there is a fatal problem.
func (g *Generator) Catalogue(ctx context.Context, input Input) (*sqlmodel.Catalogue, []SourceError, error) {
	return g.implementation().Catalogue(ctx, input)
}

// implementation returns the generator behind the facade. A nil or zero Generator has
// none, and the nil it gets reports ErrInvalidConfig from every method, as a generator
// that New did not make.
//
// Returns *app.Generator which runs each generation, or nil.
func (g *Generator) implementation() *app.Generator {
	if g == nil {
		return nil
	}
	return g.generator
}

// ContextWithLogger returns a copy of ctx that carries logger, so a generation run with
// that context logs through it. WithLogger sets a logger for every run instead.
//
// Takes logger (*slog.Logger) which receives the log records; nil returns ctx unchanged.
//
// Returns context.Context which carries the logger.
func ContextWithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return app.ContextWithLogger(ctx, logger)
}

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
	"log/slog"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/observe"
)

// WithEmitter sets the emitter that turns the analysed queries into Go.
//
// The built-in emitters are the providers under sdk/emit: emit_driver_databasesql, the
// default, for database/sql with placeholders from the dialect's metadata;
// emit_driver_pgx for pgx/v5's native interfaces; and emit_driver_clickhouse for the
// native clickhouse-go/v2 driver. Each one's New returns the emitter to pass here. An
// emitter of your own implements sdk/emit's EmitterPort, and emit_kit and emit_testkit
// help write and check one.
//
// Takes emitter (emit.EmitterPort) which generates the Go source; nil is an error.
//
// Returns Option which sets the emitter.
func WithEmitter(emitter emit.EmitterPort) Option {
	return app.WithEmitter(emitter)
}

// WithPreset sets the vocabulary of directives, diagnostics and generated code.
//
// The default is DefaultPreset(). WithDirectivePrefixes and WithForeignDirectivePrefixes
// apply on top of it whatever the order of the options.
//
// Takes chosen (Preset) which is the preset to use; New validates it.
//
// Returns Option which sets the preset.
func WithPreset(chosen Preset) Option {
	return app.WithPreset(chosen)
}

// WithDirectivePrefixes sets the directive prefixes recognised in SQL comments: the
// canonical prefix diagnostics render and the aliases accepted as synonyms, replacing the
// preset's.
//
// Takes canonical (string) which is the canonical prefix, such as sgl.
// Takes aliases (...string) which are further prefixes accepted as synonyms.
//
// Returns Option which sets the prefixes; New rejects an invalid or repeated prefix with
// ErrInvalidPrefix.
func WithDirectivePrefixes(canonical string, aliases ...string) Option {
	return app.WithDirectivePrefixes(canonical, aliases...)
}

// WithForeignDirectivePrefixes sets the prefixes that belong to another tool, replacing
// the preset's. A directive written with one is reported with a Q051 warning rather than
// parsed.
//
// Takes prefixes (...string) which are the foreign prefixes; none clears them.
//
// Returns Option which sets the foreign prefixes.
func WithForeignDirectivePrefixes(prefixes ...string) Option {
	return app.WithForeignDirectivePrefixes(prefixes...)
}

// WithTypeOverrides adds SQL-to-Go type overrides, for a SQL type or for one column.
//
// They rank below go_type column directives and above the preset's type slots and the
// emitter's and built-in defaults. A column override that names no column of the schema
// is reported as a Q037 error diagnostic.
//
// Takes overrides (...TypeOverride) which are appended to any set before. New copies
// them, their struct tags and element choice included, so a later change to them does not
// reach the generator.
//
// Returns Option which adds the overrides; New rejects with ErrInvalidConfig an override
// that names both or neither of a SQL type and a column, a malformed column reference, or
// no Go type name.
func WithTypeOverrides(overrides ...TypeOverride) Option {
	return app.WithTypeOverrides(overrides...)
}

// WithPackageNames specifies package names for Go types whose import paths imply a
// different name. Generated imports use the supplied names.
//
// Takes names (map[string]string) which maps import paths to the names their packages
// declare; it is copied, and later calls add to it.
//
// Returns Option which adds the names; New rejects with ErrInvalidConfig an entry whose
// path is not a valid import path or whose name is not a Go identifier.
func WithPackageNames(names map[string]string) Option {
	return app.WithPackageNames(names)
}

// WithCustomFunctions declares SQL functions the dialect does not know, such as those of
// a loadable extension.
//
// Takes functions (...CustomFunction) which are appended to any declared before. New
// copies them, their arguments included, so a later change to them does not reach the
// generator.
//
// Returns Option which adds the functions.
func WithCustomFunctions(functions ...CustomFunction) Option {
	return app.WithCustomFunctions(functions...)
}

// WithSchemaSource builds the catalogue from an external schema source, such as a live
// database, instead of replaying the migration files.
//
// The schema sources are the providers under sdk/schema: schema_source_postgres reads a
// PostgreSQL-compatible database and schema_source_sqlite a SQLite one. A source of your
// own implements SchemaSourcePort. The generator works on a copy, excludes runtime tables
// and resolves view definitions before query analysis.
//
// Takes source (SchemaSourcePort) which builds the catalogue; nil is an error.
//
// Returns Option which sets the schema source.
func WithSchemaSource(source SchemaSourcePort) Option {
	return app.WithSchemaSource(source)
}

// WithRuntimeTables excludes migration and seed runtime tables from a schema source's
// catalogue.
//
// Names match without regard to schema or case. The default names the tables the
// migration runtime of sdk/migrate keeps under its default namespace; a Backend built
// with another namespace or history table, or with CustomBackend, should pass the table
// names its Backend.Names reports. This option has no effect without WithSchemaSource.
//
// Takes names (...string) which replace the default; none keeps every table.
//
// Returns Option which sets the names; New rejects an empty name with ErrInvalidConfig.
func WithRuntimeTables(names ...string) Option {
	return app.WithRuntimeTables(names...)
}

// WithLogger sets the logger every run logs through. Without it a run logs through the
// logger carried by its context (see ContextWithLogger), or slog.Default().
//
// Takes logger (*slog.Logger) which receives the log records; nil keeps the default.
//
// Returns Option which sets the logger.
func WithLogger(logger *slog.Logger) Option {
	return app.WithLogger(logger)
}

// WithObserver sets the observer that receives a span for each stage of every run, for
// tracing and metrics. Without it a run reports to the observer carried by its context,
// which ContextWithObserver in sdk/observe attaches, or to none.
//
// The observer implements ObserverPort from sdk/observe, which also lists the stages.
//
// Takes observer (observe.ObserverPort) which receives the stage spans; nil keeps the
// default.
//
// Returns Option which sets the observer.
func WithObserver(observer observe.ObserverPort) Option {
	return app.WithObserver(observer)
}

// WithClock sets the clock that times each run for the trace log. The default is the wall
// clock, SystemClock in sdk/observe; a test passes a fixed Clock of its own.
//
// Takes clock (observe.Clock) which supplies the time; nil keeps the default.
//
// Returns Option which sets the clock.
func WithClock(clock observe.Clock) Option {
	return app.WithClock(clock)
}

// WithMaxFileBytes caps the size of each migration and query file.
//
// A larger file stops the run with an error wrapping ErrFileTooLarge. The default is 4
// MiB, and the cap may be at most 1 TiB.
//
// Takes limit (int64) which is the maximum size in bytes; it must be positive and at most
// 1 TiB.
//
// Returns Option which sets the cap.
func WithMaxFileBytes(limit int64) Option {
	return app.WithMaxFileBytes(limit)
}

// WithMaxTotalBytes caps the bytes one run reads across every migration and query file,
// however the files are split.
//
// Reading past the cap stops the run with an error wrapping ErrLimit. Files are read and
// processed one at a time, so the cap bounds the input, not the memory held at once. The
// default is 64 MiB.
//
// Takes limit (int64) which is the maximum total size in bytes; it must be positive.
//
// Returns Option which sets the cap.
func WithMaxTotalBytes(limit int64) Option {
	return app.WithMaxTotalBytes(limit)
}

// WithMaxFiles caps the entries in the migration and query directories.
//
// A larger directory stops the run with an error wrapping ErrLimit. The default is
// 10,000.
//
// Takes limit (int) which is the maximum number of entries; it must be positive.
//
// Returns Option which sets the cap.
func WithMaxFiles(limit int) Option {
	return app.WithMaxFiles(limit)
}

// WithMaxQueries caps the queries one run analyses, counting every query block of the
// query files, one whose header is malformed included.
//
// More stop the run with an error wrapping ErrLimit. The default is 100,000.
//
// Takes limit (int) which is the maximum number of query blocks; it must be positive.
//
// Returns Option which sets the cap.
func WithMaxQueries(limit int) Option {
	return app.WithMaxQueries(limit)
}

// WithMaxDiagnostics caps the diagnostics one run reports.
//
// The diagnostics past the cap are left out and summarised in one Q066 diagnostic at the
// highest severity among them, so a run whose errors were left out still fails. The
// default is 10,000.
//
// Takes limit (int) which is the maximum number of diagnostics; it must be positive.
//
// Returns Option which sets the cap.
func WithMaxDiagnostics(limit int) Option {
	return app.WithMaxDiagnostics(limit)
}

// WithSkipFailingQueries enables generation of queries without their own error
// diagnostics.
//
// Result.Partial identifies partial output, while Result.Err retains the errors.
// Migration failures and package-wide errors still stop generation.
//
// Returns Option which sets the behaviour.
func WithSkipFailingQueries() Option {
	return app.WithSkipFailingQueries()
}

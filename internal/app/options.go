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
	"cmp"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/directive"
	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/databasesql"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/observe"
	"seagull.sh/seagull/internal/pipeline"
	"seagull.sh/seagull/internal/preset"
	"seagull.sh/seagull/internal/schema"
	"seagull.sh/seagull/internal/sqlfile"
	"seagull.sh/seagull/internal/sqlmodel"
)

var (
	// defaultRuntimeTables holds the tables the migration runtime keeps under its default
	// namespace, which WithRuntimeTables replaces.
	defaultRuntimeTables = []string{"sgl_migrations", "sgl_seeds", "sgl_migration_lock", "sgl_seed_lock"}
)

// Option configures a Generator.
type Option func(*settings)

// settings collects the options passed to New.
type settings struct {
	// emitter generates the Go source; nil selects the database/sql emitter.
	emitter emit.EmitterPort

	// schemaSource, when set, builds the catalogue in place of the migrations.
	schemaSource schema.SchemaSourcePort

	// clock times each run.
	clock observe.Clock

	// logger receives the generator's log records.
	logger *slog.Logger

	// observer receives the generator's stage spans.
	observer observe.ObserverPort

	// directives, when set, replaces the preset's canonical prefix and aliases.
	directives *Directives

	// foreign, when set, replaces the preset's foreign prefixes.
	foreign []string

	// typeOverrides maps SQL types to Go types.
	typeOverrides []TypeOverride

	// packageNames maps import paths to the names their packages declare.
	packageNames map[string]string

	// customFunctions declares functions the dialect does not know.
	customFunctions []CustomFunction

	// runtimeTables names the tables a schema source's catalogue leaves out.
	runtimeTables []string

	// problems collects options that cannot be applied.
	problems []error

	// preset is the vocabulary of directives, diagnostics and generated code.
	preset Preset

	// limits bounds the input of each run.
	limits pipeline.Limits

	// foreignSet records that WithForeignDirectivePrefixes was called.
	foreignSet bool

	// skipFailingQueries leaves out each query with an error diagnostic of its own and
	// generates the rest.
	skipFailingQueries bool
}

// newSettings returns the settings New starts from: the default preset and no options.
//
// Returns *settings which holds the defaults.
func newSettings() *settings {
	return &settings{
		emitter:            nil,
		schemaSource:       nil,
		clock:              nil,
		logger:             nil,
		observer:           nil,
		directives:         nil,
		foreign:            nil,
		typeOverrides:      nil,
		packageNames:       nil,
		customFunctions:    nil,
		runtimeTables:      defaultRuntimeTables,
		problems:           nil,
		preset:             DefaultPreset(),
		limits:             pipeline.Limits{MaxFileBytes: 0, MaxFiles: 0, MaxQueries: 0, MaxTotalBytes: 0, MaxDiagnostics: 0},
		foreignSet:         false,
		skipFailingQueries: false,
	}
}

// pipelineConfig validates the collected options and converts them to the pipeline's
// configuration. The type overrides and custom functions are copied deeply, so the
// generator owns its copies and a caller's later change reaches neither its validation
// nor a run.
//
// Takes engine (dialect.DialectPort) which is the dialect passed to New.
//
// Returns pipeline.Config which configures the pipeline.
// Returns error which wraps ErrNoSource or ErrInvalidConfig when an option cannot be
// applied.
func (s *settings) pipelineConfig(engine dialect.DialectPort) (pipeline.Config, error) {
	problems := s.problems
	if err := requireDialect(engine); err != nil {
		problems = append([]error{err}, problems...)
	}
	if err := errors.Join(problems...); err != nil {
		return pipeline.Config{}, err
	}

	chosen := s.preset
	if s.directives != nil {
		chosen.Directives.Canonical = s.directives.Canonical
		chosen.Directives.Aliases = s.directives.Aliases
	}
	if s.foreignSet {
		chosen.Directives.Foreign = s.foreign
	}

	var buildCatalogue pipeline.CatalogueBuilder
	if s.schemaSource != nil {
		buildCatalogue = s.schemaSource.BuildCatalogue
	}
	presetSettings := chosen.settings()
	presetSettings.Style.PackageNames = maps.Clone(s.packageNames)

	return pipeline.Config{
		Dialect:            engine,
		Emitter:            s.chosenEmitter(),
		BuildCatalogue:     buildCatalogue,
		RuntimeTables:      slices.Clone(s.runtimeTables),
		Settings:           &presetSettings,
		Clock:              cmp.Or(s.clock, observe.SystemClock()),
		Logger:             s.logger,
		Observer:           s.observer,
		TypeOverrides:      cloneTypeOverrides(s.typeOverrides),
		CustomFunctions:    cloneCustomFunctions(s.customFunctions),
		Limits:             s.limits,
		SkipFailingQueries: s.skipFailingQueries,
	}, nil
}

// chosenEmitter returns the emitter a generator runs: the one WithEmitter set, or the
// database/sql emitter, whose placeholders come from the dialect's metadata.
//
// Returns emit.EmitterPort which generates the Go source.
func (s *settings) chosenEmitter() emit.EmitterPort {
	if s.emitter != nil {
		return s.emitter
	}
	return databasesql.New()
}

// reject records an option that cannot be applied, so New returns it.
//
// Takes format (string) which describes the problem.
// Takes arguments (...any) which fill the description.
func (s *settings) reject(format string, arguments ...any) {
	s.problems = append(s.problems, fmt.Errorf("%s: %w", fmt.Sprintf(format, arguments...), fault.ErrInvalidConfig))
}

// WithEmitter sets the emitter that turns the analysed queries into Go. The default is
// the database/sql emitter (internal/emit/databasesql), whose placeholders come from the
// dialect's metadata.
//
// Takes emitter (emit.EmitterPort) which generates the Go source; nil is an error.
//
// Returns Option which sets the emitter.
func WithEmitter(emitter emit.EmitterPort) Option {
	return func(collected *settings) {
		if emitter == nil {
			collected.reject("WithEmitter: emitter is nil")
			return
		}
		collected.emitter = emitter
	}
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
	return func(collected *settings) {
		collected.preset = chosen
	}
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
	return func(collected *settings) {
		collected.directives = &Directives{
			Canonical:        canonical,
			Aliases:          slices.Clone(aliases),
			Foreign:          nil,
			RequireCanonical: false,
		}
	}
}

// WithForeignDirectivePrefixes sets the prefixes that belong to another tool, replacing
// the preset's. A directive written with one is reported with a Q051 warning rather than
// parsed.
//
// Takes prefixes (...string) which are the foreign prefixes; none clears them.
//
// Returns Option which sets the foreign prefixes.
func WithForeignDirectivePrefixes(prefixes ...string) Option {
	return func(collected *settings) {
		collected.foreign = slices.Clone(prefixes)
		collected.foreignSet = true
	}
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
	return func(collected *settings) {
		collected.typeOverrides = append(collected.typeOverrides, overrides...)
	}
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
	return func(collected *settings) {
		for importPath, name := range names {
			switch {
			case !directive.IsImportPath(importPath):
				collected.reject("package name for %q: the path is not a valid import path", importPath)
			case !preset.IsDeclarableName(name):
				collected.reject("package name %q for %q is not a Go identifier", name, importPath)
			default:
				if collected.packageNames == nil {
					collected.packageNames = make(map[string]string, len(names))
				}
				collected.packageNames[importPath] = name
			}
		}
	}
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
	return func(collected *settings) {
		collected.customFunctions = append(collected.customFunctions, functions...)
	}
}

// WithSchemaSource builds the catalogue from an external schema source.
//
// The generator works on a copy, excludes runtime tables and resolves view definitions
// before query analysis.
//
// Takes source (schema.SchemaSourcePort) which builds the catalogue; nil is an error.
//
// Returns Option which sets the schema source.
func WithSchemaSource(source schema.SchemaSourcePort) Option {
	return func(collected *settings) {
		if source == nil {
			collected.reject("WithSchemaSource: source is nil")
			return
		}
		collected.schemaSource = source
	}
}

// WithRuntimeTables excludes migration and seed runtime tables from a schema source's
// catalogue.
//
// Names match without regard to schema or case. Custom backends should pass the names
// reported by Backend.Names. This option has no effect without WithSchemaSource.
//
// Takes names (...string) which replace the default; none keeps every table.
//
// Returns Option which sets the names; New rejects an empty name with ErrInvalidConfig.
func WithRuntimeTables(names ...string) Option {
	return func(collected *settings) {
		if slices.Contains(names, "") {
			collected.reject("WithRuntimeTables: a table name is empty")
			return
		}
		collected.runtimeTables = slices.Clone(names)
	}
}

// WithLogger sets the logger every run logs through. Without it a run logs through the
// logger carried by its context (see ContextWithLogger), or slog.Default().
//
// Takes logger (*slog.Logger) which receives the log records; nil keeps the default.
//
// Returns Option which sets the logger.
func WithLogger(logger *slog.Logger) Option {
	return func(collected *settings) {
		collected.logger = logger
	}
}

// WithObserver sets the observer that receives a span for each stage of every run, for
// tracing and metrics. Without it a run reports to the observer carried by its context
// (see observe.ContextWithObserver), or to none.
//
// Takes observer (observe.ObserverPort) which receives the stage spans; nil keeps the
// default.
//
// Returns Option which sets the observer.
func WithObserver(observer observe.ObserverPort) Option {
	return func(collected *settings) {
		collected.observer = observer
	}
}

// WithClock sets the clock that times each run for the trace log. The default is
// observe.SystemClock.
//
// Takes clock (observe.Clock) which supplies the time; nil keeps the default.
//
// Returns Option which sets the clock.
func WithClock(clock observe.Clock) Option {
	return func(collected *settings) {
		collected.clock = clock
	}
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
	return func(collected *settings) {
		if limit <= 0 {
			collected.reject("WithMaxFileBytes: limit must be positive, got %d", limit)
			return
		}
		if limit > sqlfile.MaxFileBytesCeiling {
			collected.reject("WithMaxFileBytes: limit must be at most %d, got %d", sqlfile.MaxFileBytesCeiling, limit)
			return
		}
		collected.limits.MaxFileBytes = limit
	}
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
	return func(collected *settings) {
		if limit <= 0 {
			collected.reject("WithMaxTotalBytes: limit must be positive, got %d", limit)
			return
		}
		collected.limits.MaxTotalBytes = limit
	}
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
	return func(collected *settings) {
		if limit <= 0 {
			collected.reject("WithMaxFiles: limit must be positive, got %d", limit)
			return
		}
		collected.limits.MaxFiles = limit
	}
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
	return func(collected *settings) {
		if limit <= 0 {
			collected.reject("WithMaxQueries: limit must be positive, got %d", limit)
			return
		}
		collected.limits.MaxQueries = limit
	}
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
	return func(collected *settings) {
		if limit <= 0 {
			collected.reject("WithMaxDiagnostics: limit must be positive, got %d", limit)
			return
		}
		collected.limits.MaxDiagnostics = limit
	}
}

// WithSkipFailingQueries enables generation of queries without their own error
// diagnostics.
//
// Result.Partial identifies partial output, while Result.Err retains the errors.
// Migration failures and package-wide errors still stop generation.
//
// Returns Option which sets the behaviour.
func WithSkipFailingQueries() Option {
	return func(collected *settings) {
		collected.skipFailingQueries = true
	}
}

// cloneTypeOverrides copies type overrides deeply: each override's struct tags and
// element choice too.
//
// Takes overrides ([]TypeOverride) which are the overrides to copy.
//
// Returns []sqlmodel.TypeOverride which shares nothing with overrides, or nil for none.
func cloneTypeOverrides(overrides []TypeOverride) []sqlmodel.TypeOverride {
	if overrides == nil {
		return nil
	}
	cloned := make([]sqlmodel.TypeOverride, len(overrides))
	for index, override := range overrides {
		override.StructTags = maps.Clone(override.StructTags)
		if override.NonNullElements != nil {
			override.NonNullElements = new(*override.NonNullElements)
		}
		cloned[index] = override
	}
	return cloned
}

// cloneCustomFunctions copies custom function declarations deeply: each one's arguments
// too.
//
// Takes functions ([]CustomFunction) which are the declarations to copy.
//
// Returns []sqlmodel.CustomFunctionConfig which shares nothing with functions, or nil for
// none.
func cloneCustomFunctions(functions []CustomFunction) []sqlmodel.CustomFunctionConfig {
	if functions == nil {
		return nil
	}
	cloned := make([]sqlmodel.CustomFunctionConfig, len(functions))
	for index, function := range functions {
		function.Arguments = slices.Clone(function.Arguments)
		cloned[index] = function
	}
	return cloned
}

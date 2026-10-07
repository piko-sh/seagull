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

package session

import (
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	"seagull.sh/seagull"
	"seagull.sh/seagull/cmd/internal/config"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_registry"
)

const (
	// DefaultDialect is the dialect a project without sgl.yaml uses unless the request's
	// override chooses another.
	DefaultDialect = "sqlite"

	// defaultDatabaseName names the database of a project without sgl.yaml.
	defaultDatabaseName = "playground"

	// defaultConfigTemplate is the configuration a project without sgl.yaml is given, filled
	// with the database name and the dialect. Parsing it applies sgl's own defaults.
	defaultConfigTemplate = "version: 1\ndatabases:\n  - name: %s\n    dialect: %s\n"

	// maxOverrideEntries is the most entries each of an override's collections, its struct
	// tags, package names and search path, may hold.
	maxOverrideEntries = 64

	// maxOverrideTextRunes is the longest text an override may hold in one of its strings or
	// in one entry of its collections.
	maxOverrideTextRunes = 256

	// playgroundMaxParseDepth is the deepest a statement may nest in the playground, however
	// deep sgl.yaml's dialect_options.max_parse_depth allows.
	playgroundMaxParseDepth = dialect_registry.DefaultMaxParseDepth

	// playgroundMaxTokensPerStatement is the most tokens one statement may hold in the
	// playground, however many sgl.yaml's dialect_options.max_tokens_per_statement allows.
	playgroundMaxTokensPerStatement = dialect_registry.DefaultMaxTokensPerStatement

	// playgroundMaxTypeParseDepth is the deepest a type name may nest in the playground,
	// however deep sgl.yaml's dialect_options.max_type_parse_depth allows.
	playgroundMaxTypeParseDepth = dialect.MaxTypeDepth

	// optionMaxParseDepth is the sgl.yaml key of the parse depth limit.
	optionMaxParseDepth = "max_parse_depth"

	// optionMaxStatementSize is the sgl.yaml key of the per-statement token limit.
	optionMaxStatementSize = "max_tokens_per_statement"

	// optionMaxTypeParseDepth is the sgl.yaml key of the type-name depth limit.
	optionMaxTypeParseDepth = "max_type_parse_depth"
)

// project is the configuration one run uses: the parsed sgl.yaml, or sgl's defaults, with
// the request's overrides applied.
type project struct {
	// loaded is the whole configuration.
	loaded *config.Config

	// database is the database the run generates, the first in the configuration.
	database *config.Database

	// file is the configuration file's path in the request, or empty when the project has
	// none.
	file string

	// migrations is the cleaned migration directory.
	migrations string

	// queries is the cleaned query directory.
	queries string

	// warnings holds the configuration the playground cannot honour.
	warnings []string

	// skipFailingQueries generates the queries without an error of their own and leaves out
	// those with one, as sgl generate does.
	skipFailingQueries bool
}

// applyDialectOverride applies the playground's dialect options to the database, leaving
// out, with a warning, each the database's dialect does not take.
//
// Takes override (*Override) which holds the settings.
func (p *project) applyDialectOverride(override *Override) {
	options := &p.database.DialectOptions
	if override.GroupByUseNulls != nil && p.takes(optionGroupByUseNulls) {
		options.GroupByUseNulls = new(*override.GroupByUseNulls)
	}
	if override.ANSIQuotes != nil && p.takes(optionANSIQuotes) {
		options.ANSIQuotes = new(*override.ANSIQuotes)
	}
	if override.SearchPath != nil && p.takes(optionSearchPath) {
		options.SearchPath = slices.Clone(override.SearchPath)
	}
}

// limitParser lowers each parser limit sgl.yaml raises above the playground's to the
// playground's, with a warning, so a project cannot make the analyser nest deeper or read
// longer statements than the playground's memory allows.
func (p *project) limitParser() {
	options := &p.database.DialectOptions
	p.limit(&options.MaxParseDepth, optionMaxParseDepth, playgroundMaxParseDepth)
	p.limit(&options.MaxTokensPerStatement, optionMaxStatementSize, playgroundMaxTokensPerStatement)
	p.limit(&options.MaxTypeParseDepth, optionMaxTypeParseDepth, playgroundMaxTypeParseDepth)
}

// limit lowers one parser limit to a maximum, warning that the playground did.
//
// Takes setting (*int) which is the configured limit, changed in place.
// Takes key (string) which is the limit's sgl.yaml key under dialect_options.
// Takes maximum (int) which is the playground's limit.
func (p *project) limit(setting *int, key string, maximum int) {
	if *setting <= maximum {
		return
	}
	p.warn("dialect_options.%s %d is above the playground's limit; the playground used %d", key, *setting, maximum)
	*setting = maximum
}

// validate checks the generated database once the overrides are applied. Parsing sgl.yaml
// checked the whole configuration, and the overrides change only this database, so it is
// checked alone, with the configuration's version and directives, rather than every
// database a second time.
//
// Returns error which wraps config.ErrInvalid and lists the problems found.
func (p *project) validate() error {
	generated := config.Config{
		Directives: p.loaded.Directives,
		Databases:  []config.Database{*p.database},
		Schema:     p.loaded.Schema,
		Version:    p.loaded.Version,
	}
	return generated.Validate()
}

// takes reports whether the database's dialect takes a dialect option, and warns that the
// playground leaves the option out when it does not.
//
// Takes key (string) which is the option's sgl.yaml key.
//
// Returns bool which is true when the dialect takes the option.
func (p *project) takes(key string) bool {
	if takesDialectOption(p.database.Dialect, key) {
		return true
	}
	p.warn("dialect_options.%s does not apply to %s; the playground left it out", key, p.database.Dialect)
	return false
}

// warn records configuration the playground cannot honour.
//
// Takes format (string) which describes it.
// Takes arguments (...any) which fill the description.
func (p *project) warn(format string, arguments ...any) {
	p.warnings = append(p.warnings, fmt.Sprintf(format, arguments...))
}

// sourcePath maps a diagnostic's file name, which is relative to its directory, back to
// the path the request gave the file: a query file first, then a migration.
//
// Takes files (map[string]string) which maps cleaned paths to contents.
// Takes filename (string) which is the diagnostic's file name.
//
// Returns string which is the request path, the configuration file when filename is
// empty, or filename itself when no file matches.
func (p *project) sourcePath(files map[string]string, filename string) string {
	if filename == "" {
		return p.file
	}
	for _, directory := range []string{p.queries, p.migrations} {
		candidate := path.Join(directory, filename)
		if _, found := files[candidate]; found {
			return candidate
		}
	}
	return filename
}

// migrationPath maps a migration's file name back to the path the request gave it.
//
// Takes files (map[string]string) which maps cleaned paths to contents.
// Takes filename (string) which is the migration's file name.
//
// Returns string which is the request path, or filename when no file matches.
func (p *project) migrationPath(files map[string]string, filename string) string {
	candidate := path.Join(p.migrations, filename)
	if _, found := files[candidate]; found {
		return candidate
	}
	return filename
}

// queryPath maps a query file's name back to the path the request gave it.
//
// Takes files (map[string]string) which maps cleaned paths to contents.
// Takes filename (string) which is the query file's name.
//
// Returns string which is the request path, or filename when no file matches.
func (p *project) queryPath(files map[string]string, filename string) string {
	candidate := path.Join(p.queries, filename)
	if _, found := files[candidate]; found {
		return candidate
	}
	return filename
}

// view returns the configuration as the response shows it.
//
// Returns *ConfigView which describes the configuration the run used.
func (p *project) view() *ConfigView {
	warnings := p.warnings
	if warnings == nil {
		warnings = []string{}
	}
	settings := &p.database.Go
	options := &p.database.DialectOptions
	return &ConfigView{
		File:               p.file,
		Database:           p.database.Name,
		Dialect:            p.database.Dialect,
		Driver:             settings.Driver,
		Package:            settings.Package,
		Migrations:         p.migrations,
		Queries:            p.queries,
		Warnings:           warnings,
		SearchPath:         nonNil(slices.Clone(options.SearchPath)),
		StructTags:         nonNilMap(settings.StructTags),
		PackageNames:       nonNilMap(settings.Types.PackageNames),
		GroupByUseNulls:    cloneFlag(options.GroupByUseNulls),
		ANSIQuotes:         cloneFlag(options.ANSIQuotes),
		InterfaceName:      settings.Interface.Name,
		JSONCase:           settings.JSONTags.Case,
		Prepared:           settings.Prepared,
		SkipFailingQueries: p.skipFailingQueries,
		NonNullElements:    settings.Types.NonNullElements,
		KeepStars:          settings.KeepStars,
		Interface:          settings.Interface.Enabled,
		JSONOmitEmpty:      settings.JSONTags.OmitEmpty,
		JSONOmit:           settings.JSONTags.Omit,
		EmptySlices:        settings.EmptySlices,
		DocComments:        settings.DocComments,
		ReuseModels:        settings.ReuseModels,
		WrapErrors:         settings.WrapErrors,
	}
}

// jsonTags says how the generated fields' json tags name the columns they hold, as the
// project's go.json_tags shapes them.
//
// Returns jsonTagStyle which names a column's tag as the emitters do.
func (p *project) jsonTags() jsonTagStyle {
	style := seagull.JSONCase(p.database.Go.JSONTags.Case)
	return jsonTagStyle{
		name:    func(column string) string { return jsonTagName(column, style) },
		omitted: p.database.Go.JSONTags.Omit,
	}
}

// resolveProject reads the project's configuration, or synthesises sgl's defaults, and
// applies the request's overrides. Parser limits above the playground's are lowered to
// them with a warning.
//
// Takes files (map[string]string) which maps cleaned paths to contents.
// Takes override (Override) which holds the playground's settings.
//
// Returns *project which is ready to assemble.
// Returns error when the override holds more than the playground takes, the configuration
// is invalid or names a directory outside the project.
func resolveProject(files map[string]string, override Override) (*project, error) {
	if err := checkOverride(&override); err != nil {
		return nil, fmt.Errorf("the playground's settings: %w", err)
	}
	resolved, err := loadProject(files)
	if err != nil {
		return nil, err
	}
	database := resolved.database
	if override.Dialect != "" {
		database.Dialect = override.Dialect
	}
	if override.Driver != "" {
		database.Go.Driver = override.Driver
	}
	resolved.skipFailingQueries = override.SkipFailingQueries != nil && *override.SkipFailingQueries
	applyGoOverride(&database.Go, &override)
	resolved.applyDialectOverride(&override)
	resolved.limitParser()
	if err := resolved.validate(); err != nil {
		return nil, fmt.Errorf("the playground's settings: %w", err)
	}

	resolved.migrations, err = projectDirectory(database, "migrations", database.Migrations)
	if err != nil {
		return nil, err
	}
	resolved.queries, err = projectDirectory(database, "queries", database.Queries)
	if err != nil {
		return nil, err
	}
	return resolved, nil
}

// applyGoOverride applies the playground's settings for the generated code to a
// database's go section.
//
// Takes settings (*config.Go) which is changed in place.
// Takes override (*Override) which holds the settings.
func applyGoOverride(settings *config.Go, override *Override) {
	setFlag(&settings.Prepared, override.Prepared)
	setFlag(&settings.Types.NonNullElements, override.NonNullElements)
	setFlag(&settings.KeepStars, override.KeepStars)
	setFlag(&settings.Interface.Enabled, override.Interface)
	setFlag(&settings.JSONTags.OmitEmpty, override.JSONOmitEmpty)
	setFlag(&settings.JSONTags.Omit, override.JSONOmit)
	setFlag(&settings.EmptySlices, override.EmptySlices)
	setFlag(&settings.DocComments, override.DocComments)
	setFlag(&settings.ReuseModels, override.ReuseModels)
	setFlag(&settings.WrapErrors, override.WrapErrors)
	if override.InterfaceName != "" {
		settings.Interface.Name = override.InterfaceName
	}
	if override.JSONCase != "" {
		settings.JSONTags.Case = override.JSONCase
	}
	if override.StructTags != nil {
		settings.StructTags = maps.Clone(override.StructTags)
	}
	if override.PackageNames != nil {
		settings.Types.PackageNames = maps.Clone(override.PackageNames)
	}
}

// setFlag replaces a setting with an override's value when the override sets one.
//
// Takes setting (*bool) which is changed in place.
// Takes value (*bool) which is the override's value, or nil to keep the setting.
func setFlag(setting, value *bool) {
	if value != nil {
		*setting = *value
	}
}

// loadProject parses the project's configuration file, or sgl's defaults when it has
// none, and notes what the playground ignores.
//
// Takes files (map[string]string) which maps cleaned paths to contents.
//
// Returns *project which holds the configuration, before overrides.
// Returns error when the configuration is invalid.
func loadProject(files map[string]string) (*project, error) {
	file, content := findConfig(files)
	if file == "" {
		content = fmt.Sprintf(defaultConfigTemplate, defaultDatabaseName, DefaultDialect)
	}
	loaded, err := config.Parse([]byte(content), path.Ext(file) == ".json")
	if err != nil {
		if file == "" {
			return nil, fmt.Errorf("the default configuration: %w", err)
		}
		return nil, fmt.Errorf("%s: %w", file, err)
	}

	resolved := &project{loaded: loaded, database: &loaded.Databases[0], file: file, migrations: "", queries: "", warnings: nil, skipFailingQueries: false}
	if len(loaded.Databases) > 1 {
		resolved.warn("%s declares %d databases; the playground generates the first, %q", file, len(loaded.Databases), resolved.database.Name)
	}
	if resolved.database.SchemaFrom != "" {
		resolved.warn("database %q: schema_from is ignored; the playground builds the schema from the migrations", resolved.database.Name)
	}
	return resolved, nil
}

// findConfig finds the configuration file at the project's root.
//
// Takes files (map[string]string) which maps cleaned paths to contents.
//
// Returns file (string) which is the file's path, or empty when there is none.
// Returns content (string) which is its content.
func findConfig(files map[string]string) (file, content string) {
	for _, name := range config.FileNames() {
		if content, found := files[name]; found {
			return name, content
		}
	}
	return "", ""
}

// projectDirectory cleans a configured directory and checks it lies inside the project.
//
// Takes database (*config.Database) which names the database, for messages.
// Takes key (string) which is the configuration key, for messages.
// Takes directory (string) which is the configured directory.
//
// Returns string which is the cleaned, slash-separated directory.
// Returns error when the directory is absolute or escapes the project.
func projectDirectory(database *config.Database, key, directory string) (string, error) {
	cleaned := path.Clean(strings.ReplaceAll(directory, `\`, "/"))
	if !fs.ValidPath(cleaned) {
		return "", fmt.Errorf("database %q: %s %q must be a directory inside the project", database.Name, key, directory)
	}
	return cleaned, nil
}

// nonNilMap copies a map of strings, or returns an empty one in place of nil, so it
// encodes as {} and shares nothing with the configuration.
//
// Takes values (map[string]string) which is copied.
//
// Returns map[string]string which is never nil.
func nonNilMap(values map[string]string) map[string]string {
	copied := make(map[string]string, len(values))
	maps.Copy(copied, values)
	return copied
}

// cloneFlag copies an optional flag, so the view shares nothing with the configuration.
//
// Takes value (*bool) which is copied.
//
// Returns *bool which is a fresh copy, or nil.
func cloneFlag(value *bool) *bool {
	if value == nil {
		return nil
	}
	return new(*value)
}

// checkOverride checks that an override's collections and strings are no larger than the
// playground takes, so a request cannot multiply them into every generated field.
//
// Takes override (*Override) which is checked.
//
// Returns error which wraps errOverLimit, naming the first field over its limit.
func checkOverride(override *Override) error {
	collections := []struct {
		key   string
		count int
	}{
		{key: "structTags", count: len(override.StructTags)},
		{key: "packageNames", count: len(override.PackageNames)},
		{key: "searchPath", count: len(override.SearchPath)},
	}
	for _, collection := range collections {
		if collection.count > maxOverrideEntries {
			return fmt.Errorf("override.%s holds %d entries, %w: at most %d", collection.key, collection.count, errOverLimit, maxOverrideEntries)
		}
	}
	fields := []struct {
		key    string
		values []string
	}{
		{key: "structTags", values: mapTexts(override.StructTags)},
		{key: "packageNames", values: mapTexts(override.PackageNames)},
		{key: "searchPath", values: override.SearchPath},
		{key: "dialect", values: []string{override.Dialect}},
		{key: "driver", values: []string{override.Driver}},
		{key: "interfaceName", values: []string{override.InterfaceName}},
		{key: "jsonCase", values: []string{override.JSONCase}},
	}
	for _, field := range fields {
		for _, value := range field.values {
			if length := utf8.RuneCountInString(value); length > maxOverrideTextRunes {
				return fmt.Errorf("override.%s holds a text of %d characters, %w: at most %d", field.key, length, errOverLimit, maxOverrideTextRunes)
			}
		}
	}
	return nil
}

// mapTexts lists the keys and values of a map of strings.
//
// Takes values (map[string]string) which is read.
//
// Returns []string which holds each key followed by its value, in no particular order.
func mapTexts(values map[string]string) []string {
	texts := make([]string, 0, 2*len(values))
	for key, value := range values {
		texts = append(texts, key, value)
	}
	return texts
}

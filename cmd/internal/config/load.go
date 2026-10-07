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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
	"piko.sh/safedisk"

	"seagull.sh/seagull/cmd/internal/editdistance"
)

const (
	// maxSuggestionDistance is the largest edit distance at which an unknown key is matched
	// to a known one.
	maxSuggestionDistance = 2

	// migrateDSNKey is the key of a database's migration connection string.
	migrateDSNKey = "migrate.dsn"

	// schemaFromKey is the key of a database's live schema connection string.
	schemaFromKey = "schema_from"

	// yamlKeyGroup is the group of unknownFieldPattern holding a key YAML does not know.
	yamlKeyGroup = 1

	// yamlTypeGroup is the group of unknownFieldPattern holding the Go type that lacks it.
	yamlTypeGroup = 2

	// jsonKeyGroup is the group of unknownFieldPattern holding a key JSON does not know.
	jsonKeyGroup = 3
)

var (
	// fileNames lists the configuration file names Discover looks for, in order.
	fileNames = []string{"sgl.yaml", "sgl.yml", "sgl.json", "seagull.yaml", "seagull.yml", "seagull.json"}

	// defaultPrefixes lists the directive prefixes a configuration that sets none accepts,
	// the canonical one first.
	defaultPrefixes = []string{"sgl", "seagull"}

	// environmentReference matches ${env:NAME}.
	environmentReference = regexp.MustCompile(`\$\{env:([A-Za-z_][A-Za-z0-9_]*)\}`)

	// unknownFieldPattern extracts the key, and for YAML the Go type that lacks it, from one
	// line of a strict-decoding error.
	unknownFieldPattern = regexp.MustCompile(`field (\S+) not found(?: in type (\S+))?|unknown field "([^"]+)"`)
)

// ExpandEnvironment replaces ${env:NAME} references in connection strings (migrate.dsn
// and schema_from) with values from lookup. Other keys are never expanded.
//
// A reference lookup cannot resolve stays as written and is reported, so a connection
// string is never used with a part of it silently missing.
//
// Load expands with the process environment and leaves an unresolved reference for
// RequireExpanded to report where the connection string is used, so a command that never
// connects does not need the variable. A program that reads untrusted configuration with
// Parse or ReadFS decides whether, and from where, to expand.
//
// Takes lookup (func(string) (string, bool)) which resolves a variable name, such as
// os.LookupEnv.
//
// Returns error which wraps ErrUnsetVariable, naming each database, key and variable that
// could not be resolved, or nil when every reference was.
func (c *Config) ExpandEnvironment(lookup func(string) (string, bool)) error {
	unset := newProblemList()
	c.expandEnvironment(lookup, func(label, key, name string) {
		unset.add(fmt.Sprintf("%s: %s refers to ${env:%s}, which is not set", label, key, name))
	})
	if unset.empty() {
		return nil
	}
	return fmt.Errorf("%w:%s%s", ErrUnsetVariable, problemIndent, unset.text())
}

// Database returns the database with the given name.
//
// Takes name (string) which identifies the database.
//
// Returns *Database which is the match.
// Returns bool which is false when no database has that name.
func (c *Config) Database(name string) (*Database, bool) {
	for index := range c.Databases {
		if c.Databases[index].Name == name {
			return &c.Databases[index], true
		}
	}
	return nil, false
}

// applyDefaults fills in every unset value with its default.
func (c *Config) applyDefaults() {
	if c.Version == 0 {
		c.Version = CurrentVersion
	}
	if len(c.Directives.Prefixes) == 0 {
		c.Directives.Prefixes = DefaultPrefixes()
	}
	for index := range c.Databases {
		database := &c.Databases[index]
		database.Migrations = firstNonEmpty(database.Migrations, DefaultMigrationsDirectory)
		database.Queries = firstNonEmpty(database.Queries, DefaultQueriesDirectory)
		database.Go.Package = firstNonEmpty(database.Go.Package, DefaultPackage)
		database.Go.Out = firstNonEmpty(database.Go.Out, DefaultOutputDirectory)
		database.Go.Driver = firstNonEmpty(database.Go.Driver, DefaultDriver)
		database.Go.QueryNames.File = firstNonEmpty(database.Go.QueryNames.File, DefaultQueryNamesFile)
		database.Go.QueryNames.Function = firstNonEmpty(database.Go.QueryNames.Function, DefaultQueryNamesFunction)
		database.Go.Interface.Name = firstNonEmpty(database.Go.Interface.Name, DefaultInterfaceName)
		database.Migrate.Namespace = firstNonEmpty(database.Migrate.Namespace, DefaultNamespace)
	}
}

// expandEnvironment replaces the ${env:NAME} references in connection strings that lookup
// resolves, and leaves the others as written.
//
// Takes lookup (func(string) (string, bool)) which resolves a variable name.
// Takes unresolved (func(label, key, name string)) which is told of each reference lookup
// cannot resolve: the database, the key and the variable's name.
func (c *Config) expandEnvironment(lookup func(string) (string, bool), unresolved func(label, key, name string)) {
	for index := range c.Databases {
		database := &c.Databases[index]
		label := databaseLabel(index, database)
		database.Migrate.DSN = expandReferences(database.Migrate.DSN, lookup, func(name string) { unresolved(label, migrateDSNKey, name) })
		database.SchemaFrom = expandReferences(database.SchemaFrom, lookup, func(name string) { unresolved(label, schemaFromKey, name) })
	}
}

// resolvePaths makes relative directories relative to base.
//
// Takes base (string) which is the configuration file's directory.
func (c *Config) resolvePaths(base string) {
	resolve := func(path string) string {
		if path == "" || filepath.IsAbs(path) {
			return path
		}
		return filepath.Join(base, path)
	}
	for index := range c.Databases {
		database := &c.Databases[index]
		database.Migrations = resolve(database.Migrations)
		database.Queries = resolve(database.Queries)
		database.Seeds = resolve(database.Seeds)
		database.Go.Out = resolve(database.Go.Out)
	}
}

// Discover finds the configuration file for a working directory by looking in it and then
// in each parent directory.
//
// Takes directory (string) which is where the search starts.
//
// Returns string which is the path of the file found.
// Returns error which wraps ErrNotFound when no file exists.
func Discover(directory string) (string, error) {
	current, err := filepath.Abs(directory)
	if err != nil {
		return "", fmt.Errorf("resolving %q: %w", directory, err)
	}
	for {
		for _, name := range fileNames {
			candidate := filepath.Join(current, name)
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
				return candidate, nil
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("%w (searched from %s)", ErrNotFound, directory)
		}
		current = parent
	}
}

// Load reads, decodes, defaults and validates a configuration file, and expands
// ${env:NAME} references in its connection strings from the process environment. A
// reference to a variable that is not set is left as written, for RequireExpanded to
// report where the connection string is used.
//
// Relative directories in the file are resolved against the file's own directory before
// validation, so two spellings of one output directory are caught. Validation problems
// name a go.out directory relative to the file's directory when it is inside it.
//
// Takes path (string) which is the configuration file.
//
// Returns *Config which is ready to use.
// Returns error which is a *FileError naming path when the file cannot be read, is too
// large, has unknown keys, or fails validation.
func Load(path string) (*Config, error) {
	config, err := load(path)
	if err != nil {
		return nil, &FileError{Err: err, Path: path}
	}
	return config, nil
}

// Parse decodes configuration data strictly, then applies defaults and validates the
// result. It reads nothing from the process: ${env:NAME} references stay as written until
// ExpandEnvironment is called.
//
// Takes data ([]byte) which is the file content.
// Takes isJSON (bool) which selects JSON instead of YAML.
//
// Returns *Config which is ready to use.
// Returns error when decoding or validation fails.
func Parse(data []byte, isJSON bool) (*Config, error) {
	config, err := decode(data, isJSON)
	if err != nil {
		return nil, err
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return config, nil
}

// ReadFS is a small helper for tests and embedders that keep sgl.yaml in an fs.FS. Like
// Parse, it reads nothing from the process environment.
//
// Takes filesystem (fs.FS) which holds the file.
// Takes name (string) which is the file's path within filesystem.
//
// Returns *Config which is ready to use; relative directories stay relative.
// Returns error when reading or parsing fails, wrapping ErrTooLarge when the file is
// larger than MaxFileBytes.
func ReadFS(filesystem fs.FS, name string) (*Config, error) {
	file, err := filesystem.Open(name)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", name, err)
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", name, err)
	}
	if len(data) > MaxFileBytes {
		return nil, fmt.Errorf("%w: %s is larger than %d bytes", ErrTooLarge, name, MaxFileBytes)
	}
	return Parse(data, filepath.Ext(name) == ".json")
}

// FileNames lists the configuration file names Discover looks for, in the order it
// prefers them.
//
// Returns []string which is a fresh copy the caller may change.
func FileNames() []string {
	return slices.Clone(fileNames)
}

// DefaultPrefixes lists the directive prefixes a configuration that sets none accepts,
// the canonical one first.
//
// Returns []string which is a fresh copy the caller may change.
func DefaultPrefixes() []string {
	return slices.Clone(defaultPrefixes)
}

// RequireExpanded checks that a connection string holds no ${env:NAME} reference. After
// Load, a reference is left only where its variable is not set, so a command checks the
// connection string it is about to use.
//
// Takes key (string) which names the setting in the error, such as migrate.dsn.
// Takes value (string) which is the connection string.
//
// Returns error which wraps ErrUnsetVariable, naming the key and the variable, or nil
// when the value holds no reference.
func RequireExpanded(key, value string) error {
	match := environmentReference.FindStringSubmatch(value)
	if match == nil {
		return nil
	}
	return fmt.Errorf("%w: %s refers to ${env:%s}, which is not set", ErrUnsetVariable, key, match[1])
}

// load reads, decodes, defaults and validates a configuration file for Load. The errors
// it returns do not name the file, because Load wraps them in a FileError that does.
//
// Takes path (string) which is the configuration file.
//
// Returns *Config which is ready to use.
// Returns error when the file cannot be read, is too large, has unknown keys, or fails
// validation.
func load(path string) (*Config, error) {
	directory := filepath.Dir(path)
	sandbox, err := safedisk.NewSandbox(directory, safedisk.ModeReadOnly)
	if err != nil {
		return nil, fileSystemCause(err)
	}
	defer func() { _ = sandbox.Close() }()

	data, _, err := sandbox.ReadFileLimit(filepath.Base(path), MaxFileBytes)
	if errors.Is(err, safedisk.ErrFileExceedsLimit) {
		return nil, fmt.Errorf("%w (more than %d bytes)", ErrTooLarge, MaxFileBytes)
	}
	if err != nil {
		return nil, fileSystemCause(err)
	}

	config, err := decode(data, filepath.Ext(path) == ".json")
	if err != nil {
		return nil, err
	}
	config.resolvePaths(directory)
	if err := config.validate(directory); err != nil {
		return nil, err
	}
	config.expandEnvironment(os.LookupEnv, func(string, string, string) {})
	return config, nil
}

// fileSystemCause reduces a file-system error to its cause, such as "no such file or
// directory". The operation and path it drops belong to the sandbox Load reads through
// (statat and a name relative to the sandbox), not to anything the user wrote.
//
// Takes err (error) which is the failure.
//
// Returns error which is the cause, still matching fs.ErrNotExist and the like, or err
// itself when it carries no path.
func fileSystemCause(err error) error {
	if pathErr, ok := errors.AsType[*fs.PathError](err); ok {
		return pathErr.Err
	}
	return err
}

// decode decodes configuration data strictly and applies defaults, without validating.
//
// Takes data ([]byte) which is the file content.
// Takes isJSON (bool) which selects JSON instead of YAML.
//
// Returns *Config which holds the decoded values and defaults.
// Returns error when the data is malformed or has an unknown key.
func decode(data []byte, isJSON bool) (*Config, error) {
	config := &Config{Directives: Directives{Prefixes: nil, RequireCanonical: false}, Databases: nil, Schema: "", Version: 0}
	if err := decodeStrict(data, isJSON, config); err != nil {
		return nil, err
	}
	config.applyDefaults()
	return config, nil
}

// decodeStrict decodes data into config, rejecting unknown keys.
//
// Takes data ([]byte) which is the file content.
// Takes isJSON (bool) which selects JSON instead of YAML.
// Takes config (*Config) which receives the decoded values.
//
// Returns error when the data is malformed or has an unknown key.
func decodeStrict(data []byte, isJSON bool, config *Config) error {
	if isJSON {
		return decodeJSON(data, config)
	}
	return decodeYAML(data, config)
}

// decodeJSON decodes one JSON object into config, rejecting unknown keys and anything but
// white space after the object.
//
// Takes data ([]byte) which is the file content.
// Takes config (*Config) which receives the decoded values.
//
// Returns error which wraps ErrInvalid when the data is malformed, has an unknown key, or
// holds more than the one object.
func decodeJSON(data []byte, config *Config) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(config); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalid, withSuggestions(err.Error()))
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: trailing data after the configuration", ErrInvalid)
	}
	return nil
}

// decodeYAML decodes one YAML document into config, rejecting unknown keys and any
// further document. An empty file decodes to no settings.
//
// Takes data ([]byte) which is the file content.
// Takes config (*Config) which receives the decoded values.
//
// Returns error which wraps ErrInvalid when the data is malformed, has an unknown key, or
// holds more than one document.
func decodeYAML(data []byte, config *Config) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	err := decoder.Decode(config)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInvalid, withSuggestions(err.Error()))
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: the configuration holds more than one document", ErrInvalid)
	}
	return nil
}

// withSuggestions proposes a known key for each unknown key a decoding error names, at
// the end of the line that names it. A YAML error lists every unknown key on a line of
// its own.
//
// Takes message (string) which is a decoding error.
//
// Returns string which is the message with a "; did you mean" suffix on each line that
// names an unknown key close to a known one.
func withSuggestions(message string) string {
	lines := strings.Split(message, "\n")
	for index, line := range lines {
		lines[index] = line + suggestionFor(line)
	}
	return strings.Join(lines, "\n")
}

// suggestionFor proposes a known key close to the unknown one named in a line of a
// decoding error. When the line names the Go type that lacks the key, as YAML errors do,
// only that type's keys are candidates.
//
// Takes line (string) which is one line of a decoding error.
//
// Returns string which is a "; did you mean" suffix, or empty.
func suggestionFor(line string) string {
	match := unknownFieldPattern.FindStringSubmatch(line)
	if match == nil {
		return ""
	}
	unknown, typeName := match[yamlKeyGroup], match[yamlTypeGroup]
	if unknown == "" {
		unknown = match[jsonKeyGroup]
	}
	best, bestDistance := "", maxSuggestionDistance+1
	for _, key := range knownKeys(typeName) {
		if distance := editdistance.Distance(unknown, key, bestDistance); distance < bestDistance {
			best, bestDistance = key, distance
		}
	}
	if best == "" {
		return ""
	}
	return fmt.Sprintf("; did you mean %q?", best)
}

// firstNonEmpty returns value, or fallback when value is empty.
//
// Takes value (string) which is preferred.
// Takes fallback (string) which is used when value is empty.
//
// Returns string which is the chosen value.
func firstNonEmpty(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// expandReferences replaces each ${env:NAME} reference in a value that lookup resolves.
//
// Takes value (string) which may hold references.
// Takes lookup (func(string) (string, bool)) which resolves a variable name.
// Takes unresolved (func(name string)) which is told of each name lookup cannot resolve.
//
// Returns string which is the value with the resolved references replaced and the others
// as written.
func expandReferences(value string, lookup func(string) (string, bool), unresolved func(name string)) string {
	return environmentReference.ReplaceAllStringFunc(value, func(reference string) string {
		name := environmentReference.FindStringSubmatch(reference)[1]
		resolved, found := lookup(name)
		if !found {
			unresolved(name)
			return reference
		}
		return resolved
	})
}

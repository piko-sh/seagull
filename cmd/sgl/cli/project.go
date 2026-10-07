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

package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"seagull.sh/seagull"
	"seagull.sh/seagull/cmd/internal/assemble"
	"seagull.sh/seagull/cmd/internal/config"
	"seagull.sh/seagull/sdk/dialect"
)

// assembledDatabase retains the validated dialect and immutable base options.
type assembledDatabase struct {
	// dialect is the validated SQL dialect implementation.
	dialect dialect.Dialect

	// options holds immutable base settings to copy for each session.
	options []seagull.Option
}

// project is a loaded configuration and the databases one command works on.
type project struct {
	// assembled holds settings already validated while loading the project.
	assembled map[*config.Database]assembledDatabase

	// config is the whole configuration.
	config *config.Config

	// path is the configuration file.
	path string

	// databases holds the databases the command works on.
	databases []*config.Database
}

// directory returns the configuration file's directory, which relative paths in the file
// are relative to.
//
// Returns string which is the directory.
func (p *project) directory() string {
	return filepath.Dir(p.path)
}

// loadProject finds, reads and validates the configuration and selects the databases.
//
// Takes database (string) which names one database, or is empty for all of them.
//
// Returns *project which is ready to use.
// Returns error when the file cannot be found or read, is invalid, names an unknown
// dialect or driver, or has no database of that name.
func (inv *invocation) loadProject(database string) (*project, error) {
	path, err := inv.configFile()
	if err != nil {
		return nil, err
	}
	loaded, err := config.Load(path)
	if err != nil {
		return nil, inv.configurationError(err)
	}
	assembled := make(map[*config.Database]assembledDatabase, len(loaded.Databases))
	for index := range loaded.Databases {
		inv.redactor.Add(loaded.Databases[index].Migrate.DSN)
		inv.redactor.Add(loaded.Databases[index].SchemaFrom)
		built, err := inv.checkDatabase(loaded, &loaded.Databases[index])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", inv.display(path), err)
		}
		assembled[&loaded.Databases[index]] = built
	}
	selected, err := selectDatabases(loaded, database)
	if err != nil {
		return nil, err
	}
	return &project{config: loaded, path: path, databases: selected, assembled: assembled}, nil
}

// configurationError names the configuration file in an error from config.Load the way
// sgl prints every path: relative to the working directory when it is inside it.
//
// Takes err (error) which is the failure.
//
// Returns error which wraps the same problem.
func (inv *invocation) configurationError(err error) error {
	fileErr, isFileError := errors.AsType[*config.FileError](err)
	if !isFileError {
		return err
	}
	return fmt.Errorf("%s: %w", inv.display(fileErr.Path), fileErr.Err)
}

// configFile returns the configuration file: the -c path, or the one found by searching
// upwards from the working directory.
//
// Returns string which is the file's path.
// Returns error which wraps config.ErrNotFound when there is none.
func (inv *invocation) configFile() (string, error) {
	if inv.configPath != "" {
		return inv.path(inv.configPath), nil
	}
	return config.Discover(inv.directory)
}

// checkDatabase validates a database's dialect, driver and generator settings before any
// files are read.
//
// Takes configuration (*config.Config) which holds the directive prefixes.
// Takes database (*config.Database) which is checked.
//
// Returns assembledDatabase which holds the validated dialect and base options.
// Returns error when configuration or generator validation fails.
func (inv *invocation) checkDatabase(configuration *config.Config, database *config.Database) (assembledDatabase, error) {
	assembled, err := inv.assembleDatabase(configuration, database)
	if err != nil {
		return assembledDatabase{}, err
	}
	if _, err := seagull.New(assembled.dialect, assembled.options...); err != nil {
		return assembledDatabase{}, usageError("database %q: %w", database.Name, err)
	}
	return assembled, nil
}

// assembleDatabase creates a database's dialect and the generator options its
// configuration sets.
//
// Takes configuration (*config.Config) which holds the directive prefixes.
// Takes database (*config.Database) which is assembled.
//
// Returns assembledDatabase which holds the dialect and base options.
// Returns error which is a usage error when the dialect, the driver or a Go type in the
// configuration cannot be used.
func (inv *invocation) assembleDatabase(configuration *config.Config, database *config.Database) (assembledDatabase, error) {
	if err := inv.knownDialect(database); err != nil {
		return assembledDatabase{}, err
	}
	options, built, err := assemble.Options(configuration, database, inv.factories())
	if err != nil {
		return assembledDatabase{}, asUsageError(err)
	}
	return assembledDatabase{dialect: built, options: options}, nil
}

// factories returns the dialects and emitters registered with this binary.
//
// Returns assemble.Factories which holds them.
func (inv *invocation) factories() assemble.Factories {
	return assemble.Factories{Dialects: inv.settings.dialects, Emitters: inv.settings.emitters}
}

// buildDialect creates the dialect a database names.
//
// Takes database (*config.Database) which names the dialect and its options.
//
// Returns dialect.Dialect which is ready to use.
// Returns error which is a usage error when the dialect is unknown or an option does not
// apply to it.
func (inv *invocation) buildDialect(database *config.Database) (dialect.Dialect, error) {
	if err := inv.knownDialect(database); err != nil {
		return dialect.Dialect{}, err
	}
	built, err := assemble.Dialect(database, inv.settings.dialects)
	if err != nil {
		return dialect.Dialect{}, asUsageError(err)
	}
	return built, nil
}

// knownDialect refuses a database whose dialect this binary neither registers nor ships,
// listing the ones it knows under the name the CLI runs as. The assemble package refuses
// such a dialect too, but it serves callers that are not this command, so its message
// cannot name the program.
//
// Takes database (*config.Database) which names the dialect.
//
// Returns error which is a usage error when the dialect is unknown, or nil.
func (inv *invocation) knownDialect(database *config.Database) error {
	known := inv.settings.dialectNames()
	if slices.Contains(known, database.Dialect) {
		return nil
	}
	return usageError("database %q: unknown dialect %q; this %s knows %s", database.Name, database.Dialect, inv.program, strings.Join(known, ", "))
}

// selectDatabases picks the databases a command works on.
//
// Takes loaded (*config.Config) which is the configuration.
// Takes name (string) which names one database, or is empty for all of them.
//
// Returns []*config.Database which holds the selection.
// Returns error when no database has that name.
func selectDatabases(loaded *config.Config, name string) ([]*config.Database, error) {
	if name != "" {
		database, found := loaded.Database(name)
		if !found {
			names := make([]string, 0, len(loaded.Databases))
			for index := range loaded.Databases {
				names = append(names, loaded.Databases[index].Name)
			}
			return nil, usageError("no database named %q; the configuration has %s", name, strings.Join(names, ", "))
		}
		return []*config.Database{database}, nil
	}
	selected := make([]*config.Database, 0, len(loaded.Databases))
	for index := range loaded.Databases {
		selected = append(selected, &loaded.Databases[index])
	}
	return selected, nil
}

// asUsageError marks a configuration that cannot be assembled as a usage error, exit
// status 2.
//
// Takes err (error) which describes the problem.
//
// Returns error which wraps err unchanged and carries exit status 2.
func asUsageError(err error) error {
	return usageError("%w", err)
}

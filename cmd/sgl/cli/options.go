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
	"io"
	"os"

	"golang.org/x/term"

	"seagull.sh/seagull/cmd/internal/assemble"
	"seagull.sh/seagull/sdk/migrate"
)

// Option configures the CLI. Pass options to Main or MainContext.
type Option func(*settings)

// DialectFactory builds a dialect from the dialect_options of a database in sgl.yaml.
type DialectFactory = assemble.DialectFactory

// EmitterRequest describes the database a custom emitter generates code for.
type EmitterRequest = assemble.EmitterRequest

// EmitterFactory builds the emitter for a go.driver value.
type EmitterFactory = assemble.EmitterFactory

// MigrationBackendFactory builds the migration backend for a dialect, with the same shape
// as the migrate_backend constructors, so migrate_backend_cockroachdb.New is one, and a
// third-party engine's is a function that returns migrate.CustomBackend of its
// migrate.BackendSpec.
//
// sgl passes the options the database's migrate section sets (namespace, history_table
// and lock) and, for a dialect of the MySQL family, whether the DSN sets multiStatements.
type MigrationBackendFactory func(options ...migrate.BackendOption) migrate.Backend

// settings holds what the options configured.
type settings struct {
	// stdin is the input stream.
	stdin io.Reader

	// stdout receives command output.
	stdout io.Writer

	// stderr receives errors, diagnostics of generate, and logs.
	stderr io.Writer

	// program is the name the CLI prints for itself, set with WithName.
	program string

	// dialects holds the dialects registered with WithDialect, by name.
	dialects map[string]DialectFactory

	// emitters holds the emitters registered with WithEmitter, by go.driver name.
	emitters map[string]EmitterFactory

	// backends holds the migration backends registered with WithMigrationBackend, by dialect
	// name or dialect family.
	backends map[string]MigrationBackendFactory

	// drivers maps a dialect name or dialect family to the database/sql driver name
	// registered with WithDriver.
	drivers map[string]string

	// terminal reports whether a file descriptor is a terminal, which decides whether sgl
	// asks for confirmation.
	terminal func(descriptor int) bool

	// directory is the working directory; empty means the process's.
	directory string

	// problems holds what the options could not use, such as a nil factory, which
	// MainContext reports as a usage error before running anything.
	problems []error
}

// newSettings applies the options to the defaults. A nil option is skipped.
//
// Takes options ([]Option) which are applied in order.
//
// Returns *settings which holds the result.
func newSettings(options []Option) *settings {
	configured := &settings{
		stdin:     os.Stdin,
		stdout:    os.Stdout,
		stderr:    os.Stderr,
		program:   defaultProgram,
		dialects:  map[string]DialectFactory{},
		emitters:  map[string]EmitterFactory{},
		backends:  map[string]MigrationBackendFactory{},
		drivers:   map[string]string{},
		terminal:  term.IsTerminal,
		directory: "",
		problems:  nil,
	}
	for _, option := range options {
		if option != nil {
			option(configured)
		}
	}
	return configured
}

// dialectNames returns the stock dialect names together with the registered ones.
//
// Returns []string which holds each name once, in ascending order.
func (s *settings) dialectNames() []string {
	return assemble.DialectNames(s.dialects)
}

// refuse records a problem with a registering option: an empty name, or a value it cannot
// use.
//
// Takes option (string) which names the option, such as WithDialect.
// Takes name (string) which is the name the option registers under.
// Takes unusable (bool) which reports that the registered value cannot be used, such as a
// nil factory.
//
// Returns bool which is true when the option has a problem and registers nothing.
func (s *settings) refuse(option, name string, unusable bool) bool {
	switch {
	case name == "":
		s.problems = append(s.problems, fmt.Errorf("%w: %s needs a name", errInvalidOption, option))
	case unusable:
		s.problems = append(s.problems, fmt.Errorf("%w: %s(%q) needs a value it can use, not a nil or empty one", errInvalidOption, option, name))
	default:
		return false
	}
	return true
}

// err joins the problems the options recorded.
//
// Returns error which describes every problem, or nil when there is none.
func (s *settings) err() error {
	return errors.Join(s.problems...)
}

// WithDialect registers a dialect under a name, so sgl.yaml can select it with dialect:.
//
// A name the stock binary already knows is replaced. An empty name or a nil factory makes
// the run fail with a usage error.
//
// Takes name (string) which is the dialect: value.
// Takes factory (DialectFactory) which builds the dialect.
//
// Returns Option which registers the dialect.
func WithDialect(name string, factory DialectFactory) Option {
	return func(s *settings) {
		if s.refuse("WithDialect", name, factory == nil) {
			return
		}
		s.dialects[name] = factory
	}
}

// WithEmitter registers an emitter under a name, so sgl.yaml can select it with
// go.driver:.
//
// A name the stock binary already knows is replaced. An empty name or a nil factory makes
// the run fail with a usage error.
//
// Takes name (string) which is the go.driver value.
// Takes factory (EmitterFactory) which builds the emitter.
//
// Returns Option which registers the emitter.
func WithEmitter(name string, factory EmitterFactory) Option {
	return func(s *settings) {
		if s.refuse("WithEmitter", name, factory == nil) {
			return
		}
		s.emitters[name] = factory
	}
}

// WithMigrationBackend registers a migration backend for a dialect or its family.
//
// Selection checks the dialect's reported name before its family. For each key, a
// registered backend takes precedence over the built-in one. A later registration for the
// same name replaces the earlier one. An empty name or a nil factory makes the run fail
// with a usage error.
//
// Takes dialectName (string) which is a dialect's Info().Name, such as cockroachdb, or a
// dialect family, such as postgres.
// Takes factory (MigrationBackendFactory) which builds the backend, such as
// migrate_backend_cockroachdb.New.
//
// Returns Option which registers the backend.
func WithMigrationBackend(dialectName string, factory MigrationBackendFactory) Option {
	return func(s *settings) {
		if s.refuse("WithMigrationBackend", dialectName, factory == nil) {
			return
		}
		s.backends[dialectName] = factory
	}
}

// WithDriver registers a database/sql driver for a dialect or its family.
//
// A dialect's own name takes precedence over its family. Later registrations replace
// earlier ones, and the binary must import the driver it registers. An empty dialect or
// driver name makes the run fail with a usage error.
//
// Takes dialectName (string) which is a dialect: value, such as duckdb, or a dialect
// family, such as postgres.
// Takes driverName (string) which is the name the driver registered with database/sql.
//
// Returns Option which registers the driver.
func WithDriver(dialectName, driverName string) Option {
	return func(s *settings) {
		if s.refuse("WithDriver", dialectName, driverName == "") {
			return
		}
		s.drivers[dialectName] = driverName
	}
}

// WithName sets the name the CLI prints for itself: in its help and usage, at the start
// of its error lines, in the commands its hints suggest and in its version line. Without
// it, the name is sgl; the seagull command passes seagull.
//
// The SARIF tool name stays sgl, so code scanning keeps one history whichever name ran
// it. An empty name makes the run fail with a usage error.
//
// Takes name (string) which is the program name, such as seagull.
//
// Returns Option which sets the name.
func WithName(name string) Option {
	return func(s *settings) {
		if s.refuse("WithName", name, false) {
			return
		}
		s.program = name
	}
}

// WithIO replaces the standard streams, for tests and for programs that capture sgl's
// output.
//
// Takes stdin (io.Reader) which is read for input; nil keeps os.Stdin.
// Takes stdout (io.Writer) which receives command output; nil keeps os.Stdout.
// Takes stderr (io.Writer) which receives errors and logs; nil keeps os.Stderr.
//
// Returns Option which replaces the streams.
func WithIO(stdin io.Reader, stdout, stderr io.Writer) Option {
	return func(s *settings) {
		if stdin != nil {
			s.stdin = stdin
		}
		if stdout != nil {
			s.stdout = stdout
		}
		if stderr != nil {
			s.stderr = stderr
		}
	}
}

// WithDirectory runs sgl as if it had been started in a directory: sgl.yaml is searched
// for from there and relative paths on the command line, including a SQLite database file
// in --dsn, --from-db or $SGL_DATABASE_URL, resolve against it.
//
// Programs that drive sgl for several projects use it instead of changing the process's
// directory.
//
// Takes directory (string) which is the directory; empty keeps the process's, and a
// relative one is resolved against the process's when sgl starts.
//
// Returns Option which sets the directory.
func WithDirectory(directory string) Option {
	return func(s *settings) {
		s.directory = directory
	}
}

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
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing/fstest"

	"piko.sh/safedisk"

	"seagull.sh/seagull"
	"seagull.sh/seagull/cmd/internal/config"
	"seagull.sh/seagull/cmd/sgl/internal/redact"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_duckdb"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_postgres"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
	"seagull.sh/seagull/sdk/observe"
	"seagull.sh/seagull/sdk/schema/schema_source_postgres"
	"seagull.sh/seagull/sdk/schema/schema_source_sqlite"
)

const (
	// goModFile is the name of a Go module's definition file.
	goModFile = "go.mod"

	// maxGoModPrefixBytes is how much of a go.mod sgl reads to find its module path.
	maxGoModPrefixBytes = 64 << 10
)

// session is one database ready for generation: its dialect, its generator, the input it
// reads, and any connection the schema source holds open.
type session struct {
	// dialect is the database's dialect.
	dialect dialect.Dialect

	// generator generates and vets the database's queries.
	generator *seagull.Generator

	// database is the configured database.
	database *config.Database

	// connection is the live database a schema source reads, or nil.
	connection *sql.DB

	// input names the migrations and queries.
	input seagull.Input
}

// Close releases the live database a schema source holds.
//
// Returns error when the connection cannot be closed.
func (s *session) Close() error {
	if s.connection == nil {
		return nil
	}
	return s.connection.Close()
}

// sessionOptions adjusts how a session is opened.
type sessionOptions struct {
	// observer receives the generator's stage spans, or is nil.
	observer observe.ObserverPort

	// schemaDSN, when set, builds the catalogue from this live database instead of the
	// configured schema_from or the migrations.
	schemaDSN string

	// migrationsOnly ignores schema_from, so the catalogue always comes from the migrations.
	migrationsOnly bool
}

// openSession assembles the dialect, emitter, preset and generator for one database.
//
// Takes loaded (*project) which holds the configuration.
// Takes database (*config.Database) which is the database.
// Takes options (sessionOptions) which adjust the session.
//
// Returns *session which is ready to use; Close releases it.
// Returns error when the configuration cannot be used or a schema source cannot connect.
func (inv *invocation) openSession(ctx context.Context, loaded *project, database *config.Database, options sessionOptions) (*session, error) {
	assembled, err := inv.assembledDatabase(loaded, database)
	if err != nil {
		return nil, err
	}
	built := assembled.dialect
	generatorOptions := slices.Clone(assembled.options)
	generatorOptions = append(generatorOptions, seagull.WithLogger(inv.generatorLogger), seagull.WithSkipFailingQueries())
	if options.observer != nil {
		generatorOptions = append(generatorOptions, seagull.WithObserver(options.observer))
	}

	input, inputErr := inv.sessionInput(loaded, database)
	if inputErr != nil {
		return nil, inputErr
	}
	opened := &session{dialect: built, generator: nil, database: database, connection: nil, input: input}
	schemaDSN, schemaDirectory, err := inv.schemaConnection(loaded, database, options)
	if err != nil {
		return nil, err
	}
	if schemaDSN == "" {
		if err := inv.requireMigrationDirectory(loaded, database); err != nil {
			return nil, err
		}
	}
	if schemaDSN != "" {
		schemaDSN = anchorDSN(built.Info().Family, schemaDSN, schemaDirectory)
		if err := inv.requireSchemaFile(built.Info().Family, schemaDSN); err != nil {
			return nil, err
		}
		source, connection, sourceErr := inv.schemaSource(ctx, database, built, schemaDSN)
		if sourceErr != nil {
			return nil, sourceErr
		}
		opened.connection = connection
		generatorOptions = append(generatorOptions,
			seagull.WithSchemaSource(source),
			seagull.WithRuntimeTables(bookkeepingTables(database, new(built.Info()), inv.settings.backends)...),
		)
	}

	opened.generator, err = seagull.New(built, generatorOptions...)
	if err != nil {
		return nil, errors.Join(usageError("database %q: %w", database.Name, err), opened.Close())
	}
	return opened, nil
}

// schemaConnection chooses the live database a session builds its catalogue from: the one
// the command names, or the configured schema_from unless the session reads only the
// migrations.
//
// Takes loaded (*project) which holds the configuration file's location.
// Takes database (*config.Database) which holds schema_from.
// Takes options (sessionOptions) which hold the command's choice.
//
// Returns dsn (string) which is the connection string, or empty for the migrations.
// Returns directory (string) which a relative SQLite file in it is relative to.
// Returns err (error) which is a usage error when schema_from names a variable that is
// not set.
func (inv *invocation) schemaConnection(loaded *project, database *config.Database, options sessionOptions) (dsn, directory string, err error) {
	if options.schemaDSN != "" || options.migrationsOnly {
		return options.schemaDSN, inv.directory, nil
	}
	if err := config.RequireExpanded("schema_from", database.SchemaFrom); err != nil {
		return "", "", usageError("database %q: %w", database.Name, err)
	}
	return database.SchemaFrom, loaded.directory(), nil
}

// sessionInput names the migrations and queries of a database.
//
// Takes loaded (*project) which holds the configuration file's location.
// Takes database (*config.Database) which holds the directories.
//
// Returns seagull.Input which reads them from disk.
// Returns error when the query directory the configuration names does not exist.
func (inv *invocation) sessionInput(loaded *project, database *config.Database) (seagull.Input, error) {
	queries, err := inv.queryFiles(loaded, database)
	if err != nil {
		return seagull.Input{}, err
	}
	return seagull.Input{
		Migrations:   os.DirFS(database.Migrations),
		Queries:      queries,
		Package:      database.Go.Package,
		PackagePath:  packageImportPath(database.Go.Out),
		MigrationDir: "",
		QueryDir:     "",
	}, nil
}

// queryFiles opens a database's query directory.
//
// A configuration that leaves queries out names the default directory, db/queries next to
// the configuration file; when that does not exist the database has no queries, so a
// project can generate its models alone. A query directory the configuration names that
// does not exist is an error naming it.
//
// Takes loaded (*project) which holds the configuration file's location.
// Takes database (*config.Database) which holds the query directory.
//
// Returns fs.FS which holds the query files, empty when there are none.
// Returns error which is a usage error when a named query directory does not exist.
func (inv *invocation) queryFiles(loaded *project, database *config.Database) (fs.FS, error) {
	_, err := os.Stat(database.Queries)
	if !errors.Is(err, fs.ErrNotExist) {
		return os.DirFS(database.Queries), nil
	}
	if database.Queries == filepath.Join(loaded.directory(), config.DefaultQueriesDirectory) {
		return fstest.MapFS{}, nil
	}
	return nil, usageError("database %q: the query directory %s does not exist; create it, or correct queries in %s",
		database.Name, inv.display(database.Queries), inv.display(loaded.path))
}

// requireMigrationDirectory checks that a database's migration directory exists, so a
// command that reads the migrations names the directory and the configuration file
// instead of failing to read a directory that is not there.
//
// Takes loaded (*project) which holds the configuration file's location.
// Takes database (*config.Database) which holds the migration directory.
//
// Returns error which is a usage error when the directory does not exist.
func (inv *invocation) requireMigrationDirectory(loaded *project, database *config.Database) error {
	if _, err := os.Stat(database.Migrations); !errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	remedy := "correct migrations in " + inv.display(loaded.path)
	if database.Migrations == filepath.Join(loaded.directory(), config.DefaultMigrationsDirectory) {
		remedy = "set migrations in " + inv.display(loaded.path) + " to the directory that holds them"
	}
	return usageError("database %q: the migration directory %s does not exist; create it, or %s",
		database.Name, inv.display(database.Migrations), remedy)
}

// schemaSource connects to a live database and returns the source that introspects it.
//
// A generator built with it leaves out the tables Seagull's migration and seed runtime
// keeps, by the names the database's migrate section gives them (see WithRuntimeTables),
// and types each view from its definition, as replaying the migrations does.
//
// Takes database (*config.Database) which names the dialect.
// Takes built (dialect.Dialect) which normalises the database's type names.
// Takes dsn (string) which is the connection string.
//
// Returns seagull.SchemaSourcePort which builds the catalogue from the database.
// Returns *sql.DB which the caller closes.
// Returns error when the dialect has no introspection or the database cannot be reached.
func (inv *invocation) schemaSource(ctx context.Context, database *config.Database, built dialect.Dialect, dsn string) (seagull.SchemaSourcePort, *sql.DB, error) {
	if !hasIntrospection(built.Info().Family) {
		return nil, nil, usageError("database %q: dialect %s cannot be introspected; build the schema from migrations instead", database.Name, database.Dialect)
	}
	connection, err := inv.openDatabase(ctx, database, built, dsn)
	if err != nil {
		return nil, nil, err
	}
	return introspector(connection, built), connection, nil
}

// driverFor returns the database/sql driver this binary opens for a database: the one
// registered with WithDriver for the dialect's name, or else for its family.
//
// Takes database (*config.Database) which names the dialect.
// Takes family (string) which is the dialect's Facts.Family.
//
// Returns string which is the driver name.
// Returns error which is a usage error when the binary has no driver for the dialect.
func (inv *invocation) driverFor(database *config.Database, family string) (string, error) {
	if name, found := inv.settings.drivers[database.Dialect]; found {
		return name, nil
	}
	if name, found := inv.settings.drivers[family]; found {
		return name, nil
	}
	if family == dialect_engine_duckdb.Family {
		return "", usageError("%s", "this "+inv.program+" has no DuckDB driver; the stock binary leaves it out because DuckDB needs cgo. "+
			"It can still generate code for DuckDB. To connect, build a custom "+inv.program+" that imports a DuckDB driver "+
			"and passes cli.WithDriver(\"duckdb\", \"duckdb\") (see the custom binaries section of docs/reference/cli.md)")
	}
	return "", usageError("this %[1]s has no database driver for dialect %[2]s; build a custom %[1]s with cli.WithDriver", inv.program, database.Dialect)
}

// openDatabase opens and pings a database. The connection string is registered with the
// error stream's redactor first, so nothing printed afterwards can carry its secrets.
//
// Takes database (*config.Database) which names the dialect.
// Takes built (dialect.Dialect) which gives the dialect's family.
// Takes dsn (string) which is the connection string.
//
// Returns *sql.DB which the caller closes.
// Returns error which is a usage error when there is no driver, or a database error, with
// the connection string redacted, when the database cannot be reached.
func (inv *invocation) openDatabase(ctx context.Context, database *config.Database, built dialect.Dialect, dsn string) (*sql.DB, error) {
	inv.redactor.Add(dsn)
	driverName, err := inv.driverFor(database, built.Info().Family)
	if err != nil {
		return nil, err
	}
	connection, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, databaseError(fmt.Errorf("opening %s: %s", redact.DSN(dsn), redact.Text(err.Error(), dsn)))
	}
	if err := connection.PingContext(ctx); err != nil {
		_ = connection.Close()
		if ctx.Err() != nil {
			return nil, context.Cause(ctx)
		}
		return nil, databaseError(fmt.Errorf("connecting to %s: %s", redact.DSN(dsn), redact.Text(err.Error(), dsn)))
	}
	return connection, nil
}

// assembledDatabase reuses validated options or assembles a manually constructed project.
//
// Takes loaded (*project) which holds the project and any validated settings.
// Takes database (*config.Database) which identifies the database to assemble.
//
// Returns assembledDatabase which holds the dialect and immutable base options.
// Returns error when the settings cannot be assembled.
func (inv *invocation) assembledDatabase(loaded *project, database *config.Database) (assembledDatabase, error) {
	if assembled, found := loaded.assembled[database]; found {
		return assembled, nil
	}
	return inv.assembleDatabase(loaded.config, database)
}

// packageImportPath finds the import path of the package generated into a directory: the
// module path of the nearest go.mod at or above it, followed by the directory's path
// within the module. Only the first maxGoModPrefixBytes bytes of a go.mod are read, which
// hold its module directive.
//
// Takes directory (string) which is the output directory.
//
// Returns string which is the import path, or empty when no go.mod declares a module
// above the directory.
func packageImportPath(directory string) string {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return ""
	}
	for current := absolute; ; current = filepath.Dir(current) {
		if content, found := goModPrefix(current); found {
			return moduleImportPath(declaredModule(content), current, absolute)
		}
		if filepath.Dir(current) == current {
			return ""
		}
	}
}

// goModPrefix reads the start of the go.mod in a directory, through a read-only sandbox
// rooted at the directory.
//
// Takes directory (string) which may hold a go.mod.
//
// Returns []byte which holds at most maxGoModPrefixBytes bytes of the file.
// Returns bool which is false when the directory holds no readable regular go.mod.
func goModPrefix(directory string) ([]byte, bool) {
	sandbox, err := safedisk.NewSandbox(directory, safedisk.ModeReadOnly)
	if err != nil {
		return nil, false
	}
	defer func() { _ = sandbox.Close() }()
	if info, statErr := sandbox.Stat(goModFile); statErr != nil || !info.Mode().IsRegular() {
		return nil, false
	}
	content, err := readPrefix(sandbox, goModFile, maxGoModPrefixBytes)
	return content, err == nil
}

// moduleImportPath joins a module path and a directory's path within the module.
//
// Takes module (string) which is the module path, or empty.
// Takes root (string) which is the module's directory.
// Takes directory (string) which is a directory at or below root.
//
// Returns string which is the directory's import path, or empty when module is.
func moduleImportPath(module, root, directory string) string {
	relative, err := filepath.Rel(root, directory)
	if module == "" || err != nil {
		return ""
	}
	if relative == "." {
		return module
	}
	return module + "/" + filepath.ToSlash(relative)
}

// declaredModule reads the module path a go.mod declares.
//
// Takes content ([]byte) which is the go.mod file.
//
// Returns string which is the module path, or empty when the file declares none.
func declaredModule(content []byte) string {
	for line := range strings.Lines(string(content)) {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 || fields[0] != "module" {
			continue
		}
		if unquoted, err := strconv.Unquote(fields[1]); err == nil {
			return unquoted
		}
		return fields[1]
	}
	return ""
}

// introspector returns the source that introspects an open database of a dialect whose
// family hasIntrospection accepts.
//
// Takes connection (*sql.DB) which is the open database; the caller keeps it.
// Takes built (dialect.Dialect) which normalises the database's type names.
//
// Returns seagull.SchemaSourcePort which builds the catalogue from the database.
func introspector(connection *sql.DB, built dialect.Dialect) seagull.SchemaSourcePort {
	if built.Info().Family == dialect_engine_sqlite.Family {
		return schema_source_sqlite.New(connection, built)
	}
	return schema_source_postgres.New(connection, built)
}

// hasIntrospection reports whether a dialect family can be introspected. DuckDB has no
// introspection provider, so its schema always comes from the migrations.
//
// Takes family (string) which is the dialect family.
//
// Returns bool which is true for the PostgreSQL and SQLite families.
func hasIntrospection(family string) bool {
	return family == dialect_engine_postgres.Family || family == dialect_engine_sqlite.Family
}

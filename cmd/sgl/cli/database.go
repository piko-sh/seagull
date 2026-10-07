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
	"os"
	"strings"

	"seagull.sh/seagull"
	"seagull.sh/seagull/cmd/internal/config"
	"seagull.sh/seagull/cmd/sgl/internal/mysqldsn"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_clickhouse"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_duckdb"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_mysql"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_postgres"
	"seagull.sh/seagull/sdk/dialect/dialect_engine_sqlite"
	"seagull.sh/seagull/sdk/migrate"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_clickhouse"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_cockroachdb"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_duckdb"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_mysql"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_postgres"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_sqlite"
	"seagull.sh/seagull/sdk/migrate/migrate_backend_timescaledb"
)

const (
	// databaseURLVariable is the environment variable that holds the connection string when
	// --dsn is absent.
	databaseURLVariable = "SGL_DATABASE_URL"

	// dsnFlag is the flag that gives the connection string.
	dsnFlag = "dsn"

	// dsnUsage describes the --dsn flag.
	dsnUsage = "the `DSN` to connect to; defaults to $" + databaseURLVariable + ", then migrate.dsn"

	// cockroachDBDialect is the dialect whose migrations need the CockroachDB backend.
	cockroachDBDialect = "cockroachdb"

	// timescaleDBDialect is the dialect whose migrations need the TimescaleDB backend, which
	// runs continuous-aggregate statements outside a transaction.
	timescaleDBDialect = "timescaledb"

	// mariaDBDialect is the MySQL-family dialect whose migrations take the MySQL backend.
	mariaDBDialect = "mariadb"
)

// stockBackends maps the dialect names and families the stock backends serve to the
// backend each gets.
var stockBackends = map[string]MigrationBackendFactory{
	dialect_engine_postgres.Family:   migrate_backend_postgres.New,
	cockroachDBDialect:               migrate_backend_cockroachdb.New,
	timescaleDBDialect:               migrate_backend_timescaledb.New,
	dialect_engine_mysql.Family:      migrate_backend_mysql.New,
	mariaDBDialect:                   migrate_backend_mysql.New,
	dialect_engine_sqlite.Family:     migrate_backend_sqlite.New,
	dialect_engine_clickhouse.Family: migrate_backend_clickhouse.New,
	dialect_engine_duckdb.Family:     migrate_backend_duckdb.New,
}

// connection is an open database with the configuration it belongs to.
type connection struct {
	// dialect is the database's dialect.
	dialect dialect.Dialect

	// database is the open connection pool.
	database *sql.DB

	// configured is the database's configuration.
	configured *config.Database

	// backends holds the migration backends registered with WithMigrationBackend.
	backends map[string]MigrationBackendFactory

	// loaded is the whole configuration.
	loaded *project

	// dsn is the connection string.
	dsn string

	// program is the name the CLI runs as, for the messages that name it.
	program string
}

// Close closes the connection pool.
//
// Returns error when the pool cannot be closed.
func (c *connection) Close() error {
	return c.database.Close()
}

// backend returns the migration backend for the connection's dialect, with the configured
// namespace, history table and lock; see migrationBackend.
//
// Returns migrate.Backend which is ready for NewMigrator and NewSeeder.
// Returns error which is a usage error when the dialect has no migration backend.
func (c *connection) backend() (migrate.Backend, error) {
	info := c.dialect.Info()
	backend, found := migrationBackend(c.configured, &info, c.backends, c.dsn)
	if !found {
		return migrate.Backend{}, usageError("database %q: dialect %s (named %q, family %q) has no migration backend; "+
			"a custom %s registers one with cli.WithMigrationBackend", c.configured.Name, c.configured.Dialect, info.Name, info.Family, c.program)
	}
	return backend, nil
}

// migrateOptions returns the options every migrator and seeder gets: the directive
// prefixes and the logger.
//
// Takes inv (*invocation) which supplies CLI logging and directive settings.
//
// Returns []migrate.Option which holds them.
func (c *connection) migrateOptions(inv *invocation) []migrate.Option {
	prefixes := c.loaded.config.Directives.Prefixes
	return []migrate.Option{
		migrate.WithDirectivePrefixes(prefixes[0], prefixes[1:]...),
		migrate.WithLogger(inv.logger),
	}
}

// connect loads the configuration, picks the one database a migrate, seed or schema diff
// command works on, and connects to it.
//
// Takes database (string) which is the --database value.
// Takes dsn (string) which is the --dsn value.
//
// Returns *connection which the caller closes.
// Returns error which is a usage error when the database or connection string cannot be
// chosen, or a database error when the database cannot be reached.
func (inv *invocation) connect(ctx context.Context, database, dsn string) (*connection, error) {
	loaded, err := inv.loadProject(database)
	if err != nil {
		return nil, err
	}
	if len(loaded.databases) != 1 {
		return nil, usageError("the configuration has %d databases; choose one with --database", len(loaded.databases))
	}
	configured := loaded.databases[0]
	resolved, directory, err := inv.resolveDSN(dsn, loaded, configured)
	if err != nil {
		return nil, err
	}
	built, err := inv.buildDialect(configured)
	if err != nil {
		return nil, err
	}
	resolved = anchorDSN(built.Info().Family, resolved, directory)
	opened, err := inv.openDatabase(ctx, configured, built, resolved)
	if err != nil {
		return nil, err
	}
	return &connection{
		dialect:    built,
		database:   opened,
		configured: configured,
		backends:   inv.settings.backends,
		loaded:     loaded,
		dsn:        resolved,
		program:    inv.program,
	}, nil
}

// resolveDSN chooses the connection string: --dsn, then $SGL_DATABASE_URL, then the
// configuration's migrate.dsn.
//
// Relative SQLite paths from --dsn or $SGL_DATABASE_URL use the working directory. Paths
// from migrate.dsn use the configuration file's directory. An unexpanded ${env:NAME}
// reference in migrate.dsn produces an error naming the unset variable.
//
// Takes flagValue (string) which is the --dsn value.
// Takes loaded (*project) which names the configuration file.
// Takes configured (*config.Database) which holds migrate.dsn.
//
// Returns string which is the connection string.
// Returns string which is the directory a relative SQLite file in it is relative to.
// Returns error which is a usage error when none is set, or migrate.dsn names a variable
// that is not set.
func (inv *invocation) resolveDSN(flagValue string, loaded *project, configured *config.Database) (dsn, directory string, err error) {
	if flagValue != "" {
		return flagValue, inv.directory, nil
	}
	if value, found := os.LookupEnv(databaseURLVariable); found && value != "" {
		return value, inv.directory, nil
	}
	if configured.Migrate.DSN != "" {
		if err := config.RequireExpanded("migrate.dsn", configured.Migrate.DSN); err != nil {
			return "", "", usageError("database %q: %w; pass --dsn or set %s instead", configured.Name, err, databaseURLVariable)
		}
		return configured.Migrate.DSN, loaded.directory(), nil
	}
	return "", "", usageError("database %q: no connection string; pass --dsn, set %s or set migrate.dsn", configured.Name, databaseURLVariable)
}

// migrationBackend selects the configured migration backend for a database.
//
// It looks up the dialect's reported name before its family and applies the namespace,
// history-table and locking options, and dialect_options.ansi_quotes, so migrations and
// seeds split as the generator reads them.
//
// Takes database (*config.Database) which holds the migrate section.
// Takes info (*dialect.Facts) which names the dialect and its family.
// Takes registered (map[string]MigrationBackendFactory) which holds the backends
// registered with WithMigrationBackend, or is nil.
// Takes dsn (string) which is the connection string, or empty when there is none.
//
// Returns migrate.Backend which is ready for NewMigrator and NewSeeder.
// Returns bool which is false when the dialect has no migration backend.
func migrationBackend(database *config.Database, info *dialect.Facts, registered map[string]MigrationBackendFactory, dsn string) (migrate.Backend, bool) {
	factory, found := migrationBackendFor(info, registered)
	if !found {
		return migrate.Backend{}, false
	}
	options := backendOptions(&database.Migrate)
	if info.Family == dialect_engine_mysql.Family {
		options = append(options, migrate.WithDriverMultiStatements(mysqldsn.MultiStatements(dsn)))
	}
	if ansiQuotes := database.DialectOptions.ANSIQuotes; ansiQuotes != nil {
		options = append(options, migrate_backend_mysql.WithANSIQuotes(*ansiQuotes))
	}
	return factory(options...), true
}

// bookkeepingTables lists the tables the migrator and seeder keep for a database, by the
// names its migrate section gives them, which the migrations do not describe.
//
// Takes database (*config.Database) which holds the migrate section.
// Takes info (*dialect.Facts) which names the dialect and its family.
// Takes registered (map[string]MigrationBackendFactory) which holds the backends
// registered with WithMigrationBackend, or is nil.
//
// Returns []string which holds their names in lower case, or nil when the dialect has no
// migration backend and so keeps no such tables.
func bookkeepingTables(database *config.Database, info *dialect.Facts, registered map[string]MigrationBackendFactory) []string {
	backend, found := migrationBackend(database, info, registered, "")
	if !found {
		return nil
	}
	names := backend.Names()
	return []string{
		strings.ToLower(names.History),
		strings.ToLower(names.Seeds),
		strings.ToLower(names.MigrationLock),
		strings.ToLower(names.SeedLock),
	}
}

// backendOptions converts a database's migrate section into backend options.
//
// Takes section (*config.Migrate) which holds the namespace, history table and lock.
//
// Returns []migrate.BackendOption which holds them.
func backendOptions(section *config.Migrate) []migrate.BackendOption {
	options := []migrate.BackendOption{migrate.WithNamespace(section.Namespace)}
	if section.HistoryTable != "" {
		options = append(options, migrate.WithHistoryTable(section.HistoryTable))
	}
	switch section.Lock {
	case "advisory":
		options = append(options, migrate.WithLock(migrate.LockAdvisory))
	case "table":
		options = append(options, migrate.WithLock(migrate.LockTable))
	case "none":
		options = append(options, migrate.WithLock(migrate.LockNone))
	default:
	}
	return options
}

// migrationBackendFor chooses the migration backend of a dialect: one registered for its
// name, the stock one of its name, one registered for its family, then the stock one of
// its family.
//
// Takes info (*dialect.Facts) which names the dialect and its family.
// Takes registered (map[string]MigrationBackendFactory) which holds the backends
// registered with WithMigrationBackend, or is nil.
//
// Returns MigrationBackendFactory which builds the backend.
// Returns bool which is false when neither the name nor the family has one.
func migrationBackendFor(info *dialect.Facts, registered map[string]MigrationBackendFactory) (MigrationBackendFactory, bool) {
	for _, key := range []string{info.Name, info.Family} {
		if key == "" {
			continue
		}
		if factory, found := registered[key]; found {
			return factory, true
		}
		if factory, found := stockBackends[key]; found {
			return factory, true
		}
	}
	return nil, false
}

// migrationFailure classifies a migration or seed error: interruption, an invalid
// configuration or an unknown version, a history that refused a force or repair, or a
// database failure.
//
// Takes err (error) which is the failure.
//
// Returns error which carries the matching exit status.
func migrationFailure(err error) error {
	_, unknown := errors.AsType[*migrate.UnknownMigrationError](err)
	_, later := errors.AsType[*migrate.LaterMigrationError](err)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled):
		return err
	case errors.Is(err, seagull.ErrInvalidConfig), unknown:
		return &exitError{err: err, code: exitUsage}
	case later, errors.Is(err, migrate.ErrRepairOutOfDate):
		return &exitError{err: err, code: exitFindings}
	default:
		return databaseError(err)
	}
}

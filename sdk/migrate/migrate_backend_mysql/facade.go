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

package migrate_backend_mysql

import (
	"seagull.sh/seagull/internal/migrate/sqlexec"
	"seagull.sh/seagull/sdk/migrate"
)

// WithServerWideLockKey enables MySQL's legacy server-wide migration lock key.
//
// Use it while older processes still share the server so all migrators take the same
// lock. Other backends ignore it.
//
// Returns migrate.BackendOption which keeps the lock keys server-wide.
func WithServerWideLockKey() migrate.BackendOption {
	return sqlexec.WithServerWideLockKey()
}

// WithANSIQuotes makes MySQL and MariaDB migration splitting treat double quotes as
// identifier delimiters, matching servers with ANSI_QUOTES enabled. Other backends ignore
// it.
//
// Takes enabled (bool) which is true when the server's sql_mode holds ANSI_QUOTES.
//
// Returns migrate.BackendOption which sets how "..." is read.
func WithANSIQuotes(enabled bool) migrate.BackendOption {
	return sqlexec.WithANSIQuotes(enabled)
}

// New returns the backend for MySQL and MariaDB: ? placeholders, statements split under
// MySQL's lexical rules, DELIMITER lines included, and, by default, GET_LOCK advisory
// locks keyed within the current schema.
//
// MySQL commits implicitly around every DDL statement, so migrations do not run in
// transactions: each statement runs on its own and the history row records the progress,
// so a migration that fails part-way stays dirty and resumes after its last completed
// statement. A seed and its history row commit together in a transaction. Seed files are
// split, as go-sql-driver/mysql runs one statement per call;
// [migrate.WithDriverMultiStatements] runs them whole when the DSN sets
// multiStatements=true. A run works on one session and qualifies the history and seed
// tables with the database current when it began, so a USE in a migration cannot move
// them.
//
// Takes options (...migrate.BackendOption) which set the namespace, history table, lock
// mode (advisory or none), lock key scope, multi-statement driver and, with
// [WithANSIQuotes], whether the server reads "..." as an identifier.
//
// Returns migrate.Backend which is ready for [migrate.NewMigrator], [migrate.NewSeeder]
// or [migrate.NewExecutor].
func New(options ...migrate.BackendOption) migrate.Backend {
	return sqlexec.MySQL(options...)
}

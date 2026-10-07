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

package migrate_backend_postgres

import (
	"seagull.sh/seagull/internal/migrate/sqlexec"
	"seagull.sh/seagull/sdk/migrate"
)

// New returns the backend for PostgreSQL: $N placeholders, transactional DDL and, by
// default, session-level advisory locks.
//
// CockroachDB has no advisory locks; use migrate_backend_cockroachdb.New for it. For
// TimescaleDB use migrate_backend_timescaledb.New, which also recognises the
// continuous-aggregate statements that cannot run inside a transaction. Behind PgBouncer
// in transaction mode use [NewPgBouncer].
//
// Takes options (...migrate.BackendOption) which set the namespace, history table and
// lock mode (advisory, table or none).
//
// Returns migrate.Backend which is ready for [migrate.NewMigrator], [migrate.NewSeeder]
// or [migrate.NewExecutor].
func New(options ...migrate.BackendOption) migrate.Backend {
	return sqlexec.Postgres(options...)
}

// NewPgBouncer returns the backend for PostgreSQL behind PgBouncer, or another pooler, in
// transaction mode, where session-level advisory locks are unavailable: the PostgreSQL
// backend with [migrate.LockTable] as its default lock mode.
//
// Use it when the database handle reaches the server through such a pooler. With a direct
// connection or a session-mode pooler, New's advisory lock needs no idle transaction and
// is the better choice.
//
// The table lock's transaction stays open, idle, for the whole run. It turns off the
// server's idle_in_transaction_session_timeout for itself, and before each migration or
// seed the executor confirms the transaction is still open, returning
// [migrate.ErrLockLost] when it is not. PgBouncer's own idle_transaction_timeout cannot
// be turned off from a session, so it must be zero or longer than the longest migration
// run.
//
// Takes options (...migrate.BackendOption) which set the namespace, history table and
// lock mode.
//
// Returns migrate.Backend which is ready for [migrate.NewMigrator], [migrate.NewSeeder]
// or [migrate.NewExecutor].
func NewPgBouncer(options ...migrate.BackendOption) migrate.Backend {
	return sqlexec.PostgresPgBouncer(options...)
}

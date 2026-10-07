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

package migrate_backend_cockroachdb

import (
	"seagull.sh/seagull/internal/migrate/sqlexec"
	"seagull.sh/seagull/sdk/migrate"
)

// New returns the backend for CockroachDB: PostgreSQL's SQL with a table lock by default,
// since CockroachDB has no advisory lock functions.
//
// The lock transaction turns on enable_durable_locking_for_serializable, so a range split
// or move during a schema change cannot drop the lock; that needs CockroachDB v23.2 or
// later, and an older server fails the lock with [migrate.ErrUnsupportedLock]. The
// transaction refers to the lock table by its descriptor ID, so it holds no lease on the
// database or schema that a migration's CREATE FUNCTION or CREATE SCHEMA would wait for.
// It also turns off idle_in_transaction_session_timeout for itself, as it sits idle for
// the whole run, and before each migration or seed the executor confirms it is still
// open, returning [migrate.ErrLockLost] when it is not.
//
// Migrations do not run in transactions, because CockroachDB does not make schema changes
// atomic with the rest of a transaction: each statement runs on its own, and a migration
// that fails part-way stays dirty and resumes at the statement that failed.
//
// Takes options (...migrate.BackendOption) which set the namespace, history table and
// lock mode (table or none).
//
// Returns migrate.Backend which is ready for [migrate.NewMigrator], [migrate.NewSeeder]
// or [migrate.NewExecutor].
func New(options ...migrate.BackendOption) migrate.Backend {
	return sqlexec.CockroachDB(options...)
}

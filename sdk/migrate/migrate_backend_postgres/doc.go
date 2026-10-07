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

// Package migrate_backend_postgres is the migration backend for PostgreSQL over
// database/sql. [New] returns the [migrate.Backend] to pass to [migrate.NewMigrator] or
// [migrate.NewSeeder].
//
// Migrations run in transactions, as PostgreSQL's DDL is transactional, and a run holds a
// session-level advisory lock, so two processes never migrate at once. A run applies its
// migrations on one session, so a SET in one migration lasts for the later ones of the
// same run.
//
// [NewPgBouncer] is for a connection that reaches the server through PgBouncer, or
// another pooler, in transaction mode, where a session-level advisory lock does not
// survive: it holds a table lock instead. For CockroachDB or TimescaleDB pick
// migrate_backend_cockroachdb or migrate_backend_timescaledb.
//
// The options every backend accepts, such as [migrate.WithNamespace],
// [migrate.WithHistoryTable] and [migrate.WithLock], are in the migrate package.
//
// # Usage
//
//	backend := migrate_backend_postgres.New()
//	migrator, err := migrate.NewMigrator(database, backend, files, "migrations")
package migrate_backend_postgres

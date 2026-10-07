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

// Package migrate_backend_cockroachdb is the migration backend for CockroachDB over
// database/sql. [New] returns the [migrate.Backend] to pass to [migrate.NewMigrator] or
// [migrate.NewSeeder].
//
// CockroachDB has no advisory locks, so runs are serialised with a lock table. Migrations
// do not run in transactions, because CockroachDB does not make schema changes atomic
// with the rest of a transaction: a migration that fails part-way stays dirty and resumes
// at the statement that failed.
//
// The options every backend accepts, such as [migrate.WithNamespace],
// [migrate.WithHistoryTable] and [migrate.WithLock], are in the migrate package.
//
// # Usage
//
//	backend := migrate_backend_cockroachdb.New()
//	migrator, err := migrate.NewMigrator(database, backend, files, "migrations")
package migrate_backend_cockroachdb

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

// Package sqlexec runs migrations and seeds over database/sql.
//
// Choose a built-in Backend for the database engine from its migrate_backend_* provider,
// or use CustomBackend to supply its SQL and locking rules. NewMigrator and NewSeeder
// create the corresponding services.
//
// Migrations use transactions where the engine supports them. Otherwise, statement
// progress is recorded so an interrupted run can resume. Non-transactional statements
// should be idempotent because a crash can cause the last statement to run again.
//
// On PostgreSQL, TimescaleDB and CockroachDB a run applies its migrations (or seeds) on
// one database session, so a SET in one migration, such as SET search_path, lasts for the
// later migrations of the same run, as the generator assumes when it replays them. The
// history and seed tables are qualified by the schema that was current when the run
// began, so such a SET cannot move them, and the session is closed when the run ends, so
// the setting reaches neither the application's queries nor a later run. A migration
// applied by a later run therefore starts from the search path its connection gives; a
// migration that depends on a path should set it itself. MySQL and MariaDB runs work the
// same way with the database DATABASE() names, so a USE in a migration moves the later
// statements of the run but not the history and seed tables.
package sqlexec

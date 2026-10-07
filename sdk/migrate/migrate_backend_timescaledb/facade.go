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

package migrate_backend_timescaledb

import (
	"seagull.sh/seagull/internal/migrate/sqlexec"
	"seagull.sh/seagull/sdk/migrate"
)

// New returns the backend for TimescaleDB: PostgreSQL's, reading migrations with
// TimescaleDB's lexical rules, so that a migration which creates a continuous aggregate
// with data or calls refresh_continuous_aggregate runs outside a transaction, as
// TimescaleDB requires.
//
// Use it in place of migrate_backend_postgres.New for a TimescaleDB database; with no
// continuous-aggregate statements the two behave alike.
//
// Takes options (...migrate.BackendOption) which set the namespace, history table and
// lock mode (advisory, table or none).
//
// Returns migrate.Backend which is ready for [migrate.NewMigrator], [migrate.NewSeeder]
// or [migrate.NewExecutor].
func New(options ...migrate.BackendOption) migrate.Backend {
	return sqlexec.TimescaleDB(options...)
}

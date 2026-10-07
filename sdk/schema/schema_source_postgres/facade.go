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

package schema_source_postgres

import (
	"database/sql"

	"seagull.sh/seagull/internal/schema/postgres"
	"seagull.sh/seagull/sdk/schema"
)

// New creates a source that reads the schema of a live PostgreSQL, CockroachDB or
// TimescaleDB database.
//
// Takes database (*sql.DB) which is the connection pool to query; the caller owns it.
// Takes typeNormaliser (schema.TypeNormaliserPort) which converts the database's type
// names to SQL types; pass the dialect the generator uses, such as
// dialect_engine_postgres.New().
//
// Returns schema.SchemaSourcePort which is ready to build catalogues; its BuildCatalogue
// fails when either argument is nil.
func New(database *sql.DB, typeNormaliser schema.TypeNormaliserPort) schema.SchemaSourcePort {
	return postgres.New(database, typeNormaliser)
}

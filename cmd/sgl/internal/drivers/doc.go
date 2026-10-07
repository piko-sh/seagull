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

// Package drivers links the database/sql drivers the stock sgl command ships with, and
// maps each dialect family to the driver that connects to it.
//
// Importing it registers the drivers, and [Options] returns the cli options that select
// them. The cli package links no database driver itself, so a custom binary imports the
// drivers it needs and passes cli.WithDriver for each.
//
// The drivers are all pure Go, so a binary that uses them builds with CGO_ENABLED=0: pgx
// for the PostgreSQL family (PostgreSQL, CockroachDB, TimescaleDB), go-sql-driver/mysql
// for MySQL and MariaDB, modernc.org/sqlite for SQLite, and clickhouse-go for ClickHouse.
// DuckDB has no pure-Go driver, so it has none here.
package drivers

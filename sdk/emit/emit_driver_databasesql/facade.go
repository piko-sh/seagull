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

package emit_driver_databasesql

import (
	"seagull.sh/seagull/internal/emit/databasesql"
	"seagull.sh/seagull/sdk/dialect"
	"seagull.sh/seagull/sdk/emit"
)

// Option configures the emitter New builds; WithDriver and WithPrepared make one.
type Option = databasesql.Option

// PreparesStatements reports whether the database/sql emitter can generate the
// prepared-statement cache, prepared.go, for a dialect's driver: it cannot for a driver
// that prepares only some statements, such as ClickHouse's database/sql driver, which
// prepares only INSERT statements, since the cache could not prepare the generated
// queries.
//
// Takes info (*dialect.Facts) which describes the dialect, whose drivers choose the
// profile unless driver names one. PreparesStatements does not change it.
// Takes driver (string) which is the module of the driver behind database/sql, as
// WithDriver takes it, or empty for the first of the dialect's drivers the emitter knows.
//
// Returns bool which is true when WithPrepared(true) works for the dialect and driver.
func PreparesStatements(info *dialect.Facts, driver string) bool {
	return databasesql.PreparesStatements(info, driver)
}

// New creates the database/sql emitter, ready to pass to seagull.WithEmitter. With no
// options it binds and scans for the first of each dialect's drivers it knows, and writes
// prepared.go wherever that driver can use it.
//
// Takes options (...Option) which configure the emitter.
//
// Returns emit.EmitterPort which is ready to emit Go source code.
func New(options ...Option) emit.EmitterPort {
	return databasesql.New(options...)
}

// WithDriver names the driver behind database/sql the generated code runs on, by the
// import path of its Go module, such as github.com/jackc/pgx/v5, instead of the first of
// the dialect's drivers the emitter knows. A driver the emitter does not know gets the
// general profile, which keeps the built-in type defaults.
//
// Takes driver (string) which is the driver's module path.
//
// Returns Option which fixes the driver.
func WithDriver(driver string) Option {
	return databasesql.WithDriver(driver)
}

// WithPrepared sets whether the emitter generates prepared.go, a PreparedDBTX that
// prepares every static query once and reuses the statements.
//
// It is on by default, except for a driver that prepares only some statements, which gets
// no cache; asking for it there with WithPrepared(true) is a problem the emitter's Check
// reports. PreparesStatements says which drivers can have it. The sgl command passes
// WithPrepared(false) unless go.prepared is set.
//
// Takes enabled (bool) which is false to leave the prepared-statement cache out.
//
// Returns Option which sets the choice.
func WithPrepared(enabled bool) Option {
	return databasesql.WithPrepared(enabled)
}

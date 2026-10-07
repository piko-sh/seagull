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

package timescaledb

import (
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/internal/dialect/timescaledb/lexical"
)

// New creates the TimescaleDB dialect: the postgres dialect configured with TimescaleDB's
// statement extensions, type aliases and functions, named timescaledb and in the postgres
// family.
//
// TimescaleDB-specific DDL parsing is performed by the registered StatementExtensionPort
// for shapes the postgres classifier does not recognise (CREATE HYPERTABLE,
// continuous-aggregate materialised views, ALTER ... SET compression bodies, CALL
// refresh_continuous_aggregate, SELECT policy and hypertable-management calls). A
// PostParseHook covers the modern CREATE TABLE foo (...) WITH (tsdb.hypertable, ...) form
// because the host postgres parser already routes that statement through its built-in
// CREATE TABLE handler; the hook lifts the trailing reloption body without
// re-implementing column parsing. Statements are split under TimescaleDB's own lexical
// rules, which add the continuous-aggregate statements that cannot run in a transaction.
// The toolkit's accessor arrows, such as stats_agg(x) -> average(), are typed as the
// accessor's function applied to the aggregate state (postgres.WithAccessorArrows).
//
// Migrations run through the TimescaleDB backend (internal/migrate/sqlexec.TimescaleDB),
// which keeps postgres transactions, advisory locks and history tables and runs the
// statements TimescaleDB cannot run in a transaction outside one.
//
// Takes options (...postgres.Option) which apply after the TimescaleDB wiring and add to
// it, as postgres.Option describes: a caller's extra functions, types, hooks and
// statement extensions join TimescaleDB's rather than replacing them. Use
// postgres.WithMaxTokensPerStatement or postgres.WithMaxParseDepth to tune the parser
// limits.
//
// Returns *postgres.Dialect which is the TimescaleDB dialect.
func New(options ...postgres.Option) *postgres.Dialect {
	return postgres.New(append([]postgres.Option{
		postgres.WithName("timescaledb"),
		postgres.WithLexicalRules(lexical.Rules()),
		postgres.WithExtraTypes(timescaleDBTypes),
		postgres.WithExtraTypes(accessorTypes),
		postgres.WithExtraFunctions(registerTimescaleDBFunctions),
		postgres.WithStatementExtensions(tsExtension{}),
		postgres.WithPostParseHook(timescaleCreateTableHypertableHook),
		postgres.WithAccessorArrows(accessorTypeNames...),
	}, options...)...)
}

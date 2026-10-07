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

package dialect_engine_cockroachdb

import (
	dialectport "seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/cockroachdb"
	"seagull.sh/seagull/internal/dialect/postgres"
	"seagull.sh/seagull/sdk/dialect"
)

// Option is a setting [New] applies to the CockroachDB dialect. It is the PostgreSQL
// dialect's dialect_engine_postgres.Option, so the options of either package apply.
//
// Options apply in the order given, after the settings New makes for CockroachDB, and
// each sets a single value, the last one winning.
type Option = postgres.Option

// New creates the CockroachDB dialect: the PostgreSQL dialect, named cockroachdb and in
// the postgres family, with CockroachDB's types, functions and schema rules, hidden
// columns included. It rejects the PostgreSQL syntax CockroachDB does not support.
//
// Takes options (...Option) which apply after the CockroachDB settings, such as
// WithSearchPath or the parser limits.
//
// Returns dialect.Dialect which is the CockroachDB dialect.
func New(options ...Option) dialect.Dialect {
	return dialectport.Wrap(cockroachdb.New(options...))
}

// WithName sets the name the dialect reports in its facts.
//
// New names it cockroachdb; a caller's WithName replaces that. The family stays postgres,
// so type mappings and drivers keep resolving.
//
// Takes name (string) which is the dialect name to report.
//
// Returns Option which applies the name.
func WithName(name string) Option {
	return postgres.WithName(name)
}

// WithSearchPath sets the initial schema search order, defaulting to public. Migrations
// create unqualified objects in the first existing schema, and generated names omit
// schemas on the path.
//
// Names follow unquoted identifier rules; "$user" is omitted, and migrations may later
// change the path.
//
// Takes schemas (...string) which are the schemas, in order; none keeps the default.
//
// Returns Option which applies the search path.
func WithSearchPath(schemas ...string) Option {
	return postgres.WithSearchPath(schemas...)
}

// WithMaxParseDepth sets how deep a statement may nest, in subqueries, parentheses and
// other recursive constructs, before the parser refuses it.
//
// Deeply nested input could otherwise overflow the goroutine stack with a fatal error
// that no recover can catch. The default, dialect_registry.DefaultMaxParseDepth, leaves
// realistic queries unaffected; lower it to harden against hostile input or raise it for
// unusually nested generated queries. A statement that nests deeper fails with an error
// wrapping seagull.ErrLimit.
//
// Takes depth (int) which is the maximum nesting depth; a value below 1 keeps the
// default, and one above dialect_registry.MaxParseDepthCeiling is held at the ceiling.
//
// Returns Option which applies the depth cap to the dialect.
func WithMaxParseDepth(depth int) Option {
	return postgres.WithMaxParseDepth(depth)
}

// WithMaxTokensPerStatement sets how many tokens one statement may hold.
//
// The budget bounds the memory and work an adversarially long statement costs: a script
// is tokenised no further into a statement than the budget, and a statement over it that
// the parser would walk fails with an error wrapping seagull.ErrLimit. The default,
// dialect_registry.DefaultMaxTokensPerStatement, leaves realistic statements unaffected,
// generated SQL with large IN lists included. A statement the parser does not walk, such
// as a large seed INSERT inside a migration, is not refused.
//
// Takes limit (int) which is the maximum token count; a value below 1 keeps the default,
// and one above dialect_registry.MaxTokensPerStatementCeiling is held at the ceiling.
//
// Returns Option which applies the token budget to the dialect.
func WithMaxTokensPerStatement(limit int) Option {
	return postgres.WithMaxTokensPerStatement(limit)
}

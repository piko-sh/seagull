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

package dialect_engine_mariadb

import (
	dialectport "seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/mariadb"
	"seagull.sh/seagull/internal/dialect/mysql"
	"seagull.sh/seagull/sdk/dialect"
)

// Option is a setting [New] applies to the MariaDB dialect. It is the MySQL dialect's
// dialect_engine_mysql.Option, so the options of either package apply.
//
// Options apply in the order given, after the settings New makes for MariaDB, and each
// sets a single value, the last one winning.
type Option = mysql.Option

// New creates the MariaDB dialect: the MySQL dialect, named mariadb and in the mysql
// family, with MariaDB's types, functions and syntax rules.
//
// Takes options (...Option) which apply after the MariaDB settings, overriding them where
// they set the same value.
//
// Returns dialect.Dialect which is the MariaDB dialect.
func New(options ...Option) dialect.Dialect {
	return dialectport.Wrap(mariadb.New(options...))
}

// WithName sets the name the dialect reports in its facts.
//
// New names it mariadb; a caller's WithName replaces that. The family stays mysql, so
// type mappings and drivers keep resolving.
//
// Takes name (string) which is the dialect name to report.
//
// Returns Option which applies the name.
func WithName(name string) Option {
	return mysql.WithName(name)
}

// WithANSIQuotes sets whether the server runs with ANSI_QUOTES in its sql_mode.
//
// MySQL's and MariaDB's default mode reads "..." as a string, as in CONCAT(a, ":", b), so
// the dialect does too unless this option turns ANSI_QUOTES on, which reads "..." as a
// quoted identifier, as standard SQL does. The lexical rules in the dialect's facts then
// read it so too; give migrate_backend_mysql.WithANSIQuotes the same value, so migrations
// split as the dialect reads them.
//
// Takes enabled (bool) which records whether the mode holds ANSI_QUOTES.
//
// Returns Option which applies the mode.
func WithANSIQuotes(enabled bool) Option {
	return mysql.WithANSIQuotes(enabled)
}

// WithReturning sets the statements that accept a RETURNING clause, replacing any set
// before. New already sets INSERT and DELETE, as MariaDB accepts it on them.
//
// Takes statements (dialect.DataChange) which is the set of statements, or zero for none.
//
// Returns Option which installs the set on a dialect.
func WithReturning(statements dialect.DataChange) Option {
	return mysql.WithReturning(statements)
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
	return mysql.WithMaxParseDepth(depth)
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
	return mysql.WithMaxTokensPerStatement(limit)
}

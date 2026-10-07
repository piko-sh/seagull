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

package dialect_engine_clickhouse

import (
	dialectport "seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/clickhouse"
	"seagull.sh/seagull/sdk/dialect"
)

// Family is the family name the [dialect.Facts] of ClickHouse report. Tools such as the
// sgl command key their own choices on it, such as which migration backend to use.
const Family = clickhouse.Family

// Option is a setting [New] applies to the ClickHouse dialect.
//
// Options apply in the order given, and each sets a single value, the last one winning.
type Option = clickhouse.Option

// New creates the ClickHouse dialect, named clickhouse and in the clickhouse family.
//
// An error decoding the built-in function catalogue does not stop New: every statement
// the dialect replays or analyses reports it.
//
// Takes options (...Option) which set the dialect's name, the join_use_nulls and
// group_by_use_nulls settings it assumes, and its parser limits.
//
// Returns dialect.Dialect which is the configured dialect.
func New(options ...Option) dialect.Dialect {
	return dialectport.Wrap(clickhouse.New(options...))
}

// WithName sets the name the dialect reports in its facts. The family stays clickhouse,
// so type mappings and drivers keep resolving.
//
// Takes name (string) which is the dialect name to report.
//
// Returns Option which applies the name.
func WithName(name string) Option {
	return clickhouse.WithName(name)
}

// WithJoinUseNulls sets the join_use_nulls value assumed during analysis.
//
// Enable it when unmatched outer-join rows should produce NULLs. The default is false,
// and a query's SETTINGS clause takes precedence.
//
// Takes enabled (bool) which is the assumed join_use_nulls setting.
//
// Returns Option which applies the setting.
func WithJoinUseNulls(enabled bool) Option {
	return clickhouse.WithJoinUseNulls(enabled)
}

// WithGroupByUseNulls sets the group_by_use_nulls value assumed during analysis.
//
// Enable it when omitted subtotal keys should be nullable. The default is false, and a
// query's SETTINGS clause takes precedence.
//
// Takes enabled (bool) which is the assumed group_by_use_nulls setting.
//
// Returns Option which applies the setting.
func WithGroupByUseNulls(enabled bool) Option {
	return clickhouse.WithGroupByUseNulls(enabled)
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
	return clickhouse.WithMaxParseDepth(depth)
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
	return clickhouse.WithMaxTokensPerStatement(limit)
}

// WithMaxTypeParseDepth sets the maximum wrapper nesting the type-name parser accepts.
//
// Type names such as Array(Array(Tuple(...))) are parsed recursively, so unbounded
// nesting would otherwise exhaust the goroutine stack. The default is also the most it
// can be, dialect.MaxTypeDepth, the depth to which Seagull follows a type's parts, such
// as when it checks that clickhouse-go can read a result; lower it to harden against
// hostile input. A type that nests deeper fails with an error wrapping seagull.ErrLimit.
//
// Takes depth (int) which is the maximum wrapper nesting; a value below 1 keeps the
// default, and one above dialect.MaxTypeDepth is held at dialect.MaxTypeDepth.
//
// Returns Option which applies the type depth cap to the dialect.
func WithMaxTypeParseDepth(depth int) Option {
	return clickhouse.WithMaxTypeParseDepth(depth)
}

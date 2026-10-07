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

package dialect_registry

import (
	dialectport "seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/dialectkit"
	"seagull.sh/seagull/internal/dialect/dialects"
	"seagull.sh/seagull/sdk/dialect"
)

const (
	// DefaultMaxParseDepth is how deep a statement may nest, in subqueries, parentheses and
	// other recursive constructs, when no depth is set.
	//
	// Set the limit with Options.MaxParseDepth or an engine provider's WithMaxParseDepth.
	DefaultMaxParseDepth = dialectkit.DefaultMaxParseDepth

	// DefaultMaxTokensPerStatement is how many tokens one statement may hold when no budget
	// is set.
	//
	// Set the limit with Options.MaxTokensPerStatement or an engine provider's
	// WithMaxTokensPerStatement.
	DefaultMaxTokensPerStatement = dialectkit.DefaultMaxTokensPerStatement

	// MaxParseDepthCeiling is the deepest parse depth a dialect accepts. A deeper limit
	// would let a nested statement exhaust the goroutine's stack before the limit stopped
	// it.
	MaxParseDepthCeiling = dialectkit.MaxParseDepthCeiling

	// MaxTokensPerStatementCeiling is the largest per-statement token budget a dialect
	// accepts.
	MaxTokensPerStatementCeiling = dialectkit.MaxTokensPerStatementCeiling
)

var (
	// ErrUnknownDialect is returned by New for a name that is not a built-in dialect. It
	// wraps seagull.ErrInvalidConfig.
	ErrUnknownDialect = dialects.ErrUnknownDialect

	// ErrInvalidOption is returned by New when a limit is negative or above its ceiling, the
	// name is blank or another family's built-in name, or an option does not apply to the
	// named dialect. It wraps seagull.ErrInvalidConfig.
	ErrInvalidOption = dialects.ErrInvalidOption
)

// Options and the problems New finds with them.
type (
	// Options is the plain-data configuration New applies to a dialect. The zero value keeps
	// every default.
	Options = dialects.Options

	// OptionError reports one option New refuses: a limit that is negative or above its
	// ceiling, a name that is blank or another family's built-in name, or an option set for
	// a dialect it does not apply to.
	//
	// New joins the problems into one error. Each OptionError wraps ErrInvalidOption, and
	// OptionErrors extracts the individual problems.
	OptionError = dialects.OptionError
)

// Names returns the names New accepts, sorted.
//
// Returns []string which is a fresh, sorted list of the built-in dialect names.
func Names() []string {
	return dialects.Names()
}

// New constructs the built-in dialect with the given name.
//
// Takes name (string) which is one of Names, such as postgres or sqlite.
// Takes options (Options) which configure the dialect.
//
// Returns dialect.Dialect which is the configured dialect, or the zero Dialect with an
// error.
// Returns error which wraps ErrUnknownDialect, and lists the known names, when name is
// not a built-in dialect, or wraps ErrInvalidOption when a limit is negative or above its
// ceiling, the name is blank or another family's built-in name, or an option does not
// apply to the dialect.
func New(name string, options Options) (dialect.Dialect, error) {
	built, err := dialects.New(name, options)
	return dialectport.Wrap(built), err
}

// OptionErrors finds every OptionError in an error New returned.
//
// Takes err (error) which is the error, or nil.
//
// Returns []*OptionError which holds them in the order New found them, or nil when the
// error holds none.
func OptionErrors(err error) []*OptionError {
	return dialects.OptionErrors(err)
}

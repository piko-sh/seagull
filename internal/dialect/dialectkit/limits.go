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

package dialectkit

const (
	// DefaultMaxParseDepth is how deep a statement may nest, in subqueries, parentheses and
	// other recursive constructs, when a dialect sets no depth of its own.
	DefaultMaxParseDepth = 256

	// DefaultMaxTokensPerStatement is how many tokens one statement may hold when a dialect
	// sets no budget of its own.
	DefaultMaxTokensPerStatement = 100_000

	// MaxParseDepthCeiling is the deepest parse depth a dialect accepts. A deeper limit
	// would let a nested statement exhaust the goroutine's stack before the limit stopped
	// it.
	MaxParseDepthCeiling = 4_096

	// MaxTokensPerStatementCeiling is the largest per-statement token budget a dialect
	// accepts.
	MaxTokensPerStatementCeiling = 1_000_000
)

// Limits holds a dialect's parse limits. A field that is not positive selects the
// default, and one above its ceiling is held at the ceiling.
type Limits struct {
	// MaxParseDepth caps how deep a statement may nest; zero selects DefaultMaxParseDepth.
	MaxParseDepth int

	// MaxTokensPerStatement caps how many tokens one statement may hold; zero selects
	// DefaultMaxTokensPerStatement.
	MaxTokensPerStatement int
}

// ParseDepth returns the parse depth limit in force.
//
// Returns int which is MaxParseDepth, held at MaxParseDepthCeiling, or
// DefaultMaxParseDepth when it is not positive.
func (limits Limits) ParseDepth() int {
	if limits.MaxParseDepth > 0 {
		return min(limits.MaxParseDepth, MaxParseDepthCeiling)
	}
	return DefaultMaxParseDepth
}

// TokensPerStatement returns the per-statement token budget in force.
//
// Returns int which is MaxTokensPerStatement, held at MaxTokensPerStatementCeiling, or
// DefaultMaxTokensPerStatement when it is not positive.
func (limits Limits) TokensPerStatement() int {
	if limits.MaxTokensPerStatement > 0 {
		return min(limits.MaxTokensPerStatement, MaxTokensPerStatementCeiling)
	}
	return DefaultMaxTokensPerStatement
}

// CheckTokens reports a statement whose token count exceeds the budget in force, as
// CheckTokenBudget does.
//
// Takes count (int) which is the statement's token count.
//
// Returns error which wraps ErrTokenBudget, or nil when the statement fits.
func (limits Limits) CheckTokens(count int) error {
	return CheckTokenBudget(count, limits.TokensPerStatement())
}

// WithMaxParseDepth returns the limits with a new parse depth limit.
//
// Takes depth (int) which is the new limit; a value below 1 leaves the limits as they
// are, and one above MaxParseDepthCeiling is held at the ceiling.
//
// Returns Limits which holds the new limit.
func (limits Limits) WithMaxParseDepth(depth int) Limits {
	if depth < 1 {
		return limits
	}
	limits.MaxParseDepth = min(depth, MaxParseDepthCeiling)
	return limits
}

// WithMaxTokensPerStatement returns the limits with a new per-statement token budget.
//
// Takes count (int) which is the new budget; a value below 1 leaves the limits as they
// are, and one above MaxTokensPerStatementCeiling is held at the ceiling.
//
// Returns Limits which holds the new budget.
func (limits Limits) WithMaxTokensPerStatement(count int) Limits {
	if count < 1 {
		return limits
	}
	limits.MaxTokensPerStatement = min(count, MaxTokensPerStatementCeiling)
	return limits
}

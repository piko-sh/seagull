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

package app

import (
	"context"
	"fmt"
	"slices"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/namespace"
)

// FormatDirectives rewrites the directives of a SQL file into their canonical form,
// reading the file with neutral lexical rules, for a caller that has no dialect at hand;
// FormatDirectivesFor reads it as a dialect writes SQL, as the sgl fmt command does.
//
// A directive written with an alias prefix is rewritten to the canonical prefix, and the
// spacing inside directive comment lines is normalised: one space after the comment
// opener, single spaces between tokens, none inside brackets or before a comma, and one
// after a comma. Continuation lines of a multi-line directive keep their indentation. SQL
// bodies, prose comments, quoted directive values and text after a directive's closing
// parenthesis are never changed, and line endings are preserved. Formatting is
// idempotent. The neutral rules read '...' and "..." quotes in which a doubled quote
// stands for one, `...` identifiers, $$...$$ and $tag$...$tag$ bodies, and -- and /* */
// comments.
//
// Takes source ([]byte) which is the SQL file content.
// Takes canonical (string) which is the canonical directive prefix, such as sgl.
// Takes aliases (...string) which are the prefixes to rewrite to canonical, such as
// seagull.
//
// Returns []byte which is the formatted content.
// Returns error which wraps ErrInvalidPrefix when a prefix has the wrong shape or
// repeats.
func FormatDirectives(source []byte, canonical string, aliases ...string) ([]byte, error) {
	prefixes, err := formatPrefixes(canonical, aliases)
	if err != nil {
		return nil, err
	}
	return []byte(syntax.FormatSource(string(source), syntax.NeutralLexicon(), prefixes)), nil
}

// FormatDirectivesFor formats SQL directives using the dialect's lexical rules.
//
// Text inside quoted values or block comments is preserved, and only the dialect's line
// comments can contain directives.
//
// Takes engine (dialect.DialectPort) which says how the file writes SQL.
// Takes source ([]byte) which is the SQL file content.
// Takes canonical (string) which is the canonical directive prefix, such as sgl.
// Takes aliases (...string) which are the prefixes to rewrite to canonical, such as
// seagull.
//
// Returns []byte which is the formatted content.
// Returns error which wraps ErrInvalidConfig for a nil dialect or an incomplete Info,
// ErrInvalidPrefix when a prefix has the wrong shape or repeats, or ErrPanic when the
// dialect's Info panicked.
func FormatDirectivesFor(engine dialect.DialectPort, source []byte, canonical string, aliases ...string) ([]byte, error) {
	if engine == nil {
		return nil, fmt.Errorf("FormatDirectivesFor needs a dialect: %w", fault.ErrInvalidConfig)
	}
	info, err := dialectInfo(context.Background(), engine)
	if err != nil {
		return nil, err
	}
	if err := validateDialectInfo(&info); err != nil {
		return nil, err
	}
	prefixes, err := formatPrefixes(canonical, aliases)
	if err != nil {
		return nil, err
	}
	lexicon := syntax.Lexicon{LinePrefix: info.Comments.LinePrefix, Rules: info.Lexical}
	return []byte(syntax.FormatSource(string(source), lexicon, prefixes)), nil
}

// formatPrefixes checks the prefixes a formatter rewrites.
//
// Takes canonical (string) which is the canonical directive prefix.
// Takes aliases ([]string) which are the prefixes rewritten to it.
//
// Returns namespace.Set which holds them.
// Returns error which wraps ErrInvalidPrefix when a prefix has the wrong shape or
// repeats.
func formatPrefixes(canonical string, aliases []string) (namespace.Set, error) {
	prefixes := namespace.Set{Canonical: canonical, Aliases: slices.Clone(aliases), Foreign: nil}
	for index, prefix := range prefixes.Accepted() {
		if !namespace.ValidPrefix(prefix) {
			return namespace.Set{}, fmt.Errorf("%w: directive prefix %q must match [a-z][a-z0-9_]{0,31}", fault.ErrInvalidPrefix, prefix)
		}
		if slices.Contains(prefixes.Accepted()[:index], prefix) {
			return namespace.Set{}, fmt.Errorf("%w: directive prefix %q repeats", fault.ErrInvalidPrefix, prefix)
		}
	}
	return prefixes, nil
}

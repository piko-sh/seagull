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

package postgres

import (
	"strings"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/dialect/dialectkit"
)

const (
	// distinctOnMismatch is the engine's refusal of an ORDER BY whose leading keys are not
	// the DISTINCT ON keys.
	distinctOnMismatch = "SELECT DISTINCT ON expressions must match initial ORDER BY expressions"
)

// checkDistinctOnOrder records a syntax error unless the leading keys of an ORDER BY are
// the DISTINCT ON keys, in any order, until every one of them is matched, as the engine
// requires.
//
// An engine built WithDistinctOnColumnsOnly matches only column references, so an
// expression key never matches. A key written as a position or a select-list alias leaves
// the check to the engine, which matches it through the item it names. A key repeated in
// either list counts once, as the engine compares the select items the keys name.
//
// Takes start (int) which is the index of the ORDER keyword.
// Takes end (int) which is the index past the clause.
// Takes outputs ([]ast.RawOutputColumn) which is the select list.
func (p *parser) checkDistinctOnOrder(start, end int, outputs []ast.RawOutputColumn) {
	unmatched := make(map[string]bool, len(p.distinctOnKeys))
	matched := make(map[string]bool, len(p.distinctOnKeys))
	remaining := 0
	for _, key := range p.distinctOnKeys {
		if _, seen := unmatched[key]; seen {
			continue
		}
		remaining++
		unmatched[key] = !p.distinctOnColumnsOnly || isColumnText(key)
	}
	for _, item := range orderByItems(p.Tokens[start:end]) {
		if remaining == 0 {
			return
		}
		key := canonicalTokens(item)
		if namesSelectItem(item, outputs) {
			return
		}
		if matched[key] {
			continue
		}
		if !unmatched[key] {
			p.recordDistinctOnMismatch(item[0])
			return
		}
		unmatched[key] = false
		matched[key] = true
		remaining--
	}
}

// recordDistinctOnMismatch records the engine's refusal at the ORDER BY key that breaks
// the match.
//
// Takes at (token) which is the key's first token.
func (p *parser) recordDistinctOnMismatch(at token) {
	_ = p.recordSyntaxError(dialectkit.NewParseError(at, distinctOnMismatch))
}

// orderByItems splits an ORDER BY clause's tokens into its keys, each without its
// direction, NULLS placement or USING operator.
//
// Takes tokens ([]token) which run from ORDER to the end of the clause.
//
// Returns [][]token which holds each key's tokens.
func orderByItems(tokens []token) [][]token {
	if len(tokens) < 2 {
		return nil
	}
	var items [][]token
	depth, itemStart := 0, 2
	for index := 2; index < len(tokens); index++ {
		depth += nestingChange(tokens[index].Kind)
		if depth == 0 && tokens[index].Kind == tokenComma {
			items = appendSortKey(items, tokens[itemStart:index])
			itemStart = index + 1
		}
	}
	return appendSortKey(items, tokens[itemStart:])
}

// nestingChange says how a token changes the depth of brackets.
//
// Takes kind (tokenKind) which is the token's kind.
//
// Returns int which is 1 for an opening bracket, -1 for a closing one and 0 otherwise.
func nestingChange(kind tokenKind) int {
	if kind == tokenLeftParen || kind == tokenLeftBracket {
		return 1
	}
	if kind == tokenRightParen || kind == tokenRightBracket {
		return -1
	}
	return 0
}

// appendSortKey adds a key, without its modifiers, to the keys found so far, unless it is
// empty.
//
// Takes items ([][]token) which holds the keys so far.
// Takes item ([]token) which is the key with its modifiers.
//
// Returns [][]token which holds the keys.
func appendSortKey(items [][]token, item []token) [][]token {
	if key := trimSortModifiers(item); len(key) > 0 {
		return append(items, key)
	}
	return items
}

// trimSortModifiers drops a key's trailing direction, NULLS placement and USING operator.
//
// Takes item ([]token) which is the key with its modifiers.
//
// Returns []token which is the key alone.
func trimSortModifiers(item []token) []token {
	for index, current := range item {
		if current.IsKeyword(tokenIdentifier, "ASC") || current.IsKeyword(tokenIdentifier, "DESC") ||
			current.IsKeyword(tokenIdentifier, "NULLS") || current.IsKeyword(tokenIdentifier, "USING") {
			return item[:index]
		}
	}
	return item
}

// namesSelectItem reports whether an ORDER BY key is a position or the alias of a
// computed select item, which the engine resolves to that item.
//
// Takes item ([]token) which is the key.
// Takes outputs ([]ast.RawOutputColumn) which is the select list.
//
// Returns bool which is true for such a key.
func namesSelectItem(item []token, outputs []ast.RawOutputColumn) bool {
	if len(item) != 1 {
		return false
	}
	if item[0].Kind == tokenNumber {
		return true
	}
	for _, output := range outputs {
		if output.Expression != nil && output.ColumnName == "" && strings.EqualFold(output.Name, item[0].Value) {
			return true
		}
	}
	return false
}

// canonicalTokens writes tokens as one comparable string: unquoted words folded to lower
// case, other tokens as written, separated by single spaces.
//
// Takes tokens ([]token) which are the tokens.
//
// Returns string which is the canonical text.
func canonicalTokens(tokens []token) string {
	parts := make([]string, 0, len(tokens))
	for _, current := range tokens {
		if current.Kind == tokenIdentifier && !current.Quoted {
			parts = append(parts, strings.ToLower(current.Value))
			continue
		}
		parts = append(parts, current.Raw)
	}
	return strings.Join(parts, " ")
}

// isColumnText reports whether a canonical key is a column reference, a name with any
// qualifiers, rather than an expression.
//
// Takes key (string) which is the canonical text.
//
// Returns bool which is true for a column reference.
func isColumnText(key string) bool {
	for part := range strings.SplitSeq(key, " . ") {
		if part == "" || strings.ContainsAny(part, " ()") {
			return false
		}
	}
	return true
}

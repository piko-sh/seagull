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

package fuzzruntime

import (
	"errors"
	"slices"
	"strings"
)

// runtimeOpenGroup opens an expression group.
const runtimeOpenGroup = "("

// runtimeCloseGroup closes an expression group.
const runtimeCloseGroup = ")"

// runtimeBooleanTokens retains precedence while removing redundant query grouping.
//
// Takes tokens ([]string) which holds the fixed runtime grammar after binding.
//
// Returns []string which has an independently grouped WHERE predicate.
// Returns error when parentheses are unbalanced.
func runtimeBooleanTokens(tokens []string) ([]string, error) {
	depth, start, end := 0, -1, len(tokens)
	for index, token := range tokens {
		switch token {
		case runtimeOpenGroup:
			depth++
		case runtimeCloseGroup:
			depth--
		case "where":
			if depth == 0 && start < 0 {
				start = index + 1
			}
		case "order", "limit", "offset":
			if depth == 0 && start >= 0 && end == len(tokens) {
				end = index
			}
		}
		if depth < 0 {
			return nil, errors.New("runtime SQL closes an unopened group")
		}
	}
	if depth != 0 {
		return nil, errors.New("runtime SQL leaves a group open")
	}
	if start < 0 {
		return runtimeFlatTokens(tokens), nil
	}
	result := runtimeFlatTokens(tokens[:start])
	result = append(result, "predicate:"+runtimeBooleanTree(tokens[start:end]))
	return append(result, runtimeFlatTokens(tokens[end:])...), nil
}

// runtimeBooleanTree records AND and OR structure within the bounded predicate grammar.
//
// Takes tokens ([]string) which holds one balanced Boolean expression.
//
// Returns string which preserves precedence and operand order.
func runtimeBooleanTree(tokens []string) string {
	for runtimeOuterGroup(tokens) {
		tokens = tokens[1 : len(tokens)-1]
	}
	for _, operator := range []string{"or", "and"} {
		if parts := runtimeBooleanParts(tokens, operator); len(parts) > 0 {

			for index, part := range parts {
				if strings.HasPrefix(part, operator+"{") && strings.HasSuffix(part, "}") {
					parts[index] = part[len(operator)+1 : len(part)-1]
				}
			}
			return operator + "{" + strings.Join(parts, "|") + "}"
		}
	}
	return "leaf{" + strings.Join(runtimeFlatTokens(tokens), " ") + "}"
}

// runtimeBooleanParts splits one associative operator at the current group depth.
//
// Takes tokens ([]string) which holds a balanced Boolean expression.
// Takes operator (string) which selects AND or OR.
//
// Returns []string which contains independently grouped operands.
func runtimeBooleanParts(tokens []string, operator string) []string {
	var parts []string
	start, depth := 0, 0
	for index, token := range tokens {
		switch token {
		case runtimeOpenGroup:
			depth++
		case runtimeCloseGroup:
			depth--
		default:
			if token == operator && depth == 0 {
				parts = append(parts, runtimeBooleanTree(tokens[start:index]))
				start = index + 1
			}
		}
	}
	if start > 0 {
		parts = append(parts, runtimeBooleanTree(tokens[start:]))
	}
	return parts
}

// runtimeOuterGroup recognises parentheses surrounding an entire expression.
//
// Takes tokens ([]string) which holds a balanced predicate.
//
// Returns bool which excludes a group followed by another operand.
func runtimeOuterGroup(tokens []string) bool {
	if len(tokens) < 2 || tokens[0] != runtimeOpenGroup || tokens[len(tokens)-1] != runtimeCloseGroup {
		return false
	}
	depth := 0
	for index, token := range tokens {
		if token == runtimeOpenGroup {
			depth++
		}
		if token == runtimeCloseGroup {
			depth--
		}
		if depth == 0 {
			return index == len(tokens)-1
		}
	}
	return false
}

// runtimeFlatTokens normalises scalar and membership syntax outside Boolean grouping.
//
// Takes tokens ([]string) which contains no significant Boolean grouping.
//
// Returns []string which preserves the established runtime token representation.
func runtimeFlatTokens(tokens []string) []string {
	flat := slices.DeleteFunc(slices.Clone(tokens), func(token string) bool { return token == runtimeOpenGroup || token == runtimeCloseGroup })
	return canonicalMembership(flat)
}

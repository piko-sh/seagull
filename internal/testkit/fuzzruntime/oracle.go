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
	"fmt"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqllex"
)

// emptySubqueryWidth counts the significant tokens in an empty membership query.
const emptySubqueryWidth = 4

// emptyListToken stands for a list with no element in the simple runtime grammar.
const emptyListToken = "empty-list"

// runtimeItemsTable is the table whose qualifier the oracle removes.
const runtimeItemsTable = "fuzz_items"

// membershipTokens precede an array argument tested with = ANY(.
var membershipTokens = []string{"=", "any", "("}

// exclusionTokens precede an array argument tested with <> ALL(.
var exclusionTokens = []string{"<>", "all", "("}

// SQLValues compares the simple runtime grammar after substituting observed bindings.
//
// Boolean grouping remains significant when optional predicates contain OR.
//
// Takes facts (*dialect.Facts) which describes lexical and placeholder rules.
// Takes statement (Statement) which contains recorded SQL and arguments.
//
// Returns []string which preserves operators, columns, clause order and values.
// Returns error when a recorded placeholder has no argument.
func SQLValues(facts *dialect.Facts, statement Statement) ([]string, error) {
	placeholders := facts.Placeholders
	placeholders.Sigils = append(slices.Clone(placeholders.Sigils), placeholders.PositionalForm())
	tokens, err := sqllex.Tokens(facts.Lexical, statement.SQL)
	if err != nil {
		return nil, fmt.Errorf("tokenising runtime SQL: %w", err)
	}
	var result []string
	ordinal := 0
	used := make([]bool, len(statement.Arguments))
	for index := 0; index < len(tokens); index++ {
		token := tokens[index]
		if token.Kind.IsNoise() || token.Raw == ";" {
			continue
		}
		if token.Kind == sqllex.KindParameter {
			argument, err := runtimeArgument(placeholders, statement, token.Raw, ordinal)
			if err != nil {
				return nil, err
			}
			used[argument] = true
			result = append(result, argumentTokens(result, statement.Arguments[argument])...)
			ordinal++
			continue
		}
		text := strings.TrimSuffix(strings.TrimPrefix(token.Raw, facts.IdentifierQuote.Open), facts.IdentifierQuote.Close)
		if text == runtimeItemsTable && index+1 < len(tokens) && tokens[index+1].Raw == "." {
			index++
			continue
		}
		result = append(result, strings.ToLower(text))
	}
	if slices.Contains(used, false) {
		return nil, fmt.Errorf("unused runtime arguments in %+v", statement)
	}
	return runtimeBooleanTokens(result)
}

// runtimeArgument resolves a placeholder without using the renderer's binding logic.
//
// Takes placeholders (dialect.Placeholders) which supplies accepted forms.
// Takes statement (Statement) which holds driver arguments.
// Takes token (string) which is one placeholder.
// Takes ordinal (int) which counts anonymous occurrences.
//
// Returns int which indexes the observed argument.
// Returns error when the placeholder cannot be bound.
func runtimeArgument(placeholders dialect.Placeholders, statement Statement, token string, ordinal int) (int, error) {
	reading, ok := placeholders.Read(token)
	if !ok {
		return 0, fmt.Errorf("unreadable runtime placeholder %q", token)
	}
	argument := reading.Number - 1
	if reading.Sigil.Kind == dialect.PlaceholderAnonymous {
		argument = ordinal
	}
	if reading.Sigil.Kind == dialect.PlaceholderNamed {
		argument = slices.IndexFunc(statement.Arguments, func(value Argument) bool { return value.Name == reading.Name })
	}
	if argument < 0 || argument >= len(statement.Arguments) {
		return 0, fmt.Errorf("placeholder %q has no argument in %+v", token, statement.Arguments)
	}
	return argument, nil
}

// runtimeArgumentTokens expands a driver scalar or list in its original order.
//
// Takes argument (Argument) which holds the observed values.
//
// Returns []string which substitutes those values into the token stream.
func runtimeArgumentTokens(argument Argument) []string {
	if argument.List && len(argument.Values) == 0 {
		return []string{emptyListToken}
	}
	var tokens []string
	for index, value := range argument.Values {
		if index > 0 {
			tokens = append(tokens, ",")
		}
		tokens = append(tokens, strings.ToLower(value))
	}
	return tokens
}

// argumentTokens returns the tokens an argument stands for. An array's text form bound
// inside = ANY( or <> ALL( gives its elements, and any other argument gives its values.
//
// Takes written ([]string) which holds the tokens before the placeholder.
// Takes argument (Argument) which is the placeholder's argument.
//
// Returns []string which holds the tokens.
func argumentTokens(written []string, argument Argument) []string {
	if elements, isArray := arrayArgumentTokens(written, argument); isArray {
		return elements
	}
	return runtimeArgumentTokens(argument)
}

// arrayArgumentTokens reads an array's text form bound inside = ANY( or <> ALL( as the
// elements an expanded IN list would write. Integers stay as they are, quoted elements
// become string literals, NULL becomes null and no element becomes an empty list.
//
// Takes written ([]string) which holds the tokens before the placeholder.
// Takes argument (Argument) which is the placeholder's argument.
//
// Returns []string which holds the elements' tokens.
// Returns bool which is false when the argument is not an array's text form there.
func arrayArgumentTokens(written []string, argument Argument) ([]string, bool) {
	if argument.List || len(argument.Values) != 1 || !followsArrayComparison(written) {
		return nil, false
	}
	body, ok := strings.CutPrefix(argument.Values[0], "{")
	if !ok {
		return nil, false
	}
	body, ok = strings.CutSuffix(body, "}")
	if !ok {
		return nil, false
	}
	if body == "" {
		return []string{emptyListToken}, true
	}
	var tokens []string
	for index := 0; index <= len(body); index++ {
		if len(tokens) > 0 {
			tokens = append(tokens, ",")
		}
		element, quoted, next := arrayElement(body, index)
		switch {
		case quoted:
			tokens = append(tokens, "'"+strings.ReplaceAll(element, "'", "''")+"'")
		default:
			tokens = append(tokens, strings.ToLower(element))
		}
		index = next
	}
	return tokens, true
}

// arrayElement reads one element of an array's text form from a position. A plain element
// runs to the next comma, and a quoted one loses its quotes and backslash escapes.
//
// Takes body (string) which is the text between the braces.
// Takes start (int) which is where the element starts.
//
// Returns string which is the element.
// Returns bool which is true for a quoted element.
// Returns int which is the position of the comma after it, or len(body).
func arrayElement(body string, start int) (string, bool, int) {
	if start < len(body) && body[start] == '"' {
		var element strings.Builder
		index := start + 1
		for ; index < len(body) && body[index] != '"'; index++ {
			if body[index] == '\\' && index+1 < len(body) {
				index++
			}
			element.WriteByte(body[index])
		}
		return element.String(), true, index + 1
	}
	end := strings.IndexByte(body[start:], ',')
	if end < 0 {
		return body[start:], false, len(body)
	}
	return body[start : start+end], false, start + end
}

// canonicalMembership normalises equivalent membership tests of non-null integers.
//
// Takes tokens ([]string) which holds the simple runtime grammar.
//
// Returns []string which uses one form for arrays and empty membership tests.
func canonicalMembership(tokens []string) []string {
	var result []string
	for index := 0; index < len(tokens); index++ {
		token := tokens[index]
		if token == "=" && index+1 < len(tokens) && tokens[index+1] == "any" {
			token = "in"
			index++
		}
		if token == "'{}'" || token == "[]" {
			token = emptyListToken
		}
		if token == "select" && len(result) > 0 && result[len(result)-1] == "in" && emptySubquery(tokens[index:]) {
			token = emptyListToken
			index += emptySubqueryWidth - 1
		}
		result = append(result, token)
	}
	return result
}

// emptySubquery recognises the exact empty-membership subquery.
//
// Takes tokens ([]string) which starts with the SELECT token.
//
// Returns bool which identifies a guaranteed empty result.
func emptySubquery(tokens []string) bool {
	return len(tokens) >= emptySubqueryWidth && tokens[1] == "null" && tokens[2] == "where" && (tokens[emptySubqueryWidth-1] == "0" || tokens[emptySubqueryWidth-1] == "false")
}

// followsArrayComparison reports whether the written tokens end in = ANY( or <> ALL(.
//
// Takes written ([]string) which holds the tokens before a placeholder.
//
// Returns bool which is true when an array argument follows.
func followsArrayComparison(written []string) bool {
	return slices.Equal(lastTokens(written, len(membershipTokens)), membershipTokens) ||
		slices.Equal(lastTokens(written, len(exclusionTokens)), exclusionTokens)
}

// lastTokens returns at most the last count tokens.
//
// Takes tokens ([]string) which holds the written tokens.
// Takes count (int) which is how many trailing tokens to keep.
//
// Returns []string which holds the trailing tokens.
func lastTokens(tokens []string, count int) []string {
	return tokens[max(len(tokens)-count, 0):]
}

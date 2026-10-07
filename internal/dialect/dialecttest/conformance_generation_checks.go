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

package dialecttest

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/sqllex"
)

// generatedSyntaxProblems validates the SQL forms used by generated helpers.
//
// It checks that list tests, pagination and casts are complete and well formed, with no
// unexpected placeholders.
//
// Takes info (*dialect.Info) which is the dialect's description.
//
// Returns []string which names each problem found, or nil.
func generatedSyntaxProblems(info *dialect.Info) []string {
	var problems []string
	problems = append(problems, emptyListProblems(info)...)
	if (info.Lists.ArrayMembership == dialect.Affix{}) != (info.Lists.ArrayExclusion == dialect.Affix{}) {
		problems = append(problems, "Info.Lists.ArrayMembership and Info.Lists.ArrayExclusion must be given together")
	}
	if info.Pagination.Limit == (dialect.Affix{}) {
		problems = append(problems, "Info.Pagination.Limit must give the text written around a row limit")
	}
	if info.Pagination.Offset == (dialect.Affix{}) {
		problems = append(problems, "Info.Pagination.Offset must give the text written around an offset")
	}
	for index, cast := range info.Filters.Casts {
		if cast.Middle == "" {
			problems = append(problems, fmt.Sprintf("Info.Filters.Casts[%d] must give the text between the value and the type", index))
		}
	}
	if len(info.Filters.Casts) > 0 && len(info.Filters.CastTypes) == 0 {
		problems = append(problems, "Info.Filters.CastTypes must list the types Info.Filters.Casts convert to")
	}
	for index, castType := range info.Filters.CastTypes {
		if castType == "" || strings.TrimSpace(castType) != castType {
			problems = append(problems, fmt.Sprintf("Info.Filters.CastTypes[%d] %q must be a type name without surrounding whitespace", index, castType))
		}
	}
	return append(problems, placeholderFactProblems(info)...)
}

// emptyListProblems checks that the tests an empty slice is written as are given and read
// under the dialect's lexical rules with their parentheses balanced, so either can stand
// where an operator and its list go.
//
// Takes info (*dialect.Info) which is the dialect's description.
//
// Returns []string which names each problem found, or nil.
func emptyListProblems(info *dialect.Info) []string {
	var problems []string
	for _, test := range []struct{ name, text string }{
		{name: "Info.Lists.EmptyMembership", text: info.Lists.EmptyMembership},
		{name: "Info.Lists.EmptyExclusion", text: info.Lists.EmptyExclusion},
	} {
		if strings.TrimSpace(test.text) == "" {
			problems = append(problems, test.name+" must give the test an operator and an empty list are written as")
			continue
		}
		tokens, err := significantTokens(info.Lexical, test.text)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s %q must read under Info.Lexical: %v", test.name, test.text, err))
			continue
		}
		if !parenthesesBalance(tokens) {
			problems = append(problems, fmt.Sprintf("%s %q must balance its parentheses", test.name, test.text))
		}
	}
	return problems
}

// parenthesesBalance reports whether every parenthesis a run of tokens opens it also
// closes, and none closes before it opens.
//
// Takes tokens ([]sqllex.Token) which are significant tokens.
//
// Returns bool which is true when the parentheses balance.
func parenthesesBalance(tokens []sqllex.Token) bool {
	depth := 0
	for _, token := range tokens {
		switch token.Raw {
		case "(":
			depth++
		case ")":
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0
}

// placeholderFactProblems reports each fact the generated code writes that holds a
// placeholder under the dialect's lexical rules.
//
// Takes info (*dialect.Info) which is the dialect's description.
//
// Returns []string which names each fact that holds a placeholder.
func placeholderFactProblems(info *dialect.Info) []string {
	facts := map[string]string{
		"Info.Lists.EmptyMembership":     info.Lists.EmptyMembership,
		"Info.Lists.EmptyExclusion":      info.Lists.EmptyExclusion,
		"Info.Lists.ArrayMembership":     info.Lists.ArrayMembership.Before + info.Lists.ArrayMembership.After,
		"Info.Lists.ArrayExclusion":      info.Lists.ArrayExclusion.Before + info.Lists.ArrayExclusion.After,
		"Info.Pagination.Limit":          info.Pagination.Limit.Before + " 1 " + info.Pagination.Limit.After,
		"Info.Pagination.Offset":         info.Pagination.Offset.Before + " 1 " + info.Pagination.Offset.After,
		"Info.Pagination.UnboundedLimit": info.Pagination.UnboundedLimit,
		"Info.Filters.Casts":             castFactText(info),
	}
	var problems []string
	for _, name := range slices.Sorted(maps.Keys(facts)) {
		tokens, err := sqllex.Tokens(info.Lexical, facts[name])
		if err != nil {
			continue
		}
		for _, token := range tokens {
			if token.Kind == sqllex.KindParameter {
				problems = append(problems, fmt.Sprintf("%s must hold no placeholder, but %q reads as one", name, token.Raw))
				break
			}
		}
	}
	return problems
}

// castFactText writes every conversion of every listed type, as the generated code would,
// around the column x.
//
// Takes info (*dialect.Info) which is the dialect's description.
//
// Returns string which holds the conversions, one per line.
func castFactText(info *dialect.Info) string {
	var builder strings.Builder
	for _, cast := range info.Filters.Casts {
		for _, castType := range info.Filters.CastTypes {
			builder.WriteString(cast.Before + "x" + cast.Middle + castType + cast.After + "\n")
		}
	}
	return builder.String()
}

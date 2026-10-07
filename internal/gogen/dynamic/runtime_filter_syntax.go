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

package dynamic

import (
	"cmp"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/gogen"
	"seagull.sh/seagull/internal/gogen/helpers"
)

const (
	// identifierPattern matches a bare identifier. It uses the Unicode-aware RE2 classes
	// \p{L} (any letter) and \p{Nd} (any decimal digit) rather than the ASCII ranges so an
	// allow-listed column whose name contains non-ASCII letters is not rejected by the
	// validator before the allow-list is consulted, matching the tokeniser's
	// unicode.IsLetter identifier rule.
	identifierPattern = `[\p{L}_][\p{L}\p{Nd}_]*`

	// quotedLiteralPattern matches a single-quoted literal holding no quote and no
	// backslash, since some dialects read a backslash inside a literal as an escape that
	// would let the literal run on past its closing quote.
	quotedLiteralPattern = `'[^'\\]*'`
	// jsonPathStepPattern matches the operand of a JSON path operator: a quoted key, or an
	// index.
	jsonPathStepPattern = `(?:` + quotedLiteralPattern + `|[0-9]+)`

	// requiredSpace matches the whitespace a cast form writes, which a caller must write.
	requiredSpace = `\s+`

	// optionalSpace matches whitespace a caller may write where a cast form writes none.
	optionalSpace = `\s*`
)

var (
	// commonFilterOperators are the predicate operators every dialect accepts, in the order
	// the generated allow-list lists them; a dialect's own operators follow.
	commonFilterOperators = []string{
		"=", "!=", "<>", "<", ">", "<=", ">=",
		"LIKE", "IS NULL", "IS NOT NULL",
		"IN", "NOT IN",
	}
)

// RuntimeSyntax describes the dialect syntax accepted and written by runtime query
// helpers.
//
// The allow-list holds the common operators and the dialect's own, less any that contain
// the text of an anonymous placeholder, which the bind counting could not tell apart from
// one. Every fact comes from the dialect's Facts.
//
// Takes emission (*gogen.Emission) which supplies the dialect.
//
// Returns *helpers.RuntimeSyntax which is the syntax.
func RuntimeSyntax(emission *gogen.Emission) *helpers.RuntimeSyntax {
	info := emission.Dialect()
	form := emission.BindForm()
	operators := slices.Clone(commonFilterOperators)
	for _, operator := range info.Filters.Operators {
		if form.Kind == dialect.PlaceholderAnonymous && strings.Contains(operator, form.Prefix) {
			continue
		}
		operators = append(operators, operator)
	}
	jsonOperators := info.JSONOperators
	openings := make([]string, 0, len(info.Filters.JSONPathFunctions))
	for _, function := range info.Filters.JSONPathFunctions {
		openings = append(openings, strings.ToLower(function)+"(")
	}
	bare := bareColumnExpressionPattern(jsonOperators, info.Filters.JSONPathFunctions)
	types := castTypeAlternation(info.Filters.CastTypes)
	castPatterns := make([]string, len(info.Filters.Casts))
	alternatives := make([]string, 0, 1+len(info.Filters.Casts))
	alternatives = append(alternatives, bare)
	for index, cast := range info.Filters.Casts {
		castPatterns[index] = `^(?i:` + castPattern(cast, `(`+bare+`)`, `(`+types+`)`) + `)$`
		alternatives = append(alternatives, castPattern(cast, bare, types))
	}
	return &helpers.RuntimeSyntax{
		ColumnExpression: `^(?i:` + strings.Join(alternatives, `|`) + `)$`,
		Lists:            info.Lists,
		Pagination:       info.Pagination,
		Form:             form,
		Operators:        operators,
		JSONPathOpenings: openings,
		RootOperators:    rootOperators(jsonOperators),
		CastPatterns:     castPatterns,
		Casts:            slices.Clone(info.Filters.Casts),
		CastTypes:        slices.Clone(info.Filters.CastTypes),
	}
}

// bareColumnExpressionPattern returns the inner regex form of a column expression.
//
// The expression is an identifier optionally extended with the dialect's JSON path
// operators, each followed by a key or an index. It can also be a call of a JSON path
// function with an identifier and a single-quoted path literal whose body holds no quote
// or backslash.
//
// Takes jsonOperators ([]string) which are the dialect's JSON path operators.
// Takes jsonPathFunctions ([]string) which are its JSON path functions.
//
// Returns string which is the regex source, a non-capturing group.
func bareColumnExpressionPattern(jsonOperators, jsonPathFunctions []string) string {
	var builder strings.Builder
	builder.WriteString(`(?:` + identifierPattern)
	if alternation := jsonOperatorAlternation(jsonOperators); alternation != "" {
		builder.WriteString(`(?:` + alternation + jsonPathStepPattern + `)*`)
	}
	for _, function := range jsonPathFunctions {
		builder.WriteString(`|` + regexp.QuoteMeta(function) + `\(` + identifierPattern + `,\s*` + quotedLiteralPattern + `\)`)
	}
	builder.WriteString(`)`)
	return builder.String()
}

// castPattern returns the regex source of a column converted in one cast form: the form's
// text around the column and the type, whatever their case, with whitespace required
// where the form writes it and allowed elsewhere.
//
// Takes cast (dialect.CastForm) which is the form.
// Takes column (string) which matches the column.
// Takes types (string) which matches the type.
//
// Returns string which is the regex source.
func castPattern(cast dialect.CastForm, column, types string) string {
	return fragmentPattern(cast.Before) + column + fragmentPattern(cast.Middle) + types + fragmentPattern(cast.After)
}

// fragmentPattern returns the regex source of a cast form's fragment: its text quoted,
// with the whitespace around it required where the fragment writes it and allowed where
// it does not. An empty fragment matches nothing more.
//
// Takes fragment (string) which is the fragment.
//
// Returns string which is the regex source.
func fragmentPattern(fragment string) string {
	core := strings.TrimSpace(fragment)
	if core == "" {
		return ""
	}
	lead, trail := optionalSpace, optionalSpace
	if strings.IndexFunc(fragment, unicode.IsSpace) == 0 {
		lead = requiredSpace
	}
	if last := len(fragment) - 1; strings.LastIndexFunc(fragment, unicode.IsSpace) == last {
		trail = requiredSpace
	}
	return lead + spacedPattern(core) + trail
}

// castTypeAlternation returns the regex alternation of the cast types, longest first so a
// longer type is never read as a shorter one it begins with.
//
// Takes castTypes ([]string) which are the types.
//
// Returns string which is the alternation, a non-capturing group.
func castTypeAlternation(castTypes []string) string {
	sorted := slices.Clone(castTypes)
	slices.SortFunc(sorted, func(left, right string) int {
		return cmp.Or(cmp.Compare(len(right), len(left)), cmp.Compare(left, right))
	})
	alternatives := make([]string, 0, len(sorted))
	for _, castType := range sorted {
		alternatives = append(alternatives, spacedPattern(castType))
	}
	return `(?:` + strings.Join(alternatives, `|`) + `)`
}

// spacedPattern returns text quoted for a regex, each run of whitespace in it matching
// any run.
//
// Takes text (string) which is the text.
//
// Returns string which is the regex source.
func spacedPattern(text string) string {
	words := strings.Fields(text)
	for index := range words {
		words[index] = regexp.QuoteMeta(words[index])
	}
	return strings.Join(words, requiredSpace)
}

// jsonOperatorAlternation returns the regex alternation of the JSON path operators,
// longest first so a longer operator is never read as a shorter one it begins with.
//
// Takes operators ([]string) which are the operators.
//
// Returns string which is the alternation, or empty when there are no operators.
func jsonOperatorAlternation(operators []string) string {
	if len(operators) == 0 {
		return ""
	}
	sorted := slices.Clone(operators)
	slices.SortFunc(sorted, func(left, right string) int {
		return cmp.Or(cmp.Compare(len(right), len(left)), cmp.Compare(left, right))
	})
	alternatives := make([]string, 0, len(sorted))
	for _, operator := range sorted {
		alternatives = append(alternatives, regexp.QuoteMeta(operator))
	}
	return `(?:` + strings.Join(alternatives, `|`) + `)`
}

// rootOperators returns the JSON path operators whose first occurrence marks the end of a
// column's root identifier: every operator that does not begin with another, since
// finding the shorter one finds both.
//
// Takes operators ([]string) which are the JSON path operators.
//
// Returns []string which holds the operators to look for, shortest first.
func rootOperators(operators []string) []string {
	var roots []string
	for _, operator := range operators {
		covered := false
		for _, other := range operators {
			if other != operator && strings.HasPrefix(operator, other) {
				covered = true
				break
			}
		}
		if !covered {
			roots = append(roots, operator)
		}
	}
	slices.SortFunc(roots, func(left, right string) int {
		return cmp.Or(cmp.Compare(len(left), len(right)), cmp.Compare(left, right))
	})
	return roots
}

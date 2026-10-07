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

package dialect_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
)

func completeInfo() dialect.Info {
	return dialect.Info{
		Comments:         ast.CommentStyle{LinePrefix: "--"},
		Name:             "mine",
		Family:           "mine",
		SchemaSearchPath: []string{"main"},
		Placeholders: dialect.Placeholders{
			Sigils:     []dialect.PlaceholderSigil{{Prefix: "?", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered}},
			Execution:  dialect.PlaceholderSigil{Prefix: "?", Suffix: "", TypeSeparator: "", Kind: dialect.PlaceholderNumbered},
			Positional: dialect.PlaceholderSigil{},
			NamePrefix: "",
		},
		Expressions: ast.SQLFeaturesBase,
		Lists:       dialect.ListSyntax{EmptyMembership: "IN (SELECT NULL WHERE FALSE)", EmptyExclusion: "NOT IN (SELECT NULL WHERE FALSE)"},
		Pagination:  dialect.PaginationSyntax{Limit: dialect.Affix{Before: "LIMIT ", After: ""}},
	}
}

func TestInfoProblemsFindsNothingWrongWithACompleteInfo(t *testing.T) {
	t.Parallel()

	info := completeInfo()
	assert.Empty(t, dialect.InfoProblems(&info))
}

func TestInfoProblemsAcceptsAHashLinePrefixTheLexicalRulesReadAsAComment(t *testing.T) {
	t.Parallel()

	info := completeInfo()
	info.Comments.LinePrefix = "#"
	info.Lexical.Comments.HashLineComments = true
	assert.Empty(t, dialect.InfoProblems(&info))
}

func TestInfoProblemsNamesEachMissingField(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		mutate func(info *dialect.Info)
		want   string
	}{
		{name: "missing name", mutate: func(info *dialect.Info) { info.Name = "" }, want: "Info.Name must name the dialect"},
		{name: "missing family", mutate: func(info *dialect.Info) { info.Family = "" }, want: "Info.Family must name the dialect's family"},
		{name: "empty line comment prefix", mutate: func(info *dialect.Info) { info.Comments = ast.CommentStyle{} }, want: `Info.Comments.LinePrefix must be a line comment prefix without whitespace, got ""`},
		{name: "prefix with whitespace", mutate: func(info *dialect.Info) { info.Comments.LinePrefix = "- -" }, want: `Info.Comments.LinePrefix must be a line comment prefix without whitespace, got "- -"`},
		{name: "prefix the lexical rules do not read as a comment", mutate: func(info *dialect.Info) { info.Comments.LinePrefix = "#" }, want: `Info.Comments.LinePrefix "#" must open a line comment under Info.Lexical`},
		{name: "terminator with whitespace", mutate: func(info *dialect.Info) { info.Lexical.Terminator = "; " }, want: `Info.Lexical.Terminator must not contain whitespace, got "; "`},
		{name: "no expressions", mutate: func(info *dialect.Info) { info.Expressions = 0 }, want: "Info.Expressions must list the expression kinds the dialect supports"},
		{name: "no executed placeholder", mutate: func(info *dialect.Info) { info.Placeholders.Execution = dialect.PlaceholderSigil{} }, want: "Info.Placeholders.Execution must give the form the generated code writes each parameter in"},
		{name: "a placeholder form without a prefix", mutate: func(info *dialect.Info) {
			info.Placeholders.Sigils = append(info.Placeholders.Sigils, dialect.PlaceholderSigil{})
		}, want: "Info.Placeholders.Sigils[1] must give the text that opens the placeholder"},
		{name: "no empty membership test", mutate: func(info *dialect.Info) { info.Lists.EmptyMembership = " " }, want: "Info.Lists.EmptyMembership must give the test IN and an empty list are written as"},
		{name: "no empty exclusion test", mutate: func(info *dialect.Info) { info.Lists.EmptyExclusion = "" }, want: "Info.Lists.EmptyExclusion must give the test NOT IN and an empty list are written as"},
		{name: "an array membership form without its exclusion", mutate: func(info *dialect.Info) {
			info.Lists.ArrayMembership = dialect.Affix{Before: "= ANY(", After: ")"}
		}, want: "Info.Lists.ArrayMembership and Info.Lists.ArrayExclusion must be given together"},
		{name: "no row limit form", mutate: func(info *dialect.Info) { info.Pagination.Limit = dialect.Affix{} }, want: "Info.Pagination.Limit must give the text written around a row limit"},
		{name: "a conversion without the text between value and type", mutate: func(info *dialect.Info) {
			info.Filters.Casts = []dialect.CastForm{{Before: "CAST(", Middle: "", After: ")"}}
			info.Filters.CastTypes = []string{"INTEGER"}
		}, want: "Info.Filters.Casts[0] must give the text between the value and the type"},
		{name: "conversions without types", mutate: func(info *dialect.Info) {
			info.Filters.Casts = []dialect.CastForm{{Before: "CAST(", Middle: " AS ", After: ")"}}
		}, want: "Info.Filters.CastTypes must list the types Info.Filters.Casts convert to"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			info := completeInfo()
			testCase.mutate(&info)
			require.Equal(t, []string{testCase.want}, dialect.InfoProblems(&info))
		})
	}
}

func TestInfoProblemsListsEveryProblemInFieldOrder(t *testing.T) {
	t.Parallel()

	var info dialect.Info
	problems := dialect.InfoProblems(&info)

	require.Equal(t, []string{
		"Info.Name must name the dialect",
		"Info.Family must name the dialect's family",
		`Info.Comments.LinePrefix must be a line comment prefix without whitespace, got ""`,
		"Info.Expressions must list the expression kinds the dialect supports",
		"Info.Placeholders.Execution must give the form the generated code writes each parameter in",
		"Info.Lists.EmptyMembership must give the test IN and an empty list are written as",
		"Info.Lists.EmptyExclusion must give the test NOT IN and an empty list are written as",
		"Info.Pagination.Limit must give the text written around a row limit",
	}, problems)
}

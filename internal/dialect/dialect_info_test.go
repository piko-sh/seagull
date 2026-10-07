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

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/fault"
	"seagull.sh/seagull/internal/testkit/fakedialect"
)

func TestNewValidatesTheDialectInfo(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		mutate      func(info *dialect.Info)
		wantMessage string
	}{
		{name: "empty line comment prefix", mutate: func(info *dialect.Info) { info.Comments = ast.CommentStyle{} }, wantMessage: "Info.Comments.LinePrefix"},
		{name: "prefix with whitespace", mutate: func(info *dialect.Info) { info.Comments.LinePrefix = "- -" }, wantMessage: "Info.Comments.LinePrefix"},
		{name: "prefix the lexical rules do not read as a comment", mutate: func(info *dialect.Info) { info.Comments.LinePrefix = "#" }, wantMessage: "must open a line comment under Info.Lexical"},
		{name: "prefix the lexical rules read as an expression", mutate: func(info *dialect.Info) {
			info.Lexical.Comments.DoubleDashRequiresWhitespace = true
			info.Comments.LinePrefix = "--x"
		}, wantMessage: "must open a line comment under Info.Lexical"},
		{name: "terminator with whitespace", mutate: func(info *dialect.Info) { info.Lexical.Terminator = "; " }, wantMessage: "Info.Lexical.Terminator"},
		{name: "missing name", mutate: func(info *dialect.Info) { info.Name = "" }, wantMessage: "Info.Name"},
		{name: "missing family", mutate: func(info *dialect.Info) { info.Family = "" }, wantMessage: "Info.Family"},
		{name: "no expressions", mutate: func(info *dialect.Info) { info.Expressions = 0 }, wantMessage: "Info.Expressions"},
		{name: "no executed placeholder", mutate: func(info *dialect.Info) { info.Placeholders.Execution = dialect.PlaceholderSigil{} }, wantMessage: "Info.Placeholders.Execution"},
		{name: "a placeholder form without a prefix", mutate: func(info *dialect.Info) {
			info.Placeholders.Sigils = append(info.Placeholders.Sigils, dialect.PlaceholderSigil{})
		}, wantMessage: "Info.Placeholders.Sigils"},
		{name: "no empty membership test", mutate: func(info *dialect.Info) { info.Lists.EmptyMembership = " " }, wantMessage: "Info.Lists.EmptyMembership"},
		{name: "no empty exclusion test", mutate: func(info *dialect.Info) { info.Lists.EmptyExclusion = "" }, wantMessage: "Info.Lists.EmptyExclusion"},
		{name: "an array membership form without its exclusion", mutate: func(info *dialect.Info) {
			info.Lists.ArrayMembership = dialect.Affix{Before: "= ANY(", After: ")"}
		}, wantMessage: "Info.Lists.ArrayExclusion"},
		{name: "no row limit form", mutate: func(info *dialect.Info) { info.Pagination.Limit = dialect.Affix{} }, wantMessage: "Info.Pagination.Limit"},
		{name: "a conversion without the text between value and type", mutate: func(info *dialect.Info) {
			info.Filters.Casts = []dialect.CastForm{{Before: "CAST(", Middle: "", After: ")"}}
			info.Filters.CastTypes = []string{"INTEGER"}
		}, wantMessage: "Info.Filters.Casts[0]"},
		{name: "conversions without types", mutate: func(info *dialect.Info) {
			info.Filters.Casts = []dialect.CastForm{{Before: "CAST(", Middle: " AS ", After: ")"}}
		}, wantMessage: "Info.Filters.CastTypes"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			info := completeInfo()
			testCase.mutate(&info)
			engine := &fakedialect.Engine{InfoFunc: func() dialect.Info { return info }}

			generator, err := app.New(engine)
			require.Nil(t, generator)
			require.ErrorIs(t, err, fault.ErrInvalidConfig)
			require.ErrorContains(t, err, testCase.wantMessage)
		})
	}
}

func TestNewAcceptsAHashLinePrefixTheLexicalRulesReadAsAComment(t *testing.T) {
	t.Parallel()

	info := completeInfo()
	info.Comments.LinePrefix = "#"
	info.Lexical.Comments.HashLineComments = true
	generator, err := app.New(&fakedialect.Engine{InfoFunc: func() dialect.Info { return info }})
	require.NoError(t, err)
	require.NotNil(t, generator)
}

func TestNewAcceptsACompleteDialectInfo(t *testing.T) {
	t.Parallel()

	info := completeInfo()
	generator, err := app.New(&fakedialect.Engine{InfoFunc: func() dialect.Info { return info }})
	require.NoError(t, err)
	require.NotNil(t, generator)
}

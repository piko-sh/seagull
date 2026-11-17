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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/dialect/dialects"
	"seagull.sh/seagull/internal/directive"
	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqlfile"
)

func TestDirectiveGrammarAgreesAcrossDialectStages(t *testing.T) {
	for _, name := range dialects.Names() {
		t.Run(name, func(t *testing.T) {
			engine, err := dialects.New(name, dialects.Options{})
			require.NoError(t, err)
			info := engine.Info()
			lexicon := syntax.Lexicon{LinePrefix: info.Comments.LinePrefix, Rules: info.Lexical}
			prefixes := namespace.Set{Canonical: "sgl", Aliases: []string{"seagull"}, Foreign: nil}
			anchor, unsupported := "$1", "{n:Int64}"
			if name == "sqlite" || name == "mysql" || name == "mariadb" {
				anchor = "?1"
			}
			if name == "clickhouse" {
				anchor, unsupported = "{n:Int64}", "$1"
			}
			t.Run("unsupported anchor remains prose", func(t *testing.T) {
				source := "-- sgl.query(Q, one)\n-- " + unsupported + " as seagull.param(a)\nSELECT 1;"
				formatted, err := app.FormatDirectivesFor(engine, []byte(source), "sgl", "seagull")
				require.NoError(t, err)
				require.Equal(t, source, string(formatted))
			})
			t.Run("tabs are parameter separators", func(t *testing.T) {
				parser := directive.NewParser(info.Placeholders.Sigils, info.Comments, directive.PrefixPolicy{Prefixes: prefixes})
				source := "-- sgl.query(Q, one)\n-- " + anchor + "\tas\tseagull.param(a)\nSELECT 1;"
				parsed, diagnostics := parser.Parse(sqlfile.QueryBlock{SQL: source, StartLine: 1, Lexical: info.Lexical}, "fuzz.sql")
				require.Empty(t, diagnostics)
				require.Len(t, parsed.Parameters, 1)
				require.Empty(t, parsed.Description)
			})

			t.Run("binding stays with its original query", func(t *testing.T) {
				source := "-- sgl.query(First, one)\nSELECT 1;\n-- " + anchor + "\tAS\tseagull.param(a)\n-- sgl.query(Second, one)\nSELECT 2;"
				querySyntax := sqlfile.QuerySyntax{Prefixes: prefixes, Directives: directive.Roles(), Lexicon: lexicon}
				blocks, truncated := sqlfile.SplitQueryFile([]byte(source), querySyntax)
				require.Zero(t, truncated)
				require.Len(t, blocks, 2)
				require.Contains(t, blocks[0].SQL, "seagull.param(a)")
				require.NotContains(t, blocks[1].SQL, "seagull.param(a)")
			})
			t.Run("case insensitive binding keyword", func(t *testing.T) {
				for _, keyword := range []string{"as", "AS", "As", "aS"} {
					source := "-- " + anchor + " " + keyword + " seagull.param(a)\nSELECT 1;"
					require.Equal(t, "SELECT 1;", syntax.StripDirectives(source, lexicon, prefixes))
					require.Equal(t, "-- "+anchor+" as sgl.param(a)\nSELECT 1;", syntax.FormatSource(source, lexicon, prefixes))
				}
			})
		})
	}
}

func TestDirectiveMeaningOracleRejectsChangedQuotedValues(t *testing.T) {
	engine, err := dialects.New("postgres", dialects.Options{})
	require.NoError(t, err)
	info := engine.Info()
	prefixes := namespace.Set{Canonical: "sgl", Aliases: []string{"seagull"}, Foreign: nil}
	source := "-- seagull.query( Q,one )\n-- seagull.column( id, struct_tags: 'json:\"id  value\"' )\nSELECT id;"
	lexicon := syntax.Lexicon{LinePrefix: info.Comments.LinePrefix, Rules: info.Lexical}
	formatted := syntax.FormatSource(source, lexicon, prefixes)
	require.Empty(t, directiveMeaningProblems(&info, prefixes, source, formatted))
	require.NotEmpty(t, directiveMeaningProblems(&info, prefixes, source, strings.Replace(formatted, "id  value", "id value", 1)))
	require.NotEmpty(t, directiveMeaningProblems(&info, prefixes, source, strings.Replace(formatted, "Q, one", "Changed, one", 1)))
}

func TestMalformedDirectiveFormattingHasOnlyStructuralObligations(t *testing.T) {
	engine, err := dialects.New("mysql", dialects.Options{})
	require.NoError(t, err)
	info := engine.Info()
	source := "-- sgl.query(\f\nSELECT 1;"
	formatted := formatFuzzSource(&info, source, namespace.Default())
	require.Empty(t, directiveMeaningProblems(&info, namespace.Default(), source, formatted))
	lexicon := syntax.Lexicon{LinePrefix: info.Comments.LinePrefix, Rules: info.Lexical}
	require.Equal(t, "SELECT 1;", syntax.StripDirectives(formatted, lexicon, namespace.Default()))
}

func TestDirectiveMeaningOracleChecksEveryQueryBlock(t *testing.T) {
	engine, err := dialects.New("postgres", dialects.Options{})
	require.NoError(t, err)
	info := engine.Info()
	source := "-- sgl.query(First, one)\nSELECT 1;\n-- sgl.query(Second, many)\nSELECT 2;"
	require.NotEmpty(t, directiveMeaningProblems(&info, namespace.Default(), source, strings.Replace(source, "Second", "Changed", 1)))
	require.NotEmpty(t, directiveMeaningProblems(&info, namespace.Default(), source, strings.Replace(source, "Second, many", "Second, one", 1)))
}

func TestDirectiveAttachmentOracleRejectsMissingToken(t *testing.T) {
	engine, err := dialects.New("postgres", dialects.Options{})
	require.NoError(t, err)
	readOnly := true
	ordinals, ok := directiveStatementOrdinals(engine.Info().Lexical, "SELECT 1;", map[int]*bool{0: &readOnly})
	require.True(t, ok)
	require.Equal(t, map[int]*bool{0: &readOnly}, ordinals)
	for _, offset := range []int{-1, 1, 6, 99} {
		_, ok = directiveStatementOrdinals(engine.Info().Lexical, "SELECT 1;", map[int]*bool{offset: &readOnly})
		require.False(t, ok, "accepted offset %d", offset)
	}
}

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

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/dialect/dialects"
	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqllex"
)

func TestParameterDirectiveRegressionsAcrossDialects(t *testing.T) {
	t.Parallel()
	for _, engine := range builtinDialects(t) {
		info := engine.Info()
		t.Run(info.Name, func(t *testing.T) {
			t.Parallel()
			lexicon := syntax.Lexicon{LinePrefix: info.Comments.LinePrefix, Rules: info.Lexical}
			prefixes := namespace.Set{Canonical: "sgl", Aliases: []string{"seagull"}, Foreign: []string{"other"}}
			t.Run("horizontal whitespace", func(t *testing.T) {
				for _, anchor := range []string{"$1", "?1", "?", ":name", "@name", "{n:Int64}"} {
					for _, gap := range []string{" ", "  ", "\t", " \t "} {
						for _, prefix := range []string{"sgl", "seagull"} {
							source := "-- " + anchor + gap + "as" + gap + prefix + ".param(a)\r\nSELECT 1;"
							formatted := syntax.FormatSource(source, lexicon, prefixes)
							require.Equal(t, "-- "+anchor+" as sgl.param(a)\r\nSELECT 1;", formatted)
							require.Equal(t, "SELECT 1;", syntax.StripDirectives(source, lexicon, prefixes), source)
							require.Equal(t, "SELECT 1;", syntax.StripDirectives(formatted, lexicon, prefixes), source)
							tokens, err := sqllex.Tokens(info.Lexical, source)
							require.NoError(t, err)
							ranges := syntax.StripTokenRanges(source, lexicon, prefixes, tokens)
							require.Equal(t, "SELECT 1;", syntax.KeptText(source, ranges), source)
						}
					}
				}
			})
			t.Run("malformed anchors", func(t *testing.T) {
				for _, source := range []string{"--( as seagull.A", "--( as\tseagull.A", "-- ( as\tseagull.A"} {
					require.Equal(t, source, syntax.FormatSource(source, lexicon, prefixes))
					expected := ""
					if info.Lexical.Comments.DoubleDashRequiresWhitespace && strings.HasPrefix(source, "--(") {
						expected = source
					}
					require.Equal(t, expected, syntax.StripDirectives(source, lexicon, prefixes))
				}
			})
			t.Run("preserved text", func(t *testing.T) {
				for _, source := range []string{
					"-- $1 as other.param(a)\nSELECT 1;",
					"-- $1\v as seagull.param(a)\nSELECT 1;",
					"-- $1\f as seagull.param(a)\nSELECT 1;",
					"SELECT '\n-- $1\tas\tseagull.param(a)\n';",
					"/*\n-- $1\tas\tseagull.param(a)\n*/\nSELECT 1;",
				} {
					require.Equal(t, source, syntax.FormatSource(source, lexicon, prefixes))
					require.Equal(t, source, syntax.StripDirectives(source, lexicon, prefixes))
				}
			})
		})
	}
}

func TestMultilineDirectiveOwnershipAcrossDialects(t *testing.T) {
	for _, engine := range builtinDialects(t) {
		info := engine.Info()
		t.Run(info.Name, func(t *testing.T) {
			prefixes := namespace.Set{Canonical: "sgl", Aliases: []string{"seagull"}, Foreign: []string{"other"}}
			for _, opening := range []string{"sgl(", "sgl.query(", "$1 as sgl.param(", "seagull.embed(", "other.query("} {
				source := "-- " + opening + "\n-- value )\n-- sgl.query(A,one)\nSELECT 1;"
				formatted := formatFuzzSource(&info, source, prefixes)
				before, _ := parsedFuzzDirectiveBlocks(&info, prefixes, source)
				after, _ := parsedFuzzDirectiveBlocks(&info, prefixes, formatted)
				require.Contains(t, before, 3, opening)
				require.Empty(t, before[3].value.Description, opening)
				require.Contains(t, after, 3, opening)
				require.Empty(t, after[3].value.Description, opening)
				require.Empty(t, directiveMeaningProblems(&info, prefixes, source, formatted), opening)
			}
		})
	}
}

func TestUnterminatedDirectiveStringsPreserveFollowingProseAcrossDialects(t *testing.T) {
	for _, engine := range builtinDialects(t) {
		info := engine.Info()
		t.Run(info.Name, func(t *testing.T) {
			prefixes := namespace.Set{Canonical: "sgl", Aliases: []string{"seagull"}, Foreign: nil}
			source := "-- sgl(\n-- 'unterminated\n-- Keep  ( this ) exactly.\n-- sgl.query(A,one)\nSELECT 1;"
			formatted := formatFuzzSource(&info, source, prefixes)
			before, truncated := parsedFuzzDirectiveBlocks(&info, prefixes, source)
			require.False(t, truncated)
			after, _ := parsedFuzzDirectiveBlocks(&info, prefixes, formatted)
			require.Contains(t, before, 4)
			require.Equal(t, "Keep  ( this ) exactly.", before[4].value.Description)
			require.Contains(t, after, 4)
			require.Equal(t, before[4].value.Description, after[4].value.Description)
		})
	}
}

func TestDirectiveFormattingPreservesControlCharactersAcrossDialects(t *testing.T) {
	for _, engine := range builtinDialects(t) {
		info := engine.Info()
		t.Run(info.Name, func(t *testing.T) {
			for _, control := range []string{"\v", "\f", "\r"} {
				for _, source := range []string{
					"-- sgl.query(\n-- sgl" + control + "(0",
					"-- sgl(\n--" + control + "0",
					"-- sgl.query(A," + control + "one)\nSELECT 1;",
				} {
					checkFuzzFormatting(t, &info, source)
					formatted := formatFuzzSource(&info, source, namespace.Default())
					require.Contains(t, formatted, control)
				}
			}
		})
	}
}

func TestIncompleteQueryHeadersEndDirectiveContinuationsAcrossDialects(t *testing.T) {
	for _, engine := range builtinDialects(t) {
		info := engine.Info()
		t.Run(info.Name, func(t *testing.T) {
			prefixes := namespace.Set{Canonical: "sgl", Aliases: []string{"seagull"}, Foreign: []string{"other"}}
			for _, prefix := range []string{"sgl", "seagull", "other"} {
				source := "-- sgl(\n-- " + prefix + ".query\n-- ,0\n-- sgl.query(A,mAnY)"
				formatted := formatFuzzSource(&info, source, prefixes)
				require.Empty(t, directiveMeaningProblems(&info, prefixes, source, formatted), "%s => %s", source, formatted)
			}
		})
	}
}

func builtinDialects(t *testing.T) []dialect.DialectPort {
	t.Helper()
	names := dialects.Names()
	engines := make([]dialect.DialectPort, 0, len(names))
	for _, name := range names {
		engine, err := dialects.New(name, dialects.Options{})
		require.NoError(t, err)
		engines = append(engines, engine)
	}
	return engines
}

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
	"errors"
	"slices"
	"strings"
	"testing"

	"seagull.sh/seagull/internal/dialect"
	"seagull.sh/seagull/internal/directive/syntax"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqllex"
	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

// FuzzLexicalProfile exercises scanner and directives under an engine's rules.
//
// Takes f (*testing.F) which registers seeds and failures.
// Takes newDialect (func() dialect.DialectPort) which supplies lexical facts.
// Takes directory (string) which holds golden SQL seeds.
func FuzzLexicalProfile(f *testing.F, newDialect func() dialect.DialectPort, directory string) {
	f.Helper()
	info := newDialect().Info()
	for _, bundle := range fuzzBundles(f, directory) {
		for _, file := range bundle.Files {
			if len(file.SQL) <= fuzzcase.MaxBytes {
				f.Add(file.SQL)
			}
		}
	}
	for _, seed := range []string{
		"", "\xef\xbb\xbfSELECT 1;", "SELECT 'unterminated", "/* nested /* body */ end */",
		"SELECT $1, ?1, :name, {n:Int64};", "-- sgl.query( X ,many)\r\nSELECT '-- sgl.query(Y, one)';",
		"SELECT $$\n-- sgl.query(X, one)\n$$;",
		"--( as seagull.A", "--( as\tseagull.A", "-- ( as\tseagull.A\nSELECT 1;",
		"-- $1\tas\tseagull.param(a)", "-- ?1\tas\tseagull.param(a)", "-- {n:Int64}\tas\tseagull.param(a)",
		"-- $1 AS seagull.param(a)", "-- ?1 As seagull.param(a)", "-- {n:Int64} aS seagull.param(a)",
		"-- $1 \t as\tseagull.param(a)\nSELECT 1;", "-- {n:Int64}  as  seagull.param(a)\nSELECT 1;",
		"-- sgl(\n-- value )\n-- sgl.query(A,one)\nSELECT 1;",
		"-- sgl(\n-- 'unfinished\n-- Keep  ( this ) exactly.\n-- sgl.query(A,one)\nSELECT 1;",
		"-- sgl.query(\n-- sgl\v(0", "-- sgl(\n--\f0",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > fuzzcase.MaxBytes {
			t.Skip("input exceeds lexical bound")
		}
		checkFuzzScanner(t, info.Lexical, source)
		checkFuzzSplit(t, info.Lexical, source)
		checkFuzzFormatting(t, &info, source)
	})
}

// checkFuzzLexicalError validates the position of a rejected lexical input.
//
// Takes t (*testing.T) which reports problems.
// Takes source (string) which is the rejected text.
// Takes err (error) which came from the scanner or splitter.
func checkFuzzLexicalError(t *testing.T, source string, err error) {
	t.Helper()
	lexical, ok := errors.AsType[*sqllex.Error](err)
	if !ok || lexical.Offset < 0 || lexical.Offset > len(source) {
		t.Fatalf("invalid lexical error: %v", err)
	}
}

// checkFormattedTokens checks that formatting keeps every significant SQL token.
//
// Source that does not split or scan has no tokens to keep.
//
// Takes t (*testing.T) which reports failures.
// Takes rules (sqllex.Rules) which defines tokenisation.
// Takes source (string) which is the original SQL.
// Takes formatted (string) which is the formatter's output.
func checkFormattedTokens(t *testing.T, rules sqllex.Rules, source, formatted string) {
	t.Helper()
	if _, err := sqllex.Split(rules, source); err != nil {
		return
	}
	original, err := sqllex.Tokens(rules, source)
	if err != nil {
		return
	}
	changed, err := sqllex.Tokens(rules, formatted)
	if err != nil {
		t.Fatal("formatting broke SQL tokenisation")
	}
	if !slices.Equal(fuzzSQLTokens(original), fuzzSQLTokens(changed)) {
		t.Fatal("formatting changed executable SQL")
	}
}

// fuzzSQLTokens retains executable token text for formatting comparisons.
//
// Takes tokens ([]sqllex.Token) which holds SQL tokens.
//
// Returns []string which holds every significant token in order.
func fuzzSQLTokens(tokens []sqllex.Token) []string {
	var result []string
	for _, token := range tokens {
		if !token.Kind.IsNoise() {
			result = append(result, token.Raw)
		}
	}
	return result
}

// checkFuzzScanner checks that the tokens cover every source byte in order.
//
// Takes t (*testing.T) which reports failures.
// Takes rules (sqllex.Rules) which defines tokenisation.
// Takes source (string) which supplies the bounded input.
func checkFuzzScanner(t *testing.T, rules sqllex.Rules, source string) {
	t.Helper()
	tokens, err := sqllex.Tokens(rules, source)
	if err != nil {
		checkFuzzLexicalError(t, source, err)
		return
	}
	offset := 0
	for _, token := range tokens {
		if token.Start != offset || token.End <= token.Start || token.End > len(source) || token.Raw != source[token.Start:token.End] {
			t.Fatalf("scanner lost byte coverage at %d: %+v", offset, token)
		}
		offset = token.End
	}
	if offset != len(source) {
		t.Fatal("scanner omitted trailing bytes")
	}
}

// checkFuzzSplit checks that statements are ordered and keep their source text.
//
// Takes t (*testing.T) which reports failures.
// Takes rules (sqllex.Rules) which defines statement boundaries.
// Takes source (string) which supplies the bounded input.
func checkFuzzSplit(t *testing.T, rules sqllex.Rules, source string) {
	t.Helper()
	statements, err := sqllex.Split(rules, source)
	if err != nil {
		checkFuzzLexicalError(t, source, err)
		return
	}
	end := 0
	for _, statement := range statements {
		if !validStatementBounds(statement, end, len(source)) {
			t.Fatalf("invalid statement bounds: %+v", statement)
		}
		if statement.Text != source[statement.Start:statement.End] {
			t.Fatal("split changed statement text")
		}
		end = statement.End
	}
}

// validStatementBounds reports whether a statement lies after the previous one and inside
// the source.
//
// Takes statement (sqllex.Statement) which is the split statement.
// Takes previousEnd (int) which is where the previous statement ended.
// Takes sourceLength (int) which is the source length in bytes.
//
// Returns bool which is true when the bounds and position are valid.
func validStatementBounds(statement sqllex.Statement, previousEnd, sourceLength int) bool {
	ordered := statement.Start >= previousEnd && statement.End > statement.Start && statement.End <= sourceLength
	leading := statement.LeadingStart >= 0 && statement.LeadingStart <= statement.Start
	return ordered && leading && statement.Line >= 1 && statement.Column >= 1
}

// checkFuzzFormatting checks that formatting keeps SQL, meaning and idempotence.
//
// Takes t (*testing.T) which reports failures.
// Takes info (*dialect.Info) which supplies lexical and comment rules.
// Takes source (string) which supplies the bounded input.
func checkFuzzFormatting(t *testing.T, info *dialect.Info, source string) {
	t.Helper()
	lexicon := syntax.Lexicon{LinePrefix: info.Comments.LinePrefix, Rules: info.Lexical}
	prefixes := namespace.Set{Canonical: "sgl", Aliases: []string{"seagull"}, Foreign: nil}
	formatted := formatFuzzSource(info, source, prefixes)
	for _, problem := range directiveMeaningProblems(info, prefixes, source, formatted) {
		t.Errorf("%s\nsource: %q\nformatted: %q", problem, source, formatted)
	}
	if formatFuzzSource(info, formatted, prefixes) != formatted {
		t.Fatal("formatting is not idempotent")
	}
	stripped := syntax.StripDirectives(source, lexicon, prefixes)
	if syntax.StripDirectives(formatted, lexicon, prefixes) != stripped {
		t.Fatal("formatting changed content outside directives")
	}
	if syntax.StripDirectives(stripped, lexicon, prefixes) != stripped {
		t.Fatal("stripping is not idempotent")
	}
	checkFormattedTokens(t, info.Lexical, source, formatted)
	if !strings.Contains(source, info.Comments.LinePrefix) && formatted != source {
		t.Fatal("formatting changed text without line comments")
	}
}

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

package sqllex

import (
	"cmp"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const fuzzMaxInputBytes = 4096

func fuzzRuleSets() map[string]Rules {
	everything := Rules{
		ProceduralBlock: func(leadingWords []string) bool {
			return len(leadingWords) > 0 && (leadingWords[0] == "CREATE" || leadingWords[0] == "DO")
		},
		RequiresNoTransaction: func(tokens []Token) bool {
			return len(tokens) > 0 && tokens[0].IsWord("VACUUM")
		},
		Quotes: QuoteRules{
			StringPrefixes:    "BX",
			SingleQuoteEscape: EscapeDoubledOrBackslash,
			DoubleQuoteEscape: EscapeDoubledOrBackslash,
			BacktickEscape:    EscapeBackslash,
			Backticks:         true,
			Brackets:          true,
			DollarQuoting:     true,
			EscapeStrings:     true,
		},
		Parameters: ParameterRules{
			NamedSigils:          ":@$",
			QuestionMark:         true,
			NumberedQuestionMark: true,
			DollarNumbers:        true,
			Braced:               true,
		},
		Comments: CommentRules{
			DoubleDashRequiresWhitespace: true,
			HashLineComments:             true,
			NestedBlockComments:          true,
		},
		DelimiterCommand:             true,
		TerminatorOutsideParentheses: true,
		DollarInIdentifiers:          true,
	}
	backslashStrings := Rules{
		Quotes: QuoteRules{DoubleQuote: DoubleQuoteString, SingleQuoteEscape: EscapeBackslash},
		ProceduralBlock: func([]string) bool {
			return true
		},
	}
	custom := Rules{Terminator: "GO", DelimiterCommand: true}
	return map[string]Rules{
		"standard":   {},
		"everything": everything,
		"strings":    backslashStrings,
		"custom":     custom,
	}
}

func addCorpusSeeds(f *testing.F) {
	f.Helper()

	patterns := []string{
		filepath.Join("..", "dialect", "*", "testdata", "*", "*", "*.sql"),
		filepath.Join("..", "..", "tests", "integration", "*", "testdata", "*", "*", "*.sql"),
		filepath.Join("..", "dialect", "dialecttest", "testdata", "*", "inputs", "*", "*", "*", "*.sql"),
	}
	for _, pattern := range patterns {
		paths, err := filepath.Glob(pattern)
		require.NoError(f, err, "globbing %s", pattern)
		require.NotEmpty(f, paths, "the seed pattern %s matched no file", pattern)
		for _, path := range paths {
			content, readErr := os.ReadFile(path)
			require.NoError(f, readErr, "reading %s", path)
			if len(content) <= fuzzMaxInputBytes {
				f.Add(string(content))
			}
		}
	}
}

func FuzzSplit(f *testing.F) {
	addCorpusSeeds(f)
	for _, seed := range []string{
		"",
		";",
		"\xef\xbb\xbfSELECT 1;",
		"SELECT 'unterminated",
		"/* open comment",
		"/* a /* b */ c */ d;",
		"$tag$ body $tag",
		"select $1, '$$', \"a\"\"b\" -- c\n; vacuum",
		"CREATE TRIGGER t AFTER INSERT ON a BEGIN UPDATE b SET c = CASE WHEN 1 THEN 2 END; END;",
		"CREATE TABLE t (begin int); CREATE TABLE u (id int);",
		"'a\\'b'; E'c\\';d'; `e;f`; [g;h]; # i;j\n k",
		"DELIMITER //\nCREATE PROCEDURE p() BEGIN SELECT 1; END //\nDELIMITER ;\nSELECT 2;",
		"DELIMITER $$\nCREATE FUNCTION f() BEGIN RETURN 1; END$$ DELIMITER ;",
		"DO (a; b); x GO y GO",
		"{id:Int32} ?1 :name @x $y ::z amount$gbp$net",
		"CREATE TRIGGER t BEGIN CASE x WHEN 1 THEN y; END CASE; END;",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > fuzzMaxInputBytes {
			return
		}
		for name, rules := range fuzzRuleSets() {
			checkScannerCoverage(t, name, rules, input)
			checkSplitPartition(t, name, rules, input)
		}
	})
}

func checkScannerCoverage(t *testing.T, name string, rules Rules, input string) {
	t.Helper()

	tokens, err := Tokens(rules, input)
	if err != nil {
		checkLexicalError(t, name, input, err)
		return
	}
	offset := 0
	for _, token := range tokens {
		require.Truef(t, token.Start == offset && token.End > token.Start && token.Raw == input[token.Start:token.End],
			"%s: token %+v does not continue at %d", name, token, offset)
		offset = token.End
	}
	require.Equalf(t, len(input), offset, "%s: tokens stop at %d of %d", name, offset, len(input))
}

func checkLexicalError(t *testing.T, name string, input string, err error) {
	t.Helper()

	lexicalError, ok := errors.AsType[*Error](err)
	require.Truef(t, ok, "%s: error %v is not a lexical error", name, err)
	require.ErrorIsf(t, err, ErrMalformed, "%s: error %v is not a lexical error", name, err)
	require.Truef(t, lexicalError.Offset >= 0 && lexicalError.Offset <= len(input) && lexicalError.Line >= 1 && lexicalError.Column >= 1,
		"%s: error position %+v out of range", name, lexicalError)
}

func checkSplitPartition(t *testing.T, name string, rules Rules, input string) {
	t.Helper()

	result, err := split(rules, input)
	if err != nil {
		checkLexicalError(t, name, input, err)
		return
	}
	var spans []span
	for _, statement := range result.statements {
		checkStatement(t, name, rules, input, statement)
		spans = append(spans, span{start: statement.Start, end: statement.End})
	}
	checkSeparators(t, name, rules, input, result.separators)
	spans = append(spans, result.separators...)
	for _, noise := range result.noise {
		checkNoise(t, name, rules, input, noise)
		spans = append(spans, noise)
	}
	slices.SortFunc(spans, func(left, right span) int { return cmp.Compare(left.start, right.start) })
	offset := 0
	for _, covered := range spans {
		require.Truef(t, covered.start == offset && covered.end > covered.start,
			"%s: span %+v does not continue the partition at %d", name, covered, offset)
		offset = covered.end
	}
	require.Equalf(t, len(input), offset, "%s: the partition stops at %d of %d", name, offset, len(input))
}

func checkStatement(t *testing.T, name string, rules Rules, input string, statement Statement) {
	t.Helper()

	require.Truef(t, statement.Text != "" && statement.Text == input[statement.Start:statement.End],
		"%s: statement %+v has text that is not its span", name, statement)
	require.Truef(t, statement.LeadingStart <= statement.Start && statement.Line >= 1 && statement.Column >= 1,
		"%s: statement %+v has an impossible position", name, statement)
	leading, err := Tokens(rules, input[statement.LeadingStart:statement.Start])
	require.NoErrorf(t, err, "%s: leading text of %+v does not scan", name, statement)
	for _, token := range leading {
		require.Truef(t, token.Kind.IsNoise(), "%s: leading text of %+v holds %+v", name, statement, token)
	}
}

func checkNoise(t *testing.T, name string, rules Rules, input string, noise span) {
	t.Helper()

	tokens, err := Tokens(rules, input[noise.start:noise.end])
	require.NoErrorf(t, err, "%s: noise %+v does not scan", name, noise)
	for _, token := range tokens {
		require.Truef(t, token.Kind.IsNoise(), "%s: noise %+v holds %+v", name, noise, token)
	}
}

func checkSeparators(t *testing.T, name string, rules Rules, input string, separators []span) {
	t.Helper()

	terminator := rules.terminator()
	for _, separator := range separators {
		text := input[separator.start:separator.end]
		if len(text) > len(delimiterCommand) && strings.EqualFold(text[:len(delimiterCommand)], delimiterCommand) &&
			rules.DelimiterCommand && isWhitespace(text[len(delimiterCommand)]) {
			terminator = strings.Fields(text[len(delimiterCommand):])[0]
			continue
		}
		require.Equalf(t, terminator, text, "%s: separator %q is not the terminator %q", name, text, terminator)
	}
}
